# FileBox

[中文](README.md)

FileBox is a self-hosted file transfer and management service that runs as a single Go binary on Windows or Linux. Stage 1 provides multi-user isolation, JWT login, single-file uploads, Range downloads, dual-hash verification, quotas, disk protection, audit logs, configurable branding, and a multilingual interface; Stage 2 (v0.2.0) adds resumable chunked uploads, instant upload, folder upload, share links, online preview, upload rate limiting, an optional public registration switch, and extended system statistics.

## Features

- Multi-user roles: `admin` and `user`; administrators manage accounts, roles, quotas, passwords, and disabled state, while regular users can access only their own files.
- JWT login and security: passwords use bcrypt; JWTs expire after 7 days by default; repeated failures can trigger temporary or permanent locks under the admin policy, with optional automatic unlock; login failures use one uniform response to reduce account enumeration.
- File transfer: upload initialization, single chunk `0` upload, completion, paginated/searchable listing, deletion, and download; `http.ServeContent` supports `Range` and returns `206 Partial Content` for range requests.
- Resumable chunked uploads: chunks of 2–8 MiB with out-of-order/idempotent re-uploads; the server persists uploaded chunks in the `chunks` table, `GET /api/files/{taskID}/status` reports progress for resume, and `complete` verifies all chunks then merges them as a stream while computing MD5 and SHA-256; the frontend uploads with 4 concurrent workers, pause/resume, and retry.
- Instant upload: `POST /api/files/check` matches MD5 first then SHA-256 within the same user; a hit returns the existing record without writing new content; the frontend computes SHA-256 with WebCrypto for instant checks.
- Folder upload: `upload-init` accepts a relative `dir` field and preserves `data/files/<user>/<yy>/<mm>/<relative-dir>/<name>`; identical names in different directories stay unsuffixed.
- User-defined folders (v011): the automatic year/month storage layer was removed in favor of `data/files/<user_id>/[<custom-dir>/]<name>`; the file page offers breadcrumbs, a "New folder" button (Chinese names supported), cascading folder renames, and empty-folder deletion; `GET /api/files?dir=` filters by directory; `filebox admin migrate-v010-paths` migrates the legacy `yy/mm` layout to `yy-mm` (with an automatic database backup).
- Share links: create with an expiry and download limit, anonymous metadata and downloads (expired/over-limit requests are rejected), and revocation; share create/view/download are recorded in the audit log, and an anonymous `/:token` share page is included.
- Online preview: `GET /api/files/{id}/preview` serves images/text/video/PDF/JSON inline from a MIME whitelist and forces attachment downloads for everything else.
- Upload rate limiting: a per-user token bucket (`golang.org/x/time/rate`) configured by administrators in bytes/sec (`0` = unlimited, the default), applied before chunk writes.
- Public registration: `POST /api/auth/register` is gated by the `registerEnabled` setting (default off), creates a regular user under the password policy, and logs the user in; the login page shows the entry only when the public switch is on.
- Transfer UX (user-feedback batch): a topbar "Transfers" button opens an independent drawer separating uploads and downloads (downloads stream with progress); the file list shows `md5` directly by default (with a persistent toggle); both the folder-picker button and directory drag-and-drop upload folders, and invalid drops (for example empty folders) show a clear message instead of a network error.
- Tabbed admin console (v011 feedback batch): the admin page is a left tab navigation (Overview / Users / Security / Branding / Locks / System) with `?tab=` deep-links that survive refresh; create/edit user flows are centered modal dialogs (role, quota, password reset, disable, TOTP re-enrollment, IP allowlist); the log page's retention panel moved to the System tab; the footer renders a brand info block (site title + description, then copyright / ICP / police lines, rendered whenever any value is non-empty).
- Concurrent upload resilience (v011 feedback batch): same-name conflict prompts are a queue with a 60s cancel timeout so concurrent duplicates are handled one by one without stalling uploads; failed/cancelled uploads stay in the transfer drawer with the failure reason and retry/dismiss actions; upload-init and chunk-upload rejections are audited with granular reasons.
- Quota and size-limit messages (v011 feedback batch): quota rejections return `QUOTA_EXCEEDED` with used/quota/file-size details and a formatted shortfall message; oversized files return `413 FILE_TOO_LARGE` with the limit instead of a misleading generic error.
- Overall transfer rate (v011 feedback batch): the transfer drawer shows the combined upload rate of all active uploads in real time (adaptive units, 1-second sampling with a 3-point moving average), hidden automatically when nothing is uploading.
- Name conflicts: a same-name file in the user's current monthly directory causes `409`; the frontend offers overwrite or rename; overwrite is transactional and rename allocates the smallest available numeric suffix.
- Filename security: pre-upload validation rejects separators, control characters, Windows-illegal characters, traversal markers, empty/`.`/`..` names, and names over 255 bytes; the on-disk name preserves the original semantics while replacing selected illegal characters.
- Quotas: initialization reserves pending bytes and the default quota is 100 GiB; overwrite subtracts the old file usage first.
- Integrity: the server computes MD5 and SHA-256 from the actual content; clients may submit expected values for comparison.
- Disk protection: admin statistics expose capacity, used, free, and usage percentage; initialization requires 2 GiB free by default and the threshold can be changed or disabled.
- Audit operations: login, upload completion, and successful/failed downloads record username, target, source IP, result, and reason; users see their own records and admins can filter all records; writes lazily prune records older than the retention period, defaulting to 30 days.
- Branding: administrators can set the title, SEO description, login/main logos, favicon, ICP text, and public-security filing text; empty text renders no blank area, unset assets use embedded SVG defaults, and assets are limited to 512 KiB with extension/content checks.
- Multilingual UI: Simplified Chinese, Traditional Chinese, and English are supported; administrators choose the system default and users can choose an immediate personal preference.
- Interface theme color: administrators can enter `#RGB`/`#RRGGBB` or use the color picker, restore the default, and apply the main color immediately across the site.
- Advanced account security: configurable initial admin credentials with forced password change, password strength policy, TOTP, per-user IP allowlists, IP login locks, and lock management.

