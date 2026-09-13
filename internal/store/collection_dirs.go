package store

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
)

// CollectionDirName 生成收集的落盘目录名：父目录固定为 ASCII 的 collections，子目录保留用户语言的
// 收集名并拼接 token 前 8 位保证唯一（v030 #7）。
// CollectionDirName builds the collection storage directory name. The parent directory stays ASCII
// ("collections") for scripts, while the child keeps the user-language name plus the token prefix.
func CollectionDirName(name, token string) string {
	safe := sanitizeCollectionDirSegment(name)
	if safe == "" {
		safe = "collection"
	}
	if prefix := tokenPrefix(token); prefix != "" {
		return safe + "-" + prefix
	}
	return safe
}

// tokenPrefix 返回 token 的前 8 位，作为收集目录的唯一后缀。
// tokenPrefix returns the first 8 characters of a collection token used as the unique suffix.
func tokenPrefix(token string) string {
	if len(token) > 8 {
		return token[:8]
	}
	return token
}

// sanitizeCollectionDirSegment 剔除文件系统不安全字符（含路径分隔符与 Windows 保留字符），
// 压缩空白，去掉首尾的点与横线，并规避 Windows 保留设备名。
// sanitizeCollectionDirSegment strips filesystem-unsafe characters (separators, Windows reserved
// characters), collapses whitespace, trims leading/trailing dots and dashes, and avoids reserved
// Windows device names.
func sanitizeCollectionDirSegment(name string) string {
	var b strings.Builder
	pendingDash := false
	for _, r := range strings.TrimSpace(name) {
		switch {
		case r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|':
			continue
		case r < 0x20 || r == 0x7f:
			continue
		case r == ' ' || r == '\t':
			pendingDash = true
			continue
		}
		if pendingDash && b.Len() > 0 {
			b.WriteRune('-')
		}
		pendingDash = false
		b.WriteRune(r)
	}
	out := strings.Trim(b.String(), "-. ")
	if runes := []rune(out); len(runes) > 60 {
		out = strings.Trim(string(runes[:60]), "-. ")
	}
	if out == "" {
		return ""
	}
	base := strings.ToUpper(out)
	if idx := strings.Index(base, "."); idx >= 0 {
		base = base[:idx]
	}
	switch base {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		out = "c-" + out
	}
	return out
}

// SetFolderDisplayName 只改目录记录的显示名，落盘路径保持不变（收集目录用 token 后缀保证唯一，
// 但界面上应显示用户语言的收集名）。
// SetFolderDisplayName changes only the folder's display name while keeping its path, so a collection
// directory can carry a unique token suffix on disk but the user-language name in the UI.
func (s *Store) SetFolderDisplayName(ctx context.Context, userID int64, path, name string) error {
	if name == "" || path == "" {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, "UPDATE folders SET name = ? WHERE user_id = ? AND path = ?", name, userID, path)
	return err
}

// CollectionDirMigration 描述一个收集从旧布局（uploads/<token>）到新布局
// （collections/<集合名>-<token 前 8 位>）的迁移计划。
// CollectionDirMigration describes the move of one collection from the legacy uploads/<token>
// layout to collections/<name>-<token8>.
type CollectionDirMigration struct {
	CollectionID  int64
	OwnerID       int64
	Token         string
	Name          string
	OldFolderPath string
	NewFolderPath string
}

// ListCollectionDirMigrations 列出所有收集的目录迁移计划（含已经处于新布局的收集，改写是幂等的）。
// ListCollectionDirMigrations lists the planned directory move for every collection; rewriting is
// idempotent, so collections already on the new layout are harmless.
func (s *Store) ListCollectionDirMigrations(ctx context.Context) ([]CollectionDirMigration, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id, created_by, token, COALESCE(name, '') FROM upload_collections ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	migrations := make([]CollectionDirMigration, 0)
	for rows.Next() {
		var m CollectionDirMigration
		if err := rows.Scan(&m.CollectionID, &m.OwnerID, &m.Token, &m.Name); err != nil {
			return nil, err
		}
		m.OldFolderPath = filepath.ToSlash(filepath.Join("uploads", m.Token))
		m.NewFolderPath = filepath.ToSlash(filepath.Join("collections", CollectionDirName(m.Name, m.Token)))
		migrations = append(migrations, m)
	}
	return migrations, rows.Err()
}

