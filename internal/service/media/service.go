package media

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zhaojiabo/bobobeads_server/conf"
	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
)

const AdminPreviewMaxFileSize = 10 * 1024 * 1024

var purposeConfig = map[string]struct {
	MaxSize      int64
	AllowedTypes []string
}{
	"original":      {MaxSize: 20 * 1024 * 1024, AllowedTypes: []string{"image/jpeg", "image/png", "image/webp", "image/heic"}},
	"pattern":       {MaxSize: 10 * 1024 * 1024, AllowedTypes: []string{"image/jpeg", "image/png", "image/webp"}},
	"avatar":        {MaxSize: 5 * 1024 * 1024, AllowedTypes: []string{"image/jpeg", "image/png", "image/webp"}},
	"feedback":      {MaxSize: 10 * 1024 * 1024, AllowedTypes: []string{"image/jpeg", "image/png", "image/webp"}},
	"style_input":   {MaxSize: 20 * 1024 * 1024, AllowedTypes: []string{"image/jpeg", "image/png", "image/webp", "image/heic"}},
	"ai_output":     {MaxSize: 20 * 1024 * 1024, AllowedTypes: []string{"image/jpeg", "image/png", "image/webp"}},
	"admin_preview": {MaxSize: AdminPreviewMaxFileSize, AllowedTypes: []string{"image/jpeg", "image/png", "image/webp"}},
}

const adminMediaOwnerID uint64 = 0

type Service struct {
	mediaDAO   *dao.MediaDAO
	storage    ObjectStorage
	storageErr error
}

func NewService(mediaDAO *dao.MediaDAO) *Service {
	var cfg conf.OSSConfig
	if conf.GlobalConfig != nil {
		cfg = conf.GlobalConfig.OSS
	}
	storage, err := NewOSSStorage(cfg)
	return &Service{mediaDAO: mediaDAO, storage: storage, storageErr: err}
}

// NewServiceWithStorage is used by integration tests and supports a future
// alternative provider without exposing provider-specific details to callers.
func NewServiceWithStorage(mediaDAO *dao.MediaDAO, storage ObjectStorage) *Service {
	if storage == nil {
		return &Service{mediaDAO: mediaDAO, storageErr: fmt.Errorf("object storage is required")}
	}
	return &Service{mediaDAO: mediaDAO, storage: storage}
}

func (s *Service) objectStorage() (ObjectStorage, error) {
	if s.storageErr != nil {
		return nil, apperr.Internal("configure object storage", s.storageErr)
	}
	if s.storage == nil {
		return nil, apperr.Internal("configure object storage", fmt.Errorf("object storage is unavailable"))
	}
	return s.storage, nil
}

type UploadToken struct {
	UploadURL    string
	FileKey      string
	Headers      map[string]string
	FormData     map[string]string
	ExpiresAt    int64
	UploadMethod string
	PublicURL    string
	MaxFileSize  int64
}

func (s *Service) GetUploadToken(ctx context.Context, userID uint64, fileName, contentType, purpose string) (*UploadToken, error) {
	pc, ok := purposeConfig[purpose]
	if !ok {
		return nil, apperr.InvalidArgument("invalid purpose: " + purpose)
	}

	if !isAllowedType(contentType, pc.AllowedTypes) {
		return nil, apperr.InvalidFileType("content type not allowed for purpose: " + purpose)
	}

	ext := inferExtension(contentType, fileName)
	now := time.Now()
	fileKey := fmt.Sprintf("%s/%d/%02d/%02d/%d/%s%s",
		purpose, now.Year(), now.Month(), now.Day(), userID, uuid.NewString(), ext)

	storage, err := s.objectStorage()
	if err != nil {
		return nil, err
	}
	presignedUpload, err := storage.PresignPut(ctx, fileKey, contentType, 30*time.Minute)
	if err != nil {
		return nil, apperr.Internal("create OSS upload token", err)
	}

	asset := &model.MediaAsset{
		UserID:      userID,
		FileKey:     fileKey,
		Purpose:     purpose,
		ContentType: contentType,
		Status:      model.MediaStatusPending,
	}
	if err := s.mediaDAO.Create(ctx, asset); err != nil {
		return nil, apperr.Internal("create media asset", err)
	}

	headers := map[string]string{
		"Content-Type": contentType,
	}
	for name, value := range presignedUpload.Headers {
		headers[name] = value
	}

	publicURL := storage.PublicURL(fileKey)

	return &UploadToken{
		UploadURL:    presignedUpload.URL,
		FileKey:      fileKey,
		Headers:      headers,
		FormData:     map[string]string{},
		ExpiresAt:    presignedUpload.ExpiresAt.Unix(),
		UploadMethod: http.MethodPut,
		PublicURL:    publicURL,
		MaxFileSize:  pc.MaxSize,
	}, nil
}

