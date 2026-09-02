# FileBox 阶段二 DSH 二次测试报告

日期：2026-08-30 ｜ 测试环境：Windows / Go 1.27 / filebox v0.2.0 单二进制（127.0.0.1:18081，独立数据目录 `.test-data\stage2\test-data`）
测试依据：docs/TEST_PLAN.md（阶段二） ｜ 被测版本：v0.2.0（阶段二全量 + 阶段一回归 + v011 反馈批次 1-5）

## 一、结果统计

| 类别 | 套件 | 用例数 | 通过 | 失败 |
|---|---|---|---|---|
| 准备 | test-setup2（改密/建用户） | 6 | 6 | 0 |
| 分片/秒传/文件夹/配额 | test-transfer2 | 19 | 19 | 0 |
| 分享/预览/限速/注册/统计/日志 | test-share2 | 24 | 24 | 0 |
| 分享过期 | test-expire | 6 | 6 | 0 |
| 1GB 断点续传 | test-resume1gb | 9 | 9 | 0 |
| 阶段一回归 | test-regress2 | 26 | 26 | 0 |
| v011 批次 5 前端专项（14-17） | test-batch5 | 14 | 14 | 0 |
| v011 批次 6 专项（18-19） | test-batch6 | 14 | 14 | 0 |
| v011 批次 7 专项（20） | test-batch7 | 6 | 6 | 0 |
| **合计** | **9 套件** | **124** | **124** | **0** |

## 二、阶段二验收项与结果

| 验收项（DEV_DOC 阶段二） | 用例 | 结果 |
|---|---|---|
| 1GB 文件断点中断后续传成功 | G01-G09：8MB×128 片并发 4 上传 30 片 → 中断 → **重启服务** → status 仍 30 片（chunks 表持久化）→ 续传剩余（含重传 0/1 幂等）→ complete 200 → md5 与本地 `Get-FileHash` 一致（`abbf45a9…76bf`）→ 磁盘文件 1GB | ✅ |
| 秒传命中不重复落盘 | F301-F304：check md5/sha256 命中 instant=true 且磁盘文件数不增（before=1 after=1）；未命中 instant=false；跨用户不命中 | ✅ |
| 文件夹上传保留结构 | F401-F403：`assets/icons` 与 `assets/images` 下同名 `logo.svg` 各保留一份、不加序号；`../` 目录 400「目录无效」 | ✅ |
| 分享链接过期后拒绝 | E01-E06：构造过期时间后 meta 404「分享链接已过期」、download 403；超次数 403「分享次数已用完」；撤销后 404 | ✅ |
| 预览白名单可看、非白名单强制下载 | F601-F602：text/plain → `inline`；application/zip → `attachment` | ✅ |
| 超配额上传被拒 | F500-F502：多分片路径配额 3MB 上传 5MB → 403「超出用户配额」 | ✅ |
| 限速生效 | F601-F603：设置 ~64KB/s 后 1MB 上传耗时 17.9s（理论 ~16s）且 complete 成功；恢复 0 后 1.8s | ✅ |
| Linux 交叉编译可运行 | 构建验证：`GOOS=linux GOARCH=amd64` 产出 `bin/filebox-linux`（17.8MB） | ✅ |
| 注册开关 | F701-F707：关时 403 + REGISTER_DISABLED；brand.registerEnabled 同步；开时注册成功并直接登录；重复 409；弱密码 400 | ✅ |
| 系统统计 | F801：admin stats 含 shares/shareDownloads | ✅ |
| 审计日志含分享/注册动作 | F901-F905：share/share_view/share_download/register 均入库，logActions 含 register | ✅ |
| 回归（阶段一） | R01-R11：单分片上传/下载/删除闭环+双哈希、0 字节（md5=d41d8cd9…）、冲突 rename/overwrite、非法名 400、越权 404、未登录 401、普通用户 admin 403、日志隔离、登录锁定/解除、品牌/语言、CLI reset-password/locks、Range 206 | ✅ |
| v011 批次 5（问题 14-17） | P14a-e：`/admin?tab=system` 200、产物含 admin-tab/admin-layout/admin-sidebar 与六个页签键、`query.tab` 深链读取；P15a-b：modal-backdrop/modal-panel + totpReenroll/ip-acl 三请求保留；P16a-c：LogsView 已移除 retention 面板、系统设置页签含 logRetentionDays/logRetentionCopy；P17a-d：brand-footer-title/brand-footer-desc 类与 brand 接口 siteTitle/siteDescription | ✅ |
| v011 批次 6（问题 18-19） | P18a-c：同名冲突 409 产生审计失败记录（reason=conflict）、日志页可见 upload_init 失败、非法名/配额失败审计 reason 细分；P18d：logActions 含 upload_init/upload_chunk；P18e：3 个同名文件 rename 后全部完成（multi.txt / multi (1).txt / multi (2).txt，name 跟随）；P19a：配额不足 403 含 QUOTA_EXCEEDED+usedBytes/quotaBytes/fileSize；P19b：单文件超限 413+FILE_TOO_LARGE+maxFileSize；P19c：前端产物含映射与冲突队列 | ✅ |
| v011 批次 7（问题 20） | P20a-e：产物含 overallRate/loadedBytes/单位自适应/overall-rate 样式、i18n 三语言、嵌入 bundle 一致 | ✅ |

