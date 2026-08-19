# 管理后台图纸草稿接口契约

本文约定 Web 管理后台新增的「草稿」能力，服务端已按本文实现（第 10 节记录了原需求稿四个开放项的结论）。鉴权与响应信封沿用 `docs/admin-template-management-api.md`：

```http
Authorization: Bearer <admin_access_token>
X-Platform: web
X-Device-Id: admin-web
```

## 0. 背景与目标

当前后台只有「发布」和「更新」两个动作，二者都立即对用户端生效：

- 生成图纸后唯一出口是 `POST /api/v1/admin/templates`，点了就上线，没有中间态。
- 编辑已发布模板走 `PUT /api/v1/admin/templates/{templateId}`，全量覆盖并立即生效，改一半保存就等于把半成品推给了用户。
- 编辑器状态纯内存，刷新或关闭浏览器即丢失，无法「先存着，下次继续改」。

需要达成的两个场景：

1. **新图纸暂存**：生成或画了一半，先存草稿，改天继续，满意了再发布。
2. **已发布模板的修订暂存**：在不影响线上版本的前提下修改已发布模板，满意了再一次性替换。

## 1. 核心模型

草稿是**独立资源**，有自己的 `draftId`，并可选关联一个已发布模板：

| 场景 | `templateId` | 发布后行为 |
| --- | --- | --- |
| 新图纸草稿 | 为空 | 新建一条已发布模板 |
| 已发布模板的修订草稿 | 指向该模板 | 覆盖该模板，草稿删除 |

关键约束：

- **草稿完全不影响线上**。已发布模板的 `patternData` 与预览资源在草稿保存期间保持原样，用户端读到的始终是已发布版本。只有「发布草稿」这一步才会替换线上内容。
- **一个模板同时最多挂一份草稿**。对同一 `templateId` 重复创建草稿应返回已存在的那份（含 `draftId`），而不是产生第二份。
- **草稿对全部管理员可见可编辑**，不做归属隔离。因此需要并发保护，见第 8 节。

## 2. 保存草稿（新建）

```http
POST /api/v1/admin/template-drafts
Content-Type: application/json

{
  "idempotencyKey": "admin-draft-1735900000000",
  "templateId": "",
  "title": "小狐狸",
  "description": "",
  "categoryId": 7,
  "tags": "动物,入门",
  "difficulty": 1,
  "previewFileKey": "",
  "patternData": {
    "schemaVersion": 1,
    "width": 3,
    "height": 3,
    "boardSpec": "3x3",
    "pixels": [0, 0, 0, 0, 0, 0, 0, 0, 0],
    "colorPalette": []
  }
}
```

> 示例用 3×3 只为把 `pixels` 写全。实际请求里 `pixels` 的长度必须**严格等于** `width * height`（29×29 就是 841 个整数），原稿里 `"pixels": []` 配 29×29 是错的，会被 400 挡下。`schemaVersion` 也不能缺省。

响应：

```json
{
  "header": {"code": 0, "message": "success"},
  "draft": {"draftId": "12", "templateId": "", "updatedAt": "2026-08-19T10:03:00.123Z"}
}
```

请求体的**精确字段集合**就是上面这些：`idempotencyKey`、`templateId`、`title`、`description`、`categoryId`、`tags`、`difficulty`、`previewFileKey`、`patternData`。服务端开了 `DisallowUnknownFields`，多一个未知键就是 400——**不要把创建接口的 payload builder 直接拿去打 §3 的 PUT**（PUT 不接受 `idempotencyKey` 与 `templateId`，但要求 `baseUpdatedAt`）。

字段规则（**与发布接口的关键差异**）：

