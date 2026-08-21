package template

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var (
	ErrInvalidPayload      = errors.New("invalid publish payload")
	ErrDuplicateKey        = errors.New("idempotency key conflict")
	ErrTemplateNotFound    = errors.New("template not found")
	ErrUnpublishReason     = errors.New("unpublish reason must not exceed 200 characters")
	ErrCategoryNameInvalid = errors.New("category name must contain 1 to 30 characters")
	ErrCategoryNameTaken   = errors.New("category name already exists")
	urlPattern             = regexp.MustCompile(`^https?://`)
)

const (
	maxUnpublishReasonRunes      = 200
	maxTemplateCategoryNameRunes = 30
)

type AdminService struct {
	dao  *dao.TemplateDAO
	pool *dao.BlindBoxPoolDAO
}

func NewAdminService(templateDAO *dao.TemplateDAO, poolDAO *dao.BlindBoxPoolDAO) *AdminService {
	return &AdminService{dao: templateDAO, pool: poolDAO}
}

type UpdatePayload struct {
	Title        string
	Description  string
	CategoryID   int
	Tags         string
	Difficulty   int8
	BoardSpec    string
	PreviewURL   string
	ThumbnailURL string
	PatternData  model.JSONMap
	Width        int
	Height       int
	ColorCount   int
	BeadCount    int
}

type PublishPayload struct {
	IdempotencyKey  string
	DraftRevisionID uint64
	// 投稿人署名。刻意放在 PublishPayload 而非 UpdatePayload：后者服务于
	// PUT /admin/templates/{id}，不给它这两个字段就不可能在编辑时误清空署名。
	ContributorUserID   uint64
	ContributorNickname string
	UpdatePayload
}

func (s *AdminService) PublishTemplate(ctx context.Context, payload PublishPayload) (uint64, error) {
	if err := s.validatePayload(payload); err != nil {
		return 0, fmt.Errorf("%w: %s", ErrInvalidPayload, err.Error())
	}

	// Check idempotency: if key exists, return original result
	existing, err := s.dao.GetPublishRecordByKey(ctx, payload.IdempotencyKey)
	if err != nil {
		return 0, err
	}
	if existing != nil {
		if existing.DraftRevisionID != payload.DraftRevisionID {
			return 0, ErrDuplicateKey
		}
		return existing.TemplateID, nil
	}

	templateID, err := s.dao.CreateOrUpdateTemplate(ctx, s.buildTemplate(payload))
	if err != nil {
		return 0, err
	}

	// Record idempotency
	record := &model.TemplatePublishRecord{
		IdempotencyKey:  payload.IdempotencyKey,
		TemplateID:      templateID,
		DraftRevisionID: payload.DraftRevisionID,
		Status:          "published",
	}
	if err := s.dao.CreatePublishRecord(ctx, record); err != nil {
		zap.L().Error("failed to create publish record", zap.Error(err))
	}

	return templateID, nil
}

// PublishTemplateTx is the review-flow counterpart of PublishTemplate. It writes
// through the caller's transaction so the template row and the submission status
// commit together, and unlike PublishTemplate it fails the whole publish when the
// idempotency record cannot be written: in the review flow that record is the only
// thing preventing a crash-retry from creating a second template.
func (s *AdminService) PublishTemplateTx(tx *gorm.DB, payload PublishPayload) (uint64, error) {
	if err := s.validatePayload(payload); err != nil {
		return 0, fmt.Errorf("%w: %s", ErrInvalidPayload, err.Error())
	}

	existing, err := s.dao.GetPublishRecordByKeyTx(tx, payload.IdempotencyKey)
	if err != nil {
		return 0, err
	}
	if existing != nil {
		if existing.DraftRevisionID != payload.DraftRevisionID {
			return 0, ErrDuplicateKey
		}
		return existing.TemplateID, nil
	}

	templateID, err := s.dao.CreateTemplateTx(tx, s.buildTemplate(payload))
	if err != nil {
		return 0, err
	}
	if err := s.dao.CreatePublishRecordTx(tx, &model.TemplatePublishRecord{
		IdempotencyKey:  payload.IdempotencyKey,
		TemplateID:      templateID,
		DraftRevisionID: payload.DraftRevisionID,
		Status:          "published",
	}); err != nil {
		return 0, err
	}
	return templateID, nil
}

