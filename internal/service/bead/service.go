package bead

import (
	"context"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
)

type Service struct {
	systemDAO *dao.SystemDAO
}

func NewService(systemDAO *dao.SystemDAO) *Service {
	return &Service{systemDAO: systemDAO}
}

func (s *Service) GetBeadColors(ctx context.Context, brand string) ([]*model.BeadColor, error) {
	return s.systemDAO.ListBeadColors(ctx, brand)
}

func (s *Service) GetBoardSpecs(ctx context.Context) ([]*model.BoardSpec, error) {
	return s.systemDAO.ListBoardSpecs(ctx)
}
