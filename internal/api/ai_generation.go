package api

import (
	"context"
	"fmt"
	"strconv"
	"time"

	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/middleware"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	ai_generation "github.com/zhaojiabo/bobobeads_server/internal/service/ai_generation"
)

type AIGenerationHandler struct {
	pb.UnimplementedAIGenerationServiceServer
	aiService   *ai_generation.Service
	avgDuration time.Duration
}

func NewAIGenerationHandler(aiService *ai_generation.Service, avgDurationSec int) *AIGenerationHandler {
	if avgDurationSec <= 0 {
		avgDurationSec = 120
	}
	return &AIGenerationHandler{
		aiService:   aiService,
		avgDuration: time.Duration(avgDurationSec) * time.Second,
	}
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

func (h *AIGenerationHandler) RetryStyleGeneration(ctx context.Context, req *pb.RetryStyleGenerationRequest) (*pb.RetryStyleGenerationResponse, error) {
	userID := middleware.GetUserID(ctx)
	result, err := h.aiService.RetryStyleGeneration(ctx, userID, req.TaskId, req.ClientRequestId)
	if err != nil {
		return &pb.RetryStyleGenerationResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	return &pb.RetryStyleGenerationResponse{
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
		Task:   h.aiTaskToProto(task),
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
		items = append(items, h.aiTaskToProto(t))
	}
	return &pb.ListStyleGenerationsResponse{
		Header: okHeaderCtx(ctx),
		Tasks:  items,
		Page:   pageResp(total, page, pageSize),
	}, nil
}

func (h *AIGenerationHandler) aiTaskToProto(t *model.AIGeneration) *pb.AIGenerationItem {
	item := &pb.AIGenerationItem{
		TaskId:             t.TaskID,
		StyleId:            fmt.Sprintf("%d", t.StyleID),
		InputImageUrl:      t.InputImageURL,
		InputThumbnailUrl:  t.InputThumbnailURL,
		OutputImageUrl:     t.OutputImageURL,
		OutputThumbnailUrl: t.OutputThumbnailURL,
		Status:             int32(t.Status),
		CreditsDeducted:    int32(t.CreditsDeducted),
		ErrorMessage:       t.ErrorMessage,
		CreatedAt:          t.CreatedAt.Unix(),
		Progress:           h.progressOf(t),
	}
	if t.StartedAt != nil {
		item.StartedAt = t.StartedAt.Unix()
	}
	if t.CompletedAt != nil {
		item.CompletedAt = t.CompletedAt.Unix()
	}
	return item
}

// progressOf estimates a percentage from elapsed time. No provider reports real
// progress, so this exists purely so the client can animate a bar instead of a
// spinner. It never reaches 100 before the task is actually done, because a bar
// that sits at 100% while the user still waits reads as a hang.
func (h *AIGenerationHandler) progressOf(t *model.AIGeneration) int32 {
	const (
		queued  = 5
		started = 10
		ceiling = 95
	)
	switch t.Status {
	case model.AIGenStatusPending:
		return queued
	case model.AIGenStatusSucceeded:
		return 100
	case model.AIGenStatusRunning:
		if t.StartedAt == nil {
			return started
		}
		elapsed := time.Since(*t.StartedAt)
		if elapsed <= 0 {
			return started
		}
		progress := started + int64(float64(ceiling-started)*elapsed.Seconds()/h.avgDuration.Seconds())
		if progress > ceiling {
			return ceiling
		}
		return int32(progress)
	default:
		// Failed, cancelled and expired: the client branches on status and shows
		// error_message, so any number here would only be noise.
		return 0
	}
}
