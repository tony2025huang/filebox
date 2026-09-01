package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// ErrSyncReferenced 表示目标系统仍被同步任务引用。
// ErrSyncReferenced indicates that a remote system is still referenced by tasks.
var ErrSyncReferenced = errors.New("sync remote system is referenced")

// RemoteSystem 保存一个可被多个同步任务复用的远端目标配置（SFTP 或另一套 FileBox）。
// RemoteSystem stores a remote target configuration reusable by multiple tasks (SFTP or another FileBox).
type RemoteSystem struct {
	ID                 int64  `json:"id"`
	UserID             int64  `json:"userId"`
	Name               string `json:"name"`
	Kind               string `json:"kind"`
	Host               string `json:"host"`
	URL                string `json:"url"`
	Port               int    `json:"port"`
	Username           string `json:"username"`
	AuthType           string `json:"authType"`
	AuthSecret         string `json:"-"`
	AuthPassphrase     string `json:"-"`
	HostKeyFingerprint string `json:"hostKeyFingerprint"`
	TaskCount          int64  `json:"taskCount"`
	LastTestAt         string `json:"lastTestAt"`
	LastTestResult     string `json:"lastTestResult"`
	CreatedAt          string `json:"createdAt"`
}

// SyncTask 描述一个 FileBox 与远端（SFTP/FileBox）之间的同步任务。
// SyncTask describes a FileBox-to-remote (SFTP/FileBox) synchronization task.
type SyncTask struct {
	ID             int64  `json:"id"`
	UserID         int64  `json:"userId"`
	Name           string `json:"name"`
	Direction      string `json:"direction"`
	RemoteSystemID int64  `json:"remoteSystemId"`
	SourceType     string `json:"sourceType"`
	SourcePath     string `json:"sourcePath"`
	SourceKind     string `json:"sourceKind"`
	TargetType     string `json:"targetType"`
	TargetPath     string `json:"targetPath"`
	ConflictPolicy string `json:"conflictPolicy"`
	ScheduleType   string `json:"scheduleType"`
	Cron           string `json:"cron"`
	Enabled        bool   `json:"enabled"`
	LastRunAt      string `json:"lastRunAt"`
	LastResult     string `json:"lastResult"`
	CreatedAt      string `json:"createdAt"`
}

// SyncLog 是一次同步执行的汇总和文件级详情。
// SyncLog stores one execution summary and its file-level details.
type SyncLog struct {
	ID         int64  `json:"id"`
	TaskID     int64  `json:"taskId"`
	UserID     int64  `json:"userId"`
	RunAt      string `json:"runAt"`
	FinishedAt string `json:"finishedAt"`
	Direction  string `json:"direction"`
	Result     string `json:"result"`
	Files      int64  `json:"files"`
	Bytes      int64  `json:"bytes"`
	Message    string `json:"message"`
	Detail     string `json:"detail"`
}

