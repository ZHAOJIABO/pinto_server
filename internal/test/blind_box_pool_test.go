package test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/zhaojiabo/bobobeads_server/conf"
	"github.com/zhaojiabo/bobobeads_server/internal/api"
	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	adminauth "github.com/zhaojiabo/bobobeads_server/internal/service/admin"
	"github.com/zhaojiabo/bobobeads_server/internal/service/template"
)

// blindBoxFixture 把奖池用例共用的三件东西收在一处：C 端 service、后台 service，
// 以及按标题取图纸的快捷方式。
type blindBoxFixture struct {
	svc   *template.Service
	admin *template.AdminService
}

func newBlindBoxFixture(t *testing.T) *blindBoxFixture {
	t.Helper()
	templateDAO := dao.NewTemplateDAO()
	poolDAO := dao.NewBlindBoxPoolDAO()
	return &blindBoxFixture{
		svc:   template.NewService(templateDAO, dao.NewBlindBoxRecordDAO(), poolDAO, dao.NewBlindBoxQuotaDAO()),
		admin: template.NewAdminService(templateDAO, poolDAO),
	}
}

func templateByTitle(t *testing.T, title string) *model.Template {
	t.Helper()
	var tpl model.Template
	if err := db.DB.Where("title = ?", title).First(&tpl).Error; err != nil {
		t.Fatalf("seed template %q missing: %v", title, err)
	}
	return &tpl
}

func templateStatus(t *testing.T, templateID uint64) int8 {
	t.Helper()
	var status int8
	if err := db.DB.Model(&model.Template{}).Where("id = ?", templateID).
		Pluck("status", &status).Error; err != nil {
		t.Fatalf("read template status: %v", err)
	}
	return status
}

func TestBlindBoxEmptyPoolReturnsNotFoundAndRecordsNothing(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)
	fixture := newBlindBoxFixture(t)

	// 有可抽的上架图纸，但奖池是空的：不能回退到全库随机。
	if _, err := fixture.svc.GetRandomTemplate(context.Background()); err == nil {
		t.Fatal("expected empty pool to fail the draw")
	}

	var records int64
	db.DB.Model(&model.BlindBoxRecord{}).Count(&records)
	if records != 0 {
		t.Fatalf("failed draw must not write a blind box record, got %d", records)
	}
}

func TestBlindBoxDrawsOnlyPoolMembers(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)
	fixture := newBlindBoxFixture(t)
	ctx := context.Background()

	cat := templateByTitle(t, "小猫咪")
	dog := templateByTitle(t, "小狗狗")
	if _, err := fixture.admin.AddToPool(ctx, cat.ID, 1, 0); err != nil {
		t.Fatalf("AddToPool(cat) failed: %v", err)
	}
	if _, err := fixture.admin.AddToPool(ctx, dog.ID, 1, 0); err != nil {
		t.Fatalf("AddToPool(dog) failed: %v", err)
	}

	inPool := map[uint64]bool{cat.ID: true, dog.ID: true}
	for i := 0; i < 50; i++ {
		tpl, err := fixture.svc.GetRandomTemplate(ctx)
		if err != nil {
			t.Fatalf("draw %d failed: %v", i, err)
		}
		if !inPool[tpl.ID] {
			t.Fatalf("draw %d returned template %d which is not in the pool", i, tpl.ID)
		}
	}
}

func TestBlindBoxDrawRespectsWeight(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)
	fixture := newBlindBoxFixture(t)
	ctx := context.Background()

	rare := templateByTitle(t, "小猫咪")
	common := templateByTitle(t, "小狗狗")
	if _, err := fixture.admin.AddToPool(ctx, rare.ID, 1, 0); err != nil {
		t.Fatalf("AddToPool(rare) failed: %v", err)
	}
	if _, err := fixture.admin.AddToPool(ctx, common.ID, 99, 0); err != nil {
		t.Fatalf("AddToPool(common) failed: %v", err)
	}

	// 阈值刻意宽松（期望 0.99，只断言 > 0.8），也不断言稀有款必现——两者都会让用例
	// 变成概率性失败。
	const draws = 500
	commonHits := 0
	for i := 0; i < draws; i++ {
		tpl, err := fixture.svc.GetRandomTemplate(ctx)
		if err != nil {
			t.Fatalf("draw %d failed: %v", i, err)
		}
		if tpl.ID == common.ID {
			commonHits++
		}
	}
	if ratio := float64(commonHits) / draws; ratio < 0.8 {
		t.Fatalf("weight 99 vs 1 should dominate, got ratio %.3f", ratio)
	}
}

