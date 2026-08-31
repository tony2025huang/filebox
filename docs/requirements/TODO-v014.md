# FileBox v014 需求/问题 解决范围分析（TODO）

- **基线**：`HEAD 2d09454`（v013 修复批次二之后）
- **分析方式**：codex CLI（`codex exec -C <repo> -s danger-full-access`，只读）分主题分析 + 人工源码核对。说明：第一批 7 主题并行批次因 codex 内部嵌套 PowerShell（CLR/ScriptDebugger 初始化失败）在产出最终结论前被截断（仅留下中间证据）；随后对最复杂的 #4/#6 以强化提示（禁止全仓库扫描、要求最终结论收尾）重跑成功，其余 5 项由人工直接核对源码完成，并与 codex 中间证据、`dist/` 内嵌 web 包（用户实际运行的构建）交叉验证。全部结论均落到真实文件与行号。
- **约束**：本文档仅为**分析产出**，本轮未修改任何功能代码、未做任何 git 操作。
- **状态**：待父任务确认后，由后续修复子任务按本文档执行。

---

## 总览表

| # | 问题 | 解决范围 | 复杂度 | 优先级 | 合并建议 |
|---|------|---------|--------|--------|---------|
| 1 | 管理后台概览页文案（标题/描述复用） | 纯前端 | S~M | P1 | 独立批次（AdminView） |
| 2 | 导航"同步"改"同步任务" | 纯前端 | S | P1 | 与 7 同在顶栏，可同批 |
| 3 | filebox 目标系统无法填写账号密码 | 纯前端（后端已就绪） | S | P0 | 与 4/5 同批（同步模块） |
| 4a | push 单文件选择现状核对 | 结论说明（HEAD 已支持） | — | P1（回归验证） | 并入 4 |
| 4b | 源/目标"根目录"无法保存且无提示 | 纯前端 | S | P0 | 与 3/5 同批（同步表单） |
| 5 | 目标系统连通性测试 | 前后端 | M | P2（需求方明确要求可升 P1） | 与 3 同批（系统管理） |
| 6 | 聚合分享不支持编辑（延期/增次） | 前后端 | M | P1 | 与 7 同批（管理页增强） |
| 7 | "我的收集"独立页面 | 纯前端 | M | P1 | 与 6/2 同批 |

> 说明：P0 = 功能缺陷（应尽快修）；P1 = 体验/对齐；P2 = 新功能/增强。
> 4a 不是缺陷——HEAD 上源端单文件选择已实现（v013 #3），本文档给出证据与回归建议；若产品要求"目标端也选具体文件"则需新设计 `targetKind`（L），见 4a 小节。

---

## P0 功能缺陷

### 3. 新建目标系统（remote_system）类型选 filebox 时，表单没有账号/密码字段

- **问题描述**：新建目标系统，类型选 filebox 后只有 URL 与用户名，**没有密码字段**；后端要求创建时必填凭据，导致 filebox 类型目标系统无法正常创建，filebox↔filebox 同步（v013 #5）无法配置。
- **现状根因**：
  - 前端：`web/src/views/SyncView.vue:16` 系统表单 filebox 分支只渲染 `<input v-model="systemForm.url">` 与 `<input v-model="systemForm.username">`，**没有 `authSecret`（密码）输入框**；对照 SFTP 分支（同 L16）有密码/key textarea（`v-model="systemForm.authSecret"`）；`systemForm` 定义（L35）与 `saveSystem`（L68）均已携带 `authSecret`，只缺渲染。
  - 后端已完全就绪，无需改动：`internal/httpapi/sync.go:100-170` `validateSyncSystemInput` 对 filebox 要求 `AuthType==password`、合法 http(s) URL（SSRF 防护：禁内嵌账号密码、校验 scheme/host/userinfo，L120-134）、用户名必填（L117）；`createSyncSystem`（L172-207）以 `requireSecret=true` 调用校验（L182）→ 无 `authSecret` 直接 400"目标系统参数无效"；凭据经 `s.encryptSyncSecret`（AES-GCM，L187-198）加密入库，`openFileBox`（`internal/httpapi/sync_filebox.go:45-86`）解密后调对方 `/api/auth/login`（token 仅存内存）。
  - 编辑路径：`updateSyncSystem`（sync.go:224-295）用 `__keep__` 哨兵保留原凭据（L248-250），前端 `openSystemEdit`（SyncView.vue:66）已把 `authSecret` 清空以便"留空不改"，新密码框接入即可。
