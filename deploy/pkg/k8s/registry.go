package k8s

import (
	"os/exec"
	"strings"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apiextensions"
	v1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apps/v1"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/helm/v3"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	networkingv1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/networking/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
)

// getGitCommitHash returns the current git commit hash
func getGitCommitHash() string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		// Fallback to a default value if git command fails
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

// DeployMCPRegistry deploys the MCP Registry to the Kubernetes cluster
func DeployMCPRegistry(ctx *pulumi.Context, cluster *providers.ProviderInfo, environment string, ingressNginx *helm.Chart, pgCluster *apiextensions.CustomResource) (*corev1.Service, error) {
	conf := config.New(ctx, "mcp-registry")
	githubClientId := conf.Require("githubClientId")

	// Determine Docker image tag based on environment
	imageTag := "main" // Default for staging
	if environment == "prod" {
		// Use explicitly configured image tag for production
		// This prevents automatic promotion and requires manual control of production releases
		imageTag = conf.Require("imageTag") // Explicit configuration required - no fallback
	}

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
			"GITHUB_CLIENT_SECRET": conf.RequireSecret("githubClientSecret"),
			"JWT_PRIVATE_KEY":      conf.RequireSecret("jwtPrivateKey"),
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
					Annotations: pulumi.StringMap{
						// Use git commit hash to trigger pod restarts when deploying new infra versions
						"registry.modelcontextprotocol.io/deployCommit": pulumi.String(getGitCommitHash()),
					},
				},
				Spec: &corev1.PodSpecArgs{
					Containers: corev1.ContainerArray{
						&corev1.ContainerArgs{
							Name:            pulumi.String("mcp-registry"),
							Image:           pulumi.Sprintf("ghcr.io/modelcontextprotocol/registry:%s", imageTag),
							ImagePullPolicy: pulumi.String("Always"),
							Ports: corev1.ContainerPortArray{
								&corev1.ContainerPortArgs{
									ContainerPort: pulumi.Int(8080),
									Name:          pulumi.String("http"),
								},
							},
							Env: corev1.EnvVarArray{
								&corev1.EnvVarArgs{
									Name: pulumi.String("MCP_REGISTRY_DATABASE_URL"),
									ValueFrom: &corev1.EnvVarSourceArgs{
										SecretKeyRef: &corev1.SecretKeySelectorArgs{
											Name: pgCluster.Metadata.Name().ApplyT(func(name *string) string {
												if name == nil {
													return "registry-pg-app"
												}
												return *name + "-app"
											}).(pulumi.StringOutput),
											Key: pulumi.String("uri"),
										},
									},
								},
								&corev1.EnvVarArgs{
									Name:  pulumi.String("MCP_REGISTRY_GITHUB_CLIENT_ID"),
									Value: pulumi.String(githubClientId),
								},
								&corev1.EnvVarArgs{
									Name: pulumi.String("MCP_REGISTRY_GITHUB_CLIENT_SECRET"),
									ValueFrom: &corev1.EnvVarSourceArgs{
										SecretKeyRef: &corev1.SecretKeySelectorArgs{
											Name: secret.Metadata.Name(),
											Key:  pulumi.String("GITHUB_CLIENT_SECRET"),
										},
									},
								},
								&corev1.EnvVarArgs{
									Name: pulumi.String("MCP_REGISTRY_JWT_PRIVATE_KEY"),
									ValueFrom: &corev1.EnvVarSourceArgs{
										SecretKeyRef: &corev1.SecretKeySelectorArgs{
											Name: secret.Metadata.Name(),
											Key:  pulumi.String("JWT_PRIVATE_KEY"),
										},
									},
								},
								// Google Cloud Identity OIDC for admin access
								&corev1.EnvVarArgs{
									Name:  pulumi.String("MCP_REGISTRY_OIDC_ENABLED"),
									Value: pulumi.String("true"),
								},
								&corev1.EnvVarArgs{
									Name:  pulumi.String("MCP_REGISTRY_OIDC_ISSUER"),
									Value: pulumi.String("https://accounts.google.com"),
								},
								&corev1.EnvVarArgs{
									Name:  pulumi.String("MCP_REGISTRY_OIDC_CLIENT_ID"),
									Value: pulumi.String("32555940559.apps.googleusercontent.com"),
								},
								&corev1.EnvVarArgs{
									Name:  pulumi.String("MCP_REGISTRY_OIDC_EXTRA_CLAIMS"),
									Value: pulumi.String(`[{"hd":"modelcontextprotocol.io"}]`),
								},
								&corev1.EnvVarArgs{
									Name:  pulumi.String("MCP_REGISTRY_OIDC_EDIT_PERMISSIONS"),
									Value: pulumi.String("*"),
								},
								&corev1.EnvVarArgs{
									Name:  pulumi.String("MCP_REGISTRY_OIDC_PUBLISH_PERMISSIONS"),
									Value: pulumi.String("*"),
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
									"memory": pulumi.String("128Mi"),
									"cpu":    pulumi.String("100m"),
								},
								Limits: pulumi.StringMap{
									"memory": pulumi.String("256Mi"),
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
					Port:       pulumi.Int(80),
					TargetPort: pulumi.Int(8080),
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
	hosts := []string{
		environment + ".registry.modelcontextprotocol.io",
	}

	// Add root domain for prod environment
	if environment == "prod" {
		hosts = append(hosts, "registry.modelcontextprotocol.io")
	}

	ingress, err := networkingv1.NewIngress(ctx, "mcp-registry", &networkingv1.IngressArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("mcp-registry"),
			Namespace: pulumi.String("default"),
			Labels: pulumi.StringMap{
				"app":         pulumi.String("mcp-registry"),
				"environment": pulumi.String(environment),
			},
			Annotations: pulumi.StringMap{
				"cert-manager.io/cluster-issuer": pulumi.String("letsencrypt-prod"),
				"kubernetes.io/ingress.class":    pulumi.String("nginx"),
			},
		},
		Spec: &networkingv1.IngressSpecArgs{
			Tls: networkingv1.IngressTLSArray{
				&networkingv1.IngressTLSArgs{
					Hosts:      pulumi.ToStringArray(hosts),
					SecretName: pulumi.Sprintf("mcp-registry-%s-tls", environment),
				},
			},
			Rules: pulumi.ToStringArray(hosts).ToStringArrayOutput().ApplyT(func(hosts []string) networkingv1.IngressRuleArray {
				rules := make(networkingv1.IngressRuleArray, 0, len(hosts))
				for _, host := range hosts {
					rules = append(rules, &networkingv1.IngressRuleArgs{
						Host: pulumi.String(host),
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
					})
				}
				return rules
			}).(networkingv1.IngressRuleArrayOutput),
		},
	}, pulumi.Provider(cluster.Provider), pulumi.DependsOnInputs(ingressNginx.Ready))
	if err != nil {
		return nil, err
	}

	ctx.Export("ingressHosts", ingress.Spec.Rules().ApplyT(func(rules []networkingv1.IngressRule) []string {
		hosts := make([]string, 0, len(rules))
		for _, rule := range rules {
			hosts = append(hosts, *rule.Host)
		}
		return hosts
	}))

	return service, nil
}
