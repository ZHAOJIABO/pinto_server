package dao

import (
	"context"

	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"gorm.io/gorm"
)

type BlindBoxRecordDAO struct{}

func NewBlindBoxRecordDAO() *BlindBoxRecordDAO { return &BlindBoxRecordDAO{} }

func (d *BlindBoxRecordDAO) DB(ctx context.Context) *gorm.DB {
	return db.DB.WithContext(ctx)
}

func (d *BlindBoxRecordDAO) Create(ctx context.Context, record *model.BlindBoxRecord) error {
	return d.DB(ctx).Create(record).Error
}

func (d *BlindBoxRecordDAO) ListByUserID(ctx context.Context, userID uint64, offset, limit int) ([]*model.BlindBoxRecord, int64, error) {
	var records []*model.BlindBoxRecord
	var total int64
	query := d.DB(ctx).Where("user_id = ?", userID)
	query.Model(&model.BlindBoxRecord{}).Count(&total)
	err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&records).Error
	return records, total, err
}
