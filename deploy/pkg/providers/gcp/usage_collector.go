package gcp

import (
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/projects"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/serviceaccount"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/usagecollector"
)

// usageCollectorKey lets the usage collector read this project's logs and
// returns a key for it, as base64-encoded JSON. The account is only referred
// to, never managed, so that retiring the collector cannot delete it.
func usageCollectorKey(ctx *pulumi.Context, projectID string, opts []pulumi.ResourceOption) (pulumi.StringOutput, error) {
	mode, err := usagecollector.Mode(ctx)
	if err != nil || mode != usagecollector.Enabled {
		return pulumi.StringOutput{}, err
	}
	email := usagecollector.ServiceAccount + "@" + projectID + ".iam.gserviceaccount.com"
	// Reading access records needs logging.logEntries.list on the project. This
	// role also reads the project's other logs, but not its data-access logs.
	_, err = projects.NewIAMMember(ctx, "usage-collector-log-viewer", &projects.IAMMemberArgs{
		Project: pulumi.String(projectID),
		Role:    pulumi.String("roles/logging.viewer"),
		Member:  pulumi.String("serviceAccount:" + email),
	}, opts...)
	if err != nil {
		return pulumi.StringOutput{}, err
	}
	key, err := serviceaccount.NewKey(ctx, "usage-collector-key", &serviceaccount.KeyArgs{
		ServiceAccountId: pulumi.String("projects/" + projectID + "/serviceAccounts/" + email),
		Keepers:          pulumi.StringMap{"generation": pulumi.String(usagecollector.CredentialGeneration)},
	}, opts...)
	if err != nil {
		return pulumi.StringOutput{}, err
	}
	return key.PrivateKey, nil
}
