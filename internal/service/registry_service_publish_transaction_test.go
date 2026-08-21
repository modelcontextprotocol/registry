//nolint:testpackage
package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// errUnexpectedCall is returned by every countingDB method the test under test
// is not expected to reach. Returning an error rather than panicking keeps a
// surprise call visible as a test failure instead of a crash.
var errUnexpectedCall = errors.New("unexpected database call")

// countingDB is a database.Database that records how many transactions were
// opened. It exists to assert where work happens relative to the transaction
// boundary, which a real database cannot observe.
type countingDB struct {
	transactions int
}

func (d *countingDB) InTransaction(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	d.transactions++
	// A nil pgx.Tx is safe here: the code paths under test must fail before
	// touching it. Any use would panic and surface as a test failure.
	return fn(ctx, nil)
}

func (d *countingDB) CreateServer(context.Context, pgx.Tx, *apiv0.ServerJSON, *apiv0.RegistryExtensions) (*apiv0.ServerResponse, error) {
	return nil, errUnexpectedCall
}

func (d *countingDB) UpdateServer(context.Context, pgx.Tx, string, string, *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	return nil, errUnexpectedCall
}

func (d *countingDB) SetServerStatus(context.Context, pgx.Tx, string, string, model.Status, *string) (*apiv0.ServerResponse, error) {
	return nil, errUnexpectedCall
}

func (d *countingDB) SetAllVersionsStatus(context.Context, pgx.Tx, string, model.Status, *string) ([]*apiv0.ServerResponse, error) {
	return nil, errUnexpectedCall
}

func (d *countingDB) ListServers(context.Context, pgx.Tx, *database.ServerFilter, string, int) ([]*apiv0.ServerResponse, string, error) {
	return nil, "", errUnexpectedCall
}

func (d *countingDB) GetServerByName(context.Context, pgx.Tx, string, bool) (*apiv0.ServerResponse, error) {
	return nil, errUnexpectedCall
}

func (d *countingDB) GetServerByNameAndVersion(context.Context, pgx.Tx, string, string, bool) (*apiv0.ServerResponse, error) {
	return nil, errUnexpectedCall
}

func (d *countingDB) GetAllVersionsByServerName(context.Context, pgx.Tx, string, bool) ([]*apiv0.ServerResponse, error) {
	return nil, errUnexpectedCall
}

func (d *countingDB) GetCurrentLatestVersion(context.Context, pgx.Tx, string) (*apiv0.ServerResponse, error) {
	return nil, errUnexpectedCall
}

func (d *countingDB) CountServerVersions(context.Context, pgx.Tx, string) (int, error) {
	return 0, errUnexpectedCall
}

func (d *countingDB) CheckVersionExists(context.Context, pgx.Tx, string, string) (bool, error) {
	return false, errUnexpectedCall
}

func (d *countingDB) UnmarkAsLatest(context.Context, pgx.Tx, string) error { return errUnexpectedCall }

func (d *countingDB) SetLatestVersion(context.Context, pgx.Tx, string, string) error {
	return errUnexpectedCall
}

func (d *countingDB) AcquirePublishLock(context.Context, pgx.Tx, string) error {
	return errUnexpectedCall
}

func (d *countingDB) Close() error { return nil }

// serverWithUnvalidatablePackage returns a request whose registry-ownership
// check fails without any network access: ValidatePackage rejects an unknown
// registry type outright.
func serverWithUnvalidatablePackage() *apiv0.ServerJSON {
	return &apiv0.ServerJSON{
		Name:        "io.github.example/publish-transaction-test",
		Description: "server used to assert transaction boundaries",
		Version:     "1.0.0",
		Packages: []model.Package{
			{
				RegistryType: "not-a-real-registry",
				Identifier:   "example",
				Version:      "1.0.0",
			},
		},
	}
}

// capturePublishLogs redirects the default slog logger for the duration of the
// test and returns a function yielding everything written to it. The publish
// phase timings are only observable through that log line.
func capturePublishLogs(t *testing.T) func() string {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return buf.String
}

// Registry-ownership validation reaches out to npm, PyPI, NuGet, Cargo, OCI and
// MCPB over the network. Doing that inside the transaction pins a pgxpool
// connection for the duration of those round-trips, so under a publish burst
// connections are held by requests that are only waiting on third parties and
// everything behind them blocks in pool.Begin. Validating before the
// transaction opens is what keeps a slow upstream from consuming a connection.
func TestCreateServerValidatesBeforeOpeningTransaction(t *testing.T) {
	db := &countingDB{}
	svc := NewRegistryService(db, &config.Config{EnableRegistryValidation: true})

	_, err := svc.CreateServer(context.Background(), serverWithUnvalidatablePackage())

	require.Error(t, err, "an unknown registry type must fail registry validation")
	assert.Equal(t, 0, db.transactions,
		"registry validation failed, so no transaction should have been opened: validation must run before the transaction to avoid holding a pool connection during external HTTP calls")
}

