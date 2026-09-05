# Requirement Change Log

## 2026-09-05 - v024 收集创建/编辑支持随机/手动/无密码模式

- 【业务确认】创建收集默认「自动生成密码」：服务端以 crypto/rand 生成 14 位无歧义字母表（不含 0/O/1/l/I）的安全密码，bcrypt 存哈希；响应一次性返回明文 `password` 与仅 fragment 携带的 `passwordUrl`（`/u/<token>#password=…`），DB/列表/详情此后只返回 `passwordProtected`。
- 【业务确认】三种显式模式契约：`passwordMode=random|manual|none`；manual 必须带非空 `password`；none 不允许同时传密码；非法模式 400。
- 【业务确认】编辑（PUT）支持 `passwordMode=keep|random|manual|none`：keep/遗留空模式不改动；none 清除密码；random/manual 更换密码并仅在该次响应一次性揭示明文与 fragment 链接。
- 【业务确认】`passwordMode` 缺省时保持旧语义（create 按 password 字段、update 按 password 指针）以兼容既有客户端；遗留路径绝不回显明文。
- 前端：创建表单三模式单选（默认 random）+ manual 密码输入；结果弹窗一次性展示密码与「带密码链接」并提示仅此一次；列表项对受保护收集显示锁标记。

## 2026-09-05 - v024 FilesView 文件夹上传与传输体验修复

- 【业务确认】文件夹上传只展示一次应用自绘确认：目录/拖放来源经自定义确认后，以 `skipBulkConfirm` 抑制 >50 文件的通用批量确认；纯文件批量仍保留一次通用确认（`needsBulkConfirm` 纯函数 + node 测试覆盖）。
- 【业务确认】修复多文件上传可靠性：以有界公平调度器 `FairStartGate`（并发上限 3，FIFO）取代忙等轮询/间隔重试，暂停/终止/失败的文件让出等待位，不再因无界轮询产生误报网络失败；分片 worker 仍保持全局上限。
- 【业务确认】恢复并细化速率展示：每进行中上传单独按秒采样字节增量并 EMA 平滑写入 `item.rate`；活动上传行显示「已传输 loaded/total + 百分比 + 单文件速率」；整体速率 = 进行中上传 + 下载速率之和。
- 【业务确认】终态归位与标识：完成（成功/秒传）从进行中自动移至「已完成」Tab 并标 success；失败显示 distinct 失败标识且可重试；终止后的上传保留为已取消记录（标 cancelled 并带清除），不再从列表中直接消失。
- 【开发拟定】恢复的快照记录保留 `done`/`cancelled` 语义，刷新后仍落在正确的已完成分区。

## 2026-09-05 - v023 SFTP 主机密钥 TOFU 策略

- 【业务确认】SFTP RemoteSystem 无指纹时首次握手自动固定观察到的 SHA-256 指纹；条件更新处理并发首次连接。
- 【业务确认】已有指纹必须严格匹配；失配拒绝连接并返回 `HOST_KEY_CHANGED` 及安全的 expected/observed 指纹，不静默更新。
- 【业务确认】owner/admin 通过专用接口明确确认后更新指纹并自动重试；普通编辑保留原指纹；审计和服务日志只记录安全元数据。

## 2026-09-04 - v023 独立静态加密密钥兼容迁移

- 【业务确认】新增独立静态 AES-256-GCM 密钥配置：`--encryption-key`、`FILEBOX_ENCRYPTION_KEY` 或 `config/secrets.json` 的 `encryptionKey`，严格标准 Base64 解码后必须为 32 字节。
- 【业务确认】TOTP secret 与同步凭据的新写入使用带 magic/version/key-slot 的版本化信封；旧版无版本 `SHA256(JWTSecret)` 派生密文继续可读，成功读取后在具备安全写路径时惰性重加密并持久化。
- 【业务确认】未配置独立密钥不阻断已有部署，继续旧行为并输出启动警告；错误密钥、错误版本或认证失败只返回通用解密错误，不记录明文或密钥。
- 【业务确认】备份口令模式同时保护 JWT 与独立密钥材料，并保持旧版仅含 JWT 的归档恢复兼容。

## 2026-09-02 - v020（用户反馈优化批次，11 项）

任务书：`docs/CODEX_TASK_v020.md`；codex 分批实施（每批 1 项、独立提交推送，前端 `npm run build`/后端 `go test ./...` 验证；缺陷 4 由并发 codex 执行者提交、内容已核验）；日期 2026-09-02。

- 缺陷1：删除/撤销类操作统一为自定义确认弹窗——全仓 12 处 `window.confirm` 枚举（文件删除/批量删除/目录删除/批量目录删除/>50 文件批量上传/撤销分享/撤销聚合分享/撤销收集/删除同步任务/删除同步系统/删除用户/重置品牌）；FilesView 队列式 `askConfirm`（60s 超时按取消）+ modal-backdrop/modal-panel 弹窗，其余四视图同模式收口（commit `ccc6272`/`54477fe`）；`web/src` 零残留
- 缺陷2：批量 ZIP 失败原因分类透传（`classifyBatchZipError`：ENOSPC/EDQUOT→磁盘空间不足、EACCES/EPERM/EROFS→无写入权限、其余→IO 错误；响应 `code=ZIP_CREATE_FAILED`+`reason`，原始错误仅日志防路径泄露；`batchZipError` 双入口复用）；ZIP 文件名 `filebox-batch-YYYYMMDD-HHMMSS.zip`（后端 Content-Disposition + 前端 download 名一致）；`createBatchTempFile` 可注入 + 双入口回归测试（commit `3ee19ce`）
- 缺陷3：传输抽屉批量控制——上传/下载分区 checkbox+全选，暂停选中/继续选中/终止选中与「全部」；上传终止 `DELETE /api/upload-tasks/{taskId}` 释放配额；下载暂停(AbortController)+Range 断点续传（206 续传/200 重下），进度速率与持久化联动；完成/校验态禁用（commit `cec1382`）
- 缺陷4：顶栏「传输」按钮图标-only → icon-text-button（图标+文字，与导航一致），角标保留（commit `1c62df8`）
- 缺陷5：我的收集双刷新去重（删列表小节头部刷新，留页面标题区）+ 顺带清理分页模板 `.value` 残留（commit `dfe4512`/`4a031de`）
- 缺陷6：`shares.copy` 键重复定义致副标题误显「复制链接」→ 拆分 `shares.intro`（三语描述句）/`shares.copy`（复制按钮 title），SharesView 副标题改引用（commit `e51cbdb`）
- 缺陷7：聚合分享编辑对齐单分享——基本信息网格+链接复制/打开+小时制延期/增次（PUT extend/increase，`replaceGroup` 同步列表与弹窗）+成员增删与到期/上限编辑保留；移除死代码 groupAction（commit `665922d`）
- 缺陷8：同步日志详情行内 `<details>` → 弹窗；解析 `detail`（`路径: 结果`/整体错误行）逐文件展示，状态徽标 成功/跳过/失败/整体错误，原始内容折叠兜底（commit `f6d7b5e`）
- 缺陷9：`.wide-modal` 1100px→`min(1280px,96vw)`，`.sync-log-table` min-width 1160px；任务编辑弹窗独立 `sync-task-modal` 保留 1100px（commit `b685856`）
- 缺陷10：日志失败原因三语本地化补齐——后端 reason 枚举比对，`logReason.*` 键 zhCN/zhTW/en 补至 38 键完全一致（share*5 补 zh/zh-TW、ipLocked/totpFailed 补 zh-TW、readOnly/settingsFailed/invalidRequest/deleteFailed/writeFailed/batch 补三语）；node 校验三语键集合一致（commit `ef9d083`，含并发方 `bdf1eee` 映射扩充）
- 缺陷11：传输按钮 ArrowLeftRight→ArrowUpDown（commit `6d75e1b`）

