# Requirement Change Log

## 2026-08-30 - v012 批次 B（功能 3/4/10：创建用户安全项 + 品牌布局 + XFF 开关）

- **功能 3（创建用户直接设置 TOTP/IP 白名单）**：`POST /api/admin/users` 请求体新增 `totpEnabled`/`reenroll`/`ipAclEnabled`/`ipWhitelist`——TOTP 启用时生成一次性 secret（enabled=true 且响应含 `totpSecret` 供管理员转交；仅 reenroll 时生成 secret 但 enabled=false，用户下次登录重绑），IP ACL 创建即应用（`normalizeWhitelist` 校验）；前端创建用户弹窗新增「安全设置」区块，提交带新字段，TOTP 启用时展示 secret。后端回归测试覆盖（`TestCreateUserSecuritySettings`）。
- **功能 4（品牌设置布局优化）**：品牌页签「界面主色」取消独占一行，改为紧凑两列网格（主色与 ICP/版权同排），整体布局更紧凑；i18n 无新增键。
- **功能 10（公网 XFF 信任开关，默认关）**：settings 新增 `trustProxy`（默认 false，migrate 自动补默认值）；仅当开关开启**且**请求直连 IP 落在 `--trusted-proxies` 白名单内时，`requestIP` 才解析 `X-Forwarded-For`（从右向左取第一个非可信 IP）；AdminView「系统设置」页签新增「信任 X-Forwarded-For」复选框；`TestRequestIPRequiresTrustProxySetting` 单测。
- 批次 B 验证：go test/vet/build 全绿；18081 回归套件（regress 26/26、transfer 19/19、share 24/24）无回归；提交 `58910e9`。

## 2026-08-30 - v012 补充批次（问题 8 补强 + 优化项 + Linux 部署验证）

- **问题 8（拖拽补强）**：`/api/brand` 公开返回 `maxFileSize`；`queueFiles` 前置校验——超过单文件大小上限的文件跳过并提示（「N 个文件超过单文件大小上限 M，已跳过」），批量 >50 文件时确认提示（「共 N 个文件，确定全部上传吗？」）；多文件/多目录拖拽（webkitGetAsEntry 递归）原有能力保留。
- **优化项 1（废弃上传任务定时清理）**：后台协程每小时清理超过 24 小时未完成的 pending 上传任务（`ListExpiredUploadTasks` 按 created_at 过期 + `DeleteUploadTask` 事务删除任务与分片记录 + 移除 tmp 目录），优雅关闭时停止；已验证（构造 25h 旧任务 → 找到并删除 → 清空）。
- **优化项 2（服务端主动推送上传进度）**：`GET /api/files/progress/stream` SSE 端点每秒推送当前用户所有 pending 任务进度（taskId/name/totalChunks/uploaded/status）；前端 FilesView 使用带 Bearer 认证且可取消的流式 fetch 订阅，刷新后/多标签页同步恢复进度，随组件卸载关闭连接。
- **二次检视修复（e811c8e）**：FilesView `reactive` 导入（阻断）、resume/retry 走并发闸门、fileIcon 分支顺序（FileCode/FileSpreadsheet 前移）、batchDownload zip 同名序号、秒传审计 target 用本次上传名。

## 2026-08-30 - v012 用户反馈批次（问题 2-12 + codex 安全检视修复）

