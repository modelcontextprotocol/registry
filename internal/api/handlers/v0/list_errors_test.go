package v0

import (
	"context"
	"errors"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListServersError_clientCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := listServersError(ctx, errors.New("error iterating rows: context canceled"))
	require.Error(t, err)
	se, ok := err.(huma.StatusError)
	require.True(t, ok)
	assert.Equal(t, 499, se.GetStatus())
}

func TestListServersError_realFailure(t *testing.T) {
	err := listServersError(context.Background(), errors.New("database unavailable"))
	require.Error(t, err)
	se, ok := err.(huma.StatusError)
	require.True(t, ok)
	assert.Equal(t, 500, se.GetStatus())
}
