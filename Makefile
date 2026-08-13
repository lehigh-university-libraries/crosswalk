.PHONY: help
.PHONY: build clean fmt generate install-tools lint test docs-docker-build docs-build docs-serve docs-preview docs-clean

DOCS_IMAGE ?= crosswalk-docs
DOCS_PORT ?= 8888
DOCS_DOCKER_USER ?= $(shell id -u):$(shell id -g)

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
	@echo "Generating arXiv category labels..."
	go generate ./format/arxiv/...
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

docs-docker-build: ## Build the Zensical docs image
	docker build -f docs/Dockerfile -t $(DOCS_IMAGE) .

docs-build: docs-docker-build ## Build the static docs site into ./docs/site
	rm -rf site docs/site
	docker run --rm \
		-u "$(DOCS_DOCKER_USER)" \
		$(if $(SITE_URL),-e SITE_URL=$(SITE_URL)) \
		-v "$(CURDIR):/work" \
		-w /work \
		$(DOCS_IMAGE) \
		build --clean --config-file docs/mkdocs.yml

docs-serve: docs-docker-build ## Serve docs with live reload at http://localhost:8888
	docker run --rm -it \
		-u "$(DOCS_DOCKER_USER)" \
		-p $(DOCS_PORT):8080 \
		-v "$(CURDIR):/work" \
		-w /work \
		$(DOCS_IMAGE) \
		serve --config-file docs/mkdocs.yml --dev-addr 0.0.0.0:8080

docs-preview: ## Build docs and serve ./docs/site at http://localhost:8888
	$(MAKE) docs-build SITE_URL=http://localhost:$(DOCS_PORT)
	docker run --rm -it \
		-p $(DOCS_PORT):8080 \
		-v "$(CURDIR)/docs/site:/site" \
		-w /site \
		--entrypoint python3 \
		$(DOCS_IMAGE) \
		-m http.server 8080

docs-clean: ## Remove the generated docs site
	rm -rf site docs/site
