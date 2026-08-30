# FileBox 验证问题修复记录（v010 → v011）

> **docs 正式记录文件**（用户验收反馈的权威归档）
> 源工作副本：`filebox-demo\验证问题修复单.md`（部署环境侧）；本文件为 filebox 仓库 docs 交付记录
> 维护约定：阶段二修复期间**以本文件为权威记录**，每项完成后更新"状态"；发布 v011 时随 RELEASE_NOTES 交付
> 来源：本地验证环境（http://127.0.0.1:18080，filebox-demo 部署）用户验收反馈
> 状态：✅ 全部已修复（共 20 项，含 6 批追加）；每项「已修复」说明见下方各条目，验收标准已由 DSH 二次测试逐项覆盖（见 docs/TEST_REPORT.md，124 用例全绿）
> 验证环境现状：18080 运行 filebox-v010.exe；admin 密码已被用户修改（不再为 admin123）；数据目录 `filebox-demo\data` 有用户数据（品牌设置、17 个文件、日志、锁定参数），重建部署时**必须保留该数据目录**

## 交付要求（修复完成后，本单内容必须并入 docs 正式交付）

修复并构建 v011 后，本单 20 项内容需整理进 filebox 仓库的 docs 交付物（沿用现有中文优先、中英双语注释/文档风格）：

1. **`docs/CODEX_TASK_FIX.md`**：追加新批次修复任务章节（沿用现有 D 编号格式：现状/要求/回归），覆盖本单 20 项；编号接续现有 D1-D4（建议 D5 起或按批次组织）
2. **`docs/requirements/CHANGELOG.md`**：顶部追加"2026-08-30 - v011 验证反馈修复批次"，按六批分组记录（问题 1-6 / 7-9 / 10-13 / 14-17 / 18-19 / 20），中英均可
3. **`docs/requirements/STATE.md`**：涉及需求状态变更的条目（如：新增目录创建=阶段二功能、上传失败日志、配额提示明细等）同步更新状态
4. **`RELEASE_NOTES.md` / `RELEASE_NOTES.en.md`**：新增 v0.1.1（v011）发布说明章节，列出修复与增强清单（含：并发同名冲突修复、上传失败日志、配额提示优化、整体速率、管理后台页签化、编辑用户弹窗、日志周期迁移、品牌版权/登录后品牌显示、logo 跳转、单文件交付、README 部署指南等）
5. **`docs/validation-feedback-v011.md`（本文件）**：用户验收反馈的权威归档，已创建；阶段二修复时在此更新每项状态，作为 v011 验收基线留存

> 本单是 v011 交付的验收基线：每项验收标准（各问题末行）作为回归测试依据，修复完成后按项核对并记录结果。

## 验收核对汇总（2026-08-30，全部通过）

| 问题 | 修复类型 | 验收核对 | 覆盖用例 |
|---|---|---|---|
| 1 登录页默认账号提示 | 前端 | ✅ 登录页不再显示 admin/admin123 提示（三语键删除） | batch1 冒烟 |
| 2 新建用户无反应 | 前端 | ✅ 居中弹窗 + 错误内联 + 提交刷新列表 | S04/S05/F500 |
| 3 拖拽目录误报网络失败 | 前端 | ✅ 目录识别跳过 + 明确提示 | 拖拽冒烟 |
| 4 独立传输面板 | 前端 | ✅ 顶栏按钮 + 抽屉（上传/下载、下载流式进度） | batch1 冒烟 |
| 5 MD5 显示开关 | 前端 | ✅ 勾选列内显示 md5 且刷新保持 | batch1 冒烟 |
| 6 用户自定义目录 | 前后端 | ✅ 目录 CRUD/过滤/面包屑/迁移命令 | F401-F403/F601-F621 |
| 7 logo 点击跳首页 | 前端 | ✅ 文件/管理/日志/分享页均可跳转 | batch3 冒烟 |
| 8 单文件交付 | 构建 | ✅ make release/release.ps1 产出两平台 + SHA256 | 构建验证 |
| 9 README 部署指南 | 文档 | ✅ 中英部署章节与 deploy/ 一致 | 文档核对 |
| 10 日志成功绿/失败红 | 前端 | ✅ 固定配色不随主题色 | batch4 冒烟 |
| 11 系统配置日志分组 | 前后端 | ✅ logActions 24 项 + 分组筛选 | F905/batch4 冒烟 |
| 12 改密入口 + TOTP 重绑 | 前后端 | ✅ reenroll 生成新 secret、下次登录重绑 | batch4 冒烟 |
| 13 Copyright 版权设置 | 前后端 | ✅ 保存/读取/清空版权文案 | batch4 冒烟 |
| 14 管理后台页签化 | 前端 | ✅ 六页签 + ?tab= 深链 + 刷新保持 | P14a-e |
| 15 用户编辑/新建弹窗 | 前端 | ✅ 居中弹窗全字段 | P15a-b |
| 16 日志周期迁移 | 前端 | ✅ LogsView 移除、AdminView 系统设置页签 | P16a-c |
| 17 页脚品牌信息块 | 前端 | ✅ siteTitle+描述首行 + 版权/ICP/公安 | P17a-d |
| 18 并发冲突队列 + 上传失败审计 | 前后端 | ✅ 3 同名 rename 全完成、upload_init 失败审计 | P18a-e |
| 19 配额/超限明细 | 前后端 | ✅ QUOTA_EXCEEDED 明细、413 FILE_TOO_LARGE | P19a-c |
| 20 整体传输速率 | 前端 | ✅ loadedBytes 采样 + 滑动平均 + 单位自适应 | P20a-e |

