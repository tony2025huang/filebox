你是 FileBox 项目（Go 后端 + Vue3 前端，i18n 三语 zh-CN/zh-TW/en）的编码工程师。当前 git 工作树干净。本任务只改前端文件，**不得改动后端 Go 代码、路由、无关代码**。完成后运行 `cd web && npm run build` 确认通过（产物 web/dist 不要提交；如报错修正到通过）。

## 需求：顶栏「传输」按钮从纯图标改为 图标+文字（缺陷 4）

背景：`web/src/views/FilesView.vue` 模板第 5 行附近，通过 AuthenticatedTopbar 的 #actions 插槽渲染传输按钮，当前形态：
`<button class="icon-button transfer-button" :title="t('files.transfers')" @click="transfersOpen = !transfersOpen"><ArrowUpDown :size="18" /><span v-if="activeTransferCount" class="transfer-badge">{{ activeTransferCount }}</span></button>`
（注意：v020 批次 1 已把 ArrowLeftRight 改为 ArrowUpDown，若你看到的是 ArrowLeftRight 请以实际代码为准，本任务不改图标方向。）

顶栏其它导航项（AuthenticatedTopbar.vue 第 7-12 行）都是 `.icon-text-button` 风格：`<RouterLink class="icon-text-button"><图标 /> 文字</RouterLink>`。传输按钮却只有图标（title 提示），与整体导航不一致。

请修改 FilesView.vue 的传输按钮，使其与导航风格一致：
1. 保留现有 @click 切换传输抽屉、角标（transfer-badge）逻辑与按钮可用性；
2. 图标后追加文字 `{{ t('files.transfers') }}`（该 i18n 键已存在：中文『传输』、繁体『傳輸』、英文『Transfers』，先确认存在再使用；若三语缺任一请在 web/src/i18n.js 补全，但不要改动已存在值）；
3. 样式：检查 web/src/styles.css 中 `.transfer-button` 及 `.icon-text-button` 定义。优先复用 `.icon-text-button` 外观（按钮而非链接）；若 `.transfer-button` 有特殊样式（如徽标定位）需要在带文字形态下仍正确显示徽标与对齐，做最小必要 CSS 调整（保留/新增规则，勿影响其它图标按钮）。

验收：FilesView 顶栏传输按钮显示 图标+『传输/傳輸/Transfers』文字且与相邻导航视觉一致；角标仍显示；批量下载进行中、抽屉打开逻辑不受影响；`npm run build` 通过。