## 三、缺陷清单（测试发现，已修复并复测）

| 编号 | 级别 | 模块 | 描述 | 复现 | 修复 | 复测 |
|---|---|---|---|---|---|---|
| D-S2-1 | 一般 | 存储 | 删除后重传同名文件返回 HTTP 500：`UNIQUE constraint failed: files.storage_path`——软删除记录仍占用 storage_path，allocateStorageName 未剔除导致复用冲突 | 上传 preview.txt → 删除 → 再传同名 → complete 500 | `allocateStorageName` 在复用路径时清理该路径下的软删除记录（其内容已物理删除）；新增单测 `TestReuploadAfterDeleteReusesStoragePath` | ✅ 删除→重传 200，md5 一致，ready 文件数=1 |
| D-S2-2 | 轻微 | CLI | `filebox admin reset-password` 再次 `flags.Parse(args[1:])` 丢掉首个 flag（`--data` 被忽略），非 admin 用户/非默认数据目录场景下操作落错库 | `admin reset-password --data=<dir> --username=user2` 在 dir 上无记录 | 改为 `flags.Parse(args)`（args 已剔除子命令名）；新增单测 `TestResetPasswordParsesDataFlag` | ✅ CLI 重置 user2 成功且新密码可登录（真实数据目录） |
| D-S2-3 | 一般 | 目录 | 目录重命名级联改写其下文件 `storage_path` 前缀时，若目标前缀下残留同名的软删除记录（先删除后重传同名文件，再重命名目录），`UNIQUE(files.storage_path)` 冲突导致 `PATCH /api/folders/{id}` 返回 500；`isUniqueError` 还会把 `FOREIGN KEY constraint failed` 误判为同名冲突 | 上传 plan.txt → 删除 → 重传 → 重命名所在目录 → 500 `rename folder: UNIQUE constraint failed: files.storage_path` | `RenameFolder` 在改写前清理目标前缀下的 `status='deleted'` 记录（内容已物理删除）；`isUniqueError` 仅匹配 `UNIQUE` 约束失败；新增单测 `TestRenameFolderClearsDeletedRows` | ✅ 重命名 200、磁盘目录物理移动、过滤命中 1；folders 21/21 连续两次运行全绿（幂等） |

修复验证：`go build ./...`、`go vet ./...`、`go test ./...` 全过；Windows/Linux 交叉编译通过；前端构建 + `sync-web` embed 同步通过；上述两处缺陷场景均回归复测通过。

## 四、验证亮点

