package ai_generation

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/service/credit"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var errDuplicateAITask = errors.New("duplicate ai task")

type Config struct {
	TaskExpireMinutes int
}

type Service struct {
	aiDAO         *dao.AIGenerationDAO
	mediaDAO      *dao.MediaDAO
	creditService *credit.Service
	provider      Provider
	config        Config
}

func NewService(
	aiDAO *dao.AIGenerationDAO,
	mediaDAO *dao.MediaDAO,
	creditService *credit.Service,
	provider Provider,
	cfg Config,
) *Service {
	if cfg.TaskExpireMinutes == 0 {
		cfg.TaskExpireMinutes = 30
	}
	return &Service{
		aiDAO:         aiDAO,
		mediaDAO:      mediaDAO,
		creditService: creditService,
		provider:      provider,
		config:        cfg,
	}
}

func (s *Service) ListStyles(ctx context.Context) ([]*model.AIStyle, error) {
	return s.aiDAO.ListActiveStyles(ctx)
}

type CreateTaskResult struct {
	TaskID           string
	Status           int8
	CreditsDeducted  int
	RemainingBalance int
	Duplicated       bool
}

func (s *Service) CreateStyleGeneration(ctx context.Context, userID uint64, styleID uint64, inputFileKey, clientRequestID string) (*CreateTaskResult, error) {
	if clientRequestID == "" {
		return nil, apperr.InvalidArgument("client_request_id required")
	}
	if inputFileKey == "" {
		return nil, apperr.InvalidArgument("input_file_key required")
	}

	asset, err := s.mediaDAO.GetUploadedAsset(ctx, inputFileKey, userID, "style_input")
	if err != nil {
		return nil, apperr.Internal("validate input file", err)
	}
	if asset == nil {
		return nil, apperr.Forbidden("input file not found or not owned by user")
	}

	style, err := s.aiDAO.GetStyleByID(ctx, styleID)
	if err != nil {
		return nil, apperr.NotFound("style not found")
	}

	var result *CreateTaskResult
	err = db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existing, txErr := s.aiDAO.GetByUserRequestIDForUpdate(tx, userID, clientRequestID)
		if txErr != nil {
			return apperr.Internal("check existing task", txErr)
		}
		if existing != nil {
			account, accErr := s.creditService.GetAccountForUpdate(tx, userID)
			if accErr != nil {
				return apperr.Internal("get credit account", accErr)
			}
			result = &CreateTaskResult{
				TaskID:           existing.TaskID,
				Status:           existing.Status,
				CreditsDeducted:  existing.CreditsDeducted,
				RemainingBalance: account.Balance,
				Duplicated:       true,
			}
			return nil
		}

		taskID := uuid.NewString()
		creditsDeducted := 0
		remainingBalance := 0

		if style.CostCredits > 0 {
			account, accErr := s.creditService.GetAccountForUpdate(tx, userID)
			if accErr != nil {
				return apperr.Internal("get credit account", accErr)
			}
			if account.Balance < style.CostCredits {
				return apperr.InsufficientCredits(account.Balance, style.CostCredits)
			}
			newBalance, deductErr := s.creditService.DeductCreditsTx(tx, userID, style.CostCredits,
				"ai_generation", "ai_generation", taskID, "AI风格转换")
			if deductErr != nil {
				return apperr.Internal("deduct credits", deductErr)
			}
			creditsDeducted = style.CostCredits
			remainingBalance = newBalance
		} else {
			balance, _ := s.creditService.GetBalance(ctx, userID)
			remainingBalance = balance
		}

		expiredAt := time.Now().Add(time.Duration(s.config.TaskExpireMinutes) * time.Minute)
		now := time.Now()
		task := &model.AIGeneration{
			TaskID:          taskID,
			UserID:          userID,
			ClientRequestID: clientRequestID,
			StyleID:         styleID,
			InputFileKey:    inputFileKey,
			InputImageURL:   asset.FileURL,
			Provider:        style.Provider,
			CreditsDeducted: creditsDeducted,
			Status:          model.AIGenStatusPending,
			ExpiredAt:       &expiredAt,
			StartedAt:       &now,
		}

		if createErr := s.aiDAO.CreateTaskTx(tx, task); createErr != nil {
			if isDuplicateKey(createErr) {
				return errDuplicateAITask
			}
			return apperr.Internal("create ai task", createErr)
		}

		result = &CreateTaskResult{
			TaskID:           taskID,
			Status:           model.AIGenStatusPending,
			CreditsDeducted:  creditsDeducted,
			RemainingBalance: remainingBalance,
			Duplicated:       false,
		}
		return nil
	})

	if errors.Is(err, errDuplicateAITask) {
		return s.loadDuplicateResult(ctx, userID, clientRequestID)
	}
	if err != nil {
		return nil, err
	}

	if !result.Duplicated {
		s.submitToProvider(ctx, result.TaskID, style, inputFileKey)
	}

	return result, nil
}

