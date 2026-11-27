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

func validateSubmissionSize(req *apiv0.ServerJSON) error {
	if err := validatePublisherExtensions(req); err != nil {
		return err
	}
	// Use the same typed encoding as database.CreateServer and UpdateServer.
	encoded, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal server.json: %w", err)
	}
	if len(encoded) > maxServerJSONBytes {
		return fmt.Errorf("serialized server.json is %d bytes (limit %d)", len(encoded), maxServerJSONBytes)
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
func checkElements[T any](field string, elements []T, check func(*T) error) error {
	for i := range elements {
		if err := check(&elements[i]); err != nil {
			return fmt.Errorf("%s[%d]: %w", field, i, err)
		}
	}
	return nil
}

type stringField struct{ name, value string }

func checkFields(fields ...stringField) error {
	for _, f := range fields {
		if len(f.value) > maxNestedStringBytes {
			return fmt.Errorf("%s is %d bytes (limit %d)", f.name, len(f.value), maxNestedStringBytes)
		}
	}
	return nil
}

func checkPackageSize(p *model.Package) error {
	if err := checkFields(stringField{"registryType", p.RegistryType}, stringField{"registryBaseUrl", p.RegistryBaseURL},
		stringField{"identifier", p.Identifier}, stringField{"version", p.Version},
		stringField{"fileSha256", p.FileSHA256}, stringField{"runtimeHint", p.RunTimeHint}); err != nil {
		return err
	}
	if err := checkTransportSize(&p.Transport); err != nil {
		return fmt.Errorf("transport: %w", err)
	}
	if err := checkElements("runtimeArguments", p.RuntimeArguments, checkArgumentSize); err != nil {
		return err
	}
	if err := checkElements("packageArguments", p.PackageArguments, checkArgumentSize); err != nil {
		return err
	}
	return checkElements("environmentVariables", p.EnvironmentVariables, checkKeyValueSize)
}

func checkTransportSize(t *model.Transport) error {
	if err := checkFields(stringField{"type", t.Type}, stringField{"url", t.URL}); err != nil {
		return err
	}
	if err := checkElements("headers", t.Headers, checkKeyValueSize); err != nil {
		return err
	}
	return checkVariableSizes(t.Variables)
}

func checkArgumentSize(a *model.Argument) error {
	if err := checkFields(stringField{"type", string(a.Type)}, stringField{"name", a.Name}, stringField{"valueHint", a.ValueHint}); err != nil {
		return err
	}
	return checkInputWithVariablesSize(&a.InputWithVariables)
}

func checkKeyValueSize(kv *model.KeyValueInput) error {
	if err := checkFields(stringField{"name", kv.Name}); err != nil {
		return err
	}
	return checkInputWithVariablesSize(&kv.InputWithVariables)
}

func checkInputWithVariablesSize(i *model.InputWithVariables) error {
	if err := checkInputSize(&i.Input); err != nil {
		return err
	}
	return checkVariableSizes(i.Variables)
}

func checkVariableSizes(variables map[string]model.Input) error {
	for name, input := range variables {
		if err := checkFields(stringField{"variable name", name}); err != nil {
			return err
		}
		if err := checkInputSize(&input); err != nil {
			return fmt.Errorf("variables entry: %w", err)
		}
	}
	return nil
}

func checkInputSize(i *model.Input) error {
	if err := checkFields(stringField{"description", i.Description}, stringField{"format", string(i.Format)},
		stringField{"value", i.Value}, stringField{"default", i.Default}, stringField{"placeholder", i.Placeholder}); err != nil {
		return err
	}
	return checkElements("choices", i.Choices, func(choice *string) error {
		return checkFields(stringField{"choice", *choice})
	})
}
