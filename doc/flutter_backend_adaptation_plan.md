# BoboBeads 后端 Flutter 客户端适配改造方案

本文档面向后端、Flutter 客户端和测试开发。目标是把当前后端骨架改造成可稳定支持 Flutter App 的生产可用服务，重点保障以下核心链路：

- 登录/游客账号
- 图片上传
- 生图额度预占
- 客户端本地生成图纸
- 保存作品/草稿
- 弱网恢复和重复请求保护
- 社区发布和后续扩展

当前服务已经具备比较清晰的模块划分：`Auth`、`Generation`、`Work`、`Media`、`Community`、`Template`、`Credit` 等。但部分实现仍处于占位或骨架阶段，尤其是生图扣费事务、图纸数据保存、OSS 上传、错误码和 Flutter REST 适配。

## 1. 总体结论

### 1.1 推荐客户端协议

建议 Flutter 首期优先使用 HTTP REST + JSON 调用 gRPC-Gateway 暴露的接口，而不是直接接入 gRPC。

原因：

- Flutter 使用 `dio` 处理 REST、token 刷新、上传进度、错误拦截更直接。
- 真机调试、抓包、日志排查成本更低。
- 当前服务已经通过 `google.api.http` 注解暴露 REST 路由。
- 后续仍可保留 protobuf 作为接口契约，服务内部继续使用 gRPC。

如果后续确实需要 gRPC，建议在 REST 稳定后再补 `grpc-dart` 生成链路，而不是首期同时维护两套客户端调用方式。

### 1.2 服务端优先级

按业务风险排序，建议按以下顺序开发：

| 优先级 | 模块 | 改造目标 |
|---|---|---|
| P0 | Generation + Credit | 统一 generation_id、事务化扣费、支持幂等、防止重复扣费 |
| P0 | Work | 完整保存和返回 PatternData，支持二次编辑和跨设备恢复 |
| P0 | Media | 实现真实 OSS 直传签名，限制类型/大小，记录上传状态 |
| P0 | Auth | 修复游客登录 panic，实现游客账号复用，补短信/Apple/微信真实校验 |
| P1 | API Error | 标准错误码、trace_id、HTTP 状态码映射 |
| P1 | Flutter Contract | 输出 REST 字段规范和 Dart API Model 约定 |
| P1 | Recovery | App 重启后恢复 pending generation、草稿和上传状态 |
| P2 | Community | 信息流 cursor 分页、发布审核、点赞收藏幂等 |
| P2 | Observability | 请求日志、业务日志、上传/扣费/保存链路追踪 |

## 2. 当前主要问题

### 2.1 Generation 扣费 ID 不一致

涉及文件：

- `internal/service/generation/service.go`
- `internal/service/credit/service.go`
- `internal/dao/generation.go`
- `internal/dao/credit.go`

当前 `CreateGeneration` 在扣积分前生成了一次 `generationID`，扣费完成后又重新生成了另一个 `generationID` 保存到 `bb_generation`。

结果：

- 积分流水的 `ref_id` 和 generation 表里的 `generation_id` 对不上。
- 客户端拿到的 generation_id 无法反查对应扣费流水。
- 退款、客服排查、风控审计都会困难。

需要改为：

- `generationID := uuid.New().String()` 只生成一次。
- 扣费流水 `ref_id` 和 `bb_generation.generation_id` 使用同一个 ID。
- 扣费、创建 generation 必须在同一个数据库事务内完成。

### 2.2 Generation/Credit 没有事务和并发保护

当前余额通过最后一条积分流水的 `balance` 得到：

```go
SELECT * FROM bb_credit_transaction WHERE user_id = ? ORDER BY created_at DESC LIMIT 1
```

风险：

- 两个请求同时扣费时都读到同一个余额，导致余额错乱。
- 扣费成功但 generation 创建失败，会出现扣了积分但没有生成记录。
- 取消/过期退款重复执行，可能重复加积分。

建议新增用户积分账户表：

```sql
CREATE TABLE bb_credit_account (
  id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  user_id BIGINT UNSIGNED NOT NULL UNIQUE,
  balance INT NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);
```

扣费时使用事务 + 行锁：

```sql
SELECT * FROM bb_credit_account WHERE user_id = ? FOR UPDATE;
```

事务内执行：

1. 锁定用户积分账户。
2. 判断余额是否足够。
3. 更新账户余额。
4. 写入积分流水。
5. 写入 generation 记录。

### 2.3 Work 没有保存 PatternData

涉及文件：

- `pkg/proto/work.proto`
- `internal/api/work.go`
- `internal/model/work.go`
- `internal/service/work/service.go`

当前 `SaveWork` 只保存了：

- title
- original_image_url
- pattern_image_url
- width
- height
- bead_count
- color_count

但没有把 `PatternData.pixel_rows` 和 `color_palette` 存入 `model.Work.PatternData`。

这会导致：

