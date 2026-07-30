package system

import (
	"context"
	"net/url"
	"strings"

	"github.com/zhaojiabo/bobobeads_server/conf"
	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
)

const (
	WatermarkModeNone      = "none"
	WatermarkModeMarketing = "marketing"
	WatermarkModeOnline    = "online"

	ExportWatermarkModeConfigKey                = "export_watermark_mode"
	ExportWatermarkMarketingURLConfigKey        = "export_watermark_marketing_url"
	ExportWatermarkOnlineURLConfigKey           = "export_watermark_online_url"
	ExportWatermarkURLConfigKey                 = "export_watermark_url"
	LegacyExportWatermarkPublicBaseURLConfigKey = "export_watermark_public_base_url"
)

type Service struct {
	systemDAO       *dao.SystemDAO
	exportWatermark conf.ExportWatermarkConfig
}

func NewService(systemDAO *dao.SystemDAO, exportWatermark conf.ExportWatermarkConfig) *Service {
	return &Service{systemDAO: systemDAO, exportWatermark: exportWatermark}
}

type ExportWatermarkPolicy struct {
	Mode string
	URL  string
}

func (s *Service) GetAppConfig(ctx context.Context) (map[string]string, error) {
	return s.systemDAO.GetAllConfigs(ctx)
}

// GetExportWatermarkPolicy returns the client-facing export policy. Database
// values take precedence over YAML so operators can switch the watermark
// without restarting the service. The selected URL is a complete CDN URL;
// invalid values fail closed.
func (s *Service) GetExportWatermarkPolicy(configs map[string]string) ExportWatermarkPolicy {
	mode := s.exportWatermark.Mode
	if databaseMode, ok := configs[ExportWatermarkModeConfigKey]; ok {
		mode = databaseMode
	}

	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != WatermarkModeMarketing && mode != WatermarkModeOnline {
		return ExportWatermarkPolicy{Mode: WatermarkModeNone}
	}

	watermarkURL, databaseURLConfigKey := s.exportWatermarkURLSource(mode)
	if databaseURL, ok := configs[databaseURLConfigKey]; ok {
		watermarkURL = databaseURL
	}
	watermarkURL, ok := normalizeWatermarkURL(watermarkURL)
	if !ok {
		return ExportWatermarkPolicy{Mode: WatermarkModeNone}
	}
	return ExportWatermarkPolicy{
		Mode: mode,
		URL:  watermarkURL,
	}
}

func (s *Service) exportWatermarkURLSource(mode string) (string, string) {
	switch mode {
	case WatermarkModeMarketing:
		return s.exportWatermark.MarketingURL, ExportWatermarkMarketingURLConfigKey
	case WatermarkModeOnline:
		return s.exportWatermark.OnlineURL, ExportWatermarkOnlineURLConfigKey
	default:
		return "", ""
	}
}

func normalizeWatermarkURL(rawURL string) (string, bool) {
	parsedURL, err := url.ParseRequestURI(strings.TrimSpace(rawURL))
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" || parsedURL.User != nil {
		return "", false
	}
	return parsedURL.String(), true
}

func (s *Service) GetBeadColors(ctx context.Context, brand string) ([]*model.BeadColor, error) {
	return s.systemDAO.ListBeadColors(ctx, brand)
}

func (s *Service) GetBoardSpecs(ctx context.Context) ([]*model.BoardSpec, error) {
	return s.systemDAO.ListBoardSpecs(ctx)
}
