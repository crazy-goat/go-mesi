package cache_memcached

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
)

func TestMemcachedCache_SetAndGet(t *testing.T) {
	mc := newMemcachedClientForTest(t)
	if mc == nil {
		t.Skip("Memcached not available")
	}
	defer func() { _ = mc.Close() }()

	cache := NewMemcachedCache(mc, time.Hour)
	ctx := context.Background()

	err := cache.Set(ctx, "mc_key1", "mc_value1", time.Hour)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	v, ok, err := cache.Get(ctx, "mc_key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !ok || v != "mc_value1" {
		t.Fatalf("expected mc_value1, got %s, ok=%v", v, ok)
	}

	if err := cache.Delete(ctx, "mc_key1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	_, ok, err = cache.Get(ctx, "mc_key1")
	if err != nil {
		t.Fatalf("Get after Delete failed: %v", err)
	}
	if ok {
		t.Fatal("key should be deleted")
	}
}

func TestMemcachedCache_TTL(t *testing.T) {
	mc := newMemcachedClientForTest(t)
	if mc == nil {
		t.Skip("Memcached not available")
	}
	defer func() { _ = mc.Close() }()

	cache := NewMemcachedCache(mc, time.Hour)
	ctx := context.Background()

	err := cache.Set(ctx, "mc_ttl_key", "ttl_value", 2*time.Second)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	v, ok, _ := cache.Get(ctx, "mc_ttl_key")
	if !ok || v != "ttl_value" {
		t.Fatalf("expected ttl_value, got %s, ok=%v", v, ok)
	}

	time.Sleep(2100 * time.Millisecond)

	_, ok, _ = cache.Get(ctx, "mc_ttl_key")
	if ok {
		t.Fatal("key should have expired")
	}
}

func TestMemcachedCache_CanceledContext(t *testing.T) {
	client := memcache.New("localhost:11211")
	cache := NewMemcachedCache(client, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, err := cache.Get(ctx, "key"); !errors.Is(err, context.Canceled) {
		t.Errorf("Get error = %v, want context.Canceled", err)
	}
	if err := cache.Set(ctx, "key", "value", time.Minute); !errors.Is(err, context.Canceled) {
		t.Errorf("Set error = %v, want context.Canceled", err)
	}
	if err := cache.Delete(ctx, "key"); !errors.Is(err, context.Canceled) {
		t.Errorf("Delete error = %v, want context.Canceled", err)
	}
}

func TestMemcachedCache_DeadlineContext(t *testing.T) {
	client := memcache.New("localhost:11211")
	cache := NewMemcachedCache(client, time.Hour)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	if _, _, err := cache.Get(ctx, "key"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Get error = %v, want context.DeadlineExceeded", err)
	}
	if err := cache.Set(ctx, "key", "value", time.Minute); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Set error = %v, want context.DeadlineExceeded", err)
	}
	if err := cache.Delete(ctx, "key"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Delete error = %v, want context.DeadlineExceeded", err)
	}
}

func TestMemcachedCache_InFlightOperationsHonorDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	accepted := make(chan net.Conn, 3)
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			accepted <- conn
		}
	}()
	defer func() {
		listener.Close()
		<-serverDone
		close(accepted)
		for conn := range accepted {
			_ = conn.Close()
		}
	}()

	client := memcache.New(listener.Addr().String())
	cache := NewMemcachedCache(client, time.Hour)
	operations := []struct {
		name string
		call func(context.Context) error
	}{
		{name: "get", call: func(ctx context.Context) error {
			_, _, err := cache.Get(ctx, "deadline-get")
			return err
		}},
		{name: "set", call: func(ctx context.Context) error {
			return cache.Set(ctx, "deadline-set", "value", time.Minute)
		}},
		{name: "delete", call: func(ctx context.Context) error {
			return cache.Delete(ctx, "deadline-delete")
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			started := time.Now()
			if err := operation.call(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("operation error = %v, want context.DeadlineExceeded", err)
			}
			if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
				t.Errorf("operation returned after %s, want promptly after the 40ms context deadline", elapsed)
			}
			select {
			case <-accepted:
			case <-time.After(100 * time.Millisecond):
				t.Fatal("operation did not reach the stalled memcached server")
			}
		})
	}
}

func TestMemcachedCache_OperationErrors(t *testing.T) {
	client := memcache.New("localhost:11211")
	cache := NewMemcachedCache(client, time.Hour)
	ctx := context.Background()

	if _, _, err := cache.Get(ctx, "invalid key"); !errors.Is(err, memcache.ErrMalformedKey) {
		t.Errorf("Get error = %v, want ErrMalformedKey", err)
	}
	if err := cache.Delete(ctx, "invalid key"); !errors.Is(err, memcache.ErrMalformedKey) {
		t.Errorf("Delete error = %v, want ErrMalformedKey", err)
	}
}

func newMemcachedClientForTest(t *testing.T) *memcache.Client {
	mc := memcache.New("localhost:11211")
	err := mc.Ping()
	if err != nil {
		return nil
	}
	return mc
}
