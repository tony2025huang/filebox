package httpapi

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/pkg/sftp"
	"github.com/robfig/cron/v3"
	"golang.org/x/crypto/ssh"

	"filebox/internal/diskusage"
	"filebox/internal/store"
)

// syncSystemRequest 是目标系统 API 请求体；authSecret 只用于写入或更新凭据。
// syncSystemRequest is the remote-system API body; authSecret is write-only credential input.
type syncSystemRequest struct {
	Name               string `json:"name"`
	Kind               string `json:"kind"`
	Host               string `json:"host"`
	URL                string `json:"url"`
	Port               int    `json:"port"`
	Username           string `json:"username"`
	AuthType           string `json:"authType"`
	AuthSecret         string `json:"authSecret"`
	AuthPassphrase     string `json:"authPassphrase"`
	HostKeyFingerprint string `json:"hostKeyFingerprint"`
}

type syncTaskRequest struct {
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
}

type syncMkdirRequest struct {
	Path string `json:"path"`
}

func publicSyncSystem(item store.RemoteSystem) map[string]any {
	kind := item.Kind
	if kind == "" {
		kind = "sftp"
	}
	return map[string]any{"id": item.ID, "name": item.Name, "kind": kind, "host": item.Host, "url": item.URL, "port": item.Port, "username": item.Username, "authType": item.AuthType, "hostKeyFingerprint": item.HostKeyFingerprint, "hasCredentials": item.AuthSecret != "", "taskCount": item.TaskCount, "createdAt": item.CreatedAt}
}

func publicSyncTask(item store.SyncTask) map[string]any {
	sourceKind := item.SourceKind
	if sourceKind == "" {
		sourceKind = "directory"
	}
	return map[string]any{"id": item.ID, "userId": item.UserID, "name": item.Name, "direction": item.Direction, "remoteSystemId": item.RemoteSystemID, "sourceType": item.SourceType, "sourcePath": item.SourcePath, "sourceKind": sourceKind, "targetType": item.TargetType, "targetPath": item.TargetPath, "conflictPolicy": item.ConflictPolicy, "scheduleType": item.ScheduleType, "cron": item.Cron, "enabled": item.Enabled, "lastRunAt": item.LastRunAt, "lastResult": item.LastResult, "createdAt": item.CreatedAt}
}

func publicSyncLog(item store.SyncLog) map[string]any {
	return map[string]any{"id": item.ID, "taskId": item.TaskID, "runAt": item.RunAt, "direction": item.Direction, "result": item.Result, "files": item.Files, "bytes": item.Bytes, "message": item.Message, "detail": item.Detail}
}

func parseSyncID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid sync id")
	}
	return id, nil
}

func validateSyncSystemInput(input syncSystemRequest, requireSecret bool) (syncSystemRequest, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Host = strings.TrimSpace(input.Host)
	input.URL = strings.TrimSpace(strings.ReplaceAll(input.URL, "\\", "/"))
	input.Username = strings.TrimSpace(input.Username)
	input.AuthType = strings.TrimSpace(input.AuthType)
	input.Kind = strings.TrimSpace(input.Kind)
	input.HostKeyFingerprint = strings.TrimSpace(input.HostKeyFingerprint)
	if input.Kind == "" {
		input.Kind = "sftp"
	}
	if input.Kind != "sftp" && input.Kind != "filebox" {
		return input, errors.New("invalid remote system kind")
	}
	if input.Port == 0 {
		input.Port = 22
	}
	if input.Name == "" || len([]byte(input.Name)) > 255 || input.Username == "" || len([]byte(input.Username)) > 255 || input.Port < 1 || input.Port > 65535 {
		return input, errors.New("invalid remote system")
	}
	if input.Kind == "filebox" {
		// FileBox 远端仅支持密码认证，且必须有合法的 http(s) 基础 URL（SSRF 防护：禁内嵌账号密码）。
		// A remote FileBox uses password auth and a valid http(s) base URL (SSRF guard: no embedded credentials).
		if input.AuthType != "password" {
			return input, errors.New("invalid auth type")
		}
		if strings.Contains(input.Host, "://") || strings.ContainsAny(input.Host, "\r\n\x00") {
			return input, errors.New("invalid remote system")
		}
		parsed, err := url.Parse(input.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || strings.ContainsAny(input.URL, "\r\n\x00") {
			return input, errors.New("invalid remote system")
		}
		input.HostKeyFingerprint = ""
		input.AuthPassphrase = ""
	} else {
		if input.Host == "" || len([]byte(input.Host)) > 255 {
			return input, errors.New("invalid remote system")
		}
		if strings.ContainsAny(input.Host, "\r\n\x00") {
			return input, errors.New("invalid remote system")
		}
		input.URL = ""
	}
	if strings.ContainsAny(input.Username, "\r\n\x00") {
		return input, errors.New("invalid remote system")
	}
	if input.AuthType != "password" && input.AuthType != "key" {
		return input, errors.New("invalid auth type")
	}
	if requireSecret && input.AuthSecret == "" {
		return input, errors.New("missing credentials")
	}
	if len([]byte(input.AuthSecret)) > 512*1024 || len([]byte(input.AuthPassphrase)) > 4096 {
		return input, errors.New("credentials too large")
	}
	if len([]byte(input.HostKeyFingerprint)) > 512 {
		return input, errors.New("invalid remote system")
	}
	if input.HostKeyFingerprint != "" {
		// 严格解码：base64 尾部 padding 位必须为零，避免"低 2 bit 变化仍匹配"的指纹伪造。
		// Strict decoding rejects non-zero trailing base64 padding bits so corrupted fingerprints never match.
		if decoded, valid := decodeFingerprintPayload(input.HostKeyFingerprint); !valid || len(decoded) != sha256.Size {
			return input, errors.New("invalid remote system")
		}
	}
	if input.AuthType == "password" {
		input.AuthPassphrase = ""
	}
	return input, nil
}

func (s *Server) createSyncSystem(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	if s.rejectReadOnly(w, r, user, "sync_system_create", "") {
		return
	}
	var input syncSystemRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	var err error
	input, err = validateSyncSystemInput(input, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "目标系统参数无效")
		return
	}
	secret, err := s.encryptSyncSecret(input.AuthSecret)
	if err != nil {
		log.Printf("encrypt sync credentials: %v", err)
		writeError(w, http.StatusInternalServerError, "保存目标系统失败")
		return
	}
	passphrase, err := s.encryptSyncSecret(input.AuthPassphrase)
	if err != nil {
		log.Printf("encrypt sync passphrase: %v", err)
		writeError(w, http.StatusInternalServerError, "保存目标系统失败")
		return
	}
	item, err := s.store.CreateRemoteSystem(r.Context(), store.RemoteSystem{UserID: user.ID, Name: input.Name, Kind: input.Kind, Host: input.Host, URL: input.URL, Port: input.Port, Username: input.Username, AuthType: input.AuthType, AuthSecret: secret, AuthPassphrase: passphrase, HostKeyFingerprint: input.HostKeyFingerprint})
	if err != nil {
		log.Printf("create sync remote system: %v", err)
		writeError(w, http.StatusInternalServerError, "创建目标系统失败")
		return
	}
	s.serviceEvent(r, "sync_system_create", user.Username, "target=%d result=success", item.ID)
	writeData(w, http.StatusCreated, "目标系统已创建", publicSyncSystem(item))
}

