# sandboxd — one entrypoint for both subprojects.
#   sandboxd core → control-plane/ (Go)     console → console/ (Vite/React)
.DEFAULT_GOAL := help
.PHONY: help test test-core test-console lint fmt build up down logs clean

CP := control-plane
CONSOLE := console

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

test: test-core test-console ## Run all tests

test-core: ## Go tests (sandboxd core)
	cd $(CP) && go test ./...

test-console: ## Console unit tests
	cd $(CONSOLE) && pnpm install --frozen-lockfile && pnpm test

lint: ## gofmt + go vet + console typecheck
	cd $(CP) && test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)
	cd $(CP) && go vet ./...
	cd $(CONSOLE) && pnpm run typecheck

fmt: ## Format Go code
	cd $(CP) && gofmt -w .

build: ## Build the control-plane binary + console bundle
	cd $(CP) && CGO_ENABLED=0 go build -o /dev/null ./cmd/sandboxd ./cmd/runtimed
	cd $(CONSOLE) && pnpm install --frozen-lockfile && pnpm run build

up: ## Bring up control plane + edge + console
	docker compose --profile console up -d --build

down: ## Tear it down
	docker compose --profile console down

logs: ## Tail the control plane
	docker compose logs -f sandboxd

clean: ## Remove local build artifacts
	rm -f $(CP)/sandboxd $(CP)/runtimed
	rm -rf $(CONSOLE)/dist
