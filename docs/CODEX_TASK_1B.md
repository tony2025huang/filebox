# Codex 变更补丁任务 — 阶段一增量（R-DISK / R-NAME）

你在 `C:\Users\huangcp\dsh-project\filebox` 已完成 FileBox 阶段一 MVP。现在实现用户新确认的三条需求变更。**先完整阅读 `docs/DEV_DOC.md`**（已更新，含 R-DISK、R-NAME 与重名规则），再动手。请使用 skills：`web-app-dev`、`file-transfer-design`。

## 变更 1：R-NAME — 磁盘保留原文件名（含重名规则）

当前落盘名是随机 hex（`randomID()`）。改为：

1. **文件名消毒**（`sanitizeName` 扩展）：
   - 现有处理保留（去路径分隔符、控制字符、TrimSpace、filepath.Base）
   - 新增：将 Windows 非法字符 `<>:"|?*` 替换为 `_`
   - 长度限制改为 ≤255 **字节**（UTF-8 字节数）
2. **存储目录**：`data/files/<user_id>/<yy>/<mm>/<stored_name>`（按用户 + 日期分目录）。
3. **重名规则（用户明确要求）**：**仅同一目录内**存在同名文件时才追加 ` (1)`、` (2)`… 序号（如 `a (1).txt`）；**不同目录（不同用户/不同日期）下的同名文件各自独立，不加序号**。
4. **分配时机**：在 `CompleteUpload` 的数据库事务内分配最终 `stored_name`（查询同用户同日期目录下已有文件名，找到最小可用序号），避免并发冲突。
5. **Schema 调整**：`files.stored_name` 去掉 UNIQUE（跨目录同名 stored_name 相同）；改为 `storage_path` 加 UNIQUE。
6. DB 的 `name` 保存原始文件名（含原字符，不消毒）；`stored_name` 保存落盘文件名。
7. 上传过程中（init→complete）临时文件仍写 `data/tmp/<task_id>/<index>`，complete 时移动到最终路径（按用户/日期目录建目录）。

## 变更 2：R-DISK — 磁盘监控与系统保护

1. 新建 `internal/diskusage/` 包（跨平台）：
   - `DiskUsage(dir string) (total, free, used int64, err error)`
   - Windows：`syscall.GetDiskFreeSpaceEx`；Linux：`syscall.Statfs`（用 build tags 分文件：`diskusage_windows.go` / `diskusage_linux.go` / `diskusage_other.go` 返回错误）
2. `cmd/filebox/main.go` 新增 flag：`--min-free-space`（int64，默认 `2*1024*1024*1024`，`0` = 关闭保护），支持环境变量 `FILEBOX_MIN_FREE_SPACE`，注入 Config。
3. **上传保护**：`POST /api/files/upload-init` 先探测磁盘：可用空间 < minFreeSpace → `503`，错误体 `{code:503, message:"系统存储空间不足，暂时禁止上传", data:{code:"DISK_FULL"}}`（与现有响应结构一致即可，message 必须含该文案）；不阻断下载/删除/管理。
4. **统计**：`GET /api/admin/stats` 返回新增字段 `disk`：`{total, used, free, usagePercent}`（usagePercent 取整）。
5. **前端**：`AdminView.vue` 统计卡片区新增「磁盘占用」卡片：显示 `formatBytes(used) / formatBytes(total)` 与百分比进度条；usagePercent ≥ 90% 时卡片加警告样式（红色/橙色）。`.stats-grid` 改为 4 列（grid-template-columns: repeat(4, 1fr)），移动端仍 1 列。

## 变更 3：md5 双哈希保持

已实现，勿回退：files 表 sha256 + md5 双字段，complete 时都计算保存；`publicFile` 与前端表格展示保持不变。

## 执行要求

1. 先读 `docs/DEV_DOC.md` 全文。
2. 保持现有代码风格（标准库、中文错误消息、`{code,message,data}` 响应）。
3. 验证必须全部通过：
   - `go build ./...`、`go vet ./...`、`go test ./...`
   - 前端改动后：`npm --prefix web run build`，再 `go run ./scripts/sync-web.go` 同步 embed
4. 冒烟（启动 `go run ./cmd/filebox --data=./data-patch` 后 curl）：
   - 登录 admin/admin123 → 上传两个同名文件 `a.txt`：第二个应落盘为 `a (1).txt`（同目录重名加序号）
   - 用另一个用户上传 `a.txt`：**不应加序号**（不同用户目录同名）
   - `GET /api/admin/stats` 返回 disk 字段且数值合理
   - 用 `--min-free-space=107374182400`（100GB，大于实际可用）启动：upload-init 返回 503 且 message 含「系统存储空间不足」；下载/删除/列表正常
   - 冒烟后删除 `data-patch/` 测试数据
5. 更新 `docs/requirements/CHANGELOG.md`（追加 2026-08-29 变更记录：R-DISK、R-NAME、重名规则），`docs/requirements/STATE.md` 同步（新增/更新 R-DISK、R-NAME 条目为 confirmed/done）。
6. 最终报告：改动文件清单、验证结果、冒烟输出摘要。
