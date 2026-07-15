# BoBo Beads Server gRPC Testing Guide

## 环境配置

| 配置项 | 值 |
|--------|-----|
| gRPC 地址 | `localhost:9090` |
| 工具 | `grpcurl`（已安装在 `/Users/zhaojiabo/go/bin/grpcurl`） |
| 反射 | 已开启，无需 proto 文件 |

### 基本命令格式

```bash
# 无需认证的接口
grpcurl -plaintext -d '<json>' localhost:9090 bobobeads.v1.ServiceName/MethodName

# 需要认证的接口
grpcurl -plaintext -H "authorization: Bearer <token>" -d '<json>' localhost:9090 bobobeads.v1.ServiceName/MethodName
```

### 获取 Token

```bash
# 先用游客登录获取 token
TOKEN=$(grpcurl -plaintext -d '{"device_id":"grpc-test-001"}' localhost:9090 bobobeads.v1.AuthService/GuestLogin | python3 -c "import sys,json; print(json.load(sys.stdin)['accessToken'])")
echo $TOKEN
```

后续命令中用 `$TOKEN` 替代实际 token 值。

### 查看所有服务

```bash
grpcurl -plaintext localhost:9090 list
```

### 查看服务方法

```bash
grpcurl -plaintext localhost:9090 list bobobeads.v1.TemplateService
```

### 查看 message 结构

```bash
grpcurl -plaintext localhost:9090 describe bobobeads.v1.CreateStyleGenerationRequest
```

---

## 1. AuthService 认证服务

### 1.1 GuestLogin 游客登录

```bash
grpcurl -plaintext -d '{
  "device_id": "grpc-test-001"
}' localhost:9090 bobobeads.v1.AuthService/GuestLogin
```

### 1.2 PhoneLogin 手机号登录

```bash
grpcurl -plaintext -d '{
  "phone": "13800138000",
  "code": "123456"
}' localhost:9090 bobobeads.v1.AuthService/PhoneLogin
```

### 1.3 SendSmsCode 发送短信验证码

```bash
grpcurl -plaintext -d '{
  "phone": "13800138000"
}' localhost:9090 bobobeads.v1.AuthService/SendSmsCode
```

### 1.4 WechatLogin 微信登录

```bash
grpcurl -plaintext -d '{
  "code": "wx-auth-code-from-sdk",
  "platform": 1
}' localhost:9090 bobobeads.v1.AuthService/WechatLogin
```

> platform 枚举: 0=UNKNOWN, 1=IOS, 2=ANDROID, 3=MINIPROGRAM, 4=WEB

### 1.5 AppleLogin Apple 登录

```bash
grpcurl -plaintext -d '{
  "identity_token": "apple-jwt-identity-token",
  "authorization_code": "apple-auth-code",
  "full_name": "张三"
}' localhost:9090 bobobeads.v1.AuthService/AppleLogin
```

### 1.6 RefreshToken 刷新令牌

```bash
grpcurl -plaintext -d '{
  "refresh_token": "eyJhbGciOiJIUzI1NiIs..."
}' localhost:9090 bobobeads.v1.AuthService/RefreshToken
```

---

## 2. UserService 用户服务

### 2.1 GetUserInfo 获取用户信息

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{}' localhost:9090 bobobeads.v1.UserService/GetUserInfo
```

### 2.2 UpdateUserInfo 更新用户信息

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "nickname": "拼豆达人",
  "avatar_url": "https://cdn/new-avatar.png"
}' localhost:9090 bobobeads.v1.UserService/UpdateUserInfo
```

### 2.3 BindPhone 绑定手机号

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "phone": "13800138000",
  "code": "123456"
}' localhost:9090 bobobeads.v1.UserService/BindPhone
```

### 2.4 DeleteAccount 注销账号

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{}' localhost:9090 bobobeads.v1.UserService/DeleteAccount
```

---

## 3. TemplateService 模板服务

