# CODEX_TASK_v029.md — v029 任务（清空能力迁入用户管理 + 删除用户可选保留文件）

- 起点：git HEAD=`607209f`（v027）+ 未提交的 v028 改动（工作区）；服务运行 v028（127.0.0.1:18080，pid 9572）。
- 来源：用户 2026-09-12 需求——① 把"管理员清空其他用户的全部文件"从文件库移到 **系统设置 → 用户管理**；② **删除用户时提示是否删除该用户全部文件（默认删除）**。
- **本文件先交付评审，确认两个决策点后再执行。**

## 决策确认（用户 2026-09-12）
1. 清空入口：**仅按用户清空**（移除全库清空）。
2. 删除用户保留文件：**转入独立 id 的回收站目录，按原用户名建子目录展示**。

## 执行进度（进行中）

**已完成（后端，全量测试全绿）**
- `store.DeleteUser(ctx, id, keepFiles)`：`keepFiles=true` 时把 ready 文件迁入回收站——归属保留账户 `users.id=0`（`RecycleOwnerID`/`RecycleOwnerUsername`，禁用登录、零配额，`store.Open` 的迁移阶段自动创建），存储路径改写为 `files/0/<原用户名>/<原相对路径>`，重名自动加序号；磁盘先同卷 rename（失败回滚），再更新记录；`keepFiles=false` 保持原行为。
- 两种模式都会 `DELETE FROM shares WHERE created_by = ?`，修复此前"删除用户后分享记录残留（`shares.created_by` 无外键）"的孤儿问题。
- `users` 列表与统计排除保留账户（`id <> 0`）。
- 新端点（均 `requireAdmin`）：
  - `POST /api/admin/users/{id}/clear-files`：按用户清空（复用 v027 二级限速 + 二次认证；`diskMode` 支持仅删空目录/整树；审计 `target=user:<name>`）。
  - `GET /api/admin/recycle`：回收站列表（含 `recycleUser`/`relativePath`）。
  - `DELETE /api/admin/recycle/{id}`：永久删除单个回收站文件。
  - `POST /api/admin/recycle/purge`：清空回收站。
- `POST /api/files/clear-all` 移除全库语义：传入 `scope != own` 一律 400 `SCOPE_UNSUPPORTED`。
- 测试：新增 `internal/httpapi/v029_test.go`（按用户清空权限/二次认证/只影响目标、保留文件的回收站迁移与列表/删除、默认删除分支）；`v028_test.go` 的全库用例改为 `TestClearAllGlobalScopeRemoved`；`cmd/filebox/main_test.go` 的备份恢复用户计数排除保留账户。`go test ./...` 全绿。
- 前端 `FilesView.vue`：清空弹窗移除"所有用户的文件（管理员）"范围单选，确认按钮与提交体同步收敛（无悬挂引用）。

**已完成（前端 + 构建 + 部署 + 验证）**
- `AdminView.vue`：用户行新增「清空文件」按钮（弹窗含二次认证与磁盘处理单选）；删除用户改为确认弹窗并含「同时删除该用户全部文件」复选框（**默认勾选**）；新增「回收站」面板（按原用户名分组、单文件永久删除、清空回收站、刷新）。
- 三语 i18n 新增键（`admin.clearUserFiles*`、`admin.deleteUser*`、`admin.recycle*`、`confirm.purgeRecycle`、`notice.userFilesCleared/userDeletedRecycled/recycleDeleted/recyclePurged`）与 `recycle-*` 样式；`node --check` 通过、键位三语齐备。
- 前端按 v028 决策 3b 构建（构建前临时还原 `transferFlow.js`、构建后还原工作区），`internal/webassets/dist` 同步；Go 二进制重建为 **v029** 并部署（`filebox-v029.exe`，127.0.0.1:18080）。
- 在线验证（一次性用户）：按用户清空 → 200（files=2/shares=1/disk=empty-dirs），目标用户清空、**其他用户文件不受影响**、非管理员 403；默认删除用户 → 回收站为空；`keepFiles=true` 删除 → 回收站出现 `recycleUser=v29c`、`relativePath=docs/keep.txt`，管理员可下载（200），单文件删除与 `purge` 均 200；保留账户不出现在用户列表；测试账号已清理，演示数据未受影响。

## 一、现状（已核实）

