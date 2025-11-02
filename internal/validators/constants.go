package validators

import "errors"

// Error messages for validation
var (
	// Repository validation errors
	ErrInvalidRepositoryURL = errors.New("invalid repository URL")
	ErrInvalidSubfolderPath = errors.New("invalid subfolder path")

	// Package validation errors
	ErrPackageNameHasSpaces  = errors.New("package name cannot contain spaces")
	ErrReservedVersionString = errors.New("version string 'latest' is reserved and cannot be used")
	ErrVersionLooksLikeRange = errors.New("version must be a specific version, not a range")

	// Remote validation errors
	ErrInvalidRemoteURL = errors.New("invalid remote URL")

	// Registry validation errors
	ErrUnsupportedRegistryBaseURL   = errors.New("unsupported registry base URL")
	ErrMismatchedRegistryTypeAndURL = errors.New("registry type and base URL do not match")

	// Argument validation errors
	ErrNamedArgumentNameRequired     = errors.New("named argument name is required")
	ErrInvalidNamedArgumentName      = errors.New("invalid named argument name format")
	ErrArgumentValueStartsWithName   = errors.New("argument value cannot start with the argument name")
	ErrArgumentDefaultStartsWithName = errors.New("argument default cannot start with the argument name")

	// Server name validation errors
	ErrMultipleSlashesInServerName = errors.New("server name cannot contain multiple slashes")
	ErrInvalidServerNameFormat     = errors.New("server name format is invalid")
)

// RepositorySource represents valid repository sources
type RepositorySource string

const (
	SourceGitHub RepositorySource = "github"
	SourceGitLab RepositorySource = "gitlab"
)

const (
	SchemeHTTPS = "https"
)
