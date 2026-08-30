# Codex 开发任务 — 阶段二（增强：分片断点续传 / 秒传 / 文件夹上传 / 分享 / 预览 / 限速 / 注册 / 统计）

请完整阅读并遵循 `docs/DEV_DOC.md`（本项目开发文档，需求与验收以它为准，重点 **5.2 / 5.3 / 8 节** 与 **10 节阶段二验收标准**）。本文件定义阶段二的执行范围、接口规格与验收标准。阶段一全部功能与测试必须保持通过。

## 0. 执行环境

- 项目根：`C:\Users\huangcp\dsh-project\filebox`（git 仓库，main 分支，HEAD=v0.1.0，工作区干净）
- Windows + PowerShell 5.1；Go 工具链在 `C:\Users\huangcp\AppData\Local\Programs\Go\go\bin`（不在 PATH，使用前 `$env:PATH = "C:\Users\huangcp\AppData\Local\Programs\Go\go\bin;" + $env:PATH`）；Node 在 `C:\Users\huangcp\AppData\Local\Programs\nodejs`
- 自测时用**独立端口 18081** 与**独立数据目录**（如 `C:\Users\huangcp\dsh-project\filebox\.test-data\stage2\dev-data`），不要用 18080（另一个任务在用）与默认 `./data`
- 依赖：Go 后端可新增 `golang.org/x/time`（`go get golang.org/x/time`）；前端**不引入**新 npm 依赖（md5 用 WebCrypto sha256 方案，见 4.3）

## 1. 目标

在阶段一（登录/文件 CRUD/配额/双哈希/管理/品牌/多语言/主题色/安全/日志/运维/服务化）基础上实现阶段二 8 项增强：

1. 分片断点续传（2–8MB 分片、前端并发 4、暂停/继续、断点续传）
2. 秒传（POST /api/files/check，md5 优先 sha256 兜底，命中不重复落盘直接返回文件记录）
3. 文件夹/多文件上传（保留相对路径，不同相对目录同名不加序号）
4. 分享链接（有效期/次数限制/匿名分享页，分享创建/查看/下载计入审计日志 R-LOG）
5. 在线预览（GET /api/files/:id/preview 按 MIME 白名单 inline，白名单外强制下载）
6. 上传限速（golang.org/x/time/rate 令牌桶按用户限速，settings 可配，默认不限）
7. 注册开关（POST /api/auth/register 受 settings 注册开关控制，默认关；前端登录页按开关显示注册入口）
8. 系统统计补全（admin stats 增加分享数等合理统计项）

## 2. 技术约束（与阶段一一致）

- 后端 Go（标准库 net/http 方法路由）；分层 `cmd/filebox` / `internal/httpapi` / `internal/store` / `web/`
- 所有**导出符号**与**关键业务逻辑**注释**中英双语**（中文在前英文在后，见 DEV_DOC 2.5 节）；修改代码时同步更新注释
- 统一响应 `{code, message, data}`；错误不泄露内部路径；`X-Content-Type-Options: nosniff`、CSP 保持
- API 归属校验：用户只能操作自己的文件；分享匿名接口不做鉴权但必须校验 token/有效期/次数
- 文件路径一律由 DB 记录解析，禁止拼接用户输入（防路径穿越）
- 前端 Vue3 轻量 i18n（zh-CN/zh-TW/en 三字典同键集）：**新增文案必须三语齐全**
- 保持 `go build ./...`、`go vet ./...`、`go test ./...`、`npm --prefix web run build`、`go run ./scripts/sync-web.go` 全部通过

## 3. 数据模型（SQLite 变更）

在 `internal/store/store.go` 的 `migrate()` schema 中新增（CREATE TABLE IF NOT EXISTS）：

```
-- 分片记录（阶段二）：任务 id + 分片序号唯一；size=该分片字节数；sha256=该分片内容哈希
chunks     task_id TEXT NOT NULL REFERENCES upload_tasks(id) ON DELETE CASCADE,
           idx INTEGER NOT NULL, size INTEGER NOT NULL, sha256 TEXT NOT NULL,
           PRIMARY KEY(task_id, idx)
-- 分享（阶段二）：token 为 64 位随机字符串（不可枚举）；expires_at 过期时间；
-- download_count 已下载次数；max_downloads 次数上限（0=不限）
shares     id INTEGER PRIMARY KEY AUTOINCREMENT,
           file_id INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
           token TEXT NOT NULL UNIQUE, created_by INTEGER NOT NULL,
           expires_at TEXT NOT NULL, download_count INTEGER NOT NULL DEFAULT 0,
           max_downloads INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL
CREATE INDEX IF NOT EXISTS idx_shares_file ON shares(file_id);
```

