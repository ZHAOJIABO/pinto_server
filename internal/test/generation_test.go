package test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/service/credit"
	"github.com/zhaojiabo/bobobeads_server/internal/service/generation"
	"github.com/zhaojiabo/bobobeads_server/internal/service/subscribe"
	"github.com/zhaojiabo/bobobeads_server/internal/service/work"
)

func setupGenerationService(t *testing.T) (*generation.Service, *credit.Service) {
	t.Helper()
	SetupTestDB(t)

	generationDAO := dao.NewGenerationDAO()
	creditDAO := dao.NewCreditDAO()
	workDAO := dao.NewWorkDAO()
	orderDAO := dao.NewOrderDAO()
	productDAO := dao.NewProductDAO()
	subscriptionDAO := dao.NewSubscriptionDAO()

	creditService := credit.NewService(creditDAO)
	subscribeService := subscribe.NewService(orderDAO, productDAO, subscriptionDAO)
	workService := work.NewService(workDAO)
	generationService := generation.NewService(generationDAO, creditService, subscribeService, workService)

	return generationService, creditService
}

func completedWorkData(title string) *model.Work {
	pd := validPatternData(3, 3)
	return &model.Work{
		Title:            title,
		OriginalImageURL: "oss://original.jpg",
		PatternImageURL:  "oss://pattern.jpg",
		PatternData:      work.PatternDataToJSONMap(pd),
		Width:            int(pd.Width),
		Height:           int(pd.Height),
		BeadCount:        int(pd.Width * pd.Height),
		ColorCount:       len(pd.ColorPalette),
	}
}

func TestCreateGeneration_FreeQuota(t *testing.T) {
	genService, creditService := setupGenerationService(t)
	ctx := context.Background()
	userID := uint64(1)

	// First 3 generations should be free
	for i := 0; i < 3; i++ {
		clientReqID := fmt.Sprintf("req-free-%d", i)
		result, err := genService.CreateGeneration(ctx, userID, "29x29", "photo", "", clientReqID)
		if err != nil {
			t.Fatalf("CreateGeneration #%d failed: %v", i+1, err)
		}
		if result.CreditsDeducted != 0 {
			t.Errorf("generation #%d: expected 0 credits deducted (free), got %d", i+1, result.CreditsDeducted)
		}
		if result.GenerationID == "" {
			t.Error("expected non-empty generation ID")
		}
	}

	// 4th generation should require credits
	_, err := genService.CreateGeneration(ctx, userID, "29x29", "photo", "", "req-no-credit")
	if err == nil {
		t.Error("expected error for 4th generation without credits")
	}

	// Add credits and try again
	if err := creditService.AddCredits(ctx, userID, 5, "test", "", "", "测试"); err != nil {
		t.Fatalf("AddCredits failed: %v", err)
	}
	result, err := genService.CreateGeneration(ctx, userID, "29x29", "photo", "", "req-with-credit")
	if err != nil {
		t.Fatalf("4th generation with credits failed: %v", err)
	}
	if result.CreditsDeducted != 1 {
		t.Errorf("expected 1 credit deducted, got %d", result.CreditsDeducted)
	}

	balance, _ := creditService.GetBalance(ctx, userID)
	if balance != 4 {
		t.Errorf("expected balance=4 after deduction, got %d", balance)
	}

	t.Log("CreateGeneration free quota + credit deduction success")
}

func TestCreateGeneration_Idempotent(t *testing.T) {
	genService, creditService := setupGenerationService(t)
	ctx := context.Background()
	userID := uint64(100)

	if err := creditService.AddCredits(ctx, userID, 10, "test", "", "", ""); err != nil {
		t.Fatalf("AddCredits failed: %v", err)
	}

	// Exhaust free quota
	for i := 0; i < 3; i++ {
		_, err := genService.CreateGeneration(ctx, userID, "29x29", "photo", "", fmt.Sprintf("exhaust-%d", i))
		if err != nil {
			t.Fatalf("exhaust free quota #%d failed: %v", i, err)
		}
	}

	// Create generation that costs credits
	result1, err := genService.CreateGeneration(ctx, userID, "29x29", "photo", "", "idempotent-key")
	if err != nil {
		t.Fatalf("first CreateGeneration failed: %v", err)
	}

	// Retry with same client_request_id
	result2, err := genService.CreateGeneration(ctx, userID, "29x29", "photo", "", "idempotent-key")
	if err != nil {
		t.Fatalf("retry CreateGeneration failed: %v", err)
	}

	if result1.GenerationID != result2.GenerationID {
		t.Errorf("expected same generation_id, got %s vs %s", result1.GenerationID, result2.GenerationID)
	}
	if !result2.Duplicated {
		t.Error("expected duplicated=true on retry")
	}

	t.Log("CreateGeneration idempotent success")
}

