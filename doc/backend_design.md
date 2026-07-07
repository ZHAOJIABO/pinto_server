# 拼豆 App 后端服务设计方案

## 1. 项目概述

拼豆 App 是一款将图片转换为拼豆图纸的工具类应用。图纸生成逻辑在客户端实现，后端服务负责提供通用基础能力：用户认证、数据存储、社区展示、支付订阅等。

### 1.1 多端支持

后端服务需支持以下客户端：

| 客户端 | 状态 | 说明 |
|--------|------|------|
| iOS App | 首期开发 | 主要客户端 |
| Android App | 首期开发 | 主要客户端 |
| 微信小程序 | 后续接入 | 轻量级体验入口 |
| Web 端 | 后续接入 | PC 浏览器使用 |

### 1.2 多端架构设计要点

- **统一 API 接口**: 使用 gRPC + gRPC-Gateway 同时提供 gRPC（移动端）和 RESTful HTTP（小程序/Web）接口
- **认证体系兼容**: 支持手机号、微信（App/小程序/公众号三种 OAuth 场景）、Apple ID 等多种登录方式，统一用户体系
- **Token 机制**: JWT access/refresh token，各端共用同一套 token 验证逻辑
- **平台标识**: 请求 Header 携带 `X-Platform`（ios/android/miniprogram/web），用于区分平台特定逻辑（如支付方式、分享链路）
- **支付适配**: App 端使用 IAP，小程序使用微信支付 JSAPI，Web 端使用微信支付 Native/H5
- **CORS 配置**: HTTP Gateway 层预留跨域配置，支持 Web 端直接调用

---

## 2. 技术栈

| 层级 | 技术选型 | 说明 |
|------|----------|------|
| 语言 | Go 1.24+ | 高性能、强类型 |
| API 框架 | gRPC + gRPC-Gateway | 同时提供 gRPC 和 REST 接口 |
| 接口定义 | Protocol Buffers | 强类型接口契约 |
| 关系数据库 | MySQL 8.0 (GORM) | 主要业务数据 |
| 缓存 | Redis 7.0 | 会话缓存、限流、分布式锁 |
| 对象存储 | 阿里云 OSS | 图片/文件存储 |
| 认证 | JWT (access + refresh token) | 无状态认证 |
| 短信 | 腾讯云 SMS | 手机验证码 |
| 日志 | Zap + Lumberjack | 结构化日志 + 日志轮转 |
| 监控 | Prometheus | 指标采集 |
| 部署 | Docker + GitLab CI | 容器化 + 自动部署 |

---

## 3. 项目结构

```
bobobeads_server/
├── cmd/
│   └── main.go                    # 服务入口
├── internal/
│   ├── api/                       # gRPC Handler 层
│   │   ├── auth.go                # 认证接口
│   │   ├── user.go                # 用户接口
│   │   ├── work.go                # 作品接口
│   │   ├── community.go           # 社区接口
│   │   ├── template.go            # 模板接口
│   │   ├── subscribe.go           # 订阅支付接口
│   │   ├── media.go               # 文件上传接口
│   │   └── system.go              # 系统配置接口
│   ├── service/                   # 业务逻辑层
│   │   ├── auth/                  # 认证服务
│   │   ├── user/                  # 用户服务
│   │   ├── work/                  # 作品服务
│   │   ├── community/             # 社区服务
│   │   ├── template/              # 模板服务
│   │   ├── subscribe/             # 订阅服务
│   │   ├── credit/                # 积分服务
│   │   ├── invite/                # 邀请服务
│   │   ├── media/                 # 文件服务
│   │   └── bead/                  # 拼豆数据服务
│   ├── dao/                       # 数据访问层
│   ├── model/                     # 数据模型
│   ├── db/                        # 数据库初始化
│   ├── middleware/                 # 中间件（认证、限流、CORS）
│   ├── bootstrap/                 # 依赖注入与初始化
│   ├── constants/                 # 常量定义
│   ├── utils/                     # 工具函数
│   └── task/                      # 后台任务
├── pkg/
│   └── proto/                     # Protobuf 定义文件
│       ├── auth.proto
│       ├── user.proto
│       ├── work.proto
│       ├── community.proto
│       ├── template.proto
│       ├── subscribe.proto
│       ├── media.proto
│       ├── system.proto
│       └── common.proto
├── conf/                          # 配置文件
│   └── server.yaml
├── doc/                           # 文档
├── migrations/                    # 数据库迁移
├── Makefile
├── Dockerfile
├── docker-compose.yml
└── go.mod
```

