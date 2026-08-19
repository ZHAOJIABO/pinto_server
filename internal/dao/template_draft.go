package dao

import (
	"context"
	"time"

	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// NowMillis 是 bb_template_draft.updated_at 唯一合法的时间戳来源，导出是为了让服务层
// 在 INSERT 时用同一个精度，而不是各自实现一份。该列既是乐观锁令牌又要回传给前端做
// 基线，所以「写进去的精度」必须等于「序列化出去的精度」，否则精确相等比较会随驱动
// 而异：gorm.io/driver/mysql 把 NowFunc 截断到毫秒，而 sqlite 驱动没有这个覆盖，底层
// mattn/go-sqlite3 按纳秒格式绑定时间——交给 GORM 生成时间戳会让同一段代码在 MySQL 上
// 正常、在测试用的 SQLite 上永远判定冲突。
func NowMillis() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

// templateDraftListColumns 刻意排除 pattern_data，理由同 templateListColumns：
// MySQL 的 filesort 会缓冲每一个被选中的列，几百 KB 的 JSON 一排序就撞 error 1038
// (out of sort memory)。草稿列表要 ORDER BY updated_at，所以这条投影是承重的。
// idempotency_key 也不返回，它只是服务端的防重键。
var templateDraftListColumns = []string{
	"id", "template_id", "title", "description", "category_id", "tags", "difficulty",
	"preview_file_key", "preview_url", "thumbnail_url", "board_spec",
	"width", "height", "bead_count", "color_count",
	"updated_by_actor", "created_at", "updated_at",
}

// TemplateDraftDAO 铁律：所有对 bb_template_draft 的写入必须用 map 形式的 Updates
// 并显式带上 updated_at；禁止 Save 与 struct 形式的 Updates。GORM 的
// callbacks/update.go 对 struct 目标无条件重写 AutoUpdateTime 字段，会把乐观锁令牌
// 冲掉，只有 map 形式才尊重显式值。这与 template_submission.go 里 MarkRejected
// 已有的「必须用 map 更新」是同一类问题。
type TemplateDraftDAO struct{}

func NewTemplateDraftDAO() *TemplateDraftDAO { return &TemplateDraftDAO{} }

func (d *TemplateDraftDAO) DB(ctx context.Context) *gorm.DB {
	return db.DB.WithContext(ctx)
}

func (d *TemplateDraftDAO) Create(ctx context.Context, draft *model.TemplateDraft) error {
	return d.DB(ctx).Create(draft).Error
}

// GetByID 返回整行（含 pattern_data），只给详情与发布路径用。
func (d *TemplateDraftDAO) GetByID(ctx context.Context, id uint64) (*model.TemplateDraft, error) {
	var draft model.TemplateDraft
	err := d.DB(ctx).Where("id = ?", id).First(&draft).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &draft, nil
}

func (d *TemplateDraftDAO) GetByIdempotencyKey(ctx context.Context, key string) (*model.TemplateDraft, error) {
	var draft model.TemplateDraft
	err := d.DB(ctx).Where("idempotency_key = ?", key).First(&draft).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &draft, nil
}

func (d *TemplateDraftDAO) GetByTemplateID(ctx context.Context, templateID uint64) (*model.TemplateDraft, error) {
	var draft model.TemplateDraft
	err := d.DB(ctx).Where("template_id = ?", templateID).First(&draft).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &draft, nil
}

func (d *TemplateDraftDAO) Count(ctx context.Context) (int64, error) {
	var count int64
	err := d.DB(ctx).Model(&model.TemplateDraft{}).Count(&count).Error
	return count, err
}

// List 按 updated_at 倒序返回，id 作为次级键保证 offset 分页确定。投影必须留在
// templateDraftListColumns，不要图省事换成裸 Find。
func (d *TemplateDraftDAO) List(ctx context.Context, offset, limit int) ([]*model.TemplateDraft, int64, error) {
	var total int64
	if err := d.DB(ctx).Model(&model.TemplateDraft{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []*model.TemplateDraft{}, 0, nil
	}

	var drafts []*model.TemplateDraft
	err := d.DB(ctx).Model(&model.TemplateDraft{}).
		Select(templateDraftListColumns).
		Order("updated_at DESC, id DESC").
		Offset(offset).Limit(limit).
		Find(&drafts).Error
	if err != nil {
		return nil, 0, err
	}
	return drafts, total, nil
}

// UpdateWithLock 是草稿的乐观锁写入：只有库里的 updated_at 与 baseUpdatedAt 精确
// 相等才落盘。它自己生成新的 updated_at 并回传，调用方不要预先塞进 fields。
//
// RowsAffected == 0 有两种成因，必须再查一次才能区分「被别人改过」（4001）与
// 「已经不存在」（4002）——手法同 TemplateDAO.UpdatePublishedTemplate。
func (d *TemplateDraftDAO) UpdateWithLock(
	ctx context.Context,
	id uint64,
	baseUpdatedAt time.Time,
	fields map[string]interface{},
) (time.Time, bool, error) {
	updatedAt := NowMillis()
	// 令牌必须严格前进。若这次写入落在上一次写入的同一毫秒里，NowMillis 会算出与
	// baseUpdatedAt 相同的值，updated_at 于是原地不动——持有同一个旧基线的第三方还能
	// 再写一次并静默覆盖本次改动。毫秒精度本身挡不住这个窗口，只有单调递增能挡住。
	if !updatedAt.After(baseUpdatedAt) {
		updatedAt = baseUpdatedAt.Add(time.Millisecond)
	}
	values := make(map[string]interface{}, len(fields)+1)
	for k, v := range fields {
		values[k] = v
	}
	values["updated_at"] = updatedAt

	result := d.DB(ctx).Model(&model.TemplateDraft{}).
		Where("id = ? AND updated_at = ?", id, baseUpdatedAt).
		Updates(values)
	if result.Error != nil {
		return time.Time{}, false, result.Error
	}
	if result.RowsAffected > 0 {
		return updatedAt, true, nil
	}

	var count int64
	if err := d.DB(ctx).Model(&model.TemplateDraft{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return time.Time{}, false, err
	}
	return time.Time{}, count > 0, nil
}

// DeleteByID 报告是否真的删掉了一行，让调用方把「重复删除」处理成幂等成功。
func (d *TemplateDraftDAO) DeleteByID(ctx context.Context, id uint64) (bool, error) {
	result := d.DB(ctx).Where("id = ?", id).Delete(&model.TemplateDraft{})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// GetByIDForUpdateTx 持有行锁到事务结束。这把锁是并发发布与并发自动保存之间唯一的
// 串行化手段：两个管理员同时点发布会生成不同的 idempotency_key，发布记录的唯一键
// 挡不住他们。
func (d *TemplateDraftDAO) GetByIDForUpdateTx(tx *gorm.DB, id uint64) (*model.TemplateDraft, error) {
	var draft model.TemplateDraft
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&draft).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &draft, nil
}

// DeleteWithLockTx 是发布末尾的双保险 CAS：SQLite 方言会静默丢弃 FOR UPDATE 子句，
// 这条 WHERE 上的 updated_at 让并发保护在那种环境下仍然成立。
func (d *TemplateDraftDAO) DeleteWithLockTx(tx *gorm.DB, id uint64, baseUpdatedAt time.Time) (bool, error) {
	result := tx.Where("id = ? AND updated_at = ?", id, baseUpdatedAt).Delete(&model.TemplateDraft{})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// MapDraftIDsByTemplateIDs 供已发布模板列表标注「这张图有未发布的改动」。
// Select 是强制的：裸 Find 是 SELECT *，一次 100 行会把 100 份 pattern_data 拉回内存。
func (d *TemplateDraftDAO) MapDraftIDsByTemplateIDs(ctx context.Context, templateIDs []uint64) (map[uint64]uint64, error) {
	if len(templateIDs) == 0 {
		return map[uint64]uint64{}, nil
	}

	var rows []struct {
		ID         uint64
		TemplateID uint64
	}
	err := d.DB(ctx).Model(&model.TemplateDraft{}).
		Select("id", "template_id").
		Where("template_id IN ?", templateIDs).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	result := make(map[uint64]uint64, len(rows))
	for _, row := range rows {
		result[row.TemplateID] = row.ID
	}
	return result, nil
}
