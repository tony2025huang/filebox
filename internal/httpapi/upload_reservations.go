package httpapi

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"
)

// staleUploadReservationAge 是未完成上传的预留额度在被回收前允许闲置的时长。
// 周期清理按 created_at < 24 小时回收，对"被一条死预留挡住"的用户来说太久了——而且这部分占用
// 在界面上不可见，会让"配额不足"看起来毫无道理；因此上传开始前顺便按需回收。
// staleUploadReservationAge is how long an unfinished upload may sit idle before its reserved quota is
// reclaimed. The periodic sweep only expires tasks after 24h (created_at), which keeps users blocked by
// a dead reservation that the UI does not even show.
const staleUploadReservationAge = 6 * time.Hour

// releaseStaleUploadReservations 回收该用户长期无活动的未完成上传任务（先删临时目录再删任务行），
// 返回释放的条数。任何错误只记录日志，绝不影响调用方的上传流程。
// releaseStaleUploadReservations reclaims the user's long-idle unfinished upload tasks (temp directory
// first, then the task row) and returns how many were released. Errors are logged only.
func (s *Server) releaseStaleUploadReservations(ctx context.Context, userID int64) int {
	cutoff := time.Now().UTC().Add(-staleUploadReservationAge).Format(time.RFC3339)
	rows, err := s.store.DB.QueryContext(ctx, "SELECT id FROM upload_tasks WHERE user_id = ? AND status IN ('pending', 'active', 'queued') AND created_at < ?", userID, cutoff)
	if err != nil {
		log.Printf("scan stale upload reservations: %v", err)
		return 0
	}
	ids := make([]string, 0, 4)
	for rows.Next() {
		var id string
		if scanErr := rows.Scan(&id); scanErr != nil {
			rows.Close()
			log.Printf("scan stale upload reservation: %v", scanErr)
			return 0
		}
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) == 0 {
		return 0
	}
	released := 0
	for _, id := range ids {
		// DeleteUploadTask 会再次确认任务仍未完成（并清理分片记录），避免删掉刚好完成的上传。
		// DeleteUploadTask re-checks that the task is still unfinished (and drops its chunks) so a task
		// that completed in the meantime is never removed.
		if err := s.store.DeleteUploadTask(ctx, id); err != nil {
			log.Printf("release stale upload reservation %s: %v", id, err)
			continue
		}
		if err := os.RemoveAll(filepath.Join(s.config.DataDir, "tmp", id)); err != nil {
			log.Printf("remove stale upload temp %s: %v", id, err)
		}
		released++
	}
	if released > 0 {
		log.Printf("released %d stale upload reservation(s) for user %d", released, userID)
	}
	return released
}
