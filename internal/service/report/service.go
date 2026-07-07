package report

import (
	"context"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"go.uber.org/zap"
)

type Service struct {
	systemDAO *dao.SystemDAO
}

func NewService(systemDAO *dao.SystemDAO) *Service {
	return &Service{systemDAO: systemDAO}
}

func (s *Service) ReportEvents(ctx context.Context, userID uint64, events []Event) error {
	for _, e := range events {
		zap.L().Info("event_reported",
			zap.Uint64("user_id", userID),
			zap.String("event", e.Name),
			zap.Int64("ts", e.Timestamp),
		)
	}
	return nil
}

func (s *Service) ReportError(ctx context.Context, userID uint64, errType, message, stackTrace string) error {
	zap.L().Error("client_error",
		zap.Uint64("user_id", userID),
		zap.String("type", errType),
		zap.String("message", message),
	)
	return nil
}

func (s *Service) SubmitFeedback(ctx context.Context, userID uint64, content, contact, platform, appVersion string) error {
	fb := &model.Feedback{
		UserID:     userID,
		Content:    content,
		Contact:    contact,
		Platform:   platform,
		AppVersion: appVersion,
		Status:     0,
	}
	return s.systemDAO.CreateFeedback(ctx, fb)
}

type Event struct {
	Name      string
	Params    map[string]string
	Timestamp int64
}
