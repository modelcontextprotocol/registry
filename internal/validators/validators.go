package validators

import (
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/registry/internal/model"
)

// ServerValidator validates server details
type ServerValidator struct {
	*RepositoryValidator // Embedded RepositoryValidator for repository validation
}

// Validate checks if the server details are valid
func (v *ServerValidator) Validate(obj *model.ServerDetail) error {
	if err := v.RepositoryValidator.Validate(&obj.Repository); err != nil {
		return err
	}
	return nil
}

// NewServerValidator creates a new ServerValidator instance
func NewServerValidator() *ServerValidator {
	return &ServerValidator{
		RepositoryValidator: NewRepositoryValidator(),
	}
}

// RepositoryValidator validates repository details
type RepositoryValidator struct {
	validSources map[RepositorySource]bool
}

// Validate checks if the repository details are valid
func (rv *RepositoryValidator) Validate(obj *model.Repository) error {
	// Skip validation for empty repository (optional field)
	if obj.URL == "" && obj.Source == "" {
		return nil
	}

	// validate the repository source
	repoSource := RepositorySource(obj.Source)
	if !IsValidRepositoryURL(repoSource, obj.URL) {
		return fmt.Errorf("%w: %s", ErrInvalidRepositoryURL, obj.URL)
	}

	return nil
}

// NewRepositoryValidator creates a new RepositoryValidator instance
func NewRepositoryValidator() *RepositoryValidator {
	return &RepositoryValidator{
		validSources: map[RepositorySource]bool{SourceGitHub: true, SourceGitLab: true},
	}
}

// PackageValidator validates package details
type PackageValidator struct{}

// Validate checks if the package details are valid
func (pv *PackageValidator) Validate(obj *model.Package) error {
	if !HasNoSpaces(obj.Identifier) {
		return ErrPackageNameHasSpaces
	}

	// Validate runtime arguments
	argumentValidator := NewArgumentValidator()
	for _, arg := range obj.RuntimeArguments {
		if err := argumentValidator.Validate(&arg); err != nil {
			return fmt.Errorf("invalid runtime argument: %w", err)
		}
	}

	// Validate package arguments
	for _, arg := range obj.PackageArguments {
		if err := argumentValidator.Validate(&arg); err != nil {
			return fmt.Errorf("invalid package argument: %w", err)
		}
	}

	return nil
}

// NewPackageValidator creates a new PackageValidator instance
func NewPackageValidator() *PackageValidator {
	return &PackageValidator{}
}

// ArgumentValidator validates argument details
type ArgumentValidator struct{}

// Validate checks if the argument details are valid
func (av *ArgumentValidator) Validate(obj *model.Argument) error {
	if obj.Type == model.ArgumentTypeNamed {
		// Validate named argument name format
		if err := av.validateNamedArgumentName(obj.Name); err != nil {
			return err
		}

		// Validate value and default don't start with the name
		if err := av.validateValueFields(obj.Name, obj.Value, obj.Default); err != nil {
			return err
		}
	}
	return nil
}

func (av *ArgumentValidator) validateNamedArgumentName(name string) error {
	// Check if name is required for named arguments
	if name == "" {
		return ErrNamedArgumentNameRequired
	}

	// Check for invalid characters that suggest embedded values or descriptions
	// Valid: "--directory", "--port", "-v", "config", "verbose"
	// Invalid: "--directory <absolute_path_to_adfin_mcp_folder>", "--port 8080"
	if strings.Contains(name, "<") || strings.Contains(name, ">") ||
		strings.Contains(name, " ") || strings.Contains(name, "$") {
		return fmt.Errorf("%w: %s", ErrInvalidNamedArgumentName, name)
	}

	return nil
}

func (av *ArgumentValidator) validateValueFields(name, value, defaultValue string) error {
	// Check if value starts with the argument name (using startsWith, not contains)
	if value != "" && strings.HasPrefix(value, name) {
		return fmt.Errorf("%w: value starts with argument name '%s': %s", ErrArgumentValueStartsWithName, name, value)
	}

	if defaultValue != "" && strings.HasPrefix(defaultValue, name) {
		return fmt.Errorf("%w: default starts with argument name '%s': %s", ErrArgumentDefaultStartsWithName, name, defaultValue)
	}

	return nil
}

// NewArgumentValidator creates a new ArgumentValidator instance
func NewArgumentValidator() *ArgumentValidator {
	return &ArgumentValidator{}
}

// RemoteValidator validates remote connection details
type RemoteValidator struct{}

// Validate checks if the remote connection details are valid
func (rv *RemoteValidator) Validate(obj *model.Remote) error {
	if !IsValidURL(obj.URL) {
		return fmt.Errorf("%w: %s", ErrInvalidRemoteURL, obj.URL)
	}
	return nil
}

// NewRemoteValidator creates a new RemoteValidator instance
func NewRemoteValidator() *RemoteValidator {
	return &RemoteValidator{}
}

// ObjectValidator aggregates multiple validators for different object types
// This allows for a single entry point to validate complex objects that may contain multiple fields
// that need validation.
type ObjectValidator struct {
	ServerValidator   *ServerValidator
	PackageValidator  *PackageValidator
	RemoteValidator   *RemoteValidator
	ArgumentValidator *ArgumentValidator
}

func NewObjectValidator() *ObjectValidator {
	return &ObjectValidator{
		ServerValidator:   NewServerValidator(),
		PackageValidator:  NewPackageValidator(),
		RemoteValidator:   NewRemoteValidator(),
		ArgumentValidator: NewArgumentValidator(),
	}
}

func (ov *ObjectValidator) Validate(obj *model.ServerDetail) error {
	if err := ov.ServerValidator.Validate(obj); err != nil {
		return err
	}

	for _, pkg := range obj.Packages {
		if err := ov.PackageValidator.Validate(&pkg); err != nil {
			return err
		}
	}

	for _, remote := range obj.Remotes {
		if err := ov.RemoteValidator.Validate(&remote); err != nil {
			return err
		}
	}
	return nil
}
