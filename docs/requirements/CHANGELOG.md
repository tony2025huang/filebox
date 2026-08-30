# Requirement Change Log

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
