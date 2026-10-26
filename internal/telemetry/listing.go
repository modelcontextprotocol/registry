package telemetry

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/time/rate"
)

type listingKey struct{}

// ListingShape describes predicates, never their values. These fields belong in
// bounded diagnostic logs, not metric labels.
type ListingShape struct {
	Search, Cursor, Updated, Latest, Version, IncludeDeleted bool
	Limit                                                    int
}

var listingLogBudget = struct {
	sync.Mutex
	limiter    *rate.Limiter
	suppressed uint64
}{limiter: rate.NewLimiter(2, 10)}

var listingPod, _ = os.Hostname()

// ListingDiagnostic follows one list request through the handler and database.
// The mutex also protects snapshots taken by the slow-request timer. All phases
// are exclusive wall-clock intervals; they are not database CPU measurements.
type ListingDiagnostic struct {
	mu                                                      sync.Mutex
	started, phaseStarted, queryStarted                     time.Time
	phase, failedPhase, requestID, route, outcome, sqlState string
	durations                                               map[string]time.Duration
	shape                                                   ListingShape
	rows, bytes                                             int64
	firstRow                                                time.Duration
	sawFirstRow                                             bool
	backendPID                                              uint32
	poolAcquired, poolMax                                   int32
	done                                                    bool
	timer                                                   *time.Timer
	threshold, interval                                     time.Duration
	emit                                                    func([]slog.Attr)
}

func listingRequestID(value string) string {
	if len(value) > 0 && len(value) <= 128 && strings.IndexFunc(value, func(r rune) bool {
		allowed := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'
		return !allowed
	}) == -1 {
		return value
	}
	return rand.Text()
}

// StartListingDiagnostic emits only slow/failed requests. One progress snapshot
// at 2s and then every 10s records requests that have not completed. Across a pod,
// all diagnostic events share a 2/s budget with a burst of 10.
func StartListingDiagnostic(ctx context.Context, requestID, route string) (context.Context, *ListingDiagnostic) {
	d := newListingDiagnostic(requestID, route, 2*time.Second, 10*time.Second, emitListingDiagnostic)
	return context.WithValue(ctx, listingKey{}, d), d
}

func newListingDiagnostic(requestID, route string, threshold, interval time.Duration, emit func([]slog.Attr)) *ListingDiagnostic {
	now := time.Now()
	d := &ListingDiagnostic{started: now, phaseStarted: now, phase: "handler", requestID: listingRequestID(requestID), route: route,
		durations: make(map[string]time.Duration), threshold: threshold, interval: interval, emit: emit}
	d.mu.Lock()
	d.timer = time.AfterFunc(threshold, d.progress)
	d.mu.Unlock()
	return d
}

func ListingFromContext(ctx context.Context) *ListingDiagnostic {
	d, _ := ctx.Value(listingKey{}).(*ListingDiagnostic)
	return d
}

func (d *ListingDiagnostic) phaseLocked(phase string) {
	now := time.Now()
	d.durations[d.phase] += now.Sub(d.phaseStarted)
	d.phase, d.phaseStarted = phase, now
}

func (d *ListingDiagnostic) Phase(phase string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.phaseLocked(phase)
}

func (d *ListingDiagnostic) SetShape(shape ListingShape) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.shape = shape
}

func (d *ListingDiagnostic) AcquireStarted(acquired, maximum int32) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.poolAcquired, d.poolMax = acquired, maximum
	d.phaseLocked("pool_acquire")
}

func (d *ListingDiagnostic) QueryStarted(pid uint32) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.backendPID, d.queryStarted = pid, time.Now()
	d.phaseLocked("query_wait")
}

func (d *ListingDiagnostic) RowReceived() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.sawFirstRow && !d.queryStarted.IsZero() {
		d.firstRow, d.sawFirstRow = time.Since(d.queryStarted), true
	}
	d.rows++
	d.phaseLocked("row_decode")
}

func (d *ListingDiagnostic) AddBytes(n int) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.bytes += int64(n)
}

