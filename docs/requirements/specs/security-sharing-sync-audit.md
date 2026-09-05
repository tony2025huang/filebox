# FileBox v023 分享与同步安全审计

## 审计边界与方法

本次只读取当前仓库源码、已有测试与 git 状态，未修改 Go/Vue/配置/部署文件，未运行部署或 Release 操作。审计对象是分享匿名入口、聚合分享入口、同步任务执行链路和相关路径校验 helper。

证据引用均以项目根目录为基准，格式为：

`> 证据: <项目相对路径>:<行号> | "单行原文"`

仓库不存在 `scripts/verify_evidence.py`。已用 `rg`、带行号源码读取和测试源码读取手工回核下列引用；因此本报告不声称通过自动证据校验器。

## 结论摘要

| ID | 严重度 | 结论 | 要点 |
|---|---|---|---|
| H1-SHARE-PREVIEW | 高 | 已确认 | 匿名预览不调用扣次；非白名单 MIME 走 attachment 且 `previewContentLimit` 返回 0，可通过预览入口获得完整文件流。 |
| H1-SHARE-RANGE | 高 | 已确认 | 单文件和聚合分享的 Range 60 秒窗口在次数耗尽后仍放行重复 Range 请求。 |
| H1-GROUP-EXPIRY | 中 | 部分成立 | 聚合公开 meta 过期后仍返回聚合详情和 ready 成员元数据；单文件下载和 ZIP 下载入口有过期拒绝。 |
| H2-SFTP-PULL-PATH | 高 | 已确认 | SFTP pull 只校验最终文件名，未校验中间目录段；Windows 反斜杠目录段可进入本地路径归一化并穿出 `data`。 |
| H2-FILEBOX-PULL-PATH | 高 | 已确认 | FileBox pull 用 `path.Join` 生成相对路径但未校验远端目录名；目录名 `..` 可令后续文件的本地目标目录回退。 |

## H1-SHARE-PREVIEW：匿名预览绕过 maxDownloads

- ID：`H1-SHARE-PREVIEW`
- 严重度：高
- 结论：已确认；白名单文本/JSON/其他允许预览类型有 64 KiB 或 512 KiB 的内容上限，但非白名单 MIME 进入 attachment 分支且没有内容截断；所有 MIME 的匿名预览都不消耗 `maxDownloads`。
- 受影响入口：`GET /api/files/shared/{token}/preview`；对照入口为 `GET /api/files/shared/{token}/download`。

证据：

> 证据: internal/httpapi/server.go:387 | "\tmux.HandleFunc(\"GET /api/files/shared/{token}/preview\", s.sharePreview)"

> 证据: internal/httpapi/server.go:3240 | "// sharePreview 以 inline 方式输出分享文件供预览，不消耗分享下载次数。"

> 证据: internal/httpapi/server.go:3289 | "\tif previewMIMEAllowed(contentType) {"

> 证据: internal/httpapi/server.go:3292 | "\t\tw.Header().Set(\"Content-Disposition\", contentDisposition(file.Name))"

> 证据: internal/httpapi/server.go:3294 | "\tvar content io.ReadSeeker = handle"

> 证据: internal/httpapi/server.go:3295 | "\tif bound := previewContentLimit(contentType); bound > 0 {"

> 证据: internal/httpapi/server.go:3306 | "\thttp.ServeContent(w, r, file.Name, parseTime(file.CreatedAt), content)"

> 证据: internal/httpapi/server.go:3444 | "\tif !previewMIMETypes[base] {"

> 证据: internal/httpapi/server.go:3445 | "\t\treturn 0"

> 证据: internal/httpapi/server.go:3204 | "\tallowed, err := s.store.IncrementShareDownloads(r.Context(), token, int64(share.MaxDownloads), r.Header.Get(\"Range\") != \"\")"

现行关系：匿名请求先校验 token 对应文件为 ready 且分享未过期，然后 `sharePreview` 打开文件并直接 `ServeContent`；其函数体没有 `IncrementShareDownloads`。普通下载才在打开文件后调用该函数，因此普通下载的次数限制不覆盖预览流。对非白名单 MIME，`previewMIMEAllowed` 为 false，响应为 attachment；`previewContentLimit` 为 0，`bound > 0` 分支不执行，传给 `ServeContent` 的仍是完整 `handle`。IP 限速和审计记录不等于分享次数限制，不能抵消该缺陷。

已有测试只验证了当前行为：

