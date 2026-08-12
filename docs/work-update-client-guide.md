# 修改已保存的作品 · 客户端对接文档

面向场景：用户打开「我的作品」里已经存好的一个作品，在编辑器里改图纸（改色、改像素、改尺寸）或者只是重命名，然后保存回同一条记录。

**最重要的三点**：

1. **原地修改，`workId` 不变。** 不要再走 `POST /api/v1/works` 重新保存一份——那样会产生一条新作品，用户会看到两个几乎一样的条目。
2. **不要用草稿接口去改已完成的作品。** `POST /api/v1/works/drafts` 会把作品状态改回草稿，并清掉缩略图、来源信息和图纸统计。它只用于草稿本身。
3. **只传要改的字段。** 字段留空 = 不修改。特别是**只改标题时不要带 `patternData`**，服务端会保留原有图纸。

---

## 1. 接口

```
PUT /api/v1/works/{workId}
Authorization: Bearer <accessToken>
Content-Type: application/json
```

需要登录态。token 缺失或过期会直接返回 **HTTP 401**（网关默认错误体，不带我们的 `header`），按现有刷新 token 流程处理。除此之外的业务错误统一在响应体的 `header.code` 里，**HTTP 状态码是 200**，不要只看 HTTP 状态码。

### Request

```json
{
  "title": "小猫 v2",
  "originalImageUrl": "",
  "patternImageUrl": "https://oss.example.com/works/pattern_v2.png",
  "thumbnailUrl": "",
  "patternData": {
    "width": 3,
    "height": 3,
    "boardSpec": "29x29",
    "schemaVersion": 1,
    "pixels": [0, 1, 1, 2, 0, 0, 2, 1, 0],
    "colorPalette": [
      { "index": 1, "hex": "#FF0000", "brand": "hama", "code": "H-22", "name": "红" },
      { "index": 2, "hex": "#FFFFFF", "brand": "hama", "code": "H-01", "name": "白" }
    ]
  }
}
```

（`patternData` 用 3x3 举例便于看清 `pixels` 的展平结构；真实请求里 `pixels` 长度就是 `width * height`，29x29 是 841 个。`boardSpec` 是豆板规格，与图纸尺寸是两回事，不要求相等。）

| 字段 | 类型 | 必填 | 说明 |
|---|---|:---:|---|
| `workId` | string | 是 | **在 URL 路径里**，不在 body 里 |
| `title` | string | 否 | 空字符串 = 不修改。首尾空格会被去掉 |
| `originalImageUrl` | string | 否 | 空 = 不修改 |
| `patternImageUrl` | string | 否 | 空 = 不修改。**值真的变了**才会触发缩略图重新生成 |
| `thumbnailUrl` | string | 否 | 缩略图的**源图**地址，不是缩略图本身。空 = 按 `patternImageUrl` 生成 |
| `patternData` | object | 否 | **不传 = 图纸内容完全不变**。传了就整份替换，不支持局部 patch |

**「空 = 不修改」是刻意的。** 这些字段都没有有意义的空值（作品不可能没有标题、没有图纸图），所以用空值表达「本次不动它」。客户端只改标题时，body 可以只有 `{"title": "新名字"}`。

### `thumbnailUrl` 的正确用法

这个字段容易误解：**传进去的是一张普通图片的地址，服务端拿它去生成缩略图**，客户端不需要自己压缩、自己编码 WebP。

- 大多数情况**不用传**。改了 `patternImageUrl` 时服务端会自动用新图纸图生成缩略图。
- 只有当你希望缩略图用另一张图（比如不带色号标注的纯净图纸图）时才传。
- 传的地址必须是**本站对象存储**的地址（走 `POST /api/v1/media/upload-token` + `POST /api/v1/media/report-upload` 上传拿到的）。外站地址会被拒绝，服务端不会去抓任意主机的图。
- 生成失败不会导致整个请求失败，旧缩略图会保留——列表里显示一张稍旧的图，好过一个空白格子。

### Response

