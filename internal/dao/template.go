package dao

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TemplateDAO struct{}

func NewTemplateDAO() *TemplateDAO { return &TemplateDAO{} }

// bb_template.status 的取值。StatusBlindBoxOnly 的图纸只能通过盲盒抽到：C 端的
// 列表、搜索、分类计数一律只查 StatusPublished，所以新增这个状态就等于把图纸从
// 所有浏览入口移除，不需要在每个查询上加排除条件。按 ID 精确定位的查询（详情、
// 开盒历史、收藏、后台管理）显式放行两种状态。
const (
	StatusUnpublished  int8 = 0
	StatusPublished    int8 = 1
	StatusBlindBoxOnly int8 = 2
)

// visibleStatuses 是"图纸仍然有效"的状态集合，供按 ID 精确定位的查询使用。
var visibleStatuses = []int8{StatusPublished, StatusBlindBoxOnly}

// templateListColumns omits pattern_data for the same reason the work list does:
// the JSON grid can reach megabytes, and MySQL's filesort buffers every selected
// column, so an "ORDER BY" over these rows fails with error 1038 (out of sort
// memory) once a large pattern exists. templateItemProto is the only consumer of
// list rows and needs nothing beyond these columns.
var templateListColumns = []string{
	"id", "category_id", "title", "preview_url", "thumbnail_url", "description",
	"board_spec", "tags", "difficulty", "width", "height", "color_count",
	"is_free", "credit_cost", "download_count", "favorite_count",
	"contributor_nickname",
}

// qualifiedTemplateListColumns renders the same projection for joined queries,
// where bare column names would be ambiguous.
func qualifiedTemplateListColumns(alias string) string {
	qualified := make([]string, len(templateListColumns))
	for i, column := range templateListColumns {
		qualified[i] = alias + "." + column
	}
	return strings.Join(qualified, ", ")
}

func (d *TemplateDAO) DB(ctx context.Context) *gorm.DB {
	return db.DB.WithContext(ctx)
}

// ListCategories 是 C 端分类导航的数据源，所以排除盲盒专用分类。需要全集的调用方
// （收藏分类聚合、后台分类管理）用 ListAllActiveCategories。
func (d *TemplateDAO) ListCategories(ctx context.Context) ([]*model.TemplateCategory, error) {
	var categories []*model.TemplateCategory
	err := d.DB(ctx).Where("status = 1 AND is_blind_box = ?", false).Order("sort_order ASC").Find(&categories).Error
	return categories, err
}

func (d *TemplateDAO) ListAllActiveCategories(ctx context.Context) ([]*model.TemplateCategory, error) {
	var categories []*model.TemplateCategory
	err := d.DB(ctx).Where("status = 1").Order("sort_order ASC").Find(&categories).Error
	return categories, err
}

func (d *TemplateDAO) ListActiveCategoryNames(ctx context.Context, categoryIDs []int) (map[int]string, error) {
	if len(categoryIDs) == 0 {
		return map[int]string{}, nil
	}

	var categories []model.TemplateCategory
	if err := d.DB(ctx).Select("id", "name").
		Where("status = 1 AND id IN ?", categoryIDs).
		Find(&categories).Error; err != nil {
		return nil, err
	}
	names := make(map[int]string, len(categories))
	for _, category := range categories {
		names[category.ID] = category.Name
	}
	return names, nil
}

func (d *TemplateDAO) GetCategoryByName(ctx context.Context, name string) (*model.TemplateCategory, error) {
	var category model.TemplateCategory
	err := d.DB(ctx).Where("name = ?", name).First(&category).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &category, err
}

func (d *TemplateDAO) GetActiveCategoryByID(ctx context.Context, categoryID int) (*model.TemplateCategory, error) {
	var category model.TemplateCategory
	err := d.DB(ctx).Where("id = ? AND status = 1", categoryID).First(&category).Error
	return &category, err
}

func (d *TemplateDAO) CreateCategory(ctx context.Context, category *model.TemplateCategory) error {
	return d.DB(ctx).Create(category).Error
}

