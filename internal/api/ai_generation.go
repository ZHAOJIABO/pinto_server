package api

import (
	"context"
	"fmt"
	"strconv"

	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/middleware"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	ai_generation "github.com/zhaojiabo/bobobeads_server/internal/service/ai_generation"
)

type AIGenerationHandler struct {
	pb.UnimplementedAIGenerationServiceServer
	aiService *ai_generation.Service
}

func NewAIGenerationHandler(aiService *ai_generation.Service) *AIGenerationHandler {
	return &AIGenerationHandler{aiService: aiService}
}

func (h *AIGenerationHandler) ListAIStyles(ctx context.Context, req *pb.ListAIStylesRequest) (*pb.ListAIStylesResponse, error) {
	styles, err := h.aiService.ListStyles(ctx)
	if err != nil {
		return &pb.ListAIStylesResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	var items []*pb.AIStyleItem
	for _, s := range styles {
		items = append(items, &pb.AIStyleItem{
			StyleId:     fmt.Sprintf("%d", s.ID),
			StyleKey:    s.StyleKey,
			Name:        s.Name,
			Description: s.Description,
			CoverUrl:    s.CoverURL,
			ExampleUrl:  s.ExampleURL,
			CostCredits: int32(s.CostCredits),
		})
	}
	return &pb.ListAIStylesResponse{Header: okHeaderCtx(ctx), Styles: items}, nil
}

func (h *AIGenerationHandler) CreateStyleGeneration(ctx context.Context, req *pb.CreateStyleGenerationRequest) (*pb.CreateStyleGenerationResponse, error) {
	userID := middleware.GetUserID(ctx)
	styleID, err := strconv.ParseUint(req.StyleId, 10, 64)
	if err != nil {
		return &pb.CreateStyleGenerationResponse{Header: errHeaderCtx(ctx, apperr.InvalidArgument("invalid style_id"))}, nil
	}

	result, err := h.aiService.CreateStyleGeneration(ctx, userID, styleID, req.InputFileKey, req.ClientRequestId)
	if err != nil {
		return &pb.CreateStyleGenerationResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	return &pb.CreateStyleGenerationResponse{
		Header:           okHeaderCtx(ctx),
		TaskId:           result.TaskID,
		Status:           int32(result.Status),
		CreditsDeducted:  int32(result.CreditsDeducted),
		RemainingBalance: int32(result.RemainingBalance),
		Duplicated:       result.Duplicated,
	}, nil
}

func (h *AIGenerationHandler) GetStyleGeneration(ctx context.Context, req *pb.GetStyleGenerationRequest) (*pb.GetStyleGenerationResponse, error) {
	userID := middleware.GetUserID(ctx)
	task, err := h.aiService.GetStyleGeneration(ctx, userID, req.TaskId)
	if err != nil {
		return &pb.GetStyleGenerationResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	return &pb.GetStyleGenerationResponse{
		Header: okHeaderCtx(ctx),
		Task:   aiTaskToProto(task),
	}, nil
}

func (h *AIGenerationHandler) ListStyleGenerations(ctx context.Context, req *pb.ListStyleGenerationsRequest) (*pb.ListStyleGenerationsResponse, error) {
	userID := middleware.GetUserID(ctx)
	page, pageSize := getPage(req.Page)
	tasks, total, err := h.aiService.ListStyleGenerations(ctx, userID, page, pageSize)
	if err != nil {
		return &pb.ListStyleGenerationsResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	var items []*pb.AIGenerationItem
	for _, t := range tasks {
		items = append(items, aiTaskToProto(t))
	}
	return &pb.ListStyleGenerationsResponse{
		Header: okHeaderCtx(ctx),
		Tasks:  items,
		Page:   pageResp(total, page, pageSize),
	}, nil
}

func aiTaskToProto(t *model.AIGeneration) *pb.AIGenerationItem {
	item := &pb.AIGenerationItem{
		TaskId:          t.TaskID,
		StyleId:         fmt.Sprintf("%d", t.StyleID),
		InputImageUrl:   t.InputImageURL,
		OutputImageUrl:  t.OutputImageURL,
		Status:          int32(t.Status),
		CreditsDeducted: int32(t.CreditsDeducted),
		ErrorMessage:    t.ErrorMessage,
		CreatedAt:       t.CreatedAt.Unix(),
	}
	if t.CompletedAt != nil {
		item.CompletedAt = t.CompletedAt.Unix()
	}
	return item
}
