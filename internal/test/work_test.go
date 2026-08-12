package test

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	"github.com/zhaojiabo/bobobeads_server/internal/service/work"
	"google.golang.org/protobuf/encoding/protojson"
)

func validPatternData(width, height int32) *pb.PatternData {
	pixels := make([]int32, int(width*height))
	for i := range pixels {
		if i%2 == 0 {
			pixels[i] = 1
		}
	}
	return &pb.PatternData{
		Width:         width,
		Height:        height,
		BoardSpec:     "29x29",
		SchemaVersion: 1,
		Pixels:        pixels,
		ColorPalette:  []*pb.ColorEntry{{Index: 1, Hex: "#FFFFFF", Brand: "hama", Code: "H-01", Name: "White"}},
	}
}

func TestSaveWork(t *testing.T) {
	SetupTestDB(t)
	workDAO := dao.NewWorkDAO()
	workService := work.NewService(workDAO, nil, nil)

	ctx := context.Background()
	w := &model.Work{
		Title:            "测试图纸",
		OriginalImageURL: "https://oss.example.com/original.jpg",
		PatternImageURL:  "https://oss.example.com/pattern.jpg",
		BeadCount:        841,
		ColorCount:       12,
	}

	patternData := &pb.PatternData{
		Width:         29,
		Height:        29,
		BoardSpec:     "29x29",
		SchemaVersion: 1,
		Pixels:        make([]int32, 29*29),
		ColorPalette: []*pb.ColorEntry{
			{Index: 1, Hex: "#FFFFFF", Brand: "hama", Code: "H-01", Name: "White"},
		},
	}

	workID, err := workService.SaveWork(ctx, 1, w, patternData)
	if err != nil {
		t.Fatalf("SaveWork failed: %v", err)
	}
	if workID == 0 {
		t.Error("expected work ID > 0")
	}

	// Verify retrieval
	saved, err := workService.GetWork(ctx, 1, workID)
	if err != nil {
		t.Fatalf("GetWork failed: %v", err)
	}
	if saved.Title != "测试图纸" {
		t.Errorf("expected title=测试图纸, got %s", saved.Title)
	}
	if saved.BoardSpec != "29x29" {
		t.Errorf("expected board_spec=29x29, got %s", saved.BoardSpec)
	}
	if saved.Status != 2 {
		t.Errorf("expected status=2 (completed), got %d", saved.Status)
	}
	if saved.PatternData == nil {
		t.Error("expected pattern_data to be saved")
	}

	t.Logf("SaveWork success: work_id=%d", workID)
}