- **问题 2+9（秒传与同名冲突协调 + 秒传审计）**：`checkInstantUpload` 接受 `name`+`dir`——目录内存在同名 ready 文件时返回 `conflict:true`（前端走冲突弹窗，覆盖/重命名），仅目录内无同名时才返回 `instant:true`（秒传）；修复"重复上传同名文件永远秒传、从不弹窗、不创建新文件"；秒传命中补审计（`recordAudit("upload", name, "success", "instant")` + serviceEvent）。同内容不同名仍秒传，同内容同名触发弹窗（可重命名创建多份）。
- **问题 3（文件夹上传并发闸门）**：`queueFiles` 经 `runGated` 并发闸门（同时最多 3 个文件进入校验/初始化），避免大批量文件夹上传时同源连接数超限导致部分文件误报「网络连接失败」。
- **问题 4（文件类型图标）**：`fileIcon` 按 MIME/扩展名映射图片/视频/音频/压缩包/JSON/表格/可执行/代码等图标，未知类型回退默认 `File` 图标。
- **问题 5+6（日志分页与筛选）**：分页增加页码数字按钮（当前页前后 2 页 + 首末页 + 总页数）；`/api/logs/actions?usedOnly=true` 返回当前用户实际存在的动作类型（store `ListUsedActions`），普通用户不再看到从未触发的「系统配置」筛选项；前端异步加载筛选项并显示「加载中…」占位。
- **问题 7（修改密码文案）**：顶栏改密入口带 `?mode=self`；ChangePasswordView 按 `mode` 区分——主动改密显示「修改密码」（ACCOUNT SECURITY），强制改密（守卫/403 跳转）仍显示「先更新初始密码」；i18n 三语 `password.self*` 键。
- **问题 10（新建用户按钮位置）**：「新建用户」按钮从页签外 page-heading 移入「用户管理」页签 toolbar，不再每个页签显示。
- **问题 11（多选聚合下载）**：新增 `POST /api/files/batch-download`（zip 打包，校验归属，任一越权整体拒绝，审计 reason=batch）；前端文件行复选框 + 全选 + toolbar「聚合下载（N）」按钮。
- **问题 12（迁移部署文档）**：README×2 部署指南新增「v010 → v011 迁移部署」章节（停服/备份/迁移命令/启动/回归）。
- **安全修复（codex 检视）**：① preview 白名单移除 `text/html` 与 `image/svg+xml`（存储型 XSS 面）——HTML/SVG 文件强制附件下载；② 默认 JWT secret 启动时打印生产警告（`--jwt-secret`/`FILEBOX_JWT_SECRET` 未设置时）；③ 修复 `startUpload` 冲突分支 `init` TDZ 错误（`let init = null` 提前声明）。

## 2026-08-30 - v011 验证反馈修复批次（问题 1-20，随 v0.2.0 交付）

### 批次 1（问题 1-6：用户实测反馈 + 自定义目录）

- 登录页不再展示初始默认账号提示：移除 LoginView 的 `login-foot` 渲染，并删除 zh-CN/zh-TW/en 三字典中的 `login.defaultAdmin` 键（不再引用，无残留）。
- 新建用户改为居中 modal 弹窗（复用 modal-backdrop/modal-panel 模式），错误内联显示紧邻「创建账号」按钮，提交成功刷新列表；服务端补 `user_create` 失败审计（含原因）。
- 拖拽目录不再误报「网络连接失败」：`handleDrop` 用 `webkitGetAsEntry()` 递归识别目录条目，空目录等无效拖拽给出明确中文提示「拖拽内容不包含可上传的文件」。
- 新增独立传输面板：顶栏「传输」按钮（带进行中数量角标）展开右侧抽屉，区分「上传」与「下载」两组；上传项保留暂停/继续/重试，下载项新增流式进度（按 Content-Length 计算百分比）。
- 文件列表 md5 直接展示 + 开关：完整性列默认直接显示 `file.md5`（悬停 title 含完整 MD5/SHA-256）；工具栏「显示 MD5」开关持久化到 `localStorage(filebox_show_md5)`，默认显示。
- **问题 6（用户自定义目录）**：移除自动年月目录层，存储结构改为 `data/files/<user_id>[/<自定义目录>/]<stored_name>`；新增 `folders` 表与目录 CRUD API（创建/列表/重命名级联/删除非空保护）；上传目标 = 当前目录；列表 `dir` 过滤 + 面包屑导航；用户隔离、配额按用户；`filebox admin migrate-v010-paths` 迁移旧 `yy/mm` 结构（备份 DB、物理移动、storage_path 重写、登记目录，幂等）。

