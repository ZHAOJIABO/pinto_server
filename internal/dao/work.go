package dao

import (
	"context"

	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"gorm.io/gorm"
)

type WorkDAO struct{}

func NewWorkDAO() *WorkDAO { return &WorkDAO{} }

func (d *WorkDAO) DB(ctx context.Context) *gorm.DB {
	return db.DB.WithContext(ctx)
}

func (d *WorkDAO) Create(ctx context.Context, work *model.Work) error {
	return d.DB(ctx).Create(work).Error
}

func (d *WorkDAO) CreateTx(tx *gorm.DB, work *model.Work) error {
	return tx.Create(work).Error
}

func (d *WorkDAO) GetByID(ctx context.Context, id uint64) (*model.Work, error) {
	var work model.Work
	err := d.DB(ctx).Where("id = ?", id).First(&work).Error
	return &work, err
}

func (d *WorkDAO) GetByIDForUser(ctx context.Context, id, userID uint64) (*model.Work, error) {
	var work model.Work
	err := d.DB(ctx).Where("id = ? AND user_id = ?", id, userID).First(&work).Error
	return &work, err
}

// listColumns omits pattern_data. That column holds a whole bead grid and can
// reach megabytes, and MySQL's filesort for "ORDER BY updated_at" buffers every
// selected column, so including it fails with error 1038 (out of sort memory)
// once a user saves a large pattern.
var listColumns = []string{
	"id", "user_id", "title", "original_image_url", "pattern_image_url", "thumbnail_url",
	"board_spec", "width", "height", "bead_count", "color_count", "source_type",
	"source_id", "status", "created_at", "updated_at",
}

func (d *WorkDAO) ListByUserID(ctx context.Context, userID uint64, status int8, offset, limit int) ([]*model.Work, int64, error) {
	var works []*model.Work
	var total int64
	query := d.DB(ctx).Where("user_id = ? AND status = ?", userID, status)
	query.Model(&model.Work{}).Count(&total)
	err := query.Select(listColumns).Order("updated_at DESC").Offset(offset).Limit(limit).Find(&works).Error
	return works, total, err
}

func (d *WorkDAO) ListByUserIDAndSource(ctx context.Context, userID uint64, status int8, sourceType string, offset, limit int) ([]*model.Work, int64, error) {
	var works []*model.Work
	var total int64
	query := d.DB(ctx).Where("user_id = ? AND status = ? AND source_type = ?", userID, status, sourceType)
	query.Model(&model.Work{}).Count(&total)
	err := query.Select(listColumns).Order("updated_at DESC").Offset(offset).Limit(limit).Find(&works).Error
	return works, total, err
}

func (d *WorkDAO) Update(ctx context.Context, work *model.Work) error {
	return d.DB(ctx).Save(work).Error
}

func (d *WorkDAO) Delete(ctx context.Context, id uint64, userID uint64) error {
	return d.DB(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&model.Work{}).Error
}
