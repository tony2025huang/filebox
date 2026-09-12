# FileBox v027 安全规格：clear-all 二次认证加固

- 背景：v026 引入 `POST /api/files/clear-all`（一次性清空当前用户全部文件，破坏性操作），要求二次认证（有 TOTP 用动态码，否则用密码）。
- 问题（2026-09-12 复审 + 在线实证）：
  1. **失败无节流**：`clearFilesReauth` 失败仅返回错误，不消耗限速桶、不写 `ip_failures`。实测连续 12 次错误密码全部 `401`、`/api/admin/locks` 为空、账号未被锁 → 持有被盗 JWT 者可把该接口当作**无限次的密码/TOTP 校验预言机**（每次一次 bcrypt，无上限），猜中即批量清空该用户文件。
  2. **动态码可重放**：TOTP 分支命中即返回，未调用 `ConsumeTOTP`。实测同一 6 位码连续两次 `clear-all` 均 `200`（登录路径有防重放）。
- 并发附带项：`sync_trigger.go` 在 `triggerMu` 锁外读取 `s.triggerCtx`（数据竞争）。

## 设计

**两级令牌桶（key = `userID + "\x00" + 来源IP`）**

| 桶 | 键后缀 | 限额 | 消耗时机 | 作用 |
|---|---|---|---|---|
| 尝试桶 | `\x00clear-all-attempt` | 10 次/分钟，burst 10 | **每次尝试（含正确）** | 约束 bcrypt/TOTP 计算的 CPU 占用，正确凭据在攻击者被限速时仍可用 |
| 失败桶 | `\x00clear-all-failed` | 5 次/分钟，burst 5 | 仅凭据错误 / 动态码失效 | 连续错误返回 `429 REAUTH_RATE_LIMITED` |

- 判定枚举：`clearReauthAllow` / `clearReauthUnauthorized` / `clearReauthRateLimited`；仅 `ConsumeTOTP` 内部错误返回 500。
- 失败（含限速）调用 `recordIPFailure` 计入来源 IP 失败窗口（R-IPBAN）；**刻意不接入账号级锁定**——持 token 者不应能把受害者账号锁死（避免把认证加固变成 DoS 升级）。
- TOTP 分支记录 `matchedCounter` 并调用 `store.ConsumeTOTP`，未消费（重放/旧码）按失败处理；保持 ±1 窗口与 `hmac.Equal` 常量时间比较。
- 行为不变：`must_change_password` 门禁、只读时段拦截（`403 READ_ONLY`）、无凭据 `401`、成功后 `ClearUserFiles` 事务 + 审计 + 触发同步通知。

**并发修复**：新增 `(*Server).triggerContextErr()`（内部持 `triggerMu`），`runTriggeredTask` 改用它判定取消；`triggerCtx` 的所有访问均在锁内或经该 helper。

## 验收（离线 + 在线）

离线（`internal/httpapi/clear_all_reauth_test.go`）：
- 前 5 次错误 `401`、第 6 次 `429 REAUTH_RATE_LIMITED`；`ip_failures` 有记录；账号 `failed_attempts=0`、`locked_until` 为空；失败桶耗尽后提交**正确**密码仍 `200`。
- 尝试桶：10 次尝试后再提交正确凭据 → `429`（CPU 边界有效）。
- TOTP：首次成功后同码重放 → `401`。
- 既有 5 个 clear-all 用例（密码成功/只读拒绝/错误密码/TOTP 拒绝密码/TOTP 成功）保持全绿。
- 全量 `go test ./...` 全绿。
- **`go test -race` 已实测通过、无 DATA RACE**（便携 w64devkit GCC 16.2.0 + `CGO_ENABLED=1`）：`./internal/httpapi/` 411.7s、`./internal/store/` 58.2s、`./internal/srvlog/` 1.6s、`./cmd/filebox/` 17.5s，均 `ok`。
- 测试加固：`-race` 下 instrumented bcrypt 慢 10–20 倍会使令牌桶在测试中途补充，故 #1 尝试桶与收集密码失败桶两条 HTTP 级断言在 `-race` 下跳过（新增 `raceDetectorEnabled` 构建标记），桶语义改由 `TestClearAllReauthLimiterBoundsAttemptsDeterministically`（1 令牌/分钟，测试期间不可能补充）在所有模式固定——属测试时序脆弱，非实现缺陷。

在线（临时实例，验证后停服）：
1. 连续 6 次错误密码 → 第 6 次 `429 REAUTH_RATE_LIMITED`（修复前 12 次全 `401`）。
2. `GET /api/admin/locks` → `ipLocks` 含该来源 IP（修复前为空）。
3. 桶满窗口内正确密码 → `200`。
4. TOTP 绑定后同码两次 → 第二次 `401`。
5. 排序/收集元数据等既有行为回归正常。

## 影响文件

- `internal/httpapi/server.go`：`clearReauthDecision` + 两级桶 + `clearFilesReauth(r, ...)` 签名 + `clearAllFiles` 处理限速/失败计数。
- `internal/httpapi/sync_trigger.go`：`triggerContextErr()` 与调用点。
- `internal/httpapi/clear_all_reauth_test.go`（新增）。
- 文档：`docs/DEV_DOC.md`（XFF 语义 ×3 + 草案声明）、`README.md`（收集默认密码语义）、`docs/requirements/CHANGELOG.md`、`docs/requirements/STATE.md`。

## 未修复（设计边界，记录）

- 账号级锁定不接入 clear-all 失败（见上，防 DoS 升级）；如需更强约束，可由管理员调低 `ipLockThreshold` 以 IP 维度生效。
- 运行 `-race` 需要 C 编译器：本机原无编译器，已下载**便携 w64devkit GCC 16.2.0** 到工作区 `tools/`（不修改系统 PATH/注册表），CI/Linux 侧安装 mingw-w64（Windows）或系统 gcc 并设 `CGO_ENABLED=1` 即可复现。
- 测试脚本侧注意（非产品问题）：绑定 TOTP 会消费当前 30 秒窗口的动态码，脚本若在绑定后立刻复用同一（或刚被消费的相邻）窗口码会得到 401；按真实用法等待新窗口后即 200。偏移扫描（+1 接受、+2/-1/+3/-2 拒绝）已确认窗口与消费语义正确。