// ValidateActiveCategory closes a gap in validateTemplateFields, which only checks
// category_id > 0 without touching the database.
func (s *AdminService) ValidateActiveCategory(ctx context.Context, categoryID int) error {
	if _, err := s.dao.GetActiveCategoryByID(ctx, categoryID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: category_id must reference an active category", ErrInvalidPayload)
		}
		return err
	}
	return nil
}

func (s *AdminService) buildTemplate(payload PublishPayload) *model.Template {
	return &model.Template{
		CategoryID:          payload.CategoryID,
		Title:               payload.Title,
		PreviewURL:          payload.PreviewURL,
		ThumbnailURL:        payload.ThumbnailURL,
		Description:         payload.Description,
		PatternData:         payload.PatternData,
		BoardSpec:           payload.BoardSpec,
		Tags:                payload.Tags,
		Difficulty:          payload.Difficulty,
		Width:               payload.Width,
		Height:              payload.Height,
		ColorCount:          payload.ColorCount,
		ContributorUserID:   payload.ContributorUserID,
		ContributorNickname: payload.ContributorNickname,
		IsFree:              true,
		Status:              1, // active
	}
}

func (s *AdminService) UnpublishTemplate(ctx context.Context, templateID uint64, reason string) error {
	if templateID == 0 {
		return ErrTemplateNotFound
	}
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > maxUnpublishReasonRunes {
		return ErrUnpublishReason
	}

	// 在奖池里的图纸不能直接下架：下架会把 status 改成 0，之后再从奖池移除时
	// SetStatusTx(from=2) 不命中，图纸会永久卡在 status=0 且奖池条目已消失的状态。
	// 强制先移出奖池，运营动作的顺序就唯一了。
	item, err := s.pool.GetByTemplateID(ctx, templateID)
	if err != nil {
		return err
	}
	if item != nil {
		return apperr.InvalidArgument("图纸在盲盒奖池中，请先将其移出奖池再下架")
	}

	found, err := s.dao.UnpublishTemplate(ctx, templateID)
	if err != nil {
		return err
	}
	if !found {
		return ErrTemplateNotFound
	}

	zap.L().Info("template unpublished",
		zap.Uint64("template_id", templateID),
		zap.String("reason", reason))
	return nil
}

func (s *AdminService) CreateCategory(ctx context.Context, name string, isBlindBox bool) (*model.TemplateCategory, error) {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) == 0 || utf8.RuneCountInString(name) > maxTemplateCategoryNameRunes {
		return nil, ErrCategoryNameInvalid
	}

	existing, err := s.dao.GetCategoryByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrCategoryNameTaken
	}

	category := &model.TemplateCategory{Name: name, Status: 1, IsBlindBox: isBlindBox}
	if err := s.dao.CreateCategory(ctx, category); err != nil {
		duplicate, lookupErr := s.dao.GetCategoryByName(ctx, name)
		if lookupErr == nil && duplicate != nil {
			return nil, ErrCategoryNameTaken
		}
		return nil, err
	}
	return category, nil
}

func (s *AdminService) GetPublishedTemplate(ctx context.Context, templateID uint64) (*model.Template, error) {
	if templateID == 0 {
		return nil, ErrTemplateNotFound
	}
	tpl, err := s.dao.GetByID(ctx, templateID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTemplateNotFound
		}
		return nil, err
	}
	return tpl, nil
}

func (s *AdminService) UpdateTemplate(ctx context.Context, templateID uint64, payload UpdatePayload) error {
	if templateID == 0 {
		return ErrTemplateNotFound
	}
	if err := s.validateUpdatePayload(payload); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidPayload, err.Error())
	}
	if _, err := s.dao.GetActiveCategoryByID(ctx, payload.CategoryID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: category_id must reference an active category", ErrInvalidPayload)
		}
		return err
	}

	updated, err := s.dao.UpdatePublishedTemplate(ctx, templateID, &model.Template{
		CategoryID:   payload.CategoryID,
		Title:        payload.Title,
		PreviewURL:   payload.PreviewURL,
		ThumbnailURL: payload.ThumbnailURL,
		Description:  payload.Description,
		PatternData:  payload.PatternData,
		BoardSpec:    payload.BoardSpec,
		Tags:         payload.Tags,
		Difficulty:   payload.Difficulty,
		Width:        payload.Width,
		Height:       payload.Height,
		ColorCount:   payload.ColorCount,
	})
	if err != nil {
		return err
	}
	if !updated {
		return ErrTemplateNotFound
	}
	return nil
}

