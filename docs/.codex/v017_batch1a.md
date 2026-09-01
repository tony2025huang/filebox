请实际编辑文件 `web/src/views/FilesView.vue`，把其中所有"我的收集"（collection）相关 UI、状态与逻辑删除。这是唯一的任务。完成后运行 `cd web && npm run build` 验证，并把 `git diff --stat` 输出在最后消息里。

背景：FileBox 已有独立收集页 CollectionsView.vue（路由 /collections），FilesView 里残留的收集区块属于重复功能，必须完全移除。注意：`web/src/views/CollectionsView.vue` 与 `web/src/router.js` 不要动。

要删除的具体内容（全部位于 FilesView.vue）：
1. 模板中 `<section id="collections" class="collection-section">...</section>` 整块（含 collection-section-header、collectionsLoading、collection-grid/collection-row、collection-files 等子元素）。
2. 模板 dir-bar 中 class 为 `collection-entry` 的"我的收集"创建按钮（`@click="openCollectionCreate"`）。
3. 模板中 `v-if="collectionCreateOpen"` 与 `v-if="collectionResult"` 两个收集弹窗。
4. script 中的状态：collections、collectionsLoading、collectionCreateOpen、collectionSaving、collectionError、collectionNotice、collectionResult、collectionDetails、editingCollection、collectionForm。
5. script 中的函数：loadCollections、openCollectionCreate、toLocalInputValue、openCollectionEdit、saveCollection、absoluteCollectionUrl、copyCollection、remainingLabel、collectionStatusLabel、viewCollection、revokeCollection。
6. 挂载/初始化中对收集逻辑的所有调用（如 onMounted 里 loadCollections）。
7. 因此不再使用的 lucide 图标 import 一并移除（但保留仍在用的：UploadCloud 用于上传拖放区、Copy 用于分享复制、Eye 用于文件预览、Pencil 用于重命名文件夹、Trash2 用于删除文件/文件夹、RefreshCw 用于刷新等——以实际使用为准）。

验收：删除后 FilesView.vue 中 grep -i "collection" 无任何残留；`cd web && npm run build` 成功；不修改其它任何文件。