func TestCreateGeneration_IdempotentConcurrent(t *testing.T) {
	t.Skip("SQLite cannot reliably exercise concurrent write/idempotency paths; run this as a MySQL integration test")
	genService, creditService := setupGenerationService(t)
	ctx := context.Background()
	userID := uint64(200)

	if err := creditService.AddCredits(ctx, userID, 10, "test", "", "", ""); err != nil {
		t.Fatalf("AddCredits failed: %v", err)
	}

	// Exhaust free quota
	for i := 0; i < 3; i++ {
		_, err := genService.CreateGeneration(ctx, userID, "29x29", "photo", "", fmt.Sprintf("concurrent-free-%d", i))
		if err != nil {
			t.Fatalf("exhaust free quota #%d failed: %v", i, err)
		}
	}

	const n = 10
	var wg sync.WaitGroup
	results := make(chan *generation.CreateResult, n)
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := genService.CreateGeneration(ctx, userID, "29x29", "photo", "", "same-key")
			if err != nil {
				errs <- err
				return
			}
			results <- r
		}()
	}

	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent CreateGeneration failed: %v", err)
	}

	var first string
	count := 0
	for r := range results {
		if first == "" {
			first = r.GenerationID
		}
		if r.GenerationID != first {
			t.Fatalf("expected same generation id, got %s vs %s", first, r.GenerationID)
		}
		count++
	}
	if count != n {
		t.Fatalf("expected %d results, got %d", n, count)
	}

	balance, _ := creditService.GetBalance(ctx, userID)
	if balance != 9 {
		t.Fatalf("expected only one credit deducted, balance=9, got %d", balance)
	}
}

func TestCompleteGeneration(t *testing.T) {
	genService, creditService := setupGenerationService(t)
	ctx := context.Background()
	userID := uint64(10)

	// Create a generation
	result, err := genService.CreateGeneration(ctx, userID, "29x29", "photo", "", "complete-test")
	if err != nil {
		t.Fatalf("CreateGeneration failed: %v", err)
	}

	// Complete it with work data
	workData := &model.Work{
		Title:            "完成的图纸",
		OriginalImageURL: "oss://original.jpg",
		PatternImageURL:  "oss://pattern.jpg",
		PatternData:      work.PatternDataToJSONMap(validPatternData(29, 29)),
		Width:            29,
		Height:           29,
		BeadCount:        841,
		ColorCount:       8,
	}
	completeResult, err := genService.CompleteGeneration(ctx, userID, result.GenerationID, workData)
	if err != nil {
		t.Fatalf("CompleteGeneration failed: %v", err)
	}
	if completeResult.WorkID == 0 {
		t.Error("expected work_id > 0")
	}

	// Verify generation status
	gen, _ := genService.GetStatus(ctx, result.GenerationID)
	if gen.Status != model.GenerationStatusCompleted {
		t.Errorf("expected status=completed(1), got %d", gen.Status)
	}
	if gen.WorkID != completeResult.WorkID {
		t.Errorf("expected work_id=%d, got %d", completeResult.WorkID, gen.WorkID)
	}

	_ = creditService
	t.Logf("CompleteGeneration success: work_id=%d", completeResult.WorkID)
}

func TestCompleteGeneration_Idempotent(t *testing.T) {
	genService, _ := setupGenerationService(t)
	ctx := context.Background()
	userID := uint64(30)

	result, err := genService.CreateGeneration(ctx, userID, "29x29", "photo", "", "double-complete")
	if err != nil {
		t.Fatalf("CreateGeneration failed: %v", err)
	}

	// First complete - success
	r1, err := genService.CompleteGeneration(ctx, userID, result.GenerationID, completedWorkData("test"))
	if err != nil {
		t.Fatalf("first CompleteGeneration failed: %v", err)
	}

	// Second complete - should return same work_id with duplicated=true
	r2, err := genService.CompleteGeneration(ctx, userID, result.GenerationID, completedWorkData("test2"))
	if err != nil {
		t.Fatalf("second CompleteGeneration failed: %v", err)
	}
	if r2.WorkID != r1.WorkID {
		t.Errorf("expected same work_id, got %d vs %d", r1.WorkID, r2.WorkID)
	}
	if !r2.Duplicated {
		t.Error("expected duplicated=true on second complete")
	}

	t.Log("CompleteGeneration idempotent success")
}

func TestCompleteGeneration_RequiresPatternData(t *testing.T) {
	genService, _ := setupGenerationService(t)
	ctx := context.Background()
	userID := uint64(31)

	result, err := genService.CreateGeneration(ctx, userID, "29x29", "photo", "", "missing-pattern")
	if err != nil {
		t.Fatalf("CreateGeneration failed: %v", err)
	}

	_, err = genService.CompleteGeneration(ctx, userID, result.GenerationID, &model.Work{Title: "missing pattern"})
	if err == nil {
		t.Fatal("expected error for missing pattern_data")
	}
}

