package validators

import "errors"

// Error messages for validation
var (
	// Server validation errors
	ErrNameRequired       = errors.New("name is required")
	ErrServerNameTooLong  = errors.New("server name is too long")
	ErrVersionRequired    = errors.New("version is required")
	ErrDescriptionTooLong = errors.New("description is too long")

	// Repository validation errors
	ErrInvalidRepositorySource = errors.New("invalid repository source")
	ErrInvalidRepositoryURL    = errors.New("invalid repository URL")

	// Package validation errors
	ErrPackageNameTooLong   = errors.New("package name is too long")
	ErrPackageNameHasSpaces = errors.New("package name cannot contain spaces")

	// Remote validation errors
	ErrInvalidRemoteURL = errors.New("invalid remote URL")
)

// Constants for validation limits
const (
	MaxLengthForServerName  = 255
	MaxLengthForDescription = 1000
	MaxLengthForPackageName = 255
)

// RepositorySource represents valid repository sources
type RepositorySource string

const (
	SourceGitHub RepositorySource = "github"
	SourceGitLab RepositorySource = "gitlab"
)
