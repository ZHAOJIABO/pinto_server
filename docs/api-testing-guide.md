# BoBo Beads Server API Testing Guide (Apifox)

## 环境配置

### 本地服务器

| 配置项 | 值 |
|--------|-----|
| Base URL | `http://localhost:8080` |
| gRPC 端口 | `9090` |
| HTTP 端口 | `8080` |

### Apifox 设置

1. 创建项目，设置 Base URL 为 `http://localhost:8080`
2. 创建环境变量：
   - `{{base_url}}` = `http://localhost:8080`
   - `{{token}}` = 登录后获取的 access_token
3. 在全局 Header 中设置：
   - `Authorization`: `Bearer {{token}}`
   - `Content-Type`: `application/json`

### 获取 Token 流程

先调用 Guest Login 获取 token，然后将返回的 `access_token` 设置到环境变量 `{{token}}` 中。

---

## 通用说明

### 请求格式

所有 POST/PUT 请求体为 JSON，由 proto message 映射而来。客户端统一使用 **lowerCamelCase**；grpc-gateway 对旧的 snake_case 输入保持兼容，但不应继续作为新接口示例。

### 响应格式

所有响应包含 `header` 字段：

```json
{
  "header": {
    "code": 0,
    "message": "success",
    "traceId": "uuid-string"
  }
}
```

- `code = 0` 表示成功
- `code != 0` 表示业务错误

### 分页参数 (GET 请求)

对于包含 `PageRequest page` 的 GET 接口，使用 grpc-gateway 嵌套消息语法：

```
?page.page=1&page.page_size=20
```

### 认证

除以下接口外，所有接口需要 `Authorization: Bearer <token>` header：
- `POST /api/v1/auth/guest`
- `POST /api/v1/auth/phone`
- `POST /api/v1/auth/wechat`
- `POST /api/v1/auth/apple`
- `POST /api/v1/auth/sms/send`
- `POST /api/v1/auth/refresh`

---

## 1. Auth 认证服务

### 1.1 游客登录

```
POST /api/v1/auth/guest
```

**Request Body:**
```json
{
  "device_id": "test-device-001"
}
```

**Response:**
```json
{
  "header": {"code": 0, "message": "success", "traceId": "..."},
  "accessToken": "eyJhbGciOiJIUzI1NiIs...",
  "refreshToken": "eyJhbGciOiJIUzI1NiIs...",
  "expiresIn": "259200",
  "user": {
    "userId": "1",
    "nickname": "用户test-d",
    "avatarUrl": "",
    "phone": "",
    "isVip": false
  }
}
```

### 1.2 手机号登录

```
POST /api/v1/auth/phone
```

**Request Body:**
```json
{
  "phone": "13800138000",
  "code": "123456"
}
```

### 1.3 发送短信验证码

```
POST /api/v1/auth/sms/send
```

**Request Body:**
```json
{
  "phone": "13800138000"
}
```

### 1.4 微信登录

```
POST /api/v1/auth/wechat
```

**Request Body:**
```json
{
  "code": "wx-auth-code",
  "platform": 1
}
```

> platform: 0=UNKNOWN, 1=IOS, 2=ANDROID, 3=MINIPROGRAM, 4=WEB

### 1.5 Apple 登录

```
POST /api/v1/auth/apple
```

**Request Body:**
```json
{
  "identity_token": "apple-identity-token",
  "authorization_code": "apple-auth-code",
  "full_name": "张三"
}
```

### 1.6 刷新 Token

```
POST /api/v1/auth/refresh
```

**Request Body:**
```json
{
  "refresh_token": "eyJhbGciOiJIUzI1NiIs..."
}
```

---

## 2. User 用户服务

### 2.1 获取用户信息

```
GET /api/v1/user/info
```

**Request:** 无参数

**Response:**
```json
{
  "header": {"code": 0, "message": "success", "traceId": "..."},
  "user": {
    "userId": "1",
    "nickname": "用户test-d",
    "avatarUrl": "",
    "phone": "",
    "isVip": false
  },
  "creditBalance": 10,
  "dailyFreeRemaining": 3
}
```

### 2.2 更新用户信息

```
PUT /api/v1/user/info
```

**Request Body:**
```json
{
  "nickname": "新昵称",
  "avatar_url": "https://cdn/avatar.png"
}
```

### 2.3 绑定手机号

```
POST /api/v1/user/bind-phone
```

