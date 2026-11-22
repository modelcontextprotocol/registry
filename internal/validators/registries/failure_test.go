package registries_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The daily sweep acts on what a failed validation means, so each of these
// must keep its kind: a registry that is down must never read as a release
// that is gone.
func TestKindOfSaysWhatAFailedValidationMeans(t *testing.T) {
	const serverName = "io.github.test/demo"
	another := `{"info":{"description":"mcp-name: io.github.someone-else/demo\n"}}`
	tests := []struct {
		name          string
		versionStatus int
		versionBody   string
		packageStatus int
		want          registries.FailureKind
	}{
		{"the release names another server", http.StatusOK, another, http.StatusOK, registries.FailureOwnership},
		{"the release is gone, the package is not", http.StatusNotFound, "", http.StatusOK, registries.FailureNotFound},
		{"the package is gone", http.StatusNotFound, "", http.StatusNotFound, registries.FailureNotFound},
		{"the registry is failing", http.StatusBadGateway, "", http.StatusOK, registries.FailureTransient},
		{"the registry turns the request away", http.StatusTooManyRequests, "", http.StatusOK, registries.FailureTransient},
		{"the release is gone and the registry will not say more", http.StatusNotFound, "", http.StatusServiceUnavailable, registries.FailureTransient},
		{"an answer the validator cannot place", http.StatusTeapot, "", http.StatusOK, registries.FailureOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newPyPIMock(tt.versionStatus, tt.versionBody, tt.packageStatus)
			defer mock.Close()
			pkg := model.Package{RegistryType: model.RegistryTypePyPI, RegistryBaseURL: mock.URL, Identifier: "demo-pkg", Version: "1.0.0"}
			err := registries.ValidatePyPIPackage(context.Background(), pkg, serverName)
			require.Error(t, err)
			assert.Equal(t, tt.want, registries.KindOf(err), err.Error())
		})
	}

	t.Run("a registry that cannot be reached", func(t *testing.T) {
		mock := newPyPIMock(http.StatusOK, another, http.StatusOK)
		mock.Close()
		pkg := model.Package{RegistryType: model.RegistryTypePyPI, RegistryBaseURL: mock.URL, Identifier: "demo-pkg", Version: "1.0.0"}
		err := registries.ValidatePyPIPackage(context.Background(), pkg, serverName)
		require.Error(t, err)
		assert.Equal(t, registries.FailureTransient, registries.KindOf(err), err.Error())
	})

	t.Run("a reference the validator cannot read", func(t *testing.T) {
		err := registries.ValidatePyPI(context.Background(), model.Package{RegistryType: model.RegistryTypePyPI, Identifier: "demo-pkg"}, serverName)
		require.ErrorIs(t, err, registries.ErrMissingVersionForPyPi)
		assert.Equal(t, registries.FailureOther, registries.KindOf(err))
	})
}
