package limiter

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/veypi/vigo"
	"github.com/veypi/vigo/contrib/config"
)

func TestRedisRequestLimiterUsesSharedRedis(t *testing.T) {
	s := miniredis.NewMiniRedis()
	if err := s.Start(); err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer s.Close()

	config.SetSharedRedis(redis.NewClient(&redis.Options{Addr: s.Addr()}))

	limiter := NewRedisRequestLimiter(time.Second, 2, 0)

	newX := func() *vigo.X {
		req, _ := http.NewRequest(http.MethodGet, "/hello", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		x := new(vigo.X)
		x.Request = req
		return x
	}

	allowed, _, err := limiter.isAllowed(context.Background(), newX())
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	if !allowed {
		t.Fatal("expected first request to pass")
	}
	allowed, _, err = limiter.isAllowed(context.Background(), newX())
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	if !allowed {
		t.Fatal("expected second request to pass")
	}
	allowed, _, err = limiter.isAllowed(context.Background(), newX())
	if err != nil {
		t.Fatalf("third request returned unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected third request to be limited")
	}

	info := limiter.GetRateInfo(newX())
	if info["requests_in_window"].(int64) != 2 {
		t.Fatalf("expected 2 requests in window, got %v", info["requests_in_window"])
	}
	if info["retry_after"] == "" {
		t.Fatal("expected retry_after to be populated after limit hit")
	}
}
