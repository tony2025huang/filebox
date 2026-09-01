package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestListScheduledSyncTasksExcludesDisabledUsers(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := db.CreateUser(ctx, "sync-active", "hash", "user", 1024); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateUser(ctx, "sync-disabled", "hash", "user", 1024); err != nil {
		t.Fatal(err)
	}
	active, err := db.GetUserByUsername("sync-active")
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := db.GetUserByUsername("sync-disabled")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE users SET disabled = 1 WHERE id = ?", disabled.ID); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	remoteIDs := make([]int64, 0, 2)
	for _, userID := range []int64{active.ID, disabled.ID} {
		result, err := db.DB.Exec("INSERT INTO remote_systems(user_id, name, host, port, username, auth_type, auth_secret, auth_passphrase, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)", userID, "sync-target", "127.0.0.1", 22, "sync", "password", "secret", "", now)
		if err != nil {
			t.Fatal(err)
		}
		remoteID, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		remoteIDs = append(remoteIDs, remoteID)
	}
	for index, userID := range []int64{active.ID, disabled.ID} {
		if _, err := db.DB.Exec(`INSERT INTO sync_tasks(user_id, name, direction, remote_system_id, source_type, source_path, target_type, target_path, conflict_policy, schedule_type, cron, enabled, created_at)
VALUES(?, ?, 'push', ?, 'filebox', '/', 'sftp', '/', 'overwrite', 'periodic', '0 * * * *', 1, ?)`, userID, "scheduled-task", remoteIDs[index], now); err != nil {
			t.Fatal(err)
		}
	}

	tasks, err := db.ListScheduledSyncTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].UserID != active.ID {
		t.Fatalf("scheduled tasks = %+v, want only active user's task", tasks)
	}
}

func TestSyncLogRunningResultUpdate(t *testing.T) {
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
	now := time.Now().UTC().Format(time.RFC3339)
	remote, err := db.DB.Exec("INSERT INTO remote_systems(user_id, name, host, port, username, auth_type, auth_secret, auth_passphrase, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)", user.ID, "sync-target", "127.0.0.1", 22, "sync", "password", "secret", "", now)
	if err != nil {
		t.Fatal(err)
	}
	remoteID, err := remote.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	task, err := db.DB.Exec(`INSERT INTO sync_tasks(user_id, name, direction, remote_system_id, source_type, source_path, target_type, target_path, conflict_policy, schedule_type, cron, enabled, created_at)
VALUES(?, 'sync-test', 'push', ?, 'filebox', '', 'sftp', '/', 'overwrite', 'once', '', 1, ?)`, user.ID, remoteID, now)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := task.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	created, err := db.CreateSyncLog(context.Background(), SyncLog{TaskID: taskID, UserID: user.ID, RunAt: now, Direction: "push", Result: "running", Message: "执行中"})
	if err != nil {
		t.Fatal(err)
	}
	finishedAt := time.Now().UTC().Format(time.RFC3339)
	if err := db.UpdateSyncLogResult(context.Background(), created.ID, "success", finishedAt, 3, 128, "同步完成", "a.txt\nb.txt"); err != nil {
		t.Fatal(err)
	}
	logs, total, err := db.ListSyncLogs(context.Background(), taskID, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(logs) != 1 || logs[0].Result != "success" || logs[0].FinishedAt != finishedAt || logs[0].Files != 3 || logs[0].Bytes != 128 || logs[0].Message != "同步完成" || logs[0].Detail != "a.txt\nb.txt" {
		t.Fatalf("updated sync log = %+v, total=%d", logs, total)
	}
	if err := db.UpdateSyncLogResult(context.Background(), created.ID+1, "failure", finishedAt, 0, 0, "", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing sync log update error = %v, want ErrNotFound", err)
	}
}
