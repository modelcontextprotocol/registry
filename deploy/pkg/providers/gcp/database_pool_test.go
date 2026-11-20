package gcp

import (
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/container"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"testing"
)

type databasePoolMonitor struct {
	t       *testing.T
	machine string
	seen    bool
}

func (m *databasePoolMonitor) Call(a pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return a.Args, nil
}
func (m *databasePoolMonitor) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	if a.TypeToken == "gcp:container/nodePool:NodePool" {
		m.seen = true
		if a.Name != "registry-database-nodepool" || a.Inputs["nodeCount"].NumberValue() != 1 {
			m.t.Error("database pool identity/count changed")
		}
		c := a.Inputs["nodeConfig"].ObjectValue()
		if c["machineType"].StringValue() != m.machine || c["diskSizeGb"].NumberValue() != 50 || c["diskType"].StringValue() != "pd-balanced" {
			m.t.Error("unexpected database machine/disk")
		}
		if c["labels"].ObjectValue()["registry-role"].StringValue() != "database" || len(c["taints"].ArrayValue()) != 1 {
			m.t.Error("database isolation lost")
		}
	}
	return a.Name, a.Inputs, nil
}
func TestDatabaseMachineOverride(t *testing.T) {
	for _, override := range []string{"", "e2-standard-8"} {
		t.Run(override, func(t *testing.T) {
			m := &databasePoolMonitor{t: t, machine: "e2-standard-4"}
			cfg := map[string]string{}
			if override != "" {
				m.machine = override
				cfg["mcp-registry:databaseNodeMachineType"] = override
			}
			err := pulumi.RunErr(func(ctx *pulumi.Context) error {
				cluster, err := container.NewCluster(ctx, "cluster", &container.ClusterArgs{Name: pulumi.String("cluster")})
				if err != nil {
					return err
				}
				_, err = createDatabaseNodePool(ctx, cluster, "mcp-registry-prod", "us-central1-b", "e2-standard-4", nil)
				return err
			}, pulumi.WithMocks("test", "prod", m), func(i *pulumi.RunInfo) { i.Config = cfg })
			if err != nil {
				t.Fatal(err)
			}
			if !m.seen {
				t.Fatal("missing database pool")
			}
		})
	}
}