func TestCancelGeneration_WithRefund(t *testing.T) {
	genService, creditService := setupGenerationService(t)
	ctx := context.Background()
	userID := uint64(20)

	// Add credits and exhaust free quota
	if err := creditService.AddCredits(ctx, userID, 10, "test", "", "", ""); err != nil {
		t.Fatalf("AddCredits failed: %v", err)
	}
	for i := 0; i < 3; i++ {
		_, err := genService.CreateGeneration(ctx, userID, "29x29", "photo", "", fmt.Sprintf("cancel-free-%d", i))
		if err != nil {
			t.Fatalf("exhaust free quota #%d failed: %v", i, err)
		}
	}

	// This one costs 1 credit
	result, err := genService.CreateGeneration(ctx, userID, "29x29", "photo", "", "cancel-paid")
	if err != nil {
		t.Fatalf("CreateGeneration paid failed: %v", err)
	}
	balanceAfterDeduct, _ := creditService.GetBalance(ctx, userID)

	// Cancel - should refund
	refunded, err := genService.CancelGeneration(ctx, userID, result.GenerationID, "test cancel")
	if err != nil {
		t.Fatalf("CancelGeneration failed: %v", err)
	}
	if refunded != 1 {
		t.Errorf("expected refund=1, got %d", refunded)
	}

	balanceAfterRefund, _ := creditService.GetBalance(ctx, userID)
	if balanceAfterRefund != balanceAfterDeduct+1 {
		t.Errorf("expected balance=%d after refund, got %d", balanceAfterDeduct+1, balanceAfterRefund)
	}

	// Verify status
	gen, _ := genService.GetStatus(ctx, result.GenerationID)
	if gen.Status != model.GenerationStatusCancelled {
		t.Errorf("expected status=cancelled(2), got %d", gen.Status)
	}

	t.Logf("CancelGeneration refund success: refunded=%d, balance=%d", refunded, balanceAfterRefund)
}

func TestCancelGeneration_AfterComplete(t *testing.T) {
	genService, _ := setupGenerationService(t)
	ctx := context.Background()
	userID := uint64(40)

	result, err := genService.CreateGeneration(ctx, userID, "29x29", "photo", "", "cancel-after-complete")
	if err != nil {
		t.Fatalf("CreateGeneration failed: %v", err)
	}
	_, err = genService.CompleteGeneration(ctx, userID, result.GenerationID, completedWorkData("test"))
	if err != nil {
		t.Fatalf("CompleteGeneration failed: %v", err)
	}

	// Cancel after complete - should fail
	_, err = genService.CancelGeneration(ctx, userID, result.GenerationID, "too late")
	if err == nil {
		t.Error("expected error for cancel after complete")
	}

	t.Log("Cancel after complete rejected correctly")
}

func TestExpireTimeoutGenerations(t *testing.T) {
	genService, creditService := setupGenerationService(t)
	ctx := context.Background()
	userID := uint64(50)

	if err := creditService.AddCredits(ctx, userID, 10, "test", "", "", ""); err != nil {
		t.Fatalf("AddCredits failed: %v", err)
	}
	// Exhaust free quota
	for i := 0; i < 3; i++ {
		_, err := genService.CreateGeneration(ctx, userID, "29x29", "photo", "", fmt.Sprintf("expire-free-%d", i))
		if err != nil {
			t.Fatalf("exhaust free quota #%d failed: %v", i, err)
		}
	}

	// Create generation that costs credits
	result, err := genService.CreateGeneration(ctx, userID, "29x29", "photo", "", "expire-paid")
	if err != nil {
		t.Fatalf("CreateGeneration paid failed: %v", err)
	}
	balanceBefore, _ := creditService.GetBalance(ctx, userID)

	// Manually expire the generation by updating expired_at
	genDAO := dao.NewGenerationDAO()
	genDAO.UpdateStatus(ctx, result.GenerationID, model.GenerationStatusPending, map[string]interface{}{
		"expired_at": "2020-01-01 00:00:00",
	})

	// Run expiry task
	err = genService.ExpireTimeoutGenerations(ctx)
	if err != nil {
		t.Fatalf("ExpireTimeoutGenerations failed: %v", err)
	}

	// Verify status
	gen, _ := genService.GetStatus(ctx, result.GenerationID)
	if gen.Status != model.GenerationStatusExpired {
		t.Errorf("expected status=expired(3), got %d", gen.Status)
	}

	// Verify credits refunded
	balanceAfter, _ := creditService.GetBalance(ctx, userID)
	if balanceAfter != balanceBefore+1 {
		t.Errorf("expected balance=%d after expiry refund, got %d", balanceBefore+1, balanceAfter)
	}

	t.Logf("ExpireTimeoutGenerations success: balance %d → %d", balanceBefore, balanceAfter)
}
