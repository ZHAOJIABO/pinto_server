package invite

import (
	"context"
	"math/rand"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
)

type Service struct {
	inviteDAO *dao.InviteDAO
}

func NewService(inviteDAO *dao.InviteDAO) *Service {
	return &Service{inviteDAO: inviteDAO}
}

func (s *Service) GetOrCreateInviteCode(ctx context.Context, userID uint64) string {
	// TODO: store/retrieve invite code from user profile or dedicated table
	return generateCode()
}

func (s *Service) BindInviteCode(ctx context.Context, inviteeID uint64, code string) (int, error) {
	// TODO: look up inviter by code, create record, grant rewards
	invite := &model.Invite{
		InviterID:  0, // TODO: resolve from code
		InviteeID:  inviteeID,
		InviteCode: code,
	}
	if err := s.inviteDAO.Create(ctx, invite); err != nil {
		return 0, err
	}
	return 5, nil // reward credits
}

func (s *Service) GetStats(ctx context.Context, userID uint64) (int64, int64, error) {
	count, err := s.inviteDAO.CountByInviterID(ctx, userID)
	if err != nil {
		return 0, 0, err
	}
	return count, count * 5, nil
}

func (s *Service) ListRecords(ctx context.Context, userID uint64, page, pageSize int) ([]*model.Invite, int64, error) {
	offset := (page - 1) * pageSize
	return s.inviteDAO.ListByInviterID(ctx, userID, offset, pageSize)
}

func generateCode() string {
	const chars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	code := make([]byte, 6)
	for i := range code {
		code[i] = chars[rand.Intn(len(chars))]
	}
	return string(code)
}
