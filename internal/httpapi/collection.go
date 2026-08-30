package httpapi

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"filebox/internal/store"
)

type collectionCreateRequest struct {
	Name           string `json:"name"`
	ExpiresInHours int    `json:"expiresInHours"`
	MaxUploads     int    `json:"maxUploads"`
	MaxFileBytes   int64  `json:"maxFileBytes"`
}

// collectionUpdateRequest 是收集编辑请求体；expiresAt 为绝对时间（RFC3339），0=不限沿用创建语义。
// collectionUpdateRequest is the collection-edit body; expiresAt is an absolute RFC3339 time, 0 keeps the "unlimited" semantics.
type collectionUpdateRequest struct {
	Name         string `json:"name"`
	ExpiresAt    string `json:"expiresAt"`
	MaxUploads   int    `json:"maxUploads"`
	MaxFileBytes int64  `json:"maxFileBytes"`
}

type collectionUploadInitRequest struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	ChunkSize int64  `json:"chunkSize"`
	SHA256    string `json:"sha256"`
	MD5       string `json:"md5"`
	Mime      string `json:"mime"`
	Remark    string `json:"remark"`
}

type collectionUploadCompleteRequest struct {
	TaskID string `json:"taskId"`
	SHA256 string `json:"sha256"`
	MD5    string `json:"md5"`
}

// createCollection creates a public upload endpoint for the current user.
// createCollection 为当前用户创建公开上传收集链接。
func (s *Server) createCollection(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	if s.rejectReadOnly(w, r, user, "collection_create", "") {
		return
	}
	var input collectionCreateRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 255 || input.ExpiresInHours < 1 || input.ExpiresInHours > 87600 || input.MaxUploads < 0 || input.MaxUploads > 100000 || input.MaxFileBytes < 0 {
		writeError(w, http.StatusBadRequest, "收集链接参数无效")
		return
	}
	token, err := randomShareToken()
	if err != nil {
		log.Printf("create collection token: %v", err)
		writeError(w, http.StatusInternalServerError, "创建收集链接失败")
		return
	}
	collection, err := s.store.CreateUploadCollection(r.Context(), store.UploadCollection{
		CreatedBy: user.ID, Name: input.Name, Token: token,
		ExpiresAt:  time.Now().UTC().Add(time.Duration(input.ExpiresInHours) * time.Hour).Format(time.RFC3339),
		MaxUploads: input.MaxUploads, MaxFileBytes: input.MaxFileBytes,
	})
	if err != nil {
		log.Printf("create collection: %v", err)
		writeError(w, http.StatusInternalServerError, "创建收集链接失败")
		return
	}
	s.serviceEvent(r, "collection_create", user.Username, "collection=%d result=success", collection.ID)
	s.recordAudit(r, &user.ID, user.Username, "collection", collection.Name, "success", "create")
	result := publicCollection(collection, false)
	result["token"] = collection.Token
	result["url"] = "/u/" + collection.Token
	writeData(w, http.StatusCreated, "收集链接已创建", result)
}

// listCollections returns only collections owned by the authenticated user.
// listCollections 仅返回当前登录用户创建的收集链接。
func (s *Server) listCollections(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	items, err := s.store.ListUploadCollections(r.Context(), user.ID, false)
	if err != nil {
		log.Printf("list collections: %v", err)
		writeError(w, http.StatusInternalServerError, "读取收集链接失败")
		return
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		value := publicCollection(item, true)
		value["url"] = "/u/" + item.Token
		result = append(result, value)
	}
	writeData(w, http.StatusOK, "读取成功", map[string]any{"items": result})
}

// getCollection returns owner-scoped collection details and received-file records.
// getCollection 返回归属范围内的收集详情及已收到文件记录。
func (s *Server) getCollection(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "收集链接不存在")
		return
	}
	collection, err := s.store.GetUploadCollection(r.Context(), id, user.ID, user.Role == "admin")
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "收集链接不存在")
		return
	}
	if err != nil {
		log.Printf("get collection: %v", err)
		writeError(w, http.StatusInternalServerError, "读取收集链接失败")
		return
	}
	files, err := s.store.ListUploadCollectionFiles(r.Context(), collection.ID)
	if err != nil {
		log.Printf("list collection files: %v", err)
		writeError(w, http.StatusInternalServerError, "读取已收文件失败")
		return
	}
	result := publicCollection(collection, true)
	result["url"] = "/u/" + collection.Token
	result["files"] = publicCollectionFiles(files)
	writeData(w, http.StatusOK, "读取成功", result)
}

