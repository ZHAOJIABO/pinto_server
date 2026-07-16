package test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zhaojiabo/bobobeads_server/conf"
	"github.com/zhaojiabo/bobobeads_server/internal/api"
	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	adminauth "github.com/zhaojiabo/bobobeads_server/internal/service/admin"
	"github.com/zhaojiabo/bobobeads_server/internal/service/media"
	templateservice "github.com/zhaojiabo/bobobeads_server/internal/service/template"
	"github.com/zhaojiabo/bobobeads_server/internal/service/work"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestAdminPortalPublishesOnlyAuthenticatedUploadedPattern(t *testing.T) {
	SetupTestDB(t)
	passwordHash, err := adminauth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	previousConfig := conf.GlobalConfig
	conf.GlobalConfig = &conf.Config{
		Pattern: conf.PatternConfig{MaxWidth: 200, MaxHeight: 200, MaxPixels: 40000, MaxColors: 221},
		Admin: conf.AdminConfig{
			JWTSecret: "admin-test-secret",
			Accounts: []conf.AdminAccountConfig{{
				Username:     "operator",
				PasswordHash: passwordHash,
			}},
		},
	}
	t.Cleanup(func() { conf.GlobalConfig = previousConfig })

	category := &model.TemplateCategory{Name: "官方", Status: 1}
	if err := db.DB.Create(category).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
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

	templateDAO := dao.NewTemplateDAO()
	storage := newMemoryObjectStorage("https://cdn.example.test")
	handler := api.NewAdminPortalHTTPHandler(
		adminauth.NewAuthService(conf.GlobalConfig.Admin),
		media.NewServiceWithStorage(dao.NewMediaDAO(), storage),
		templateservice.NewService(templateDAO),
		templateservice.NewAdminService(templateDAO),
	)

	patternData := validPatternData(2, 2)
	patternData.BoardSpec = "2x2"
	patternJSON, err := protojson.Marshal(patternData)
	if err != nil {
		t.Fatalf("marshal pattern data: %v", err)
	}
	publishBody, err := json.Marshal(map[string]interface{}{
		"idempotencyKey": "admin-portal-publish-001",
		"title":          "官方小图纸",
		"categoryId":     category.ID,
		"difficulty":     1,
		"previewFileKey": previewKey,
		"patternData":    json.RawMessage(patternJSON),
	})
	if err != nil {
		t.Fatalf("marshal publish body: %v", err)
	}

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/api/v1/admin/templates", bytes.NewReader(publishBody)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated publish to return 401, got %d", unauthorized.Code)
	}

	accessToken := adminPortalLogin(t, handler, "operator", "correct horse battery staple")
	publish := httptest.NewRecorder()
	publishRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/templates", bytes.NewReader(publishBody))
	publishRequest.Header.Set("Authorization", "Bearer "+accessToken)
	handler.ServeHTTP(publish, publishRequest)
	if publish.Code != http.StatusOK {
		t.Fatalf("expected publish to return 200, got %d: %s", publish.Code, publish.Body.String())
	}

	var result struct {
		TemplateID string `json:"templateId"`
	}
	if err := json.Unmarshal(publish.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode publish response: %v", err)
	}
	if result.TemplateID == "" {
		t.Fatal("expected published template id")
	}

	var template model.Template
	if err := db.DB.WithContext(context.Background()).First(&template).Error; err != nil {
		t.Fatalf("find published template: %v", err)
	}
	if template.PreviewURL != "https://cdn.example.test/"+previewKey {
		t.Fatalf("preview URL must be derived from uploaded asset, got %q", template.PreviewURL)
	}
	if template.Width != 2 || template.Height != 2 || template.ColorCount != 1 {
		t.Fatalf("server must calculate pattern statistics and dimensions, got %dx%d with %d colors", template.Width, template.Height, template.ColorCount)
	}
}

