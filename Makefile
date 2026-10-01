.PHONY: help up down obs-up obs-down preflight backup-teste gen migrate lint test build run

help:
	@echo "Morfeu makefile targets:"
	@echo "  up       - Start docker-compose (PG + Redis)"
	@echo "  down     - Stop docker-compose"
	@echo "  obs-up   - Start app + observability stack (docs/observabilidade.md)"
	@echo "  obs-down - Stop app + observability stack"
	@echo "  preflight - Validate .env.prod and .env.observability (no placeholders, strong passwords)"
	@echo "  backup-teste - Backup/restore round trip (image build + PG; needs the Docker socket)"
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

preflight:
	bash scripts/preflight.sh .env.prod .env.observability

# Ida e volta do backup (task 0043); BACKUP_TESTE_S3=1 inclui o ciclo contra um servidor S3 (rclone serve s3).
backup-teste:
	docker run --rm -v "$(CURDIR)":/src -w /src -v /var/run/docker.sock:/var/run/docker.sock \
	  -v morfeu-gomodcache:/go/pkg/mod -e TESTCONTAINERS_RYUK_DISABLED=true -e BACKUP_TESTE_S3 \
	  golang:1.25 go test -race -tags=integration -run TestBackup -v ./test/infra/

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
