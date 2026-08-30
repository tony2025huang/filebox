# FileBox Requirement State

Updated: 2026-08-30 (v012 批次 B：功能 3/4/10 已交付；功能 6 设计已确认待开发)

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
