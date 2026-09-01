# CODEX_TASK_v019.md — v019 批次开发任务书

- 基准：git HEAD=1628b25（v018 完成后，工作树干净）
- 执行方式：codex CLI 优先（--ephemeral，UTF-8 提示词文件 → stdin 管道）；codex 连续 2 次中断未落盘 → 按相同规格由 agent 直接应用
- 每批验收：`npm run build`（web）或 `go test ./...`（后端）通过 → 提交 → 推送

## 一、问题清单与验收（7 项 = 4 缺陷 + 3 优化）

| # | 类型 | 问题 | 根因（已定位） | 修复方案 | 验收标准 |
|---|------|------|----------------|----------|----------|
| 1 | 缺陷 | 日志时间范围弹层面板无「确定/清空」按钮 | LogsView.vue 第 6 行 time-range-popover 只有两个 datetime-local 输入框，直接 v-model 绑定 filters.from/to，改动立即生效且无提交/清空入口 | 弹层内加「确定」（应用草稿 from/to 并 applyFilters）与「清空」（清空两时间并 applyFilters）按钮；输入用草稿值（timeDraft），关闭弹层未确定不生效 | 打开时间面板 → 修改时间 → 关闭 → 列表未变；点「确定」→ 应用并刷新；点「清空」→ 两时间清空并刷新 |
| 2 | 缺陷 | 日志 67 条未显示分页（应为 4 页，pageSize=20） | LogsView.vue 第 9 行模板 `v-if="total > pageSize.value"`：Vue3 模板中 ref 自动解包，`pageSize` 已是数值，`.value` 为 undefined → `67 > undefined` 恒 false → 分页永不显示 | 模板中所有 `pageSize.value` 改为 `pageSize`（含 disabled 表达式） | 67 条日志时显示分页条：4 页、页码按钮、上一页/下一页、每页条数选择 |
| 3 | 缺陷 | 我的文件分页未显示 | FilesView.vue 第 16 行同 2：`total > pageSize.value`、`page * pageSize.value >= total` 恒 false；SharesView.vue 第 7/8 行 `sharePageSize.value`/`groupPageSize.value` 同病 | 三视图模板 `pageSize.value` → `pageSize`（FilesView 2 处、SharesView 4 处、LogsView 2 处） | 多页时显示分页条（LogsView/FilesView/SharesView 三处） |
| 4 | 缺陷 | 点击目录进入提示「目录无效」 | ① user-test(18090) 库 folders 表存在 v010 遗留记录 `uploads\mixEN...`（含反斜杠路径，3/4 条），点击后 `dir=uploads\xxx` → validateUploadDir 拒绝反斜杠 → 400；② 18080 演示库 872 条目录中 869 条带 `files/` 前缀（同步 createFolder 传了完整 storage 前缀），点击进入后列表错误（空/错位）；③ validateFolderName 未拒绝 `.`/`..`（可造出必 400 目录） | 后端 listFolders 过滤并归一化：去掉 `files/` 或 `files/<uid>/` 前缀；仍不合法的路径（如 `uploads\...`）直接剔除；validateFolderName 增加拒绝 `.`/`..`；前端 navigateDir 保持传相对路径 | ① user-test 中遗留 `uploads\xxx` 目录不再出现在列表、点击不报 400；② 演示库 `files/Net3.5/sxs2012` 点击进入 `Net3.5/sxs2012` 且文件列表正确；③ 新建目录名 `.`/`..` 被拒绝 |
| 5 | 优化 | 聚合分享小眼睛功能调整 | SharesView.vue：groups 行已有 Eye+Copy+Trash 三图标，但存在死代码 groupFiles 只读弹窗（无按钮引用）与 Eye 打开的编辑弹窗（成员增删+有效期/上限）功能重叠 | 删除 groupFiles 死弹窗与 openGroupFiles；Eye 弹窗重构为「文件范围列表」为首（成员列表+增删），有效期/上限编辑紧随其后，弹窗内附链接与打开分享页；行内保持 Eye+Copy+Trash 三图标 | 聚合分享行仅 3 图标；点小眼睛弹出合并弹窗（成员列表增删 + 有效期/上限编辑 + 链接）；无多余图标/弹窗 |
| 6 | 优化 | 同步任务查看日志 UI 布局 | SyncView.vue 第 20 行详情弹窗 `wide-modal` 仅 780px 宽，日志表 min-width 860px 需横向滚动；4 条失败日志 detail 内容雷同（同一批文件 connection lost），每行一个可展开 `<details>`，窄弹窗中反复展开观感为「4 份重复详情」 | 详情弹窗改用加宽弹窗（新增 xwide-modal ≈1200px，占满内容区）；日志表 min-width 提升、列紧凑排布（开始/结束/状态/下次执行/文件数/大小/详情 定宽列+详情弹性列）；详情展开互斥（同一时间只展开一条）并逐行展示各自文件明细，避免重复观感 | 点「查看日志」打开宽弹窗，7 列紧凑排布无横向挤压；展开一条详情不会同时出现多份相同内容；不同运行记录各自展示明细 |
| 7 | 功能 | 目录支持批量操作 | FilesView.vue 选中机制 selectedIds 仅含文件 id（第 53-58 行）；目录行 checkbox 列为空 | 扩展选中机制：新增 selectedDirIds（目录 id 集合）；目录行加 checkbox；工具栏批量按钮按「文件数+目录数」计数；批量删除：先删选中目录（DELETE /api/folders/{id}，逐个，非空提示）再删文件；批量重命名：逐目录打开重命名弹窗（复用 folderPrompt）；批量分享/批量下载仅对文件（目录不含）并在 UI 说明；全选含当前页目录 | 目录行可选中有 checkbox；勾选目录后批量删除/批量重命名可用；批量分享/下载按钮仅统计文件；多目录删除/重命名成功后刷新列表 |

## 二、批次计划

| 批 | 内容 | 验证 |
|----|------|------|
| 1 | 缺陷 1-3（LogsView 时间按钮、LogsView/FilesView/SharesView 分页） | npm run build |
| 2 | 缺陷 4（后端 listFolders 过滤归一化 + validateFolderName + 前端） | go test + npm run build |
| 3 | 优化 5（SharesView 小眼睛合并） | npm run build |
| 4 | 优化 6（SyncView 宽布局 + 紧凑列 + 详情互斥） | npm run build |
| 5 | 功能 7（FilesView 目录批量操作） | npm run build |
| 6 | 全量 go test + npm build + 补测试（时间筛选按钮/分页/目录导航/批量目录操作） | go test ./... |
| 7 | 文档（CHANGELOG/STATE/RELEASE_NOTES×2/README×2） | — |
| 8 | 发布产物 dist 双平台 + SHA256SUMS + 三端部署 + Release v0.2.0 附件 | 健康检查 |

## 三、环境事实

- codex：`C:\Users\huangcp\.codex\packages\standalone\current\bin\codex.exe`；调用 `$prompt | & $codex exec -C <repo> --skip-git-repo-check -s danger-full-access --ephemeral -`
- 8-10 分钟无落盘视为失败 → 杀进程 → 按相同规格直接应用
- 每项完成即提交推送，发简短进度消息；阻塞立即报告
