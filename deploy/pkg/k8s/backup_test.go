package k8s

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type backupMonitor struct {
	mu        sync.Mutex
	resources map[string]resource.PropertyMap
}

func (m *backupMonitor) Call(a pulumi.MockCallArgs) (resource.PropertyMap, error) {
	if a.Token != "kubernetes:yaml:decode" {
		return nil, fmt.Errorf("unexpected provider call: %s", a.Token)
	}
	var obj any
	if err := json.Unmarshal([]byte(a.Args["text"].StringValue()), &obj); err != nil {
		return nil, err
	}
	return resource.NewPropertyMapFromMap(map[string]any{"result": []any{obj}}), nil
}
func (m *backupMonitor) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resources[a.TypeToken+"::"+a.Name] = a.Inputs
	return a.Name, a.Inputs, nil
}

func TestBackupRetirementPreservesProductionArchiveJob(t *testing.T) {
	for _, env := range []string{"prod", "staging", "local"} {
		t.Run(env, func(t *testing.T) {
			m := &backupMonitor{resources: map[string]resource.PropertyMap{}}
			err := pulumi.RunErr(func(ctx *pulumi.Context) error {
				p, err := kubernetes.NewProvider(ctx, "provider", &kubernetes.ProviderArgs{})
				if err != nil {
					return err
				}
				credentials, err := corev1.NewSecret(ctx, "k8up-backup-credentials", &corev1.SecretArgs{})
				if err != nil {
					return err
				}
				return DeployBackups(ctx, &providers.ProviderInfo{Provider: p}, env, &providers.BackupStorageInfo{Endpoint: "https://storage.googleapis.com", BucketName: "mcp-registry-prod-backups", Credentials: credentials})
			}, pulumi.WithMocks("mcp-registry-infra", "test", m))
			if err != nil {
				t.Fatal(err)
			}
			password, ok := m.resources["kubernetes:core/v1:Secret::k8up-repo-password"]
			if env == "staging" {
				if ok {
					t.Fatal("retired staging repository password recreated")
				}
			} else if !ok || password["metadata"].ObjectValue()["name"].StringValue() != "k8up-repo-password" {
				t.Fatal("existing repository secret identity was lost")
			}
			jobs := 0
			for identity, inputs := range m.resources {
				if strings.Contains(identity, "kubernetes:helm.sh/") || strings.Contains(identity, "CustomResourceDefinition") || strings.Contains(identity, "kubernetes:k8up.io/") {
					t.Fatalf("legacy operator resource recreated: %s", identity)
				}
				if !strings.HasPrefix(identity, "kubernetes:batch/v1:CronJob::") {
					continue
				}
				jobs++
				spec := inputs["spec"].ObjectValue()
				if spec["schedule"].StringValue() != "46 4 * * *" {
					t.Fatal("production schedule changed")
				}
				if inputs["metadata"].ObjectValue()["name"].StringValue() != "registry-database-backup" {
					t.Fatal("production backup identity changed")
				}
				b, err := json.Marshal(inputs.Mappable())
				if err != nil {
					t.Fatal(err)
				}
				for _, required := range []string{"k8up-repo-password", "k8up-backup-credentials", "postgres-custom-v1", "--keep-daily", "registry-pg-backup"} {
					if !strings.Contains(string(b), required) {
						t.Errorf("lost active backup dependency: %s", required)
					}
				}
			}
			want := 0
			if env == "prod" {
				want = 1
			}
			if jobs != want {
				t.Fatalf("got %d scheduled backups, want %d", jobs, want)
			}
		})
	}
}
