# FileBox 变更版本汇总（v031 – v042）

> 用途：把本阶段（2026-09-12 ~ 09-13）的开发与修复按版本编入本文档，供后续逐项校验。
> 逐条详细说明另见 `docs/requirements/CHANGELOG.md`；任务书见 `docs/CODEX_TASK_v0XX.md`。
> 部署环境：`filebox-demo\filebox.exe`，`http://127.0.0.1:18080`，数据目录 `filebox-demo\data`。
> 状态：全部已提交并推送（远端 `origin/main`），且已部署验证。

| 版本 | 类型 | 变更摘要 | 关键文件 | 校验方式 | 状态 |
| --- | --- | --- | --- | --- | --- |
| v031 | 性能 | 校验改为 Worker 全覆盖：≤256MiB 原生 WebCrypto、>256MiB Worker 内 WASM 流式；新增共享兜底实现；CSP 增加 `'wasm-unsafe-eval'` | `web/src/hashWorker.js`、`web/src/sha256Fallback.js`、`web/src/api.js`、`internal/httpapi/server.go`(CSP) | `node --test web/tests/*.mjs`；`node web/tests/hashCeilings.mjs`；线上响应头含 `wasm-unsafe-eval` | 已部署；浏览器内 1GiB 实测待用户补充 |
| v032 | 可观测性 | 校验上报 engine（native/wasm/js/js-main）与 read/hash 耗时；≥64MiB 打印 warn 级日志；Worker 失败快速降级（看门狗） | `web/src/api.js`、`web/src/hashWorker.js`、`web/tests/hashWorker.test.mjs` | `node --test web/tests/hashWorker.test.mjs` | 已部署 |
| v033 | 修复 | 部署后旧标签页引用已删除的哈希资源会被 SPA 回退成 `200 text/html`，导致模块脚本 MIME 失败并静默降级 → 资源形态路径现在返回 404；`index.html` 设 `no-cache`；`assets/*` 设 `immutable` | `internal/httpapi/server.go` | 请求已删除的 `assets/*` 应得 404；`/` 响应头 `Cache-Control: no-cache`；`/files` 仍 200 text/html | 已部署 |
| v034 / v035 | 可观测性（临时） | 因控制台不可见，临时在页面右下角渲染诊断浮层与实时速率 | `web/src/api.js` | ——（v041 已移除） | 已移除 |
| v036 | 功能 | >1GiB 的文件跳过客户端哈希，改由服务端组装时计算并在完成响应回传 `sha256`；阈值可经 `globalThis.FILEBOX_CLIENT_HASH_MAX` 覆盖 | `web/src/hashPolicy.js`、`web/src/api.js`、`web/src/views/UploadView.vue` | `node --test web/tests/hashPolicy.test.mjs`；≤1GiB 文件行为不变 | 已部署 |
| v036.1 | 修复 | 跳过哈希后仍调用按哈希查重的 `/api/files/check`，被 400「文件校验值缺失」判为失败；现仅在有哈希时调用 | `web/src/views/FilesView.vue` | 上传 >1GiB 文件：不再出现该错误，直接进入上传 | 已部署 |
| v037 | 修复 | 「已用远低于配额却提示配额不足」：配额是预留模型，被放弃的上传任务会一直占额度，而周期清理按 `created_at < 24h` 才回收 → 上传前按需回收"创建超过 6 小时仍未完成"的预留 | `internal/httpapi/upload_reservations.go`、`server.go`、`upload_reservations_test.go` | `go test ./internal/httpapi -run TestUploadInit`；日志应出现 `released N stale upload reservation(s)` | 已部署 |
| v038 | 修复 | 落盘由单次 `os.Rename` 改为退避重试 + 复制兜底（Windows 上刚写完的大文件可能被安全软件短暂持有，曾两次 `save_failed`）；落盘终局失败立即释放任务与预留；补齐 `upload_place`/`upload_release` 结构化日志 | `internal/httpapi/upload_place.go`、`server.go`、`upload_place_test.go` | `go test ./internal/httpapi -run TestPlaceUploadFile`；日志 `upload_place result=… via=…` | 已部署 |
| v039 | 变更 | 配额错误（`QUOTA_EXCEEDED`）新增 `pendingBytes`（未完成上传占用的预留），便于界面说明"为何配额不足" | `internal/store/store.go`、`internal/httpapi/server.go` | 触发一次配额拒绝，响应数据含 `pendingBytes` | 已部署（界面文案待做） |
| v040 | 修复 | 清空后文件库仍显示 1081 个文件：管理员"全库"列表未排除回收站保留账户 `users.id=0` → 现加 `AND user_id <> 0`（回收站视图仍可见） | `internal/store/store.go`、`recycle_visibility_test.go` | `go test ./internal/httpapi -run TestAdminFileListExcludesRecycleOwner`；`GET /api/files` total 应为 0 且 `/api/admin/recycle` 仍列出 | 已部署 |
| v041 | 功能 | 回收站独立化：新增 `/recycle` 页面（分组查看、多选、**移动到指定用户+目录**、单项永久删除、下载、**清空需二次认证**）；后端新增 `POST /api/admin/recycle/move`，purge 复用 clear-all 的两级限速与密码/TOTP；导航新增管理员专属入口（含图标）与页签归属；用户管理里的回收站面板与死代码移除；临时诊断浮层移除 | `internal/store/recycle_move.go`、`internal/httpapi/recycle_move.go`、`recycle_purge.go`、`web/src/views/RecycleView.vue`、`router.js`、`topbarNav.js`、`AuthenticatedTopbar.vue`、`i18n.js`、`AdminView.vue` | `go test ./internal/httpapi -run 'TestMoveRecycleFiles|TestPurgeRecycleRequiresReauth'`；线上实测：移动成功、单项删除成功、无凭据 purge 返回 401；日志 `recycle_move`/`recycle_delete`/`recycle_purge` | 已部署 |

