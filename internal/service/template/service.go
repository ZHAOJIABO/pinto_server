package template

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	apperr "github.com/zhaojiabo/bobobeads_server/internal/errors"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"gorm.io/gorm"
)

type Service struct {
	templateDAO *dao.TemplateDAO
	blindBoxDAO *dao.BlindBoxRecordDAO
	poolDAO     *dao.BlindBoxPoolDAO
	quotaDAO    *dao.BlindBoxQuotaDAO
}

func NewService(templateDAO *dao.TemplateDAO, blindBoxDAO *dao.BlindBoxRecordDAO, poolDAO *dao.BlindBoxPoolDAO, quotaDAO *dao.BlindBoxQuotaDAO) *Service {
	return &Service{templateDAO: templateDAO, blindBoxDAO: blindBoxDAO, poolDAO: poolDAO, quotaDAO: quotaDAO}
}

type ListInput struct {
	CategoryID int
	Scene      string
	Keyword    string
	Page       int
	PageSize   int
}

func (s *Service) ListCategories(ctx context.Context) ([]*model.TemplateCategory, []int64, error) {
	categories, err := s.ListNavCategories(ctx)
	if err != nil {
		return nil, nil, err
	}
	counts := make([]int64, len(categories))
	for i, c := range categories {
		count, _ := s.templateDAO.CountByCategory(ctx, c.ID)
		counts[i] = count
	}
	return categories, counts, nil
}

// ListCategoriesForAdmin 与 ListCategories 的区别只在分类集合：后台要能看到并选中
// 盲盒专用分类，C 端导航不能。
func (s *Service) ListCategoriesForAdmin(ctx context.Context) ([]*model.TemplateCategory, []int64, error) {
	categories, err := s.ListActiveCategories(ctx)
	if err != nil {
		return nil, nil, err
	}
	counts := make([]int64, len(categories))
	for i, c := range categories {
		count, _ := s.templateDAO.CountByCategory(ctx, c.ID)
		counts[i] = count
	}
	return categories, counts, nil
}

// ListNavCategories 是 C 端分类导航用的，排除盲盒专用分类。
func (s *Service) ListNavCategories(ctx context.Context) ([]*model.TemplateCategory, error) {
	return s.templateDAO.ListCategories(ctx)
}

// ListActiveCategories 返回全部启用分类（含盲盒专用）。收藏分类聚合依赖它：用户抽到
// 并收藏的盲盒图纸要能在收藏页显示出所属的盲盒分类。后台分类管理也用它。
func (s *Service) ListActiveCategories(ctx context.Context) ([]*model.TemplateCategory, error) {
	return s.templateDAO.ListAllActiveCategories(ctx)
}

func (s *Service) ListActiveCategoryNames(ctx context.Context, categoryIDs []int) (map[int]string, error) {
	return s.templateDAO.ListActiveCategoryNames(ctx, categoryIDs)
}

func (s *Service) ListTemplates(ctx context.Context, input ListInput) ([]*model.Template, int64, error) {
	offset := (input.Page - 1) * input.PageSize

	if input.Keyword != "" {
		return s.templateDAO.ListByKeyword(ctx, input.Keyword, offset, input.PageSize)
	}
	if input.CategoryID > 0 {
		return s.templateDAO.ListByCategory(ctx, input.CategoryID, offset, input.PageSize)
	}
	if input.Scene == "home" {
		return s.templateDAO.ListByScene(ctx, input.Scene, offset, input.PageSize)
	}
	return s.templateDAO.ListByCategory(ctx, input.CategoryID, offset, input.PageSize)
}

func (s *Service) ListPublishedTemplates(ctx context.Context, page, pageSize int) ([]*model.Template, int64, error) {
	offset := (page - 1) * pageSize
	return s.templateDAO.ListPublished(ctx, offset, pageSize)
}

