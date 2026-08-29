# FileBox v0.1.0

Stage 1 release notes

[中文](RELEASE_NOTES.md)

## Release overview

`v0.1.0` is the FileBox Stage 1 release for single-binary deployment on Windows and Linux.

## Delivered

### MVP

- JWT login, logout, and current-user lookup; bcrypt password hashing; `admin`/`user` roles and multi-user file isolation.
- Single-file single-chunk upload, paginated search, Range downloads (`206 Partial Content`), and deletion.
- MD5 and SHA-256 computation on completion; SQLite metadata persistence.
- User quotas, admin user CRUD, and storage statistics.

### R-DISK

- Windows/Linux disk capacity, used, free, and usage statistics.
- `--min-free-space` defaults to 2 GiB; upload initialization returns `DISK_FULL` below the threshold.

### R-NAME / R-CONFLICT

- Original on-disk names under user/year/month directories with unique storage paths.
- Same-name initialization returns `409`; the service supports transactional overwrite or the smallest available numbered suffix.

### R-VALID

- Pre-upload rejection of separators, control characters, Windows-illegal characters, traversal markers, empty/dot names, and names over 255 bytes.

### R-LOG / R-LOCK

- Login, upload-completion, and download outcomes with user isolation, admin filtering, pagination, and lazy retention cleanup.
- Failure thresholds, temporary/permanent locks, automatic unlock, uniform login errors, and admin reset.

### R-BRAND

- Site title, SEO description, favicon, login/main logos, ICP text, and public-security filing text.
- 512 KiB custom-asset limit with extension/content checks, atomic saves, embedded SVG fallback, and no blank filing footer for empty text.

## Security notice

The default first-start administrator is `admin/admin123`; change the password immediately after the first login or disable the account. Public deployments must set a strong random `--jwt-secret` or `FILEBOX_JWT_SECRET` and use an HTTPS reverse proxy. JWTs expire after 7 days by default; logout does not maintain a server-side JWT blacklist.

## Deployment and operation

Development mode:

```powershell
npm --prefix web install
npm --prefix web run build
go run ./scripts/sync-web.go
go run ./cmd/filebox
```

Use `make build` for a production build and `make start` to run it; Windows can run `bin/filebox.exe`, while Linux uses `make build-linux`. The default data root is `./data`, and the default listener is `:8080`.

## Known limitations and Stage 2 roadmap

Stage 1 excludes resumable chunks, true multi-chunk concurrency, instant upload, folder uploads, share links, online previews, rate limiting, open registration, and scheduled cleanup of abandoned upload tasks. Stage 2 will focus on these transfer and collaboration capabilities; `--register-enabled` currently has no public registration route.

## Change history

See [`docs/requirements/CHANGELOG.md`](docs/requirements/CHANGELOG.md) for detailed requirement changes.
