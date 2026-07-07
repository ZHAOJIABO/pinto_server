package dao

import (
	"context"

	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"gorm.io/gorm"
)

type OrderDAO struct{}

func NewOrderDAO() *OrderDAO { return &OrderDAO{} }

func (d *OrderDAO) DB(ctx context.Context) *gorm.DB {
	return db.DB.WithContext(ctx)
}

func (d *OrderDAO) Create(ctx context.Context, order *model.Order) error {
	return d.DB(ctx).Create(order).Error
}

func (d *OrderDAO) GetByOrderNo(ctx context.Context, orderNo string) (*model.Order, error) {
	var order model.Order
	err := d.DB(ctx).Where("order_no = ?", orderNo).First(&order).Error
	return &order, err
}

func (d *OrderDAO) Update(ctx context.Context, order *model.Order) error {
	return d.DB(ctx).Save(order).Error
}

func (d *OrderDAO) ListByUserID(ctx context.Context, userID uint64, offset, limit int) ([]*model.Order, int64, error) {
	var orders []*model.Order
	var total int64
	query := d.DB(ctx).Where("user_id = ?", userID)
	query.Model(&model.Order{}).Count(&total)
	err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&orders).Error
	return orders, total, err
}

type ProductDAO struct{}

func NewProductDAO() *ProductDAO { return &ProductDAO{} }

func (d *ProductDAO) DB(ctx context.Context) *gorm.DB {
	return db.DB.WithContext(ctx)
}

func (d *ProductDAO) List(ctx context.Context, platform string) ([]*model.Product, error) {
	var products []*model.Product
	query := d.DB(ctx).Where("status = 1")
	if platform != "" {
		query = query.Where("platform = '' OR platform = ?", platform)
	}
	err := query.Order("sort_order ASC").Find(&products).Error
	return products, err
}

func (d *ProductDAO) GetByID(ctx context.Context, id int) (*model.Product, error) {
	var product model.Product
	err := d.DB(ctx).Where("id = ? AND status = 1", id).First(&product).Error
	return &product, err
}

type SubscriptionDAO struct{}

func NewSubscriptionDAO() *SubscriptionDAO { return &SubscriptionDAO{} }

func (d *SubscriptionDAO) DB(ctx context.Context) *gorm.DB {
	return db.DB.WithContext(ctx)
}

func (d *SubscriptionDAO) Create(ctx context.Context, sub *model.Subscription) error {
	return d.DB(ctx).Create(sub).Error
}

func (d *SubscriptionDAO) GetActiveByUserID(ctx context.Context, userID uint64) (*model.Subscription, error) {
	var sub model.Subscription
	err := d.DB(ctx).Where("user_id = ? AND status = 1 AND expire_at > NOW()", userID).
		Order("expire_at DESC").First(&sub).Error
	return &sub, err
}