### 3.1 ListCategories 获取分类列表

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{}' localhost:9090 bobobeads.v1.TemplateService/ListCategories
```

### 3.2 ListTemplates 获取模板列表

**首页推荐：**
```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "page": {"page": 1, "page_size": 20},
  "scene": "home"
}' localhost:9090 bobobeads.v1.TemplateService/ListTemplates
```

**按分类筛选：**
```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "category_id": 1,
  "page": {"page": 1, "page_size": 20}
}' localhost:9090 bobobeads.v1.TemplateService/ListTemplates
```

**关键词搜索：**
```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "keyword": "猫",
  "page": {"page": 1, "page_size": 20}
}' localhost:9090 bobobeads.v1.TemplateService/ListTemplates
```

### 3.3 GetTemplate 获取模板详情

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "template_id": "1"
}' localhost:9090 bobobeads.v1.TemplateService/GetTemplate
```

### 3.4 FavoriteTemplate 收藏模板

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "template_id": "1"
}' localhost:9090 bobobeads.v1.TemplateService/FavoriteTemplate
```

### 3.5 UnfavoriteTemplate 取消收藏

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "template_id": "1"
}' localhost:9090 bobobeads.v1.TemplateService/UnfavoriteTemplate
```

### 3.6 ListFavoriteTemplates 获取收藏列表

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "page": {"page": 1, "page_size": 20}
}' localhost:9090 bobobeads.v1.TemplateService/ListFavoriteTemplates
```

---

## 4. AIGenerationService AI 风格生成服务

### 4.1 ListAIStyles 获取 AI 风格列表

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{}' localhost:9090 bobobeads.v1.AIGenerationService/ListAIStyles
```

### 4.2 CreateStyleGeneration 创建 AI 风格生成

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "style_id": "1",
  "input_file_key": "style_input/2026/07/08/5/80a01ea5-8854-4f88-89d4-54c5c57fdcb0.png",
  "client_request_id": "grpc-test-uuid-001"
}' localhost:9090 bobobeads.v1.AIGenerationService/CreateStyleGeneration
```

> - `style_id`: 从 ListAIStyles 获取
> - `input_file_key`: 需先通过 MediaService 获取上传凭证并上传，再 report-upload
> - `client_request_id`: 客户端生成的幂等 UUID，相同值重复调用返回相同结果

### 4.3 GetStyleGeneration 查询任务状态

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "task_id": "98f275e2-0cf9-4fec-bd16-cf151fd3f67a"
}' localhost:9090 bobobeads.v1.AIGenerationService/GetStyleGeneration
```

> status 含义: 0=pending, 1=processing, 2=succeeded, 3=failed

### 4.4 ListStyleGenerations 获取任务列表

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "page": {"page": 1, "page_size": 10}
}' localhost:9090 bobobeads.v1.AIGenerationService/ListStyleGenerations
```

---

## 5. MediaService 媒体服务

### 5.1 GetUploadToken 获取上传凭证

**AI 风格输入图片：**
```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "file_name": "my-photo.png",
  "content_type": "image/png",
  "purpose": "style_input"
}' localhost:9090 bobobeads.v1.MediaService/GetUploadToken
```

**原始图片：**
```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "file_name": "original.jpg",
  "content_type": "image/jpeg",
  "purpose": "original"
}' localhost:9090 bobobeads.v1.MediaService/GetUploadToken
```

**头像：**
```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "file_name": "avatar.png",
  "content_type": "image/png",
  "purpose": "avatar"
}' localhost:9090 bobobeads.v1.MediaService/GetUploadToken
```

> purpose 可选值: `original`, `pattern`, `avatar`, `community`, `style_input`, `ai_output`

### 5.2 ReportUpload 上报上传完成

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "file_key": "style_input/2026/07/08/5/80a01ea5-8854-4f88-89d4-54c5c57fdcb0.png",
  "file_size": 102400
}' localhost:9090 bobobeads.v1.MediaService/ReportUpload
```

