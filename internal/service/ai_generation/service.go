package ai_generation

import (
	"context"
	"encoding/json"
	"errors"
	"path"
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

const (
	styleInputPurpose = "style_input"
	retryBackoff      = 2 * time.Second
	persistTimeout    = 30 * time.Second
	settleTimeout     = 15 * time.Second

	// ActiveModelConfigKey is the bb_config row that forces the first attempt of
	// every task onto one configured model, so switching the model online is a
	// single UPDATE and does not need a deploy or a per-style edit. It does not
	// govern user-initiated retries: those go through RetryModelConfigKey.
	ActiveModelConfigKey = "ai_active_model"

	// RetryModelConfigKey is the bb_config row that overrides which model serves
	// a user-initiated retry. It wins over ActiveModelConfigKey, otherwise a
	// global override would send the retry straight back to the model that just
	// failed.
	RetryModelConfigKey = "ai_retry_model"
)

// submitOrigin distinguishes a first submission from a user-initiated retry,
// which is the only thing that decides whether the retry model applies.
type submitOrigin int

const (
	originFirstAttempt submitOrigin = iota
	originRetry
)

// ImageStore is the slice of the media service this package needs: read the
// private input object, write the public output object, and write a thumbnail
// beside either of them.
type ImageStore interface {
	GetObjectBytes(ctx context.Context, userID uint64, fileKey, purpose string) ([]byte, string, error)
	UploadAIOutput(ctx context.Context, userID uint64, contentType string, content []byte) (string, string, error)
	PutThumbnail(ctx context.Context, sourceFileKey string, content []byte) (string, error)
}

type VIPChecker interface {
	IsVIP(ctx context.Context, userID uint64) (bool, error)
}

// ConfigStore reads runtime configuration from bb_config. Only the active model
// key is needed here, so the whole system DAO is not pulled in.
type ConfigStore interface {
	GetConfig(ctx context.Context, key string) (string, error)
}

type Config struct {
	TaskExpireMinutes   int
	StuckRunningMinutes int
	FreeConcurrent      int
	FreeQueueSize       int
	VIPConcurrent       int
	VIPQueueSize        int
	// DefaultModel is the last link in the model resolution chain, used when
	// neither bb_config nor the style row names one.
	DefaultModel string
	// RetryModel serves user-initiated retries. Empty means a retry reuses the
	// first-attempt chain.
	RetryModel string
}

// quotaTier splits two different knobs: QueueSize bounds how much a user may
// pile up (checked at submit), Concurrent bounds how many global worker slots
// one user may hold at once (checked at claim).
type quotaTier struct {
	Concurrent int
	QueueSize  int
}

type Service struct {
	aiDAO         *dao.AIGenerationDAO
	mediaDAO      *dao.MediaDAO
	creditService *credit.Service
	store         ImageStore
	vip           VIPChecker
	configStore   ConfigStore
	providers     *Registry
	config        Config
	notify        func()
}

func NewService(
	aiDAO *dao.AIGenerationDAO,
	mediaDAO *dao.MediaDAO,
	creditService *credit.Service,
	store ImageStore,
	vip VIPChecker,
	configStore ConfigStore,
	providers *Registry,
	cfg Config,
) *Service {
	if cfg.TaskExpireMinutes <= 0 {
		cfg.TaskExpireMinutes = 30
	}
	if cfg.StuckRunningMinutes <= 0 {
		cfg.StuckRunningMinutes = 15
	}
	if cfg.FreeConcurrent <= 0 {
		cfg.FreeConcurrent = 1
	}
	if cfg.FreeQueueSize <= 0 {
		cfg.FreeQueueSize = 3
	}
	if cfg.VIPConcurrent <= 0 {
		cfg.VIPConcurrent = 2
	}
	if cfg.VIPQueueSize <= 0 {
		cfg.VIPQueueSize = 10
	}
	return &Service{
		aiDAO:         aiDAO,
		mediaDAO:      mediaDAO,
		creditService: creditService,
		store:         store,
		vip:           vip,
		configStore:   configStore,
		providers:     providers,
		config:        cfg,
	}
}

// SetDispatchNotifier registers a callback that wakes the dispatcher once a new
// task is committed. It stays a bare func so this package does not depend on
// the dispatcher's owner.
func (s *Service) SetDispatchNotifier(notify func()) {
	s.notify = notify
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
	return s.submitTask(ctx, userID, styleID, inputFileKey, clientRequestID, "")
}

// RetryStyleGeneration submits a new task from a failed or expired one, reusing
// the original input image so the client does not have to still hold it. The
// original row is kept for its failure details and marked as superseded by the
// new task, which both hides it from the user's list and makes a task retryable
// only once. It is separately charged: the original was refunded when it failed.
func (s *Service) RetryStyleGeneration(ctx context.Context, userID uint64, sourceTaskID, clientRequestID string) (*CreateTaskResult, error) {
	if clientRequestID == "" {
		return nil, apperr.InvalidArgument("client_request_id required")
	}
	if sourceTaskID == "" {
		return nil, apperr.InvalidArgument("task_id required")
	}

	source, err := s.GetStyleGeneration(ctx, userID, sourceTaskID)
	if err != nil {
		return nil, err
	}
	if source.Status != model.AIGenStatusFailed && source.Status != model.AIGenStatusExpired {
		return nil, apperr.InvalidArgument("只有失败或过期的任务可以重试")
	}
	if source.InputFileKey == "" {
		return nil, apperr.InvalidArgument("原任务缺少原图，无法重试")
	}
	// An already-superseded source is deliberately not rejected here: a resend of
	// the same client_request_id must still come back as the duplicate it is, and
	// only submitTask knows that. Rejecting a genuine second retry is left to the
	// superseded marker inside its transaction.

	return s.submitTask(ctx, userID, source.StyleID, source.InputFileKey, clientRequestID, source.TaskID)
}

// submitTask is the whole accept-and-charge path, shared by first submission and
// retry. A non-empty sourceTaskID makes it a retry. Everything is re-validated
// against current state rather than copied from a previous task: a deleted input,
// a deactivated style or a changed price must all be seen before the user is
// charged again.
func (s *Service) submitTask(ctx context.Context, userID uint64, styleID uint64, inputFileKey, clientRequestID, sourceTaskID string) (*CreateTaskResult, error) {
	origin := originFirstAttempt
	if sourceTaskID != "" {
		origin = originRetry
	}

	asset, err := s.mediaDAO.GetUploadedAsset(ctx, inputFileKey, userID, styleInputPurpose)
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
	// Resolve before charging: an unconfigured model must not produce a task
	// that can only ever fail. The result is pinned onto the task row below so
	// a later config change cannot move an in-flight task to another model.
	modelKey := s.resolveModelKey(ctx, style, origin)
	if _, err := s.providers.Resolve(modelKey); err != nil {
		return nil, apperr.Internal("resolve ai provider", err)
	}

	tier := s.quotaTierFor(ctx, userID)

	var result *CreateTaskResult
	err = db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Locking the credit account is unconditional, including for free
		// styles: it is what serializes concurrent submissions from the same
		// user, which is what makes the quota count below race-free. It must
		// also come before any consistent read, because that read is what
		// establishes the REPEATABLE READ snapshot -- a snapshot taken before
		// the lock is granted would hide a concurrent submission from the count.
		account, accErr := s.creditService.GetAccountForUpdate(tx, userID)
		if accErr != nil {
			return apperr.Internal("get credit account", accErr)
		}

		existing, txErr := s.aiDAO.GetByUserRequestIDTx(tx, userID, clientRequestID)
		if txErr != nil {
			return apperr.Internal("check existing task", txErr)
		}
		if existing != nil {
			result = &CreateTaskResult{
				TaskID:           existing.TaskID,
				Status:           existing.Status,
				CreditsDeducted:  existing.CreditsDeducted,
				RemainingBalance: account.Balance,
				Duplicated:       true,
			}
			return nil
		}

		active, cntErr := s.aiDAO.CountUserTasksByStatusTx(tx, userID,
			[]int8{model.AIGenStatusPending, model.AIGenStatusRunning})
		if cntErr != nil {
			return apperr.Internal("count active ai tasks", cntErr)
		}
		if int(active) >= tier.QueueSize {
			return apperr.TaskQuotaExceeded(int(active), tier.QueueSize)
		}

		taskID := uuid.NewString()
		creditsDeducted := 0
		remainingBalance := account.Balance

		if style.CostCredits > 0 {
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
		}

		expiredAt := time.Now().Add(time.Duration(s.config.TaskExpireMinutes) * time.Minute)
		task := &model.AIGeneration{
			TaskID:          taskID,
			UserID:          userID,
			ClientRequestID: clientRequestID,
			StyleID:         styleID,
			InputFileKey:    inputFileKey,
			InputImageURL:   asset.FileURL,
			Provider:        modelKey,
			CreditsDeducted: creditsDeducted,
			Status:          model.AIGenStatusPending,
			ExpiredAt:       &expiredAt,
		}

		if createErr := s.aiDAO.CreateTaskTx(tx, task); createErr != nil {
			if isDuplicateKey(createErr) {
				return errDuplicateAITask
			}
			return apperr.Internal("create ai task", createErr)
		}

		// Inside the same transaction as the charge, so a source task that another
		// retry already claimed rolls this one back instead of charging twice.
		if sourceTaskID != "" {
			marked, markErr := s.aiDAO.MarkSupersededTx(tx, sourceTaskID, taskID)
			if markErr != nil {
				return apperr.Internal("mark source task superseded", markErr)
			}
			if !marked {
				return apperr.InvalidArgument("该任务已经重试过了")
			}
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

	// After commit, so the dispatcher cannot look for a row that is not visible
	// yet.
	if !result.Duplicated && s.notify != nil {
		s.notify()
	}

	return result, nil
}

// resolveModelKey decides which configured model serves a task. A user-initiated
// retry looks at the retry model first, so it does not go straight back to the
// model that just failed. For a first attempt the bb_config override wins so
// operations can move all online traffic onto another model with one UPDATE; the
// style's own column is next, and the YAML default is the last resort.
func (s *Service) resolveModelKey(ctx context.Context, style *model.AIStyle, origin submitOrigin) string {
	if origin == originRetry {
		if key := s.retryModelKey(ctx); key != "" {
			return key
		}
	}
	if s.configStore != nil {
		active, err := s.configStore.GetConfig(ctx, ActiveModelConfigKey)
		if err != nil {
			// A config read failure must not take generation down: fall through
			// to the style's own model.
			zap.L().Warn("read active ai model config failed", zap.Error(err))
		} else if key := strings.TrimSpace(active); key != "" {
			return key
		}
	}
	return firstNonEmpty(style.Provider, s.config.DefaultModel)
}

// retryModelKey returns the model a user-initiated retry should use, or "" to
// fall back to the first-attempt chain. An unconfigured key degrades instead of
// rejecting the request: unlike ActiveModelConfigKey, refusing a retry would
// leave the user with no way forward at all, so retrying on the original model
// beats not retrying.
func (s *Service) retryModelKey(ctx context.Context) string {
	key := ""
	if s.configStore != nil {
		configured, err := s.configStore.GetConfig(ctx, RetryModelConfigKey)
		if err != nil {
			zap.L().Warn("read retry ai model config failed", zap.Error(err))
		} else {
			key = strings.TrimSpace(configured)
		}
	}
	key = firstNonEmpty(key, s.config.RetryModel)
	if key == "" {
		return ""
	}
	if _, ok := s.providers.Get(key); !ok {
		zap.L().Warn("retry ai model is not configured, falling back to the first-attempt model",
			zap.String("retry_model", key))
		return ""
	}
	return key
}

// styleOptions parses bb_ai_style.config into the per-style knobs layered over
// the model's YAML defaults. Malformed JSON is ignored rather than fatal, so a
// bad edit degrades to the defaults instead of failing every task for a style.
func styleOptions(style *model.AIStyle) map[string]string {
	raw := strings.TrimSpace(style.Config)
	if raw == "" {
		return nil
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		zap.L().Warn("ignoring malformed ai style config",
			zap.String("style_key", style.StyleKey), zap.Error(err))
		return nil
	}
	options := make(map[string]string, len(decoded))
	for key, value := range decoded {
		var text string
		if err := json.Unmarshal(value, &text); err == nil {
			options[key] = text
			continue
		}
		options[key] = string(value)
	}
	return options
}

func (s *Service) quotaTierFor(ctx context.Context, userID uint64) quotaTier {
	isVIP := false
	if s.vip != nil {
		isVIP, _ = s.vip.IsVIP(ctx, userID)
	}
	if isVIP {
		return quotaTier{Concurrent: s.config.VIPConcurrent, QueueSize: s.config.VIPQueueSize}
	}
	return quotaTier{Concurrent: s.config.FreeConcurrent, QueueSize: s.config.FreeQueueSize}
}

func (s *Service) loadDuplicateResult(ctx context.Context, userID uint64, clientRequestID string) (*CreateTaskResult, error) {
	var result *CreateTaskResult
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		account, accErr := s.creditService.GetAccountForUpdate(tx, userID)
		if accErr != nil {
			return apperr.Internal("get credit account", accErr)
		}
		existing, txErr := s.aiDAO.GetByUserRequestIDTx(tx, userID, clientRequestID)
		if txErr != nil {
			return apperr.Internal("load duplicate task", txErr)
		}
		if existing == nil {
			return apperr.Internal("load duplicate task", errDuplicateAITask)
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

// ClaimBatch takes ownership of up to limit pending tasks. Every returned task
// is already marked running in the database, so the caller must execute it or
// leave it for the stuck-running reaper.
func (s *Service) ClaimBatch(ctx context.Context, limit int) ([]*model.AIGeneration, error) {
	if limit <= 0 {
		return nil, nil
	}
	// Over-fetch: the head of the queue may belong entirely to users already at
	// their concurrency cap, and we need to see past them.
	candidates, err := s.aiDAO.ListPendingForDispatch(ctx, limit*4)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	runningByUser := make(map[uint64]int64, len(candidates))
	tierByUser := make(map[uint64]quotaTier, len(candidates))
	claimed := make([]*model.AIGeneration, 0, limit)

	for _, candidate := range candidates {
		if len(claimed) >= limit {
			break
		}
		// Already past its deadline: leave it to the reaper instead of paying
		// for an image that will be refunded anyway.
		if candidate.ExpiredAt != nil && !candidate.ExpiredAt.After(now) {
			continue
		}

		tier, ok := tierByUser[candidate.UserID]
		if !ok {
			tier = s.quotaTierFor(ctx, candidate.UserID)
			tierByUser[candidate.UserID] = tier
		}
		running, ok := runningByUser[candidate.UserID]
		if !ok {
			running, err = s.aiDAO.CountUserTasksByStatus(ctx, candidate.UserID, []int8{model.AIGenStatusRunning})
			if err != nil {
				return nil, err
			}
			runningByUser[candidate.UserID] = running
		}
		if running >= int64(tier.Concurrent) {
			continue
		}

		owned, claimErr := s.aiDAO.ClaimPendingTask(ctx, candidate.TaskID, now)
		if claimErr != nil {
			return nil, claimErr
		}
		if !owned {
			continue
		}
		runningByUser[candidate.UserID] = running + 1
		claimed = append(claimed, candidate)
	}
	return claimed, nil
}

// ExecuteTask runs one claimed task to a terminal state. It never returns an
// error: every outcome is persisted here, because nobody is waiting on it.
func (s *Service) ExecuteTask(ctx context.Context, taskID string) {
	task, err := s.aiDAO.GetByTaskID(ctx, taskID)
	if err != nil {
		zap.L().Error("load ai task failed", zap.String("task_id", taskID), zap.Error(err))
		return
	}
	if task.Status != model.AIGenStatusRunning {
		return
	}

	result, failure := s.runAttempts(ctx, task)
	if failure != nil {
		zap.L().Warn("ai task failed",
			zap.String("task_id", taskID),
			zap.String("error_code", failure.code),
			zap.String("provider_detail", failure.detail))
		s.finalizeFailure(ctx, taskID, model.AIGenStatusFailed, failure.code, failure.message,
			model.AIGenStatusRunning)
		return
	}

	if err := s.persistSuccess(ctx, task, result); err != nil {
		zap.L().Error("persist ai output failed", zap.String("task_id", taskID), zap.Error(err))
		s.finalizeFailure(ctx, taskID, model.AIGenStatusFailed, "storage_write_failed", "生成结果保存失败",
			model.AIGenStatusRunning)
	}
}

// FailTask puts a task into a terminal failed state and refunds it. Used for
// outcomes discovered outside the normal execution path, such as a panic.
func (s *Service) FailTask(ctx context.Context, taskID, errCode, errMsg string) {
	s.finalizeFailure(ctx, taskID, model.AIGenStatusFailed, errCode, errMsg,
		model.AIGenStatusPending, model.AIGenStatusRunning)
}

type taskFailure struct {
	code    string
	message string
	detail  string
}

func (s *Service) runAttempts(ctx context.Context, task *model.AIGeneration) (*Result, *taskFailure) {
	style, err := s.aiDAO.GetStyleByIDAnyStatus(ctx, task.StyleID)
	if err != nil {
		return nil, &taskFailure{code: "style_missing", message: "风格不存在", detail: err.Error()}
	}
	// task.Provider was pinned when the task was accepted, so a config change
	// mid-flight cannot move it to another model. The fallback only covers rows
	// written before the column existed, where the retry origin is unknowable.
	provider, err := s.providers.Resolve(firstNonEmpty(task.Provider, s.resolveModelKey(ctx, style, originFirstAttempt)))
	if err != nil {
		return nil, &taskFailure{code: "provider_not_configured", message: "生成服务不可用", detail: err.Error()}
	}
	if s.store == nil {
		return nil, &taskFailure{code: "storage_unconfigured", message: "生成服务不可用", detail: "image store is not configured"}
	}

	content, contentType, err := s.store.GetObjectBytes(ctx, task.UserID, task.InputFileKey, styleInputPurpose)
	if err != nil {
		return nil, &taskFailure{code: "input_unreadable", message: "原图读取失败", detail: err.Error()}
	}
	s.storeInputThumbnail(ctx, task, content)

	req := &SubmitRequest{
		StyleKey:   style.StyleKey,
		Prompt:     style.PromptTemplate,
		Negative:   style.NegativePrompt,
		ModelName:  style.ModelName,
		Options:    styleOptions(style),
		InputImage: content,
		InputName:  path.Base(task.InputFileKey),
		InputMIME:  contentType,
	}

	// One retry, inside the same worker slot: releasing and re-queueing would
	// turn a provider outage into a retry storm.
	const maxAttempts = 2
	for attempt := 1; ; attempt++ {
		result, submitErr := provider.Submit(ctx, req)
		if submitErr != nil {
			return nil, &taskFailure{code: "provider_error", message: "生成失败，请稍后重试", detail: submitErr.Error()}
		}
		if result.Status == StatusSucceeded {
			return result, nil
		}
		if result.Retryable && attempt < maxAttempts {
			select {
			case <-ctx.Done():
				return nil, &taskFailure{code: "provider_timeout", message: "生成超时，请稍后重试", detail: ctx.Err().Error()}
			case <-time.After(retryBackoff):
			}
			continue
		}
		return nil, &taskFailure{
			code:    firstNonEmpty(result.ErrorCode, "provider_failed"),
			message: "生成失败，请稍后重试",
			detail:  result.ErrorMsg,
		}
	}
}

// storeInputThumbnail reuses the input bytes already buffered for the provider
// call, so it costs one encode and one upload instead of a second download. It
// is persisted on its own rather than with the task result so that a failed
// generation still gets a cheap image in the history list.
func (s *Service) storeInputThumbnail(ctx context.Context, task *model.AIGeneration, content []byte) {
	thumbnailURL, err := s.store.PutThumbnail(ctx, task.InputFileKey, content)
	if err != nil {
		zap.L().Warn("ai input thumbnail generation failed",
			zap.String("task_id", task.TaskID), zap.Error(err))
		return
	}
	if err := s.aiDAO.UpdateTask(ctx, task.TaskID, map[string]interface{}{
		"input_thumbnail_url": thumbnailURL,
	}); err != nil {
		zap.L().Warn("persist ai input thumbnail failed",
			zap.String("task_id", task.TaskID), zap.Error(err))
	}
}

func (s *Service) persistSuccess(ctx context.Context, task *model.AIGeneration, result *Result) error {
	// The image is already generated and paid for, so the transfer to our own
	// storage must not die with the attempt deadline.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
	defer cancel()

	now := time.Now()
	updates := map[string]interface{}{
		"status":       model.AIGenStatusSucceeded,
		"completed_at": &now,
	}
	if result.JobID != "" {
		updates["provider_job_id"] = result.JobID
	}

	switch {
	case len(result.ImageBytes) > 0:
		contentType := result.ImageMIME
		if contentType == "" {
			contentType = "image/png"
		}
		fileKey, fileURL, err := s.store.UploadAIOutput(persistCtx, task.UserID, contentType, result.ImageBytes)
		if err != nil {
			return err
		}
		updates["output_file_key"] = fileKey
		updates["output_image_url"] = fileURL
		// A missing thumbnail only costs the client extra bytes, so it must not
		// discard an image the user has already paid for.
		if thumbnailURL, thumbErr := s.store.PutThumbnail(persistCtx, fileKey, result.ImageBytes); thumbErr != nil {
			zap.L().Warn("ai output thumbnail generation failed",
				zap.String("task_id", task.TaskID), zap.Error(thumbErr))
		} else {
			updates["output_thumbnail_url"] = thumbnailURL
		}
	case result.OutputURL != "":
		updates["output_image_url"] = result.OutputURL
	default:
		return errors.New("provider returned no image")
	}

	applied, err := s.aiDAO.CompleteTaskIfRunning(persistCtx, task.TaskID, updates)
	if err != nil {
		return err
	}
	if !applied {
		// The reaper got there first and already refunded the user; keep its
		// terminal state rather than handing back an image they were paid for.
		zap.L().Warn("ai task was already finalized, discarding output",
			zap.String("task_id", task.TaskID))
	}
	return nil
}

// finalizeFailure moves a task to a terminal state and refunds it at most once,
// both in one transaction. Doing the two together is what makes concurrent
// callers (worker, reaper) safe: the loser sees a status it does not accept and
// does nothing. Lock order is bb_ai_generation then bb_credit_account, matching
// every other path.
func (s *Service) finalizeFailure(ctx context.Context, taskID string, terminalStatus int8, errCode, errMsg string, allowedFrom ...int8) bool {
	// The deadline that caused the failure has usually already cancelled ctx, so
	// settling on it would roll the refund back and leave the task running until
	// the reaper picks it up minutes later.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), settleTimeout)
	defer cancel()

	applied := false
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		locked, lockErr := s.aiDAO.GetByTaskIDForUpdate(tx, taskID)
		if lockErr != nil {
			return lockErr
		}
		if !containsStatus(allowedFrom, locked.Status) {
			return nil
		}

		now := time.Now()
		updates := map[string]interface{}{
			"status":       terminalStatus,
			"completed_at": &now,
		}
		if errCode != "" {
			updates["error_code"] = errCode
		}
		if errMsg != "" {
			updates["error_message"] = errMsg
		}

		if locked.CreditsDeducted > 0 && locked.RefundedAt == nil {
			reason := "ai_generation"
			remark := "AI任务失败退还"
			if terminalStatus == model.AIGenStatusExpired {
				reason = "ai_generation_expired"
				remark = "AI任务超时退还"
			}
			if _, refundErr := s.creditService.AddCreditsTx(tx, locked.UserID, locked.CreditsDeducted,
				"refund", reason, taskID, remark); refundErr != nil {
				return refundErr
			}
			updates["refunded_at"] = &now
		}

		if updErr := s.aiDAO.UpdateTaskTx(tx, taskID, updates); updErr != nil {
			return updErr
		}
		applied = true
		return nil
	})
	if err != nil {
		zap.L().Error("finalize ai task failed", zap.String("task_id", taskID), zap.Error(err))
		return false
	}
	return applied
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

// ReapTasks recovers the two states nobody else will: tasks that sat in the
// queue past their deadline, and tasks whose worker died mid-execution.
func (s *Service) ReapTasks(ctx context.Context) error {
	expired, err := s.aiDAO.FindExpiredPending(ctx, time.Now(), 100)
	if err != nil {
		return err
	}
	for _, task := range expired {
		if s.finalizeFailure(ctx, task.TaskID, model.AIGenStatusExpired, "queue_timeout", "任务排队超时",
			model.AIGenStatusPending) {
			zap.L().Warn("ai task expired while queued", zap.String("task_id", task.TaskID))
		}
	}

	// started_at is rewritten on every claim, so this cutoff cannot catch a
	// worker that is merely slow.
	cutoff := time.Now().Add(-time.Duration(s.config.StuckRunningMinutes) * time.Minute)
	stuck, err := s.aiDAO.FindStuckRunning(ctx, cutoff, 100)
	if err != nil {
		return err
	}
	for _, task := range stuck {
		if s.finalizeFailure(ctx, task.TaskID, model.AIGenStatusFailed, "worker_lost", "生成中断，请重新发起",
			model.AIGenStatusRunning) {
			zap.L().Warn("ai task recovered from a lost worker", zap.String("task_id", task.TaskID))
		}
	}
	return nil
}

func containsStatus(statuses []int8, status int8) bool {
	for _, candidate := range statuses {
		if candidate == status {
			return true
		}
	}
	return false
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
