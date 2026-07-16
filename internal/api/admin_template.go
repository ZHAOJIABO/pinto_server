package api

import (
	"context"

	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	templateservice "github.com/zhaojiabo/bobobeads_server/internal/service/template"
	"github.com/zhaojiabo/bobobeads_server/internal/service/work"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AdminTemplateHandler struct {
	pb.UnimplementedAdminTemplateServiceServer
	adminService *templateservice.AdminService
}

func NewAdminTemplateHandler(adminService *templateservice.AdminService) *AdminTemplateHandler {
	return &AdminTemplateHandler{adminService: adminService}
}

func (h *AdminTemplateHandler) PublishTemplate(ctx context.Context, req *pb.PublishTemplateRequest) (*pb.PublishTemplateResponse, error) {
	if req.PatternData == nil {
		return &pb.PublishTemplateResponse{Success: false, Error: "pattern_data required"}, nil
	}

	if err := work.ValidatePatternData(req.PatternData); err != nil {
		return &pb.PublishTemplateResponse{Success: false, Error: "pattern validation failed: " + err.Error()}, nil
	}

	patternJSON := work.PatternDataToJSONMap(req.PatternData)

	payload := templateservice.PublishPayload{
		IdempotencyKey:  req.IdempotencyKey,
		DraftRevisionID: req.DraftRevisionId,
		UpdatePayload: templateservice.UpdatePayload{
			Title:        req.Title,
			Description:  req.Description,
			CategoryID:   int(req.CategoryId),
			Tags:         req.Tags,
			Difficulty:   int8(req.Difficulty),
			BoardSpec:    req.BoardSpec,
			PreviewURL:   req.PreviewUrl,
			ThumbnailURL: req.ThumbnailUrl,
			PatternData:  patternJSON,
			Width:        int(req.Width),
			Height:       int(req.Height),
			ColorCount:   int(req.ColorCount),
			BeadCount:    int(req.BeadCount),
		},
	}

	templateID, err := h.adminService.PublishTemplate(ctx, payload)
	if err != nil {
		zap.L().Error("publish template failed", zap.Error(err))
		return &pb.PublishTemplateResponse{Success: false, Error: err.Error()}, nil
	}

	return &pb.PublishTemplateResponse{
		TemplateId: templateID,
		Success:    true,
	}, nil
}

func (h *AdminTemplateHandler) UnpublishTemplate(ctx context.Context, req *pb.UnpublishTemplateRequest) (*pb.UnpublishTemplateResponse, error) {
	if req.TemplateId == 0 {
		return &pb.UnpublishTemplateResponse{Success: false, Error: "template_id required"}, nil
	}

	if err := h.adminService.UnpublishTemplate(ctx, req.TemplateId, req.Reason); err != nil {
		return &pb.UnpublishTemplateResponse{Success: false, Error: err.Error()}, nil
	}

	return &pb.UnpublishTemplateResponse{Success: true}, nil
}

func (h *AdminTemplateHandler) GetPublishStatus(ctx context.Context, req *pb.GetPublishStatusRequest) (*pb.GetPublishStatusResponse, error) {
	if req.IdempotencyKey == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency_key required")
	}

	record, err := h.adminService.GetPublishStatus(ctx, req.IdempotencyKey)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if record == nil {
		return &pb.GetPublishStatusResponse{Found: false}, nil
	}

	return &pb.GetPublishStatusResponse{
		Found:      true,
		TemplateId: record.TemplateID,
		Status:     record.Status,
	}, nil
}

// Ensure Template model has ID field accessible
func init() {
	_ = model.Template{}
}
