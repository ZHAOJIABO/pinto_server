# 拼豆 App 后端接口设计方案

本文档基于当前代码整理，目标覆盖以下功能：

- 首页官方图纸区域：展示后端配置的拼豆图纸，用户可收藏。
- 风格转换页：展示后端配置的 AI 风格，用户上传图片后生成不同风格图片。
- 用户图纸管理：用户生成图纸后上传服务端，支持“我的生成记录”。
- 用户收藏管理：支持“我的收藏”。

结论先说：当前代码已经有可复用的主干，不建议另起一套平行接口。

## 1. 当前代码可复用点

| 能力 | 当前代码 | 现状 | 本次设计怎么用 |
|---|---|---|---|
| 官方图纸库 | `TemplateService`, `bb_template`, `bb_template_category` | 已有分类、列表、详情雏形 | 作为“图纸广场/官方图纸”的主模块继续扩展 |
| 用户作品 | `WorkService`, `bb_work` | 已支持保存、详情、列表、草稿 | 作为“我的生成记录”的完成态记录 |
| 图片上传 | `MediaService`, `bb_media_asset` | 已支持 OSS 直传签名、上传回调 | 复用上传原图、AI 输入图、图纸预览图 |
| 本地图纸生成扣费 | `GenerationService`, `bb_generation` | 已支持预扣费、完成、取消、幂等 | 继续用于“客户端生成拼豆图纸” |
| 社区收藏 | `CommunityService`, `bb_favorite` | 只收藏社区帖子 `post_id` | 不直接拿来收藏官方模板，避免语义混乱 |
| 系统配置 | `SystemService`, `bb_config` | 已支持 app 配置、banner、颜色库、板规格 | 可承载简单开关，但 AI 风格建议单独建表 |

## 2. 范围决定

你选择了完整设计，分期实现。

### P1 必须落地

- 官方图纸列表、详情、收藏、取消收藏。
- 我的官方图纸收藏列表。
- 上传图片 purpose 补充 `style_input`。
- AI 风格列表。
- AI 风格生成任务创建、查询。
- 生成完成后保存为用户作品，复用 `WorkService` 或 `GenerationService.CompleteGeneration`。
- 我的生成记录：至少返回已完成作品列表。

### P2 可延后

- AI 任务取消和退款。
- AI 任务回调 webhook。
- “我的生成记录”聚合 pending、failed、style task、work 的统一时间线。
- 社区收藏和官方图纸收藏的统一收藏流。
- 内容审核、推荐排序、运营后台。

## 3. 总体架构

```
Flutter App
  |
  | REST JSON through gRPC-Gateway
  v
API Handler
  |
  +-- TemplateService -------------- bb_template
  |       |                           bb_template_category
  |       +-- TemplateFavorite ------- bb_template_favorite
  |
  +-- MediaService ----------------- bb_media_asset -> OSS
  |
  +-- AIGenerationService ---------- bb_ai_style
  |       |                           bb_ai_generation
  |       +-- AI Provider API
  |
  +-- GenerationService ------------ bb_generation
  |       +-- CreditService --------- bb_credit_account / transaction
  |
  +-- WorkService ------------------ bb_work
```

推荐新增 `AIGenerationService`，不要把服务端 AI 风格转换塞进现有 `GenerationService`。

原因：

- 当前 `GenerationService` 的语义是“客户端本地生成拼豆图纸前的预扣费凭证”。
- AI 风格转换是服务端异步任务，有 provider job、输入图、输出图、失败原因、重试状态。
- 两者都可以扣积分，但状态机不同。硬塞一张表会让查询、退款、重试变复杂。

## 4. 数据流

### 4.1 首页官方图纸

```
App 首页
  |
  | GET /api/v1/templates/categories
  | GET /api/v1/templates?scene=home&page=1&page_size=20
  v
TemplateService
  |
  | 查询 bb_template / bb_template_category
  | 批量查询当前用户是否收藏
  v
返回 TemplateItem(is_favorited, favorite_count)
```

### 4.2 收藏官方图纸

