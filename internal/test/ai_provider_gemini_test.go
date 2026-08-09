package test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zhaojiabo/bobobeads_server/conf"
	"github.com/zhaojiabo/bobobeads_server/internal/service/ai_generation"
)

func newGeminiProvider(t *testing.T, baseURL string) *ai_generation.GeminiProvider {
	t.Helper()
	provider, err := ai_generation.NewGeminiProvider("gemini-3-1-flash-image-preview", conf.AIModelConfig{
		Adapter: ai_generation.AdapterGeminiGenerateContent,
		BaseURL: baseURL,
		APIKey:  "test-key",
		Model:   "gemini-3.1-flash-image-preview",
		Options: map[string]string{"aspect_ratio": "1:1", "image_size": "1K"},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("build provider failed: %v", err)
	}
	return provider
}

func geminiSubmitRequest() *ai_generation.SubmitRequest {
	return &ai_generation.SubmitRequest{
		StyleKey:   "watercolor",
		Prompt:     "make it watercolor",
		InputImage: []byte("original-jpeg-bytes"),
		InputName:  "input.jpg",
		InputMIME:  "image/jpeg",
	}
}

func geminiImageResponse(mimeKey, data string) string {
	return `{"candidates":[{"content":{"parts":[{"` + mimeKey + `":{"mime_type":"image/png","data":"` + data + `"}}]},"finishReason":"STOP"}]}`
}

// The model name goes in the path and the key in the query string: this route has
// no Authorization header, so a wrong shape means a 401 in production.
func TestGeminiProvider_SubmitSendsInlineImage(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("result-png"))

	var gotPath, gotKey, gotAuth string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.URL.Query().Get("key")
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Write([]byte(geminiImageResponse("inlineData", encoded)))
	}))
	defer server.Close()

	result, err := newGeminiProvider(t, server.URL).Submit(context.Background(), geminiSubmitRequest())
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	if gotPath != "/v1beta/models/gemini-3.1-flash-image-preview:generateContent" {
		t.Errorf("path = %q", gotPath)
	}
	if gotKey != "test-key" {
		t.Errorf("key query = %q", gotKey)
	}
	if gotAuth != "" {
		t.Errorf("Authorization must stay empty, got %q", gotAuth)
	}

	parts := body["contents"].([]any)[0].(map[string]any)["parts"].([]any)
	if text := parts[0].(map[string]any)["text"]; text != "make it watercolor" {
		t.Errorf("prompt part = %v", text)
	}
	inline := parts[1].(map[string]any)["inline_data"].(map[string]any)
	if inline["mime_type"] != "image/jpeg" {
		t.Errorf("inline mime = %v", inline["mime_type"])
	}
	if decoded, _ := base64.StdEncoding.DecodeString(inline["data"].(string)); string(decoded) != "original-jpeg-bytes" {
		t.Errorf("inline data = %q", decoded)
	}

	generationConfig := body["generationConfig"].(map[string]any)
	if modalities := generationConfig["responseModalities"].([]any); len(modalities) != 1 || modalities[0] != "IMAGE" {
		t.Errorf("responseModalities = %v", modalities)
	}
	imageConfig := generationConfig["imageConfig"].(map[string]any)
	if imageConfig["aspectRatio"] != "1:1" || imageConfig["imageSize"] != "1K" {
		t.Errorf("imageConfig = %v", imageConfig)
	}

	if result.Status != ai_generation.StatusSucceeded {
		t.Fatalf("status = %v error=%s", result.Status, result.ErrorMsg)
	}
	if string(result.ImageBytes) != "result-png" {
		t.Errorf("decoded image = %q", result.ImageBytes)
	}
	if result.ImageMIME != "image/png" {
		t.Errorf("image mime = %q", result.ImageMIME)
	}
}

// Google answers in camelCase, but proxies in front of it echo the snake_case
// spelling used in the request; both must decode.
func TestGeminiProvider_AcceptsBothBlobSpellings(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("result-png"))
	for _, mimeKey := range []string{"inlineData", "inline_data"} {
		t.Run(mimeKey, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Write([]byte(geminiImageResponse(mimeKey, encoded)))
			}))
			defer server.Close()

			result, err := newGeminiProvider(t, server.URL).Submit(context.Background(), geminiSubmitRequest())
			if err != nil {
				t.Fatalf("Submit failed: %v", err)
			}
			if result.Status != ai_generation.StatusSucceeded {
				t.Fatalf("status = %v error=%s", result.Status, result.ErrorMsg)
			}
			if string(result.ImageBytes) != "result-png" {
				t.Errorf("decoded image = %q", result.ImageBytes)
			}
		})
	}
}

