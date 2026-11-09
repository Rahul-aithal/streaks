.PHONY: install dev build build-api build-web test lint migrate migrate-status deploy-worker


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
	pnpm --filter worker typecheck
	pnpm exec oxlint apps/web/src apps/worker/src

migrate: build-api
	./$(BIN) migrate up

migrate-status: build-api
	./$(BIN) migrate status

# Builds the web app and deploys it with the Worker. Run `wrangler login` once first.
deploy-worker:
	pnpm --filter worker run deploy