func TestAdminPortalRejectsInvalidPassword(t *testing.T) {
	passwordHash, err := adminauth.HashPassword("correct-password")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	handler := api.NewAdminPortalHTTPHandler(
		adminauth.NewAuthService(conf.AdminConfig{
			JWTSecret: "admin-test-secret",
			Accounts:  []conf.AdminAccountConfig{{Username: "operator", PasswordHash: passwordHash}},
		}),
		media.NewServiceWithStorage(dao.NewMediaDAO(), newMemoryObjectStorage("https://cdn.example.test")),
		templateservice.NewService(dao.NewTemplateDAO()),
		templateservice.NewAdminService(dao.NewTemplateDAO()),
	)

	response := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"username":"operator","password":"wrong-password"}`)
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/admin/login", body))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected invalid password to return 401, got %d", response.Code)
	}
}

func TestAdminPortalUploadsPreviewThroughApplicationServer(t *testing.T) {
	SetupTestDB(t)
	passwordHash, err := adminauth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	previousConfig := conf.GlobalConfig
	conf.GlobalConfig = &conf.Config{
		Admin: conf.AdminConfig{
			JWTSecret: "admin-test-secret",
			Accounts: []conf.AdminAccountConfig{{
				Username:     "operator",
				PasswordHash: passwordHash,
			}},
		},
	}
	t.Cleanup(func() { conf.GlobalConfig = previousConfig })

	templateDAO := dao.NewTemplateDAO()
	storage := newMemoryObjectStorage("https://cdn.example.test")
	handler := api.NewAdminPortalHTTPHandler(
		adminauth.NewAuthService(conf.GlobalConfig.Admin),
		media.NewServiceWithStorage(dao.NewMediaDAO(), storage),
		templateservice.NewService(templateDAO),
		templateservice.NewAdminService(templateDAO),
	)
	accessToken := adminPortalLogin(t, handler, "operator", "correct horse battery staple")

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/media/upload", bytes.NewReader([]byte{1, 2, 3}))
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Content-Type", "image/png")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected upload to return 200, got %d: %s", response.Code, response.Body.String())
	}

	var result struct {
		FileKey string `json:"fileKey"`
		FileURL string `json:"fileUrl"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if result.FileKey == "" || result.FileURL == "" {
		t.Fatalf("expected uploaded preview metadata, got %#v", result)
	}
	if storage.contentType != "image/png" {
		t.Fatalf("storage content type = %q", storage.contentType)
	}
	if !storage.publicRead {
		t.Fatal("official preview must be uploaded with public-read access")
	}
	if !bytes.Equal(storage.objects[result.FileKey], []byte{1, 2, 3}) {
		t.Fatalf("object storage payload = %v", storage.objects[result.FileKey])
	}
	asset, err := dao.NewMediaDAO().GetUploadedAsset(context.Background(), result.FileKey, 0, "admin_preview")
	if err != nil || asset == nil {
		t.Fatalf("expected uploaded media asset, asset=%#v err=%v", asset, err)
	}
}

