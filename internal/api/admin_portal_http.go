package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	adminauth "github.com/zhaojiabo/bobobeads_server/internal/service/admin"
	"github.com/zhaojiabo/bobobeads_server/internal/service/media"
	templateservice "github.com/zhaojiabo/bobobeads_server/internal/service/template"
	"github.com/zhaojiabo/bobobeads_server/internal/service/work"
	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protojson"
)

// AdminPortalHTTPHandler is intentionally separate from the gRPC Gateway.
// Its routes accept only an administrator token, while AdminTemplateService
// remains reserved for service-to-service callers.
type AdminPortalHTTPHandler struct {
	auth          *adminauth.AuthService
	media         *media.Service
	templates     *templateservice.Service
	templateAdmin *templateservice.AdminService
}

func NewAdminPortalHTTPHandler(
	auth *adminauth.AuthService,
	mediaService *media.Service,
	templateService *templateservice.Service,
	templateAdmin *templateservice.AdminService,
) *AdminPortalHTTPHandler {
	return &AdminPortalHTTPHandler{
		auth:          auth,
		media:         mediaService,
		templates:     templateService,
		templateAdmin: templateAdmin,
	}
}

func (h *AdminPortalHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/login":
		h.login(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/media/upload-token":
		h.withAdmin(w, r, h.createPreviewUpload)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/media/upload":
		h.withAdmin(w, r, h.uploadPreview)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/media/report-upload":
		h.withAdmin(w, r, h.reportPreviewUpload)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/template-categories":
		h.withAdmin(w, r, h.listCategories)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/templates":
		h.withAdmin(w, r, h.publishTemplate)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/admin/templates/") && strings.HasSuffix(r.URL.Path, "/unpublish"):
		h.withAdmin(w, r, h.unpublishTemplate)
	default:
		h.writeError(w, http.StatusNotFound, "route not found")
	}
}

func (h *AdminPortalHTTPHandler) login(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	token, err := h.auth.Login(request.Username, request.Password)
	if err != nil {
		if errors.Is(err, adminauth.ErrLoginLocked) {
			h.writeError(w, http.StatusTooManyRequests, "too many failed login attempts")
			return
		}
		h.writeError(w, http.StatusUnauthorized, "invalid administrator credentials")
		return
	}
	h.writeSuccess(w, http.StatusOK, map[string]interface{}{
		"accessToken": token.AccessToken,
		"expiresIn":   token.ExpiresIn,
	})
}

func (h *AdminPortalHTTPHandler) createPreviewUpload(w http.ResponseWriter, r *http.Request, actor string) {
	var request struct {
		FileName    string `json:"fileName"`
		ContentType string `json:"contentType"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	token, err := h.media.GetAdminPreviewUploadToken(r.Context(), request.FileName, request.ContentType)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	zap.L().Info("admin preview upload initialized", zap.String("actor", actor), zap.String("file_key", token.FileKey))
	h.writeSuccess(w, http.StatusOK, map[string]interface{}{
		"uploadUrl":    token.UploadURL,
		"fileKey":      token.FileKey,
		"headers":      token.Headers,
		"expiresAt":    token.ExpiresAt,
		"uploadMethod": token.UploadMethod,
		"maxFileSize":  token.MaxFileSize,
	})
}

func (h *AdminPortalHTTPHandler) reportPreviewUpload(w http.ResponseWriter, r *http.Request, actor string) {
	var request struct {
		FileKey  string `json:"fileKey"`
		FileSize int64  `json:"fileSize"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	fileURL, err := h.media.ReportAdminPreviewUpload(r.Context(), request.FileKey, request.FileSize)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	zap.L().Info("admin preview upload reported", zap.String("actor", actor), zap.String("file_key", request.FileKey))
	h.writeSuccess(w, http.StatusOK, map[string]interface{}{"fileUrl": fileURL})
}

func (h *AdminPortalHTTPHandler) uploadPreview(w http.ResponseWriter, r *http.Request, actor string) {
	contentType := strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])
	r.Body = http.MaxBytesReader(w, r.Body, media.AdminPreviewMaxFileSize+1)
	content, err := io.ReadAll(r.Body)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "preview image is too large")
		return
	}
	fileKey, fileURL, err := h.media.UploadAdminPreview(r.Context(), contentType, content)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	zap.L().Info("admin preview uploaded", zap.String("actor", actor), zap.String("file_key", fileKey))
	h.writeSuccess(w, http.StatusOK, map[string]interface{}{
		"fileKey": fileKey,
		"fileUrl": fileURL,
	})
}

