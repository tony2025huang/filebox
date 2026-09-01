package httpapi

// FileBox↔FileBox 同步适配器：以保存的账号密码登录对方 FileBox HTTP API，
// push 走对方 upload-init/chunk/complete（含秒传与冲突策略映射），pull 走对方 download 后复用本地入库流程。
// The FileBox↔FileBox sync adapter logs into the remote FileBox HTTP API with the stored credentials,
// pushes via the remote upload-init/chunk/complete flow (with instant-upload and conflict mapping),
// and pulls via the remote download endpoint, reusing the local ingestion flow.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"filebox/internal/diskusage"
	"filebox/internal/store"
)

// fileBoxRemoteClient 封装对一个远端 FileBox 实例的 HTTP 调用；token 仅保存在内存。
// fileBoxRemoteClient wraps HTTP calls to one remote FileBox instance; the token lives in memory only.
type fileBoxRemoteClient struct {
	baseURL string
	token   string
	http    *http.Client
}

const (
	fileBoxChunkSize = 8 * 1024 * 1024
	fileBoxTimeout   = 120 * time.Second
)

// openFileBox 校验目标系统并登录远端 FileBox。
// openFileBox validates the remote system and logs into the remote FileBox.
func (s *Server) openFileBox(ctx context.Context, item store.RemoteSystem) (*fileBoxRemoteClient, error) {
	if item.Kind != "filebox" {
		return nil, errors.New("remote system is not filebox kind")
	}
	secret, err := s.decryptSyncSecret(item.AuthSecret)
	if err != nil {
		return nil, errors.New("invalid credentials")
	}
	base := strings.TrimSuffix(item.URL, "/")
	client := &fileBoxRemoteClient{
		baseURL: base,
		http:    &http.Client{Timeout: fileBoxTimeout},
	}
	loginBody, _ := json.Marshal(map[string]string{"username": item.Username, "password": secret})
	response, err := client.request(ctx, http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody), true)
	if err != nil {
		return nil, err
	}
	defer response.body.Close()
	var result struct {
		Data struct {
			Token        string `json:"token"`
			TOTPRequired bool   `json:"totpRequired"`
			TOTPSetup    bool   `json:"totpSetup"`
		} `json:"data"`
		Message string `json:"message"`
	}
	if response.status != http.StatusOK {
		return nil, fmt.Errorf("远端登录失败 (%d): %s", response.status, response.message)
	}
	if err := json.NewDecoder(response.body).Decode(&result); err != nil {
		return nil, errors.New("远端登录响应无效")
	}
	if result.Data.Token == "" {
		if result.Data.TOTPRequired || result.Data.TOTPSetup {
			return nil, errors.New("远端账号启用了动态验证码（TOTP），暂不支持同步")
		}
		return nil, errors.New("远端登录未返回令牌")
	}
	client.token = result.Data.Token
	return client, nil
}

// close 释放内存中的远端令牌（不调用远端登出，token 仅本地内存持有）。
// close drops the in-memory remote token (no remote logout is issued; the token is memory-only).
func (c *fileBoxRemoteClient) close() {
	c.token = ""
}

type rawResponse struct {
	status  int
	message string
	body    io.ReadCloser
}

// request 发送一个携带可选 Bearer token 的 JSON 请求，返回状态、消息与响应体。
// request performs one JSON request with an optional Bearer token, returning status, message, and body.
func (c *fileBoxRemoteClient) request(ctx context.Context, method, apiPath string, body io.Reader, authenticated bool) (rawResponse, error) {
	full := c.baseURL + apiPath
	req, err := http.NewRequestWithContext(ctx, method, full, body)
	if err != nil {
		return rawResponse{}, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if authenticated && c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.http.Do(req)
	if err != nil {
		return rawResponse{}, err
	}
	raw := rawResponse{status: response.StatusCode, body: response.Body}
	// 仅读取小段响应用于错误消息；正常响应体由调用方解码。
	// Only read a small chunk for the error message; callers decode the body themselves.
	if response.StatusCode >= 400 {
		limited := io.LimitReader(response.Body, 64*1024)
		data, readErr := io.ReadAll(limited)
		response.Body.Close()
		raw.body = nil
		if readErr == nil {
			var payload struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(data, &payload)
			raw.message = payload.Message
		}
		if raw.message == "" {
			raw.message = http.StatusText(response.StatusCode)
		}
	}
	return raw, nil
}

// doJSON 发送 JSON 请求并解码 {data: ...} 响应。
// doJSON sends a JSON request and decodes the {data: ...} response.
func (c *fileBoxRemoteClient) doJSON(ctx context.Context, method, apiPath string, payload any, out any, authenticated bool) error {
	var reader io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	response, err := c.request(ctx, method, apiPath, reader, authenticated)
	if err != nil {
		return err
	}
	defer func() { _ = response.body.Close() }()
	if response.status < 200 || response.status >= 300 {
		return fmt.Errorf("远端请求失败 (%d): %s", response.status, response.message)
	}
	if out != nil {
		if err := json.NewDecoder(response.body).Decode(out); err != nil {
			return errors.New("远端响应无效")
		}
	}
	return nil
}

type remoteBrowseEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
	Kind  string `json:"kind"`
	Size  int64  `json:"size"`
	ID    int64  `json:"id"`
}

