# FileBox 文件传输系统 — 开发文档 v0.1（待确认）

## 1. 项目概述

一个公网部署的 Web 文件传输系统：多用户登录、文件上传/下载/管理、断点续传、秒传、文件夹上传、分享链接、在线预览、配额与限速，带独立的登录界面和独立的管理员账号权限管理界面。

**用户新增需求（已确认纳入）**：
- **R-GH（GitHub 集成）**：代码开发完成后推送到 GitHub，创建新的远程仓库，便于版本管理与协作。
- **R-MD5（文件 md5 字段）**：文件传输完成后为文件记录增加并保存 `md5` 字段（随 API 返回，可对外提供完整性校验）。
- **R-DISK（磁盘监控与系统保护）**：管理员统计卡片增加存储目录所在磁盘的占用大小/比例；系统可用空间低于阈值（默认 2GB）时禁止用户上传，防止磁盘写满。
- **R-NAME（磁盘保留原文件名）**：落盘文件名不再使用 UUID，而是使用消毒后的原文件名，后台可直接识别；md5/sha256 仍存数据库供快速核对。**重名规则：仅同一目录内重名才加序号（如 `a (1).txt`）；不同目录（不同用户 / 不同自定义目录 / 不同相对路径）下的同名文件各自独立存储，不加序号。**（v011 起按用户分目录，不再按上传日期自动分目录。）
- **R-CONFLICT（同名冲突策略）**：同一目录下出现同名文件时，上传前由用户弹窗选择「**覆盖**」或「**重命名**」（默认重命名，自动加序号）。**覆盖后数据库记录同步更新**：旧文件记录删除（软删除+物理删除）、旧文件磁盘文件删除、新记录入库、用户 used_bytes 差额校正。不同目录同名不触发弹窗。
- **R-VALID（非法字符前置校验）**：文件名含非法字符（路径分隔符、控制字符、Windows 非法字符 `<>:"|?*`、超长等）时，**upload-init 阶段即返回 400 拒绝**，不进入上传流程。
- **R-LOG（操作审计日志）**：记录 登录（成功/失败）、上传、下载、分享创建、分享查看、分享下载 等操作：时间、用户、来源 IP、操作类型、结果（成功/失败）、失败原因。管理员可查看全部用户日志并可**设置日志留存周期（默认 30 天），超过周期自动清理**；普通用户可查看**自己的**日志。
- **R-LOCK（登录安全）**：连续登录失败达到阈值（默认 5 次）禁用该用户（可配置，0=关闭）；支持自动解禁（默认开启，5 分钟后解除）。**登录失败提示统一**（「用户名或密码错误」，不区分用户是否存在，防账号枚举爆破）；登录日志中记录**具体失败原因**（用户不存在/密码错误/账号已禁用/登录已锁定），该原因仅在登录成功后的日志界面可见，不在登录页显示。
- **R-BRAND（品牌定制）**：管理员可自主设置/恢复默认：网页 title、网站描述（SEO description）、网页 favicon（ico）、登录页 logo、主页 logo、备案信息（**ICP 备案号与公安备案号，均可为空，默认即为空**）。设置后全站（登录页、文件页、管理页、分享页）统一生效；恢复默认即回退内置 FileBox 品牌。**前端空值不留空白**：备案号、描述等为空时对应区域不渲染（不留空白占位）。
- **R-LANG（多语言）**：管理员可设置**系统默认语言**，个人可设置自己的语言（不设置则跟随系统默认）。支持**简体中文（默认）/ 繁体中文 / 英文**。登录后按用户设置的语言显示；**用户修改语言后自动生效，无需重新登录**（管理员修改品牌等前端属性保存后同样即时生效，无需重新登录）。**前端全部文案（登录/文件/管理/日志/品牌/冲突弹窗/提示等）做语言转化**；后端错误消息由前端按状态码/错误码映射本地化文案（无法映射时回退显示后端消息）。
- **R-THEME（界面主题色）**：管理员可设置 Web 界面主色，支持**输入 RGB 色号（十六进制）或弹出色盘选择**，同样支持**重置为默认**；设置后**无需重新登录即时生效**（CSS 变量方式，全站统一应用：按钮、链接、进度条、焦点、选中态等主色元素）。
- **R-INIT（初始账号与强制改密）**：首次部署支持参数指定初始管理员账号密码（`--admin-user` / `--admin-pass`，默认 admin/admin123）；初始账号**登录后强制修改密码**（改密前不可使用其他功能），支持后续在后台重置管理员密码。
- **R-PWD（密码强度策略）**：管理员可设置普通用户密码强度要求：最小长度（默认 8 位）、复杂度（默认：大写/小写/数字/特殊字符四类中**至少 3 类**，0=不要求）；应用于创建用户、重置密码、改密；管理界面显示策略提示。
- **R-TOTP（双因素认证）**：管理员可为用户开启 TOTP 双因素认证；**用户首次登录时显示二维码与密钥字符串**（绑定验证通过后生效），此后登录需输入 6 位 TOTP 动态码（两步登录：密码 → 动态码）。TOTP 按 RFC 6238 实现（HMAC-SHA1、30 秒、±1 窗口），密钥服务端加密保存。
- **R-IPACL（来源 IP 白名单）**：管理员可为用户启用来源 IP 限制并设置 IP 白名单（支持单个 IP 与 CIDR，IPv4/IPv6）；**不开启则不限制**。开启后该用户所有请求来源 IP 不在白名单 → 403。
- **R-IPBAN（IP 级登录锁定）**：登录安全增加 **IP 维度锁定**：10 分钟内连续登录失败 50 次锁定该来源 IP（**不区分登录账号**）；时间窗口、锁定阈值、是否自动解禁、解禁周期均可由管理员设置（默认：窗口 10 分钟、阈值 50 次、自动解禁开、解禁 30 分钟）。
- **R-LOCKADMIN（锁定管理）**：管理后台可**查看与删除锁定信息**：IP 锁定记录（IP/失败次数/锁定截止/状态）与用户锁定记录（用户名/失败次数/锁定截止），支持手动解除。
- **R-OPS（后台命令行运维）**：在 Web 之外提供**命令行运维命令**（Linux/Windows 后台均可执行，服务停止或 Web 不可用时的兜底通道）：**重置管理员（或任意用户）密码**（重置后强制改密，支持指定新密码或自动生成并打印）、**查看与删除锁定信息**（IP 锁定与用户锁定）。同一单文件二进制，无需额外工具。
- **R-SRVLOG（服务日志）**：服务启动支持配置：**是否启用文件日志（默认否）**、**日志文件路径（默认程序执行目录下 logs 目录）**、**日志保留天数（默认 90 天）**。不启用时仅输出控制台；启用时输出服务启动-运行-退出的**详细日志**（至少包含用户登录/查看/上传/下载/分享、管理员配置、用户禁用/解禁等事件），**按天滚动并 gzip 压缩归档**，超保留天数自动清理。
- **R-SERVICE（服务化部署）**：支持将 FileBox 作为系统服务运行，**兼容 Linux 与 Windows**：提供 systemd unit 示例（Linux）与 Windows 服务注册方案（sc/NSSM），含部署说明文档（中英）。
- **R-PROXY（反向代理部署）**：考虑前端部署 Nginx 等反向代理的场景：提供 **`--trusted-proxies` 可信代理配置**（逗号分隔 IP/CIDR，默认空=直连模式忽略 X-Forwarded-For），来源 IP 从右向左解析首个非可信 IP（防止伪造 XFF 绕过 IP 锁定/白名单）；提供 **Nginx 配置示例**（HTTPS 终结、`client_max_body_size 0`、`proxy_request_buffering off` 流式上传、XFF 头传递、SPA `try_files`）；README 说明反代部署步骤与安全注意。

