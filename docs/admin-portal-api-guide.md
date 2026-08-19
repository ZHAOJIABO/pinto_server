# 管理后台接口对接文档（Admin Portal API）

面向内部运营后台（浏览器）的接口集合，全部挂在 `/api/v1/admin/` 前缀下，由独立的 HTTP Handler 提供（`internal/api/admin_portal_http.go`），**不走用户端 gRPC Gateway**，也**不接受用户端 JWT**。

> 另有 gRPC 服务 `AdminTemplateService`（`pkg/proto/admin_template.proto`）提供 `PublishTemplate` / `UnpublishTemplate` / `GetPublishStatus`，但未声明 HTTP 注解，仅用于服务间调用，后台页面不使用。

---

## 1. 通用约定

### 1.1 基础地址

```
{BASE_URL}/api/v1/admin/...
```

本地默认 `http://localhost:{server.http_port}`。

### 1.2 鉴权

- 除 `POST /api/v1/admin/login` 外，所有接口都必须携带：
  ```
  Authorization: Bearer <accessToken>
  ```
- 管理员账号配置在 `conf/server.yaml` 的 `admin.accounts`（用户名 + PBKDF2-SHA256 密码哈希），token 由 `admin.jwt_secret` 签发，有效期 `admin.access_expire_m`（默认 1440 分钟，未配置时回退 8 小时）。
- token 缺失/无效/过期统一返回 `401`，业务码 `1101`，`message = "administrator authentication required"`。

### 1.2.1 会话自动续期（前端必须实现）

有效期是**闲置超时**，不是会话总长。只要持续操作后台就不会被登出；连续 24 小时没有任何请求才需要重新登录。

实现方式：任何一次鉴权成功的响应，如果当前 token 已过半程（剩余不足 12 小时），服务端会在响应头附带一个新 token：

```
X-Admin-Access-Token: eyJhbGciOi...
X-Admin-Expires-In: 86400
```

前端需要在统一的响应拦截器里检查这两个头，出现就覆盖本地保存的 token：

```dart
final fresh = response.headers.value('x-admin-access-token');
if (fresh != null && fresh.isNotEmpty) {
  await storage.saveToken(fresh);
}
```

注意事项：

- **不是每个响应都带这两个头**，token 还在前半程时不会下发。没有头就保持现有 token 不变，不要清空。
- header 名大小写不敏感，但多数 HTTP 客户端要求用**小写**取值。
- 并发请求可能各自带回不同的新 token，都是有效的，直接覆盖即可，无需加锁或去重。
- **没有 refresh 接口**。收到 `401` 就是会话真的结束了，直接跳登录页，不要尝试续期或重试。
- 如果不实现这个拦截器，功能不会出错，但会退化成"固定 24 小时后被登出"。
- 管理员账号从 `admin.accounts` 中移除后，其在手 token 立即失效（下一次请求就是 `401`），不受有效期影响。

### 1.3 统一响应结构

成功：

```json
{
  "header": { "code": 0, "message": "success" },
  "...": "业务字段与 header 同级"
}
```

失败：

```json
{
  "header": { "code": 1101, "message": "invalid request" }
}
```

### 1.4 错误码

| HTTP | code | 含义 |
|---|---|---|
| 200 | 0 | 成功 |
| 400 | 1101 | 参数错误 / 校验失败 |
| 401 | 1101 | 未登录或 token 无效 |
| 401 | 1001 / 1002 | 未授权 / token 过期 |
| 403 | 1003 | 资源不属于 admin 上传通道等禁止操作 |
| 404 | 1102 | 资源不存在（模板、投稿、路由不存在） |
| 429 | 1101 | 登录失败次数过多被锁定 |
| 400 | 2004 | 并发重复审核（`submission was reviewed concurrently`） |
| 400 | 3002 / 3003 | 文件类型不允许 / 文件过大 |
| 500 | 5000 | 服务端内部错误 |

### 1.5 请求体规则

- JSON 请求体上限 **2 MB**（`/media/upload` 除外，见 3.2）。
- JSON 解析开启 **DisallowUnknownFields**：传入未定义字段会直接 `400 invalid request`，前端不要顺手回传多余字段。
- 大整数 ID（模板 ID、投稿 ID、用户 ID、作品 ID）在响应中一律为 **字符串**；分类 ID 为数字。

