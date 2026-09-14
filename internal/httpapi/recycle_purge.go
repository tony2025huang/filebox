package httpapi

import (
	"log"
	"net/http"
)

// purgeRecycleBinWithReauth 在清空回收站之前要求二次认证（复用 clear-all 的那套两级限速 + 密码/TOTP），
// 因为清空回收站不可恢复——与"清空全部文件"保持同一安全等级（v041）。认证通过后交给 purgeRecycleBin
// 执行实际清空，避免重复实现删除与目录清理逻辑。
// purgeRecycleBinWithReauth requires the same re-authentication as clear-all before emptying the recycle
// bin, because the operation is irreversible (v041). It delegates the actual purge to purgeRecycleBin.
func (s *Server) purgeRecycleBinWithReauth(w http.ResponseWriter, r *http.Request) {
	admin := currentUser(r.Context())
	var input struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	decision, err := s.clearFilesReauth(r.Context(), r, admin, input.Password, input.Code)
	if err != nil {
		log.Printf("recycle_purge reauth result=failure reason=internal err=%v", err)
		s.recordAudit(r, &admin.ID, admin.Username, "recycle_purge", "recycle", "failure", "reauth_failed")
		writeError(w, http.StatusInternalServerError, "清空回收站失败")
		return
	}
	if decision != clearReauthAllow {
		// 失败（含被限速）都计入来源 IP 失败窗口（R-IPBAN）；刻意不接入账号级锁定，避免持被盗 JWT 者锁死管理员。
		// Failures (including throttled ones) feed the source-IP failure window; no account lockout.
		if settings, settingsErr := s.store.GetLogSettings(r.Context()); settingsErr != nil {
			log.Printf("recycle_purge reauth settings result=failure err=%v", settingsErr)
		} else {
			s.recordIPFailure(r, settings)
		}
		if decision == clearReauthRateLimited {
			log.Printf("recycle_purge operator=%s result=failure reason=reauth_rate_limited", admin.Username)
			s.recordAudit(r, &admin.ID, admin.Username, "recycle_purge", "recycle", "failure", "reauth_rate_limited")
			writeErrorData(w, http.StatusTooManyRequests, "验证尝试过于频繁，请稍后再试", map[string]string{"code": "REAUTH_RATE_LIMITED"})
			return
		}
		log.Printf("recycle_purge operator=%s result=failure reason=reauth_failed", admin.Username)
		s.recordAudit(r, &admin.ID, admin.Username, "recycle_purge", "recycle", "failure", "reauth_failed")
		writeError(w, http.StatusUnauthorized, "认证失败")
		return
	}
	s.purgeRecycleBin(w, r)
}