func TestAdminPortalListsPublishedTemplates(t *testing.T) {
	SetupTestDB(t)
	passwordHash, err := adminauth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	category := &model.TemplateCategory{Name: "动物", Status: 1}
	if err := db.DB.Create(category).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	for _, template := range []*model.Template{
		{
			CategoryID: category.ID, Title: "小狐狸", PreviewURL: "https://cdn.example.test/fox.png",
			Description: "适合入门", PatternData: model.JSONMap{"cells": []string{"A1"}}, Tags: "动物, 入门",
			Difficulty: 1, Width: 29, Height: 29, ColorCount: 8, SortOrder: 0, Status: 1,
		},
		{
			CategoryID: category.ID, Title: "小熊", PreviewURL: "https://cdn.example.test/bear.png",
			Tags: "动物", Difficulty: 2, Width: 30, Height: 30, ColorCount: 9, SortOrder: 1, Status: 1,
		},
	} {
		if err := db.DB.Create(template).Error; err != nil {
			t.Fatalf("create template: %v", err)
		}
	}
	hidden := &model.Template{CategoryID: category.ID, Title: "已下架"}
	if err := db.DB.Create(hidden).Error; err != nil {
		t.Fatalf("create hidden template: %v", err)
	}
	if err := db.DB.Model(hidden).Update("status", 0).Error; err != nil {
		t.Fatalf("hide template: %v", err)
	}

	templateDAO := dao.NewTemplateDAO()
	handler := api.NewAdminPortalHTTPHandler(
		adminauth.NewAuthService(conf.AdminConfig{
			JWTSecret: "admin-test-secret",
			Accounts:  []conf.AdminAccountConfig{{Username: "operator", PasswordHash: passwordHash}},
		}),
		media.NewServiceWithStorage(dao.NewMediaDAO(), newMemoryObjectStorage("https://cdn.example.test")),
		templateservice.NewService(templateDAO),
		templateservice.NewAdminService(templateDAO),
	)
	accessToken := adminPortalLogin(t, handler, "operator", "correct horse battery staple")

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/admin/templates", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated list to return 401, got %d", unauthorized.Code)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/templates?page.page=1&page.pageSize=1", nil)
	request.Header.Set("Authorization", "Bearer "+accessToken)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected list to return 200, got %d: %s", response.Code, response.Body.String())
	}

	var result struct {
		Templates []struct {
			TemplateID   string   `json:"templateId"`
			CategoryID   int      `json:"categoryId"`
			CategoryName string   `json:"categoryName"`
			PreviewURL   string   `json:"previewUrl"`
			ThumbnailURL string   `json:"thumbnailUrl"`
			Tags         []string `json:"tags"`
		} `json:"templates"`
		Page struct {
			Total    int  `json:"total"`
			Page     int  `json:"page"`
			PageSize int  `json:"pageSize"`
			HasMore  bool `json:"hasMore"`
		} `json:"page"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if result.Page.Total != 2 || result.Page.Page != 1 || result.Page.PageSize != 1 || !result.Page.HasMore {
		t.Fatalf("unexpected page metadata: %#v", result.Page)
	}
	if len(result.Templates) != 1 {
		t.Fatalf("expected one first-page template, got %d", len(result.Templates))
	}
	item := result.Templates[0]
	if item.TemplateID == "" || item.CategoryID != category.ID || item.CategoryName != category.Name {
		t.Fatalf("expected template category data, got %#v", item)
	}
	if item.ThumbnailURL != item.PreviewURL {
		t.Fatalf("expected thumbnail fallback to preview URL, got %#v", item)
	}
	if len(item.Tags) == 0 {
		t.Fatalf("expected tags array, got %#v", item.Tags)
	}
	summaries, _, err := templateservice.NewService(templateDAO).ListPublishedTemplates(context.Background(), 1, 1)
	if err != nil || len(summaries) != 1 || summaries[0].PatternData != nil {
		t.Fatalf("template list must not load pattern data, summaries=%#v err=%v", summaries, err)
	}

	secondPage := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/templates?page.page=2&page.pageSize=1", nil)
	secondRequest.Header.Set("Authorization", "Bearer "+accessToken)
	handler.ServeHTTP(secondPage, secondRequest)
	if secondPage.Code != http.StatusOK {
		t.Fatalf("expected second page to return 200, got %d: %s", secondPage.Code, secondPage.Body.String())
	}
	if err := json.Unmarshal(secondPage.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode second-page response: %v", err)
	}
	if len(result.Templates) != 1 || result.Page.HasMore {
		t.Fatalf("unexpected second-page response: %#v", result)
	}

	oversizedPage := httptest.NewRecorder()
	oversizedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/templates?page.pageSize=101", nil)
	oversizedRequest.Header.Set("Authorization", "Bearer "+accessToken)
	handler.ServeHTTP(oversizedPage, oversizedRequest)
	if oversizedPage.Code != http.StatusBadRequest {
		t.Fatalf("expected oversized page to return 400, got %d: %s", oversizedPage.Code, oversizedPage.Body.String())
	}
}

func TestAdminPortalUnpublishValidatesReasonAndIsIdempotent(t *testing.T) {
	SetupTestDB(t)
	passwordHash, err := adminauth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	category := &model.TemplateCategory{Name: "动物", Status: 1}
	if err := db.DB.Create(category).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	template := &model.Template{CategoryID: category.ID, Title: "小狐狸", Status: 1}
	if err := db.DB.Create(template).Error; err != nil {
		t.Fatalf("create template: %v", err)
	}

	templateDAO := dao.NewTemplateDAO()
	handler := api.NewAdminPortalHTTPHandler(
		adminauth.NewAuthService(conf.AdminConfig{
			JWTSecret: "admin-test-secret",
			Accounts:  []conf.AdminAccountConfig{{Username: "operator", PasswordHash: passwordHash}},
		}),
		media.NewServiceWithStorage(dao.NewMediaDAO(), newMemoryObjectStorage("https://cdn.example.test")),
		templateservice.NewService(templateDAO),
		templateservice.NewAdminService(templateDAO),
	)
	accessToken := adminPortalLogin(t, handler, "operator", "correct horse battery staple")

	tooLong := httptest.NewRecorder()
	tooLongRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/templates/"+strconv.FormatUint(template.ID, 10)+"/unpublish", strings.NewReader(`{"reason":"`+strings.Repeat("内", 201)+`"}`))
	tooLongRequest.Header.Set("Authorization", "Bearer "+accessToken)
	handler.ServeHTTP(tooLong, tooLongRequest)
	if tooLong.Code != http.StatusBadRequest {
		t.Fatalf("expected too-long reason to return 400, got %d: %s", tooLong.Code, tooLong.Body.String())
	}
	if err := db.DB.First(template, template.ID).Error; err != nil || template.Status != 1 {
		t.Fatalf("too-long reason must not unpublish template, template=%#v err=%v", template, err)
	}

	notFound := httptest.NewRecorder()
	notFoundRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/templates/999999/unpublish", strings.NewReader(`{}`))
	notFoundRequest.Header.Set("Authorization", "Bearer "+accessToken)
	handler.ServeHTTP(notFound, notFoundRequest)
	if notFound.Code != http.StatusNotFound {
		t.Fatalf("expected unknown template to return 404, got %d: %s", notFound.Code, notFound.Body.String())
	}

	for attempt := 0; attempt < 2; attempt++ {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/templates/"+strconv.FormatUint(template.ID, 10)+"/unpublish", strings.NewReader(`{"reason":"内容需要修订"}`))
		request.Header.Set("Authorization", "Bearer "+accessToken)
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("unpublish attempt %d expected 200, got %d: %s", attempt+1, response.Code, response.Body.String())
		}
	}
	if err := db.DB.First(template, template.ID).Error; err != nil || template.Status != 0 {
		t.Fatalf("expected template to be unpublished, template=%#v err=%v", template, err)
	}
	if _, err := templateservice.NewService(templateDAO).GetTemplate(context.Background(), template.ID); err == nil {
		t.Fatal("unpublished template must not be visible through client template service")
	}
	published, total, err := templateservice.NewService(templateDAO).ListTemplates(context.Background(), templateservice.ListInput{
		Scene: "home", Page: 1, PageSize: 20,
	})
	if err != nil || total != 0 || len(published) != 0 {
		t.Fatalf("unpublished template must not be visible through client list, templates=%#v total=%d err=%v", published, total, err)
	}
}

func TestAdminPortalCreatesTemplateCategory(t *testing.T) {
	SetupTestDB(t)
	passwordHash, err := adminauth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	templateDAO := dao.NewTemplateDAO()
	handler := api.NewAdminPortalHTTPHandler(
		adminauth.NewAuthService(conf.AdminConfig{
			JWTSecret: "admin-test-secret",
			Accounts:  []conf.AdminAccountConfig{{Username: "operator", PasswordHash: passwordHash}},
		}),
		media.NewServiceWithStorage(dao.NewMediaDAO(), newMemoryObjectStorage("https://cdn.example.test")),
		templateservice.NewService(templateDAO),
		templateservice.NewAdminService(templateDAO),
	)

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/api/v1/admin/template-categories", strings.NewReader(`{"name":"节日"}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated category creation to return 401, got %d", unauthorized.Code)
	}

	accessToken := adminPortalLogin(t, handler, "operator", "correct horse battery staple")
	create := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/template-categories", strings.NewReader(`{"name":" 节日 "}`))
	createRequest.Header.Set("Authorization", "Bearer "+accessToken)
	handler.ServeHTTP(create, createRequest)
	if create.Code != http.StatusOK {
		t.Fatalf("expected category creation to return 200, got %d: %s", create.Code, create.Body.String())
	}
	var result struct {
		Category struct {
			CategoryID    int    `json:"categoryId"`
			Name          string `json:"name"`
			TemplateCount int    `json:"templateCount"`
		} `json:"category"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode category response: %v", err)
	}
	if result.Category.CategoryID == 0 || result.Category.Name != "节日" || result.Category.TemplateCount != 0 {
		t.Fatalf("unexpected category response: %#v", result.Category)
	}

	for _, body := range []string{`{"name":"节日"}`, `{"name":"   "}`, `{"name":"` + strings.Repeat("节", 31) + `"}`} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/template-categories", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+accessToken)
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid category request to return 400, got %d: %s", response.Code, response.Body.String())
		}
	}
}

func TestAdminPortalGetsAndUpdatesTemplate(t *testing.T) {
	SetupTestDB(t)
	passwordHash, err := adminauth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	category := &model.TemplateCategory{Name: "动物", Status: 1}
	updatedCategory := &model.TemplateCategory{Name: "节日", Status: 1}
	if err := db.DB.Create(category).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	if err := db.DB.Create(updatedCategory).Error; err != nil {
		t.Fatalf("create updated category: %v", err)
	}
	initialPattern := validPatternData(2, 2)
	initialPattern.BoardSpec = "2x2"
	template := &model.Template{
		CategoryID:   category.ID,
		Title:        "小狐狸",
		Description:  "旧描述",
		PreviewURL:   "https://cdn.example.test/old-preview.png",
		ThumbnailURL: "https://cdn.example.test/old-thumbnail.png",
		Tags:         "动物,入门",
		Difficulty:   1,
		BoardSpec:    "2x2",
		Width:        2,
		Height:       2,
		ColorCount:   1,
		PatternData:  work.PatternDataToJSONMap(initialPattern),
		Status:       1,
	}
	if err := db.DB.Create(template).Error; err != nil {
		t.Fatalf("create template: %v", err)
	}
	previewKey := "admin_preview/2026/07/16/0/updated-preview.png"
	if err := db.DB.Create(&model.MediaAsset{
		UserID:      0,
		FileKey:     previewKey,
		Purpose:     "admin_preview",
		ContentType: "image/png",
		Status:      model.MediaStatusUploaded,
	}).Error; err != nil {
		t.Fatalf("create preview asset: %v", err)
	}

	templateDAO := dao.NewTemplateDAO()
	handler := api.NewAdminPortalHTTPHandler(
		adminauth.NewAuthService(conf.AdminConfig{
			JWTSecret: "admin-test-secret",
			Accounts:  []conf.AdminAccountConfig{{Username: "operator", PasswordHash: passwordHash}},
		}),
		media.NewServiceWithStorage(dao.NewMediaDAO(), newMemoryObjectStorage("https://cdn.example.test")),
		templateservice.NewService(templateDAO),
		templateservice.NewAdminService(templateDAO),
	)
	accessToken := adminPortalLogin(t, handler, "operator", "correct horse battery staple")

	detail := httptest.NewRecorder()
	detailRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/templates/"+strconv.FormatUint(template.ID, 10), nil)
	detailRequest.Header.Set("Authorization", "Bearer "+accessToken)
	handler.ServeHTTP(detail, detailRequest)
	if detail.Code != http.StatusOK {
		t.Fatalf("expected template detail to return 200, got %d: %s", detail.Code, detail.Body.String())
	}
	var detailResult struct {
		Template struct {
			TemplateID string   `json:"templateId"`
			Title      string   `json:"title"`
			CategoryID int      `json:"categoryId"`
			Tags       []string `json:"tags"`
		} `json:"template"`
		PatternData struct {
			Width     int    `json:"width"`
			Height    int    `json:"height"`
			BoardSpec string `json:"boardSpec"`
		} `json:"patternData"`
	}
	if err := json.Unmarshal(detail.Body.Bytes(), &detailResult); err != nil {
		t.Fatalf("decode detail response: %v", err)
	}
	if detailResult.Template.TemplateID != strconv.FormatUint(template.ID, 10) || detailResult.Template.Title != "小狐狸" || detailResult.Template.CategoryID != category.ID || len(detailResult.Template.Tags) != 2 {
		t.Fatalf("unexpected template detail: %#v", detailResult.Template)
	}
	if detailResult.PatternData.Width != 2 || detailResult.PatternData.Height != 2 || detailResult.PatternData.BoardSpec != "2x2" {
		t.Fatalf("unexpected detail pattern data: %#v", detailResult.PatternData)
	}

	updatedPattern := validPatternData(3, 3)
	updatedPattern.BoardSpec = "3x3"
	updatedPatternJSON, err := protojson.Marshal(updatedPattern)
	if err != nil {
		t.Fatalf("marshal update pattern: %v", err)
	}
	invalidBody, err := json.Marshal(map[string]interface{}{
		"title":          "更新后的小狐狸",
		"description":    "新描述",
		"categoryId":     updatedCategory.ID,
		"tags":           "节日,动物",
		"difficulty":     2,
		"previewFileKey": previewKey,
		"patternData":    json.RawMessage(strings.Replace(string(updatedPatternJSON), "3x3", "4x4", 1)),
	})
	if err != nil {
		t.Fatalf("marshal invalid update request: %v", err)
	}
	invalidUpdate := httptest.NewRecorder()
	invalidRequest := httptest.NewRequest(http.MethodPut, "/api/v1/admin/templates/"+strconv.FormatUint(template.ID, 10), bytes.NewReader(invalidBody))
	invalidRequest.Header.Set("Authorization", "Bearer "+accessToken)
	handler.ServeHTTP(invalidUpdate, invalidRequest)
	if invalidUpdate.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid update to return 400, got %d: %s", invalidUpdate.Code, invalidUpdate.Body.String())
	}
	if err := db.DB.First(template, template.ID).Error; err != nil || template.Title != "小狐狸" || template.CategoryID != category.ID {
		t.Fatalf("invalid update must not partially change template, template=%#v err=%v", template, err)
	}

	updateBody, err := json.Marshal(map[string]interface{}{
		"title":          "更新后的小狐狸",
		"description":    "新描述",
		"categoryId":     updatedCategory.ID,
		"tags":           "节日,动物",
		"difficulty":     2,
		"previewFileKey": previewKey,
		"patternData":    json.RawMessage(updatedPatternJSON),
	})
	if err != nil {
		t.Fatalf("marshal update request: %v", err)
	}
	update := httptest.NewRecorder()
	updateRequest := httptest.NewRequest(http.MethodPut, "/api/v1/admin/templates/"+strconv.FormatUint(template.ID, 10), bytes.NewReader(updateBody))
	updateRequest.Header.Set("Authorization", "Bearer "+accessToken)
	handler.ServeHTTP(update, updateRequest)
	if update.Code != http.StatusOK {
		t.Fatalf("expected template update to return 200, got %d: %s", update.Code, update.Body.String())
	}
	unchangedUpdate := httptest.NewRecorder()
	unchangedRequest := httptest.NewRequest(http.MethodPut, "/api/v1/admin/templates/"+strconv.FormatUint(template.ID, 10), bytes.NewReader(updateBody))
	unchangedRequest.Header.Set("Authorization", "Bearer "+accessToken)
	handler.ServeHTTP(unchangedUpdate, unchangedRequest)
	if unchangedUpdate.Code != http.StatusOK {
		t.Fatalf("expected unchanged template update to return 200, got %d: %s", unchangedUpdate.Code, unchangedUpdate.Body.String())
	}
	if err := db.DB.First(template, template.ID).Error; err != nil {
		t.Fatalf("find updated template: %v", err)
	}
	if template.Title != "更新后的小狐狸" || template.Description != "新描述" || template.CategoryID != updatedCategory.ID || template.Tags != "节日,动物" || template.Difficulty != 2 || template.Width != 3 || template.Height != 3 || template.ColorCount != 1 {
		t.Fatalf("template fields were not fully updated: %#v", template)
	}
	wantPreviewURL := "https://cdn.example.test/" + previewKey
	if template.PreviewURL != wantPreviewURL || template.ThumbnailURL != wantPreviewURL {
		t.Fatalf("updated template must contain public preview URLs, got preview=%q thumbnail=%q", template.PreviewURL, template.ThumbnailURL)
	}

	list := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/templates", nil)
	listRequest.Header.Set("Authorization", "Bearer "+accessToken)
	handler.ServeHTTP(list, listRequest)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), wantPreviewURL) {
		t.Fatalf("updated list item must contain browser-accessible preview URL, got %d: %s", list.Code, list.Body.String())
	}
}

func TestAdminPortalListUsesAccessiblePreviewURL(t *testing.T) {
	SetupTestDB(t)
	passwordHash, err := adminauth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	category := &model.TemplateCategory{Name: "动物", Status: 1}
	if err := db.DB.Create(category).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	if err := db.DB.Create(&model.Template{
		CategoryID:   category.ID,
		Title:        "小狐狸",
		PreviewURL:   "admin_preview/raw-object-key.png",
		ThumbnailURL: "/assets/template-preview.png",
		Status:       1,
	}).Error; err != nil {
		t.Fatalf("create template: %v", err)
	}

	templateDAO := dao.NewTemplateDAO()
	handler := api.NewAdminPortalHTTPHandler(
		adminauth.NewAuthService(conf.AdminConfig{
			JWTSecret: "admin-test-secret",
			Accounts:  []conf.AdminAccountConfig{{Username: "operator", PasswordHash: passwordHash}},
		}),
		media.NewServiceWithStorage(dao.NewMediaDAO(), newMemoryObjectStorage("https://cdn.example.test")),
		templateservice.NewService(templateDAO),
		templateservice.NewAdminService(templateDAO),
	)
	accessToken := adminPortalLogin(t, handler, "operator", "correct horse battery staple")
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/templates", nil)
	request.Header.Set("Authorization", "Bearer "+accessToken)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected template list to return 200, got %d: %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "admin_preview/raw-object-key.png") || !strings.Contains(response.Body.String(), "/assets/template-preview.png") {
		t.Fatalf("list must not return raw object keys as preview URLs: %s", response.Body.String())
	}
}

type memoryObjectStorage struct {
	publicBaseURL string
	objects       map[string][]byte
	contentType   string
	publicRead    bool
}

func newMemoryObjectStorage(publicBaseURL string) *memoryObjectStorage {
	return &memoryObjectStorage{
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
		objects:       make(map[string][]byte),
	}
}

func (s *memoryObjectStorage) PresignPut(_ context.Context, fileKey, _ string, expires time.Duration) (*media.PresignedUpload, error) {
	return &media.PresignedUpload{
		URL:       "https://uploads.example.test/" + fileKey,
		Headers:   map[string]string{},
		ExpiresAt: time.Now().Add(expires),
	}, nil
}

func (s *memoryObjectStorage) PutPublic(_ context.Context, fileKey, contentType string, content []byte) error {
	s.contentType = contentType
	s.publicRead = true
	s.objects[fileKey] = append([]byte(nil), content...)
	return nil
}

func (s *memoryObjectStorage) PublicURL(fileKey string) string {
	return s.publicBaseURL + "/" + fileKey
}

func adminPortalLogin(t *testing.T, handler http.Handler, username, password string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		t.Fatalf("marshal login body: %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/admin/login", bytes.NewReader(body)))
	if response.Code != http.StatusOK {
		t.Fatalf("expected successful login, got %d: %s", response.Code, response.Body.String())
	}
	var result struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if result.AccessToken == "" {
		t.Fatal("login response missing access token")
	}
	return result.AccessToken
}
