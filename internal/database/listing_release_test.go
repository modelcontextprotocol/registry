package database_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/stretchr/testify/require"
)

type listingTestTx struct {
	pgx.Tx
	rows pgx.Rows
}

func (tx listingTestTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return tx.rows, nil
}

type listingTestRows struct {
	pgx.Rows
	values  []string
	read    int
	closed  bool
	scanErr error
	endErr  error
	onClose func()
}

func (r *listingTestRows) Next() bool {
	if r.read < len(r.values) && !r.closed {
		r.read++
		return true
	}
	r.Close()
	return false
}

func (r *listingTestRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	*dest[0].(*string), *dest[1].(*string), *dest[2].(*string) = "example/server", "1.0", "active"
	*dest[3].(*time.Time) = time.Unix(100, 0).UTC()
	*dest[5].(*time.Time), *dest[6].(*time.Time) = time.Unix(200, 0).UTC(), time.Unix(300, 0).UTC()
	*dest[7].(*bool) = true
	*dest[8].(*[]byte) = []byte(r.values[r.read-1])
	return nil
}

func (r *listingTestRows) Close() {
	if !r.closed && r.onClose != nil {
		r.onClose()
	}
	r.closed = true
}

func (r *listingTestRows) Err() error { return r.endErr }

func TestListingReleasesRowsBeforeJSONDecode(t *testing.T) {
	// The first row is malformed. Decoding in the fetch loop would return before
	// consuming the second row and leave application work inside the query span.
	rows := &listingTestRows{values: []string{"{", `{}`}}
	db := &database.PostgreSQL{}
	_, _, err := db.ListServers(context.Background(), listingTestTx{rows: rows}, nil, "", 2)
	require.ErrorContains(t, err, "failed to unmarshal")
	require.Equal(t, 2, rows.read)
	require.True(t, rows.closed)
}

func TestListingReleaseErrorsAndCancellation(t *testing.T) {
	sentinel := errors.New("row failure")
	for _, test := range []struct {
		name string
		rows *listingTestRows
	}{
		{"scan", &listingTestRows{values: []string{`{}`}, scanErr: sentinel}},
		{"iteration", &listingTestRows{values: []string{`{}`}, endErr: sentinel}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := (&database.PostgreSQL{}).ListServers(context.Background(), listingTestTx{rows: test.rows}, nil, "", 2)
			require.ErrorIs(t, err, sentinel)
			require.True(t, test.rows.closed)
		})
	}
	t.Run("canceled before decode", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		rows := &listingTestRows{values: []string{"{"}, onClose: cancel}
		_, _, err := (&database.PostgreSQL{}).ListServers(ctx, listingTestTx{rows: rows}, nil, "", 2)
		require.ErrorIs(t, err, context.Canceled)
		require.True(t, rows.closed)
	})
}