### 1.6 分页

列表接口统一使用 query 参数：

| 参数 | 类型 | 默认 | 约束 |
|---|---|---|---|
| `page.page` | int | 1 | ≥ 1 |
| `page.pageSize` | int | 100 | 1 ~ 100 |

响应中的分页结构：

```json
"page": { "total": 128, "page": 1, "pageSize": 20, "hasMore": true }
```

---

## 2. 登录

### 2.1 管理员登录

`POST /api/v1/admin/login`（无需鉴权）

请求：

```json
{ "username": "operator", "password": "correct horse battery staple" }
```

响应：

```json
{
  "header": { "code": 0, "message": "success" },
  "accessToken": "eyJhbGciOi...",
  "expiresIn": 86400
}
```

说明：

- `expiresIn` 单位为秒（当前 86400，即 24 小时）。这是**闲置超时**，持续操作会自动续期，见 1.2.1。
- 同一用户名连续 **5 次** 密码错误后锁定 **15 分钟**，锁定期间返回 `429`，`message = "too many failed login attempts"`。
- 用户名不存在与密码错误的返回一致（`401 invalid administrator credentials`），前端不要区分提示。

---

## 3. 预览图上传（媒体）

官方模板的预览图必须先通过下面任一通道上传，拿到 `fileKey` 后再传给发布/更新/审核通过接口。**服务端只接受 `fileKey`，不接受前端直传的 URL**，公开 URL 由服务端根据 fileKey 推导。

- 允许的类型：`image/jpeg`、`image/png`、`image/webp`
- 大小上限：**10 MB**
- 上传成功后服务端会异步生成最长边 600px 的 WebP 缩略图，模板的 `thumbnailUrl` 由此而来；生成失败时会退化为原图 URL。

### 3.1 方式 A：直传对象存储（推荐）

**Step 1 获取直传凭证**

`POST /api/v1/admin/media/upload-token`

```json
{ "fileName": "preview.png", "contentType": "image/png" }
```

响应：

```json
{
  "header": { "code": 0, "message": "success" },
  "uploadUrl": "https://bucket.oss-cn-x.aliyuncs.com/admin_preview/...?X-Amz-...",
  "fileKey": "admin_preview/2026/08/14/0/uuid.png",
  "headers": { "Content-Type": "image/png" },
  "expiresAt": 1755100000,
  "uploadMethod": "PUT",
  "maxFileSize": 10485760
}
```

**Step 2 前端按 `uploadMethod` + `headers` 把文件 PUT 到 `uploadUrl`**（凭证有效期 30 分钟）。

**Step 3 回报上传结果**

`POST /api/v1/admin/media/report-upload`

```json
{ "fileKey": "admin_preview/2026/08/14/0/uuid.png", "fileSize": 204800 }
```

响应：

```json
{ "header": { "code": 0, "message": "success" }, "fileUrl": "https://cdn.example.com/admin_preview/..." }
```

未回报的 fileKey 处于 `pending` 状态，直接拿去发布模板会被拒绝（`403 admin preview must be uploaded before publishing`）。

### 3.2 方式 B：经服务端代理上传（规避浏览器跨域）

`POST /api/v1/admin/media/upload`

- 请求体为**图片二进制**（不是 JSON），`Content-Type` 必须是允许的图片类型。
- 体积上限 10 MB，超出返回 `400 preview image is too large`；空 body 返回 `400 preview image is empty`。

响应：

```json
{
  "header": { "code": 0, "message": "success" },
  "fileKey": "admin_preview/2026/08/14/0/uuid.png",
  "fileUrl": "https://cdn.example.com/admin_preview/..."
}
```

该方式内部已完成"取凭证 + 上传 + 回报"，返回的 `fileKey` 可直接用于发布。

---

## 4. 模板分类

### 4.1 分类列表

`GET /api/v1/admin/template-categories`

响应：

```json
{
  "header": { "code": 0, "message": "success" },
  "categories": [
    { "categoryId": 1, "name": "节日", "templateCount": 12 }
  ]
}
```

无分页；`templateCount` 为该分类下已上架模板数。

### 4.2 新建分类

`POST /api/v1/admin/template-categories`

```json
{ "name": "节日" }
```

响应：

