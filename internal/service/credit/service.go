package credit

import (
	"context"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"gorm.io/gorm"
)

type Service struct {
	creditDAO *dao.CreditDAO
}

func NewService(creditDAO *dao.CreditDAO) *Service {
	return &Service{creditDAO: creditDAO}
}

func (s *Service) GetBalance(ctx context.Context, userID uint64) (int, error) {
	return s.creditDAO.GetBalance(ctx, userID)
}

func (s *Service) GetAccountForUpdate(tx *gorm.DB, userID uint64) (*model.CreditAccount, error) {
	account, err := s.creditDAO.GetAccountForUpdate(tx, userID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		account = &model.CreditAccount{UserID: userID, Balance: 0}
		if err := s.creditDAO.CreateAccount(tx, account); err != nil {
			return nil, err
		}
	}
	return account, nil
}

func (s *Service) DeductCreditsTx(tx *gorm.DB, userID uint64, amount int, txType, refType, refID, desc string) (int, error) {
	account, err := s.GetAccountForUpdate(tx, userID)
	if err != nil {
		return 0, err
	}
	if account.Balance < amount {
		return 0, apperr.InsufficientCredits(account.Balance, amount)
	}
	account.Balance -= amount
	if err := s.creditDAO.UpdateAccount(tx, account); err != nil {
		return 0, err
	}
	t := &model.CreditTransaction{
		UserID:      userID,
		Amount:      -amount,
		Balance:     account.Balance,
		Type:        txType,
		RefType:     refType,
		RefID:       refID,
		Description: desc,
	}
	if err := s.creditDAO.CreateTransactionTx(tx, t); err != nil {
		return 0, err
	}
	return account.Balance, nil
}

func (s *Service) AddCreditsTx(tx *gorm.DB, userID uint64, amount int, txType, refType, refID, desc string) (int, error) {
	account, err := s.GetAccountForUpdate(tx, userID)
	if err != nil {
		return 0, err
	}
	account.Balance += amount
	if err := s.creditDAO.UpdateAccount(tx, account); err != nil {
		return 0, err
	}
	t := &model.CreditTransaction{
		UserID:      userID,
		Amount:      amount,
		Balance:     account.Balance,
		Type:        txType,
		RefType:     refType,
		RefID:       refID,
		Description: desc,
	}
	if err := s.creditDAO.CreateTransactionTx(tx, t); err != nil {
		return 0, err
	}
	return account.Balance, nil
}

func (s *Service) AddCredits(ctx context.Context, userID uint64, amount int, txType, refType, refID, desc string) error {
	if amount <= 0 {
		return apperr.InvalidArgument("amount must be positive")
	}
	return db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, err := s.AddCreditsTx(tx, userID, amount, txType, refType, refID, desc)
		return err
	})
}

func (s *Service) DeductCredits(ctx context.Context, userID uint64, amount int, txType, refType, refID, desc string) error {
	if amount <= 0 {
		return apperr.InvalidArgument("amount must be positive")
	}
	return db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, err := s.DeductCreditsTx(tx, userID, amount, txType, refType, refID, desc)
		return err
	})
}

func (s *Service) InitAccount(ctx context.Context, tx *gorm.DB, userID uint64) error {
	account := &model.CreditAccount{UserID: userID, Balance: 0}
	return s.creditDAO.CreateAccount(tx, account)
}

func (s *Service) ListTransactions(ctx context.Context, userID uint64, page, pageSize int) ([]*model.CreditTransaction, int64, error) {
	offset := (page - 1) * pageSize
	return s.creditDAO.ListByUserID(ctx, userID, offset, pageSize)
}
