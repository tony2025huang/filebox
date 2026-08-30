# FileBox Requirement State

Updated: 2026-08-29 (Stage 2 delivered as v0.2.0)

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