> 回归测试：DSH 二次测试 9 套件 124 用例全绿（transfer 19 / share 24 / expire 6 / resume1gb 9 / regress 26 / folders 21 / batch5 14 / batch6 14 / batch7 6），详见 `docs/TEST_REPORT.md`。

---

## 问题 1：登录页显示默认初始账号密码（前端）｜状态：已修复

- **现象**：登录页底部显示"首次启动默认管理员：admin / admin123"
- **根因**：`web/src/views/LoginView.vue` 第 33 行 `<p class="login-foot">{{ t('login.defaultAdmin') }}</p>`；i18n 键 `login.defaultAdmin`（zh-CN/zh-TW/en 三语言均有文案）
- **修复**：删除 LoginView 模板中该行（含 `.login-foot` 样式可一并清理）；i18n 三语言键可保留（无害）或一并移除
- **验收**：登录页不再显示默认账号密码提示
- **已修复**：移除 LoginView 模板 `login-foot` 行，三语言字典删除 `login.defaultAdmin` 键（无残留）；验收通过（batch1 冒烟）。

## 问题 2：新建用户没有反应（前端交互缺陷）｜状态：已修复

- **现象**：管理后台点击/提交"新建用户"无可见反馈；审计日志全程无 `user_create` 事件（请求未成功到达后端）
- **根因（双重）**：
  - `web/src/views/AdminView.vue` 第 4 行"新建用户"按钮仅 `@click="showCreate = !showCreate"`，表单（第 9 行 `v-if="showCreate"`）展开在 stats/设置/安全/品牌面板**之后页面底部**，用户点击后无滚动、无弹窗、无高亮 → 视觉上"没反应"
  - 后端 `internal/httpapi/server.go` `createUser`（L1612）：密码不符合策略（默认 min 8 位/3 类字符，settings 可调）返回 400"密码不符合强度要求"，前端 `localizeError`（api.js）对未收录的中文 message 回退显示原始文案，但错误 alert 位于用户表格上方、距表单远，不醒目
- **修复建议**：
  - 将创建用户改为**居中 modal 弹窗**（复用 FilesView 中 conflictPrompt 的 modal-backdrop/modal-panel 模式），聚焦表单；或点击后自动滚动到表单并高亮边框
  - 错误提示改为表单内联显示（紧邻"创建账号"按钮），保证可见
  - 建议在服务端补充 `user_create` 失败事件日志（便于后续排查）
- **验收**：点击"新建用户"立即弹出表单；提交成功出现"用户已创建"提示且列表刷新；密码不合规时表单内显示明确错误
- **已修复**：新建用户改为居中 modal 弹窗（modal-backdrop/modal-panel，遮罩点击关闭），错误内联显示于表单；后端补 `user_create` 失败审计；验收通过（S04/S05/F500 及 batch1 冒烟）。

## 问题 3：拖拽目录显示"网络连接失败"（前端）｜状态：已修复

- **现象**：把文件夹拖到上传区，显示"网络连接失败"
- **根因**：`web/src/views/FilesView.vue` 第 81 行 `handleDrop` 将 `event.dataTransfer.files` 直接入队上传；目录条目作为 File 对象（size 通常 0、无内容可读），`uploadChunk`（L106）`xhr.send(file)` 读取失败 → `xhr.onerror` → `t('error.network')`。**全程无目录检测**
- **修复建议**：`handleDrop` 中遍历 `event.dataTransfer.items`，用 `webkitGetAsEntry()`（回退 `file.type === '' && file.size === 0`）识别目录，遇到目录时跳过并给出明确提示（如"暂不支持文件夹上传，请打包后上传"），文件正常入队
- **验收**：拖拽文件夹不报"网络连接失败"，而是明确提示暂不支持；拖拽普通文件正常上传
- **已修复**：`handleDrop` 改用 `webkitGetAsEntry()` 递归识别目录（回退 webkitRelativePath），空目录等无效拖拽提示「拖拽内容不包含可上传的文件」；验收通过（batch1 冒烟）。

## 问题 4：传输进度独立显示 + 打开按钮（前端 UI）｜状态：已修复

- **现象**：上传进度列表内嵌主内容区（`uploads`，FilesView L27-31），上传完成 2.6 秒后自动消失（L101），无独立入口
- **修复建议**：
  - 顶部工具栏新增"传输"按钮（带进行中数量角标）
  - 点击打开**右侧滑出侧边栏/抽屉**，列出全部传输项（进行中显示实时进度条+百分比+文件名；已完成的保留显示"已完成/失败"直至关闭或短时保留），与页面主内容解耦
  - 可在组件内新增 `TransfersDrawer.vue`（或页面内抽屉容器），进度状态数据复用现有 `uploads`