// ListPublishedTemplatesForAdmin 额外返回盲盒专属图纸（status=2），并带上 status 列，
// 让后台列表能标出哪些图纸只在盲盒里出现。
func (s *Service) ListPublishedTemplatesForAdmin(ctx context.Context, page, pageSize int) ([]*model.Template, int64, error) {
	offset := (page - 1) * pageSize
	return s.templateDAO.ListPublishedForAdmin(ctx, offset, pageSize)
}

// TemplateVisibility 把 status 翻译成后台契约里的字符串。放在 service 层是为了不让
// api 包为一个状态常量去 import dao——那会打穿现有的分层。
func (s *Service) TemplateVisibility(tpl *model.Template) string {
	if tpl.Status == dao.StatusBlindBoxOnly {
		return "blind_box"
	}
	return "public"
}

func (s *Service) GetTemplate(ctx context.Context, templateID uint64) (*model.Template, error) {
	if templateID == 0 {
		return nil, apperr.InvalidArgument("invalid template_id")
	}
	tpl, err := s.templateDAO.GetByID(ctx, templateID)
	if err != nil {
		return nil, apperr.NotFound("template not found")
	}
	s.templateDAO.IncrementDownload(ctx, templateID)
	return tpl, nil
}

func (s *Service) FavoriteTemplate(ctx context.Context, userID, templateID uint64) (int, error) {
	if templateID == 0 {
		return 0, apperr.InvalidArgument("invalid template_id")
	}
	_, err := s.templateDAO.GetByID(ctx, templateID)
	if err != nil {
		return 0, apperr.NotFound("template not found")
	}

	existing, err := s.templateDAO.GetFavorite(ctx, userID, templateID)
	if err != nil {
		return 0, apperr.Internal("check favorite", err)
	}
	if existing != nil {
		tpl, _ := s.templateDAO.GetByID(ctx, templateID)
		return tpl.FavoriteCount, nil
	}

	fav := &model.TemplateFavorite{UserID: userID, TemplateID: templateID}
	if err := s.templateDAO.CreateFavorite(ctx, fav); err != nil {
		tpl, _ := s.templateDAO.GetByID(ctx, templateID)
		return tpl.FavoriteCount, nil
	}
	s.templateDAO.IncrementFavoriteCount(ctx, templateID)

	tpl, _ := s.templateDAO.GetByID(ctx, templateID)
	return tpl.FavoriteCount, nil
}

func (s *Service) UnfavoriteTemplate(ctx context.Context, userID, templateID uint64) (int, error) {
	if templateID == 0 {
		return 0, apperr.InvalidArgument("invalid template_id")
	}
	_, err := s.templateDAO.GetByID(ctx, templateID)
	if err != nil {
		return 0, apperr.NotFound("template not found")
	}

	existing, err := s.templateDAO.GetFavorite(ctx, userID, templateID)
	if err != nil {
		return 0, apperr.Internal("check favorite", err)
	}
	if existing == nil {
		tpl, _ := s.templateDAO.GetByID(ctx, templateID)
		return tpl.FavoriteCount, nil
	}

	s.templateDAO.DeleteFavorite(ctx, userID, templateID)
	s.templateDAO.DecrementFavoriteCount(ctx, templateID)

	tpl, _ := s.templateDAO.GetByID(ctx, templateID)
	return tpl.FavoriteCount, nil
}

func (s *Service) ListFavoriteTemplates(ctx context.Context, userID uint64, categoryID, page, pageSize int) ([]*model.Template, int64, error) {
	offset := (page - 1) * pageSize
	return s.templateDAO.ListFavoriteTemplates(ctx, userID, categoryID, offset, pageSize)
}

func (s *Service) BatchGetFavorited(ctx context.Context, userID uint64, templateIDs []uint64) (map[uint64]bool, error) {
	return s.templateDAO.BatchGetFavorited(ctx, userID, templateIDs)
}

