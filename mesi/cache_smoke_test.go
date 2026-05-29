package mesi

import (
	"testing"
)

func TestMemoryCacheAvailableWithoutBuildTags(t *testing.T) {
	cache := NewMemoryCache(100, 0)
	if cache == nil {
		t.Fatal("NewMemoryCache returned nil")
	}
}

func TestCacheInterfaceSatisfied(t *testing.T) {
	var _ Cache = (*MemoryCache)(nil)
}
