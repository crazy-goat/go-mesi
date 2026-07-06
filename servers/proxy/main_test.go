package main

import (
	"testing"
	"time"

	"github.com/crazy-goat/go-mesi/mesi"
)

func TestCacheLabel(t *testing.T) {
	tests := []struct {
		backend string
		want    string
	}{
		{"", "off"},
		{"memory", "memory"},
		{"redis", "redis"},
		{"memcached", "memcached"},
	}
	for _, tt := range tests {
		got := cacheLabel(tt.backend)
		if got != tt.want {
			t.Errorf("cacheLabel(%q) = %q, want %q", tt.backend, got, tt.want)
		}
	}
}

func TestValidateHostPort(t *testing.T) {
	tests := []struct {
		addr string
		ok   bool
	}{
		{"localhost:6379", true},
		{"127.0.0.1:11211", true},
		{"redis.example.com:6379", true},
		{"10.0.0.5:6379", true},
		{"host:1", true},
		{"host:65535", true},
		// invalid
		{"", false},
		{"no-port", false},
		{"host:0", false},
		{"host:65536", false},
		{"host:-1", false},
		{"host:abc", false},
		{":6379", false},
		// Note: leading/trailing whitespace in CLI flags is unusual;
		// net.SplitHostPort does not trim, so such inputs are accepted
		// with a whitespace-containing host label.
	}
	for _, tt := range tests {
		err := validateHostPort(tt.addr)
		if tt.ok && err != nil {
			t.Errorf("validateHostPort(%q) = %v, want nil", tt.addr, err)
		}
		if !tt.ok && err == nil {
			t.Errorf("validateHostPort(%q) = nil, want error", tt.addr)
		}
	}
}

func TestInitCache_empty(t *testing.T) {
	cfg := mesi.CreateDefaultConfig()
	got, err := initCache(cfg, "", 0, 0, "", "", 0, "")
	if err != nil {
		t.Fatalf("initCache empty: %v", err)
	}
	if got.Cache != nil {
		t.Error("expected Cache nil for empty backend")
	}
}

func TestInitCache_memory(t *testing.T) {
	cfg := mesi.CreateDefaultConfig()
	got, err := initCache(cfg, "memory", 100, 30*time.Second, "", "", 0, "")
	if err != nil {
		t.Fatalf("initCache memory: %v", err)
	}
	if got.Cache == nil {
		t.Fatal("expected Cache non-nil for memory backend")
	}
	if got.CacheTTL != 30*time.Second {
		t.Errorf("CacheTTL = %v, want 30s", got.CacheTTL)
	}
}

func TestInitCache_memory_zeroSize(t *testing.T) {
	cfg := mesi.CreateDefaultConfig()
	_, err := initCache(cfg, "memory", 0, 0, "", "", 0, "")
	if err == nil {
		t.Fatal("expected error for cache-size 0")
	}
}

func TestInitCache_memory_negativeSize(t *testing.T) {
	cfg := mesi.CreateDefaultConfig()
	_, err := initCache(cfg, "memory", -1, 0, "", "", 0, "")
	if err == nil {
		t.Fatal("expected error for negative cache-size")
	}
}

func TestInitCache_memory_negativeTTL(t *testing.T) {
	cfg := mesi.CreateDefaultConfig()
	_, err := initCache(cfg, "memory", 10, -5*time.Second, "", "", 0, "")
	if err == nil {
		t.Fatal("expected error for negative cache-ttl")
	}
}

func TestInitCache_redis(t *testing.T) {
	cfg := mesi.CreateDefaultConfig()
	got, err := initCache(cfg, "redis", 0, 30*time.Second, "localhost:6379", "", 0, "")
	if err != nil {
		t.Fatalf("initCache redis: %v", err)
	}
	if got.Cache == nil {
		t.Fatal("expected Cache non-nil for redis backend")
	}
	if got.CacheTTL != 30*time.Second {
		t.Errorf("CacheTTL = %v, want 30s", got.CacheTTL)
	}
}

func TestInitCache_redis_badAddr(t *testing.T) {
	cfg := mesi.CreateDefaultConfig()
	_, err := initCache(cfg, "redis", 0, 0, "bad-addr", "", 0, "")
	if err == nil {
		t.Fatal("expected error for invalid redis addr")
	}
}

func TestInitCache_redis_negativeTTL(t *testing.T) {
	cfg := mesi.CreateDefaultConfig()
	_, err := initCache(cfg, "redis", 0, -1*time.Second, "localhost:6379", "", 0, "")
	if err == nil {
		t.Fatal("expected error for negative cache-ttl")
	}
}

func TestInitCache_memcached(t *testing.T) {
	cfg := mesi.CreateDefaultConfig()
	got, err := initCache(cfg, "memcached", 0, 30*time.Second, "", "", 0, "10.0.0.1:11211,10.0.0.2:11211")
	if err != nil {
		t.Fatalf("initCache memcached: %v", err)
	}
	if got.Cache == nil {
		t.Fatal("expected Cache non-nil for memcached backend")
	}
	if got.CacheTTL != 30*time.Second {
		t.Errorf("CacheTTL = %v, want 30s", got.CacheTTL)
	}
}

func TestInitCache_memcached_emptyServers(t *testing.T) {
	cfg := mesi.CreateDefaultConfig()
	_, err := initCache(cfg, "memcached", 0, 0, "", "", 0, "")
	if err == nil {
		t.Fatal("expected error for empty memcached servers")
	}
}

func TestInitCache_memcached_negativeTTL(t *testing.T) {
	cfg := mesi.CreateDefaultConfig()
	_, err := initCache(cfg, "memcached", 0, -1*time.Second, "", "", 0, "host:11211")
	if err == nil {
		t.Fatal("expected error for negative cache-ttl")
	}
}

func TestInitCache_memcached_badServer(t *testing.T) {
	cfg := mesi.CreateDefaultConfig()
	_, err := initCache(cfg, "memcached", 0, 0, "", "", 0, "invalid")
	if err == nil {
		t.Fatal("expected error for invalid memcached server entry")
	}
}

func TestInitCache_unknown(t *testing.T) {
	cfg := mesi.CreateDefaultConfig()
	_, err := initCache(cfg, "nonexistent", 0, 0, "", "", 0, "")
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
}

func TestSplitAndTrim(t *testing.T) {
	tests := []struct {
		s    string
		sep  string
		want []string
	}{
		{"a,b,c", ",", []string{"a", "b", "c"}},
		{" a , b , c ", ",", []string{"a", "b", "c"}},
		{"", ",", nil},
		{",,", ",", nil},
		{"a", ",", []string{"a"}},
	}
	for _, tt := range tests {
		got := splitAndTrim(tt.s, tt.sep)
		if len(got) != len(tt.want) {
			t.Errorf("splitAndTrim(%q, %q) = %v, want %v", tt.s, tt.sep, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitAndTrim(%q, %q) = %v, want %v", tt.s, tt.sep, got, tt.want)
			}
		}
	}
}
