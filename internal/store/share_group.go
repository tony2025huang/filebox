package store

// 批量分享聚合模型（v013 #7）：一个 share_groups 记录 + 若干 share_group_files 成员，
// 计数按聚合整体计（单文件下载 1 次、ZIP 下载 1 次），已删除文件在列表中隐藏。
// Batch-share aggregate model (v013 #7): one share_groups row plus share_group_files members.
// Counting is group-wide (single-file download = 1, ZIP download = 1); deleted files are hidden from the list.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ShareGroup 是一个聚合分享链接（统一 token，多个文件）。
// ShareGroup is one aggregate share link (single token, multiple files).
type ShareGroup struct {
	ID            int64  `json:"id"`
	Token         string `json:"token"`
	CreatedBy     int64  `json:"createdBy"`
	ExpiresAt     string `json:"expiresAt"`
	DownloadCount int64  `json:"downloadCount"`
	MaxDownloads  int    `json:"maxDownloads"`
	RevokedAt     string `json:"revokedAt,omitempty"`
	CreatedAt     string `json:"createdAt"`
	FileCount     int64  `json:"fileCount"`
}

// ShareGroupFile 是聚合分享的一个成员文件。
// ShareGroupFile is one member file of an aggregate share.
type ShareGroupFile struct {
	ID           int64  `json:"id"`
	GroupID      int64  `json:"groupId"`
	FileID       int64  `json:"fileId"`
	DisplayOrder int    `json:"displayOrder"`
	CreatedAt    string `json:"createdAt"`
	File         File   `json:"file"`
}

