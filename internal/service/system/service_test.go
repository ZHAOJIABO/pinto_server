package system

import (
	"testing"

	"github.com/zhaojiabo/bobobeads_server/conf"
)

func TestExportWatermarkPolicy(t *testing.T) {
	tests := []struct {
		name   string
		config conf.ExportWatermarkConfig
		mode   string
		url    string
	}{
		{
			name:   "marketing",
			config: conf.ExportWatermarkConfig{Mode: "marketing", MarketingURL: "https://cdn.example.com/watermarks/marketing-v1.png"},
			mode:   "marketing",
			url:    "https://cdn.example.com/watermarks/marketing-v1.png",
		},
		{
			name:   "normalized online",
			config: conf.ExportWatermarkConfig{Mode: " ONLINE ", OnlineURL: " https://cdn.example.com/watermarks/online-v1.png "},
			mode:   "online",
			url:    "https://cdn.example.com/watermarks/online-v1.png",
		},
		{
			name:   "invalid mode",
			config: conf.ExportWatermarkConfig{Mode: "unknown", OnlineURL: "https://cdn.example.com/watermarks/online-v1.png"},
			mode:   "none",
		},
		{
			name:   "explicitly disabled",
			config: conf.ExportWatermarkConfig{Mode: "none", OnlineURL: "https://cdn.example.com/watermarks/online-v1.png"},
			mode:   "none",
		},
		{
			name:   "missing selected URL",
			config: conf.ExportWatermarkConfig{Mode: "online"},
			mode:   "none",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := NewService(nil, test.config).GetExportWatermarkPolicy(nil)
			if policy.Mode != test.mode || policy.URL != test.url {
				t.Fatalf("policy = %#v, want mode=%q URL=%q", policy, test.mode, test.url)
			}
		})
	}
}

func TestExportWatermarkPolicyPrefersDatabaseConfig(t *testing.T) {
	policy := NewService(nil, conf.ExportWatermarkConfig{
		Mode:         "online",
		MarketingURL: "https://yaml.example.com/marketing.png",
	}).GetExportWatermarkPolicy(map[string]string{
		"export_watermark_mode":          "marketing",
		"export_watermark_marketing_url": "https://cdn.example.com/marketing-v2.png",
	})

	if policy.Mode != "marketing" || policy.URL != "https://cdn.example.com/marketing-v2.png" {
		t.Fatalf("policy = %#v", policy)
	}
}

func TestExportWatermarkPolicyUsesYAMLURLWhenDatabaseOnlySetsMode(t *testing.T) {
	policy := NewService(nil, conf.ExportWatermarkConfig{
		Mode:         "online",
		MarketingURL: "https://yaml.example.com/marketing.png",
	}).GetExportWatermarkPolicy(map[string]string{
		"export_watermark_mode": "marketing",
	})

	if policy.Mode != "marketing" || policy.URL != "https://yaml.example.com/marketing.png" {
		t.Fatalf("policy = %#v", policy)
	}
}

func TestExportWatermarkPolicyUsesYAMLFallbackForMissingDatabaseKeys(t *testing.T) {
	policy := NewService(nil, conf.ExportWatermarkConfig{
		Mode:      "online",
		OnlineURL: "https://yaml.example.com/online.png",
	}).GetExportWatermarkPolicy(map[string]string{})

	if policy.Mode != "online" || policy.URL != "https://yaml.example.com/online.png" {
		t.Fatalf("policy = %#v", policy)
	}
}

func TestExportWatermarkPolicyDisablesInvalidCDNURL(t *testing.T) {
	policy := NewService(nil, conf.ExportWatermarkConfig{
		Mode:      "online",
		OnlineURL: "not-a-url",
	}).GetExportWatermarkPolicy(nil)

	if policy.Mode != WatermarkModeNone || policy.URL != "" {
		t.Fatalf("policy = %#v", policy)
	}
}