func (s *Server) listSyncSystems(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	items, err := s.store.ListRemoteSystems(r.Context(), user.ID, user.Role == "admin")
	if err != nil {
		log.Printf("list sync remote systems: %v", err)
		writeError(w, http.StatusInternalServerError, "获取目标系统失败")
		return
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, publicSyncSystem(item))
	}
	writeData(w, http.StatusOK, "获取成功", map[string]any{"items": result})
}

func (s *Server) updateSyncSystem(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	id, err := parseSyncID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "目标系统不存在")
		return
	}
	if s.rejectReadOnly(w, r, user, "sync_system_update", r.PathValue("id")) {
		return
	}
	existing, err := s.store.GetRemoteSystem(r.Context(), id, user.ID, user.Role == "admin")
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "目标系统不存在")
		return
	}
	if err != nil {
		log.Printf("get sync remote system: %v", err)
		writeError(w, http.StatusInternalServerError, "读取目标系统失败")
		return
	}
	var input syncSystemRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.AuthSecret == "" {
		input.AuthSecret = "__keep__"
	}
	input, err = validateSyncSystemInput(input, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "目标系统参数无效")
		return
	}
	secret := existing.AuthSecret
	passphrase := existing.AuthPassphrase
	fingerprint := existing.HostKeyFingerprint
	if input.HostKeyFingerprint != "" {
		fingerprint = input.HostKeyFingerprint
	}
	if input.AuthSecret != "__keep__" {
		secret, err = s.encryptSyncSecret(input.AuthSecret)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "保存目标系统失败")
			return
		}
		passphrase, err = s.encryptSyncSecret(input.AuthPassphrase)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "保存目标系统失败")
			return
		}
	} else if input.AuthType != existing.AuthType {
		writeError(w, http.StatusBadRequest, "修改认证方式时必须提供凭据")
		return
	}
	if input.AuthType == "password" {
		passphrase = ""
	}
	if err := s.store.UpdateRemoteSystem(r.Context(), store.RemoteSystem{ID: id, Name: input.Name, Kind: input.Kind, Host: input.Host, URL: input.URL, Port: input.Port, Username: input.Username, AuthType: input.AuthType, AuthSecret: secret, AuthPassphrase: passphrase, HostKeyFingerprint: fingerprint}, user.ID, user.Role == "admin"); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "目标系统不存在")
		return
	} else if err != nil {
		log.Printf("update sync remote system: %v", err)
		writeError(w, http.StatusInternalServerError, "保存目标系统失败")
		return
	}
	updated, err := s.store.GetRemoteSystem(r.Context(), id, user.ID, user.Role == "admin")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取目标系统失败")
		return
	}
	s.serviceEvent(r, "sync_system_update", user.Username, "target=%d result=success", id)
	writeData(w, http.StatusOK, "目标系统已更新", publicSyncSystem(updated))
}

func (s *Server) deleteSyncSystem(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	id, err := parseSyncID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "目标系统不存在")
		return
	}
	if s.rejectReadOnly(w, r, user, "sync_system_delete", r.PathValue("id")) {
		return
	}
	references, err := s.store.DeleteRemoteSystem(r.Context(), id, user.ID, user.Role == "admin")
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "目标系统不存在")
		return
	}
	if errors.Is(err, store.ErrSyncReferenced) {
		writeErrorData(w, http.StatusConflict, "目标系统仍被同步任务引用", map[string]any{"code": "SYNC_SYSTEM_REFERENCED", "references": references})
		return
	}
	if err != nil {
		log.Printf("delete sync remote system: %v", err)
		writeError(w, http.StatusInternalServerError, "删除目标系统失败")
		return
	}
	s.serviceEvent(r, "sync_system_delete", user.Username, "target=%d result=success", id)
	writeData(w, http.StatusOK, "目标系统已删除", nil)
}

func (s *Server) loadSyncSystem(r *http.Request) (store.RemoteSystem, error) {
	user := currentUser(r.Context())
	id, err := parseSyncID(r.PathValue("id"))
	if err != nil {
		return store.RemoteSystem{}, store.ErrNotFound
	}
	return s.store.GetRemoteSystem(r.Context(), id, user.ID, user.Role == "admin")
}

// browseSyncSystem 列出远端直接子项；includeFiles=true 时同时返回文件（源端选择用）。
// browseSyncSystem lists direct children of a remote path; includeFiles=true also returns files (for source picking).
func (s *Server) browseSyncSystem(w http.ResponseWriter, r *http.Request) {
	item, err := s.loadSyncSystem(r)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "目标系统不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取目标系统失败")
		return
	}
	remotePath, err := validateRemotePath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "远端目录无效")
		return
	}
	includeFiles := r.URL.Query().Get("includeFiles") == "1" || r.URL.Query().Get("includeFiles") == "true"
	items, err := s.browseRemoteEntries(r.Context(), item, remotePath, includeFiles)
	if err != nil {
		writeError(w, http.StatusBadGateway, "读取远端目录失败")
		return
	}
	writeData(w, http.StatusOK, "获取成功", map[string]any{"path": remotePath, "items": items})
}

