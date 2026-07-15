package media

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/zhaojiabo/bobobeads_server/conf"
)

func TestOSSPresignPutUsesV4Signature(t *testing.T) {
	cfg := conf.OSSConfig{
		Endpoint:        "https://oss-cn-beijing.aliyuncs.com",
		Region:          "cn-beijing",
		AccessKeyID:     "test-access-key",
		AccessKeySecret: "test-access-secret",
		Bucket:          "pinto-test",
	}

	storage, err := NewOSSStorage(cfg)
	if err != nil {
		t.Fatalf("NewOSSStorage: %v", err)
	}
	presigned, err := storage.PresignPut(context.Background(), "original/2026/image.png", "image/png", 30*time.Minute)
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}
	parsedURL, err := url.Parse(presigned.URL)
	if err != nil {
		t.Fatalf("parse presigned URL: %v", err)
	}

	if parsedURL.Host != "pinto-test.oss-cn-beijing.aliyuncs.com" {
		t.Errorf("host = %q", parsedURL.Host)
	}
	if parsedURL.Path != "/original/2026/image.png" {
		t.Errorf("path = %q", parsedURL.Path)
	}
	query := parsedURL.Query()
	if len(query) == 0 {
		t.Fatal("expected signed OSS query parameters")
	}
	if !strings.Contains(presigned.URL, "OSS4-HMAC-SHA256") {
		t.Errorf("expected OSS V4 signature, URL = %q", presigned.URL)
	}
	if !presigned.ExpiresAt.After(time.Now()) {
		t.Errorf("expires at = %v", presigned.ExpiresAt)
	}
}

func TestOSSPublicURLUsesConfiguredDomain(t *testing.T) {
	cfg := conf.OSSConfig{
		Endpoint:        "https://oss-cn-beijing.aliyuncs.com",
		Region:          "cn-beijing",
		AccessKeyID:     "test-access-key",
		AccessKeySecret: "test-access-secret",
		Bucket:          "pinto-test",
		PublicBaseURL:   "https://cdn.appbobo.com/",
	}

	storage, err := NewOSSStorage(cfg)
	if err != nil {
		t.Fatalf("NewOSSStorage: %v", err)
	}
	got := storage.PublicURL("avatar/2026/profile photo.png")
	want := "https://cdn.appbobo.com/avatar/2026/profile%20photo.png"
	if got != want {
		t.Errorf("public URL = %q, want %q", got, want)
	}
}
