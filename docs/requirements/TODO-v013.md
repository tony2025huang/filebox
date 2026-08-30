# FileBox v013 需求/问题 解决范围分析（TODO）

- **基线**：`HEAD 1ad65a0`（安全加固+修复批次之后）
- **分析方式**：codex CLI（`codex exec -C <repo> -s danger-full-access`，只读）分 4 批主题分析 + 人工源码核对；全部结论均落到真实文件与行号
- **约束**：本文档仅为**分析产出**，本轮未修改任何功能代码、未做任何 git 操作（另一子任务正在同仓库做第三轮修复）
- **状态**：待父任务确认后，由后续修复子任务按本文档执行

---

## 总览表

| # | 问题 | 解决范围 | 复杂度 | 优先级 | 合并建议 |
|---|------|---------|--------|--------|---------|
| 1 | 配额错误提示显示不全 | 纯前端 | S | P0 | — |
| 2 | 传输记录刷新后清空 | 前端为主 | M | P0 | 与 1 同在 FilesView 传输面板 |
| 3 | 同步支持选择目录/文件 | 前后端 | M | P1 | 与 4 合并（picker 改造） |
| 4 | 目录选择支持上级 | 前端为主 | S | P0 | 与 3 合并 |
| 5 | 同步到另一套 filebox | 前后端+DB迁移 | L | P2 | 依赖 3/4 的路径语义 |
| 6 | 新建同步任务 UI 高度不一致 | 纯前端 | S | P1 | — |
| 7 | 批量分享统一链接 | 前后端 | L | P2 | 公开页可复用 8 的错误处理 |
| 8 | 分享次数用完重复提示 | 纯前端 | S | P0 | — |
| 9 | 收集上传配额不足泄露信息 | 后端为主 | S | P0 | 与 10 同在 collection.go |
| 10 | 日志外部上传无法区分用户目录 | 后端 | S~M | P1 | 与 9 同批修 |
| 11 | 我的收集支持编辑 | 前后端 | M | P1 | — |
| 12 | 我的收集入口到菜单栏 | 纯前端 | S~M | P1 | 与 13/14 合并（共享顶栏） |
| 13 | 管理后台隐藏问题 | 纯前端 | M | P1 | 与 12/14 合并 |
| 14 | 同步独立菜单 | 纯前端 | S | P1 | 与 12/13 合并 |
| 15 | 本地上传速度受限分析 | 分析类（+可选优化） | M | P2 | 产出为结论与建议 |

> 说明：P0 = 功能缺陷/安全（应尽快修）；P1 = 体验/可发现性；P2 = 新功能/增强。
> codex 对各问题给出的优先级与上表基本一致，差异点已在各节注明（如 7/11 codex 倾向 P1，本文按"新功能归 P2、但作为需求方明确要求可升 P1"处理）。

---

## P0 功能缺陷

### 1. 配额错误提示显示不全

- **问题描述**：配额不足时传输面板提示"配额不足：当前已用 709 MB / 总配额。。。"显示不全，需鼠标移上去显示完整。
- **现状分析**（根因在展示层，非 API/i18n）：
  - `web/src/views/FilesView.vue:34` 传输行错误状态 `<span :class="{ 'transfer-error-text': item.failed }">` **没有 `title` 属性**；
  - `web/src/styles.css:84` `.transfer-error-text { max-width: 150px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }` 固定 150px 截断省略；
  - `web/src/api.js:70-76` `localizeError` 对 `QUOTA_EXCEEDED` 已用 `usedBytes/quotaBytes/fileSize/short` 生成完整文案；
  - `web/src/i18n.js`（zh-CN:18 / zh-TW:34 / en:48）`error.quotaExceededDetail` 均为完整长文本。
- **解决范围**：纯前端。
- **复杂度**：S。
- **建议实现方式**：给失败状态 span 加 `:title="item.failed ? item.error || item.status : item.status"`，保留现有省略样式，桌面/移动端均可用原生悬浮提示。同一模板中名称 `<strong>` 已有 `title`（`item.relPath || item.file.name`），错误文本补上即可；可顺带检查其他失败态（下载行 L37）同型处理。
- **优先级**：P0（错误原因直接影响用户判断与处理方式）。

### 2. 传输记录刷新后清空

