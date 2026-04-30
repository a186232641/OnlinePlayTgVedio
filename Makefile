.PHONY: build run test fmt vet tidy compose-up compose-down web-dev

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

compose-up:
	cd deploy && docker compose --env-file ../.env up -d --build

compose-down:
	cd deploy && docker compose --env-file ../.env down

# Production: pull pre-built images from GHCR and run.
prod-up:
	cd deploy && docker compose -f docker-compose.prod.yml --env-file ../.env pull
	cd deploy && docker compose -f docker-compose.prod.yml --env-file ../.env up -d

prod-down:
	cd deploy && docker compose -f docker-compose.prod.yml --env-file ../.env down

prod-logs:
	cd deploy && docker compose -f docker-compose.prod.yml --env-file ../.env logs -f --tail=100

web-dev:
	cd web && npm run dev
