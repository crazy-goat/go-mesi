package mesi

import (
	"container/list"
	"context"
	"sync"
	"time"
)

type memoryCacheEntry struct {
	key      string
	value    string
	expireAt time.Time
	element  *list.Element
}

type MemoryCache struct {
	mu         sync.RWMutex
	items      map[string]*memoryCacheEntry
	lru        *list.List
	maxSize    int
	defaultTTL time.Duration
}

func NewMemoryCache(maxSize int, defaultTTL time.Duration) *MemoryCache {
	return &MemoryCache{
		items:      make(map[string]*memoryCacheEntry),
		lru:        list.New(),
		maxSize:    maxSize,
		defaultTTL: defaultTTL,
	}
}

func (c *MemoryCache) Get(ctx context.Context, key string) (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.items[key]
	if !ok {
		return "", false, nil
	}

	if !entry.expireAt.IsZero() && time.Now().After(entry.expireAt) {
		c.removeEntry(key, entry)
		return "", false, nil
	}

	c.lru.MoveToFront(entry.element)
	return entry.value, true, nil
}

func (c *MemoryCache) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, ok := c.items[key]; ok {
		entry.value = value
		if ttl == 0 {
			ttl = c.defaultTTL
		}
		if ttl > 0 {
			entry.expireAt = time.Now().Add(ttl)
		} else {
			entry.expireAt = time.Time{}
		}
		c.lru.MoveToFront(entry.element)
		return nil
	}

	if c.maxSize > 0 && c.lru.Len() >= c.maxSize {
		c.evictLRU()
	}

	var expireAt time.Time
	if ttl == 0 {
		ttl = c.defaultTTL
	}
	if ttl > 0 {
		expireAt = time.Now().Add(ttl)
	}

	entry := &memoryCacheEntry{
		key:      key,
		value:    value,
		expireAt: expireAt,
	}
	entry.element = c.lru.PushFront(entry)
	c.items[key] = entry

	return nil
}

func (c *MemoryCache) Delete(ctx context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, ok := c.items[key]; ok {
		c.removeEntry(key, entry)
	}
	return nil
}

func (c *MemoryCache) removeEntry(key string, entry *memoryCacheEntry) {
	c.lru.Remove(entry.element)
	delete(c.items, key)
}

func (c *MemoryCache) evictLRU() {
	if c.lru.Len() == 0 {
		return
	}
	elem := c.lru.Back()
	if elem != nil {
		entry := elem.Value.(*memoryCacheEntry)
		delete(c.items, entry.key)
		c.lru.Remove(elem)
	}
}
