package store

import (
	"context"
	"testing"
	"time"
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

func TestShareStorageAndSettings(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.EnsureAdmin("admin", "admin123", 1024*1024); err != nil {
		t.Fatal(err)
	}
	user, err := db.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, 'ready', ?, ?)", user.ID, "shared.txt", "shared.txt", 3, "text/plain", "sha", "md5", "files/admin/shared.txt", now)
	if err != nil {
		t.Fatal(err)
	}
	fileID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := db.CreateShare(ctx, fileID, user.ID, "storage-token", time.Now().Add(time.Hour), 1); err != nil {
		t.Fatal(err)
	}
	share, err := db.GetShareByToken(ctx, "storage-token")
	if err != nil || share.FileID != fileID || share.MaxDownloads != 1 {
		t.Fatalf("GetShareByToken() = %+v, %v", share, err)
	}
	shares, err := db.ListSharesByFile(ctx, fileID)
	if err != nil || len(shares) != 1 {
		t.Fatalf("ListSharesByFile() = %d, %v", len(shares), err)
	}
	allowed, err := db.IncrementShareDownloads(ctx, "storage-token", 1)
	if err != nil || !allowed {
		t.Fatalf("first IncrementShareDownloads() = %t, %v", allowed, err)
	}
	allowed, err = db.IncrementShareDownloads(ctx, "storage-token", 1)
	if err != nil || allowed {
		t.Fatalf("second IncrementShareDownloads() = %t, %v", allowed, err)
	}
	stats, err := db.Stats(ctx)
	if err != nil || stats["shares"] != 1 || stats["shareDownloads"] != 1 {
		t.Fatalf("Stats() = %#v, %v", stats, err)
	}
	if removed, err := db.DeleteSharesByFile(ctx, fileID); err != nil || removed != 1 {
		t.Fatalf("DeleteSharesByFile() = %d, %v", removed, err)
	}
	settings, err := db.GetLogSettings(ctx)
	if err != nil || settings.RegisterEnabled || settings.UploadRateLimit != 0 {
		t.Fatalf("default settings = %+v, %v", settings, err)
	}
	settings.RegisterEnabled = true
	settings.UploadRateLimit = 65536
	if err := db.UpdateLogSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	updated, err := db.GetLogSettings(ctx)
	if err != nil || !updated.RegisterEnabled || updated.UploadRateLimit != 65536 {
		t.Fatalf("updated settings = %+v, %v", updated, err)
	}
	if err := db.SetSettingDefault(ctx, "registerEnabled", "false"); err != nil {
		t.Fatal(err)
	}
	unchanged, err := db.GetLogSettings(ctx)
	if err != nil || !unchanged.RegisterEnabled {
		t.Fatalf("SetSettingDefault overwrote setting = %+v, %v", unchanged, err)
	}
}
