# FileBox

[English](README.en.md)

FileBox 是一个可自托管的文件传输与管理系统，使用单个 Go 二进制文件跨 Windows/Linux 部署。阶段一提供多用户隔离、JWT 登录、单文件上传、Range 下载、双哈希校验、配额、磁盘保护、审计日志、品牌定制和多语言界面；阶段二（v0.2.0）在此基础上加入分片断点续传、秒传、文件夹上传、分享链接、在线预览、上传限速、开放注册开关和系统统计补全。

## 功能清单

- 多用户与角色：`admin` 管理员和 `user` 普通用户；管理员可管理账户、角色、配额、密码和禁用状态，普通用户只能访问自己的文件。
- JWT 登录与安全：密码使用 bcrypt；JWT 默认有效期 7 天；连续失败可按后台策略临时或永久锁定，锁定可自动解除；登录失败统一返回“用户名或密码错误”，减少账号枚举风险。
- 文件传输：上传初始化、单分片 `0` 上传、完成提交、列表搜索/分页、删除和下载；`http.ServeContent` 支持 `Range`，范围请求返回 `206 Partial Content`。
- 分片断点续传：支持 2–8 MiB 分片，分片可乱序/重复上传（幂等覆盖），服务端以 `chunks` 表持久化已上传分片，`GET /api/files/{taskID}/status` 供断点续传，`complete` 校验缺片后流式合并并计算双哈希；前端 4 路并发分片、暂停/继续、失败重试。
- 秒传：`POST /api/files/check` 按 md5 优先、sha256 兜底在本人文件内匹配，命中直接返回已有记录，不重复落盘；前端用 WebCrypto 计算 sha256 实现秒传检查。
- 文件夹上传：`upload-init` 支持 `dir` 相对目录字段，保留 `data/files/<user>/<yy>/<mm>/<相对目录>/<name>` 结构；不同相对目录同名文件不加序号。
- 分享链接：创建（有效期/最大下载次数）、匿名元数据与匿名下载（过期/超次数拒绝）、撤销分享；分享创建/查看/下载计入审计日志；前端提供 `/:token` 匿名分享页。
- 在线预览：`GET /api/files/{id}/preview` 对图片/文本/视频/PDF/JSON 白名单 inline 输出，其余类型强制附件下载。
- 上传限速：按用户令牌桶（`golang.org/x/time/rate`），速率由管理员设置（字节/秒，`0`=不限，默认不限），作用于分片写入。
- 开放注册：`POST /api/auth/register` 由 `registerEnabled` 设置控制（默认关闭），开启后按密码策略创建普通用户并直接登录；登录页按公开开关显示注册入口。
- 传输体验（用户反馈批次）：顶栏「传输」按钮展开独立面板（区分上传/下载，下载带流式进度）；文件列表默认直接显示 md5（可勾选开关、持久化）；「上传文件夹」与拖拽目录均支持文件夹上传，空目录等无效拖拽给出明确中文提示。
- 同名冲突：同一用户当月目录存在同名文件时，初始化返回 `409`，前端可选择覆盖或重命名；覆盖事务性替换，重命名分配最小可用数字后缀。
- 文件名安全：上传前拒绝路径分隔符、控制字符、Windows 非法字符、点号遍历标记、空名/`.`/`..` 和超过 255 字节的名称；落盘名保留原名语义并替换部分非法字符。
- 配额：初始化时预留待上传字节，默认配额为 100 GiB；覆盖时先扣除旧文件占用。
- 完整性：服务端从实际内容计算 MD5 和 SHA-256；客户端可选提交期望值进行比对。
- 磁盘保护：管理员统计提供容量、已用、可用和占用百分比；默认要求数据目录所在文件系统至少保留 2 GiB，可调整或关闭。
- 操作审计：记录登录、上传完成和下载成功/失败，包括用户名、目标、来源 IP、结果和失败原因；用户只能看自己的记录，管理员可筛选全部记录；写入时按留存天数惰性清理，默认 30 天。
- 品牌定制：支持标题、SEO 描述、登录页/主页 logo、favicon、ICP 和公安备案文本；空文本不渲染空白区域，未设置资源使用内置 SVG；资源限制 512 KiB 并校验扩展名和内容。
- 多语言：支持简体中文、繁体中文和英文；管理员可设置系统默认语言，个人可设置语言并即时切换。
- 界面主题色定制：管理员可输入 `#RGB`/`#RRGGBB` 或使用色盘设置主色，支持恢复默认并即时应用全站主色。
- 高级账号安全：首次管理员账号可配置并强制改密；管理员可设置密码强度、TOTP 双因素认证、用户 IP 白名单、IP 登录锁定和锁定管理。

### 阶段交付标记

`MVP`：登录、JWT、bcrypt、SQLite、多用户隔离、文件上传/下载/删除、分页搜索、单文件单分片、MD5/SHA-256 和管理员控制台。

`R-DISK`：跨平台磁盘统计、最小可用空间配置和 `DISK_FULL` 上传保护。

`R-NAME`：按用户/月份分目录保存原始落盘文件名，并保证存储路径唯一。