settings 表新增键（migrateSettings 的 defaults 中补充，缺失时用默认值插入）：
- `registerEnabled` = `"false"`（注册开关，默认关；`--register-enabled` flag 仅在首次启动且用户未显式设置时作种子值）
- `uploadRateLimit` = `"0"`（每用户上传限速，字节/秒；0=不限，默认 0）

`GetLogSettings` / `LogSettings` 增加字段：`RegisterEnabled bool`、`UploadRateLimit int64`（bytes/sec，0=不限）；`PUT /api/admin/settings` 对应增加可空指针字段 `registerEnabled *bool`、`uploadRateLimit *int64`，校验：uploadRateLimit >= 0（非法 400「上传限速无效」）。

## 4. 后端 API 规格

### 4.1 分片断点续传（改造现有 3 个接口 + 新增 status）

**POST /api/files/upload-init**（现有接口扩展，保持向后兼容）
请求体：`{name, size, chunkSize, sha256?, md5?, mime, resolve?, dir?}`
- `dir`（新，可选）：文件夹上传的相对目录，如 `assets/images`。校验：路径分隔符统一 `/`，**禁止 `..`、`.`、绝对路径、空段、控制字符与 Windows 非法字符**，否则 400「目录无效」；单文件上传不传或传空串。
- `chunkSize` 语义（保持阶段一兼容）：
  - `chunkSize == 0` 或 `chunkSize >= size`（且 size>0）→ 单分片（totalChunks=1，chunkSize=size），与阶段一行为完全一致；
  - `size == 0` → 单分片（chunkSize=1，totalChunks=1），保持阶段一 D2 修复；
  - 其余（多分片）：**chunkSize 必须在 [2MB, 8MB]**，否则 400「分片大小必须在 2MB-8MB 之间」；totalChunks = ceil(size/chunkSize)。
- 其余校验与阶段一相同：文件名 R-VALID（400）、磁盘保护 R-DISK（503 DISK_FULL）、配额预留（403）、冲突检测（409 + conflict 数据，resolve 必填）。
- **冲突检测必须按完整目录**：`storage_path = files/<user>/<yy>/<mm>[/<dir>]/<stored_name>` 判定，不同相对目录同名不冲突。
- 响应：`{taskId, chunkSize, totalChunks}`（新增可选项 `uploadedChunks: [...]`，便于 init 后直接续传）。

**PUT /api/files/{taskID}/chunks/{index}**（扩展为多分片）
- 校验任务归属与 status='pending'；index 必须在 [0, totalChunks)；请求体为原始二进制（阶段一即如此）。
- 期望分片大小：index < totalChunks-1 时必须 == chunkSize；最后一个分片允许 <= chunkSize（且 == size - chunkSize*(totalChunks-1)）。大小不符 400「分片大小与声明不一致」。
- 写入 `data/tmp/<taskID>/<index>`（O_CREATE|O_TRUNC 幂等覆盖，支持重试/重复上传）；写入成功后向 `chunks` 表 upsert `(task_id, idx, size, sha256)`（对同一分片重传即覆盖）。
- 响应：`{index, size}`；错误码保持阶段一风格（413 超声明、400 内容不符、404 任务不存在）。
- **限速钩子**：本接口在写入前调用按用户令牌桶 `WaitN`（见 4.5）。

**GET /api/files/{taskID}/status**（新接口，断点续传用）
- 校验任务归属；返回 `{taskId, name, size, chunkSize, totalChunks, status, uploadedChunks: [已上传分片序号，升序]}`。
- uploadedChunks 以 `chunks` 表为准（服务重启后仍可靠）；任务 complete 后返回 uploadedChunks=全部分片。

