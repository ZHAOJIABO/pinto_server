package test

import (
	"context"
	"testing"
	"time"

	"github.com/zhaojiabo/bobobeads_server/internal/api"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/middleware"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
)

func seedSingleItemPool(t *testing.T, fixture *blindBoxFixture) uint64 {
	t.Helper()
	seedTemplateData(t)
	cat := templateByTitle(t, "小猫咪")
	if _, err := fixture.admin.AddToPool(context.Background(), cat.ID, 1, 0); err != nil {
		t.Fatalf("AddToPool failed: %v", err)
	}
	return cat.ID
}

// dailyDrawLimit 从 service 问出当天上限，用例一律不写死次数。每日次数是会被调整的
// 运营参数（开发期临时放宽、将来会员多次），断言跟着它走才不会一改常量就红一片。
func dailyDrawLimit(t *testing.T, fixture *blindBoxFixture, userID uint64) int {
	t.Helper()
	quota, err := fixture.svc.GetDailyDrawQuota(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetDailyDrawQuota failed: %v", err)
	}
	if quota.Limit < 1 {
		t.Fatalf("daily draw limit must be at least 1, got %d", quota.Limit)
	}
	return quota.Limit
}

func blindBoxRecordCount(t *testing.T, userID uint64) int64 {
	t.Helper()
	var n int64
	db.DB.Model(&model.BlindBoxRecord{}).Where("user_id = ?", userID).Count(&n)
	return n
}

func TestBlindBoxDrawRejectedAfterQuotaUsedUp(t *testing.T) {
	SetupTestDB(t)
	fixture := newBlindBoxFixture(t)
	templateID := seedSingleItemPool(t, fixture)
	ctx := context.Background()
	limit := dailyDrawLimit(t, fixture, 7)

	for i := 0; i < limit; i++ {
		tpl, quota, err := fixture.svc.DrawBlindBox(ctx, 7)
		if err != nil {
			t.Fatalf("draw %d/%d failed: %v", i+1, limit, err)
		}
		if tpl.ID != templateID {
			t.Fatalf("expected template %d, got %d", templateID, tpl.ID)
		}
		if quota.Used != i+1 || quota.Remaining != limit-i-1 {
			t.Fatalf("after draw %d expected used=%d remaining=%d, got %d/%d",
				i+1, i+1, limit-i-1, quota.Used, quota.Remaining)
		}
	}

	_, _, err := fixture.svc.DrawBlindBox(ctx, 7)
	assertErrCode(t, err, apperr.CodeBlindBoxQuotaUsedUp)

	// 被拒的抽取不能留下开盒记录，否则历史列表会出现用户没抽到的图纸。
	if n := blindBoxRecordCount(t, 7); n != int64(limit) {
		t.Fatalf("expected exactly %d blind box records, got %d", limit, n)
	}

	// 额度是按用户隔离的，另一个用户当天仍然能抽。
	if _, _, err := fixture.svc.DrawBlindBox(ctx, 8); err != nil {
		t.Fatalf("another user's draw must not be blocked: %v", err)
	}
}

// 额度只允许被占用 limit 次，多余的尝试既不加计数也不写记录。这条断言的是
// ConsumeTx 的 used_count < limit 条件，而不是 Go 侧的先读后判。
func TestBlindBoxQuotaNeverExceedsLimit(t *testing.T) {
	SetupTestDB(t)
	fixture := newBlindBoxFixture(t)
	seedSingleItemPool(t, fixture)
	ctx := context.Background()
	limit := dailyDrawLimit(t, fixture, 7)

	succeeded := 0
	for i := 0; i < limit+3; i++ {
		if _, _, err := fixture.svc.DrawBlindBox(ctx, 7); err == nil {
			succeeded++
		}
	}
	if succeeded != limit {
		t.Fatalf("expected exactly %d successful draws out of %d, got %d", limit, limit+3, succeeded)
	}

	var used int
	if err := db.DB.Model(&model.BlindBoxDailyQuota{}).Where("user_id = ?", 7).
		Pluck("used_count", &used).Error; err != nil {
		t.Fatalf("read used_count: %v", err)
	}
	if used != limit {
		t.Fatalf("expected used_count=%d, got %d", limit, used)
	}
	if n := blindBoxRecordCount(t, 7); n != int64(limit) {
		t.Fatalf("expected exactly %d blind box records, got %d", limit, n)
	}
}

// 空池必须在占额度之前就失败，否则运营忘配奖池会白吃掉所有用户当天的机会。
func TestBlindBoxEmptyPoolDoesNotConsumeQuota(t *testing.T) {
	SetupTestDB(t)
	seedTemplateData(t)
	fixture := newBlindBoxFixture(t)
	ctx := context.Background()
	limit := dailyDrawLimit(t, fixture, 7)

	if _, _, err := fixture.svc.DrawBlindBox(ctx, 7); err == nil {
		t.Fatal("expected empty pool to fail the draw")
	}

	var rows int64
	db.DB.Model(&model.BlindBoxDailyQuota{}).Where("user_id = ?", 7).Count(&rows)
	if rows != 0 {
		t.Fatalf("failed draw must not create a quota row, got %d", rows)
	}

	quota, err := fixture.svc.GetDailyDrawQuota(ctx, 7)
	if err != nil {
		t.Fatalf("GetDailyDrawQuota failed: %v", err)
	}
	if quota.Remaining != limit {
		t.Fatalf("expected remaining=%d after a failed draw, got %d", limit, quota.Remaining)
	}

	// 补上奖池后同一个用户当天还能抽。
	cat := templateByTitle(t, "小猫咪")
	if _, err := fixture.admin.AddToPool(ctx, cat.ID, 1, 0); err != nil {
		t.Fatalf("AddToPool failed: %v", err)
	}
	if _, _, err := fixture.svc.DrawBlindBox(ctx, 7); err != nil {
		t.Fatalf("draw after filling the pool failed: %v", err)
	}
}