// browseRemoteEntries 按目标系统类型（SFTP/FileBox）返回远端直接子项。
// browseRemoteEntries returns direct remote children by remote-system kind (SFTP/FileBox).
func (s *Server) browseRemoteEntries(ctx context.Context, item store.RemoteSystem, remotePath string, includeFiles bool) ([]map[string]any, error) {
	if item.Kind == "filebox" {
		client, err := s.openFileBox(ctx, item)
		if err != nil {
			return nil, err
		}
		defer client.close()
		return client.browse(ctx, remotePath, includeFiles)
	}
	client, closeClient, err := s.openSFTP(ctx, item)
	if err != nil {
		return nil, err
	}
	defer closeClient()
	entries, err := client.ReadDir(remotePath)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		if !includeFiles && !entry.IsDir() {
			continue
		}
		items = append(items, map[string]any{
			"name": entry.Name(), "path": pathpkg.Join(remotePath, entry.Name()),
			"isDir": entry.IsDir(), "kind": entryKind(entry.IsDir()), "size": entry.Size(),
		})
	}
	return items, nil
}

func entryKind(isDir bool) string {
	if isDir {
		return "directory"
	}
	return "file"
}

// browseLocalFileBox 返回本地 FileBox 目录的直接子文件夹与文件（同步源端选择用）。
// browseLocalFileBox lists the direct child folders and files of a local FileBox path (for sync source picking).
func (s *Server) browseLocalFileBox(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	path, err := validateFileBoxSyncPath(r.URL.Query().Get("path"), true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "目录无效")
		return
	}
	includeFiles := r.URL.Query().Get("includeFiles") == "1" || r.URL.Query().Get("includeFiles") == "true"
	folders, err := s.store.ListFolders(r.Context(), user.ID)
	if err != nil {
		log.Printf("list folders for sync browse: %v", err)
		writeError(w, http.StatusInternalServerError, "获取目录失败")
		return
	}
	items := make([]map[string]any, 0)
	for _, folder := range folders {
		parent := ""
		if index := strings.LastIndex(folder.Path, "/"); index >= 0 {
			parent = folder.Path[:index]
		}
		if parent == path {
			items = append(items, map[string]any{"name": folder.Name, "path": folder.Path, "isDir": true, "kind": "directory", "size": 0, "id": folder.ID})
		}
	}
	if includeFiles {
		files, err := s.store.ListDirectChildFiles(r.Context(), user.ID, path)
		if err != nil {
			log.Printf("list files for sync browse: %v", err)
			writeError(w, http.StatusInternalServerError, "获取文件列表失败")
			return
		}
		for _, file := range files {
			relative := filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(file.StoragePath), filepath.ToSlash(filepath.Join("files", strconv.FormatInt(user.ID, 10)))+"/"))
			items = append(items, map[string]any{"name": file.Name, "path": relative, "isDir": false, "kind": "file", "size": file.Size, "id": file.ID})
		}
	}
	writeData(w, http.StatusOK, "获取成功", map[string]any{"path": path, "items": items})
}

func (s *Server) mkdirSyncSystem(w http.ResponseWriter, r *http.Request) {
	item, err := s.loadSyncSystem(r)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "目标系统不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取目标系统失败")
		return
	}
	var input syncMkdirRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	remotePath, err := validateRemotePath(input.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "远端目录无效")
		return
	}
	if item.Kind == "filebox" {
		client, openErr := s.openFileBox(r.Context(), item)
		if openErr != nil {
			writeError(w, http.StatusBadGateway, "无法连接目标系统")
			return
		}
		defer client.close()
		if mkdirErr := client.ensureDir(r.Context(), remotePath); mkdirErr != nil {
			writeError(w, http.StatusBadGateway, "创建远端目录失败")
			return
		}
		writeData(w, http.StatusOK, "远端目录已创建", map[string]any{"path": remotePath})
		return
	}
	client, closeClient, err := s.openSFTP(r.Context(), item)
	if err != nil {
		writeError(w, http.StatusBadGateway, "无法连接目标系统")
		return
	}
	defer closeClient()
	if err := client.MkdirAll(remotePath); err != nil {
		writeError(w, http.StatusBadGateway, "创建远端目录失败")
		return
	}
	writeData(w, http.StatusOK, "远端目录已创建", map[string]any{"path": remotePath})
}

func validateFileBoxSyncPath(value string, allowEmpty bool) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" {
		if allowEmpty {
			return "", nil
		}
		return "", errors.New("empty path")
	}
	if strings.HasPrefix(value, "/") || strings.Contains(value, ":") || strings.ContainsRune(value, '\x00') {
		return "", errors.New("invalid local path")
	}
	parts := strings.Split(value, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("invalid local path")
		}
		for _, char := range part {
			if unicode.IsControl(char) || strings.ContainsRune(`<>:"|?*`, char) {
				return "", errors.New("invalid local path")
			}
		}
	}
	if len([]byte(value)) > 1024 {
		return "", errors.New("local path too long")
	}
	return strings.Join(parts, "/"), nil
}

func validateRemotePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return ".", nil
	}
	if strings.ContainsAny(value, "\\\x00\r\n") {
		return "", errors.New("invalid remote path")
	}
	cleaned := pathpkg.Clean(value)
	for _, part := range strings.Split(strings.TrimPrefix(cleaned, "/"), "/") {
		if part == ".." {
			return "", errors.New("invalid remote path")
		}
	}
	return cleaned, nil
}

