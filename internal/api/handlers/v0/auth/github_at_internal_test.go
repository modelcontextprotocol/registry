package auth

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The 401 returned to the client must not echo the internal error, which can
// include the raw upstream GitHub response body captured by readErrorBody
// (CWE-209). huma serializes extra error args into the response body, so the fix
// is to log server-side and return only a generic message. This mirrors the
// regression test added for the GET /v0/servers 500 leak (#1338).
func TestTokenExchangeError_doesNotLeakDetail(t *testing.T) {
	const internal = `failed to get GitHub organizations: GitHub API error (status 500): {"message":"internal-host db detail"}`

	err := tokenExchangeError(errors.New(internal))
	require.Error(t, err)

	var se huma.StatusError
	require.True(t, errors.As(err, &se))
	assert.Equal(t, 401, se.GetStatus())

	body, marshalErr := json.Marshal(err)
	require.NoError(t, marshalErr)
	assert.NotContains(t, string(body), "internal-host db detail")
	assert.NotContains(t, string(body), "status 500")
	assert.Contains(t, string(body), "Token exchange failed")
}
