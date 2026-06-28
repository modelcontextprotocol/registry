package v0

import (
	"context"
	"errors"
	"log"

	"github.com/danielgtaylor/huma/v2"
)

func clientClosedRequest(ctx context.Context, err error) (error, bool) {
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return huma.NewError(499, "Client closed request", err), true
	}
	return nil, false
}

// ListServersError maps ListServers failures; client disconnects must not log as 500s.
func ListServersError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if cerr, ok := clientClosedRequest(ctx, err); ok {
		return cerr
	}
	log.Printf("list servers failed: %v", err)
	// Do not pass err here: huma serializes extra error args into the response
	// body, which would leak internal (e.g. pgx) error detail to clients. Log it
	// server-side only, like the sibling handlers in servers.go.
	return huma.Error500InternalServerError("Failed to get registry list")
}

// GetServerDetailsError maps get-server-version failures; client disconnects must not log as 500s.
func GetServerDetailsError(ctx context.Context, err error, serverName, version string) error {
	if err == nil {
		return nil
	}
	if cerr, ok := clientClosedRequest(ctx, err); ok {
		return cerr
	}
	log.Printf("get server details (%q/%q) failed: %v", serverName, version, err)
	return huma.Error500InternalServerError("Failed to get server details")
}

// GetServerVersionsError maps get-server-versions failures; client disconnects must not log as 500s.
func GetServerVersionsError(ctx context.Context, err error, serverName string) error {
	if err == nil {
		return nil
	}
	if cerr, ok := clientClosedRequest(ctx, err); ok {
		return cerr
	}
	log.Printf("get server versions (%q) failed: %v", serverName, err)
	return huma.Error500InternalServerError("Failed to get server versions")
}
