package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	templateservice "github.com/zhaojiabo/bobobeads_server/internal/service/template"
	"github.com/zhaojiabo/bobobeads_server/internal/service/work"
	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protojson"
)

// 草稿路由的处理函数单独放一个文件，只为不让 admin_portal_http.go 继续膨胀；它们仍然
// 挂在 *AdminPortalHTTPHandler 上，共用同一套鉴权、信封与错误映射。

// draftFieldsRequest 是保存与更新草稿共有的字段集合。decodeJSON 开了
// DisallowUnknownFields，所以这里的字段名就是契约：多一个未知键就是 400。
type draftFieldsRequest struct {
	Title          string          `json:"title"`
	Description    string          `json:"description"`
	CategoryID     int             `json:"categoryId"`
	Tags           string          `json:"tags"`
	Difficulty     int8            `json:"difficulty"`
	PreviewFileKey string          `json:"previewFileKey"`
	PatternData    json.RawMessage `json:"patternData"`
}

func (h *AdminPortalHTTPHandler) createDraft(w http.ResponseWriter, r *http.Request, actor string) {
	var request struct {
		IdempotencyKey string `json:"idempotencyKey"`
		// 字符串而非数字，对齐 §2 示例与其余接口返回 templateId 的惯例。空串表示
		// 这是一份还没上过线的新图纸草稿。
		TemplateID string `json:"templateId"`
		draftFieldsRequest
	}
	if err := decodeJSON(w, r, &request); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	templateID, err := optionalTemplateID(request.TemplateID)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid templateId")
		return
	}
	fields, err := draftFields(request.draftFieldsRequest)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	draft, err := h.drafts.CreateDraft(r.Context(), actor, request.IdempotencyKey, templateID, fields)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	zap.L().Info("admin template draft saved",
		zap.String("actor", actor),
		zap.Uint64("draft_id", draft.ID))
	h.writeSuccess(w, http.StatusOK, map[string]interface{}{
		"draft": draftStampResponse(draft.ID, draft.TemplateID, draft.UpdatedAt),
	})
}