```
App 点击收藏
  |
  | POST /api/v1/templates/{template_id}/favorite
  v
TemplateService
  |
  | 校验 template 存在且 status=1
  | INSERT bb_template_favorite(user_id, template_id)
  | 幂等处理 duplicate key
  | bb_template.favorite_count + 1
  v
返回 success
```

取消收藏反向执行：

```
DELETE /api/v1/templates/{template_id}/favorite
  |
  | DELETE bb_template_favorite
  | 如果实际删除了 1 行，favorite_count - 1
  v
success
```

### 4.3 AI 风格转换

```
App 风格页
  |
  | GET /api/v1/ai/styles
  v
展示风格

用户选择图片
  |
  | POST /api/v1/media/upload-token purpose=style_input
  | PUT OSS
  | POST /api/v1/media/report-upload
  v
拿到 input_file_key / input_url

用户选择风格生成
  |
  | POST /api/v1/ai/style-generations
  v
AIGenerationService
  |
  | 创建 bb_ai_generation(status=pending)
  | 扣积分或校验权益
  | 调用 AI provider
  | status=running
  v
App 轮询 GET /api/v1/ai/style-generations/{task_id}
  |
  | status=succeeded
  | output_image_url
  v
客户端用 output_image_url 继续生成拼豆图纸
```

### 4.4 AI 图片保存为图纸

推荐仍复用现有 `GenerationService` 的完成链路：

```
AI output image
  |
  | App 本地转换为 PatternData
  | POST /api/v1/generation/create
  | source_type = "ai_style"
  | source_id = ai_task_id
  v
Generation pending
  |
  | POST /api/v1/media/upload-token purpose=pattern
  | 上传图纸预览图
  | POST /api/v1/generation/{id}/complete
  v
bb_work completed
```

这样“我的生成记录”仍以 `bb_work` 为完成态主表，不需要复制作品保存逻辑。

## 5. 数据模型设计

### 5.1 扩展 `bb_template`

当前模型：

- `Title`
- `PreviewURL`
- `PatternData`
- `BoardSpec`
- `IsFree`
- `CreditCost`
- `DownloadCount`
- `SortOrder`
- `Status`

建议补充：

```go
type Template struct {
    // existing fields...
    Description   string `gorm:"type:varchar(512)"`
    ThumbnailURL  string `gorm:"type:varchar(512)"`
    Tags          string `gorm:"type:varchar(512)"` // comma separated for P1
    Difficulty    int8   `gorm:"type:tinyint;default:1"`
    FavoriteCount int    `gorm:"type:int;default:0"`
}
```

P1 可以先用逗号字符串存 tags，避免为了展示标签引入新表。后续如果要运营筛选、搜索、标签聚合，再拆 `bb_template_tag`。

### 5.2 新增 `bb_template_favorite`

不要复用当前 `bb_favorite`。

当前 `bb_favorite` 是社区帖子收藏：

```go
type Favorite struct {
    UserID uint64
    PostID uint64
}
```

官方图纸收藏建议单独建表：

```sql
CREATE TABLE bb_template_favorite (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    user_id BIGINT NOT NULL,
    template_id BIGINT NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    UNIQUE KEY uk_user_template (user_id, template_id),
    KEY idx_user_created (user_id, created_at),
    KEY idx_template_id (template_id)
);
```

取舍：

- 优点：最小改动，不影响社区收藏现有代码和计数逻辑。
- 缺点：如果未来要统一收藏所有目标，需要做聚合接口。
- 推荐：P1 先这样。统一收藏流是 P2。

### 5.3 新增 `bb_ai_style`

后端可配置风格，客户端只拿展示字段，prompt 等敏感配置只留服务端。

```sql
CREATE TABLE bb_ai_style (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    style_key VARCHAR(64) UNIQUE NOT NULL,
    name VARCHAR(64) NOT NULL,
    description VARCHAR(512),
    cover_url VARCHAR(512),
    example_url VARCHAR(512),
    cost_credits INT DEFAULT 1,
    sort_order INT DEFAULT 0,
    status TINYINT DEFAULT 1,
    provider VARCHAR(32),
    model_name VARCHAR(64),
    prompt_template TEXT,
    negative_prompt TEXT,
    config JSON,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    KEY idx_status_sort (status, sort_order)
);
```