```json
{
  "header": { "code": 0, "message": "success", "requestId": "..." },
  "work": {
    "workId": "64",
    "title": "小猫 v2",
    "originalImageUrl": "https://oss.example.com/works/origin.jpg",
    "patternImageUrl": "https://oss.example.com/works/pattern_v2.png",
    "thumbnailUrl": "https://oss.example.com/works/thumb_v2.webp",
    "boardSpec": "29x29",
    "width": 29,
    "height": 29,
    "beadCount": 512,
    "colorCount": 12,
    "status": 2,
    "sourceType": "ai",
    "sourceId": "gen-1024",
    "createdAt": 1754899200,
    "updatedAt": 1754985600
  }
}
```

`work` 与作品列表 `GET /api/v1/works` 返回的条目结构完全一致，可以直接替换本地列表里的同 `workId` 条目，不需要额外再请求一次列表或详情。

---

## 2. 服务端会自动重算的字段

传了 `patternData` 时，这些字段**由服务端从图纸数据推导**，客户端传什么都会被覆盖，直接用响应里的值即可：

| 字段 | 推导方式 |
|---|---|
| `width` / `height` | 取 `patternData.width` / `height` |
| `boardSpec` | 取 `patternData.boardSpec` |
| `beadCount` | 统计 `pixels` 里非 0 的格子数 |
| `colorCount` | 统计 `pixels` 里实际用到的颜色数（不是 `colorPalette` 的长度） |
| `updatedAt` | 服务端当前时间 |

**注意 `colorCount`**：调色板里带了但一个格子都没用上的颜色不计入。所以用户删掉某个颜色的全部格子后，`colorCount` 会下降，即使调色板没动。

## 3. 服务端会保留的字段

这些字段客户端改不了，修改前后不变：

- `status` — 已完成的作品仍是已完成（`2`），草稿仍是草稿（`1`）。**改图不会让作品变回草稿。**
- `sourceType` / `sourceId` — AI 生图来源等信息不丢
- `createdAt` — 仍是首次保存的时间
- `thumbnailUrl` — 除非满足上面的重新生成条件，否则保持原值

---

## 4. `patternData` 校验规则

传了 `patternData` 就会全量校验，任一条不满足返回 `1101`，并且**这条记录一个字段都不会被修改**（校验在写库之前，不存在改了一半的情况）：

| 规则 | 说明 |
|---|---|
| `width` / `height` > 0 且 ≤ 200 | |
| `width * height` ≤ 40000 | |
| `pixels.length == width * height` | 展平后的一维数组，行优先。**不是二维数组** |
| `boardSpec` 非空 | 例如 `"29x29"` |
| `schemaVersion == 1` | 必须显式传 1 |
| `colorPalette` 非空且 ≤ 221 项 | |
| `colorPalette[].index` > 0 且不重复 | `0` 保留给空格子，不能出现在调色板里 |
| `colorPalette[].hex` 形如 `#RRGGBB` | 必须 6 位十六进制，`#FFF` 这种简写会被拒 |
| `pixels` 里每个非 0 值都能在 `colorPalette` 里找到 | 删颜色时记得把对应格子一起清成 0 |

`brand` / `code` / `name` 是可选的展示字段，不校验。

---

## 5. 错误处理

| `header.code` | 含义 | 建议处理 |
|---|---|---|
| `0` | 成功 | 用 `work` 更新本地列表 |
| `1101` | `workId` 不是合法数字，或 `patternData` 校验失败 | 提示「图纸数据有误，请重试」；这属于客户端 bug，应记日志上报 |
| `1102` | 作品不存在，或不属于当前用户 | 提示「作品已不存在」并从本地列表移除 |
| `2006` | 该作品有投稿正在审核中 | 提示「投稿审核中，暂时无法修改此图纸」，见下节 |
| `5000` | 服务端内部错误 | 提示重试，保留用户的本地编辑不要丢 |

`1102` 把「作品不存在」和「是别人的作品」合并成同一个码，这是故意的——避免通过错误码探测别人的 `workId`。客户端两种情况按同样方式处理即可。

**HTTP 501**：如果收到 501，说明请求打到了不存在的路由，检查是不是把 `workId` 拼错了（比如拼成了 `PUT /api/v1/works` 少了 ID，或多了尾斜杠）。

---

## 6. 投稿审核中的作品不能修改（`2006`）

用户把作品投稿到官方图纸库（`POST /api/v1/template-submissions`）后，只要那条投稿还是**待审核**（`status = 0`），这个作品就被锁住：本接口和 `POST /api/v1/works/drafts` 都会返回 `2006`，记录一个字段都不会被改。

