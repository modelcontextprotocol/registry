package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

const maxServerVersionsPerServer = 10000

// Publish phase names emitted on the structured "publish complete"/"publish failed"
// log. validate and pool_begin happen in CreateServer, before or while acquiring the
// transaction; the rest inside createServerInTransaction. Constants because
// version_checks is reported from multiple branches.
const (
	phaseValidate           = "validate"
	phasePoolBegin          = "pool_begin"
	phaseAcquireLock        = "acquire_lock"
	phaseValidateRemoteURLs = "validate_remote_urls"
	phaseVersionChecks      = "version_checks"
	phaseUnmarkLatest       = "unmark_latest"
	phaseDBCreate           = "db_create"
)

// publishTimings accumulates the per-phase durations reported on the single
// structured "publish complete"/"publish failed" event emitted per publish.
//
// CreateServer records the phases that run before the transaction opens
// (validate) plus the wait for a pool connection; createServerInTransaction
// records the phases inside it. Splitting it that way is deliberate: the previous
// shape started its clock after pool.Begin had already returned, so a publish
// that spent 40s waiting for a free connection logged total_ms=200 and looked
// healthy. That is why the HTTP histogram and these logs disagreed — over 7 days
// the histogram saw 17 publishes above 10s while no logged publish exceeded 6.5s.
// pool_wait_ms is the missing time.
//
// total_ms covers the whole CreateServer call, so any time not accounted for by
// the named phases is transaction commit plus overhead.
type publishTimings struct {
	validateMs      int64
	poolWaitMs      int64
	lockMs          int64
	remotesMs       int64
	versionChecksMs int64
	unmarkMs        int64
	createMs        int64
	failedPhase     string
}

// run times fn into *ms. On failure it records which phase failed and returns
// the error so the caller can abort.
func (t *publishTimings) run(name string, ms *int64, fn func() error) error {
	started := time.Now()
	err := fn()
	*ms = time.Since(started).Milliseconds()
	if err != nil {
		t.failedPhase = name
	}
	return err
}

// log emits the one structured event for this publish.
func (t *publishTimings) log(ctx context.Context, serverJSON apiv0.ServerJSON, totalMs int64, err error) {
	attrs := []any{
		"server_name", serverJSON.Name,
		"version", serverJSON.Version,
		"total_ms", totalMs,
		"validate_ms", t.validateMs,
		"pool_wait_ms", t.poolWaitMs,
		"lock_ms", t.lockMs,
		"remotes_ms", t.remotesMs,
		"version_checks_ms", t.versionChecksMs,
		"unmark_ms", t.unmarkMs,
		"create_ms", t.createMs,
	}
	if err != nil {
		attrs = append(attrs, "failed_phase", t.failedPhase, "error", err.Error())
		slog.WarnContext(ctx, "publish failed", attrs...)
	} else {
		slog.InfoContext(ctx, "publish complete", attrs...)
	}
}

// registryServiceImpl implements the RegistryService interface using our Database
type registryServiceImpl struct {
	db  database.Database
	cfg *config.Config
}

// NewRegistryService creates a new registry service with the provided database
func NewRegistryService(db database.Database, cfg *config.Config) RegistryService {
	return &registryServiceImpl{
		db:  db,
		cfg: cfg,
	}
}

// ListServers returns registry entries with cursor-based pagination and optional filtering
func (s *registryServiceImpl) ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	// If limit is not set or negative, use a default limit
	if limit <= 0 {
		limit = 30
	}

	// Use the database's ListServers method with pagination and filtering
	serverRecords, nextCursor, err := s.db.ListServers(ctx, nil, filter, cursor, limit)
	if err != nil {
		return nil, "", err
	}

	return serverRecords, nextCursor, nil
}

// GetServerByName retrieves the latest version of a server by its server name
func (s *registryServiceImpl) GetServerByName(ctx context.Context, serverName string, includeDeleted bool) (*apiv0.ServerResponse, error) {
	serverRecord, err := s.db.GetServerByName(ctx, nil, serverName, includeDeleted)
	if err != nil {
		return nil, err
	}

	return serverRecord, nil
}

