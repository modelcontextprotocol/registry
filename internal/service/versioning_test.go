package service_test

import (
	"testing"
	"time"

	"github.com/modelcontextprotocol/registry/internal/service"
)

func TestIsSemanticVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		// Valid semantic versions
		{"basic semver", "1.0.0", true},
		{"with patch", "1.2.3", true},
		{"with zeros", "0.0.0", true},
		{"large numbers", "100.200.300", true},
		{"with prerelease alpha", "1.0.0-alpha", true},
		{"with prerelease beta", "2.1.3-beta", true},
		{"with prerelease rc", "3.0.0-rc", true},
		{"with prerelease number", "1.0.0-1", true},
		{"with prerelease complex", "1.0.0-alpha.1", true},
		{"with prerelease dots", "1.0.0-beta.2.3", true},
		{"with hyphen in prerelease", "1.0.0-pre-release", false}, // Multiple hyphens not allowed in current implementation

		// Invalid semantic versions
		{"empty string", "", false},
		{"single number", "1", false},
		{"two parts only", "1.0", false},
		{"four parts", "1.0.0.0", false},
		{"with v prefix", "v1.0.0", false},
		{"date format", "2021.03.15", true}, // Technically valid semver format (3 numeric parts)
		{"date format with leading zeros", "2021.03.05", true}, // Also valid
		{"non-numeric major", "a.0.0", false},
		{"non-numeric minor", "1.b.0", false},
		{"non-numeric patch", "1.0.c", false},
		{"empty prerelease", "1.0.0-", false},
		{"multiple hyphens", "1.0.0-alpha-beta", false},
		{"special chars in prerelease", "1.0.0-alpha@1", false},
		{"snapshot", "snapshot", false},
		{"latest", "latest", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := service.IsSemanticVersion(tt.version); got != tt.want {
				t.Errorf("IsSemanticVersion(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestCompareSemanticVersions(t *testing.T) {
	tests := []struct {
		name     string
		version1 string
		version2 string
		want     int
	}{
		// Major version differences
		{"major less", "1.0.0", "2.0.0", -1},
		{"major greater", "2.0.0", "1.0.0", 1},
		{"major equal", "1.0.0", "1.0.0", 0},

		// Minor version differences
		{"minor less", "1.1.0", "1.2.0", -1},
		{"minor greater", "1.2.0", "1.1.0", 1},
		{"minor equal", "1.2.0", "1.2.0", 0},

		// Patch version differences
		{"patch less", "1.0.1", "1.0.2", -1},
		{"patch greater", "1.0.2", "1.0.1", 1},
		{"patch equal", "1.0.1", "1.0.1", 0},

		// Complex comparisons
		{"complex less", "1.9.9", "2.0.0", -1},
		{"complex greater", "2.0.0", "1.9.9", 1},
		{"complex mixed", "1.10.0", "1.2.0", 1},

		// Prerelease comparisons
		{"prerelease vs stable less", "1.0.0-alpha", "1.0.0", -1},
		{"stable vs prerelease greater", "1.0.0", "1.0.0-alpha", 1},
		{"prerelease alpha vs beta", "1.0.0-alpha", "1.0.0-beta", -1},
		{"prerelease beta vs alpha", "1.0.0-beta", "1.0.0-alpha", 1},
		{"prerelease same", "1.0.0-alpha", "1.0.0-alpha", 0},
		{"prerelease numeric", "1.0.0-1", "1.0.0-2", -1},
		{"prerelease complex", "1.0.0-alpha.1", "1.0.0-alpha.2", -1},
		{"prerelease rc vs alpha", "1.0.0-rc", "1.0.0-alpha", 1},

		// Edge cases
		{"zero versions", "0.0.0", "0.0.1", -1},
		{"large numbers", "100.200.300", "100.200.301", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := service.CompareSemanticVersions(tt.version1, tt.version2); got != tt.want {
				t.Errorf("CompareSemanticVersions(%q, %q) = %v, want %v", tt.version1, tt.version2, got, tt.want)
			}
		})
	}
}

func TestCompareVersions(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-1 * time.Hour)
	later := now.Add(1 * time.Hour)

	tests := []struct {
		name       string
		version1   string
		version2   string
		timestamp1 time.Time
		timestamp2 time.Time
		want       int
	}{
		// Both semantic versions
		{"both semver less", "1.0.0", "2.0.0", now, now, -1},
		{"both semver greater", "2.0.0", "1.0.0", now, now, 1},
		{"both semver equal", "1.0.0", "1.0.0", now, now, 0},
		{"both semver ignore timestamps", "1.0.0", "2.0.0", later, earlier, -1},

		// Neither semantic versions - use timestamps
		{"neither semver earlier", "snapshot", "latest", earlier, later, -1},
		{"neither semver later", "snapshot", "latest", later, earlier, 1},
		{"neither semver same time", "snapshot", "latest", now, now, 0},
		{"neither semver v-prefix", "v1.0", "v2.0", earlier, later, -1},

		// Mixed: one semver, one not - semver always wins
		{"semver vs non-semver", "1.0.0", "v2.0.0", now, now, 1},
		{"non-semver vs semver", "v2.0.0", "1.0.0", now, now, -1},
		{"semver vs snapshot", "0.0.1", "snapshot", earlier, later, 1},
		{"latest vs semver", "latest", "0.0.1", later, earlier, -1},
		{"semver prerelease vs non-semver", "1.0.0-alpha", "v5.0.0", now, now, 1},

		// Edge cases
		{"empty vs semver", "", "1.0.0", now, now, -1},
		{"semver vs empty", "1.0.0", "", now, now, 1},
		{"both empty", "", "", earlier, later, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := service.CompareVersions(tt.version1, tt.version2, tt.timestamp1, tt.timestamp2); got != tt.want {
				t.Errorf("CompareVersions(%q, %q, %v, %v) = %v, want %v", 
					tt.version1, tt.version2, tt.timestamp1, tt.timestamp2, got, tt.want)
			}
		})
	}
}