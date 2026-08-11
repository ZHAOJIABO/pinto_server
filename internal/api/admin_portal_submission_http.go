package api

import (
	"fmt"
	"net/http"
	"strings"

	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/service/templatesubmission"
	"go.uber.org/zap"
)

const submissionPathPrefix = "/api/v1/admin/template-submissions/"

func (h *AdminPortalHTTPHandler) listSubmissions(w http.ResponseWriter, r *http.Request, _ string) {
	page, pageSize, err := adminPage(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	status, err := submissionStatusFilter(r.URL.Query().Get("status"))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	submissions, total, err := h.submissions.ListForAdmin(r.Context(), status, (page-1)*pageSize, pageSize)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	items := make([]map[string]interface{}, 0, len(submissions))
	for _, sub := range submissions {
		items = append(items, submissionJSON(sub))
	}
	h.writeSuccess(w, http.StatusOK, map[string]interface{}{
		"submissions": items,
		"page": map[string]interface{}{
			"total":    total,
			"page":     page,
			"pageSize": pageSize,
			"hasMore":  int64(page)*int64(pageSize) < total,
		},
	})
}

func (h *AdminPortalHTTPHandler) getSubmission(w http.ResponseWriter, r *http.Request, _ string) {
	id, err := adminPathID(r.URL.Path, submissionPathPrefix, "")
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid submission id")
		return
	}
	sub, err := h.submissions.GetForAdmin(r.Context(), id)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	patternData, err := adminPatternData(sub.PatternData)
	if err != nil {
		h.writeErrorWithCode(w, http.StatusInternalServerError, apperr.CodeInternal, "submission pattern data unavailable")
		return
	}
	h.writeSuccess(w, http.StatusOK, map[string]interface{}{
		"submission":  submissionJSON(sub),
		"patternData": patternData,
	})
}

func (h *AdminPortalHTTPHandler) approveSubmission(w http.ResponseWriter, r *http.Request, actor string) {
	id, err := adminPathID(r.URL.Path, submissionPathPrefix, "/approve")
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid submission id")
		return
	}
	var request struct {
		CategoryID     int    `json:"categoryId"`
		Difficulty     int8   `json:"difficulty"`
		Tags           string `json:"tags"`
		Title          string `json:"title"`
		Description    string `json:"description"`
		PreviewFileKey string `json:"previewFileKey"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	in := templatesubmission.ApproveInput{
		CategoryID:  request.CategoryID,
		Difficulty:  request.Difficulty,
		Tags:        strings.TrimSpace(request.Tags),
		Title:       strings.TrimSpace(request.Title),
		Description: strings.TrimSpace(request.Description),
	}
	// Only override the snapshot's preview when the reviewer actually uploaded one.
	if strings.TrimSpace(request.PreviewFileKey) != "" {
		previewURL, err := h.media.GetUploadedAdminPreviewURL(r.Context(), request.PreviewFileKey)
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		if !isBrowserAccessibleURL(previewURL) {
			h.writeErrorWithCode(w, http.StatusInternalServerError, apperr.CodeInternal, "admin preview URL is not browser accessible")
			return
		}
		in.PreviewURL = previewURL
		in.ThumbnailURL = h.media.AdminPreviewThumbnailURL(r.Context(), request.PreviewFileKey)
	}

	templateID, err := h.submissions.Approve(r.Context(), id, actor, in)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	zap.L().Info("admin template submission approved",
		zap.String("actor", actor),
		zap.Uint64("submission_id", id),
		zap.Uint64("template_id", templateID))
	h.writeSuccess(w, http.StatusOK, map[string]interface{}{"templateId": fmt.Sprintf("%d", templateID)})
}

func (h *AdminPortalHTTPHandler) rejectSubmission(w http.ResponseWriter, r *http.Request, actor string) {
	id, err := adminPathID(r.URL.Path, submissionPathPrefix, "/reject")
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid submission id")
		return
	}
	var request struct {
		Reason string `json:"reason"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := h.submissions.Reject(r.Context(), id, actor, request.Reason); err != nil {
		h.writeServiceError(w, err)
		return
	}
	zap.L().Info("admin template submission rejected",
		zap.String("actor", actor),
		zap.Uint64("submission_id", id),
		zap.String("reason", strings.TrimSpace(request.Reason)))
	h.writeSuccess(w, http.StatusOK, map[string]interface{}{})
}

func submissionStatusFilter(value string) (*int8, error) {
	switch value {
	case "":
		return nil, nil
	case "pending":
		status := model.TemplateSubmissionStatusPending
		return &status, nil
	case "approved":
		status := model.TemplateSubmissionStatusApproved
		return &status, nil
	case "rejected":
		status := model.TemplateSubmissionStatusRejected
		return &status, nil
	default:
		return nil, fmt.Errorf("status must be one of pending, approved, rejected")
	}
}

// submissionJSON deliberately tolerates an empty preview URL, unlike the
// published-template handlers: a contributor's work image may live outside our
// bucket, and the reviewer can still judge the submission from patternData.
func submissionJSON(sub *model.TemplateSubmission) map[string]interface{} {
	previewURL, thumbnailURL := browserPreviewURLs(sub.PreviewURL, sub.ThumbnailURL)
	templateID := ""
	if sub.TemplateID > 0 {
		templateID = fmt.Sprintf("%d", sub.TemplateID)
	}
	reviewedAt := int64(0)
	if sub.ReviewedAt != nil {
		reviewedAt = sub.ReviewedAt.Unix()
	}
	return map[string]interface{}{
		"submissionId":  fmt.Sprintf("%d", sub.ID),
		"userId":        fmt.Sprintf("%d", sub.UserID),
		"workId":        fmt.Sprintf("%d", sub.WorkID),
		"title":         sub.Title,
		"description":   sub.Description,
		"status":        sub.Status,
		"reviewReason":  sub.ReviewReason,
		"reviewerActor": sub.ReviewerActor,
		"templateId":    templateID,
		"boardSpec":     sub.BoardSpec,
		"width":         sub.Width,
		"height":        sub.Height,
		"beadCount":     sub.BeadCount,
		"colorCount":    sub.ColorCount,
		"previewUrl":    previewURL,
		"thumbnailUrl":  thumbnailURL,
		"createdAt":     sub.CreatedAt.Unix(),
		"reviewedAt":    reviewedAt,
	}
}
