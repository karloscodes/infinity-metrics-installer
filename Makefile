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
.PHONY: all build clean test test-local test-ci test-short lint deps help release build-linux install-multipass

all: test build

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

test-unit:
	$(GOTEST) -v ./installer/... ./pkg/...

# Run all tests (will use appropriate runner based on environment)
test:
	@if [ "$(IN_GITHUB_ACTIONS)" = "true" ]; then \
		make test-ci; \
	else \
		make test-local; \
	fi

# CI-specific testing (no multipass)
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
	
# Local testing with multipass
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
