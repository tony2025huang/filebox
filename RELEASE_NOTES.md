# FileBox v0.1.0

阶段一发布说明

[English](RELEASE_NOTES.en.md)

## 版本概览

`v0.1.0` 是 FileBox 阶段一版本，面向 Windows/Linux 单二进制部署。

## 本次交付

### MVP

- JWT 登录、登出和当前用户查询；密码 bcrypt 哈希；`admin`/`user` 角色和多用户文件隔离。
- 单文件单分片上传、分页搜索、Range 下载（`206 Partial Content`）和删除。
- 上传完成时计算 MD5 与 SHA-256；SQLite 持久化元数据。
- 用户配额、管理员用户 CRUD 和存储统计。

### R-DISK

- 提供 Windows/Linux 磁盘容量、已用、可用和占用率统计。
- `--min-free-space` 默认 2 GiB；空间低于阈值时上传初始化返回 `DISK_FULL`。

### R-NAME / R-CONFLICT

- 原始落盘文件名保存于用户/年份/月份目录，并保证存储路径唯一。
- 同名初始化返回 `409`；支持事务性覆盖或分配最小可用数字后缀重命名。

### R-VALID

- 上传前拒绝路径分隔符、控制字符、Windows 非法字符、遍历标记、空名/点名和超过 255 字节的文件名。

### R-LOG / R-LOCK

- 记录登录、上传完成和下载结果，支持用户隔离、管理员筛选、分页和惰性留存清理。
- 支持失败阈值、临时/永久锁定、自动解锁、统一登录失败提示和管理员重置。

### R-BRAND

- 支持网站标题、SEO 描述、favicon、登录页/主页 logo、ICP 和公安备案文本。
- 自定义资源限制 512 KiB，执行扩展名/内容校验、原子保存和内置 SVG 回退；空备案文本不渲染空白页脚。

## 安全提示

首次启动默认管理员为 `admin/admin123`，首次登录后请立即修改密码或禁用账户。公网部署必须设置强随机 `--jwt-secret` 或 `FILEBOX_JWT_SECRET`，并使用 HTTPS 反向代理。默认 JWT 有效期为 7 天；登出不会建立服务端 JWT 黑名单。

## 部署与运行

开发模式依次运行：

```powershell
npm --prefix web install
npm --prefix web run build
go run ./scripts/sync-web.go
go run ./cmd/filebox
```

生产构建使用 `make build`，运行使用 `make start`；Windows 可运行 `bin/filebox.exe`，Linux 使用 `make build-linux`。默认数据根目录为 `./data`，默认监听地址为 `:8080`。

## 已知限制与阶段二路线图

阶段一不包含分片断点续传、真正的多分片并发、秒传、文件夹上传、分享链接、在线预览、限速、开放注册和废弃上传任务定时清理。阶段二路线图将围绕这些传输与协作能力展开；`--register-enabled` 当前没有对应公开注册路由。

## 变更历史

详细需求变更见 [`docs/requirements/CHANGELOG.md`](docs/requirements/CHANGELOG.md)。
