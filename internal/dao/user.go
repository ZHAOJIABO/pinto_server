package dao

import (
	"context"

	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"gorm.io/gorm"
)

type UserDAO struct{}

func NewUserDAO() *UserDAO { return &UserDAO{} }

func (d *UserDAO) DB(ctx context.Context) *gorm.DB {
	return db.DB.WithContext(ctx)
}

func (d *UserDAO) Create(ctx context.Context, user *model.User) error {
	return d.DB(ctx).Create(user).Error
}

func (d *UserDAO) GetByID(ctx context.Context, id uint64) (*model.User, error) {
	var user model.User
	err := d.DB(ctx).Where("id = ? AND status = 1", id).First(&user).Error
	return &user, err
}

func (d *UserDAO) GetByUUID(ctx context.Context, uuid string) (*model.User, error) {
	var user model.User
	err := d.DB(ctx).Where("uuid = ? AND status = 1", uuid).First(&user).Error
	return &user, err
}

func (d *UserDAO) GetByPhone(ctx context.Context, phone string) (*model.User, error) {
	var user model.User
	err := d.DB(ctx).Where("phone = ? AND status = 1", phone).First(&user).Error
	return &user, err
}

func (d *UserDAO) GetByDeviceIDAndType(ctx context.Context, deviceID, loginType string) (*model.User, error) {
	var user model.User
	err := d.DB(ctx).Where("device_id = ? AND login_type = ? AND status = 1", deviceID, loginType).First(&user).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &user, err
}

func (d *UserDAO) GetByWechatUnionID(ctx context.Context, unionID string) (*model.User, error) {
	var user model.User
	err := d.DB(ctx).Where("wechat_union_id = ? AND status = 1", unionID).First(&user).Error
	return &user, err
}

func (d *UserDAO) GetByAppleID(ctx context.Context, appleID string) (*model.User, error) {
	var user model.User
	err := d.DB(ctx).Where("apple_id = ? AND status = 1", appleID).First(&user).Error
	return &user, err
}

func (d *UserDAO) Update(ctx context.Context, user *model.User) error {
	return d.DB(ctx).Save(user).Error
}

func (d *UserDAO) UpdateStatus(ctx context.Context, id uint64, status int8) error {
	return d.DB(ctx).Model(&model.User{}).Where("id = ?", id).Update("status", status).Error
}
