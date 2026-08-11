package templatesubmission

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/service/media"
	templateservice "github.com/zhaojiabo/bobobeads_server/internal/service/template"
	"github.com/zhaojiabo/bobobeads_server/internal/service/work"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	defaultLimit         = 12
	maxLimit             = 50
	defaultDailyLimit    = 5
	maxTitleRunes        = 40
	maxDescriptionRunes  = 200
	maxReviewReasonRunes = 200
	maxAdminPageSize     = 100
	defaultAdminPageSize = 20
)

type Service struct {
	submissionDAO *dao.TemplateSubmissionDAO
	workDAO       *dao.WorkDAO
	userDAO       *dao.UserDAO
	mediaService  *media.Service
	templateAdmin *templateservice.AdminService
	dailyLimit    int
}

func NewService(
	submissionDAO *dao.TemplateSubmissionDAO,
	workDAO *dao.WorkDAO,
	userDAO *dao.UserDAO,
	mediaService *media.Service,
	templateAdmin *templateservice.AdminService,
	dailyLimit int,
) *Service {
	if dailyLimit <= 0 {
		dailyLimit = defaultDailyLimit
	}
	return &Service{
		submissionDAO: submissionDAO,
		workDAO:       workDAO,
		userDAO:       userDAO,
		mediaService:  mediaService,
		templateAdmin: templateAdmin,
		dailyLimit:    dailyLimit,
	}
}

type SubmitInput struct {
	WorkID          uint64
	Title           string
	Description     string
	ClientRequestID string
}

// Submit snapshots the work into a pending submission. Everything the reviewer
// and the eventual template read comes from that snapshot, so later edits or a
// deletion of the source work cannot change what gets published.
func (s *Service) Submit(ctx context.Context, userID uint64, in SubmitInput) (*model.TemplateSubmission, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	in.ClientRequestID = strings.TrimSpace(in.ClientRequestID)

	if in.WorkID == 0 {
		return nil, apperr.InvalidArgument("work_id is required")
	}
	if in.ClientRequestID == "" {
		return nil, apperr.InvalidArgument("client_request_id is required")
	}
	if in.Title == "" {
		return nil, apperr.InvalidArgument("title is required")
	}
	if utf8.RuneCountInString(in.Title) > maxTitleRunes {
		return nil, apperr.InvalidArgument("title must not exceed 40 characters")
	}
	if utf8.RuneCountInString(in.Description) > maxDescriptionRunes {
		return nil, apperr.InvalidArgument("description must not exceed 200 characters")
	}

	existing, err := s.submissionDAO.GetByClientRequestID(ctx, userID, in.ClientRequestID)
	if err != nil {
		return nil, apperr.Internal("get template submission by client request id", err)
	}
	if existing != nil {
		return existing, nil
	}

	if err := s.checkDailyQuota(ctx, userID); err != nil {
		return nil, err
	}

	// GetByIDForUser cannot tell "someone else's work" from "no such work", and
	// reporting NotFound for both keeps foreign work IDs unprobeable.
	w, err := s.workDAO.GetByIDForUser(ctx, in.WorkID, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("work not found")
		}
		return nil, apperr.Internal("get work for submission", err)
	}
	if w.PatternData == nil {
		return nil, apperr.InvalidArgument("work has no pattern data")
	}
	// Recompute the stats instead of trusting bb_work's columns, so a corrupt work
	// row is rejected here rather than becoming a broken official template.
	if err := work.NormalizeWorkPatternData(w); err != nil {
		return nil, err
	}

	active, err := s.submissionDAO.GetActiveByWork(ctx, userID, in.WorkID)
	if err != nil {
		return nil, apperr.Internal("get active submission for work", err)
	}
	if active != nil {
		return nil, apperr.New(apperr.CodeDuplicateRequest, "work already submitted")
	}

	activeWorkKey := strconv.FormatUint(in.WorkID, 10)
	sub := &model.TemplateSubmission{
		UserID:        userID,
		WorkID:        in.WorkID,
		ActiveWorkKey: &activeWorkKey,
		Title:         in.Title,
		Description:   in.Description,
		PatternData:   w.PatternData,
		BoardSpec:     w.BoardSpec,
		Width:         w.Width,
		Height:        w.Height,
		BeadCount:     w.BeadCount,
		ColorCount:    w.ColorCount,
		// The work image is only a candidate preview: bb_work stores whatever URL the
		// client reported, so anything outside our own bucket is dropped and the
		// reviewer uploads a preview instead.
		PreviewURL:       s.mediaService.OwnedImageURL(w.PatternImageURL),
		ThumbnailURL:     s.thumbnailURL(ctx, userID, w),
		OriginalImageURL: s.mediaService.OwnedImageURL(w.OriginalImageURL),
		Status:           model.TemplateSubmissionStatusPending,
		ClientRequestID:  in.ClientRequestID,
	}
	if err := s.submissionDAO.Create(ctx, sub); err != nil {
		if !isDuplicateKey(err) {
			return nil, apperr.Internal("create template submission", err)
		}
		// A concurrent request won one of the unique indexes. Its INSERT blocked
		// ours until it committed, so the winner is readable now.
		return s.resolveCreateConflict(ctx, userID, in, err)
	}
	return sub, nil
}

