package test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/service/ai_generation"
	"github.com/zhaojiabo/bobobeads_server/internal/service/credit"
	"github.com/zhaojiabo/bobobeads_server/internal/service/generation"
	"github.com/zhaojiabo/bobobeads_server/internal/service/media"
	"github.com/zhaojiabo/bobobeads_server/internal/service/subscribe"
	"github.com/zhaojiabo/bobobeads_server/internal/service/work"
)

// stubProvider lets a test decide what the third party does, including blocking
// forever, which is how the concurrency ceiling is observed.
type stubProvider struct {
	name   string
	submit func(ctx context.Context, req *ai_generation.SubmitRequest) (*ai_generation.Result, error)
	calls  atomic.Int64
}

func (p *stubProvider) Name() string             { return p.name }
func (p *stubProvider) Mode() ai_generation.Mode { return ai_generation.ModeSync }

func (p *stubProvider) Submit(ctx context.Context, req *ai_generation.SubmitRequest) (*ai_generation.Result, error) {
	p.calls.Add(1)
	return p.submit(ctx, req)
}

func (p *stubProvider) Query(context.Context, string) (*ai_generation.Result, error) {
	return nil, ai_generation.ErrQueryUnsupported
}

type stubVIPChecker struct{ vip bool }

func (c *stubVIPChecker) IsVIP(context.Context, uint64) (bool, error) { return c.vip, nil }

type aiTestEnv struct {
	svc      *ai_generation.Service
	credits  *credit.Service
	media    *media.Service
	storage  *memoryObjectStorage
	provider *stubProvider
	vip      *stubVIPChecker
}

func newAITestEnv(t *testing.T, cfg ai_generation.Config) *aiTestEnv {
	t.Helper()
	SetupTestDB(t)

	storage := newMemoryObjectStorage("https://cdn.example.test")
	mediaService := media.NewServiceWithStorage(dao.NewMediaDAO(), storage)
	creditService := credit.NewService(dao.NewCreditDAO())
	succeed := func(context.Context, *ai_generation.SubmitRequest) (*ai_generation.Result, error) {
		return &ai_generation.Result{
			Status:     ai_generation.StatusSucceeded,
			ImageBytes: []byte("generated-png-bytes"),
			ImageMIME:  "image/png",
		}, nil
	}
	provider := &stubProvider{name: "fake", submit: succeed}
	// A second configured model, so tests can exercise model selection without
	// standing up a real adapter.
	altProvider := &stubProvider{name: "gemini-3-1-flash-image-preview", submit: succeed}
	vip := &stubVIPChecker{}

	env := &aiTestEnv{
		credits:  creditService,
		media:    mediaService,
		storage:  storage,
		provider: provider,
		vip:      vip,
	}
	env.svc = ai_generation.NewService(
		dao.NewAIGenerationDAO(),
		dao.NewMediaDAO(),
		creditService,
		mediaService,
		vip,
		dao.NewSystemDAO(),
		ai_generation.NewRegistry(provider, altProvider),
		cfg,
	)
	return env
}

// drain claims and executes every runnable task synchronously, standing in for
// the dispatcher so tests observe deterministic ordering.
func (e *aiTestEnv) drain(t *testing.T, limit int) []*model.AIGeneration {
	t.Helper()
	claimed, err := e.svc.ClaimBatch(context.Background(), limit)
	if err != nil {
		t.Fatalf("ClaimBatch failed: %v", err)
	}
	for _, task := range claimed {
		e.svc.ExecuteTask(context.Background(), task.TaskID)
	}
	return claimed
}

func setupAIService(t *testing.T) (*ai_generation.Service, *credit.Service) {
	t.Helper()
	env := newAITestEnv(t, ai_generation.Config{TaskExpireMinutes: 30})
	return env.svc, env.credits
}

func seedAIStyle(t *testing.T) *model.AIStyle {
	t.Helper()
	style := &model.AIStyle{
		StyleKey:    "watercolor",
		Name:        "水彩风格",
		Description: "将图片转为水彩画风格",
		CoverURL:    "https://cdn/watercolor-cover.png",
		ExampleURL:  "https://cdn/watercolor-example.png",
		CostCredits: 2,
		SortOrder:   1,
		Status:      1,
		Provider:    "fake",
	}
	db.DB.Create(style)
	return style
}

func seedUploadedMedia(t *testing.T, userID uint64) string {
	t.Helper()
	fileKey := "style_input/2024/01/01/test-input.png"
	asset := &model.MediaAsset{
		UserID:      userID,
		FileKey:     fileKey,
		Purpose:     "style_input",
		ContentType: "image/png",
		Status:      model.MediaStatusUploaded,
	}
	db.DB.Create(asset)
	return fileKey
}

// seedInputObject registers the asset row and the object bytes together, which
// is what the execution path needs to read the original image.
func seedInputObject(t *testing.T, storage *memoryObjectStorage, userID uint64, fileKey string) {
	t.Helper()
	db.DB.Create(&model.MediaAsset{
		UserID:      userID,
		FileKey:     fileKey,
		Purpose:     "style_input",
		ContentType: "image/png",
		Status:      model.MediaStatusUploaded,
	})
	storage.put(fileKey, "image/png", []byte("original-png-bytes"))
}

func TestListAIStyles(t *testing.T) {
	env := newAITestEnv(t, ai_generation.Config{})
	svc := env.svc

	// Seed styles
	db.DB.Create(&model.AIStyle{StyleKey: "style1", Name: "Style 1", Status: 1, SortOrder: 2})
	db.DB.Create(&model.AIStyle{StyleKey: "style2", Name: "Style 2", Status: 1, SortOrder: 1})
	hiddenStyle := &model.AIStyle{StyleKey: "style3", Name: "Hidden", Status: 1, SortOrder: 3}
	db.DB.Create(hiddenStyle)
	db.DB.Model(hiddenStyle).Update("status", 0)

	styles, err := svc.ListStyles(context.Background())
	if err != nil {
		t.Fatalf("ListStyles failed: %v", err)
	}
	if len(styles) != 2 {
		t.Errorf("expected 2 active styles, got %d", len(styles))
	}
	if styles[0].Name != "Style 2" {
		t.Errorf("expected first style sorted by sort_order, got %s", styles[0].Name)
	}
	t.Log("ListAIStyles success")
}

