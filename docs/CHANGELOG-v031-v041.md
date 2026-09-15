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

| v042.4 | 审计 | **日志审计第 1 批**：`collection.go` 6 处与用户上传直接相关的失败点改为结构化（`collection_create`/`collection_revoke`/`collection_disk_check`/`collection_upload owner_load|task_create|chunk_save`），全部保留 `err=…` 原始错误 | `internal/httpapi/collection.go` | 源码含新字段；`go test ./...`；日志可用 `result=failure` 检索 | 已部署 |
| v042.5 | 审计 | **第 2 批**：`collection.go` 其余读写失败点 13 处（列表/取单个/更新/元数据/加载收集者/建立目录/显示名/秒传匹配等）→ `result=failure err=%v`，仅改格式串、参数不变 | `internal/httpapi/collection.go` | 同上 | 已部署 |
| v042.6 | 审计 | **第 3 批（规则 1）**：`log.Printf("<说明>: %v", errVar)` → `"<说明> result=failure err=%v"`，一次 127 处（`server.go` 79 / `sync.go` 25 / `share_group.go` 13 / `collection.go` 10）；含编码自检与构建失败回退 | `internal/httpapi/{server,sync,share_group,collection}.go` | 编码自检 4/4（`U+FFFD=0`、中文字符数未丢失）；`go build`、`go test ./...`、8 个 node 测试 | 已部署 |
| v042.7 | 审计 | **收尾（规则 2）**：多占位符 + 末尾 error 形态 25 处（清除文件/临时目录删除、回收站文件删除、批量分享回滚、批量删除目录树、同步凭据解密与上传回滚、TOTP 密钥迁移等） | 同上 + `sync_filebox.go`、`recycle_purge.go` | 编码自检 6/6；构建与测试全绿 | 已部署 |
| — | 审计结果 | **累计 171 处**失败日志结构化，`result=failure` 行达 **199 行**；剩余 6 处为设计上的信息类（同步跳过、`released N stale reservation(s)`、host-key 首信任等），保留原样 | — | `grep 'result=failure'` 可直接定位失败上下文与底层错误 | — |

| v043 | 功能 | **客户端校验阈值配置化（T3）**：`FILEBOX_HASH_DIRECT_LIMIT`（原生校验上限，默认 256MiB）与 `FILEBOX_CLIENT_HASH_MAX`（跳过客户端哈希上限，默认 1GiB）由只读前端常量改为管理员设置项，含取值夹取、内存代价提示与三语文案；生效值经公开 `/api/brand` 下发，未登录的收集页上传者也生效 | `internal/store/store.go`、`internal/httpapi/server.go`、`web/src/hashPolicy.js`、`web/src/brand.js`、`web/src/api.js`、`web/src/views/AdminView.vue`、`web/src/i18n.js` | `go test ./...` + 8 个 node 测试全绿；`internal/store/hash_limits_test.go`、`internal/httpapi/hash_limits_settings_test.go`；隔离实例实测非法值 400、改值后 `/api/brand` 同步 | 已部署 |

| v044 | 修复 | **审计日志不再露出英文码**：日志页「操作类型 / 失败原因」对未录入的码原样显示（用户报 `clear_all`、`recycle_move`、`recycle_purge`、`reauth_failed`）→ 补 37 个三语键，映射抽成 `web/src/logLabels.js` 单一来源（日志页与分享下载日志共用），未知码回退为「其他操作（码）/其他原因（码）」 | `web/src/logLabels.js`（新增）、`web/src/views/LogsView.vue`、`web/src/views/SharesView.vue`、`web/src/i18n.js` | `node --test web/tests/logLabels.test.mjs`（6 例）；只读核对演示库 `audit_logs` 的 23 种 action + 43 种 reason 全部有译；产物含三语新标签 | 已部署 |

| v044.1 | 修复 | **日志页列名改「原因 / 说明」**：`logs.failureReason` → `logs.reasonColumn`（三语「原因 / 说明」「原因 / 說明」「Reason / note」）；`reason` 在成功记录里也描述"怎么做的"，旧列名与内容不符，成功行继续显示该值 | `web/src/i18n.js`、`web/src/views/LogsView.vue`、`web/tests/logLabels.test.mjs` | 测试新增 `logs.reasonColumn` 三语存在断言；产物核对：三语表头均在、旧文案已消失 | 已部署 |

## v043：客户端校验阈值配置化（T3）

