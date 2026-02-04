// middleware/cache.go
package cache

import (
	"github.com/veypi/vigo"
	"sync"
	"time"
)

// 缓存条目
type cacheItem struct {
	data    interface{} // 缓存的数据
	expires time.Time   // 过期时间
}

// 缓存中间件
type CacheMiddleware struct {
	sync.RWMutex
	cache      map[string]*cacheItem // URL -> 缓存条目
	expiration time.Duration         // 缓存过期时间
	fc         vigo.FuncX2AnyErr
}

// 创建新的缓存中间件，默认10秒过期
func NewCacheMiddleware(fc vigo.FuncX2AnyErr, expiration time.Duration) *CacheMiddleware {
	if expiration <= 0 {
		expiration = 10 * time.Second
	}
	c := &CacheMiddleware{
		cache:      make(map[string]*cacheItem),
		expiration: expiration,
		fc:         fc,
	}
	c.StartCleanup(0)
	return c
}

// 实现中间件接口
func (m *CacheMiddleware) Handler(x *vigo.X) (any, error) {
	// 只缓存GET请求
	if x.Request.Method != "GET" {
		return m.fc(x)
	}

	// 获取请求URL作为缓存键
	key := x.Request.URL.String()

	// 检查缓存
	m.RLock()
	item, exists := m.cache[key]
	m.RUnlock()

	// 如果缓存存在且未过期，直接返回缓存
	if exists && time.Now().Before(item.expires) {
		return item.data, nil
	}
	data, err := m.fc(x)
	if err != nil {
		return nil, err
	}
	// 缓存响应
	m.Lock()
	m.cache[key] = &cacheItem{
		data:    data,
		expires: time.Now().Add(m.expiration),
	}
	m.Unlock()

	return data, nil
}

// 清理过期的缓存项
func (m *CacheMiddleware) Cleanup() {
	m.Lock()
	defer m.Unlock()

	now := time.Now()
	for key, item := range m.cache {
		if now.After(item.expires) {
			delete(m.cache, key)
		}
	}
}

// 启动定期清理协程
func (m *CacheMiddleware) StartCleanup(interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			m.Cleanup()
		}
	}()
}
