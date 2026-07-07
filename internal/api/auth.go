package api

import (
	"context"
	"fmt"

	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/middleware"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	"github.com/zhaojiabo/bobobeads_server/internal/service/auth"
)

type AuthHandler struct {
	pb.UnimplementedAuthServiceServer
	authService *auth.Service
}

func NewAuthHandler(authService *auth.Service) *AuthHandler {
	return &AuthHandler{authService: authService}
}

func (h *AuthHandler) GuestLogin(ctx context.Context, req *pb.GuestLoginRequest) (*pb.LoginResponse, error) {
	if req.DeviceId == "" {
		return &pb.LoginResponse{Header: errHeaderCtx(ctx, apperr.InvalidArgument("device_id is required"))}, nil
	}

	user, tokens, err := h.authService.GuestLogin(ctx, req.DeviceId)
	if err != nil {
		return &pb.LoginResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	return &pb.LoginResponse{
		Header:       okHeaderCtx(ctx),
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresIn:    tokens.ExpiresIn,
		User:         userToProto(user),
	}, nil
}

func (h *AuthHandler) PhoneLogin(ctx context.Context, req *pb.PhoneLoginRequest) (*pb.LoginResponse, error) {
	if req.Phone == "" {
		return &pb.LoginResponse{Header: errHeaderCtx(ctx, apperr.InvalidArgument("phone is required"))}, nil
	}

	user, tokens, err := h.authService.PhoneLogin(ctx, req.Phone, req.Code)
	if err != nil {
		return &pb.LoginResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	return &pb.LoginResponse{
		Header:       okHeaderCtx(ctx),
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresIn:    tokens.ExpiresIn,
		User:         userToProto(user),
	}, nil
}

func (h *AuthHandler) SendSmsCode(ctx context.Context, req *pb.SendSmsCodeRequest) (*pb.SendSmsCodeResponse, error) {
	// TODO: implement SMS sending
	return &pb.SendSmsCodeResponse{Header: okHeaderCtx(ctx)}, nil
}

func (h *AuthHandler) WechatLogin(ctx context.Context, req *pb.WechatLoginRequest) (*pb.LoginResponse, error) {
	user, tokens, err := h.authService.WechatLogin(ctx, req.Code, req.Platform.String())
	if err != nil {
		return &pb.LoginResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	return &pb.LoginResponse{
		Header:       okHeaderCtx(ctx),
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresIn:    tokens.ExpiresIn,
		User:         userToProto(user),
	}, nil
}

func (h *AuthHandler) AppleLogin(ctx context.Context, req *pb.AppleLoginRequest) (*pb.LoginResponse, error) {
	user, tokens, err := h.authService.AppleLogin(ctx, req.IdentityToken, req.AuthorizationCode, req.FullName)
	if err != nil {
		return &pb.LoginResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	return &pb.LoginResponse{
		Header:       okHeaderCtx(ctx),
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresIn:    tokens.ExpiresIn,
		User:         userToProto(user),
	}, nil
}

func (h *AuthHandler) RefreshToken(ctx context.Context, req *pb.RefreshTokenRequest) (*pb.LoginResponse, error) {
	tokens, err := h.authService.RefreshToken(ctx, req.RefreshToken)
	if err != nil {
		return &pb.LoginResponse{Header: errHeaderCtx(ctx, err)}, nil
	}

	userID := middleware.GetUserID(ctx)
	return &pb.LoginResponse{
		Header:       okHeaderCtx(ctx),
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresIn:    tokens.ExpiresIn,
		User:         &pb.UserInfo{UserId: fmt.Sprintf("%d", userID)},
	}, nil
}