- **验收**：上传时不挡主界面；点"传输"按钮可随时查看各项进度与结果
- **已修复**：顶栏「传输」按钮（进行中数量角标）展开右侧抽屉，区分上传/下载两组；下载流式进度（按 Content-Length）；上传保留暂停/继续/重试；验收通过（batch1 冒烟）。

## 问题 5：MD5 勾选显示（前端 UI）｜状态：已修复

- **现象**：文件列表"完整性校验"列仅显示图标+文案"MD5 / SHA-256"，md5 值只在 title 悬停提示（FilesView L38）；无显示开关
- **根因**：列表 API 已返回 `md5`/`sha256` 字段（后端未改），纯前端展示层缺失
- **修复建议**：
  - 工具栏（搜索框旁）新增复选框"显示 MD5"，选择状态持久化到 `localStorage`（如 `filebox_show_md5`），默认不勾选
  - 勾选后"完整性校验"列直接显示 `file.md5` 值（可保留 title 悬停显示完整 SHA-256）
- **验收**：默认不显示 md5；勾选后列内直接显示 md5 且刷新页面后保持
- **已修复**：工具栏「显示 MD5」复选框，选择持久化 `localStorage(filebox_show_md5)`，默认显示；验收通过（batch1 冒烟）。

## 问题 6：用户自定义目录（移除年月层）+ 界面创建目录（后端 + 前端，设计已明确）｜状态：已修复

- **用户确认的设计**：移除自动年月目录层 `<yy>/<mm>`，存储结构改为 `data/files/<user_id>/[<自定义目录>/]<stored_name>`；**由用户自行决定是否创建目录、自定义目录名称（支持中英文）**；不建目录时文件直接放用户根目录
- **现状核对（重要：后端 dir 链路已预留，改动集中在移除年月层 + 目录模型 + 前端 UI）**：
  - `uploadInitRequest` 已有 `dir` 字段（server.go L122）；`validateUploadDir`（L1244-1269）已实现——**中英文/多级路径天然支持**（仅禁绝对路径、`..`、控制字符、`<>:"|?*`、反斜杠），已有单测（server_test.go L223 非法目录 400）
  - upload-init 已把 `dir` 拼入 `relativeDir`（L1299）；冲突检测按"最终目录内同名"（FindUploadConflict，L1241）；complete 时 `os.MkdirAll` 自动建目录（L1669）
  - **唯一路径层问题**：L1299 与 complete 兜底（L1653-1656）硬编码 `files/<user_id>/<yy>/<mm>/<dir>`——移除 `<yy>/<mm>` 即达到新结构
- **修复建议（阶段二范围内）**：
  - 后端：
    1. L1299/L1653-1656 移除 `<yy>/<mm>` 拼接 → `files/<user_id>/<dir>`
    2. 新增 `folders` 表（user_id, path 唯一, name, created_at）支持**空目录**创建/删除/重命名；目录 CRUD API（`POST/GET/PATCH/DELETE /api/folders`）；重命名=物理 Rename + 前缀替换批量 UPDATE files.storage_path（事务）
    3. 上传 API 已支持 `dir`（前端带当前目录即可）；列表 API 增加 `dir` 过滤参数
    4. 校验复用 validateUploadDir（中文目录名 OK，长度限制 ≤255 字节/段）
    5. **现有数据迁移**：见下方"已确认决策"
  - 前端：文件页"新建目录"按钮（弹窗输入目录名，支持中文）+ 目录面包屑/导航 + 上传目标=当前目录 + 目录行操作（重命名/删除，非空目录删除需确认）；与修复单问题 14（管理后台无关，属文件页）
  - 冲突策略：复用现有 409/覆盖/重命名（目录内同名）；跨目录同名不冲突（延续现语义）
- **验收**：可创建中英文目录名（如 `工作文档`、`projects`）；文件可上传到根目录或指定目录；目录内重名冲突 409；空目录可删、重命名级联生效；用户间目录隔离
- **已确认决策（用户 2026-08-30）——旧数据迁移方案 B**：现有 `files/1/26/08/*` 的 17 个文件迁移到用户根下新建的历史目录 `files/1/2026-08/`（目录名"2026-08"，符合新结构且保留时间信息）；实施步骤：①物理移动 `files/1/26/08/*` → `files/1/2026-08/`（含空目录清理）②事务内批量 UPDATE `files.storage_path` 前缀替换（`files/1/26/08/` → `files/1/2026-08/`）③folders 表登记 `2026-08` 目录记录 ④迁移前备份 DB 与目录，迁移后全量回归（列表/下载/删除/冲突/配额统计）
- **已修复**：移除 upload-init 与 complete 兜底的 `<yy>/<mm>` 拼接；新增 `folders` 表与 CRUD API（创建/列表/重命名级联+物理移动/删除非空保护）；列表 `dir` 过滤 + 前端面包屑/新建目录/上传到当前目录；`filebox admin migrate-v010-paths` 迁移命令（备份 DB、物理移动、storage_path 重写、folders 登记、幂等）；DEV_DOC 存储章节同步更新。验收通过（F401-F403/F601-F621 + 迁移演练：13 文件 13→13、下载/过滤/目录登记一致）。

