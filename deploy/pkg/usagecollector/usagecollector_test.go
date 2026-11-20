package usagecollector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"gopkg.in/yaml.v2"
)

const (
	flagKey    = "mcp-registry:" + flagConfig
	aliasesKey = "mcp-registry:" + aliasesConfig
)

type noResources struct{}

func (noResources) Call(a pulumi.MockCallArgs) (resource.PropertyMap, error) { return a.Args, nil }
func (noResources) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	return a.Name, a.Inputs, nil
}

func validate(settings map[string]string, secrets ...string) error {
	return pulumi.RunErr(Validate, pulumi.WithMocks("mcp-registry-infra", "test", noResources{}), func(info *pulumi.RunInfo) {
		info.Config = settings
		info.ConfigSecretKeys = secrets
	})
}

func hosted(changes map[string]string) map[string]string {
	settings := map[string]string{
		flagKey:                                 "true",
		"mcp-registry:environment":              "staging",
		"mcp-registry:provider":                 "gcp",
		"mcp-registry:gatewayStagingStandalone": "true",
		"gcp:project":                           "mcp-registry-staging",
	}
	for key, value := range changes {
		settings[key] = value
	}
	return settings
}

func TestTheFlagIsCheckedBeforeAnythingIsCreated(t *testing.T) {
	for name, test := range map[string]struct {
		settings map[string]string
		invalid  bool
	}{
		"not set":                      {map[string]string{"mcp-registry:environment": "local"}, false},
		"hosted staging":               {hosted(nil), false},
		"hosted production":            {hosted(map[string]string{"mcp-registry:environment": "prod", "mcp-registry:gatewayStagingStandalone": "", "mcp-registry:gatewayProductionPhase": "standalone"}), false},
		"retired anywhere":             {map[string]string{flagKey: "false", "mcp-registry:environment": "local"}, false},
		"neither on nor retired":       {hosted(map[string]string{flagKey: "yes"}), true},
		"no Cloud Logging to read":     {hosted(map[string]string{"mcp-registry:provider": "local"}), true},
		"no Gateway counters to scale": {hosted(map[string]string{"mcp-registry:gatewayStagingStandalone": ""}), true},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validate(test.settings); (err != nil) != test.invalid {
				t.Fatalf("unexpected result: %v", err)
			}
		})
	}
}

// The aliases name real organizations and this repository is public.
func TestAliasesAreOnlyAcceptedEncryptedAndWellFormed(t *testing.T) {
	const aliases = "EXAMPLECORP=Example Corp;EXAMPLE-CORP=Example Corp"
	if err := validate(hosted(map[string]string{aliasesKey: aliases}), aliasesKey); err != nil {
		t.Fatalf("a valid value is refused: %v", err)
	}
	for name, test := range map[string]struct {
		value  string
		secret bool
	}{
		"stored unencrypted": {aliases, false},
		"no name":            {"EXAMPLECORP", true},
		"short key":          {"EX=Example Corp", true},
		"two names":          {"EXAMPLECORP=Example=Corp", true},
	} {
		t.Run(name, func(t *testing.T) {
			var secrets []string
			if test.secret {
				secrets = []string{aliasesKey}
			}
			err := validate(hosted(map[string]string{aliasesKey: test.value}), secrets...)
			if err == nil {
				t.Fatal("accepted")
			}
			if strings.Contains(strings.ToUpper(err.Error()), "EXAMPLE") {
				t.Errorf("the error shows the value: %v", err)
			}
		})
	}
}

func TestStackFilesKeepTheAliasesEncrypted(t *testing.T) {
	stacks, err := filepath.Glob("../../Pulumi.*.yaml")
	if err != nil || len(stacks) == 0 {
		t.Fatalf("no stack files found: %v", err)
	}
	for _, stack := range stacks {
		data, err := os.ReadFile(stack)
		if err != nil {
			t.Fatal(err)
		}
		var file struct {
			Config map[string]any
		}
		if err := yaml.Unmarshal(data, &file); err != nil {
			t.Fatalf("%s: %v", stack, err)
		}
		value, ok := file.Config[aliasesKey]
		if !ok {
			continue
		}
		encrypted, _ := value.(map[any]any)
		if _, ok := encrypted["secure"]; !ok || len(encrypted) != 1 {
			t.Errorf("%s: %s is not encrypted; set it with `pulumi config set --secret`", stack, aliasesKey)
		}
	}
}