```json
{
  "header": { "code": 0, "message": "success" },
  "category": { "categoryId": 7, "name": "节日", "templateCount": 0 }
}
```

校验：名称首尾空格会被裁剪，长度 **1~30 字符**（按 Unicode 字符计），重名返回 `400 category name already exists`。

---

## 5. 官方模板管理

### 5.1 模板列表

`GET /api/v1/admin/templates?page.page=1&page.pageSize=20`

响应：

```json
{
  "header": { "code": 0, "message": "success" },
  "templates": [
    {
      "templateId": "1001",
      "title": "圣诞树",
      "categoryId": 1,
      "categoryName": "节日",
      "previewUrl": "https://cdn.example.com/admin_preview/....png",
      "thumbnailUrl": "https://cdn.example.com/admin_preview/thumb/....webp",
      "description": "适合新手",
      "tags": ["圣诞", "新手"],
      "difficulty": 2,
      "width": 29,
      "height": 29,
      "colorCount": 8
    }
  ],
  "page": { "total": 128, "page": 1, "pageSize": 20, "hasMore": true }
}
```

说明：

- `tags` 响应为字符串数组（入参为逗号分隔字符串，见 5.2）。
- 只返回已上架模板。
- 若某条模板的预览图 URL 不可用（非 `/`、`http://`、`https://` 开头），整个请求返回 `500`，属于数据异常。

### 5.2 发布模板

`POST /api/v1/admin/templates`

```json
{
  "idempotencyKey": "publish-20260814-001",
  "title": "圣诞树",
  "description": "适合新手",
  "categoryId": 1,
  "tags": "圣诞,新手",
  "difficulty": 2,
  "previewFileKey": "admin_preview/2026/08/14/0/uuid.png",
  "patternData": {
    "width": 29,
    "height": 29,
    "boardSpec": "29x29",
    "colorPalette": [
      { "index": 0, "hex": "#FFFFFF", "brand": "漫漫", "code": "A01", "name": "白" }
    ],
    "pixels": [0, 0, 1],
    "schemaVersion": 1
  }
}
```

字段说明：

| 字段 | 必填 | 说明 |
|---|---|---|
| `idempotencyKey` | 是 | 幂等键。相同键重复调用返回同一 `templateId`；键相同但内容归属不同来源时返回 `400 idempotency key conflict` |
| `title` | 是 | 首尾空格会被裁剪，不能为空 |
| `description` | 否 | 首尾空格会被裁剪 |
| `categoryId` | 是 | 必须 > 0 且指向**启用中**的分类，否则 `400 category_id must reference an active category` |
| `tags` | 否 | 逗号分隔字符串 |
| `difficulty` | 否 | int8 |
| `previewFileKey` | 是 | 必须是 admin 通道已完成上传的 fileKey |
| `patternData` | 是 | 见附录 A；`boardSpec` 必须等于 `"{width}x{height}"`，否则 `400 boardSpec must match pattern dimensions` |

`width` / `height` / `colorCount` / `beadCount` 由服务端从 `patternData` 计算，不接受前端传入。

响应：

```json
{ "header": { "code": 0, "message": "success" }, "templateId": "1001" }
```

### 5.3 模板详情

`GET /api/v1/admin/templates/{templateId}`

响应：

```json
{
  "header": { "code": 0, "message": "success" },
  "template": {
    "templateId": "1001",
    "title": "圣诞树",
    "categoryId": 1,
    "description": "适合新手",
    "tags": ["圣诞", "新手"],
    "difficulty": 2,
    "previewUrl": "https://cdn.example.com/...png",
    "thumbnailUrl": "https://cdn.example.com/...webp",
    "boardSpec": "29x29",
    "width": 29,
    "height": 29,
    "colorCount": 8
  },
  "patternData": { "width": 29, "height": 29, "boardSpec": "29x29", "colorPalette": [], "pixels": [] }
}
```

不存在返回 `404 template not found`；ID 非法（非正整数）返回 `400 invalid template id`。

### 5.4 更新模板

`PUT /api/v1/admin/templates/{templateId}`

请求体与 5.2 完全一致（`idempotencyKey` 在此接口被忽略，可不传）。校验规则同发布。

响应：

```json
{ "header": { "code": 0, "message": "success" }, "templateId": "1001" }
```