func TestCreateStyleGeneration_Success(t *testing.T) {
	aiService, creditService := setupAIService(t)
	ctx := context.Background()
	userID := uint64(1)

	style := seedAIStyle(t)
	fileKey := seedUploadedMedia(t, userID)
	creditService.AddCredits(ctx, userID, 10, "test", "", "", "")

	result, err := aiService.CreateStyleGeneration(ctx, userID, style.ID, fileKey, "req-001")
	if err != nil {
		t.Fatalf("CreateStyleGeneration failed: %v", err)
	}
	if result.TaskID == "" {
		t.Error("expected non-empty task_id")
	}
	if result.CreditsDeducted != 2 {
		t.Errorf("expected 2 credits deducted, got %d", result.CreditsDeducted)
	}
	if result.RemainingBalance != 8 {
		t.Errorf("expected remaining_balance=8, got %d", result.RemainingBalance)
	}
	if result.Duplicated {
		t.Error("expected duplicated=false")
	}
	t.Log("CreateStyleGeneration success")
}

func TestCreateStyleGeneration_Idempotent(t *testing.T) {
	aiService, creditService := setupAIService(t)
	ctx := context.Background()
	userID := uint64(1)

	style := seedAIStyle(t)
	fileKey := seedUploadedMedia(t, userID)
	creditService.AddCredits(ctx, userID, 10, "test", "", "", "")

	result1, err := aiService.CreateStyleGeneration(ctx, userID, style.ID, fileKey, "dup-req")
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	result2, err := aiService.CreateStyleGeneration(ctx, userID, style.ID, fileKey, "dup-req")
	if err != nil {
		t.Fatalf("second create failed: %v", err)
	}

	if result1.TaskID != result2.TaskID {
		t.Errorf("expected same task_id, got %s vs %s", result1.TaskID, result2.TaskID)
	}
	if !result2.Duplicated {
		t.Error("expected duplicated=true on retry")
	}

	// Balance should only be deducted once
	balance, _ := creditService.GetBalance(ctx, userID)
	if balance != 8 {
		t.Errorf("expected balance=8 (deducted once), got %d", balance)
	}
	t.Log("CreateStyleGeneration idempotent success")
}

func TestCreateStyleGeneration_InsufficientCredits(t *testing.T) {
	aiService, _ := setupAIService(t)
	ctx := context.Background()
	userID := uint64(1)

	style := seedAIStyle(t)
	fileKey := seedUploadedMedia(t, userID)

	_, err := aiService.CreateStyleGeneration(ctx, userID, style.ID, fileKey, "no-credit")
	if err == nil {
		t.Error("expected error for insufficient credits")
	}
	t.Log("CreateStyleGeneration insufficient credits check success")
}

func TestCreateStyleGeneration_InvalidInput(t *testing.T) {
	aiService, creditService := setupAIService(t)
	ctx := context.Background()
	userID := uint64(1)

	style := seedAIStyle(t)
	creditService.AddCredits(ctx, userID, 10, "test", "", "", "")

	// Missing client_request_id
	_, err := aiService.CreateStyleGeneration(ctx, userID, style.ID, "some-key", "")
	if err == nil {
		t.Error("expected error for missing client_request_id")
	}

	// Missing input_file_key
	_, err = aiService.CreateStyleGeneration(ctx, userID, style.ID, "", "req-1")
	if err == nil {
		t.Error("expected error for missing input_file_key")
	}

	// Wrong user's file
	otherKey := "style_input/2024/other-user.png"
	db.DB.Create(&model.MediaAsset{
		UserID:  999,
		FileKey: otherKey,
		Purpose: "style_input",
		Status:  model.MediaStatusUploaded,
	})
	_, err = aiService.CreateStyleGeneration(ctx, userID, style.ID, otherKey, "req-2")
	if err == nil {
		t.Error("expected error for other user's file")
	}

	t.Log("CreateStyleGeneration input validation success")
}

func TestGetStyleGeneration_Ownership(t *testing.T) {
	aiService, creditService := setupAIService(t)
	ctx := context.Background()
	userID := uint64(1)

	style := seedAIStyle(t)
	fileKey := seedUploadedMedia(t, userID)
	creditService.AddCredits(ctx, userID, 10, "test", "", "", "")

	result, _ := aiService.CreateStyleGeneration(ctx, userID, style.ID, fileKey, "get-test")

	// Owner can get
	task, err := aiService.GetStyleGeneration(ctx, userID, result.TaskID)
	if err != nil {
		t.Fatalf("GetStyleGeneration failed: %v", err)
	}
	if task.TaskID != result.TaskID {
		t.Errorf("expected task_id=%s, got %s", result.TaskID, task.TaskID)
	}

	// Other user cannot get
	_, err = aiService.GetStyleGeneration(ctx, 999, result.TaskID)
	if err == nil {
		t.Error("expected error for other user reading task")
	}
	t.Log("GetStyleGeneration ownership check success")
}

func TestListStyleGenerations(t *testing.T) {
	aiService, creditService := setupAIService(t)
	ctx := context.Background()
	userID := uint64(1)

	style := seedAIStyle(t)
	fileKey := seedUploadedMedia(t, userID)
	creditService.AddCredits(ctx, userID, 10, "test", "", "", "")

	aiService.CreateStyleGeneration(ctx, userID, style.ID, fileKey, "list-1")
	aiService.CreateStyleGeneration(ctx, userID, style.ID, fileKey, "list-2")

	tasks, total, err := aiService.ListStyleGenerations(ctx, userID, 1, 20)
	if err != nil {
		t.Fatalf("ListStyleGenerations failed: %v", err)
	}
	if total != 2 {
		t.Errorf("expected total=2, got %d", total)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}

	// Other user should see 0
	_, total2, _ := aiService.ListStyleGenerations(ctx, 999, 1, 20)
	if total2 != 0 {
		t.Errorf("expected total=0 for other user, got %d", total2)
	}
	t.Log("ListStyleGenerations success")
}

