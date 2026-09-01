你是 FileBox 项目（Go 后端 + Vue3 前端，i18n 三语 zh-CN/zh-TW/en）的编码工程师。本任务改动：`internal/store/sync.go`、`internal/store/sync_test.go`、`internal/httpapi/sync.go`、`internal/httpapi/sync_test.go`、`web/src/components/AuthenticatedTopbar.vue`、`web/src/i18n.js`（可选 `web/src/views/ChangePasswordView.vue` 只读参考）。**不得改动其它文件**。完成后运行 `go build ./... && go vet ./... && go test ./internal/store/ ./internal/httpapi/` 与 `cd web && npm run build` 确认通过（web/dist 不要提交）。

## 需求 A：同步任务日志实时状态（running 行 + 完成后 UPDATE）

背景：
- `internal/httpapi/sync.go` `executeSyncTask`（约 line 1170）：目前**执行完成后才** `CreateSyncLog` 写一行（runAt=开始时间，无结束时间列），所有分支（owner 加载失败/只读跳过/正常执行）都只写一行最终结果。
- `internal/store/sync.go`：sync_logs 表（migrateSyncSchema，约 line 113-126）列为 `id/task_id/user_id/run_at/direction/result/files/bytes/message/detail`，且 `result TEXT NOT NULL CHECK(result IN ('success','failure'))`；`SyncLog` struct（line 62-73）；`CreateSyncLog`（line 491）；`ListSyncLogs`（line 521）；`scanSyncLog`（line 510）。SQLite 无法 ALTER 修改 CHECK 约束，**必须重建表**。
- 并发安全：`runSyncTaskNow` 与调度路径都用 `syncLock`（TryLock）保证同一任务不并发执行，executeSyncTask 内无并发写同一 log 的风险。

要求：

### store 层（internal/store/sync.go）
1. `SyncLog` struct 增加字段 `FinishedAt string \`json:"finishedAt"\``（放在 RunAt 之后）。
2. 迁移（migrateSyncSchema 内、现有 sync_logs 建表语句之后）：检查 sync_logs 是否已有 `finished_at` 列（`tableColumns(s.DB, "sync_logs")`，模式参照现有 remote_systems 的列检查）。若没有：**重建表去掉 CHECK 并加列**——在一个事务里执行：
   ```sql
   CREATE TABLE sync_logs_new (
     id INTEGER PRIMARY KEY AUTOINCREMENT,
     task_id INTEGER NOT NULL REFERENCES sync_tasks(id) ON DELETE CASCADE,
     user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
     run_at TEXT NOT NULL,
     direction TEXT NOT NULL,
     result TEXT NOT NULL,
     files INTEGER NOT NULL DEFAULT 0,
     bytes INTEGER NOT NULL DEFAULT 0,
     message TEXT NOT NULL DEFAULT '',
     detail TEXT NOT NULL DEFAULT '',
     finished_at TEXT NOT NULL DEFAULT ''
   );
   INSERT INTO sync_logs_new(id, task_id, user_id, run_at, direction, result, files, bytes, message, detail, finished_at)
     SELECT id, task_id, user_id, run_at, direction, result, files, bytes, message, detail, '' FROM sync_logs;
   DROP TABLE sync_logs;
   ALTER TABLE sync_logs_new RENAME TO sync_logs;
   ```
   随后重建两个索引：`idx_sync_logs_task_run ON sync_logs(task_id, run_at DESC, id DESC)` 与 `idx_sync_logs_run ON sync_logs(run_at)`。（注意：迁移在项目其它地方也用事务/直接 Exec 模式，参照现有写法；事务可用 s.DB.Exec 逐条执行，或使用已有的事务辅助函数——以仓库现有惯例为准。）
3. `scanSyncLog` 与 `ListSyncLogs` 的 SELECT 增加 `COALESCE(finished_at, '')` 字段（保持列顺序一致）。
4. 新增 store 函数：`UpdateSyncLogResult(ctx context.Context, logID int64, result, finishedAt string, files, bytes int64, message, detail string) error`：`UPDATE sync_logs SET result=?, finished_at=?, files=?, bytes=?, message=?, detail=? WHERE id=?`；RowsAffected 为 0 时返回 ErrNotFound。
5. `ListSyncLogs` 保持按 `run_at DESC, id DESC` 排序。

