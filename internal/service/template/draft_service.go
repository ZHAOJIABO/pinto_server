package template

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/service/media"
	"gorm.io/gorm"
)

// defaultMaxDrafts 兜底草稿箱上限。现有测试手搓 conf.GlobalConfig，不会设
// template_draft.max_count，没有兜底就等于零上限。
const defaultMaxDrafts = 200

// DraftService 管理管理后台的图纸草稿。它与 AdminService 同包，才能直接持有后者而
// 不产生循环依赖：发布草稿要复用 AdminService.PublishTemplateTx 的幂等写入。
type DraftService struct {
	drafts    *dao.TemplateDraftDAO
	templates *dao.TemplateDAO
	media     *media.Service
	admin     *AdminService
	maxDrafts int
}

func NewDraftService(
	drafts *dao.TemplateDraftDAO,
	templates *dao.TemplateDAO,
	mediaService *media.Service,
	admin *AdminService,
	maxDrafts int,
) *DraftService {
	if maxDrafts <= 0 {
		maxDrafts = defaultMaxDrafts
	}
	return &DraftService{
		drafts:    drafts,
		templates: templates,
		media:     mediaService,
		admin:     admin,
		maxDrafts: maxDrafts,
	}
}

// DraftFields 是已经通过结构校验的草稿载荷。除 PatternData 外全部允许为零值：草稿的
// 用途就是承载半成品，完整校验只在发布时刻执行。
type DraftFields struct {
	Title          string
	Description    string
	CategoryID     int
	Tags           string
	Difficulty     int8
	PreviewFileKey string
	PatternData    model.JSONMap
	BoardSpec      string
	Width          int
	Height         int
	BeadCount      int
	ColorCount     int
}

// DraftListItem 把草稿箱一行需要的派生显示字段与草稿本身放在一起。Draft 来自列投影，
// 不含 PatternData。
type DraftListItem struct {
	Draft        *model.TemplateDraft
	CategoryName string
	ThumbnailURL string
}

// CreateDraft 的步骤顺序是承重的，见每一步的注释。
func (s *DraftService) CreateDraft(
	ctx context.Context,
	actor string,
	idempotencyKey string,
	templateID *uint64,
	f DraftFields,
) (*model.TemplateDraft, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	// 必填。列上有唯一索引，允许空串的话第二份草稿会撞 uk_tpl_draft_idem 并被兜底
	// 分支变成 500；改成可空则 NULL 让唯一索引失效，丢掉防重复点击的唯一保护。
	if idempotencyKey == "" {
		return nil, apperr.InvalidArgument("idempotencyKey is required")
	}

	if existing, err := s.drafts.GetByIdempotencyKey(ctx, idempotencyKey); err != nil {
		return nil, apperr.Internal("get draft by idempotency key", err)
	} else if existing != nil {
		return existing, nil
	}

	if templateID != nil {
		if _, err := s.templates.GetByID(ctx, *templateID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, apperr.NotFound("template not found")
			}
			return nil, apperr.Internal("get template for draft", err)
		}
		// 一个模板同时最多挂一份草稿：重复创建返回已存在的那份而不是产生第二份。
		if existing, err := s.drafts.GetByTemplateID(ctx, *templateID); err != nil {
			return nil, apperr.Internal("get draft by template id", err)
		} else if existing != nil {
			return existing, nil
		}
	}

	// 上限校验必须排在上面两个「返回已存在」的分支之后，否则草稿箱一满，连「重新打开
	// 已存在的修订草稿」都做不到。也只在创建路径校验：若同时卡住 UpdateDraft，管理员
	// 在满箱状态下连「编辑现有草稿以便发布腾位」都做不了，直接死锁。
	// COUNT→INSERT 不原子，并发下可能略微超出，所以这是近似上限。
	count, err := s.drafts.Count(ctx)
	if err != nil {
		return nil, apperr.Internal("count template drafts", err)
	}
	if count >= int64(s.maxDrafts) {
		return nil, apperr.DraftLimitExceeded(s.maxDrafts)
	}

	previewURL, thumbnailURL, err := s.resolvePreview(ctx, f.PreviewFileKey)
	if err != nil {
		return nil, err
	}

	now := dao.NowMillis()
	draft := &model.TemplateDraft{
		TemplateID:     templateID,
		IdempotencyKey: idempotencyKey,
		Title:          f.Title,
		Description:    f.Description,
		CategoryID:     f.CategoryID,
		Tags:           f.Tags,
		Difficulty:     f.Difficulty,
		PreviewFileKey: f.PreviewFileKey,
		PreviewURL:     previewURL,
		ThumbnailURL:   thumbnailURL,
		PatternData:    f.PatternData,
		BoardSpec:      f.BoardSpec,
		Width:          f.Width,
		Height:         f.Height,
		BeadCount:      f.BeadCount,
		ColorCount:     f.ColorCount,
		UpdatedByActor: actor,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.drafts.Create(ctx, draft); err != nil {
		// 并发请求赢了某个唯一索引。它的 INSERT 阻塞了我们直到提交，所以赢家现在可读。
		if recovered := s.resolveCreateConflict(ctx, idempotencyKey, templateID); recovered != nil {
			return recovered, nil
		}
		return nil, apperr.Internal("create template draft", err)
	}
	return draft, nil
}

