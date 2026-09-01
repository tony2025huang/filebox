package httpapi

// 聚合分享（批量分享统一链接，v013 #7）HTTP 处理器。
// Aggregate-share (batch-share unified link, v013 #7) HTTP handlers.

import (
	"archive/zip"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"filebox/internal/store"
)

type batchShareGroupRequest struct {
	FileIDs        []int64 `json:"fileIds"`
	ExpiresInHours int     `json:"expiresInHours"`
	MaxDownloads   int     `json:"maxDownloads"`
}

func shareGroupStatus(group store.ShareGroup) (string, int64) {
	if group.RevokedAt != "" {
		return "revoked", 0
	}
	deadline, err := time.Parse(time.RFC3339, group.ExpiresAt)
	if err != nil || !time.Now().UTC().Before(deadline) {
		return "expired", 0
	}
	if group.MaxDownloads > 0 && group.DownloadCount >= int64(group.MaxDownloads) {
		return "limit_reached", int64(time.Until(deadline).Seconds())
	}
	return "active", int64(time.Until(deadline).Seconds())
}

func publicShareGroup(group store.ShareGroup) map[string]any {
	status, remaining := shareGroupStatus(group)
	return map[string]any{
		"id": group.ID, "token": group.Token, "url": "/g/" + group.Token, "createdBy": group.CreatedBy,
		"expiresAt": group.ExpiresAt, "downloadCount": group.DownloadCount, "maxDownloads": group.MaxDownloads,
		"revokedAt": group.RevokedAt, "createdAt": group.CreatedAt, "fileCount": group.FileCount,
		"status": status, "remainingSeconds": remaining,
	}
}

// createBatchShareGroup 创建统一聚合分享链接（事务内整批校验，任一文件非法整体回滚）。
// createBatchShareGroup creates one unified aggregate share (whole-batch validation in a transaction).
func (s *Server) createBatchShareGroup(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	if s.rejectReadOnly(w, r, user, "batch_share", "batch-group") {
		return
	}
	result, reason := "failure", "invalid_request"
	target := "batch-group"
	defer func() {
		s.recordAudit(r, &user.ID, user.Username, "batch_share", target, result, reason)
		s.serviceEvent(r, "batch_share", user.Username, "target=%s result=%s reason=%s", target, result, reason)
	}()
	var input batchShareGroupRequest
	if !decodeJSON(w, r, &input) || len(input.FileIDs) == 0 {
		writeError(w, http.StatusBadRequest, "请选择要分享的文件")
		return
	}
	if len(input.FileIDs) > 500 {
		writeError(w, http.StatusBadRequest, "批量操作数量超出上限（最多 500 个）")
		return
	}
	if input.ExpiresInHours < 1 || input.ExpiresInHours > 87600 {
		writeError(w, http.StatusBadRequest, "分享有效期无效")
		return
	}
	if input.MaxDownloads < 0 || input.MaxDownloads > 100000 {
		writeError(w, http.StatusBadRequest, "分享次数限制无效")
		return
	}
	token, err := randomShareToken()
	if err != nil {
		reason = "token_failed"
		writeError(w, http.StatusInternalServerError, "创建分享链接失败")
		return
	}
	expiresAt := time.Now().UTC().Add(time.Duration(input.ExpiresInHours) * time.Hour).Format(time.RFC3339)
	group, files, err := s.store.CreateShareGroup(r.Context(), user.ID, token, input.FileIDs, expiresAt, input.MaxDownloads)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}
	if err != nil {
		reason = "create_failed"
		log.Printf("create batch share group: %v", err)
		writeError(w, http.StatusInternalServerError, "创建分享链接失败")
		return
	}
	itemNames := make([]string, 0, len(files))
	items := make([]map[string]any, 0, len(files))
	for _, file := range files {
		itemNames = append(itemNames, file.File.Name)
		items = append(items, map[string]any{"fileId": file.FileID, "fileName": file.File.Name})
	}
	target = strings.Join(itemNames, ",")
	result, reason = "success", "batch_group"
	writeData(w, http.StatusCreated, "批量分享链接已创建", map[string]any{
		"token": group.Token, "url": "/g/" + group.Token, "expiresAt": group.ExpiresAt,
		"maxDownloads": group.MaxDownloads, "items": items,
	})
}

