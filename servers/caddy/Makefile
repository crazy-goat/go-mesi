build:
	xcaddy build --tags "memcached redis" --with github.com/crazy-goat/go-mesi/servers/caddy=.

run: build
	./caddy run --config Caddyfile