.PHONY: help build local-preview local-up staging-preview staging-up prod-preview prod-up

# Default target
help: ## Show this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'

# Build the Go binary
build: ## Build the Pulumi Go program
	go build

# Local stack commands
local-preview: build ## Preview local infrastructure changes
	PULUMI_CONFIG_PASSPHRASE="" pulumi preview --stack local

local-up: build ## Deploy local infrastructure
	PULUMI_CONFIG_PASSPHRASE="" pulumi up --yes --stack local

# Staging stack commands
staging-preview: build ## Preview staging infrastructure changes
	PULUMI_CONFIG_PASSPHRASE_FILE=passphrase.staging.txt pulumi preview --stack gcpStaging

staging-up: build ## Deploy staging infrastructure
	PULUMI_CONFIG_PASSPHRASE_FILE=passphrase.staging.txt pulumi up --yes --stack gcpStaging

# Production stack commands
prod-preview: build ## Preview production infrastructure changes
	PULUMI_CONFIG_PASSPHRASE_FILE=passphrase.prod.txt pulumi preview --stack gcpProd

prod-up: build ## Deploy production infrastructure
	PULUMI_CONFIG_PASSPHRASE_FILE=passphrase.prod.txt pulumi up --yes --stack gcpProd