package test

import (
	"context"
	"strings"
	"testing"

	"github.com/zhaojiabo/bobobeads_server/conf"
	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/service/media"
	templateservice "github.com/zhaojiabo/bobobeads_server/internal/service/template"
	"github.com/zhaojiabo/bobobeads_server/internal/service/templatesubmission"
	"github.com/zhaojiabo/bobobeads_server/internal/service/work"
)

// newSubmissionService returns the service plus the media service backed by the
// same storage, so tests can register work images that count as ours.
func newSubmissionService(t *testing.T, dailyLimit int) (*templatesubmission.Service, *media.Service, *memoryObjectStorage) {
	t.Helper()
	previousConfig := conf.GlobalConfig
	conf.GlobalConfig = &conf.Config{
		Pattern: conf.PatternConfig{MaxWidth: 200, MaxHeight: 200, MaxPixels: 40000, MaxColors: 221},
	}
	t.Cleanup(func() { conf.GlobalConfig = previousConfig })

	storage := newMemoryObjectStorage("https://cdn.example.test")
	mediaSvc := media.NewServiceWithStorage(dao.NewMediaDAO(), storage)
	templateDAO := dao.NewTemplateDAO()
	service := templatesubmission.NewService(
		dao.NewTemplateSubmissionDAO(), dao.NewWorkDAO(), dao.NewUserDAO(),
		mediaSvc, templateservice.NewAdminService(templateDAO, dao.NewBlindBoxPoolDAO()), dailyLimit,
	)
	return service, mediaSvc, storage
}

// createSubmittableWork stores a work the same way SaveWork does, so the snapshot
// under test is taken from a realistic row.
func createSubmittableWork(t *testing.T, userID uint64, patternImageURL string) *model.Work {
	t.Helper()
	w := &model.Work{
		UserID:           userID,
		Title:            "我的作品",
		OriginalImageURL: patternImageURL,
		PatternImageURL:  patternImageURL,
	}
	if err := work.ApplyPatternData(w, validPatternData(4, 4)); err != nil {
		t.Fatalf("ApplyPatternData: %v", err)
	}
	if err := dao.NewWorkDAO().Create(context.Background(), w); err != nil {
		t.Fatalf("create work: %v", err)
	}
	return w
}