> 证据: internal/httpapi/server_test.go:1313 | "func TestSharePreviewDoesNotConsumeDownloadCount(t *testing.T) {"

> 证据: internal/httpapi/server_test.go:1332 | "\tif metaData[\"downloadAvailable\"] != true {"

> 证据: internal/httpapi/server_test.go:1339 | "\tlimited := testBinaryRequest(t, handler, http.MethodGet, \"/api/files/shared/\"+shareToken+\"/download\", \"\", nil)"

修复建议：

- 涉及文件/函数：`internal/httpapi/server.go` 的 `sharePreview`、`previewContentLimit`，以及与 `shareDownload` 共用的分享额度判定路径。
- 需要业务拍板的策略项：预览是否计入下载次数；若不计次，预览必须定义并强制所有 MIME 的最大返回字节数，且 attachment 不能继续成为完整下载替代入口。若计次，Range 预览也要使用明确的原子扣次语义。
- 预计测试名：`TestSharePreviewEnforcesMaxDownloadsForNonWhitelistedMIME`、`TestSharePreviewNonWhitelistedMIMEIsBounded`、`TestSharePreviewRangeCannotExceedPreviewBudget`。

## H1-SHARE-RANGE：60 秒 Range 窗口绕过次数耗尽

- ID：`H1-SHARE-RANGE`
- 严重度：高
- 结论：已确认；当首次 Range 请求已经使 `download_count == max_downloads` 后，60 秒内后续任意带 Range 的请求仍满足 SQL 的最后一个 OR 条件，返回 allowed；单文件与聚合分享均存在同构问题。
- 受影响入口：`GET /api/files/shared/{token}/download`、`GET /api/shared-groups/{token}/download/{fileID}`；聚合 ZIP 入口使用非 Range 模式。

单文件调用与 SQL：

> 证据: internal/httpapi/server.go:3204 | "\tallowed, err := s.store.IncrementShareDownloads(r.Context(), token, int64(share.MaxDownloads), r.Header.Get(\"Range\") != \"\")"

> 证据: internal/store/store.go:2055 | "\t\tresult, err := s.DB.ExecContext(ctx, `UPDATE shares SET"

> 证据: internal/store/store.go:2056 | "  download_count = CASE WHEN last_download_at IS NOT NULL AND julianday(last_download_at) > julianday(?) THEN download_count ELSE download_count + 1 END,"

> 证据: internal/store/store.go:2059 | "  AND (max_downloads = 0 OR download_count < max_downloads"

> 证据: internal/store/store.go:2060 | "    OR (last_download_at IS NOT NULL AND julianday(last_download_at) > julianday(?)))`, windowStart, nowValue, token, nowValue, windowStart)"

聚合分享调用与 SQL：

> 证据: internal/httpapi/share_group.go:248 | "\tallowed, err := s.store.IncrementShareGroupDownloads(r.Context(), token, group.MaxDownloads, r.Header.Get(\"Range\") != \"\")"

> 证据: internal/store/share_group.go:247 | "\t\tresult, err := s.DB.ExecContext(ctx, `UPDATE share_groups SET"

> 证据: internal/store/share_group.go:248 | "  download_count = CASE WHEN last_download_at IS NOT NULL AND julianday(last_download_at) > julianday(?) THEN download_count ELSE download_count + 1 END,"

> 证据: internal/store/share_group.go:251 | "  AND (max_downloads = 0 OR download_count < max_downloads"

> 证据: internal/store/share_group.go:252 | "    OR (last_download_at IS NOT NULL AND julianday(last_download_at) > julianday(?)))`, windowStart, nowValue, token, nowValue, windowStart)"

行为推导：`max_downloads=1` 时首次 Range 将计数置为 1 并记录 `last_download_at`。60 秒内，`download_count < max_downloads` 为 false，但 `last_download_at > windowStart` 为 true；UPDATE 仍影响一行，CASE 保持计数不增加，因此任意重复 Range 请求都被视为同一个窗口并继续输出数据。超过 60 秒后该 OR 条件失效，才会因次数耗尽拒绝。

已有 store 测试覆盖了窗口过期后重新计数，但没有验证“窗口内已耗尽上限”的拒绝条件：

> 证据: internal/store/store_test.go:451 | "func TestShareDownloadRangeWindowDeduplicatesContinuousRanges(t *testing.T) {"

> 证据: internal/store/store_test.go:507 | "\tallowed, err = db.IncrementShareDownloads(ctx, \"range-expired\", 2, true)"

