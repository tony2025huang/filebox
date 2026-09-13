# FileBox 会话交接（v030 进行中）— 2026-09-12

> 用途：上下文压缩/换会话后据此无损续做。包含环境、已完成项、剩余项的**实现落点**、验证方式与踩坑。

## 一、环境与部署

- 仓库：`C:\Users\huangcp\dsh-project\filebox`（git 仓库，HEAD=`607209f` v027；工作区含 **v028/v029/v030 全部未提交改动**）。
- 未合并：`web/src/transferFlow.js` 有 28 行 v026 未提交改动（generation/batching/completionTimestamp 助手）。**用户决策：暂不合并**。因此构建前端必须用"**3b 流程**"：
  ```powershell
  Copy-Item 'web\src\transferFlow.js' (Join-Path $env:TEMP 'transferFlow.worktree.js') -Force
  git checkout -- web/src/transferFlow.js      # 还原为 HEAD
  npm --prefix web run build
  go run ./scripts/sync-web.go                 # web/dist → internal/webassets/dist
  Copy-Item (Join-Path $env:TEMP 'transferFlow.worktree.js') 'web\src\transferFlow.js' -Force   # 还原工作区
  ```
- 部署：二进制 → `filebox-demo\filebox.exe`（并留存 `filebox-v0XX.exe`），启动命令：
  ```powershell
  filebox.exe --addr=127.0.0.1:18080 --data=filebox-demo\data --log-enabled=true --log-dir=filebox-demo\logs
  ```
  数据目录 `C:\Users\huangcp\dsh-project\filebox-demo\data`；密钥在 `data/config/secrets.json`。
- 账号：管理员 admin、演示用户 user（id=2，1300+ 文件）、测试账号 test、check（管理员）。**口令不入库**，从部署控制台或 `filebox-demo\data\config\secrets.json` 获取；**勿动演示数据**。
- 工具：Go `C:\Users\huangcp\AppData\Local\Programs\Go\go\bin`；node `C:\Users\huangcp\AppData\Local\Programs\nodejs`；git `C:\PortableGit\bin\git.exe`（不在 PATH）；便携 GCC `C:\Users\huangcp\.zcode\workspace\default\tools\w64devkit\bin\gcc.exe`（跑 `go test -race` 用：`$env:CC=<该路径>; $env:CGO_ENABLED=1`）。
- 已知工具坑：PS5.1 不能设 Range 头（用 curl.exe）；`Invoke-WebRequest` 非终止错误后 `$r` 保留旧值易误读；PowerShell `-Command` 里别用 `ForEach-Object`/`Select-Object` 跟管道混写（改用 `.ps1` 文件）；中文输出经重定向易变 UTF-16（用 `Out-File -Encoding utf8` 或直接读）。
- 浏览器（IAB）：**不支持文件选择/上传**，无法驱动真实上传；仅可做 UI 目视（登录、点表头排序、开弹窗）。

## 二、已完成（v028 → v030）