## 本阶段已落地的关键结论（供校验时对照）

1. **6.04 GiB 上传端到端 81 秒**（≈80 MB/s；此前为 370s 客户端校验 + 90s 上传 ≈ 460s）；服务端计算出的 `sha256` 与历史多次一致。
2. **校验慢的根因不是磁盘、不是 CPU、不是限速**：Worker 内读取 385MB/s、原生 WebCrypto 934MB/s、JS JIT 0.08s 均正常；同一份 WASM 在 Node 188MB/s、在其浏览器 Worker 内仅 21.5MB/s（浏览器侧 WASM 执行差异）。
3. **`save_failed` 的底层错误此前只进 stderr**（未落结构化日志），现已补 `upload_place` 日志并持续重定向 stderr 到 `filebox-demo\logs\stderr.log`。

| v042 | 修复 | **失败文案跟随用户语言（第一批）**：`codeKeys` 补齐语义已有对应键的码（`SHARE_DENIED`/`INVALID_USER_ID`/`COLLECTION_UNAUTHORIZED`/`COLLECTION_TASK_QUEUED`/`COLLECTION_MAX_UPLOADS_BELOW_USED`）；新增守卫"带稳定码但未映射 → 返回已翻译通用文案"，不再透出裸码或仅中文后端消息；`QUOTA_EXCEEDED` 在 `pendingBytes>0` 时改用 `error.quotaExceededPending`，把"已用/未完成占用/配额/本次文件"一次说清 | `web/src/api.js`、`web/src/i18n.js` | 触发配额拒绝并切换语言观察文案；产物含 `quotaExceededPending` | 已部署 |
| v042.1 | 修复 | **i18n 字典被误编码导致页面菜单乱码**：v042 用 PowerShell 文本替换写回 `i18n.js`，PS 5.1 按 ANSI/GBK 解码后以 UTF-8 写出 → 整份字典二次编码（129,982→172,474 字节、157 处乱码特征、写入 BOM，既有键值全损）。改法：`git checkout 6eccb6e -- web/src/i18n.js` 恢复，再用 **Edit 工具**重加新键 | `web/src/i18n.js` | Node 健康检查：BOM=false、U+FFFD=0、乱码特征=0、`我的文件`/`回收站`/`系统设置` 均在 | 已部署 |
| v042.2 | 修复 | **失败文案第二批（本地化收口）**：为剩余差集补 9 个三语键（`error.reauthRateLimited`/`scopeUnsupported`/`invalidDiskMode`/`batchTooLarge`/`batchLimitExceeded`/`folderEmpty`/`folderNotEmpty`/`collectionRateLimited`、`sync.hostKeyUpdateConflict`），`codeKeys` 同步新增 10 条映射（含后端小写码 `task_not_found`）→ 约 40 个后端错误码全部有本地化出口 | `web/src/i18n.js`、`web/src/api.js` | 键值三语各 1 处；`node --check`；8 个 node 测试与 `go test ./...` | 已部署 |

## 工具纪律（v042.1 事故后的硬性约定）

1. **中文/多语言文件只用 Edit / Write 工具修改**，禁止 PowerShell 文本替换后写回（会整份损坏）。
2. **交给 PowerShell 执行的 `.ps1` 必须纯 ASCII**（PS 5.1 按 ANSI 读取脚本，中文字面量会导致解析失败）。
3. 校验中文时用 **Node 脚本按 UTF-8 读取**，只输出布尔/计数（U+FFFD、乱码特征、已知键值是否存在）。

## 待办（尚未实施）

| 编号 | 事项 | 要点 | 依赖 |
| --- | --- | --- | --- |
| T1 | 日志审计第 1 批 | 把写入/落盘类约 40–50 处 `log.Printf` 迁为 `logger.Event`（保留 `err=…`），纯调试类保留 | 无 |
| T2 | 失败文案跟随语言 | 盘点 `writeError/writeErrorData` 的 `code` 与前端 `localizeError.codeKeys` 差集，补三语键并加未知码回退 | 先做只读盘点 |
| T3 | 哈希阈值配置化 | 把 `FILEBOX_HASH_DIRECT_LIMIT`（及"跳过客户端哈希上限"）做成管理员设置项，含内存代价提示与取值夹取 | 无 |
| T4 | 窄屏导航折叠 | 宽度不足时收起为「更多」菜单，复用移动菜单样式，配纯函数单测与三语 `nav.more` | 无 |

## 仍待用户侧确认

- 回收站页面观感（导航图标与页签、分组列表、移动弹窗、清空认证弹窗）。
- #5 目录上传聚合行、#8 传输进度明细的界面观感。
- 浏览器内 1 GiB 文件的校验耗时与 Performance 面板长任务。
