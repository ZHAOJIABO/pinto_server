package finishedproduct

import (
	"context"
	"encoding/base64"
	"strconv"
	"strings"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/service/media"
)

const (
	defaultLimit = 12
	maxLimit     = 50
)

type Service struct {
	finishedProductDAO *dao.FinishedProductDAO
	mediaService       *media.Service
}

func NewService(finishedProductDAO *dao.FinishedProductDAO, mediaService *media.Service) *Service {
	return &Service{
		finishedProductDAO: finishedProductDAO,
		mediaService:       mediaService,
	}
}

// Create binds an uploaded media file to a finished product. It is idempotent
// on both client_request_id and media_file_key: a repeat submit returns the
// record that already exists instead of failing.
func (s *Service) Create(ctx context.Context, userID uint64, mediaFileKey, clientRequestID string) (*model.FinishedProduct, error) {
	mediaFileKey = strings.TrimSpace(mediaFileKey)
	clientRequestID = strings.TrimSpace(clientRequestID)
	if mediaFileKey == "" {
		return nil, apperr.InvalidArgument("media_file_key is required")
	}
	if clientRequestID == "" {
		return nil, apperr.InvalidArgument("client_request_id is required")
	}

	existing, err := s.finishedProductDAO.GetByClientRequestID(ctx, userID, clientRequestID)
	if err != nil {
		return nil, apperr.Internal("get finished product by client request id", err)
	}
	if existing != nil {
		return existing, nil
	}

	_, imageURL, err := s.mediaService.ResolveUploadedAsset(ctx, userID, mediaFileKey, media.PurposeFinishedProduct)
	if err != nil {
		return nil, err
	}

	existing, err = s.finishedProductDAO.GetByMediaFileKey(ctx, userID, mediaFileKey)
	if err != nil {
		return nil, apperr.Internal("get finished product by media file key", err)
	}
	if existing != nil {
		return existing, nil
	}

	// Phone photos are the largest images this app stores, so the grid must not
	// fall back to them. A failed thumbnail is logged and left empty rather than
	// costing the user their saved product.
	fp := &model.FinishedProduct{
		UserID:          userID,
		MediaFileKey:    mediaFileKey,
		ImageURL:        imageURL,
		ThumbnailURL:    s.mediaService.ThumbnailURLByFileKey(ctx, userID, mediaFileKey),
		ClientRequestID: clientRequestID,
	}
	if err := s.finishedProductDAO.Create(ctx, fp); err != nil {
		if !isDuplicateKey(err) {
			return nil, apperr.Internal("create finished product", err)
		}
		// A concurrent request won one of the unique indexes. Its INSERT blocked
		// ours until it committed, so the winner is readable now.
		return s.findExisting(ctx, userID, mediaFileKey, clientRequestID, err)
	}
	return fp, nil
}

func (s *Service) findExisting(ctx context.Context, userID uint64, mediaFileKey, clientRequestID string, createErr error) (*model.FinishedProduct, error) {
	if fp, err := s.finishedProductDAO.GetByClientRequestID(ctx, userID, clientRequestID); err == nil && fp != nil {
		return fp, nil
	}
	if fp, err := s.finishedProductDAO.GetByMediaFileKey(ctx, userID, mediaFileKey); err == nil && fp != nil {
		return fp, nil
	}
	return nil, apperr.Internal("create finished product", createErr)
}

// List returns the user's finished products newest first, plus the cursor for
// the following page when one exists.
func (s *Service) List(ctx context.Context, userID uint64, limit int32, cursor string) ([]*model.FinishedProduct, string, error) {
	pageSize := normalizeLimit(limit)
	beforeID, err := decodeCursor(cursor)
	if err != nil {
		return nil, "", err
	}

	// Read one extra row to learn whether another page exists.
	items, err := s.finishedProductDAO.ListByUser(ctx, userID, beforeID, pageSize+1)
	if err != nil {
		return nil, "", apperr.Internal("list finished products", err)
	}

	nextCursor := ""
	if len(items) > pageSize {
		items = items[:pageSize]
		nextCursor = encodeCursor(items[len(items)-1].ID)
	}
	return items, nextCursor, nil
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

// The cursor stays opaque so it is not mistaken for a finished product ID,
// which is the same underlying number.
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
