package middleware

import (
	"context"
	"strings"

	"github.com/zhaojiabo/bobobeads_server/conf"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type serviceAuthKey struct{}

func ServiceAuthInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if !isAdminTemplateService(info.FullMethod) {
			return handler(ctx, req)
		}

		token := extractServiceToken(ctx)
		if token == "" {
			zap.L().Warn("admin template service: missing service token", zap.String("method", info.FullMethod))
			return nil, status.Error(codes.Unauthenticated, "service authentication required")
		}

		expectedToken := conf.GlobalConfig.AdminService.Token
		if expectedToken == "" || token != expectedToken {
			zap.L().Warn("admin template service: invalid service token", zap.String("method", info.FullMethod))
			return nil, status.Error(codes.PermissionDenied, "invalid service credentials")
		}

		ctx = context.WithValue(ctx, serviceAuthKey{}, true)
		return handler(ctx, req)
	}
}

func isAdminTemplateService(method string) bool {
	return strings.Contains(method, "AdminTemplateService")
}

func extractServiceToken(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	tokens := md.Get("x-service-token")
	if len(tokens) == 0 {
		return ""
	}
	return tokens[0]
}

func IsServiceAuthenticated(ctx context.Context) bool {
	v, _ := ctx.Value(serviceAuthKey{}).(bool)
	return v
}
