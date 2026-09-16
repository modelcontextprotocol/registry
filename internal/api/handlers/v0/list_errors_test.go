package v0_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListServersError_clientCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := v0.ListServersError(ctx, errors.New("error iterating rows: context canceled"))
	require.Error(t, err)
	var se huma.StatusError
	require.True(t, errors.As(err, &se))
	assert.Equal(t, 499, se.GetStatus())
}

func TestListServersError_realFailure(t *testing.T) {
	err := v0.ListServersError(context.Background(), errors.New("database unavailable"))
	require.Error(t, err)
	var se huma.StatusError
	require.True(t, errors.As(err, &se))
	assert.Equal(t, 500, se.GetStatus())
}

// The 500 response body must not echo the internal error back to the client,
// since it can carry DB/driver detail (CWE-209).
func TestListServersError_realFailureDoesNotLeakDetail(t *testing.T) {
	const internal = "database unavailable: dsn=postgres://user:pw@internal-host/db"
	err := v0.ListServersError(context.Background(), errors.New(internal))
	require.Error(t, err)

	body, marshalErr := json.Marshal(err)
	require.NoError(t, marshalErr)
	assert.NotContains(t, string(body), "database unavailable")
	assert.NotContains(t, string(body), "internal-host")
}