- **解决范围**：纯前端（后端与数据库零改动）。
- **复杂度**：S。
- **建议实现方式**：在 `SyncView.vue:16` 的 filebox 分支（url、username 之后）增加密码字段：`<label class="form-label">{{ t('sync.password') }}<input v-model="systemForm.authSecret" type="password" :required="!systemEditing" autocomplete="new-password" /></label>`（复用现有 `sync.password` i18n 键与 `authSecret` 加密链路；编辑模式不强制、留空保持原凭据）。顺带在目标系统表格（L11）为 filebox 行增加"是否已配置凭据"提示（`hasCredentials` 后端已返回，`publicSyncSystem` sync.go:77），并在 filebox 分支复用 `sync.credentialsHint` 说明文案。回归：创建 filebox 系统（成功/缺密码 400）、编辑不改密码保留旧凭据、改密码后旧密码失效。
- **优先级**：P0（功能缺陷——filebox 类型目标系统当前无法创建成功）。

### 4. 同步任务源/目标"根目录"无法保存，且没有任何提示（含 4a 单文件现状结论）

#### 4b 根目录无法保存且无提示

- **问题描述**：push 源选择"根目录"、pull 目标选择"根目录"保存失败，且**没有应用内提示**。
- **现状根因**（前端校验阻断，非后端）：
  - 根目录在任务表单中表示为**空字符串**：FileBox 根 = `''`（picker"根目录"按钮 `choosePath('')`，SyncView.vue:18/108）；远端路径空值后端归一为 `"."`（`internal/httpapi/sync.go:515-519`）。
  - 前端表单两个路径输入框带原生 `required`：`SyncView.vue:14` `<input v-model="taskForm.sourcePath" required />` 与 `<input v-model="taskForm.targetPath" required />` → 提交时浏览器原生 HTML5 校验拦截空值，**`submit` 事件不触发**、`saveTask()`（L61）不执行、请求根本不会发出；`formError`（L61 catch、L14 `<p v-if="formError">`）永远不显示 → 用户只看到浏览器原生气泡（"请填写此字段/Please fill out this field"，且样式与业务提示无关），表现为"无法保存且无提示"。
  - 后端**允许**根目录：`validateFileBoxSyncPath(value, allowEmpty=true)` 空值放行（sync.go:487-493），任务校验按 `allowEmpty=true` 调用（sync.go:557-568）；远端空值 → `"."`；任务表仅 `NOT NULL` 不禁止空串（`internal/store/sync.go:91-100`）。**后端与数据库不是根因**。
- **解决范围**：纯前端。
- **复杂度**：S。
- **建议实现方式**（`web/src/views/SyncView.vue`）：
  - 移除 `sourcePath`/`targetPath` 的无条件 `required`（L14）；
  - `saveTask()`（L61）增加显式校验：仅当**非 FileBox 端点**（SFTP）且路径为空时报错（远端空 → `"."` 为用户 home，若产品不接受 home 作根则提示），FileBox 端点空串视为合法根目录（列表/详情已用 `path || t('sync.root')` 展示，L9/L19）；
  - 校验失败直接写 `formError`，保证应用内明确提示（可复用 `sync.root` 文案作 placeholder）；
  - 建议远端 `.` 保持 `.` 存储，不要在前端统一转空串，避免语义混淆；FileBox 根继续用 `''`。
- **优先级**：P0（明确用户可见的功能缺陷）。

#### 4a push"推送到远端"是否支持选择具体文件（现状核对结论）

