.PHONY: install dev build build-api build-web test lint migrate migrate-status google-auth

BIN := bin/streaks

install:
	pnpm install

dev: build-api
	@./scripts/dev.sh

build: build-api build-web

build-api:
	cd apps/api && go build -trimpath -ldflags="-s -w" -o ../../$(BIN) ./cmd/streaks

build-web:
	pnpm --filter web build

test:
	cd apps/api && go test ./...
	pnpm --filter web typecheck

lint:
	cd apps/api && go vet ./...
	@out="$$(cd apps/api && gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	pnpm --filter web typecheck

migrate: build-api
	./$(BIN) migrate up

migrate-status: build-api
	./$(BIN) migrate status

google-auth: build-api
	./$(BIN) google-auth