- **问题描述**：刷新页面后传输面板记录丢失，需保持当前会话的传输记录。
- **现状分析**：
  - `web/src/views/FilesView.vue:54` `uploads`/`downloads` 均为组件内存 `ref([])`；任务对象含 `File`、`Set(pending)`、`Map(controllers)`、`AbortController` 等**不可序列化**成员（L230 创建）；
  - `FilesView.vue:447` `onBeforeUnmount` 会 abort 所有上传/下载控制器——**刷新 = 卸载组件**，任务被中止且记录丢失；
  - 成功项短时保留：上传 2.2s（L260）、下载 4s（L370），面板本质是"活动/短暂记录"；
  - 后端已有断点续传基础：`GET /api/files/{taskID}/status` 返回已上传分片（`internal/httpapi/server.go:1720-1757`）；但后端任务超 24h 清理（`cmd/filebox/main.go` 后台清理），`taskId` 非永久有效。
- **解决范围**：基础记录恢复为**纯前端**；"刷新后自动续传"需前端重新取得文件对象，后端接口基本够用，不必立即改后端。
- **复杂度**：M。
- **建议实现方式**：
  - 用 `sessionStorage`（按用户隔离）保存**可序列化快照**：任务类型、文件名/相对路径、大小、进度、错误、`taskId`、状态时间；**不保存** `File`/`AbortController`/`Set`/`Map`/Promise/流；
  - 刷新恢复策略：已完成项按现有短时保留逻辑展示后移除；**失败/暂停/未完成上传**恢复为"需要重新选择文件"，用户重选同一文件（校验文件名/大小/哈希）后用原 `taskId` 调 `/status` 只补缺失分片；`taskId` 为空（未 init）只能作为历史记录；taskId 过期/被清理则降级为"任务已失效"；
  - 下载流无法靠 sessionStorage 续传（Blob/ReadableStream 刷新即失），保持仅恢复"下载记录展示"；
  - `api.js:153-158` `clearSession()` 目前只清 localStorage，持久化记录需在退出登录时一并清理，避免同浏览器换用户看到旧记录。
- **优先级**：P0（用户可感知的状态丢失；"刷新后继续传输"的交互边界需在实现时与需求方确认）。

### 4. 目录选择支持上级

- **问题描述**：选择目录时无法切换到更上级（如 `/tmp`）。
- **现状分析**：
  - `web/src/views/SyncView.vue:21` SFTP 弹窗"返回上级"按钮条件为 `picker.kind === 'sftp' && picker.path !== '.'`；`parentRemotePath`（L63）对无斜杠路径返回 `'.'`（home 根），**从 `.` 无法进入绝对根 `/`**，在 `/` 时按钮仍显示但点击无效；
  - filebox 弹窗只有"根目录"`choosePath('')` + 当前目录子文件夹（`fileboxFoldersAt` L42），**无逐级上级导航**；
  - 后端 `validateRemotePath`（`internal/httpapi/sync.go:398-413`）本身**允许绝对路径与 `/`**，问题在前端导航状态。
- **解决范围**：以前端为主，后端只需补路径归一化测试。
- **复杂度**：S。
- **建议实现方式**：
  - SFTP：保留 `.` 为用户 home 根；增加"文件系统根"入口 `browseRemote('/')`；上级按钮条件改为 `path !== '.' && path !== '/'`；归一化规则：`.`→无上级，`foo`→`.`，`foo/bar`→`foo`，`/tmp`→`/`，`/tmp/a`→`/tmp`，`/`→无上级；
  - filebox：toolbar 增加上级按钮（`a/b`→`a`，`a`→`''`，`''`→无上级）；导航与"选择此目录"是两个动作，根目录按钮保留。
- **优先级**：P0（阻止访问合法远端目录；与 3 的 picker 改造高度相关，建议合并处理）。

### 8. 分享次数用完重复提示

- **问题描述**：点击下载出现两条"分享次数已用完"。
- **现状分析**（根因确认：同一状态同时命中两个模板条件）：
  - `web/src/views/ShareView.vue:7` 模板同时渲染 `<p v-if="downloadExhausted" ...>{{ t('share.limitReached') }}</p>` 与 `<p v-if="downloadError" ...>{{ downloadError }}</p>` 两条 alert；
  - `downloadFile`（L31-64）：`downloadExhausted` 分支（L33）设置 `downloadError = share.limitReached` 后 return（L57 服务端 `SHARE_DOWNLOAD_LIMIT` 分支同样写 `downloadError` 并把 `downloadAvailable=false`）→ 两条同文案提示同时出现。
