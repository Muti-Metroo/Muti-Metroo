# Muti Metroo Makefile

.PHONY: all build test lint clean install run help

# Build variables
BINARY_NAME := muti-metroo
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"
LDFLAGS_RELEASE := -ldflags "-s -w -buildid= -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# Go parameters
GOCMD := go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOGET := $(GOCMD) get
GOMOD := $(GOCMD) mod
GOFMT := gofmt
GOVET := $(GOCMD) vet

# Directories
CMD_DIR := ./cmd/muti-metroo
BUILD_DIR := ./build
COVERAGE_DIR := ./coverage

all: lint test build

## build: Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)

## test: Run all tests
test:
	@echo "Running tests..."
	$(GOTEST) -v -race ./...

## test-coverage: Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	@mkdir -p $(COVERAGE_DIR)
	$(GOTEST) -v -race -coverprofile=$(COVERAGE_DIR)/coverage.out ./...
	$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html
	@echo "Coverage report: $(COVERAGE_DIR)/coverage.html"

## test-short: Run short tests only
test-short:
	@echo "Running short tests..."
	$(GOTEST) -v -short ./...

## lint: Run linters
lint:
	@echo "Running linters..."
	$(GOFMT) -s -l .
	$(GOVET) ./...

## fmt: Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) -s -w .

## clean: Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -rf $(COVERAGE_DIR)
	$(GOCMD) clean

## deps: Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

## install: Install the binary
install: build
	@echo "Installing $(BINARY_NAME)..."
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(GOPATH)/bin/

## run: Run the agent (requires config.yaml)
run: build
	$(BUILD_DIR)/$(BINARY_NAME) run -c ./config.yaml

## init-dev: Initialize development environment
init-dev:
	@echo "Initializing development environment..."
	@mkdir -p ./data ./certs
	$(BUILD_DIR)/$(BINARY_NAME) init -d ./data

## generate-certs: Generate self-signed certificates for development
generate-certs:
	@echo "Generating development certificates..."
	@mkdir -p ./certs
	openssl req -x509 -newkey rsa:4096 -keyout ./certs/agent.key -out ./certs/agent.crt \
		-days 365 -nodes -subj "/CN=muti-metroo-dev"
	openssl req -x509 -newkey rsa:4096 -keyout ./certs/ca.key -out ./certs/ca.crt \
		-days 365 -nodes -subj "/CN=muti-metroo-ca"

## build-dll: Build Windows DLL on Windows (requires GCC)
build-dll:
	@echo "Building $(BINARY_NAME).dll..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 \
		$(GOBUILD) -buildmode=c-shared -trimpath -buildvcs=false \
		$(LDFLAGS_RELEASE) -o $(BUILD_DIR)/$(BINARY_NAME).dll ./cmd/muti-dll
	@rm -f $(BUILD_DIR)/$(BINARY_NAME).h
	@echo "DLL built. For smaller size, compress with: upx --best --lzma $(BUILD_DIR)/$(BINARY_NAME).dll"

## build-dll-cross: Cross-compile Windows amd64 DLL from macOS/Linux (requires mingw-w64)
build-dll-cross:
	@echo "Cross-compiling $(BINARY_NAME)-windows-amd64.dll..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc GOOS=windows GOARCH=amd64 \
		$(GOBUILD) -buildmode=c-shared -trimpath -buildvcs=false \
		$(LDFLAGS_RELEASE) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.dll ./cmd/muti-dll
	@rm -f $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.h
	@echo "DLL built. For smaller size, compress with: upx --best --lzma $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.dll"

## build-dll-cross-arm64: Cross-compile Windows ARM64 DLL from macOS/Linux (requires llvm-mingw)
##                       Install: https://github.com/mstorsjo/llvm-mingw -- provides aarch64-w64-mingw32-clang
build-dll-cross-arm64:
	@echo "Cross-compiling $(BINARY_NAME)-windows-arm64.dll..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 CC=aarch64-w64-mingw32-clang GOOS=windows GOARCH=arm64 \
		$(GOBUILD) -buildmode=c-shared -trimpath -buildvcs=false \
		$(LDFLAGS_RELEASE) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-arm64.dll ./cmd/muti-dll
	@rm -f $(BUILD_DIR)/$(BINARY_NAME)-windows-arm64.h
	@echo "DLL built. For smaller size, compress with: upx --best --lzma $(BUILD_DIR)/$(BINARY_NAME)-windows-arm64.dll"

## help: Show this help
help:
	@echo "Muti Metroo - Userspace Mesh Networking Agent"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'