**Request Body:**
```json
{
  "phone": "13800138000",
  "code": "123456"
}
```

### 2.4 注销账号

```
DELETE /api/v1/user/account
```

**Request:** 无参数

---

## 3. Template 模板服务 (新增)

### 3.1 获取模板分类列表

```
GET /api/v1/templates/categories
```

**Request:** 无参数

**Response:**
```json
{
  "header": {"code": 0, "message": "success", "traceId": "..."},
  "categories": [
    {
      "categoryId": 1,
      "name": "动物",
      "iconUrl": "https://cdn/animal.png",
      "templateCount": 2
    },
    {
      "categoryId": 2,
      "name": "风景",
      "iconUrl": "https://cdn/scenery.png",
      "templateCount": 0
    }
  ]
}
```

### 3.2 获取模板列表

```
GET /api/v1/templates
```

**Query Params:**
| 参数 | 类型 | 说明 |
|------|------|------|
| scene | string | 场景：`home` (首页推荐) |
| category_id | int | 按分类筛选 |
| keyword | string | 关键词搜索（标题/标签） |
| page.page | int | 页码，默认 1 |
| page.page_size | int | 每页条数，默认 20 |

**示例:**
```
GET /api/v1/templates?scene=home&page.page=1&page.page_size=20
GET /api/v1/templates?category_id=1&page.page=1&page.page_size=10
GET /api/v1/templates?keyword=猫&page.page=1&page.page_size=20
```

**Response:**
```json
{
  "header": {"code": 0, "message": "success", "traceId": "..."},
  "templates": [
    {
      "templateId": "1",
      "title": "小猫咪",
      "previewUrl": "https://cdn/cat.png",
      "boardSpec": "",
      "colorCount": 12,
      "isFree": true,
      "creditCost": 0,
      "downloadCount": 0,
      "thumbnailUrl": "https://cdn/cat.png",
      "description": "",
      "tags": ["猫", "动物", "可爱"],
      "difficulty": 2,
      "favoriteCount": 0,
      "isFavorited": false,
      "width": 29,
      "height": 29
    }
  ],
  "page": {
    "total": 2,
    "page": 1,
    "pageSize": 20,
    "hasMore": false
  }
}
```

### 3.3 获取模板详情

```
GET /api/v1/templates/{template_id}
```

**示例:** `GET /api/v1/templates/1`

**Response:**
```json
{
  "header": {"code": 0, "message": "success", "traceId": "..."},
  "template": {
    "templateId": "1",
    "title": "小猫咪",
    "previewUrl": "https://cdn/cat.png",
    "boardSpec": "29x29",
    "colorCount": 2,
    "isFree": true,
    "creditCost": 0,
    "downloadCount": 1,
    "thumbnailUrl": "https://cdn/cat.png",
    "description": "可爱的小猫咪拼豆图纸",
    "tags": ["猫", "动物", "可爱"],
    "difficulty": 2,
    "favoriteCount": 1,
    "isFavorited": false,
    "width": 3,
    "height": 3
  },
  "patternData": {
    "width": 3,
    "height": 3,
    "boardSpec": "29x29",
    "pixels": [1, 1, 0, 1, 2, 1, 0, 1, 1],
    "colorPalette": [
      {"index": 1, "hex": "#FF0000", "name": "Red"},
      {"index": 2, "hex": "#00FF00", "name": "Green"}
    ],
    "schemaVersion": 1
  }
}
```

> `patternData` 只有模板有图纸数据时才返回，否则为 null

### 3.4 收藏模板

```
POST /api/v1/templates/{template_id}/favorite
```

**Request Body:** `{}`（空 body）

**示例:** `POST /api/v1/templates/1/favorite`

**Response:**
```json
{
  "header": {"code": 0, "message": "success", "traceId": "..."},
  "isFavorited": true,
  "favoriteCount": 1
}
```

### 3.5 取消收藏模板

```
DELETE /api/v1/templates/{template_id}/favorite
```

**示例:** `DELETE /api/v1/templates/1/favorite`

**Response:**
```json
{
  "header": {"code": 0, "message": "success", "traceId": "..."},
  "isFavorited": false,
  "favoriteCount": 0
}
```

### 3.6 获取我的收藏模板

```
GET /api/v1/templates/favorites
```

**Query Params:**
| 参数 | 类型 | 说明 |
|------|------|------|
| page.page | int | 页码 |
| page.page_size | int | 每页条数 |

