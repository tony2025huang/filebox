package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math/big"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"github.com/skip2/go-qrcode"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/time/rate"

	"filebox/internal/diskusage"
	"filebox/internal/srvlog"
	"filebox/internal/store"
	"filebox/internal/webassets"
)

// Config 定义 HTTP 服务、上传保护、认证和静态资源的运行配置。
// Config defines the HTTP service, upload protection, authentication, and static-resource settings.
type Config struct {
	DataDir         string
	MaxFileSize     int64
	MinFreeSpace    int64
	RegisterEnabled bool
	JWTSecret       []byte
	EncryptionKey   []byte
	JWTExpiry       time.Duration
	TrustedProxies  []*net.IPNet
	Logger          *srvlog.Logger
	Static          fs.FS
}

// Server 组合持久化存储与 HTTP API 配置。
// Server combines the persistent store with the HTTP API configuration.
type Server struct {
	store              *store.Store
	config             Config
	rateLimiter        rateLimiter
	findUploadConflict func(context.Context, int64, string, string) (store.File, error)
	syncMu             sync.Mutex
	syncLocks          map[int64]*sync.Mutex
	syncProgressMu     sync.Mutex
	syncProgress       map[int64]*syncRunProgress
}

const uploadChunkIdleTimeout = 30 * time.Second
const batchDownloadTempMaxAge = 24 * time.Hour
const batchDownloadTempCleanupInterval = time.Hour
const maxPage = 1_000_000

var loginDummyPasswordHash = []byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")

// requestBodyWithIdleTimeout closes a stalled upload body and resets the timer after each read.
// requestBodyWithIdleTimeout 在每次读到数据后重置计时，长时间无数据时关闭上传请求体。
type requestBodyWithIdleTimeout struct {
	io.ReadCloser
	reset chan<- struct{}
}

func (r *requestBodyWithIdleTimeout) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		select {
		case r.reset <- struct{}{}:
		default:
		}
	}
	return n, err
}

// copyRequestBodyWithIdleTimeout streams a bounded request body while aborting an idle client.
// copyRequestBodyWithIdleTimeout 流式读取有大小上限的请求体，并中止空闲客户端。
func copyRequestBodyWithIdleTimeout(ctx context.Context, dst io.Writer, src io.ReadCloser, maxBytes int64) (int64, error) {
	reset := make(chan struct{}, 1)
	done := make(chan struct{})
	var closeOnce sync.Once
	closeBody := func() { closeOnce.Do(func() { _ = src.Close() }) }
	go func() {
		timer := time.NewTimer(uploadChunkIdleTimeout)
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				closeBody()
				return
			case <-reset:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(uploadChunkIdleTimeout)
			case <-ctx.Done():
				closeBody()
				return
			case <-done:
				return
			}
		}
	}()
	defer close(done)
	reader := &requestBodyWithIdleTimeout{ReadCloser: src, reset: reset}
	return io.Copy(dst, io.LimitReader(reader, maxBytes))
}

// diskUsageFunc is replaceable in package tests so disk-full behavior can be
// exercised without depending on the host filesystem's actual free space.
// diskUsageFunc 可在包内测试中替换，以便不依赖测试机真实磁盘空间验证磁盘保护。
var diskUsageFunc = diskusage.DiskUsage

// createBatchTempFile is replaceable in package tests so archive creation
// failures can be exercised without relying on host filesystem permissions.
// createBatchTempFile 可在包内测试中替换，以便不依赖测试机权限验证归档创建失败。
var createBatchTempFile = os.CreateTemp

// batchDownloadMaxBytes 限制单次批量 ZIP 下载的原始文件总大小，避免临时归档耗尽磁盘。
// batchDownloadMaxBytes caps the raw total size of one batch ZIP download so its temporary archive cannot exhaust disk space.
const batchDownloadMaxBytes int64 = 2 << 30

// batchDownloadMaxFiles 限制单次批量 ZIP 下载的文件数量。
// batchDownloadMaxFiles caps the number of files in one batch ZIP download.
const batchDownloadMaxFiles = 500

const maxLogRetentionDays = 3650

// rateLimiter keeps one token bucket per authenticated user and evicts idle buckets.
// rateLimiter 按用户维护令牌桶，并清理长期未访问的桶。
type rateLimiter struct {
	mu              sync.Mutex
	buckets         map[int64]*rate.Limiter
	lastSeen        map[int64]time.Time
	publicBuckets   map[string]*rate.Limiter
	publicLastSeen  map[string]time.Time
	requestBuckets  map[string]*rate.Limiter
	requestLastSeen map[string]time.Time
}

type contextKey string

const userContextKey contextKey = "filebox-user"

type response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type changePasswordRequest struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

type totpRequest struct {
	TOTPChallenge string `json:"totpChallenge"`
	Code          string `json:"code"`
}

type totpToggleRequest struct {
	Enabled bool `json:"enabled"`
	// Reenroll 为 true 时生成新随机 secret 且 enabled=false，要求用户下次登录重新扫码绑定（问题 12）。
	// Reenroll generates a fresh random secret with enabled=false so the user re-binds on their next login.
	Reenroll bool `json:"reenroll"`
}

type ipACLRequest struct {
	Enabled   bool   `json:"enabled"`
	Whitelist string `json:"whitelist"`
}

type uploadInitRequest struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	ChunkSize int64  `json:"chunkSize"`
	SHA256    string `json:"sha256"`
	MD5       string `json:"md5"`
	Mime      string `json:"mime"`
	Resolve   string `json:"resolve"`
	Dir       string `json:"dir"`
}

type uploadCompleteRequest struct {
	Action string `json:"action"`
	SHA256 string `json:"sha256"`
	MD5    string `json:"md5"`
}

type instantCheckRequest struct {
	SHA256 string `json:"sha256"`
	MD5    string `json:"md5"`
	Size   int64  `json:"size"`
	Name   string `json:"name"`
	Dir    string `json:"dir"`
}

type shareRequest struct {
	ExpiresInHours int `json:"expiresInHours"`
	MaxDownloads   int `json:"maxDownloads"`
}

type shareExtendRequest struct {
	ExpiresInHours int `json:"expiresInHours"`
}

type shareIncreaseRequest struct {
	MaxDownloads int `json:"maxDownloads"`
}

type registerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type createUserRequest struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	Role         string `json:"role"`
	QuotaBytes   int64  `json:"quotaBytes"`
	TOTPEnabled  bool   `json:"totpEnabled"`
	Reenroll     bool   `json:"reenroll"`
	IPACLEnabled bool   `json:"ipAclEnabled"`
	IPWhitelist  string `json:"ipWhitelist"`
}

type updateUserRequest struct {
	Role       *string `json:"role"`
	QuotaBytes *int64  `json:"quotaBytes"`
	Disabled   *bool   `json:"disabled"`
	Password   string  `json:"password"`
}

type readOnlyRequest struct {
	From  string `json:"from"`
	Until string `json:"until"`
}

type logSettingsRequest struct {
	LogRetentionDays    *int    `json:"logRetentionDays"`
	LockThreshold       *int    `json:"lockThreshold"`
	AutoUnlockEnabled   *bool   `json:"autoUnlockEnabled"`
	AutoUnlockMinutes   *int    `json:"autoUnlockMinutes"`
	DefaultLang         *string `json:"defaultLang"`
	ThemeColor          *string `json:"themeColor"`
	PasswordMinLength   *int    `json:"passwordMinLength"`
	PasswordComplexity  *int    `json:"passwordComplexity"`
	IPLockWindowMinutes *int    `json:"ipLockWindowMinutes"`
	IPLockThreshold     *int    `json:"ipLockThreshold"`
	IPAutoUnlockEnabled *bool   `json:"ipAutoUnlockEnabled"`
	IPUnlockMinutes     *int    `json:"ipUnlockMinutes"`
	RegisterEnabled     *bool   `json:"registerEnabled"`
	UploadRateLimit     *int64  `json:"uploadRateLimit"`
	TrustProxy          *bool   `json:"trustProxy"`
}

type languageRequest struct {
	Language string `json:"language"`
}

const (
	defaultSiteTitle = "FileBox 文件管理"
	brandMaxSize     = 512 * 1024
)

type brandResponse struct {
	SiteTitle       string `json:"siteTitle"`
	SiteDescription string `json:"siteDescription"`
	ICPText         string `json:"icpText"`
	PoliceText      string `json:"policeText"`
	CopyrightText   string `json:"copyrightText"`
	HasFavicon      bool   `json:"hasFavicon"`
	HasLoginLogo    bool   `json:"hasLoginLogo"`
	HasMainLogo     bool   `json:"hasMainLogo"`
	DefaultLang     string `json:"defaultLang"`
	ThemeColor      string `json:"themeColor"`
	RegisterEnabled bool   `json:"registerEnabled"`
	MaxFileSize     int64  `json:"maxFileSize"`
}

type brandAsset struct {
	field       string
	settingKey  string
	prefix      string
	allowedExts map[string]string
	defaultPath string
}

var brandAssets = map[string]brandAsset{
	"favicon":    {field: "favicon", settingKey: store.BrandFaviconKey, prefix: "favicon", allowedExts: map[string]string{".ico": "image/x-icon", ".png": "image/png", ".svg": "image/svg+xml"}, defaultPath: "brand/favicon.svg"},
	"login-logo": {field: "loginLogo", settingKey: store.BrandLoginLogoKey, prefix: "login-logo", allowedExts: map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".svg": "image/svg+xml"}, defaultPath: "brand/logo.svg"},
	"main-logo":  {field: "mainLogo", settingKey: store.BrandMainLogoKey, prefix: "main-logo", allowedExts: map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".svg": "image/svg+xml"}, defaultPath: "brand/logo.svg"},
}

func NewServer(db *store.Store, config Config) *Server {
	// JWT timestamps preserve sub-second ordering for password-change revocation.
	// JWT 时间戳保留亚秒精度，避免密码变更撤销检查在同一秒内失去顺序信息。
	jwt.TimePrecision = time.Nanosecond
	// NewServer 创建 API 服务，并为未指定的 JWT 有效期设置七天默认值。
	// NewServer creates the API server and defaults an unspecified JWT lifetime to seven days.
	if config.JWTExpiry <= 0 {
		config.JWTExpiry = 7 * 24 * time.Hour
	}
	server := &Server{store: db, config: config, rateLimiter: rateLimiter{buckets: make(map[int64]*rate.Limiter), lastSeen: make(map[int64]time.Time), publicBuckets: make(map[string]*rate.Limiter), publicLastSeen: make(map[string]time.Time)}, findUploadConflict: db.FindUploadConflict, syncLocks: make(map[int64]*sync.Mutex), syncProgress: make(map[int64]*syncRunProgress)}
	server.startBatchDownloadTempCleanup()
	return server
}

func (s *Server) startBatchDownloadTempCleanup() {
	if err := cleanupBatchDownloadTempFiles(filepath.Join(s.config.DataDir, "tmp"), time.Now()); err != nil {
		log.Printf("cleanup batch download temp files: %v", err)
	}
	go func() {
		ticker := time.NewTicker(batchDownloadTempCleanupInterval)
		defer ticker.Stop()
		for now := range ticker.C {
			if err := cleanupBatchDownloadTempFiles(filepath.Join(s.config.DataDir, "tmp"), now); err != nil {
				log.Printf("cleanup batch download temp files: %v", err)
			}
		}
	}()
}

func cleanupBatchDownloadTempFiles(tmpDir string, now time.Time) error {
	entries, err := os.ReadDir(tmpDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	cutoff := now.Add(-batchDownloadTempMaxAge)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || (!strings.HasPrefix(name, "batch-download-") && !strings.HasPrefix(name, "batch-share-group-")) || !strings.HasSuffix(name, ".zip") {
			continue
		}
		info, infoErr := entry.Info()
		if errors.Is(infoErr, os.ErrNotExist) {
			continue
		}
		if infoErr != nil {
			return infoErr
		}
		if !info.ModTime().Before(cutoff) {
			continue
		}
		if removeErr := os.Remove(filepath.Join(tmpDir, name)); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return removeErr
		}
	}
	return nil
}

func (s *Server) Handler() http.Handler {
	// Handler 注册公开、登录保护和管理员保护的路由，并包装 SPA 与安全响应头。
	// Handler registers public, authenticated, and admin-only routes, then wraps them with SPA and security handling.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/register", s.register)
	mux.HandleFunc("POST /api/auth/logout", s.requireAuth(s.logout))
	mux.HandleFunc("POST /api/auth/change-password", s.requireAuth(s.changePassword))
	mux.HandleFunc("GET /api/auth/password-policy", s.requireAuth(s.passwordPolicy))
	mux.HandleFunc("POST /api/auth/totp", s.totp)
	mux.HandleFunc("GET /api/auth/totp-qrcode", s.totpQRCode)
	mux.HandleFunc("GET /api/auth/me", s.requireAuth(s.me))
	mux.HandleFunc("PUT /api/auth/language", s.requireAuth(s.updateLanguage))
	mux.HandleFunc("GET /api/brand", s.brand)
	mux.HandleFunc("GET /brand/{asset}", s.brandAsset)

	mux.HandleFunc("GET /api/files", s.requireAuth(s.listFiles))
	mux.HandleFunc("POST /api/files/upload-init", s.requireAuth(s.uploadInit))
	mux.HandleFunc("DELETE /api/upload-tasks/{taskID}", s.requireAuth(s.deleteUploadTask))
	mux.HandleFunc("POST /api/files/check", s.requireAuth(s.checkInstantUpload))
	mux.HandleFunc("PUT /api/files/{taskID}/chunks/{index}", s.requireAuth(s.uploadChunk))
	mux.HandleFunc("GET /api/files/{taskID}/status", s.requireAuth(s.uploadStatus))
	mux.HandleFunc("POST /api/files/{taskID}/complete", s.requireAuth(s.completeUpload))
	mux.HandleFunc("GET /api/files/{id}/download", s.requireAuth(s.download))
	mux.HandleFunc("POST /api/files/batch-download", s.requireAuth(s.batchDownload))
	mux.HandleFunc("POST /api/files/batch-share", s.requireAuth(s.batchShare))
	mux.HandleFunc("POST /api/files/batch-share-group", s.requireAuth(s.createBatchShareGroup))
	mux.HandleFunc("GET /api/shares/groups", s.requireAuth(s.listShareGroups))
	mux.HandleFunc("GET /api/shared-groups/{token}/meta", s.shareGroupMeta)
	mux.HandleFunc("GET /api/shared-groups/{token}/download/{fileID}", s.shareGroupDownload)
	mux.HandleFunc("POST /api/shared-groups/{token}/batch-download", s.shareGroupBatchDownload)
	mux.HandleFunc("DELETE /api/shared-groups/{token}", s.requireAuth(s.revokeShareGroup))
	mux.HandleFunc("PUT /api/shared-groups/{token}/extend", s.requireAuth(s.extendShareGroup))
	mux.HandleFunc("PUT /api/shared-groups/{token}/increase", s.requireAuth(s.increaseShareGroup))
	mux.HandleFunc("GET /api/shared-groups/{token}/files", s.requireAuth(s.listShareGroupFiles))
	mux.HandleFunc("POST /api/shared-groups/{token}/files", s.requireAuth(s.addShareGroupFiles))
	mux.HandleFunc("DELETE /api/shared-groups/{token}/files/{fileID}", s.requireAuth(s.removeShareGroupFile))
	mux.HandleFunc("PUT /api/shared-groups/{token}", s.requireAuth(s.updateShareGroup))
	mux.HandleFunc("POST /api/files/batch-delete", s.requireAuth(s.batchDelete))
	mux.HandleFunc("GET /api/files/progress/stream", s.requireAuth(s.uploadProgressStream))
	mux.HandleFunc("GET /api/files/{id}/preview", s.requireAuth(s.preview))
	mux.HandleFunc("POST /api/files/{id}/share", s.requireAuth(s.createShare))
	mux.HandleFunc("GET /api/files/{id}/shares", s.requireAuth(s.listFileShares))
	mux.HandleFunc("DELETE /api/files/{id}/shares", s.requireAuth(s.deleteShares))
	mux.HandleFunc("GET /api/shares", s.requireAuth(s.listShares))
	mux.HandleFunc("GET /api/shares/{token}", s.requireAuth(s.getManagedShare))
	mux.HandleFunc("GET /api/files/shared/{token}/meta", s.shareMeta)
	mux.HandleFunc("GET /api/files/shared/{token}/download", s.shareDownload)
	mux.HandleFunc("GET /api/files/shared/{token}/preview", s.sharePreview)
	mux.HandleFunc("GET /api/shares/{token}/logs", s.requireAuth(s.shareLogs))
	mux.HandleFunc("PUT /api/shares/{token}/extend", s.requireAuth(s.extendShare))
	mux.HandleFunc("PUT /api/shares/{token}/increase", s.requireAuth(s.increaseShare))
	mux.HandleFunc("DELETE /api/shares/{token}", s.requireAuth(s.deleteShare))
	mux.HandleFunc("POST /api/collections", s.requireAuth(s.createCollection))
	mux.HandleFunc("GET /api/collections", s.requireAuth(s.listCollections))
	mux.HandleFunc("GET /api/collections/{id}", s.requireAuth(s.getCollection))
	mux.HandleFunc("GET /api/collections/{id}/files", s.requireAuth(s.getCollectionFiles))
	mux.HandleFunc("PUT /api/collections/{id}", s.requireAuth(s.updateCollection))
	mux.HandleFunc("DELETE /api/collections/{id}", s.requireAuth(s.deleteCollection))
	mux.HandleFunc("GET /api/collections/{token}/meta", s.collectionMeta)
	mux.HandleFunc("POST /api/collections/{token}/upload-init", s.collectionUploadInit)
	mux.HandleFunc("PUT /api/collections/{token}/upload-chunk/{taskID}/{index}", s.collectionUploadChunk)
	mux.HandleFunc("POST /api/collections/{token}/upload-chunk/{taskID}/{index}", s.collectionUploadChunk)
	mux.HandleFunc("PUT /api/collections/{token}/upload-chunk", s.collectionUploadChunk)
	mux.HandleFunc("POST /api/collections/{token}/upload-chunk", s.collectionUploadChunk)
	mux.HandleFunc("GET /api/collections/{token}/upload-status/{taskID}", s.collectionUploadStatus)
	mux.HandleFunc("GET /api/collections/{token}/upload-queue/{taskID}", s.collectionUploadTaskState)
	mux.HandleFunc("POST /api/collections/{token}/upload-complete/{taskID}", s.collectionUploadComplete)
	mux.HandleFunc("POST /api/collections/{token}/upload-complete", s.collectionUploadComplete)
	mux.HandleFunc("POST /api/folders", s.requireAuth(s.createFolder))
	mux.HandleFunc("GET /api/folders", s.requireAuth(s.listFolders))
	mux.HandleFunc("PATCH /api/folders/{id}", s.requireAuth(s.renameFolder))
	mux.HandleFunc("DELETE /api/folders/{id}", s.requireAuth(s.deleteFolder))
	mux.HandleFunc("DELETE /api/files/{id}", s.requireAuth(s.deleteFile))
	mux.HandleFunc("GET /api/logs", s.requireAuth(s.listLogs))
	mux.HandleFunc("GET /api/logs/actions", s.requireAuth(s.logActions))
	mux.HandleFunc("GET /api/sync/systems", s.requireAuth(s.listSyncSystems))
	mux.HandleFunc("POST /api/sync/systems", s.requireAuth(s.createSyncSystem))
	mux.HandleFunc("PUT /api/sync/systems/{id}", s.requireAuth(s.updateSyncSystem))
	mux.HandleFunc("POST /api/sync/systems/{id}/host-key", s.requireAuth(s.updateSyncSystemHostKey))
	mux.HandleFunc("DELETE /api/sync/systems/{id}", s.requireAuth(s.deleteSyncSystem))
	mux.HandleFunc("GET /api/sync/systems/{id}/secret", s.requireAuth(s.getSyncSystemSecret))
	mux.HandleFunc("GET /api/sync/systems/{id}/browse", s.requireAuth(s.browseSyncSystem))
	mux.HandleFunc("POST /api/sync/systems/{id}/test", s.requireAuth(s.testSyncSystem))
	mux.HandleFunc("POST /api/sync/systems/{id}/mkdir", s.requireAuth(s.mkdirSyncSystem))
	mux.HandleFunc("GET /api/sync/browse-filebox", s.requireAuth(s.browseLocalFileBox))
	mux.HandleFunc("GET /api/sync/tasks", s.requireAuth(s.listSyncTasks))
	mux.HandleFunc("POST /api/sync/tasks", s.requireAuth(s.createSyncTask))
	mux.HandleFunc("GET /api/sync/tasks/{id}", s.requireAuth(s.getSyncTask))
	mux.HandleFunc("PUT /api/sync/tasks/{id}", s.requireAuth(s.updateSyncTask))
	mux.HandleFunc("DELETE /api/sync/tasks/{id}", s.requireAuth(s.deleteSyncTask))
	mux.HandleFunc("POST /api/sync/tasks/{id}/run", s.requireAuth(s.runSyncTaskNow))
	mux.HandleFunc("GET /api/sync/tasks/{id}/logs", s.requireAuth(s.listSyncTaskLogs))
	mux.HandleFunc("GET /api/sync/tasks/{id}/progress", s.requireAuth(s.getSyncTaskProgress))

	mux.HandleFunc("GET /api/admin/users", s.requireAdmin(s.listUsers))
	mux.HandleFunc("POST /api/admin/users", s.requireAdmin(s.createUser))
	mux.HandleFunc("PUT /api/admin/users/{id}", s.requireAdmin(s.updateUser))
	mux.HandleFunc("PUT /api/admin/users/{id}/read-only", s.requireAdmin(s.updateUserReadOnly))
	mux.HandleFunc("DELETE /api/admin/users/{id}", s.requireAdmin(s.deleteUser))
	mux.HandleFunc("GET /api/admin/stats", s.requireAdmin(s.stats))
	mux.HandleFunc("GET /api/admin/settings", s.requireAdmin(s.getSettings))
	mux.HandleFunc("PUT /api/admin/settings", s.requireAdmin(s.updateSettings))
	mux.HandleFunc("PUT /api/admin/brand", s.requireAdmin(s.updateBrand))
	mux.HandleFunc("PUT /api/admin/users/{id}/totp", s.requireAdmin(s.updateUserTOTP))
	mux.HandleFunc("PUT /api/admin/users/{id}/ip-acl", s.requireAdmin(s.updateUserIPACL))
	mux.HandleFunc("GET /api/admin/locks", s.requireAdmin(s.listLocks))
	mux.HandleFunc("DELETE /api/admin/locks/ip/{ip}", s.requireAdmin(s.deleteIPLock))
	mux.HandleFunc("DELETE /api/admin/locks/user/{id}", s.requireAdmin(s.deleteUserLock))

	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The compatibility path cannot be registered directly because it overlaps
		// DELETE /api/files/{id}/shares in net/http's ServeMux pattern set.
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/files/tasks/") {
			taskID := strings.TrimPrefix(r.URL.Path, "/api/files/tasks/")
			if taskID != "" && !strings.Contains(taskID, "/") {
				r.URL.Path = "/api/upload-tasks/" + taskID
			}
		}
		mux.ServeHTTP(w, r)
	})
	return s.securityHeaders(s.spa(api))
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; img-src 'self' data:; connect-src 'self'; frame-src 'self'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) spa(api http.Handler) http.Handler {
	if s.config.Static == nil {
		return api
	}
	static := http.FileServer(http.FS(s.config.Static))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/brand/") {
			api.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			api.ServeHTTP(w, r)
			return
		}
		name := strings.TrimPrefix(pathForFS(r.URL.Path), "/")
		if name != "" {
			if file, err := s.config.Static.Open(name); err == nil {
				file.Close()
				static.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(s.config.Static, "index.html")
		if err != nil {
			http.Error(w, "frontend unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(index)
	})
}

func pathForFS(path string) string {
	clean := filepath.ToSlash(filepath.Clean("/" + path))
	return strings.TrimPrefix(clean, "/")
}

func (s *Server) brand(w http.ResponseWriter, r *http.Request) {
	// brand 返回公开品牌配置；空标题由内置标题兜底，空备案文本不会渲染。
	// brand returns public branding; the built-in title is used when empty, and empty filing text is omitted.
	settings, err := s.store.GetBrandSettings(r.Context())
	if err != nil {
		log.Printf("get brand settings: %v", err)
		writeError(w, http.StatusInternalServerError, "获取品牌设置失败")
		return
	}
	writeData(w, http.StatusOK, "获取成功", s.publicBrand(settings))
}

func (s *Server) publicBrand(settings store.BrandSettings) brandResponse {
	title := strings.TrimSpace(settings.Title)
	if title == "" {
		title = defaultSiteTitle
	}
	defaultLang := "zh-CN"
	themeColor := store.DefaultThemeColor
	registerEnabled := false
	if languageSettings, err := s.store.GetLogSettings(context.Background()); err == nil {
		if isValidLang(languageSettings.DefaultLang) {
			defaultLang = languageSettings.DefaultLang
			themeColor = languageSettings.ThemeColor
		}
		registerEnabled = languageSettings.RegisterEnabled
	}
	return brandResponse{
		SiteTitle:       title,
		SiteDescription: settings.Description,
		ICPText:         settings.ICP,
		PoliceText:      settings.Police,
		CopyrightText:   settings.Copyright,
		HasFavicon:      s.brandAssetExists(brandAssets["favicon"], settings.Favicon),
		HasLoginLogo:    s.brandAssetExists(brandAssets["login-logo"], settings.LoginLogo),
		HasMainLogo:     s.brandAssetExists(brandAssets["main-logo"], settings.MainLogo),
		DefaultLang:     defaultLang,
		MaxFileSize:     s.config.MaxFileSize,
		ThemeColor:      themeColor,
		RegisterEnabled: registerEnabled,
	}
}

func (s *Server) brandAsset(w http.ResponseWriter, r *http.Request) {
	// brandAsset 只提供经过文件名和扩展名校验的自定义资源，否则返回内置资源。
	// brandAsset serves only validated custom assets and falls back to the embedded resource otherwise.
	asset, ok := brandAssets[r.PathValue("asset")]
	if !ok {
		writeError(w, http.StatusNotFound, "品牌资源不存在")
		return
	}
	settings, err := s.store.GetBrandSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "获取品牌资源失败")
		return
	}
	filename := brandSettingFilename(settings, asset.settingKey)
	if path, exists := s.brandAssetPath(asset, filename); exists {
		file, err := os.Open(path)
		if err == nil {
			defer file.Close()
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Content-Type", asset.allowedExts[strings.ToLower(filepath.Ext(filename))])
			http.ServeContent(w, r, filename, parseTime(""), file)
			return
		}
	}
	contents, err := fs.ReadFile(webassets.BrandFS, asset.defaultPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取默认品牌资源失败")
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", asset.allowedExts[filepath.Ext(asset.defaultPath)])
	http.ServeContent(w, r, filepath.Base(asset.defaultPath), time.Time{}, bytes.NewReader(contents))
}

