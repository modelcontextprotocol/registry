package registries

import (
	"context"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

func ValidateMCPB(_ context.Context, _ model.Package, _ string) error {
	// MCPB packages do not currently support ownership validation
	return nil
}