### 批次 2（问题 7-9：logo 跳转 + 单文件交付 + 部署文档）

- **问题 7**：`BrandLogo.vue` 增加 `link` prop（RouterLink 包裹 + cursor），文件/管理/日志/分享页顶栏 logo 点击跳转首页 `/`；登录页与品牌面板预览不加。
- **问题 8**：Makefile 新增 `release` 目标（CGO_ENABLED=0、-trimpath、-ldflags="-s -w"，Windows/Linux amd64 + SHA256SUMS）+ Windows 等价脚本 `scripts/release.ps1`；已实际执行验证（Linux 静态 ELF、校验和正确）。
- **问题 9**：README.md/README.en.md 新增「部署指南（单文件交付）」章节（Windows/Linux 分步 + systemd + Nginx 反代 + 生产注意事项）。

### 批次 3（问题 10-13：日志配色 + 系统配置分组 + 改密/TOTP 重绑 + 版权）

- **问题 10**：`.result-label.success` 固定绿色（`#1e7e34/#e8f5ec`）不随主题色；失败固定红（`#a83e2d/#fff0ed`）。
- **问题 11**：`logActions` 扩为完整 24 项动作集；LogsView `actionLabel` 三语文案补齐；筛选下拉按「业务/系统配置」分组（optgroup）。
- **问题 12**：顶栏「修改密码」入口（跳转 /change-password，普通用户可自助改密）；`PUT /api/admin/users/{id}/totp` 支持 `reenroll`（新 secret 且 enabled=false，用户下次登录重绑）；AdminView「要求下次重新绑定 TOTP」复选框。
- **问题 13**：store `brand_copyright` 键 + `BrandSettings.Copyright`；`/api/brand` 返回 `copyrightText`；`PUT /api/admin/brand` 支持 `copyrightText`/`clearCopyright`；AdminView 品牌面板输入框；BrandFooter 渲染版权（空值不渲染空白页脚）；i18n 三语。

### 批次 4（问题 14-17：管理后台页签化 + 用户弹窗 + 日志周期迁移 + 页脚品牌）

- **问题 14**：AdminView 左侧竖菜单 + 右侧内容区，六页签（概览/用户管理/安全设置/品牌设置/锁定管理/系统设置）；`?tab=` 深链 + 刷新保持；页签内容用 `v-show`（保留 DOM），切换不丢已填内容（修正验收点）。
- **问题 15**：新建/编辑用户改为居中 `modal-backdrop/modal-panel` 弹窗（遮罩点击关闭，角色/配额/重置密码/禁用/TOTP 重绑/IP 白名单字段齐全，保存仍走三请求）。
- **问题 16**：LogsView 移除 logRetentionDays 面板；日志保存天数迁入 AdminView「系统设置」页签（复用 `PUT /api/admin/settings`，后端零改动）；清理 `logs.retention*` 残留键。
- **问题 17**：BrandFooter 首行 `siteTitle` + 小字 `siteDescription`，随后版权/ICP/公安备案，任一非空即渲染。
- **D-S2-3 修复（目录重命名 + 软删除记录冲突 500）**：`RenameFolder` 改写 storage_path 前清理目标前缀下 `status='deleted'` 记录；`isUniqueError` 只匹配 `UNIQUE` 约束失败（避免 FK 误判）；新增单测 `TestRenameFolderClearsDeletedRows`。

### 批次 5（问题 18-19：并发冲突队列 + 上传失败日志 + 配额/超限明细）

