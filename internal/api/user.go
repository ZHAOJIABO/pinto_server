package api

import (
	"context"
	"fmt"

	"github.com/zhaojiabo/bobobeads_server/internal/middleware"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	"github.com/zhaojiabo/bobobeads_server/internal/service/user"
)

type UserHandler struct {
	pb.UnimplementedUserServiceServer
	userService *user.Service
}

func NewUserHandler(userService *user.Service) *UserHandler {
	return &UserHandler{userService: userService}
}

func (h *UserHandler) GetUserInfo(ctx context.Context, req *pb.GetUserInfoRequest) (*pb.GetUserInfoResponse, error) {
	userID := middleware.GetUserID(ctx)
	u, err := h.userService.GetUserInfo(ctx, userID)
	if err != nil {
		return &pb.GetUserInfoResponse{Header: errHeader(err)}, nil
	}
	return &pb.GetUserInfoResponse{
		Header: okHeader(),
		User: &pb.UserInfo{
			UserId:    fmt.Sprintf("%d", u.ID),
			Nickname:  u.Nickname,
			AvatarUrl: u.AvatarURL,
			Phone:     u.Phone,
		},
	}, nil
}

func (h *UserHandler) UpdateUserInfo(ctx context.Context, req *pb.UpdateUserInfoRequest) (*pb.UpdateUserInfoResponse, error) {
	userID := middleware.GetUserID(ctx)
	if err := h.userService.UpdateUserInfo(ctx, userID, req.Nickname, req.AvatarUrl); err != nil {
		return &pb.UpdateUserInfoResponse{Header: errHeader(err)}, nil
	}
	return &pb.UpdateUserInfoResponse{Header: okHeader()}, nil
}

func (h *UserHandler) BindPhone(ctx context.Context, req *pb.BindPhoneRequest) (*pb.BindPhoneResponse, error) {
	userID := middleware.GetUserID(ctx)
	if err := h.userService.BindPhone(ctx, userID, req.Phone, req.Code); err != nil {
		return &pb.BindPhoneResponse{Header: errHeader(err)}, nil
	}
	return &pb.BindPhoneResponse{Header: okHeader()}, nil
}

func (h *UserHandler) DeleteAccount(ctx context.Context, req *pb.DeleteAccountRequest) (*pb.DeleteAccountResponse, error) {
	userID := middleware.GetUserID(ctx)
	if err := h.userService.DeleteAccount(ctx, userID); err != nil {
		return &pb.DeleteAccountResponse{Header: errHeader(err)}, nil
	}
	return &pb.DeleteAccountResponse{Header: okHeader()}, nil
}
