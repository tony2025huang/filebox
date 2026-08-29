package httpapi

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"github.com/skip2/go-qrcode"
	"golang.org/x/crypto/bcrypt"

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
	JWTExpiry       time.Duration
	TrustedProxies  []*net.IPNet
	Logger          *srvlog.Logger
	Static          fs.FS
}

// Server 组合持久化存储与 HTTP API 配置。
// Server combines the persistent store with the HTTP API configuration.
type Server struct {
	store  *store.Store
	config Config
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
}

type ipACLRequest struct {
	Enabled   bool   `json:"enabled"`
	Whitelist string `json:"whitelist"`
}

type uploadInitRequest struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	ChunkSize int64  `json:"chunkSize"`
	Mime      string `json:"mime"`
	Resolve   string `json:"resolve"`
}

type uploadCompleteRequest struct {
	SHA256 string `json:"sha256"`
	MD5    string `json:"md5"`
}

type createUserRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	Role       string `json:"role"`
	QuotaBytes int64  `json:"quotaBytes"`
}

type updateUserRequest struct {
	Role       *string `json:"role"`
	QuotaBytes *int64  `json:"quotaBytes"`
	Disabled   *bool   `json:"disabled"`
	Password   string  `json:"password"`
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
	HasFavicon      bool   `json:"hasFavicon"`
	HasLoginLogo    bool   `json:"hasLoginLogo"`
	HasMainLogo     bool   `json:"hasMainLogo"`
	DefaultLang     string `json:"defaultLang"`
	ThemeColor      string `json:"themeColor"`
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
	// NewServer 创建 API 服务，并为未指定的 JWT 有效期设置七天默认值。
	// NewServer creates the API server and defaults an unspecified JWT lifetime to seven days.
	if config.JWTExpiry <= 0 {
		config.JWTExpiry = 7 * 24 * time.Hour
	}
	return &Server{store: db, config: config}
}