- 作品详情无法恢复图纸网格。
- 换设备后无法编辑。
- 草稿只能看到图片，不能继续调整。
- 社区详情无法展示材料清单、颜色用量、图纸数据。

需要改为：

- `SaveWork` 和 `SaveDraft` 必须序列化完整 `PatternData` 到 `bb_work.pattern_data`。
- `GetWork` 必须反序列化并返回 `pattern_data`。
- 列表接口可以不返回完整 `pattern_data`，只返回摘要，减少流量。

### 2.4 PatternData 当前结构对 REST/Flutter 不够友好

当前 proto：

```proto
message PatternData {
  int32 width = 1;
  int32 height = 2;
  string board_spec = 3;
  repeated bytes pixel_rows = 4;
  repeated ColorEntry color_palette = 5;
}
```

`repeated bytes pixel_rows` 对 gRPC 很省空间，但 REST JSON 中会变成 base64 字符串数组，Flutter 处理时不如普通 int 数组直观。

建议保留 `pixel_rows`，同时补充 REST 友好字段：

```proto
message PatternData {
  int32 width = 1;
  int32 height = 2;
  string board_spec = 3;
  repeated bytes pixel_rows = 4;
  repeated ColorEntry color_palette = 5;

  // REST/Flutter 友好格式，按行展开，每个值表示颜色 index。
  // 长度必须等于 width * height。
  repeated int32 pixels = 6;

  // 数据版本，方便后续升级图纸格式。
  int32 schema_version = 7;
}
```

服务端保存时：

- gRPC 客户端可以传 `pixel_rows`。
- Flutter REST 客户端优先传 `pixels`。
- 服务端统一校验并存 JSON。

推荐 JSON 存储格式：

```json
{
  "schema_version": 1,
  "width": 29,
  "height": 29,
  "board_spec": "29x29",
  "pixels": [1, 1, 2, 2, 0],
  "color_palette": [
    {
      "index": 1,
      "hex": "#FF5733",
      "brand": "hama",
      "code": "H-04",
      "name": "Red"
    }
  ],
  "metadata": {
    "algorithm": "client-v1",
    "dither": "floyd_steinberg",
    "created_by": "flutter"
  }
}
```

### 2.5 Media 上传只是占位

涉及文件：

- `pkg/proto/media.proto`
- `internal/service/media/service.go`
- `internal/api/media.go`
- `conf/conf.go`
- `conf/server.yaml`

当前 `GetUploadToken` 只是拼出一个 OSS URL，并没有生成真实可上传签名。

需要改为真实 OSS 直传：

- 方案 A：服务端生成 presigned PUT URL。
- 方案 B：服务端生成 POST policy + form data。

推荐首期使用 presigned PUT URL，因为 Flutter 用 `dio.put` 最简单。

返回结构建议：

```proto
message GetUploadTokenResponse {
  ResponseHeader header = 1;
  string upload_url = 2;
  string file_key = 3;
  map<string, string> headers = 4;
  map<string, string> form_data = 5;
  int64 expires_at = 6;
  string upload_method = 7; // PUT or POST
  string public_url = 8;
  int64 max_file_size = 9;
}
```

Flutter 上传示例：

```dart
final token = await api.getUploadToken(
  fileName: 'source.jpg',
  contentType: 'image/jpeg',
  purpose: 'original',
);

await dio.put(
  token.uploadUrl,
  data: File(path).openRead(),
  options: Options(headers: token.headers),
  onSendProgress: (sent, total) {
    // 更新上传进度
  },
);

await api.reportUpload(
  fileKey: token.fileKey,
  fileSize: await File(path).length(),
);
```

### 2.6 Auth 仍是开发占位

涉及文件：

- `internal/service/auth/service.go`
- `internal/api/auth.go`
- `internal/middleware/auth.go`

问题：

- `GuestLogin` 使用 `deviceID[:6]`，device_id 为空或长度不足时会 panic。
- 游客登录每次都创建新用户，没有复用 device_id。
- 手机号登录没有校验短信验证码。
- 微信/Apple 登录未实现。
- refresh token 没有校验 token type，当前 refresh 接口只解析 user_id 后直接发新 token。

需要改为：

- `GuestLogin(device_id)`：如果存在同设备游客用户，直接返回原用户。
- `device_id` 必填，长度不足时返回参数错误。
- 用户表增加 `device_id`、`login_type`、`apple_sub`、`wechat_unionid` 等字段或独立账号绑定表。
- 短信验证码写 Redis，校验后删除。
- Apple 登录校验 identity token 的 issuer、audience、exp、sub。
- 微信登录根据平台区分 App、小程序、Web，并统一用 unionid 关联账号。

## 3. 目标接口规范

### 3.1 通用请求 Header

Flutter 所有请求统一携带：

