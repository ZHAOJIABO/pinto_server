package test

import (
	"context"
	"testing"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/service/system"
)

func TestGetBeadColors(t *testing.T) {
	SetupTestDB(t)
	systemDAO := dao.NewSystemDAO()
	systemService := system.NewService(systemDAO)

	ctx := context.Background()

	// Seed some bead colors
	colors := []*model.BeadColor{
		{Brand: "hama", Code: "H-01", Name: "白色", Hex: "#FFFFFF", R: 255, G: 255, B: 255, Status: 1},
		{Brand: "hama", Code: "H-02", Name: "奶油色", Hex: "#FFF5E1", R: 255, G: 245, B: 225, Status: 1},
		{Brand: "perler", Code: "P-01", Name: "白色", Hex: "#FEFEFE", R: 254, G: 254, B: 254, Status: 1},
	}
	for _, c := range colors {
		db.DB.Create(c)
	}

	// Get all brands
	result, err := systemService.GetBeadColors(ctx, "")
	if err != nil {
		t.Fatalf("GetBeadColors failed: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("expected 3 colors total, got %d", len(result))
	}

	// Filter by brand
	result, err = systemService.GetBeadColors(ctx, "hama")
	if err != nil {
		t.Fatalf("GetBeadColors(hama) failed: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 hama colors, got %d", len(result))
	}

	t.Logf("GetBeadColors success: total=%d, hama=%d", 3, 2)
}

func TestGetBoardSpecs(t *testing.T) {
	SetupTestDB(t)
	systemDAO := dao.NewSystemDAO()
	systemService := system.NewService(systemDAO)

	ctx := context.Background()

	// Seed board specs
	specs := []*model.BoardSpec{
		{Name: "小方板", Shape: "square", Width: 15, Height: 15, BeadSize: 5.0, Status: 1},
		{Name: "标准方板", Shape: "square", Width: 29, Height: 29, BeadSize: 5.0, Status: 1},
		{Name: "大方板", Shape: "square", Width: 39, Height: 39, BeadSize: 5.0, Status: 1},
	}
	for _, s := range specs {
		db.DB.Create(s)
	}

	result, err := systemService.GetBoardSpecs(ctx)
	if err != nil {
		t.Fatalf("GetBoardSpecs failed: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("expected 3 specs, got %d", len(result))
	}

	t.Logf("GetBoardSpecs success: %d specs", len(result))
}

func TestAppConfig(t *testing.T) {
	SetupTestDB(t)
	systemDAO := dao.NewSystemDAO()
	systemService := system.NewService(systemDAO)

	ctx := context.Background()

	// Seed configs
	db.DB.Create(&model.Config{ConfigKey: "app_name", ConfigValue: "拼豆"})
	db.DB.Create(&model.Config{ConfigKey: "min_version", ConfigValue: "1.0.0"})

	configs, err := systemService.GetAppConfig(ctx)
	if err != nil {
		t.Fatalf("GetAppConfig failed: %v", err)
	}
	if configs["app_name"] != "拼豆" {
		t.Errorf("expected app_name=拼豆, got %s", configs["app_name"])
	}
	if configs["min_version"] != "1.0.0" {
		t.Errorf("expected min_version=1.0.0, got %s", configs["min_version"])
	}

	t.Logf("AppConfig success: %d configs", len(configs))
}