func (s *DraftService) resolveCreateConflict(ctx context.Context, idempotencyKey string, templateID *uint64) *model.TemplateDraft {
	if draft, err := s.drafts.GetByIdempotencyKey(ctx, idempotencyKey); err == nil && draft != nil {
		return draft
	}
	if templateID != nil {
		if draft, err := s.drafts.GetByTemplateID(ctx, *templateID); err == nil && draft != nil {
			return draft
		}
	}
	return nil
}

// UpdateDraft 用 baseUpdatedAt 做乐观锁，返回新的 updated_at 供前端更新本地基线，以及
// 草稿关联的 templateId 让 PUT 的响应形状与 POST 一致。
// 刻意不接受 templateId 入参：草稿与已发布模板的关联在创建时确定，改关联等于换一份草稿。
func (s *DraftService) UpdateDraft(
	ctx context.Context,
	actor string,
	draftID uint64,
	baseUpdatedAt time.Time,
	f DraftFields,
) (time.Time, *uint64, error) {
	current, err := s.drafts.GetByID(ctx, draftID)
	if err != nil {
		return time.Time{}, nil, apperr.Internal("get template draft", err)
	}
	if current == nil {
		return time.Time{}, nil, apperr.DraftNotFound()
	}

	// 不要每次自动保存都重算缩略图：media.AdminPreviewThumbnailURL 会无条件做一次
	// 对象存储 GET + 解码 + resize + PUT，没有缓存。只有 previewFileKey 真的变了才
	// 重新解析，常见的自动保存（previewFileKey 未变或为空）因此完全不碰外部服务。
	previewURL, thumbnailURL := current.PreviewURL, current.ThumbnailURL
	if f.PreviewFileKey != current.PreviewFileKey {
		previewURL, thumbnailURL, err = s.resolvePreview(ctx, f.PreviewFileKey)
		if err != nil {
			return time.Time{}, nil, err
		}
	}

	updatedAt, exists, err := s.drafts.UpdateWithLock(ctx, draftID, baseUpdatedAt, map[string]interface{}{
		"title":            f.Title,
		"description":      f.Description,
		"category_id":      f.CategoryID,
		"tags":             f.Tags,
		"difficulty":       f.Difficulty,
		"preview_file_key": f.PreviewFileKey,
		"preview_url":      previewURL,
		"thumbnail_url":    thumbnailURL,
		"pattern_data":     f.PatternData,
		"board_spec":       f.BoardSpec,
		"width":            f.Width,
		"height":           f.Height,
		"bead_count":       f.BeadCount,
		"color_count":      f.ColorCount,
		"updated_by_actor": actor,
	})
	if err != nil {
		return time.Time{}, nil, apperr.Internal("update template draft", err)
	}
	if updatedAt.IsZero() {
		if !exists {
			return time.Time{}, nil, apperr.DraftNotFound()
		}
		// 行还在但 updated_at 已经不是基线了：别人抢先写过。
		return time.Time{}, nil, apperr.DraftConflict(s.lastActor(ctx, draftID))
	}
	return updatedAt, current.TemplateID, nil
}

// PublishDraftInput 的 PreviewURL/ThumbnailURL 由调用方（handler）在进入本层之前解析
// 完毕。这是刻意的：解析要做对象存储的 GET + 解码 + resize + PUT，秒级网络 I/O 绝不能
// 发生在事务里。
type PublishDraftInput struct {
	Actor          string
	IdempotencyKey string
	BaseUpdatedAt  time.Time
	PreviewFileKey string
	PreviewURL     string
	ThumbnailURL   string
}