func TestAIGeneration_EndToEnd(t *testing.T) {
	env := newAITestEnv(t, ai_generation.Config{TaskExpireMinutes: 30})
	ctx := context.Background()
	userID := uint64(1)

	generationDAO := dao.NewGenerationDAO()
	workDAO := dao.NewWorkDAO()
	orderDAO := dao.NewOrderDAO()
	productDAO := dao.NewProductDAO()
	subscriptionDAO := dao.NewSubscriptionDAO()

	creditService := env.credits
	aiService := env.svc

	workService := work.NewService(workDAO)
	subscribeService := subscribe.NewService(orderDAO, productDAO, subscriptionDAO)
	genService := generation.NewService(generationDAO, creditService, subscribeService, workService, nil)
	genService.SetAIValidator(aiService)

	// Seed data
	style := &model.AIStyle{StyleKey: "e2e", Name: "E2E Style", CostCredits: 1, Status: 1, Provider: "fake"}
	db.DB.Create(style)

	fileKey := "style_input/e2e/test.png"
	seedInputObject(t, env.storage, userID, fileKey)

	creditService.AddCredits(ctx, userID, 10, "test", "", "", "")

	// 1. Create AI style generation
	aiResult, err := aiService.CreateStyleGeneration(ctx, userID, style.ID, fileKey, "e2e-req")
	if err != nil {
		t.Fatalf("AI create failed: %v", err)
	}

	// 2. Submission only queues the task; the dispatcher is what executes it.
	queued, _ := aiService.GetStyleGeneration(ctx, userID, aiResult.TaskID)
	if queued.Status != model.AIGenStatusPending {
		t.Fatalf("expected AI task pending after create, got status=%d", queued.Status)
	}
	if len(env.drain(t, 10)) != 1 {
		t.Fatal("expected the dispatcher to claim exactly one task")
	}

	task, _ := aiService.GetStyleGeneration(ctx, userID, aiResult.TaskID)
	if task.Status != model.AIGenStatusSucceeded {
		t.Fatalf("expected AI task succeeded, got status=%d error=%s", task.Status, task.ErrorMessage)
	}
	if task.OutputFileKey == "" || task.OutputImageURL == "" {
		t.Fatalf("expected output stored in our own storage, got key=%q url=%q",
			task.OutputFileKey, task.OutputImageURL)
	}

	// 3. Create generation with ai_style source
	genResult, err := genService.CreateGeneration(ctx, userID, "29x29", "ai_style", aiResult.TaskID, "gen-e2e")
	if err != nil {
		t.Fatalf("CreateGeneration ai_style failed: %v", err)
	}

	// 4. Complete generation with pattern data
	pd := validPatternData(3, 3)
	workData := &model.Work{
		Title:       "AI Generated",
		PatternData: work.PatternDataToJSONMap(pd),
		Width:       int(pd.Width),
		Height:      int(pd.Height),
		BeadCount:   int(pd.Width * pd.Height),
		ColorCount:  len(pd.ColorPalette),
	}
	completeResult, err := genService.CompleteGeneration(ctx, userID, genResult.GenerationID, workData)
	if err != nil {
		t.Fatalf("CompleteGeneration failed: %v", err)
	}

	// 5. Verify work has source metadata
	savedWork, _ := workService.GetWork(ctx, userID, completeResult.WorkID)
	if savedWork.SourceType != "ai_style" {
		t.Errorf("expected source_type=ai_style, got %s", savedWork.SourceType)
	}
	if savedWork.SourceID != aiResult.TaskID {
		t.Errorf("expected source_id=%s, got %s", aiResult.TaskID, savedWork.SourceID)
	}

	// 6. ListWorks returns the record
	works, total, _ := workService.ListWorks(ctx, userID, 1, 20, "")
	if total != 1 {
		t.Errorf("expected 1 work, got %d", total)
	}
	if len(works) > 0 && works[0].SourceType != "ai_style" {
		t.Errorf("expected work source_type=ai_style")
	}

	// 7. ListWorks with source filter
	worksFiltered, totalFiltered, _ := workService.ListWorks(ctx, userID, 1, 20, "ai_style")
	if totalFiltered != 1 {
		t.Errorf("expected 1 ai_style work, got %d", totalFiltered)
	}
	_ = worksFiltered

	t.Log("AI Generation end-to-end flow success")
}

func TestCreateGeneration_AISource_Validation(t *testing.T) {
	env := newAITestEnv(t, ai_generation.Config{})
	ctx := context.Background()
	userID := uint64(1)

	generationDAO := dao.NewGenerationDAO()
	workDAO := dao.NewWorkDAO()
	orderDAO := dao.NewOrderDAO()
	productDAO := dao.NewProductDAO()
	subscriptionDAO := dao.NewSubscriptionDAO()

	workService := work.NewService(workDAO)
	subscribeService := subscribe.NewService(orderDAO, productDAO, subscriptionDAO)
	genService := generation.NewService(generationDAO, env.credits, subscribeService, workService, nil)
	genService.SetAIValidator(env.svc)

	// ai_style without source_id should fail
	_, err := genService.CreateGeneration(ctx, userID, "29x29", "ai_style", "", "no-source")
	if err == nil {
		t.Error("expected error for ai_style without source_id")
	}

	// ai_style with non-existent task should fail
	_, err = genService.CreateGeneration(ctx, userID, "29x29", "ai_style", "non-existent-task", "bad-task")
	if err == nil {
		t.Error("expected error for ai_style with non-existent task")
	}

	t.Log("CreateGeneration AI source validation success")
}

