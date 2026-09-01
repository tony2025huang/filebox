# CODEX 任务书 v018 批次（8 项需求）

> 批次负责人：dsh 子代理。全部编码由 codex CLI 实施（`--ephemeral` + stdin 提示词）。
> 基线：`ed67db1`（工作树干净）。每批 1 项需求（大文件任务再拆），完成即验证 + 提交 + 推送。

## 执行纪律（v017 教训）

1. 单个 codex 任务必须小：1 项需求/次，优先单文件；大文件（>300 行，FilesView/SyncView）由提示词给出**精确行级修改指令**或拆分到最小。
2. 提示词写 UTF-8 文件，`[System.IO.File]::ReadAllText(..., UTF8)` 读取后经 stdin 管道传给 codex；禁止命令行直传长提示词。
3. codex 8 分钟无落盘视为失败：`Get-Process codex | Stop-Process -Force` 后换更小任务重试。
4. 每完成 1 项立即 `go build ./... && go test ./...`（Go 改动时）+ `npm run build`（前端改动时）→ git 提交推送。
5. 全部加 `--ephemeral` 防孤儿进程；`--skip-git-repo-check -s danger-full-access`。
6. 阻塞立即报告，不静默等待。

## 需求对照表

| # | 需求 | 涉及文件 | 方案 | 验收 |
|---|------|---------|------|------|
| 1 | nav.admin 中文显示原文（bug） | `web/src/components/AuthenticatedTopbar.vue` | 根因：`t(\`nav.${section}\`)`，AdminView 传 `section="admin"`，但 v017 已把键改名 `nav.system`，`nav.admin` 键不存在 → t() 回退原文。修复：section→nav 键映射（admin→nav.system，其余直连），或恢复 `nav.admin` 键别名 | 中文模式顶栏 section-name 显示「系统设置」，三语均不出现 `nav.admin` 原文 |
| 2 | 聚合分享编辑增强 | `internal/httpapi/share_group.go`、`internal/store/share_group.go`、`web/src/views/SharesView.vue` | ① 管理端成员文件列表：`GET /api/shared-groups/{token}/files`（复用 ListShareGroupFiles）；② 成员管理：`POST /api/shared-groups/{token}/files`（fileIds 增成员，校验归属/ready/上限 500）、`DELETE /api/shared-groups/{token}/files/{fileId}`；③ 属性编辑：新增 `PUT /api/shared-groups/{token}`（expiresAt 绝对时间 + maxDownloads，不缩短/不降低，校验同 extend/increase）。前端聚合分享卡片加眼睛图标（成员列表弹窗）+ 编辑弹窗 | 眼睛图标弹窗展示成员文件名/大小；编辑弹窗可增删成员文件、改有效期/次数；go test 覆盖成员增删与属性更新 |
| 3 | 收集「复制链接」与「查看已收文件」同一行 | `web/src/views/CollectionsView.vue` | 收集项行内布局：viewFiles 按钮与 copy 图标按钮并排（同一行容器），链接行独立 | 两按钮同一行，布局不破 |
| 4 | 同步任务传输进度展示 | `internal/httpapi/sync.go`、`web/src/views/SyncView.vue` | 后端：Server 增加进程内 SyncProgress 注册表（mutex 保护，map[logID]），executeSyncPush/Pull 每文件循环更新（currentFile/filesDone/totalFiles/currentBytes/totalBytes/rate 采样），执行结束清注册表；新增 `GET /api/sync/tasks/{id}/progress`（running 时返回进度，无 running 返回 404/空）。前端：任务详情弹窗对 running 日志轮询（2s），渲染进度条 + 当前文件 + 速率 | 执行中能看到当前文件/已传文件数/字节/速率；结束或失败后进度归位 |
| 5 | 同步日志列表列增强 | `web/src/views/SyncView.vue`、`internal/httpapi/sync.go`、`internal/store/sync.go` | sync_logs 已有 runAt/finishedAt/result/files/bytes/message/detail；补充：① 周期任务「下次执行时间」由 cron 计算（复用 latestCronOccurrence 或新增 nextCronTime 纯函数返回下次时间戳）；② 日志行改为表格列：开始时间/结束时间/状态/下次执行(仅周期)/结果/文件数/大小；③ 详情展开显示 detail 逐行（文件传输列表） | 日志区为表格列展示；周期任务行显示下次执行时间；点击展开文件明细 |
| 6 | 日志时间筛选 UI 调整 | `web/src/views/LogsView.vue`、`web/src/i18n.js` | 把两个 datetime-local 标签框改为**一个整体「时间范围」字段**：点击按钮弹出下拉面板，内含开始/结束两个 datetime-local，任一留空表示不限该端；筛选按钮不变。新增 i18n 键 `logs.timeRange` | 工具栏只显示一个时间范围字段；弹层设置开始/结束；留空端不限 |
| 7 | 分页增强（pageSize 选择器 + 跳页） | `web/src/views/FilesView.vue`、`SharesView.vue`、`CollectionsView.vue`、`LogsView.vue`、`internal/httpapi/server.go`（listShares/listCollections/listShareGroups） | 后端：`listShares`、`listCollections`、`listShareGroups` 增加分页（复用 pagination()，上限 100，返回 page/pageSize/total）。前端：四页统一 pageSize 选择器（10/20/50/100，localStorage 记忆）+ 页数过多时跳页输入框+按钮 | 四页均支持选择每页条数；可输入页码跳转；后端超上限截断 100 |
| 8 | 我的文件目录与文件合并展示 | `web/src/views/FilesView.vue`、`web/src/i18n.js`、`web/src/styles.css` | 目录从独立 folder-list 移入文件表格：合并列表（目录在前按名称、文件按名称），目录行图标 Folder、点击行进入；类型列显示「目录」；size/时间列目录显示 '-'；checkbox/分享/删除对目录行隐藏。分页只计文件（保持现有 total 语义） | 目录与文件同一表格；目录在前；点击目录行进入下一级；无回归（新建/重命名/删除目录按钮保留） |