**Response:** 同 ListTemplates

---

## 4. AI Generation AI风格生成服务 (新增)

### 4.1 获取 AI 风格列表

```
GET /api/v1/ai/styles
```

**Request:** 无参数

**Response:**
```json
{
  "header": {"code": 0, "message": "success", "traceId": "..."},
  "styles": [
    {
      "styleId": "1",
      "styleKey": "watercolor",
      "name": "水彩风格",
      "description": "将图片转为水彩画风格",
      "coverUrl": "https://cdn/watercolor-cover.png",
      "exampleUrl": "https://cdn/watercolor-example.png",
      "costCredits": 2
    },
    {
      "styleId": "2",
      "styleKey": "pixel",
      "name": "像素风格",
      "description": "将图片转为像素艺术风格",
      "coverUrl": "https://cdn/pixel-cover.png",
      "exampleUrl": "https://cdn/pixel-example.png",
      "costCredits": 1
    }
  ]
}
```

### 4.2 创建 AI 风格生成任务

```
POST /api/v1/ai/style-generations
```

**Request Body:**
```json
{
  "style_id": "1",
  "input_file_key": "style_input/2026/07/08/5/xxxxx.png",
  "client_request_id": "unique-request-id-001"
}
```

> - `style_id`: 从 ListAIStyles 获取的风格 ID
> - `input_file_key`: 先通过 GetUploadToken (purpose=style_input) 上传图片后获得的 file_key
> - `client_request_id`: 客户端生成的幂等 key（UUID），重复提交会返回相同结果

**Response:**
```json
{
  "header": {"code": 0, "message": "success", "traceId": "..."},
  "taskId": "98f275e2-0cf9-4fec-bd16-cf151fd3f67a",
  "status": 0,
  "creditsDeducted": 2,
  "remainingBalance": 8,
  "duplicated": false
}
```

> - `status`: 0=pending, 1=processing, 2=succeeded, 3=failed
> - `duplicated`: true 表示这是幂等重试，不会重复扣费

### 4.3 查询 AI 生成任务状态

```
GET /api/v1/ai/style-generations/{task_id}
```

**示例:** `GET /api/v1/ai/style-generations/98f275e2-0cf9-4fec-bd16-cf151fd3f67a`

**Response:**
```json
{
  "header": {"code": 0, "message": "success", "traceId": "..."},
  "task": {
    "taskId": "98f275e2-0cf9-4fec-bd16-cf151fd3f67a",
    "styleId": "1",
    "styleName": "水彩风格",
    "inputImageUrl": "https://cdn/input.png",
    "outputImageUrl": "https://fake-ai-output.example.com/result.png",
    "status": 2,
    "creditsDeducted": 2,
    "errorMessage": "",
    "createdAt": "1783446355",
    "completedAt": "1783446355"
  }
}
```

### 4.4 获取 AI 生成任务列表

```
GET /api/v1/ai/style-generations
```

**Query Params:**
| 参数 | 类型 | 说明 |
|------|------|------|
| page.page | int | 页码 |
| page.page_size | int | 每页条数 |

**示例:** `GET /api/v1/ai/style-generations?page.page=1&page.page_size=10`

**Response:**
```json
{
  "header": {"code": 0, "message": "success", "traceId": "..."},
  "tasks": [
    {
      "taskId": "98f275e2-...",
      "styleId": "1",
      "styleName": "",
      "inputImageUrl": "",
      "outputImageUrl": "https://fake-ai-output.example.com/result.png",
      "status": 2,
      "creditsDeducted": 2,
      "errorMessage": "",
      "createdAt": "1783446355",
      "completedAt": "1783446355"
    }
  ],
  "page": {
    "total": 1,
    "page": 1,
    "pageSize": 10,
    "hasMore": false
  }
}
```

---

## 5. Media 媒体服务

### 5.1 获取上传凭证

```
POST /api/v1/media/upload-token
```

**Request Body:**
```json
{
  "file_name": "photo.png",
  "content_type": "image/png",
  "purpose": "style_input"
}
```

> purpose 可选值：
> - `original` - 原始图片
> - `pattern` - 图纸图片
> - `avatar` - 头像
> - `community` - 社区帖子
> - `style_input` - AI 风格生成输入图片 (新增)
> - `ai_output` - AI 生成输出图片 (新增)

