package validators

import "errors"

// Error messages for validation
var (
	// Repository validation errors
	ErrInvalidRepositoryURL = errors.New("invalid repository URL")

	// Package validation errors
	ErrPackageNameHasSpaces = errors.New("package name cannot contain spaces")

	// Remote validation errors
	ErrInvalidRemoteURL = errors.New("invalid remote URL")
)

// RepositorySource represents valid repository sources
type RepositorySource string

const (
	SourceGitHub RepositorySource = "github"
	SourceGitLab RepositorySource = "gitlab"
)
