package k8s //nolint:testpackage // Verify rendered production resources and transition guards.

import (
	"testing"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apiextensions"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/helm/v3"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func productionScalingConfig(phase string) map[string]string {
	values := map[string]string{"productionAppScaling": phase, "productionInfrastructureOnly": "true", "coordinatedCapacity": "true", "isolatedDatabase": "true", "burstAutoscaling": "true", "provider": "gcp", "environment": "prod", "nodeMachineType": "e2-standard-4", "registryCPURequest": "1200m", "postgresCPURequest": "3", "postgresCPULimit": "3", "nodeMinCount": "3", "nodeMaxCount": "5", "imageTag": "1.8.7", "preparedListingJSON": "true", "registryRolloutCommit": "tested-app", "githubClientId": "fixture", "githubClientSecret": "fixture", "jwtPrivateKey": "fixture"}
	cfg := map[string]string{}
	for k, v := range values {
		cfg["mcp-registry:"+k] = v
	}
	return cfg
}

func TestProductionScalingRejectsIncompleteProfiles(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"productionAppScaling", "true"}, {"environment", "staging"},
		{"productionInfrastructureOnly", "false"}, {"coordinatedCapacity", "false"},
		{"isolatedDatabase", "false"}, {"nodeMaxCount", "4"}, {"nodeMaxCount", "6"},
		{"registryCPURequest", "800m"}, {"imageTag", "1.8.6"}, {"imageTag", "latest"}, {"imageTag", "1.8.8-rc.1"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			cfg := productionScalingConfig("enabled")
			cfg["mcp-registry:"+tc.key] = tc.value
			err := pulumi.RunErr(ValidateCapacityConfig, pulumi.WithMocks("scaling-test", "prod", &capacityMonitor{t: t}), func(info *pulumi.RunInfo) { info.Config = cfg })
			if err == nil {
				t.Fatal("unsafe or incomplete profile accepted")
			}
		})
	}
}

type productionScalingMonitor struct {
	t                       *testing.T
	phase                   string
	syncCapacity            string
	packed                  bool
	deploymentSeen, hpaSeen bool
}

