# FileBox Requirement State

Updated: 2026-09-12 (v031 checksum performance: worker-wide hashing + WASM fast path; v030 collection storage layout, share tree, upload queue grouping)

| Requirement | State | Notes |
|---|---|---|
| v031 checksum performance (worker covers all sizes) | done | `computeFileSHA256` always hashes in the worker first: native WebCrypto up to `FILEBOX_HASH_DIRECT_LIMIT` (256MiB), streaming beyond it, so files over 256MiB no longer block the main thread. The main thread only handles the case where no worker can be created. |
| v031 WASM fast path + shared fallback module | done | New `web/src/sha256Fallback.js` holds the dependency-free streaming SHA-256 shared by the main thread and the worker; the worker statically imports `hash-wasm@4.11.0` for the WASM streaming path and falls back to the shared module when WASM cannot be initialised. Worker chunk 0.67kB → 21.4kB (tree-shaken to sha256 only). Measured on Node/V8 (64MiB): 203.7MB/s WASM vs 23.4MB/s pure JS (8.7x, identical digests); 1GiB ≈ 5.0s vs 44s. |
| v031 CSP for WebAssembly | done | `script-src 'self'` → `script-src 'self' 'wasm-unsafe-eval'` (allows WebAssembly compilation only, not `unsafe-eval`); verified in the live response header. |
| v031 tests | done | `web/tests/sha256Fallback.test.mjs` (block boundaries 0/1/55/56/57/63/64/65/127/128/129/1MiB+1 with unaligned chunks), `web/tests/wasmSha256.test.mjs` (hash-wasm byte-identical to node:crypto), benchmark script `web/tests/hashBench.mjs`. |
| v030 collection storage layout + migration CLI | done | Collection uploads now land in `files/<uid>/collections/<name>-<token8>` (ASCII parent, user-language child). `filebox admin migrate-collection-dirs [--dry-run]` rewrites `files.storage_path`, `folders.path` and `upload_tasks.storage_dir` plus the on-disk tree, then prunes the emptied legacy `uploads` directories; there is no startup migration. The folder listing shows the user-language collection name while every other directory keeps the v019 rule (path basename wins). |
| v030 share page directory tree (#9) | done | The aggregate share page renders a collapsible directory tree from the new `files[].relativePath` field, with per-directory selection feeding the ZIP download. |
| v030 collection upload queue grouping + transfer detail (#5/#8) | done | Directory uploads collapse into one expandable queue row (file count, total size, aggregate progress, failure reason); each file row expands a transfer detail block (bytes, live EMA rate, status, failure reason). The upload engine still walks the flat queue. |
| v030 collection password page + clear-all pagination (#4/#3) | done | `.public-password-form` had no CSS rule at all and now matches the site form rhythm; the file library resets to page 1 after a successful clear. |

| Requirement | State | Notes |
|---|---|---|
| v029 per-user clear in user management | done | `POST /api/admin/users/{id}/clear-files` (admin only, re-uses the v027 re-auth buckets and second-factor flow, supports `empty-dirs` / `whole-tree`, audits `target=user:<name>`) clears only the target user. The file library's global scope is gone: `POST /api/files/clear-all` rejects `scope != own` with 400 `SCOPE_UNSUPPORTED` and the dialog no longer offers the all-users option. |
| v029 delete user with optional file retention | done | `DELETE /api/admin/users/{id}` accepts `{"keepFiles": bool}` (default false = delete files). With `keepFiles=true` the ready files move into the recycle bin owned by the reserved account `users.id=0` (login disabled, zero quota, created during migration and hidden from user lists/stats) under `files/0/<former username>/<relative path>` with numeric suffixes on conflicts and same-volume renames (rolled back on failure). |
| v029 orphan share cleanup on user delete | done | Both delete modes now remove the user's `shares` rows, fixing orphan share records left behind because `shares.created_by` has no foreign key. |
| v029 recycle bin APIs and UI | done | Admin-only `GET /api/admin/recycle` (grouped by former username, exposes `recycleUser`/`relativePath`), `DELETE /api/admin/recycle/{id}`, `POST /api/admin/recycle/purge`; recycled files remain downloadable through `/api/files/{id}/download`. User management gained a per-row 「清空文件」 action with re-auth and disk-mode options, a delete confirmation with the 「同时删除该用户全部文件」 checkbox (default checked), and a recycle-bin panel with per-file delete and purge, all trilingual. |

| Requirement | State | Notes |
|---|---|---|
| v028 clear-all scope, residues and disk handling | done | `ClearFiles(ctx, userID, allUsers)` clears ready files, folder records, unfinished upload tasks and revokes the owner's shares in one transaction, resets quota, and reports counts. HTTP layer removes file content plus `tmp/<taskID>` and applies `diskMode` (`empty-dirs` prunes empty directories bottom-up, `whole-tree` removes `files/<uid>`). Verified by 5 new Go tests and the online checklist. |
| v028 files sort in table headers | done | Toolbar sort controls removed; 名称/大小/类型/上传时间 headers are clickable (▲/▼ + `aria-sort`, keyboard accessible); the integrity column stays unsortable; folders follow the same params. |
| v028 drag-drop upload removal | done | Files view and collection upload page lost their drop zones/hints, `dragging` state, `handleDrop`/`collectDrop*` helpers and related CSS; 选择文件 / 上传文件夹 remain. |
| v028 new-folder button placement | done | The 新建文件夹 button now shares the action row with 选择文件 / 上传文件夹. |
| v028 transfer failure reason on hover | done | Failure reasons are written to the transfer item and bound as a row-level `title` (active and finished tabs) so hovering anywhere on a failed row shows the cause. |
| v028 share error distinction | done | Anonymous share endpoints return `SHARE_NOT_FOUND` / `SHARE_REVOKED` / `SHARE_EXPIRED` / `SHARE_CONTENT_MISSING`; the owner's share list marks links whose file is gone with `status=content_missing` + `contentMissing`; frontend maps the codes and i18n carries trilingual strings. |

| Requirement | State | Notes |
|---|---|---|
| v027 clear-all reauth throttling | done | Two buckets keyed by userID + source IP: attempt bucket (10/min, every attempt incl. correct) bounds bcrypt/TOTP work, failure bucket (5/min, wrong credentials only) returns `429 REAUTH_RATE_LIMITED`; failures feed `ip_failures` (R-IPBAN) but deliberately never lock the account (a stolen JWT must not lock the victim out). Verified by 3 new Go tests plus the 5 pre-existing clear-all tests. |
| v027 clear-all TOTP replay protection | done | The TOTP branch now calls `store.ConsumeTOTP`, so one code cannot authorize repeated destructive clears (same code replayed → 401); ±1 window and constant-time compare unchanged. |
| v027 sync trigger data race | done | `runTriggeredTask` reads the coordinator context through the new mutex-guarded `triggerContextErr()`; every `triggerCtx` access is now inside `triggerMu` or via that helper. **Verified with the race detector**: `go test -race ./internal/httpapi/` → ok, no data race (portable w64devkit GCC + `CGO_ENABLED=1`), likewise `./internal/store/`, `./internal/srvlog/`, `./cmd/filebox/`. Two timing-sensitive rate-limit assertions (clear-all attempt bucket, collection password bucket) are skipped under `-race` because the instrumented build slows bcrypt ~10-20x and the token buckets refill mid-test; their semantics are pinned by the deterministic `TestClearAllReauthLimiterBoundsAttemptsDeterministically` (1 token/minute, no refill possible) which runs in every mode. |
| v027 DEV_DOC source-IP wording | done | Three statements claiming "X-Forwarded-For first entry" contradicted the implementation; corrected to the actual semantics (plain `RemoteAddr` unless `trustProxy` is on and the peer is a trusted proxy, then the rightmost untrusted XFF entry), plus a banner stating DEV_DOC is a v0.1 draft superseded by `requirements/STATE.md` + `CHANGELOG.md`. |
| v027 collection create default password semantics | done | Documented in `README.md`: omitting `passwordMode` creates an unprotected collection (legacy semantics); a random password requires explicit `passwordMode=random`; `manual` requires `password` (else 400). |
| v027 workspace hygiene | done | Removed the untracked debug probes `tmp-read-users.go` / `.tmp-read-users.go`. |
| v027 workspace API key rotation | blocked-on-user | `dsh-project/run-codex.ps1` hard-coded an OpenAI API key; the script was hardened to prefer `$env:OPENAI_API_KEY` with an optional local key file, but **rotating the exposed key is a manual operator action** (the file lives outside the filebox repo). |

| Requirement | State | Notes |
|---|---|---|
| v026 sort APIs/UI | done | Sort support is exposed consistently by the APIs and rendered in the UI. |
| v026 clear-all reauth and password-vs-TOTP | done | Clear-all requires reauthentication; password and TOTP paths are distinct and covered by tests. |
| v026 anonymous collection metadata concealment | done | Expired or revoked collections do not disclose anonymous collection metadata. |
| v026 transfer completion and queue reliability | in-progress | Transfer completion is unified with timestamps; folder queue batching and request race protection are in progress. |
| v026 native directory picker | done | `showDirectoryPicker` is preferred, with `webkitdirectory` as fallback; browser prompts cannot be suppressed in the fallback. |
| v026 deployment | pending | Deployment remains pending. |

| Requirement | State | Notes |
|---|---|---|
| v025 mobile topbar collapse | done | 【业务确认】≤800px 时 AuthenticatedTopbar 将导航链接、语言选择、修改密码与退出折叠到单一无障碍菜单（汉堡按钮，aria-expanded/aria-controls，打开后聚焦关闭按钮）；菜单支持背板点击 / Esc / 路由跳转 / 视口转宽自动关闭；各页 actions 插槽（如文件页传输）在菜单内可达；桌面内联顶栏保持不变。 |
| v025 shared responsive polish | done | 【业务确认】共享 CSS 移动端梳理：page-heading 操作区折行、表格只在语义表格容器内横向滚动（不引发 body 溢出）、≤800/≤500 触控目标加大、模态框背板可滚动、分页换行、文件名列宽随屏收缩；Files/Collections/Shares/Sync/Logs/Admin 均受益，不改页面逻辑。 |

| Requirement | State | Notes |
|---|---|---|
| v025 triggered sync schedule | done | 【业务确认】`scheduleType=triggered` allowed only for push tasks whose local FileBox directory is the source (sourceType=filebox, sourceKind=directory); pull/SFTP/single-file rejected at API and DB CHECK (relaxed by table rebuild); invalid combinations return 400. |
| v025 debounce/coalesce/serialization | done | 【业务确认】30s debounce after the final change; changes during an active run coalesce into exactly one follow-up after completion; the same task never runs concurrently (shared per-task lock with manual/scheduled runs and `executeSyncTask`). |
| v025 mutation hooks & matching | done | 【业务确认】Trigger on upload complete (incl. overwrite/rename), folder create/rename(old+new)/delete, file delete, batch delete incl. forced recursive tree, and collection completion that creates owner files; no trigger on chunk uploads, failed/incomplete chunks or instant-upload hits (no new file created). Matching: root source matches all; otherwise change must equal the source or be inside its subtree. |
| v025 lifecycle/restart | done | 【业务确认】Coordinator starts with the process, refreshes registry after task CRUD/enable changes, stops all timers on shutdown; no retroactive run after restart (documented). |
| v025 SyncView | done | 【开发拟定】Schedule dropdown adds 变更触发/On change, disabled when direction is not push with an explainer on save; cron hidden under triggered with a 30s debounce hint; task rows show on-change status. |

| Requirement | State | Notes |
|---|---|---|
| v024.2 login brand footer anchoring | done | 【业务确认】LoginView moves `BrandFooter` out of `.login-form-wrap` to be a direct child of `.login-panel`; the panel becomes a centered flex column (`.login-form-wrap { margin: auto 0 }`) and the footer is constrained to the form width and anchored lower, giving meaningful separation below the form. Responsive via clamp padding and the existing 800px breakpoint; no fixed heights/widths, inputs stay on-screen. |
| v024.2 compliance row responsive | done | 【业务确认】BrandFooter is split into a heading block (title/description) and a single horizontal compliance row (copyright/ICP/police) that uses `flex-wrap` centered with readable gaps on narrow widths; responsive behavior lives in the scoped CSS so it is not defeated by global styles; brand data/API unchanged. |

| Requirement | State | Notes |
|---|---|---|
| v024.1 nginx HSTS example | done | 【业务确认】Conservative one-year `Strict-Transport-Security` (`max-age=31536000; includeSubDomains`) added only to the 443 TLS server block of `deploy/nginx.conf.example`; commented safe only after every subdomain serves HTTPS; no `preload`. |
| v024.1 collection result default copy | done | 【业务确认】Default sharing no longer embeds the password in a URL: the result dialog shows the clean link and the one-time password; the primary copy action copies localized `链接：<url>\n密码：<password>` (trilingual `collectionCredentialText` pure helper + node tests assert the password never appears in the URL). `#password=` fragment remains only as backward-compatible API behavior and is not rendered as the default copy target; plaintext is cleared from memory when the dialog closes. |
| v024.1 workspace-root .gitignore | done | Created `C:\Users\huangcp\dsh-project\.gitignore` — the root is not a Git repo, so it is reported separately and not committed to filebox — ignoring `.test-data/` (tokens/TOTP/DB/logs), `.tools/`, `filebox-demo/`; no existing test data removed. |


| Requirement | State | Notes |
|---|---|---|
| v024 collection random/manual/none password modes | done | Create defaults to server-generated random (crypto/rand, unambiguous alphabet), bcrypt-hashed, plaintext + `#password=` fragment convenience URL revealed exactly once; edit supports keep/random/manual/none; legacy clients without `passwordMode` keep old semantics and never see plaintext. |

| Requirement | State | Notes |
|---|---|---|
| v024 folder upload single application confirmation | done | Folder picker/drop path shows the app-owned custom folder confirmation and marks the batch `folderConfirmed`; the >50-file generic bulk confirm is suppressed for that path (`needsBulkConfirm` pure helper + node tests). Plain file batches still get exactly one bulk confirmation. |
| v024 many-file upload reliability | done | Busy-wait/interval gates replaced with bounded fair `FairStartGate` (max 3 concurrent file starts, FIFO); paused/failed/terminated waiters yield; chunk workers keep the global cap; queue waits are no longer reported as network failures. |
| v024 visible per-file and overall rate | done | Active upload rows show transferred/total + live per-file rate (EMA of per-second loadedBytes deltas); overall rate sums active upload + download rates. |
| v024 active/done separation with distinct results | done | Successful uploads move to the Done tab labelled success; failures show a distinct failure label and stay retryable; terminated uploads are kept as cancelled records with a distinct label and clear action. |

| v020 delete/revoke confirm → custom modal | done | All 12 `window.confirm` call sites replaced: FilesView queue-based `askConfirm` (60s timeout=cancel) + modal-backdrop/modal-panel; Shares/Collections/Sync/Admin adopt the same pattern; zero `window.confirm` left in web/src; commits `ccc6272`/`54477fe`. |
| v020 batch-download error cause + timestamped ZIP | done | ZIP build/read failures classified (ENOSPC/EDQUOT→disk full, EACCES/EPERM/EROFS→not writable, else→IO error; `code=ZIP_CREATE_FAILED`+`reason`; raw error only logged); archive named `filebox-batch-YYYYMMDD-HHMMSS.zip` on server and client; injectable `createBatchTempFile` + regression tests; commit `3ee19ce`. |
| v020 transfer batch control | done | Drawer selection/select-all per upload & download section with pause/resume/terminate-selected and "all" actions; upload terminate calls `DELETE /api/upload-tasks/{taskId}`; downloads gain pause (AbortController) + Range resume (206/200); commit `cec1382`. |
| v020 topbar transfers label | done | Icon-only transfers entry → icon-text-button with trilingual label; badge kept; commit `1c62df8`. |
| v020 my-collections duplicate refresh removed | done | List-section header refresh icon removed (page-heading refresh kept); plus `.value` residual cleaned so the collections pagination bar shows for >1 page; commits `dfe4512`/`4a031de`. |
| v020 shares subtitle wrong "Copy link" | done | Duplicated `shares.copy` key split into `shares.intro` (subtitle) and `shares.copy` (copy-button title); trilingual; commit `e51cbdb`. |
| v020 aggregate-share edit aligned with single share | done | Group edit dialog now shows info grid + link (copy/open) + hour-based extend/increase (PUT extend/increase) + member add/remove + expiry/limit edit; dead groupAction code removed; commit `665922d`. |
| v020 sync file detail modal | done | Inline `<details>` replaced by a button → modal parsing `detail` into per-file rows with success/skipped/failure/overall-error badges; raw view kept; commit `f6d7b5e`. |
| v020 sync detail modal width | done | `.wide-modal` 1100px→`min(1280px,96vw)`, log table min-width 1160px; task-edit modal keeps 1100px via `sync-task-modal`; commit `b685856`. |
| v020 log failure reason localization | done | Backend reasons enumerated; trilingual `logReason.*` key sets aligned to 38 identical keys (share*5, ipLocked/totpFailed, readOnly/settingsFailed/invalidRequest/deleteFailed/writeFailed/batch); node-verified; commits `ef9d083` (+`bdf1eee` mapping). |
| v020 transfers icon direction | done | ArrowLeftRight → ArrowUpDown on the topbar transfers button; commit `6d75e1b`. |
| v023 independent static encryption key migration | done | 【业务确认】`--encryption-key`/`FILEBOX_ENCRYPTION_KEY`/`config/secrets.json` support strict Base64-encoded 32-byte AES-256 keys; versioned envelopes use the independent key, legacy JWT-derived ciphertext falls back and lazily migrates on successful TOTP/sync reads; absent key keeps legacy behavior with a warning. |
| v023 SFTP TOFU host-key policy | done | 【业务确认】SFTP systems pin the first observed SHA-256 host-key fingerprint atomically; matching connections proceed, mismatches return structured `HOST_KEY_CHANGED` without mutation, and owner/admin confirmation uses an expected-value guarded update followed by retry. |

| Requirement | State | Notes |
|---|---|---|
| v019 time-range apply/clear buttons | done | Draft-state time panel: edits only commit on 确定, 清空 resets; v019 batch (7 items). |
| v019 pagination display fixes | done | Template ref-unwrap bug (`pageSize.value` → undefined) fixed across logs/files/shares/groups list pages. |
| v019 folder navigation invalid fix | done | navigateDir normalization + backend listFolders filtering/`validateFolderName` rejects `.`/`..`. |
| v019 aggregate-share card polish | done | Eye/Copy/Trash only; edit merged into the eye dialog. |
| v019 sync dialog width + collections/group pagination etc. | done | wide-modal 780→1100px; pagination controls shown on four list pages; v019 doc/commits already recorded in CHANGELOG/RELEASE_NOTES. |

| Requirement | State | Notes |
|---|---|---|
| v018 nav.admin Chinese label fix | done | `SECTION_NAV_KEYS` maps admin→nav.system (and sync→nav.syncTasks); the topbar section name no longer falls back to the literal key; commits `b9f98f3`/`3e78698`. |
| v018 aggregate-share editing | done | Member files can be listed/added/removed (`GET/POST /api/shared-groups/{token}/files`, `DELETE .../files/{fileID}`) and attributes edited (`PUT /api/shared-groups/{token}`: future expiry, limit ≥ used); SharesView adds an eye (file-scope dialog) and an edit dialog; `share_group_update` audit; commit `a6d3ef5`. |
| v018 collections copy/view buttons on one row | done | "Copy link" and "View received files" buttons are grouped on the same row; the link spans the full width; commit `b4b04c2`. |
| v018 sync transfer progress | done | In-process `syncProgress` registry updated by all four push/pull paths (SFTP + FileBox); `GET /api/sync/tasks/{id}/progress` polls current file / done files / bytes / rate; SyncView renders a live progress row; commit `fde3c24`. |
| v018 sync log columns | done | `publicSyncTask` exposes `nextRunAt` (cron-derived); the details dialog log area is a table (start/end/status/next-run-for-periodic/files/bytes/expandable detail); commit `fde3c24`. |
| v018 log time-range UI | done | The two datetime-local fields became one "Time range" button with a dropdown panel (either bound optional); trilingual `logs.timeRange`; commit `b4b04c2`. |
| v018 pagination enhancements | done | listShares/listCollections/listShareGroups return page/pageSize/total (cap 100); the four list pages gain pageSize selectors (10/20/50/100, localStorage) and page-number jumping when > 7 pages; commits `4186425`/`7e23a4d`. |
| v018 merged folder/file listing | done | FilesView merges folders into the file table (folders first, Folder icon + 目录 type column, click-to-enter, rename/delete kept; no checkbox/file actions on folder rows); commit `47d1b73`. |
| v018 tests | done | Member add/remove/attribute-edit tests (httpapi + store, including a store guard fix for past expiries), pagination pageSize cap, and nextSyncRunTime; commit `e14881c`. |

| Requirement | State | Notes |
|---|---|---|
| v017 file library drops the embedded collections block | done | FilesView no longer renders the collections section/entry/modals or collection state; the standalone /collections page (CollectionsView) remains the single entry. |
| v017 per-tab admin console descriptions | done | AdminView page heading copy switches with the active tab (overview/users/security/brand/locks/system) with dedicated trilingual texts. |
| v017 navigation order and naming | done | Topbar order: My files → My collections → My shares → Sync tasks → Logs → System settings; `nav.shares` is "My shares", admin is "System settings" (nav.system), trilingual. |
| v017 audit log time-range filter | done | `GET /api/logs` accepts from/to (RFC3339, invalid → 400); `ListAuditLogs` adds created_at >= / <= (boundary-inclusive); LogsView adds start/end datetime-local inputs; store + httpapi tests added. |
| v017 sync credentials saved & viewable | done | Credentials stay AES-GCM encrypted with blank-edit keeping existing; new `GET /api/sync/systems/{id}/secret` returns decrypted secret/passphrase once for the owner/admin with audit; SyncView shows a saved-password placeholder with an eye toggle; endpoint test added. |
| v017 sync execution history | done | Task detail logs show start time, end time (when finishedAt exists), and running/success/failure state; compatible with the new sync_logs schema. |
| v017 sync path picker enhancements | done | Picker filters the current directory list by name (local & remote) and accepts a manually entered full path with confirmation (remote paths validated via browse). |
| v017 sync task list target host | done | Task rows show the target host:port for SFTP or the URL host for FileBox, with graceful fallback. |
| v017 sync log real-time status | done | sync_logs gains finished_at (table rebuilt without the result CHECK); execution writes a running row first and updates it on completion (`UpdateSyncLogResult`); failure paths never leave running rows; store + httpapi tests added. |
| v017 change-password modal | done | The topbar change-password entry opens a centered modal (old/new/confirm → POST /api/auth/change-password → session refresh); forced change-password still redirects to the standalone page. |

| Requirement | State | Notes |
|---|---|---|
| Login, logout, current user with JWT and bcrypt | done | JWT expires after 7 days by default; disabled users are rejected. |
| File listing with pagination and keyword search | done | User ownership is enforced; admins can inspect all files. |
| Direct single-part upload | done | Upload init, chunk 0, complete, including zero-byte files; the browser UI limits files to 100MB. |
| MD5 and SHA-256 integrity fields | done | Both hashes are calculated from the stored upload before the record is committed. |
| Streaming download with Range 206 | done | Content-Disposition uses the original sanitized filename. |
| File deletion | done | Database soft delete and immediate physical cleanup. |
| Multi-user isolation and quota check | done | Pending uploads reserve quota during initialization. |
| Admin user CRUD and statistics | done | Create, update role/quota/disabled/password, delete, and overview stats. |
| R-NAME original disk filenames | done | Sanitized names are stored under per-user/per-month directories; same-directory collisions receive the smallest available `name (n).ext` suffix and storage_path is unique. |
| R-DISK disk monitoring and upload protection | done | Admin stats expose disk capacity/free/used/usage percentage; upload-init rejects free space below the configured threshold with DISK_FULL. |
| R-VALID upload filename pre-validation | done | upload-init rejects path separators, control characters, Windows-illegal characters, dot traversal markers, empty/dot names, and names over 255 bytes. |
| R-CONFLICT same-directory collision resolution | done | init reports existing ready files with HTTP 409; overwrite replaces transactionally with quota correction, rename allocates the next suffix. |
| R-LOG operation audit trail | done | Login, upload, download, share create/view/download, and register outcomes are recorded; users see their own logs, admins can filter all logs and configure lazy retention cleanup. |
| R-LOCK login lockout | done | Failed login thresholds, temporary/permanent locks, automatic unlock, uniform failure responses, and admin reset are implemented. |
| R-BRAND configurable branding | done | Admin can set/reset title, SEO description, favicon, login/main logos, ICP and public-security filing text; public branding APIs, embedded defaults, and conditional footer rendering are implemented. |
| R-LANG multilingual interface | done | `users.language` and validated `settings.defaultLang` are persisted; authenticated language updates are immediate; public brand exposes the system default; all Vue views use complete zh-CN/zh-TW/en dictionaries with localized dates and API errors. |
| R-THEME interface theme color | done | `settings.theme_color` persists validated `#RGB`/`#RRGGBB` values; public brand exposes the effective color; admin input and picker update CSS variables immediately and can restore the default. |
| R-INIT initial account and forced password change | done | Configurable first-start admin credentials, `must_change_password` migration, forced-change middleware, password rotation, and admin reset flag are implemented. |
| R-PWD password strength policy | done | Admin settings expose minimum length and character-class requirements; create, reset, and self-service password changes enforce them. |
| R-TOTP two-factor authentication | done | Encrypted AES-GCM secrets, RFC 6238 verification with setup flow, QR endpoint, short-lived challenges, and replay protection are implemented. |
| R-IPACL source IP allowlist | done | Admins can configure IP/CIDR allowlists for users; authenticated requests enforce them only when enabled. |
| R-IPBAN IP login lockout | done | Sliding source-IP failure windows, configurable thresholds and auto-unlock, uniform login errors, and success reset are implemented. |
| R-LOCKADMIN lock management | done | Admin API and UI list/remove IP and user lock records, with security policy controls. |
| R-OPS local maintenance CLI | done | The single binary supports password reset with forced change, IP ACL recovery, IP/user lock listing, lock clearing, and SQLite busy-timeout coexistence. |
| R-SRVLOG service logging | done | Optional console-plus-file logging with local-date rotation, gzip archives, retention cleanup, operator/source-IP event fields, and CLI/server lifecycle events. |
| R-SERVICE/R-PROXY deployment | done | Linux systemd, Windows NSSM/sc, and Nginx HTTPS reverse-proxy templates and bilingual deployment guidance are provided under deploy/. |
| First-start admin creation | done | `admin / admin123`; change credentials after first login. |
| SQLite persistence | done | Pure Go `modernc.org/sqlite`; data directory is configurable. |
| Vue 3 pages for login, files, and admin | done | Production bundle is embedded in the Go binary. |
| Stage 2 resumable chunked upload | done | 2–8MB chunks, out-of-order/idempotent chunk PUT, persisted `chunks` table resume (survives server restart), streamed merge, missing-chunk rejection, and backward-compatible single-part upload. |
| Stage 2 instant upload | done | `POST /api/files/check` matches MD5 first then SHA-256 within the same user; hits return the existing record without writing a new file. |
| Stage 2 folder upload | done | `dir` field preserves relative paths under `files/<user>/<yy>/<mm>/<dir>`; identical names in different directories stay unsuffixed. |
| Stage 2 share links | done | 64-char tokens, expiry and download limits, anonymous meta/download endpoints, revocation, anonymous share page `/:token`, and share audit actions. |
| Stage 2 online preview | done | MIME-whitelisted inline preview (image/text/video/pdf/json) with attachment fallback for everything else. |
| Stage 2 upload rate limiting | done | Per-user token bucket (`golang.org/x/time/rate`), configurable `uploadRateLimit` in bytes/sec (0 = unlimited), applied before chunk writes. |
| Stage 2 registration switch | done | `settings.registerEnabled` (default off, `--register-enabled` first-run seed), `POST /api/auth/register` with password policy and direct login, public `brand.registerEnabled`, login-page entry. |
| Stage 2 system statistics | done | Admin stats include active share count and cumulative share downloads alongside users/files/bytes/disk. |
| Login page hides default credentials (user feedback) | done | The `admin / admin123` hint was removed from the login page and from all three dictionaries. |
| Transfer progress panel (user feedback) | done | A topbar badge opens a right-side drawer separating uploads and downloads; downloads stream with progress, uploads keep pause/resume/retry. |
| MD5 column with display toggle (user feedback) | done | The integrity column shows `file.md5` directly by default with a localStorage-persisted toolbar switch. |
| Directory upload entries (user feedback) | done | Both the folder-picker button and directory drag-and-drop upload folders; empty/invalid drops show a clear Chinese message instead of a network error. |
| User-defined folders (v011 feedback, plan B) | done | Removed the automatic year/month storage layer; added the `folders` table and CRUD API (create/list/rename/delete with non-empty protection), directory-filtered file listing with breadcrumbs, upload-into-current-folder, and the `admin migrate-v010-paths` migration command (yy/mm → yy-mm with DB backup and storage-path rewrite). |
| Topbar logo navigation (v011 feedback) | done | `BrandLogo` gained a `link` prop; clicking the topbar logo on files/admin/logs/share pages navigates to `/`. |
| Single-binary release build (v011 feedback) | done | `make release` (and `scripts/release.ps1`) produce static trimmed Windows/Linux amd64 binaries plus SHA256 checksums; README/RELEASE_NOTES document single-file delivery and step-by-step deployment. |
| Log result colors (v011 feedback) | done | Success labels use fixed green, failures fixed red, independent of the theme color. |
| Log "system configuration" actions (v011 feedback) | done | `logActions` exposes the full action set and the log page groups/filters business vs system-configuration actions with trilingual labels. |
| Self password change entry + TOTP re-enrollment (v011 feedback) | done | Topbars link to the existing change-password page; admin TOTP toggle supports `reenroll` so the user re-binds on the next login. |
| Copyright text branding (v011 feedback) | done | `brand_copyright` setting, `/api/brand` `copyrightText`, admin panel input, and conditional footer rendering with empty-value suppression. |
| Tabbed admin console (v011 feedback) | done | AdminView uses a left vertical menu with six tabs (overview/users/security/branding/locks/system); `?tab=` deep-links and survives refresh; log retention, registration switch, and upload rate limit live in the system tab. |
| User modal dialogs (v011 feedback) | done | Create/edit user flows are centered `modal-backdrop`/`modal-panel` dialogs with full role/quota/password/TOTP/IP-allowlist fields. |
| Log retention moved to admin (v011 feedback) | done | The log page no longer embeds the retention panel; logRetentionDays is edited in the admin system tab via the existing settings API. |
| Footer brand info block (v011 feedback) | done | BrandFooter renders `siteTitle` + `siteDescription` on the first line, then copyright/ICP/police lines; nothing renders when all are empty. |
| Concurrent same-name conflicts (v011 feedback) | done | Conflict prompts are a queue with a 60s timeout; every awaiting upload resolves, so concurrent same-name uploads no longer stall in "preparing". Renamed duplicates expose `multi (1).txt`-style user-visible names. |
| Upload failure logging (v011 feedback) | done | `upload_init` and `upload_chunk` record audit rows and service events on every rejection with granular reasons (invalid_name/too_large/conflict/disk_full/quota_exceeded/…); visible in the admin log page and server.err.log. Failed uploads stay in the transfer drawer with a retry/dismiss action. |
| Quota error details (v011 feedback) | done | Quota rejection returns `QUOTA_EXCEEDED` with usedBytes/quotaBytes/fileSize; oversized files return `413 FILE_TOO_LARGE` with maxFileSize; the UI shows a formatted shortfall message and the single-file limit instead of the misleading generic error. |
| Overall transfer rate (v011 feedback) | done | The transfer drawer shows the combined upload rate (B/KB/MB/GB per s) sampled every second and smoothed over 3 points; each upload tracks `loadedBytes`; the panel hides and the timer is cleaned up when no transfer is active or the component unmounts. |
| Create user with TOTP/IP allowlist directly (v012 功能3) | done | `POST /api/admin/users` accepts `totpEnabled`/`reenroll`/`ipAclEnabled`/`ipWhitelist`; one-time TOTP secret returned in the response; create-user modal gains a security section. |
| Brand layout: theme color not alone on a line (v012 功能4) | done | Theme color moved into the compact two-column brand grid instead of occupying its own row. |
| Public-deployment XFF trust toggle, default off (v012 功能10) | done | `settings.trustProxy` (default false) gates `X-Forwarded-For` parsing behind `--trusted-proxies`; admin system tab checkbox. |
| External-user upload collection links (v012 功能6) | done | Design confirmed; all logged-in users can create collection links; files land under the creator's `uploads/<token>/`; limits = expiry + upload count + per-file size; route `/u/:token`; optional remark field labeled 备注 (no 姓名 wording); anonymous chunked upload with token auth, IP rate limiting, quota into creator, revoke, and audit (`collection`/`upload_collect`/`upload_collect_fail`). DSH-tested 17/17 + expiry scenario; commit `80a71df`. |
| Per-user read-only window (v012 功能6 first part) | done | Admin sets a one-time from/until window per user; inside it the user can only view/download (all 12 write-operation entries return 403 READ_ONLY with audit reason read_only); admin exempt; set/clear via `PUT /api/admin/users/{id}/read-only`; /me and admin lists expose readOnly; frontend disables write entries with a notice bar. DSH-tested 8/8; commit `1e6a2c9`. |
| Share management (v012 功能8) | done | New `/shares` page: list my shares (file name/token/expiry/downloads/status), detail with usage, extend expiry, increase download limit (never lower), revoke single link (soft delete), copy link, per-share download logs; cross-user access hidden as 404. DSH-tested 17/17; commit `d687801`. |
| Share download logs & failure reasons (v012 功能9) | done | `shares.revoked_at` soft revoke (anonymous access 403 + audit share_revoked); `audit_logs.share_owner_id` lets sharers see anonymous download logs for their own shares; failure reasons expanded to share_not_found/share_expired/share_revoked/share_limit/share_denied; logActions adds share_extend/share_increase/share_revoke. |
| Batch sharing and detailed download progress (功能 5) | done | `POST /api/files/batch-share` creates one independent link per authorized file with all-or-nothing ownership validation and `batch_share` audit; FilesView shows/copies each link; single and ZIP downloads stream chunks with bytes, percentage, rate, cancellation, and ZIP `Content-Length`. |
| Sync feature filebox↔sftp (v012 功能11) | done | Per-user remote systems (password/key/passphrase auth, AES-GCM encrypted credentials) referenced by sync tasks; push/pull directions over SFTP with auto-created target directories, overwrite/skip/rename conflict policies, once/cron periodic scheduling (robfig/cron), immediate run with per-task mutex, 30-day log retention, and 404 cross-user isolation. End-to-end verified on a real Linux SFTP server (password/key/passphrase auth, push, pull with subdirs, cron auto-trigger, isolation); commit `d1c5b0c`. |
| v016 running backup WAL consistency | done | `admin backup` checkpoints SQLite WAL before archiving; restore validates that `filebox.db` is readable and non-empty before activation, with streaming hash verification for large files. Stage3 backup/restore reversal passed. |
| v016 collection upload disk protection and slot semantics | done | Collection `init` and `chunk` enforce minimum free space and request rate limits; collection slots are consumed only after successful `complete`, and abandoned task cleanup releases reservations. `DISK_FULL` and empty-init assertions passed. |
| v016 share download and preview safeguards | done | Missing files are opened before download counters are incremented; continuous Range requests deduplicate within a 60-second window; shared previews are capped at 64KB for text and 512KB for other content, including Range truncation. |
| v016 upload-task cancellation and sync resilience | done | `DELETE /api/upload-tasks/{taskID}` removes pending tasks/tmp chunks and releases quota; failed sync pulls roll back pending tasks; cron catch-up, independent manual-sync context, SFTP timeout, ZIP limits, folder locking, streaming backup/restore, and goroutine recovery are covered. |
| v016 JWT logout revocation and read-only coverage | done | `last_logout_at` invalidates tokens issued before logout; the read-only guard covers sync mkdir, password/language changes, collection and all other write paths; revoked-share responses are normalized. |
| v016 security and correctness hardening | done | Audit writes no longer prune on every insert; TOTP counters are monotonic; secrets length and frame-protection headers are enforced; restore requires `filebox.db`; admin safeguards, finite-share conversion, frontend 401 handling, directory-prefix filtering, i18n, and `golang.org/x/crypto` v0.35.0+ are complete. |
| v019 log time-range confirm/clear | done | Log filter popover gained Confirm/Clear actions with a draft state (timeDraft): edits are ignored until Confirm; Clear resets and reloads. |
| v019 pagination display fix | done | Logs, files, shares and share-groups pages show pagination correctly (template ref-unwrap bug fixed); per-page size and jump controls work. |
| v019 folder navigation normalization | done | navigateDir normalizes separators and trims slashes; the backend folder listing filters legacy `files/`/`files/<uid>/` prefixed paths and drops v010 backslash orphans and `.`/`..`, and `validateFolderName` rejects `.`/`..`, so entering folders never reports an invalid directory. |
| v019 aggregate-share eye interaction | done | The group eye opens the edit dialog (members add/remove + expiry/limit); card keeps only eye/copy/delete icons. |
| v019 sync log dialog width | done | The sync task details dialog widened to 1100px with a 1000px log table for better readability. |
| v019 folder batch operations | done | Folder rows support multi-select, batch deletion (non-empty failures reported separately) and batch rename (step-by-step dialog with progress); select-all covers current-page folders. |
