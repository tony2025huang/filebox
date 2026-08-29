package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
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
)

const version = "dev"

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

	logger, err := logging.newLogger()
	if err != nil {
		return err
	}
	defer logger.Close()
	logger.Event("startup", "operator=system ip=- version=%s addr=%s data=%s log_enabled=%t log_dir=%s log_retention_days=%d", version, *addr, *dataDir, *logging.enabled, *logging.dir, *logging.retentionDays)

	db, err := store.Open(*dataDir)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer db.Close()

	if err := db.EnsureAdmin(*adminUser, *adminPass, 100*1024*1024*1024); err != nil {
		return fmt.Errorf("ensure admin: %w", err)
	}

	server := httpapi.NewServer(db, httpapi.Config{
		DataDir:         *dataDir,
		MaxFileSize:     *maxFileSize,
		MinFreeSpace:    *minFreeSpace,
		RegisterEnabled: *registerEnabled,
		JWTSecret:       []byte(*jwtSecret),
		JWTExpiry:       7 * 24 * time.Hour,
		TrustedProxies:  trustedProxies,
		Logger:          logger,
		Static:          webassets.FS,
	})

	logger.Infof("FileBox listening on %s", *addr)
	logger.Infof("data directory: %s", *dataDir)
	httpServer := &http.Server{Addr: *addr, Handler: server.Handler()}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- httpServer.ListenAndServe() }()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	select {
	case signalValue := <-signals:
		logger.Event("shutdown", "operator=system ip=- signal=%s result=graceful", signalValue)
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
	default:
		printAdminUsage(os.Stderr)
		return 2
	}
}

func runAdminResetPassword(args []string) int {
	flags := flag.NewFlagSet("filebox admin reset-password", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	dataDir := flags.String("data", "./data", "data directory")
	username := flags.String("username", "admin", "username")
	newPassword := flags.String("new-password", "", "new password")
	generate := flags.Bool("generate", false, "generate a one-time password")
	logging := addLoggingFlags(flags)
	if err := flags.Parse(args[1:]); err != nil {
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