### Stage delivery markers

`MVP`: login, JWT, bcrypt, SQLite, multi-user isolation, file upload/download/deletion, paginated search, single-file single-chunk transfer, MD5/SHA-256, and the admin console.

`R-DISK`: cross-platform disk statistics, minimum free-space configuration, and `DISK_FULL` upload protection.

`R-NAME`: original on-disk names under per-user/month directories with unique storage paths.

`R-CONFLICT`: `409` same-directory conflicts with overwrite and numbered rename choices.

`R-VALID`: pre-upload filename safety validation.

`R-LOG`: operation audit, retention cleanup, pagination/filtering, and the logs page.

`R-LOCK`: failed-login thresholds, temporary/permanent locks, automatic unlock, uniform errors, and admin reset.

`R-BRAND`: text and logo/favicon settings, resource validation, embedded fallback, and filing footer.

`R-LANG`: complete Simplified Chinese, Traditional Chinese, and English UI dictionaries with system-default and personal language preferences.

`R-THEME`: configurable interface theme color with hex input, color picker, default reset, and immediate application.

`R-INIT/R-PWD/R-TOTP/R-IPACL/R-IPBAN/R-LOCKADMIN`: forced initial password change, password policy, encrypted TOTP, source-IP allowlists, IP login lockout, and administrator lock management.

`R-OPS`: local maintenance commands bypassing web authentication for direct SQLite recovery when the service or web UI is unavailable. Restrict server shell access to trusted operators.

`R-SRVLOG`: optional service file logging with local-date rotation, gzip archives, and retention cleanup; disabled mode writes only to the console.

`R-SERVICE/R-PROXY`: Linux systemd, Windows NSSM/sc, and Nginx HTTPS reverse-proxy templates are in [`deploy/`](deploy/).

`STAGE2`: resumable chunked uploads, instant upload, folder upload, share links, online preview, upload rate limiting, the optional registration switch, and extended system statistics (see the feature list above).

## Technology stack

Go 1.22+ standard-library HTTP, `modernc.org/sqlite`, Vue 3, Vite, `vue-router`, `lucide-vue-next`, and `embed`. The Vite production bundle and embedded branding assets are packaged into the Go binary.

## Quick start

Requirements: Go 1.22+, Node.js 20+, and npm. Development mode:

```powershell
npm --prefix web install
npm --prefix web run build
go run ./scripts/sync-web.go
go run ./cmd/filebox
```

The default listener is `:8080`; open <http://localhost:8080>. The first start creates `admin/admin123`; change the password immediately after the first login or disable the account. You can also run `go run ./cmd/filebox --addr=:8080 --data=./data`. For frontend-only development, run `npm run dev` from `web/`; Vite proxies `/api` to `http://localhost:8080`.

## Local maintenance commands

The single binary also provides local maintenance subcommands. `admin reset-password` resets an administrator or regular-user password and forces a password change at the next login; `admin clear-ip-acl` disables and clears a user's IP allowlist so an administrator can recover from an allowlist misconfiguration; `locks list` shows IP and user lock state; `locks clear` clears a lock by IP, user ID, or all records. `--generate` creates and prints a one-time 16-character strong password.

```bash
filebox admin reset-password --data=./data --username=admin --new-password='NewPass123!'
filebox admin reset-password --data=./data --username=admin --generate
filebox admin clear-ip-acl --data=./data --username=admin
filebox locks list --data=./data
filebox locks clear --data=./data --ip=1.2.3.4
filebox locks clear --data=./data --user=2
filebox locks clear --data=./data --all
```

