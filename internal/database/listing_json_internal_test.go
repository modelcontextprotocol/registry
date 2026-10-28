package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/require"
)

func TestListingJSONPreservesNormalizationAndFreshMetadata(t *testing.T) {
	raw := []byte(`{"name":"example/server","version":"1.0","unknown":"discard","description":"<hello>\u2028","packages":[{"registryType":"npm","identifier":"x","version":"1","runtimeArguments":[{"type":"positional","value":"","valueHint":""}]}],"_meta":{"io.modelcontextprotocol.registry/publisher-provided":{"n":123.5}}}`)
	for _, status := range []model.Status{model.StatusActive, model.StatusDeleted} {
		meta := apiv0.RegistryExtensions{Status: status, IsLatest: status == model.StatusActive, UpdatedAt: time.Unix(100, 0).UTC()}
		typed, err := decodeListingPage(context.Background(), []listingRow{{value: raw, metadata: meta}}, nil)
		require.NoError(t, err)
		expected, err := json.Marshal(apiv0.ServerListResponse{Servers: []apiv0.ServerResponse{*typed[0]}, Metadata: apiv0.Metadata{Count: 1, NextCursor: "example/server:1.0"}})
		require.NoError(t, err)
		wire, err := encodeListingPage(context.Background(), []listingRow{{value: raw, metadata: meta}}, 1, nil)
		require.NoError(t, err)
		actual, err := json.Marshal(wire)
		require.NoError(t, err)
		require.JSONEq(t, string(expected), string(actual))
		require.NotContains(t, string(actual), "discard")
	}
	changed := []byte(strings.Replace(string(raw), "<hello>", "changed", 1))
	got, err := normalizeListingJSON(changed)
	require.NoError(t, err)
	require.Contains(t, string(got.json), "changed")
	_, err = normalizeListingJSON([]byte(`{`))
	require.Error(t, err)
}

func TestListingJSONEmptyAndCanceled(t *testing.T) {
	empty, err := encodeListingPage(context.Background(), nil, 30, nil)
	require.NoError(t, err)
	encoded, err := json.Marshal(empty)
	require.NoError(t, err)
	require.JSONEq(t, `{"servers":[],"metadata":{"count":0}}`, string(encoded))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = encodeListingPage(ctx, []listingRow{{value: []byte(`{}`)}}, 30, nil)
	require.ErrorIs(t, err, context.Canceled)
}

