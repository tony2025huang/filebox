# CODEX 任务书 v017 批次（10 项需求）

> 批次基准：git HEAD = 57a5592（工作树干净）
> 实施方式：codex CLI 分批编码（每批 1-2 项），本文件为总任务书与验收对照表。
> 每批完成后：`go build ./... && go vet ./...`（Go 改动时）+ `npm run build`（前端改动时）→ git 提交推送。

---

## 0. 执行纪律（v016 教训，必须遵守）

1. **单个 codex 任务短小**：每批只含 1-2 项需求，提示词文件自包含（背景+现状+要求+验收）。
2. **绝不允许**一个 codex 任务包含全部 10 项。
3. codex 8 分钟无输出视为失败：杀进程，换更小任务。
4. **每完成 1-2 项即 git 提交推送**（禁止攒批）。
5. 遇到阻塞立即报告，不要默默等待。
6. codex 调用方式（已验证可行，禁止把长提示词放命令行参数）：

```powershell
$auth = Get-Content "C:\Users\huangcp\.codex\auth.json" -Raw | ConvertFrom-Json
$env:OPENAI_API_KEY = $auth.OPENAI_API_KEY
$prompt = [System.IO.File]::ReadAllText("<提示词文件路径>", [System.Text.Encoding]::UTF8)
$prompt | & "C:\Users\huangcp\.codex\packages\standalone\current\bin\codex.exe" exec -C "C:\Users\huangcp\dsh-project\filebox" --skip-git-repo-check -s danger-full-access - 2>&1 | Select-Object -Last 25
```

7. 前端构建：`web` 目录下 `npm run build`，产物需构建/复制到 `internal/webassets/dist`（见 Makefile 或已有流程）。
8. 本批次沿用 v016 的既有基础设施：AES-GCM 凭据加密（`encryptSyncSecret`）、`__keep__` 空密码保留逻辑、i18n 三语（zh-CN/zh-TW/en）、modal-backdrop 弹窗模式均已存在，**不得重复造轮子**。

---

## 1. 需求对照表