## 批次计划（1 项/批，串行）

| 批次 | 需求 | 类型 | 提示词要点 |
|------|------|------|-----------|
| B1 | 1 | 前端单文件小改 | AuthenticatedTopbar.vue section 映射函数 |
| B2 | 6 | 前端单文件小改 | LogsView 时间范围字段 + i18n 键 |
| B3 | 3 | 前端单文件小改 | CollectionsView 行布局 |
| B4 | 7a | 后端分页 | listShares/listCollections/listShareGroups 加分页 |
| B5 | 7b | 前端分页 | 四页 pageSize 选择器 + 跳页（先 FilesView/LogsView，再 Shares/Collections） |
| B6 | 2a | 后端成员管理 | share_group.go store+httpapi 成员增删 + 属性编辑 |
| B7 | 2b | 前端编辑弹窗 | SharesView 眼睛 + 编辑弹窗 |
| B8 | 4a | 后端进度 | sync.go 进度注册表 + progress 端点 |
| B9 | 4b | 前端进度 | SyncView 轮询进度条 |
| B10 | 5a | 后端日志字段 | nextRun 计算 + publicSyncLog 扩展 |
| B11 | 5b | 前端日志列 | SyncView 日志表格列 + 详情展开 |
| B12 | 8 | 前端合并展示 | FilesView 合并列表（拆 1-2 个小步） |

> B1 前先完成本任务书提交推送（docs-only 提交）。

## 全量收尾

- `go build ./... && go test ./...`（全量）+ `npm run build`（web 产物同步 internal/webassets/dist）
- 补测试：需求 1 键映射（前端难以单测则跳过，改为人工验收）；需求 2 成员增删 API（store+httpapi）；需求 4/5 进度字段与 nextRun（store/httpapi）；需求 7 pageSize 上限（httpapi）
- 文档：CHANGELOG.md v018、STATE.md、RELEASE_NOTES×2、README×2
- 发布：dist 双平台 + SHA256SUMS → 三端部署（18080/18090/202.6.205.59）→ Release v0.2.0 附件更新