- **解决范围**：纯前端，后端无需改。
- **复杂度**：S。
- **建议实现方式**：统一单错误出口：`<p v-if="downloadExhausted || downloadError" class="alert error">{{ downloadExhausted ? t('share.limitReached') : downloadError }}</p>`；`downloadExhausted` 分支只 return 不再写 `downloadError`；`SHARE_DOWNLOAD_LIMIT` 分支设置 `downloadAvailable=false` 并清空普通错误；`loadMeta` 继续清理旧错误。补充"服务端返回限额错误"与"刷新后再次点击"两个回归用例。
- **优先级**：P0（明确用户可见缺陷）。

### 9. 收集上传配额不足提示泄露信息

- **问题描述**：外部人员通过收集链接上传配额不足时，看到"filebox-batch-download (1).zip: 配额不足：当前已用 709 MB / 总配额…"，应改为"配额不足，请联系链接提供方"。
- **现状分析**：
  - `internal/httpapi/collection.go:443-447` `collectionUploadInit` 配额错误经 `collectionFailure` 返回 `{"code":"QUOTA_EXCEEDED","usedBytes":…,"quotaBytes":…,"fileSize":…}` 给**匿名外部用户**；
  - `collection.go:312-320` `collectionFailure` → `writeErrorData` 原样返回 data；
  - 前端 `web/src/views/UploadView.vue:53` `error.value = \`${item.file.name}: ${err.message}\`` 拼接文件名+本地化错误；
  - `web/src/api.js:70-76` `localizeError` 对 `QUOTA_EXCEEDED` 专门读取并格式化配额明细。
- **解决范围**：前后端，**以后端为主**（外部用户可绕过前端直接调 API）。
- **复杂度**：S。
- **建议实现方式**：新增公共错误码 `COLLECTION_QUOTA_EXCEEDED`，匿名收集场景只返回 `{"code":"COLLECTION_QUOTA_EXCEEDED"}`，消息统一"配额不足，请联系链接提供方"，**不返回** `usedBytes/quotaBytes/fileSize`；登录用户普通上传保留现有 `QUOTA_EXCEEDED` 明细（不影响现有功能）；前端 `api.js` 增加新码映射 + `UploadView` 防御性兜底。文件名是否保留可与需求方确认（当前需求只要求隐藏配额明细）。
- **优先级**：P0（匿名接口泄露内部用户配额状态，已有明确外部可见异常）。

---

## P1 体验

### 3. 同步支持选择目录/文件

- **问题描述**：目前同步路径选择只能选目录，需支持选文件。
- **现状分析**：
  - 前端 filebox picker 只展示目录：`SyncView.vue:21` 遍历 `fileboxFoldersAt(picker.path)`；`fileboxFoldersAt`（L42）只筛目录；`loadAll`（L44）只请求 `/api/folders`；
  - SFTP browse 后端显式过滤非目录：`internal/httpapi/sync.go:302-336`（`if !entry.IsDir() { continue }`）；
  - **但执行引擎已部分支持精确文件路径**：`internal/store/sync.go:477-494` `ListReadyFilesUnder` 匹配 `relative == sourcePath`；push 时源路径为文件则用 basename（`internal/httpapi/sync.go:1015-1022`）；pull 用 `Stat` 区分文件/目录（sync.go:1149/1160/1292）；目标路径按目录处理（MkdirAll / EnsureFolderPath，sync.go:1000/1171-1175）。
- **解决范围**：前后端。
- **复杂度**：M。
- **建议实现方式（MVP 语义）**：源端允许"目录或单个文件"，**目标路径仍只表示目录**、目标文件名继承源 basename（如 `push: docs/readme.txt -> /backup` → `/backup/readme.txt`）；冲突继续复用 overwrite/skip/rename。改动点：
  - SFTP browse 增加 `includeFiles=true`，返回统一 `isDir`/`kind` 字段；前端目录 entry 进目录、文件 entry 在 source picker 直接选中；**target picker 仍只允许目录**；
  - FileBox picker 增加"当前目录直接子项"的文件列表接口（不建议直接复用递归式 `/api/files?dir=`：分页上限 100、不带目录路径、无法可靠建目录树）；文件 entry 至少含 `{id,name,path,size,isDir:false}`；
  - 源路径增加 `sourceKind: directory|file` 消歧（同一目录下可同时存在同名文件和目录，现有 `relative == sourcePath` 与 `sourcePath + "/"` 前缀会同时命中）；
  - 源文件在任务执行前已删除时应记录失败而非"0 files 后报成功"；
  - 目标为具体文件名的语义建议另立需求（`targetKind`）。