---

## 4. 功能模块详细设计

### 4.1 认证与用户管理（AuthService / UserService）

#### 功能列表

| 接口 | 说明 | 多端差异 |
|------|------|----------|
| GuestLogin | 游客登录，体验后再注册 | 各端通用 |
| PhoneLogin | 手机号 + 短信验证码 | 各端通用 |
| WechatLogin | 微信授权登录 | App 用 SDK，小程序用 wx.login，Web 用扫码 |
| AppleLogin | Apple ID 登录 | 仅 iOS |
| RefreshToken | 刷新 access token | 各端通用 |
| GetUserInfo | 获取用户信息 | 各端通用 |
| UpdateUserInfo | 修改昵称/头像 | 各端通用 |
| DeleteAccount | 注销账号 | 各端通用 |
| BindPhone | 绑定手机号（游客升级） | 各端通用 |

#### 微信登录多端适配

```
App 端:       微信开放平台 → App OAuth → unionid
小程序端:     wx.login → code2Session → unionid
Web 端:       微信开放平台 → 扫码登录 → unionid
```

通过 unionid 统一用户身份，同一微信账号在不同端登录识别为同一用户。

---

### 4.2 作品管理（WorkService）

用户在客户端生成拼豆图纸后，将结果保存到服务端。

#### 功能列表

| 接口 | 说明 |
|------|------|
| SaveWork | 保存作品（图纸图片 + 配色数据） |
| GetWork | 获取作品详情 |
| ListWorks | 作品列表（分页） |
| DeleteWork | 删除作品 |
| SaveDraft | 保存草稿（未完成的设计） |
| ListDrafts | 草稿列表 |

#### 作品数据结构

```json
{
  "id": "work_id",
  "user_id": "user_id",
  "title": "作品名称",
  "original_image_url": "原图 OSS 地址",
  "pattern_image_url": "图纸图片 OSS 地址",
  "pattern_data": {
    "width": 29,
    "height": 29,
    "board_spec": "29x29",
    "pixels": [[1, 5, 3, ...], ...],
    "color_palette": [
      {"index": 1, "hex": "#FF5733", "brand": "hama", "code": "H-04"},
      {"index": 2, "hex": "#33FF57", "brand": "hama", "code": "H-11"}
    ]
  },
  "bead_count": 841,
  "color_count": 12,
  "status": "completed",
  "created_at": "2026-06-01T10:00:00Z"
}
```

---

### 4.3 文件存储（MediaService）

#### 功能列表

| 接口 | 说明 |
|------|------|
| GetUploadToken | 获取 OSS 直传签名（客户端直传） |
| ReportUpload | 客户端上传完成后回调通知 |
| GetFileUrl | 获取文件访问 URL（带签名） |

#### 上传流程

```
客户端 → GetUploadToken → 服务端返回 OSS STS 凭证
客户端 → 直传 OSS → 上传成功
客户端 → ReportUpload → 服务端记录文件元信息
```

采用客户端直传 OSS 方案，减轻服务端流量压力，适合图片较多的场景。

---

### 4.4 社区/展示（CommunityService）

#### 功能列表

| 接口 | 说明 |
|------|------|
| PublishWork | 分享作品到社区 |
| UnpublishWork | 取消分享 |
| GetFeed | 社区信息流（热门/最新/关注） |
| GetWorkDetail | 社区作品详情 |
| LikeWork | 点赞 |
| UnlikeWork | 取消点赞 |
| FavoriteWork | 收藏 |
| UnfavoriteWork | 取消收藏 |
| AddComment | 评论 |
| ListComments | 评论列表 |
| ReportContent | 举报 |
| FollowUser | 关注用户 |
| UnfollowUser | 取消关注 |

#### 信息流策略

- **热门**: 按综合分排序（点赞数 × 0.4 + 收藏数 × 0.3 + 评论数 × 0.2 + 时间衰减 × 0.1）
- **最新**: 按发布时间倒序
- **关注**: 关注用户的作品按时间倒序

---

### 4.5 模板/素材库（TemplateService）

#### 功能列表

| 接口 | 说明 |
|------|------|
| ListCategories | 模板分类列表 |
| ListTemplates | 分类下模板列表 |
| GetTemplate | 模板详情（含图纸数据） |
| ListColorPalettes | 配色方案列表 |

