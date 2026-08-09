package ai_generation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zhaojiabo/bobobeads_server/conf"
)

// GeminiProvider talks to Gemini's native generateContent endpoint, which takes
// the prompt and the input image as inline parts of one JSON body and returns
// the generated image inline as base64. Unlike the OpenAI-compatible image edit
// endpoint it has no separate "edit" route: an image part in the request is what
// turns generation into editing.
type GeminiProvider struct {
	name     string
	baseURL  string
	apiKey   string
	model    string
	defaults map[string]string
	client   *http.Client
}

func NewGeminiProvider(name string, cfg conf.AIModelConfig, timeout time.Duration) (*GeminiProvider, error) {
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
	return &GeminiProvider{
		name:     name,
		baseURL:  baseURL,
		apiKey:   strings.TrimSpace(cfg.APIKey),
		model:    strings.TrimSpace(cfg.Model),
		defaults: cfg.Options,
		client:   &http.Client{Timeout: timeout},
	}, nil
}

func (p *GeminiProvider) Name() string { return p.name }

func (p *GeminiProvider) Mode() Mode { return ModeSync }

func (p *GeminiProvider) Query(_ context.Context, _ string) (*Result, error) {
	return nil, ErrQueryUnsupported
}

func (p *GeminiProvider) Submit(ctx context.Context, req *SubmitRequest) (*Result, error) {
	if len(req.InputImage) == 0 {
		return nil, fmt.Errorf("input image is empty")
	}

	body, err := json.Marshal(p.buildRequest(req))
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint(req), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
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
			ErrorMsg:  truncateForStorage(extractGeminiError(payload)),
			Retryable: isRetryableStatusCode(resp.StatusCode),
		}, nil
	}

	var parsed geminiResponse
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
			ErrorCode: firstNonEmpty(strings.ToLower(parsed.Error.Status), "provider_error"),
			ErrorMsg:  truncateForStorage(parsed.Error.Message),
		}, nil
	}
	// A blocked prompt never reaches the model, and retrying sends the same
	// prompt into the same filter.
	if parsed.PromptFeedback != nil && parsed.PromptFeedback.BlockReason != "" {
		return &Result{
			Status:    StatusFailed,
			ErrorCode: "prompt_blocked",
			ErrorMsg:  truncateForStorage(parsed.PromptFeedback.BlockReason),
		}, nil
	}

	blob, finishReason, text := parsed.firstImage()
	if blob == nil {
		// The model answered with words instead of an image: a refusal or a
		// safety stop. Surface the reason so operations can tell the two apart.
		return &Result{
			Status:    StatusFailed,
			ErrorCode: firstNonEmpty(geminiFailureCode(finishReason), "empty_response"),
			ErrorMsg:  truncateForStorage(firstNonEmpty(text, "provider returned no image")),
		}, nil
	}

	content, err := base64.StdEncoding.DecodeString(blob.Data)
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
		ImageMIME:  firstNonEmpty(blob.mimeType(), "image/png"),
	}, nil
}

// endpoint carries the api key as a query parameter, which is how the vendor
// documents this route.
func (p *GeminiProvider) endpoint(req *SubmitRequest) string {
	model := firstNonEmpty(req.ModelName, p.model)
	return fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s",
		p.baseURL, url.PathEscape(model), url.QueryEscape(p.apiKey))
}

func (p *GeminiProvider) buildRequest(req *SubmitRequest) *geminiRequest {
	options := mergeOptions(p.defaults, req.Options)

	parts := []geminiPart{{Text: req.Prompt}}
	parts = append(parts, geminiPart{InlineData: &geminiInlineData{
		MIMEType: firstNonEmpty(req.InputMIME, "image/png"),
		Data:     base64.StdEncoding.EncodeToString(req.InputImage),
	}})

	generationConfig := &geminiGenerationConfig{ResponseModalities: []string{"IMAGE"}}
	if aspectRatio, imageSize := options["aspect_ratio"], options["image_size"]; aspectRatio != "" || imageSize != "" {
		generationConfig.ImageConfig = &geminiImageConfig{AspectRatio: aspectRatio, ImageSize: imageSize}
	}

	return &geminiRequest{
		Contents:         []geminiContent{{Role: "user", Parts: parts}},
		GenerationConfig: generationConfig,
	}
}

type geminiRequest struct {
	Contents         []geminiContent         `json:"contents"`
	GenerationConfig *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text       string            `json:"text,omitempty"`
	InlineData *geminiInlineData `json:"inline_data,omitempty"`
}

type geminiInlineData struct {
	MIMEType string `json:"mime_type"`
	Data     string `json:"data"`
}

type geminiGenerationConfig struct {
	ResponseModalities []string           `json:"responseModalities,omitempty"`
	ImageConfig        *geminiImageConfig `json:"imageConfig,omitempty"`
}

type geminiImageConfig struct {
	AspectRatio string `json:"aspectRatio,omitempty"`
	ImageSize   string `json:"imageSize,omitempty"`
}

type geminiResponse struct {
	Candidates     []geminiCandidate `json:"candidates"`
	PromptFeedback *struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	Error *geminiError `json:"error"`
}

type geminiCandidate struct {
	Content struct {
		Parts []geminiResponsePart `json:"parts"`
	} `json:"content"`
	FinishReason string `json:"finishReason"`
}

// geminiResponsePart accepts both spellings of the blob field: Google's REST
// API answers in camelCase, but proxies in front of it have been seen echoing
// the snake_case spelling used in the request.
type geminiResponsePart struct {
	Text            string      `json:"text"`
	InlineData      *geminiBlob `json:"inlineData"`
	InlineDataSnake *geminiBlob `json:"inline_data"`
}

func (p *geminiResponsePart) blob() *geminiBlob {
	if p.InlineData != nil && p.InlineData.Data != "" {
		return p.InlineData
	}
	if p.InlineDataSnake != nil && p.InlineDataSnake.Data != "" {
		return p.InlineDataSnake
	}
	return nil
}

type geminiBlob struct {
	MIMETypeCamel string `json:"mimeType"`
	MIMETypeSnake string `json:"mime_type"`
	Data          string `json:"data"`
}

func (b *geminiBlob) mimeType() string {
	return firstNonEmpty(b.MIMETypeCamel, b.MIMETypeSnake)
}

type geminiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// firstImage also returns the finish reason and any text the model produced, so
// a response without an image can be reported with its cause.
func (r *geminiResponse) firstImage() (*geminiBlob, string, string) {
	finishReason := ""
	texts := make([]string, 0, 1)
	for _, candidate := range r.Candidates {
		if finishReason == "" {
			finishReason = candidate.FinishReason
		}
		for i := range candidate.Content.Parts {
			part := &candidate.Content.Parts[i]
			if blob := part.blob(); blob != nil {
				return blob, candidate.FinishReason, ""
			}
			if part.Text != "" {
				texts = append(texts, part.Text)
			}
		}
	}
	return nil, finishReason, strings.Join(texts, " ")
}

// geminiFailureCode turns a non-STOP finish reason into an error code. STOP with
// no image is not a stop reason at all, so it stays empty and the caller falls
// back to empty_response.
func geminiFailureCode(finishReason string) string {
	normalized := strings.ToLower(strings.TrimSpace(finishReason))
	if normalized == "" || normalized == "stop" {
		return ""
	}
	return normalized
}

func extractGeminiError(payload []byte) string {
	var parsed geminiResponse
	if err := json.Unmarshal(payload, &parsed); err == nil && parsed.Error != nil && parsed.Error.Message != "" {
		return parsed.Error.Message
	}
	return string(payload)
}
