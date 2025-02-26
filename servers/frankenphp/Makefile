build:
	CGO_ENABLED=1 \
	XCADDY_GO_BUILD_FLAGS="-ldflags='-w -s' -tags=nobadger,nomysql,nopgx,nowatcher" \
	CGO_CFLAGS=$$(php-config --includes) \
	CGO_LDFLAGS="$$(php-config --ldflags) $$(php-config --libs)" \
	xcaddy build \
		--output frankenphp \
		--with github.com/dunglas/frankenphp \
		--with github.com/crazy-goat/go-mesi/servers/caddy=../caddy

run: build
	./frankenphp run --config Caddyfile