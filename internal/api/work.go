package api

import (
	"context"
	"fmt"
	"strconv"

	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/middleware"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	"github.com/zhaojiabo/bobobeads_server/internal/service/work"
)

type WorkHandler struct {
	pb.UnimplementedWorkServiceServer
	workService *work.Service
}

func NewWorkHandler(workService *work.Service) *WorkHandler {
	return &WorkHandler{workService: workService}
}

func (h *WorkHandler) SaveWork(ctx context.Context, req *pb.SaveWorkRequest) (*pb.SaveWorkResponse, error) {
	userID := middleware.GetUserID(ctx)
	w := &model.Work{
		Title:            req.Title,
		OriginalImageURL: req.OriginalImageUrl,
		PatternImageURL:  req.PatternImageUrl,
	}

	id, err := h.workService.SaveWork(ctx, userID, w, req.PatternData)
	if err != nil {
		return &pb.SaveWorkResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	return &pb.SaveWorkResponse{Header: okHeaderCtx(ctx), WorkId: fmt.Sprintf("%d", id)}, nil
}

func (h *WorkHandler) GetWork(ctx context.Context, req *pb.GetWorkRequest) (*pb.GetWorkResponse, error) {
	userID := middleware.GetUserID(ctx)
	workID, err := strconv.ParseUint(req.WorkId, 10, 64)
	if err != nil {
		return &pb.GetWorkResponse{Header: errHeaderCtx(ctx, apperr.InvalidArgument("invalid work_id"))}, nil
	}

	w, err := h.workService.GetWork(ctx, userID, workID)
	if err != nil {
		return &pb.GetWorkResponse{Header: errHeaderCtx(ctx, err)}, nil
	}

	patternData, err := work.DecodePatternData(w.PatternData)
	if err != nil {
		return &pb.GetWorkResponse{Header: errHeaderCtx(ctx, err)}, nil
	}

	return &pb.GetWorkResponse{
		Header:      okHeaderCtx(ctx),
		Work:        workToProto(w),
		PatternData: patternData,
	}, nil
}

func (h *WorkHandler) ListWorks(ctx context.Context, req *pb.ListWorksRequest) (*pb.ListWorksResponse, error) {
	userID := middleware.GetUserID(ctx)
	page, pageSize := getPage(req.Page)
	works, total, err := h.workService.ListWorks(ctx, userID, page, pageSize, req.SourceType)
	if err != nil {
		return &pb.ListWorksResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	var items []*pb.WorkItem
	for _, w := range works {
		items = append(items, workToProto(w))
	}
	return &pb.ListWorksResponse{
		Header: okHeaderCtx(ctx),
		Works:  items,
		Page:   pageResp(total, page, pageSize),
	}, nil
}

func (h *WorkHandler) DeleteWork(ctx context.Context, req *pb.DeleteWorkRequest) (*pb.DeleteWorkResponse, error) {
	userID := middleware.GetUserID(ctx)
	workID, err := strconv.ParseUint(req.WorkId, 10, 64)
	if err != nil {
		return &pb.DeleteWorkResponse{Header: errHeaderCtx(ctx, apperr.InvalidArgument("invalid work_id"))}, nil
	}
	if err := h.workService.DeleteWork(ctx, userID, workID); err != nil {
		return &pb.DeleteWorkResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	return &pb.DeleteWorkResponse{Header: okHeaderCtx(ctx)}, nil
}

func (h *WorkHandler) SaveDraft(ctx context.Context, req *pb.SaveDraftRequest) (*pb.SaveDraftResponse, error) {
	userID := middleware.GetUserID(ctx)
	w := &model.Work{
		Title:            req.Title,
		OriginalImageURL: req.OriginalImageUrl,
		PatternImageURL:  req.PatternImageUrl,
	}
	if req.DraftId != "" {
		id, err := strconv.ParseUint(req.DraftId, 10, 64)
		if err != nil {
			return &pb.SaveDraftResponse{Header: errHeaderCtx(ctx, apperr.InvalidArgument("invalid draft_id"))}, nil
		}
		w.ID = id
	}

	id, err := h.workService.SaveDraft(ctx, userID, w, req.PatternData)
	if err != nil {
		return &pb.SaveDraftResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	return &pb.SaveDraftResponse{Header: okHeaderCtx(ctx), DraftId: fmt.Sprintf("%d", id)}, nil
}

func (h *WorkHandler) ListDrafts(ctx context.Context, req *pb.ListDraftsRequest) (*pb.ListDraftsResponse, error) {
	userID := middleware.GetUserID(ctx)
	page, pageSize := getPage(req.Page)
	drafts, total, err := h.workService.ListDrafts(ctx, userID, page, pageSize)
	if err != nil {
		return &pb.ListDraftsResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	var items []*pb.WorkItem
	for _, w := range drafts {
		items = append(items, workToProto(w))
	}
	return &pb.ListDraftsResponse{
		Header: okHeaderCtx(ctx),
		Drafts: items,
		Page:   pageResp(total, page, pageSize),
	}, nil
}

func workToProto(w *model.Work) *pb.WorkItem {
	return &pb.WorkItem{
		WorkId:           fmt.Sprintf("%d", w.ID),
		Title:            w.Title,
		OriginalImageUrl: w.OriginalImageURL,
		PatternImageUrl:  w.PatternImageURL,
		BoardSpec:        w.BoardSpec,
		Width:            int32(w.Width),
		Height:           int32(w.Height),
		BeadCount:        int32(w.BeadCount),
		ColorCount:       int32(w.ColorCount),
		Status:           int32(w.Status),
		CreatedAt:        w.CreatedAt.Unix(),
		UpdatedAt:        w.UpdatedAt.Unix(),
		SourceType:       w.SourceType,
		SourceId:         w.SourceID,
	}
}

func getPage(p *pb.PageRequest) (int, int) {
	page := 1
	pageSize := 20
	if p != nil {
		if p.Page > 0 {
			page = int(p.Page)
		}
		if p.PageSize > 0 {
			pageSize = int(p.PageSize)
		}
	}
	return page, pageSize
}

func pageResp(total int64, page, pageSize int) *pb.PageResponse {
	return &pb.PageResponse{
		Total:    int32(total),
		Page:     int32(page),
		PageSize: int32(pageSize),
		HasMore:  int64(page*pageSize) < total,
	}
}
