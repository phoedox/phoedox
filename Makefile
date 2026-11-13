.PHONY: dev web build test lint

web:
	cd web && bun install && bun run build

build: web
	go build -o bin/phoedox ./cmd/phoedox
	go build -o bin/phx ./cmd/phx

test:
	go test ./...
	cd web && bun test

lint:
	gofmt -l . | (! grep .)
	go vet ./...
	cd web && bun run typecheck

dev:
	@echo "Terminal 1: go run ./cmd/phoedox   Terminal 2: cd web && bun run dev"