// DatabaseDone preserves the failing phase and switches to response processing.
// The caller must close rows first, so release/close time is not attributed to
// response processing. Bytes count JSONB payload bytes, not HTTP response bytes.
func (d *ListingDiagnostic) DatabaseDone(err error) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err != nil {
		d.failedPhase = d.phase
		d.outcome = "error"
		if errors.Is(err, context.Canceled) {
			d.outcome = outcomeCanceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			d.outcome = outcomeTimeout
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			d.sqlState = pgErr.Code
		}
	}
	d.phaseLocked("response")
}

func (d *ListingDiagnostic) snapshotLocked(event string, status int) []slog.Attr {
	now := time.Now()
	outcome := d.outcome
	if !d.done && outcome == "" {
		outcome = "in_progress"
	}
	attrs := []slog.Attr{
		slog.String("event", event), slog.String("request_id", d.requestID), slog.String("pod", listingPod), slog.String("route", d.route),
		slog.Time("started_at", d.started.UTC()), slog.Time("observed_at", now.UTC()), slog.Float64("total_ms", float64(now.Sub(d.started))/float64(time.Millisecond)),
		slog.String("phase", d.phase), slog.String("failed_phase", d.failedPhase), slog.String("outcome", outcome), slog.String("sqlstate", d.sqlState), slog.Int("status", status),
		slog.Int64("rows_received", d.rows), slog.Int64("json_bytes", d.bytes), slog.Bool("first_row_received", d.sawFirstRow), slog.Float64("first_row_ms", float64(d.firstRow)/float64(time.Millisecond)),
		slog.Uint64("backend_pid", uint64(d.backendPID)), slog.Int("pool_acquired_at_start", int(d.poolAcquired)), slog.Int("pool_max", int(d.poolMax)),
		slog.Bool("search", d.shape.Search), slog.Bool("cursor", d.shape.Cursor), slog.Bool("updated", d.shape.Updated), slog.Bool("latest", d.shape.Latest),
		slog.Bool("exact_version", d.shape.Version), slog.Bool("include_deleted", d.shape.IncludeDeleted), slog.Int("limit", d.shape.Limit),
	}
	for _, phase := range []string{"handler", "pool_acquire", "query_wait", "row_fetch", "row_decode", "response"} {
		elapsed := d.durations[phase]
		if d.phase == phase {
			elapsed += now.Sub(d.phaseStarted)
		}
		attrs = append(attrs, slog.Float64(phase+"_ms", float64(elapsed)/float64(time.Millisecond)))
	}
	return attrs
}

func (d *ListingDiagnostic) progress() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.done {
		return
	}
	d.emit(d.snapshotLocked("listing_slow_in_progress", 0))
	d.timer = time.AfterFunc(d.interval, d.progress)
}

// Finish is idempotent and serializes with progress: no progress event can be
// emitted after the completion event. Context cancellation does not erase logs.
func (d *ListingDiagnostic) Finish(status int, requestErr error) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.done {
		return
	}
	d.done = true
	d.timer.Stop()
	if errors.Is(requestErr, context.Canceled) {
		d.outcome = outcomeCanceled
		status = 499
	}
	if errors.Is(requestErr, context.DeadlineExceeded) {
		d.outcome = outcomeTimeout
	}
	if d.outcome == "" {
		d.outcome = outcomeSuccess
		if status >= 400 {
			d.outcome = "http_error"
		}
	}
	if time.Since(d.started) >= d.threshold || d.outcome != outcomeSuccess {
		d.emit(d.snapshotLocked("listing_complete", status))
	}
}

func emitListingDiagnostic(attrs []slog.Attr) {
	listingLogBudget.Lock()
	defer listingLogBudget.Unlock()
	if !listingLogBudget.limiter.Allow() {
		listingLogBudget.suppressed++
		return
	}
	attrs = append(attrs, slog.Uint64("diagnostics_suppressed", listingLogBudget.suppressed))
	listingLogBudget.suppressed = 0
	slog.LogAttrs(context.Background(), slog.LevelWarn, "listing diagnostic", attrs...)
}