func (s *Server) updateBrand(w http.ResponseWriter, r *http.Request) {
	// updateBrand 校验 multipart 品牌文本和资源，并以固定文件名原子替换自定义资源。
	// updateBrand validates multipart branding fields and atomically replaces custom assets under fixed names.
	const maxRequestSize = 3*brandMaxSize + 256*1024
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	if err := r.ParseMultipartForm(64 * 1024); err != nil {
		writeError(w, http.StatusBadRequest, "品牌设置格式无效")
		return
	}
	if reset, err := parseOptionalBool(r.FormValue("reset")); err != nil {
		writeError(w, http.StatusBadRequest, "恢复默认参数无效")
		return
	} else if reset {
		if err := s.clearBrandFiles(); err != nil {
			log.Printf("clear brand files: %v", err)
			writeError(w, http.StatusInternalServerError, "清理品牌资源失败")
			return
		}
		if err := s.store.UpdateBrandSettings(r.Context(), emptyBrandSettings()); err != nil {
			writeError(w, http.StatusInternalServerError, "恢复默认品牌失败")
			return
		}
		settings, err := s.store.GetBrandSettings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取品牌设置失败")
			return
		}
		admin := currentUser(r.Context())
		s.serviceEvent(r, "brand_update", admin.Username, "target=brand action=reset result=success")
		writeData(w, http.StatusOK, "已恢复默认品牌", s.publicBrand(settings))
		return
	}

	values := map[string]string{}
	textFields := []struct {
		formKey    string
		clearKey   string
		settingKey string
		maxRunes   int
	}{
		{formKey: "siteTitle", clearKey: "clearTitle", settingKey: store.BrandTitleKey, maxRunes: 64},
		{formKey: "siteDescription", clearKey: "clearDescription", settingKey: store.BrandDescriptionKey, maxRunes: 200},
		{formKey: "icpText", clearKey: "clearIcp", settingKey: store.BrandICPKey, maxRunes: 128},
		{formKey: "policeText", clearKey: "clearPolice", settingKey: store.BrandPoliceKey, maxRunes: 128},
		{formKey: "copyrightText", clearKey: "clearCopyright", settingKey: store.BrandCopyrightKey, maxRunes: 128},
	}
	for _, field := range textFields {
		clear, err := parseOptionalBool(r.FormValue(field.clearKey))
		if err != nil {
			writeError(w, http.StatusBadRequest, "品牌清除参数无效")
			return
		}
		value := strings.TrimSpace(r.FormValue(field.formKey))
		if clear {
			values[field.settingKey] = ""
			continue
		}
		if value == "" {
			continue
		}
		if err := validateBrandText(value, field.maxRunes); err != nil {
			writeError(w, http.StatusBadRequest, "品牌文本长度或格式无效")
			return
		}
		values[field.settingKey] = value
	}

	uploads := make([]brandUpload, 0, len(brandAssets))
	for _, key := range []string{"favicon", "login-logo", "main-logo"} {
		asset := brandAssets[key]
		header := firstMultipartFile(r.MultipartForm, asset.field)
		if header == nil {
			continue
		}
		upload, err := readBrandUpload(header, asset)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		uploads = append(uploads, upload)
	}
	for _, key := range []string{"favicon", "login-logo", "main-logo"} {
		asset := brandAssets[key]
		removeKey := "remove" + strings.ToUpper(asset.field[:1]) + asset.field[1:]
		remove, err := parseOptionalBool(r.FormValue(removeKey))
		if err != nil {
			writeError(w, http.StatusBadRequest, "品牌资源移除参数无效")
			return
		}
		if remove && firstMultipartFile(r.MultipartForm, asset.field) == nil {
			if err := s.removeBrandAsset(asset); err != nil {
				writeError(w, http.StatusInternalServerError, "移除品牌资源失败")
				return
			}
			values[asset.settingKey] = ""
		}
	}
	for _, upload := range uploads {
		filename, err := s.saveBrandUpload(upload)
		if err != nil {
			log.Printf("save brand asset: %v", err)
			writeError(w, http.StatusInternalServerError, "保存品牌资源失败")
			return
		}
		values[upload.asset.settingKey] = filename
	}
	if len(values) > 0 {
		if err := s.store.UpdateBrandSettings(r.Context(), values); err != nil {
			writeError(w, http.StatusInternalServerError, "保存品牌设置失败")
			return
		}
	}
	settings, err := s.store.GetBrandSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取品牌设置失败")
		return
	}
	admin := currentUser(r.Context())
	s.serviceEvent(r, "brand_update", admin.Username, "target=brand fields=%d result=success", len(values))
	writeData(w, http.StatusOK, "品牌设置已保存", s.publicBrand(settings))
}

type brandUpload struct {
	asset brandAsset
	data  []byte
	ext   string
}

func emptyBrandSettings() map[string]string {
	return map[string]string{store.BrandTitleKey: "", store.BrandDescriptionKey: "", store.BrandICPKey: "", store.BrandPoliceKey: "", store.BrandCopyrightKey: "", store.BrandFaviconKey: "", store.BrandLoginLogoKey: "", store.BrandMainLogoKey: ""}
}

func parseOptionalBool(value string) (bool, error) {
	if strings.TrimSpace(value) == "" {
		return false, nil
	}
	parsed, err := strconv.ParseBool(value)
	return parsed, err
}

func validateBrandText(value string, maxRunes int) error {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxRunes {
		return errors.New("invalid brand text")
	}
	return nil
}

func firstMultipartFile(form *multipart.Form, field string) *multipart.FileHeader {
	if form == nil || len(form.File[field]) == 0 {
		return nil
	}
	return form.File[field][0]
}

func readBrandUpload(header *multipart.FileHeader, asset brandAsset) (brandUpload, error) {
	// readBrandUpload 同时限制大小、扩展名和内容签名，避免伪造品牌资源类型。
	// readBrandUpload enforces size, extension, and content signatures to reject spoofed asset types.
	ext := strings.ToLower(filepath.Ext(header.Filename))
	contentType, ok := asset.allowedExts[ext]
	if !ok {
		return brandUpload{}, fmt.Errorf("品牌资源类型不支持，仅允许 %s", strings.Join(sortedBrandExtensions(asset.allowedExts), ", "))
	}
	if header.Size > brandMaxSize {
		return brandUpload{}, errors.New("品牌资源不能超过 512KB")
	}
	file, err := header.Open()
	if err != nil {
		return brandUpload{}, errors.New("无法读取品牌资源")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, brandMaxSize+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return brandUpload{}, errors.New("读取品牌资源失败")
	}
	if len(data) == 0 || len(data) > brandMaxSize || !brandContentMatches(ext, contentType, data) {
		return brandUpload{}, errors.New("品牌资源内容与文件类型不匹配")
	}
	return brandUpload{asset: asset, data: data, ext: ext}, nil
}

func brandContentMatches(ext, contentType string, data []byte) bool {
	detected := http.DetectContentType(data)
	switch ext {
	case ".png":
		return bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}) || detected == contentType
	case ".jpg":
		return len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff || detected == contentType
	case ".ico":
		return len(data) >= 4 && data[0] == 0 && data[1] == 0 && (data[2] == 1 || data[2] == 2) && data[3] == 0 || detected == contentType
	case ".svg":
		return bytes.Contains(bytes.ToLower(data), []byte("<svg")) || detected == contentType || detected == "text/xml; charset=utf-8"
	default:
		return false
	}
}

func sortedBrandExtensions(allowed map[string]string) []string {
	result := make([]string, 0, len(allowed))
	for ext := range allowed {
		result = append(result, ext)
	}
	sort.Strings(result)
	return result
}

func brandSettingFilename(settings store.BrandSettings, key string) string {
	switch key {
	case store.BrandFaviconKey:
		return settings.Favicon
	case store.BrandLoginLogoKey:
		return settings.LoginLogo
	case store.BrandMainLogoKey:
		return settings.MainLogo
	default:
		return ""
	}
}

func (s *Server) brandAssetPath(asset brandAsset, filename string) (string, bool) {
	if filename == "" || filepath.Base(filename) != filename || !strings.HasPrefix(filename, asset.prefix+".") {
		return "", false
	}
	if _, ok := asset.allowedExts[strings.ToLower(filepath.Ext(filename))]; !ok {
		return "", false
	}
	brandDir := filepath.Join(s.config.DataDir, "brand")
	path := filepath.Join(brandDir, filename)
	relative, err := filepath.Rel(brandDir, path)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return "", false
	}
	return path, true
}

func (s *Server) brandAssetExists(asset brandAsset, filename string) bool {
	_, exists := s.brandAssetPath(asset, filename)
	return exists
}

func (s *Server) saveBrandUpload(upload brandUpload) (string, error) {
	// saveBrandUpload 先写入权限收紧的临时文件，再通过重命名完成替换。
	// saveBrandUpload writes a restricted temporary file first, then completes replacement with a rename.
	brandDir := filepath.Join(s.config.DataDir, "brand")
	if err := os.MkdirAll(brandDir, 0o755); err != nil {
		return "", err
	}
	temporary, err := os.CreateTemp(brandDir, ".brand-upload-*")
	if err != nil {
		return "", err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return "", err
	}
	if _, err := temporary.Write(upload.data); err != nil {
		temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	for ext := range upload.asset.allowedExts {
		if err := os.Remove(filepath.Join(brandDir, upload.asset.prefix+ext)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	filename := upload.asset.prefix + upload.ext
	if err := os.Rename(temporaryName, filepath.Join(brandDir, filename)); err != nil {
		return "", err
	}
	return filename, nil
}

func (s *Server) removeBrandAsset(asset brandAsset) error {
	brandDir := filepath.Join(s.config.DataDir, "brand")
	for ext := range asset.allowedExts {
		if err := os.Remove(filepath.Join(brandDir, asset.prefix+ext)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (s *Server) clearBrandFiles() error {
	brandDir := filepath.Join(s.config.DataDir, "brand")
	entries, err := os.ReadDir(brandDir)
	if errors.Is(err, os.ErrNotExist) {
		return os.MkdirAll(brandDir, 0o755)
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(brandDir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	// login 使用统一失败响应防止枚举，并在密码失败达到阈值后锁定账号。
	// login uses a uniform failure response to prevent enumeration and locks accounts after the configured threshold.
	var input loginRequest
	operator := "unknown"
	loginResult, loginReason := "failure", "invalid_request"
	defer func() { s.serviceEvent(r, "login", operator, "result=%s reason=%s", loginResult, loginReason) }()
	if !decodeJSON(w, r, &input) {
		return
	}
	username := strings.TrimSpace(input.Username)
	operator = username
	if operator == "" {
		operator = "unknown"
	}
	if strings.TrimSpace(input.Username) == "" || input.Password == "" {
		writeError(w, http.StatusBadRequest, "用户名和密码不能为空")
		return
	}
	settings, settingsErr := s.store.GetLogSettings(r.Context())
	if settingsErr != nil {
		log.Printf("get login settings: %v", settingsErr)
		loginReason = "settings_unavailable"
	}
	ip := s.requestIP(r)
	if locked, err := s.store.IsIPLocked(r.Context(), ip, settings.IPAutoUnlockEnabled); err != nil {
		log.Printf("check ip lock: %v", err)
	} else if locked {
		loginReason = "ip_locked"
		s.recordAudit(r, nil, username, "login", "", "failure", "ip_locked")
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	user, err := s.store.GetUserByUsername(username)
	if errors.Is(err, store.ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(loginDummyPasswordHash, []byte(input.Password))
		loginReason = "user_not_found"
		s.recordIPFailure(r, settings)
		s.recordAudit(r, nil, username, "login", "", "failure", "user_not_found")
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if err != nil {
		loginReason = "user_not_found"
		log.Printf("get login user: %v", err)
		s.recordIPFailure(r, settings)
		s.recordAudit(r, nil, username, "login", "", "failure", "user_not_found")
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	userID := user.ID
	if user.Disabled {
		loginReason = "user_disabled"
		s.recordIPFailure(r, settings)
		s.recordAudit(r, &userID, user.Username, "login", "", "failure", "user_disabled")
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if user.LockedUntil != "" {
		lockedUntil, parseErr := time.Parse(time.RFC3339, user.LockedUntil)
		if parseErr == nil && time.Now().UTC().Before(lockedUntil) {
			loginReason = "locked"
			s.recordAudit(r, &userID, user.Username, "login", "", "failure", "locked")
			writeError(w, http.StatusUnauthorized, "用户名或密码错误")
			return
		}
		if parseErr == nil || user.LockedUntil != "" {
			if err := s.store.ResetLoginState(r.Context(), user.ID); err != nil {
				log.Printf("reset expired login lock: %v", err)
			}
		}
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.Password)) != nil {
		loginReason = "wrong_password"
		if err := s.store.RecordLoginFailure(r.Context(), user.ID, settings.LockThreshold, settings.AutoUnlockEnabled, settings.AutoUnlockMinutes); err != nil {
			log.Printf("record login failure: %v", err)
		}
		s.recordIPFailure(r, settings)
		s.recordAudit(r, &userID, user.Username, "login", "", "failure", "wrong_password")
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if user.TOTPSecret != "" {
		challenge, err := s.issueTOTPChallenge(user)
		if err != nil {
			loginReason = "token_issue"
			writeError(w, http.StatusInternalServerError, "登录失败")
			return
		}
		data := map[string]any{"totpChallenge": challenge}
		if user.TOTPEnabled {
			data["totpRequired"] = true
		} else {
			secret, err := s.decryptTOTPSecretForUser(r.Context(), user)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "登录失败")
				return
			}
			data["totpSetup"] = true
			data["secret"] = secret
			data["otpauthUrl"] = totpURL(user.Username, secret)
		}
		loginResult, loginReason = "challenge", "totp_required"
		writeData(w, http.StatusOK, "需要动态验证码", data)
		return
	}
	if err := s.store.ResetLoginState(r.Context(), user.ID); err != nil {
		log.Printf("reset login state: %v", err)
	}
	if err := s.store.ResetIPFailure(r.Context(), ip); err != nil {
		log.Printf("reset ip failure: %v", err)
	}
	token, err := s.issueToken(user)
	if err != nil {
		loginReason = "token_issue"
		writeError(w, http.StatusInternalServerError, "登录失败")
		return
	}
	s.recordAudit(r, &userID, user.Username, "login", "", "success", "")
	loginResult, loginReason = "success", ""
	writeData(w, http.StatusOK, "登录成功", map[string]any{"token": token, "user": publicUser(user)})
}

// register creates a normal user only when the administrator-enabled setting is true.
// register 在管理员开启注册设置后创建普通用户，并直接签发登录令牌。
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var input registerRequest
	operator := "anonymous"
	result, reason := "failure", "invalid_request"
	defer func() { s.serviceEvent(r, "register", operator, "result=%s reason=%s", result, reason) }()
	defer func() { s.recordAudit(r, nil, operator, "register", strings.TrimSpace(input.Username), result, reason) }()
	if !s.rateLimiter.allowPublicRequest(s.requestIP(r), 5, 5) {
		reason = "rate_limited"
		writeError(w, http.StatusTooManyRequests, "请求过于频繁，请稍后重试")
		return
	}
	settings, err := s.store.GetLogSettings(r.Context())
	if err != nil {
		reason = "settings_unavailable"
		writeError(w, http.StatusInternalServerError, "读取注册设置失败")
		return
	}
	if !settings.RegisterEnabled {
		reason = "register_disabled"
		writeErrorData(w, http.StatusForbidden, "注册功能未开放", map[string]string{"code": "REGISTER_DISABLED"})
		return
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	operator = input.Username
	if !validUsername(input.Username) {
		reason = "invalid_username"
		writeError(w, http.StatusBadRequest, "用户信息无效")
		return
	}
	if err := s.validatePassword(input.Password); err != nil {
		reason = "invalid_password"
		writeError(w, http.StatusBadRequest, "密码不符合强度要求")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		reason = "hash_failed"
		writeError(w, http.StatusInternalServerError, "密码处理失败")
		return
	}
	if err := s.store.CreateUser(r.Context(), input.Username, string(hash), "user", 100*1024*1024*1024); errors.Is(err, store.ErrConflict) {
		reason = "username_exists"
		writeError(w, http.StatusConflict, "用户名已存在")
		return
	} else if err != nil {
		reason = "create_failed"
		writeError(w, http.StatusInternalServerError, "创建用户失败")
		return
	}
	user, err := s.store.GetUserByUsername(input.Username)
	if err != nil {
		reason = "load_failed"
		writeError(w, http.StatusInternalServerError, "创建用户失败")
		return
	}
	token, err := s.issueToken(user)
	if err != nil {
		reason = "token_issue"
		writeError(w, http.StatusInternalServerError, "注册失败")
		return
	}
	result, reason = "success", ""
	writeData(w, http.StatusCreated, "注册成功", map[string]any{"token": token, "user": publicUser(user)})
}

func validUsername(username string) bool {
	if username == "" || !utf8.ValidString(username) || utf8.RuneCountInString(username) > 64 {
		return false
	}
	for _, char := range username {
		if unicode.IsControl(char) || unicode.IsSpace(char) {
			return false
		}
		switch char {
		case '/', '\\', '<', '>', ':', '"', '|', '?', '*':
			return false
		}
	}
	return true
}

func (s *Server) recordIPFailure(r *http.Request, settings store.LogSettings) {
	if err := s.store.RecordIPFailure(r.Context(), s.requestIP(r), settings.IPLockWindowMinutes, settings.IPLockThreshold, settings.IPAutoUnlockEnabled, settings.IPUnlockMinutes); err != nil {
		log.Printf("record ip failure: %v", err)
	}
}

// changePassword verifies the old credential, enforces the current policy, and rotates the JWT.
// changePassword 校验旧密码、执行当前策略并重新签发 JWT。
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	auditResult, auditReason := "failure", "failure"
	defer func() {
		s.recordAudit(r, &user.ID, user.Username, "password_change", user.Username, auditResult, auditReason)
	}()
	if s.rejectReadOnly(w, r, user, "password_change", user.Username) {
		return
	}
	var input changePasswordRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.OldPassword)) != nil {
		writeError(w, http.StatusBadRequest, "旧密码错误")
		return
	}
	if err := s.validatePassword(input.NewPassword); err != nil {
		writeError(w, http.StatusBadRequest, "密码不符合强度要求")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "密码处理失败")
		return
	}
	if err := s.store.ChangePassword(r.Context(), user.ID, string(hash)); err != nil {
		writeError(w, http.StatusInternalServerError, "修改密码失败")
		return
	}
	s.serviceEvent(r, "password_change", user.Username, "target=%s result=success", user.Username)
	updated, err := s.store.GetUser(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "获取用户信息失败")
		return
	}
	token, err := s.issueToken(updated)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "登录失败")
		return
	}
	auditResult, auditReason = "success", "success"
	writeData(w, http.StatusOK, "密码已更新", map[string]any{"token": token, "user": publicUser(updated)})
}

func (s *Server) passwordPolicy(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.GetLogSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "获取密码策略失败")
		return
	}
	writeData(w, http.StatusOK, "获取成功", map[string]int{"passwordMinLength": settings.PasswordMinLength, "passwordComplexity": settings.PasswordComplexity})
}

func (s *Server) validatePassword(password string) error {
	settings, err := s.store.GetLogSettings(context.Background())
	if err != nil {
		return err
	}
	if password == "" || utf8.RuneCountInString(password) < settings.PasswordMinLength || utf8.RuneCountInString(password) > 200 {
		return errors.New("password policy")
	}
	classes := 0
	var upper, lower, digit, special bool
	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			upper = true
		case unicode.IsLower(char):
			lower = true
		case unicode.IsDigit(char):
			digit = true
		default:
			special = true
		}
	}
	for _, present := range []bool{upper, lower, digit, special} {
		if present {
			classes++
		}
	}
	if classes < settings.PasswordComplexity {
		return errors.New("password policy")
	}
	return nil
}

