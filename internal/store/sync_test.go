package store

import (
	"context"
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
