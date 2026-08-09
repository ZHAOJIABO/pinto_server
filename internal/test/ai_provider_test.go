package test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zhaojiabo/bobobeads_server/conf"
	"github.com/zhaojiabo/bobobeads_server/internal/service/ai_generation"
)

func newImageEditProvider(t *testing.T, baseURL string) *ai_generation.OpenAIImageEditProvider {
	t.Helper()
	provider, err := ai_generation.NewOpenAIImageEditProvider("gpt-image-2", conf.AIModelConfig{
		Adapter: ai_generation.AdapterOpenAIImageEdit,
		BaseURL: baseURL,
		APIKey:  "test-key",
		Model:   "gpt-image-2",
		Options: map[string]string{"size": "1024x1024", "quality": "high"},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("build provider failed: %v", err)
	}
	return provider
}

func imageEditSubmitRequest() *ai_generation.SubmitRequest {
	return &ai_generation.SubmitRequest{
		StyleKey:   "watercolor",
		Prompt:     "make it watercolor",
		InputImage: []byte("original-png-bytes"),
		InputName:  "input.png",
		InputMIME:  "image/png",
	}
}

func TestImageEditProvider_SubmitSendsMultipart(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("result-png"))

	var gotAuth, gotPrompt, gotModel, gotN, gotSize, gotQuality, gotFileName string
	var gotFileBytes []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/edits" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		gotPrompt = r.FormValue("prompt")
		gotModel = r.FormValue("model")
		gotN = r.FormValue("n")
		gotSize = r.FormValue("size")
		gotQuality = r.FormValue("quality")
		file, header, err := r.FormFile("image")
		if err != nil {
			t.Fatalf("read image part: %v", err)
		}
		defer file.Close()
		gotFileName = header.Filename
		gotFileBytes = make([]byte, header.Size)
		file.Read(gotFileBytes)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"created":       1,
			"output_format": "png",
			"data":          map[string]string{"b64_json": encoded},
		})
	}))
	defer server.Close()

	result, err := newImageEditProvider(t, server.URL).Submit(context.Background(), imageEditSubmitRequest())
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization header = %q", gotAuth)
	}
	if gotPrompt != "make it watercolor" {
		t.Errorf("prompt = %q", gotPrompt)
	}
	if gotModel != "gpt-image-2" {
		t.Errorf("model = %q", gotModel)
	}
	if gotN != "1" {
		t.Errorf("n = %q", gotN)
	}
	if gotSize != "1024x1024" || gotQuality != "high" {
		t.Errorf("size/quality = %q/%q", gotSize, gotQuality)
	}
	if gotFileName != "input.png" || string(gotFileBytes) != "original-png-bytes" {
		t.Errorf("image part = %q/%q", gotFileName, gotFileBytes)
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

// Per-request options come from the style row, so they must win over the model's
// YAML defaults while leaving the untouched defaults in place. prompt/model/n are
// owned by the adapter and must not be overridable.
func TestImageEditProvider_RequestOptionsOverrideDefaults(t *testing.T) {
	var gotSize, gotQuality, gotBackground, gotModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		gotSize = r.FormValue("size")
		gotQuality = r.FormValue("quality")
		gotBackground = r.FormValue("background")
		gotModel = r.FormValue("model")
		w.Write([]byte(`{"data":{"b64_json":"` + base64.StdEncoding.EncodeToString([]byte("x")) + `"}}`))
	}))
	defer server.Close()

	req := imageEditSubmitRequest()
	req.Options = map[string]string{"size": "512x512", "background": "transparent", "model": "hijacked"}

	if _, err := newImageEditProvider(t, server.URL).Submit(context.Background(), req); err != nil {
		t.Fatalf("Submit failed: %v", err)
	}
	if gotSize != "512x512" {
		t.Errorf("size = %q, want the request override", gotSize)
	}
	if gotQuality != "high" {
		t.Errorf("quality = %q, want the model default", gotQuality)
	}
	if gotBackground != "transparent" {
		t.Errorf("background = %q", gotBackground)
	}
	if gotModel != "gpt-image-2" {
		t.Errorf("model = %q, options must not override it", gotModel)
	}
}

