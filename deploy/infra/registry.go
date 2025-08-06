package main

import (
	"fmt"

	appsv1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apps/v1"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	networkingv1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/networking/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// deployMCPRegistry deploys the MCP Registry application to the Kubernetes cluster
func deployMCPRegistry(ctx *pulumi.Context, cluster *ClusterInfo, environment string) error {
	conf := config.New(ctx, "mcp-registry")
	
	// Configuration values
	appName := "mcp-registry"
	namespace := environment
	
	// Hardcoded defaults based on environment
	replicas := 2
	if environment == "prod" {
		replicas = 2
	}

	// MongoDB connection - using the Kubernetes service DNS
	mongoDBURL := fmt.Sprintf("mongodb://mongodb.%s.svc.cluster.local:27017", namespace)

	// Create ConfigMap for non-sensitive configuration
	configMap, err := corev1.NewConfigMap(ctx, fmt.Sprintf("%s-config", appName), &corev1.ConfigMapArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(fmt.Sprintf("%s-config", appName)),
			Namespace: pulumi.String(namespace),
			Labels: pulumi.StringMap{
				"app":         pulumi.String(appName),
				"environment": pulumi.String(environment),
			},
		},
		Data: pulumi.StringMap{
			"MCP_REGISTRY_APP_VERSION":     pulumi.String(fmt.Sprintf("registry-%s", environment)),
			"MCP_REGISTRY_DATABASE_TYPE":   pulumi.String("mongodb"),
			"MCP_REGISTRY_COLLECTION_NAME": pulumi.String("servers_v2"),
			"MCP_REGISTRY_DATABASE_NAME":   pulumi.String("mcp-registry"),
			"MCP_REGISTRY_LOG_LEVEL":       pulumi.String("info"),
			"MCP_REGISTRY_SEED_IMPORT":     pulumi.String("true"),
			"MCP_REGISTRY_SERVER_ADDRESS":  pulumi.String(":8080"),
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	// Create Secret for sensitive configuration
	secret, err := corev1.NewSecret(ctx, fmt.Sprintf("%s-secrets", appName), &corev1.SecretArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(fmt.Sprintf("%s-secrets", appName)),
			Namespace: pulumi.String(namespace),
			Labels: pulumi.StringMap{
				"app":         pulumi.String(appName),
				"environment": pulumi.String(environment),
			},
		},
		StringData: pulumi.StringMap{
			"MCP_REGISTRY_DATABASE_URL":          pulumi.String(mongoDBURL),
			"MCP_REGISTRY_GITHUB_CLIENT_ID":      pulumi.String(conf.Get("githubClientId")),
			"MCP_REGISTRY_GITHUB_CLIENT_SECRET":  pulumi.String(conf.Get("githubClientSecret")),
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	// Create Deployment
	deployment, err := appsv1.NewDeployment(ctx, fmt.Sprintf("%s-deployment", appName), &appsv1.DeploymentArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(appName),
			Namespace: pulumi.String(namespace),
			Labels: pulumi.StringMap{
				"app":         pulumi.String(appName),
				"environment": pulumi.String(environment),
			},
		},
		Spec: &appsv1.DeploymentSpecArgs{
			Replicas: pulumi.Int(replicas),
			Selector: &metav1.LabelSelectorArgs{
				MatchLabels: pulumi.StringMap{
					"app": pulumi.String(appName),
				},
			},
			Template: &corev1.PodTemplateSpecArgs{
				Metadata: &metav1.ObjectMetaArgs{
					Labels: pulumi.StringMap{
						"app":         pulumi.String(appName),
						"environment": pulumi.String(environment),
					},
				},
				Spec: &corev1.PodSpecArgs{
					Containers: corev1.ContainerArray{
						&corev1.ContainerArgs{
							Name:  pulumi.String(appName),
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
										Name: configMap.Metadata.Name(),
									},
								},
								&corev1.EnvFromSourceArgs{
									SecretRef: &corev1.SecretEnvSourceArgs{
										Name: secret.Metadata.Name(),
									},
								},
							},
							Resources: &corev1.ResourceRequirementsArgs{
								Requests: pulumi.StringMap{
									"cpu":    pulumi.String("100m"),
									"memory": pulumi.String("128Mi"),
								},
								Limits: pulumi.StringMap{
									"cpu":    pulumi.String("500m"),
									"memory": pulumi.String("512Mi"),
								},
							},
							LivenessProbe: &corev1.ProbeArgs{
								HttpGet: &corev1.HTTPGetActionArgs{
									Path: pulumi.String("/v0/health"),
									Port: pulumi.Int(8080),
								},
								InitialDelaySeconds: pulumi.Int(5),
								PeriodSeconds:       pulumi.Int(10),
							},
							ReadinessProbe: &corev1.ProbeArgs{
								HttpGet: &corev1.HTTPGetActionArgs{
									Path: pulumi.String("/v0/health"),
									Port: pulumi.Int(8080),
								},
								InitialDelaySeconds: pulumi.Int(5),
								PeriodSeconds:       pulumi.Int(10),
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
	service, err := corev1.NewService(ctx, fmt.Sprintf("%s-service", appName), &corev1.ServiceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(appName),
			Namespace: pulumi.String(namespace),
			Labels: pulumi.StringMap{
				"app":         pulumi.String(appName),
				"environment": pulumi.String(environment),
			},
		},
		Spec: &corev1.ServiceSpecArgs{
			Selector: pulumi.StringMap{
				"app": pulumi.String(appName),
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

	// Create Ingress (always enabled)
	ingressHost := fmt.Sprintf("mcp-registry-%s.example.com", environment)

		_, err = networkingv1.NewIngress(ctx, fmt.Sprintf("%s-ingress", appName), &networkingv1.IngressArgs{
			Metadata: &metav1.ObjectMetaArgs{
				Name:      pulumi.String(appName),
				Namespace: pulumi.String(namespace),
				Labels: pulumi.StringMap{
					"app":         pulumi.String(appName),
					"environment": pulumi.String(environment),
				},
				Annotations: pulumi.StringMap{
					"nginx.ingress.kubernetes.io/rewrite-target": pulumi.String("/"),
					"cert-manager.io/cluster-issuer":             pulumi.String("letsencrypt-prod"),
				},
			},
			Spec: &networkingv1.IngressSpecArgs{
				IngressClassName: pulumi.String("nginx"),
				Tls: networkingv1.IngressTLSArray{
					&networkingv1.IngressTLSArgs{
						Hosts: pulumi.StringArray{
							pulumi.String(ingressHost),
						},
						SecretName: pulumi.String(fmt.Sprintf("%s-tls", appName)),
					},
				},
				Rules: networkingv1.IngressRuleArray{
					&networkingv1.IngressRuleArgs{
						Host: pulumi.String(ingressHost),
						Http: &networkingv1.HTTPIngressRuleValueArgs{
							Paths: networkingv1.HTTPIngressPathArray{
								&networkingv1.HTTPIngressPathArgs{
									Path:     pulumi.String("/"),
									PathType: pulumi.String("Prefix"),
									Backend: &networkingv1.IngressBackendArgs{
										Service: &networkingv1.IngressServiceBackendArgs{
											Name: service.Metadata.Name(),
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
		})
	if err != nil {
		return err
	}

	// Add dependencies
	pulumi.DependsOn([]pulumi.Resource{deployment})

	return nil
}