// listShareGroups 返回创建者（或管理员）的聚合分享列表。
// listShareGroups lists aggregate shares for the owner (or admin).
func (s *Server) listShareGroups(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	groups, err := s.store.ListShareGroupsByOwner(r.Context(), user.ID, user.Role == "admin")
	if err != nil {
		log.Printf("list share groups: %v", err)
		writeError(w, http.StatusInternalServerError, "获取分享列表失败")
		return
	}
	items := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		items = append(items, publicShareGroup(group))
	}
	writeData(w, http.StatusOK, "获取成功", map[string]any{"items": items})
}

// shareGroupMeta 公开聚合分享元数据（匿名访问，限速）。
// shareGroupMeta exposes public aggregate-share metadata (anonymous, rate-limited).
func (s *Server) shareGroupMeta(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	var ownerID *int64
	result, reason := "failure", "share_not_found"
	defer func() {
		s.serviceEvent(r, "share_view", "anonymous", "target=%s result=%s reason=%s", token, result, reason)
		s.recordShareAudit(r, nil, ownerID, "anonymous", "share_view", token, result, reason)
	}()
	if !s.rateLimiter.allowPublicRequest(s.requestIP(r), 30, 10) {
		result, reason = "failure", "rate_limited"
		writeError(w, http.StatusTooManyRequests, "请求过于频繁，请稍后重试")
		return
	}
	group, err := s.store.GetShareGroupByToken(r.Context(), token)
	if err != nil {
		if revoked, revokedErr := s.store.GetShareGroupByTokenIncludingRevoked(r.Context(), token); revokedErr == nil && revoked.RevokedAt != "" {
			ownerID = &revoked.CreatedBy
			reason = "share_revoked"
			writeError(w, http.StatusForbidden, "分享已撤销")
			return
		}
		writeError(w, http.StatusNotFound, "分享不存在")
		return
	}
	ownerID = &group.CreatedBy
	files, err := s.store.ListShareGroupFiles(r.Context(), group.ID)
	if err != nil {
		reason = "list_failed"
		writeError(w, http.StatusInternalServerError, "读取分享文件失败")
		return
	}
	result, reason = "success", ""
	data := publicShareGroup(group)
	fileItems := make([]map[string]any, 0, len(files))
	for _, item := range files {
		fileItems = append(fileItems, map[string]any{
			"fileId": item.FileID, "name": item.File.Name, "size": item.File.Size, "mime": item.File.Mime, "createdAt": item.File.CreatedAt,
		})
	}
	data["files"] = fileItems
	writeData(w, http.StatusOK, "读取成功", data)
}

// loadShareGroupTask 校验 token 对应的聚合分享可用并加载成员文件。
// loadShareGroupTask validates the aggregate share and loads its member files.
func (s *Server) loadShareGroupTask(r *http.Request) (store.ShareGroup, map[int64]store.ShareGroupFile, error) {
	token := r.PathValue("token")
	group, err := s.store.GetShareGroupByToken(r.Context(), token)
	if err != nil {
		return store.ShareGroup{}, nil, err
	}
	files, err := s.store.ListShareGroupFiles(r.Context(), group.ID)
	if err != nil {
		return store.ShareGroup{}, nil, err
	}
	byID := make(map[int64]store.ShareGroupFile, len(files))
	for _, item := range files {
		byID[item.FileID] = item
	}
	return group, byID, nil
}