func (s *Server) getCollectionFiles(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "收集链接不存在")
		return
	}
	collection, err := s.store.GetUploadCollection(r.Context(), id, user.ID, user.Role == "admin")
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "收集链接不存在")
		return
	}
	if err != nil {
		log.Printf("get collection files: %v", err)
		writeError(w, http.StatusInternalServerError, "读取已收文件失败")
		return
	}
	files, err := s.store.ListUploadCollectionFiles(r.Context(), collection.ID)
	if err != nil {
		log.Printf("list collection files: %v", err)
		writeError(w, http.StatusInternalServerError, "读取已收文件失败")
		return
	}
	writeData(w, http.StatusOK, "读取成功", map[string]any{"items": publicCollectionFiles(files)})
}

// updateCollection 更新当前用户（或管理员）创建的收集链接：到期时间、上传次数、单文件大小上限。
// updateCollection updates an owned collection (or any, for admins): expiry, upload limit, single-file size limit.
func (s *Server) updateCollection(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "收集链接不存在")
		return
	}
	if s.rejectReadOnly(w, r, user, "collection_update", r.PathValue("id")) {
		return
	}
	var input collectionUpdateRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 255 {
		writeError(w, http.StatusBadRequest, "收集链接参数无效")
		return
	}
	collection, err := s.store.UpdateUploadCollection(r.Context(), id, user.ID, user.Role == "admin", input.Name, input.ExpiresAt, input.MaxUploads, input.MaxFileBytes)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "收集链接不存在")
		return
	case errors.Is(err, store.ErrCollectionRevoked):
		writeError(w, http.StatusBadRequest, "收集链接已撤销，不可编辑")
		return
	case errors.Is(err, store.ErrCollectionMaxUploadsBelow):
		writeErrorData(w, http.StatusBadRequest, "上传次数上限不能低于当前已用次数", map[string]string{"code": "COLLECTION_MAX_UPLOADS_BELOW_USED"})
		return
	case errors.Is(err, store.ErrCollectionInvalidUpdate):
		writeError(w, http.StatusBadRequest, "收集链接参数无效")
		return
	case err != nil:
		log.Printf("update collection: %v", err)
		writeError(w, http.StatusInternalServerError, "保存收集链接失败")
		return
	}
	s.serviceEvent(r, "collection_update", user.Username, "collection=%d result=success", collection.ID)
	s.recordAudit(r, &user.ID, user.Username, "collection_update", collection.Name, "success", "update")
	result := publicCollection(collection, true)
	result["url"] = "/u/" + collection.Token
	writeData(w, http.StatusOK, "收集链接已更新", result)
}

// deleteCollection revokes a collection while keeping files already received.
// deleteCollection 撤销收集链接，但保留已收到的文件。
func (s *Server) deleteCollection(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "收集链接不存在")
		return
	}
	if s.rejectReadOnly(w, r, user, "collection_revoke", r.PathValue("id")) {
		return
	}
	err = s.store.RevokeUploadCollection(r.Context(), id, user.ID, user.Role == "admin")
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "收集链接不存在")
		return
	}
	if err != nil {
		log.Printf("revoke collection: %v", err)
		writeError(w, http.StatusInternalServerError, "撤销收集链接失败")
		return
	}
	s.serviceEvent(r, "collection_revoke", user.Username, "collection=%d result=success", id)
	s.recordAudit(r, &user.ID, user.Username, "collection", strconv.FormatInt(id, 10), "success", "revoke")
	writeData(w, http.StatusOK, "收集链接已撤销", map[string]any{"id": id})
}

// collectionMeta exposes only safe public collection metadata.
// collectionMeta 向匿名访问者仅公开安全的收集元数据。
func (s *Server) collectionMeta(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if !s.rateLimiter.allowPublicRequest(s.requestIP(r), 30, 10) {
		s.collectionFailure(w, r, token, "rate_limited", http.StatusTooManyRequests, "请求过于频繁，请稍后重试", nil)
		return
	}
	collection, err := s.store.GetUploadCollectionByToken(r.Context(), token)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "收集链接不存在")
		return
	}
	if err != nil {
		log.Printf("collection meta: %v", err)
		writeError(w, http.StatusInternalServerError, "读取收集链接失败")
		return
	}
	result := publicCollection(collection, false)
	result["uploadAllowed"] = collectionStatus(collection) == "active"
	writeData(w, http.StatusOK, "读取成功", result)
}

