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

	// Deploy Grafana
	return deployGrafana(ctx, cluster, ns, environment, ingressNginx)
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

	// Create VictoriaMetrics datasource
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
	_, err = helm.NewChart(ctx, "grafana", helm.ChartArgs{
		Chart:   pulumi.String("grafana"),
		Version: pulumi.String("9.4.4"),
		FetchArgs: &helm.FetchArgs{
			Repo: pulumi.String("https://grafana.github.io/helm-charts"),
		},
		Namespace: ns.Metadata.Name().Elem(),
		Values: pulumi.Map{
			"extraConfigmapMounts": pulumi.Array{
				pulumi.Map{
					"name":      pulumi.String("grafana-datasources"),
					"mountPath": pulumi.String("/etc/grafana/provisioning/datasources"),
					"configMap": grafanaDataSourcesConfigMap.Metadata.Name(),
					"readOnly":  pulumi.Bool(true),
				},
			},
			"grafana.ini": pulumi.Map{
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
	grafanaHost := "grafana.registry." + environment + ".modelcontextprotocol.io"

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
