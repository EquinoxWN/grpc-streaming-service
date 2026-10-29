.PHONY: setup lint generate generate-check breaking baseline test audit build demo ci

EXE := $(if $(filter Windows_NT,$(OS)),.exe,)
BIN := .tmp/bin
BUF := $(BIN)/buf$(EXE)
# The race detector needs cgo; CI (Linux) has it, a plain Windows Go install does not.
RACE ?= $(if $(filter Windows_NT,$(OS)),,-race)
export BUF_CACHE_DIR := $(CURDIR)/.tmp/buf-cache

# Go modules, buf (SHA-256 checked) and the pinned protoc-gen-go / protoc-gen-go-grpc.
setup:
	go mod download
	bash scripts/get-buf.sh

lint:
	@files="$$(gofmt -l cmd internal)"; test -z "$$files" || (echo "$$files"; echo "run gofmt -w cmd internal"; exit 1)
	go vet ./...
	$(BUF) lint
	$(BUF) format --diff --exit-code

generate:
	$(BUF) generate

# Generated code must match the contract: regenerate into .tmp and compare.
generate-check:
	rm -rf .tmp/gen-check && $(BUF) generate --output .tmp/gen-check
	diff -r gen .tmp/gen-check/gen

# The contract must stay compatible with contract/ticker-v1.binpb; the script also proves that
# four kinds of breaking edits are refused.
breaking:
	bash scripts/breaking-check.sh

# Accept the current contract as the new baseline (only for an intended, versioned change).
baseline:
	$(BUF) build -o contract/ticker-v1.binpb

test:
	go test $(RACE) -count=1 ./...

# Known vulnerabilities in the code paths actually called (Go 1.26.10, as in CI).
audit:
	go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...

build:
	go build -trimpath -o $(BIN)/ticker-server$(EXE) ./cmd/ticker-server
	go build -trimpath -o $(BIN)/tickerctl$(EXE) ./cmd/tickerctl

# Starts the server, runs the three call types against it, stops the server.
demo: build
	bash scripts/demo.sh

ci: setup lint generate-check breaking test
