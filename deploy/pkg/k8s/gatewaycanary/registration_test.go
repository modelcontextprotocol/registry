package gatewaycanary

import (
	"fmt"
	"sync"
	"testing"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type uniqueRegistrationMocks struct {
	mocks
	mu     sync.Mutex
	counts map[string]int
}

func (m *uniqueRegistrationMocks) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	identity := a.TypeToken + "::" + a.Name
	m.counts[identity]++
	if m.counts[identity] != 1 {
		return "", nil, fmt.Errorf("duplicate resource registration: %s", identity)
	}
	return m.mocks.NewResource(a)
}

func TestDeployRegistersSharedUpstreamsOnceAcrossHandoffModes(t *testing.T) {
	for _, mode := range []string{"legacy", "overlap", "gateway", "standalone"} {
		t.Run(mode, func(t *testing.T) {
			m := &uniqueRegistrationMocks{counts: map[string]int{}}
			err := pulumi.RunErr(func(ctx *pulumi.Context) error {
				provider, err := kubernetes.NewProvider(ctx, "staging-provider", &kubernetes.ProviderArgs{})
				if err != nil {
					return err
				}
				return Deploy(ctx, provider, pulumi.String("35.222.36.75").ToStringOutput())
			}, pulumi.WithMocks("test", "staging", m), func(info *pulumi.RunInfo) {
				info.Config = map[string]string{
					"mcp-registry:gatewayCanary": "true", "mcp-registry:environment": "staging",
					"mcp-registry:provider": "gcp", "gcp:project": "mcp-registry-staging",
					"mcp-registry:gatewayStagingPublicIP": "35.222.36.75",
				}
				if mode == "standalone" {
					info.Config["mcp-registry:gatewayStagingStandalone"] = "true"
				} else {
					info.Config["mcp-registry:gatewayStagingHandoff"] = mode
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"default-registry-upstream", "monitoring-grafana-upstream"} {
				key := "kubernetes:gateway.nginx.org/v1alpha1:UpstreamSettingsPolicy::gateway-canary-upstreamsettingspolicy-" + name
				if m.counts[key] != 1 {
					t.Fatalf("%s registered %d times", name, m.counts[key])
				}
			}
		})
	}
}