## 2026-09-01 - v019（用户反馈修复批次，7 项）

- 缺陷1：日志时间筛选弹出面板增加「确定」「清空」按钮（common.confirm/clear 三语 + time-range-actions 样式）；采用草稿态（timeDraft）——打开面板时同步生效值，输入不直接改 filters，未点「确定」关闭不生效；点「确定」应用草稿并刷新、点「清空」清空草稿与生效时间并刷新
- 缺陷2+3：日志页（67 条）与我的文件页分页不显示修复——根因：模板中误用 `pageSize.value`（Vue 模板 ref 自动解包，`.value` 为未定义 → 分页条件恒 false）；日志/文件/分享/聚合分享四个列表页模板一并修正
- 缺陷4：目录点击「目录无效」修复，双保险——①前端 navigateDir 规范化（反斜杠转正斜杠 + 去首尾斜杠）；②后端 listFolders 过滤归一化（剥离历史遗留 `files/`/`files/<uid>/` storage 前缀，剔除 v010 反斜杠遗留路径 `uploads\xxx` 与 `.`/`..` 目录），`validateFolderName` 拒绝创建 `.`/`..` 目录；新增 `normalizeFolderPath` + 3 个测试
- 缺陷5：聚合分享小眼睛交互合并——眼睛图标直接打开编辑弹窗（含成员文件增删 + 有效期/下载上限编辑），卡片仅保留 眼睛/复制/删除 三图标
- 缺陷6：同步任务日志弹窗加宽（wide-modal 780px → 1100px、日志表 min-width 1000px），内容展示更充分
- 缺陷7：我的文件目录批量操作——目录行复选框（selectedFolderIds）+ 批量删除（逐目录删除、非空失败单独提示 foldersNotEmpty）+ 批量重命名（逐个复用重命名弹窗 + 进度提示 batchRenameProgress）+ 全选含当前页目录；分享/下载仍仅文件

## 2026-09-01 - v018（8 项新需求批次）

任务书：`docs/CODEX_TASK_v018.md`；codex 分批实施（后端任务全部由 codex 完成，前端精确替换批次部分由批次负责人按同规格直接应用——codex 对前端小改动连续 3 次进程中断未落盘后回退）；每批独立提交推送；全量 go test + npm build + webassets 同步通过。

- **#1 nav.admin 中文修复**：根因 v017 将 i18n 键 `nav.admin` 改名 `nav.system`，而 AuthenticatedTopbar 用 `t(\`nav.${section}\`)`、AdminView 传 `section="admin"` → 键不存在回退原文。修复：新增 `SECTION_NAV_KEYS` 映射（admin→nav.system、sync→nav.syncTasks，其余直连），模板改用 `t(sectionKey)`；提交 `b9f98f3`/`3e78698`。
- **#2 聚合分享编辑增强**：管理端成员文件增删（`GET/POST /api/shared-groups/{token}/files`、`DELETE .../files/{fileID}`）+ 属性编辑（`PUT /api/shared-groups/{token}`：绝对到期时间须晚于当前、上限不得低于已用次数）；store 新增 `AddShareGroupFiles`（事务校验归属/ready/去重/上限 500）/`RemoveShareGroupFile`/`UpdateShareGroupAttributes`；SharesView 聚合分享卡片加眼睛（成员文件列表弹窗）与编辑弹窗（增删成员 + 改有效期/上限）；日志动作 `share_group_update`；提交 `a6d3ef5`。
- **#3 我的收集按钮同行**：CollectionsView 收集项「查看已收文件」与「复制链接」包入 `.collection-row-actions` 同一行，链接整行显示；提交 `b4b04c2`。
- **#4 同步任务传输进度展示**：Server 进程内 `syncProgress` 注册表（mutex 保护，任务开始注册/结束清理），SFTP 与 FileBox 的 push/pull 四路径更新 totalFiles/doneFiles/currentFile/transferredBytes；新增 `GET /api/sync/tasks/{id}/progress`；SyncView 详情弹窗每 2s 轮询，进度条 + 当前文件 + 文件数 + 字节 + 速率（两次采样差值计算）；提交 `fde3c24`。
- **#5 同步日志列增强**：`publicSyncTask` 增加 `nextRunAt`（周期任务按 cron 计算下次执行时间）；SyncView 日志区改为表格列：开始时间/结束时间/状态（进行中/已结束）/下次执行（仅周期）/文件数/大小/详情（点击展开逐文件明细）；提交 `fde3c24`。
- **#6 日志时间筛选 UI**：两个独立 datetime-local 标签改为一个「时间范围」按钮 + 下拉面板（开始/结束两个 datetime-local，留空端不限），三语 `logs.timeRange`；提交 `b4b04c2`。
- **#7 分页增强**：后端 listShares/listCollections/listShareGroups 内存分页（page/pageSize/total，pageSize 上限 100）；前端我的文件/我的分享（含聚合分享两表）/我的收集/日志四页 pageSize 选择器（10/20/50/100，localStorage 记忆）+ 页数过多（>7）时页码输入跳转；提交 `4186425`/`7e23a4d`。
- **#8 我的文件目录与文件合并展示**：删除独立 folder-list 区块，目录行并入文件表格（目录在前，Folder 图标 + 「目录」类型列 + 名称点击进入下一级，重命名/删除按钮保留，checkbox 与文件操作对目录隐藏）；提交 `47d1b73`。
- 补测试：`TestShareGroupMemberManagementAndEdit`（httpapi 成员增删/编辑/越权 404/过去时间 400）、`TestShareGroupMemberCRUDAndAttributeEdit`（store，含修复属性编辑过去时间守卫）、`TestPaginationCapsPageSize`、`TestNextSyncRunTime`；提交 `e14881c`。

## 2026-09-01 - v017（10 项新需求批次）

任务书：`docs/CODEX_TASK_v017.md`；codex 分批实施（每批 1-2 项，每批独立提交推送）；前端纯改 5 批 + 前后端混合 1 批。

