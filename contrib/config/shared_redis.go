package config

import (
	"sync"

	"github.com/redis/go-redis/v9"
)

var (
	sharedRedisMu     sync.RWMutex
	sharedRedisCfg    = Redis{Addr: "memory"}
	sharedRedisClient *redis.Client
)

func SharedRedis() *redis.Client {
	sharedRedisMu.RLock()
	client := sharedRedisClient
	cfg := sharedRedisCfg
	sharedRedisMu.RUnlock()

	if client != nil {
		return client
	}
	return cfg.Client()
}

func SetSharedRedis(client *redis.Client) {
	sharedRedisMu.Lock()
	defer sharedRedisMu.Unlock()
	sharedRedisClient = client
}

func SetSharedRedisConfig(cfg Redis) {
	sharedRedisMu.Lock()
	defer sharedRedisMu.Unlock()
	sharedRedisCfg = cfg
	sharedRedisClient = nil
}
