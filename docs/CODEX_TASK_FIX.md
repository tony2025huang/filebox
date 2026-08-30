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

---

# Codex 修复任务 — 阶段二 v011 验证反馈（D5–D24，共 20 项）

你在 `C:\Users\huangcp\dsh-project\filebox` 完成 FileBox 阶段二（v0.2.0）开发与 v011 验证反馈批次修复。本任务书覆盖验证问题修复单全部 20 项（来源：`filebox-demo\验证问题修复单.md`，权威记录见 `docs/validation-feedback-v011.md`）。**先读 `docs/validation-feedback-v011.md`（每项含现象/根因/修复要求/验收标准）与相关源码**。新代码注释保持中英双语；新增前端文案进三语言 i18n 字典；修复后同步 `docs/requirements/CHANGELOG.md`、`docs/requirements/STATE.md` 与 `RELEASE_NOTES`。

## 批次 1（D5–D10）：问题 1-6（用户实测反馈 + 自定义目录）

- **D5（问题 1，轻微/前端）登录页隐藏默认账号提示**：现状 LoginView 渲染 `login-foot`（`login.defaultAdmin` 文案）；要求删除该行与三语言键；回归：登录页不再显示 `admin / admin123` 提示。
- **D6（问题 2，一般/前端）新建用户无反应**：现状「新建用户」按钮仅展开页面底部表单、错误提示不醒目；要求改为居中 modal 弹窗（复用 modal-backdrop/modal-panel），错误内联显示，提交成功刷新列表；回归：点击即弹窗、密码不合规表单内显示错误。
- **D7（问题 3，一般/前端）拖拽目录误报网络失败**：现状 `handleDrop` 直接把目录条目当 File 上传失败显示「网络连接失败」；要求用 `webkitGetAsEntry()` 识别目录并跳过+明确提示；回归：拖文件夹不报网络错误、拖文件正常。
- **D8（问题 4，一般/前端）独立传输面板**：现状上传进度内嵌主内容、完成后 2.6s 消失；要求顶栏「传输」按钮（进行中角标）+ 右侧抽屉区分上传/下载（下载流式进度）；回归：上传不挡主界面、可随时查看进度与结果。
- **D9（问题 5，轻微/前端）MD5 显示开关**：现状 md5 仅在 title 悬停；要求工具栏「显示 MD5」复选框，持久化 localStorage；回归：勾选后列内直接显示 md5 且刷新保持。
- **D10（问题 6，重大/前后端）用户自定义目录**：移除自动年月目录层（`files/<uid>/[<dir>/]<name>`），新增 `folders` 表与 CRUD API（创建/列表/重命名/删除非空保护）、列表 `dir` 过滤、前端面包屑+新建目录+上传到当前目录、用户隔离、配额按用户；新增 `filebox admin migrate-v010-paths` 迁移旧 `yy/mm` 数据（备份 DB、物理移动、storage_path 前缀重写、登记目录，幂等）；回归：中英文目录创建/上传/重命名级联/空目录删除/迁移后列表下载正常。

## 批次 2（D11–D13）：问题 7-9（logo 跳转 + 单文件交付 + 部署文档）

- **D11（问题 7，轻微/前端）顶栏 logo 点击跳首页**：BrandLogo 增加 `link` prop（RouterLink 包裹 + cursor），文件/管理/日志/分享页 topbar 启用；回归：点击 logo 跳转 `/`。
- **D12（问题 8，一般/构建）单文件交付**：Makefile `release` 目标（CGO_ENABLED=0、-trimpath、-ldflags="-s -w"、Windows/Linux amd64 + SHA256SUMS）+ `scripts/release.ps1`；回归：`make release` 产出两平台单文件 + 校验和。
- **D13（问题 9，轻微/文档）README 部署指南**：README（中英）「配置项」前新增「部署指南（单文件交付）」章节（Windows/Linux 分步 + systemd + Nginx + 生产注意事项）；回归：按文档可从下载到服务化完成部署。

## 批次 3（D14–D17）：问题 10-13（日志/改密/TOTP/版权）

