package validators

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

func TestPreflightStopsAtFirstIssueOnlyForWrites(t *testing.T) {
	var req apiv0.ServerJSON
	for i := range maxPackageOwnershipChecks + 1 {
		req.Packages = append(req.Packages, model.Package{Identifier: fmt.Sprintf("pkg-%d", i)})
	}
	for i := range maxRemoteHostLookupsPerRequest + 1 {
		req.Remotes = append(req.Remotes, model.Transport{URL: fmt.Sprintf("https://host-%d.example.org", i)})
	}
	req.Packages[0].RunTimeHint = strings.Repeat("x", maxNestedStringBytes+1)
	opts := ValidationOptions{EnableRegistryValidation: true}

	_, write := validateWritePreflight(&req, opts, stopAtFirstIssue)
	require.Len(t, write.Issues, 1)
	require.Equal(t, "submission-size", write.Issues[0].Reference)

	_, offline := validateWritePreflight(&req, opts, collectAllIssues)
	require.Len(t, offline.Issues, 3)
	require.Equal(t, "submission-size", offline.Issues[0].Reference)
	require.Equal(t, "package-ownership-limit", offline.Issues[1].Reference)
	require.Equal(t, "remote-host-lookup-limit", offline.Issues[2].Reference)

	req.Packages[0].RunTimeHint = ""
	_, write = validateWritePreflight(&req, opts, stopAtFirstIssue)
	require.Len(t, write.Issues, 1)
	require.Equal(t, "package-ownership-limit", write.Issues[0].Reference)
}
