package middleware

import (
	"context"
	"time"

	"github.com/zhaojiabo/bobobeads_server/internal/logger"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const reqMetaKey contextKey = "req_meta"

// reqMeta lets inner interceptors report values back to the outer logging
// interceptor, which has already returned from their ctx by the time it logs.
type reqMeta struct {
	userID uint64
}

func setLogUserID(ctx context.Context, userID uint64) {
	if m, ok := ctx.Value(reqMetaKey).(*reqMeta); ok {
		m.userID = userID
	}
}

// LoggingInterceptor emits one access log line per gRPC call. It must run after
// TraceInterceptor so trace_id is available, and before the auth interceptors so
// rejected requests are still logged.
func LoggingInterceptor(slowThreshold time.Duration) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		meta := &reqMeta{}
		ctx = context.WithValue(ctx, reqMetaKey, meta)

		l := zap.L().With(zap.String("trace_id", GetTraceID(ctx)))
		ctx = logger.NewContext(ctx, l)

		start := time.Now()
		resp, err := handler(ctx, req)
		elapsed := time.Since(start)

		fields := []zap.Field{
			zap.String("method", info.FullMethod),
			zap.String("code", status.Code(err).String()),
			zap.Duration("elapsed", elapsed),
			zap.String("peer", clientIP(ctx)),
		}
		if meta.userID != 0 {
			fields = append(fields, zap.Uint64("user_id", meta.userID))
		}
		if platform := metadataValue(ctx, "x-platform"); platform != "" {
			fields = append(fields, zap.String("platform", platform))
		}

		// Handlers report business failures inside the response header rather
		// than as a gRPC error, so a bare status code hides most problems.
		bizCode, bizMessage := responseStatus(resp)
		if bizCode != 0 {
			fields = append(fields,
				zap.Int32("biz_code", bizCode),
				zap.String("biz_message", bizMessage))
		}

		switch {
		case err != nil:
			l.Error("request failed", append(fields, zap.Error(err))...)
		case bizCode != 0:
			l.Warn("request rejected", fields...)
		case slowThreshold > 0 && elapsed > slowThreshold:
			l.Warn("slow request", fields...)
		default:
			l.Info("request", fields...)
		}

		return resp, err
	}
}

func responseStatus(resp interface{}) (int32, string) {
	h, ok := resp.(interface{ GetHeader() *pb.ResponseHeader })
	if !ok || h.GetHeader() == nil {
		return 0, ""
	}
	return h.GetHeader().GetCode(), h.GetHeader().GetMessage()
}

func clientIP(ctx context.Context) string {
	if ip := gatewayClientIP(ctx); ip != "" {
		return ip
	}
	return clientPeerIP(ctx)
}

func metadataValue(ctx context.Context, key string) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	if values := md.Get(key); len(values) > 0 {
		return values[0]
	}
	return ""
}
