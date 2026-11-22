package validators

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/registry/internal/malware"
	"github.com/modelcontextprotocol/registry/internal/validators/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

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
	default:
		return fmt.Errorf("unsupported registry type: %s", pkg.RegistryType)
	}
}

// PackageOwnership asks what ValidatePackage asks on a publish, about a
// package a registry entry already names, and returns what the answer means
// for the daily sweep.
func PackageOwnership(ctx context.Context, serverName string, pkg malware.Package) malware.Ownership {
	err := ValidatePackage(ctx, model.Package{RegistryType: pkg.RegistryType, Identifier: pkg.Identifier, Version: pkg.Version}, serverName)
	if err == nil {
		return malware.OwnershipHeld
	}
	switch registries.KindOf(err) {
	case registries.FailureNotFound:
		return malware.OwnershipReleaseMissing
	case registries.FailureOwnership:
		return malware.OwnershipMismatch
	case registries.FailureTransient:
		return malware.OwnershipUnknown
	case registries.FailureOther:
	}
	return malware.OwnershipUncheckable
}
