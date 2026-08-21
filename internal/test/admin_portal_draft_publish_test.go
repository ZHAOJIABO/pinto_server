package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zhaojiabo/bobobeads_server/internal/api"
	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	templateservice "github.com/zhaojiabo/bobobeads_server/internal/service/template"
	"google.golang.org/protobuf/encoding/protojson"
)

// seedPreviewKey 造一份「已上传的管理端预览图」。发布路径要求它存在（§6），所以每个发布
// 用例都得先过这一步。
func (p *draftPortal) seedPreviewKey(t *testing.T, name string) string {
	t.Helper()
	fileKey := "admin_preview/2026/08/19/0/" + name + ".png"
	if err := db.DB.Create(&model.MediaAsset{
		FileKey:     fileKey,
		Purpose:     "admin_preview",
		ContentType: "image/png",
		Status:      model.MediaStatusUploaded,
	}).Error; err != nil {
		t.Fatalf("create preview asset: %v", err)
	}
	p.storage.put(fileKey, "image/png", pngBytes(t, 400, 400))
	return fileKey
}

// createPublishableDraft 建一份已补全发布必填项的草稿。templateID 非 nil 时是修订草稿。
func (p *draftPortal) createPublishableDraft(t *testing.T, key string, templateID *uint64) (draftStamp, string) {
	t.Helper()
	previewKey := p.seedPreviewKey(t, key)
	body := map[string]interface{}{
		"idempotencyKey": key,
		"title":          "小猫",
		"description":    "四格小猫",
		"categoryId":     p.category.ID,
		"tags":           "猫,动物",
		"difficulty":     2,
		"previewFileKey": previewKey,
		"patternData":    draftPatternJSON(t, 4, 4),
	}
	if templateID != nil {
		body["templateId"] = fmt.Sprintf("%d", *templateID)
	}
	response := p.do(t, http.MethodPost, "/api/v1/admin/template-drafts", body)
	if response.Code != http.StatusOK {
		t.Fatalf("expected publishable draft create to return 200, got %d: %s", response.Code, response.Body.String())
	}
	return decodeDraftStamp(t, response), previewKey
}

func (p *draftPortal) publishDraft(t *testing.T, draftID, key, previewKey, baseUpdatedAt string) *httptest.ResponseRecorder {
	t.Helper()
	return p.do(t, http.MethodPost, "/api/v1/admin/template-drafts/"+draftID+"/publish", map[string]interface{}{
		"idempotencyKey": key,
		"previewFileKey": previewKey,
		"baseUpdatedAt":  baseUpdatedAt,
	})
}