### 5.3 GetFileUrl 获取文件访问地址

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "file_key": "style_input/2026/07/08/5/80a01ea5-8854-4f88-89d4-54c5c57fdcb0.png"
}' localhost:9090 bobobeads.v1.MediaService/GetFileUrl
```

---

## 6. GenerationService 图纸生成服务

### 6.1 CreateGeneration 创建生成任务

**从照片生成：**
```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "board_spec": "29x29",
  "source_type": "photo",
  "source_id": "",
  "client_request_id": "gen-photo-001"
}' localhost:9090 bobobeads.v1.GenerationService/CreateGeneration
```

**从 AI 风格结果生成：**
```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "board_spec": "29x29",
  "source_type": "ai_style",
  "source_id": "98f275e2-0cf9-4fec-bd16-cf151fd3f67a",
  "client_request_id": "gen-ai-001"
}' localhost:9090 bobobeads.v1.GenerationService/CreateGeneration
```

**从模板生成：**
```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "board_spec": "29x29",
  "source_type": "template",
  "source_id": "1",
  "client_request_id": "gen-tpl-001"
}' localhost:9090 bobobeads.v1.GenerationService/CreateGeneration
```

> source_type 可选值: `photo`, `album`, `template`, `ai_style`

### 6.2 CompleteGeneration 完成生成

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "generation_id": "your-generation-id",
  "title": "我的拼豆作品",
  "original_image_url": "original/2026/test.jpg",
  "pattern_image_url": "pattern/2026/test.png",
  "pattern_data": {
    "width": 3,
    "height": 3,
    "board_spec": "29x29",
    "pixels": [1, 2, 3, 1, 2, 3, 1, 2, 3],
    "color_palette": [
      {"index": 1, "hex": "#FF0000", "brand": "artkal", "code": "C01", "name": "红色"},
      {"index": 2, "hex": "#00FF00", "brand": "artkal", "code": "C02", "name": "绿色"},
      {"index": 3, "hex": "#0000FF", "brand": "artkal", "code": "C03", "name": "蓝色"}
    ],
    "schema_version": 1
  },
  "bead_count": 9,
  "color_count": 3
}' localhost:9090 bobobeads.v1.GenerationService/CompleteGeneration
```

### 6.3 CancelGeneration 取消生成

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "generation_id": "your-generation-id",
  "reason": "不想要了"
}' localhost:9090 bobobeads.v1.GenerationService/CancelGeneration
```

### 6.4 GetGenerationStatus 查询生成状态

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "generation_id": "your-generation-id"
}' localhost:9090 bobobeads.v1.GenerationService/GetGenerationStatus
```

> status: 0=pending, 1=completed, 2=cancelled, 3=expired

---

## 7. WorkService 作品服务

### 7.1 SaveWork 保存作品

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "title": "直接保存的作品",
  "original_image_url": "original/2026/direct.jpg",
  "pattern_image_url": "pattern/2026/direct.png",
  "pattern_data": {
    "width": 5,
    "height": 1,
    "board_spec": "15x15",
    "pixels": [1, 2, 1, 2, 1],
    "color_palette": [
      {"index": 1, "hex": "#FF6600", "name": "橙色"},
      {"index": 2, "hex": "#6600FF", "name": "紫色"}
    ],
    "schema_version": 1
  },
  "bead_count": 5,
  "color_count": 2
}' localhost:9090 bobobeads.v1.WorkService/SaveWork
```

### 7.2 GetWork 获取作品详情

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "work_id": "1"
}' localhost:9090 bobobeads.v1.WorkService/GetWork
```

### 7.3 ListWorks 获取作品列表

**全部作品：**
```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "page": {"page": 1, "page_size": 20}
}' localhost:9090 bobobeads.v1.WorkService/ListWorks
```

**按来源筛选：**
```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "page": {"page": 1, "page_size": 20},
  "source_type": "ai_style"
}' localhost:9090 bobobeads.v1.WorkService/ListWorks
```

> source_type 可选值: `photo`, `template`, `ai_style`（空字符串表示全部）

### 7.4 DeleteWork 删除作品

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "work_id": "1"
}' localhost:9090 bobobeads.v1.WorkService/DeleteWork
```

### 7.5 SaveDraft 保存草稿

**新建草稿：**
```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "title": "未完成的作品",
  "original_image_url": "original/2026/draft.jpg"
}' localhost:9090 bobobeads.v1.WorkService/SaveDraft
```

**更新草稿：**
```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "draft_id": "existing-draft-id",
  "title": "更新后的草稿"
}' localhost:9090 bobobeads.v1.WorkService/SaveDraft
```

### 7.6 ListDrafts 获取草稿列表

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "page": {"page": 1, "page_size": 20}
}' localhost:9090 bobobeads.v1.WorkService/ListDrafts
```