- **优先级**：P1（设计文档 `docs/design-sync.md:22` 已定义 FileBox 源可为"文件或目录"，属功能缺口）。

### 6. 新建同步任务 UI 高度不一致

- **问题描述**：新建同步任务表单中方向/目标系统/同名冲突/执行方式等字段高度与其他字段不一致（对照截图 ui-7）。
- **现状分析**：
  - `web/src/views/SyncView.vue:17` 任务表单：`taskName/sourcePath/targetPath/cron` 用 `<input>`，`direction/remoteSystem/conflictPolicy/scheduleType/cronPreset` 用 `<select>`，路径行是 `input + icon-button` 组合（`field-with-action`）；
  - `web/src/styles.css:44` 只给 `.form-label input` 设 `height: 40px`，**没有 `.form-label select` 高度规则**（`select` 仅 L4 字体继承）→ select 用浏览器原生高度，明显矮于 input；
  - `styles.css:19/34` `.icon-button` 及 `.field-with-action .icon-button` 固定 `34px` → 与 40px 输入框同一行时高度不一致；
  - `styles.css:41` 的 `select` 高度规则只覆盖语言选择器/设置控件，不匹配同步表单。
- **解决范围**：纯前端。
- **复杂度**：S。
- **建议实现方式**：统一表单控件基线 40px：`.form-label input:not([type='checkbox']), .form-label select { height: 40px; min-width: 0; padding: 0 10px; border: 1px solid #d4dee8; background: #fff; }`；同步表单内路径操作按钮单独设 40px（**不要**改全局工具栏 34px 规则）；顺带统一 `.sync-form` 内 checkbox 行（`check-label`）的垂直对齐。
- **优先级**：P1（视觉一致性问题）。
- **截图说明**：`ui-7.png` 尝试用图片工具识别未成功（describe-image 环境缺少 baseURL 配置、当前模型不支持图像输入），已回退为直接核对 SyncView.vue 表单模板与 styles.css 控件样式，结论一致（select 无高度样式是根因）。

### 10. 日志外部上传无法区分用户目录

- **问题描述**：日志中"外部上传成功"无法区分上传到哪个用户目录。
- **现状分析**：
  - 收集上传实际落在 `files/{owner.ID}/uploads/{token}`（`internal/httpapi/collection.go:385-396`）；
  - 秒传成功审计仅 `recordAudit(..., name, "success", "collection_upload")`，serviceEvent 仅 `name=%s size=%d`（collection.go:404-407）；
  - 普通上传完成同样只记文件名/大小（collection.go:598-613），owner 已加载但未进日志；
  - `audit_logs.target` 为无长度限制 TEXT（`internal/store/store.go:390-400`）；服务日志为自由文本（`internal/httpapi/server.go:3775-3786`）。
- **解决范围**：后端。
- **复杂度**：S（仅增强 serviceEvent）；若给审计表加结构化字段则 M。
- **建议实现方式**：先保持 `audit target=文件名` 不变（避免改变日志页显示与关键字语义），在两处成功路径（collection.go:404-407 秒传、598-613 普通完成）的 serviceEvent 追加 `owner=<username> owner_id=<id> collection_id=<id> directory=uploads/<脱敏token> name=<filename> size=<size>`；**不要把完整收集 token 写入日志**（token 是公开凭据，可记 token 前缀/哈希或仅 collection_id）。长期方案：`audit_logs` 增加 `owner_id/collection_id` 字段 + 迁移 + API/前端展示（升 M）。
- **优先级**：P1（运维审计可追溯性）。

### 11. 我的收集支持编辑

- **问题描述**：收集创建后应可编辑（到期时间、上传数量、单文件大小上限等）。
- **现状分析**：
  - 后端仅有 `createCollection`（collection.go:49-84）/`listCollections`（89-104）/`getCollection`（108-135）/`deleteCollection`（165-188），**无 PUT**；路由仅注册 POST/GET/GET-files/DELETE（`internal/httpapi/server.go:298-302`）；
  - store 层有创建/查询/列表/撤销方法，无更新方法（`internal/store/store.go` CreateUploadCollection 等）；
  - 前端 FilesView.vue:14 收集区块只有 `viewCollection`/`revokeCollection`，创建弹窗 L195-202 无编辑模式；
  - 注意点：`uploadCount` 在 upload-init 时事务性预占递增（store.go CreateCollectionUploadTask），失败任务清理不回退计数（`DeleteUploadTask` 不减少 upload_count）→ 下调 `maxUploads` 必须保证 `newMax >= uploadCount`；完成阶段不再校验 `max_file_bytes`（store.go CompleteCollectionFile 只查 revoked/expired）→ 下调 `maxFileBytes` 后完成阶段需重校验。