func decodePublishedTemplateID(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		TemplateID string `json:"templateId"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode publish response: %v (%s)", err, response.Body.String())
	}
	if body.TemplateID == "" || body.TemplateID == "0" {
		t.Fatalf("expected a templateId in the publish response, got %q", response.Body.String())
	}
	return body.TemplateID
}

func countRows(t *testing.T, dest interface{}) int64 {
	t.Helper()
	var count int64
	if err := db.DB.Model(dest).Count(&count).Error; err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}

func patternJSON(t *testing.T, pattern model.JSONMap) string {
	t.Helper()
	encoded, err := json.Marshal(pattern)
	if err != nil {
		t.Fatalf("marshal pattern data: %v", err)
	}
	return string(encoded)
}

// seedLiveTemplate 造一个可被修订草稿覆盖的线上模板。PatternData 刻意可辨认，好让快照
// 断言能证明存下来的是「覆盖前」的值。
func (p *draftPortal) seedLiveTemplate(t *testing.T) *model.Template {
	t.Helper()
	live := &model.Template{
		CategoryID:   p.category.ID,
		Title:        "旧标题",
		Description:  "旧描述",
		PreviewURL:   "https://cdn.example.test/old-preview.png",
		ThumbnailURL: "https://cdn.example.test/old-thumb.png",
		PatternData:  model.JSONMap{"boardSpec": "2x2", "marker": "before-overwrite"},
		BoardSpec:    "2x2",
		Tags:         "旧",
		Width:        2,
		Height:       2,
		ColorCount:   1,
		IsFree:       true,
		Status:       1,
	}
	if err := db.DB.Create(live).Error; err != nil {
		t.Fatalf("create live template: %v", err)
	}
	return live
}

func TestAdminPortalDraftPublishRequiresToken(t *testing.T) {
	portal := newDraftPortal(t, 0)
	response := portal.request(t, http.MethodPost, "/api/v1/admin/template-drafts/1/publish", map[string]interface{}{}, false)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", response.Code)
	}
}

// 新建分支：产出模板、草稿消失、恰好一条发布记录、零条 revision（没有东西被覆盖）。
func TestAdminPortalDraftPublishCreatesTemplate(t *testing.T) {
	portal := newDraftPortal(t, 0)
	stamp, previewKey := portal.createPublishableDraft(t, "draft-publish-new", nil)

	response := portal.publishDraft(t, stamp.Draft.DraftID, "publish-new-001", previewKey, stamp.Draft.UpdatedAt)
	if response.Code != http.StatusOK {
		t.Fatalf("expected publish to return 200, got %d: %s", response.Code, response.Body.String())
	}
	templateID := decodePublishedTemplateID(t, response)

	var tpl model.Template
	if err := db.DB.First(&tpl).Error; err != nil {
		t.Fatalf("find published template: %v", err)
	}
	if fmt.Sprintf("%d", tpl.ID) != templateID {
		t.Fatalf("response templateId %q does not match the created row %d", templateID, tpl.ID)
	}
	if tpl.Title != "小猫" || tpl.CategoryID != portal.category.ID || tpl.Status != 1 {
		t.Fatalf("published template did not take the draft's fields: %#v", tpl)
	}
	if tpl.PreviewURL == "" || tpl.ThumbnailURL == "" {
		t.Fatalf("published template must carry the freshly resolved preview URLs: %#v", tpl)
	}

	if count := countRows(t, &model.TemplateDraft{}); count != 0 {
		t.Fatalf("published draft must be deleted, %d rows remain", count)
	}
	if count := countRows(t, &model.TemplatePublishRecord{}); count != 1 {
		t.Fatalf("expected exactly one publish record, got %d", count)
	}
	// 新建分支没有东西被覆盖，写快照就是纯噪音。
	if count := countRows(t, &model.TemplateRevision{}); count != 0 {
		t.Fatalf("creating a template must not write a revision snapshot, got %d", count)
	}
}

// 覆盖分支：恰好一条 revision，其 pattern_data 等于覆盖前的值。
func TestAdminPortalDraftPublishOverwritesTemplate(t *testing.T) {
	portal := newDraftPortal(t, 0)
	live := portal.seedLiveTemplate(t)
	before := patternJSON(t, live.PatternData)
	stamp, previewKey := portal.createPublishableDraft(t, "draft-publish-overwrite", &live.ID)

	response := portal.publishDraft(t, stamp.Draft.DraftID, "publish-overwrite-001", previewKey, stamp.Draft.UpdatedAt)
	if response.Code != http.StatusOK {
		t.Fatalf("expected publish to return 200, got %d: %s", response.Code, response.Body.String())
	}
	if templateID := decodePublishedTemplateID(t, response); templateID != fmt.Sprintf("%d", live.ID) {
		t.Fatalf("revision draft must republish the linked template %d, got %s", live.ID, templateID)
	}

	if count := countRows(t, &model.Template{}); count != 1 {
		t.Fatalf("overwriting must not create a second template, got %d rows", count)
	}
	var updated model.Template
	if err := db.DB.First(&updated, live.ID).Error; err != nil {
		t.Fatalf("reload template: %v", err)
	}
	if updated.Title != "小猫" || updated.BoardSpec != "4x4" || updated.Width != 4 {
		t.Fatalf("template was not overwritten with the draft: %#v", updated)
	}
	if patternJSON(t, updated.PatternData) == before {
		t.Fatal("template pattern_data must be replaced by the draft's")
	}

	var revisions []model.TemplateRevision
	if err := db.DB.Find(&revisions).Error; err != nil {
		t.Fatalf("list revisions: %v", err)
	}
	if len(revisions) != 1 {
		t.Fatalf("expected exactly one revision snapshot, got %d", len(revisions))
	}
	if got := patternJSON(t, revisions[0].PatternData); got != before {
		t.Fatalf("revision must snapshot the pre-overwrite pattern_data\n want %s\n got  %s", before, got)
	}
	if revisions[0].Title != "旧标题" || revisions[0].TemplateID != live.ID {
		t.Fatalf("revision must mirror the pre-overwrite row: %#v", revisions[0])
	}
	if revisions[0].ReplacedByActor != "operator" {
		t.Fatalf("revision must record the acting admin, got %q", revisions[0].ReplacedByActor)
	}
}

// 重放：草稿行已经删掉了，唯一还能工作的检查是发布记录（事务的 S1）。它必须让重试返回
// 200 + 原 templateId，而不是莫名的 4002。
func TestAdminPortalDraftPublishReplaysIdempotencyKey(t *testing.T) {
	portal := newDraftPortal(t, 0)
	live := portal.seedLiveTemplate(t)
	stamp, previewKey := portal.createPublishableDraft(t, "draft-publish-replay", &live.ID)

	first := portal.publishDraft(t, stamp.Draft.DraftID, "publish-replay-001", previewKey, stamp.Draft.UpdatedAt)
	if first.Code != http.StatusOK {
		t.Fatalf("first publish: expected 200, got %d: %s", first.Code, first.Body.String())
	}
	second := portal.publishDraft(t, stamp.Draft.DraftID, "publish-replay-001", previewKey, stamp.Draft.UpdatedAt)
	if second.Code != http.StatusOK {
		t.Fatalf("replayed publish: expected 200, got %d: %s", second.Code, second.Body.String())
	}
	if a, b := decodePublishedTemplateID(t, first), decodePublishedTemplateID(t, second); a != b {
		t.Fatalf("replay must return the same templateId, got %s then %s", a, b)
	}

	if count := countRows(t, &model.Template{}); count != 1 {
		t.Fatalf("replay must not create a second template, got %d", count)
	}
	if count := countRows(t, &model.TemplatePublishRecord{}); count != 1 {
		t.Fatalf("replay must not create a second publish record, got %d", count)
	}
	if count := countRows(t, &model.TemplateRevision{}); count != 1 {
		t.Fatalf("replay must not write a second revision, got %d", count)
	}
}

func TestAdminPortalDraftPublishRejectsStaleBaseUpdatedAt(t *testing.T) {
	portal := newDraftPortal(t, 0)
	live := portal.seedLiveTemplate(t)
	beforePattern := patternJSON(t, live.PatternData)
	stamp, previewKey := portal.createPublishableDraft(t, "draft-publish-stale", &live.ID)

	// 有人先改了一笔，前端手里的 baseUpdatedAt 于是过期。
	updated := portal.do(t, http.MethodPut, "/api/v1/admin/template-drafts/"+stamp.Draft.DraftID, map[string]interface{}{
		"title":          "小猫改",
		"categoryId":     portal.category.ID,
		"previewFileKey": previewKey,
		"patternData":    draftPatternJSON(t, 4, 4),
		"baseUpdatedAt":  stamp.Draft.UpdatedAt,
	})
	if updated.Code != http.StatusOK {
		t.Fatalf("intervening update: expected 200, got %d: %s", updated.Code, updated.Body.String())
	}

	response := portal.publishDraft(t, stamp.Draft.DraftID, "publish-stale-001", previewKey, stamp.Draft.UpdatedAt)
	if response.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a stale baseUpdatedAt, got %d: %s", response.Code, response.Body.String())
	}
	if code := responseCode(t, response); code != apperr.CodeDraftConflict {
		t.Fatalf("expected code %d, got %d", apperr.CodeDraftConflict, code)
	}

	// 冲突必须让整个事务回滚：草稿完好，线上模板一个字节都没动。
	var draft model.TemplateDraft
	if err := db.DB.First(&draft).Error; err != nil {
		t.Fatalf("draft must survive a publish conflict: %v", err)
	}
	if draft.Title != "小猫改" {
		t.Fatalf("draft content must be untouched, got %q", draft.Title)
	}
	var reloaded model.Template
	if err := db.DB.First(&reloaded, live.ID).Error; err != nil {
		t.Fatalf("reload template: %v", err)
	}
	if reloaded.Title != "旧标题" || patternJSON(t, reloaded.PatternData) != beforePattern {
		t.Fatalf("conflicted publish must not touch the live template: %#v", reloaded)
	}
	if count := countRows(t, &model.TemplateRevision{}); count != 0 {
		t.Fatalf("conflicted publish must not leave a revision, got %d", count)
	}
}

// 发布必填项没补全 → 4004。草稿必须完好，前端把用户挡回编辑页补全。
func TestAdminPortalDraftPublishRequiresCompleteFields(t *testing.T) {
	cases := []struct {
		name  string
		title string
		// 0 表示还没选分类。
		categoryID int
	}{
		{name: "MissingTitle", title: "", categoryID: -1},
		{name: "MissingCategory", title: "小猫", categoryID: 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			portal := newDraftPortal(t, 0)
			previewKey := portal.seedPreviewKey(t, "incomplete")
			categoryID := testCase.categoryID
			if categoryID == -1 {
				categoryID = portal.category.ID
			}
			stamp := decodeDraftStamp(t, portal.do(t, http.MethodPost, "/api/v1/admin/template-drafts", map[string]interface{}{
				"idempotencyKey": "draft-incomplete-001",
				"title":          testCase.title,
				"categoryId":     categoryID,
				"previewFileKey": previewKey,
				"patternData":    draftPatternJSON(t, 4, 4),
			}))

			response := portal.publishDraft(t, stamp.Draft.DraftID, "publish-incomplete-001", previewKey, stamp.Draft.UpdatedAt)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for an incomplete draft, got %d: %s", response.Code, response.Body.String())
			}
			if code := responseCode(t, response); code != apperr.CodeDraftNotPublishable {
				t.Fatalf("expected code %d, got %d", apperr.CodeDraftNotPublishable, code)
			}
			if count := countRows(t, &model.TemplateDraft{}); count != 1 {
				t.Fatalf("draft must survive a rejected publish, got %d rows", count)
			}
			if count := countRows(t, &model.Template{}); count != 0 {
				t.Fatalf("rejected publish must not create a template, got %d", count)
			}
		})
	}
}

// previewFileKey 为空由 media.GetUploadedAdminPreviewURL 自己挡下来（403/1003），发布
// 路径不再另加空判断。
func TestAdminPortalDraftPublishRequiresPreviewFileKey(t *testing.T) {
	portal := newDraftPortal(t, 0)
	stamp, _ := portal.createPublishableDraft(t, "draft-publish-nopreview", nil)

	response := portal.publishDraft(t, stamp.Draft.DraftID, "publish-nopreview-001", "", stamp.Draft.UpdatedAt)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a missing previewFileKey, got %d: %s", response.Code, response.Body.String())
	}
	if code := responseCode(t, response); code != apperr.CodeForbidden {
		t.Fatalf("expected code %d, got %d", apperr.CodeForbidden, code)
	}
	if count := countRows(t, &model.TemplateDraft{}); count != 1 {
		t.Fatalf("draft must survive a rejected publish, got %d rows", count)
	}
	if count := countRows(t, &model.Template{}); count != 0 {
		t.Fatalf("rejected publish must not create a template, got %d", count)
	}
}

// 关联模板在草稿创建之后被下架 → 必须是 4004 而不是 4002：草稿明明就在箱里，报「草稿
// 不存在」会让管理员无路可走。
func TestAdminPortalDraftPublishRejectsUnpublishedTemplate(t *testing.T) {
	portal := newDraftPortal(t, 0)
	live := portal.seedLiveTemplate(t)
	stamp, previewKey := portal.createPublishableDraft(t, "draft-publish-unpublished", &live.ID)

	if err := db.DB.Model(&model.Template{}).Where("id = ?", live.ID).Update("status", 0).Error; err != nil {
		t.Fatalf("unpublish template: %v", err)
	}

	response := portal.publishDraft(t, stamp.Draft.DraftID, "publish-unpublished-001", previewKey, stamp.Draft.UpdatedAt)
	if code := responseCode(t, response); code != apperr.CodeDraftNotPublishable {
		t.Fatalf("expected code %d for an unpublished linked template, got %d: %s",
			apperr.CodeDraftNotPublishable, code, response.Body.String())
	}
	if count := countRows(t, &model.TemplateDraft{}); count != 1 {
		t.Fatalf("draft must survive, got %d rows", count)
	}
	if count := countRows(t, &model.TemplateRevision{}); count != 0 {
		t.Fatalf("failed publish must not leave a revision, got %d", count)
	}
}

// 直接测 §6 的「失败时不得留下只更新了一半的模板」：预置一条同 key 不同 draft 的发布
// 记录，逼覆盖分支在写发布记录之前就失败，断言模板与 revision 都没被动过。
func TestAdminPortalDraftPublishRollsBackOnDuplicateKey(t *testing.T) {
	portal := newDraftPortal(t, 0)
	live := portal.seedLiveTemplate(t)
	beforePattern := patternJSON(t, live.PatternData)
	stamp, previewKey := portal.createPublishableDraft(t, "draft-publish-rollback", &live.ID)

	if err := db.DB.Create(&model.TemplatePublishRecord{
		IdempotencyKey:  "publish-rollback-001",
		TemplateID:      live.ID,
		DraftRevisionID: 999999,
		Status:          "published",
	}).Error; err != nil {
		t.Fatalf("seed publish record: %v", err)
	}

	response := portal.publishDraft(t, stamp.Draft.DraftID, "publish-rollback-001", previewKey, stamp.Draft.UpdatedAt)
	if response.Code == http.StatusOK {
		t.Fatalf("expected the reused idempotency key to be rejected, got 200: %s", response.Body.String())
	}

	var reloaded model.Template
	if err := db.DB.First(&reloaded, live.ID).Error; err != nil {
		t.Fatalf("reload template: %v", err)
	}
	if reloaded.Title != "旧标题" || patternJSON(t, reloaded.PatternData) != beforePattern {
		t.Fatalf("failed publish must leave the live template untouched: %#v", reloaded)
	}
	if count := countRows(t, &model.TemplateRevision{}); count != 0 {
		t.Fatalf("failed publish must not leave a revision, got %d", count)
	}
	if count := countRows(t, &model.TemplateDraft{}); count != 1 {
		t.Fatalf("draft must survive a failed publish, got %d rows", count)
	}
}

// 两个管理员同时点发布会生成不同的 idempotencyKey，所以发布记录的唯一键挡不住他们：
// 唯一的保护是事务里的行锁加末尾按 updated_at 的 CAS 删除。这里断言的是那条不变量
// ——模板只被写一次——而不是败者具体的错误码：SQLite 的表级锁可能让败者拿到 5000。
func TestAdminPortalDraftPublishConcurrentlyWritesOnce(t *testing.T) {
	portal := newDraftPortal(t, 0)
	stamp, previewKey := portal.createPublishableDraft(t, "draft-publish-concurrent", nil)

	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := range codes {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			response := portal.publishDraft(t, stamp.Draft.DraftID,
				fmt.Sprintf("publish-concurrent-%d", index), previewKey, stamp.Draft.UpdatedAt)
			codes[index] = response.Code
		}(i)
	}
	wg.Wait()

	winners := 0
	for _, code := range codes {
		if code == http.StatusOK {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("expected exactly one publish to win, got codes %v", codes)
	}
	if count := countRows(t, &model.Template{}); count != 1 {
		t.Fatalf("concurrent publishes must produce exactly one template, got %d", count)
	}
	if count := countRows(t, &model.TemplateDraft{}); count != 0 {
		t.Fatalf("the winning publish must delete the draft, got %d rows", count)
	}
	if count := countRows(t, &model.TemplatePublishRecord{}); count != 1 {
		t.Fatalf("expected exactly one publish record, got %d", count)
	}
}

// §9：已发布模板列表要标注「这张图有未发布的改动」。
func TestAdminPortalTemplateListAnnotatesDrafts(t *testing.T) {
	portal := newDraftPortal(t, 0)
	withDraft := portal.seedLiveTemplate(t)
	withoutDraft := portal.seedLiveTemplate(t)
	draft, _ := portal.createPublishableDraft(t, "draft-annotate", &withDraft.ID)

	response := portal.do(t, http.MethodGet, "/api/v1/admin/templates?page.page=1&page.pageSize=10", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected template list to return 200, got %d: %s", response.Code, response.Body.String())
	}

	var body struct {
		Templates []struct {
			TemplateID string `json:"templateId"`
			HasDraft   bool   `json:"hasDraft"`
			DraftID    string `json:"draftId"`
		} `json:"templates"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode template list: %v (%s)", err, response.Body.String())
	}
	if len(body.Templates) != 2 {
		t.Fatalf("expected two templates, got %d", len(body.Templates))
	}
	for _, item := range body.Templates {
		switch item.TemplateID {
		case fmt.Sprintf("%d", withDraft.ID):
			if !item.HasDraft || item.DraftID != draft.Draft.DraftID {
				t.Fatalf("template %s must report its draft %s, got hasDraft=%v draftId=%q",
					item.TemplateID, draft.Draft.DraftID, item.HasDraft, item.DraftID)
			}
		case fmt.Sprintf("%d", withoutDraft.ID):
			if item.HasDraft || item.DraftID != "" {
				t.Fatalf("template %s has no draft, got hasDraft=%v draftId=%q",
					item.TemplateID, item.HasDraft, item.DraftID)
			}
		default:
			t.Fatalf("unexpected templateId %q", item.TemplateID)
		}
	}
}