#### 模板分类

- 动物
- 花卉植物
- 卡通动漫
- 食物
- 节日主题
- 文字符号
- 几何图案
- 游戏像素

---

### 4.6 订阅与支付（SubscribeService）

#### 功能列表

| 接口 | 说明 | 多端差异 |
|------|------|----------|
| ListProducts | 商品列表 | 各端价格可不同 |
| CreateOrder | 创建订单 | 各端通用 |
| GetPaymentParams | 获取支付参数 | 按平台返回不同参数 |
| PaymentCallback | 支付回调 | IAP/微信支付各自回调 |
| RestorePurchase | 恢复购买 | 仅 iOS |
| GetSubscription | 查询订阅状态 | 各端通用 |
| ListOrders | 订单历史 | 各端通用 |

#### 支付方式适配

| 平台 | 支付方式 | 说明 |
|------|----------|------|
| iOS | Apple IAP | App Store 内购 |
| Android | Google Play / 微信支付 | 按地区选择 |
| 小程序 | 微信支付 JSAPI | 小程序内支付 |
| Web | 微信支付 Native | 扫码支付 |

#### 商品设计

| SKU | 说明 | 权益 |
|-----|------|------|
| free | 免费用户 | 每日 3 次生成 |
| vip_weekly | 周会员 | 无限生成 + 高级模板 |
| vip_monthly | 月会员 | 无限生成 + 高级模板 + 无水印导出 |
| vip_yearly | 年会员 | 全部权益 + 优先客服 |

---

### 4.7 积分/额度系统（CreditService）

#### 功能列表

| 接口 | 说明 |
|------|------|
| GetBalance | 查询积分余额 |
| ListTransactions | 积分流水 |
| DeductCredit | 消耗积分（内部调用） |
| AddCredit | 增加积分（内部调用） |

#### 积分获取方式

| 来源 | 数量 | 说明 |
|------|------|------|
| 每日签到 | +1 | 每天登录赠送 |
| 邀请好友 | +5 | 好友注册成功 |
| 分享作品 | +1 | 首次分享到社区 |
| 观看广告 | +1 | 激励视频广告 |
| 会员赠送 | +30/月 | 会员每月额外赠送 |

#### 积分消耗

| 操作 | 消耗 | 说明 |
|------|------|------|
| 生成图纸 | 1 次 | 免费用户每日 3 次免费 |
| 下载高级模板 | 3 次 | 付费模板 |
| 无水印导出 | 1 次 | 非会员需消耗积分 |

---

### 4.8 系统配置（SystemService）

#### 功能列表

| 接口 | 说明 |
|------|------|
| GetAppConfig | 获取 App 配置 |
| CheckUpdate | 版本检查 |
| GetBanners | 首页 Banner |
| GetAnnouncements | 公告列表 |
| GetBeadColors | 拼豆颜色库 |
| GetBoardSpecs | 豆板规格列表 |

#### 拼豆颜色库

服务端维护各品牌拼豆的颜色数据，客户端启动时拉取并缓存：

```json
{
  "brands": [
    {
      "name": "hama",
      "display_name": "Hama",
      "colors": [
        {"code": "H-01", "name": "白色", "hex": "#FFFFFF"},
        {"code": "H-02", "name": "奶油色", "hex": "#FFF5E1"},
        {"code": "H-03", "name": "黄色", "hex": "#FFD700"}
      ]
    },
    {
      "name": "perler",
      "display_name": "Perler",
      "colors": [...]
    }
  ]
}
```

#### 豆板规格

```json
{
  "specs": [
    {"name": "小方板", "width": 15, "height": 15, "bead_size": "5mm"},
    {"name": "标准方板", "width": 29, "height": 29, "bead_size": "5mm"},
    {"name": "大方板", "width": 39, "height": 39, "bead_size": "5mm"},
    {"name": "六角板", "shape": "hexagon", "radius": 15, "bead_size": "5mm"},
    {"name": "迷你方板", "width": 29, "height": 29, "bead_size": "2.6mm"}
  ]
}
```

---

### 4.9 邀请系统（InviteService）

#### 功能列表

| 接口 | 说明 |
|------|------|
| GetInviteCode | 获取邀请码 |
| BindInviteCode | 绑定邀请码（被邀请人） |
| GetInviteStats | 邀请统计 |
| ListInviteRecords | 邀请记录 |

#### 邀请奖励