注意：这是**全量覆盖**，编辑前建议先调 5.3 拿到 `patternData` 与现有字段，再整体回传。

### 5.5 下架模板

`POST /api/v1/admin/templates/{templateId}/unpublish`

```json
{ "reason": "内容需要修订" }
```

- `reason` 可选，首尾空格裁剪后长度不得超过 **200 字符**，超出返回 `400 unpublish reason must not exceed 200 characters`。
- 成功响应仅含 `header`。
- 操作会记录 audit 日志（actor = token 中的管理员用户名）。

---

## 6. 用户投稿审核

用户把作品投稿为模板后进入审核队列，通过后会生成一条官方模板。

状态取值：`0 = 待审核(pending)`、`1 = 已通过(approved)`、`2 = 已驳回(rejected)`。

### 6.1 投稿列表

`GET /api/v1/admin/template-submissions?status=pending&page.page=1&page.pageSize=20`

| 参数 | 说明 |
|---|---|
| `status` | 可选，`pending` / `approved` / `rejected`；不传为全部；非法值返回 `400 status must be one of pending, approved, rejected` |
| `page.page` / `page.pageSize` | 同 1.6 |

响应：

```json
{
  "header": { "code": 0, "message": "success" },
  "submissions": [
    {
      "submissionId": "88",
      "userId": "1024",
      "workId": "2048",
      "title": "小猫",
      "description": "第一次投稿",
      "status": 0,
      "reviewReason": "",
      "reviewerActor": "",
      "templateId": "",
      "boardSpec": "29x29",
      "width": 29,
      "height": 29,
      "beadCount": 512,
      "colorCount": 8,
      "previewUrl": "https://cdn.example.com/...png",
      "thumbnailUrl": "https://cdn.example.com/...webp",
      "createdAt": 1755100000,
      "reviewedAt": 0
    }
  ],
  "page": { "total": 3, "page": 1, "pageSize": 20, "hasMore": false }
}
```

说明：

- `templateId` 仅在审核通过后有值，否则为空字符串；`reviewedAt` 未审核时为 `0`。
- 时间戳为**秒级** Unix 时间。
- 与模板列表不同，投稿的 `previewUrl` 允许为空（投稿图可能不在自有存储），此时审核人可依据 `patternData` 判断。

### 6.2 投稿详情

`GET /api/v1/admin/template-submissions/{submissionId}`

```json
{
  "header": { "code": 0, "message": "success" },
  "submission": { "...": "同 6.1 单项结构" },
  "patternData": { "width": 29, "height": 29, "boardSpec": "29x29", "colorPalette": [], "pixels": [] }
}
```

### 6.3 审核通过

`POST /api/v1/admin/template-submissions/{submissionId}/approve`

```json
{
  "categoryId": 1,
  "difficulty": 2,
  "tags": "动物,新手",
  "title": "小猫",
  "description": "运营润色后的描述",
  "previewFileKey": "admin_preview/2026/08/14/0/uuid.png"
}
```

字段说明：

| 字段 | 必填 | 说明 |
|---|---|---|
| `categoryId` | 是 | 必须指向启用中的分类 |
| `difficulty` | 否 | int8 |
| `tags` | 否 | 逗号分隔字符串 |
| `title` | 否 | 留空则沿用投稿标题 |
| `description` | 否 | 留空则沿用投稿描述 |
| `previewFileKey` | 否 | 只有运营重新上传预览图时才传；留空则沿用投稿自带的预览图与缩略图 |

响应：

```json
{ "header": { "code": 0, "message": "success" }, "templateId": "1001" }
```

行为说明：

- **幂等**：已通过的投稿再次调用会返回同一个 `templateId`，不会重复建模板（幂等键由投稿 ID 派生）。
- 已驳回的投稿再调用 approve 返回 `400 submission already rejected`。
- 若投稿自带预览图为空且未传 `previewFileKey`，返回 `400 preview image required`。
- 模板会带上投稿人署名（用户 ID + 昵称快照）；昵称读取失败不阻塞发布。
- 并发审核会返回 `2004 submission was reviewed concurrently`。

### 6.4 审核驳回

`POST /api/v1/admin/template-submissions/{submissionId}/reject`

```json
{ "reason": "图案存在版权风险" }
```

