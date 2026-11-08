package gatewaycanary

import (
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type compactMocks struct {
	mocks
	t         *testing.T
	workloads atomic.Int32
}

func (m *compactMocks) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	if a.TypeToken == "kubernetes:apps/v1:Deployment" || a.TypeToken == "kubernetes:apps/v1:DaemonSet" {
		m.workloads.Add(1)
		pod := a.Inputs["spec"].ObjectValue()["template"].ObjectValue()["spec"].ObjectValue()
		if pod["nodeSelector"].ObjectValue()["registry-role"].StringValue() != "worker" || len(pod["tolerations"].ArrayValue()) != 0 {
			m.t.Error("controller/Vector still requires the removed Gateway pool")
		}
		cpu := "50m"
		if a.TypeToken == "kubernetes:apps/v1:DaemonSet" {
			cpu = "100m"
		}
		resources := pod["containers"].ArrayValue()[0].ObjectValue()["resources"].ObjectValue()
		if resources["requests"].ObjectValue()["cpu"].StringValue() != cpu || resources["requests"].ObjectValue()["memory"].StringValue() != "128Mi" || resources["limits"].ObjectValue()["memory"].StringValue() != "512Mi" {
			m.t.Error("wrong compact CPU or changed memory budget")
		}
	}
	return m.mocks.NewResource(a)
}

func TestCompactRegistersControllerAndVectorOnWorker(t *testing.T) {
	m := &compactMocks{t: t}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		p, err := kubernetes.NewProvider(ctx, "staging-provider", &kubernetes.ProviderArgs{})
		if err != nil {
			return err
		}
		return Deploy(ctx, p, pulumi.String("").ToStringOutput())
	}, pulumi.WithMocks("test", "staging", m), func(i *pulumi.RunInfo) {
		i.Config = map[string]string{"gcp:project": "mcp-registry-staging"}
		for k, v := range map[string]string{"gatewayStagingCompact": "true", "gatewayStagingStandalone": "true", "gatewayStagingPublicIP": "35.222.36.75", "environment": "staging", "provider": "gcp", "stagingIdleCapacity": "true", "stagingSmallCapacity": "enabled", "nodeMinCount": "2", "nodeMaxCount": "2"} {
			i.Config["mcp-registry:"+k] = v
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.workloads.Load() != 2 {
		t.Fatal("expected registered controller and Vector workloads")
	}
}

func TestCompactPreservesProxyFeatures(t *testing.T) {
	objects, err := handoffObjectsWithOptions(true, true, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range objects {
		if o["kind"] != "NginxProxy" {
			continue
		}
		spec := o["spec"].(map[string]any)
		logging, _ := json.Marshal(spec["logging"])
		d := spec["kubernetes"].(map[string]any)["deployment"].(map[string]any)
		patches, _ := json.Marshal(d["patches"])
		limits, _ := json.Marshal(d["container"].(map[string]any)["resources"].(map[string]any)["limits"])
		compactObject(o)
		a := d["autoscaling"].(map[string]any)
		if a["minReplicas"] != 1 || a["maxReplicas"] != 1 {
			t.Fatal("compact profile must not autoscale")
		}
		pod := d["pod"].(map[string]any)
		if pod["nodeSelector"].(map[string]any)["registry-role"] != "worker" || len(pod["tolerations"].([]any)) != 0 {
			t.Fatal("must share worker without tolerating database")
		}
		afterLogging, _ := json.Marshal(spec["logging"])
		afterPatches, _ := json.Marshal(d["patches"])
		afterLimits, _ := json.Marshal(d["container"].(map[string]any)["resources"].(map[string]any)["limits"])
		if string(logging) != string(afterLogging) || string(patches) != string(afterPatches) || string(limits) != string(afterLimits) {
			t.Fatal("compact placement changed logging, graceful shutdown or cache/memory limits")
		}
		if d["podDisruptionBudget"].(map[string]any)["minAvailable"] != 0 {
			t.Fatal("singleton must permit planned staging maintenance")
		}
		return
	}
	t.Fatal("public proxy not found")
}

func TestCompactRequiresCompleteStagingProfile(t *testing.T) {
	for _, tc := range []struct {
		key, value string
		valid      bool
	}{
		{"environment", "staging", true}, {"environment", "prod", false},
		{"gatewayStagingStandalone", "false", false}, {"stagingSmallCapacity", "prepare", false},
		{"nodeMaxCount", "4", false}, {"nodeMinCount", "1", false},
	} {
		cfg := map[string]string{"gcp:project": "mcp-registry-staging"}
		for k, v := range map[string]string{"gatewayStagingCompact": "true", "gatewayStagingStandalone": "true", "gatewayStagingPublicIP": "35.222.36.75", "environment": "staging", "provider": "gcp", "stagingIdleCapacity": "true", "stagingSmallCapacity": "enabled", "nodeMinCount": "2", "nodeMaxCount": "2"} {
			cfg["mcp-registry:"+k] = v
		}
		cfg["mcp-registry:"+tc.key] = tc.value
		err := pulumi.RunErr(Validate, pulumi.WithMocks("test", "staging", mocks{}), func(i *pulumi.RunInfo) { i.Config = cfg })
		if (err == nil) != tc.valid {
			t.Fatalf("%s=%s: %v", tc.key, tc.value, err)
		}
	}
}
