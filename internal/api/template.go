package api

import (
	"context"
	"fmt"
	"strconv"

	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	"github.com/zhaojiabo/bobobeads_server/internal/service/template"
)

type TemplateHandler struct {
	pb.UnimplementedTemplateServiceServer
	templateService *template.Service
}

func NewTemplateHandler(templateService *template.Service) *TemplateHandler {
	return &TemplateHandler{templateService: templateService}
}

func (h *TemplateHandler) ListCategories(ctx context.Context, req *pb.ListCategoriesRequest) (*pb.ListCategoriesResponse, error) {
	categories, err := h.templateService.ListCategories(ctx)
	if err != nil {
		return &pb.ListCategoriesResponse{Header: errHeader(err)}, nil
	}
	var items []*pb.CategoryItem
	for _, c := range categories {
		items = append(items, &pb.CategoryItem{
			CategoryId: int32(c.ID),
			Name:       c.Name,
			IconUrl:    c.IconURL,
		})
	}
	return &pb.ListCategoriesResponse{Header: okHeader(), Categories: items}, nil
}

func (h *TemplateHandler) ListTemplates(ctx context.Context, req *pb.ListTemplatesRequest) (*pb.ListTemplatesResponse, error) {
	page, pageSize := getPage(req.Page)
	templates, total, err := h.templateService.ListTemplates(ctx, int(req.CategoryId), page, pageSize)
	if err != nil {
		return &pb.ListTemplatesResponse{Header: errHeader(err)}, nil
	}
	var items []*pb.TemplateItem
	for _, t := range templates {
		items = append(items, &pb.TemplateItem{
			TemplateId:    fmt.Sprintf("%d", t.ID),
			Title:         t.Title,
			PreviewUrl:    t.PreviewURL,
			BoardSpec:     t.BoardSpec,
			IsFree:        t.IsFree,
			CreditCost:    int32(t.CreditCost),
			DownloadCount: int32(t.DownloadCount),
		})
	}
	return &pb.ListTemplatesResponse{
		Header:    okHeader(),
		Templates: items,
		Page:      pageResp(total, page, pageSize),
	}, nil
}

func (h *TemplateHandler) GetTemplate(ctx context.Context, req *pb.GetTemplateRequest) (*pb.GetTemplateResponse, error) {
	templateID, _ := strconv.ParseUint(req.TemplateId, 10, 64)
	tpl, err := h.templateService.GetTemplate(ctx, templateID)
	if err != nil {
		return &pb.GetTemplateResponse{Header: errHeader(err)}, nil
	}
	return &pb.GetTemplateResponse{
		Header: okHeader(),
		Template: &pb.TemplateItem{
			TemplateId:    fmt.Sprintf("%d", tpl.ID),
			Title:         tpl.Title,
			PreviewUrl:    tpl.PreviewURL,
			BoardSpec:     tpl.BoardSpec,
			IsFree:        tpl.IsFree,
			CreditCost:    int32(tpl.CreditCost),
			DownloadCount: int32(tpl.DownloadCount),
		},
	}, nil
}