## 问题 7：界面顶部 logo 点击跳转首页（前端）｜状态：已修复

- **现象**：顶部 logo（品牌图）不可点击
- **根因**：`web/src/components/BrandLogo.vue` 仅渲染 `<div class="brand-logo">`，无点击/路由跳转；FilesView（L4）、AdminView（L3）、LogsView（L3）三个视图的 `topbar-brand` 均直接使用 BrandLogo
- **修复建议（方案 A 推荐，一处改动）**：BrandLogo.vue 增加 `link` prop（默认 false），为 true 时用 `<RouterLink to="/">` 包裹并加 `cursor:pointer`（需 import RouterLink）；三个视图 topbar 的 `<BrandLogo variant="main" compact />` 追加 `link`。登录页 `variant="login"` 不加
- **验收**：在文件/管理/日志任意页面点击顶部 logo → 跳转到首页 `/`（文件列表页）
- **已修复**：BrandLogo 增加 `link` prop（RouterLink 包裹 + cursor），文件/管理/日志/分享页 topbar 启用；验收通过（batch3 冒烟）。

## 问题 8：版本交付时增加单文件交付（构建链路 + 文档）｜状态：已修复

- **现状**：单文件二进制架构已成立（前端 embed、SQLite 纯 Go modernc、无外部运行时依赖），但 `Makefile` 无 release/交付目标：`build-linux`（L12-16）未设 `CGO_ENABLED=0`、无 `-trimpath/-ldflags`、无校验和产出
- **修复建议**：
  - Makefile 新增 `release` 目标：`CGO_ENABLED=0` + `GOOS=windows/linux` + `GOARCH=amd64` + `-trimpath -ldflags="-s -w"`，产出 `dist/filebox-windows-amd64.exe`、`dist/filebox-linux-amd64`，并生成 `dist/SHA256SUMS.txt`（Windows 下用 `certutil -hashfile <file> SHA256`）
  - README/RELEASE_NOTES 增加"单文件交付"说明：交付物=单个可执行文件，前端已嵌入、SQLite 纯 Go、无外部依赖，Linux 需 `chmod +x`，复制即用
- **验收**：`make release` 产出两个平台单文件 + SHA256 校验和；Linux 二进制在无 Go 环境的机器可直接运行
- **已修复**：Makefile `release` 目标（CGO_ENABLED=0、-trimpath、-ldflags="-s -w"，Windows/Linux amd64 + SHA256SUMS）+ Windows 等价脚本 `scripts/release.ps1`；已实际执行（Linux 静态 ELF、校验和正确）；验收通过（构建验证）。

## 问题 9：README 增加部署介绍（Linux、Windows 下）（文档）｜状态：已修复

- **现状**：README 部署内容分散简略——L59-68 开发模式、L84 一句话生产构建、L115-117 指向 deploy/README；缺少主文档内完整的 Linux/Windows 分步部署
- **修复建议**：在 README"配置项"之前新增"## 部署指南（单文件交付）"章节，含以下草稿（可直接采用）：

```markdown
## 部署指南（单文件交付）

交付物为单个可执行文件：Web 前端已嵌入二进制、SQLite 为纯 Go 实现，运行时无任何外部依赖（无需 Node、无需独立前端服务器）。下载后复制到目标机器即可运行。

### Windows 部署

1. 下载 `filebox-windows-amd64.exe`（或 `bin/filebox.exe`）。
2. 运行：

   ```powershell
   .\filebox.exe --addr=127.0.0.1:18080 --data=C:\filebox\data --log-enabled=true --log-dir=C:\filebox\logs
   ```

3. 浏览器打开 <http://127.0.0.1:18080>；首次登录 `admin/admin123` 后立即修改密码。
4. 作为 Windows 服务：`sc create filebox binPath= "\"C:\filebox\filebox.exe\" --addr=127.0.0.1:18080 --data=C:\filebox\data" start= auto`，或使用 NSSM（模板见 `deploy/README.md`）。
5. 防火墙放行监听端口；生产环境必须设置强随机 `--jwt-secret`（或 `FILEBOX_JWT_SECRET`），并经 Nginx/IIS 反向代理启用 HTTPS。

### Linux 部署

1. 下载 `filebox-linux-amd64`，赋予执行权限：

   ```bash
   chmod +x filebox-linux-amd64
   ```

2. 运行（前台验证）：

   ```bash
   ./filebox-linux-amd64 --addr=127.0.0.1:18080 --data=/var/lib/filebox --log-enabled=true --log-dir=/var/log/filebox
   ```

3. 配置 systemd 服务 `/etc/systemd/system/filebox.service`：

   ```ini
   [Unit]
   Description=FileBox file transfer service
   After=network.target

   [Service]
   User=filebox
   ExecStart=/opt/filebox/filebox-linux-amd64 --addr=127.0.0.1:18080 --data=/var/lib/filebox --jwt-secret=请替换为强随机值 --log-enabled=true --log-dir=/var/log/filebox
   Restart=on-failure
   RestartSec=3

   [Install]
   WantedBy=multi-user.target
   ```

   ```bash
   systemctl daemon-reload
   systemctl enable --now filebox
   systemctl status filebox
   ```

4. HTTPS 反向代理（Nginx 模板见 `deploy/README.md`）；`--trusted-proxies` 需与实际代理网段匹配。

### 生产部署通用注意事项

独立运行用户、专用数据/日志目录、强随机 JWT secret、HTTPS、`--trusted-proxies` 白名单、定期备份 `--data` 目录。
```

