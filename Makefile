# Build into bin/, which is gitignored.
BINARY  := bin/ax
PKG     := ./cmd/ax
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.Version=$(VERSION)

.PHONY: build install test vet fmt lint clean run

build:
	@mkdir -p $(dir $(BINARY))
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

install:
	CGO_ENABLED=0 go install -trimpath -ldflags "$(LDFLAGS)" $(PKG)

test:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w -s .

lint:
	golangci-lint run

clean:
	rm -rf bin dist

run: build
	./$(BINARY) $(ARGS)
