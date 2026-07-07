package media

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zhaojiabo/bobobeads_server/conf"
	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
)

var purposeConfig = map[string]struct {
	MaxSize      int64
	AllowedTypes []string
}{
	"original": {MaxSize: 20 * 1024 * 1024, AllowedTypes: []string{"image/jpeg", "image/png", "image/webp", "image/heic"}},
	"pattern":  {MaxSize: 10 * 1024 * 1024, AllowedTypes: []string{"image/jpeg", "image/png", "image/webp"}},
	"avatar":   {MaxSize: 5 * 1024 * 1024, AllowedTypes: []string{"image/jpeg", "image/png", "image/webp"}},
	"feedback": {MaxSize: 10 * 1024 * 1024, AllowedTypes: []string{"image/jpeg", "image/png", "image/webp"}},
}

type Service struct {
	mediaDAO *dao.MediaDAO
}

func NewService(mediaDAO *dao.MediaDAO) *Service {
	return &Service{mediaDAO: mediaDAO}
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

	cfg := conf.GlobalConfig.OSS
	expiresAt := now.Add(30 * time.Minute)
	uploadURL := generatePresignedPutURL(cfg, fileKey, contentType, expiresAt)

	headers := map[string]string{
		"Content-Type": contentType,
	}

	publicURL := fmt.Sprintf("https://%s/%s", getPublicDomain(cfg), fileKey)

	return &UploadToken{
		UploadURL:    uploadURL,
		FileKey:      fileKey,
		Headers:      headers,
		FormData:     map[string]string{},
		ExpiresAt:    expiresAt.Unix(),
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

	cfg := conf.GlobalConfig.OSS
	publicURL := fmt.Sprintf("https://%s/%s", getPublicDomain(cfg), fileKey)
	return publicURL, nil
}

func (s *Service) GetFileURL(ctx context.Context, fileKey string) (string, int64, error) {
	cfg := conf.GlobalConfig.OSS
	domain := getPublicDomain(cfg)
	url := fmt.Sprintf("https://%s/%s", domain, fileKey)
	expiresAt := time.Now().Add(2 * time.Hour).Unix()
	return url, expiresAt, nil
}

func generatePresignedPutURL(cfg conf.OSSConfig, fileKey, contentType string, expires time.Time) string {
	expUnix := expires.Unix()
	stringToSign := fmt.Sprintf("PUT\n\n%s\n%d\n/%s/%s", contentType, expUnix, cfg.BucketName, fileKey)

	mac := hmac.New(sha1.New, []byte(cfg.AccessKeySecret))
	mac.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	values := url.Values{}
	values.Set("OSSAccessKeyId", cfg.AccessKeyID)
	values.Set("Expires", fmt.Sprintf("%d", expUnix))
	values.Set("Signature", signature)

	return fmt.Sprintf("https://%s.%s/%s?%s", cfg.BucketName, cfg.Endpoint, fileKey, values.Encode())
}

func getPublicDomain(cfg conf.OSSConfig) string {
	if cfg.CDNDomain != "" {
		return cfg.CDNDomain
	}
	return fmt.Sprintf("%s.%s", cfg.BucketName, cfg.Endpoint)
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
