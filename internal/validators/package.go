package validators

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/registry/internal/validators/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// ValidatePackageOwnership validates that the package referenced in the server configuration
// is owned by the publisher, by checking for a matching server name in the package metadata.
func ValidatePackageOwnership(ctx context.Context, pkg model.Package, serverName string) error {
	switch pkg.RegistryType {
	case model.RegistryTypeNPM:
		return registries.ValidateNPM(ctx, pkg, serverName)
	case model.RegistryTypePyPI:
		return registries.ValidatePyPI(ctx, pkg, serverName)
	case model.RegistryTypeNuGet:
		return registries.ValidateNuGet(ctx, pkg, serverName)
	case model.RegistryTypeOCI:
		return registries.ValidateOCI(ctx, pkg, serverName)
	case model.RegistryTypeMCPB:
		return registries.ValidateMCPB(ctx, pkg, serverName)
	default:
		return fmt.Errorf("unsupported registry type: %s", pkg.RegistryType)
	}
}