func validateSyncTaskInput(input syncTaskRequest) (syncTaskRequest, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Direction = strings.TrimSpace(input.Direction)
	input.SourceType = strings.TrimSpace(input.SourceType)
	input.SourceKind = strings.TrimSpace(input.SourceKind)
	input.TargetType = strings.TrimSpace(input.TargetType)
	input.ConflictPolicy = strings.TrimSpace(input.ConflictPolicy)
	input.ScheduleType = strings.TrimSpace(input.ScheduleType)
	input.Cron = strings.TrimSpace(input.Cron)
	if input.Name == "" || len([]byte(input.Name)) > 255 || input.RemoteSystemID <= 0 {
		return input, errors.New("invalid sync task")
	}
	if input.Direction != "push" && input.Direction != "pull" || input.ConflictPolicy != "overwrite" && input.ConflictPolicy != "skip" && input.ConflictPolicy != "rename" || input.ScheduleType != "once" && input.ScheduleType != "periodic" {
		return input, errors.New("invalid sync task")
	}
	// 方向矩阵：push 以本地 FileBox 为源（目标为 SFTP 或远端 FileBox）；pull 以本地 FileBox 为目标（源为 SFTP 或远端 FileBox）。
	// Direction matrix: push uses local FileBox as the source (target SFTP or remote FileBox); pull targets local FileBox (source SFTP or remote FileBox).
	if input.Direction == "push" && (input.SourceType != "filebox" || input.TargetType != "sftp" && input.TargetType != "filebox") ||
		input.Direction == "pull" && (input.SourceType != "sftp" && input.SourceType != "filebox" || input.TargetType != "filebox") {
		return input, errors.New("invalid sync endpoints")
	}
	if input.SourceKind != "" && input.SourceKind != "directory" && input.SourceKind != "file" {
		return input, errors.New("invalid sync task")
	}
	var err error
	if input.SourceType == "filebox" {
		input.SourcePath, err = validateFileBoxSyncPath(input.SourcePath, true)
	} else {
		input.SourcePath, err = validateRemotePath(input.SourcePath)
	}
	if err != nil {
		return input, err
	}
	if input.TargetType == "filebox" {
		input.TargetPath, err = validateFileBoxSyncPath(input.TargetPath, true)
	} else {
		input.TargetPath, err = validateRemotePath(input.TargetPath)
	}
	if err != nil {
		return input, err
	}
	if input.ScheduleType == "periodic" {
		if input.Cron == "" {
			return input, errors.New("missing cron")
		}
		if _, err := cron.ParseStandard(input.Cron); err != nil {
			return input, errors.New("invalid cron")
		}
	} else {
		input.Cron = ""
	}
	return input, nil
}

func syncTaskFromRequest(input syncTaskRequest, userID int64) store.SyncTask {
	sourceKind := input.SourceKind
	if sourceKind == "" {
		sourceKind = "directory"
	}
	return store.SyncTask{UserID: userID, Name: input.Name, Direction: input.Direction, RemoteSystemID: input.RemoteSystemID, SourceType: input.SourceType, SourcePath: input.SourcePath, SourceKind: sourceKind, TargetType: input.TargetType, TargetPath: input.TargetPath, ConflictPolicy: input.ConflictPolicy, ScheduleType: input.ScheduleType, Cron: input.Cron, Enabled: input.Enabled}
}

func (s *Server) createSyncTask(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	if s.rejectReadOnly(w, r, user, "sync_task_create", "") {
		return
	}
	var input syncTaskRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	var err error
	input, err = validateSyncTaskInput(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "同步任务参数无效")
		return
	}
	item, err := s.store.CreateSyncTask(r.Context(), syncTaskFromRequest(input, user.ID), user.Role == "admin")
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "目标系统不存在")
		return
	}
	if err != nil {
		log.Printf("create sync task: %v", err)
		writeError(w, http.StatusInternalServerError, "创建同步任务失败")
		return
	}
	s.serviceEvent(r, "sync_task_create", user.Username, "target=%d result=success", item.ID)
	writeData(w, http.StatusCreated, "同步任务已创建", publicSyncTask(item))
}

func (s *Server) listSyncTasks(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	items, err := s.store.ListSyncTasks(r.Context(), user.ID, user.Role == "admin")
	if err != nil {
		log.Printf("list sync tasks: %v", err)
		writeError(w, http.StatusInternalServerError, "获取同步任务失败")
		return
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, publicSyncTask(item))
	}
	writeData(w, http.StatusOK, "获取成功", map[string]any{"items": result})
}

func (s *Server) getSyncTaskForRequest(r *http.Request) (store.SyncTask, error) {
	user := currentUser(r.Context())
	id, err := parseSyncID(r.PathValue("id"))
	if err != nil {
		return store.SyncTask{}, store.ErrNotFound
	}
	return s.store.GetSyncTask(r.Context(), id, user.ID, user.Role == "admin")
}

func (s *Server) getSyncTask(w http.ResponseWriter, r *http.Request) {
	item, err := s.getSyncTaskForRequest(r)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "同步任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取同步任务失败")
		return
	}
	logs, total, err := s.store.ListSyncLogs(r.Context(), item.ID, 1, 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取同步日志失败")
		return
	}
	logItems := make([]map[string]any, 0, len(logs))
	for _, entry := range logs {
		logItems = append(logItems, publicSyncLog(entry))
	}
	data := publicSyncTask(item)
	data["logs"] = logItems
	data["logTotal"] = total
	writeData(w, http.StatusOK, "获取成功", data)
}

func (s *Server) updateSyncTask(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	item, err := s.getSyncTaskForRequest(r)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "同步任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取同步任务失败")
		return
	}
	if s.rejectReadOnly(w, r, user, "sync_task_update", r.PathValue("id")) {
		return
	}
	var input syncTaskRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	input, err = validateSyncTaskInput(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "同步任务参数无效")
		return
	}
	updated := syncTaskFromRequest(input, item.UserID)
	updated.ID = item.ID
	if err := s.store.UpdateSyncTask(r.Context(), updated, user.ID, user.Role == "admin"); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "同步任务不存在")
		return
	} else if err != nil {
		log.Printf("update sync task: %v", err)
		writeError(w, http.StatusInternalServerError, "保存同步任务失败")
		return
	}
	result, _ := s.store.GetSyncTask(r.Context(), item.ID, user.ID, user.Role == "admin")
	s.serviceEvent(r, "sync_task_update", user.Username, "target=%d result=success", item.ID)
	writeData(w, http.StatusOK, "同步任务已更新", publicSyncTask(result))
}

func (s *Server) deleteSyncTask(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	item, err := s.getSyncTaskForRequest(r)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "同步任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取同步任务失败")
		return
	}
	if s.rejectReadOnly(w, r, user, "sync_task_delete", r.PathValue("id")) {
		return
	}
	if err := s.store.DeleteSyncTask(r.Context(), item.ID, user.ID, user.Role == "admin"); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "同步任务不存在")
		return
	} else if err != nil {
		log.Printf("delete sync task: %v", err)
		writeError(w, http.StatusInternalServerError, "删除同步任务失败")
		return
	}
	s.releaseSyncLock(item.ID)
	s.serviceEvent(r, "sync_task_delete", user.Username, "target=%d result=success", item.ID)
	writeData(w, http.StatusOK, "同步任务已删除", nil)
}

