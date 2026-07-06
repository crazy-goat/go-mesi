package roadrunner

import (
	"testing"
	"time"
)

// TestInitMemoryCacheAcceptsMaxSize verifies that the documented
// upper bound MaxCacheSize is accepted by Init (boundary: max).
func TestInitMemoryCacheAcceptsMaxSize(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "60s"
	config.CacheSize = MaxCacheSize

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Init rejected MaxCacheSize (%d): %v", MaxCacheSize, err)
	}
	if p.cache == nil {
		t.Fatal("expected non-nil cache for size == MaxCacheSize")
	}
}

// TestInitMemoryCacheRejectsOversize verifies the boundary class
// above the upper bound is explicitly rejected — silent overflow or
// silent fallback to the documented default is an anti-pattern.
func TestInitMemoryCacheRejectsOversize(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "60s"
	config.CacheSize = MaxCacheSize + 1

	p := &Plugin{config: config}
	err := p.Init()
	if err == nil {
		t.Fatal("expected error for cache_size above MaxCacheSize, got nil")
	}
	// Cache must remain nil on rejection so a failed init cannot
	// leave a partial / wrong-sized cache behind.
	if p.cache != nil {
		t.Fatal("expected nil cache on rejected size")
	}
}

// TestInitMemoryCacheZeroSizeUsesDefault verifies the documented
// silent-default contract: cache_size == 0 falls back to
// DefaultCacheSize. This matches caddy / apache behaviour and the
// README so operators may omit the directive.
func TestInitMemoryCacheZeroSizeUsesDefault(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "60s"
	config.CacheSize = 0

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Init rejected cache_size=0 (documented default): %v", err)
	}
	if p.cache == nil {
		t.Fatal("expected non-nil cache for cache_size=0 (default fallback)")
	}
}

// TestInitMemoryCacheNegativeSizeUsesDefault mirrors the zero case:
// a hint like -1 (some mapstructure sources emit -1 for "unset")
// must fall back to the documented default rather than be rejected.
func TestInitMemoryCacheNegativeSizeUsesDefault(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "60s"
	config.CacheSize = -7

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Init rejected negative cache_size (documented default): %v", err)
	}
	if p.cache == nil {
		t.Fatal("expected non-nil cache for negative cache_size (default fallback)")
	}
}

// TestInitMemoryCacheAcceptedSizesBoundedTable walks a representative
// selection of in-range sizes (1, a small value, and MaxCacheSize).
// Outside that range the boundary tests above already cover rejection.
func TestInitMemoryCacheAcceptedSizesBoundedTable(t *testing.T) {
	for _, size := range []int{1, 100, 50_000, MaxCacheSize} {
		config := CreateConfig()
		config.CacheBackend = "memory"
		config.CacheTTL = "60s"
		config.CacheSize = size

		p := &Plugin{config: config}
		if err := p.Init(); err != nil {
			t.Errorf("Init rejected valid cache_size=%d: %v", size, err)
			continue
		}
		if p.cache == nil {
			t.Errorf("Init accepted cache_size=%d but cache is nil", size)
		}
	}
}

// TestInitCacheTTLInvalidStringRejected guards the parse path: a
// non-duration string must produce a wrapped error so operator typos
// surface in plugin init rather than silently degrading to
// initCache's "no TTL" branch.
func TestInitCacheTTLInvalidStringRejected(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "not-a-duration"

	p := &Plugin{config: config}
	err := p.Init()
	if err == nil {
		t.Fatal("expected error for unparseable cache_ttl, got nil")
	}
}

// TestInitCacheTTLNegativeRejected closes the silent-bug class where
// time.ParseDuration accepts strings like "-1s" without warning.
// mesi.NewMemoryCache would translate that into "no expiry" (cache
// forever) at runtime, which is the opposite of what the operator
// typed — the plugin must reject up-front.
func TestInitCacheTTLNegativeRejected(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "-1s"

	p := &Plugin{config: config}
	err := p.Init()
	if err == nil {
		t.Fatal("expected error for negative cache_ttl, got nil")
	}
	if p.cache != nil {
		t.Fatal("expected nil cache on rejected TTL")
	}
}

// TestInitCacheTTLAtMaxAccepted verifies the boundary class
// MaxCacheTTL (24h) is the inclusive upper bound.
func TestInitCacheTTLAtMaxAccepted(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = MaxCacheTTL.String()

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Init rejected MaxCacheTTL: %v", err)
	}
	if p.cacheTTL != MaxCacheTTL {
		t.Fatalf("expected cacheTTL=%s, got %s", MaxCacheTTL, p.cacheTTL)
	}
}

// TestInitCacheTTLJustAboveMaxRejected verifies that exceeding
// MaxCacheTTL by one second is rejected: 24h+1s is the next hour
// bucket and we want operators to either shorten or accept the bound.
func TestInitCacheTTLJustAboveMaxRejected(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = (MaxCacheTTL + time.Second).String()

	p := &Plugin{config: config}
	err := p.Init()
	if err == nil {
		t.Fatal("expected error for cache_ttl > MaxCacheTTL, got nil")
	}
}

// TestInitCacheTTLEmptyIsNoExpiry matches the documented behaviour:
// an empty cache_ttl means "no expiry" (Duration 0 in cache config).
func TestInitCacheTTLEmptyIsNoExpiry(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = ""

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Init rejected empty cache_ttl: %v", err)
	}
	if p.cacheTTL != 0 {
		t.Fatalf("expected cacheTTL=0 for empty string, got %s", p.cacheTTL)
	}
	if p.cache == nil {
		t.Fatal("expected non-nil cache even when TTL is unset")
	}
}

// TestInitCacheTTLWithoutBackendIgnored checks that an explicit
// cache_ttl with no cache_backend is not used as cache init input
// (matching the existing caddy behaviour) — the parse path is gated
// on CacheBackend != "" so neither a valid nor invalid TTL can affect
// plugin init when caching is disabled. cacheTTL stays 0 in that
// case so initCache sees no expiry option.
func TestInitCacheTTLWithoutBackendIgnored(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = ""
	config.CacheTTL = "garbage"

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Init rejected cache_ttl when CacheBackend is empty: %v", err)
	}
	if p.cache != nil {
		t.Fatal("expected nil cache when CacheBackend is empty")
	}
	if p.cacheTTL != 0 {
		t.Fatalf("expected cacheTTL=0 when CacheBackend is empty, got %s", p.cacheTTL)
	}
}
