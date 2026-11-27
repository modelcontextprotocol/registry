package validators

import (
	"encoding/json"
	"fmt"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

const (
	maxServerJSONBytes   = 128 * 1024
	maxNestedStringBytes = 4 * 1024
)

// Keep the machine-readable location alongside the error returned on publish and edit.
type submissionSizeError struct {
	path string
	error
}

func (e *submissionSizeError) Unwrap() error { return e.error }

func (e *submissionSizeError) under(path string) *submissionSizeError {
	if e.path != "" {
		e.path = path + "." + e.path
	} else {
		e.path = path
	}
	e.error = fmt.Errorf("%s: %w", path, e.error)
	return e
}

func validateSubmissionSize(req *apiv0.ServerJSON) *submissionSizeError {
	if err := validatePublisherExtensions(req); err != nil {
		return &submissionSizeError{path: `_meta["io.modelcontextprotocol.registry/publisher-provided"]`, error: err}
	}
	// Use the same typed encoding as database.CreateServer and UpdateServer.
	encoded, err := json.Marshal(req)
	if err != nil {
		return &submissionSizeError{path: "$", error: fmt.Errorf("failed to marshal server.json: %w", err)}
	}
	if len(encoded) > maxServerJSONBytes {
		return &submissionSizeError{path: "$", error: fmt.Errorf("serialized server.json is %d bytes (limit %d)", len(encoded), maxServerJSONBytes)}
	}
	// JSON escaping can only increase the byte length of a valid UTF-8 string.
	if len(encoded) <= maxNestedStringBytes {
		return nil
	}
	if err := checkElements("packages", req.Packages, checkPackageSize); err != nil {
		return err
	}
	return checkElements("remotes", req.Remotes, checkTransportSize)
}

// Build the location on error only. Never include submitted values in size errors.
func checkElements[T any](field string, elements []T, check func(*T) *submissionSizeError) *submissionSizeError {
	for i := range elements {
		if err := check(&elements[i]); err != nil {
			return err.under(fmt.Sprintf("%s[%d]", field, i))
		}
	}
	return nil
}

type stringField struct{ name, value string }

func checkFields(fields ...stringField) *submissionSizeError {
	for _, f := range fields {
		if len(f.value) > maxNestedStringBytes {
			return &submissionSizeError{path: f.name, error: fmt.Errorf("%s is %d bytes (limit %d)", f.name, len(f.value), maxNestedStringBytes)}
		}
	}
	return nil
}

func checkPackageSize(p *model.Package) *submissionSizeError {
	if err := checkFields(stringField{"registryType", p.RegistryType}, stringField{"registryBaseUrl", p.RegistryBaseURL},
		stringField{"identifier", p.Identifier}, stringField{"version", p.Version},
		stringField{"fileSha256", p.FileSHA256}, stringField{"runtimeHint", p.RunTimeHint}); err != nil {
		return err
	}
	if err := checkTransportSize(&p.Transport); err != nil {
		return err.under("transport")
	}
	if err := checkElements("runtimeArguments", p.RuntimeArguments, checkArgumentSize); err != nil {
		return err
	}
	if err := checkElements("packageArguments", p.PackageArguments, checkArgumentSize); err != nil {
		return err
	}
	return checkElements("environmentVariables", p.EnvironmentVariables, checkKeyValueSize)
}

func checkTransportSize(t *model.Transport) *submissionSizeError {
	if err := checkFields(stringField{"type", t.Type}, stringField{"url", t.URL}); err != nil {
		return err
	}
	if err := checkElements("headers", t.Headers, checkKeyValueSize); err != nil {
		return err
	}
	return checkVariableSizes(t.Variables)
}

func checkArgumentSize(a *model.Argument) *submissionSizeError {
	if err := checkFields(stringField{"type", string(a.Type)}, stringField{"name", a.Name}, stringField{"valueHint", a.ValueHint}); err != nil {
		return err
	}
	return checkInputWithVariablesSize(&a.InputWithVariables)
}

func checkKeyValueSize(kv *model.KeyValueInput) *submissionSizeError {
	if err := checkFields(stringField{"name", kv.Name}); err != nil {
		return err
	}
	return checkInputWithVariablesSize(&kv.InputWithVariables)
}

func checkInputWithVariablesSize(i *model.InputWithVariables) *submissionSizeError {
	if err := checkInputSize(&i.Input); err != nil {
		return err
	}
	return checkVariableSizes(i.Variables)
}

func checkVariableSizes(variables map[string]model.Input) *submissionSizeError {
	for name, input := range variables {
		if err := checkFields(stringField{"variable name", name}); err != nil {
			err.path = "variables"
			return err
		}
		if err := checkInputSize(&input); err != nil {
			// Keep user-supplied variable names out of size diagnostics.
			return err.under("variables[*]")
		}
	}
	return nil
}

func checkInputSize(i *model.Input) *submissionSizeError {
	if err := checkFields(stringField{"description", i.Description}, stringField{"format", string(i.Format)},
		stringField{"value", i.Value}, stringField{"default", i.Default}, stringField{"placeholder", i.Placeholder}); err != nil {
		return err
	}
	return checkElements("choices", i.Choices, func(choice *string) *submissionSizeError {
		err := checkFields(stringField{"choice", *choice})
		if err != nil {
			err.path = "" // The array index identifies the string itself.
		}
		return err
	})
}
