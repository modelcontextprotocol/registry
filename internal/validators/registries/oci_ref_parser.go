package registries

import (
	"fmt"
	"strings"

	"github.com/distribution/reference"
)

const (
	// defaultOCINamespace is the default namespace for official images
	defaultOCINamespace = "library"
)

// OCIReference represents a parsed OCI image reference
type OCIReference struct {
	Registry  string // e.g., "ghcr.io", "docker.io"
	Namespace string // e.g., "owner", "library"
	Image     string // e.g., "repo"
	Tag       string // e.g., "v1.0.0", "latest" (optional)
	Digest    string // e.g., "sha256:abc..." (optional)
}

// ParseOCIReference parses a canonical OCI image reference using github.com/distribution/reference.
// Supported formats:
//   - registry/namespace/image:tag
//   - registry/namespace/image@digest
//   - registry/namespace/image:tag@digest
//   - namespace/image:tag (defaults to docker.io)
//   - image:tag (defaults to docker.io/library)
func ParseOCIReference(ref string) (*OCIReference, error) {
	if ref == "" {
		return nil, fmt.Errorf("OCI reference cannot be empty")
	}

	// Parse using distribution/reference - normalizes short forms to canonical
	named, err := reference.ParseNormalizedNamed(ref)
	if err != nil {
		return nil, fmt.Errorf("invalid OCI reference format: %w", err)
	}

	result := &OCIReference{}

	// Extract registry (domain)
	result.Registry = reference.Domain(named)

	// Extract path (namespace/image or just image)
	path := reference.Path(named)
	parts := strings.Split(path, "/")

	// Parse namespace and image from path
	if len(parts) == 1 {
		// Single part: library/image (docker.io default namespace)
		result.Namespace = defaultOCINamespace
		result.Image = parts[0]
	} else {
		// Multiple parts: namespace/image or org/team/image
		result.Namespace = strings.Join(parts[:len(parts)-1], "/")
		result.Image = parts[len(parts)-1]
	}

	// Extract tag if present
	if tagged, ok := named.(reference.Tagged); ok {
		result.Tag = tagged.Tag()
	}

	// Extract digest if present
	if digested, ok := named.(reference.Digested); ok {
		result.Digest = digested.Digest().String()
	}

	// Validate we have either a tag or digest (required for MCP registry)
	if result.Tag == "" && result.Digest == "" {
		return nil, fmt.Errorf("OCI reference must include either a tag or digest: %s", ref)
	}

	// Default tag to "latest" if only digest is provided (for display purposes)
	// Note: when pulling by digest, the tag is ignored by registries
	if result.Tag == "" && result.Digest != "" {
		result.Tag = "latest"
	}

	return result, nil
}

// String returns the canonical string representation of the OCI reference
func (r *OCIReference) String() string {
	var sb strings.Builder

	sb.WriteString(r.Registry)
	sb.WriteString("/")
	sb.WriteString(r.Namespace)
	sb.WriteString("/")
	sb.WriteString(r.Image)

	if r.Tag != "" {
		sb.WriteString(":")
		sb.WriteString(r.Tag)
	}

	if r.Digest != "" {
		sb.WriteString("@")
		sb.WriteString(r.Digest)
	}

	return sb.String()
}

// GetRegistryBaseURL returns the full registry URL (e.g., "https://docker.io" or "https://ghcr.io")
func (r *OCIReference) GetRegistryBaseURL() string {
	switch r.Registry {
	case "docker.io", "registry.docker.io", "index.docker.io":
		return "https://docker.io"
	case "ghcr.io":
		return "https://ghcr.io"
	default:
		return "https://" + r.Registry
	}
}
