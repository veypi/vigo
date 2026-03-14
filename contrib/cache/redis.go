package cache

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/veypi/vigo/contrib/config"
	"golang.org/x/sync/singleflight"
)

type RedisCache[T any] struct {
	client     *redis.Client
	prefix     string
	defaultTTL time.Duration
	getter     GetterFunc[T]
	sf         singleflight.Group
}

func NewRedisCache[T any](prefix string, defaultTTL time.Duration, getter GetterFunc[T], clients ...*redis.Client) *RedisCache[T] {
	if getter == nil {
		panic("getter function cannot be nil")
	}
	var client *redis.Client
	if len(clients) > 0 {
		client = clients[0]
	}
	if client == nil {
		client = config.SharedRedis()
	}
	return &RedisCache[T]{
		client:     client,
		prefix:     prefix,
		defaultTTL: defaultTTL,
		getter:     getter,
	}
}

func (c *RedisCache[T]) key(key string) string {
	if c.prefix == "" {
		return key
	}
	return c.prefix + ":" + key
}

func (c *RedisCache[T]) Get(key string) (T, error) {
	ctx := context.Background()
	var zero T
	if c.client == nil {
		return zero, errors.New("redis client is nil")
	}
	cacheKey := c.key(key)
	data, err := c.client.Get(ctx, cacheKey).Bytes()
	if err == nil {
		var value T
		if unmarshalErr := json.Unmarshal(data, &value); unmarshalErr == nil {
			return value, nil
		}
	}

	v, err, _ := c.sf.Do(cacheKey, func() (any, error) {
		// Double-check after singleflight.
		data, err := c.client.Get(ctx, cacheKey).Bytes()
		if err == nil {
			var value T
			if unmarshalErr := json.Unmarshal(data, &value); unmarshalErr == nil {
				return value, nil
			}
		}

		value, err := c.getter(key)
		if err != nil {
			return zero, err
		}
		payload, err := json.Marshal(value)
		if err != nil {
			return zero, err
		}
		if err := c.client.Set(ctx, cacheKey, payload, c.defaultTTL).Err(); err != nil {
			return zero, err
		}
		return value, nil
	})
	if err != nil {
		return zero, err
	}
	return v.(T), nil
}

func (c *RedisCache[T]) Set(key string, value T, ttl time.Duration) error {
	if c.client == nil {
		return errors.New("redis client is nil")
	}
	if ttl <= 0 {
		ttl = c.defaultTTL
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.client.Set(context.Background(), c.key(key), payload, ttl).Err()
}

func (c *RedisCache[T]) Refresh(key string) (T, error) {
	if c.client == nil {
		var zero T
		return zero, errors.New("redis client is nil")
	}
	value, err := c.getter(key)
	if err != nil {
		var zero T
		return zero, err
	}
	if err := c.Set(key, value, c.defaultTTL); err != nil {
		var zero T
		return zero, err
	}
	return value, nil
}

func (c *RedisCache[T]) Delete(key string) error {
	if c.client == nil {
		return errors.New("redis client is nil")
	}
	return c.client.Del(context.Background(), c.key(key)).Err()
}

func (c *RedisCache[T]) Exists(key string) bool {
	if c.client == nil {
		return false
	}
	n, err := c.client.Exists(context.Background(), c.key(key)).Result()
	return err == nil && n > 0
}

func (c *RedisCache[T]) GetWithTTL(key string) (T, time.Duration, error) {
	ctx := context.Background()
	var zero T
	if c.client == nil {
		return zero, 0, errors.New("redis client is nil")
	}
	cacheKey := c.key(key)
	data, err := c.client.Get(ctx, cacheKey).Bytes()
	if err == nil {
		var value T
		if unmarshalErr := json.Unmarshal(data, &value); unmarshalErr == nil {
			ttl, ttlErr := c.client.TTL(ctx, cacheKey).Result()
			if ttlErr != nil {
				return zero, 0, ttlErr
			}
			return value, ttl, nil
		}
	}

	value, err := c.Refresh(key)
	if err != nil {
		return zero, 0, err
	}
	return value, c.defaultTTL, nil
}

func (c *RedisCache[T]) Keys() ([]string, error) {
	if c.client == nil {
		return nil, errors.New("redis client is nil")
	}
	keys, err := c.scanKeys(context.Background())
	if err != nil {
		return nil, err
	}
	if c.prefix == "" {
		return keys, nil
	}
	res := make([]string, 0, len(keys))
	prefix := c.prefix + ":"
	for _, key := range keys {
		res = append(res, strings.TrimPrefix(key, prefix))
	}
	return res, nil
}

func (c *RedisCache[T]) Size() (int, error) {
	keys, err := c.Keys()
	if err != nil {
		return 0, err
	}
	return len(keys), nil
}

func (c *RedisCache[T]) Clear() error {
	if c.client == nil {
		return errors.New("redis client is nil")
	}
	keys, err := c.scanKeys(context.Background())
	if err != nil || len(keys) == 0 {
		return err
	}
	return c.client.Del(context.Background(), keys...).Err()
}

func (c *RedisCache[T]) scanKeys(ctx context.Context) ([]string, error) {
	pattern := c.prefix + ":*"
	if c.prefix == "" {
		pattern = "*"
	}
	var (
		cursor uint64
		keys   []string
	)
	for {
		batch, next, err := c.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, batch...)
		cursor = next
		if cursor == 0 {
			return keys, nil
		}
	}
}
