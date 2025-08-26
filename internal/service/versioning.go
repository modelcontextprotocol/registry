package service

import (
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// IsSemanticVersion checks if a version string follows semantic versioning format
// Uses the official golang.org/x/mod/semver package for validation
// Requires exactly three parts: major.minor.patch (optionally with prerelease/build)
func IsSemanticVersion(version string) bool {
	// The semver package requires a "v" prefix, so add it for validation
	versionWithV := ensureVPrefix(version)
	if !semver.IsValid(versionWithV) {
		return false
	}

	// Additional validation: require exactly three parts (major.minor.patch)
	// Strip the v prefix and any prerelease/build metadata for counting parts
	// This ensures semver compliance, because the default go module accepts invalid semvers :/
	// (See https://pkg.go.dev/golang.org/x/mod/semver)
	versionCore := strings.TrimPrefix(versionWithV, "v")
	if idx := strings.Index(versionCore, "-"); idx != -1 {
		versionCore = versionCore[:idx]
	}
	if idx := strings.Index(versionCore, "+"); idx != -1 {
		versionCore = versionCore[:idx]
	}

	parts := strings.Split(versionCore, ".")
	return len(parts) == 3
}

// ensureVPrefix adds a "v" prefix if not present
func ensureVPrefix(version string) string {
	if !strings.HasPrefix(version, "v") {
		return "v" + version
	}
	return version
}

// compareSemanticVersions compares two semantic version strings
// Uses the official golang.org/x/mod/semver package for comparison
// Returns:
//
//	-1 if version1 < version2
//	 0 if version1 == version2
//	+1 if version1 > version2
func compareSemanticVersions(version1 string, version2 string) int {
	// The semver package requires a "v" prefix, so add it for comparison
	v1 := ensureVPrefix(version1)
	v2 := ensureVPrefix(version2)
	return semver.Compare(v1, v2)
}

// CompareVersions implements the versioning strategy agreed upon in the discussion:
// 1. If both versions are valid semver, use semantic version comparison
// 2. If neither are valid semver, use publication timestamp (return 0 to indicate equal for sorting)
// 3. If one is semver and one is not, the semver version is always considered higher
func CompareVersions(version1 string, version2 string, timestamp1 time.Time, timestamp2 time.Time) int {
	isSemver1 := IsSemanticVersion(version1)
	isSemver2 := IsSemanticVersion(version2)

	if isSemver1 && isSemver2 {
		// Both are semver - use semantic comparison
		return compareSemanticVersions(version1, version2)
	}

	if !isSemver1 && !isSemver2 {
		// Neither are semver - use timestamp comparison
		if timestamp1.Before(timestamp2) {
			return -1
		} else if timestamp1.After(timestamp2) {
			return 1
		}
		return 0
	}

	// One is semver, one is not - semver is always higher
	if isSemver1 && !isSemver2 {
		return 1
	}
	return -1
}