func (s *Service) loadDuplicateResult(ctx context.Context, userID uint64, clientRequestID string) (*CreateTaskResult, error) {
	var result *CreateTaskResult
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existing, txErr := s.aiDAO.GetByUserRequestIDForUpdate(tx, userID, clientRequestID)
		if txErr != nil {
			return apperr.Internal("load duplicate task", txErr)
		}
		if existing == nil {
			return apperr.Internal("load duplicate task", errDuplicateAITask)
		}
		account, accErr := s.creditService.GetAccountForUpdate(tx, userID)
		if accErr != nil {
			return apperr.Internal("get credit account", accErr)
		}
		result = &CreateTaskResult{
			TaskID:           existing.TaskID,
			Status:           existing.Status,
			CreditsDeducted:  existing.CreditsDeducted,
			RemainingBalance: account.Balance,
			Duplicated:       true,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) submitToProvider(ctx context.Context, taskID string, style *model.AIStyle, inputFileKey string) {
	inputURL := s.buildInputURL(inputFileKey)

	providerResult, err := s.provider.Submit(style.StyleKey, style.PromptTemplate, inputURL)
	if err != nil {
		zap.L().Error("ai provider submit failed", zap.String("task_id", taskID), zap.Error(err))
		s.handleProviderFailure(ctx, taskID)
		return
	}

	now := time.Now()
	updates := map[string]interface{}{
		"provider_job_id": providerResult.JobID,
		"started_at":      &now,
	}

	switch providerResult.Status {
	case "succeeded":
		updates["status"] = model.AIGenStatusSucceeded
		updates["output_image_url"] = providerResult.OutputURL
		updates["completed_at"] = &now
	case "failed":
		updates["status"] = model.AIGenStatusFailed
		updates["error_code"] = providerResult.ErrorCode
		updates["error_message"] = providerResult.ErrorMsg
		s.refundCredits(ctx, taskID)
	default:
		updates["status"] = model.AIGenStatusRunning
	}

	s.aiDAO.UpdateTask(ctx, taskID, updates)
}

func (s *Service) handleProviderFailure(ctx context.Context, taskID string) {
	now := time.Now()
	s.aiDAO.UpdateTask(ctx, taskID, map[string]interface{}{
		"status":        model.AIGenStatusFailed,
		"error_message": "provider submission failed",
		"completed_at":  &now,
	})
	s.refundCredits(ctx, taskID)
}

func (s *Service) refundCredits(ctx context.Context, taskID string) {
	task, err := s.aiDAO.GetByTaskID(ctx, taskID)
	if err != nil || task == nil || task.CreditsDeducted <= 0 {
		return
	}

	db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		locked, lockErr := s.aiDAO.GetByTaskIDForUpdate(tx, taskID)
		if lockErr != nil || locked == nil {
			return lockErr
		}
		if locked.Status != model.AIGenStatusFailed && locked.Status != model.AIGenStatusExpired {
			return nil
		}
		if locked.CreditsDeducted <= 0 {
			return nil
		}
		_, refErr := s.creditService.AddCreditsTx(tx, locked.UserID, locked.CreditsDeducted,
			"refund", "ai_generation", taskID, "AI任务失败退还")
		return refErr
	})
}