func (s *Server) listSyncTaskLogs(w http.ResponseWriter, r *http.Request) {
	item, err := s.getSyncTaskForRequest(r)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "同步任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取同步任务失败")
		return
	}
	page, pageSize := pagination(r)
	logs, total, err := s.store.ListSyncLogs(r.Context(), item.ID, page, pageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取同步日志失败")
		return
	}
	items := make([]map[string]any, 0, len(logs))
	for _, entry := range logs {
		items = append(items, publicSyncLog(entry))
	}
	writeData(w, http.StatusOK, "获取成功", map[string]any{"items": items, "page": page, "pageSize": pageSize, "total": total})
}

func (s *Server) syncLock(id int64) *sync.Mutex {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if s.syncLocks == nil {
		s.syncLocks = make(map[int64]*sync.Mutex)
	}
	lock := s.syncLocks[id]
	if lock == nil {
		// 容量保护：锁表超过上限时清扫未被持有的锁，避免已删除任务的 Mutex 永久滞留。
		// Capacity guard: when the lock table grows past its cap, sweep locks nobody holds
		// so mutexes of deleted tasks do not accumulate forever.
		if len(s.syncLocks) >= 4096 {
			for key, candidate := range s.syncLocks {
				if candidate.TryLock() {
					candidate.Unlock()
					delete(s.syncLocks, key)
				}
			}
		}
		lock = &sync.Mutex{}
		s.syncLocks[id] = lock
	}
	return lock
}

// releaseSyncLock 在任务删除后移除对应的锁条目，防止 map 无限增长。
// releaseSyncLock drops a task's lock entry after deletion so the map cannot grow unbounded.
func (s *Server) releaseSyncLock(id int64) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	delete(s.syncLocks, id)
}

func (s *Server) runSyncTaskNow(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	item, err := s.getSyncTaskForRequest(r)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "同步任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取同步任务失败")
		return
	}
	owner, ownerErr := s.store.GetUser(item.UserID)
	if ownerErr != nil {
		writeError(w, http.StatusInternalServerError, "读取同步任务失败")
		return
	}
	if s.userReadOnly(owner) {
		target := strconv.FormatInt(item.ID, 10)
		s.recordAudit(r, &owner.ID, owner.Username, "sync_run", target, "failure", "read_only")
		s.serviceEvent(r, "sync_run", owner.Username, "target=%d result=failure reason=read_only", item.ID)
		writeErrorData(w, http.StatusForbidden, "当前账号处于只读时段，无法执行同步", map[string]string{"code": "READ_ONLY"})
		return
	}
	lock := s.syncLock(item.ID)
	if !lock.TryLock() {
		writeErrorData(w, http.StatusConflict, "同步任务正在执行", map[string]string{"code": "SYNC_TASK_RUNNING"})
		return
	}
	defer lock.Unlock()
	entry := s.executeSyncTask(r.Context(), item)
	writeData(w, http.StatusOK, "同步执行完成", publicSyncLog(entry))
	s.serviceEvent(r, "sync_run", user.Username, "target=%d result=success", item.ID)
}

// StartSyncScheduler 每分钟扫描周期任务，服务重启后按当前 cron 重新恢复调度。
// StartSyncScheduler scans periodic tasks every minute and resumes them after restart.
func (s *Server) StartSyncScheduler(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.scheduleSyncTasks(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (s *Server) scheduleSyncTasks(ctx context.Context) {
	items, err := s.store.ListScheduledSyncTasks(ctx)
	if err != nil {
		if !errors.Is(ctx.Err(), context.Canceled) {
			log.Printf("list scheduled sync tasks: %v", err)
		}
		return
	}
	now := time.Now()
	for _, item := range items {
		schedule, err := cron.ParseStandard(item.Cron)
		if err != nil {
			continue
		}
		next := schedule.Next(now.Add(-time.Minute))
		if next.After(now) || next.Before(now.Add(-time.Minute)) {
			continue
		}
		owner, err := s.store.GetUser(item.UserID)
		if err != nil {
			log.Printf("load scheduled sync task owner %d: %v", item.ID, err)
			continue
		}
		if owner.Disabled {
			log.Printf("skip scheduled sync task %d: owner disabled", item.ID)
			continue
		}
		if s.userReadOnly(owner) {
			log.Printf("skip scheduled sync task %d: owner in read-only window", item.ID)
			continue
		}
		lock := s.syncLock(item.ID)
		if !lock.TryLock() {
			continue
		}
		go func(task store.SyncTask, taskLock *sync.Mutex) {
			defer taskLock.Unlock()
			s.executeSyncTask(ctx, task)
		}(item, lock)
	}
}

func (s *Server) encryptSyncSecret(secret string) (string, error) {
	key := sha256.Sum256(s.config.JWTSecret)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(secret), nil)), nil
}

func (s *Server) decryptSyncSecret(value string) (string, error) {
	sealed, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	key := sha256.Sum256(s.config.JWTSecret)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(sealed) < gcm.NonceSize() {
		return "", errors.New("invalid encrypted secret")
	}
	plain, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], nil)
	return string(plain), err
}

// hostKeyCallbackFor 返回按配置指纹校验的 HostKeyCallback；未配置指纹时回退 InsecureIgnoreHostKey 并打警告日志（兼容既有数据）。
// hostKeyCallbackFor returns a HostKeyCallback that verifies the configured fingerprint, falling back to InsecureIgnoreHostKey with a warning when none is set.
func hostKeyCallbackFor(address, fingerprint string) ssh.HostKeyCallback {
	if fingerprint == "" {
		log.Printf("WARNING: connecting to %s without host key verification (no host_key_fingerprint configured)", address)
		return ssh.InsecureIgnoreHostKey()
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		got := ssh.FingerprintSHA256(key) // "SHA256:..."
		if !hostKeyFingerprintMatches(got, fingerprint) {
			return fmt.Errorf("host key fingerprint mismatch: got %s, want %s", got, fingerprint)
		}
		return nil
	}
}

