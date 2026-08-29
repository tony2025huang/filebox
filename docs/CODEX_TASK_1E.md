# Codex 文档任务 — 阶段一收尾（双语 README、发布说明与代码注释）

你在 `C:\Users\huangcp\dsh-project\filebox` 完成了 FileBox 阶段一全部代码（MVP + R-DISK/R-NAME/R-CONFLICT/R-VALID/R-LOG/R-LOCK/R-BRAND，如 1D 未完成先完成）。现在生成**中英双语交付物**（文档 + 代码注释）。**先完整阅读 `docs/DEV_DOC.md`（重点 2.5 注释规范与 9.5 文档要求）与 `docs/requirements/STATE.md`**，并核对实际代码（flag、接口、默认值以代码为准）。

## 交付物 A：代码注释中英双语（不改变任何代码逻辑）

1. 扫描 `cmd/`、`internal/`（全部 .go）、`scripts/`、`web/src/`（.js/.vue）下所有源文件。
2. 对以下注释补充英文（中文保留在前）或补充中文（英文保留在前），统一为「中文在前、英文在后」：
   - 所有导出符号（Go 导出函数/类型/常量/方法，JS 导出函数，Vue `<script setup>` 中的关键函数）的 doc 注释；
   - 关键业务逻辑处注释：鉴权/登录锁定、配额、冲突处理（overwrite/rename）、审计日志埋点与清理、磁盘保护、文件名消毒/前置校验、品牌渲染与空值处理、事务与并发、md5/sha256 计算、Range 下载等。
3. 简单行内注释（如 `// 删除临时文件`）保持单语即可；已双语的注释跳过。
4. **铁律：只改注释，不改任何代码逻辑、字符串、接口；`go build ./...`、`go vet ./...`、`go test ./...` 与 `npm run build` 必须保持通过**（改注释后重新跑一遍确认）。

## 交付物 B：双语 README 与发布说明

1. **`README.md`**（中文，完整版）与 **`README.en.md`**（英文，与中文内容一一对应）：
   - 顶部互相提供语言切换链接（`[English](README.en.md)` / `[中文](README.md)`）
   - 内容章节：项目简介（FileBox 文件传输系统，单文件跨平台部署）｜功能清单（阶段一全部：多用户与角色、JWT 登录与登录安全（连续失败锁定/自动解禁/统一错误提示防枚举）、文件上传下载删除（Range 206）、同名冲突覆盖/重命名选择、非法字符前置校验、配额（默认 100GB）、md5+sha256 双哈希、磁盘占用监控与可用空间保护（默认 2GB）、操作审计日志（留存周期可配）、品牌定制（标题/描述/favicon/登录页与主页 logo/ICP 与公安备案，空值不留空白）｜技术栈（Go 1.22+ 标准库、modernc SQLite、Vue3+Vite、embed 单文件）｜快速开始（开发模式：npm --prefix web install/run build → go run ./scripts/sync-web.go → go run ./cmd/filebox；生产构建：make build / make build-linux；默认管理员 admin/admin123 及改密提示）｜配置项表（--addr/--data/--max-file-size/--min-free-space/--jwt-secret/--register-enabled 及对应 FILEBOX_* 环境变量，注明默认值与含义）｜目录结构｜数据存储说明（data/ 布局、原文件名落盘与重名规则）｜已知限制（阶段二未实现：分片断点续传、秒传、文件夹上传、分享链接、在线预览、限速、开放注册）｜许可与致谢（可选）。
   - 内容必须与实际代码一致（如配置项、接口路径、默认值）。

2. **`RELEASE_NOTES.md`**（中文）与 **`RELEASE_NOTES.en.md`**（英文）：
   - 版本 `v0.1.0`（阶段一）
   - 本次交付：功能清单（同上，按 MVP + 各补丁分批列出，含 R-DISK/R-NAME/R-CONFLICT/R-VALID/R-LOG/R-LOCK/R-BRAND）
   - 安全提示：默认管理员 admin/admin123，首次登录请立即修改；公网部署请配置 --jwt-secret 与 HTTPS 反代
   - 部署与运行摘要（Windows/Linux 单二进制）
   - 已知限制与阶段二路线图
   - 变更历史：链接 docs/requirements/CHANGELOG.md

## 执行要求
1. 中文为主语言，英文版内容与中文版一一对应（不要遗漏章节）。
2. 交付物 A 只改注释；交付物 B 只写 4 个文档文件（README 已存在则替换）；不修改任何代码逻辑。
3. 核对：`go run ./cmd/filebox -h` 的输出与配置项表一致（如不一致以代码为准并说明）。
4. 更新 `docs/requirements/CHANGELOG.md`：追加「中英双语注释、双语 README 与发布说明」条目。
5. 最终报告：注释双语化覆盖的文件数与样例、4 个文档文件路径与章节列表、构建/测试复验结果。
