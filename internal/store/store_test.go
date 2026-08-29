package store

import (
	"context"
	"testing"
)

func TestOperationsResetPasswordAndClearLocks(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		t.Fatal(err)
	}
	user, err := db.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE users SET failed_attempts = 3, locked_until = '9999-12-31T23:59:59Z', must_change_password = 0 WHERE id = ?", user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("INSERT INTO ip_failures(ip, failed_count, window_started_at, locked_until) VALUES('192.0.2.1', 50, '2026-08-29T00:00:00Z', '9999-12-31T23:59:59Z')"); err != nil {
		t.Fatal(err)
	}

	updated, err := db.ResetPassword("admin", "new-hash")
	if err != nil || updated != 1 {
		t.Fatalf("ResetPassword() = %d, %v; want 1, nil", updated, err)
	}
	user, err = db.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	if user.PasswordHash != "new-hash" || !user.MustChangePassword || user.FailedAttempts != 0 || user.LockedUntil != "" {
		t.Fatalf("password reset did not clear user lock state: %+v", user)
	}

	ipLocks, userLocks, err := db.ListLocks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ipLocks) != 1 || len(userLocks) != 0 || !ipLocks[0].LockedNow {
		t.Fatalf("ListLocks() = %d IP locks, %d user locks, %+v", len(ipLocks), len(userLocks), ipLocks)
	}
	cleared, err := db.ClearIPLock(context.Background(), "192.0.2.1")
	if err != nil || !cleared {
		t.Fatalf("ClearIPLock() = %v, %v; want true, nil", cleared, err)
	}
	if _, err := db.DB.Exec("UPDATE users SET failed_attempts = 2, locked_until = '9999-12-31T23:59:59Z' WHERE id = ?", user.ID); err != nil {
		t.Fatal(err)
	}
	cleared, err = db.ClearUserLock(context.Background(), user.ID)
	if err != nil || !cleared {
		t.Fatalf("ClearUserLock() = %v, %v; want true, nil", cleared, err)
	}
	cleared, err = db.ClearUserLock(context.Background(), 99999)
	if err != nil || cleared {
		t.Fatalf("ClearUserLock(missing) = %v, %v; want false, nil", cleared, err)
	}
	cleared, err = db.ClearUserLock(context.Background(), user.ID)
	if err != nil || cleared {
		t.Fatalf("ClearUserLock(unlocked user) = %v, %v; want false, nil", cleared, err)
	}
}

func TestClearAllLocks(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		t.Fatal(err)
	}
	user, err := db.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE users SET failed_attempts = 1 WHERE id = ?", user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("INSERT INTO ip_failures(ip, failed_count, window_started_at) VALUES('192.0.2.2', 1, '2026-08-29T00:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	count, err := db.ClearAllLocks(context.Background())
	if err != nil || count != 2 {
		t.Fatalf("ClearAllLocks() = %d, %v; want 2, nil", count, err)
	}
	ipLocks, userLocks, err := db.ListLocks(context.Background())
	if err != nil || len(ipLocks) != 0 || len(userLocks) != 0 {
		t.Fatalf("locks remain after ClearAllLocks(): %d IP, %d user, %v", len(ipLocks), len(userLocks), err)
	}
}

func TestStorageNameCandidateKeepsExtensionAfterSuffix(t *testing.T) {
	if got := storageNameCandidate("conflict.txt", 1); got != "conflict (1).txt" {
		t.Fatalf("storageNameCandidate() = %q, want conflict (1).txt", got)
	}

	base := string(make([]byte, 250)) + ".txt"
	got := storageNameCandidate(base, 1)
	if len(got) > 255 {
		t.Fatalf("storageNameCandidate() length = %d, want <= 255", len(got))
	}
	if got[len(got)-4:] != ".txt" || got[len(got)-8:len(got)-4] != " (1)" {
		t.Fatalf("storageNameCandidate() = %q, want suffix before extension", got)
	}
}

func TestClearIPACL(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateIPACL(context.Background(), 1, true, "192.0.2.1"); err != nil {
		t.Fatal(err)
	}

	cleared, err := db.ClearIPACL("admin")
	if err != nil || !cleared {
		t.Fatalf("ClearIPACL(existing) = %v, %v; want true, nil", cleared, err)
	}
	user, err := db.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	if user.IPACLEnabled || user.IPWhitelist != "" {
		t.Fatalf("ClearIPACL did not clear user ACL: enabled=%t whitelist=%q", user.IPACLEnabled, user.IPWhitelist)
	}
	cleared, err = db.ClearIPACL("missing")
	if err != nil || cleared {
		t.Fatalf("ClearIPACL(missing) = %v, %v; want false, nil", cleared, err)
	}
}
