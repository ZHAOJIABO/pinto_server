# 用户投稿图纸 · 客户端对接文档

面向场景：用户在「我的作品」里挑一个自己做好的作品，投稿到官方图纸库 → 运营审核 → 通过后该作品出现在官方图纸库，并署名投稿人昵称。

**最重要的两点**：

1. **投稿不等于发布。** 提交成功只是进了审核队列（`status = 0`），是否上架由运营决定。客户端不要在提交成功后提示「已发布」。
2. **投稿是快照。** 提交那一刻服务端把作品的图纸数据、尺寸、用豆/用色统计整份复制到投稿记录里。之后用户改图、甚至删掉这个作品，都不会改变审核内容和最终发布出来的图纸。所以 UI 上不要写「修改作品会同步更新投稿」。

本文只覆盖用户侧两个新接口。运营审核用的管理端接口在后台仓库，客户端不需要关心。

---

## 1. 全流程

```
[我的作品列表]
      │
      │  用户点某个作品的「投稿到官方图纸库」
      ▼
 [投稿表单页]  填标题(必填) + 描述(可选)
      │
      ├─ POST /api/v1/template-submissions   → submissionId, status=0
      │        body: {workId, title, description, clientRequestId}
      │
      ▼
 [我的投稿列表页]  GET /api/v1/template-submissions
      │
      └─ status: 0 待审核 → 1 已通过(带 templateId) / 2 已驳回(带 reviewReason)
```

投稿**没有**独立的上传流程 —— 只传一个 `workId`，图纸数据服务端自己从作品里取。用户必须先把作品保存成功（`POST /api/v1/works`）拿到 `workId`，草稿也可以投，只要它有 `patternData`。

**没有轮询需求。** 审核是人工的，通常要几小时到一天。不要写定时轮询，用户下拉刷新「我的投稿」列表即可。

两个接口都需要登录态：`Authorization: Bearer <accessToken>`，缺失或过期会直接返回 **HTTP 401**（走的是网关默认错误体，不带我们的 `header`），按现有的刷新 token 流程处理即可。除此之外的业务错误统一在响应体的 `header.code` 里，**HTTP 状态码是 200**，不要只看 HTTP 状态码。

---

## 2. 接口详情

### 2.1 提交投稿

```
POST /api/v1/template-submissions
```

**Request**

```json
{
  "workId": "123",
  "title": "小猫",
  "description": "两色拼豆，适合新手",
  "clientRequestId": "6f1e...-uuid-v4"
}
```

| 字段 | 必填 | 说明 |
|---|:---:|---|
| `workId` | ✔ | 作品 ID，**字符串形态的数字**。必须属于当前用户 |
| `title` | ✔ | 图纸标题（代号）。前后空格服务端会 trim，trim 后不能为空，最长 **40 个字符**（按字符数算，中文也是 1 个） |
| `description` | | 描述，最长 **200 个字符**，可传空串或不传 |
| `clientRequestId` | ✔ | 客户端生成的幂等键，建议 UUID v4。见 §4 |

**Response**

```json
{
  "header": {"code": 0, "message": "success", "traceId": "..."},
  "item": {
    "submissionId": "9",
    "workId": "123",
    "title": "小猫",
    "description": "两色拼豆，适合新手",
    "status": 0,
    "reviewReason": "",
    "templateId": "",
    "boardSpec": "29x29",
    "width": 29,
    "height": 29,
    "beadCount": 420,
    "colorCount": 8,
    "previewUrl": "https://cdn.../work/2026/08/11/7/pattern.png",
    "thumbnailUrl": "https://cdn.../work/2026/08/11/7/pattern_thumb.webp",
    "createdAt": 1767225600,
    "reviewedAt": 0
  }
}
```

提交成功后建议直接跳「我的投稿」列表或弹一个「已提交，等待审核」的轻提示，不要停在表单页。

### 2.2 我的投稿列表

```
GET /api/v1/template-submissions?limit=20&cursor=
```

| 参数 | 说明 |
|---|---|
| `limit` | 每页数量，缺省 **12**，上限 **50**（传更大值服务端按 50 处理，不报错） |
| `cursor` | 下一页游标，首页**不传或传空串**。原样回传上一页响应里的 `nextCursor`，不要自己拼 |

**Response**