| # | 需求 | 现状定位（代码位置） | 实现要点 | 验收标准 |
|---|------|---------------------|---------|---------|
| 1 | 文件库移除收集区块 | `web/src/views/FilesView.vue`：line 14 `collection-section` 区块、line 11 dir-bar 中 `collection-entry` 按钮、line 55 收集相关 state（collections/collectionsLoading/collectionCreateOpen/…）、line 201-215 收集函数（loadCollections/openCollectionCreate/openCollectionEdit/saveCollection/viewCollection/revokeCollection/copyCollection/toLocalInputValue/remainingLabel/collectionStatusLabel/absoluteCollectionUrl）、line 25-26 两个收集 modal。`CollectionsView.vue` 已有独立导航页（/collections 路由），功能完整保留 | 删除 FilesView 中收集区块及其 UI/状态/加载逻辑（collection-section、collection-entry 按钮、收集 modal、相关 ref/函数/import）；`CollectionsView.vue` 与 `/collections` 路由不动 | ①FilesView 不再出现"我的收集"区块与创建入口；②收集相关 API（/api/collections）不再从 FilesView 请求；③`npm run build` 通过；④"我的收集"独立页（/collections）功能不变 |
| 2 | 管理后台各页签说明文案区分 | `web/src/views/AdminView.vue`：line 9 页面顶部共用 `admin.eyebrow` + `admin.heading` + `admin.copy`；六个页签 overview/users/security/brand/locks/system（tabs 定义 line 64-71）。`web/src/i18n.js`：`admin.copy` 三语均为"管理账号、分配空间，并查看当前存储概况" | 为每个页签配置独立说明文案（三语）：在 i18n 新增 `admin.copyOverview / admin.copyUsers / admin.copySecurity / admin.copyBrand / admin.copyLocks / admin.copySystem`（与各页签实际功能匹配：概览=存储/账号概况；用户管理=账号配额与状态；安全设置=密码强度与 IP 锁定；品牌设置=站点标题/标识/备案；锁定管理=IP 与账号锁定；系统设置=日志留存/注册/限速/代理信任）；AdminView 页面 heading 的 copy 按 `activeTab` 动态取对应键；eyebrow 各页签已有独立键（admin.securityEyebrow/brandEyebrow/locksEyebrow 等）可不改 | ①六个页签切换时说明文案各不相同且与实际功能匹配；②三语（zh-CN/zh-TW/en）都有对应文案；③i18n 键完整、`npm run build` 通过 |
| 3 | 导航顺序调整 | `web/src/components/AuthenticatedTopbar.vue`：line 7-12 当前顺序 = 文件/分享/同步/日志/收集/管理后台(admin)；命名：`nav.shares`='分享管理'、`nav.admin`='管理后台'。`web/src/i18n.js`：nav 系列键（nav.files/nav.shares/nav.collections/nav.sync/nav.logs/nav.admin/page.*） | 顺序调整为：**我的文件 → 我的收集 → 我的分享 → 同步任务 → 日志 → 系统设置**；命名调整：`nav.shares`/`page.shares`/`shares.heading` 改为"我的分享"（en: My shares，zh-TW: 我的分享），`nav.admin`/`page.admin` 改为"系统设置"（en: System settings，zh-TW: 系統設定）；路由不变（/shares 与 /admin） | ①顶栏顺序与命名符合目标顺序；②三语一致；③`npm run build` 通过；④shares/admin 路由与页面功能不变 |
| 4 | 操作日志时间范围筛选 | 前端 `web/src/views/LogsView.vue`：line 6 toolbar 现有筛选（关键词/动作/结果/用户），`loadLogs`（line 24）构造 URLSearchParams。后端 `internal/httpapi/server.go` line 3828 `listLogs` → `store.ListAuditLogs`（`internal/store/store.go` line 3039，签名 `(ctx, userID, action, result, keyword, page, pageSize)`）；审计表 created_at 为 RFC3339 文本 | 前端：LogsView 增加"开始时间/结束时间"两个 datetime-local 输入（三语 label：logs.startTime/logs.endTime），随 loadLogs 传 `from`/`to`（ISO8601，前端 `new Date(value).toISOString()` 转换，可为空）；后端：`listLogs` 解析 `from`/`to`（RFC3339，非法值 400），`store.ListAuditLogs` 增加 `from, to string` 参数并在 SQL 加 `created_at >= ?` / `created_at <= ?` 条件（BETWEEN 语义，含边界）；**补单测**（store 层时间过滤 + httpapi 层参数校验/过滤） | ①日志页可按时间范围筛选且与其它筛选条件叠加；②空值=不过滤；③非法时间参数返回 400；④单测通过；⑤三语文案齐全 |
| 5 | 同步任务密码入库+可查看 | 现状已确认：`internal/httpapi/sync.go` `createSyncSystem`/`updateSyncSystem` 已用 AES-GCM（`encryptSyncSecret`）加密入库；`publicSyncSystem`（line 76）已返回 `hasCredentials`；编辑留空=保留（`__keep__`，line 262-264）已实现，**"编辑丢失密码"问题已解决**。前端 `web/src/views/SyncView.vue`：`openSystemEdit`（line 66）清空 authSecret；密码输入框（line 16 附近）placeholder 用 `sync.credentialsHint`，但**没有"已保存密码"提示，也没有查看已保存密码的入口** | 前端：密码字段在编辑已保存凭据时显示占位"已保存密码（点击眼睛查看）"（或 `hasCredentials` 为真时显示 `******` 占位）；增加眼睛图标按钮，点击调用**专用端点** `GET /api/sync/systems/{id}/secret`（新增，返回解密后的 `{secret, authPassphrase}` 一次），切换显示明文/掩码；后端：新增该端点（仅系统所有者/admin 可访问，走 `loadSyncSystem` 权限校验 + `decryptSyncSecret`），可加审计 serviceEvent | ①编辑目标系统时密码不丢失（留空=保留，现状已满足）；②有已保存密码提示；③点击眼睛可查看已保存密码（API 单次返回明文）；④三语 i18n；⑤`go build`/`go test`/`npm run build` 通过 |
| 6 | 同步任务执行历史 | `web/src/views/SyncView.vue`：`openDetails`/`loadDetails`（line 118-119）已有详情弹窗展示 `detailLogs`（GET /api/sync/tasks/{id} 返回 logs），现有展示 runAt/result/files/bytes/message/detail，**无开始/结束时间对、无进行中状态**。后端 `internal/httpapi/sync.go` 任务详情 handler 返回 logs（分页） | 前端：执行历史列表展示**开始时间、结束时间（进行中则不显示）、结果（成功/失败/进行中）**；无 logs 时显示空态；后端：配合需求 9 的 sync_logs 结构调整（新增 finished_at 列、result 支持 running），详情接口返回新字段（finishedAt/result='running'）；分页与刷新按钮保留 | ①详情弹窗展示每次执行的开始/结束时间与结果；②进行中的执行显示"进行中"且无结束时间；③完成后刷新显示结束时间与结果；④三语 i18n |
| 7 | 同步任务目录选择增强 | `web/src/views/SyncView.vue`：line 18 picker modal（浏览本地 FileBox 目录/远端 SFTP/FileBox 目录），`browseRemote`/`browseLocal`/`fileboxFoldersAt`/`remoteEntries`/`localFileEntries`；本地目录来自 `folders`（/api/folders），远端来自 browse API | ①picker 弹窗增加**过滤输入框**：按名称过滤当前目录列表（本地与远端都支持，前端过滤已加载列表即可）；②增加**手动输入目录全路径**的文本输入框 + 确认按钮：输入完整路径后设置 `taskForm[path]` 并关闭弹窗（本地路径与远端路径分别校验：远端可调用 browse 验证，本地路径不做存在性强制校验但需通过后端校验）；③i18n 三语（sync.filterPlaceholder / sync.enterPath / sync.confirmPath 等） | ①可过滤当前目录列表；②可手动输入全路径并确认选中；③本地与远端 picker 均支持；④三语 i18n；⑤`npm run build` 通过 |
| 8 | 同步任务列表显示目标 | `web/src/views/SyncView.vue`：line 9 任务表"源 → 目标"列只显示 `sourcePath → targetPath`，系统名仅以 `systemName(item.remoteSystemId)` 小字显示在任务名下；目标系统的 host/url 未在任务行展示（systems 列表已加载，`item.host`/`item.url` 可用） | 前端：任务表"目标"侧（或系统名下小字）明确显示目标 IP/域名：SFTP 显示 `host:port`，filebox 显示 URL 主机部分（`new URL(item.url).host`，解析失败则显示原 url）；三语可加 `sync.targetHost` 前缀或直接拼接；**纯前端改动** | ①任务列表能看出同步目标的主机/IP/域名（SFTP=host:port，filebox=URL 主机）；②无 host/url 时优雅降级（显示系统名）；③`npm run build` 通过 |
| 9 | 同步任务日志实时状态 | 后端 `internal/httpapi/sync.go` `executeSyncTask`（line 1170+）：**执行完成才 CreateSyncLog 写一行**（runAt=开始时间，无结束时间列）；`internal/store/sync.go`：sync_logs 表（line 113-126）列为 id/task_id/user_id/run_at/direction/result(CHECK IN success,failure)/files/bytes/message/detail；`CreateSyncLog`（line 491）、`ListSyncLogs`（line 521） | ①sync_logs 新增 `finished_at TEXT NOT NULL DEFAULT ''` 列（迁移：migrateSyncSchema 加 ALTER TABLE，参照现有 last_test_at 模式）；result 的 CHECK 约束需允许 'running'（SQLite 改 CHECK 需重建表，或采用**约定：running 行 result='running' 时使用新表/放宽约束**——建议迁移时重建 sync_logs 表去掉 CHECK 或新增 CHECK 含 running）；②`executeSyncTask`：执行开始时先 CreateSyncLog 写 running 行（result='running', finished_at=''），完成后 `UPDATE` 该行（按 log id）写入结果/finished_at/files/bytes/message/detail；幂等：单任务加锁已存在（syncLocks），失败路径也需更新 running 行；③store 增加 `UpdateSyncLogResult(ctx, logID, result, finishedAt, files, bytes, message, detail)`；`ListSyncLogs`/任务详情返回 finishedAt 字段；④前端（需求 6 一并）展示进行中状态 | ①执行开始时数据库即存在 running 行；②完成后同一条记录被更新（结束时间+结果）；③进行中的执行在列表/详情可见且无结束时间；④失败路径同样更新；⑤单测覆盖（store 层 update + httpapi 层 running→final 流程）；⑥`go build`/`go test` 通过 |
| 10 | 修改密码弹窗化 | `web/src/views/ChangePasswordView.vue`：独立页（/change-password，?mode=self 为自助改密）；`web/src/components/AuthenticatedTopbar.vue` line 13：顶栏"修改密码"是 RouterLink 到 /change-password?mode=self；router.js line 55：must_change_password 时强制跳 /change-password 页；后端 POST /api/auth/change-password（server.go line 1110 附近）已存在 | ①顶栏"修改密码"入口改为按钮，点击弹出**居中 modal**（modal-backdrop + modal-panel 模式，表单复用 ChangePasswordView 的字段与逻辑：旧密码/新密码/确认，提交 POST /api/auth/change-password，成功 saveSession 后关闭并提示）；②modal 实现放 **AuthenticatedTopbar 内部**（所有登录视图共享，无需在每个视图复制）；③**强制改密逻辑不变**：must_change_password 时仍由路由守卫跳 /change-password 独立页；④ChangePasswordView 保留（强制改密用）；⑤i18n 复用 password.* 键（必要时新增弹窗标题键） | ①顶栏点"修改密码"弹出居中弹窗，不跳页；②提交成功更新会话并关闭弹窗；③must_change_password 用户仍被强制跳转独立页；④三语；⑤`npm run build` 通过 |

