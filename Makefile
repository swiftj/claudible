# Claudible Makefile
# Build and development commands for the Claude Code notification system

.PHONY: all build test lint fmt clean install setup-hooks help

# Build variables
BINARY_NAME := claudible
BUILD_DIR := build
VERSION := 0.1.0
LDFLAGS := -ldflags "-s -w"

# Default target
all: build

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/claudible

# Build for all platforms
build-all: build-darwin build-linux
	@echo "Built binaries for all platforms"

build-darwin:
	@echo "Building for macOS..."
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/claudible
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/claudible

build-linux:
	@echo "Building for Linux..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/claudible
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/claudible

# Run tests
test:
	go test ./...

# Run tests with coverage
test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Run tests with race detector
test-race:
	go test -race ./...

# Run linter (requires golangci-lint)
lint:
	golangci-lint run

# Format code
fmt:
	go fmt ./...

# Tidy dependencies
tidy:
	go mod tidy

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

# Install to GOPATH/bin
install:
	go install ./cmd/claudible

# Install to /usr/local/bin (requires sudo)
install-system:
	@echo "Installing to /usr/local/bin..."
	go build $(LDFLAGS) -o /usr/local/bin/$(BINARY_NAME) ./cmd/claudible
	@echo "Installed $(BINARY_NAME) to /usr/local/bin/"

# Create config directory
config-dir:
	@mkdir -p ~/.config/claudible
	@echo "Created ~/.config/claudible/"

# Show version
version:
	@echo "$(BINARY_NAME) version $(VERSION)"

# Setup git hooks (auto-bump version on commit)
setup-hooks:
	@echo "Installing git hooks..."
	@cp scripts/pre-commit .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "Git hooks installed. Version will auto-bump on commit."

# Help
help:
	@echo "Claudible - Claude Code Notification System"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  build          Build the binary"
	@echo "  build-all      Build for macOS and Linux (amd64, arm64)"
	@echo "  test           Run tests"
	@echo "  test-coverage  Run tests with coverage report"
	@echo "  test-race      Run tests with race detector"
	@echo "  lint           Run golangci-lint"
	@echo "  fmt            Format code"
	@echo "  tidy           Tidy go.mod"
	@echo "  clean          Remove build artifacts"
	@echo "  install        Install to GOPATH/bin"
	@echo "  install-system Install to /usr/local/bin"
	@echo "  config-dir     Create config directory"
	@echo "  setup-hooks    Install git hooks (auto-bump version)"
	@echo "  version        Show version"
	@echo "  help           Show this help"