func BenchmarkListingJSON(b *testing.B) {
	page := make([]listingRow, 50)
	for i := range page {
		server := apiv0.ServerJSON{Name: fmt.Sprintf("example/server-%d", i), Version: "1.0", Description: strings.Repeat("description ", 8), Meta: &apiv0.ServerMeta{PublisherProvided: map[string]interface{}{"documentation": strings.Repeat("sample metadata and instructions. ", 150)}}}
		value, err := json.Marshal(server)
		if err != nil {
			b.Fatal(err)
		}
		page[i] = listingRow{value: value, metadata: apiv0.RegistryExtensions{Status: model.StatusActive}}
	}
	for _, mode := range []string{"typed_roundtrip", "prepared_json"} {
		b.Run(mode, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				copyPage := append([]listingRow(nil), page...)
				for i := range copyPage {
					copyPage[i].prepared = mode == "prepared_json"
				}
				var response apiv0.ServerListResponse
				if mode == "typed_roundtrip" {
					rows, err := decodeListingPage(context.Background(), copyPage, nil)
					if err != nil {
						b.Fatal(err)
					}
					values := make([]apiv0.ServerResponse, len(rows))
					for i, row := range rows {
						values[i] = *row
					}
					response = apiv0.ServerListResponse{Servers: values, Metadata: apiv0.Metadata{Count: 50}}
				} else {
					var err error
					response, err = encodeListingPage(context.Background(), copyPage, 100, nil)
					if err != nil {
						b.Fatal(err)
					}
				}
				var output any = response
				if mode == "typed_roundtrip" {
					type originalList apiv0.ServerListResponse
					output = originalList(response)
				}
				if _, err := json.Marshal(output); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestPreparedListingWritesFallbackAndBackfill(t *testing.T) {
	db := NewTestDB(t).(*PostgreSQL)
	ctx := context.Background()
	conn, err := db.pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	server := apiv0.ServerJSON{Name: "example/prepared", Version: "1", Description: "original", Meta: &apiv0.ServerMeta{PublisherProvided: map[string]interface{}{"large": int64(9007199254740993)}}}
	stamp := time.Now().UTC()
	meta := apiv0.RegistryExtensions{Status: model.StatusActive, IsLatest: true, UpdatedAt: stamp, PublishedAt: stamp, StatusChangedAt: stamp}
	_, err = db.CreateServer(ctx, nil, &server, &meta)
	require.NoError(t, err)
	check := func(wantPrepared bool) {
		t.Helper()
		rows, err := db.listServerRows(ctx, nil, nil, "", 10, true)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, wantPrepared, rows[0].prepared)
		fast, err := db.ListServersJSON(ctx, nil, "", 1)
		require.NoError(t, err)
		typed, cursor, err := db.ListServers(ctx, nil, nil, "", 1)
		require.NoError(t, err)
		expected, err := json.Marshal(apiv0.ServerListResponse{Servers: []apiv0.ServerResponse{*typed[0]}, Metadata: apiv0.Metadata{Count: 1, NextCursor: cursor}})
		require.NoError(t, err)
		actual, err := json.Marshal(fast)
		require.NoError(t, err)
		require.JSONEq(t, string(expected), string(actual))
	}
	check(true)
	// Metadata-only changes are immediately visible and do not require rewriting payloads.
	_, err = db.pool.Exec(ctx, `UPDATE servers SET status_message = 'fresh metadata', updated_at = now()`)
	require.NoError(t, err)
	check(true)
	// Simulate an older binary: JSONB changes without writing prepared fields.
	_, err = db.pool.Exec(ctx, `UPDATE servers SET value = jsonb_set(value, '{description}', '"old writer"')`)
	require.NoError(t, err)
	check(false)
	var value []byte
	require.NoError(t, conn.QueryRow(ctx, `SELECT value FROM servers`).Scan(&value))
	normalized, err := normalizeListingJSON(value)
	require.NoError(t, err)
	// A concurrent update after the backfill read must not install stale bytes.
	_, err = db.pool.Exec(ctx, `UPDATE servers SET value = jsonb_set(value, '{description}', '"concurrent writer"')`)
	require.NoError(t, err)
	tag, err := conn.Exec(ctx, prepareListingSQL, []byte(normalized.json), listingCodecVersion, server.Name, server.Version, value)
	require.NoError(t, err)
	require.Zero(t, tag.RowsAffected())
	check(false)
	result, err := BackfillListingJSONBatch(ctx, conn.Conn(), "", "", 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, result.Prepared)
	require.Equal(t, 1, result.Scanned)
	check(true)
	result, err = BackfillListingJSONBatch(ctx, conn.Conn(), result.LastName, result.LastVersion, 1)
	require.NoError(t, err)
	require.Zero(t, result.Scanned)
	// Unknown future codecs safely fall back during rolling versions/rollback.
	_, err = db.pool.Exec(ctx, `UPDATE servers SET listing_codec = 999`)
	require.NoError(t, err)
	check(false)
	server.Description = "new writer"
	_, err = db.UpdateServer(ctx, nil, server.Name, server.Version, &server)
	require.NoError(t, err)
	check(true)
	// Identity mismatches in legacy rows retain their old cursor semantics.
	_, err = db.pool.Exec(ctx, `UPDATE servers SET value = jsonb_set(value, '{name}', '"example/legacy-name"')`)
	require.NoError(t, err)
	result, err = BackfillListingJSONBatch(ctx, conn.Conn(), "", "", 10)
	require.NoError(t, err)
	require.Equal(t, 1, result.SkippedIdentity)
	require.Zero(t, result.Prepared)
	check(false)
}
