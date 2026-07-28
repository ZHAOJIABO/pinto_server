# AI 风格转换 · Flutter 客户端对接文档

面向场景:用户已在 App 内选好/拍好一张图片,进入「风格转换」页面 → 选择一个后台配置好的风格 → 提交 → 等待出图。

**最重要的一点**:提交接口是**异步**的。`POST /api/v1/ai/style-generations` 会在几十毫秒内返回,此时图片还没生成,`status = 0`。真正出图要靠轮询详情接口。不要把创建成功当作生成成功。

---

## 1. 全流程

```
[已选好本地图片]
      │
      ├─ 1. POST /api/v1/media/upload-token   (purpose = style_input)  → uploadUrl, fileKey
      ├─ 2. PUT  {uploadUrl}                  (图片字节直传 OSS，不经过我们服务器)
      ├─ 3. POST /api/v1/media/report-upload  (上报 fileKey)
      │
      ├─ 4. GET  /api/v1/ai/styles            (风格列表，进页面时就可以先拉)
      │
      ├─ 5. POST /api/v1/ai/style-generations (提交，立即返回 taskId + status=0)
      │
      └─ 6. GET  /api/v1/ai/style-generations/{taskId}   ← 轮询，直到 status >= 2
```

1~3 步是通用的媒体上传流程,和图纸生成用的是同一套,唯一区别是 `purpose` 必须传 `style_input`。

所有接口(除登录)都要带 `Authorization: Bearer <accessToken>`。业务错误统一在响应体的 `header.code` 里,HTTP 状态码仍是 200,**不要只看 HTTP 状态码**。

---

## 2. 接口详情

### 2.1 获取风格列表

```
GET /api/v1/ai/styles
```

**Response**

```json
{
  "header": {"code": 0, "message": "success", "traceId": "..."},
  "styles": [
    {
      "styleId": "2",
      "styleKey": "pixel",
      "name": "像素风格",
      "description": "将图片转为像素艺术风格",
      "coverUrl": "https://.../pixel-cover.png",
      "exampleUrl": "https://.../pixel-example.png",
      "costCredits": 1
    }
  ]
}
```

| 字段 | 说明 |
|---|---|
| `styleId` | 提交任务时用这个,注意是**字符串**形态的数字 |
| `styleKey` | 稳定标识,做本地埋点/兜底文案用,不要用来提交 |
| `coverUrl` / `exampleUrl` | 风格卡片封面 / 效果示例图 |
| `costCredits` | 该风格消耗的积分,页面上要显示,便于用户预期 |

列表只返回后台已上架的风格,顺序即展示顺序。风格可能随时新增或下架,**不要在客户端硬编码风格**。

### 2.2 获取上传凭证

```
POST /api/v1/media/upload-token
```

```json
{
  "file_name": "photo.png",
  "content_type": "image/png",
  "purpose": "style_input"
}
```

**Response**

```json
{
  "header": {"code": 0, "message": "success"},
  "uploadUrl": "https://pinto-test.oss-cn-beijing.aliyuncs.com/style_input/2026/07/28/44/xxx.png?...",
  "fileKey": "style_input/2026/07/28/44/xxx.png",
  "headers": {"Content-Type": "image/png"},
  "uploadMethod": "PUT",
  "maxFileSize": "20971520"
}
```

`style_input` 的限制:**最大 20MB**,允许 `image/jpeg` / `image/png` / `image/webp` / `image/heic`。

> iOS 相册里的 HEIC 可以直接传,但第三方生图对 JPEG/PNG 兼容性最好,建议客户端统一转成 JPEG 再上传,顺便压到 2MB 以内 —— 上传更快,而且服务端读原图的耗时也更短。

### 2.3 直传 OSS

用返回的 `uploadMethod`(PUT)、`uploadUrl`、`headers` 原样发起请求,body 是图片字节。

**不要加 `Authorization` header** —— 签名已在 URL 里,多带一个头会导致 OSS 签名校验失败。

`uploadUrl` 有有效期(`expiresAt`),用户在选图页停留很久后再提交的话,建议重新取一次凭证。

### 2.4 上报上传完成

```
POST /api/v1/media/report-upload
```

```json
{"file_key": "style_input/2026/07/28/44/xxx.png", "file_size": 102400}
```

**这一步不能省**。服务端要靠它把文件标记为「已上传」,漏掉的话第 2.5 步会直接返回 `1003 input file not found or not owned by user`。