- `reason` **必填**，裁剪后不能为空（`400 reason is required`），长度不超过 **200 字符**。
- 已驳回的投稿重复调用返回成功（幂等）。
- 已通过的投稿不能驳回，返回 `400 submission already approved; unpublish the template instead`——应改用 5.5 下架模板。
- 成功响应仅含 `header`。

---

## 7. 典型流程

### 7.1 运营手动发布官方模板

```
POST /admin/login                     -> accessToken
POST /admin/template-categories       (可选，先建分类)
POST /admin/media/upload-token        -> uploadUrl / fileKey
PUT  uploadUrl                        (前端直传图片)
POST /admin/media/report-upload       -> 确认上传完成
POST /admin/templates                 -> templateId
```

### 7.2 用户投稿审核

```
GET  /admin/template-submissions?status=pending
GET  /admin/template-submissions/{id}                  (查看 patternData 决策)
POST /admin/media/upload               (可选：重做预览图)
POST /admin/template-submissions/{id}/approve          -> templateId
或
POST /admin/template-submissions/{id}/reject
```

### 7.3 模板下架

```
GET  /admin/templates
POST /admin/templates/{id}/unpublish
```

---

## 附录 A：patternData 结构

对应 proto `PatternData`（`pkg/proto/work.proto`），JSON 采用 protojson 的 lowerCamelCase 字段名。

```json
{
  "width": 29,
  "height": 29,
  "boardSpec": "29x29",
  "colorPalette": [
    { "index": 0, "hex": "#FFFFFF", "brand": "漫漫", "code": "A01", "name": "白" }
  ],
  "pixels": [0, 1, 1, 0],
  "schemaVersion": 1
}
```

| 字段 | 类型 | 说明 |
|---|---|---|
| `width` / `height` | int32 | 图纸宽高，必须 > 0 |
| `boardSpec` | string | 豆板规格，必须等于 `"{width}x{height}"` |
| `colorPalette` | ColorEntry[] | 颜色表，`index` 与 `pixels` 中的取值对应 |
| `pixels` | int32[] | **展平**的像素索引（长度应为 width × height），不支持二维数组 |
| `schemaVersion` | int32 | 图纸数据结构版本 |

ColorEntry：`index`（颜色索引）、`hex`（十六进制色值）、`brand`（品牌）、`code`（品牌色号）、`name`（颜色名称）。

---

## 附录 B：接口速查

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| POST | `/api/v1/admin/login` | 否 | 管理员登录 |
| POST | `/api/v1/admin/media/upload-token` | 是 | 取预览图直传凭证 |
| POST | `/api/v1/admin/media/upload` | 是 | 服务端代理上传预览图（二进制 body） |
| POST | `/api/v1/admin/media/report-upload` | 是 | 回报直传结果 |
| GET | `/api/v1/admin/template-categories` | 是 | 分类列表 |
| POST | `/api/v1/admin/template-categories` | 是 | 新建分类 |
| GET | `/api/v1/admin/templates` | 是 | 已上架模板分页列表 |
| POST | `/api/v1/admin/templates` | 是 | 发布模板（幂等） |
| GET | `/api/v1/admin/templates/{id}` | 是 | 模板详情（含 patternData） |
| PUT | `/api/v1/admin/templates/{id}` | 是 | 全量更新模板 |
| POST | `/api/v1/admin/templates/{id}/unpublish` | 是 | 下架模板 |
| GET | `/api/v1/admin/template-submissions` | 是 | 投稿列表（可按状态过滤） |
| GET | `/api/v1/admin/template-submissions/{id}` | 是 | 投稿详情（含 patternData） |
| POST | `/api/v1/admin/template-submissions/{id}/approve` | 是 | 审核通过并生成模板 |
| POST | `/api/v1/admin/template-submissions/{id}/reject` | 是 | 审核驳回 |

未匹配的 `/api/v1/admin/*` 路径统一返回 `404 route not found`。

---

## 附录 C：当前尚无后台接口的领域

以下能力目前**没有**管理端接口，后台页面不要预期：用户管理、积分与订阅管理、举报处理、社区内容审核、AI 生图任务管理。AI 任务相关接口（`/api/v1/ai/style-generations` 等）走普通用户鉴权，属于客户端接口。