**POST /api/files/{taskID}/complete**（扩展为分片合并）
- 请求体 `{action: 'overwrite'|'rename', sha256?, md5?}`（action 仅 init 返回 conflict 时必填，语义同阶段一）。
- 校验：任务归属、status='pending'；`chunks` 表记录数 == totalChunks 且每片对应临时文件存在且 size 匹配（缺片 400「上传分片不完整」）。
- **流式合并**：按 index 升序将 `data/tmp/<taskID>/<index>` 依次拷贝到最终路径（先写同目录临时文件再原子 rename，避免半截文件）；合并同时流式计算 md5+sha256（双哈希基于实际字节，客户端声明值仅可选比对，不一致 400「文件校验值不匹配」）。
- 落盘路径：`data/files/<user>/<yy>/<mm>[/<dir>]/<stored_name>`；冲突 rename/overwrite 逻辑沿用阶段一事务（CompleteUploadWithPlacement），overwrite 时删除同路径旧文件。
- 完成后：删除 `data/tmp/<taskID>` 与 `chunks` 表记录（任务 status='complete'）；保留审计日志与文件记录。
- 响应同阶段一：文件记录（含 md5/sha256）。

### 4.2 秒传

**POST /api/files/check**（新接口，需登录）
- 请求体：`{sha256, md5?, size}`；size 必填且 >0（size<=0 400「文件大小无效」）。
- 匹配范围：**当前用户自己**的 ready 文件。优先 `md5 = ? AND size = ?`（md5 非空时）；否则 `sha256 = ? AND size = ?`。
- 命中：返回 `{instant: true, file: <publicFile>}`（不新建任务、不落盘）；未命中：`{instant: false}`。
- 说明：跨用户不秒传（文件归属与配额均按用户隔离）；上传记录以命中文件的记录为准。

### 4.3 文件夹上传

- 前端：`<input type="file" webkitdirectory multiple>` 收集文件夹；每个文件带 `webkitRelativePath`，拆出相对目录 `dir` 与文件名 `name`。
- 后端：upload-init 携带 `dir`（见 4.1）；`StorageDir = files/<user>/<yy>/<mm>/<dir>`；`storage_path` 保留相对目录结构；**不同相对目录下同名文件不加序号**（现有 allocateStorageName 已按目录区分，只需传入完整目录）。
- 多文件并发上传：每个文件独立 task；前端统一调度（并发上限 4 个 task 并行，每个 task 内部并发 4 个分片——可简化为全局分片并发池，见 6.1）。

### 4.4 分享链接

**POST /api/files/{id}/share**（需登录，文件归属校验；404「文件不存在」）
- 请求体：`{expiresInHours, maxDownloads?}`；expiresInHours 必填且 >=1（400「分享有效期无效」）；maxDownloads 默认 0（不限），<0 或 >100000 400「分享次数限制无效」。
- 生成 64 位随机 token（crypto/rand，字母数字）；`expires_at = now + expiresInHours*time.Hour`；写入 shares 表。
- 响应：`{id, token, url: "/<token>", expiresAt, maxDownloads, downloadCount: 0, fileName, fileSize}`。
- 审计/服务日志：action `share`（success/failure），operator=当前用户；服务日志事件 `share_create`。

**GET /api/files/shared/{token}/meta**（公开，匿名）
- 校验 token 存在；文件 status='ready'（否则 404）；`expires_at < now` → 404「分享链接已过期」（不泄露存在性细节）。
- 响应：`{fileName, fileSize, mime, expiresAt, maxDownloads, downloadCount, createdBy}`。
- 审计：action `share_view`（user_id=NULL，username='anonymous'，ip=来源 IP），成功才记录；失败（token 无效/过期）记录 failure。服务日志事件 `share_view`。

**GET /api/files/shared/{token}/download**（公开，匿名）
- 校验顺序：token 存在 → 文件 ready → 未过期（`expires_at < now` → 403「分享链接已过期」）→ `max_downloads>0 && download_count >= max_downloads` → 403「分享次数已用完」。
- **原子递增** download_count（UPDATE ... WHERE id=? AND (max_downloads=0 OR download_count<max_downloads)，RowsAffected==0 → 403「分享次数已用完」）；再流式输出（http.ServeContent，支持 Range 206），Content-Disposition attachment + 原文件名。
- 审计：action `share_download`（匿名）；服务日志事件 `share_download`。

**DELETE /api/files/{id}/shares**（需登录，文件归属校验）
- 删除该文件全部分享记录；响应 `{removed: N}`。审计 action `share`（撤销，target=文件名）。

### 4.5 上传限速

