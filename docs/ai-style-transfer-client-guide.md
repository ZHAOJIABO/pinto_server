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
                 │
                 └─ 失败(3)/过期(5) 时用户点「重新生成」：
                    POST /api/v1/ai/style-generations/{taskId}/retry
                    → 返回新 taskId，回到第 6 步轮询（不用重走 1~3 步）
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

失败或过期的任务想让用户重试,**不要走这个接口**,用 §2.6 的重试接口,可以省掉重新上传原图。

### 2.6 重试失败的任务

```
POST /api/v1/ai/style-generations/{taskId}/retry
```

`{taskId}` 是那个**失败(3)或过期(5)**的原任务 ID。服务端从原任务上取回原图和风格,不需要客户端还持有原图 —— 用户清了缓存、杀了进程、换了设备都能重试。

**Request Body**

```json
{
  "clientRequestId": "b7f1c0e2-..."
}
```

**Response**:字段与 §2.5 的创建接口完全一致(`taskId` / `status` / `creditsDeducted` / `remainingBalance` / `duplicated`)。

要点:

- 返回的 `taskId` 是**新任务**的 ID,和请求里的原任务 ID 不同。拿新 ID 去轮询,原任务保持失败状态不变。
- `clientRequestId` 规则同 §2.5:同一次重试操作的网络重发复用同一个值,服务端靠它去重;用户再点一次「重新生成」才换新值。
- 这是一次**新的扣费**。原任务失败时积分已自动退回,所以用户不会为同一张图付两次。
- 只有 status 为 3 或 5 的任务能调。对排队中/生成中/已成功的任务调用会报错且不扣费——成功的任务想「换一张」请走 §2.5 创建新任务。
- 可能的失败:原图已被清理、风格已下架、积分不足、并发配额已满,这些都在扣费前拒绝。

### 2.7 查询任务状态(轮询)

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
    "inputImageUrl": "https://img.appbobo.cn/style_input/2026/07/28/44/xxx.png",
    "inputThumbnailUrl": "https://img.appbobo.cn/style_input/2026/07/28/44/xxx-low.webp",
    "outputImageUrl": "https://img.appbobo.cn/ai_output/2026/07/28/44/xxx.png",
    "outputThumbnailUrl": "https://img.appbobo.cn/ai_output/2026/07/28/44/xxx-low.webp",
    "status": 2,
    "creditsDeducted": 1,
    "errorMessage": "",
    "createdAt": "1785209431",
    "startedAt": "1785209431",
    "completedAt": "1785209432",
    "progress": 100
  }
}
```

**status 取值**

| 值 | 含义 | 客户端表现 |
|---|---|---|
| 0 | pending,排队中 | 继续轮询。"排队中…" |
| 1 | running,正在生成 | 继续轮询。"生成中…" |
| 2 | succeeded | 停止轮询,展示 `outputImageUrl` |
| 3 | failed | 停止轮询,展示 `errorMessage`,积分**已自动退回**。重试走 §2.6 |
| 4 | cancelled | 停止轮询 |
| 5 | expired,排队超时 | 停止轮询,积分**已自动退回**。重试走 §2.6 |

终止条件就是 `status >= 2`。

**进度条:用 `progress`(0-100)**

| 字段 | 说明 |
|---|---|
| `progress` | 服务端给出的进度百分比,直接绑到进度条上 |
| `startedAt` | 开始**生成**的秒级时间戳(字符串);`"0"` 表示还在排队 |

`progress` 的取值规律:

| 情况 | progress |
|---|---|
| `status = 0` 排队中 | `5` |
| `status = 1` 刚开始生成 | `10` |
| `status = 1` 生成中 | 随耗时线性递增,到平均耗时(120s)时到 `95` |
| `status = 1` 比平均更慢 | 停在 `95` 不再动 |
| `status = 2` 成功 | `100` |
| `status = 3/4/5` 失败/取消/超时 | `0`,请改为展示 `errorMessage` |

**必须知道的两件事:**

1. **这是服务端按已耗时算的估算值,不是第三方回传的真实进度。** 第三方接口是一次阻塞调用,中途不吐进度,所以没人能给出真实百分比。`progress` 的作用是让进度条能动,比一个转圈的 spinner 体感好,不要拿它做任何业务判断。
2. **判断完成只看 `status >= 2`,永远不要用 `progress == 100`。** 未完成时 `progress` 封顶在 95 就是为了防止这种误判。

进度条动画建议在客户端做插值平滑:两次轮询间隔 3-5s,直接把 `progress` 赋给进度条会一跳一跳。用 `TweenAnimationBuilder` 之类把新旧值之间补上就行。另外**进度只增不减** —— 万一因为服务端配置调整拿到了比上次更小的值,取 `max(旧值, 新值)`,倒退的进度条比不动更让人困惑。

```dart
// startedAt 同样是字符串形态的秒级时间戳
final isQueued = task.startedAt == '0';   // 还没轮到它，进度条留在 5%
```

如果想自己算而不用服务端的 `progress`,用 `startedAt` 而**不要**用 `createdAt` —— `createdAt` 里混着排队时间,免费用户排队上限 3 个任务,排队可能好几分钟,拿它算出来的进度会严重超前。

> `startedAt` 记的是「本次尝试被调度器领取的时刻」。服务端内部重试发生在同一个执行槽内,不会重写它,所以客户端可以放心当作单调递增。

`outputImageUrl` 是公网可直接访问的图片地址(我们自己的 OSS),可以直接丢给 `Image.network` 或缓存组件,不需要再调任何接口换取。

**缩略图字段(列表页请用这两个)**

| 字段 | 用途 |
|---|---|
| `inputThumbnailUrl` | 原图缩略图,列表 / 小卡片用 |
| `outputThumbnailUrl` | 结果图缩略图,列表 / 小卡片用 |
| `inputImageUrl` / `outputImageUrl` | 全尺寸原图,详情页、大图预览、保存到相册用 |

缩略图由服务端生成后作为独立对象存下来,长边不超过 600 的 lossy webp,地址是原图去掉扩展名再加 `-low.webp`。它是一个普通静态文件,不带任何查询参数,生成后不再变化,可以放心做本地磁盘缓存。

两个字段**都可能是空字符串**,渲染前必须判空并降级到对应的全尺寸地址:

```dart
String thumbOrFull(String thumb, String full) => thumb.isEmpty ? full : thumb;

