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

const openAIImageEditPath = "/v1/images/edits"

// OpenAIImageEditProvider talks to the OpenAI-compatible image edit endpoint
// (gpt-image-2 and friends), which blocks for roughly 30 seconds and returns
// the image inline as base64.
type OpenAIImageEditProvider struct {
	name     string
	baseURL  string
	apiKey   string
	model    string
	defaults map[string]string
	client   *http.Client
}

// NewOpenAIImageEditProvider fails fast on a missing key so a misconfigured
// deployment cannot silently accept tasks it can never run.
func NewOpenAIImageEditProvider(name string, cfg conf.AIModelConfig, timeout time.Duration) (*OpenAIImageEditProvider, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("model %q: base_url is required", name)
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("model %q: api_key is required", name)
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("model %q: model is required", name)
	}
	if timeout <= 0 {
		timeout = 200 * time.Second
	}
	return &OpenAIImageEditProvider{
		name:     name,
		baseURL:  baseURL,
		apiKey:   strings.TrimSpace(cfg.APIKey),
		model:    strings.TrimSpace(cfg.Model),
		defaults: cfg.Options,
		// Only a backstop for a context that fails to fire; the per-attempt
		// deadline is what normally bounds a call.
		client: &http.Client{Timeout: timeout},
	}, nil
}

func (p *OpenAIImageEditProvider) Name() string { return p.name }

func (p *OpenAIImageEditProvider) Mode() Mode { return ModeSync }

func (p *OpenAIImageEditProvider) Query(_ context.Context, _ string) (*Result, error) {
	return nil, ErrQueryUnsupported
}

func (p *OpenAIImageEditProvider) Submit(ctx context.Context, req *SubmitRequest) (*Result, error) {
	if len(req.InputImage) == 0 {
		return nil, fmt.Errorf("input image is empty")
	}

	body, contentType, err := p.buildMultipartBody(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+openAIImageEditPath, bytes.NewReader(body))
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

	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxProviderResponseBytes))
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
			ErrorMsg:  truncateForStorage(extractOpenAIImageError(payload)),
			Retryable: isRetryableStatusCode(resp.StatusCode),
		}, nil
	}

	var parsed openAIImageResponse
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

func (p *OpenAIImageEditProvider) buildMultipartBody(req *SubmitRequest) ([]byte, string, error) {
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
		"prompt": req.Prompt,
		"model":  firstNonEmpty(req.ModelName, p.model),
		"n":      "1",
	}
	// Everything else (size, quality, background, moderation, ...) is passed
	// through verbatim, so supporting one more upstream field is a config
	// change rather than a code change.
	for name, value := range mergeOptions(p.defaults, req.Options) {
		if _, reserved := fields[name]; reserved {
			continue
		}
		fields[name] = value
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

type openAIImageResponse struct {
	Created      int64             `json:"created"`
	Data         openAIImageList   `json:"data"`
	OutputFormat string            `json:"output_format"`
	Error        *openAIImageError `json:"error"`
}

type openAIImageError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Type    string `json:"type"`
}

type openAIImage struct {
	B64JSON string `json:"b64_json"`
	URL     string `json:"url"`
}

// openAIImageList accepts both the documented single object and the
// OpenAI-style array, because the upstream docs and the reference API disagree.
type openAIImageList []openAIImage

func (d *openAIImageList) UnmarshalJSON(raw []byte) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*d = nil
		return nil
	}
	if trimmed[0] == '[' {
		var items []openAIImage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return err
		}
		*d = items
		return nil
	}
	var single openAIImage
	if err := json.Unmarshal(trimmed, &single); err != nil {
		return err
	}
	*d = []openAIImage{single}
	return nil
}

func (r *openAIImageResponse) firstB64() string {
	for _, item := range r.Data {
		if item.B64JSON != "" {
			return item.B64JSON
		}
	}
	return ""
}

func (r *openAIImageResponse) firstURL() string {
	for _, item := range r.Data {
		if item.URL != "" {
			return item.URL
		}
	}
	return ""
}

func extractOpenAIImageError(payload []byte) string {
	var parsed openAIImageResponse
	if err := json.Unmarshal(payload, &parsed); err == nil && parsed.Error != nil && parsed.Error.Message != "" {
		return parsed.Error.Message
	}
	return string(payload)
}
