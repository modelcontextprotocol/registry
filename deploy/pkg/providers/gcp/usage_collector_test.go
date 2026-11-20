package gcp //nolint:testpackage // Exercise resource registration with Pulumi mocks.

import (
	"sync"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// usageAccessMocks also records which resources Pulumi would leave in place
// when they are removed from the program.
type usageAccessMocks struct {
	sync.Mutex
	resources map[string]resource.PropertyMap
	retained  map[string]bool
}

func (m *usageAccessMocks) Call(a pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return a.Args, nil
}

func (m *usageAccessMocks) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.Lock()
	defer m.Unlock()
	identity := a.TypeToken + "::" + a.Name
	m.resources[identity] = a.Inputs
	m.retained[identity] = a.RegisterRPC != nil && a.RegisterRPC.GetRetainOnDelete()
	return a.Name, a.Inputs, nil
}

func usageAccess(t *testing.T, flag string) *usageAccessMocks {
	t.Helper()
	m := &usageAccessMocks{resources: map[string]resource.PropertyMap{}, retained: map[string]bool{}}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		_, err := usageCollectorAccess(ctx, "mcp-registry-staging", "staging", nil)
		return err
	}, pulumi.WithMocks("test", "staging", m), func(i *pulumi.RunInfo) {
		i.Config = map[string]string{"mcp-registry:usageCollector": flag}
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

const (
	usageBucketIdentity = "gcp:storage/bucket:Bucket::usage-backup-bucket"
	usageAccount        = "registry-usage-reader@mcp-registry-staging.iam.gserviceaccount.com"
)

func TestUsageCollectorAccessIsLimitedToLogsAndItsOwnBackups(t *testing.T) {
	m := usageAccess(t, "true")
	grant := m.resources["gcp:projects/iAMMember:IAMMember::usage-collector-log-viewer"]
	if grant["role"].StringValue() != "roles/logging.viewer" || grant["member"].StringValue() != "serviceAccount:"+usageAccount || grant["project"].StringValue() != "mcp-registry-staging" {
		t.Errorf("unexpected project grant: %v", grant)
	}
	key := m.resources["gcp:serviceaccount/key:Key::usage-collector-key"]
	if key["serviceAccountId"].StringValue() != "projects/mcp-registry-staging/serviceAccounts/"+usageAccount {
		t.Errorf("the key is for %v", key["serviceAccountId"])
	}
	// The history's backups get a bucket of their own: a grant on the bucket
	// that holds the registry's backups would let this account alter those.
	bucket := m.resources[usageBucketIdentity]
	if bucket["name"].StringValue() != "mcp-registry-staging-usage-backups" || bucket["publicAccessPrevention"].StringValue() != "enforced" || !bucket["uniformBucketLevelAccess"].BoolValue() {
		t.Errorf("unexpected backup bucket: %v", bucket)
	}
	roles := map[string]bool{}
	for identity, inputs := range m.resources {
		switch {
		case identity == usageBucketIdentity, identity == "gcp:projects/iAMMember:IAMMember::usage-collector-log-viewer", identity == "gcp:serviceaccount/key:Key::usage-collector-key":
		case inputs["bucket"].HasValue() && inputs["bucket"].StringValue() == "mcp-registry-staging-usage-backups" && inputs["member"].StringValue() == "serviceAccount:"+usageAccount:
			roles[inputs["role"].StringValue()] = true
		default:
			// In particular not the account itself: managing it would let
			// retiring the collector delete it, and the deployment could not recreate it.
			t.Errorf("unexpected resource %s", identity)
		}
	}
	if len(roles) != 2 || !roles["roles/storage.objectAdmin"] || !roles["roles/storage.legacyBucketReader"] {
		t.Errorf("unexpected grants on the backup bucket: %v", roles)
	}
}

// Retiring removes every grant and the key, and keeps the backups.
func TestRetiredUsageCollectorKeepsItsBackups(t *testing.T) {
	m := usageAccess(t, "false")
	if len(m.resources) != 1 || m.resources[usageBucketIdentity] == nil {
		t.Fatalf("retiring keeps %v, want only the backup bucket", m.resources)
	}
	// Removing the setting afterwards, or destroying the stack, must not
	// delete the bucket either. Deleting a bucket that holds backups also fails.
	for _, flag := range []string{"true", "false"} {
		if !usageAccess(t, flag).retained[usageBucketIdentity] {
			t.Errorf("with the flag %q the backup bucket would be deleted with its resource", flag)
		}
	}
	if absent := usageAccess(t, ""); len(absent.resources) != 0 {
		t.Errorf("created without the flag: %v", absent.resources)
	}
}
