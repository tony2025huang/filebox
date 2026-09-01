你是 FileBox 项目（Go 后端 + Vue3 前端，i18n 三语 zh-CN/zh-TW/en）的编码工程师。当前 git HEAD 工作树干净。本任务只改前端两个文件（外加 i18n），**不得改动路由、后端 Go 代码**。完成后运行 `cd web && npm run build` 确认通过（构建产物 web/dist 不要提交，只需确认构建成功）。

## 需求 1：文件库移除收集区块

背景：FileBox 已有独立的"我的收集"导航页（CollectionsView.vue，路由 /collections，功能完整）。现在文件库页 FilesView.vue 里还残留一份收集区块，需要完全移除，避免功能重复。

文件：`web/src/views/FilesView.vue`，需移除以下内容（逐项核对，移除后不得残留任何"收集/collection"相关 UI 或逻辑）：

1. 模板中 `<section id="collections" class="collection-section">...</section>` 整块（含 collection-section-header、collectionsLoading 空态、collections 列表、collection-grid/collection-row、viewCollection/copyCollection/openCollectionEdit/revokeCollection 等按钮、collection-files 明细展开）。
2. 模板 dir-bar 中"我的收集"创建入口按钮（`collection-entry`，含 `@click="openCollectionCreate"` 与 UploadCloud 图标）——注意 FilesView 的 upload-zone 里还有 UploadCloud 图标用于拖放区，那个保留。
3. 两个收集 modal：`v-if="collectionCreateOpen"` 的创建/编辑弹窗、`v-if="collectionResult"` 的创建结果弹窗。
4. script 中收集相关状态：`collections / collectionsLoading / collectionCreateOpen / collectionSaving / collectionError / collectionNotice / collectionResult / collectionDetails / editingCollection / collectionForm`。
5. script 中收集相关函数：`loadCollections / openCollectionCreate / toLocalInputValue / openCollectionEdit / saveCollection / absoluteCollectionUrl / copyCollection / remainingLabel / collectionStatusLabel / viewCollection / revokeCollection`。
6. 挂载/初始化时对收集逻辑的调用（检查 onMounted 及任何引用上述 ref/函数的代码，一并清理）。
7. 不再使用的 lucide 图标 import 一并移除（但**保留仍在使用的**：UploadCloud 用于上传拖放区、Copy 用于分享链接复制、Eye 用于文件预览、Pencil 用于重命名文件夹、Trash2 用于删除文件/文件夹——请以实际使用为准，只删确实不再被引用的）。

要求：移除后 FilesView 不发起任何 /api/collections 请求；`CollectionsView.vue` 与 `/collections` 路由完全不动。

## 需求 3：导航顺序与命名调整

背景：顶栏（AuthenticatedTopbar.vue）当前顺序是 文件/分享/同步/日志/收集/管理后台，且"分享管理"、"管理后台"命名要改。

文件：`web/src/components/AuthenticatedTopbar.vue`：
- 将导航顺序调整为：**我的文件 → 我的收集 → 我的分享 → 同步任务 → 日志 → 系统设置**（RouterLink 顺序：`/` files、`/collections`、`/shares`、`/sync`、`/logs`、`/admin`，admin 仍仅 `user.role === 'admin'` 显示）。
- 顶栏 section 名（`t('nav.' + section)`）不受影响。

文件：`web/src/i18n.js`（三语 zhCN/zhTW/en 各改一处）：
- `nav.shares`：'分享管理' → '我的分享'（en: 'My shares'，zh-TW: '我的分享'）
- `page.shares`：'分享管理' → '我的分享'（en: 'My shares'，zh-TW: '我的分享'）
- `shares.heading`：'分享管理' → '我的分享'（en: 'My shares'，zh-TW: '我的分享'）——注意 SharesView.vue 页面标题使用此键，确认视图里没有硬编码"分享管理"字样。
- `nav.admin`：'管理后台' → '系统设置'（en: 'System settings'，zh-TW: '系統設定'）
- `page.admin`：'管理后台' → '系统设置'（en: 'System settings'，zh-TW: '系統設定'）
- 路由与页面组件名不改（/shares 仍用 SharesView，/admin 仍用 AdminView）。

## 验收

1. `web/src/views/FilesView.vue` 中 grep 不到 `collection`（大小写不敏感）的任何残留（除注释外完全无）。
2. `web/src/components/AuthenticatedTopbar.vue` 导航顺序与命名符合上述目标。
3. `web/src/i18n.js` 三语键完整（无缺键）。
4. `cd web && npm run build` 构建成功。
5. 不触碰：router.js、任何 .go 文件、CollectionsView.vue、SharesView.vue 内部逻辑。

请实施并简述每处改动。若某个收集相关函数还被其它代码引用导致删除会报错，报告该引用点并保守处理（保持可编译）。
