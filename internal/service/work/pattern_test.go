package work

import (
	"testing"

	"github.com/zhaojiabo/bobobeads_server/conf"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
)

func TestCalculatePatternStatsAllowsAtMostConfiguredColors(t *testing.T) {
	previousConfig := conf.GlobalConfig
	conf.GlobalConfig = &conf.Config{Pattern: conf.PatternConfig{MaxColors: 221}}
	t.Cleanup(func() { conf.GlobalConfig = previousConfig })

	pattern := &pb.PatternData{
		Width:         1,
		Height:        1,
		BoardSpec:     "standard",
		SchemaVersion: currentPatternSchemaVersion,
		Pixels:        []int32{1},
	}
	for i := 1; i <= 221; i++ {
		pattern.ColorPalette = append(pattern.ColorPalette, &pb.ColorEntry{Index: int32(i), Hex: "#000000"})
	}

	if _, err := CalculatePatternStats(pattern); err != nil {
		t.Fatalf("expected 221 colors to be valid: %v", err)
	}

	pattern.ColorPalette = append(pattern.ColorPalette, &pb.ColorEntry{Index: 222, Hex: "#000000"})
	if _, err := CalculatePatternStats(pattern); err == nil {
		t.Fatal("expected 222 colors to be rejected")
	}
}
