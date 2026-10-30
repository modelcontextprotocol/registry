package k8s

import (
	"encoding/hex"
	"fmt"
	"strings"

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
	infraOnly := c.GetBool("productionInfrastructureOnly")
	if infraOnly && (c.Get("environment") != "prod" || !c.GetBool("coordinatedCapacity") || !c.GetBool("isolatedDatabase")) {
		return fmt.Errorf("productionInfrastructureOnly requires the complete isolated production coordinatedCapacity profile")
	}
	if !c.GetBool("coordinatedCapacity") {
		return nil
	}
	if !c.GetBool("burstAutoscaling") || c.Get("provider") != "gcp" || c.Get("nodeMachineType") != "e2-standard-4" {
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
		image, digest, pinned := strings.Cut(c.Get("stagingImage"), "@sha256:")
		_, digestErr := hex.DecodeString(digest)
		if !infraOnly && (!pinned || image == "" || len(digest) != 64 || digestErr != nil) {
			return fmt.Errorf("coordinatedCapacity with isolatedDatabase requires a digest-pinned stagingImage")
		}
	}
	appCPU := coordinatedAppCPU
	// Keep staging's matched profile fixed. Production can reserve more CPU
	// for its two app replicas without increasing nodes or pool connections.
	// Retain 800m as the rollback setting; larger requests need a new fit review.
	if infraOnly && c.Get("registryCPURequest") == "1200m" {
		appCPU = "1200m"
	}
	for key, expected := range map[string]string{"registryCPURequest": appCPU, "postgresCPURequest": dbCPU, "postgresCPULimit": "3", "nodeMinCount": "3", "nodeMaxCount": "4"} {
		if c.Get(key) != expected {
			return fmt.Errorf("coordinatedCapacity requires %s=%s", key, expected)
		}
	}
	return nil
}

func registryAutoscalerSpec() *autoscalingv2.HorizontalPodAutoscalerSpecArgs {
	return &autoscalingv2.HorizontalPodAutoscalerSpecArgs{
		MinReplicas: pulumi.Int(coordinatedIdleReplicas), MaxReplicas: pulumi.Int(4),
		ScaleTargetRef: &autoscalingv2.CrossVersionObjectReferenceArgs{ApiVersion: pulumi.String("apps/v1"), Kind: pulumi.String("Deployment"), Name: pulumi.String("mcp-registry")},
		Metrics: autoscalingv2.MetricSpecArray{&autoscalingv2.MetricSpecArgs{
			Type: pulumi.String("Resource"), Resource: &autoscalingv2.ResourceMetricSourceArgs{
				Name: pulumi.String("cpu"), Target: &autoscalingv2.MetricTargetArgs{Type: pulumi.String("Utilization"), AverageUtilization: pulumi.Int(70)},
			},
		}},
		Behavior: &autoscalingv2.HorizontalPodAutoscalerBehaviorArgs{
			ScaleUp:   &autoscalingv2.HPAScalingRulesArgs{StabilizationWindowSeconds: pulumi.Int(0), Policies: autoscalingv2.HPAScalingPolicyArray{&autoscalingv2.HPAScalingPolicyArgs{Type: pulumi.String("Pods"), Value: pulumi.Int(2), PeriodSeconds: pulumi.Int(60)}}},
			ScaleDown: &autoscalingv2.HPAScalingRulesArgs{StabilizationWindowSeconds: pulumi.Int(600), Policies: autoscalingv2.HPAScalingPolicyArray{&autoscalingv2.HPAScalingPolicyArgs{Type: pulumi.String("Pods"), Value: pulumi.Int(1), PeriodSeconds: pulumi.Int(120)}}},
		},
	}
}

func deployRegistryAutoscaler(ctx *pulumi.Context, provider pulumi.ProviderResource) error {
	_, err := autoscalingv2.NewHorizontalPodAutoscaler(ctx, "mcp-registry-capacity", &autoscalingv2.HorizontalPodAutoscalerArgs{
		Metadata: &metav1.ObjectMetaArgs{Name: pulumi.String("mcp-registry"), Namespace: pulumi.String("default")},
		Spec:     registryAutoscalerSpec(),
	}, pulumi.Provider(provider))
	return err
}

// Production's older binary has a fixed 60-connection pool. Keep two replicas
// until an application release explicitly enables the bounded-pool profile.
func registryCapacitySettings(ctx *pulumi.Context) (replicas int, autoscale bool) {
	c := config.New(ctx, "mcp-registry")
	if c.GetBool("coordinatedCapacity") && !c.GetBool("productionInfrastructureOnly") {
		return coordinatedIdleReplicas, true
	}
	return 2, false
}