- **项目名**：FileBox（工作目录 `C:\Users\huangcp\dsh-project\filebox`）
- **部署形态**：编译为单个可执行文件（前端页面内嵌），Linux / Windows 均可直接运行，零外部运行时依赖，资源占用低
- **交付方式**：分两阶段（MVP → 增强），每阶段有验收标准

## 2. 技术选型

| 层 | 选型 | 理由 |
|---|---|---|
| 后端 | Go（标准库 `net/http`，Go 1.22+ 方法路由） | 单文件编译、跨平台交叉编译（GOOS=linux/windows）、内存占用低 |
| 数据库 | SQLite（`modernc.org/sqlite` 纯 Go 驱动） | 单文件存储、免部署；纯 Go 无 CGO，交叉编译无障碍 |
| 前端 | Vue3 + Vite + vue-router | 中文生态好、构建体积小，产物经 `embed` 打包进 Go 二进制 |
| 鉴权 | JWT（`golang-jwt/jwt/v5`）+ `x/crypto/bcrypt` | 标准方案 |
| 限速 | `golang.org/x/time/rate` 令牌桶 | 按用户限速（已实现） |

依赖保持最小集合（不引入重型框架），存储目录可配置（默认 `./data`）。

## 2.5 代码注释规范（中英双语）

- 所有**导出符号**（Go 导出函数/类型/常量、Vue 组件、JS 模块函数）与**关键业务逻辑**（鉴权、配额、冲突处理、锁定、日志、磁盘保护、品牌渲染等）的注释采用**中英双语**，格式统一为：
  ```go
  // 上传前校验文件名合法性，非法字符直接拒绝。
  // validateUploadName validates the upload file name and rejects illegal characters before upload.
  func validateUploadName(...)
  ```
- 中文在前、英文在后，分行或同行皆可，但同一文件内保持一致；简单行内注释可单语，但全库风格统一（不混用中英注释比例悬殊）。
- 注释只描述意图与约定，**不解释代码逐行抄写**；修改代码逻辑时同步更新注释。

## 3. 角色与权限

- **admin（管理员）**：登录后可见 `/admin` 管理界面；管理用户（创建/禁用/删除、改角色、设配额、重置密码）、查看系统统计
- **user（普通用户）**：登录后管理自己的文件（上传/下载/删除/分享/预览），受配额限制
- 未登录：只能访问登录页与分享链接（分享链接是匿名可下载的 token 链接，见 5.5）

## 4. 数据模型（SQLite 表）

```
users      id, username(unique), password_hash, role('admin'|'user'),
           quota_bytes, used_bytes, disabled, failed_attempts, locked_until,
           language(''=跟随系统|'zh-CN'|'zh-TW'|'en'),
           must_change_password, totp_secret(加密), totp_enabled,
           ip_acl_enabled, ip_whitelist(CIDR/单IP 逗号分隔),
           created_at, updated_at
ip_failures  ip, failed_count, window_started_at, locked_until   -- IP 级登录失败与锁定（R-IPBAN）
files      id, user_id, name, stored_name(消毒后原文件名), size, mime, sha256, md5,
           status('uploading'|'ready'|'deleted'), created_at, deleted_at
chunks     task_id, index, size, sha256   -- 分片记录（已实现）
upload_tasks  id, user_id, file_id, total_chunks, chunk_size, status,
              created_at, updated_at      -- 断点续传任务（已实现）
shares     id, file_id, token(随机64位), created_by, expires_at,
           download_count, max_downloads  -- 分享（已实现）
audit_logs id, user_id, username(快照), action('login'|'upload'|'download'|'share'|'share_view'|'share_download'|...),
           target(文件名/资源), ip, result('success'|'failure'), reason(具体失败原因),
           created_at                     -- 操作审计日志（R-LOG）
settings   key, value                     -- 注册开关/默认配额/上传上限/日志留存周期/锁定阈值/自动解禁等
```

磁盘存储（R-NAME，v011 起）：`data/files/<user_id>/[<自定义目录>/]<stored_name>`（**按用户分目录，用户自行创建自定义目录（支持中英文、多级路径，如 `工作文档/projects`）；不建目录时文件直接放用户根目录**；v011 起不再按上传时间自动生成 `<yy>/<mm>` 年月层）。`stored_name` = **消毒后的原文件名**（去除路径分隔符、控制字符与 Windows 非法字符 `<>:"|?*` 并替换为 `_`，限制 ≤255 字节）；**重名规则：仅当同一用户同一目录（含根目录）内已存在同名文件时，追加 ` (1)`、` (2)`… 序号后缀（不覆盖既有文件）；不同目录下的同名文件不加序号**。UUID 仅作 DB 主键与 API 资源 id，不落盘文件名。目录结构由 `folders` 表维护（user_id 归属、path 唯一、parent_id 父子），支持空目录创建/删除（非空保护）与重命名（级联更新子目录与文件路径、物理移动）；上传 API 的 `dir` 字段保留相对路径（多级可用），同名文件在不同相对目录下同样不加序号。分片临时目录 `data/tmp/<task_id>/<index>`。

