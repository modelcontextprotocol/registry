package registries_test

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func generateRandomPackageName() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to a static name if crypto/rand fails
		return "nonexistent-pkg-fallback"
	}
	return fmt.Sprintf("nonexistent-pkg-%s", hex.EncodeToString(bytes))
}

func generateRandomNuGetPackageName() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "NonExistent.Package.Fallback"
	}
	return fmt.Sprintf("NonExistent.Package.%s", hex.EncodeToString(bytes)[:16])
}
