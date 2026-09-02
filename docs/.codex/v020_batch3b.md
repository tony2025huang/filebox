你是 FileBox 项目（Vue3 前端）的编码工程师。当前 git 工作树基本干净（仅未跟踪 docs/.codex 提示词）。**本任务只改 web/src/views/CollectionsView.vue 模板中的两处表达式，不要改动其它任何文件/逻辑，不要运行 git add/commit。**

背景：CollectionsView.vue 模板的分页区（第 8 行末段）存在 v019 同类残留 bug——模板中直接写了 `collectionsPageSize.value` / `collectionsPage * collectionsPageSize.value`。Vue3 模板中 ref 已自动解包，`.value` 取到 undefined，导致 `collectionsTotal > undefined` 恒 false → 我的收集超过一页时**分页条永不显示**（其它视图 v019 已修复，此视图遗漏）。

请修改模板中以下两处（只删除 `.value`，其余一字不动）：
1. `v-if="collectionsTotal > collectionsPageSize.value"` → `v-if="collectionsTotal > collectionsPageSize"`
2. `:disabled="collectionsPage * collectionsPageSize.value >= collectionsTotal"` → `:disabled="collectionsPage * collectionsPageSize >= collectionsTotal"`

自查：确认该文件模板内不再出现 `PageSize.value`（脚本里的 `collectionsPageSize.value` 等是正常的，不要动）；运行 `cd web && npm run build` 确认通过（产物不提交）。

验收：模板仅两处 `.value` 移除；多页收集时显示分页条；build 通过。