func TestCreateStyleGeneration_QuotaExceededDoesNotCharge(t *testing.T) {
	env := newAITestEnv(t, ai_generation.Config{TaskExpireMinutes: 30, FreeQueueSize: 2})
	ctx := context.Background()
	userID := uint64(1)

	style := seedAIStyle(t)
	fileKey := "style_input/quota/input.png"
	seedInputObject(t, env.storage, userID, fileKey)
	env.credits.AddCredits(ctx, userID, 100, "test", "", "", "")

	for i := 0; i < 2; i++ {
		if _, err := env.svc.CreateStyleGeneration(ctx, userID, style.ID, fileKey, fmt.Sprintf("quota-%d", i)); err != nil {
			t.Fatalf("create %d failed: %v", i, err)
		}
	}

	balanceBefore, _ := env.credits.GetBalance(ctx, userID)
	_, err := env.svc.CreateStyleGeneration(ctx, userID, style.ID, fileKey, "quota-over")
	if err == nil {
		t.Fatal("expected the third submission to be rejected")
	}
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperr.CodeTaskQuotaExceeded {
		t.Fatalf("expected task quota error, got %v", err)
	}

	balanceAfter, _ := env.credits.GetBalance(ctx, userID)
	if balanceAfter != balanceBefore {
		t.Fatalf("a rejected submission must not charge credits: %d -> %d", balanceBefore, balanceAfter)
	}
	var rows int64
	db.DB.Model(&model.AIGeneration{}).Where("user_id = ?", userID).Count(&rows)
	if rows != 2 {
		t.Fatalf("expected 2 task rows, got %d", rows)
	}
}

func TestClaimPendingTask_OnlyOneWinner(t *testing.T) {
	env := newAITestEnv(t, ai_generation.Config{TaskExpireMinutes: 30})
	ctx := context.Background()
	userID := uint64(1)

	style := seedAIStyle(t)
	fileKey := "style_input/claim/input.png"
	seedInputObject(t, env.storage, userID, fileKey)
	env.credits.AddCredits(ctx, userID, 100, "test", "", "", "")

	created, err := env.svc.CreateStyleGeneration(ctx, userID, style.ID, fileKey, "claim-req")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	aiDAO := dao.NewAIGenerationDAO()
	first, err := aiDAO.ClaimPendingTask(ctx, created.TaskID, time.Now())
	if err != nil || !first {
		t.Fatalf("first claim should win: owned=%v err=%v", first, err)
	}
	second, err := aiDAO.ClaimPendingTask(ctx, created.TaskID, time.Now())
	if err != nil {
		t.Fatalf("second claim errored: %v", err)
	}
	if second {
		t.Fatal("a task must never be claimed twice")
	}
}

func TestClaimBatch_PerUserConcurrency(t *testing.T) {
	env := newAITestEnv(t, ai_generation.Config{
		TaskExpireMinutes: 30,
		FreeConcurrent:    1,
		FreeQueueSize:     5,
	})
	ctx := context.Background()
	userID := uint64(1)

	style := seedAIStyle(t)
	fileKey := "style_input/gate/input.png"
	seedInputObject(t, env.storage, userID, fileKey)
	env.credits.AddCredits(ctx, userID, 100, "test", "", "", "")

	for i := 0; i < 3; i++ {
		if _, err := env.svc.CreateStyleGeneration(ctx, userID, style.ID, fileKey, fmt.Sprintf("gate-%d", i)); err != nil {
			t.Fatalf("create %d failed: %v", i, err)
		}
	}

	claimed, err := env.svc.ClaimBatch(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimBatch failed: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("a free user may only hold 1 slot, got %d", len(claimed))
	}

	// A second user is not blocked by the first user's slot.
	otherID := uint64(2)
	otherKey := "style_input/gate/other.png"
	seedInputObject(t, env.storage, otherID, otherKey)
	env.credits.AddCredits(ctx, otherID, 100, "test", "", "", "")
	if _, err := env.svc.CreateStyleGeneration(ctx, otherID, style.ID, otherKey, "gate-other"); err != nil {
		t.Fatalf("other user create failed: %v", err)
	}
	claimed, err = env.svc.ClaimBatch(ctx, 10)
	if err != nil {
		t.Fatalf("second ClaimBatch failed: %v", err)
	}
	if len(claimed) != 1 || claimed[0].UserID != otherID {
		t.Fatalf("expected only the second user's task to be claimed, got %d tasks", len(claimed))
	}
}

func TestDispatcher_RespectsGlobalConcurrency(t *testing.T) {
	const (
		maxConcurrency = 3
		userCount      = 6
	)
	env := newAITestEnv(t, ai_generation.Config{
		TaskExpireMinutes: 30,
		FreeConcurrent:    1,
		FreeQueueSize:     5,
	})
	ctx := context.Background()
	style := seedAIStyle(t)

	var inFlight, peak atomic.Int64
	release := make(chan struct{})
	env.provider.submit = func(ctx context.Context, _ *ai_generation.SubmitRequest) (*ai_generation.Result, error) {
		current := inFlight.Add(1)
		for {
			observed := peak.Load()
			if current <= observed || peak.CompareAndSwap(observed, current) {
				break
			}
		}
		<-release
		inFlight.Add(-1)
		return &ai_generation.Result{
			Status:     ai_generation.StatusSucceeded,
			ImageBytes: []byte("generated-png-bytes"),
			ImageMIME:  "image/png",
		}, nil
	}

	// One task per user so the per-user gate never masks the global ceiling.
	for i := 1; i <= userCount; i++ {
		userID := uint64(i)
		fileKey := fmt.Sprintf("style_input/pool/%d.png", i)
		seedInputObject(t, env.storage, userID, fileKey)
		env.credits.AddCredits(ctx, userID, 100, "test", "", "", "")
		if _, err := env.svc.CreateStyleGeneration(ctx, userID, style.ID, fileKey, fmt.Sprintf("pool-%d", i)); err != nil {
			t.Fatalf("create for user %d failed: %v", i, err)
		}
	}

	dispatcher := ai_generation.NewDispatcher(env.svc, ai_generation.DispatcherConfig{
		MaxConcurrency: maxConcurrency,
		BatchSize:      50,
		Interval:       10 * time.Millisecond,
		TaskTimeout:    10 * time.Second,
	})
	env.svc.SetDispatchNotifier(dispatcher.Notify)
	// Shared-cache SQLite raises "table is locked" instead of waiting, so the
	// test database must take one writer at a time. Only the provider calls need
	// to overlap for the ceiling to be observable.
	serializeTestDBWrites(t)
	dispatcher.Start()

	waitFor(t, 3*time.Second, func() bool { return inFlight.Load() == maxConcurrency })
	// Hold the slots long enough that a leak would show up as a higher peak.
	time.Sleep(100 * time.Millisecond)
	if got := inFlight.Load(); got != maxConcurrency {
		t.Fatalf("in-flight drifted to %d while the pool was saturated", got)
	}
	close(release)

	waitFor(t, 5*time.Second, func() bool {
		var done int64
		db.DB.Model(&model.AIGeneration{}).
			Where("status = ?", model.AIGenStatusSucceeded).Count(&done)
		return done == userCount
	})
	dispatcher.Stop()

	if peak.Load() > maxConcurrency {
		t.Fatalf("peak in-flight %d exceeded the ceiling %d", peak.Load(), maxConcurrency)
	}
	if env.provider.calls.Load() != userCount {
		t.Fatalf("expected %d provider calls, got %d", userCount, env.provider.calls.Load())
	}
}