// decodeFingerprintPayload 解码 SHA256 主机密钥指纹：十六进制优先，base64 使用严格解码
// （Strict 要求尾部 padding 位为零，43 字符 raw base64 的末字符低 2 bit 必须是 0）。
// decodeFingerprintPayload decodes a SHA256 host-key fingerprint: hex first, then strict
// base64 whose trailing padding bits must be zero (the last char of a 43-char raw encoding
// may only use 4 of its 6 bits, so its low 2 bits must be 0).
func decodeFingerprintPayload(value string) ([]byte, bool) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "SHA256:")
	isHex := strings.Contains(value, ":") || (len(value) == sha256.Size*2 && strings.Trim(value, "0123456789abcdefABCDEF") == "")
	if isHex {
		value = strings.ReplaceAll(value, ":", "")
		decoded, err := hex.DecodeString(value)
		return decoded, err == nil && len(decoded) == sha256.Size
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil {
		decoded, err = base64.RawStdEncoding.Strict().DecodeString(value)
	}
	return decoded, err == nil && len(decoded) == sha256.Size
}

func hostKeyFingerprintMatches(got, want string) bool {
	gotBytes, gotOK := decodeFingerprintPayload(got)
	wantBytes, wantOK := decodeFingerprintPayload(want)
	return gotOK && wantOK && subtle.ConstantTimeCompare(gotBytes, wantBytes) == 1
}

func (s *Server) openSFTP(ctx context.Context, item store.RemoteSystem) (*sftp.Client, func(), error) {
	secret, err := s.decryptSyncSecret(item.AuthSecret)
	if err != nil {
		return nil, func() {}, errors.New("invalid credentials")
	}
	auth := []ssh.AuthMethod{}
	if item.AuthType == "password" {
		auth = append(auth, ssh.Password(secret))
	} else {
		passphrase := ""
		if item.AuthPassphrase != "" {
			passphrase, err = s.decryptSyncSecret(item.AuthPassphrase)
			if err != nil {
				return nil, func() {}, errors.New("invalid credentials")
			}
		}
		var signer ssh.Signer
		if passphrase == "" {
			signer, err = ssh.ParsePrivateKey([]byte(secret))
		} else {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(secret), []byte(passphrase))
		}
		if err != nil {
			return nil, func() {}, errors.New("invalid key")
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	address := net.JoinHostPort(item.Host, strconv.Itoa(item.Port))
	dialer := net.Dialer{Timeout: 15 * time.Second}
	netConn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, func() {}, err
	}
	sshConfig := &ssh.ClientConfig{User: item.Username, Auth: auth, HostKeyCallback: hostKeyCallbackFor(address, strings.TrimSpace(item.HostKeyFingerprint)), Timeout: 15 * time.Second}
	connection, channels, requests, err := ssh.NewClientConn(netConn, address, sshConfig)
	if err != nil {
		netConn.Close()
		return nil, func() {}, err
	}
	sshClient := ssh.NewClient(connection, channels, requests)
	client, err := sftp.NewClient(sshClient)
	if err != nil {
		sshClient.Close()
		return nil, func() {}, err
	}
	return client, func() { client.Close(); sshClient.Close() }, nil
}

type syncRunResult struct {
	files   int64
	bytes   int64
	message string
	detail  []string
}

func (s *Server) executeSyncTask(ctx context.Context, task store.SyncTask) store.SyncLog {
	// executeSyncTask 在执行前再次校验任务所有者的只读状态。
	// executeSyncTask rechecks the task owner's read-only state before execution.
	owner, ownerErr := s.store.GetUser(task.UserID)
	if ownerErr != nil {
		// 所有者加载失败按失败记录并跳过执行（fail-closed），与调度/手动路径一致。
		// A failed owner lookup records a failure and skips execution (fail-closed), matching the scheduler and manual paths.
		log.Printf("load sync task owner %d: %v", task.ID, ownerErr)
		runAt := time.Now().UTC().Format(time.RFC3339)
		entry := store.SyncLog{TaskID: task.ID, UserID: task.UserID, RunAt: runAt, Direction: task.Direction, Result: "failure", Message: "读取任务所有者失败，任务已跳过", Detail: "同步跳过: 任务所有者不可用"}
		if _, logErr := s.store.CreateSyncLog(context.Background(), entry); logErr != nil {
			log.Printf("create sync log: %v", logErr)
		}
		if err := s.store.UpdateSyncTaskResult(context.Background(), task.ID, runAt, "failure"); err != nil {
			log.Printf("update sync task result: %v", err)
		}
		return entry
	}
	if s.userReadOnly(owner) {
		runAt := time.Now().UTC().Format(time.RFC3339)
		entry := store.SyncLog{TaskID: task.ID, UserID: task.UserID, RunAt: runAt, Direction: task.Direction, Result: "failure", Message: "用户在只读时段，任务已跳过", Detail: "同步跳过: 用户在只读时段"}
		if _, logErr := s.store.CreateSyncLog(context.Background(), entry); logErr != nil {
			log.Printf("create sync log: %v", logErr)
		}
		if err := s.store.UpdateSyncTaskResult(context.Background(), task.ID, runAt, "failure"); err != nil {
			log.Printf("update sync task result: %v", err)
		}
		return entry
	}
	runAt := time.Now().UTC().Format(time.RFC3339)
	result := syncRunResult{}
	item, err := s.store.GetRemoteSystem(context.Background(), task.RemoteSystemID, task.UserID, false)
	if err == nil {
		if task.Direction == "push" {
			result = s.executeSyncPush(ctx, task, item)
		} else {
			result = s.executeSyncPull(ctx, task, item)
		}
	} else {
		result.message = "目标系统不存在或凭据不可用"
		result.detail = []string{"同步连接失败: 目标系统不可用"}
	}
	resultValue := "success"
	if result.message != "" {
		resultValue = "failure"
	}
	if result.message == "" {
		result.message = "同步完成"
	}
	entry := store.SyncLog{TaskID: task.ID, UserID: task.UserID, RunAt: runAt, Direction: task.Direction, Result: resultValue, Files: result.files, Bytes: result.bytes, Message: result.message, Detail: strings.Join(result.detail, "\n")}
	if _, logErr := s.store.CreateSyncLog(context.Background(), entry); logErr != nil {
		log.Printf("create sync log: %v", logErr)
	}
	if err := s.store.UpdateSyncTaskResult(context.Background(), task.ID, runAt, resultValue); err != nil {
		log.Printf("update sync task result: %v", err)
	}
	return entry
}

