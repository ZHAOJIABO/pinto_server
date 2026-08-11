package test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/zhaojiabo/bobobeads_server/conf"
	"github.com/zhaojiabo/bobobeads_server/internal/api"
	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	adminauth "github.com/zhaojiabo/bobobeads_server/internal/service/admin"
	"github.com/zhaojiabo/bobobeads_server/internal/service/media"
	templateservice "github.com/zhaojiabo/bobobeads_server/internal/service/template"
	"github.com/zhaojiabo/bobobeads_server/internal/service/templatesubmission"
)

// newTestSubmissionService satisfies the portal constructor for tests that only
// exercise the template routes. Submission tests build the service themselves so
// they can share one storage backend with the media service.
func newTestSubmissionService() *templatesubmission.Service {
	templateDAO := dao.NewTemplateDAO()
	return templatesubmission.NewService(
		dao.NewTemplateSubmissionDAO(), dao.NewWorkDAO(), dao.NewUserDAO(),
		media.NewServiceWithStorage(dao.NewMediaDAO(), newMemoryObjectStorage("https://cdn.example.test")),
		templateservice.NewAdminService(templateDAO), 5,
	)
}

type submissionPortal struct {
	handler  *api.AdminPortalHTTPHandler
	token    string
	service  *templatesubmission.Service
	media    *media.Service
	category *model.TemplateCategory
}

func newSubmissionPortal(t *testing.T) *submissionPortal {
	t.Helper()
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
	}
	t.Cleanup(func() { conf.GlobalConfig = previousConfig })

	storage := newMemoryObjectStorage("https://cdn.example.test")
	mediaSvc := media.NewServiceWithStorage(dao.NewMediaDAO(), storage)
	templateDAO := dao.NewTemplateDAO()
	templateAdmin := templateservice.NewAdminService(templateDAO)
	service := templatesubmission.NewService(
		dao.NewTemplateSubmissionDAO(), dao.NewWorkDAO(), dao.NewUserDAO(),
		mediaSvc, templateAdmin, 20,
	)
	handler := api.NewAdminPortalHTTPHandler(
		adminauth.NewAuthService(conf.GlobalConfig.Admin),
		mediaSvc,
		templateservice.NewService(templateDAO, dao.NewBlindBoxRecordDAO()),
		templateAdmin,
		service,
	)

	category := &model.TemplateCategory{Name: "动物", Status: 1}
	if err := db.DB.Create(category).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}

	return &submissionPortal{
		handler:  handler,
		token:    adminPortalLogin(t, handler, "operator", "correct horse battery staple"),
		service:  service,
		media:    mediaSvc,
		category: category,
	}
}

func (p *submissionPortal) do(t *testing.T, method, path string, body interface{}) *httptest.ResponseRecorder {
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
	request.Header.Set("Authorization", "Bearer "+p.token)
	response := httptest.NewRecorder()
	p.handler.ServeHTTP(response, request)
	return response
}

// submit creates a pending submission through the real user-facing path.
func (p *submissionPortal) submit(t *testing.T, userID uint64, clientRequestID, patternImageURL string) *model.TemplateSubmission {
	t.Helper()
	w := createSubmittableWork(t, userID, patternImageURL)
	sub, err := p.service.Submit(context.Background(), userID, templatesubmission.SubmitInput{
		WorkID:          w.ID,
		Title:           "小猫",
		Description:     "两色拼豆",
		ClientRequestID: clientRequestID,
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return sub
}

func TestAdminPortalSubmissionRoutesRequireAdminToken(t *testing.T) {
	SetupTestDB(t)
	portal := newSubmissionPortal(t)
	sub := portal.submit(t, 7, "req-1", "https://cdn.example.test/work/pattern.png")

	for _, route := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/admin/template-submissions"},
		{http.MethodGet, "/api/v1/admin/template-submissions/1"},
		{http.MethodPost, "/api/v1/admin/template-submissions/1/approve"},
		{http.MethodPost, "/api/v1/admin/template-submissions/1/reject"},
	} {
		response := httptest.NewRecorder()
		portal.handler.ServeHTTP(response, httptest.NewRequest(route.method, route.path, bytes.NewReader([]byte("{}"))))
		if response.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", route.method, route.path, response.Code)
		}
	}

	var reloaded model.TemplateSubmission
	if err := db.DB.First(&reloaded, sub.ID).Error; err != nil {
		t.Fatalf("reload submission: %v", err)
	}
	if reloaded.Status != model.TemplateSubmissionStatusPending {
		t.Errorf("status = %d, want pending after unauthenticated calls", reloaded.Status)
	}
}

