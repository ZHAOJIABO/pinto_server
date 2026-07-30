package user

import (
	"context"
	"errors"
	"strconv"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"gorm.io/gorm"
)

type Service struct {
	userDAO *dao.UserDAO
}

func NewService(userDAO *dao.UserDAO) *Service {
	return &Service{userDAO: userDAO}
}

func (s *Service) GetUserInfo(ctx context.Context, userID uint64) (*model.User, error) {
	return s.userDAO.GetByID(ctx, userID)
}

func (s *Service) GetUserByPublicID(ctx context.Context, publicUserID string) (*model.User, error) {
	user, err := s.userDAO.GetByPublicUserID(ctx, publicUserID)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return user, err
	}

	internalUserID, parseErr := strconv.ParseUint(publicUserID, 10, 64)
	if parseErr != nil {
		return nil, err
	}
	return s.userDAO.GetByID(ctx, internalUserID)
}

func (s *Service) UpdateUserInfo(ctx context.Context, userID uint64, nickname, avatarURL string) error {
	user, err := s.userDAO.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if nickname != "" {
		user.Nickname = nickname
	}
	if avatarURL != "" {
		user.AvatarURL = avatarURL
	}
	return s.userDAO.Update(ctx, user)
}

func (s *Service) BindPhone(ctx context.Context, userID uint64, phone, code string) error {
	// TODO: verify SMS code
	user, err := s.userDAO.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	user.Phone = phone
	return s.userDAO.Update(ctx, user)
}

func (s *Service) DeleteAccount(ctx context.Context, userID uint64) error {
	return s.userDAO.UpdateStatus(ctx, userID, 3)
}
