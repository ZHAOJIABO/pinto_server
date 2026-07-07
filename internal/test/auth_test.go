package test

import (
	"context"
	"testing"

	"github.com/zhaojiabo/bobobeads_server/conf"
	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/service/auth"
)

func init() {
	conf.GlobalConfig = &conf.Config{
		JWT: conf.JWTConfig{
			Secret:         "test-secret-key",
			AccessExpireH:  72,
			RefreshExpireH: 720,
		},
	}
}

func TestGuestLogin(t *testing.T) {
	SetupTestDB(t)
	userDAO := dao.NewUserDAO()
	authService := auth.NewService(userDAO)

	ctx := context.Background()
	user, tokens, err := authService.GuestLogin(ctx, "device123456")
	if err != nil {
		t.Fatalf("GuestLogin failed: %v", err)
	}

	if user.ID == 0 {
		t.Error("expected user ID > 0")
	}
	if user.UUID == "" {
		t.Error("expected non-empty UUID")
	}
	if user.Nickname == "" {
		t.Error("expected non-empty nickname")
	}
	if tokens.AccessToken == "" {
		t.Error("expected non-empty access token")
	}
	if tokens.RefreshToken == "" {
		t.Error("expected non-empty refresh token")
	}
	if tokens.ExpiresIn != 72*3600 {
		t.Errorf("expected ExpiresIn=%d, got %d", 72*3600, tokens.ExpiresIn)
	}

	t.Logf("Guest login success: user_id=%d, uuid=%s", user.ID, user.UUID)
}

func TestPhoneLogin_NewUser(t *testing.T) {
	SetupTestDB(t)
	userDAO := dao.NewUserDAO()
	authService := auth.NewService(userDAO)

	ctx := context.Background()
	user, tokens, err := authService.PhoneLogin(ctx, "13800138000", "1234")
	if err != nil {
		t.Fatalf("PhoneLogin failed: %v", err)
	}

	if user.Phone != "13800138000" {
		t.Errorf("expected phone=13800138000, got %s", user.Phone)
	}
	if tokens.AccessToken == "" {
		t.Error("expected non-empty access token")
	}

	t.Logf("Phone login (new user) success: user_id=%d", user.ID)
}

func TestPhoneLogin_ExistingUser(t *testing.T) {
	SetupTestDB(t)
	userDAO := dao.NewUserDAO()
	authService := auth.NewService(userDAO)

	ctx := context.Background()

	// First login creates user
	user1, _, err := authService.PhoneLogin(ctx, "13900139000", "1234")
	if err != nil {
		t.Fatalf("first PhoneLogin failed: %v", err)
	}

	// Second login finds existing user
	user2, _, err := authService.PhoneLogin(ctx, "13900139000", "1234")
	if err != nil {
		t.Fatalf("second PhoneLogin failed: %v", err)
	}

	if user1.ID != user2.ID {
		t.Errorf("expected same user ID, got %d and %d", user1.ID, user2.ID)
	}

	t.Logf("Phone login (existing user) success: same user_id=%d", user1.ID)
}

func TestRefreshToken(t *testing.T) {
	SetupTestDB(t)
	userDAO := dao.NewUserDAO()
	authService := auth.NewService(userDAO)

	ctx := context.Background()
	_, tokens, err := authService.GuestLogin(ctx, "device789")
	if err != nil {
		t.Fatalf("GuestLogin failed: %v", err)
	}

	newTokens, err := authService.RefreshToken(ctx, tokens.RefreshToken)
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}

	if newTokens.AccessToken == "" {
		t.Error("expected non-empty new access token")
	}
	if newTokens.RefreshToken == "" {
		t.Error("expected non-empty new refresh token")
	}

	// Validate the new access token works
	userID, err := authService.ValidateAccessToken(newTokens.AccessToken)
	if err != nil {
		t.Fatalf("new access token validation failed: %v", err)
	}
	if userID == 0 {
		t.Error("expected valid user ID from refreshed token")
	}

	t.Log("RefreshToken success")
}

func TestValidateAccessToken(t *testing.T) {
	SetupTestDB(t)
	userDAO := dao.NewUserDAO()
	authService := auth.NewService(userDAO)

	ctx := context.Background()
	user, tokens, _ := authService.GuestLogin(ctx, "device000")

	userID, err := authService.ValidateAccessToken(tokens.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken failed: %v", err)
	}
	if userID != user.ID {
		t.Errorf("expected userID=%d, got %d", user.ID, userID)
	}

	// Invalid token
	_, err = authService.ValidateAccessToken("invalid.token.here")
	if err == nil {
		t.Error("expected error for invalid token")
	}

	// Refresh token should not pass as access token
	_, err = authService.ValidateAccessToken(tokens.RefreshToken)
	if err == nil {
		t.Error("expected error when using refresh token as access token")
	}

	t.Log("ValidateAccessToken success")
}
