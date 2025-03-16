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

# OS detection for Multipass installation
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Linux)
    INSTALL_MULTIPASS = sudo snap install multipass
else ifeq ($(UNAME_S),Darwin)
    INSTALL_MULTIPASS = brew install --cask multipass
else
    INSTALL_MULTIPASS = echo "Unsupported OS for automatic Multipass installation. Please install manually: https://multipass.run/"
endif

# Check if multipass is installed
MULTIPASS_INSTALLED := $(shell command -v multipass 2> /dev/null)

# Build targets
.PHONY: all build clean test coverage lint deps help release multipass integration-tests build-linux

all: deps test build

build: 
	mkdir -p $(BINARY_DIR)
	$(GOBUILD) -o $(BINARY_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	chmod +x $(BINARY_DIR)/$(BINARY_NAME)

build-all: deps
	mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(BINARY_DIR)/$(BINARY_NAME)-linux-amd64 $(MAIN_PATH)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GOBUILD) -o $(BINARY_DIR)/$(BINARY_NAME)-linux-arm64 $(MAIN_PATH)
	chmod +x $(BINARY_DIR)/*

clean:
	$(GOCLEAN)
	rm -rf $(BINARY_DIR)
	rm -rf coverage.out

test:
	make integration-tests

coverage:
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out

lint:
	golangci-lint run ./...

deps:
	$(GOMOD) download
	$(GOMOD) tidy
	command -v golangci-lint >/dev/null 2>&1 || go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

release: build-all
	mkdir -p release
	tar -czvf release/infinity-metrics-installer-linux-amd64.tar.gz -C $(BINARY_DIR) $(BINARY_NAME)-linux-amd64
	tar -czvf release/infinity-metrics-installer-linux-arm64.tar.gz -C $(BINARY_DIR) $(BINARY_NAME)-linux-arm64
	sha256sum release/*.tar.gz > release/checksums.txt

multipass:
ifndef MULTIPASS_INSTALLED
	@echo "Multipass not found. Installing..."
	@$(INSTALL_MULTIPASS)
else
	@echo "Multipass is already installed."
endif

build-linux:
	mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GOBUILD) -o $(BINARY_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	chmod +x $(BINARY_DIR)/$(BINARY_NAME)
	file $(BINARY_DIR)/$(BINARY_NAME)

integration-tests: clean build-linux multipass
	@echo "Running integration tests with KEEP_VM=$(KEEP_VM)"
	@if [ "$(KEEP_VM)" = "1" ]; then \
		echo "Keeping VM after tests"; \
		BINARY_PATH=$(shell pwd)/$(BINARY_DIR)/$(BINARY_NAME) \
		$(GOTEST) -v -tags keepvm ./tests; \
		echo "VM kept, not deleting infinity-test-vm"; \
	else \
		echo "Not keeping VM, will delete after tests"; \
		BINARY_PATH=$(shell pwd)/$(BINARY_DIR)/$(BINARY_NAME) \
		$(GOTEST) -v ./tests; \
		multipass delete infinity-test-vm --purge || true; \
	fi

help:
	@echo "Available commands:"
	@echo "  make              : Build the project after running tests"
	@echo "  make build        : Build the binary"
	@echo "  make build-all    : Build for multiple platforms"
	@echo "  make clean        : Clean build files"
	@echo "  make test         : Run tests"
	@echo "  make coverage     : Run tests with coverage report"
	@echo "  make lint         : Run linting"
	@echo "  make deps         : Install dependencies"
	@echo "  make release      : Create release packages"
	@echo "  make integration-tests : Run integration tests"
	@echo "  make help         : Show this help message"