// migrateShareGroupsSchema 创建聚合分享表。
// migrateShareGroupsSchema creates the aggregate share tables.
func (s *Store) migrateShareGroupsSchema() error {
	if _, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS share_groups (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  token TEXT NOT NULL UNIQUE,
  created_by INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  download_count INTEGER NOT NULL DEFAULT 0,
  max_downloads INTEGER NOT NULL DEFAULT 0,
  last_download_at TEXT,
  revoked_at TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_share_groups_created_by ON share_groups(created_by, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_share_groups_expires ON share_groups(expires_at);
CREATE TABLE IF NOT EXISTS share_group_files (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  group_id INTEGER NOT NULL REFERENCES share_groups(id) ON DELETE CASCADE,
  file_id INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
  display_order INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);
	CREATE INDEX IF NOT EXISTS idx_share_group_files_group ON share_group_files(group_id, display_order, id);`); err != nil {
		return err
	}
	columns, err := tableColumns(s.DB, "share_groups")
	if err != nil {
		return err
	}
	if !columns["last_download_at"] {
		_, err = s.DB.Exec("ALTER TABLE share_groups ADD COLUMN last_download_at TEXT")
	}
	return err
}

func scanShareGroup(row interface{ Scan(...any) error }) (ShareGroup, error) {
	var item ShareGroup
	err := row.Scan(&item.ID, &item.Token, &item.CreatedBy, &item.ExpiresAt, &item.DownloadCount, &item.MaxDownloads, &item.RevokedAt, &item.CreatedAt, &item.FileCount)
	if errors.Is(err, sql.ErrNoRows) {
		return ShareGroup{}, ErrNotFound
	}
	return item, err
}

// CreateShareGroup 在一个事务中校验整批文件（归属 + ready）并创建聚合分享，任一文件非法则整体回滚。
// CreateShareGroup validates the whole batch (ownership + ready) in one transaction and creates the
// aggregate share; any invalid file rolls the entire batch back.
func (s *Store) CreateShareGroup(ctx context.Context, createdBy int64, token string, fileIDs []int64, expiresAt string, maxDownloads int) (ShareGroup, []ShareGroupFile, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return ShareGroup{}, nil, err
	}
	defer tx.Rollback()
	if maxDownloads < 0 || maxDownloads > 100000 {
		return ShareGroup{}, nil, fmt.Errorf("invalid max downloads")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	seen := make(map[int64]bool, len(fileIDs))
	ordered := make([]int64, 0, len(fileIDs))
	for _, id := range fileIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		var owner int64
		if err := tx.QueryRowContext(ctx, "SELECT user_id FROM files WHERE id = ? AND status = 'ready'", id).Scan(&owner); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ShareGroup{}, nil, ErrNotFound
			}
			return ShareGroup{}, nil, err
		}
		if owner != createdBy {
			return ShareGroup{}, nil, ErrNotFound
		}
		ordered = append(ordered, id)
	}
	if len(ordered) == 0 {
		return ShareGroup{}, nil, fmt.Errorf("empty share group")
	}
	if len(ordered) > 500 {
		return ShareGroup{}, nil, fmt.Errorf("too many files")
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO share_groups(token, created_by, expires_at, download_count, max_downloads, created_at)
VALUES(?, ?, ?, 0, ?, ?)`, token, createdBy, expiresAt, maxDownloads, now)
	if err != nil {
		return ShareGroup{}, nil, err
	}
	groupID, err := result.LastInsertId()
	if err != nil {
		return ShareGroup{}, nil, err
	}
	files := make([]ShareGroupFile, 0, len(ordered))
	for index, fileID := range ordered {
		insert, err := tx.ExecContext(ctx, `INSERT INTO share_group_files(group_id, file_id, display_order, created_at) VALUES(?, ?, ?, ?)`, groupID, fileID, index, now)
		if err != nil {
			return ShareGroup{}, nil, err
		}
		id, err := insert.LastInsertId()
		if err != nil {
			return ShareGroup{}, nil, err
		}
		files = append(files, ShareGroupFile{ID: id, GroupID: groupID, FileID: fileID, DisplayOrder: index, CreatedAt: now})
	}
	if err := tx.Commit(); err != nil {
		return ShareGroup{}, nil, err
	}
	return ShareGroup{ID: groupID, Token: token, CreatedBy: createdBy, ExpiresAt: expiresAt, MaxDownloads: maxDownloads, CreatedAt: now, FileCount: int64(len(files))}, files, nil
}

// GetShareGroupByToken 按 token 读取未撤销的聚合分享。
// GetShareGroupByToken loads a non-revoked aggregate share by token.
func (s *Store) GetShareGroupByToken(ctx context.Context, token string) (ShareGroup, error) {
	return scanShareGroup(s.DB.QueryRowContext(ctx, `SELECT sg.id, sg.token, sg.created_by, sg.expires_at, sg.download_count, sg.max_downloads, COALESCE(sg.revoked_at, ''), sg.created_at,
(SELECT COUNT(*) FROM share_group_files sgf WHERE sgf.group_id = sg.id AND EXISTS (SELECT 1 FROM files f WHERE f.id = sgf.file_id AND f.status = 'ready'))
FROM share_groups sg WHERE sg.token = ? AND sg.revoked_at IS NULL`, token))
}

// GetShareGroupByTokenIncludingRevoked 含已撤销记录读取，供管理端与错误分类使用。
// GetShareGroupByTokenIncludingRevoked includes revoked records for management and error classification.
func (s *Store) GetShareGroupByTokenIncludingRevoked(ctx context.Context, token string) (ShareGroup, error) {
	return scanShareGroup(s.DB.QueryRowContext(ctx, `SELECT sg.id, sg.token, sg.created_by, sg.expires_at, sg.download_count, sg.max_downloads, COALESCE(sg.revoked_at, ''), sg.created_at,
(SELECT COUNT(*) FROM share_group_files sgf WHERE sgf.group_id = sg.id AND EXISTS (SELECT 1 FROM files f WHERE f.id = sgf.file_id AND f.status = 'ready'))
FROM share_groups sg WHERE sg.token = ?`, token))
}

// ListShareGroupsByOwner 列出创建者的聚合分享（管理员可查看全部）。
// ListShareGroupsByOwner lists aggregate shares owned by a user (admins may list all).
func (s *Store) ListShareGroupsByOwner(ctx context.Context, userID int64, admin bool) ([]ShareGroup, error) {
	query := `SELECT sg.id, sg.token, sg.created_by, sg.expires_at, sg.download_count, sg.max_downloads, COALESCE(sg.revoked_at, ''), sg.created_at,
(SELECT COUNT(*) FROM share_group_files sgf WHERE sgf.group_id = sg.id AND EXISTS (SELECT 1 FROM files f WHERE f.id = sgf.file_id AND f.status = 'ready'))
FROM share_groups sg`
	args := []any{}
	if !admin {
		query += " WHERE sg.created_by = ?"
		args = append(args, userID)
	}
	query += " ORDER BY sg.created_at DESC, sg.id DESC"
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ShareGroup, 0)
	for rows.Next() {
		item, err := scanShareGroup(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ListShareGroupFiles 列出聚合分享的成员文件（隐藏已删除文件）。
// ListShareGroupFiles lists member files, hiding deleted ones.
func (s *Store) ListShareGroupFiles(ctx context.Context, groupID int64) ([]ShareGroupFile, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT sgf.id, sgf.group_id, sgf.file_id, sgf.display_order, sgf.created_at,
f.id, f.user_id, f.name, f.stored_name, f.size, f.mime, f.sha256, f.md5, f.status, f.storage_path, f.created_at, COALESCE(f.deleted_at, '')
FROM share_group_files sgf JOIN files f ON f.id = sgf.file_id
WHERE sgf.group_id = ? AND f.status = 'ready'
ORDER BY sgf.display_order, sgf.id`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ShareGroupFile, 0)
	for rows.Next() {
		var item ShareGroupFile
		if err := rows.Scan(&item.ID, &item.GroupID, &item.FileID, &item.DisplayOrder, &item.CreatedAt, &item.File.ID, &item.File.UserID, &item.File.Name, &item.File.StoredName, &item.File.Size, &item.File.Mime, &item.File.SHA256, &item.File.MD5, &item.File.Status, &item.File.StoragePath, &item.File.CreatedAt, &item.File.DeletedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// RevokeShareGroup 撤销聚合分享（管理员或创建者）。
// RevokeShareGroup revokes an aggregate share (owner or admin).
func (s *Store) RevokeShareGroup(ctx context.Context, token string, ownerID int64, admin bool) error {
	query := "UPDATE share_groups SET revoked_at = ? WHERE token = ?"
	args := []any{time.Now().UTC().Format(time.RFC3339), token}
	if !admin {
		query += " AND created_by = ?"
		args = append(args, ownerID)
	}
	result, err := s.DB.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return ErrNotFound
	}
	return err
}

// IncrementShareGroupDownloads 原子消耗一次聚合分享下载次数，返回是否允许。
// IncrementShareGroupDownloads atomically consumes one aggregate download slot and reports whether it is allowed.
// Range requests may share one slot within the rolling 60-second window when windowMode is true.
func (s *Store) IncrementShareGroupDownloads(ctx context.Context, token string, maxDownloads int, windowMode bool) (bool, error) {
	_ = maxDownloads
	now := time.Now().UTC()
	nowValue := now.Format(time.RFC3339)
	if windowMode {
		windowStart := now.Add(-60 * time.Second).Format(time.RFC3339)
		result, err := s.DB.ExecContext(ctx, `UPDATE share_groups SET
  download_count = CASE WHEN last_download_at IS NOT NULL AND julianday(last_download_at) > julianday(?) THEN download_count ELSE download_count + 1 END,
  last_download_at = ?
WHERE token = ? AND revoked_at IS NULL AND expires_at > ?
  AND (max_downloads = 0 OR download_count < max_downloads
    OR (last_download_at IS NOT NULL AND julianday(last_download_at) > julianday(?)))`, windowStart, nowValue, token, nowValue, windowStart)
		if err != nil {
			return false, err
		}
		count, err := result.RowsAffected()
		return count == 1, err
	}
	result, err := s.DB.ExecContext(ctx, `UPDATE share_groups SET download_count = download_count + 1, last_download_at = NULL
WHERE token = ? AND revoked_at IS NULL AND expires_at > ? AND (max_downloads = 0 OR download_count < max_downloads)`, token, nowValue)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return count == 1, nil
}

// UpdateShareGroupExpiry 延长聚合分享有效期，且新截止时间不会缩短原有截止时间（#6）。
// UpdateShareGroupExpiry extends an aggregate share without moving its expiry backwards (#6).
func (s *Store) UpdateShareGroupExpiry(ctx context.Context, token string, newExpiresAt time.Time, ownerID int64) error {
	value := newExpiresAt.UTC().Format(time.RFC3339)
	result, err := s.DB.ExecContext(ctx, `UPDATE share_groups SET expires_at = CASE WHEN expires_at > ? THEN expires_at ELSE ? END
WHERE token = ? AND created_by = ? AND revoked_at IS NULL`, value, value, token, ownerID)
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

// UpdateShareGroupMaxDownloads 提高聚合分享下载上限，拒绝降低上限（0 表示不限，允许从有限提升为不限）（#6）。
// UpdateShareGroupMaxDownloads raises an aggregate share's download limit and rejects decreases;
// 0 means unlimited and a finite limit may be raised to unlimited (#6).
func (s *Store) UpdateShareGroupMaxDownloads(ctx context.Context, token string, newMax int, ownerID int64) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE share_groups SET max_downloads = ?
WHERE token = ? AND created_by = ? AND revoked_at IS NULL AND max_downloads >= 0 AND (? = 0 OR ? > max_downloads)`, newMax, token, ownerID, newMax, newMax)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		current, lookupErr := s.GetShareGroupByTokenIncludingRevoked(ctx, token)
		if lookupErr != nil || current.CreatedBy != ownerID || current.RevokedAt != "" {
			return ErrNotFound
		}
		return ErrConflict
	}
	return nil
}

// AddShareGroupFiles 向聚合分享追加成员文件（校验归属与 ready，整体去重，总数上限 500）。
// AddShareGroupFiles appends member files to an aggregate share (ownership + ready checked, deduped, cap 500).
func (s *Store) AddShareGroupFiles(ctx context.Context, groupID int64, fileIDs []int64) ([]ShareGroupFile, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var current int64
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(id) FROM share_group_files WHERE group_id = ?", groupID).Scan(&current); err != nil {
		return nil, err
	}
	if current >= 500 {
		return nil, fmt.Errorf("share group too large")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	seen := make(map[int64]bool, len(fileIDs))
	added := make([]ShareGroupFile, 0, len(fileIDs))
	nextOrder := current
	for _, id := range fileIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		var exists int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(id) FROM share_group_files WHERE group_id = ? AND file_id = ?", groupID, id).Scan(&exists); err != nil {
			return nil, err
		}
		if exists > 0 {
			continue
		}
		var owner int64
		if err := tx.QueryRowContext(ctx, "SELECT user_id FROM files WHERE id = ? AND status = 'ready'", id).Scan(&owner); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		var groupOwner int64
		if err := tx.QueryRowContext(ctx, "SELECT created_by FROM share_groups WHERE id = ?", groupID).Scan(&groupOwner); err != nil {
			return nil, err
		}
		if owner != groupOwner {
			return nil, ErrNotFound
		}
		if current+int64(len(added)) >= 500 {
			return nil, fmt.Errorf("share group too large")
		}
		result, err := tx.ExecContext(ctx, "INSERT INTO share_group_files(group_id, file_id, display_order, created_at) VALUES(?, ?, ?, ?)", groupID, id, nextOrder+int64(len(added)), now)
		if err != nil {
			return nil, err
		}
		rowID, err := result.LastInsertId()
		if err != nil {
			return nil, err
		}
		added = append(added, ShareGroupFile{ID: rowID, GroupID: groupID, FileID: id, DisplayOrder: int(nextOrder + int64(len(added)) - 1), CreatedAt: now})
	}
	if len(added) == 0 {
		return nil, ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return added, nil
}