**Response:**
```json
{
  "header": {"code": 0, "message": "success", "traceId": "..."},
  "uploadUrl": "https://bobobeads.oss-cn-beijing.aliyuncs.com/style_input/2026/07/08/...",
  "fileKey": "style_input/2026/07/08/5/80a01ea5-...",
  "formData": {},
  "expiresAt": "1783448077",
  "headers": {"Content-Type": "image/png"},
  "uploadMethod": "PUT",
  "publicUrl": "https://bobobeads.oss-cn-beijing.aliyuncs.com/style_input/...",
  "maxFileSize": "20971520"
}
```

### 5.2 上报上传完成

```
POST /api/v1/media/report-upload
```

**Request Body:**
```json
{
  "file_key": "style_input/2026/07/08/5/80a01ea5-...",
  "file_size": 102400
}
```

### 5.3 获取文件 URL

```
GET /api/v1/media/url?file_key=style_input/2026/07/08/5/xxxxx.png
```

---

## 6. Generation 图纸生成服务

### 6.1 创建生成任务

```
POST /api/v1/generation/create
```

**Request Body:**
```json
{
  "boardSpec": "29x29",
  "sourceType": "photo",
  "sourceId": "",
  "clientRequestId": "gen-unique-001"
}
```

> source_type 可选值：
> - `photo` - 照片生成
> - `album` - 相册选择
> - `template` - 模板生成（source_id = template_id）
> - `ai_style` - AI 风格生成结果（source_id = ai_task_id）(新增)

**Response:**
```json
{
  "header": {"code": 0, "message": "success", "traceId": "..."},
  "generationId": "uuid-string",
  "creditsDeducted": 0,
  "remainingBalance": 10,
  "expiresAt": "1783448000",
  "duplicated": false
}
```

### 6.2 完成生成（提交结果）

```
POST /api/v1/generation/{generationId}/complete
```

**Request Body:**
```json
{
  "title": "我的拼豆作品",
  "originalImageUrl": "oss://original.jpg",
  "patternImageUrl": "oss://pattern.jpg",
  "patternData": {
    "width": 3,
    "height": 3,
    "boardSpec": "29x29",
    "pixels": [1, 1, 0, 1, 2, 1, 0, 1, 1],
    "colorPalette": [
      {"index": 1, "hex": "#FF0000", "name": "Red"},
      {"index": 2, "hex": "#00FF00", "name": "Green"}
    ],
    "schemaVersion": 1
  },
  "beadCount": 7,
  "colorCount": 2
}
```

`beadCount`、`colorCount` 仅为兼容字段，服务端会根据 `patternData.pixels` 重算并存储最终统计值。`patternData.boardSpec` 必须与创建 generation 时的 `boardSpec` 一致。

**Response:**
```json
{
  "header": {"code": 0, "message": "success", "traceId": "..."},
  "workId": "123",
  "duplicated": false
}
```

### 6.3 取消生成

```
POST /api/v1/generation/{generationId}/cancel
```

**Request Body:**
```json
{
  "reason": "用户主动取消"
}
```

**Response:**
```json
{
  "header": {"code": 0, "message": "success", "traceId": "..."},
  "creditsRefunded": 1
}
```

### 6.4 查询生成状态

```
GET /api/v1/generation/{generationId}
```

**Response:**
```json
{
  "header": {"code": 0, "message": "success", "traceId": "..."},
  "status": 0,
  "creditsDeducted": 1,
  "workId": ""
}
```

> status: 0=pending, 1=completed, 2=cancelled, 3=expired

---

## 7. Work 作品服务

### 7.1 保存作品

```
POST /api/v1/works
```

**Request Body:**
```json
{
  "title": "我的作品",
  "originalImageUrl": "oss://original.jpg",
  "patternImageUrl": "oss://pattern.jpg",
  "patternData": {
    "width": 3,
    "height": 3,
    "boardSpec": "29x29",
    "pixels": [1, 1, 0, 1, 2, 1, 0, 1, 1],
    "colorPalette": [
      {"index": 1, "hex": "#FF0000", "name": "Red"},
      {"index": 2, "hex": "#00FF00", "name": "Green"}
    ],
    "schemaVersion": 1
  },
  "beadCount": 7,
  "colorCount": 2
}
```

### 7.2 获取作品详情

```
GET /api/v1/works/{workId}
```

