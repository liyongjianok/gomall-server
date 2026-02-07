package database

import (
	"context"
	"fmt"
	"log"
	"time"

	"go-ecommerce/pkg/config"

	"github.com/redis/go-redis/v9"
)

// InitRedis 初始化 Redis 连接
func InitRedis(cfg config.RedisConfig) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Address,
		Password: cfg.Password, // 没有密码则留空
		DB:       cfg.Db,       // 默认 0
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	fmt.Println("Redis connected successfully")
	return rdb
}