func TestBlindBoxRejectsInvalidWeightAndTreatsZeroWeightRowAsEmpty(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)
	fixture := newBlindBoxFixture(t)
	ctx := context.Background()

	cat := templateByTitle(t, "小猫咪")
	if _, err := fixture.admin.AddToPool(ctx, cat.ID, 0, 0); err == nil {
		t.Fatal("weight 0 must be rejected; use status=0 to disable an entry")
	}
	if _, err := fixture.admin.AddToPool(ctx, cat.ID, 10001, 0); err == nil {
		t.Fatal("weight above the ceiling must be rejected")
	}

	// 绕过服务层直接写脏数据：weight=0 的条目要被抽奖当成不存在，而不是除零。
	// 必须 Create 后再 Update：Weight 带 default:1 标签，GORM 会把零值换成默认值。
	dirty := &model.BlindBoxPoolItem{TemplateID: cat.ID, Weight: 1, Status: 1}
	if err := db.DB.Create(dirty).Error; err != nil {
		t.Fatalf("insert pool row: %v", err)
	}
	if err := db.DB.Model(dirty).Update("weight", 0).Error; err != nil {
		t.Fatalf("zero the weight: %v", err)
	}
	if _, err := fixture.svc.GetRandomTemplate(ctx); err == nil {
		t.Fatal("a pool holding only zero-weight rows must behave like an empty pool")
	}
}

func TestBlindBoxSkipsUnpublishedTemplates(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)
	fixture := newBlindBoxFixture(t)
	ctx := context.Background()

	cat := templateByTitle(t, "小猫咪")
	dog := templateByTitle(t, "小狗狗")
	if _, err := fixture.admin.AddToPool(ctx, cat.ID, 1, 0); err != nil {
		t.Fatalf("AddToPool(cat) failed: %v", err)
	}
	if _, err := fixture.admin.AddToPool(ctx, dog.ID, 1, 0); err != nil {
		t.Fatalf("AddToPool(dog) failed: %v", err)
	}

	// 模拟"图纸被下架但忘了移出奖池"这种脏状态。
	db.DB.Model(&model.Template{}).Where("id = ?", cat.ID).Update("status", dao.StatusUnpublished)

	for i := 0; i < 20; i++ {
		tpl, err := fixture.svc.GetRandomTemplate(ctx)
		if err != nil {
			t.Fatalf("draw %d failed: %v", i, err)
		}
		if tpl.ID == cat.ID {
			t.Fatalf("draw %d returned unpublished template %d", i, cat.ID)
		}
	}

	db.DB.Model(&model.Template{}).Where("id = ?", dog.ID).Update("status", dao.StatusUnpublished)
	if _, err := fixture.svc.GetRandomTemplate(ctx); err == nil {
		t.Fatal("pool with only unpublished templates must behave like an empty pool")
	}
}

func TestBlindBoxTemplateDisappearsFromClientLists(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)
	fixture := newBlindBoxFixture(t)
	ctx := context.Background()

	cat := templateByTitle(t, "小猫咪")
	beforeCount, err := dao.NewTemplateDAO().CountByCategory(ctx, cat.CategoryID)
	if err != nil {
		t.Fatalf("CountByCategory failed: %v", err)
	}

	if _, err := fixture.admin.AddToPool(ctx, cat.ID, 1, 0); err != nil {
		t.Fatalf("AddToPool failed: %v", err)
	}
	if status := templateStatus(t, cat.ID); status != dao.StatusBlindBoxOnly {
		t.Fatalf("expected status=%d after joining the pool, got %d", dao.StatusBlindBoxOnly, status)
	}

	inputs := map[string]template.ListInput{
		"scene=home": {Scene: "home", Page: 1, PageSize: 20},
		"byCategory": {CategoryID: cat.CategoryID, Page: 1, PageSize: 20},
		"byKeyword":  {Keyword: "猫", Page: 1, PageSize: 20},
	}
	for name, input := range inputs {
		templates, _, err := fixture.svc.ListTemplates(ctx, input)
		if err != nil {
			t.Fatalf("ListTemplates(%s) failed: %v", name, err)
		}
		for _, tpl := range templates {
			if tpl.ID == cat.ID {
				t.Fatalf("ListTemplates(%s) must not return a blind-box-only template", name)
			}
		}
	}

	afterCount, err := dao.NewTemplateDAO().CountByCategory(ctx, cat.CategoryID)
	if err != nil {
		t.Fatalf("CountByCategory failed: %v", err)
	}
	if afterCount != beforeCount-1 {
		t.Fatalf("category count should drop by 1, got %d -> %d", beforeCount, afterCount)
	}
}

