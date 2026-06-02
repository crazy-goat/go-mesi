build: ## Build custom caddy with mesi module
	xcaddy build --with github.com/crazy-goat/go-mesi/servers/caddy=.

run: build ## Run caddy with local Caddyfile
	./caddy run --config Caddyfile

test: ## Run integration tests
	./test.sh

.PHONY: build run test
