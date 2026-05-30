package cache

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

var rdb *redis.Client

// Init подключается к Redis. Вызывается один раз при старте из initialize.go.
// Если Redis недоступен — кэш работает как no-op, сервер продолжает работу без кэша.
func Init(host, port, password string) {
	if host == "" || port == "" {
		log.Println("[cache] REDIS_HOST/REDIS_PORT не заданы — кэш отключён")
		return
	}

	rdb = redis.NewClient(&redis.Options{
		Addr:     host + ":" + port,
		Password: password,
		DB:       0,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Printf("[cache] Redis недоступен (%s:%s): %v — кэш отключён", host, port, err)
		rdb = nil
	} else {
		log.Printf("[cache] Redis подключён (%s:%s)", host, port)
	}
}

// Cache — типизированная обёртка над Redis с JSON-сериализацией значений.
// Ключи Redis: "<prefix>:<key>".
type Cache[V any] struct {
	prefix string
}

func New[V any](prefix string) *Cache[V] {
	return &Cache[V]{prefix: prefix}
}

func (c *Cache[V]) k(key string) string {
	return c.prefix + ":" + key
}

func (c *Cache[V]) Set(key string, value V, ttl time.Duration) {
	if rdb == nil {
		return
	}
	data, err := json.Marshal(value)
	if err != nil {
		log.Printf("[cache] marshal error key=%s: %v", key, err)
		return
	}
	if err := rdb.Set(context.Background(), c.k(key), data, ttl).Err(); err != nil {
		log.Printf("[cache] set error key=%s: %v", key, err)
	}
}

func (c *Cache[V]) Get(key string) (V, bool) {
	var zero V
	if rdb == nil {
		return zero, false
	}
	data, err := rdb.Get(context.Background(), c.k(key)).Bytes()
	if err != nil {
		if err != redis.Nil {
			log.Printf("[cache] get error key=%s: %v", key, err)
		}
		return zero, false
	}
	var value V
	if err := json.Unmarshal(data, &value); err != nil {
		log.Printf("[cache] unmarshal error key=%s: %v", key, err)
		return zero, false
	}
	return value, true
}

func (c *Cache[V]) Delete(key string) {
	if rdb == nil {
		return
	}
	if err := rdb.Del(context.Background(), c.k(key)).Err(); err != nil {
		log.Printf("[cache] delete error key=%s: %v", key, err)
	}
}
