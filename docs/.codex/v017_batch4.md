你是 FileBox 项目（Go 后端 + Vue3 前端，i18n 三语 zh-CN/zh-TW/en）的编码工程师。本任务改动：`internal/httpapi/server.go`（路由注册）、`internal/httpapi/sync.go`（新端点）、`internal/httpapi/sync_test.go`（测试）、`web/src/views/SyncView.vue`、`web/src/i18n.js`。**不得改动其它文件**（尤其不要动 internal/store、不要动 sync_logs 表结构——执行历史展示按现有字段先做兼容实现）。完成后运行 `go build ./... && go vet ./... && go test ./internal/httpapi/` 与 `cd web && npm run build` 确认通过（web/dist 不要提交）。

## 需求 A：同步任务密码可查看（点击眼睛显示已保存密码）

背景（现状已确认，勿重复造轮子）：
- 密码已 AES-GCM 加密入库：`createSyncSystem`/`updateSyncSystem` 用 `s.encryptSyncSecret(...)`；解密函数 `s.decryptSyncSecret(value)` 已存在（`internal/httpapi/sync.go` line 1040）。
- 编辑留空=保留已有 `__keep__` 逻辑（updateSyncSystem 中 `input.AuthSecret == "" → "__keep__"`），"编辑不丢失密码"已解决。
- `publicSyncSystem`（sync.go line 76）已返回 `hasCredentials`（布尔，凭据是否存在）。
- 前端 `web/src/views/SyncView.vue` 的系统表单（systemModal）中密码字段是 `systemForm.authSecret`，编辑时 `openSystemEdit` 将其清空、placeholder 用 `t('sync.credentialsHint')`；**缺少"已保存密码"提示与查看入口**。

要求：
1. 后端新增端点 `GET /api/sync/systems/{id}/secret`（路由注册在 `internal/httpapi/server.go` line 401-407 附近，`mux.HandleFunc("GET /api/sync/systems/{id}/secret", s.requireAuth(s.getSyncSystemSecret))`）：
   - handler 实现放 sync.go：走 `s.loadSyncSystem(r)` 权限校验（越权返回 404）；读取 store.RemoteSystem 后调用 `s.decryptSyncSecret(item.AuthSecret)` 解密 authSecret（authPassphrase 同样解密，空字符串直接返回空）；返回 `{"secret": "...", "authPassphrase": "..."}`（响应结构用 writeData）。
   - 解密失败返回 500（"读取凭据失败"），并 log.Printf 记录错误（不含明文）。
   - 记录一条 serviceEvent 审计（如 `s.serviceEvent(r, "sync_system_secret_view", user.Username, "target=%d result=success", id)`）。
2. 前端 SyncView.vue：
   - 系统表单密码输入行改为"输入框 + 眼睛图标按钮"组合：当 `systemEditing && item.hasCredentials`（或系统对象 hasCredentials 为真）时，密码框显示掩码占位（值保持为空字符串表示"不改"），输入框旁显示眼睛图标（lucide `Eye`/`EyeOff`）。
   - 点击眼睛：若尚未加载明文，调用 `api('/api/sync/systems/${id}/secret')` 获取 `{secret, authPassphrase}` 存入本地 ref（如 revealedSecret），然后把输入框显示为明文（仅用于本次编辑会话展示；**不要自动回填到 authSecret 提交值**——提交逻辑保持"留空=保留"，若用户手动修改了输入框内容则提交新值）。再次点击眼睛切回掩码并清空输入框显示。
   - 若解密失败显示 formError（t('sync.secretError')）。
   - 只有 authType=password 时显示眼睛；authType=key 时按现状（密钥内容同样可用此端点查看，统一处理即可，但 UI 标签保持"密码/密钥内容"）。
3. i18n 三语新增：`sync.viewSecret`（查看已保存的密码）、`sync.hideSecret`（隐藏）、`sync.secretError`（读取已保存凭据失败）、`sync.savedSecretPlaceholder`（已保存，点击眼睛查看）。（en/zh-TW 按语义翻译。）
4. 单测（sync_test.go）：新增用例——创建系统（带 authSecret）→ GET /api/sync/systems/{id}/secret 返回解密后的原始 secret；无权限用户访问返回 404。参考文件内现有测试的 helper（testJSONRequest、adminToken/otherToken、formatID 等）。

## 需求 B：同步任务执行历史（兼容现有字段的增强展示）

背景：
- `web/src/views/SyncView.vue` 详情弹窗（details，line 20 模板 + `openDetails`/`loadDetails` line 118-119）已展示 `detailLogs`（GET /api/sync/tasks/{id} 返回 logs 数组，每条含 id/taskId/runAt/direction/result/files/bytes/message/detail）。
- 目前每条只显示 `formatDate(entry.runAt)` + result + files + bytes + message。**注意：后端本轮不改**，sync_logs 尚无 finished_at 字段；前端按兼容方式实现——若条目含 `finishedAt` 字段则显示"结束时间"，否则只显示开始时间；若 `result === 'running'` 显示"进行中"（i18n `sync.running` 已存在），否则 success/failure。

要求（仅前端）：
1. 详情弹窗执行日志列表每条改为展示：**开始时间**（runAt）、**结束时间**（若存在 finishedAt 则显示，否则不显示）、**结果徽标**（running=进行中 / success=成功 / failure=失败，样式沿用现有 result-label class，running 可加 running 样式）、文件数、字节数、消息。
2. "进行中"条目显示醒目标识（如 spin 图标或不同底色，用现有 class 体系，不要引入复杂新样式）。
3. 列表为空仍显示 `sync.noLogs` 空态；刷新按钮行为不变（loadDetails）。
4. i18n 三语新增：`sync.startTime`（开始时间）、`sync.endTime`（结束时间）。（en: 'Start time'/'End time'，zh-TW: '開始時間'/'結束時間'。）

## 验收

1. `go build ./... && go vet ./...`、`go test ./internal/httpapi/`、`cd web && npm run build` 全部通过。
2. 编辑目标系统时能看到"已保存密码"提示，点眼睛可查看明文（后端单次返回解密值），再点隐藏；不修改输入框直接保存不会改变原密码（保持 __keep__ 语义）。
3. 执行历史显示开始时间与（若有的）结束时间；running 显示"进行中"。
4. 不触碰 sync_logs 表结构/迁移/store 层。
5. 三语键完整。

请实施并简述每处改动。