func TestSaveWork_PatternDataValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*pb.PatternData)
	}{
		{
			name: "pixels length mismatch",
			mutate: func(p *pb.PatternData) {
				p.Pixels = p.Pixels[:3]
			},
		},
		{
			name: "missing board spec",
			mutate: func(p *pb.PatternData) {
				p.BoardSpec = ""
			},
		},
		{
			name: "unsupported schema version",
			mutate: func(p *pb.PatternData) {
				p.SchemaVersion = 2
			},
		},
		{
			name: "duplicate palette index",
			mutate: func(p *pb.PatternData) {
				p.ColorPalette = append(p.ColorPalette, &pb.ColorEntry{Index: 1, Hex: "#000000"})
			},
		},
		{
			name: "invalid color hex",
			mutate: func(p *pb.PatternData) {
				p.ColorPalette[0].Hex = "#FFF"
			},
		},
		{
			name: "pixel references missing color",
			mutate: func(p *pb.PatternData) {
				p.Pixels[0] = 2
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := validPatternData(2, 2)
			tt.mutate(pattern)
			if err := work.ValidatePatternData(pattern); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestSaveWork_RequiresPatternData(t *testing.T) {
	SetupTestDB(t)
	workDAO := dao.NewWorkDAO()
	workService := work.NewService(workDAO, nil, nil)

	_, err := workService.SaveWork(context.Background(), 1, &model.Work{Title: "no pattern"}, nil)
	if err == nil {
		t.Fatal("expected validation error for missing pattern_data")
	}
}

func TestListWorks(t *testing.T) {
	SetupTestDB(t)
	workDAO := dao.NewWorkDAO()
	workService := work.NewService(workDAO, nil, nil)

	ctx := context.Background()
	userID := uint64(1)

	// Save 3 works
	for i := 0; i < 3; i++ {
		_, err := workService.SaveWork(ctx, userID, &model.Work{
			Title:     "作品" + string(rune('A'+i)),
			BeadCount: 841,
		}, validPatternData(3, 3))
		if err != nil {
			t.Fatalf("SaveWork #%d failed: %v", i, err)
		}
	}

	works, total, err := workService.ListWorks(ctx, userID, 1, 10, "")
	if err != nil {
		t.Fatalf("ListWorks failed: %v", err)
	}
	if total != 3 {
		t.Errorf("expected total=3, got %d", total)
	}
	if len(works) != 3 {
		t.Errorf("expected 3 works, got %d", len(works))
	}

	t.Logf("ListWorks success: total=%d", total)
}

func TestDeleteWork(t *testing.T) {
	SetupTestDB(t)
	workDAO := dao.NewWorkDAO()
	workService := work.NewService(workDAO, nil, nil)

	ctx := context.Background()
	userID := uint64(1)

	workID, err := workService.SaveWork(ctx, userID, &model.Work{Title: "to delete"}, validPatternData(3, 3))
	if err != nil {
		t.Fatalf("SaveWork failed: %v", err)
	}
	err = workService.DeleteWork(ctx, userID, workID)
	if err != nil {
		t.Fatalf("DeleteWork failed: %v", err)
	}

	works, total, _ := workService.ListWorks(ctx, userID, 1, 10, "")
	if total != 0 {
		t.Errorf("expected total=0 after delete, got %d", total)
	}
	if len(works) != 0 {
		t.Errorf("expected 0 works, got %d", len(works))
	}

	t.Log("DeleteWork success")
}

func TestDrafts(t *testing.T) {
	SetupTestDB(t)
	workDAO := dao.NewWorkDAO()
	workService := work.NewService(workDAO, nil, nil)

	ctx := context.Background()
	userID := uint64(1)

	// Save draft
	draftID, err := workService.SaveDraft(ctx, userID, &model.Work{Title: "草稿1", BoardSpec: "15x15"}, nil)
	if err != nil {
		t.Fatalf("SaveDraft failed: %v", err)
	}
	if draftID == 0 {
		t.Error("expected draft ID > 0")
	}

	// List drafts
	drafts, total, err := workService.ListDrafts(ctx, userID, 1, 10)
	if err != nil {
		t.Fatalf("ListDrafts failed: %v", err)
	}
	if total != 1 {
		t.Errorf("expected 1 draft, got %d", total)
	}
	if drafts[0].Status != 1 {
		t.Errorf("expected status=1 (draft), got %d", drafts[0].Status)
	}

	// Works should not contain drafts
	works, workTotal, _ := workService.ListWorks(ctx, userID, 1, 10, "")
	if workTotal != 0 {
		t.Errorf("expected 0 completed works, got %d", workTotal)
	}
	_ = works

	t.Logf("Drafts success: draft_id=%d", draftID)
}

func TestGetWork_RejectsOtherUser(t *testing.T) {
	SetupTestDB(t)
	workDAO := dao.NewWorkDAO()
	workService := work.NewService(workDAO, nil, nil)

	ctx := context.Background()

	patternData := &pb.PatternData{
		Width:         5,
		Height:        5,
		BoardSpec:     "5x5",
		SchemaVersion: 1,
		Pixels:        make([]int32, 25),
		ColorPalette:  []*pb.ColorEntry{{Index: 1, Hex: "#FF0000", Name: "Red"}},
	}

	workID, err := workService.SaveWork(ctx, 1, &model.Work{Title: "private"}, patternData)
	if err != nil {
		t.Fatalf("SaveWork failed: %v", err)
	}

	// Owner can read
	_, err = workService.GetWork(ctx, 1, workID)
	if err != nil {
		t.Fatalf("owner GetWork failed: %v", err)
	}

	// Other user cannot read
	_, err = workService.GetWork(ctx, 2, workID)
	if err == nil {
		t.Fatal("expected error when reading another user's work")
	}

	t.Log("GetWork permission check success")
}

func TestSaveWork_PatternDataRoundTrip(t *testing.T) {
	SetupTestDB(t)
	workDAO := dao.NewWorkDAO()
	workService := work.NewService(workDAO, nil, nil)

	ctx := context.Background()
	userID := uint64(1)

	pixels := make([]int32, 9)
	pixels[0] = 1
	pixels[4] = 2
	pixels[8] = 1

	patternData := &pb.PatternData{
		Width:         3,
		Height:        3,
		BoardSpec:     "3x3",
		SchemaVersion: 1,
		Pixels:        pixels,
		ColorPalette: []*pb.ColorEntry{
			{Index: 1, Hex: "#FF0000", Name: "Red"},
			{Index: 2, Hex: "#00FF00", Name: "Green"},
		},
	}

	workID, err := workService.SaveWork(ctx, userID, &model.Work{Title: "roundtrip"}, patternData)
	if err != nil {
		t.Fatalf("SaveWork failed: %v", err)
	}

	saved, err := workService.GetWork(ctx, userID, workID)
	if err != nil {
		t.Fatalf("GetWork failed: %v", err)
	}

	pd := work.JSONMapToPatternData(saved.PatternData)
	if pd == nil {
		t.Fatal("expected non-nil PatternData")
	}
	if pd.Width != 3 || pd.Height != 3 {
		t.Errorf("expected 3x3, got %dx%d", pd.Width, pd.Height)
	}
	if len(pd.Pixels) != 9 {
		t.Fatalf("expected 9 pixels, got %d", len(pd.Pixels))
	}
	if pd.Pixels[0] != 1 || pd.Pixels[4] != 2 || pd.Pixels[8] != 1 {
		t.Errorf("pixel data mismatch: got %v", pd.Pixels)
	}
	if len(pd.ColorPalette) != 2 {
		t.Errorf("expected 2 colors, got %d", len(pd.ColorPalette))
	}

	t.Log("PatternData round-trip success")
}

func TestSaveWork_RecalculatesPatternStatistics(t *testing.T) {
	SetupTestDB(t)
	workService := work.NewService(dao.NewWorkDAO(), nil, nil)

	pattern := validPatternData(3, 3)
	workID, err := workService.SaveWork(context.Background(), 1, &model.Work{
		Title:      "server-derived stats",
		BeadCount:  999,
		ColorCount: 999,
	}, pattern)
	if err != nil {
		t.Fatalf("SaveWork failed: %v", err)
	}

	saved, err := workService.GetWork(context.Background(), 1, workID)
	if err != nil {
		t.Fatalf("GetWork failed: %v", err)
	}
	if saved.BeadCount != 5 || saved.ColorCount != 1 {
		t.Fatalf("expected derived stats 5 beads and 1 color, got %d beads and %d colors", saved.BeadCount, saved.ColorCount)
	}
}

func TestDecodePatternData_ReadsLegacyStorageWithoutSchemaVersion(t *testing.T) {
	pattern := validPatternData(2, 2)
	stored := work.PatternDataToJSONMap(pattern)
	delete(stored, "schema_version")

	decoded, err := work.DecodePatternData(stored)
	if err != nil {
		t.Fatalf("DecodePatternData failed: %v", err)
	}
	if decoded.SchemaVersion != 1 {
		t.Fatalf("expected legacy storage to be read as schema version 1, got %d", decoded.SchemaVersion)
	}
}

func TestDecodePatternData_RejectsExplicitUnsupportedSchemaVersion(t *testing.T) {
	stored := work.PatternDataToJSONMap(validPatternData(2, 2))
	stored["schema_version"] = int32(0)

	if _, err := work.DecodePatternData(stored); err == nil {
		t.Fatal("expected an explicit schema version of 0 to be rejected")
	}
}

func TestPatternDataProtoJSONAcceptsOnlyContractFields(t *testing.T) {
	var pattern pb.PatternData
	if err := protojson.Unmarshal([]byte(`{
  "width": 3,
  "height": 3,
  "boardSpec": "29x29",
  "pixels": [1, 1, 0, 1, 2, 1, 0, 1, 1],
  "colorPalette": [
    {"index": 1, "hex": "#FF0000"},
    {"index": 2, "hex": "#FFFFFF"}
  ],
  "schemaVersion": 1
}`), &pattern); err != nil {
		t.Fatalf("expected lowerCamelCase PatternData to decode: %v", err)
	}
	if err := work.ValidatePatternData(&pattern); err != nil {
		t.Fatalf("expected decoded PatternData to validate: %v", err)
	}

	if err := protojson.Unmarshal([]byte(`{"pixelRows": []}`), &pb.PatternData{}); err == nil {
		t.Fatal("expected removed pixelRows field to be rejected")
	}
}

func TestValidatePatternData_RequiresPixels(t *testing.T) {
	SetupTestDB(t)
	workDAO := dao.NewWorkDAO()
	workService := work.NewService(workDAO, nil, nil)

	ctx := context.Background()

	// An empty pixels array should fail.
	badPattern := &pb.PatternData{
		Width:         5,
		Height:        5,
		SchemaVersion: 1,
		ColorPalette:  []*pb.ColorEntry{{Index: 1, Hex: "#000"}},
	}
	_, err := workService.SaveWork(ctx, 1, &model.Work{Title: "test"}, badPattern)
	if err == nil {
		t.Error("expected validation error for missing pixels")
	}

	t.Log("ValidatePatternData requires pixels check success")
}

// fakeThumbnailer records the source it was asked to work from, so tests can tell
// "regenerated from the new pattern image" from "left the old thumbnail alone".
type fakeThumbnailer struct {
	sources []string
	result  string
}

func (f *fakeThumbnailer) ThumbnailURLByImageURL(_ context.Context, _ uint64, imageURL string) string {
	f.sources = append(f.sources, imageURL)
	return f.result
}

func createWorkForUpdate(t *testing.T, svc *work.Service, userID uint64) *model.Work {
	t.Helper()
	w := &model.Work{
		Title:            "原标题",
		OriginalImageURL: "https://oss.example.com/original.jpg",
		PatternImageURL:  "https://oss.example.com/pattern.jpg",
		ThumbnailURL:     "https://oss.example.com/thumb_old.webp",
		SourceType:       "ai",
		SourceID:         "gen-1",
	}
	id, err := svc.SaveWork(context.Background(), userID, w, validPatternData(4, 4))
	if err != nil {
		t.Fatalf("SaveWork: %v", err)
	}
	// SaveWork does not persist the thumbnail, so seed it the way generation does.
	w.ID = id
	if err := dao.NewWorkDAO().Update(context.Background(), w); err != nil {
		t.Fatalf("seed thumbnail: %v", err)
	}
	return w
}

func TestUpdateWorkKeepsServerOwnedFields(t *testing.T) {
	SetupTestDB(t)
	workService := work.NewService(dao.NewWorkDAO(), nil, nil)
	w := createWorkForUpdate(t, workService, 1)

	updated, err := workService.UpdateWork(context.Background(), 1, w.ID, work.UpdateWorkInput{
		Title:       "新标题",
		PatternData: validPatternData(6, 6),
	})
	if err != nil {
		t.Fatalf("UpdateWork: %v", err)
	}
	if updated.Title != "新标题" {
		t.Errorf("title = %q, want 新标题", updated.Title)
	}
	if updated.Width != 6 || updated.Height != 6 {
		t.Errorf("size = %dx%d, want 6x6", updated.Width, updated.Height)
	}
	// The whole point of this endpoint: none of these may be reset the way
	// SaveDraft resets them.
	if updated.Status != 2 {
		t.Errorf("status = %d, want 2 (still a finished work)", updated.Status)
	}
	if updated.ThumbnailURL != w.ThumbnailURL {
		t.Errorf("thumbnailUrl = %q, want the existing %q", updated.ThumbnailURL, w.ThumbnailURL)
	}
	if updated.SourceType != "ai" || updated.SourceID != "gen-1" {
		t.Errorf("source = %s/%s, want ai/gen-1", updated.SourceType, updated.SourceID)
	}
	if updated.OriginalImageURL != w.OriginalImageURL {
		t.Errorf("originalImageUrl = %q, want unchanged %q", updated.OriginalImageURL, w.OriginalImageURL)
	}

	reloaded, err := workService.GetWork(context.Background(), 1, w.ID)
	if err != nil {
		t.Fatalf("GetWork: %v", err)
	}
	if reloaded.Title != "新标题" || reloaded.Width != 6 {
		t.Errorf("reloaded = %q %dx%d, want 新标题 6x6", reloaded.Title, reloaded.Width, reloaded.Height)
	}
}

func TestUpdateWorkOmittedFieldsKeepPatternData(t *testing.T) {
	SetupTestDB(t)
	workService := work.NewService(dao.NewWorkDAO(), nil, nil)
	w := createWorkForUpdate(t, workService, 1)

	updated, err := workService.UpdateWork(context.Background(), 1, w.ID, work.UpdateWorkInput{Title: "只改标题"})
	if err != nil {
		t.Fatalf("UpdateWork: %v", err)
	}
	if updated.PatternData == nil {
		t.Fatal("pattern data was wiped by a title-only update")
	}
	if updated.Width != 4 || updated.BeadCount == 0 {
		t.Errorf("stats = %dx%d / %d beads, want the original 4x4 with beads",
			updated.Width, updated.Height, updated.BeadCount)
	}
}

func TestUpdateWorkRegeneratesThumbnailWhenPatternImageChanges(t *testing.T) {
	SetupTestDB(t)
	thumbnailer := &fakeThumbnailer{result: "https://oss.example.com/thumb_new.webp"}
	workService := work.NewService(dao.NewWorkDAO(), thumbnailer, nil)
	w := createWorkForUpdate(t, workService, 1)

	updated, err := workService.UpdateWork(context.Background(), 1, w.ID, work.UpdateWorkInput{
		PatternImageURL: "https://oss.example.com/pattern_v2.jpg",
		PatternData:     validPatternData(4, 4),
	})
	if err != nil {
		t.Fatalf("UpdateWork: %v", err)
	}
	if updated.ThumbnailURL != thumbnailer.result {
		t.Errorf("thumbnailUrl = %q, want the regenerated %q", updated.ThumbnailURL, thumbnailer.result)
	}
	if len(thumbnailer.sources) != 1 || thumbnailer.sources[0] != "https://oss.example.com/pattern_v2.jpg" {
		t.Errorf("thumbnail sources = %v, want one call with the new pattern image", thumbnailer.sources)
	}
}

func TestUpdateWorkSkipsThumbnailWhenPatternImageUnchanged(t *testing.T) {
	SetupTestDB(t)
	thumbnailer := &fakeThumbnailer{result: "https://oss.example.com/thumb_new.webp"}
	workService := work.NewService(dao.NewWorkDAO(), thumbnailer, nil)
	w := createWorkForUpdate(t, workService, 1)

	updated, err := workService.UpdateWork(context.Background(), 1, w.ID, work.UpdateWorkInput{
		Title:           "改名",
		PatternImageURL: w.PatternImageURL,
	})
	if err != nil {
		t.Fatalf("UpdateWork: %v", err)
	}
	if len(thumbnailer.sources) != 0 {
		t.Errorf("thumbnail was regenerated for an unchanged image: %v", thumbnailer.sources)
	}
	if updated.ThumbnailURL != w.ThumbnailURL {
		t.Errorf("thumbnailUrl = %q, want the existing %q", updated.ThumbnailURL, w.ThumbnailURL)
	}
}

func TestUpdateWorkKeepsOldThumbnailWhenGenerationFails(t *testing.T) {
	SetupTestDB(t)
	thumbnailer := &fakeThumbnailer{result: ""}
	workService := work.NewService(dao.NewWorkDAO(), thumbnailer, nil)
	w := createWorkForUpdate(t, workService, 1)

	updated, err := workService.UpdateWork(context.Background(), 1, w.ID, work.UpdateWorkInput{
		PatternImageURL: "https://evil.example.com/pattern.jpg",
	})
	if err != nil {
		t.Fatalf("UpdateWork: %v", err)
	}
	if updated.ThumbnailURL != w.ThumbnailURL {
		t.Errorf("thumbnailUrl = %q, want the old %q kept when generation yields nothing",
			updated.ThumbnailURL, w.ThumbnailURL)
	}
}

func TestUpdateWorkRejectsForeignAndMissingWork(t *testing.T) {
	SetupTestDB(t)
	workService := work.NewService(dao.NewWorkDAO(), nil, nil)
	w := createWorkForUpdate(t, workService, 1)

	if _, err := workService.UpdateWork(context.Background(), 2, w.ID, work.UpdateWorkInput{Title: "抢作品"}); err == nil {
		t.Error("expected an error when updating someone else's work")
	} else {
		assertErrCode(t, err, apperr.CodeNotFound)
	}

	if _, err := workService.UpdateWork(context.Background(), 1, 999999, work.UpdateWorkInput{Title: "不存在"}); err == nil {
		t.Error("expected an error for a missing work")
	} else {
		assertErrCode(t, err, apperr.CodeNotFound)
	}

	if _, err := workService.UpdateWork(context.Background(), 1, 0, work.UpdateWorkInput{Title: "零"}); err == nil {
		t.Error("expected an error for work_id = 0")
	} else {
		assertErrCode(t, err, apperr.CodeInvalidArgument)
	}
}

func TestUpdateWorkRejectsInvalidPatternData(t *testing.T) {
	SetupTestDB(t)
	workService := work.NewService(dao.NewWorkDAO(), nil, nil)
	w := createWorkForUpdate(t, workService, 1)

	_, err := workService.UpdateWork(context.Background(), 1, w.ID, work.UpdateWorkInput{
		PatternData: &pb.PatternData{Width: 5, Height: 5, SchemaVersion: 1},
	})
	if err == nil {
		t.Fatal("expected a validation error for pattern data without pixels")
	}

	// The rejected update must not have touched the row.
	reloaded, err := workService.GetWork(context.Background(), 1, w.ID)
	if err != nil {
		t.Fatalf("GetWork: %v", err)
	}
	if reloaded.Width != 4 || reloaded.Title != "原标题" {
		t.Errorf("row changed despite the failed update: %q %dx%d", reloaded.Title, reloaded.Width, reloaded.Height)
	}
}

// seedSubmission writes a submission row straight to the table: these tests only
// care about the review lock, not about how a submission comes to exist.
func seedSubmission(t *testing.T, userID, workID uint64, status int8) {
	t.Helper()
	activeKey := strconv.FormatUint(workID, 10)
	sub := &model.TemplateSubmission{
		UserID:          userID,
		WorkID:          workID,
		Title:           "投稿标题",
		Status:          status,
		ClientRequestID: fmt.Sprintf("req-%d-%d-%d", userID, workID, status),
	}
	if status != model.TemplateSubmissionStatusRejected {
		sub.ActiveWorkKey = &activeKey
	}
	if err := dao.NewTemplateSubmissionDAO().Create(context.Background(), sub); err != nil {
		t.Fatalf("seed submission: %v", err)
	}
}

func newWorkServiceWithSubmissions() *work.Service {
	return work.NewService(dao.NewWorkDAO(), nil, dao.NewTemplateSubmissionDAO())
}

func TestUpdateWorkBlockedWhileSubmissionPendingReview(t *testing.T) {
	SetupTestDB(t)
	workService := newWorkServiceWithSubmissions()
	w := createWorkForUpdate(t, workService, 1)
	seedSubmission(t, 1, w.ID, model.TemplateSubmissionStatusPending)

	_, err := workService.UpdateWork(context.Background(), 1, w.ID, work.UpdateWorkInput{Title: "审核期间改名"})
	if err == nil {
		t.Fatal("expected the edit to be refused while the submission waits for review")
	}
	assertErrCode(t, err, apperr.CodeWorkUnderReview)

	reloaded, err := workService.GetWork(context.Background(), 1, w.ID)
	if err != nil {
		t.Fatalf("GetWork: %v", err)
	}
	if reloaded.Title != "原标题" {
		t.Errorf("title = %q, want the row left untouched", reloaded.Title)
	}
}

func TestSaveDraftBlockedWhileSubmissionPendingReview(t *testing.T) {
	SetupTestDB(t)
	workService := newWorkServiceWithSubmissions()
	w := createWorkForUpdate(t, workService, 1)
	seedSubmission(t, 1, w.ID, model.TemplateSubmissionStatusPending)

	// Overwriting an existing row through the draft endpoint is an edit too, so it
	// must not be a way around the lock.
	_, err := workService.SaveDraft(context.Background(), 1, &model.Work{
		BaseModel: model.BaseModel{ID: w.ID},
		Title:     "绕过审核锁",
	}, validPatternData(4, 4))
	if err == nil {
		t.Fatal("expected SaveDraft to be refused for a work under review")
	}
	assertErrCode(t, err, apperr.CodeWorkUnderReview)

	reloaded, err := workService.GetWork(context.Background(), 1, w.ID)
	if err != nil {
		t.Fatalf("GetWork: %v", err)
	}
	if reloaded.Title != "原标题" || reloaded.Status != 2 {
		t.Errorf("row = %q status %d, want it untouched and still finished", reloaded.Title, reloaded.Status)
	}
}

func TestUpdateWorkAllowedOnceSubmissionIsReviewed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int8
	}{
		{"approved", model.TemplateSubmissionStatusApproved},
		{"rejected", model.TemplateSubmissionStatusRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			SetupTestDB(t)
			workService := newWorkServiceWithSubmissions()
			w := createWorkForUpdate(t, workService, 1)
			seedSubmission(t, 1, w.ID, tc.status)

			// An approved submission published an independent snapshot, so editing the
			// work afterwards cannot affect the template and must be allowed.
			updated, err := workService.UpdateWork(context.Background(), 1, w.ID, work.UpdateWorkInput{Title: "审核后改名"})
			if err != nil {
				t.Fatalf("UpdateWork: %v", err)
			}
			if updated.Title != "审核后改名" {
				t.Errorf("title = %q, want 审核后改名", updated.Title)
			}
		})
	}
}

func TestUpdateWorkLockAppliesOnlyToTheSubmittedWork(t *testing.T) {
	SetupTestDB(t)
	workService := newWorkServiceWithSubmissions()
	target := createWorkForUpdate(t, workService, 1)
	other := createWorkForUpdate(t, workService, 1)

	seedSubmission(t, 1, other.ID, model.TemplateSubmissionStatusPending)
	// Same work id, different owner: must not lock this user's work.
	seedSubmission(t, 2, target.ID, model.TemplateSubmissionStatusPending)

	if _, err := workService.UpdateWork(context.Background(), 1, target.ID, work.UpdateWorkInput{Title: "可以改"}); err != nil {
		t.Fatalf("UpdateWork was blocked by an unrelated submission: %v", err)
	}
}