- **结论**：HEAD 上**源端单文件选择已实现**（v013 #3），"push 不支持选文件"在 HEAD/dist 上不可复现；需先确认用户实际运行构建（`dist/` 内嵌包含 `includeFiles`/`sourceKind`/`browse-filebox` 字符串，即运行构建已含 v013 同步改造）。逐场景证据：
  - 前端：`SyncView.vue:74-79` `openPathPicker` 对源端（`includeFiles = target === 'source'`）加载文件；`browseLocal`（L87）与 `browseRemote`（L86）传 `includeFiles=1`；`chooseEntry`（L107）选文件并置 `sourceKind='file'`；picker 模板（L18）渲染文件条目。
  - 后端：`browseSyncSystem`（`internal/httpapi/sync.go:336-358`）与 `browseRemoteEntries`（L362-391，SFTP 按 `includeFiles` 过滤、filebox 走 `client.browse(ctx,path,includeFiles)`）、`browseLocalFileBox`（L400-439，`ListDirectChildFiles` 返回文件）；任务校验接受 `sourceKind=file`（sync.go:553-555）；执行：SFTP push 单文件精确匹配（sync.go:1137-1153）、SFTP pull 用 `Stat` 区分文件/目录（sync.go:1306-1319、1449-1452）、FileBox push/pull 单文件（`internal/httpapi/sync_filebox.go:358-368`、390-392、543-576）。
  - 边界：**目标是目录语义**——push 的目标端（远端 SFTP 或远端 FileBox）picker 不展示文件（L75 `includeFiles` 仅源端为 true），执行器对目标无条件 `MkdirAll`/`ensureDir`。若产品要求"目标端也选择具体文件"，需新增 `targetKind: directory|file`（数据模型+校验+两套执行逻辑），复杂度 L，默认不做（另立需求）。
- **建议**：P1——补回归测试（push FileBox 源单文件、pull SFTP 源单文件、源文件删除后报失败而非"0 files 成功"）；若用户仍反馈不可用，先核对运行构建版本再排查。

---

## P1 体验 / 对齐

### 1. 管理后台"概览"页文案问题

- **问题描述**：管理后台"概览"页全部显示"工作区管理 管理账号、分配空间，并查看当前存储概况"。
- **现状根因**（HEAD 逐行核对 + `dist/` 运行构建交叉验证）：
  - 该文案是**整页共享页眉**：`web/src/views/AdminView.vue:9` `<h1>{{ t('admin.heading') }}</h1><p class="muted">{{ t('admin.copy') }}</p>` 位于 `activeTab` 各 `v-show` 区块**之外** → **所有管理页签（概览/用户/安全/品牌/锁定/系统）顶部都显示同一标题"工作区管理"+ 同一描述"管理账号、分配空间，并查看当前存储概况。"**，与当前页签内容不匹配（i18n 三语定义：`web/src/i18n.js:15`(zh-CN)/31(zh-TW)/45(en) `admin.heading`/`admin.copy`）。
  - 概览页**统计卡片标签本身是独立键**：L13 `stats-grid` 各 `stat-block` 分别用 `admin.userCount`/`admin.fileCount`/`admin.usedSpace`/`admin.shares`/`admin.diskUsage`（i18n 值：用户数/文件数/已用空间/分享链接/磁盘占用，互不相同）；系统语言面板 L14 用 `admin.systemLanguage`/`admin.systemLanguageCopy`。`dist/` 内嵌包中每个 admin 键出现 4 次（3 语言字典 + 1 次模板引用），证实**运行构建的统计卡片标签各不相同，"全部区块同文案"在 HEAD/dist 无法复现**。
  - 结论：若用户所见确为"每个卡片都显示该文案"，应属更旧构建（需确认部署版本）；按 HEAD 最接近的描述是"**每个管理页签顶部共享同一页眉文案**"（切页不更新标题/描述），属标题/描述错误复用。
- **解决范围**：纯前端。
- **复杂度**：S~M。
- **建议实现方式**：把 `AdminView.vue:9` 的共享页眉改为**按页签区分**——页眉移入各 `v-show` 区块（或按 `activeTab` 计算键名），新增每页签专属 i18n 键（如 `admin.overviewEyebrow/Heading/Copy`、`admin.usersEyebrow/Heading/Copy`、`admin.security...`、`admin.brand...`、`admin.locks...`、`admin.system...`，三语补齐；概览页眉可保留"工作区管理"语义，其余页签用各自标题，如 用户管理/安全设置/品牌设置/锁定管理/系统设置）；统计卡片如需更细描述可再为每卡片加 `muted` 行。若只是修复"同一文案"，最小改动为删除共享 copy、改为各页签独立 heading+copy。
- **优先级**：P1（文案/标题体验；HEAD 上不构成数据或功能错误）。

### 2. 导航栏"同步"改"同步任务"