- **解决范围**：前后端。
- **复杂度**：M。
- **建议实现方式**：`PUT /api/collections/{id}`（body：`expiresAt` 绝对时间 / `maxUploads` / `maxFileBytes`，`0`=不限）；store 原子更新 + 条件校验（有效期合法且非过去、有限值不低于当前 uploadCount、撤销后默认不可编辑、`rejectReadOnly`、普通用户仅自己的收集，管理员权限与现有查询/撤销对齐）；前端收集行加编辑图标，复用创建弹窗加 `editingCollection` 状态，编辑模式显示 datetime-local、提交转 UTC，保存后刷新；新增 `collection_update` 审计动作并登记。**须与业务确认**：到期收集能否编辑重新激活？撤销后是否永久不可编辑？maxUploads 能否下调？已初始化未完成的上传是否受新 maxFileBytes 影响？管理员能否编辑他人收集？
- **优先级**：P1（收集功能的管理能力缺口）。

### 12. 我的收集入口到菜单栏

- **问题描述**：与分享管理一样，收集入口应放到顶部菜单栏。
- **现状分析**：收集目前只是 FilesView 页面内嵌区块（`FilesView.vue:14` `collection-section`，入口即文件页本身）；所有视图顶栏（FilesView:5 / SharesView:5 / LogsView:3 / AdminView:3 / SyncView:5）均无"收集"菜单项；路由无 `/collections`。
- **解决范围**：纯前端。
- **复杂度**：S~M。
- **建议实现方式**：与 13/14 一并抽出共享顶栏组件，新增"收集"菜单项；落地两种可选：a) 顶栏链接锚定/跳转到文件页收集区块（最省，S）；b) 新增 `/collections` 独立视图（把 FilesView 收集区块迁出，M，后续收集编辑/聚合下载更好扩展）。建议先 a 后视需要升级 b。
- **优先级**：P1（功能可发现性）。

### 13. 管理后台隐藏问题

- **问题描述**：点击管理后台不应将分享管理等置为不可见。
- **现状分析**：各视图顶栏链接集不一致——`AdminView.vue:3` 只有 文件/日志（**无分享、无同步、无收集**）；`SharesView.vue:5` 有 文件/日志/管理后台（无同步）；`LogsView.vue:3` 有 文件/分享/管理后台（无同步）；`SyncView.vue:5` 有 文件/分享/日志/管理后台；`FilesView.vue:5` 有 管理后台/分享/日志。路由本身都在（`router.js:17-21`），是**导航配置缺失**而非权限缺失（路由守卫 admin 校验在 router.js:48，顶栏隐藏不能替代权限校验）。
- **解决范围**：纯前端。
- **复杂度**：M（涉及多视图、登出逻辑与 FilesView 传输按钮例外）。
- **建议实现方式**：抽共享 `AuthenticatedTopbar` 组件统一含：文件/分享/同步/日志/收集/管理后台（仅 admin）/语言切换/改密/退出；通过 props/slot 保留 FilesView 传输按钮与角标等差异；各页重复 `logout()` 一并收敛；检查中等宽度下顶栏溢出（响应式 800px/500px）。
- **优先级**：P1（管理后台用户无法发现分享/同步入口）。

### 14. 同步独立菜单

- **问题描述**：同步应作为独立菜单（与文件/分享/收集同级别），而非仅图标/单独一行。
- **现状分析**：同步入口仅在 `FilesView.vue:7` 的 `nav.sync-entry-link` 单独一行（顶栏之外，`styles.css:88` 为其设独立行布局）；其他视图顶栏完全无 `/sync` 入口。
- **解决范围**：纯前端。
- **复杂度**：S（并入 13 的共享顶栏后为同一改造）。
- **建议实现方式**：`/sync` 纳入共享顶栏菜单（覆盖所有登录后视图），删除 FilesView:7 独立导航与 `.sync-entry-link` 样式，避免双入口；13+14 建议作为一次顶栏重构统一处理。
- **优先级**：P1（其他页面完全不可发现同步功能）。