- **除 `patternData` 外所有业务字段都允许为空或缺省**。草稿的用途就是承载半成品：标题没想好、分类还没选、`difficulty` 未定都必须能存下来。发布接口现有的必填校验不可照搬到本接口，否则草稿功能失去意义。
- `patternData` 仍需通过结构校验（沿用 `docs/pattern-data-front-backend-contract.md`），结构非法返回可读错误。校验规则：`schemaVersion == 1`、`len(pixels) == width * height`、`boardSpec == "{width}x{height}"`、像素引用必须落在调色板范围内、调色板不超过配置上限。**唯一为草稿放宽的一条**：全空画布（所有像素为 0，一颗豆都没落）允许 `colorPalette` 为空数组；只要有任何非 0 像素，调色板就必须非空。
- `previewFileKey` 可为空。草稿列表的缩略图不强制要求：为空时前端用 `patternData` 本地渲染兜底（该兜底渲染已在模板库列表使用）。这样自动保存不必每次重新上传 358×358 PNG。若前端提交了 `previewFileKey`，服务端照常存储并在列表返回可访问 URL。
- `templateId` 非空时表示这是某个已发布模板的修订草稿，服务端需校验该模板存在且处于已发布状态，否则返回 `404`。
- `idempotencyKey` **必填非空**，沿用发布接口的语义，防止重复点击产生多份草稿。空串返回 `1101`。
- `draftId`、`templateId` 一律是**字符串**（没有关联模板时 `templateId` 为 `""`）。`updatedAt` 是毫秒精度的 UTC RFC3339，见第 8 节。

## 3. 更新草稿

```http
PUT /api/v1/admin/template-drafts/{draftId}
Content-Type: application/json

{
  "title": "小狐狸",
  "description": "适合入门",
  "categoryId": 7,
  "tags": "动物,入门",
  "difficulty": 1,
  "previewFileKey": "",
  "patternData": {"...": "..."},
  "baseUpdatedAt": "2026-08-19T10:03:00.123Z"
}
```

- 请求体的精确字段集合：`title`、`description`、`categoryId`、`tags`、`difficulty`、`previewFileKey`、`patternData`、`baseUpdatedAt`。**不接受** `idempotencyKey` 与 `templateId`——草稿与已发布模板的关联在创建时确定，改关联等于换一份草稿。多传任何一个键都是 400。
- 字段与创建接口一致，同样允许为空。
- `baseUpdatedAt` **必填**，是前端读到这份草稿时的 `updatedAt`，用于乐观锁，见第 8 节。
- 响应形状与创建接口一致（`draft.draftId` / `draft.templateId` / `draft.updatedAt`），前端据新的 `updatedAt` 更新本地基线。
- `previewFileKey` 与库里存的相同时服务端不会重新生成缩略图（那要走一趟对象存储的下载—缩放—上传）。常见的自动保存因此完全不碰外部服务。

## 4. 草稿箱列表

```http
GET /api/v1/admin/template-drafts?page.page=1&page.pageSize=50
```

```json
{
  "header": {"code": 0, "message": "success"},
  "drafts": [
    {
      "draftId": "12",
      "templateId": "",
      "title": "小狐狸",
      "categoryId": 7,
      "categoryName": "动物",
      "thumbnailUrl": "",
      "difficulty": 1,
      "width": 29,
      "height": 29,
      "colorCount": 8,
      "updatedAt": "2026-08-19T10:03:00.123Z",
      "updatedByActor": "admin-zhao"
    }
  ],
  "page": {"total": 1, "page": 1, "pageSize": 50, "hasMore": false}
}
```

- 按 `updatedAt` 倒序返回，最近改的排最前（`draftId` 作为次级键保证分页确定）。
- `updatedByActor` 用于在多人协作时显示「最后由谁修改」。
- `title` 为空的草稿由前端显示为「未命名草稿」，服务端不必填充占位标题。
- **列表不返回 `patternData`**（§10 开放项 2 的结论）。单份 `patternData` 在 200×200 上限下约 160–250KB，一页 50 条就是十几 MB；更要紧的是列表要 `ORDER BY updated_at`，MySQL 的 filesort 会缓冲每一个被选中的列，几 MB 的 JSON 一排序就撞 error 1038（out of sort memory）。同理 `width`/`height`/`colorCount` 是保存时派生落列的，列表因此完全不必读 `pattern_data`。
- `thumbnailUrl` 是**尽力而为**的三档取值，不是必填：
  1. 修订草稿（`templateId` 非空）→ 借用线上模板的缩略图。视觉上可能滞后于草稿里的改动，但不必为每次自动保存重新生成一张 PNG。
  2. 否则草稿自带 `previewFileKey` → 用它自己的缩略图。
  3. 都没有 → `""`，前端拉 §5 详情用 `patternData` 本地渲染兜底。

