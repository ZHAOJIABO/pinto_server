# Flutter 后端适配 P0 Review 修复方案

本文档基于 `doc/flutter_backend_adaptation_plan.md` 后续代码实现的 review 结果整理，目标是修复当前仍未完全达标的 P0 问题，并补齐测试迁移与失败用例。

## 1. 修复目标

本轮修复完成后应满足：

- 私有作品只能本人读取、删除、更新。
- 所有积分变更都以 `bb_credit_account` 为唯一余额来源。
- `CreateGeneration` 在并发相同 `client_request_id` 下不重复扣费、不重复创建 generation。
- `PatternData` 必须有真实像素数据，且保存/读取不丢数据。
- `go test ./...` 通过，并覆盖关键失败场景。

## 2. 问题一：Work 权限校验缺失

### 2.1 当前问题

涉及文件：

- `internal/api/work.go`
- `internal/service/work/service.go`
- `internal/dao/work.go`
- `internal/test/work_test.go`

当前 `GetWork` 只按 `work_id` 查询，没有校验 `user_id`。这会导致任意登录用户只要拿到作品 ID，就可以读取其他用户的完整作品详情和 `pattern_data`。

当前风险：

- 私有作品泄露。
- 图纸完整像素数据泄露。
- Flutter 端无法依赖后端做权限隔离。

### 2.2 修复方案

API 层读取当前用户：

```go
func (h *WorkHandler) GetWork(ctx context.Context, req *pb.GetWorkRequest) (*pb.GetWorkResponse, error) {
	userID := middleware.GetUserID(ctx)

	workID, err := strconv.ParseUint(req.WorkId, 10, 64)
	if err != nil {
		return &pb.GetWorkResponse{
			Header: errHeaderCtx(ctx, apperr.InvalidArgument("invalid work_id")),
		}, nil
	}

	w, err := h.workService.GetWork(ctx, userID, workID)
	if err != nil {
		return &pb.GetWorkResponse{Header: errHeaderCtx(ctx, err)}, nil
	}

	return &pb.GetWorkResponse{
		Header:      okHeaderCtx(ctx),
		Work:        workToProto(w),
		PatternData: work.JSONMapToPatternData(w.PatternData),
	}, nil
}
```

Service 层传入 `userID`：

```go
func (s *Service) GetWork(ctx context.Context, userID, workID uint64) (*model.Work, error) {
	w, err := s.workDAO.GetByIDForUser(ctx, workID, userID)
	if err != nil {
		return nil, apperr.NotFound("work not found")
	}
	return w, nil
}
```

DAO 层使用 `id + user_id` 查询：

```go
func (d *WorkDAO) GetByIDForUser(ctx context.Context, id, userID uint64) (*model.Work, error) {
	var work model.Work
	err := d.DB(ctx).Where("id = ? AND user_id = ?", id, userID).First(&work).Error
	return &work, err
}
```

### 2.3 注意事项

社区公开作品详情不要直接复用私有 `WorkService.GetWork`。

建议：

- 私有作品接口：必须校验 `user_id`。
- 社区公开详情：通过 community post 关系读取已发布作品。
- 如果需要公共读取能力，新增 `GetPublishedWork` 或放在 community service 内实现。

### 2.4 测试用例

新增：

```go
func TestGetWork_RejectsOtherUser(t *testing.T) {
	SetupTestDB(t)
	svc := work.NewService(dao.NewWorkDAO())
	ctx := context.Background()

	workID, err := svc.SaveWork(ctx, 1, &model.Work{Title: "private"}, validPatternData())
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.GetWork(ctx, 2, workID)
	if err == nil {
		t.Fatal("expected error when reading another user's work")
	}
}
```

验收：

- 用户 A 创建的 work，用户 B 调用 `GetWork` 返回 `not_found` 或 `forbidden`。
- 用户 A 正常读取自己的 work。

## 3. 问题二：积分账户一致性