// ListFavoriteCategories 只返回用户确实有收藏的分类，计数为该用户在分类下的收藏数量。
func (s *Service) ListFavoriteCategories(ctx context.Context, userID uint64) ([]*model.TemplateCategory, []int64, error) {
	rows, err := s.templateDAO.CountFavoritesByCategory(ctx, userID)
	if err != nil {
		return nil, nil, apperr.Internal("count favorites by category", err)
	}
	if len(rows) == 0 {
		return nil, nil, nil
	}

	countMap := make(map[int]int64, len(rows))
	for _, r := range rows {
		countMap[r.CategoryID] = r.Count
	}

	categories, err := s.ListActiveCategories(ctx)
	if err != nil {
		return nil, nil, apperr.Internal("list categories", err)
	}

	result := make([]*model.TemplateCategory, 0, len(countMap))
	counts := make([]int64, 0, len(countMap))
	for _, c := range categories {
		if count, ok := countMap[c.ID]; ok {
			result = append(result, c)
			counts = append(counts, count)
		}
	}
	return result, counts, nil
}

func (s *Service) SplitTags(tags string) []string {
	return s.templateDAO.SplitTags(tags)
}

// GetRandomTemplate 从盲盒奖池按权重抽一张图纸。
//
// 这是纯粹的"挑一张"，不占用户的每日额度也不写开盒记录。C 端的抽盲盒入口必须走
// DrawBlindBox，直接调这里会绕过每日次数限制。
//
// 加权抽取放在 Go 侧：候选集只有 (template_id, weight) 两列、几十到几百行，比在 SQL
// 里 ORDER BY RAND() 便宜，也不依赖 MySQL 专有语法（测试跑在 SQLite 上）。
//
// 并发正确性依赖 math/rand/v2 顶层的 rand.IntN——它是 per-P 状态、goroutine 安全、
// 自动播种的。不要换成 rand.New(rand.NewSource(time.Now().UnixNano()))：那个 *Rand
// 不并发安全，且同毫秒内的多个请求会撞到同一个种子。
//
// 空池不回退到全库随机：那会让普通图纸混进盲盒、破坏运营语义，也会掩盖"忘配奖池"
// 这种线上故障。
func (s *Service) GetRandomTemplate(ctx context.Context) (*model.Template, error) {
	candidates, err := s.poolDAO.ListActiveCandidates(ctx)
	if err != nil {
		return nil, apperr.Internal("list blind box pool", err)
	}
	if len(candidates) == 0 {
		return nil, apperr.NotFound("blind box pool is empty")
	}

	total := 0
	for _, c := range candidates {
		total += c.Weight
	}

	n := rand.IntN(total)
	picked := candidates[len(candidates)-1].TemplateID
	for _, c := range candidates {
		if n < c.Weight {
			picked = c.TemplateID
			break
		}
		n -= c.Weight
	}

	tpl, err := s.templateDAO.GetByID(ctx, picked)
	if err != nil {
		return nil, apperr.NotFound("template not found")
	}
	return tpl, nil
}

// 盲盒每日额度。
//
// businessLocation 是算"今天"用的固定时区。不能用 time.Now() 的本地时区：MySQL 的 DSN 是
// loc=Local，服务器时区一变每日额度的分界就跟着漂。也不能用 Truncate(24*time.Hour)
// （internal/dao/generation.go:81 是那种写法）——Truncate 按 Unix 纪元的绝对时间取整，得到
// 的是 UTC 零点而不是本地零点。
//
// 用 FixedZone 而不是 LoadLocation("Asia/Shanghai")：中国不实行夏令时，固定偏移在语义上
// 完全等价，且不依赖容器里装了 tzdata（精简镜像常常没有）。
var businessLocation = time.FixedZone("CST", 8*60*60)

// TODO(上线前改回 1): 5 是开发和内测期的临时值，为了能反复抽取验证流程，不是产品设定。
const defaultDailyDrawLimit = 5

func drawDateOf(t time.Time) string {
	return t.In(businessLocation).Format("2006-01-02")
}

func nextResetAt(t time.Time) time.Time {
	local := t.In(businessLocation)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, businessLocation).AddDate(0, 0, 1)
}

// DailyDrawQuota 是用户当天的盲盒额度状况，客户端靠它显示"今日剩余 N 次"和重置时间。
type DailyDrawQuota struct {
	Limit     int
	Used      int
	Remaining int
	ResetAt   time.Time
}

