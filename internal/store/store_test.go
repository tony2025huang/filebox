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

func TestDeleteCollectionUploadTasksDoesNotChangeSlots(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	collection, err := db.CreateUploadCollection(ctx, UploadCollection{
		CreatedBy: 1, Name: "cleanup", Token: "cleanup-token",
		ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339), MaxUploads: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"collection-task-1", "collection-task-2"} {
		if err := db.CreateCollectionUploadTask(ctx, UploadTask{ID: id, UserID: 1, CollectionID: collection.ID, Name: id, Size: 1, ChunkSize: 1, TotalChunks: 1, Status: "pending", Mime: "text/plain"}, collection.Token); err != nil {
			t.Fatalf("CreateCollectionUploadTask(%q) = %v", id, err)
		}
	}
	var count int
	if err := db.DB.QueryRow("SELECT upload_count FROM upload_collections WHERE id = ?", collection.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("upload_count before cleanup = %d, want 0", count)
	}
	old := time.Now().UTC().Add(-25 * time.Hour).Format(time.RFC3339)
	if _, err := db.DB.Exec("UPDATE upload_tasks SET created_at = ? WHERE collection_id = ?", old, collection.ID); err != nil {
		t.Fatal(err)
	}
	expired, err := db.ListExpiredUploadTasks(ctx, 24*time.Hour)
	if err != nil || len(expired) != 2 {
		t.Fatalf("ListExpiredUploadTasks() = %v, %v; want 2 tasks", expired, err)
	}
	for _, id := range expired {
		if err := db.DeleteUploadTask(ctx, id); err != nil {
			t.Fatalf("DeleteUploadTask(%q) = %v", id, err)
		}
	}
	if err := db.DB.QueryRow("SELECT upload_count FROM upload_collections WHERE id = ?", collection.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("upload_count after cleanup = %d, want 0", count)
	}
	if err := db.CreateCollectionUploadTask(ctx, UploadTask{ID: "collection-task-3", UserID: 1, CollectionID: collection.ID, Name: "collection-task-3", Size: 1, ChunkSize: 1, TotalChunks: 1, Status: "pending", Mime: "text/plain"}, collection.Token); err != nil {
		t.Fatalf("new collection task after cleanup = %v", err)
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

func TestCollectionPendingUploadTaskLimitAndRelease(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	collection, err := db.CreateUploadCollection(ctx, UploadCollection{
		CreatedBy: 1, Name: "pending-limit", Token: "pending-limit-token",
		ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	tasks := make([]string, 0, MaxPendingCollectionUploadTasks)
	for i := 0; i < MaxPendingCollectionUploadTasks; i++ {
		id := "pending-limit-task-" + strconv.Itoa(i)
		tasks = append(tasks, id)
		if err := db.CreateCollectionUploadTask(ctx, UploadTask{
			ID: id, UserID: 1, CollectionID: collection.ID, Name: id,
			Size: 1, ChunkSize: 1, TotalChunks: 1, Status: "pending", Mime: "text/plain",
		}, collection.Token); err != nil {
			t.Fatalf("CreateCollectionUploadTask(%d) = %v", i+1, err)
		}
	}
	queuedState, err := db.CreateCollectionUploadTaskWithState(ctx, UploadTask{
		ID: "pending-limit-task-51", UserID: 1, CollectionID: collection.ID,
		Name: "rejected", Size: 1, ChunkSize: 1, TotalChunks: 1, Status: "pending", Mime: "text/plain",
	}, collection.Token)
	if err != nil || queuedState.State != "queued" || queuedState.QueuePosition != 1 {
		t.Fatalf("51st CreateCollectionUploadTaskWithState() = %+v, %v; want queued at position 1", queuedState, err)
	}

	completedTask, err := db.GetUploadTask(ctx, tasks[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CompleteCollectionFile(ctx, completedTask, File{
		UserID: 1, Name: "completed.txt", StoredName: "completed.txt", Size: 1,
		Mime: "text/plain", SHA256: "completed-sha", MD5: "completed-md5",
		StoragePath: "files/1/completed.txt",
	}, "completed.txt", ""); err != nil {
		t.Fatalf("CompleteCollectionFile() = %v", err)
	}
	if err := db.DeleteUploadTask(ctx, tasks[1]); err != nil {
		t.Fatalf("DeleteUploadTask(cancelled) = %v", err)
	}
	old := time.Now().UTC().Add(-25 * time.Hour).Format(time.RFC3339)
	if _, err := db.DB.Exec("UPDATE upload_tasks SET created_at = ? WHERE id = ?", old, tasks[2]); err != nil {
		t.Fatal(err)
	}
	expired, err := db.ListExpiredUploadTasks(ctx, 24*time.Hour)
	if err != nil || len(expired) != 1 || expired[0] != tasks[2] {
		t.Fatalf("ListExpiredUploadTasks() = %v, %v; want %q", expired, err, tasks[2])
	}
	if err := db.DeleteUploadTask(ctx, tasks[2]); err != nil {
		t.Fatalf("DeleteUploadTask(expired) = %v", err)
	}

	for i := 0; i < 2; i++ {
		id := "pending-limit-replacement-" + strconv.Itoa(i)
		if err := db.CreateCollectionUploadTask(ctx, UploadTask{
			ID: id, UserID: 1, CollectionID: collection.ID, Name: id,
			Size: 1, ChunkSize: 1, TotalChunks: 1, Status: "pending", Mime: "text/plain",
		}, collection.Token); err != nil {
			t.Fatalf("replacement CreateCollectionUploadTask(%d) = %v", i+1, err)
		}
	}
	var pendingTasks int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM upload_tasks WHERE collection_id = ? AND status NOT IN ('complete', 'cancelled', 'expired')", collection.ID).Scan(&pendingTasks); err != nil {
		t.Fatal(err)
	}
	if pendingTasks != MaxPendingCollectionUploadTasks {
		t.Fatalf("non-terminal task count after releases = %d, want %d", pendingTasks, MaxPendingCollectionUploadTasks)
	}
}

func TestConcurrentCollectionPendingUploadTaskLimit(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	collection, err := db.CreateUploadCollection(ctx, UploadCollection{
		CreatedBy: 1, Name: "concurrent-pending-limit", Token: "concurrent-pending-limit-token",
		ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, MaxPendingCollectionUploadTasks+1)
	for i := 0; i < MaxPendingCollectionUploadTasks+1; i++ {
		go func(index int) {
			<-start
			errs <- db.CreateCollectionUploadTask(ctx, UploadTask{
				ID: "concurrent-pending-task-" + strconv.Itoa(index), UserID: 1,
				CollectionID: collection.ID, Name: "concurrent-" + strconv.Itoa(index),
				Size: 1, ChunkSize: 1, TotalChunks: 1, Status: "pending", Mime: "text/plain",
			}, collection.Token)
		}(i)
	}
	close(start)
	successes := 0
	limitErrors := 0
	for i := 0; i < MaxPendingCollectionUploadTasks+1; i++ {
		err := <-errs
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrCollectionLimit):
			limitErrors++
		default:
			t.Fatalf("concurrent CreateCollectionUploadTask() = %v", err)
		}
	}
	if successes != MaxPendingCollectionUploadTasks+1 || limitErrors != 0 {
		t.Fatalf("concurrent results = %d successes, %d limit errors; want %d, 0", successes, limitErrors, MaxPendingCollectionUploadTasks+1)
	}
	var active, queued int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM upload_tasks WHERE collection_id = ? AND status = 'active'", collection.ID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM upload_tasks WHERE collection_id = ? AND status = 'queued'", collection.ID).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if active != MaxPendingCollectionUploadTasks || queued != 1 {
		t.Fatalf("concurrent task states = %d active, %d queued; want %d, 1", active, queued, MaxPendingCollectionUploadTasks)
	}
}

func TestCollectionQueuedTasksPromoteFIFOOnCompleteCancelAndExpired(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.EnsureAdmin("admin", "admin123", 1024*1024); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	collection, err := db.CreateUploadCollection(ctx, UploadCollection{
		CreatedBy: 1, Name: "fifo", Token: "fifo-token",
		ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	create := func(id string) {
		t.Helper()
		if err := db.CreateCollectionUploadTask(ctx, UploadTask{ID: id, UserID: 1, CollectionID: collection.ID, Name: id, Size: 1, ChunkSize: 1, TotalChunks: 1, Status: "pending", Mime: "text/plain"}, collection.Token); err != nil {
			t.Fatalf("create task %q: %v", id, err)
		}
	}
	for i := 0; i < MaxPendingCollectionUploadTasks; i++ {
		create("fifo-active-" + strconv.Itoa(i))
	}
	create("fifo-queued-1")
	create("fifo-queued-2")
	create("fifo-queued-3")

	active, err := db.GetUploadTask(ctx, "fifo-active-0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CompleteCollectionFile(ctx, active, File{UserID: 1, Name: "fifo-complete.txt", StoredName: "fifo-complete.txt", Size: 1, Mime: "text/plain", StoragePath: "files/1/fifo-complete.txt"}, active.Name, ""); err != nil {
		t.Fatalf("complete active task: %v", err)
	}
	assertCollectionTaskStatus(t, db, ctx, "fifo-queued-1", "active")
	assertCollectionTaskStatus(t, db, ctx, "fifo-queued-2", "queued")

	if err := db.DeleteUploadTask(ctx, "fifo-active-1"); err != nil {
		t.Fatalf("cancel active task: %v", err)
	}
	assertCollectionTaskStatus(t, db, ctx, "fifo-queued-2", "active")
	assertCollectionTaskStatus(t, db, ctx, "fifo-queued-3", "queued")

	old := time.Now().UTC().Add(-25 * time.Hour).Format(time.RFC3339)
	if _, err := db.DB.Exec("UPDATE upload_tasks SET created_at = ? WHERE id = ?", old, "fifo-active-2"); err != nil {
		t.Fatal(err)
	}
	expired, err := db.ListExpiredUploadTasks(ctx, 24*time.Hour)
	if err != nil || len(expired) != 1 || expired[0] != "fifo-active-2" {
		t.Fatalf("expired tasks = %v, %v; want fifo-active-2", expired, err)
	}
	if err := db.DeleteUploadTask(ctx, expired[0]); err != nil {
		t.Fatalf("expire active task: %v", err)
	}
	assertCollectionTaskStatus(t, db, ctx, "fifo-queued-3", "active")
}

func TestCollectionQueuedTaskDoesNotReserveQuotaOrProcessChunks(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	collection, err := db.CreateUploadCollection(ctx, UploadCollection{CreatedBy: 1, Name: "quota-queue", Token: "quota-queue-token", ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxPendingCollectionUploadTasks; i++ {
		if err := db.CreateCollectionUploadTask(ctx, UploadTask{ID: "quota-active-" + strconv.Itoa(i), UserID: 1, CollectionID: collection.ID, Name: "active", Size: 10, ChunkSize: 10, TotalChunks: 1, Status: "pending", Mime: "text/plain"}, collection.Token); err != nil {
			t.Fatal(err)
		}
	}
	state, err := db.CreateCollectionUploadTaskWithState(ctx, UploadTask{ID: "quota-queued-1", UserID: 1, CollectionID: collection.ID, Name: "queued-1", Size: 600, ChunkSize: 600, TotalChunks: 1, Status: "pending", Mime: "text/plain"}, collection.Token)
	if err != nil || state.State != "queued" {
		t.Fatalf("queued task = %+v, %v", state, err)
	}
	if _, err := db.CreateCollectionUploadTaskWithState(ctx, UploadTask{ID: "quota-queued-2", UserID: 1, CollectionID: collection.ID, Name: "queued-2", Size: 1, ChunkSize: 1, TotalChunks: 1, Status: "pending", Mime: "text/plain"}, collection.Token); err != nil {
		t.Fatal(err)
	}
	if err := db.SetChunk(ctx, "quota-queued-1", 0, 600, "hash"); !errors.Is(err, ErrCollectionTaskQueued) {
		t.Fatalf("SetChunk queued = %v, want ErrCollectionTaskQueued", err)
	}
	queued, err := db.GetUploadTask(ctx, "quota-queued-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CompleteCollectionFile(ctx, queued, File{UserID: 1, Name: "queued.txt", Size: 600, Mime: "text/plain", StoragePath: "files/1/queued.txt"}, queued.Name, ""); !errors.Is(err, ErrCollectionTaskQueued) {
		t.Fatalf("complete queued = %v, want ErrCollectionTaskQueued", err)
	}
	var reserved int64
	if err := db.DB.QueryRow("SELECT COALESCE(SUM(size), 0) FROM upload_tasks WHERE user_id = 1 AND status IN ('pending', 'active')").Scan(&reserved); err != nil {
		t.Fatal(err)
	}
	if reserved != MaxPendingCollectionUploadTasks*10 {
		t.Fatalf("reserved bytes = %d, want %d", reserved, MaxPendingCollectionUploadTasks*10)
	}
	regular := UploadTask{ID: "quota-regular", UserID: 1, Name: "regular", Size: 500, ChunkSize: 500, TotalChunks: 1, Status: "pending", Mime: "text/plain"}
	if err := db.CreateUploadTask(ctx, regular); err != nil {
		t.Fatalf("regular task after queued task = %v", err)
	}
	if err := db.DeleteUploadTask(ctx, regular.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteUploadTask(ctx, "quota-active-0"); err != nil {
		t.Fatal(err)
	}
	state, err = db.GetCollectionUploadTaskState(ctx, collection.ID, "quota-queued-1")
	if err != nil {
		t.Fatal(err)
	}
	if state.State != "queued" || state.WaitReason != "quota_exceeded" {
		t.Fatalf("queue head state = %+v, want quota_exceeded queued", state)
	}
	assertCollectionTaskStatus(t, db, ctx, "quota-queued-2", "queued")
}

func TestConcurrentCollectionCompleteAndInitPromoteWithoutOverflow(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.EnsureAdmin("admin", "admin123", 1024*1024); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	collection, err := db.CreateUploadCollection(ctx, UploadCollection{CreatedBy: 1, Name: "concurrent-complete", Token: "concurrent-complete-token", ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxPendingCollectionUploadTasks+10; i++ {
		if err := db.CreateCollectionUploadTask(ctx, UploadTask{ID: "concurrent-complete-task-" + strconv.Itoa(i), UserID: 1, CollectionID: collection.ID, Name: "task", Size: 1, ChunkSize: 1, TotalChunks: 1, Status: "pending", Mime: "text/plain"}, collection.Token); err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	errs := make(chan error, 20)
	for i := 0; i < 10; i++ {
		id := "concurrent-complete-task-" + strconv.Itoa(i)
		go func(index int, taskID string) {
			<-start
			task, getErr := db.GetUploadTask(ctx, taskID)
			if getErr != nil {
				errs <- getErr
				return
			}
			_, completeErr := db.CompleteCollectionFile(ctx, task, File{UserID: 1, Name: "concurrent-" + strconv.Itoa(index) + ".txt", StoredName: "concurrent-" + strconv.Itoa(index) + ".txt", Size: 1, Mime: "text/plain", StoragePath: "files/1/concurrent-" + strconv.Itoa(index) + ".txt"}, task.Name, "")
			errs <- completeErr
		}(i, id)
	}
	for i := 0; i < 10; i++ {
		go func(index int) {
			<-start
			errs <- db.CreateCollectionUploadTask(ctx, UploadTask{ID: "concurrent-init-task-" + strconv.Itoa(index), UserID: 1, CollectionID: collection.ID, Name: "new", Size: 1, ChunkSize: 1, TotalChunks: 1, Status: "pending", Mime: "text/plain"}, collection.Token)
		}(i)
	}
	close(start)
	for i := 0; i < cap(errs); i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent complete/init = %v", err)
		}
	}
	var active, queued int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM upload_tasks WHERE collection_id = ? AND status = 'active'", collection.ID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM upload_tasks WHERE collection_id = ? AND status = 'queued'", collection.ID).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if active != MaxPendingCollectionUploadTasks || queued != 10 {
		t.Fatalf("concurrent final states = %d active, %d queued; want %d, 10", active, queued, MaxPendingCollectionUploadTasks)
	}
}

func assertCollectionTaskStatus(t *testing.T, db *Store, ctx context.Context, taskID, want string) {
	t.Helper()
	task, err := db.GetUploadTask(ctx, taskID)
	if err != nil {
		t.Fatalf("get task %q: %v", taskID, err)
	}
	if task.Status != want {
		t.Fatalf("task %q status = %q, want %q", taskID, task.Status, want)
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

func TestListUsersEscapesLikeWildcards(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, username := range []string{"literal%user", "literalXuser", "literal_user", "literalXuser2"} {
		if err := db.CreateUser(ctx, username, "hash", "user", 1024); err != nil {
			t.Fatal(err)
		}
	}
	users, total, err := db.ListUsers(ctx, "%", 1, 20)
	if err != nil || total != 1 || len(users) != 1 || users[0].Username != "literal%user" {
		t.Fatalf("percent search = %#v, total=%d, err=%v", users, total, err)
	}
	users, total, err = db.ListUsers(ctx, "_", 1, 20)
	if err != nil || total != 1 || len(users) != 1 || users[0].Username != "literal_user" {
		t.Fatalf("underscore search = %#v, total=%d, err=%v", users, total, err)
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
	allowed, err := db.IncrementShareDownloads(ctx, "storage-token", 1, false)
	if err != nil || !allowed {
		t.Fatalf("first IncrementShareDownloads() = %t, %v", allowed, err)
	}
	allowed, err = db.IncrementShareDownloads(ctx, "storage-token", 1, false)
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

// TestShareDownloadRangeWindowDeduplicatesContinuousRanges verifies Range-window counting and full-download behavior.
// TestShareDownloadRangeWindowDeduplicatesContinuousRanges 验证 Range 窗口去重及完整下载计数行为。
func TestShareDownloadRangeWindowDeduplicatesContinuousRanges(t *testing.T) {
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
	result, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(?, 'range.txt', 'range.txt', 1, 'text/plain', 'sha', 'md5', 'ready', ?, ?)", user.ID, "files/admin/range.txt", now)
	if err != nil {
		t.Fatal(err)
	}
	fileID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := db.CreateShare(ctx, fileID, user.ID, "range-window", time.Now().UTC().Add(time.Hour), 3); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		allowed, err := db.IncrementShareDownloads(ctx, "range-window", 3, true)
		if err != nil || !allowed {
			t.Fatalf("Range download %d = %t, %v; want allowed", i+1, allowed, err)
		}
	}
	share, err := db.GetShareByToken(ctx, "range-window")
	if err != nil || share.DownloadCount != 1 {
		t.Fatalf("continuous Range count = %d, %v; want 1", share.DownloadCount, err)
	}

	if err := db.CreateShare(ctx, fileID, user.ID, "full-download", time.Now().UTC().Add(time.Hour), 1); err != nil {
		t.Fatal(err)
	}
	allowed, err := db.IncrementShareDownloads(ctx, "full-download", 1, false)
	if err != nil || !allowed {
		t.Fatalf("full download = %t, %v; want allowed", allowed, err)
	}
	allowed, err = db.IncrementShareDownloads(ctx, "full-download", 1, false)
	if err != nil || allowed {
		t.Fatalf("second full download = %t, %v; want denied", allowed, err)
	}
	share, err = db.GetShareByToken(ctx, "full-download")
	if err != nil || share.DownloadCount != 1 {
		t.Fatalf("full download count = %d, %v; want 1", share.DownloadCount, err)
	}

	if err := db.CreateShare(ctx, fileID, user.ID, "range-expired", time.Now().UTC().Add(time.Hour), 2); err != nil {
		t.Fatal(err)
	}
	allowed, err = db.IncrementShareDownloads(ctx, "range-expired", 2, true)
	if err != nil || !allowed {
		t.Fatalf("initial expired-window Range = %t, %v; want allowed", allowed, err)
	}
	old := time.Now().UTC().Add(-61 * time.Second).Format(time.RFC3339)
	if _, err := db.DB.Exec("UPDATE shares SET last_download_at = ? WHERE token = ?", old, "range-expired"); err != nil {
		t.Fatal(err)
	}
	allowed, err = db.IncrementShareDownloads(ctx, "range-expired", 2, true)
	if err != nil || !allowed {
		t.Fatalf("Range outside window = %t, %v; want allowed", allowed, err)
	}
	share, err = db.GetShareByToken(ctx, "range-expired")
	if err != nil || share.DownloadCount != 2 {
		t.Fatalf("outside-window Range count = %d, %v; want 2", share.DownloadCount, err)
	}
}

func TestIncrementShareDownloadsRangeDeniedAfterLimit(t *testing.T) {
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
	result, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(?, 'range-limit.txt', 'range-limit.txt', 1, 'text/plain', 'sha', 'md5', 'ready', ?, ?)", user.ID, "files/admin/range-limit.txt", now)
	if err != nil {
		t.Fatal(err)
	}
	fileID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := db.CreateShare(ctx, fileID, user.ID, "range-limit", time.Now().UTC().Add(time.Hour), 1); err != nil {
		t.Fatal(err)
	}
	allowed, err := db.IncrementShareDownloads(ctx, "range-limit", 1, true)
	if err != nil || !allowed {
		t.Fatalf("initial limited Range = %t, %v; want allowed", allowed, err)
	}
	allowed, err = db.IncrementShareDownloads(ctx, "range-limit", 1, true)
	if err != nil || allowed {
		t.Fatalf("Range after limit = %t, %v; want denied", allowed, err)
	}
	share, err := db.GetShareByToken(ctx, "range-limit")
	if err != nil || share.DownloadCount != 1 {
		t.Fatalf("limited Range count = %d, %v; want 1", share.DownloadCount, err)
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
	groupExpiry := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	if _, _, err := db.CreateShareGroup(ctx, user.ID, "prune-group-revoked", []int64{fileID}, groupExpiry, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.CreateShareGroup(ctx, user.ID, "prune-group-expired", []int64{fileID}, old, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.CreateShareGroup(ctx, user.ID, "prune-group-active", []int64{fileID}, groupExpiry, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE shares SET revoked_at = ? WHERE token = ?", old, "prune-revoked"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE share_groups SET revoked_at = ? WHERE token = ?", old, "prune-group-revoked"); err != nil {
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
	var remainingGroups, remainingGroupFiles int
	if err := db.DB.QueryRow("SELECT COUNT(id) FROM share_groups").Scan(&remainingGroups); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.QueryRow("SELECT COUNT(id) FROM share_group_files").Scan(&remainingGroupFiles); err != nil {
		t.Fatal(err)
	}
	if remainingGroups != 1 || remainingGroupFiles != 1 {
		t.Fatalf("share groups remaining = %d, files = %d; want 1, 1", remainingGroups, remainingGroupFiles)
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

	removed, err := db.PruneAuditLogs(ctx, 0)
	if err != nil || removed != 0 {
		t.Fatalf("PruneAuditLogs(0) = %d, %v; want 0, nil", removed, err)
	}
	removed, err = db.PruneAuditLogs(ctx, 7)
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

// TestAddAuditLogOnlyInsertsWithoutPruning verifies audit writes do not delete retained records.
// TestAddAuditLogOnlyInsertsWithoutPruning 验证审计写入只插入记录，不清理已有记录。
func TestAddAuditLogOnlyInsertsWithoutPruning(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := time.Now().UTC().Add(-31 * 24 * time.Hour).Format(time.RFC3339)
	if _, err := db.DB.Exec("INSERT INTO audit_logs(user_id, username, action, target, ip, result, reason, created_at) VALUES(NULL, ?, ?, ?, ?, ?, ?, ?)", "audit-user", "old", "", "-", "success", "", old); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, action := range []string{"first", "second"} {
		if err := db.AddAuditLog(ctx, nil, "audit-user", action, "", "-", "success", ""); err != nil {
			t.Fatalf("AddAuditLog(%q) = %v", action, err)
		}
	}
	var count int
	if err := db.DB.QueryRow("SELECT COUNT(id) FROM audit_logs").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("audit log count after writes = %d, want 3", count)
	}
}

// TestConsumeTOTPRejectsReplayedAndOlderCounters verifies monotonic replay protection across time windows.
// TestConsumeTOTPRejectsReplayedAndOlderCounters 验证跨时间窗口拒绝相同及更旧的动态码计数器。
func TestConsumeTOTPRejectsReplayedAndOlderCounters(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	firstAt := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	if accepted, err := db.ConsumeTOTP(ctx, 1, 100, firstAt); err != nil || !accepted {
		t.Fatalf("first TOTP consumption = %t, %v; want true, nil", accepted, err)
	}
	if accepted, err := db.ConsumeTOTP(ctx, 1, 100, firstAt.Add(2*time.Hour)); err != nil || accepted {
		t.Fatalf("same-counter TOTP replay = %t, %v; want false, nil", accepted, err)
	}
	if accepted, err := db.ConsumeTOTP(ctx, 1, 99, firstAt.Add(2*time.Hour)); err != nil || accepted {
		t.Fatalf("older-counter TOTP replay = %t, %v; want false, nil", accepted, err)
	}
	if accepted, err := db.ConsumeTOTP(ctx, 1, 101, firstAt.Add(2*time.Hour)); err != nil || !accepted {
		t.Fatalf("newer TOTP counter = %t, %v; want true, nil", accepted, err)
	}
	if accepted, err := db.ConsumeTOTP(ctx, 1, 100, firstAt.Add(4*time.Hour)); err != nil || accepted {
		t.Fatalf("counter below maximum replay = %t, %v; want false, nil", accepted, err)
	}
}

func TestAuditLogsTimeRangeFilter(t *testing.T) {
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
	ctx := context.Background()
	base := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	// 直接写入三条不同时间点的审计日志（created_at 为 RFC3339 文本）。
	// Insert three audit entries at distinct timestamps (created_at is RFC3339 text).
	for i, offset := range []time.Duration{-48 * time.Hour, 0, 48 * time.Hour} {
		at := base.Add(offset).Format(time.RFC3339)
		if _, err := db.DB.ExecContext(ctx, "INSERT INTO audit_logs(user_id, share_owner_id, username, action, target, ip, result, reason, created_at) VALUES(?, NULL, ?, 'login', 'time-range', '127.0.0.1', 'success', NULL, ?)", user.ID, user.Username, at); err != nil {
			t.Fatalf("insert audit log %d: %v", i, err)
		}
	}
	// 空 from/to：返回全部三条。
	all, total, err := db.ListAuditLogs(ctx, &user.ID, "", "", "", "", "", 1, 20)
	if err != nil || total != 3 || len(all) != 3 {
		t.Fatalf("no-range logs = %d, total=%d, err=%v", len(all), total, err)
	}
	// 仅 from：包含边界当天（>= 基准时间）→ 应命中 3 条中的 2 条（当天与未来）。
	fromOnly, totalFrom, err := db.ListAuditLogs(ctx, &user.ID, "", "", "", base.Format(time.RFC3339), "", 1, 20)
	if err != nil || totalFrom != 2 || len(fromOnly) != 2 {
		t.Fatalf("from-only logs = %d, total=%d, err=%v", len(fromOnly), totalFrom, err)
	}
	// 仅 to：<= 基准时间 → 2 条（过去与当天，含边界）。
	toOnly, totalTo, err := db.ListAuditLogs(ctx, &user.ID, "", "", "", "", base.Format(time.RFC3339), 1, 20)
	if err != nil || totalTo != 2 || len(toOnly) != 2 {
		t.Fatalf("to-only logs = %d, total=%d, err=%v", len(toOnly), totalTo, err)
	}
	// from+to 同时给出：过去 24h 内 → 1 条。
	both, totalBoth, err := db.ListAuditLogs(ctx, &user.ID, "", "", "", base.Add(-24*time.Hour).Format(time.RFC3339), base.Add(24*time.Hour).Format(time.RFC3339), 1, 20)
	if err != nil || totalBoth != 1 || len(both) != 1 || both[0].CreatedAt == "" {
		_ = both
		t.Fatalf("both-range logs = total=%d, err=%v", totalBoth, err)
	}
	// 完全未来区间：0 条。
	future, totalFuture, err := db.ListAuditLogs(ctx, &user.ID, "", "", "", base.Add(96*time.Hour).Format(time.RFC3339), base.Add(120*time.Hour).Format(time.RFC3339), 1, 20)
	if err != nil || totalFuture != 0 || len(future) != 0 {
		t.Fatalf("future-range logs = %d, total=%d, err=%v", len(future), totalFuture, err)
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
	ownerLogs, total, err := db.ListAuditLogs(ctx, &user.ID, "share_download", "", "", "", "", 1, 20)
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

// TestAdminListFilesDirUsesStoragePrefix 验证管理员目录过滤不绑定管理员 uid。
// TestAdminListFilesDirUsesStoragePrefix verifies admin directory filters are not tied to the admin uid.
func TestAdminListFilesDirUsesStoragePrefix(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		t.Fatal(err)
	}
	admin, err := db.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateUser(context.Background(), "dir-user", "hash", "user", 1024); err != nil {
		t.Fatal(err)
	}
	other, err := db.GetUserByUsername("dir-user")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	dir := filepath.Join(strconv.FormatInt(other.ID, 10), "docs")
	storagePath := filepath.Join("files", dir, "remote.txt")
	if _, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(?, ?, ?, 1, 'text/plain', 'sha', 'md5', 'ready', ?, ?)", other.ID, "remote.txt", "remote.txt", storagePath, now); err != nil {
		t.Fatal(err)
	}
	files, total, err := db.ListFiles(context.Background(), admin.ID, true, "", dir, 1, 20)
	if err != nil || total != 1 || len(files) != 1 || files[0].UserID != other.ID {
		t.Fatalf("admin directory list = %+v, total=%d, err=%v", files, total, err)
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

// TestCompleteUploadWaitsForFolderLock verifies completion cannot allocate or place while a folder operation holds the user lock.
// TestCompleteUploadWaitsForFolderLock 验证目录操作持有用户锁时 complete 不会分配或放置文件。
func TestCompleteUploadWaitsForFolderLock(t *testing.T) {
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
	ctx := context.Background()
	if _, err := db.CreateFolder(ctx, user.ID, "", "docs"); err != nil {
		t.Fatal(err)
	}
	storageDir := filepath.Join("files", strconv.FormatInt(user.ID, 10), "docs")
	task := UploadTask{ID: "folder-lock-complete", UserID: user.ID, Name: "report.txt", Size: 1, ChunkSize: 1, TotalChunks: 1, Status: "pending", StorageDir: storageDir}
	if err := db.CreateUploadTask(ctx, task); err != nil {
		t.Fatal(err)
	}

	lock := db.folderLock(user.ID)
	lock.Lock()
	placed := make(chan string, 1)
	completed := make(chan error, 1)
	go func() {
		file := File{UserID: user.ID, Name: task.Name, StoredName: task.Name, Size: task.Size, StoragePath: storageDir}
		_, completeErr := db.CompleteUploadWithPlacement(ctx, task, file, func(storagePath string, replace bool) error {
			placed <- storagePath
			return nil
		})
		completed <- completeErr
	}()
	select {
	case path := <-placed:
		t.Fatalf("placement started while folder lock was held: %q", path)
	case <-time.After(100 * time.Millisecond):
	}
	lock.Unlock()

	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("completion did not proceed after folder lock was released")
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