func (s *Server) syncErrorDetail(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ReplaceAll(strings.ReplaceAll(err.Error(), "\r", " "), "\n", " ")
	if s.config.DataDir != "" {
		message = strings.ReplaceAll(message, s.config.DataDir, "[data]")
		message = strings.ReplaceAll(message, filepath.ToSlash(s.config.DataDir), "[data]")
	}
	return message
}

func remoteJoin(base, relative string) string {
	if base == "." {
		return pathpkg.Clean(relative)
	}
	return pathpkg.Join(base, relative)
}

func remoteRelative(base, target string) string {
	base = pathpkg.Clean(base)
	target = pathpkg.Clean(target)
	if base == "." {
		return strings.TrimPrefix(target, "./")
	}
	return strings.TrimPrefix(target, strings.TrimSuffix(base, "/")+"/")
}

func remoteRename(client *sftp.Client, target string) (string, error) {
	if _, err := client.Stat(target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return target, nil
		}
		return "", err
	}
	ext := pathpkg.Ext(target)
	stem := strings.TrimSuffix(target, ext)
	for index := 1; index <= 10000; index++ {
		candidate := fmt.Sprintf("%s (%d)%s", stem, index, ext)
		if _, err := client.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", errors.New("too many conflicting files")
}

func (s *Server) executeSyncPush(ctx context.Context, task store.SyncTask, system store.RemoteSystem) syncRunResult {
	if system.Kind == "filebox" {
		return s.executeSyncPushFileBox(ctx, task, system)
	}
	result := syncRunResult{}
	client, closeClient, err := s.openSFTP(ctx, system)
	if err != nil {
		result.message = "无法连接目标系统"
		result.detail = []string{"同步连接失败: " + s.syncErrorDetail(err)}
		return result
	}
	defer closeClient()
	files, err := s.store.ListReadyFilesUnder(ctx, task.UserID, task.SourcePath)
	if err != nil {
		result.message = "读取 FileBox 源文件失败"
		result.detail = append(result.detail, "读取源文件失败: "+s.syncErrorDetail(err))
		return result
	}
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
	if err := client.MkdirAll(task.TargetPath); err != nil {
		result.message = "创建远端目录失败"
		result.detail = append(result.detail, "创建目标目录失败: "+s.syncErrorDetail(err))
		return result
	}
	settings, settingsErr := s.store.GetLogSettings(ctx)
	if settingsErr != nil {
		result.message = "读取传输设置失败"
		result.detail = append(result.detail, "读取传输设置失败")
		return result
	}
	limiter := s.rateLimiter.limiterFor(task.UserID, settings.UploadRateLimit)
	root := filepath.ToSlash(filepath.Join("files", strconv.FormatInt(task.UserID, 10))) + "/"
	for _, file := range files {
		relative := filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(file.StoragePath), root))
		if task.SourcePath != "" {
			if relative == task.SourcePath {
				relative = pathpkg.Base(relative)
			} else {
				relative = strings.TrimPrefix(relative, strings.TrimSuffix(task.SourcePath, "/")+"/")
			}
		}
		remotePath := remoteJoin(task.TargetPath, relative)
		if task.ConflictPolicy == "skip" {
			if _, statErr := client.Stat(remotePath); statErr == nil {
				result.detail = append(result.detail, relative+": skipped (exists)")
				continue
			} else if !errors.Is(statErr, os.ErrNotExist) {
				result.message = "检查远端文件失败"
				result.detail = append(result.detail, relative+": "+s.syncErrorDetail(statErr))
				continue
			}
		} else if task.ConflictPolicy == "rename" {
			remotePath, err = remoteRename(client, remotePath)
			if err != nil {
				result.message = "处理远端文件冲突失败"
				result.detail = append(result.detail, relative+": "+s.syncErrorDetail(err))
				continue
			}
		}
		if err := client.MkdirAll(pathpkg.Dir(remotePath)); err != nil {
			result.message = "创建远端目录失败"
			result.detail = append(result.detail, relative+": "+s.syncErrorDetail(err))
			continue
		}
		localPath := filepath.Join(s.config.DataDir, filepath.FromSlash(file.StoragePath))
		handle, openErr := os.Open(localPath)
		if openErr != nil {
			result.message = "读取 FileBox 文件失败"
			result.detail = append(result.detail, relative+": 文件内容不可用")
			continue
		}
		if limiter != nil {
			if rateErr := waitForUploadRate(ctx, limiter, file.Size); rateErr != nil {
				_ = handle.Close()
				result.message = "上传文件失败"
				result.detail = append(result.detail, relative+": 传输被取消或限速等待超时")
				continue
			}
		}
		tempRemote := remotePath + ".filebox-part-" + newSyncTaskID()
		remoteFile, createErr := client.OpenFile(tempRemote, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
		if createErr == nil {
			_, copyErr := io.Copy(remoteFile, handle)
			closeErr := remoteFile.Close()
			if copyErr != nil {
				createErr = copyErr
				_ = client.Remove(tempRemote)
			} else if closeErr != nil {
				createErr = closeErr
				_ = client.Remove(tempRemote)
			} else {
				// 原子替换远端目标，避免传输中断时破坏原文件。
				// Atomically replace the remote target so interrupted transfers cannot corrupt it.
				renameErr := client.PosixRename(tempRemote, remotePath)
				if renameErr != nil {
					// 回退到标准重命名，以兼容不支持 POSIX 扩展的服务器。
					// Fall back to the standard rename for servers without the POSIX extension.
					renameErr = client.Rename(tempRemote, remotePath)
				}
				if renameErr != nil {
					_ = client.Remove(tempRemote)
					createErr = renameErr
				}
			}
		}
		closeErr := handle.Close()
		if createErr == nil {
			createErr = closeErr
		}
		if createErr != nil {
			result.message = "上传文件失败"
			result.detail = append(result.detail, relative+": "+s.syncErrorDetail(createErr))
			continue
		}
		result.files++
		result.bytes += file.Size
		result.detail = append(result.detail, relative+": uploaded ("+strconv.FormatInt(file.Size, 10)+" bytes)")
	}
	if result.message != "" {
		return result
	}
	return result
}