func (h *AdminPortalHTTPHandler) updateDraft(w http.ResponseWriter, r *http.Request, actor string) {
	draftID, err := adminDraftID(r.URL.Path, "")
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid draft id")
		return
	}

	var request struct {
		draftFieldsRequest
		// 前端读到这份草稿时的 updatedAt，乐观锁基线。刻意不接受 templateId：草稿与
		// 已发布模板的关联在创建时确定。
		BaseUpdatedAt string `json:"baseUpdatedAt"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	baseUpdatedAt, err := parseDraftTimestamp(request.BaseUpdatedAt)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	fields, err := draftFields(request.draftFieldsRequest)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	updatedAt, templateID, err := h.drafts.UpdateDraft(r.Context(), actor, draftID, baseUpdatedAt, fields)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	h.writeSuccess(w, http.StatusOK, map[string]interface{}{
		"draft": draftStampResponse(draftID, templateID, updatedAt),
	})
}

func (h *AdminPortalHTTPHandler) listDrafts(w http.ResponseWriter, r *http.Request, _ string) {
	page, pageSize, err := adminPage(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	items, total, err := h.drafts.ListDrafts(r.Context(), page, pageSize)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	drafts := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		// 缩略图是尽力而为的：拿不到就返回空串，前端用 §5 详情里的 patternData 本地
		// 渲染兜底。列表刻意不返回 patternData，理由见 dao.templateDraftListColumns。
		_, thumbnailURL := browserPreviewURLs("", item.ThumbnailURL)
		drafts = append(drafts, map[string]interface{}{
			"draftId":        fmt.Sprintf("%d", item.Draft.ID),
			"templateId":     draftTemplateIDString(item.Draft.TemplateID),
			"title":          item.Draft.Title,
			"categoryId":     item.Draft.CategoryID,
			"categoryName":   item.CategoryName,
			"thumbnailUrl":   thumbnailURL,
			"difficulty":     item.Draft.Difficulty,
			"width":          item.Draft.Width,
			"height":         item.Draft.Height,
			"colorCount":     item.Draft.ColorCount,
			"updatedAt":      formatDraftTimestamp(item.Draft.UpdatedAt),
			"updatedByActor": item.Draft.UpdatedByActor,
		})
	}

	h.writeSuccess(w, http.StatusOK, map[string]interface{}{
		"drafts": drafts,
		"page": map[string]interface{}{
			"total":    total,
			"page":     page,
			"pageSize": pageSize,
			"hasMore":  int64(page)*int64(pageSize) < total,
		},
	})
}

func (h *AdminPortalHTTPHandler) getDraft(w http.ResponseWriter, r *http.Request, _ string) {
	draftID, err := adminDraftID(r.URL.Path, "")
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid draft id")
		return
	}
	draft, err := h.drafts.GetDraft(r.Context(), draftID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	patternData, err := adminPatternData(draft.PatternData)
	if err != nil {
		h.writeErrorWithCode(w, http.StatusInternalServerError, apperr.CodeInternal, "draft pattern data unavailable")
		return
	}
	// tags 在详情里是数组而在请求里是逗号字符串：这个不对称是文档刻意定的，与
	// getTemplate 一致。
	tags := h.templates.SplitTags(draft.Tags)
	if tags == nil {
		tags = []string{}
	}

	h.writeSuccess(w, http.StatusOK, map[string]interface{}{
		"draft": map[string]interface{}{
			"draftId":        fmt.Sprintf("%d", draft.ID),
			"templateId":     draftTemplateIDString(draft.TemplateID),
			"title":          draft.Title,
			"description":    draft.Description,
			"categoryId":     draft.CategoryID,
			"tags":           tags,
			"difficulty":     draft.Difficulty,
			"previewFileKey": draft.PreviewFileKey,
			"updatedAt":      formatDraftTimestamp(draft.UpdatedAt),
			"updatedByActor": draft.UpdatedByActor,
		},
		"patternData": patternData,
	})
}

func (h *AdminPortalHTTPHandler) deleteDraft(w http.ResponseWriter, r *http.Request, actor string) {
	draftID, err := adminDraftID(r.URL.Path, "")
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid draft id")
		return
	}
	// 幂等：删除不存在的草稿也返回 200，前端的「丢弃」按钮不需要处理竞态。
	if err := h.drafts.DeleteDraft(r.Context(), draftID); err != nil {
		h.writeServiceError(w, err)
		return
	}
	zap.L().Info("admin template draft discarded",
		zap.String("actor", actor),
		zap.Uint64("draft_id", draftID))
	h.writeSuccess(w, http.StatusOK, map[string]interface{}{})
}

// publishDraft 把草稿落成线上模板。
//
// 对象存储的往返（GetUploadedAdminPreviewURL + AdminPreviewThumbnailURL）刻意全部在这里
// 做完再进服务层：那是秒级网络 I/O，绝不能发生在发布事务里。代价是发布失败时 P3 生成的
// 缩略图对象会在对象存储里成为孤儿——线上模板仍指向旧预览，业务上无影响。
func (h *AdminPortalHTTPHandler) publishDraft(w http.ResponseWriter, r *http.Request, actor string) {
	draftID, err := adminDraftID(r.URL.Path, "/publish")
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid draft id")
		return
	}

	var request struct {
		IdempotencyKey string `json:"idempotencyKey"`
		PreviewFileKey string `json:"previewFileKey"`
		BaseUpdatedAt  string `json:"baseUpdatedAt"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if strings.TrimSpace(request.IdempotencyKey) == "" {
		h.writeError(w, http.StatusBadRequest, "idempotencyKey is required")
		return
	}
	baseUpdatedAt, err := parseDraftTimestamp(request.BaseUpdatedAt)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	// 这就是 §6「previewFileKey 存在」的落地点：空值时 GetUploadedAdminPreviewURL 自己
	// 返回 Forbidden，不要在这之前另加一个空判断。
	previewFileKey := strings.TrimSpace(request.PreviewFileKey)
	previewURL, err := h.media.GetUploadedAdminPreviewURL(r.Context(), previewFileKey)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	if !isBrowserAccessibleURL(previewURL) {
		h.writeServiceError(w, apperr.Internal("admin preview URL is not browser accessible", nil))
		return
	}
	thumbnailURL := h.media.AdminPreviewThumbnailURL(r.Context(), previewFileKey)

	templateID, err := h.drafts.PublishDraft(r.Context(), draftID, templateservice.PublishDraftInput{
		Actor:          actor,
		IdempotencyKey: strings.TrimSpace(request.IdempotencyKey),
		BaseUpdatedAt:  baseUpdatedAt,
		PreviewFileKey: previewFileKey,
		PreviewURL:     previewURL,
		ThumbnailURL:   thumbnailURL,
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	zap.L().Info("admin template draft published",
		zap.String("actor", actor),
		zap.Uint64("draft_id", draftID),
		zap.Uint64("template_id", templateID))
	h.writeSuccess(w, http.StatusOK, map[string]interface{}{
		"templateId": fmt.Sprintf("%d", templateID),
	})
}

// draftFields 刻意不复用 parseTemplatePayload。后者调 GetUploadedAdminPreviewURL，
// previewFileKey 为空时返回 Forbidden，复用就永远存不下一份还没上传预览图的草稿；
// 同理不复用 AdminService.validateTemplateFields，它要求 title 非空且 categoryId > 0。
// 除 patternData 外所有字段都允许为空是草稿的立命之本，完整校验只在发布时刻执行。
func draftFields(request draftFieldsRequest) (templateservice.DraftFields, error) {
	pattern := &pb.PatternData{}
	if err := protojson.Unmarshal(request.PatternData, pattern); err != nil {
		return templateservice.DraftFields{}, apperr.InvalidArgument("invalid patternData")
	}
	stats, err := work.ValidateDraftPatternStats(pattern)
	if err != nil {
		return templateservice.DraftFields{}, err
	}
	if pattern.BoardSpec != fmt.Sprintf("%dx%d", pattern.Width, pattern.Height) {
		return templateservice.DraftFields{}, apperr.InvalidArgument("boardSpec must match pattern dimensions")
	}

	return templateservice.DraftFields{
		Title:          strings.TrimSpace(request.Title),
		Description:    strings.TrimSpace(request.Description),
		CategoryID:     request.CategoryID,
		Tags:           strings.TrimSpace(request.Tags),
		Difficulty:     request.Difficulty,
		PreviewFileKey: strings.TrimSpace(request.PreviewFileKey),
		PatternData:    work.PatternDataToJSONMap(pattern),
		BoardSpec:      pattern.BoardSpec,
		Width:          int(pattern.Width),
		Height:         int(pattern.Height),
		BeadCount:      stats.BeadCount,
		ColorCount:     stats.ColorCount,
	}, nil
}

func draftStampResponse(draftID uint64, templateID *uint64, updatedAt time.Time) map[string]interface{} {
	return map[string]interface{}{
		"draftId":    fmt.Sprintf("%d", draftID),
		"templateId": draftTemplateIDString(templateID),
		"updatedAt":  formatDraftTimestamp(updatedAt),
	}
}

func draftTemplateIDString(templateID *uint64) string {
	if templateID == nil {
		return ""
	}
	return fmt.Sprintf("%d", *templateID)
}

func optionalTemplateID(value string) (*uint64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil || id == 0 {
		return nil, fmt.Errorf("invalid templateId")
	}
	return &id, nil
}

func adminDraftID(path, suffix string) (uint64, error) {
	return adminPathID(path, "/api/v1/admin/template-drafts/", suffix)
}

// formatDraftTimestamp 与 parseDraftTimestamp 是乐观锁令牌的序列化契约。毫秒精度是
// 承重的：秒级精度会让同一毫秒内的两次更新都判定成功，其中一次静默丢失。
func formatDraftTimestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseDraftTimestamp(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, apperr.InvalidArgument("baseUpdatedAt is required")
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, apperr.InvalidArgument("baseUpdatedAt must be an RFC3339 timestamp")
	}
	// 截断到写入时的同一精度，客户端多带的小数位不至于变成永久冲突。
	return parsed.UTC().Truncate(time.Millisecond), nil
}