Image.network(thumbOrFull(task.outputThumbnailUrl, task.outputImageUrl));
```

为空的两种情况:任务还没出图(`status < 2` 时 `outputThumbnailUrl` 必然为空),或服务端生成缩略图失败(单张失败不影响原图,降级用全尺寸地址即可)。

`createdAt` / `completedAt` 是**字符串形态的秒级 Unix 时间戳**(protobuf int64 转 JSON 的结果),解析时先 `int.parse` 再 `×1000`。

> 已知缺口:`styleName` 目前返回空字符串,风格名请用列表接口的数据在本地对应(`styleId` → `name`)。
>
> `inputImageUrl` 是本次提交的原图公网地址。**只有此次修复之后新建的任务才有值**,历史任务仍是空字符串,渲染前请判空并回退到本地原图。

### 2.8 历史列表

```
GET /api/v1/ai/style-generations?page.page=1&page.pageSize=10
```

用于「我的 AI 作品」页。用户杀进程后想找回上次的任务,也可以用它的第一页兜底。

**查询参数必须写成嵌套形式**:`page.page` 和 `page.pageSize`(`page.page_size` 也接受)。
写成 `?page=1&page_size=10` 不会报错,但会被服务端静默忽略,拿到的是默认的第 1 页 20 条。
缺省值:`page` = 1(从 1 开始,不是 0),`pageSize` = 20,服务端当前没有上限。

**Response**

```json
{
  "header": {"code": 0, "message": "success", "traceId": "..."},
  "tasks": [
    {
      "taskId": "42e575c1-...",
      "styleId": "2",
      "styleName": "",
      "inputImageUrl": "https://img.appbobo.cn/style_input/2026/07/28/44/xxx.png",
      "inputThumbnailUrl": "https://img.appbobo.cn/style_input/2026/07/28/44/xxx-low.webp",
      "outputImageUrl": "https://img.appbobo.cn/ai_output/2026/07/28/44/xxx.png",
      "outputThumbnailUrl": "https://img.appbobo.cn/ai_output/2026/07/28/44/xxx-low.webp",
      "status": 2,
      "creditsDeducted": 1,
      "errorMessage": "",
      "createdAt": "1785209431",
      "startedAt": "1785209431",
      "completedAt": "1785209432",
      "progress": 100
    }
  ],
  "page": {"total": 37, "page": 1, "pageSize": 10, "hasMore": true}
}
```

元素结构与 §2.7 的 `task` 完全一致,同样的注意事项都适用:`createdAt` / `completedAt` 是**字符串**形态的秒级时间戳;`styleName` 目前是空字符串,风格名请用 §2.1 的列表在本地按 `styleId` 对应;`inputImageUrl` 只有新任务才有值,历史任务是空串,要判空回退。未完成的任务 `completedAt` 是 `"0"`,不是 `null`、也不会缺字段 —— 判断是否完成请用 `status >= 2`,不要用 `completedAt`。

**这个接口是缩略图收益最大的地方**:列表一屏十几张图,请一律用 `outputThumbnailUrl`(判空回退 `outputImageUrl`),不要在列表里加载全尺寸原图。用户点进详情再换成 `outputImageUrl`。

零值字段一律照常输出(不会缺 key),没有数据时 `tasks` 是 `[]`,可以放心直接遍历。

**这个接口返回该用户的全部任务,不区分状态** —— 包含 `status` 为 3(失败)、4(取消)、5(超时)的记录。「我的 AI 作品」这类只展示成品的页面,需要客户端自己过滤 `status == 2 && outputImageUrl.isNotEmpty`。注意过滤会让实际渲染条数少于 `pageSize`,分页 UI 不要依赖「本页返回条数 == pageSize」来判断有没有下一页,请用 `page.hasMore`。

翻页判断只看 `page.hasMore`。它由服务端按 `page * pageSize < total` 算出。

```dart
Future<List<AiTask>> fetchHistory({int page = 1, int pageSize = 20}) async {
  final resp = await dio.get(
    '/api/v1/ai/style-generations',
    queryParameters: {'page.page': page, 'page.pageSize': pageSize},
  );
  final body = resp.data as Map<String, dynamic>;
  if (body['header']['code'] != 0) {
    throw ApiException(body['header']['code'], body['header']['message']);
  }
  return (body['tasks'] as List)
      .map((e) => AiTask.fromJson(e as Map<String, dynamic>))
      .toList();
}

