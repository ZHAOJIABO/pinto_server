package dao

import (
	"context"

	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CreditDAO struct{}

func NewCreditDAO() *CreditDAO { return &CreditDAO{} }

func (d *CreditDAO) DB(ctx context.Context) *gorm.DB {
	return db.DB.WithContext(ctx)
}

func (d *CreditDAO) GetAccountForUpdate(tx *gorm.DB, userID uint64) (*model.CreditAccount, error) {
	var account model.CreditAccount
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ?", userID).First(&account).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &account, err
}

func (d *CreditDAO) CreateAccount(tx *gorm.DB, account *model.CreditAccount) error {
	return tx.Create(account).Error
}

func (d *CreditDAO) UpdateAccount(tx *gorm.DB, account *model.CreditAccount) error {
	return tx.Save(account).Error
}

func (d *CreditDAO) CreateTransactionTx(tx *gorm.DB, t *model.CreditTransaction) error {
	return tx.Create(t).Error
}

func (d *CreditDAO) Create(ctx context.Context, tx *model.CreditTransaction) error {
	return d.DB(ctx).Create(tx).Error
}

func (d *CreditDAO) GetBalance(ctx context.Context, userID uint64) (int, error) {
	var account model.CreditAccount
	err := d.DB(ctx).Where("user_id = ?", userID).First(&account).Error
	if err == gorm.ErrRecordNotFound {
		return 0, nil
	}
	return account.Balance, err
}

func (d *CreditDAO) ListByUserID(ctx context.Context, userID uint64, offset, limit int) ([]*model.CreditTransaction, int64, error) {
	var txs []*model.CreditTransaction
	var total int64
	query := d.DB(ctx).Where("user_id = ?", userID)
	query.Model(&model.CreditTransaction{}).Count(&total)
	err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&txs).Error
	return txs, total, err
}

type InviteDAO struct{}

func NewInviteDAO() *InviteDAO { return &InviteDAO{} }

func (d *InviteDAO) DB(ctx context.Context) *gorm.DB {
	return db.DB.WithContext(ctx)
}

func (d *InviteDAO) Create(ctx context.Context, invite *model.Invite) error {
	return d.DB(ctx).Create(invite).Error
}

func (d *InviteDAO) GetByInviteeID(ctx context.Context, inviteeID uint64) (*model.Invite, error) {
	var invite model.Invite
	err := d.DB(ctx).Where("invitee_id = ?", inviteeID).First(&invite).Error
	return &invite, err
}

func (d *InviteDAO) ListByInviterID(ctx context.Context, inviterID uint64, offset, limit int) ([]*model.Invite, int64, error) {
	var invites []*model.Invite
	var total int64
	query := d.DB(ctx).Where("inviter_id = ?", inviterID)
	query.Model(&model.Invite{}).Count(&total)
	err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&invites).Error
	return invites, total, err
}

func (d *InviteDAO) CountByInviterID(ctx context.Context, inviterID uint64) (int64, error) {
	var count int64
	err := d.DB(ctx).Model(&model.Invite{}).Where("inviter_id = ?", inviterID).Count(&count).Error
	return count, err
}
