package api

import (
	"context"

	"github.com/zhaojiabo/bobobeads_server/internal/middleware"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	"github.com/zhaojiabo/bobobeads_server/internal/service/media"
)

type MediaHandler struct {
	pb.UnimplementedMediaServiceServer
	mediaService *media.Service
}

func NewMediaHandler(mediaService *media.Service) *MediaHandler {
	return &MediaHandler{mediaService: mediaService}
}

func (h *MediaHandler) GetUploadToken(ctx context.Context, req *pb.GetUploadTokenRequest) (*pb.GetUploadTokenResponse, error) {
	userID := middleware.GetUserID(ctx)
	token, err := h.mediaService.GetUploadToken(ctx, userID, req.FileName, req.ContentType, req.Purpose)
	if err != nil {
		return &pb.GetUploadTokenResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	return &pb.GetUploadTokenResponse{
		Header:       okHeaderCtx(ctx),
		UploadUrl:    token.UploadURL,
		FileKey:      token.FileKey,
		FormData:     token.FormData,
		ExpiresAt:    token.ExpiresAt,
		Headers:      token.Headers,
		UploadMethod: token.UploadMethod,
		PublicUrl:    token.PublicURL,
		MaxFileSize:  token.MaxFileSize,
	}, nil
}

func (h *MediaHandler) ReportUpload(ctx context.Context, req *pb.ReportUploadRequest) (*pb.ReportUploadResponse, error) {
	userID := middleware.GetUserID(ctx)
	url, err := h.mediaService.ReportUpload(ctx, userID, req.FileKey, req.FileSize)
	if err != nil {
		return &pb.ReportUploadResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	return &pb.ReportUploadResponse{
		Header:  okHeaderCtx(ctx),
		FileUrl: url,
	}, nil
}

func (h *MediaHandler) GetFileUrl(ctx context.Context, req *pb.GetFileUrlRequest) (*pb.GetFileUrlResponse, error) {
	url, expiresAt, err := h.mediaService.GetFileURL(ctx, req.FileKey)
	if err != nil {
		return &pb.GetFileUrlResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	return &pb.GetFileUrlResponse{
		Header:    okHeaderCtx(ctx),
		Url:       url,
		ExpiresAt: expiresAt,
	}, nil
}
