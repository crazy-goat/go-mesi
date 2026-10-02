module github.com/crazy-goat/go-mesi/cli

go 1.24

replace github.com/crazy-goat/go-mesi => ..

require (
	github.com/bradfitz/gomemcache v0.0.0-20250403215159-8d39553ac7cf
	github.com/crazy-goat/go-mesi v0.5.0
	github.com/redis/go-redis/v9 v9.22.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sys v0.30.0 // indirect
)
