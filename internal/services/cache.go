package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type CacheService struct {
	rdb      *redis.Client
	memCache *memCache
}

const memCacheMaxCapacity = 5000

type memCache struct {
	mu      sync.RWMutex
	entries map[string]memEntry
}

type memEntry struct {
	value     string
	expiresAt time.Time
}

func NewCacheService(redisHost string, redisPort int, redisPassword string) *CacheService {
	mc := &memCache{entries: make(map[string]memEntry)}

	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			mc.cleanup()
		}
	}()

	var rdb *redis.Client
	addr := fmt.Sprintf("%s:%d", redisHost, redisPort)
	opts := &redis.Options{Addr: addr}
	if redisPassword != "" {
		opts.Password = redisPassword
	}
	rdb = redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Warn("Redis unavailable, using in-memory cache", "error", err)
		rdb = nil
	} else {
		slog.Info("Redis cache connected")
	}

	return &CacheService{rdb: rdb, memCache: mc}
}

func (c *CacheService) Get(ctx context.Context, key string) (string, bool) {
	if c.rdb != nil {
		val, err := c.rdb.Get(ctx, key).Result()
		if err == nil {
			return val, true
		}
	}
	return c.memCache.get(key)
}

func (c *CacheService) Set(ctx context.Context, key, value string, ttl time.Duration) {
	if c.rdb != nil {
		c.rdb.Set(ctx, key, value, ttl)
	}
	c.memCache.set(key, value, ttl)
}

func (c *CacheService) Del(ctx context.Context, key string) {
	if c.rdb != nil {
		c.rdb.Del(ctx, key)
	}
	c.memCache.del(key)
}

func (c *CacheService) DelPattern(ctx context.Context, pattern string) {
	if c.rdb != nil {
		keys, err := c.rdb.Keys(ctx, pattern).Result()
		if err == nil {
			for _, key := range keys {
				c.rdb.Del(ctx, key)
			}
		}
	}
	prefix := strings.ReplaceAll(pattern, "*", "")
	c.memCache.delPrefix(prefix)
}

func (c *CacheService) GetJSON(ctx context.Context, key string, dest interface{}) bool {
	raw, ok := c.Get(ctx, key)
	if !ok {
		return false
	}
	return json.Unmarshal([]byte(raw), dest) == nil
}

func (c *CacheService) SetJSON(ctx context.Context, key string, value interface{}, ttl time.Duration) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	c.Set(ctx, key, string(data), ttl)
}

func (mc *memCache) get(key string) (string, bool) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	e, ok := mc.entries[key]
	if !ok || time.Now().After(e.expiresAt) {
		return "", false
	}
	return e.value, true
}

func (mc *memCache) set(key, value string, ttl time.Duration) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if len(mc.entries) >= memCacheMaxCapacity {
		now := time.Now()
		for k, e := range mc.entries {
			if now.After(e.expiresAt) {
				delete(mc.entries, k)
			}
		}
		if len(mc.entries) >= memCacheMaxCapacity {
			for k := range mc.entries {
				delete(mc.entries, k)
				if len(mc.entries) < memCacheMaxCapacity*3/4 {
					break
				}
			}
		}
	}
	mc.entries[key] = memEntry{value: value, expiresAt: time.Now().Add(ttl)}
}

func (mc *memCache) del(key string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	delete(mc.entries, key)
}

func (mc *memCache) delPrefix(prefix string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	for k := range mc.entries {
		if strings.HasPrefix(k, prefix) {
			delete(mc.entries, k)
		}
	}
}

func (mc *memCache) cleanup() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	now := time.Now()
	for k, e := range mc.entries {
		if now.After(e.expiresAt) {
			delete(mc.entries, k)
		}
	}
}
