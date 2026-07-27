package template

import (
	"context"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
)

type Service struct {
	templateDAO *dao.TemplateDAO
	blindBoxDAO *dao.BlindBoxRecordDAO
}

func NewService(templateDAO *dao.TemplateDAO, blindBoxDAO *dao.BlindBoxRecordDAO) *Service {
	return &Service{templateDAO: templateDAO, blindBoxDAO: blindBoxDAO}
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
	if input.CategoryID > 0 {
		return s.templateDAO.ListByCategory(ctx, input.CategoryID, offset, input.PageSize)
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

func (s *Service) GetRandomTemplate(ctx context.Context) (*model.Template, error) {
	tpl, err := s.templateDAO.GetRandom(ctx)
	if err != nil {
		return nil, apperr.NotFound("no templates available")
	}
	return tpl, nil
}

func (s *Service) RecordBlindBox(ctx context.Context, userID, templateID uint64) {
	record := &model.BlindBoxRecord{UserID: userID, TemplateID: templateID}
	s.blindBoxDAO.Create(ctx, record)
}

func (s *Service) ListBlindBoxRecords(ctx context.Context, userID uint64, page, pageSize int) ([]*model.Template, int64, error) {
	offset := (page - 1) * pageSize
	records, total, err := s.blindBoxDAO.ListByUserID(ctx, userID, offset, pageSize)
	if err != nil {
		return nil, 0, apperr.Internal("list blind box records", err)
	}
	if len(records) == 0 {
		return nil, 0, nil
	}
	templateIDs := make([]uint64, 0, len(records))
	for _, r := range records {
		templateIDs = append(templateIDs, r.TemplateID)
	}
	templates, err := s.templateDAO.GetByIDs(ctx, templateIDs)
	if err != nil {
		return nil, 0, apperr.Internal("get templates", err)
	}
	templateMap := make(map[uint64]*model.Template, len(templates))
	for _, t := range templates {
		templateMap[t.ID] = t
	}
	result := make([]*model.Template, 0, len(records))
	for _, r := range records {
		if t, ok := templateMap[r.TemplateID]; ok {
			result = append(result, t)
		}
	}
	return result, total, nil
}
