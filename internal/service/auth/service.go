package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/zhaojiabo/bobobeads_server/conf"
	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
)

type Service struct {
	userDAO *dao.UserDAO
}

func NewService(userDAO *dao.UserDAO) *Service {
	return &Service{userDAO: userDAO}
}

type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
}

type GuestLoginParams struct {
	Platform  string
	AndroidID string
	OAID      string
	IDFV      string
	IDFA      string
}

func (s *Service) GuestLoginWithDevice(ctx context.Context, params GuestLoginParams) (*model.User, *TokenPair, error) {
	deviceID, platformSuffix := selectGuestIdentity(params)
	guestIdentity := hashGuestIdentity(platformSuffix, deviceID)
	if existing, err := s.userDAO.GetByGuestIdentity(ctx, guestIdentity); err != nil {
		return nil, nil, apperr.Internal("get guest user", err)
	} else if existing != nil {
		tokens, err := s.generateTokens(existing.ID)
		if err != nil {
			return nil, nil, err
		}
		return existing, tokens, nil
	}

	userID := generateGuestUserID(deviceID, platformSuffix)

	nickname := "用户" + userID[len(userID)-6:]

	user := &model.User{
		UserID:        &userID,
		UUID:          uuid.New().String(),
		Nickname:      nickname,
		DeviceID:      guestIdentity,
		GuestIdentity: &guestIdentity,
		LoginType:     "guest",
		Status:        1,
	}
	user, err := s.userDAO.CreateOrGetGuest(ctx, user)
	if err != nil {
		if err == dao.ErrGuestUserIDCollision {
			return nil, nil, apperr.New(apperr.CodeDuplicateRequest, "guest user id collision")
		}
		return nil, nil, apperr.Internal("create guest user", err)
	}

	tokens, err := s.generateTokens(user.ID)
	if err != nil {
		return nil, nil, err
	}
	return user, tokens, nil
}

func hashGuestIdentity(platformSuffix, identity string) string {
	mac := hmac.New(sha256.New, []byte(conf.GlobalConfig.JWT.Secret))
	mac.Write([]byte(platformSuffix + ":" + identity))
	return hex.EncodeToString(mac.Sum(nil))
}

func selectGuestIdentity(params GuestLoginParams) (string, string) {
	switch strings.ToLower(params.Platform) {
	case "android":
		if params.AndroidID != "" {
			return params.AndroidID, "3"
		}
		if params.OAID != "" {
			return params.OAID, "3"
		}
		return strings.ReplaceAll(uuid.New().String(), "-", ""), "3"
	case "ios":
		if params.IDFV != "" {
			return params.IDFV, "2"
		}
		if params.IDFA != "" {
			return params.IDFA, "2"
		}
		return strings.ReplaceAll(uuid.New().String(), "-", ""), "2"
	default:
		return strings.ReplaceAll(uuid.New().String(), "-", ""), ""
	}
}

func generateGuestUserID(identity, platformSuffix string) string {
	digest := sha256.Sum256([]byte(identity))
	decimal := new(big.Int).SetBytes(digest[:]).String()
	return "86" + platformSuffix + decimal[:10]
}

func (s *Service) PhoneLogin(ctx context.Context, phone, code string) (*model.User, *TokenPair, error) {
	// TODO: verify SMS code from Redis
	user, err := s.userDAO.GetByPhone(ctx, phone)
	if err != nil {
		user = &model.User{
			UUID:      uuid.New().String(),
			Phone:     phone,
			LoginType: "phone",
			Status:    1,
		}
		if err := s.userDAO.Create(ctx, user); err != nil {
			return nil, nil, apperr.Internal("create phone user", err)
		}
	}
	tokens, err := s.generateTokens(user.ID)
	if err != nil {
		return nil, nil, err
	}
	return user, tokens, nil
}

func (s *Service) WechatLogin(ctx context.Context, code string, platform string) (*model.User, *TokenPair, error) {
	// TODO: exchange code for access_token & unionid via WeChat API
	return nil, nil, apperr.New(apperr.CodeInternal, "not implemented")
}

func (s *Service) AppleLogin(ctx context.Context, identityToken, authCode, fullName string) (*model.User, *TokenPair, error) {
	// TODO: verify Apple identity token
	return nil, nil, apperr.New(apperr.CodeInternal, "not implemented")
}

func (s *Service) RefreshToken(ctx context.Context, refreshToken string) (*model.User, *TokenPair, error) {
	claims, err := s.parseToken(refreshToken)
	if err != nil {
		return nil, nil, apperr.Unauthorized("invalid refresh token")
	}

	tokenType, _ := claims["type"].(string)
	if tokenType != "refresh" {
		return nil, nil, apperr.Unauthorized("not a refresh token")
	}

	userID, ok := claims["user_id"].(float64)
	if !ok {
		return nil, nil, apperr.Unauthorized("invalid token claims")
	}
	user, err := s.userDAO.GetByID(ctx, uint64(userID))
	if err != nil {
		return nil, nil, apperr.Unauthorized("user not found")
	}
	tokens, err := s.generateTokens(user.ID)
	if err != nil {
		return nil, nil, err
	}
	return user, tokens, nil
}

func (s *Service) generateTokens(userID uint64) (*TokenPair, error) {
	cfg := conf.GlobalConfig.JWT
	now := time.Now()

	accessExp := now.Add(time.Duration(cfg.AccessExpireH) * time.Hour)
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"exp":     accessExp.Unix(),
		"iat":     now.Unix(),
		"type":    "access",
	})
	accessStr, err := accessToken.SignedString([]byte(cfg.Secret))
	if err != nil {
		return nil, apperr.Internal("generate access token", err)
	}

	refreshExp := now.Add(time.Duration(cfg.RefreshExpireH) * time.Hour)
	refreshTokenObj := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"exp":     refreshExp.Unix(),
		"iat":     now.Unix(),
		"type":    "refresh",
	})
	refreshStr, err := refreshTokenObj.SignedString([]byte(cfg.Secret))
	if err != nil {
		return nil, apperr.Internal("generate refresh token", err)
	}

	return &TokenPair{
		AccessToken:  accessStr,
		RefreshToken: refreshStr,
		ExpiresIn:    int64(cfg.AccessExpireH * 3600),
	}, nil
}

func (s *Service) parseToken(tokenStr string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		return []byte(conf.GlobalConfig.JWT.Secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}

func (s *Service) ValidateAccessToken(tokenStr string) (uint64, error) {
	claims, err := s.parseToken(tokenStr)
	if err != nil {
		return 0, err
	}
	tokenType, _ := claims["type"].(string)
	if tokenType != "access" {
		return 0, fmt.Errorf("not an access token")
	}
	userID, ok := claims["user_id"].(float64)
	if !ok {
		return 0, fmt.Errorf("invalid user_id in token")
	}
	return uint64(userID), nil
}
