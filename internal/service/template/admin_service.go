package template

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var (
	ErrInvalidPayload      = errors.New("invalid publish payload")
	ErrDuplicateKey        = errors.New("idempotency key conflict")
	ErrTemplateNotFound    = errors.New("template not found")
	ErrUnpublishReason     = errors.New("unpublish reason must not exceed 200 characters")
	ErrCategoryNameInvalid = errors.New("category name must contain 1 to 30 characters")
	ErrCategoryNameTaken   = errors.New("category name already exists")
	urlPattern             = regexp.MustCompile(`^https?://`)
)

const (
	maxUnpublishReasonRunes      = 200
	maxTemplateCategoryNameRunes = 30
)

type AdminService struct {
	dao *dao.TemplateDAO
}

func NewAdminService(dao *dao.TemplateDAO) *AdminService {
	return &AdminService{dao: dao}
}

type UpdatePayload struct {
	Title        string
	Description  string
	CategoryID   int
	Tags         string
	Difficulty   int8
	BoardSpec    string
	PreviewURL   string
	ThumbnailURL string
	PatternData  model.JSONMap
	Width        int
	Height       int
	ColorCount   int
	BeadCount    int
}

type PublishPayload struct {
	IdempotencyKey  string
	DraftRevisionID uint64
	UpdatePayload
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
	if templateID == 0 {
		return ErrTemplateNotFound
	}
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > maxUnpublishReasonRunes {
		return ErrUnpublishReason
	}

	found, err := s.dao.UnpublishTemplate(ctx, templateID)
	if err != nil {
		return err
	}
	if !found {
		return ErrTemplateNotFound
	}

	zap.L().Info("template unpublished",
		zap.Uint64("template_id", templateID),
		zap.String("reason", reason))
	return nil
}

func (s *AdminService) CreateCategory(ctx context.Context, name string) (*model.TemplateCategory, error) {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) == 0 || utf8.RuneCountInString(name) > maxTemplateCategoryNameRunes {
		return nil, ErrCategoryNameInvalid
	}

	existing, err := s.dao.GetCategoryByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrCategoryNameTaken
	}

	category := &model.TemplateCategory{Name: name, Status: 1}
	if err := s.dao.CreateCategory(ctx, category); err != nil {
		duplicate, lookupErr := s.dao.GetCategoryByName(ctx, name)
		if lookupErr == nil && duplicate != nil {
			return nil, ErrCategoryNameTaken
		}
		return nil, err
	}
	return category, nil
}

func (s *AdminService) GetPublishedTemplate(ctx context.Context, templateID uint64) (*model.Template, error) {
	if templateID == 0 {
		return nil, ErrTemplateNotFound
	}
	tpl, err := s.dao.GetByID(ctx, templateID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTemplateNotFound
		}
		return nil, err
	}
	return tpl, nil
}

func (s *AdminService) UpdateTemplate(ctx context.Context, templateID uint64, payload UpdatePayload) error {
	if templateID == 0 {
		return ErrTemplateNotFound
	}
	if err := s.validateUpdatePayload(payload); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidPayload, err.Error())
	}
	if _, err := s.dao.GetActiveCategoryByID(ctx, payload.CategoryID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: category_id must reference an active category", ErrInvalidPayload)
		}
		return err
	}

	updated, err := s.dao.UpdatePublishedTemplate(ctx, templateID, &model.Template{
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
	})
	if err != nil {
		return err
	}
	if !updated {
		return ErrTemplateNotFound
	}
	return nil
}

func (s *AdminService) GetPublishStatus(ctx context.Context, idempotencyKey string) (*model.TemplatePublishRecord, error) {
	return s.dao.GetPublishRecordByKey(ctx, idempotencyKey)
}

func (s *AdminService) validatePayload(p PublishPayload) error {
	if p.IdempotencyKey == "" {
		return fmt.Errorf("idempotency_key required")
	}
	return s.validateTemplateFields(p.Title, p.CategoryID, p.Width, p.Height, p.PatternData, p.PreviewURL, p.ThumbnailURL)
}

func (s *AdminService) validateUpdatePayload(p UpdatePayload) error {
	return s.validateTemplateFields(p.Title, p.CategoryID, p.Width, p.Height, p.PatternData, p.PreviewURL, p.ThumbnailURL)
}

func (s *AdminService) validateTemplateFields(title string, categoryID, width, height int, patternData model.JSONMap, previewURL, thumbnailURL string) error {
	if title == "" {
		return fmt.Errorf("title required")
	}
	if categoryID <= 0 {
		return fmt.Errorf("category_id must be positive")
	}
	if width <= 0 || height <= 0 {
		return fmt.Errorf("width and height must be positive")
	}
	if patternData == nil {
		return fmt.Errorf("pattern_data required")
	}
	if previewURL != "" && !urlPattern.MatchString(previewURL) {
		return fmt.Errorf("invalid preview_url")
	}
	if thumbnailURL != "" && !urlPattern.MatchString(thumbnailURL) {
		return fmt.Errorf("invalid thumbnail_url")
	}
	return nil
}
