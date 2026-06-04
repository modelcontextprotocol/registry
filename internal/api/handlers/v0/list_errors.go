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
	return huma.Error500InternalServerError("Failed to get registry list", err)
}
