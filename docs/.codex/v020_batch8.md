你是 FileBox 项目（Go 后端 + Vue3 前端）的编码工程师。当前 git 工作树干净。本任务只改 CSS（web/src/styles.css 为主，必要时微调 SyncView.vue），**不得改动后端 Go 代码**。完成后运行 `cd web && npm run build` 确认通过（产物 web/dist 不要提交；如报错修正到通过）。

## 需求：同步任务详情弹窗进一步加宽（缺陷 9）

背景：web/src/styles.css 中有：
- `.wide-modal { width: min(1100px, 96vw); }`（v019 已从 780px 加到 1100px，用户仍觉窄）；
- `.sync-log-table { min-width: 1000px; }`、`.sync-log-table-wrap { overflow-x: auto; }` 等；
- 详情弹窗（SyncView.vue 约第 20 行 `v-if="details"` 的 modal）使用 `wide-modal sync-modal` 类，日志表有 7 列（开始/结束/状态/下次执行/文件数/字节/详情）。

请修改：
1. `.wide-modal` 宽度改为 `min(1280px, 96vw)`（或 90vw 取较大视觉效果，以 1280px 目标为准）；
2. 检查详情弹窗内日志表 `.sync-log-table` 的 min-width：确保 1280px 弹窗内 7 列不再横向挤压（min-width 适当加大，如 1160px，配合 .sync-log-table-wrap overflow-x:auto 横向自适应）；同时核对 v-if="details.scheduleType==='periodic'" 动态列（非周期任务少一列）不会因 min-width 造成大片空白——必要时用更合适的方式（如列紧奏/详情列弹性）处理，但不要大改布局；
3. 同步相关其它弹窗（任务编辑/系统编辑/路径选择）也复用 .wide-modal 的酌情保持一致性即可，避免破坏其布局（它们大多有自己的表单栅格）；若加宽明显影响它们，仅对详情弹窗增加覆盖类（如 `.wide-modal.sync-detail-modal { width: min(1280px, 96vw) }`，并让 SyncView 详情弹窗使用该覆盖类）是更稳妥的做法——请自行判断并选择最稳妥方案。
4. 最小必要改动；别动其它页面样式。

验收：同步任务详情弹窗明显更宽（目标 ~1280px），日志表 7 列内容完整展示无需大幅挤压；横向滚动兜底可用；其它页面弹窗不受影响；`npm run build` 通过。
