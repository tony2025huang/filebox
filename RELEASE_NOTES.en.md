# FileBox v0.2.0 (Stage 2)

Stage 2 release notes

[中文](RELEASE_NOTES.md)

## v0.2.0-v019 (user-feedback fix batch)

Seven user-reported fixes (4 defects + 3 polish items):

**Defect fixes**
- Log time-range popover gained Confirm/Clear buttons
- Logs (67 rows) and Files pages now show pagination (template ref-unwrap bug fixed)
- Entering a folder no longer reports "invalid directory" (navigation normalization)
- Aggregate-share card icons simplified (eye/copy/delete); editing merged into the eye dialog

**Polish**
- Sync task log dialog widened to 1100px for better readability
- Folder rows support multi-select and batch deletion
- Pagination controls (page size / jump) display correctly on all four list pages

## v0.2.0-v018 (8 new requirements) release notes

v018 implements 8 new requirements raised by the user. Codex implemented them in small batches (all backend tasks by codex; a few frontend micro-edits were applied directly by the batch lead with identical specifications after codex processes kept terminating without writing); all passed full builds and tests.

### New features and improvements

- **#1 Topbar "System settings" label fix**: after v017 renamed the i18n key `nav.admin` to `nav.system`, the topbar section name looked up the missing key and fell back to the literal text; a section→key mapping (admin→nav.system, sync→nav.syncTasks) restores correct trilingual labels.
- **#2 Aggregate-share editing**: aggregate share cards gain an eye icon (member-file list dialog) and an edit dialog — add/remove member files (`GET/POST/DELETE /api/shared-groups/{token}/files`) and edit expiry and download limit (`PUT /api/shared-groups/{token}`; expiry must be in the future, the limit cannot be lower than the used count).
- **#3 Collections buttons on one row**: "Copy link" and "View received files" are grouped on the same row; the link spans the full width.
- **#4 Sync transfer progress**: in-flight sync tasks show the current file, files done, bytes transferred, and rate (2-second polling of `GET /api/sync/tasks/{id}/progress` with a progress bar).
- **#5 Sync log columns**: the log area is a table — start time / end time / status (running/ended) / next run (periodic tasks, cron-derived) / files / bytes / detail (expandable per-file lines).
- **#6 Log time-range UI**: the two datetime-local fields became one "Time range" button with a dropdown panel; either bound is optional.
- **#7 Pagination enhancements**: the My files / My shares (incl. aggregate shares) / My collections / Logs pages gain per-page-size selectors (10/20/50/100, remembered) and page-number jumping when more than 7 pages; the three list APIs now return page/pageSize/total (cap 100).
- **#8 Merged folder/file listing**: folders and files share one table (folders first, Folder icon + "Folder" type column, click to enter; rename/delete kept, no checkbox/file actions on folder rows).

### Data and deployment notes

- No database schema changes in this batch (sync_logs schema from v017 is unchanged).
- New APIs: `GET/POST /api/shared-groups/{token}/files`, `DELETE /api/shared-groups/{token}/files/{fileID}`, `PUT /api/shared-groups/{token}`, `GET /api/sync/tasks/{id}/progress`; `/api/shares`, `/api/collections`, `/api/shares/groups` now return `page/pageSize/total`.
- Sync progress is in-process state only (not persisted): after a restart an in-flight run reports no progress until the next run, and final results remain authoritative in sync_logs.
- Verification: `go build ./...` + `go test ./...` all green; 4 new tests (member add/remove & attribute edit, pageSize cap, next-run calculation); `npm run build` + webassets sync passed.

## v0.2.0-v017 (10 new requirements) release notes

v017 implements 10 new requirements raised by the user. Codex implemented them in small batches (1-2 items per batch, each committed and pushed separately); all passed full builds and tests.

### New features and improvements

