// Package attemptguard 提供按任意键累计失败次数的计数器，用于防爆破。
// 优先使用 Redis（多实例共享），Redis 不可用时退回进程内计数。
package attemptguard

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/dujiao-next/internal/logger"

	"github.com/redis/go-redis/v9"
)

const maxLocalEntries = 50000

// incrScript 递增计数，首次写入时设置过期时间（窗口从第一次失败开始计算）。
var incrScript = redis.NewScript(`
local current = redis.call('INCR', KEYS[1])
if current == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
return current
`)

type entry struct {
	count     int64
	expiresAt time.Time
}

// Counter 是带过期窗口的失败计数器。
type Counter struct {
	client *redis.Client
	prefix string
	window time.Duration
	now    func() time.Time

	mu      sync.Mutex
	entries map[string]entry
}

// New 创建计数器；client 为空时只使用进程内计数。
func New(client *redis.Client, prefix string, window time.Duration) *Counter {
	return &Counter{
		client:  client,
		prefix:  strings.TrimRight(prefix, ":"),
		window:  window,
		now:     time.Now,
		entries: make(map[string]entry),
	}
}

func (counter *Counter) redisKey(key string) string {
	return counter.prefix + ":" + key
}

// Increment 失败次数加一并返回当前次数。
func (counter *Counter) Increment(ctx context.Context, key string) int64 {
	if counter.client != nil {
		result, err := incrScript.Run(ctx, counter.client, []string{counter.redisKey(key)}, int(counter.window.Seconds())).Int64()
		if err == nil {
			return result
		}
		logger.Warnw("attempt_guard_redis_fallback", "prefix", counter.prefix, "error", err)
	}
	return counter.incrementLocal(key)
}

// Count 返回当前窗口内的失败次数。
func (counter *Counter) Count(ctx context.Context, key string) int64 {
	if counter.client != nil {
		value, err := counter.client.Get(ctx, counter.redisKey(key)).Int64()
		if err == nil {
			return value
		}
		if errors.Is(err, redis.Nil) {
			return 0
		}
		logger.Warnw("attempt_guard_redis_fallback", "prefix", counter.prefix, "error", err)
	}
	counter.mu.Lock()
	defer counter.mu.Unlock()
	item, ok := counter.entries[key]
	if !ok || !counter.now().Before(item.expiresAt) {
		return 0
	}
	return item.count
}

// Set 写入一个带窗口的标记（值为 1），用于记录「已通过验证码」等状态。
func (counter *Counter) Set(ctx context.Context, key string) {
	if counter.client != nil {
		if err := counter.client.Set(ctx, counter.redisKey(key), 1, counter.window).Err(); err == nil {
			return
		}
	}
	counter.mu.Lock()
	defer counter.mu.Unlock()
	counter.ensureCapacity()
	counter.entries[key] = entry{count: 1, expiresAt: counter.now().Add(counter.window)}
}

// Reset 清除计数。
func (counter *Counter) Reset(ctx context.Context, key string) {
	if counter.client != nil {
		_ = counter.client.Del(ctx, counter.redisKey(key)).Err()
	}
	counter.mu.Lock()
	delete(counter.entries, key)
	counter.mu.Unlock()
}

func (counter *Counter) incrementLocal(key string) int64 {
	counter.mu.Lock()
	defer counter.mu.Unlock()
	now := counter.now()
	item, ok := counter.entries[key]
	if !ok || !now.Before(item.expiresAt) {
		counter.ensureCapacity()
		item = entry{expiresAt: now.Add(counter.window)}
	}
	item.count++
	counter.entries[key] = item
	return item.count
}

// ensureCapacity 清理过期项；仍超限时整体清空，防止内存被刷爆（调用方需持有锁）。
func (counter *Counter) ensureCapacity() {
	if len(counter.entries) < maxLocalEntries {
		return
	}
	now := counter.now()
	for key, item := range counter.entries {
		if !now.Before(item.expiresAt) {
			delete(counter.entries, key)
		}
	}
	if len(counter.entries) >= maxLocalEntries {
		counter.entries = make(map[string]entry)
	}
}