// 跨日重置：把当日额度行改成过去的日期，等价于时间走到了第二天。
func TestBlindBoxQuotaResetsNextDay(t *testing.T) {
	SetupTestDB(t)
	fixture := newBlindBoxFixture(t)
	seedSingleItemPool(t, fixture)
	ctx := context.Background()
	limit := dailyDrawLimit(t, fixture, 7)

	for i := 0; i < limit; i++ {
		if _, _, err := fixture.svc.DrawBlindBox(ctx, 7); err != nil {
			t.Fatalf("draw %d/%d failed: %v", i+1, limit, err)
		}
	}
	if _, _, err := fixture.svc.DrawBlindBox(ctx, 7); err == nil {
		t.Fatal("expected the draw past the daily limit to be rejected")
	}

	yesterday := time.Now().In(time.FixedZone("CST", 8*60*60)).AddDate(0, 0, -1).Format("2006-01-02")
	if err := db.DB.Model(&model.BlindBoxDailyQuota{}).Where("user_id = ?", 7).
		UpdateColumn("draw_date", yesterday).Error; err != nil {
		t.Fatalf("backdate quota row: %v", err)
	}

	quota, err := fixture.svc.GetDailyDrawQuota(ctx, 7)
	if err != nil {
		t.Fatalf("GetDailyDrawQuota failed: %v", err)
	}
	if quota.Used != 0 || quota.Remaining != limit {
		t.Fatalf("expected a fresh quota for the new day, got used=%d remaining=%d", quota.Used, quota.Remaining)
	}

	if _, _, err := fixture.svc.DrawBlindBox(ctx, 7); err != nil {
		t.Fatalf("draw on the next day failed: %v", err)
	}
	if n := blindBoxRecordCount(t, 7); n != int64(limit+1) {
		t.Fatalf("expected %d blind box records across two days, got %d", limit+1, n)
	}
}

func TestBlindBoxQuotaEndpoints(t *testing.T) {
	SetupTestDB(t)
	fixture := newBlindBoxFixture(t)
	seedSingleItemPool(t, fixture)
	handler := api.NewTemplateHandler(fixture.svc)
	ctx := context.WithValue(context.Background(), middleware.UserIDKey, uint64(7))
	limit := dailyDrawLimit(t, fixture, 7)

	before, err := handler.GetBlindBoxQuota(ctx, &pb.GetBlindBoxQuotaRequest{})
	if err != nil || before.Header.Code != 0 {
		t.Fatalf("GetBlindBoxQuota failed: err=%v header=%#v", err, before.Header)
	}
	if before.Quota.DailyLimit != int32(limit) || before.Quota.Used != 0 || before.Quota.Remaining != int32(limit) {
		t.Fatalf("unexpected quota before drawing: %#v", before.Quota)
	}
	if before.Quota.ResetAt <= time.Now().Unix() {
		t.Fatalf("reset_at must be in the future, got %d", before.Quota.ResetAt)
	}

	// 抽取响应自带额度，客户端不需要再调一次 GetBlindBoxQuota 才能刷新按钮状态。
	for i := 0; i < limit; i++ {
		drawn, err := handler.RandomTemplate(ctx, &pb.RandomTemplateRequest{})
		if err != nil || drawn.Header.Code != 0 {
			t.Fatalf("RandomTemplate %d/%d failed: err=%v header=%#v", i+1, limit, err, drawn.Header)
		}
		if drawn.Quota == nil || drawn.Quota.Used != int32(i+1) || drawn.Quota.Remaining != int32(limit-i-1) {
			t.Fatalf("unexpected quota on draw %d: %#v", i+1, drawn.Quota)
		}
	}

	blocked, err := handler.RandomTemplate(ctx, &pb.RandomTemplateRequest{})
	if err != nil {
		t.Fatalf("RandomTemplate returned a transport error: %v", err)
	}
	if blocked.Header.Code != apperr.CodeBlindBoxQuotaUsedUp {
		t.Fatalf("expected code %d, got %d (%s)",
			apperr.CodeBlindBoxQuotaUsedUp, blocked.Header.Code, blocked.Header.Message)
	}

	after, err := handler.GetBlindBoxQuota(ctx, &pb.GetBlindBoxQuotaRequest{})
	if err != nil || after.Header.Code != 0 {
		t.Fatalf("GetBlindBoxQuota failed: err=%v header=%#v", err, after.Header)
	}
	if after.Quota.Used != int32(limit) || after.Quota.Remaining != 0 {
		t.Fatalf("unexpected quota after drawing: %#v", after.Quota)
	}
}