// PublishDraft 把草稿落成线上模板并删除草稿，返回模板 id。
//
// 两个管理员同时点发布会生成不同的 idempotencyKey，所以 bb_template_publish_record 的
// 唯一键挡不住他们：唯一的并发保护是事务里的行锁（GetByIDForUpdateTx）加末尾按
// updated_at 的 CAS 删除，幂等键检查不能替代它们。
func (s *DraftService) PublishDraft(ctx context.Context, draftID uint64, in PublishDraftInput) (uint64, error) {
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return 0, apperr.InvalidArgument("idempotencyKey is required")
	}

	// 事务外先读一次（不加锁）并校验分类，只为尽早给出可读的 4004。
	//
	// 草稿读不到时刻意不在这里报 4002：重放一个已成功的发布请求时草稿已经删掉了，唯一
	// 还能工作的检查是事务里第一步的发布记录。这里硬失败会让重试拿到 4002 而不是 200 +
	// 原 templateId。所以这一段是纯优化，判定权全部留给事务。
	//
	// 分类在这之后被停用的 TOCTOU 是接受的，templatesubmission.Approve 现在就是这么做的。
	draft, err := s.drafts.GetByID(ctx, draftID)
	if err != nil {
		return 0, apperr.Internal("get template draft", err)
	}
	if draft != nil {
		payload := publishPayloadFromDraft(draft, draftID, in)
		if err := s.admin.ValidatePublishPayload(payload); err != nil {
			return 0, apperr.DraftNotPublishable(err.Error())
		}
		if err := s.admin.ValidateActiveCategory(ctx, draft.CategoryID); err != nil {
			if errors.Is(err, ErrInvalidPayload) {
				return 0, apperr.DraftNotPublishable(err.Error())
			}
			return 0, apperr.Internal("validate draft category", err)
		}
	}

	var templateID uint64
	txErr := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// S1 必须是第一步：草稿行删掉之后，这是唯一还能工作的检查，正是它让重试的发布
		// 请求返回原来的 templateId 而不是莫名的 4002。
		record, err := s.templates.GetPublishRecordByKeyTx(tx, in.IdempotencyKey)
		if err != nil {
			return apperr.Internal("get publish record", err)
		}
		if record != nil {
			if record.DraftRevisionID != draftID {
				return apperr.DraftNotPublishable("idempotency key already used by another draft")
			}
			templateID = record.TemplateID
			return nil
		}

		locked, err := s.drafts.GetByIDForUpdateTx(tx, draftID)
		if err != nil {
			return apperr.Internal("lock template draft", err)
		}
		if locked == nil {
			return apperr.DraftNotFound()
		}
		// 在 Go 里比而不是在 SQL 的 WHERE 里：此刻已持有行，可以把抢先写入的管理员带进
		// 冲突消息。
		if !locked.UpdatedAt.Equal(in.BaseUpdatedAt) {
			return apperr.DraftConflict(locked.UpdatedByActor)
		}
		payload := publishPayloadFromDraft(locked, draftID, in)

		if locked.TemplateID == nil {
			// 新建分支：原样复用 PublishTemplateTx，它一并写模板与发布记录。没有快照，
			// 因为没有任何东西被覆盖。
			templateID, err = s.admin.PublishTemplateTx(tx, payload)
			if err != nil {
				return publishTxError(err)
			}
		} else {
			templateID = *locked.TemplateID
			// status = 1 前置。草稿创建之后模板被下架时必须返回 4004 而不是 4002：草稿
			// 明明就在箱里，报「草稿不存在」会让管理员无路可走。
			current, err := s.templates.GetByIDForUpdateTx(tx, templateID)
			if err != nil {
				return apperr.Internal("lock published template", err)
			}
			if current == nil {
				return apperr.DraftNotPublishable("linked template is no longer published")
			}
			// 这条分支绕过了 PublishTemplateTx，不显式校验就会静默跳过发布必填项检查。
			if err := s.admin.ValidatePublishPayload(payload); err != nil {
				return apperr.DraftNotPublishable(err.Error())
			}
			// 快照在更新之前、事务之内：与覆盖同生共死。
			if err := s.templates.CreateTemplateRevisionTx(tx, snapshotTemplate(current, draftID, in.Actor)); err != nil {
				return apperr.Internal("create template revision", err)
			}
			updated, err := s.templates.UpdatePublishedTemplateTx(tx, templateID, s.admin.buildTemplate(payload))
			if err != nil {
				return apperr.Internal("update published template", err)
			}
			if !updated {
				// 上面的行锁让这里不可达，但断言比静默成功好。
				return apperr.Internal("published template vanished mid-transaction", nil)
			}
			// 出错必须让整个事务失败：没有这条记录，崩溃重试会产出第二次覆盖。
			if err := s.templates.CreatePublishRecordTx(tx, &model.TemplatePublishRecord{
				IdempotencyKey:  in.IdempotencyKey,
				TemplateID:      templateID,
				DraftRevisionID: draftID,
				Status:          "published",
			}); err != nil {
				return apperr.Internal("create publish record", err)
			}
		}

		// 双保险 CAS：SQLite 方言会静默丢弃上面的 FOR UPDATE，这条 WHERE 让并发保护在
		// 那种环境下仍然成立。
		deleted, err := s.drafts.DeleteWithLockTx(tx, draftID, in.BaseUpdatedAt)
		if err != nil {
			return apperr.Internal("delete published draft", err)
		}
		if !deleted {
			return apperr.DraftConflict("")
		}
		return nil
	})
	if txErr != nil {
		return 0, txErr
	}
	return templateID, nil
}