func TestExecuteTask_FailureRefundsOnce(t *testing.T) {
	env := newAITestEnv(t, ai_generation.Config{TaskExpireMinutes: 30})
	ctx := context.Background()
	userID := uint64(1)

	style := seedAIStyle(t)
	fileKey := "style_input/fail/input.png"
	seedInputObject(t, env.storage, userID, fileKey)
	env.credits.AddCredits(ctx, userID, 10, "test", "", "", "")

	env.provider.submit = func(context.Context, *ai_generation.SubmitRequest) (*ai_generation.Result, error) {
		return &ai_generation.Result{
			Status:    ai_generation.StatusFailed,
			ErrorCode: "content_policy",
			ErrorMsg:  "vendor detail that must stay out of the client response",
		}, nil
	}

	created, err := env.svc.CreateStyleGeneration(ctx, userID, style.ID, fileKey, "fail-req")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if balance, _ := env.credits.GetBalance(ctx, userID); balance != 8 {
		t.Fatalf("expected 2 credits held, balance=%d", balance)
	}

	env.drain(t, 10)

	task, err := env.svc.GetStyleGeneration(ctx, userID, created.TaskID)
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	if task.Status != model.AIGenStatusFailed {
		t.Fatalf("expected failed status, got %d", task.Status)
	}
	if task.RefundedAt == nil {
		t.Fatal("a failed task must record its refund")
	}
	if task.ErrorMessage == "" || strings.Contains(task.ErrorMessage, "vendor detail") {
		t.Fatalf("error_message must be a user-facing string, got %q", task.ErrorMessage)
	}
	if balance, _ := env.credits.GetBalance(ctx, userID); balance != 10 {
		t.Fatalf("expected credits refunded, balance=%d", balance)
	}

	// The reaper racing a worker must not pay the same task back twice.
	env.svc.FailTask(ctx, created.TaskID, "worker_lost", "生成中断，请重新发起")
	if balance, _ := env.credits.GetBalance(ctx, userID); balance != 10 {
		t.Fatalf("refund must be idempotent, balance=%d", balance)
	}
}

