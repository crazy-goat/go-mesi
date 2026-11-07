module github.com/crazy-goat/go-mesi/servers/traefik

go 1.26.0

require (
	github.com/bradfitz/gomemcache v0.0.0-20250403215159-8d39553ac7cf
	github.com/crazy-goat/go-mesi v0.0.0-20250204204515-1f5435b2af61
	github.com/redis/go-redis/v9 v9.23.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	go.uber.org/atomic v1.12.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
)

replace github.com/crazy-goat/go-mesi v0.0.0-20250204204515-1f5435b2af61 => ../..
