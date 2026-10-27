package k8s //nolint:testpackage // Verify the unexported Pulumi child transformation.

import (
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type ownershipMonitor struct{ t *testing.T }

func (m ownershipMonitor) Call(a pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return a.Args, nil
}
func (m ownershipMonitor) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	if a.RegisterRPC != nil {
		switch a.TypeToken + "::" + a.Name {
		case "kubernetes:apps/v1:Deployment::ingress-nginx/ingress-nginx-controller":
			ignored := a.RegisterRPC.GetIgnoreChanges()
			if len(ignored) != 1 || ignored[0] != "spec.replicas" {
				m.t.Errorf("HPA ownership did not reach chart child: %v", ignored)
			}
		case "kubernetes:core/v1:ConfigMap::monitoring/victoria-metrics-agent-config", "kubernetes:core/v1:ConfigMap::ingress-nginx/ingress-nginx-controller":
			if !a.RegisterRPC.GetDeleteBeforeReplace() {
				m.t.Error("fixed-name ConfigMap would be created before deletion")
			}
		case "kubernetes:apps/v1:Deployment::unrelated-deployment":
			if len(a.RegisterRPC.GetIgnoreChanges()) != 0 {
				m.t.Error("unrelated replicas must remain managed")
			}
		}
	}
	return a.Name + "-id", a.Inputs, nil
}

func TestChartChildrenKeepControllerOwnership(t *testing.T) {
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		var parent pulumi.ResourceState
		if err := ctx.RegisterComponentResource("kubernetes:helm.sh/v3:Chart", "chart", &parent, pulumi.Transformations([]pulumi.ResourceTransformation{burstResourceTransform})); err != nil {
			return err
		}
		for _, r := range []struct{ typ, name string }{
			{"kubernetes:apps/v1:Deployment", "ingress-nginx/ingress-nginx-controller"},
			{"kubernetes:core/v1:ConfigMap", "monitoring/victoria-metrics-agent-config"},
			{"kubernetes:core/v1:ConfigMap", "ingress-nginx/ingress-nginx-controller"},
			{"kubernetes:apps/v1:Deployment", "unrelated-deployment"},
		} {
			var child pulumi.CustomResourceState
			if err := ctx.RegisterResource(r.typ, r.name, pulumi.Map{}, &child, pulumi.Parent(&parent)); err != nil {
				return err
			}
		}
		return nil
	}, pulumi.WithMocks("ownership-test", "staging", ownershipMonitor{t}))
	if err != nil {
		t.Fatal(err)
	}
}
