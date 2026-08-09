package test

import (
	"context"
	"testing"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/service/finishedproduct"
	"github.com/zhaojiabo/bobobeads_server/internal/service/media"
)

func newFinishedProductService(t *testing.T) (*finishedproduct.Service, *media.Service) {
	t.Helper()
	mediaSvc := media.NewServiceWithStorage(dao.NewMediaDAO(), newMemoryObjectStorage("https://cdn.example.test"))
	return finishedproduct.NewService(dao.NewFinishedProductDAO(), mediaSvc), mediaSvc
}

// uploadFinishedProductFile walks the real upload flow so tests exercise the
// same asset state the App produces.
func uploadFinishedProductFile(t *testing.T, mediaSvc *media.Service, userID uint64) string {
	t.Helper()
	token, err := mediaSvc.GetUploadToken(context.Background(), userID, "finished.jpg", "image/jpeg", media.PurposeFinishedProduct)
	if err != nil {
		t.Fatalf("GetUploadToken: %v", err)
	}
	if _, err := mediaSvc.ReportUpload(context.Background(), userID, token.FileKey, 1024); err != nil {
		t.Fatalf("ReportUpload: %v", err)
	}
	return token.FileKey
}

func assertErrCode(t *testing.T, err error, want int32) {
	t.Helper()
	appErr, ok := apperr.IsAppError(err)
	if !ok {
		t.Fatalf("err = %v, want *apperr.AppError with code %d", err, want)
	}
	if appErr.Code != want {
		t.Fatalf("err code = %d (%s), want %d", appErr.Code, appErr.Message, want)
	}
}

func TestFinishedProduct_CreateIsIdempotentOnClientRequestID(t *testing.T) {
	SetupTestDB(t)
	svc, mediaSvc := newFinishedProductService(t)
	ctx := context.Background()
	userID := uint64(1)
	fileKey := uploadFinishedProductFile(t, mediaSvc, userID)

	first, err := svc.Create(ctx, userID, fileKey, "finished-1")
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if first.ImageURL != "https://cdn.example.test/"+fileKey {
		t.Errorf("ImageURL = %q, want the public URL of %q", first.ImageURL, fileKey)
	}

	second, err := svc.Create(ctx, userID, fileKey, "finished-1")
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("second Create id = %d, want %d", second.ID, first.ID)
	}

	var count int64
	db.DB.Model(&model.FinishedProduct{}).Where("user_id = ?", userID).Count(&count)
	if count != 1 {
		t.Errorf("row count = %d, want 1", count)
	}
}

func TestFinishedProduct_CreateReusesRowForSameMediaKey(t *testing.T) {
	SetupTestDB(t)
	svc, mediaSvc := newFinishedProductService(t)
	ctx := context.Background()
	userID := uint64(1)
	fileKey := uploadFinishedProductFile(t, mediaSvc, userID)

	first, err := svc.Create(ctx, userID, fileKey, "finished-1")
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}

	// Different idempotency key, same file: return the existing record rather
	// than binding one upload to two products.
	second, err := svc.Create(ctx, userID, fileKey, "finished-2")
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("second Create id = %d, want %d", second.ID, first.ID)
	}
}

func TestFinishedProduct_CreateRejectsForeignAndInvalidMedia(t *testing.T) {
	SetupTestDB(t)
	svc, mediaSvc := newFinishedProductService(t)
	ctx := context.Background()
	owner := uint64(1)
	other := uint64(2)

	t.Run("other user's file", func(t *testing.T) {
		fileKey := uploadFinishedProductFile(t, mediaSvc, owner)
		_, err := svc.Create(ctx, other, fileKey, "finished-foreign")
		assertErrCode(t, err, apperr.CodeForbidden)
	})

	t.Run("unknown file key", func(t *testing.T) {
		_, err := svc.Create(ctx, owner, "finished_product/1/missing.jpg", "finished-missing")
		assertErrCode(t, err, apperr.CodeNotFound)
	})

	t.Run("upload not reported", func(t *testing.T) {
		token, err := mediaSvc.GetUploadToken(ctx, owner, "pending.jpg", "image/jpeg", media.PurposeFinishedProduct)
		if err != nil {
			t.Fatalf("GetUploadToken: %v", err)
		}
		_, err = svc.Create(ctx, owner, token.FileKey, "finished-pending")
		assertErrCode(t, err, apperr.CodeInvalidArgument)
	})

	t.Run("purpose mismatch", func(t *testing.T) {
		token, err := mediaSvc.GetUploadToken(ctx, owner, "avatar.jpg", "image/jpeg", "avatar")
		if err != nil {
			t.Fatalf("GetUploadToken: %v", err)
		}
		if _, err := mediaSvc.ReportUpload(ctx, owner, token.FileKey, 1024); err != nil {
			t.Fatalf("ReportUpload: %v", err)
		}
		_, err = svc.Create(ctx, owner, token.FileKey, "finished-avatar")
		assertErrCode(t, err, apperr.CodeInvalidArgument)
	})

	t.Run("missing arguments", func(t *testing.T) {
		_, err := svc.Create(ctx, owner, "", "finished-empty-key")
		assertErrCode(t, err, apperr.CodeInvalidArgument)
		_, err = svc.Create(ctx, owner, "finished_product/1/a.jpg", "")
		assertErrCode(t, err, apperr.CodeInvalidArgument)
	})
}