// GetServerByNameAndVersion retrieves a specific version of a server by server name and version
func (s *registryServiceImpl) GetServerByNameAndVersion(ctx context.Context, serverName string, version string, includeDeleted bool) (*apiv0.ServerResponse, error) {
	serverRecord, err := s.db.GetServerByNameAndVersion(ctx, nil, serverName, version, includeDeleted)
	if err != nil {
		return nil, err
	}

	return serverRecord, nil
}

// GetAllVersionsByServerName retrieves all versions of a server by server name
func (s *registryServiceImpl) GetAllVersionsByServerName(ctx context.Context, serverName string, includeDeleted bool) ([]*apiv0.ServerResponse, error) {
	serverRecords, err := s.db.GetAllVersionsByServerName(ctx, nil, serverName, includeDeleted)
	if err != nil {
		return nil, err
	}

	return serverRecords, nil
}

// CreateServer creates a new server version
func (s *registryServiceImpl) CreateServer(ctx context.Context, req *apiv0.ServerJSON) (resp *apiv0.ServerResponse, err error) {
	start := time.Now()
	serverJSON := *req
	timings := &publishTimings{}

	defer func() {
		timings.log(ctx, serverJSON, time.Since(start).Milliseconds(), err)
	}()

	// Registry-ownership validation fans out to npm/PyPI/NuGet/Cargo/OCI/MCPB with
	// a 10s timeout per host. It runs before the transaction opens so those
	// round-trips never hold a pgxpool connection. Inside the transaction, a burst
	// of publishes each waiting on a third-party registry would occupy connections
	// while doing no database work, starving the pool and stalling unrelated
	// requests in pool.Begin. Nothing here reads our own database, so hoisting it
	// introduces no race; the per-server advisory lock was already taken after
	// validation, and still is.
	if err = timings.run(phaseValidate, &timings.validateMs, func() error {
		return validators.ValidatePublishRequest(ctx, serverJSON, s.cfg)
	}); err != nil {
		return nil, err
	}

	// Everything from here needs the database. InTransaction calls pool.Begin
	// before invoking this callback, so the gap between these two timestamps is
	// exactly how long the publish waited for a free connection.
	beforeBegin := time.Now()
	gotConnection := false
	resp, err = database.InTransactionT(ctx, s.db, func(ctx context.Context, tx pgx.Tx) (*apiv0.ServerResponse, error) {
		gotConnection = true
		timings.poolWaitMs = time.Since(beforeBegin).Milliseconds()
		return s.createServerInTransaction(ctx, tx, req, timings)
	})
	if !gotConnection {
		// Begin never handed us a connection, so the callback above did not run and
		// no phase inside the transaction was reached. This is the shape the worst
		// starvation takes — Begin blocking until the context deadline — so record
		// the whole elapsed wait and name the phase, rather than reporting zero
		// against an empty phase for the exact case this instrumentation targets.
		timings.poolWaitMs = time.Since(beforeBegin).Milliseconds()
		timings.failedPhase = phasePoolBegin
	}
	return resp, err
}