### 5.4 新增 `bb_ai_generation`

```sql
CREATE TABLE bb_ai_generation (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    task_id VARCHAR(36) UNIQUE NOT NULL,
    user_id BIGINT NOT NULL,
    client_request_id VARCHAR(64) NOT NULL,
    style_id BIGINT NOT NULL,
    input_file_key VARCHAR(512) NOT NULL,
    input_image_url VARCHAR(1024),
    output_file_key VARCHAR(512),
    output_image_url VARCHAR(1024),
    provider VARCHAR(32),
    provider_job_id VARCHAR(128),
    credits_deducted INT DEFAULT 0,
    status TINYINT DEFAULT 0,
    error_code VARCHAR(64),
    error_message VARCHAR(512),
    expired_at DATETIME,
    started_at DATETIME,
    completed_at DATETIME,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    UNIQUE KEY uk_user_request (user_id, client_request_id),
    KEY idx_user_created (user_id, created_at),
    KEY idx_status_created (status, created_at),
    KEY idx_provider_job (provider, provider_job_id)
);
```

状态建议：

| 值 | 名称 | 含义 |
|---|---|---|
| 0 | pending | 已创建，等待调用 provider |
| 1 | running | provider 已接收 |
| 2 | succeeded | 生成成功 |
| 3 | failed | 生成失败 |
| 4 | cancelled | 用户取消 |
| 5 | expired | 超时失败 |

## 6. Proto 与接口设计

### 6.1 TemplateService 扩展

保留现有接口：

```text
GET /api/v1/templates/categories
GET /api/v1/templates
GET /api/v1/templates/{template_id}
```

新增接口：

```text
POST   /api/v1/templates/{template_id}/favorite
DELETE /api/v1/templates/{template_id}/favorite
GET    /api/v1/templates/favorites
```

建议 proto：

```proto
message TemplateItem {
  string template_id = 1;
  string title = 2;
  string preview_url = 3;
  string board_spec = 4;
  int32 color_count = 5;
  bool is_free = 6;
  int32 credit_cost = 7;
  int32 download_count = 8;
  string thumbnail_url = 9;
  string description = 10;
  repeated string tags = 11;
  int32 difficulty = 12;
  int32 favorite_count = 13;
  bool is_favorited = 14;
}

message ListTemplatesRequest {
  RequestHeader header = 1;
  int32 category_id = 2;
  PageRequest page = 3;
  string scene = 4;      // home, category, search
  string keyword = 5;
}

message FavoriteTemplateRequest {
  RequestHeader header = 1;
  string template_id = 2;
}

message FavoriteTemplateResponse {
  ResponseHeader header = 1;
  bool is_favorited = 2;
  int32 favorite_count = 3;
}

message ListFavoriteTemplatesRequest {
  RequestHeader header = 1;
  PageRequest page = 2;
}

message ListFavoriteTemplatesResponse {
  ResponseHeader header = 1;
  repeated TemplateItem templates = 2;
  PageResponse page = 3;
}
```

注意：`GetTemplate` 当前 handler 没有返回 `PatternData`，但 proto 已经有字段。实现时必须补上，否则客户端点进官方图纸详情拿不到图纸数据。

### 6.2 MediaService 扩展

当前 `purposeConfig`：

```go
"original"
"pattern"
"avatar"
"feedback"
```

新增：

```go
"style_input": {MaxSize: 20 * 1024 * 1024, AllowedTypes: []string{"image/jpeg", "image/png", "image/webp", "image/heic"}}
"ai_output":   {MaxSize: 20 * 1024 * 1024, AllowedTypes: []string{"image/jpeg", "image/png", "image/webp"}}
```

客户端上传输入图仍走：

```text
POST /api/v1/media/upload-token
PUT  OSS upload_url
POST /api/v1/media/report-upload
```

`ai_output` 主要给服务端保存 provider 生成结果时复用文件分类。如果 provider 直接返回公网 URL，P1 可以只存 `output_image_url`，P2 再做服务端转存 OSS。

### 6.3 AIGenerationService 新增

路由：