func (s *Server) issueTOTPChallenge(user store.User) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{"sub": strconv.FormatInt(user.ID, 10), "purpose": "totp", "exp": now.Add(5 * time.Minute).Unix(), "iat": now.Unix()}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.config.JWTSecret)
}

func (s *Server) parseTOTPChallenge(value string) (int64, error) {
	token, err := jwt.Parse(strings.TrimSpace(value), func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return s.config.JWTSecret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil || !token.Valid {
		return 0, errors.New("invalid totp challenge")
	}
	purpose, _ := token.Claims.(jwt.MapClaims)["purpose"].(string)
	if purpose != "totp" {
		return 0, errors.New("invalid totp challenge")
	}
	sub, err := token.Claims.GetSubject()
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(sub, 10, 64)
}

// totp handles both first-time binding and the second step of an enabled login.
// totp 同时处理首次绑定和已启用账号的登录第二步。
func (s *Server) totp(w http.ResponseWriter, r *http.Request) {
	var input totpRequest
	operator := "unknown"
	loginResult, loginReason := "failure", "invalid_request"
	defer func() { s.serviceEvent(r, "login", operator, "result=%s reason=%s", loginResult, loginReason) }()
	if !decodeJSON(w, r, &input) {
		return
	}
	id, err := s.parseTOTPChallenge(input.TOTPChallenge)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	user, err := s.store.GetUser(id)
	if err != nil || user.Disabled || user.TOTPSecret == "" {
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	operator = user.Username
	if len(input.Code) != 6 {
		settings, _ := s.store.GetLogSettings(r.Context())
		s.recordIPFailure(r, settings)
		s.recordAudit(r, &user.ID, user.Username, "login", "", "failure", "totp_failed")
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	secret, err := s.decryptTOTPSecretForUser(r.Context(), user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "登录失败")
		return
	}
	now := time.Now().UTC()
	baseCounter := now.Unix() / 30
	matchedCounter := int64(-1)
	for offset := int64(-1); offset <= 1; offset++ {
		candidate := baseCounter + offset
		if hmac.Equal([]byte(totpCode(secret, candidate)), []byte(input.Code)) {
			matchedCounter = candidate
			break
		}
	}
	settings, _ := s.store.GetLogSettings(r.Context())
	if matchedCounter < 0 {
		s.recordIPFailure(r, settings)
		s.recordAudit(r, &user.ID, user.Username, "login", "", "failure", "totp_failed")
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	consumed, err := s.store.ConsumeTOTP(r.Context(), user.ID, matchedCounter, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "登录失败")
		return
	}
	if !consumed {
		s.recordIPFailure(r, settings)
		s.recordAudit(r, &user.ID, user.Username, "login", "", "failure", "totp_failed")
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if !user.TOTPEnabled {
		if err := s.store.ActivateTOTP(r.Context(), user.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "绑定动态验证失败")
			return
		}
	}
	if err := s.store.ResetIPFailure(r.Context(), s.requestIP(r)); err != nil {
		log.Printf("reset ip failure after totp: %v", err)
	}
	updated, err := s.store.GetUser(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "获取用户信息失败")
		return
	}
	token, err := s.issueToken(updated)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "登录失败")
		return
	}
	s.recordAudit(r, &updated.ID, updated.Username, "login", "", "success", "")
	loginResult, loginReason = "success", ""
	writeData(w, http.StatusOK, "登录成功", map[string]any{"token": token, "user": publicUser(updated)})
}

func (s *Server) totpQRCode(w http.ResponseWriter, r *http.Request) {
	id, err := s.parseTOTPChallenge(r.URL.Query().Get("challenge"))
	if err != nil {
		http.Error(w, "invalid challenge", http.StatusUnauthorized)
		return
	}
	user, err := s.store.GetUser(id)
	if err != nil || user.TOTPSecret == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	secret, err := s.decryptTOTPSecretForUser(r.Context(), user)
	if err != nil {
		http.Error(w, "qrcode unavailable", http.StatusInternalServerError)
		return
	}
	png, err := qrcode.Encode(totpURL(user.Username, secret), qrcode.Medium, 256)
	if err != nil {
		http.Error(w, "qrcode unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}

func (s *Server) encryptTOTPSecret(secret string) (string, error) {
	return s.encryptSensitiveSecret(secret)
}

func (s *Server) decryptTOTPSecret(value string) (string, error) {
	secret, _, err := s.decryptSensitiveSecret(value)
	return secret, err
}

func (s *Server) decryptTOTPSecretForUser(ctx context.Context, user store.User) (string, error) {
	secret, legacy, err := s.decryptSensitiveSecret(user.TOTPSecret)
	if err != nil {
		return "", err
	}
	if legacy && len(s.config.EncryptionKey) == 32 {
		migrated, encryptErr := s.encryptTOTPSecret(secret)
		if encryptErr != nil {
			log.Printf("migrate TOTP secret for user id=%d: %v", user.ID, encryptErr)
		} else if updateErr := s.store.ReencryptTOTPSecret(ctx, user.ID, migrated); updateErr != nil {
			log.Printf("persist migrated TOTP secret for user id=%d: %v", user.ID, updateErr)
		}
	}
	return secret, nil
}

func totpURL(username, secret string) string {
	return "otpauth://totp/FileBox:" + url.PathEscape(username) + "?secret=" + secret + "&issuer=FileBox"
}

func totpCode(secret string, counter int64) string {
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return ""
	}
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], uint64(counter))
	mac := hmac.New(sha1.New, decoded)
	_, _ = mac.Write(message[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset])&0x7f)<<24 | uint32(sum[offset+1])<<16 | uint32(sum[offset+2])<<8 | uint32(sum[offset+3])
	return fmt.Sprintf("%06d", value%1000000)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	if err := s.store.RevokeUserTokens(r.Context(), user.ID); err != nil {
		log.Printf("revoke user tokens: %v", err)
		s.serviceEvent(r, "logout", user.Username, "result=failure")
		writeError(w, http.StatusInternalServerError, "退出登录失败")
		return
	}
	s.serviceEvent(r, "logout", user.Username, "result=success")
	writeData(w, http.StatusOK, "已退出登录", nil)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	writeData(w, http.StatusOK, "获取成功", publicUser(user))
}

// updateLanguage validates and persists the authenticated user's language preference.
// updateLanguage 校验并保存已登录用户的语言偏好。
func (s *Server) updateLanguage(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	if s.rejectReadOnly(w, r, user, "language_update", user.Username) {
		return
	}
	var input languageRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	if !isValidLang(input.Language) {
		writeError(w, http.StatusBadRequest, "语言设置无效")
		return
	}
	user, err := s.store.UpdateUserLanguage(r.Context(), user.ID, input.Language)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "请先登录")
		return
	}
	if err != nil {
		log.Printf("update user language: %v", err)
		writeError(w, http.StatusInternalServerError, "保存语言设置失败")
		return
	}
	s.serviceEvent(r, "language_update", user.Username, "target=%s language=%s result=success", user.Username, input.Language)
	writeData(w, http.StatusOK, "语言设置已更新", publicUser(user))
}

// uploadChunkLayout 按阶段一兼容规则计算实际分片大小和分片总数。
// uploadChunkLayout maps the requested chunk size to the backward-compatible upload layout.
func uploadChunkLayout(size, requested int64) (int64, int, error) {
	if size == 0 {
		return 1, 1, nil
	}
	if requested == 0 || requested >= size {
		return size, 1, nil
	}
	if requested < 2*1024*1024 || requested > 8*1024*1024 {
		return 0, 0, errors.New("分片大小必须在 2MB-8MB 之间")
	}
	return requested, int((size-1)/requested + 1), nil
}

// expectedUploadChunkSize 返回指定分片必须接收的精确字节数。
// expectedUploadChunkSize returns the exact byte count required for one chunk.
func expectedUploadChunkSize(task store.UploadTask, index int) int64 {
	if index < task.TotalChunks-1 {
		return task.ChunkSize
	}
	return task.Size - task.ChunkSize*int64(task.TotalChunks-1)
}

// validateUploadDir 校验相对目录并返回去掉首尾斜杠后的规范化路径。
// validateUploadDir validates a relative folder path and returns its slash-normalized form.
func validateUploadDir(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}
	if strings.HasPrefix(dir, "/") || strings.HasPrefix(dir, "\\") || filepath.IsAbs(dir) || filepath.VolumeName(dir) != "" {
		return "", errors.New("invalid directory")
	}
	dir = strings.Trim(dir, "/")
	if dir == "" {
		return "", errors.New("invalid directory")
	}
	parts := strings.Split(dir, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.Contains(part, "\\") {
			return "", errors.New("invalid directory")
		}
		for _, char := range part {
			if unicode.IsControl(char) || strings.ContainsRune(`<>:"|?*`, char) {
				return "", errors.New("invalid directory")
			}
		}
	}
	return strings.Join(parts, "/"), nil
}

func (s *Server) uploadInit(w http.ResponseWriter, r *http.Request) {
	// uploadInit 先校验文件名、大小、同名处理和磁盘空间，再以事务预留用户配额。
	// uploadInit validates the name, size, conflict mode, and disk space before transactionally reserving user quota.
	user := currentUser(r.Context())
	if s.rejectReadOnly(w, r, user, "upload_init", "") {
		return
	}
	var input uploadInitRequest
	if !decodeJSON(w, r, &input) {
		s.recordAudit(r, &user.ID, user.Username, "upload_init", "", "failure", "invalid_request")
		s.serviceEvent(r, "upload_init", user.Username, "result=failure reason=invalid_request")
		return
	}
	// 失败审计：upload-init 的每个拒绝分支都记录审计与服务事件，便于后台日志页与
	// server.err.log 排查（原因细分 invalid_name/too_large/conflict/disk_full/quota_exceeded/…）。
	// Failure audit: every rejection branch records an audit row and a service event so the
	// admin log page and server.err.log can explain why initialization failed.
	auditName := input.Name
	rejectInit := func(status int, message, reason string, data any) {
		s.recordAudit(r, &user.ID, user.Username, "upload_init", auditName, "failure", reason)
		s.serviceEvent(r, "upload_init", user.Username, "name=%s size=%d result=failure reason=%s", auditName, input.Size, reason)
		if data != nil {
			writeErrorData(w, status, message, data)
		} else {
			writeError(w, status, message)
		}
	}
	displayName, err := validateUploadName(input.Name)
	if err != nil {
		rejectInit(http.StatusBadRequest, "文件名包含非法字符，禁止上传", "invalid_name", nil)
		return
	}
	name, err := sanitizeName(displayName)
	if err != nil || input.Size < 0 {
		rejectInit(http.StatusBadRequest, "文件名或文件大小无效", "invalid_name", nil)
		return
	}
	if input.Size > s.config.MaxFileSize {
		rejectInit(http.StatusRequestEntityTooLarge, "文件超过单文件大小上限", "too_large", map[string]any{"code": "FILE_TOO_LARGE", "maxFileSize": s.config.MaxFileSize})
		return
	}
	if input.Resolve != "" && input.Resolve != "overwrite" && input.Resolve != "rename" {
		rejectInit(http.StatusBadRequest, "冲突处理方式无效", "invalid_resolve", nil)
		return
	}
	dir, err := validateUploadDir(input.Dir)
	if err != nil {
		rejectInit(http.StatusBadRequest, "目录无效", "invalid_dir", nil)
		return
	}
	// v011：上传目标目录自动补齐目录记录，保证导航与上传一致。
	// v011: upload target directories get folder records created so navigation stays consistent with uploads.
	if err := s.store.EnsureFolderPath(r.Context(), user.ID, dir); err != nil {
		log.Printf("ensure folder path: %v", err)
	}
	relativeDir := filepath.Join("files", strconv.FormatInt(user.ID, 10), dir)
	conflict, conflictErr := s.store.FindUploadConflict(r.Context(), user.ID, relativeDir, name)
	if conflictErr != nil && !errors.Is(conflictErr, store.ErrNotFound) {
		log.Printf("find upload conflict: %v", conflictErr)
		rejectInit(http.StatusInternalServerError, "检查同名文件失败", "conflict_check_failed", nil)
		return
	}
	if conflictErr == nil && input.Resolve == "" {
		rejectInit(http.StatusConflict, "同名文件已存在", "conflict", map[string]any{
			"conflict": true,
			"existing": map[string]any{"id": conflict.ID, "name": conflict.Name, "size": conflict.Size, "createdAt": conflict.CreatedAt, "md5": conflict.MD5},
		})
		return
	}
	if s.config.MinFreeSpace > 0 {
		// 磁盘保护在创建上传任务前拒绝低于阈值的可用空间。
		// Disk protection rejects upload initialization when free space is below the configured threshold.
		_, free, _, err := diskusage.DiskUsage(s.config.DataDir)
		if err != nil {
			log.Printf("check disk usage: %v", err)
			rejectInit(http.StatusInternalServerError, "无法检查系统存储空间", "disk_check_failed", nil)
			return
		}
		if free < s.config.MinFreeSpace {
			rejectInit(http.StatusServiceUnavailable, "系统存储空间不足，暂时禁止上传", "disk_full", map[string]string{"code": "DISK_FULL"})
			return
		}
	}
	chunkSize, totalChunks, err := uploadChunkLayout(input.Size, input.ChunkSize)
	if err != nil {
		rejectInit(http.StatusBadRequest, err.Error(), "invalid_chunk_size", nil)
		return
	}
	taskID, err := randomID()
	if err != nil {
		rejectInit(http.StatusInternalServerError, "创建上传任务失败", "task_create_failed", nil)
		return
	}
	mimeType := strings.TrimSpace(input.Mime)
	if mimeType == "" {
		mimeType = mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	task := store.UploadTask{ID: taskID, UserID: user.ID, Name: name, Size: input.Size, ChunkSize: chunkSize, TotalChunks: totalChunks, Status: "pending", Mime: mimeType, StorageDir: relativeDir, Resolve: input.Resolve}
	if err := s.store.CreateUploadTask(r.Context(), task); err != nil {
		var quotaErr *store.QuotaError
		if errors.As(err, &quotaErr) {
			rejectInit(http.StatusForbidden, "超出用户配额", "quota_exceeded", map[string]any{"code": "QUOTA_EXCEEDED", "usedBytes": quotaErr.UsedBytes, "quotaBytes": quotaErr.QuotaBytes, "fileSize": quotaErr.FileSize})
			return
		}
		log.Printf("create upload task: %v", err)
		rejectInit(http.StatusInternalServerError, "创建上传任务失败", "task_create_failed", nil)
		return
	}
	s.serviceEvent(r, "upload_init", user.Username, "name=%s size=%d result=success task=%s", name, input.Size, task.ID)
	writeData(w, http.StatusOK, "上传任务已创建", map[string]any{"taskId": task.ID, "chunkSize": task.ChunkSize, "totalChunks": task.TotalChunks, "uploadedChunks": []int{}})
}

// deleteUploadTask 删除当前用户拥有的待上传任务，管理员可删除任意用户的任务并清理临时分片。
// deleteUploadTask deletes an owner's pending upload task; admins may delete any task and its temporary chunks.
func (s *Server) deleteUploadTask(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	taskID := r.PathValue("taskID")
	task, err := s.store.GetUploadTask(r.Context(), taskID)
	if errors.Is(err, store.ErrNotFound) || err != nil || (task.UserID != user.ID && user.Role != "admin") || task.Status != "pending" {
		writeErrorData(w, http.StatusNotFound, "上传任务不存在", map[string]string{"code": "task_not_found"})
		return
	}
	if err := s.store.DeleteUploadTask(r.Context(), taskID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErrorData(w, http.StatusNotFound, "上传任务不存在", map[string]string{"code": "task_not_found"})
			return
		}
		log.Printf("delete upload task %s: %v", taskID, err)
		writeError(w, http.StatusInternalServerError, "删除上传任务失败")
		return
	}
	if err := os.RemoveAll(filepath.Join(s.config.DataDir, "tmp", taskID)); err != nil {
		log.Printf("remove upload task temporary directory %s: %v", taskID, err)
	}
	s.serviceEvent(r, "upload_cancel", user.Username, "task=%s result=success", taskID)
	writeData(w, http.StatusOK, "上传任务已删除", nil)
}

