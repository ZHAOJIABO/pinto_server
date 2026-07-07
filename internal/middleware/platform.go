package middleware

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func PlatformInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if ok {
			if platforms := md.Get("x-platform"); len(platforms) > 0 {
				ctx = context.WithValue(ctx, PlatformKey, platforms[0])
			}
		}
		return handler(ctx, req)
	}
}
