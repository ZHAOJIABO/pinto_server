package test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/zhaojiabo/bobobeads_server/internal/api"
	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/middleware"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	"github.com/zhaojiabo/bobobeads_server/internal/service/ai_generation"
	"github.com/zhaojiabo/bobobeads_server/internal/service/work"
)

func userCtx(userID uint64) context.Context {
	return context.WithValue(context.Background(), middleware.UserIDKey, userID)
}

func seedWork(t *testing.T, userID uint64, patternURL, thumbnailURL string) uint64 {
	t.Helper()
	w := &model.Work{
		UserID:          userID,
		Title:           "作品",
		PatternImageURL: patternURL,
		ThumbnailURL:    thumbnailURL,
		Status:          1,
	}
	if err := db.DB.Create(w).Error; err != nil {
		t.Fatalf("seed work: %v", err)
	}
	return w.ID
}

func TestWorkHandler_ReturnsStoredThumbnailVerbatim(t *testing.T) {
	SetupTestDB(t)
	handler := api.NewWorkHandler(work.NewService(dao.NewWorkDAO()))
	ctx := userCtx(7)

	// Thumbnails are produced at write time, so the read path must not derive
	// anything: an empty column stays empty and the client falls back to the
	// full-size pattern image.
	emptyID := seedWork(t, 7, "https://cdn.example.test/pattern.png", "")
	storedID := seedWork(t, 7, "https://cdn.example.test/pattern.png", "https://cdn.example.test/pattern-low.webp")

	resp, err := handler.GetWork(ctx, &pb.GetWorkRequest{WorkId: strconv.FormatUint(emptyID, 10)})
	if err != nil {
		t.Fatalf("GetWork: %v", err)
	}
	if got := resp.Work.ThumbnailUrl; got != "" {
		t.Errorf("thumbnailUrl = %q, want empty", got)
	}

	resp, err = handler.GetWork(ctx, &pb.GetWorkRequest{WorkId: strconv.FormatUint(storedID, 10)})
	if err != nil {
		t.Fatalf("GetWork: %v", err)
	}
	if got := resp.Work.ThumbnailUrl; got != "https://cdn.example.test/pattern-low.webp" {
		t.Errorf("thumbnailUrl = %q, want the stored value", got)
	}
}

func TestAIGenerationHandler_ReturnsStoredThumbnailsVerbatim(t *testing.T) {
	env := newAITestEnv(t, ai_generation.Config{TaskExpireMinutes: 30})
	handler := api.NewAIGenerationHandler(env.svc, 120)

	completed := time.Now()
	task := &model.AIGeneration{
		TaskID:             "task-thumb-1",
		UserID:             9,
		ClientRequestID:    "req-thumb-1",
		StyleID:            1,
		InputFileKey:       "style_input/a.png",
		InputImageURL:      "https://cdn.example.test/style_input/a.png",
		InputThumbnailURL:  "https://cdn.example.test/style_input/a-low.webp",
		OutputImageURL:     "https://cdn.example.test/ai_output/b.png",
		OutputThumbnailURL: "https://cdn.example.test/ai_output/b-low.webp",
		Status:             model.AIGenStatusSucceeded,
		CompletedAt:        &completed,
	}
	if err := db.DB.Create(task).Error; err != nil {
		t.Fatalf("seed ai task: %v", err)
	}

	resp, err := handler.GetStyleGeneration(userCtx(9), &pb.GetStyleGenerationRequest{TaskId: task.TaskID})
	if err != nil {
		t.Fatalf("GetStyleGeneration: %v", err)
	}
	if resp.Task.InputThumbnailUrl != task.InputThumbnailURL {
		t.Errorf("inputThumbnailUrl = %q, want %q", resp.Task.InputThumbnailUrl, task.InputThumbnailURL)
	}
	if resp.Task.OutputThumbnailUrl != task.OutputThumbnailURL {
		t.Errorf("outputThumbnailUrl = %q, want %q", resp.Task.OutputThumbnailUrl, task.OutputThumbnailURL)
	}
}

func TestAIGenerationHandler_ThumbnailsEmptyForPendingTask(t *testing.T) {
	env := newAITestEnv(t, ai_generation.Config{TaskExpireMinutes: 30})
	handler := api.NewAIGenerationHandler(env.svc, 120)

	task := &model.AIGeneration{
		TaskID:          "task-thumb-2",
		UserID:          9,
		ClientRequestID: "req-thumb-2",
		StyleID:         1,
		InputFileKey:    "style_input/a.png",
		InputImageURL:   "https://cdn.example.test/style_input/a.png",
		Status:          model.AIGenStatusPending,
	}
	if err := db.DB.Create(task).Error; err != nil {
		t.Fatalf("seed ai task: %v", err)
	}

	resp, err := handler.GetStyleGeneration(userCtx(9), &pb.GetStyleGenerationRequest{TaskId: task.TaskID})
	if err != nil {
		t.Fatalf("GetStyleGeneration: %v", err)
	}
	if resp.Task.OutputThumbnailUrl != "" {
		t.Errorf("outputThumbnailUrl = %q, want empty", resp.Task.OutputThumbnailUrl)
	}
	if resp.Task.InputThumbnailUrl != "" {
		t.Errorf("inputThumbnailUrl = %q, want empty", resp.Task.InputThumbnailUrl)
	}
}