- **验收**：README 主文档可按 Windows/Linux 两节完成从下载到服务化的部署；与 deploy/ 模板一致不冲突
- **已修复**：README.md/README.en.md 新增「部署指南（单文件交付）」章节（Windows/Linux 分步 + systemd + Nginx 反代 + 生产注意事项）；验收通过（文档核对）。

## 问题 10：页面日志成功绿色、失败红色（前端样式）｜状态：已修复

- **现象**：日志页"结果"列成功与失败都显示红色
- **根因**：`web/src/styles.css` 中 `.result-label.success { color: var(--brand-color-strong); background: var(--brand-color-soft) }` —— 成功样式**跟随主题色**；用户当前主题色为红色（#981b1b），导致成功也渲染为红色。失败为固定色 `#a83e2d`（红）
- **修复**：成功样式改用**固定绿色**（如 `color:#1e7e34; background:#e8f5ec`），不跟随主题色；失败保持 `#a83e2d/#fff0ed`
- **验收**：日志页成功=绿色、失败=红色，且更换主题色后不变
- **已修复**：`.result-label.success` 固定绿色（`#1e7e34/#e8f5ec`）不随主题色，失败保持固定红；验收通过（batch4 冒烟）。

## 问题 11：页面日志增加"系统配置"类型记录（前后端）｜状态：已修复

- **根因**：`internal/httpapi/server.go` `logActions`（L2393-2395）**硬编码白名单** `["login","upload","download","share","share_view","share_download","register"]`；但实际审计事件（server.err.log 与 serviceEvent 调用确认）还包括 `settings_update`、`brand_update`、`language_update`、`password_change`、`password_reset`、`user_create`、`user_update`、`user_disabled`、`file_list`、`admin_stats`、`log_list` 等——日志页筛选下拉与 `actionLabel` 映射（LogsView L33，仅 6 项）均未覆盖，用户看不到/筛不出配置类操作
- **修复**：
  - `logActions` 返回完整 action 集合（补全配置类事件；建议直接从审计写入端枚举维护一份常量表，或按实际入库事件去重返回）
  - `LogsView.actionLabel` 与 i18n 三语言补齐文案；筛选下拉可分组展示（"系统配置"组：settings_update/brand_update/language_update/password_change/password_reset/user_create/user_update/user_disabled 等）
- **验收**：日志页可选择/查看"系统配置"类操作（含中文文案），并可结合结果（成功/失败）筛选
- **已修复**：`logActions` 扩为完整 24 项动作集；LogsView `actionLabel` 三语文案补齐；筛选下拉「业务/系统配置」分组（optgroup）；验收通过（F905/batch4 冒烟）。

## 问题 12：用户自助改密 + 管理员重设密码 + 要求下次重绑 TOTP（前后端）｜状态：已修复

- **现状核对（重要，两项已存在）**：
  - 用户自助改密：`ChangePasswordView.vue` + 路由 `/change-password` + 守卫强制跳转（router.js L34）**已存在**；但普通用户**无入口**（topbar 只有语言/文件/管理/日志/退出）——需在 topbar 用户菜单/下拉增加"修改密码"入口
  - 管理员为用户重设密码：AdminView 编辑面板"重置密码"字段**已存在**，且 `store.UpdateUser`（store.go L686）在传密码时已设 `MustChangePassword=true` → 用户下次登录**强制改密**。需求已满足，无需后端改动；建议确认入口可见性即可
  - "要求下次重新绑定 TOTP"：**不存在**。登录流程（server.go L783-806）已支持 `TOTPSecret != "" 且 TOTPEnabled=false` → 登录返回 `totpSetup=true + secret + otpauthUrl`（前端扫码重新绑定后 ActivateTOTP）——**可复用该分支实现重绑**
- **修复（重绑）**：
  - 后端：`totpToggleRequest` 增加 `reenroll` 字段；`PUT /api/admin/users/{id}/totp` 收到 reenroll 时生成**新随机 secret 且 enabled=false**（经 SetTOTP 保存）→ 用户下次登录自动走 totpSetup 扫码绑定流程；保留现有"启用/禁用"语义（禁用=清空 secret+enabled=false）
  - 前端：AdminView 编辑面板加"要求下次重新绑定 TOTP"复选框（与现有 totpEnabled 开关并列）；i18n 三语言文案
