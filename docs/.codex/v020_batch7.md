你是 FileBox 项目（Go 后端 + Vue3 前端，i18n 三语 zh-CN/zh-TW/en）的编码工程师。当前 git 工作树干净。本任务只改前端（web/src/views/SyncView.vue 为主，必要时 web/src/styles.css 与 i18n.js），**不得改动后端 Go 代码**。完成后运行 `cd web && npm run build` 确认通过（产物 web/dist 不要提交；如报错修正到通过）。

## 需求：同步任务详情日志的「文件详情」从行内展开改为弹窗（缺陷 8）

背景与格式事实（已核实）：
- SyncView.vue 第 20 行详情弹窗（v-if="details"）内日志表（detailLogs，v-for="entry in detailLogs"）的最后一列当前为：
  `<td><details v-if="entry.detail" class="sync-log-detail"><summary>{{ t('sync.detail') }}</summary><pre>{{ entry.detail }}</pre></details><span v-else>-</span></td>`
- 后端 SyncLog.Detail 字段是 `strings.Join(detail 行, "\n")` 的多行文本（internal/httpapi/sync*.go 已核实），每行两类：
  - 文件级行：`<相对路径>: <结果描述>`，如 `a/b.txt: uploaded (1234 bytes)`、`a/b.txt: downloaded (99 bytes)`、`x.txt: skipped (exists)`、`x.txt: skipped (exceeds max file size)`、`x.txt: skipped (insufficient disk space)`、`x.txt: 连接被拒绝(connection refused)`（失败时是 syncErrorDetail 的本地化或英文错误）等；
  - 整体错误行：无文件前缀，如 `同步连接失败: ...`、`读取源文件失败: ...`、`创建目标目录失败: ...`、`传输被取消或限速等待超时`、`同步跳过: 任务所有者不可用` 等。

请把该列改为「查看文件详情」按钮 → 点击弹出新 modal（复用 modal-backdrop/modal-panel + 可加 .sync-file-detail-modal 等新类），弹窗内展示该 entry 的文件明细：
1. 解析 entry.detail：按行拆分，每行尽量拆出「文件路径」（第一个 `: ` 之前部分，注意整体错误行没有冒号+空格则视为无文件路径的整体错误行，保留整行原文）；
2. 结构化展示：解析出的行渲染为列表/表格，列为「文件」与「结果/状态」；根据行内关键词给状态徽标并上色（参照现有 .result-label/.status-label 风格）：
   - 成功：行含 `uploaded` 或 `downloaded` → 绿色 success 风格；
   - 跳过：行含 `skipped`（exists / exceeds max file size / insufficient disk space 等）→ 中性/黄 note 风格；
   - 失败/错误：行含中文「失败」「不可用」「取消」或匹配错误性英文（如 connection refused / permission denied / no such file / disk check failed 等）→ 红色 failure 风格；无法可靠归类（无这些关键词）的行按中性展示原文；
   - 文件路径为空（整体错误行）显示「—」或省略文件列，整行原文展示。
3. 弹窗内也保留"原始内容"折叠/切换（或简单 pre 兜底），保证解析不到时用户仍能看到原文；样式最小必要（新增 CSS 可放 styles.css，别破坏现有表格/弹窗布局）。
4. 替换原来的 `<details>` 单元格：`<td><button v-if="entry.detail" type="button" class="secondary-button/sync-detail-button" @click="openFileDetail(entry)">图标+{{ t('sync.fileDetail') }}</button><span v-else>-</span></td>`。需要的 i18n 键（如 'sync.fileDetail' 文件详情/檔案詳情/File details，及可能的 状态徽标文案 '成功/跳过/失败/整体错误' 等）先在 i18n.js 三语字典补齐（不存在才加，勿覆盖已有值）。
5. 弹窗关闭时清理；不影响现有轮询/进度展示；原有 summary 展开逻辑删除后不得残留死代码。

验收：点击某条日志的「文件详情」弹出 modal，逐文件显示路径+状态徽标（成功绿/跳过黄/失败红）+失败原因；无文件前缀的整体错误行有独立样式展示；`npm run build` 通过。
