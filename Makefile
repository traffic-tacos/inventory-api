# Variables
APP_NAME := inventory-api
VERSION ?= latest
REGISTRY ?= localhost:5000
IMAGE_NAME := $(REGISTRY)/$(APP_NAME):$(VERSION)

# Go variables
GOBASE := $(shell pwd)
GOPATH := $(GOBASE)/vendor:$(GOBASE)
GOBIN := $(GOBASE)/bin
GOFILES := $(wildcard *.go)

.PHONY: all build clean test lint docker-build docker-push help

# Default target
all: clean lint test build

# Install development tools
install-tools:
	@echo "Installing development tools..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest

# Build the application
build:
	@echo "Building $(APP_NAME)..."
	@CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -a -installsuffix cgo -o bin/$(APP_NAME) ./cmd/$(APP_NAME)

# Build for local development
build-local:
	@echo "Building $(APP_NAME) for local..."
	@go build -o bin/$(APP_NAME) ./cmd/$(APP_NAME)

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf bin/
	@go clean

# Run tests
test:
	@echo "Running tests..."
	@go test -v -race -coverprofile=coverage.out ./...

# Run integration tests
test-integration:
	@echo "Running integration tests..."
	@go test -v -tags=integration ./tests/...

# Lint code
lint:
	@echo "Running linter..."
	@golangci-lint run


# Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...

# Tidy dependencies
tidy:
	@echo "Tidying dependencies..."
	@go mod tidy

# Docker build
docker-build:
	@echo "Building Docker image..."
	@docker build --platform linux/arm64,linux/amd64 -t $(IMAGE_NAME) .

# Docker push
docker-push: docker-build
	@echo "Pushing Docker image..."
	@docker push $(IMAGE_NAME)

# Run locally
run-local: build-local
	@echo "Running $(APP_NAME) locally..."
	@./bin/$(APP_NAME)

# Load test with ghz (requires proto-contracts)
load-test-ghz:
	@echo "Running load test with ghz..."
	@echo "Load testing requires proto files from proto-contracts repository"

# Performance test
perf-test:
	@echo "Running performance test..."
	@go test -bench=. -benchmem ./...

# Check dependencies
deps-check:
	@echo "Checking dependencies..."
	@go list -u -m all

# Security scan
security-scan:
	@echo "Running security scan..."
	@gosec ./...

# Install development dependencies
install-dev:
	@echo "Installing development dependencies..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest

# CI pipeline
ci: clean lint test build

# Help
help:
	@echo "Available targets:"
	@echo "  all              - Clean, generate, lint, test, and build"
	@echo "  build            - Build the application for linux/arm64"
	@echo "  build-local      - Build the application for local development"
	@echo "  clean            - Clean build artifacts"
	@echo "  test             - Run unit tests"
	@echo "  test-integration - Run integration tests"
	@echo "  lint             - Run linter"
	@echo "  fmt              - Format code"
	@echo "  docker-build     - Build Docker image"
	@echo "  docker-push      - Push Docker image"
	@echo "  run-local        - Run application locally"
	@echo "  load-test-ghz    - Run load test with ghz"
	@echo "  perf-test        - Run performance benchmarks"
	@echo "  install-tools    - Install required tools"
	@echo "  install-dev      - Install development dependencies"
	@echo "  ci               - Run CI pipeline"