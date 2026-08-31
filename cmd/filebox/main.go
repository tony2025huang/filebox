package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"filebox/internal/httpapi"
	"filebox/internal/srvlog"
	"filebox/internal/store"
	"filebox/internal/webassets"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/pbkdf2"
	_ "modernc.org/sqlite"
)

const version = "dev"

// restore 解压限额：防止压缩炸弹耗尽磁盘（条目数 / 单文件 / 总量）。
// Restore extraction limits guard against decompression bombs exhausting disk (entries / single file / total).
const (
	restoreMaxEntries     = 200000
	restoreMaxSingleBytes = 200 << 30 // 200 GiB
	restoreMaxTotalBytes  = 4 << 40   // 4 TiB
)

// minJWTSecretBytes 是 --jwt-secret / FILEBOX_JWT_SECRET 的最小长度，空或过短直接拒绝。
// minJWTSecretBytes is the minimum length for --jwt-secret / FILEBOX_JWT_SECRET; empty or too short is rejected.
const minJWTSecretBytes = 16

type cliFailure struct {
	code    int
	err     error
	printed bool
}

type loggingOptions struct {
	enabled       *bool
	dir           *string
	retentionDays *int
}

func addLoggingFlags(flags *flag.FlagSet) loggingOptions {
	return loggingOptions{
		enabled:       flags.Bool("log-enabled", envBool("FILEBOX_LOG_ENABLED", false), "enable service log files"),
		dir:           flags.String("log-dir", envOr("FILEBOX_LOG_DIR", defaultLogDir()), "service log directory"),
		retentionDays: flags.Int("log-retention-days", int(envNonNegativeInt64("FILEBOX_LOG_RETENTION_DAYS", 90)), "service log retention in days"),
	}
}

func (options loggingOptions) newLogger() (*srvlog.Logger, error) {
	if *options.retentionDays < 0 {
		return nil, errors.New("log retention days must not be negative")
	}
	return srvlog.New(srvlog.Config{Enabled: *options.enabled, Dir: *options.dir, RetentionDays: *options.retentionDays}), nil
}

func (e *cliFailure) Error() string { return e.err.Error() }

func (e *cliFailure) Unwrap() error { return e.err }

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		if err := runServe(args); err != nil {
			return finishCLI(err)
		}
		return 0
	}
	switch args[0] {
	case "serve":
		if err := runServe(args[1:]); err != nil {
			return finishCLI(err)
		}
		return 0
	case "admin":
		return runAdmin(args[1:])
	case "locks":
		return runLocks(args[1:])
	default:
		printUsage(os.Stderr)
		return 2
	}
}

