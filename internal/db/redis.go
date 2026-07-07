package db

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/zhaojiabo/bobobeads_server/conf"
	"go.uber.org/zap"
)

var RDB *redis.Client

func InitRedis() error {
	cfg := conf.GlobalConfig.Redis
	RDB = redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.DB,
		PoolSize: 100,
	})

	if err := RDB.Ping(context.Background()).Err(); err != nil {
		return fmt.Errorf("failed to connect redis: %w", err)
	}

	zap.L().Info("Redis connected", zap.String("host", cfg.Host), zap.Int("port", cfg.Port))
	return nil
}

func CloseRedis() {
	if RDB != nil {
		RDB.Close()
	}
}
