package template

import (
	"context"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
)

type Service struct {
	templateDAO *dao.TemplateDAO
}

func NewService(templateDAO *dao.TemplateDAO) *Service {
	return &Service{templateDAO: templateDAO}
}

func (s *Service) ListCategories(ctx context.Context) ([]*model.TemplateCategory, error) {
	return s.templateDAO.ListCategories(ctx)
}

func (s *Service) ListTemplates(ctx context.Context, categoryID int, page, pageSize int) ([]*model.Template, int64, error) {
	offset := (page - 1) * pageSize
	return s.templateDAO.ListByCategory(ctx, categoryID, offset, pageSize)
}

func (s *Service) GetTemplate(ctx context.Context, templateID uint64) (*model.Template, error) {
	tpl, err := s.templateDAO.GetByID(ctx, templateID)
	if err != nil {
		return nil, err
	}
	s.templateDAO.IncrementDownload(ctx, templateID)
	return tpl, nil
}