func seedAITask(t *testing.T, taskID string, status int8, startedAgo time.Duration) *model.AIGeneration {
	t.Helper()
	task := &model.AIGeneration{
		TaskID:          taskID,
		UserID:          9,
		ClientRequestID: "req-" + taskID,
		StyleID:         1,
		InputFileKey:    "style_input/a.png",
		InputImageURL:   "https://cdn.example.test/style_input/a.png",
		Status:          status,
	}
	if startedAgo > 0 {
		started := time.Now().Add(-startedAgo)
		task.StartedAt = &started
	}
	if err := db.DB.Create(task).Error; err != nil {
		t.Fatalf("seed ai task: %v", err)
	}
	return task
}

func TestAIGenerationHandler_ProgressEstimate(t *testing.T) {
	env := newAITestEnv(t, ai_generation.Config{TaskExpireMinutes: 30})
	handler := api.NewAIGenerationHandler(env.svc, 120)

	cases := []struct {
		name       string
		status     int8
		startedAgo time.Duration
		want       int32
	}{
		{"queued", model.AIGenStatusPending, 0, 5},
		{"just claimed", model.AIGenStatusRunning, 0, 10},
		{"halfway", model.AIGenStatusRunning, 60 * time.Second, 52},
		{"at the average", model.AIGenStatusRunning, 120 * time.Second, 95},
		// A task slower than average must plateau, never overshoot.
		{"well past the average", model.AIGenStatusRunning, 10 * time.Minute, 95},
		{"succeeded", model.AIGenStatusSucceeded, 90 * time.Second, 100},
		{"failed", model.AIGenStatusFailed, 90 * time.Second, 0},
		{"expired", model.AIGenStatusExpired, 0, 0},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			task := seedAITask(t, "progress-"+strconv.Itoa(i), c.status, c.startedAgo)
			resp, err := handler.GetStyleGeneration(userCtx(9), &pb.GetStyleGenerationRequest{TaskId: task.TaskID})
			if err != nil {
				t.Fatalf("GetStyleGeneration: %v", err)
			}
			// Tolerate a point of drift: the estimate is computed against wall
			// clock, so a slow test machine shifts it slightly.
			if got := resp.Task.Progress; got < c.want-1 || got > c.want {
				t.Errorf("progress = %d, want ~%d", got, c.want)
			}
		})
	}
}

func TestAIGenerationHandler_ProgressNeverReports100BeforeDone(t *testing.T) {
	env := newAITestEnv(t, ai_generation.Config{TaskExpireMinutes: 30})
	// A misconfigured tiny average must not let a running task claim 100%,
	// which the client would read as done while it keeps polling.
	handler := api.NewAIGenerationHandler(env.svc, 1)
	task := seedAITask(t, "progress-tiny-avg", model.AIGenStatusRunning, time.Hour)

	resp, err := handler.GetStyleGeneration(userCtx(9), &pb.GetStyleGenerationRequest{TaskId: task.TaskID})
	if err != nil {
		t.Fatalf("GetStyleGeneration: %v", err)
	}
	if got := resp.Task.Progress; got != 95 {
		t.Errorf("progress = %d, want 95", got)
	}
}

func TestAIGenerationHandler_ExposesStartedAt(t *testing.T) {
	env := newAITestEnv(t, ai_generation.Config{TaskExpireMinutes: 30})
	handler := api.NewAIGenerationHandler(env.svc, 120)

	queued := seedAITask(t, "started-queued", model.AIGenStatusPending, 0)
	resp, err := handler.GetStyleGeneration(userCtx(9), &pb.GetStyleGenerationRequest{TaskId: queued.TaskID})
	if err != nil {
		t.Fatalf("GetStyleGeneration: %v", err)
	}
	// Still queued: the client uses 0 to tell queue time apart from generation time.
	if resp.Task.StartedAt != 0 {
		t.Errorf("startedAt = %d, want 0 while queued", resp.Task.StartedAt)
	}

	running := seedAITask(t, "started-running", model.AIGenStatusRunning, 30*time.Second)
	resp, err = handler.GetStyleGeneration(userCtx(9), &pb.GetStyleGenerationRequest{TaskId: running.TaskID})
	if err != nil {
		t.Fatalf("GetStyleGeneration: %v", err)
	}
	if want := running.StartedAt.Unix(); resp.Task.StartedAt != want {
		t.Errorf("startedAt = %d, want %d", resp.Task.StartedAt, want)
	}
}
