# BoboBead Server
#ssh -N -L 13306:127.0.0.1:13306 root@123.57.175.126
go run ./cmd -config conf/server.yaml
拼豆 App 后端服务，提供用户认证、作品管理、社区、支付订阅等通用能力。图纸生成逻辑在客户端完成，服务端通过预扣费模型管控积分消耗。

## 技术栈

- **语言**: Go 1.25
- **API**: gRPC + gRPC-Gateway (REST)
- **数据库**: MySQL 8.0 (GORM) + Redis 7.0
- **存储**: 阿里云 OSS
- **认证**: JWT (access + refresh token)
- **Proto 管理**: buf

## 本地开发

```bash
# 请先按 conf/server.yaml（及未提交的 conf/server.local.yaml）准备 MySQL、Redis 和 OSS 凭证。
make run

# 或编译后运行
make build
./bin/server -config conf/server.yaml
```

服务启动后：
- gRPC: `:9090`（移动端）
- HTTP REST: `:8080`（小程序 / Web）

## 图纸导出水印

将两张 PNG 上传到 CDN 后，服务端优先从 `bb_config` 读取水印策略和完整 CDN 地址；数据库没有对应键时，才使用 YAML 兜底：

```yaml
export_watermark:
  # none（关闭）、marketing（宣传图）或 online（线上导出）
  mode: online
  # 客户端可直接访问的完整 CDN 图片地址。
  marketing_url: https://cdn.example.com/bobobeads/watermarks/marketing-v1.png
  online_url: https://cdn.example.com/bobobeads/watermarks/online-v1.png
```

客户端请求 `GET /api/v1/system/config` 后读取：

- `export_watermark_mode`：`none`、`marketing` 或 `online`；
- `export_watermark_url`：当前策略对应的 PNG。`none` 时为空。

线上可直接写入数据库，立即对下一次配置请求生效：

```sql
INSERT INTO bb_config (config_key, config_value, description)
VALUES
  ('export_watermark_mode', 'online', '图纸导出水印：none / marketing / online'),
  ('export_watermark_marketing_url', 'https://cdn.example.com/bobobeads/watermarks/marketing-v1.png', '宣传图水印 CDN 地址'),
  ('export_watermark_online_url', 'https://cdn.example.com/bobobeads/watermarks/online-v1.png', '线上水印 CDN 地址')
ON DUPLICATE KEY UPDATE config_value = VALUES(config_value);
```

仅需切换策略时，更新 `export_watermark_mode` 即可；未写当前模式对应的 CDN URL 时，服务会使用 YAML 中相应的 URL。服务端只向客户端下发最终的 `export_watermark_url`，不再托管水印图片。CDN 必须提供 HTTPS 地址，并为 Web Canvas 导出允许跨域读取（如 `Access-Control-Allow-Origin: *`）。

## AI 生图模型切换

`ai_generation.models` 是一张「逻辑模型名 → 适配器」的映射表，新增一个模型通常只改 YAML：

```yaml
ai_generation:
  # 兜底模型，必须是下面 models 里已配置的 key
  default_model: "gpt-image-2-c"
  # 用户主动重试用的模型，留空则重试沿用首发链条
  retry_model: "gemini-3-1-flash-image-preview"
  models:
    gpt-image-2-c:
      adapter: openai_image_edit        # OpenAI 兼容的 /v1/images/edits
      base_url: "https://api.vectorengine.cn"
      model: "gpt-image-2-c"
      options: { size: "1024x1024", quality: "medium" }
    gemini-3-1-flash-image-preview:
      adapter: gemini_generate_content  # Gemini 原生 :generateContent
      base_url: "https://api.vectorengine.cn"
      model: "gemini-3.1-flash-image-preview"
      options: { aspect_ratio: "1:1", image_size: "1K" }
```