- **1GB 断点续传跨重启**：服务重启后 `chunks` 表保留 30/128 分片，续传 98 片（另重传 2 片验证幂等覆盖）后 complete 成功，md5 与本地一致，磁盘文件恰为 1GB。
- **v010 迁移演练（问题 6 方案 B）**：构造 13 个 v010 文件（`files/<uid>/26/08|09`，含 `test.txt/(1)/(2)`、DeepSeek 安装包等验证环境同类样本）+ DB 记录；`migrate-v010-paths` 迁移 3 个年月目录（10+1+2 文件）成功：备份生成 `filebox.db.bak-v011`、物理文件完整移动到 `files/<uid>/2026-08|09/`、DB 13 条 storage_path 全部重写（无 `26/08` 残留）、folders 登记 3 条；迁移前后文件数 13→13 一致；迁移后回归：列表 13 项可见、`test.txt` 下载 200 且内容正确、`dir=2026-08` 过滤命中 10、目录列表可见 2026-08/2026-09。幂等性由 F618-F621 复跑验证。
- **分片校验**：乱序上传、末片小于 chunkSize、缺片 complete 400「上传分片不完整」、重复 complete 404 均符合预期。
- **限速**：1MB @ 64KB/s 实测 17.9s（令牌桶生效），恢复 0 后 1.8s。
- **分享原子计数**：maxDownloads=1 时第 2 次匿名下载 403「分享次数已用完」；过期 meta 404 / download 403。
- **Linux 真机部署实测（202.6.205.59:8022，Ubuntu 24.04 x86_64，无 go/node）**：上传静态 `filebox-linux-amd64`（12MB）→ chmod +x → 启动 18084 → root 200；`/api/brand` 返回含 `maxFileSize`；登录+强制改密 → 上传 complete 200 → 列表可见；秒传协调（同名→conflict、异名→instant）；聚合下载 zip（magic PK、155B）；`migrate-v010-paths` 对运行中数据目录幂等执行（备份 filebox.db.bak-v011 + 0 目录迁移 + success）；SSE 上传进度（上传分片前 uploaded:0 → 后 uploaded:1）。
- **v012 优化项验证**：① 废弃上传任务定时清理——构造 25h 旧 pending 任务 → `ListExpiredUploadTasks` 命中 → `DeleteUploadTask` 删除（含 chunks）→ 再查为空；② SSE 推送——`GET /api/files/progress/stream` 每秒推送当前用户 pending 任务（taskId/name/totalChunks/uploaded），前端使用带 Bearer 认证的流式 fetch 订阅。
- **秒传不落盘**：同文件二次 check 磁盘文件数不增；跨用户不命中（归属隔离）。
- **回归全绿**：阶段一 26 项（含 TOTP 相关路径的登录/锁定/审计/CLI）无回归。

## 五、结论

阶段二全部验收标准达成：124/124 用例通过（9 套件，含 v011 反馈批次 5/6/7 前端专项 14/14/6 项），0 遗留缺陷（测试发现的 3 个缺陷均已代码级修复并复测）。满足 docs/DEV_DOC.md 阶段二验收标准与 docs/CODEX_TASK_2.md 全部条目；v011 验证问题修复单 20 项全部交付。

## 六、v016 深入检视修复测试报告