func TestBlindBoxDuplicateAddRejected(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)
	fixture := newBlindBoxFixture(t)
	ctx := context.Background()

	cat := templateByTitle(t, "小猫咪")
	if _, err := fixture.admin.AddToPool(ctx, cat.ID, 1, 0); err != nil {
		t.Fatalf("AddToPool failed: %v", err)
	}
	if _, err := fixture.admin.AddToPool(ctx, cat.ID, 5, 0); err == nil {
		t.Fatal("adding the same template twice must be rejected")
	}

	var items int64
	db.DB.Model(&model.BlindBoxPoolItem{}).Where("template_id = ?", cat.ID).Count(&items)
	if items != 1 {
		t.Fatalf("expected exactly one pool entry, got %d", items)
	}
}

func TestBlindBoxTemplateStaysViewableAndFavoritable(t *testing.T) {
	SetupTestDB(t)
	fixture := newBlindBoxFixture(t)
	ctx := context.Background()

	// 盲盒专用分类：不进 C 端分类导航，但收藏分类聚合要能看到它。
	blindCategory := &model.TemplateCategory{Name: "盲盒限定", Status: 1, IsBlindBox: true}
	if err := db.DB.Create(blindCategory).Error; err != nil {
		t.Fatalf("create blind box category: %v", err)
	}
	tpl := &model.Template{
		CategoryID: blindCategory.ID,
		Title:      "限定小猫",
		PreviewURL: "https://oss/limited-cat.png",
		Width:      15,
		Height:     15,
		Status:     1,
	}
	if err := db.DB.Create(tpl).Error; err != nil {
		t.Fatalf("create template: %v", err)
	}

	if _, err := fixture.admin.AddToPool(ctx, tpl.ID, 1, 0); err != nil {
		t.Fatalf("AddToPool failed: %v", err)
	}

	if _, err := fixture.svc.GetTemplate(ctx, tpl.ID); err != nil {
		t.Fatalf("a drawn blind box template must still open its detail page: %v", err)
	}
	if _, err := fixture.svc.FavoriteTemplate(ctx, 42, tpl.ID); err != nil {
		t.Fatalf("FavoriteTemplate failed: %v", err)
	}

	favorites, total, err := fixture.svc.ListFavoriteTemplates(ctx, 42, 0, 1, 20)
	if err != nil {
		t.Fatalf("ListFavoriteTemplates failed: %v", err)
	}
	if total != 1 || len(favorites) != 1 || favorites[0].ID != tpl.ID {
		t.Fatalf("expected the blind box template in favorites, got total=%d items=%d", total, len(favorites))
	}

	favCategories, favCounts, err := fixture.svc.ListFavoriteCategories(ctx, 42)
	if err != nil {
		t.Fatalf("ListFavoriteCategories failed: %v", err)
	}
	if len(favCategories) != 1 || favCategories[0].ID != blindCategory.ID || favCounts[0] != 1 {
		t.Fatalf("favorites should group under the blind box category, got %+v %v", favCategories, favCounts)
	}

	// 同一个分类不能出现在 C 端导航里。
	navCategories, _, err := fixture.svc.ListCategories(ctx)
	if err != nil {
		t.Fatalf("ListCategories failed: %v", err)
	}
	for _, category := range navCategories {
		if category.ID == blindCategory.ID {
			t.Fatal("blind box categories must not appear in the client category navigation")
		}
	}
}