## 5. 草稿详情

```http
GET /api/v1/admin/template-drafts/{draftId}
```

响应结构对齐 `GET /api/v1/admin/templates/{templateId}`，用于重新进入编辑页：

```json
{
  "header": {"code": 0, "message": "success"},
  "draft": {
    "draftId": "12",
    "templateId": "",
    "title": "小狐狸",
    "description": "适合入门",
    "categoryId": 7,
    "tags": ["动物", "入门"],
    "difficulty": 1,
    "previewFileKey": "",
    "updatedAt": "2026-08-19T10:03:00.123Z",
    "updatedByActor": "admin-zhao"
  },
  "patternData": {"...": "..."}
}
```

- 注意 `tags` 在详情里是**数组**，而在 §2/§3 的请求体里是**逗号分隔字符串**。这个不对称是刻意的，与现有 `GET /api/v1/admin/templates/{templateId}` 一致。
- 草稿不存在返回 HTTP `404` + `4002`。

## 6. 发布草稿

```http
POST /api/v1/admin/template-drafts/{draftId}/publish
Content-Type: application/json

{
  "idempotencyKey": "admin-publish-1735900000000",
  "previewFileKey": "oss-key-of-358x358-png",
  "baseUpdatedAt": "2026-08-19T10:03:00.123Z"
}
```

- 请求体的精确字段集合就是这三个，三个**都必填**，多传任何键都是 400。
- **完整校验在此刻执行**，而非保存草稿时：`title` 非空、`categoryId` 引用一个启用中的分类、`patternData` 合法、`previewFileKey` 存在。任一不满足返回 `4004` + 可读 `message`，前端把用户挡回编辑页补全（`previewFileKey` 为空是个例外，它由媒体层挡下来，返回 HTTP `403` + `1003`）。
- `previewFileKey` 由前端在发布前重新生成并上传 358×358 PNG（走现有 `POST /api/v1/admin/media/upload`），规范见 `docs/admin-template-management-api.md` 第 1 节。发布请求携带该 key，因为草稿期间可能一直没有缩略图。
- `baseUpdatedAt` 与 §3 同义：过期就返回 `4001`，不会静默覆盖别人的改动。
- 服务端按 `templateId` 分支处理，**整个过程在一个数据库事务里**：
  - `templateId` 为空 → 新建已发布模板，返回新 `templateId`。
  - `templateId` 非空 → 覆盖该模板的图纸、元信息、预览资源。覆盖前会把旧版本写进一张只写的历史表（§10 开放项 4），无读接口无回滚接口，仅供误覆盖后人工找回。
  - `templateId` 非空但该模板已被下架 → `4004`（**不是** `4002`：草稿明明还在箱里，报「草稿不存在」会让管理员无路可走）。
- 发布成功后删除该草稿。失败时草稿完整保留，且不会留下只更新了一半的模板。唯一的例外是发布前生成的缩略图对象可能在对象存储里成为孤儿——线上模板仍指向旧预览，业务上无影响。
- **重放安全**：用同一个 `idempotencyKey` 重发同一个发布请求返回 `200` + 相同的 `templateId`，不会产生第二条模板；此时草稿已经删掉了，服务端靠发布记录而不是草稿行来识别重放。用同一个 key 去发布**另一份**草稿会被拒绝。

响应：

```json
{
  "header": {"code": 0, "message": "success"},
  "templateId": "37"
}
```

## 7. 丢弃草稿

```http
DELETE /api/v1/admin/template-drafts/{draftId}
```

- 操作幂等：删除不存在的草稿也返回 HTTP `200` + `code: 0`，响应体为 `{}`。前端的「丢弃」按钮不需要处理竞态。
- 丢弃草稿不影响其关联的已发布模板。

## 8. 并发保护

草稿全员可编辑，两名管理员同时改同一份草稿会互相覆盖。约定：

