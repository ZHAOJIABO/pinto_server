package template

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"go.uber.org/zap"
)

var (
	ErrInvalidPayload    = errors.New("invalid publish payload")
	ErrDuplicateKey      = errors.New("idempotency key conflict")
	ErrTemplateNotFound  = errors.New("template not found")
	urlPattern           = regexp.MustCompile(`^https?://`)
)

type AdminService struct {
	dao *dao.TemplateDAO
}

func NewAdminService(dao *dao.TemplateDAO) *AdminService {
	return &AdminService{dao: dao}
}

type PublishPayload struct {
	IdempotencyKey  string
	DraftRevisionID uint64
	Title           string
	Description     string
	CategoryID      int
	Tags            string
	Difficulty      int8
	BoardSpec       string
	PreviewURL      string
	ThumbnailURL    string
	PatternData     model.JSONMap
	Width           int
	Height          int
	ColorCount      int
	BeadCount       int
}

func (s *AdminService) PublishTemplate(ctx context.Context, payload PublishPayload) (uint64, error) {
	if err := s.validatePayload(payload); err != nil {
		return 0, fmt.Errorf("%w: %s", ErrInvalidPayload, err.Error())
	}

	// Check idempotency: if key exists, return original result
	existing, err := s.dao.GetPublishRecordByKey(ctx, payload.IdempotencyKey)
	if err != nil {
		return 0, err
	}
	if existing != nil {
		if existing.DraftRevisionID != payload.DraftRevisionID {
			return 0, ErrDuplicateKey
		}
		return existing.TemplateID, nil
	}

	// Create or update the template
	tpl := &model.Template{
		CategoryID:   payload.CategoryID,
		Title:        payload.Title,
		PreviewURL:   payload.PreviewURL,
		ThumbnailURL: payload.ThumbnailURL,
		Description:  payload.Description,
		PatternData:  payload.PatternData,
		BoardSpec:    payload.BoardSpec,
		Tags:         payload.Tags,
		Difficulty:   payload.Difficulty,
		Width:        payload.Width,
		Height:       payload.Height,
		ColorCount:   payload.ColorCount,
		IsFree:       true,
		Status:       1, // active
	}

	templateID, err := s.dao.CreateOrUpdateTemplate(ctx, tpl)
	if err != nil {
		return 0, err
	}

	// Record idempotency
	record := &model.TemplatePublishRecord{
		IdempotencyKey:  payload.IdempotencyKey,
		TemplateID:      templateID,
		DraftRevisionID: payload.DraftRevisionID,
		Status:          "published",
	}
	if err := s.dao.CreatePublishRecord(ctx, record); err != nil {
		zap.L().Error("failed to create publish record", zap.Error(err))
	}

	return templateID, nil
}

func (s *AdminService) UnpublishTemplate(ctx context.Context, templateID uint64, reason string) error {
	if err := s.dao.SetTemplateStatus(ctx, templateID, 0); err != nil { // 0 = inactive
		return err
	}

	zap.L().Info("template unpublished",
		zap.Uint64("template_id", templateID),
		zap.String("reason", reason))
	return nil
}

func (s *AdminService) GetPublishStatus(ctx context.Context, idempotencyKey string) (*model.TemplatePublishRecord, error) {
	return s.dao.GetPublishRecordByKey(ctx, idempotencyKey)
}

func (s *AdminService) validatePayload(p PublishPayload) error {
	if p.IdempotencyKey == "" {
		return fmt.Errorf("idempotency_key required")
	}
	if p.Title == "" {
		return fmt.Errorf("title required")
	}
	if p.CategoryID <= 0 {
		return fmt.Errorf("category_id must be positive")
	}
	if p.Width <= 0 || p.Height <= 0 {
		return fmt.Errorf("width and height must be positive")
	}
	if p.PatternData == nil {
		return fmt.Errorf("pattern_data required")
	}
	if p.PreviewURL != "" && !urlPattern.MatchString(p.PreviewURL) {
		return fmt.Errorf("invalid preview_url")
	}
	if p.ThumbnailURL != "" && !urlPattern.MatchString(p.ThumbnailURL) {
		return fmt.Errorf("invalid thumbnail_url")
	}
	return nil
}
