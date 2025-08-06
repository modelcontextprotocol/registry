package k8s

import (
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apps/v1"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	networkingv1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/networking/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
)

// DeployMCPRegistry deploys the MCP Registry to the Kubernetes cluster
func DeployMCPRegistry(ctx *pulumi.Context, cluster *providers.ProviderInfo, environment string) error {
	conf := config.New(ctx, "mcp-registry")
	githubClientId := conf.Require("githubClientId")
	githubClientSecret := conf.RequireSecret("githubClientSecret")

	// Determine replica count based on environment
	replicas := 3
	if environment == "prod" {
		replicas = 5
	}

	// Create ConfigMap with non-sensitive configuration
	_, err := corev1.NewConfigMap(ctx, "mcp-registry-config", &corev1.ConfigMapArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("mcp-registry-config"),
			Namespace: pulumi.String(environment),
			Labels: pulumi.StringMap{
				"app":         pulumi.String("mcp-registry"),
				"environment": pulumi.String(environment),
			},
		},
		Data: pulumi.StringMap{
			"DATABASE_URL":  pulumi.Sprintf("mongodb://mongodb.%s.svc.cluster.local:27017", environment),
			"PORT":          pulumi.String("8080"),
			"NODE_ENV":      pulumi.String("production"),
			"LOG_LEVEL":     pulumi.String("info"),
			"CORS_ORIGINS":  pulumi.String("*"),
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	// Create Secret with sensitive configuration
	_, err = corev1.NewSecret(ctx, "mcp-registry-secrets", &corev1.SecretArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("mcp-registry-secrets"),
			Namespace: pulumi.String(environment),
			Labels: pulumi.StringMap{
				"app":         pulumi.String("mcp-registry"),
				"environment": pulumi.String(environment),
			},
		},
		Data: pulumi.StringMap{
			"GITHUB_CLIENT_ID":     pulumi.ToSecret(githubClientId).(pulumi.StringOutput),
			"GITHUB_CLIENT_SECRET": githubClientSecret,
		},
		Type: pulumi.String("Opaque"),
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	// Create Deployment
	deployment, err := v1.NewDeployment(ctx, "mcp-registry", &v1.DeploymentArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("mcp-registry"),
			Namespace: pulumi.String(environment),
			Labels: pulumi.StringMap{
				"app":         pulumi.String("mcp-registry"),
				"environment": pulumi.String(environment),
			},
		},
		Spec: &v1.DeploymentSpecArgs{
			Replicas: pulumi.Int(replicas),
			Selector: &metav1.LabelSelectorArgs{
				MatchLabels: pulumi.StringMap{
					"app": pulumi.String("mcp-registry"),
				},
			},
			Template: &corev1.PodTemplateSpecArgs{
				Metadata: &metav1.ObjectMetaArgs{
					Labels: pulumi.StringMap{
						"app": pulumi.String("mcp-registry"),
					},
				},
				Spec: &corev1.PodSpecArgs{
					Containers: corev1.ContainerArray{
						&corev1.ContainerArgs{
							Name:  pulumi.String("mcp-registry"),
							Image: pulumi.String("registry:latest"),
							Ports: corev1.ContainerPortArray{
								&corev1.ContainerPortArgs{
									ContainerPort: pulumi.Int(8080),
									Name:          pulumi.String("http"),
								},
							},
							EnvFrom: corev1.EnvFromSourceArray{
								&corev1.EnvFromSourceArgs{
									ConfigMapRef: &corev1.ConfigMapEnvSourceArgs{
										Name: pulumi.String("mcp-registry-config"),
									},
								},
								&corev1.EnvFromSourceArgs{
									SecretRef: &corev1.SecretEnvSourceArgs{
										Name: pulumi.String("mcp-registry-secrets"),
									},
								},
							},
							LivenessProbe: &corev1.ProbeArgs{
								HttpGet: &corev1.HTTPGetActionArgs{
									Path: pulumi.String("/v0/health"),
									Port: pulumi.Int(8080),
								},
								InitialDelaySeconds: pulumi.Int(30),
								TimeoutSeconds:      pulumi.Int(5),
							},
							ReadinessProbe: &corev1.ProbeArgs{
								HttpGet: &corev1.HTTPGetActionArgs{
									Path: pulumi.String("/v0/health"),
									Port: pulumi.Int(8080),
								},
								InitialDelaySeconds: pulumi.Int(5),
								TimeoutSeconds:      pulumi.Int(3),
							},
							Resources: &corev1.ResourceRequirementsArgs{
								Requests: pulumi.StringMap{
									"memory": pulumi.String("256Mi"),
									"cpu":    pulumi.String("250m"),
								},
								Limits: pulumi.StringMap{
									"memory": pulumi.String("512Mi"),
									"cpu":    pulumi.String("500m"),
								},
							},
						},
					},
				},
			},
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	// Create Service
	service, err := corev1.NewService(ctx, "mcp-registry", &corev1.ServiceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("mcp-registry"),
			Namespace: pulumi.String(environment),
			Labels: pulumi.StringMap{
				"app":         pulumi.String("mcp-registry"),
				"environment": pulumi.String(environment),
			},
		},
		Spec: &corev1.ServiceSpecArgs{
			Selector: pulumi.StringMap{
				"app": pulumi.String("mcp-registry"),
			},
			Ports: corev1.ServicePortArray{
				&corev1.ServicePortArgs{
					Port:       pulumi.Int(8080),
					TargetPort: pulumi.Int(8080),
					Name:       pulumi.String("http"),
				},
			},
			Type: pulumi.String("ClusterIP"),
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	// Create Ingress
	_, err = networkingv1.NewIngress(ctx, "mcp-registry", &networkingv1.IngressArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("mcp-registry"),
			Namespace: pulumi.String(environment),
			Labels: pulumi.StringMap{
				"app":         pulumi.String("mcp-registry"),
				"environment": pulumi.String(environment),
			},
			Annotations: pulumi.StringMap{
				"kubernetes.io/ingress.class":                pulumi.String("nginx"),
				"cert-manager.io/cluster-issuer":             pulumi.String("letsencrypt-prod"),
				"nginx.ingress.kubernetes.io/rewrite-target": pulumi.String("/"),
			},
		},
		Spec: &networkingv1.IngressSpecArgs{
			Tls: networkingv1.IngressTLSArray{
				&networkingv1.IngressTLSArgs{
					Hosts: pulumi.StringArray{
						pulumi.Sprintf("mcp-registry-%s.example.com", environment),
					},
					SecretName: pulumi.Sprintf("mcp-registry-%s-tls", environment),
				},
			},
			Rules: networkingv1.IngressRuleArray{
				&networkingv1.IngressRuleArgs{
					Host: pulumi.Sprintf("mcp-registry-%s.example.com", environment),
					Http: &networkingv1.HTTPIngressRuleValueArgs{
						Paths: networkingv1.HTTPIngressPathArray{
							&networkingv1.HTTPIngressPathArgs{
								Path:     pulumi.String("/"),
								PathType: pulumi.String("Prefix"),
								Backend: &networkingv1.IngressBackendArgs{
									Service: &networkingv1.IngressServiceBackendArgs{
										Name: service.Metadata.Name().Elem(),
										Port: &networkingv1.ServiceBackendPortArgs{
											Number: pulumi.Int(8080),
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	// Output deployment status
	ctx.Export("registryDeployment", deployment.Metadata.Name())

	return nil
}