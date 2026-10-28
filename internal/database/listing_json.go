package database

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/modelcontextprotocol/registry/internal/telemetry"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

type normalizedServer struct {
	key           [32]byte
	json          json.RawMessage
	name, version string
}

// listingJSONCache caches only the typed JSON normalization of a stored value.
// Every request still queries PostgreSQL for selection, ordering and metadata.
// Content addressing makes updates visible immediately without invalidation or
// TTLs. Both retained payload capacity and entry count are bounded.
type listingJSONCache struct {
	mu                          sync.Mutex
	entries                     map[[32]byte]*list.Element
	lru                         list.List
	bytes, maxBytes, maxEntries int
	hits, misses                int64
}

func newListingJSONCache(maxBytes, maxEntries int) *listingJSONCache {
	return &listingJSONCache{entries: make(map[[32]byte]*list.Element), maxBytes: maxBytes, maxEntries: maxEntries}
}

func (c *listingJSONCache) normalize(value []byte) (normalizedServer, error) {
	key := sha256.Sum256(value)
	if c != nil {
		c.mu.Lock()
		if e := c.entries[key]; e != nil {
			c.lru.MoveToFront(e)
			c.hits++
			result := e.Value.(normalizedServer)
			c.mu.Unlock()
			return result, nil
		}
		c.misses++
		c.mu.Unlock()
	}
	// Preserve the existing API's typed normalization, including omitted fields,
	// custom argument JSON handling and removal of unknown stored properties.
	var server apiv0.ServerJSON
	if err := json.Unmarshal(value, &server); err != nil {
		return normalizedServer{}, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}
	encoded, err := json.Marshal(server)
	if err != nil {
		return normalizedServer{}, fmt.Errorf("encode server JSON: %w", err)
	}
	result := normalizedServer{key: key, json: encoded, name: server.Name, version: server.Version}
	if c != nil {
		c.store(result)
	}
	return result, nil
}

func (c *listingJSONCache) store(value normalizedServer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.maxEntries <= 0 || cap(value.json) > c.maxBytes {
		return
	}
	if e := c.entries[value.key]; e != nil {
		c.lru.MoveToFront(e)
		return
	}
	for c.lru.Len() >= c.maxEntries || c.bytes+cap(value.json) > c.maxBytes {
		e := c.lru.Back()
		old := e.Value.(normalizedServer)
		delete(c.entries, old.key)
		c.bytes -= cap(old.json)
		c.lru.Remove(e)
	}
	c.entries[value.key] = c.lru.PushFront(value)
	c.bytes += cap(value.json)
}

// ListServersJSON is an HTTP serialization path. Typed listings remain in use
// for transactions, publish validation and other callers that inspect records.
func (db *PostgreSQL) ListServersJSON(ctx context.Context, filter *ServerFilter, cursor string, limit int) (_ apiv0.ServerListResponse, resultErr error) {
	diagnostic := telemetry.ListingFromContext(ctx)
	defer func() { diagnostic.DatabaseDone(resultErr) }()
	page, err := db.listServerRows(ctx, nil, filter, cursor, limit)
	if err != nil {
		return apiv0.ServerListResponse{}, err
	}
	if limit <= 0 {
		limit = 10
	}
	return encodeListingPage(ctx, page, limit, db.encodedListings, diagnostic)
}

func encodeListingPage(ctx context.Context, page []listingRow, limit int, cache *listingJSONCache, diagnostic *telemetry.ListingDiagnostic) (apiv0.ServerListResponse, error) {
	diagnostic.Phase("row_decode")
	records := make([]apiv0.EncodedServerRecord, 0, len(page))
	nextCursor := ""
	for i := range page {
		if err := ctx.Err(); err != nil {
			return apiv0.ServerListResponse{}, err
		}
		server, err := cache.normalize(page[i].value)
		if err != nil {
			return apiv0.ServerListResponse{}, err
		}
		page[i].value = nil
		records = append(records, apiv0.EncodedServerRecord{Server: server.json, Meta: apiv0.ResponseMeta{Official: &page[i].metadata}})
		if i == len(page)-1 && len(page) >= limit {
			nextCursor = server.name + ":" + server.version
		}
	}
	return apiv0.NewEncodedServerList(records, apiv0.Metadata{Count: len(records), NextCursor: nextCursor}), nil
}

func (c *listingJSONCache) stats() telemetry.ListingCacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return telemetry.ListingCacheStats{Hits: c.hits, Misses: c.misses, Entries: int64(c.lru.Len()), PayloadBytes: int64(c.bytes)}
}
