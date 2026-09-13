# CODEX_TASK_v028.md — v028 任务（clear-all 语义修复 + 文件库交互调整 + 分享错误区分）

- 起点：git HEAD=`607209f`（v027）。工作区含 1 处**未提交**改动 `web/src/transferFlow.js`（v026 传输可靠性，按指示**不合并**）。
- 来源：用户 2026-09-12 反馈 6 项（#1 为分析类，#2–#6 为改进类）+ 本次复现取证。
- 执行方式：小步提交；前端改动需 `npm run build` 并同步 `internal/webassets/dist`（注意工作区 transferFlow.js 的影响，见决策点 3）。
- 验证要求：`go test ./...`、`go test -race ./internal/httpapi/`（本机已有便携 GCC，见 v027 记录）、前端 `npm run build` + node 测试、部署后在线复现清单。
- **本文件先交付评审，确认后再执行。**

## 执行结果（2026-09-12 已完成，决策按 1B / 2做 / 3b / 4移除 / 5弹窗选择）

- **#1**：`store.ClearFiles(ctx, userID, allUsers)` 取代 `ClearUserFiles`——单事务内软删 ready 文件、删目录记录、**删未完成上传任务（pending/active/queued）**、**按创建者软撤销分享**、重置配额，并返回明细；HTTP 层清理文件内容与 `tmp/<taskID>`，按 `diskMode` 执行 **empty-dirs（自底向上删空目录）** 或 **whole-tree（删 `files/<uid>` 整树）**；`scope=all` 仅管理员（否则 403 `SCOPE_FORBIDDEN`）；审计与服务事件记录 scope/disk/文件·任务·分享计数。前端清空弹窗新增**范围单选（仅我的文件/所有用户的文件（管理员））**、**磁盘处理单选（仅删空目录/删除整个存储目录）**、范围数量提示「将清空你名下的 N 个文件」与本地化成功提示（含清理统计）。
- **#2**：移除工具栏排序控件，表头 `文件名/大小/类型/上传时间` 改为可点击排序（▲/▼ + `aria-sort`，键盘可达）；`文件完整性` 列不参与排序。
- **#3**：文件库与集合上传页的拖放入口、提示文案、`dragging` 状态、`handleDrop`/`collectDropFiles`(Entries) 与相关样式全部移除；`选择文件/上传文件夹` 及目录确认流程保持不变。
- **#4**：`新建文件夹` 移入同一操作行（选择文件 → 上传文件夹 → 新建文件夹）。
- **#5**：失败原因统一写入 `item.error`，`title` 提升到**失败行整行**并覆盖已完成/失败页签（触屏可点击查看由既有展开逻辑承载）。
- **#6**：匿名分享端点返回稳定 code `SHARE_NOT_FOUND`/`SHARE_REVOKED`/`SHARE_EXPIRED`/`SHARE_CONTENT_MISSING`（meta/download/preview 一致）；「我的分享」新增 `status=content_missing` + `contentMissing` 标记与提示；`api.js` 的 `codeKeys` 新增四条映射，i18n 三语新增 `error.shareContentMissing`、`shares.status.content_missing`、`shares.contentMissingHint`。
- **验证**：`go test ./...` 全绿（新增 `internal/httpapi/v028_test.go` 5 用例：own 范围全清+空目录回收、scope=all 权限、whole-tree、四种分享错误码、内容缺失列表标记）；web 三个 node 测试 exit=0；前端按决策 3b 构建（构建前把 `transferFlow.js` 还原为 HEAD、构建后还原工作区，产物不含 v026 改动）。在线 13 项复现清单通过（own 清空 files=2/tasks=1/shares=1、磁盘空目录已回收、任务 404、usedBytes=0、公开链接 SHARE_REVOKED、非管理员 scope=all→403、未知 token→SHARE_NOT_FOUND、内容删除→SHARE_CONTENT_MISSING（meta+download）、列表 content_missing、上传/排序/非法排序 400/下载回归）。浏览器目视：无拖放提示、三按钮同行、表头 `大小▲` 且点击后升序生效（19B/31B/31B）、清空弹窗含范围与磁盘选项及「将清空你名下的 0 个文件」提示。
- **未执行（安全考虑）**：admin `scope=all` 的破坏性全库清空未在共享演示数据上执行（会删除 user id=2 的 1082 个文件）；该路径由 Go 测试 `TestClearAllScopeAllRequiresAdmin` 在隔离实例验证。
- **服务**：已构建 v028 并重启（`filebox-v028.exe`，127.0.0.1:18080，pid 9572）；工作区 v028 改动**未提交**，`transferFlow.js` 保持 v026 未合并状态。