func (s *Service) ReportUpload(ctx context.Context, userID uint64, fileKey string, fileSize int64) (string, error) {
	asset, err := s.mediaDAO.GetByFileKeyAndUser(ctx, fileKey, userID)
	if err != nil {
		return "", apperr.Forbidden("file not found or not owned by user")
	}

	pc, ok := purposeConfig[asset.Purpose]
	if ok && fileSize > pc.MaxSize {
		return "", apperr.FileTooLarge(pc.MaxSize)
	}

	if err := s.mediaDAO.MarkUploaded(ctx, fileKey, fileSize); err != nil {
		return "", apperr.Internal("mark uploaded", err)
	}

	storage, err := s.objectStorage()
	if err != nil {
		return "", err
	}
	publicURL := storage.PublicURL(fileKey)
	return publicURL, nil
}

// GetAdminPreviewUploadToken only creates assets intended to become a public
// official-template preview. The browser receives a presigned object-storage
// upload URL, never object-storage credentials.
func (s *Service) GetAdminPreviewUploadToken(ctx context.Context, fileName, contentType string) (*UploadToken, error) {
	return s.GetUploadToken(ctx, adminMediaOwnerID, fileName, contentType, "admin_preview")
}

func (s *Service) ReportAdminPreviewUpload(ctx context.Context, fileKey string, fileSize int64) (string, error) {
	return s.ReportUpload(ctx, adminMediaOwnerID, fileKey, fileSize)
}

// UploadAdminPreview receives the small, operator-only preview through the
// application server, then writes it as a public object. This deliberately
// avoids browser CORS access and keeps normal user uploads private.
func (s *Service) UploadAdminPreview(ctx context.Context, contentType string, content []byte) (string, string, error) {
	if len(content) == 0 {
		return "", "", apperr.InvalidArgument("preview image is empty")
	}
	if len(content) > AdminPreviewMaxFileSize {
		return "", "", apperr.FileTooLarge(AdminPreviewMaxFileSize)
	}

	token, err := s.GetAdminPreviewUploadToken(ctx, "official-template-preview.png", contentType)
	if err != nil {
		return "", "", err
	}
	storage, err := s.objectStorage()
	if err != nil {
		return "", "", err
	}
	if err := storage.PutPublic(ctx, token.FileKey, contentType, content); err != nil {
		return "", "", apperr.Internal("upload admin preview to object storage", err)
	}

	fileURL, err := s.ReportAdminPreviewUpload(ctx, token.FileKey, int64(len(content)))
	if err != nil {
		return "", "", err
	}
	return token.FileKey, fileURL, nil
}

// GetUploadedAdminPreviewURL proves that a file key was created by the admin
// upload flow and has completed uploading before it can become public template
// metadata. The public URL is derived server-side instead of accepted from the
// browser request.
func (s *Service) GetUploadedAdminPreviewURL(ctx context.Context, fileKey string) (string, error) {
	asset, err := s.mediaDAO.GetUploadedAsset(ctx, fileKey, adminMediaOwnerID, "admin_preview")
	if err != nil {
		return "", apperr.Internal("get admin preview asset", err)
	}
	if asset == nil {
		return "", apperr.Forbidden("admin preview must be uploaded before publishing")
	}
	storage, err := s.objectStorage()
	if err != nil {
		return "", err
	}
	return storage.PublicURL(asset.FileKey), nil
}

func (s *Service) GetFileURL(ctx context.Context, fileKey string) (string, int64, error) {
	storage, err := s.objectStorage()
	if err != nil {
		return "", 0, err
	}
	url := storage.PublicURL(fileKey)
	expiresAt := time.Now().Add(2 * time.Hour).Unix()
	return url, expiresAt, nil
}

func isAllowedType(contentType string, allowed []string) bool {
	for _, t := range allowed {
		if strings.EqualFold(contentType, t) {
			return true
		}
	}
	return false
}

func inferExtension(contentType, fileName string) string {
	switch strings.ToLower(contentType) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/heic":
		return ".heic"
	default:
		ext := path.Ext(fileName)
		if ext != "" {
			return ext
		}
		return ".bin"
	}
}