// 泄漏测试：草稿是后台概念，C 端模板列表绝不能出现 hasDraft/draftId。这两个字段只在
// listTemplates 这个后台 handler 里补，共享的 templateListColumns 一个字节都没动。
func TestClientTemplateListOmitsDraftAnnotations(t *testing.T) {
	portal := newDraftPortal(t, 0)
	live := portal.seedLiveTemplate(t)
	portal.createPublishableDraft(t, "draft-leak", &live.ID)

	handler := api.NewTemplateHandler(templateservice.NewService(dao.NewTemplateDAO(), dao.NewBlindBoxRecordDAO(), dao.NewBlindBoxPoolDAO(), dao.NewBlindBoxQuotaDAO()))
	clientResponse, err := handler.ListTemplates(context.Background(), &pb.ListTemplatesRequest{
		CategoryId: int32(portal.category.ID),
		Page:       &pb.PageRequest{Page: 1, PageSize: 10},
	})
	if err != nil {
		t.Fatalf("client ListTemplates: %v", err)
	}
	if len(clientResponse.Templates) != 1 {
		t.Fatalf("expected the live template in the client list, got %d items", len(clientResponse.Templates))
	}
	encoded, err := protojson.Marshal(clientResponse)
	if err != nil {
		t.Fatalf("marshal client response: %v", err)
	}
	for _, leaked := range []string{"hasDraft", "draftId", "has_draft", "draft_id"} {
		if strings.Contains(string(encoded), leaked) {
			t.Fatalf("client template list must not expose %q: %s", leaked, encoded)
		}
	}
}
