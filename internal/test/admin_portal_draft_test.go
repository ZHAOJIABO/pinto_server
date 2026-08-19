package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhaojiabo/bobobeads_server/conf"
	"github.com/zhaojiabo/bobobeads_server/internal/api"
	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	adminauth "github.com/zhaojiabo/bobobeads_server/internal/service/admin"
	"google.golang.org/protobuf/encoding/protojson"
	"gorm.io/gorm"
)

type draftPortal struct {
	handler  *api.AdminPortalHTTPHandler
	token    string
	storage  *memoryObjectStorage
	category *model.TemplateCategory
}

func newDraftPortal(t *testing.T, maxDrafts int) *draftPortal {
	t.Helper()
	SetupTestDB(t)
	passwordHash, err := adminauth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	previousConfig := conf.GlobalConfig
	conf.GlobalConfig = &conf.Config{
		Pattern: conf.PatternConfig{MaxWidth: 200, MaxHeight: 200, MaxPixels: 40000, MaxColors: 221},
		Admin: conf.AdminConfig{
			JWTSecret: "admin-test-secret",
			Accounts:  []conf.AdminAccountConfig{{Username: "operator", PasswordHash: passwordHash}},
		},
		TemplateDraft: conf.TemplateDraftConfig{MaxCount: maxDrafts},
	}
	t.Cleanup(func() { conf.GlobalConfig = previousConfig })

	storage := newMemoryObjectStorage("https://cdn.example.test")
	handler := newTestPortalHandler(conf.GlobalConfig.Admin, storage, dao.NewTemplateDAO(), newTestSubmissionService())

	category := &model.TemplateCategory{Name: "动物", Status: 1}
	if err := db.DB.Create(category).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}

	return &draftPortal{
		handler:  handler,
		token:    adminPortalLogin(t, handler, "operator", "correct horse battery staple"),
		storage:  storage,
		category: category,
	}
}

func (p *draftPortal) do(t *testing.T, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	return p.request(t, method, path, body, true)
}

func (p *draftPortal) request(t *testing.T, method, path string, body interface{}, authorized bool) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, path, reader)
	if authorized {
		request.Header.Set("Authorization", "Bearer "+p.token)
	}
	response := httptest.NewRecorder()
	p.handler.ServeHTTP(response, request)
	return response
}

// draftStamp 是 §2/§3 响应里 draft 字段的形状。
type draftStamp struct {
	Draft struct {
		DraftID    string `json:"draftId"`
		TemplateID string `json:"templateId"`
		UpdatedAt  string `json:"updatedAt"`
	} `json:"draft"`
	Header struct {
		Code    int32  `json:"code"`
		Message string `json:"message"`
	} `json:"header"`
}

func decodeDraftStamp(t *testing.T, response *httptest.ResponseRecorder) draftStamp {
	t.Helper()
	var stamp draftStamp
	if err := json.Unmarshal(response.Body.Bytes(), &stamp); err != nil {
		t.Fatalf("decode draft response: %v (%s)", err, response.Body.String())
	}
	return stamp
}

