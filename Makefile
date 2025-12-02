.PHONY: run build test clean deps db-up db-down migrate

# Load .env if exists
ifneq (,$(wildcard ./.env))
    include .env
    export
endif

# Default database URL
DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/gateway?sslmode=disable

run:
	go run cmd/gateway/main.go

build:
	go build -o bin/gateway cmd/gateway/main.go

test:
	go test -v ./...

clean:
	rm -rf bin/

deps:
	go mod tidy
	go mod download

# Docker commands
db-up:
	docker-compose up -d db redis

db-down:
	docker-compose down

# Run migrations
migrate:
	psql $(DATABASE_URL) -f migrations/001_initial.sql

# Load testing (requires k6)
load-test:
	k6 run tests/load.js

# Development helpers
logs:
	docker-compose logs -f

psql:
	psql $(DATABASE_URL)