示例：admin（`user_id=1`）上传文件到自定义目录「工作文档」落盘为 `data/files/1/工作文档/test.txt`；不建目录直接上传则落盘为 `data/files/1/test.txt`；同目录再次上传同名 `test.txt` 时自动分配序号后缀，落盘为 `data/files/1/工作文档/test (1).txt`。v010 遗留数据（`files/<uid>/<yy>/<mm>/*`）由 `filebox admin migrate-v010-paths` 迁移为历史目录 `files/<uid>/<yy>-<mm>/`（如 `2026-08`，保留时间语义；迁移前备份 DB 与目录、幂等可重复）。

示例：admin（`user_id=1`）上传文件到自定义目录「工作文档」落盘为 `data/files/1/工作文档/test.txt`；同目录再次上传同名 `test.txt` 时自动分配序号后缀，落盘为 `data/files/1/工作文档/test (1).txt`。

## 5. API 设计（RESTful，统一 `{code, message, data}`）

### 5.1 认证
- `POST /api/auth/login` → JWT（响应体返回 token，前端存 localStorage，请求带 `Authorization: Bearer`）。**登录安全（R-LOCK / R-TOTP / R-IPBAN）**：
  - 失败时**统一返回**「用户名或密码错误」（401），不区分用户是否存在/密码错误；
  - 记录登录日志（成功/失败），失败日志带**具体原因**（`user_not_found`/`wrong_password`/`user_disabled`/`locked`/`ip_locked`/`totp_failed`），仅日志界面可见；
  - 连续失败达阈值（默认 5 次，settings 可配，0=关闭）→ 该用户锁定（`locked_until`）；自动解禁开关默认开启、默认 5 分钟（settings 可配）；
  - **IP 维度（R-IPBAN）**：任意账号登录失败均累计到来源 IP（`ip_failures`），窗口（默认 10 分钟）内失败 ≥ 阈值（默认 50 次）→ 锁定该 IP（自动解禁默认开、30 分钟，均可配）；锁定期间该 IP 任何登录请求返回统一 401；
  - 被锁定时登录返回 401「用户名或密码错误」，日志记 `locked`/`ip_locked`；锁定期满自动恢复。
  - **TOTP（R-TOTP）**：密码正确后若用户已启用 TOTP → 返回 `totpRequired:true` + 临时挑战 token（5 分钟有效，仅用于第二步）；`POST /api/auth/totp`（临时 token + 6 位码）校验通过后签发正式 JWT。首次启用（未验证绑定）时响应 `totpSetup:true` + `otpauthUrl` + `secret`（base32 明文仅此一次返回），前端显示**二维码与密钥字符串**，输入动态码完成绑定后启用。
- `POST /api/auth/change-password`（登录后）：旧密码 + 新密码（按 R-PWD 强度校验），成功后清除 `must_change_password` 并重签 JWT；`must_change_password=true` 时**强制改密**：除登录/改密/登出外所有接口返回 403（前端强制跳转改密页）。
- `POST /api/auth/logout`（客户端清除 token）
- `GET  /api/auth/me` → 当前用户信息（含角色、配额用量、**language**、**mustChangePassword**、**totpEnabled**、**ipAclEnabled**）
- `PUT  /api/auth/language` → `{language: 'zh-CN'|'zh-TW'|'en'|''}`（空=跟随系统默认）；保存后前端立即切换，无需重新登录
- `POST /api/auth/register`（已实现：受 settings 注册开关控制，默认关；开启后按密码策略创建普通用户并直接登录）

### 5.2 文件管理
- `GET    /api/files?page=&pageSize=&keyword=` → 分页列表（本人文件，含大小/时间/状态/md5）
- `POST   /api/files/upload-init` → `{name, size, chunkSize, sha256?, md5?}` 创建上传任务，返回 `taskId`。**前置校验（R-VALID）**：文件名含非法字符（路径分隔符/控制字符/Windows 非法字符 `<>:"|?*`/超长）→ **400**「文件名包含非法字符，禁止上传」；**系统保护（R-DISK）**：可用空间 < `--min-free-space`（默认 2GB）→ `503`「系统存储空间不足，暂时禁止上传」。**冲突检测（R-CONFLICT）**：同目录已存在同名文件 → 响应 `conflict: true`（含既有文件信息），前端弹窗由用户选择；无冲突时正常返回 taskId
- `PUT    /api/files/:taskId/chunks/:index` → 上传一个分片（阶段一：index=0 整体传）
- `POST   /api/files/:taskId/complete` → 请求体含 `{action: 'overwrite'|'rename'}`（仅当 init 返回 conflict 时必填）：`rename` 自动分配 ` (1)` 序号名；`overwrite` 删除同目录同名旧文件（软删除+物理删除+used_bytes 差额校正）后以原文件名落盘。校验分片齐备/哈希，合并落盘，**计算并保存 md5 与 sha256**，返回文件记录（含 md5）
- `GET    /api/files/:taskId/status` → 已上传分片列表（断点续传用）
- `POST   /api/files/check` → `{sha256, md5?, size}` 秒传判定（md5 优先，sha256 兜底），命中直接返回文件记录
- `GET    /api/files/:id/download` → 流式下载（`Content-Disposition: attachment`，支持 Range 206）
- `GET    /api/files/:id/preview` → 在线预览（按 MIME 白名单输出 inline）
- `DELETE /api/files/:id` → 删除（软删除 + 物理清理）

### 5.3 分享链接（已实现）
- `POST /api/files/:id/share` → `{expiresInHours, maxDownloads?}` 生成分享
- `GET  /api/files/shared/:token/meta` → 分享元数据（匿名）
- `GET  /api/files/shared/:token/download` → 匿名下载（校验有效期/次数）
- `DELETE /api/files/:id/shares` → 撤销该文件全部分享

