package main

import (
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
	commands.SetVersionInfo(Version, GitCommit, BuildTime)
	if err := commands.ExecuteMcpPublisherCmd(); err != nil {
		os.Exit(1)
	}
}