```text
GET  /api/v1/ai/styles
POST /api/v1/ai/style-generations
GET  /api/v1/ai/style-generations/{task_id}
GET  /api/v1/ai/style-generations
POST /api/v1/ai/style-generations/{task_id}/cancel
```

P1 最少实现前三个。列表和取消可在 P1.5 或 P2。

建议 proto：

```proto
service AIGenerationService {
  rpc ListAIStyles(ListAIStylesRequest) returns (ListAIStylesResponse) {
    option (google.api.http) = { get: "/api/v1/ai/styles" };
  }

  rpc CreateStyleGeneration(CreateStyleGenerationRequest) returns (CreateStyleGenerationResponse) {
    option (google.api.http) = {
      post: "/api/v1/ai/style-generations"
      body: "*"
    };
  }

  rpc GetStyleGeneration(GetStyleGenerationRequest) returns (GetStyleGenerationResponse) {
    option (google.api.http) = {
      get: "/api/v1/ai/style-generations/{task_id}"
    };
  }

  rpc ListStyleGenerations(ListStyleGenerationsRequest) returns (ListStyleGenerationsResponse) {
    option (google.api.http) = {
      get: "/api/v1/ai/style-generations"
    };
  }
}

message AIStyleItem {
  string style_id = 1;
  string style_key = 2;
  string name = 3;
  string description = 4;
  string cover_url = 5;
  string example_url = 6;
  int32 cost_credits = 7;
  int32 sort_order = 8;
}

message ListAIStylesRequest {
  RequestHeader header = 1;
}

message ListAIStylesResponse {
  ResponseHeader header = 1;
  repeated AIStyleItem styles = 2;
}

message CreateStyleGenerationRequest {
  RequestHeader header = 1;
  string style_id = 2;
  string input_file_key = 3;
  string input_image_url = 4;
  string client_request_id = 5;
}

message CreateStyleGenerationResponse {
  ResponseHeader header = 1;
  string task_id = 2;
  int32 status = 3;
  int32 credits_deducted = 4;
  int32 remaining_balance = 5;
  int64 expires_at = 6;
  bool duplicated = 7;
}

message GetStyleGenerationRequest {
  RequestHeader header = 1;
  string task_id = 2;
}

message GetStyleGenerationResponse {
  ResponseHeader header = 1;
  string task_id = 2;
  int32 status = 3;
  AIStyleItem style = 4;
  string input_image_url = 5;
  string output_image_url = 6;
  string error_code = 7;
  string error_message = 8;
  int64 created_at = 9;
  int64 updated_at = 10;
}

message ListStyleGenerationsRequest {
  RequestHeader header = 1;
  PageRequest page = 2;
  int32 status = 3; // 0 means all if you keep status values 1-based in API, otherwise use optional wrapper later
}

message ListStyleGenerationsResponse {
  ResponseHeader header = 1;
  repeated GetStyleGenerationResponse tasks = 2;
  PageResponse page = 3;
}
```

### 6.4 GenerationService 小扩展

当前 `CreateGenerationRequest.source_type` 已支持自由字符串。

建议约定：

| source_type | source_id |
|---|---|
| `photo` | 空或 media file key |
| `album` | 空或客户端本地来源标识 |
| `template` | template_id |
| `ai_style` | ai task_id |

如果 `source_type=ai_style`，服务端应校验：

- `source_id` 对应的 `bb_ai_generation.task_id` 存在。
- task 属于当前用户。
- task 状态为 `succeeded`。

这可以避免用户拿别人的 AI 输出生成自己的图纸记录。

### 6.5 WorkService 作为我的生成记录

已有：

```text
GET /api/v1/works
GET /api/v1/works/{work_id}
POST /api/v1/works
DELETE /api/v1/works/{work_id}
```

建议保持“已完成图纸记录”由 `GET /api/v1/works` 承担。

可以新增筛选字段：

```proto
message ListWorksRequest {
  RequestHeader header = 1;
  PageRequest page = 2;
  string source_type = 3; // all, photo, template, ai_style
}
```

但当前 `bb_work` 没有 source 字段。P1 如果需要筛选，建议补：