## 一、#1「clear-all 实际没有全部清空」根因分析（已实测）

**复现方法**：在 v027 实例（127.0.0.1:18080）用一次性用户 pt8b 造出「根文件 + 子目录文件 + 分享 + 未完成上传任务」，执行 clear-all 前后逐项快照。

| 观察项 | 清空前 | 清空后 | 判定 |
|---|---|---|---|
| 根目录文件（列表 total） | 1 | 0 | ✅ 正常 |
| 子目录文件（`dir=sub`） | 1 | 0 | ✅ 正常（返回 deleted=2） |
| 目录记录 folders | 1 | 0 | ✅ 正常 |
| `usedBytes` | — | 0 | ✅ 正常 |
| **磁盘目录** `data/files/<uid>/sub` | 存在 | **仍存在（空目录）** | ❌ 残留，磁盘空间未回收 |
| **未完成上传任务**（pending） | 存在 | **仍存在**（`GET /api/files/{taskID}/status` = 200） | ❌ 残留（配额预留与该任务 tmp 分片目录仍在） |
| **分享记录** | active | **仍 active**（公开链接已 404） | ❌ 残留（"我的分享"显示有效，点开却报"分享不存在"） |
| **管理员文件列表** | 全库 1082 | 全库 1082 | ❌ **作用域错配（主因）** |

**四项根因**

1. **管理员作用域错配（用户现象的主因）**：`GET /api/files` 对 admin 是"全库视图"（`ListFilesSorted` 在 admin 且无 dir 时不加 `user_id` 过滤；实测 admin 名下 0 个文件、列表却是 1082 个其他用户的文件），而 `clear-all` 只清**当前用户名下**的 ready 文件（`ClearUserFiles` 按 `user_id`）。以 admin 执行时 `deleted=0`、列表不变 → 看起来"没有清空"。
2. **未完成上传任务未清理**：`ClearUserFiles` 只处理 `files` / `folders` / `users.used_bytes`，不触碰 `upload_tasks`（pending/active/queued）与 `data/tmp/<taskID>`。
3. **物理目录未删除**：目录记录删了，磁盘 `files/<uid>/…` 的空目录保留（`clearAllFiles` 只对每个文件路径 `RemoveAll`，不删目录）。
4. **分享记录未撤销**：清空后 `shares` 仍为未撤销，`/api/shares` 中显示 `status=active`，但公开链接 404 → 与 #6 叠加成"分享不存在"的困惑。

**附带问题**：清空成功提示 `notice.value = 'Files cleared'` 为硬编码英文（未走 i18n），且清空前未告知"将清空多少个文件/清空范围"。

**修复方案（建议）**

- **范围可见（根因 1）**：`clear-all` 保持"仅本人文件"语义，但必须让范围可见——前端清空弹窗显示"将清空你名下的 N 个文件（其他用户的文件不受影响）"；admin 的全库视图下附注"当前列表为全库视图，其中属于你的文件 N 个"。可选（决策点 1）：为 admin 提供"清空范围 = 我的文件 / 全部用户"，后者需更强二次确认 + 独立审计原因码。
- **残留清理（根因 2/3/4）**：`ClearUserFiles` 扩展为一次性清空事务——①删除该用户全部未完成任务（`upload_tasks` 的 pending/active/queued），提交后清理 `data/tmp/<taskID>`；②提交后**自底向上删除 `files/<uid>` 下的空目录**（不删非空目录，规避与并发上传的竞态）；③**软撤销该用户全部分享**（`shares.revoked_at=now`）；④审计/服务事件返回明细（文件数/目录数/任务数/撤销分享数）。
- **前端提示**：成功提示走 i18n（新增 `files.clearedNotice`，三语）并展示清理统计；保持 `loadMe/loadFolders/loadFiles` 刷新。