- `internal/httpapi` 新增按用户令牌桶：`map[int64]*rate.Limiter` + `sync.Mutex`；惰性创建（rate=settings.UploadRateLimit bytes/sec，burst=rate 与 1MB 取较大者，保证单分片能开始）；速率 0 时跳过（不限速）；对空闲 limiter 做简单清理（如每次访问时清理超过 10 分钟未用的条目）。
- settings 变化后**下一次请求即生效**（每次 uploadChunk 从 settings 读取速率；速率变化时重建 limiter）。
- 应用位置：`uploadChunk` 在写盘前 `WaitN(ctx, 分片大小)`（ctx 取消则返回 408/499 类错误，前端可续传）。
- 注意：限速只影响上传写入，不影响下载/预览/管理接口。

### 4.6 注册开关

- `POST /api/auth/register`（公开）请求体 `{username, password}`：
  - settings `registerEnabled != true` → 403「注册功能未开放」（错误码 `REGISTER_DISABLED`）；
  - username 非空、合法（与创建用户一致：trim、长度、字符集）、唯一（409「用户名已存在」）；password 按 R-PWD 策略校验（400 具体文案）；
  - 成功：创建 role='user' 用户，配额=默认 100GB，must_change_password=false，language=''；**直接签发 JWT** 并返回 `{token, user}`（等效登录，前端免二次登录）。
  - 审计：action `register`（success/failure）；服务日志事件 `register`。
- `GET /api/brand` 响应增加 `registerEnabled`（公开，登录页据此显示/隐藏注册入口）。
- `--register-enabled` flag（main.go 已有）保留：启动时若为 true 且 settings 无显式值则写入 registerEnabled=true（作为首次部署种子；之后以 settings/管理界面为准）。
- 前端登录页：brand.registerEnabled 时显示「注册账号」入口 → 注册表单（用户名/密码/确认密码，显示密码策略提示）；注册成功后直接进入文件页。

### 4.7 系统统计补全

- `GET /api/admin/stats` 增加：`shares`（未过期的分享数：`SELECT COUNT(id) FROM shares WHERE expires_at > now`）、`shareDownloads`（分享累计下载次数：`SELECT COALESCE(SUM(download_count),0) FROM shares`）。原有 users/files/bytes/disk 保持不变。
- 前端管理统计卡片增加「分享链接」数（分享数 + 累计下载次）。

### 4.8 在线预览

