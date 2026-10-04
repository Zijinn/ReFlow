.PHONY: build check cli dev dev-server dev-web format test

build:
	pnpm --dir web build
	go build -o bin/reflow-server ./cmd/reflow-server
	go build -o bin/reflow ./cmd/reflow

# The CLI is Go-only, so it builds without the web bundle or the desktop chain.
cli:
	go build -o bin/reflow ./cmd/reflow

check:
	@test -z "$$(gofmt -l cmd internal)" || { echo "gofmt needed:"; gofmt -l cmd internal; exit 1; }
	go test ./...
	go vet ./...
	pnpm --dir web typecheck
	pnpm --dir web lint
	pnpm --dir web test
	pnpm --dir web build

dev:
	pnpm dev

dev-server:
	go run ./cmd/reflow-server

dev-web:
	pnpm --dir web dev

format:
	gofmt -w cmd internal
	pnpm --dir web format

test:
	go test ./...
	pnpm --dir web test
