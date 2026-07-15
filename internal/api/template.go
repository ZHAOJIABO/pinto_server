package api

import (
	"context"
	"fmt"
	"strconv"

	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/middleware"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	"github.com/zhaojiabo/bobobeads_server/internal/service/template"
	"github.com/zhaojiabo/bobobeads_server/internal/service/work"
)

type TemplateHandler struct {
	pb.UnimplementedTemplateServiceServer
	templateService *template.Service
}

func NewTemplateHandler(templateService *template.Service) *TemplateHandler {
	return &TemplateHandler{templateService: templateService}
}

func (h *TemplateHandler) ListCategories(ctx context.Context, req *pb.ListCategoriesRequest) (*pb.ListCategoriesResponse, error) {
	categories, counts, err := h.templateService.ListCategories(ctx)
	if err != nil {
		return &pb.ListCategoriesResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	var items []*pb.CategoryItem
	for i, c := range categories {
		items = append(items, &pb.CategoryItem{
			CategoryId:    int32(c.ID),
			Name:          c.Name,
			IconUrl:       c.IconURL,
			TemplateCount: int32(counts[i]),
		})
	}
	return &pb.ListCategoriesResponse{Header: okHeaderCtx(ctx), Categories: items}, nil
}

func (h *TemplateHandler) ListTemplates(ctx context.Context, req *pb.ListTemplatesRequest) (*pb.ListTemplatesResponse, error) {
	userID := middleware.GetUserID(ctx)
	page, pageSize := getPage(req.Page)

	input := template.ListInput{
		CategoryID: int(req.CategoryId),
		Scene:      req.Scene,
		Keyword:    req.Keyword,
		Page:       page,
		PageSize:   pageSize,
	}

	templates, total, err := h.templateService.ListTemplates(ctx, input)
	if err != nil {
		return &pb.ListTemplatesResponse{Header: errHeaderCtx(ctx, err)}, nil
	}

	templateIDs := make([]uint64, 0, len(templates))
	for _, t := range templates {
		templateIDs = append(templateIDs, t.ID)
	}
	favMap, _ := h.templateService.BatchGetFavorited(ctx, userID, templateIDs)

	var items []*pb.TemplateItem
	for _, t := range templates {
		thumbnailURL := t.ThumbnailURL
		if thumbnailURL == "" {
			thumbnailURL = t.PreviewURL
		}
		items = append(items, &pb.TemplateItem{
			TemplateId:    fmt.Sprintf("%d", t.ID),
			Title:         t.Title,
			PreviewUrl:    t.PreviewURL,
			ThumbnailUrl:  thumbnailURL,
			Description:   t.Description,
			BoardSpec:     t.BoardSpec,
			Tags:          h.templateService.SplitTags(t.Tags),
			Difficulty:    int32(t.Difficulty),
			Width:         int32(t.Width),
			Height:        int32(t.Height),
			ColorCount:    int32(t.ColorCount),
			IsFree:        t.IsFree,
			CreditCost:    int32(t.CreditCost),
			DownloadCount: int32(t.DownloadCount),
			FavoriteCount: int32(t.FavoriteCount),
			IsFavorited:   favMap[t.ID],
		})
	}
	return &pb.ListTemplatesResponse{
		Header:    okHeaderCtx(ctx),
		Templates: items,
		Page:      pageResp(total, page, pageSize),
	}, nil
}

func (h *TemplateHandler) GetTemplate(ctx context.Context, req *pb.GetTemplateRequest) (*pb.GetTemplateResponse, error) {
	userID := middleware.GetUserID(ctx)
	templateID, err := strconv.ParseUint(req.TemplateId, 10, 64)
	if err != nil {
		return &pb.GetTemplateResponse{Header: errHeaderCtx(ctx, apperr.InvalidArgument("invalid template_id"))}, nil
	}

	tpl, err := h.templateService.GetTemplate(ctx, templateID)
	if err != nil {
		return &pb.GetTemplateResponse{Header: errHeaderCtx(ctx, err)}, nil
	}

	favMap, _ := h.templateService.BatchGetFavorited(ctx, userID, []uint64{templateID})
	thumbnailURL := tpl.ThumbnailURL
	if thumbnailURL == "" {
		thumbnailURL = tpl.PreviewURL
	}

	var patternData *pb.PatternData
	if tpl.PatternData != nil {
		patternData, err = work.DecodePatternData(tpl.PatternData)
		if err != nil {
			return &pb.GetTemplateResponse{Header: errHeaderCtx(ctx, err)}, nil
		}
	}

	return &pb.GetTemplateResponse{
		Header: okHeaderCtx(ctx),
		Template: &pb.TemplateItem{
			TemplateId:    fmt.Sprintf("%d", tpl.ID),
			Title:         tpl.Title,
			PreviewUrl:    tpl.PreviewURL,
			ThumbnailUrl:  thumbnailURL,
			Description:   tpl.Description,
			BoardSpec:     tpl.BoardSpec,
			Tags:          h.templateService.SplitTags(tpl.Tags),
			Difficulty:    int32(tpl.Difficulty),
			Width:         int32(tpl.Width),
			Height:        int32(tpl.Height),
			ColorCount:    int32(tpl.ColorCount),
			IsFree:        tpl.IsFree,
			CreditCost:    int32(tpl.CreditCost),
			DownloadCount: int32(tpl.DownloadCount),
			FavoriteCount: int32(tpl.FavoriteCount),
			IsFavorited:   favMap[templateID],
		},
		PatternData: patternData,
	}, nil
}

func (h *TemplateHandler) FavoriteTemplate(ctx context.Context, req *pb.FavoriteTemplateRequest) (*pb.FavoriteTemplateResponse, error) {
	userID := middleware.GetUserID(ctx)
	templateID, err := strconv.ParseUint(req.TemplateId, 10, 64)
	if err != nil {
		return &pb.FavoriteTemplateResponse{Header: errHeaderCtx(ctx, apperr.InvalidArgument("invalid template_id"))}, nil
	}

	count, err := h.templateService.FavoriteTemplate(ctx, userID, templateID)
	if err != nil {
		return &pb.FavoriteTemplateResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	return &pb.FavoriteTemplateResponse{
		Header:        okHeaderCtx(ctx),
		IsFavorited:   true,
		FavoriteCount: int32(count),
	}, nil
}

func (h *TemplateHandler) UnfavoriteTemplate(ctx context.Context, req *pb.UnfavoriteTemplateRequest) (*pb.UnfavoriteTemplateResponse, error) {
	userID := middleware.GetUserID(ctx)
	templateID, err := strconv.ParseUint(req.TemplateId, 10, 64)
	if err != nil {
		return &pb.UnfavoriteTemplateResponse{Header: errHeaderCtx(ctx, apperr.InvalidArgument("invalid template_id"))}, nil
	}

	count, err := h.templateService.UnfavoriteTemplate(ctx, userID, templateID)
	if err != nil {
		return &pb.UnfavoriteTemplateResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	return &pb.UnfavoriteTemplateResponse{
		Header:        okHeaderCtx(ctx),
		IsFavorited:   false,
		FavoriteCount: int32(count),
	}, nil
}

func (h *TemplateHandler) ListFavoriteTemplates(ctx context.Context, req *pb.ListFavoriteTemplatesRequest) (*pb.ListFavoriteTemplatesResponse, error) {
	userID := middleware.GetUserID(ctx)
	page, pageSize := getPage(req.Page)

	templates, total, err := h.templateService.ListFavoriteTemplates(ctx, userID, page, pageSize)
	if err != nil {
		return &pb.ListFavoriteTemplatesResponse{Header: errHeaderCtx(ctx, err)}, nil
	}

	templateIDs := make([]uint64, 0, len(templates))
	for _, t := range templates {
		templateIDs = append(templateIDs, t.ID)
	}

	var items []*pb.TemplateItem
	for _, t := range templates {
		thumbnailURL := t.ThumbnailURL
		if thumbnailURL == "" {
			thumbnailURL = t.PreviewURL
		}
		items = append(items, &pb.TemplateItem{
			TemplateId:    fmt.Sprintf("%d", t.ID),
			Title:         t.Title,
			PreviewUrl:    t.PreviewURL,
			ThumbnailUrl:  thumbnailURL,
			Description:   t.Description,
			BoardSpec:     t.BoardSpec,
			Tags:          h.templateService.SplitTags(t.Tags),
			Difficulty:    int32(t.Difficulty),
			Width:         int32(t.Width),
			Height:        int32(t.Height),
			ColorCount:    int32(t.ColorCount),
			IsFree:        t.IsFree,
			CreditCost:    int32(t.CreditCost),
			DownloadCount: int32(t.DownloadCount),
			FavoriteCount: int32(t.FavoriteCount),
			IsFavorited:   true,
		})
	}
	return &pb.ListFavoriteTemplatesResponse{
		Header:    okHeaderCtx(ctx),
		Templates: items,
		Page:      pageResp(total, page, pageSize),
	}, nil
}
