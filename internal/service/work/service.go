package work

import (
	"context"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	"gorm.io/gorm"
)

type Service struct {
	workDAO *dao.WorkDAO
}

func NewService(workDAO *dao.WorkDAO) *Service {
	return &Service{workDAO: workDAO}
}

func (s *Service) SaveWork(ctx context.Context, userID uint64, work *model.Work, patternData *pb.PatternData) (uint64, error) {
	if err := ValidatePatternData(patternData); err != nil {
		return 0, err
	}
	work.PatternData = PatternDataToJSONMap(patternData)
	work.Width = int(patternData.Width)
	work.Height = int(patternData.Height)
	if patternData.BoardSpec != "" {
		work.BoardSpec = patternData.BoardSpec
	}

	work.UserID = userID
	work.Status = 2
	if err := s.workDAO.Create(ctx, work); err != nil {
		return 0, apperr.Internal("save work", err)
	}
	return work.ID, nil
}

func (s *Service) CreateWorkTx(tx *gorm.DB, work *model.Work) error {
	return s.workDAO.CreateTx(tx, work)
}

func (s *Service) GetWork(ctx context.Context, userID, workID uint64) (*model.Work, error) {
	w, err := s.workDAO.GetByIDForUser(ctx, workID, userID)
	if err != nil {
		return nil, apperr.NotFound("work not found")
	}
	return w, nil
}

func (s *Service) ListWorks(ctx context.Context, userID uint64, page, pageSize int) ([]*model.Work, int64, error) {
	offset := (page - 1) * pageSize
	return s.workDAO.ListByUserID(ctx, userID, 2, offset, pageSize)
}

func (s *Service) DeleteWork(ctx context.Context, userID, workID uint64) error {
	return s.workDAO.Delete(ctx, workID, userID)
}

func (s *Service) SaveDraft(ctx context.Context, userID uint64, work *model.Work, patternData *pb.PatternData) (uint64, error) {
	if patternData != nil {
		if err := ValidatePatternData(patternData); err != nil {
			return 0, err
		}
		work.PatternData = PatternDataToJSONMap(patternData)
		work.Width = int(patternData.Width)
		work.Height = int(patternData.Height)
		if patternData.BoardSpec != "" {
			work.BoardSpec = patternData.BoardSpec
		}
	}

	work.UserID = userID
	work.Status = 1

	if work.ID > 0 {
		existing, err := s.workDAO.GetByIDForUser(ctx, work.ID, userID)
		if err != nil {
			return 0, apperr.Forbidden("draft not found or not owned by user")
		}
		work.CreatedAt = existing.CreatedAt
		return work.ID, s.workDAO.Update(ctx, work)
	}

	if err := s.workDAO.Create(ctx, work); err != nil {
		return 0, apperr.Internal("save draft", err)
	}
	return work.ID, nil
}

func (s *Service) ListDrafts(ctx context.Context, userID uint64, page, pageSize int) ([]*model.Work, int64, error) {
	offset := (page - 1) * pageSize
	return s.workDAO.ListByUserID(ctx, userID, 1, offset, pageSize)
}
