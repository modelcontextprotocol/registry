package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	Version   string
	GitCommit string
	BuildTime string
)

var mcpPublisherCmd = &cobra.Command{
	Use:   "mcp-publisher <command> [arguments]",
	Short: "MCP Registry Publisher Tool",
}

func SetVersionInfo(version, gitCommit, buildTime string) {
	Version = version
	GitCommit = gitCommit
	BuildTime = buildTime

	mcpPublisherCmd.Version = Version
	mcpPublisherCmd.SetVersionTemplate(
		fmt.Sprintf("mcp-publisher %s (commit: %s, built: %s)", Version, GitCommit, BuildTime),
	)
}

func ExecuteMcpPublisherCmd() error {
	return mcpPublisherCmd.Execute()
}
