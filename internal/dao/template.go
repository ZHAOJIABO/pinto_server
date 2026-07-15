package dao

import (
	"context"
	"fmt"
	"strings"

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

func (d *TemplateDAO) CountByCategory(ctx context.Context, categoryID int) (int64, error) {
	var count int64
	err := d.DB(ctx).Model(&model.Template{}).Where("category_id = ? AND status = 1", categoryID).Count(&count).Error
	return count, err
}

func (d *TemplateDAO) ListByCategory(ctx context.Context, categoryID int, offset, limit int) ([]*model.Template, int64, error) {
	var templates []*model.Template
	var total int64
	query := d.DB(ctx).Where("category_id = ? AND status = 1", categoryID)
	query.Model(&model.Template{}).Count(&total)
	err := query.Order("sort_order ASC, created_at DESC").Offset(offset).Limit(limit).Find(&templates).Error
	return templates, total, err
}

func (d *TemplateDAO) ListByScene(ctx context.Context, scene string, offset, limit int) ([]*model.Template, int64, error) {
	var templates []*model.Template
	var total int64
	query := d.DB(ctx).Where("status = 1")
	query.Model(&model.Template{}).Count(&total)
	err := query.Order("sort_order ASC, created_at DESC").Offset(offset).Limit(limit).Find(&templates).Error
	return templates, total, err
}

func (d *TemplateDAO) ListByKeyword(ctx context.Context, keyword string, offset, limit int) ([]*model.Template, int64, error) {
	var templates []*model.Template
	var total int64
	like := fmt.Sprintf("%%%s%%", keyword)
	query := d.DB(ctx).Where("status = 1 AND (title LIKE ? OR tags LIKE ?)", like, like)
	query.Model(&model.Template{}).Count(&total)
	err := query.Order("sort_order ASC, created_at DESC").Offset(offset).Limit(limit).Find(&templates).Error
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

// Favorite methods

func (d *TemplateDAO) CreateFavorite(ctx context.Context, fav *model.TemplateFavorite) error {
	return d.DB(ctx).Create(fav).Error
}

func (d *TemplateDAO) DeleteFavorite(ctx context.Context, userID, templateID uint64) error {
	return d.DB(ctx).Where("user_id = ? AND template_id = ?", userID, templateID).
		Delete(&model.TemplateFavorite{}).Error
}

func (d *TemplateDAO) GetFavorite(ctx context.Context, userID, templateID uint64) (*model.TemplateFavorite, error) {
	var fav model.TemplateFavorite
	err := d.DB(ctx).Where("user_id = ? AND template_id = ?", userID, templateID).First(&fav).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &fav, err
}

func (d *TemplateDAO) BatchGetFavorited(ctx context.Context, userID uint64, templateIDs []uint64) (map[uint64]bool, error) {
	result := make(map[uint64]bool)
	if len(templateIDs) == 0 {
		return result, nil
	}
	var favs []*model.TemplateFavorite
	err := d.DB(ctx).Where("user_id = ? AND template_id IN ?", userID, templateIDs).Find(&favs).Error
	if err != nil {
		return nil, err
	}
	for _, f := range favs {
		result[f.TemplateID] = true
	}
	return result, nil
}

func (d *TemplateDAO) IncrementFavoriteCount(ctx context.Context, templateID uint64) error {
	return d.DB(ctx).Model(&model.Template{}).Where("id = ?", templateID).
		Update("favorite_count", gorm.Expr("favorite_count + 1")).Error
}

func (d *TemplateDAO) DecrementFavoriteCount(ctx context.Context, templateID uint64) error {
	return d.DB(ctx).Model(&model.Template{}).Where("id = ? AND favorite_count > 0", templateID).
		Update("favorite_count", gorm.Expr("favorite_count - 1")).Error
}

func (d *TemplateDAO) ListFavoriteTemplates(ctx context.Context, userID uint64, offset, limit int) ([]*model.Template, int64, error) {
	var total int64
	d.DB(ctx).Model(&model.TemplateFavorite{}).Where("user_id = ?", userID).Count(&total)

	var favs []*model.TemplateFavorite
	err := d.DB(ctx).Where("user_id = ?", userID).
		Order("created_at DESC").Offset(offset).Limit(limit).Find(&favs).Error
	if err != nil {
		return nil, 0, err
	}
	if len(favs) == 0 {
		return []*model.Template{}, total, nil
	}

	templateIDs := make([]uint64, 0, len(favs))
	for _, f := range favs {
		templateIDs = append(templateIDs, f.TemplateID)
	}

	var templates []*model.Template
	err = d.DB(ctx).Where("id IN ?", templateIDs).Find(&templates).Error
	if err != nil {
		return nil, 0, err
	}

	tplMap := make(map[uint64]*model.Template)
	for _, t := range templates {
		tplMap[t.ID] = t
	}

	ordered := make([]*model.Template, 0, len(favs))
	for _, f := range favs {
		if t, ok := tplMap[f.TemplateID]; ok {
			ordered = append(ordered, t)
		}
	}

	return ordered, total, nil
}

func (d *TemplateDAO) SplitTags(tags string) []string {
	if tags == "" {
		return nil
	}
	parts := strings.Split(tags, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// Admin publishing methods

func (d *TemplateDAO) CreateOrUpdateTemplate(ctx context.Context, tpl *model.Template) (uint64, error) {
	if err := d.DB(ctx).Create(tpl).Error; err != nil {
		return 0, err
	}
	return tpl.ID, nil
}

func (d *TemplateDAO) SetTemplateStatus(ctx context.Context, templateID uint64, status int8) error {
	return d.DB(ctx).Model(&model.Template{}).Where("id = ?", templateID).
		Update("status", status).Error
}

func (d *TemplateDAO) CreatePublishRecord(ctx context.Context, record *model.TemplatePublishRecord) error {
	return d.DB(ctx).Create(record).Error
}

func (d *TemplateDAO) GetPublishRecordByKey(ctx context.Context, key string) (*model.TemplatePublishRecord, error) {
	var record model.TemplatePublishRecord
	err := d.DB(ctx).Where("idempotency_key = ?", key).First(&record).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &record, err
}

func (d *TemplateDAO) GetTemplateByIDIncludeInactive(ctx context.Context, id uint64) (*model.Template, error) {
	var tpl model.Template
	err := d.DB(ctx).Where("id = ?", id).First(&tpl).Error
	return &tpl, err
}
