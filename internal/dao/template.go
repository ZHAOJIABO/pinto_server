package dao

import (
	"context"

	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"gorm.io/gorm"
)

type TemplateDAO struct{}

func NewTemplateDAO() *TemplateDAO { return &TemplateDAO{} }

func (d *TemplateDAO) DB(ctx context.Context) *gorm.DB {
	return db.DB.WithContext(ctx)
}

func (d *TemplateDAO) ListCategories(ctx context.Context) ([]*model.TemplateCategory, error) {
	var categories []*model.TemplateCategory
	err := d.DB(ctx).Where("status = 1").Order("sort_order ASC").Find(&categories).Error
	return categories, err
}

func (d *TemplateDAO) ListByCategory(ctx context.Context, categoryID int, offset, limit int) ([]*model.Template, int64, error) {
	var templates []*model.Template
	var total int64
	query := d.DB(ctx).Where("category_id = ? AND status = 1", categoryID)
	query.Model(&model.Template{}).Count(&total)
	err := query.Order("sort_order ASC").Offset(offset).Limit(limit).Find(&templates).Error
	return templates, total, err
}

func (d *TemplateDAO) GetByID(ctx context.Context, id uint64) (*model.Template, error) {
	var tpl model.Template
	err := d.DB(ctx).Where("id = ? AND status = 1", id).First(&tpl).Error
	return &tpl, err
}

func (d *TemplateDAO) IncrementDownload(ctx context.Context, id uint64) error {
	return d.DB(ctx).Model(&model.Template{}).Where("id = ?", id).
		Update("download_count", gorm.Expr("download_count + 1")).Error
}
