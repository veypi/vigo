package config

import "testing"

func TestSharedRedisDefault(t *testing.T) {
	SetSharedRedis(nil)
	SetSharedRedisConfig(Redis{Addr: "memory"})

	client := SharedRedis()
	if client == nil {
		t.Fatal("expected shared redis client")
	}
}
