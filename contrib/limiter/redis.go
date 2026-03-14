package limiter

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/veypi/vigo"
	"github.com/veypi/vigo/contrib/config"
)

type RedisRequestLimiter struct {
	client *redis.Client
	prefix string
	config LimiterConfig
}

func NewRedisRequestLimiter(window time.Duration, maxRequests int, minInterval time.Duration, keyFunc ...func(*vigo.X) string) *RedisRequestLimiter {
	cfg := LimiterConfig{
		Window:      window,
		MaxRequests: maxRequests,
		MinInterval: minInterval,
		KeyFunc:     GetPathKeyFunc,
	}
	if len(keyFunc) > 0 && keyFunc[0] != nil {
		cfg.KeyFunc = keyFunc[0]
	}
	return &RedisRequestLimiter{
		client: config.SharedRedis(),
		prefix: "vigo:limiter",
		config: cfg,
	}
}

func (l *RedisRequestLimiter) SetRedis(client *redis.Client) *RedisRequestLimiter {
	l.client = client
	return l
}

func (l *RedisRequestLimiter) SetPrefix(prefix string) *RedisRequestLimiter {
	l.prefix = prefix
	return l
}

func (l *RedisRequestLimiter) Limit(x *vigo.X, data any) (any, error) {
	if l.client == nil {
		return data, nil
	}
	allowed, retryAfter, err := l.isAllowed(context.Background(), x)
	if err != nil {
		return nil, err
	}
	if !allowed {
		x.Header().Set("Content-Type", "application/json; charset=utf-8")
		x.Header().Set("Retry-After", retryAfter.String())
		return nil, vigo.ErrTooManyRequests.WithMessage("retry after " + retryAfter.String())
	}
	return data, nil
}

func (l *RedisRequestLimiter) GetRateInfo(x *vigo.X) map[string]any {
	if l.client == nil {
		return map[string]any{
			"requests_in_window": 0,
			"max_requests":       l.config.MaxRequests,
			"window":             l.config.Window.String(),
		}
	}
	key := l.rateKey(x)
	ctx := context.Background()
	now := time.Now().UnixMilli()
	minScore := strconv.FormatInt(now-l.config.Window.Milliseconds(), 10)
	count, _ := l.client.ZCount(ctx, key, minScore, "+inf").Result()
	last := ""
	retryAfter := ""
	lastItems, _ := l.client.ZRangeArgsWithScores(ctx, redis.ZRangeArgs{
		Key:   key,
		Start: -1,
		Stop:  -1,
	}).Result()
	if len(lastItems) > 0 {
		lastTs := int64(lastItems[0].Score)
		last = time.UnixMilli(lastTs).Format(time.RFC3339)
		if l.config.MinInterval > 0 {
			remain := l.config.MinInterval - time.Duration(now-lastTs)*time.Millisecond
			if remain > 0 {
				retryAfter = remain.String()
			}
		}
	}
	if retryAfter == "" && l.config.MaxRequests > 0 && count >= int64(l.config.MaxRequests) {
		oldestItems, _ := l.client.ZRangeArgsWithScores(ctx, redis.ZRangeArgs{
			Key:   key,
			Start: 0,
			Stop:  0,
		}).Result()
		if len(oldestItems) > 0 {
			oldestTs := int64(oldestItems[0].Score)
			remain := l.config.Window - time.Duration(now-oldestTs)*time.Millisecond
			if remain > 0 {
				retryAfter = remain.String()
			}
		}
	}
	return map[string]any{
		"requests_in_window": count,
		"max_requests":       l.config.MaxRequests,
		"window":             l.config.Window.String(),
		"min_interval":       l.config.MinInterval.String(),
		"last_request":       last,
		"retry_after":        retryAfter,
	}
}

func (l *RedisRequestLimiter) rateKey(x *vigo.X) string {
	return l.prefix + ":" + l.config.KeyFunc(x)
}

var redisLimiterScript = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local max_requests = tonumber(ARGV[3])
local min_interval = tonumber(ARGV[4])
local member = ARGV[5]

redis.call("ZREMRANGEBYSCORE", key, "-inf", now - window)
local count = redis.call("ZCARD", key)
local last = redis.call("ZRANGE", key, -1, -1, "WITHSCORES")
if min_interval > 0 and #last > 0 then
  local last_ts = tonumber(last[2])
  if now - last_ts < min_interval then
    return {0, min_interval - (now - last_ts)}
  end
end
if max_requests > 0 and count >= max_requests then
  local oldest = redis.call("ZRANGE", key, 0, 0, "WITHSCORES")
  if #oldest > 0 then
    local oldest_ts = tonumber(oldest[2])
    return {0, window - (now - oldest_ts)}
  end
  return {0, window}
end
redis.call("ZADD", key, now, member)
redis.call("PEXPIRE", key, window)
return {1, 0}
`)

func (l *RedisRequestLimiter) isAllowed(ctx context.Context, x *vigo.X) (bool, time.Duration, error) {
	if l.client == nil {
		return true, 0, nil
	}
	if l.config.KeyFunc == nil {
		return false, 0, errors.New("key func is nil")
	}
	now := time.Now()
	res, err := redisLimiterScript.Run(ctx, l.client, []string{l.rateKey(x)},
		now.UnixMilli(),
		l.config.Window.Milliseconds(),
		l.config.MaxRequests,
		l.config.MinInterval.Milliseconds(),
		strconv.FormatInt(now.UnixNano(), 10),
	).Result()
	if err != nil {
		return false, 0, err
	}
	values, ok := res.([]any)
	if !ok || len(values) != 2 {
		return false, 0, errors.New("unexpected limiter response")
	}
	allowed, ok := values[0].(int64)
	if !ok {
		return false, 0, errors.New("unexpected limiter allow flag")
	}
	retryMS, ok := values[1].(int64)
	if !ok {
		return false, 0, errors.New("unexpected limiter retry value")
	}
	if retryMS < 0 {
		retryMS = 0
	}
	return allowed == 1, time.Duration(retryMS) * time.Millisecond, nil
}