### 3.1 当前问题

涉及文件：

- `internal/service/credit/service.go`
- `internal/dao/credit.go`
- `internal/model/subscribe.go`
- `internal/test/credit_test.go`

当前代码已经将余额读取切换到 `bb_credit_account`：

```go
func (d *CreditDAO) GetBalance(ctx context.Context, userID uint64) (int, error) {
	var account model.CreditAccount
	err := d.DB(ctx).Where("user_id = ?", userID).First(&account).Error
	...
	return account.Balance, err
}
```

但非事务版 `AddCredits` / `DeductCredits` 仍然只写 `bb_credit_transaction`，不会更新 `bb_credit_account`。

结果：

- 流水存在，但余额不变。
- 发放奖励、购买积分、邀请奖励等非 generation 场景会失效。
- 测试中 `AddCredits` 后 `GetBalance` 仍为 0。

### 3.2 修复方案

非事务方法也必须通过事务更新 account + transaction。

```go
func (s *Service) AddCredits(ctx context.Context, userID uint64, amount int, txType, refType, refID, desc string) error {
	if amount <= 0 {
		return apperr.InvalidArgument("amount must be positive")
	}

	return db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, err := s.AddCreditsTx(tx, userID, amount, txType, refType, refID, desc)
		return err
	})
}
```

```go
func (s *Service) DeductCredits(ctx context.Context, userID uint64, amount int, txType, refType, refID, desc string) error {
	if amount <= 0 {
		return apperr.InvalidArgument("amount must be positive")
	}

	return db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, err := s.DeductCreditsTx(tx, userID, amount, txType, refType, refID, desc)
		return err
	})
}
```

`DeductCreditsTx` 自身也要校验余额，防止后续直接调用 tx 方法绕过检查：

```go
func (s *Service) DeductCreditsTx(tx *gorm.DB, userID uint64, amount int, txType, refType, refID, desc string) (int, error) {
	account, err := s.GetAccountForUpdate(tx, userID)
	if err != nil {
		return 0, err
	}
	if account.Balance < amount {
		return 0, apperr.InsufficientCredits(account.Balance, amount)
	}

	account.Balance -= amount
	if err := s.creditDAO.UpdateAccount(tx, account); err != nil {
		return 0, err
	}

	t := &model.CreditTransaction{
		UserID:      userID,
		Amount:      -amount,
		Balance:     account.Balance,
		Type:        txType,
		RefType:     refType,
		RefID:       refID,
		Description: desc,
	}
	if err := s.creditDAO.CreateTransactionTx(tx, t); err != nil {
		return 0, err
	}

	return account.Balance, nil
}
```

### 3.3 重复流水建议

如果希望从数据库层防止重复退款，可以给 `CreditTransaction` 增加去重字段或唯一索引。

谨慎方案：

- 不对所有 `ref_type/ref_id/type` 强制唯一。
- 仅对有幂等需求的场景传入 `RequestID`。
- 退款、订单回调、邀请奖励等业务必须传稳定 `RequestID`。

如果短期只针对 generation 退款，可以先在业务层保证：

- 事务内锁定 generation。
- 只有 `status = pending` 才退款。
- 同一事务内更新状态和写退款流水。

### 3.4 测试用例

需要补充或修复：

```go
func TestAddAndDeductCredits(t *testing.T)
func TestInsufficientCredits(t *testing.T)
func TestListTransactions(t *testing.T)
func TestDeductCreditsTx_RejectsInsufficient(t *testing.T)
func TestAddCredits_CreatesAccountIfMissing(t *testing.T)
```

关键断言：

- `AddCredits(10)` 后 `GetBalance == 10`。
- 流水第一条 `balance == 10`。
- `DeductCredits(3)` 后 `GetBalance == 7`。
- 余额不足扣费返回 `CodeInsufficientCredit`。
- 余额不足时余额不变。
- 余额不足时不新增扣费流水。

