//go:build !memcached

package roadrunner

import (
	"testing"
)

func TestCreateConfigMemcachedServersDefault(t *testing.T) {
	config := CreateConfig()
	if config.CacheMemcachedServers != nil {
		t.Errorf("Expected nil CacheMemcachedServers, got %v", config.CacheMemcachedServers)
	}
}

func TestMemcachedBackendRequiresTag(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "memcached"
	config.CacheTTL = "60s"
	config.CacheMemcachedServers = []string{"localhost:11211"}

	p := &Plugin{config: config}
	if err := p.Init(); err == nil {
		t.Fatal("Expected error for memcached backend without build tag")
	}
}

func TestMemcachedServersAcceptedInConfig(t *testing.T) {
	config := CreateConfig()
	config.CacheMemcachedServers = []string{"10.0.0.1:11211", "10.0.0.2:11211"}

	if len(config.CacheMemcachedServers) != 2 {
		t.Errorf("Expected 2 servers, got %d", len(config.CacheMemcachedServers))
	}
	if config.CacheMemcachedServers[0] != "10.0.0.1:11211" {
		t.Errorf("Expected first server '10.0.0.1:11211', got '%s'", config.CacheMemcachedServers[0])
	}
	if config.CacheMemcachedServers[1] != "10.0.0.2:11211" {
		t.Errorf("Expected second server '10.0.0.2:11211', got '%s'", config.CacheMemcachedServers[1])
	}
}