- **D14（问题 10，轻微/前端）日志成功绿/失败红**：`.result-label.success` 固定绿色 `#1e7e34/#e8f5ec`，不随主题色；回归：更换主题色后成功/失败配色不变。
- **D15（问题 11，一般/前后端）日志系统配置分组**：`logActions` 返回完整 24 项动作集；LogsView `actionLabel` 三语文案补齐；筛选下拉「业务/系统配置」分组；回归：可筛出配置类操作（含中文文案）。
- **D16（问题 12，一般/前后端）改密入口 + TOTP 重绑**：FilesView/AdminView/LogsView 顶栏「修改密码」入口；`PUT /api/admin/users/{id}/totp` 支持 `reenroll`（生成新 secret 且 enabled=false，用户下次登录重绑）；AdminView 编辑面板「要求下次重新绑定 TOTP」复选框；回归：普通用户自助改密、管理员重设密码强制改密、勾选重绑后下次登录扫码。
- **D17（问题 13，一般/前后端）Copyright 版权设置**：store `brand_copyright` 键 + `BrandSettings.Copyright`；`/api/brand` 返回 `copyrightText`；`PUT /api/admin/brand` 支持 `copyrightText`/`clearCopyright`；AdminView 品牌面板输入框；BrandFooter 渲染版权；i18n 三语言；回归：保存/读取/清空版权文案且空值不渲染空白页脚。

## 批次 4（D18–D21）：问题 14-17（管理后台重构 + 弹窗 + 日志周期迁移 + 页脚品牌）

- **D18（问题 14，一般/前端）管理后台页签化**：AdminView 左侧竖菜单 + 右侧内容区，六页签（概览/用户管理/安全设置/品牌设置/锁定管理/系统设置）；`?tab=` 深链 + 刷新保持；回归：页签切换不丢已填内容、深链直达。
- **D19（问题 15，轻微/前端）用户编辑/新建弹窗**：新建与编辑用户均改为居中 `modal-backdrop/modal-panel`；回归：保存/关闭后滚动位置不变、字段齐全。
- **D20（问题 16，轻微/前端）日志周期迁移**：LogsView 移除 settings-panel（保留纯展示+筛选）；AdminView「系统设置」页签加入日志保存天数（复用 `PUT /api/admin/settings`，后端零改动）；回归：日志页不再出现保留期设置、后台可设置并生效。
- **D21（问题 17，轻微/前端）页脚品牌信息块**：BrandFooter 首行 `siteTitle` + 小字 `siteDescription`，随后版权/ICP/公安，任一非空渲染；回归：登录后各页面可见品牌标题。

## 批次 5（D22–D23）：问题 18-19（并发冲突队列 + 上传失败日志 + 配额/超限明细）

- **D22（问题 18，重大/前后端）并发同名冲突队列 + 上传失败审计**：前端 `conflictPrompt` 单 ref 改**冲突队列**（数组依次弹出 + 60s 超时取消），并发同名不再互相覆盖卡「准备中」；失败/取消项保留在传输抽屉（红色状态 + 原因 + 重试/移除）；后端 `uploadInit`/`uploadChunk` 失败分支补 `recordAudit` + `serviceEvent`（reason 细分 invalid_name/too_large/conflict/disk_full/quota_exceeded/task_not_found/…），`logActions` 补两动作；重命名后用户可见 name 跟随序号；回归：3 个同名文件 rename 后全部完成（xx.txt/xx (1).txt/xx (2).txt）、日志页可见 upload_init 失败记录。
- **D23（问题 19，一般/前后端）配额/单文件超限提示**：store `QuotaError`（usedBytes/quotaBytes/fileSize）；配额拒绝 403 + `QUOTA_EXCEEDED` 明细；`max-file-size` 超限独立 `413 FILE_TOO_LARGE` + maxFileSize（与非法名分离）；前端 `localizeError` 映射（「配额不足：当前已用 X / 总配额 Y，文件需 Z，超出 W…」「文件超过单文件大小上限 M」）+ i18n 三语言；回归：配额不足提示含明细、超限明确提示上限。

## 批次 6（D24）：问题 20（整体传输速率）

- **D24（问题 20，轻微/前端）整体传输速率**：传输侧边栏顶部「整体速率」（所有进行中上传合计）；每项维护 `loadedBytes`，1s 采样 + 3 点滑动平均；单位自适应（B/KB/MB/GB per s）；无传输隐藏；定时器随组件卸载清理；回归：单/多文件并发上传时速率实时平滑显示、完成后归零/隐藏。

## 执行要求（v011 批次）
1. 先读 `docs/validation-feedback-v011.md`（每项验收标准即回归依据）与相关源码；本任务书按 D 编号与问题号一一对应。
2. 验证全过：`go build ./...`、`go vet ./...`、`go test ./...`；前端 `npm --prefix web run build` + `go run ./scripts/sync-web.go`；DSH 二次测试套件（.test-data\stage2：transfer/share/expire/resume1gb/regress/folders/batch5/batch6/batch7）全绿（124 用例）。
3. 每项按修复单「验收」行核对并记录于 `docs/validation-feedback-v011.md` 状态列。
4. 最终报告：改动文件、验证结果、20 项核对结论。
