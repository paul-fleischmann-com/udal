.PHONY: generate generate-openapi-v3 validate-openapi-v3 build test lint check install-tools

GOBIN ?= $(shell go env GOPATH)/bin

# Generate Go + OpenAPI (v2) from proto definitions (requires buf + remote plugins access)
generate:
	buf generate
	$(MAKE) generate-openapi-v3

# Convert the generated OpenAPI v2 (Swagger) spec to OpenAPI v3
generate-openapi-v3:
	npx --yes swagger2openapi code/api/openapi/udal/v1/device.swagger.json \
		--outfile code/api/openapi/udal/v1/device.openapi.v3.json \
		--patch

# Validate the OpenAPI v3 spec (structural validity, see redocly.yaml)
validate-openapi-v3:
	npx --yes @redocly/cli lint

# Build the gateway binary
build:
	cd code/gateway && go build ./...

# Run all tests
test:
	cd code/gateway && go test -race ./...

# Run linter
lint:
	cd code/gateway && golangci-lint run ./...

# Run all checks (lint + test)
check: lint test validate-openapi-v3

# Install required tools
install-tools:
	go install github.com/bufbuild/buf/cmd/buf@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