// 客户端按 category_id 映射本地内置的图案和文案，所以每条返回图纸的路由都必须带上分类。
// 名称是兜底：盲盒专用分类不在 ListCategories 的结果里，App 还没内置某个新分类时，
// 只能靠服务端下发的名称显示。
func TestBlindBoxResponseCarriesCategory(t *testing.T) {
	SetupTestDB(t)
	fixture := newBlindBoxFixture(t)
	ctx := context.Background()

	blindCategory := &model.TemplateCategory{Name: "盲盒限定·动物系列", Status: 1, IsBlindBox: true}
	if err := db.DB.Create(blindCategory).Error; err != nil {
		t.Fatalf("create blind box category: %v", err)
	}
	tpl := &model.Template{
		CategoryID: blindCategory.ID,
		Title:      "限定小猫",
		PreviewURL: "https://oss/limited-cat.png",
		Width:      15,
		Height:     15,
		Status:     1,
	}
	if err := db.DB.Create(tpl).Error; err != nil {
		t.Fatalf("create template: %v", err)
	}
	if _, err := fixture.admin.AddToPool(ctx, tpl.ID, 1, 0); err != nil {
		t.Fatalf("AddToPool failed: %v", err)
	}

	handler := api.NewTemplateHandler(fixture.svc)

	drawn, err := handler.RandomTemplate(ctx, &pb.RandomTemplateRequest{})
	if err != nil || drawn.Header.Code != 0 {
		t.Fatalf("RandomTemplate failed: err=%v header=%#v", err, drawn.Header)
	}
	if drawn.Template.CategoryId != int32(blindCategory.ID) {
		t.Fatalf("expected category_id %d, got %d", blindCategory.ID, drawn.Template.CategoryId)
	}
	if drawn.Template.CategoryName != blindCategory.Name {
		t.Fatalf("expected category_name %q, got %q", blindCategory.Name, drawn.Template.CategoryName)
	}

	// 开盒历史和详情页走的是另外两条投影，分类同样不能丢。
	history, err := handler.ListBlindBoxRecords(ctx, &pb.ListBlindBoxRecordsRequest{
		Page: &pb.PageRequest{Page: 1, PageSize: 20},
	})
	if err != nil || history.Header.Code != 0 {
		t.Fatalf("ListBlindBoxRecords failed: err=%v header=%#v", err, history.Header)
	}
	if len(history.Templates) != 1 || history.Templates[0].CategoryName != blindCategory.Name {
		t.Fatalf("blind box history must carry the category, got %+v", history.Templates)
	}

	detail, err := handler.GetTemplate(ctx, &pb.GetTemplateRequest{
		TemplateId: strconv.FormatUint(tpl.ID, 10),
	})
	if err != nil || detail.Header.Code != 0 {
		t.Fatalf("GetTemplate failed: err=%v header=%#v", err, detail.Header)
	}
	if detail.Template.CategoryName != blindCategory.Name {
		t.Fatalf("template detail must carry the category, got %q", detail.Template.CategoryName)
	}
}

func TestBlindBoxRemoveFromPoolRestoresTemplate(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)
	fixture := newBlindBoxFixture(t)
	ctx := context.Background()

	cat := templateByTitle(t, "小猫咪")
	item, err := fixture.admin.AddToPool(ctx, cat.ID, 1, 0)
	if err != nil {
		t.Fatalf("AddToPool failed: %v", err)
	}
	if err := fixture.admin.RemoveFromPool(ctx, item.ID); err != nil {
		t.Fatalf("RemoveFromPool failed: %v", err)
	}

	if status := templateStatus(t, cat.ID); status != dao.StatusPublished {
		t.Fatalf("expected status=%d after leaving the pool, got %d", dao.StatusPublished, status)
	}

	templates, _, err := fixture.svc.ListTemplates(ctx, template.ListInput{Scene: "home", Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListTemplates failed: %v", err)
	}
	found := false
	for _, tpl := range templates {
		if tpl.ID == cat.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("template should be back in the client list after leaving the pool")
	}

	if _, err := fixture.svc.GetRandomTemplate(ctx); err == nil {
		t.Fatal("pool is empty again, so the draw must fail")
	}
}

func TestBlindBoxRemoveDoesNotRepublishUnpublishedTemplate(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)
	fixture := newBlindBoxFixture(t)
	ctx := context.Background()

	cat := templateByTitle(t, "小猫咪")
	item, err := fixture.admin.AddToPool(ctx, cat.ID, 1, 0)
	if err != nil {
		t.Fatalf("AddToPool failed: %v", err)
	}
	db.DB.Model(&model.Template{}).Where("id = ?", cat.ID).Update("status", dao.StatusUnpublished)

	if err := fixture.admin.RemoveFromPool(ctx, item.ID); err != nil {
		t.Fatalf("RemoveFromPool failed: %v", err)
	}
	if status := templateStatus(t, cat.ID); status != dao.StatusUnpublished {
		t.Fatalf("removing a pool entry must not republish an unpublished template, got status %d", status)
	}
}

