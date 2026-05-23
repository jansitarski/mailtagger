.PHONY: build test lint run clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags="-s -w -X main.version=$(VERSION)"

build:
	CGO_ENABLED=0 go build $(LDFLAGS) -trimpath -o bin/mailtagger ./cmd/mailtagger

test:
	go test -v -race ./...

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed"; exit 1; }
	golangci-lint run ./...

run: build
	./bin/mailtagger serve --addr :8080

clean:
	rm -rf bin/