- `models` 的 key 是对外的逻辑模型名，**不能带小数点**：viper 把点当层级分隔符，`gemini-3.1-flash` 会被拆成 `gemini-3`。上游真实模型名写在 `model` 字段里，它可以带点。
- `options` 原样透传给上游，`bb_ai_style.config` 里的同名字段可以逐个覆盖，所以调分辨率、比例不需要改代码。
- `api_key` 不要提交，按模型 key 分别写进未提交的 `conf/server.local.yaml` / `conf/server.production.local.yaml`。

**首次提交**用哪个模型，优先级是 `bb_config.ai_active_model` > `bb_ai_style.provider` > `default_model`：

```sql
-- 全局强制切到某个模型，立即对下一个提交的任务生效（只影响第一次尝试）
INSERT INTO bb_config (config_key, config_value, description)
VALUES ('ai_active_model', 'gemini-3-1-flash-image-preview', 'AI 生图首次提交使用的模型 key，留空则回退风格表和 YAML')
ON DUPLICATE KEY UPDATE config_value = VALUES(config_value);

-- 只让某个风格换模型：把上面这个键置空或删除，再改风格表
UPDATE bb_ai_style SET provider = 'gemini-3-1-flash-image-preview' WHERE style_key = 'watercolor';
```

**用户主动重试**（`RetryStyleGeneration`）走另一条链：`bb_config.ai_retry_model` > YAML `retry_model` > 首发链条。刚失败的模型再试一次多半还会失败，所以重试默认换一家。

```sql
-- 换重试模型，不用发版
INSERT INTO bb_config (config_key, config_value, description)
VALUES ('ai_retry_model', 'gemini-3-1-flash-image-preview', 'AI 生图主动重试使用的模型 key，留空则沿用首发链条')
ON DUPLICATE KEY UPDATE config_value = VALUES(config_value);
```

- **重试模型优先于 `ai_active_model`**：后者只管所有任务的第一次尝试，若它压过重试模型，重试就又回到刚失败的那个模型上了。
- 两个重试配置都为空时，重试行为与首发完全一致。
- 只有一个重试模型，所以第二次、第三次重试仍然是它。
- 重试模型名若不在 `models` 里，会打 warn 并退回首发模型继续重试——拒绝重试等于让用户彻底没路可走。
- 进程内的自动重试（同一个任务失败后立刻再试一次）不换模型。

模型名在提交时会固化到 `bb_ai_generation.provider`，所以切换配置不会把已经扣费、正在执行的任务挪到另一个模型上。提交时若模型名没有对应配置，请求会在扣积分之前被拒绝——因此改动 key 之后要确认历史的 `bb_ai_style.provider` 值仍然在 `models` 里。

## ECS Docker 部署（HTTPS 域名）

根目录的 `docker-compose.yml` 会启动 MySQL、Redis、`backend` 和
`admin-web` 四个服务。只有 Nginx 的 `80` 和 `443` 端口对外发布；`/api/`
会在 Docker 内部转发给后端的 `8080` 端口，MySQL、Redis 和 gRPC 都不暴露到公网。

以下示例使用生产服务器目录 `/root/work/pinto_server` 和域名
`appbobo.cn`。域名 A 记录必须先指向 ECS 公网 IP，且阿里云安全组需开放 TCP
`80`、`443`；不要开放 `3306`、`6379`、`8080` 或 `9090`。

### 1. 在 ECS 准备未提交的凭证配置

```bash
cd /root/work/pinto_server
cp .env.production.example .env.production
cp conf/server.production.local.yaml.example conf/server.production.local.yaml
chmod 600 .env.production conf/server.production.local.yaml
```

编辑 `.env.production`，填写两个随机的 MySQL 密码；其中
`MYSQL_PASSWORD` 必须与 `conf/server.production.local.yaml` 中的
`mysql.password` 完全一致。然后在后一个文件填写 OSS RAM 凭证、用户 JWT
密钥、管理端 JWT 密钥和管理员密码哈希。配置文件中的明文密钥不可提交 Git。

