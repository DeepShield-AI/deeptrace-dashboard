# DeepTrace Backend — build/test/lint tooling.
# Requires: go 1.26+, gofumpt, goimports, golangci-lint, staticcheck
# (install: `make tools`)

GO       ?= go
BIN      := deeptrace-server
TOOLS    := $(HOME)/go/bin

.PHONY: all build run test fmt lint vet staticcheck tools tidy clean check

all: fmt lint vet test build

## --- Build & run -----------------------------------------------------------

build:
	$(GO) build -o $(BIN) .

run: build
	./$(BIN)

tidy:
	$(GO) mod tidy

clean:
	rm -f $(BIN)

## --- Quality gates ---------------------------------------------------------

# gofumpt (stricter gofmt) + goimports (import grouping) — run before commit.
fmt:
	$(TOOLS)/gofumpt -w .
	$(TOOLS)/goimports -w .

# Check-only variant for CI / pre-commit.
fmt-check:
	@test -z "$$($(TOOLS)/gofumpt -l .)" || (echo "❌ gofumpt: files need formatting (run: make fmt)"; exit 1)
	@test -z "$$($(TOOLS)/goimports -l .)" || (echo "❌ goimports: files need formatting (run: make fmt)"; exit 1)

# golangci-lint (comprehensive; config in .golangci.yml).
lint:
	$(TOOLS)/golangci-lint run ./...

# staticcheck (deep static analysis).
staticcheck:
	$(TOOLS)/staticcheck ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

# Full quality gate — mirrors the pre-commit hook.
check: fmt-check lint vet test

## --- Toolchain bootstrap ---------------------------------------------------

tools:
	$(GO) install mvdan.cc/gofumpt@latest
	$(GO) install golang.org/x/tools/cmd/goimports@latest
	$(GO) install honnef.co/go/tools/cmd/staticcheck@latest
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