修复建议：

- 涉及文件/函数：`internal/store/store.go` 的 `IncrementShareDownloads`、`internal/store/share_group.go` 的 `IncrementShareGroupDownloads`，并复核 `internal/httpapi/server.go:3204` 与 `internal/httpapi/share_group.go:248` 的 Range 调用约定。
- 保留 Range 续传体验时，需要把“同一次受信任下载”的识别条件与“任意带 Range 请求”分开，并让 SQL 在次数已经耗尽时不能仅凭 `last_download_at` 放行；具体窗口/会话策略需业务确认。
- 预计测试名：`TestIncrementShareDownloadsRangeDeniedAfterLimit`、`TestIncrementShareGroupDownloadsRangeDeniedAfterLimit`、`TestConcurrentRangeRequestsCannotExceedLimit`。

## H1-GROUP-EXPIRY：聚合分享过期后的公开详情

- ID：`H1-GROUP-EXPIRY`
- 严重度：中
- 结论：部分成立；聚合公开详情接口过期后仍返回 `publicShareGroup` 和 ready 成员的文件名、大小、MIME、创建时间；但单文件下载和批量 ZIP 下载在流出内容前均调用 `shareActive` 并拒绝过期请求。当前没有单独的聚合公开 preview 入口。
- 受影响入口：`GET /api/shared-groups/{token}/meta`（过期后泄露详情/成员列表）；对照 `GET /api/shared-groups/{token}/download/{fileID}`、`POST /api/shared-groups/{token}/batch-download`。

过期判定本身存在且下载入口使用了它：

> 证据: internal/httpapi/share_group.go:31 | "\tdeadline, err := time.Parse(time.RFC3339, group.ExpiresAt)"

> 证据: internal/httpapi/share_group.go:32 | "\tif err != nil || !time.Now().UTC().Before(deadline) {"

> 证据: internal/httpapi/share_group.go:243 | "\tif !shareActive(group.ExpiresAt) {"

> 证据: internal/httpapi/share_group.go:245 | "\t\twriteError(w, http.StatusForbidden, \"分享链接已过期\")"

> 证据: internal/httpapi/share_group.go:306 | "\tif !shareActive(group.ExpiresAt) {"

> 证据: internal/httpapi/share_group.go:308 | "\t\twriteError(w, http.StatusForbidden, \"分享链接已过期\")"

公开 meta 的缺口：

> 证据: internal/httpapi/server.go:367 | "\tmux.HandleFunc(\"GET /api/shared-groups/{token}/meta\", s.shareGroupMeta)"

> 证据: internal/store/share_group.go:155 | "FROM share_groups sg WHERE sg.token = ? AND sg.revoked_at IS NULL`, token))"

> 证据: internal/httpapi/share_group.go:156 | "\tgroup, err := s.store.GetShareGroupByToken(r.Context(), token)"

> 证据: internal/httpapi/share_group.go:168 | "\tfiles, err := s.store.ListShareGroupFiles(r.Context(), group.ID)"

> 证据: internal/httpapi/share_group.go:182 | "\tdata[\"files\"] = fileItems"

`GetShareGroupByToken` 的 SQL 只排除 revoked，没有排除 `expires_at`；`shareGroupMeta` 读取后也没有调用 `shareActive`，直接列 ready 成员并返回 200。`publicShareGroup` 会把状态计算为 `expired`，但仍把详情返回，因此“状态标记为过期”不能满足“不存在/已过期时不返回内容”的期望。

修复建议：

- 涉及文件/函数：`internal/httpapi/share_group.go` 的 `shareGroupMeta`，以及 `internal/store/share_group.go` 的 `GetShareGroupByToken`/新增的公开可用性查询约定；下载入口的现有 `shareActive` 检查继续保留。
- 需要业务拍板的策略项：过期公开 meta 返回 404“不存在”还是 403“已过期”；报告不替业务选择状态码和文案。
- 预计测试名：`TestShareGroupMetaRejectsExpiredGroup`、`TestShareGroupExpiredMemberListIsNotReturned`、`TestShareGroupDownloadsRejectExpiredGroup`。

## H2-SFTP-PULL-PATH：SFTP pull 中间目录段穿越

