package test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/zhaojiabo/bobobeads_server/conf"
	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
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
		Platform:        "android",
		GuestCredential: "credential-device123456",
		AndroidID:       "device123456",
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
	if user.DeviceID == "device123456" || user.GuestCredentialHash == nil || *user.GuestCredentialHash == "credential-device123456" {
		t.Error("guest credential and device identifier must not be persisted in readable form")
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

func TestGuestLogin_UsesCredentialBasedPublicUserID(t *testing.T) {
	SetupTestDB(t)
	authService := auth.NewService(dao.NewUserDAO())

	user, _, err := authService.GuestLoginWithDevice(context.Background(), auth.GuestLoginParams{
		Platform:        "android",
		GuestCredential: "credential-android-123",
		AndroidID:       "android-123",
	})
	if err != nil {
		t.Fatalf("GuestLoginWithDevice failed: %v", err)
	}

	if !strings.HasPrefix(user.PublicUserID(), "863") {
		t.Errorf("UserID = %q, want Android guest prefix", user.PublicUserID())
	}
}

func TestGuestLogin_UsesPlatformSpecificFallbackIdentifiers(t *testing.T) {
	cases := []struct {
		name           string
		platformSuffix string
		deviceID       string
		params         auth.GuestLoginParams
		legacyUserID   string
	}{
		{
			name:           "android falls back to oaid",
			platformSuffix: "3",
			deviceID:       "oaid-123",
			params:         auth.GuestLoginParams{Platform: "android", GuestCredential: "credential-oaid-123", OAID: "oaid-123"},
			legacyUserID:   "8630000000000",
		},
		{
			name:           "ios falls back to idfa",
			platformSuffix: "2",
			deviceID:       "idfa-123",
			params:         auth.GuestLoginParams{Platform: "ios", GuestCredential: "credential-idfa-123", IDFA: "idfa-123"},
			legacyUserID:   "8620000000000",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			SetupTestDB(t)
			userDAO := dao.NewUserDAO()
			authService := auth.NewService(userDAO)
			legacy := createLegacyGuest(t, userDAO, tc.platformSuffix, tc.deviceID, tc.legacyUserID)
			user, _, err := authService.GuestLoginWithDevice(context.Background(), tc.params)
			if err != nil {
				t.Fatalf("GuestLoginWithDevice failed: %v", err)
			}
			if user.ID != legacy.ID {
				t.Errorf("legacy user ID = %d, want %d", user.ID, legacy.ID)
			}
			if user.GuestCredentialHash == nil {
				t.Error("legacy user did not receive a credential hash")
			}
		})
	}
}

func TestGuestLogin_ReusesUserForSameCredential(t *testing.T) {
	SetupTestDB(t)
	authService := auth.NewService(dao.NewUserDAO())
	params := auth.GuestLoginParams{Platform: "ios", GuestCredential: "credential-ios-device-123", IDFV: "ios-device-123"}

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
	if second.PublicUserID() != first.PublicUserID() {
		t.Errorf("public UserID changed: %q != %q", second.PublicUserID(), first.PublicUserID())
	}
}

func TestGuestLogin_BindsCredentialToLegacyDeviceUser(t *testing.T) {
	SetupTestDB(t)
	userDAO := dao.NewUserDAO()
	authService := auth.NewService(userDAO)

	legacyDevice := "legacy-ios-idfv"
	legacy := createLegacyGuest(t, userDAO, "2", legacyDevice, "8620000000000")

	credential := "credential-upgraded-ios"
	upgraded, _, err := authService.GuestLoginWithDevice(context.Background(), auth.GuestLoginParams{
		Platform:        "ios",
		GuestCredential: credential,
		IDFV:            legacyDevice,
	})
	if err != nil {
		t.Fatalf("upgrade GuestLoginWithDevice failed: %v", err)
	}
	if upgraded.ID != legacy.ID || upgraded.PublicUserID() != legacy.PublicUserID() {
		t.Fatalf("upgrade returned user %d/%s, want %d/%s", upgraded.ID, upgraded.PublicUserID(), legacy.ID, legacy.PublicUserID())
	}
	if upgraded.GuestCredentialHash == nil || *upgraded.GuestCredentialHash == credential {
		t.Fatal("credential hash was not securely persisted")
	}

	recovered, _, err := authService.GuestLoginWithDevice(context.Background(), auth.GuestLoginParams{
		Platform:        "ios",
		GuestCredential: credential,
		IDFV:            "new-idfv-after-reinstall",
	})
	if err != nil {
		t.Fatalf("recovery GuestLoginWithDevice failed: %v", err)
	}
	if recovered.ID != legacy.ID {
		t.Errorf("credential recovery returned %d, want legacy user %d", recovered.ID, legacy.ID)
	}
}

func TestGuestLogin_RejectsNewCredentialForAlreadyBoundLegacyUser(t *testing.T) {
	SetupTestDB(t)
	userDAO := dao.NewUserDAO()
	authService := auth.NewService(userDAO)
	legacyDevice := "already-bound-ios-idfv"
	legacy := createLegacyGuest(t, userDAO, "2", legacyDevice, "8620000000001")

	first, _, err := authService.GuestLoginWithDevice(context.Background(), auth.GuestLoginParams{
		Platform:        "ios",
		GuestCredential: "first-credential",
		IDFV:            legacyDevice,
	})
	if err != nil {
		t.Fatalf("bind first credential: %v", err)
	}
	if first.ID != legacy.ID {
		t.Fatalf("first credential returned %d, want %d", first.ID, legacy.ID)
	}

	if _, _, err := authService.GuestLoginWithDevice(context.Background(), auth.GuestLoginParams{
		Platform:        "ios",
		GuestCredential: "second-credential",
		IDFV:            legacyDevice,
	}); err == nil {
		t.Fatal("expected a different credential to be rejected")
	}
}