func TestFinishedProduct_ListPagesWithoutGapsOrRepeats(t *testing.T) {
	SetupTestDB(t)
	svc, mediaSvc := newFinishedProductService(t)
	ctx := context.Background()
	userID := uint64(1)

	created := make([]uint64, 0, 5)
	for i := 0; i < 5; i++ {
		fileKey := uploadFinishedProductFile(t, mediaSvc, userID)
		fp, err := svc.Create(ctx, userID, fileKey, "finished-"+fileKey)
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
		created = append(created, fp.ID)
	}
	// Newest first is the reverse of creation order.
	want := make([]uint64, 0, len(created))
	for i := len(created) - 1; i >= 0; i-- {
		want = append(want, created[i])
	}

	got := make([]uint64, 0, len(want))
	cursor := ""
	for page := 0; ; page++ {
		if page > len(want) {
			t.Fatal("pagination did not terminate")
		}
		items, next, err := svc.List(ctx, userID, 2, cursor)
		if err != nil {
			t.Fatalf("List page %d: %v", page, err)
		}
		for _, item := range items {
			got = append(got, item.ID)
		}
		if next == "" {
			break
		}
		cursor = next
	}

	if len(got) != len(want) {
		t.Fatalf("paged ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("paged ids = %v, want %v", got, want)
		}
	}
}

func TestFinishedProduct_ListIsolatesUsersAndValidatesCursor(t *testing.T) {
	SetupTestDB(t)
	svc, mediaSvc := newFinishedProductService(t)
	ctx := context.Background()

	ownerKey := uploadFinishedProductFile(t, mediaSvc, 1)
	if _, err := svc.Create(ctx, 1, ownerKey, "finished-owner"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	items, next, err := svc.List(ctx, 2, 12, "")
	if err != nil {
		t.Fatalf("List other user: %v", err)
	}
	if len(items) != 0 || next != "" {
		t.Errorf("other user got %d items and cursor %q, want none", len(items), next)
	}

	for _, cursor := range []string{"not-base64!!", "MA", "YWJj"} {
		if _, _, err := svc.List(ctx, 1, 12, cursor); err == nil {
			t.Errorf("List with cursor %q succeeded, want invalid-argument", cursor)
		} else {
			assertErrCode(t, err, apperr.CodeInvalidArgument)
		}
	}
}

func TestFinishedProduct_CreateStoresServerGeneratedThumbnail(t *testing.T) {
	SetupTestDB(t)
	storage := newMemoryObjectStorage("https://cdn.example.test")
	mediaSvc := media.NewServiceWithStorage(dao.NewMediaDAO(), storage)
	svc := finishedproduct.NewService(dao.NewFinishedProductDAO(), mediaSvc)
	userID := uint64(1)

	fileKey := uploadFinishedProductFile(t, mediaSvc, userID)
	storage.put(fileKey, "image/jpeg", pngBytes(t, 1200, 900))

	fp, err := svc.Create(context.Background(), userID, fileKey, "finished-thumb")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	want := "https://cdn.example.test/" + media.ThumbnailFileKey(fileKey)
	if fp.ThumbnailURL != want {
		t.Fatalf("ThumbnailURL = %q, want %q", fp.ThumbnailURL, want)
	}
}

func TestFinishedProduct_CreateSucceedsWhenThumbnailFails(t *testing.T) {
	SetupTestDB(t)
	svc, mediaSvc := newFinishedProductService(t)
	userID := uint64(1)

	// No object bytes were stored, so thumbnail generation cannot succeed. The
	// user still paid for the upload, so the product must be saved anyway.
	fileKey := uploadFinishedProductFile(t, mediaSvc, userID)
	fp, err := svc.Create(context.Background(), userID, fileKey, "finished-no-thumb")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if fp.ThumbnailURL != "" {
		t.Errorf("ThumbnailURL = %q, want empty", fp.ThumbnailURL)
	}
	if fp.ImageURL == "" {
		t.Error("ImageURL must still be stored when the thumbnail fails")
	}
}