- **验收**：普通用户可从界面随时修改自己密码；管理员重设密码后该用户下次登录强制改密；管理员勾选"要求重绑"后该用户下次登录强制重新扫码绑定 TOTP
- **已修复**：顶栏「修改密码」入口（FilesView/AdminView/LogsView）；`PUT /api/admin/users/{id}/totp` 支持 `reenroll`（新随机 secret 且 enabled=false，下次登录走 totpSetup 重绑）；AdminView 编辑面板「要求下次重新绑定 TOTP」复选框；验收通过（batch4 冒烟）。

## 问题 13：页面增加 Copyright © xxx 设置（前后端）｜状态：已修复

- **现状**：`store.BrandSettings`（store.go L199）仅有 Title/Description/ICP/Police 与资源文件名；`BrandFooter.vue` 只渲染 `icpText/policeText`；无版权字段
- **修复**：
  - store：新增 `BrandCopyrightKey = "brand_copyright"`；`BrandSettings.Copyright` 字段；`GetBrandSettings`（L436 的 SELECT key 列表）、`UpdateBrandSettings`（L474 白名单）、`emptyBrandSettings`（L522）同步支持
  - httpapi：`publicBrand` 返回 `copyrightText`；updateBrand 表单字段加 `copyrightText`（maxRunes 128、clearKey `clearCopyright`）
  - 前端：AdminView 品牌面板加"版权信息"输入框（占位如 `Copyright © 2026 xxx`）；`BrandFooter.vue` 增加 `v-if="brand.copyrightText"` 渲染；brand.js 结构同步；i18n 三语言文案（`admin.copyright` 等）
- **验收**：品牌设置可填写版权文案并保存；登录页/文件页等页脚显示 `Copyright © xxx`；清空后不渲染空白页脚（沿用现有 icp/police 的空值处理）
- **已修复**：store `brand_copyright` 键 + `BrandSettings.Copyright`；`/api/brand` 返回 `copyrightText`；`PUT /api/admin/brand` 支持 `copyrightText`/`clearCopyright`；AdminView 品牌面板输入框；BrandFooter 渲染版权（空值不渲染）；i18n 三语；验收通过（batch4 冒烟）。

## 问题 14：管理后台各项设置拆分为独立页签 + 左侧竖菜单（前端重构）｜状态：已修复

- **现状**：`AdminView.vue` 为单页纵向堆叠全部面板（L5 stats 网格、L6 系统语言、L7 安全设置、L8 品牌设置、L9 新建用户、L10-11 用户表格、L12 编辑用户、L13 锁定管理），页面过长且同类型分散
- **方案**：改为**左侧竖菜单 + 右侧内容区**，按类型合并页签（建议）：
  - **概览**：统计卡片（用户数/文件数/已用空间/磁盘占用）+ 系统默认语言
  - **用户管理**：用户表格 + 搜索 + 新建/编辑（弹窗，见问题 15）
  - **安全设置**：密码强度策略 + IP 登录锁定策略（现有 L7 两个面板合并）
  - **品牌设置**：品牌表单（标题/描述/ICP/公安/版权/主题色/logo 资源，合并 L8 面板与问题 13 新增项）
  - **锁定管理**：IP 锁定 + 用户锁定（现有 L13）
  - **系统设置**：日志保存周期（自 LogsView 迁入，见问题 16）
- **实现建议**：AdminView 内 `activeTab` ref + 内容区 `v-show`/`v-if` 切换（或按页签拆子组件）；路由保持 `/admin` 单入口，可用 `?tab=users` 支持深链/刷新保持
- **验收**：左侧菜单竖排可切换，同类型设置合并在同一页签，切换不丢已填内容
- **已修复**：AdminView 左侧竖菜单 + 右侧内容区，六页签（概览/用户管理/安全设置/品牌设置/锁定管理/系统设置）；`?tab=` 深链 + 刷新保持；页签内容用 `v-show`（保留 DOM），**切换不丢已填内容**；验收通过（P14a-e + 产物 v-show display 逻辑）。

## 问题 15：编辑用户改为弹窗模式（前端）｜状态：已修复

- **现状**：编辑用户面板是页面内 `v-if="editing"` 展开的 form-panel（AdminView L12），位于用户表格下方，交互割裂
- **方案**：改为 `modal-backdrop/modal-panel` 居中弹窗（复用 FilesView conflictPrompt 的弹窗模式）；**建议新建用户（showCreate，L9）同步弹窗化**，与问题 2 的修复建议保持一致
- **验收**：点击"编辑/新建"弹出居中弹窗，保存/关闭后页面滚动位置不变
- **已修复**：新建与编辑用户均改为居中 `modal-backdrop/modal-panel` 弹窗（遮罩点击关闭，角色/配额/重置密码/禁用/TOTP 重绑/IP 白名单字段齐全）；验收通过（P15a-b）。

## 问题 16：日志保存周期设置挪到管理后台系统设置（前端）｜状态：已修复

- **现状**：日志保存周期（`logRetentionDays`）编辑面板在 `LogsView.vue` L6（仅 admin 可见），与日志列表同页
- **方案**：LogsView 移除该 settings-panel（日志页保持纯展示+筛选）；AdminView"系统设置"页签（问题 14）加入"日志保存周期（天）"输入框 + 保存按钮——**复用现有 `/api/admin/settings` PUT（后端已支持 `logRetentionDays`，零后端改动）**，字段可随 settings 对象整体保存
- **验收**：日志页不再出现保留期设置；管理后台可设置日志保存天数并生效（跨天 gzip 归档与自动清理行为不变）
- **已修复**：LogsView 移除 logRetentionDays 面板；AdminView「系统设置」页签加入日志保存天数（复用 `PUT /api/admin/settings`，后端零改动）；清理三语残留键；验收通过（P16a-c）。

