package main

import (
	"flag"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/registry/internal/api/router"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/service"
	"github.com/modelcontextprotocol/registry/internal/telemetry"
)

// Version info for the OpenAPI Export tool
// These variables are injected at build time via ldflags
var (
	Version = "dev"

	BuildTime = "unknown"

	GitCommit = "unknown"
)

func main() {
	// Define flags
	format := flag.String("format", "yaml", "Output format: json, yaml, json3.0, yaml3.0")
	output := flag.String("output", "", "Output file path (default: stdout)")
	outputShort := flag.String("o", "", "Output file path (short form)")
	showVersion := flag.Bool("version", false, "Display version information")
	showHelp := flag.Bool("help", false, "Display help information")

	flag.Parse()

	if *showVersion {
		log.Printf("openapi-export %s (commit: %s, built: %s)", Version, GitCommit, BuildTime)
		return
	}

	if *showHelp {
		printUsage()
		return
	}

	if *output != "" && *outputShort != "" {
		log.Fatal("Error: Cannot specify both -o and -output flags. Please use only one.")
	}

	outputPath := *output
	if outputPath == "" && *outputShort != "" {
		outputPath = *outputShort
	}

	// Initialize minimal config (we don't need a full server for this)
	cfg := config.NewConfig()

	// Create a mock registry service (we don't need a real one for OpenAPI export)
	// It's safe to pass nil because:
	// 1. Route definitions are statically registered and don't require the service
	// 2. We're only generating the OpenAPI spec, not executing any handlers
	// 3. The service is only used by handler implementations, not by OpenAPI generation
	var mockRegistry service.RegistryService

	// Create a minimal HTTP mux and metrics for API initialization
	mux := http.NewServeMux()

	// Initialize telemetry to get metrics
	_, metrics, err := telemetry.InitMetrics(cfg.Version)
	if err != nil {
		log.Fatalf("Failed to initialize metrics: %v", err)
	}

	api := router.NewHumaAPI(cfg, mockRegistry, mux, metrics)

	var data []byte
	switch *format {
	case "json":
		data, err = api.OpenAPI().MarshalJSON()
		if err != nil {
			log.Fatalf("Failed to generate OpenAPI JSON: %v", err)
		}
	case "yaml":
		data, err = api.OpenAPI().YAML()
		if err != nil {
			log.Fatalf("Failed to generate OpenAPI YAML: %v", err)
		}
	case "json3.0":
		data, err = api.OpenAPI().Downgrade()
		if err != nil {
			log.Fatalf("Failed to generate OpenAPI 3.0 JSON: %v", err)
		}
	case "yaml3.0":
		data, err = api.OpenAPI().DowngradeYAML()
		if err != nil {
			log.Fatalf("Failed to generate OpenAPI 3.0 YAML: %v", err)
		}
	default:
		log.Fatalf("Invalid format: %s. Use json, yaml, json3.0, or yaml3.0", *format)
	}

	if outputPath != "" {
		if err := os.WriteFile(outputPath, data, 0600); err != nil {
			log.Fatalf("Failed to write to file %s: %v", outputPath, err)
		}
		log.Printf("OpenAPI specification exported to %s", outputPath)
	} else {
		if _, err := io.WriteString(os.Stdout, string(data)+"\n"); err != nil {
			log.Fatalf("Failed to write to stdout: %v", err)
		}
	}
}

func printUsage() {
	usage := `OpenAPI Export Tool

Recommended Usage:
  make generate-openapi                   # Generate to docs/reference/api/openapi.yaml

Advanced Usage:
  openapi-export [flags]

Flags:
  -format string
        Output format: json, yaml, json3.0, yaml3.0 (default: yaml)
  -o, -output string
        Output file path (default: stdout)
  -version
        Display version information
  -help
        Display this help information

Examples:
  openapi-export                          # Export OpenAPI 3.1 YAML to stdout
  openapi-export -format json             # Export OpenAPI 3.1 JSON to stdout
  openapi-export -o openapi.yaml          # Export to file
  openapi-export -format json3.0 -o spec.json  # Export OpenAPI 3.0 JSON to file
`
	if _, err := io.WriteString(os.Stdout, usage); err != nil {
		log.Fatalf("Failed to write usage: %v", err)
	}
}