For production, run `make build` and then `make start`; Windows can run `bin/filebox.exe`, while Linux uses `make build-linux`. Production deployments must set a strong random `FILEBOX_JWT_SECRET` and use an HTTPS reverse proxy.

## Service logs

Service logs are disabled by default. When enabled, they are written to the console and `filebox-YYYY-MM-DD.log`; the previous day is gzip archived and logs older than the retention period are removed. Service logs never record passwords, tokens, or file contents; event lines include the operator and source IP.

```bash
filebox --log-enabled=true --log-dir=/var/log/filebox --log-retention-days=90
```

## Deployment guide (single-binary delivery)

The deliverable is a single executable: the web frontend is embedded, SQLite is pure Go, and there are no runtime external dependencies (no Node, no separate frontend server). Copy it to the target machine and run.

### Windows deployment

1. Download `filebox-windows-amd64.exe` (or `bin/filebox.exe`).
2. Run:

   ```powershell
   .\filebox.exe --addr=127.0.0.1:18080 --data=C:\filebox\data --log-enabled=true --log-dir=C:\filebox\logs
   ```

3. Open <http://127.0.0.1:18080>; change the initial `admin/admin123` password immediately after the first login.
4. As a Windows service: `sc create filebox binPath= "\"C:\filebox\filebox.exe\" --addr=127.0.0.1:18080 --data=C:\filebox\data" start= auto`, or use NSSM (template in `deploy/README.md`).
5. Allow the listening port through the firewall; production must set a strong random `--jwt-secret` (or `FILEBOX_JWT_SECRET`) and enable HTTPS behind an Nginx/IIS reverse proxy.

### Linux deployment

1. Download `filebox-linux-amd64` and make it executable:

   ```bash
   chmod +x filebox-linux-amd64
   ```

2. Run in the foreground to verify:

   ```bash
   ./filebox-linux-amd64 --addr=127.0.0.1:18080 --data=/var/lib/filebox --log-enabled=true --log-dir=/var/log/filebox
   ```

3. Configure the systemd unit `/etc/systemd/system/filebox.service`:

   ```ini
   [Unit]
   Description=FileBox file transfer service
   After=network.target

   [Service]
   User=filebox
   ExecStart=/opt/filebox/filebox-linux-amd64 --addr=127.0.0.1:18080 --data=/var/lib/filebox --jwt-secret=replace-with-a-strong-random-value --log-enabled=true --log-dir=/var/log/filebox
   Restart=on-failure
   RestartSec=3

   [Install]
   WantedBy=multi-user.target
   ```

   ```bash
   systemctl daemon-reload
   systemctl enable --now filebox
   systemctl status filebox
   ```

4. HTTPS reverse proxy (Nginx template in `deploy/README.md`); `--trusted-proxies` must match the actual proxy networks.

### General production notes

Use a dedicated run user, dedicated data/log directories, a strong random JWT secret, HTTPS, a `--trusted-proxies` allowlist, and regular backups of the `--data` directory.

### Building the single-file deliverables

`make release` (or `scripts\release.ps1` on Windows) produces `dist/filebox-windows-amd64.exe`, `dist/filebox-linux-amd64`, and `dist/SHA256SUMS.txt` (checksums) using `CGO_ENABLED=0` and `-trimpath -ldflags="-s -w"`; the binaries are statically trimmed and run without Go or Node installed.

## Configuration

Each flag reads its `FILEBOX_*` environment variable as the default; an explicit command-line value takes precedence.

| Flag | Environment variable | Default | Meaning |
|---|---|---:|---|
| `--addr` | `FILEBOX_ADDR` | `:8080` | HTTP listen address |
| `--data` | `FILEBOX_DATA` | `./data` | Root for SQLite, files, temporary content, and branding assets |
| `--max-file-size` | `FILEBOX_MAX_FILE_SIZE` | `107374182400` (100 GiB) | Backend per-file size limit |
| `--min-free-space` | `FILEBOX_MIN_FREE_SPACE` | `2147483648` (2 GiB) | Minimum free space at upload initialization; `0` disables protection |
| `--jwt-secret` | `FILEBOX_JWT_SECRET` | `filebox-development-secret-change-me` | HS256 signing key; replace in production |
| `--register-enabled` | `FILEBOX_REGISTER_ENABLED` | `false` | First-deployment seed for the registration switch; the persisted `registerEnabled` admin setting controls later restarts |
| `--admin-user` | `FILEBOX_ADMIN_USER` | `admin` | Initial administrator username |
| `--admin-pass` | `FILEBOX_ADMIN_PASS` | `admin123` | Initial administrator password; forced change on first login |
| `--trusted-proxies` | `FILEBOX_TRUSTED_PROXIES` | empty | Trusted proxy IP/CIDR list; empty means X-Forwarded-For is ignored |
| `--log-enabled` | `FILEBOX_LOG_ENABLED` | `false` | Enable service file logs; console output remains enabled |
| `--log-dir` | `FILEBOX_LOG_DIR` | `<executable directory>/logs` | Service log directory |
| `--log-retention-days` | `FILEBOX_LOG_RETENTION_DAYS` | `90` | Retention for service logs and gzip archives |

