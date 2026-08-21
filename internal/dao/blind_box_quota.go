package dao

import (
	"context"
	"errors"

	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type BlindBoxQuotaDAO struct{}

func NewBlindBoxQuotaDAO() *BlindBoxQuotaDAO { return &BlindBoxQuotaDAO{} }

func (d *BlindBoxQuotaDAO) DB(ctx context.Context) *gorm.DB {
	return db.DB.WithContext(ctx)
}

// ConsumeTx 原子地占用一次当日额度，返回是否占到。
//
// 判重条件 used_count < limit 写在 UPDATE 的 WHERE 里，而不是先 SELECT 出来再在 Go 里
// 比较：先读后写的话，两个并发请求会同时读到 "今天 0 次" 然后都放行。交给数据库串行化，
// RowsAffected == 0 就是额度用尽。
//
// 先用 ON CONFLICT DO NOTHING 建当日行是因为 UPDATE 需要行存在。并发下多个请求都插会撞
// (user_id, draw_date) 唯一索引，DoNothing 把它变成无害的空操作。
func (d *BlindBoxQuotaDAO) ConsumeTx(tx *gorm.DB, userID uint64, drawDate string, limit int) (bool, error) {
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
		Create(&model.BlindBoxDailyQuota{UserID: userID, DrawDate: drawDate}).Error; err != nil {
		return false, err
	}

	result := tx.Model(&model.BlindBoxDailyQuota{}).
		Where("user_id = ? AND draw_date = ? AND used_count < ?", userID, drawDate, limit).
		UpdateColumn("used_count", gorm.Expr("used_count + 1"))
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// GetUsedCount 返回当日已用次数；当日还没有行时返回 0。
func (d *BlindBoxQuotaDAO) GetUsedCount(ctx context.Context, userID uint64, drawDate string) (int, error) {
	var row model.BlindBoxDailyQuota
	err := d.DB(ctx).Where("user_id = ? AND draw_date = ?", userID, drawDate).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return row.UsedCount, nil
}
