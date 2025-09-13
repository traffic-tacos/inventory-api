.PHONY: all build clean test lint format generate docker-build docker-run help

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
BINARY_NAME=inventory-api
BINARY_UNIX=$(BINARY_NAME)_unix
MAIN_PATH=./cmd/inventory-api

# Build the project
all: clean format lint test build

build:
	$(GOBUILD) -o $(BINARY_NAME) -v $(MAIN_PATH)

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GOBUILD) -o $(BINARY_UNIX) -v $(MAIN_PATH)

test:
	$(GOTEST) -v ./...

test-coverage:
	$(GOTEST) -race -coverprofile=coverage.out -covermode=atomic ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run

format:
	gofmt -s -w .
	goimports -w .

generate:
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		proto/inventory.proto

clean:
	$(GOCLEAN)
	rm -f $(BINARY_NAME)
	rm -f $(BINARY_UNIX)
	rm -f coverage.out coverage.html

run:
	$(GOBUILD) -o $(BINARY_NAME) -v $(MAIN_PATH)
	./$(BINARY_NAME)

deps:
	$(GOMOD) download
	$(GOMOD) tidy

deps-update:
	$(GOMOD) tidy
	$(GOCMD) get -u ./...

# Docker commands
docker-build:
	docker build -t inventory-api:latest .

docker-run:
	docker run --rm -p 8080:8080 inventory-api:latest

# Development tools
install-tools:
	$(GOCMD) install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	$(GOCMD) install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	$(GOCMD) install golang.org/x/tools/cmd/goimports@latest
	$(GOCMD) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Load testing
load-test-ghz:
	ghz --insecure \
		--proto ./proto/inventory.proto \
		--call inventory.v1.Inventory.CommitReservation \
		--data-file ./tools/testdata/commit_payload.json \
		--rps 500 \
		--duration 30s \
		--concurrency 10 \
		localhost:8080

help:
	@echo "Available commands:"
	@echo "  build         Build the binary"
	@echo "  test          Run tests"
	@echo "  lint          Run linter"
	@echo "  format        Format code"
	@echo "  generate      Generate protobuf code"
	@echo "  clean         Clean build artifacts"
	@echo "  run           Build and run the application"
	@echo "  deps          Download and tidy dependencies"
	@echo "  docker-build  Build Docker image"
	@echo "  docker-run    Run Docker container"
	@echo "  load-test-ghz Run load test with ghz"
