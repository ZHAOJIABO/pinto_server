package dao

import (
	"context"

	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"gorm.io/gorm"
)

type BlindBoxPoolDAO struct{}

func NewBlindBoxPoolDAO() *BlindBoxPoolDAO { return &BlindBoxPoolDAO{} }

func (d *BlindBoxPoolDAO) DB(ctx context.Context) *gorm.DB {
	return db.DB.WithContext(ctx)
}

// PoolCandidate 只带抽奖需要的两列。奖池规模是几十到几百条，把整个候选集拉进内存
// 做加权抽取远比在 SQL 里排序便宜，也避开了 ORDER BY RAND() 的 MySQL 依赖——测试
// 跑在 SQLite 上，那里没有 RAND()。更重要的是这样完全不碰 pattern_data 大列。
type PoolCandidate struct {
	TemplateID uint64 `gorm:"column:template_id"`
	Weight     int    `gorm:"column:weight"`
}

// ListActiveCandidates 返回当前可被抽中的奖池条目。
//
// JOIN bb_template 是脏数据兜底：图纸被下架但忘了移出奖池时不能被抽中。
// weight > 0 在 SQL 层过滤，于是"权重全为 0"天然退化成空池而不是除零。
func (d *BlindBoxPoolDAO) ListActiveCandidates(ctx context.Context) ([]PoolCandidate, error) {
	var rows []PoolCandidate
	err := d.DB(ctx).Table("bb_blind_box_pool AS p").
		Select("p.template_id, p.weight").
		Joins("JOIN bb_template AS t ON t.id = p.template_id AND t.status IN (1,2)").
		Where("p.status = 1 AND p.weight > 0").
		Order("p.id ASC").
		Scan(&rows).Error
	return rows, err
}

func (d *BlindBoxPoolDAO) List(ctx context.Context, offset, limit int) ([]*model.BlindBoxPoolItem, int64, error) {
	var items []*model.BlindBoxPoolItem
	var total int64
	if err := d.DB(ctx).Model(&model.BlindBoxPoolItem{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := d.DB(ctx).Order("sort_order ASC, id DESC").Offset(offset).Limit(limit).Find(&items).Error
	return items, total, err
}

func (d *BlindBoxPoolDAO) GetByID(ctx context.Context, id uint64) (*model.BlindBoxPoolItem, error) {
	var item model.BlindBoxPoolItem
	err := d.DB(ctx).Where("id = ?", id).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (d *BlindBoxPoolDAO) GetByTemplateID(ctx context.Context, templateID uint64) (*model.BlindBoxPoolItem, error) {
	var item model.BlindBoxPoolItem
	err := d.DB(ctx).Where("template_id = ?", templateID).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (d *BlindBoxPoolDAO) GetByTemplateIDTx(tx *gorm.DB, templateID uint64) (*model.BlindBoxPoolItem, error) {
	var item model.BlindBoxPoolItem
	err := tx.Where("template_id = ?", templateID).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (d *BlindBoxPoolDAO) GetByIDTx(tx *gorm.DB, id uint64) (*model.BlindBoxPoolItem, error) {
	var item model.BlindBoxPoolItem
	err := tx.Where("id = ?", id).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (d *BlindBoxPoolDAO) CreateTx(tx *gorm.DB, item *model.BlindBoxPoolItem) error {
	return tx.Create(item).Error
}

func (d *BlindBoxPoolDAO) Update(ctx context.Context, id uint64, fields map[string]interface{}) (bool, error) {
	result := d.DB(ctx).Model(&model.BlindBoxPoolItem{}).Where("id = ?", id).Updates(fields)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (d *BlindBoxPoolDAO) DeleteTx(tx *gorm.DB, id uint64) error {
	return tx.Where("id = ?", id).Delete(&model.BlindBoxPoolItem{}).Error
}
