package config

import "testing"

func TestSharedRedisDefault(t *testing.T) {
	SetSharedRedis(nil)
	SetSharedRedisConfig(&Redis{Addr: "memory"})

	client := SharedRedis()
	if client == nil {
		t.Fatal("expected shared redis client")
	}
	// 修复前 cfg 按值复制导致每次调用都新建 miniredis/client，
	// 此处锁定指针共享后必须返回同一实例。
	if again := SharedRedis(); again != client {
		t.Fatal("expected the same shared redis client across calls")
	}
}
