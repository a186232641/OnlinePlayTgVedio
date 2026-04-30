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

web-dev:
	cd web && npm run dev