- **#1 File library drops the embedded collections block**: the file library page no longer shows the "My collections" section or its create entry; collection management now lives solely on the standalone "My collections" page (/collections).
- **#2 Per-tab admin console descriptions**: the Overview / Users / Security / Branding / Locks / System tabs each show their own description text (trilingual) matching the tab's actual function.
- **#3 Navigation order and naming**: topbar order is now My files → My collections → My shares → Sync tasks → Logs → System settings; "Share management" was renamed "My shares" and "Admin" renamed "System settings" (trilingual).
- **#4 Audit log time-range filter**: the log page adds start/end time inputs; `GET /api/logs` accepts from/to (RFC3339), boundary-inclusive filtering, invalid times return 400; store and httpapi tests added.
- **#5 Sync credentials saved and viewable**: remote-system passwords stay AES-GCM encrypted with blank-edit keeping existing credentials; new `GET /api/sync/systems/{id}/secret` returns the decrypted secret once for the owner/admin with audit logging; the edit dialog shows a "saved" placeholder with an eye toggle to view/hide.
- **#6 Sync execution history**: task details show start time, end time, and the result (success/failure/running) for every execution.
- **#7 Sync path picker enhancements**: the directory browser filters the current directory by name and accepts a manually entered full path with confirmation (remote paths validated via browse).
- **#8 Sync task target host display**: task rows now show the target host (SFTP host:port or FileBox URL host) in the source → target column.
- **#9 Real-time sync log status**: execution writes a `running` row at start and updates it in place with the end time and result on completion; in-flight runs are visible in lists/details (sync_logs migration adds finished_at with automatic table rebuild).
- **#10 Change-password modal**: the topbar "Change password" entry opens a centered modal (submits POST /api/auth/change-password); forced password change (must_change_password) still redirects to the standalone page.

### Data and deployment notes

- sync_logs migration: on first startup the table is rebuilt automatically (adds `finished_at`, removes the result CHECK constraint), preserving foreign keys, indexes, and historical data; no manual steps.
- New endpoint: `GET /api/sync/systems/{id}/secret` (owner or admin only; returns decrypted credentials once and records an audit event).

## v0.2.0-v016 (security and logic review fixes) release notes

v016 was reviewed independently by codex and DSH. All 38 identified issues were fixed, followed by reversal tests and regression checks.

### Fix list

- **Critical issues**: running `admin backup` now checkpoints SQLite WAL, and restore validates that the database is readable and non-empty before activation; collection upload `init/chunk` now enforce disk protection, return `DISK_FULL` when space is insufficient, and apply request-level chunk throttling.
- **Evidence-backed medium issues**: `mkdirSyncSystem` is protected by the read-only guard; instant upload is limited to the target directory; share downloads open the file before incrementing the counter and deduplicate Range requests within a 60-second window; collection slots are consumed only after successful completion; shared previews are capped at 64KB for text and 512KB for other content, with Range truncation.
- **Remaining medium issues**: complete/rename share a directory lock; missed cron tasks are caught up; manual sync uses an independent context; SFTP has an overall timeout; batch ZIPs enforce total-byte and disk checks; backup/restore use streaming hashes; upload-task cancellation and sync-pull rollback were added; sync goroutines recover from panics and remote type assertions are safe.
- **Minor issues**: share-group cleanup, file-count semantics, JWT logout revocation, missing i18n keys, batch-share rollback, 5/min registration limiting, chunk read timeout, audit-write cleanup decoupling, monotonic TOTP counters, complete read-only coverage, fail-closed instant-upload errors, unified 404 revocation responses, data-directory pull temp files, secret-length validation, clickjacking headers, frontend 401 session handling, required restore entries and the nine V1-27 fixes were completed, along with password-change audit, last-admin protection, user-directory cleanup, unlimited-to-finite shares, the x/crypto upgrade, and admin directory-prefix filtering.

### Data and deployment notes

- `backup` now automatically checkpoints WAL; production deployments should still stop the service before backups to reduce the consistency window during active writes.
- New upload-task cancellation API: `DELETE /api/upload-tasks/{taskID}`. Cancellation removes the pending task and temporary chunks and releases the quota reservation.
- `logout` now revokes old JWTs through `last_logout_at`; tokens issued before that timestamp can no longer access protected endpoints.

## v0.2.0-v013 (fixes and new features) release notes

v013 is the batch of three fix rounds plus 15 feature requests (developed by codex, re-verified by dsh), released in the v0.2.0 single binary:

