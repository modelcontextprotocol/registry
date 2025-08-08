.PHONY: help build test test-unit test-integration test-endpoints test-publish test-all lint lint-fix validate validate-schemas validate-examples check dev-local dev-compose clean publisher

# Default target
help: ## Show this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'

# Build targets
build: ## Build the registry application
	go build -o bin/registry ./cmd/registry

publisher: ## Build the publisher tool
	cd tools/publisher && ./build.sh

# Test targets
test-unit: ## Run unit tests with coverage
	go test -v -race -coverprofile=coverage.out -covermode=atomic ./internal/...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

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

validate-examples: ## Validate examples against schemas
	./tools/validate-examples.sh

validate: validate-schemas validate-examples ## Run all validation checks

# Lint targets
lint: ## Run linter (includes formatting)
	golangci-lint run --timeout=5m

lint-fix: ## Run linter with auto-fix (includes formatting)
	golangci-lint run --fix --timeout=5m

# Combined targets
check: lint validate test-all ## Run all checks (lint, validate, unit tests)
	@echo "All checks passed!"

# Development targets
dev-compose: ## Start development environment with Docker Compose (builds image automatically)
	docker compose up --build

dev-local: ## Run registry locally (requires MongoDB)
	go run cmd/registry/main.go

# Cleanup
clean: ## Clean build artifacts and coverage files
	rm -rf bin
	rm -f coverage.out coverage.html
	cd tools/publisher && rm -f publisher


.DEFAULT_GOAL := help
