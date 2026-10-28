package database

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPoolLimitsRejectInvalidConfigurationBeforeConnecting(t *testing.T) {
	for _, limits := range []PoolLimits{{0, 0}, {-1, 0}, {30, -1}, {30, 31}} {
		_, err := NewPostgreSQLWithPoolLimits(context.Background(), "invalid connection URI", limits)
		require.ErrorContains(t, err, "database")
	}
}

func TestExplicitPoolBudgetIsEnforced(t *testing.T) {
	original := NewTestDB(t).(*PostgreSQL)
	require.EqualValues(t, 60, original.pool.Config().MaxConns)
	require.EqualValues(t, 10, original.pool.Config().MinConns)
	db, err := NewPostgreSQLWithPoolLimits(context.Background(), original.pool.Config().ConnString(), PoolLimits{MaxConns: 30, MinConns: 5})
	require.NoError(t, err)
	defer db.Close()
	require.EqualValues(t, 30, db.pool.Config().MaxConns)
	require.EqualValues(t, 5, db.pool.Config().MinConns)
	for range 30 {
		conn, err := db.pool.Acquire(context.Background())
		require.NoError(t, err)
		defer conn.Release()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = db.pool.Acquire(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.EqualValues(t, 30, db.pool.Stat().AcquiredConns())
}