// RemoveShareGroupFile 从聚合分享移除一个成员文件。
// RemoveShareGroupFile removes one member file from an aggregate share.
func (s *Store) RemoveShareGroupFile(ctx context.Context, groupID, fileID int64) error {
	result, err := s.DB.ExecContext(ctx, "DELETE FROM share_group_files WHERE group_id = ? AND file_id = ?", groupID, fileID)
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

// UpdateShareGroupAttributes 编辑聚合分享的到期时间与下载上限（到期须晚于当前时间，上限不得低于已用次数）。
// UpdateShareGroupAttributes edits an aggregate share's expiry and download limit (expiry must be in the future,
// the limit cannot be lower than the current download count).
func (s *Store) UpdateShareGroupAttributes(ctx context.Context, token string, ownerID int64, expiresAt string, maxDownloads int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.DB.ExecContext(ctx, `UPDATE share_groups SET expires_at = ?, max_downloads = ?
WHERE token = ? AND created_by = ? AND revoked_at IS NULL AND ? > ? AND (? = 0 OR ? >= download_count)`,
		expiresAt, maxDownloads, token, ownerID, expiresAt, now, maxDownloads, maxDownloads)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		current, lookupErr := s.GetShareGroupByTokenIncludingRevoked(ctx, token)
		if lookupErr != nil || current.CreatedBy != ownerID || current.RevokedAt != "" {
			return ErrNotFound
		}
		return ErrConflict
	}
	return nil
}