func responseCode(t *testing.T, response *httptest.ResponseRecorder) int32 {
	t.Helper()
	var envelope struct {
		Header struct {
			Code int32 `json:"code"`
		} `json:"header"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v (%s)", err, response.Body.String())
	}
	return envelope.Header.Code
}

// draftPatternJSON 生成一份结构合法的 patternData，boardSpec 与尺寸严格一致。
func draftPatternJSON(t *testing.T, width, height int32) json.RawMessage {
	t.Helper()
	pattern := validPatternData(width, height)
	pattern.BoardSpec = fmt.Sprintf("%dx%d", width, height)
	encoded, err := protojson.Marshal(pattern)
	if err != nil {
		t.Fatalf("marshal pattern data: %v", err)
	}
	return json.RawMessage(encoded)
}

// blankPatternJSON 是全空画布：一颗豆都没落，调色板合法地为空。
func blankPatternJSON(t *testing.T, width, height int32) json.RawMessage {
	t.Helper()
	encoded, err := protojson.Marshal(&pb.PatternData{
		Width:         width,
		Height:        height,
		BoardSpec:     fmt.Sprintf("%dx%d", width, height),
		SchemaVersion: 1,
		Pixels:        make([]int32, int(width*height)),
	})
	if err != nil {
		t.Fatalf("marshal blank pattern: %v", err)
	}
	return json.RawMessage(encoded)
}

// createMinimalDraft 只带必填的两个字段，这本身就是「草稿不复用发布校验」的断言。
func (p *draftPortal) createMinimalDraft(t *testing.T, idempotencyKey string) draftStamp {
	t.Helper()
	response := p.do(t, http.MethodPost, "/api/v1/admin/template-drafts", map[string]interface{}{
		"idempotencyKey": idempotencyKey,
		"patternData":    draftPatternJSON(t, 4, 4),
	})
	if response.Code != http.StatusOK {
		t.Fatalf("expected minimal draft create to return 200, got %d: %s", response.Code, response.Body.String())
	}
	return decodeDraftStamp(t, response)
}

func TestAdminPortalDraftRoutesRequireToken(t *testing.T) {
	portal := newDraftPortal(t, 0)

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/admin/template-drafts"},
		{http.MethodGet, "/api/v1/admin/template-drafts"},
		{http.MethodGet, "/api/v1/admin/template-drafts/1"},
		{http.MethodPut, "/api/v1/admin/template-drafts/1"},
		{http.MethodDelete, "/api/v1/admin/template-drafts/1"},
	}
	for _, route := range routes {
		response := portal.request(t, route.method, route.path, map[string]interface{}{}, false)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: expected 401 without token, got %d", route.method, route.path, response.Code)
		}
	}
}

// 除 patternData 与 idempotencyKey 外全部留空必须成功。这条直接证明草稿路径没有复用
// parseTemplatePayload——复用会从 GetUploadedAdminPreviewURL 得到 403。
func TestAdminPortalDraftStoresEmptyFields(t *testing.T) {
	portal := newDraftPortal(t, 0)
	stamp := portal.createMinimalDraft(t, "draft-empty-001")

	if stamp.Draft.DraftID == "" {
		t.Fatal("expected draftId in create response")
	}
	if stamp.Draft.TemplateID != "" {
		t.Fatalf("standalone draft must report empty templateId, got %q", stamp.Draft.TemplateID)
	}

	var draft model.TemplateDraft
	if err := db.DB.First(&draft).Error; err != nil {
		t.Fatalf("find draft: %v", err)
	}
	if draft.Title != "" || draft.CategoryID != 0 || draft.PreviewFileKey != "" {
		t.Fatalf("draft must persist empty business fields, got %#v", draft)
	}
	if draft.Width != 4 || draft.Height != 4 || draft.ColorCount != 1 {
		t.Fatalf("draft must derive dimensions from patternData, got %dx%d/%d colors", draft.Width, draft.Height, draft.ColorCount)
	}
	if draft.UpdatedByActor != "operator" {
		t.Fatalf("draft must record the acting admin, got %q", draft.UpdatedByActor)
	}
}

func TestAdminPortalDraftRequiresIdempotencyKey(t *testing.T) {
	portal := newDraftPortal(t, 0)

	body := map[string]interface{}{
		"idempotencyKey": "",
		"patternData":    draftPatternJSON(t, 2, 2),
	}
	// 连打两次：第二次若走到 INSERT 会撞 uk_tpl_draft_idem 并被兜底分支变成 500。
	for attempt := 0; attempt < 2; attempt++ {
		response := portal.do(t, http.MethodPost, "/api/v1/admin/template-drafts", body)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d: expected 400 for empty idempotencyKey, got %d: %s", attempt, response.Code, response.Body.String())
		}
		if code := responseCode(t, response); code != apperr.CodeInvalidArgument {
			t.Fatalf("attempt %d: expected code %d, got %d", attempt, apperr.CodeInvalidArgument, code)
		}
	}

	var count int64
	if err := db.DB.Model(&model.TemplateDraft{}).Count(&count).Error; err != nil {
		t.Fatalf("count drafts: %v", err)
	}
	if count != 0 {
		t.Fatalf("rejected creates must not persist rows, got %d", count)
	}
}

func TestAdminPortalDraftReplaysIdempotencyKey(t *testing.T) {
	portal := newDraftPortal(t, 0)
	first := portal.createMinimalDraft(t, "draft-replay-001")
	second := portal.createMinimalDraft(t, "draft-replay-001")

	if first.Draft.DraftID != second.Draft.DraftID {
		t.Fatalf("replayed create must return the same draftId, got %s then %s", first.Draft.DraftID, second.Draft.DraftID)
	}
	var count int64
	if err := db.DB.Model(&model.TemplateDraft{}).Count(&count).Error; err != nil {
		t.Fatalf("count drafts: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one draft after replay, got %d", count)
	}
}

func TestAdminPortalDraftAcceptsBlankCanvasAndRejectsBadSchema(t *testing.T) {
	portal := newDraftPortal(t, 0)

	blank := portal.do(t, http.MethodPost, "/api/v1/admin/template-drafts", map[string]interface{}{
		"idempotencyKey": "draft-blank-001",
		"patternData":    blankPatternJSON(t, 3, 3),
	})
	if blank.Code != http.StatusOK {
		t.Fatalf("expected blank canvas draft to save, got %d: %s", blank.Code, blank.Body.String())
	}

	// 非空调色板但缺 schemaVersion：委托给 CalculatePatternStats 的规则仍须生效。
	pattern := validPatternData(2, 2)
	pattern.BoardSpec = "2x2"
	pattern.SchemaVersion = 0
	encoded, err := protojson.Marshal(pattern)
	if err != nil {
		t.Fatalf("marshal pattern: %v", err)
	}
	bad := portal.do(t, http.MethodPost, "/api/v1/admin/template-drafts", map[string]interface{}{
		"idempotencyKey": "draft-blank-002",
		"patternData":    json.RawMessage(encoded),
	})
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("expected missing schemaVersion to return 400, got %d: %s", bad.Code, bad.Body.String())
	}

	// boardSpec 与尺寸不一致同样是 400。
	pattern = validPatternData(2, 2)
	pattern.BoardSpec = "29x29"
	encoded, err = protojson.Marshal(pattern)
	if err != nil {
		t.Fatalf("marshal pattern: %v", err)
	}
	mismatch := portal.do(t, http.MethodPost, "/api/v1/admin/template-drafts", map[string]interface{}{
		"idempotencyKey": "draft-blank-003",
		"patternData":    json.RawMessage(encoded),
	})
	if mismatch.Code != http.StatusBadRequest {
		t.Fatalf("expected boardSpec mismatch to return 400, got %d: %s", mismatch.Code, mismatch.Body.String())
	}
}

func TestAdminPortalDraftRejectsUnknownFields(t *testing.T) {
	portal := newDraftPortal(t, 0)
	response := portal.do(t, http.MethodPost, "/api/v1/admin/template-drafts", map[string]interface{}{
		"idempotencyKey": "draft-unknown-001",
		"patternData":    draftPatternJSON(t, 2, 2),
		"isFree":         true,
	})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected unknown field to return 400, got %d: %s", response.Code, response.Body.String())
	}
}

func TestAdminPortalDraftRejectsUnknownTemplate(t *testing.T) {
	portal := newDraftPortal(t, 0)

	missing := portal.do(t, http.MethodPost, "/api/v1/admin/template-drafts", map[string]interface{}{
		"idempotencyKey": "draft-link-001",
		"templateId":     "999999",
		"patternData":    draftPatternJSON(t, 2, 2),
	})
	if missing.Code != http.StatusNotFound {
		t.Fatalf("expected unknown templateId to return 404, got %d: %s", missing.Code, missing.Body.String())
	}

	hidden := &model.Template{CategoryID: portal.category.ID, Title: "已下架", Status: 1}
	if err := db.DB.Create(hidden).Error; err != nil {
		t.Fatalf("create template: %v", err)
	}
	if err := db.DB.Model(hidden).Update("status", 0).Error; err != nil {
		t.Fatalf("unpublish template: %v", err)
	}
	unpublished := portal.do(t, http.MethodPost, "/api/v1/admin/template-drafts", map[string]interface{}{
		"idempotencyKey": "draft-link-002",
		"templateId":     fmt.Sprintf("%d", hidden.ID),
		"patternData":    draftPatternJSON(t, 2, 2),
	})
	if unpublished.Code != http.StatusNotFound {
		t.Fatalf("expected unpublished templateId to return 404, got %d: %s", unpublished.Code, unpublished.Body.String())
	}
}

// §1 的核心不变量：草稿保存前后线上模板逐字节不变。
func TestAdminPortalDraftLeavesPublishedTemplateUntouched(t *testing.T) {
	portal := newDraftPortal(t, 0)

	live := &model.Template{
		CategoryID:   portal.category.ID,
		Title:        "线上小猫",
		PreviewURL:   "https://cdn.example.test/live-preview.png",
		ThumbnailURL: "https://cdn.example.test/live-thumb.png",
		PatternData:  model.JSONMap{"schemaVersion": float64(1), "width": float64(2), "height": float64(2)},
		BoardSpec:    "2x2",
		Width:        2,
		Height:       2,
		ColorCount:   1,
		Status:       1,
	}
	if err := db.DB.Create(live).Error; err != nil {
		t.Fatalf("create live template: %v", err)
	}
	var before model.Template
	if err := db.DB.First(&before, live.ID).Error; err != nil {
		t.Fatalf("snapshot live template: %v", err)
	}

	stamp := portal.do(t, http.MethodPost, "/api/v1/admin/template-drafts", map[string]interface{}{
		"idempotencyKey": "draft-revision-001",
		"templateId":     fmt.Sprintf("%d", live.ID),
		"title":          "改了一半的小猫",
		"categoryId":     portal.category.ID,
		"patternData":    draftPatternJSON(t, 6, 6),
	})
	if stamp.Code != http.StatusOK {
		t.Fatalf("expected revision draft to save, got %d: %s", stamp.Code, stamp.Body.String())
	}
	created := decodeDraftStamp(t, stamp)
	if created.Draft.TemplateID != fmt.Sprintf("%d", live.ID) {
		t.Fatalf("revision draft must echo its templateId, got %q", created.Draft.TemplateID)
	}

	portal.do(t, http.MethodPut, "/api/v1/admin/template-drafts/"+created.Draft.DraftID, map[string]interface{}{
		"baseUpdatedAt": created.Draft.UpdatedAt,
		"title":         "又改了一次",
		"categoryId":    portal.category.ID,
		"patternData":   draftPatternJSON(t, 8, 8),
	})

	var after model.Template
	if err := db.DB.First(&after, live.ID).Error; err != nil {
		t.Fatalf("reload live template: %v", err)
	}
	beforeJSON, _ := json.Marshal(before.PatternData)
	afterJSON, _ := json.Marshal(after.PatternData)
	if !bytes.Equal(beforeJSON, afterJSON) {
		t.Fatalf("draft save must not touch live pattern_data: %s vs %s", beforeJSON, afterJSON)
	}
	if after.PreviewURL != before.PreviewURL || after.Title != before.Title || after.Width != before.Width {
		t.Fatalf("draft save must not touch live template fields: %#v vs %#v", before, after)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("draft save must not bump live updated_at: %s vs %s", before.UpdatedAt, after.UpdatedAt)
	}
}

// 同一个模板最多挂一份草稿：重复创建返回已存在的那份。
func TestAdminPortalDraftReusesDraftForSameTemplate(t *testing.T) {
	portal := newDraftPortal(t, 0)
	live := &model.Template{CategoryID: portal.category.ID, Title: "线上", Status: 1}
	if err := db.DB.Create(live).Error; err != nil {
		t.Fatalf("create live template: %v", err)
	}

	body := func(key string) map[string]interface{} {
		return map[string]interface{}{
			"idempotencyKey": key,
			"templateId":     fmt.Sprintf("%d", live.ID),
			"patternData":    draftPatternJSON(t, 2, 2),
		}
	}
	first := decodeDraftStamp(t, portal.do(t, http.MethodPost, "/api/v1/admin/template-drafts", body("draft-same-001")))
	second := decodeDraftStamp(t, portal.do(t, http.MethodPost, "/api/v1/admin/template-drafts", body("draft-same-002")))
	if first.Draft.DraftID != second.Draft.DraftID {
		t.Fatalf("second create for the same template must reuse the draft, got %s then %s", first.Draft.DraftID, second.Draft.DraftID)
	}
}

func TestAdminPortalDraftOptimisticLock(t *testing.T) {
	portal := newDraftPortal(t, 0)
	created := portal.createMinimalDraft(t, "draft-lock-001")
	path := "/api/v1/admin/template-drafts/" + created.Draft.DraftID

	first := portal.do(t, http.MethodPut, path, map[string]interface{}{
		"baseUpdatedAt": created.Draft.UpdatedAt,
		"title":         "第一次改",
		"patternData":   draftPatternJSON(t, 4, 4),
	})
	if first.Code != http.StatusOK {
		t.Fatalf("expected first update to return 200, got %d: %s", first.Code, first.Body.String())
	}
	updated := decodeDraftStamp(t, first)
	if updated.Draft.UpdatedAt == created.Draft.UpdatedAt {
		t.Fatal("successful update must return a fresh updatedAt")
	}

	var beforeConflict model.TemplateDraft
	if err := db.DB.First(&beforeConflict).Error; err != nil {
		t.Fatalf("load draft: %v", err)
	}

	// 同一个已过期的基线再打一次：必须 409 + 4001，且库里那行完全没动。
	stale := portal.do(t, http.MethodPut, path, map[string]interface{}{
		"baseUpdatedAt": created.Draft.UpdatedAt,
		"title":         "第二次改",
		"patternData":   draftPatternJSON(t, 4, 4),
	})
	if stale.Code != http.StatusConflict {
		t.Fatalf("expected stale baseUpdatedAt to return 409, got %d: %s", stale.Code, stale.Body.String())
	}
	if code := responseCode(t, stale); code != apperr.CodeDraftConflict {
		t.Fatalf("expected code %d, got %d", apperr.CodeDraftConflict, code)
	}

	var afterConflict model.TemplateDraft
	if err := db.DB.First(&afterConflict).Error; err != nil {
		t.Fatalf("reload draft: %v", err)
	}
	if afterConflict.Title != beforeConflict.Title || !afterConflict.UpdatedAt.Equal(beforeConflict.UpdatedAt) {
		t.Fatalf("rejected update must not modify the row: %#v vs %#v", beforeConflict, afterConflict)
	}
}

// 秒级精度会让同一毫秒内的第二次更新静默成功并丢掉第一次的改动。
func TestAdminPortalDraftConflictWithinSameMillisecond(t *testing.T) {
	portal := newDraftPortal(t, 0)
	created := portal.createMinimalDraft(t, "draft-ms-001")
	path := "/api/v1/admin/template-drafts/" + created.Draft.DraftID

	body := map[string]interface{}{
		"baseUpdatedAt": created.Draft.UpdatedAt,
		"title":         "并发改",
		"patternData":   draftPatternJSON(t, 4, 4),
	}
	first := portal.do(t, http.MethodPut, path, body)
	second := portal.do(t, http.MethodPut, path, body)
	if first.Code != http.StatusOK {
		t.Fatalf("expected first update to succeed, got %d: %s", first.Code, first.Body.String())
	}
	if second.Code != http.StatusConflict {
		t.Fatalf("expected second update on the same baseline to conflict, got %d: %s", second.Code, second.Body.String())
	}
}

// 精度回归：写进库的时间戳必须是整毫秒，且 JSON 往返回精确同一时刻。
func TestAdminPortalDraftTimestampPrecision(t *testing.T) {
	portal := newDraftPortal(t, 0)
	created := portal.createMinimalDraft(t, "draft-precision-001")

	var draft model.TemplateDraft
	if err := db.DB.First(&draft).Error; err != nil {
		t.Fatalf("load draft: %v", err)
	}
	if draft.UpdatedAt.Nanosecond()%int(time.Millisecond) != 0 {
		t.Fatalf("stored updated_at must be millisecond-aligned, got %s", draft.UpdatedAt)
	}
	if !draft.CreatedAt.Equal(draft.UpdatedAt) {
		t.Fatalf("create must stamp both timestamps identically, got %s vs %s", draft.CreatedAt, draft.UpdatedAt)
	}

	parsed, err := time.Parse(time.RFC3339Nano, created.Draft.UpdatedAt)
	if err != nil {
		t.Fatalf("parse serialized updatedAt %q: %v", created.Draft.UpdatedAt, err)
	}
	if !parsed.Equal(draft.UpdatedAt) {
		t.Fatalf("serialized updatedAt %s must round-trip to the stored value %s", parsed, draft.UpdatedAt)
	}
}

func TestAdminPortalDraftDetailReturnsPatternData(t *testing.T) {
	portal := newDraftPortal(t, 0)
	created := portal.do(t, http.MethodPost, "/api/v1/admin/template-drafts", map[string]interface{}{
		"idempotencyKey": "draft-detail-001",
		"title":          "半成品",
		"description":    "还没画完",
		"categoryId":     portal.category.ID,
		"tags":           "猫, 简单 ,",
		"difficulty":     2,
		"patternData":    draftPatternJSON(t, 4, 4),
	})
	stamp := decodeDraftStamp(t, created)

	response := portal.do(t, http.MethodGet, "/api/v1/admin/template-drafts/"+stamp.Draft.DraftID, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected draft detail to return 200, got %d: %s", response.Code, response.Body.String())
	}
	var detail struct {
		Draft struct {
			DraftID    string   `json:"draftId"`
			TemplateID string   `json:"templateId"`
			Title      string   `json:"title"`
			CategoryID int      `json:"categoryId"`
			Tags       []string `json:"tags"`
			Difficulty int      `json:"difficulty"`
			UpdatedAt  string   `json:"updatedAt"`
		} `json:"draft"`
		PatternData struct {
			Width         int32 `json:"width"`
			Height        int32 `json:"height"`
			SchemaVersion int32 `json:"schemaVersion"`
		} `json:"patternData"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode draft detail: %v", err)
	}
	if detail.Draft.DraftID != stamp.Draft.DraftID || detail.Draft.Title != "半成品" || detail.Draft.Difficulty != 2 {
		t.Fatalf("unexpected draft detail: %#v", detail.Draft)
	}
	// tags 在详情里是数组，在请求里是逗号字符串——这个不对称与 getTemplate 一致。
	if len(detail.Draft.Tags) != 2 || detail.Draft.Tags[0] != "猫" || detail.Draft.Tags[1] != "简单" {
		t.Fatalf("detail tags must be a trimmed array, got %#v", detail.Draft.Tags)
	}
	if detail.PatternData.Width != 4 || detail.PatternData.Height != 4 || detail.PatternData.SchemaVersion != 1 {
		t.Fatalf("detail must return patternData for local rendering, got %#v", detail.PatternData)
	}

	if missing := portal.do(t, http.MethodGet, "/api/v1/admin/template-drafts/999999", nil); missing.Code != http.StatusNotFound {
		t.Fatalf("expected unknown draft detail to return 404, got %d", missing.Code)
	} else if code := responseCode(t, missing); code != apperr.CodeDraftNotFound {
		t.Fatalf("expected code %d, got %d", apperr.CodeDraftNotFound, code)
	}
}

