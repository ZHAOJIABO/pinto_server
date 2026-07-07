package middleware

import (
	"context"
	"strings"

	"github.com/zhaojiabo/bobobeads_server/internal/service/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type contextKey string

const (
	UserIDKey   contextKey = "user_id"
	PlatformKey contextKey = "platform"
)

var publicMethods = map[string]bool{
	"/bobobeads.v1.AuthService/GuestLogin":    true,
	"/bobobeads.v1.AuthService/PhoneLogin":    true,
	"/bobobeads.v1.AuthService/SendSmsCode":   true,
	"/bobobeads.v1.AuthService/WechatLogin":   true,
	"/bobobeads.v1.AuthService/AppleLogin":    true,
	"/bobobeads.v1.AuthService/RefreshToken":  true,
	"/bobobeads.v1.SystemService/GetAppConfig": true,
	"/bobobeads.v1.SystemService/CheckUpdate":  true,
	"/bobobeads.v1.SystemService/GetBanners":   true,
	"/bobobeads.v1.SystemService/GetBeadColors": true,
	"/bobobeads.v1.SystemService/GetBoardSpecs": true,
	"/bobobeads.v1.CommunityService/GetFeed":    true,
	"/bobobeads.v1.CommunityService/GetPostDetail": true,
	"/bobobeads.v1.ReportService/PaymentCallback": true,
}

func AuthInterceptor(authService *auth.Service) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if publicMethods[info.FullMethod] {
			return handler(ctx, req)
		}

		token := extractToken(ctx)
		if token == "" {
			return nil, status.Error(codes.Unauthenticated, "missing token")
		}

		userID, err := authService.ValidateAccessToken(token)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}

		ctx = context.WithValue(ctx, UserIDKey, userID)
		return handler(ctx, req)
	}
}

func extractToken(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return ""
	}
	token := values[0]
	if strings.HasPrefix(token, "Bearer ") {
		token = token[7:]
	}
	return token
}

func GetUserID(ctx context.Context) uint64 {
	id, _ := ctx.Value(UserIDKey).(uint64)
	return id
}

func GetPlatform(ctx context.Context) string {
	p, _ := ctx.Value(PlatformKey).(string)
	return p
}
