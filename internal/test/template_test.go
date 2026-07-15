package test

import (
	"context"
	"testing"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/service/template"
	"github.com/zhaojiabo/bobobeads_server/internal/service/work"
)

func seedTemplateData(t *testing.T) {
	t.Helper()
	cat := &model.TemplateCategory{Name: "动物", IconURL: "https://oss/cat.png", SortOrder: 1, Status: 1}
	db.DB.Create(cat)

	tpl := &model.Template{
		CategoryID:  cat.ID,
		Title:       "小猫咪",
		PreviewURL:  "https://oss/cat-preview.png",
		Tags:        "猫,动物,可爱",
		Difficulty:  2,
		Width:       29,
		Height:      29,
		ColorCount:  12,
		IsFree:      true,
		SortOrder:   1,
		Status:      1,
		PatternData: work.PatternDataToJSONMap(validPatternData(29, 29)),
	}
	db.DB.Create(tpl)

	tpl2 := &model.Template{
		CategoryID: cat.ID,
		Title:      "小狗狗",
		PreviewURL: "https://oss/dog-preview.png",
		Tags:       "狗,动物",
		Difficulty: 1,
		Width:      15,
		Height:     15,
		ColorCount: 8,
		IsFree:     false,
		CreditCost: 2,
		SortOrder:  2,
		Status:     1,
	}
	db.DB.Create(tpl2)

	// Inactive template - must update after create due to GORM zero-value handling
	hidden := &model.Template{
		CategoryID: cat.ID,
		Title:      "隐藏模板",
		PreviewURL: "https://oss/hidden.png",
		Status:     1,
	}
	db.DB.Create(hidden)
	db.DB.Model(hidden).Update("status", 0)
}

func TestListTemplates_SceneHome(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)

	templateDAO := dao.NewTemplateDAO()
	svc := template.NewService(templateDAO)

	templates, total, err := svc.ListTemplates(context.Background(), template.ListInput{
		Scene: "home", Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListTemplates failed: %v", err)
	}
	if total != 2 {
		t.Errorf("expected total=2 (active only), got %d", total)
	}
	if len(templates) != 2 {
		t.Fatalf("expected 2 templates, got %d", len(templates))
	}
	if templates[0].Title != "小猫咪" {
		t.Errorf("expected first template=小猫咪, got %s", templates[0].Title)
	}
	t.Log("ListTemplates scene=home success")
}

func TestListTemplates_ByCategoryID(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)

	templateDAO := dao.NewTemplateDAO()
	svc := template.NewService(templateDAO)

	var cat model.TemplateCategory
	db.DB.Where("name = ?", "动物").First(&cat)

	templates, total, err := svc.ListTemplates(context.Background(), template.ListInput{
		CategoryID: cat.ID, Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListTemplates by category failed: %v", err)
	}
	if total != 2 {
		t.Errorf("expected total=2, got %d", total)
	}
	_ = templates
	t.Log("ListTemplates by category success")
}

func TestListTemplates_Keyword(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)

	templateDAO := dao.NewTemplateDAO()
	svc := template.NewService(templateDAO)

	templates, total, err := svc.ListTemplates(context.Background(), template.ListInput{
		Keyword: "猫", Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListTemplates keyword failed: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total=1, got %d", total)
	}
	if len(templates) != 1 || templates[0].Title != "小猫咪" {
		t.Error("keyword search did not match expected template")
	}
	t.Log("ListTemplates keyword success")
}

func TestGetTemplate_WithPatternData(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)

	templateDAO := dao.NewTemplateDAO()
	svc := template.NewService(templateDAO)

	var tpl model.Template
	db.DB.Where("title = ?", "小猫咪").First(&tpl)

	result, err := svc.GetTemplate(context.Background(), tpl.ID)
	if err != nil {
		t.Fatalf("GetTemplate failed: %v", err)
	}
	if result.PatternData == nil {
		t.Error("expected non-nil pattern_data")
	}
	if result.Title != "小猫咪" {
		t.Errorf("expected title=小猫咪, got %s", result.Title)
	}
	t.Log("GetTemplate with pattern_data success")
}

func TestGetTemplate_InvalidID(t *testing.T) {
	SetupTestDB(t)
	templateDAO := dao.NewTemplateDAO()
	svc := template.NewService(templateDAO)

	_, err := svc.GetTemplate(context.Background(), 0)
	if err == nil {
		t.Error("expected error for invalid template_id")
	}
	t.Log("GetTemplate invalid ID check success")
}

func TestGetTemplate_ThumbnailFallback(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)

	templateDAO := dao.NewTemplateDAO()
	svc := template.NewService(templateDAO)

	var tpl model.Template
	db.DB.Where("title = ?", "小猫咪").First(&tpl)

	result, _ := svc.GetTemplate(context.Background(), tpl.ID)
	if result.ThumbnailURL != "" {
		t.Log("template has thumbnail_url set")
	} else {
		t.Log("thumbnail_url is empty, handler should fallback to preview_url")
	}
}