func publicCollection(collection store.UploadCollection, includeToken bool) map[string]any {
	status := collectionStatus(collection)
	result := map[string]any{
		"id": collection.ID, "name": collection.Name, "expiresAt": collection.ExpiresAt,
		"maxUploads": collection.MaxUploads, "uploadCount": collection.UploadCount,
		"maxFileBytes": collection.MaxFileBytes, "status": status,
		"uploadAllowed": status == "active", "remainingSeconds": remainingSeconds(collection.ExpiresAt),
	}
	if includeToken {
		result["token"] = collection.Token
		result["createdBy"] = collection.CreatedBy
		result["revokedAt"] = collection.RevokedAt
		result["createdAt"] = collection.CreatedAt
	}
	return result
}

func collectionStatus(collection store.UploadCollection) string {
	if collection.RevokedAt != "" {
		return "revoked"
	}
	if !shareActive(collection.ExpiresAt) {
		return "expired"
	}
	if collection.MaxUploads > 0 && collection.UploadCount >= collection.MaxUploads {
		return "limit_reached"
	}
	return "active"
}

func remainingSeconds(expiresAt string) int64 {
	deadline := parseTime(expiresAt)
	if deadline.IsZero() {
		return 0
	}
	remaining := int64(time.Until(deadline).Seconds())
	if remaining < 0 {
		return 0
	}
	return remaining
}

func publicCollectionFiles(items []store.UploadCollectionFile) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]any{
			"id": item.ID, "fileId": item.FileID, "originalName": item.OriginalName,
			"remark": item.Remark, "createdAt": item.CreatedAt, "file": publicFile(item.File),
		})
	}
	return result
}

func (s *Server) loadCollectionTask(r *http.Request) (store.UploadCollection, store.User, store.UploadTask, error) {
	collection, err := s.store.GetUploadCollectionByToken(r.Context(), r.PathValue("token"))
	if err != nil {
		return store.UploadCollection{}, store.User{}, store.UploadTask{}, err
	}
	if err := collectionTaskStateForAPI(collection); err != nil {
		return store.UploadCollection{}, store.User{}, store.UploadTask{}, err
	}
	taskID := r.PathValue("taskID")
	if taskID == "" {
		taskID = r.URL.Query().Get("taskId")
	}
	task, err := s.store.GetUploadTask(r.Context(), taskID)
	if err != nil || task.CollectionID != collection.ID || task.UserID != collection.CreatedBy || task.Status != "pending" {
		return store.UploadCollection{}, store.User{}, store.UploadTask{}, store.ErrNotFound
	}
	user, err := s.store.GetUser(collection.CreatedBy)
	if err != nil {
		return store.UploadCollection{}, store.User{}, store.UploadTask{}, err
	}
	return collection, user, task, nil
}

func collectionStateForAPI(collection store.UploadCollection) error {
	if collection.RevokedAt != "" {
		return store.ErrCollectionRevoked
	}
	if !shareActive(collection.ExpiresAt) {
		return store.ErrCollectionExpired
	}
	if collection.MaxUploads > 0 && collection.UploadCount >= collection.MaxUploads {
		return store.ErrCollectionLimit
	}
	return nil
}

func collectionTaskStateForAPI(collection store.UploadCollection) error {
	if collection.RevokedAt != "" {
		return store.ErrCollectionRevoked
	}
	if !shareActive(collection.ExpiresAt) {
		return store.ErrCollectionExpired
	}
	return nil
}

func (s *Server) collectionFailure(w http.ResponseWriter, r *http.Request, target, reason string, status int, message string, data any) {
	s.recordAudit(r, nil, "anonymous", "upload_collect_fail", target, "failure", reason)
	s.serviceEvent(r, "upload_collect_fail", "anonymous", "target=%s result=failure reason=%s", target, reason)
	if data == nil {
		writeError(w, status, message)
		return
	}
	writeErrorData(w, status, message, data)
}

// maskedCollectionToken 返回收集 token 的脱敏前缀（前 8 位），完整 token 是公开凭据，禁止写入日志。
// maskedCollectionToken returns a masked prefix (first 8 chars) of a collection token; the full token is a public credential and must never be logged.
func maskedCollectionToken(token string) string {
	if len(token) <= 8 {
		return "****"
	}
	return token[:8] + "…"
}