---

## P2 新功能

### 5. 同步到另一套 filebox

- **问题描述**：同步任务需支持 push/pull 到另一套 filebox 实例（设置对方账号密码）。
- **现状分析**：
  - 方向校验写死（`internal/httpapi/sync.go:417`）：push 必须 `source=filebox,target=sftp`；pull 必须 `source=sftp,target=filebox`；执行分发（sync.go:909-914）与 browse/mkdir（305/345）均固定 `openSFTP`；
  - `RemoteSystem` 完全是 SFTP 模型（host/port/username/authType/authSecret/authPassphrase/hostKeyFingerprint，`internal/store/sync.go:19-29`）；
  - **任务表枚举已允许** `filebox/filebox`（`sync_tasks` 表 `CHECK(source_type IN ('filebox','sftp'))`，store/sync.go:94-96）→ 只需放开 Go 校验，无需任务表迁移；
  - 前端把方向写死（`SyncView.vue:40/50`）；远端 FileBox 的 HTTP API 已具备登录/upload-init/upload-chunk/upload-complete/download/文件列表/目录接口（server.go:262-313）。
- **解决范围**：前后端 + 数据库迁移（remote_systems 表）。
- **复杂度**：L（新增一种完整传输协议）。
- **建议实现方式（MVP）**：
  - 数据模型：保留 SFTP 字段，新增 `kind: sftp|filebox` 与 `url`（FileBox 基础 URL）；旧记录迁移为 `kind='sftp'`；`auth_secret` 沿用现有 AES-GCM 加密（sync.go:746-763）；FileBox 仅密码认证，`auth_passphrase`/fingerprint 不适用；
  - 方向矩阵：push=本地 filebox→远端 filebox，pull=远端 filebox→本地 filebox，`sourceType`/`targetType` 均 filebox，`remoteSystemId` 指向对方实例；
  - FileBox adapter：任务执行时用保存的账号密码调对方 `/api/auth/login`（token 仅存内存），push 走远端 upload-init/chunk/complete（含秒传与冲突策略映射），pull 走远端 download→本地临时文件→复用现有入库流程；目录创建用远端目录 API；任务结束释放 token；新增远端专用 browse API（`GET /api/sync/browse?path=...&includeFiles=true` 返回直接子项——现有 `/api/files?dir=` 是递归前缀且分页上限 100，不能可靠建目录树）；
  - MVP 明确不支持：远端 TOTP（登录会返回 challenge 而非 token，`server.go:864-871`）、目标路径为具体文件、镜像删除、任务级断点续传、API token；
  - 远端安全边界：disabled 账号登录拒绝（server.go:833）、mustChangePassword 拦截（3799）、IP ACL（3803）、远端 read-only 会拒 push（upload-init 1356/1473/1896）、远端配额由对方 upload-init 检查、限速叠加（本地 push 用本地上传限速 sync.go:1011 + 对方账号限速 server.go:1511）、**URL SSRF 防护**（仅 http/https、禁内嵌账号密码、设连接/请求超时、不记录 token/密码）；
  - 完整版增量：统一 adapter 接口、API token/服务账号、远端能力/版本探测、任务级断点重试、独立同步限速、跨实例测试。
- **优先级**：P2（FileBox↔SFTP 已可用；若产品明确要求多套 FileBox 互备则升 P1）。建议实施顺序：4 → 3 → 5。

### 7. 批量分享统一链接

- **问题描述**：批量分享应生成一个统一分享链接（而非每文件单独链接），支持外部用户查看文件列表、单文件下载、批量选中下载。
- **现状分析**：
  - 前端 `FilesView.vue:77-95` `createBatchShare` 调 `POST /api/files/batch-share`，按返回 `items` **逐文件**展示各自 url；
  - 后端 `internal/httpapi/server.go:2177-2276` `batchShare` 整批校验后**逐文件** `CreateShare` 生成独立 token/url（L2248-2272）；非事务，中途失败可能留下部分分享记录；
  - `shares` 表一行一文件（`internal/store/store.go:489-503`），无聚合实体；公开页 `ShareView.vue:7` 假定单文件元数据（meta.fileName/单下载按钮）；
  - 批量下载 `POST /api/files/batch-download`（server.go:2104）目前**仅登录用户**可用。
