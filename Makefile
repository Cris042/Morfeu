.PHONY: help up down obs-up obs-down gen migrate lint test build run

help:
	@echo "Morfeu makefile targets:"
	@echo "  up       - Start docker-compose (PG + Redis)"
	@echo "  down     - Stop docker-compose"
	@echo "  obs-up   - Start app + observability stack (docs/observabilidade.md)"
	@echo "  obs-down - Stop app + observability stack"
	@echo "  gen      - Generate sqlc code"
	@echo "  migrate  - Run database migrations"
	@echo "  lint     - Run golangci-lint"
	@echo "  test     - Run tests"
	@echo "  build    - Build the application"
	@echo "  run      - Run the application"

OBS_COMPOSE = docker compose -f docker-compose.yml -f docker-compose.observability.yml --profile app

up:
	docker compose up -d

down:
	docker compose down

obs-up:
	$(OBS_COMPOSE) up -d --build

obs-down:
	$(OBS_COMPOSE) down

gen:
	sqlc generate

migrate:
	migrate -path ./migrations -database $(DATABASE_URL) up

lint:
	golangci-lint run ./...

test:
	go test -race -cover ./...

build:
	CGO_ENABLED=0 go build -o app ./cmd/morfeu

run: build
	./app
