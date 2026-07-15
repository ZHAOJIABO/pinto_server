package generation

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zhaojiabo/bobobeads_server/conf"
	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/service/credit"
	"github.com/zhaojiabo/bobobeads_server/internal/service/subscribe"
	"github.com/zhaojiabo/bobobeads_server/internal/service/work"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var errDuplicateGeneration = errors.New("duplicate generation request")

type AITaskValidator interface {
	ValidateUserSucceededTask(ctx context.Context, userID uint64, taskID string) error
}

type Service struct {
	generationDAO    *dao.GenerationDAO
	creditService    *credit.Service
	subscribeService *subscribe.Service
	workService      *work.Service
	aiValidator      AITaskValidator
}

func NewService(
	generationDAO *dao.GenerationDAO,
	creditService *credit.Service,
	subscribeService *subscribe.Service,
	workService *work.Service,
) *Service {
	return &Service{
		generationDAO:    generationDAO,
		creditService:    creditService,
		subscribeService: subscribeService,
		workService:      workService,
	}
}

func (s *Service) SetAIValidator(v AITaskValidator) {
	s.aiValidator = v
}

type CreateResult struct {
	GenerationID     string
	CreditsDeducted  int
	RemainingBalance int
	ExpiresAt        int64
	Duplicated       bool
}