## 4. 问题三：CreateGeneration 并发幂等

### 4.1 当前问题

涉及文件：

- `internal/model/generation.go`
- `internal/dao/generation.go`
- `internal/service/generation/service.go`
- `internal/test/generation_test.go`

当前 `ClientRequestID` 只是普通 index：

```go
ClientRequestID string `gorm:"type:varchar(64);index" json:"client_request_id"`
```

风险：

- 两个相同 `client_request_id` 的并发请求同时进入。
- 都在 `GetByUserRequestIDForUpdate` 查不到 existing。
- 都进入扣费和创建 generation。
- 最终可能重复扣费、重复创建。

### 4.2 数据库唯一索引

必须增加联合唯一索引：

```go
type Generation struct {
	BaseModel
	UserID          uint64 `gorm:"not null;uniqueIndex:uk_generation_user_request;index" json:"user_id"`
	GenerationID    string `gorm:"type:varchar(36);uniqueIndex;not null" json:"generation_id"`
	ClientRequestID string `gorm:"type:varchar(64);not null;uniqueIndex:uk_generation_user_request" json:"client_request_id"`
	...
}
```

迁移注意：

- 如果线上已有历史空 `client_request_id`，先补数据或清理 pending 数据。
- 代码层已经要求 `client_request_id required`，后续新数据必须非空。
- SQLite 和 MySQL 对唯一索引中空值行为不同，测试应全部传非空。

### 4.3 Duplicate key 处理

当前创建流程是：

1. 查 existing。
2. 扣费。
3. 创建 generation。

因为扣费和创建在同一 DB transaction 内，理论上 duplicate key 触发后事务回滚，扣费也会回滚。但仍建议显式处理 duplicate key，回查已存在 generation 并返回 duplicated。

示例：

```go
if err := s.generationDAO.CreateTx(tx, gen); err != nil {
	if isDuplicateKey(err) {
		existing, getErr := s.generationDAO.GetByUserRequestIDForUpdate(tx, userID, clientRequestID)
		if getErr != nil || existing == nil {
			return apperr.Internal("load duplicated generation", getErr)
		}
		result = s.createResultFromExistingTx(tx, userID, existing, true)
		return nil
	}
	return apperr.Internal("create generation", err)
}
```

需要增加 helper：

```go
func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicated key")
}
```

### 4.4 Existing 分支余额读取

当前 existing 分支使用 tx 外读余额：

```go
balance, _ := s.creditService.GetBalance(ctx, userID)
```

建议改为 tx 内读：

```go
account, err := s.creditService.GetAccountForUpdate(tx, userID)
if err != nil {
	return err
}
balance := account.Balance
```

或者提供：

```go
func (s *Service) GetBalanceTx(tx *gorm.DB, userID uint64) (int, error)
```

### 4.5 建议封装 result helper

```go
func (s *Service) createResultFromExistingTx(tx *gorm.DB, userID uint64, gen *model.Generation, duplicated bool) (*CreateResult, error) {
	account, err := s.creditService.GetAccountForUpdate(tx, userID)
	if err != nil {
		return nil, err
	}
	return &CreateResult{
		GenerationID:     gen.GenerationID,
		CreditsDeducted:  gen.CreditsDeducted,
		RemainingBalance: account.Balance,
		ExpiresAt:        gen.ExpiredAt.Unix(),
		Duplicated:       duplicated,
	}, nil
}
```

### 4.6 并发测试

新增：

