# Codex 变更补丁任务 — 阶段一增量 6（R-INIT / R-PWD / R-TOTP / R-IPACL / R-IPBAN / R-LOCKADMIN）

你在 `C:\Users\huangcp\dsh-project\filebox` 完成了 FileBox 阶段一 MVP 及补丁 1B–1G（如 1F 多语言、1G 主题色未完成先完成，本任务新增的全部前端文案必须进 1F 的三语言 i18n 字典；新增代码注释遵循 1E 的中英双语规范）。现在实现用户第六批高级安全需求。**先完整阅读 `docs/DEV_DOC.md`（第 1 节 R-INIT/R-PWD/R-TOTP/R-IPACL/R-IPBAN/R-LOCKADMIN、5.1/5.4/5.10 节、6 节）**，再动手。

## 变更 1：R-INIT — 初始账号与强制改密
1. `cmd/filebox/main.go` 新增 flag：`--admin-user`（默认 `admin`）、`--admin-pass`（默认 `admin123`，对应 `FILEBOX_ADMIN_USER/FILEBOX_ADMIN_PASS`）；`EnsureAdmin` 创建时置 `must_change_password=1`（已存在的 admin 不动）。
2. `users` 表加 `must_change_password INTEGER NOT NULL DEFAULT 0`（迁移）。
3. **强制改密中间件**：`must_change_password=true` 的用户，除 `POST /api/auth/change-password`、`POST /api/auth/logout`、`GET /api/auth/me` 外全部接口返回 403，错误体 `{code:403, message:"请先修改初始密码", data:{code:"PASSWORD_CHANGE_REQUIRED"}}`；前端统一拦截（api.js 收到该错误码 → 跳 `/change-password`）。
4. `POST /api/auth/change-password`（登录后）：`{oldPassword, newPassword}`；校验旧密码、按 R-PWD 校验新密码、更新密码并清 `must_change_password`、重签 JWT 返回新 token。
5. 前端新增 `/change-password` 页（旧密码/新密码/确认，显示策略要求）。
6. 管理后台「编辑用户」重置密码时同时置 `must_change_password=true`（下次登录强制改密）。

## 变更 2：R-PWD — 密码强度策略
1. settings 增加 `passwordMinLength`（默认 8）、`passwordComplexity`（默认 3，0=不要求类别）。
2. 校验函数：长度 ≥ minLength；四类字符（大写/小写/数字/特殊）中出现的类别数 ≥ complexity。
3. 应用于：`POST /api/admin/users`（创建）、`PUT /api/admin/users/:id`（重置密码）、`POST /api/auth/change-password`（改密）；不通过返回 400「密码不符合强度要求」。
4. 前端：创建用户/重置密码/改密表单显示当前策略提示（长度 + 类别要求）；`/api/admin/settings` 增加这两个字段。

## 变更 3：R-TOTP — 双因素认证
1. `users` 表加 `totp_secret TEXT`（**加密存储**：AES-GCM，密钥由 --jwt-secret SHA-256 派生）、`totp_enabled INTEGER NOT NULL DEFAULT 0`。
2. `PUT /api/admin/users/:id/totp`（admin）：`{enabled:true}` 生成新 secret（20 随机字节 → base32，加密存库，totp_enabled=0 待绑定）；`{enabled:false}` 清除 secret 与标记。
3. 登录流程改造（`POST /api/auth/login`）：
   - 密码正确后：若 `totp_enabled=true` → 返回 `{totpRequired:true, totpChallenge:"<临时token 5分钟>"}`（不发正式 JWT）；若 `totp_secret` 存在但未启用 → 返回 `{totpSetup:true, totpChallenge, otpauthUrl, secret:"<base32 明文仅此一次>"}`（前端显示**二维码与密钥字符串**）。
   - `POST /api/auth/totp`：`{totpChallenge, code}` → 校验（RFC 6238：HMAC-SHA1、30 秒、±1 窗口、60 秒防重放：记录 last_used_totp 时间戳）；未启用时校验成功 → `totp_enabled=1`（完成绑定）；已启用 → 签发正式 JWT；失败 → 401 统一文案 + 日志 reason `totp_failed`。
   - 二维码：`GET /api/auth/totp-qrcode?challenge=...` 返回 PNG（`github.com/skip2/go-qrcode` 依赖），前端 `<img>` 展示。
4. `GET /api/auth/me` 返回 `totpEnabled`。

## 变更 4：R-IPACL — 来源 IP 白名单
1. `users` 表加 `ip_acl_enabled INTEGER NOT NULL DEFAULT 0`、`ip_whitelist TEXT NOT NULL DEFAULT ''`（逗号分隔单 IP/CIDR，IPv4/IPv6）。
2. `PUT /api/admin/users/:id/ip-acl`（admin）：`{enabled, whitelist}`；whitelist 逐项 `net.ParseCIDR`/`net.ParseIP` 校验，非法 400。
3. requireAuth 中间件：用户 `ip_acl_enabled=true` 时解析来源 IP（与日志一致：X-Forwarded-For 首项或 RemoteAddr），不在白名单 → 403「当前 IP 不在白名单」；**未开启不校验**。
4. 管理后台「编辑用户」增加 TOTP 开关与 IP 白名单表单。