管理员密码哈希可在本地项目根目录执行下面的命令生成，再复制到生产配置中：

```bash
go run ./cmd/admin-password-hash
```

### 2. 申请并续期 Let's Encrypt 证书

证书和私钥只保存在服务器的 `certbot/` 目录中，该目录已被 Git 忽略，绝不能
提交。首次签发时，短暂停止 Nginx 以释放 80 端口：

```bash
cd /root/work/pinto_server
mkdir -p certbot/conf certbot/www
docker-compose --env-file .env.production stop admin-web

docker run --rm -p 80:80 \
  -v /root/work/pinto_server/certbot/conf:/etc/letsencrypt \
  certbot/certbot certonly \
  --standalone \
  -d appbobo.cn \
  --email your-email@example.com \
  --agree-tos \
  --no-eff-email \
  --non-interactive
```

签发成功后，继续下面的部署命令启动 Nginx。使用 root 的 crontab 配置每日检查
续期（续期时会短暂重启 Nginx）：

```cron
0 3 * * * /bin/sh -c 'cd /root/work/pinto_server || exit; docker-compose --env-file .env.production stop admin-web; docker run --rm -p 80:80 -v /root/work/pinto_server/certbot/conf:/etc/letsencrypt certbot/certbot renew --quiet; docker-compose --env-file .env.production up -d admin-web'
```

### 3. 构建并上传 Flutter 管理端

在 Flutter 客户端项目中构建，API 地址必须使用 HTTPS 域名：

```bash
cd /Users/zhaojiabo/app_project/bobobeads
flutter build web --release --target lib/admin_main.dart \
  --dart-define=BOBOBEADS_API_BASE_URL=https://appbobo.cn

scp -r build root@123.57.175.126:/root/work/pinto_server/admin-web/
```

上传完成后，ECS 上应存在 `admin-web/build/index.html`。

### 4. 启动与验证

```bash
cd /root/work/pinto_server
docker-compose --env-file .env.production up -d --build
docker-compose --env-file .env.production ps
docker-compose --env-file .env.production logs --tail=100 backend admin-web
curl -I http://appbobo.cn
curl -I https://appbobo.cn
```

HTTP 应返回 `301` 并跳转至 HTTPS，HTTPS 应返回 `200`。浏览器访问
`https://appbobo.cn` 即可打开管理端。

更新版本时，先重新构建并上传 `admin-web/build`，再在 ECS 仓库目录执行：

```bash
git pull
docker-compose --env-file .env.production up -d --build
```

## 项目结构

```
├── cmd/main.go                 启动入口（gRPC + HTTP Gateway + 后台任务）
├── conf/                       配置
│   ├── conf.go                 配置结构体
│   └── server.yaml             本地开发配置
├── pkg/proto/                  Proto 定义（接口契约）
├── internal/
│   ├── pb/                     Proto 生成的 Go 代码
│   ├── api/                    gRPC Handler（请求 → 响应）
│   ├── service/                业务逻辑
│   ├── dao/                    数据访问
│   ├── model/                  数据模型（对应数据库表）
│   ├── db/                     数据库连接初始化
│   ├── middleware/             中间件（JWT认证、平台识别、限流）
│   ├── bootstrap/              依赖注入容器
│   └── task/                   后台定时任务
├── doc/                        设计文档
├── Makefile                    构建命令
├── Dockerfile                  容器镜像
├── docker-compose.yml          ECS Docker 服务编排
└── admin-web/nginx.conf        管理端静态站点与 API 反向代理
```

## 对接文档

- [Flutter 客户端后端接口对接文档](docs/flutter-client-api-integration.md)
- [官方图纸与 AI 风格转换执行计划](docs/plans/2026-07-07-001-feat-pattern-ai-api-plan.md)

## 功能模块

