# CODEX_TASK_v027.md — v027 修复任务（clear-all 二次认证加固 + 并发/文档/卫生）

- 起点：git HEAD=`a2dce8e`（v026「文件排序、清空认证与传输优化」），工作区干净（仅 2 个未跟踪调试文件，见 #6）。
- 来源：2026-09-12 从零复审 + 在线渗透（临时拉起 v026 实例验证后已停服），发现 2 项安全缺口（中/低各一）、1 项并发瑕疵、2 项文档漂移、1 项卫生问题、1 项仓库外密钥泄露。
- 执行方式：codex CLI（UTF-8 输出到 stdin），逐项小步提交；**不得引入新依赖**。
- 验证要求：`go test ./...`（必过）+ `go test -race ./internal/httpapi/`（#3 必过）+ web 侧如有改动跑 `npm run build`。
- 交付物：代码 + 回归测试 + 文档修正 + `docs/requirements/CHANGELOG.md` / `STATE.md` 登记（v027 段落）+ RELEASE_NOTES 追加。

## 执行结果（2026-09-12 已完成）

- #1/#2 已实施：`server.go` 两级限速（尝试桶 10/分钟含正确尝试、失败桶 5/分钟）+ TOTP 走 `ConsumeTOTP`；失败计入 `ip_failures`、不锁账号；`clearFilesReauth` 改为返回判定枚举。新增 `internal/httpapi/clear_all_reauth_test.go`（4 用例）。
- #3 已实施：`sync_trigger.go` 新增持锁 `triggerContextErr()`；`triggerCtx` 全部访问锁内/经 helper。
- #4/#5 已实施：DEV_DOC 三处 XFF 语义修正 + 草案声明；README 收集默认密码语义；新增规格 `docs/requirements/specs/v027-clear-all-reauth.md`。
- #6 已实施：删除 `tmp-read-users.go`、`.tmp-read-users.go`；`data-smoke-20260829/` 已按授权删除。
- #7 脚本已硬化（环境变量优先 + `.secrets/` 兜底并加入工作区 .gitignore）；**密钥吊销/轮换仍为人工待办**。
- 验证：`go test ./...` 全绿；**`go test -race` 全绿、无 DATA RACE**（便携 w64devkit GCC，httpapi 411.7s / store 58.2s / srvlog / cmd）；在线 13 项复现清单通过（6 次错误密码 = 5×401+429、`ipLocks` 记录、账号不锁、正确密码 200 并完成清空、TOTP 已消费码 401 / 新窗口码 200 / 重放 401、排序与收集隐私回归、演示数据未变）。
- 说明：`web/src/transferFlow.js` 的既有未提交改动（v026 传输可靠性）按指示**不合并**；`dist/` 发布产物仍为 v026（未执行 `make release`，因它会带上该前端改动）。

## 一、修复项清单（8 项）

