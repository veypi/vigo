package cache

import (
	"sync"
	"time"
)

// GetterFunc 定义获取对象的函数类型（泛型）
type GetterFunc[T any] func(string) (T, error)

// CacheItem 缓存项（泛型）
type CacheItem[T any] struct {
	Value      T
	CreateTime time.Time
	ExpireTime time.Time
}

// Cache 缓存结构体（泛型）
type Cache[T any] struct {
	data       map[string]*CacheItem[T]
	mutex      sync.RWMutex
	defaultTTL time.Duration
	getter     GetterFunc[T]
}

// NewCache 创建新的缓存实例，在构建时确定获取函数和类型
func NewCache[T any](defaultTTL time.Duration, getter GetterFunc[T]) *Cache[T] {
	if getter == nil {
		panic("getter function cannot be nil")
	}

	cache := &Cache[T]{
		data:       make(map[string]*CacheItem[T]),
		defaultTTL: defaultTTL,
		getter:     getter,
	}

	// 启动后台清理协程
	go cache.cleanup()

	return cache
}

// Get 获取缓存项，如果不存在或过期则调用预设的getter函数获取
func (c *Cache[T]) Get(key string) (T, error) {
	// 先尝试从缓存获取
	c.mutex.RLock()
	item, exists := c.data[key]
	c.mutex.RUnlock()

	now := time.Now()

	// 如果存在且未过期，直接返回
	if exists && now.Before(item.ExpireTime) {
		return item.Value, nil
	}

	// 否则调用getter函数获取新值
	value, err := c.getter(key)
	if err != nil {
		var zero T
		return zero, err
	}

	// 更新缓存
	c.mutex.Lock()
	c.data[key] = &CacheItem[T]{
		Value:      value,
		CreateTime: now,
		ExpireTime: now.Add(c.defaultTTL),
	}
	c.mutex.Unlock()

	return value, nil
}

// Set 手动设置缓存项（可选功能）
func (c *Cache[T]) Set(key string, value T, ttl time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	createTime := time.Now()
	expireTime := createTime.Add(ttl)

	if ttl == 0 {
		expireTime = createTime.Add(c.defaultTTL)
	}

	c.data[key] = &CacheItem[T]{
		Value:      value,
		CreateTime: createTime,
		ExpireTime: expireTime,
	}
}

// Delete 删除缓存项
func (c *Cache[T]) Delete(key string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	delete(c.data, key)
}

// Exists 检查缓存项是否存在且未过期
func (c *Cache[T]) Exists(key string) bool {
	c.mutex.RLock()
	item, exists := c.data[key]
	c.mutex.RUnlock()

	if !exists {
		return false
	}

	return time.Now().Before(item.ExpireTime)
}

// Refresh 强制刷新指定key的缓存
func (c *Cache[T]) Refresh(key string) (T, error) {
	value, err := c.getter(key)
	if err != nil {
		var zero T
		return zero, err
	}

	c.mutex.Lock()
	c.data[key] = &CacheItem[T]{
		Value:      value,
		CreateTime: time.Now(),
		ExpireTime: time.Now().Add(c.defaultTTL),
	}
	c.mutex.Unlock()

	return value, nil
}

// Size 返回缓存大小
func (c *Cache[T]) Size() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	count := 0
	now := time.Now()

	// 只计算未过期的项
	for _, item := range c.data {
		if now.Before(item.ExpireTime) {
			count++
		}
	}

	return count
}

// Clear 清空所有缓存
func (c *Cache[T]) Clear() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.data = make(map[string]*CacheItem[T])
}

// Keys 获取所有未过期的键
func (c *Cache[T]) Keys() []string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	now := time.Now()
	keys := make([]string, 0, len(c.data))

	for key, item := range c.data {
		if now.Before(item.ExpireTime) {
			keys = append(keys, key)
		}
	}

	return keys
}

// GetWithTTL 获取缓存项并返回剩余TTL
func (c *Cache[T]) GetWithTTL(key string) (T, time.Duration, error) {
	// 先尝试从缓存获取
	c.mutex.RLock()
	item, exists := c.data[key]
	c.mutex.RUnlock()

	now := time.Now()

	// 如果存在且未过期，直接返回
	if exists && now.Before(item.ExpireTime) {
		ttl := item.ExpireTime.Sub(now)
		return item.Value, ttl, nil
	}

	// 否则调用getter函数获取新值
	value, err := c.getter(key)
	if err != nil {
		var zero T
		return zero, 0, err
	}

	// 更新缓存
	c.mutex.Lock()
	c.data[key] = &CacheItem[T]{
		Value:      value,
		CreateTime: now,
		ExpireTime: now.Add(c.defaultTTL),
	}
	c.mutex.Unlock()

	return value, c.defaultTTL, nil
}

// cleanup 后台清理过期缓存
func (c *Cache[T]) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.mutex.Lock()

		now := time.Now()
		for key, item := range c.data {
			if now.After(item.ExpireTime) {
				delete(c.data, key)
			}
		}

		c.mutex.Unlock()
	}
}
