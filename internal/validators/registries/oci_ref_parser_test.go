package registries_test

import (
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators/registries"
)

func TestParseOCIReference(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      *registries.OCIReference
		wantError bool
	}{
		{
			name:  "full reference with tag",
			input: "ghcr.io/owner/repo:v1.0.0",
			want: &registries.OCIReference{
				Registry:  "ghcr.io",
				Namespace: "owner",
				Image:     "repo",
				Tag:       "v1.0.0",
				Digest:    "",
			},
		},
		{
			name:  "full reference with digest only",
			input: "ghcr.io/owner/repo@sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			want: &registries.OCIReference{
				Registry:  "ghcr.io",
				Namespace: "owner",
				Image:     "repo",
				Tag:       "latest", // Default when only digest provided
				Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			},
		},
		{
			name:  "full reference with tag and digest",
			input: "ghcr.io/owner/repo:v1.0.0@sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			want: &registries.OCIReference{
				Registry:  "ghcr.io",
				Namespace: "owner",
				Image:     "repo",
				Tag:       "v1.0.0",
				Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			},
		},
		{
			name:  "docker.io short form",
			input: "owner/repo:latest",
			want: &registries.OCIReference{
				Registry:  "docker.io",
				Namespace: "owner",
				Image:     "repo",
				Tag:       "latest",
				Digest:    "",
			},
		},
		{
			name:  "docker.io library image",
			input: "postgres:16",
			want: &registries.OCIReference{
				Registry:  "docker.io",
				Namespace: "library",
				Image:     "postgres",
				Tag:       "16",
				Digest:    "",
			},
		},
		{
			name:  "docker.io library image with digest",
			input: "postgres@sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			want: &registries.OCIReference{
				Registry:  "docker.io",
				Namespace: "library",
				Image:     "postgres",
				Tag:       "latest",
				Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			},
		},
		{
			name:  "multi-level namespace",
			input: "ghcr.io/org/team/repo:v2.0.0",
			want: &registries.OCIReference{
				Registry:  "ghcr.io",
				Namespace: "org/team",
				Image:     "repo",
				Tag:       "v2.0.0",
				Digest:    "",
			},
		},
		{
			name:      "empty reference",
			input:     "",
			wantError: true,
		},
		{
			name:      "no tag or digest",
			input:     "ghcr.io/owner/repo",
			wantError: true,
		},
		{
			name:      "invalid digest format",
			input:     "ghcr.io/owner/repo@md5:abc123",
			wantError: true,
		},
		{
			name:      "invalid digest length",
			input:     "ghcr.io/owner/repo@sha256:abc123",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := registries.ParseOCIReference(tt.input)

			if tt.wantError {
				if err == nil {
					t.Errorf("registries.ParseOCIReference() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("registries.ParseOCIReference() unexpected error: %v", err)
				return
			}

			if got.Registry != tt.want.Registry {
				t.Errorf("Registry = %v, want %v", got.Registry, tt.want.Registry)
			}
			if got.Namespace != tt.want.Namespace {
				t.Errorf("Namespace = %v, want %v", got.Namespace, tt.want.Namespace)
			}
			if got.Image != tt.want.Image {
				t.Errorf("Image = %v, want %v", got.Image, tt.want.Image)
			}
			if got.Tag != tt.want.Tag {
				t.Errorf("Tag = %v, want %v", got.Tag, tt.want.Tag)
			}
			if got.Digest != tt.want.Digest {
				t.Errorf("Digest = %v, want %v", got.Digest, tt.want.Digest)
			}
		})
	}
}

func TestOCIReference_String(t *testing.T) {
	tests := []struct {
		name string
		ref  *registries.OCIReference
		want string
	}{
		{
			name: "full reference with tag",
			ref: &registries.OCIReference{
				Registry:  "ghcr.io",
				Namespace: "owner",
				Image:     "repo",
				Tag:       "v1.0.0",
			},
			want: "ghcr.io/owner/repo:v1.0.0",
		},
		{
			name: "full reference with digest",
			ref: &registries.OCIReference{
				Registry:  "ghcr.io",
				Namespace: "owner",
				Image:     "repo",
				Tag:       "latest",
				Digest:    "sha256:abc123",
			},
			want: "ghcr.io/owner/repo:latest@sha256:abc123",
		},
		{
			name: "docker.io library image",
			ref: &registries.OCIReference{
				Registry:  "docker.io",
				Namespace: "library",
				Image:     "postgres",
				Tag:       "16",
			},
			want: "docker.io/library/postgres:16",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ref.String(); got != tt.want {
				t.Errorf("OCIReference.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOCIReference_GetRegistryBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		registry string
		want     string
	}{
		{
			name:     "docker.io",
			registry: "docker.io",
			want:     "https://docker.io",
		},
		{
			name:     "ghcr.io",
			registry: "ghcr.io",
			want:     "https://ghcr.io",
		},
		{
			name:     "custom registry",
			registry: "my-registry.com",
			want:     "https://my-registry.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref := &registries.OCIReference{Registry: tt.registry}
			if got := ref.GetRegistryBaseURL(); got != tt.want {
				t.Errorf("GetRegistryBaseURL() = %v, want %v", got, tt.want)
			}
		})
	}
}
