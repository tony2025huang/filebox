# Codex 变更补丁任务 — 阶段一增量 2（R-CONFLICT / R-VALID / R-LOG / R-LOCK）

你在 `C:\Users\huangcp\dsh-project\filebox` 完成了 FileBox 阶段一 MVP 及第一批补丁（R-DISK / R-NAME，如未完成先完成）。现在实现用户第二批确认的需求。**先完整阅读 `docs/DEV_DOC.md`**（已更新），再动手。使用 skills：`web-app-dev`、`file-transfer-design`、`app-testing`。

## 变更 1：R-VALID — 非法字符前置校验

`POST /api/files/upload-init` 在创建任务前先做文件名校验（复用/重构现有 sanitizeName 逻辑）：含路径分隔符、控制字符、Windows 非法字符 `<>:"|?*`、空名、`.`/`..`、超过 255 字节 → 返回 **400**，message 为「文件名包含非法字符，禁止上传」。

## 变更 2：R-CONFLICT — 同目录同名冲突：用户选择覆盖/重命名

替换"自动加序号"的默认行为（重命名仍自动加序号，但须用户确认）：

1. `upload-init` 请求体支持可选字段 `resolve`（`'overwrite' | 'rename'`）。
2. 服务端在 init 时检测**同目录（同 user_id、同日期目录）已存在 status='ready' 的同名文件**：
   - 无冲突 → 正常创建任务，返回 `taskId`。
   - 有冲突且未提供 resolve → 返回 `200 {code:409, message:"同名文件已存在", data:{conflict:true, existing:{id,name,size,createdAt,md5}}}`（HTTP 409）。
   - 有冲突且 resolve='rename' → 正常创建任务（complete 时分配 ` (1)` 序号名）。
   - 有冲突且 resolve='overwrite' → **事务内执行覆盖**：删除旧文件记录（软删除或物理删除均可，但磁盘文件必须删除、used_bytes 必须校正差额）、再创建新任务；complete 时以原名落盘。
3. `CompleteUpload` 的 stored_name 分配逻辑保留（rename 时加序号；overwrite 时原名；无冲突时原名）。
4. **前端 `FilesView.vue`**：uploadFile 时若 init 返回 409/conflict → 弹出确认框「该目录已存在同名文件 *名称*，请选择：」按钮「覆盖」（提示：将删除旧文件并替换为新文件）与「重命名」（自动加序号）；选择后以对应 resolve 重新调用 init 继续上传。

## 变更 3：R-LOG — 操作审计日志

1. 新增表 `audit_logs`：`id, user_id(可空), username(快照), action, target, ip, result('success'|'failure'), reason(可空), created_at`；索引 `(user_id, created_at DESC)`、`(created_at)`。
2. `internal/store` 增加 `AddAuditLog(...)` 与 `ListAuditLogs(userID *int64, action, result, keyword string, page, pageSize)`；**写日志时惰性清理**：按 settings 留存周期删除过期记录。
3. 埋点（阶段一）：
   - 登录成功/失败（失败 reason：`user_not_found`/`wrong_password`/`user_disabled`/`locked`）
   - 上传 complete 成功/失败（target=文件名）
   - 下载 成功/失败（target=文件名；失败如 404/403）
4. API：
   - `GET /api/logs?action=&result=&keyword=&page=&pageSize=`：普通用户仅自己的；admin 额外支持 `userId=`（空=全部）。
   - `GET /api/logs/actions`：返回操作枚举。
   - `GET /api/admin/settings`、`PUT /api/admin/settings`（仅 admin）：`{logRetentionDays:30, lockThreshold:5, autoUnlockEnabled:true, autoUnlockMinutes:5}`，存 settings 表。
5. 来源 IP：`X-Forwarded-For` 首项，否则 `RemoteAddr`。
6. **前端**：新增 `/logs` 页面（表格：时间/用户/操作/目标/来源IP/结果/失败原因；筛选控件：操作类型、结果、关键字；分页）。普通用户导航加「日志」入口；admin 在日志页顶部或 `/admin` 内加「日志留存周期」设置（天数输入+保存）。顶栏加日志入口链接（对所有登录用户）。

## 变更 4：R-LOCK — 登录安全

1. `users` 表加列：`failed_attempts INTEGER NOT NULL DEFAULT 0`、`locked_until TEXT`（RFC3339，NULL=未锁定）。
2. 登录流程（`POST /api/auth/login`）：
   - 用户不存在 → 记日志（`user_not_found`），返回统一 401「用户名或密码错误」。
   - 用户 disabled → 记日志（`user_disabled`），统一 401。
   - `locked_until` 未过期 → 记日志（`locked`），统一 401。
   - 密码错误 → `failed_attempts + 1`；达到 `lockThreshold`（默认 5；0=关闭锁定）→ 设置 `locked_until`：`autoUnlockEnabled` 为 true 时 = now + `autoUnlockMinutes`（默认 5 分钟），false 时 = 远期时间（如 9999-12-31），并清空 failed_attempts；记日志（`wrong_password`）。
   - 密码正确 → 重置 `failed_attempts=0`、`locked_until=NULL`；记日志 success。
   - **所有失败响应统一**：401 + 「用户名或密码错误」（绝不在登录响应中区分原因）。
3. 管理员在 `/admin` 编辑用户保存时，重置该用户 `failed_attempts=0`、`locked_until=NULL`（解除锁定）。
4. 登录页前端：仅展示服务端 message（统一文案），无额外提示。

## 执行要求

1. 先读 `docs/DEV_DOC.md` 全文；如第一批补丁（1B）尚未完成，先完成它再开始本任务。
2. 保持现有风格（标准库、中文消息、`{code,message,data}`）。
3. 验证全过：`go build ./...`、`go vet ./...`、`go test ./...`；前端改动后 `npm --prefix web run build` + `go run ./scripts/sync-web.go`。
4. 冒烟（`go run ./cmd/filebox --data=./data-patch2`）：
   - 非法文件名（如 `a<b>.txt`、`a..txt`、含控制字符）→ init 返回 400。
   - 上传 `a.txt` 两次：第二次 init 返回 409 conflict；resolve=rename 后落盘 `a (1).txt`；再传第三次 resolve=overwrite：磁盘仅一个 `a.txt`，DB 记录数正确，used_bytes 正确。
   - 登录：错误密码 5 次 → 第 6 次即使正确密码也 401（锁定）；5 分钟后（或调小 autoUnlockMinutes 验证）恢复；不存在用户登录 → 401 且提示与密码错误一致；`/api/logs` 登录日志含具体 reason（登录成功后查询）。
   - `/api/logs` 普通用户只见自己；admin 见全部并可设置留存周期；写入日志后检查惰性清理（可临时把 retention 设为 0 验证删除）。
   - 冒烟后删除 `data-patch2/`。
5. 更新 `docs/requirements/CHANGELOG.md`（追加 2026-08-29 第二批变更），`docs/requirements/STATE.md` 同步（R-CONFLICT/R-VALID/R-LOG/R-LOCK 标 done）。
6. 最终报告：改动文件清单、验证结果、冒烟摘要。
