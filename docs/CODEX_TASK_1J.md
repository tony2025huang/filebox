# Codex 变更补丁任务 — 阶段一增量 8（R-SRVLOG 服务日志 / R-SERVICE 服务化部署）

你在 `C:\Users\huangcp\dsh-project\filebox` 完成了 FileBox 阶段一 MVP 及补丁 1B–1I（如 1I 运维命令未完成先完成）。现在实现用户第七批需求。**先完整阅读 `docs/DEV_DOC.md`（第 1 节 R-SRVLOG/R-SERVICE、9.7/9.8 节）**，再动手。新增代码注释中英双语（遵循 1E 规范）。

## 变更 1：R-SRVLOG — 服务日志

1. **`internal/srvlog` 新包**：
   - `Config{Enabled bool, Dir string, RetentionDays int}`；`New(cfg) *Logger`。
   - 按天滚动 writer：文件名 `filebox-YYYY-MM-DD.log`（本地时区）；写入时检测日期变化 → 关闭旧文件、将旧文件 **gzip 压缩**为 `filebox-YYYY-MM-DD.log.gz`（删除原文件）、打开新文件；跨进程同日文件用 O_APPEND|O_CREATE 打开（多实例同目录安全追加）。
   - 保留清理：启动时与每次滚动后扫描日志目录，删除 `filebox-*.log(.gz)` 中 mtime/文件名日期早于（今天 - RetentionDays）的文件。
   - 未启用时 Logger 退化为控制台输出（内部仅用标准库 `log`，无第三方依赖）。
   - 提供 `Infof/Errorf` 与 `Event(event, format, args...)`（事件行格式：`<RFC3339时间> <级别> [<event>] <详情>`）。
   - **Event 前两个格式化参数必须为操作者与来源 IP**：约定 `Event(event, "operator=%s ip=%s ...", operator, ip, ...)`，即每条事件行包含 `operator=<用户名|system|cli>` 与 `ip=<来源IP|->` 字段（详见事件埋点要求）。