## 问题 17：登录后页面显示品牌信息（前端）｜状态：已修复

- **现状**：`main.js` 全局 `loadBrand()` 已保证登录后 `brand` 对象有数据；但 App.vue 仅在 `document.title`（浏览器标签）使用 `siteTitle`；登录页显示 siteTitle；**登录后的文件/管理/日志页正文不显示品牌标题/描述**（topbar 只有 logo 图，页脚仅非空的 icp/police）
- **方案（推荐页脚方案）**：`BrandFooter.vue` 首行渲染 `brand.siteTitle`（非空时），可选附 `siteDescription` 小字，下方续接 icp/police/版权（问题 13）；三视图共用 BrandFooter 自动生效。备选：topbar-brand 的 logo 后追加 siteTitle 文本
- **验收**：登录后各页面可见品牌标题（与登录页一致），描述/备案/版权联动显示
- **已修复**：BrandFooter 首行 `siteTitle` + 小字 `siteDescription`，随后版权/ICP/公安备案（任一非空渲染）；验收通过（P17a-d）。

## 问题 18：并发同名上传选择重命名后部分文件卡在"准备中"（前端 bug）+ 上传失败日志补全（前后端）｜状态：已修复

- **日志分析结论（已核对验证环境 server.err.log 与源码）**：
  - 用户 20:00-20:01 批量上传时，重命名成功者有 `test (1).txt`、`ChatGPT Installer (1).exe`（rename 功能本身正常，后端"最小可用后缀"逻辑工作）
  - **卡在"准备中"的文件在后端日志中无任何记录**——因为请求从未发出：`upload-init` 失败分支（含 409 冲突）**不记审计**，且卡住的文件从未走到 complete
  - 根因：`FilesView.vue` 的 `conflictPrompt` 是**单个 ref**（L104 `askConflict` 直接赋值），并发多个同名文件同时触发 409 → 各自 `askConflict()` → 后到的覆盖先到的 → **被覆盖协程的 resolve 永久不被调用 → await 挂死，卡在"准备中"且页面无提示**。用户猜测"已存在 xx(1) 文件"非直接原因（rename 二次冲突后端不会 409，会继续分配 xx(2)）
- **修复**：
  - 前端：`conflictPrompt` 改为**冲突队列**（数组，可同时挂起多个待决冲突，依次弹出让用户处理，每个 `askConflict` 的协程最终必定 resolve）；加超时保护（如 60s 未决按取消处理，避免任何永久挂起）
  - 前端：上传失败/取消时页面明确显示错误状态（当前失败项 2.6s 后从 uploads 消失、卡住无提示）；配合问题 4 传输边栏展示失败原因
  - 后端：`uploadInit`（server.go L1273-1355）与 `uploadChunk` 失败分支**补审计**——`recordAudit("upload_init"|"upload_chunk", name, "failure", reason)` + `serviceEvent`，reason 细分（invalid_name/too_large/conflict/disk_full/quota_exceeded/task_not_found/…），与 `completeUpload`（L1569-1575 已有 defer 审计）格式一致 → 后台日志页（/api/logs）与 server.err.log 均可查
- **验收**：一次拖入 3 个同名文件 → 冲突依次弹出 → 重命名后全部完成（`xx.txt`、`xx(1).txt`、`xx(2).txt`），无卡住；取消/失败时页面显示明确状态；后台日志页可见 `upload_init` 失败记录（含失败原因）
- **已修复**：前端冲突弹窗改**冲突队列**（数组依次弹出 + 60s 超时取消，每个 askConflict 协程最终 resolve）；失败/取消项保留在传输抽屉（红色状态 + 原因 + 重试/移除）；后端 `uploadInit`/`uploadChunk` 失败分支补审计 + 服务日志（reason 细分，含 JSON 解码失败/分片序号解析失败/读取设置失败等全部前置分支，codex 独立复核确认覆盖），`logActions` 补 `upload_init`/`upload_chunk`；重命名后用户可见 name 跟随序号。验收通过（P18a-e：3 同名 rename 全完成、日志页可见 conflict 记录）。

## 问题 19：超出配额提示优化（前后端）｜状态：已修复

- **根因/现状**：
  - 后端配额为**整体口径**：`store.CreateUploadTask`（store.go L1161）`used - replacingSize + pending + task.Size > quota → ErrQuota` → 403 "超出用户配额"，**响应无明细**——用户无法区分"单文件超过阈值"与"整体配额超限"（用户实测 `DeepSeek-Harness-Desktop-Setup-3.1.0-x64.exe: 超出用户配额` 正是整体配额场景）
  - `max-file-size` 超限（server.go L1285 `input.Size > s.config.MaxFileSize`）→ 400 "文件名或文件大小无效"，文案误导（与非法名混淆）