func (d *TemplateDAO) CountByCategory(ctx context.Context, categoryID int) (int64, error) {
	var count int64
	err := d.DB(ctx).Model(&model.Template{}).Where("category_id = ? AND status = 1", categoryID).Count(&count).Error
	return count, err
}

// GetByIDs 放行盲盒专属图纸：开盒历史全靠它回捞，只查 status=1 会让历史恒为空。
func (d *TemplateDAO) GetByIDs(ctx context.Context, ids []uint64) ([]*model.Template, error) {
	var templates []*model.Template
	err := d.DB(ctx).Where("id IN ? AND status IN ?", ids, visibleStatuses).Find(&templates).Error
	return templates, err
}

func (d *TemplateDAO) ListByCategory(ctx context.Context, categoryID int, offset, limit int) ([]*model.Template, int64, error) {
	var templates []*model.Template
	var total int64
	query := d.DB(ctx).Where("category_id = ? AND status = 1", categoryID)
	query.Model(&model.Template{}).Count(&total)
	err := query.Select(templateListColumns).Order("sort_order ASC, created_at DESC").Offset(offset).Limit(limit).Find(&templates).Error
	return templates, total, err
}

func (d *TemplateDAO) ListByScene(ctx context.Context, _ string, offset, limit int) ([]*model.Template, int64, error) {
	return d.ListPublished(ctx, offset, limit)
}

func (d *TemplateDAO) ListPublished(ctx context.Context, offset, limit int) ([]*model.Template, int64, error) {
	var templates []*model.Template
	var total int64
	query := d.DB(ctx).Where("status = 1")
	query.Model(&model.Template{}).Count(&total)
	err := query.Select(templateListColumns).Order("sort_order ASC, created_at DESC").Offset(offset).Limit(limit).Find(&templates).Error
	return templates, total, err
}

// adminTemplateListColumns 比 templateListColumns 多一个 status，后台列表要靠它区分
// 普通图纸和盲盒专属图纸。刻意不往 templateListColumns 里加：那个投影被 C 端多条
// 列表路径共用，加列会把 status 泄漏到客户端 API。
var adminTemplateListColumns = append(append([]string{}, templateListColumns...), "status")

// ListPublishedForAdmin 是后台图纸列表专用：它要看到盲盒专属图纸，而共用的
// ListPublished 必须继续只返回 status=1（C 端 scene=home 也走它）。
func (d *TemplateDAO) ListPublishedForAdmin(ctx context.Context, offset, limit int) ([]*model.Template, int64, error) {
	var templates []*model.Template
	var total int64
	query := d.DB(ctx).Where("status IN ?", visibleStatuses)
	query.Model(&model.Template{}).Count(&total)
	err := query.Select(adminTemplateListColumns).Order("sort_order ASC, created_at DESC").Offset(offset).Limit(limit).Find(&templates).Error
	return templates, total, err
}

// TemplateBrief 是后台奖池列表需要的图纸摘要。不复用 ListThumbnailsByIDs：那个方法
// 缺 title 和 category_id。
type TemplateBrief struct {
	ID           uint64 `gorm:"column:id"`
	Title        string `gorm:"column:title"`
	PreviewURL   string `gorm:"column:preview_url"`
	ThumbnailURL string `gorm:"column:thumbnail_url"`
	CategoryID   int    `gorm:"column:category_id"`
	Status       int8   `gorm:"column:status"`
}

func (d *TemplateDAO) ListBriefsByIDs(ctx context.Context, ids []uint64) (map[uint64]TemplateBrief, error) {
	if len(ids) == 0 {
		return map[uint64]TemplateBrief{}, nil
	}
	var rows []TemplateBrief
	if err := d.DB(ctx).Model(&model.Template{}).
		Select("id", "title", "preview_url", "thumbnail_url", "category_id", "status").
		Where("id IN ? AND status IN ?", ids, visibleStatuses).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	briefs := make(map[uint64]TemplateBrief, len(rows))
	for _, row := range rows {
		briefs[row.ID] = row
	}
	return briefs, nil
}