- **现状根因**：顶栏菜单项 `web/src/components/AuthenticatedTopbar.vue:9` `<RouterLink to="/sync">{{ t('nav.sync') }}</RouterLink>`；`nav.sync` 三语值为 `web/src/i18n.js:180`（zh-CN `'同步'`）、`183`（zh-TW `'同步'`）、`186`（en `'Sync'`）。全仓库仅此一处引用 `nav.sync`（SyncView 页内分区标题用的是 `sync.tasks`='同步任务'，互不冲突）。
- **解决范围**：纯前端。
- **复杂度**：S。
- **建议实现方式**：改 `nav.sync` 三语值 → zh-CN `'同步任务'`、zh-TW `'同步任務'`、en `'Sync tasks'`（与 `sync.tasks` 措辞对齐）；仅影响顶栏菜单，无路由/逻辑改动。
- **优先级**：P1。

### 6. 聚合分享（/g/:token）不支持编辑（延期/增次），与单文件分享能力不一致

- **问题描述**：聚合分享创建后只能在分享管理页复制/打开/撤销，**没有延期/增次**能力。
- **现状根因**：
  - 后端 HTTP 层：`internal/httpapi/share_group.go` 仅有创建（L53-111 `createBatchShareGroup`）、列表（L115-128）、公开 meta/下载（L132-259）、ZIP（L263-401）、撤销（L405-424 `revokeShareGroup`），**无 extend/increase handler**；路由 `internal/httpapi/server.go:283-288` 只有 `POST /api/files/batch-share-group`、`GET /api/shares/groups`、`GET /api/shared-groups/{token}/meta|download/{fileID}`、`POST /api/shared-groups/{token}/batch-download`、`DELETE /api/shared-groups/{token}`。
  - store 层：`internal/store/share_group.go` `share_groups` 表**已含所需字段**（`expires_at`/`download_count`/`max_downloads`/`revoked_at`，L44-65），但只有 Create（L79）/Get（L143-155）/List（L159）/Revoke（L210）/Increment（L230），**无更新 expires_at / max_downloads 的方法** → **无需数据库迁移**。
  - 单文件分享基线（对齐参照）：路由 `PUT /api/shares/{token}/extend`、`PUT /api/shares/{token}/increase`（server.go:301-302）；`extendShare`（server.go:2594-2626，`expiresInHours` 1..87600，`UpdateShareExpiry` 保留更晚原截止时间）+ `increaseShare`（server.go:2630-2664，`maxDownloads` 0..100000、有限→更高或 0=不限，拒绝降低）；store `UpdateShareExpiry`（store.go:1931-1946，`CASE WHEN expires_at > ? THEN expires_at ELSE ? END`）+ `UpdateShareMaxDownloads`（store.go:1950-1968，原子 UPDATE 带守卫条件，失败区分 ErrNotFound/ErrConflict）；权限 `managedShare`（server.go:2548-2554 附近，创建者或管理员）。
  - 前端：`web/src/views/SharesView.vue:8` 聚合分组行操作只有 复制/打开(`/g/:token`)/撤销；单文件行有详情弹窗内 延期/增次 表单（L36-40）。`BatchShareView.vue`（/g/:token 公开页，L46-91）只做展示与下载，**不应**加编辑（匿名权限风险）。