---

## 8. CreditService 积分服务

### 8.1 GetBalance 获取余额

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{}' localhost:9090 bobobeads.v1.CreditService/GetBalance
```

### 8.2 ListTransactions 获取流水

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "page": {"page": 1, "page_size": 20}
}' localhost:9090 bobobeads.v1.CreditService/ListTransactions
```

---

## 9. SubscribeService 订阅服务

### 9.1 ListProducts 获取商品列表

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{}' localhost:9090 bobobeads.v1.SubscribeService/ListProducts
```

### 9.2 CreateOrder 创建订单

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "product_id": "1",
  "payment_method": "apple_iap"
}' localhost:9090 bobobeads.v1.SubscribeService/CreateOrder
```

### 9.3 GetPaymentParams 获取支付参数

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "order_no": "ORDER202607080001"
}' localhost:9090 bobobeads.v1.SubscribeService/GetPaymentParams
```

### 9.4 PaymentCallback 支付回调

```bash
grpcurl -plaintext -d '{
  "payment_method": "wechat_pay",
  "raw_data": "{\"out_trade_no\":\"ORDER202607080001\",\"transaction_id\":\"wx123\"}"
}' localhost:9090 bobobeads.v1.SubscribeService/PaymentCallback
```

### 9.5 RestorePurchase 恢复购买

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "receipt_data": "base64-encoded-apple-receipt"
}' localhost:9090 bobobeads.v1.SubscribeService/RestorePurchase
```

### 9.6 GetSubscription 获取订阅状态

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{}' localhost:9090 bobobeads.v1.SubscribeService/GetSubscription
```

### 9.7 ListOrders 获取订单列表

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "page": {"page": 1, "page_size": 20}
}' localhost:9090 bobobeads.v1.SubscribeService/ListOrders
```

---

## 10. CommunityService 社区服务

### 10.1 PublishWork 发布作品

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "work_id": "1",
  "description": "看看我做的拼豆小猫咪！"
}' localhost:9090 bobobeads.v1.CommunityService/PublishWork
```

### 10.2 UnpublishWork 取消发布

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "post_id": "1"
}' localhost:9090 bobobeads.v1.CommunityService/UnpublishWork
```

### 10.3 GetFeed 获取动态流

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "feed_type": "hot",
  "page": {"page": 1, "page_size": 20}
}' localhost:9090 bobobeads.v1.CommunityService/GetFeed
```

> feed_type: `hot`(热门), `latest`(最新), `following`(关注)

### 10.4 GetPostDetail 获取帖子详情

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "post_id": "1"
}' localhost:9090 bobobeads.v1.CommunityService/GetPostDetail
```

### 10.5 LikePost 点赞

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "post_id": "1"
}' localhost:9090 bobobeads.v1.CommunityService/LikePost
```

### 10.6 UnlikePost 取消点赞

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "post_id": "1"
}' localhost:9090 bobobeads.v1.CommunityService/UnlikePost
```

### 10.7 FavoritePost 收藏帖子

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "post_id": "1"
}' localhost:9090 bobobeads.v1.CommunityService/FavoritePost
```

### 10.8 UnfavoritePost 取消收藏帖子

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "post_id": "1"
}' localhost:9090 bobobeads.v1.CommunityService/UnfavoritePost
```

### 10.9 AddComment 发表评论

**一级评论：**
```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "post_id": "1",
  "content": "做得真好看！配色很棒",
  "parent_id": ""
}' localhost:9090 bobobeads.v1.CommunityService/AddComment
```

**回复评论：**
```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "post_id": "1",
  "content": "谢谢！",
  "parent_id": "comment-id-to-reply"
}' localhost:9090 bobobeads.v1.CommunityService/AddComment
```

### 10.10 ListComments 获取评论列表

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "post_id": "1",
  "page": {"page": 1, "page_size": 20}
}' localhost:9090 bobobeads.v1.CommunityService/ListComments
```

### 10.11 FollowUser 关注用户

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "user_id": "2"
}' localhost:9090 bobobeads.v1.CommunityService/FollowUser
```

