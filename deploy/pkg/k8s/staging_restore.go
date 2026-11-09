package k8s

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/yaml"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
	"strings"
)

//go:embed stagingrestore/run.py
var stagingRestoreController string

//go:embed stagingrestore/resources.json
var stagingRestoreResources string

func deployStagingRestore(ctx *pulumi.Context, cluster *providers.ProviderInfo) error {
	conf := config.New(ctx, "mcp-registry")
	if !conf.GetBool("stagingRestore") {
		return nil
	}
	if conf.Get("environment") != "staging" || config.New(ctx, "gcp").Get("project") != "mcp-registry-staging" {
		return fmt.Errorf("database hydration is staging-only")
	}
	script, _ := json.Marshal(stagingRestoreController)
	manifest := strings.ReplaceAll(stagingRestoreResources, `"__CONTROLLER__"`, string(script))
	manifest = strings.ReplaceAll(manifest, `"__SUSPEND__"`, fmt.Sprint(!conf.GetBool("stagingRestoreScheduleEnabled")))
	state, err := corev1.NewConfigMap(ctx, "registry-staging-restore-state", &corev1.ConfigMapArgs{
		Metadata: &metav1.ObjectMetaArgs{Name: pulumi.String("registry-staging-restore-state"), Namespace: pulumi.String("default")},
		Data:     pulumi.StringMap{"active_id": pulumi.String(""), "phase": pulumi.String("idle"), "last_success": pulumi.String("")},
	}, pulumi.Provider(cluster.Provider), pulumi.IgnoreChanges([]string{"data"}))
	if err != nil {
		return err
	}
	_, err = yaml.NewConfigGroup(ctx, "staging-database-restore", &yaml.ConfigGroupArgs{YAML: []string{manifest}}, pulumi.Provider(cluster.Provider), pulumi.DependsOn([]pulumi.Resource{state}))
	return err
}
