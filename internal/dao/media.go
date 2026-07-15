package dao

import (
	"context"
	"time"

	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"gorm.io/gorm"
)

type MediaDAO struct{}

func NewMediaDAO() *MediaDAO { return &MediaDAO{} }

func (d *MediaDAO) DB(ctx context.Context) *gorm.DB {
	return db.DB.WithContext(ctx)
}

func (d *MediaDAO) Create(ctx context.Context, asset *model.MediaAsset) error {
	return d.DB(ctx).Create(asset).Error
}

func (d *MediaDAO) GetByFileKey(ctx context.Context, fileKey string) (*model.MediaAsset, error) {
	var asset model.MediaAsset
	err := d.DB(ctx).Where("file_key = ?", fileKey).First(&asset).Error
	return &asset, err
}

func (d *MediaDAO) GetByFileKeyAndUser(ctx context.Context, fileKey string, userID uint64) (*model.MediaAsset, error) {
	var asset model.MediaAsset
	err := d.DB(ctx).Where("file_key = ? AND user_id = ?", fileKey, userID).First(&asset).Error
	return &asset, err
}

func (d *MediaDAO) MarkUploaded(ctx context.Context, fileKey string, fileSize int64) error {
	now := time.Now()
	return d.DB(ctx).Model(&model.MediaAsset{}).
		Where("file_key = ?", fileKey).
		Updates(map[string]interface{}{
			"status":      model.MediaStatusUploaded,
			"file_size":   fileSize,
			"uploaded_at": &now,
		}).Error
}

func (d *MediaDAO) GetUploadedAsset(ctx context.Context, fileKey string, userID uint64, purpose string) (*model.MediaAsset, error) {
	var asset model.MediaAsset
	err := d.DB(ctx).Where("file_key = ? AND user_id = ? AND purpose = ? AND status = ?",
		fileKey, userID, purpose, model.MediaStatusUploaded).First(&asset).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &asset, err
}