// shareGroupDownload 单文件下载（匿名，每次消耗 1 次聚合分享次数）。
// shareGroupDownload downloads one member file (anonymous, consumes one aggregate slot).
func (s *Server) shareGroupDownload(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	fileID, err := strconv.ParseInt(r.PathValue("fileID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}
	var ownerID *int64
	result, reason := "failure", "share_not_found"
	defer func() {
		s.serviceEvent(r, "share_download", "anonymous", "target=%s file=%d result=%s reason=%s", token, fileID, result, reason)
		s.recordShareAudit(r, nil, ownerID, "anonymous", "share_download", token, result, reason)
	}()
	if !s.rateLimiter.allowPublicRequest(s.requestIP(r), 30, 10) {
		result, reason = "failure", "rate_limited"
		writeError(w, http.StatusTooManyRequests, "请求过于频繁，请稍后重试")
		return
	}
	group, filesByID, err := s.loadShareGroupTask(r)
	if err != nil {
		if revoked, revokedErr := s.store.GetShareGroupByTokenIncludingRevoked(r.Context(), token); revokedErr == nil && revoked.RevokedAt != "" {
			ownerID = &revoked.CreatedBy
			reason = "share_revoked"
			writeError(w, http.StatusForbidden, "分享已撤销")
			return
		}
		writeError(w, http.StatusNotFound, "分享不存在")
		return
	}
	ownerID = &group.CreatedBy
	item, ok := filesByID[fileID]
	if !ok {
		reason = "share_denied"
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}
	if !shareActive(group.ExpiresAt) {
		reason = "share_expired"
		writeError(w, http.StatusForbidden, "分享链接已过期")
		return
	}
	allowed, err := s.store.IncrementShareGroupDownloads(r.Context(), token, group.MaxDownloads, r.Header.Get("Range") != "")
	if err != nil {
		reason = "download_count_failed"
		writeError(w, http.StatusInternalServerError, "分享下载失败")
		return
	}
	if !allowed {
		reason = "share_limit"
		writeErrorData(w, http.StatusForbidden, "分享次数已用完", map[string]string{"code": "SHARE_DOWNLOAD_LIMIT"})
		return
	}
	handle, err := os.Open(filepath.Join(s.config.DataDir, item.File.StoragePath))
	if err != nil {
		reason = "content_not_found"
		writeError(w, http.StatusNotFound, "文件内容不存在")
		return
	}
	defer handle.Close()
	w.Header().Set("Content-Type", effectiveFileMIME(item.File))
	w.Header().Set("Content-Disposition", contentDisposition(item.File.Name))
	result, reason = "success", ""
	http.ServeContent(w, r, item.File.Name, parseTime(item.File.CreatedAt), handle)
}

// shareGroupBatchDownload 匿名 ZIP 聚合下载（全选或选中文件，整体消耗 1 次）。
// shareGroupBatchDownload zips selected member files for anonymous download (consumes one slot).
func (s *Server) shareGroupBatchDownload(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	var input struct {
		IDs []int64 `json:"ids"`
	}
	if !decodeJSON(w, r, &input) {
		writeError(w, http.StatusBadRequest, "请选择要下载的文件")
		return
	}
	var ownerID *int64
	result, reason := "failure", "share_not_found"
	defer func() {
		s.serviceEvent(r, "share_download", "anonymous", "target=%s count=%d result=%s reason=%s", token, len(input.IDs), result, reason)
		s.recordShareAudit(r, nil, ownerID, "anonymous", "share_download", token, result, reason)
	}()
	if !s.rateLimiter.allowPublicRequest(s.requestIP(r), 30, 10) {
		result, reason = "failure", "rate_limited"
		writeError(w, http.StatusTooManyRequests, "请求过于频繁，请稍后重试")
		return
	}
	group, filesByID, err := s.loadShareGroupTask(r)
	if err != nil {
		if revoked, revokedErr := s.store.GetShareGroupByTokenIncludingRevoked(r.Context(), token); revokedErr == nil && revoked.RevokedAt != "" {
			ownerID = &revoked.CreatedBy
			reason = "share_revoked"
			writeError(w, http.StatusForbidden, "分享已撤销")
			return
		}
		writeError(w, http.StatusNotFound, "分享不存在")
		return
	}
	ownerID = &group.CreatedBy
	if !shareActive(group.ExpiresAt) {
		reason = "share_expired"
		writeError(w, http.StatusForbidden, "分享链接已过期")
		return
	}
	selected := make([]store.ShareGroupFile, 0, len(input.IDs))
	seen := make(map[int64]bool, len(input.IDs))
	for _, id := range input.IDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		if len(seen) > batchDownloadMaxFiles {
			writeErrorData(w, http.StatusRequestEntityTooLarge, "批量下载文件数量超过上限", map[string]string{"code": "BATCH_TOO_LARGE"})
			return
		}
		item, ok := filesByID[id]
		if !ok {
			reason = "share_denied"
			writeError(w, http.StatusNotFound, "文件不存在")
			return
		}
		selected = append(selected, item)
	}
	if len(selected) == 0 {
		writeError(w, http.StatusBadRequest, "请选择要下载的文件")
		return
	}
	var totalBytes int64
	for _, item := range selected {
		if item.File.Size < 0 || item.File.Size > batchDownloadMaxBytes-totalBytes {
			writeErrorData(w, http.StatusRequestEntityTooLarge, "批量下载文件总大小超过 2 GiB 上限", map[string]string{"code": "BATCH_TOO_LARGE"})
			return
		}
		totalBytes += item.File.Size
	}
	if hasSpace, diskErr := s.batchDownloadDiskAvailable(totalBytes); diskErr != nil {
		log.Printf("check share-group batch download disk usage: %v", diskErr)
		writeError(w, http.StatusInternalServerError, "无法检查系统存储空间")
		return
	} else if !hasSpace {
		writeErrorData(w, http.StatusServiceUnavailable, "系统存储空间不足，暂时禁止批量下载", map[string]string{"code": "DISK_FULL"})
		return
	}
	allowed, err := s.store.IncrementShareGroupDownloads(r.Context(), token, group.MaxDownloads, false)
	if err != nil {
		reason = "download_count_failed"
		writeError(w, http.StatusInternalServerError, "分享下载失败")
		return
	}
	if !allowed {
		reason = "share_limit"
		writeErrorData(w, http.StatusForbidden, "分享次数已用完", map[string]string{"code": "SHARE_DOWNLOAD_LIMIT"})
		return
	}
	temp, err := os.CreateTemp(filepath.Join(s.config.DataDir, "tmp"), "batch-share-group-*.zip")
	if err != nil {
		reason = "archive_failed"
		writeError(w, http.StatusInternalServerError, "创建下载文件失败")
		return
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	zw := zip.NewWriter(temp)
	entryNames := make(map[string]struct{})
	for _, item := range selected {
		handle, openErr := os.Open(filepath.Join(s.config.DataDir, item.File.StoragePath))
		if openErr != nil {
			zw.Close()
			temp.Close()
			reason = "content_not_found"
			writeError(w, http.StatusNotFound, "文件内容不存在")
			return
		}
		entryName := uniqueZipEntryName(item.File.Name, entryNames)
		entry, createErr := zw.Create(entryName)
		if createErr != nil {
			handle.Close()
			zw.Close()
			temp.Close()
			reason = "archive_failed"
			writeError(w, http.StatusInternalServerError, "创建下载文件失败")
			return
		}
		_, copyErr := io.Copy(entry, handle)
		handle.Close()
		if copyErr != nil {
			zw.Close()
			temp.Close()
			reason = "archive_failed"
			writeError(w, http.StatusInternalServerError, "读取文件内容失败")
			return
		}
	}
	if err := zw.Close(); err != nil {
		temp.Close()
		reason = "archive_failed"
		writeError(w, http.StatusInternalServerError, "创建下载文件失败")
		return
	}
	if err := temp.Close(); err != nil {
		reason = "archive_failed"
		writeError(w, http.StatusInternalServerError, "创建下载文件失败")
		return
	}
	info, err := os.Stat(tempPath)
	if err != nil {
		reason = "archive_failed"
		writeError(w, http.StatusInternalServerError, "读取下载文件失败")
		return
	}
	archive, err := os.Open(tempPath)
	if err != nil {
		reason = "archive_failed"
		writeError(w, http.StatusInternalServerError, "读取下载文件失败")
		return
	}
	defer archive.Close()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", contentDisposition("filebox-batch-download.zip"))
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	if _, err := io.Copy(w, archive); err != nil {
		log.Printf("stream share-group archive: %v", err)
		return
	}
	result, reason = "success", ""
}

// managedShareGroup 校验 token 对应的聚合分享由当前用户创建（或当前用户为管理员）。
// managedShareGroup loads an aggregate share and enforces owner/admin scope.
func (s *Server) managedShareGroup(r *http.Request) (store.ShareGroup, error) {
	user := currentUser(r.Context())
	group, err := s.store.GetShareGroupByTokenIncludingRevoked(r.Context(), r.PathValue("token"))
	if err != nil || (group.CreatedBy != user.ID && user.Role != "admin") || group.RevokedAt != "" {
		return store.ShareGroup{}, store.ErrNotFound
	}
	return group, nil
}

// extendShareGroup 延长聚合分享有效期（创建者或管理员），不缩短原截止时间（#6）。
// extendShareGroup extends an aggregate share's validity (owner or admin) without shortening it (#6).
func (s *Server) extendShareGroup(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	group, err := s.managedShareGroup(r)
	if err != nil {
		writeError(w, http.StatusNotFound, "分享不存在")
		return
	}
	if s.rejectReadOnly(w, r, user, "share", group.Token) {
		return
	}
	var input shareExtendRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.ExpiresInHours < 1 || input.ExpiresInHours > 87600 {
		writeError(w, http.StatusBadRequest, "分享有效期无效")
		return
	}
	if err := s.store.UpdateShareGroupExpiry(r.Context(), group.Token, time.Now().UTC().Add(time.Duration(input.ExpiresInHours)*time.Hour), group.CreatedBy); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "分享不存在")
		} else {
			log.Printf("extend share group: %v", err)
			writeError(w, http.StatusInternalServerError, "延期分享失败")
		}
		return
	}
	updated, _ := s.store.GetShareGroupByTokenIncludingRevoked(r.Context(), group.Token)
	s.recordAudit(r, &user.ID, user.Username, "share_group_extend", group.Token, "success", "extend")
	s.serviceEvent(r, "share_group_extend", user.Username, "target=%s hours=%d result=success", group.Token, input.ExpiresInHours)
	writeData(w, http.StatusOK, "分享有效期已更新", publicShareGroup(updated))
}