- **解决范围**：前后端（无数据库迁移）。
- **复杂度**：M（后端 S：2 store 方法 + 2 handler + 2 路由；前端 M：分组详情/编辑弹窗）。
- **建议实现方式**：
  - 后端：新增 `PUT /api/shared-groups/{token}/extend`（body `{expiresInHours}` 1..87600）与 `PUT /api/shared-groups/{token}/increase`（body `{maxDownloads}` 0..100000，规则对齐单文件：`maxDownloads==0`（不限）不可再增、有限必须提升或置 0）；新增 `managedShareGroup` helper（`GetShareGroupByTokenIncludingRevoked` + 创建者或管理员校验，未命中返回 404 不泄露状态）；新增 store `UpdateShareGroupExpiry` / `UpdateShareGroupMaxDownloads`（单条原子 UPDATE，条件 `created_by=owner AND revoked_at IS NULL`，延期用 CASE 不缩短原截止、增次带守卫条件，失败区分 ErrNotFound/ErrConflict）；handler 顺序：requireAuth → 归属校验 → rejectReadOnly → 参数校验 → store → 返回 `publicShareGroup`；审计 `share_group_extend`/`share_group_increase`（target=token）+ serviceEvent。
  - 状态语义对齐单文件：revoked 不可编辑（store 条件拒绝）；expired 允许 extend 重新激活、increase 可改次数；limit_reached 允许 extend/increase 恢复。
  - 前端：`SharesView.vue:8` 分组行"复制"前加详情按钮（Eye），新增分组详情弹窗（复用单文件详情弹窗布局：token/文件数/次数/有效期/剩余/状态/链接），内含 延期、增次 两个表单（与 L36-42 同款），成功后更新详情与 `groups` 列表（参考 `replaceShare` L42）；revoked 行禁用编辑按钮。`api.js` 无需新封装（通用 `api()` 已带 token/错误本地化；后端错误文案可复用 `shares.extendHours/increaseMax` 与现有 error 映射，若加聚合专用错误码再补映射）。
- **优先级**：P1（与单文件分享能力对齐；若产品验收要求一致则升 P0）。

### 7. "我的收集"跳转问题：改为独立页面管理

- **问题描述**：点击顶栏"我的收集"跳到文件库页内锚点区块（`/#collections` 滚动定位），预期是与分享管理类似的独立页面。
- **现状根因**：
  - 顶栏入口：`web/src/components/AuthenticatedTopbar.vue:11` `<RouterLink :to="{ path: '/', hash: '#collections' }">{{ t('nav.collections') }}</RouterLink>` → 跳转 `/` 并锚定 FilesView 内嵌区块；`FilesView.vue:14` `<section id="collections" class="collection-section">`，L434 `scrollToCollections()` 滚动定位（v013 #12 的最小方案）。
  - 路由：`web/src/router.js:15-27` 无 `/collections` 路由。
  - 收集管理功能与 API 已完整（v013 #11 已实现编辑 PUT）：FilesView 内嵌区块含 创建/列表/编辑/撤销/查看文件（L14 模板、L55 状态、L201-215 函数：`loadCollections`/`openCollectionCreate`/`openCollectionEdit`/`saveCollection`/`absoluteCollectionUrl`/`copyCollection`/`collectionStatusLabel`/`viewCollection`/`revokeCollection`；接口 `GET|POST /api/collections`、`GET|PUT|DELETE /api/collections/{id}`），**可独立支撑新页面，后端零改动**。
- **解决范围**：纯前端。
- **复杂度**：M。
- **建议实现方式**：
  1. 新建 `web/src/views/CollectionsView.vue`：整体迁移 FilesView 收集区块（模板 L14 `collection-section` + 创建/编辑弹窗 L25 + 结果弹窗 L26 + 脚本 L55 状态与 L201-215 函数 + onMounted 加载），页面结构参照 SharesView（page-heading + `AuthenticatedTopbar section="collections"` + BrandFooter）；`page.collections` 标题键补三语（`nav.collections`='我的收集' 已有）。
  2. 路由：`router.js` 增加 `{ path: '/collections', component: CollectionsView, meta: { titleKey: 'page.collections' } }`（放在 `/` 与 `/shares` 之间）。
  3. 顶栏：`AuthenticatedTopbar.vue:11` 改为 `<RouterLink to="/collections">`；`nav.collections` 文案不变。
  4. FilesView：移除内嵌收集区块（L14 区块 + L55 相关状态 + L201-215 函数 + L434/507 锚点/加载逻辑），保留 `#collections` 空锚点或删除（如担心旧书签，可保留一个空 `id="collections"` 占位并提示跳转 /collections）；`scrollToCollections` 相关逻辑一并清理。
  5. 注意 readOnly：收集创建按钮（L11 `v-if="!readOnly"`）与编辑/撤销（后端 rejectReadOnly 兜底）在新页面保持一致。
- **优先级**：P1（管理体验与可发现性，与分享管理对齐）。

---

## P2 新功能

### 5. 目标系统连通性测试（"测试连接"按钮）