```json
{
  "header": {"code": 0, "message": "success", "traceId": "..."},
  "items": [
    {
      "submissionId": "9",
      "workId": "123",
      "title": "小猫",
      "description": "两色拼豆，适合新手",
      "status": 2,
      "reviewReason": "分辨率过低，请用更清晰的原图",
      "templateId": "",
      "boardSpec": "29x29",
      "width": 29, "height": 29, "beadCount": 420, "colorCount": 8,
      "previewUrl": "https://cdn.../pattern.png",
      "thumbnailUrl": "https://cdn.../pattern_thumb.webp",
      "createdAt": 1767225600,
      "reviewedAt": 1767312000
    }
  ],
  "nextCursor": ""
}
```

- 按投稿时间**倒序**（最新在前）。
- `nextCursor` 为空字符串 = 没有下一页，到底了。**不要用 `items.length < limit` 判断结尾**，请以 `nextCursor` 为准。
- 只返回当前用户自己的投稿，不存在越权风险。
- 传了非法 `cursor`（不是上一页返回的值）会返回 `code = 1101`，客户端应重置为首页重新拉。

---

## 3. `TemplateSubmissionItem` 字段说明

| 字段 | 类型 | 说明 |
|---|---|---|
| `submissionId` | string | 投稿 ID（字符串形态的数字），列表 key 用它 |
| `workId` | string | 来源作品 ID，仅用于溯源。**作品可能已被用户删除，跳转前要容错** |
| `title` / `description` | string | 投稿时填的内容。运营通过时可能改标题，但**不会回写这里**，这两个字段永远是用户当初提交的原文 |
| `status` | int | `0` 待审核 / `1` 已通过 / `2` 已驳回。见 §3.1 |
| `reviewReason` | string | 驳回原因，仅 `status = 2` 时有值，可直接展示给用户 |
| `templateId` | string | 通过后生成的官方图纸 ID。未通过时是**空字符串**（不是 `"0"`） |
| `boardSpec` | string | 拼豆板规格，如 `29x29` |
| `width` / `height` | int | 图纸宽高（格数） |
| `beadCount` / `colorCount` | int | 用豆总数 / 用色数量，都是服务端投稿时重算的，可直接显示 |
| `previewUrl` | string | 预览图。**可能为空**，见 §3.2 |
| `thumbnailUrl` | string | 缩略图（WebP，最长边 600px）。优先复用作品自带的缩略图，没有则由服务端从图纸图生成。**可能为空**，为空时降级用 `previewUrl` |
| `createdAt` | int64 | 投稿时间，**秒级** Unix 时间戳 |
| `reviewedAt` | int64 | 审核时间，秒级；未审核时为 `0`（不是 null） |

所有 ID 都是**字符串**形态，不要 parse 成 int 存（可能超过 JS safe integer，Dart 侧也统一按 String 处理更省事）。

### 3.1 `status` 与 UI

| status | 文案建议 | 可做的操作 |
|:---:|---|---|
| `0` 待审核 | 「审核中」灰色/橙色标 | 无。不提供撤回（服务端没有撤回接口） |
| `1` 已通过 | 「已通过」绿色标 | 用 `templateId` 跳官方图纸详情 `GET /api/v1/templates/{templateId}` |
| `2` 已驳回 | 「未通过」红色标 + 展示 `reviewReason` | 引导用户改图后**重新投稿**（见 §5） |

`status` 用的是裸 int，不要在客户端硬编码穷举 switch 后 crash —— 遇到未知值按「审核中」降级处理。

### 3.2 `previewUrl` 为什么可能是空的

作品的图纸预览图地址是客户端保存作品时上报的，服务端不信任任意外部 URL。投稿时只有当它确实指向我们自己的对象存储时才会被快照成候选预览图，否则留空，等运营在后台上传一张。`thumbnailUrl` 同理：优先复用作品自带的缩略图，没有就从图纸图现生成一张，两步都失败则为空。

对客户端的影响：

- **展示时要兜底**：`thumbnailUrl` → `previewUrl` → 本地占位图（可以按 `boardSpec`/`width`x`height` 画个网格占位）。
- **要让这两个字段有值**，保存作品时 `patternImageUrl` 必须是走 `POST /api/v1/media/upload-token`（`purpose = pattern`）+ 直传 + `report-upload` 拿到的我们自己的地址，不要填第三方图床或本地路径。这是投稿能带图的唯一前提。
- 走「图纸生成完成」（`CompleteGeneration`）保存的作品自带缩略图，投稿会直接复用，图和作品列表里看到的一致。手动 `POST /api/v1/works` 保存的作品没有，服务端会在投稿时现生成一张。

---

## 4. 幂等：`clientRequestId` 怎么用

服务端按 `(userId, clientRequestId)` 唯一去重：

- **同一个值重放** → 返回**同一条**投稿，`header.code = 0`，不会重复创建，也**不占用**每日配额。
- 所以提交请求超时 / 断网 / 不确定服务端有没有收到时，**用同一个 `clientRequestId` 原样重发**即可，这是安全的。