// FindCollectionFolderPath 按 token 前 8 位后缀查找该用户已存在的收集目录，改名后继续复用原目录。
// FindCollectionFolderPath finds the user's existing collection folder by token suffix so renaming a
// collection keeps its original directory instead of splitting into a second one.
func (s *Store) FindCollectionFolderPath(ctx context.Context, userID int64, token string) (string, bool, error) {
	suffix := "-" + tokenPrefix(token)
	if suffix == "-" {
		return "", false, nil
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT path FROM folders WHERE user_id = ? AND path LIKE 'collections/%' AND path NOT LIKE 'collections/%/%' ORDER BY id", userID)
	if err != nil {
		return "", false, err
	}
	defer rows.Close()
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return "", false, err
		}
		if strings.HasSuffix(path, suffix) {
			return path, true, nil
		}
	}
	return "", false, rows.Err()
}

// RewriteCollectionStoragePaths 在一个事务内把某收集在 files.storage_path、folders.path 与
// upload_tasks.storage_dir 中的旧前缀改写为新布局，并清理空的遗留 uploads 目录记录。
// RewriteCollectionStoragePaths rewrites one collection's stored paths (files, folders, tasks) from
// the legacy prefix to the new layout in a single transaction, then drops the now-empty legacy
// uploads folder record.
func (s *Store) RewriteCollectionStoragePaths(ctx context.Context, m CollectionDirMigration) (int64, int64, int64, error) {
	ownerDir := filepath.ToSlash(filepath.Join("files", strconv.FormatInt(m.OwnerID, 10)))
	oldDir := filepath.ToSlash(filepath.Join(ownerDir, m.OldFolderPath))
	newDir := filepath.ToSlash(filepath.Join(ownerDir, m.NewFolderPath))
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, 0, err
	}
	defer tx.Rollback()

	// 存储路径在不同平台上可能含反斜杠（filepath.Join 的结果），比较与改写前统一归一化为斜杠，
	// 否则 Windows 数据的迁移会 0 命中。改写后的路径为斜杠形式，Go 在 Windows 上同样接受。
	// Stored paths may contain backslashes (filepath.Join output); normalise separators before
	// comparing or rewriting, otherwise Windows data silently matches nothing. Slash paths are
	// accepted by Go on Windows as well.
	normalize := func(column string) string { return "replace(" + column + ", '\\', '/')" }
	filesSQL := "UPDATE files SET storage_path = ? || substr(" + normalize("storage_path") + ", ?) WHERE user_id = ? AND (" +
		normalize("storage_path") + " = ? OR substr(" + normalize("storage_path") + ", 1, ?) = ? || '/')"
	tasksSQL := "UPDATE upload_tasks SET storage_dir = ? || substr(" + normalize("storage_dir") + ", ?) WHERE collection_id = ? AND storage_dir <> '' AND (" +
		normalize("storage_dir") + " = ? OR substr(" + normalize("storage_dir") + ", 1, ?) = ? || '/')"
	foldersSQL := "UPDATE folders SET path = ? || substr(" + normalize("path") + ", ?) WHERE user_id = ? AND (" +
		normalize("path") + " = ? OR substr(" + normalize("path") + ", 1, ?) = ? || '/')"

	filesResult, err := tx.ExecContext(ctx, filesSQL,
		newDir, len(oldDir)+1, m.OwnerID, oldDir, len(oldDir)+1, oldDir)
	if err != nil {
		return 0, 0, 0, err
	}
	foldersResult, err := tx.ExecContext(ctx, foldersSQL,
		m.NewFolderPath, len(m.OldFolderPath)+1, m.OwnerID, m.OldFolderPath, len(m.OldFolderPath)+1, m.OldFolderPath)
	if err != nil {
		return 0, 0, 0, err
	}
	tasksResult, err := tx.ExecContext(ctx, tasksSQL,
		newDir, len(oldDir)+1, m.CollectionID, oldDir, len(oldDir)+1, oldDir)
	if err != nil {
		return 0, 0, 0, err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM folders WHERE user_id = ? AND path = 'uploads' AND NOT EXISTS (SELECT 1 FROM folders f WHERE f.user_id = ? AND f.path LIKE 'uploads/%')", m.OwnerID, m.OwnerID); err != nil {
		return 0, 0, 0, err
	}
	files, _ := filesResult.RowsAffected()
	folders, _ := foldersResult.RowsAffected()
	tasks, _ := tasksResult.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, 0, 0, err
	}
	return files, folders, tasks, nil
}