| 模块 | Proto | 说明 |
|------|-------|------|
| Auth | `auth.proto` | 游客登录、手机号登录、微信/Apple登录、Token刷新 |
| User | `user.proto` | 用户信息管理、绑定手机、注销 |
| Generation | `generation.proto` | 生图流程管控（预扣费 → 生成 → 确认/取消） |
| Work | `work.proto` | 作品保存/列表/删除、草稿管理 |
| Media | `media.proto` | OSS 上传凭证、文件 URL |
| Community | `community.proto` | 社区发布、信息流、点赞/收藏/评论/关注 |
| Template | `template.proto` | 官方图纸分类、列表/详情、收藏 |
| AI Generation | `ai_generation.proto` | AI 风格列表、风格转换任务、任务记录 |
| Subscribe | `subscribe.proto` | 商品列表、订单、支付、订阅状态 |
| Credit | `credit.proto` | 积分余额、流水查询 |
| Invite | `invite.proto` | 邀请码、绑定、统计 |
| System | `system.proto` | 应用配置、版本检查、拼豆颜色库、豆板规格 |
| Report | `report.proto` | 事件上报、错误上报、用户反馈 |

## 客户端生图流程

图纸生成在客户端本地完成，服务端通过 **预扣费 + 确认** 模型管控：

```
客户端                              服务端
  │                                   │
  │  POST /generation/create          │
  ├──────────────────────────────────→│ 检查额度 → 预扣积分
  │←──────────────────────────────────┤ 返回 generation_id
  │                                   │
  │  本地生成图纸                      │
  │                                   │
  │  POST /generation/{id}/complete   │
  ├──────────────────────────────────→│ 保存作品 → 标记完成
  │←──────────────────────────────────┤ 返回 work_id
  │                                   │
  │  失败时: POST /generation/{id}/cancel
  ├──────────────────────────────────→│ 退还积分
  │                                   │
  │  超时(30min): 后台自动退还          │
```

扣费优先级：VIP 免费 → 每日免费 3 次 → 扣积分 → 不足则拒绝

## API 接口一览

### Auth
```
POST   /api/v1/auth/guest            游客登录
POST   /api/v1/auth/phone            手机号登录
POST   /api/v1/auth/sms/send         发送验证码
POST   /api/v1/auth/wechat           微信登录
POST   /api/v1/auth/apple            Apple 登录
POST   /api/v1/auth/refresh          刷新 Token
```

### User
```
GET    /api/v1/user/info             获取用户信息
PUT    /api/v1/user/info             更新用户信息
POST   /api/v1/user/bind-phone       绑定手机号
DELETE /api/v1/user/account           注销账号
```

### Generation
```
POST   /api/v1/generation/create              发起生成（预扣积分）
POST   /api/v1/generation/{id}/complete       生成完成（上传结果）
POST   /api/v1/generation/{id}/cancel         取消生成（退还积分）
GET    /api/v1/generation/{id}                查询状态
```

### Work
```
POST   /api/v1/works                 保存作品
GET    /api/v1/works                 作品列表
GET    /api/v1/works/{work_id}       作品详情
DELETE /api/v1/works/{work_id}       删除作品
POST   /api/v1/works/drafts          保存草稿
GET    /api/v1/works/drafts          草稿列表
```

### Media
```
POST   /api/v1/media/upload-token    获取上传凭证
POST   /api/v1/media/report-upload   上传完成回调
GET    /api/v1/media/url             获取文件 URL
```

### Community
```
POST   /api/v1/community/posts                      发布到社区
DELETE /api/v1/community/posts/{post_id}             取消发布
GET    /api/v1/community/feed                        信息流
GET    /api/v1/community/posts/{post_id}             帖子详情
POST   /api/v1/community/posts/{post_id}/like        点赞
DELETE /api/v1/community/posts/{post_id}/like        取消点赞
POST   /api/v1/community/posts/{post_id}/favorite    收藏
DELETE /api/v1/community/posts/{post_id}/favorite    取消收藏
POST   /api/v1/community/posts/{post_id}/comments    评论
GET    /api/v1/community/posts/{post_id}/comments    评论列表
POST   /api/v1/community/users/{user_id}/follow      关注
DELETE /api/v1/community/users/{user_id}/follow      取消关注
POST   /api/v1/community/report                      举报
```