func (s *Server) uploadChunk(w http.ResponseWriter, r *http.Request) {
	// uploadChunk 按任务声明的精确大小流式写入单个分片并记录哈希。
	// uploadChunk streams one exact-sized chunk to disk and records its hash.
	user := currentUser(r.Context())
	taskID := r.PathValue("taskID")
	if s.rejectReadOnly(w, r, user, "upload_chunk", taskID) {
		return
	}
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		s.recordAudit(r, &user.ID, user.Username, "upload_chunk", taskID, "failure", "invalid_index")
		s.serviceEvent(r, "upload_chunk", user.Username, "task=%s index=%s result=failure reason=invalid_index", taskID, r.PathValue("index"))
		writeError(w, http.StatusBadRequest, "分片序号无效")
		return
	}
	task, err := s.store.GetUploadTask(r.Context(), taskID)
	if err == nil && task.UserID == user.ID && task.Status == "queued" {
		writeErrorData(w, http.StatusConflict, "收集上传任务仍在排队，请等待活动槽位", map[string]string{"code": "COLLECTION_TASK_QUEUED"})
		return
	}
	if errors.Is(err, store.ErrNotFound) || task.UserID != user.ID || task.Status != "pending" {
		s.recordAudit(r, &user.ID, user.Username, "upload_chunk", taskID, "failure", "task_not_found")
		s.serviceEvent(r, "upload_chunk", user.Username, "task=%s index=%d result=failure reason=task_not_found", taskID, index)
		writeError(w, http.StatusNotFound, "上传任务不存在")
		return
	}
	if index < 0 || index >= task.TotalChunks {
		s.recordAudit(r, &user.ID, user.Username, "upload_chunk", task.Name, "failure", "invalid_index")
		s.serviceEvent(r, "upload_chunk", user.Username, "name=%s index=%d result=failure reason=invalid_index", task.Name, index)
		writeError(w, http.StatusBadRequest, "分片序号无效")
		return
	}
	expectedSize := expectedUploadChunkSize(task, index)
	if r.ContentLength >= 0 && r.ContentLength > expectedSize {
		s.recordAudit(r, &user.ID, user.Username, "upload_chunk", task.Name, "failure", "too_large")
		s.serviceEvent(r, "upload_chunk", user.Username, "name=%s index=%d result=failure reason=too_large", task.Name, index)
		writeError(w, http.StatusRequestEntityTooLarge, "上传内容超过声明大小")
		return
	}
	settings, err := s.store.GetLogSettings(r.Context())
	if err != nil {
		log.Printf("get upload settings: %v", err)
		s.recordAudit(r, &user.ID, user.Username, "upload_chunk", task.Name, "failure", "settings_failed")
		s.serviceEvent(r, "upload_chunk", user.Username, "name=%s index=%d result=failure reason=settings_failed", task.Name, index)
		writeError(w, http.StatusInternalServerError, "读取上传设置失败")
		return
	}
	if limiter := s.rateLimiter.limiterFor(user.ID, settings.UploadRateLimit); limiter != nil {
		waitContext, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		waitErr := waitForUploadRate(waitContext, limiter, expectedSize)
		cancel()
		if waitErr != nil {
			s.recordAudit(r, &user.ID, user.Username, "upload_chunk", task.Name, "failure", "rate_limited")
			s.serviceEvent(r, "upload_chunk", user.Username, "name=%s index=%d result=failure reason=rate_limited", task.Name, index)
			writeError(w, http.StatusTooManyRequests, "上传过慢，请稍后重试")
			return
		}
	}
	tmpDir := filepath.Join(s.config.DataDir, "tmp", task.ID)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		s.recordAudit(r, &user.ID, user.Username, "upload_chunk", task.Name, "failure", "prepare_failed")
		s.serviceEvent(r, "upload_chunk", user.Username, "name=%s index=%d result=failure reason=prepare_failed", task.Name, index)
		writeError(w, http.StatusInternalServerError, "无法准备上传空间")
		return
	}
	path := filepath.Join(tmpDir, strconv.Itoa(index))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		s.recordAudit(r, &user.ID, user.Username, "upload_chunk", task.Name, "failure", "write_failed")
		s.serviceEvent(r, "upload_chunk", user.Username, "name=%s index=%d result=failure reason=write_failed", task.Name, index)
		writeError(w, http.StatusInternalServerError, "无法写入上传内容")
		return
	}
	hash := sha256.New()
	written, copyErr := copyRequestBodyWithIdleTimeout(r.Context(), io.MultiWriter(file, hash), r.Body, expectedSize+1)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(path)
		s.recordAudit(r, &user.ID, user.Username, "upload_chunk", task.Name, "failure", "write_failed")
		s.serviceEvent(r, "upload_chunk", user.Username, "name=%s index=%d result=failure reason=write_failed", task.Name, index)
		writeError(w, http.StatusBadRequest, "写入上传内容失败")
		return
	}
	if written > expectedSize {
		_ = os.Remove(path)
		s.recordAudit(r, &user.ID, user.Username, "upload_chunk", task.Name, "failure", "too_large")
		s.serviceEvent(r, "upload_chunk", user.Username, "name=%s index=%d result=failure reason=too_large", task.Name, index)
		writeError(w, http.StatusRequestEntityTooLarge, "上传内容超过声明大小")
		return
	}
	if written != expectedSize {
		_ = os.Remove(path)
		s.recordAudit(r, &user.ID, user.Username, "upload_chunk", task.Name, "failure", "size_mismatch")
		s.serviceEvent(r, "upload_chunk", user.Username, "name=%s index=%d result=failure reason=size_mismatch", task.Name, index)
		writeError(w, http.StatusBadRequest, "上传内容大小与声明不一致")
		return
	}
	if err := s.store.SetChunk(r.Context(), task.ID, index, written, hex.EncodeToString(hash.Sum(nil))); err != nil {
		_ = os.Remove(path)
		log.Printf("set uploaded chunk: %v", err)
		s.recordAudit(r, &user.ID, user.Username, "upload_chunk", task.Name, "failure", "save_failed")
		s.serviceEvent(r, "upload_chunk", user.Username, "name=%s index=%d result=failure reason=save_failed", task.Name, index)
		writeError(w, http.StatusInternalServerError, "保存上传分片失败")
		return
	}
	writeData(w, http.StatusOK, "分片上传成功", map[string]any{"index": index, "size": written})
}

// rateLimiterMaxKeys 限制各令牌桶 map 的容量，防止来源 IP / 用户键无限增长的内存耗尽攻击。
// rateLimiterMaxKeys caps each token-bucket map so unbounded source keys cannot exhaust memory.
const rateLimiterMaxKeys = 10000

// evictIfFull 在惰性清理后仍超容量时驱逐最久未访问的键。
// evictIfFull evicts the least-recently-seen key when a map still exceeds capacity after the lazy sweep.
func evictIfFull[K comparable](now time.Time, lastSeen map[K]time.Time, target map[K]*rate.Limiter) {
	if len(target) < rateLimiterMaxKeys {
		return
	}
	var oldestKey K
	var oldestSeen time.Time
	found := false
	for key, seen := range lastSeen {
		if !found || seen.Before(oldestSeen) {
			oldestKey, oldestSeen, found = key, seen, true
		}
	}
	if found {
		delete(lastSeen, oldestKey)
		delete(target, oldestKey)
	}
}

// limiterFor returns or rebuilds a user's bucket and evicts entries idle for ten minutes.
// limiterFor 返回或重建用户令牌桶，并清理超过十分钟未访问的条目。
func (l *rateLimiter) limiterFor(userID, bytesPerSec int64) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buckets == nil {
		l.buckets = make(map[int64]*rate.Limiter)
	}
	if l.lastSeen == nil {
		l.lastSeen = make(map[int64]time.Time)
	}
	now := time.Now()
	for id, seen := range l.lastSeen {
		if now.Sub(seen) > 10*time.Minute {
			delete(l.lastSeen, id)
			delete(l.buckets, id)
		}
	}
	evictIfFull(now, l.lastSeen, l.buckets)
	if bytesPerSec <= 0 {
		return nil
	}
	limiter, ok := l.buckets[userID]
	if !ok || limiter == nil || int64(limiter.Limit()) != bytesPerSec {
		burst := bytesPerSec
		if burst < 1024*1024 {
			burst = 1024 * 1024
		}
		limiter = rate.NewLimiter(rate.Limit(bytesPerSec), int(burst))
		// Start new buckets empty so the configured bytes-per-second rate applies to the first chunk too.
		// 新建令牌桶从空开始，确保首个分片也遵守配置的每秒字节数。
		limiter.ReserveN(now, limiter.Burst())
		l.buckets[userID] = limiter
	}
	l.lastSeen[userID] = now
	return limiter
}

// limiterForPublic applies the same byte-rate bucket to anonymous IP/collection keys.
// limiterForPublic 对匿名 IP 与收集 token 组合使用同一套字节限速基础设施。
func (l *rateLimiter) limiterForPublic(key string, bytesPerSec int64) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.publicBuckets == nil {
		l.publicBuckets = make(map[string]*rate.Limiter)
	}
	if l.publicLastSeen == nil {
		l.publicLastSeen = make(map[string]time.Time)
	}
	now := time.Now()
	for item, seen := range l.publicLastSeen {
		if now.Sub(seen) > 10*time.Minute {
			delete(l.publicLastSeen, item)
			delete(l.publicBuckets, item)
		}
	}
	evictIfFull(now, l.publicLastSeen, l.publicBuckets)
	if bytesPerSec <= 0 {
		return nil
	}
	limiter, ok := l.publicBuckets[key]
	if !ok || limiter == nil || int64(limiter.Limit()) != bytesPerSec {
		burst := bytesPerSec
		if burst < 1024*1024 {
			burst = 1024 * 1024
		}
		limiter = rate.NewLimiter(rate.Limit(bytesPerSec), int(burst))
		limiter.ReserveN(now, limiter.Burst())
		l.publicBuckets[key] = limiter
	}
	l.publicLastSeen[key] = now
	return limiter
}

// allowPublicRequest 对匿名公开接口按来源 IP 做请求级限速（令牌桶，突发耗尽后按速率补充）。
// allowPublicRequest rate-limits anonymous public endpoints per source IP with a token bucket.
func (l *rateLimiter) allowPublicRequest(ip string, perMinute, burst int) bool {
	if perMinute <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.requestBuckets == nil {
		l.requestBuckets = make(map[string]*rate.Limiter)
	}
	if l.requestLastSeen == nil {
		l.requestLastSeen = make(map[string]time.Time)
	}
	now := time.Now()
	for key, seen := range l.requestLastSeen {
		if now.Sub(seen) > 10*time.Minute {
			delete(l.requestLastSeen, key)
			delete(l.requestBuckets, key)
		}
	}
	evictIfFull(now, l.requestLastSeen, l.requestBuckets)
	limiter, ok := l.requestBuckets[ip]
	limit := rate.Limit(float64(perMinute) / 60.0)
	if !ok || limiter == nil || limiter.Limit() != limit {
		limiter = rate.NewLimiter(limit, burst)
		l.requestBuckets[ip] = limiter
	}
	l.requestLastSeen[ip] = now
	return limiter.Allow()
}

func waitForUploadRate(ctx context.Context, limiter *rate.Limiter, bytes int64) error {
	// waitForUploadRate waits in burst-sized pieces so large chunks remain compatible with small bursts.
	// waitForUploadRate 按 burst 大小分段等待，确保大分片兼容较小的 burst。
	remaining := bytes
	for remaining > 0 {
		batch := remaining
		if limit := int64(limiter.Burst()); batch > limit {
			batch = limit
		}
		if err := limiter.WaitN(ctx, int(batch)); err != nil {
			return err
		}
		remaining -= batch
	}
	return nil
}

func (s *Server) uploadStatus(w http.ResponseWriter, r *http.Request) {
	// uploadStatus 返回当前用户任务的持久化分片状态。
	// uploadStatus returns the authenticated user's persisted chunk state.
	user := currentUser(r.Context())
	task, err := s.store.GetUploadTask(r.Context(), r.PathValue("taskID"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "上传任务不存在")
		return
	}
	if err != nil {
		log.Printf("get upload status: %v", err)
		writeError(w, http.StatusInternalServerError, "读取上传任务失败")
		return
	}
	if task.UserID != user.ID {
		writeError(w, http.StatusNotFound, "上传任务不存在")
		return
	}
	if task.Status == "queued" {
		writeErrorData(w, http.StatusConflict, "收集上传任务仍在排队，暂不能读取分片状态", map[string]string{"code": "COLLECTION_TASK_QUEUED"})
		return
	}
	chunks, err := s.store.ListChunks(r.Context(), task.ID)
	if err != nil {
		log.Printf("list upload status chunks: %v", err)
		writeError(w, http.StatusInternalServerError, "读取上传分片失败")
		return
	}
	indices := make([]int, 0, len(chunks))
	for index := range chunks {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	writeData(w, http.StatusOK, "获取成功", map[string]any{
		"taskId":         task.ID,
		"name":           task.Name,
		"size":           task.Size,
		"chunkSize":      task.ChunkSize,
		"totalChunks":    task.TotalChunks,
		"status":         task.Status,
		"uploadedChunks": indices,
	})
}

// uploadProgressStream 通过 SSE 每秒推送当前用户的进行中上传任务进度。
// uploadProgressStream pushes live upload progress for the current user over SSE every second.
func (s *Server) uploadProgressStream(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "当前连接不支持流式推送")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	send := func() {
		progress, err := s.store.ListPendingTaskProgress(r.Context(), user.ID)
		if err != nil {
			return
		}
		data, _ := json.Marshal(progress)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
	send()
	nextAuthCheck := time.Now().Add(30 * time.Second)
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if time.Now().After(nextAuthCheck) {
				nextAuthCheck = time.Now().Add(30 * time.Second)
				if _, err := s.authenticate(r); err != nil {
					_, _ = fmt.Fprint(w, "event: auth-error\ndata: {\"status\":401}\n\n")
					flusher.Flush()
					return
				}
			}
			send()
		}
	}
}

func (s *Server) checkInstantUpload(w http.ResponseWriter, r *http.Request) {
	// checkInstantUpload 在当前用户范围内查询 ready 文件，不创建新的上传内容。
	// checkInstantUpload searches the current user's ready files without creating upload content.
	user := currentUser(r.Context())
	if s.rejectReadOnly(w, r, user, "upload", "") {
		return
	}
	var input instantCheckRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.Size <= 0 {
		writeError(w, http.StatusBadRequest, "文件大小无效")
		return
	}
	input.MD5 = strings.ToLower(strings.TrimSpace(input.MD5))
	input.SHA256 = strings.ToLower(strings.TrimSpace(input.SHA256))
	if input.MD5 == "" && input.SHA256 == "" {
		writeError(w, http.StatusBadRequest, "文件校验值缺失")
		return
	}
	if input.Size > s.config.MaxFileSize {
		s.recordAudit(r, &user.ID, user.Username, "upload", input.Name, "failure", "too_large")
		s.serviceEvent(r, "upload", user.Username, "name=%s size=%d result=failure reason=too_large", input.Name, input.Size)
		writeErrorData(w, http.StatusRequestEntityTooLarge, "文件超过单文件大小上限", map[string]any{"code": "FILE_TOO_LARGE", "maxFileSize": s.config.MaxFileSize})
		return
	}
	dir, dirErr := validateUploadDir(input.Dir)
	if dirErr != nil {
		s.recordAudit(r, &user.ID, user.Username, "upload", input.Name, "failure", "invalid_dir")
		s.serviceEvent(r, "upload", user.Username, "name=%s size=%d result=failure reason=invalid_dir", input.Name, input.Size)
		writeError(w, http.StatusBadRequest, "目录无效")
		return
	}
	// 秒传限定在目标目录内查找（与收集箱行为一致）：不同目录的同内容文件不应跨目录秒传，
	// 否则文件会静默落在错误目录而列表为空。未指定目录时回退到全库查找。
	// Instant upload is scoped to the target storage directory (matching collection uploads)
	// so identical content in another directory is not silently "uploaded" into this one.
	// Without a directory the check falls back to a whole-user search.
	var file store.File
	var err error
	if dir == "" {
		file, err = s.store.FindInstantMatch(r.Context(), user.ID, input.MD5, input.SHA256, input.Size)
	} else {
		file, err = s.store.FindInstantMatchInDirectory(r.Context(), user.ID, filepath.Join("files", strconv.FormatInt(user.ID, 10), dir), input.MD5, input.SHA256, input.Size)
	}
	if errors.Is(err, store.ErrNotFound) {
		writeData(w, http.StatusOK, "检查完成", map[string]any{"instant": false})
		return
	}
	if err != nil {
		log.Printf("find instant upload match: %v", err)
		writeError(w, http.StatusInternalServerError, "检查文件失败")
		return
	}
	// 秒传与同名冲突协调：即使内容已在库中，若目标目录存在同名 ready 文件，本次上传
	// 本应触发 409 冲突选择（覆盖/重命名），而非直接秒传——否则用户多次上传同名文件
	// 永远秒传成功、从不弹冲突窗（问题 2）。此时返回 conflict 让前端走冲突流程。
	// Instant-upload and name-conflict coordination: even when the content already exists, a
	// same-name ready file in the target directory means this upload would normally get a 409
	// conflict choice (overwrite/rename). Returning instant here would silently skip that flow.
	// We therefore surface conflict so the frontend can prompt.
	relativeDir := filepath.Join("files", strconv.FormatInt(user.ID, 10), dir)
	name, nameErr := sanitizeName(validateOrEmpty(input.Name))
	if nameErr != nil || name == "" {
		// 未提供文件名时无法做同名判断，保持纯秒传语义。
		// Without a name there is no same-name check; keep pure instant-upload semantics.
		s.recordAudit(r, &user.ID, user.Username, "upload", input.Name, "success", "instant")
		s.serviceEvent(r, "upload", user.Username, "name=%s size=%d result=success reason=instant", input.Name, input.Size)
		writeData(w, http.StatusOK, "检查完成", map[string]any{"instant": true, "file": publicFile(file)})
		return
	}
	conflict, conflictErr := s.findUploadConflict(r.Context(), user.ID, relativeDir, name)
	// 目录内存在同名 ready 文件即应触发冲突流程（覆盖/重命名），即使内容相同。
	// 秒传只应在"目录内无同名文件"时命中——否则用户重复上传同名文件永远静默秒传、
	// 从不弹冲突窗（问题 2）。FindUploadConflict 查的是目标目录内的同名文件。
	// A same-name ready file in the target directory always triggers the conflict flow
	// (overwrite/rename), even when the content matches. Instant upload only applies when
	// no same-name file exists there — otherwise repeated same-name uploads would silently
	// deduplicate and never show the conflict dialog.
	if conflictErr == nil {
		writeData(w, http.StatusOK, "检查完成", map[string]any{"instant": false, "conflict": true, "existing": map[string]any{"id": conflict.ID, "name": conflict.Name, "size": conflict.Size, "createdAt": conflict.CreatedAt, "md5": conflict.MD5}})
		return
	}
	if !errors.Is(conflictErr, store.ErrNotFound) {
		log.Printf("find upload conflict during instant check: %v", conflictErr)
		writeError(w, http.StatusInternalServerError, "检查文件冲突失败")
		return
	}
	// 秒传命中：记录审计（此前无任何日志，问题 9）；target 用本次上传名。
	// Instant-upload hit: record an audit row (previously nothing was logged); the target is the submitted name.
	s.recordAudit(r, &user.ID, user.Username, "upload", input.Name, "success", "instant")
	s.serviceEvent(r, "upload", user.Username, "name=%s size=%d result=success reason=instant", input.Name, input.Size)
	writeData(w, http.StatusOK, "检查完成", map[string]any{"instant": true, "file": publicFile(file)})
}

// validateOrEmpty 返回去除首尾空白后的字符串（非法时返回空）。
// validateOrEmpty trims a string and returns it, or an empty string when invalid.
func validateOrEmpty(value string) string { return strings.TrimSpace(value) }

func (s *Server) completeUpload(w http.ResponseWriter, r *http.Request) {
	// completeUpload 从临时内容计算 SHA-256 与 MD5，并在数据库事务中提交记录和最终文件路径。
	// completeUpload computes SHA-256 and MD5 from temporary content, then commits the record and final path in one transaction.
	user := currentUser(r.Context())
	taskID := r.PathValue("taskID")
	if s.rejectReadOnly(w, r, user, "upload", taskID) {
		return
	}
	task, err := s.store.GetUploadTask(r.Context(), taskID)
	if err == nil && task.UserID == user.ID && task.Status == "queued" {
		writeErrorData(w, http.StatusConflict, "收集上传任务仍在排队，请等待活动槽位", map[string]string{"code": "COLLECTION_TASK_QUEUED"})
		return
	}
	if errors.Is(err, store.ErrNotFound) || task.UserID != user.ID || task.Status != "pending" {
		writeError(w, http.StatusNotFound, "上传任务不存在")
		return
	}
	auditResult, auditReason := "failure", "upload_failed"
	serviceResult, serviceReason := "failure", "upload_failed"
	hashSummary := ""
	defer func() {
		s.serviceEvent(r, "upload", user.Username, "name=%s size=%d sha256=%s result=%s reason=%s", task.Name, task.Size, hashSummary, serviceResult, serviceReason)
	}()
	defer func() { s.recordAudit(r, &user.ID, user.Username, "upload", task.Name, auditResult, auditReason) }()
	var input uploadCompleteRequest
	if r.Body != nil && r.ContentLength != 0 {
		if !decodeJSON(w, r, &input) {
			return
		}
	}
	chunks, err := s.store.ListChunks(r.Context(), task.ID)
	if err != nil {
		log.Printf("list uploaded chunks: %v", err)
		writeError(w, http.StatusInternalServerError, "读取上传分片失败")
		return
	}
	if len(chunks) != task.TotalChunks {
		writeError(w, http.StatusBadRequest, "上传分片不完整")
		return
	}
	tmpDir := filepath.Join(s.config.DataDir, "tmp", task.ID)
	for index, chunk := range chunks {
		if index < 0 || index >= task.TotalChunks || chunk.Size != expectedUploadChunkSize(task, index) {
			writeError(w, http.StatusBadRequest, "上传分片不完整")
			return
		}
		info, statErr := os.Stat(filepath.Join(tmpDir, strconv.Itoa(index)))
		if statErr != nil || info.Size() != chunk.Size {
			writeError(w, http.StatusBadRequest, "上传分片不完整")
			return
		}
	}
	mergedPath := filepath.Join(tmpDir, ".merged")
	merged, err := os.OpenFile(mergedPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
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
			writeError(w, http.StatusBadRequest, "上传分片不完整")
			return
		}
		chunkHash := sha256.New()
		written, copyErr := io.Copy(io.MultiWriter(merged, sha, md5Hash, chunkHash), chunkFile)
		closeErr := chunkFile.Close()
		if copyErr != nil || closeErr != nil {
			merged.Close()
			writeError(w, http.StatusInternalServerError, "合并上传内容失败")
			return
		}
		if chunks[index].SHA256 != "" && !strings.EqualFold(chunks[index].SHA256, hex.EncodeToString(chunkHash.Sum(nil))) {
			merged.Close()
			auditReason = "checksum_mismatch"
			serviceReason = "checksum_mismatch"
			writeError(w, http.StatusBadRequest, "上传分片校验值不匹配")
			return
		}
		mergedSize += written
	}
	if err := merged.Sync(); err != nil {
		merged.Close()
		writeError(w, http.StatusInternalServerError, "保存合并文件失败")
		return
	}
	if err := merged.Close(); err != nil || mergedSize != task.Size {
		writeError(w, http.StatusBadRequest, "上传分片不完整")
		return
	}
	shaHex := hex.EncodeToString(sha.Sum(nil))
	md5Hex := hex.EncodeToString(md5Hash.Sum(nil))
	hashSummary = shaHex[:12]
	if (input.SHA256 != "" && !strings.EqualFold(input.SHA256, shaHex)) || (input.MD5 != "" && !strings.EqualFold(input.MD5, md5Hex)) {
		auditReason = "checksum_mismatch"
		serviceReason = "checksum_mismatch"
		writeError(w, http.StatusBadRequest, "文件校验值不匹配")
		return
	}
	storedName, err := sanitizeName(task.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "文件名无效")
		return
	}
	relativeDir := task.StorageDir
	if relativeDir == "" {
		relativeDir = filepath.Join("files", strconv.FormatInt(user.ID, 10))
	}
	finalPath := ""
	cleanupFinal := true
	defer func() {
		if cleanupFinal && finalPath != "" {
			_ = os.Remove(finalPath)
		}
		_ = os.RemoveAll(tmpDir)
	}()
	fileRecord := store.File{UserID: user.ID, Name: task.Name, StoredName: storedName, Size: task.Size, Mime: task.Mime, SHA256: shaHex, MD5: md5Hex, StoragePath: relativeDir}
	completed, err := s.store.CompleteUploadWithPlacement(r.Context(), task, fileRecord, func(storagePath string, replace bool) error {
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
		// 覆盖上传直接原子 rename 替换旧文件（POSIX/Windows 均替换存在目标），
		// 旧物理文件由替换本身清除，不存在"先删旧文件再放新文件"的竞态窗口（G5）。
		// Overwrite uploads rename over the old file atomically (both platforms replace an
		// existing target), so the old file is cleared by the replacement itself and no
		// delete-then-place race window exists (G5).
		return os.Rename(mergedPath, finalPath)
	})
	if err != nil {
		log.Printf("complete upload: %v", err)
		auditReason = "save_failed"
		serviceReason = "save_failed"
		writeError(w, http.StatusInternalServerError, "保存文件记录失败")
		return
	}
	if err := s.store.DeleteChunks(r.Context(), task.ID); err != nil {
		log.Printf("delete uploaded chunks: %v", err)
	}
	cleanupFinal = false
	auditResult, auditReason = "success", ""
	serviceResult, serviceReason = "success", ""
	writeData(w, http.StatusOK, "上传完成", publicFile(completed))
}