**Response:**
```json
{
  "header": {"code": 0, "message": "success", "traceId": "..."},
  "work": {
    "workId": "1",
    "title": "我的作品",
    "originalImageUrl": "oss://original.jpg",
    "patternImageUrl": "oss://pattern.jpg",
    "boardSpec": "29x29",
    "width": 3,
    "height": 3,
    "beadCount": 7,
    "colorCount": 2,
    "status": 2,
    "createdAt": "1783446000",
    "thumbnailUrl": "",
    "updatedAt": "1783446000",
    "sourceType": "ai_style",
    "sourceId": "98f275e2-..."
  },
  "patternData": {
    "width": 3,
    "height": 3,
    "boardSpec": "29x29",
    "pixels": [1, 1, 0, 1, 2, 1, 0, 1, 1],
    "colorPalette": [
      {"index": 1, "hex": "#FF0000", "name": "Red"},
      {"index": 2, "hex": "#00FF00", "name": "Green"}
    ],
    "schemaVersion": 1
  }
}
```

### 7.3 获取作品列表

```
GET /api/v1/works
```

**Query Params:**
| 参数 | 类型 | 说明 |
|------|------|------|
| page.page | int | 页码 |
| page.pageSize | int | 每页条数 |
| sourceType | string | 来源筛选：photo/template/ai_style (新增) |

**示例:** `GET /api/v1/works?page.page=1&page.pageSize=20&sourceType=ai_style`

### 7.4 删除作品

```
DELETE /api/v1/works/{workId}
```

### 7.5 保存草稿

```
POST /api/v1/works/drafts
```

**Request Body:**
```json
{
  "title": "草稿",
  "originalImageUrl": "oss://original.jpg",
  "draftId": ""
}
```

> `draftId` 为空则新建，非空则更新。未完成草稿可以暂不传 `patternData`；传入时必须符合完整 `PatternData` 契约。

### 7.6 获取草稿列表

```
GET /api/v1/works/drafts?page.page=1&page.pageSize=20
```

---

## 8. Credit 积分服务

### 8.1 获取积分余额

```
GET /api/v1/credits/balance
```

**Response:**
```json
{
  "header": {"code": 0, "message": "success", "traceId": "..."},
  "balance": 10,
  "dailyFreeRemaining": 3,
  "dailyFreeTotal": 3
}
```

### 8.2 获取积分流水

```
GET /api/v1/credits/transactions?page.page=1&page.page_size=20
```

**Response:**
```json
{
  "header": {"code": 0, "message": "success", "traceId": "..."},
  "transactions": [
    {
      "transactionId": "1",
      "amount": -2,
      "balanceAfter": 8,
      "type": "ai_style_generation",
      "description": "AI风格生成",
      "createdAt": "1783446355"
    }
  ],
  "page": {"total": 1, "page": 1, "pageSize": 20, "hasMore": false}
}
```

---

## 9. Subscribe 订阅服务

### 9.1 获取商品列表

```
GET /api/v1/subscribe/products
```

### 9.2 创建订单

```
POST /api/v1/subscribe/orders
```

**Request Body:**
```json
{
  "product_id": "1",
  "payment_method": "apple_iap"
}
```

### 9.3 获取支付参数

```
GET /api/v1/subscribe/orders/{order_no}/payment
```

### 9.4 支付回调

```
POST /api/v1/subscribe/callback
```

**Request Body:**
```json
{
  "payment_method": "apple_iap",
  "raw_data": "receipt-base64-data"
}
```

### 9.5 恢复购买

```
POST /api/v1/subscribe/restore
```

**Request Body:**
```json
{
  "receipt_data": "apple-receipt-base64"
}
```

### 9.6 获取订阅状态

```
GET /api/v1/subscribe/status
```

### 9.7 获取订单列表

```
GET /api/v1/subscribe/orders?page.page=1&page.page_size=20
```

---

## 10. Community 社区服务

### 10.1 发布作品到社区

```
POST /api/v1/community/posts
```

**Request Body:**
```json
{
  "work_id": "1",
  "description": "分享我的拼豆作品"
}
```

### 10.2 删除帖子

```
DELETE /api/v1/community/posts/{post_id}
```

### 10.3 获取 Feed

```
GET /api/v1/community/feed?feed_type=hot&page.page=1&page.page_size=20
```

> feed_type: `hot`(热门), `latest`(最新), `following`(关注)

### 10.4 获取帖子详情

```
GET /api/v1/community/posts/{post_id}
```

### 10.5 点赞

```
POST /api/v1/community/posts/{post_id}/like
```

