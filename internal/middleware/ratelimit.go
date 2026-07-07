package middleware

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func RateLimitInterceptor(rps int) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		userID := GetUserID(ctx)
		if userID == 0 {
			return handler(ctx, req)
		}

		key := fmt.Sprintf("ratelimit:%d:%s", userID, info.FullMethod)
		count, err := db.RDB.Incr(ctx, key).Result()
		if err != nil && err != redis.Nil {
			return handler(ctx, req)
		}
		if count == 1 {
			db.RDB.Expire(ctx, key, time.Second)
		}
		if count > int64(rps) {
			return nil, status.Error(codes.ResourceExhausted, "rate limit exceeded")
		}

		return handler(ctx, req)
	}
}
