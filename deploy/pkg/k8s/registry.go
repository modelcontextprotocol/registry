package k8s

import (
	v1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apps/v1"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	networkingv1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/networking/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
)

// DeployMCPRegistry deploys the MCP Registry to the Kubernetes cluster
func DeployMCPRegistry(ctx *pulumi.Context, cluster *providers.ProviderInfo, environment string) (*corev1.Service, error) {
	conf := config.New(ctx, "mcp-registry")
	githubClientId := conf.Require("githubClientId")
	githubClientSecret := conf.RequireSecret("githubClientSecret")

	// Create Secret with sensitive configuration
	secret, err := corev1.NewSecret(ctx, "mcp-registry-secrets", &corev1.SecretArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("mcp-registry-secrets"),
			Namespace: pulumi.String("default"),
			Labels: pulumi.StringMap{
				"app":         pulumi.String("mcp-registry"),
				"environment": pulumi.String(environment),
			},
		},
		StringData: pulumi.StringMap{
			"GITHUB_CLIENT_SECRET": githubClientSecret,
		},
		Type: pulumi.String("Opaque"),
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return nil, err
	}

	// Create Deployment
	_, err = v1.NewDeployment(ctx, "mcp-registry", &v1.DeploymentArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("mcp-registry"),
			Namespace: pulumi.String("default"),
			Labels: pulumi.StringMap{
				"app":         pulumi.String("mcp-registry"),
				"environment": pulumi.String(environment),
			},
		},
		Spec: &v1.DeploymentSpecArgs{
			Replicas: pulumi.Int(2),
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
							Name: pulumi.String("mcp-registry"),
							// TODO: Replace with actual MCP registry image once on GHCR
							Image: pulumi.String("nginx:alpine"),
							Ports: corev1.ContainerPortArray{
								&corev1.ContainerPortArgs{
									// TODO: Change to port 8080 once using registry image
									ContainerPort: pulumi.Int(80),
									Name:          pulumi.String("http"),
								},
							},
							Env: corev1.EnvVarArray{
								&corev1.EnvVarArgs{
									Name:  pulumi.String("DATABASE_URL"),
									Value: pulumi.String("mongodb://mongodb.default.svc.cluster.local:27017"),
								},
								&corev1.EnvVarArgs{
									Name:  pulumi.String("PORT"),
									Value: pulumi.String("8080"),
								},
								&corev1.EnvVarArgs{
									Name:  pulumi.String("NODE_ENV"),
									Value: pulumi.String("production"),
								},
								&corev1.EnvVarArgs{
									Name:  pulumi.String("LOG_LEVEL"),
									Value: pulumi.String("info"),
								},
								&corev1.EnvVarArgs{
									Name:  pulumi.String("CORS_ORIGINS"),
									Value: pulumi.String("*"),
								},
								&corev1.EnvVarArgs{
									Name:  pulumi.String("GITHUB_CLIENT_ID"),
									Value: pulumi.String(githubClientId),
								},
								&corev1.EnvVarArgs{
									Name: pulumi.String("GITHUB_CLIENT_SECRET"),
									ValueFrom: &corev1.EnvVarSourceArgs{
										SecretKeyRef: &corev1.SecretKeySelectorArgs{
											Name: secret.Metadata.Name(),
											Key:  pulumi.String("GITHUB_CLIENT_SECRET"),
										},
									},
								},
							},
							// TODO: uncomment when using registry image
							// LivenessProbe: &corev1.ProbeArgs{
							// 	HttpGet: &corev1.HTTPGetActionArgs{
							// 		Path: pulumi.String("/v0/health"),
							// 		Port: pulumi.Int(8080),
							// 	},
							// 	InitialDelaySeconds: pulumi.Int(30),
							// 	TimeoutSeconds:      pulumi.Int(5),
							// },
							// ReadinessProbe: &corev1.ProbeArgs{
							// 	HttpGet: &corev1.HTTPGetActionArgs{
							// 		Path: pulumi.String("/v0/health"),
							// 		Port: pulumi.Int(8080),
							// 	},
							// 	InitialDelaySeconds: pulumi.Int(5),
							// 	TimeoutSeconds:      pulumi.Int(3),
							// },
						},
					},
				},
			},
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return nil, err
	}

	// Create Service
	service, err := corev1.NewService(ctx, "mcp-registry", &corev1.ServiceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("mcp-registry"),
			Namespace: pulumi.String("default"),
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
					Port: pulumi.Int(80),
					// TODO: Change to port 8080 once using registry image
					TargetPort: pulumi.Int(80),
					Name:       pulumi.String("http"),
				},
			},
			Type: pulumi.String("ClusterIP"),
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return nil, err
	}

	// Create Ingress
	ingress, err := networkingv1.NewIngress(ctx, "mcp-registry", &networkingv1.IngressArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("mcp-registry"),
			Namespace: pulumi.String("default"),
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
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return nil, err
	}

	ctx.Export("ingressHost", ingress.Spec.Rules().Index(pulumi.Int(0)).Host())

	return service, nil
}
