package admin

import (
	"testing"
	"time"

	"github.com/zhaojiabo/bobobeads_server/conf"
)

const testPassword = "correct horse battery staple"

func TestValidateAndRenewSlidesSessionWithoutOutlivingIdleWindow(t *testing.T) {
	hash, err := HashPassword(testPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	service := NewAuthService(conf.AdminConfig{
		JWTSecret:     "admin-test-secret",
		AccessExpireM: 1440,
		Accounts:      []conf.AdminAccountConfig{{Username: "operator", PasswordHash: hash}},
	})

	base := time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC)
	clock := base
	service.now = func() time.Time { return clock }

	token, err := service.Login("operator", testPassword)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if token.ExpiresIn != int64(24*time.Hour/time.Second) {
		t.Fatalf("expected a 24h window, got %ds", token.ExpiresIn)
	}

	clock = base.Add(time.Hour)
	actor, renewed, err := service.ValidateAndRenew(token.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAndRenew on fresh token: %v", err)
	}
	if actor != "operator" {
		t.Fatalf("expected actor operator, got %q", actor)
	}
	if renewed != nil {
		t.Fatal("a token still in its first half must not be replaced")
	}

	clock = base.Add(13 * time.Hour)
	_, renewed, err = service.ValidateAndRenew(token.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAndRenew past midpoint: %v", err)
	}
	if renewed == nil {
		t.Fatal("expected a replacement token past the midpoint")
	}

	// The original expires on its own schedule; the replacement carries the
	// session beyond it. This is what keeps an active administrator signed in.
	clock = base.Add(30 * time.Hour)
	if _, _, err := service.ValidateAndRenew(token.AccessToken); err == nil {
		t.Fatal("the original token must expire 24h after login")
	}
	if _, _, err := service.ValidateAndRenew(renewed.AccessToken); err != nil {
		t.Fatalf("the replacement should still authenticate: %v", err)
	}

	// Going idle past the window ends the session: an expired token is rejected
	// outright rather than renewed, so renewal can never resurrect one.
	clock = base.Add(13*time.Hour + 25*time.Hour)
	if _, _, err := service.ValidateAndRenew(renewed.AccessToken); err == nil {
		t.Fatal("an idle session must expire instead of renewing forever")
	}
}

func TestValidateAndRenewRejectsRemovedAccount(t *testing.T) {
	hash, err := HashPassword(testPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	service := NewAuthService(conf.AdminConfig{
		JWTSecret:     "admin-test-secret",
		AccessExpireM: 1440,
		Accounts:      []conf.AdminAccountConfig{{Username: "operator", PasswordHash: hash}},
	})
	token, err := service.Login("operator", testPassword)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	delete(service.accounts, "operator")
	if _, _, err := service.ValidateAndRenew(token.AccessToken); err == nil {
		t.Fatal("revoking an account must invalidate its outstanding token")
	}
}