## 二、#2–#6 改进项

| # | 类型 | 现状（已核实） | 方案 | 验收标准 |
|---|---|---|---|---|
| 2 | 交互 | 排序是工具栏中的独立控件 `div.file-sort-controls`（`ArrowUpDown` 图标 + `sortBy` 选择 name/size/type/updatedAt + `sortOrder` 选择 A-Z/Z-A），`applySort()` 触发；后端 `GET /api/files?sortBy=&sortOrder=` 与 `/api/folders` 已支持（v026） | 移除该控件；将表头 `files.name` / `files.size` / `files.type` / `files.uploadedAt` 四列改为**可点击排序**（点击同列在 asc/desc 间切换，切到其他列默认 asc），带 ▲/▼ 指示与 `aria-sort`；`files.integrity` 列不参与排序；保留 `sortBy/sortOrder` refs 供 `loadFiles/loadFolders` 使用 | 表头点击可排序且指示正确；工具栏不再有排序控件；目录行与文件行遵循同一排序参数；键盘可达（Enter/Space）；构建与 node 测试通过 |
| 3 | 交互 | 拖放由 `content-wrap` 的 `@dragover/@dragleave/@drop` + `dragging` 状态 + `handleDrop()`/`collectDropFiles()` 实现；提示文案是 `.upload-zone` 内的 `files.dropTitle`/`files.dropCopy`（"拖放文件到这里上传"），同区还含"选择文件/上传文件夹"按钮 | **仅移除拖放**：删除 `@drag*` 处理器、`dragging` 状态与相关样式（`.toolbar.dragging`、`.upload-zone` 拖放态）、`handleDrop`、`collectDropFiles`（确认无其他调用后）；移除 `dropTitle`/`dropCopy` 元素与 i18n 键（三语，`dropEmpty` 若无引用一并清理）；**保留** `选择文件`/`上传文件夹` 按钮与目录确认弹窗逻辑；集合上传页 `.public-drop-zone` 不在本次范围（决策点 4） | 文件库无拖放入口与提示；"选择文件/上传文件夹"流程行为不变；样式无残留死代码 |
| 4 | 交互 | "新建文件夹"在独立行 `div.dir-actions`（`openNewFolder()`），与工具栏内的 选择文件/上传文件夹 分离 | 将"新建文件夹"移入 选择文件/上传文件夹 所在操作行，顺序建议：选择文件 → 上传文件夹 → 新建文件夹；只读用户仍隐藏写操作按钮；空列表状态下该行仍可见 | 三按钮同一行；窄屏（≤800px）自动换行不溢出；只读用户不见"新建文件夹" |
| 5 | 交互 | 失败行**已有** `:title="item.failed ? (item.error \|\| item.status) : item.status"`，但**仅在状态文本 span 上**，且多条失败路径未写入 `item.error`；完成/失败页签的行没有 title | 统一失败原因：① 所有失败路径（分片/complete/网络 `friendlyError`/批量 ZIP/下载）都写入 `item.error`；② 将 `title` 提升到**失败行整行**（hover 任意位置可见），失败页签行同样处理；③ 文案使用 `friendlyError`/`localizeError` 的本地化原因，不暴露原始英文堆栈；④ 触屏补充"点击展开原因"（title 在触屏无效） | hover 失败行任意处显示具体原因；Done/Failed 页签失败项同样可见；原因缺失时回退显示状态文案；三语正确 |
| 6 | 后端+前端 | `shareMeta` / `shareDownload` / `sharePreview` 对"链接不存在/已撤销"与"**文件已被删除**"都返回 `404 {"message":"分享不存在"}`（无 `code`）；`shareDownload` 另有 `share_limit`/`share_expired`。前端 `api.js` 的 `localizeError` 已支持按 `data.code` 映射，i18n 已有 `share.notFound/expired`、`error.shareNotFound/shareExpired/shareRevoked/fileContentNotFound` | 后端为匿名分享端点引入稳定 code：`SHARE_NOT_FOUND`（token 不存在）、`SHARE_REVOKED`、`SHARE_EXPIRED`、`SHARE_CONTENT_MISSING`（链接有效但文件已删除/不可用）、`SHARE_DOWNLOAD_LIMIT`（已有）；`shareMeta` 的"文件缺失"分支文案改为"分享内容不存在"；前端 `localizeError`/`ShareView` 映射新 code（新增三语 `error.shareContentMissing`）；"我的分享"列表对内容缺失的分享给出可辨识状态（决策点 2） | 三种场景文案互不相同：链接不存在/已撤销、链接已过期、**分享内容不存在**；响应含对应 `data.code`；分享页正确展示；既有 share 相关 Go 测试同步更新且全绿 |