| # | 类型 | 问题 | 现状（已核实，含行号/实测） | 解决方向 | 验证标准 |
|---|------|------|------------------------------|----------|----------|
| 1 | 安全（中） | `POST /api/files/clear-all` 二次认证可无限试错，构成密码/TOTP 爆破预言机 | `internal/httpapi/server.go:1427` `clearFilesReauth` 与 `:1452` `clearAllFiles`：失败分支仅 `return errors.New(genericError)`，既不调用任何限速桶，也不写 `ip_failures`。**在线实测**：连续 12 次错误密码 → 全部 401、`/api/admin/locks` 的 ipLocks/userLocks 均为空、账号未被锁、随后正确密码仍可登录；而登录路径同等错误会累计并锁定 | 在密码/TOTP 校验**之前**加独立限速桶：key = `strconv.FormatInt(user.ID,10) + "\x00" + s.requestIP(r)`，建议 10 次/分钟、burst 5（对齐 `collection.go:574-586` 的"失败才消耗、正确不消耗"模式），超限返回 `429` + `{"code":"REAUTH_RATE_LIMITED"}`；失败同时调用 `s.recordIPFailure(r, settings)`（`server.go:1169`）纳入 IP 维度（R-IPBAN）。**明确不接入账号级 `RecordLoginFailure`**：持有被盗 JWT 者不应能把受害者账号锁死（避免 DoS 升级）。校验先行、限速兜底，避免无节制的 bcrypt CPU 消耗 | 新增 Go 测试：①第 6 次连续失败返回 429；②失败后 `ip_failures` 有该 IP 记录；③桶满时提交**正确**凭据仍返回 200（正确路径不消耗失败桶）；④该用户 `failed_attempts`/`locked_until` 不被 clear-all 失败改写；⑤`clear-all` 无凭据/空 JSON 仍 401 且不消耗桶 |
| 2 | 安全（低） | `clear-all` 的 TOTP 校验无防重放（同一动态码可重复授权破坏性操作） | `server.go:1427-1450`：TOTP 分支用 `hmac.Equal` 命中即 `return nil`，**未调用 `store.ConsumeTOTP`**（登录路径 `totp()` 已调用）。**在线实测**：对一次性用户绑定 TOTP 后，同一 6 位码连续两次 `clear-all` 均返回 200；错误码 401 | TOTP 分支在命中时记录 `matchedCounter`，成功前调用 `store.ConsumeTOTP(ctx, user.ID, matchedCounter, now)`（`store.go:1161`）；未消费（重放/旧码）返回 401 并计入 #1 的失败桶。保持 ±1 窗口与 `hmac.Equal` 常量时间比较不变 | 新增 Go 测试：①同码第二次调用返回 401；②`counter+1` 的前瞻码可成功（窗口语义不回退）；③错误码 401 且不消费计数器；④密码路径（TOTP 未启用）行为不变 |
| 3 | 并发（低） | 未同步读取 `s.triggerCtx`（数据竞争） | `internal/httpapi/sync_trigger.go:226` 在 `triggerMu` **之外**读取 `s.triggerCtx`；同文件 `:139`（写侧 `:39`）、`:206`、`:262/:265` 均在锁内 | 持锁读取：把 `if s.triggerCtx != nil && s.triggerCtx.Err() != nil` 改为调用新增的 `func (s *Server) triggerContextErr() bool`（内部 `triggerMu.Lock()` 后读取并返回 `ctx == nil \|\| ctx.Err() != nil`），供 `runTriggeredTask` 复用（`triggerDoneChannel()` 已持锁，可参照） | `go test -race ./internal/httpapi/` 无 race 报告；触发式同步既有测试全绿 |
| 4 | 文档（低） | DEV_DOC 的来源 IP 描述与实现相反（若按文档"修正"代码会引入 XFF 伪造漏洞） | `docs/DEV_DOC.md:142`、`:166`、`:253` 三处称"优先取 `X-Forwarded-For` 首项，否则 RemoteAddr"；实际实现（`server.go` `requestIP`，及 R-PROXY 需求）为：仅当管理员开启 `trustProxy` **且**直连对端在 `--trusted-proxies` 内时解析 XFF，并取**从右向左首个不可信 IP** | 修正三处描述为准确语义（trustProxy 开关 + trustedProxies 白名单 + 右起首个不可信 IP），并交叉引用 R-PROXY；同时在 `DEV_DOC.md` 顶部加醒目说明：**"本文件为 v0.1 架构草案，功能与验收以 `docs/requirements/STATE.md` + `CHANGELOG.md` 为准"** | 三处描述与 `requestIP` 实现一致；全文无"X-Forwarded-For 首项"残留；顶部草案声明存在 |
| 5 | 文档（低） | 收集创建的默认密码语义未写明（文档称"默认随机"，裸 API 实测为无密码） | `CHANGELOG.md`（v024 段）称创建默认随机密码；实测 `POST /api/collections` 不带 `passwordMode`/`password` → `passwordProtected=false`（旧语义），随机密码需显式 `passwordMode=random`（前端默认显式传 random） | 在 DEV_DOC 收集章节与 README API 说明中明确：**缺省（无 passwordMode）= 无密码（历史语义）；设置随机密码需显式 `passwordMode=random`**；`manual` 必须携带 `password`（否则 400） | 文档描述与实测一致；给出三种模式的请求示例 |
| 6 | 卫生（低） | 调试残留物 | 仓库根存在 `tmp-read-users.go` 与 `.tmp-read-users.go`（内容完全相同，只读 SQLite 探测工具，无凭据，**未被 git 跟踪**）；`data-smoke-20260829/` 仅剩 `filebox.db`（被 `*.db` 忽略）与空目录 | 删除两个 `.go` 残留（确认不影响 `go test ./...`：二者为根目录 `package main`，删除后构建面更干净）；`data-smoke-20260829/` 属测试数据，**先确认再删**（若保留则确认其内容始终不被跟踪）；完成后 `git status` 应无新增未跟踪文件 | `git status --short` 无这两个文件；`go test ./...` 全绿；仓库根无遗留调试脚本 |
| 7 | 运维（中，**仓库外，需人工**） | 工作区脚本硬编码 OpenAI API key | `C:\Users\huangcp\dsh-project\run-codex.ps1` 明文写入 `$env:OPENAI_API_KEY = "sk-b4bf…"`，且以 `-s danger-full-access` 运行 codex。该文件在 workspace 根，**不在 filebox 仓库内、未推送 GitHub** | **人工执行**：①立即吊销/轮换该 key；②脚本改为 `if (-not $env:OPENAI_API_KEY) { throw 'OPENAI_API_KEY not set' }`（不回显、不落盘）；③确认该文件永远不进入任何仓库（工作区根 `.gitignore` 已忽略相关目录，可再显式加该文件名） | 旧 key 已失效；脚本内无明文密钥（grep `sk-` 无命中）；新 key 通过环境变量注入 |
| 8 | 验证（必做） | 全量回归 | — | 8a 离线：`go test ./...`、`go test -race ./internal/httpapi/`、（含前端改动时）`npm run build` 且同步 `internal/webassets/dist`；8b 在线：按下方"在线复现清单"在**临时实例**验证 | 8a 全绿；8b 全部符合预期；`CHANGELOG.md`/`STATE.md` 登记 v027 |

