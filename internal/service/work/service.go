package work

import (
	"context"
	"strings"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	"gorm.io/gorm"
)

// Thumbnailer generates a thumbnail for one of our own public image URLs and
// returns "" for anything it cannot handle. Foreign URLs are rejected by the
// implementation, so a client cannot make the server fetch an arbitrary host.
type Thumbnailer interface {
	ThumbnailURLByImageURL(ctx context.Context, userID uint64, imageURL string) string
}

// SubmissionLock reports whether a work still has a template submission waiting
// for review. Only pending submissions count: once a submission is approved the
// published template is an independent snapshot, so later edits to the work
// cannot affect it, and a rejected submission holds nothing.
type SubmissionLock interface {
	HasPendingByWork(ctx context.Context, userID, workID uint64) (bool, error)
}

type Service struct {
	workDAO     *dao.WorkDAO
	thumbnailer Thumbnailer
	submissions SubmissionLock
}

func NewService(workDAO *dao.WorkDAO, thumbnailer Thumbnailer, submissions SubmissionLock) *Service {
	return &Service{workDAO: workDAO, thumbnailer: thumbnailer, submissions: submissions}
}

// ensureNotUnderReview blocks edits while a submission is queued for review.
// The snapshot means an edit could not corrupt the review, but letting it
// through would tell the user their changes are what gets reviewed, which is
// false. Failing the request beats that misunderstanding.
func (s *Service) ensureNotUnderReview(ctx context.Context, userID, workID uint64) error {
	if s.submissions == nil {
		return nil
	}
	pending, err := s.submissions.HasPendingByWork(ctx, userID, workID)
	if err != nil {
		return apperr.Internal("check template submission", err)
	}
	if pending {
		return apperr.New(apperr.CodeWorkUnderReview, "work has a template submission under review")
	}
	return nil
}

func (s *Service) SaveWork(ctx context.Context, userID uint64, work *model.Work, patternData *pb.PatternData) (uint64, error) {
	if err := ApplyPatternData(work, patternData); err != nil {
		return 0, err
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

// UpdateWorkInput carries only the fields a client may change. An empty string
// means "leave as is": none of these columns has a meaningful empty value, and
// proto3 cannot tell an omitted field from an empty one.
type UpdateWorkInput struct {
	Title            string
	OriginalImageURL string
	PatternImageURL  string
	// ThumbnailSource is the image the thumbnail is generated from, not the
	// thumbnail itself. Empty falls back to the pattern image.
	ThumbnailSource string
	PatternData     *pb.PatternData
}

// UpdateWork edits an existing work in place. The row is loaded first and only
// the client-owned fields are overwritten, so Status, SourceType, SourceID and
// CreatedAt survive the full-column save the DAO performs.
func (s *Service) UpdateWork(ctx context.Context, userID, workID uint64, in UpdateWorkInput) (*model.Work, error) {
	if workID == 0 {
		return nil, apperr.InvalidArgument("work_id is required")
	}
	w, err := s.workDAO.GetByIDForUser(ctx, workID, userID)
	if err != nil {
		return nil, apperr.NotFound("work not found")
	}
	// Ownership is resolved first so the review state of someone else's work is
	// never observable.
	if err := s.ensureNotUnderReview(ctx, userID, workID); err != nil {
		return nil, err
	}

	if title := strings.TrimSpace(in.Title); title != "" {
		w.Title = title
	}
	if url := strings.TrimSpace(in.OriginalImageURL); url != "" {
		w.OriginalImageURL = url
	}
	patternImageChanged := false
	if url := strings.TrimSpace(in.PatternImageURL); url != "" && url != w.PatternImageURL {
		w.PatternImageURL = url
		patternImageChanged = true
	}
	if in.PatternData != nil {
		if err := ApplyPatternData(w, in.PatternData); err != nil {
			return nil, err
		}
	}

	// Regenerating costs an object-storage round trip, so it only happens when the
	// client offers a new source or replaces the pattern image the old thumbnail
	// was built from. Otherwise the existing thumbnail stays.
	if source := strings.TrimSpace(in.ThumbnailSource); source != "" {
		s.refreshThumbnail(ctx, userID, w, source)
	} else if patternImageChanged {
		s.refreshThumbnail(ctx, userID, w, w.PatternImageURL)
	}

	if err := s.workDAO.Update(ctx, w); err != nil {
		return nil, apperr.Internal("update work", err)
	}
	return w, nil
}

// refreshThumbnail keeps the previous thumbnail when generation fails: a stale
// image beats an empty grid cell in the work list.
func (s *Service) refreshThumbnail(ctx context.Context, userID uint64, w *model.Work, sourceURL string) {
	if s.thumbnailer == nil {
		return
	}
	if url := s.thumbnailer.ThumbnailURLByImageURL(ctx, userID, sourceURL); url != "" {
		w.ThumbnailURL = url
	}
}

func (s *Service) ListWorks(ctx context.Context, userID uint64, page, pageSize int, sourceType string) ([]*model.Work, int64, error) {
	offset := (page - 1) * pageSize
	if sourceType != "" {
		return s.workDAO.ListByUserIDAndSource(ctx, userID, 2, sourceType, offset, pageSize)
	}
	return s.workDAO.ListByUserID(ctx, userID, 2, offset, pageSize)
}

func (s *Service) DeleteWork(ctx context.Context, userID, workID uint64) error {
	return s.workDAO.Delete(ctx, workID, userID)
}

func (s *Service) SaveDraft(ctx context.Context, userID uint64, work *model.Work, patternData *pb.PatternData) (uint64, error) {
	if patternData != nil {
		if err := ApplyPatternData(work, patternData); err != nil {
			return 0, err
		}
	}

	work.UserID = userID
	work.Status = 1

	if work.ID > 0 {
		existing, err := s.workDAO.GetByIDForUser(ctx, work.ID, userID)
		if err != nil {
			return 0, apperr.Forbidden("draft not found or not owned by user")
		}
		// Saving over an existing row is an edit, so the review lock applies here
		// too. Without this the draft endpoint would be a way around UpdateWork.
		if err := s.ensureNotUnderReview(ctx, userID, work.ID); err != nil {
			return 0, err
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
