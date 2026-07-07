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

func (d *WorkDAO) ListByUserID(ctx context.Context, userID uint64, status int8, offset, limit int) ([]*model.Work, int64, error) {
	var works []*model.Work
	var total int64
	query := d.DB(ctx).Where("user_id = ? AND status = ?", userID, status)
	query.Model(&model.Work{}).Count(&total)
	err := query.Order("updated_at DESC").Offset(offset).Limit(limit).Find(&works).Error
	return works, total, err
}

func (d *WorkDAO) Update(ctx context.Context, work *model.Work) error {
	return d.DB(ctx).Save(work).Error
}

func (d *WorkDAO) Delete(ctx context.Context, id uint64, userID uint64) error {
	return d.DB(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&model.Work{}).Error
}
