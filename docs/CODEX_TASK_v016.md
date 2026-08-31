# FileBox v016 修复批次任务书（38 项）

- **依据**：`docs/code-review-v015.md`（主检视合并版，V1-01~V1-38，HEAD `93886b6`）与 `docs/code-review-dsh-v015.md`（并行 DSH 版 D-01~D-18，已并入主报告）
- **范围**：严重 2 项 / 中等 14 项 / 轻微 22 项，全部 38 项逐项修复
- **工作流**：codex 开发（分批自包含指令）→ DSH 复核测试（`.test-data\stage3\` 按验收标准写脚本、重跑原实证看行为反转）→ 双路复检（go test + go test -race）→ 三端部署 → Release 同步
- **环境**：Go 1.27（`C:\Users\huangcp\AppData\Local\Programs\Go\go\bin`，不在 PATH，用前加）；codex CLI `C:\Users\huangcp\.codex\packages\standalone\current\bin\codex.exe`（调用：`codex exec -C <dir> --skip-git-repo-check -s danger-full-access "<任务>"`，需在命令内先设 `OPENAI_API_KEY`）；`-race` 工具链 `C:\Users\huangcp\dsh-project\.tools\w64devkit`（无则普通 `go test -count=1 ./...` 并说明）
- **口径**：不引入新依赖（除 x/crypto 升级）；保持既有风格与双语文案；有歧义的产品口径按报告建议默认实现并在汇报中说明

---

## 一、38 项对照表

### 批次 1（严重，优先）

| 编号 | 位置 | 根因 | 修复要求 | 验收标准 |
|---|---|---|---|---|
| V1-28 | `cmd/filebox/main.go` `runAdminBackup`/`buildBackupArchive`（L672-864）、`runAdminRestore`（L884-1154） | 运行中备份直接 `os.ReadFile(filebox.db)`，未 checkpoint、未带 `-wal`，WAL 模式下主库可能仅 4096 字节 → 恢复后 0 表全丢 | 备份前打开连接执行 `PRAGMA wal_checkpoint(TRUNCATE)`（或 modernc Backup API）；恢复激活前校验目标库可读非空（`SELECT count(*) FROM sqlite_master`）；检测 `-wal` 非空且未 checkpoint 时拒绝并明确报错 | 运行中备份→恢复后表/用户/文件数一致（原实证：恢复后 tables=0 → 修复后 tables≥17）；非空校验拒绝空库激活 |
| V1-01 | `internal/httpapi/collection.go` `collectionUploadInit`（L389-522）与 `collectionUploadChunk`（L526-606） | 收集上传全程无 `diskusage.DiskUsage` 检查，绕过 MinFreeSpace；分片无请求级限速 | `collectionUploadInit`（任务创建前）与 `collectionUploadChunk`（写分片前）补 `diskusage.DiskUsage` + `MinFreeSpace` 检查（503 DISK_FULL）；收集分片接入请求级限速；文档说明公网收集建议设置 maxUploads/maxFileBytes | 原实证（`--min-free-space` 大于实际空闲）：普通上传 503，收集 upload-init 200 → 修复后收集 upload-init/chunk 返回 503 DISK_FULL；分片被限速 |

### 批次 2（实证中等项）

| 编号 | 位置 | 根因 | 修复要求 | 验收标准 |
|---|---|---|---|---|
| V1-30 | `internal/httpapi/sync.go:531-575` `mkdirSyncSystem` | 唯一未接 `rejectReadOnly` 的写入口，只读时段仍可远端 mkdir | 开头补 `rejectReadOnly` | 只读窗口内 `POST /api/sync/systems/{id}/mkdir` 返回 403 READ_ONLY（原实证 502） |
| V1-29 | `internal/httpapi/server.go:1849` `checkInstantUpload`；`internal/store/store.go:1598-1622` | 认证秒传用全库匹配 `FindInstantMatch`，未限定目录 | 改用 `FindInstantMatchInDirectory(userID, filepath.Join("files", uid, dir), …)` 与收集箱对齐；无 dir 时回退全库（按报告建议） | 原实证：dir2 传 dir1 内容 instant=true 且 dir2 列表 0 → 修复后 dir2 不命中秒传（instant=false 或仅 dir1 命中），同目录内秒传仍生效 |
| V1-04 | `internal/store/store.go:2227-2277` `CreateCollectionUploadTask`、`1561-1588` `DeleteUploadTask`、`main.go:249-269` 清理循环 | 槽位在 init 即递增，废弃任务永久占位，`DeleteUploadTask` 不回退 `upload_count` | 槽位改 complete 成功时消耗（init 只校验不递增），或任务废弃/过期清理时同事务 `UPDATE upload_collections SET upload_count=MAX(0,upload_count-1)`；给收集"有效提交"口径 | 原实证：maxUploads=2 两次空 init 后第 3 次 403 → 修复后空 init 不再占位，可继续上传；废弃任务清理后槽位回退 |
| V1-02 | `internal/httpapi/server.go:2827-2882` `sharePreview` | preview 不耗次且全文输出，绕过 maxDownloads | preview 仅输出受限范围（文本前 N KB/视频首段+Content-Range 截断），或 preview 同样消耗次数；至少字节级限速 | 原实证：maxDownloads=1 耗尽后 preview 仍 200 全文 → 修复后 preview 受限（截断/403/耗次其一，行为可验证） |
| V1-03 | `internal/httpapi/server.go:2746-2823` `shareDownload`（L2783 先扣次再 L2812 os.Open）；`share_group.go:237/258`、`319-330` | 先扣次后开文件，Range 逐次扣次 | 先 `os.Open` 校验存在再扣次，失败回滚；同一 token 短窗口内 Range 请求不重复扣次（或按会话/首次扣次语义） | 原实证：物理文件缺失时 404 且 downloadCount+1 → 修复后 404 不扣次；Range 连续请求不重复扣次；正常下载仍正确扣次 |

### 批次 3（其余中等）

| 编号 | 位置 | 根因 | 修复要求 | 验收标准 |
|---|---|---|---|---|
| V1-05 | `internal/store/store.go:2518-2595` `RenameFolder`；`1643-1758` `completeUploadWithCollection` | rename 提交后磁盘移动窗口与并发 complete 竞态 | completeUpload 路径分配/放置获取同用户 folder 锁（与 rename 同一把锁）；或两阶段提交 | 并发 rename+complete 时序下文件记录与磁盘路径一致（无 404）；go test -race 无竞态 |
| V1-06 | `internal/httpapi/sync.go:934-974` `scheduleSyncTasks` | cron 错过窗口不补跑 | 按 `last_run_at` 判定：`next <= now && last_run_at < next` 则补跑（限次防积压） | 停机跨 cron 时刻重启后当天任务补跑（单测或构造验证） |
| V1-07 | `internal/httpapi/sync.go:912` | 手动同步绑 HTTP ctx，客户端断开即取消 | 改独立 `context.Background()`（+全局超时），返回任务状态供前端轮询 | 手动执行不受请求断开影响；单测覆盖 |
| V1-08 | `internal/httpapi/sync.go:1053-1099` `openSFTP`、L1307/L1468 `io.Copy`；`executeSyncTask` | SFTP 无整体超时/读写 deadline | SFTP conn 设读写 deadline 或 `executeSyncTask` 外层 `context.WithTimeout` + defer 强制 Close | 远端挂起时任务在超时后失败释放锁（不永久占用） |
| V1-09 | `server.go:2115-2220` `batchDownload`；`share_group.go:263-401` `shareGroupBatchDownload` | ZIP 无总字节上限、匿名聚合无 IDs 上限 | 构建前计算总原始字节设上限（≤2GB 或磁盘剩余阈值）；匿名聚合 ZIP 补去重后 IDs 上限与单次总字节上限；流式打包+限速 | 原实证：1000 ids 200 → 修复后超限 4xx；单次总字节超限拒绝 |
| V1-31 | `cmd/filebox/main.go` `buildBackupArchive` L798、restore 校验 L1032-1039 | 全量 `os.ReadFile` 内存 OOM | 备份/校验改流式（`io.Copy` 同时喂 sha256 逐块写 tar/比对），恢复边提取边哈希 | 大文件备份内存平稳（构造 ≥100MB 文件备份成功且校验通过） |
| V1-32 | `internal/store/store.go:1425-1458` `CreateUploadTask`；`main.go:249-269`；`sync.go:1498-1519`、`sync_filebox.go:517-536` | 配额预留无释放路径 | 提供 `DELETE /api/files/{taskID}` 取消接口（删任务+删 tmp+释放预留）；前端加取消按钮；同步 pull 失败时调用 `DeleteUploadTask` 回滚（V1-17 一并） | 原实证：quota=2000B 两个 1000B pending 后第 3 个 403 → 修复后 DELETE 任务释放配额可继续 init；同步 pull 失败不留 pending 挂账 |
| V1-33 | `internal/httpapi/sync.go:969-972`；`sync_filebox.go:552-562` | goroutine 无 recover；类型断言可 panic | `scheduleSyncTasks` 的 go func 包 recover 记日志；walk 类型断言改逗号 ok 形式或结构体解码 | 畸形远端响应不崩溃进程（单测/构造验证）；goroutine 带 recover |

### 批次 4（轻微 22 项，全部处理）

| 编号 | 修复要求 | 验收标准 |
|---|---|---|
| V1-10 | `PruneShares` 同事务级联清理 `share_groups`/`share_group_files`（按留存策略） | 过期/撤销聚合分享被清理，无孤儿行 |
| V1-11 | `fileCount` 按 ready 过滤或拆 memberCount/availableFileCount | 删成员后 fileCount 与 files 列表一致 |
| V1-12 | logout 撤销 JWT（jti+黑名单或会话版本号） | logout 后旧 token `/api/auth/me` 401 |
| V1-13 | i18n 补 `files.uploadFailed`/`shares.revoked` 三语键 | 键扫描无缺失；两处 UI 显示正常文案 |
| V1-14 | batchShare 事务化（失败回滚） | 批量中途失败不留部分链接 |
| V1-15 | register 接入 `allowPublicRequest`（如 5/min/IP） | 注册超频 429 |
| V1-16 | 慢速 body 超时（分片读取总时长上限或 SetReadDeadline） | 慢速连接在超时后被断开（构造验证/单测） |
| V1-17 | AddAuditLog 惰性清理改计数触发或 main.go 每小时 PruneAuditLogs | 不再每次写日志全表 DELETE（单测/代码证据） |
| V1-18 | TOTP 防重放改拒绝 `counter <= lastUsedCounter`（单调） | 旧 counter 重放被拒（单测） |
| V1-19 | 只读时段拦截改密/改语言（产品豁免需说明） | 只读窗口内 change-password/update-language 403 READ_ONLY |
| V1-20 | 秒传冲突查询非 ErrNotFound 错误改 500（不再 fail-open） | 冲突查询出错返回 500 而非 instant:true |
| V1-21 | 撤销状态侧信道统一（shareMeta 与 shareDownload 对 revoked 同状态码） | 已撤销 token 的 meta/download 同状态码 |
| V1-22 | FileBox pull 临时文件建在 `DataDir/tmp` | 跨卷 rename 不失败（代码证据 + 单测） |
| V1-23 | secrets.json 读取后校验 JWTSecret ≥16 字节 | 短密钥启动报错 |
| V1-24 | 加 `X-Frame-Options: DENY` 或 CSP `frame-ancestors 'none'` | 响应头含防护 |
| V1-25 | 前端路由守卫仅 401 清会话（5xx/网络异常保留）；恢复快照保留失败态 | 5xx 不清 token；失败态恢复显示 |
| V1-26 | restore 强制必需条目（filebox.db 必须存在）+ manifest 校验强化（可 HMAC） | 无 filebox.db 的归档恢复被拒 |
| V1-27 | 小项合并 9 项：① 指纹输入宽容收紧 ② 临时文件未关先删改先 Close ③ pull 磁盘预留当前文件大小 ④ PosixRename 回退说明 ⑤ retention=0 语义保护+上限 ⑥ 优雅关闭 handler 超时 ⑦ ListReadyFilesUnder 分页/限制 ⑧ 覆盖物理峰值文档说明 ⑨ 分片哈希 complete 校验 | 逐项代码证据 + 相关单测/文档 |
| V1-34 | 改密补 `recordAudit("password_change")` | 改密产生 audit 行，日志页筛选项有数据 |
| V1-35 | 唯一管理员降权/禁用保护 + `deleteUser` 时 `os.RemoveAll(files/<uid>)` | 唯一管理员降权/禁用被拒；删用户后磁盘目录树清除 |
| V1-36 | 不限次分享转有限（产品语义：支持 0→N，补文档） | 0=不限可设上限（单测 + 文档） |
| V1-37 | 升级 golang.org/x/crypto ≥v0.35.0 | go.mod 版本 ≥ v0.35.0，go mod tidy 干净 |
| V1-38 | admin dir 过滤改按 storage_path 前缀过滤（或文档化限制） | admin 可按任意用户目录过滤（或文档说明） |

---

## 二、批次计划与流程

1. **批次 1（严重）**：V1-28、V1-01 → codex 开发 → DSH 复核（backup WAL 恢复反转 + 收集填盘对照反转）
2. **批次 2（实证中等）**：V1-30、V1-29、V1-04、V1-02、V1-03 → codex 开发 → DSH 复核（mkdir 只读/秒传跨目录/槽位占坑/preview 次数/分享扣次反转）
3. **批次 3（其余中等）**：V1-05、V1-06、V1-07、V1-08、V1-09、V1-31、V1-32、V1-33 → codex 开发 → DSH 复核
4. **批次 4（轻微 22 项）**：V1-10~V1-27、V1-34~V1-38 → codex 开发 → DSH 复核
5. **全量回归**：既有套件（transfer/share/expire/resume/regress/folders/batchC-F）+ 新批次测试；`go test -race`（httpapi+store，w64devkit gcc）
6. **文档**：CHANGELOG 顶部 v016 批次（按 4 批分组）、STATE.md 同步、README×2/RELEASE_NOTES×2 补 v016 章节（含安全修复清单）、TEST_REPORT.md
7. **提交推送**：消息格式「v016 修复批次: ...」；可选更新 GitHub Release v0.2.0 附件
8. **三端部署**：18080（filebox-demo，保留数据，备份先行）、18090（user-test）、远端 202.6.205.59:18080（systemd，dist/filebox-linux-amd64）——部署前确认 schema 兼容与备份；18080 特别注意旧数据目录（已有 secrets.json）

## 三、验收标准（总）

- 38 项逐项有代码证据（git diff 对应位置）
- 原实证全部行为反转（见各批验收标准）
- `go build ./...`、`go vet ./...`、`go test -count=1 ./...` 全绿；`go test -race`（httpapi+store）全绿
- 前端 `npm run build` + `sync-web` 通过（有前端改动批次）
- 三端部署后健康检查 + 数据保留验证通过
