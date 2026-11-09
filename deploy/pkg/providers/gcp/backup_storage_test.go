package gcp //nolint:testpackage // Exercise resource registration with Pulumi mocks.

import (
	"sync"
	"testing"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type backupStorageMocks struct {
	sync.Mutex
	resources map[string]resource.PropertyMap
}

func (m *backupStorageMocks) Call(a pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return a.Args, nil
}
func (m *backupStorageMocks) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.Lock()
	defer m.Unlock()
	m.resources[a.TypeToken+"::"+a.Name] = a.Inputs
	out := a.Inputs.Copy()
	if a.TypeToken == "gcp:storage/hmacKey:HmacKey" {
		out["accessId"] = resource.NewStringProperty("test-id")
		out["secret"] = resource.NewStringProperty("test-secret")
	}
	return a.Name, out, nil
}
func TestStagingRetiresCredentialsWithoutDeletingBucket(t *testing.T) {
	for _, env := range []string{"prod", "staging"} {
		t.Run(env, func(t *testing.T) {
			m := &backupStorageMocks{resources: map[string]resource.PropertyMap{}}
			err := pulumi.RunErr(func(ctx *pulumi.Context) error {
				k, err := kubernetes.NewProvider(ctx, "k8s", &kubernetes.ProviderArgs{})
				if err != nil {
					return err
				}
				storage, err := (&Provider{}).CreateBackupStorage(ctx, &providers.ProviderInfo{Provider: k}, env)
				if err == nil && ((storage == nil) != (env == "staging")) {
					t.Error("unexpected backup storage consumer configuration")
				}
				return err
			}, pulumi.WithMocks("test", env, m), func(i *pulumi.RunInfo) { i.Config = map[string]string{"gcp:project": "mcp-registry-" + env} })
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"gcp:storage/bucket:Bucket::backup-bucket", "gcp:storage/bucketIAMMember:BucketIAMMember::backup-bucket-iam"} {
				if _, ok := m.resources[name]; !ok {
					t.Errorf("retained resource missing: %s", name)
				}
			}
			for _, name := range []string{"gcp:storage/hmacKey:HmacKey::backup-hmac-key", "kubernetes:core/v1:Secret::k8up-backup-credentials"} {
				_, ok := m.resources[name]
				if ok != (env == "prod") {
					t.Errorf("unexpected credential registration: %s", name)
				}
			}
		})
	}
}
