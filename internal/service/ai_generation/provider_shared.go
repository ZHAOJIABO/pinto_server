package ai_generation

import (
	"net/http"
	"strings"
)

// Adapter ids select which wire protocol a configured model speaks. They are
// referenced from ai_generation.models.<key>.adapter in the YAML.
const (
	AdapterOpenAIImageEdit       = "openai_image_edit"
	AdapterGeminiGenerateContent = "gemini_generate_content"
)

const maxProviderResponseBytes = 64 << 20

// mergeOptions layers the per-style options from bb_ai_style.config over the
// model's YAML defaults, so a style can retune one knob without restating the
// rest.
func mergeOptions(defaults, overrides map[string]string) map[string]string {
	merged := make(map[string]string, len(defaults)+len(overrides))
	for key, value := range defaults {
		merged[key] = value
	}
	for key, value := range overrides {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			merged[key] = trimmed
		}
	}
	return merged
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