func (s *Server) Handler() http.Handler {
	// Handler 注册公开、登录保护和管理员保护的路由，并包装 SPA 与安全响应头。
	// Handler registers public, authenticated, and admin-only routes, then wraps them with SPA and security handling.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/login", s.login)
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
	mux.HandleFunc("PUT /api/files/{taskID}/chunks/{index}", s.requireAuth(s.uploadChunk))
	mux.HandleFunc("POST /api/files/{taskID}/complete", s.requireAuth(s.completeUpload))
	mux.HandleFunc("GET /api/files/{id}/download", s.requireAuth(s.download))
	mux.HandleFunc("DELETE /api/files/{id}", s.requireAuth(s.deleteFile))
	mux.HandleFunc("GET /api/logs", s.requireAuth(s.listLogs))
	mux.HandleFunc("GET /api/logs/actions", s.requireAuth(s.logActions))

	mux.HandleFunc("GET /api/admin/users", s.requireAdmin(s.listUsers))
	mux.HandleFunc("POST /api/admin/users", s.requireAdmin(s.createUser))
	mux.HandleFunc("PUT /api/admin/users/{id}", s.requireAdmin(s.updateUser))
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

	return s.securityHeaders(s.spa(mux))
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; img-src 'self' data:; connect-src 'self'; frame-src 'self'")
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
	if languageSettings, err := s.store.GetLogSettings(context.Background()); err == nil && isValidLang(languageSettings.DefaultLang) {
		defaultLang = languageSettings.DefaultLang
		themeColor = languageSettings.ThemeColor
	}
	return brandResponse{
		SiteTitle:       title,
		SiteDescription: settings.Description,
		ICPText:         settings.ICP,
		PoliceText:      settings.Police,
		HasFavicon:      s.brandAssetExists(brandAssets["favicon"], settings.Favicon),
		HasLoginLogo:    s.brandAssetExists(brandAssets["login-logo"], settings.LoginLogo),
		HasMainLogo:     s.brandAssetExists(brandAssets["main-logo"], settings.MainLogo),
		DefaultLang:     defaultLang,
		ThemeColor:      themeColor,
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
	return map[string]string{store.BrandTitleKey: "", store.BrandDescriptionKey: "", store.BrandICPKey: "", store.BrandPoliceKey: "", store.BrandFaviconKey: "", store.BrandLoginLogoKey: "", store.BrandMainLogoKey: ""}
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
			secret, err := s.decryptTOTPSecret(user.TOTPSecret)
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

func (s *Server) recordIPFailure(r *http.Request, settings store.LogSettings) {
	if err := s.store.RecordIPFailure(r.Context(), s.requestIP(r), settings.IPLockWindowMinutes, settings.IPLockThreshold, settings.IPAutoUnlockEnabled, settings.IPUnlockMinutes); err != nil {
		log.Printf("record ip failure: %v", err)
	}
}

// changePassword verifies the old credential, enforces the current policy, and rotates the JWT.
// changePassword 校验旧密码、执行当前策略并重新签发 JWT。
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var input changePasswordRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	user := currentUser(r.Context())
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
	secret, err := s.decryptTOTPSecret(user.TOTPSecret)
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
	secret, err := s.decryptTOTPSecret(user.TOTPSecret)
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
	sealed := gcm.Seal(nonce, nonce, []byte(secret), nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (s *Server) decryptTOTPSecret(value string) (string, error) {
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
	secret, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], nil)
	return string(secret), err
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
	var input languageRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	if !isValidLang(input.Language) {
		writeError(w, http.StatusBadRequest, "语言设置无效")
		return
	}
	user, err := s.store.UpdateUserLanguage(r.Context(), currentUser(r.Context()).ID, input.Language)
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

func (s *Server) uploadInit(w http.ResponseWriter, r *http.Request) {
	// uploadInit 先校验文件名、大小、同名处理和磁盘空间，再以事务预留用户配额。
	// uploadInit validates the name, size, conflict mode, and disk space before transactionally reserving user quota.
	user := currentUser(r.Context())
	var input uploadInitRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	displayName, err := validateUploadName(input.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "文件名包含非法字符，禁止上传")
		return
	}
	name, err := sanitizeName(displayName)
	if err != nil || input.Size < 0 || input.Size > s.config.MaxFileSize {
		writeError(w, http.StatusBadRequest, "文件名或文件大小无效")
		return
	}
	if input.Resolve != "" && input.Resolve != "overwrite" && input.Resolve != "rename" {
		writeError(w, http.StatusBadRequest, "冲突处理方式无效")
		return
	}
	now := time.Now().UTC()
	relativeDir := filepath.Join("files", strconv.FormatInt(user.ID, 10), now.Format("06"), now.Format("01"))
	conflict, conflictErr := s.store.FindUploadConflict(r.Context(), user.ID, relativeDir, name)
	if conflictErr != nil && !errors.Is(conflictErr, store.ErrNotFound) {
		log.Printf("find upload conflict: %v", conflictErr)
		writeError(w, http.StatusInternalServerError, "检查同名文件失败")
		return
	}
	if conflictErr == nil && input.Resolve == "" {
		writeErrorData(w, http.StatusConflict, "同名文件已存在", map[string]any{
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
			writeError(w, http.StatusInternalServerError, "无法检查系统存储空间")
			return
		}
		if free < s.config.MinFreeSpace {
			writeErrorData(w, http.StatusServiceUnavailable, "系统存储空间不足，暂时禁止上传", map[string]string{"code": "DISK_FULL"})
			return
		}
	}
	chunkSize := input.Size
	if chunkSize == 0 {
		chunkSize = 1
	}
	if input.ChunkSize > 0 && input.ChunkSize != input.Size {
		writeError(w, http.StatusBadRequest, "阶段一仅支持单分片上传")
		return
	}
	taskID, err := randomID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建上传任务失败")
		return
	}
	mimeType := strings.TrimSpace(input.Mime)
	if mimeType == "" {
		mimeType = mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	task := store.UploadTask{ID: taskID, UserID: user.ID, Name: name, Size: input.Size, ChunkSize: chunkSize, TotalChunks: 1, Status: "pending", Mime: mimeType, StorageDir: relativeDir, Resolve: input.Resolve}
	if err := s.store.CreateUploadTask(r.Context(), task); err != nil {
		if errors.Is(err, store.ErrQuota) {
			writeError(w, http.StatusForbidden, "超出用户配额")
			return
		}
		log.Printf("create upload task: %v", err)
		writeError(w, http.StatusInternalServerError, "创建上传任务失败")
		return
	}
	writeData(w, http.StatusOK, "上传任务已创建", map[string]any{"taskId": task.ID, "chunkSize": task.ChunkSize, "totalChunks": task.TotalChunks})
}

func (s *Server) uploadChunk(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	taskID := r.PathValue("taskID")
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || index != 0 {
		writeError(w, http.StatusBadRequest, "阶段一仅支持分片 0")
		return
	}
	task, err := s.store.GetUploadTask(r.Context(), taskID)
	if errors.Is(err, store.ErrNotFound) || task.UserID != user.ID || task.Status != "pending" {
		writeError(w, http.StatusNotFound, "上传任务不存在")
		return
	}
	if r.ContentLength > task.Size && r.ContentLength >= 0 {
		writeError(w, http.StatusRequestEntityTooLarge, "上传内容超过声明大小")
		return
	}
	tmpDir := filepath.Join(s.config.DataDir, "tmp", task.ID)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "无法准备上传空间")
		return
	}
	path := filepath.Join(tmpDir, "0")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法写入上传内容")
		return
	}
	limited := io.LimitReader(r.Body, task.Size+1)
	written, copyErr := io.Copy(file, limited)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		writeError(w, http.StatusBadRequest, "写入上传内容失败")
		return
	}
	if written != task.Size {
		writeError(w, http.StatusBadRequest, "上传内容大小与声明不一致")
		return
	}
	writeData(w, http.StatusOK, "分片上传成功", map[string]any{"index": 0, "size": written})
}