- **问题 18**：前端冲突弹窗改**冲突队列**（数组依次弹出 + 60s 超时取消），并发同名不再互相覆盖卡「准备中」；失败/取消项保留在传输抽屉（红色状态 + 原因 + 重试/移除）；后端 `uploadInit`/`uploadChunk` 失败分支补 `recordAudit` + `serviceEvent`（reason 细分 invalid_name/too_large/conflict/disk_full/quota_exceeded/task_not_found/invalid_index/rate_limited/size_mismatch/settings_failed/invalid_request/…，含 JSON 解码失败与分片序号解析失败等前置分支），`logActions` 补 `upload_init`/`upload_chunk`；重命名后用户可见 name 跟随序号。codex 独立复核确认全部分支覆盖。
- **问题 19**：store `QuotaError`（usedBytes/quotaBytes/fileSize）；配额拒绝 403 + `QUOTA_EXCEEDED` 明细；`max-file-size` 超限独立 `413 FILE_TOO_LARGE` + maxFileSize；前端 `localizeError` 映射（「配额不足：当前已用 X / 总配额 Y，文件需 Z，超出 W…」「文件超过单文件大小上限 M」）+ i18n 三语。

### 批次 6（问题 20：整体传输速率）

- **问题 20**：传输侧边栏顶部「整体速率」（所有进行中上传合计）；每项维护 `loadedBytes`，1s 采样 + 3 点滑动平均；单位自适应（B/KB/MB/GB per s）；无传输隐藏；定时器随组件卸载清理；i18n 三语（`files.overallRate`）。

## 2026-08-30 - 用户实测反馈批次（并入阶段二 v0.2.0 交付）

- 登录页不再展示初始默认账号提示：移除 LoginView 的 `login-foot` 渲染，并删除 zh-CN/zh-TW/en 三字典中的 `login.defaultAdmin` 键（不再引用，无残留）。
- 新增独立传输面板：顶栏「传输」按钮（带进行中数量角标）展开右侧抽屉，区分「上传」与「下载」两组；上传项保留暂停/继续/重试，下载项新增流式进度（按 Content-Length 计算百分比），面板可随时展开/收起。
- 文件列表 md5 直接展示 + 开关：完整性列默认直接显示 `file.md5` 值（悬停 title 含完整 MD5/SHA-256）；工具栏「显示 MD5」勾选开关控制该列展示方式，选择持久化到 `localStorage(filebox_show_md5)`，默认显示。
- 上传目录入口补齐 + 拖拽容错：点击「上传文件夹」与拖拽目录两条入口均走目录上传（webkitdirectory / webkitGetAsEntry 递归，回退 webkitRelativePath）；拖拽内容不含可上传文件（如空目录）时给出明确中文提示「拖拽内容不包含可上传的文件」，不再静默；上传/下载错误区分真实网络失败（映射「网络连接失败」）与业务错误，避免误导。
- 新建用户流程复核：按钮→表单→提交→API→列表刷新的前端链路完整，无 JS 错误；后端 `POST /api/admin/users` 已由 DSH 用例（S04/S05/F500，201）覆盖验证。（用户初始「无反应」为浏览器缓存旧页面所致，非代码缺陷。）

## 2026-08-29 - Stage 2 (v0.2.0) batches

### Batch A - resumable chunked upload, instant upload, folder upload (backend)

- Added the `chunks` table and `SetChunk`/`ListChunks`/`DeleteChunks` so uploaded chunk state is the durable source of truth for resuming uploads across server restarts.
- `upload-init` now accepts multi-chunk layouts: chunk sizes of 2–8MB for files larger than the chunk size, single-part (including zero-byte) otherwise; backward compatible with the Stage 1 client.
- `PUT /api/files/{taskID}/chunks/{index}` supports arbitrary chunk indexes with exact-size validation, idempotent overwrite, per-chunk SHA-256 recording, and streamed writes.
- Added `GET /api/files/{taskID}/status` returning the uploaded chunk list for resume.
- `complete` verifies every chunk record and temp file, then streams a sequential merge while computing MD5 and SHA-256, with an atomic rename into place.
- Added `POST /api/files/check` for instant upload: MD5-first, SHA-256-fallback matching within the current user; hits return the existing record without writing new content.
- Added the optional `dir` field to `upload-init` with strict validation (rejects traversal, control characters, and Windows-illegal characters); folder uploads preserve relative paths and identical names in different directories remain unsuffixed.

### Batch B - share links, online preview, rate limiting, registration, statistics (backend)