// thumbnailURL prefers the thumbnail the work already carries: CompleteGeneration
// builds it from the bare pattern image without the colour swatches, which is the
// better grid image, and reusing it saves an object-storage round trip. SaveWork
// and SaveDraft never set that column, so those works still need one generated.
func (s *Service) thumbnailURL(ctx context.Context, userID uint64, w *model.Work) string {
	if url := s.mediaService.OwnedImageURL(w.ThumbnailURL); url != "" {
		return url
	}
	return s.mediaService.ThumbnailURLByImageURL(ctx, userID, w.PatternImageURL)
}

func (s *Service) resolveCreateConflict(ctx context.Context, userID uint64, in SubmitInput, createErr error) (*model.TemplateSubmission, error) {
	if sub, err := s.submissionDAO.GetByClientRequestID(ctx, userID, in.ClientRequestID); err == nil && sub != nil {
		return sub, nil
	}
	if sub, err := s.submissionDAO.GetActiveByWork(ctx, userID, in.WorkID); err == nil && sub != nil {
		return nil, apperr.New(apperr.CodeDuplicateRequest, "work already submitted")
	}
	return nil, apperr.Internal("create template submission", createErr)
}

// checkDailyQuota bounds submissions per day. RateLimitInterceptor works per
// second, which does nothing against a few hundred submissions spread over a day.
func (s *Service) checkDailyQuota(ctx context.Context, userID uint64) error {
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	count, err := s.submissionDAO.CountByUserSince(ctx, userID, startOfDay)
	if err != nil {
		return apperr.Internal("count today's template submissions", err)
	}
	if count >= int64(s.dailyLimit) {
		return apperr.New(apperr.CodeRateLimited, fmt.Sprintf("daily submission limit is %d", s.dailyLimit))
	}
	return nil
}

// ListMine returns the user's submissions newest first, plus the cursor for the
// following page when one exists.
func (s *Service) ListMine(ctx context.Context, userID uint64, limit int32, cursor string) ([]*model.TemplateSubmission, string, error) {
	pageSize := normalizeLimit(limit)
	beforeID, err := decodeCursor(cursor)
	if err != nil {
		return nil, "", err
	}

	// Read one extra row to learn whether another page exists.
	items, err := s.submissionDAO.ListByUser(ctx, userID, beforeID, pageSize+1)
	if err != nil {
		return nil, "", apperr.Internal("list template submissions", err)
	}

	nextCursor := ""
	if len(items) > pageSize {
		items = items[:pageSize]
		nextCursor = encodeCursor(items[len(items)-1].ID)
	}
	return items, nextCursor, nil
}

// ListForAdmin serves the review queue. status is nil to list every state.
func (s *Service) ListForAdmin(ctx context.Context, status *int8, offset, limit int) ([]*model.TemplateSubmission, int64, error) {
	if limit <= 0 {
		limit = defaultAdminPageSize
	}
	if limit > maxAdminPageSize {
		limit = maxAdminPageSize
	}
	if offset < 0 {
		offset = 0
	}
	items, total, err := s.submissionDAO.ListForAdmin(ctx, status, offset, limit)
	if err != nil {
		return nil, 0, apperr.Internal("list template submissions for admin", err)
	}
	return items, total, nil
}

func (s *Service) GetForAdmin(ctx context.Context, id uint64) (*model.TemplateSubmission, error) {
	if id == 0 {
		return nil, apperr.NotFound("submission not found")
	}
	sub, err := s.submissionDAO.GetByID(ctx, id)
	if err != nil {
		return nil, apperr.Internal("get template submission", err)
	}
	if sub == nil {
		return nil, apperr.NotFound("submission not found")
	}
	return sub, nil
}

// ApproveInput carries the fields only a reviewer can decide. Empty Title,
// Description, PreviewURL and ThumbnailURL fall back to the snapshot.
type ApproveInput struct {
	CategoryID   int
	Difficulty   int8
	Tags         string
	Title        string
	Description  string
	PreviewURL   string
	ThumbnailURL string
}