func (s *Server) listFiles(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	page, pageSize := pagination(r)
	dir := ""
	if raw := strings.TrimSpace(r.URL.Query().Get("dir")); raw != "" {
		validated, err := validateUploadDir(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "目录无效")
			return
		}
		dir = validated
	}
	files, total, err := s.store.ListFiles(r.Context(), user.ID, user.Role == "admin", strings.TrimSpace(r.URL.Query().Get("keyword")), dir, page, pageSize)
	if err != nil {
		log.Printf("list files: %v", err)
		writeError(w, http.StatusInternalServerError, "获取文件列表失败")
		return
	}
	items := make([]map[string]any, 0, len(files))
	for _, file := range files {
		items = append(items, publicFile(file))
	}
	s.serviceEvent(r, "file_list", user.Username, "result=success page=%d page_size=%d total=%d dir=%s", page, pageSize, total, dir)
	writeData(w, http.StatusOK, "获取成功", map[string]any{"items": items, "page": page, "pageSize": pageSize, "total": total, "dir": dir})
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	// download 校验文件归属后交给 ServeContent 处理 Range 请求，并记录成功或失败审计结果。
	// download verifies ownership, delegates Range handling to ServeContent, and records the audit outcome.
	user := currentUser(r.Context())
	target := r.PathValue("id")
	auditResult, auditReason := "failure", "not_found"
	serviceResult, serviceReason := "failure", "not_found"
	defer func() {
		s.serviceEvent(r, "download", user.Username, "name=%s result=%s reason=%s", target, serviceResult, serviceReason)
	}()
	defer func() { s.recordAudit(r, &user.ID, user.Username, "download", target, auditResult, auditReason) }()
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "文件编号无效")
		return
	}
	file, err := s.store.FindFile(r.Context(), id)
	if err == nil {
		target = file.Name
	}
	if errors.Is(err, store.ErrNotFound) || file.Status != "ready" || (file.UserID != user.ID && user.Role != "admin") {
		auditReason = "not_found"
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}
	path := filepath.Join(s.config.DataDir, file.StoragePath)
	handle, err := os.Open(path)
	if err != nil {
		auditReason = "content_not_found"
		writeError(w, http.StatusNotFound, "文件内容不存在")
		return
	}
	defer handle.Close()
	w.Header().Set("Content-Type", file.Mime)
	w.Header().Set("Content-Disposition", contentDisposition(file.Name))
	auditResult, auditReason = "success", ""
	serviceResult, serviceReason = "success", ""
	http.ServeContent(w, r, file.Name, parseTime(file.CreatedAt), handle)
}

// batchDownload 将多个文件打包为 zip 一次性下载（仅限本人文件，管理员可含他人）。
// batchDownload zips multiple files into one download (owner-only, admins may include others).
func (s *Server) batchDownload(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	var input struct {
		IDs []int64 `json:"ids"`
	}
	if !decodeJSON(w, r, &input) || len(input.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "请选择要下载的文件")
		return
	}
	// 限制批量操作数量，避免单次请求消耗过多资源。
	// Cap batch size to avoid excessive resource use from a single request.
	if len(input.IDs) > batchDownloadMaxFiles {
		writeError(w, http.StatusBadRequest, "批量操作数量超出上限（最多 500 个）")
		return
	}
	// 去重并限定归属；任一文件不存在或无权访问则整体拒绝（避免部分下载误导）。
	// Deduplicate and enforce ownership; reject the whole batch if any file is missing or forbidden.
	seen := make(map[int64]bool)
	files := make([]store.File, 0, len(input.IDs))
	for _, id := range input.IDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		file, err := s.store.FindFile(r.Context(), id)
		if err != nil || file.Status != "ready" || (file.UserID != user.ID && user.Role != "admin") {
			writeError(w, http.StatusNotFound, "文件不存在")
			return
		}
		files = append(files, file)
	}
	var totalBytes int64
	for _, file := range files {
		if file.Size < 0 || file.Size > batchDownloadMaxBytes-totalBytes {
			writeErrorData(w, http.StatusRequestEntityTooLarge, "批量下载文件总大小超过 2 GiB 上限", map[string]string{"code": "BATCH_TOO_LARGE"})
			return
		}
		totalBytes += file.Size
	}
	if hasSpace, diskErr := s.batchDownloadDiskAvailable(totalBytes); diskErr != nil {
		log.Printf("check batch download disk usage: %v", diskErr)
		writeError(w, http.StatusInternalServerError, "无法检查系统存储空间")
		return
	} else if !hasSpace {
		writeErrorData(w, http.StatusServiceUnavailable, "系统存储空间不足，暂时禁止批量下载", map[string]string{"code": "DISK_FULL"})
		return
	}
	// Build the archive before writing headers so the client receives a reliable Content-Length.
	// 先生成临时 ZIP 再写响应头，确保客户端能拿到可靠的 Content-Length。
	temp, err := createBatchTempFile(filepath.Join(s.config.DataDir, "tmp"), "batch-download-*.zip")
	if err != nil {
		batchZipError(w, "创建下载文件失败", err)
		return
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	zw := zip.NewWriter(temp)
	// zip 内同名文件追加序号（不同目录的同名文件避免互相覆盖）。
	// Same-name files inside the zip get a numeric suffix so they do not overwrite each other.
	entryNames := make(map[string]struct{})
	for _, file := range files {
		path := filepath.Join(s.config.DataDir, file.StoragePath)
		handle, err := os.Open(path)
		if err != nil {
			zw.Close()
			temp.Close()
			writeError(w, http.StatusNotFound, "文件内容不存在")
			return
		}
		entryName := uniqueZipEntryName(file.Name, entryNames)
		entry, err := zw.Create(entryName)
		if err != nil {
			handle.Close()
			zw.Close()
			temp.Close()
			batchZipError(w, "创建下载文件失败", err)
			return
		}
		_, copyErr := io.Copy(entry, handle)
		handle.Close()
		if copyErr != nil {
			zw.Close()
			temp.Close()
			batchZipError(w, "读取文件内容失败", copyErr)
			return
		}
	}
	if err := zw.Close(); err != nil {
		temp.Close()
		batchZipError(w, "创建下载文件失败", err)
		return
	}
	if err := temp.Close(); err != nil {
		batchZipError(w, "创建下载文件失败", err)
		return
	}
	info, err := os.Stat(tempPath)
	if err != nil {
		batchZipError(w, "读取下载文件失败", err)
		return
	}
	archive, err := os.Open(tempPath)
	if err != nil {
		batchZipError(w, "读取下载文件失败", err)
		return
	}
	defer archive.Close()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", contentDisposition(batchDownloadFilename()))
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	if _, err := io.Copy(w, archive); err != nil {
		log.Printf("stream batch download archive: %v", err)
		return
	}
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.Name)
	}
	s.recordAudit(r, &user.ID, user.Username, "download", strings.Join(names, ","), "success", "batch")
	s.serviceEvent(r, "download", user.Username, "name=%s count=%d result=success reason=batch", strings.Join(names, ","), len(names))
}

const batchZipErrorCode = "ZIP_CREATE_FAILED"

// batchZipError returns a safe, actionable archive error while logging the
// original error for operators. Never expose PathError.Error() to clients.
// batchZipError 向用户返回可操作的归档错误，同时仅在日志中保留原始错误，避免泄露服务器路径。
func batchZipError(w http.ResponseWriter, action string, err error) {
	reason, message := classifyBatchZipError(err)
	log.Printf("batch zip %s (%s): %v", action, reason, err)
	writeErrorData(w, http.StatusInternalServerError, action+"："+message, map[string]string{
		"code":   batchZipErrorCode,
		"reason": reason,
	})
}

func classifyBatchZipError(err error) (string, string) {
	switch {
	case errors.Is(err, syscall.ENOSPC), errors.Is(err, syscall.EDQUOT):
		return "disk_full", "磁盘空间不足"
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM), errors.Is(err, syscall.EROFS):
		return "permission_denied", "目录无写入权限"
	default:
		return "io_error", "IO 错误"
	}
}

func batchDownloadFilename() string {
	return "filebox-batch-" + time.Now().Format("20060102-150405") + ".zip"
}

// batchDownloadDiskAvailable 检查临时 ZIP 占用后是否仍保留配置的最小可用空间。
// batchDownloadDiskAvailable checks that the raw batch size can be written while preserving the configured free-space floor.
func (s *Server) batchDownloadDiskAvailable(totalBytes int64) (bool, error) {
	_, free, _, err := diskUsageFunc(s.config.DataDir)
	if err != nil {
		return false, err
	}
	if free < totalBytes {
		return false, nil
	}
	return free-totalBytes >= s.config.MinFreeSpace, nil
}

// batchShare creates one independent share link per selected file after validating the whole batch.
// batchShare 先校验整批文件，再为每个文件创建一个独立分享链接，避免产生部分越权结果。
func (s *Server) batchShare(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	if s.rejectReadOnly(w, r, user, "batch_share", "batch") {
		return
	}
	result, reason := "failure", "invalid_request"
	target := "batch"
	defer func() {
		s.recordAudit(r, &user.ID, user.Username, "batch_share", target, result, reason)
		s.serviceEvent(r, "batch_share", user.Username, "target=%s result=%s reason=%s", target, result, reason)
	}()
	createdTokens := make([]string, 0)
	defer func() {
		if result == "success" {
			return
		}
		for _, token := range createdTokens {
			if err := s.store.DeleteShareByToken(r.Context(), token, user.ID); err != nil {
				log.Printf("rollback batch share %s: %v", token, err)
			}
		}
	}()
	var input struct {
		FileIDs        []int64 `json:"fileIds"`
		ExpiresInHours int     `json:"expiresInHours"`
		MaxDownloads   int     `json:"maxDownloads"`
	}
	if !decodeJSON(w, r, &input) || len(input.FileIDs) == 0 {
		writeError(w, http.StatusBadRequest, "请选择要分享的文件")
		return
	}
	// 限制批量操作数量，避免单次请求消耗过多资源。
	// Cap batch size to avoid excessive resource use from a single request.
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

	// Deduplicate while preserving request order; reject the entire batch on any forbidden file.
	// 去重并保留请求顺序；任一文件无权访问时整体拒绝。
	seen := make(map[int64]struct{}, len(input.FileIDs))
	files := make([]store.File, 0, len(input.FileIDs))
	fileIDs := make([]string, 0, len(input.FileIDs))
	for _, id := range input.FileIDs {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		file, err := s.store.FindFile(r.Context(), id)
		if err != nil || file.Status != "ready" || (file.UserID != user.ID && user.Role != "admin") {
			writeError(w, http.StatusNotFound, "文件不存在")
			return
		}
		files = append(files, file)
		fileIDs = append(fileIDs, strconv.FormatInt(id, 10))
	}

	target = strings.Join(fileIDs, ",")
	reason = "create_failed"

	expiresAt := time.Now().UTC().Add(time.Duration(input.ExpiresInHours) * time.Hour)
	items := make([]map[string]any, 0, len(files))
	for _, file := range files {
		var share store.Share
		created := false
		for attempt := 0; attempt < 5; attempt++ {
			token, err := randomShareToken()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "创建分享链接失败")
				return
			}
			err = s.store.CreateShare(r.Context(), file.ID, user.ID, token, expiresAt, input.MaxDownloads)
			if errors.Is(err, store.ErrConflict) {
				continue
			}
			if err != nil {
				log.Printf("create batch share: %v", err)
				writeError(w, http.StatusInternalServerError, "创建分享链接失败")
				return
			}
			createdTokens = append(createdTokens, token)
			share, err = s.store.GetShareByToken(r.Context(), token)
			if err != nil {
				log.Printf("load batch share: %v", err)
				writeError(w, http.StatusInternalServerError, "创建分享链接失败")
				return
			}
			created = true
			break
		}
		if !created {
			writeError(w, http.StatusInternalServerError, "创建分享链接失败")
			return
		}
		items = append(items, map[string]any{
			"fileId": share.FileID, "fileName": file.Name, "token": share.Token, "url": "/" + share.Token,
			"expiresAt": share.ExpiresAt, "maxDownloads": share.MaxDownloads,
		})
	}
	result, reason = "success", "batch"
	writeData(w, http.StatusCreated, "批量分享链接已创建", map[string]any{"items": items})
}

