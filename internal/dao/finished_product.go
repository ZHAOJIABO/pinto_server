package dao

import (
	"context"
	stderrors "errors"

	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"gorm.io/gorm"
)

type FinishedProductDAO struct{}

func NewFinishedProductDAO() *FinishedProductDAO { return &FinishedProductDAO{} }

func (d *FinishedProductDAO) DB(ctx context.Context) *gorm.DB {
	return db.DB.WithContext(ctx)
}

func (d *FinishedProductDAO) Create(ctx context.Context, fp *model.FinishedProduct) error {
	return d.DB(ctx).Create(fp).Error
}

func (d *FinishedProductDAO) GetByClientRequestID(ctx context.Context, userID uint64, clientRequestID string) (*model.FinishedProduct, error) {
	return d.first(ctx, "user_id = ? AND client_request_id = ?", userID, clientRequestID)
}

func (d *FinishedProductDAO) GetByMediaFileKey(ctx context.Context, userID uint64, mediaFileKey string) (*model.FinishedProduct, error) {
	return d.first(ctx, "user_id = ? AND media_file_key = ?", userID, mediaFileKey)
}

// first 在未命中时返回 (nil, nil)，与 MediaDAO.GetUploadedAsset 的约定一致。
func (d *FinishedProductDAO) first(ctx context.Context, query string, args ...interface{}) (*model.FinishedProduct, error) {
	var fp model.FinishedProduct
	err := d.DB(ctx).Where(query, args...).First(&fp).Error
	if stderrors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &fp, nil
}

// ListByUser 按 id 倒序返回；beforeID 为 0 时从最新一条开始。
func (d *FinishedProductDAO) ListByUser(ctx context.Context, userID, beforeID uint64, limit int) ([]*model.FinishedProduct, error) {
	var items []*model.FinishedProduct
	query := d.DB(ctx).Where("user_id = ?", userID)
	if beforeID > 0 {
		query = query.Where("id < ?", beforeID)
	}
	err := query.Order("id DESC").Limit(limit).Find(&items).Error
	return items, err
}