func (m *productionScalingMonitor) Call(a pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return a.Args, nil
}
func (m *productionScalingMonitor) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	if a.TypeToken == "kubernetes:apps/v1:Deployment" && a.Name == "mcp-registry" {
		m.deploymentSeen = true
		spec := a.Inputs["spec"].ObjectValue()
		if spec["replicas"].NumberValue() != 2 {
			m.t.Error("production must start with two replicas")
		}
		rolling := spec["strategy"].ObjectValue()["rollingUpdate"].ObjectValue()
		if rolling["maxSurge"].NumberValue() != 0 || rolling["maxUnavailable"].NumberValue() != 1 {
			m.t.Error("unexpected rollout overlap")
		}
		pod := spec["template"].ObjectValue()["spec"].ObjectValue()
		if spec["minReadySeconds"].NumberValue() <= pod["terminationGracePeriodSeconds"].NumberValue() {
			m.t.Error("replacement availability must wait beyond normal termination")
		}
		anti := pod["affinity"].ObjectValue()["podAntiAffinity"].ObjectValue()
		if m.packed {
			if anti["requiredDuringSchedulingIgnoredDuringExecution"].HasValue() || len(anti["preferredDuringSchedulingIgnoredDuringExecution"].ArrayValue()) != 1 {
				m.t.Error("packed apps must permit consolidation")
			}
			spread := pod["topologySpreadConstraints"].ArrayValue()[0].ObjectValue()
			if spread["whenUnsatisfiable"].StringValue() != "DoNotSchedule" || spread["matchLabelKeys"].ArrayValue()[0].StringValue() != "pod-template-hash" || spread["minDomains"].HasValue() {
				m.t.Error("packed apps must spread revisions without a worker-count floor")
			}
		} else {
			required := anti["requiredDuringSchedulingIgnoredDuringExecution"].ArrayValue()
			if len(required) != 1 || required[0].ObjectValue()["topologyKey"].StringValue() != "kubernetes.io/hostname" || required[0].ObjectValue()["labelSelector"].ObjectValue()["matchLabels"].ObjectValue()["app"].StringValue() != "mcp-registry" {
				m.t.Error("legacy apps must require separate workers across revisions")
			}
		}
		container := pod["containers"].ArrayValue()[0].ObjectValue()
		expectedImage, expectedPool := "ghcr.io/modelcontextprotocol/registry:1.8.7", "30"
		if m.syncCapacity != "" {
			expectedImage, expectedPool = "ghcr.io/modelcontextprotocol/registry:1.8.9", "20"
		}
		if container["image"].StringValue() != expectedImage {
			m.t.Error("production must retain its explicit release image")
		}
		env := map[string]string{}
		for _, e := range container["env"].ArrayValue() {
			v := e.ObjectValue()
			if v["value"].IsString() {
				env[v["name"].StringValue()] = v["value"].StringValue()
			}
		}
		if env["MCP_REGISTRY_DATABASE_MAX_CONNECTIONS"] != expectedPool || env["MCP_REGISTRY_DATABASE_MIN_CONNECTIONS"] != "5" || env["MCP_REGISTRY_PREPARED_LISTING_JSON"] != "true" {
			m.t.Error("missing bounded pools or changed prepared reads")
		}
		if container["resources"].ObjectValue()["requests"].ObjectValue()["cpu"].StringValue() != "1200m" {
			m.t.Error("incorrect scheduling reservation")
		}
		ignores := a.RegisterRPC.GetIgnoreChanges()
		if (len(ignores) == 1 && ignores[0] == "spec.replicas") != (m.phase == "enabled") {
			m.t.Error("HPA replica ownership incorrect")
		}
	}
	if a.TypeToken == "kubernetes:autoscaling/v2:HorizontalPodAutoscaler" {
		m.hpaSeen = true
		spec := a.Inputs["spec"].ObjectValue()
		maximum, target, period := float64(4), float64(70), float64(60)
		if m.syncCapacity == "enabled" {
			maximum, target, period = 6, 40, 30
		}
		if spec["minReplicas"].NumberValue() != 2 || spec["maxReplicas"].NumberValue() != maximum {
			m.t.Error("incorrect production HPA bounds")
		}
		metric := spec["metrics"].ArrayValue()[0].ObjectValue()["resource"].ObjectValue()
		if metric["target"].ObjectValue()["averageUtilization"].NumberValue() != target {
			m.t.Error("unexpected CPU target")
		}
		if spec["behavior"].ObjectValue()["scaleUp"].ObjectValue()["policies"].ArrayValue()[0].ObjectValue()["periodSeconds"].NumberValue() != period {
			m.t.Error("unexpected scale-up rate")
		}
	}
	return a.Name + "-id", a.Inputs, nil
}

