package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestOperationsResetPasswordAndClearLocks(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// v013 #15：确认 WAL + synchronous=NORMAL 已生效（向后兼容打开老库同样适用）。
	// v013 #15: verify WAL + synchronous=NORMAL are active (applies to legacy databases opened the same way).
	var journalMode, synchronous string
	if err := db.DB.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.QueryRow("PRAGMA synchronous").Scan(&synchronous); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}
	if synchronous != "1" && synchronous != "normal" {
		t.Fatalf("synchronous = %q, want normal(1)", synchronous)
	}
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

func TestUploadTaskProgressAndConditionalDelete(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	task := UploadTask{ID: "pending-task", UserID: 1, Name: "large.bin", Size: 4, ChunkSize: 4, TotalChunks: 1, Status: "pending", Mime: "application/octet-stream"}
	if err := db.CreateUploadTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := db.SetChunk(ctx, task.ID, 0, 4, "chunk-hash"); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-25 * time.Hour).Format(time.RFC3339)
	if _, err := db.DB.Exec("UPDATE upload_tasks SET created_at = ? WHERE id = ?", old, task.ID); err != nil {
		t.Fatal(err)
	}
	progress, err := db.ListPendingTaskProgress(ctx, 1)
	if err != nil || len(progress) != 1 || progress[0].Uploaded != 1 {
		t.Fatalf("pending progress = %+v, %v", progress, err)
	}
	if _, err := db.DB.Exec("UPDATE upload_tasks SET status = 'complete' WHERE id = ?", task.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteUploadTask(ctx, task.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete completed task = %v, want ErrNotFound", err)
	}
	var chunks int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM chunks WHERE task_id = ?", task.ID).Scan(&chunks); err != nil {
		t.Fatal(err)
	}
	if chunks != 1 {
		t.Fatalf("completed task chunks = %d, want 1", chunks)
	}
	if _, err := db.DB.Exec("UPDATE upload_tasks SET status = 'pending' WHERE id = ?", task.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteUploadTask(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetUploadTask(ctx, task.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted task lookup = %v, want ErrNotFound", err)
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

func TestListUsersIncludesFolderAndReadyFileCounts(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.EnsureAdmin("admin", "admin123", 1024*1024); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := db.CreateUser(ctx, "member", "hash", "user", 1024); err != nil {
		t.Fatal(err)
	}
	member, err := db.GetUserByUsername("member")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.DB.Exec("INSERT INTO folders(user_id, name, path, created_at) VALUES(?, ?, ?, ?)", member.ID, "docs", "docs", now); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name, status string
	}{
		{"ready.bin", "ready"}, {"deleted.bin", "deleted"},
	} {
		if _, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(?, ?, ?, 4, ?, ?, ?, ?, ?, ?)", member.ID, item.name, item.name, "application/octet-stream", "sha", "md5", item.status, "files/member/"+item.name, now); err != nil {
			t.Fatal(err)
		}
	}
	users, total, err := db.ListUsers(ctx, "member", 1, 20)
	if err != nil || total != 1 || len(users) != 1 {
		t.Fatalf("ListUsers() = %d users, total %d, %v", len(users), total, err)
	}
	if users[0].FolderCount != 1 || users[0].FileCount != 1 {
		t.Fatalf("user counts = folders %d, files %d", users[0].FolderCount, users[0].FileCount)
	}
}

func TestBatchDeleteFilesIsAtomicAndUpdatesQuota(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.EnsureAdmin("admin", "admin123", 1024*1024); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	admin, err := db.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	ids := make([]int64, 0, 2)
	for _, item := range []struct {
		name string
		size int
	}{
		{"one.bin", 3}, {"two.bin", 4},
	} {
		result, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, 'ready', ?, ?)", admin.ID, item.name, item.name, item.size, "application/octet-stream", "sha", "md5", "files/admin/"+item.name, now)
		if err != nil {
			t.Fatal(err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if _, err := db.DB.Exec("UPDATE users SET used_bytes = 7 WHERE id = ?", admin.ID); err != nil {
		t.Fatal(err)
	}
	paths, err := db.BatchDeleteFiles(ctx, ids, admin.ID, false)
	if err != nil || len(paths) != 2 {
		t.Fatalf("BatchDeleteFiles() = %v, %v", paths, err)
	}
	for _, id := range ids {
		file, err := db.FindFile(ctx, id)
		if err != nil || file.Status != "deleted" {
			t.Fatalf("deleted file %d = %+v, %v", id, file, err)
		}
	}
	admin, err = db.GetUser(admin.ID)
	if err != nil || admin.UsedBytes != 0 {
		t.Fatalf("used bytes after batch delete = %d, %v", admin.UsedBytes, err)
	}

	if err := db.CreateUser(ctx, "other", "hash", "user", 1024); err != nil {
		t.Fatal(err)
	}
	other, err := db.GetUserByUsername("other")
	if err != nil {
		t.Fatal(err)
	}
	result, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(?, 'other.bin', 'other.bin', 2, ?, ?, ?, 'ready', ?, ?)", other.ID, "application/octet-stream", "sha", "md5", "files/other/other.bin", now)
	if err != nil {
		t.Fatal(err)
	}
	otherID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.BatchDeleteFiles(ctx, []int64{otherID, ids[0]}, admin.ID, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user batch delete error = %v, want ErrNotFound", err)
	}
	file, err := db.FindFile(ctx, otherID)
	if err != nil || file.Status != "ready" {
		t.Fatalf("cross-user batch delete changed file = %+v, %v", file, err)
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

func TestPruneSharesRemovesRevokedAndExpired(t *testing.T) {
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
	result, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, 'ready', ?, ?)", user.ID, "prune.txt", "prune.txt", 3, "text/plain", "sha", "md5", "files/admin/prune.txt", now)
	if err != nil {
		t.Fatal(err)
	}
	fileID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := db.CreateShare(ctx, fileID, user.ID, "prune-revoked", time.Now().Add(time.Hour), 0); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateShare(ctx, fileID, user.ID, "prune-expired", time.Now().Add(-30*24*time.Hour), 0); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateShare(ctx, fileID, user.ID, "prune-active", time.Now().Add(24*time.Hour), 0); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	if _, err := db.DB.Exec("UPDATE shares SET revoked_at = ? WHERE token = ?", old, "prune-revoked"); err != nil {
		t.Fatal(err)
	}
	removed, err := db.PruneShares(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("PruneShares() removed = %d, want 2", removed)
	}
	var remaining int
	if err := db.DB.QueryRow("SELECT COUNT(id) FROM shares").Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("shares remaining = %d, want 1", remaining)
	}
	// 最近撤销（未超过留存期）不删除
	if _, err := db.DB.Exec("UPDATE shares SET revoked_at = ? WHERE token = ?", time.Now().UTC().Format(time.RFC3339), "prune-active"); err != nil {
		t.Fatal(err)
	}
	removed, err = db.PruneShares(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Fatalf("recently revoked share pruned: removed = %d, want 0", removed)
	}
}

func TestPruneAuditLogsRemovesExpiredRecords(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	old := time.Now().UTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	recent := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.DB.Exec("INSERT INTO audit_logs(user_id, username, action, target, ip, result, reason, created_at) VALUES(NULL, ?, ?, ?, ?, ?, ?, ?)", "audit-user", "old", "", "-", "success", "", old); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("INSERT INTO audit_logs(user_id, username, action, target, ip, result, reason, created_at) VALUES(NULL, ?, ?, ?, ?, ?, ?, ?)", "audit-user", "recent", "", "-", "success", "", recent); err != nil {
		t.Fatal(err)
	}

	removed, err := db.PruneAuditLogs(ctx, 7)
	if err != nil || removed != 1 {
		t.Fatalf("PruneAuditLogs() = %d, %v; want 1, nil", removed, err)
	}
	var remaining int
	if err := db.DB.QueryRow("SELECT COUNT(id) FROM audit_logs").Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("audit logs remaining = %d, want 1", remaining)
	}
	if _, err := db.PruneAuditLogs(ctx, -1); err == nil {
		t.Fatal("PruneAuditLogs() accepted negative retention")
	}
}

func TestShareManagementPreservesRevocationAndOwnership(t *testing.T) {
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
	result, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(?, 'managed.txt', 'managed.txt', 1, 'text/plain', 'sha', 'md5', 'ready', 'files/admin/managed.txt', ?)", user.ID, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	fileID, _ := result.LastInsertId()
	ctx := context.Background()
	if err := db.CreateShare(ctx, fileID, user.ID, "managed-token", time.Now().UTC().Add(time.Hour), 2); err != nil {
		t.Fatal(err)
	}
	if err := db.AddAuditLogWithShareOwner(ctx, nil, &user.ID, "anonymous", "share_download", "managed-token", "127.0.0.1", "failure", "share_limit"); err != nil {
		t.Fatal(err)
	}
	ownerLogs, total, err := db.ListAuditLogs(ctx, &user.ID, "share_download", "", "", 1, 20)
	if err != nil || total != 1 || len(ownerLogs) != 1 || ownerLogs[0].Reason != "share_limit" {
		t.Fatalf("owner-visible share logs = %+v, total=%d, err=%v", ownerLogs, total, err)
	}
	if err := db.UpdateShareExpiry(ctx, "managed-token", time.Now().UTC().Add(48*time.Hour), user.ID+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner expiry error = %v", err)
	}
	if err := db.UpdateShareExpiry(ctx, "managed-token", time.Now().UTC().Add(48*time.Hour), user.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateShareMaxDownloads(ctx, "managed-token", 3, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateShareMaxDownloads(ctx, "managed-token", 2, user.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("decreasing max downloads error = %v", err)
	}
	shares, err := db.ListSharesByOwner(ctx, user.ID)
	if err != nil || len(shares) != 1 || shares[0].FileName != "managed.txt" || shares[0].MaxDownloads != 3 {
		t.Fatalf("managed shares = %+v, %v", shares, err)
	}
	if err := db.DeleteShareByToken(ctx, "managed-token", user.ID+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner revoke error = %v", err)
	}
	if err := db.DeleteShareByToken(ctx, "managed-token", user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetShareByToken(ctx, "managed-token"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked share default lookup = %v", err)
	}
	revoked, err := db.GetShareByTokenIncludingRevoked(ctx, "managed-token")
	if err != nil || revoked.RevokedAt == "" {
		t.Fatalf("revoked share lookup = %+v, %v", revoked, err)
	}
}

// TestOverwriteUploadKeepsOldFileUntilComplete verifies the overwrite flow preserves the old
// file until the replacement lands, then swaps content atomically (G5/G6).
// TestOverwriteUploadKeepsOldFileUntilComplete 验证覆盖上传在替换落盘前保留旧文件，随后原子换新（G5/G6）。
func TestOverwriteUploadKeepsOldFileUntilComplete(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.EnsureAdmin("admin", "admin123", 1024*1024); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	admin, err := db.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	userDir := filepath.Join("files", strconv.FormatInt(admin.ID, 10))
	oldStoragePath := filepath.Join(userDir, "old.txt")
	oldPhysicalPath := filepath.Join(db.DataDir, oldStoragePath)
	if err := os.MkdirAll(filepath.Dir(oldPhysicalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPhysicalPath, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(?, 'old.txt', 'old.txt', 3, 'text/plain', 'sha', 'md5', 'ready', ?, ?)", admin.ID, oldStoragePath, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE users SET used_bytes = 3 WHERE id = ?", admin.ID); err != nil {
		t.Fatal(err)
	}
	task := UploadTask{ID: "ow-1", UserID: admin.ID, Name: "old.txt", Size: 5, ChunkSize: 5, TotalChunks: 1, Status: "pending", StorageDir: userDir, Resolve: "overwrite"}
	if err := db.CreateUploadTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	// 任务创建后旧文件与旧记录必须原样保留：上传失败/放弃不丢数据（G6）。
	// After task creation the old file and record must remain intact: an aborted upload loses nothing (G6).
	if _, err := os.Stat(oldPhysicalPath); err != nil {
		t.Fatalf("old physical file removed at task creation: %v", err)
	}
	var oldRowCount int
	if err := db.DB.QueryRow("SELECT COUNT(id) FROM files WHERE storage_path = ? AND status = 'ready'", oldStoragePath).Scan(&oldRowCount); err != nil {
		t.Fatal(err)
	}
	if oldRowCount != 1 {
		t.Fatalf("old file row count = %d, want 1", oldRowCount)
	}
	var usedBefore int64
	if err := db.DB.QueryRow("SELECT used_bytes FROM users WHERE id = ?", admin.ID).Scan(&usedBefore); err != nil {
		t.Fatal(err)
	}
	if usedBefore != 3 {
		t.Fatalf("used bytes at task creation = %d, want 3", usedBefore)
	}

	// 完成覆盖：新内容原子替换旧文件，旧记录删除、配额更新为新文件大小（G5）。
	// Completing the overwrite atomically replaces the old file, drops the old record, and
	// updates quota to the new size (G5).
	newContent := []byte("new-content")
	newPhysicalPath := filepath.Join(db.DataDir, "tmp", "ow-1-new")
	if err := os.MkdirAll(filepath.Dir(newPhysicalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPhysicalPath, newContent, 0o600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(newPhysicalPath)
	file := File{UserID: admin.ID, Name: "old.txt", StoredName: "old.txt", Size: int64(len(newContent)), Mime: "text/plain", SHA256: "sha-new", MD5: "md5-new", StoragePath: userDir}
	completed, err := db.CompleteUploadWithPlacement(ctx, task, file, func(storagePath string, replace bool) error {
		if !replace {
			t.Fatalf("overwrite placement replace flag = false, want true")
		}
		if storagePath != oldStoragePath {
			t.Fatalf("overwrite storage path = %q, want %q", storagePath, oldStoragePath)
		}
		return os.Rename(newPhysicalPath, filepath.Join(db.DataDir, storagePath))
	})
	if err != nil {
		t.Fatal(err)
	}
	if completed.StoragePath != oldStoragePath {
		t.Fatalf("completed storage path = %q, want %q", completed.StoragePath, oldStoragePath)
	}
	content, err := os.ReadFile(oldPhysicalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(newContent) {
		t.Fatalf("replaced physical content = %q, want %q", content, newContent)
	}
	oldRowCount = 0
	if err := db.DB.QueryRow("SELECT COUNT(id) FROM files WHERE storage_path = ? AND status = 'ready'", oldStoragePath).Scan(&oldRowCount); err != nil {
		t.Fatal(err)
	}
	if oldRowCount != 1 {
		t.Fatalf("file rows at replaced path = %d, want 1", oldRowCount)
	}
	var newRowID int64
	if err := db.DB.QueryRow("SELECT id, size FROM files WHERE storage_path = ?", oldStoragePath).Scan(&newRowID, new(int64)); err != nil {
		t.Fatal(err)
	}
	if newRowID == 0 {
		t.Fatal("new file row missing")
	}
	var used int64
	if err := db.DB.QueryRow("SELECT used_bytes FROM users WHERE id = ?", admin.ID).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if used != int64(len(newContent)) {
		t.Fatalf("used bytes after overwrite = %d, want %d", used, len(newContent))
	}

	// 非覆盖任务绝不触碰同名目标文件。
	// A non-overwrite task must never touch the same-named target.
	keepStoragePath := filepath.Join(userDir, "keep.txt")
	keepPhysicalPath := filepath.Join(db.DataDir, keepStoragePath)
	if err := os.WriteFile(keepPhysicalPath, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(?, 'keep.txt', 'keep.txt', 4, 'text/plain', 'sha', 'md5', 'ready', ?, ?)", admin.ID, keepStoragePath, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE users SET used_bytes = ? WHERE id = ?", int64(len(newContent)), admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateUploadTask(ctx, UploadTask{ID: "keep-1", UserID: admin.ID, Name: "keep.txt", Size: 5, ChunkSize: 5, TotalChunks: 1, Status: "pending", StorageDir: userDir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keepPhysicalPath); err != nil {
		t.Fatalf("non-overwrite physical file was removed: %v", err)
	}
}

// TestRenameFolderMovesDiskDirectoryAfterCommit verifies the disk move follows the database commit.
// TestRenameFolderMovesDiskDirectoryAfterCommit 验证磁盘目录移动发生在数据库事务提交之后。
func TestRenameFolderMovesDiskDirectoryAfterCommit(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.DB.Exec("INSERT INTO users(id, username, password_hash, role, created_at, updated_at) VALUES(1, 'u1', 'h', 'user', ?, ?)", now, now); err != nil {
		t.Fatal(err)
	}
	folder, err := db.CreateFolder(ctx, 1, "", "docs")
	if err != nil {
		t.Fatal(err)
	}
	docsDiskPath := filepath.Join(db.DataDir, "files", "1", "docs")
	if err := os.MkdirAll(docsDiskPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsDiskPath, "plan.txt"), []byte("plan"), 0o644); err != nil {
		t.Fatal(err)
	}
	storagePath := filepath.Join("files", "1", "docs", "plan.txt")
	if _, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(1, 'plan.txt', 'plan.txt', 4, 'text/plain', 'sha', 'md5', 'ready', ?, ?)", storagePath, now); err != nil {
		t.Fatal(err)
	}
	if err := db.RenameFolder(ctx, folder.ID, 1, "archive"); err != nil {
		t.Fatal(err)
	}
	archivePlanPath := filepath.Join(db.DataDir, "files", "1", "archive", "plan.txt")
	if _, err := os.Stat(archivePlanPath); err != nil {
		t.Fatalf("renamed physical file is missing: %v", err)
	}
	if _, err := os.Stat(docsDiskPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old disk directory still exists: %v", err)
	}
	var updatedStoragePath string
	if err := db.DB.QueryRow("SELECT storage_path FROM files WHERE status = 'ready'").Scan(&updatedStoragePath); err != nil {
		t.Fatal(err)
	}
	newPrefix := filepath.Join("files", "1", "archive") + string(filepath.Separator)
	if !strings.HasPrefix(updatedStoragePath, newPrefix) {
		t.Fatalf("ready file storage path = %q, want prefix %q", updatedStoragePath, newPrefix)
	}
}

// TestRenameFolderClearsDeletedRows: 目录重命名会把其下 ready 文件的 storage_path
// 前缀改写为新目录；若目标前缀下残留同名的软删除记录（删除后重传再改目录），
// UNIQUE(storage_path) 会冲突。deleted 内容已物理删除，重命名前应清理。
// TestRenameFolderClearsDeletedRows covers the rename-folder path where a deleted
// row already holds the target storage_path; the rewrite must clear it first.
func TestRenameFolderClearsDeletedRows(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.DB.Exec("INSERT INTO users(id, username, password_hash, role, created_at, updated_at) VALUES(1, 'u1', 'h', 'user', ?, ?)", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateFolder(ctx, 1, "", "docs"); err != nil {
		t.Fatal(err)
	}
	// ready 文件位于 files/1/docs/plan.txt
	if _, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(1, 'plan.txt', 'plan.txt', 4, 'text/plain', 'sha', 'md5', 'ready', 'files\\1\\docs\\plan.txt', ?)", now); err != nil {
		t.Fatal(err)
	}
	// 软删除记录已占用重命名后的目标路径 files/1/archive/plan.txt
	if _, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(1, 'plan.txt', 'plan.txt', 4, 'text/plain', 'sha', 'md5', 'deleted', 'files\\1\\archive\\plan.txt', ?)", now); err != nil {
		t.Fatal(err)
	}
	folder, err := db.GetFolderByPath(ctx, 1, "docs")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RenameFolder(ctx, folder.ID, 1, "archive"); err != nil {
		t.Fatalf("RenameFolder with colliding deleted row = %v", err)
	}
	var count int
	if err := db.DB.QueryRow("SELECT COUNT(id) FROM files WHERE status = 'deleted' AND storage_path = 'files\\1\\archive\\plan.txt'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("deleted row was not cleared before rewrite: count=%d", count)
	}
	var sp string
	if err := db.DB.QueryRow("SELECT storage_path FROM files WHERE status = 'ready'").Scan(&sp); err != nil {
		t.Fatal(err)
	}
	if sp != "files\\1\\archive\\plan.txt" {
		t.Fatalf("ready file not rewritten to new prefix: %q", sp)
	}
}
