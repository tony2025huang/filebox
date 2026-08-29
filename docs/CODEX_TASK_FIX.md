# Codex 修复任务 — 阶段一缺陷修复（D1–D4）

你在 `C:\Users\huangcp\dsh-project\filebox` 完成了 FileBox 阶段一全部开发。DSH 二次测试（docs/TEST_REPORT.md）发现 4 项问题，请全部修复。**先读 `docs/TEST_REPORT.md` 与 `docs/DEV_DOC.md` 相关章节**。新代码注释保持中英双语；新增前端文案进三语言 i18n 字典；修复后同步 `docs/requirements/CHANGELOG.md` 与 `STATE.md`。

## D1（轻微）：重命名落盘序号应在扩展名前
- 现状：同目录重名选择 rename 后，磁盘文件为 `conflict.txt (1)`（`storageNameCandidate` 中 `stem + extension + trailer`）。
- 要求：按文档约定改为 `conflict (1).txt`（扩展名前、序号后），即 `stem + trailer + extension`；注意 255 字节截断逻辑同步调整（extension 优先保留）。回归：同目录重名 rename 后磁盘文件名符合 `name (1).ext`。

## D2（一般）：0 字节文件应可上传
- 现状：`size=0` 时 `totalChunks=ceil(0/chunkSize)=0`，服务端校验 `totalChunks != 1` → 400「阶段一仅支持单分片上传」。
- 要求：`size=0` 的文件按单分片处理（totalChunks 视为 1），upload-init 允许；complete 时 0 字节文件正常落库，md5 应为 `d41d8cd98f00b204e9800998ecf8427e`，sha256 为 `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`；磁盘文件 0 字节正常。注意 chunkSize 为 0 时已有兜底（置 1），保持兼容。

## D3（轻微）：CLI 增加 IP 白名单自救命令
- 现状：管理员配置错自身 IP 白名单后 Web 被 403 自锁，CLI 无解除手段。
- 要求：新增 `filebox admin clear-ip-acl --data=./data --username=<user>`：将该用户 `ip_acl_enabled=0`、`ip_whitelist=''`；用户不存在退出码 1，参数错误退出码 2；store 新增 `ClearIPACL(username) (bool, error)`；写入服务日志（operator=cli，参考现有 reset-password 模式）；README（中英）后台运维命令章节补充该命令；usage 提示同步。

## D4（建议）：settings 部分更新语义与错误提示
- 现状：`PUT /api/admin/settings` 全量覆盖，缺字段按零值校验（如缺 passwordMinLength → 400「设置无效」），信息笼统。
- 要求（轻量方案，不破坏前端兼容）：`logSettingsRequest` 的数字/布尔字段改为指针（`*int`/`*bool`），未提供字段保持原值；`updateSettings` 先读取现有设置再合并；校验失败时返回 `400` + message「设置无效」→ 改为带具体字段的错误信息（如「密码最小长度无效」）。前端现有全量提交不受影响，需回归验证。

## 执行要求
1. 先读 `docs/TEST_REPORT.md`、`docs/DEV_DOC.md`（9.6 运维命令、2.5 注释规范）。
2. 验证全过：`go build ./...`、`go vet ./...`、`go test ./...`、Linux 交叉编译；前端如有改动 `npm --prefix web run build` + `go run ./scripts/sync-web.go`。
3. 冒烟（`go run ./cmd/filebox --data=./data-fix`，admin 密码用 `Admin12345!` 避免强度问题）：
   - D1：上传 a.txt 两次（第二次 rename）→ 磁盘出现 `a (1).txt`。
   - D2：upload-init size=0 → 200；complete → md5=d41d8cd9…；列表可见。
   - D3：`filebox admin clear-ip-acl --username=admin` → 清除后 admin 可访问；不存在用户 → 退出码 1。
   - D4：`PUT /api/admin/settings {"ipLockThreshold":3}`（仅一个字段）→ 200 且其他字段保持不变；非法值（如 passwordMinLength=0）→ 400 且提示具体字段。
   - 清理 `data-fix/`。
4. 最终报告：改动文件、验证结果、冒烟摘要。
