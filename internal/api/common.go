package api

import (
	"context"
	"fmt"

	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/middleware"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
)

func okHeader() *pb.ResponseHeader {
	return &pb.ResponseHeader{Code: 0, Message: "success"}
}

func okHeaderCtx(ctx context.Context) *pb.ResponseHeader {
	return &pb.ResponseHeader{
		Code:    0,
		Message: "success",
		TraceId: middleware.GetTraceID(ctx),
	}
}

func errHeader(err error) *pb.ResponseHeader {
	if ae, ok := apperr.IsAppError(err); ok {
		return &pb.ResponseHeader{Code: ae.Code, Message: ae.Message}
	}
	return &pb.ResponseHeader{Code: apperr.CodeInternal, Message: err.Error()}
}

func errHeaderCtx(ctx context.Context, err error) *pb.ResponseHeader {
	h := errHeader(err)
	h.TraceId = middleware.GetTraceID(ctx)
	return h
}

func userToProto(user *model.User) *pb.UserInfo {
	if user == nil {
		return nil
	}
	return &pb.UserInfo{
		UserId:    fmt.Sprintf("%d", user.ID),
		Nickname:  user.Nickname,
		AvatarUrl: user.AvatarURL,
		Phone:     user.Phone,
	}
}
