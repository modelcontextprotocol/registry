package main

import (
	"fmt"
	"os"

	"github.com/modelcontextprotocol/registry/tools/publisher/commands"
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