- **#1 文件库移除收集区块**：FilesView.vue 删除 `collection-section` 区块、"我的收集"入口按钮、两个收集弹窗及全部收集状态/函数（收集已由独立页 CollectionsView + /collections 路由承载）；提交 `3945fba`。
- **#2 管理后台各页签说明文案**：AdminView 页面顶部说明改为按 activeTab 动态取键（admin.copyOverview/Users/Security/Brand/Locks/System），三语与各页签实际功能匹配；提交 `3ad7e51`。
- **#3 导航顺序调整**：顶栏顺序改为 我的文件 → 我的收集 → 我的分享 → 同步任务 → 日志 → 系统设置；`nav.shares`/`page.shares`/`shares.heading` 改「我的分享」，`nav.admin` 改「系统设置」（nav.system 键）；提交 `8ddd6f5`。
- **#4 操作日志时间范围筛选**：GET /api/logs 支持 from/to（RFC3339 校验，非法 400），store `ListAuditLogs` 增加 created_at >= / <= 范围条件（含边界）；LogsView 增加开始/结束 datetime-local 输入，空值不传参；补 store/httpapi 两层单测（`TestAuditLogsTimeRangeFilter`/`TestLogsEndpointTimeRangeFilter`）；提交 `c0b7814`/`540aeac`/`aff8d04`。
- **#5 同步任务密码入库+可查看**：凭据 AES-GCM 加密入库与编辑留空保留（`__keep__`）为既有能力；新增 `GET /api/sync/systems/{id}/secret`（仅属主/admin，解密返回 secret/authPassphrase 一次，审计 sync_system_secret_view）；SyncView 密码框显示"已保存"占位 + 眼睛图标查看/隐藏；补 `TestSyncSystemSecretEndpoint`；提交 `9598687`。
- **#6 同步任务执行历史**：任务详情弹窗日志条目展示开始时间/结束时间（finishedAt 存在时）/结果（running/success/failure）；与 #9 的 sync_logs 结构协同；提交 `9598687`。
- **#7 同步目录选择增强**：picker 增加当前目录名称过滤（本地/远端）；手动输入完整路径 + 确认（远端路径 browse 校验，失败留在弹窗提示）；三语 `sync.filterPlaceholder`/`enterPath`/`confirmPath`/`invalidRemotePath`；提交 `1a51521`。
- **#8 同步任务列表显示目标**：任务行目标侧显示 SFTP `host:port` 或 FileBox URL host（解析失败回退原始 url），无数据时系统名兜底；提交 `1a51521`。
- **#9 同步任务日志实时状态**：sync_logs 迁移新增 `finished_at` 列（重建表去掉 result CHECK，保留外键/索引/数据）；executeSyncTask 执行开始写 `running` 行、完成 UPDATE 同一条（`UpdateSyncLogResult`）；失败分支（owner 不可用/只读）直接写 failure 行不遗留 running；publicSyncLog 返回 finishedAt；补 `TestSyncLogRunningResultUpdate`/`TestSyncTaskExecutionCompletesLog`；提交 `05f045e`。
- **#10 修改密码弹窗化**：顶栏"修改密码"改为居中 modal（AuthenticatedTopbar 内实现，复用 modal-backdrop；旧密码/新密码/确认，提交 POST /api/auth/change-password，成功 saveSession + 提示）；强制改密（must_change_password）仍由路由守卫跳 /change-password 独立页；提交 `05f045e`。

## 2026-09-01 - v016 深入检视修复批次（38 项）

### 批次 1（严重）

- **V1-28 运行中备份一致性**：`admin backup` 备份前执行 WAL checkpoint，避免运行中归档得到空库；restore 激活前校验数据库可读且非空，拒绝空库归档。
- **V1-01 收集上传磁盘保护**：收集上传的 `init` 与 `chunk` 写入前均执行最小可用空间检查，空间不足返回 `DISK_FULL`，并接入请求级分片限速。

### 批次 2（实证中等）

- **V1-30 只读拦截**：`mkdirSyncSystem` 补充只读时段检查，避免在只读窗口发起远端建目录。
- **V1-29 秒传目录限定**：登录用户的秒传匹配限定目标目录；无目录时保留根目录兼容回退。
- **V1-03 分享下载计数**：分享下载先打开文件再扣次；物理文件不存在时不消耗次数；同一 token 的 Range 请求在 60 秒窗口内去重计数。
- **V1-04 收集槽位口径**：收集槽位改为 `complete` 成功时消耗，`init` 不再占坑；清理废弃任务时同步处理槽位。
- **V1-02 分享预览限额**：预览改为字节级限速，文本最多 64KB、其他类型最多 512KB，并对 Range 请求做截断。

### 批次 3（其余中等）

- **V1-05**：`complete` 与 `rename` 共享目录锁，避免并发时数据库记录与磁盘路径不一致。
- **V1-06/V1-07/V1-08**：cron 对错过的最近任务补跑；手动同步使用独立 context；SFTP 同步增加整体超时并确保挂起时释放锁。
- **V1-09**：批量 ZIP 增加总字节上限、匿名聚合 IDs 上限与磁盘空间检查，保持流式打包。
- **V1-31**：backup/restore 改用流式读写与流式 SHA-256，避免大文件整体载入内存。
- **V1-32**：新增 `DELETE /api/upload-tasks/{taskID}` 取消接口，删除任务和临时分片并释放配额预留；同步 pull 失败时回滚 pending 任务。
- **V1-33**：同步 goroutine 增加 `recover`，远端响应类型断言改为安全检查，畸形响应不再导致进程崩溃。

### 批次 4（轻微）

- **V1-10/V1-11/V1-12/V1-13/V1-14/V1-15/V1-16**：清理 `share_groups` 孤儿记录；统一 `fileCount` 为可用文件口径；logout 通过 `last_logout_at` 撤销 JWT；补齐 `files.uploadFailed`/`shares.revoked` 三语键；批量分享失败事务回滚；注册限制为 5/min；分片读取增加超时。
- **V1-17/V1-18/V1-19/V1-20/V1-21/V1-22/V1-23/V1-24/V1-25**：审计写入与清理解耦；TOTP counter 单调防重放；只读窗口覆盖改密和改语言；秒传冲突查询异常返回 500；撤销状态统一 404；pull 临时文件置于数据目录；校验 `secrets.json` 密钥长度；增加点击劫持防护头；前端仅对 401 清理会话并保留失败态。
- **V1-26/V1-27**：restore 强制要求 `filebox.db` 且校验必需条目；完成九项小修：收紧指纹格式、先 Close 再删临时文件、pull 磁盘预留当前文件大小、说明 `PosixRename` 回退语义、保护 retention=0 并限制上限、完善优雅关闭、限制 ready 文件批量读取、记录覆盖上传物理峰值说明、complete 校验分片哈希。
- **V1-34/V1-35/V1-36/V1-37/V1-38**：补改密审计行；保护唯一管理员不被降权/禁用并删除用户目录；支持不限次分享转为有限次数；升级 `golang.org/x/crypto` 至 v0.35.0；修正 admin 按任意用户存储路径前缀过滤目录。

## 2026-08-31 - v014（7 项用户问题修复批次）