| 项 | 现状 |
|---|---|
| 清空能力位置 | 文件库「清空全部文件」弹窗内含 `scope` 单选：仅我的文件 / **所有用户的文件（管理员）**（v028 新增）；`POST /api/files/clear-all {scope:"all"}` 作用于**全库** |
| 删除用户 | `DELETE /api/admin/users/{id}` → `store.DeleteUser`（`store.go:1488`）删除 users 行：`files` / `folders` / `upload_tasks` / `share_groups` / `upload_collections` 由外键 `ON DELETE CASCADE` 连带删除，返回 `storage_path` 列表 → handler 物理删除文件并 `RemoveAll files/<uid>`。**没有"是否删除文件"的选择** |
| 已知缺陷 | `shares.created_by` 无外键（`store.go:665`），且 `shares.file_id` 有 CASCADE → 删除用户后**分享记录残留**（引用已删文件，成为不可见的"内容缺失"孤儿记录）；`share_groups.created_by` 有 CASCADE ✅ |
| 用户管理界面 | `web/src/views/AdminView.vue:20` 用户表（行内操作按钮）、`:45` 编辑弹窗 |

## 二、设计（建议）

1. **后端新增按用户清空端点**：`POST /api/admin/users/{id}/clear-files`
   - 仅管理员；复用 v027 的二级限速与二次认证（密码或 TOTP，`clearFilesReauth`），失败计入 `ip_failures`。
   - 复用 `store.ClearFiles(ctx, userID, false)`：清理该用户 ready 文件、目录记录、未完成任务、软撤销其分享、重置其配额；HTTP 层删物理文件与 `tmp/<taskID>`，按 `diskMode` 回收磁盘。
   - 返回明细 `{files, tasks, shares, diskMode}`；审计 `action=clear_all target=user:<username>`。
2. **文件库清空弹窗收敛**：移除「所有用户的文件（管理员）」选项与 `scope` 传参（恢复为仅本人）；全库清空能力移出文件库。
3. **用户管理新增操作**：
   - 每行「清空文件」按钮 → 弹窗（二次认证 + 磁盘处理单选：仅删空目录 / 删除整个存储目录）→ 调用新端点。
   - 可选工具栏「清空所有用户文件」（对应"原全库能力"，高危，需二次认证 + 强确认）——见**决策点 1**。
4. **删除用户可选保留文件**（默认删除）：删除弹窗新增复选框「同时删除该用户全部文件」（**默认勾选**）。
   - 勾选（默认）：现有行为（记录级联删除 + 物理删除 + 目录树删除），并**同时清理其分享记录**（修复孤儿）。
   - 不勾选：需保留文件（`files.user_id` 有 CASCADE，无法原地保留）——见**决策点 2**。
5. **附带修复**：删除用户事务内一并删除该用户的 `shares` 记录（消除孤儿分享）。

## 三、决策点（确认后执行）

1. **是否保留"清空所有用户文件"全局入口**：
   - (A) 只在用户管理中提供**按用户**清空（去掉全局清空；更安全，推荐）；
   - (B) 按用户清空 + 工具栏「清空所有用户文件」（保留原能力，置于用户管理）；
   - (C) 仅保留全局「清空所有用户文件」，不做按用户（不推荐）。
2. **删除用户时选择"不删除文件"，文件如何保留**：
   - (a) **转移给执行删除的管理员**（改写 `storage_path` 前缀 + 物理移动到 `files/<admin>/`，复用目录锁与路径改写逻辑；管理员可在自己文件库继续管理，推荐）；
   - (b) 移入独立「孤立文件」占位账户（新增系统账号，文件集中可见，便于后续清理）；
   - (c) 仅保留磁盘文件、删除数据库记录（文件在界面不可见，仅磁盘留存；不推荐，易造成空间与合规问题）。

## 四、验证计划（确认后）

- Go 单测：①非管理员访问新端点 → 403；②二次认证失败 → 401、连续失败 → 429（复用 v027 桶）；③按用户清空后该用户文件/目录/任务/分享全清且**其他用户不受影响**；④删除用户默认分支（记录+物理+目录树消失）与保留分支（文件按策略可见/可达，且原用户不再存在）；⑤孤儿分享清理。
- 前端：文件库弹窗不再有「所有用户的文件」；用户管理出现「清空文件」与删除弹窗复选框；`npm run build`（仍按 v028 决策 3b 处理 `transferFlow.js`）+ node 测试。
- 在线：按上述场景逐项实测（仅用一次性用户，不触碰演示数据；破坏性全库清空不在共享实例执行）。

## 五、影响文件（预估）

- 后端：`internal/httpapi/server.go`（新端点 + 删除分支 + 审计）、`internal/store/store.go`（删除用户保留/转移分支、孤儿分享清理）。
- 前端：`web/src/views/AdminView.vue`（用户行操作、清空弹窗、删除弹窗复选框）、`web/src/views/FilesView.vue`（移除 scope=all）、`web/src/api.js`/`i18n.js`/`styles.css`。
- 文档：`CHANGELOG.md`、`STATE.md`、本文件执行结果。