- **v028**：clear-all 语义（范围/磁盘模式/未完成任务/分享/空目录）、表头排序、去拖放、失败原因 hover、分享错误码区分（SHARE_NOT_FOUND/REVOKED/EXPIRED/CONTENT_MISSING）、收集元数据隐私。
- **v029**：`ClearFiles(ctx,userID,allUsers)`；**按用户清空**端点 `POST /api/admin/users/{id}/clear-files`（文件库移除全库 scope，`scope!=own` → 400 `SCOPE_UNSUPPORTED`）；**删除用户可选保留文件** → 回收站（保留账户 `users.id=0`，路径 `files/0/<用户名>/<相对路径>`，创建于迁移 `EnsureRecycleOwner`，列表/统计隐藏）；删除时清理 `shares`（`created_by` 无外键的孤儿问题）；回收站接口 `GET /api/admin/recycle`、`DELETE /api/admin/recycle/{id}`、`POST /api/admin/recycle/purge`；AdminView 用户行「清空文件」+ 删除弹窗复选框（默认勾选删除）+ 回收站面板 + 三语 i18n。
- **v030 已完成**：
  1. **#1 上传未进「已完成」**：完成态契约单源化到 `web/src/transferStatus.js`（`isUploadTerminalState`/`uploadTerminalKind`/`isDownloadTerminalState`），`FilesView` 的 `uploadsActive/uploadsDone/isUploadComplete` 已切换；node 契约测试 `web/tests/transferStatus.test.mjs`。**用户已复测通过**。
  2. **#3**：实测判定**非缺陷**（跨页全量清理 13 文件 + `files/<uid>` 已删；观感来自管理员全库视图/批量删除语义）。遗留体验项：清空成功后分页重置回第 1 页（未做）。
  3. **#10**：`shareGroupMeta` 增加 `createdByName`；`BatchShareView` 优先显示用户名。已在线验证。
  4. **#11**：`GET /api/files` 支持管理员带 `userId=` 按指定用户列出（不带=全库）；`SharesView` 编辑/移除成员时按 `group.createdBy` 取候选。已在线验证。
  5. **#9 第一批**：`store.ExpandFolderFileIDs`（目录递归展开为 ready 文件）；`batch-share` 与 `batch-share-group` 均接受 `folderIds`；前端聚合分享提交 `folderIds`、仅选目录也可分享、空目录 → 400 `FOLDER_EMPTY`。**未做**：分享页/聚合分享页**完整可折叠目录树**渲染。
  6. **校验性能**：`web/src/api.js` 的 `computeFileSHA256` 原生 WebCrypto 上限 32MiB → **256MiB**（`globalThis.FILEBOX_HASH_DIRECT_LIMIT` 可覆盖）。**非限速**：根因是 >32MiB 回退手写纯 JS SHA-256（~5–20MB/s）；上传阶段为分片裸传故 80MB/s。

## 三、剩余（v030 未完成）

- **#9（剩余）**：聚合分享页展示**完整可折叠目录树**。后端 `GET /api/shared-groups/{token}/meta` 的 `files[]` 目前只含 `fileId/name/size/mime/createdAt`，**需补 `relativePath`**（store.ListShareGroupFiles 已有 `storage_path`，可派生）再由 `BatchShareView` 渲染树。
- **#7 收集落盘目录**（用户决策：ASCII `collections`，界面跟随语言；**显式 CLI 迁移**，不在启动期迁移）：`internal/httpapi/collection.go` 现用 `storageDir = files/<uid>/uploads/<token>`；改为 `files/<uid>/collections/<集合名>-<token前8位>`，并新增 `filebox admin migrate-collection-dirs` 把旧 `uploads/<token>` 迁到新布局（同时改写 `files.storage_path` 与 `folders.path`）。
- **#5 目录上传待上传区聚合**（允许点开明细）：`web/src/views/UploadView.vue` 队列 `v-for="item in queue"` 逐文件渲染；目录来源的文件需聚合为**一条目录条目**（名称/文件数/总大小/聚合进度），明细进"传输进度"视图。
- **#8 「我的收集」传输进度展开页**：逐文件进度、实时速率（EMA，与文件库一致）、失败原因（复用 v028 的失败原因展示）。
- **UI 批次**：#2 表头排序间距≥6px + 图标化 + hover（`styles.css` 的 `.sortable-col`）；#4 收集密码页样式与全站统一（`UploadView` 密码表单）；#6 收集页进度条溢出（`.public-queue-row .progress-track` 盒模型）；#12「新的下载上限（0=不限）」不换行（`SharesView` 分组编辑表单，`white-space: nowrap` + flex）；#3 体验项（清空后分页重置）。

## 四、质量门与常用命令

```powershell
go test -count=1 ./...                       # 后端全量（v029 起含保留账户 id=0，注意排除）
foreach ($f in (Get-ChildItem 'web\tests' -Filter '*.test.mjs')) { node $f.FullName }   # 4 个 node 测试
npm --prefix web run build                   # 必须配合上面的 3b 流程
go run ./scripts/sync-web.go                 # 同步内嵌前端
```
- 后端测试：`internal/httpapi/v028_test.go`、`v029_test.go`、`v030` 相关（#10/#11/#9 暂未加 Go 用例，可补：目录展开、createdByName、userId 作用域）。
- i18n 增删一律用 Node 脚本对 `web/src/i18n.js` 做**三语同步**，并 `node --check` 校验（历史坑：插入键必须带**前置逗号**；文案避免 ASCII 撇号 `'`；删键要按字符扫描处理 `\'` 转义）。