```go
SourceType string `gorm:"type:varchar(32);index"`
SourceID   string `gorm:"type:varchar(64);index"`
```

`GenerationService.CompleteGeneration` 创建 work 时从 generation 写入。

## 7. 我的页面接口建议

P1 不新增 `MeService`。客户端用现有模块接口组合：

```text
GET /api/v1/works
GET /api/v1/templates/favorites
GET /api/v1/ai/style-generations
```

如果客户端强烈需要一个聚合接口，P2 再新增：

```text
GET /api/v1/me/library
```

返回：

```proto
message MyLibraryResponse {
  ResponseHeader header = 1;
  repeated WorkItem recent_works = 2;
  repeated TemplateItem favorite_templates = 3;
  repeated GetStyleGenerationResponse recent_ai_tasks = 4;
}
```

不建议 P1 做聚合接口，原因是它会把 Template、Work、AI task 三个模块绑在一起，早期迭代反而更难改。

## 8. 关键校验与错误处理

### 8.1 参数解析不能忽略错误

当前部分 handler 有这种模式：

```go
postID, _ := strconv.ParseUint(req.PostId, 10, 64)
```

新接口不要继续照抄。所有 ID 解析失败必须返回：

```go
apperr.InvalidArgument("invalid template_id")
```

建议顺手补旧接口，至少包括：

- `internal/api/community.go`
- `internal/api/template.go`

### 8.2 收藏接口必须幂等

重复收藏：

- 返回 success。
- `is_favorited=true`。
- `favorite_count` 不重复增加。

重复取消：

- 返回 success。
- `is_favorited=false`。
- `favorite_count` 不继续减少。

实现时不要简单 “insert 后无脑 +1”。需要根据实际插入/删除行数更新计数。

### 8.3 AI 创建必须幂等

`CreateStyleGenerationRequest.client_request_id` 必填。

相同用户 + 相同 `client_request_id`：

- 返回同一个 `task_id`。
- 不重复扣积分。
- 不重复调用 AI provider。

这和当前 `CreateGeneration` 的设计一致，客户端弱网重试会安全很多。

### 8.4 上传文件必须校验归属

创建 AI task 时，`input_file_key` 必须属于当前用户，且 `purpose=style_input`，且状态为 uploaded。

否则返回 forbidden 或 invalid_argument。

## 9. 性能设计

### 9.1 模板列表避免 N+1

`ListTemplates` 返回 `is_favorited` 时，不要对每个 template 单独查一次收藏。

推荐：

```sql
SELECT template_id
FROM bb_template_favorite
WHERE user_id = ? AND template_id IN (...)
```

在 service 层构建 `map[uint64]bool`。

### 9.2 索引

必需索引：

```sql
bb_template:           KEY idx_category_status_sort (category_id, status, sort_order)
bb_template_favorite:  UNIQUE uk_user_template (user_id, template_id)
bb_template_favorite:  KEY idx_user_created (user_id, created_at)
bb_ai_style:           KEY idx_status_sort (status, sort_order)
bb_ai_generation:      UNIQUE uk_user_request (user_id, client_request_id)
bb_ai_generation:      KEY idx_user_created (user_id, created_at)
bb_ai_generation:      KEY idx_status_created (status, created_at)
```

### 9.3 分页

当前项目使用 `PageRequest` + `PageResponse`。P1 继续保持。

当模板和收藏数据量上来后，再改 cursor 分页：

```text
GET /api/v1/templates?cursor=...
```

不要 P1 就同时维护 page 和 cursor 两套协议。

## 10. 测试计划

当前项目测试在 `internal/test`，使用 SQLite 内存库和 service 层测试。新功能继续沿用。

### 10.1 覆盖图