- **Security hardening (round 3, G1-G14)**: strict SFTP fingerprint decoding (rejects base64 padding-bit-corrupted pins, constant-time compare); sharePreview anonymous rate limit; collectionUploadChunk read-only check; atomic overwrite upload (old file kept until complete succeeds); RenameFolder pre-check + rollback + per-directory lock; rate-limiter capacity cap; backup 0600 permissions / restore extraction limits; `--jwt-secret` empty/short validation; unified frontend 401 redirect.
- **P0 defect fixes**: deduplicated share-exhausted message (single error output); transfer-panel failure tips get `title` tooltip for full text; collection quota errors sanitized (`COLLECTION_QUOTA_EXCEEDED`, no quota details leaked); sync directory picker supports navigating up (SFTP root `/` + filebox per-level parent); transfer records persisted to sessionStorage with re-select resume.
- **P1 UX**: sync source can select files; form controls unified to 40px; external upload logs include owner/dir (token masked); collection editing `PUT /api/collections/{id}` (expiry/count/per-file limit); shared `AuthenticatedTopbar` component unifies all views (files/shares/sync/logs/collections/admin).
- **P2 new features**: filebox↔filebox sync (kind/url + HTTP adapter with SSRF protection; MVP limits noted in the commit); batch-share aggregate link (share_groups table + `/g/:token` public page + single-file/selected ZIP anonymous download); SQLite WAL + synchronous=NORMAL performance optimization.

## v0.2.0-v012 (new-feature batch) release notes

v012 is the batch of 11 new feature requests (developed by codex, re-verified and tested by dsh), released in the v0.2.0 single binary:

- **Batch delete and per-user statistics (features 1/2)**: file multi-select batch delete (`POST /api/files/batch-delete`); the admin user list shows per-user folder/file counts and used bytes.
- **Create-user security settings (feature 3)**: the create-user dialog directly configures TOTP (one-time secret returned for handoff), TOTP re-enrollment, and the IP allowlist without a second edit.
- **Brand layout optimization (feature 4)**: the theme color no longer occupies its own row; the brand grid is more compact.
- **Batch sharing and download progress (feature 5)**: `POST /api/files/batch-share` creates independent links for multiple selected files; single-file and zip downloads stream with bytes/percentage/rate and cancellation.
- **Per-user read-only window (feature 6a)**: an admin sets a one-time window during which the user can only view/download; all write operations return `403 READ_ONLY`; admins are exempt.
- **External upload collections (feature 6b)**: public `/u/:token` page where external visitors upload without an account (expiry/upload-count/per-file-size limits); files land under the creator's directory and count against quota.
- **Explicit download-exhausted message (feature 7)**: exhausted share downloads return a structured `403 SHARE_DOWNLOAD_LIMIT` with a clear UI message instead of a browser permission error.
- **Share management (feature 8)**: the `/shares` page centrally manages shares — list/detail, extend expiry, increase the download limit (never lower), soft revoke, copy links, and download logs.
- **Share download logs and failure reasons (feature 9)**: sharers see their own share download logs (`share_owner_id`); failure reasons split into `share_not_found/expired/revoked/limit/denied`.
- **Public-proxy XFF trust toggle (feature 10)**: the `trustProxy` setting (default off) parses `X-Forwarded-For` only when enabled and the direct peer matches `--trusted-proxies`.
- **Sync feature (feature 11)**: per-user remote systems (SFTP password/key incl. passphrase, AES-GCM encrypted) and sync tasks — push/pull bidirectional, auto-created target directories, three conflict policies, one-shot/cron periodic scheduling, detailed logs (30-day retention); end-to-end verified on a real Linux SFTP server.

## v0.1.1 (v011 validation-feedback fixes) release notes

v011 is a **validation-feedback fix batch independent of the Stage 2 feature work** (source: user acceptance feedback from the local validation environment, 20 items, authoritative record in `docs/validation-feedback-v011.md`), released together with v0.2.0 (Stage 2). All fixes and enhancements are merged into the v0.2.0 single binary (`filebox-v011.exe` / `filebox-windows-amd64.exe`).

### Fixes

