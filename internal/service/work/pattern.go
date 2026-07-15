package work

import (
	"regexp"
	"strings"

	"github.com/zhaojiabo/bobobeads_server/conf"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
)

const currentPatternSchemaVersion int32 = 1

var patternHex = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

type PatternStats struct {
	BeadCount  int
	ColorCount int
}

func ValidatePatternData(p *pb.PatternData) error {
	_, err := CalculatePatternStats(p)
	return err
}

func CalculatePatternStats(p *pb.PatternData) (PatternStats, error) {
	if p == nil {
		return PatternStats{}, apperr.InvalidArgument("pattern_data required")
	}

	cfg := conf.GlobalConfig.Pattern
	maxWidth := cfg.MaxWidth
	maxHeight := cfg.MaxHeight
	maxPixels := cfg.MaxPixels
	maxColors := cfg.MaxColors
	if maxWidth == 0 {
		maxWidth = 200
	}
	if maxHeight == 0 {
		maxHeight = 200
	}
	if maxPixels == 0 {
		maxPixels = 40000
	}
	if maxColors == 0 {
		maxColors = 128
	}

	if p.Width <= 0 || p.Height <= 0 {
		return PatternStats{}, apperr.InvalidArgument("pattern width and height must be positive")
	}
	if int(p.Width) > maxWidth || int(p.Height) > maxHeight {
		return PatternStats{}, apperr.InvalidArgument("pattern dimensions exceed maximum")
	}
	totalPixels := int(p.Width) * int(p.Height)
	if totalPixels > maxPixels {
		return PatternStats{}, apperr.InvalidArgument("pattern pixel count exceeds maximum")
	}
	if strings.TrimSpace(p.BoardSpec) == "" {
		return PatternStats{}, apperr.InvalidArgument("pattern board_spec required")
	}
	if p.SchemaVersion != currentPatternSchemaVersion {
		return PatternStats{}, apperr.InvalidArgument("unsupported pattern schema_version")
	}
	if len(p.Pixels) != totalPixels {
		return PatternStats{}, apperr.InvalidArgument("pixels length must equal width * height")
	}

	if len(p.ColorPalette) == 0 {
		return PatternStats{}, apperr.InvalidArgument("color_palette is required")
	}
	if len(p.ColorPalette) > maxColors {
		return PatternStats{}, apperr.InvalidArgument("color_palette exceeds maximum colors")
	}

	paletteIndexes := make(map[int32]struct{}, len(p.ColorPalette))
	for _, c := range p.ColorPalette {
		if c == nil {
			return PatternStats{}, apperr.InvalidArgument("color_palette contains nil entry")
		}
		if c.Index <= 0 {
			return PatternStats{}, apperr.InvalidArgument("color index must be positive")
		}
		if _, exists := paletteIndexes[c.Index]; exists {
			return PatternStats{}, apperr.InvalidArgument("color_palette index must be unique")
		}
		if !patternHex.MatchString(c.Hex) {
			return PatternStats{}, apperr.InvalidArgument("color hex must use #RRGGBB")
		}
		paletteIndexes[c.Index] = struct{}{}
	}

	stats := PatternStats{}
	usedIndexes := make(map[int32]struct{}, len(p.ColorPalette))
	for _, px := range p.Pixels {
		if px == 0 {
			continue
		}
		if _, exists := paletteIndexes[px]; !exists {
			return PatternStats{}, apperr.InvalidArgument("pixel index not found in color_palette")
		}
		stats.BeadCount++
		usedIndexes[px] = struct{}{}
	}

	stats.ColorCount = len(usedIndexes)
	return stats, nil
}

func PatternDataToJSONMap(p *pb.PatternData) model.JSONMap {
	if p == nil {
		return nil
	}

	colors := make([]interface{}, 0, len(p.ColorPalette))
	for _, c := range p.ColorPalette {
		if c == nil {
			continue
		}
		colors = append(colors, map[string]interface{}{
			"index": c.Index,
			"hex":   c.Hex,
			"brand": c.Brand,
			"code":  c.Code,
			"name":  c.Name,
		})
	}

	pixelValues := make([]interface{}, 0, len(p.Pixels))
	for _, px := range p.Pixels {
		pixelValues = append(pixelValues, px)
	}

	return model.JSONMap{
		"schema_version": p.SchemaVersion,
		"width":          p.Width,
		"height":         p.Height,
		"board_spec":     p.BoardSpec,
		"pixels":         pixelValues,
		"color_palette":  colors,
	}
}

func ApplyPatternData(w *model.Work, p *pb.PatternData) error {
	if w == nil {
		return apperr.InvalidArgument("work required")
	}

	stats, err := CalculatePatternStats(p)
	if err != nil {
		return err
	}
	w.PatternData = PatternDataToJSONMap(p)
	w.Width = int(p.Width)
	w.Height = int(p.Height)
	w.BoardSpec = p.BoardSpec
	w.BeadCount = stats.BeadCount
	w.ColorCount = stats.ColorCount
	return nil
}

func NormalizeWorkPatternData(w *model.Work) error {
	if w == nil {
		return apperr.InvalidArgument("work required")
	}
	p, err := DecodePatternData(w.PatternData)
	if err != nil {
		return err
	}
	return ApplyPatternData(w, p)
}

func DecodePatternData(m model.JSONMap) (*pb.PatternData, error) {
	if m == nil {
		return nil, nil
	}

	pd := &pb.PatternData{}

	values := map[string]interface{}(m)
	pd.Width = int32Value(patternValue(values, "width", "width"))
	pd.Height = int32Value(patternValue(values, "height", "height"))
	pd.BoardSpec, _ = patternValue(values, "board_spec", "boardSpec").(string)
	if schemaVersion, exists := values["schema_version"]; exists {
		pd.SchemaVersion = int32Value(schemaVersion)
	} else if schemaVersion, exists := values["schemaVersion"]; exists {
		pd.SchemaVersion = int32Value(schemaVersion)
	} else {
		pd.SchemaVersion = currentPatternSchemaVersion
	}

	if pixels, ok := patternValue(values, "pixels", "pixels").([]interface{}); ok {
		pd.Pixels = make([]int32, 0, len(pixels))
		for _, p := range pixels {
			pd.Pixels = append(pd.Pixels, int32Value(p))
		}
	}

	if palette, ok := patternValue(values, "color_palette", "colorPalette").([]interface{}); ok {
		for _, entry := range palette {
			if e, ok := entry.(map[string]interface{}); ok {
				ce := &pb.ColorEntry{}
				ce.Index = int32Value(patternValue(e, "index", "index"))
				if v, ok := e["hex"].(string); ok {
					ce.Hex = v
				}
				if v, ok := e["brand"].(string); ok {
					ce.Brand = v
				}
				if v, ok := e["code"].(string); ok {
					ce.Code = v
				}
				if v, ok := e["name"].(string); ok {
					ce.Name = v
				}
				pd.ColorPalette = append(pd.ColorPalette, ce)
			}
		}
	}

	if err := ValidatePatternData(pd); err != nil {
		return nil, err
	}
	return pd, nil
}

func JSONMapToPatternData(m model.JSONMap) *pb.PatternData {
	p, _ := DecodePatternData(m)
	return p
}

func patternValue(m map[string]interface{}, snake, camel string) interface{} {
	if value, ok := m[snake]; ok {
		return value
	}
	return m[camel]
}

func int32Value(value interface{}) int32 {
	switch v := value.(type) {
	case int:
		return int32(v)
	case int32:
		return v
	case int64:
		return int32(v)
	case float64:
		return int32(v)
	case float32:
		return int32(v)
	default:
		return 0
	}
}