// collectionUploadInit validates limits and creates an anonymous upload task.
// collectionUploadInit 校验收集限制并创建匿名上传任务，支持目录内秒传。
func (s *Server) collectionUploadInit(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if !s.rateLimiter.allowPublicRequest(s.requestIP(r), 30, 10) {
		s.collectionFailure(w, r, token, "rate_limited", http.StatusTooManyRequests, "请求过于频繁，请稍后重试", nil)
		return
	}
	var input collectionUploadInitRequest
	if !decodeJSON(w, r, &input) {
		s.collectionFailure(w, r, token, "invalid_request", http.StatusBadRequest, "请求格式无效", nil)
		return
	}
	collection, err := s.store.GetUploadCollectionByToken(r.Context(), token)
	if errors.Is(err, store.ErrNotFound) {
		s.collectionFailure(w, r, token, "not_found", http.StatusNotFound, "收集链接不存在", nil)
		return
	}
	if err != nil {
		log.Printf("load collection for init: %v", err)
		s.collectionFailure(w, r, token, "load_failed", http.StatusInternalServerError, "读取收集链接失败", nil)
		return
	}
	rejectState := func(stateErr error) {
		status, message, code, reason := http.StatusForbidden, "收集链接不可用", "COLLECTION_LIMIT", "collection_limit"
		switch {
		case errors.Is(stateErr, store.ErrCollectionExpired):
			message, code, reason = "收集链接已过期", "COLLECTION_EXPIRED", "collection_expired"
		case errors.Is(stateErr, store.ErrCollectionRevoked):
			message, code, reason = "收集链接已撤销", "COLLECTION_REVOKED", "collection_revoked"
		}
		s.collectionFailure(w, r, token, reason, status, message, map[string]string{"code": code})
	}
	if err := collectionStateForAPI(collection); err != nil {
		rejectState(err)
		return
	}
	name, err := validateUploadName(input.Name)
	if err != nil || name == "" || input.Size < 0 {
		s.collectionFailure(w, r, input.Name, "invalid_name", http.StatusBadRequest, "文件名或文件大小无效", nil)
		return
	}
	if input.Size > s.config.MaxFileSize {
		s.collectionFailure(w, r, name, "too_large", http.StatusRequestEntityTooLarge, "文件超过单文件大小上限", map[string]any{"code": "FILE_TOO_LARGE", "maxFileSize": s.config.MaxFileSize})
		return
	}
	if collection.MaxFileBytes > 0 && input.Size > collection.MaxFileBytes {
		s.collectionFailure(w, r, name, "collection_file_too_large", http.StatusRequestEntityTooLarge, "文件超过收集链接单文件大小上限", map[string]any{"code": "COLLECTION_FILE_TOO_LARGE", "maxFileBytes": collection.MaxFileBytes})
		return
	}
	if len(input.Remark) > 2000 {
		s.collectionFailure(w, r, name, "invalid_remark", http.StatusBadRequest, "备注过长", nil)
		return
	}
	if input.ChunkSize < 0 {
		s.collectionFailure(w, r, name, "invalid_chunk_size", http.StatusBadRequest, "分片大小无效", nil)
		return
	}
	chunkSize, totalChunks, err := uploadChunkLayout(input.Size, input.ChunkSize)
	if err != nil {
		s.collectionFailure(w, r, name, "invalid_chunk_size", http.StatusBadRequest, err.Error(), nil)
		return
	}
	owner, err := s.store.GetUser(collection.CreatedBy)
	if err != nil {
		log.Printf("load collection owner: %v", err)
		s.collectionFailure(w, r, name, "owner_not_found", http.StatusNotFound, "收集链接不存在", nil)
		return
	}
	if s.userReadOnly(owner) {
		s.collectionFailure(w, r, name, "read_only", http.StatusForbidden, "收集者处于只读时段，暂不接受上传", map[string]string{"code": "READ_ONLY"})
		return
	}
	storageDir := filepath.Join("files", strconv.FormatInt(owner.ID, 10), "uploads", token)
	if err := s.store.EnsureFolderPath(r.Context(), owner.ID, filepath.Join("uploads", token)); err != nil {
		log.Printf("ensure collection folder: %v", err)
	}
	input.MD5 = strings.ToLower(strings.TrimSpace(input.MD5))
	input.SHA256 = strings.ToLower(strings.TrimSpace(input.SHA256))
	if input.Size > 0 && (input.MD5 != "" || input.SHA256 != "") {
		file, matchErr := s.store.FindInstantMatchInDirectory(r.Context(), owner.ID, storageDir, input.MD5, input.SHA256, input.Size)
		if matchErr == nil {
			linked, linkErr := s.store.CreateCollectionFile(r.Context(), token, file.ID, name, strings.TrimSpace(input.Remark))
			if linkErr == nil {
				s.recordAudit(r, nil, "anonymous", "upload_collect", name, "success", "collection_upload")
				s.serviceEvent(r, "upload_collect", "anonymous", "name=%s size=%d owner=%s owner_id=%d collection=%d dir=uploads/%s result=success reason=collection_upload", name, input.Size, owner.Username, owner.ID, collection.ID, maskedCollectionToken(collection.Token))
				writeData(w, http.StatusOK, "上传完成", map[string]any{"instant": true, "file": publicFile(linked.File)})
				return
			}
			if errors.Is(linkErr, store.ErrCollectionLimit) || errors.Is(linkErr, store.ErrCollectionExpired) || errors.Is(linkErr, store.ErrCollectionRevoked) {
				rejectState(linkErr)
				return
			}
			log.Printf("record instant collection file: %v", linkErr)
			s.collectionFailure(w, r, name, "instant_save_failed", http.StatusInternalServerError, "保存上传记录失败", nil)
			return
		}
		if !errors.Is(matchErr, store.ErrNotFound) {
			log.Printf("find collection instant match: %v", matchErr)
			s.collectionFailure(w, r, name, "instant_check_failed", http.StatusInternalServerError, "检查文件失败", nil)
			return
		}
	}
	taskID, err := randomID()
	if err != nil {
		s.collectionFailure(w, r, name, "task_create_failed", http.StatusInternalServerError, "创建上传任务失败", nil)
		return
	}
	mimeType := strings.TrimSpace(input.Mime)
	if mimeType == "" {
		mimeType = mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	err = s.store.CreateCollectionUploadTask(r.Context(), store.UploadTask{ID: taskID, UserID: owner.ID, CollectionID: collection.ID, Remark: strings.TrimSpace(input.Remark), Name: name, Size: input.Size, ChunkSize: chunkSize, TotalChunks: totalChunks, Status: "pending", Mime: mimeType, StorageDir: storageDir}, token)
	if errors.Is(err, store.ErrCollectionLimit) || errors.Is(err, store.ErrCollectionExpired) || errors.Is(err, store.ErrCollectionRevoked) {
		rejectState(err)
		return
	}
	if err != nil {
		var quotaErr *store.QuotaError
		if errors.As(err, &quotaErr) {
			// 配额错误脱敏：匿名收集场景不返回 usedBytes/quotaBytes/fileSize 等内部配额明细，
			// 避免外部人员借此推断收集者账户的配额状态；登录用户普通上传仍保留 QUOTA_EXCEEDED 明细。
			// Quota errors are masked for anonymous collection uploads: internal quota details are
			// never exposed to outsiders; regular authenticated uploads keep the QUOTA_EXCEEDED detail.
			s.collectionFailure(w, r, name, "quota_exceeded", http.StatusRequestEntityTooLarge, "配额不足，请联系链接提供方", map[string]string{"code": "COLLECTION_QUOTA_EXCEEDED"})
			return
		}
		log.Printf("create collection upload task: %v", err)
		s.collectionFailure(w, r, name, "task_create_failed", http.StatusInternalServerError, "创建上传任务失败", nil)
		return
	}
	writeData(w, http.StatusOK, "上传任务已创建", map[string]any{"taskId": taskID, "chunkSize": chunkSize, "totalChunks": totalChunks, "uploadedChunks": []int{}})
}

// collectionUploadChunk writes one anonymous chunk after token/task validation.
// collectionUploadChunk 在 token 和任务校验后写入一个匿名分片。
func (s *Server) collectionUploadChunk(w http.ResponseWriter, r *http.Request) {
	collection, owner, task, err := s.loadCollectionTask(r)
	if err != nil {
		s.writeCollectionTaskError(w, r, err)
		return
	}
	indexValue := r.PathValue("index")
	if indexValue == "" {
		indexValue = r.URL.Query().Get("index")
	}
	index, err := strconv.Atoi(indexValue)
	if err != nil || index < 0 || index >= task.TotalChunks {
		s.collectionFailure(w, r, task.Name, "invalid_index", http.StatusBadRequest, "分片序号无效", nil)
		return
	}
	expectedSize := expectedUploadChunkSize(task, index)
	// 只读时段检查：收集者处于只读窗口时拒绝写临时分片（与 upload-init 的拦截一致），
	// 检查放在任何磁盘/数据库写入之前，避免留下无文件的分片记录。
	// Read-only check: refuse to write temp chunks while the collector is in a read-only window
	// (consistent with the upload-init guard); it runs before any disk/database write so no
	// orphan chunk records are left behind.
	if s.userReadOnly(owner) {
		s.collectionFailure(w, r, task.Name, "read_only", http.StatusForbidden, "收集者处于只读时段，暂不接受上传", map[string]string{"code": "READ_ONLY"})
		return
	}
	if r.ContentLength >= 0 && r.ContentLength > expectedSize {
		s.collectionFailure(w, r, task.Name, "too_large", http.StatusRequestEntityTooLarge, "上传内容超过声明大小", nil)
		return
	}
	settings, err := s.store.GetLogSettings(r.Context())
	if err != nil {
		log.Printf("get public upload settings: %v", err)
		s.collectionFailure(w, r, task.Name, "settings_failed", http.StatusInternalServerError, "读取上传设置失败", nil)
		return
	}
	if limiter := s.rateLimiter.limiterForPublic(s.requestIP(r)+"\x00"+collection.Token, settings.UploadRateLimit); limiter != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		err = waitForUploadRate(ctx, limiter, expectedSize)
		cancel()
		if err != nil {
			s.collectionFailure(w, r, task.Name, "rate_limited", http.StatusTooManyRequests, "上传过慢，请稍后重试", nil)
			return
		}
	}
	tmpDir := filepath.Join(s.config.DataDir, "tmp", task.ID)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		s.collectionFailure(w, r, task.Name, "prepare_failed", http.StatusInternalServerError, "无法准备上传空间", nil)
		return
	}
	path := filepath.Join(tmpDir, strconv.Itoa(index))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		s.collectionFailure(w, r, task.Name, "write_failed", http.StatusInternalServerError, "无法写入上传内容", nil)
		return
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(r.Body, expectedSize+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(path)
		s.collectionFailure(w, r, task.Name, "write_failed", http.StatusBadRequest, "写入上传内容失败", nil)
		return
	}
	if written > expectedSize {
		_ = os.Remove(path)
		s.collectionFailure(w, r, task.Name, "too_large", http.StatusRequestEntityTooLarge, "上传内容超过声明大小", nil)
		return
	}
	if written != expectedSize {
		_ = os.Remove(path)
		s.collectionFailure(w, r, task.Name, "size_mismatch", http.StatusBadRequest, "上传内容大小与声明不一致", nil)
		return
	}
	if err := s.store.SetChunk(r.Context(), task.ID, index, written, hex.EncodeToString(hash.Sum(nil))); err != nil {
		_ = os.Remove(path)
		log.Printf("set public upload chunk: %v", err)
		s.collectionFailure(w, r, task.Name, "save_failed", http.StatusInternalServerError, "保存上传分片失败", nil)
		return
	}
	writeData(w, http.StatusOK, "分片上传成功", map[string]any{"index": index, "size": written})
}

