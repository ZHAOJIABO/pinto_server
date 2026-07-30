package dao

import (
	"context"
	"errors"

	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrGuestUserIDCollision = errors.New("guest user id collision")

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

func (d *UserDAO) GetByPublicUserID(ctx context.Context, userID string) (*model.User, error) {
	var user model.User
	err := d.DB(ctx).Where("user_id = ? AND status = 1", userID).First(&user).Error
	return &user, err
}

func (d *UserDAO) GetByPhone(ctx context.Context, phone string) (*model.User, error) {
	var user model.User
	err := d.DB(ctx).Where("phone = ? AND status = 1", phone).First(&user).Error
	return &user, err
}

func (d *UserDAO) GetByGuestIdentity(ctx context.Context, guestIdentity string) (*model.User, error) {
	var user model.User
	err := d.DB(ctx).Where("guest_identity = ? AND status = 1", guestIdentity).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &user, err
}

// CreateOrGetGuest atomically creates a guest user or returns the user created
// by a concurrent login from the same device. A conflicting public ID for a
// different device is surfaced instead of merging the two identities.
func (d *UserDAO) CreateOrGetGuest(ctx context.Context, user *model.User) (*model.User, error) {
	if user.GuestIdentity == nil || user.UserID == nil {
		return nil, errors.New("guest identity and user id are required")
	}

	result := d.DB(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(user)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 1 {
		return user, nil
	}

	existing, err := d.GetByGuestIdentity(ctx, *user.GuestIdentity)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, ErrGuestUserIDCollision
	}
	return existing, nil
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