- **#1 管理后台概览文案**：根因为 `admin.heading` 键值即"工作区管理"；三语改为 管理后台/管理後台/Admin console，eyebrow 同步（ADMIN / 管理后台）。
- **#2 导航文案**：`nav.sync` 三语改为 同步任务/同步任務/Sync tasks（`page.sync` 保留"同步中心"）。
- **#3 filebox 目标系统密码字段**：SyncView filebox 分支新增密码输入框（编辑留空=保留凭据）；后端 create/update 校验 filebox 必须提供密码，返回明确提示「FileBox 目标系统必须设置账号密码」。
- **#4 根目录与选文件**：路径 picker 两侧统一支持选文件（includeFiles=1，sourceKind=file）；源/目标路径空串=根目录（后端放行），表单去除 required 加根目录占位，错误经 formError 展示。
- **#5 目标系统连通性测试**：新增 `POST /api/sync/systems/{id}/test`（SFTP openSFTP+Stat / filebox openFileBox+auth/me，返回 ok/latencyMs/testedAt，失败信息脱敏不泄漏凭据）；`remote_systems` 迁移加 `last_test_at`/`last_test_result` 持久化；前端系统表「测试连接」按钮+结果徽标+时间。
- **#6 聚合分享编辑**：新增 `PUT /api/shared-groups/{token}/extend`（1..87600h 不缩短）与 `increase`（maxDownloads 不降低）；store 两方法；审计 share_group_extend/increase 登记 logActions；SharesView 聚合分组延期/增次弹窗。
- **#7 我的收集独立页**：新建 CollectionsView.vue + `/collections` 受保护路由（置于 /:token 前）；顶栏收集入口改 RouterLink to=/collections；FilesView 内嵌区块保留兼容。
- 验证：go build/vet/test 全绿；go test -race（httpapi+store）无竞态；DSH 实测连通性测试（成功/失败）、聚合分享 extend/increase、filebox 密码必填、根目录任务保存均通过；npm build + sync-web 通过。提交 `6211349`。

## 2026-08-30 - v013（三轮修复 + 15 项需求批次）

- **第三轮安全加固（G1-G14，commit `7032402`）**：SFTP 指纹严格解码（拒绝 base64 padding-bit 损坏、常量时间比较）；sharePreview 匿名限速 30/min；collectionUploadChunk 补 owner 只读检查；覆盖上传原子替换（旧文件保留至 complete 成功，消除路径复用竞态）；RenameFolder 预检+失败反向修正+per-user 目录锁；限速器三桶容量上限 10000；backup 外层 0600+O_EXCL、restore 解压限额+拒绝重复条目；`--jwt-secret` 空值/短密钥校验；keys.json 明文警告；syncLocks 清理；前端 api() 统一 401 跳登录。
- **v013 需求批次（commit `e0b0051`/`3712dfb`/`080d21c`，依 `docs/requirements/TODO-v013.md`）**：
  - P0：#8 分享次数双提示去重；#1 传输失败提示 title 完整显示；#9 收集配额错误脱敏（`COLLECTION_QUOTA_EXCEEDED` 不泄露明细）；#4 同步目录上级导航（SFTP 根 `/` + filebox 逐级上级）；#2 传输记录 sessionStorage 持久化 + 重选文件续传。
  - P1：#3 同步源支持选文件（sourceKind）；#6 表单控件统一 40px；#10 外部上传日志带 owner/目录（token 脱敏）；#11 收集编辑 `PUT /api/collections/{id}`（到期/次数/上限，撤销不可编辑、下调须≥已用）；#12/13/14 AuthenticatedTopbar 共享顶栏统一全部视图。
  - P2：#5 filebox↔filebox 同步（kind/url + HTTP adapter + SSRF 防护，MVP）；#7 批量分享聚合链接（share_groups + `/g/:token` 公开页 + 单文件/ZIP 匿名下载）；#15 SQLite WAL + synchronous=NORMAL 速率优化。
- DSH 复核：go test 全量 + go test -race（httpapi/store）全绿；实测聚合分享（meta/单文件/ZIP）、收集配额脱敏、收集编辑均通过。

## 2026-08-30 - v012 批次 G（功能 11：同步功能 filebox↔sftp）

- **目标系统（remote_systems）**：独立配置（名称/主机/端口/用户名/密码或密钥含 passphrase），凭据 AES-GCM 加密存储（密钥派生自 JWTSecret）；CRUD；被任务引用时拒绝删除；`/systems/{id}/browse` 远端目录浏览、`/mkdir` 建目录（复用已存凭据）。
- **同步任务（sync_tasks）**：direction push/pull、remote_system_id、源/目标路径、conflict_policy（overwrite/skip/rename）、schedule_type（once/periodic）+ cron 5 段、enabled、last_run/last_result；CRUD；`/tasks/{id}/run` 立即执行（同任务互斥防并发重叠）；越权 404。
- **执行引擎**：push = FileBox 文件遍历 → SFTP 自动建目录层级上传；pull = SFTP 递归下载 → FileBox 自动建目录、计入属主配额；每文件进度写入 sync_logs.detail。
- **调度器**：后台 goroutine 每分钟扫描 enabled+periodic 任务（robfig/cron 匹配）自动触发；重启恢复；`PruneSyncLogs` 按 LogRetentionDays（默认 30 天）清理。
- **前端**：SyncView 新页面（任务列表/新建编辑弹窗/目标系统管理/详情日志）、SFTP 目录浏览、cron 输入+常用预设；i18n 三语 631 键。
- 验证：go test/vet/build 全绿；**DSH 在 Linux 真实环境完成端到端验证**——密码/密钥/密钥+passphrase 认证、push（自动建目录）、pull（子目录保留+入库）、cron 周期自动触发（整分钟）、冲突 skip/overwrite、越权 404、凭据加密；本地 regress 26/26、batchC 17/17 无回归。提交 `d1c5b0c`。

## 2026-08-30 - v012 批次 F（功能 5：批量分享 + 下载详细进度）

- **批量分享**：新增 `POST /api/files/batch-share`，一次校验整批归属后为每个文件创建独立链接，统一应用有效期/下载次数参数；审计动作 `batch_share`，结果同步出现在分享管理页。
- **详细下载进度**：单文件和批量 ZIP 下载都由前端按响应流逐块读取，传输抽屉显示已传输字节、总字节、百分比和 B/KB/MB/GB 每秒速率，并支持 `AbortController` 取消；批量 ZIP 响应补充 `Content-Length`。
- **前端**：FilesView 新增批量分享弹窗、逐文件链接复制和三语 i18n 文案。

## 2026-08-30 - v012 批次 E（功能 8/9：分享管理 + 分享下载日志）