### httpapi 层（internal/httpapi/sync.go）
6. `publicSyncLog`（line 92）增加 `"finishedAt": item.FinishedAt`。
7. 改造 `executeSyncTask`：**在执行开始时（正常执行路径，即 owner 校验通过、非只读之后、调用 push/pull 之前）**先 `CreateSyncLog` 写一条 running 行（`Result:"running"`，`Message:"执行中"`，Files/Bytes 为 0，RunAt=当前 UTC RFC3339），拿到返回的 `logID`；执行完成后用 `UpdateSyncLogResult(logID, resultValue, finishedAt, files, bytes, message, detail)` 更新同一条记录（finishedAt=完成时刻 UTC RFC3339）。`UpdateSyncTaskResult` 保持。
8. 失败分支（owner 加载失败、只读跳过）：保持现有直接写 failure 行的行为即可（不要求写 running 行），但**如果**这些分支写在 running 行之后会遗留 running 死行——确保任意路径最终都不遗留 result='running' 的行（owner/只读分支先于 running 行创建，天然无遗留）。
9. `getSyncTask`（line 744）与 `listSyncTaskLogs`（line 834）返回的日志条目自动携带 finishedAt（publicSyncLog 已加）。

### 测试
10. `internal/store/sync_test.go`：新增用例——CreateSyncLog(running) → UpdateSyncLogResult(成功) → ListSyncLogs 断言 result/finishedAt/files/bytes/message 更新正确；UpdateSyncLogResult 对不存在 id 返回 ErrNotFound。
11. `internal/httpapi/sync_test.go`：新增用例——创建一个指向无效 SFTP（如 127.0.0.1:1）的任务，调用 executeSyncTask（或通过 POST /api/sync/tasks/{id}/run），断言：执行后任务日志中**存在一条非 running 的最终行**且 finishedAt 非空；执行期间（executeSyncTask 内难以中途断言，可改为直接验证最终结果行 + store 层已有 running→final 单测覆盖时序）。参照文件内现有测试 helper（testJSONRequest、db、createTestUser 等）。

## 需求 B：普通修改密码改为居中弹窗（强制改密仍走独立页）

背景：
- 顶栏 `web/src/components/AuthenticatedTopbar.vue` line 13：`<RouterLink to="/change-password?mode=self" ...>` 是跳页入口。
- `web/src/views/ChangePasswordView.vue`：独立页表单（旧密码/新密码/确认，POST /api/auth/change-password，成功 `saveSession(body)` 后 `router.push('/')`）。
- `web/src/router.js` 守卫：`mustChangePassword` 时强制跳 /change-password（**必须保留**）。
- 后端 `POST /api/auth/change-password` 已存在，勿动。

要求（仅前端）：
1. AuthenticatedTopbar.vue：把"修改密码"RouterLink 改为按钮（保留 KeyRound 图标与 title），点击设置 `changePasswordOpen = true`。
2. 在 AuthenticatedTopbar 模板内（header 内部末尾）增加居中弹窗（复用项目通用 modal-backdrop + modal-panel class）：
   - 标题：`t('password.title')`；关闭按钮（X 图标）。
   - 表单字段：旧密码 `password.old`、新密码 `password.new`、确认新密码 `password.confirm`（均 type=password，autocomplete 对应值），提示 `t('password.policy', policy)`（policy 从 `/api/auth/password-policy` 加载，参照 ChangePasswordView 的 onMounted 实现；弹窗打开时加载一次即可）。
   - 提交：本地校验两次新密码一致（不一致显示 `password.mismatch`），POST `/api/auth/change-password`，成功 `saveSession(body)`、关闭弹窗并清空表单（可显示成功提示 notice 或复用现有 alert success 样式，三语用 `password.submit`/新增 `notice.passwordChanged` 键）；失败显示错误消息（`error.` 键或服务端消息）。
   - 提交中禁用按钮并显示 loading（`password.submitting`）。
3. 弹窗相关 ref（changePasswordOpen、changePasswordForm、changePasswordConfirm、changePasswordError、changePasswordLoading、changePasswordPolicy）都在组件内管理。
4. `web/src/i18n.js` 三语新增（若需要）：`notice.passwordChanged`（密码已修改/Password changed/密碼已修改）。password.* 键已存在，直接复用。
5. 强制改密逻辑与 ChangePasswordView **完全不动**：must_change_password 时仍跳独立页。

## 验收

1. `go build ./... && go vet ./...`、`go test ./internal/store/ ./internal/httpapi/`、`cd web && npm run build` 全部通过。
2. 执行开始时 sync_logs 出现 result='running' 的行；完成后同一条被更新为 success/failure + finished_at 非空；owner/只读失败分支不遗留 running 行。
3. 任务详情/日志接口返回 finishedAt；前端详情弹窗（已由之前批次支持）可展示结束时间与"进行中"。
4. 顶栏点"修改密码"弹出居中弹窗、不跳页；提交成功更新会话并关闭；强制改密仍跳 /change-password 独立页。
5. 三语键完整。
6. 不触碰：其它视图、router.js（守卫逻辑）、后端认证接口。

请实施并简述每处改动。特别注意迁移重建表时保留外键引用与索引，且对已有数据的库升级后 `go test` 仍通过。
