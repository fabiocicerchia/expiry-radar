BINARY  := expiry-radar
BIN_DIR := bin
PKG     := ./cmd/expiry-radar

.PHONY: all build test tidy clean

all: build

## build: compile the binary into ./bin
build:
	go build -o $(BIN_DIR)/$(BINARY) $(PKG)

## test: run tests
test:
	go test ./...

## tidy: tidy modules
tidy:
	go mod tidy

## clean: remove build artifacts
clean:
	rm -rf $(BIN_DIR)

setup: ## Install git hooks and dev tooling
	git config core.hooksPath .githooks
	@command -v pre-commit >/dev/null 2>&1 && pre-commit install || true

lint: ## Run all pre-commit checks on the whole tree
	pre-commit run --all-files
