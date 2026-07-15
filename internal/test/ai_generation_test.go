package test

import (
	"context"
	"testing"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/service/ai_generation"
	"github.com/zhaojiabo/bobobeads_server/internal/service/credit"
	"github.com/zhaojiabo/bobobeads_server/internal/service/generation"
	"github.com/zhaojiabo/bobobeads_server/internal/service/subscribe"
	"github.com/zhaojiabo/bobobeads_server/internal/service/work"
)

func setupAIService(t *testing.T) (*ai_generation.Service, *credit.Service) {
	t.Helper()
	SetupTestDB(t)

	aiDAO := dao.NewAIGenerationDAO()
	mediaDAO := dao.NewMediaDAO()
	creditDAO := dao.NewCreditDAO()
	creditService := credit.NewService(creditDAO)
	provider := ai_generation.NewFakeProvider()

	cfg := ai_generation.Config{TaskExpireMinutes: 30}
	aiService := ai_generation.NewService(aiDAO, mediaDAO, creditService, provider, cfg)

	return aiService, creditService
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

func TestListAIStyles(t *testing.T) {
	SetupTestDB(t)
	aiDAO := dao.NewAIGenerationDAO()
	mediaDAO := dao.NewMediaDAO()
	creditDAO := dao.NewCreditDAO()
	creditService := credit.NewService(creditDAO)
	provider := ai_generation.NewFakeProvider()
	cfg := ai_generation.Config{}
	svc := ai_generation.NewService(aiDAO, mediaDAO, creditService, provider, cfg)

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
	SetupTestDB(t)
	ctx := context.Background()
	userID := uint64(1)

	// Setup services
	aiDAO := dao.NewAIGenerationDAO()
	mediaDAO := dao.NewMediaDAO()
	creditDAO := dao.NewCreditDAO()
	generationDAO := dao.NewGenerationDAO()
	workDAO := dao.NewWorkDAO()
	orderDAO := dao.NewOrderDAO()
	productDAO := dao.NewProductDAO()
	subscriptionDAO := dao.NewSubscriptionDAO()

	creditService := credit.NewService(creditDAO)
	provider := ai_generation.NewFakeProvider()
	aiCfg := ai_generation.Config{TaskExpireMinutes: 30}
	aiService := ai_generation.NewService(aiDAO, mediaDAO, creditService, provider, aiCfg)

	workService := work.NewService(workDAO)
	subscribeService := subscribe.NewService(orderDAO, productDAO, subscriptionDAO)
	genService := generation.NewService(generationDAO, creditService, subscribeService, workService)
	genService.SetAIValidator(aiService)

	// Seed data
	style := &model.AIStyle{StyleKey: "e2e", Name: "E2E Style", CostCredits: 1, Status: 1, Provider: "fake"}
	db.DB.Create(style)

	fileKey := "style_input/e2e/test.png"
	db.DB.Create(&model.MediaAsset{
		UserID: userID, FileKey: fileKey, Purpose: "style_input",
		ContentType: "image/png", Status: model.MediaStatusUploaded,
	})

	creditService.AddCredits(ctx, userID, 10, "test", "", "", "")

	// 1. Create AI style generation
	aiResult, err := aiService.CreateStyleGeneration(ctx, userID, style.ID, fileKey, "e2e-req")
	if err != nil {
		t.Fatalf("AI create failed: %v", err)
	}

	// 2. Verify AI task succeeded (fake provider completes immediately)
	task, _ := aiService.GetStyleGeneration(ctx, userID, aiResult.TaskID)
	if task.Status != model.AIGenStatusSucceeded {
		t.Fatalf("expected AI task succeeded, got status=%d", task.Status)
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
	SetupTestDB(t)
	ctx := context.Background()
	userID := uint64(1)

	aiDAO := dao.NewAIGenerationDAO()
	mediaDAO := dao.NewMediaDAO()
	creditDAO := dao.NewCreditDAO()
	generationDAO := dao.NewGenerationDAO()
	workDAO := dao.NewWorkDAO()
	orderDAO := dao.NewOrderDAO()
	productDAO := dao.NewProductDAO()
	subscriptionDAO := dao.NewSubscriptionDAO()

	creditService := credit.NewService(creditDAO)
	provider := ai_generation.NewFakeProvider()
	aiService := ai_generation.NewService(aiDAO, mediaDAO, creditService, provider, ai_generation.Config{})

	workService := work.NewService(workDAO)
	subscribeService := subscribe.NewService(orderDAO, productDAO, subscriptionDAO)
	genService := generation.NewService(generationDAO, creditService, subscribeService, workService)
	genService.SetAIValidator(aiService)

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