- **功能 8（分享管理）**：新增 `/shares` 分享管理页与 API——`GET /api/shares`（我的分享列表：文件名/token/有效期/下载次数/状态/剩余时间）、`GET /api/shares/{token}`（详情）、`PUT /api/shares/{token}/extend`（延期，仅创建者/管理员）、`PUT /api/shares/{token}/increase`（增次，不允许降低）、`DELETE /api/shares/{token}`（单条撤销，软撤销）、`GET /api/shares/{token}/logs`（下载日志）；越权一律 404。
- **功能 9（分享下载日志 + 失败原因细分）**：`shares.revoked_at` 软撤销（匿名访问 403 分享已撤销、审计 share_revoked）；`audit_logs.share_owner_id` 列让分享者可见自己分享的匿名下载日志（查询 user_id=me OR share_owner_id=me）；失败原因枚举补全 `share_not_found`/`share_expired`/`share_revoked`/`share_limit`/`share_denied`；logActions 补 `share_extend`/`share_increase`/`share_revoke`。
- **前端**：SharesView 管理页（列表/详情/延期/增次/撤销/复制链接/下载日志）、FilesView 分享入口联动、LogsView 普通用户可见分享相关动作；i18n 三语。
- 验证：go test/vet/build 全绿；DSH 实测批次 E 专属测试 17/17（列表/详情/延期/增次/降次被拒/下载限制/日志含 limit/越权 404/撤销后 403 share_revoked/日志含 revoked）+ share 24/24、regress 26/26 无回归。提交 `d687801`。

## 2026-08-30 - v012 批次 D（功能 6 第一部分：系统内用户只读时段）

- **功能 6 第一部分（用户只读时段）**：管理员对单个用户设置一次性只读窗口（users 表 `read_only_from`/`read_only_until` 列，migrate 自动补）；`PUT /api/admin/users/{id}/read-only` 设置/清除（起止倒置或格式非法 400 INVALID_READ_ONLY_WINDOW）；`/api/auth/me` 与管理端用户列表返回 `readOnlyFrom`/`readOnlyUntil`/`readOnly`。
- **服务端强制**：`rejectReadOnly` 统一拦截全部写操作——uploadInit/uploadChunk/complete（含秒传与批量）、deleteFile/batch-delete、createFolder/renameFolder/deleteFolder、createShare（12 处入口），返回 403 `READ_ONLY`「当前账号处于只读时段，仅可查看和下载」，审计 reason=read_only；管理员豁免（`userReadOnlyAt` 含边界判定，非法窗口按非只读处理）。
- **前端**：AdminView 用户编辑弹窗新增「只读时段」开始/结束时间字段（datetime-local，可清除）；FilesView `me.readOnly=true` 时禁用上传/删除/目录/分享入口并显示提示条；i18n 三语 `readOnly.*` 键。
- 验证：go test/vet/build 全绿；DSH 实测批次 D 专属测试 8/8（设置/me 只读/全部写操作 403/查看下载正常/管理员豁免/清除恢复/起止倒置 400）+ regress 26/26、batchC 17/17 无回归。提交 `1e6a2c9`。

## 2026-08-30 - v012 批次 C（功能 6 第二部分：外部用户上传收集链接）

- **功能 6（外部用户上传收集）**：需求澄清为「分享给外部用户时允许外部上传」（非系统内用户配额），设计文档 `docs/design-upload-policy.md` 拆两部分并确认。本批次实现第二部分：
  - 新表 `upload_collections`（token 64 位随机、created_by、name、expires_at、max_uploads、max_file_bytes、upload_count、status、created_at、revoked_at）+ `upload_collection_files`（collection_id、file_id、original_name、remark、created_at）。
  - API：`POST/GET /api/collections`、`GET/DELETE /api/collections/{id}`（+`/files`）、`GET /api/collections/{token}/meta`（匿名）、`POST /api/collections/{token}/upload-init|upload-chunk|upload-complete|upload-status`（token 鉴权，复用分片链路）。
  - 限制：有效期（过期 403 COLLECTION_EXPIRED）、总上传次数（原子预留，超限 403 COLLECTION_LIMIT）、单文件大小（413 COLLECTION_FILE_TOO_LARGE）、撤销（403 COLLECTION_REVOKED）；文件落入创建者 `uploads/<token>/` 子目录，计入创建者配额（超配额 413 QUOTA_EXCEEDED）；秒传（目录内 MD5/SHA256 匹配）；上传者可选「备注」（标签无「姓名」字样）；匿名上传 IP 限速（`limiterForPublic` IP+collection 键）；审计 `collection`/`upload_collect`/`upload_collect_fail`。
  - 前端：公开上传页 `UploadView`（路由 `/u/:token`，拖拽/多文件/进度/秒传/已收列表/备注输入）；FilesView「上传收集」入口（创建弹窗 + 我的收集列表 + 撤销 + 已收文件查看）；i18n 三语。
  - 验证：go test/vet/build 全绿；DSH 实测批次 C 专属测试 17/17（创建/参数校验/匿名 meta/分片上传闭环/次数超限/详情备注/越权 404/单文件超限/撤销后 403/文件树可见）+ 过期场景（Go 辅助置过期 → 403 COLLECTION_EXPIRED）；regress 26/26、transfer 19/19、share 24/24、folders 21/21 无回归。提交 `80a71df`。
- **功能 6（第一部分：用户只读时段）**：设计已确认（一次性起止窗口、禁止全部写操作、并入用户编辑弹窗、前端禁用入口+提示条），codex 批次 D 开发中。

## 2026-08-30 - v012 批次 B（功能 3/4/10：创建用户安全项 + 品牌布局 + XFF 开关）

- **功能 3（创建用户直接设置 TOTP/IP 白名单）**：`POST /api/admin/users` 请求体新增 `totpEnabled`/`reenroll`/`ipAclEnabled`/`ipWhitelist`——TOTP 启用时生成一次性 secret（enabled=true 且响应含 `totpSecret` 供管理员转交；仅 reenroll 时生成 secret 但 enabled=false，用户下次登录重绑），IP ACL 创建即应用（`normalizeWhitelist` 校验）；前端创建用户弹窗新增「安全设置」区块，提交带新字段，TOTP 启用时展示 secret。后端回归测试覆盖（`TestCreateUserSecuritySettings`）。
- **功能 4（品牌设置布局优化）**：品牌页签「界面主色」取消独占一行，改为紧凑两列网格（主色与 ICP/版权同排），整体布局更紧凑；i18n 无新增键。
- **功能 10（公网 XFF 信任开关，默认关）**：settings 新增 `trustProxy`（默认 false，migrate 自动补默认值）；仅当开关开启**且**请求直连 IP 落在 `--trusted-proxies` 白名单内时，`requestIP` 才解析 `X-Forwarded-For`（从右向左取第一个非可信 IP）；AdminView「系统设置」页签新增「信任 X-Forwarded-For」复选框；`TestRequestIPRequiresTrustProxySetting` 单测。
- 批次 B 验证：go test/vet/build 全绿；18081 回归套件（regress 26/26、transfer 19/19、share 24/24）无回归；提交 `58910e9`。

## 2026-08-30 - v012 补充批次（问题 8 补强 + 优化项 + Linux 部署验证）

