package test

import (
	"context"
	"testing"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/service/credit"
	"github.com/zhaojiabo/bobobeads_server/internal/service/generation"
	"github.com/zhaojiabo/bobobeads_server/internal/service/media"
	"github.com/zhaojiabo/bobobeads_server/internal/service/subscribe"
	"github.com/zhaojiabo/bobobeads_server/internal/service/work"
)

func setupGenerationServiceWithThumbnails(t *testing.T) (*generation.Service, *media.Service, *memoryObjectStorage) {
	t.Helper()
	SetupTestDB(t)

	storage := newMemoryObjectStorage("https://cdn.example.test")
	mediaSvc := media.NewServiceWithStorage(dao.NewMediaDAO(), storage)
	svc := generation.NewService(
		dao.NewGenerationDAO(),
		credit.NewService(dao.NewCreditDAO()),
		subscribe.NewService(dao.NewOrderDAO(), dao.NewProductDAO(), dao.NewSubscriptionDAO()),
		work.NewService(dao.NewWorkDAO()),
		mediaSvc,
	)
	return svc, mediaSvc, storage
}

// uploadPatternImage walks the real upload flow and stores decodable bytes, so
// the thumbnail path can download and re-encode the object like it does in prod.
func uploadPatternImage(t *testing.T, mediaSvc *media.Service, storage *memoryObjectStorage, userID uint64, fileName string) string {
	t.Helper()
	ctx := context.Background()
	token, err := mediaSvc.GetUploadToken(ctx, userID, fileName, "image/png", "pattern")
	if err != nil {
		t.Fatalf("GetUploadToken: %v", err)
	}
	storage.put(token.FileKey, "image/png", pngBytes(t, 900, 600))
	if _, err := mediaSvc.ReportUpload(ctx, userID, token.FileKey, 1024); err != nil {
		t.Fatalf("ReportUpload: %v", err)
	}
	return token.FileKey
}

func completeGenerationForThumbnail(t *testing.T, svc *generation.Service, userID uint64, clientRequestID string, workData *model.Work) *model.Work {
	t.Helper()
	ctx := context.Background()
	created, err := svc.CreateGeneration(ctx, userID, "29x29", "photo", "", clientRequestID)
	if err != nil {
		t.Fatalf("CreateGeneration: %v", err)
	}
	result, err := svc.CompleteGeneration(ctx, userID, created.GenerationID, workData)
	if err != nil {
		t.Fatalf("CompleteGeneration: %v", err)
	}
	var saved model.Work
	if err := db.DB.First(&saved, result.WorkID).Error; err != nil {
		t.Fatalf("load work %d: %v", result.WorkID, err)
	}
	return &saved
}

func TestCompleteGeneration_ThumbnailUsesClientSourceImage(t *testing.T) {
	svc, mediaSvc, storage := setupGenerationServiceWithThumbnails(t)
	userID := uint64(1)

	patternKey := uploadPatternImage(t, mediaSvc, storage, userID, "pattern.png")
	// The client's thumbnail_url is the bare pattern without the bottom colour
	// swatches, so it must win over pattern_image_url as the source image.
	bareKey := uploadPatternImage(t, mediaSvc, storage, userID, "bare.png")

	workData := completedWorkData("client source")
	workData.PatternImageURL = storage.PublicURL(patternKey)
	workData.ThumbnailURL = storage.PublicURL(bareKey)

	saved := completeGenerationForThumbnail(t, svc, userID, "req-client-source", workData)
	want := storage.PublicURL(media.ThumbnailFileKey(bareKey))
	if saved.ThumbnailURL != want {
		t.Errorf("thumbnailUrl = %q, want %q", saved.ThumbnailURL, want)
	}
}

func TestCompleteGeneration_ThumbnailFallsBackToPatternImage(t *testing.T) {
	svc, mediaSvc, storage := setupGenerationServiceWithThumbnails(t)
	userID := uint64(1)
	patternKey := uploadPatternImage(t, mediaSvc, storage, userID, "pattern.png")

	cases := map[string]string{
		// Older clients send no source at all.
		"empty":   "",
		"blank":   "   ",
		"foreign": "https://evil.example.com/whatever.png",
	}
	for name, clientSource := range cases {
		t.Run(name, func(t *testing.T) {
			workData := completedWorkData("fallback")
			workData.PatternImageURL = storage.PublicURL(patternKey)
			workData.ThumbnailURL = clientSource

			saved := completeGenerationForThumbnail(t, svc, userID, "req-fallback-"+name, workData)
			want := storage.PublicURL(media.ThumbnailFileKey(patternKey))
			if saved.ThumbnailURL != want {
				t.Errorf("thumbnailUrl = %q, want %q", saved.ThumbnailURL, want)
			}
		})
	}
}

func TestCompleteGeneration_SucceedsWhenThumbnailFails(t *testing.T) {
	svc, _, storage := setupGenerationServiceWithThumbnails(t)
	userID := uint64(1)

	// No uploaded asset behind either URL: the thumbnail must be skipped rather
	// than fail the generation, and the client falls back to the full-size image.
	workData := completedWorkData("no asset")
	workData.PatternImageURL = storage.PublicURL("pattern/missing.png")
	workData.ThumbnailURL = storage.PublicURL("pattern/missing-bare.png")

	saved := completeGenerationForThumbnail(t, svc, userID, "req-no-asset", workData)
	if saved.ThumbnailURL != "" {
		t.Errorf("thumbnailUrl = %q, want empty", saved.ThumbnailURL)
	}
	if saved.PatternImageURL != workData.PatternImageURL {
		t.Errorf("patternImageUrl = %q, want %q", saved.PatternImageURL, workData.PatternImageURL)
	}
}
