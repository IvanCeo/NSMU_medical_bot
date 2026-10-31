GO ?= go
GOLANGCI_LINT_VERSION ?= v2.13.2
GOOSE_VERSION ?= v3.26.0

ifeq ($(OS),Windows_NT)
SHELL := cmd.exe
BIN ?= bin/bot.exe
BACKUP_BIN ?= bin/backup.exe
else
BIN ?= bin/bot
BACKUP_BIN ?= bin/backup
endif

.PHONY: setup env tools run test test-race lint fmt build build-backup migrate test-coverage docker-build docker-up docker-down docker-logs docker-check backup

setup: env tools
	$(GO) mod download

env:
ifeq ($(OS),Windows_NT)
	@if not exist ".env" copy /y ".env.example" ".env" >NUL
else
	@test -e .env || cp .env.example .env
endif

tools:
ifeq ($(OS),Windows_NT)
	@golangci-lint version >NUL 2>&1 || $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@goose -version >NUL 2>&1 || $(GO) install github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION)
else
	@command -v golangci-lint >/dev/null 2>&1 || $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@command -v goose >/dev/null 2>&1 || $(GO) install github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION)
endif

run:
	$(GO) run ./cmd/bot

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

test-coverage:
	$(GO) test -coverpkg=./internal/service -coverprofile=coverage.out ./internal/service ./internal/repository/sqlite ./internal/bot
	$(GO) tool cover -func=coverage.out

lint: fmt
	$(GO) vet ./...
	golangci-lint run

fmt:
	gofmt -s -w cmd internal migrations

build:
ifeq ($(OS),Windows_NT)
	set "CGO_ENABLED=0"&& $(GO) build -trimpath -ldflags="-s -w" -o "$(BIN)" ./cmd/bot
else
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o "$(BIN)" ./cmd/bot
endif

migrate:
ifeq ($(OS),Windows_NT)
	$(if $(DB_PATH),set "DB_PATH=$(DB_PATH)"&& )$(GO) run ./cmd/migrate
else
	$(if $(DB_PATH),DB_PATH="$(DB_PATH)" )$(GO) run ./cmd/migrate
endif

build-backup:
ifeq ($(OS),Windows_NT)
	set "CGO_ENABLED=0"&& $(GO) build -trimpath -ldflags="-s -w" -o "$(BACKUP_BIN)" ./cmd/backup
else
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o "$(BACKUP_BIN)" ./cmd/backup
endif

docker-build:
	docker compose build

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f bot

docker-check:
	docker compose config --quiet

# Local only: stop the bot first; start it separately after the copy.
backup:
	docker compose run --rm --no-deps backup