// publishPayloadFromDraft 把草稿映射成发布载荷。DraftRevisionID 存草稿 id：草稿发布后
// 即删除，这个值只用来把重放的幂等键与「同一份草稿」对上。
func publishPayloadFromDraft(draft *model.TemplateDraft, draftID uint64, in PublishDraftInput) PublishPayload {
	return PublishPayload{
		IdempotencyKey:  in.IdempotencyKey,
		DraftRevisionID: draftID,
		UpdatePayload: UpdatePayload{
			Title:        draft.Title,
			Description:  draft.Description,
			CategoryID:   draft.CategoryID,
			Tags:         draft.Tags,
			Difficulty:   draft.Difficulty,
			BoardSpec:    draft.BoardSpec,
			PreviewURL:   in.PreviewURL,
			ThumbnailURL: in.ThumbnailURL,
			PatternData:  draft.PatternData,
			Width:        draft.Width,
			Height:       draft.Height,
			ColorCount:   draft.ColorCount,
			BeadCount:    draft.BeadCount,
		},
	}
}

func snapshotTemplate(current *model.Template, draftID uint64, actor string) *model.TemplateRevision {
	return &model.TemplateRevision{
		TemplateID:      current.ID,
		DraftID:         draftID,
		CategoryID:      current.CategoryID,
		Title:           current.Title,
		Description:     current.Description,
		PreviewURL:      current.PreviewURL,
		ThumbnailURL:    current.ThumbnailURL,
		PatternData:     current.PatternData,
		BoardSpec:       current.BoardSpec,
		Tags:            current.Tags,
		Difficulty:      current.Difficulty,
		Width:           current.Width,
		Height:          current.Height,
		ColorCount:      current.ColorCount,
		ReplacedByActor: actor,
		CreatedAt:       dao.NowMillis(),
	}
}

// publishTxError 把 AdminService 的 sentinel 错误翻成草稿流程的业务码。ErrInvalidPayload
// 走 4004（草稿在，但发不出去），ErrDuplicateKey 同理——幂等键被另一份草稿占了。
func publishTxError(err error) error {
	if errors.Is(err, ErrInvalidPayload) {
		return apperr.DraftNotPublishable(err.Error())
	}
	if errors.Is(err, ErrDuplicateKey) {
		return apperr.DraftNotPublishable("idempotency key already used by another draft")
	}
	return apperr.Internal("publish template from draft", err)
}

// lastActor 尽力取「最后由谁修改」放进冲突消息里。取不到就返回空串：报告冲突比报告
// 一个查询错误有用得多。
func (s *DraftService) lastActor(ctx context.Context, draftID uint64) string {
	if draft, err := s.drafts.GetByID(ctx, draftID); err == nil && draft != nil {
		return draft.UpdatedByActor
	}
	return ""
}