### 10.12 UnfollowUser 取消关注

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "user_id": "2"
}' localhost:9090 bobobeads.v1.CommunityService/UnfollowUser
```

### 10.13 ReportContent 举报内容

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "target_type": "post",
  "target_id": "1",
  "reason": "违规内容"
}' localhost:9090 bobobeads.v1.CommunityService/ReportContent
```

> target_type: `post`, `comment`

---

## 11. InviteService 邀请服务

### 11.1 GetInviteCode 获取邀请码

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{}' localhost:9090 bobobeads.v1.InviteService/GetInviteCode
```

### 11.2 BindInviteCode 绑定邀请码

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "invite_code": "ABC123"
}' localhost:9090 bobobeads.v1.InviteService/BindInviteCode
```

### 11.3 GetInviteStats 获取邀请统计

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{}' localhost:9090 bobobeads.v1.InviteService/GetInviteStats
```

### 11.4 ListInviteRecords 获取邀请记录

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "page": {"page": 1, "page_size": 20}
}' localhost:9090 bobobeads.v1.InviteService/ListInviteRecords
```

---

## 12. SystemService 系统服务

### 12.1 GetAppConfig 获取应用配置

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{}' localhost:9090 bobobeads.v1.SystemService/GetAppConfig
```

### 12.2 CheckUpdate 检查更新

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "current_version": "1.0.0"
}' localhost:9090 bobobeads.v1.SystemService/CheckUpdate
```

### 12.3 GetBanners 获取 Banner

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{}' localhost:9090 bobobeads.v1.SystemService/GetBanners
```

### 12.4 GetBeadColors 获取拼豆颜色表

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "brand": "artkal"
}' localhost:9090 bobobeads.v1.SystemService/GetBeadColors
```

### 12.5 GetBoardSpecs 获取拼豆板规格

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{}' localhost:9090 bobobeads.v1.SystemService/GetBoardSpecs
```

---

## 13. ReportService 上报服务

### 13.1 ReportEvent 上报事件

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "events": [
    {
      "event_name": "page_view",
      "params": {"page": "home", "duration": "5000"},
      "timestamp": 1783446000
    },
    {
      "event_name": "button_click",
      "params": {"button": "generate", "source": "photo"},
      "timestamp": 1783446005
    }
  ]
}' localhost:9090 bobobeads.v1.ReportService/ReportEvent
```

### 13.2 ReportError 上报错误

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "error_type": "crash",
  "message": "Unexpected null in pattern_data",
  "stack_trace": "at GenerationPage.build (generation_page.dart:42)\nat ...",
  "context": {"screen": "generation", "board_spec": "29x29", "os_version": "iOS 18.0"}
}' localhost:9090 bobobeads.v1.ReportService/ReportError
```

### 13.3 SubmitFeedback 提交反馈

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{
  "content": "希望能增加更多的拼豆颜色品牌支持",
  "contact": "user@example.com",
  "image_urls": ["https://cdn/feedback/screenshot1.png"]
}' localhost:9090 bobobeads.v1.ReportService/SubmitFeedback
```

---

## 端到端测试脚本

### AI 风格生成完整流程