// A provider timeout cancels the very ctx settlement would run on. The refund
// must still land, otherwise the task sits in "generating" until the
// stuck-running reaper notices minutes later.
func TestExecuteTask_ProviderTimeoutStillSettles(t *testing.T) {
	env := newAITestEnv(t, ai_generation.Config{TaskExpireMinutes: 30})
	ctx := context.Background()
	userID := uint64(1)

	style := seedAIStyle(t)
	fileKey := "style_input/timeout/input.png"
	seedInputObject(t, env.storage, userID, fileKey)
	env.credits.AddCredits(ctx, userID, 10, "test", "", "", "")

	env.provider.submit = func(ctx context.Context, _ *ai_generation.SubmitRequest) (*ai_generation.Result, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	created, err := env.svc.CreateStyleGeneration(ctx, userID, style.ID, fileKey, "timeout-req")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if _, err := env.svc.ClaimBatch(ctx, 10); err != nil {
		t.Fatalf("claim failed: %v", err)
	}

	taskCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	env.svc.ExecuteTask(taskCtx, created.TaskID)

	task, err := env.svc.GetStyleGeneration(ctx, userID, created.TaskID)
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	if task.Status != model.AIGenStatusFailed {
		t.Fatalf("a timed-out task must reach a terminal state, got status=%d", task.Status)
	}
	if task.RefundedAt == nil {
		t.Fatal("a timed-out task must record its refund")
	}
	if balance, _ := env.credits.GetBalance(ctx, userID); balance != 10 {
		t.Fatalf("expected credits refunded, balance=%d", balance)
	}
}

func TestReapTasks_ExpiredAndStuck(t *testing.T) {
	env := newAITestEnv(t, ai_generation.Config{TaskExpireMinutes: 30, StuckRunningMinutes: 15})
	ctx := context.Background()
	style := seedAIStyle(t)

	expiredUser := uint64(1)
	expiredKey := "style_input/reap/expired.png"
	seedInputObject(t, env.storage, expiredUser, expiredKey)
	env.credits.AddCredits(ctx, expiredUser, 10, "test", "", "", "")
	expired, err := env.svc.CreateStyleGeneration(ctx, expiredUser, style.ID, expiredKey, "reap-expired")
	if err != nil {
		t.Fatalf("create expired task failed: %v", err)
	}
	past := time.Now().Add(-time.Minute)
	db.DB.Model(&model.AIGeneration{}).Where("task_id = ?", expired.TaskID).
		Update("expired_at", past)

	stuckUser := uint64(2)
	stuckKey := "style_input/reap/stuck.png"
	seedInputObject(t, env.storage, stuckUser, stuckKey)
	env.credits.AddCredits(ctx, stuckUser, 10, "test", "", "", "")
	stuck, err := env.svc.CreateStyleGeneration(ctx, stuckUser, style.ID, stuckKey, "reap-stuck")
	if err != nil {
		t.Fatalf("create stuck task failed: %v", err)
	}
	db.DB.Model(&model.AIGeneration{}).Where("task_id = ?", stuck.TaskID).
		Updates(map[string]interface{}{
			"status":     model.AIGenStatusRunning,
			"started_at": time.Now().Add(-30 * time.Minute),
		})

	if err := env.svc.ReapTasks(ctx); err != nil {
		t.Fatalf("ReapTasks failed: %v", err)
	}

	expiredTask, _ := env.svc.GetStyleGeneration(ctx, expiredUser, expired.TaskID)
	if expiredTask.Status != model.AIGenStatusExpired {
		t.Fatalf("expected expired status, got %d", expiredTask.Status)
	}
	if balance, _ := env.credits.GetBalance(ctx, expiredUser); balance != 10 {
		t.Fatalf("expired task must be refunded, balance=%d", balance)
	}

	stuckTask, _ := env.svc.GetStyleGeneration(ctx, stuckUser, stuck.TaskID)
	if stuckTask.Status != model.AIGenStatusFailed {
		t.Fatalf("expected failed status for a lost worker, got %d", stuckTask.Status)
	}
	if balance, _ := env.credits.GetBalance(ctx, stuckUser); balance != 10 {
		t.Fatalf("stuck task must be refunded, balance=%d", balance)
	}
}

// TestReapTasks_LeavesFreshRunningAlone guards the boundary the reaper depends
// on: started_at is rewritten on claim, so a task that is merely slow must not
// be refunded out from under an in-flight provider call.
func TestReapTasks_LeavesFreshRunningAlone(t *testing.T) {
	env := newAITestEnv(t, ai_generation.Config{TaskExpireMinutes: 30, StuckRunningMinutes: 15})
	ctx := context.Background()
	userID := uint64(1)

	style := seedAIStyle(t)
	fileKey := "style_input/reap/fresh.png"
	seedInputObject(t, env.storage, userID, fileKey)
	env.credits.AddCredits(ctx, userID, 10, "test", "", "", "")
	created, err := env.svc.CreateStyleGeneration(ctx, userID, style.ID, fileKey, "reap-fresh")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if _, err := dao.NewAIGenerationDAO().ClaimPendingTask(ctx, created.TaskID, time.Now()); err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	// Past its queue deadline, but already running: the expiry sweep must skip it.
	db.DB.Model(&model.AIGeneration{}).Where("task_id = ?", created.TaskID).
		Update("expired_at", time.Now().Add(-time.Minute))

	if err := env.svc.ReapTasks(ctx); err != nil {
		t.Fatalf("ReapTasks failed: %v", err)
	}

	task, _ := env.svc.GetStyleGeneration(ctx, userID, created.TaskID)
	if task.Status != model.AIGenStatusRunning {
		t.Fatalf("a freshly claimed task must stay running, got %d", task.Status)
	}
	if balance, _ := env.credits.GetBalance(ctx, userID); balance != 8 {
		t.Fatalf("an in-flight task must not be refunded, balance=%d", balance)
	}
}

func TestExecuteTask_LateWorkerDoesNotOverwriteReapedTask(t *testing.T) {
	env := newAITestEnv(t, ai_generation.Config{TaskExpireMinutes: 30, StuckRunningMinutes: 15})
	ctx := context.Background()
	userID := uint64(1)

	style := seedAIStyle(t)
	fileKey := "style_input/late/input.png"
	seedInputObject(t, env.storage, userID, fileKey)
	env.credits.AddCredits(ctx, userID, 10, "test", "", "", "")
	created, err := env.svc.CreateStyleGeneration(ctx, userID, style.ID, fileKey, "late-req")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	claimed := env.drainAsyncClaim(t)
	if len(claimed) != 1 {
		t.Fatalf("expected 1 claimed task, got %d", len(claimed))
	}
	// The reaper decides the worker is gone and refunds while it is still running.
	env.svc.FailTask(ctx, created.TaskID, "worker_lost", "生成中断，请重新发起")
	// The worker then finishes anyway.
	env.svc.ExecuteTask(ctx, created.TaskID)

	task, _ := env.svc.GetStyleGeneration(ctx, userID, created.TaskID)
	if task.Status != model.AIGenStatusFailed {
		t.Fatalf("the reaper's terminal state must win, got %d", task.Status)
	}
	if balance, _ := env.credits.GetBalance(ctx, userID); balance != 10 {
		t.Fatalf("expected exactly one refund, balance=%d", balance)
	}
}

// drainAsyncClaim claims without executing, leaving the tasks running so a test
// can interleave a reaper decision.
func (e *aiTestEnv) drainAsyncClaim(t *testing.T) []*model.AIGeneration {
	t.Helper()
	claimed, err := e.svc.ClaimBatch(context.Background(), 10)
	if err != nil {
		t.Fatalf("ClaimBatch failed: %v", err)
	}
	return claimed
}

// The registry no longer has a fallback entry: an unknown key must be an error
// so the submit path can reject the request before charging credits.
func TestRegistry_ResolveByModelKey(t *testing.T) {
	registry := ai_generation.NewRegistry(&stubProvider{name: "fake"}, &stubProvider{name: "gemini-3-1-flash-image-preview"})

	resolved, err := registry.Resolve("gemini-3-1-flash-image-preview")
	if err != nil {
		t.Fatalf("resolve by name failed: %v", err)
	}
	if resolved.Name() != "gemini-3-1-flash-image-preview" {
		t.Fatalf("expected gemini-3-1-flash-image-preview, got %s", resolved.Name())
	}

	for _, key := range []string{"", "nope"} {
		if _, err := registry.Resolve(key); err == nil {
			t.Fatalf("resolving %q must be an error, not a nil provider", key)
		}
	}
}

// Resolution order is bb_config > bb_ai_style.provider > yaml default, and the
// key is pinned onto the task row so a config change mid-flight cannot move a
// task that has already been charged.
func TestCreateStyleGeneration_ResolvesModelInOrder(t *testing.T) {
	ctx := context.Background()
	userID := uint64(1)

	cases := []struct {
		name        string
		activeModel string
		styleModel  string
		want        string
	}{
		{"bb_config wins", "gemini-3-1-flash-image-preview", "fake", "gemini-3-1-flash-image-preview"},
		{"blank bb_config falls back to the style row", "   ", "fake", "fake"},
		{"no style provider falls back to the yaml default", "", "", "gemini-3-1-flash-image-preview"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newAITestEnv(t, ai_generation.Config{
				TaskExpireMinutes: 30,
				DefaultModel:      "gemini-3-1-flash-image-preview",
			})
			if tc.activeModel != "" {
				db.DB.Create(&model.Config{ConfigKey: ai_generation.ActiveModelConfigKey, ConfigValue: tc.activeModel})
			}
			style := seedAIStyle(t)
			db.DB.Model(style).Update("provider", tc.styleModel)
			fileKey := "style_input/resolve/input.png"
			seedInputObject(t, env.storage, userID, fileKey)
			env.credits.AddCredits(ctx, userID, 10, "test", "", "", "")

			created, err := env.svc.CreateStyleGeneration(ctx, userID, style.ID, fileKey, tc.name)
			if err != nil {
				t.Fatalf("CreateStyleGeneration failed: %v", err)
			}

			var task model.AIGeneration
			db.DB.Where("task_id = ?", created.TaskID).First(&task)
			if task.Provider != tc.want {
				t.Fatalf("pinned provider = %q, want %q", task.Provider, tc.want)
			}
		})
	}
}