// ListDrafts 返回草稿箱，最近改的排最前。列表不带 pattern_data，缩略图尽力而为。
func (s *DraftService) ListDrafts(ctx context.Context, page, pageSize int) ([]DraftListItem, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 50
	}
	drafts, total, err := s.drafts.List(ctx, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, 0, apperr.Internal("list template drafts", err)
	}

	linkedTemplateIDs := make([]uint64, 0, len(drafts))
	categoryIDs := make([]int, 0, len(drafts))
	seenTemplateIDs := make(map[uint64]struct{}, len(drafts))
	seenCategoryIDs := make(map[int]struct{}, len(drafts))
	for _, draft := range drafts {
		if draft.TemplateID != nil {
			if _, seen := seenTemplateIDs[*draft.TemplateID]; !seen {
				seenTemplateIDs[*draft.TemplateID] = struct{}{}
				linkedTemplateIDs = append(linkedTemplateIDs, *draft.TemplateID)
			}
		}
		// 0 表示还没选分类，查它只会白跑一次 IN。
		if draft.CategoryID > 0 {
			if _, seen := seenCategoryIDs[draft.CategoryID]; !seen {
				seenCategoryIDs[draft.CategoryID] = struct{}{}
				categoryIDs = append(categoryIDs, draft.CategoryID)
			}
		}
	}

	previews, err := s.templates.ListThumbnailsByIDs(ctx, linkedTemplateIDs)
	if err != nil {
		return nil, 0, apperr.Internal("list linked template thumbnails", err)
	}
	categoryNames, err := s.templates.ListActiveCategoryNames(ctx, categoryIDs)
	if err != nil {
		return nil, 0, apperr.Internal("list draft category names", err)
	}

	items := make([]DraftListItem, 0, len(drafts))
	for _, draft := range drafts {
		items = append(items, DraftListItem{
			Draft:        draft,
			CategoryName: categoryNames[draft.CategoryID],
			ThumbnailURL: draftThumbnailURL(draft, previews),
		})
	}
	return items, total, nil
}

// draftThumbnailURL 三档取值。修订草稿借用线上模板的缩略图：视觉上可能滞后于草稿里
// 的改动，但不必为每次自动保存重新生成一张 PNG。都取不到就返回空串，前端用 §5 详情
// 里的 patternData 本地渲染兜底。
func draftThumbnailURL(draft *model.TemplateDraft, previews map[uint64]dao.TemplatePreview) string {
	if draft.TemplateID != nil {
		if preview, ok := previews[*draft.TemplateID]; ok {
			if preview.ThumbnailURL != "" {
				return preview.ThumbnailURL
			}
			return preview.PreviewURL
		}
	}
	if draft.PreviewFileKey != "" {
		if draft.ThumbnailURL != "" {
			return draft.ThumbnailURL
		}
		return draft.PreviewURL
	}
	return ""
}

func (s *DraftService) GetDraft(ctx context.Context, draftID uint64) (*model.TemplateDraft, error) {
	draft, err := s.drafts.GetByID(ctx, draftID)
	if err != nil {
		return nil, apperr.Internal("get template draft", err)
	}
	if draft == nil {
		return nil, apperr.DraftNotFound()
	}
	return draft, nil
}

// DeleteDraft 是幂等的：删除不存在的草稿也算成功。丢弃草稿不影响它关联的已发布模板。
func (s *DraftService) DeleteDraft(ctx context.Context, draftID uint64) error {
	if _, err := s.drafts.DeleteByID(ctx, draftID); err != nil {
		return apperr.Internal("delete template draft", err)
	}
	return nil
}

func (s *DraftService) MapDraftIDsByTemplateIDs(ctx context.Context, templateIDs []uint64) (map[uint64]uint64, error) {
	result, err := s.drafts.MapDraftIDsByTemplateIDs(ctx, templateIDs)
	if err != nil {
		return nil, apperr.Internal("map draft ids by template ids", err)
	}
	return result, nil
}

// resolvePreview 把 previewFileKey 换成可存储的 URL 对。空 key 合法：草稿期间可能
// 一直没有缩略图，发布时才要求上传。
func (s *DraftService) resolvePreview(ctx context.Context, fileKey string) (string, string, error) {
	if fileKey == "" {
		return "", "", nil
	}
	previewURL, err := s.media.GetUploadedAdminPreviewURL(ctx, fileKey)
	if err != nil {
		return "", "", err
	}
	// AdminPreviewThumbnailURL 失败时返回空串，前端会退化到完整预览图。
	return previewURL, s.media.AdminPreviewThumbnailURL(ctx, fileKey), nil
}
