package store

import (
	"context"
	"errors"
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
