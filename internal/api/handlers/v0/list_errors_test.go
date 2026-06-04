package v0_test

import (
	"context"
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