func (s *Service) CreateGeneration(ctx context.Context, userID uint64, boardSpec, sourceType, sourceID, clientRequestID string) (*CreateResult, error) {
	if clientRequestID == "" {
		return nil, apperr.InvalidArgument("client_request_id required")
	}
	if strings.TrimSpace(boardSpec) == "" {
		return nil, apperr.InvalidArgument("board_spec required")
	}

	if sourceType == "ai_style" {
		if sourceID == "" {
			return nil, apperr.InvalidArgument("source_id required for ai_style")
		}
		if s.aiValidator != nil {
			if err := s.aiValidator.ValidateUserSucceededTask(ctx, userID, sourceID); err != nil {
				return nil, err
			}
		}
	}

	cfg := conf.GlobalConfig.Generation
	dailyFreeLimit := cfg.DailyFreeLimit
	creditCostPerGen := cfg.CreditCost
	expireMinutes := cfg.ExpireMinutes
	if dailyFreeLimit == 0 {
		dailyFreeLimit = 3
	}
	if creditCostPerGen == 0 {
		creditCostPerGen = 1
	}
	if expireMinutes == 0 {
		expireMinutes = 30
	}

	var result *CreateResult
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existing, err := s.generationDAO.GetByUserRequestIDForUpdate(tx, userID, clientRequestID)
		if err != nil {
			return apperr.Internal("check existing generation", err)
		}
		if existing != nil {
			account, accErr := s.creditService.GetAccountForUpdate(tx, userID)
			if accErr != nil {
				return apperr.Internal("get credit account", accErr)
			}
			result = &CreateResult{
				GenerationID:     existing.GenerationID,
				CreditsDeducted:  existing.CreditsDeducted,
				RemainingBalance: account.Balance,
				ExpiresAt:        existing.ExpiredAt.Unix(),
				Duplicated:       true,
			}
			return nil
		}

		generationID := uuid.NewString()
		creditsDeducted := 0
		remainingBalance := 0

		isVIP, _ := s.subscribeService.IsVIP(ctx, userID)
		if !isVIP {
			todayCount, err := s.generationDAO.CountTodayByUserTx(tx, userID)
			if err != nil {
				return apperr.Internal("check daily count", err)
			}

			if todayCount >= int64(dailyFreeLimit) {
				account, err := s.creditService.GetAccountForUpdate(tx, userID)
				if err != nil {
					return apperr.Internal("get credit account", err)
				}
				if account.Balance < creditCostPerGen {
					return apperr.InsufficientCredits(account.Balance, creditCostPerGen)
				}
				newBalance, err := s.creditService.DeductCreditsTx(tx, userID, creditCostPerGen,
					"generation", "generation", generationID, "生成拼豆图纸")
				if err != nil {
					return apperr.Internal("deduct credits", err)
				}
				creditsDeducted = creditCostPerGen
				remainingBalance = newBalance
			} else {
				remainingBalance, _ = s.creditService.GetBalance(ctx, userID)
			}
		} else {
			remainingBalance, _ = s.creditService.GetBalance(ctx, userID)
		}

		expiredAt := time.Now().Add(time.Duration(expireMinutes) * time.Minute)
		gen := &model.Generation{
			UserID:          userID,
			GenerationID:    generationID,
			ClientRequestID: clientRequestID,
			BoardSpec:       boardSpec,
			SourceType:      sourceType,
			SourceID:        sourceID,
			CreditsDeducted: creditsDeducted,
			Status:          model.GenerationStatusPending,
			ExpiredAt:       expiredAt,
		}
		if err := s.generationDAO.CreateTx(tx, gen); err != nil {
			if isDuplicateKey(err) {
				return errDuplicateGeneration
			}
			return apperr.Internal("create generation", err)
		}

		result = &CreateResult{
			GenerationID:     generationID,
			CreditsDeducted:  creditsDeducted,
			RemainingBalance: remainingBalance,
			ExpiresAt:        expiredAt.Unix(),
			Duplicated:       false,
		}
		return nil
	})

	if errors.Is(err, errDuplicateGeneration) {
		duplicateResult, loadErr := s.loadDuplicateCreateResult(ctx, userID, clientRequestID)
		if loadErr != nil {
			return nil, loadErr
		}
		return duplicateResult, nil
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) loadDuplicateCreateResult(ctx context.Context, userID uint64, clientRequestID string) (*CreateResult, error) {
	var result *CreateResult
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existing, err := s.generationDAO.GetByUserRequestIDForUpdate(tx, userID, clientRequestID)
		if err != nil {
			return apperr.Internal("load duplicated generation", err)
		}
		if existing == nil {
			return apperr.Internal("load duplicated generation", errDuplicateGeneration)
		}
		account, err := s.creditService.GetAccountForUpdate(tx, userID)
		if err != nil {
			return apperr.Internal("get credit account", err)
		}
		result = &CreateResult{
			GenerationID:     existing.GenerationID,
			CreditsDeducted:  existing.CreditsDeducted,
			RemainingBalance: account.Balance,
			ExpiresAt:        existing.ExpiredAt.Unix(),
			Duplicated:       true,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type CompleteResult struct {
	WorkID     uint64
	Duplicated bool
}

func (s *Service) CompleteGeneration(ctx context.Context, userID uint64, generationID string, workData *model.Work) (*CompleteResult, error) {
	var result *CompleteResult
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		gen, err := s.generationDAO.GetByGenerationIDForUpdate(tx, generationID)
		if err != nil {
			return apperr.NotFound("generation not found")
		}

		if gen.UserID != userID {
			return apperr.Forbidden("unauthorized")
		}

		if gen.Status == model.GenerationStatusCompleted && gen.WorkID > 0 {
			result = &CompleteResult{WorkID: gen.WorkID, Duplicated: true}
			return nil
		}

		if gen.Status != model.GenerationStatusPending {
			return apperr.New(apperr.CodeGenerationCompleted, "generation already "+statusText(gen.Status))
		}

		if time.Now().After(gen.ExpiredAt) {
			return apperr.GenerationExpired()
		}
		if err := work.NormalizeWorkPatternData(workData); err != nil {
			return err
		}
		if gen.BoardSpec != workData.BoardSpec {
			return apperr.InvalidArgument("pattern_data.board_spec must match generation board_spec")
		}

		workData.UserID = userID
		workData.SourceType = gen.SourceType
		workData.SourceID = gen.SourceID
		workData.Status = 2
		if err := s.workService.CreateWorkTx(tx, workData); err != nil {
			return apperr.Internal("save work", err)
		}

		now := time.Now()
		updates := map[string]interface{}{
			"status":       model.GenerationStatusCompleted,
			"work_id":      workData.ID,
			"completed_at": &now,
		}
		if err := s.generationDAO.UpdateStatusTx(tx, generationID, updates); err != nil {
			return apperr.Internal("update generation status", err)
		}

		result = &CompleteResult{WorkID: workData.ID, Duplicated: false}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) CancelGeneration(ctx context.Context, userID uint64, generationID, reason string) (int, error) {
	var refunded int
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		gen, err := s.generationDAO.GetByGenerationIDForUpdate(tx, generationID)
		if err != nil {
			return apperr.NotFound("generation not found")
		}

		if gen.UserID != userID {
			return apperr.Forbidden("unauthorized")
		}
		if gen.Status != model.GenerationStatusPending {
			return apperr.New(apperr.CodeGenerationCompleted, "generation already "+statusText(gen.Status))
		}

		if gen.CreditsDeducted > 0 {
			_, err = s.creditService.AddCreditsTx(tx, userID, gen.CreditsDeducted,
				"refund", "generation", generationID, "生成取消退还")
			if err != nil {
				return apperr.Internal("refund credits", err)
			}
			refunded = gen.CreditsDeducted
		}

		updates := map[string]interface{}{
			"status":        model.GenerationStatusCancelled,
			"cancel_reason": reason,
		}
		if err := s.generationDAO.UpdateStatusTx(tx, generationID, updates); err != nil {
			return apperr.Internal("update generation status", err)
		}

		return nil
	})

	if err != nil {
		return 0, err
	}

	zap.L().Info("generation cancelled",
		zap.Uint64("user_id", userID),
		zap.String("generation_id", generationID),
		zap.String("reason", reason),
		zap.Int("refunded", refunded),
	)

	return refunded, nil
}

