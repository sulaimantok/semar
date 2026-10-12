GO      ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -s -w \
	-X github.com/sulaimantok/semar/internal/version.Version=$(VERSION) \
	-X github.com/sulaimantok/semar/internal/version.Commit=$(COMMIT)

.PHONY: all fmt vet test lint build run-api image clean

all: vet test build

fmt:
	gofmt -w .

vet:
	$(GO) vet ./...

test:
	$(GO) test -race -count=1 ./...

lint:
	golangci-lint run ./...

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/ ./cmd/semar-api ./cmd/semar-worker

run-api: build
	./bin/semar-api

# Multi-arch image; build arm64 natively on the ARM64 build host when possible.
image:
	docker buildx build --platform linux/amd64,linux/arm64 \
		--build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t semar:$(VERSION) .

COMPOSE := docker compose -f deploy/compose/compose.lite.yaml

.PHONY: lite-up lite-down lite-logs
lite-up:
	$(COMPOSE) up -d --build

lite-down:
	$(COMPOSE) down

lite-logs:
	$(COMPOSE) logs -f semar-worker

clean:
	rm -rf bin coverage.out

.PHONY: generate check-generate
generate:
	$(GO) generate ./...

# Fails if generated code is out of date with api/openapi.yaml.
check-generate: generate
	git diff --exit-code -- internal/api/gen