func runServe(args []string) error {
	// main 启动 FileBox HTTP 服务，并从 flag 或环境变量加载运行配置。
	// main starts the FileBox HTTP service and loads runtime settings from flags or environment variables.
	flags := flag.NewFlagSet("filebox serve", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	addr := flags.String("addr", envOr("FILEBOX_ADDR", ":8080"), "HTTP listen address")
	dataDir := flags.String("data", envOr("FILEBOX_DATA", "./data"), "data directory")
	maxFileSize := flags.Int64("max-file-size", envInt64("FILEBOX_MAX_FILE_SIZE", 100*1024*1024*1024), "maximum file size in bytes")
	minFreeSpace := flags.Int64("min-free-space", envNonNegativeInt64("FILEBOX_MIN_FREE_SPACE", 2*1024*1024*1024), "minimum free disk space in bytes; 0 disables upload protection")
	registerEnabled := flags.Bool("register-enabled", envBool("FILEBOX_REGISTER_ENABLED", false), "enable public registration")
	jwtSecret := flags.String("jwt-secret", envOr("FILEBOX_JWT_SECRET", "filebox-development-secret-change-me"), "JWT signing secret")
	adminUser := flags.String("admin-user", envOr("FILEBOX_ADMIN_USER", "admin"), "initial administrator username")
	adminPass := flags.String("admin-pass", envOr("FILEBOX_ADMIN_PASS", "admin123"), "initial administrator password")
	trustedProxiesValue := flags.String("trusted-proxies", envOr("FILEBOX_TRUSTED_PROXIES", ""), "trusted proxy IPs or CIDRs, comma-separated")
	logging := addLoggingFlags(flags)
	if err := flags.Parse(args); err != nil {
		return &cliFailure{code: 2, err: err, printed: true}
	}
	if flags.NArg() != 0 {
		return &cliFailure{code: 2, err: fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))}
	}
	trustedProxies, err := parseTrustedProxies(*trustedProxiesValue)
	if err != nil {
		return fmt.Errorf("invalid trusted proxies: %w", err)
	}
	// JWT 密钥解析优先级：显式 --jwt-secret > FILEBOX_JWT_SECRET > <dataDir>/config/secrets.json > 新目录首次生成。
	// JWT secret resolution priority: explicit --jwt-secret > FILEBOX_JWT_SECRET > <dataDir>/config/secrets.json > first-run generation.
	resolvedJWTSecret, secretSource, secretErr := resolveJWTSecret(*dataDir, flagWasSet(flags, "jwt-secret"), envSet("FILEBOX_JWT_SECRET"), *jwtSecret, true)
	if secretErr != nil {
		return secretErr
	}
	// 安全提示：未显式设置敏感配置时使用内置默认值，仅适合本地测试。
	// Security notice: without explicit sensitive settings, built-in defaults are used, which are only safe for local testing.
	warnings := make([]string, 0, 2)
	if resolvedJWTSecret == "filebox-development-secret-change-me" {
		warnings = append(warnings, "JWT signing secret: using the built-in development value; set --jwt-secret or FILEBOX_JWT_SECRET to a strong random value")
	}
	if !envSet("FILEBOX_ADMIN_PASS") && !flagWasSet(flags, "admin-pass") && *adminPass == "admin123" {
		warnings = append(warnings, "admin password: using the default 'admin123'; set --admin-pass or FILEBOX_ADMIN_PASS to a strong value before exposing this service")
	}
	warnProductionDefaults(warnings)

	logger, err := logging.newLogger()
	if err != nil {
		return err
	}
	defer logger.Close()
	logger.Event("startup", "operator=system ip=- version=%s addr=%s data=%s log_enabled=%t log_dir=%s log_retention_days=%d jwt_secret_source=%s", version, *addr, *dataDir, *logging.enabled, *logging.dir, *logging.retentionDays, secretSource)

	db, err := store.Open(*dataDir)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer db.Close()
	if *registerEnabled {
		// --register-enabled is a first-deployment seed; the persisted admin setting controls later restarts.
		// --register-enabled 仅用于首次部署种子，后续重启以持久化的管理设置为准。
		_, adminErr := db.GetUserByUsername(*adminUser)
		if adminErr != nil && !errors.Is(adminErr, store.ErrNotFound) {
			return fmt.Errorf("check admin account: %w", adminErr)
		}
		var storedRegisterSetting string
		settingErr := db.DB.QueryRowContext(context.Background(), "SELECT value FROM settings WHERE key = 'registerEnabled'").Scan(&storedRegisterSetting)
		if errors.Is(settingErr, sql.ErrNoRows) || storedRegisterSetting == "" || errors.Is(adminErr, store.ErrNotFound) {
			if _, err := db.DB.ExecContext(context.Background(), "INSERT INTO settings(key, value) VALUES('registerEnabled', 'true') ON CONFLICT(key) DO UPDATE SET value = excluded.value"); err != nil {
				return fmt.Errorf("seed register setting: %w", err)
			}
		}
	}

	if err := db.EnsureAdmin(*adminUser, *adminPass, 100*1024*1024*1024); err != nil {
		return fmt.Errorf("ensure admin: %w", err)
	}

	server := httpapi.NewServer(db, httpapi.Config{
		DataDir:         *dataDir,
		MaxFileSize:     *maxFileSize,
		MinFreeSpace:    *minFreeSpace,
		RegisterEnabled: *registerEnabled,
		JWTSecret:       []byte(resolvedJWTSecret),
		JWTExpiry:       7 * 24 * time.Hour,
		TrustedProxies:  trustedProxies,
		Logger:          logger,
		Static:          webassets.FS,
	})

	logger.Infof("FileBox listening on %s", *addr)
	logger.Infof("data directory: %s", *dataDir)
	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// ReadTimeout 保持为 0，避免大文件上传被服务器读超时截断。
		// ReadTimeout remains 0 so large file uploads are not cut off by the server read timeout.
		ReadTimeout: 0,
		// WriteTimeout 保持为 0，支持 SSE 推送和大文件下载。
		// WriteTimeout remains 0 to support SSE pushes and large downloads.
		WriteTimeout:   0,
		MaxHeaderBytes: 1 << 20,
	}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- httpServer.ListenAndServe() }()
	// 后台清理：每小时清理超过 24 小时未完成的废弃上传任务及其临时分片目录。
	// Background cleanup: hourly, remove abandoned upload tasks older than 24 hours and their temp chunk directories.
	cleanupContext, stopCleanup := context.WithCancel(context.Background())
	cleanupDone := make(chan struct{})
	server.StartSyncScheduler(cleanupContext)
	defer func() {
		stopCleanup()
		<-cleanupDone
	}()
	go func() {
		defer close(cleanupDone)
		if settings, settingsErr := db.GetLogSettings(cleanupContext); settingsErr == nil {
			if _, pruneErr := db.PruneAuditLogs(cleanupContext, settings.LogRetentionDays); pruneErr != nil && !errors.Is(cleanupContext.Err(), context.Canceled) {
				logger.Event("cleanup", "operator=system ip=- command=prune-audit-logs result=failure reason=%s", pruneErr.Error())
			}
		}
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if settings, settingsErr := db.GetLogSettings(cleanupContext); settingsErr == nil {
					if _, pruneErr := db.PruneSyncLogs(cleanupContext, settings.LogRetentionDays); pruneErr != nil && !errors.Is(cleanupContext.Err(), context.Canceled) {
						logger.Event("cleanup", "operator=system ip=- command=prune-sync-logs result=failure reason=%s", pruneErr.Error())
					}
					if _, pruneErr := db.PruneAuditLogs(cleanupContext, settings.LogRetentionDays); pruneErr != nil && !errors.Is(cleanupContext.Err(), context.Canceled) {
						logger.Event("cleanup", "operator=system ip=- command=prune-audit-logs result=failure reason=%s", pruneErr.Error())
					}
					if pruned, pruneErr := db.PruneShares(cleanupContext, settings.LogRetentionDays); pruneErr != nil && !errors.Is(cleanupContext.Err(), context.Canceled) {
						logger.Event("cleanup", "operator=system ip=- command=prune-shares result=failure reason=%s", pruneErr.Error())
					} else if pruned > 0 {
						logger.Event("cleanup", "operator=system ip=- command=prune-shares result=success count=%d", pruned)
					}
				}
				expired, err := db.ListExpiredUploadTasks(cleanupContext, 24*time.Hour)
				if err != nil {
					if errors.Is(cleanupContext.Err(), context.Canceled) {
						return
					}
					logger.Event("cleanup", "operator=system ip=- command=expire-uploads result=failure reason=%s", err.Error())
					continue
				}
				deletedCount := 0
				for _, taskID := range expired {
					if err := db.DeleteUploadTask(cleanupContext, taskID); err != nil {
						if !errors.Is(err, store.ErrNotFound) && !errors.Is(cleanupContext.Err(), context.Canceled) {
							logger.Event("cleanup", "operator=system ip=- command=expire-upload task=%s result=failure reason=%s", taskID, err.Error())
						}
						continue
					}
					if err := os.RemoveAll(filepath.Join(*dataDir, "tmp", taskID)); err != nil {
						logger.Event("cleanup", "operator=system ip=- command=expire-upload task=%s result=failure reason=%s", taskID, err.Error())
					}
					deletedCount++
				}
				if deletedCount > 0 {
					logger.Event("cleanup", "operator=system ip=- command=expire-uploads result=success count=%d", deletedCount)
				}
			case <-cleanupContext.Done():
				return
			}
		}
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	select {
	case signalValue := <-signals:
		logger.Event("shutdown", "operator=system ip=- signal=%s result=graceful", signalValue)
		stopCleanup()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownContext); err != nil {
			return err
		}
		logger.Event("exit", "operator=system ip=- result=success")
		return nil
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			logger.Event("exit", "operator=system ip=- result=success")
			return nil
		}
		logger.Event("exit", "operator=system ip=- result=failure reason=%s", err)
		return err
	}
}

func finishCLI(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	var failure *cliFailure
	if errors.As(err, &failure) {
		if !failure.printed {
			fmt.Fprintln(os.Stderr, failure.err)
		}
		return failure.code
	}
	fmt.Fprintln(os.Stderr, err)
	return 1
}

func runAdmin(args []string) int {
	if len(args) < 1 {
		printAdminUsage(os.Stderr)
		return 2
	}
	switch args[0] {
	case "reset-password":
		return runAdminResetPassword(args[1:])
	case "clear-ip-acl":
		return runAdminClearIPACL(args[1:])
	case "migrate-v010-paths":
		return runAdminMigrateV010Paths(args[1:])
	case "backup":
		return runAdminBackup(args[1:])
	case "restore":
		return runAdminRestore(args[1:])
	default:
		printAdminUsage(os.Stderr)
		return 2
	}
}

