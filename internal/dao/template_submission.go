package dao

import (
	"context"
	stderrors "errors"
	"time"

	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"gorm.io/gorm"
)

type TemplateSubmissionDAO struct{}

func NewTemplateSubmissionDAO() *TemplateSubmissionDAO { return &TemplateSubmissionDAO{} }

func (d *TemplateSubmissionDAO) DB(ctx context.Context) *gorm.DB {
	return db.DB.WithContext(ctx)
}

func (d *TemplateSubmissionDAO) Create(ctx context.Context, s *model.TemplateSubmission) error {
	return d.DB(ctx).Create(s).Error
}

func (d *TemplateSubmissionDAO) GetByClientRequestID(ctx context.Context, userID uint64, clientRequestID string) (*model.TemplateSubmission, error) {
	return d.first(d.DB(ctx), "user_id = ? AND client_request_id = ?", userID, clientRequestID)
}

// GetActiveByWork 只看未驳回的投稿：驳回后 active_work_key 置 NULL，该作品即可重投。
func (d *TemplateSubmissionDAO) GetActiveByWork(ctx context.Context, userID, workID uint64) (*model.TemplateSubmission, error) {
	return d.first(d.DB(ctx), "user_id = ? AND work_id = ? AND active_work_key IS NOT NULL", userID, workID)
}

func (d *TemplateSubmissionDAO) GetByID(ctx context.Context, id uint64) (*model.TemplateSubmission, error) {
	return d.first(d.DB(ctx), "id = ?", id)
}

// HasPendingByWork 判断作品是否有正在等待审核的投稿。已通过和已驳回都不算：
// 发布出去的模板是独立快照，用户之后怎么改作品都不影响它。
func (d *TemplateSubmissionDAO) HasPendingByWork(ctx context.Context, userID, workID uint64) (bool, error) {
	var count int64
	err := d.DB(ctx).Model(&model.TemplateSubmission{}).
		Where("user_id = ? AND work_id = ? AND status = ?", userID, workID, model.TemplateSubmissionStatusPending).
		Count(&count).Error
	return count > 0, err
}

func (d *TemplateSubmissionDAO) GetByIDTx(tx *gorm.DB, id uint64) (*model.TemplateSubmission, error) {
	return d.first(tx, "id = ?", id)
}

// first 在未命中时返回 (nil, nil)，与 FinishedProductDAO.first 的约定一致。
func (d *TemplateSubmissionDAO) first(query *gorm.DB, where string, args ...interface{}) (*model.TemplateSubmission, error) {
	var sub model.TemplateSubmission
	err := query.Where(where, args...).First(&sub).Error
	if stderrors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

// ListByUser 按 id 倒序返回；beforeID 为 0 时从最新一条开始。
func (d *TemplateSubmissionDAO) ListByUser(ctx context.Context, userID, beforeID uint64, limit int) ([]*model.TemplateSubmission, error) {
	var items []*model.TemplateSubmission
	query := d.DB(ctx).Select(submissionListColumns).Where("user_id = ?", userID)
	if beforeID > 0 {
		query = query.Where("id < ?", beforeID)
	}
	err := query.Order("id DESC").Limit(limit).Find(&items).Error
	return items, err
}

// ListForAdmin 支持按状态过滤的偏移分页。status 为 nil 表示不过滤。
func (d *TemplateSubmissionDAO) ListForAdmin(ctx context.Context, status *int8, offset, limit int) ([]*model.TemplateSubmission, int64, error) {
	var total int64
	countQuery := d.DB(ctx).Model(&model.TemplateSubmission{})
	if status != nil {
		countQuery = countQuery.Where("status = ?", *status)
	}
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var items []*model.TemplateSubmission
	listQuery := d.DB(ctx).Select(submissionListColumns)
	if status != nil {
		listQuery = listQuery.Where("status = ?", *status)
	}
	err := listQuery.Order("id DESC").Offset(offset).Limit(limit).Find(&items).Error
	return items, total, err
}

func (d *TemplateSubmissionDAO) CountByUserSince(ctx context.Context, userID uint64, since time.Time) (int64, error) {
	var count int64
	err := d.DB(ctx).Model(&model.TemplateSubmission{}).
		Where("user_id = ? AND created_at >= ?", userID, since).Count(&count).Error
	return count, err
}

// MarkApprovedTx 以 status = pending 为前置条件做 CAS 更新，返回是否命中。
// 并发审核时败者拿到 false，调用方据此回滚整个事务。
func (d *TemplateSubmissionDAO) MarkApprovedTx(tx *gorm.DB, id, templateID uint64, actor string, at time.Time) (bool, error) {
	result := tx.Model(&model.TemplateSubmission{}).
		Where("id = ? AND status = ?", id, model.TemplateSubmissionStatusPending).
		Updates(map[string]interface{}{
			"status":         model.TemplateSubmissionStatusApproved,
			"template_id":    templateID,
			"reviewer_actor": actor,
			"reviewed_at":    at,
		})
	return result.RowsAffected > 0, result.Error
}

// MarkRejected 必须用 map 更新：struct 更新会跳过 nil 指针，active_work_key 就清不掉，
// 该作品也就永远无法重投。
func (d *TemplateSubmissionDAO) MarkRejected(ctx context.Context, id uint64, actor, reason string, at time.Time) (bool, error) {
	result := d.DB(ctx).Model(&model.TemplateSubmission{}).
		Where("id = ? AND status = ?", id, model.TemplateSubmissionStatusPending).
		Updates(map[string]interface{}{
			"status":          model.TemplateSubmissionStatusRejected,
			"active_work_key": gorm.Expr("NULL"),
			"reviewer_actor":  actor,
			"review_reason":   reason,
			"reviewed_at":     at,
		})
	return result.RowsAffected > 0, result.Error
}

// submissionListColumns 刻意排除 pattern_data：满板快照约 150-250 KB，
// 列表页拉不动，只有详情页才需要。
var submissionListColumns = []string{
	"id", "created_at", "updated_at", "user_id", "work_id", "active_work_key",
	"title", "description", "board_spec", "width", "height", "bead_count", "color_count",
	"preview_url", "thumbnail_url", "original_image_url",
	"status", "reviewer_actor", "review_reason", "reviewed_at", "template_id", "client_request_id",
}