// batchDelete 以单事务删除选中文件，提交后再清理物理文件内容。
// batchDelete deletes selected files atomically and removes their physical content after commit.
func (s *Server) batchDelete(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	if s.rejectReadOnly(w, r, user, "delete", "batch") {
		return
	}
	var input struct {
		IDs       []int64 `json:"ids"`
		FolderIDs []int64 `json:"folder_ids"`
		Force     bool    `json:"force"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if len(input.IDs) == 0 && len(input.FolderIDs) == 0 {
		writeErrorData(w, http.StatusBadRequest, "请至少选择一个文件", map[string]string{"code": "BATCH_DELETE_EMPTY"})
		return
	}
	// 限制批量操作数量，避免单次请求消耗过多资源。
	// Cap batch size to avoid excessive resource use from a single request.
	if len(input.IDs)+len(input.FolderIDs) > 500 {
		writeErrorData(w, http.StatusBadRequest, "批量操作数量超出上限（最多 500 个）", map[string]string{"code": "BATCH_LIMIT_EXCEEDED"})
		return
	}
	for _, id := range input.IDs {
		if id < 1 {
			writeErrorData(w, http.StatusBadRequest, "文件编号无效", map[string]string{"code": "INVALID_FILE_ID"})
			return
		}
	}
	for _, id := range input.FolderIDs {
		if id < 1 {
			writeErrorData(w, http.StatusBadRequest, "文件编号无效", map[string]string{"code": "INVALID_FILE_ID"})
			return
		}
	}
	targetIDs := make([]string, 0, len(input.IDs)+len(input.FolderIDs))
	seen := make(map[int64]struct{}, len(input.IDs))
	for _, id := range input.IDs {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		targetIDs = append(targetIDs, strconv.FormatInt(id, 10))
	}
	seenFolders := make(map[int64]struct{}, len(input.FolderIDs))
	for _, id := range input.FolderIDs {
		if _, exists := seenFolders[id]; exists {
			continue
		}
		seenFolders[id] = struct{}{}
		targetIDs = append(targetIDs, "d"+strconv.FormatInt(id, 10))
	}
	target := strings.Join(targetIDs, ",")
	auditResult, auditReason := "failure", "batch"
	defer func() {
		s.serviceEvent(r, "delete", user.Username, "target=%s result=%s reason=batch", target, auditResult)
		s.recordAudit(r, &user.ID, user.Username, "delete", target, auditResult, auditReason)
	}()
	if len(input.FolderIDs) > 0 {
		nonEmpty, err := s.store.CheckFoldersDeletable(r.Context(), user.ID, input.FolderIDs)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "目录不存在")
			return
		}
		if err != nil {
			log.Printf("check batch-delete folders: %v", err)
			writeError(w, http.StatusInternalServerError, "检查目录失败")
			return
		}
		if len(nonEmpty) > 0 && !input.Force {
			names := make([]string, 0, len(nonEmpty))
			for _, folder := range nonEmpty {
				names = append(names, folder.Name)
			}
			writeErrorData(w, http.StatusBadRequest, "以下目录非空，无法删除："+strings.Join(names, "、")+"（请先清空目录内容）", map[string]string{"code": "FOLDER_NOT_EMPTY"})
			return
		}
	}
	if len(input.FolderIDs) > 0 && input.Force {
		treeFileIDs := make([]int64, 0)
		treeFileSeen := make(map[int64]struct{})
		treeFolderIDs := make([]int64, 0)
		treeFolderSeen := make(map[int64]struct{})
		rootFolders := make([]store.Folder, 0, len(input.FolderIDs))
		rootSeen := make(map[int64]struct{}, len(input.FolderIDs))

		for _, id := range input.FolderIDs {
			root, err := s.store.GetFolderByID(r.Context(), id, user.ID)
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "目录不存在")
				return
			}
			if err != nil {
				log.Printf("load batch-delete root folder %d: %v", id, err)
				writeError(w, http.StatusInternalServerError, "删除目录失败")
				return
			}
			if _, exists := rootSeen[root.ID]; !exists {
				rootSeen[root.ID] = struct{}{}
				rootFolders = append(rootFolders, root)
			}
			_, treeFiles, treeFolders, err := s.store.ListFolderTree(r.Context(), user.ID, id)
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "目录不存在")
				return
			}
			if err != nil {
				log.Printf("list batch-delete folder tree %d: %v", id, err)
				writeError(w, http.StatusInternalServerError, "检查目录失败")
				return
			}
			for _, file := range treeFiles {
				if _, exists := treeFileSeen[file.ID]; exists {
					continue
				}
				treeFileSeen[file.ID] = struct{}{}
				treeFileIDs = append(treeFileIDs, file.ID)
			}
			for _, folder := range treeFolders {
				if _, exists := treeFolderSeen[folder.ID]; exists {
					continue
				}
				treeFolderSeen[folder.ID] = struct{}{}
				treeFolderIDs = append(treeFolderIDs, folder.ID)
			}
		}

		standaloneIDs := make([]int64, 0, len(input.IDs))
		standaloneSeen := make(map[int64]struct{}, len(input.IDs))
		for _, id := range input.IDs {
			if _, inTree := treeFileSeen[id]; inTree {
				continue
			}
			if _, exists := standaloneSeen[id]; exists {
				continue
			}
			standaloneSeen[id] = struct{}{}
			standaloneIDs = append(standaloneIDs, id)
		}
		paths := make([]string, 0)
		if len(standaloneIDs) > 0 {
			filePaths, err := s.store.BatchDeleteFiles(r.Context(), standaloneIDs, user.ID, user.Role == "admin")
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "文件不存在")
				return
			}
			if err != nil {
				log.Printf("batch delete files: %v", err)
				writeError(w, http.StatusInternalServerError, "删除文件失败")
				return
			}
			paths = append(paths, filePaths...)
		}
		treePaths, err := s.store.DeleteFolderTree(r.Context(), treeFileIDs, treeFolderIDs, user.ID)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "文件或目录不存在")
			return
		}
		if err != nil {
			log.Printf("delete folder tree: %v", err)
			writeError(w, http.StatusInternalServerError, "删除目录失败")
			return
		}
		paths = append(paths, treePaths...)
		for _, path := range paths {
			if err := os.Remove(filepath.Join(s.config.DataDir, path)); err != nil && !errors.Is(err, os.ErrNotExist) {
				log.Printf("remove batch-deleted file content: %v", err)
			}
		}
		for _, folder := range rootFolders {
			diskPath := filepath.Join(s.config.DataDir, "files", strconv.FormatInt(user.ID, 10), filepath.FromSlash(folder.Path))
			if err := os.RemoveAll(diskPath); err != nil {
				log.Printf("remove batch-deleted folder content: %v", err)
			}
			s.serviceEvent(r, "folder_delete", user.Username, "target=%s result=success", folder.Path)
			s.recordAudit(r, &user.ID, user.Username, "folder_delete", folder.Path, "success", "folder_delete")
		}
		auditResult = "success"
		writeData(w, http.StatusOK, "已删除", map[string]any{"deleted": len(paths), "foldersDeleted": len(treeFolderIDs)})
		return
	}
	paths := make([]string, 0)
	if len(input.IDs) > 0 {
		var err error
		paths, err = s.store.BatchDeleteFiles(r.Context(), input.IDs, user.ID, user.Role == "admin")
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "文件不存在")
			return
		}
		if err != nil {
			log.Printf("batch delete files: %v", err)
			writeError(w, http.StatusInternalServerError, "删除文件失败")
			return
		}
	}
	for _, path := range paths {
		if err := os.Remove(filepath.Join(s.config.DataDir, path)); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("remove batch-deleted file content: %v", err)
		}
	}
	foldersDeleted := 0
	for _, id := range input.FolderIDs {
		folder, err := s.store.GetFolderByID(r.Context(), id, user.ID)
		if err != nil {
			log.Printf("load batch-deleted folder %d: %v", id, err)
			continue
		}
		deleted, err := s.store.DeleteFolder(r.Context(), id, user.ID)
		if err != nil {
			log.Printf("delete batch folder %d: %v", id, err)
			continue
		}
		if !deleted {
			continue
		}
		foldersDeleted++
		s.serviceEvent(r, "folder_delete", user.Username, "target=%s result=success", folder.Path)
		s.recordAudit(r, &user.ID, user.Username, "folder_delete", folder.Path, "success", "folder_delete")
	}
	auditResult = "success"
	writeData(w, http.StatusOK, "已删除", map[string]any{"deleted": len(paths), "foldersDeleted": foldersDeleted})
}

func uniqueZipEntryName(name string, used map[string]struct{}) string {
	key := strings.ToLower(name)
	if _, exists := used[key]; !exists {
		used[key] = struct{}{}
		return name
	}
	extension := filepath.Ext(name)
	stem := strings.TrimSuffix(name, extension)
	for suffix := 1; ; suffix++ {
		candidate := fmt.Sprintf("%s (%d)%s", stem, suffix, extension)
		key = strings.ToLower(candidate)
		if _, exists := used[key]; exists {
			continue
		}
		used[key] = struct{}{}
		return candidate
	}
}

// createShare validates ownership and creates a random, time-limited sharing link.
// createShare 校验文件归属并创建随机、限时的分享链接。
func (s *Server) createShare(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	if s.rejectReadOnly(w, r, user, "share", r.PathValue("id")) {
		return
	}
	target := r.PathValue("id")
	result, reason := "failure", "not_found"
	defer func() {
		s.serviceEvent(r, "share_create", user.Username, "name=%s result=%s reason=%s", target, result, reason)
		s.recordAudit(r, &user.ID, user.Username, "share", target, result, reason)
	}()
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}
	file, err := s.store.FindFile(r.Context(), id)
	if err != nil || file.Status != "ready" || (file.UserID != user.ID && user.Role != "admin") {
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}
	target = file.Name
	var input shareRequest
	if !decodeJSON(w, r, &input) {
		reason = "invalid_request"
		return
	}
	// 分享有效期上限与收集箱一致为 10 年，避免转换为 time.Duration 时溢出。
	// The 10-year cap matches collection expiry and prevents time.Duration conversion overflow.
	if input.ExpiresInHours < 1 || input.ExpiresInHours > 87600 {
		reason = "invalid_expiry"
		writeError(w, http.StatusBadRequest, "分享有效期无效")
		return
	}
	if input.MaxDownloads < 0 || input.MaxDownloads > 100000 {
		reason = "invalid_limit"
		writeError(w, http.StatusBadRequest, "分享次数限制无效")
		return
	}
	expiresAt := time.Now().UTC().Add(time.Duration(input.ExpiresInHours) * time.Hour)
	var share store.Share
	for attempt := 0; attempt < 5; attempt++ {
		token, tokenErr := randomShareToken()
		if tokenErr != nil {
			reason = "token_failed"
			writeError(w, http.StatusInternalServerError, "创建分享链接失败")
			return
		}
		if err := s.store.CreateShare(r.Context(), file.ID, user.ID, token, expiresAt, input.MaxDownloads); errors.Is(err, store.ErrConflict) {
			continue
		} else if err != nil {
			reason = "create_failed"
			writeError(w, http.StatusInternalServerError, "创建分享链接失败")
			return
		}
		share, err = s.store.GetShareByToken(r.Context(), token)
		if err != nil {
			reason = "load_failed"
			writeError(w, http.StatusInternalServerError, "创建分享链接失败")
			return
		}
		result, reason = "success", ""
		writeData(w, http.StatusCreated, "分享链接已创建", map[string]any{
			"id": share.ID, "token": share.Token, "url": "/" + share.Token,
			"expiresAt": share.ExpiresAt, "maxDownloads": share.MaxDownloads, "downloadCount": share.DownloadCount,
			"fileName": file.Name, "fileSize": file.Size,
		})
		return
	}
	reason = "token_conflict"
	writeError(w, http.StatusInternalServerError, "创建分享链接失败")
}

// shareStatus reports the owner-facing state without changing the persisted share record.
// shareStatus 返回管理端状态，不修改持久化的分享记录。
func shareStatus(share store.Share) (string, int64) {
	if share.RevokedAt != "" {
		return "revoked", 0
	}
	deadline, err := time.Parse(time.RFC3339, share.ExpiresAt)
	if err != nil || !time.Now().UTC().Before(deadline) {
		return "expired", 0
	}
	if share.MaxDownloads > 0 && share.DownloadCount >= int64(share.MaxDownloads) {
		return "limit_reached", int64(time.Until(deadline).Seconds())
	}
	return "active", int64(time.Until(deadline).Seconds())
}

func managedShareData(share store.Share) map[string]any {
	status, remaining := shareStatus(share)
	return map[string]any{
		"id": share.ID, "fileId": share.FileID, "fileName": share.FileName, "token": share.Token, "url": "/" + share.Token,
		"createdBy": share.CreatedBy, "expiresAt": share.ExpiresAt, "downloadCount": share.DownloadCount,
		"maxDownloads": share.MaxDownloads, "revokedAt": share.RevokedAt, "createdAt": share.CreatedAt,
		"status": status, "remainingSeconds": remaining,
	}
}

// listShares lists every share created by the current user for the management screen.
// listShares 列出当前用户创建的全部分享，供管理页面使用。
func (s *Server) listShares(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	shares, err := s.store.ListSharesByOwner(r.Context(), user.ID)
	if err != nil {
		log.Printf("list shares: %v", err)
		writeError(w, http.StatusInternalServerError, "获取分享列表失败")
		return
	}
	items := make([]map[string]any, 0, len(shares))
	for _, share := range shares {
		items = append(items, managedShareData(share))
	}
	page, pageSize := pagination(r)
	total := len(items)
	start := int(int64(page-1) * int64(pageSize))
	if start >= total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	items = items[start:end]
	writeData(w, http.StatusOK, "获取成功", map[string]any{"items": items, "page": page, "pageSize": pageSize, "total": total})
}

// listFileShares lists links attached to one owned file and keeps cross-user access at 404.
// listFileShares 列出单个归属文件的分享，并将跨用户访问统一隐藏为 404。
func (s *Server) listFileShares(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}
	file, err := s.store.FindFile(r.Context(), id)
	if err != nil || file.Status != "ready" || (file.UserID != user.ID && user.Role != "admin") {
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}
	shares, err := s.store.ListSharesByFile(r.Context(), id)
	if err != nil {
		log.Printf("list file shares: %v", err)
		writeError(w, http.StatusInternalServerError, "获取分享列表失败")
		return
	}
	items := make([]map[string]any, 0, len(shares))
	for _, share := range shares {
		share.FileName = file.Name
		items = append(items, managedShareData(share))
	}
	writeData(w, http.StatusOK, "获取成功", map[string]any{"items": items})
}

func (s *Server) managedShare(r *http.Request) (store.Share, store.User, error) {
	user := currentUser(r.Context())
	share, err := s.store.GetShareByTokenIncludingRevoked(r.Context(), r.PathValue("token"))
	if err != nil || (share.CreatedBy != user.ID && user.Role != "admin") {
		return store.Share{}, user, store.ErrNotFound
	}
	return share, user, nil
}

// getManagedShare returns owner/admin-only usage details for one share.
// getManagedShare 返回仅创建者/管理员可见的单条分享使用情况。
func (s *Server) getManagedShare(w http.ResponseWriter, r *http.Request) {
	share, _, err := s.managedShare(r)
	if err != nil {
		writeError(w, http.StatusNotFound, "分享不存在")
		return
	}
	file, err := s.store.FindFile(r.Context(), share.FileID)
	if err != nil || file.Status != "ready" {
		writeError(w, http.StatusNotFound, "分享不存在")
		return
	}
	share.FileName = file.Name
	writeData(w, http.StatusOK, "获取成功", managedShareData(share))
}

// shareLogs returns only download events belonging to one owner/admin-visible share.
// shareLogs 仅返回归属该分享的下载事件，创建者和管理员可见。
func (s *Server) shareLogs(w http.ResponseWriter, r *http.Request) {
	share, _, err := s.managedShare(r)
	if err != nil {
		writeError(w, http.StatusNotFound, "分享不存在")
		return
	}
	page, pageSize := pagination(r)
	logs, total, err := s.store.ListShareAuditLogs(r.Context(), share.Token, share.CreatedBy, page, pageSize)
	if err != nil {
		log.Printf("list share logs: %v", err)
		writeError(w, http.StatusInternalServerError, "获取分享日志失败")
		return
	}
	writeData(w, http.StatusOK, "获取成功", map[string]any{"items": logs, "page": page, "pageSize": pageSize, "total": total})
}

// extendShare replaces the expiry with now+hours, while the store preserves a later old deadline.
// extendShare 将有效期设置为当前时间加小时数，存储层会保留更晚的原截止时间。
func (s *Server) extendShare(w http.ResponseWriter, r *http.Request) {
	share, user, err := s.managedShare(r)
	if err != nil {
		writeError(w, http.StatusNotFound, "分享不存在")
		return
	}
	if s.rejectReadOnly(w, r, user, "share", share.Token) {
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
	if err := s.store.UpdateShareExpiry(r.Context(), share.Token, time.Now().UTC().Add(time.Duration(input.ExpiresInHours)*time.Hour), share.CreatedBy); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "分享不存在")
		} else {
			log.Printf("extend share: %v", err)
			writeError(w, http.StatusInternalServerError, "延期分享失败")
		}
		return
	}
	updated, _ := s.store.GetShareByTokenIncludingRevoked(r.Context(), share.Token)
	if file, fileErr := s.store.FindFile(r.Context(), share.FileID); fileErr == nil {
		updated.FileName = file.Name
	}
	s.recordAudit(r, &user.ID, user.Username, "share_extend", share.Token, "success", "extend")
	writeData(w, http.StatusOK, "分享有效期已更新", managedShareData(updated))
}

// increaseShare raises the download allowance, including the finite-to-unlimited transition.
// increaseShare 提高下载次数上限，并支持从有限次数提升为不限次数。
func (s *Server) increaseShare(w http.ResponseWriter, r *http.Request) {
	share, user, err := s.managedShare(r)
	if err != nil {
		writeError(w, http.StatusNotFound, "分享不存在")
		return
	}
	if s.rejectReadOnly(w, r, user, "share", share.Token) {
		return
	}
	var input shareIncreaseRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.MaxDownloads < 0 || input.MaxDownloads > 100000 || (input.MaxDownloads != 0 && input.MaxDownloads <= share.MaxDownloads) {
		writeError(w, http.StatusBadRequest, "分享次数限制无效")
		return
	}
	if err := s.store.UpdateShareMaxDownloads(r.Context(), share.Token, input.MaxDownloads, share.CreatedBy); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "分享不存在")
		} else if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusBadRequest, "分享次数限制无效")
		} else {
			log.Printf("increase share: %v", err)
			writeError(w, http.StatusInternalServerError, "增加分享次数失败")
		}
		return
	}
	updated, _ := s.store.GetShareByTokenIncludingRevoked(r.Context(), share.Token)
	if file, fileErr := s.store.FindFile(r.Context(), share.FileID); fileErr == nil {
		updated.FileName = file.Name
	}
	s.recordAudit(r, &user.ID, user.Username, "share_increase", share.Token, "success", "increase")
	writeData(w, http.StatusOK, "分享次数已更新", managedShareData(updated))
}

// deleteShare soft-revokes one share while retaining it for later audit classification.
// deleteShare 软撤销单条分享，并保留记录以便后续审计分类。
func (s *Server) deleteShare(w http.ResponseWriter, r *http.Request) {
	share, user, err := s.managedShare(r)
	if err != nil {
		writeError(w, http.StatusNotFound, "分享不存在")
		return
	}
	if s.rejectReadOnly(w, r, user, "share", share.Token) {
		return
	}
	if err := s.store.DeleteShareByToken(r.Context(), share.Token, share.CreatedBy); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "分享不存在")
		} else {
			log.Printf("revoke share: %v", err)
			writeError(w, http.StatusInternalServerError, "撤销分享失败")
		}
		return
	}
	s.recordAudit(r, &user.ID, user.Username, "share_revoke", share.Token, "success", "revoke")
	writeData(w, http.StatusOK, "分享已撤销", map[string]any{"token": share.Token})
}

// shareMeta exposes only public file and sharing metadata for anonymous viewers.
// shareMeta 向匿名访问者仅公开文件和分享元数据。
func (s *Server) shareMeta(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	target := token
	var shareOwnerID *int64
	result, reason := "failure", "share_not_found"
	defer func() {
		s.serviceEvent(r, "share_view", "anonymous", "target=%s result=%s reason=%s", target, result, reason)
		s.recordShareAudit(r, nil, shareOwnerID, "anonymous", "share_view", target, result, reason)
	}()
	if !s.rateLimiter.allowPublicRequest(s.requestIP(r), 30, 10) {
		result, reason = "failure", "rate_limited"
		writeError(w, http.StatusTooManyRequests, "请求过于频繁，请稍后重试")
		return
	}
	share, err := s.store.GetShareByToken(r.Context(), token)
	if err != nil {
		if revoked, revokedErr := s.store.GetShareByTokenIncludingRevoked(r.Context(), token); revokedErr == nil && revoked.RevokedAt != "" {
			shareOwnerID = &revoked.CreatedBy
			reason = "share_revoked"
		}
		writeError(w, http.StatusNotFound, "分享不存在")
		return
	}
	shareOwnerID = &share.CreatedBy
	file, err := s.store.FindFile(r.Context(), share.FileID)
	if err != nil || file.Status != "ready" {
		reason = "share_denied"
		writeError(w, http.StatusNotFound, "分享不存在")
		return
	}
	if !shareActive(share.ExpiresAt) {
		reason = "share_expired"
		writeError(w, http.StatusNotFound, "分享链接已过期")
		return
	}
	owner, err := s.store.GetUser(share.CreatedBy)
	if err != nil {
		writeError(w, http.StatusNotFound, "分享不存在")
		return
	}
	result, reason = "success", ""
	writeData(w, http.StatusOK, "获取成功", map[string]any{
		"fileName": file.Name, "fileSize": file.Size, "mime": effectiveFileMIME(file),
		"expiresAt": share.ExpiresAt, "maxDownloads": share.MaxDownloads, "downloadCount": share.DownloadCount,
		"downloadAvailable": share.MaxDownloads == 0 || share.DownloadCount < int64(share.MaxDownloads),
		"createdBy":         owner.Username,
	})
}

// authorizeShareContent atomically consumes a share slot and classifies a rejected request.
// authorizeShareContent 原子消耗分享次数，并对拒绝请求分类。
func (s *Server) authorizeShareContent(ctx context.Context, token string, maxDownloads int64, rangeMode bool) (string, error) {
	allowed, err := s.store.IncrementShareDownloads(ctx, token, maxDownloads, rangeMode)
	if err != nil {
		return "download_count_failed", err
	}
	if allowed {
		return "", nil
	}

	current, currentErr := s.store.GetShareByTokenIncludingRevoked(ctx, token)
	switch {
	case currentErr == nil && current.RevokedAt != "":
		return "share_revoked", nil
	case currentErr == nil && !shareActive(current.ExpiresAt):
		return "share_expired", nil
	case currentErr == nil && current.MaxDownloads > 0 && current.DownloadCount >= int64(current.MaxDownloads):
		return "share_limit", nil
	default:
		return "share_denied", nil
	}
}

func writeShareAuthorizationError(w http.ResponseWriter, reason string) {
	switch reason {
	case "share_limit":
		writeErrorData(w, http.StatusForbidden, "分享次数已用完", map[string]string{"code": "SHARE_DOWNLOAD_LIMIT"})
	case "share_expired":
		writeError(w, http.StatusForbidden, "分享链接已过期")
	case "share_revoked":
		writeError(w, http.StatusForbidden, "分享已撤销")
	default:
		writeError(w, http.StatusForbidden, "分享下载被拒绝")
	}
}

// shareDownload consumes a share slot before streaming the ready file with Range support.
// shareDownload 原子消耗分享次数后，以支持 Range 的方式流式输出 ready 文件。
func (s *Server) shareDownload(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	target := token
	var shareOwnerID *int64
	result, reason := "failure", "share_not_found"
	defer func() {
		s.serviceEvent(r, "share_download", "anonymous", "target=%s result=%s reason=%s", target, result, reason)
		s.recordShareAudit(r, nil, shareOwnerID, "anonymous", "share_download", target, result, reason)
	}()
	if !s.rateLimiter.allowPublicRequest(s.requestIP(r), 30, 10) {
		result, reason = "failure", "rate_limited"
		writeError(w, http.StatusTooManyRequests, "请求过于频繁，请稍后重试")
		return
	}
	share, err := s.store.GetShareByToken(r.Context(), token)
	if err != nil {
		if revoked, revokedErr := s.store.GetShareByTokenIncludingRevoked(r.Context(), token); revokedErr == nil && revoked.RevokedAt != "" {
			shareOwnerID = &revoked.CreatedBy
			reason = "share_revoked"
			writeError(w, http.StatusNotFound, "分享不存在")
			return
		}
		writeError(w, http.StatusNotFound, "分享不存在")
		return
	}
	shareOwnerID = &share.CreatedBy
	file, err := s.store.FindFile(r.Context(), share.FileID)
	if err != nil || file.Status != "ready" {
		reason = "share_denied"
		writeError(w, http.StatusNotFound, "分享不存在")
		return
	}
	if !shareActive(share.ExpiresAt) {
		reason = "share_expired"
		writeError(w, http.StatusForbidden, "分享链接已过期")
		return
	}
	// 先打开文件再扣次：物理内容缺失（404）不应消耗分享额度，避免"0 字节交付烧光次数"。
	// Open the file before consuming a download slot so a missing file (404) does not
	// burn share quota — otherwise a storage failure can exhaust maxDownloads with zero bytes served.
	handle, err := os.Open(filepath.Join(s.config.DataDir, file.StoragePath))
	if err != nil {
		reason = "content_not_found"
		writeError(w, http.StatusNotFound, "文件内容不存在")
		return
	}
	defer handle.Close()
	reason, err = s.authorizeShareContent(r.Context(), token, int64(share.MaxDownloads), r.Header.Get("Range") != "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "分享下载失败")
		return
	}
	if reason != "" {
		writeShareAuthorizationError(w, reason)
		return
	}
	w.Header().Set("Content-Type", effectiveFileMIME(file))
	w.Header().Set("Content-Disposition", contentDisposition(file.Name))
	result, reason = "success", ""
	http.ServeContent(w, r, file.Name, parseTime(file.CreatedAt), handle)
}

// sharePreview streams a shared file inline for preview while consuming a share slot.
// sharePreview 以 inline 方式输出分享文件供预览，同时消耗分享下载次数。
func (s *Server) sharePreview(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	target := token
	var shareOwnerID *int64
	result, reason := "failure", "share_not_found"
	defer func() {
		s.serviceEvent(r, "share_preview", "anonymous", "target=%s result=%s reason=%s", target, result, reason)
		s.recordShareAudit(r, nil, shareOwnerID, "anonymous", "share_preview", target, result, reason)
	}()
	// 与 shareMeta/shareDownload 一致：匿名预览同样按来源 IP 限速，防止持 token 无限拉取文件流。
	// Like shareMeta/shareDownload, anonymous preview is rate-limited per source IP so a token cannot stream files endlessly.
	if !s.rateLimiter.allowPublicRequest(s.requestIP(r), 30, 10) {
		result, reason = "failure", "rate_limited"
		writeError(w, http.StatusTooManyRequests, "请求过于频繁，请稍后重试")
		return
	}
	share, err := s.store.GetShareByToken(r.Context(), token)
	if err != nil {
		if revoked, revokedErr := s.store.GetShareByTokenIncludingRevoked(r.Context(), token); revokedErr == nil && revoked.RevokedAt != "" {
			shareOwnerID = &revoked.CreatedBy
			reason = "share_revoked"
			writeError(w, http.StatusNotFound, "分享不存在")
			return
		}
		writeError(w, http.StatusNotFound, "分享不存在")
		return
	}
	shareOwnerID = &share.CreatedBy
	file, err := s.store.FindFile(r.Context(), share.FileID)
	if err != nil || file.Status != "ready" {
		reason = "share_denied"
		writeError(w, http.StatusNotFound, "分享不存在")
		return
	}
	if !shareActive(share.ExpiresAt) {
		reason = "share_expired"
		writeError(w, http.StatusForbidden, "分享链接已过期")
		return
	}
	handle, err := os.Open(filepath.Join(s.config.DataDir, file.StoragePath))
	if err != nil {
		reason = "content_not_found"
		writeError(w, http.StatusNotFound, "文件内容不存在")
		return
	}
	defer handle.Close()
	reason, err = s.authorizeShareContent(r.Context(), token, int64(share.MaxDownloads), r.Header.Get("Range") != "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "分享下载失败")
		return
	}
	if reason != "" {
		writeShareAuthorizationError(w, reason)
		return
	}
	contentType := effectiveFileMIME(file)
	w.Header().Set("Content-Type", contentType)
	if previewMIMEAllowed(contentType) {
		w.Header().Set("Content-Disposition", "inline")
	} else {
		w.Header().Set("Content-Disposition", contentDisposition(file.Name))
	}
	var content io.ReadSeeker = handle
	if bound := previewContentLimit(contentType); bound > 0 {
		if file.Size > bound {
			w.Header().Set("X-Content-Length-Limit", strconv.FormatInt(bound, 10))
		}
		contentSize := file.Size
		if contentSize > bound {
			contentSize = bound
		}
		content = io.NewSectionReader(handle, 0, contentSize)
	}
	result, reason = "success", ""
	http.ServeContent(w, r, file.Name, parseTime(file.CreatedAt), content)
}

// deleteShares revokes all links for an owned file without exposing other users' files.
// deleteShares 撤销当前用户文件的全部分享，并对他人文件保持 404。
func (s *Server) deleteShares(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	if s.rejectReadOnly(w, r, user, "share", r.PathValue("id")) {
		return
	}
	target := r.PathValue("id")
	result, reason := "failure", "not_found"
	defer func() {
		s.serviceEvent(r, "share_revoke", user.Username, "name=%s result=%s reason=%s", target, result, reason)
		s.recordAudit(r, &user.ID, user.Username, "share", target, result, reason)
	}()
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}
	file, err := s.store.FindFile(r.Context(), id)
	if err != nil || file.Status != "ready" || (file.UserID != user.ID && user.Role != "admin") {
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}
	target = file.Name
	removed, err := s.store.DeleteSharesByFile(r.Context(), id)
	if err != nil {
		reason = "delete_failed"
		writeError(w, http.StatusInternalServerError, "撤销分享失败")
		return
	}
	result, reason = "success", ""
	writeData(w, http.StatusOK, "分享已撤销", map[string]any{"removed": removed})
}

func shareActive(expiresAt string) bool {
	// shareActive accepts a link only while its RFC3339 deadline is still in the future.
	// shareActive 仅在 RFC3339 有效期截止时间尚未到达时接受链接。
	deadline, err := time.Parse(time.RFC3339, expiresAt)
	return err == nil && time.Now().UTC().Before(deadline)
}

func randomShareToken() (string, error) {
	// randomShareToken uses crypto/rand to create a 64-character alphanumeric token.
	// randomShareToken 使用 crypto/rand 生成 64 位字母数字 token。
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, 64)
	for index := range result {
		value, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		result[index] = alphabet[value.Int64()]
	}
	return string(result), nil
}

// preview serves approved media inline and treats every other MIME type as a download.
// preview 对白名单 MIME inline 输出，其他类型按附件下载处理。
func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	target := r.PathValue("id")
	result, reason := "failure", "not_found"
	defer func() {
		s.serviceEvent(r, "download", user.Username, "name=%s result=%s reason=%s", target, result, reason)
		s.recordAudit(r, &user.ID, user.Username, "download", target, result, reason)
	}()
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "文件编号无效")
		return
	}
	file, err := s.store.FindFile(r.Context(), id)
	if err == nil {
		target = file.Name
	}
	if errors.Is(err, store.ErrNotFound) || err != nil || file.Status != "ready" || (file.UserID != user.ID && user.Role != "admin") {
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}
	handle, err := os.Open(filepath.Join(s.config.DataDir, file.StoragePath))
	if err != nil {
		reason = "content_not_found"
		writeError(w, http.StatusNotFound, "文件内容不存在")
		return
	}
	defer handle.Close()
	contentType := effectiveFileMIME(file)
	w.Header().Set("Content-Type", contentType)
	if previewMIMEAllowed(contentType) {
		w.Header().Set("Content-Disposition", "inline")
	} else {
		w.Header().Set("Content-Disposition", contentDisposition(file.Name))
	}
	result, reason = "success", ""
	http.ServeContent(w, r, file.Name, parseTime(file.CreatedAt), handle)
}

var previewMIMETypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true,
	"text/plain": true, "text/markdown": true, "text/csv": true, "text/x-log": true,
	"application/json": true, "application/pdf": true,
	"video/mp4": true, "video/webm": true,
}

func effectiveFileMIME(file store.File) string {
	// effectiveFileMIME falls back to the filename extension when metadata has no MIME.
	// effectiveFileMIME 在元数据 MIME 为空时按文件扩展名推断类型。
	contentType := strings.TrimSpace(file.Mime)
	if contentType == "" {
		contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(file.Name)))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return contentType
}

func previewMIMEAllowed(contentType string) bool {
	// previewMIMEAllowed compares the media type while preserving any original parameters in the header.
	// previewMIMEAllowed 比较媒体类型，同时保留响应头中的原始参数。
	base, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		base = strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])
	}
	return previewMIMETypes[strings.ToLower(base)]
}

func previewContentLimit(contentType string) int64 {
	// previewContentLimit bounds preview streams so they cannot become an unmetered full download.
	// previewContentLimit 限制预览流大小，避免预览变成不计次的完整下载。
	base, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		base = strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])
	}
	base = strings.ToLower(base)
	if !previewMIMETypes[base] {
		return 0
	}
	if strings.HasPrefix(base, "text/") || base == "application/json" {
		return 64 * 1024
	}
	return 512 * 1024
}

// folderRequest 是目录创建/重命名请求体。
// folderRequest is the folder create/rename request body.
type folderRequest struct {
	Name   string `json:"name"`
	Parent string `json:"parent"`
}

// validateFolderName 校验单个目录名（禁止路径分隔符，长度 ≤255 字节，拒绝控制字符与 Windows 非法字符）。
// validateFolderName validates a single folder name (no separators, ≤255 bytes, no control or Windows-illegal characters).
func validateFolderName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return "", errors.New("invalid folder name")
	}
	for _, char := range name {
		if unicode.IsControl(char) || strings.ContainsRune(`<>:"|?*`, char) {
			return "", errors.New("invalid folder name")
		}
	}
	if len([]byte(name)) > 255 {
		return "", errors.New("invalid folder name")
	}
	return name, nil
}

