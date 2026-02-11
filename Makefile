.PHONY: help
.PHONY: build clean fmt generate install-tools lint test

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the CLI
	go build -o crosswalk .

clean: ## Clean generated files
	@echo "Cleaning generated files..."
	rm -rf gen/
	rm -f crosswalk
	@echo "Done"

fmt: ## Format all go code the CLI
	find . -type f -name "*.go" -exec gofmt -w {} \;

generate: ## Generate Go code and JSON Schema from .proto files
	@echo "Generating protobuf code..."
	buf generate
	@echo "Generating JSON Schema for Hub..."
	buf generate --path hub/v1 --template buf.gen.jsonschema.yaml
	@echo "Done"

install-tools: ## Install required development tools
	@echo "Installing development tools..."
	go install github.com/bufbuild/buf/cmd/buf@v1.65.0
	go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
	@echo "Done"

lint: ## Lint proto files and Go code
	@echo "Linting proto files..."
	buf lint
	@echo "Linting Go code..."
	golangci-lint run

test: ## Run all tests
	go test -v -race ./...
