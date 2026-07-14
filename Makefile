.PHONY: generate build test lint check install-tools

GOBIN ?= $(shell go env GOPATH)/bin

# Generate Go + OpenAPI from proto definitions (requires buf + remote plugins access)
generate:
	buf generate

# Build the gateway binary
build:
	cd gateway && go build ./...

# Run all tests
test:
	cd gateway && go test -race ./...

# Run linter
lint:
	cd gateway && golangci-lint run ./...

# Run all checks (lint + test)
check: lint test

# Install required tools
install-tools:
	go install github.com/bufbuild/buf/cmd/buf@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