func TestAdminPortalDraftListOrdersAndOmitsPatternData(t *testing.T) {
	portal := newDraftPortal(t, 0)

	// 第一份挂在线上模板上（借用它的缩略图），第二份自带预览图，第三份什么都没有。
	live := &model.Template{
		CategoryID:   portal.category.ID,
		Title:        "线上",
		PreviewURL:   "https://cdn.example.test/live-preview.png",
		ThumbnailURL: "https://cdn.example.test/live-thumb.png",
		Status:       1,
	}
	if err := db.DB.Create(live).Error; err != nil {
		t.Fatalf("create live template: %v", err)
	}
	linked := decodeDraftStamp(t, portal.do(t, http.MethodPost, "/api/v1/admin/template-drafts", map[string]interface{}{
		"idempotencyKey": "draft-list-001",
		"templateId":     fmt.Sprintf("%d", live.ID),
		"categoryId":     portal.category.ID,
		"patternData":    draftPatternJSON(t, 2, 2),
	}))

	previewKey := "admin_preview/2026/08/19/0/draft.png"
	if err := db.DB.Create(&model.MediaAsset{
		FileKey:     previewKey,
		Purpose:     "admin_preview",
		ContentType: "image/png",
		Status:      model.MediaStatusUploaded,
	}).Error; err != nil {
		t.Fatalf("create preview asset: %v", err)
	}
	portal.storage.put(previewKey, "image/png", pngBytes(t, 400, 400))
	withPreview := decodeDraftStamp(t, portal.do(t, http.MethodPost, "/api/v1/admin/template-drafts", map[string]interface{}{
		"idempotencyKey": "draft-list-002",
		"previewFileKey": previewKey,
		"patternData":    draftPatternJSON(t, 2, 2),
	}))
	bare := decodeDraftStamp(t, portal.do(t, http.MethodPost, "/api/v1/admin/template-drafts", map[string]interface{}{
		"idempotencyKey": "draft-list-003",
		"patternData":    draftPatternJSON(t, 2, 2),
	}))

	queries := captureQueries(t)
	response := portal.do(t, http.MethodGet, "/api/v1/admin/template-drafts?page.page=1&page.pageSize=10", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected draft list to return 200, got %d: %s", response.Code, response.Body.String())
	}
	executed := queries()
	if len(executed) == 0 {
		t.Fatal("query capture recorded nothing, the pattern_data assertion would be vacuous")
	}
	for _, sql := range executed {
		if strings.Contains(sql, "pattern_data") {
			t.Fatalf("draft list must never select pattern_data: %s", sql)
		}
	}
	if strings.Contains(response.Body.String(), "patternData") {
		t.Fatalf("draft list body must not carry patternData: %s", response.Body.String())
	}

	var list struct {
		Drafts []struct {
			DraftID      string `json:"draftId"`
			TemplateID   string `json:"templateId"`
			CategoryName string `json:"categoryName"`
			ThumbnailURL string `json:"thumbnailUrl"`
			Width        int    `json:"width"`
		} `json:"drafts"`
		Page struct {
			Total    int64 `json:"total"`
			HasMore  bool  `json:"hasMore"`
			Page     int   `json:"page"`
			PageSize int   `json:"pageSize"`
		} `json:"page"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode draft list: %v", err)
	}
	if list.Page.Total != 3 || list.Page.HasMore {
		t.Fatalf("unexpected page envelope: %#v", list.Page)
	}
	// 最近改的排最前，所以创建顺序反过来。
	wantOrder := []string{bare.Draft.DraftID, withPreview.Draft.DraftID, linked.Draft.DraftID}
	for i, want := range wantOrder {
		if list.Drafts[i].DraftID != want {
			t.Fatalf("draft list must sort by updatedAt desc, got %#v want %#v", list.Drafts, wantOrder)
		}
	}
	if list.Drafts[2].ThumbnailURL != live.ThumbnailURL {
		t.Fatalf("revision draft must borrow the live thumbnail, got %q", list.Drafts[2].ThumbnailURL)
	}
	if list.Drafts[2].CategoryName != portal.category.Name {
		t.Fatalf("draft list must resolve category names, got %q", list.Drafts[2].CategoryName)
	}
	if list.Drafts[1].ThumbnailURL == "" {
		t.Fatal("draft with its own previewFileKey must expose a thumbnail")
	}
	if list.Drafts[0].ThumbnailURL != "" {
		t.Fatalf("draft without any preview must report an empty thumbnail, got %q", list.Drafts[0].ThumbnailURL)
	}
	if list.Drafts[0].Width != 2 {
		t.Fatalf("draft list must expose dimensions without patternData, got %d", list.Drafts[0].Width)
	}
}

func TestAdminPortalDraftAutosaveSkipsThumbnailRegeneration(t *testing.T) {
	portal := newDraftPortal(t, 0)
	previewKey := "admin_preview/2026/08/19/0/autosave.png"
	if err := db.DB.Create(&model.MediaAsset{
		FileKey:     previewKey,
		Purpose:     "admin_preview",
		ContentType: "image/png",
		Status:      model.MediaStatusUploaded,
	}).Error; err != nil {
		t.Fatalf("create preview asset: %v", err)
	}
	portal.storage.put(previewKey, "image/png", pngBytes(t, 400, 400))

	created := decodeDraftStamp(t, portal.do(t, http.MethodPost, "/api/v1/admin/template-drafts", map[string]interface{}{
		"idempotencyKey": "draft-autosave-001",
		"previewFileKey": previewKey,
		"patternData":    draftPatternJSON(t, 2, 2),
	}))
	objectsAfterCreate := len(portal.storage.objects)

	updated := decodeDraftStamp(t, portal.do(t, http.MethodPut, "/api/v1/admin/template-drafts/"+created.Draft.DraftID, map[string]interface{}{
		"baseUpdatedAt":  created.Draft.UpdatedAt,
		"previewFileKey": previewKey,
		"title":          "自动保存",
		"patternData":    draftPatternJSON(t, 2, 2),
	}))
	if updated.Draft.UpdatedAt == "" {
		t.Fatal("autosave must return a fresh updatedAt")
	}
	if len(portal.storage.objects) != objectsAfterCreate {
		t.Fatalf("autosave with an unchanged previewFileKey must not regenerate the thumbnail: %d objects became %d", objectsAfterCreate, len(portal.storage.objects))
	}

	var draft model.TemplateDraft
	if err := db.DB.First(&draft).Error; err != nil {
		t.Fatalf("load draft: %v", err)
	}
	if draft.ThumbnailURL == "" || draft.PreviewURL == "" {
		t.Fatalf("autosave must keep the persisted preview URLs, got %#v", draft)
	}
}

func TestAdminPortalDraftLimitAppliesOnlyToCreate(t *testing.T) {
	portal := newDraftPortal(t, 2)
	first := portal.createMinimalDraft(t, "draft-limit-001")
	portal.createMinimalDraft(t, "draft-limit-002")

	third := portal.do(t, http.MethodPost, "/api/v1/admin/template-drafts", map[string]interface{}{
		"idempotencyKey": "draft-limit-003",
		"patternData":    draftPatternJSON(t, 2, 2),
	})
	if third.Code != http.StatusBadRequest {
		t.Fatalf("expected the draft cap to reject the third create, got %d: %s", third.Code, third.Body.String())
	}
	if code := responseCode(t, third); code != apperr.CodeDraftLimitExceeded {
		t.Fatalf("expected code %d, got %d", apperr.CodeDraftLimitExceeded, code)
	}

	// 满箱时仍然必须能编辑现有草稿，否则管理员没有腾位的路径。
	update := portal.do(t, http.MethodPut, "/api/v1/admin/template-drafts/"+first.Draft.DraftID, map[string]interface{}{
		"baseUpdatedAt": first.Draft.UpdatedAt,
		"title":         "满箱也要能改",
		"patternData":   draftPatternJSON(t, 4, 4),
	})
	if update.Code != http.StatusOK {
		t.Fatalf("expected update to succeed at the cap, got %d: %s", update.Code, update.Body.String())
	}

	// 满箱时重新打开已存在的修订草稿也必须可行。
	live := &model.Template{CategoryID: portal.category.ID, Title: "线上", Status: 1}
	if err := db.DB.Create(live).Error; err != nil {
		t.Fatalf("create live template: %v", err)
	}
	linkedDraft := &model.TemplateDraft{
		TemplateID:     &live.ID,
		IdempotencyKey: "draft-limit-preexisting",
		PatternData:    model.JSONMap{"schemaVersion": float64(1)},
		CreatedAt:      dao.NowMillis(),
		UpdatedAt:      dao.NowMillis(),
	}
	if err := db.DB.Create(linkedDraft).Error; err != nil {
		t.Fatalf("create linked draft: %v", err)
	}
	reopen := portal.do(t, http.MethodPost, "/api/v1/admin/template-drafts", map[string]interface{}{
		"idempotencyKey": "draft-limit-004",
		"templateId":     fmt.Sprintf("%d", live.ID),
		"patternData":    draftPatternJSON(t, 2, 2),
	})
	if reopen.Code != http.StatusOK {
		t.Fatalf("expected reopening an existing revision draft to succeed at the cap, got %d: %s", reopen.Code, reopen.Body.String())
	}
	if got := decodeDraftStamp(t, reopen).Draft.DraftID; got != fmt.Sprintf("%d", linkedDraft.ID) {
		t.Fatalf("expected the pre-existing draft %d, got %s", linkedDraft.ID, got)
	}
}

func TestAdminPortalDraftDeleteIsIdempotent(t *testing.T) {
	portal := newDraftPortal(t, 0)
	live := &model.Template{CategoryID: portal.category.ID, Title: "线上", Status: 1}
	if err := db.DB.Create(live).Error; err != nil {
		t.Fatalf("create live template: %v", err)
	}
	created := decodeDraftStamp(t, portal.do(t, http.MethodPost, "/api/v1/admin/template-drafts", map[string]interface{}{
		"idempotencyKey": "draft-delete-001",
		"templateId":     fmt.Sprintf("%d", live.ID),
		"patternData":    draftPatternJSON(t, 2, 2),
	}))

	paths := []string{
		"/api/v1/admin/template-drafts/" + created.Draft.DraftID,
		"/api/v1/admin/template-drafts/" + created.Draft.DraftID,
		"/api/v1/admin/template-drafts/999999",
	}
	for i, path := range paths {
		response := portal.do(t, http.MethodDelete, path, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("delete %d (%s): expected 200, got %d: %s", i, path, response.Code, response.Body.String())
		}
	}

	var count int64
	if err := db.DB.Model(&model.TemplateDraft{}).Count(&count).Error; err != nil {
		t.Fatalf("count drafts: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected the draft to be gone, got %d rows", count)
	}
	var stillLive model.Template
	if err := db.DB.First(&stillLive, live.ID).Error; err != nil {
		t.Fatalf("discarding a draft must not touch its template: %v", err)
	}
	if stillLive.Status != 1 {
		t.Fatalf("linked template must stay published, got status %d", stillLive.Status)
	}
}

// captureQueries 记录钩子安装期间执行过的每一条 SQL，用来断言投影里没有 pattern_data。
// 返回的闭包同时卸载钩子。
func captureQueries(t *testing.T) func() []string {
	t.Helper()
	const callbackName = "test:capture_queries"
	var statements []string
	if err := db.DB.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		statements = append(statements, tx.Statement.SQL.String())
	}); err != nil {
		t.Fatalf("register query callback: %v", err)
	}
	t.Cleanup(func() {
		_ = db.DB.Callback().Query().Remove(callbackName)
	})
	return func() []string {
		_ = db.DB.Callback().Query().Remove(callbackName)
		return statements
	}
}