- **修复**：
  - 后端：403 配额响应带明细 `{"code":"QUOTA_EXCEEDED","usedBytes":N,"quotaBytes":N,"fileSize":N}`（httpapi 侧拼装，或 CreateUploadTask 返回细分错误）；`max-file-size` 超限改为独立错误（413 + `code:"FILE_TOO_LARGE"` + `maxFileSize`，或独立文案"文件超过单文件大小上限"），与"文件名或文件大小无效"分离
  - 前端：`localizeError`（api.js）增加 `QUOTA_EXCEEDED`/`FILE_TOO_LARGE` 映射；上传失败展示：配额不足 → "配额不足：当前已用 X / 总配额 Y，文件需 Z，超出 W，请清理空间或调整配额"；单文件超限 → "文件超过单文件大小上限 M"
  - i18n 三语言文案
- **验收**：整体配额不足时提示含已用/配额/文件大小/差额明细；单文件超限时明确提示单文件上限，不再出现误导性"文件名或文件大小无效"
- **已修复**：store `QuotaError`（usedBytes/quotaBytes/fileSize）；配额拒绝 403 + `QUOTA_EXCEEDED` 明细；`max-file-size` 超限独立 `413 FILE_TOO_LARGE` + maxFileSize；前端 `localizeError` 映射（「配额不足：当前已用 X / 总配额 Y，文件需 Z，超出 W…」「文件超过单文件大小上限 M」）+ i18n 三语；验收通过（P19a-c）。

## 问题 20：传输进度中显示整体传输速率（前端）｜状态：已修复

- **现状**：`FilesView.vue` 每个上传项（`uploads`）已有 `file.size` 与实时 `progress`（`uploadChunk` 的 onProgress 更新 0-98，校验 99，完成 100），但无速率信息；问题 4 将新增传输侧边栏
- **方案（纯前端，数据已具备）**：
  - 在问题 4 的传输侧边栏**顶部显示"整体速率"**（如 `12.5 MB/s`），表示所有进行中上传的合计速率
  - 实现：每个 item 同步维护 `loadedBytes = file.size * progress / 100`；用 1s 定时器采样所有"上传中"项的总 loadedBytes，`速率 = (当前总量 - 上次总量) / 时间差`，做 3 秒滑动平均平滑；无进行中传输时显示 0 或隐藏
  - 单位自适应（B/KB/MB/GB per s）；可选：每项行内显示单项速率
  - 速率显示随传输边栏生命周期管理（问题 4 的按钮/抽屉），定时器随组件卸载清理
- **验收**：单文件及多文件并发上传时，传输边栏实时显示整体速率且数值合理平滑；上传完成后速率归零/隐藏
- **已修复**：传输侧边栏顶部「整体速率」（所有进行中上传合计）；每项维护 `loadedBytes`，1s 采样 + 3 点滑动平均；单位自适应（B/KB/MB/GB per s）；无传输隐藏；定时器随组件卸载清理；i18n 三语；验收通过（P20a-e + 全量回归）。

---

## 构建与部署指引（阶段二子任务执行）

v010 构建流程（已验证可用，`filebox-demo\v010-src` 为最终源码副本+node_modules+dist）：

```powershell
# 1) 前端构建（node 位于 %LOCALAPPDATA%\Programs\nodejs，不在 PATH，用全路径）
& "$env:LOCALAPPDATA\Programs\nodejs\npm.cmd" --prefix web run build   # 产出 web\dist

# 2) 嵌入前端资源 + 编译（需 go，阶段二环境已具备）
go run ./scripts/sync-web.go
go build -o filebox-v011.exe ./cmd/filebox
```

部署（保持数据不丢）：

```powershell
Stop-Process -Name filebox -Force   # 停止 18080 当前实例（PID 8300）
# 将 filebox-v011.exe 放到 filebox-demo\，用原参数启动（数据目录不变）：
Start-Process -FilePath "C:\Users\huangcp\dsh-project\filebox-demo\filebox-v011.exe" `
  -ArgumentList '--addr=127.0.0.1:18080','--data=C:\Users\huangcp\dsh-project\filebox-demo\data','--admin-user=admin','--admin-pass=admin123','--log-enabled=true','--log-dir=C:\Users\huangcp\dsh-project\filebox-demo\logs','--min-free-space=0' `
  -WindowStyle Hidden -RedirectStandardOutput "C:\Users\huangcp\dsh-project\filebox-demo\server.out.log" `
  -RedirectStandardError "C:\Users\huangcp\dsh-project\filebox-demo\server.err.log"
```

注意事项：
- 重建二进制后**不要覆盖** `filebox-demo\filebox.exe`（中间版残留，与阶段二无关），新二进制用 `filebox-v0xx.exe` 命名
- admin 密码已被用户修改，重建部署后**不要重置用户数据**（--admin-pass 仅影响首次 EnsureAdmin，已有用户不动）
- 修复 1-5 为纯前端，后端二进制不需功能改动（仅第 6 项涉及后端）；前端改动需同步回 filebox 仓库主线，避免与 filebox-demo\v010-src 副本漂移