func (s *Server) completeUpload(w http.ResponseWriter, r *http.Request) {
	// completeUpload 从临时内容计算 SHA-256 与 MD5，并在数据库事务中提交记录和最终文件路径。
	// completeUpload computes SHA-256 and MD5 from temporary content, then commits the record and final path in one transaction.
	user := currentUser(r.Context())
	taskID := r.PathValue("taskID")
	task, err := s.store.GetUploadTask(r.Context(), taskID)
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
	tmpPath := filepath.Join(s.config.DataDir, "tmp", task.ID, "0")
	info, err := os.Stat(tmpPath)
	if err != nil || info.Size() != task.Size {
		writeError(w, http.StatusBadRequest, "上传分片不完整")
		return
	}
	file, err := os.Open(tmpPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法读取上传内容")
		return
	}
	sha := sha256.New()
	md5Hash := md5.New()
	// 双哈希均基于实际落盘前读取的上传内容，客户端校验值仅用于可选比对。
	// Both hashes are computed from the uploaded bytes; client-provided values are optional checks only.
	if _, err := io.Copy(io.MultiWriter(sha, md5Hash), file); err != nil {
		file.Close()
		writeError(w, http.StatusInternalServerError, "计算文件校验值失败")
		return
	}
	file.Close()
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
		now := time.Now().UTC()
		relativeDir = filepath.Join("files", strconv.FormatInt(user.ID, 10), now.Format("06"), now.Format("01"))
	}
	finalPath := ""
	cleanup := true
	defer func() {
		if cleanup && finalPath != "" {
			_ = os.Remove(finalPath)
		}
		_ = os.RemoveAll(filepath.Join(s.config.DataDir, "tmp", task.ID))
	}()
	fileRecord := store.File{UserID: user.ID, Name: task.Name, StoredName: storedName, Size: task.Size, Mime: task.Mime, SHA256: shaHex, MD5: md5Hex, StoragePath: relativeDir}
	completed, err := s.store.CompleteUploadWithPlacement(r.Context(), task, fileRecord, func(storagePath string) error {
		finalPath = filepath.Join(s.config.DataDir, storagePath)
		if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
			return err
		}
		if _, err := os.Stat(finalPath); err == nil {
			return fmt.Errorf("storage path already exists")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return os.Rename(tmpPath, finalPath)
	})
	if err != nil {
		log.Printf("complete upload: %v", err)
		auditReason = "save_failed"
		serviceReason = "save_failed"
		writeError(w, http.StatusInternalServerError, "保存文件记录失败")
		return
	}
	cleanup = false
	auditResult, auditReason = "success", ""
	serviceResult, serviceReason = "success", ""
	writeData(w, http.StatusOK, "上传完成", publicFile(completed))
}