The backend limit is 100 GiB; the Stage 2 frontend splits large files into 4 MiB chunks, still bounded by the backend limit. Admin policy defaults are 30-day log retention, lock after 5 failures, automatic unlock enabled, a 5-minute unlock period, unlimited upload rate (`0`), and the registration switch off.

## Service deployment

Complete systemd, Windows NSSM/sc, and Nginx reverse-proxy examples and security notes are in [`deploy/README.md`](deploy/README.md) and [`deploy/README.en.md`](deploy/README.en.md). Production should use a dedicated account, separate data/log directories, a strong random JWT secret, HTTPS, and `--trusted-proxies` matching the actual proxy networks.

## API summary

JSON responses use `{ "code": number, "message": string, "data": any }`; protected endpoints require `Authorization: Bearer <token>`. Main paths are auth `/api/auth/login`, `/api/auth/register`, `/api/auth/totp`, `/api/auth/totp-qrcode`, `/api/auth/change-password`, `/api/auth/logout`, `/api/auth/me`, `/api/auth/language`; public branding `/api/brand` and `/brand/{asset}`; files `/api/files`, `/api/files/upload-init`, `/api/files/check`, `/api/files/{taskID}/chunks/{index}`, `/api/files/{taskID}/status`, `/api/files/{taskID}/complete`, `/api/files/{id}/download`, `/api/files/{id}/preview`, `/api/files/{id}/share`, `/api/files/{id}/shares`, `/api/files/shared/{token}/meta`, `/api/files/shared/{token}/download`, `/api/files/{id}`; logs `/api/logs`, `/api/logs/actions`; and admin `/api/admin/users[/{id}]`, `/api/admin/users/{id}/totp`, `/api/admin/users/{id}/ip-acl`, `/api/admin/stats`, `/api/admin/settings`, `/api/admin/brand`, `/api/admin/locks`.

File, user, and log lists default to `pageSize=20` and cap it at 100. Administrators can view all files and logs; regular users are isolated by account. Downloads support Range, and logout does not maintain a server-side JWT blacklist.

## Directory structure

`cmd/filebox/` is the Go entry point; `internal/httpapi/` handles routes, auth, transfer, download, branding, and audit; `internal/store/` owns the SQLite schema and persistence; `internal/diskusage/` provides Windows/Linux disk statistics; `internal/webassets/` provides embedding and fallback assets; `scripts/sync-web.go` syncs the frontend bundle; `web/src/` is the Vue frontend; `docs/requirements/` stores requirement state and change history; `data/` is the default runtime data directory.

## Data storage

Under `--data`: `filebox.db`; with the default path, `data/files/<userID>/<yy>/<mm>[/<relative-dir>]/<stored-name>`, `data/tmp/<taskID>/<chunk-index>`, and `data/brand/<favicon|login-logo|main-logo>.<ext>`. SQLite stores metadata, ownership, quotas, audit logs, chunk records (`chunks`), and share records (`shares`). Chunks are staged in the temporary directory and `complete` verifies them, then merges them as a stream into the user/year/month directory (optionally preserving a folder structure). The visible name is `name`, while the on-disk name is `stored_name`; conflicts use the smallest suffix such as `name (1).ext` and `name (2).ext`. Deletion marks `deleted`, subtracts quota, and removes physical content; storage paths held by soft-deleted records are automatically reused on re-upload.

## Known limitations

- Instant upload matches only within the same user (no cross-user content deduplication).
- Share links are random-token based; the `meta` endpoint exposes the file name, size, and usage counters, so do not share files that are sensitive in name or size.
- Rate limiting is a per-user token bucket applied to chunk writes; setting changes take effect on the next request.
- Not yet implemented: scheduled cleanup of abandoned upload tasks and server-pushed upload progress.

## License and acknowledgements

The repository currently has no project-level `LICENSE` file. Third-party dependencies remain subject to their own licenses; see the upstream notices for the Go modules and npm packages. Requirement state and change history are in [`docs/requirements/STATE.md`](docs/requirements/STATE.md) and [`docs/requirements/CHANGELOG.md`](docs/requirements/CHANGELOG.md).