| Header | 必填 | 示例 | 说明 |
|---|---|---|---|
| Authorization | 登录后必填 | Bearer xxx | access token |
| X-Platform | 必填 | ios/android | 平台 |
| X-App-Version | 必填 | 1.0.0 | App 版本 |
| X-Device-Id | 必填 | uuid | 设备 ID |
| X-Language | 可选 | zh-CN | 语言 |
| X-Request-Id | 强烈建议 | uuid | 客户端请求 ID |

说明：

- `RequestHeader` 可以保留给 gRPC，但 REST 客户端不建议在 body 里传 header。
- 服务端 middleware 应从 HTTP/gRPC metadata 中读取这些 header。
- `X-Request-Id` 缺失时服务端生成。

### 3.2 通用响应格式

成功：

```json
{
  "header": {
    "code": 0,
    "message": "success",
    "traceId": "018f..."
  },
  "dataField": "value"
}
```

失败：

```json
{
  "header": {
    "code": 2001,
    "message": "insufficient credits",
    "traceId": "018f..."
  }
}
```

注意：

- 当前 proto 字段是 `trace_id`，gRPC-Gateway 默认 JSON 可能输出 `traceId`。
- Flutter model 要按实际 JSON 名称生成或手写映射。

### 3.3 标准错误码

建议新增 `internal/errors` 包：

```go
type AppError struct {
	Code    int32
	Message string
	Cause   error
}
```

错误码规划：

| Code | 名称 | HTTP | Flutter 行为 |
|---|---|---|---|
| 0 | success | 200 | 正常 |
| 1001 | unauthorized | 401 | 刷新 token 或跳登录 |
| 1002 | token_expired | 401 | 自动刷新 token |
| 1003 | forbidden | 403 | 弹无权限 |
| 1101 | invalid_argument | 400 | 展示参数错误或静默上报 |
| 1102 | not_found | 404 | 展示资源不存在 |
| 1103 | rate_limited | 429 | 提示稍后重试 |
| 2001 | insufficient_credits | 200/400 | 跳积分购买页 |
| 2002 | generation_expired | 200/409 | 提示重新生成 |
| 2003 | generation_completed | 200/409 | 拉取已有 work |
| 2004 | duplicate_request | 200 | 使用已有结果 |
| 3001 | upload_token_failed | 500 | 重试/上报 |
| 3002 | invalid_file_type | 400 | 提示格式不支持 |
| 3003 | file_too_large | 400 | 提示压缩或换图 |
| 5000 | internal_error | 500 | 统一服务异常 |

## 4. P0 改造：Generation + Credit

### 4.1 数据表调整

新增积分账户表：

```go
type CreditAccount struct {
	BaseModel
	UserID  uint64 `gorm:"not null;uniqueIndex" json:"user_id"`
	Balance int    `gorm:"not null;default:0" json:"balance"`
}

func (CreditAccount) TableName() string { return "bb_credit_account" }
```

`CreditTransaction` 建议增加：

```go
RequestID string `gorm:"type:varchar(64);index" json:"request_id"`
```

`Generation` 建议增加：

```go
ClientRequestID string `gorm:"type:varchar(64);index" json:"client_request_id"`
CancelReason    string `gorm:"type:varchar(255)" json:"cancel_reason"`
```

唯一索引建议：

```sql
UNIQUE KEY uk_generation_user_request (user_id, client_request_id)
```

### 4.2 CreateGeneration 请求增加幂等字段

Proto 建议：

```proto
message CreateGenerationRequest {
  RequestHeader header = 1;
  string board_spec = 2;
  string source_type = 3;
  string source_id = 4;
  string client_request_id = 5;
}
```

Flutter 调用时每次点击“开始生成”生成一个 UUID，并持久化到本地，直到 complete/cancel/expired。

请求示例：

```json
{
  "boardSpec": "29x29",
  "sourceType": "photo",
  "sourceId": "",
  "clientRequestId": "018f2a3b-..."
}
```

响应示例：

```json
{
  "header": { "code": 0, "message": "success", "traceId": "..." },
  "generationId": "9b1c...",
  "creditsDeducted": 1,
  "remainingBalance": 12,
  "expiresAt": 1760000000
}
```

### 4.3 CreateGeneration 服务端流程

伪代码：

```go
func CreateGeneration(ctx context.Context, userID uint64, req CreateGenerationInput) (*CreateResult, error) {
  if req.ClientRequestID == "" {
    return nil, errors.InvalidArgument("client_request_id required")
  }

  return db.Transaction(ctx, func(tx *gorm.DB) (*CreateResult, error) {
    existing := generationDAO.GetByUserRequestIDForUpdate(tx, userID, req.ClientRequestID)
    if existing != nil {
      balance := creditDAO.GetBalanceForUpdate(tx, userID)
      return existing.ToCreateResult(balance), nil
    }

    generationID := uuid.NewString()
    creditsDeducted := 0

    isVIP := subscribeDAO.IsVIP(tx, userID)
    if !isVIP {
      used := generationDAO.CountTodayChargedOrFree(tx, userID)
      if used >= dailyFreeLimit {
        account := creditDAO.GetAccountForUpdate(tx, userID)
        if account.Balance < creditCostPerGen {
          return nil, errors.InsufficientCredits(...)
        }
        account.Balance -= creditCostPerGen
        creditDAO.UpdateAccount(tx, account)
        creditDAO.CreateTransaction(tx, generationID, -creditCostPerGen, account.Balance)
        creditsDeducted = creditCostPerGen
      }
    }

    gen := Generation{
      GenerationID: generationID,
      ClientRequestID: req.ClientRequestID,
      UserID: userID,
      CreditsDeducted: creditsDeducted,
      Status: Pending,
      ExpiredAt: now + 30min,
    }
    generationDAO.Create(tx, gen)

    return result, nil
  })
}
```

