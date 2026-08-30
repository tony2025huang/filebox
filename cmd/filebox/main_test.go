package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filebox/internal/store"
)

func TestGeneratePasswordMeetsRequiredClasses(t *testing.T) {
	password, err := generatePassword()
	if err != nil {
		t.Fatal(err)
	}
	if len(password) != 16 {
		t.Fatalf("generated password length = %d, want 16", len(password))
	}
	for _, class := range []string{"ABCDEFGHIJKLMNOPQRSTUVWXYZ", "abcdefghijklmnopqrstuvwxyz", "0123456789", "!@#$%^&*"} {
		if !strings.ContainsAny(password, class) {
			t.Fatalf("generated password %q does not contain a character from %q", password, class)
		}
	}
}

func TestUnknownSubcommandReturnsUsageError(t *testing.T) {
	if code := run([]string{"unknown-command"}); code != 2 {
		t.Fatalf("run(unknown command) = %d, want 2", code)
	}
}

func TestClearIPACLCommand(t *testing.T) {
	dataDir := t.TempDir()
	db, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.UpdateIPACL(context.Background(), 1, true, "192.0.2.1"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if code := run([]string{"admin", "clear-ip-acl", "--data", dataDir, "--username", "admin"}); code != 0 {
		t.Fatalf("clear-ip-acl exit code = %d, want 0", code)
	}
	db, err = store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	user, err := db.GetUserByUsername("admin")
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if user.IPACLEnabled || user.IPWhitelist != "" {
		db.Close()
		t.Fatalf("clear-ip-acl did not clear ACL: enabled=%t whitelist=%q", user.IPACLEnabled, user.IPWhitelist)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"admin", "clear-ip-acl", "--data", dataDir, "--username", "missing"}); code != 1 {
		t.Fatalf("missing clear-ip-acl exit code = %d, want 1", code)
	}
	if code := run([]string{"admin", "clear-ip-acl", "--data", dataDir}); code != 2 {
		t.Fatalf("invalid clear-ip-acl exit code = %d, want 2", code)
	}
}

// TestResetPasswordParsesDataFlag 验证 --data 在子命令名之后仍被解析（回归 D-FIX）。
// TestResetPasswordParsesDataFlag verifies --data is parsed after the subcommand name (regression D-FIX).
func TestResetPasswordParsesDataFlag(t *testing.T) {
	dataDir := t.TempDir()
	db, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"admin", "reset-password", "--data", dataDir, "--username", "admin", "--new-password", "NewPass123!"}); code != 0 {
		t.Fatalf("reset-password exit code = %d, want 0", code)
	}
	db, err = store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	user, err := db.GetUserByUsername("admin")
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if !user.MustChangePassword {
		db.Close()
		t.Fatal("reset password did not set must_change_password")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestBackupRestoreRoundTrip 验证 admin backup → restore 端到端流程与密钥持久化。
// TestBackupRestoreRoundTrip verifies the admin backup → restore flow end to end with key persistence.
func TestBackupRestoreRoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	db, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// 模拟 serve 首次运行生成的 secrets.json，供 backup 读取。
	secret := "test-backup-secret-0123456789abcdef"
	if err := os.MkdirAll(filepath.Join(dataDir, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeSecretsFile(filepath.Join(dataDir, "config", "secrets.json"), secretsFilePayload{JWTSecret: secret}); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "backup.tar.gz")
	if code := run([]string{"admin", "backup", "--data", dataDir, "--out", out}); code != 0 {
		t.Fatalf("admin backup exit code = %d, want 0", code)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("backup archive missing: %v", err)
	}
	restoreDir := t.TempDir()
	if code := run([]string{"admin", "restore", "--data", restoreDir, "--in", out}); code != 0 {
		t.Fatalf("admin restore exit code = %d, want 0", code)
	}
	payload, err := readSecretsFile(filepath.Join(restoreDir, "config", "secrets.json"))
	if err != nil {
		t.Fatal(err)
	}
	if payload.JWTSecret != secret || secretFingerprint(payload.JWTSecret) != secretFingerprint(secret) {
		t.Fatalf("restored secret mismatch: %q", payload.JWTSecret)
	}
	restored, err := store.Open(restoreDir)
	if err != nil {
		t.Fatal(err)
	}
	user, err := restored.GetUserByUsername("admin")
	if err != nil || user.Username != "admin" {
		restored.Close()
		t.Fatalf("restored admin = %+v, %v", user, err)
	}
	if err := restored.Close(); err != nil {
		t.Fatal(err)
	}
	// 非空目标未加 --force --yes 时拒绝恢复
	if code := run([]string{"admin", "restore", "--data", restoreDir, "--in", out}); code == 0 {
		t.Fatal("restore into non-empty target without --force --yes should fail")
	}
	// --force --yes 允许替换并保留旧目录
	if code := run([]string{"admin", "restore", "--data", restoreDir, "--in", out, "--force", "--yes"}); code != 0 {
		t.Fatalf("forced restore exit code = %d, want 0", code)
	}
	if _, err := readSecretsFile(filepath.Join(restoreDir, "config", "secrets.json")); err != nil {
		t.Fatalf("forced restore did not write secrets.json: %v", err)
	}
	if matches, _ := filepath.Glob(restoreDir + ".pre-restore-*"); len(matches) != 1 {
		t.Fatalf("pre-restore backup dirs = %v, want 1", matches)
	}
}

// TestBackupRequiresJWTSecret 验证缺失密钥时 backup 报错而不是回退开发密钥。
// TestBackupRequiresJWTSecret verifies backup fails when no JWT secret is available instead of falling back.
func TestBackupRequiresJWTSecret(t *testing.T) {
	dataDir := t.TempDir()
	db, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "backup.tar.gz")
	if code := run([]string{"admin", "backup", "--data", dataDir, "--out", out}); code != 1 {
		t.Fatalf("backup without secret exit code = %d, want 1", code)
	}
	if code := run([]string{"admin", "backup", "--data", dataDir, "--out", out, "--jwt-secret", "explicit-secret"}); code != 0 {
		t.Fatalf("backup with --jwt-secret exit code = %d, want 0", code)
	}
}
