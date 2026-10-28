package database

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/registry/internal/telemetry"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Bump whenever ServerJSON's wire normalization changes. Unknown codecs fall
// back to authoritative JSONB, allowing mixed-version deployments and rollback.
const listingCodecVersion = 1

type normalizedServer struct {
	json          json.RawMessage
	name, version string
}

func normalizeListingJSON(value []byte) (normalizedServer, error) {
	var server apiv0.ServerJSON
	if err := json.Unmarshal(value, &server); err != nil {
		return normalizedServer{}, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}
	encoded, err := json.Marshal(server)
	if err != nil {
		return normalizedServer{}, fmt.Errorf("encode server JSON: %w", err)
	}
	return normalizedServer{json: encoded, name: server.Name, version: server.Version}, nil
}

func listingPayload(prefix string) string {
	valid := fmt.Sprintf("(%slisting_codec = %d AND %slisting_value IS NOT NULL)", prefix, listingCodecVersion, prefix)
	return fmt.Sprintf("CASE WHEN %s THEN %slisting_value ELSE convert_to(%svalue::text, 'UTF8') END, COALESCE(%s, false)", valid, prefix, prefix, valid)
}

// ListServersJSON is an HTTP serialization path. Typed listings remain in use
// for transactions, publish validation and other callers that inspect records.
func (db *PostgreSQL) ListServersJSON(ctx context.Context, filter *ServerFilter, cursor string, limit int) (_ apiv0.ServerListResponse, resultErr error) {
	diagnostic := telemetry.ListingFromContext(ctx)
	defer func() { diagnostic.DatabaseDone(resultErr) }()
	page, err := db.listServerRows(ctx, nil, filter, cursor, limit, true)
	if err != nil {
		return apiv0.ServerListResponse{}, err
	}
	if limit <= 0 {
		limit = 10
	}
	if db.listingMetrics != nil {
		var prepared int64
		for _, row := range page {
			if row.prepared {
				prepared++
			}
		}
		db.listingMetrics.Add(ctx, prepared, metric.WithAttributes(attribute.String("source", "prepared")))
		db.listingMetrics.Add(ctx, int64(len(page))-prepared, metric.WithAttributes(attribute.String("source", "fallback")))
	}
	return encodeListingPage(ctx, page, limit, diagnostic)
}

func encodeListingPage(ctx context.Context, page []listingRow, limit int, diagnostic *telemetry.ListingDiagnostic) (apiv0.ServerListResponse, error) {
	diagnostic.Phase("row_decode")
	records := make([]apiv0.EncodedServerRecord, 0, len(page))
	nextCursor := ""
	for i := range page {
		if err := ctx.Err(); err != nil {
			return apiv0.ServerListResponse{}, err
		}
		server := normalizedServer{json: page[i].value, name: page[i].name, version: page[i].version}
		if !page[i].prepared {
			var err error
			server, err = normalizeListingJSON(page[i].value)
			if err != nil {
				return apiv0.ServerListResponse{}, err
			}
		}
		page[i].value = nil
		records = append(records, apiv0.EncodedServerRecord{Server: server.json, Meta: apiv0.ResponseMeta{Official: &page[i].metadata}})
		if i == len(page)-1 && len(page) >= limit {
			nextCursor = server.name + ":" + server.version
		}
	}
	return apiv0.NewEncodedServerList(records, apiv0.Metadata{Count: len(records), NextCursor: nextCursor}), nil
}
