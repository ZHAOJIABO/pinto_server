package media

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	PresignPublicPut(ctx context.Context, fileKey, contentType string, expires time.Duration) (*PresignedUpload, error)
	PutPublic(ctx context.Context, fileKey, contentType string, content []byte) error
	Get(ctx context.Context, fileKey string, maxSize int64) ([]byte, string, error)
	PublicURL(fileKey string) string
	// FileKeyFromPublicURL inverts PublicURL. It reports false for URLs that do
	// not belong to this bucket, so callers cannot be tricked into treating a
	// foreign URL as one of our objects.
	FileKeyFromPublicURL(publicURL string) (string, bool)
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

// PresignPublicPut creates a browser upload grant that makes the uploaded
// object publicly readable. The bucket itself remains private.
func (s *ossStorage) PresignPublicPut(ctx context.Context, fileKey, contentType string, expires time.Duration) (*PresignedUpload, error) {
	request := &oss.PutObjectRequest{
		Bucket:      oss.Ptr(s.bucket),
		Key:         oss.Ptr(fileKey),
		ContentType: oss.Ptr(contentType),
		Acl:         oss.ObjectACLPublicRead,
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

// PutPublic uploads a public-read object through the application server.
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

// Get reads an object. maxSize caps how much is buffered so a malicious or
// unexpectedly large object cannot exhaust process memory.
func (s *ossStorage) Get(ctx context.Context, fileKey string, maxSize int64) ([]byte, string, error) {
	result, err := s.client.GetObject(ctx, &oss.GetObjectRequest{
		Bucket: oss.Ptr(s.bucket),
		Key:    oss.Ptr(fileKey),
	})
	if err != nil {
		return nil, "", err
	}
	defer result.Body.Close()

	content, err := io.ReadAll(io.LimitReader(result.Body, maxSize+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(content)) > maxSize {
		return nil, "", fmt.Errorf("object %s exceeds %d bytes", fileKey, maxSize)
	}

	contentType := ""
	if result.ContentType != nil {
		contentType = *result.ContentType
	}
	return content, contentType, nil
}

func (s *ossStorage) PublicURL(fileKey string) string {
	if s.publicBaseURL == "" {
		return ""
	}
	return s.publicBaseURL + "/" + escapeObjectKey(fileKey)
}

func (s *ossStorage) FileKeyFromPublicURL(publicURL string) (string, bool) {
	return fileKeyFromPublicURL(s.publicBaseURL, publicURL)
}

func fileKeyFromPublicURL(publicBaseURL, publicURL string) (string, bool) {
	publicURL = strings.TrimSpace(publicURL)
	if publicBaseURL == "" || publicURL == "" {
		return "", false
	}
	// Query strings and fragments are not part of the object key.
	if index := strings.IndexAny(publicURL, "?#"); index >= 0 {
		publicURL = publicURL[:index]
	}
	escapedKey, found := strings.CutPrefix(publicURL, publicBaseURL+"/")
	if !found || escapedKey == "" {
		return "", false
	}
	fileKey, err := url.PathUnescape(escapedKey)
	if err != nil {
		return "", false
	}
	return fileKey, true
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
