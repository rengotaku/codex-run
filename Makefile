.PHONY: deps build run lint test test-cov check ci clean install uninstall help

# Default target
.DEFAULT_GOAL := help

# Variables
BINARY_NAME := codex-run
BIN_DIR := bin
COVERAGE_FILE := coverage.out
PREFIX ?= $(HOME)/.local
INSTALL_DST := $(PREFIX)/bin/$(BINARY_NAME)
VERSION ?= dev
LDFLAGS := -s -w -X github.com/rengotaku/codex-run/cmd.Version=$(VERSION)

## deps: Download dependencies
deps:
	go mod download

## build: Build the binary into $(BIN_DIR)/$(BINARY_NAME)
build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME) .

## run: Run the CLI (use ARGS="..." to pass arguments)
run:
	go run main.go $(ARGS)

## lint: Run golangci-lint
lint:
	golangci-lint run

## test: Run tests
test:
	go test ./...

## test-cov: Run tests with coverage
test-cov:
	go test -coverprofile=$(COVERAGE_FILE) ./...
	go tool cover -func=$(COVERAGE_FILE)

## check: Run lint + test
check: lint test

## ci: Run lint + test with coverage
ci: lint test-cov

## clean: Remove build artifacts
clean:
	rm -rf $(BIN_DIR)/ $(COVERAGE_FILE)

## install: Build and install into $(PREFIX)/bin (default ~/.local/bin)
install: build
	@mkdir -p "$(dir $(INSTALL_DST))"
	@install -m 0755 "$(BIN_DIR)/$(BINARY_NAME)" "$(INSTALL_DST)"
	@echo "installed: $(INSTALL_DST)"

## uninstall: Remove the installed binary from $(PREFIX)/bin
uninstall:
	@rm -f "$(INSTALL_DST)"
	@echo "removed: $(INSTALL_DST)"

## help: Show this help
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