v016 完成 V1-01～V1-38 共 38 项修复。DSH 在 `.test-data\stage3\` 执行反转验证 11/11，通过项如下：

1. `test-v128-backup-wal.ps1`：运行中备份恢复后用户/文件数据保持一致；空数据库归档和缺少 `filebox.db` 的归档均被拒绝。
2. `test-v101-collection-disk.ps1`：普通上传与收集上传在最小可用空间不足时均返回 `503 DISK_FULL`，收集入口不能绕过磁盘保护。
3. `test-v130-mkdir-readonly.ps1`：只读窗口内 `mkdirSyncSystem` 在远端连接前返回 `403 READ_ONLY`。
4. `test-v129-instant-crossdir.ps1`：跨目录内容不命中秒传；同目录仍命中；省略目录的根目录兼容回退仍命中。
5. `test-v104-collection-slot.ps1`：空 `init` 均成功且 `uploadCount=0`；两个真实 complete 后计数为 2；第三次 init 返回 `403 COLLECTION_LIMIT`。
6. `test-v102-preview-limit.ps1`：下载次数耗尽后 preview 仍可用但响应不超过 512KB，不能通过 preview 取走全文。
7. `test-v103-share-count.ps1`：物理文件缺失返回 404 且不扣次；连续 3 次 Range 最多扣 1 次；正常下载仍正确计数。
8. `test-v132-quota-cancel.ps1`：配额被两个 pending 任务占满时第三次 init 被拒；DELETE 返回 200、删除临时目录并释放配额；重复 DELETE 返回 404，管理员可取消其他用户任务。
9. `test-v131-backup-stream.ps1`：64MB 文件 backup/restore 成功，恢复内容和大小与源文件一致，验证流式处理结果。
10. `test-v123-secrets-length.ps1`：`secrets.json` 中短 JWT secret 使服务以非零状态退出，不能带短密钥启动。
11. `test-v134-batch4-runtime.ps1`：验证防点击劫持响应头、改密审计、唯一管理员守卫、不限次转有限、fileCount、logout 后旧 JWT 返回 401、只读改密/改语言 403、撤销 meta/download 状态一致，以及注册超过 5/min 返回 429。

### v016 新增单测清单

- `cmd/filebox/main_test.go`：`TestBackupCheckpointsWALAndRestoreValidatesDatabase`、`TestValidateRestoredDatabaseRejectsEmptyDatabase`、`TestBackupRestoreLargeFileStreams`、`TestRestoreRejectsArchiveWithoutDatabase`。
- `internal/httpapi/server_test.go`：`TestCollectionUploadRejectsWhenDiskFull`、`TestSharePreviewLimitsLargeTextAndRange`、`TestDeleteCollectionUploadTasksReleasesSlots`、`TestCompleteUploadWaitsForFolderLock`、`TestBatchDownloadsRejectOversizedArchives`、`TestDeleteUploadTaskReleasesQuotaAndTemporaryChunks`、`TestLogoutRevokesJWT`、`TestBatchShareRollsBackCreatedShares`、`TestRegistrationRateLimit`、`TestShareGroupFileCountMatchesReadyMembers`、`TestCheckInstantUploadReturns500WhenConflictLookupFails`、`TestFileBoxDownloadUsesDataDirectoryTemp`、`TestSecurityHeadersIncludeFrameProtection`、`TestPasswordChangeWritesAudit`、`TestCompleteRejectsChangedChunkHash`、`TestAdminGuardsLastAdministratorAndRemovesUserDirectory`、`TestUnlimitedShareCanBecomeFinite`、`TestAdminListFilesDirUsesStoragePrefix`、`TestCollectionUploadInitDoesNotConsumeSlots`、`TestDeleteCollectionUploadTasksDoesNotChangeSlots`、`TestShareDownloadRangeWindowDeduplicatesContinuousRanges`。
- `internal/httpapi/read_only_test.go`：`TestReadOnlyBlocksSyncAndCollectionWrites`、`TestReadOnlyBlocksCollectionChunkUpload`。
- `internal/httpapi/sync_test.go`：`TestLatestCronOccurrenceReturnsMostRecentMissedRun`、`TestFileBoxRemoteClientRejectsMalformedBrowseEntry`。
- `internal/store/store_test.go`：`TestAddAuditLogOnlyInsertsWithoutPruning`、`TestConsumeTOTPRejectsReplayedAndOlderCounters`。

全量复检：`go build ./...`、`go vet ./...`、`go test -count=1 ./...` 全绿；`go test -race`（`internal/httpapi`、`internal/store`）全绿。阶段 3 反转验证结果为 11/11，未发现遗留失败项。

## v019 测试报告（2026-09-01）

- 7 项用户反馈修复全部完成：日志时间筛选确定/清空按钮（草稿态，未确定不生效）、日志/文件/分享/聚合分享分页显示修复（模板 ref 解包 bug）、目录导航规范化（前端规范化 + 后端目录列表过滤归一化 + validateFolderName 拒 `.`/`..`）、聚合分享小眼睛合并、同步日志弹窗加宽、目录批量操作（勾选/删除/重命名）
- 全量 `go test ./...` 通过（httpapi/store/srvlog/cmd 全绿）
- `npm --prefix web run build` 通过（v019 前端含分页修复与目录批量操作）
- 缺陷根因说明：分页不显示 = 模板 `pageSize.value`（Vue 模板 ref 自动解包，`.value` 未定义）；目录无效 = ①user-test 库 v010 遗留 `uploads\xxx` 反斜杠路径记录（3/4 条）点击后 dir 含反斜杠被 validateUploadDir 拒绝；②18080 演示库 872 条目录中 869 条带 `files/` storage 前缀（同步写入），点击进入列表错位；③validateFolderName 未拒 `.`/`..` 可造出必 400 目录
- 新增单测（`internal/httpapi/v019_test.go`）：`TestListFoldersFiltersLegacyPaths`（遗留路径过滤 + files 前缀归一化 + 导航不再 400）、`TestValidateFolderNameRejectsDotDirs`、`TestNormalizeFolderPath`（分支覆盖）——全部通过
