package validators

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/registry/internal/validators/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// The largest of 262,556 catalog versions measured in November 2026 needed nine checks.
const maxPackageOwnershipChecks = 16

// ownershipCheckIndices returns the first index of each distinct check in request
// order. Keep the key in sync with all fields read by ValidatePackage and its
// registry validators, including fields they reject when present. The server
// name is shared by all entries in this request.
func ownershipCheckIndices(packages []model.Package) ([]int, error) {
	type ownershipKey struct {
		registryType, registryBaseURL, identifier, version, fileSHA256 string
	}
	seen := make(map[ownershipKey]struct{})
	var indices []int
	for i, pkg := range packages {
		key := ownershipKey{
			registryType:    pkg.RegistryType,
			registryBaseURL: pkg.RegistryBaseURL,
			identifier:      pkg.Identifier,
			version:         pkg.Version,
			fileSHA256:      pkg.FileSHA256,
		}
		if _, ok := seen[key]; ok {
			continue
		}
		if len(indices) == maxPackageOwnershipChecks {
			return nil, fmt.Errorf("package %d (%s) exceeds the limit of %d distinct package ownership checks per server version",
				i, pkg.Identifier, maxPackageOwnershipChecks)
		}
		seen[key] = struct{}{}
		indices = append(indices, i)
	}
	return indices, nil
}

// ValidatePackage validates that the package referenced in the server configuration is:
// 1. allowed on the official registry (based on registry base url); and
// 2. owned by the publisher, by checking for a matching server name in the package metadata
func ValidatePackage(ctx context.Context, pkg model.Package, serverName string) error {
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
	case model.RegistryTypeCargo:
		return registries.ValidateCargo(ctx, pkg, serverName)
	case model.RegistryTypeGo:
		return registries.ValidateGo(ctx, pkg, serverName)
	default:
		return fmt.Errorf("unsupported registry type: %s", pkg.RegistryType)
	}
}