func (s *AdminService) GetPublishStatus(ctx context.Context, idempotencyKey string) (*model.TemplatePublishRecord, error) {
	return s.dao.GetPublishRecordByKey(ctx, idempotencyKey)
}

// 盲盒奖池管理。入池与出池都要同时改两张表（奖池条目 + bb_template.status），
// 所以一律在事务里做：只写一半会留下"图纸仅盲盒可见但不在任何奖池里"的孤儿，
// 那种图纸从 C 端消失且再也抽不到，只能手工改库。
const (
	minPoolWeight = 1
	maxPoolWeight = 10000
)

// AddToPool 把图纸加入奖池，并把它切成"仅盲盒可见"（status 1 → 2）。
//
// weight 下界是 1 而不是 0：weight=0 会让条目在 ListActiveCandidates 里被 SQL 过滤掉，
// 看起来"在池中却永不出现"。要停用条目应该用 status=0 表达，语义明确。
func (s *AdminService) AddToPool(ctx context.Context, templateID uint64, weight, sortOrder int) (*model.BlindBoxPoolItem, error) {
	if templateID == 0 {
		return nil, ErrTemplateNotFound
	}
	if weight < minPoolWeight || weight > maxPoolWeight {
		return nil, apperr.InvalidArgument(fmt.Sprintf("weight 必须在 %d 到 %d 之间", minPoolWeight, maxPoolWeight))
	}

	item := &model.BlindBoxPoolItem{TemplateID: templateID, Weight: weight, SortOrder: sortOrder, Status: 1}
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 行锁先拿：否则并发的下架请求可能在判重之后、切 status 之前把图纸改成 0。
		tpl, err := s.dao.GetByIDForUpdateTx(tx, templateID)
		if err != nil {
			return err
		}
		if tpl == nil {
			return ErrTemplateNotFound
		}

		existing, err := s.pool.GetByTemplateIDTx(tx, templateID)
		if err != nil {
			return err
		}
		if existing != nil {
			return apperr.InvalidArgument("该图纸已在盲盒奖池中")
		}

		if err := s.pool.CreateTx(tx, item); err != nil {
			return err
		}
		// status 已经是 2 说明是脏数据（条目被手工删过），此时不命中也不算失败。
		if tpl.Status == dao.StatusPublished {
			if _, err := s.dao.SetStatusTx(tx, templateID, dao.StatusPublished, dao.StatusBlindBoxOnly); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return item, nil
}

// PoolItemPatch 用指针表达"没传就不改"，避免把未提交的字段清零。
type PoolItemPatch struct {
	Weight    *int
	SortOrder *int
	Status    *int8
}

// UpdatePoolItem 只动奖池条目自身，不碰 bb_template.status：条目停用不等于图纸重新上架。
func (s *AdminService) UpdatePoolItem(ctx context.Context, itemID uint64, patch PoolItemPatch) error {
	if itemID == 0 {
		return apperr.NotFound("blind box pool item not found")
	}

	fields := make(map[string]interface{}, 3)
	if patch.Weight != nil {
		if *patch.Weight < minPoolWeight || *patch.Weight > maxPoolWeight {
			return apperr.InvalidArgument(fmt.Sprintf("weight 必须在 %d 到 %d 之间", minPoolWeight, maxPoolWeight))
		}
		fields["weight"] = *patch.Weight
	}
	if patch.SortOrder != nil {
		fields["sort_order"] = *patch.SortOrder
	}
	if patch.Status != nil {
		if *patch.Status != 0 && *patch.Status != 1 {
			return apperr.InvalidArgument("status 只能是 0 或 1")
		}
		fields["status"] = *patch.Status
	}
	if len(fields) == 0 {
		return apperr.InvalidArgument("没有需要修改的字段")
	}

	item, err := s.pool.GetByID(ctx, itemID)
	if err != nil {
		return err
	}
	if item == nil {
		return apperr.NotFound("blind box pool item not found")
	}
	if _, err := s.pool.Update(ctx, itemID, fields); err != nil {
		return err
	}
	return nil
}

// RemoveFromPool 移出奖池并把图纸放回普通上架（status 2 → 1）。
func (s *AdminService) RemoveFromPool(ctx context.Context, itemID uint64) error {
	if itemID == 0 {
		return apperr.NotFound("blind box pool item not found")
	}

	return db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := s.pool.GetByIDTx(tx, itemID)
		if err != nil {
			return err
		}
		if item == nil {
			return apperr.NotFound("blind box pool item not found")
		}
		if err := s.pool.DeleteTx(tx, itemID); err != nil {
			return err
		}
		// from=2 是关键：图纸若已经是 status=0，这里不命中，移出条目不会把它重新上架。
		if _, err := s.dao.SetStatusTx(tx, item.TemplateID, dao.StatusBlindBoxOnly, dao.StatusPublished); err != nil {
			return err
		}
		return nil
	})
}