func TestAdminPortalListsSubmissionsByStatusWithPaging(t *testing.T) {
	SetupTestDB(t)
	portal := newSubmissionPortal(t)
	first := portal.submit(t, 7, "req-1", "https://cdn.example.test/work/pattern.png")
	second := portal.submit(t, 7, "req-2", "https://cdn.example.test/work/pattern.png")
	if err := portal.service.Reject(context.Background(), first.ID, "operator", "分辨率过低"); err != nil {
		t.Fatalf("Reject: %v", err)
	}

	var result struct {
		Submissions []struct {
			SubmissionID string `json:"submissionId"`
			Status       int    `json:"status"`
			ReviewReason string `json:"reviewReason"`
			PatternData  any    `json:"patternData"`
		} `json:"submissions"`
		Page struct {
			Total    int  `json:"total"`
			Page     int  `json:"page"`
			PageSize int  `json:"pageSize"`
			HasMore  bool `json:"hasMore"`
		} `json:"page"`
	}

	response := portal.do(t, http.MethodGet, "/api/v1/admin/template-submissions?status=pending&page.page=1&page.pageSize=20", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list pending = %d: %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if result.Page.Total != 1 || len(result.Submissions) != 1 {
		t.Fatalf("pending filter returned %#v", result)
	}
	if result.Submissions[0].SubmissionID == "" {
		t.Error("submissionId must be a non-empty string")
	}
	if result.Submissions[0].PatternData != nil {
		t.Error("the list must not carry pattern data")
	}

	response = portal.do(t, http.MethodGet, "/api/v1/admin/template-submissions?status=rejected", nil)
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode rejected list: %v", err)
	}
	if result.Page.Total != 1 || result.Submissions[0].ReviewReason != "分辨率过低" {
		t.Fatalf("rejected filter returned %#v", result)
	}

	response = portal.do(t, http.MethodGet, "/api/v1/admin/template-submissions?page.page=1&page.pageSize=1", nil)
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode unfiltered list: %v", err)
	}
	if result.Page.Total != 2 || !result.Page.HasMore || len(result.Submissions) != 1 {
		t.Fatalf("unfiltered paging returned %#v", result)
	}
	_ = second

	if response = portal.do(t, http.MethodGet, "/api/v1/admin/template-submissions?status=bogus", nil); response.Code != http.StatusBadRequest {
		t.Errorf("unknown status filter = %d, want 400", response.Code)
	}
}