- ID：`H2-SFTP-PULL-PATH`
- 严重度：高
- 结论：已确认；远端目录遍历得到的 `relative` 只做了一个以 `/../` 开头的粗粒度检查，随后只对最后文件名调用 `safeSyncFileName`。中间目录名没有逐段拒绝 `..`、反斜杠、控制字符或 Windows 非法字符；Windows 下反斜杠可在 `filepath.FromSlash`/`filepath.Join` 中重新成为分隔符，形成 `data` 外路径。
- 受影响入口：认证后的 `POST /api/sync/tasks/{id}/run` 执行 `pull`，SFTP 分支 `executeSyncPull`；任务创建入口为 `POST /api/sync/tasks`。

join 链与最终落盘：

> 证据: internal/httpapi/sync.go:1569 | "\t\t\trelative = remoteRelative(task.SourcePath, remotePath)"

> 证据: internal/httpapi/sync.go:1692 | "\t\twalker := client.Walk(task.SourcePath)"

> 证据: internal/httpapi/sync.go:1570 | "\t\t\tif relative == \".\" || relative == remotePath || strings.HasPrefix(relative, \"../\") {"

> 证据: internal/httpapi/sync.go:1574 | "\t\tparts := strings.Split(relative, \"/\")"

> 证据: internal/httpapi/sync.go:1575 | "\t\tname, nameErr := safeSyncFileName(parts[len(parts)-1])"

> 证据: internal/httpapi/sync.go:1581 | "\t\t\tlocalDir = strings.Trim(strings.Join(append([]string{task.TargetPath}, parts[:len(parts)-1]...), \"/\"), \"/\")"

> 证据: internal/httpapi/sync.go:1586 | "\t\tstorageDir := filepath.Join(\"files\", strconv.FormatInt(task.UserID, 10), filepath.FromSlash(localDir))"

> 证据: internal/httpapi/sync.go:1664 | "\t\t\tfinalPath := filepath.Join(s.config.DataDir, filepath.FromSlash(storagePath))"

例如中间段携带 `..\\..\\outside` 时，`strings.Split(relative, "/")` 不会拆出反斜杠段，`safeSyncFileName` 只看到最终文件名；但 Windows 的 `filepath.FromSlash` 和 `filepath.Join` 会按反斜杠解释该目录段。`EnsureFolderPath` 也按 `/` 分割且自身不做安全校验，不能构成边界守卫。

> 证据: internal/store/store.go:2651 | "\tfor _, seg := range strings.Split(path, \"/\") {"

> 证据: internal/store/store.go:2662 | "\t\t\tif _, err := s.CreateFolder(ctx, userID, parent, seg); err != nil && !errors.Is(err, ErrConflict) {"

对照 helper 的分段校验：

> 证据: internal/httpapi/server.go:1466 | "\tif strings.HasPrefix(dir, \"/\") || strings.HasPrefix(dir, \"\\\\\") || filepath.IsAbs(dir) || filepath.VolumeName(dir) != \"\" {"

> 证据: internal/httpapi/server.go:1473 | "\tparts := strings.Split(dir, \"/\")"

> 证据: internal/httpapi/server.go:1475 | "\t\tif part == \"\" || part == \".\" || part == \"..\" || strings.Contains(part, \"\\\\\") {"

> 证据: internal/httpapi/server.go:1479 | "\t\t\tif unicode.IsControl(char) || strings.ContainsRune(`<>:\"|?*`, char) {"

> 证据: internal/httpapi/sync.go:1511 | "\tif value == \"\" || value == \".\" || value == \"..\" || strings.ContainsAny(value, \"/\\\\\\x00\") || len([]byte(value)) > 255 {"

`validateUploadDir` 对每个目录段做了 `..`、反斜杠、控制字符和 Windows 非法字符校验；`safeSyncFileName` 只对单个最终文件名做对应校验，不能替代中间段校验。

修复建议：

- 涉及文件/函数：`internal/httpapi/sync.go` 的 `executeSyncPull`、`remoteRelative`/新增的远端相对路径逐段校验 helper，以及最终 `finalPath` 落盘前的 data-root containment guard。
- 校验至少应覆盖：统一分隔符后逐段拒绝空段、`.`、`..`、反斜杠、控制字符、Windows 非法字符和超长段；并在最终路径上用绝对路径/清理后的 root containment 检查兜底。
- 预计测试名：`TestSFTPPullRejectsTraversalInIntermediateDirectory`、`TestSFTPPullRejectsBackslashIntermediateDirectory`、`TestSyncPullFinalPathStaysUnderDataDir`。

## H2-FILEBOX-PULL-PATH：FileBox pull 远端目录名穿越