### 4.4 CompleteGeneration 必须幂等

客户端可能出现：

- complete 请求超时，但服务端已经保存成功。
- 用户重新打开 App 后再次 complete。
- Flutter 上传图片成功后，保存作品接口被重复调用。

因此 `CompleteGeneration` 需要支持：

- 如果 generation 已完成，直接返回已有 `work_id`，不要报错。
- 如果 generation 已过期，但服务端已经有 work_id，返回已有 work_id。
- 保存 work 和更新 generation 状态在同一事务内完成。

返回建议增加：

```proto
message CompleteGenerationResponse {
  ResponseHeader header = 1;
  string work_id = 2;
  bool duplicated = 3;
}
```

### 4.5 CancelGeneration/过期任务防重复退款

取消和过期都可能退款。需要确保同一个 generation 最多退款一次。

实现方式：

- 事务内 `SELECT generation FOR UPDATE`。
- 只有 `status = pending` 才能退款。
- 先更新状态，再写退款流水，或在同一事务内完成。
- 退款流水使用唯一键 `(ref_type, ref_id, type)` 防止重复。

## 5. P0 改造：Work 和 Draft

### 5.1 PatternData 校验

保存作品/草稿时必须校验：

- `width > 0`
- `height > 0`
- `width * height <= max_pattern_pixels`
- `len(pixels) == width * height`，如果使用 REST pixels。
- `len(pixel_rows) == height`，如果使用 gRPC bytes。
- `color_palette` 不为空。
- 每个像素 index 必须能在 `color_palette` 中找到，0 可作为空白。
- `board_spec` 必须存在于系统豆板规格中。

建议配置：

```yaml
pattern:
  max_width: 200
  max_height: 200
  max_pixels: 40000
  max_colors: 128
```

需要同步修改 `conf.Config`。

### 5.2 保存完整 PatternData

新增转换函数：

```go
func patternDataToJSONMap(p *pb.PatternData) (model.JSONMap, error)
func jsonMapToPatternData(m model.JSONMap) (*pb.PatternData, error)
```

建议放在：

- `internal/api/pattern.go`

或：

- `internal/service/work/pattern.go`

如果转换只处理 pb 和 model，放 `internal/api` 更直接；如果校验属于业务逻辑，放 `service/work` 更合适。推荐放 `service/work`，避免 API 层过重。

### 5.3 SaveWork 流程

服务端流程：

1. 读取 user_id。
2. 校验 title、图片 URL、pattern_data。
3. 将 pattern_data 转成 JSONMap。
4. 写入 `bb_work`。
5. 返回 work_id。

### 5.4 GetWork 返回详情

当前 `GetWorkResponse` 已有：

```proto
message GetWorkResponse {
  ResponseHeader header = 1;
  WorkItem work = 2;
  PatternData pattern_data = 3;
}
```

需要实现返回 `pattern_data`。

### 5.5 ListWorks/ListDrafts 只返回摘要

列表接口不返回完整图纸，避免流量过大。

建议 `WorkItem` 增加：

```proto
string thumbnail_url = 12;
int64 updated_at = 13;
```

Flutter 列表页使用：

- `pattern_image_url` 或 `thumbnail_url`
- title
- board_spec
- bead_count
- color_count
- updated_at

点击详情再拉 `GetWork`。

### 5.6 SaveDraft 应支持 upsert

当前 `SaveDraftRequest` 有 `draft_id`，但解析失败时会默默当 0。

需要改为：

- `draft_id` 非空但不是数字，返回 `invalid_argument`。
- 保存已有草稿时必须校验草稿属于当前用户。
- upsert 时更新 `updated_at`。
- 支持 `client_request_id` 防止自动保存重复创建。

## 6. P0 改造：Media 上传

### 6.1 上传目的 purpose

建议限制 `purpose` 枚举：

| purpose | 说明 | 最大大小 | 类型 |
|---|---|---|---|
| original | 用户原图 | 20MB | jpg/png/webp/heic |
| pattern | 生成图纸预览 | 10MB | jpg/png/webp |
| avatar | 头像 | 5MB | jpg/png/webp |
| feedback | 反馈截图 | 10MB | jpg/png/webp |