// The style row may ask for a different aspect ratio than the model default.
func TestGeminiProvider_RequestOptionsOverrideDefaults(t *testing.T) {
	var imageConfig map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &body)
		imageConfig = body["generationConfig"].(map[string]any)["imageConfig"].(map[string]any)
		w.Write([]byte(geminiImageResponse("inlineData", base64.StdEncoding.EncodeToString([]byte("x")))))
	}))
	defer server.Close()

	req := geminiSubmitRequest()
	req.Options = map[string]string{"aspect_ratio": "9:16"}

	if _, err := newGeminiProvider(t, server.URL).Submit(context.Background(), req); err != nil {
		t.Fatalf("Submit failed: %v", err)
	}
	if imageConfig["aspectRatio"] != "9:16" {
		t.Errorf("aspectRatio = %v, want the request override", imageConfig["aspectRatio"])
	}
	if imageConfig["imageSize"] != "1K" {
		t.Errorf("imageSize = %v, want the model default", imageConfig["imageSize"])
	}
}

func TestGeminiProvider_ClassifiesFailures(t *testing.T) {
	cases := []struct {
		name          string
		status        int
		body          string
		wantCode      string
		wantRetryable bool
	}{
		{"rate limited", http.StatusTooManyRequests, `{"error":{"code":429,"message":"quota"}}`, "http_429", true},
		{"bad request", http.StatusBadRequest, `{"error":{"code":400,"message":"bad prompt"}}`, "http_400", false},
		// A 200 body carrying an error object is a provider-side rejection.
		{"error in body", http.StatusOK, `{"error":{"message":"boom","status":"INVALID_ARGUMENT"}}`, "invalid_argument", false},
		// A blocked prompt never reaches the model, so retrying is pointless.
		{"prompt blocked", http.StatusOK, `{"promptFeedback":{"blockReason":"SAFETY"}}`, "prompt_blocked", false},
		// Words instead of an image: report the finish reason so a refusal can be
		// told apart from a safety stop.
		{"safety stop", http.StatusOK, `{"candidates":[{"content":{"parts":[{"text":"cannot do that"}]},"finishReason":"IMAGE_SAFETY"}]}`, "image_safety", false},
		{"no image", http.StatusOK, `{"candidates":[{"content":{"parts":[{"text":"here you go"}]},"finishReason":"STOP"}]}`, "empty_response", false},
		{"invalid base64", http.StatusOK, geminiImageResponse("inlineData", "!!!not-base64!!!"), "invalid_base64", false},
		{"garbage", http.StatusOK, `not json at all`, "invalid_response", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer server.Close()

			result, err := newGeminiProvider(t, server.URL).Submit(context.Background(), geminiSubmitRequest())
			if err != nil {
				t.Fatalf("Submit returned a hard error: %v", err)
			}
			if result.Status != ai_generation.StatusFailed {
				t.Fatalf("expected failed status, got %v", result.Status)
			}
			if result.ErrorCode != tc.wantCode {
				t.Errorf("error code = %q, want %q", result.ErrorCode, tc.wantCode)
			}
			if result.Retryable != tc.wantRetryable {
				t.Errorf("retryable = %v, want %v", result.Retryable, tc.wantRetryable)
			}
		})
	}
}

func TestGeminiProvider_RequiresCredentials(t *testing.T) {
	cases := map[string]conf.AIModelConfig{
		"no base_url": {APIKey: "k", Model: "m"},
		"no api_key":  {BaseURL: "https://x", Model: "m"},
		"no model":    {BaseURL: "https://x", APIKey: "k"},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ai_generation.NewGeminiProvider("m", cfg, time.Second); err == nil {
				t.Error("expected a build error")
			}
		})
	}
}

func TestGeminiProvider_QueryUnsupported(t *testing.T) {
	provider := newGeminiProvider(t, "https://api.example.test")
	if provider.Name() != "gemini-3-1-flash-image-preview" {
		t.Errorf("name = %q", provider.Name())
	}
	if provider.Mode() != ai_generation.ModeSync {
		t.Fatalf("mode = %v", provider.Mode())
	}
	if _, err := provider.Query(context.Background(), "job-1"); err != ai_generation.ErrQueryUnsupported {
		t.Fatalf("expected ErrQueryUnsupported, got %v", err)
	}
}
