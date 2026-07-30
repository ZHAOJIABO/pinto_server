package test

import (
	"context"
	"testing"

	"github.com/zhaojiabo/bobobeads_server/conf"
	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/service/auth"
	"github.com/zhaojiabo/bobobeads_server/internal/service/user"
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
	user, tokens, err := authService.GuestLoginWithDevice(ctx, auth.GuestLoginParams{
		Platform:  "android",
		AndroidID: "device123456",
	})
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
	if user.DeviceID == "device123456" || user.Nickname == "用户device" {
		t.Error("guest device identifier must not be persisted in readable form")
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

func TestGuestLogin_UsesDeterministicPublicUserID(t *testing.T) {
	SetupTestDB(t)
	authService := auth.NewService(dao.NewUserDAO())

	user, _, err := authService.GuestLoginWithDevice(context.Background(), auth.GuestLoginParams{
		Platform:  "android",
		AndroidID: "android-123",
	})
	if err != nil {
		t.Fatalf("GuestLoginWithDevice failed: %v", err)
	}

	if got, want := user.PublicUserID(), "8635871597563"; got != want {
		t.Errorf("UserID = %q, want %q", got, want)
	}
}

func TestGuestLogin_UsesPlatformSpecificFallbackIdentifiers(t *testing.T) {
	SetupTestDB(t)
	authService := auth.NewService(dao.NewUserDAO())
	cases := []struct {
		name   string
		params auth.GuestLoginParams
		want   string
	}{
		{
			name:   "android falls back to oaid",
			params: auth.GuestLoginParams{Platform: "android", OAID: "oaid-123"},
			want:   "8639299916114",
		},
		{
			name:   "ios falls back to idfa",
			params: auth.GuestLoginParams{Platform: "ios", IDFA: "idfa-123"},
			want:   "8629062758246",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user, _, err := authService.GuestLoginWithDevice(context.Background(), tc.params)
			if err != nil {
				t.Fatalf("GuestLoginWithDevice failed: %v", err)
			}
			if got := user.PublicUserID(); got != tc.want {
				t.Errorf("UserID = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGuestLogin_ReusesUserForSameDevice(t *testing.T) {
	SetupTestDB(t)
	authService := auth.NewService(dao.NewUserDAO())
	params := auth.GuestLoginParams{Platform: "ios", IDFV: "ios-device-123"}

	first, _, err := authService.GuestLoginWithDevice(context.Background(), params)
	if err != nil {
		t.Fatalf("first GuestLoginWithDevice failed: %v", err)
	}
	second, _, err := authService.GuestLoginWithDevice(context.Background(), params)
	if err != nil {
		t.Fatalf("second GuestLoginWithDevice failed: %v", err)
	}

	if first.ID != second.ID {
		t.Errorf("internal ID changed: %d != %d", first.ID, second.ID)
	}
	if got, want := second.PublicUserID(), "8621057774721"; got != want {
		t.Errorf("UserID = %q, want %q", got, want)
	}
}

func TestGuestLogin_SeparatesPlatformsWithTheSameDeviceIdentifier(t *testing.T) {
	SetupTestDB(t)
	authService := auth.NewService(dao.NewUserDAO())

	android, _, err := authService.GuestLoginWithDevice(context.Background(), auth.GuestLoginParams{
		Platform:  "android",
		AndroidID: "shared-device-id",
	})
	if err != nil {
		t.Fatalf("android GuestLoginWithDevice failed: %v", err)
	}
	ios, _, err := authService.GuestLoginWithDevice(context.Background(), auth.GuestLoginParams{
		Platform: "ios",
		IDFV:     "shared-device-id",
	})
	if err != nil {
		t.Fatalf("ios GuestLoginWithDevice failed: %v", err)
	}

	if android.ID == ios.ID || android.PublicUserID() == ios.PublicUserID() {
		t.Error("the same raw identifier on different platforms must produce separate users")
	}
}

func TestGetUserByPublicID(t *testing.T) {
	SetupTestDB(t)
	userDAO := dao.NewUserDAO()
	authService := auth.NewService(userDAO)
	userService := user.NewService(userDAO)

	guest, _, err := authService.GuestLoginWithDevice(context.Background(), auth.GuestLoginParams{
		Platform:  "android",
		AndroidID: "public-id-lookup-device",
	})
	if err != nil {
		t.Fatalf("GuestLoginWithDevice failed: %v", err)
	}

	resolved, err := userService.GetUserByPublicID(context.Background(), guest.PublicUserID())
	if err != nil {
		t.Fatalf("GetUserByPublicID failed: %v", err)
	}
	if resolved.ID != guest.ID {
		t.Errorf("resolved internal ID = %d, want %d", resolved.ID, guest.ID)
	}
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
	user, tokens, err := authService.GuestLoginWithDevice(ctx, auth.GuestLoginParams{
		Platform:  "android",
		AndroidID: "device789",
	})
	if err != nil {
		t.Fatalf("GuestLogin failed: %v", err)
	}

	refreshedUser, newTokens, err := authService.RefreshToken(ctx, tokens.RefreshToken)
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}

	if newTokens.AccessToken == "" {
		t.Error("expected non-empty new access token")
	}
	if newTokens.RefreshToken == "" {
		t.Error("expected non-empty new refresh token")
	}
	if refreshedUser.ID != user.ID || refreshedUser.PublicUserID() != user.PublicUserID() {
		t.Error("expected refresh to return the original user")
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
	user, tokens, _ := authService.GuestLoginWithDevice(ctx, auth.GuestLoginParams{
		Platform:  "android",
		AndroidID: "device000",
	})

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
