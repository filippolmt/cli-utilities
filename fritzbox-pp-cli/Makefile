.PHONY: build test lint install clean

BIN_EXT := $(if $(filter windows,$(shell go env GOOS)),.exe,)

build:
	go build -o bin/fritzbox-pp-cli$(BIN_EXT) ./cmd/fritzbox-pp-cli

test:
	go test ./...

lint:
	golangci-lint run

install:
	go install ./cmd/fritzbox-pp-cli

clean:
	rm -rf bin/

build-mcp:
	go build -o bin/fritzbox-pp-mcp$(BIN_EXT) ./cmd/fritzbox-pp-mcp

install-mcp:
	go install ./cmd/fritzbox-pp-mcp

build-all: build build-mcp
