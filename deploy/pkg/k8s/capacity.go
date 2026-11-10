package k8s

import (
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/k8s/gatewaycanary"
	autoscalingv2 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/autoscaling/v2"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

const (
	coordinatedEnvironment  = "staging"
	coordinatedAppCPU       = "800m"
	coordinatedIdleReplicas = 3
	coordinatedDBCPU        = "1500m"
)

// ValidateCapacityConfig runs before creating resources. This experimental
// profile is opt-in and must use the complete node/ingress/DB budget.
func ValidateCapacityConfig(ctx *pulumi.Context) error {
	c := config.New(ctx, "mcp-registry")
	if c.GetBool("isolatedDatabase") && !c.GetBool("coordinatedCapacity") {
		return fmt.Errorf("isolatedDatabase requires coordinatedCapacity")
	}
	idle := c.GetBool("stagingIdleCapacity")
	if idle && (c.Get("environment") != "staging" || !c.GetBool("coordinatedCapacity") || !c.GetBool("isolatedDatabase")) {
		return fmt.Errorf("stagingIdleCapacity requires the isolated staging capacity profile")
	}
	small := c.Get("stagingSmallCapacity")
	if small != "" && ((small != "prepare" && small != "enabled") || !idle) {
		return fmt.Errorf("stagingSmallCapacity requires prepare or enabled on isolated idle staging")
	}
	infraOnly := c.GetBool("productionInfrastructureOnly")
	phase := c.Get("productionAppScaling")
	ingressScaling := c.GetBool("productionIngressScaling")
	burstHeadroom := c.GetBool("productionBurstHeadroom")
	syncCapacity := c.Get("productionSyncCapacity")
	if syncCapacity != "" {
		if (syncCapacity != "prepare" && syncCapacity != "enabled") || phase != "enabled" || !burstHeadroom || !ingressScaling || !infraOnly || c.Get("environment") != "prod" {
			return fmt.Errorf("productionSyncCapacity requires prepare or enabled on the production burst-headroom profile")
		}
	}
	if burstHeadroom && !ingressScaling {
		return fmt.Errorf("productionBurstHeadroom requires productionIngressScaling")
	}
	if ingressScaling && (!infraOnly || c.Get("environment") != "prod" || phase != "enabled") {
		return fmt.Errorf("productionIngressScaling requires enabled isolated production app scaling")
	}
	if phase != "" && phase != "off" {
		if (phase != "prepare" && phase != "enabled") || !infraOnly || c.Get("environment") != "prod" {
			return fmt.Errorf("productionAppScaling requires prepare or enabled on the isolated production profile")
		}
		version := "v" + c.Get("imageTag")
		if !semver.IsValid(version) || semver.Canonical(version) != version || semver.Prerelease(version) != "" || semver.Compare(version, "v1.8.7") < 0 {
			return fmt.Errorf("productionAppScaling requires a stable imageTag >= 1.8.7 with configurable connection pools")
		}
	}
	if infraOnly && (c.Get("environment") != "prod" || !c.GetBool("coordinatedCapacity") || !c.GetBool("isolatedDatabase")) {
		return fmt.Errorf("productionInfrastructureOnly requires the complete isolated production coordinatedCapacity profile")
	}
	if !c.GetBool("coordinatedCapacity") {
		return nil
	}
	machineType := "e2-standard-4"
	if small == "enabled" {
		machineType = "e2-standard-2"
	}
	if !c.GetBool("burstAutoscaling") || c.Get("provider") != "gcp" || c.Get("nodeMachineType") != machineType {
		return fmt.Errorf("coordinatedCapacity requires GCP e2-standard-4 nodes and burstAutoscaling")
	}
	if c.Get("environment") != coordinatedEnvironment && !infraOnly {
		return fmt.Errorf("coordinatedCapacity is staging-only until burst and recovery validation passes")
	}
	if infraOnly && (c.Get("imageTag") == "" || c.Get("imageTag") == "main" || c.Get("imageTag") == "latest") {
		return fmt.Errorf("productionInfrastructureOnly requires an explicit production imageTag")
	}
	dbCPU := coordinatedDBCPU
	if c.GetBool("isolatedDatabase") {
		dbCPU = "3"
		staging := stagingImage(c)
		image, digest, pinned := strings.Cut(staging, "@sha256:")
		_, digestErr := hex.DecodeString(digest)
		if !infraOnly && staging != "" && (!pinned || image == "" || len(digest) != 64 || digestErr != nil) {
			return fmt.Errorf("explicit stagingImage or REGISTRY_STAGING_IMAGE must be digest-pinned")
		}
	}
	appCPU, nodeMin, nodeMax := coordinatedAppCPU, "3", "4"
	if idle {
		nodeMin = "2"
	}
	if small != "" {
		appCPU, dbCPU = "100m", "250m"
	}
	if phase == "prepare" || phase == "enabled" {
		appCPU, nodeMax = "1200m", "5"
	}
	if ingressScaling {
		nodeMax = "6"
		if burstHeadroom {
			nodeMax = "8"
		}
	}
	if gatewaycanary.Compact(ctx) {
		if err := gatewaycanary.Validate(ctx); err != nil {
			return err
		}
		nodeMax = "2"
	}
	for key, expected := range map[string]string{"registryCPURequest": appCPU, "postgresCPURequest": dbCPU, "postgresCPULimit": "3", "nodeMinCount": nodeMin, "nodeMaxCount": nodeMax} {
		if c.Get(key) != expected {
			return fmt.Errorf("coordinatedCapacity requires %s=%s", key, expected)
		}
	}

	return nil
}

func registryAutoscalerSpec(minReplicas int, syncCapacity bool) *autoscalingv2.HorizontalPodAutoscalerSpecArgs {
	maximum, target, scaleUpSeconds := 4, 70, 60
	if syncCapacity {
		maximum, target, scaleUpSeconds = 6, 40, 30
	}
	return &autoscalingv2.HorizontalPodAutoscalerSpecArgs{
		MinReplicas: pulumi.Int(minReplicas), MaxReplicas: pulumi.Int(maximum),
		ScaleTargetRef: &autoscalingv2.CrossVersionObjectReferenceArgs{ApiVersion: pulumi.String("apps/v1"), Kind: pulumi.String("Deployment"), Name: pulumi.String("mcp-registry")},
		Metrics: autoscalingv2.MetricSpecArray{&autoscalingv2.MetricSpecArgs{
			Type: pulumi.String("Resource"), Resource: &autoscalingv2.ResourceMetricSourceArgs{
				Name: pulumi.String("cpu"), Target: &autoscalingv2.MetricTargetArgs{Type: pulumi.String("Utilization"), AverageUtilization: pulumi.Int(target)},
			},
		}},
		Behavior: &autoscalingv2.HorizontalPodAutoscalerBehaviorArgs{
			ScaleUp:   &autoscalingv2.HPAScalingRulesArgs{StabilizationWindowSeconds: pulumi.Int(0), Policies: autoscalingv2.HPAScalingPolicyArray{&autoscalingv2.HPAScalingPolicyArgs{Type: pulumi.String("Pods"), Value: pulumi.Int(2), PeriodSeconds: pulumi.Int(scaleUpSeconds)}}},
			ScaleDown: &autoscalingv2.HPAScalingRulesArgs{StabilizationWindowSeconds: pulumi.Int(600), Policies: autoscalingv2.HPAScalingPolicyArray{&autoscalingv2.HPAScalingPolicyArgs{Type: pulumi.String("Pods"), Value: pulumi.Int(1), PeriodSeconds: pulumi.Int(120)}}},
		},
	}
}

// Only the idle staging transition takes ownership of previously patched HPA fields.
func stagingAutoscalerAnnotations(ctx *pulumi.Context) pulumi.StringMap {
	if config.New(ctx, "mcp-registry").GetBool("stagingIdleCapacity") {
		return pulumi.StringMap{"pulumi.com/patchForce": pulumi.String("true")}
	}
	return nil
}

func deployRegistryAutoscaler(ctx *pulumi.Context, provider pulumi.ProviderResource) error {
	minimum := coordinatedMinimum(ctx)
	if config.New(ctx, "mcp-registry").Get("productionAppScaling") == "enabled" {
		minimum = 2
	}
	spec := registryAutoscalerSpec(minimum, config.New(ctx, "mcp-registry").Get("productionSyncCapacity") == "enabled")
	if gatewaycanary.Compact(ctx) {
		spec.MaxReplicas = pulumi.Int(1)
	}
	_, err := autoscalingv2.NewHorizontalPodAutoscaler(ctx, "mcp-registry-capacity", &autoscalingv2.HorizontalPodAutoscalerArgs{
		Metadata: &metav1.ObjectMetaArgs{Name: pulumi.String("mcp-registry"), Namespace: pulumi.String("default"), Annotations: stagingAutoscalerAnnotations(ctx)},
		Spec:     spec,
	}, pulumi.Provider(provider))
	return err
}

// Preparation keeps two replicas while the smaller connection pools roll out.
func registryCapacitySettings(ctx *pulumi.Context) (replicas int, autoscale bool) {
	c := config.New(ctx, "mcp-registry")
	if c.Get("productionAppScaling") == "enabled" {
		return 2, true
	}
	if c.GetBool("coordinatedCapacity") && !c.GetBool("productionInfrastructureOnly") {
		return coordinatedMinimum(ctx), true
	}
	return 2, false
}

// Idle staging retains autoscaling headroom but does not reserve test replicas.
func coordinatedMinimum(ctx *pulumi.Context) int {
	if config.New(ctx, "mcp-registry").GetBool("stagingIdleCapacity") {
		return 1
	}
	return coordinatedIdleReplicas
}
