package middleware

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const TraceIDKey contextKey = "trace_id"

func TraceInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		traceID := extractRequestID(ctx)
		if traceID == "" {
			traceID = uuid.NewString()
		}
		ctx = context.WithValue(ctx, TraceIDKey, traceID)
		return handler(ctx, req)
	}
}

func extractRequestID(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	if values := md.Get("x-request-id"); len(values) > 0 {
		return values[0]
	}
	return ""
}

func GetTraceID(ctx context.Context) string {
	id, _ := ctx.Value(TraceIDKey).(string)
	return id
}