- **改造前**：两个阈值只存在于前端 `globalThis`——`web/src/api.js` 读 `FILEBOX_HASH_DIRECT_LIMIT`（默认 256MiB，≤ 该值走原生 WebCrypto），`web/src/hashPolicy.js` 读 `FILEBOX_CLIENT_HASH_MAX`（默认 1GiB，> 该值整个跳过客户端哈希）；服务端没有对应设置，管理员改不了。
- **实现**：① `store.LogSettings` 增 `hashDirectLimitBytes`/`hashClientLimitBytes`，随 `GET/PUT /api/admin/settings` 读写，`migrateSettings` 写入默认值；② `store.ClampHashLimits` 统一夹取——非正数回默认值、下限 1MiB、直算上限 2GiB（原生路径会把整个文件读进一个 `ArrayBuffer`）、总上限 64GiB、且直算上限不高于总上限；读路径夹取兜住旧库或手改值，`validateLogSettings` 对越界与「直算 > 总上限」返回 400；③ 公开 `GET /api/brand` 增加两个字段（与 `maxFileSize` 同通道，收集页未登录上传者也能拿到）；④ `web/src/hashPolicy.js` 成为唯一来源（默认值、`hashDirectLimit()`、`clientHashLimit()`、`applyServerHashLimits()`），`brand.js` 的 `applyBrand` 在每次加载公开配置时应用，`api.js` 改用 `hashDirectLimit()`；⑤ 管理员「系统设置」新增阈值面板（MiB 输入、内存代价提示、保存前夹取）。
- **默认值与旧硬编码完全一致**，所以升级本身不改变任何前台校验行为，除非管理员主动调整。
- **验证**：隔离实例（独立数据目录 + 端口 18099，管理员口令由 `--admin-pass` 指定，全程不触碰演示数据）实测——默认 `268435456 / 1073741824`；`direct > client` → 400「校验直算上限不能大于客户端哈希上限」；`client` 超上限 → 400「客户端哈希上限无效」；改成 512MiB / 4GiB → 200，且未登录 `/api/brand` 立即返回 `536870912 / 4294967296`。
- **已知边界**：阈值不参与 `index.html` 的缓存策略（沿用 v033 的 `no-cache`）；阈值变更对**已在传输中的上传**不回溯生效，下一次校验才读取新值。

## v044：审计日志标签本地化（操作类型 / 失败原因）

- **问题**：日志页的「操作类型」是 `labels[value] || value`、「失败原因」是 `keys[value] ? t(...) : value`——任何后端写了、前端未录入的码都直接显示英文原文（用户报 `clear_all`、`recycle_move`、`recycle_purge`、`reauth_failed`，筛选下拉同样受影响，它也用 `actionLabel`）。
- **盘点方法**：源码里 72 处 `s.recordAudit` / `s.recordShareAudit` 的字符串字面量 ∪ 演示库 `audit_logs` 的实测值，取并集再与前端键求差集。**注意 `recordShareAudit` 比 `recordAudit` 多一个 `shareOwnerID` 参数**，解析时 action/reason 的参数位不同（否则会把 username 当成 action）。
- **差集结果**：缺 8 个操作类型键（`delete` 已有 `logs.delete` 可复用，另加 `clear_all`/`recycle_move`/`recycle_purge`/`share_preview`/`sync_run`/`sync_host_key`/`sync_host_key_update`）与 29 个原因/回退键（含 `reauth_failed`、`scope_forbidden`、`success`、`create`/`revoke`/`update`/`extend`/`batch_group`/`instant`/`collection_*` 等"成功记录里的操作说明"）。
- **实现**：新增 `web/src/logLabels.js`，集中 `ACTION_LABEL_KEYS`/`REASON_LABEL_KEYS` 与 `actionLabel`/`reasonLabel`，`LogsView` 与 `SharesView`（分享下载日志）共用，两处不再各写一份；未知码回退为「其他操作（{code}）」/「其他原因（{code}）」——仍带码便于排查，但明确标注为待翻译，不再是裸英文。
- **回归保护**：`web/tests/logLabels.test.mjs` 把「源码 ∪ 演示库」的码清单固化为夹具，断言：每个后端码都有映射、每个映射键在三语字典中都存在、三语键数一致、已知码不退化成原文码、未知码回退包含码、空值显示 `-`。
- **列名调整（v044.1，用户确认）**：`logs.failureReason`（失败原因）改名为 `logs.reasonColumn`，文案「原因 / 说明」/「原因 / 說明」/「Reason / note」——因为 `reason` 在成功记录里也描述"这次是怎么做的"，旧列名与内容不符；**成功行照旧显示该值**，没有改成 `-`。分享页的下载日志是行式列表（无表头），不受影响。
- **验证**：`go test ./...` 与 9 个 node 测试文件（45 例）全绿；用 `mode=ro` 的临时只读程序（用完即删）比对演示库：**23 种 action + 43 种 reason 全部有译，无未覆盖项**；部署产物 `index-DPM-rJxm.js`（v044）与 `index-CdcoZrlT.js`（v044.1）中确认含中/繁/英标签，且旧文案「失败原因」已从产物中消失。

## 工具纪律（v042.1 事故后的硬性约定）

1. **中文/多语言文件只用 Edit / Write 工具修改**，禁止 PowerShell 文本替换后写回（会整份损坏）。
2. **交给 PowerShell 执行的 `.ps1` 必须纯 ASCII**（PS 5.1 按 ANSI 读取脚本，中文字面量会导致解析失败）。
3. 校验中文时用 **Node 脚本按 UTF-8 读取**，只输出布尔/计数（U+FFFD、乱码特征、已知键值是否存在）。

## 待办（尚未实施）

| 编号 | 事项 | 要点 | 依赖 |
| --- | --- | --- | --- |
| T4 | 窄屏导航折叠 | 宽度不足时收起为「更多」菜单，复用移动菜单样式，配纯函数单测与三语 `nav.more` | 无 |

> T1（日志审计）已在 v042.4–v042.7 完成（171 处结构化），T2（失败文案跟随语言）已在 v042–v042.2 完成，T3（哈希阈值配置化）已在 v043 完成并部署；此表仅剩 T4。

## 仍待用户侧确认

- 回收站页面观感（导航图标与页签、分组列表、移动弹窗、清空认证弹窗）。
- #5 目录上传聚合行、#8 传输进度明细的界面观感。
- 浏览器内 1 GiB 文件的校验耗时与 Performance 面板长任务。