// slowBeginDB simulates a starved connection pool: pool.Begin blocks before the
// transaction body runs. Everything after that succeeds, so a publish against it
// spends nearly all its time waiting for a connection.
type slowBeginDB struct {
	countingDB
	beginDelay time.Duration
}

func (d *slowBeginDB) InTransaction(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	time.Sleep(d.beginDelay)
	d.transactions++
	return fn(ctx, nil)
}

func (d *slowBeginDB) AcquirePublishLock(context.Context, pgx.Tx, string) error { return nil }

func (d *slowBeginDB) CountServerVersions(context.Context, pgx.Tx, string) (int, error) {
	return 0, nil
}

func (d *slowBeginDB) CheckVersionExists(context.Context, pgx.Tx, string, string) (bool, error) {
	return false, nil
}

func (d *slowBeginDB) GetCurrentLatestVersion(context.Context, pgx.Tx, string) (*apiv0.ServerResponse, error) {
	return nil, database.ErrNotFound
}

func (d *slowBeginDB) CreateServer(_ context.Context, _ pgx.Tx, serverJSON *apiv0.ServerJSON, meta *apiv0.RegistryExtensions) (*apiv0.ServerResponse, error) {
	return &apiv0.ServerResponse{Server: *serverJSON, Meta: apiv0.ResponseMeta{Official: meta}}, nil
}

// Time spent waiting for a pool connection was invisible: InTransaction calls
// pool.Begin before invoking the transaction body, and the old timing started
// inside that body. A publish stalled 40s on a starved pool therefore logged
// total_ms=200 and looked healthy, which is why the HTTP metric and these logs
// disagreed. pool_wait_ms has to account for it.
func TestCreateServerReportsTimeWaitingForAPoolConnection(t *testing.T) {
	logs := capturePublishLogs(t)
	const beginDelay = 150 * time.Millisecond
	db := &slowBeginDB{beginDelay: beginDelay}
	svc := NewRegistryService(db, &config.Config{EnableRegistryValidation: false})

	_, err := svc.CreateServer(context.Background(), &apiv0.ServerJSON{
		Name:        "io.github.example/pool-wait-test",
		Description: "server used to assert the pool wait is measured",
		Version:     "1.0.0",
	})
	require.NoError(t, err)

	poolWaitMs, ok := phaseValueFromLog(t, logs(), "pool_wait_ms")
	require.True(t, ok, "the publish log must report pool_wait_ms")
	assert.GreaterOrEqual(t, poolWaitMs, beginDelay.Milliseconds(),
		"pool_wait_ms must cover the time pool.Begin blocked, otherwise a starved pool is invisible in the logs")
}

// The log event moved out of the transaction body, so the deferred logger has to
// observe errors raised inside it too — otherwise failures after the transaction
// opens would stop being reported. Guards that, rather than driving new code.
func TestCreateServerLogsFailureRaisedInsideTransaction(t *testing.T) {
	logs := capturePublishLogs(t)
	db := &countingDB{} // AcquirePublishLock returns errUnexpectedCall
	svc := NewRegistryService(db, &config.Config{EnableRegistryValidation: false})

	_, err := svc.CreateServer(context.Background(), &apiv0.ServerJSON{
		Name:        "io.github.example/in-transaction-failure",
		Description: "server used to assert in-transaction failures are logged",
		Version:     "1.0.0",
	})

	require.Error(t, err)
	require.Equal(t, 1, db.transactions, "the failure must occur after the transaction opened")
	out := logs()
	assert.Contains(t, out, "publish failed")
	assert.Contains(t, out, "failed_phase="+phaseAcquireLock,
		"a phase failing inside the transaction must still be named in the log event")
}

// phaseValueFromLog pulls a single integer phase field out of the captured
// "key=value" slog text output.
func phaseValueFromLog(t *testing.T, out, field string) (int64, bool) {
	t.Helper()
	m := regexp.MustCompile(field + `=(\d+)`).FindStringSubmatch(out)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseInt(m[1], 10, 64)
	require.NoError(t, err)
	return v, true
}

// Moving validation out of the transaction must not cost us the diagnostic log
// line: a validation failure still has to report one "publish failed" event
// naming the phase that failed.
func TestCreateServerLogsValidationFailureWithPhase(t *testing.T) {
	logs := capturePublishLogs(t)
	svc := NewRegistryService(&countingDB{}, &config.Config{EnableRegistryValidation: true})

	_, err := svc.CreateServer(context.Background(), serverWithUnvalidatablePackage())
	require.Error(t, err)

	out := logs()
	assert.Contains(t, out, "publish failed", "a failed publish must emit its structured log event")
	assert.Contains(t, out, "failed_phase=validate", "the event must name validate as the failing phase")
	assert.Equal(t, 1, strings.Count(out, "publish failed"), "exactly one event per publish")
}