// browse 列出远端目录直接子项。
// browse lists the direct children of a remote directory.
func (c *fileBoxRemoteClient) browse(ctx context.Context, remotePath string, includeFiles bool) ([]map[string]any, error) {
	query := url.Values{}
	if remotePath != "" && remotePath != "." {
		query.Set("path", remotePath)
	}
	if includeFiles {
		query.Set("includeFiles", "1")
	}
	var result struct {
		Data struct {
			Items []remoteBrowseEntry `json:"items"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/sync/browse-filebox?"+query.Encode(), nil, &result, true); err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(result.Data.Items))
	for _, entry := range result.Data.Items {
		items = append(items, map[string]any{"name": entry.Name, "path": entry.Path, "isDir": entry.IsDir, "kind": entry.Kind, "size": entry.Size, "id": entry.ID})
	}
	return items, nil
}

// ensureDir 按路径分段在远端 FileBox 逐级创建目录，已存在则忽略。
// ensureDir creates each path segment on the remote FileBox, ignoring existing folders.
func (c *fileBoxRemoteClient) ensureDir(ctx context.Context, remotePath string) error {
	if remotePath == "" || remotePath == "." || remotePath == "/" {
		return nil
	}
	segments := strings.Split(strings.Trim(remotePath, "/"), "/")
	parent := ""
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		var result struct {
			Data map[string]any `json:"data"`
		}
		err := c.doJSON(ctx, http.MethodPost, "/api/folders", map[string]any{"name": segment, "parent": parent}, &result, true)
		if err != nil {
			// 已存在（409）视为成功；其他错误返回。
			// A 409 conflict (already exists) counts as success; any other error is returned.
			if strings.Contains(err.Error(), "409") {
				parent = pathpkg.Join(parent, segment)
				continue
			}
			return err
		}
		parent = pathpkg.Join(parent, segment)
	}
	return nil
}

// findFile 在远端目录中按名称查找文件条目。
// findFile looks up a file entry by name inside a remote directory.
func (c *fileBoxRemoteClient) findFile(ctx context.Context, dir, name string) (*remoteBrowseEntry, error) {
	entries, err := c.browse(ctx, dir, true)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		isDir, ok := entry["isDir"].(bool)
		if !ok {
			continue
		}
		entryName, ok := entry["name"].(string)
		if !ok || isDir || entryName != name {
			continue
		}
		id, _ := entry["id"].(int64)
		size, _ := entry["size"].(int64)
		entryPath, ok := entry["path"].(string)
		if !ok {
			continue
		}
		return &remoteBrowseEntry{Name: name, Path: entryPath, IsDir: false, Kind: "file", Size: size, ID: id}, nil
	}
	return nil, nil
}

// walkFileBoxEntries 解析远端目录条目并跳过字段类型错误的响应。
// walkFileBoxEntries parses remote directory entries and skips responses with invalid field types.
func walkFileBoxEntries(entries []map[string]any, relative string, process func(remoteBrowseEntry, string) error, descend func(string, string) error, detail *[]string) error {
	for _, entry := range entries {
		name, ok := entry["name"].(string)
		if !ok {
			*detail = append(*detail, "remote entry skipped: invalid name")
			continue
		}
		childRelative := pathpkg.Join(relative, name)
		isDir, ok := entry["isDir"].(bool)
		if !ok {
			*detail = append(*detail, childRelative+": remote entry has invalid isDir")
			continue
		}
		if isDir {
			entryPath, ok := entry["path"].(string)
			if !ok {
				*detail = append(*detail, childRelative+": remote entry has invalid path")
				continue
			}
			if err := descend(entryPath, childRelative); err != nil {
				return err
			}
			continue
		}
		id, ok := entry["id"].(int64)
		if !ok {
			*detail = append(*detail, childRelative+": remote entry has invalid id")
			continue
		}
		size, ok := entry["size"].(int64)
		if !ok {
			*detail = append(*detail, childRelative+": remote entry has invalid size")
			continue
		}
		if err := process(remoteBrowseEntry{ID: id, Name: name, Size: size}, childRelative); err != nil {
			return err
		}
	}
	return nil
}

// pushFile 将本地文件推送到远端 FileBox（秒传/冲突策略由远端校验），返回是否成功及错误详情。
// pushFile pushes one local file to the remote FileBox (instant upload and conflict policy handled remotely).
func (c *fileBoxRemoteClient) pushFile(ctx context.Context, localPath, name, dir, resolve string, size int64) error {
	handle, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer handle.Close()
	chunkSize := size
	if size > fileBoxChunkSize {
		chunkSize = fileBoxChunkSize
	}
	initPayload := map[string]any{"name": name, "size": size, "chunkSize": chunkSize, "dir": dir, "resolve": resolve}
	if resolve == "" {
		delete(initPayload, "resolve")
	}
	if dir == "" {
		delete(initPayload, "dir")
	}
	var initResult struct {
		Data struct {
			Instant        bool   `json:"instant"`
			TaskID         string `json:"taskId"`
			ChunkSize      int64  `json:"chunkSize"`
			TotalChunks    int    `json:"totalChunks"`
			UploadedChunks []int  `json:"uploadedChunks"`
			Conflict       bool   `json:"conflict"`
			Existing       any    `json:"existing"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/files/upload-init", initPayload, &initResult, true); err != nil {
		return err
	}
	if initResult.Data.Instant {
		return nil
	}
	if initResult.Data.TaskID == "" {
		return errors.New("远端未返回上传任务")
	}
	buffer := make([]byte, chunkSize)
	for index := 0; index < initResult.Data.TotalChunks; index++ {
		start := int64(index) * chunkSize
		read, readErr := handle.ReadAt(buffer, start)
		if readErr != nil && readErr != io.EOF {
			return readErr
		}
		response, reqErr := c.request(ctx, http.MethodPut, fmt.Sprintf("/api/files/%s/chunks/%d", url.PathEscape(initResult.Data.TaskID), index), bytes.NewReader(buffer[:read]), true)
		if reqErr != nil {
			return reqErr
		}
		_ = response.body.Close()
		if response.status != http.StatusOK {
			return fmt.Errorf("远端分片上传失败 (%d): %s", response.status, response.message)
		}
	}
	var completeResult struct {
		Data map[string]any `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/api/files/%s/complete", url.PathEscape(initResult.Data.TaskID)), map[string]any{"sha256": ""}, &completeResult, true); err != nil {
		return err
	}
	return nil
}

// downloadFile 将远端文件流式下载到本地临时文件，返回临时路径与大小。
// downloadFile streams a remote file into a local temp file and returns its path and size.
func (c *fileBoxRemoteClient) downloadFile(ctx context.Context, fileID int64, dataDir string) (string, int64, error) {
	tempDir := filepath.Join(dataDir, "tmp")
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return "", 0, err
	}
	temp, err := os.CreateTemp(tempDir, "sync-filebox-download-*")
	if err != nil {
		return "", 0, err
	}
	tempPath := temp.Name()
	response, err := c.request(ctx, http.MethodGet, fmt.Sprintf("/api/files/%d/download", fileID), nil, true)
	if err != nil {
		temp.Close()
		_ = os.Remove(tempPath)
		return "", 0, err
	}
	defer response.body.Close()
	if response.status != http.StatusOK {
		_ = temp.Close()
		_ = os.Remove(tempPath)
		return "", 0, fmt.Errorf("远端下载失败 (%d): %s", response.status, response.message)
	}
	written, copyErr := io.Copy(temp, response.body)
	closeErr := temp.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(tempPath)
		return "", 0, errors.New("保存远端文件失败")
	}
	return tempPath, written, nil
}

// executeSyncPushFileBox 将本地 FileBox 目录/文件推送到远端 FileBox。
// executeSyncPushFileBox pushes local FileBox files/directories to a remote FileBox.
func (s *Server) executeSyncPushFileBox(ctx context.Context, task store.SyncTask, system store.RemoteSystem) syncRunResult {
	result := syncRunResult{}
	client, err := s.openFileBox(ctx, system)
	if err != nil {
		result.message = "无法连接目标系统"
		result.detail = []string{"同步连接失败: " + s.syncErrorDetail(err)}
		return result
	}
	defer client.close()
	files, err := s.store.ListReadyFilesUnder(ctx, task.UserID, task.SourcePath)
	if err != nil {
		result.message = "读取 FileBox 源文件失败"
		result.detail = append(result.detail, "读取源文件失败: "+s.syncErrorDetail(err))
		return result
	}
	s.syncProgressSet(task.ID, func(p *syncRunProgress) { p.TotalFiles = len(files) })
	sourceKind := task.SourceKind
	if sourceKind == "" {
		sourceKind = "directory"
	}
	if sourceKind == "file" && len(files) == 0 {
		// 源为单文件且已不存在：记录失败，避免"0 files 后报成功"。
		// A single-file source that no longer exists records a failure instead of a misleading "0 files" success.
		result.message = "源文件不存在"
		result.detail = append(result.detail, task.SourcePath+": 源文件已被删除或不可用")
		return result
	}
	if err := client.ensureDir(ctx, task.TargetPath); err != nil {
		result.message = "创建远端目录失败"
		result.detail = append(result.detail, "创建目标目录失败: "+s.syncErrorDetail(err))
		return result
	}
	settings, settingsErr := s.store.GetLogSettings(ctx)
	if settingsErr != nil {
		result.message = "读取传输设置失败"
		return result
	}
	limiter := s.rateLimiter.limiterFor(task.UserID, settings.UploadRateLimit)
	root := filepath.ToSlash(filepath.Join("files", strconv.FormatInt(task.UserID, 10))) + "/"
	for _, file := range files {
		s.syncProgressSet(task.ID, func(p *syncRunProgress) { p.CurrentFile = file.Name })
		relative := filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(file.StoragePath), root))
		if task.SourcePath != "" {
			if relative == task.SourcePath {
				relative = pathpkg.Base(relative)
			} else {
				relative = strings.TrimPrefix(relative, strings.TrimSuffix(task.SourcePath, "/")+"/")
			}
		}
		if sourceKind == "file" && relative != pathpkg.Base(task.SourcePath) {
			continue
		}
		remoteDir := task.TargetPath
		remoteName := pathpkg.Base(relative)
		dirParts := strings.Split(strings.TrimSuffix(relative, "/"+remoteName), "/")
		if len(dirParts) > 0 && dirParts[0] != "" {
			remoteDir = pathpkg.Join(remoteDir, pathpkg.Join(dirParts...))
		}
		if remoteDir == "." || remoteDir == "" {
			remoteDir = "."
		}
		localPath := filepath.Join(s.config.DataDir, filepath.FromSlash(file.StoragePath))
		if limiter != nil {
			if rateErr := waitForUploadRate(ctx, limiter, file.Size); rateErr != nil {
				result.message = "上传文件失败"
				result.detail = append(result.detail, relative+": 传输被取消或限速等待超时")
				continue
			}
		}
		resolve := ""
		switch task.ConflictPolicy {
		case "overwrite":
			resolve = "overwrite"
		case "rename":
			resolve = "rename"
		default: // skip
			existing, findErr := client.findFile(ctx, remoteDir, remoteName)
			if findErr != nil {
				result.message = "检查远端文件失败"
				result.detail = append(result.detail, relative+": "+s.syncErrorDetail(findErr))
				continue
			}
			if existing != nil {
				result.detail = append(result.detail, relative+": skipped (exists)")
				continue
			}
		}
		if err := client.ensureDir(ctx, remoteDir); err != nil {
			result.message = "创建远端目录失败"
			result.detail = append(result.detail, relative+": "+s.syncErrorDetail(err))
			continue
		}
		if err := client.pushFile(ctx, localPath, remoteName, remoteDir, resolve, file.Size); err != nil {
			result.message = "上传文件失败"
			result.detail = append(result.detail, relative+": "+s.syncErrorDetail(err))
			continue
		}
		result.files++
		result.bytes += file.Size
		result.detail = append(result.detail, relative+": uploaded ("+strconv.FormatInt(file.Size, 10)+" bytes)")
		s.syncProgressSet(task.ID, func(p *syncRunProgress) { p.DoneFiles++; p.TransferredBytes += file.Size })
	}
	return result
}

// executeSyncPullFileBox 从远端 FileBox 拉取目录/文件到本地 FileBox。
// executeSyncPullFileBox pulls remote FileBox files/directories into the local FileBox.
func (s *Server) executeSyncPullFileBox(ctx context.Context, task store.SyncTask, system store.RemoteSystem) syncRunResult {
	result := syncRunResult{}
	client, err := s.openFileBox(ctx, system)
	if err != nil {
		result.message = "无法连接目标系统"
		result.detail = []string{"同步连接失败: " + s.syncErrorDetail(err)}
		return result
	}
	defer client.close()
	process := func(entry remoteBrowseEntry, relative string) error {
		s.syncProgressSet(task.ID, func(p *syncRunProgress) { p.CurrentFile = entry.Name })
		if entry.Size > s.config.MaxFileSize {
			result.detail = append(result.detail, relative+": skipped (exceeds max file size)")
			return nil
		}
		if s.config.MinFreeSpace > 0 {
			_, free, _, diskErr := diskusage.DiskUsage(s.config.DataDir)
			if diskErr == nil && free < s.config.MinFreeSpace {
				result.detail = append(result.detail, relative+": skipped (insufficient disk space)")
				return nil
			}
		}
		parts := strings.Split(relative, "/")
		name, nameErr := safeSyncFileName(parts[len(parts)-1])
		if nameErr != nil {
			return nameErr
		}
		localDir := task.TargetPath
		if len(parts) > 1 {
			localDir = strings.Trim(strings.Join(append([]string{task.TargetPath}, parts[:len(parts)-1]...), "/"), "/")
		}
		if folderErr := s.store.EnsureFolderPath(ctx, task.UserID, localDir); folderErr != nil {
			return folderErr
		}
		storageDir := filepath.Join("files", strconv.FormatInt(task.UserID, 10), filepath.FromSlash(localDir))
		if task.ConflictPolicy == "skip" {
			if _, conflictErr := s.store.FindUploadConflict(ctx, task.UserID, storageDir, name); conflictErr == nil {
				result.detail = append(result.detail, relative+": skipped (exists)")
				return nil
			} else if !errors.Is(conflictErr, store.ErrNotFound) {
				return conflictErr
			}
		}
		tempPath, size, downloadErr := client.downloadFile(ctx, entry.ID, s.config.DataDir)
		if downloadErr != nil {
			return downloadErr
		}
		cleanup := true
		defer func() {
			if cleanup {
				_ = os.Remove(tempPath)
			}
		}()
		if size != entry.Size {
			return fmt.Errorf("size mismatch: got %d want %d", size, entry.Size)
		}
		shaHex, md5Hex, hashErr := hashSyncFile(tempPath)
		if hashErr != nil {
			return hashErr
		}
		resolve := ""
		if task.ConflictPolicy == "overwrite" {
			resolve = "overwrite"
		} else if task.ConflictPolicy == "rename" {
			resolve = "rename"
		}
		mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		uploadTask := store.UploadTask{ID: newSyncTaskID(), UserID: task.UserID, Name: name, Size: size, ChunkSize: size, TotalChunks: 1, Status: "pending", Mime: mimeType, StorageDir: storageDir, Resolve: resolve}
		if createErr := s.store.CreateUploadTask(ctx, uploadTask); createErr != nil {
			return createErr
		}
		file := store.File{UserID: task.UserID, Name: name, StoredName: name, Size: size, Mime: mimeType, SHA256: shaHex, MD5: md5Hex, StoragePath: storageDir}
		if _, completeErr := s.store.CompleteUploadWithPlacement(ctx, uploadTask, file, func(storagePath string, replace bool) error {
			finalPath := filepath.Join(s.config.DataDir, filepath.FromSlash(storagePath))
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
			return os.Rename(tempPath, finalPath)
		}); completeErr != nil {
			if deleteErr := s.store.DeleteUploadTask(ctx, uploadTask.ID); deleteErr != nil {
				log.Printf("rollback filebox sync upload task %s after complete failure: %v", uploadTask.ID, deleteErr)
			}
			return completeErr
		}
		cleanup = false
		result.files++
		result.bytes += size
		result.detail = append(result.detail, relative+": downloaded ("+strconv.FormatInt(size, 10)+" bytes)")
		s.syncProgressSet(task.ID, func(p *syncRunProgress) { p.DoneFiles++; p.TransferredBytes += size })
		return nil
	}
	// 递归收集远端目录树（逐层 browse，MVP 不做深度限制之外的保护）。
	// Collect the remote directory tree recursively via per-level browse.
	var walk func(remotePath, relative string) error
	walk = func(remotePath, relative string) error {
		entries, err := client.browse(ctx, remotePath, true)
		if err != nil {
			return err
		}
		return walkFileBoxEntries(entries, relative, process, walk, &result.detail)
	}
	if err := walk(task.SourcePath, ""); err != nil {
		result.message = "拉取远端文件失败"
		result.detail = append(result.detail, task.SourcePath+": "+s.syncErrorDetail(err))
	}
	if result.message != "" {
		return result
	}
	return result
}