- Added the `shares` table and share endpoints: `POST /api/files/{id}/share`, `GET /api/files/shared/{token}/meta`, `GET /api/files/shared/{token}/download` (atomic slot consumption, expiry/limit enforcement), and `DELETE /api/files/{id}/shares`; share create/view/download are recorded in audit and service logs.
- Added `GET /api/files/{id}/preview` serving inline content only for the MIME whitelist (image/text/video/pdf/json) and attachment for everything else.
- Added a per-user token bucket (`golang.org/x/time/rate`) with an idle-eviction cache; `uploadRateLimit` (bytes/sec, 0 = unlimited) is configurable through admin settings and enforced before chunk writes.
- Added `POST /api/auth/register` gated by the `registerEnabled` setting (default off, `--register-enabled` seeds first deployment), enforcing the password policy, returning a login token, and exposing `registerEnabled` via `/api/brand`.
- Extended admin stats with active share count and cumulative share downloads; added the `register` audit action.

### Batch C - frontend (Vue3)

- Reworked the file page uploader: 4 concurrent chunk workers, per-file progress, pause/resume (resume uses the status endpoint to send only missing chunks), retry with exponential backoff, folder picker + recursive directory drag-and-drop, and WebCrypto SHA-256 instant-upload checks.
- Added the share dialog (expiry/max downloads, copyable link, revoke), the preview modal (image/video/pdf/text), and the anonymous `/:token` share page.
- Added the registration entry to the login page driven by the public `registerEnabled` flag.
- Extended the admin page with share statistics and registration/upload-rate settings (KB/s), and added all new UI strings to the zh-CN/zh-TW/en dictionaries.

### Defect fixes found during DSH acceptance testing

- **D-S2-1 (一般, storage reuse)**: re-uploading a file name that had been soft-deleted returned HTTP 500 (`UNIQUE constraint failed: files.storage_path`). Fixed in `allocateStorageName`: the soft-deleted row holding the target storage path is now removed when the path is reused, and a regression test (`TestReuploadAfterDeleteReusesStoragePath`) was added.
- **D-S2-2 (轻微, CLI)**: `filebox admin reset-password` dropped its first flag (parsed `args[1:]` again), so `--data` was ignored. Fixed to parse all remaining flags and covered by `TestResetPasswordParsesDataFlag`.

## 2026-08-29 - D1-D4 defect fixes

- Fixed same-directory rename placement to use `name (n).ext` while preserving the extension within the 255-byte storage-name limit.
- Completed the zero-byte single-part upload path and verified empty-file MD5/SHA-256 values and on-disk storage.
- Added `admin clear-ip-acl` for local recovery from an accidentally restrictive source-IP allowlist, with structured CLI service logging and standard exit codes.
- Changed admin settings updates to merge omitted numeric/boolean fields with persisted values and return field-specific validation messages for invalid settings.

## 2026-08-29

- Delivered Stage 1 MVP from `CODEX_TASK_1.md`.
- Added the confirmed MD5 requirement alongside SHA-256; completed file records and APIs expose both values.
- Kept all Stage 2 items out of this delivery: resumable/chunked transfer, instant upload, folders, sharing, previews, rate limiting, and public registration.
- Added a Vue 3 frontend and embedded production bundle workflow.
- Implemented R-DISK: cross-platform disk usage stats, configurable minimum free space, and DISK_FULL upload protection.
- Implemented R-NAME: sanitized original disk names, per-user/per-month storage directories, transactional same-directory suffix allocation, and unique storage paths.

## 2026-08-29 — 第二批变更

- Implemented R-VALID: upload-init now rejects path separators, control characters, Windows-illegal characters, dot traversal markers, empty/dot names, and names over 255 bytes.
- Implemented R-CONFLICT: same-directory collisions return the existing file metadata for a user choice; overwrite removes the old record/content and corrects used bytes, while rename allocates a numbered name.
- Implemented R-LOG: audit log schema, lazy retention cleanup, login/upload/download instrumentation, paginated log APIs, user isolation, admin user filtering, and retention settings.
- Implemented R-LOCK: configurable failed-login lockout, automatic unlock, uniform login errors, failure reasons in audit logs, and admin lock reset on user edits.
- Added the authenticated `/logs` frontend page, admin retention controls, and the upload conflict resolution dialog.

