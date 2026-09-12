package traefik

import (
	"time"

	"github.com/crazy-goat/go-mesi/mesi"
)

func newMemoryCache(size int, ttl time.Duration) mesi.Cache {
	return mesi.NewMemoryCache(size, ttl)
}
