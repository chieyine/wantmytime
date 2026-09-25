.PHONY: bootstrap dev stack stack-down web api db-up db-down migrate verify build

bootstrap:
	cd apps/web && npm ci

dev:
	docker compose up --build -d db api
	$(MAKE) web

stack:
	docker compose up --build

stack-down:
	docker compose down

web:
	cd apps/web && npm run dev -- --host 127.0.0.1

api:
	cd services/core && go run ./cmd/api migrate
	cd services/core && go run ./cmd/api

db-up:
	docker compose up -d db

db-down:
	docker compose down

migrate:
	docker compose run --rm migrate

verify:
	cd apps/web && npm run check && npm run build
	cd services/core && go test ./... && go vet ./...

build:
	cd apps/web && npm run build
