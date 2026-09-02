# CODEX_TASK_v020.md — v020 批次开发任务书

- 基准：git HEAD=d7b07e6（v019 完成后，工作树干净）
- 执行方式：codex CLI（--ephemeral，UTF-8 提示词文件 → stdin 管道）；codex 卡住（10 分钟无落盘）→ 杀进程重启更小任务；同一小项连续 3 次失败 → 上报父任务，禁止子任务直接写代码
- 每批验收：`npm run build`（web，勿提交 web/dist）或 `go test ./...`（后端）通过 → 提交 → 推送
- 全部完成后：全量 `go test ./...` + `npm run build`；补测试；文档（CHANGELOG/STATE/RELEASE_NOTES×2/README×2）；重建发布产物（dist 双平台 + SHA256SUMS）；三端部署；Release v0.2.0 附件与 body 更新

## 一、问题清单与验收（11 项）

| # | 类型 | 问题 | 定位（已核） | 修复方向 | 验收标准 |
|---|------|------|------------|---------|---------|
| 1 | 优化 | 删除/撤销等操作使用浏览器原生 confirm | 全仓 window.confirm 调用点共 12 处：FilesView.vue L63(batchDeleteFolders)/L168(batchDelete)/L247(removeFolder)/L313(queueFiles>50 bulkConfirm)/L455(remove)；SharesView.vue L44(revokeGroup)/L71(revokeShare)；CollectionsView.vue L51(revokeCollection)；SyncView.vue L111(deleteTask)/L138(deleteSystem)；AdminView.vue L93(resetBrand)/L101(remove) | 先枚举全部调用点，再统一改为 FileBox 自定义确认弹窗（modal-backdrop/modal-panel 风格，参照 FilesView conflictPrompt 队列 Promise 模式；建议新建通用 ConfirmDialog 组件或 composable，避免 5 个视图重复造轮子） | 删除文件/批量删除/删除目录/批量删除目录/撤销分享（含聚合分享）/撤销收集/删除同步任务系统/删用户/重置品牌等操作均弹出自定义确认框；无 window.confirm 残留 |
| 2 | 缺陷 | 聚合下载失败提示"创建下载文件失败"无具体原因 | 后端 internal/httpapi/server.go batchDownload L2278-2399（L2330/2354/2368/2372 写死"创建下载文件失败"，L2362"读取文件内容失败"，L2319"无法检查系统存储空间"；真实错误只 log.Printf 未透传）；share_group.go shareGroupBatchDownload L274+ 同模式；前端 FilesView L141-161 batchDownload→streamDownload(L403-438) 已会显示后端 message | 后端把底层原因（磁盘满/权限/文件缺失/io 错误）并入错误响应（data.code 或 message 后缀），前端原样显示；下载文件名 filebox-batch-download.zip 加时间戳 → filebox-batch-YYYYMMDD-HHMMSS.zip（后端 Content-Disposition L2387/425 + 前端 L154 与 BatchShareView L78） | 构造磁盘满/缺文件/无权限场景，前端提示包含具体原因；zip 文件名带时间戳后缀 |
| 3 | 功能 | 传输任务缺批量控制 | FilesView 传输抽屉 L23-35：上传行已有单任务 暂停/继续/重试/移除（L30，pauseUpload L336/resumeUpload L337/retryUpload L338）；下载行仅有取消（L33 cancelDownload L162，AbortController）；断点续传基础已有（taskId/uploaded chunks；后端 DELETE /api/upload-tasks/{taskID} L351 释放配额）；后端文件下载已用 http.ServeContent（server.go L2273/3040 等）支持 Range | 抽屉增加批量选择（checkbox）+ 批量按钮：暂停选中/继续选中/终止选中 + 暂停全部/继续全部/终止全部；终止=DELETE /api/upload-tasks/{taskId}；下载暂停=abort，继续=带 Range 重新请求（需前端支持分段续传，流合并） | 可勾选多个上传/下载任务批量暂停/继续/终止；终止释放配额；下载暂停后续传字节不重复 |
| 4 | 优化 | 顶栏「传输」按钮仅图标 | FilesView L5：`<button class="icon-button transfer-button" :title="t('files.transfers')">` 只有 ArrowLeftRight 图标+角标，无文字；AuthenticatedTopbar 其他导航均为 icon-text-button（图标+文字） | 改为图标+文字（如 icon-text-button 风格 + t('files.transfers')），保留角标 | 传输按钮与其余导航一致显示「传输」文字 |
| 5 | 缺陷 | 我的收集页有两个刷新按钮 | CollectionsView.vue L5 page-heading 内 secondary-button 刷新（loadCollections）与 L8 collection-section-header 内 icon-button 刷新（同为 loadCollections），功能重复 | 分析后保留一个（建议保留页面标题区按钮，删除列表区头部按钮；或反之，但只能留一个） | 页面只出现一个刷新入口，点击均刷新收集列表 |
| 6 | 缺陷 | 「复制链接」文字多余/错位 | web/src/i18n.js zh（L146）'shares.copy' 键出现两次：首次为页面副标题描述句（SharesView L5 muted 段落使用），其后再次 'shares.copy':'复制链接'（行内图标 title 使用 L7/8/11）→ JS 对象后者覆盖前者，中文下页面副标题显示成"复制链接"；zh-Hant/en 字典仅存 '複製連結'/'Copy link' 单值 → 繁体/英文副标题同样错误 | 拆分为两个键：新增如 'shares.intro'（三语描述句）供页面副标题 L5 使用；'shares.copy' 保留 '复制链接/複製連結/Copy link' 仅作复制按钮 title；清理重复键 | 分享管理页副标题显示描述句、复制图标 title 显示复制链接；三语正常 |
| 7 | 优化 | 聚合分享编辑弹窗与单个分享编辑不一致 | SharesView openGroupEdit 弹窗（L14）= 仅 expiresAt(datetime-local)+maxDownloads+成员增删；单个分享 openDetails 弹窗（L11）= 基本信息网格(token/usage/expires/remaining/status/createdAt)+链接展示+延期(extend)/增次(increase) 操作+下载日志；聚合分享 groupAction 延期/增次弹窗（L12）已存在但 openGroupAction(L59) 无调用方（死代码），聚合行仅 Eye/Copy/Trash | 按单个分享弹窗的布局与逻辑对齐聚合编辑弹窗：基本信息网格、链接展示+打开分享页、延期/增次（小时制，接通 openGroupAction 死代码按钮）、保留成员管理区+保存 | 聚合分享编辑弹窗包含：基本信息网格、链接+打开、延期/增次按钮可用（调用现有后端 extend/increase 接口）、成员增删、保存 |
| 8 | 优化 | 同步任务详情「文件详情」行内展开 → 弹窗 | SyncView.vue L20 详情弹窗日志表每行 detail 列：`<details class="sync-log-detail"><summary>{{ t('sync.detail') }}</summary><pre>{{ entry.detail }}</pre></details>` | 先分析 entry.detail 实际格式（同步引擎写入，多为每文件一行），改为弹窗展示：解析 detail 为结构化列表（文件、同步状态、失败原因）或 pre+状态高亮 | 点击"详情"弹窗展示文件列表与每文件状态/失败原因；无行内 <details> |
| 9 | 优化 | 同步任务详情弹窗仍窄 | web/src/styles.css `.wide-modal { width: min(1100px, 96vw) }`；`.sync-log-table { min-width: 1000px }`（v019 已 1100px） | 进一步加宽（1280px 或 90vw），表格横向自适应（min-width 加大/紧奏排布） | 详情弹窗 1280px 左右；7 列+详情列无横向挤压或可横向滚动完整展示 |
| 10 | 缺陷 | 日志失败原因中文模式显示英文 | LogsView.vue reasonLabel L79 现有映射 32 项；i18n logReason 键 zh 现有 32 项 | 枚举后端全部 reason 枚举（upload_init/upload_chunk：invalid_name/too_large/conflict/disk_full/quota_exceeded/task_not_found/invalid_index/rate_limited/size_mismatch；登录：user_not_found/wrong_password/user_disabled/locked/ip_locked/totp_failed；分享/同步等），与 LogsView 映射及 i18n 三语字典比对，补齐缺失项；未映射的显示原始值 | 三语下所有后端 reason 均有本地化文案；未覆盖 reason 显示原始值兜底 |
| 11 | 优化 | 传输图标方向不对（左右箭头 vs 上下传输） | FilesView L5 传输按钮 `ArrowLeftRight`；import L47 引入 ArrowLeftRight；全仓 ArrowLeftRight 仅此一处 | 改为 ArrowUpDown（上下箭头），import 同步；检查其他左右箭头语义不符场景 | 传输按钮显示上下箭头 |