## 二、分批执行

| 批 | 内容 | 前置 | 验证 | 影响文件（预估） |
|----|------|------|------|------------------|
| 1 | #1 reauth 限速 + #2 TOTP 防重放 | 无 | `go test ./internal/httpapi/` | `internal/httpapi/server.go`、`server_test.go`（或新增 `clear_all_test.go`） |
| 2 | #3 触发协调器竞态 | 无 | `go test -race ./internal/httpapi/` | `internal/httpapi/sync_trigger.go` |
| 3 | #4/#5 文档修正 | 无 | 人工核对 | `docs/DEV_DOC.md`、`README.md`/`README.en.md` |
| 4 | #6 卫生清理 | 用户确认 data-smoke 处理方式 | `git status`、`go test ./...` | 仓库根 |
| 5 | #7 密钥轮换 | **用户人工** | grep 无 `sk-` | workspace 根脚本（仓库外） |
| 6 | #8 回归 + 发布登记 | 1–5 | 8a/8b 全绿 | `CHANGELOG.md`、`STATE.md`、`RELEASE_NOTES*` |

## 三、在线复现清单（临时实例；验证后必须停服）

> 服务当前为**停止**状态。临时拉起（数据目录沿用演示目录，勿改动数据）：
> `filebox-v026.exe --addr=127.0.0.1:18080 --data=<demo>\data --log-enabled=true --log-dir=<demo>\logs`
> 验证完毕后 `Stop-Process`，保持环境恢复为停止状态。**成功路径会清空文件，只允许在一次性用户上执行。**

1. 用普通用户登录取 token；`POST /api/files/clear-all {"password":"wrong"}` 连续 6 次 → 第 6 次应为 **429 REAUTH_RATE_LIMITED**（修复前为 12×401）。
2. 失败后管理员 `GET /api/admin/locks` → `ipLocks` 应包含该来源 IP（修复前为空）。
3. 桶满窗口内提交正确密码 → 仍 **200**（正确路径不消耗失败桶）。
4. 对一次性用户启用 TOTP（`PUT /api/admin/users/{id}/totp {enabled:true,reenroll:true}`）→ 绑定后同一动态码两次 `clear-all` → 第二次应 **401**（修复前 200）。
5. `GET /api/files?sortBy=<非法>` → 400；`sortBy=name;DROP TABLE files--` → 400；文件列表仍 200（回归确认排序白名单未被本次改动破坏）。
6. 验证结束后删除一次性用户、撤销其收集链接；确认演示账号文件数未变。

## 四、不在本次范围（设计边界，仅记录）

- 管理员可解密任意用户的同步凭据（`GET /api/sync/systems/{id}/secret`）：管理员固有能力，已由独立 `FILEBOX_ENCRYPTION_KEY` 缓解，保持现状。
- 无内置 TLS/HSTS（由反代承担，`deploy/nginx.conf.example` 已给 HSTS 示例）；`trustProxy` 由管理员开关，属信任决策。
- 前端 token 存 localStorage（由 CSP `script-src 'self'` 缓解）；单机单进程 + SQLite 单写者；审计日志无防篡改（WORM）。
- `data-smoke-20260829/`、`.test-data/`、`bin/`、`dist/` 均未被 git 跟踪（git 卫生良好），无需清理即可，但建议逐步移出源码树。