func (h *AdminPortalHTTPHandler) listCategories(w http.ResponseWriter, r *http.Request, _ string) {
	categories, counts, err := h.templates.ListCategories(r.Context())
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	items := make([]map[string]interface{}, 0, len(categories))
	for index, category := range categories {
		items = append(items, map[string]interface{}{
			"categoryId":    category.ID,
			"name":          category.Name,
			"templateCount": counts[index],
		})
	}
	h.writeSuccess(w, http.StatusOK, map[string]interface{}{"categories": items})
}

func (h *AdminPortalHTTPHandler) publishTemplate(w http.ResponseWriter, r *http.Request, actor string) {
	var request struct {
		IdempotencyKey string          `json:"idempotencyKey"`
		Title          string          `json:"title"`
		Description    string          `json:"description"`
		CategoryID     int             `json:"categoryId"`
		Tags           string          `json:"tags"`
		Difficulty     int8            `json:"difficulty"`
		PreviewFileKey string          `json:"previewFileKey"`
		PatternData    json.RawMessage `json:"patternData"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	pattern := &workPatternData{PatternData: &pb.PatternData{}}
	if err := protojson.Unmarshal(request.PatternData, pattern.PatternData); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid patternData")
		return
	}
	stats, err := work.CalculatePatternStats(pattern.PatternData)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	if pattern.PatternData.BoardSpec != fmt.Sprintf("%dx%d", pattern.PatternData.Width, pattern.PatternData.Height) {
		h.writeError(w, http.StatusBadRequest, "boardSpec must match pattern dimensions")
		return
	}
	previewURL, err := h.media.GetUploadedAdminPreviewURL(r.Context(), request.PreviewFileKey)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	templateID, err := h.templateAdmin.PublishTemplate(r.Context(), templateservice.PublishPayload{
		IdempotencyKey: request.IdempotencyKey,
		Title:          strings.TrimSpace(request.Title),
		Description:    strings.TrimSpace(request.Description),
		CategoryID:     request.CategoryID,
		Tags:           strings.TrimSpace(request.Tags),
		Difficulty:     request.Difficulty,
		BoardSpec:      pattern.PatternData.BoardSpec,
		PreviewURL:     previewURL,
		ThumbnailURL:   previewURL,
		PatternData:    work.PatternDataToJSONMap(pattern.PatternData),
		Width:          int(pattern.PatternData.Width),
		Height:         int(pattern.PatternData.Height),
		ColorCount:     stats.ColorCount,
		BeadCount:      stats.BeadCount,
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	zap.L().Info("admin template published", zap.String("actor", actor), zap.Uint64("template_id", templateID))
	h.writeSuccess(w, http.StatusOK, map[string]interface{}{"templateId": fmt.Sprintf("%d", templateID)})
}

func (h *AdminPortalHTTPHandler) unpublishTemplate(w http.ResponseWriter, r *http.Request, actor string) {
	path := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/admin/templates/"), "/unpublish")
	templateID, err := strconv.ParseUint(strings.Trim(path, "/"), 10, 64)
	if err != nil || templateID == 0 {
		h.writeError(w, http.StatusBadRequest, "invalid template id")
		return
	}
	var request struct {
		Reason string `json:"reason"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := h.templateAdmin.UnpublishTemplate(r.Context(), templateID, strings.TrimSpace(request.Reason)); err != nil {
		h.writeServiceError(w, err)
		return
	}
	zap.L().Info("admin template unpublished", zap.String("actor", actor), zap.Uint64("template_id", templateID))
	h.writeSuccess(w, http.StatusOK, map[string]interface{}{})
}

func (h *AdminPortalHTTPHandler) withAdmin(w http.ResponseWriter, r *http.Request, next func(http.ResponseWriter, *http.Request, string)) {
	actor, err := h.auth.ValidateAccessToken(bearerToken(r.Header.Get("Authorization")))
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "administrator authentication required")
		return
	}
	next(w, r, actor)
}

func (h *AdminPortalHTTPHandler) writeSuccess(w http.ResponseWriter, status int, body map[string]interface{}) {
	body["header"] = map[string]interface{}{"code": 0, "message": "success"}
	h.writeJSON(w, status, body)
}

func (h *AdminPortalHTTPHandler) writeServiceError(w http.ResponseWriter, err error) {
	h.writeError(w, http.StatusBadRequest, err.Error())
}

func (h *AdminPortalHTTPHandler) writeError(w http.ResponseWriter, status int, message string) {
	h.writeJSON(w, status, map[string]interface{}{
		"header": map[string]interface{}{"code": 1101, "message": message},
	})
}

func (h *AdminPortalHTTPHandler) writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func bearerToken(value string) string {
	return strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
}

// workPatternData avoids exposing protobuf serialization details in every
// handler request struct while retaining protojson's exact REST field contract.
type workPatternData struct {
	PatternData *pb.PatternData
}
