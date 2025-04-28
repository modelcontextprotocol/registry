# Makefile for MCP Registry project

# Go parameters
BINARY_NAME=registry
MAIN_PACKAGE=./cmd/registry
BUILD_DIR=build
GO=go

# Build settings
VERSION=0.1.0
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-s -w -X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildTime=$(BUILD_TIME)"

VERSION_INFO=Version: $(BINARY_NAME) v$(VERSION)

.PHONY: all build clean test run run-test lint fmt vet help	

all: clean build

# Build the application
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@$(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PACKAGE)
	@echo "Build complete!"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@echo "Clean complete!"

# Run tests
test:
	@echo "Running tests..."
	@$(GO) test ./... -v
	@echo "Tests complete!"

# Run the application
run:
	@echo "Running $(BINARY_NAME)..."
	@$(GO) run $(MAIN_PACKAGE)/main.go $(MAIN_PACKAGE)/version.go

# Run the application with test environment (using fake service)
run-test:
	@echo "Running $(BINARY_NAME) with test environment..."
	@APP_ENV=test $(GO) run $(MAIN_PACKAGE)/main.go $(MAIN_PACKAGE)/version.go

# Install dependencies
deps:
	@echo "Installing dependencies..."
	@$(GO) mod download
	@echo "Dependencies installed!"

# Format code
fmt:
	@echo "Formatting code..."
	@$(GO) fmt ./...
	@echo "Formatting complete!"

# Run go vet
vet:
	@echo "Running go vet..."
	@$(GO) vet ./...
	@echo "Vet complete!"

# Run golint if installed
lint:
	@if command -v golint > /dev/null; then \
		echo "Running golint..."; \
		golint ./...; \
		echo "Lint complete!"; \
	else \
		echo "golint not installed. Run: go install golang.org/x/lint/golint@latest"; \
	fi

# Build for multiple platforms (cross-compilation)
build-all: clean
	@echo "Building for multiple platforms..."
	@mkdir -p $(BUILD_DIR)
	@GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 $(MAIN_PACKAGE)
	@GOOS=darwin GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 $(MAIN_PACKAGE)
	@GOOS=darwin GOARCH=arm64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 $(MAIN_PACKAGE)
	@GOOS=windows GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe $(MAIN_PACKAGE)
	@echo "Cross-compilation complete!"

# Show help
help:
	@echo "Available targets:"
	@echo "  all        - Clean and build the project"
	@echo "  build      - Build the project binary"
	@echo "  clean      - Remove build artifacts"
	@echo "  test       - Run all tests"
	@echo "  run        - Run the application"
	@echo "  run-test   - Run the application with test environment (fake service)"
	@echo "  deps       - Download dependencies"
	@echo "  fmt        - Format code using go fmt"
	@echo "  vet        - Run go vet"
	@echo "  lint       - Run golint (if installed)"
	@echo "  build-all  - Build for multiple platforms"
	@echo "  help       - Show this help message"