不要让客户端自由传任意目录，避免污染 OSS 路径。

文件 key 格式：

```text
{purpose}/{yyyy}/{mm}/{dd}/{user_id}/{uuid}.{ext}
```

示例：

```text
original/2026/06/07/123/9b1c.jpg
```

### 6.2 Media 表

建议新增：

```go
type MediaAsset struct {
  BaseModel
  UserID      uint64 `gorm:"not null;index"`
  FileKey     string `gorm:"type:varchar(512);not null;uniqueIndex"`
  FileURL     string `gorm:"type:varchar(1024)"`
  Purpose     string `gorm:"type:varchar(32);not null;index"`
  ContentType string `gorm:"type:varchar(128)"`
  FileSize    int64
  Status      int8   `gorm:"type:tinyint;default:0"` // 0:pending 1:uploaded 2:failed
  UploadedAt  *time.Time
}
```

### 6.3 GetUploadToken 流程

1. 校验 `file_name`、`content_type`、`purpose`。
2. 根据 content_type 推导扩展名，不完全信任 file_name。
3. 生成 file_key。
4. 创建 `bb_media_asset`，状态 pending。
5. 生成 OSS presigned PUT URL。
6. 返回 upload_url、headers、expires_at、file_key。

### 6.4 ReportUpload 流程

1. 校验 file_key 属于当前 user。
2. 可选：调用 OSS HeadObject 确认对象存在。
3. 校验 file_size 和 content_type。
4. 更新状态 uploaded。
5. 返回 CDN URL 或签名 URL。

### 6.5 GetFileUrl

如果文件是公开 CDN，可返回：

```json
{
  "url": "https://cdn.example.com/original/...",
  "expiresAt": 0
}
```

如果私有读，返回短期签名 URL。

## 7. P0 改造：Auth

### 7.1 游客登录

请求：

```json
{
  "deviceId": "flutter-generated-device-id"
}
```

规则：

- `device_id` 必填。
- 如果存在 `device_id + login_type=guest` 用户，返回原用户。
- 如果不存在，创建用户。
- 昵称生成不要依赖固定长度切片。

建议：

```go
prefix := deviceID
if len(prefix) > 6 {
  prefix = prefix[:6]
}
nickname := fmt.Sprintf("用户%s", prefix)
```

更推荐：

```go
nickname := fmt.Sprintf("用户%d", time.Now().UnixNano()%1000000)
```

### 7.2 Token 策略

当前 access token 72 小时较长。移动端可接受，但建议：

- access token：2 到 24 小时。
- refresh token：30 到 90 天。
- refresh token 存 Redis 或 DB，支持撤销。
- refresh token 携带 `type=refresh`，刷新接口必须校验。

RefreshToken 需要补：

```go
tokenType, _ := claims["type"].(string)
if tokenType != "refresh" {
  return nil, errors.Unauthorized("not a refresh token")
}
```

### 7.3 手机号登录

发送验证码：

- 生成 6 位数字。
- Redis key：`sms:login:{phone}`。
- TTL：5 分钟。
- 同手机号 60 秒内只能发一次。
- 同 IP/设备限流。

校验验证码：

- 验证成功后删除 Redis key。
- 测试环境可以配置固定验证码，如 `123456`。
- 生产环境不能接受万能验证码。

### 7.4 Apple 登录

服务端校验：

- Apple 公钥 JWK。
- `iss == https://appleid.apple.com`
- `aud == bundle_id/client_id`
- `exp > now`
- 提取 `sub` 作为稳定账号标识。

### 7.5 微信登录

按平台分支：

- iOS/Android App：微信开放平台 OAuth code。
- 小程序：`wx.login` code2Session。
- Web：扫码登录 code。

统一身份：

- 优先用 unionid。
- 没有 unionid 时用 openid + platform。

## 8. P1 改造：Flutter REST 适配

### 8.1 Flutter API Client 建议

客户端建议封装：

- `ApiClient`
- `AuthInterceptor`
- `RequestIdInterceptor`
- `ErrorMapper`
- `TokenStorage`

`dio` 拦截器逻辑：

1. 每个请求注入 `X-Request-Id`。
2. 注入 `Authorization`、`X-Platform`、`X-App-Version`、`X-Device-Id`。
3. 收到 `1002 token_expired` 或 HTTP 401 时刷新 token。
4. 刷新成功后重放原请求。
5. 刷新失败跳登录。

### 8.2 字段命名

gRPC-Gateway 默认 JSON 名称通常是 lowerCamelCase：

- `generation_id` -> `generationId`
- `pattern_data` -> `patternData`
- `file_key` -> `fileKey`

Flutter model 统一使用 camelCase。

服务端文档和测试用例也应该使用 camelCase 示例，减少客户端误解。

### 8.3 推荐 Flutter 本地状态

本地需要保存：

