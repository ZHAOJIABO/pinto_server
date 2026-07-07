package dao

import (
	"context"

	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"gorm.io/gorm"
)

type SystemDAO struct{}

func NewSystemDAO() *SystemDAO { return &SystemDAO{} }

func (d *SystemDAO) DB(ctx context.Context) *gorm.DB {
	return db.DB.WithContext(ctx)
}

func (d *SystemDAO) GetConfig(ctx context.Context, key string) (string, error) {
	var cfg model.Config
	err := d.DB(ctx).Where("config_key = ?", key).First(&cfg).Error
	if err == gorm.ErrRecordNotFound {
		return "", nil
	}
	return cfg.ConfigValue, err
}

func (d *SystemDAO) GetAllConfigs(ctx context.Context) (map[string]string, error) {
	var configs []model.Config
	err := d.DB(ctx).Find(&configs).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(configs))
	for _, c := range configs {
		result[c.ConfigKey] = c.ConfigValue
	}
	return result, nil
}

func (d *SystemDAO) ListBeadColors(ctx context.Context, brand string) ([]*model.BeadColor, error) {
	var colors []*model.BeadColor
	query := d.DB(ctx).Where("status = 1")
	if brand != "" {
		query = query.Where("brand = ?", brand)
	}
	err := query.Order("brand ASC, code ASC").Find(&colors).Error
	return colors, err
}

func (d *SystemDAO) ListBoardSpecs(ctx context.Context) ([]*model.BoardSpec, error) {
	var specs []*model.BoardSpec
	err := d.DB(ctx).Where("status = 1").Find(&specs).Error
	return specs, err
}

func (d *SystemDAO) CreateFeedback(ctx context.Context, fb *model.Feedback) error {
	return d.DB(ctx).Create(fb).Error
}
