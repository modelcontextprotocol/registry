package registries

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	// defaultOCIRegistry is the default registry when none is specified
	defaultOCIRegistry = "docker.io"
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

// ParseOCIReference parses a canonical OCI image reference.
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

	result := &OCIReference{}

	// Split by @ to separate digest
	var mainPart string
	if idx := strings.Index(ref, "@"); idx >= 0 {
		mainPart = ref[:idx]
		result.Digest = ref[idx+1:]

		// Validate digest format
		if !strings.HasPrefix(result.Digest, "sha256:") {
			return nil, fmt.Errorf("invalid digest format: must start with 'sha256:'")
		}
		digestPattern := regexp.MustCompile(`^sha256:[a-fA-F0-9]{64}$`)
		if !digestPattern.MatchString(result.Digest) {
			return nil, fmt.Errorf("invalid digest format: must be sha256 followed by 64 hex characters")
		}
	} else {
		mainPart = ref
	}

	// Split by : to separate tag
	var pathPart string
	if idx := strings.LastIndex(mainPart, ":"); idx >= 0 {
		// Check if this looks like a registry with port (e.g., localhost:5000)
		// or a tag. If there's a / after the :, it's likely a port.
		if idx > 0 && !strings.Contains(mainPart[idx:], "/") {
			pathPart = mainPart[:idx]
			result.Tag = mainPart[idx+1:]
		} else {
			pathPart = mainPart
		}
	} else {
		pathPart = mainPart
	}

	// Parse the path (registry/namespace/image or namespace/image or image)
	parts := strings.Split(pathPart, "/")

	switch len(parts) {
	case 1:
		// Just image name: "postgres:16" -> docker.io/library/postgres:16
		result.Registry = defaultOCIRegistry
		result.Namespace = defaultOCINamespace
		result.Image = parts[0]

	case 2:
		// namespace/image: "owner/repo:tag" -> docker.io/owner/repo:tag
		// OR registry/image: "ghcr.io/image" -> ghcr.io/library/image
		// Heuristic: if first part looks like a domain (contains . or :), treat as registry
		if strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":") {
			result.Registry = parts[0]
			result.Namespace = defaultOCINamespace
			result.Image = parts[1]
		} else {
			result.Registry = defaultOCIRegistry
			result.Namespace = parts[0]
			result.Image = parts[1]
		}

	case 3:
		// registry/namespace/image: "ghcr.io/owner/repo:tag"
		result.Registry = parts[0]
		result.Namespace = parts[1]
		result.Image = parts[2]

	default:
		// More than 3 parts could be multi-level namespace (e.g., ghcr.io/org/team/repo)
		// Take first as registry, last as image, everything in between as namespace
		if len(parts) > 3 {
			result.Registry = parts[0]
			result.Namespace = strings.Join(parts[1:len(parts)-1], "/")
			result.Image = parts[len(parts)-1]
		} else {
			return nil, fmt.Errorf("invalid OCI reference format: %s", ref)
		}
	}

	// Validate we have either a tag or digest
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