```bash
#!/bin/bash

# 1. 登录
echo "=== Step 1: Guest Login ==="
LOGIN=$(grpcurl -plaintext -d '{"device_id":"e2e-test-001"}' localhost:9090 bobobeads.v1.AuthService/GuestLogin)
echo $LOGIN | python3 -m json.tool
TOKEN=$(echo $LOGIN | python3 -c "import sys,json; print(json.load(sys.stdin)['accessToken'])")

# 2. 查看 AI 风格
echo -e "\n=== Step 2: List AI Styles ==="
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{}' localhost:9090 bobobeads.v1.AIGenerationService/ListAIStyles

# 3. 获取上传凭证
echo -e "\n=== Step 3: Get Upload Token ==="
UPLOAD=$(grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{"file_name":"test.png","content_type":"image/png","purpose":"style_input"}' \
  localhost:9090 bobobeads.v1.MediaService/GetUploadToken)
echo $UPLOAD | python3 -m json.tool
FILE_KEY=$(echo $UPLOAD | python3 -c "import sys,json; print(json.load(sys.stdin)['fileKey'])")

# 4. 模拟上传完成
echo -e "\n=== Step 4: Report Upload ==="
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d "{\"file_key\":\"$FILE_KEY\",\"file_size\":102400}" \
  localhost:9090 bobobeads.v1.MediaService/ReportUpload

# 5. 创建 AI 生成
echo -e "\n=== Step 5: Create Style Generation ==="
AI_RESULT=$(grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d "{\"style_id\":\"1\",\"input_file_key\":\"$FILE_KEY\",\"client_request_id\":\"e2e-$(date +%s)\"}" \
  localhost:9090 bobobeads.v1.AIGenerationService/CreateStyleGeneration)
echo $AI_RESULT | python3 -m json.tool
TASK_ID=$(echo $AI_RESULT | python3 -c "import sys,json; print(json.load(sys.stdin)['taskId'])")

# 6. 查询任务状态
echo -e "\n=== Step 6: Get Task Status ==="
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d "{\"task_id\":\"$TASK_ID\"}" \
  localhost:9090 bobobeads.v1.AIGenerationService/GetStyleGeneration

# 7. 用 AI 结果创建图纸生成
echo -e "\n=== Step 7: Create Generation from AI ==="
GEN_RESULT=$(grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d "{\"board_spec\":\"29x29\",\"source_type\":\"ai_style\",\"source_id\":\"$TASK_ID\",\"client_request_id\":\"gen-$(date +%s)\"}" \
  localhost:9090 bobobeads.v1.GenerationService/CreateGeneration)
echo $GEN_RESULT | python3 -m json.tool
GEN_ID=$(echo $GEN_RESULT | python3 -c "import sys,json; print(json.load(sys.stdin)['generationId'])")

# 8. 完成生成
echo -e "\n=== Step 8: Complete Generation ==="
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d "{
  \"generation_id\":\"$GEN_ID\",
  \"title\":\"AI风格作品\",
  \"original_image_url\":\"original/e2e.jpg\",
  \"pattern_image_url\":\"pattern/e2e.png\",
  \"pattern_data\":{\"width\":29,\"height\":29,\"board_spec\":\"29x29\",\"pixels\":[1,2,3,1,2,3],\"color_palette\":[{\"index\":1,\"hex\":\"#FF0000\",\"name\":\"红\"}]},
  \"bead_count\":841,
  \"color_count\":1
}" localhost:9090 bobobeads.v1.GenerationService/CompleteGeneration

# 9. 查看作品
echo -e "\n=== Step 9: List Works ==="
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{"page":{"page":1,"page_size":20},"source_type":"ai_style"}' \
  localhost:9090 bobobeads.v1.WorkService/ListWorks

# 10. 查看积分余额
echo -e "\n=== Step 10: Check Balance ==="
grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{}' localhost:9090 bobobeads.v1.CreditService/GetBalance
```

---

## 快速测试（跳过 OSS 上传）

如果只想验证 AI 生成逻辑，可以在 MySQL 中手动插入一条已上传的媒体记录：

```bash
/usr/local/mysql-8.0.40-macos14-arm64/bin/mysql -u root -p1561000609 bobobeads -e "
INSERT INTO bb_media_asset (user_id, file_key, purpose, content_type, status, created_at, updated_at)
VALUES (<your_user_id>, 'style_input/test/fake-input.png', 'style_input', 'image/png', 1, NOW(), NOW());
"
```

然后直接用 `style_input/test/fake-input.png` 作为 `input_file_key`。

给用户添加积分：

```bash
/usr/local/mysql-8.0.40-macos14-arm64/bin/mysql -u root -p1561000609 bobobeads -e "
INSERT INTO bb_credit_account (user_id, balance, created_at, updated_at)
VALUES (<your_user_id>, 100, NOW(), NOW())
ON DUPLICATE KEY UPDATE balance = balance + 100;
"
```

> 本地环境 `fake_provider=true`，AI 生成会立即完成，无需轮询等待。