// migrateSyncSchema 创建同步功能所需的三张表。
// migrateSyncSchema creates the three tables used by synchronization.
func (s *Store) migrateSyncSchema() error {
	_, err := s.DB.Exec(`
CREATE TABLE IF NOT EXISTS remote_systems (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  host TEXT NOT NULL,
  port INTEGER NOT NULL DEFAULT 22,
  username TEXT NOT NULL,
  auth_type TEXT NOT NULL CHECK(auth_type IN ('password', 'key')),
  auth_secret TEXT NOT NULL,
  auth_passphrase TEXT NOT NULL DEFAULT '',
  host_key_fingerprint TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_remote_systems_user ON remote_systems(user_id, created_at DESC);
CREATE TABLE IF NOT EXISTS sync_tasks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  direction TEXT NOT NULL CHECK(direction IN ('push', 'pull')),
  remote_system_id INTEGER NOT NULL REFERENCES remote_systems(id) ON DELETE RESTRICT,
  source_type TEXT NOT NULL CHECK(source_type IN ('filebox', 'sftp')),
  source_path TEXT NOT NULL,
  target_type TEXT NOT NULL CHECK(target_type IN ('filebox', 'sftp')),
  target_path TEXT NOT NULL,
  conflict_policy TEXT NOT NULL DEFAULT 'overwrite' CHECK(conflict_policy IN ('overwrite', 'skip', 'rename')),
  schedule_type TEXT NOT NULL CHECK(schedule_type IN ('once', 'periodic')),
  cron TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  last_run_at TEXT,
  last_result TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sync_tasks_user ON sync_tasks(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_sync_tasks_schedule ON sync_tasks(enabled, schedule_type);
CREATE TABLE IF NOT EXISTS sync_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id INTEGER NOT NULL REFERENCES sync_tasks(id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  run_at TEXT NOT NULL,
  direction TEXT NOT NULL,
  result TEXT NOT NULL,
  files INTEGER NOT NULL DEFAULT 0,
  bytes INTEGER NOT NULL DEFAULT 0,
  message TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT '',
  finished_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_sync_logs_task_run ON sync_logs(task_id, run_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_sync_logs_run ON sync_logs(run_at);`)
	if err != nil {
		return err
	}
	syncLogColumns, err := tableColumns(s.DB, "sync_logs")
	if err != nil {
		return err
	}
	if !syncLogColumns["finished_at"] {
		tx, err := s.DB.Begin()
		if err != nil {
			return err
		}
		const schema = `
CREATE TABLE sync_logs_new (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id INTEGER NOT NULL REFERENCES sync_tasks(id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  run_at TEXT NOT NULL,
  direction TEXT NOT NULL,
  result TEXT NOT NULL,
  files INTEGER NOT NULL DEFAULT 0,
  bytes INTEGER NOT NULL DEFAULT 0,
  message TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT '',
  finished_at TEXT NOT NULL DEFAULT ''
);
INSERT INTO sync_logs_new(id, task_id, user_id, run_at, direction, result, files, bytes, message, detail, finished_at)
  SELECT id, task_id, user_id, run_at, direction, result, files, bytes, message, detail, '' FROM sync_logs;
DROP TABLE sync_logs;
ALTER TABLE sync_logs_new RENAME TO sync_logs;
CREATE INDEX IF NOT EXISTS idx_sync_logs_task_run ON sync_logs(task_id, run_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_sync_logs_run ON sync_logs(run_at);
`
		if _, err := tx.Exec(schema); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	columns, err := tableColumns(s.DB, "remote_systems")
	if err != nil {
		return err
	}
	if !columns["host_key_fingerprint"] {
		if _, err := s.DB.Exec("ALTER TABLE remote_systems ADD COLUMN host_key_fingerprint TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	// v013：目标系统支持另一套 FileBox（kind='filebox' 时使用 url 登录对方 HTTP API）。
	// v013: remote systems may point at another FileBox instance (kind='filebox' uses url for its HTTP API).
	if !columns["kind"] {
		if _, err := s.DB.Exec("ALTER TABLE remote_systems ADD COLUMN kind TEXT NOT NULL DEFAULT 'sftp'"); err != nil {
			return err
		}
	}
	if !columns["url"] {
		if _, err := s.DB.Exec("ALTER TABLE remote_systems ADD COLUMN url TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	// v014（#5）：记录最近一次连通性测试的结果与时间（仅 ok/failure，不保存错误详情）。
	// v014 (#5): persist the latest connectivity-test outcome and time (ok/failure only).
	if !columns["last_test_at"] {
		if _, err := s.DB.Exec("ALTER TABLE remote_systems ADD COLUMN last_test_at TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	if !columns["last_test_result"] {
		if _, err := s.DB.Exec("ALTER TABLE remote_systems ADD COLUMN last_test_result TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	syncColumns, err := tableColumns(s.DB, "sync_tasks")
	if err != nil {
		return err
	}
	// v013：源路径可为单个文件（source_kind='file'），用于消除同名文件/目录歧义。
	// v013: the source path may be a single file (source_kind='file') to disambiguate same-name files and directories.
	if !syncColumns["source_kind"] {
		if _, err := s.DB.Exec("ALTER TABLE sync_tasks ADD COLUMN source_kind TEXT NOT NULL DEFAULT 'directory'"); err != nil {
			return err
		}
	}
	return nil
}

func scanRemoteSystem(row interface{ Scan(...any) error }) (RemoteSystem, error) {
	var item RemoteSystem
	var err error
	err = row.Scan(&item.ID, &item.UserID, &item.Name, &item.Kind, &item.Host, &item.URL, &item.Port, &item.Username, &item.AuthType, &item.AuthSecret, &item.AuthPassphrase, &item.HostKeyFingerprint, &item.LastTestAt, &item.LastTestResult, &item.CreatedAt, &item.TaskCount)
	if errors.Is(err, sql.ErrNoRows) {
		return RemoteSystem{}, ErrNotFound
	}
	return item, err
}

const remoteSystemColumns = `r.id, r.user_id, r.name, r.kind, r.host, COALESCE(r.url, ''), r.port, r.username, r.auth_type,
 r.auth_secret, COALESCE(r.auth_passphrase, ''), COALESCE(r.host_key_fingerprint, ''), COALESCE(r.last_test_at, ''), COALESCE(r.last_test_result, ''),
 r.created_at,
 (SELECT COUNT(t.id) FROM sync_tasks t WHERE t.remote_system_id = r.id)`

// CreateRemoteSystem 写入目标系统配置。
// CreateRemoteSystem inserts a remote system configuration.
func (s *Store) CreateRemoteSystem(ctx context.Context, item RemoteSystem) (RemoteSystem, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	kind := item.Kind
	if kind == "" {
		kind = "sftp"
	}
	result, err := s.DB.ExecContext(ctx, `INSERT INTO remote_systems(user_id, name, kind, host, url, port, username, auth_type, auth_secret, auth_passphrase, host_key_fingerprint, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, item.UserID, item.Name, kind, item.Host, item.URL, item.Port, item.Username, item.AuthType, item.AuthSecret, item.AuthPassphrase, item.HostKeyFingerprint, now)
	if err != nil {
		return RemoteSystem{}, err
	}
	item.ID, err = result.LastInsertId()
	if err != nil {
		return RemoteSystem{}, err
	}
	item.CreatedAt = now
	return item, nil
}

// GetRemoteSystem 按用户范围读取目标系统；管理员可读取全部系统。
// GetRemoteSystem loads a scoped remote system; administrators may read all systems.
func (s *Store) GetRemoteSystem(ctx context.Context, id, userID int64, admin bool) (RemoteSystem, error) {
	where := "r.id = ? AND r.user_id = ?"
	args := []any{id, userID}
	if admin {
		where = "r.id = ?"
		args = []any{id}
	}
	return scanRemoteSystem(s.DB.QueryRowContext(ctx, "SELECT "+remoteSystemColumns+" FROM remote_systems r WHERE "+where, args...))
}

// ListRemoteSystems 返回用户的目标系统，管理员可查看全部。
// ListRemoteSystems lists a user's remote systems; administrators may list all systems.
func (s *Store) ListRemoteSystems(ctx context.Context, userID int64, admin bool) ([]RemoteSystem, error) {
	where := "r.user_id = ?"
	args := []any{userID}
	if admin {
		where, args = "1 = 1", nil
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT "+remoteSystemColumns+" FROM remote_systems r WHERE "+where+" ORDER BY r.created_at DESC, r.id DESC", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]RemoteSystem, 0)
	for rows.Next() {
		item, err := scanRemoteSystem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// UpdateRemoteSystem 更新目标系统及其加密凭据。
// UpdateRemoteSystem updates a remote system and its encrypted credentials.
func (s *Store) UpdateRemoteSystem(ctx context.Context, item RemoteSystem, userID int64, admin bool) error {
	where := "id = ? AND user_id = ?"
	args := []any{item.ID, userID}
	if admin {
		where, args = "id = ?", []any{item.ID}
	}
	kind := item.Kind
	if kind == "" {
		kind = "sftp"
	}
	values := []any{item.Name, kind, item.Host, item.URL, item.Port, item.Username, item.AuthType, item.AuthSecret, item.AuthPassphrase, item.HostKeyFingerprint}
	values = append(values, args...)
	result, err := s.DB.ExecContext(ctx, "UPDATE remote_systems SET name = ?, kind = ?, host = ?, url = ?, port = ?, username = ?, auth_type = ?, auth_secret = ?, auth_passphrase = ?, host_key_fingerprint = ? WHERE "+where,
		values...)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrNotFound
	}
	return nil
}

// UpdateRemoteSystemTest 持久化最近一次连通性测试结果（#5），仅记录 ok/failure 与测试时间。
// UpdateRemoteSystemTest persists the latest connectivity-test outcome (#5); only the
// ok/failure result and the tested time are stored.
func (s *Store) UpdateRemoteSystemTest(ctx context.Context, id, userID int64, admin bool, testedAt, result string) error {
	where := "id = ? AND user_id = ?"
	args := []any{testedAt, result, id, userID}
	if admin {
		where, args = "id = ?", []any{testedAt, result, id}
	}
	exec, err := s.DB.ExecContext(ctx, "UPDATE remote_systems SET last_test_at = ?, last_test_result = ? WHERE "+where, args...)
	if err != nil {
		return err
	}
	count, err := exec.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrNotFound
	}
	return nil
}

// DeleteRemoteSystem 删除未被任务引用的目标系统，并返回引用数。
// DeleteRemoteSystem deletes an unused remote system and returns its reference count.
func (s *Store) DeleteRemoteSystem(ctx context.Context, id, userID int64, admin bool) (int64, error) {
	item, err := s.GetRemoteSystem(ctx, id, userID, admin)
	if err != nil {
		return 0, err
	}
	var references int64
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(id) FROM sync_tasks WHERE remote_system_id = ?", item.ID).Scan(&references); err != nil {
		return 0, err
	}
	if references > 0 {
		return references, ErrSyncReferenced
	}
	result, err := s.DB.ExecContext(ctx, "DELETE FROM remote_systems WHERE id = ?", item.ID)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if count != 1 {
		return 0, ErrNotFound
	}
	return 0, nil
}

func scanSyncTask(row interface{ Scan(...any) error }) (SyncTask, error) {
	var item SyncTask
	var enabled int
	err := row.Scan(&item.ID, &item.UserID, &item.Name, &item.Direction, &item.RemoteSystemID, &item.SourceType, &item.SourcePath, &item.SourceKind, &item.TargetType, &item.TargetPath, &item.ConflictPolicy, &item.ScheduleType, &item.Cron, &enabled, &item.LastRunAt, &item.LastResult, &item.CreatedAt)
	item.Enabled = enabled != 0
	if errors.Is(err, sql.ErrNoRows) {
		return SyncTask{}, ErrNotFound
	}
	return item, err
}

const syncTaskColumns = `id, user_id, name, direction, remote_system_id, source_type, source_path,
 COALESCE(source_kind, 'directory'), target_type, target_path, conflict_policy, schedule_type, cron, enabled,
 COALESCE(last_run_at, ''), COALESCE(last_result, ''), created_at`

// CreateSyncTask 创建同步任务，并校验目标系统归属。
// CreateSyncTask creates a task and validates ownership of its remote system.
func (s *Store) CreateSyncTask(ctx context.Context, item SyncTask, admin bool) (SyncTask, error) {
	var owner int64
	if err := s.DB.QueryRowContext(ctx, "SELECT user_id FROM remote_systems WHERE id = ?", item.RemoteSystemID).Scan(&owner); errors.Is(err, sql.ErrNoRows) {
		return SyncTask{}, ErrNotFound
	} else if err != nil {
		return SyncTask{}, err
	} else if owner != item.UserID && !admin {
		return SyncTask{}, ErrNotFound
	} else if owner != item.UserID {
		return SyncTask{}, ErrNotFound
	}
	now := time.Now().UTC().Format(time.RFC3339)
	sourceKind := item.SourceKind
	if sourceKind == "" {
		sourceKind = "directory"
	}
	result, err := s.DB.ExecContext(ctx, `INSERT INTO sync_tasks(user_id, name, direction, remote_system_id, source_type, source_path, source_kind, target_type, target_path, conflict_policy, schedule_type, cron, enabled, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, item.UserID, item.Name, item.Direction, item.RemoteSystemID, item.SourceType, item.SourcePath, sourceKind, item.TargetType, item.TargetPath, item.ConflictPolicy, item.ScheduleType, item.Cron, boolInt(item.Enabled), now)
	if err != nil {
		return SyncTask{}, err
	}
	item.ID, err = result.LastInsertId()
	if err != nil {
		return SyncTask{}, err
	}
	item.CreatedAt = now
	return item, nil
}

// GetSyncTask 按归属读取任务；越权由 ErrNotFound 统一表示。
// GetSyncTask loads a task by ownership; unauthorized access is represented as ErrNotFound.
func (s *Store) GetSyncTask(ctx context.Context, id, userID int64, admin bool) (SyncTask, error) {
	where := "id = ? AND user_id = ?"
	args := []any{id, userID}
	if admin {
		where, args = "id = ?", []any{id}
	}
	return scanSyncTask(s.DB.QueryRowContext(ctx, "SELECT "+syncTaskColumns+" FROM sync_tasks WHERE "+where, args...))
}

// ListSyncTasks 返回任务列表。
// ListSyncTasks lists scoped synchronization tasks.
func (s *Store) ListSyncTasks(ctx context.Context, userID int64, admin bool) ([]SyncTask, error) {
	where := "user_id = ?"
	args := []any{userID}
	if admin {
		where, args = "1 = 1", nil
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT "+syncTaskColumns+" FROM sync_tasks WHERE "+where+" ORDER BY created_at DESC, id DESC", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]SyncTask, 0)
	for rows.Next() {
		item, err := scanSyncTask(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// UpdateSyncTask 更新任务配置，并保持任务与目标系统同属一个用户。
// UpdateSyncTask updates a task while keeping its remote system in the same ownership boundary.
func (s *Store) UpdateSyncTask(ctx context.Context, item SyncTask, userID int64, admin bool) error {
	if _, err := s.GetRemoteSystem(ctx, item.RemoteSystemID, item.UserID, false); err != nil {
		return ErrNotFound
	}
	where := "id = ? AND user_id = ?"
	args := []any{item.ID, userID}
	if admin {
		where, args = "id = ?", []any{item.ID}
	}
	sourceKind := item.SourceKind
	if sourceKind == "" {
		sourceKind = "directory"
	}
	values := []any{item.Name, item.Direction, item.RemoteSystemID, item.SourceType, item.SourcePath, sourceKind, item.TargetType, item.TargetPath, item.ConflictPolicy, item.ScheduleType, item.Cron, boolInt(item.Enabled)}
	values = append(values, args...)
	result, err := s.DB.ExecContext(ctx, `UPDATE sync_tasks SET name = ?, direction = ?, remote_system_id = ?, source_type = ?, source_path = ?, source_kind = ?, target_type = ?, target_path = ?, conflict_policy = ?, schedule_type = ?, cron = ?, enabled = ? WHERE `+where,
		values...)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrNotFound
	}
	return nil
}

// DeleteSyncTask 删除任务，日志通过外键级联删除。
// DeleteSyncTask deletes a task; its logs are removed by the foreign key cascade.
func (s *Store) DeleteSyncTask(ctx context.Context, id, userID int64, admin bool) error {
	where := "id = ? AND user_id = ?"
	args := []any{id, userID}
	if admin {
		where, args = "id = ?", []any{id}
	}
	result, err := s.DB.ExecContext(ctx, "DELETE FROM sync_tasks WHERE "+where, args...)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrNotFound
	}
	return nil
}

// ListScheduledSyncTasks 返回调度器扫描所需的周期任务。
// ListScheduledSyncTasks returns periodic tasks for the scheduler scan.
func (s *Store) ListScheduledSyncTasks(ctx context.Context) ([]SyncTask, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT sync_tasks.id, sync_tasks.user_id, sync_tasks.name, sync_tasks.direction, sync_tasks.remote_system_id,
 sync_tasks.source_type, sync_tasks.source_path, COALESCE(sync_tasks.source_kind, 'directory'), sync_tasks.target_type, sync_tasks.target_path, sync_tasks.conflict_policy,
 sync_tasks.schedule_type, sync_tasks.cron, sync_tasks.enabled, COALESCE(sync_tasks.last_run_at, ''),
 COALESCE(sync_tasks.last_result, ''), sync_tasks.created_at
 FROM sync_tasks JOIN users ON users.id = sync_tasks.user_id
 WHERE sync_tasks.enabled = 1 AND sync_tasks.schedule_type = 'periodic' AND users.disabled = 0
 ORDER BY sync_tasks.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]SyncTask, 0)
	for rows.Next() {
		item, err := scanSyncTask(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// CreateSyncLog 写入一次执行日志。
// CreateSyncLog inserts one execution log.
func (s *Store) CreateSyncLog(ctx context.Context, item SyncLog) (SyncLog, error) {
	if item.RunAt == "" {
		item.RunAt = time.Now().UTC().Format(time.RFC3339)
	}
	result, err := s.DB.ExecContext(ctx, `INSERT INTO sync_logs(task_id, user_id, run_at, finished_at, direction, result, files, bytes, message, detail) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, item.TaskID, item.UserID, item.RunAt, item.FinishedAt, item.Direction, item.Result, item.Files, item.Bytes, item.Message, item.Detail)
	if err != nil {
		return SyncLog{}, err
	}
	item.ID, err = result.LastInsertId()
	return item, err
}

// UpdateSyncLogResult completes one execution log in place.
func (s *Store) UpdateSyncLogResult(ctx context.Context, logID int64, result, finishedAt string, files, bytes int64, message, detail string) error {
	updated, err := s.DB.ExecContext(ctx, "UPDATE sync_logs SET result = ?, finished_at = ?, files = ?, bytes = ?, message = ?, detail = ? WHERE id = ?", result, finishedAt, files, bytes, message, detail, logID)
	if err != nil {
		return err
	}
	affected, err := updated.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateSyncTaskResult 更新任务最近执行状态。
// UpdateSyncTaskResult records the task's latest execution status.
func (s *Store) UpdateSyncTaskResult(ctx context.Context, taskID int64, runAt, result string) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE sync_tasks SET last_run_at = ?, last_result = ? WHERE id = ?", runAt, result, taskID)
	return err
}

func scanSyncLog(row interface{ Scan(...any) error }) (SyncLog, error) {
	var item SyncLog
	err := row.Scan(&item.ID, &item.TaskID, &item.UserID, &item.RunAt, &item.FinishedAt, &item.Direction, &item.Result, &item.Files, &item.Bytes, &item.Message, &item.Detail)
	if errors.Is(err, sql.ErrNoRows) {
		return SyncLog{}, ErrNotFound
	}
	return item, err
}

// ListSyncLogs 返回任务日志并分页。
// ListSyncLogs returns paginated logs for one task.
func (s *Store) ListSyncLogs(ctx context.Context, taskID int64, page, pageSize int) ([]SyncLog, int, error) {
	var total int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(id) FROM sync_logs WHERE task_id = ?", taskID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT id, task_id, user_id, run_at, COALESCE(finished_at, ''), direction, result, files, bytes, message, detail FROM sync_logs WHERE task_id = ? ORDER BY run_at DESC, id DESC LIMIT ? OFFSET ?", taskID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]SyncLog, 0)
	for rows.Next() {
		item, err := scanSyncLog(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

// PruneSyncLogs 删除超过留存天数的同步日志。
// PruneSyncLogs removes synchronization logs older than the retention period.
func (s *Store) PruneSyncLogs(ctx context.Context, retentionDays int) (int64, error) {
	if retentionDays < 0 {
		return 0, fmt.Errorf("invalid sync log retention")
	}
	if retentionDays == 0 {
		return 0, nil
	}
	if retentionDays > constMaxRetentionDays {
		return 0, fmt.Errorf("sync log retention exceeds maximum")
	}
	cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour).Format(time.RFC3339)
	result, err := s.DB.ExecContext(ctx, "DELETE FROM sync_logs WHERE run_at < ?", cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// ListReadyFilesUnder 返回用户源目录下的 ready 文件，支持目录或单文件路径。
// ListReadyFilesUnder lists ready files below a user's directory or one file path.
func (s *Store) ListReadyFilesUnder(ctx context.Context, userID int64, sourcePath string) ([]File, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at, COALESCE(deleted_at, '')
FROM files WHERE user_id = ? AND status = 'ready' ORDER BY storage_path LIMIT ?`, userID, 10000)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	root := filepath.ToSlash(filepath.Join("files", fmt.Sprint(userID))) + "/"
	sourcePath = strings.Trim(sourcePath, "/")
	items := make([]File, 0)
	for rows.Next() {
		var item File
		if err := rows.Scan(&item.ID, &item.UserID, &item.Name, &item.StoredName, &item.Size, &item.Mime, &item.SHA256, &item.MD5, &item.Status, &item.StoragePath, &item.CreatedAt, &item.DeletedAt); err != nil {
			return nil, err
		}
		relative := filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(item.StoragePath), root))
		if sourcePath == "" || relative == sourcePath || strings.HasPrefix(relative, sourcePath+"/") {
			items = append(items, item)
		}
	}
	return items, rows.Err()
}