func TestAdminPortalSubmissionDetailReturnsPatternData(t *testing.T) {
	SetupTestDB(t)
	portal := newSubmissionPortal(t)
	sub := portal.submit(t, 7, "req-1", "https://cdn.example.test/work/pattern.png")

	response := portal.do(t, http.MethodGet, "/api/v1/admin/template-submissions/"+strconv.FormatUint(sub.ID, 10), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("detail = %d: %s", response.Code, response.Body.String())
	}
	var result struct {
		Submission struct {
			SubmissionID string `json:"submissionId"`
			UserID       string `json:"userId"`
			WorkID       string `json:"workId"`
			PreviewURL   string `json:"previewUrl"`
			TemplateID   string `json:"templateId"`
			ReviewedAt   int64  `json:"reviewedAt"`
		} `json:"submission"`
		PatternData struct {
			Width  int32   `json:"width"`
			Height int32   `json:"height"`
			Pixels []int32 `json:"pixels"`
		} `json:"patternData"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if result.PatternData.Width != 4 || result.PatternData.Height != 4 || len(result.PatternData.Pixels) != 16 {
		t.Fatalf("pattern data = %#v, want the 4x4 snapshot", result.PatternData)
	}
	if result.Submission.UserID != "7" || result.Submission.WorkID == "" {
		t.Fatalf("submission identifiers = %#v", result.Submission)
	}
	if result.Submission.TemplateID != "" || result.Submission.ReviewedAt != 0 {
		t.Errorf("pending submission must report no template and no review time: %#v", result.Submission)
	}

	if response = portal.do(t, http.MethodGet, "/api/v1/admin/template-submissions/999999", nil); response.Code == http.StatusOK {
		t.Error("unknown submission id must not return 200")
	}
}

func TestAdminPortalApproveCreatesTemplateWithContributorSnapshot(t *testing.T) {
	SetupTestDB(t)
	portal := newSubmissionPortal(t)
	contributor := &model.User{Nickname: "豆豆妈", Status: 1}
	if err := db.DB.Create(contributor).Error; err != nil {
		t.Fatalf("create contributor: %v", err)
	}
	sub := portal.submit(t, contributor.ID, "req-1", "https://cdn.example.test/work/pattern.png")

	response := portal.do(t, http.MethodPost, "/api/v1/admin/template-submissions/"+strconv.FormatUint(sub.ID, 10)+"/approve", map[string]interface{}{
		"categoryId": portal.category.ID,
		"difficulty": 2,
		"tags":       "动物, 入门",
		"title":      "官方小猫",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("approve = %d: %s", response.Code, response.Body.String())
	}
	var result struct {
		TemplateID string `json:"templateId"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode approve: %v", err)
	}
	if result.TemplateID == "" || result.TemplateID == "0" {
		t.Fatalf("templateId = %q", result.TemplateID)
	}

	var published model.Template
	if err := db.DB.Where("title = ?", "官方小猫").First(&published).Error; err != nil {
		t.Fatalf("load published template: %v", err)
	}
	if published.ContributorUserID != contributor.ID || published.ContributorNickname != "豆豆妈" {
		t.Errorf("contributor snapshot = %d/%q, want %d/豆豆妈",
			published.ContributorUserID, published.ContributorNickname, contributor.ID)
	}
	if published.Status != 1 || published.Difficulty != 2 || published.PatternData == nil {
		t.Errorf("published template = %#v", published)
	}
	if published.Width != sub.Width || published.BoardSpec != sub.BoardSpec {
		t.Errorf("published template ignored the snapshot: %dx%d %s", published.Width, published.Height, published.BoardSpec)
	}

	// Renaming later must not rewrite the published signature.
	if err := db.DB.Model(contributor).Update("nickname", "改名了").Error; err != nil {
		t.Fatalf("rename contributor: %v", err)
	}
	if err := db.DB.First(&published, published.ID).Error; err != nil {
		t.Fatalf("reload template: %v", err)
	}
	if published.ContributorNickname != "豆豆妈" {
		t.Errorf("nickname = %q, want the publish-time snapshot", published.ContributorNickname)
	}

	var reloaded model.TemplateSubmission
	if err := db.DB.First(&reloaded, sub.ID).Error; err != nil {
		t.Fatalf("reload submission: %v", err)
	}
	if reloaded.Status != model.TemplateSubmissionStatusApproved || reloaded.TemplateID != published.ID {
		t.Errorf("submission = status %d template %d, want approved/%d", reloaded.Status, reloaded.TemplateID, published.ID)
	}
	if reloaded.ReviewerActor != "operator" || reloaded.ReviewedAt == nil {
		t.Errorf("review audit fields = %q/%v", reloaded.ReviewerActor, reloaded.ReviewedAt)
	}
}