func (s *Server) writeCollectionTaskError(w http.ResponseWriter, r *http.Request, err error) {
	reason, status, message, data := "task_not_found", http.StatusNotFound, "上传任务不存在", any(nil)
	switch {
	case errors.Is(err, store.ErrCollectionExpired):
		reason, status, message, data = "collection_expired", http.StatusForbidden, "收集链接已过期", map[string]string{"code": "COLLECTION_EXPIRED"}
	case errors.Is(err, store.ErrCollectionRevoked):
		reason, status, message, data = "collection_revoked", http.StatusForbidden, "收集链接已撤销", map[string]string{"code": "COLLECTION_REVOKED"}
	case errors.Is(err, store.ErrCollectionLimit):
		reason, status, message, data = "collection_limit", http.StatusForbidden, "收集链接上传次数已用完", map[string]string{"code": "COLLECTION_LIMIT"}
	case !errors.Is(err, store.ErrNotFound):
		status, message = http.StatusInternalServerError, "读取上传任务失败"
	}
	s.collectionFailure(w, r, r.PathValue("taskID"), reason, status, message, data)
}

func (s *Server) collectionUploadStatus(w http.ResponseWriter, r *http.Request) {
	if !s.rateLimiter.allowPublicRequest(s.requestIP(r), 30, 10) {
		s.collectionFailure(w, r, r.PathValue("token"), "rate_limited", http.StatusTooManyRequests, "请求过于频繁，请稍后重试", nil)
		return
	}
	_, _, task, err := s.loadCollectionTask(r)
	if err != nil {
		s.writeCollectionTaskError(w, r, err)
		return
	}
	chunks, err := s.store.ListChunks(r.Context(), task.ID)
	if err != nil {
		log.Printf("list public upload chunks: %v", err)
		s.collectionFailure(w, r, task.ID, "status_failed", http.StatusInternalServerError, "读取上传进度失败", nil)
		return
	}
	indexes := make([]int, 0, len(chunks))
	for index := range chunks {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	writeData(w, http.StatusOK, "读取成功", map[string]any{"taskId": task.ID, "chunkSize": task.ChunkSize, "totalChunks": task.TotalChunks, "uploadedChunks": indexes})
}

// collectionUploadComplete merges chunks, verifies hashes, and records the remark.
// collectionUploadComplete 合并分片、校验哈希并记录上传备注。
func (s *Server) collectionUploadComplete(w http.ResponseWriter, r *http.Request) {
	if !s.rateLimiter.allowPublicRequest(s.requestIP(r), 30, 10) {
		s.collectionFailure(w, r, r.PathValue("token"), "rate_limited", http.StatusTooManyRequests, "请求过于频繁，请稍后重试", nil)
		return
	}
	var input collectionUploadCompleteRequest
	preloaded := false
	if r.PathValue("taskID") == "" && r.URL.Query().Get("taskId") == "" {
		if !decodeJSON(w, r, &input) {
			s.collectionFailure(w, r, r.PathValue("token"), "invalid_request", http.StatusBadRequest, "请求格式无效", nil)
			return
		}
		if input.TaskID == "" {
			s.collectionFailure(w, r, r.PathValue("token"), "task_not_found", http.StatusNotFound, "上传任务不存在", nil)
			return
		}
		query := r.URL.Query()
		query.Set("taskId", input.TaskID)
		r.URL.RawQuery = query.Encode()
		preloaded = true
	}
	collection, owner, task, err := s.loadCollectionTask(r)
	if err != nil {
		s.writeCollectionTaskError(w, r, err)
		return
	}
	auditReason := "upload_failed"
	defer func() {
		if auditReason == "" {
			s.recordAudit(r, nil, "anonymous", "upload_collect", task.Name, "success", "collection_upload")
			s.serviceEvent(r, "upload_collect", "anonymous", "name=%s size=%d owner=%s owner_id=%d collection=%d dir=uploads/%s result=success reason=collection_upload", task.Name, task.Size, owner.Username, owner.ID, collection.ID, maskedCollectionToken(collection.Token))
		} else {
			s.recordAudit(r, nil, "anonymous", "upload_collect_fail", task.Name, "failure", auditReason)
			s.serviceEvent(r, "upload_collect_fail", "anonymous", "name=%s size=%d owner=%s owner_id=%d collection=%d dir=uploads/%s result=failure reason=%s", task.Name, task.Size, owner.Username, owner.ID, collection.ID, maskedCollectionToken(collection.Token), auditReason)
		}
	}()
	if s.userReadOnly(owner) {
		auditReason = "read_only"
		writeErrorData(w, http.StatusForbidden, "收集者处于只读时段，暂不接受上传", map[string]string{"code": "READ_ONLY"})
		return
	}
	// 完成阶段重校验单文件上限：收集编辑可能下调 maxFileBytes，需拦截在编辑前已初始化的超限任务。
	// Re-check the single-file limit at completion: an edit may have lowered maxFileBytes, so pending
	// tasks initialized before the edit must be rejected here.
	if collection.MaxFileBytes > 0 && task.Size > collection.MaxFileBytes {
		auditReason = "collection_file_too_large"
		writeErrorData(w, http.StatusRequestEntityTooLarge, "文件超过收集链接单文件大小上限", map[string]any{"code": "COLLECTION_FILE_TOO_LARGE"})
		return
	}
	if !preloaded && r.Body != nil && r.ContentLength != 0 {
		if !decodeJSON(w, r, &input) {
			auditReason = "invalid_request"
			return
		}
	}
	if input.TaskID != "" && input.TaskID != task.ID {
		auditReason = "task_not_found"
		writeError(w, http.StatusNotFound, "上传任务不存在")
		return
	}
	chunks, err := s.store.ListChunks(r.Context(), task.ID)
	if err != nil {
		log.Printf("list public upload chunks: %v", err)
		auditReason = "chunks_read_failed"
		writeError(w, http.StatusInternalServerError, "读取上传分片失败")
		return
	}
	if len(chunks) != task.TotalChunks {
		auditReason = "incomplete"
		writeError(w, http.StatusBadRequest, "上传分片不完整")
		return
	}
	tmpDir := filepath.Join(s.config.DataDir, "tmp", task.ID)
	for index, chunk := range chunks {
		if index < 0 || index >= task.TotalChunks || chunk.Size != expectedUploadChunkSize(task, index) {
			auditReason = "incomplete"
			writeError(w, http.StatusBadRequest, "上传分片不完整")
			return
		}
		info, statErr := os.Stat(filepath.Join(tmpDir, strconv.Itoa(index)))
		if statErr != nil || info.Size() != chunk.Size {
			auditReason = "incomplete"
			writeError(w, http.StatusBadRequest, "上传分片不完整")
			return
		}
	}
	mergedPath := filepath.Join(tmpDir, ".merged")
	merged, err := os.OpenFile(mergedPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		auditReason = "merge_failed"
		writeError(w, http.StatusInternalServerError, "无法创建合并文件")
		return
	}
	defer os.Remove(mergedPath)
	sha := sha256.New()
	md5Hash := md5.New()
	var mergedSize int64
	for index := 0; index < task.TotalChunks; index++ {
		chunkFile, openErr := os.Open(filepath.Join(tmpDir, strconv.Itoa(index)))
		if openErr != nil {
			merged.Close()
			auditReason = "incomplete"
			writeError(w, http.StatusBadRequest, "上传分片不完整")
			return
		}
		written, copyErr := io.Copy(io.MultiWriter(merged, sha, md5Hash), chunkFile)
		closeErr := chunkFile.Close()
		if copyErr != nil || closeErr != nil {
			merged.Close()
			auditReason = "merge_failed"
			writeError(w, http.StatusInternalServerError, "合并上传内容失败")
			return
		}
		mergedSize += written
	}
	if err := merged.Sync(); err != nil {
		merged.Close()
		auditReason = "merge_failed"
		writeError(w, http.StatusInternalServerError, "保存合并文件失败")
		return
	}
	if err := merged.Close(); err != nil || mergedSize != task.Size {
		auditReason = "size_mismatch"
		writeError(w, http.StatusBadRequest, "上传分片不完整")
		return
	}
	shaHex, md5Hex := hex.EncodeToString(sha.Sum(nil)), hex.EncodeToString(md5Hash.Sum(nil))
	if (input.SHA256 != "" && !strings.EqualFold(input.SHA256, shaHex)) || (input.MD5 != "" && !strings.EqualFold(input.MD5, md5Hex)) {
		auditReason = "checksum_mismatch"
		writeError(w, http.StatusBadRequest, "文件校验值不匹配")
		return
	}
	storedName, err := sanitizeName(task.Name)
	if err != nil {
		auditReason = "invalid_name"
		writeError(w, http.StatusBadRequest, "文件名无效")
		return
	}
	finalPath := ""
	cleanupFinal := true
	defer func() {
		if cleanupFinal && finalPath != "" {
			_ = os.Remove(finalPath)
		}
		_ = os.RemoveAll(tmpDir)
	}()
	fileRecord := store.File{UserID: task.UserID, Name: task.Name, StoredName: storedName, Size: task.Size, Mime: task.Mime, SHA256: shaHex, MD5: md5Hex, StoragePath: task.StorageDir}
	completed, err := s.store.CompleteCollectionFileWithPlacement(r.Context(), task, fileRecord, func(storagePath string, replace bool) error {
		finalPath = filepath.Join(s.config.DataDir, storagePath)
		if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
			return err
		}
		if !replace {
			if _, err := os.Stat(finalPath); err == nil {
				return fmt.Errorf("storage path already exists")
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		return os.Rename(mergedPath, finalPath)
	}, task.Name, task.Remark)
	if err != nil {
		log.Printf("complete collection upload: %v", err)
		if errors.Is(err, store.ErrCollectionExpired) {
			auditReason = "collection_expired"
			writeErrorData(w, http.StatusForbidden, "收集链接已过期", map[string]string{"code": "COLLECTION_EXPIRED"})
			return
		}
		if errors.Is(err, store.ErrCollectionRevoked) {
			auditReason = "collection_revoked"
			writeErrorData(w, http.StatusForbidden, "收集链接已撤销", map[string]string{"code": "COLLECTION_REVOKED"})
			return
		}
		auditReason = "save_failed"
		writeError(w, http.StatusInternalServerError, "保存文件记录失败")
		return
	}
	if err := s.store.DeleteChunks(r.Context(), task.ID); err != nil {
		log.Printf("delete public upload chunks: %v", err)
	}
	cleanupFinal = false
	auditReason = ""
	writeData(w, http.StatusOK, "上传完成", publicFile(completed))
}