- 邀请人：获得 5 积分
- 被邀请人：获得 3 积分
- 邀请满 5 人：额外赠送 3 天 VIP

---

### 4.10 数据上报（ReportService）

#### 功能列表

| 接口 | 说明 |
|------|------|
| ReportEvent | 上报事件 |
| ReportError | 错误日志上报 |
| SubmitFeedback | 用户反馈 |

#### 关键埋点事件

- `work_generate_start` — 开始生成
- `work_generate_success` — 生成成功
- `work_generate_fail` — 生成失败
- `work_save` — 保存作品
- `work_share` — 分享到社区
- `template_download` — 下载模板
- `payment_start` — 发起支付
- `payment_success` — 支付成功

---

## 5. 数据库设计

### 5.1 核心表

```sql
-- 用户表
CREATE TABLE bb_user (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    uuid VARCHAR(36) UNIQUE NOT NULL,
    nickname VARCHAR(64),
    avatar_url VARCHAR(512),
    phone VARCHAR(20),
    wechat_unionid VARCHAR(64),
    wechat_openid_app VARCHAR(64),
    wechat_openid_mp VARCHAR(64),    -- 小程序 openid
    wechat_openid_web VARCHAR(64),   -- 公众号/网页 openid
    apple_id VARCHAR(128),
    status TINYINT DEFAULT 1,        -- 1:正常 2:禁用 3:注销
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    INDEX idx_phone (phone),
    INDEX idx_wechat_unionid (wechat_unionid)
);

-- 作品表
CREATE TABLE bb_work (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    user_id BIGINT NOT NULL,
    title VARCHAR(128),
    original_image_url VARCHAR(512),
    pattern_image_url VARCHAR(512),
    pattern_data JSON,               -- 图纸像素数据
    board_spec VARCHAR(32),          -- 豆板规格
    width INT,
    height INT,
    bead_count INT,
    color_count INT,
    status TINYINT DEFAULT 1,        -- 1:草稿 2:已完成
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    INDEX idx_user_id (user_id),
    INDEX idx_created_at (created_at)
);

-- 社区分享表
CREATE TABLE bb_community_post (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    user_id BIGINT NOT NULL,
    work_id BIGINT NOT NULL,
    description TEXT,
    like_count INT DEFAULT 0,
    favorite_count INT DEFAULT 0,
    comment_count INT DEFAULT 0,
    status TINYINT DEFAULT 1,        -- 1:正常 2:隐藏 3:违规下架
    created_at DATETIME NOT NULL,
    INDEX idx_user_id (user_id),
    INDEX idx_created_at (created_at)
);

-- 点赞表
CREATE TABLE bb_like (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    user_id BIGINT NOT NULL,
    post_id BIGINT NOT NULL,
    created_at DATETIME NOT NULL,
    UNIQUE INDEX uk_user_post (user_id, post_id)
);

-- 收藏表
CREATE TABLE bb_favorite (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    user_id BIGINT NOT NULL,
    post_id BIGINT NOT NULL,
    created_at DATETIME NOT NULL,
    UNIQUE INDEX uk_user_post (user_id, post_id)
);

-- 评论表
CREATE TABLE bb_comment (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    user_id BIGINT NOT NULL,
    post_id BIGINT NOT NULL,
    parent_id BIGINT DEFAULT 0,      -- 回复哪条评论
    content VARCHAR(500),
    status TINYINT DEFAULT 1,
    created_at DATETIME NOT NULL,
    INDEX idx_post_id (post_id)
);

-- 关注表
CREATE TABLE bb_follow (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    follower_id BIGINT NOT NULL,
    following_id BIGINT NOT NULL,
    created_at DATETIME NOT NULL,
    UNIQUE INDEX uk_follow (follower_id, following_id)
);

-- 模板表
CREATE TABLE bb_template (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    category_id INT NOT NULL,
    title VARCHAR(128),
    preview_url VARCHAR(512),
    pattern_data JSON,
    board_spec VARCHAR(32),
    is_free TINYINT DEFAULT 1,       -- 1:免费 0:付费
    credit_cost INT DEFAULT 0,
    download_count INT DEFAULT 0,
    sort_order INT DEFAULT 0,
    status TINYINT DEFAULT 1,
    created_at DATETIME NOT NULL
);

-- 模板分类表
CREATE TABLE bb_template_category (
    id INT PRIMARY KEY AUTO_INCREMENT,
    name VARCHAR(64),
    icon_url VARCHAR(512),
    sort_order INT DEFAULT 0,
    status TINYINT DEFAULT 1
);

-- 订单表
CREATE TABLE bb_order (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    order_no VARCHAR(64) UNIQUE NOT NULL,
    user_id BIGINT NOT NULL,
    product_id INT NOT NULL,
    amount DECIMAL(10, 2),
    currency VARCHAR(8) DEFAULT 'CNY',
    payment_method VARCHAR(32),       -- iap/wechat_jsapi/wechat_native
    platform VARCHAR(16),             -- ios/android/miniprogram/web
    status TINYINT DEFAULT 0,         -- 0:待支付 1:已支付 2:已退款 3:已关闭
    paid_at DATETIME,
    transaction_id VARCHAR(128),
    created_at DATETIME NOT NULL,
    INDEX idx_user_id (user_id),
    INDEX idx_order_no (order_no)
);

-- 商品表
CREATE TABLE bb_product (
    id INT PRIMARY KEY AUTO_INCREMENT,
    sku VARCHAR(64) UNIQUE NOT NULL,
    name VARCHAR(128),
    description VARCHAR(512),
    price DECIMAL(10, 2),
    currency VARCHAR(8) DEFAULT 'CNY',
    duration_days INT,                -- 会员时长
    platform VARCHAR(16),             -- 适用平台，空为全平台
    apple_product_id VARCHAR(128),    -- IAP product id
    status TINYINT DEFAULT 1,
    sort_order INT DEFAULT 0
);

-- 用户订阅表
CREATE TABLE bb_subscription (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    user_id BIGINT NOT NULL,
    product_id INT NOT NULL,
    order_id BIGINT NOT NULL,
    start_at DATETIME NOT NULL,
    expire_at DATETIME NOT NULL,
    status TINYINT DEFAULT 1,         -- 1:生效中 2:已过期 3:已取消
    INDEX idx_user_id (user_id),
    INDEX idx_expire_at (expire_at)
);

-- 积分流水表
CREATE TABLE bb_credit_transaction (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    user_id BIGINT NOT NULL,
    amount INT NOT NULL,              -- 正数增加，负数消耗
    balance INT NOT NULL,             -- 变动后余额
    type VARCHAR(32),                 -- daily_free/invite/share/purchase/consume
    ref_type VARCHAR(32),             -- 关联类型
    ref_id VARCHAR(64),               -- 关联 ID
    description VARCHAR(256),
    created_at DATETIME NOT NULL,
    INDEX idx_user_id (user_id),
    INDEX idx_created_at (created_at)
);

-- 邀请记录表
CREATE TABLE bb_invite (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    inviter_id BIGINT NOT NULL,
    invitee_id BIGINT NOT NULL,
    invite_code VARCHAR(16) NOT NULL,
    reward_granted TINYINT DEFAULT 0,
    created_at DATETIME NOT NULL,
    INDEX idx_inviter_id (inviter_id),
    INDEX idx_invite_code (invite_code)
);

-- 拼豆颜色库
CREATE TABLE bb_bead_color (
    id INT PRIMARY KEY AUTO_INCREMENT,
    brand VARCHAR(32) NOT NULL,       -- hama/perler/artkal
    code VARCHAR(16) NOT NULL,        -- 品牌色号
    name VARCHAR(64),
    hex VARCHAR(7) NOT NULL,          -- #RRGGBB
    r TINYINT UNSIGNED,
    g TINYINT UNSIGNED,
    b TINYINT UNSIGNED,
    category VARCHAR(32),             -- 色系分类
    status TINYINT DEFAULT 1,
    UNIQUE INDEX uk_brand_code (brand, code)
);

-- 豆板规格表
CREATE TABLE bb_board_spec (
    id INT PRIMARY KEY AUTO_INCREMENT,
    name VARCHAR(64),
    shape VARCHAR(16) DEFAULT 'square', -- square/hexagon/circle
    width INT,
    height INT,
    bead_size DECIMAL(3,1),           -- 豆子直径 mm
    status TINYINT DEFAULT 1
);

-- 系统配置表
CREATE TABLE bb_config (
    id INT PRIMARY KEY AUTO_INCREMENT,
    config_key VARCHAR(128) UNIQUE NOT NULL,
    config_value TEXT,
    description VARCHAR(256),
    updated_at DATETIME
);

-- 用户反馈表
CREATE TABLE bb_feedback (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    user_id BIGINT NOT NULL,
    content TEXT,
    contact VARCHAR(128),
    platform VARCHAR(16),
    app_version VARCHAR(16),
    status TINYINT DEFAULT 0,         -- 0:待处理 1:已处理
    created_at DATETIME NOT NULL
);
```