func TestBlindBoxPooledTemplateCannotBeUnpublished(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)
	fixture := newBlindBoxFixture(t)
	ctx := context.Background()

	cat := templateByTitle(t, "小猫咪")
	if _, err := fixture.admin.AddToPool(ctx, cat.ID, 1, 0); err != nil {
		t.Fatalf("AddToPool failed: %v", err)
	}

	if err := fixture.admin.UnpublishTemplate(ctx, cat.ID, "下架试试"); err == nil {
		t.Fatal("unpublishing a pooled template must be rejected so the operator removes it first")
	}
	if status := templateStatus(t, cat.ID); status != dao.StatusBlindBoxOnly {
		t.Fatalf("rejected unpublish must leave status untouched, got %d", status)
	}
}

func TestBlindBoxAdminEditKeepsBlindBoxStatus(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)
	fixture := newBlindBoxFixture(t)
	ctx := context.Background()

	cat := templateByTitle(t, "小猫咪")
	if _, err := fixture.admin.AddToPool(ctx, cat.ID, 1, 0); err != nil {
		t.Fatalf("AddToPool failed: %v", err)
	}

	if err := fixture.admin.UpdateTemplate(ctx, cat.ID, template.UpdatePayload{
		Title:       "小猫咪改名",
		CategoryID:  cat.CategoryID,
		Width:       cat.Width,
		Height:      cat.Height,
		PatternData: cat.PatternData,
	}); err != nil {
		t.Fatalf("UpdateTemplate failed: %v", err)
	}
	if status := templateStatus(t, cat.ID); status != dao.StatusBlindBoxOnly {
		t.Fatalf("editing must not knock status back to published, got %d", status)
	}
	if updated := templateByTitle(t, "小猫咪改名"); updated.ID != cat.ID {
		t.Fatalf("expected the edit to land on template %d, got %d", cat.ID, updated.ID)
	}
}

func TestBlindBoxPoolAdminHTTPRoutes(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)

	passwordHash, err := adminauth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	previousConfig := conf.GlobalConfig
	conf.GlobalConfig = &conf.Config{Admin: testAdminConfig(passwordHash)}
	t.Cleanup(func() { conf.GlobalConfig = previousConfig })

	templateDAO := dao.NewTemplateDAO()
	handler := newTestPortalHandler(
		testAdminConfig(passwordHash),
		newMemoryObjectStorage("https://cdn.example.test"),
		templateDAO,
		newTestSubmissionService(),
	)
	token := adminPortalLogin(t, handler, "operator", "correct horse battery staple")
	call := func(method, path string, body interface{}) *httptest.ResponseRecorder {
		t.Helper()
		var reader *bytes.Reader
		if body != nil {
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			reader = bytes.NewReader(encoded)
		} else {
			reader = bytes.NewReader(nil)
		}
		request := httptest.NewRequest(method, path, reader)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	cat := templateByTitle(t, "小猫咪")
	catID := strconv.FormatUint(cat.ID, 10)

	added := call(http.MethodPost, "/api/v1/admin/blind-box-pool", map[string]interface{}{
		"templateId": catID, "weight": 10, "sortOrder": 1,
	})
	if added.Code != http.StatusOK {
		t.Fatalf("add to pool expected 200, got %d: %s", added.Code, added.Body.String())
	}
	var addResult struct {
		Item struct {
			ItemID string `json:"itemId"`
			Weight int    `json:"weight"`
		} `json:"item"`
	}
	if err := json.Unmarshal(added.Body.Bytes(), &addResult); err != nil {
		t.Fatalf("decode add response: %v", err)
	}
	if addResult.Item.ItemID == "" || addResult.Item.Weight != 10 {
		t.Fatalf("unexpected add response: %s", added.Body.String())
	}

	// 重复入池走 InvalidArgument，映射到 400。
	if duplicate := call(http.MethodPost, "/api/v1/admin/blind-box-pool", map[string]interface{}{
		"templateId": catID, "weight": 3,
	}); duplicate.Code != http.StatusBadRequest {
		t.Fatalf("duplicate add expected 400, got %d: %s", duplicate.Code, duplicate.Body.String())
	}

	listed := call(http.MethodGet, "/api/v1/admin/blind-box-pool", nil)
	if listed.Code != http.StatusOK {
		t.Fatalf("list pool expected 200, got %d: %s", listed.Code, listed.Body.String())
	}
	if !strings.Contains(listed.Body.String(), "小猫咪") {
		t.Fatalf("pool list should carry the template title: %s", listed.Body.String())
	}

	// 后台图纸列表必须仍然看得到盲盒图纸，并标出 visibility。
	templates := call(http.MethodGet, "/api/v1/admin/templates", nil)
	if templates.Code != http.StatusOK {
		t.Fatalf("admin template list expected 200, got %d: %s", templates.Code, templates.Body.String())
	}
	if !strings.Contains(templates.Body.String(), `"visibility":"blind_box"`) {
		t.Fatalf("admin template list should mark pooled templates as blind_box: %s", templates.Body.String())
	}

	// 只传 weight：sortOrder 不能被清零。
	if updated := call(http.MethodPut, "/api/v1/admin/blind-box-pool/"+addResult.Item.ItemID, map[string]interface{}{
		"weight": 5,
	}); updated.Code != http.StatusOK {
		t.Fatalf("update pool item expected 200, got %d: %s", updated.Code, updated.Body.String())
	}
	var item model.BlindBoxPoolItem
	if err := db.DB.Where("template_id = ?", cat.ID).First(&item).Error; err != nil {
		t.Fatalf("reload pool item: %v", err)
	}
	if item.Weight != 5 || item.SortOrder != 1 {
		t.Fatalf("partial update must leave sortOrder alone, got weight=%d sortOrder=%d", item.Weight, item.SortOrder)
	}

	// 在池中的图纸不能下架。
	if unpublished := call(http.MethodPost, "/api/v1/admin/templates/"+catID+"/unpublish", map[string]interface{}{
		"reason": "试试",
	}); unpublished.Code != http.StatusBadRequest {
		t.Fatalf("unpublishing a pooled template expected 400, got %d: %s", unpublished.Code, unpublished.Body.String())
	}

	if removed := call(http.MethodDelete, "/api/v1/admin/blind-box-pool/"+addResult.Item.ItemID, nil); removed.Code != http.StatusOK {
		t.Fatalf("remove pool item expected 200, got %d: %s", removed.Code, removed.Body.String())
	}
	if status := templateStatus(t, cat.ID); status != dao.StatusPublished {
		t.Fatalf("expected status=%d after removal, got %d", dao.StatusPublished, status)
	}
}