- 更新（§3）与发布（§6）都必填 `baseUpdatedAt`。服务端当前 `updatedAt` 与它**不精确相等**时返回 `4001` + HTTP `409`，不会静默覆盖。
- **`updatedAt` 是毫秒精度的 UTC RFC3339**，形如 `2026-08-19T10:03:00.123Z`。前端必须原样回传服务端给出的字符串，不要重新格式化、不要截到秒——秒级精度会让同一秒内的两次更新都判定成功，其中一次静默丢失。服务端会把多带的小数位截到毫秒，所以往返是安全的。
- `4001` 的 `message` 里带了抢先写入的管理员（`draft was modified by {actor}`），但**那只供日志排查**。要展示「这份草稿已被 X 修改」，请在收到 `4001` 后重新 `GET` §5 详情，取其中的 `updatedByActor`——`message` 文案不是契约。
- 两名管理员同时点**发布**时，二者会生成不同的 `idempotencyKey`，所以幂等键挡不住他们；服务端靠行锁加 `updated_at` 的 CAS 保证只有一个赢，败者拿到 `4001`。不会产生重复模板。

## 9. 已发布模板列表需补充的字段

`GET /api/v1/admin/templates` 的每条模板增加两个字段，用于在模板库标示「这张图有未发布的改动」：

```json
{"hasDraft": true, "draftId": "12"}
```

没有草稿时 `hasDraft` 为 `false`、`draftId` 为 `""`。前端据此在卡片上打「草稿」角标，并把编辑入口指向草稿而非模板本身，避免管理员绕过草稿直接覆盖线上。

这两个字段**只在这条管理端路由上出现**，用户端的模板列表不含它们。

> 遗留：`PUT /api/v1/admin/templates/{templateId}` 仍是不经草稿的直接覆盖，且对草稿无感知。管理员用它改了图之后再发布那份修订草稿，会静默覆盖掉这次改动且没有冲突检测——§9 的角标只是 UI 层的提示。至少覆盖前的版本会进历史表。

## 10. 开放项的结论

| # | 开放项 | 结论 |
| --- | --- | --- |
| 1 | 草稿上限与保留期限 | 全局上限 **200**，可配置（`template_draft.max_count`）。**只在创建时校验**：若同时卡住更新接口，管理员在满箱状态下连「编辑现有草稿以便发布腾位」都做不了。达到上限时创建返回 `4003` + 可读消息，前端提示先清理。上限是近似值（并发下可能略微超出）。**不做 TTL 自动清理**——草稿是管理员的在制品，静默删除比留着更糟。 |
| 2 | 列表缩略图方案 | 列表**不返回** `patternData`，`thumbnailUrl` 尽力而为而非必填，详见 §4。 |
| 3 | 错误码分配 | 见下表。 |
| 4 | 历史版本 | 覆盖已发布模板前写一条**只写快照**。没有读接口也没有回滚接口，唯一用途是误覆盖后由人工从数据库找回。本次不提供回滚 API。 |

### 错误码

4xxx 是本次为管理后台草稿流程新开的号段。**数值即契约**，前端按码值分支。

| `header.code` | HTTP | 含义 | 前端处理 |
| --- | --- | --- | --- |
| `4001` | `409` | 乐观锁冲突，草稿已被别人改过 | 提示冲突并提供重新加载入口（actor 从 §5 详情取） |
| `4002` | `404` | 草稿不存在 | 关闭编辑页，回草稿箱 |
| `4003` | `400` | 草稿数量超出上限 | 提示先清理草稿箱 |
| `4004` | `400` | 草稿存在但现在发不出去（必填项未补全 / 关联模板已下架 / 幂等键已被另一份草稿占用） | 按 `message` 把用户挡回编辑页 |

沿用的现有码：`1101` 参数非法（含空 `idempotencyKey`、`baseUpdatedAt` 缺失或格式错误、`patternData` 结构非法）、`1003` 权限不足（含 `previewFileKey` 未上传）、`5000` 服务端内部错误。

## 11. 权限与错误约定

沿用现有约定：无权限返回 HTTP `401/403`，草稿或模板不存在返回 HTTP `404`，其余业务错误通过 `header.code` 与 `header.message` 返回。所有新增接口只接受管理员 access token。