```go
func TestCreateGeneration_IdempotentConcurrent(t *testing.T) {
	genService, creditService := setupGenerationService(t)
	ctx := context.Background()
	userID := uint64(100)

	if err := creditService.AddCredits(ctx, userID, 10, "test", "", "", ""); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		_, err := genService.CreateGeneration(ctx, userID, "29x29", "photo", "", fmt.Sprintf("free-%d", i))
		if err != nil {
			t.Fatal(err)
		}
	}

	const n = 20
	var wg sync.WaitGroup
	results := make(chan *generation.CreateResult, n)
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := genService.CreateGeneration(ctx, userID, "29x29", "photo", "", "same-key")
			if err != nil {
				errs <- err
				return
			}
			results <- r
		}()
	}

	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent CreateGeneration failed: %v", err)
	}

	var first string
	count := 0
	for r := range results {
		if first == "" {
			first = r.GenerationID
		}
		if r.GenerationID != first {
			t.Fatalf("expected same generation id, got %s vs %s", first, r.GenerationID)
		}
		count++
	}
	if count != n {
		t.Fatalf("expected %d results, got %d", n, count)
	}

	balance, _ := creditService.GetBalance(ctx, userID)
	if balance != 9 {
		t.Fatalf("expected only one credit deducted, balance=9, got %d", balance)
	}
}
```

### 4.7 SQLite 测试注意

SQLite 内存库并发测试可能出现每个连接独立数据库的问题。测试 DB 建议改为：

```go
sqlite.Open("file::memory:?cache=shared")
```

并设置：

```go
sqlDB, _ := db.DB.DB()
sqlDB.SetMaxOpenConns(1)
```

注意：

- `SetMaxOpenConns(1)` 可避免 SQLite 内存库 `no such table`。
- 但它会降低真实并发度。
- 真正验证 MySQL 行锁和唯一索引，后续建议补 MySQL 集成测试。

## 5. 问题四：PatternData 完整性

### 5.1 当前问题

涉及文件：

- `internal/service/work/pattern.go`
- `internal/service/work/service.go`
- `internal/api/generation.go`
- `internal/test/work_test.go`

当前问题：

- `pixels` 和 `pixel_rows` 都为空也能通过。
- `pixel_rows` 没有保存到 JSON。
- 没校验 `pixel_rows` 每行长度。
- 没校验像素 index 是否存在于 `color_palette`。
- Flutter 或 gRPC 客户端传入的数据可能保存后无法恢复。

### 5.2 目标规则

`PatternData` 必须满足：

- `width > 0`
- `height > 0`
- `width <= max_width`
- `height <= max_height`
- `width * height <= max_pixels`
- `pixels` 和 `pixel_rows` 必须二选一。
- 如果传 `pixels`，长度必须等于 `width * height`。
- 如果传 `pixel_rows`，行数必须等于 `height`，每行长度必须等于 `width`。
- `color_palette` 必填。
- `color_palette` 数量不超过 `max_colors`。
- 像素 index 必须存在于 `color_palette` 中。
- `0` 可作为空白像素，允许不出现在 palette。

### 5.3 pixel_rows 转 pixels

如果 `pixel_rows` 的每个 byte 表示颜色 index，可以统一转成 `pixels` 保存。

```go
func pixelRowsToPixels(rows [][]byte, width, height int32) ([]int32, error) {
	if len(rows) != int(height) {
		return nil, apperr.InvalidArgument("pixel_rows length must equal height")
	}

	pixels := make([]int32, 0, int(width*height))
	for _, row := range rows {
		if len(row) != int(width) {
			return nil, apperr.InvalidArgument("each pixel row length must equal width")
		}
		for _, b := range row {
			pixels = append(pixels, int32(b))
		}
	}
	return pixels, nil
}
```

### 5.4 ValidatePatternData 修改

```go
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

	checkPixel := func(v int32) error {
		if !paletteIndexes[v] {
			return apperr.InvalidArgument("pixel index not found in color_palette")
		}
		return nil
	}

	for _, px := range p.Pixels {
		if err := checkPixel(px); err != nil {
			return err
		}
	}

	for _, row := range p.PixelRows {
		for _, b := range row {
			if err := checkPixel(int32(b)); err != nil {
				return err
			}
		}
	}

	return nil
}
```

### 5.5 PatternDataToJSONMap 修改