`R-CONFLICT`：同目录同名文件返回 `409`，支持覆盖和数字后缀重命名。

`R-VALID`：上传文件名前置安全校验。

`R-LOG`：操作审计、留存清理、分页筛选和日志页面。

`R-LOCK`：失败登录阈值、临时/永久锁定、自动解锁、统一错误和管理员重置。

`R-BRAND`：文本与 logo/favicon 配置、资源校验、内置回退和备案页脚。

`R-LANG`：简体中文/繁体中文/英文三套界面文案，支持系统默认语言和个人语言偏好。

`R-THEME`：管理员可配置界面主色，支持十六进制输入、色盘选择、恢复默认和即时生效。

`R-INIT/R-PWD/R-TOTP/R-IPACL/R-IPBAN/R-LOCKADMIN`：初始账号强制改密、密码强度策略、加密 TOTP、来源 IP 白名单、IP 登录锁定和后台锁定管理。

`R-OPS`：提供不经过 Web 鉴权的本机后台运维命令，可在服务停止或 Web 不可用时直接维护 SQLite。请确保只有受信任的运维人员可以访问服务器 shell。

`R-SRVLOG`：可选服务文件日志，按本地日期滚动、gzip 归档并按保留天数清理；未启用时仅输出控制台。

`R-SERVICE/R-PROXY`：Linux systemd、Windows NSSM/sc 和 Nginx HTTPS 反向代理模板见 [`deploy/`](deploy/)。

`STAGE2`：分片断点续传、秒传、文件夹上传、分享链接、在线预览、上传限速、开放注册开关和系统统计补全（见上方功能清单）。

## 技术栈

Go 1.22+ 标准库 HTTP、`modernc.org/sqlite`、Vue 3、Vite、`vue-router`、`lucide-vue-next` 和 `embed`。Vite 生产产物与内置品牌资源嵌入 Go 二进制，运行时无需独立前端服务器。

## 快速开始

要求 Go 1.22+、Node.js 20+ 和 npm。开发模式：

```powershell
npm --prefix web install
npm --prefix web run build
go run ./scripts/sync-web.go
go run ./cmd/filebox
```

默认监听 `:8080`，打开 <http://localhost:8080>。首次启动创建 `admin/admin123`，首次登录后请立即修改密码或禁用账户。也可以运行 `go run ./cmd/filebox --addr=:8080 --data=./data`。仅开发前端时，在 `web/` 运行 `npm run dev`，Vite 将 `/api` 代理到 `http://localhost:8080`。

## 后台运维命令

单文件二进制同时提供本机运维子命令。`admin reset-password` 重置管理员或普通用户密码，重置后下次登录必须改密；`admin clear-ip-acl` 禁用指定用户的 IP 白名单并清空列表，用于管理员误配置白名单后的自救；`locks list` 查看 IP 与用户锁定状态；`locks clear` 按 IP、用户 ID 或全部清除锁定信息。`--generate` 会生成并仅打印一次 16 位强密码。

```bash
filebox admin reset-password --data=./data --username=admin --new-password='NewPass123!'
filebox admin reset-password --data=./data --username=admin --generate
filebox admin clear-ip-acl --data=./data --username=admin
filebox locks list --data=./data
filebox locks clear --data=./data --ip=1.2.3.4
filebox locks clear --data=./data --user=2
filebox locks clear --data=./data --all
```

生产构建：`make build` 后使用 `make start`；Windows 可运行 `bin/filebox.exe`，Linux 使用 `make build-linux`。生产环境必须设置强随机 `FILEBOX_JWT_SECRET`，并通过 HTTPS 反向代理。

## 服务日志

服务日志默认关闭，启用后同时输出控制台和 `filebox-YYYY-MM-DD.log`。跨天写入时前一天文件会 gzip 为 `.log.gz`，超出保留天数的日志和归档会自动清理。服务日志不记录密码、token 或文件内容；事件行包含操作者和来源 IP。

```bash
filebox --log-enabled=true --log-dir=/var/log/filebox --log-retention-days=90
```

## 配置项

命令行 flag 从同名 `FILEBOX_*` 环境变量读取默认值；命令行显式传入的值优先。

| Flag | 环境变量 | 默认值 | 含义 |
|---|---|---:|---|
| `--addr` | `FILEBOX_ADDR` | `:8080` | HTTP 监听地址 |
| `--data` | `FILEBOX_DATA` | `./data` | SQLite、文件、临时内容和品牌资源根目录 |
| `--max-file-size` | `FILEBOX_MAX_FILE_SIZE` | `107374182400`（100 GiB） | 后端单文件大小上限 |
| `--min-free-space` | `FILEBOX_MIN_FREE_SPACE` | `2147483648`（2 GiB） | 上传初始化要求的最小可用空间；`0` 关闭保护 |
| `--jwt-secret` | `FILEBOX_JWT_SECRET` | `filebox-development-secret-change-me` | JWT HS256 签名密钥；生产必须替换 |
| `--register-enabled` | `FILEBOX_REGISTER_ENABLED` | `false` | 注册开关首次部署种子；此后以管理后台 `registerEnabled` 设置为准 |
| `--admin-user` | `FILEBOX_ADMIN_USER` | `admin` | 首次创建的管理员用户名 |
| `--admin-pass` | `FILEBOX_ADMIN_PASS` | `admin123` | 首次创建的管理员密码；首次登录必须修改 |
| `--trusted-proxies` | `FILEBOX_TRUSTED_PROXIES` | 空 | 可信代理 IP/CIDR 列表；为空时忽略 X-Forwarded-For |
| `--log-enabled` | `FILEBOX_LOG_ENABLED` | `false` | 是否启用服务文件日志；启用时仍输出控制台 |
| `--log-dir` | `FILEBOX_LOG_DIR` | `<程序执行目录>/logs` | 服务日志目录 |
| `--log-retention-days` | `FILEBOX_LOG_RETENTION_DAYS` | `90` | 服务日志和 gzip 归档保留天数 |

