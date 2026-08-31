# FileBox 深入代码检视报告（v015 · 主检视 · 合并双路线）

- **检视对象**：`HEAD 93886b6`（工作树干净，远程 tony2025huang/filebox main）
- **检视方式**：主检视＝人工逐文件源码分析（server.go 4122 行 / store.go 3084 行 / sync.go+sync_filebox.go / collection.go / share_group.go / main.go / srvlog / 前端全量）+ **6 个 codex CLI 深度任务**（认证授权/上传链路/分享下载/同步/并发运维/前端，独立交叉验证）+ **关键高危项真实请求实证**（测试实例 18085，独立数据目录 `.test-data\review\data`，`--min-free-space=60GB` 构造磁盘保护对照）
- **并行路线**：`.test-data\review2` 独立 DSH 复核报告 `docs/code-review-dsh-v015.md`（D-01~D-18，实测端口 18086）——本报告**已将其独立发现全量合并**，重叠项去重后统一编号 V1-xx
- **测试基线**：`go build ./...` 通过；`go test -count=1 ./...` 全绿；`go test -race`（httpapi+store，w64devkit gcc）全绿
- **范围约定**：既往已修复项（P0-P3 批次 `1ad65a0`、G1-G14 批次 `7032402`、v013/v014 用户批次）不重复报告，仅复核修复质量；既往"9 项人工检查项"未在仓库内留档，本报告对相应代码面做了同等深度人工复核
- **说明**：只读检视，未修改产品代码；验证脚本/测试数据均在 `.test-data\` 下（不污染仓库，git 工作树仅新增两份检视报告）

---

## 一、问题清单摘要（合并后 V1-01 ~ V1-38）

| 档位 | 数量 | 关键项 |
|---|---|---|
| 严重（数据丢失/DoS/越权/注入） | 2 | V1-01 收集上传绕过磁盘保护（实证）；V1-28 运行中 `admin backup` 不含 WAL→恢复全丢（实证） |
| 中等（功能缺陷/部分绕过/一致性/资源滥用） | 14 | V1-02 preview 绕过下载次数（实证）；V1-03 Range 逐次扣次+先扣次后开文件（实证）；V1-04 收集槽位永久占次（实证）；V1-05 RenameFolder 竞态（codex 独立确认）；V1-06 cron 错过不补跑；V1-07 手动同步绑请求 ctx；V1-08 SFTP 无整体超时；V1-09 ZIP 无总字节上限（实证）；V1-29 登录秒传跨目录（实证）；V1-30 mkdirSyncSystem 漏只读检查（实证）；V1-31 backup/restore 全量 ReadFile OOM；V1-32 配额预留无取消 API（24h 挂账）；V1-33 同步 goroutine 无 recover/类型断言 panic |
| 轻微（体验/健壮性/口径） | 22 | V1-10~V1-27 主检视+codex 项；V1-34 改密无审计行；V1-35 无唯一管理员守卫；V1-36 不限次分享无法转有限；V1-37 go.mod 依赖滞后；V1-38 admin dir 过滤 |

> 无"越权/注入"级严重问题：IDOR 抽查全部 404；XSS 面（v-html=0、html/svg 预览已除）复核通过。严重档 2 项均为数据可用性/完整性 DoS 类。

---

## 二、严重（数据安全 / 越权 / 注入 / DoS）

### V1-01 收集上传链路完全绕过 `--min-free-space` 磁盘保护（匿名磁盘耗尽）
- **位置**：`internal/httpapi/collection.go` — `collectionUploadInit`（L389-522）与 `collectionUploadChunk`（L526-606）全程无 `diskusage.DiskUsage` 调用（全文件 grep 无任何磁盘检查）；对比普通上传 `server.go:1433-1446`（`uploadInit` 创建任务前检查 `free < MinFreeSpace → 503 DISK_FULL`）
- **根因**：v012 批次 C 实现收集上传时只复用分片链路，未接入 P0-P3 批次给普通上传加的磁盘保护；收集分片写入 `data/tmp/<taskID>/`（L570-576）同样无检查。叠加放大：① 创建收集允许 `maxUploads=0`（不限）+ `maxFileBytes=0`（不限），默认即不限；② `collectionUploadChunk` 只走字节限速 `limiterForPublic`，默认 `UploadRateLimit=0`=不限速，且**不调用** `allowPublicRequest` 的 30/min 请求级限速（仅 meta/init/status/complete 调用）——匿名持 token 者可无节流并发写分片
- **影响**：持任一有效收集 token 的匿名攻击者可将宿主磁盘填满（不触发 DISK_FULL），全站上传不可用、DB/日志盘满；公网部署属数据可用性 DoS
- **验证方法/实证结果**：测试实例 `--min-free-space=60GB`（实际空闲 47GB）——普通 `POST /api/files/upload-init` 返回 **503 DISK_FULL**；匿名 `POST /api/collections/{token}/upload-init` 返回 **200**，chunk(200)+complete(200) 全链路落盘成功。**实证成立**（codex upload 独立确认）
- **修复建议**：`collectionUploadInit`（任务创建前）与 `collectionUploadChunk`（每次写分片前）补 `diskusage.DiskUsage` + `MinFreeSpace` 检查；收集分片接入请求级限速；文档明确公网收集需设置 maxUploads/maxFileBytes 与限速

### V1-28 运行中执行 `admin backup` 产生不含 WAL 的空库归档，恢复后全量数据丢失（默认路径）
- **位置**：`cmd/filebox/main.go` `runAdminBackup`/`buildBackupArchive`（L672-864）、`runAdminRestore`（L884-1154）
- **根因**：备份直接 `os.ReadFile(filebox.db)`（L798），未先 `PRAGMA wal_checkpoint(TRUNCATE)`、未复制 `-wal/-shm`、未用 SQLite Online Backup API。服务运行中（`journal_mode=WAL`）主库文件可能仅 4096 字节，schema 与数据全在 `-wal`。CLI 只打印 "note: stop the FileBox service before backup"（L697），不检测、不拒绝——**默认操作即踩坑**（README/deploy 文档均建议"运行中备份"场景存在）
- **影响**：任何在服务运行期做的备份都是"看起来成功、实际全丢"的炸弹：归档自洽（manifest SHA256 全过），恢复激活后 0 表 0 用户 0 文件，服务重启重建空库；`--force` 还先把原目录改名保留后整体替换，若运维未保留旧目录即不可逆
- **验证方法/实证结果**：并行 DSH 路线实证（上传 2 文件后 live db 17 表/1 用户/2 文件；运行中备份→恢复：`restored/filebox.db` 4096 字节，`tables=0`、users 查询报 "no such table"）。**实证成立**
- **修复建议**：备份前打开连接执行 `PRAGMA wal_checkpoint(TRUNCATE)` 或改用 modernc Backup API；恢复激活前校验目标库可读非空（`SELECT count(*) FROM sqlite_master`）；检测 `-wal` 非空且未 checkpoint 时拒绝并明确报错

---

## 三、中等（功能缺陷 / 部分绕过 / 一致性 / 资源滥用）

### V1-02 分享 preview 完全绕过下载次数上限（匿名可无限取走白名单类型文件全文）
- **位置**：`internal/httpapi/server.go:2827-2882` `sharePreview`——校验有效期/撤销后直接 `http.ServeContent`，**不调用** `IncrementShareDownloads`（L2783 仅在 `shareDownload`）；`previewMIMETypes`（L2981-2986）含 text/plain、markdown、csv、x-log、json、pdf、image/*、video/*
- **根因**：设计上"preview 不耗次"，但 preview 对白名单类型是**全文 inline 输出**（L2875-2879）；G2 只加 30/min 的**请求数**限速（L2838），不限制单请求字节量
- **影响**：`maxDownloads` 对可预览类型形同虚设——次数耗尽（`downloadAvailable=false`）后仍可经 preview 无限次取走全文（30 次/分/IP）
- **验证方法/实证结果**：`maxDownloads=1` 分享，匿名 download 1 次耗尽后，连续 3 次 preview 全部 **200 且返回完整 1024 字节内容**。**实证成立**（codex share 独立确认）
- **修复建议**：preview 仅输出受限范围（文本前 N KB / 视频首段 + Content-Range 截断）；或 preview 同样消耗次数；至少把 30/min 换成字节级限速

### V1-03 分享下载先扣次后开文件 + Range 逐次扣次（空转烧光次数）
- **位置**：`internal/httpapi/server.go:2746-2823` `shareDownload`（L2783 先 `IncrementShareDownloads` 再 L2812 `os.Open`）；`share_group.go:237/258`（shareGroupDownload）、`share_group.go:319-330`（聚合 zip 构建前已扣）；每次请求（无论 Range）都扣次后 `ServeContent`（L2822）
- **影响**：① 物理内容缺失/磁盘故障时返回 404 `文件内容不存在` 但次数已 +1——审计记 failure 而计数 +1，口径矛盾；持 token 反复请求可把 maxDownloads 烧成"0 成功下载就失效"；② 播放器/多线程下载的 Range 请求逐次扣次，一次会话即可打满 N 次
- **验证方法/实证结果**：`curl -H "Range: bytes=0-100"` 返回 **206** 且 downloadCount 递增（本路线实证）；并行 DSH 实证：删除物理文件后下载 2 次均 404 但 downloadCount=2、downloadAvailable=false。**实证成立**（codex share 独立确认）
- **修复建议**：先开文件（校验存在）再扣次，失败回滚；明确"次数"语义（按会话/首次扣次），或同一 token 短窗口内 Range 请求不重复扣次

### V1-04 收集箱 abandoned 上传任务永久占用 upload_count（垃圾任务堵死收集箱）
- **位置**：`internal/store/store.go:2227-2277` `CreateCollectionUploadTask`（init 事务内 `upload_count+1`）；`store.go:1561-1588` `DeleteUploadTask`（后台 24h 清理只删任务+分片，**不**回退 `upload_count`）；`main.go:249-269` 清理循环
- **影响**：`maxUploads=10` 的收集被 10 次"init 后放弃"占满即永久拒收（403 COLLECTION_LIMIT），`UpdateUploadCollection` 又强制 `maxUploads >= uploadCount` 只能上调；同时 owner 配额被 pending 挂起 24h；攻击者可用空 init 打满上限（0 文件送达）
- **验证方法/实证结果**：`maxUploads=2` 收集：连续 2 次匿名 upload-init（无分片）后第 3 次 **403 COLLECTION_LIMIT**；meta `uploadCount=2`、`status=limit_reached`、files 为空。**实证成立**（并行 DSH 的 D-03 同实证；codex upload 独立确认；TODO-v013 #11 曾文档化提示但未修复）
- **修复建议**：槽位改 complete 成功时消耗（init 只校验不递增），或任务废弃/过期清理时同事务 `UPDATE upload_collections SET upload_count = MAX(0, upload_count-1) WHERE id = task.collection_id`

### V1-05 RenameFolder 提交后磁盘移动窗口与并发上传的 storage_path 竞态（极端时序下文件记录指向错路径）
- **位置**：`internal/store/store.go:2518-2595` `RenameFolder`（事务改写 path 前缀 → `tx.Commit()` L2583 → `os.Rename(oldDisk,newDisk)` L2586）；`completeUploadWithCollection`（L1643-1758，事务内 `allocateStorageName` L1760 按旧目录匹配已改写 DB → `place()` 落盘旧路径 → INSERT 旧路径）
- **根因**：G7 的 per-user 锁只串行化"rename vs rename"，未覆盖"rename vs 并发 complete"。坏时序：R 提交 DB 改写 → C 事务完整落在 R 的磁盘移动之前（间隙为 R.commit 与 R.rename 之间的微秒级窗口）→ R 把含新文件的整目录移走 → DB 记录旧路径、文件在新路径 → 下载 404
- **验证方法**：静态时序分析成立；**codex upload 独立判"G5 复核不通过：仍有窗口"**
- **修复建议**：completeUpload 的路径分配/放置获取同用户 folder 锁；或引入文件系统操作日志/两阶段提交

### V1-06 周期同步 cron 错过执行窗口后不补跑（"重启恢复"声明与实际不符）
- **位置**：`internal/httpapi/sync.go:934-974` `scheduleSyncTasks`——`next := schedule.Next(now.Add(-time.Minute))`，`next.After(now) || next.Before(now.Add(-time.Minute))` 则跳过
- **影响**：停机在 cron 时刻（如每日 09:00，停机 08:00-12:00）重启后当天任务整体漏跑，无补跑记录；"重启恢复"仅覆盖最近 1 分钟（codex sync 独立判 G4"不通过"）
- **修复建议**：按 `last_run_at` 判定：`next <= now && last_run_at < next` 则补跑（限次防积压）

### V1-07 手动触发同步绑定 HTTP 请求上下文（客户端断开即取消整个同步）
- **位置**：`internal/httpapi/sync.go:912` `entry := s.executeSyncTask(r.Context(), item)`（scheduled 路径 ctx 是独立 `cleanupContext`，无此问题）
- **影响**：用户点"立即执行"后关页面/断网，同步被取消；已同步文件保留（每文件原子），未同步丢弃，任务记为 failure；大任务无法后台跑
- **修复建议**：手动执行用独立 `context.Background()`（+全局超时，见 V1-08），返回任务状态供前端轮询

### V1-08 SFTP 同步无整体超时/读写 deadline（远端挂起永久持有任务锁）
- **位置**：`internal/httpapi/sync.go:1053-1099` `openSFTP`（仅拨号 15s 超时）；`io.Copy`（L1307/L1468）无 `SetRead/WriteDeadline`；`executeSyncTask` 无整体 deadline
- **影响**：远端 TCP 半死时同步 goroutine 无限挂起，`syncLock` 被永久持有 → 该任务后续执行全部 409/跳过，goroutine 泄漏直至重启
- **修复建议**：SFTP conn 设读写 deadline，或 `executeSyncTask` 外层 `context.WithTimeout` + defer 强制 Close

### V1-09 批量 ZIP 下载无总字节上限（tmp 磁盘耗尽，匿名聚合分享可触达）
- **位置**：`server.go:2115-2220` `batchDownload`（≤500 文件 × ≤100GB）；`share_group.go:263-401` `shareGroupBatchDownload`（**匿名**，成员 ≤500，**无请求 IDs 数量上限**）；zip 在 `data/tmp/` 全量构建（L2148-2195/L330-379），构建无磁盘检查
- **验证方法/实证结果**：`POST /api/shared-groups/{token}/batch-download` 带 1000 个 id 返回 **200**（无 500 上限拦截）。**实证成立**（codex share 独立确认）
- **修复建议**：构建前计算总原始字节并设上限（如 ≤2GB 或磁盘剩余阈值）；匿名 ZIP 补去重后 IDs 上限与单次总字节上限；流式打包+限速

### V1-29 登录用户秒传跨目录匹配：目标目录不出文件，前端却显示"上传完成"
- **位置**：`internal/httpapi/server.go:1849` `checkInstantUpload` 用 `FindInstantMatch`（`store.go:1598-1606`，**全库匹配**，无目录限定）；`FindInstantMatchInDirectory`（`store.go:1610-1622`）存在但**仅收集箱在用**（`collection.go:467`）
- **根因**：认证上传的秒传检查未像收集箱那样限定目标目录。用户在 dir2 上传与 dir1 相同内容 → 返回 `instant:true` 及 dir1 的文件记录；前端置 100%、提示"秒传成功"并刷新列表，但 dir2 下没有任何文件——文件静默落在错误目录（或根本不新建），与收集箱行为不一致
- **验证方法/实证结果**：并行 DSH 实证：dir1 上传 hello.txt 后，dir2 发起 `/api/files/check` → `instant=true fileId=1`，`GET /api/files?dir=dir2` → `total=0`。**实证成立**
- **修复建议**：`checkInstantUpload` 改用 `FindInstantMatchInDirectory(userID, filepath.Join("files", uid, dir), …)` 与收集箱对齐；无 dir 时回退全库或直接不秒传（需产品确认口径）

### V1-30 `mkdirSyncSystem` 未检查只读时段，且会发起真实远端连接（只读约束绕过 + 网络探测）
- **位置**：`internal/httpapi/sync.go:531-575` `mkdirSyncSystem`——唯一未接 `rejectReadOnly` 的写入口（对照 create/update/delete system、create/update/delete task、run 均已接）
- **影响**：只读时段内用户仍可对远端（SFTP/FileBox）执行 mkdir——远程写操作，且会先解密凭据、建立真实 TCP/SSH 连接；响应差异（502 无法连接 vs 403）向只读用户泄露远端网络可达性
- **验证方法/实证结果**：并行 DSH 实证：设只读窗口后调 `POST /api/sync/systems/{id}/mkdir` → 返回 `502 无法连接目标系统`（实际尝试连接 10.255.255.1），而非 `403 READ_ONLY`；同用户 create sync task 正确 403。**实证成立**
- **修复建议**：`mkdirSyncSystem` 开头补 `rejectReadOnly`

### V1-31 backup/restore 对每个文件全量 `os.ReadFile`：大库备份/恢复内存 OOM
- **位置**：`cmd/filebox/main.go` `buildBackupArchive` L798（备份读全部文件入内存再写 tar）；restore 校验 L1032-1039（`os.ReadFile` + `sha256Hex` 整文件读入）
- **影响**：文件库稍大（数百 MB 起）备份即高内存；`restoreMaxSingleBytes` 允许 200 GiB 条目——恶意归档可单文件触发 200 GiB 内存分配直接 OOM；解压限额在"整文件读入校验"面前形同虚设
- **验证方法**：静态确认（并行 DSH 建议构造大文件实测）
- **修复建议**：备份/校验改流式（`io.Copy` 同时喂 `sha256`，逐块写 tar/比对），不整文件缓冲；恢复"边提取边哈希"

### V1-32 配额预留无释放路径：废弃 pending 任务锁定配额 24h，无取消 API
- **位置**：`internal/store/store.go:1425-1458` `CreateUploadTask`（pending 求和预留）；`main.go:249-269` 清理器（>24h 才删除）；前端仅 pause/resume，无 cancel 端点；同步 pull 每文件 CreateUploadTask 失败后任务保持 pending（`sync.go:1498-1519`、`sync_filebox.go:517-536`）
- **影响**：用户连续废弃几个大上传后 24h 内无法再上传（QUOTA_EXCEEDED）；收集场景放大 V1-04；同步 pull 失败挂账配额且不重试
- **验证方法/实证结果**：并行 DSH 实证：quota=2000B 用户连续 init 两个 1000B 任务（未传分片）→ 第 3 个 **403 QUOTA_EXCEEDED**。**实证成立**
- **修复建议**：提供 `DELETE /api/files/{taskID}` 取消接口（删任务+删 tmp+释放预留）；同步 pull 失败时调用 `DeleteUploadTask` 回滚

### V1-33 同步执行 goroutine 无 panic recover + 远端响应类型断言可致进程崩溃
- **位置**：`internal/httpapi/sync.go:969-972` `scheduleSyncTasks` 的 `go func(){ executeSyncTask }` 无 recover；`sync_filebox.go:552-562` `executeSyncPullFileBox` 的 `walk` 对远端响应做 `entry["name"].(string)`、`entry["isDir"].(bool)` 类型断言
- **影响**：畸形/恶意远端 FileBox 响应（字段类型不符）可 panic 整个进程（Go 未恢复 panic 会终止程序）；SFTP 无 deadline 时连接僵死使任务锁永久占用（并入 V1-08）
- **修复建议**：goroutine 包 recover 并记录失败日志；类型断言用逗号 ok 形式或结构体解码

---

## 四、轻微（体验 / 健壮性 / 口径）

### 主检视项（本报告人工 + codex 交叉发现）

- **V1-10** 聚合分享（share_groups）永不被清理：`store.go:2002-2012` `PruneShares` 仅 DELETE `shares`；`share_groups`/`share_group_files` 无过期/撤销清理。修复：同事务按留存策略清理（级联）。
- **V1-11** 聚合分享公开 meta 的 fileCount 口径与文件列表不一致：`store/share_group.go:144-154`（COUNT 全部成员）vs `L187-206`（过滤 ready）。实证：3 文件组删 1 后 `fileCount=3` 但 `files` 仅 2 项。修复：fileCount 按 ready 过滤或拆 memberCount/availableFileCount。
- **V1-12** logout 不撤销 JWT（无状态残留 7 天）：`server.go:1277-1281`；实证 logout 后同 token `/api/auth/me` 仍 200。修复：jti+黑名单或会话版本号。
- **V1-13** 前端 i18n 缺键：`SharesView.vue:33,48` 用 `t('shares.revoked')`、`FilesView.vue:271` 用 `t('files.uploadFailed')`（i18n.js 均无，`t()` 回退显示原始 key）。实证：538 个模板键扫描仅此 2 个真实缺失。
- **V1-14** batchShare 非事务（批量分享中途失败留部分链接）：`server.go:2224-2321` 逐文件 CreateShare 不回滚（对比 v013 shareGroup 事务实现）。修复：失败逐个回滚或整体事务。
- **V1-15** register 无 IP 限速（开放注册时账户/存储 DoS 面）：`server.go:918-979`。修复：接入 `allowPublicRequest`（如 5/min/IP）。
- **V1-16** 慢速 body 无读超时（慢速连接占用，匿名分片面放大）：`main.go:200-212` ReadTimeout=0；`server.go:1549-1550`/`collection.go:582` io.Copy 无读 deadline。修复：分片读取设总时长上限或 SetReadDeadline 轮询。
- **V1-17** AddAuditLog 每次写入执行全表 DELETE 惰性清理（写放大与锁竞争）：`store.go:2934-2956`。修复：改计数触发或交给 main.go 每小时 PruneAuditLogs。
- **V1-18** TOTP 重放保护非单调：`store.go:961-985` `ConsumeTOTP` 只记录最近一个 counter——旧 counter 在更新 counter 之后仍可被接受（当前窗口 offset -1..+1 覆盖前一个 30s 窗口时）。修复：按用户拒绝 `counter <= lastUsedCounter`。
- **V1-19** 只读时段未拦截改密/改语言：`server.go:1005` changePassword、`server.go:1290` updateLanguage 不调 `rejectReadOnly`（是否豁免需产品确认）。
- **V1-20** 秒传冲突查询失败 fail-open：`server.go:1888-1895` `FindUploadConflict` 非 ErrNotFound 错误仅 log 后仍返回 `instant:true`。修复：非 ErrNotFound 应 500。
- **V1-21** 撤销状态侧信道：`server.go:2711-2715`（shareMeta 对 revoked 404）vs `2762-2765`（shareDownload 403）——同一 token 可区分"从未存在/已撤销"。修复：统一状态码。
- **V1-22** FileBox pull 临时文件跨卷 rename 失败（Windows）：`sync_filebox.go:316` `os.CreateTemp("")` 系统临时目录与 DataDir 可能不同卷，`os.Rename` 跨设备失败。修复：临时文件建在 `DataDir/tmp`。
- **V1-23** secrets.json 读取路径未校验长度：`main.go:530-536` `resolveJWTSecret` 对 secrets.json 内 JWTSecret 只查非空，不调 `validateJWTSecret`（G11 部分通过）。修复：读取后同样校验 ≥16 字节。
- **V1-24** 缺点击劫持防护头：`server.go:362-369` CSP 有 `frame-src` 无 `frame-ancestors`，也无 `X-Frame-Options`。修复：加 `frame-ancestors 'none'`。
- **V1-25** 前端路由守卫把 5xx/网络异常当登录失效：`router.js:47-59` `!result.ok`/catch 一律清 token 跳登录；且 `FilesView.vue:271` 恢复快照时 failed/canContinue 被固定 false（失败态丢失）。修复：仅 401 清理会话；恢复时保留快照失败态。
- **V1-26** restore 校验强化：`main.go:1009-1047/1058-1086` 未强制 `filebox.db` 存在、manifest SHA-256 无认证（可连同篡改）、FileCount 未比对。修复：强制必需条目 + HMAC/签名 manifest。
- **V1-27** 其余小项（合并）：① 指纹解码规范格式不严（`sync.go:1032-1045`，Go base64 忽略 CR/LF、hex 移除全部冒号——不构成匹配绕过，仅输入宽容）；② FileBox 非 200 响应临时文件未先 Close 再删（`sync_filebox.go:329`，Windows 句柄累积）；③ pull 磁盘检查未预留当前文件大小（`sync.go:1444`，`free-fileSize >= MinFreeSpace`）；④ `PosixRename` 回退普通 `Rename` 的原子性依赖服务器（`sync.go:1318-1323`）；⑤ `retentionDays=0` 语义危险（每次写入清空历史审计日志）且无上限（`store.go:2810/2945`）；⑥ 优雅关闭 10s 超时后活跃 handler 仍运行即关 DB（`main.go:285-289`）；⑦ `ListReadyFilesUnder` 全量加载用户文件到内存（`store/sync.go:558-579`）；⑧ 覆盖上传物理峰值超过逻辑配额（旧文件+分片+merged 同时占盘，配额公式本身正确）；⑨ 分片 SetChunk 记录哈希在 complete 从不校验（`store.go:1473-1476`、`server.go:1940-1948`，complete 靠客户端声明 sha256 校验）。

### 并行 DSH 路线新增项（已合并）

- **V1-34** 改密无审计行：`server.go:1028` `changePassword` 只调 `serviceEvent` 不写 `audit_logs`；`logActions` 含 `password_change`（L3624）——日志页该筛选项永远为空。修复：补 `recordAudit("password_change", ...)`。
- **V1-35** 无"唯一管理员"守卫 + 删用户不清理磁盘目录树：`server.go:3285-3330` `updateUser` 允许唯一管理员把自己降为 user/禁用（系统永久失去管理入口）；`deleteUser`（L3547-3551）只删文件内容，`files/<uid>` 目录及空文件夹残留。修复：唯一管理员降权/禁用保护 + `os.RemoveAll(files/<uid>)`。
- **V1-36** 不限次数分享无法转为有限次数：`UpdateShareMaxDownloads`/`UpdateShareGroupMaxDownloads` WHERE 要求 `max_downloads > 0`，HTTP 层显式拒绝 `MaxDownloads==0` 的分享（`server.go:2646`、`share_group.go:465`）——"0=不限"永远不能设上限。需产品确认语义并补文档。
- **V1-37** go.mod 依赖滞后：`golang.org/x/crypto v0.31.0`（2024-12）早于 2025-03 安全修复版 v0.35.0（CVE-2025-22869 涉 ssh 包；本项目仅作 SSH 客户端，风险低，建议升级）。
- **V1-38** 管理员文件列表的 dir 过滤按管理员自身 ID 拼前缀：`store.go:2707` `ListFiles` 中 `filepath.Join("files", strconv.FormatInt(userID,10), dir)`——admin 传 `dir=` 只匹配自己名下路径，无法按其他用户目录过滤（功能限制，非安全问题）。

---

## 五、已确认安全（重点模块复核，避免后续重复检视）

以下项经逐行核对 + 真实请求抽查 + codex/并行 DSH 交叉验证，未发现可利用问题（既往修复质量确认）：

1. **JWT 认证链路**（复核通过）：HS256 白名单（`server.go:3841-3846`）、每次请求重载用户识别禁用（实证：禁用即时 401、恢复即放行）、`LastPasswordChange` 撤销（实证：管理员重置密码后旧密码登录 401、旧 token 失效）、TOTP challenge purpose 校验 + `ConsumeTOTP` 事务防重放（60s 窗口内同 counter 阻断；跨 counter 间隙见 V1-18）、`jwt.TimePrecision=Nanosecond`
2. **强制改密**（复核通过）：`requireAuth` 的 MustChangePassword 拦截（实证：403 PASSWORD_CHANGE_REQUIRED，豁免仅 me/change-password/password-policy）
3. **只读时段**（复核通过，1 处例外）：`rejectReadOnly` 覆盖文件/业务全部写入口（upload/collection/folder/share/batch/sync task 与 system CRUD/settings/brand/admin），**唯一遗漏 `mkdirSyncSystem`（见 V1-30）**；管理员豁免；收集链路 init/chunk/complete 三处检查齐全
4. **IDOR 归属校验**（复核通过，抽查全绿）：文件下载/删除/批量删除（实证 user2→user1 文件 404）、分享管理 `managedShare`、收集 `GetUploadCollection`（实证 404）、同步系统/任务 `GetRemoteSystem/GetSyncTask`、日志 `listLogs` userId 强制、`shareLogs` token+owner 双条件、批量操作整批校验
5. **上传事务一致性**（G5/G6 复核：除 V1-05 窗口外通过）：overwrite 旧文件保留至 complete 成功、原子 rename 替换、配额事务内扣减（`store.go:1702/1717`）、pending 配额实时计算（清理自动释放）、used_bytes 恒 ≥0、分片 index/大小/重复/乱序全部有校验（codex upload 确认 ContentLength=-1 与 LimitReader 超长分支正确）、size=0 正常（MD5=d41d8cd9 有测试）
6. **删除一致性**（复核通过）：单文件/批量软删除事务 + 提交后物理删除；混合归属的 admin 批量删除按 owner 汇总扣配额（`store.go:1401-1414`）；删除后 `allocateStorageName` 清理同名 deleted 记录复用路径；收集 complete 失败 `cleanupFinal` 无孤儿文件
7. **文件名/路径消毒**（复核通过）：validateUploadName/sanitizeName/validateUploadDir/validateFolderName/safeSyncFileName 覆盖分隔符、控制字符、Windows 非法字符、`..`、255 字节、UTF-8；contentDisposition 防头注入；`originalName` 用 `filepath.Base` 兜底
8. **预览 XSS 面**（复核通过）：`previewMIMETypes` 无 text/html、无 image/svg+xml；HTML/SVG 一律附件下载；CSP `script-src 'self'`；前端全仓库**无 v-html/innerHTML**；预览文本 `<pre>` 插值转义
9. **分享/聚合分享状态机**（复核通过，除 V1-03 扣次时机）：IncrementShareDownloads/IncrementShareGroupDownloads/IncrementCollectionUploads 均为条件 UPDATE 原子操作；聚合分享创建事务整批校验+回滚；已删成员下载先校验再扣次（实证 404 不扣次）；extend 不缩短、increase 不降低、revoked 不可编辑
10. **同步 SFTP 认证**（G1 复核：核心通过）：指纹解码 hex/Strict base64（拒绝 padding 低位非零）+ `subtle.ConstantTimeCompare`；凭据 AES-GCM 落库、API 只回 hasCredentials、syncErrorDetail 对 DataDir 脱敏、远端 URL 校验防 SSRF
11. **backup/restore**（G9/G10/G12 复核：除 V1-28/V1-31/V1-26 外通过）：外层 0600+O_EXCL 随机临时名、restore 解压限额+拒绝重复条目+staging 随机名+safeArchiveName 防穿越+manifest SHA256 校验+密钥冲突需 --force --yes；明文 keys.json 醒目警告；keys.json 口令加密（PBKDF2-210k/AES-256-GCM）
12. **配置与密钥**（G11 复核：外部参数路径通过）：--jwt-secret/env/首次生成 ≥16 字节校验、已有 DB 无密钥拒绝启动、secrets.json 0600、JWT/admin 默认值启动警告（secrets.json 内部校验缺口见 V1-23）
13. **限速器/锁表容量**（G8/G13 复核通过）：三桶+请求桶各 10000 上限（LRU 驱逐）；syncLocks/folderLocks 删除任务时清理+容量上限清扫；匿名公开端点统一 30/min/IP
14. **收集配额脱敏与归属**（v013 #9 复核通过）：匿名收集配额错误只回 `COLLECTION_QUOTA_EXCEEDED` 无明细；收集 token 日志脱敏（maskedCollectionToken）；owner 配额计入（含 pending）
15. **SQLite 运行参数**（v013 #15 复核通过）：busy_timeout(5000)+WAL+synchronous=NORMAL+SetMaxOpenConns(1)+foreign_keys=ON（接受断电丢最近提交的文档化取舍）
16. **前端生命周期**（G14 复核通过）：SSE AbortController+onBeforeUnmount 关闭、sessionStorage 快照序列化白名单（不含 File/AbortController/Set/Map）、收集 token 不写 localStorage、COLLECTION_QUOTA_EXCEEDED 脱敏文案、/u//g/ 路由顺序正确、saveSession 复位 401 标志
17. **requestIP 可信代理**（复核通过）：仅设置开启且直连来源在 `--trusted-proxies` 白名单内才解析 X-Forwarded-For（防伪造 IP 绕过限速/锁定）
18. **用户删除级联**（并行 DSH 实测通过）：带同步任务/远端系统的用户删除成功，sync_tasks/remote_systems/files/shares 全清（RESTRICT 不阻塞）
19. **-race 全绿**：httpapi/store 全部测试在 race 下通过

---

## 六、与既往检视的差异说明

### 新增发现（本报告首次报告，均附实证或独立确认）

- **V1-01（严重）** 收集上传绕过 MinFreeSpace（实证）——P0-P3 只给普通 uploadInit 加磁盘保护，collection.go 从未覆盖
- **V1-28（严重）** 运行中 backup 不含 WAL 恢复全丢（并行 DSH 实证）——此前所有检视均未实测 backup/restore
- **V1-02** preview 不耗次绕过 maxDownloads（实证）；**V1-03** 先扣次后开文件+Range 逐次扣次（实证）
- **V1-04** 收集 abandoned 任务永久占次（实证；TODO-v013 文档化提示但未修复）
- **V1-05** RenameFolder 与并发上传竞态（codex 独立判"G5 仍有窗口"）——G5/G6/G7 覆盖 upload-vs-upload 与 rename-vs-rename，未覆盖 rename-vs-upload
- **V1-06/07/08** cron 补跑缺失（G4 判不通过）、手动同步绑请求 ctx、SFTP 无整体超时
- **V1-09** ZIP 无总字节上限 + 匿名聚合 ZIP 无 IDs 上限（实证 1000 ids 200）
- **V1-29** 登录秒传跨目录（并行 DSH 实证）——既往 v012"秒传与冲突协调"修复未覆盖跨目录匹配口径
- **V1-30** mkdirSyncSystem 漏只读检查（并行 DSH 实证）——既往 D 批次只读覆盖枚举遗漏此入口
- **V1-31/32/33** backup OOM、配额无取消 API、同步 panic——均为新增
- **V1-10~V1-27、V1-34~V1-38**：见上（share_groups 无清理、fileCount 口径、logout 无撤销、i18n 缺键、batchShare 非事务、register 无限速、慢 body、审计写放大、TOTP counter 间隙、改密审计、唯一管理员守卫、依赖滞后、admin dir 过滤等）

### 既往项复核结论（非重复报告）

- P0-P3 批次：上传事务一致性 ✅（除 V1-05 窗口）；改密撤销旧 token ✅（实证）；失效 token 前端死循环 ✅（G14）；SFTP 主机密钥 ✅（G1，规范面见 V1-27①）；匿名分享限速 ✅（字节面见 V1-02）；HTTP 超时配置 ✅（慢 body 面见 V1-16）；分享/批量上限 ✅（匿名缺口见 V1-09）；禁用用户任务排除 ✅；审计补全 ✅（改密审计缺口见 V1-34）
- G1-G14：G1/G2/G4/G5/G8-G14 已逐条复核，除文中注明的"面外缺口"（V1-02/05/06/09/23/24）外全部通过
- 既往"9 项人工检查项"未在仓库留档（属父任务会话内部产物），本报告按同等深度复核了全链路，未发现与之冲突的新问题

### 双路线合并说明（避免父任务重复仲裁）

- 本报告（V1 系）与并行 DSH 报告（D 系）的重叠项：D-03↔V1-04、D-04/D-17↔V1-32、D-07↔V1-03、D-09↔V1-14、D-11↔V1-13、D-12↔V1-11、D-18↔V1-15——结论一致，已合并
- D 系独有（已并入 V1）：D-01→V1-28、D-02→V1-29、D-05→V1-31、D-06→V1-30、D-08→V1-36、D-10→V1-34、D-13→V1-33、D-14→V1-35、D-15→V1-37、D-16→V1-38
- V1 系独有（D 系未覆盖）：V1-01/02/05/06/07/08/09/12/15/16/17/18/19/20/21/22/23/24/25/26/27 等
- codex 交叉验证记录：`codex2-*.txt` 六份；一处疑似误报已澄清（completeUpload 的 `for index, chunk := range chunks` 中 index 即 map 键，ListChunks 乱序不影响正确性）

---

## 七、测试数据清理与复现说明

- 本路线测试实例（18085）与并行路线实例（18086）均已停止；`.test-data\review\`（本路线）与 `.test-data\review2\`（并行路线）保留复现现场（实证产生的文件/收集/分享/DB 记录），删除两个目录即可清理（`.gitignore` 已覆盖 `.test-data/`）
- 本路线验证脚本：`.test-data\review\test-*.ps1`（收集绕过/分享次数/preview 绕过/IDOR 抽查/聚合分享口径/认证行为）；codex 任务定义与输出：`run-all-codex2.ps1`、`codex2-*.txt`
- 并行路线脚本（见 `docs/code-review-dsh-v015.md` 第五节）：`.test-data\review2\t1~t6*.ps1`、`dbcheck\main.go` 等
- 产品代码零修改；git 工作树仅新增两份检视报告（`docs/code-review-v015.md`、`docs/code-review-dsh-v015.md`）