2. **`cmd/filebox/main.go`**：新增 flag `--log-enabled`（默认 false）、`--log-dir`（默认 `filepath.Join(filepath.Dir(os.Executable()), "logs")`，即程序执行目录下 logs/；支持 env `FILEBOX_LOG_ENABLED/FILEBOX_LOG_DIR/FILEBOX_LOG_RETENTION_DAYS`）、`--log-retention-days`（默认 90）；初始化 srvlog 并注入 httpapi.Config。
3. **事件埋点（服务日志，覆盖文档 9.7 清单；每条事件必带 operator 字段；Web 请求事件必带 ip 字段）**：
   - 启动（版本/addr/data/log 配置摘要，operator=system ip=-）、正常退出（含收到 SIGINT/SIGTERM 时，operator=system ip=-）；
   - 登录成功/失败（operator=尝试登录的用户名，ip=requestIP(r)，含失败 reason）、登出（operator=当前用户 ip=requestIP(r)）；
   - 文件列表查看（operator=当前用户 ip=requestIP(r)）、文件详情/预览查看（同上）；
   - 上传完成（operator=当前用户 ip=requestIP(r) + name+size+哈希摘要）、下载（operator=当前用户 ip=requestIP(r) + name+result）、删除（operator=当前用户 ip=requestIP(r) + name）；
   - 管理事件（operator=执行操作的管理员 ip=requestIP(r)，target=作用对象）：用户创建/修改/**禁用/解禁**（disabled 变化单独事件，target=目标用户名）、重置密码（target=目标用户）、删除用户（target）、settings 变更（target=变更项）、brand 变更、锁定解除（用户/IP，Web 与 R-OPS CLI 均记录）、TOTP 开关（target=目标用户）、IP 白名单变更（target=目标用户）；
   - R-OPS 命令执行记录（operator=cli ip=-，含子命令与目标参数摘要；CLI 模式下若未启用文件日志则仅控制台）。
   - **绝不记录**密码、token、文件内容。
4. 审计日志（DB）保持现状，服务日志是其补充（运维视角）。

## 变更 2：R-SERVICE — 服务化部署

新增 `deploy/` 目录：
1. **`deploy/filebox.service`**（Linux systemd 示例）：`[Unit]`、`[Service]`：`User=filebox`、`ExecStart=/opt/filebox/filebox --data=/var/lib/filebox --log-enabled=true --log-dir=/var/log/filebox`、`WorkingDirectory=/opt/filebox`、`Environment=FILEBOX_JWT_SECRET=CHANGE_ME`、`Restart=on-failure`、`RestartSec=3`、`[Install] WantedBy=multi-user.target`；注释说明安装步骤（用户创建、目录权限、systemctl enable --now）。
2. **`deploy/install-service.ps1`**（Windows 示例脚本）：优先 NSSM（检测 `nssm` 命令，`nssm install FileBox <exe路径> --data=<data目录> --log-enabled=true --log-dir=<logs目录>`，nssm set FileBox AppEnvironmentExtra 设置 FILEBOX_JWT_SECRET、AppStdout/AppStderr 重定向到日志目录、Start SERVICE_AUTO_START），无 nssm 时给出 `sc create FileBox binPath= ...` 基础方案与 `sc start/stop/delete` 注释；带参数校验与错误提示。
3. **`deploy/nginx.conf.example`**（R-PROXY）：完整 Nginx server 块示例——HTTP→HTTPS 跳转注释、SSL 配置占位、`location /`（反代 + `try_files $uri /index.html;`）、`location /api/` 与 `location /brand/`（反代 `127.0.0.1:8080`）、关键指令 `client_max_body_size 0;`、`proxy_request_buffering off;`、`proxy_read_timeout 600s;`、请求头 `X-Forwarded-For $proxy_add_x_forwarded_for;` / `X-Real-IP $remote_addr` / `X-Forwarded-Proto $scheme;`；文件头部注释说明对应 FileBox 侧 `--trusted-proxies=127.0.0.1/32` 配置。
4. **`deploy/README.md` 与 `deploy/README.en.md`**：中英部署说明（Linux systemd 完整步骤；Windows NSSM 推荐 + sc 备选；**Nginx 反代部署（含可信代理配置与安全说明）**；服务化最佳实践：独立用户、专用数据目录、强 jwt-secret、启用服务日志、日志位置与归档说明、升级替换二进制的步骤）。

## 执行要求

1. 先读 `docs/DEV_DOC.md`（9.7/9.8 节）；确认 1I 已完成再开始。
2. 验证：`go build ./...`、`go vet ./...`、`go test ./...`、Linux 交叉编译。
3. 冒烟（Windows 本机）：
   - `go run ./cmd/filebox --data=./data-srvlog --log-enabled=true --log-dir=./logs-smoke` → 登录/上传/下载/登出 + admin 修改设置 → 检查 `logs-smoke/filebox-<今天>.log` 含对应事件行（login/upload/download/admin 等）；控制台同时输出。
   - 未加 `--log-enabled` → 无日志文件产生，仅控制台。
   - 压缩归档与保留清理：手工构造一个昨天的旧日志文件（如 `filebox-<昨天>.log` 内容随意）放入 logs-smoke，触发一次新日志写入 → 旧文件被压缩为 `.log.gz`；设 `--log-retention-days=1` 并把一个 3 天前的 `.log.gz` 放入 → 启动后该归档被删除。
   - 删除 `data-srvlog/` 与 `logs-smoke/`。
4. 更新 `docs/requirements/CHANGELOG.md`（追加第七批变更）、`docs/requirements/STATE.md`（R-SRVLOG/R-SERVICE done）；同步 `README.md`/`README.en.md`（配置项表加三个日志 flag；新增「服务日志」与「服务化部署」章节指向 deploy/）。
5. 最终报告：改动文件清单、验证结果、冒烟摘要。
