package media

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	oss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
	"github.com/zhaojiabo/bobobeads_server/conf"
)

// PresignedUpload is the browser-safe portion of an OSS upload grant. It does
// not include server credentials.
type PresignedUpload struct {
	URL       string
	Headers   map[string]string
	ExpiresAt time.Time
}

// ObjectStorage keeps media-service behaviour independent from the cloud
// vendor client and permits deterministic tests without a network connection.
type ObjectStorage interface {
	PresignPut(ctx context.Context, fileKey, contentType string, expires time.Duration) (*PresignedUpload, error)
	PresignPublicPut(ctx context.Context, fileKey, contentType string, expires time.Duration) (*PresignedUpload, error)
	PutPublic(ctx context.Context, fileKey, contentType string, content []byte) error
	PublicURL(fileKey string) string
}

type ossStorage struct {
	client        *oss.Client
	bucket        string
	publicBaseURL string
}

func NewOSSStorage(cfg conf.OSSConfig) (ObjectStorage, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	region := strings.TrimSpace(cfg.Region)
	bucket := strings.TrimSpace(cfg.Bucket)
	if endpoint == "" || region == "" || bucket == "" {
		return nil, fmt.Errorf("OSS endpoint, region, and bucket are required")
	}
	if strings.TrimSpace(cfg.AccessKeyID) == "" || strings.TrimSpace(cfg.AccessKeySecret) == "" {
		return nil, fmt.Errorf("OSS access key id and secret are required")
	}

	clientConfig := oss.LoadDefaultConfig().
		WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.AccessKeySecret)).
		WithRegion(region).
		WithEndpoint(endpoint).
		WithSignatureVersion(oss.SignatureVersionV4).
		WithAdditionalHeaders([]string{"x-oss-object-acl"}).
		WithHttpClient(&http.Client{Timeout: 30 * time.Second})

	return &ossStorage{
		client:        oss.NewClient(clientConfig),
		bucket:        bucket,
		publicBaseURL: strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/"),
	}, nil
}

func (s *ossStorage) PresignPut(ctx context.Context, fileKey, contentType string, expires time.Duration) (*PresignedUpload, error) {
	return s.presignPut(ctx, fileKey, contentType, expires, false)
}

// PresignPublicPut creates a browser upload grant that makes only the uploaded
// object publicly readable. The bucket itself remains private.
func (s *ossStorage) PresignPublicPut(ctx context.Context, fileKey, contentType string, expires time.Duration) (*PresignedUpload, error) {
	return s.presignPut(ctx, fileKey, contentType, expires, true)
}

func (s *ossStorage) presignPut(ctx context.Context, fileKey, contentType string, expires time.Duration, publicRead bool) (*PresignedUpload, error) {
	request := &oss.PutObjectRequest{
		Bucket:      oss.Ptr(s.bucket),
		Key:         oss.Ptr(fileKey),
		ContentType: oss.Ptr(contentType),
	}
	if publicRead {
		request.Acl = oss.ObjectACLPublicRead
	}

	result, err := s.client.Presign(
		ctx,
		request,
		oss.PresignExpires(expires),
	)
	if err != nil {
		return nil, err
	}
	return &PresignedUpload{
		URL:       result.URL,
		Headers:   result.SignedHeaders,
		ExpiresAt: result.Expiration,
	}, nil
}

// PutPublic uploads an official-template preview with public-read ACL. Other
// uploads use their separate presigned path and inherit the private bucket ACL.
func (s *ossStorage) PutPublic(ctx context.Context, fileKey, contentType string, content []byte) error {
	contentLength := int64(len(content))
	_, err := s.client.PutObject(ctx, &oss.PutObjectRequest{
		Bucket:        oss.Ptr(s.bucket),
		Key:           oss.Ptr(fileKey),
		ContentType:   oss.Ptr(contentType),
		ContentLength: oss.Ptr(contentLength),
		Acl:           oss.ObjectACLPublicRead,
		Body:          bytes.NewReader(content),
	})
	return err
}

func (s *ossStorage) PublicURL(fileKey string) string {
	if s.publicBaseURL == "" {
		return ""
	}
	return s.publicBaseURL + "/" + escapeObjectKey(fileKey)
}

func escapeObjectKey(fileKey string) string {
	parts := strings.Split(fileKey, "/")
	for index, part := range parts {
		parts[index] = urlPathEscape(part)
	}
	return strings.Join(parts, "/")
}

func urlPathEscape(value string) string {
	const safe = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.~"
	var builder strings.Builder
	for _, char := range []byte(value) {
		if strings.ContainsRune(safe, rune(char)) {
			builder.WriteByte(char)
			continue
		}
		fmt.Fprintf(&builder, "%%%02X", char)
	}
	return builder.String()
}