### 5.4 管理（仅 admin）
- `GET  /api/admin/users` → 用户列表（含锁定状态、TOTP 状态、IP 白名单状态）
- `POST /api/admin/users` → 创建用户（用户名/密码（按 R-PWD 校验）/角色/配额）
- `PUT  /api/admin/users/:id` → 改角色/配额/禁用/重置密码（按 R-PWD 校验，重置后 must_change_password=true）/解除锁定（清 failed_attempts+locked_until）
- `PUT  /api/admin/users/:id/totp` → `{enabled: true|false}` 为用户开启/关闭 TOTP（开启生成新 secret；关闭清除）
- `PUT  /api/admin/users/:id/ip-acl` → `{enabled, whitelist: "1.2.3.4,10.0.0.0/8,..."}` 设置来源 IP 白名单（R-IPACL；格式校验，空=不限制）
- `DELETE /api/admin/users/:id` → 删除用户（其文件一并处理）
- `GET  /api/admin/stats` → 用户数、文件数、总用量、**磁盘信息 `{diskTotal, diskUsed, diskFree, diskUsagePercent}`**（存储目录所在磁盘，管理员统计卡片展示占用大小与比例）
- **锁定管理（R-LOCKADMIN）**：`GET /api/admin/locks` → `{ipLocks:[{ip, failedCount, windowStartedAt, lockedUntil, autoUnlock}], userLocks:[{id, username, failedAttempts, lockedUntil}]}`；`DELETE /api/admin/locks/ip/{ip}`、`DELETE /api/admin/locks/user/{id}` 手动解除对应锁定

### 5.5 操作日志（R-LOG）
- `GET /api/logs?action=&result=&keyword=&page=&pageSize=` → 日志列表；普通用户仅返回自己的；admin 可带 `userId=` 查看任意用户或全部
- `GET /api/logs/actions` → 可选操作类型枚举
- 日志字段：时间、用户（username）、操作（登录/上传/下载/分享创建/分享查看/分享下载）、目标（文件名等）、来源 IP、结果、失败原因（仅成功登录后可见）
- **留存周期**：`GET /api/admin/settings` / `PUT /api/admin/settings` → `{logRetentionDays(默认30), lockThreshold(默认5), autoUnlockEnabled(默认true), autoUnlockMinutes(默认5), defaultLang('zh-CN'|'zh-TW'|'en', 默认'zh-CN'), themeColor('#RRGGBB', 默认'#1b998b', 空/重置=默认), passwordMinLength(默认8), passwordComplexity(默认3, 0=不要求), ipLockWindowMinutes(默认10), ipLockThreshold(默认50, 0=关闭), ipAutoUnlockEnabled(默认true), ipUnlockMinutes(默认30)}`；日志写入时惰性清理过期记录（超过周期自动删除）
- 来源 IP：优先取 `X-Forwarded-For` 首项（公网反代场景），否则 `RemoteAddr`；日志脱敏（不记录密码/token/文件内容）

### 5.6 分享页（前端路由）
- `/:token` 匿名分享页（展示文件名/大小/预览，提供下载按钮）——无需登录；查看与下载行为记入分享日志（R-LOG）

### 5.7 品牌定制（R-BRAND）
- `GET  /api/brand`（**公开**，登录页/分享页未登录时可用）→ `{siteTitle, siteDescription, icpText, policeText, hasFavicon, hasLoginLogo, hasMainLogo, defaultLang, themeColor}`（defaultLang 供未登录页面确定系统默认语言；themeColor 供全站主色应用；全部可为空，默认 title=「FileBox 文件管理」、defaultLang='zh-CN'、themeColor='#1b998b'、其余空）
- `GET  /brand/favicon`、`/brand/login-logo`、`/brand/main-logo`（**公开**）：输出管理员上传的资源；未设置时输出内置默认资源（favicon 内置 .ico/.svg，logo 内置 SVG）
- `PUT  /api/admin/brand`（仅 admin，`multipart/form-data`）：字段 `siteTitle`（≤64 字）、`siteDescription`（≤200 字，SEO description）、`icpText`（≤128 字，**可空**）、`policeText`（≤128 字，**公安备案号，可空**）、`favicon`（.ico/.png/.svg，≤512KB）、`loginLogo`、`mainLogo`（png/jpg/svg，≤512KB）；缺省字段保持原值；`reset=true` 时清空全部自定义回退默认
- 资源落盘：`data/brand/favicon.ico|favicon.svg`、`login-logo.*`、`main-logo.*`（固定文件名，按上传扩展名覆盖，防路径穿越）；settings 表记录 `brand_title`、`brand_description`、`brand_icp`、`brand_police`、`brand_favicon`、`brand_login_logo`、`brand_main_logo` 标记
- **SEO**：`index.html` 与运行时同步更新 `<title>`、`<meta name="description">`（siteDescription）、`<meta name="keywords">`（可由 title/描述派生或留空不渲染）；`<link rel="icon">` 指向 `/brand/favicon`
- 前端渲染（**空值不留空白**）：登录页与主页 logo 区（无自定义时用内置 logo 正常显示，不算空白）；页脚备案区 **仅当 icpText 或 policeText 非空时渲染**（两者都为空则整个备案区不出现，不留空行）；siteDescription 为空时 meta description 不输出

### 5.8 多语言（R-LANG）
- 语言取值：`zh-CN`（简体中文，默认）/ `zh-TW`（繁体中文）/ `en`（英文）；用户 `language=''` 表示跟随系统默认（settings.defaultLang，默认 zh-CN）。
- 语言解析优先级（前端）：本地已保存用户语言 → `GET /api/auth/me` 的 `language`（非空）→ `GET /api/brand` 的 `defaultLang` → `zh-CN`。
- `PUT /api/auth/language` 保存后立即切换（更新 localStorage + 前端 locale + 重渲染），**无需重新登录**；未登录页面（登录页/分享页）用 defaultLang。
- 前端 i18n：`web/src/i18n.js`（或同风格轻量模块）维护 `zh-CN / zh-TW / en` 三份文案字典与 `t(key)` 翻译函数，覆盖**全部 UI 文案**（导航、按钮、表单、表格、弹窗、上传状态、错误提示、品牌面板、日志页、日期格式等）；不引入重型 i18n 依赖，保持现有轻量风格。
- 后端错误消息：前端按 `HTTP 状态码 + 错误码（DISK_FULL 等）+ 常见 message` 建立本地化映射表，映射不到时回退显示后端原始消息（中文兜底）；后端 API 保持中文 message 不变（避免双端维护爆炸）。