func TestProductionScalingRendersPreparationAndActivation(t *testing.T) {
	t.Setenv("REGISTRY_STAGING_IMAGE", "must-not-affect-production")
	for _, profile := range []struct {
		phase, sync string
		packed      bool
	}{{"prepare", "", false}, {"enabled", "", false}, {"enabled", "prepare", false}, {"enabled", "enabled", false}, {"enabled", "enabled", true}} {
		phase := profile.phase
		t.Run(phase+"/sync="+profile.sync, func(t *testing.T) {
			m := &productionScalingMonitor{t: t, phase: phase, syncCapacity: profile.sync, packed: profile.packed}
			err := pulumi.RunErr(func(ctx *pulumi.Context) error {
				if err := ValidateCapacityConfig(ctx); err != nil {
					return err
				}
				provider, err := kubernetes.NewProvider(ctx, "provider", &kubernetes.ProviderArgs{})
				if err != nil {
					return err
				}
				db, err := apiextensions.NewCustomResource(ctx, "db", &apiextensions.CustomResourceArgs{ApiVersion: pulumi.String("postgresql.cnpg.io/v1"), Kind: pulumi.String("Cluster"), Metadata: &metav1.ObjectMetaArgs{Name: pulumi.String("registry-pg")}})
				if err != nil {
					return err
				}
				chart := &helm.Chart{Ready: pulumi.ResourceArray{}.ToResourceArrayOutput()}
				_, err = DeployMCPRegistry(ctx, &providers.ProviderInfo{Provider: provider}, "prod", chart, db)
				return err
			}, pulumi.WithMocks("scaling-test", "prod", m), func(info *pulumi.RunInfo) {
				info.Config = productionScalingConfig(phase)
				if profile.packed {
					info.Config["mcp-registry:productionPackedPlacement"] = "true"
					info.Config["mcp-registry:gatewayProductionPhase"] = "standalone"
				}
				if profile.sync != "" {
					for k, v := range map[string]string{"productionSyncCapacity": profile.sync, "productionIngressScaling": "true", "productionBurstHeadroom": "true", "nodeMaxCount": "8", "imageTag": "1.8.9"} {
						info.Config["mcp-registry:"+k] = v
					}
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			if !m.deploymentSeen || m.hpaSeen != (phase == "enabled") {
				t.Fatal("incorrect deployment/HPA resources")
			}
		})
	}
}

func TestProductionIngressScalingRequiresCompleteProfile(t *testing.T) {
	for _, tc := range []struct {
		key, value string
		invalid    bool
	}{
		{"productionIngressScaling", "true", false},
		{"nodeMaxCount", "5", true},
		{"nodeMinCount", "4", true},
		{"productionAppScaling", "prepare", true},
		{"environment", "staging", true},
		{"isolatedDatabase", "false", true},
		{"burstAutoscaling", "false", true},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			cfg := productionScalingConfig("enabled")
			cfg["mcp-registry:productionIngressScaling"] = "true"
			cfg["mcp-registry:nodeMaxCount"] = "6"
			cfg["mcp-registry:"+tc.key] = tc.value
			err := pulumi.RunErr(ValidateCapacityConfig, pulumi.WithMocks("scaling-test", "prod", &capacityMonitor{t: t}), func(info *pulumi.RunInfo) { info.Config = cfg })
			if (err != nil) != tc.invalid {
				t.Fatalf("unexpected validation: %v", err)
			}
		})
	}
}

func TestProductionSyncCapacityRejectsIncompleteProfiles(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"productionSyncCapacity", "true"}, {"productionBurstHeadroom", "false"},
		{"productionIngressScaling", "false"}, {"productionAppScaling", "prepare"},
		{"environment", "staging"}, {"imageTag", "1.8.6"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			cfg := productionScalingConfig("enabled")
			for k, v := range map[string]string{"productionSyncCapacity": "enabled", "productionIngressScaling": "true", "productionBurstHeadroom": "true", "nodeMaxCount": "8", "imageTag": "1.8.9"} {
				cfg["mcp-registry:"+k] = v
			}
			cfg["mcp-registry:"+tc.key] = tc.value
			err := pulumi.RunErr(ValidateCapacityConfig, pulumi.WithMocks("scaling-test", "prod", &capacityMonitor{t: t}), func(info *pulumi.RunInfo) { info.Config = cfg })
			if err == nil {
				t.Fatal("incomplete sync capacity profile accepted")
			}
		})
	}
}

func TestPackedPlacementRequiresEnabledSyncAndProductionGateway(t *testing.T) {
	for _, phase := range []string{"", "prepare", "enabled"} {
		cfg := productionScalingConfig("enabled")
		cfg["mcp-registry:productionPackedPlacement"] = "true"
		cfg["mcp-registry:productionSyncCapacity"] = phase
		err := pulumi.RunErr(ValidateCapacityConfig, pulumi.WithMocks("scaling-test", "prod", &capacityMonitor{t: t}), func(info *pulumi.RunInfo) { info.Config = cfg })
		if err == nil {
			t.Fatal("packed placement accepted incomplete production profile")
		}
	}
}