- **解决范围**：前后端。
- **复杂度**：L。
- **建议实现方式**：**新增独立模型**而非给 shares 加 group_id（保留单文件分享兼容）：
  - `share_groups(id, token, created_by, expires_at, download_count, max_downloads, revoked_at, created_at)` + `share_group_files(group_id, file_id, display_order, created_at)`；
  - API：`POST /api/files/batch-share-group`（事务内去重≤500、整批校验 ready+归属、任一失败整体回滚）、`GET /api/files/shared-groups/{token}/meta`、`GET .../download/{itemID}`、`POST .../batch-download`（匿名 ZIP）；
  - 路由：公开聚合页用 **`/g/:token`** 新增 `BatchShareView.vue`，与 `/:token` 单文件页、`/u/:token` 收集页区分（router.js:21-23）；聚合页支持列表/单文件下载/全选/选中 ZIP 下载/失效与撤销与次数耗尽状态；ZIP 命名冲突处理可复用现有批量下载逻辑；
  - 分享管理页（SharesView）增加 group 类型展示：文件数量与列表、有效期、已用/最大次数、撤销/延期/增次、下载日志；
  - **须与业务确认**：次数按聚合整体计还是每文件计？单文件下载消耗 1 次、ZIP 多文件消耗 1 次还是 N 次？文件被删后隐藏还是使链接失效？成员创建后是否可增删文件？是否要预览？默认建议：整体计数、单文件 1 次、ZIP 1 次、成员不可变、已删文件隐藏（仍需业务确认）。
- **优先级**：P2（新功能；需求方明确要求时可按 P1 排期）。公开页错误处理可复用 8 的修复经验。

### 15. 本地上传速度受限分析（分析类）

- **问题描述**：本地验证测试上传速度受限较低，需分析是否存在传输速率瓶颈。
- **现状分析（逐链路结论）**：
  1. **上传限速配置**：默认**不限速**——新库默认 `uploadRateLimit="0"`（`internal/store/store.go:596`、GetLogSettings 默认 0）；`limiterFor` 在 `bytesPerSec<=0` 时返回 nil（`internal/httpapi/server.go:1575-1608`）。管理入口在 AdminView `system` 页签（`AdminView.vue:34-35`，加载 L81 / 保存 L90，前端单位 KB/s×1024=bytes/s）。注意 `INSERT OR IGNORE` 不覆盖已有库，老库需后台确认。→ 非默认瓶颈，但**若后台配了非零值则严格限速**（token bucket，burst=max(限速,1MiB)，`server.go:1596-1603`）。
  2. **分片大小**：>8MiB 文件固定 4MiB（`FilesView.vue:252`）；服务端仅接受 2-8MiB（`server.go:1304-1315`），≤8MiB 文件整文件单分片。4MiB 合法，但对本地高速 SSD 偏小（请求数/打开次数/SQLite 提交数约为 8MiB 的两倍）。
  3. **并发度**：FilesView 全局 4 worker、每 worker 一次一片（L234-236）；同时最多 3 个文件进入流程；**公共收集上传严格串行**（`UploadView.vue:53-55`）；同步走后端 SFTP 不占浏览器链路。4 并发本地通常够用（HDD 上加并发反而因寻道变慢），NVMe 高速环境需 1/2/4/8 实测。
  4. **分片写入 + SQLite（主要可疑瓶颈之一）**：每片 = 读任务+读设置+io.Copy 写临时文件算 SHA-256+`SetChunk` 单条 autocommit（`server.go:1483-1569`；`store.go:SetChunk` 无显式事务）；SQLite 仅 `busy_timeout(5000)`（store.go:301）且 **`SetMaxOpenConns(1)` 单连接**（store.go:305），未设 WAL/`synchronous` → 4 路并行写盘但元数据提交全部串行排队，每 4MiB 一次提交。
  5. **HTTP 超时**：ReadHeaderTimeout 10s / IdleTimeout 120s / ReadTimeout 0 / WriteTimeout 0（`cmd/filebox/main.go`）→ 不会截断持续传输的上传，非瓶颈。
  6. **SSE 进度**：每秒推送一次并每次查 pending 任务（`server.go:1735` 起、store.go ListPendingTaskProgress），不读文件数据；因 SQLite 单连接会与分片提交**短暂竞争**，通常可忽略、高分片速率时可能可见。
  7. **前端前置计算**：`startUpload` 先算整文件 SHA-256（`FilesView.vue:247-252`，api.js 按 8MiB 块读）→ "选文件到首个 PUT" 之间有一次整文件读+哈希，**明显拖慢端到端耗时但拖慢的是"校验中"阶段而非持续吞吐**；速率采样每秒 + 3s 滑动平均（FilesView.vue:150-161），4MiB 分片下显示呈阶梯状，非实时网络速率；80ms 轮询可忽略。
  8. **完成阶段（端到端主要瓶颈）**：complete 时逐片重读写入 `.merged`、**双哈希 SHA-256+MD5**、`merged.Sync()` fsync、rename（server.go:1946-2018；收集同构 collection.go:666-687）→ 大文件多一次整文件读写 + 一次物理落盘同步；代码用标准 `io.Copy` 无自定义缓冲（可测 `io.CopyBuffer` 256KiB-1MiB，但解决不了 SQLite 提交与 fsync）。