// The vendor docs show data as an object while the OpenAI-compatible API returns
// an array, so both must decode.
func TestImageEditProvider_AcceptsDataObjectAndArray(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("result-png"))
	cases := map[string]string{
		"object": `{"output_format":"jpeg","data":{"b64_json":"` + encoded + `"}}`,
		"array":  `{"output_format":"jpeg","data":[{"b64_json":"` + encoded + `"}]}`,
	}

	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Write([]byte(payload))
			}))
			defer server.Close()

			result, err := newImageEditProvider(t, server.URL).Submit(context.Background(), imageEditSubmitRequest())
			if err != nil {
				t.Fatalf("Submit failed: %v", err)
			}
			if result.Status != ai_generation.StatusSucceeded {
				t.Fatalf("status = %v error=%s", result.Status, result.ErrorMsg)
			}
			if string(result.ImageBytes) != "result-png" {
				t.Errorf("decoded image = %q", result.ImageBytes)
			}
			if result.ImageMIME != "image/jpeg" {
				t.Errorf("image mime = %q", result.ImageMIME)
			}
		})
	}
}

func TestImageEditProvider_ClassifiesFailures(t *testing.T) {
	cases := []struct {
		name          string
		status        int
		body          string
		wantCode      string
		wantRetryable bool
	}{
		{"rate limited", http.StatusTooManyRequests, `{"error":{"message":"slow down"}}`, "http_429", true},
		{"bad gateway", http.StatusBadGateway, "upstream down", "http_502", true},
		// A 400 means the request itself is wrong; retrying would fail the same way.
		{"bad request", http.StatusBadRequest, `{"error":{"message":"bad prompt"}}`, "http_400", false},
		// Moderation rejections are terminal: the image will never be produced.
		{"moderation", http.StatusOK, `{"error":{"code":"content_policy","message":"rejected"}}`, "content_policy", false},
		{"no image", http.StatusOK, `{"data":[]}`, "empty_response", false},
		{"invalid base64", http.StatusOK, `{"data":{"b64_json":"!!!not-base64!!!"}}`, "invalid_base64", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer server.Close()

			result, err := newImageEditProvider(t, server.URL).Submit(context.Background(), imageEditSubmitRequest())
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

// A read timeout may already have been billed by the provider, so it must not be
// retried even though it looks like a transport failure.
func TestImageEditProvider_ReadTimeoutIsNotRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.Write([]byte(`{"data":{"b64_json":""}}`))
	}))
	defer server.Close()

	provider, err := ai_generation.NewOpenAIImageEditProvider("gpt-image-2", conf.AIModelConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "gpt-image-2",
	}, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("build provider failed: %v", err)
	}

	result, err := provider.Submit(context.Background(), imageEditSubmitRequest())
	if err != nil {
		t.Fatalf("Submit returned a hard error: %v", err)
	}
	if result.Retryable {
		t.Fatal("a timed-out generation must not be retried")
	}
}

// A model missing base_url, api_key or model can never run a task, so it must
// fail at boot instead of accepting and charging tasks.
func TestImageEditProvider_RequiresCredentials(t *testing.T) {
	cases := map[string]conf.AIModelConfig{
		"no base_url": {APIKey: "k", Model: "m"},
		"no api_key":  {BaseURL: "https://x", Model: "m"},
		"no model":    {BaseURL: "https://x", APIKey: "k"},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ai_generation.NewOpenAIImageEditProvider("m", cfg, time.Second); err == nil {
				t.Error("expected a build error")
			}
		})
	}
}

func TestImageEditProvider_QueryUnsupported(t *testing.T) {
	provider := newImageEditProvider(t, "https://api.example.test")
	if provider.Name() != "gpt-image-2" {
		t.Errorf("name = %q", provider.Name())
	}
	if provider.Mode() != ai_generation.ModeSync {
		t.Fatalf("mode = %v", provider.Mode())
	}
	if _, err := provider.Query(context.Background(), "job-1"); err != ai_generation.ErrQueryUnsupported {
		t.Fatalf("expected ErrQueryUnsupported, got %v", err)
	}
}
