# FileBox v015 独立逻辑复核报告（DSH 路线，前缀 D-xx）

- 复核对象：`C:\Users\huangcp\dsh-project\filebox`，git HEAD `93886b6`（工作树干净）
- 复核方式：独立逻辑走查（不依赖 codex 路线），全部关键疑点**实测复现**（本机起服务 `127.0.0.1:18086`，独立数据目录 `.test-data\review2\data`，UTF-8 字节请求体；验证后已停止进程并清理运行时数据，仅保留测试脚本与构建二进制）
- 分级口径：**严重**（数据丢失/安全绕过/可直接利用）、**中等**（明显错误行为/资源滥用/一致性破坏）、**轻微**（UX/口径/健壮性/工程卫生）
- 计数：严重 1、中等 6、轻微 10（D-01 ~ D-17）

---

## 一、复核范围与方法

1. **数据层全量**：`internal/store/store.go`（3084 行）、`internal/store/sync.go`、`internal/store/share_group.go` —— 逐行通读 schema、迁移、事务、配额核算、分享/收集计数、目录路径维护。
2. **HTTP 层**：`internal/httpapi/server.go`（4122 行，按路由分组）、`collection.go`、`share_group.go`、`sync.go`、`sync_filebox.go` —— 认证/授权、read-only 覆盖面、上传状态机、失败清理、匿名限速、日志脱敏。
3. **CLI 与运维**：`cmd/filebox/main.go`（1623 行）—— backup/restore 安全、migrate、locks、admin 子命令；`internal/srvlog`、`internal/diskusage`。
4. **前端**：`web/src` 全部视图与 `api.js`、`router.js` —— 路由守卫、上传状态机、批量操作、i18n 键扫描、XSS 面（grep v-html/innerHTML）。
5. **实证**（6 组脚本，均落在 `.test-data\review2\`）：
   - t1：秒传跨目录；t2 + Go 直连 store：删用户级联删除；
   - 备份 WAL 一致性（运行中备份→恢复→DB 内容比对）；
   - t3/t4：收集箱槽位燃烧、read-only 各写入口覆盖；
   - t5：分享下载先扣次后开文件；t6：配额预留无释放。

---

## 二、问题清单

### 严重（1）

**D-01 运行中执行 `admin backup` 产生不含 WAL 的空库归档，恢复后全量数据丢失（静默）**
- 位置：`cmd/filebox/main.go` `runAdminBackup`/`buildBackupArchive`（L672-864）、`runAdminRestore`（L884-1154）
- 根因：备份直接 `os.ReadFile(filebox.db)`，未先 `PRAGMA wal_checkpoint(TRUNCATE)`、未复制 `-wal/-shm`、未使用 SQLite Online Backup。服务运行中（`journal_mode=WAL`）主库文件可能仅 4096 字节，schema 与数据全在 `-wal`。CLI 只打印 "note: stop the FileBox service before backup"，不检测、不拒绝。
- 影响：恢复端对归档做 manifest SHA256 校验全部通过（归档自洽），然后激活——恢复出的库 **0 张表、0 用户、0 文件**，服务启动后重建空库。任何在服务运行期做的备份都是"看起来成功、实际全丢"的炸弹；`--force` 还会把原数据目录改名保留（`*.pre-restore-*`）后整体替换，运维若未保留旧目录即不可逆。
- 实证：上传 2 个文件后（live db：17 表 / 1 用户 / 2 文件），运行中备份→恢复：`restored/filebox.db` 4096 字节，`tables=0`、`users` 查询报 "no such table"。
- 修复建议：备份前对 `filebox.db` 执行 `PRAGMA wal_checkpoint(TRUNCATE)`（需要打开连接）或改用 modernc 的 Backup API；恢复前同样校验目标库可读且非空（`SELECT count(*) FROM sqlite_master`）再激活；至少检测同目录存在 `-wal` 且非空时拒绝执行并明确报错。

### 中等（6）

**D-02 登录用户秒传跨目录匹配：目标目录不出文件，前端却显示"上传完成"**
- 位置：`internal/httpapi/server.go` L1849（`checkInstantUpload` 用 `FindInstantMatch`，全库匹配）；`internal/store/store.go` L1608-1622 存在 `FindInstantMatchInDirectory` 但**仅收集箱在用**（`collection.go` L467）
- 根因：认证上传的秒传检查没有像收集箱那样限定目标目录。用户在 dir2 上传与 dir1 相同内容 → 返回 `instant:true` 及 dir1 的文件记录；前端（`FilesView.vue` startUpload）置 100%、提示"秒传成功"并刷新列表，但 dir2 下没有任何文件。
- 影响：文件静默落在错误目录；用户以为已上传成功。与收集箱行为（目录内秒传）不一致，属于明显的业务规则缺陷。
- 实证：dir1 上传 hello.txt 后，dir2 发起 `/api/files/check` → `instant=true fileId=1`，`GET /api/files?dir=dir2` → `total=0`。
- 修复建议：`checkInstantUpload` 改用 `FindInstantMatchInDirectory(userID, filepath.Join("files", uid, dir), …)`，与收集箱对齐；无 dir 时再回退全库或直接不秒传。

**D-03 收集箱上传次数在 upload-init 时即消耗且永不退还：token 持有者可用空 init 打满上限（0 文件送达）**
- 位置：`internal/store/store.go` `CreateCollectionUploadTask`（L2227-2277，槽位 + 配额在同一事务内递增）；`collection.go` init 路径（L490-521）
- 根因：`upload_count` 在任务创建时 +1，只有任务被清理（删除记录）时才释放槽位；但 `DeleteUploadTask` 只删任务行，**不扣回 upload_count**。废弃/失败任务（不传任何分片、不 complete）永久占用收集次数，owner 配额同样被预留 24h。
- 影响：拿到收集 token 的任何人可对 maxUploads=2 的收集发 2 个空 init → 收集变为 `limit_reached`、0 文件；同时把 owner 配额挂起 24 小时。收集箱对"占坑"攻击完全无防护。
- 实证：`maxUploads=2` 收集连续 2 次 upload-init（无分片）→ 第 3 次 `403 COLLECTION_LIMIT`；`meta` 显示 `uploadCount=2`、`status=limit_reached`，`files` 列表为空。
- 修复建议：槽位改为 complete 成功时消耗（init 只校验不递增），或任务废弃/过期清理时 `upload_count` 回退（`DELETE` 语句中 `UPDATE upload_collections SET upload_count = MAX(0, upload_count-1) WHERE id = task.collection_id`）；给收集增加"有效提交"口径。

**D-04 配额预留无释放路径：废弃 pending 任务锁定配额 24h，无取消 API**
- 位置：`internal/store/store.go` `CreateUploadTask`（L1425-1458，pending 求和预留）；`main.go` 清理器（L249-269，>24h 才删除）；前端仅有 pause/resume，无 cancel 端点
- 根因：upload-init 成功后配额即被 pending 预留；任务若被浏览器关闭/放弃，既不 complete 也不删除，需等小时级清理（>24h）。没有"取消任务"的 API，前端也没有释放入口。
- 影响：用户连续废弃几个大上传后，在 24h 内无法再上传（QUOTA_EXCEEDED），体验与业务双重受损；收集场景下同时放大 D-03。
- 实证：quota=2000B 用户连续 init 两个 1000B 任务（未传分片）→ 第 3 个 `403 QUOTA_EXCEEDED`。
- 修复建议：提供 `DELETE /api/files/{taskID}` 取消接口（删任务+删 tmp+释放预留），清理器对收集任务同时回退槽位（见 D-03）。

**D-05 backup/restore 对每个文件全量 `os.ReadFile`：大库备份/恢复内存 OOM**
- 位置：`cmd/filebox/main.go` `buildBackupArchive` L798（备份读全部文件入内存）、restore 校验 L1032-1039（`os.ReadFile` + `sha256Hex`）
- 根因：归档构建把每个文件整体读进内存再写 tar；恢复端为校验 manifest SHA256 又把每个提取文件整体读进内存。`restoreMaxSingleBytes` 允许 200 GiB 条目——恶意归档可单文件触发 200 GiB 内存分配直接 OOM。
- 影响：文件库稍大（数百 MB 起）备份即高内存；遇到超大文件/恶意归档进程被杀。解压限额（条目/总量）形同虚设，因为校验阶段先于一切内存保护。
- 修复建议：备份/校验改为流式（`io.Copy` 同时喂 `sha256`，逐块写 tar/比对），不要整文件缓冲；恢复校验与提取合并为"边提取边哈希"。

**D-06 read-only 漏网：`mkdirSyncSystem` 未检查只读时段，且会发起真实远端连接**
- 位置：`internal/httpapi/sync.go` L531-575（唯一未接 `rejectReadOnly` 的写入口；对照 create/update/delete system、create/update/delete task、run 均已接）
- 根因：只读时段内用户仍可对远端（SFTP/FileBox）执行 mkdir——远程写操作，且代码会先解密凭据、建立真实 TCP/SSH 连接。
- 影响：只读约束被绕过（远端目录被创建）；响应差异（502 无法连接 vs 403）向只读用户泄露远端网络可达性。
- 实证：给用户设只读窗口后调用 `POST /api/sync/systems/{id}/mkdir` → 返回 `502 无法连接目标系统`（实际尝试连接 10.255.255.1），而非 `403 READ_ONLY`；同用户 create sync task 正确返回 403。
- 修复建议：`mkdirSyncSystem` 开头补 `rejectReadOnly`。

**D-07 分享/聚合分享先扣次数再开文件：物理内容缺失（404）也消耗额度，0 字节交付即打满**
- 位置：`internal/httpapi/server.go` `shareDownload` L2783-2811；`share_group.go` `shareGroupDownload` L237-258、`shareGroupBatchDownload` L319-330（zip 构建前已扣）
- 根因：`IncrementShareDownloads` 在 `os.Open` 之前执行；内容缺失/磁盘故障时返回 404 `文件内容不存在`，但 `download_count` 已 +1。聚合 zip 则在打 zip 之前扣次，zip 中途任一文件缺失也会白扣。
- 影响：分享者看到 `downloadCount` 虚高；存储故障或恶意调用（持 token 反复请求）可烧光 `maxDownloads`，导致分享"0 成功下载就失效"。审计结果记 failure 而计数记 +1，口径自相矛盾。
- 实证：maxDownloads=2 的分享在删除物理文件后下载 2 次均 404，随后 meta `downloadCount=2`、`downloadAvailable=false`，第 3 次 `403 SHARE_DOWNLOAD_LIMIT`。
- 修复建议：先开文件（校验存在）再扣次；或扣次与交付同事务化（至少把扣次移到成功路径，失败回滚）。

### 轻微（10）

**D-08 不限次数分享无法转为有限次数**：`UpdateShareMaxDownloads`/`UpdateShareGroupMaxDownloads` 的 WHERE 要求 `max_downloads > 0`，HTTP 层又显式拒绝 `MaxDownloads == 0` 的分享（server.go L2646、share_group.go L465）。即"0=不限"的分享永远不能设置上限（只能从有限提升，含有限→不限）。语义上可能是设计选择，但无文档、与"提升上限"的帮助文案冲突。`store.go` L1950-1968、`share_group.go` L266-284。

**D-09 batchShare 非事务：批量分享中途失败留下部分链接**：先整体校验后逐个 `CreateShare`（server.go L2224-2321），第 k 个失败时前 k-1 个已创建，响应 500；用户重试会产生重复分享。无资源回滚。轻微原子性问题。

**D-10 改密无审计行**：`changePassword` 只调 `serviceEvent`（server.go L1028），不写 `audit_logs`；而 `logActions` 列表包含 `password_change`（L3624），日志页该筛选项永远为空。审计口径不一致。

**D-11 i18n 缺失键（2 处，UI 显示原始键名）**：`files.uploadFailed`（FilesView.vue L271，恢复传输记录为 failed 时的兜底文案）；`shares.revoked`（SharesView.vue L33/L48，撤销分享/聚合分享后的成功提示）。经全量键扫描确认两者在所有语言包中均未定义。

**D-12 聚合分享 fileCount 口径不一致**：`GetShareGroupByToken`/`ListShareGroupsByOwner` 的 `FileCount` 子查询 `COUNT(*)` 含已删除成员（store/share_group.go L145/L161），而 `ListShareGroupFiles` 过滤 `status='ready'`（L191）。成员被删后：`fileCount` 不变、公开 meta 的 `files` 列表变少；全部成员被删时展示 fileCount>0 但列表为空。轻微口径问题。

**D-13 同步执行 goroutine 无 panic recover + SFTP 传输无 deadline（并发健壮性）**：`scheduleSyncTasks` 每次调度 `go func(){ executeSyncTask }`（sync.go L969-972），`executeSyncPullFileBox` 的 `walk` 对远端响应做 `entry["name"].(string)` 类型断言（sync_filebox.go L552-562）——畸形远端 FileBox 响应可 panic 整个进程；SFTP `io.Copy`（sync.go L1307）无 deadline，连接僵死时 goroutine 与任务互斥锁永久占用，任务之后永远 `TryLock` 失败。建议：goroutine 包 recover；SFTP 会话设 read/write deadline 或整体超时。

**D-14 删除用户不清理磁盘目录树 + 无"最后一个管理员"守卫**：`deleteUser` 只删文件物理内容（server.go L3547-3551），`files/<uid>` 目录及空文件夹残留（`DeleteFolder` 依赖记录，记录已级联删除）；`updateUser` 允许管理员把自己降为 user/禁用（L3285-3330），若为唯一管理员则系统永久失去管理入口。建议：删用户时 `os.RemoveAll(files/<uid>)`；`updateUser` 对"唯一管理员降权"做保护。

**D-15 go.mod 依赖滞后**：`golang.org/x/crypto v0.31.0`（2024-12）早于 2025-03 的安全修复版 v0.35.0（CVE-2025-22869 涉及 ssh 包；本项目仅作 SSH **客户端**使用，风险低，但建议升级）。其余依赖版本正常。

**D-16 管理员文件列表的 dir 过滤按管理员自身 ID 拼前缀**：`store.go ListFiles` L2707 `filepath.Join("files", strconv.FormatInt(userID,10), dir)`——admin 传 `dir=` 只会匹配到自己名下的路径，无法按其他用户目录过滤；前端对 admin 浏览他人目录没有对应 UI。功能限制（非安全问题），建议 admin+dir 语义改为按 storage_path 前缀（而非用户前缀）过滤。

**D-17 同步 pull 失败留下 pending 任务（配额挂账）**：`executeSyncPull`/`executeSyncPullFileBox` 每文件先 `CreateUploadTask`（预留配额）再 `CompleteUploadWithPlacement`（sync.go L1498-1519、sync_filebox.go L517-536）；complete 失败（目标路径已存在/中途配额变化/磁盘错误）时任务保持 pending，配额挂起至 24h 清理，且同步不重试。与 D-04 同源、入口不同。建议失败时调用 `DeleteUploadTask` 回滚。

**D-18（附注，未单独实证）register 无 IP 限速**：`RegisterEnabled` 开启时匿名注册无请求级限速（login 有 IP/账号锁定，register 无），可批量创建 100GB 配额账号。默认关闭，风险低；建议注册复用 `allowPublicRequest`。

---

## 三、两路可能一致的重点项（供仲裁）

高置信重叠（逻辑或代码对照明显，codex 大概率也命中）：
1. **D-01 运行中备份 WAL 不一致**（CLI 细读必现，且危害最大）。
2. **D-02 秒传跨目录**（`FindInstantMatch` vs `FindInstantMatchInDirectory` 双函数对照极醒目）。
3. **D-03 收集箱槽位 init 即消耗不退还**（业务规则核算类，两条路线都会算账）。
4. **D-07 分享先扣次后开文件**（计数口径类）。
5. **D-04/D-17 配额预留无释放/无取消 API**。
6. **D-06 read-only 漏 mkdir**（写入口覆盖枚举类）。
7. **D-11 i18n 缺失键**（前端键扫描类，若 codex 也扫描则必中）。

可能分歧：
- D-05（backup/restore OOM）：取决于 codex 是否细读 restore 校验路径——建议仲裁时实测（构造大文件备份）。
- D-13（goroutine panic/无 deadline）：并发类，codex 若只查 goroutine 泄漏可能不落到 panic-recover。
- D-10（改密审计缺口）、D-12（fileCount 口径）、D-08（不限→限）、D-16（admin dir 过滤）：口径/设计类，倾向"codex 不一定报"。
- 特别提示：D-01 与 D-05 同属 backup/restore 链路，若 codex 报的是"备份前未 checkpoint"而 DSH 报"OOM"，两者是同一文件的两个独立缺陷，不是矛盾。

---

## 四、已确认安全清单（复核后未见问题）

1. **覆盖上传事务模型**（store.go `completeUpload` L1639-1758）：路径分配/落盘/元数据/配额/任务完成单事务；覆盖不先删旧文件（G6），旧路径占位防并发复用（G5）；overwrite 由 rename 原子替换。
2. **删除一致性**：单文件/批量软删除事务 + 提交后物理删除；混合归属的 admin 批量删除按 owner 汇总扣配额（L1401-1414）；删除后 `allocateStorageName` 清理同名 deleted 记录以复用路径。
3. **目录重命名**：DB 事务提交后 `os.Rename`，磁盘失败反向修正 DB（rollbackRenameFolder）；用户级锁串行 + 锁表容量上限（4096）清扫；`escapeLike` 正确转义 LIKE 通配符。
4. **收集 complete 失败清理**：`collectionUploadComplete` 的 `cleanupFinal` defer 在事务回滚（过期/撤销/配额失败）后删除已落盘文件——**无孤儿文件**（读码确认，未发现泄漏路径）。
5. **上传分片安全**：`io.LimitReader(expectedSize+1)` + 写后长度双校验；分片写入 task 私有 tmp 目录、序号为整数索引无穿越；失败即删分片文件与记录。
6. **认证/会话**：HS256 白名单（`WithValidMethods`）、每请求重读用户（禁用即时生效）、`iat < last_password_change` 撤销（亚秒精度）；TOTP 计数器 60s 防重放在事务内完成（store.go `ConsumeTOTP`）。
7. **限速与内存**：用户/IP/公开三类令牌桶，`rateLimiterMaxKeys=10000` 惰性驱逐 + LRU 兜底；匿名公开端点统一 30/min/IP（meta/download/preview/收集全套）。
8. **匿名收集配额与只读**：owner 配额计入（含 pending），init/chunk/complete 三处只读检查齐全；配额错误对匿名脱敏。
9. **路径穿越**：上传名/目录（`validateUploadDir`/`sanitizeName`）、远端路径（`validateRemotePath`）、本地 FileBox 路径（`validateFileBoxSyncPath`）均拒绝分隔符/控制字符/`..`；`originalName` 用 `filepath.Base` 兜底。
10. **restore 解压防护**：`safeArchiveName`（拒绝绝对/`..`/`\`/`:`）+ 随机 staging 目录 + `O_EXCL` 防符号链接覆盖 + 条目/单文件/总量三重限额 + 条目去重 + manifest SHA256 双向校验 + keys.json 口令加密（PBKDF2-210k/AES-256-GCM）+ 明文备份醒目警告（G12）。
11. **用户删除级联（实测）**：带同步任务/远端系统的用户删除成功（SQLite 同批级联不受 RESTRICT 阻塞），sync_tasks/remote_systems/files/shares 全部清空。
12. **前端 XSS 面**：全仓无 `v-html`/`innerHTML`；预览文本用 `<pre>{{ previewText }}`（Vue 转义）；text/html 不在 inline 白名单（`previewMIMEAllowed`）；文件名全程插值转义。
13. **日志脱敏**：收集 token 用 `maskedCollectionToken`（前 8 位）；同步测试结果只落 ok/failure 不落错误详情（v014#5）；服务日志不含 JWT/admin 密码明文（startup 只记 `jwt_secret_source`）。
14. **凭据存储**：TOTP 与同步凭据均 AES-256-GCM 加密（key = SHA-256(jwtSecret)）；`secrets.json` 0600、备份归档 0600、tmp 分片 0600。
15. **WAL/并发基础**：`SetMaxOpenConns(1)` + 每连接 `PRAGMA foreign_keys=ON` + `busy_timeout(5000)`；SSE 随 `r.Context().Done()` 退出且每 30s 重验 JWT；重启幂等的后台清理（audit/sync 日志、分享、过期任务）。
16. **分享计数原子性**：`IncrementShareDownloads`/`IncrementShareGroupDownloads`/`IncrementCollectionUploads` 均为条件 UPDATE 原子操作。
17. **requestIP 可信代理**：仅当设置开启且直连来源在 `--trusted-proxies` 白名单内才解析 X-Forwarded-For（防伪造 IP 绕过限速/锁定）。

---

## 五、报告路径

- 本报告：`docs/code-review-dsh-v015.md`
- 验证脚本与复现材料：`.test-data\review2\t1_instant_crossdir.ps1`、`t2_deleteuser_synctask.ps1`、`t3_collection_readonly.ps1`、`t4_readonly_mkdir.ps1`、`t5_shareslot_missing.ps1`、`t6_quota_reservation.ps1`、`dbcheck\main.go`、`fk_deleteuser_test\main.go`（产品代码未做任何修改，git 工作树保持干净）。