func TestBlindBoxAdminCategoryCarriesBlindBoxFlag(t *testing.T) {
	SetupTestDB(t)

	passwordHash, err := adminauth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	previousConfig := conf.GlobalConfig
	conf.GlobalConfig = &conf.Config{Admin: testAdminConfig(passwordHash)}
	t.Cleanup(func() { conf.GlobalConfig = previousConfig })

	handler := newTestPortalHandler(
		testAdminConfig(passwordHash),
		newMemoryObjectStorage("https://cdn.example.test"),
		dao.NewTemplateDAO(),
		newTestSubmissionService(),
	)
	token := adminPortalLogin(t, handler, "operator", "correct horse battery staple")

	body, err := json.Marshal(map[string]interface{}{"name": "盲盒限定", "isBlindBox": true})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/template-categories", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, request)
	if created.Code != http.StatusOK {
		t.Fatalf("create category expected 200, got %d: %s", created.Code, created.Body.String())
	}
	if !strings.Contains(created.Body.String(), `"isBlindBox":true`) {
		t.Fatalf("create response should echo isBlindBox: %s", created.Body.String())
	}

	// 后台分类列表要包含盲盒分类，否则发布时选不到它。
	request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/template-categories", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	listed := httptest.NewRecorder()
	handler.ServeHTTP(listed, request)
	if listed.Code != http.StatusOK {
		t.Fatalf("list categories expected 200, got %d: %s", listed.Code, listed.Body.String())
	}
	if !strings.Contains(listed.Body.String(), "盲盒限定") {
		t.Fatalf("admin category list must include blind box categories: %s", listed.Body.String())
	}
}

func TestListBlindBoxRecordsReturnsDrawnTemplate(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)
	fixture := newBlindBoxFixture(t)
	ctx := context.Background()

	cat := templateByTitle(t, "小猫咪")
	if _, err := fixture.admin.AddToPool(ctx, cat.ID, 1, 0); err != nil {
		t.Fatalf("AddToPool failed: %v", err)
	}

	if _, _, err := fixture.svc.DrawBlindBox(ctx, 1); err != nil {
		t.Fatalf("DrawBlindBox failed: %v", err)
	}

	// 回归 GetByIDs 对 status=2 的放行：只查 status=1 会让开盒历史恒为空。
	templates, total, err := fixture.svc.ListBlindBoxRecords(ctx, 1, 1, 20)
	if err != nil {
		t.Fatalf("ListBlindBoxRecords failed: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected total=1, got %d", total)
	}
	if len(templates) != 1 || templates[0].ID != cat.ID {
		t.Fatalf("expected drawn template %d, got %+v", cat.ID, templates)
	}
}