客户端约定：

1. 进入投稿表单页时生成一个 UUID 并**存住**（页面 state 即可，不需要落盘）。
2. 该页面所有重试都复用这个值。
3. 收到 `code = 0` 后清掉；用户返回后重新进表单页时生成新的 UUID。

```dart
class SubmitTemplatePage extends StatefulWidget { ... }

class _SubmitTemplatePageState extends State<SubmitTemplatePage> {
  // 整个页面生命周期内固定，重试时复用
  final String _clientRequestId = const Uuid().v4();

  Future<void> _submit() async {
    final resp = await api.createTemplateSubmission(
      workId: widget.workId,
      title: _titleCtrl.text,
      description: _descCtrl.text,
      clientRequestId: _clientRequestId,
    );
    // 5000 / 网络异常都可以直接重试本方法，不会产生第二条投稿
  }
}
```

**不要**每次点提交都换一个新 UUID —— 那样弱网下用户连点会创建多条投稿并白耗配额（虽然同作品的第二条会被 2004 拦住，但配额检查在前，体验更差）。

---

## 5. 限制与规则

| 规则 | 值 | 触发时的表现 |
|---|---|---|
| 每日投稿次数 | 默认 **5 条/人/天**（服务端可配，按服务器本地时间的自然日计） | `code = 1103` |
| 同一作品同时只能有一条**未驳回**的投稿 | 待审核 / 已通过都算占用 | `code = 2004` |
| 被驳回后 | 同一作品**可以再投**（会是一条新的投稿记录） | 正常返回 |
| 已通过后 | 同一作品**不能再投**，会一直返回 2004 | `code = 2004` |

所以「重新投稿」按钮只在 `status = 2` 的卡片上出现。已通过的作品应该展示成「已收录」并链到官方图纸。

驳回后重投会新增一行，**旧的驳回记录仍留在列表里**，列表里会同时看到同一个 `workId` 的多条投稿。这是预期行为，客户端不要按 `workId` 去重。

### 5.1 待审核期间作品被锁定

投稿提交后，只要它还是 `status = 0`，这个作品就**不能修改**：`PUT /api/v1/works/{workId}` 和 `POST /api/v1/works/drafts` 都会返回 `code = 2006`，作品记录一个字段都不会被改。

投稿是快照，改作品其实动不了审核内容；锁定是为了不让用户误以为「我改了，运营审的就是新版本」。审核一旦有结果（通过或驳回）就解锁。

| 投稿状态 | 作品能否修改 |
|---|:---:|
| `0` 待审核 | ✗ 返回 `2006` |
| `1` 已通过 | ✓ |
| `2` 已驳回 | ✓ |

客户端建议：在「我的作品」列表里，对 `status == 0` 投稿对应的 `workId` 显示「审核中」角标并置灰编辑入口，别等用户改完点保存才弹错。**目前没有撤回投稿的接口**，待审核期间没有别的解锁办法，文案上要写清「审核完成后即可修改」。

已通过后作品是可以随意修改的——发布出去的官方图纸是独立副本，用户改自己的作品甚至删掉它，都不影响已上架的图纸。

---

## 6. 错误处理（`header.code`）

| code | 含义 | 客户端处理 |
|---|---|---|
| 0 | 成功 | — |
| 1101 | 参数非法：`workId` 非数字或为 0、`clientRequestId` 为空、标题为空或超 40 字、描述超 200 字、作品没有图纸数据、`cursor` 非法 | 大部分是表单校验能提前拦住的，客户端应在本地先校验一遍字数。`message` 是英文，**不要直接展示**，按字段出中文提示 |
| 1102 | 作品不存在，**或**不属于当前用户 | 提示「作品已删除」并刷新作品列表。服务端刻意不区分这两种情况（防止探测别人的作品 ID），客户端也不用区分 |
| 1103 | 超过每日投稿上限，`message` 形如 `daily submission limit is 5` | 提示「今天的投稿次数已用完，明天再来吧」。**投稿未创建** |
| 2004 | 该作品已有一条待审核/已通过的投稿 | 提示「这个作品已经在审核中了」，并引导去「我的投稿」查看 |
| 5000 | 服务端内部错误 | 可重试，**复用同一个 `clientRequestId`** |

未登录 / token 过期不走 `header.code`，而是直接 **HTTP 401**，走通用的刷新 token 拦截器；刷新后重试时**复用同一个 `clientRequestId`**。

本地建议的前置校验（能显著减少 1101）：

