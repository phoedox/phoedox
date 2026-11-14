.PHONY: dev web website build test lint

web:
	cd web && bun install && bun run build

website:
	cd website && bun install && bun run build

build: web website
	go build -o bin/phoedox ./cmd/phoedox
	go build -o bin/phx ./cmd/phx
	go build -o bin/phoedox-website ./cmd/phoedox-website

test:
	go test ./...
	cd web && bun test
	cd website && bun test

lint:
	gofmt -l . | (! grep .)
	go vet ./...
	cd web && bun run typecheck
	cd website && bun run typecheck

dev:
	@echo "Terminal 1: go run ./cmd/phoedox   Terminal 2: cd web && bun run dev"
