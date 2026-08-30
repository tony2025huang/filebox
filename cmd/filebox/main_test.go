package main

import (
	"context"
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