**Request Body:** `{}`

### 10.6 取消点赞

```
DELETE /api/v1/community/posts/{post_id}/like
```

### 10.7 收藏帖子

```
POST /api/v1/community/posts/{post_id}/favorite
```

**Request Body:** `{}`

### 10.8 取消收藏帖子

```
DELETE /api/v1/community/posts/{post_id}/favorite
```

### 10.9 评论

```
POST /api/v1/community/posts/{post_id}/comments
```

**Request Body:**
```json
{
  "content": "做得真好看!",
  "parent_id": ""
}
```

> `parent_id` 不为空则为回复评论

### 10.10 获取评论列表

```
GET /api/v1/community/posts/{post_id}/comments?page.page=1&page.page_size=20
```

### 10.11 关注用户

```
POST /api/v1/community/users/{user_id}/follow
```

**Request Body:** `{}`

### 10.12 取消关注

```
DELETE /api/v1/community/users/{user_id}/follow
```

### 10.13 举报

```
POST /api/v1/community/report
```

**Request Body:**
```json
{
  "target_type": "post",
  "target_id": "1",
  "reason": "不当内容"
}
```

---

## 11. Invite 邀请服务

### 11.1 获取邀请码

```
GET /api/v1/invite/code
```

### 11.2 绑定邀请码

```
POST /api/v1/invite/bind
```

**Request Body:**
```json
{
  "invite_code": "ABC123"
}
```

### 11.3 获取邀请统计

```
GET /api/v1/invite/stats
```

### 11.4 获取邀请记录

```
GET /api/v1/invite/records?page.page=1&page.page_size=20
```

---

## 12. System 系统服务

### 12.1 获取 App 配置

```
GET /api/v1/system/config
```

### 12.2 检查更新

```
GET /api/v1/system/update?current_version=1.0.0
```

### 12.3 获取 Banner

```
GET /api/v1/system/banners
```

### 12.4 获取拼豆颜色表

```
GET /api/v1/system/bead-colors?brand=artkal
```

### 12.5 获取拼豆板规格

```
GET /api/v1/system/board-specs
```

---

## 13. Report 上报服务

### 13.1 上报事件

```
POST /api/v1/report/event
```

**Request Body:**
```json
{
  "events": [
    {
      "event_name": "page_view",
      "params": {"page": "home"},
      "timestamp": 1783446000
    }
  ]
}
```

### 13.2 上报错误

```
POST /api/v1/report/error
```

**Request Body:**
```json
{
  "error_type": "crash",
  "message": "null pointer exception",
  "stack_trace": "...",
  "context": {"screen": "generation"}
}
```

### 13.3 提交反馈

```
POST /api/v1/report/feedback
```

**Request Body:**
```json
{
  "content": "希望增加更多风格",
  "contact": "user@example.com",
  "image_urls": ["https://cdn/feedback1.png"]
}
```

---

## Apifox 测试流程示例

### 完整的 AI 风格生成流程

```
步骤 1: 游客登录 → 获取 token
步骤 2: 获取 AI 风格列表 → 选择 style_id
步骤 3: 获取上传凭证 (purpose=style_input) → 获取 upload_url 和 file_key
步骤 4: 上传图片到 OSS (用 upload_url PUT)
步骤 5: 上报上传完成 (report-upload) → 将 file_key 标记为已上传
步骤 6: 创建 AI 风格生成 → 获取 task_id
步骤 7: 轮询任务状态 (GetStyleGeneration) → 等待 status=2(succeeded)
步骤 8: 使用输出结果创建 generation (source_type=ai_style, source_id=task_id)
步骤 9: 完成 generation → 得到 work_id
步骤 10: 查看作品 (GetWork)
```

### 快速测试（跳过 OSS 上传）

如果只想测试 API 逻辑不需要真实上传文件，可以手动在数据库中插入 media 记录：

```sql
-- 获取你的 user_id（从 login response 中获取）
-- 然后手动插入一条已上传的媒体记录
INSERT INTO bb_media_asset (user_id, file_key, purpose, content_type, status, created_at, updated_at)
VALUES (<your_user_id>, 'style_input/test/fake-input.png', 'style_input', 'image/png', 1, NOW(), NOW());
```

然后用 `style_input/test/fake-input.png` 作为 `input_file_key` 调用 CreateStyleGeneration。

> 注意：本地开发环境 `fake_provider=true`，AI 生成会立即返回成功，无需等待。
