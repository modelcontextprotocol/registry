package v0

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/auth"
)

// personalOnlyPermissions models what the github-at exchange actually issues for a
// device-flow login: the caller's own namespace and nothing else. This is the exact
// shape that produces the 403 reported in issue #1468.
func personalOnlyPermissions(username string) []auth.Permission {
	return []auth.Permission{{
		Action:          auth.PermissionActionPublish,
		ResourcePattern: "io.github." + username + "/*",
	}}
}

func TestBuildPermissionErrorMessageDoesNotAdvisePublicisingOrgMembership(t *testing.T) {
	msg := buildPermissionErrorMessage("io.github.qatouch/qatouch", personalOnlyPermissions("premnathm"))

	// Public org membership stopped being how org namespaces are authorised once the
	// registry switched to checking the caller's org *role*. Still advising it sends
	// reporters off to re-check GitHub settings that were never the problem.
	if strings.Contains(msg, "publicizing-or-hiding-organization-membership") {
		t.Errorf("message still links the publicise-membership doc:\n%s", msg)
	}
	if strings.Contains(msg, "membership public") {
		t.Errorf("message still advises making org membership public:\n%s", msg)
	}
}

func TestBuildPermissionErrorMessageNamesWorkingOrgAuthPaths(t *testing.T) {
	msg := buildPermissionErrorMessage("io.github.qatouch/qatouch", personalOnlyPermissions("premnathm"))

	// Both real requirements, and both credentials that can actually satisfy them.
	for _, want := range []string{
		"Owner",
		"read:org",
		"--token",
		"GitHub Actions",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message does not mention %q:\n%s", want, msg)
		}
	}
}

func TestBuildPermissionErrorMessageKeepsAttemptedAndGrantedNamespaces(t *testing.T) {
	msg := buildPermissionErrorMessage("io.github.qatouch/qatouch", personalOnlyPermissions("premnathm"))

	// The diagnostic core of the message: what you asked for vs what you hold.
	if !strings.Contains(msg, "io.github.qatouch/qatouch") {
		t.Errorf("message omits the attempted resource:\n%s", msg)
	}
	if !strings.Contains(msg, "io.github.premnathm/*") {
		t.Errorf("message omits the granted pattern:\n%s", msg)
	}
}

func TestBuildPermissionErrorMessageOmitsGitHubGuidanceForOtherNamespaces(t *testing.T) {
	msg := buildPermissionErrorMessage("com.example/server", []auth.Permission{{
		Action:          auth.PermissionActionPublish,
		ResourcePattern: "com.other/*",
	}})

	// DNS/HTTP-verified namespaces have nothing to do with GitHub org roles.
	if strings.Contains(msg, "GitHub organization") {
		t.Errorf("GitHub org guidance leaked into a non-GitHub namespace:\n%s", msg)
	}
}
