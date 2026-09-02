你是 FileBox 项目（Go 后端 + Vue3 前端，i18n 三语 zh-CN/zh-TW/en）的编码工程师。当前 git 工作树干净。本任务只改前端（FilesView.vue 为主，如新建共享模块则新建文件），**不得改动后端 Go 代码**。完成后运行 `cd web && npm run build` 确认通过（产物 web/dist 不要提交；如报错修正到通过）。

## 需求：FilesView 的 window.confirm 全部改为 FileBox 自定义确认弹窗（缺陷 1，第一批：FilesView）

背景：FilesView.vue 目前共有 5 处 `window.confirm`：
1. `batchDeleteFolders`（约第 63 行）：`if (!window.confirm(t('confirm.deleteFolders', { count: selectedFolderIds.size }))) return`
2. `batchDelete`（约第 168 行）：`if (!ids.length || !window.confirm(t('confirm.deleteFiles', { count: ids.length }))) return`
3. `removeFolder`（约第 247 行）：`if (!window.confirm(t('confirm.deleteFolder', { name: folder.name }))) return`
4. `queueFiles`（约第 313 行）：`if (keep.length > 50 && !window.confirm(t('files.bulkConfirm', { count: keep.length }))) return`
5. `remove`（约第 455 行）：`if (!window.confirm(t('confirm.deleteFile', { name: file.name }))) return`

参照 FilesView 内已有的冲突提示弹窗实现（askConflict/conflictQueue/activeConflict/chooseConflict，约第 350-367 行：Promise 队列 + 计算属性取队首 + 模板 modal-backdrop/modal-panel）以及第 18 行冲突弹窗模板，实现**通用的确认弹窗**（队列式，防止连续多次确认互相覆盖）：

实现要求：
1. **队列式确认状态**：新增 `confirmQueue = ref([])` 与计算属性 `activeConfirm`（取队首），`askConfirm(message)` 返回 Promise<boolean>：把 `{ message, resolve }` 入队并挂 60s 超时（超时按 false 处理），`chooseConfirm(value)` 取出队首并 resolve（对照 conflictQueue/chooseConflict 模式）。
2. **模板确认弹窗**：在 FilesView 模板中（与其它 modal 同级）新增 `v-if="activeConfirm"` 的弹窗，结构用现成的 `modal-backdrop` + `modal-panel` 类（参照第 18 行冲突弹窗）：eyebrow 静态文本 CONFIRM；正文显示 `activeConfirm.message`（段落样式参照 .modal-copy / conflict-details）；操作按钮两个：取消（`t('common.cancel')`，secondary-button）与 确认（`t('common.confirm')`，primary-button + 危险操作可加 danger 风格）。点击 backdrop 或右上角 X 等同取消。确认按钮文案键：确认使用 `t('common.confirm')`。先确认 i18n 三语都存在 `common.cancel`/`common.confirm`（应为存在；如某语缺失则补齐，不改已有值）。
3. **替换 5 处调用点**：删除/批量删除/删除目录/批量删除目录/批量上传确认全部改为 `if (!(await askConfirm(...))) return` 形式；queueFiles 需要改成 async（或内部 await 后再继续，保持调用方兼容——handleInput/handleDrop 等调用点无需显式 await，fire-and-forget 即可，但要确保队列逻辑正确）。
4. **自检**：确认替换后 FilesView 中不再有 `window.confirm`；其余视图（SharesView/CollectionsView/SyncView/AdminView）本批**不要动**（后续批次处理）。
5. 样式尽量复用现有 CSS 类，如需新规则做最小必要补充；不要为了统一而重构其它弹窗。

验收：FilesView 内 5 个删除/批量/大数量上传场景均弹出 FileBox 风格确认弹窗（非浏览器原生）；连续触发不会挂死或覆盖；取消/确认行为正确；`npm run build` 通过。
