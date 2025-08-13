.PHONY: help build local-login local-preview local-up staging-login staging-preview staging-up prod-login prod-preview prod-up

# Default target
help: ## Show this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'

# Build the Go binary
build: ## Build the Pulumi Go program
	go build

# Local stack commands
local-login: ## Login to local Pulumi backend
	pulumi login --local

local-preview: build local-login ## Preview local infrastructure changes
	PULUMI_CONFIG_PASSPHRASE="" pulumi preview --stack local

local-up: build local-login ## Deploy local infrastructure
	PULUMI_CONFIG_PASSPHRASE="" pulumi up --yes --stack local

# Staging stack commands
staging-login: ## Login to staging Pulumi backend
	pulumi login gs://mcp-registry-staging-pulumi-state

staging-preview: build staging-login ## Preview staging infrastructure changes
	PULUMI_CONFIG_PASSPHRASE_FILE=passphrase.staging.txt pulumi preview --stack gcpStaging

staging-up: build staging-login ## Deploy staging infrastructure
	PULUMI_CONFIG_PASSPHRASE_FILE=passphrase.staging.txt pulumi up --yes --stack gcpStaging

# Production stack commands
prod-login: ## Login to production Pulumi backend
	pulumi login gs://mcp-registry-prod-pulumi-state

prod-preview: build prod-login ## Preview production infrastructure changes
	PULUMI_CONFIG_PASSPHRASE_FILE=passphrase.prod.txt pulumi preview --stack gcpProd

prod-up: build prod-login ## Deploy production infrastructure
	PULUMI_CONFIG_PASSPHRASE_FILE=passphrase.prod.txt pulumi up --yes --stack gcpProd