// createFolder 创建用户目录（parent 为空表示根目录）。
// createFolder creates a user folder; an empty parent means the root directory.
func (s *Server) createFolder(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	if s.rejectReadOnly(w, r, user, "folder_create", "") {
		return
	}
	var input folderRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	name, err := validateFolderName(input.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "目录名无效")
		return
	}
	parent := ""
	if strings.TrimSpace(input.Parent) != "" {
		validated, err := validateUploadDir(input.Parent)
		if err != nil {
			writeError(w, http.StatusBadRequest, "目录无效")
			return
		}
		parent = validated
	}
	folder, err := s.store.CreateFolder(r.Context(), user.ID, parent, name)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "父目录不存在")
		return
	}
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "同名目录已存在")
		return
	}
	if err != nil {
		log.Printf("create folder: %v", err)
		writeError(w, http.StatusInternalServerError, "创建目录失败")
		return
	}
	s.serviceEvent(r, "folder_create", user.Username, "target=%s result=success", folder.Path)
	writeData(w, http.StatusCreated, "目录已创建", folder)
}

// normalizeFolderPath 归一化目录记录路径：剥掉历史遗留的 files/ 或 files/<uid>/ storage 前缀，
// 再按 validateUploadDir 校验相对路径；不合法（如 v010 遗留 uploads\xxx、`.`、`..`）返回 false。
// normalizeFolderPath strips the legacy files/ or files/<uid>/ storage prefix from a folder-record path,
// then validates the relative path; records that fail (e.g. legacy uploads\xxx, ".", "..") yield false.
func normalizeFolderPath(path string) (string, bool) {
	normalized := strings.TrimSpace(path)
	// v010 遗留路径以反斜杠分隔（uploads\xxx）：历史孤儿记录，直接过滤，不归一化，
	// 避免把不存在的目录当作可进入目录展示。
	// v010 legacy paths use backslashes (uploads\xxx): orphaned records are dropped as-is,
	// so nonexistent directories never appear as navigable folders.
	if strings.Contains(normalized, "\\") {
		return "", false
	}
	normalized = filepath.ToSlash(normalized)
	// 剥掉 storage 前缀：files/<uid>/… 或 files/…
	re := regexp.MustCompile(`^files/\d+/`)
	if re.MatchString(normalized) {
		normalized = re.ReplaceAllString(normalized, "")
	} else if strings.HasPrefix(normalized, "files/") {
		normalized = strings.TrimPrefix(normalized, "files/")
	}
	validated, err := validateUploadDir(normalized)
	if err != nil {
		return "", false
	}
	return validated, true
}

// listFolders 返回当前用户的全部目录（过滤并归一化历史遗留的非法/带前缀路径，v019 #4）。
// listFolders returns all of the current user's folders, filtering and normalizing legacy invalid/prefixed paths (v019 #4).
func (s *Server) listFolders(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	folders, err := s.store.ListFolders(r.Context(), user.ID)
	if err != nil {
		log.Printf("list folders: %v", err)
		writeError(w, http.StatusInternalServerError, "获取目录列表失败")
		return
	}
	items := make([]store.Folder, 0, len(folders))
	for _, folder := range folders {
		normalized, ok := normalizeFolderPath(folder.Path)
		if !ok {
			// 历史遗留的非法路径记录（如 v010 反斜杠路径）不再返回，避免前端点击触发「目录无效」。
			// Legacy invalid path records (e.g. v010 backslash paths) are dropped so navigation never hits "invalid directory".
			continue
		}
		folder.Path = normalized
		items = append(items, folder)
	}
	s.serviceEvent(r, "folder_list", user.Username, "result=success count=%d", len(items))
	writeData(w, http.StatusOK, "获取成功", map[string]any{"items": items})
}

// renameFolder 重命名目录并级联更新子目录与文件路径。
// renameFolder renames a folder and cascades the change to child folders and files.
func (s *Server) renameFolder(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	if s.rejectReadOnly(w, r, user, "folder_rename", r.PathValue("id")) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "目录编号无效")
		return
	}
	var input folderRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	name, err := validateFolderName(input.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "目录名无效")
		return
	}
	err = s.store.RenameFolder(r.Context(), id, user.ID, name)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "目录不存在")
		return
	}
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "同名目录已存在")
		return
	}
	if err != nil {
		log.Printf("rename folder: %v", err)
		writeError(w, http.StatusInternalServerError, "重命名目录失败")
		return
	}
	folder, _ := s.store.GetFolderByID(r.Context(), id, user.ID)
	s.serviceEvent(r, "folder_rename", user.Username, "target=%s result=success", folder.Path)
	writeData(w, http.StatusOK, "目录已重命名", folder)
}

// deleteFolder 删除空目录（非空返回 400，防止误删）。
// deleteFolder deletes an empty folder and rejects non-empty directories with 400.
func (s *Server) deleteFolder(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	if s.rejectReadOnly(w, r, user, "folder_delete", r.PathValue("id")) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "目录编号无效")
		return
	}
	folder, findErr := s.store.GetFolderByID(r.Context(), id, user.ID)
	deleted, err := s.store.DeleteFolder(r.Context(), id, user.ID)
	if errors.Is(err, store.ErrNotFound) || (findErr != nil && errors.Is(findErr, store.ErrNotFound)) {
		writeError(w, http.StatusNotFound, "目录不存在")
		return
	}
	if errors.Is(err, store.ErrNotEmpty) {
		writeError(w, http.StatusBadRequest, "目录非空，无法删除")
		return
	}
	if err != nil {
		log.Printf("delete folder: %v", err)
		writeError(w, http.StatusInternalServerError, "删除目录失败")
		return
	}
	s.serviceEvent(r, "folder_delete", user.Username, "target=%s result=success", folder.Path)
	writeData(w, http.StatusOK, "目录已删除", map[string]any{"removed": deleted})
}

func (s *Server) deleteFile(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	if s.rejectReadOnly(w, r, user, "delete", r.PathValue("id")) {
		return
	}
	target := r.PathValue("id")
	if id, parseErr := strconv.ParseInt(target, 10, 64); parseErr == nil {
		if file, findErr := s.store.FindFile(r.Context(), id); findErr == nil {
			target = file.Name
		}
	}
	serviceResult, serviceReason := "failure", "delete_failed"
	defer func() {
		s.serviceEvent(r, "delete", user.Username, "name=%s result=%s reason=%s", target, serviceResult, serviceReason)
	}()
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "文件编号无效")
		return
	}
	path, err := s.store.DeleteFile(r.Context(), id, user.ID, user.Role == "admin")
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}
	if err != nil {
		log.Printf("delete file: %v", err)
		writeError(w, http.StatusInternalServerError, "删除文件失败")
		return
	}
	if err := os.Remove(filepath.Join(s.config.DataDir, path)); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("remove file content: %v", err)
	}
	serviceResult, serviceReason = "success", ""
	writeData(w, http.StatusOK, "文件已删除", nil)
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	page, pageSize := pagination(r)
	users, total, err := s.store.ListUsers(r.Context(), strings.TrimSpace(r.URL.Query().Get("keyword")), page, pageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "获取用户列表失败")
		return
	}
	items := make([]map[string]any, 0, len(users))
	for _, user := range users {
		items = append(items, publicUser(user))
	}
	admin := currentUser(r.Context())
	s.serviceEvent(r, "user_list", admin.Username, "target=users result=success page=%d page_size=%d total=%d", page, pageSize, total)
	writeData(w, http.StatusOK, "获取成功", map[string]any{"items": items, "page": page, "pageSize": pageSize, "total": total})
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var input createUserRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	if input.Username == "" || len(input.Username) > 64 || input.Password == "" || len(input.Password) > 200 || (input.Role != "admin" && input.Role != "user") {
		writeError(w, http.StatusBadRequest, "用户信息无效")
		return
	}
	if err := s.validatePassword(input.Password); err != nil {
		writeError(w, http.StatusBadRequest, "密码不符合强度要求")
		return
	}
	if input.QuotaBytes <= 0 {
		input.QuotaBytes = 100 * 1024 * 1024 * 1024
	}
	whitelist, err := normalizeWhitelist(input.IPWhitelist)
	if err != nil {
		writeError(w, http.StatusBadRequest, "IP 白名单格式无效")
		return
	}
	plainTOTPSecret, encryptedTOTPSecret, err := s.prepareTOTPSecret(input.TOTPEnabled || input.Reenroll)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成动态验证密钥失败")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "密码处理失败")
		return
	}
	if err := s.store.CreateUser(r.Context(), input.Username, string(hash), input.Role, input.QuotaBytes); errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "用户名已存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "创建用户失败")
		return
	}
	user, err := s.store.GetUserByUsername(input.Username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取新用户信息失败")
		return
	}
	if encryptedTOTPSecret != "" {
		if err := s.store.SetTOTP(r.Context(), user.ID, encryptedTOTPSecret, input.TOTPEnabled && !input.Reenroll); err != nil {
			writeError(w, http.StatusInternalServerError, "保存动态验证设置失败")
			return
		}
	}
	if err := s.store.UpdateIPACL(r.Context(), user.ID, input.IPACLEnabled, whitelist); err != nil {
		writeError(w, http.StatusInternalServerError, "保存 IP 白名单失败")
		return
	}
	user, err = s.store.GetUser(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取新用户信息失败")
		return
	}
	admin := currentUser(r.Context())
	s.serviceEvent(r, "user_create", admin.Username, "target=%s role=%s totp_enabled=%t ip_acl_enabled=%t result=success", input.Username, input.Role, input.TOTPEnabled && !input.Reenroll, input.IPACLEnabled)
	data := publicUser(user)
	if plainTOTPSecret != "" {
		data["totpSecret"] = plainTOTPSecret
	}
	writeData(w, http.StatusCreated, "用户已创建", data)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	admin := currentUser(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "用户编号无效")
		return
	}
	var input updateUserRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	previous, _ := s.store.GetUser(id)
	if previous.Role == "admin" && ((input.Role != nil && *input.Role != "admin") || (input.Disabled != nil && *input.Disabled)) {
		var adminCount int
		if err := s.store.DB.QueryRowContext(r.Context(), "SELECT COUNT(id) FROM users WHERE role = 'admin'").Scan(&adminCount); err != nil {
			writeError(w, http.StatusInternalServerError, "检查管理员账号失败")
			return
		}
		if adminCount <= 1 {
			writeError(w, http.StatusBadRequest, "不能移除唯一管理员账号")
			return
		}
	}
	var hash *string
	if input.Password != "" {
		if err := s.validatePassword(input.Password); err != nil {
			writeError(w, http.StatusBadRequest, "密码不符合强度要求")
			return
		}
		value, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "密码处理失败")
			return
		}
		encoded := string(value)
		hash = &encoded
	}
	if err := s.store.UpdateUser(r.Context(), id, input.Role, input.QuotaBytes, input.Disabled, hash); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	} else if errors.Is(err, store.ErrQuota) {
		writeError(w, http.StatusBadRequest, "配额不能小于已用空间")
		return
	} else if err != nil {
		writeError(w, http.StatusBadRequest, "用户信息无效")
		return
	}
	user, _ := s.store.GetUser(id)
	s.serviceEvent(r, "user_update", admin.Username, "target=%s result=success", user.Username)
	if input.Disabled != nil && previous.Disabled != *input.Disabled {
		s.serviceEvent(r, "user_disabled", admin.Username, "target=%s disabled=%t result=success", user.Username, *input.Disabled)
	}
	if input.Password != "" {
		s.serviceEvent(r, "password_reset", admin.Username, "target=%s result=success", user.Username)
	}
	writeData(w, http.StatusOK, "用户已更新", publicUser(user))
}

// updateUserReadOnly 保存管理员为指定用户设置的一次性只读窗口。
// updateUserReadOnly stores an administrator-managed one-time read-only window.
func (s *Server) updateUserReadOnly(w http.ResponseWriter, r *http.Request) {
	admin := currentUser(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "用户编号无效")
		return
	}
	var input readOnlyRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	input.From = strings.TrimSpace(input.From)
	input.Until = strings.TrimSpace(input.Until)
	if (input.From == "") != (input.Until == "") {
		writeErrorData(w, http.StatusBadRequest, "只读时段必须同时设置开始和结束时间", map[string]string{"code": "INVALID_READ_ONLY_WINDOW"})
		return
	}
	if input.From != "" {
		from, fromErr := time.Parse(time.RFC3339, input.From)
		until, untilErr := time.Parse(time.RFC3339, input.Until)
		if fromErr != nil || untilErr != nil || from.After(until) {
			writeErrorData(w, http.StatusBadRequest, "只读时段无效", map[string]string{"code": "INVALID_READ_ONLY_WINDOW"})
			return
		}
	}
	user, err := s.store.UpdateUserReadOnly(r.Context(), id, input.From, input.Until)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	if err != nil {
		log.Printf("update user read-only window: %v", err)
		writeError(w, http.StatusInternalServerError, "保存只读时段失败")
		return
	}
	s.serviceEvent(r, "user_read_only_update", admin.Username, "target=%s from_set=%t until_set=%t result=success", user.Username, input.From != "", input.Until != "")
	writeData(w, http.StatusOK, "只读时段已更新", publicUser(user))
}

func (s *Server) updateUserTOTP(w http.ResponseWriter, r *http.Request) {
	admin := currentUser(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "用户编号无效")
		return
	}
	var input totpToggleRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	user, err := s.store.GetUser(id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "获取用户信息失败")
		return
	}
	_, secret, err := s.prepareTOTPSecret(input.Enabled || input.Reenroll)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成动态验证密钥失败")
		return
	}
	if err := s.store.SetTOTP(r.Context(), user.ID, secret, input.Enabled && !input.Reenroll); err != nil {
		writeError(w, http.StatusInternalServerError, "保存动态验证设置失败")
		return
	}
	updated, _ := s.store.GetUser(id)
	s.serviceEvent(r, "totp_update", admin.Username, "target=%s enabled=%t reenroll=%t result=success", user.Username, input.Enabled, input.Reenroll)
	writeData(w, http.StatusOK, "动态验证设置已更新", publicUser(updated))
}

func (s *Server) updateUserIPACL(w http.ResponseWriter, r *http.Request) {
	admin := currentUser(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "用户编号无效")
		return
	}
	var input ipACLRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	whitelist, err := normalizeWhitelist(input.Whitelist)
	if err != nil {
		writeError(w, http.StatusBadRequest, "IP 白名单格式无效")
		return
	}
	if err := s.store.UpdateIPACL(r.Context(), id, input.Enabled, whitelist); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "保存 IP 白名单失败")
		return
	}
	user, _ := s.store.GetUser(id)
	s.serviceEvent(r, "ip_acl_update", admin.Username, "target=%s enabled=%t result=success", user.Username, input.Enabled)
	writeData(w, http.StatusOK, "IP 白名单设置已更新", publicUser(user))
}

// prepareTOTPSecret 按需生成并加密新的 TOTP 密钥；返回值依次为明文和密文。
// prepareTOTPSecret generates and encrypts a new TOTP secret when requested.
func (s *Server) prepareTOTPSecret(requested bool) (string, string, error) {
	if !requested {
		return "", "", nil
	}
	randomSecret := make([]byte, 20)
	if _, err := rand.Read(randomSecret); err != nil {
		return "", "", err
	}
	plain := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(randomSecret)
	encrypted, err := s.encryptTOTPSecret(plain)
	if err != nil {
		return "", "", err
	}
	return plain, encrypted, nil
}

func normalizeWhitelist(value string) (string, error) {
	parts := strings.Split(value, ",")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if ip := net.ParseIP(part); ip == nil {
			if _, _, err := net.ParseCIDR(part); err != nil {
				return "", errors.New("invalid ip whitelist")
			}
		}
		clean = append(clean, part)
	}
	return strings.Join(clean, ","), nil
}

func (s *Server) listLocks(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.GetLogSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "获取锁定信息失败")
		return
	}
	ipLocks, err := s.store.ListIPLocks(r.Context(), settings.IPLockWindowMinutes, settings.IPAutoUnlockEnabled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "获取锁定信息失败")
		return
	}
	userLocks, err := s.store.ListUserLocks(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "获取锁定信息失败")
		return
	}
	writeData(w, http.StatusOK, "获取成功", map[string]any{"ipLocks": ipLocks, "userLocks": userLocks})
}

func (s *Server) deleteIPLock(w http.ResponseWriter, r *http.Request) {
	admin := currentUser(r.Context())
	ip := r.PathValue("ip")
	if net.ParseIP(ip) == nil {
		writeError(w, http.StatusBadRequest, "IP 地址无效")
		return
	}
	if err := s.store.DeleteIPFailure(r.Context(), ip); err != nil {
		writeError(w, http.StatusInternalServerError, "解除 IP 锁定失败")
		return
	}
	s.serviceEvent(r, "lock_release", admin.Username, "target=%s lock=ip result=success", ip)
	writeData(w, http.StatusOK, "IP 锁定已解除", nil)
}