```
CODE PATHS                                      USER FLOWS
[+] TemplateService                              [+] 首页官方图纸
  |-- [GAP] List templates with favorite flags      |-- [GAP] 首次打开首页看到官方图纸
  |-- [GAP] Get template returns PatternData         |-- [GAP] 点进图纸详情能还原图纸
  |-- [GAP] Favorite template idempotent             |-- [GAP] 连点收藏不重复计数
  |-- [GAP] Unfavorite template idempotent           |-- [GAP] 我的收藏出现/移除该图纸

[+] MediaService                                  [+] 风格转换上传
  |-- [CODE] invalid purpose, no test yet            |-- [GAP] 上传 style_input 成功
  |-- [CODE] invalid content type, no test yet       |-- [GAP] 过大文件有明确错误
  |-- [GAP] style_input purpose

[+] AIGenerationService                           [+] AI 风格转换
  |-- [GAP] List active styles                       |-- [GAP] 用户看到后端配置风格
  |-- [GAP] Create task idempotent                   |-- [GAP] 弱网重试不重复扣费
  |-- [GAP] Reject other user's file_key             |-- [GAP] 轮询看到 running/succeeded/failed
  |-- [GAP] Provider failure maps to failed status

[+] GenerationService                              [+] AI 输出保存为图纸
  |-- [TESTED] CreateGeneration idempotent           |-- [GAP] source_type=ai_style 校验任务归属
  |-- [TESTED] CompleteGeneration idempotent         |-- [TESTED] Complete 后生成 work
  |-- [GAP] ai_style source succeeded-only

[+] WorkService                                    [+] 我的页面
  |-- [TESTED] Save/Get/List/Delete work             |-- [TESTED] 我的作品列表
  |-- [TESTED] Reject other user's work              |-- [GAP] 按 source_type 过滤 ai_style

COVERAGE TODAY: 现有核心 Work/Generation 部分已覆盖，Media 有校验代码但缺测试
NEW GAPS: 14 个关键分支，P1 必须补 service 层测试
```

### 10.2 必加测试

Template：

- `ListTemplates` 返回收藏状态。
- `GetTemplate` 返回 `PatternData`。
- 收藏同一个 template 两次，只增加一次计数。
- 取消收藏两次，计数不小于 0。
- 收藏不存在 template 返回 not_found。
- `ListFavoriteTemplates` 只返回当前用户收藏。

Media：

- `GetUploadToken(purpose=style_input)` 成功。
- style_input 不允许非图片类型。
- ReportUpload 非本人 file_key 返回 forbidden。

AI：

- `ListAIStyles` 只返回 status=1。
- `CreateStyleGeneration` 缺少 client_request_id 返回 invalid_argument。
- 相同 client_request_id 返回同一个 task_id。
- input_file_key 不属于当前用户时拒绝。
- provider 返回失败时任务变成 failed，并保留 error_message。

Generation：

- `source_type=ai_style` 且 task 不存在，拒绝。
- `source_type=ai_style` 且 task 属于其他用户，拒绝。
- `source_type=ai_style` 且 task 未 succeeded，拒绝。
- `source_type=ai_style` 成功 complete 后 work 写入 SourceType/SourceID。

## 11. 分期实施清单

### P1.1 官方图纸收藏

涉及文件：

- `pkg/proto/template.proto`
- `internal/model/work.go` 或新增 `internal/model/template.go`
- `internal/dao/template.go`
- `internal/service/template/service.go`
- `internal/api/template.go`
- `internal/bootstrap/service_provider.go`
- `cmd/main.go`
- `internal/test/template_test.go`

任务：

1. 扩展 `TemplateItem` 和 `ListTemplatesRequest`。
2. 新增 `TemplateFavorite` model。
3. AutoMigrate 注册新 model。
4. DAO 增加批量收藏状态查询、收藏、取消收藏、收藏列表。
5. Service 实现幂等收藏和计数。
6. Handler 返回稳定错误码。
7. 测试收藏幂等和我的收藏列表。

### P1.2 AI 风格配置与任务

涉及文件：

- `pkg/proto/ai_generation.proto`
- `internal/model/ai_generation.go`
- `internal/dao/ai_generation.go`
- `internal/service/ai_generation/service.go`
- `internal/api/ai_generation.go`
- `internal/bootstrap/service_provider.go`
- `cmd/main.go`
- `internal/test/ai_generation_test.go`

任务：

