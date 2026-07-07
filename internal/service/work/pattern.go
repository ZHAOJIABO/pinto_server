package work

import (
	"github.com/zhaojiabo/bobobeads_server/conf"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
)

func ValidatePatternData(p *pb.PatternData) error {
	if p == nil {
		return apperr.InvalidArgument("pattern_data required")
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
		return apperr.InvalidArgument("pattern width and height must be positive")
	}
	if int(p.Width) > maxWidth || int(p.Height) > maxHeight {
		return apperr.InvalidArgument("pattern dimensions exceed maximum")
	}
	totalPixels := int(p.Width) * int(p.Height)
	if totalPixels > maxPixels {
		return apperr.InvalidArgument("pattern pixel count exceeds maximum")
	}

	if len(p.Pixels) == 0 && len(p.PixelRows) == 0 {
		return apperr.InvalidArgument("pixels or pixel_rows required")
	}

	if len(p.Pixels) > 0 && len(p.Pixels) != totalPixels {
		return apperr.InvalidArgument("pixels length must equal width * height")
	}

	if len(p.PixelRows) > 0 {
		if len(p.PixelRows) != int(p.Height) {
			return apperr.InvalidArgument("pixel_rows length must equal height")
		}
		for _, row := range p.PixelRows {
			if len(row) != int(p.Width) {
				return apperr.InvalidArgument("each pixel row length must equal width")
			}
		}
	}

	if len(p.ColorPalette) == 0 {
		return apperr.InvalidArgument("color_palette is required")
	}
	if len(p.ColorPalette) > maxColors {
		return apperr.InvalidArgument("color_palette exceeds maximum colors")
	}

	paletteIndexes := map[int32]bool{0: true}
	for _, c := range p.ColorPalette {
		if c == nil {
			return apperr.InvalidArgument("color_palette contains nil entry")
		}
		if c.Index <= 0 {
			return apperr.InvalidArgument("color index must be positive")
		}
		paletteIndexes[c.Index] = true
	}

	for _, px := range p.Pixels {
		if !paletteIndexes[px] {
			return apperr.InvalidArgument("pixel index not found in color_palette")
		}
	}

	for _, row := range p.PixelRows {
		for _, b := range row {
			if !paletteIndexes[int32(b)] {
				return apperr.InvalidArgument("pixel index not found in color_palette")
			}
		}
	}

	return nil
}

func PatternDataToJSONMap(p *pb.PatternData) model.JSONMap {
	if p == nil {
		return nil
	}

	colors := make([]interface{}, 0, len(p.ColorPalette))
	for _, c := range p.ColorPalette {
		colors = append(colors, map[string]interface{}{
			"index": c.Index,
			"hex":   c.Hex,
			"brand": c.Brand,
			"code":  c.Code,
			"name":  c.Name,
		})
	}

	pixelData := p.Pixels
	if len(pixelData) == 0 && len(p.PixelRows) > 0 {
		pixelData = pixelRowsToPixels(p.PixelRows, p.Width, p.Height)
	}

	pixelValues := make([]interface{}, 0, len(pixelData))
	for _, px := range pixelData {
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

func pixelRowsToPixels(rows [][]byte, width, height int32) []int32 {
	pixels := make([]int32, 0, int(width)*int(height))
	for _, row := range rows {
		for _, b := range row {
			pixels = append(pixels, int32(b))
		}
	}
	return pixels
}

func JSONMapToPatternData(m model.JSONMap) *pb.PatternData {
	if m == nil {
		return nil
	}

	pd := &pb.PatternData{}

	if v, ok := m["width"].(float64); ok {
		pd.Width = int32(v)
	}
	if v, ok := m["height"].(float64); ok {
		pd.Height = int32(v)
	}
	if v, ok := m["board_spec"].(string); ok {
		pd.BoardSpec = v
	}
	if v, ok := m["schema_version"].(float64); ok {
		pd.SchemaVersion = int32(v)
	}

	if pixels, ok := m["pixels"].([]interface{}); ok {
		pd.Pixels = make([]int32, 0, len(pixels))
		for _, p := range pixels {
			if v, ok := p.(float64); ok {
				pd.Pixels = append(pd.Pixels, int32(v))
			}
		}
	}

	if palette, ok := m["color_palette"].([]interface{}); ok {
		for _, entry := range palette {
			if e, ok := entry.(map[string]interface{}); ok {
				ce := &pb.ColorEntry{}
				if v, ok := e["index"].(float64); ok {
					ce.Index = int32(v)
				}
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

	return pd
}