## 二、批次计划

| 批 | 内容 | 验证 | 涉及文件（预估） |
|----|------|------|----------------|
| 1 | 缺陷 6（shares.copy 键冲突）+ 缺陷 11（ArrowLeftRight→ArrowUpDown） | npm run build | i18n.js、SharesView.vue、FilesView.vue |
| 2 | 优化 4（传输按钮加文字） | npm run build | FilesView.vue（+styles.css/i18n 如需要） |
| 3 | 缺陷 5（收集双刷新） | npm run build | CollectionsView.vue |
| 4 | 缺陷 1a（FilesView confirm → 自定义弹窗，或通用组件先行） | npm run build | FilesView.vue（+ 新组件/composable） |
| 5 | 缺陷 1b（Shares/Collections/Sync/Admin 剩余 confirm 收口） | npm run build | SharesView/CollectionsView/SyncView/AdminView |
| 6 | 缺陷 10（日志 reason 映射补齐） | npm run build | LogsView.vue、i18n.js（三语） |
| 7 | 优化 8（同步文件详情弹窗化） | npm run build | SyncView.vue（+styles.css） |
| 8 | 优化 9（详情弹窗加宽） | npm run build | styles.css、SyncView.vue |
| 9 | 缺陷 2（聚合下载错误透传+文件名时间戳） | go test + npm run build | server.go、share_group.go、FilesView.vue、BatchShareView.vue |
| 10 | 优化 7（聚合分享编辑对齐单个分享） | npm run build | SharesView.vue |
| 11 | 功能 3（传输批量控制） | npm run build（必要时 go test） | FilesView.vue（+styles.css/i18n） |
| 12 | 全量 go test + npm build + 补测试（reason 映射/错误透传/批量控制） | go test ./... | *_test.go |
| 13 | 文档（CHANGELOG/STATE/RELEASE_NOTES×2/README×2） | — | docs |
| 14 | 发布产物 dist 双平台 + SHA256SUMS + 三端部署 + Release v0.2.0 附件/body | 健康检查 | dist、deploy |

注：批 4/5 可视 codex 产出质量决定先做通用组件再逐视图替换；缺陷 3 体量最大，如 codex 单批难完成可拆 3a(上传批量)/3b(下载暂停续传)。

## 三、环境事实

- codex：`C:\Users\huangcp\.codex\packages\standalone\current\bin\codex.exe`；调用 `$prompt | & $codex exec -C <repo> --skip-git-repo-check -s danger-full-access --ephemeral -`
- 提示词放 docs/.codex/v020_*.md（UTF-8）；前端构建在 web/ 下 `npm run build`（输出 web/dist，不提交；go embed 目录 internal/webassets/dist 由 scripts/sync-web.go 同步，仅部署阶段执行）
- Go 工具链需前置 PATH：`C:\Users\huangcp\AppData\Local\Programs\Go\go\bin`；node：`C:\Users\huangcp\AppData\Local\Programs\nodejs`
- 每批完成即提交推送（git 身份 dsh&codex 已配），发简短进度消息；阻塞立即上报
