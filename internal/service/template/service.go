package template

import (
	"context"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
)

type Service struct {
	templateDAO *dao.TemplateDAO
}

func NewService(templateDAO *dao.TemplateDAO) *Service {
	return &Service{templateDAO: templateDAO}
}

type ListInput struct {
	CategoryID int
	Scene      string
	Keyword    string
	Page       int
	PageSize   int
}

func (s *Service) ListCategories(ctx context.Context) ([]*model.TemplateCategory, []int64, error) {
	categories, err := s.ListActiveCategories(ctx)
	if err != nil {
		return nil, nil, err
	}
	counts := make([]int64, len(categories))
	for i, c := range categories {
		count, _ := s.templateDAO.CountByCategory(ctx, c.ID)
		counts[i] = count
	}
	return categories, counts, nil
}

func (s *Service) ListActiveCategories(ctx context.Context) ([]*model.TemplateCategory, error) {
	return s.templateDAO.ListCategories(ctx)
}

func (s *Service) ListActiveCategoryNames(ctx context.Context, categoryIDs []int) (map[int]string, error) {
	return s.templateDAO.ListActiveCategoryNames(ctx, categoryIDs)
}

func (s *Service) ListTemplates(ctx context.Context, input ListInput) ([]*model.Template, int64, error) {
	offset := (input.Page - 1) * input.PageSize

	if input.Keyword != "" {
		return s.templateDAO.ListByKeyword(ctx, input.Keyword, offset, input.PageSize)
	}
	if input.Scene == "home" {
		return s.templateDAO.ListByScene(ctx, input.Scene, offset, input.PageSize)
	}
	return s.templateDAO.ListByCategory(ctx, input.CategoryID, offset, input.PageSize)
}

func (s *Service) ListPublishedTemplates(ctx context.Context, page, pageSize int) ([]*model.Template, int64, error) {
	offset := (page - 1) * pageSize
	return s.templateDAO.ListPublished(ctx, offset, pageSize)
}

func (s *Service) GetTemplate(ctx context.Context, templateID uint64) (*model.Template, error) {
	if templateID == 0 {
		return nil, apperr.InvalidArgument("invalid template_id")
	}
	tpl, err := s.templateDAO.GetByID(ctx, templateID)
	if err != nil {
		return nil, apperr.NotFound("template not found")
	}
	s.templateDAO.IncrementDownload(ctx, templateID)
	return tpl, nil
}

func (s *Service) FavoriteTemplate(ctx context.Context, userID, templateID uint64) (int, error) {
	if templateID == 0 {
		return 0, apperr.InvalidArgument("invalid template_id")
	}
	_, err := s.templateDAO.GetByID(ctx, templateID)
	if err != nil {
		return 0, apperr.NotFound("template not found")
	}

	existing, err := s.templateDAO.GetFavorite(ctx, userID, templateID)
	if err != nil {
		return 0, apperr.Internal("check favorite", err)
	}
	if existing != nil {
		tpl, _ := s.templateDAO.GetByID(ctx, templateID)
		return tpl.FavoriteCount, nil
	}

	fav := &model.TemplateFavorite{UserID: userID, TemplateID: templateID}
	if err := s.templateDAO.CreateFavorite(ctx, fav); err != nil {
		tpl, _ := s.templateDAO.GetByID(ctx, templateID)
		return tpl.FavoriteCount, nil
	}
	s.templateDAO.IncrementFavoriteCount(ctx, templateID)

	tpl, _ := s.templateDAO.GetByID(ctx, templateID)
	return tpl.FavoriteCount, nil
}

func (s *Service) UnfavoriteTemplate(ctx context.Context, userID, templateID uint64) (int, error) {
	if templateID == 0 {
		return 0, apperr.InvalidArgument("invalid template_id")
	}
	_, err := s.templateDAO.GetByID(ctx, templateID)
	if err != nil {
		return 0, apperr.NotFound("template not found")
	}

	existing, err := s.templateDAO.GetFavorite(ctx, userID, templateID)
	if err != nil {
		return 0, apperr.Internal("check favorite", err)
	}
	if existing == nil {
		tpl, _ := s.templateDAO.GetByID(ctx, templateID)
		return tpl.FavoriteCount, nil
	}

	s.templateDAO.DeleteFavorite(ctx, userID, templateID)
	s.templateDAO.DecrementFavoriteCount(ctx, templateID)

	tpl, _ := s.templateDAO.GetByID(ctx, templateID)
	return tpl.FavoriteCount, nil
}

func (s *Service) ListFavoriteTemplates(ctx context.Context, userID uint64, page, pageSize int) ([]*model.Template, int64, error) {
	offset := (page - 1) * pageSize
	return s.templateDAO.ListFavoriteTemplates(ctx, userID, offset, pageSize)
}

func (s *Service) BatchGetFavorited(ctx context.Context, userID uint64, templateIDs []uint64) (map[uint64]bool, error) {
	return s.templateDAO.BatchGetFavorited(ctx, userID, templateIDs)
}

func (s *Service) SplitTags(tags string) []string {
	return s.templateDAO.SplitTags(tags)
}
