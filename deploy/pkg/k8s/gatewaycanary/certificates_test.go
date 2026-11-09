package gatewaycanary

import (
	"sync"
	"testing"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func TestDetachedCertificate(t *testing.T) {
	for _, test := range []struct {
		name string
		meta *metav1.ObjectMeta
		want bool
	}{
		{"missing metadata", nil, false},
		{"no owner", &metav1.ObjectMeta{}, true},
		{"shim owner", &metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{{Kind: "Ingress", Name: "mcp-registry"}}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := detachedCertificate(test.meta)
			if got != test.want || (err == nil) != test.want {
				t.Fatalf("got %v, %v; want %v", got, err, test.want)
			}
		})
	}
}

type certificateOwnershipMocks struct {
	mocks
	t                   *testing.T
	owned               bool
	mu                  sync.Mutex
	reads, certificates int
}

func (m *certificateOwnershipMocks) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	if a.TypeToken != "kubernetes:cert-manager.io/v1:Certificate" {
		return m.mocks.NewResource(a)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if a.ReadRPC != nil {
		m.reads++
		if m.owned {
			a.Inputs["metadata"].ObjectValue()["ownerReferences"] = resource.NewPropertyValue([]any{map[string]any{"kind": "Ingress", "name": "legacy", "uid": "legacy-uid", "apiVersion": "networking.k8s.io/v1"}})
		}
	} else {
		m.certificates++
		if _, found := a.Inputs["metadata"].ObjectValue()["ownerReferences"]; found {
			m.t.Error("standalone Certificate must omit the empty owner list")
		}
		if a.RegisterRPC == nil || !a.RegisterRPC.GetProtect() {
			m.t.Error("public Certificate lost deletion protection")
		}
	}
	return a.Name, a.Inputs, nil
}

func TestStandaloneCertificateNormalizationStillRejectsLiveOwners(t *testing.T) {
	for _, owned := range []bool{false, true} {
		m := &certificateOwnershipMocks{t: t, owned: owned}
		err := pulumi.RunErr(func(ctx *pulumi.Context) error {
			provider, err := kubernetes.NewProvider(ctx, "provider", &kubernetes.ProviderArgs{})
			if err != nil {
				return err
			}
			return DeployPublicCertificates(ctx, provider)
		}, pulumi.WithMocks("test", "prod", m), func(info *pulumi.RunInfo) { info.Config = productionConfig("standalone") })
		if (err != nil) != owned {
			t.Fatalf("owned=%v: unexpected preview outcome: %v", owned, err)
		}
		if m.reads != 2 || (!owned && m.certificates != 2) {
			t.Fatalf("public Certificate reads/registrations missing: %d/%d", m.reads, m.certificates)
		}
	}
}
