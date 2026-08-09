package media

import (
	"context"
	stderrors "errors"
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
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const AdminPreviewMaxFileSize = 10 * 1024 * 1024

// PurposeFinishedProduct 标记用户「我的成品」照片。
const PurposeFinishedProduct = "finished_product"

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

	PurposeFinishedProduct: {MaxSize: 20 * 1024 * 1024, AllowedTypes: []string{"image/jpeg", "image/png", "image/webp"}},
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
	presignedUpload, err := storage.PresignPublicPut(ctx, fileKey, contentType, 30*time.Minute)
	if err != nil {
		return nil, apperr.Internal("create OSS upload token", err)
	}

	// The public URL is a pure join of the base URL and the key, so it is known
	// before the upload happens and is stored with the record.
	publicURL := storage.PublicURL(fileKey)

	asset := &model.MediaAsset{
		UserID:      userID,
		FileKey:     fileKey,
		FileURL:     publicURL,
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

// ResolveUploadedAsset proves a file key is owned by userID, was created for the
// given purpose, and finished uploading, then returns it with its public URL.
// Unlike GetUploadedAsset it reports each failure distinctly so callers can map
// them to not-found / forbidden / invalid-argument.
func (s *Service) ResolveUploadedAsset(ctx context.Context, userID uint64, fileKey, purpose string) (*model.MediaAsset, string, error) {
	if _, ok := purposeConfig[purpose]; !ok {
		return nil, "", apperr.InvalidArgument("invalid purpose: " + purpose)
	}

	asset, err := s.mediaDAO.GetByFileKey(ctx, fileKey)
	if stderrors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", apperr.NotFound("media file not found")
	}
	if err != nil {
		return nil, "", apperr.Internal("get media asset", err)
	}
	if asset.UserID != userID {
		return nil, "", apperr.Forbidden("media file not owned by user")
	}
	if asset.Purpose != purpose {
		return nil, "", apperr.InvalidArgument("media file purpose is not " + purpose)
	}
	if asset.Status != model.MediaStatusUploaded {
		return nil, "", apperr.InvalidArgument("media file upload is not confirmed")
	}

	storage, err := s.objectStorage()
	if err != nil {
		return nil, "", err
	}
	publicURL := storage.PublicURL(asset.FileKey)
	if publicURL == "" {
		// Persisting an empty URL would be permanent; fail loudly instead.
		return nil, "", apperr.Internal("resolve media public url", fmt.Errorf("public base URL is not configured"))
	}
	return asset, publicURL, nil
}

// PutThumbnail generates a thumbnail from bytes the caller already holds and
// stores it as a public object beside the original. It returns the public
// thumbnail URL.
func (s *Service) PutThumbnail(ctx context.Context, sourceFileKey string, content []byte) (string, error) {
	thumbnailKey := ThumbnailFileKey(sourceFileKey)
	if thumbnailKey == "" {
		return "", apperr.InvalidArgument("source file key is required")
	}
	thumbnail, err := GenerateThumbnail(content)
	if err != nil {
		return "", apperr.Internal("generate thumbnail", err)
	}
	storage, err := s.objectStorage()
	if err != nil {
		return "", err
	}
	if err := storage.PutPublic(ctx, thumbnailKey, ThumbnailContentType, thumbnail); err != nil {
		return "", apperr.Internal("upload thumbnail to object storage", err)
	}
	publicURL := storage.PublicURL(thumbnailKey)
	if publicURL == "" {
		return "", apperr.Internal("resolve thumbnail public url", fmt.Errorf("public base URL is not configured"))
	}
	return publicURL, nil
}

// ThumbnailForFileKey downloads an uploaded object owned by userID and stores a
// thumbnail beside it. Use it on paths where the server never saw the bytes
// because the client uploaded straight to object storage.
func (s *Service) ThumbnailForFileKey(ctx context.Context, userID uint64, fileKey string) (string, error) {
	asset, err := s.mediaDAO.GetByFileKeyAndUser(ctx, fileKey, userID)
	if err != nil {
		return "", apperr.Forbidden("file not found or not owned by user")
	}
	if asset.Status != model.MediaStatusUploaded {
		return "", apperr.InvalidArgument("media file upload is not confirmed")
	}

	maxSize, ok := GetPurposeMaxSize(asset.Purpose)
	if !ok {
		return "", apperr.InvalidArgument("invalid purpose: " + asset.Purpose)
	}
	storage, err := s.objectStorage()
	if err != nil {
		return "", err
	}
	content, _, err := storage.Get(ctx, asset.FileKey, maxSize)
	if err != nil {
		return "", apperr.Internal("read object from object storage", err)
	}
	return s.PutThumbnail(ctx, asset.FileKey, content)
}

// ThumbnailURLByFileKey is the non-fatal form of ThumbnailForFileKey. A
// thumbnail is only a bandwidth optimisation, so a failure is logged and
// reported as "", and the client falls back to the full-size image.
func (s *Service) ThumbnailURLByFileKey(ctx context.Context, userID uint64, fileKey string) string {
	thumbnailURL, err := s.ThumbnailForFileKey(ctx, userID, fileKey)
	if err != nil {
		zap.L().Warn("thumbnail generation failed",
			zap.Uint64("user_id", userID),
			zap.String("file_key", fileKey),
			zap.Error(err))
		return ""
	}
	return thumbnailURL
}

// ThumbnailURLByImageURL is for callers that only hold a public image URL, such
// as the work APIs where the client submits pattern_image_url rather than a file
// key. URLs outside our own bucket yield "".
func (s *Service) ThumbnailURLByImageURL(ctx context.Context, userID uint64, imageURL string) string {
	if strings.TrimSpace(imageURL) == "" {
		return ""
	}
	storage, err := s.objectStorage()
	if err != nil {
		zap.L().Warn("skip thumbnail: object storage unavailable", zap.Error(err))
		return ""
	}
	fileKey, ok := storage.FileKeyFromPublicURL(imageURL)
	if !ok {
		// A legacy record or an externally hosted image, not one of our objects.
		return ""
	}
	return s.ThumbnailURLByFileKey(ctx, userID, fileKey)
}

// AdminPreviewThumbnailURL stores a thumbnail for an uploaded official-template
// preview. Both admin upload paths end holding only the key, so the bytes are
// read back from object storage here rather than at upload time.
func (s *Service) AdminPreviewThumbnailURL(ctx context.Context, fileKey string) string {
	return s.ThumbnailURLByFileKey(ctx, adminMediaOwnerID, fileKey)
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
// avoids browser CORS access.
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

// GetPurposeMaxSize exposes the configured size ceiling so callers that buffer
// object content in memory can bound their reads.
func GetPurposeMaxSize(purpose string) (int64, bool) {
	pc, ok := purposeConfig[purpose]
	if !ok {
		return 0, false
	}
	return pc.MaxSize, true
}

// GetObjectBytes reads an uploaded object owned by userID for the given
// purpose. Ownership is still verified before the service reads from object
// storage, even though uploaded images are publicly readable.
func (s *Service) GetObjectBytes(ctx context.Context, userID uint64, fileKey, purpose string) ([]byte, string, error) {
	pc, ok := purposeConfig[purpose]
	if !ok {
		return nil, "", apperr.InvalidArgument("invalid purpose: " + purpose)
	}
	asset, err := s.mediaDAO.GetUploadedAsset(ctx, fileKey, userID, purpose)
	if err != nil {
		return nil, "", apperr.Internal("get media asset", err)
	}
	if asset == nil {
		return nil, "", apperr.Forbidden("file not found or not owned by user")
	}
	storage, err := s.objectStorage()
	if err != nil {
		return nil, "", err
	}
	content, contentType, err := storage.Get(ctx, asset.FileKey, pc.MaxSize)
	if err != nil {
		return nil, "", apperr.Internal("read object from object storage", err)
	}
	if contentType == "" {
		contentType = asset.ContentType
	}
	return content, contentType, nil
}

// UploadAIOutput stores a generated image as a public object so the client can
// render it directly from the returned URL.
func (s *Service) UploadAIOutput(ctx context.Context, userID uint64, contentType string, content []byte) (string, string, error) {
	if len(content) == 0 {
		return "", "", apperr.InvalidArgument("ai output image is empty")
	}
	pc := purposeConfig["ai_output"]
	if int64(len(content)) > pc.MaxSize {
		return "", "", apperr.FileTooLarge(pc.MaxSize)
	}

	token, err := s.GetUploadToken(ctx, userID, "ai-output", contentType, "ai_output")
	if err != nil {
		return "", "", err
	}
	storage, err := s.objectStorage()
	if err != nil {
		return "", "", err
	}
	if err := storage.PutPublic(ctx, token.FileKey, contentType, content); err != nil {
		return "", "", apperr.Internal("upload ai output to object storage", err)
	}

	fileURL, err := s.ReportUpload(ctx, userID, token.FileKey, int64(len(content)))
	if err != nil {
		return "", "", err
	}
	return token.FileKey, fileURL, nil
}

func (s *Service) GetFileURL(ctx context.Context, fileKey string) (string, int64, error) {
	storage, err := s.objectStorage()
	if err != nil {
		return "", 0, err
	}
	return storage.PublicURL(fileKey), 0, nil
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