---

## 2. 批次划分（codex 执行计划）

| 批次 | 需求 | 主要改动 | 验证 |
|------|------|---------|------|
| 批 1 | #1 + #3 | 纯前端：FilesView 去收集区块 + 顶栏导航顺序/命名 | npm build + 提交推送 |
| 批 2 | #2 | 纯前端：AdminView 各页签文案 + i18n 三语 | npm build + 提交推送 |
| 批 3 | #4 | 前端 LogsView + 后端 ListAuditLogs from/to + 单测 | go build/test + npm build + 提交推送 |
| 批 4 | #5 + #6 | 后端 secret 端点 + 前端密码查看；前端执行历史展示（依赖 #9 字段时先做可兼容部分） | go build/test + npm build + 提交推送 |
| 批 5 | #7 + #8 | 纯前端：picker 增强 + 任务列表目标显示 | npm build + 提交推送 |
| 批 6 | #9 + #10 | 后端 sync_logs running/update + 迁移；前端弹窗改密 | go build/test + npm build + 提交推送 |
| 收尾 | 全量 | go test ./... + npm build；补测试；更新文档；重建发布产物；三端部署；Release 更新 | 见最终汇报 |

> 注：批 4 与批 6 中 #6 与 #9 有耦合（执行历史展示依赖 finished_at/running 字段），实施时若 #9 未完成，#6 前端先行实现可展示的字段、后端字段就绪后自动生效；批次顺序允许微调，但每次 codex 任务仍不得超过 2 项需求。
