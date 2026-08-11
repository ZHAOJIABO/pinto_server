package api

import (
	"context"
	"strconv"

	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/middleware"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	"github.com/zhaojiabo/bobobeads_server/internal/service/templatesubmission"
)

type TemplateSubmissionHandler struct {
	pb.UnimplementedTemplateSubmissionServiceServer
	submissionService *templatesubmission.Service
}

func NewTemplateSubmissionHandler(submissionService *templatesubmission.Service) *TemplateSubmissionHandler {
	return &TemplateSubmissionHandler{submissionService: submissionService}
}

func (h *TemplateSubmissionHandler) CreateTemplateSubmission(ctx context.Context, req *pb.CreateTemplateSubmissionRequest) (*pb.CreateTemplateSubmissionResponse, error) {
	workID, err := strconv.ParseUint(req.WorkId, 10, 64)
	if err != nil {
		return &pb.CreateTemplateSubmissionResponse{Header: errHeaderCtx(ctx, apperr.InvalidArgument("invalid work_id"))}, nil
	}

	userID := middleware.GetUserID(ctx)
	sub, err := h.submissionService.Submit(ctx, userID, templatesubmission.SubmitInput{
		WorkID:          workID,
		Title:           req.Title,
		Description:     req.Description,
		ClientRequestID: req.ClientRequestId,
	})
	if err != nil {
		return &pb.CreateTemplateSubmissionResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	return &pb.CreateTemplateSubmissionResponse{
		Header: okHeaderCtx(ctx),
		Item:   h.toProto(sub),
	}, nil
}

func (h *TemplateSubmissionHandler) ListMyTemplateSubmissions(ctx context.Context, req *pb.ListMyTemplateSubmissionsRequest) (*pb.ListMyTemplateSubmissionsResponse, error) {
	userID := middleware.GetUserID(ctx)
	subs, nextCursor, err := h.submissionService.ListMine(ctx, userID, req.Limit, req.Cursor)
	if err != nil {
		return &pb.ListMyTemplateSubmissionsResponse{Header: errHeaderCtx(ctx, err)}, nil
	}

	items := make([]*pb.TemplateSubmissionItem, 0, len(subs))
	for _, sub := range subs {
		items = append(items, h.toProto(sub))
	}
	return &pb.ListMyTemplateSubmissionsResponse{
		Header:     okHeaderCtx(ctx),
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

func (h *TemplateSubmissionHandler) toProto(sub *model.TemplateSubmission) *pb.TemplateSubmissionItem {
	templateID := ""
	if sub.TemplateID > 0 {
		templateID = strconv.FormatUint(sub.TemplateID, 10)
	}
	reviewedAt := int64(0)
	if sub.ReviewedAt != nil {
		reviewedAt = sub.ReviewedAt.Unix()
	}
	return &pb.TemplateSubmissionItem{
		SubmissionId: strconv.FormatUint(sub.ID, 10),
		WorkId:       strconv.FormatUint(sub.WorkID, 10),
		Title:        sub.Title,
		Description:  sub.Description,
		Status:       int32(sub.Status),
		ReviewReason: sub.ReviewReason,
		TemplateId:   templateID,
		BoardSpec:    sub.BoardSpec,
		Width:        int32(sub.Width),
		Height:       int32(sub.Height),
		BeadCount:    int32(sub.BeadCount),
		ColorCount:   int32(sub.ColorCount),
		PreviewUrl:   sub.PreviewURL,
		ThumbnailUrl: sub.ThumbnailURL,
		CreatedAt:    sub.CreatedAt.Unix(),
		ReviewedAt:   reviewedAt,
	}
}
