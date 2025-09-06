package main

// Version info for the MCP Registry application
// These variables are injected at build time via ldflags by goreleaser
var (
	// Version is the current version of the MCP Registry application
	Version = "dev"

	// BuildTime is the time at which the binary was built
	BuildTime = "unknown"

	// GitCommit is the git commit that was compiled
	GitCommit = "unknown"
)