## 三、待确认决策点（执行前需拍板）

1. **#1 管理员清空范围**：(A) 仅本人（默认，弹窗明确范围与数量）；(B) 额外提供"全部用户"选项（高危，需更强确认与审计）。**推荐 A**。
2. **#6 是否同时改"我的分享"列表**：是否把"内容已不存在"的分享标记为新状态（如"内容缺失"标签 + 一键撤销）。**推荐做**（否则用户仍困惑）。
3. **前端重建与未提交的 `transferFlow.js`**：`npm run build` 会把当前工作区该文件（v026 未合并改动）编入 `internal/webassets/dist`。选项：(a) 允许本次构建包含它；(b) 构建前把该文件临时恢复为 HEAD 版本、构建后还原工作区（产物不含 v026，且不合并 v026）；(c) 等你先处理 v026。**推荐 (b)**。
4. **#3 是否也移除集合上传页的拖放**（`.public-drop-zone`）：默认**仅文件库**。
5. **#1 磁盘处理方式**：仅删空目录（推荐，规避并发竞态） vs 删除整个 `files/<uid>`。

## 四、分批执行（确认后）

| 批 | 内容 | 验证 |
|---|---|---|
| 1 | #1 后端（`ClearUserFiles` 扩展 + 清空统计/范围数据）+ Go 测试 | `go test ./...`、`go test -race ./internal/httpapi/` |
| 2 | #6 后端 code + Go 测试 | 同上 + 在线三场景 |
| 3 | #2/#3/#4/#5 前端 + i18n + 样式 | `npm run build` + node 测试 |
| 4 | 同步 `internal/webassets/dist`（按决策点 3）+ 部署验证 | 在线复现清单 |
| 5 | 文档登记（CHANGELOG/STATE + 本文件"执行结果"） | — |

## 五、在线复现清单（部署后）

1. **普通用户**：根 + 子目录文件、分享、未完成任务 → clear-all → 文件/目录记录/未完成任务/分享状态/磁盘空目录**全部清理**，`usedBytes=0`，提示含清理数量。
2. **管理员**：全库列表下清空 → 弹窗明确"仅清空你名下 N 个文件"，执行后其他用户文件仍在（符合预期，不再被误判为"没清空"）。
3. **分享页**：分别访问 ①不存在的 token ②已撤销 ③已过期 ④内容被删除的 token → 四种文案各自正确且响应含对应 code。
4. **文件库**：表头点击排序（四列、升降序切换、指示正确）；无拖放提示且不可拖拽上传；三个按钮同一行；失败传输 hover 显示具体原因。
5. **回归**：上传/下载/分享/收集/同步主路径不受影响；`go test ./...` 与 race 全绿。

## 六、不在本次范围

- 集合上传页（`/u/:token`）的拖放入口（决策点 4 可另行安排）。
- v026 `web/src/transferFlow.js` 的未提交改动（按指示不合并）。
- `dist/` 发布产物重建（`make release` 会带上 v026 前端改动，待 v026 合并后再做）。
