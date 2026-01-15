package main

import (
	"fmt"
	"log"
	"os"

	"github.com/modelcontextprotocol/registry/cmd/publisher/commands"
)

// Version info for the MCP Publisher tool
// These variables are injected at build time via ldflags by goreleaser
var (
	// Version is the current version of the MCP Publisher tool
	Version = "dev"

	// BuildTime is the time at which the binary was built
	BuildTime = "unknown"

	// GitCommit is the git commit that was compiled
	GitCommit = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Check for help flag for subcommands
	if len(os.Args) >= 3 && (os.Args[2] == "--help" || os.Args[2] == "-h") {
		printCommandHelp(os.Args[1])
		return
	}

	var err error
	switch os.Args[1] {
	case "init":
		err = commands.InitCommand()
	case "login":
		err = commands.LoginCommand(os.Args[2:])
	case "logout":
		err = commands.LogoutCommand()
	case "publish":
		err = commands.PublishCommand(os.Args[2:])
	case "validate":
		err = commands.ValidateCommand(os.Args[2:])
	case "--version", "-v", "version":
		log.Printf("mcp-publisher %s (commit: %s, built: %s)", Version, GitCommit, BuildTime)
		return
	case "--help", "-h", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	_, _ = fmt.Fprintln(os.Stdout, "MCP Registry Publisher Tool")
	_, _ = fmt.Fprintln(os.Stdout)
	_, _ = fmt.Fprintln(os.Stdout, "Usage:")
	_, _ = fmt.Fprintln(os.Stdout, "  mcp-publisher <command> [arguments]")
	_, _ = fmt.Fprintln(os.Stdout)
	_, _ = fmt.Fprintln(os.Stdout, "Commands:")
	_, _ = fmt.Fprintln(os.Stdout, "  init          Create a server.json file template")
	_, _ = fmt.Fprintln(os.Stdout, "  login         Authenticate with the registry")
	_, _ = fmt.Fprintln(os.Stdout, "  logout        Clear saved authentication")
	_, _ = fmt.Fprintln(os.Stdout, "  publish       Publish server.json to the registry")
	_, _ = fmt.Fprintln(os.Stdout, "  validate      Validate server.json without publishing")
	_, _ = fmt.Fprintln(os.Stdout)
	_, _ = fmt.Fprintln(os.Stdout, "Use 'mcp-publisher <command> --help' for more information about a command.")
}

func printCommandHelp(command string) {
	switch command {
	case "init":
		_, _ = fmt.Fprintln(os.Stdout, "Create a server.json file template")
		_, _ = fmt.Fprintln(os.Stdout)
		_, _ = fmt.Fprintln(os.Stdout, "Usage:")
		_, _ = fmt.Fprintln(os.Stdout, "  mcp-publisher init")
		_, _ = fmt.Fprintln(os.Stdout)
		_, _ = fmt.Fprintln(os.Stdout, "This command creates a server.json file in the current directory with")
		_, _ = fmt.Fprintln(os.Stdout, "auto-detected values from your project (package.json, git remote, etc.).")
		_, _ = fmt.Fprintln(os.Stdout)
		_, _ = fmt.Fprintln(os.Stdout, "After running init, edit the generated server.json to customize your")
		_, _ = fmt.Fprintln(os.Stdout, "server's metadata before publishing.")

	case "login":
		_, _ = fmt.Fprintln(os.Stdout, "Authenticate with the registry")
		_, _ = fmt.Fprintln(os.Stdout)
		_, _ = fmt.Fprintln(os.Stdout, "Usage:")
		_, _ = fmt.Fprintln(os.Stdout, "  mcp-publisher login <method> [options]")
		_, _ = fmt.Fprintln(os.Stdout)
		_, _ = fmt.Fprintln(os.Stdout, "Methods:")
		_, _ = fmt.Fprintln(os.Stdout, "  github        Interactive GitHub authentication")
		_, _ = fmt.Fprintln(os.Stdout, "  github-oidc   GitHub Actions OIDC authentication")
		_, _ = fmt.Fprintln(os.Stdout, "  dns           DNS-based authentication (requires --domain)")
		_, _ = fmt.Fprintln(os.Stdout, "  http          HTTP-based authentication (requires --domain)")
		_, _ = fmt.Fprintln(os.Stdout, "  none          Anonymous authentication (for testing)")
		_, _ = fmt.Fprintln(os.Stdout)
		_, _ = fmt.Fprintln(os.Stdout, "Examples:")
		_, _ = fmt.Fprintln(os.Stdout, "  mcp-publisher login github")
		_, _ = fmt.Fprintln(os.Stdout, "  mcp-publisher login dns --domain example.com --private-key <key>")

	case "logout":
		_, _ = fmt.Fprintln(os.Stdout, "Clear saved authentication")
		_, _ = fmt.Fprintln(os.Stdout)
		_, _ = fmt.Fprintln(os.Stdout, "Usage:")
		_, _ = fmt.Fprintln(os.Stdout, "  mcp-publisher logout")
		_, _ = fmt.Fprintln(os.Stdout)
		_, _ = fmt.Fprintln(os.Stdout, "This command removes the saved authentication token from your system.")

	case "publish":
		_, _ = fmt.Fprintln(os.Stdout, "Publish server.json to the registry")
		_, _ = fmt.Fprintln(os.Stdout)
		_, _ = fmt.Fprintln(os.Stdout, "Usage:")
		_, _ = fmt.Fprintln(os.Stdout, "  mcp-publisher publish [server.json]")
		_, _ = fmt.Fprintln(os.Stdout)
		_, _ = fmt.Fprintln(os.Stdout, "Arguments:")
		_, _ = fmt.Fprintln(os.Stdout, "  server.json   Path to the server.json file (default: ./server.json)")
		_, _ = fmt.Fprintln(os.Stdout)
		_, _ = fmt.Fprintln(os.Stdout, "You must be logged in before publishing. Run 'mcp-publisher login' first.")

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		printUsage()
	}
}
