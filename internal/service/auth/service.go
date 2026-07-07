package auth

import (
	"context"
	"fmt"
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

func (s *Service) GuestLogin(ctx context.Context, deviceID string) (*model.User, *TokenPair, error) {
	if deviceID == "" {
		return nil, nil, apperr.InvalidArgument("device_id is required")
	}

	existing, err := s.userDAO.GetByDeviceIDAndType(ctx, deviceID, "guest")
	if err != nil {
		return nil, nil, apperr.Internal("check existing guest", err)
	}
	if existing != nil {
		tokens, err := s.generateTokens(existing.ID)
		if err != nil {
			return nil, nil, err
		}
		return existing, tokens, nil
	}

	prefix := deviceID
	if len(prefix) > 6 {
		prefix = prefix[:6]
	}
	nickname := fmt.Sprintf("用户%s", prefix)

	user := &model.User{
		UUID:      uuid.New().String(),
		Nickname:  nickname,
		DeviceID:  deviceID,
		LoginType: "guest",
		Status:    1,
	}
	if err := s.userDAO.Create(ctx, user); err != nil {
		return nil, nil, apperr.Internal("create guest user", err)
	}

	tokens, err := s.generateTokens(user.ID)
	if err != nil {
		return nil, nil, err
	}
	return user, tokens, nil
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

func (s *Service) RefreshToken(ctx context.Context, refreshToken string) (*TokenPair, error) {
	claims, err := s.parseToken(refreshToken)
	if err != nil {
		return nil, apperr.Unauthorized("invalid refresh token")
	}

	tokenType, _ := claims["type"].(string)
	if tokenType != "refresh" {
		return nil, apperr.Unauthorized("not a refresh token")
	}

	userID, ok := claims["user_id"].(float64)
	if !ok {
		return nil, apperr.Unauthorized("invalid token claims")
	}
	return s.generateTokens(uint64(userID))
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