- **问题描述**：目标系统（remote_system）创建后应有"测试连接"能力，显示连通性结果与测试时间；当前系统行只有 编辑/删除（`SyncView.vue:11`）。
- **现状根因**：后端无专门探测端点；现有 `GET /api/sync/systems/{id}/browse`（`internal/httpapi/server.go:330`，实现 `internal/httpapi/sync.go:336-358`）会**真实建连**（SFTP：`openSFTP` 拨号+SSH 握手+主机密钥校验，sync.go:963-1009，15s 超时；filebox：`openFileBox` 登录对方 `/api/auth/login` 并检查 TOTP 拦截，sync_filebox.go:45-86）→ 可复用其建连能力，但 browse 语义是"列目录"（需 path 参数、返回目录条目、大目录慢），不适合直接当探测。
- **解决范围**：前后端。
- **复杂度**：M。
- **建议实现方式**：
  - 后端新增 `GET /api/sync/systems/{id}/test`（路由注册在 server.go:330 旁）：加载系统（`loadSyncSystem`）→ 解密凭据 → 按 kind 探测：sftp=`openSFTP`（成功即关，可选 `Stat(".")` 验证可访问）；filebox=`openFileBox`（登录成功即 `close()`）；返回 `{ok, message, elapsedMs, testedAt}`（失败时 message 用 `syncErrorDetail` 脱敏，不返回凭据/远端 token）；超时沿用现有 15s/120s；建议 `serviceEvent("sync_system_test", ...)` 轻量记录，不写审计明细（避免刷屏）。
  - 前端：`SyncView.vue:11` 系统行"编辑"前加"测试连接"按钮（图标可复用 Globe/Activity）；行内展示结果态（成功/失败 + `testedAt` 时间戳，可存于行内临时状态或 ref map）；点击禁用防抖；i18n 补 `sync.testConnection`/`sync.testing`/`sync.testOk`/`sync.testFail` 三语。
  - 注意：filebox 探测会产生一次真实远端登录（对方审计有记录）；SSRF 防护已内建于 `validateSyncSystemInput`（sync.go:120-134）。
- **优先级**：P2（新功能；需求方明确要求时按 P1 排期，可与 #3 同批实现）。

---

## 合并与实施顺序建议

1. **同步模块批次（3 + 4b + 4a + 5）**：全部落在 `SyncView.vue` 与 `internal/httpapi/sync.go`。建议顺序：4b（根目录保存，S）→ 3（filebox 密码框，S）→ 5（测试连接，M）→ 4a（单文件回归测试，P1 验证）。同一次构建/回归即可覆盖同步表单全部改动。
2. **顶栏批次（2 + 7 的顶栏入口）**：`AuthenticatedTopbar.vue` 的 `nav.sync` 文案（2）与收集入口目标（7）一起改；7 的独立页面迁移（M）可与 6 同批。
3. **管理页增强批次（6 + 7）**：聚合分享编辑（6）与收集独立页（7）都是"管理页能力补齐"，可同批（SharesView 分组弹窗模式可复制到 CollectionsView）。
4. **AdminView 批次（1）**：独立小批（每页签标题/描述 + i18n 三语）。
5. **建议总实施顺序**：4b → 3 → 2 → 1 → 5 → 6 → 7 → 4a（回归）。

## 待父任务确认的关键业务规则

- 1：用户实际运行版本是否与 HEAD/dist 一致（HEAD 上统计卡片标签互不相同）？期望的每页签标题/描述文案是什么？
- 4b：允许根目录后，远端空路径（用户 home `.`）与 FileBox 根（`''`）的展示/存储是否统一？"根目录"是否也允许作为 SFTP 源（当前远端空 → `.`）？
- 4a：是否需要"目标端选择具体文件"（`targetKind`，L）？默认不做。
- 5：测试连接是否需要审计记录？对远端 filebox 的探测会产生真实登录，是否可接受？
- 6：过期聚合分享允许 extend 重新激活（与单文件一致）？revoked 后不可编辑？增次规则与单文件完全一致？
- 7：独立收集页建立后，FilesView 内嵌收集区块是否移除？是否需要保留 `/#collections` 锚点兼容旧链接/书签？

---

**结尾声明**：本文档为分析产出（codex CLI 只读分析 + 人工源码核对，未修改任何功能代码、未执行 git 操作），所列文件/行号以 `HEAD 2d09454` 为准；待父任务确认后由后续修复子任务按本文档执行。