### 2.5 提交风格转换任务

```
POST /api/v1/ai/style-generations
```

```json
{
  "style_id": "2",
  "input_file_key": "style_input/2026/07/28/44/xxx.png",
  "client_request_id": "b1f0c9de-4a2e-4c31-9f77-1f8a2d3c5e61"
}
```

**Response**

```json
{
  "header": {"code": 0, "message": "success"},
  "taskId": "42e575c1-c4af-410d-aa27-b8f01c6cf5dc",
  "status": 0,
  "creditsDeducted": 1,
  "remainingBalance": 99,
  "duplicated": false
}
```

| 字段 | 说明 |
|---|---|
| `taskId` | 轮询用,建议本地持久化(见 §5) |
| `status` | 恒为 `0`(pending),**不代表已完成** |
| `creditsDeducted` | 本次实际扣除的积分 |
| `remainingBalance` | 扣除后余额,可直接用来刷新页面上的积分显示 |
| `duplicated` | `true` 表示这是同一个 `client_request_id` 的重复提交,没有二次扣费 |

**`client_request_id` 的用法(重要)**:进入等待状态前生成一个 UUID v4,**在同一次提交的所有重试中复用它**。网络超时、切后台重发都用同一个 id,服务端会返回原任务的 `taskId` 和 `duplicated: true`,不会重复扣积分。用户点了「重新生成」才换新 id —— 那是一个新任务,会正常扣第二次费。

### 2.6 查询任务状态(轮询)

```
GET /api/v1/ai/style-generations/{taskId}
```

**Response**

```json
{
  "header": {"code": 0, "message": "success"},
  "task": {
    "taskId": "42e575c1-...",
    "styleId": "2",
    "styleName": "",
    "inputImageUrl": "",
    "outputImageUrl": "https://pinto-test.oss-cn-beijing.aliyuncs.com/ai_output/2026/07/28/44/xxx.png",
    "status": 2,
    "creditsDeducted": 1,
    "errorMessage": "",
    "createdAt": "1785209431",
    "completedAt": "1785209432"
  }
}
```

**status 取值**

| 值 | 含义 | 客户端表现 |
|---|---|---|
| 0 | pending,排队中 | 继续轮询。"排队中…" |
| 1 | running,正在生成 | 继续轮询。"生成中…" |
| 2 | succeeded | 停止轮询,展示 `outputImageUrl` |
| 3 | failed | 停止轮询,展示 `errorMessage`,积分**已自动退回** |
| 4 | cancelled | 停止轮询 |
| 5 | expired,排队超时 | 停止轮询,提示重试,积分**已自动退回** |

终止条件就是 `status >= 2`。

`outputImageUrl` 是公网可直接访问的图片地址(我们自己的 OSS),可以直接丢给 `Image.network` 或缓存组件,不需要再调任何接口换取。

`createdAt` / `completedAt` 是**字符串形态的秒级 Unix 时间戳**(protobuf int64 转 JSON 的结果),解析时先 `int.parse` 再 `×1000`。

> 已知缺口:`styleName` 和 `inputImageUrl` 目前返回空字符串。风格名请用列表接口的数据在本地对应(`styleId` → `name`),原图请用本地文件,不要依赖这两个字段。

### 2.7 历史列表(可选)

```
GET /api/v1/ai/style-generations?page.page=1&page.page_size=10
```

返回 `tasks` 数组,元素结构与 §2.6 的 `task` 相同。用于「我的 AI 作品」页。用户杀进程后想找回上次的任务,也可以用它的第一页兜底。

---

## 3. 轮询策略

真实生成耗时约 **30 秒**;高峰排队时可能到几分钟。

推荐节奏:

```
第 1~3 次:每 1s
第 4 次起:每 2s
超过 30s 后:每 3s
总超时:5 分钟 → 停止轮询，提示"仍在生成中，可稍后在历史记录里查看"
```

要点:

- **不要用 500ms 以内的间隔**。每个用户每接口有 30 次/秒的限流,更重要的是没意义 —— 服务端状态本来就是秒级变化的。
- **`status == 1` 不要重置节奏**,继续按原退避走即可。
- 客户端超时后**不要**认为任务失败。任务在服务端照常执行,用户回到历史列表还能看到结果。只有服务端返回 `status >= 2` 才是终态。
- 页面退出 / App 切后台时**停止轮询**,回到前台用同一个 `taskId` 恢复。