后端上限为 100 GiB；阶段二前端对大文件采用 4 MiB 分片上传，单文件仍受后端上限约束。管理员策略默认值为：日志留存 30 天、连续失败 5 次锁定、自动解锁开启、5 分钟后解锁、上传限速 `0`（不限）、注册开关关闭。

## 服务化部署

systemd、Windows NSSM/sc 和 Nginx 反向代理的完整示例与安全说明见 [`deploy/README.md`](deploy/README.md) 和 [`deploy/README.en.md`](deploy/README.en.md)。生产部署应使用独立运行用户、专用数据/日志目录、强随机 JWT secret、HTTPS，以及与实际代理网段匹配的 `--trusted-proxies`。

## API 摘要

JSON 响应格式为 `{ "code": number, "message": string, "data": any }`；受保护接口需要 `Authorization: Bearer <token>`。主要路径包括：认证 `/api/auth/login`、`/api/auth/register`、`/api/auth/totp`、`/api/auth/totp-qrcode`、`/api/auth/change-password`、`/api/auth/logout`、`/api/auth/me`、`/api/auth/language`；公开品牌 `/api/brand` 和 `/brand/{asset}`；文件 `/api/files`、`/api/files/upload-init`、`/api/files/check`、`/api/files/{taskID}/chunks/{index}`、`/api/files/{taskID}/status`、`/api/files/{taskID}/complete`、`/api/files/{id}/download`、`/api/files/{id}/preview`、`/api/files/{id}/share`、`/api/files/{id}/shares`、`/api/files/shared/{token}/meta`、`/api/files/shared/{token}/download`、`/api/files/{id}`；日志 `/api/logs`、`/api/logs/actions`；管理员 `/api/admin/users[/{id}]`、`/api/admin/users/{id}/totp`、`/api/admin/users/{id}/ip-acl`、`/api/admin/stats`、`/api/admin/settings`、`/api/admin/brand`、`/api/admin/locks`。

文件列表、用户列表和日志列表默认 `pageSize=20`，服务端最大 100。管理员可查看全部文件和日志，普通用户按账户隔离。下载支持 Range；登出不建立 JWT 服务端黑名单。

## 目录结构

`cmd/filebox/` 是 Go 服务入口；`internal/httpapi/` 提供路由、认证、传输、下载、品牌和审计；`internal/store/` 提供 SQLite schema 与持久化；`internal/diskusage/` 提供 Windows/Linux 磁盘统计；`internal/webassets/` 提供 embed 和内置资源；`scripts/sync-web.go` 同步前端产物；`web/src/` 是 Vue 前端；`docs/requirements/` 保存需求状态和变更历史；`data/` 是默认运行时数据目录。

## 数据存储说明

`--data` 下包含 `filebox.db`；默认配置对应 `data/files/<userID>/<yy>/<mm>[/<相对目录>]/<stored-name>`、`data/tmp/<taskID>/<分片索引>` 和 `data/brand/<favicon|login-logo|main-logo>.<ext>`。SQLite 保存元数据、所有权、配额、审计日志、分片记录（`chunks`）和分享记录（`shares`）。上传分片先写入临时目录，`complete` 校验齐备后流式合并到用户/年份/月份目录（相对目录可保留文件夹结构）。用户看到的原始名保存为 `name`，落盘名保存为 `stored_name`；冲突时使用 `name (1).ext`、`name (2).ext` 等最小可用后缀。删除先标记 `deleted`、扣减配额，再清理物理内容；被删除记录占用的存储路径可在重传时自动复用。

## 已知限制

- 秒传仅在同一用户范围内匹配（跨用户不做内容去重）。
- 分享 token 随机生成；`meta` 接口公开文件名/大小/次数等元数据，请勿分享含敏感信息的文件。
- 限速按用户令牌桶生效于分片写入；速率变更在下一请求生效。
- 未实现的优化项：废弃上传任务的定时清理、服务端主动推送上传进度。

## 许可与致谢

当前仓库未附加项目级 `LICENSE` 文件。第三方依赖按各自许可证发布，详见 Go module 和 npm 包的上游说明。需求状态和变更历史见 [`docs/requirements/STATE.md`](docs/requirements/STATE.md) 与 [`docs/requirements/CHANGELOG.md`](docs/requirements/CHANGELOG.md)。
