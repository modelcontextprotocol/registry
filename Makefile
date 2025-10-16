.PHONY: help build test test-unit test-integration test-endpoints test-publish test-all lint lint-fix validate validate-schemas validate-examples check dev-compose clean publisher generate-schema check-schema

# Default target
help: ## Show this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'

# Build targets
build: ## Build the registry application with version info
	@mkdir -p bin
	go build -ldflags="-X main.Version=dev-$(shell git rev-parse --short HEAD) -X main.GitCommit=$(shell git rev-parse HEAD) -X main.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)" -o bin/registry ./cmd/registry

publisher: ## Build the publisher tool with version info
	@mkdir -p bin
	go build -ldflags="-X main.Version=dev-$(shell git rev-parse --short HEAD) -X main.GitCommit=$(shell git rev-parse HEAD) -X main.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)" -o bin/mcp-publisher ./cmd/publisher

# Schema generation targets
generate-schema: ## Generate server.schema.json from openapi.yaml
	@mkdir -p bin
	go build -o bin/extract-server-schema ./tools/extract-server-schema
	@./bin/extract-server-schema

check-schema: ## Check if server.schema.json is in sync with openapi.yaml
	@mkdir -p bin
	go build -o bin/extract-server-schema ./tools/extract-server-schema
	@./bin/extract-server-schema -check

# Test targets
test-unit: ## Run unit tests with coverage (requires PostgreSQL)
	@echo "Starting PostgreSQL for unit tests..."
	@docker compose up -d postgres
	@echo "Waiting for PostgreSQL to be ready..."
	@sleep 3
	@echo "Running unit tests..."
	go test -v -race -coverprofile=coverage.out -covermode=atomic ./internal/...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"
	@echo "Stopping PostgreSQL..."
	@docker compose down postgres

test: ## Run unit tests (use 'make test-all' to run all tests)
	@echo "⚠️  Running unit tests only. Use 'make test-all' to run both unit and integration tests."
	@$(MAKE) test-unit

test-integration: ## Run integration tests
	./tests/integration/run.sh

test-endpoints: ## Test API endpoints (requires running server)
	./scripts/test_endpoints.sh

test-publish: ## Test publish endpoint (requires BEARER_TOKEN env var)
	./scripts/test_publish.sh

test-all: test-unit test-integration ## Run all tests (unit and integration)

# Validation targets
validate-schemas: ## Validate JSON schemas
	./tools/validate-schemas.sh
	@$(MAKE) check-schema

validate-examples: ## Validate examples against schemas
	./tools/validate-examples.sh

validate: validate-schemas validate-examples ## Run all validation checks

# Lint targets
lint: ## Run linter (includes formatting)
	golangci-lint run --timeout=5m

lint-fix: ## Run linter with auto-fix (includes formatting)
	golangci-lint run --fix --timeout=5m

# Combined targets
check: dev-down lint validate test-all ## Run all checks (lint, validate, unit tests) and ensure dev environment is down
	@echo "All checks passed!"

# Development targets
dev-compose: ## Start development environment with Docker Compose (builds image automatically)
	GIT_COMMIT=$$(git rev-parse HEAD) \
	GIT_COMMIT_SHORT=$$(git rev-parse --short HEAD) \
	BUILD_TIME=$$(date -u +%Y-%m-%dT%H:%M:%SZ) \
	docker compose up --build

dev-down: ## Stop development environment
	docker compose down

# Cleanup
clean: ## Clean build artifacts and coverage files
	rm -rf bin
	rm -f coverage.out coverage.html


.DEFAULT_GOAL := help
