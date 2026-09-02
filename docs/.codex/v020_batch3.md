你是 FileBox 项目（Go 后端 + Vue3 前端，i18n 三语 zh-CN/zh-TW/en）的编码工程师。当前 git 工作树干净。本任务只改前端文件，**不得改动后端 Go 代码、路由、无关代码**。完成后运行 `cd web && npm run build` 确认通过（产物 web/dist 不要提交；如报错修正到通过）。

## 需求：我的收集页两个刷新按钮去重（缺陷 5）

背景：`web/src/views/CollectionsView.vue` 存在两个功能完全相同的刷新入口，都调用同一个 `loadCollections()`：
1. 第 5 行 page-heading 内 `.sync-heading-actions` 中的刷新按钮：`<button class="secondary-button" :title="t('files.refresh')" @click="loadCollections"><RefreshCw :size="16" :class="{ spin: collectionsLoading }" /></button>`（旁边是「创建上传收集」primary-button）；
2. 第 8 行 collection-section-header 标题行右侧的刷新按钮：`<button class="icon-button" :title="t('files.refresh')" @click="loadCollections"><RefreshCw :size="17" :class="{ spin: collectionsLoading }" /></button>`（collection-section-header 是「我的收集」小节标题）。

请修复：
1. 移除其中之一（建议保留页面标题区第 5 行那个 secondary-button，删除列表小节标题行第 8 行的 icon-button），使「我的收集」页面只剩一个刷新入口；
2. 若移除第 8 行按钮后 collection-section-header 变空，检查该 div 内是否只剩 h2 标题，若是请简化/保留标题结构本身（h2 仍在，只是不再有多余按钮）；RefreshCw 图标 import 若不再被使用需一并从 import 中移除（确认页面无其它 RefreshCw 用法后）；
3. 其余任何逻辑（分页、创建、编辑、撤销等）一律不动。

验收：页面只出现一个刷新入口（点击刷新收集列表）；`npm run build` 通过；无残留未使用 import。
