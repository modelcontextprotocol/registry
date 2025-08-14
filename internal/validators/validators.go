package validators

import (
	"fmt"

	"github.com/modelcontextprotocol/registry/internal/model"
)

// ServerValidator validates server details
type ServerValidator struct {
	*RespositoryValidator // Embedded RespositoryValidator for repository validation
}

// Validate checks if the server details are valid
func (v *ServerValidator) Validate(obj *model.ServerDetail) error {
	if obj.Name == "" {
		return ErrNameRequired
	}

	// Add format validation according to https://github.com/modelcontextprotocol/modelcontextprotocol/issues/1086 in the next PR
	if len(obj.Name) > MaxLengthForServerName {
		return ErrServerNameTooLong
	}

	// Version is required
	if obj.VersionDetail.Version == "" {
		return ErrVersionRequired
	}

	if len(obj.Description) > MaxLengthForDescription {
		return ErrDescriptionTooLong
	}

	if err := v.RespositoryValidator.Validate(&obj.Repository); err != nil {
		return err
	}
	return nil
}

// NewServerValidator creates a new ServerValidator instance
func NewServerValidator() *ServerValidator {
	return &ServerValidator{
		RespositoryValidator: NewRepositoryValidator(),
	}
}

// RepositoryValidator validates repository details
type RespositoryValidator struct {
	validSources map[RepositorySource]bool
}

// Validate checks if the repository details are valid
func (rv *RespositoryValidator) Validate(obj *model.Repository) error {
	// empty repository object, this check is needed because we get empty repo object (if not present in request) everytime we unmarshal the request json
	if len(obj.URL) == 0 && len(obj.Source) == 0 && len(obj.ID) == 0 {
		return nil
	}
	// validate the repository URL
	repoSource := RepositorySource(obj.Source)
	if _, ok := rv.validSources[repoSource]; !ok {
		return fmt.Errorf("%w: %s", ErrInvalidRepositorySource, obj.Source)
	}
	if !IsValidRepositoryURL(repoSource, obj.URL) {
		return fmt.Errorf("%w: %s", ErrInvalidRepositoryURL, obj.URL)
	}

	// Add validator for repo ID after confirming ID type
	return nil
}

// NewRepositoryValidator creates a new RespositoryValidator instance
func NewRepositoryValidator() *RespositoryValidator {
	return &RespositoryValidator{
		validSources: map[RepositorySource]bool{SourceGitHub: true, SourceGitLab: true},
	}
}

// PackageValidator validates package details
type PackageValidator struct{}

// Validate checks if the package details are valid
func (pv *PackageValidator) Validate(obj *model.Package) error {
	if len(obj.Name) > MaxLengthForPackageName {
		return ErrPackageNameTooLong
	}

	if !HasNoSpaces(obj.Name) {
		return ErrPackageNameHasSpaces
	}

	return nil
}

// NewPackageValidator creates a new PackageValidator instance
func NewPackageValidator() *PackageValidator {
	return &PackageValidator{}
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
	ServerValidator  *ServerValidator
	PackageValidator *PackageValidator
	RemoteValidator  *RemoteValidator
}

func NewObjectValidator() *ObjectValidator {
	return &ObjectValidator{
		ServerValidator:  NewServerValidator(),
		PackageValidator: NewPackageValidator(),
		RemoteValidator:  NewRemoteValidator(),
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
