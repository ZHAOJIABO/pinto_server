package admin

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/zhaojiabo/bobobeads_server/conf"
)

const (
	passwordAlgorithm  = "pbkdf2-sha256"
	passwordIterations = 600000
	passwordKeyLength  = 32
	maxLoginFailures   = 5
	loginLockDuration  = 15 * time.Minute
)

var (
	ErrInvalidCredentials = errors.New("invalid admin credentials")
	ErrLoginLocked        = errors.New("too many failed login attempts")
	ErrInvalidToken       = errors.New("invalid admin token")
)

type Token struct {
	AccessToken string
	ExpiresIn   int64
}

type loginAttempt struct {
	Failures int
	LockedAt time.Time
}

// AuthService owns the small, independently configured identity boundary for
// the internal browser portal. End-user JWTs and service-to-service tokens are
// intentionally not accepted here.
type AuthService struct {
	accounts map[string]string
	secret   []byte
	ttl      time.Duration
	now      func() time.Time

	mu       sync.Mutex
	attempts map[string]loginAttempt
}

func NewAuthService(cfg conf.AdminConfig) *AuthService {
	accounts := make(map[string]string, len(cfg.Accounts))
	for _, account := range cfg.Accounts {
		username := strings.TrimSpace(account.Username)
		if username == "" || account.PasswordHash == "" {
			continue
		}
		accounts[username] = account.PasswordHash
	}

	ttl := time.Duration(cfg.AccessExpireM) * time.Minute
	if ttl <= 0 {
		ttl = 8 * time.Hour
	}

	return &AuthService{
		accounts: accounts,
		secret:   []byte(cfg.JWTSecret),
		ttl:      ttl,
		now:      time.Now,
		attempts: make(map[string]loginAttempt),
	}
}

func (s *AuthService) Login(username, password string) (*Token, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" || len(s.secret) == 0 {
		return nil, ErrInvalidCredentials
	}

	if s.isLocked(username) {
		return nil, ErrLoginLocked
	}

	hash, ok := s.accounts[username]
	if !ok || !VerifyPassword(password, hash) {
		s.recordFailure(username)
		return nil, ErrInvalidCredentials
	}

	s.clearFailures(username)
	now := s.now()
	expiresAt := now.Add(s.ttl)
	claims := jwt.MapClaims{
		"sub":   username,
		"scope": "admin:templates",
		"type":  "admin_access",
		"iat":   now.Unix(),
		"exp":   expiresAt.Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	encoded, err := token.SignedString(s.secret)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	return &Token{AccessToken: encoded, ExpiresIn: int64(s.ttl.Seconds())}, nil
}

func (s *AuthService) ValidateAccessToken(raw string) (string, error) {
	if raw == "" || len(s.secret) == 0 {
		return "", ErrInvalidToken
	}

	token, err := jwt.Parse(raw, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return s.secret, nil
	})
	if err != nil || !token.Valid {
		return "", ErrInvalidToken
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || claims["type"] != "admin_access" || claims["scope"] != "admin:templates" {
		return "", ErrInvalidToken
	}
	username, ok := claims["sub"].(string)
	if !ok || username == "" {
		return "", ErrInvalidToken
	}
	if _, ok := s.accounts[username]; !ok {
		return "", ErrInvalidToken
	}
	return username, nil
}

// HashPassword creates the deployment-time password hash format accepted by
// AdminConfig.accounts. It is exported for a future bootstrap command and tests;
// the application never accepts or persists a plaintext administrator password.
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", errors.New("password is required")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	derived, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, passwordKeyLength)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"%s$%d$%s$%s",
		passwordAlgorithm,
		passwordIterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(derived),
	), nil
}

func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != passwordAlgorithm {
		return false
	}

	iterations := 0
	if _, err := fmt.Sscanf(parts[1], "%d", &iterations); err != nil || iterations < passwordIterations {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(salt) < 16 {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(expected) != passwordKeyLength {
		return false
	}
	actual, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(expected))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func (s *AuthService) isLocked(username string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	attempt := s.attempts[username]
	if attempt.Failures < maxLoginFailures {
		return false
	}
	if s.now().Sub(attempt.LockedAt) >= loginLockDuration {
		delete(s.attempts, username)
		return false
	}
	return true
}

func (s *AuthService) recordFailure(username string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	attempt := s.attempts[username]
	attempt.Failures++
	if attempt.Failures >= maxLoginFailures {
		attempt.LockedAt = s.now()
	}
	s.attempts[username] = attempt
}

func (s *AuthService) clearFailures(username string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.attempts, username)
}
