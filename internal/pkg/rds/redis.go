package rds

import (
	"context"
	"fmt"
	"log"

	"github.com/redis/go-redis/v9"

	"github.com/gamewinner2019/FlowManPay/internal/config"
)

var rdb *redis.Client

// Init initializes the Redis connection.
func Init() *redis.Client {
	cfg := config.Get()

	rdb = redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("Redis连接失败: %v", err)
	}

	log.Println("Redis连接成功")
	return rdb
}

// Get returns the Redis client instance.
func Get() *redis.Client {
	return rdb
}