```dart
class PendingGeneration {
  final String clientRequestId;
  final String generationId;
  final String sourceLocalPath;
  final String? originalFileKey;
  final String? patternFileKey;
  final int expiresAt;
  final String status; // creating/uploading/completing/done/cancelled
}
```

App 启动时：

1. 扫描本地 pending generation。
2. 对每条调用 `GetGenerationStatus`。
3. 如果服务端 completed，拉取 work。
4. 如果 pending 且未过期，继续上传/complete。
5. 如果 expired，清理本地状态并提示重新生成。

## 9. 核心业务链路

### 9.1 首次启动登录

```text
Flutter
  1. 生成或读取 device_id
  2. POST /api/v1/auth/guest
  3. 保存 access_token/refresh_token/user
  4. 拉取 /api/v1/system/config
  5. 拉取 /api/v1/system/bead-colors
  6. 拉取 /api/v1/system/board-specs
```

### 9.2 用户从照片生成作品

```text
Flutter
  1. 选择照片
  2. 本地压缩/裁剪
  3. CreateGeneration(client_request_id)
  4. GetUploadToken(purpose=original)
  5. 上传原图到 OSS
  6. ReportUpload(original)
  7. 本地生成 PatternData
  8. 渲染 pattern preview 图片
  9. GetUploadToken(purpose=pattern)
 10. 上传 pattern 图片
 11. ReportUpload(pattern)
 12. CompleteGeneration(generation_id, pattern_data, image urls)
 13. 保存 work_id，清理 pending generation
```

### 9.3 失败取消

```text
Flutter
  1. 如果 CreateGeneration 已成功，但本地生成失败
  2. POST /api/v1/generation/{id}/cancel
  3. 服务端只允许 pending 状态取消
  4. 如已扣积分，则退款
```

### 9.4 弱网重试

```text
Flutter
  1. CreateGeneration 超时
  2. 使用同一个 client_request_id 重试
  3. 服务端返回同一个 generation_id
  4. 不重复扣费
```

```text
Flutter
  1. CompleteGeneration 超时
  2. 使用同一个 generation_id 重试
  3. 服务端如果已完成，返回已有 work_id
  4. 不重复创建作品
```

## 10. 代码改造清单

### 10.1 Proto

需要修改：

- `pkg/proto/common.proto`
- `pkg/proto/generation.proto`
- `pkg/proto/work.proto`
- `pkg/proto/media.proto`

建议改动：

```proto
message ResponseHeader {
  int32 code = 1;
  string message = 2;
  string trace_id = 3;
}
```

保持已有字段，服务端开始填充 `trace_id`。

`generation.proto`：

```proto
message CreateGenerationRequest {
  RequestHeader header = 1;
  string board_spec = 2;
  string source_type = 3;
  string source_id = 4;
  string client_request_id = 5;
}

message CreateGenerationResponse {
  ResponseHeader header = 1;
  string generation_id = 2;
  int32 credits_deducted = 3;
  int32 remaining_balance = 4;
  int64 expires_at = 5;
  bool duplicated = 6;
}

message CompleteGenerationResponse {
  ResponseHeader header = 1;
  string work_id = 2;
  bool duplicated = 3;
}
```

`work.proto`：

```proto
message PatternData {
  int32 width = 1;
  int32 height = 2;
  string board_spec = 3;
  repeated bytes pixel_rows = 4;
  repeated ColorEntry color_palette = 5;
  repeated int32 pixels = 6;
  int32 schema_version = 7;
}
```

`media.proto`：

```proto
message GetUploadTokenResponse {
  ResponseHeader header = 1;
  string upload_url = 2;
  string file_key = 3;
  map<string, string> form_data = 4;
  int64 expires_at = 5;
  map<string, string> headers = 6;
  string upload_method = 7;
  string public_url = 8;
  int64 max_file_size = 9;
}
```

修改 proto 后执行：

```bash
make proto
```

如果 Makefile 没有该目标，则使用项目现有 buf 命令：

```bash
buf generate
```

### 10.2 Model

新增：

- `internal/model/credit_account.go`
- `internal/model/media.go`

修改：

- `internal/model/generation.go`
- `internal/model/work.go`
- `internal/model/user.go`

### 10.3 DAO

新增或修改：

- `CreditDAO.GetAccountForUpdate`
- `CreditDAO.CreateTransactionTx`
- `GenerationDAO.GetByUserRequestIDForUpdate`
- `GenerationDAO.GetByGenerationIDForUpdate`
- `GenerationDAO.CountTodayByUserTx`
- `WorkDAO.CreateTx`
- `WorkDAO.UpdateTx`
- `MediaDAO.Create`
- `MediaDAO.MarkUploaded`

DAO 方法建议接收 `*gorm.DB` 或支持从 context 取 tx。推荐简单直接：

```go
func (d *CreditDAO) GetAccountForUpdate(ctx context.Context, tx *gorm.DB, userID uint64) (*model.CreditAccount, error)
```

### 10.4 Service

重点修改：

