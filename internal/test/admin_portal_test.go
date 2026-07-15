package test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		Pattern: conf.PatternConfig{MaxWidth: 200, MaxHeight: 200, MaxPixels: 40000, MaxColors: 128},
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
