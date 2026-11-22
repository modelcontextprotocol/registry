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

// PackageRelease asks, about a package a registry entry already names,
// whether the release is still in its registry. For PyPI, NuGet and crates.io
// it asks at the addresses a publish asks, so that a release that was found
// then is looked for the same way now; what the release says about its owner
// is not part of the answer. For an image it asks only whether the image is
// there. npm is not asked here: the sweep reads its record itself.
func PackageRelease(ctx context.Context, serverName string, pkg malware.Package) malware.Release {
	var err error
	switch pkg.RegistryType {
	case model.RegistryTypePyPI, model.RegistryTypeNuGet, model.RegistryTypeCargo:
		err = ValidatePackage(ctx, model.Package{RegistryType: pkg.RegistryType, Identifier: pkg.Identifier, Version: pkg.Version}, serverName)
	case model.RegistryTypeOCI:
		err = registries.OCIImageExists(ctx, pkg.Identifier)
	default:
		return malware.ReleaseUnreadable
	}
	if err == nil {
		return malware.ReleasePresent
	}
	switch registries.KindOf(err) {
	case registries.FailureOwnership:
		return malware.ReleasePresent
	case registries.FailureNotFound:
		return malware.ReleaseMissing
	case registries.FailureTransient:
		return malware.ReleaseUnknown
	case registries.FailureOther:
	}
	return malware.ReleaseUnreadable
}
