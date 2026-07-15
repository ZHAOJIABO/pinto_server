package api

import (
	"context"
	"fmt"
	"strconv"

	"github.com/zhaojiabo/bobobeads_server/internal/middleware"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	"github.com/zhaojiabo/bobobeads_server/internal/service/generation"
	"github.com/zhaojiabo/bobobeads_server/internal/service/work"
)

type GenerationHandler struct {
	pb.UnimplementedGenerationServiceServer
	generationService *generation.Service
}

func NewGenerationHandler(generationService *generation.Service) *GenerationHandler {
	return &GenerationHandler{generationService: generationService}
}

func (h *GenerationHandler) CreateGeneration(ctx context.Context, req *pb.CreateGenerationRequest) (*pb.CreateGenerationResponse, error) {
	userID := middleware.GetUserID(ctx)
	result, err := h.generationService.CreateGeneration(ctx, userID,
		req.BoardSpec, req.SourceType, req.SourceId, req.ClientRequestId)
	if err != nil {
		return &pb.CreateGenerationResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	return &pb.CreateGenerationResponse{
		Header:           okHeaderCtx(ctx),
		GenerationId:     result.GenerationID,
		CreditsDeducted:  int32(result.CreditsDeducted),
		RemainingBalance: int32(result.RemainingBalance),
		ExpiresAt:        result.ExpiresAt,
		Duplicated:       result.Duplicated,
	}, nil
}

func (h *GenerationHandler) CompleteGeneration(ctx context.Context, req *pb.CompleteGenerationRequest) (*pb.CompleteGenerationResponse, error) {
	userID := middleware.GetUserID(ctx)
	workData := &model.Work{
		Title:            req.Title,
		OriginalImageURL: req.OriginalImageUrl,
		PatternImageURL:  req.PatternImageUrl,
	}

	workData.PatternData = work.PatternDataToJSONMap(req.PatternData)

	result, err := h.generationService.CompleteGeneration(ctx, userID, req.GenerationId, workData)
	if err != nil {
		return &pb.CompleteGenerationResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	return &pb.CompleteGenerationResponse{
		Header:     okHeaderCtx(ctx),
		WorkId:     fmt.Sprintf("%d", result.WorkID),
		Duplicated: result.Duplicated,
	}, nil
}

func (h *GenerationHandler) CancelGeneration(ctx context.Context, req *pb.CancelGenerationRequest) (*pb.CancelGenerationResponse, error) {
	userID := middleware.GetUserID(ctx)
	refunded, err := h.generationService.CancelGeneration(ctx, userID, req.GenerationId, req.Reason)
	if err != nil {
		return &pb.CancelGenerationResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	return &pb.CancelGenerationResponse{
		Header:          okHeaderCtx(ctx),
		CreditsRefunded: int32(refunded),
	}, nil
}

func (h *GenerationHandler) GetGenerationStatus(ctx context.Context, req *pb.GetGenerationStatusRequest) (*pb.GetGenerationStatusResponse, error) {
	userID := middleware.GetUserID(ctx)
	gen, err := h.generationService.GetStatus(ctx, userID, req.GenerationId)
	if err != nil {
		return &pb.GetGenerationStatusResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	workID := ""
	if gen.WorkID > 0 {
		workID = strconv.FormatUint(gen.WorkID, 10)
	}
	return &pb.GetGenerationStatusResponse{
		Header:          okHeaderCtx(ctx),
		Status:          int32(gen.Status),
		CreditsDeducted: int32(gen.CreditsDeducted),
		WorkId:          workID,
	}, nil
}
