# FileBox v0.2.0 (Stage 2)

Stage 2 release notes

[中文](RELEASE_NOTES.md)

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
