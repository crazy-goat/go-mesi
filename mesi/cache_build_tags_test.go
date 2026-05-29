package mesi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCacheBackendsAreBuildTagProtected(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	files := []struct {
		file string
		tag  string
	}{
		{"cache_memcached.go", "memcached"},
		{"cache_redis.go", "redis"},
		{"cache_memcached_test.go", "memcached"},
		{"cache_redis_test.go", "redis"},
	}

	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(wd, f.file))
		if err != nil {
			t.Fatalf("failed to read %s: %v", f.file, err)
		}
		if !strings.Contains(string(data), "//go:build "+f.tag) {
			t.Errorf("%s is missing build tag '//go:build %s'", f.file, f.tag)
		}
	}
}

func TestMemoryCacheAvailableWithoutBuildTags(t *testing.T) {
	cache := NewMemoryCache(100, 0)
	if cache == nil {
		t.Fatal("NewMemoryCache returned nil")
	}
}

func TestCacheInterfaceSatisfied(t *testing.T) {
	var _ Cache = (*MemoryCache)(nil)
}
