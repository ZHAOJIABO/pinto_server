package test

import (
	"context"
	"testing"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/service/credit"
)

func TestAddAndDeductCredits(t *testing.T) {
	SetupTestDB(t)
	creditDAO := dao.NewCreditDAO()
	creditService := credit.NewService(creditDAO)

	ctx := context.Background()
	userID := uint64(1)

	// Initial balance should be 0
	balance, err := creditService.GetBalance(ctx, userID)
	if err != nil {
		t.Fatalf("GetBalance failed: %v", err)
	}
	if balance != 0 {
		t.Errorf("expected initial balance=0, got %d", balance)
	}

	// Add 10 credits
	err = creditService.AddCredits(ctx, userID, 10, "signup_bonus", "", "", "注册赠送")
	if err != nil {
		t.Fatalf("AddCredits failed: %v", err)
	}

	balance, _ = creditService.GetBalance(ctx, userID)
	if balance != 10 {
		t.Errorf("expected balance=10, got %d", balance)
	}

	// Deduct 3 credits
	err = creditService.DeductCredits(ctx, userID, 3, "generation", "generation", "gen-001", "生成图纸")
	if err != nil {
		t.Fatalf("DeductCredits failed: %v", err)
	}

	balance, _ = creditService.GetBalance(ctx, userID)
	if balance != 7 {
		t.Errorf("expected balance=7, got %d", balance)
	}

	t.Logf("Credits add/deduct success: balance=%d", balance)
}

func TestInsufficientCredits(t *testing.T) {
	SetupTestDB(t)
	creditDAO := dao.NewCreditDAO()
	creditService := credit.NewService(creditDAO)

	ctx := context.Background()
	userID := uint64(2)

	// Add 2 credits
	creditService.AddCredits(ctx, userID, 2, "daily", "", "", "每日赠送")

	// Try to deduct 5 - should fail
	err := creditService.DeductCredits(ctx, userID, 5, "generation", "", "", "")
	if err == nil {
		t.Error("expected error for insufficient credits")
	}

	// Balance should remain unchanged
	balance, _ := creditService.GetBalance(ctx, userID)
	if balance != 2 {
		t.Errorf("expected balance=2 (unchanged), got %d", balance)
	}

	t.Log("Insufficient credits check success")
}

func TestListTransactions(t *testing.T) {
	SetupTestDB(t)
	creditDAO := dao.NewCreditDAO()
	creditService := credit.NewService(creditDAO)

	ctx := context.Background()
	userID := uint64(3)

	creditService.AddCredits(ctx, userID, 10, "bonus", "", "", "奖励")
	creditService.DeductCredits(ctx, userID, 3, "consume", "", "", "消费")
	creditService.AddCredits(ctx, userID, 5, "invite", "", "", "邀请奖励")

	txs, total, err := creditService.ListTransactions(ctx, userID, 1, 10)
	if err != nil {
		t.Fatalf("ListTransactions failed: %v", err)
	}
	if total != 3 {
		t.Errorf("expected 3 transactions, got %d", total)
	}

	// Latest transaction first
	if txs[0].Amount != 5 {
		t.Errorf("expected first tx amount=5, got %d", txs[0].Amount)
	}
	if txs[0].Balance != 12 {
		t.Errorf("expected first tx balance=12, got %d", txs[0].Balance)
	}

	t.Logf("ListTransactions success: %d records", total)
}
