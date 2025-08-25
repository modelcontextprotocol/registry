package service

import (
	"strconv"
	"strings"
	"time"
)

// IsSemanticVersion checks if a version string follows semantic versioning format
func IsSemanticVersion(version string) bool {
	// Basic regex pattern for semantic versioning (simplified)
	// Allows: major.minor.patch with optional prerelease (e.g., 1.0.0-alpha.1)
	parts := strings.Split(version, "-")
	if len(parts) > 2 {
		return false
	}

	// Check main version part (major.minor.patch)
	versionParts := strings.Split(parts[0], ".")
	if len(versionParts) != 3 {
		return false
	}

	for _, part := range versionParts {
		if _, err := strconv.Atoi(part); err != nil {
			return false
		}
	}

	// If there's a prerelease part, it can contain alphanumeric characters and dots
	if len(parts) == 2 {
		prerelease := parts[1]
		if prerelease == "" {
			return false
		}
		// Basic validation for prerelease - allow letters, numbers, dots
		for _, r := range prerelease {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '.' && r != '-' {
				return false
			}
		}
	}

	return true
}

// comparePrereleaseVersions compares two prerelease version strings according to semver spec
// Returns:
//
//	-1 if prerelease1 < prerelease2
//	 0 if prerelease1 == prerelease2
//	+1 if prerelease1 > prerelease2
func comparePrereleaseVersions(prerelease1, prerelease2 string) int {
	// Split prerelease identifiers by dots
	ids1 := strings.Split(prerelease1, ".")
	ids2 := strings.Split(prerelease2, ".")

	// Compare each identifier from left to right
	minLen := len(ids1)
	if len(ids2) < minLen {
		minLen = len(ids2)
	}

	for i := 0; i < minLen; i++ {
		id1 := ids1[i]
		id2 := ids2[i]

		// Check if both are numeric
		num1, err1 := strconv.Atoi(id1)
		num2, err2 := strconv.Atoi(id2)

		switch {
		case err1 == nil && err2 == nil:
			// Both are numeric, compare as integers
			if num1 < num2 {
				return -1
			} else if num1 > num2 {
				return 1
			}
		case err1 == nil && err2 != nil:
			// id1 is numeric, id2 is not - numeric has lower precedence
			return -1
		case err1 != nil && err2 == nil:
			// id1 is not numeric, id2 is - numeric has lower precedence
			return 1
		default:
			// Both are non-numeric, compare lexicographically
			if id1 < id2 {
				return -1
			} else if id1 > id2 {
				return 1
			}
		}
	}

	// If all compared identifiers are equal, shorter list has lower precedence
	if len(ids1) < len(ids2) {
		return -1
	} else if len(ids1) > len(ids2) {
		return 1
	}

	return 0
}

// CompareSemanticVersions compares two semantic version strings
// Returns:
//
//	-1 if version1 < version2
//	 0 if version1 == version2
//	+1 if version1 > version2
func CompareSemanticVersions(version1, version2 string) int {
	// Parse version parts (main version and prerelease)
	parts1 := strings.Split(version1, "-")
	parts2 := strings.Split(version2, "-")

	mainParts1 := strings.Split(parts1[0], ".")
	mainParts2 := strings.Split(parts2[0], ".")

	// Compare major, minor, patch
	for i := 0; i < 3; i++ {
		num1, _ := strconv.Atoi(mainParts1[i])
		num2, _ := strconv.Atoi(mainParts2[i])

		if num1 < num2 {
			return -1
		} else if num1 > num2 {
			return 1
		}
	}

	// If main versions are equal, compare prerelease
	hasPrerelease1 := len(parts1) > 1
	hasPrerelease2 := len(parts2) > 1

	// Version without prerelease is higher than with prerelease
	if !hasPrerelease1 && hasPrerelease2 {
		return 1
	}
	if hasPrerelease1 && !hasPrerelease2 {
		return -1
	}

	// Both have prerelease, compare according to semver spec
	if hasPrerelease1 && hasPrerelease2 {
		return comparePrereleaseVersions(parts1[1], parts2[1])
	}

	return 0
}

// CompareVersions implements the versioning strategy agreed upon in the discussion:
// 1. If both versions are valid semver, use semantic version comparison
// 2. If neither are valid semver, use publication timestamp (return 0 to indicate equal for sorting)
// 3. If one is semver and one is not, the semver version is always considered higher
func CompareVersions(version1, version2 string, timestamp1, timestamp2 time.Time) int {
	isSemver1 := IsSemanticVersion(version1)
	isSemver2 := IsSemanticVersion(version2)

	if isSemver1 && isSemver2 {
		// Both are semver - use semantic comparison
		return CompareSemanticVersions(version1, version2)
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