**为什么要锁**：投稿是快照，用户改作品其实动不了审核内容。但如果放开修改，用户会以为「我改了，运营审的就是新版本」——实际审的还是投稿那一刻的版本。锁住是为了不产生这个误解，不是为了数据安全。

**什么时候解锁**：

| 投稿状态 | 能否修改作品 |
|---|:---:|
| `0` 待审核 | ✗ 返回 `2006` |
| `1` 已通过 | ✓ 可以改 |
| `2` 已驳回 | ✓ 可以改 |

**已通过后可以随便改**，因为发布出去的官方图纸是一份独立的副本，与用户自己的作品彻底脱钩了——用户改自己的作品、甚至删掉它，都不会影响已上架的那张图纸。所以不要在 UI 上写「修改作品会同步更新已发布的图纸」。

**客户端建议**：

- 在「我的作品」列表里，对有待审核投稿的作品显示一个「审核中」角标，并把编辑入口置灰，而不是等用户改完点保存才弹 `2006`。判断依据是 `GET /api/v1/template-submissions` 里 `status == 0` 的那些条目的 `workId`。
- 用户已经进了编辑器才拿到 `2006` 时，**不要丢掉他的本地编辑**。提示「投稿审核中，暂时无法保存修改」，让他可以选择「另存为新作品」（走 `POST /api/v1/works` 存成新的一份）。
- 目前**没有撤回投稿的接口**，所以待审核期间用户没有别的解锁办法，只能等审核结果。文案上说清楚「审核完成后即可修改」，避免用户反复重试。

---

## 7. 重试与并发

- **这个接口不是幂等键式设计**（没有 `clientRequestId`），但它天然可安全重试：同样的 body 重放一次，结果和只发一次一样，因为它是「把这些字段设成这些值」而不是「在原值上增减」。网络超时后直接重试即可。
- **没有乐观锁**。同一个作品在两台设备上同时编辑，后提交的覆盖先提交的。目前不做冲突检测，客户端如果关心，可以在保存前对比本地缓存的 `updatedAt` 和服务端最新值，不一致时提示用户。

---

## 8. Dart 参考

```dart
// 只改标题
Future<WorkItem> renameWork(String workId, String title) async {
  final resp = await client.put(
    '/api/v1/works/$workId',
    data: {'title': title},
  );
  return WorkItem.fromJson(_unwrap(resp)['work']);
}

// 改图纸（编辑器保存）
Future<WorkItem> updateWorkPattern({
  required String workId,
  required PatternData pattern,
  String? patternImageUrl, // 重新渲染并上传了图纸图时才传
  String? title,
}) async {
  final resp = await client.put('/api/v1/works/$workId', data: {
    if (title != null && title.isNotEmpty) 'title': title,
    if (patternImageUrl != null) 'patternImageUrl': patternImageUrl,
    'patternData': pattern.toJson(), // width/height/boardSpec/schemaVersion/pixels/colorPalette
  });
  return WorkItem.fromJson(_unwrap(resp)['work']);
}
```

`_unwrap` 即现有的「检查 `header.code == 0`，否则抛业务异常」的公共逻辑，与其他接口一致。

---

## 9. 联调要点

1. **先跑通只改标题**：body 只有 `{"title": "x"}`，断言响应里 `beadCount`、`width`、`thumbnailUrl` 都和改之前一样。这一步能立刻暴露「误传了空 `patternData`」的问题。
2. **再跑改图纸**：改几个格子后保存，断言 `beadCount` 按预期变化，`status` 仍是 `2`，`sourceType` 没丢。
3. **改尺寸**：从 29x29 改成 20x20，确认 `pixels` 长度同步改成 400，否则会拿到 `1101`。
4. **换图纸图**：传新的 `patternImageUrl`，确认响应里 `thumbnailUrl` 变了（是一个新的 `.webp` 地址）。
5. **异常路径**：拿别人的 `workId` 调一次，确认收到 `1102` 且 UI 不崩。
6. **审核锁**：把作品投稿一次（`POST /api/v1/template-submissions`），再调本接口，确认收到 `2006` 且作品没被改动。

curl 示例见 `docs/api-testing-guide.md` §7.4。
