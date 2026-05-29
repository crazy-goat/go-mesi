module github.com/crazy-goat/go-mesi/servers/traefik

go 1.24

require (
	github.com/crazy-goat/go-mesi v0.0.0-20250204204515-1f5435b2af61
	github.com/redis/go-redis/v9 v9.20.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
)

replace github.com/crazy-goat/go-mesi v0.0.0-20250204204515-1f5435b2af61 => ../..