func (s *Server) listFiles(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
	page, pageSize := pagination(r)
	files, total, err := s.store.ListFiles(r.Context(), user.ID, user.Role == "admin", strings.TrimSpace(r.URL.Query().Get("keyword")), page, pageSize)
	if err != nil {
		log.Printf("list files: %v", err)
		writeError(w, http.StatusInternalServerError, "获取文件列表失败")
		return
	}
	items := make([]map[string]any, 0, len(files))
	for _, file := range files {
		items = append(items, publicFile(file))
	}
	s.serviceEvent(r, "file_list", user.Username, "result=success page=%d page_size=%d total=%d", page, pageSize, total)
	writeData(w, http.StatusOK, "获取成功", map[string]any{"items": items, "page": page, "pageSize": pageSize, "total": total})
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

func (s *Server) deleteFile(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r.Context())
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
	user, _ := s.store.GetUserByUsername(input.Username)
	admin := currentUser(r.Context())
	s.serviceEvent(r, "user_create", admin.Username, "target=%s role=%s result=success", input.Username, input.Role)
	writeData(w, http.StatusCreated, "用户已创建", publicUser(user))
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
	secret := ""
	if input.Enabled {
		randomSecret := make([]byte, 20)
		if _, err := rand.Read(randomSecret); err != nil {
			writeError(w, http.StatusInternalServerError, "生成动态验证密钥失败")
			return
		}
		plain := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(randomSecret)
		secret, err = s.encryptTOTPSecret(plain)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "保存动态验证密钥失败")
			return
		}
	}
	if err := s.store.SetTOTP(r.Context(), user.ID, secret, false); err != nil {
		writeError(w, http.StatusInternalServerError, "保存动态验证设置失败")
		return
	}
	updated, _ := s.store.GetUser(id)
	s.serviceEvent(r, "totp_update", admin.Username, "target=%s enabled=%t result=success", user.Username, input.Enabled)
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
		"users": stats["users"],
		"files": stats["files"],
		"bytes": stats["bytes"],
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
	logs, total, err := s.store.ListAuditLogs(r.Context(), userID, strings.TrimSpace(r.URL.Query().Get("action")), strings.TrimSpace(r.URL.Query().Get("result")), strings.TrimSpace(r.URL.Query().Get("keyword")), page, pageSize)
	if err != nil {
		log.Printf("list audit logs: %v", err)
		writeError(w, http.StatusInternalServerError, "获取日志失败")
		return
	}
	s.serviceEvent(r, "log_list", user.Username, "target=logs result=success page=%d page_size=%d total=%d", page, pageSize, total)
	writeData(w, http.StatusOK, "获取成功", map[string]any{"items": logs, "page": page, "pageSize": pageSize, "total": total})
}

func (s *Server) logActions(w http.ResponseWriter, _ *http.Request) {
	writeData(w, http.StatusOK, "获取成功", []string{"login", "upload", "download", "share", "share_view", "share_download"})
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
	// recordAudit 统一写入来源 IP 和业务结果；清理由存储层按留存设置惰性执行。
	// recordAudit centralizes source-IP and outcome recording; the store lazily prunes records by retention settings.
	if err := s.store.AddAuditLog(r.Context(), userID, username, action, target, s.requestIP(r), result, reason); err != nil {
		log.Printf("write audit log: %v", err)
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
	return map[string]any{"id": user.ID, "username": user.Username, "role": user.Role, "language": user.Language, "quotaBytes": user.QuotaBytes, "usedBytes": user.UsedBytes, "disabled": user.Disabled, "mustChangePassword": user.MustChangePassword, "totpEnabled": user.TOTPEnabled, "ipAclEnabled": user.IPACLEnabled, "ipWhitelist": user.IPWhitelist, "createdAt": user.CreatedAt}
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
	remote := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		remote = host
	}
	remoteIP := net.ParseIP(remote)
	if remoteIP == nil || len(s.config.TrustedProxies) == 0 || !trustedIP(remoteIP, s.config.TrustedProxies) {
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