func (s *Service) GetStyleGeneration(ctx context.Context, userID uint64, taskID string) (*model.AIGeneration, error) {
	task, err := s.aiDAO.GetByTaskID(ctx, taskID)
	if err != nil {
		return nil, apperr.NotFound("task not found")
	}
	if task.UserID != userID {
		return nil, apperr.Forbidden("unauthorized")
	}
	return task, nil
}

func (s *Service) ListStyleGenerations(ctx context.Context, userID uint64, page, pageSize int) ([]*model.AIGeneration, int64, error) {
	offset := (page - 1) * pageSize
	return s.aiDAO.ListByUserID(ctx, userID, offset, pageSize)
}

func (s *Service) ValidateUserSucceededTask(ctx context.Context, userID uint64, taskID string) error {
	task, err := s.aiDAO.GetUserSucceededTask(ctx, userID, taskID)
	if err != nil {
		return apperr.Internal("validate ai task", err)
	}
	if task == nil {
		return apperr.Forbidden("ai task not found or not succeeded")
	}
	return nil
}

func (s *Service) ProcessPendingTasks(ctx context.Context) error {
	tasks, err := s.aiDAO.FindExpired(ctx, time.Now(), 100)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		s.expireTask(ctx, task)
	}

	runningTasks, err := s.aiDAO.FindPendingOrRunning(ctx, time.Now().Add(-5*time.Second), 50)
	if err != nil {
		return err
	}
	for _, task := range runningTasks {
		if task.Status == model.AIGenStatusRunning && task.ProviderJobID != "" {
			s.pollProvider(ctx, task)
		}
	}
	return nil
}

func (s *Service) expireTask(ctx context.Context, task *model.AIGeneration) {
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		locked, lockErr := s.aiDAO.GetByTaskIDForUpdate(tx, task.TaskID)
		if lockErr != nil {
			return lockErr
		}
		if locked.Status != model.AIGenStatusPending && locked.Status != model.AIGenStatusRunning {
			return nil
		}
		now := time.Now()
		if err := s.aiDAO.UpdateTaskTx(tx, task.TaskID, map[string]interface{}{
			"status":       model.AIGenStatusExpired,
			"completed_at": &now,
		}); err != nil {
			return err
		}
		if locked.CreditsDeducted > 0 {
			_, err := s.creditService.AddCreditsTx(tx, locked.UserID, locked.CreditsDeducted,
				"refund", "ai_generation_expired", locked.TaskID, "AI任务超时退还")
			return err
		}
		return nil
	})
	if err != nil {
		zap.L().Error("expire ai task failed", zap.String("task_id", task.TaskID), zap.Error(err))
	}
}

func (s *Service) pollProvider(ctx context.Context, task *model.AIGeneration) {
	providerResult, err := s.provider.Query(task.ProviderJobID)
	if err != nil {
		zap.L().Warn("poll ai provider failed", zap.String("task_id", task.TaskID), zap.Error(err))
		return
	}

	now := time.Now()
	switch providerResult.Status {
	case "succeeded":
		s.aiDAO.UpdateTask(ctx, task.TaskID, map[string]interface{}{
			"status":           model.AIGenStatusSucceeded,
			"output_image_url": providerResult.OutputURL,
			"completed_at":     &now,
		})
	case "failed":
		s.aiDAO.UpdateTask(ctx, task.TaskID, map[string]interface{}{
			"status":        model.AIGenStatusFailed,
			"error_code":    providerResult.ErrorCode,
			"error_message": providerResult.ErrorMsg,
			"completed_at":  &now,
		})
		s.refundCredits(ctx, task.TaskID)
	}
}

func (s *Service) buildInputURL(fileKey string) string {
	return "https://cdn.example.com/" + fileKey
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
