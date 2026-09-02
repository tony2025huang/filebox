你是 FileBox 项目（Go 后端 + Vue3 前端，i18n 三语 zh-CN/zh-TW/en）的编码工程师。当前 git 工作树干净。本任务只改前端 4 个文件：`web/src/views/SharesView.vue`、`web/src/views/CollectionsView.vue`、`web/src/views/SyncView.vue`、`web/src/views/AdminView.vue`；**不得改动后端 Go 代码、路由、无关代码**。完成后运行 `cd web && npm run build` 确认通过（产物 web/dist 不要提交；如报错修正到通过）。

## 需求：其余视图的 window.confirm 统一改为自定义确认弹窗（缺陷 1 第二批）

背景：v020 第一批已在 `web/src/views/FilesView.vue` 实现队列式自定义确认弹窗（askConfirm/confirmQueue/activeConfirm/chooseConfirm + modal-backdrop/modal-panel 模板，Promise 队列，60s 超时按 false）。请先阅读 FilesView.vue 中该实现并**复用同样的模式**（可照搬简化版，不要引入新的共享组件/组合式文件，保持与 FilesView 一致的形态），替换以下 4 个视图中剩余的 `window.confirm` 调用点（共 7 处）：

1. `web/src/views/SharesView.vue`：
   - `revokeGroup`（约第 44 行）：`if (!window.confirm(t('shares.confirmRevoke'))) return`
   - `revokeShare`（约第 71 行）：`if (!window.confirm(t('shares.confirmRevoke'))) return`
2. `web/src/views/CollectionsView.vue`：
   - `revokeCollection`（约第 51 行）：`if (!window.confirm(t('collection.confirmRevoke'))) return`
3. `web/src/views/SyncView.vue`：
   - `deleteTask`（约第 111 行）：`if (!window.confirm(t('sync.confirmDeleteTask', { name: item.name }))) return`
   - `deleteSystem`（约第 138 行）：`if (!window.confirm(t('sync.confirmDeleteSystem', { name: item.name }))) return`
4. `web/src/views/AdminView.vue`：
   - `resetBrand`（约第 93 行）：`if (!window.confirm(t('confirm.resetBrand'))) return`
   - `remove`（约第 101 行）：`if (!window.confirm(t('confirm.deleteUser', { name: item.username }))) return`

要求：
1. 每个视图在 <script setup> 内加入队列式确认状态与函数（askConfirm(message) → Promise<boolean>；chooseConfirm(value)；确认弹窗 UI 加到该视图模板根部、与既有 modal 同级，用 modal-backdrop/modal-panel 类；按钮：取消 t('common.cancel') + 确定 t('common.confirm')，危险操作（删除/撤销/重置）的确定按钮建议用 danger 风格；eyebrow 用静态 CONFIRM；点击 backdrop/右上角 X 视为取消）。
2. 上述 7 个调用点全部改为 `if (!(await askConfirm(...))) return` 形式（对应函数已是 async 的无需改签名，AdminView remove/resetBrand 与 SharesView/CollectionsView/SyncView 的相关函数若原本不是 async 请改成 async，注意调用方是否 await 不影响正确性——它们大多是事件处理器）。
3. 完成后 `window.confirm` 在 web/src 下应完全绝迹（可用搜索确认）。
4. 不要改动这些视图的其它业务逻辑。

验收：web/src 内无 window.confirm 残留；5 个视图的删除/撤销/重置类操作均弹 FileBox 自定义确认框；`npm run build` 通过。
