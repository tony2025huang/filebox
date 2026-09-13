package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"filebox/internal/store"
)

// moveRecycleFiles 把选中的回收站文件移动到指定用户的指定目录（仅管理员，v041）。
// 目标目录沿用上传目录的同一套校验（逐段拒绝绝对路径、..、反斜杠、控制字符与 Windows 保留字符），
// 因此回收站移动不会成为绕过路径校验的新入口。
// moveRecycleFiles moves selected recycle-bin files into a target user's directory (admin only, v041).
// The target directory reuses the upload directory validator, so this cannot bypass path validation.
func (s *Server) moveRecycleFiles(w http.ResponseWriter, r *http.Request) {
	var input struct {
		FileIDs      []int64 `json:"fileIds"`
		TargetUserID int64   `json:"targetUserId"`
		TargetDir    string  `json:"targetDir"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	if len(input.FileIDs) == 0 || len(input.FileIDs) > 500 {
		writeError(w, http.StatusBadRequest, "请选择要移动的文件（单次最多 500 个）")
		return
	}
	dir, err := validateUploadDir(input.TargetDir)
	if err != nil {
		writeError(w, http.StatusBadRequest, "目标目录无效")
		return
	}
	admin := currentUser(r.Context())
	result, moveErr := s.store.MoveRecycleFiles(r.Context(), input.FileIDs, input.TargetUserID, dir)
	if moveErr != nil {
		var quotaErr *store.QuotaError
		switch {
		case errors.Is(moveErr, store.ErrNotFound):
			writeError(w, http.StatusNotFound, "目标用户或回收站文件不存在")
		case errors.As(moveErr, &quotaErr):
			writeErrorData(w, http.StatusForbidden, "目标用户配额不足", map[string]any{
				"code": "QUOTA_EXCEEDED", "usedBytes": quotaErr.UsedBytes, "quotaBytes": quotaErr.QuotaBytes,
				"fileSize": quotaErr.FileSize, "pendingBytes": quotaErr.PendingBytes,
			})
		default:
			log.Printf("recycle_move result=failure target_user=%d dir=%q err=%v", input.TargetUserID, dir, moveErr)
			writeError(w, http.StatusInternalServerError, "移动文件失败")
		}
		return
	}
	// folders 记录在事务提交后补齐：move 内部持有写事务，SQLite 单连接下事务内再写入会自锁。
	// Folder records are ensured after the transaction commits (single SQLite connection).
	if dir != "" {
		if ensureErr := s.store.EnsureFolderPath(r.Context(), input.TargetUserID, dir); ensureErr != nil {
			log.Printf("recycle_move ensure folder target_user=%d dir=%q err=%v", input.TargetUserID, dir, ensureErr)
		}
	}
	target := "-"
	if dir != "" {
		target = dir
	}
	log.Printf("recycle_move operator=%s target_user=%d dir=%q moved=%d result=success", admin.Username, input.TargetUserID, dir, len(result.MovedIDs))
	s.recordAudit(r, &admin.ID, admin.Username, "recycle_move", target, "success", "")
	writeData(w, http.StatusOK, "移动成功", map[string]any{
		"moved": len(result.MovedIDs), "fileIds": result.MovedIDs, "targetUserId": input.TargetUserID, "targetDir": result.TargetDir,
	})
}