// Approve publishes the snapshot as an official template and marks the submission
// approved in the same transaction, so no crash or concurrent reviewer can leave
// one written without the other.
func (s *Service) Approve(ctx context.Context, id uint64, actor string, in ApproveInput) (uint64, error) {
	if id == 0 {
		return 0, apperr.NotFound("submission not found")
	}
	if err := s.templateAdmin.ValidateActiveCategory(ctx, in.CategoryID); err != nil {
		return 0, apperr.InvalidArgument(err.Error())
	}

	// The nickname is a display snapshot, so a missing or unreadable contributor
	// row must not block publishing.
	nickname := ""
	sub, err := s.GetForAdmin(ctx, id)
	if err != nil {
		return 0, err
	}
	if u, err := s.userDAO.GetByID(ctx, sub.UserID); err == nil && u != nil {
		nickname = u.Nickname
	}

	var templateID uint64
	txErr := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		sub, err := s.submissionDAO.GetByIDTx(tx, id)
		if err != nil {
			return apperr.Internal("get template submission", err)
		}
		if sub == nil {
			return apperr.NotFound("submission not found")
		}
		switch sub.Status {
		case model.TemplateSubmissionStatusApproved:
			templateID = sub.TemplateID
			return nil
		case model.TemplateSubmissionStatusRejected:
			return apperr.InvalidArgument("submission already rejected")
		}

		previewURL := firstNonEmpty(in.PreviewURL, sub.PreviewURL)
		if previewURL == "" {
			return apperr.InvalidArgument("preview image required")
		}

		templateID, err = s.templateAdmin.PublishTemplateTx(tx, templateservice.PublishPayload{
			// A deterministic key derived from the submission ID is what makes approve
			// crash-safe: bb_template_publish_record.idempotency_key is unique, so a
			// retry cannot produce a second template.
			IdempotencyKey:      submissionIdempotencyKey(sub.ID),
			DraftRevisionID:     sub.ID,
			ContributorUserID:   sub.UserID,
			ContributorNickname: nickname,
			UpdatePayload: templateservice.UpdatePayload{
				Title:        firstNonEmpty(in.Title, sub.Title),
				Description:  firstNonEmpty(in.Description, sub.Description),
				CategoryID:   in.CategoryID,
				Tags:         in.Tags,
				Difficulty:   in.Difficulty,
				PreviewURL:   previewURL,
				ThumbnailURL: firstNonEmpty(in.ThumbnailURL, sub.ThumbnailURL),
				BoardSpec:    sub.BoardSpec,
				PatternData:  sub.PatternData,
				Width:        sub.Width,
				Height:       sub.Height,
				ColorCount:   sub.ColorCount,
				BeadCount:    sub.BeadCount,
			},
		})
		if err != nil {
			if errors.Is(err, templateservice.ErrInvalidPayload) || errors.Is(err, templateservice.ErrDuplicateKey) {
				return apperr.InvalidArgument(err.Error())
			}
			return apperr.Internal("publish template from submission", err)
		}

		ok, err := s.submissionDAO.MarkApprovedTx(tx, sub.ID, templateID, actor, time.Now())
		if err != nil {
			return apperr.Internal("mark submission approved", err)
		}
		if !ok {
			return apperr.New(apperr.CodeDuplicateRequest, "submission was reviewed concurrently")
		}
		return nil
	})
	if txErr != nil {
		return 0, txErr
	}

	zap.L().Info("template submission approved",
		zap.Uint64("submission_id", id),
		zap.Uint64("template_id", templateID),
		zap.String("actor", actor))
	return templateID, nil
}

// Reject records the reason and releases active_work_key so the contributor can
// fix the work and submit it again.
func (s *Service) Reject(ctx context.Context, id uint64, actor, reason string) error {
	if id == 0 {
		return apperr.NotFound("submission not found")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return apperr.InvalidArgument("reason is required")
	}
	if utf8.RuneCountInString(reason) > maxReviewReasonRunes {
		return apperr.InvalidArgument("reason must not exceed 200 characters")
	}

	sub, err := s.GetForAdmin(ctx, id)
	if err != nil {
		return err
	}
	switch sub.Status {
	case model.TemplateSubmissionStatusRejected:
		return nil
	case model.TemplateSubmissionStatusApproved:
		// Taking a live template down is a different operation with its own audit log.
		return apperr.InvalidArgument("submission already approved; unpublish the template instead")
	}

	ok, err := s.submissionDAO.MarkRejected(ctx, id, actor, reason, time.Now())
	if err != nil {
		return apperr.Internal("mark submission rejected", err)
	}
	if !ok {
		return apperr.New(apperr.CodeDuplicateRequest, "submission was reviewed concurrently")
	}
	return nil
}

func submissionIdempotencyKey(id uint64) string {
	return fmt.Sprintf("submission-%d", id)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func normalizeLimit(limit int32) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return int(limit)
}

// The cursor stays opaque so it is not mistaken for a submission ID, which is the
// same underlying number.
func encodeCursor(id uint64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatUint(id, 10)))
}

func decodeCursor(cursor string) (uint64, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, apperr.InvalidArgument("invalid cursor")
	}
	id, err := strconv.ParseUint(string(raw), 10, 64)
	if err != nil || id == 0 {
		return 0, apperr.InvalidArgument("invalid cursor")
	}
	return id, nil
}

func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicated key")
}