// createdAt 是字符串,别直接当 int 用
DateTime _ts(String v) =>
    DateTime.fromMillisecondsSinceEpoch(int.parse(v) * 1000);
```

> 分页用的是 offset,排序是 `createdAt` 倒序。用户在翻页过程中又提交了新任务,后面几页可能出现一条重复。下拉刷新时请回到第 1 页并清空已有列表,不要做增量合并。

---

## 3. 轮询策略

真实生成耗时平均 **120 秒**;服务端单次生成的硬上限是 180s,超过就判失败并退积分。高峰排队时到终态可能要更久。

所以**不要在开头密集轮询** —— 前 60 秒几乎不可能出图,轮 60 次和轮 12 次拿到的都是 `status=0`。把密集的那段放在最可能完成的窗口上:

```
0~10s     :每 2s   （本地 fake_provider 约 1s 就完成，保证联调能立刻看到）
10~90s    :每 5s   （这段基本不会出图，粗轮省电省流量）
90~210s   :每 3s   （平均 120s 落在这里，密一点让出图更及时）
210s 之后 :每 5s   （已经在排队或重试了，慢轮）
总超时:6 分钟 → 停止轮询，提示"仍在生成中，可稍后在历史记录里查看"
```

总超时定 6 分钟是为了盖住「单次 180s + 一轮排队」。免费用户排队上限是 3 个任务,最坏情况到终态要 9 分钟以上,那种情况让用户回历史列表看,不要一直挂着页面。

等待页的进度条用响应里的 `progress` 字段(见 §2.7),不用自己按耗时估。轮询间隔 3-5s 一跳,记得做插值平滑。

要点:

- **不要用 500ms 以内的间隔**。每个用户每接口有 30 次/秒的限流,更重要的是没意义 —— 服务端状态本来就是秒级变化的。
- **`status == 1` 不要重置节奏**,继续按原退避走即可。`status` 从 0 变 1 只说明开始执行,不代表快好了。
- 客户端超时后**不要**认为任务失败。任务在服务端照常执行,用户回到历史列表还能看到结果。只有服务端返回 `status >= 2` 才是终态。
- 页面退出 / App 切后台时**停止轮询**,回到前台用同一个 `taskId` 恢复。切后台超过几分钟回来时,先立即请求一次再进入退避,不要按切走时的 `attempt` 继续。

```dart
Duration _pollDelay(Duration elapsed) {
  final s = elapsed.inSeconds;
  if (s < 10) return const Duration(seconds: 2);
  if (s < 90) return const Duration(seconds: 5);
  if (s < 210) return const Duration(seconds: 3);
  return const Duration(seconds: 5);
}

Future<AiTask> pollUntilDone(String taskId) async {
  final started = DateTime.now();
  const timeout = Duration(minutes: 6);
  while (true) {
    final task = await api.getStyleGeneration(taskId);
    if (task.status >= 2) return task;
    final elapsed = DateTime.now().difference(started);
    if (elapsed >= timeout) throw TaskStillRunningException(taskId);
    await Future.delayed(_pollDelay(elapsed));
  }
}
```

节奏按**已耗时**算,不按第几次请求算 —— 这样中途切后台再回来也不会错位。

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

失败页的「重新生成」按钮请调 §2.6 的重试接口,传失败任务的 `taskId` 即可,不用管本地还有没有原图。只有 `原图读取失败` 这一种是服务端存储问题,重试大概率还是失败,可以直接引导用户重新选图走 §2.2 起的完整流程。

---

## 5. 状态持久化与恢复

生成要两分钟左右,用户很可能中途切后台、杀进程或者返回上一页。这条路径必须做,否则用户等不到结果还白扣积分。建议:

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
| 失败后重试要重新上传原图 | 失败/过期任务可直接调 §2.6 的重试接口,复用服务端已有的原图 |

---

## 7. 联调环境

本地服务:`http://127.0.0.1:8080`,配置里 `fake_provider: true` 时会返回一张占位图,**约 1 秒即完成**,方便快速验证状态流转;但它同样走真实的 OSS 上传和真实的积分扣减,所以测试账号需要有余额。

联调时如果一直停在 `status=0`,先确认服务端启动日志里有 `ai dispatcher started`;如果任务变成 `status=3` 且提示「原图读取失败」,是服务端 OSS 权限问题,不是客户端的问题。