- **Concurrent same-name upload stall (item 18)**: conflict prompts are now a queue with a 60s cancel timeout, so dropping several same-name files resolves them one by one without leaving uploads stuck in "preparing"; renamed duplicates expose numbered user-visible names (`xx.txt`, `xx (1).txt`, `xx (2).txt`).
- **Upload failure logging (item 18)**: every rejection of upload initialization and chunk upload records an audit row and service event with a granular reason (invalid name / too large / conflict / disk full / quota exceeded / task not found / chunk size mismatch / etc.), filterable in the admin log page; failed/cancelled uploads stay in the transfer drawer with the reason and retry/dismiss actions.
- **Quota message improvements (item 19)**: quota rejection returns `QUOTA_EXCEEDED` with used/quota/file-size details and a formatted shortfall message; files over `--max-file-size` clearly state the single-file limit instead of the misleading generic name/size error.
- **Folder rename soft-delete conflict fix (D-S2-3)**: renaming a folder whose path rewrite collides with a soft-deleted row no longer returns 500; covered by a unit test.

### Enhancements

- **User-defined folders (item 6)**: automatic year/month storage layer removed; create/rename/delete Chinese-or-English folders (non-empty protected), breadcrumbs, upload into the current folder; `migrate-v010-paths` migrates legacy data in one command.
- **Transfer drawer + overall rate (items 4/20)**: a topbar "Transfers" button opens a drawer (uploads/downloads, streaming download progress); the drawer header shows the combined upload rate in real time (adaptive units, moving-average smoothing).
- **Tabbed admin console (items 14/15/16)**: left tab navigation (overview/users/security/branding/locks/system) with `?tab=` deep-links; create/edit user modal dialogs; log retention moved to the System tab.
- **Log experience (items 10/11)**: fixed green/red result colors independent of the theme; 20+ "system configuration" actions filterable with trilingual labels.
- **Account security (item 12)**: "Change password" entry in topbars; admins can "Require TOTP re-enrollment".
- **Branding & delivery (items 13/17/7/8/9)**: copyright text setting and a footer brand block (title + description, then copyright/ICP/police); topbar logo navigates home; single-binary delivery (`make release`/`release.ps1` with SHA256 checksums); README deployment guide (Windows/Linux).
- **Login & upload UX (items 1/2/3/5)**: default-account hint removed; create-user modal with inline errors; directory drag-and-drop shows a clear message instead of a network error; MD5 column toggle (persisted).

### Data and deployment

- Storage layout changed (year/month layer removed): run `filebox-v011.exe admin migrate-v010-paths --data=<data-dir>` before upgrading (backs up the DB, migrates physical folders and path records, idempotent).
- Binary named `filebox-v011.exe` to avoid confusion with the intermediate `filebox.exe`; user data in `--data` (branding/files/logs/lock settings) is fully preserved after migration.

## Overview

`v0.2.0` is the FileBox Stage 2 release. Built on v0.1.0 (Stage 1), it completes the transfer and collaboration features while remaining a single-binary deployment for Windows/Linux.

## What's new (Stage 2)

### Resumable chunked uploads

- Chunks of 2–8 MiB; out-of-order and duplicate uploads are accepted (idempotent overwrite), and every chunk records its SHA-256.
- The server persists uploaded chunks in the `chunks` table; `GET /api/files/{taskID}/status` returns the missing-chunk list, and uploads resume across server restarts.
- `complete` verifies all chunks, then merges them as a stream while computing MD5/SHA-256, and atomically moves the result into place; incomplete uploads are rejected.
- The frontend uploads with 4 concurrent workers, per-file progress, pause/resume (only missing chunks are re-sent), and exponential-backoff retry; small files still use the single-part path, fully compatible with Stage 1.

### Instant upload

- `POST /api/files/check` matches MD5 first, then SHA-256, within the same user; a hit returns the existing record without writing new content.
- The frontend computes SHA-256 with WebCrypto and checks automatically before uploading.

### Folder / multi-file upload

- `upload-init` accepts a relative `dir` field; storage preserves `data/files/<user>/<yy>/<mm>/<relative-dir>/<name>`.
- Identical names in different directories stay independent and unsuffixed; directory names are strictly validated (no `..`, control characters, or Windows-illegal characters).

