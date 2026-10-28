package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// ListingCacheStats contains only bounded aggregate state, never payload keys.
type ListingCacheStats struct{ Hits, Misses, Entries, PayloadBytes int64 }

func (m *DatabaseMetrics) ObserveListingCache(stats func() ListingCacheStats) (metric.Registration, error) {
	lookups, err := m.meter.Int64ObservableCounter(Namespace+".db.listing_json.cache.lookups", metric.WithDescription("Stored server JSON normalization lookups by hit or miss"))
	if err != nil {
		return nil, fmt.Errorf("create cache lookup counter: %w", err)
	}
	entries, err := m.meter.Int64ObservableGauge(Namespace + ".db.listing_json.cache.entries")
	if err != nil {
		return nil, fmt.Errorf("create cache entry gauge: %w", err)
	}
	bytes, err := m.meter.Int64ObservableGauge(Namespace+".db.listing_json.cache.payload_bytes", metric.WithDescription("Retained normalized JSON buffer capacity, excluding bounded map/list/key overhead"))
	if err != nil {
		return nil, fmt.Errorf("create cache bytes gauge: %w", err)
	}
	return m.meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		s := stats()
		observer.ObserveInt64(lookups, s.Hits, metric.WithAttributes(attribute.String("result", "hit")))
		observer.ObserveInt64(lookups, s.Misses, metric.WithAttributes(attribute.String("result", "miss")))
		observer.ObserveInt64(entries, s.Entries)
		observer.ObserveInt64(bytes, s.PayloadBytes)
		return nil
	}, lookups, entries, bytes)
}