func TestCreateStyleGeneration_UnconfiguredProviderDoesNotCharge(t *testing.T) {
	env := newAITestEnv(t, ai_generation.Config{TaskExpireMinutes: 30})
	ctx := context.Background()
	userID := uint64(1)

	style := seedAIStyle(t)
	db.DB.Model(style).Update("provider", "not-configured")
	fileKey := "style_input/unconfigured/input.png"
	seedInputObject(t, env.storage, userID, fileKey)
	env.credits.AddCredits(ctx, userID, 10, "test", "", "", "")

	if _, err := env.svc.CreateStyleGeneration(ctx, userID, style.ID, fileKey, "unconfigured"); err == nil {
		t.Fatal("expected submission to be rejected before charging credits")
	}
	if balance, _ := env.credits.GetBalance(ctx, userID); balance != 10 {
		t.Fatalf("credits must not be charged, balance=%d", balance)
	}
}

// failedTaskForRetry submits a task and drives it to the failed state, which is
// the only state a retry is allowed to start from.
func failedTaskForRetry(t *testing.T, svc *ai_generation.Service, credits *credit.Service, userID, styleID uint64, fileKey, requestID string) string {
	t.Helper()
	ctx := context.Background()
	created, err := svc.CreateStyleGeneration(ctx, userID, styleID, fileKey, requestID)
	if err != nil {
		t.Fatalf("seed original task: %v", err)
	}
	svc.FailTask(ctx, created.TaskID, "provider_failed", "生成失败，请稍后重试")
	task, err := svc.GetStyleGeneration(ctx, userID, created.TaskID)
	if err != nil {
		t.Fatalf("reload seeded task: %v", err)
	}
	if task.Status != model.AIGenStatusFailed {
		t.Fatalf("seeded task must be failed, got status=%d", task.Status)
	}
	// FailTask refunds, so the retry starts from the pre-charge balance.
	if balance, _ := credits.GetBalance(ctx, userID); balance != 10 {
		t.Fatalf("expected refund to restore balance to 10, got %d", balance)
	}
	return created.TaskID
}

func TestRetryStyleGeneration_Success(t *testing.T) {
	aiService, creditService := setupAIService(t)
	ctx := context.Background()
	userID := uint64(1)

	style := seedAIStyle(t)
	fileKey := seedUploadedMedia(t, userID)
	creditService.AddCredits(ctx, userID, 10, "test", "", "", "")
	originalID := failedTaskForRetry(t, aiService, creditService, userID, style.ID, fileKey, "retry-origin")

	result, err := aiService.RetryStyleGeneration(ctx, userID, originalID, "retry-001")
	if err != nil {
		t.Fatalf("RetryStyleGeneration failed: %v", err)
	}
	if result.TaskID == originalID {
		t.Error("retry must create a new task, not reuse the original id")
	}
	if result.Status != model.AIGenStatusPending {
		t.Errorf("expected new task to be pending, got %d", result.Status)
	}
	if result.CreditsDeducted != 2 {
		t.Errorf("expected 2 credits deducted, got %d", result.CreditsDeducted)
	}
	if result.RemainingBalance != 8 {
		t.Errorf("expected remaining_balance=8, got %d", result.RemainingBalance)
	}

	retried, err := aiService.GetStyleGeneration(ctx, userID, result.TaskID)
	if err != nil {
		t.Fatalf("load retried task: %v", err)
	}
	if retried.InputFileKey != fileKey {
		t.Errorf("expected input_file_key reused, got %s", retried.InputFileKey)
	}
	if retried.StyleID != style.ID {
		t.Errorf("expected style_id=%d reused, got %d", style.ID, retried.StyleID)
	}

	// The original stays failed: a retry is a new row, not an in-place reset.
	original, err := aiService.GetStyleGeneration(ctx, userID, originalID)
	if err != nil {
		t.Fatalf("load original task: %v", err)
	}
	if original.Status != model.AIGenStatusFailed {
		t.Errorf("expected original to stay failed, got %d", original.Status)
	}
}