func (d *TemplateDAO) ListByKeyword(ctx context.Context, keyword string, offset, limit int) ([]*model.Template, int64, error) {
	var templates []*model.Template
	var total int64
	like := fmt.Sprintf("%%%s%%", keyword)
	query := d.DB(ctx).Where("status = 1 AND (title LIKE ? OR tags LIKE ?)", like, like)
	query.Model(&model.Template{}).Count(&total)
	err := query.Select(templateListColumns).Order("sort_order ASC, created_at DESC").Offset(offset).Limit(limit).Find(&templates).Error
	return templates, total, err
}

// GetByID 放行盲盒专属图纸：抽到之后的详情页、收藏校验和后台管理都走它。代价是
// 盲盒图纸可以被猜 id 直接访问，抽奖本身不去重，所以没有防枚举需求。
func (d *TemplateDAO) GetByID(ctx context.Context, id uint64) (*model.Template, error) {
	var tpl model.Template
	err := d.DB(ctx).Where("id = ? AND status IN ?", id, visibleStatuses).First(&tpl).Error
	return &tpl, err
}

func (d *TemplateDAO) IncrementDownload(ctx context.Context, id uint64) error {
	return d.DB(ctx).Model(&model.Template{}).Where("id = ?", id).
		Update("download_count", gorm.Expr("download_count + 1")).Error
}

// Favorite methods

func (d *TemplateDAO) CreateFavorite(ctx context.Context, fav *model.TemplateFavorite) error {
	return d.DB(ctx).Create(fav).Error
}

func (d *TemplateDAO) DeleteFavorite(ctx context.Context, userID, templateID uint64) error {
	return d.DB(ctx).Where("user_id = ? AND template_id = ?", userID, templateID).
		Delete(&model.TemplateFavorite{}).Error
}

func (d *TemplateDAO) GetFavorite(ctx context.Context, userID, templateID uint64) (*model.TemplateFavorite, error) {
	var fav model.TemplateFavorite
	err := d.DB(ctx).Where("user_id = ? AND template_id = ?", userID, templateID).First(&fav).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &fav, err
}

func (d *TemplateDAO) BatchGetFavorited(ctx context.Context, userID uint64, templateIDs []uint64) (map[uint64]bool, error) {
	result := make(map[uint64]bool)
	if len(templateIDs) == 0 {
		return result, nil
	}
	var favs []*model.TemplateFavorite
	err := d.DB(ctx).Where("user_id = ? AND template_id IN ?", userID, templateIDs).Find(&favs).Error
	if err != nil {
		return nil, err
	}
	for _, f := range favs {
		result[f.TemplateID] = true
	}
	return result, nil
}

func (d *TemplateDAO) IncrementFavoriteCount(ctx context.Context, templateID uint64) error {
	return d.DB(ctx).Model(&model.Template{}).Where("id = ?", templateID).
		Update("favorite_count", gorm.Expr("favorite_count + 1")).Error
}

func (d *TemplateDAO) DecrementFavoriteCount(ctx context.Context, templateID uint64) error {
	return d.DB(ctx).Model(&model.Template{}).Where("id = ? AND favorite_count > 0", templateID).
		Update("favorite_count", gorm.Expr("favorite_count - 1")).Error
}

func (d *TemplateDAO) ListFavoriteTemplates(ctx context.Context, userID uint64, categoryID int, offset, limit int) ([]*model.Template, int64, error) {
	// 每次重新构造查询：Count 会污染 gorm 的语句状态，复用同一个 *gorm.DB 拿不到正确的分页结果。
	base := func() *gorm.DB {
		q := d.DB(ctx).Table("bb_template_favorite AS f").
			Joins("JOIN bb_template AS t ON t.id = f.template_id AND t.status IN (1,2)").
			Where("f.user_id = ?", userID)
		if categoryID > 0 {
			q = q.Where("t.category_id = ?", categoryID)
		}
		return q
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []*model.Template{}, 0, nil
	}

	var templates []*model.Template
	err := base().Select(qualifiedTemplateListColumns("t")).
		Order("f.created_at DESC, f.id DESC").
		Offset(offset).Limit(limit).
		Scan(&templates).Error
	if err != nil {
		return nil, 0, err
	}

	return templates, total, nil
}

