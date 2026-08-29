# Codex 变更补丁任务 — 阶段一增量 7（R-OPS 后台命令行运维）

你在 `C:\Users\huangcp\dsh-project\filebox` 完成了 FileBox 阶段一 MVP 及补丁 1B–1H（如 1H 高级安全未完成先完成：本任务依赖 1H 的 must_change_password、ip_failures、用户锁定字段）。现在实现用户第七批需求。**先完整阅读 `docs/DEV_DOC.md`（第 1 节 R-OPS、9.6 节）**，再动手。

## 需求：单文件二进制提供后台运维子命令（Linux/Windows 一致）

Web 服务之外的本机运维兜底通道，直接操作 SQLite，**不经过 HTTP/鉴权**：

```bash
# 重置密码（指定新密码；重置后该用户 must_change_password=true，下次登录须改密）
filebox admin reset-password --data=./data --username=admin --new-password='新密码'
filebox admin reset-password --data=./data --username=admin --generate   # 生成强密码并打印一次

# 查看锁定信息
filebox locks list --data=./data

# 删除锁定信息
filebox locks clear --data=./data --ip=1.2.3.4
filebox locks clear --data=./data --user=2
filebox locks clear --data=./data --all
```

## 实现

1. **命令分发**（`cmd/filebox/main.go` 重构）：
   - 无子命令（或 `serve` 子命令）→ 现有 Web 服务逻辑（保持全部现有 flag 行为不变）。
   - `admin reset-password`：`--data`（默认 ./data）、`--username`（默认 admin）、`--new-password`、`--generate`（与 --new-password 互斥，同时给则报错退出码 2）；用户不存在 → stderr 报错退出码 1；成功后 stdout 输出成功信息（generate 时打印一次性密码，并提示用户登录后强制改密）。
   - `locks list`：`--data`；输出两张表（文本表格）：IP 锁定（ip / failed_count / window_started_at / locked_until / 状态(锁定中|未锁定)）与用户锁定（id / username / failed_attempts / locked_until / 状态）；无记录输出「无锁定信息」。
   - `locks clear`：`--data`；`--ip`（精确匹配 ip_failures.ip）、`--user`（用户 id）、`--all`（清空全部 ip_failures 与所有用户的 failed_attempts/locked_until）；三者互斥（同时给 → 退出码 2）；删除后输出删除摘要；找不到目标 → 退出码 1。
   - 统一：未知子命令 → stderr 用法说明，退出码 2。
2. **store 层新增方法**（`internal/store`）：
   - `ResetPassword(username, newHash string) (int64, error)`：更新 password_hash 并置 `must_change_password=1`、清 failed_attempts/locked_until。
   - `ListLocks(ctx) (ipLocks, userLocks []..., error)`：ip_failures 全表 + users 表 failed_attempts>0 或 locked_until 非空。
   - `ClearIPLock(ctx, ip) (bool, error)`、`ClearUserLock(ctx, id) (bool, error)`、`ClearAllLocks(ctx) (int, error)`。
3. **SQLite 并发**：Web 服务运行中执行 CLI 可能撞库锁；`store.Open` 的 DSN 增加 `?_pragma=busy_timeout(5000)`（modernc.org/sqlite 支持该 DSN 参数；如已有 DSN 参数则追加），保证 CLI 与运行中实例短时共存安全。
4. 密码生成：`--generate` 用 `crypto/rand` 生成 16 位密码（保证至少含大写/小写/数字/特殊各 1），stdout 打印一次。
5. 子命令不校验 Web 密码强度策略（运维兜底），但**必须**置 `must_change_password=1`。

## 执行要求

1. 先读 `docs/DEV_DOC.md`（9.6 节）；确认 1H 已完成再开始。
2. 验证：`go build ./...`、`go vet ./...`、`go test ./...`、Linux 交叉编译。
3. 冒烟（Windows 本机即可，Linux 交叉编译产物仅构建验证）：
   - `go run ./cmd/filebox --data=./data-ops`（后台启动）→ 登录 admin/admin123 改密后退出；或直接初始化数据目录。
   - `go run ./cmd/filebox admin reset-password --data=./data-ops --username=admin --new-password=NewPass123!` → 用新密码登录成功且 me.mustChangePassword=true → 改密后恢复。
   - `--generate` 打印密码可登录。
   - 制造一个 IP 锁定（多次错误登录）→ `locks list` 可见 → `locks clear --all` → `locks list` 无记录；Web 登录恢复。
   - 未知子命令与参数冲突 → 退出码 2。
   - 删除 `data-ops/`。
4. 更新 `docs/requirements/CHANGELOG.md`（追加 R-OPS 变更）、`docs/requirements/STATE.md`（R-OPS done）；同步 `README.md`/`README.en.md`（新增「后台运维命令」章节：三个子命令示例与用途）。
5. 最终报告：改动文件清单、验证结果、冒烟摘要。
