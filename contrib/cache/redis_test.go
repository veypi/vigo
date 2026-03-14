package cache

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/veypi/vigo/contrib/config"
)

func TestRedisCacheUsesSharedRedis(t *testing.T) {
	s := miniredis.NewMiniRedis()
	if err := s.Start(); err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer s.Close()

	config.SetSharedRedis(redis.NewClient(&redis.Options{Addr: s.Addr()}))

	var loads int
	cache := NewRedisCache[string]("test", time.Minute, func(key string) (string, error) {
		loads++
		return "value:" + key, nil
	})

	v, err := cache.Get("a")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if v != "value:a" {
		t.Fatalf("unexpected value: %q", v)
	}

	v, err = cache.Get("a")
	if err != nil {
		t.Fatalf("second get failed: %v", err)
	}
	if v != "value:a" {
		t.Fatalf("unexpected cached value: %q", v)
	}
	if loads != 1 {
		t.Fatalf("expected getter to run once, got %d", loads)
	}

	if err := cache.Set("b", "value:b", time.Minute); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	keys, err := cache.Keys()
	if err != nil {
		t.Fatalf("keys failed: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
	size, err := cache.Size()
	if err != nil {
		t.Fatalf("size failed: %v", err)
	}
	if size != 2 {
		t.Fatalf("expected size 2, got %d", size)
	}
	if err := cache.Clear(); err != nil {
		t.Fatalf("clear failed: %v", err)
	}
	size, err = cache.Size()
	if err != nil {
		t.Fatalf("size after clear failed: %v", err)
	}
	if size != 0 {
		t.Fatalf("expected empty cache after clear, got %d", size)
	}
}