---

## 6. 中间件设计

### 6.1 认证中间件

```
请求 → 提取 Authorization Header → 解析 JWT → 注入 user_id 到 context
```

白名单接口（无需认证）：
- GuestLogin
- PhoneLogin / SendSmsCode
- WechatLogin
- GetAppConfig / CheckUpdate
- GetFeed（社区浏览）

### 6.2 平台识别中间件

从 Header 提取客户端信息：
- `X-Platform`: ios / android / miniprogram / web
- `X-App-Version`: 1.0.0
- `X-Device-Id`: 设备唯一标识

### 6.3 限流中间件

- 全局 QPS 限制
- 单用户 QPS 限制
- 短信验证码：同一手机号 60s 间隔，每日上限 10 条

### 6.4 CORS 中间件

Web 端和小程序 webview 需要跨域支持，在 HTTP Gateway 层配置 CORS。

---

## 7. 后台任务

| 任务 | 频率 | 说明 |
|------|------|------|
| 订阅过期检查 | 每小时 | 检查并标记过期订阅 |
| 每日积分重置 | 每日 00:00 | 重置免费用户每日次数 |
| 社区内容审核 | 实时/定时 | 检查违规内容 |
| 数据统计汇总 | 每日 | 生成运营统计数据 |
| 过期文件清理 | 每周 | 清理未关联的 OSS 文件 |

