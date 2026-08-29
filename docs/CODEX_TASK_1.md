# Codex 开发任务 — 阶段一（MVP）

请完整阅读并遵循 `docs/DEV_DOC.md`（本项目开发文档，需求与验收以它为准）。本文件定义阶段一的执行范围与约束。

## 目标
在 `C:\Users\huangcp\dsh-project\filebox` 下开发 FileBox 文件传输系统 **阶段一 MVP**，代码可编译、可运行、接口与页面可用。

## 范围（仅阶段一，不要实现阶段二功能）
- 登录 / 登出 / 当前用户信息（JWT + bcrypt）
- 文件管理：列表（分页+关键字）、整体上传（单分片，≤100MB 直接传）、流式下载（含 Range 206）、删除（软删除+物理清理）
- 多用户：管理员创建用户、修改（角色/配额/禁用/重置密码）、删除用户、用户列表
- 首次启动自动创建管理员 `admin / admin123`
- SQLite 持久化（modernc.org/sqlite，纯 Go）
- 配额检查：上传前校验 used_bytes + size <= quota_bytes，超额拒绝（默认配额 100GB）
- 文件完整性：complete 时计算并保存 **md5 与 sha256 双哈希**，随 API 返回
- 前端页面（Vue3）：`/login` 登录页、`/` 文件管理页（列表/上传/下载/删除/进度）、`/admin` 管理后台（用户 CRUD + 统计卡片）
- README.md（环境要求、开发/构建/运行步骤、配置项）

## 技术约束
- 后端 Go（标准库 net/http 方法路由，Go 1.22+ 语法可用）；依赖保持最小
- 前端 Vue3 + Vite + vue-router，中文界面；构建产物由 Go `embed` 打进二进制
- 目录结构：`cmd/filebox/`（入口）、`internal/`（handler/service/store 分层）、`web/`（前端）、`docs/`、`README.md`
- 存储目录默认 `./data`（文件内容按 `data/files/yy/mm/<stored_name>`，DB 为 `data/filebox.db`）
- 配置：命令行 flag `--addr`（默认 :8080）、`--data`（默认 ./data）、`--max-file-size`（默认 100GB）、JWT 过期默认 7 天
- 统一响应 `{code, message, data}`；错误不泄露内部路径
- API 归属校验：用户只能操作自己的文件；管理接口仅 admin
- 文件名消毒（服务端 UUID 存储名）；下载 Content-Disposition 正确；响应头 nosniff
- **不要**实现：分片/断点续传、秒传、文件夹上传、分享链接、预览、限速、注册（这些是阶段二）

## 使用 skills
本环境已安装以下 skills，请按需使用（读 `~/.codex/skills/<name>/SKILL.md`）：
- `web-app-dev`（工程规范）
- `file-transfer-design`（传输设计参考）

## 执行要求
1. 先读 `docs/DEV_DOC.md` 全文，再动手。
2. 前后端都要实际写完代码；后端提供 `curl` 冒烟（登录→建用户→上传→下载→删除）。
3. 保证 `go build ./...`、`go vet ./...` 通过；前端 `npm run build` 通过（需要 Node 时先安装：Node 不在 PATH，请检查并自行处理，或跳过前端构建但保证代码完整可构建）。
4. 项目根已有 `.gitignore`；**不要**提交 `data/`、`.env`、`web/node_modules`。
5. 完成后在 `docs/requirements/STATE.md` 建立需求状态表（阶段一需求条目标 confirmed/in-progress/done），在 `docs/requirements/CHANGELOG.md` 记录本次开发。
6. 最后输出：完成的文件清单、如何启动、curl 冒烟结果、已知限制。
