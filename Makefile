# Tool versions that are not pinned in go.mod (they run with `go run`).
BUF_VERSION           := v1.73.0
GOLANGCI_LINT_VERSION := v2.14.0

# Build the tools with the project's Go version (go.mod), so the linter
# understands the newest language features.
GO_TOOLCHAIN  := $(shell go env GOVERSION)
BUF           := GOTOOLCHAIN=$(GO_TOOLCHAIN) go run github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION)
GOLANGCI_LINT := GOTOOLCHAIN=$(GO_TOOLCHAIN) go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

COMPOSE_CORE     := docker compose -f docker-compose.yml
COMPOSE_OPTIONAL := docker compose -f docker-compose.optional.yml

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

## ---- Development

.PHONY: env
env: ## Create .env from .env.example (if missing)
	@test -f .env || cp .env.example .env

.PHONY: run
run: ## Run the service
	go run ./cmd/app

.PHONY: build
build: ## Build binaries into ./bin
	CGO_ENABLED=0 go build -trimpath -o bin/app ./cmd/app
	CGO_ENABLED=0 go build -trimpath -o bin/migrate ./cmd/migrate

.PHONY: test
test: ## Run unit tests
	go test -race ./...

.PHONY: cover
cover: ## Run unit tests with coverage report (coverage.html)
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -n 1
	go tool cover -html=coverage.out -o coverage.html

.PHONY: lint
lint: ## Run golangci-lint and buf lint
	$(GOLANGCI_LINT) run ./...
	$(BUF) lint

.PHONY: fmt
fmt: ## Format code
	$(GOLANGCI_LINT) fmt ./...
	$(BUF) format -w

.PHONY: tidy
tidy: ## Tidy go.mod
	go mod tidy

## ---- Code generation

.PHONY: generate
generate: proto mocks ## Generate protobuf code and mocks

.PHONY: proto
proto: ## Generate Go code from api/proto
	$(BUF) generate

.PHONY: mocks
mocks: ## Generate mocks (go:generate directives in ports.go files)
	go generate ./...

## ---- Database

.PHONY: migrate-up
migrate-up: ## Apply schema migrations
	go run ./cmd/migrate up

.PHONY: migrate-down
migrate-down: ## Roll back the last schema migration
	go run ./cmd/migrate down 1

.PHONY: migrate-version
migrate-version: ## Show the current schema version
	go run ./cmd/migrate version

.PHONY: migrate-create
migrate-create: ## Create a migration pair: make migrate-create name=add_orders_table
	@test -n "$(name)" || (echo "usage: make migrate-create name=<snake_case_name>" && exit 1)
	@n=$$(ls migrations/*.up.sql 2>/dev/null | wc -l | tr -d ' '); v=$$(printf "%06d" $$((n + 1))); \
	touch migrations/$${v}_$(name).up.sql migrations/$${v}_$(name).down.sql; \
	echo "created migrations/$${v}_$(name).{up,down}.sql"

.PHONY: seed
seed: ## Insert dummy data (local only)
	go run ./cmd/migrate seed

.PHONY: seed-down
seed-down: ## Remove dummy data
	go run ./cmd/migrate seed-down

## ---- Local infrastructure

.PHONY: infra-up
infra-up: ## Start PostgreSQL + Redis
	$(COMPOSE_CORE) up -d --wait

.PHONY: infra-mq-up
infra-mq-up: ## Start RabbitMQ
	$(COMPOSE_OPTIONAL) --profile mq up -d --wait

.PHONY: infra-otel-up
infra-otel-up: ## Start OTel Collector, Tempo, Loki, Prometheus, Grafana
	$(COMPOSE_OPTIONAL) --profile otel up -d

.PHONY: infra-all-up
infra-all-up: infra-up ## Start every dependency
	$(COMPOSE_OPTIONAL) --profile mq --profile otel up -d

.PHONY: infra-down
infra-down: ## Stop every dependency (data volumes are kept)
	$(COMPOSE_OPTIONAL) --profile mq --profile otel down
	$(COMPOSE_CORE) down

.PHONY: infra-reset
infra-reset: ## Stop every dependency and DELETE their data volumes
	$(COMPOSE_OPTIONAL) --profile mq --profile otel down -v
	$(COMPOSE_CORE) down -v

## ---- Docker image

.PHONY: docker-build
docker-build: ## Build the service image
	docker build -t codebase-go:local .