- `internal/service/generation/service.go`
- `internal/service/credit/service.go`
- `internal/service/work/service.go`
- `internal/service/media/service.go`
- `internal/service/auth/service.go`

建议新增：

- `internal/service/work/pattern.go`
- `internal/errors/errors.go`
- `internal/middleware/trace.go`

### 10.5 API Handler

重点修改：

- `internal/api/common.go`
- `internal/api/generation.go`
- `internal/api/work.go`
- `internal/api/media.go`
- `internal/api/auth.go`

要求：

- 参数解析失败不能忽略，例如 `strconv.ParseUint` 失败必须返回 `invalid_argument`。
- 所有返回都带 trace_id。
- AppError 转换为稳定 code。

## 11. 测试计划

### 11.1 单元测试

Generation：

- 每日免费次数未用完，不扣积分。
- 免费次数用完且余额足够，扣 1 积分。
- 余额不足返回 `insufficient_credits`。
- 相同 `client_request_id` 重试返回同一 generation，不重复扣费。
- complete 成功创建 work。
- complete 重试返回已有 work_id。
- cancel pending generation 退款。
- cancel 已 completed generation 不退款。
- timeout processor 不重复退款。

Work：

- SaveWork 保存完整 PatternData。
- GetWork 返回完整 PatternData。
- pixels 长度不等于 width * height 返回参数错误。
- 超过 max_pixels 返回参数错误。
- 非本人 work 不可读取或删除。
- SaveDraft update 时校验 user_id。

Media：

- 不支持 content_type 返回错误。
- 超过大小限制返回错误。
- purpose 非法返回错误。
- ReportUpload 非本人 file_key 返回 forbidden。

Auth：

- 空 device_id 返回参数错误。
- 短 device_id 不 panic。
- 同 device_id 游客登录复用用户。
- refresh token 必须 type=refresh。

### 11.2 集成测试

核心链路：

```text
GuestLogin
  -> GetUploadToken(original)
  -> ReportUpload(original)
  -> CreateGeneration
  -> GetUploadToken(pattern)
  -> ReportUpload(pattern)
  -> CompleteGeneration
  -> GetWork
```

弱网重试链路：

```text
CreateGeneration(client_request_id=A)
CreateGeneration(client_request_id=A)
assert same generation_id
assert only one credit transaction
```

完成重试链路：

```text
CompleteGeneration(generation_id=A)
CompleteGeneration(generation_id=A)
assert same work_id
assert only one work
```

### 11.3 Flutter 联调验收

必须通过：

- iOS 模拟器游客登录。
- Android 模拟器游客登录。
- 真机选择照片、上传原图、生成、上传预览图、保存作品。
- 飞行模式/断网后恢复，不重复扣费。
- App kill 后重启，pending generation 能恢复。
- access token 过期后自动 refresh。
- 上传进度可展示。
- 积分不足时客户端能跳购买页。

## 12. 建议开发排期

### Phase 1：核心闭环稳定

范围：

- Generation 事务 + 幂等
- CreditAccount
- Work PatternData 保存/返回
- 基础错误码
- GuestLogin 修复

验收：

- 本地集成测试跑通完整生图保存链路。
- 重试不重复扣费，不重复创建作品。

### Phase 2：上传和客户端体验

范围：

- OSS presigned PUT
- MediaAsset 表
- ReportUpload
- Flutter 上传文档
- trace_id

验收：

- Flutter 真机可上传原图和 pattern 图片。
- 服务端可追踪 file_key 和用户关系。

### Phase 3：账号和商业化

范围：

- 短信验证码
- Apple 登录
- 微信登录
- 订阅/IAP 收据校验
- 积分购买

验收：

- 游客可升级账号。
- iOS/Android 登录和购买链路可上线灰度。

### Phase 4：社区和增长

范围：

- 社区 cursor feed
- 发布审核
- 点赞/收藏/关注幂等
- 邀请统计

验收：

- 社区浏览和发布对未登录/游客/登录用户行为清晰。

## 13. 开发注意事项

### 13.1 不要让客户端承担后端一致性

客户端可以做重试和本地恢复，但不能依赖客户端来避免重复扣费、重复保存。服务端必须通过事务、唯一索引、幂等 key 兜住。

### 13.2 不要把图片二进制传给业务接口

图片统一走 Media 上传，业务接口只传：

- file_key
- image_url
- pattern_data

这样能避免 gRPC/HTTP 请求体过大。

### 13.3 列表接口不要返回大字段

作品列表、草稿列表、社区 feed 不返回完整 `pattern_data`。详情页按需拉取。

### 13.4 字段向后兼容

proto 新增字段不要复用旧 tag，不要删除旧字段。Flutter 客户端升级可能不是同时完成，后端要允许旧字段缺失。

### 13.5 配置不要写死

以下都应进入配置：

- 每日免费次数
- 单次生成积分成本
- generation 过期时间
- pattern 最大宽高/像素数
- 上传大小限制
- OSS URL 过期时间

