package k8s

import (
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apiextensions"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/helm/v3"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	networkingv1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/networking/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
	"gopkg.in/yaml.v2"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
)

func DeployMonitoringStack(ctx *pulumi.Context, cluster *providers.ProviderInfo, environment string, ingressNginx *helm.Chart) error {
	// Create namespace
	ns, err := corev1.NewNamespace(ctx, "monitoring", &corev1.NamespaceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String("monitoring"),
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	// Deploy VictoriaMetrics
	_, err = helm.NewChart(ctx, "victoria-metrics", helm.ChartArgs{
		Chart:     pulumi.String("victoria-metrics-single"),
		Version:   pulumi.String("0.24.4"),
		Namespace: ns.Metadata.Name().Elem(),
		FetchArgs: helm.FetchArgs{
			Repo: pulumi.String("https://victoriametrics.github.io/helm-charts/"),
		},
		Values: pulumi.Map{
			"server": pulumi.Map{
				"retentionPeriod": pulumi.String("14d"),
				"resources": pulumi.Map{
					"requests": pulumi.Map{
						"memory": pulumi.String("128Mi"),
						"cpu":    pulumi.String("50m"),
					},
					"limits": pulumi.Map{
						"memory": pulumi.String("256Mi"),
					},
				},
			},
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	// Deploy VMAgent
	_, err = helm.NewChart(ctx, "victoria-metrics-agent", helm.ChartArgs{
		Chart:     pulumi.String("victoria-metrics-agent"),
		Version:   pulumi.String("0.25.3"),
		Namespace: ns.Metadata.Name().Elem(),
		FetchArgs: helm.FetchArgs{
			Repo: pulumi.String("https://victoriametrics.github.io/helm-charts/"),
		},
		Values: pulumi.Map{
			"remoteWrite": pulumi.Array{
				pulumi.Map{
					"url": pulumi.String("http://victoria-metrics-victoria-metrics-single-server:8428/api/v1/write"),
				},
			},
			"config": pulumi.Map{
				"global": pulumi.Map{
					"scrape_interval": pulumi.String("60s"),
				},
				"scrape_configs": pulumi.Array{
					pulumi.Map{
						"job_name": pulumi.String("mcp-registry"),
						"kubernetes_sd_configs": pulumi.Array{
							pulumi.Map{
								"role": pulumi.String("pod"),
								"namespaces": pulumi.Map{
									"names": pulumi.Array{pulumi.String("default")},
								},
							},
						},
						"relabel_configs": pulumi.Array{
							pulumi.Map{
								"source_labels": pulumi.Array{pulumi.String("__meta_kubernetes_pod_label_app")},
								"regex":         pulumi.String("mcp-registry.*"),
								"action":        pulumi.String("keep"),
							},
						},
					},
				},
			},
			"resources": pulumi.Map{
				"requests": pulumi.Map{
					"memory": pulumi.String("64Mi"),
					"cpu":    pulumi.String("25m"),
				},
				"limits": pulumi.Map{
					"memory": pulumi.String("128Mi"),
				},
			},
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	// Deploy VictoriaLogs for log storage
	err = deployVictoriaLogs(ctx, cluster, ns, environment)
	if err != nil {
		return err
	}

	// Deploy OpenTelemetry Collector DaemonSet
	err = deployOtelCollectorDaemonSet(ctx, cluster, ns, environment)
	if err != nil {
		return err
	}

	// Deploy Grafana
	return deployGrafana(ctx, cluster, ns, environment, ingressNginx)
}

// deployVictoriaLogs deploys VictoriaLogs for log storage
func deployVictoriaLogs(ctx *pulumi.Context, cluster *providers.ProviderInfo, ns *corev1.Namespace, environment string) error {
	// Deploy VictoriaLogs using Helm chart
	_, err := helm.NewChart(ctx, "victoria-logs", helm.ChartArgs{
		Chart:     pulumi.String("victoria-logs-single"),
		Version:   pulumi.String("0.11.8"),
		Namespace: ns.Metadata.Name().Elem(),
		FetchArgs: helm.FetchArgs{
			Repo: pulumi.String("https://victoriametrics.github.io/helm-charts/"),
		},
		Values: pulumi.Map{
			"server": pulumi.Map{
				"retentionPeriod": pulumi.String("15d"),
				"resources": pulumi.Map{
					"requests": pulumi.Map{
						"memory": pulumi.String("256Mi"),
						"cpu":    pulumi.String("100m"),
					},
					"limits": pulumi.Map{
						"memory": pulumi.String("2Gi"),
						"cpu":    pulumi.String("1000m"),
					},
				},
				"persistence": pulumi.Map{
					"enabled": pulumi.Bool(true),
					"size":    pulumi.String("20Gi"),
				},
			},
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	return nil
}

// deployOtelCollectorDaemonSet deploys OpenTelemetry Collector using Helm chart
func deployOtelCollectorDaemonSet(ctx *pulumi.Context, cluster *providers.ProviderInfo, ns *corev1.Namespace, environment string) error {
	// Deploy OpenTelemetry Collector using Helm chart
	_, err := helm.NewChart(ctx, "opentelemetry-collector", helm.ChartArgs{
		Chart:     pulumi.String("opentelemetry-collector"),
		Version:   pulumi.String("0.133.0"),
		Namespace: ns.Metadata.Name().Elem(),
		FetchArgs: helm.FetchArgs{
			Repo: pulumi.String("https://open-telemetry.github.io/opentelemetry-helm-charts"),
		},
		Values: pulumi.Map{
			"mode": pulumi.String("daemonset"),
			"image": pulumi.Map{
				"repository": pulumi.String("otel/opentelemetry-collector-contrib"),
				"tag":        pulumi.String("0.133.0"),
			},
			"clusterRole": pulumi.Map{
				"create": pulumi.Bool(true),
				"rules": pulumi.Array{
					pulumi.Map{
						"apiGroups": pulumi.StringArray{pulumi.String("")},
						"resources": pulumi.StringArray{
							pulumi.String("pods"),
							pulumi.String("pods/log"),
							pulumi.String("nodes"),
							pulumi.String("namespaces"),
						},
						"verbs": pulumi.StringArray{
							pulumi.String("get"),
							pulumi.String("list"),
							pulumi.String("watch"),
						},
					},
				},
			},
			"config": pulumi.Map{
				"receivers": pulumi.Map{
					"filelog": pulumi.Map{
						"include":           pulumi.StringArray{pulumi.String("/var/log/pods/default_mcp-registry*/*/*.log")},
						"exclude":           pulumi.StringArray{pulumi.String("/var/log/pods/*/*-collector-*/*.log")},
						"start_at":          pulumi.String("end"),
						"include_file_path": pulumi.Bool(true),
						"include_file_name": pulumi.Bool(false),
						"operators": pulumi.Array{
							pulumi.Map{
								"type":       pulumi.String("regex_parser"),
								"id":         pulumi.String("extract_metadata_from_filepath"),
								"regex":      pulumi.String(`^.*\/(?P<namespace>[^_]+)_(?P<pod_name>[^_]+)_(?P<uid>[a-f0-9\-]{36})\/(?P<container_name>[^\._]+)\/(?P<restart_count>\d+)\.log`),
								"parse_from": pulumi.String("attributes[\"log.file.path\"]"),
								"cache": pulumi.Map{
									"size": pulumi.Int(128),
								},
							},
							pulumi.Map{
								"type": pulumi.String("move"),
								"from": pulumi.String("attributes.container_name"),
								"to":   pulumi.String("resource[\"k8s.container.name\"]"),
							},
							pulumi.Map{
								"type": pulumi.String("move"),
								"from": pulumi.String("attributes.namespace"),
								"to":   pulumi.String("resource[\"k8s.namespace.name\"]"),
							},
							pulumi.Map{
								"type": pulumi.String("move"),
								"from": pulumi.String("attributes.pod_name"),
								"to":   pulumi.String("resource[\"k8s.pod.name\"]"),
							},
							pulumi.Map{
								"type": pulumi.String("move"),
								"from": pulumi.String("attributes.restart_count"),
								"to":   pulumi.String("resource[\"k8s.container.restart_count\"]"),
							},
							pulumi.Map{
								"type": pulumi.String("move"),
								"from": pulumi.String("attributes.uid"),
								"to":   pulumi.String("resource[\"k8s.pod.uid\"]"),
							},
						},
					},
				},
				"processors": pulumi.Map{
					"batch": pulumi.Map{},
					"k8sattributes": pulumi.Map{
						"auth_type":   pulumi.String("serviceAccount"),
						"passthrough": pulumi.Bool(false),
						"filter": pulumi.Map{
							"node_from_env_var": pulumi.String("KUBERNETES_NODE_NAME"),
						},
						"extract": pulumi.Map{
							"metadata": pulumi.StringArray{
								pulumi.String("k8s.pod.name"),
								pulumi.String("k8s.pod.uid"),
								pulumi.String("k8s.deployment.name"),
								pulumi.String("k8s.namespace.name"),
								pulumi.String("k8s.node.name"),
								pulumi.String("k8s.pod.start_time"),
								pulumi.String("k8s.cluster.uid"),
							},
							"labels": pulumi.Array{
								pulumi.Map{
									"tag_name": pulumi.String("app"),
									"key":      pulumi.String("app"),
									"from":     pulumi.String("pod"),
								},
							},
						},
						"pod_association": pulumi.Array{
							pulumi.Map{
								"sources": pulumi.Array{
									pulumi.Map{
										"from": pulumi.String("resource_attribute"),
										"name": pulumi.String("k8s.pod.name"),
									},
									pulumi.Map{
										"from": pulumi.String("resource_attribute"),
										"name": pulumi.String("k8s.namespace.name"),
									},
								},
							},
						},
					},
				},
				"exporters": pulumi.Map{
					"otlphttp/victorialogs": pulumi.Map{
						"logs_endpoint": pulumi.String("http://victoria-logs-victoria-logs-single-server:9428/insert/opentelemetry/v1/logs"),
						"headers": pulumi.Map{
							"VL-Msg-Field":     pulumi.String("body"),
							"VL-Time-Field":    pulumi.String("timestamp"),
							"VL-Stream-Fields": pulumi.String("k8s.namespace.name,k8s.pod.name,k8s.container.name,log.iostream"),
						},
						"timeout": pulumi.String("10s"),
						"retry_on_failure": pulumi.Map{
							"enabled":          pulumi.Bool(true),
							"initial_interval": pulumi.String("5s"),
							"max_interval":     pulumi.String("30s"),
							"max_elapsed_time": pulumi.String("300s"),
						},
						"sending_queue": pulumi.Map{
							"enabled":       pulumi.Bool(true),
							"num_consumers": pulumi.Int(10),
							"queue_size":    pulumi.Int(50),
						},
					},
				},
				"service": pulumi.Map{
					"pipelines": pulumi.Map{
						"logs": pulumi.Map{
							"receivers":  pulumi.StringArray{pulumi.String("filelog")},
							"processors": pulumi.StringArray{pulumi.String("batch"), pulumi.String("k8sattributes")},
							"exporters":  pulumi.StringArray{pulumi.String("otlphttp/victorialogs")},
						},
					},
				},
			},
			"extraVolumes": pulumi.Array{
				pulumi.Map{
					"name": pulumi.String("varlogpods"),
					"hostPath": pulumi.Map{
						"path": pulumi.String("/var/log/pods"),
					},
				},
				pulumi.Map{
					"name": pulumi.String("varlibdockercontainers"),
					"hostPath": pulumi.Map{
						"path": pulumi.String("/var/lib/docker/containers"),
					},
				},
			},
			"extraVolumeMounts": pulumi.Array{
				pulumi.Map{
					"name":      pulumi.String("varlogpods"),
					"mountPath": pulumi.String("/var/log/pods"),
					"readOnly":  pulumi.Bool(true),
				},
				pulumi.Map{
					"name":      pulumi.String("varlibdockercontainers"),
					"mountPath": pulumi.String("/var/lib/docker/containers"),
					"readOnly":  pulumi.Bool(true),
				},
			},
			"extraEnvs": pulumi.Array{
				pulumi.Map{
					"name": pulumi.String("KUBERNETES_NODE_NAME"),
					"valueFrom": pulumi.Map{
						"fieldRef": pulumi.Map{
							"fieldPath": pulumi.String("spec.nodeName"),
						},
					},
				},
			},
			"resources": pulumi.Map{
				"requests": pulumi.Map{
					"memory": pulumi.String("200Mi"),
					"cpu":    pulumi.String("100m"),
				},
				"limits": pulumi.Map{
					"memory": pulumi.String("400Mi"),
					"cpu":    pulumi.String("200m"),
				},
			},
			"tolerations": pulumi.Array{
				pulumi.Map{
					"key":      pulumi.String("node-role.kubernetes.io/master"),
					"operator": pulumi.String("Exists"),
					"effect":   pulumi.String("NoSchedule"),
				},
				pulumi.Map{
					"key":      pulumi.String("node-role.kubernetes.io/control-plane"),
					"operator": pulumi.String("Exists"),
					"effect":   pulumi.String("NoSchedule"),
				},
			},
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	return nil
}

func deployGrafana(ctx *pulumi.Context, cluster *providers.ProviderInfo, ns *corev1.Namespace, environment string, ingressNginx *helm.Chart) error {
	conf := config.New(ctx, "mcp-registry")
	grafanaSecret, err := corev1.NewSecret(ctx, "grafana-secrets", &corev1.SecretArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("grafana-secrets"),
			Namespace: ns.Metadata.Name(),
		},
		StringData: pulumi.StringMap{
			"GF_AUTH_GOOGLE_CLIENT_SECRET": conf.RequireSecret("googleOauthClientSecret"),
		},
		Type: pulumi.String("Opaque"),
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	grafanaPgCluster, err := apiextensions.NewCustomResource(ctx, "grafana-pg", &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String("postgresql.cnpg.io/v1"),
		Kind:       pulumi.String("Cluster"),
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("grafana-pg"),
			Namespace: ns.Metadata.Name(),
			Labels: pulumi.StringMap{
				"app":         pulumi.String("grafana-pg"),
				"environment": pulumi.String(environment),
			},
		},
		OtherFields: map[string]any{
			"spec": map[string]any{
				"instances": 1,
				"storage": map[string]any{
					"size": "10Gi",
				},
			},
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	// Create VictoriaMetrics and VictoriaLogs datasources
	datasourcesConfig := map[string]interface{}{
		"apiVersion": 1,
		"datasources": []map[string]interface{}{
			{
				"name":      "VictoriaMetrics",
				"type":      "prometheus",
				"url":       "http://victoria-metrics-victoria-metrics-single-server:8428",
				"access":    "proxy",
				"isDefault": true,
			},
			{
				"name":   "VictoriaLogs",
				"type":   "victoriametrics-logs-datasource",
				"url":    "http://victoria-logs-victoria-logs-single-server:9428",
				"access": "proxy",
				"jsonData": map[string]interface{}{
					"maxLines": 1000,
				},
			},
		},
	}

	datasourcesConfigYAML, _ := yaml.Marshal(datasourcesConfig)
	grafanaDataSourcesConfigMap, err := corev1.NewConfigMap(ctx, "grafana-datasources", &corev1.ConfigMapArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("grafana-datasources"),
			Namespace: ns.Metadata.Name(),
		},
		Data: pulumi.StringMap{
			"datasources.yaml": pulumi.String(string(datasourcesConfigYAML)),
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	// Deploy Grafana
	grafanaHost := "grafana." + environment + ".registry.modelcontextprotocol.io"
	_, err = helm.NewChart(ctx, "grafana", helm.ChartArgs{
		Chart:   pulumi.String("grafana"),
		Version: pulumi.String("9.4.4"),
		FetchArgs: &helm.FetchArgs{
			Repo: pulumi.String("https://grafana.github.io/helm-charts"),
		},
		Namespace: ns.Metadata.Name().Elem(),
		Values: pulumi.Map{
			"plugins": pulumi.Array{
				pulumi.String("victoriametrics-logs-datasource"),
			},
			"extraConfigmapMounts": pulumi.Array{
				pulumi.Map{
					"name":      pulumi.String("grafana-datasources"),
					"mountPath": pulumi.String("/etc/grafana/provisioning/datasources"),
					"configMap": grafanaDataSourcesConfigMap.Metadata.Name(),
					"readOnly":  pulumi.Bool(true),
				},
			},
			"grafana.ini": pulumi.Map{
				"server": pulumi.Map{
					"root_url": pulumi.String("https://" + grafanaHost),
				},
				"auth": pulumi.Map{
					"disable_login_form": pulumi.Bool(true),
				},
				"auth.basic": pulumi.Map{
					"enabled": pulumi.Bool(false),
				},
				"security": pulumi.Map{
					"disable_initial_admin_creation": pulumi.Bool(true),
				},
				"users": pulumi.Map{
					"auto_assign_org_role": pulumi.String("Admin"),
				},
				"auth.google": pulumi.Map{
					"enabled":            pulumi.Bool(true),
					"client_id":          pulumi.String("606636202366-tpjm7d5vpp4lp9helg5ld2vrcafnrgh7.apps.googleusercontent.com"),
					"hosted_domain":      pulumi.String("modelcontextprotocol.io"),
					"allowed_domains":    pulumi.String("modelcontextprotocol.io"),
					"skip_org_role_sync": pulumi.Bool(true),
				},
				"database": pulumi.Map{
					"type": pulumi.String("postgres"),
					"host": pulumi.String("grafana-pg-rw:5432"),
				},
			},
			"envValueFrom": pulumi.Map{
				"GF_AUTH_GOOGLE_CLIENT_SECRET": pulumi.Map{
					"secretKeyRef": pulumi.Map{
						"name": grafanaSecret.Metadata.Name(),
						"key":  pulumi.String("GF_AUTH_GOOGLE_CLIENT_SECRET"),
					},
				},
				"GF_DATABASE_USER": pulumi.Map{
					"secretKeyRef": pulumi.Map{
						"name": grafanaPgCluster.Metadata.Name().ApplyT(func(name *string) string {
							if name == nil {
								return "grafana-pg-app"
							}
							return *name + "-app"
						}).(pulumi.StringOutput),
						"key": pulumi.String("username"),
					},
				},
				"GF_DATABASE_PASSWORD": pulumi.Map{
					"secretKeyRef": pulumi.Map{
						"name": grafanaPgCluster.Metadata.Name().ApplyT(func(name *string) string {
							if name == nil {
								return "grafana-pg-app"
							}
							return *name + "-app"
						}).(pulumi.StringOutput),
						"key": pulumi.String("password"),
					},
				},
				"GF_DATABASE_NAME": pulumi.Map{
					"secretKeyRef": pulumi.Map{
						"name": grafanaPgCluster.Metadata.Name().ApplyT(func(name *string) string {
							if name == nil {
								return "grafana-pg-app"
							}
							return *name + "-app"
						}).(pulumi.StringOutput),
						"key": pulumi.String("dbname"),
					},
				},
			},
			"resources": pulumi.Map{
				"requests": pulumi.Map{
					"memory": pulumi.String("128Mi"),
					"cpu":    pulumi.String("50m"),
				},
				"limits": pulumi.Map{
					"memory": pulumi.String("256Mi"),
				},
			},
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	// Create ingress for external access
	_, err = networkingv1.NewIngress(ctx, "grafana-ingress", &networkingv1.IngressArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("grafana-ingress"),
			Namespace: ns.Metadata.Name(),
			Annotations: pulumi.StringMap{
				"cert-manager.io/cluster-issuer": pulumi.String("letsencrypt-prod"),
				"kubernetes.io/ingress.class":    pulumi.String("nginx"),
			},
		},
		Spec: &networkingv1.IngressSpecArgs{
			Tls: networkingv1.IngressTLSArray{
				&networkingv1.IngressTLSArgs{
					Hosts:      pulumi.StringArray{pulumi.String(grafanaHost)},
					SecretName: pulumi.String("grafana-tls"),
				},
			},
			Rules: networkingv1.IngressRuleArray{
				&networkingv1.IngressRuleArgs{
					Host: pulumi.String(grafanaHost),
					Http: &networkingv1.HTTPIngressRuleValueArgs{
						Paths: networkingv1.HTTPIngressPathArray{
							&networkingv1.HTTPIngressPathArgs{
								Path:     pulumi.String("/"),
								PathType: pulumi.String("Prefix"),
								Backend: &networkingv1.IngressBackendArgs{
									Service: &networkingv1.IngressServiceBackendArgs{
										Name: pulumi.String("grafana"),
										Port: &networkingv1.ServiceBackendPortArgs{
											Number: pulumi.Int(80),
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}, pulumi.Provider(cluster.Provider), pulumi.DependsOnInputs(ingressNginx.Ready))
	if err != nil {
		return err
	}

	ctx.Export("grafanaUrl", pulumi.Sprintf("https://%s", grafanaHost))
	return nil
}
