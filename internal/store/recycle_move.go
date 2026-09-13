package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// RecycleMoveResult 描述一次"回收站 → 用户目录"移动的结果。
// RecycleMoveResult reports one recycle-bin → user-directory move.
type RecycleMoveResult struct {
	MovedIDs  []int64
	TargetDir string
}

// recycleMovePlan 是一个文件的移动计划（数据库行 + 磁盘源/目标路径）。
// recycleMovePlan is one file's move plan (row id plus source and destination paths).
type recycleMovePlan struct {
	id   int64
	from string
	to   string
	name string
}

// MoveRecycleFiles 把选中的回收站文件移动到目标用户的指定目录下（v041）。
//
// 与 DeleteUser 里"迁入回收站"的流程对称：先算不冲突的文件名，再在磁盘上逐个同卷 rename（任一步
// 失败都把已移动的文件移回原处，避免磁盘与数据库不一致），最后在一个事务里改写 files 行并累加
// 目标用户的 used_bytes；目标用户配额不足时返回 *QuotaError。folders 记录由调用方在提交后补齐：
// 本函数持有写事务，而 SQLite 连接数为 1，事务内再次写入会自锁。
//
// MoveRecycleFiles moves selected recycle-bin files into the target user's directory. It mirrors the
// delete-user flow in reverse. Folder records are left to the caller because this holds the single
// SQLite write transaction.
func (s *Store) MoveRecycleFiles(ctx context.Context, ids []int64, targetUserID int64, targetDir string) (RecycleMoveResult, error) {
	result := RecycleMoveResult{TargetDir: targetDir}
	if len(ids) == 0 {
		return result, errors.New("no files selected")
	}
	if targetUserID <= 0 || targetUserID == RecycleOwnerID {
		return result, ErrNotFound
	}
	target, err := s.GetUser(targetUserID)
	if err != nil {
		return result, err
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()

	type recycleRow struct {
		id          int64
		name        string
		size        int64
		storagePath string
	}
	rows := make([]recycleRow, 0, len(ids))
	var totalSize int64
	for _, id := range ids {
		var item recycleRow
		scanErr := tx.QueryRowContext(ctx,
			"SELECT id, name, size, storage_path FROM files WHERE id = ? AND user_id = ? AND status = 'ready'",
			id, RecycleOwnerID).Scan(&item.id, &item.name, &item.size, &item.storagePath)
		if errors.Is(scanErr, sql.ErrNoRows) {
			continue // 已被永久删除或不属于回收站：跳过而不是整体失败
		}
		if scanErr != nil {
			return result, scanErr
		}
		rows = append(rows, item)
		totalSize += item.size
	}
	if len(rows) == 0 {
		return result, ErrNotFound
	}

	if target.QuotaBytes > 0 && target.UsedBytes+totalSize > target.QuotaBytes {
		return result, &QuotaError{UsedBytes: target.UsedBytes, QuotaBytes: target.QuotaBytes, FileSize: totalSize}
	}

	targetRoot := filepath.Join("files", strconv.FormatInt(targetUserID, 10), filepath.FromSlash(targetDir))
	plans := make([]recycleMovePlan, 0, len(rows))
	for _, item := range rows {
		base := filepath.Base(filepath.FromSlash(strings.ReplaceAll(item.storagePath, "\\", "/")))
		if base == "." || base == "" {
			base = item.name
		}
		newName, newPath, pathErr := recycleStoragePath(ctx, tx, targetRoot, base)
		if pathErr != nil {
			return result, pathErr
		}
		plans = append(plans, recycleMovePlan{id: item.id, from: item.storagePath, to: newPath, name: newName})
	}

	// 磁盘先动：同卷 rename，任一步失败则回滚已移动的文件（与 DeleteUser 相同的策略）。
	// Disk first with rollback of the files already moved.
	moved := 0
	for _, item := range plans {
		if mkErr := os.MkdirAll(filepath.Dir(filepath.Join(s.DataDir, item.to)), 0o755); mkErr != nil {
			s.rollbackRecycleMoves(plans[:moved])
			return result, mkErr
		}
		if renameErr := os.Rename(filepath.Join(s.DataDir, item.from), filepath.Join(s.DataDir, item.to)); renameErr != nil {
			if !errors.Is(renameErr, os.ErrNotExist) {
				s.rollbackRecycleMoves(plans[:moved])
				return result, renameErr
			}
			// 磁盘文件缺失：仍改写数据库路径，避免留下永远打不开的记录。
		}
		moved++
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, item := range plans {
		if _, err := tx.ExecContext(ctx,
			"UPDATE files SET user_id = ?, storage_path = ?, stored_name = ?, name = ? WHERE id = ?",
			targetUserID, filepath.ToSlash(item.to), item.name, item.name, item.id); err != nil {
			s.rollbackRecycleMoves(plans[:moved])
			return result, err
		}
		result.MovedIDs = append(result.MovedIDs, item.id)
	}
	if totalSize > 0 {
		if _, err := tx.ExecContext(ctx, "UPDATE users SET used_bytes = used_bytes + ?, updated_at = ? WHERE id = ?",
			totalSize, now, targetUserID); err != nil {
			s.rollbackRecycleMoves(plans[:moved])
			return result, err
		}
	}
	if err := tx.Commit(); err != nil {
		s.rollbackRecycleMoves(plans[:moved])
		return result, err
	}
	return result, nil
}

// rollbackRecycleMoves 把已完成的磁盘移动移回原处（尽力而为，仅用于错误回滚）。
// rollbackRecycleMoves moves already-moved files back (best effort, error paths only).
func (s *Store) rollbackRecycleMoves(plans []recycleMovePlan) {
	for index := len(plans) - 1; index >= 0; index-- {
		_ = os.Rename(filepath.Join(s.DataDir, plans[index].to), filepath.Join(s.DataDir, plans[index].from))
	}
}