func TestGuestLogin_RequiresGuestCredential(t *testing.T) {
	SetupTestDB(t)
	authService := auth.NewService(dao.NewUserDAO())
	if _, _, err := authService.GuestLoginWithDevice(context.Background(), auth.GuestLoginParams{Platform: "ios", IDFV: "idfv"}); err == nil {
		t.Fatal("expected missing guest credential to fail")
	}
}

func TestGuestLogin_RecoversUserByGuestCredentialAfterDeviceChanges(t *testing.T) {
	SetupTestDB(t)
	authService := auth.NewService(dao.NewUserDAO())

	first, _, err := authService.GuestLoginWithDevice(context.Background(), auth.GuestLoginParams{
		Platform:        "ios",
		GuestCredential: "credential-46d2672e-42c3-4cff-8d4d-8c7ee0bd328c",
		IDFV:            "original-idfv",
	})
	if err != nil {
		t.Fatalf("first GuestLoginWithDevice failed: %v", err)
	}

	recovered, _, err := authService.GuestLoginWithDevice(context.Background(), auth.GuestLoginParams{
		Platform:        "ios",
		GuestCredential: "credential-46d2672e-42c3-4cff-8d4d-8c7ee0bd328c",
		IDFV:            "reinstalled-idfv",
	})
	if err != nil {
		t.Fatalf("recovery GuestLoginWithDevice failed: %v", err)
	}

	if recovered.ID != first.ID || recovered.PublicUserID() != first.PublicUserID() {
		t.Errorf("credential recovery returned user %d/%s, want %d/%s", recovered.ID, recovered.PublicUserID(), first.ID, first.PublicUserID())
	}
}

func TestGuestLogin_PrefersCredentialOverDeviceIdentity(t *testing.T) {
	SetupTestDB(t)
	authService := auth.NewService(dao.NewUserDAO())

	credentialOwner, _, err := authService.GuestLoginWithDevice(context.Background(), auth.GuestLoginParams{
		Platform:        "ios",
		GuestCredential: "credential-owner",
		IDFV:            "credential-owner-device",
	})
	if err != nil {
		t.Fatalf("create credential owner: %v", err)
	}
	if _, _, err := authService.GuestLoginWithDevice(context.Background(), auth.GuestLoginParams{
		Platform:        "ios",
		GuestCredential: "other-credential",
		IDFV:            "other-device",
	}); err != nil {
		t.Fatalf("create other device owner: %v", err)
	}

	resolved, _, err := authService.GuestLoginWithDevice(context.Background(), auth.GuestLoginParams{
		Platform:        "ios",
		GuestCredential: "credential-owner",
		IDFV:            "other-device",
	})
	if err != nil {
		t.Fatalf("credential priority login failed: %v", err)
	}
	if resolved.ID != credentialOwner.ID {
		t.Errorf("credential resolved user %d, want %d", resolved.ID, credentialOwner.ID)
	}
}

func TestGuestLogin_SeparatesPlatformsWithTheSameDeviceIdentifier(t *testing.T) {
	SetupTestDB(t)
	authService := auth.NewService(dao.NewUserDAO())

	android, _, err := authService.GuestLoginWithDevice(context.Background(), auth.GuestLoginParams{
		Platform:        "android",
		GuestCredential: "credential-android-shared-device",
		AndroidID:       "shared-device-id",
	})
	if err != nil {
		t.Fatalf("android GuestLoginWithDevice failed: %v", err)
	}
	ios, _, err := authService.GuestLoginWithDevice(context.Background(), auth.GuestLoginParams{
		Platform:        "ios",
		GuestCredential: "credential-ios-shared-device",
		IDFV:            "shared-device-id",
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
		Platform:        "android",
		GuestCredential: "credential-public-id-lookup-device",
		AndroidID:       "public-id-lookup-device",
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
		Platform:        "android",
		GuestCredential: "credential-device789",
		AndroidID:       "device789",
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
		Platform:        "android",
		GuestCredential: "credential-device000",
		AndroidID:       "device000",
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

func legacyGuestIdentity(t *testing.T, platformSuffix, deviceID string) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(conf.GlobalConfig.JWT.Secret))
	mac.Write([]byte(platformSuffix + ":" + deviceID))
	return hex.EncodeToString(mac.Sum(nil))
}

func createLegacyGuest(t *testing.T, userDAO *dao.UserDAO, platformSuffix, deviceID, userID string) *model.User {
	t.Helper()
	identity := legacyGuestIdentity(t, platformSuffix, deviceID)
	user := &model.User{
		UserID:        &userID,
		UUID:          uuid.NewString(),
		Nickname:      "legacy guest",
		DeviceID:      identity,
		GuestIdentity: &identity,
		LoginType:     "guest",
		Status:        1,
	}
	if err := userDAO.Create(context.Background(), user); err != nil {
		t.Fatalf("create legacy guest: %v", err)
	}
	return user
}