### Template
```
GET    /api/v1/templates/categories   模板分类
GET    /api/v1/templates              模板列表
GET    /api/v1/templates/{id}         模板详情
```

### Subscribe
```
GET    /api/v1/subscribe/products                商品列表
POST   /api/v1/subscribe/orders                  创建订单
GET    /api/v1/subscribe/orders/{no}/payment     获取支付参数
POST   /api/v1/subscribe/callback                支付回调
POST   /api/v1/subscribe/restore                 恢复购买(iOS)
GET    /api/v1/subscribe/status                  订阅状态
GET    /api/v1/subscribe/orders                  订单历史
```

### Credit
```
GET    /api/v1/credits/balance         积分余额
GET    /api/v1/credits/transactions    积分流水
```

### Invite
```
GET    /api/v1/invite/code             获取邀请码
POST   /api/v1/invite/bind             绑定邀请码
GET    /api/v1/invite/stats            邀请统计
GET    /api/v1/invite/records          邀请记录
```

### System
```
GET    /api/v1/system/config           应用配置
GET    /api/v1/system/update           版本检查
GET    /api/v1/system/banners          Banner
GET    /api/v1/system/bead-colors      拼豆颜色库
GET    /api/v1/system/board-specs      豆板规格
```

### Report
```
POST   /api/v1/report/event            事件上报
POST   /api/v1/report/error            错误上报
POST   /api/v1/report/feedback         用户反馈
```

## 通用约定

- **认证**: `Authorization: Bearer <access_token>`
- **平台标识**: `X-Platform: ios | android | miniprogram | web`
- **响应格式**: `{ "header": {"code": 0, "message": "success"}, ...data }`
- **分页请求**: `{"page": 1, "pageSize": 20}`
- **分页响应**: `{"total": 100, "page": 1, "pageSize": 20, "hasMore": true}`
- **无需认证的接口**: 游客登录、手机号登录、发送验证码、微信/Apple登录、Token刷新、系统配置、版本检查、社区浏览

## 多端支持

| 端 | 协议 | 认证 | 支付 |
|----|------|------|------|
| iOS | gRPC / REST | JWT + Apple/微信/手机号 | Apple IAP |
| Android | gRPC / REST | JWT + 微信/手机号 | 微信支付 |
| 小程序 | REST | JWT + wx.login | 微信支付 JSAPI |
| Web | REST | JWT + 微信扫码/手机号 | 微信支付 Native |

微信登录通过 unionid 统一用户身份，各端 openid 分别存储。

## 数据库表

所有表前缀 `bb_`，启动时自动创建：

| 表 | 说明 |
|----|------|
| bb_user | 用户 |
| bb_work | 作品 |
| bb_generation | 生成记录（预扣费凭证） |
| bb_community_post | 社区帖子 |
| bb_like | 点赞 |
| bb_favorite | 收藏 |
| bb_comment | 评论 |
| bb_follow | 关注 |
| bb_template | 模板 |
| bb_template_category | 模板分类 |
| bb_order | 订单 |
| bb_product | 商品 |
| bb_subscription | 订阅 |
| bb_credit_transaction | 积分流水 |
| bb_invite | 邀请记录 |
| bb_bead_color | 拼豆颜色库 |
| bb_board_spec | 豆板规格 |
| bb_config | 系统配置 |
| bb_feedback | 用户反馈 |

## 开发命令

```bash
make proto          # 重新生成 Proto 代码
make build          # 编译
make run            # 运行
make fmt            # 格式化代码
make tidy           # 整理依赖
make test           # 运行测试
make docker-up      # 启动 MySQL + Redis
make docker-down    # 停止依赖服务
```