### 5.10 高级账号安全（R-INIT / R-PWD / R-TOTP / R-IPACL / R-IPBAN / R-LOCKADMIN）
- **强制改密（R-INIT）**：`must_change_password=true` 的用户，除 `/api/auth/me`、`/api/auth/change-password`、`/api/auth/logout` 外全部接口返回 403（错误码 `PASSWORD_CHANGE_REQUIRED`）；改密成功后清除标记并重签 JWT。首次部署：`--admin-user`（默认 admin）与 `--admin-pass`（默认 admin123）指定初始管理员，创建时 must_change_password=true。
- **密码强度（R-PWD）**：校验函数按 settings：长度 ≥ passwordMinLength；字符类别（大写/小写/数字/特殊）计数 ≥ passwordComplexity（默认 3；0=不校验类别）。应用于创建用户、管理员重置密码、用户改密（管理员账号同样建议但不强制，admin 重置自己的密码也校验）。前端表单显示当前策略提示（长度与类别要求）。
- **TOTP（R-TOTP）**：secret 生成后经密钥（--jwt-secret 派生）加密存库；`otpauth://totp/FileBox:<user>?secret=...&issuer=FileBox` 生成二维码 PNG（后端返回 data URI 或 base64，前端 `<img>` 展示）+ secret base32 字符串（明文仅绑定页返回一次）；绑定验证通过后 `totp_enabled=true`。登录流程两步（见 5.1）。校验 RFC 6238：HMAC-SHA1、30 秒步长、允许 ±1 窗口、防重放（同一码 60 秒内不可复用）。
- **IP 白名单（R-IPACL）**：`ip_acl_enabled=true` 时，requireAuth 中间件校验请求来源 IP（X-Forwarded-For 首项或 RemoteAddr）匹配 `ip_whitelist`（单 IP 或 CIDR）；不匹配 → 403「当前 IP 不在白名单」。**不开启则不校验**。来源 IP 解析与审计日志一致。
- **IP 锁定（R-IPBAN）**：登录失败（含用户不存在）均累计 `ip_failures`；窗口滑动（window_started_at 距今 > windowMinutes 则重置计数）；计数 ≥ ipLockThreshold → locked_until（自动解禁开=now+ipUnlockMinutes，关=9999）；登录前检查 IP 锁定 → 401 统一文案 + 日志 reason `ip_locked`。登录成功时清除该 IP 的失败计数。
- **锁定管理（R-LOCKADMIN）**：管理后台「锁定管理」面板：IP 锁定表（IP/失败次数/窗口起点/锁定截止/状态）与用户锁定表，各带「解除」按钮；解除 IP 锁定=清 ip_failures 记录，解除用户锁定=清 failed_attempts/locked_until。

### 5.9 界面主题色（R-THEME）
- 存储：settings `theme_color`（`#RRGGBB`，默认 `#1b998b`；`''`=默认）。
- 设置/重置：`PUT /api/admin/settings` 的 `themeColor` 字段（非法色值 400；`''` 或 `resetTheme=true` 恢复默认）；公开读取：`GET /api/brand` 返回 `themeColor`（未登录页面同样应用）。
- 前端实现：样式中的主色硬编码（按钮、链接、进度条、焦点环、选中态、状态标签等，如 `#1b998b` 及其衍生透明色）统一替换为 CSS 变量 `--brand-color` / `--brand-color-strong` / `--brand-color-soft`（`:root` 定义默认值）；启动时按 `GET /api/brand` 的 themeColor 应用（`document.documentElement.style.setProperty`），**保存/重置后立即生效，无需刷新或重新登录**。
- 管理后台品牌面板新增「界面主色」项：色值输入框（支持 `#RGB`/`#RRGGBB` 校验）+ 原生色盘 `<input type="color">` + 「恢复默认」按钮。

## 6. 前端页面清单（Vue3，多语言界面 R-LANG）

**语言选择器**：登录页右上角与登录后顶栏用户区各提供语言切换下拉（简体中文/繁体中文/英文）；修改后立即生效（无需重新登录），选择持久化（登录用户存服务端 language 字段 + localStorage；未登录仅存 localStorage）。全部页面文案经 i18n 字典渲染。

| 路由 | 页面 | 说明 |
|---|---|---|
| `/login` | 独立登录界面 | 用户名/密码；含"记住我"；无其他入口；**登录失败统一提示「用户名或密码错误」（不区分用户是否存在）**；**TOTP 用户第二步输入 6 位动态码；首次启用显示二维码与密钥字符串完成绑定** |
| `/change-password` | 强制改密页 | 仅 must_change_password 用户可进入（其余接口 403 时前端强制跳转）：旧密码 + 新密码 + 确认新密码（按 R-PWD 策略校验并显示要求） |
| `/` | 文件管理 | 文件列表（分页/关键字搜索）、上传按钮+拖拽上传（支持多文件与文件夹）、整体进度条、暂停/继续、秒传提示、下载、删除、分享、预览弹窗、**同名冲突弹窗（覆盖/重命名，覆盖时提示将替换旧文件并同步更新记录）** |
| `/logs` | 操作日志 | 时间/用户/操作/目标/来源 IP/结果/失败原因；普通用户仅见自己，admin 可按用户筛选；admin 另含日志留存周期设置面板 |
| `/admin` | 独立管理员后台 | 用户列表（搜索/分页）、创建用户、编辑（角色/配额/禁用/重置密码）、删除、系统统计卡片（用户数/文件数/已用空间 + **磁盘占用大小与比例**，磁盘使用率过高时显示警告色）、**品牌设置面板（R-BRAND）**、**界面主色（R-THEME）**、**用户安全编辑（R-TOTP 开关 + IP 白名单设置）**、**锁定管理面板（R-LOCKADMIN：IP 锁定与用户锁定列表 + 解除按钮）**、**安全设置（R-PWD/R-IPBAN：密码强度、IP 锁定窗口/阈值/自动解禁/解禁周期）** |
| `/:token` | 匿名分享页 | 下载 + 预览 |