```dart
Future<AiTask> pollUntilDone(String taskId) async {
  final started = DateTime.now();
  var attempt = 0;
  while (DateTime.now().difference(started) < const Duration(minutes: 5)) {
    final task = await api.getStyleGeneration(taskId);
    if (task.status >= 2) return task;
    attempt++;
    final delay = attempt <= 3
        ? const Duration(seconds: 1)
        : (attempt <= 15 ? const Duration(seconds: 2) : const Duration(seconds: 3));
    await Future.delayed(delay);
  }
  throw TaskStillRunningException(taskId);
}
```

---

## 4. 错误处理

### 4.1 提交阶段(`header.code`)

| code | 含义 | 客户端处理 |
|---|---|---|
| 0 | 成功 | 进入等待页 |
| 2001 | 积分不足,`message` 形如 `insufficient credits: have 0, need 1` | 弹充值/看广告引导。**积分未扣,任务未创建,不要轮询** |
| 2005 | 排队已满,`message` 形如 `task quota exceeded: 3 running or queued, limit 3` | 提示"你还有任务在生成中,完成后再试"。**积分未扣,任务未创建,不要轮询** |
| 1003 | 输入图不属于当前用户 / 未上报上传 | 检查 §2.4 是否漏了,或重走一遍上传 |
| 1101 | 参数缺失(`style_id` / `input_file_key` / `client_request_id`) | 客户端 bug |
| 1102 | 风格不存在(可能刚被下架) | 重新拉一次风格列表 |
| 1001 / 1002 | 未登录 / token 过期 | 走刷新 token 流程后重试(复用同一个 `client_request_id`) |
| 5000 | 服务端内部错误 | 可重试,**复用同一个 `client_request_id`** 以免重复扣费 |

**并发配额说明**(对应 2005):免费用户同时最多 **3 个**任务在「排队中 + 生成中」,其中同时只有 **1 个**在真正生成;会员是同时 **10 个**、并发 **2 个**。超出的提交直接被拒,不会排队,也不扣积分。

### 4.2 生成阶段(`status == 3` 或 `5`)

失败时 `errorMessage` 是可以直接展示给用户的中文文案,例如:

- `生成失败，请稍后重试`
- `生成超时，请稍后重试`
- `原图读取失败`
- `任务排队超时`
- `生成中断，请重新发起`

**所有失败和超时,服务端都会自动把积分退回**,客户端不需要做任何补偿动作,但应在失败后刷新一次余额显示。

---

## 5. 状态持久化与恢复

生成要 30 秒以上,用户很可能中途切后台、杀进程或者返回上一页。建议:

1. 提交成功后立刻把 `{taskId, styleId, clientRequestId, localImagePath, submittedAt}` 写入本地存储(Hive / shared_preferences 都行)。
2. 进入等待页(或 App 冷启动)时,若本地存在未完成记录,直接用 `taskId` 恢复轮询,**不要重新提交**。
3. 收到 `status >= 2` 后清除本地记录。
4. 若提交请求本身超时、不确定服务端是否收到:用**同一个** `client_request_id` 重发即可。返回 `duplicated: true` 说明上次其实成功了,直接拿返回的 `taskId` 去轮询。

这套做法配合服务端的幂等键,能保证「无论客户端怎么重试,同一次用户操作只会扣一次积分」。

---

## 6. 与旧行为的差异(升级注意)

如果客户端已经按早期文档实现过:

| 旧行为 | 现在 |
|---|---|
| 创建接口同步返回,`status` 直接是 2,`outputImageUrl` 当场就有 | 创建只返回 `taskId` + `status=0`,**必须轮询** |
| 创建请求耗时 = 生成耗时 | 创建 ~50ms 返回;生成在服务端后台进行 |
| 输出图是第三方地址 | 输出图在我们自己的 OSS(`ai_output/` 前缀),长期可访问 |
| 无并发限制 | 单用户排队上限(免费 3 / 会员 10),超出返回 2005 |

---

## 7. 联调环境

本地服务:`http://127.0.0.1:8080`,配置里 `fake_provider: true` 时会返回一张占位图,**约 1 秒即完成**,方便快速验证状态流转;但它同样走真实的 OSS 上传和真实的积分扣减,所以测试账号需要有余额。

联调时如果一直停在 `status=0`,先确认服务端启动日志里有 `ai dispatcher started`;如果任务变成 `status=3` 且提示「原图读取失败」,是服务端 OSS 权限问题,不是客户端的问题。
