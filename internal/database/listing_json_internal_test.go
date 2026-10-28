package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/require"
)

func TestListingJSONPreservesNormalizationAndFreshMetadata(t *testing.T) {
	raw := []byte(`{"name":"example/server","version":"1.0","unknown":"discard","description":"<hello>\u2028","packages":[{"registryType":"npm","identifier":"x","version":"1","runtimeArguments":[{"type":"positional","value":"","valueHint":""}]}],"_meta":{"io.modelcontextprotocol.registry/publisher-provided":{"n":123.5}}}`)
	cache := newListingJSONCache(4096, 2)
	for _, status := range []model.Status{model.StatusActive, model.StatusDeleted} {
		meta := apiv0.RegistryExtensions{Status: status, IsLatest: status == model.StatusActive, UpdatedAt: time.Unix(100, 0).UTC()}
		typed, err := decodeListingPage(context.Background(), []listingRow{{value: raw, metadata: meta}}, nil)
		require.NoError(t, err)
		expected, err := json.Marshal(apiv0.ServerListResponse{Servers: []apiv0.ServerResponse{*typed[0]}, Metadata: apiv0.Metadata{Count: 1, NextCursor: "example/server:1.0"}})
		require.NoError(t, err)
		wire, err := encodeListingPage(context.Background(), []listingRow{{value: raw, metadata: meta}}, 1, cache, nil)
		require.NoError(t, err)
		actual, err := json.Marshal(wire)
		require.NoError(t, err)
		require.JSONEq(t, string(expected), string(actual))
		require.NotContains(t, string(actual), "discard")
	}
	changed := []byte(strings.Replace(string(raw), "<hello>", "changed", 1))
	got, err := cache.normalize(changed)
	require.NoError(t, err)
	require.Contains(t, string(got.json), "changed")
	require.Len(t, cache.entries, 2)
	_, err = cache.normalize([]byte(`{`))
	require.Error(t, err)
	require.Len(t, cache.entries, 2, "invalid JSON must not enter the cache")
}

func TestListingJSONCacheBoundsAndConcurrentReuse(t *testing.T) {
	cache := newListingJSONCache(1024, 2)
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_, err := cache.normalize([]byte(fmt.Sprintf(`{"name":"example/%d","version":"1"}`, i%4)))
				if err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	require.LessOrEqual(t, len(cache.entries), 2)
	require.LessOrEqual(t, cache.bytes, 1024)
	tiny := newListingJSONCache(1, 1)
	_, err := tiny.normalize([]byte(`{"name":"example/server"}`))
	require.NoError(t, err)
	require.Empty(t, tiny.entries)
	empty, err := encodeListingPage(context.Background(), nil, 30, cache, nil)
	require.NoError(t, err)
	encoded, err := json.Marshal(empty)
	require.NoError(t, err)
	require.JSONEq(t, `{"servers":[],"metadata":{"count":0}}`, string(encoded))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = encodeListingPage(ctx, []listingRow{{value: []byte(`{}`)}}, 30, cache, nil)
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
	for _, mode := range []string{"typed_roundtrip", "normalized_cache"} {
		b.Run(mode, func(b *testing.B) {
			cache := newListingJSONCache(64<<20, 8192)
			for _, row := range page {
				if _, err := cache.normalize(row.value); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				copyPage := append([]listingRow(nil), page...)
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
					response, err = encodeListingPage(context.Background(), copyPage, 100, cache, nil)
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
