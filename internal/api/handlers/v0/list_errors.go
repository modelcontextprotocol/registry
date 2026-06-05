package v0

import (
	"context"
	"errors"
	"log"

	"github.com/danielgtaylor/huma/v2"
)

// ListServersError maps ListServers failures; client disconnects must not log as 500s.
func ListServersError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return huma.NewError(499, "Client closed request", err)
	}
	log.Printf("list servers failed: %v", err)
	// Do not pass err here: huma serializes extra error args into the response
	// body, which would leak internal (e.g. pgx) error detail to clients. Log it
	// server-side only, like the sibling handlers in servers.go.
	return huma.Error500InternalServerError("Failed to get registry list")
}
