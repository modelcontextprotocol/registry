package k8s

import (
	v1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/helm/v3"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
)

// Preserve ingress-nginx's combined-log prefix for existing consumers. Append
// cache diagnostics; the original request URI, method, request ID, total time,
// upstream address and response time are already present in that prefix.
const registryIngressLogFormat = `$remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$http_referer" "$http_user_agent" $request_length $request_time [$proxy_upstream_name] [$proxy_alternative_upstream_name] $upstream_addr $upstream_response_length $upstream_response_time $upstream_status $req_id cache_status="$upstream_cache_status" scheme="$scheme" host="$host" ingress_pod="$hostname" upstream_connect_time="$upstream_connect_time" upstream_header_time="$upstream_header_time" completed_at="$msec"`

// SetupIngressController sets up the NGINX Ingress Controller
//
//nolint:goconst // Keep Kubernetes and Helm schema keys literal.
func SetupIngressController(ctx *pulumi.Context, cluster *providers.ProviderInfo, environment string) (*helm.Chart, error) {
	conf := config.New(ctx, "mcp-registry")
	provider := conf.Get("provider")
	if provider == "" {
		provider = "local"
	}

	// Create namespace for ingress-nginx
	ingressNginxNamespace, err := v1.NewNamespace(ctx, "ingress-nginx", &v1.NamespaceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String("ingress-nginx"),
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return nil, err
	}

	// Usually we should expose the ingress to a LoadBalancer
	// This works in GCP and most local setups e.g. minikube (with minikube tunnel)
	// Kind unfortunately does not support LoadBalancer type, and hangs indefinitely. This is a workaround for that.
	serviceType := cluster.Name.ApplyT(func(name string) string {
		if name == "kind-kind" {
			return "NodePort"
		}
		return "LoadBalancer"
	}).(pulumi.StringOutput)

	// Match the production cache and load-balancing topology in staging so
	// performance tests exercise two independent ingress replicas.
	replicaCount := 1
	if environment == "prod" || environment == "staging" {
		replicaCount = 2
	}

	ingressCPURequest := "100m"
	spreadConstraints := pulumi.Array{}
	if environment == "prod" || environment == "staging" {
		// Reserve CPU for the proxy and its health endpoint without limiting
		// bursts or exhausting the CPU needed for a rolling-update surge pod.
		ingressCPURequest = "300m"
		spreadConstraints = pulumi.Array{pulumi.Map{
			"maxSkew":           pulumi.Int(1),
			"topologyKey":       pulumi.String("kubernetes.io/hostname"),
			"whenUnsatisfiable": pulumi.String("DoNotSchedule"),
			"nodeTaintsPolicy":  pulumi.String("Honor"),
			"matchLabelKeys":    pulumi.StringArray{pulumi.String("pod-template-hash")},
			"labelSelector": pulumi.Map{
				"matchLabels": pulumi.StringMap{
					"app.kubernetes.io/name":      pulumi.String("ingress-nginx"),
					"app.kubernetes.io/instance":  pulumi.String("ingress-nginx"),
					"app.kubernetes.io/component": pulumi.String("controller"),
				},
			},
		}}
	}

	// Enable this only after the same stack opts into node autoscaling. Keep
	// existing environments unchanged until their explicit promotion.
	burstAutoscaling := config.New(ctx, "mcp-registry").GetBool("burstAutoscaling")
	ingressMemoryRequest := "90Mi"
	clientKeepaliveSnippet := ""
	autoscaling := pulumi.Map{"enabled": pulumi.Bool(false)}
	chartOptions := []pulumi.ResourceOption{pulumi.Provider(cluster.Provider)}
	if burstAutoscaling {
		ingressCPURequest = "800m"
		ingressMemoryRequest = "512Mi"
		// Bound active connection age so added controllers can receive existing
		// client traffic. NGINX finishes the current request before closing.
		clientKeepaliveSnippet = "keepalive_time 60s;\n"
		autoscaling = ingressAutoscalingValues()
		if conf.GetBool("coordinatedCapacity") {
			replicaCount = coordinatedIdleReplicas
			autoscaling["minReplicas"] = pulumi.Int(coordinatedIdleReplicas)
			// Scale at 560m instead of 704m to add capacity before contention.
			autoscaling["targetCPUUtilizationPercentage"] = pulumi.Int(70)
		}
		// HPA owns replicas. Refresh before updates so the checkpoint follows
		// the controller rather than resetting a scaled Deployment to two.
		chartOptions = append(chartOptions, pulumi.Transformations([]pulumi.ResourceTransformation{burstResourceTransform}))
	}

	// Install NGINX Ingress Controller
	ingressNginx, err := helm.NewChart(ctx, "ingress-nginx", helm.ChartArgs{
		Chart:   pulumi.String("ingress-nginx"),
		Version: pulumi.String("4.13.0"),
		FetchArgs: helm.FetchArgs{
			Repo: pulumi.String("https://kubernetes.github.io/ingress-nginx"),
		},
		Namespace: ingressNginxNamespace.Metadata.Name().Elem(),
		Values: pulumi.Map{
			"controller": pulumi.Map{
				"replicaCount": pulumi.Int(replicaCount),
				"autoscaling":  autoscaling,
				"metrics":      pulumi.Map{"enabled": pulumi.Bool(burstAutoscaling)},
				"resources": pulumi.Map{
					"requests": pulumi.StringMap{
						"cpu":    pulumi.String(ingressCPURequest),
						"memory": pulumi.String(ingressMemoryRequest),
					},
				},
				"topologySpreadConstraints": spreadConstraints,
				"updateStrategy": pulumi.Map{
					"type": pulumi.String("RollingUpdate"),
					"rollingUpdate": pulumi.Map{
						"maxSurge":       pulumi.Int(1),
						"maxUnavailable": pulumi.Int(0),
					},
				},
				// Keep readiness more responsive than liveness: a short stall should
				// stop new traffic before it causes a restart and discards the cache.
				// Helm retains the other probe defaults (10s period, 3/5 failures).
				"readinessProbe": pulumi.Map{"timeoutSeconds": pulumi.Int(3)},
				"livenessProbe":  pulumi.Map{"timeoutSeconds": pulumi.Int(5)},
				"service": pulumi.Map{
					"type":                  serviceType,
					"externalTrafficPolicy": pulumi.String("Local"),
					"annotations":           pulumi.Map{},
				},
				"config": pulumi.Map{
					"log-format-upstream": pulumi.String(registryIngressLogFormat),
					// Cache the public list endpoints at the ingress. Registry consumers
					// repeatedly walk the same cursor pages, so sending every walk to the
					// application needlessly consumes application CPU and database pool
					// connections. The cache is intentionally short-lived so newly
					// published servers become visible quickly.
					//
					// Use a variable cache zone so this global location snippet remains a
					// no-op for publish, auth, mutation, and individual-server routes.
					"http-snippet": pulumi.String(`
proxy_cache_path /tmp/registry-read-cache levels=1:2 keys_zone=registry_read_cache:20m max_size=1g inactive=5m use_temp_path=off;
map "$request_method:$uri" $registry_read_cache_zone {
    default off;
    ~^GET:/v0(?:\.1)?/servers$ registry_read_cache;
}
` + clientKeepaliveSnippet),
					"location-snippet": pulumi.String(`
proxy_cache $registry_read_cache_zone;
proxy_cache_key "$scheme$request_method$host$request_uri";
proxy_cache_valid 200 30s;
proxy_cache_lock on;
proxy_cache_lock_timeout 5s;
proxy_cache_background_update on;
proxy_cache_use_stale updating error timeout http_500 http_502 http_503 http_504;
add_header X-Registry-Cache $upstream_cache_status always;
`),
					// Disable strict path validation, to work around a bug in ingress-nginx
					// https://cert-manager.io/docs/releases/release-notes/release-notes-1.18/#acme-http01-challenge-paths-now-use-pathtype-exact-in-ingress-routes
					// https://github.com/kubernetes/ingress-nginx/issues/11176
					"strict-validate-path-type": pulumi.String("false"),

					// Do NOT use forwarded headers with L4 load balancer
					// GCP L4 Passthrough Network Load Balancer does not set X-Forwarded-For
					// Real client IP comes from TCP connection source with externalTrafficPolicy: Local
					"use-forwarded-headers": pulumi.String("false"),

					// Set rate limit rejection status code to 429 (Too Many Requests)
					"limit-req-status-code": pulumi.String("429"),
				},
			},
		},
	}, chartOptions...)
	if err != nil {
		return nil, err
	}

	// Extract ingress IPs from the Helm chart's controller service
	ingressIps := ingressNginx.Resources.ApplyT(func(resources interface{}) interface{} {
		// Look for the ingress-nginx-controller service
		resourceMap := resources.(map[string]pulumi.Resource)
		for resourceName, resource := range resourceMap {
			if resourceName == "v1/Service::ingress-nginx/ingress-nginx-controller" {
				if svc, ok := resource.(*v1.Service); ok {
					// Return the LoadBalancer ingress IPs
					return svc.Status.LoadBalancer().Ingress().ApplyT(func(ingresses []v1.LoadBalancerIngress) []string {
						var ips []string
						for _, ingress := range ingresses {
							if ip := ingress.Ip; ip != nil && *ip != "" {
								ips = append(ips, *ip)
							}
						}
						return ips
					})
				}
			}
		}
		// Return empty array if no matching service found
		return []string{}
	})
	ctx.Export("ingressIps", ingressIps)

	return ingressNginx, nil
}
