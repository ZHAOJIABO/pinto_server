package ai_generation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zhaojiabo/bobobeads_server/conf"
)

const (
	vectorEngineName        = "vectorengine"
	vectorEngineEditPath    = "/v1/images/edits"
	vectorEngineMaxResponse = 64 << 20
)

// VectorEngineProvider talks to the gpt-image-2 image edit endpoint, which
// blocks for roughly 30 seconds and returns the image inline as base64.
type VectorEngineProvider struct {
	baseURL    string
	apiKey     string
	model      string
	size       string
	quality    string
	background string
	moderation string
	client     *http.Client
}

// NewVectorEngineProvider fails fast on a missing key so a misconfigured
// deployment cannot silently accept tasks it can never run.
func NewVectorEngineProvider(cfg conf.VectorEngineConfig, timeout time.Duration) (*VectorEngineProvider, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("vector engine base_url is required")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("vector engine api_key is required")
	}
	if timeout <= 0 {
		timeout = 200 * time.Second
	}
	return &VectorEngineProvider{
		baseURL:    baseURL,
		apiKey:     strings.TrimSpace(cfg.APIKey),
		model:      firstNonEmpty(cfg.Model, "gpt-image-2"),
		size:       cfg.Size,
		quality:    cfg.Quality,
		background: cfg.Background,
		moderation: cfg.Moderation,
		// Only a backstop for a context that fails to fire; the per-attempt
		// deadline is what normally bounds a call.
		client: &http.Client{Timeout: timeout},
	}, nil
}

func (p *VectorEngineProvider) Name() string { return vectorEngineName }

func (p *VectorEngineProvider) Mode() Mode { return ModeSync }

func (p *VectorEngineProvider) Query(_ context.Context, _ string) (*Result, error) {
	return nil, ErrQueryUnsupported
}

func (p *VectorEngineProvider) Submit(ctx context.Context, req *SubmitRequest) (*Result, error) {
	if len(req.InputImage) == 0 {
		return nil, fmt.Errorf("input image is empty")
	}

	body, contentType, err := p.buildMultipartBody(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+vectorEngineEditPath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", contentType)
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		// A transport error before any response means the request may never
		// have reached the model, so let the caller retry unless the read
		// itself timed out (generation may already have been billed).
		return &Result{
			Status:    StatusFailed,
			ErrorCode: "transport_error",
			ErrorMsg:  err.Error(),
			Retryable: isRetryableTransportError(err),
		}, nil
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, vectorEngineMaxResponse))
	if err != nil {
		return &Result{
			Status:    StatusFailed,
			ErrorCode: "read_response_failed",
			ErrorMsg:  err.Error(),
		}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return &Result{
			Status:    StatusFailed,
			ErrorCode: "http_" + strconv.Itoa(resp.StatusCode),
			ErrorMsg:  truncateForStorage(extractVectorEngineError(payload)),
			Retryable: isRetryableStatusCode(resp.StatusCode),
		}, nil
	}

	var parsed vectorEngineResponse
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return &Result{
			Status:    StatusFailed,
			ErrorCode: "invalid_response",
			ErrorMsg:  err.Error(),
		}, nil
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return &Result{
			Status:    StatusFailed,
			ErrorCode: firstNonEmpty(parsed.Error.Code, "provider_error"),
			ErrorMsg:  truncateForStorage(parsed.Error.Message),
		}, nil
	}

	encoded := parsed.firstB64()
	if encoded == "" {
		if url := parsed.firstURL(); url != "" {
			return &Result{Status: StatusSucceeded, OutputURL: url}, nil
		}
		return &Result{
			Status:    StatusFailed,
			ErrorCode: "empty_response",
			ErrorMsg:  "provider returned no image",
		}, nil
	}

	content, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return &Result{
			Status:    StatusFailed,
			ErrorCode: "invalid_base64",
			ErrorMsg:  err.Error(),
		}, nil
	}

	return &Result{
		Status:     StatusSucceeded,
		ImageBytes: content,
		ImageMIME:  outputFormatMIME(parsed.OutputFormat),
	}, nil
}

func (p *VectorEngineProvider) buildMultipartBody(req *SubmitRequest) ([]byte, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	fileName := firstNonEmpty(req.InputName, "input.png")
	part, err := writer.CreateFormFile("image", fileName)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(req.InputImage); err != nil {
		return nil, "", err
	}

	fields := map[string]string{
		"prompt":     req.Prompt,
		"model":      firstNonEmpty(req.ModelName, p.model),
		"n":          "1",
		"size":       firstNonEmpty(req.Options["size"], p.size),
		"quality":    firstNonEmpty(req.Options["quality"], p.quality),
		"background": firstNonEmpty(req.Options["background"], p.background),
		"moderation": firstNonEmpty(req.Options["moderation"], p.moderation),
	}
	for name, value := range fields {
		if value == "" {
			continue
		}
		if err := writer.WriteField(name, value); err != nil {
			return nil, "", err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), writer.FormDataContentType(), nil
}

type vectorEngineResponse struct {
	Created      int64                `json:"created"`
	Data         vectorEngineDataList `json:"data"`
	OutputFormat string               `json:"output_format"`
	Error        *vectorEngineError   `json:"error"`
}

type vectorEngineError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Type    string `json:"type"`
}

type vectorEngineImage struct {
	B64JSON string `json:"b64_json"`
	URL     string `json:"url"`
}

// vectorEngineDataList accepts both the documented single object and the
// OpenAI-style array, because the upstream docs and the reference API disagree.
type vectorEngineDataList []vectorEngineImage

func (d *vectorEngineDataList) UnmarshalJSON(raw []byte) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*d = nil
		return nil
	}
	if trimmed[0] == '[' {
		var items []vectorEngineImage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return err
		}
		*d = items
		return nil
	}
	var single vectorEngineImage
	if err := json.Unmarshal(trimmed, &single); err != nil {
		return err
	}
	*d = []vectorEngineImage{single}
	return nil
}

func (r *vectorEngineResponse) firstB64() string {
	for _, item := range r.Data {
		if item.B64JSON != "" {
			return item.B64JSON
		}
	}
	return ""
}

func (r *vectorEngineResponse) firstURL() string {
	for _, item := range r.Data {
		if item.URL != "" {
			return item.URL
		}
	}
	return ""
}

func extractVectorEngineError(payload []byte) string {
	var parsed vectorEngineResponse
	if err := json.Unmarshal(payload, &parsed); err == nil && parsed.Error != nil && parsed.Error.Message != "" {
		return parsed.Error.Message
	}
	return string(payload)
}

func isRetryableStatusCode(code int) bool {
	switch code {
	case http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// isRetryableTransportError distinguishes "never reached the model" from "the
// response was lost". Retrying the latter risks paying twice for one image.
func isRetryableTransportError(err error) bool {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "context canceled") {
		return false
	}
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "dial tcp") ||
		strings.Contains(msg, "tls handshake") ||
		strings.Contains(msg, "eof")
}

func outputFormatMIME(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "jpeg", "jpg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	default:
		return "image/png"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// truncateForStorage keeps provider messages inside the error_message column.
func truncateForStorage(msg string) string {
	const maxLen = 480
	msg = strings.TrimSpace(msg)
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen]
}