func TestFavoriteTemplate_Idempotent(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)

	templateDAO := dao.NewTemplateDAO()
	svc := template.NewService(templateDAO)

	var tpl model.Template
	db.DB.Where("title = ?", "小猫咪").First(&tpl)

	userID := uint64(1)

	count1, err := svc.FavoriteTemplate(context.Background(), userID, tpl.ID)
	if err != nil {
		t.Fatalf("FavoriteTemplate failed: %v", err)
	}
	if count1 != 1 {
		t.Errorf("expected favorite_count=1, got %d", count1)
	}

	// Favorite again - idempotent
	count2, err := svc.FavoriteTemplate(context.Background(), userID, tpl.ID)
	if err != nil {
		t.Fatalf("second FavoriteTemplate failed: %v", err)
	}
	if count2 != 1 {
		t.Errorf("expected favorite_count=1 (no double increment), got %d", count2)
	}

	t.Log("FavoriteTemplate idempotent success")
}

func TestUnfavoriteTemplate_Idempotent(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)

	templateDAO := dao.NewTemplateDAO()
	svc := template.NewService(templateDAO)

	var tpl model.Template
	db.DB.Where("title = ?", "小猫咪").First(&tpl)

	userID := uint64(1)

	// Favorite first
	svc.FavoriteTemplate(context.Background(), userID, tpl.ID)

	// Unfavorite
	count1, err := svc.UnfavoriteTemplate(context.Background(), userID, tpl.ID)
	if err != nil {
		t.Fatalf("UnfavoriteTemplate failed: %v", err)
	}
	if count1 != 0 {
		t.Errorf("expected favorite_count=0, got %d", count1)
	}

	// Unfavorite again - idempotent, should not go negative
	count2, err := svc.UnfavoriteTemplate(context.Background(), userID, tpl.ID)
	if err != nil {
		t.Fatalf("second UnfavoriteTemplate failed: %v", err)
	}
	if count2 < 0 {
		t.Errorf("favorite_count went negative: %d", count2)
	}

	t.Log("UnfavoriteTemplate idempotent success")
}

func TestFavoriteTemplate_NotFound(t *testing.T) {
	SetupTestDB(t)
	templateDAO := dao.NewTemplateDAO()
	svc := template.NewService(templateDAO)

	_, err := svc.FavoriteTemplate(context.Background(), 1, 99999)
	if err == nil {
		t.Error("expected error for nonexistent template")
	}
	t.Log("FavoriteTemplate not found check success")
}

func TestListFavoriteTemplates(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)

	templateDAO := dao.NewTemplateDAO()
	svc := template.NewService(templateDAO)

	var tpl1, tpl2 model.Template
	db.DB.Where("title = ?", "小猫咪").First(&tpl1)
	db.DB.Where("title = ?", "小狗狗").First(&tpl2)

	userA := uint64(1)
	userB := uint64(2)

	svc.FavoriteTemplate(context.Background(), userA, tpl1.ID)
	svc.FavoriteTemplate(context.Background(), userA, tpl2.ID)
	svc.FavoriteTemplate(context.Background(), userB, tpl1.ID)

	// User A should see 2 favorites
	favs, total, err := svc.ListFavoriteTemplates(context.Background(), userA, 1, 20)
	if err != nil {
		t.Fatalf("ListFavoriteTemplates failed: %v", err)
	}
	if total != 2 {
		t.Errorf("expected total=2 for userA, got %d", total)
	}
	if len(favs) != 2 {
		t.Errorf("expected 2 favorites, got %d", len(favs))
	}

	// User B should see 1 favorite
	favsB, totalB, _ := svc.ListFavoriteTemplates(context.Background(), userB, 1, 20)
	if totalB != 1 {
		t.Errorf("expected total=1 for userB, got %d", totalB)
	}
	_ = favsB

	t.Log("ListFavoriteTemplates isolation success")
}

func TestListCategories_WithCount(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)

	templateDAO := dao.NewTemplateDAO()
	svc := template.NewService(templateDAO)

	categories, counts, err := svc.ListCategories(context.Background())
	if err != nil {
		t.Fatalf("ListCategories failed: %v", err)
	}
	if len(categories) != 1 {
		t.Fatalf("expected 1 category, got %d", len(categories))
	}
	if counts[0] != 2 {
		t.Errorf("expected template_count=2, got %d", counts[0])
	}
	t.Log("ListCategories with count success")
}

func TestListTemplates_IsFavorited(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)

	templateDAO := dao.NewTemplateDAO()
	svc := template.NewService(templateDAO)

	var tpl model.Template
	db.DB.Where("title = ?", "小猫咪").First(&tpl)

	userID := uint64(1)
	svc.FavoriteTemplate(context.Background(), userID, tpl.ID)

	templates, _, _ := svc.ListTemplates(context.Background(), template.ListInput{
		Scene: "home", Page: 1, PageSize: 20,
	})

	templateIDs := make([]uint64, len(templates))
	for i, tp := range templates {
		templateIDs[i] = tp.ID
	}

	favMap, _ := svc.BatchGetFavorited(context.Background(), userID, templateIDs)
	if !favMap[tpl.ID] {
		t.Error("expected is_favorited=true for favorited template")
	}

	t.Log("ListTemplates is_favorited check success")
}
