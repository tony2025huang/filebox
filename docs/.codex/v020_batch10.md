你是 FileBox 项目（Go 后端 + Vue3 前端，i18n 三语 zh-CN/zh-TW/en）的编码工程师。当前 git 工作树干净。本任务只改前端 web/src/views/SharesView.vue（必要时加 i18n.js 键与 styles.css 少量样式），**不得改动后端 Go 代码**。完成后运行 `cd web && npm run build` 确认通过（产物 web/dist 不要提交；如报错修正到通过）。

## 需求：聚合分享编辑弹窗对齐单个分享编辑弹窗（缺陷 7）

背景（已核）：
- SharesView.vue 单文件分享「查看详情」弹窗（`v-if="selected"`，约第 11 行 openDetails 打开）布局：`share-detail-grid` 基本信息网格（token / usage 已下载·上限 / expiresAt / remaining / status / createdAt 六项 dt+dd）+ 链接行（share-url 输入框 + Copy 复制 + 可打开分享页）+ `share-manage-actions` 两个表单（延期 extendHours 小时制 → PUT /api/shares/{token}/extend；增加下载次数 increaseMax → PUT /api/shares/{token}/increase）+ 下载日志区（share-log-section：表 + 刷新）。
- 聚合分享「编辑」弹窗（`v-if="groupEdit"`，约第 14 行 openGroupEdit(group) 打开，行内眼睛按钮触发）当前只有：expiresAt(datetime-local) + maxDownloads(number) + 成员文件增删（group-member-section：成员列表 + 待加文件下拉）+ 保存/取消。没有基本信息网格、没有链接展示/打开页、没有小时制延期/增次。
- 后端已有聚合分享接口：PUT /api/shared-groups/{token}/extend（expiresInHours）、PUT /api/shared-groups/{token}/increase（maxDownloads）（前端 submitGroupAction 约 L62 已实现，openGroupAction 约 L59 与 groupAction 弹窗约 L12 存在但无调用方——死代码）。

请把聚合分享编辑弹窗（openGroupEdit/groupEdit）调整为与单文件分享详情弹窗一致的布局与能力（保留聚合特有的成员管理）：
1. **基本信息网格**：仿照 share-detail-grid 增加一行/网格展示聚合分享的基本信息（在打开弹窗时把 group 数据带全：token、已下载 group.downloadCount / 上限 group.maxDownloads、expiresAt、remaining、status、createdAt——需要时在 openGroupEdit 里从 group 行对象直接取；若行对象缺 createdAt/status 等字段，先查 /api/shares/groups 返回字段与 groupEdit 现有数据，实在没有的字段可省略但至少含 token/usage/expires/remaining/status）。
2. **链接展示**：弹窗内加 share-url 输入框展示该聚合分享链接（group.url → 绝对地址，参照 groupUrl/copyGroupUrl/openGroupPage），提供 Copy 复制与「打开分享页」（openGroupPage）入口；i18n 键优先复用现有 shares.link / shares.copy / shares.open 等。
3. **延期/增次**：在弹窗内放与单文件分享一致的「延期（小时）/增加下载次数」操作——复用 openGroupAction(group, 'extend'|'increase') 与现有 groupAction 弹窗（把死代码接通用），或直接在编辑弹窗内嵌两个小时制/次数输入+提交（调用 PUT extend/increase 后刷新 groups 与本地 groupEdit）；选择实现简单、与单文件分享交互一致的方式。
4. **成员管理区保留**：现有的成员列表+增删+保存（saveGroupEdit PUT /api/shared-groups/{token} 更新 expiresAt/maxDownloads）保留并合理布局到弹窗下半部。
5. 布局顺序建议：基本信息网格 → 链接展示 → 延期/增次 → 成员管理 → 保存/取消；样式复用 share-detail-grid/share-url/modal 既有类，必要时少量新增 CSS。
6. 完成后检查：聚合行内眼睛按钮仍是打开该编辑弹窗的唯一入口（保持三图标 Eye/Copy/Trash）；不要破坏分页与其它逻辑。

验收：点击聚合分享行的眼睛 → 弹窗含基本信息网格、链接+复制+打开页、小时制延期/增次（成功后列表行即时更新）、成员增删与保存；无死代码残留（openGroupAction/groupAction 被实际调用或按新结构处理）；`npm run build` 通过。