### Share links

- Create shares with an expiry (hours) and a download limit (0 = unlimited) backed by a 64-character random token.
- Anonymous `meta` and `download` endpoints: expired links return 404/403 with a clear message, over-limit downloads return 403, and the download counter is atomic.
- All shares of a file can be revoked; share create/view/download are recorded in the audit log (`share` / `share_view` / `share_download`).
- A new anonymous `/:token` share page shows metadata, preview, and download.

### Online preview

- `GET /api/files/{id}/preview` serves images/text/video/PDF/JSON inline from a MIME whitelist (with Range support) and forces `attachment` for everything else.
- The file page adds a preview dialog (image/video/PDF/text).

### Upload rate limiting

- A per-user token bucket (`golang.org/x/time/rate`); administrators configure `uploadRateLimit` in bytes/sec (0 = unlimited, the default), enforced before chunk writes.
- The admin UI inputs KB/s; changes take effect on the next request.

### Registration switch

- `POST /api/auth/register` is gated by the `registerEnabled` setting (default off; `--register-enabled` is only a first-deployment seed).
- When enabled, registration enforces the password policy, creates a regular user, and returns a login token immediately; `GET /api/brand` exposes the switch publicly and the login page shows the entry accordingly.

### Extended system statistics

- Admin statistics add the active share count and cumulative share downloads alongside users/files/bytes/disk.

### User-feedback batch (delivered with v0.2.0)

- The login page no longer shows the "default admin: admin / admin123" hint (removed from all three dictionaries).
- A topbar "Transfers" button opens an independent transfer drawer separating uploads and downloads; downloads now stream with progress.
- The file-list integrity column shows `md5` directly by default, with a persistent "Show MD5" toolbar switch.
- Both the folder-picker button and directory drag-and-drop upload folders; invalid drops (such as empty folders) show a clear message instead of a misleading network error.

### User-defined folders (v011 feedback batch, delivered with v0.2.0)

- Removed the automatic year/month storage layer: storage is now `data/files/<user_id>/[<custom-dir>/]<filename>`, with files at the user root when no folder is used.
- Added the `folders` table and folder CRUD API (create/list/rename/delete; renames cascade to children and file paths with a physical directory move; non-empty folders cannot be deleted).
- The file page gained breadcrumbs, a "New folder" button (Chinese names supported), uploads into the current folder, and directory-filtered listings.
- Folders are isolated per user; quotas remain user-wide (files inside folders count toward the quota).
- Added `filebox admin migrate-v010-paths`: migrates the legacy `files/<uid>/<yy>/<mm>` layout to `files/<uid>/<yy>-<mm>` (for example `2026-08`), backs up the database before migrating, registers historical folder records, and is idempotent.

### Log and branding experience (v011 feedback batch, delivered with v0.2.0)

- The log page "result" column now uses fixed green for success and fixed red for failure, independent of the theme color.
- The log filter gained a "System configuration" group covering 20+ actions (settings/brand/language/password/user/TOTP/IP-allowlist/folders/stats) with trilingual labels.
- The topbar gained a "Change password" entry so regular users can rotate their password anytime; admins can check "Require TOTP re-enrollment" so the user re-binds on the next login.
- Brand settings gained a copyright field (for example `Copyright © 2026 xxx`) rendered in the footer with empty-value suppression.

### Tabbed admin console and UX (v011 feedback batch, delivered with v0.2.0)

- The admin console is now a left tab navigation with a content area: Overview (stats + system language) / Users (search + table) / Security (password policy + IP locking) / Branding / Locks / System (log retention days + registration switch + upload rate limit); `?tab=` deep-links to a tab and the active tab survives refresh.
- Create/edit user flows are centered modal dialogs (backdrop-click to close) with full role/quota/password/disabled/TOTP re-enrollment/IP-allowlist fields.
- The log page no longer embeds the retention panel; log retention is managed from the admin System tab (same API).
- The footer is now a brand info block: site title + description on the first line, followed by copyright / ICP / police filing lines.

