# Makefile for Infinity Metrics Installer

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
BINARY_NAME=infinity-metrics
BINARY_DIR=bin
MAIN_PATH=cmd/infinitymetrics/main.go
ARCH ?= $(shell uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')

# Get version from file
VERSION := $(shell cat .version 2>/dev/null || echo "0.0.1")

# Check if running in GitHub Actions
IN_GITHUB_ACTIONS := $(if $(GITHUB_ACTIONS),true,false)

# Check if multipass is installed
MULTIPASS_INSTALLED := $(shell command -v multipass 2> /dev/null)

# Build targets
.PHONY: all build clean test test-local test-ci test-short lint deps help build-linux install-multipass start-test-vm

all: test build

deps:
	$(GOMOD) download
	$(GOMOD) tidy

build: 
	mkdir -p $(BINARY_DIR)
	$(GOBUILD) -o $(BINARY_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	chmod +x $(BINARY_DIR)/$(BINARY_NAME)

build-linux:
	mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -ldflags "-X main.currentInstallerVersion=$(VERSION)" -o $(BINARY_DIR)/$(BINARY_NAME)-v$(VERSION)-amd64 $(MAIN_PATH)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GOBUILD) -ldflags "-X main.currentInstallerVersion=$(VERSION)" -o $(BINARY_DIR)/$(BINARY_NAME)-v$(VERSION)-arm64 $(MAIN_PATH)
	chmod +x $(BINARY_DIR)/$(BINARY_NAME)-v*

clean:
	$(GOCLEAN)
	rm -rf $(BINARY_DIR)
	rm -rf coverage.out

# Install gotestsum (Go test runner with improved output)
install-gotestsum:
	@echo "Checking for gotestsum..."
	@if command -v gotestsum >/dev/null 2>&1; then \
		echo "gotestsum is already installed."; \
	else \
		echo "gotestsum not found. Installing..."; \
		GO111MODULE=on go install gotest.tools/gotestsum@latest; \
	fi

# Run all unit tests using gotestsum (in internal/*)
test-unit: install-gotestsum
	$(shell go env GOPATH)/bin/gotestsum --format=short-verbose -- ./internal/...

# Run all integration tests using go test (in tests/integration), mimicking previous working logic
test-integration: clean build-linux
	@if [ "$(ARCH)" = "arm64" ]; then \
		cp $(BINARY_DIR)/$(BINARY_NAME)-v$(VERSION)-arm64 $(BINARY_DIR)/$(BINARY_NAME); \
	else \
		cp $(BINARY_DIR)/$(BINARY_NAME)-v$(VERSION)-amd64 $(BINARY_DIR)/$(BINARY_NAME); \
	fi
	@echo "Running integration tests with KEEP_VM=$(KEEP_VM)"
	BINARY_PATH=$(shell pwd)/$(BINARY_DIR)/$(BINARY_NAME) \
	DEBUG=1 \
	$(GOTEST) -v ./tests/integration

# Run all tests (unit + integration)
test: install-gotestsum
	make test-unit
	make test-integration

# Run all tests (will use appropriate runner based on environment)
test-local: clean build-linux install-multipass
	@echo "Running tests in local environment"
	@if [ "$(ARCH)" = "arm64" ]; then \
		cp $(BINARY_DIR)/$(BINARY_NAME)-v$(VERSION)-arm64 $(BINARY_DIR)/$(BINARY_NAME); \
	else \
		cp $(BINARY_DIR)/$(BINARY_NAME)-v$(VERSION)-amd64 $(BINARY_DIR)/$(BINARY_NAME); \
	fi
	
	@echo "Running tests with KEEP_VM=$(KEEP_VM)"
	BINARY_PATH=$(shell pwd)/$(BINARY_DIR)/$(BINARY_NAME) \
	DEBUG=1 \
	$(GOTEST) -v ./tests
	
test-ci: build-linux
	@echo "Running tests in CI environment"
	@if [ "$(ARCH)" = "arm64" ]; then \
		cp $(BINARY_DIR)/$(BINARY_NAME)-v$(VERSION)-arm64 $(BINARY_DIR)/$(BINARY_NAME); \
	else \
		cp $(BINARY_DIR)/$(BINARY_NAME)-v$(VERSION)-amd64 $(BINARY_DIR)/$(BINARY_NAME); \
	fi
	
	@echo "Running all installer tests..."
	BINARY_PATH=$(shell pwd)/$(BINARY_DIR)/$(BINARY_NAME) \
	DEBUG=1 \
	$(GOTEST) -v ./tests
	
install-multipass:
ifeq ($(origin GITHUB_RUN_NUMBER), environment)
	@echo "Skipping Multipass installation in GitHub Actions"
else
	@echo "Checking for Multipass..."
	@if command -v multipass >/dev/null 2>&1; then \
		echo "Multipass is already installed."; \
	else \
		echo "Multipass not found. Installing..."; \
		UNAME_S=$$(uname -s); \
		if [ "$$UNAME_S" = "Linux" ]; then \
			sudo snap install multipass; \
		elif [ "$$UNAME_S" = "Darwin" ]; then \
			brew install --cask multipass; \
		else \
			echo "Unsupported OS for automatic Multipass installation. Please install manually: https://multipass.run/"; \
		fi; \
	fi
endif

start-test-vm:
	bash scripts/start-vm.sh

help:
	@echo "Available targets:"
	@echo "  build         - Build the binary"
	@echo "  build-linux   - Build Linux binaries for both architectures"
	@echo "  clean         - Clean build artifacts"
	@echo "  test          - Run all tests"
	@echo "  test-unit     - Run unit tests only"
	@echo "  test-integration - Run integration tests only"
	@echo "  test-local    - Run tests in local environment with Multipass"
	@echo "  test-ci       - Run tests in CI environment"
	@echo "  install-multipass - Install Multipass (if not already installed)"
	@echo "  start-test-vm - Start test VM using script"
	@echo "  deps          - Install dependencies"