推荐统一保存为 `pixels`，方便 Flutter 读取。

```go
func PatternDataToJSONMap(p *pb.PatternData) model.JSONMap {
	if p == nil {
		return nil
	}

	pixels := p.Pixels
	if len(pixels) == 0 && len(p.PixelRows) > 0 {
		converted, _ := pixelRowsToPixels(p.PixelRows, p.Width, p.Height)
		pixels = converted
	}

	pixelValues := make([]interface{}, 0, len(pixels))
	for _, px := range pixels {
		pixelValues = append(pixelValues, px)
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

	return model.JSONMap{
		"schema_version": p.SchemaVersion,
		"width":          p.Width,
		"height":         p.Height,
		"board_spec":     p.BoardSpec,
		"pixels":         pixelValues,
		"color_palette":  colors,
	}
}
```

### 5.6 JSONMapToPatternData

保持读取 `pixels` 即可：

```go
if pixels, ok := m["pixels"].([]interface{}); ok {
	pd.Pixels = make([]int32, 0, len(pixels))
	for _, p := range pixels {
		if v, ok := p.(float64); ok {
			pd.Pixels = append(pd.Pixels, int32(v))
		}
	}
}
```

如果需要兼容历史 `pixel_rows`，再补 fallback。

### 5.7 测试用例

新增：

```go
func TestValidatePatternData_RequiresPixelsOrRows(t *testing.T)
func TestValidatePatternData_RejectsInvalidPixelIndex(t *testing.T)
func TestPatternDataToJSONMap_ConvertsPixelRowsToPixels(t *testing.T)
func TestSaveWork_GetWork_RoundTripPatternData(t *testing.T)
```

关键断言：

- 空 `pixels` + 空 `pixel_rows` 返回参数错误。
- `pixels` 长度错误返回参数错误。
- 像素 index 不在 palette 中返回参数错误。
- 使用 `pixel_rows` 保存后，读回 `pixels` 长度等于 `width * height`。
- 保存后 `GetWork` 返回的 `PatternData` 能完整恢复 Flutter 图纸。

## 6. 测试迁移修复

### 6.1 当前问题

涉及文件：

- `internal/test/setup.go`

当前测试 DB 没迁移新表：

- `bb_credit_account`
- `bb_media_asset`

因此 `go test ./...` 报错：

```text
no such table: bb_credit_account
```

### 6.2 修复方案

修改测试迁移：

```go
err = db.DB.AutoMigrate(
	&model.User{},
	&model.Work{},
	&model.CommunityPost{},
	&model.Like{},
	&model.Favorite{},
	&model.Comment{},
	&model.Follow{},
	&model.Template{},
	&model.TemplateCategory{},
	&model.Order{},
	&model.Product{},
	&model.Subscription{},
	&model.CreditTransaction{},
	&model.CreditAccount{},
	&model.Invite{},
	&model.BeadColor{},
	&model.BoardSpec{},
	&model.Config{},
	&model.Feedback{},
	&model.Generation{},
	&model.MediaAsset{},
)
```

### 6.3 SQLite DSN 建议

如果要补并发测试，建议把 SQLite DSN 改为共享内存：

```go
db.DB, err = gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
	Logger: logger.Default.LogMode(logger.Silent),
})
```

并设置连接数：

```go
sqlDB, err := db.DB.DB()
if err == nil {
	sqlDB.SetMaxOpenConns(1)
}
```

注意：

- 这主要是为了避免 SQLite 测试环境不稳定。
- MySQL 的真实锁行为仍需要集成测试验证。

## 7. 现有失败用例修复

### 7.1 当前 `go test ./...` 失败原因

当前失败集中在：

- `TestAddAndDeductCredits`
- `TestInsufficientCredits`
- `TestListTransactions`
- `TestCreateGeneration_FreeQuota`
- `TestCreateGeneration_Idempotent`
- `TestCancelGeneration_WithRefund`

