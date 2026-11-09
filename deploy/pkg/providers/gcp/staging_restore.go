package gcp

import (
	"fmt"
	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/serviceaccount"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// Reader permissions on the production bucket are bootstrapped separately by an
// authorized production operator. This account has no project-level grants.
func stagingRestoreReader(ctx *pulumi.Context, cluster *providers.ProviderInfo, opts []pulumi.ResourceOption) error {
	if !config.New(ctx, "mcp-registry").GetBool("stagingRestore") {
		return nil
	}
	if config.New(ctx, "gcp").Get("project") != "mcp-registry-staging" {
		return fmt.Errorf("restore reader requires staging project")
	}
	// The operator pre-creates this account: CI cannot create arbitrary accounts.
	accountOpts := append(append([]pulumi.ResourceOption{}, opts...), pulumi.Import(pulumi.ID("projects/mcp-registry-staging/serviceAccounts/registry-restore-reader@mcp-registry-staging.iam.gserviceaccount.com")))
	account, err := serviceaccount.NewAccount(ctx, "staging-restore-reader", &serviceaccount.AccountArgs{
		AccountId: pulumi.String("registry-restore-reader"), Project: pulumi.String("mcp-registry-staging"), DisplayName: pulumi.String("Staging production-backup reader"),
	}, accountOpts...)
	if err != nil {
		return err
	}
	key, err := serviceaccount.NewKey(ctx, "staging-restore-reader-key", &serviceaccount.KeyArgs{ServiceAccountId: account.Name}, opts...)
	if err != nil {
		return err
	}
	_, err = corev1.NewSecret(ctx, "registry-staging-restore-gcs", &corev1.SecretArgs{
		Metadata: &metav1.ObjectMetaArgs{Name: pulumi.String("registry-staging-restore-gcs"), Namespace: pulumi.String("default")},
		Type:     pulumi.String("Opaque"), Data: pulumi.StringMap{"gcs.json": key.PrivateKey},
	}, pulumi.Provider(cluster.Provider))
	return err
}