// PoolEntry 是后台奖池列表的一行：条目字段 + 图纸摘要。
type PoolEntry struct {
	Item         *model.BlindBoxPoolItem
	Title        string
	PreviewURL   string
	ThumbnailURL string
	CategoryID   int
	CategoryName string
}

func (s *AdminService) ListPool(ctx context.Context, page, pageSize int) ([]PoolEntry, int64, error) {
	offset := (page - 1) * pageSize
	items, total, err := s.pool.List(ctx, offset, pageSize)
	if err != nil {
		return nil, 0, err
	}
	if len(items) == 0 {
		return []PoolEntry{}, total, nil
	}

	templateIDs := make([]uint64, 0, len(items))
	for _, item := range items {
		templateIDs = append(templateIDs, item.TemplateID)
	}
	briefs, err := s.dao.ListBriefsByIDs(ctx, templateIDs)
	if err != nil {
		return nil, 0, err
	}

	categoryIDs := make([]int, 0, len(briefs))
	seen := make(map[int]struct{}, len(briefs))
	for _, brief := range briefs {
		if _, ok := seen[brief.CategoryID]; ok {
			continue
		}
		seen[brief.CategoryID] = struct{}{}
		categoryIDs = append(categoryIDs, brief.CategoryID)
	}
	categoryNames, err := s.dao.ListActiveCategoryNames(ctx, categoryIDs)
	if err != nil {
		return nil, 0, err
	}

	// 图纸被硬删时 briefs 会缺行，此处仍然返回条目（标题留空），让运营看得见并能清理，
	// 而不是让条目在列表里凭空消失。
	entries := make([]PoolEntry, 0, len(items))
	for _, item := range items {
		entry := PoolEntry{Item: item}
		if brief, ok := briefs[item.TemplateID]; ok {
			entry.Title = brief.Title
			entry.PreviewURL = brief.PreviewURL
			entry.ThumbnailURL = brief.ThumbnailURL
			entry.CategoryID = brief.CategoryID
			entry.CategoryName = categoryNames[brief.CategoryID]
		}
		entries = append(entries, entry)
	}
	return entries, total, nil
}

// ValidatePublishPayload 是 validatePayload 的导出封装，供发布修订草稿那条分支使用：
// 它绕过 PublishTemplateTx（因为要覆盖而不是新建），不显式调用就会静默跳过完整校验。
func (s *AdminService) ValidatePublishPayload(p PublishPayload) error {
	if err := s.validatePayload(p); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidPayload, err.Error())
	}
	return nil
}

func (s *AdminService) validatePayload(p PublishPayload) error {
	if p.IdempotencyKey == "" {
		return fmt.Errorf("idempotency_key required")
	}
	return s.validateTemplateFields(p.Title, p.CategoryID, p.Width, p.Height, p.PatternData, p.PreviewURL, p.ThumbnailURL)
}

func (s *AdminService) validateUpdatePayload(p UpdatePayload) error {
	return s.validateTemplateFields(p.Title, p.CategoryID, p.Width, p.Height, p.PatternData, p.PreviewURL, p.ThumbnailURL)
}

func (s *AdminService) validateTemplateFields(title string, categoryID, width, height int, patternData model.JSONMap, previewURL, thumbnailURL string) error {
	if title == "" {
		return fmt.Errorf("title required")
	}
	if categoryID <= 0 {
		return fmt.Errorf("category_id must be positive")
	}
	if width <= 0 || height <= 0 {
		return fmt.Errorf("width and height must be positive")
	}
	if patternData == nil {
		return fmt.Errorf("pattern_data required")
	}
	if previewURL != "" && !urlPattern.MatchString(previewURL) {
		return fmt.Errorf("invalid preview_url")
	}
	if thumbnailURL != "" && !urlPattern.MatchString(thumbnailURL) {
		return fmt.Errorf("invalid thumbnail_url")
	}
	return nil
}