## 变更 5：R-IPBAN — IP 级登录锁定
1. 新表 `ip_failures(ip TEXT PRIMARY KEY, failed_count INTEGER, window_started_at TEXT, locked_until TEXT)`。
2. settings 增加 `ipLockWindowMinutes`（10）、`ipLockThreshold`（50，0=关闭）、`ipAutoUnlockEnabled`（true）、`ipUnlockMinutes`（30）。
3. 登录失败（**所有失败分支，含 user_not_found**）累计来源 IP：窗口滑动（距 window_started_at > windowMinutes → 重置计数与窗口）；计数 ≥ threshold → locked_until（自动解禁开=now+ipUnlockMinutes，关=9999-12-31）。
4. 登录前检查来源 IP 锁定：锁定中 → 401 统一文案 + 审计日志 reason `ip_locked`（不累计计数）。
5. 登录成功（含 TOTP 完成后）→ 清除该 IP 的失败记录。

## 变更 6：R-LOCKADMIN — 锁定管理
1. `GET /api/admin/locks`（admin）→ `{ipLocks:[{ip, failedCount, windowStartedAt, lockedUntil, lockedNow}], userLocks:[{id, username, failedAttempts, lockedUntil, lockedNow}]}`（只列出当前有效/锁定中记录，可含最近活跃）。
2. `DELETE /api/admin/locks/ip/{ip}`、`DELETE /api/admin/locks/user/{id}`（admin）→ 解除对应锁定（清记录）。
3. 管理后台新增「锁定管理」面板：两个表格（IP 锁定/用户锁定）各带「解除」按钮 + 空态。
4. 管理后台「安全设置」区扩展：密码强度（最小长度/复杂度）、IP 锁定（窗口分钟/阈值/自动解禁开关/解禁分钟）。

## 变更 7：R-PROXY — 反向代理与可信来源 IP（与 1J 的 deploy/nginx 示例配套）

1. `cmd/filebox/main.go` 新增 flag `--trusted-proxies`（默认空，env `FILEBOX_TRUSTED_PROXIES`），解析为 CIDR/IP 列表注入 Config。
2. **统一来源 IP 解析**（重构现有 `requestIP`，审计日志、R-IPACL、R-IPBAN、服务日志共用）：
   - 直连模式（trusted-proxies 为空）：直接取 `RemoteAddr` 主机部分，**忽略 X-Forwarded-For**（防伪造绕过）；
   - 代理模式：`RemoteAddr` 命中可信列表且带 XFF → 从 XFF **右向左**取第一个不在可信列表中的 IP；XFF 空/全可信 → 取最左项（或 RemoteAddr 兜底）；
   - 输出即「客户端来源 IP」，供全部 IP 功能使用。
3. 校验非法可信代理条目 → 启动时报错退出。
4. 冒烟补充：配置 `--trusted-proxies=127.0.0.1/32` 后带 `X-Forwarded-For: 1.2.3.4` 的请求 → 审计日志/IP 锁定记录 1.2.3.4；不配置时同请求记录 127.0.0.1。

## 通用要求
- 新增前端文案全部进三语言 i18n 字典（zh-CN/zh-TW/en）；新增代码注释中英双语；错误响应沿用 `{code,message,data}`；统一登录失败文案「用户名或密码错误」。
- 验证全过：`go build ./...`、`go vet ./...`、`go test ./...`；`npm --prefix web run build` + `go run ./scripts/sync-web.go`；Linux 交叉编译。
- 冒烟（`go run ./cmd/filebox --data=./data-patch6 --admin-user=admin --admin-pass=Admin123!`）：
  1. 初始 admin 登录 → me.mustChangePassword=true → 访问文件列表 403 → change-password 后恢复访问。
  2. 创建弱密码用户（如 `abc`）→ 400；符合策略（如 `Abc12345!`）→ 成功。
  3. admin 为用户开启 TOTP → 该用户登录返回 totpSetup + secret → POST /api/auth/totp 错误码 400 → 正确 6 位码 → totpEnabled=true；再次登录需两步（totpRequired）→ 错误码 401 → 正确码登录成功。
  4. 为用户设置 IP 白名单（127.0.0.1）→ 本机访问正常；设置 10.0.0.0/8 后本机访问 → 403；关闭后恢复。
  5. `--ip-lock-threshold`（无此 flag 则用 settings 调小阈值如 3）连续失败 → GET /api/admin/locks 出现该 IP 锁定 → DELETE 解除 → 登录恢复。
  6. 冒烟后删除 `data-patch6/`。
- 更新 `docs/requirements/CHANGELOG.md`（追加第六批变更）、`docs/requirements/STATE.md`（6 个新条目 done）；同步 `README.md`/`README.en.md` 功能清单（增加：强制改密、密码强度策略、TOTP 双因素、IP 白名单、IP 登录锁定与锁定管理）。
- 最终报告：改动文件清单、验证结果、冒烟摘要。
