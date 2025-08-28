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
	fmt.Println("MCP Registry Publisher Tool")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  mcp-publisher <command> [arguments]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  init          Create a server.json file template")
	fmt.Println("  login         Authenticate with the registry")
	fmt.Println("  logout        Clear saved authentication")
	fmt.Println("  publish       Publish server.json to the registry")
	fmt.Println()
	fmt.Println("Use 'mcp-publisher <command> --help' for more information about a command.")
}