func TestAdminPortalApproveReusesSubmissionPreviewWhenNoFileKey(t *testing.T) {
	SetupTestDB(t)
	portal := newSubmissionPortal(t)
	workImage := "https://cdn.example.test/work/2026/07/15/7/pattern.png"
	sub := portal.submit(t, 7, "req-1", workImage)

	response := portal.do(t, http.MethodPost, "/api/v1/admin/template-submissions/"+strconv.FormatUint(sub.ID, 10)+"/approve", map[string]interface{}{
		"categoryId": portal.category.ID,
		"difficulty": 1,
		"tags":       "动物",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("approve = %d: %s", response.Code, response.Body.String())
	}

	var published model.Template
	if err := db.DB.Order("id DESC").First(&published).Error; err != nil {
		t.Fatalf("load template: %v", err)
	}
	if published.PreviewURL != workImage {
		t.Errorf("previewUrl = %q, want the submitted work image %q", published.PreviewURL, workImage)
	}
	if published.Title != sub.Title {
		t.Errorf("title = %q, want the submission title %q", published.Title, sub.Title)
	}
}

func TestAdminPortalApproveOverridesPreviewWithUploadedFileKey(t *testing.T) {
	SetupTestDB(t)
	portal := newSubmissionPortal(t)
	sub := portal.submit(t, 7, "req-1", "https://cdn.example.test/work/pattern.png")

	previewKey := "admin_preview/2026/07/15/0/preview.png"
	if err := db.DB.Create(&model.MediaAsset{
		UserID:      0,
		FileKey:     previewKey,
		Purpose:     "admin_preview",
		ContentType: "image/png",
		Status:      model.MediaStatusUploaded,
	}).Error; err != nil {
		t.Fatalf("create preview asset: %v", err)
	}

	response := portal.do(t, http.MethodPost, "/api/v1/admin/template-submissions/"+strconv.FormatUint(sub.ID, 10)+"/approve", map[string]interface{}{
		"categoryId":     portal.category.ID,
		"difficulty":     1,
		"tags":           "动物",
		"previewFileKey": previewKey,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("approve = %d: %s", response.Code, response.Body.String())
	}

	var published model.Template
	if err := db.DB.Order("id DESC").First(&published).Error; err != nil {
		t.Fatalf("load template: %v", err)
	}
	if published.PreviewURL != "https://cdn.example.test/"+previewKey {
		t.Errorf("previewUrl = %q, want the reviewer upload", published.PreviewURL)
	}
}

func TestAdminPortalApproveIsIdempotent(t *testing.T) {
	SetupTestDB(t)
	portal := newSubmissionPortal(t)
	sub := portal.submit(t, 7, "req-1", "https://cdn.example.test/work/pattern.png")

	body := map[string]interface{}{"categoryId": portal.category.ID, "difficulty": 1, "tags": "动物"}
	first := portal.do(t, http.MethodPost, "/api/v1/admin/template-submissions/"+strconv.FormatUint(sub.ID, 10)+"/approve", body)
	second := portal.do(t, http.MethodPost, "/api/v1/admin/template-submissions/"+strconv.FormatUint(sub.ID, 10)+"/approve", body)
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("approve codes = %d/%d: %s | %s", first.Code, second.Code, first.Body.String(), second.Body.String())
	}

	decode := func(recorder *httptest.ResponseRecorder) string {
		var result struct {
			TemplateID string `json:"templateId"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode approve: %v", err)
		}
		return result.TemplateID
	}
	if decode(first) != decode(second) {
		t.Errorf("templateId changed between approves: %s vs %s", decode(first), decode(second))
	}

	var templates, records int64
	db.DB.Model(&model.Template{}).Count(&templates)
	db.DB.Model(&model.TemplatePublishRecord{}).Count(&records)
	if templates != 1 || records != 1 {
		t.Errorf("rows = %d templates / %d publish records, want 1 / 1", templates, records)
	}
}

func TestAdminPortalApproveRejectsInactiveCategory(t *testing.T) {
	SetupTestDB(t)
	portal := newSubmissionPortal(t)
	sub := portal.submit(t, 7, "req-1", "https://cdn.example.test/work/pattern.png")
	hidden := &model.TemplateCategory{Name: "已停用"}
	if err := db.DB.Create(hidden).Error; err != nil {
		t.Fatalf("create hidden category: %v", err)
	}
	// Status has a column default of 1, so the zero value cannot be inserted directly.
	if err := db.DB.Model(hidden).Update("status", 0).Error; err != nil {
		t.Fatalf("deactivate category: %v", err)
	}

	response := portal.do(t, http.MethodPost, "/api/v1/admin/template-submissions/"+strconv.FormatUint(sub.ID, 10)+"/approve", map[string]interface{}{
		"categoryId": hidden.ID,
		"difficulty": 1,
		"tags":       "动物",
	})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("approve with inactive category = %d, want 400: %s", response.Code, response.Body.String())
	}

	var templates int64
	db.DB.Model(&model.Template{}).Count(&templates)
	if templates != 0 {
		t.Errorf("template rows = %d, want none", templates)
	}
	var reloaded model.TemplateSubmission
	if err := db.DB.First(&reloaded, sub.ID).Error; err != nil {
		t.Fatalf("reload submission: %v", err)
	}
	if reloaded.Status != model.TemplateSubmissionStatusPending {
		t.Errorf("status = %d, want pending", reloaded.Status)
	}
}

// A publish record whose key already exists but points at a different draft is the
// signature of a key collision. Approve must abort whole rather than leave a
// template without its idempotency record.
func TestAdminPortalApproveDoesNotHalfApplyWhenPublishRecordConflicts(t *testing.T) {
	SetupTestDB(t)
	portal := newSubmissionPortal(t)
	sub := portal.submit(t, 7, "req-1", "https://cdn.example.test/work/pattern.png")

	if err := db.DB.Create(&model.TemplatePublishRecord{
		IdempotencyKey:  "submission-" + strconv.FormatUint(sub.ID, 10),
		TemplateID:      4242,
		DraftRevisionID: sub.ID + 1000,
		Status:          "published",
	}).Error; err != nil {
		t.Fatalf("seed conflicting publish record: %v", err)
	}

	response := portal.do(t, http.MethodPost, "/api/v1/admin/template-submissions/"+strconv.FormatUint(sub.ID, 10)+"/approve", map[string]interface{}{
		"categoryId": portal.category.ID,
		"difficulty": 1,
		"tags":       "动物",
	})
	if response.Code == http.StatusOK {
		t.Fatalf("approve must fail on a conflicting publish record, got %s", response.Body.String())
	}

	var templates int64
	db.DB.Model(&model.Template{}).Count(&templates)
	if templates != 0 {
		t.Errorf("template rows = %d, want none", templates)
	}
	var reloaded model.TemplateSubmission
	if err := db.DB.First(&reloaded, sub.ID).Error; err != nil {
		t.Fatalf("reload submission: %v", err)
	}
	if reloaded.Status != model.TemplateSubmissionStatusPending {
		t.Errorf("status = %d, want pending", reloaded.Status)
	}
}

func TestAdminPortalRejectStoresReasonAndBlocksLaterApprove(t *testing.T) {
	SetupTestDB(t)
	portal := newSubmissionPortal(t)
	sub := portal.submit(t, 7, "req-1", "https://cdn.example.test/work/pattern.png")
	path := "/api/v1/admin/template-submissions/" + strconv.FormatUint(sub.ID, 10) + "/reject"

	if response := portal.do(t, http.MethodPost, path, map[string]interface{}{"reason": "  "}); response.Code != http.StatusBadRequest {
		t.Errorf("empty reason = %d, want 400", response.Code)
	}
	longReason := make([]rune, 201)
	for i := range longReason {
		longReason[i] = '长'
	}
	if response := portal.do(t, http.MethodPost, path, map[string]interface{}{"reason": string(longReason)}); response.Code != http.StatusBadRequest {
		t.Errorf("oversized reason = %d, want 400", response.Code)
	}

	if response := portal.do(t, http.MethodPost, path, map[string]interface{}{"reason": "分辨率过低"}); response.Code != http.StatusOK {
		t.Fatalf("reject = %d: %s", response.Code, response.Body.String())
	}
	var reloaded model.TemplateSubmission
	if err := db.DB.First(&reloaded, sub.ID).Error; err != nil {
		t.Fatalf("reload submission: %v", err)
	}
	if reloaded.Status != model.TemplateSubmissionStatusRejected || reloaded.ReviewReason != "分辨率过低" {
		t.Fatalf("submission = status %d reason %q", reloaded.Status, reloaded.ReviewReason)
	}
	if reloaded.ActiveWorkKey != nil {
		t.Error("active_work_key must be released so the work can be resubmitted")
	}

	if response := portal.do(t, http.MethodPost, path, map[string]interface{}{"reason": "重复驳回"}); response.Code != http.StatusOK {
		t.Errorf("repeated reject = %d, want 200", response.Code)
	}

	approve := portal.do(t, http.MethodPost, "/api/v1/admin/template-submissions/"+strconv.FormatUint(sub.ID, 10)+"/approve", map[string]interface{}{
		"categoryId": portal.category.ID,
		"difficulty": 1,
		"tags":       "动物",
	})
	if approve.Code != http.StatusBadRequest {
		t.Errorf("approving a rejected submission = %d, want 400: %s", approve.Code, approve.Body.String())
	}
}

func TestAdminPortalApproveSurvivesMissingContributorUser(t *testing.T) {
	SetupTestDB(t)
	portal := newSubmissionPortal(t)
	sub := portal.submit(t, 4242, "req-1", "https://cdn.example.test/work/pattern.png")

	response := portal.do(t, http.MethodPost, "/api/v1/admin/template-submissions/"+strconv.FormatUint(sub.ID, 10)+"/approve", map[string]interface{}{
		"categoryId": portal.category.ID,
		"difficulty": 1,
		"tags":       "动物",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("approve = %d: %s", response.Code, response.Body.String())
	}

	var published model.Template
	if err := db.DB.Order("id DESC").First(&published).Error; err != nil {
		t.Fatalf("load template: %v", err)
	}
	if published.ContributorUserID != 4242 || published.ContributorNickname != "" {
		t.Errorf("contributor = %d/%q, want 4242 with an empty nickname",
			published.ContributorUserID, published.ContributorNickname)
	}
}