// createServerInTransaction contains the actual CreateServer logic that needs the
// database. Registry-ownership validation deliberately happens in CreateServer,
// before the transaction opens — see the comment there.
//
// Each phase is timed into the shared publishTimings so CreateServer can emit one
// structured log event per publish. During the 2026-04-27 incident a validate-only
// timing hid pool-exhaustion stalls in acquire_lock / version_checks / db_create;
// with every phase reported the next slow publish says which step to blame.
func (s *registryServiceImpl) createServerInTransaction(ctx context.Context, tx pgx.Tx, req *apiv0.ServerJSON, t *publishTimings) (resp *apiv0.ServerResponse, err error) {
	serverJSON := *req
	publishTime := time.Now()

	// Acquire advisory lock to prevent concurrent publishes of the same server
	if err = t.run(phaseAcquireLock, &t.lockMs, func() error {
		return s.db.AcquirePublishLock(ctx, tx, serverJSON.Name)
	}); err != nil {
		return nil, err
	}

	// Check for duplicate remote URLs
	if err = t.run(phaseValidateRemoteURLs, &t.remotesMs, func() error {
		return s.validateNoDuplicateRemoteURLs(ctx, tx, serverJSON)
	}); err != nil {
		return nil, err
	}

	// Version checks: count, exists, current-latest (small DB lookups, but on a
	// starved pool any of them stalls until a connection is free). Bundled under
	// one phase since they share a logical step.
	var currentLatest *apiv0.ServerResponse
	if err = t.run(phaseVersionChecks, &t.versionChecksMs, func() error {
		versionCount, e := s.db.CountServerVersions(ctx, tx, serverJSON.Name)
		if e != nil && !errors.Is(e, database.ErrNotFound) {
			return e
		}
		if versionCount >= maxServerVersionsPerServer {
			return database.ErrMaxServersReached
		}
		versionExists, e := s.db.CheckVersionExists(ctx, tx, serverJSON.Name, serverJSON.Version)
		if e != nil {
			return e
		}
		if versionExists {
			return database.ErrInvalidVersion
		}
		currentLatest, e = s.db.GetCurrentLatestVersion(ctx, tx, serverJSON.Name)
		if e != nil && !errors.Is(e, database.ErrNotFound) {
			return e
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// Determine if this version should be marked as latest
	isNewLatest := true
	if currentLatest != nil {
		var existingPublishedAt time.Time
		if currentLatest.Meta.Official != nil {
			existingPublishedAt = currentLatest.Meta.Official.PublishedAt
		}
		isNewLatest = CompareVersions(
			serverJSON.Version,
			currentLatest.Server.Version,
			publishTime,
			existingPublishedAt,
		) > 0
	}

	// Unmark old latest version if needed
	if isNewLatest && currentLatest != nil {
		if err = t.run(phaseUnmarkLatest, &t.unmarkMs, func() error {
			return s.db.UnmarkAsLatest(ctx, tx, serverJSON.Name)
		}); err != nil {
			return nil, err
		}
	}

	// Create metadata for the new server
	officialMeta := &apiv0.RegistryExtensions{
		Status:          model.StatusActive, /* New versions are active by default */
		StatusChangedAt: publishTime,
		PublishedAt:     publishTime,
		UpdatedAt:       publishTime,
		IsLatest:        isNewLatest,
	}

	// Insert new server version
	if err = t.run(phaseDBCreate, &t.createMs, func() error {
		var e error
		resp, e = s.db.CreateServer(ctx, tx, &serverJSON, officialMeta)
		return e
	}); err != nil {
		return nil, err
	}
	return resp, nil
}

// recalculateLatest picks the highest non-deleted version of the given server and flags it
// as latest, clearing is_latest on every other row. If every version is deleted, the highest
// deleted version keeps the flag so admin lookups (GetServerByName with includeDeleted=true)
// still find the server. Caller must hold the per-server publish lock.
func (s *registryServiceImpl) recalculateLatest(ctx context.Context, tx pgx.Tx, serverName string) error {
	versions, err := s.db.GetAllVersionsByServerName(ctx, tx, serverName, true)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return s.db.SetLatestVersion(ctx, tx, serverName, "")
		}
		return fmt.Errorf("failed to load versions for latest recalculation: %w", err)
	}

	winner := pickLatestVersion(versions, false)
	if winner == nil {
		// No non-deleted versions — fall back to highest deleted so the server is still
		// addressable via includeDeleted=true lookups.
		winner = pickLatestVersion(versions, true)
	}

	winnerVersion := ""
	if winner != nil {
		winnerVersion = winner.Server.Version
	}
	return s.db.SetLatestVersion(ctx, tx, serverName, winnerVersion)
}

// pickLatestVersion returns the highest version from the given slice. If allowDeleted is
// false, deleted versions are skipped.
func pickLatestVersion(versions []*apiv0.ServerResponse, allowDeleted bool) *apiv0.ServerResponse {
	var winner *apiv0.ServerResponse
	for _, v := range versions {
		if !allowDeleted && v.Meta.Official != nil && v.Meta.Official.Status == model.StatusDeleted {
			continue
		}
		if winner == nil {
			winner = v
			continue
		}
		var winnerPublishedAt, candidatePublishedAt time.Time
		if winner.Meta.Official != nil {
			winnerPublishedAt = winner.Meta.Official.PublishedAt
		}
		if v.Meta.Official != nil {
			candidatePublishedAt = v.Meta.Official.PublishedAt
		}
		if CompareVersions(v.Server.Version, winner.Server.Version, candidatePublishedAt, winnerPublishedAt) > 0 {
			winner = v
		}
	}
	return winner
}

// validateNoDuplicateRemoteURLs checks that no other server is using the same remote URLs
func (s *registryServiceImpl) validateNoDuplicateRemoteURLs(ctx context.Context, tx pgx.Tx, serverDetail apiv0.ServerJSON) error {
	// Check each remote URL in the new server for conflicts
	for _, remote := range serverDetail.Remotes {
		// Use filter to find servers with this remote URL
		filter := &database.ServerFilter{RemoteURL: &remote.URL}

		conflictingServers, _, err := s.db.ListServers(ctx, tx, filter, "", 1000)
		if err != nil {
			return fmt.Errorf("failed to check remote URL conflict: %w", err)
		}

		// Check if any conflicting server has a different name
		for _, conflictingServer := range conflictingServers {
			if conflictingServer.Server.Name != serverDetail.Name {
				return fmt.Errorf("remote URL %s is already used by server %s", remote.URL, conflictingServer.Server.Name)
			}
		}
	}

	return nil
}

// UpdateServer updates an existing server with new details
func (s *registryServiceImpl) UpdateServer(ctx context.Context, serverName, version string, req *apiv0.ServerJSON, statusChange *StatusChangeRequest) (*apiv0.ServerResponse, error) {
	// Wrap the entire operation in a transaction
	return database.InTransactionT(ctx, s.db, func(ctx context.Context, tx pgx.Tx) (*apiv0.ServerResponse, error) {
		return s.updateServerInTransaction(ctx, tx, serverName, version, req, statusChange)
	})
}

// updateServerInTransaction contains the actual UpdateServer logic within a transaction
func (s *registryServiceImpl) updateServerInTransaction(ctx context.Context, tx pgx.Tx, serverName, version string, req *apiv0.ServerJSON, statusChange *StatusChangeRequest) (*apiv0.ServerResponse, error) {
	// Get current server to check if it's deleted or being deleted
	// Include deleted servers since we may need to update or restore them
	currentServer, err := s.db.GetServerByNameAndVersion(ctx, tx, serverName, version, true)
	if err != nil {
		return nil, err
	}

	// Skip registry validation if:
	// 1. Server is currently deleted, OR
	// 2. Server is being set to deleted status
	currentlyDeleted := currentServer.Meta.Official != nil && currentServer.Meta.Official.Status == model.StatusDeleted
	beingDeleted := statusChange != nil && statusChange.NewStatus == model.StatusDeleted
	skipRegistryValidation := currentlyDeleted || beingDeleted

	// Unlike the publish path, this registry validation still runs inside the
	// transaction, so its external HTTP calls hold a pool connection. It cannot
	// simply be hoisted: skipRegistryValidation is derived from the
	// GetServerByNameAndVersion read above, so moving validation out needs a
	// second read before the transaction and a decision about the race in whether
	// to skip. Left as-is because the edit path carries a small fraction of
	// publish traffic; revisit if it shows up in pool_wait_ms.
	if err := validators.ValidateUpdateRequest(ctx, *req, s.cfg, skipRegistryValidation); err != nil {
		return nil, err
	}

	// Acquire advisory lock to prevent concurrent edits of servers with same name
	if err := s.db.AcquirePublishLock(ctx, tx, serverName); err != nil {
		return nil, err
	}

	// Merge the request with the current server, preserving metadata
	updatedServer := *req

	// Check for duplicate remote URLs using the updated server
	if err := s.validateNoDuplicateRemoteURLs(ctx, tx, updatedServer); err != nil {
		return nil, err
	}

	// Update server in database
	updatedServerResponse, err := s.db.UpdateServer(ctx, tx, serverName, version, &updatedServer)
	if err != nil {
		return nil, err
	}

	// Handle status change if provided
	if statusChange != nil {
		if _, err := s.db.SetServerStatus(ctx, tx, serverName, version, statusChange.NewStatus, statusChange.StatusMessage); err != nil {
			return nil, err
		}
		if err := s.recalculateLatest(ctx, tx, serverName); err != nil {
			return nil, err
		}
		// Re-read to pick up the possibly updated is_latest flag.
		return s.db.GetServerByNameAndVersion(ctx, tx, serverName, version, true)
	}

	return updatedServerResponse, nil
}

// UpdateServerStatus updates only the status metadata of a server version
func (s *registryServiceImpl) UpdateServerStatus(ctx context.Context, serverName, version string, statusChange *StatusChangeRequest) (*apiv0.ServerResponse, error) {
	// Wrap the entire operation in a transaction
	return database.InTransactionT(ctx, s.db, func(ctx context.Context, tx pgx.Tx) (*apiv0.ServerResponse, error) {
		return s.updateServerStatusInTransaction(ctx, tx, serverName, version, statusChange)
	})
}

// updateServerStatusInTransaction contains the actual UpdateServerStatus logic within a transaction
func (s *registryServiceImpl) updateServerStatusInTransaction(ctx context.Context, tx pgx.Tx, serverName, version string, statusChange *StatusChangeRequest) (*apiv0.ServerResponse, error) {
	// Get current server to verify it exists and check current status
	// Include deleted servers since we may need to restore them
	currentServer, err := s.db.GetServerByNameAndVersion(ctx, tx, serverName, version, true)
	if err != nil {
		return nil, err
	}

	// Acquire advisory lock to prevent concurrent edits of servers with same name
	if err := s.db.AcquirePublishLock(ctx, tx, serverName); err != nil {
		return nil, err
	}

	// When transitioning to active from deleted, validate remote URLs don't conflict
	if statusChange.NewStatus == model.StatusActive &&
		currentServer.Meta.Official != nil &&
		currentServer.Meta.Official.Status == model.StatusDeleted {
		if err := s.validateNoDuplicateRemoteURLs(ctx, tx, currentServer.Server); err != nil {
			return nil, err
		}
	}

	// Update only the status metadata
	if _, err := s.db.SetServerStatus(ctx, tx, serverName, version, statusChange.NewStatus, statusChange.StatusMessage); err != nil {
		return nil, err
	}
	if err := s.recalculateLatest(ctx, tx, serverName); err != nil {
		return nil, err
	}
	// Re-read to pick up the possibly updated is_latest flag.
	return s.db.GetServerByNameAndVersion(ctx, tx, serverName, version, true)
}

// UpdateAllVersionsStatus updates the status metadata of all versions of a server in a single transaction
func (s *registryServiceImpl) UpdateAllVersionsStatus(ctx context.Context, serverName string, statusChange *StatusChangeRequest) ([]*apiv0.ServerResponse, error) {
	// Wrap the entire operation in a transaction
	return database.InTransactionT(ctx, s.db, func(ctx context.Context, tx pgx.Tx) ([]*apiv0.ServerResponse, error) {
		return s.updateAllVersionsStatusInTransaction(ctx, tx, serverName, statusChange)
	})
}

// updateAllVersionsStatusInTransaction contains the actual UpdateAllVersionsStatus logic within a transaction
func (s *registryServiceImpl) updateAllVersionsStatusInTransaction(ctx context.Context, tx pgx.Tx, serverName string, statusChange *StatusChangeRequest) ([]*apiv0.ServerResponse, error) {
	// Acquire advisory lock to prevent concurrent edits of servers with same name
	if err := s.db.AcquirePublishLock(ctx, tx, serverName); err != nil {
		return nil, err
	}

	// When transitioning to active, validate remote URLs for any versions currently deleted
	if statusChange.NewStatus == model.StatusActive {
		includeDeleted := true

		// When transitioning to active, it means the current status is either deprecated or deleted, so it should include deleted server also
		filter := &database.ServerFilter{Name: &serverName, IncludeDeleted: &includeDeleted}
		versions, _, err := s.db.ListServers(ctx, tx, filter, "", 1000)
		if err != nil {
			return nil, fmt.Errorf("failed to list server versions: %w", err)
		}

		for _, version := range versions {
			if version.Meta.Official != nil &&
				version.Meta.Official.Status == model.StatusDeleted {
				if err := s.validateNoDuplicateRemoteURLs(ctx, tx, version.Server); err != nil {
					return nil, err
				}
			}
		}
	}

	// Update all versions' status in a single database call
	if _, err := s.db.SetAllVersionsStatus(ctx, tx, serverName, statusChange.NewStatus, statusChange.StatusMessage); err != nil {
		return nil, err
	}
	if err := s.recalculateLatest(ctx, tx, serverName); err != nil {
		return nil, err
	}
	// Re-read to pick up the possibly updated is_latest flags.
	return s.db.GetAllVersionsByServerName(ctx, tx, serverName, true)
}
