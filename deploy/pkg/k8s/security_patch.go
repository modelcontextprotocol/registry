package k8s

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// securityPatchVersion keeps production unchanged during staging qualification.
// Promote the same reviewed versions by opting in the production stack.
func securityPatchVersion(ctx *pulumi.Context, previous, patched string) pulumi.String {
	if config.New(ctx, "mcp-registry").GetBool("securityPatch202611") {
		return pulumi.String(patched)
	}
	return pulumi.String(previous)
}