// A user-initiated retry must not be handed back to the model that just failed.
// The retry model deliberately outranks ai_active_model: that key only governs
// first attempts, so letting it win here would make retry_model dead config.
func TestRetryStyleGeneration_ResolvesRetryModel(t *testing.T) {
	ctx := context.Background()
	userID := uint64(1)

	cases := []struct {
		name             string
		yamlRetryModel   string
		configRetryModel string
		activeModel      string
		want             string
	}{
		{"yaml retry model serves the retry", "gemini-3-1-flash-image-preview", "", "", "gemini-3-1-flash-image-preview"},
		{"retry model beats the global active model", "gemini-3-1-flash-image-preview", "", "fake", "gemini-3-1-flash-image-preview"},
		{"bb_config beats the yaml retry model", "fake", "gemini-3-1-flash-image-preview", "", "gemini-3-1-flash-image-preview"},
		// Refusing the retry would leave the user with no way forward, so an
		// unusable retry model degrades to the first-attempt chain.
		{"unconfigured retry model degrades", "not-configured", "", "", "fake"},
		{"no retry model keeps the first-attempt model", "", "", "", "fake"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newAITestEnv(t, ai_generation.Config{
				TaskExpireMinutes: 30,
				DefaultModel:      "fake",
				RetryModel:        tc.yamlRetryModel,
			})
			if tc.configRetryModel != "" {
				db.DB.Create(&model.Config{ConfigKey: ai_generation.RetryModelConfigKey, ConfigValue: tc.configRetryModel})
			}
			if tc.activeModel != "" {
				db.DB.Create(&model.Config{ConfigKey: ai_generation.ActiveModelConfigKey, ConfigValue: tc.activeModel})
			}
			style := seedAIStyle(t)
			fileKey := seedUploadedMedia(t, userID)
			env.credits.AddCredits(ctx, userID, 10, "test", "", "", "")
			originalID := failedTaskForRetry(t, env.svc, env.credits, userID, style.ID, fileKey, "retry-origin")

			result, err := env.svc.RetryStyleGeneration(ctx, userID, originalID, "retry-model")
			if err != nil {
				t.Fatalf("RetryStyleGeneration failed: %v", err)
			}
			var task model.AIGeneration
			db.DB.Where("task_id = ?", result.TaskID).First(&task)
			if task.Provider != tc.want {
				t.Fatalf("retry pinned provider = %q, want %q", task.Provider, tc.want)
			}
		})
	}
}

// retry_model must not leak into first submissions: they all start on the
// first-attempt model.
func TestCreateStyleGeneration_IgnoresRetryModel(t *testing.T) {
	ctx := context.Background()
	userID := uint64(1)

	env := newAITestEnv(t, ai_generation.Config{
		TaskExpireMinutes: 30,
		DefaultModel:      "fake",
		RetryModel:        "gemini-3-1-flash-image-preview",
	})
	style := seedAIStyle(t)
	fileKey := seedUploadedMedia(t, userID)
	env.credits.AddCredits(ctx, userID, 10, "test", "", "", "")

	created, err := env.svc.CreateStyleGeneration(ctx, userID, style.ID, fileKey, "first-attempt")
	if err != nil {
		t.Fatalf("CreateStyleGeneration failed: %v", err)
	}
	var task model.AIGeneration
	db.DB.Where("task_id = ?", created.TaskID).First(&task)
	if task.Provider != "fake" {
		t.Fatalf("first attempt pinned provider = %q, want %q", task.Provider, "fake")
	}
}

func TestRetryStyleGeneration_RejectsNonRetryableStatus(t *testing.T) {
	aiService, creditService := setupAIService(t)
	ctx := context.Background()
	userID := uint64(1)

	style := seedAIStyle(t)
	fileKey := seedUploadedMedia(t, userID)
	creditService.AddCredits(ctx, userID, 10, "test", "", "", "")

	created, err := aiService.CreateStyleGeneration(ctx, userID, style.ID, fileKey, "pending-origin")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	balanceAfterCreate, _ := creditService.GetBalance(ctx, userID)

	// Pending: still in flight, so a retry would pay twice for one image.
	if _, err := aiService.RetryStyleGeneration(ctx, userID, created.TaskID, "retry-pending"); err == nil {
		t.Error("expected pending task to be rejected")
	}

	db.DB.Model(&model.AIGeneration{}).Where("task_id = ?", created.TaskID).
		Update("status", model.AIGenStatusSucceeded)
	if _, err := aiService.RetryStyleGeneration(ctx, userID, created.TaskID, "retry-succeeded"); err == nil {
		t.Error("expected succeeded task to be rejected")
	}

	if balance, _ := creditService.GetBalance(ctx, userID); balance != balanceAfterCreate {
		t.Errorf("rejected retries must not charge, balance %d -> %d", balanceAfterCreate, balance)
	}
}

func TestRetryStyleGeneration_Ownership(t *testing.T) {
	aiService, creditService := setupAIService(t)
	ctx := context.Background()
	owner := uint64(1)
	other := uint64(999)

	style := seedAIStyle(t)
	fileKey := seedUploadedMedia(t, owner)
	creditService.AddCredits(ctx, owner, 10, "test", "", "", "")
	creditService.AddCredits(ctx, other, 10, "test", "", "", "")
	originalID := failedTaskForRetry(t, aiService, creditService, owner, style.ID, fileKey, "own-origin")

	if _, err := aiService.RetryStyleGeneration(ctx, other, originalID, "retry-other"); err == nil {
		t.Error("expected another user's retry to be rejected")
	}
	if balance, _ := creditService.GetBalance(ctx, other); balance != 10 {
		t.Errorf("rejected retry must not charge the caller, got %d", balance)
	}
}

func TestRetryStyleGeneration_Idempotent(t *testing.T) {
	aiService, creditService := setupAIService(t)
	ctx := context.Background()
	userID := uint64(1)

	style := seedAIStyle(t)
	fileKey := seedUploadedMedia(t, userID)
	creditService.AddCredits(ctx, userID, 10, "test", "", "", "")
	originalID := failedTaskForRetry(t, aiService, creditService, userID, style.ID, fileKey, "idem-origin")

	first, err := aiService.RetryStyleGeneration(ctx, userID, originalID, "retry-dup")
	if err != nil {
		t.Fatalf("first retry failed: %v", err)
	}
	second, err := aiService.RetryStyleGeneration(ctx, userID, originalID, "retry-dup")
	if err != nil {
		t.Fatalf("second retry failed: %v", err)
	}

	if first.TaskID != second.TaskID {
		t.Errorf("expected same task_id, got %s vs %s", first.TaskID, second.TaskID)
	}
	if !second.Duplicated {
		t.Error("expected duplicated=true on repeated retry")
	}
	if balance, _ := creditService.GetBalance(ctx, userID); balance != 8 {
		t.Errorf("expected balance=8 (charged once), got %d", balance)
	}
}

// serializeTestDBWrites caps the test database at a single connection.
func serializeTestDBWrites(t *testing.T) {
	t.Helper()
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}