// increaseShareGroup 提高聚合分享下载上限，拒绝降低（#6）。
// increaseShareGroup raises an aggregate share's download limit and rejects decreases (#6).
func (s *Server) increaseShareGroup(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	group, err := s.managedShareGroup(r)
	if err != nil {
		writeError(w, http.StatusNotFound, "分享不存在")
		return
	}
	if s.rejectReadOnly(w, r, user, "share", group.Token) {
		return
	}
	var input shareIncreaseRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.MaxDownloads < 0 || input.MaxDownloads > 100000 || (input.MaxDownloads != 0 && input.MaxDownloads <= group.MaxDownloads) {
		writeError(w, http.StatusBadRequest, "分享次数限制无效")
		return
	}
	if err := s.store.UpdateShareGroupMaxDownloads(r.Context(), group.Token, input.MaxDownloads, group.CreatedBy); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "分享不存在")
		} else if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusBadRequest, "分享次数限制无效")
		} else {
			log.Printf("increase share group: %v", err)
			writeError(w, http.StatusInternalServerError, "增加分享次数失败")
		}
		return
	}
	updated, _ := s.store.GetShareGroupByTokenIncludingRevoked(r.Context(), group.Token)
	s.recordAudit(r, &user.ID, user.Username, "share_group_increase", group.Token, "success", "increase")
	s.serviceEvent(r, "share_group_increase", user.Username, "target=%s max=%d result=success", group.Token, input.MaxDownloads)
	writeData(w, http.StatusOK, "分享次数已更新", publicShareGroup(updated))
}

// revokeShareGroup 撤销聚合分享（创建者或管理员）。
// revokeShareGroup revokes an aggregate share (owner or admin).
func (s *Server) revokeShareGroup(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	token := r.PathValue("token")
	if s.rejectReadOnly(w, r, user, "share", token) {
		return
	}
	err := s.store.RevokeShareGroup(r.Context(), token, user.ID, user.Role == "admin")
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "分享不存在")
		return
	}
	if err != nil {
		log.Printf("revoke share group: %v", err)
		writeError(w, http.StatusInternalServerError, "撤销分享失败")
		return
	}
	s.serviceEvent(r, "share_revoke", user.Username, "target=%s result=success", token)
	s.recordAudit(r, &user.ID, user.Username, "share", token, "success", "revoke_group")
	writeData(w, http.StatusOK, "分享已撤销", map[string]any{"token": token})
}