布局：登录/管理界面与用户主界面在视觉上分区明确（管理入口仅 admin 可见）。

## 7. 安全基线

- 密码 bcrypt；JWT 有效期（默认 7 天）
- 所有文件接口做归属校验（只能操作自己的文件；admin 除外）
- **登录防爆破（R-LOCK）**：登录失败提示统一（防账号枚举）；连续失败锁定 + 自动解禁（可配）；锁定与失败计数对不存在的用户不产生副作用（不存在的用户名不累积状态，仅记日志）
- 上传文件名**前置校验（R-VALID）**：非法字符在 upload-init 即 400 拒绝；消毒规则（R-NAME）：去除路径分隔符/控制字符/Windows 非法字符 `<>:"|?*`，存储名 = 消毒后原文件名（**仅同目录重名才由用户选择覆盖/重命名，不同目录同名不加序号**，不覆盖），下载时重新拼 Content-Disposition；DB 记录 `name`（原始名）与 `stored_name`（落盘名）
- 文件路径一律由 DB 记录解析，禁止拼接用户输入（防路径穿越）
- 上传大小上限可配置（默认单文件 100GB、单用户配额 100GB）；分片哈希校验；**md5 与 sha256 双哈希并存**：传输完成后计算保存两者，校验优先 md5（文件记录字段，前端可在下载后校验一致性）
- 预览 MIME 白名单：image/*（png/jpg/gif/webp）、text/*（txt/md/log/json/csv 等 UTF-8）、video/*（mp4/webm）、application/pdf；错误信息不泄露内部路径
- 响应头：`X-Content-Type-Options: nosniff`、CSP 基础头
- 管理接口额外校验 admin 角色
- **审计日志（R-LOG）**：登录/上传/下载/分享操作全量记录（时间/用户/来源 IP/操作/结果/原因）；日志不记录密码、token、文件内容；留存周期可配，超期自动清理
- **品牌资源（R-BRAND）**：logo/favicon 上传限制类型与大小（png/jpg/svg/ico ≤512KB）；固定文件名落盘防路径穿越；`/api/brand` 与品牌资源路由为公开只读接口（不泄露敏感信息）

## 8. 限速与配额（已实现）

- 配额：上传前检查 `used_bytes + size <= quota_bytes`，超额返回 403 + 结构化错误；用量实时累计
- 限速：每用户令牌桶（`x/time/rate`），速率可在 settings 配置（默认不限）
- 并发：分片并发上限（默认 4），服务端合并流式写入避免内存暴涨

## 8.5 系统保护与磁盘监控（R-DISK，阶段一即实现）

- **磁盘占用统计**：跨平台获取存储目录所在文件系统容量（Windows：`GetDiskFreeSpaceEx`；Linux：`syscall.Statfs`），`GET /api/admin/stats` 返回 `diskTotal/diskUsed/diskFree/diskUsagePercent`；管理后台统计卡片展示「磁盘占用 X GB / Y GB（Z%）」。
- **上传保护**：每次 `upload-init` 前检查磁盘可用空间，`可用空间 < --min-free-space`（默认 2GB，可配置，`0` 表示关闭）→ 拒绝上传，返回 `503`，错误码 `DISK_FULL`，提示「系统存储空间不足，暂时禁止上传」。
- 保护不阻断下载/删除/管理操作；磁盘恢复后自动放行（每次请求实时探测，无需重启）。

## 9. 构建与运行

```bash
# 开发模式
cd filebox
make dev          # 后端 :8080（代理前端 vite :5173）
# 或分别：go run ./cmd/filebox 与 cd web && npm run dev

# 生产构建（单文件）
make build        # 输出 bin/filebox.exe（Windows）
make build-linux  # GOOS=linux 交叉编译，输出 bin/filebox-linux

# 运行
./filebox --addr=:8080 --data=./data --admin-user=admin --admin-pass=admin123  # 首次启动自动创建管理员（登录后强制改密）
```

- README 写明：环境要求（Go 1.22+、Node 20+）、开发/构建/运行步骤、配置项（端口、数据目录、单文件上限、注册开关、JWT 过期）
- 前端 `web/` 独立包（package.json）；后端 `cmd/filebox` + `internal/`（handler/service/store 分层）
- 配置文件：`.env` / 命令行 flag（`--addr`、`--data`、`--max-file-size`、`--min-free-space`（默认 2GB，0=关闭）、`--register-enabled`、`--admin-user`（默认 admin）、`--admin-pass`（默认 admin123，仅首次创建时生效）、`--log-enabled`（默认 false）、`--log-dir`（默认程序执行目录下 `logs/`）、`--log-retention-days`（默认 90））

## 9.7 服务日志（R-SRVLOG）

- 配置：`--log-enabled`（默认否）、`--log-dir`（默认 `<程序执行目录>/logs`，可配置）、`--log-retention-days`（默认 90）。
- **不启用**：所有日志仅输出控制台（stdout/stderr），行为与现有一致。
- **启用**：同时输出控制台与文件；文件**按天滚动**：`logs/filebox-YYYY-MM-DD.log`，跨天自动切换；**前一天文件自动 gzip 压缩归档**为 `filebox-YYYY-MM-DD.log.gz`；启动时与写入时惰性清理超过保留天数的日志与归档。
- 详细事件日志（服务日志，独立于 DB 审计日志，至少覆盖）：
  - 启动/退出：版本、监听地址、数据目录、配置摘要、优雅退出；
  - 用户：登录成功/失败（用户名+来源 IP）、登出、查看（文件列表/详情/预览）、上传完成（文件名+大小）、下载（文件名+结果）、分享相关（share_create/share_view/share_download，已实现）；
  - 管理：管理员配置变更（settings/brand/语言/主题色）、用户创建/修改/**禁用/解禁**、重置密码、锁定与解锁（用户/IP）、运维命令执行记录（R-OPS）。
- **操作用户字段（所有事件必带）**：服务日志每条事件记录**是谁发起的操作**，统一字段 `operator`：
  - 登录事件：operator = 尝试登录的用户名（失败时也记录尝试者）；
  - 文件/日志等业务事件：operator = 当前登录用户；
  - 管理事件：operator = 执行操作的管理员，并用 `target` 字段标注作用对象（如 `operator=admin target=user2 action=disable`）；
  - 系统/定时/启动退出：operator=`system`；R-OPS 命令行操作：operator=`cli`（并记录子命令）。
  - 日志格式：`时间 级别 [事件] operator=<用户名|system|cli> [target=<对象>] 详情`；审计日志（DB）的 `username` 列即操作者，与服务日志 operator 语义一致。
- **来源 IP 字段（Web 请求事件必带）**：所有由 HTTP 请求触发的事件（登录、查看、上传、下载、删除、管理操作等）统一携带 `ip=<来源IP>`（与审计日志一致：`X-Forwarded-For` 首项，否则 `RemoteAddr`）；系统/CLI 事件无来源 IP 时记 `ip=-`。
- 日志格式：`时间 级别 [事件] operator=... ip=... [target=...] 详情`（如 `2026-08-29T10:00:00+08:00 INFO [login] operator=admin ip=127.0.0.1 result=success`）；含敏感信息（密码/token/文件内容）一律不记录。

## 9.9 反向代理部署（R-PROXY）

- **可信代理配置**：`--trusted-proxies`（逗号分隔 IP/CIDR，默认空，env `FILEBOX_TRUSTED_PROXIES`）。**默认空=直连模式**：来源 IP 直接取 `RemoteAddr`，忽略 `X-Forwarded-For`（防伪造绕过）。
- **来源 IP 解析算法**（审计日志、IP 白名单、IP 锁定、服务日志统一使用）：
  1. 取请求 `RemoteAddr` 主机部分；
  2. 若该地址命中 `--trusted-proxies` 且请求带 `X-Forwarded-For`：从 XFF **右向左**取第一个**不在**可信代理列表中的 IP（标准反代链解析）；若 XFF 为空或全部在可信列表 → 取 XFF 最左项（或 RemoteAddr 兜底）；
  3. 解析结果即该请求的「客户端来源 IP」，用于全部 IP 相关功能。
- **Nginx 部署（示例见 `deploy/nginx.conf.example`）**：
  - HTTPS 终结（80→443 跳转；证书配置提示），`location /`、`/api/`、`/brand/` 反代 `127.0.0.1:8080`；
  - 关键指令：`client_max_body_size 0;`（大文件上传不受 Nginx 限制，由 FileBox `--max-file-size` 控制）、`proxy_request_buffering off;`（流式上传避免 Nginx 缓存磁盘）、`proxy_read_timeout 600s;`；
  - 请求头：`proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;`、`X-Real-IP $remote_addr`、`X-Forwarded-Proto $scheme;`；
  - SPA 路由：`try_files $uri /index.html;`（或由 FileBox 内置 fallback 处理）。
  - **FileBox 侧**：本机 Nginx 场景 `--trusted-proxies=127.0.0.1/32`（容器/多级反代按实际 CIDR 配置）；直连公网且未配置可信代理时，XFF 不会被采信（安全默认）。
- 其他反代（Caddy/Apache/Traefik）原理相同：正确透传 XFF 并在 FileBox 配置对应可信代理。

## 9.8 服务化部署（R-SERVICE）

- **Linux（systemd）**：提供 `deploy/filebox.service` 示例（`ExecStart=<路径>/filebox --data=/var/lib/filebox --log-enabled=true --log-dir=/var/log/filebox`、`User=filebox`、`WorkingDirectory`、`Restart=on-failure`、`Environment=FILEBOX_JWT_SECRET=...`），部署文档含安装/启停/开机自启/日志查看步骤。
- **Windows**：提供 `deploy/install-service.ps1` 示例（推荐 NSSM：`nssm install FileBox "path\to\filebox.exe" --data=C:\filebox\data --log-enabled=true`；同时给出 `sc create` 基础方案），含卸载与日志位置说明。
- `deploy/` 目录提供中英双语 README 与上述模板；服务化运行时建议：独立运行用户、专用数据/日志目录、`--jwt-secret` 强随机、`--log-enabled=true`。

## 9.6 后台命令行运维（R-OPS）

单文件二进制同时提供 Web 服务与运维子命令（Windows/Linux 用法一致，需服务停止时使用或与运行中实例共用同一 `--data` 目录时注意数据库锁）：

```bash
# 重置密码（指定新密码；重置后该用户 must_change_password=true，登录须改密）
./filebox admin reset-password --data=./data --username=admin --new-password='新密码'
./filebox admin reset-password --data=./data --username=admin --generate   # 自动生成强密码并打印一次

# 查看锁定信息（IP 锁定 + 用户锁定）
./filebox locks list --data=./data

# 删除锁定信息（按 IP / 按用户 / 全部）
./filebox locks clear --data=./data --ip=1.2.3.4
./filebox locks clear --data=./data --user=2
./filebox locks clear --data=./data --all
```

- 子命令直接操作 SQLite（bcrypt 更新密码、清 failed_attempts/locked_until/ip_failures），**不经过 HTTP/鉴权**（本机运维通道；文档提示保管好服务器 shell 权限）。
- 不指定 `--data` 时默认 `./data`；子命令返回非零退出码表示失败（便于脚本化）。
- 重置密码不校验 Web 密码策略（运维兜底场景），但强制改密保证用户下次按策略设置。

## 9.5 GitHub 集成与推送（R-GH）
- **仓库**：开发（阶段）完成后在 GitHub 创建新仓库 `filebox`（可见性按用户配置：公开/私有），代码推送至远程 main 分支。
- **流程**：`git init` → 编写规范 `.gitignore`（排除 `data/`、`.env`、构建产物等）→ 阶段完成即提交（含 docs/DEV_DOC.md 与 docs/requirements/ 状态文件）→ 创建远程仓库（`gh repo create` 或 GitHub REST API，需用户提供认证）→ `git push -u origin main`。
- **仓库内容**：完整源码（server/、web/、docs/、README、构建脚本）；密钥与数据文件一律不入库。
- **文档要求（双语）**：
  - `README.md`（中文）与 `README.en.md`（英文），两文件顶部互相提供语言切换链接；内容含：项目简介、功能清单、技术栈、快速开始（开发/构建/运行）、配置项表（flag 与环境变量）、默认管理员说明、目录结构、常见问题。
  - `RELEASE_NOTES.md`（中文）与 `RELEASE_NOTES.en.md`（英文）：版本号（v0.1.0 起）、本次交付功能清单、默认账号与安全提示（首次登录改密）、部署与运行说明摘要、已知限制（阶段二未实现项）、变更历史指向 docs/requirements/CHANGELOG.md。
  - 推送时以上 4 个文件必须齐全；RELEASE_NOTES 内容同时作为 GitHub Release 描述（如创建 Release）。
- **认证方式**（已确认）：用户提供 GitHub PAT（Fine-grained token，含 repo 权限）；使用 GitHub REST API 创建仓库 + git 推送，不安装 gh CLI。
- **仓库归属**（已确认）：用户名 `tony2025huang`，仓库 `filebox`，**公开**（public）。
- **git 身份**（已确认）：`user.name = dsh&codex`，`user.email = tony2025huang@users.noreply.github.com`（已全局配置）。

## 10. 阶段划分与验收标准

### 阶段一（MVP）
范围：登录/登出、文件列表、整体上传（≤100MB 直接传）、下载（含 Range）、删除、多用户（管理员创建用户、改角色/配额/禁用、删除用户）、SQLite 持久化、配额检查、首次启动自动创建 admin、README、**文件 md5 字段（complete 时计算保存并返回）**、**磁盘监控与系统保护（R-DISK）**、**落盘保留原文件名（R-NAME）**、**同名冲突选择（R-CONFLICT）**、**非法字符前置校验（R-VALID）**、**操作审计日志（R-LOG）**、**登录安全（R-LOCK）**、**品牌定制（R-BRAND）**、**多语言（R-LANG）**、**界面主题色（R-THEME）**。

验收：单用户完整走通 上传→列表→下载→删除；第二个用户无法看到/删除他人文件；admin 界面完成用户 CRUD；100MB 文件上传/下载正常；**上传完成后文件记录的 md5 与 sha256 均与本地计算一致（`Get-FileHash -Algorithm MD5 / SHA256`）**；**磁盘 data/files 下可见文件名为原始文件名（同目录重名时由用户选择覆盖/重命名，不同目录/不同用户同名文件不加序号），且下载/删除仍按 DB 记录精确工作**；**覆盖上传后：磁盘仅剩新文件、DB 旧记录消失、used_bytes 正确**；**非法文件名在 upload-init 即被 400 拒绝**；**admin 统计卡片显示磁盘占用大小与比例**；**可用空间低于阈值（测试时用 --min-free-space 调大验证）时上传被 503 拒绝、下载删除不受影响**；**连续 5 次密码错误后账号锁定、5 分钟后自动解禁；登录失败前后端提示统一为「用户名或密码错误」**；**登录/上传/下载操作均产生日志，admin 可查全部用户并可设置留存周期，普通用户仅见自己日志**；**admin 设置品牌后：网页标题/favicon/登录页与主页 logo/页脚备案立即生效，「恢复默认」后回退内置品牌；ICP 与公安备案号留空时页面页脚不出现备案区（无空白残留）**；**系统默认语言可配（zh-CN 默认），用户可设个人语言（默认跟随系统）；切换语言立即生效无需重新登录；简体/繁体/英文三套界面文案完整无遗漏（含错误提示本地化）**；**管理员设置界面主色（输入色号或色盘）后全站主色元素立即变更，重置回默认 #1b998b，无需刷新或重新登录**；**首次部署用 --admin-user/--admin-pass 创建的初始管理员登录后强制改密，改密前无法使用其他功能；管理员可在后台重置管理员密码**；**密码策略（8 位 + 四类中至少 3 类）在创建/重置/改密时生效**；**为用户开启 TOTP 后，用户首次登录显示二维码与密钥字符串，绑定后登录需输入动态码**；**启用 IP 白名单的用户，白名单外 IP 请求被 403 拒绝（未启用不限制）**；**10 分钟内 50 次登录失败（可配）锁定来源 IP，自动解禁可配；管理后台可查看并手动解除 IP/用户锁定**；**命令行运维：`filebox admin reset-password` 重置密码（重置后强制改密）、`filebox locks list/clear` 查看/删除锁定信息，Linux 与 Windows 后台均可用**；**服务日志：`--log-enabled`（默认否）开启后按天滚动 + gzip 归档到 `--log-dir`（默认程序目录 logs/），保留 `--log-retention-days`（默认 90）天，详细记录登录/查看/上传/下载/分享/管理配置/禁用解禁等事件（含 operator 操作用户与 ip 来源字段）；不启用仅控制台**；**服务化部署：Linux systemd 与 Windows NSSM/sc 示例与中英文部署文档可用**；**反代部署：`--trusted-proxies` 生效——配置后 XFF 解析出真实客户端 IP（审计/白名单/IP 锁定/服务日志一致），未配置时忽略 XFF 防伪造；`deploy/nginx.conf.example` 可直接套用**；重启后数据不丢。

### 阶段二（增强）（已实现，v0.2.0）
范围：分片断点续传（2–8MB 分片、并发 4、暂停/继续、断点续传）、秒传、文件夹/多文件上传（保留相对路径）、分享链接（有效期/次数限制/匿名页）、在线预览（图片/文本/视频）、上传限速、注册开关、系统统计。

验收：1GB 文件断点中断后续传成功；秒传命中不重复落盘；文件夹上传保留结构；分享链接过期后拒绝；预览白名单格式可看、非白名单强制下载；超配额上传被拒；限速生效；Linux 交叉编译二进制可在 Linux 上运行。

### GitHub 推送验收（两阶段均适用，R-GH）
- 阶段一完成：代码提交并推送至 GitHub 新仓库 `filebox`（含 README 与文档），仓库可正常 clone。
- 阶段二完成：增量提交全部推送，远程与本地一致。

## 11. 测试策略
- 开发期：codex 自测核心接口（curl 冒烟）
- 验收期：由 DSH 按 `app-testing` skill 执行二次测试（功能/边界/并发/安全/性能），输出缺陷清单与测试报告
