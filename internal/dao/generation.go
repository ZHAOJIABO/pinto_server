package dao

import (
	"context"
	"time"

	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type GenerationDAO struct{}

func NewGenerationDAO() *GenerationDAO { return &GenerationDAO{} }

func (d *GenerationDAO) DB(ctx context.Context) *gorm.DB {
	return db.DB.WithContext(ctx)
}

func (d *GenerationDAO) Create(ctx context.Context, gen *model.Generation) error {
	return d.DB(ctx).Create(gen).Error
}

func (d *GenerationDAO) CreateTx(tx *gorm.DB, gen *model.Generation) error {
	return tx.Create(gen).Error
}

func (d *GenerationDAO) GetByGenerationID(ctx context.Context, generationID string) (*model.Generation, error) {
	var gen model.Generation
	err := d.DB(ctx).Where("generation_id = ?", generationID).First(&gen).Error
	return &gen, err
}

func (d *GenerationDAO) GetByGenerationIDForUpdate(tx *gorm.DB, generationID string) (*model.Generation, error) {
	var gen model.Generation
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("generation_id = ?", generationID).First(&gen).Error
	return &gen, err
}

func (d *GenerationDAO) GetByUserRequestIDForUpdate(tx *gorm.DB, userID uint64, clientRequestID string) (*model.Generation, error) {
	var gen model.Generation
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ? AND client_request_id = ?", userID, clientRequestID).First(&gen).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &gen, err
}

func (d *GenerationDAO) UpdateStatus(ctx context.Context, generationID string, status int8, updates map[string]interface{}) error {
	return d.DB(ctx).Model(&model.Generation{}).
		Where("generation_id = ?", generationID).
		Updates(updates).Error
}

func (d *GenerationDAO) UpdateStatusTx(tx *gorm.DB, generationID string, updates map[string]interface{}) error {
	return tx.Model(&model.Generation{}).
		Where("generation_id = ?", generationID).
		Updates(updates).Error
}

func (d *GenerationDAO) FindExpiredPending(ctx context.Context, before time.Time, limit int) ([]*model.Generation, error) {
	var gens []*model.Generation
	err := d.DB(ctx).
		Where("status = ? AND expired_at < ?", model.GenerationStatusPending, before).
		Limit(limit).
		Find(&gens).Error
	return gens, err
}

func (d *GenerationDAO) BatchUpdateStatus(ctx context.Context, ids []uint64, status int8) error {
	return d.DB(ctx).Model(&model.Generation{}).
		Where("id IN ?", ids).
		Update("status", status).Error
}

func (d *GenerationDAO) CountTodayByUser(ctx context.Context, userID uint64) (int64, error) {
	var count int64
	today := time.Now().Truncate(24 * time.Hour)
	err := d.DB(ctx).Model(&model.Generation{}).
		Where("user_id = ? AND created_at >= ? AND status != ?", userID, today, model.GenerationStatusCancelled).
		Count(&count).Error
	return count, err
}

func (d *GenerationDAO) CountTodayByUserTx(tx *gorm.DB, userID uint64) (int64, error) {
	var count int64
	today := time.Now().Truncate(24 * time.Hour)
	err := tx.Model(&model.Generation{}).
		Where("user_id = ? AND created_at >= ? AND status != ?", userID, today, model.GenerationStatusCancelled).
		Count(&count).Error
	return count, err
}