- **瓶颈排序**：① 完成阶段整文件合并+双哈希+fsync；② PUT 阶段每片一次 SQLite autocommit + 单连接串行；③ 前端整文件 SHA-256 预计算（首包延迟）；④ 4MiB×4 组合在 NVMe 高速环境可能限制吞吐；⑤ 存储设备/杀软/磁盘缓存/CPU 哈希；⑥ 条件性：后台限速非零；⑦ 可忽略：HTTP 超时、80ms 轮询、每秒采样、SSE。
- **建议**：优先实测验证（用不命中秒传的唯一大文件，记录 T0 选文件/T1 前端哈希完成/T2 首个 PUT/T3 末个 PUT/T4 complete；对比限速 0 vs 非零、4MiB vs 8MiB、并发 1/2/4/8、SSE 开关、T0-T1/T1-T3/T3-T4 三段耗时；观察浏览器 Network waterfall、进程 CPU、磁盘 active time、SQLite 等待）。按结果定向优化：T3-T4 长→合并与 fsync（如合并时对已校验分片跳过双哈希或延迟 Sync）；PUT 排队→SQLite 开 WAL + `synchronous=NORMAL`（接受断电持久性权衡）、合并分片元数据批量提交、减少每片重复读任务/设置；T0-T1 占比高→优化哈希前置（如并行/分块流式）而非传输链路；NVMe 环境→分片提到 8MiB、并发按实测调。默认配置无上限，先按上述测量排除环境因素再动手。
- **解决范围**：分析产出（本文档结论）；代码优化若实施为可选增强。
- **复杂度**：M（测量+针对性优化）。
- **优先级**：P2。

---

## 合并与实施顺序建议

1. **顶栏重构（12/13/14）**：抽 `AuthenticatedTopbar` 共享组件，统一链接集（文件/分享/同步/日志/收集/管理后台(admin)/语言/改密/退出），删 FilesView:7 独立同步入口；收集菜单先锚定文件页区块（S），后续可升级独立视图。
2. **同步 picker 改造（3+4）**：同一路径选择弹窗内实现"上级/根目录导航（4）"与"源端文件选择（3）"，顺带统一 browse API（includeFiles/kind）；5 依赖其路径语义，排在其后。
3. **collection.go 同批修（9+10）**：配额错误脱敏（9）+ 外部上传日志带 owner/目录（10），同一文件同一批。
4. **FilesView 传输面板（1+2）**：错误提示 title（1）与 sessionStorage 恢复（2）都在传输面板，可同批。
5. **ShareView 错误出口（8）** 独立小修，聚合分享公开页（7）可复用其经验。
6. **建议实施顺序**：8 → 1 → 9 → 4 → 3 → 6 → 10 → 2 → 11 → 12/13/14 → 7 → 5 → 15（15 为测量先行、按需优化）。

## 待父任务确认的关键业务规则

- 2：刷新恢复后"未完成任务"是否接受"重新选文件后续传"的交互？
- 7：聚合分享次数计数口径（整体 vs 每文件；ZIP 消耗次数）；成员文件是否可增删；已删文件展示策略。
- 11：到期/撤销收集可否编辑；maxUploads 是否允许下调；管理员可否编辑他人收集；下调 maxFileBytes 对 pending 任务的影响。
- 15：是否投入 SQLite WAL/synchronous 等持久性权衡类优化。

---

**结尾声明**：本文档为分析产出（codex CLI 只读分析 + 人工源码核对，未修改任何功能代码、未执行 git 操作），所列文件/行号以 `HEAD 1ad65a0` 为准；待父任务确认后由后续修复子任务按本文档执行。