- **问题 8（拖拽补强）**：`/api/brand` 公开返回 `maxFileSize`；`queueFiles` 前置校验——超过单文件大小上限的文件跳过并提示（「N 个文件超过单文件大小上限 M，已跳过」），批量 >50 文件时确认提示（「共 N 个文件，确定全部上传吗？」）；多文件/多目录拖拽（webkitGetAsEntry 递归）原有能力保留。
- **优化项 1（废弃上传任务定时清理）**：后台协程每小时清理超过 24 小时未完成的 pending 上传任务（`ListExpiredUploadTasks` 按 created_at 过期 + `DeleteUploadTask` 事务删除任务与分片记录 + 移除 tmp 目录），优雅关闭时停止；已验证（构造 25h 旧任务 → 找到并删除 → 清空）。
- **优化项 2（服务端主动推送上传进度）**：`GET /api/files/progress/stream` SSE 端点每秒推送当前用户所有 pending 任务进度（taskId/name/totalChunks/uploaded/status）；前端 FilesView 使用带 Bearer 认证且可取消的流式 fetch 订阅，刷新后/多标签页同步恢复进度，随组件卸载关闭连接。
- **二次检视修复（e811c8e）**：FilesView `reactive` 导入（阻断）、resume/retry 走并发闸门、fileIcon 分支顺序（FileCode/FileSpreadsheet 前移）、batchDownload zip 同名序号、秒传审计 target 用本次上传名。

## 2026-08-30 - v012 用户反馈批次（问题 2-12 + codex 安全检视修复）

- **问题 2+9（秒传与同名冲突协调 + 秒传审计）**：`checkInstantUpload` 接受 `name`+`dir`——目录内存在同名 ready 文件时返回 `conflict:true`（前端走冲突弹窗，覆盖/重命名），仅目录内无同名时才返回 `instant:true`（秒传）；修复"重复上传同名文件永远秒传、从不弹窗、不创建新文件"；秒传命中补审计（`recordAudit("upload", name, "success", "instant")` + serviceEvent）。同内容不同名仍秒传，同内容同名触发弹窗（可重命名创建多份）。
- **问题 3（文件夹上传并发闸门）**：`queueFiles` 经 `runGated` 并发闸门（同时最多 3 个文件进入校验/初始化），避免大批量文件夹上传时同源连接数超限导致部分文件误报「网络连接失败」。
- **问题 4（文件类型图标）**：`fileIcon` 按 MIME/扩展名映射图片/视频/音频/压缩包/JSON/表格/可执行/代码等图标，未知类型回退默认 `File` 图标。
- **问题 5+6（日志分页与筛选）**：分页增加页码数字按钮（当前页前后 2 页 + 首末页 + 总页数）；`/api/logs/actions?usedOnly=true` 返回当前用户实际存在的动作类型（store `ListUsedActions`），普通用户不再看到从未触发的「系统配置」筛选项；前端异步加载筛选项并显示「加载中…」占位。
- **问题 7（修改密码文案）**：顶栏改密入口带 `?mode=self`；ChangePasswordView 按 `mode` 区分——主动改密显示「修改密码」（ACCOUNT SECURITY），强制改密（守卫/403 跳转）仍显示「先更新初始密码」；i18n 三语 `password.self*` 键。
- **问题 10（新建用户按钮位置）**：「新建用户」按钮从页签外 page-heading 移入「用户管理」页签 toolbar，不再每个页签显示。
- **问题 11（多选聚合下载）**：新增 `POST /api/files/batch-download`（zip 打包，校验归属，任一越权整体拒绝，审计 reason=batch）；前端文件行复选框 + 全选 + toolbar「聚合下载（N）」按钮。
- **问题 12（迁移部署文档）**：README×2 部署指南新增「v010 → v011 迁移部署」章节（停服/备份/迁移命令/启动/回归）。
- **安全修复（codex 检视）**：① preview 白名单移除 `text/html` 与 `image/svg+xml`（存储型 XSS 面）——HTML/SVG 文件强制附件下载；② 默认 JWT secret 启动时打印生产警告（`--jwt-secret`/`FILEBOX_JWT_SECRET` 未设置时）；③ 修复 `startUpload` 冲突分支 `init` TDZ 错误（`let init = null` 提前声明）。

## 2026-08-30 - v011 验证反馈修复批次（问题 1-20，随 v0.2.0 交付）

### 批次 1（问题 1-6：用户实测反馈 + 自定义目录）

- 登录页不再展示初始默认账号提示：移除 LoginView 的 `login-foot` 渲染，并删除 zh-CN/zh-TW/en 三字典中的 `login.defaultAdmin` 键（不再引用，无残留）。
- 新建用户改为居中 modal 弹窗（复用 modal-backdrop/modal-panel 模式），错误内联显示紧邻「创建账号」按钮，提交成功刷新列表；服务端补 `user_create` 失败审计（含原因）。
- 拖拽目录不再误报「网络连接失败」：`handleDrop` 用 `webkitGetAsEntry()` 递归识别目录条目，空目录等无效拖拽给出明确中文提示「拖拽内容不包含可上传的文件」。
- 新增独立传输面板：顶栏「传输」按钮（带进行中数量角标）展开右侧抽屉，区分「上传」与「下载」两组；上传项保留暂停/继续/重试，下载项新增流式进度（按 Content-Length 计算百分比）。
- 文件列表 md5 直接展示 + 开关：完整性列默认直接显示 `file.md5`（悬停 title 含完整 MD5/SHA-256）；工具栏「显示 MD5」开关持久化到 `localStorage(filebox_show_md5)`，默认显示。
- **问题 6（用户自定义目录）**：移除自动年月目录层，存储结构改为 `data/files/<user_id>[/<自定义目录>/]<stored_name>`；新增 `folders` 表与目录 CRUD API（创建/列表/重命名级联/删除非空保护）；上传目标 = 当前目录；列表 `dir` 过滤 + 面包屑导航；用户隔离、配额按用户；`filebox admin migrate-v010-paths` 迁移旧 `yy/mm` 结构（备份 DB、物理移动、storage_path 重写、登记目录，幂等）。

### 批次 2（问题 7-9：logo 跳转 + 单文件交付 + 部署文档）

- **问题 7**：`BrandLogo.vue` 增加 `link` prop（RouterLink 包裹 + cursor），文件/管理/日志/分享页顶栏 logo 点击跳转首页 `/`；登录页与品牌面板预览不加。
- **问题 8**：Makefile 新增 `release` 目标（CGO_ENABLED=0、-trimpath、-ldflags="-s -w"，Windows/Linux amd64 + SHA256SUMS）+ Windows 等价脚本 `scripts/release.ps1`；已实际执行验证（Linux 静态 ELF、校验和正确）。
- **问题 9**：README.md/README.en.md 新增「部署指南（单文件交付）」章节（Windows/Linux 分步 + systemd + Nginx 反代 + 生产注意事项）。

### 批次 3（问题 10-13：日志配色 + 系统配置分组 + 改密/TOTP 重绑 + 版权）

- **问题 10**：`.result-label.success` 固定绿色（`#1e7e34/#e8f5ec`）不随主题色；失败固定红（`#a83e2d/#fff0ed`）。
- **问题 11**：`logActions` 扩为完整 24 项动作集；LogsView `actionLabel` 三语文案补齐；筛选下拉按「业务/系统配置」分组（optgroup）。
- **问题 12**：顶栏「修改密码」入口（跳转 /change-password，普通用户可自助改密）；`PUT /api/admin/users/{id}/totp` 支持 `reenroll`（新 secret 且 enabled=false，用户下次登录重绑）；AdminView「要求下次重新绑定 TOTP」复选框。
- **问题 13**：store `brand_copyright` 键 + `BrandSettings.Copyright`；`/api/brand` 返回 `copyrightText`；`PUT /api/admin/brand` 支持 `copyrightText`/`clearCopyright`；AdminView 品牌面板输入框；BrandFooter 渲染版权（空值不渲染空白页脚）；i18n 三语。

