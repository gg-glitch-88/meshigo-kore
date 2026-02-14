##############################################################################
# MeshCommons – Makefile
# Targets: build, test, lint, clean, run, proto, install-deps
##############################################################################

BINARY     := meshcommons
CMD_DIR    := ./cmd/meshcommons
BUILD_DIR  := ./bin
PROTO_DIR  := ./proto
GEN_DIR    := ./internal/proto

# Go toolchain
GO         := go
GOFLAGS    := -trimpath
LDFLAGS    := -s -w

# Versioning
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
VERSION    := $(shell git describe --tags --always 2>/dev/null || echo "0.0.1")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS += -X github.com/meshcommons/meshcommons/internal/version.Commit=$(GIT_COMMIT)
LDFLAGS += -X github.com/meshcommons/meshcommons/internal/version.Version=$(VERSION)
LDFLAGS += -X github.com/meshcommons/meshcommons/internal/version.BuildTime=$(BUILD_TIME)

.PHONY: all build test lint clean run proto fmt vet tidy install-deps coverage

##@ General

all: build ## Default target: build the binary

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} \
	     /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 } \
	     /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)

##@ Build

build: ## Compile the binary to ./bin/
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) $(CMD_DIR)
	@echo "→ Built $(BUILD_DIR)/$(BINARY)"

build-arm64: ## Cross-compile for Raspberry Pi (ARM64 / Debian Bookworm)
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=1 \
	CC=aarch64-linux-gnu-gcc \
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-arm64 $(CMD_DIR)
	@echo "→ Built $(BUILD_DIR)/$(BINARY)-arm64"

run: build ## Build and run with default config
	$(BUILD_DIR)/$(BINARY) -config configs/default.yaml

##@ Code Quality

fmt: ## Run gofmt on all Go source
	$(GO) fmt ./...

vet: ## Run go vet
	$(GO) vet ./...

lint: ## Run golangci-lint (install separately)
	golangci-lint run ./...

tidy: ## Tidy and verify go.mod / go.sum
	$(GO) mod tidy
	$(GO) mod verify

##@ Testing

test: ## Run all unit tests
	$(GO) test -race -timeout 120s ./...

test-verbose: ## Run tests with verbose output
	$(GO) test -race -v -timeout 120s ./...

coverage: ## Generate HTML coverage report
	@mkdir -p $(BUILD_DIR)
	$(GO) test -race -coverprofile=$(BUILD_DIR)/coverage.out ./...
	$(GO) tool cover -html=$(BUILD_DIR)/coverage.out -o $(BUILD_DIR)/coverage.html
	@echo "→ Coverage report: $(BUILD_DIR)/coverage.html"

bench: ## Run benchmarks
	$(GO) test -bench=. -benchmem ./...

##@ Code Generation

proto: ## Generate Go code from .proto files
	@which protoc > /dev/null 2>&1 || (echo "protoc not found – run 'make install-deps'" && exit 1)
	@mkdir -p $(GEN_DIR)
	protoc --go_out=$(GEN_DIR) --go_opt=paths=source_relative \
	       --proto_path=$(PROTO_DIR) $(PROTO_DIR)/*.proto
	@echo "→ Protobuf generated in $(GEN_DIR)"

##@ Storage

migrate: ## Run SQLite schema migrations
	$(GO) run ./cmd/migrate

##@ Maintenance

clean: ## Remove build artifacts
	rm -rf $(BUILD_DIR)
	@echo "→ Cleaned"

install-deps: ## Install required system and Go tools
	@echo "Installing Go tools..."
	$(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "→ Done. Make sure protoc is installed via apt: apt install protobuf-compiler"
