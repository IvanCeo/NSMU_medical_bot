GO ?= go
BIN ?= bin/bot

.PHONY: setup run test test-race lint fmt build migrate

setup:
	cp -n .env.example .env || true
	$(GO) mod download

run:
	$(GO) run ./cmd/bot

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

lint: fmt
	$(GO) vet ./...
	golangci-lint run

fmt:
	gofmt -s -w .

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o $(BIN) ./cmd/bot

migrate:
	goose -dir migrations sqlite3 "$(DB_PATH)" up