```dart
String? validate() {
  final title = _titleCtrl.text.trim();
  if (title.isEmpty) return '请填写图纸标题';
  if (title.characters.length > 40) return '标题最多 40 个字';
  if (_descCtrl.text.trim().characters.length > 200) return '描述最多 200 个字';
  return null;
}
```

用 `characters.length`（`characters` 包）而不是 `String.length` —— 服务端按 Unicode 字符数算，emoji 和部分中文在 `String.length` 下会偏大，两边口径要一致。

---

## 7. Dart 模型参考

```dart
class TemplateSubmission {
  final String submissionId;
  final String workId;
  final String title;
  final String description;
  final int status;            // 0 待审核 / 1 已通过 / 2 已驳回
  final String reviewReason;   // status == 2 时有值
  final String templateId;     // status == 1 时有值，否则 ''
  final String boardSpec;
  final int width;
  final int height;
  final int beadCount;
  final int colorCount;
  final String previewUrl;     // 可能为 ''
  final String thumbnailUrl;   // 可能为 ''
  final int createdAt;         // 秒级时间戳
  final int reviewedAt;        // 未审核时为 0

  bool get isPending  => status == 0;
  bool get isApproved => status == 1 && templateId.isNotEmpty;
  bool get isRejected => status == 2;

  /// 展示图：缩略图优先，都没有时由 UI 画网格占位
  String? get displayImageUrl {
    if (thumbnailUrl.isNotEmpty) return thumbnailUrl;
    if (previewUrl.isNotEmpty) return previewUrl;
    return null;
  }

  factory TemplateSubmission.fromJson(Map<String, dynamic> json) =>
      TemplateSubmission(
        submissionId: json['submissionId'] as String? ?? '',
        workId: json['workId'] as String? ?? '',
        title: json['title'] as String? ?? '',
        description: json['description'] as String? ?? '',
        status: json['status'] as int? ?? 0,
        reviewReason: json['reviewReason'] as String? ?? '',
        templateId: json['templateId'] as String? ?? '',
        boardSpec: json['boardSpec'] as String? ?? '',
        width: json['width'] as int? ?? 0,
        height: json['height'] as int? ?? 0,
        beadCount: json['beadCount'] as int? ?? 0,
        colorCount: json['colorCount'] as int? ?? 0,
        previewUrl: json['previewUrl'] as String? ?? '',
        thumbnailUrl: json['thumbnailUrl'] as String? ?? '',
        createdAt: json['createdAt'] as int? ?? 0,
        reviewedAt: json['reviewedAt'] as int? ?? 0,
      );
}
```

所有字段都做了缺省兜底：JSON 序列化会省略零值字段（`status: 0`、`reviewedAt: 0`、空字符串都可能整个 key 不出现），直接 `as int` 会崩。

---

## 8. 通过后的署名展示

投稿通过后生成的官方图纸，`TemplateItem` 会带一个新字段：

```json
{ "templateId": "88", "title": "小猫", "contributorNickname": "小明" }
```

- `contributorNickname` 为空字符串 = 官方自建图纸，UI 不显示署名行。
- 有值时显示成「投稿人：小明」之类。
- 它是**发布那一刻的昵称快照**，用户后来改昵称不会改变已发布图纸上的署名，这是刻意设计，不是 bug。
- 图纸列表和详情两个接口都会返回它，不需要额外请求。

不暴露投稿人的 userId，客户端做不了「点昵称进主页」，暂时也不需要做。

---

## 9. 联调要点

本地服务：`http://127.0.0.1:8080`。

最小验证顺序：

1. `POST /api/v1/auth/guest` 拿 token。
2. 走 `upload-token` → 直传 → `report-upload` 拿到 `patternImageUrl`，再 `POST /api/v1/works` 保存作品拿 `workId`。**跳过上传直接填假 URL 也能投稿成功，但 `previewUrl` 会是空的**，测图片兜底逻辑时可以刻意这么做。
3. `POST /api/v1/template-submissions` → `status = 0`。
4. 同 `clientRequestId` 再发一次 → 返回**同一个** `submissionId`。
5. 换新 `clientRequestId` 再投**同一个作品** → `code = 2004`。
6. `GET /api/v1/template-submissions` → 能看到该条，`status = 0`。
7. 连投 5 条后第 6 条 → `code = 1103`。

审核动作要运营在后台做（本地联调需要配好管理端账号）。客户端只需要能正确渲染 0/1/2 三种状态；本地想快速看到通过/驳回态，最省事的办法是让服务端同学直接改一下 `bb_template_submission` 表的 `status`、`review_reason`、`template_id` 三个字段。