---

## 8. 接口版本与兼容性

- URL 路径包含版本号：`/api/v1/...`
- Proto 文件通过 package 区分版本
- 新增字段向后兼容（Protobuf 天然支持）
- 废弃字段标记 deprecated，保留 2 个版本后移除

---

## 9. 安全设计

| 措施 | 说明 |
|------|------|
| HTTPS | 全链路 TLS |
| JWT 签名 | RS256 非对称签名 |
| 请求签名 | 关键接口请求体签名防篡改 |
| 限流 | Redis 令牌桶限流 |
| SQL 注入 | GORM 参数化查询 |
| XSS | 输出转义 + CSP Header |
| 文件上传 | 类型/大小校验，OSS 防盗链 |
| 敏感数据 | 手机号脱敏存储 |

---

## 10. 开发阶段规划

### 第一期（MVP）

- [ ] 项目脚手架搭建
- [ ] 认证模块（手机号 + 游客）
- [ ] 作品保存与管理
- [ ] 文件上传（OSS 直传）
- [ ] 拼豆颜色库 & 豆板规格
- [ ] 系统配置接口
- [ ] 基础积分系统（每日免费次数）

### 第二期

- [ ] 微信登录
- [ ] 订阅与支付（IAP）
- [ ] 模板库
- [ ] 邀请系统
- [ ] 数据上报

### 第三期

- [ ] 社区功能（分享/点赞/评论）
- [ ] 小程序适配（微信登录 + JSAPI 支付）
- [ ] Web 端适配（CORS + 扫码支付）
- [ ] 内容审核
- [ ] 推送通知

### 第四期（扩展）

- [ ] 物料清单（计算用豆量）
- [ ] 电商导购（一键购买豆子）
- [ ] PDF 导出（实际尺寸打印）
- [ ] AI 增强（服务端图片处理）
- [ ] 多语言支持

---

## 11. 部署架构

```
                    ┌─────────────┐
                    │   Nginx     │
                    │  (反向代理)  │
                    └──────┬──────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
       ┌──────┴──────┐    │    ┌───────┴──────┐
       │ HTTP Gateway │    │    │  gRPC Server │
       │  (REST API)  │    │    │  (移动端直连) │
       │  :8080       │    │    │  :9090       │
       └──────┬──────┘    │    └───────┬──────┘
              │            │            │
              └────────────┼────────────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
        ┌─────┴─────┐ ┌───┴───┐ ┌─────┴─────┐
        │  MySQL     │ │ Redis │ │ Aliyun OSS│
        │  (主从)    │ │       │ │           │
        └───────────┘ └───────┘ └───────────┘
```

- 小程序/Web 端通过 HTTP Gateway 访问 REST API
- App 端可直连 gRPC Server 或走 HTTP Gateway
- Nginx 负责 TLS 终止、负载均衡、CORS
