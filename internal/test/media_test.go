package test

import (
	"context"
	"testing"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/service/media"
)

func TestMedia_StyleInputPurpose(t *testing.T) {
	SetupTestDB(t)
	mediaDAO := dao.NewMediaDAO()
	svc := media.NewServiceWithStorage(mediaDAO, newMemoryObjectStorage("https://cdn.example.test"))

	ctx := context.Background()
	userID := uint64(1)

	// style_input with valid type should succeed
	token, err := svc.GetUploadToken(ctx, userID, "input.png", "image/png", "style_input")
	if err != nil {
		t.Fatalf("GetUploadToken style_input png failed: %v", err)
	}
	if token.FileKey == "" {
		t.Error("expected non-empty file_key")
	}

	t.Log("style_input purpose with image/png success")
}

func TestMedia_AllPurposesCreatePublicReadUploads(t *testing.T) {
	SetupTestDB(t)
	mediaDAO := dao.NewMediaDAO()
	svc := media.NewServiceWithStorage(mediaDAO, newMemoryObjectStorage("https://cdn.example.test"))

	for purpose, contentType := range map[string]string{
		"original":      "image/png",
		"pattern":       "image/png",
		"avatar":        "image/png",
		"feedback":      "image/png",
		"style_input":   "image/png",
		"ai_output":     "image/png",
		"admin_preview": "image/png",

		media.PurposeFinishedProduct: "image/jpeg",
	} {
		t.Run(purpose, func(t *testing.T) {
			token, err := svc.GetUploadToken(context.Background(), 1, "image.png", contentType, purpose)
			if err != nil {
				t.Fatalf("GetUploadToken: %v", err)
			}
			if got := token.Headers["X-Oss-Object-Acl"]; got != "public-read" {
				t.Errorf("X-Oss-Object-Acl = %q, want public-read", got)
			}
			if want := "https://cdn.example.test/" + token.FileKey; token.PublicURL != want {
				t.Errorf("public URL = %q, want %q", token.PublicURL, want)
			}
		})
	}
}

func TestMedia_GetUploadTokenPersistsPublicURL(t *testing.T) {
	SetupTestDB(t)
	mediaDAO := dao.NewMediaDAO()
	svc := media.NewServiceWithStorage(mediaDAO, newMemoryObjectStorage("https://cdn.example.test"))

	token, err := svc.GetUploadToken(context.Background(), 1, "input.png", "image/png", "style_input")
	if err != nil {
		t.Fatalf("GetUploadToken: %v", err)
	}

	// AIGeneration copies this column into input_image_url at submit time, so an
	// empty FileURL silently blanks the original image in every task response.
	var asset model.MediaAsset
	if err := db.DB.Where("file_key = ?", token.FileKey).First(&asset).Error; err != nil {
		t.Fatalf("load asset: %v", err)
	}
	if want := "https://cdn.example.test/" + token.FileKey; asset.FileURL != want {
		t.Errorf("stored FileURL = %q, want %q", asset.FileURL, want)
	}
}

func TestMedia_GetFileURLReturnsStablePublicURL(t *testing.T) {
	svc := media.NewServiceWithStorage(dao.NewMediaDAO(), newMemoryObjectStorage("https://img.appbobo.cn"))

	url, expiresAt, err := svc.GetFileURL(context.Background(), "original/2026/example.png")
	if err != nil {
		t.Fatalf("GetFileURL: %v", err)
	}
	if want := "https://img.appbobo.cn/original/2026/example.png"; url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
	if expiresAt != 0 {
		t.Errorf("expiresAt = %d, want 0 for a stable public URL", expiresAt)
	}
}

func TestMedia_StyleInputInvalidType(t *testing.T) {
	SetupTestDB(t)
	mediaDAO := dao.NewMediaDAO()
	svc := media.NewServiceWithStorage(mediaDAO, newMemoryObjectStorage("https://cdn.example.test"))

	ctx := context.Background()
	userID := uint64(1)

	// style_input with pdf should fail
	_, err := svc.GetUploadToken(ctx, userID, "doc.pdf", "application/pdf", "style_input")
	if err == nil {
		t.Error("expected error for style_input with application/pdf")
	}
	t.Log("style_input rejects application/pdf correctly")
}

func TestMedia_AIOutputInvalidType(t *testing.T) {
	SetupTestDB(t)
	mediaDAO := dao.NewMediaDAO()
	svc := media.NewServiceWithStorage(mediaDAO, newMemoryObjectStorage("https://cdn.example.test"))

	ctx := context.Background()
	userID := uint64(1)

	// ai_output with heic should fail
	_, err := svc.GetUploadToken(ctx, userID, "output.heic", "image/heic", "ai_output")
	if err == nil {
		t.Error("expected error for ai_output with image/heic")
	}

	// ai_output with png should succeed
	token, err := svc.GetUploadToken(ctx, userID, "output.png", "image/png", "ai_output")
	if err != nil {
		t.Fatalf("GetUploadToken ai_output png failed: %v", err)
	}
	if token.FileKey == "" {
		t.Error("expected non-empty file_key")
	}

	t.Log("ai_output type validation success")
}

func TestMedia_UploadedAssetValidation(t *testing.T) {
	SetupTestDB(t)
	mediaDAO := dao.NewMediaDAO()

	ctx := context.Background()
	userID := uint64(1)

	// Create an uploaded asset
	asset := &model.MediaAsset{
		UserID:      userID,
		FileKey:     "style_input/2024/test-key",
		Purpose:     "style_input",
		ContentType: "image/png",
		Status:      model.MediaStatusUploaded,
	}
	db.DB.Create(asset)

	// Should find for correct user/purpose
	found, err := mediaDAO.GetUploadedAsset(ctx, "style_input/2024/test-key", userID, "style_input")
	if err != nil {
		t.Fatalf("GetUploadedAsset failed: %v", err)
	}
	if found == nil {
		t.Error("expected to find uploaded asset")
	}

	// Wrong user should not find
	found2, err := mediaDAO.GetUploadedAsset(ctx, "style_input/2024/test-key", 999, "style_input")
	if err != nil {
		t.Fatalf("GetUploadedAsset other user failed: %v", err)
	}
	if found2 != nil {
		t.Error("should not find asset for wrong user")
	}

	// Wrong purpose should not find
	found3, err := mediaDAO.GetUploadedAsset(ctx, "style_input/2024/test-key", userID, "original")
	if err != nil {
		t.Fatalf("GetUploadedAsset wrong purpose failed: %v", err)
	}
	if found3 != nil {
		t.Error("should not find asset for wrong purpose")
	}

	// Pending asset should not be found
	pendingAsset := &model.MediaAsset{
		UserID:      userID,
		FileKey:     "style_input/2024/pending-key",
		Purpose:     "style_input",
		ContentType: "image/png",
		Status:      model.MediaStatusPending,
	}
	db.DB.Create(pendingAsset)
	found4, _ := mediaDAO.GetUploadedAsset(ctx, "style_input/2024/pending-key", userID, "style_input")
	if found4 != nil {
		t.Error("should not find pending asset")
	}

	t.Log("Uploaded asset validation success")
}
