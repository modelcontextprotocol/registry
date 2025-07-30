.PHONY: help build test lint lint-fix validate validate-schemas validate-examples integration-test check dev clean docker publisher coverage

# Default target
help: ## Show this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'

# Build targets
build: ## Build the registry application
	go build ./cmd/registry

publisher: ## Build the publisher tool
	cd tools/publisher && ./build.sh

docker: ## Build Docker image
	docker build -t registry .

# Test targets
test: ## Run unit tests
	go test -v -race -coverprofile=coverage.out -covermode=atomic ./internal/...

integration-test: ## Run integration tests
	./tests/integration/run.sh

test-endpoints: ## Test API endpoints (requires running server)
	./scripts/test_endpoints.sh

test-publish: ## Test publish endpoint (requires BEARER_TOKEN env var)
	./scripts/test_publish.sh

# Validation targets
validate-schemas: ## Validate JSON schemas
	./tools/validate-schemas.sh

validate-examples: ## Validate examples against schemas
	./tools/validate-examples.sh

validate: validate-schemas validate-examples ## Run all validation checks

# Code quality targets
lint: ## Run linter (includes formatting)
	golangci-lint run --timeout=5m

lint-fix: ## Run linter with auto-fix (includes formatting)
	golangci-lint run --fix --timeout=5m

# Combined targets
check: lint validate test ## Run all checks (lint, validate, test)

pre-commit: check integration-test ## Run all pre-commit checks
	@echo "✅ All pre-commit checks passed!"

# Development targets
dev: ## Start development environment with Docker Compose
	docker compose up

dev-local: ## Run registry locally (requires MongoDB)
	go run cmd/registry/main.go

# Cleanup
clean: ## Clean build artifacts and coverage files
	rm -f registry
	rm -f coverage.out
	cd tools/publisher && rm -f publisher

# Coverage
coverage: ## Generate test coverage report
	go test -v -race -coverprofile=coverage.out -covermode=atomic ./internal/...

.DEFAULT_GOAL := help