### Concurrent uploads and failure feedback (v011 feedback batch, delivered with v0.2.0)

- Same-name conflicts are handled as a queue: dropping several same-name files shows one conflict prompt after another, every upload is eventually resolved (with a 60s cancel timeout), and uploads can no longer stall in "preparing"; renamed duplicates get user-visible numbered names (`xx.txt`, `xx (1).txt`, `xx (2).txt`).
- Failed or cancelled uploads stay in the transfer drawer with a red status and reason, and can be retried or dismissed instead of silently disappearing after a couple of seconds.
- Every rejection path of upload initialization and chunk upload now records an audit row and a service event with a granular reason (invalid name / too large / conflict / disk full / quota exceeded / task not found / etc.), filterable in the admin log page.

### Quota and single-file limit message improvements (v011 feedback batch, delivered with v0.2.0)

- Quota rejection returns `QUOTA_EXCEEDED` with used/quota/file-size details; the UI shows "Quota exceeded: X used of Y, this file needs Z, short by W. Free up space or adjust the quota."
- Files over `--max-file-size` return `413 FILE_TOO_LARGE` with the limit; the UI clearly says the file exceeds the single-file limit instead of the misleading generic name/size error.

### Overall transfer rate (v011 feedback batch, delivered with v0.2.0)

- The transfer drawer shows the combined upload rate (for example `12.5 MB/s`) across all active uploads, with adaptive units (B/KB/MB/GB per s), 1-second sampling and a 3-point moving average; the panel hides when nothing is uploading.

### Single-binary delivery and deployment guide

- The deliverable is a single executable (embedded frontend, pure-Go SQLite, zero runtime dependencies); `make release` (or `scripts\release.ps1` on Windows) produces `dist/filebox-windows-amd64.exe`, `dist/filebox-linux-amd64`, and `dist/SHA256SUMS.txt` (CGO_ENABLED=0, -trimpath, -s -w) that run without Go or Node installed.
- README gained a "Deployment guide (single-binary delivery)" section with step-by-step Windows (download/run/firewall/sc or NSSM/HTTPS) and Linux (download/chmod/foreground/systemd unit/Nginx reverse proxy/--trusted-proxies) instructions plus general production notes.

### Stage 1 features preserved

Login/logout, file CRUD, quotas, dual hashes, disk protection, conflict overwrite/rename, filename validation, audit logs, login locking, TOTP, IP allowlists, branding, multilingual UI, theme color, forced password change, maintenance CLI, service logging, and service/reverse-proxy deployment all remain available and pass regression testing.

## Security notes

- The default administrator is `admin/admin123` on first start; change the password or disable the account immediately after first login.
- Public deployments must set a strong random `--jwt-secret`/`FILEBOX_JWT_SECRET` and use an HTTPS reverse proxy.
- Share links allow anonymous downloads: tokens are random and unguessable, but `meta` exposes the file name and size, so do not share private files whose names are sensitive; shares can be revoked at any time.
- Public registration is off by default; before opening it on the public internet, confirm the password policy and quota settings in the admin panel.
- JWTs expire after 7 days by default; logout does not maintain a server-side JWT blacklist.

## Deployment

Development mode:

```powershell
npm --prefix web install
npm --prefix web run build
go run ./scripts/sync-web.go
go run ./cmd/filebox
```

Production builds use `make build` (Windows `bin/filebox.exe`) or `make build-linux` (Linux); the default data root is `./data` and the default listen address is `:8080`. The first start creates `admin/admin123`.

## Known limitations

- Instant upload matches only within the same user (no cross-user content deduplication).
- Rate limiting is a per-user token bucket applied to chunk writes; setting changes take effect on the next request.
- Not implemented: scheduled cleanup of abandoned upload tasks and server-pushed upload progress.

## Change history

- v0.2.0 batches and defect fixes are recorded in [`docs/requirements/CHANGELOG.md`](docs/requirements/CHANGELOG.md) (Stage 2 batches A/B/C plus D-S2-1/D-S2-2 fixes).
- v0.1.0 (Stage 1) is described in the repository history and [`docs/requirements/CHANGELOG.md`](docs/requirements/CHANGELOG.md).