func (s *Server) deleteUserLock(w http.ResponseWriter, r *http.Request) {
	admin := currentUser(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "用户编号无效")
		return
	}
	if err := s.store.DeleteUserLock(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "解除用户锁定失败")
		return
	}
	target := strconv.FormatInt(id, 10)
	if user, getErr := s.store.GetUser(id); getErr == nil {
		target = user.Username
	}
	s.serviceEvent(r, "lock_release", admin.Username, "target=%s lock=user result=success", target)
	writeData(w, http.StatusOK, "用户锁定已解除", nil)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	admin := currentUser(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id == admin.ID {
		writeError(w, http.StatusBadRequest, "不能删除当前管理员")
		return
	}
	target := strconv.FormatInt(id, 10)
	if user, getErr := s.store.GetUser(id); getErr == nil {
		target = user.Username
	}
	paths, err := s.store.DeleteUser(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "删除用户失败")
		return
	}
	for _, path := range paths {
		if removeErr := os.Remove(filepath.Join(s.config.DataDir, path)); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			log.Printf("remove deleted user file: %v", removeErr)
		}
	}
	if removeErr := os.RemoveAll(filepath.Join(s.config.DataDir, "files", strconv.FormatInt(id, 10))); removeErr != nil {
		log.Printf("remove deleted user directory: %v", removeErr)
	}
	s.serviceEvent(r, "user_delete", admin.Username, "target=%s result=success", target)
	writeData(w, http.StatusOK, "用户已删除", nil)
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	// stats 汇总数据库文件统计和数据目录所在文件系统的磁盘统计。
	// stats combines database file counters with filesystem statistics for the data directory.
	stats, err := s.store.Stats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "获取统计失败")
		return
	}
	total, free, used, err := diskusage.DiskUsage(s.config.DataDir)
	if err != nil {
		log.Printf("read disk usage: %v", err)
		writeError(w, http.StatusInternalServerError, "获取磁盘统计失败")
		return
	}
	data := map[string]any{
		"users":          stats["users"],
		"files":          stats["files"],
		"bytes":          stats["bytes"],
		"shares":         stats["shares"],
		"shareDownloads": stats["shareDownloads"],
		"disk": map[string]int64{
			"total":        total,
			"used":         used,
			"free":         free,
			"usagePercent": diskUsagePercent(total, used),
		},
	}
	admin := currentUser(r.Context())
	s.serviceEvent(r, "admin_stats", admin.Username, "target=stats result=success")
	writeData(w, http.StatusOK, "获取成功", data)
}

func (s *Server) listLogs(w http.ResponseWriter, r *http.Request) {
	// listLogs 强制普通用户只能查看自己的日志，管理员可以按用户、操作、结果和关键字筛选。
	// listLogs restricts regular users to their own records while allowing admins to filter by user, action, result, and keyword.
	user := currentUser(r.Context())
	var userID *int64
	if user.Role == "admin" && r.URL.Query().Get("userId") != "" {
		id, err := strconv.ParseInt(r.URL.Query().Get("userId"), 10, 64)
		if err != nil || id < 1 {
			writeError(w, http.StatusBadRequest, "用户编号无效")
			return
		}
		userID = &id
	} else if user.Role != "admin" {
		id := user.ID
		userID = &id
	}
	page, pageSize := pagination(r)
	// 时间范围筛选：from/to 接受 RFC3339/ISO8601 字符串，按 audit_logs.created_at（TEXT）比较。
	// Time-range filter: from/to accept RFC3339/ISO8601 strings compared against audit_logs.created_at (TEXT).
	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	if from != "" {
		if _, err := time.Parse(time.RFC3339, from); err != nil {
			writeError(w, http.StatusBadRequest, "开始时间格式无效")
			return
		}
	}
	if to != "" {
		if _, err := time.Parse(time.RFC3339, to); err != nil {
			writeError(w, http.StatusBadRequest, "结束时间格式无效")
			return
		}
	}
	logs, total, err := s.store.ListAuditLogs(r.Context(), userID, strings.TrimSpace(r.URL.Query().Get("action")), strings.TrimSpace(r.URL.Query().Get("result")), strings.TrimSpace(r.URL.Query().Get("keyword")), from, to, page, pageSize)
	if err != nil {
		log.Printf("list audit logs: %v", err)
		writeError(w, http.StatusInternalServerError, "获取日志失败")
		return
	}
	s.serviceEvent(r, "log_list", user.Username, "target=logs result=success page=%d page_size=%d total=%d", page, pageSize, total)
	writeData(w, http.StatusOK, "获取成功", map[string]any{"items": logs, "page": page, "pageSize": pageSize, "total": total})
}

// logActions 返回全部审计动作（含"系统配置"类事件），供日志页筛选。
// logActions returns every audit action (including system-configuration events) for log-page filtering.
func (s *Server) logActions(w http.ResponseWriter, r *http.Request) {
	// usedOnly=true 时仅返回当前用户日志中实际存在的动作类型（按发生时间去重），
	// 避免普通用户看到自己从未触发的"系统配置"类筛选项（问题 6）；异步加载由前端控制。
	// usedOnly=true returns only the action types actually present in the current user's logs
	// (deduplicated by recency), so regular users are not offered filters they never triggered.
	all := []string{
		"login", "register", "upload", "upload_init", "upload_chunk", "download", "delete", "share", "share_view", "share_download",
		"share_extend", "share_increase", "share_revoke", "batch_share", "share_group_extend", "share_group_increase",
		"settings_update", "brand_update", "language_update", "password_change", "password_reset",
		"user_create", "user_update", "user_disabled", "totp_update", "ip_acl_update",
		"folder_create", "folder_list", "folder_rename", "folder_delete", "collection", "upload_collect", "upload_collect_fail",
		"file_list", "admin_stats", "log_list",
	}
	if r.URL.Query().Get("usedOnly") != "true" {
		writeData(w, http.StatusOK, "获取成功", all)
		return
	}
	user := currentUser(r.Context())
	used, err := s.store.ListUsedActions(r.Context(), user.ID, user.Role == "admin")
	if err != nil {
		log.Printf("list used actions: %v", err)
		writeData(w, http.StatusOK, "获取成功", all)
		return
	}
	writeData(w, http.StatusOK, "获取成功", used)
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.GetLogSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "获取设置失败")
		return
	}
	writeData(w, http.StatusOK, "获取成功", settings)
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var input logSettingsRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	settings, err := s.store.GetLogSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取设置失败")
		return
	}
	if input.LogRetentionDays != nil {
		settings.LogRetentionDays = *input.LogRetentionDays
	}
	if input.LockThreshold != nil {
		settings.LockThreshold = *input.LockThreshold
	}
	if input.AutoUnlockEnabled != nil {
		settings.AutoUnlockEnabled = *input.AutoUnlockEnabled
	}
	if input.AutoUnlockMinutes != nil {
		settings.AutoUnlockMinutes = *input.AutoUnlockMinutes
	}
	if input.DefaultLang != nil {
		settings.DefaultLang = *input.DefaultLang
	}
	if input.ThemeColor != nil {
		settings.ThemeColor = *input.ThemeColor
	}
	if input.PasswordMinLength != nil {
		settings.PasswordMinLength = *input.PasswordMinLength
	}
	if input.PasswordComplexity != nil {
		settings.PasswordComplexity = *input.PasswordComplexity
	}
	if input.IPLockWindowMinutes != nil {
		settings.IPLockWindowMinutes = *input.IPLockWindowMinutes
	}
	if input.IPLockThreshold != nil {
		settings.IPLockThreshold = *input.IPLockThreshold
	}
	if input.IPAutoUnlockEnabled != nil {
		settings.IPAutoUnlockEnabled = *input.IPAutoUnlockEnabled
	}
	if input.IPUnlockMinutes != nil {
		settings.IPUnlockMinutes = *input.IPUnlockMinutes
	}
	if input.RegisterEnabled != nil {
		settings.RegisterEnabled = *input.RegisterEnabled
	}
	if input.UploadRateLimit != nil {
		settings.UploadRateLimit = *input.UploadRateLimit
	}
	if input.TrustProxy != nil {
		settings.TrustProxy = *input.TrustProxy
	}
	if validationError := validateLogSettings(settings); validationError != nil {
		writeError(w, http.StatusBadRequest, validationError.Error())
		return
	}
	if err := s.store.UpdateLogSettings(r.Context(), settings); err != nil {
		writeError(w, http.StatusBadRequest, "设置无效")
		return
	}
	updatedSettings, err := s.store.GetLogSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取设置失败")
		return
	}
	admin := currentUser(r.Context())
	s.serviceEvent(r, "settings_update", admin.Username, "target=settings result=success")
	writeData(w, http.StatusOK, "设置已更新", updatedSettings)
}

// validateLogSettings 返回首个无效设置字段，便于 API 返回可操作的错误提示。
// validateLogSettings identifies the first invalid setting for an actionable API error.
func validateLogSettings(settings store.LogSettings) error {
	switch {
	case settings.LogRetentionDays < 0:
		return errors.New("日志留存天数无效")
	case settings.LogRetentionDays > maxLogRetentionDays:
		return errors.New("日志留存天数超过上限")
	case settings.LockThreshold < 0:
		return errors.New("登录失败锁定阈值无效")
	case settings.AutoUnlockMinutes < 1:
		return errors.New("自动解锁时长无效")
	case settings.DefaultLang != "zh-CN" && settings.DefaultLang != "zh-TW" && settings.DefaultLang != "en":
		return errors.New("系统默认语言无效")
	case settings.ThemeColor != "" && !validThemeColor(settings.ThemeColor):
		return errors.New("界面主题色无效")
	case settings.PasswordMinLength < 1 || settings.PasswordMinLength > 200:
		return errors.New("密码最小长度无效")
	case settings.PasswordComplexity < 0 || settings.PasswordComplexity > 4:
		return errors.New("密码复杂度无效")
	case settings.IPLockWindowMinutes < 1:
		return errors.New("IP 锁定窗口无效")
	case settings.IPLockThreshold < 0:
		return errors.New("IP 锁定阈值无效")
	case settings.IPUnlockMinutes < 1:
		return errors.New("IP 解锁时长无效")
	case settings.UploadRateLimit < 0:
		return errors.New("上传限速无效")
	default:
		return nil
	}
}

func validThemeColor(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 4 && len(value) != 7 || value[0] != '#' {
		return false
	}
	for _, char := range value[1:] {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}

func (s *Server) recordAudit(r *http.Request, userID *int64, username, action, target, result, reason string) {
	// recordAudit 统一写入来源 IP 和业务结果；审计清理由后台定时任务执行。
	// recordAudit centralizes source-IP and outcome recording; audit retention is handled by scheduled cleanup.
	if err := s.store.AddAuditLog(r.Context(), userID, username, action, target, s.requestIP(r), result, reason); err != nil {
		log.Printf("write audit log: %v", err)
	}
}

// recordShareAudit adds creator ownership to anonymous share events without exposing it in the request identity.
// recordShareAudit 为匿名分享事件记录创建者归属，但不伪造请求用户身份。
func (s *Server) recordShareAudit(r *http.Request, userID, shareOwnerID *int64, username, action, target, result, reason string) {
	if err := s.store.AddAuditLogWithShareOwner(r.Context(), userID, shareOwnerID, username, action, target, s.requestIP(r), result, reason); err != nil {
		log.Printf("write share audit log: %v", err)
	}
}

func (s *Server) serviceEvent(r *http.Request, event, operator, details string, args ...any) {
	if s.config.Logger == nil {
		return
	}
	ip := "-"
	if r != nil {
		ip = s.requestIP(r)
	}
	values := make([]any, 0, len(args)+2)
	values = append(values, operator, ip)
	values = append(values, args...)
	s.config.Logger.Event(event, "operator=%s ip=%s "+details, values...)
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	// requireAuth 验证 Bearer JWT，并把当前用户放入请求上下文。
	// requireAuth validates the Bearer JWT and places the current user in the request context.
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := s.authenticate(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "请先登录")
			return
		}
		request := r.WithContext(context.WithValue(r.Context(), userContextKey, user))
		if user.MustChangePassword && request.URL.Path != "/api/auth/me" && request.URL.Path != "/api/auth/change-password" && request.URL.Path != "/api/auth/password-policy" {
			writeErrorData(w, http.StatusForbidden, "请先修改初始密码", map[string]string{"code": "PASSWORD_CHANGE_REQUIRED"})
			return
		}
		if user.IPACLEnabled && !ipAllowed(s.requestIP(request), user.IPWhitelist) {
			writeError(w, http.StatusForbidden, "当前 IP 不在白名单")
			return
		}
		next(w, request)
	}
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	// requireAdmin 在认证基础上限制管理员角色访问。
	// requireAdmin restricts access to the admin role after authentication.
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if currentUser(r.Context()).Role != "admin" {
			writeError(w, http.StatusForbidden, "需要管理员权限")
			return
		}
		next(w, r)
	})
}

func (s *Server) authenticate(r *http.Request) (store.User, error) {
	// authenticate 只接受 HS256 JWT，并在每次请求重新读取用户以识别禁用账号。
	// authenticate accepts only HS256 JWTs and reloads the user on every request to detect disabled accounts.
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return store.User{}, errors.New("missing bearer token")
	}
	token, err := jwt.Parse(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")), func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.config.JWTSecret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil || !token.Valid {
		return store.User{}, errors.New("invalid token")
	}
	sub, err := token.Claims.GetSubject()
	if err != nil {
		return store.User{}, err
	}
	id, err := strconv.ParseInt(sub, 10, 64)
	if err != nil {
		return store.User{}, err
	}
	user, err := s.store.GetUser(id)
	if err != nil || user.Disabled {
		return store.User{}, errors.New("user unavailable")
	}
	issuedAt, err := token.Claims.GetIssuedAt()
	if err != nil || issuedAt == nil {
		return store.User{}, errors.New("invalid token")
	}
	if user.LastPasswordChange != "" {
		lastChange, parseErr := time.Parse(time.RFC3339, user.LastPasswordChange)
		// 容差 1µs：JWT 的 iat 以 JSON number（float64）序列化/回读，纳秒精度会丢失约 ±0.5µs。
		// 若新 token 恰在改密后的亚微秒窗口内签发，回读 iat 可能略小于 last_password_change 而被误拒。
		// A 1µs tolerance absorbs the float64 round-trip error of the JWT iat claim (≈±0.5µs); without it a
		// token issued within a microsecond of a password change could be wrongly rejected as pre-change.
		if parseErr == nil && issuedAt.Time.Add(time.Microsecond).Before(lastChange) {
			return store.User{}, errors.New("invalid token")
		}
	}
	if user.LastLogoutAt != "" {
		lastLogout, parseErr := time.Parse(time.RFC3339, user.LastLogoutAt)
		// JWT iat is second-precision; reject the rounding window after logout as well.
		// JWT 的 iat 只有秒级精度，注销时一并拒绝该精度窗口，避免跨秒时旧令牌复活。
		if parseErr == nil && !issuedAt.Time.After(lastLogout.Add(time.Second)) {
			return store.User{}, errors.New("invalid token")
		}
	}
	return user, nil
}

func (s *Server) issueToken(user store.User) (string, error) {
	// issueToken 使用 HS256 为用户 ID 签发带有效期的 JWT。
	// issueToken signs a time-limited HS256 JWT whose subject is the user ID.
	now := time.Now()
	claims := jwt.RegisteredClaims{Subject: strconv.FormatInt(user.ID, 10), IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(s.config.JWTExpiry))}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.config.JWTSecret)
}

func currentUser(ctx context.Context) store.User {
	user, _ := ctx.Value(userContextKey).(store.User)
	return user
}

// userReadOnly 判断用户当前是否处于包含边界的只读时段；管理员不受该限制。
// userReadOnly checks an inclusive read-only window; administrators are exempt.
func (s *Server) userReadOnly(user store.User) bool {
	return userReadOnlyAt(user, time.Now().UTC())
}

func userReadOnlyAt(user store.User, now time.Time) bool {
	if user.Role == "admin" || user.ReadOnlyFrom == "" || user.ReadOnlyUntil == "" {
		return false
	}
	from, fromErr := time.Parse(time.RFC3339, user.ReadOnlyFrom)
	until, untilErr := time.Parse(time.RFC3339, user.ReadOnlyUntil)
	if fromErr != nil || untilErr != nil || from.After(until) {
		return false
	}
	return !now.Before(from) && !now.After(until)
}

// rejectReadOnly 统一拒绝只读时段内的写操作，并记录原动作和拒绝原因。
// rejectReadOnly rejects writes during a read-only window and records the original action and reason.
func (s *Server) rejectReadOnly(w http.ResponseWriter, r *http.Request, user store.User, action, target string) bool {
	if !s.userReadOnly(user) {
		return false
	}
	s.recordAudit(r, &user.ID, user.Username, action, target, "failure", "read_only")
	s.serviceEvent(r, action, user.Username, "target=%s result=failure reason=read_only", target)
	writeErrorData(w, http.StatusForbidden, "当前账号处于只读时段，仅可查看和下载", map[string]string{"code": "READ_ONLY"})
	return true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式无效")
		return false
	}
	return true
}

func writeData(w http.ResponseWriter, status int, message string, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response{Code: status, Message: message, Data: data})
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeData(w, status, message, nil)
}

func writeErrorData(w http.ResponseWriter, status int, message string, data any) {
	writeData(w, status, message, data)
}

func publicUser(user store.User) map[string]any {
	readOnly := userReadOnlyAt(user, time.Now().UTC())
	return map[string]any{"id": user.ID, "username": user.Username, "role": user.Role, "language": user.Language, "quotaBytes": user.QuotaBytes, "usedBytes": user.UsedBytes, "folderCount": user.FolderCount, "fileCount": user.FileCount, "disabled": user.Disabled, "mustChangePassword": user.MustChangePassword, "totpEnabled": user.TOTPEnabled, "ipAclEnabled": user.IPACLEnabled, "ipWhitelist": user.IPWhitelist, "readOnlyFrom": user.ReadOnlyFrom, "readOnlyUntil": user.ReadOnlyUntil, "readOnly": readOnly, "createdAt": user.CreatedAt}
}

func isValidLang(value string) bool {
	return value == "" || value == "zh-CN" || value == "zh-TW" || value == "en"
}

func publicFile(file store.File) map[string]any {
	return map[string]any{"id": file.ID, "name": file.Name, "size": file.Size, "mime": file.Mime, "sha256": file.SHA256, "md5": file.MD5, "status": file.Status, "createdAt": file.CreatedAt}
}

func sanitizeName(name string) (string, error) {
	// sanitizeName 保留原始文件名语义，同时将 Windows 非法字符替换为下划线。
	// sanitizeName preserves the original filename semantics while replacing Windows-illegal characters with underscores.
	name, err := originalName(name)
	if err != nil {
		return "", err
	}
	var sanitized strings.Builder
	for _, char := range name {
		switch char {
		case '<', '>', ':', '"', '|', '?', '*':
			sanitized.WriteByte('_')
		default:
			sanitized.WriteRune(char)
		}
	}
	name = sanitized.String()
	if name == "" || name == "." || name == ".." || len(name) > 255 {
		return "", errors.New("invalid name")
	}
	return name, nil
}

func validateUploadName(name string) (string, error) {
	// validateUploadName 在进入存储层前拒绝路径分隔符、控制字符、遍历标记和超长名称。
	// validateUploadName rejects separators, control characters, traversal markers, and overlong names before storage.
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || name == "" || name == "." || name == ".." || strings.Contains(name, "..") || len(name) > 255 {
		return "", errors.New("invalid name")
	}
	for _, char := range name {
		if unicode.IsControl(char) {
			return "", errors.New("control character")
		}
		switch char {
		case '/', '\\', '<', '>', ':', '"', '|', '?', '*':
			return "", errors.New("illegal character")
		}
	}
	return name, nil
}

func originalName(name string) (string, error) {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || name == "" || name == "." || name == ".." {
		return "", errors.New("invalid name")
	}
	for _, char := range name {
		if unicode.IsControl(char) {
			return "", errors.New("control character")
		}
	}
	return name, nil
}

func diskUsagePercent(total, used int64) int64 {
	if total <= 0 || used <= 0 {
		return 0
	}
	if used >= total {
		return 100
	}
	return used * 100 / total
}

func randomID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func pagination(r *http.Request) (int, int) {
	page := parsePositive(r.URL.Query().Get("page"), 1)
	if page > maxPage {
		page = maxPage
	}
	pageSize := parsePositive(r.URL.Query().Get("pageSize"), 20)
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func parsePositive(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}

func contentDisposition(name string) string {
	// contentDisposition 清理引号和反斜杠后生成下载响应的原始文件名头。
	// contentDisposition sanitizes quotes and backslashes before producing the download filename header.
	quoted := strings.ReplaceAll(name, "\\", "_")
	quoted = strings.ReplaceAll(quoted, "\"", "_")
	return fmt.Sprintf("attachment; filename=\"%s\"; filename*=UTF-8''%s", quoted, url.PathEscape(name))
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func (s *Server) requestIP(r *http.Request) string {
	// 只有管理员显式开启且直连来源在可信代理白名单内时，才解析 X-Forwarded-For。
	// X-Forwarded-For is used only when enabled in settings and the direct peer is trusted.
	remote := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		remote = host
	}
	remoteIP := net.ParseIP(remote)
	settings, err := s.store.GetLogSettings(r.Context())
	if err != nil || !settings.TrustProxy || remoteIP == nil || len(s.config.TrustedProxies) == 0 || !trustedIP(remoteIP, s.config.TrustedProxies) {
		return remote
	}
	forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	valid := make([]net.IP, 0, len(forwarded))
	for _, value := range forwarded {
		if ip := net.ParseIP(strings.TrimSpace(value)); ip != nil {
			valid = append(valid, ip)
		}
	}
	for i := len(valid) - 1; i >= 0; i-- {
		if !trustedIP(valid[i], s.config.TrustedProxies) {
			return valid[i].String()
		}
	}
	if len(valid) > 0 {
		return valid[0].String()
	}
	return remote
}

func trustedIP(ip net.IP, proxies []*net.IPNet) bool {
	for _, network := range proxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func ipAllowed(value, whitelist string) bool {
	ip := net.ParseIP(value)
	if ip == nil {
		return false
	}
	for _, item := range strings.Split(whitelist, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if exact := net.ParseIP(item); exact != nil && exact.Equal(ip) {
			return true
		}
		if _, network, err := net.ParseCIDR(item); err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}
