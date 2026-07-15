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

func (d *AIGenerationDAO) GetByUserRequestIDForUpdate(tx *gorm.DB, userID uint64, clientRequestID string) (*model.AIGeneration, error) {
	var task model.AIGeneration
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ? AND client_request_id = ?", userID, clientRequestID).First(&task).Error
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

func (d *AIGenerationDAO) FindPendingOrRunning(ctx context.Context, before time.Time, limit int) ([]*model.AIGeneration, error) {
	var tasks []*model.AIGeneration
	err := d.DB(ctx).
		Where("status IN ? AND created_at < ?", []int8{model.AIGenStatusPending, model.AIGenStatusRunning}, before).
		Limit(limit).Find(&tasks).Error
	return tasks, err
}

func (d *AIGenerationDAO) FindExpired(ctx context.Context, now time.Time, limit int) ([]*model.AIGeneration, error) {
	var tasks []*model.AIGeneration
	err := d.DB(ctx).
		Where("status IN ? AND expired_at IS NOT NULL AND expired_at < ?",
			[]int8{model.AIGenStatusPending, model.AIGenStatusRunning}, now).
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
