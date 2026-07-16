package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
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
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/template-categories":
		h.withAdmin(w, r, h.createCategory)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/templates":
		h.withAdmin(w, r, h.listTemplates)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/templates":
		h.withAdmin(w, r, h.publishTemplate)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/admin/templates/") && strings.HasSuffix(r.URL.Path, "/unpublish"):
		h.withAdmin(w, r, h.unpublishTemplate)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/admin/templates/"):
		h.withAdmin(w, r, h.getTemplate)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/v1/admin/templates/"):
		h.withAdmin(w, r, h.updateTemplate)
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

func (h *AdminPortalHTTPHandler) createCategory(w http.ResponseWriter, r *http.Request, actor string) {
	var request struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	category, err := h.templateAdmin.CreateCategory(r.Context(), request.Name)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	zap.L().Info("admin template category created", zap.String("actor", actor), zap.Int("category_id", category.ID))
	h.writeSuccess(w, http.StatusOK, map[string]interface{}{
		"category": map[string]interface{}{
			"categoryId":    category.ID,
			"name":          category.Name,
			"templateCount": 0,
		},
	})
}

func (h *AdminPortalHTTPHandler) listTemplates(w http.ResponseWriter, r *http.Request, _ string) {
	page, pageSize, err := adminPage(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	templates, total, err := h.templates.ListPublishedTemplates(r.Context(), page, pageSize)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	categoryIDs := make([]int, 0, len(templates))
	seenCategoryIDs := make(map[int]struct{}, len(templates))
	for _, template := range templates {
		if _, seen := seenCategoryIDs[template.CategoryID]; !seen {
			seenCategoryIDs[template.CategoryID] = struct{}{}
			categoryIDs = append(categoryIDs, template.CategoryID)
		}
	}
	categoryNames, err := h.templates.ListActiveCategoryNames(r.Context(), categoryIDs)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	items := make([]map[string]interface{}, 0, len(templates))
	for _, template := range templates {
		previewURL, thumbnailURL := browserPreviewURLs(template.PreviewURL, template.ThumbnailURL)
		if previewURL == "" {
			h.writeErrorWithCode(w, http.StatusInternalServerError, apperr.CodeInternal, "published template preview URL unavailable")
			return
		}
		tags := h.templates.SplitTags(template.Tags)
		if tags == nil {
			tags = []string{}
		}
		items = append(items, map[string]interface{}{
			"templateId":   fmt.Sprintf("%d", template.ID),
			"title":        template.Title,
			"categoryId":   template.CategoryID,
			"categoryName": categoryNames[template.CategoryID],
			"previewUrl":   previewURL,
			"thumbnailUrl": thumbnailURL,
			"description":  template.Description,
			"tags":         tags,
			"difficulty":   template.Difficulty,
			"width":        template.Width,
			"height":       template.Height,
			"colorCount":   template.ColorCount,
		})
	}

	h.writeSuccess(w, http.StatusOK, map[string]interface{}{
		"templates": items,
		"page": map[string]interface{}{
			"total":    total,
			"page":     page,
			"pageSize": pageSize,
			"hasMore":  int64(page)*int64(pageSize) < total,
		},
	})
}

func (h *AdminPortalHTTPHandler) publishTemplate(w http.ResponseWriter, r *http.Request, actor string) {
	idempotencyKey, payload, err := h.parseTemplatePayload(w, r)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	templateID, err := h.templateAdmin.PublishTemplate(r.Context(), templateservice.PublishPayload{
		IdempotencyKey: idempotencyKey,
		UpdatePayload:  payload,
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	zap.L().Info("admin template published", zap.String("actor", actor), zap.Uint64("template_id", templateID))
	h.writeSuccess(w, http.StatusOK, map[string]interface{}{"templateId": fmt.Sprintf("%d", templateID)})
}

func (h *AdminPortalHTTPHandler) getTemplate(w http.ResponseWriter, r *http.Request, _ string) {
	templateID, err := adminTemplateID(r.URL.Path, "")
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid template id")
		return
	}
	template, err := h.templateAdmin.GetPublishedTemplate(r.Context(), templateID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	previewURL, thumbnailURL := browserPreviewURLs(template.PreviewURL, template.ThumbnailURL)
	if previewURL == "" {
		h.writeErrorWithCode(w, http.StatusInternalServerError, apperr.CodeInternal, "published template preview URL unavailable")
		return
	}
	patternData, err := adminPatternData(template.PatternData)
	if err != nil {
		h.writeErrorWithCode(w, http.StatusInternalServerError, apperr.CodeInternal, "template pattern data unavailable")
		return
	}
	tags := h.templates.SplitTags(template.Tags)
	if tags == nil {
		tags = []string{}
	}
	h.writeSuccess(w, http.StatusOK, map[string]interface{}{
		"template": map[string]interface{}{
			"templateId":   fmt.Sprintf("%d", template.ID),
			"title":        template.Title,
			"categoryId":   template.CategoryID,
			"description":  template.Description,
			"tags":         tags,
			"difficulty":   template.Difficulty,
			"previewUrl":   previewURL,
			"thumbnailUrl": thumbnailURL,
			"boardSpec":    template.BoardSpec,
			"width":        template.Width,
			"height":       template.Height,
			"colorCount":   template.ColorCount,
		},
		"patternData": patternData,
	})
}

func (h *AdminPortalHTTPHandler) updateTemplate(w http.ResponseWriter, r *http.Request, actor string) {
	templateID, err := adminTemplateID(r.URL.Path, "")
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid template id")
		return
	}
	_, payload, err := h.parseTemplatePayload(w, r)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	if err := h.templateAdmin.UpdateTemplate(r.Context(), templateID, payload); err != nil {
		h.writeServiceError(w, err)
		return
	}
	zap.L().Info("admin template updated", zap.String("actor", actor), zap.Uint64("template_id", templateID))
	h.writeSuccess(w, http.StatusOK, map[string]interface{}{"templateId": fmt.Sprintf("%d", templateID)})
}

func (h *AdminPortalHTTPHandler) unpublishTemplate(w http.ResponseWriter, r *http.Request, actor string) {
	templateID, err := adminTemplateID(r.URL.Path, "/unpublish")
	if err != nil {
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
	zap.L().Info("admin template unpublished",
		zap.String("actor", actor),
		zap.Uint64("template_id", templateID),
		zap.String("reason", strings.TrimSpace(request.Reason)),
	)
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
	if errors.Is(err, templateservice.ErrTemplateNotFound) {
		h.writeErrorWithCode(w, http.StatusNotFound, apperr.CodeNotFound, err.Error())
		return
	}
	if appErr, ok := apperr.IsAppError(err); ok {
		h.writeErrorWithCode(w, httpStatusForAppError(appErr.Code), appErr.Code, appErr.Message)
		return
	}
	if errors.Is(err, templateservice.ErrInvalidPayload) ||
		errors.Is(err, templateservice.ErrDuplicateKey) ||
		errors.Is(err, templateservice.ErrUnpublishReason) ||
		errors.Is(err, templateservice.ErrCategoryNameInvalid) ||
		errors.Is(err, templateservice.ErrCategoryNameTaken) {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	zap.L().Error("admin portal request failed", zap.Error(err))
	h.writeErrorWithCode(w, http.StatusInternalServerError, apperr.CodeInternal, "internal server error")
}

func httpStatusForAppError(code int32) int {
	switch code {
	case apperr.CodeUnauthorized, apperr.CodeTokenExpired:
		return http.StatusUnauthorized
	case apperr.CodeForbidden:
		return http.StatusForbidden
	case apperr.CodeNotFound:
		return http.StatusNotFound
	case apperr.CodeInternal:
		return http.StatusInternalServerError
	default:
		return http.StatusBadRequest
	}
}

func (h *AdminPortalHTTPHandler) writeError(w http.ResponseWriter, status int, message string) {
	h.writeErrorWithCode(w, status, apperr.CodeInvalidArgument, message)
}

func (h *AdminPortalHTTPHandler) writeErrorWithCode(w http.ResponseWriter, status int, code int32, message string) {
	h.writeJSON(w, status, map[string]interface{}{
		"header": map[string]interface{}{"code": code, "message": message},
	})
}

func adminPage(r *http.Request) (int, int, error) {
	const maxPageSize = 100

	page, pageSize := 1, maxPageSize
	if value := r.URL.Query().Get("page.page"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			return 0, 0, fmt.Errorf("page.page must be a positive integer")
		}
		page = parsed
	}
	if value := r.URL.Query().Get("page.pageSize"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > maxPageSize {
			return 0, 0, fmt.Errorf("page.pageSize must be between 1 and %d", maxPageSize)
		}
		pageSize = parsed
	}
	maxInt := int(^uint(0) >> 1)
	if page-1 > maxInt/pageSize {
		return 0, 0, fmt.Errorf("page.page is too large")
	}
	return page, pageSize, nil
}

func adminTemplateID(path, suffix string) (uint64, error) {
	value := strings.TrimPrefix(path, "/api/v1/admin/templates/")
	if suffix != "" {
		value = strings.TrimSuffix(value, suffix)
	}
	if value == "" || strings.Contains(value, "/") {
		return 0, fmt.Errorf("invalid template id")
	}
	templateID, err := strconv.ParseUint(value, 10, 64)
	if err != nil || templateID == 0 {
		return 0, fmt.Errorf("invalid template id")
	}
	return templateID, nil
}

func (h *AdminPortalHTTPHandler) parseTemplatePayload(w http.ResponseWriter, r *http.Request) (string, templateservice.UpdatePayload, error) {
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
		return "", templateservice.UpdatePayload{}, apperr.InvalidArgument("invalid request")
	}

	pattern := &workPatternData{PatternData: &pb.PatternData{}}
	if err := protojson.Unmarshal(request.PatternData, pattern.PatternData); err != nil {
		return "", templateservice.UpdatePayload{}, apperr.InvalidArgument("invalid patternData")
	}
	stats, err := work.CalculatePatternStats(pattern.PatternData)
	if err != nil {
		return "", templateservice.UpdatePayload{}, err
	}
	if pattern.PatternData.BoardSpec != fmt.Sprintf("%dx%d", pattern.PatternData.Width, pattern.PatternData.Height) {
		return "", templateservice.UpdatePayload{}, apperr.InvalidArgument("boardSpec must match pattern dimensions")
	}
	previewURL, err := h.media.GetUploadedAdminPreviewURL(r.Context(), request.PreviewFileKey)
	if err != nil {
		return "", templateservice.UpdatePayload{}, err
	}
	if !isBrowserAccessibleURL(previewURL) {
		return "", templateservice.UpdatePayload{}, apperr.Internal("admin preview URL is not browser accessible", nil)
	}

	return request.IdempotencyKey, templateservice.UpdatePayload{
		Title:        strings.TrimSpace(request.Title),
		Description:  strings.TrimSpace(request.Description),
		CategoryID:   request.CategoryID,
		Tags:         strings.TrimSpace(request.Tags),
		Difficulty:   request.Difficulty,
		BoardSpec:    pattern.PatternData.BoardSpec,
		PreviewURL:   previewURL,
		ThumbnailURL: previewURL,
		PatternData:  work.PatternDataToJSONMap(pattern.PatternData),
		Width:        int(pattern.PatternData.Width),
		Height:       int(pattern.PatternData.Height),
		ColorCount:   stats.ColorCount,
		BeadCount:    stats.BeadCount,
	}, nil
}

func browserPreviewURLs(previewURL, thumbnailURL string) (string, string) {
	if !isBrowserAccessibleURL(previewURL) {
		previewURL = ""
	}
	if !isBrowserAccessibleURL(thumbnailURL) {
		thumbnailURL = ""
	}
	if previewURL == "" {
		previewURL = thumbnailURL
	}
	if thumbnailURL == "" {
		thumbnailURL = previewURL
	}
	return previewURL, thumbnailURL
}

func isBrowserAccessibleURL(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "/") || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

func adminPatternData(patternData model.JSONMap) (json.RawMessage, error) {
	pattern, err := work.DecodePatternData(patternData)
	if err != nil || pattern == nil {
		return nil, fmt.Errorf("invalid pattern data")
	}
	encoded, err := protojson.Marshal(pattern)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
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