func safeSyncFileName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, "/\\\x00") || len([]byte(value)) > 255 {
		return "", errors.New("invalid file name")
	}
	for _, char := range value {
		if unicode.IsControl(char) || strings.ContainsRune(`<>:"|?*`, char) {
			return "", errors.New("invalid file name")
		}
	}
	return value, nil
}

func newSyncTaskID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("sync-%d", time.Now().UnixNano())
	}
	return "sync-" + hex.EncodeToString(value)
}

func hashSyncFile(path string) (string, string, error) {
	handle, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer handle.Close()
	sha := sha256.New()
	md5Hash := md5.New()
	if _, err := io.Copy(io.MultiWriter(sha, md5Hash), handle); err != nil {
		return "", "", err
	}
	return hex.EncodeToString(sha.Sum(nil)), hex.EncodeToString(md5Hash.Sum(nil)), nil
}

func (s *Server) executeSyncPull(ctx context.Context, task store.SyncTask, system store.RemoteSystem) syncRunResult {
	if system.Kind == "filebox" {
		return s.executeSyncPullFileBox(ctx, task, system)
	}
	result := syncRunResult{}
	client, closeClient, err := s.openSFTP(ctx, system)
	if err != nil {
		result.message = "无法连接目标系统"
		result.detail = []string{"同步连接失败: " + s.syncErrorDetail(err)}
		return result
	}
	defer closeClient()
	info, err := client.Stat(task.SourcePath)
	if err != nil {
		result.message = "读取远端源目录失败"
		result.detail = []string{"读取源目录失败: " + s.syncErrorDetail(err)}
		return result
	}
	process := func(remotePath string, remoteInfo os.FileInfo) error {
		if remoteInfo.IsDir() {
			return nil
		}
		relative := pathpkg.Base(remotePath)
		if info.IsDir() {
			relative = remoteRelative(task.SourcePath, remotePath)
			if relative == "." || relative == remotePath || strings.HasPrefix(relative, "../") {
				return errors.New("invalid remote file path")
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
		mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		if task.ConflictPolicy == "skip" {
			if _, conflictErr := s.store.FindUploadConflict(ctx, task.UserID, storageDir, name); conflictErr == nil {
				result.detail = append(result.detail, relative+": skipped (exists)")
				return nil
			} else if !errors.Is(conflictErr, store.ErrNotFound) {
				return conflictErr
			}
		}
		if remoteInfo.Size() > s.config.MaxFileSize {
			result.detail = append(result.detail, relative+": skipped (exceeds max file size)")
			return nil
		}
		if s.config.MinFreeSpace > 0 {
			_, free, _, diskErr := diskusage.DiskUsage(s.config.DataDir)
			if diskErr != nil {
				result.detail = append(result.detail, relative+": disk check failed")
				return nil
			}
			if free < s.config.MinFreeSpace {
				result.detail = append(result.detail, relative+": skipped (insufficient disk space)")
				return nil
			}
		}
		temp, tempErr := os.CreateTemp(filepath.Join(s.config.DataDir, "tmp"), "sync-download-*")
		if tempErr != nil {
			return tempErr
		}
		tempPath := temp.Name()
		cleanup := true
		defer func() {
			if cleanup {
				_ = os.Remove(tempPath)
			}
		}()
		remoteFile, openErr := client.Open(remotePath)
		if openErr != nil {
			return openErr
		}
		_, copyErr := io.Copy(temp, remoteFile)
		remoteCloseErr := remoteFile.Close()
		localCloseErr := temp.Close()
		if copyErr != nil {
			return copyErr
		}
		if remoteCloseErr != nil {
			return remoteCloseErr
		}
		if localCloseErr != nil {
			return localCloseErr
		}
		tempInfo, statErr := os.Stat(tempPath)
		if statErr != nil {
			return statErr
		}
		if tempInfo.Size() != remoteInfo.Size() {
			return fmt.Errorf("size mismatch: got %d want %d", tempInfo.Size(), remoteInfo.Size())
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
		uploadTask := store.UploadTask{ID: newSyncTaskID(), UserID: task.UserID, Name: name, Size: remoteInfo.Size(), ChunkSize: remoteInfo.Size(), TotalChunks: 1, Status: "pending", Mime: mimeType, StorageDir: storageDir, Resolve: resolve}
		if createErr := s.store.CreateUploadTask(ctx, uploadTask); createErr != nil {
			return createErr
		}
		file := store.File{UserID: task.UserID, Name: name, StoredName: name, Size: remoteInfo.Size(), Mime: mimeType, SHA256: shaHex, MD5: md5Hex, StoragePath: storageDir}
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
			// 覆盖同步原子替换目标，避免传输中断破坏旧文件（与远端 push 的临时文件方案同源）。
			// Overwrite sync atomically replaces the target so interrupted transfers cannot corrupt it.
			return os.Rename(tempPath, finalPath)
		}); completeErr != nil {
			return completeErr
		}
		cleanup = false
		result.files++
		result.bytes += remoteInfo.Size()
		result.detail = append(result.detail, relative+": downloaded ("+strconv.FormatInt(remoteInfo.Size(), 10)+" bytes)")
		return nil
	}
	if info.IsDir() {
		walker := client.Walk(task.SourcePath)
		for walker.Step() {
			if walker.Err() != nil {
				result.message = "遍历远端目录失败"
				result.detail = append(result.detail, pathpkg.Base(walker.Path())+": "+s.syncErrorDetail(walker.Err()))
				continue
			}
			if err := process(walker.Path(), walker.Stat()); err != nil {
				result.message = "下载文件失败"
				result.detail = append(result.detail, pathpkg.Base(walker.Path())+": "+s.syncErrorDetail(err))
			}
		}
	} else if err := process(task.SourcePath, info); err != nil {
		result.message = "下载文件失败"
		result.detail = append(result.detail, pathpkg.Base(task.SourcePath)+": "+s.syncErrorDetail(err))
	}
	return result
}