// FavoriteCategoryCount 表示用户在某个分类下的收藏数量。
type FavoriteCategoryCount struct {
	CategoryID int   `gorm:"column:category_id"`
	Count      int64 `gorm:"column:count"`
}

func (d *TemplateDAO) CountFavoritesByCategory(ctx context.Context, userID uint64) ([]*FavoriteCategoryCount, error) {
	var rows []*FavoriteCategoryCount
	err := d.DB(ctx).Table("bb_template_favorite AS f").
		Select("t.category_id AS category_id, COUNT(*) AS count").
		Joins("JOIN bb_template AS t ON t.id = f.template_id AND t.status IN (1,2)").
		Where("f.user_id = ?", userID).
		Group("t.category_id").
		Scan(&rows).Error
	return rows, err
}

func (d *TemplateDAO) SplitTags(tags string) []string {
	if tags == "" {
		return nil
	}
	parts := strings.Split(tags, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// Admin publishing methods

func (d *TemplateDAO) CreateOrUpdateTemplate(ctx context.Context, tpl *model.Template) (uint64, error) {
	if err := d.DB(ctx).Create(tpl).Error; err != nil {
		return 0, err
	}
	return tpl.ID, nil
}

// UpdatePublishedTemplate 放行盲盒专属图纸，否则入池后后台就再也编辑不了它。
// Updates 的 map 刻意不含 status，所以编辑不会把 status=2 打回 1。
func (d *TemplateDAO) UpdatePublishedTemplate(ctx context.Context, templateID uint64, tpl *model.Template) (bool, error) {
	result := d.DB(ctx).Model(&model.Template{}).Where("id = ? AND status IN ?", templateID, visibleStatuses).
		Updates(map[string]interface{}{
			"category_id":   tpl.CategoryID,
			"title":         tpl.Title,
			"preview_url":   tpl.PreviewURL,
			"thumbnail_url": tpl.ThumbnailURL,
			"description":   tpl.Description,
			"pattern_data":  tpl.PatternData,
			"board_spec":    tpl.BoardSpec,
			"tags":          tpl.Tags,
			"difficulty":    tpl.Difficulty,
			"width":         tpl.Width,
			"height":        tpl.Height,
			"color_count":   tpl.ColorCount,
		})
	if result.Error != nil || result.RowsAffected > 0 {
		return result.RowsAffected > 0, result.Error
	}

	var count int64
	if err := d.DB(ctx).Model(&model.Template{}).Where("id = ? AND status IN ?", templateID, visibleStatuses).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (d *TemplateDAO) UnpublishTemplate(ctx context.Context, templateID uint64) (bool, error) {
	result := d.DB(ctx).Model(&model.Template{}).Where("id = ? AND status IN ?", templateID, visibleStatuses).
		Update("status", StatusUnpublished)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected > 0 {
		return true, nil
	}

	var count int64
	if err := d.DB(ctx).Model(&model.Template{}).Where("id = ?", templateID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (d *TemplateDAO) CreatePublishRecord(ctx context.Context, record *model.TemplatePublishRecord) error {
	return d.DB(ctx).Create(record).Error
}

func (d *TemplateDAO) GetPublishRecordByKey(ctx context.Context, key string) (*model.TemplatePublishRecord, error) {
	var record model.TemplatePublishRecord
	err := d.DB(ctx).Where("idempotency_key = ?", key).First(&record).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &record, err
}

// 以下 *Tx 方法供审核发布流程在单个事务内组合使用：模板行与发布记录必须同生同死，
// 否则崩溃重试会产出第二条模板。

func (d *TemplateDAO) CreateTemplateTx(tx *gorm.DB, tpl *model.Template) (uint64, error) {
	if err := tx.Create(tpl).Error; err != nil {
		return 0, err
	}
	return tpl.ID, nil
}

func (d *TemplateDAO) CreatePublishRecordTx(tx *gorm.DB, record *model.TemplatePublishRecord) error {
	return tx.Create(record).Error
}

func (d *TemplateDAO) GetPublishRecordByKeyTx(tx *gorm.DB, key string) (*model.TemplatePublishRecord, error) {
	var record model.TemplatePublishRecord
	err := tx.Where("idempotency_key = ?", key).First(&record).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// TemplatePreview 是列表标注缩略图所需的最小列集合。
type TemplatePreview struct {
	ID           uint64 `gorm:"column:id"`
	PreviewURL   string `gorm:"column:preview_url"`
	ThumbnailURL string `gorm:"column:thumbnail_url"`
}

// ListThumbnailsByIDs 刻意不复用 GetByIDs：后者是 SELECT *，会把每一份 pattern_data
// 一起拉回来（理由见 templateListColumns 的注释）。草稿列表只需要这三列。
func (d *TemplateDAO) ListThumbnailsByIDs(ctx context.Context, ids []uint64) (map[uint64]TemplatePreview, error) {
	if len(ids) == 0 {
		return map[uint64]TemplatePreview{}, nil
	}
	var rows []TemplatePreview
	err := d.DB(ctx).Model(&model.Template{}).
		Select("id", "preview_url", "thumbnail_url").
		Where("id IN ? AND status IN ?", ids, visibleStatuses).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[uint64]TemplatePreview, len(rows))
	for _, row := range rows {
		result[row.ID] = row
	}
	return result, nil
}

// GetByIDForUpdateTx 取整行（含 pattern_data）用于覆盖前的快照，并持有行锁到事务结束。
// 状态前置：已下架的模板不是合法的覆盖目标；盲盒专属图纸是（入池后仍要能改）。
func (d *TemplateDAO) GetByIDForUpdateTx(tx *gorm.DB, id uint64) (*model.Template, error) {
	var tpl model.Template
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND status IN ?", id, visibleStatuses).First(&tpl).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &tpl, nil
}

// UpdatePublishedTemplateTx 镜像 UpdatePublishedTemplate，走调用方的事务。
//
// 必须用 map 形式的 Updates 且始终带上 updated_at：internal/db/mysql.go 拼的 DSN 没开
// clientFoundRows，MySQL 返回的是「实际变更行数」，所以值全同的更新会得到
// RowsAffected == 0，与「WHERE 没命中」无法区分。恒变的 updated_at 才让 RowsAffected
// 可信。不要加「无变化就跳过写入」的优化。
func (d *TemplateDAO) UpdatePublishedTemplateTx(tx *gorm.DB, templateID uint64, tpl *model.Template) (bool, error) {
	result := tx.Model(&model.Template{}).Where("id = ? AND status IN ?", templateID, visibleStatuses).
		Updates(map[string]interface{}{
			"category_id":   tpl.CategoryID,
			"title":         tpl.Title,
			"preview_url":   tpl.PreviewURL,
			"thumbnail_url": tpl.ThumbnailURL,
			"description":   tpl.Description,
			"pattern_data":  tpl.PatternData,
			"board_spec":    tpl.BoardSpec,
			"tags":          tpl.Tags,
			"difficulty":    tpl.Difficulty,
			"width":         tpl.Width,
			"height":        tpl.Height,
			"color_count":   tpl.ColorCount,
			"updated_at":    time.Now(),
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (d *TemplateDAO) CreateTemplateRevisionTx(tx *gorm.DB, revision *model.TemplateRevision) error {
	return tx.Create(revision).Error
}

// SetStatusTx 做条件状态迁移，供盲盒奖池的入池（1→2）和出池（2→1）使用。
// 带上 from 是为了不覆盖并发改动：出池时若图纸已被下架（status=0），这里不命中，
// 图纸就不会被意外重新上架。
func (d *TemplateDAO) SetStatusTx(tx *gorm.DB, id uint64, from, to int8) (bool, error) {
	result := tx.Model(&model.Template{}).Where("id = ? AND status = ?", id, from).
		Updates(map[string]interface{}{"status": to, "updated_at": time.Now()})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}
