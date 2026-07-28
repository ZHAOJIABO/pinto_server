package dao

import (
	"context"
	"time"

	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AIGenerationDAO struct{}

func NewAIGenerationDAO() *AIGenerationDAO { return &AIGenerationDAO{} }

func (d *AIGenerationDAO) DB(ctx context.Context) *gorm.DB {
	return db.DB.WithContext(ctx)
}

// Style methods

func (d *AIGenerationDAO) ListActiveStyles(ctx context.Context) ([]*model.AIStyle, error) {
	var styles []*model.AIStyle
	err := d.DB(ctx).Where("status = 1").Order("sort_order ASC").Find(&styles).Error
	return styles, err
}

func (d *AIGenerationDAO) GetStyleByID(ctx context.Context, id uint64) (*model.AIStyle, error) {
	var style model.AIStyle
	err := d.DB(ctx).Where("id = ? AND status = 1", id).First(&style).Error
	return &style, err
}

// GetStyleByIDAnyStatus is used when executing an already-accepted task, so
// deactivating a style does not fail the tasks already queued against it.
func (d *AIGenerationDAO) GetStyleByIDAnyStatus(ctx context.Context, id uint64) (*model.AIStyle, error) {
	var style model.AIStyle
	err := d.DB(ctx).Where("id = ?", id).First(&style).Error
	return &style, err
}

// Task methods

func (d *AIGenerationDAO) CreateTask(ctx context.Context, task *model.AIGeneration) error {
	return d.DB(ctx).Create(task).Error
}

func (d *AIGenerationDAO) CreateTaskTx(tx *gorm.DB, task *model.AIGeneration) error {
	return tx.Create(task).Error
}

func (d *AIGenerationDAO) GetByTaskID(ctx context.Context, taskID string) (*model.AIGeneration, error) {
	var task model.AIGeneration
	err := d.DB(ctx).Where("task_id = ?", taskID).First(&task).Error
	return &task, err
}

func (d *AIGenerationDAO) GetByTaskIDForUpdate(tx *gorm.DB, taskID string) (*model.AIGeneration, error) {
	var task model.AIGeneration
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("task_id = ?", taskID).First(&task).Error
	return &task, err
}

// GetByUserRequestIDTx must not lock: on a miss, InnoDB would take a gap lock on
// uk_ai_gen_user_request that every concurrent submission then deadlocks against
// when its own insert needs an insert-intention lock. Idempotency comes from the
// unique key rejecting the duplicate insert instead.
func (d *AIGenerationDAO) GetByUserRequestIDTx(tx *gorm.DB, userID uint64, clientRequestID string) (*model.AIGeneration, error) {
	var task model.AIGeneration
	err := tx.Where("user_id = ? AND client_request_id = ?", userID, clientRequestID).First(&task).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &task, err
}

func (d *AIGenerationDAO) UpdateTask(ctx context.Context, taskID string, updates map[string]interface{}) error {
	return d.DB(ctx).Model(&model.AIGeneration{}).
		Where("task_id = ?", taskID).Updates(updates).Error
}

func (d *AIGenerationDAO) UpdateTaskTx(tx *gorm.DB, taskID string, updates map[string]interface{}) error {
	return tx.Model(&model.AIGeneration{}).
		Where("task_id = ?", taskID).Updates(updates).Error
}

func (d *AIGenerationDAO) ListByUserID(ctx context.Context, userID uint64, offset, limit int) ([]*model.AIGeneration, int64, error) {
	var tasks []*model.AIGeneration
	var total int64
	query := d.DB(ctx).Where("user_id = ?", userID)
	query.Model(&model.AIGeneration{}).Count(&total)
	err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&tasks).Error
	return tasks, total, err
}

// FindExpiredPending only considers pending tasks. Running tasks are left to
// FindStuckRunning, so a task that waited in the queue until its deadline is
// never refunded out from under a provider call that is actively in flight.
func (d *AIGenerationDAO) FindExpiredPending(ctx context.Context, now time.Time, limit int) ([]*model.AIGeneration, error) {
	var tasks []*model.AIGeneration
	err := d.DB(ctx).
		Where("status = ? AND expired_at IS NOT NULL AND expired_at < ?",
			model.AIGenStatusPending, now).
		Limit(limit).Find(&tasks).Error
	return tasks, err
}

// ClaimPendingTask atomically moves a task from pending to running. Only one
// caller can observe RowsAffected == 1, so a task is never executed twice even
// without an enclosing transaction or row lock.
func (d *AIGenerationDAO) ClaimPendingTask(ctx context.Context, taskID string, startedAt time.Time) (bool, error) {
	result := d.DB(ctx).Model(&model.AIGeneration{}).
		Where("task_id = ? AND status = ?", taskID, model.AIGenStatusPending).
		Updates(map[string]interface{}{
			"status":     model.AIGenStatusRunning,
			"started_at": startedAt,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// CompleteTaskIfRunning applies a terminal update only while the row is still
// running, so a worker finishing late cannot overwrite a status the reaper
// already wrote (and already refunded).
func (d *AIGenerationDAO) CompleteTaskIfRunning(ctx context.Context, taskID string, updates map[string]interface{}) (bool, error) {
	result := d.DB(ctx).Model(&model.AIGeneration{}).
		Where("task_id = ? AND status = ?", taskID, model.AIGenStatusRunning).
		Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// ListPendingForDispatch returns the oldest pending tasks in submission order.
func (d *AIGenerationDAO) ListPendingForDispatch(ctx context.Context, limit int) ([]*model.AIGeneration, error) {
	var tasks []*model.AIGeneration
	err := d.DB(ctx).
		Where("status = ?", model.AIGenStatusPending).
		Order("id ASC").Limit(limit).Find(&tasks).Error
	return tasks, err
}

// CountUserTasksByStatus is the single source of truth for per-user quota: task
// rows themselves, so no separate counter can drift out of sync.
func (d *AIGenerationDAO) CountUserTasksByStatus(ctx context.Context, userID uint64, statuses []int8) (int64, error) {
	var total int64
	err := d.DB(ctx).Model(&model.AIGeneration{}).
		Where("user_id = ? AND status IN ?", userID, statuses).Count(&total).Error
	return total, err
}

func (d *AIGenerationDAO) CountUserTasksByStatusTx(tx *gorm.DB, userID uint64, statuses []int8) (int64, error) {
	var total int64
	err := tx.Model(&model.AIGeneration{}).
		Where("user_id = ? AND status IN ?", userID, statuses).Count(&total).Error
	return total, err
}

// FindStuckRunning finds tasks claimed long enough ago that their worker cannot
// still be alive, which happens when the process dies mid-execution.
func (d *AIGenerationDAO) FindStuckRunning(ctx context.Context, before time.Time, limit int) ([]*model.AIGeneration, error) {
	var tasks []*model.AIGeneration
	err := d.DB(ctx).
		Where("status = ? AND started_at IS NOT NULL AND started_at < ?", model.AIGenStatusRunning, before).
		Limit(limit).Find(&tasks).Error
	return tasks, err
}

func (d *AIGenerationDAO) GetUserSucceededTask(ctx context.Context, userID uint64, taskID string) (*model.AIGeneration, error) {
	var task model.AIGeneration
	err := d.DB(ctx).Where("task_id = ? AND user_id = ? AND status = ?",
		taskID, userID, model.AIGenStatusSucceeded).First(&task).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &task, err
}