### 批次 4（问题 14-17：管理后台页签化 + 用户弹窗 + 日志周期迁移 + 页脚品牌）

- **问题 14**：AdminView 左侧竖菜单 + 右侧内容区，六页签（概览/用户管理/安全设置/品牌设置/锁定管理/系统设置）；`?tab=` 深链 + 刷新保持；页签内容用 `v-show`（保留 DOM），切换不丢已填内容（修正验收点）。
- **问题 15**：新建/编辑用户改为居中 `modal-backdrop/modal-panel` 弹窗（遮罩点击关闭，角色/配额/重置密码/禁用/TOTP 重绑/IP 白名单字段齐全，保存仍走三请求）。
- **问题 16**：LogsView 移除 logRetentionDays 面板；日志保存天数迁入 AdminView「系统设置」页签（复用 `PUT /api/admin/settings`，后端零改动）；清理 `logs.retention*` 残留键。
- **问题 17**：BrandFooter 首行 `siteTitle` + 小字 `siteDescription`，随后版权/ICP/公安备案，任一非空即渲染。
- **D-S2-3 修复（目录重命名 + 软删除记录冲突 500）**：`RenameFolder` 改写 storage_path 前清理目标前缀下 `status='deleted'` 记录；`isUniqueError` 只匹配 `UNIQUE` 约束失败（避免 FK 误判）；新增单测 `TestRenameFolderClearsDeletedRows`。

### 批次 5（问题 18-19：并发冲突队列 + 上传失败日志 + 配额/超限明细）

- **问题 18**：前端冲突弹窗改**冲突队列**（数组依次弹出 + 60s 超时取消），并发同名不再互相覆盖卡「准备中」；失败/取消项保留在传输抽屉（红色状态 + 原因 + 重试/移除）；后端 `uploadInit`/`uploadChunk` 失败分支补 `recordAudit` + `serviceEvent`（reason 细分 invalid_name/too_large/conflict/disk_full/quota_exceeded/task_not_found/invalid_index/rate_limited/size_mismatch/settings_failed/invalid_request/…，含 JSON 解码失败与分片序号解析失败等前置分支），`logActions` 补 `upload_init`/`upload_chunk`；重命名后用户可见 name 跟随序号。codex 独立复核确认全部分支覆盖。
- **问题 19**：store `QuotaError`（usedBytes/quotaBytes/fileSize）；配额拒绝 403 + `QUOTA_EXCEEDED` 明细；`max-file-size` 超限独立 `413 FILE_TOO_LARGE` + maxFileSize；前端 `localizeError` 映射（「配额不足：当前已用 X / 总配额 Y，文件需 Z，超出 W…」「文件超过单文件大小上限 M」）+ i18n 三语。

### 批次 6（问题 20：整体传输速率）

- **问题 20**：传输侧边栏顶部「整体速率」（所有进行中上传合计）；每项维护 `loadedBytes`，1s 采样 + 3 点滑动平均；单位自适应（B/KB/MB/GB per s）；无传输隐藏；定时器随组件卸载清理；i18n 三语（`files.overallRate`）。

## 2026-08-30 - 用户实测反馈批次（并入阶段二 v0.2.0 交付）

- 登录页不再展示初始默认账号提示：移除 LoginView 的 `login-foot` 渲染，并删除 zh-CN/zh-TW/en 三字典中的 `login.defaultAdmin` 键（不再引用，无残留）。
- 新增独立传输面板：顶栏「传输」按钮（带进行中数量角标）展开右侧抽屉，区分「上传」与「下载」两组；上传项保留暂停/继续/重试，下载项新增流式进度（按 Content-Length 计算百分比），面板可随时展开/收起。
- 文件列表 md5 直接展示 + 开关：完整性列默认直接显示 `file.md5` 值（悬停 title 含完整 MD5/SHA-256）；工具栏「显示 MD5」勾选开关控制该列展示方式，选择持久化到 `localStorage(filebox_show_md5)`，默认显示。
- 上传目录入口补齐 + 拖拽容错：点击「上传文件夹」与拖拽目录两条入口均走目录上传（webkitdirectory / webkitGetAsEntry 递归，回退 webkitRelativePath）；拖拽内容不含可上传文件（如空目录）时给出明确中文提示「拖拽内容不包含可上传的文件」，不再静默；上传/下载错误区分真实网络失败（映射「网络连接失败」）与业务错误，避免误导。
- 新建用户流程复核：按钮→表单→提交→API→列表刷新的前端链路完整，无 JS 错误；后端 `POST /api/admin/users` 已由 DSH 用例（S04/S05/F500，201）覆盖验证。（用户初始「无反应」为浏览器缓存旧页面所致，非代码缺陷。）

## 2026-08-29 - Stage 2 (v0.2.0) batches

### Batch A - resumable chunked upload, instant upload, folder upload (backend)

- Added the `chunks` table and `SetChunk`/`ListChunks`/`DeleteChunks` so uploaded chunk state is the durable source of truth for resuming uploads across server restarts.
- `upload-init` now accepts multi-chunk layouts: chunk sizes of 2–8MB for files larger than the chunk size, single-part (including zero-byte) otherwise; backward compatible with the Stage 1 client.
- `PUT /api/files/{taskID}/chunks/{index}` supports arbitrary chunk indexes with exact-size validation, idempotent overwrite, per-chunk SHA-256 recording, and streamed writes.
- Added `GET /api/files/{taskID}/status` returning the uploaded chunk list for resume.
- `complete` verifies every chunk record and temp file, then streams a sequential merge while computing MD5 and SHA-256, with an atomic rename into place.
- Added `POST /api/files/check` for instant upload: MD5-first, SHA-256-fallback matching within the current user; hits return the existing record without writing new content.
- Added the optional `dir` field to `upload-init` with strict validation (rejects traversal, control characters, and Windows-illegal characters); folder uploads preserve relative paths and identical names in different directories remain unsuffixed.

### Batch B - share links, online preview, rate limiting, registration, statistics (backend)

- Added the `shares` table and share endpoints: `POST /api/files/{id}/share`, `GET /api/files/shared/{token}/meta`, `GET /api/files/shared/{token}/download` (atomic slot consumption, expiry/limit enforcement), and `DELETE /api/files/{id}/shares`; share create/view/download are recorded in audit and service logs.
- Added `GET /api/files/{id}/preview` serving inline content only for the MIME whitelist (image/text/video/pdf/json) and attachment for everything else.
- Added a per-user token bucket (`golang.org/x/time/rate`) with an idle-eviction cache; `uploadRateLimit` (bytes/sec, 0 = unlimited) is configurable through admin settings and enforced before chunk writes.
- Added `POST /api/auth/register` gated by the `registerEnabled` setting (default off, `--register-enabled` seeds first deployment), enforcing the password policy, returning a login token, and exposing `registerEnabled` via `/api/brand`.
- Extended admin stats with active share count and cumulative share downloads; added the `register` audit action.

