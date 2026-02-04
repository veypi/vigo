//
// time.go
// Copyright (C) 2025 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package limiter

import (
	"sync"
	"time"

	"github.com/veypi/vigo"
)

// LimiterConfig 限流配置
type LimiterConfig struct {
	Window      time.Duration        // 时间窗口
	MaxRequests int                  // 最大请求数
	MinInterval time.Duration        // 最小请求间隔
	KeyFunc     func(*vigo.X) string // 自定义key生成函数
}

// AdvancedRequestLimiter 高级请求限制器
type AdvancedRequestLimiter struct {
	mu      sync.RWMutex
	clients map[string]*ClientRecord
	config  LimiterConfig
}

// ClientRecord 客户端记录
type ClientRecord struct {
	Requests    []time.Time
	LastRequest time.Time
}

// NewAdvancedRequestLimiter 创建高级请求限制器
func NewAdvancedRequestLimiter(window time.Duration, MaxRequests int, MinInterval time.Duration, keyFinc ...func(*vigo.X) string) *AdvancedRequestLimiter {
	config := LimiterConfig{
		Window:      window,
		MaxRequests: MaxRequests,
		MinInterval: MinInterval,
	}
	if len(keyFinc) > 0 && keyFinc[0] != nil {
		config.KeyFunc = keyFinc[0]
	} else {
		config.KeyFunc = GetPathKeyFunc
	}

	return &AdvancedRequestLimiter{
		clients: make(map[string]*ClientRecord),
		config:  config,
	}
}

// isAllowed 检查是否允许请求
func (al *AdvancedRequestLimiter) isAllowed(x *vigo.X) bool {
	al.mu.Lock()
	defer al.mu.Unlock()

	clientKey := al.config.KeyFunc(x)
	now := time.Now()

	record, exists := al.clients[clientKey]
	if !exists {
		record = &ClientRecord{}
		al.clients[clientKey] = record
	}

	// 清理过期请求
	validRequests := make([]time.Time, 0)
	for _, reqTime := range record.Requests {
		if now.Sub(reqTime) <= al.config.Window {
			validRequests = append(validRequests, reqTime)
		}
	}

	// 检查最小间隔
	if al.config.MinInterval > 0 && !record.LastRequest.IsZero() {
		if now.Sub(record.LastRequest) < al.config.MinInterval {
			return false
		}
	}

	// 检查最大请求数
	if al.config.MaxRequests > 0 && len(validRequests) >= al.config.MaxRequests {
		return false
	}

	// 更新记录
	validRequests = append(validRequests, now)
	record.Requests = validRequests
	record.LastRequest = now

	return true
}

// cleanExpired 清理过期数据
func (al *AdvancedRequestLimiter) cleanExpired() {
	al.mu.Lock()
	defer al.mu.Unlock()

	now := time.Now()
	for clientKey, record := range al.clients {
		validRequests := make([]time.Time, 0)
		for _, reqTime := range record.Requests {
			if now.Sub(reqTime) <= al.config.Window {
				validRequests = append(validRequests, reqTime)
			}
		}

		if len(validRequests) == 0 {
			delete(al.clients, clientKey)
		} else {
			record.Requests = validRequests
		}
	}
}

// StartCleaner 启动清理协程
func (al *AdvancedRequestLimiter) StartCleaner(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			al.cleanExpired()
		}
	}()
}

// Limit 限流装饰器
func (al *AdvancedRequestLimiter) Limit(x *vigo.X, data any) (any, error) {
	if !al.isAllowed(x) {
		x.Header().Set("Content-Type", "application/json")
		x.Header().Set("Retry-After", al.config.MinInterval.String())
		return nil, vigo.ErrTooManyRequests.WithMessage("retry after " + al.config.MinInterval.String())
	}
	return data, nil
}

// GetRateInfo 获取限流信息
func (al *AdvancedRequestLimiter) GetRateInfo(x *vigo.X) map[string]any {
	al.mu.RLock()
	defer al.mu.RUnlock()

	clientKey := al.config.KeyFunc(x)
	record, exists := al.clients[clientKey]

	if !exists {
		return map[string]interface{}{
			"requests_in_window": 0,
			"max_requests":       al.config.MaxRequests,
			"window":             al.config.Window.String(),
		}
	}

	now := time.Now()
	validRequests := 0
	for _, reqTime := range record.Requests {
		if now.Sub(reqTime) <= al.config.Window {
			validRequests++
		}
	}

	return map[string]interface{}{
		"requests_in_window": validRequests,
		"max_requests":       al.config.MaxRequests,
		"window":             al.config.Window.String(),
		"last_request":       record.LastRequest.Format(time.RFC3339),
	}
}