1. 新增 AI style 和 AI generation proto。
2. 新增 `AIStyle`、`AIGeneration` model。
3. 新增 DAO 和 service。
4. P1 provider 可先做接口抽象和 fake provider，避免阻塞客户端联调。
5. Create task 做幂等和文件归属校验。
6. Get task 返回 running/succeeded/failed。
7. 测试幂等、归属、失败状态。

### P1.3 Work/Generation 关联来源

涉及文件：

- `pkg/proto/work.proto`
- `pkg/proto/generation.proto`
- `internal/model/work.go`
- `internal/service/generation/service.go`
- `internal/dao/generation.go`
- `internal/test/generation_test.go`

任务：

1. `Work` 增加 `SourceType`、`SourceID`。
2. `WorkItem` 返回来源字段。
3. `CompleteGeneration` 把 generation 来源写入 work。
4. `CreateGeneration(source_type=ai_style)` 校验 AI task。
5. `ListWorks` 可按 source_type 筛选。

## 12. 不在本期范围

- 运营后台：先用数据库或脚本配置 template/style。
- 推荐算法：P1 使用 sort_order + created_at。
- 统一收藏流：P1 分开官方图纸收藏和社区帖子收藏。
- AI provider webhook：P1 可轮询或后台 goroutine，后续再补 webhook。
- 内容审核：P2 再接入。
- PDF 导出和物料清单：不影响这次接口闭环。

## 13. 风险与建议

1. 不要把官方图纸收藏塞进 `bb_favorite.post_id`。
   - 这会让 post_id 有时是帖子，有时是模板，后续一定混乱。

2. 不要把 AI 风格任务硬塞进 `bb_generation`。
   - 当前 generation 是本地图纸生成凭证，AI 是服务端异步任务，状态机不同。

3. 新接口不要继续忽略 `ParseUint` 错误。
   - 非法 ID 变成 0 会制造假数据、错误权限判断和难查的问题。

4. P1 先 fake provider。
   - 先让客户端完成上传、创建任务、轮询、保存 work 的闭环，再替换真实 AI provider。

5. 所有创建类接口都要有 `client_request_id`。
   - 移动端弱网很常见，没有幂等键就会重复扣费、重复任务、重复作品。

## 14. 推荐最终接口清单

官方图纸：

```text
GET    /api/v1/templates/categories
GET    /api/v1/templates?scene=home&category_id=&keyword=&page=&page_size=
GET    /api/v1/templates/{template_id}
POST   /api/v1/templates/{template_id}/favorite
DELETE /api/v1/templates/{template_id}/favorite
GET    /api/v1/templates/favorites?page=&page_size=
```

上传：

```text
POST   /api/v1/media/upload-token
POST   /api/v1/media/report-upload
GET    /api/v1/media/url
```

AI 风格：

```text
GET    /api/v1/ai/styles
POST   /api/v1/ai/style-generations
GET    /api/v1/ai/style-generations/{task_id}
GET    /api/v1/ai/style-generations?page=&page_size=&status=
POST   /api/v1/ai/style-generations/{task_id}/cancel
```

图纸生成和我的记录：

```text
POST   /api/v1/generation/create
POST   /api/v1/generation/{generation_id}/complete
POST   /api/v1/generation/{generation_id}/cancel
GET    /api/v1/generation/{generation_id}
GET    /api/v1/works?page=&page_size=&source_type=
GET    /api/v1/works/{work_id}
DELETE /api/v1/works/{work_id}
```

## 15. 首期客户端推荐调用顺序

首页：

```text
GuestLogin
  -> GetAppConfig
  -> ListCategories
  -> ListTemplates(scene=home)
```

收藏：

```text
FavoriteTemplate
  -> ListFavoriteTemplates
```

AI 风格生成：

```text
ListAIStyles
  -> GetUploadToken(style_input)
  -> OSS PUT
  -> ReportUpload
  -> CreateStyleGeneration(client_request_id)
  -> GetStyleGeneration polling
  -> CreateGeneration(source_type=ai_style, source_id=task_id)
  -> 本地生成 PatternData
  -> GetUploadToken(pattern)
  -> OSS PUT
  -> ReportUpload
  -> CompleteGeneration
  -> ListWorks
```

我的页面：

```text
ListWorks
ListFavoriteTemplates
ListStyleGenerations
```