func TestTemplateSubmission_SubmitSnapshotsWorkFields(t *testing.T) {
	SetupTestDB(t)
	service, _, _ := newSubmissionService(t, 5)
	w := createSubmittableWork(t, 7, "https://cdn.example.test/work/2026/07/15/7/pattern.png")

	sub, err := service.Submit(context.Background(), 7, templatesubmission.SubmitInput{
		WorkID:          w.ID,
		Title:           "小猫",
		Description:     "两色拼豆",
		ClientRequestID: "req-1",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if sub.Status != model.TemplateSubmissionStatusPending {
		t.Errorf("status = %d, want pending", sub.Status)
	}
	if sub.BoardSpec != w.BoardSpec || sub.Width != w.Width || sub.Height != w.Height {
		t.Errorf("snapshot dimensions = %s %dx%d, want %s %dx%d",
			sub.BoardSpec, sub.Width, sub.Height, w.BoardSpec, w.Width, w.Height)
	}
	if sub.BeadCount != w.BeadCount || sub.ColorCount != w.ColorCount {
		t.Errorf("snapshot stats = %d beads / %d colors, want %d / %d",
			sub.BeadCount, sub.ColorCount, w.BeadCount, w.ColorCount)
	}
	if sub.PatternData == nil {
		t.Error("snapshot is missing pattern data")
	}
	if sub.PreviewURL != w.PatternImageURL {
		t.Errorf("previewUrl = %q, want the work image %q", sub.PreviewURL, w.PatternImageURL)
	}
	if sub.ActiveWorkKey == nil {
		t.Error("active_work_key must be set while the submission is pending")
	}
}

func TestTemplateSubmission_SubmitIsIdempotentOnClientRequestID(t *testing.T) {
	SetupTestDB(t)
	service, _, _ := newSubmissionService(t, 5)
	w := createSubmittableWork(t, 7, "https://cdn.example.test/work/pattern.png")

	in := templatesubmission.SubmitInput{WorkID: w.ID, Title: "小猫", ClientRequestID: "req-replay"}
	first, err := service.Submit(context.Background(), 7, in)
	if err != nil {
		t.Fatalf("first Submit: %v", err)
	}
	second, err := service.Submit(context.Background(), 7, in)
	if err != nil {
		t.Fatalf("replayed Submit: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("replay created submission %d, want %d", second.ID, first.ID)
	}

	var count int64
	db.DB.Model(&model.TemplateSubmission{}).Count(&count)
	if count != 1 {
		t.Errorf("submission rows = %d, want 1", count)
	}
}

func TestTemplateSubmission_SubmitRejectsForeignWork(t *testing.T) {
	SetupTestDB(t)
	service, _, _ := newSubmissionService(t, 5)
	w := createSubmittableWork(t, 7, "https://cdn.example.test/work/pattern.png")

	_, err := service.Submit(context.Background(), 8, templatesubmission.SubmitInput{
		WorkID: w.ID, Title: "小猫", ClientRequestID: "req-foreign",
	})
	assertErrCode(t, err, apperr.CodeNotFound)
}

func TestTemplateSubmission_SubmitRejectsWorkWithoutPatternData(t *testing.T) {
	SetupTestDB(t)
	service, _, _ := newSubmissionService(t, 5)
	w := &model.Work{UserID: 7, Title: "空作品"}
	if err := dao.NewWorkDAO().Create(context.Background(), w); err != nil {
		t.Fatalf("create work: %v", err)
	}

	_, err := service.Submit(context.Background(), 7, templatesubmission.SubmitInput{
		WorkID: w.ID, Title: "小猫", ClientRequestID: "req-nopattern",
	})
	assertErrCode(t, err, apperr.CodeInvalidArgument)
}

func TestTemplateSubmission_SubmitValidatesTitleAndDescription(t *testing.T) {
	SetupTestDB(t)
	service, _, _ := newSubmissionService(t, 5)
	w := createSubmittableWork(t, 7, "https://cdn.example.test/work/pattern.png")

	cases := []struct {
		name  string
		input templatesubmission.SubmitInput
	}{
		{"empty title", templatesubmission.SubmitInput{WorkID: w.ID, Title: "  ", ClientRequestID: "a"}},
		{"missing client request id", templatesubmission.SubmitInput{WorkID: w.ID, Title: "小猫"}},
		{"title too long", templatesubmission.SubmitInput{WorkID: w.ID, Title: strings.Repeat("猫", 41), ClientRequestID: "b"}},
		{"description too long", templatesubmission.SubmitInput{WorkID: w.ID, Title: "小猫", Description: strings.Repeat("说", 201), ClientRequestID: "c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.Submit(context.Background(), 7, tc.input)
			assertErrCode(t, err, apperr.CodeInvalidArgument)
		})
	}
}

func TestTemplateSubmission_SubmitRejectsSecondActiveSubmissionForSameWork(t *testing.T) {
	SetupTestDB(t)
	service, _, _ := newSubmissionService(t, 5)
	w := createSubmittableWork(t, 7, "https://cdn.example.test/work/pattern.png")

	if _, err := service.Submit(context.Background(), 7, templatesubmission.SubmitInput{
		WorkID: w.ID, Title: "小猫", ClientRequestID: "req-1",
	}); err != nil {
		t.Fatalf("first Submit: %v", err)
	}
	_, err := service.Submit(context.Background(), 7, templatesubmission.SubmitInput{
		WorkID: w.ID, Title: "小猫", ClientRequestID: "req-2",
	})
	assertErrCode(t, err, apperr.CodeDuplicateRequest)
}

func TestTemplateSubmission_SubmitAllowedAgainAfterRejection(t *testing.T) {
	SetupTestDB(t)
	service, _, _ := newSubmissionService(t, 5)
	w := createSubmittableWork(t, 7, "https://cdn.example.test/work/pattern.png")

	first, err := service.Submit(context.Background(), 7, templatesubmission.SubmitInput{
		WorkID: w.ID, Title: "小猫", ClientRequestID: "req-1",
	})
	if err != nil {
		t.Fatalf("first Submit: %v", err)
	}
	if err := service.Reject(context.Background(), first.ID, "operator", "分辨率过低"); err != nil {
		t.Fatalf("Reject: %v", err)
	}

	second, err := service.Submit(context.Background(), 7, templatesubmission.SubmitInput{
		WorkID: w.ID, Title: "小猫 v2", ClientRequestID: "req-2",
	})
	if err != nil {
		t.Fatalf("resubmit after rejection: %v", err)
	}
	if second.ID == first.ID {
		t.Error("resubmission must create a new row")
	}
}

func TestTemplateSubmission_SubmitEnforcesDailyQuota(t *testing.T) {
	SetupTestDB(t)
	service, _, _ := newSubmissionService(t, 2)

	for i := 0; i < 2; i++ {
		w := createSubmittableWork(t, 7, "https://cdn.example.test/work/pattern.png")
		if _, err := service.Submit(context.Background(), 7, templatesubmission.SubmitInput{
			WorkID: w.ID, Title: "小猫", ClientRequestID: "req-" + string(rune('a'+i)),
		}); err != nil {
			t.Fatalf("Submit %d: %v", i, err)
		}
	}

	w := createSubmittableWork(t, 7, "https://cdn.example.test/work/pattern.png")
	_, err := service.Submit(context.Background(), 7, templatesubmission.SubmitInput{
		WorkID: w.ID, Title: "小猫", ClientRequestID: "req-over",
	})
	assertErrCode(t, err, apperr.CodeRateLimited)
}

func TestTemplateSubmission_SubmitReusesWorkThumbnail(t *testing.T) {
	SetupTestDB(t)
	service, _, _ := newSubmissionService(t, 5)
	w := createSubmittableWork(t, 7, "https://cdn.example.test/work/pattern.png")
	w.ThumbnailURL = "https://cdn.example.test/thumbnail/work/pattern.webp"
	if err := dao.NewWorkDAO().Update(context.Background(), w); err != nil {
		t.Fatalf("update work: %v", err)
	}

	sub, err := service.Submit(context.Background(), 7, templatesubmission.SubmitInput{
		WorkID: w.ID, Title: "小猫", ClientRequestID: "req-thumb",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	// Regenerating would need a bb_media row for the pattern image, which this work
	// has none of, so any value other than the reused one means the reuse path broke.
	if sub.ThumbnailURL != w.ThumbnailURL {
		t.Errorf("thumbnailUrl = %q, want the work thumbnail %q", sub.ThumbnailURL, w.ThumbnailURL)
	}
}

func TestTemplateSubmission_SubmitIgnoresForeignHostedPatternImage(t *testing.T) {
	SetupTestDB(t)
	service, _, _ := newSubmissionService(t, 5)
	w := createSubmittableWork(t, 7, "https://evil.example.com/pattern.png")
	w.ThumbnailURL = "https://evil.example.com/pattern_thumb.webp"
	if err := dao.NewWorkDAO().Update(context.Background(), w); err != nil {
		t.Fatalf("update work: %v", err)
	}

	sub, err := service.Submit(context.Background(), 7, templatesubmission.SubmitInput{
		WorkID: w.ID, Title: "小猫", ClientRequestID: "req-foreign-host",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if sub.PreviewURL != "" {
		t.Errorf("previewUrl = %q, want empty for an externally hosted work image", sub.PreviewURL)
	}
	if sub.ThumbnailURL != "" {
		t.Errorf("thumbnailUrl = %q, want empty for an externally hosted work thumbnail", sub.ThumbnailURL)
	}
	if sub.OriginalImageURL != "" {
		t.Errorf("originalImageUrl = %q, want empty", sub.OriginalImageURL)
	}
}

func TestTemplateSubmission_SnapshotUnaffectedByWorkEditAndDelete(t *testing.T) {
	SetupTestDB(t)
	service, _, _ := newSubmissionService(t, 5)
	w := createSubmittableWork(t, 7, "https://cdn.example.test/work/pattern.png")

	sub, err := service.Submit(context.Background(), 7, templatesubmission.SubmitInput{
		WorkID: w.ID, Title: "小猫", ClientRequestID: "req-1",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	originalWidth := sub.Width

	if err := work.ApplyPatternData(w, validPatternData(6, 6)); err != nil {
		t.Fatalf("ApplyPatternData: %v", err)
	}
	if err := dao.NewWorkDAO().Update(context.Background(), w); err != nil {
		t.Fatalf("update work: %v", err)
	}
	if err := dao.NewWorkDAO().Delete(context.Background(), w.ID, 7); err != nil {
		t.Fatalf("delete work: %v", err)
	}

	reloaded, err := service.GetForAdmin(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("GetForAdmin after work deletion: %v", err)
	}
	if reloaded.Width != originalWidth {
		t.Errorf("snapshot width = %d, want %d unchanged by later work edits", reloaded.Width, originalWidth)
	}
	if reloaded.PatternData == nil {
		t.Error("snapshot pattern data must survive deletion of the source work")
	}
}

func TestTemplateSubmission_ListMinePagesWithoutGapsOrRepeats(t *testing.T) {
	SetupTestDB(t)
	service, _, _ := newSubmissionService(t, 10)

	created := make([]uint64, 0, 5)
	for i := 0; i < 5; i++ {
		w := createSubmittableWork(t, 7, "https://cdn.example.test/work/pattern.png")
		sub, err := service.Submit(context.Background(), 7, templatesubmission.SubmitInput{
			WorkID: w.ID, Title: "小猫", ClientRequestID: "req-" + string(rune('a'+i)),
		})
		if err != nil {
			t.Fatalf("Submit %d: %v", i, err)
		}
		created = append(created, sub.ID)
	}

	seen := make([]uint64, 0, 5)
	cursor := ""
	for page := 0; page < 5; page++ {
		items, next, err := service.ListMine(context.Background(), 7, 2, cursor)
		if err != nil {
			t.Fatalf("ListMine page %d: %v", page, err)
		}
		for _, item := range items {
			seen = append(seen, item.ID)
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if len(seen) != len(created) {
		t.Fatalf("paged through %d submissions, want %d", len(seen), len(created))
	}
	for i, id := range seen {
		want := created[len(created)-1-i]
		if id != want {
			t.Fatalf("page order[%d] = %d, want %d (newest first)", i, id, want)
		}
	}
}

func TestTemplateSubmission_ListMineIsolatesUsersAndValidatesCursor(t *testing.T) {
	SetupTestDB(t)
	service, _, _ := newSubmissionService(t, 10)

	mine := createSubmittableWork(t, 7, "https://cdn.example.test/work/pattern.png")
	if _, err := service.Submit(context.Background(), 7, templatesubmission.SubmitInput{
		WorkID: mine.ID, Title: "小猫", ClientRequestID: "req-mine",
	}); err != nil {
		t.Fatalf("Submit as user 7: %v", err)
	}
	theirs := createSubmittableWork(t, 8, "https://cdn.example.test/work/pattern.png")
	if _, err := service.Submit(context.Background(), 8, templatesubmission.SubmitInput{
		WorkID: theirs.ID, Title: "小狗", ClientRequestID: "req-theirs",
	}); err != nil {
		t.Fatalf("Submit as user 8: %v", err)
	}

	items, _, err := service.ListMine(context.Background(), 7, 10, "")
	if err != nil {
		t.Fatalf("ListMine: %v", err)
	}
	if len(items) != 1 || items[0].UserID != 7 {
		t.Fatalf("ListMine returned %d items for user 7, want only their own", len(items))
	}

	if _, _, err := service.ListMine(context.Background(), 7, 10, "not-a-cursor"); err == nil {
		t.Error("expected an error for a malformed cursor")
	} else {
		assertErrCode(t, err, apperr.CodeInvalidArgument)
	}
}