- ID：`H2-FILEBOX-PULL-PATH`
- 严重度：高
- 结论：已确认；`walkFileBoxEntries` 直接对远端 `name` 做 `path.Join(relative, name)`，目录条目随后以该结果递归；`executeSyncPullFileBox.process` 仅对最终段调用 `safeSyncFileName`，中间段进入 `localDir`、`storageDir` 和最终 `filepath.Join`。远端目录 `name == ".."` 可使后续文件的相对目录回退。
- 受影响入口：认证后的 `POST /api/sync/tasks/{id}/run`，方向为 `pull`、源类型为 `filebox`、目标类型为本地 FileBox；递归 browse 由 `walkFileBoxEntries` 驱动。

远端路径构造：

> 证据: internal/httpapi/sync_filebox.go:262 | "func walkFileBoxEntries(entries []map[string]any, relative string, process func(remoteBrowseEntry, string) error, descend func(string, string) error, detail *[]string) error {"

> 证据: internal/httpapi/sync_filebox.go:264 | "\t\tname, ok := entry[\"name\"].(string)"

> 证据: internal/httpapi/sync_filebox.go:269 | "\t\tchildRelative := pathpkg.Join(relative, name)"

> 证据: internal/httpapi/sync_filebox.go:618 | "\t\treturn walkFileBoxEntries(entries, relative, process, walk, &result.detail)"

> 证据: internal/httpapi/sync_filebox.go:275 | "\t\tif isDir {"

> 证据: internal/httpapi/sync_filebox.go:281 | "\t\t\tif err := descend(entryPath, childRelative); err != nil {"

> 证据: internal/httpapi/sync_filebox.go:296 | "\t\tif err := process(remoteBrowseEntry{ID: id, Name: name, Size: size}, childRelative); err != nil {"

本地 join 链：

> 证据: internal/httpapi/sync_filebox.go:531 | "\t\tparts := strings.Split(relative, \"/\")"

> 证据: internal/httpapi/sync_filebox.go:532 | "\t\tname, nameErr := safeSyncFileName(parts[len(parts)-1])"

> 证据: internal/httpapi/sync_filebox.go:538 | "\t\t\tlocalDir = strings.Trim(strings.Join(append([]string{task.TargetPath}, parts[:len(parts)-1]...), \"/\"), \"/\")"

> 证据: internal/httpapi/sync_filebox.go:543 | "\t\tstorageDir := filepath.Join(\"files\", strconv.FormatInt(task.UserID, 10), filepath.FromSlash(localDir))"

> 证据: internal/httpapi/sync_filebox.go:585 | "\t\t\tfinalPath := filepath.Join(s.config.DataDir, filepath.FromSlash(storagePath))"

当远端返回一个 `isDir=true, name=".."` 的目录条目时，`pathpkg.Join(relative, name)` 会得到回退后的 `childRelative`，并继续 `descend`；目录下的普通文件最终段可能是安全文件名，但其前置 `parts` 已包含回退目录，因而绕过了只校验最终段的 helper。现有 `validateFileBoxSyncPath` 只约束用户创建的任务目标路径，不能约束远端 browse 返回的目录名。

> 证据: internal/httpapi/sync.go:646 | "\tparts := strings.Split(value, \"/\")"

> 证据: internal/httpapi/sync.go:648 | "\t\tif part == \"\" || part == \".\" || part == \"..\" {"

> 证据: internal/httpapi/sync.go:713 | "\tif input.TargetType == \"filebox\" {"

> 证据: internal/httpapi/sync.go:714 | "\t\tinput.TargetPath, err = validateFileBoxSyncPath(input.TargetPath, true)"

现有测试只覆盖 malformed 字段类型不 panic，没有覆盖恶意目录名：

> 证据: internal/httpapi/sync_test.go:46 | "// TestFileBoxRemoteClientRejectsMalformedBrowseEntry verifies malformed remote field types return an error instead of panicking."

修复建议：

- 涉及文件/函数：`internal/httpapi/sync_filebox.go` 的 `walkFileBoxEntries`、`executeSyncPullFileBox`，复用或抽取与 `validateUploadDir` 等价的远端相对路径段校验，并在落盘前做 root containment guard。
- `walkFileBoxEntries` 在构造 `childRelative` 前应校验每个远端 `name`；对目录和文件采用同一套分段规则，不能只在 `process` 中校验最终文件名。
- 预计测试名：`TestWalkFileBoxEntriesRejectsDotDotDirectory`、`TestFileBoxPullRejectsBackslashIntermediateDirectory`、`TestFileBoxPullFinalPathStaysUnderDataDir`。

