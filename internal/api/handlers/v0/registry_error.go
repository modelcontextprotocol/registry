package v0

import (
	"context"
	"errors"
	"log"

	"github.com/danielgtaylor/huma/v2"
)

// RegistryError maps registry errors to HTTP responses; client disconnects
// must not log as 500s.
func RegistryError(ctx context.Context, err error, logPrefix string, userMsg string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return huma.NewError(499, "Client closed request", err)
	}
	log.Printf("%s failed: %v", logPrefix, err)
	return huma.Error500InternalServerError(userMsg)
}