根因：

- 测试 DB 没有 `bb_credit_account`。
- `AddCredits` 没更新 account。
- generation 测试中部分 `CreateGeneration` 忽略错误，然后继续访问 `result.GenerationID`，导致 panic。

### 7.2 Generation 测试错误处理

把所有忽略错误的位置改为显式检查。

示例：

```go
result, err := genService.CreateGeneration(ctx, userID, "29x29", "photo", "", "cancel-paid")
if err != nil {
	t.Fatalf("CreateGeneration paid failed: %v", err)
}
```

至少需要检查这些位置：

- `internal/test/generation_test.go` 中 first complete 前创建 generation 的地方。
- cancel refund 前创建 paid generation 的地方。
- cancel after complete 前创建 generation 的地方。
- expire timeout 测试中创建 generation 的地方。

原则：

- 测试中除非明确测试错误，否则不允许忽略 service 返回的 error。
- `result` 可能为 nil，访问前必须确认 `err == nil`。

## 8. 建议补充的完整验收测试清单

### 8.1 Credit

- `TestAddAndDeductCredits`
- `TestInsufficientCredits`
- `TestListTransactions`
- `TestDeductCreditsTx_RejectsInsufficient`
- `TestAddCredits_CreatesAccountIfMissing`

### 8.2 Generation

- `TestCreateGeneration_FreeQuota`
- `TestCreateGeneration_Idempotent`
- `TestCreateGeneration_IdempotentConcurrent`
- `TestCompleteGeneration_Idempotent`
- `TestCancelGeneration_WithRefund`
- `TestCancelGeneration_AfterComplete`
- `TestExpireTimeoutGenerations_NoDoubleRefund`

### 8.3 Work

- `TestSaveWork_PatternDataRoundTrip`
- `TestGetWork_RejectsOtherUser`
- `TestSaveDraft_RejectsOtherUserDraft`
- `TestValidatePatternData_RequiresPixels`
- `TestValidatePatternData_RejectsUnknownColorIndex`
- `TestPatternData_PixelRowsConverted`

### 8.4 Media

- `TestGetUploadToken_RejectsInvalidPurpose`
- `TestGetUploadToken_RejectsInvalidContentType`
- `TestReportUpload_RejectsOtherUserFile`
- `TestReportUpload_RejectsTooLarge`

## 9. 推荐修复顺序

建议按以下顺序执行，阻塞最少：

1. 修 `SetupTestDB`，迁移 `CreditAccount` 和 `MediaAsset`。
2. 修 `CreditService.AddCredits/DeductCredits`，确保 account 和 transaction 同步更新。
3. 跑：

```bash
go test ./internal/test -run TestAddAndDeductCredits
```

4. 修 `Generation.ClientRequestID` 联合唯一索引。
5. 修 `CreateGeneration` duplicate key 处理和 tx 内余额读取。
6. 修 generation 测试里忽略错误导致 panic 的地方。
7. 修 `Work.GetWork(userID, workID)` 权限。
8. 修 `PatternData` 校验和转换。
9. 补并发幂等、权限、PatternData roundtrip 测试。
10. 跑：

```bash
go test ./...
```

## 10. 最终验收标准

修完后必须满足：

```bash
go test ./...
```

人工检查：

- 相同 `client_request_id` 重试只产生一条 paid generation。
- 相同 `client_request_id` 重试只产生一条扣费流水。
- `GetWork` 不能读取其他用户作品。
- `PatternData` 详情返回能让 Flutter 完整恢复图纸。
- 积分余额以 `bb_credit_account.balance` 为准。
- `bb_credit_transaction` 只作为审计流水，不作为余额来源。

达到以上标准后，当前 P0 review 问题基本闭环，可以进入下一轮 Flutter 联调或继续补 P1 的 trace_id 全接口覆盖、OSS 签名 URL escape、手机号/Apple/微信登录等问题。
