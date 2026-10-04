.PHONY: build test lint install clean

BIN_EXT := $(if $(filter windows,$(shell go env GOOS)),.exe,)

build:
	go build -trimpath -o bin/subito-pp-cli$(BIN_EXT) ./cmd/subito-pp-cli

test:
	go test ./...

lint:
	golangci-lint run

install:
	go install ./cmd/subito-pp-cli

clean:
	rm -rf bin/

build-mcp:
	go build -trimpath -o bin/subito-pp-mcp$(BIN_EXT) ./cmd/subito-pp-mcp

install-mcp:
	go install ./cmd/subito-pp-mcp

build-all: build build-mcp