## 14. 最小可执行任务拆分

后端任务：

1. 新增 `CreditAccount` 模型和迁移。
2. 重写 `CreditService`，支持事务内加减积分。
3. 为 `CreateGeneration` 增加 `client_request_id`，实现幂等。
4. 将扣费和创建 generation 放入同一事务。
5. 将 complete 保存 work 和更新 generation 放入同一事务。
6. 实现 cancel/expire 防重复退款。
7. 实现 PatternData JSON 转换和校验。
8. SaveWork/SaveDraft 保存完整 PatternData。
9. GetWork 返回完整 PatternData。
10. 实现真实 OSS presigned upload。
11. 新增 MediaAsset 表和上传状态。
12. 修复 GuestLogin 空 device_id panic 和游客复用。
13. 新增 AppError 错误码和 trace_id。
14. 补齐 P0/P1 测试。

Flutter 任务：

1. 封装 Dio ApiClient。
2. 实现 token 存储和刷新拦截器。
3. 实现 device_id 生成和持久化。
4. 实现游客登录。
5. 实现 Media 上传方法，支持进度。
6. 实现 pending generation 本地表。
7. 生图前调用 CreateGeneration 并保存 `client_request_id`。
8. 本地生成 PatternData 后调用 CompleteGeneration。
9. App 启动时恢复 pending generation。
10. 根据错误码处理积分不足、过期、未登录、限流。

测试任务：

1. 编写 Generation 幂等和事务测试。
2. 编写 Work PatternData 保存/读取测试。
3. 编写 Media 参数校验测试。
4. 编写 Auth 游客登录测试。
5. 编写 Flutter 真机联调用例。

## 15. 推荐最终核心接口示例

### 15.1 CreateGeneration

请求：

```http
POST /api/v1/generation/create
Authorization: Bearer <token>
X-Platform: ios
X-App-Version: 1.0.0
X-Device-Id: 7D8C...
X-Request-Id: 018f...
Content-Type: application/json
```

```json
{
  "boardSpec": "29x29",
  "sourceType": "photo",
  "sourceId": "",
  "clientRequestId": "018f2a3b-2af0-7b33-a811-cb13d6b18790"
}
```

响应：

```json
{
  "header": {
    "code": 0,
    "message": "success",
    "traceId": "018f2a3b-..."
  },
  "generationId": "9b1c2d8c-4f9a-4a1b-bb70-2d3d83d41a11",
  "creditsDeducted": 1,
  "remainingBalance": 12,
  "expiresAt": 1791345678,
  "duplicated": false
}
```

### 15.2 CompleteGeneration

请求：

```json
{
  "generationId": "9b1c2d8c-4f9a-4a1b-bb70-2d3d83d41a11",
  "title": "小熊拼豆",
  "originalImageUrl": "https://cdn.example.com/original/...",
  "patternImageUrl": "https://cdn.example.com/pattern/...",
  "beadCount": 841,
  "colorCount": 12,
  "patternData": {
    "schemaVersion": 1,
    "width": 29,
    "height": 29,
    "boardSpec": "29x29",
    "pixels": [0, 1, 1, 2],
    "colorPalette": [
      {
        "index": 1,
        "hex": "#FF5733",
        "brand": "hama",
        "code": "H-04",
        "name": "Red"
      }
    ]
  }
}
```

响应：

```json
{
  "header": {
    "code": 0,
    "message": "success",
    "traceId": "018f..."
  },
  "workId": "123",
  "duplicated": false
}
```

### 15.3 GetWork

响应：

```json
{
  "header": {
    "code": 0,
    "message": "success",
    "traceId": "018f..."
  },
  "work": {
    "workId": "123",
    "title": "小熊拼豆",
    "originalImageUrl": "https://cdn.example.com/original/...",
    "patternImageUrl": "https://cdn.example.com/pattern/...",
    "boardSpec": "29x29",
    "width": 29,
    "height": 29,
    "beadCount": 841,
    "colorCount": 12,
    "status": 2,
    "createdAt": 1791345678
  },
  "patternData": {
    "schemaVersion": 1,
    "width": 29,
    "height": 29,
    "boardSpec": "29x29",
    "pixels": [0, 1, 1, 2],
    "colorPalette": []
  }
}
```

## 16. 验收标准

P0 完成后必须满足：

- Flutter 使用 REST 能完整跑通登录、上传、生图、保存、详情读取。
- 同一个 `client_request_id` 重试不会重复扣费。
- 同一个 `generation_id` complete 重试不会重复创建作品。
- 作品详情能恢复完整图纸数据。
- 图片上传是真实 OSS 直传，不经过业务服务转发二进制。
- 所有失败都返回稳定错误码，而不是统一 `-1`。
- 服务端日志和响应中都有 trace_id。

达到以上标准后，这个后端才算真正从“基础功能骨架”进入“可支撑 Flutter 客户端开发”的状态。