## 2026-08-29 — 第三批变更（R-BRAND）

- Implemented configurable brand settings for site title, SEO description, ICP/public-security filing text, favicon, login logo, and main logo.
- Added public `/api/brand` and `/brand/*` resource endpoints with fixed-name storage under `data/brand`, strict extension/size/content validation, atomic writes, and embedded SVG fallbacks.
- Added the admin multipart brand panel with previews, per-resource removal, text clearing, and confirmed full reset; runtime SEO, logos, and conditional filing footer update immediately.

## 2026-08-29 — 文档与注释收尾

- Added Chinese-first, English-second bilingual comments for exported Go APIs and the authentication, quota, conflict, filename validation, disk protection, hashing, transactional storage, Range download, audit, and branding paths.
- Added bilingual `README.md` / `README.en.md` and `RELEASE_NOTES.md` / `RELEASE_NOTES.en.md`, including the Stage 1 feature markers, configuration defaults, API/storage summaries, deployment guidance, security notice, and Stage 2 limitations.

## 2026-08-29 — 第四批变更（R-LANG）

- Implemented the `users.language` migration, authenticated language update endpoint, and `defaultLang` system setting with validation and public brand exposure.
- Added lightweight three-dictionary frontend i18n for Simplified Chinese, Traditional Chinese, and English, including runtime switching, local persistence, user preference syncing, locale-aware dates, page titles, and localized API errors.
- Converted login, files, admin, logs, navigation, dialogs, upload states, empty states, and branding controls to i18n keys; added language selectors for public and authenticated views.

## 2026-08-29 - Fifth batch (R-THEME)

- Added persisted `theme_color` settings with `#RGB`/`#RRGGBB` validation, default fallback, and public `themeColor` exposure through `/api/brand`.
- Added admin hex input, native color picker, default reset, and immediate CSS variable/meta updates for buttons, links, progress, focus rings, selected states, and status accents.

## 2026-08-29 - Sixth batch (R-INIT / R-PWD / R-TOTP / R-IPACL / R-IPBAN / R-LOCKADMIN)

- Added configurable initial administrator credentials and forced password changes, including a protected change-password route and admin reset behavior.
- Added configurable password length and character-class policy enforcement for user creation, password resets, and self-service changes.
- Added encrypted TOTP secrets, RFC 6238 setup/login challenges, QR generation, one-time setup secret display, and replay protection.
- Added per-user IPv4/IPv6 IP/CIDR allowlists and trusted-proxy-aware source-IP resolution.
- Added sliding-window IP login lockout with configurable thresholds, automatic unlock, audit reasons, success reset, and admin lock removal APIs/UI.

## 2026-08-29 - Seventh batch (R-OPS)

- Added `admin reset-password` with explicit or generated one-time passwords, forced password change, and lock-state reset.
- Added `locks list` and `locks clear` for IP and user lock recovery without HTTP authentication.
- Added a SQLite busy timeout so CLI maintenance can briefly coexist with the running service.

## 2026-08-29 - Eighth batch (R-SRVLOG / R-SERVICE / R-PROXY)

- Added optional service logging with console fallback, daily local-time files, gzip rollover, startup/rollover retention cleanup, and operator/source-IP structured events.
- Instrumented authentication, file operations, account administration, settings/branding, lock management, and R-OPS commands without recording passwords, tokens, or file contents.
- Added graceful SIGINT/SIGTERM shutdown events and `--log-enabled`, `--log-dir`, and `--log-retention-days` flags with environment-variable defaults.
- Added Linux systemd, Windows NSSM/sc, and Nginx HTTPS reverse-proxy examples plus bilingual deployment documentation under `deploy/`.