func (s *Service) GetStatus(ctx context.Context, userID uint64, generationID string) (*model.Generation, error) {
	gen, err := s.generationDAO.GetByGenerationID(ctx, generationID)
	if err != nil {
		return nil, apperr.NotFound("generation not found")
	}
	if gen.UserID != userID {
		return nil, apperr.Forbidden("unauthorized")
	}
	return gen, nil
}

func (s *Service) ExpireTimeoutGenerations(ctx context.Context) error {
	gens, err := s.generationDAO.FindExpiredPending(ctx, time.Now(), 100)
	if err != nil {
		return err
	}

	for _, gen := range gens {
		s.expireSingle(ctx, gen)
	}

	return nil
}

func (s *Service) expireSingle(ctx context.Context, gen *model.Generation) {
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		locked, err := s.generationDAO.GetByGenerationIDForUpdate(tx, gen.GenerationID)
		if err != nil {
			return err
		}
		if locked.Status != model.GenerationStatusPending {
			return nil
		}

		if locked.CreditsDeducted > 0 {
			_, err = s.creditService.AddCreditsTx(tx, locked.UserID, locked.CreditsDeducted,
				"refund", "generation_expired", locked.GenerationID, "生成超时自动退还")
			if err != nil {
				return err
			}
		}

		return s.generationDAO.UpdateStatusTx(tx, locked.GenerationID, map[string]interface{}{
			"status": model.GenerationStatusExpired,
		})
	})

	if err != nil {
		zap.L().Error("expire generation failed",
			zap.String("generation_id", gen.GenerationID),
			zap.Error(err),
		)
		return
	}

	zap.L().Info("generation expired",
		zap.Uint64("user_id", gen.UserID),
		zap.String("generation_id", gen.GenerationID),
		zap.Int("refunded", gen.CreditsDeducted),
	)
}

func statusText(status int8) string {
	switch status {
	case model.GenerationStatusCompleted:
		return "completed"
	case model.GenerationStatusCancelled:
		return "cancelled"
	case model.GenerationStatusExpired:
		return "expired"
	default:
		return "unknown"
	}
}

func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicated key")
}