// runAdminMigrateV010Paths 将 v010 的 files/<uid>/<yy>/<mm> 结构迁移为 v011 的 files/<uid>/<yy>-<mm>（方案 B）。
// runAdminMigrateV010Paths migrates the v010 files/<uid>/<yy>/<mm> layout to v011 files/<uid>/<yy>-<mm> (plan B).
func runAdminMigrateV010Paths(args []string) int {
	flags := flag.NewFlagSet("filebox admin migrate-v010-paths", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	dataDir := flags.String("data", "./data", "data directory")
	logging := addLoggingFlags(flags)
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		printAdminUsage(os.Stderr)
		return 2
	}
	logger, err := logging.newLogger()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer logger.Close()
	result, reason := "failure", "command_failed"
	defer func() {
		logger.Event("ops", "operator=cli ip=- command=admin migrate-v010-paths target=all result=%s reason=%s", result, reason)
	}()
	// 迁移前备份 DB（物理文件目录建议运维自行备份或复制整个 --data）
	backupPath := filepath.Join(*dataDir, "filebox.db.bak-v011")
	if err := copyFile(filepath.Join(*dataDir, "filebox.db"), backupPath); err != nil {
		fmt.Fprintf(os.Stderr, "backup database: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stdout, "database backed up to %s\n", backupPath)

	db, err := store.Open(*dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open storage: %v\n", err)
		return 1
	}
	defer db.Close()
	ctx := context.Background()
	filesRoot := filepath.Join(*dataDir, "files")
	entries, err := os.ReadDir(filesRoot)
	if err != nil {
		if os.IsNotExist(err) {
			result, reason = "success", ""
			fmt.Fprintln(os.Stdout, "no files directory; nothing to migrate")
			return 0
		}
		fmt.Fprintf(os.Stderr, "read files root: %v\n", err)
		return 1
	}
	migrated := 0
	for _, userEntry := range entries {
		if !userEntry.IsDir() {
			continue
		}
		userID, parseErr := strconv.ParseInt(userEntry.Name(), 10, 64)
		if parseErr != nil {
			continue
		}
		userRoot := filepath.Join(filesRoot, userEntry.Name())
		yearEntries, err := os.ReadDir(userRoot)
		if err != nil {
			continue
		}
		for _, yearEntry := range yearEntries {
			if !yearEntry.IsDir() {
				continue
			}
			yearRoot := filepath.Join(userRoot, yearEntry.Name())
			monthEntries, err := os.ReadDir(yearRoot)
			if err != nil {
				continue
			}
			for _, monthEntry := range monthEntries {
				if !monthEntry.IsDir() {
					continue
				}
				oldRel := filepath.Join("files", userEntry.Name(), yearEntry.Name(), monthEntry.Name())
				newName := "20" + yearEntry.Name() + "-" + monthEntry.Name() // 方案 B：4 位年份 2026-08
				newRel := filepath.Join("files", userEntry.Name(), newName)
				oldDisk := filepath.Join(*dataDir, oldRel)
				newDisk := filepath.Join(*dataDir, newRel)
				if _, err := os.Stat(newDisk); err == nil {
					continue // 已迁移或目标已存在，跳过
				}
				if err := os.Rename(oldDisk, newDisk); err != nil {
					fmt.Fprintf(os.Stderr, "move %s -> %s: %v\n", oldDisk, newDisk, err)
					continue
				}
				prefix := oldRel + string(filepath.Separator)
				newPrefix := newRel + string(filepath.Separator)
				escaped := strings.ReplaceAll(prefix, `\`, `\\`)
				escaped = strings.ReplaceAll(escaped, "%", `\%`)
				escaped = strings.ReplaceAll(escaped, "_", `\_`)
				if _, err := db.DB.ExecContext(ctx, "UPDATE files SET storage_path = ? || substr(storage_path, length(?) + 1) WHERE user_id = ? AND storage_path LIKE ? ESCAPE '\\'", newPrefix, prefix, userID, escaped+"%"); err != nil {
					fmt.Fprintf(os.Stderr, "rewrite storage paths for %s: %v\n", oldRel, err)
					continue
				}
				if _, err := db.DB.ExecContext(ctx, "INSERT OR IGNORE INTO folders(user_id, parent_id, name, path, created_at) SELECT ?, NULL, ?, ?, ? WHERE EXISTS (SELECT 1 FROM users WHERE id = ?)", userID, newName, newName, time.Now().UTC().Format(time.RFC3339), userID); err != nil {
					fmt.Fprintf(os.Stderr, "register folder %s: %v\n", newName, err)
				}
				_ = os.Remove(yearRoot) // 清理空年份目录（仅当已空）
				migrated++
				fmt.Fprintf(os.Stdout, "migrated %s -> %s (%d files)\n", oldRel, newRel, countPrefixFiles(ctx, db, userID, newPrefix))
			}
		}
	}
	result, reason = "success", ""
	fmt.Fprintf(os.Stdout, "migration complete: %d year/month directories migrated\n", migrated)
	return 0
}

func countPrefixFiles(ctx context.Context, db *store.Store, userID int64, prefix string) int {
	var count int
	escaped := strings.ReplaceAll(prefix, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, "%", `\%`)
	escaped = strings.ReplaceAll(escaped, "_", `\_`)
	_ = db.DB.QueryRowContext(ctx, "SELECT COUNT(id) FROM files WHERE user_id = ? AND storage_path LIKE ? ESCAPE '\\'", userID, escaped+"%").Scan(&count)
	return count
}

func copyFile(source, target string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o600)
}

// secretsFilePayload 是 <dataDir>/config/secrets.json 的磁盘格式。
// secretsFilePayload is the on-disk format of <dataDir>/config/secrets.json.
type secretsFilePayload struct {
	JWTSecret string `json:"jwtSecret"`
}

func readSecretsFile(path string) (secretsFilePayload, error) {
	var payload secretsFilePayload
	data, err := os.ReadFile(path)
	if err != nil {
		return payload, err
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return payload, fmt.Errorf("parse %s: %w", path, err)
	}
	if payload.JWTSecret == "" {
		return payload, fmt.Errorf("%s is missing jwtSecret", path)
	}
	return payload, nil
}

func writeSecretsFile(path string, payload secretsFilePayload) error {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// existingJWTSecret returns the persisted key of a data directory, or "" when none exists.
// existingJWTSecret 返回数据目录中已持久化的密钥，不存在时返回空字符串。
func existingJWTSecret(dataDir string) (string, error) {
	payload, err := readSecretsFile(filepath.Join(dataDir, "config", "secrets.json"))
	if err == nil {
		return payload.JWTSecret, nil
	}
	if os.IsNotExist(err) {
		return "", nil
	}
	return "", err
}

// validateJWTSecret 拒绝空串与过短的 JWT 密钥（空串会静默退化成极弱的 HMAC key）。
// validateJWTSecret rejects empty and too-short JWT secrets (an empty string silently becomes a degenerate HMAC key).
func validateJWTSecret(secret string) error {
	if secret == "" {
		return errors.New("JWT secret must not be empty")
	}
	if len([]byte(secret)) < minJWTSecretBytes {
		return fmt.Errorf("JWT secret must be at least %d bytes (got %d)", minJWTSecretBytes, len([]byte(secret)))
	}
	return nil
}

// resolveJWTSecret 按优先级解析 JWT 签名密钥：
// 显式 --jwt-secret > FILEBOX_JWT_SECRET > <dataDir>/config/secrets.json > 首次运行生成（仅全新数据目录）。
// resolveJWTSecret resolves the JWT signing secret with this priority:
// explicit --jwt-secret > FILEBOX_JWT_SECRET > <dataDir>/config/secrets.json > first-run generation (new data dirs only).
func resolveJWTSecret(dataDir string, flagSet, envSet bool, explicitValue string, allowGenerate bool) (string, string, error) {
	if flagSet || envSet {
		if err := validateJWTSecret(explicitValue); err != nil {
			return "", "", fmt.Errorf("invalid JWT secret: %w", err)
		}
		return explicitValue, "flag-or-env", nil
	}
	secretFile := filepath.Join(dataDir, "config", "secrets.json")
	payload, readErr := readSecretsFile(secretFile)
	if readErr == nil {
		if payload.JWTSecret == "" {
			return "", "", fmt.Errorf("JWT secret in %s is empty: re-run with --jwt-secret or recreate the file", secretFile)
		}
		return payload.JWTSecret, "secrets.json", nil
	}
	if !os.IsNotExist(readErr) {
		return "", "", readErr
	}
	if !allowGenerate {
		return "", "", fmt.Errorf("no JWT secret configured for %s: provide --jwt-secret / FILEBOX_JWT_SECRET or create %s", dataDir, secretFile)
	}
	if _, statErr := os.Stat(filepath.Join(dataDir, "filebox.db")); statErr == nil {
		return "", "", fmt.Errorf("no JWT secret configured for existing data directory %s: provide --jwt-secret / FILEBOX_JWT_SECRET with the value used when this data directory was first created, or create %s", dataDir, secretFile)
	} else if !os.IsNotExist(statErr) {
		return "", "", fmt.Errorf("stat %s: %w", filepath.Join(dataDir, "filebox.db"), statErr)
	}
	randomBytes := make([]byte, 48)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", "", fmt.Errorf("generate JWT secret: %w", err)
	}
	generated := hex.EncodeToString(randomBytes)
	if err := os.MkdirAll(filepath.Dir(secretFile), 0o700); err != nil {
		return "", "", fmt.Errorf("create config directory: %w", err)
	}
	if err := writeSecretsFile(secretFile, secretsFilePayload{JWTSecret: generated}); err != nil {
		return "", "", fmt.Errorf("persist JWT secret: %w", err)
	}
	return generated, "generated", nil
}

func secretFingerprint(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// encryptSecret 用口令经 PBKDF2 派生密钥，以 AES-256-GCM 加密 jwtSecret。
// encryptSecret derives a key from the passphrase with PBKDF2 and seals jwtSecret with AES-256-GCM.
func encryptSecret(secret, passphrase string) (encoded, saltB64 string, err error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", "", err
	}
	key := pbkdf2.Key([]byte(passphrase), salt, 210000, 32, sha256.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(secret), nil)
	return base64.StdEncoding.EncodeToString(sealed), base64.StdEncoding.EncodeToString(salt), nil
}

func decryptSecret(encoded, saltB64, passphrase string) (string, error) {
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode encrypted secret: %w", err)
	}
	salt, err := base64.StdEncoding.DecodeString(saltB64)
	if err != nil {
		return "", fmt.Errorf("decode salt: %w", err)
	}
	key := pbkdf2.Key([]byte(passphrase), salt, 210000, 32, sha256.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize() {
		return "", fmt.Errorf("encrypted secret too short")
	}
	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt secret (wrong passphrase?): %w", err)
	}
	return string(plain), nil
}

// backupManifest 描述归档内容与完整性校验信息。
// backupManifest describes the archive contents and integrity checks.
type backupManifest struct {
	FormatVersion  int               `json:"formatVersion"`
	CreatedAt      string            `json:"createdAt"`
	FileCount      int               `json:"fileCount"`
	SHA256         map[string]string `json:"sha256"`
	JWTFingerprint string            `json:"jwtFingerprint"`
	KeysEncrypted  bool              `json:"keysEncrypted"`
}

// keysPayload 是归档内 keys.json 的格式；带口令备份时 jwtSecret 为 AES-GCM 密文。
// keysPayload is the keys.json format inside the archive; with a passphrase, jwtSecret holds AES-GCM ciphertext.
type keysPayload struct {
	JWTSecret   string `json:"jwtSecret"`
	Encrypted   bool   `json:"encrypted"`
	Fingerprint string `json:"fingerprint"`
	Note        string `json:"note"`
	CreatedAt   string `json:"createdAt"`
	Salt        string `json:"salt,omitempty"`
}

type archiveEntry struct {
	name     string
	diskPath string
}

// checkpointDatabase merges SQLite WAL pages into the main database before the
// backup reads any files. A busy checkpoint is retried briefly, then rejected.
// checkpointDatabase 在读取备份文件前将 SQLite WAL 合并到主库；checkpoint 忙时短暂重试，仍失败则拒绝备份。
func checkpointDatabase(dataDir string) error {
	dbPath := filepath.Join(dataDir, "filebox.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return fmt.Errorf("open database for WAL checkpoint: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	for attempt := 0; attempt < 3; attempt++ {
		var busy, logFrames, checkpointed int
		if err := db.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed); err != nil {
			return fmt.Errorf("checkpoint WAL: %w", err)
		}
		if busy == 0 {
			if err := db.Close(); err != nil {
				return fmt.Errorf("close database after WAL checkpoint: %w", err)
			}
			walPath := dbPath + "-wal"
			info, err := os.Stat(walPath)
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return fmt.Errorf("check WAL after checkpoint: %w", err)
			}
			if info.Size() > 0 {
				return fmt.Errorf("SQLite WAL remains non-empty after checkpoint (%d bytes); refusing backup", info.Size())
			}
			return nil
		}
		if attempt < 2 {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		return fmt.Errorf("SQLite WAL checkpoint is busy after retries (busy=%d log=%d checkpointed=%d)", busy, logFrames, checkpointed)
	}
	return errors.New("SQLite WAL checkpoint failed")
}

// validateRestoredDatabase checks the extracted database before it can replace
// the active data directory.
// validateRestoredDatabase 在恢复库替换活动数据目录前检查其可读且包含 schema。
func validateRestoredDatabase(dataDir string) error {
	dbPath := filepath.Join(dataDir, "filebox.db")
	info, err := os.Stat(dbPath)
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return errors.New("restored database is empty or unreadable")
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var tables int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master").Scan(&tables); err != nil {
		return err
	}
	if tables == 0 {
		return errors.New("restored database is empty or unreadable")
	}
	return nil
}

func collectDirEntries(root, prefix string) ([]archiveEntry, error) {
	var result []archiveEntry
	err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		result = append(result, archiveEntry{name: prefix + "/" + filepath.ToSlash(rel), diskPath: current})
		return nil
	})
	return result, err
}

func runAdminBackup(args []string) int {
	flags := flag.NewFlagSet("filebox admin backup", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	dataDir := flags.String("data", "./data", "data directory")
	outPath := flags.String("out", "", "output backup archive path (.tar.gz)")
	passphraseFile := flags.String("passphrase-file", "", "file containing the passphrase used to encrypt keys.json")
	jwtSecretFlag := flags.String("jwt-secret", "", "JWT signing secret override (falls back to FILEBOX_JWT_SECRET then config/secrets.json)")
	logging := addLoggingFlags(flags)
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || strings.TrimSpace(*outPath) == "" {
		printAdminUsage(os.Stderr)
		return 2
	}
	logger, err := logging.newLogger()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer logger.Close()
	result, reason := "failure", "command_failed"
	defer func() {
		logger.Event("ops", "operator=cli ip=- command=admin backup target=%s result=%s reason=%s", *dataDir, result, reason)
	}()
	fmt.Fprintln(os.Stdout, "note: backup automatically checkpoints SQLite WAL; active writes may leave a small consistency window, so production backups should still stop the FileBox service")
	explicit := *jwtSecretFlag
	if !flagWasSet(flags, "jwt-secret") {
		explicit = os.Getenv("FILEBOX_JWT_SECRET")
	}
	secret, _, err := resolveJWTSecret(*dataDir, flagWasSet(flags, "jwt-secret"), envSet("FILEBOX_JWT_SECRET"), explicit, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	passphrase := ""
	if strings.TrimSpace(*passphraseFile) != "" {
		data, readErr := os.ReadFile(*passphraseFile)
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "read passphrase file: %v\n", readErr)
			return 1
		}
		passphrase = strings.TrimSpace(string(data))
		if passphrase == "" {
			fmt.Fprintln(os.Stderr, "passphrase file is empty")
			return 1
		}
	}
	manifest, err := buildBackupArchive(*dataDir, *outPath, secret, passphrase)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backup failed: %v\n", err)
		return 1
	}
	result, reason = "success", ""
	fmt.Fprintf(os.Stdout, "backup written to %s\n", *outPath)
	fmt.Fprintf(os.Stdout, "files archived: %d\n", manifest.FileCount)
	fmt.Fprintf(os.Stdout, "jwt fingerprint: %s\n", manifest.JWTFingerprint)
	if manifest.KeysEncrypted {
		fmt.Fprintln(os.Stdout, "keys.json encrypted with AES-256-GCM (PBKDF2 from --passphrase-file)")
	} else {
		// G12：明文备份必须醒目提示——归档内含明文 JWT secret，泄露归档等于泄露全部凭据。
		// G12: plaintext backups need a prominent warning — the archive holds the raw JWT
		// secret, so losing the archive means losing every credential derived from it.
		fmt.Fprintf(os.Stderr, "\n!!! WARNING: keys.json is stored in PLAINTEXT inside the archive !!!\n")
		fmt.Fprintf(os.Stderr, "!!! The archive contains your raw JWT signing secret (jwt fingerprint %s) !!!\n", manifest.JWTFingerprint)
		fmt.Fprintf(os.Stderr, "!!! Anyone with this archive can forge sessions and decrypt sync credentials. !!!\n")
		fmt.Fprintf(os.Stderr, "!!! PRODUCTION MUST use --passphrase-file to encrypt keys.json and keep the archive on a secure channel. !!!\n")
		fmt.Fprintf(os.Stderr, "!!! The manifest marks this archive as unencrypted (keysEncrypted=false). !!!\n\n")
	}
	return 0
}

func buildBackupArchive(dataDir, outPath, secret, passphrase string) (backupManifest, error) {
	if err := checkpointDatabase(dataDir); err != nil {
		return backupManifest{}, err
	}
	manifest := backupManifest{
		FormatVersion:  1,
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
		SHA256:         make(map[string]string),
		JWTFingerprint: secretFingerprint(secret),
	}
	keys := keysPayload{
		JWTSecret:   secret,
		Fingerprint: manifest.JWTFingerprint,
		Note:        "TOTP 与同步系统凭据由 SHA-256(jwtSecret) 派生 AES-GCM 密钥加密；恢复时必须使用与备份一致的 jwtSecret",
		CreatedAt:   manifest.CreatedAt,
	}
	if passphrase != "" {
		encrypted, salt, err := encryptSecret(secret, passphrase)
		if err != nil {
			return manifest, fmt.Errorf("encrypt keys.json: %w", err)
		}
		keys.JWTSecret = encrypted
		keys.Encrypted = true
		keys.Salt = salt
		manifest.KeysEncrypted = true
	}
	keysJSON, err := json.MarshalIndent(keys, "", "  ")
	if err != nil {
		return manifest, err
	}
	entries := []archiveEntry{{name: "filebox.db", diskPath: filepath.Join(dataDir, "filebox.db")}}
	for _, dir := range []string{"files", "brand"} {
		root := filepath.Join(dataDir, dir)
		if _, err := os.Stat(root); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return manifest, err
		}
		collected, err := collectDirEntries(root, dir)
		if err != nil {
			return manifest, err
		}
		entries = append(entries, collected...)
	}
	type stagedFile struct {
		name    string
		content []byte
	}
	staged := make([]stagedFile, 0, len(entries)+1)
	for _, entry := range entries {
		content, err := os.ReadFile(entry.diskPath)
		if err != nil {
			return manifest, fmt.Errorf("read %s: %w", entry.diskPath, err)
		}
		manifest.SHA256[entry.name] = sha256Hex(content)
		staged = append(staged, stagedFile{name: entry.name, content: content})
	}
	manifest.SHA256["keys.json"] = sha256Hex(keysJSON)
	staged = append(staged, stagedFile{name: "keys.json", content: keysJSON})
	manifest.FileCount = len(staged)
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return manifest, err
	}
	// 外层归档使用随机临时名 + O_EXCL：固定 .tmp 名可能被符号链接劫持，O_EXCL 保证
	// 目标不存在；权限 0600，防止其他本地用户读取含密钥的备份文件。
	// The outer archive uses a random temp name with O_EXCL (a fixed .tmp name could be
	// symlinked) and 0600 permissions so other local users cannot read the key-bearing backup.
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return manifest, err
	}
	tmp := outPath + ".tmp-" + hex.EncodeToString(suffix)
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return manifest, err
	}
	ok := false
	defer func() {
		file.Close()
		if !ok {
			os.Remove(tmp)
		}
	}()
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	writeStaged := func(name string, content []byte) error {
		header := &tar.Header{Name: name, Mode: 0o600, Size: int64(len(content)), ModTime: time.Now()}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		_, err := tw.Write(content)
		return err
	}
	for _, item := range staged {
		if err := writeStaged(item.name, item.content); err != nil {
			return manifest, err
		}
	}
	if err := writeStaged("manifest.json", manifestJSON); err != nil {
		return manifest, err
	}
	if err := tw.Close(); err != nil {
		return manifest, err
	}
	if err := gz.Close(); err != nil {
		return manifest, err
	}
	if err := file.Close(); err != nil {
		return manifest, err
	}
	if err := os.Rename(tmp, outPath); err != nil {
		return manifest, err
	}
	ok = true
	return manifest, nil
}

// safeArchiveName 拒绝绝对路径与 .. 穿越路径，防止恶意归档写越界。
// safeArchiveName rejects absolute paths and .. traversal to keep extraction inside staging.
func safeArchiveName(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.ContainsAny(name, `\:`) {
		return false
	}
	cleaned := path.Clean(name)
	if cleaned != name || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return false
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

func runAdminRestore(args []string) int {
	flags := flag.NewFlagSet("filebox admin restore", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	dataDir := flags.String("data", "./data", "data directory")
	inPath := flags.String("in", "", "backup archive path (.tar.gz)")
	force := flags.Bool("force", false, "allow replacing a non-empty data directory / adopting a conflicting key")
	yes := flags.Bool("yes", false, "confirm destructive restore (required together with --force)")
	passphraseFile := flags.String("passphrase-file", "", "file containing the passphrase for an encrypted keys.json")
	logging := addLoggingFlags(flags)
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || strings.TrimSpace(*inPath) == "" {
		printAdminUsage(os.Stderr)
		return 2
	}
	logger, err := logging.newLogger()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer logger.Close()
	result, reason := "failure", "command_failed"
	defer func() {
		logger.Event("ops", "operator=cli ip=- command=admin restore target=%s result=%s reason=%s", *dataDir, result, reason)
	}()
	fmt.Fprintln(os.Stdout, "note: stop the FileBox service before restore; the target data directory will be replaced")
	forceMode := *force && *yes
	archive, err := os.Open(*inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open archive: %v\n", err)
		return 1
	}
	defer archive.Close()
	target := filepath.Clean(*dataDir)
	timestamp := time.Now().UTC().Format("20060102T150405")
	stagingSuffix := make([]byte, 8)
	if _, err := rand.Read(stagingSuffix); err != nil {
		fmt.Fprintf(os.Stderr, "generate staging suffix: %v\n", err)
		return 1
	}
	// staging 使用随机名，避免可预测的固定名被符号链接劫持。
	// The staging directory uses a random name so a predictable fixed name cannot be symlinked.
	staging := target + ".staging-" + timestamp + "-" + hex.EncodeToString(stagingSuffix)
	if err := os.MkdirAll(staging, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "create staging directory: %v\n", err)
		return 1
	}
	defer os.RemoveAll(staging)
	gz, err := gzip.NewReader(archive)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open gzip stream: %v\n", err)
		return 1
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	extracted := map[string]bool{}
	var extractedCount, extractedBytes int64
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "read archive: %v\n", err)
			return 1
		}
		name := header.Name
		if !safeArchiveName(name) {
			fmt.Fprintf(os.Stderr, "archive contains unsafe path %q\n", name)
			return 1
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg {
			fmt.Fprintf(os.Stderr, "archive entry %q has unsupported type\n", name)
			return 1
		}
		if extracted[name] {
			fmt.Fprintf(os.Stderr, "archive contains duplicate entry %q\n", name)
			return 1
		}
		if extractedCount >= restoreMaxEntries {
			fmt.Fprintf(os.Stderr, "archive exceeds the entry limit (%d)\n", restoreMaxEntries)
			return 1
		}
		if header.Size < 0 || header.Size > restoreMaxSingleBytes {
			fmt.Fprintf(os.Stderr, "archive entry %q is too large (%d bytes)\n", name, header.Size)
			return 1
		}
		if extractedBytes+header.Size > restoreMaxTotalBytes {
			fmt.Fprintf(os.Stderr, "archive exceeds the total size limit (%d bytes)\n", restoreMaxTotalBytes)
			return 1
		}
		dest := filepath.Join(staging, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "create directory for %q: %v\n", name, err)
			return 1
		}
		// O_EXCL：目标必须不存在，防止归档条目覆盖既有 staging 文件或符号链接。
		// O_EXCL requires a fresh target so an entry cannot overwrite an existing file or symlink.
		out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			fmt.Fprintf(os.Stderr, "extract %q: %v\n", name, err)
			return 1
		}
		written, copyErr := io.Copy(out, tr)
		if copyErr != nil {
			out.Close()
			fmt.Fprintf(os.Stderr, "extract %q: %v\n", name, copyErr)
			return 1
		}
		if closeErr := out.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "extract %q: %v\n", name, closeErr)
			return 1
		}
		if written != header.Size {
			fmt.Fprintf(os.Stderr, "extract %q: size mismatch (got %d, want %d)\n", name, written, header.Size)
			return 1
		}
		extracted[name] = true
		extractedCount++
		extractedBytes += header.Size
	}
	manifestData, err := os.ReadFile(filepath.Join(staging, "manifest.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "archive has no valid manifest.json: %v\n", err)
		return 1
	}
	var manifest backupManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		fmt.Fprintf(os.Stderr, "parse manifest.json: %v\n", err)
		return 1
	}
	if manifest.FormatVersion != 1 {
		fmt.Fprintf(os.Stderr, "unsupported archive format version %d\n", manifest.FormatVersion)
		return 1
	}
	for name := range extracted {
		if name == "manifest.json" {
			continue
		}
		want, ok := manifest.SHA256[name]
		if !ok {
			fmt.Fprintf(os.Stderr, "archive file %q is missing from manifest\n", name)
			return 1
		}
		content, err := os.ReadFile(filepath.Join(staging, filepath.FromSlash(name)))
		if err != nil {
			fmt.Fprintf(os.Stderr, "read extracted %q: %v\n", name, err)
			return 1
		}
		if sha256Hex(content) != want {
			fmt.Fprintf(os.Stderr, "checksum mismatch for %q\n", name)
			return 1
		}
	}
	for name := range manifest.SHA256 {
		if !extracted[name] {
			fmt.Fprintf(os.Stderr, "manifest file %q is missing from archive\n", name)
			return 1
		}
	}
	if err := validateRestoredDatabase(staging); err != nil {
		fmt.Fprintf(os.Stderr, "restored database is empty or unreadable: %v\n", err)
		return 1
	}
	keysData, err := os.ReadFile(filepath.Join(staging, "keys.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "archive has no keys.json: %v\n", err)
		return 1
	}
	var keys keysPayload
	if err := json.Unmarshal(keysData, &keys); err != nil {
		fmt.Fprintf(os.Stderr, "parse keys.json: %v\n", err)
		return 1
	}
	if keys.Fingerprint != manifest.JWTFingerprint {
		fmt.Fprintln(os.Stderr, "keys.json fingerprint does not match manifest")
		return 1
	}
	archiveKey := keys.JWTSecret
	if keys.Encrypted {
		passphrase := ""
		if strings.TrimSpace(*passphraseFile) != "" {
			data, readErr := os.ReadFile(*passphraseFile)
			if readErr != nil {
				fmt.Fprintf(os.Stderr, "read passphrase file: %v\n", readErr)
				return 1
			}
			passphrase = strings.TrimSpace(string(data))
		}
		if passphrase == "" {
			fmt.Fprintln(os.Stderr, "keys.json is encrypted; provide --passphrase-file")
			return 1
		}
		archiveKey, err = decryptSecret(keys.JWTSecret, keys.Salt, passphrase)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
	}
	if secretFingerprint(archiveKey) != manifest.JWTFingerprint {
		fmt.Fprintln(os.Stderr, "decrypted key fingerprint mismatch")
		return 1
	}
	existingKey, err := existingJWTSecret(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read existing secret: %v\n", err)
		return 1
	}
	keyConflict := existingKey != "" && secretFingerprint(existingKey) != manifest.JWTFingerprint
	if keyConflict && !forceMode {
		fmt.Fprintf(os.Stderr, "KEY_CONFLICT: existing data directory uses a different JWT secret (fingerprint %s); pass --force --yes to adopt the archive key\n", secretFingerprint(existingKey))
		return 1
	}
	_, statErr := os.Stat(target)
	targetExists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		fmt.Fprintf(os.Stderr, "stat target: %v\n", statErr)
		return 1
	}
	if targetExists {
		entries, err := os.ReadDir(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read target data directory: %v\n", err)
			return 1
		}
		if len(entries) > 0 && !forceMode {
			fmt.Fprintf(os.Stderr, "target data directory %s is not empty; pass --force --yes to replace it (the old directory is kept as *.pre-restore-<timestamp>)\n", target)
			return 1
		}
	}
	if targetExists {
		entries, err := os.ReadDir(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read target data directory: %v\n", err)
			return 1
		}
		if len(entries) > 0 {
			backupTarget := target + ".pre-restore-" + timestamp
			if err := os.Rename(target, backupTarget); err != nil {
				fmt.Fprintf(os.Stderr, "rename existing data directory: %v\n", err)
				return 1
			}
			fmt.Fprintf(os.Stdout, "previous data directory preserved at %s\n", backupTarget)
		} else if err := os.Remove(target); err != nil {
			fmt.Fprintf(os.Stderr, "remove empty target data directory: %v\n", err)
			return 1
		}
	}
	if err := os.Rename(staging, target); err != nil {
		fmt.Fprintf(os.Stderr, "activate restored data directory: %v\n", err)
		return 1
	}
	secretSource := "matched-existing"
	if existingKey == "" {
		secretSource = "archive-adopted"
	} else if keyConflict {
		secretSource = "archive-adopted-conflict"
	}
	secretFile := filepath.Join(target, "config", "secrets.json")
	if err := os.MkdirAll(filepath.Dir(secretFile), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "create config directory: %v\n", err)
		return 1
	}
	if err := writeSecretsFile(secretFile, secretsFilePayload{JWTSecret: archiveKey}); err != nil {
		fmt.Fprintf(os.Stderr, "write secrets.json: %v\n", err)
		return 1
	}
	result, reason = "success", ""
	fmt.Fprintf(os.Stdout, "restore complete: %d files, jwt fingerprint %s, secret source %s\n", manifest.FileCount, manifest.JWTFingerprint, secretSource)
	return 0
}

func runAdminResetPassword(args []string) int {
	flags := flag.NewFlagSet("filebox admin reset-password", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	dataDir := flags.String("data", "./data", "data directory")
	username := flags.String("username", "admin", "username")
	newPassword := flags.String("new-password", "", "new password")
	generate := flags.Bool("generate", false, "generate a one-time password")
	logging := addLoggingFlags(flags)
	// runAdminResetPassword 解析剩余 flags（args 已剔除子命令名，不能再次去掉首项）。
	// runAdminResetPassword parses the remaining flags; args already excludes the subcommand name.
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || (*generate && *newPassword != "") || (!*generate && *newPassword == "") {
		printAdminUsage(os.Stderr)
		return 2
	}
	logger, err := logging.newLogger()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer logger.Close()
	mode := "explicit"
	if *generate {
		mode = "generated"
	}
	result, reason := "failure", "command_failed"
	defer func() {
		logger.Event("ops", "operator=cli ip=- command=admin reset-password target=%s mode=%s result=%s reason=%s", *username, mode, result, reason)
	}()
	password := *newPassword
	if *generate {
		password, err = generatePassword()
		if err != nil {
			fmt.Fprintf(os.Stderr, "generate password: %v\n", err)
			return 1
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hash password: %v\n", err)
		return 1
	}
	db, err := store.Open(*dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open storage: %v\n", err)
		return 1
	}
	defer db.Close()
	updated, err := db.ResetPassword(*username, string(hash))
	if errors.Is(err, store.ErrNotFound) {
		fmt.Fprintf(os.Stderr, "user not found: %s\n", *username)
		return 1
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "reset password: %v\n", err)
		return 1
	}
	result, reason = "success", ""
	fmt.Fprintf(os.Stdout, "password reset for user %s (%d record updated); login will require a password change\n", *username, updated)
	if *generate {
		fmt.Fprintf(os.Stdout, "one-time password: %s\n", password)
	}
	return 0
}

func runAdminClearIPACL(args []string) int {
	flags := flag.NewFlagSet("filebox admin clear-ip-acl", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	dataDir := flags.String("data", "./data", "data directory")
	username := flags.String("username", "", "username")
	logging := addLoggingFlags(flags)
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || strings.TrimSpace(*username) == "" {
		printAdminUsage(os.Stderr)
		return 2
	}
	logger, err := logging.newLogger()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer logger.Close()
	result, reason := "failure", "command_failed"
	defer func() {
		logger.Event("ops", "operator=cli ip=- command=admin clear-ip-acl target=%s result=%s reason=%s", *username, result, reason)
	}()
	db, err := store.Open(*dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open storage: %v\n", err)
		return 1
	}
	defer db.Close()
	cleared, err := db.ClearIPACL(strings.TrimSpace(*username))
	if err != nil {
		fmt.Fprintf(os.Stderr, "clear IP ACL: %v\n", err)
		return 1
	}
	if !cleared {
		fmt.Fprintf(os.Stderr, "user not found: %s\n", *username)
		return 1
	}
	result, reason = "success", ""
	fmt.Fprintf(os.Stdout, "cleared IP ACL for user %s\n", *username)
	return 0
}

func runLocks(args []string) int {
	if len(args) < 1 {
		printLocksUsage(os.Stderr)
		return 2
	}
	switch args[0] {
	case "list":
		return runLocksList(args[1:])
	case "clear":
		return runLocksClear(args[1:])
	default:
		printLocksUsage(os.Stderr)
		return 2
	}
}

func runLocksList(args []string) int {
	flags := flag.NewFlagSet("filebox locks list", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	dataDir := flags.String("data", "./data", "data directory")
	logging := addLoggingFlags(flags)
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		printLocksUsage(os.Stderr)
		return 2
	}
	logger, err := logging.newLogger()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer logger.Close()
	result, reason := "failure", "command_failed"
	defer func() {
		logger.Event("ops", "operator=cli ip=- command=locks list target=all result=%s reason=%s", result, reason)
	}()
	db, err := store.Open(*dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open storage: %v\n", err)
		return 1
	}
	defer db.Close()
	ipLocks, userLocks, err := db.ListLocks(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "list locks: %v\n", err)
		return 1
	}
	if len(ipLocks) == 0 && len(userLocks) == 0 {
		result, reason = "success", ""
		fmt.Fprintln(os.Stdout, "无锁定信息")
		return 0
	}
	if len(ipLocks) > 0 {
		fmt.Fprintln(os.Stdout, "IP locks")
		fmt.Fprintln(os.Stdout, "ip\tfailed_count\twindow_started_at\tlocked_until\tstatus")
		for _, lock := range ipLocks {
			fmt.Fprintf(os.Stdout, "%s\t%d\t%s\t%s\t%s\n", lock.IP, lock.FailedCount, lock.WindowStartedAt, displayValue(lock.LockedUntil), lockStatus(lock.LockedNow))
		}
	}
	if len(userLocks) > 0 {
		fmt.Fprintln(os.Stdout, "User locks")
		fmt.Fprintln(os.Stdout, "id\tusername\tfailed_attempts\tlocked_until\tstatus")
		for _, lock := range userLocks {
			fmt.Fprintf(os.Stdout, "%d\t%s\t%d\t%s\t%s\n", lock.ID, lock.Username, lock.FailedAttempts, displayValue(lock.LockedUntil), lockStatus(lock.LockedNow))
		}
	}
	result, reason = "success", ""
	return 0
}

func runLocksClear(args []string) int {
	flags := flag.NewFlagSet("filebox locks clear", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	dataDir := flags.String("data", "./data", "data directory")
	ip := flags.String("ip", "", "source IP")
	userID := flags.Int64("user", 0, "user ID")
	flags.Bool("all", false, "clear all locks")
	logging := addLoggingFlags(flags)
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		printLocksUsage(os.Stderr)
		return 2
	}
	hasIP, hasUser, hasAll := false, false, false
	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "ip":
			hasIP = true
		case "user":
			hasUser = true
		case "all":
			hasAll = true
		}
	})
	if (boolInt(hasIP) + boolInt(hasUser) + boolInt(hasAll)) != 1 {
		printLocksUsage(os.Stderr)
		return 2
	}
	logger, err := logging.newLogger()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer logger.Close()
	target := "all"
	if hasIP {
		target = *ip
	} else if hasUser {
		target = strconv.FormatInt(*userID, 10)
	}
	result, reason := "failure", "command_failed"
	defer func() {
		logger.Event("ops", "operator=cli ip=- command=locks clear target=%s result=%s reason=%s", target, result, reason)
	}()
	db, err := store.Open(*dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open storage: %v\n", err)
		return 1
	}
	defer db.Close()
	ctx := context.Background()
	if hasIP {
		cleared, err := db.ClearIPLock(ctx, *ip)
		if err != nil {
			fmt.Fprintf(os.Stderr, "clear IP lock: %v\n", err)
			return 1
		}
		if !cleared {
			fmt.Fprintf(os.Stderr, "IP lock not found: %s\n", *ip)
			return 1
		}
		fmt.Fprintf(os.Stdout, "cleared IP lock: %s\n", *ip)
		result, reason = "success", ""
		return 0
	}
	if hasUser {
		cleared, err := db.ClearUserLock(ctx, *userID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "clear user lock: %v\n", err)
			return 1
		}
		if !cleared {
			fmt.Fprintf(os.Stderr, "user lock not found: %d\n", *userID)
			return 1
		}
		fmt.Fprintf(os.Stdout, "cleared user lock: %d\n", *userID)
		result, reason = "success", ""
		return 0
	}
	count, err := db.ClearAllLocks(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "clear all locks: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stdout, "cleared locks: %d\n", count)
	result, reason = "success", ""
	return 0
}

func generatePassword() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%^&*"
	classes := []string{"ABCDEFGHIJKLMNOPQRSTUVWXYZ", "abcdefghijklmnopqrstuvwxyz", "0123456789", "!@#$%^&*"}
	result := make([]byte, 0, 16)
	for _, class := range classes {
		character, err := randomCharacter(class)
		if err != nil {
			return "", err
		}
		result = append(result, character)
	}
	for len(result) < 16 {
		character, err := randomCharacter(alphabet)
		if err != nil {
			return "", err
		}
		result = append(result, character)
	}
	for i := len(result) - 1; i > 0; i-- {
		value, err := rand.Int(rand.Reader, bigInt(int64(i+1)))
		if err != nil {
			return "", err
		}
		result[i], result[value.Int64()] = result[value.Int64()], result[i]
	}
	return string(result), nil
}

func randomCharacter(alphabet string) (byte, error) {
	value, err := rand.Int(rand.Reader, bigInt(int64(len(alphabet))))
	if err != nil {
		return 0, err
	}
	return alphabet[value.Int64()], nil
}

func bigInt(value int64) *big.Int { return big.NewInt(value) }

func displayValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func lockStatus(locked bool) string {
	if locked {
		return "锁定中"
	}
	return "未锁定"
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: filebox [serve] [web flags]")
	fmt.Fprintln(writer, "       filebox admin reset-password [flags]")
	fmt.Fprintln(writer, "       filebox admin clear-ip-acl [flags]")
	fmt.Fprintln(writer, "       filebox locks list [flags]")
	fmt.Fprintln(writer, "       filebox locks clear [flags]")
}

func printAdminUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: filebox admin reset-password --data=./data --username=admin --new-password=PASSWORD")
	fmt.Fprintln(writer, "       filebox admin reset-password --data=./data --username=admin --generate")
	fmt.Fprintln(writer, "       filebox admin clear-ip-acl --data=./data --username=admin")
	fmt.Fprintln(writer, "       filebox admin migrate-v010-paths --data=./data   # v010 yy/mm → v011 yy-mm")
	fmt.Fprintln(writer, "       filebox admin backup --data=./data --out=backup.tar.gz [--passphrase-file=FILE]")
	fmt.Fprintln(writer, "       filebox admin restore --data=./data --in=backup.tar.gz [--passphrase-file=FILE] [--force --yes]")
}

func printLocksUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: filebox locks list --data=./data")
	fmt.Fprintln(writer, "       filebox locks clear --data=./data --ip=IP | --user=ID | --all")
}

func parseTrustedProxies(value string) ([]*net.IPNet, error) {
	proxies := make([]*net.IPNet, 0)
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if ip := net.ParseIP(item); ip != nil {
			bits := 128
			if ip.To4() != nil {
				bits = 32
				ip = ip.To4()
			}
			proxies = append(proxies, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		_, network, err := net.ParseCIDR(item)
		if err != nil {
			return nil, err
		}
		proxies = append(proxies, network)
	}
	return proxies, nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// warnProductionDefaults 输出生产环境默认值警告，并在终端中使用醒目的红色显示。
// warnProductionDefaults prints production-default warnings, using prominent red output in terminals.
func warnProductionDefaults(warnings []string) {
	if len(warnings) == 0 {
		return
	}
	lines := append([]string{
		"WARNING: using insecure default secrets in production!",
		"----------------------------------------------------",
	}, warnings...)
	stderrInfo, err := os.Stderr.Stat()
	terminal := err == nil && stderrInfo.Mode()&os.ModeCharDevice != 0
	if terminal {
		const boldRed = "\x1b[1;31m"
		const reset = "\x1b[0m"
		for index, line := range lines {
			if index > 0 {
				fmt.Fprint(os.Stderr, "\n")
			}
			fmt.Fprint(os.Stderr, boldRed, line)
		}
		fmt.Fprintln(os.Stderr, reset)
		return
	}
	for _, line := range lines {
		fmt.Fprintln(os.Stderr, line)
	}
}

// envSet 报告环境变量是否已设置（非空）。
// envSet reports whether an environment variable is set to a non-empty value.
func envSet(key string) bool { return os.Getenv(key) != "" }

// flagWasSet 报告 flag 是否在命令行显式提供。
// flagWasSet reports whether a flag was explicitly provided on the command line.
func flagWasSet(flags *flag.FlagSet, name string) bool {
	found := false
	flags.Visit(func(flag *flag.Flag) {
		if flag.Name == name {
			found = true
		}
	})
	return found
}

func defaultLogDir() string {
	executable, err := os.Executable()
	if err != nil {
		return filepath.Join(".", "logs")
	}
	return filepath.Join(filepath.Dir(executable), "logs")
}

func envInt64(key string, fallback int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envNonNegativeInt64(key string, fallback int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func envBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
