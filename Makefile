
.PHONY: build run test fmt vet tidy compose-up compose-down web-dev \
        dev-db-up dev-db-down dev-server dev-web prod-up prod-down prod-logs

# Auto-load .env so `make dev-*` targets see TG_API_ID/MASTER_KEY/etc.
ifneq (,$(wildcard .env))
include .env
export
endif

build:
	go build -o bin/server ./cmd/server

run:
	go run ./cmd/server

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

tidy:
	go mod tidy

# ===== Local development =====
# Typical workflow:
#   1) make dev-db-up      (one-time: bring up Postgres on localhost:5432)
#   2) make dev-server     (terminal A: Go backend on :8080)
#   3) make dev-web        (terminal B: Vite dev server on :5173 with /api proxy)
#   open http://localhost:5173

dev-db-up:
	docker compose -f deploy/docker-compose.yml --env-file .env up -d postgres
	@echo "postgres listening on localhost:5432"

dev-db-down:
	docker compose -f deploy/docker-compose.yml --env-file .env down

dev-server:
	@mkdir -p tmp/cache
	go run ./cmd/server

dev-web:
	cd web && npm install && npm run dev

# ===== Local "prod-mode" testing (full compose with Caddy) =====
compose-up:
	cd deploy && docker compose --env-file ../.env up -d --build

compose-down:
	cd deploy && docker compose --env-file ../.env down

# ===== Production: pull pre-built images from GHCR =====
prod-up:
	cd deploy && docker compose -f docker-compose.prod.yml --env-file ../.env pull
	cd deploy && docker compose -f docker-compose.prod.yml --env-file ../.env up -d

prod-down:
	cd deploy && docker compose -f docker-compose.prod.yml --env-file ../.env down

prod-logs:
	cd deploy && docker compose -f docker-compose.prod.yml --env-file ../.env logs -f --tail=100

web-dev: dev-web