// dailyDrawLimit 是每日额度的唯一来源。将来加会员多次机会时只改这里：按 userID 查
// bb_subscription，生效中的会员返回更大的值。签到赠送的机会不要塞进这个返回值——那是
// 可累积的资产，和"当天用不掉就作废"的免费次数语义不同，应该另起一套机会券余额。
func (s *Service) dailyDrawLimit(_ context.Context, _ uint64) int {
	return defaultDailyDrawLimit
}

func (s *Service) GetDailyDrawQuota(ctx context.Context, userID uint64) (*DailyDrawQuota, error) {
	now := time.Now()
	limit := s.dailyDrawLimit(ctx, userID)
	used, err := s.quotaDAO.GetUsedCount(ctx, userID, drawDateOf(now))
	if err != nil {
		return nil, apperr.Internal("get blind box quota", err)
	}
	return newDailyDrawQuota(limit, used, now), nil
}

func newDailyDrawQuota(limit, used int, now time.Time) *DailyDrawQuota {
	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}
	return &DailyDrawQuota{Limit: limit, Used: used, Remaining: remaining, ResetAt: nextResetAt(now)}
}

// DrawBlindBox 是 C 端抽盲盒的唯一入口：挑图纸、占当日额度、写开盒记录。
//
// 先挑图纸再占额度：奖池为空时直接返回，不会先吃掉用户当天的机会。占额度和写记录在同一个
// 事务里，写记录失败会把额度退回去——宁可让用户重试，也不能让一次数据库故障吃掉当天唯一
// 的机会。
//
// 额度判定不放在这里而在 quotaDAO.ConsumeTx 的 UPDATE 条件里，是为了让并发请求由数据库
// 串行化；在 Go 侧先读后判会让两个同时到达的请求都通过。
func (s *Service) DrawBlindBox(ctx context.Context, userID uint64) (*model.Template, *DailyDrawQuota, error) {
	tpl, err := s.GetRandomTemplate(ctx)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now()
	drawDate := drawDateOf(now)
	limit := s.dailyDrawLimit(ctx, userID)

	err = db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		consumed, err := s.quotaDAO.ConsumeTx(tx, userID, drawDate, limit)
		if err != nil {
			return apperr.Internal("consume blind box quota", err)
		}
		if !consumed {
			return apperr.BlindBoxQuotaUsedUp(limit)
		}
		return s.blindBoxDAO.CreateTx(tx, &model.BlindBoxRecord{UserID: userID, TemplateID: tpl.ID})
	})
	if err != nil {
		return nil, nil, err
	}

	used, err := s.quotaDAO.GetUsedCount(ctx, userID, drawDate)
	if err != nil {
		// 额度已经扣成功了，只是回读失败。抽奖结果不该因此丢掉，按刚扣完的值兜底。
		used = limit
	}
	return tpl, newDailyDrawQuota(limit, used, now), nil
}

func (s *Service) ListBlindBoxRecords(ctx context.Context, userID uint64, page, pageSize int) ([]*model.Template, int64, error) {
	offset := (page - 1) * pageSize
	records, total, err := s.blindBoxDAO.ListByUserID(ctx, userID, offset, pageSize)
	if err != nil {
		return nil, 0, apperr.Internal("list blind box records", err)
	}
	if len(records) == 0 {
		return nil, 0, nil
	}
	templateIDs := make([]uint64, 0, len(records))
	for _, r := range records {
		templateIDs = append(templateIDs, r.TemplateID)
	}
	templates, err := s.templateDAO.GetByIDs(ctx, templateIDs)
	if err != nil {
		return nil, 0, apperr.Internal("get templates", err)
	}
	templateMap := make(map[uint64]*model.Template, len(templates))
	for _, t := range templates {
		templateMap[t.ID] = t
	}
	result := make([]*model.Template, 0, len(records))
	for _, r := range records {
		if t, ok := templateMap[r.TemplateID]; ok {
			result = append(result, t)
		}
	}
	return result, total, nil
}
