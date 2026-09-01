你是 FileBox 项目（Go 后端 + Vue3 前端，i18n 三语 zh-CN/zh-TW/en）的编码工程师。本任务只改前端 `web/src/views/SyncView.vue` 与 `web/src/i18n.js`，**不得改动后端 Go 代码**。完成后运行 `cd web && npm run build` 确认通过（web/dist 不要提交）。

## 需求 A：同步任务目录选择增强（picker 过滤 + 手动输入路径）

背景：`web/src/views/SyncView.vue` 的路径选择器（picker modal，模板 line 18 附近）支持浏览本地 FileBox 目录（`fileboxFoldersAt` 从已加载的 `folders` 过滤、`localFileEntries` 文件条目）与远端目录（SFTP/FileBox，`remoteEntries` 来自 browse API）。现状只有浏览，没有过滤与手动输入。

要求（仅前端）：
1. picker 弹窗增加**过滤输入框**（放在 picker-toolbar 区域）：按名称（文件夹名/文件名）过滤当前目录列表。实现：新增 ref 如 `pickerFilter`；模板中列表渲染前用 computed 过滤——本地：`fileboxFoldersAt(picker.path).filter(f => f.name.includes(pickerFilter))` 与文件条目同理；远端：`remoteEntries.filter(e => e.name.includes(pickerFilter))`。过滤只作用于已加载的当前目录列表（不需要重新请求）。
2. 增加**手动输入目录全路径**区域：一个文本输入框（默认值 = 当前 picker.path）+ "确认"按钮；点击确认后把输入值赋给 `taskForm[${picker.target}Path]`（即选择该路径，source 侧同步 sourceKind='directory'），并关闭 picker。远端路径建议先调用一次 browse 校验该路径存在（成功则选中关闭；失败提示 formError 并留在弹窗，如 `sync.invalidRemotePath`），本地路径直接选中（不做存在性强制校验，因为本地目录可能尚未在 folders 中加载）。
3. i18n 三语新增键：`sync.filterPlaceholder`（过滤当前目录…/Filter current directory…/篩選目前目錄…）、`sync.enterPath`（手动输入路径/Enter a full path/手動輸入路徑）、`sync.confirmPath`（确认/Confirm/確認）、`sync.invalidRemotePath`（远端路径不存在/Remote path not found/遠端路徑不存在）。

## 需求 B：同步任务列表显示目标主机

背景：`web/src/views/SyncView.vue` 任务表（line 9 附近）"源 → 目标"列只显示 `sourcePath → targetPath`，目标系统的 host/url 未在任务行展示。`systems` 列表已加载（含 `item.host`、`item.url`、`item.port`、`item.kind`、`item.name`），任务有 `remoteSystemId`。

要求（仅前端）：
1. 任务表"目标"侧路径下方（或系统名小字处，样式复用现有 `sync-subline`）显示目标主机信息：
   - SFTP（kind='sftp'）：`host:port`（如 `sftp.example:22`；port 为 22 或缺失时只显示 host 亦可，但建议显示 port）。
   - filebox（kind='filebox'）：URL 主机部分 `new URL(item.url).host`；URL 解析失败则显示原始 `item.url`。
   - 找不到系统或 host/url 为空：显示系统名兜底（现有 `systemName()`）。
2. 新增 helper 函数如 `systemTargetLabel(system)`（返回上述字符串），并在任务行渲染。可加 i18n 前缀键 `sync.targetHost`（目标/→）按需，或直接拼接（推荐直接拼接，减少 i18n 键）。

## 验收

1. picker 弹窗可输入过滤词即时过滤当前目录/文件列表；可手动输入完整路径并确认选中（远端路径不存在时给出错误提示且不关闭弹窗）。
2. 任务列表每行目标侧能明确看到目标主机/IP/域名（SFTP=host:port，filebox=URL 主机），无数据时优雅兜底。
3. 三语 i18n 键完整。
4. `cd web && npm run build` 通过。
5. 不触碰：后端、其它视图、router。

请实施并简述每处改动。
