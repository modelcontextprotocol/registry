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
	_, _ = fmt.Fprintln(os.Stdout)
	_, _ = fmt.Fprintln(os.Stdout, "Use 'mcp-publisher <command> --help' for more information about a command.")
}
