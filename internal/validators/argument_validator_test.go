package validators_test

import (
	"testing"

	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/modelcontextprotocol/registry/internal/validators"
)

func TestArgumentValidator_ValidateNamedArgument(t *testing.T) {
	validator := validators.NewArgumentValidator()

	t.Run("Valid named arguments", func(t *testing.T) {
		validCases := []model.Argument{
			{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Value: "/path/to/dir"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "--directory",
			},
			{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Default: "8080"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "--port",
			},
			{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Value: "true"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "-v",
			},
			{
				Type: model.ArgumentTypeNamed,
				Name: "-p",
			},
			{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Value: "/etc/config.json"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "config",
			},
			{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Default: "false"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "verbose",
			},
			// No dash prefix requirement as per modification #1
			{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Value: "json"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "output-format",
			},
		}

		for _, arg := range validCases {
			err := validator.Validate(&arg)
			if err != nil {
				t.Errorf("Expected valid argument %+v, got error: %v", arg, err)
			}
		}
	})

	t.Run("Valid positional arguments (should not validate named rules)", func(t *testing.T) {
		positionalCases := []model.Argument{
			{Type: model.ArgumentTypePositional, Name: "anything with spaces"},
			{Type: model.ArgumentTypePositional, Name: "anything<with>brackets"},
			{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Value: "--port 8080"}},
				Type:               model.ArgumentTypePositional,
			}, // Can contain flags in value for positional
		}

		for _, arg := range positionalCases {
			err := validator.Validate(&arg)
			if err != nil {
				t.Errorf("Expected valid positional argument %+v, got error: %v", arg, err)
			}
		}
	})

	t.Run("Invalid named argument names", func(t *testing.T) {
		invalidNameCases := []model.Argument{
			{Type: model.ArgumentTypeNamed, Name: "--directory <absolute_path_to_adfin_mcp_folder>"},
			{Type: model.ArgumentTypeNamed, Name: "--port 8080"},
			{Type: model.ArgumentTypeNamed, Name: "--config $CONFIG_FILE"},
			{Type: model.ArgumentTypeNamed, Name: "--file <path>"},
			{Type: model.ArgumentTypeNamed, Name: ""},
			{Type: model.ArgumentTypeNamed, Name: "name with spaces"},
		}

		for _, arg := range invalidNameCases {
			err := validator.Validate(&arg)
			if err == nil {
				t.Errorf("Expected error for invalid named argument name: %+v", arg)
			}
		}
	})

	t.Run("Invalid value fields (startsWith check)", func(t *testing.T) {
		invalidValueCases := []model.Argument{
			{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Value: "--port 8080"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "--port",
			},
			{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Default: "--config /etc/app.conf"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "--config",
			},
			{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Value: "--with-editable $REPOSITORY_DIRECTORY"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "--with-editable",
			},
			{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Default: "--with-editable $REPOSITORY_DIRECTORY"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "--with-editable",
			},
		}

		for _, arg := range invalidValueCases {
			err := validator.Validate(&arg)
			if err == nil {
				t.Errorf("Expected error for argument with value starting with name: %+v", arg)
			}
		}
	})

	t.Run("Valid value fields (doesn't start with name)", func(t *testing.T) {
		validValueCases := []model.Argument{
			{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Value: "8080"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "--port",
			},
			{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Default: "/etc/app.conf"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "--config",
			},
			{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Value: "$REPOSITORY_DIRECTORY"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "--with-editable",
			},
			{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Value: "/absolute/path/to/directory"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "--directory",
			},
			// Contains the name but doesn't start with it
			{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Value: "use --port for configuration"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "--port",
			},
		}

		for _, arg := range validValueCases {
			err := validator.Validate(&arg)
			if err != nil {
				t.Errorf("Expected valid argument %+v, got error: %v", arg, err)
			}
		}
	})
}