- `GET /api/files/{id}/preview`（需登录，归属校验同 download）
- MIME 白名单（精确匹配 mime 字段；文件 mime 为空按扩展名推断）：
  - image: png / jpeg / gif / webp / svg+xml
  - text: plain / markdown / csv / x-log / html（text/* 白名单按 DEV_DOC：txt/md/log/json/csv 及同类文本）
  - application: json / pdf / x-subrip?（不扩展，仅 json、pdf）
  - video: mp4 / webm
- 命中白名单：`Content-Disposition: inline` + 原 Content-Type，流式输出（支持 Range，方便视频拖动）；未命中：`attachment`（等价下载）。审计 action `download`（target=文件名，preview 也计一次下载审计，与阶段一一致）。
- 前端：文件列表增加「预览」按钮（可预览类型高亮），弹窗内 `<img>`/`<video>`/`<iframe>`/`<pre>` 展示 `/api/files/:id/preview`。

## 5. 前端（web/src）规格

### 5.1 文件页（FilesView.vue）

- **上传改造**：每个上传项独立 task；统一调度器控制**全局并发 4 个分片**（队列 + 4 个 worker）；每项显示整体进度（已传分片/总分片）与状态（准备中/上传中/校验中/已完成/已暂停/失败原因）。
- **分片**：size<=8MB（或单分片）→ 单分片直传（与阶段一相同路径，保证回归兼容）；否则 chunkSize 取 4MB（在 2–8MB 内），循环上传各分片；**分片失败重试**（同分片最多 3 次，指数退避）。
- **暂停/继续**：每项「暂停/继续」按钮；暂停=停止调度新分片并 abort 进行中的请求；继续=先 GET status 拿 uploadedChunks，只传缺失分片（断点续传），再 complete。
- **秒传**：上传前用 WebCrypto 分块计算 sha256（`crypto.subtle.digest('SHA-256', ...)`；<=32MB 直接整体算，>32MB 按 8MB 块滚动算——WebCrypto 无 MD5，故用 sha256 作秒传键，后端 sha256 兜底路径天然支持）；POST /api/files/check，命中直接提示「秒传成功」并刷新列表（不落盘）。
- **文件夹上传**：上传按钮旁增加「上传文件夹」按钮（`webkitdirectory`）；多文件支持拖拽（含目录拖拽，webkitGetAsEntry 递归收集相对路径，无目录拖拽支持时回退：用 dataTransfer.items 的 webkitRelativePath）。每个文件 init 时带 `dir`。
- **分享**：每行「分享」按钮 → 分享对话框（有效期小时数、最大下载次数、生成后展示 `url` 并复制按钮、打开分享页按钮、「撤销分享」按钮）。文件已被分享时列表项显示分享状态标记。
- **预览**：每行「预览」按钮（可预览类型）；弹窗内嵌展示。
- 保留：冲突弹窗、下载、删除、搜索、分页、配额条、多语言、主题色、品牌。

### 5.2 匿名分享页（新路由 /:token）

- `router.js` 增加 `{ path: '/:token', component: ShareView, meta: { public: true, share: true } }`（置于 catch-all 之前；beforeEach 中 `to.meta.share` 直接放行，登录与否都可见）。
- ShareView：调用 `GET /api/files/shared/:token/meta` 展示文件名/大小/过期时间/剩余次数；下载按钮（`GET /api/files/shared/:token/download`）；可预览类型内嵌预览（img/video/iframe 用 download URL）。token 无效/过期显示错误文案。使用未登录 i18n（defaultLang）。

### 5.3 登录页（LoginView.vue）

- 登录表单下方：`GET /api/brand` 的 registerEnabled 为 true 时显示「注册账号」切换；注册表单（用户名/密码/确认密码 + 密码策略提示）；成功注册后直接进入 `/`。

### 5.4 管理页（AdminView.vue）

- 统计卡片增加「分享链接」：分享数 / 累计下载。
- 设置面板增加：**注册开关**（checkbox/switch）、**上传限速**（数字输入，KB/s，0=不限）；保存走现有 PUT /api/admin/settings。

### 5.5 i18n（i18n.js 三字典）

新增键（必须三语齐全，风格与现有键一致）：
- `files.uploadFolder`（上传文件夹）、`files.instantUpload`（秒传成功）、`files.paused`（已暂停）、`files.resume`（继续）、`files.pause`（暂停）、`files.chunks`（{done}/{total} 分片）、`files.preview`（预览）、`files.share`（分享）、`files.shareDialog`（分享文件）、`files.shareExpiresHours`（有效期（小时））、`files.shareMaxDownloads`（最大下载次数（0=不限））、`files.shareUrl`（分享链接）、`files.shareCopied`（链接已复制）、`files.revokeShares`（撤销分享）、`files.shared`（已分享）、`files.openShare`（打开分享页）
- `share.heading`（文件分享）、`share.download`（下载文件）、`share.expired`（分享链接已过期）、`share.limitReached`（分享次数已用完）、`share.notFound`（分享不存在）、`share.downloads`（已下载 {count} 次）、`share.availableDownloads`（剩余 {count} 次）、`share.expiresAt`（有效期至）
- `login.register`（注册账号）、`login.registerCopy`（还没有账号？立即注册）、`login.registerSubmit`（注册并登录）、`login.confirmPassword`（确认密码）、`login.passwordMismatch`（两次输入的密码不一致）、`login.hasAccount`（已有账号？返回登录）
- `admin.registerEnabled`（开放注册）、`admin.registerEnabledCopy`（允许未登录用户自助注册普通账号）、`admin.uploadRateLimit`（上传限速（KB/s））、`admin.uploadRateLimitCopy`（0 表示不限速）、`admin.shares`（分享链接）
- `error.registerDisabled`（注册功能未开放）、`error.shareExpired`（分享链接已过期）、`error.shareLimit`（分享次数已用完）、`error.shareNotFound`（分享不存在）、`error.invalidShareHours`（分享有效期无效）、`error.invalidShareMax`（分享次数限制无效）、`error.invalidDir`（目录无效）、`error.invalidChunkSize`（分片大小必须在 2MB-8MB 之间）、`error.invalidRateLimit`（上传限速无效）
- `logReason.shareExpired`（分享已过期）、`logReason.shareLimit`（次数已用完）、`logReason.shareNotFound`（分享不存在）
- api.js 的 messageKeys/codeKeys 增加：`注册功能未开放` → error.registerDisabled；`分享链接已过期` → error.shareExpired；`分享次数已用完` → error.shareLimit；`分享不存在` → error.shareNotFound；`REGISTER_DISABLED` → error.registerDisabled；`分片大小必须在 2MB-8MB 之间`/`目录无效`/`上传限速无效`/`分享有效期无效`/`分享次数限制无效` 对应键。

## 6. 实现要求

1. 阶段一全部现有功能、接口与测试不得回归：单分片上传（含 0 字节）、冲突 rename/overwrite、配额、R-DISK、R-VALID、日志、锁定、TOTP、IP 白名单、品牌、多语言、主题色、强制改密、运维命令、服务日志、反代。
2. 后端提供 `go test ./...` 单元测试覆盖新逻辑关键路径（分片大小/索引校验、status 续传、秒传命中、分享过期/次数、预览白名单、限速设置、注册开关、目录校验）。
3. 自测冒烟用 18081 端口 + 独立数据目录，全部走通后再交付（参考阶段一 CODEX_TASK 冒烟方式：登录→上传→列表→下载→删除 + 阶段二新接口）。
4. 保持前端构建可过：`npm --prefix web run build`；构建产物由 `go run ./scripts/sync-web.go` 同步到 `internal/webassets/dist`。
5. 中英双语注释：所有新增导出符号与关键业务逻辑（分片合并、秒传、分享鉴权、限速、预览白名单、注册、目录校验）必须中英注释齐全。

## 7. 验收标准（来自 DEV_DOC 阶段二 + 本文件细化）

1. **1GB 文件断点续传**：分片上传（如 4MB/256 片）上传部分分片后 GET status 确认缺片，中断后继续只传剩余分片，complete 后文件完整、md5/sha256 与本地 `Get-FileHash` 一致；
2. **秒传命中不重复落盘**：同 md5/sha256+size 二次 check 返回已有记录（instant:true），磁盘 data/files 文件数不增；
3. **文件夹上传保留相对路径结构**：`data/files/<user>/<yy>/<mm>/<相对目录>/<name>`；不同相对目录同名文件不加序号、各自独立；
4. **分享链接**：过期后 meta/download 拒绝（404/403 明确文案）；超次数拒绝；有效期内匿名下载正常（内容与本地一致）；撤销后 404；
5. **预览白名单**：image/text/video/pdf 白名单内 inline（Content-Disposition: inline），非白名单（如 .zip/.exe）attachment；
6. **超配额上传被拒**（403 配额错误，含多分片路径）；
7. **限速生效**：settings 设置小速率（如 64KB/s）后连续上传被限流（上传耗时 >= 理论值 或 WaitN 生效）；恢复 0 后不限；
8. **注册开关**：关时 register 403「注册功能未开放」；开时可注册成功并直接登录；brand.registerEnabled 同步；
9. **系统统计**：admin stats 含 shares/shareDownloads，数值与真实分享一致；
10. **回归**：阶段一全部功能仍正常（登录/上传/下载/删除/冲突/日志/锁定/TOTP/品牌/多语言/主题色/磁盘保护）；
11. **构建**：Windows 与 Linux（GOOS=linux GOARCH=amd64）均编译通过；前端构建+embed 同步通过；
12. 审计日志包含 share/share_view/share_download/register 动作；服务日志含对应事件（operator+ip）。

## 8. 交付物

- 代码（后端 + 前端）与单元测试；构建产物 `bin/filebox.exe`（Windows）、`bin/filebox-linux`（Linux 交叉编译）
- `docs/requirements/STATE.md` 阶段二条目标记 done；`docs/requirements/CHANGELOG.md` 记录本次开发批次
- 测试脚本（PowerShell，UTF-8 BOM）与测试报告（阶段二 TEST_PLAN/TEST_REPORT 更新）
- README/README.en/RELEASE_NOTES/RELEASE_NOTES.en 更新至 v0.2.0

## 9. 已知边界与约定

- 秒传仅限同用户（跨用户不秒传）；秒传匹配 md5 优先、sha256 兜底，size 必须一致
- 分享 token 64 位随机；meta 接口不泄露 owner 内部信息（createdBy 用用户名）
- 限速按用户而非 IP；速率 0=不限（默认）；限速仅作用于分片上传写入
- 注册用户默认配额 100GB、role=user；注册成功直接签发 JWT
- 分片并发由前端控制（默认 4）；服务端不限制并发分片数，但每片校验 index/size/哈希