### Batch C - frontend (Vue3)

- Reworked the file page uploader: 4 concurrent chunk workers, per-file progress, pause/resume (resume uses the status endpoint to send only missing chunks), retry with exponential backoff, folder picker + recursive directory drag-and-drop, and WebCrypto SHA-256 instant-upload checks.
- Added the share dialog (expiry/max downloads, copyable link, revoke), the preview modal (image/video/pdf/text), and the anonymous `/:token` share page.
- Added the registration entry to the login page driven by the public `registerEnabled` flag.
- Extended the admin page with share statistics and registration/upload-rate settings (KB/s), and added all new UI strings to the zh-CN/zh-TW/en dictionaries.

### Defect fixes found during DSH acceptance testing

- **D-S2-1 (一般, storage reuse)**: re-uploading a file name that had been soft-deleted returned HTTP 500 (`UNIQUE constraint failed: files.storage_path`). Fixed in `allocateStorageName`: the soft-deleted row holding the target storage path is now removed when the path is reused, and a regression test (`TestReuploadAfterDeleteReusesStoragePath`) was added.
- **D-S2-2 (轻微, CLI)**: `filebox admin reset-password` dropped its first flag (parsed `args[1:]` again), so `--data` was ignored. Fixed to parse all remaining flags and covered by `TestResetPasswordParsesDataFlag`.

## 2026-08-29 - D1-D4 defect fixes

- Fixed same-directory rename placement to use `name (n).ext` while preserving the extension within the 255-byte storage-name limit.
- Completed the zero-byte single-part upload path and verified empty-file MD5/SHA-256 values and on-disk storage.
- Added `admin clear-ip-acl` for local recovery from an accidentally restrictive source-IP allowlist, with structured CLI service logging and standard exit codes.
- Changed admin settings updates to merge omitted numeric/boolean fields with persisted values and return field-specific validation messages for invalid settings.

## 2026-08-29

- Delivered Stage 1 MVP from `CODEX_TASK_1.md`.
- Added the confirmed MD5 requirement alongside SHA-256; completed file records and APIs expose both values.
- Kept all Stage 2 items out of this delivery: resumable/chunked transfer, instant upload, folders, sharing, previews, rate limiting, and public registration.
- Added a Vue 3 frontend and embedded production bundle workflow.
- Implemented R-DISK: cross-platform disk usage stats, configurable minimum free space, and DISK_FULL upload protection.
- Implemented R-NAME: sanitized original disk names, per-user/per-month storage directories, transactional same-directory suffix allocation, and unique storage paths.

## 2026-08-29 — 第二批变更

- Implemented R-VALID: upload-init now rejects path separators, control characters, Windows-illegal characters, dot traversal markers, empty/dot names, and names over 255 bytes.
- Implemented R-CONFLICT: same-directory collisions return the existing file metadata for a user choice; overwrite removes the old record/content and corrects used bytes, while rename allocates a numbered name.
- Implemented R-LOG: audit log schema, lazy retention cleanup, login/upload/download instrumentation, paginated log APIs, user isolation, admin user filtering, and retention settings.
- Implemented R-LOCK: configurable failed-login lockout, automatic unlock, uniform login errors, failure reasons in audit logs, and admin lock reset on user edits.
- Added the authenticated `/logs` frontend page, admin retention controls, and the upload conflict resolution dialog.

## 2026-08-29 — 第三批变更（R-BRAND）

- Implemented configurable brand settings for site title, SEO description, ICP/public-security filing text, favicon, login logo, and main logo.
- Added public `/api/brand` and `/brand/*` resource endpoints with fixed-name storage under `data/brand`, strict extension/size/content validation, atomic writes, and embedded SVG fallbacks.
- Added the admin multipart brand panel with previews, per-resource removal, text clearing, and confirmed full reset; runtime SEO, logos, and conditional filing footer update immediately.

## 2026-08-29 — 文档与注释收尾

- Added Chinese-first, English-second bilingual comments for exported Go APIs and the authentication, quota, conflict, filename validation, disk protection, hashing, transactional storage, Range download, audit, and branding paths.
- Added bilingual `README.md` / `README.en.md` and `RELEASE_NOTES.md` / `RELEASE_NOTES.en.md`, including the Stage 1 feature markers, configuration defaults, API/storage summaries, deployment guidance, security notice, and Stage 2 limitations.

## 2026-08-29 — 第四批变更（R-LANG）

- Implemented the `users.language` migration, authenticated language update endpoint, and `defaultLang` system setting with validation and public brand exposure.
- Added lightweight three-dictionary frontend i18n for Simplified Chinese, Traditional Chinese, and English, including runtime switching, local persistence, user preference syncing, locale-aware dates, page titles, and localized API errors.
- Converted login, files, admin, logs, navigation, dialogs, upload states, empty states, and branding controls to i18n keys; added language selectors for public and authenticated views.

## 2026-08-29 - Fifth batch (R-THEME)

- Added persisted `theme_color` settings with `#RGB`/`#RRGGBB` validation, default fallback, and public `themeColor` exposure through `/api/brand`.
- Added admin hex input, native color picker, default reset, and immediate CSS variable/meta updates for buttons, links, progress, focus rings, selected states, and status accents.

## 2026-08-29 - Sixth batch (R-INIT / R-PWD / R-TOTP / R-IPACL / R-IPBAN / R-LOCKADMIN)

- Added configurable initial administrator credentials and forced password changes, including a protected change-password route and admin reset behavior.
- Added configurable password length and character-class policy enforcement for user creation, password resets, and self-service changes.
- Added encrypted TOTP secrets, RFC 6238 setup/login challenges, QR generation, one-time setup secret display, and replay protection.
- Added per-user IPv4/IPv6 IP/CIDR allowlists and trusted-proxy-aware source-IP resolution.
- Added sliding-window IP login lockout with configurable thresholds, automatic unlock, audit reasons, success reset, and admin lock removal APIs/UI.

## 2026-08-29 - Seventh batch (R-OPS)

- Added `admin reset-password` with explicit or generated one-time passwords, forced password change, and lock-state reset.
- Added `locks list` and `locks clear` for IP and user lock recovery without HTTP authentication.
- Added a SQLite busy timeout so CLI maintenance can briefly coexist with the running service.

## 2026-08-29 - Eighth batch (R-SRVLOG / R-SERVICE / R-PROXY)

- Added optional service logging with console fallback, daily local-time files, gzip rollover, startup/rollover retention cleanup, and operator/source-IP structured events.
- Instrumented authentication, file operations, account administration, settings/branding, lock management, and R-OPS commands without recording passwords, tokens, or file contents.
- Added graceful SIGINT/SIGTERM shutdown events and `--log-enabled`, `--log-dir`, and `--log-retention-days` flags with environment-variable defaults.
- Added Linux systemd, Windows NSSM/sc, and Nginx HTTPS reverse-proxy examples plus bilingual deployment documentation under `deploy/`.
