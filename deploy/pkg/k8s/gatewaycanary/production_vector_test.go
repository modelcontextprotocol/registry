package gatewaycanary

import (
	"testing"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type vectorReservationMocks struct {
	mocks
	registered chan pulumi.MockResourceArgs
}

func (m *vectorReservationMocks) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.registered <- a
	return m.mocks.NewResource(a)
}

func TestProductionVectorReservationPreservesMonitoring(t *testing.T) {
	m := &vectorReservationMocks{registered: make(chan pulumi.MockResourceArgs, 8)}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		p, err := kubernetes.NewProvider(ctx, "provider", &kubernetes.ProviderArgs{})
		if err != nil {
			return err
		}
		return vector(ctx, p, p)
	}, pulumi.WithMocks("test", "prod", m), func(i *pulumi.RunInfo) { i.Config = productionConfig("standalone") })
	if err != nil {
		t.Fatal(err)
	}
	close(m.registered)
	seenConfig, seenCollector := false, false
	for a := range m.registered {
		if a.TypeToken == "kubernetes:core/v1:ConfigMap" {
			want, err := vectorConfiguration(true, true)
			if err != nil || a.Inputs["data"].ObjectValue()["vector.json"].StringValue() != string(want) {
				t.Fatal("production monitoring configuration changed")
			}
			seenConfig = true
		}
		if a.TypeToken != "kubernetes:apps/v1:DaemonSet" {
			continue
		}
		seenCollector = true
		pod := a.Inputs["spec"].ObjectValue()["template"].ObjectValue()["spec"].ObjectValue()
		if pod["nodeSelector"].ObjectValue()["registry-role"].StringValue() != "worker" || len(pod["tolerations"].ArrayValue()) != 0 {
			t.Fatal("collector must remain off the isolated database node")
		}
		r := pod["containers"].ArrayValue()[0].ObjectValue()["resources"].ObjectValue()
		requests, limits := r["requests"].ObjectValue(), r["limits"].ObjectValue()
		if requests["cpu"].StringValue() != "250m" || requests["memory"].StringValue() != "128Mi" || limits["memory"].StringValue() != "512Mi" {
			t.Fatal("unexpected production collector budget")
		}
		if _, capped := limits["cpu"]; capped {
			t.Fatal("collector must remain able to burst above its CPU reservation")
		}
	}
	if !seenConfig || !seenCollector {
		t.Fatal("monitoring config or collector missing")
	}
}
