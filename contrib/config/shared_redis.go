package config

import (
	"sync"

	"github.com/redis/go-redis/v9"
)

var (
	sharedRedisMu     sync.RWMutex
	sharedRedisCfg    = &Redis{Addr: "memory"}
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

// SetSharedRedisConfig 替换全局共享配置。cfg 必须是非零指针，
// 内部直接持有并共享该指针（Redis 含 sync.Once 不可复制）。
func SetSharedRedisConfig(cfg *Redis) {
	sharedRedisMu.Lock()
	defer sharedRedisMu.Unlock()
	sharedRedisCfg = cfg
	sharedRedisClient = nil
}