## 审计维度回核

- [权限与角色] 适用：分享 preview/meta/download 是匿名入口；同步任务 API 由认证用户创建/执行，远端返回数据属于不可信输入。入口证据见 `internal/httpapi/server.go:367`、`internal/httpapi/server.go:387`、`internal/httpapi/server.go:428`。
- [状态与流转] 适用：单/聚合分享有 active、expired、revoked、limit reached；聚合 meta 只排除 revoked，下载入口另行判过期。证据见 `internal/httpapi/share_group.go:27`、`internal/store/share_group.go:155`。
- [数量与量级] 适用：`maxDownloads` 与 `download_count` 是共享状态，Range 窗口不应在耗尽后继续授权。证据见 `internal/store/store.go:2059`、`internal/store/share_group.go:251`。
- [并发与幂等] 适用：扣次通过 SQL UPDATE 原子化，但 Range OR 条件改变了耗尽后的授权语义；需增加并发回归测试。证据见 `internal/store/store.go:2055`、`internal/store/share_group.go:247`。
- [存量与迁移] 本次未发现新增迁移逻辑参与这些入口；只审计现有表字段和运行时路径。
- [时序与时效] 适用：过期判定是当前时间与 `expires_at` 比较；Range 复用窗口为 60 秒。证据见 `internal/httpapi/server.go:3346`、`internal/store/store.go:2054`。
- [删除与撤销] 适用：revoked 记录在公开入口被区分并拒绝；过期与 revoked 不能只依赖后台 prune。证据见 `internal/httpapi/share_group.go:158`、`internal/store/store.go:2076`。
- [异常与边界] 适用：已有测试覆盖 malformed FileBox 字段和正常预览上限，但缺少恶意目录段、非白名单 MIME 完整流和已耗尽 Range。证据见 `internal/httpapi/sync_test.go:48`、`internal/httpapi/server_test.go:1280`。
- [可见性与审计] 适用：分享 preview/download 会写 service/audit 事件，但日志存在不改变匿名内容授权。证据见 `internal/httpapi/server.go:3247`、`internal/httpapi/server.go:3163`。
- [与现有功能关系] `validateUploadDir`、`validateFileBoxSyncPath` 和 `safeSyncFileName` 的校验边界不同；任务目标校验不能替代远端返回路径校验。证据见 `internal/httpapi/server.go:1473`、`internal/httpapi/sync.go:646`、`internal/httpapi/sync.go:1511`。
- [测试与验收] 本轮仅运行限定命令，两个包均通过；未新增测试代码，报告中的测试名为修复后预计新增的回归用例。

## 验证记录

- Git 初始状态：`## main...origin/main`，工作树无改动。
- 证据校验器：`scripts/verify_evidence.py` 不存在；已手工 `rg`/带行号读取回核。
- 测试命令：`go test ./internal/httpapi/ ./internal/store/`
- 测试结果：通过；`filebox/internal/httpapi` 与 `filebox/internal/store` 均输出 `ok`（cached）。
- 未运行：部署、Release、前端构建、全量测试及未被用户授权的其他命令。

## V023 C/1：匿名收集未完成上传任务上限

- 【业务确认】2026-09-04：匿名收集链接未完成上传任务上限 50（不设字节上限）。

### 技术发现

- `upload_tasks.status` 是独立状态列：正常上传任务创建为 `pending`，完成路径在同一 store 事务中写为 `complete`。当前没有持久化的 `cancelled` 或 `expired` 状态；取消和 24 小时过期清理都是删除 `pending` 任务行，因此计数排除 `complete`、`cancelled`、`expired`，未知状态按未完成计数。
- `upload_collections.upload_count` 只在完成上传或秒传写入收集记录时递增，不是未完成任务计数；owner 的 pending 字节配额仍按原有实现计算，本次不新增按字节上限。
- `CreateCollectionUploadTask` 在同一 `BeginTx` 中先对 collection 执行写锁语句，再按 `collection_id` 和状态计数；达到 50 返回现有结构化 `COLLECTION_LIMIT` 业务错误，否则插入 `pending` 任务并提交。SQLite `Open` 将连接池固定为单连接，并配置 WAL/`busy_timeout`，因此并发 init 不会同时越过 50。
- 完成方法在同一 store 事务中将任务写为 `complete`；取消和过期清理删除 `pending` 行，计数查询因此天然释放容量。
