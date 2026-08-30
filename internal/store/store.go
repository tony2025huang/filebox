package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

// ErrNotFound 表示请求的用户、文件或上传任务不存在。
// ErrNotFound indicates that the requested user, file, or upload task does not exist.
var ErrNotFound = errors.New("not found")

// ErrConflict 表示资源与现有记录发生冲突。
// ErrConflict indicates that a resource conflicts with an existing record.
var ErrConflict = errors.New("conflict")

// ErrQuota 表示操作会超过用户配额。
// ErrQuota indicates that an operation would exceed the user's quota.
var ErrQuota = errors.New("quota exceeded")

// QuotaError 携带配额明细，供 API 层向用户展示已用/配额/文件大小/差额。
// QuotaError carries quota details so the API layer can surface used/quota/file size/shortfall.
type QuotaError struct {
	UsedBytes  int64
	QuotaBytes int64
	FileSize   int64
}

func (e *QuotaError) Error() string { return ErrQuota.Error() }
func (e *QuotaError) Unwrap() error { return ErrQuota }

// ErrNotEmpty 表示目录非空，禁止删除。
// ErrNotEmpty indicates that a directory is not empty and cannot be deleted.
var ErrNotEmpty = errors.New("directory not empty")

const (
	// BrandTitleKey 是网站标题的设置键。
	// BrandTitleKey is the settings key for the site title.
	BrandTitleKey = "brand_title"
	// BrandDescriptionKey 是 SEO 描述的设置键。
	// BrandDescriptionKey is the settings key for the SEO description.
	BrandDescriptionKey = "brand_description"
	// BrandICPKey 是 ICP 备案文本的设置键。
	// BrandICPKey is the settings key for ICP filing text.
	BrandICPKey = "brand_icp"
	// BrandPoliceKey 是公安备案文本的设置键。
	// BrandPoliceKey is the settings key for public-security filing text.
	BrandPoliceKey = "brand_police"
	// BrandCopyrightKey 是页脚版权文本的设置键。
	// BrandCopyrightKey is the settings key for the footer copyright text.
	BrandCopyrightKey = "brand_copyright"
	// BrandFaviconKey 是 favicon 资源文件名的设置键。
	// BrandFaviconKey is the settings key for the favicon asset filename.
	BrandFaviconKey = "brand_favicon"
	// BrandLoginLogoKey 是登录页 logo 资源文件名的设置键。
	// BrandLoginLogoKey is the settings key for the login-logo asset filename.
	BrandLoginLogoKey = "brand_login_logo"
	// BrandMainLogoKey 是主页 logo 资源文件名的设置键。
	// BrandMainLogoKey is the settings key for the main-logo asset filename.
	BrandMainLogoKey = "brand_main_logo"
	// DefaultThemeColor 是界面主题色的内置默认值。
	// DefaultThemeColor is the built-in default interface theme color.
	DefaultThemeColor = "#1b998b"
)

var themeColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{3}([0-9a-fA-F]{3})?$`)

// Store 管理 SQLite 连接、数据目录以及文件元数据的持久化操作。
// Store manages the SQLite connection, data directory, and file-metadata persistence.
type Store struct {
	DB      *sql.DB
	DataDir string
}

// User 表示账户、角色、配额和登录锁定状态。
// User represents account, role, quota, and login-lock state.
type User struct {
	ID                 int64  `json:"id"`
	Username           string `json:"username"`
	PasswordHash       string `json:"-"`
	Role               string `json:"role"`
	Language           string `json:"language"`
	QuotaBytes         int64  `json:"quotaBytes"`
	UsedBytes          int64  `json:"usedBytes"`
	FolderCount        int64  `json:"folderCount"`
	FileCount          int64  `json:"fileCount"`
	Disabled           bool   `json:"disabled"`
	FailedAttempts     int    `json:"-"`
	LockedUntil        string `json:"-"`
	MustChangePassword bool   `json:"-"`
	TOTPSecret         string `json:"-"`
	TOTPEnabled        bool   `json:"-"`
	LastUsedTOTP       string `json:"-"`
	IPACLEnabled       bool   `json:"-"`
	IPWhitelist        string `json:"-"`
	ReadOnlyFrom       string `json:"readOnlyFrom"`
	ReadOnlyUntil      string `json:"readOnlyUntil"`
	CreatedAt          string `json:"createdAt"`
	UpdatedAt          string `json:"updatedAt"`
}

// File 表示已上传文件的公开元数据和内部存储定位信息。
// File represents uploaded-file metadata and its internal storage location.
type File struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"userId"`
	Name        string `json:"name"`
	StoredName  string `json:"-"`
	Size        int64  `json:"size"`
	Mime        string `json:"mime"`
	SHA256      string `json:"sha256"`
	MD5         string `json:"md5"`
	Status      string `json:"status"`
	StoragePath string `json:"-"`
	CreatedAt   string `json:"createdAt"`
	DeletedAt   string `json:"deletedAt,omitempty"`
}

// UploadTask 表示单文件单分片上传流程中的暂存任务。
// UploadTask represents a temporary task in the single-file, single-chunk upload flow.
type UploadTask struct {
	ID           string
	UserID       int64
	CollectionID int64
	Remark       string
	Name         string
	Size         int64
	ChunkSize    int64
	TotalChunks  int
	Status       string
	Mime         string
	StorageDir   string
	Resolve      string
}

// UploadCollection represents an owner-controlled public upload endpoint.
// UploadCollection 表示创建者控制的公开上传收集链接。
type UploadCollection struct {
	ID           int64  `json:"id"`
	CreatedBy    int64  `json:"createdBy"`
	Name         string `json:"name"`
	Token        string `json:"token"`
	ExpiresAt    string `json:"expiresAt"`
	MaxUploads   int    `json:"maxUploads"`
	UploadCount  int    `json:"uploadCount"`
	MaxFileBytes int64  `json:"maxFileBytes"`
	RevokedAt    string `json:"revokedAt,omitempty"`
	CreatedAt    string `json:"createdAt"`
}

// UploadCollectionFile records the source collection for an uploaded file.
// UploadCollectionFile 记录文件来自哪个收集链接以及访客备注。
type UploadCollectionFile struct {
	ID           int64  `json:"id"`
	CollectionID int64  `json:"collectionId"`
	FileID       int64  `json:"fileId"`
	OriginalName string `json:"originalName"`
	Remark       string `json:"remark"`
	CreatedAt    string `json:"createdAt"`
	File         File   `json:"file"`
}

var (
	ErrCollectionLimit   = errors.New("collection upload limit reached")
	ErrCollectionExpired = errors.New("collection expired")
	ErrCollectionRevoked = errors.New("collection revoked")
)

// ChunkInfo 表示一个已写入临时目录的上传分片。
// ChunkInfo describes an uploaded chunk recorded for resumable uploads.
type ChunkInfo struct {
	Size   int64
	SHA256 string
}

// Share 表示一个文件分享链接及其访问限制。
// Share represents a file sharing link and its access limits.
type Share struct {
	ID            int64  `json:"id"`
	FileID        int64  `json:"fileId"`
	FileName      string `json:"fileName,omitempty"`
	Token         string `json:"token"`
	CreatedBy     int64  `json:"createdBy"`
	ExpiresAt     string `json:"expiresAt"`
	DownloadCount int64  `json:"downloadCount"`
	MaxDownloads  int    `json:"maxDownloads"`
	RevokedAt     string `json:"revokedAt,omitempty"`
	CreatedAt     string `json:"createdAt"`
}

// Folder 表示一个用户自定义目录（v011：移除自动年月层后的目录模型）。
// Folder represents a user-defined directory (v011: directory model after removing the automatic year/month layer).
type Folder struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"userId"`
	ParentID  *int64 `json:"parentId,omitempty"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	CreatedAt string `json:"createdAt"`
}

// AuditLog 表示一次登录、上传或下载等操作的审计记录。
// AuditLog represents an audit record for a login, upload, download, or related operation.
type AuditLog struct {
	ID           int64  `json:"id"`
	UserID       *int64 `json:"userId,omitempty"`
	ShareOwnerID *int64 `json:"shareOwnerId,omitempty"`
	Username     string `json:"username"`
	Action       string `json:"action"`
	Target       string `json:"target"`
	IP           string `json:"ip"`
	Result       string `json:"result"`
	Reason       string `json:"reason,omitempty"`
	CreatedAt    string `json:"createdAt"`
}

// LogSettings 定义日志留存和登录失败锁定策略。
// LogSettings defines log retention and failed-login lockout policy.
type LogSettings struct {
	LogRetentionDays    int    `json:"logRetentionDays"`
	LockThreshold       int    `json:"lockThreshold"`
	AutoUnlockEnabled   bool   `json:"autoUnlockEnabled"`
	AutoUnlockMinutes   int    `json:"autoUnlockMinutes"`
	DefaultLang         string `json:"defaultLang"`
	ThemeColor          string `json:"themeColor"`
	PasswordMinLength   int    `json:"passwordMinLength"`
	PasswordComplexity  int    `json:"passwordComplexity"`
	IPLockWindowMinutes int    `json:"ipLockWindowMinutes"`
	IPLockThreshold     int    `json:"ipLockThreshold"`
	IPAutoUnlockEnabled bool   `json:"ipAutoUnlockEnabled"`
	IPUnlockMinutes     int    `json:"ipUnlockMinutes"`
	RegisterEnabled     bool   `json:"registerEnabled"`
	UploadRateLimit     int64  `json:"uploadRateLimit"`
	TrustProxy          bool   `json:"trustProxy"`
}

// IPLock represents a source-IP failure window and its optional lock deadline.
// IPLock 表示来源 IP 的失败窗口及可选锁定截止时间。
type IPLock struct {
	IP              string `json:"ip"`
	FailedCount     int    `json:"failedCount"`
	WindowStartedAt string `json:"windowStartedAt"`
	LockedUntil     string `json:"lockedUntil"`
	LockedNow       bool   `json:"lockedNow"`
	AutoUnlock      bool   `json:"autoUnlock"`
}

// UserLock is the lock state exposed to administrators without password data.
// UserLock 是提供给管理员的用户锁定状态，不包含密码数据。
type UserLock struct {
	ID             int64  `json:"id"`
	Username       string `json:"username"`
	FailedAttempts int    `json:"failedAttempts"`
	LockedUntil    string `json:"lockedUntil"`
	LockedNow      bool   `json:"lockedNow"`
}

// BrandSettings 保存标题、描述、备案文本及品牌资源文件名。
// BrandSettings stores the title, description, filing text, and branding asset filenames.
type BrandSettings struct {
	Title       string
	Description string
	ICP         string
	Police      string
	Copyright   string
	Favicon     string
	LoginLogo   string
	MainLogo    string
}

func Open(dataDir string) (*Store, error) {
	// Open 创建 data/files、data/tmp 和 data/brand 目录，初始化 SQLite schema 后返回存储层。
	// Open creates data/files, data/tmp, and data/brand, migrates the SQLite schema, and returns the store.
	if err := os.MkdirAll(filepath.Join(dataDir, "files"), 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "tmp"), 0o755); err != nil {
		return nil, fmt.Errorf("create temp directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "brand"), 0o755); err != nil {
		return nil, fmt.Errorf("create brand directory: %w", err)
	}
	dbPath := filepath.Join(dataDir, "filebox.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	store := &Store{DB: db, DataDir: dataDir}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

// Close 关闭底层 SQLite 连接。
// Close closes the underlying SQLite connection.
func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) migrate() error {
	const schema = `
PRAGMA foreign_keys = ON;
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL CHECK(role IN ('admin', 'user')),
  language TEXT NOT NULL DEFAULT '',
  quota_bytes INTEGER NOT NULL DEFAULT 107374182400,
  used_bytes INTEGER NOT NULL DEFAULT 0,
  disabled INTEGER NOT NULL DEFAULT 0,
  failed_attempts INTEGER NOT NULL DEFAULT 0,
  locked_until TEXT,
  must_change_password INTEGER NOT NULL DEFAULT 0,
  totp_secret TEXT,
  totp_enabled INTEGER NOT NULL DEFAULT 0,
  last_used_totp TEXT,
  ip_acl_enabled INTEGER NOT NULL DEFAULT 0,
  ip_whitelist TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS files (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  stored_name TEXT NOT NULL,
  size INTEGER NOT NULL,
  mime TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  md5 TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('uploading', 'ready', 'deleted')),
  storage_path TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL,
  deleted_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_files_user_status ON files(user_id, status, created_at DESC);
CREATE TABLE IF NOT EXISTS upload_tasks (
  id TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  collection_id INTEGER NOT NULL DEFAULT 0,
  remark TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  size INTEGER NOT NULL,
  mime TEXT NOT NULL,
  chunk_size INTEGER NOT NULL,
  total_chunks INTEGER NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_upload_tasks_user_status ON upload_tasks(user_id, status);
CREATE INDEX IF NOT EXISTS idx_upload_tasks_user_status_created ON upload_tasks(user_id, status, created_at);
CREATE INDEX IF NOT EXISTS idx_upload_tasks_status_created ON upload_tasks(status, created_at);
CREATE TABLE IF NOT EXISTS chunks (
  task_id TEXT NOT NULL REFERENCES upload_tasks(id) ON DELETE CASCADE,
  idx INTEGER NOT NULL,
  size INTEGER NOT NULL,
  sha256 TEXT NOT NULL,
  PRIMARY KEY(task_id, idx)
);
CREATE TABLE IF NOT EXISTS folders (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  parent_id INTEGER REFERENCES folders(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  path TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(user_id, path)
);
CREATE INDEX IF NOT EXISTS idx_folders_user ON folders(user_id);
CREATE TABLE IF NOT EXISTS audit_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
  username TEXT NOT NULL,
  action TEXT NOT NULL,
  target TEXT NOT NULL,
  ip TEXT NOT NULL,
  result TEXT NOT NULL CHECK(result IN ('success', 'failure')),
  reason TEXT,
  share_owner_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user_created ON audit_logs(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created ON audit_logs(created_at);
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS ip_failures (
  ip TEXT PRIMARY KEY,
  failed_count INTEGER NOT NULL DEFAULT 0,
  window_started_at TEXT NOT NULL,
  locked_until TEXT
);
`
	if _, err := s.DB.Exec(schema); err != nil {
		return err
	}
	if err := s.migrateUsersSchema(); err != nil {
		return err
	}
	if err := s.migrateUploadTasksSchema(); err != nil {
		return err
	}
	if err := s.migrateSettings(); err != nil {
		return err
	}
	if err := s.migrateFilesSchema(); err != nil {
		return err
	}
	if err := s.migrateSharesSchema(); err != nil {
		return err
	}
	if err := s.migrateAuditLogsSchema(); err != nil {
		return err
	}
	if err := s.migrateCollectionsSchema(); err != nil {
		return err
	}
	return s.migrateSyncSchema()
}

// migrateCollectionsSchema creates collection tables and adds the optional task link.
// migrateCollectionsSchema 创建收集链接表，并为旧上传任务补充可选关联字段。
func (s *Store) migrateCollectionsSchema() error {
	columns, err := tableColumns(s.DB, "upload_tasks")
	if err != nil {
		return err
	}
	if !columns["collection_id"] {
		if _, err := s.DB.Exec("ALTER TABLE upload_tasks ADD COLUMN collection_id INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
	}
	if !columns["remark"] {
		if _, err := s.DB.Exec("ALTER TABLE upload_tasks ADD COLUMN remark TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	_, err = s.DB.Exec(`CREATE TABLE IF NOT EXISTS upload_collections (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  created_by INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  token TEXT NOT NULL UNIQUE,
  expires_at TEXT NOT NULL,
  max_uploads INTEGER NOT NULL DEFAULT 0,
  upload_count INTEGER NOT NULL DEFAULT 0,
  max_file_bytes INTEGER NOT NULL DEFAULT 0,
  revoked_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_upload_collections_created_by ON upload_collections(created_by, created_at DESC);
CREATE TABLE IF NOT EXISTS upload_collection_files (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  collection_id INTEGER NOT NULL REFERENCES upload_collections(id) ON DELETE CASCADE,
  file_id INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
  original_name TEXT NOT NULL,
  remark TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_upload_collection_files_collection ON upload_collection_files(collection_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_upload_collection_files_file ON upload_collection_files(file_id);`)
	return err
}

// migrateSharesSchema creates sharing records after any legacy files-table rebuild has finished.
// migrateSharesSchema 在旧 files 表迁移完成后创建分享表，避免外键阻断升级。
func (s *Store) migrateSharesSchema() error {
	if _, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS shares (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  file_id INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
  token TEXT NOT NULL UNIQUE,
  created_by INTEGER NOT NULL,
  expires_at TEXT NOT NULL,
  download_count INTEGER NOT NULL DEFAULT 0,
  max_downloads INTEGER NOT NULL DEFAULT 0,
  revoked_at TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_shares_file ON shares(file_id);
CREATE INDEX IF NOT EXISTS idx_shares_created_by ON shares(created_by, created_at DESC);`); err != nil {
		return err
	}
	columns, err := tableColumns(s.DB, "shares")
	if err != nil {
		return err
	}
	if !columns["revoked_at"] {
		if _, err := s.DB.Exec("ALTER TABLE shares ADD COLUMN revoked_at TEXT"); err != nil {
			return err
		}
	}
	return nil
}

// migrateAuditLogsSchema adds share ownership to existing databases so anonymous share events can be shown to their creator.
// migrateAuditLogsSchema 为旧数据库补充分享者归属字段，使匿名分享事件可以展示给创建者。
func (s *Store) migrateAuditLogsSchema() error {
	columns, err := tableColumns(s.DB, "audit_logs")
	if err != nil {
		return err
	}
	if !columns["share_owner_id"] {
		if _, err := s.DB.Exec("ALTER TABLE audit_logs ADD COLUMN share_owner_id INTEGER REFERENCES users(id) ON DELETE SET NULL"); err != nil {
			return err
		}
	}
	_, err = s.DB.Exec("CREATE INDEX IF NOT EXISTS idx_audit_logs_share_owner_created ON audit_logs(share_owner_id, created_at DESC)")
	return err
}

func (s *Store) migrateUsersSchema() error {
	columns, err := tableColumns(s.DB, "users")
	if err != nil {
		return err
	}
	if !columns["failed_attempts"] {
		if _, err := s.DB.Exec("ALTER TABLE users ADD COLUMN failed_attempts INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
	}
	if !columns["locked_until"] {
		if _, err := s.DB.Exec("ALTER TABLE users ADD COLUMN locked_until TEXT"); err != nil {
			return err
		}
	}
	if !columns["language"] {
		if _, err := s.DB.Exec("ALTER TABLE users ADD COLUMN language TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	for _, definition := range []struct{ name, sql string }{
		{"must_change_password", "ALTER TABLE users ADD COLUMN must_change_password INTEGER NOT NULL DEFAULT 0"},
		{"totp_secret", "ALTER TABLE users ADD COLUMN totp_secret TEXT"},
		{"totp_enabled", "ALTER TABLE users ADD COLUMN totp_enabled INTEGER NOT NULL DEFAULT 0"},
		{"last_used_totp", "ALTER TABLE users ADD COLUMN last_used_totp TEXT"},
		{"ip_acl_enabled", "ALTER TABLE users ADD COLUMN ip_acl_enabled INTEGER NOT NULL DEFAULT 0"},
		{"ip_whitelist", "ALTER TABLE users ADD COLUMN ip_whitelist TEXT NOT NULL DEFAULT ''"},
		{"read_only_from", "ALTER TABLE users ADD COLUMN read_only_from TEXT"},
		{"read_only_until", "ALTER TABLE users ADD COLUMN read_only_until TEXT"},
	} {
		if !columns[definition.name] {
			if _, err := s.DB.Exec(definition.sql); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) migrateUploadTasksSchema() error {
	columns, err := tableColumns(s.DB, "upload_tasks")
	if err != nil {
		return err
	}
	if !columns["storage_dir"] {
		if _, err := s.DB.Exec("ALTER TABLE upload_tasks ADD COLUMN storage_dir TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	if !columns["resolve"] {
		if _, err := s.DB.Exec("ALTER TABLE upload_tasks ADD COLUMN resolve TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) migrateSettings() error {
	defaults := map[string]string{
		"logRetentionDays": "30", "lockThreshold": "5", "autoUnlockEnabled": "true", "autoUnlockMinutes": "5",
		"passwordMinLength": "8", "passwordComplexity": "3",
		"ipLockWindowMinutes": "10", "ipLockThreshold": "50", "ipAutoUnlockEnabled": "true", "ipUnlockMinutes": "30",
		"defaultLang": "zh-CN",
		"theme_color": "",
		BrandTitleKey: "", BrandDescriptionKey: "", BrandICPKey: "", BrandPoliceKey: "", BrandCopyrightKey: "", BrandFaviconKey: "", BrandLoginLogoKey: "", BrandMainLogoKey: "",
		"registerEnabled": "false", "uploadRateLimit": "0", "trustProxy": "false",
	}
	for key, value := range defaults {
		if _, err := s.DB.Exec("INSERT OR IGNORE INTO settings(key, value) VALUES(?, ?)", key, value); err != nil {
			return err
		}
	}
	return nil
}

// SetSettingDefault 仅在设置不存在时写入默认值，供首次部署种子使用。
// SetSettingDefault inserts a setting only when no value has been stored yet.
func (s *Store) SetSettingDefault(ctx context.Context, key, value string) error {
	_, err := s.DB.ExecContext(ctx, "INSERT OR IGNORE INTO settings(key, value) VALUES(?, ?)", key, value)
	return err
}

func (s *Store) GetBrandSettings(ctx context.Context) (BrandSettings, error) {
	// GetBrandSettings 读取品牌文本和自定义资源文件名，缺失值保持为空。
	// GetBrandSettings reads branding text and custom asset filenames, leaving missing values empty.
	settings := BrandSettings{}
	rows, err := s.DB.QueryContext(ctx, "SELECT key, value FROM settings WHERE key IN (?, ?, ?, ?, ?, ?, ?, ?)", BrandTitleKey, BrandDescriptionKey, BrandICPKey, BrandPoliceKey, BrandCopyrightKey, BrandFaviconKey, BrandLoginLogoKey, BrandMainLogoKey)
	if err != nil {
		return settings, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return settings, err
		}
		switch key {
		case BrandTitleKey:
			settings.Title = value
		case BrandDescriptionKey:
			settings.Description = value
		case BrandICPKey:
			settings.ICP = value
		case BrandPoliceKey:
			settings.Police = value
		case BrandCopyrightKey:
			settings.Copyright = value
		case BrandFaviconKey:
			settings.Favicon = value
		case BrandLoginLogoKey:
			settings.LoginLogo = value
		case BrandMainLogoKey:
			settings.MainLogo = value
		}
	}
	return settings, rows.Err()
}

func (s *Store) UpdateBrandSettings(ctx context.Context, values map[string]string) error {
	// UpdateBrandSettings 只接受已知品牌键，并在一个事务中 upsert 设置。
	// UpdateBrandSettings accepts only known branding keys and upserts them in one transaction.
	allowed := map[string]bool{
		BrandTitleKey: true, BrandDescriptionKey: true, BrandICPKey: true, BrandPoliceKey: true, BrandCopyrightKey: true,
		BrandFaviconKey: true, BrandLoginLogoKey: true, BrandMainLogoKey: true,
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for key, value := range values {
		if !allowed[key] {
			tx.Rollback()
			return fmt.Errorf("invalid brand setting")
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, value); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

func (s *Store) migrateFilesSchema() error {
	var tableSQL string
	if err := s.DB.QueryRow("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'files'").Scan(&tableSQL); err != nil {
		return err
	}
	if strings.Contains(strings.ToLower(tableSQL), "storage_path text not null unique") {
		return nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	const schema = `
CREATE TABLE files_new (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  stored_name TEXT NOT NULL,
  size INTEGER NOT NULL,
  mime TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  md5 TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('uploading', 'ready', 'deleted')),
  storage_path TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL,
  deleted_at TEXT
);
INSERT INTO files_new(id, user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at, deleted_at)
  SELECT id, user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at, deleted_at FROM files;
DROP TABLE files;
ALTER TABLE files_new RENAME TO files;
CREATE INDEX IF NOT EXISTS idx_files_user_status ON files(user_id, status, created_at DESC);
`
	if _, err := tx.Exec(schema); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) EnsureAdmin(username, password string, quota int64) error {
	// EnsureAdmin 在首次启动时创建默认管理员；已有同名账户不会被覆盖。
	// EnsureAdmin creates the default administrator on first start without overwriting an existing account.
	var id int64
	err := s.DB.QueryRow("SELECT id FROM users WHERE username = ?", username).Scan(&id)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := s.CreateUser(context.Background(), username, string(hash), "admin", quota); err != nil {
		return err
	}
	_, err = s.DB.Exec("UPDATE users SET must_change_password = 1 WHERE username = ?", username)
	return err
}

// ResetPassword 更新用户密码、强制下次登录改密，并清除登录锁定状态。
// ResetPassword updates a user's password, forces the next login to change it, and clears login locks.
func (s *Store) ResetPassword(username, newHash string) (int64, error) {
	result, err := s.DB.Exec("UPDATE users SET password_hash = ?, must_change_password = 1, failed_attempts = 0, locked_until = NULL, updated_at = ? WHERE username = ?", newHash, time.Now().UTC().Format(time.RFC3339), username)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, ErrNotFound
	}
	return count, nil
}

// ClearIPACL 禁用指定用户的来源 IP 白名单并清空白名单内容。
// ClearIPACL disables a user's source-IP allowlist and clears its entries.
func (s *Store) ClearIPACL(username string) (bool, error) {
	result, err := s.DB.Exec("UPDATE users SET ip_acl_enabled = 0, ip_whitelist = '', updated_at = ? WHERE username = ?", time.Now().UTC().Format(time.RFC3339), username)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

func (s *Store) GetUserByUsername(username string) (User, error) {
	// GetUserByUsername 按唯一用户名读取账户及其登录锁定状态。
	// GetUserByUsername loads an account and its login-lock state by unique username.
	return scanUser(s.DB.QueryRow("SELECT id, username, password_hash, role, language, quota_bytes, used_bytes, disabled, failed_attempts, COALESCE(locked_until, ''), COALESCE(must_change_password, 0), COALESCE(totp_secret, ''), COALESCE(totp_enabled, 0), COALESCE(last_used_totp, ''), COALESCE(ip_acl_enabled, 0), COALESCE(ip_whitelist, ''), COALESCE(read_only_from, ''), COALESCE(read_only_until, ''), created_at, updated_at FROM users WHERE username = ?", username))
}

func (s *Store) GetUser(id int64) (User, error) {
	// GetUser 按账户 ID 读取用户记录。
	// GetUser loads a user record by account ID.
	return scanUser(s.DB.QueryRow("SELECT id, username, password_hash, role, language, quota_bytes, used_bytes, disabled, failed_attempts, COALESCE(locked_until, ''), COALESCE(must_change_password, 0), COALESCE(totp_secret, ''), COALESCE(totp_enabled, 0), COALESCE(last_used_totp, ''), COALESCE(ip_acl_enabled, 0), COALESCE(ip_whitelist, ''), COALESCE(read_only_from, ''), COALESCE(read_only_until, ''), created_at, updated_at FROM users WHERE id = ?", id))
}

func scanUser(row *sql.Row) (User, error) {
	var user User
	var disabled, mustChange, totpEnabled, ipACLEnabled int
	err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.Language, &user.QuotaBytes, &user.UsedBytes, &disabled, &user.FailedAttempts, &user.LockedUntil, &mustChange, &user.TOTPSecret, &totpEnabled, &user.LastUsedTOTP, &ipACLEnabled, &user.IPWhitelist, &user.ReadOnlyFrom, &user.ReadOnlyUntil, &user.CreatedAt, &user.UpdatedAt)
	user.Disabled = disabled != 0
	user.MustChangePassword = mustChange != 0
	user.TOTPEnabled = totpEnabled != 0
	user.IPACLEnabled = ipACLEnabled != 0
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return user, err
}

func (s *Store) CreateUser(ctx context.Context, username, passwordHash, role string, quota int64) error {
	// CreateUser 写入已哈希密码、角色和配额；重复用户名转换为冲突错误。
	// CreateUser stores the hashed password, role, and quota, translating duplicate usernames to a conflict error.
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.DB.ExecContext(ctx, "INSERT INTO users(username, password_hash, role, language, quota_bytes, created_at, updated_at) VALUES(?, ?, ?, '', ?, ?, ?)", username, passwordHash, role, quota, now, now)
	if err != nil {
		if isUniqueError(err) {
			return ErrConflict
		}
		return err
	}
	return nil
}

// UpdateUserLanguage stores the user's language preference and returns the refreshed account.
// UpdateUserLanguage 保存用户语言偏好并返回刷新后的账户信息。
func (s *Store) UpdateUserLanguage(ctx context.Context, id int64, language string) (User, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.DB.ExecContext(ctx, "UPDATE users SET language = ?, updated_at = ? WHERE id = ?", language, now, id)
	if err != nil {
		return User{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return User{}, err
	}
	if count == 0 {
		return User{}, ErrNotFound
	}
	return s.GetUser(id)
}

// UpdateUserReadOnly 设置或清除用户的一次性只读时段。
// UpdateUserReadOnly stores or clears a user's one-time read-only window.
func (s *Store) UpdateUserReadOnly(ctx context.Context, id int64, from, until string) (User, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.DB.ExecContext(ctx, "UPDATE users SET read_only_from = ?, read_only_until = ?, updated_at = ? WHERE id = ?", nullableString(from), nullableString(until), now, id)
	if err != nil {
		return User{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return User{}, err
	}
	if count == 0 {
		return User{}, ErrNotFound
	}
	return s.GetUser(id)
}

func (s *Store) UpdateUser(ctx context.Context, id int64, role *string, quota *int64, disabled *bool, passwordHash *string) error {
	// UpdateUser 更新账户属性，并拒绝低于当前已用空间的配额。
	// UpdateUser changes account attributes and rejects quotas below current usage.
	user, err := s.GetUser(id)
	if err != nil {
		return err
	}
	if role != nil {
		if *role != "admin" && *role != "user" {
			return fmt.Errorf("invalid role")
		}
		user.Role = *role
	}
	if quota != nil {
		if *quota < user.UsedBytes {
			return ErrQuota
		}
		user.QuotaBytes = *quota
	}
	if disabled != nil {
		user.Disabled = *disabled
	}
	if passwordHash != nil {
		user.PasswordHash = *passwordHash
		user.MustChangePassword = true
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.DB.ExecContext(ctx, "UPDATE users SET password_hash = ?, role = ?, quota_bytes = ?, disabled = ?, failed_attempts = 0, locked_until = NULL, must_change_password = ?, updated_at = ? WHERE id = ?", user.PasswordHash, user.Role, user.QuotaBytes, boolInt(user.Disabled), boolInt(user.MustChangePassword), now, id)
	return err
}

// ChangePassword replaces a user's password and clears the forced-change marker.
// ChangePassword 更新用户密码并清除强制改密标记。
func (s *Store) ChangePassword(ctx context.Context, id int64, passwordHash string) error {
	result, err := s.DB.ExecContext(ctx, "UPDATE users SET password_hash = ?, must_change_password = 0, updated_at = ? WHERE id = ?", passwordHash, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

// SetTOTP stores the encrypted secret and activation state for a user.
// SetTOTP 保存用户的加密密钥和启用状态。
func (s *Store) SetTOTP(ctx context.Context, id int64, secret string, enabled bool) error {
	result, err := s.DB.ExecContext(ctx, "UPDATE users SET totp_secret = ?, totp_enabled = ?, last_used_totp = NULL, updated_at = ? WHERE id = ?", nullableString(secret), boolInt(enabled), time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

// ActivateTOTP enables an already stored secret without discarding replay state.
// ActivateTOTP 启用已有密钥，同时保留动态码防重放状态。
func (s *Store) ActivateTOTP(ctx context.Context, id int64) error {
	result, err := s.DB.ExecContext(ctx, "UPDATE users SET totp_enabled = 1, updated_at = ? WHERE id = ?", time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

// ConsumeTOTP atomically rejects replayed counters for sixty seconds.
// ConsumeTOTP 原子记录动态码计数器并在 60 秒内拒绝重放。
func (s *Store) ConsumeTOTP(ctx context.Context, id int64, counter int64, now time.Time) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	var previous string
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(last_used_totp, '') FROM users WHERE id = ?", id).Scan(&previous); err != nil {
		tx.Rollback()
		return false, err
	}
	if parts := strings.SplitN(previous, "|", 2); len(parts) == 2 && parts[0] == strconv.FormatInt(counter, 10) {
		if timestamp, parseErr := strconv.ParseInt(parts[1], 10, 64); parseErr == nil && now.Unix()-timestamp < 60 {
			tx.Rollback()
			return false, nil
		}
	}
	marker := strconv.FormatInt(counter, 10) + "|" + strconv.FormatInt(now.Unix(), 10)
	if _, err := tx.ExecContext(ctx, "UPDATE users SET last_used_totp = ?, updated_at = ? WHERE id = ?", marker, now.UTC().Format(time.RFC3339), id); err != nil {
		tx.Rollback()
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// UpdateIPACL validates are performed by the HTTP layer before persisting this list.
// UpdateIPACL 由 HTTP 层完成格式校验后持久化来源 IP 白名单。
func (s *Store) UpdateIPACL(ctx context.Context, id int64, enabled bool, whitelist string) error {
	result, err := s.DB.ExecContext(ctx, "UPDATE users SET ip_acl_enabled = ?, ip_whitelist = ?, updated_at = ? WHERE id = ?", boolInt(enabled), whitelist, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ResetLoginState(ctx context.Context, id int64) error {
	// ResetLoginState 清零失败次数并解除登录锁定。
	// ResetLoginState clears failed attempts and removes the login lock.
	_, err := s.DB.ExecContext(ctx, "UPDATE users SET failed_attempts = 0, locked_until = NULL, updated_at = ? WHERE id = ?", time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func (s *Store) RecordLoginFailure(ctx context.Context, id int64, threshold int, autoUnlock bool, unlockMinutes int) error {
	// RecordLoginFailure 在事务中累加失败次数，并按策略设置临时或永久锁定。
	// RecordLoginFailure increments failures transactionally and applies a temporary or permanent lock by policy.
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	var attempts int
	if err := tx.QueryRowContext(ctx, "SELECT failed_attempts FROM users WHERE id = ?", id).Scan(&attempts); err != nil {
		tx.Rollback()
		return err
	}
	attempts++
	lockedUntil := ""
	if threshold > 0 && attempts >= threshold {
		if autoUnlock {
			if unlockMinutes < 1 {
				unlockMinutes = 5
			}
			lockedUntil = time.Now().UTC().Add(time.Duration(unlockMinutes) * time.Minute).Format(time.RFC3339)
		} else {
			lockedUntil = "9999-12-31T23:59:59Z"
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var updateErr error
	if lockedUntil == "" {
		_, updateErr = tx.ExecContext(ctx, "UPDATE users SET failed_attempts = ?, updated_at = ? WHERE id = ?", attempts, now, id)
	} else {
		_, updateErr = tx.ExecContext(ctx, "UPDATE users SET failed_attempts = ?, locked_until = ?, updated_at = ? WHERE id = ?", attempts, lockedUntil, now, id)
	}
	if updateErr != nil {
		tx.Rollback()
		return updateErr
	}
	return tx.Commit()
}

// IsIPLocked checks and lazily clears an expired source-IP lock.
// IsIPLocked 检查来源 IP 锁定状态，并惰性清理已过期的锁定。
func (s *Store) IsIPLocked(ctx context.Context, ip string, autoUnlock bool) (bool, error) {
	var lockedUntil string
	err := s.DB.QueryRowContext(ctx, "SELECT COALESCE(locked_until, '') FROM ip_failures WHERE ip = ?", ip).Scan(&lockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if lockedUntil == "" {
		return false, nil
	}
	deadline, err := time.Parse(time.RFC3339, lockedUntil)
	if err != nil {
		return false, nil
	}
	if time.Now().UTC().Before(deadline) {
		return true, nil
	}
	if autoUnlock {
		if _, err := s.DB.ExecContext(ctx, "DELETE FROM ip_failures WHERE ip = ?", ip); err != nil {
			return false, err
		}
	}
	return false, nil
}

// RecordIPFailure increments a sliding source-IP failure window and applies its lock policy.
// RecordIPFailure 累加来源 IP 滑动窗口失败次数并应用锁定策略。
func (s *Store) RecordIPFailure(ctx context.Context, ip string, windowMinutes, threshold int, autoUnlock bool, unlockMinutes int) error {
	now := time.Now().UTC()
	windowStart := now
	count := 0
	var storedStart, lockedUntil string
	err := s.DB.QueryRowContext(ctx, "SELECT failed_count, window_started_at, COALESCE(locked_until, '') FROM ip_failures WHERE ip = ?", ip).Scan(&count, &storedStart, &lockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		// Start a new failure window below.
	} else if err != nil {
		return err
	} else {
		if start, parseErr := time.Parse(time.RFC3339, storedStart); parseErr == nil && now.Sub(start) <= time.Duration(windowMinutes)*time.Minute {
			windowStart = start
		} else {
			count = 0
		}
		if lockedUntil != "" {
			if deadline, parseErr := time.Parse(time.RFC3339, lockedUntil); parseErr == nil && now.Before(deadline) {
				return nil
			}
			lockedUntil = ""
		}
	}
	count++
	if threshold > 0 && count >= threshold {
		if autoUnlock {
			if unlockMinutes < 1 {
				unlockMinutes = 30
			}
			lockedUntil = now.Add(time.Duration(unlockMinutes) * time.Minute).Format(time.RFC3339)
		} else {
			lockedUntil = "9999-12-31T23:59:59Z"
		}
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO ip_failures(ip, failed_count, window_started_at, locked_until) VALUES(?, ?, ?, ?)
ON CONFLICT(ip) DO UPDATE SET failed_count = excluded.failed_count, window_started_at = excluded.window_started_at, locked_until = excluded.locked_until`, ip, count, windowStart.Format(time.RFC3339), nullableString(lockedUntil))
	return err
}

// ResetIPFailure clears all failed-login state for a source IP after successful authentication.
// ResetIPFailure 在登录成功后清除来源 IP 的全部失败状态。
func (s *Store) ResetIPFailure(ctx context.Context, ip string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM ip_failures WHERE ip = ?", ip)
	return err
}

// ListIPLocks returns active failure windows and currently locked source IPs.
// ListIPLocks 返回有效失败窗口和当前锁定的来源 IP。
func (s *Store) ListIPLocks(ctx context.Context, windowMinutes int, autoUnlock bool) ([]IPLock, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT ip, failed_count, window_started_at, COALESCE(locked_until, '') FROM ip_failures WHERE failed_count > 0 ORDER BY window_started_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now().UTC()
	locks := make([]IPLock, 0)
	for rows.Next() {
		var lock IPLock
		if err := rows.Scan(&lock.IP, &lock.FailedCount, &lock.WindowStartedAt, &lock.LockedUntil); err != nil {
			return nil, err
		}
		start, startErr := time.Parse(time.RFC3339, lock.WindowStartedAt)
		deadline, deadlineErr := time.Parse(time.RFC3339, lock.LockedUntil)
		lock.LockedNow = deadlineErr == nil && now.Before(deadline)
		lock.AutoUnlock = autoUnlock
		if !lock.LockedNow && (startErr != nil || now.Sub(start) > time.Duration(windowMinutes)*time.Minute) {
			continue
		}
		locks = append(locks, lock)
	}
	return locks, rows.Err()
}

// DeleteIPFailure removes one administrator-selected IP failure record.
// DeleteIPFailure 删除管理员选中的来源 IP 失败记录。
func (s *Store) DeleteIPFailure(ctx context.Context, ip string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM ip_failures WHERE ip = ?", ip)
	return err
}

// ListUserLocks returns users with active failure counts or lock deadlines.
// ListUserLocks 返回存在失败次数或锁定截止时间的用户。
func (s *Store) ListUserLocks(ctx context.Context) ([]UserLock, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id, username, failed_attempts, COALESCE(locked_until, '') FROM users WHERE failed_attempts > 0 OR locked_until IS NOT NULL ORDER BY locked_until DESC, failed_attempts DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now().UTC()
	locks := make([]UserLock, 0)
	for rows.Next() {
		var lock UserLock
		if err := rows.Scan(&lock.ID, &lock.Username, &lock.FailedAttempts, &lock.LockedUntil); err != nil {
			return nil, err
		}
		deadline, deadlineErr := time.Parse(time.RFC3339, lock.LockedUntil)
		lock.LockedNow = deadlineErr == nil && now.Before(deadline)
		locks = append(locks, lock)
	}
	return locks, rows.Err()
}

// DeleteUserLock clears a user's failed-login counter and lock deadline.
// DeleteUserLock 清除用户的登录失败次数和锁定截止时间。
func (s *Store) DeleteUserLock(ctx context.Context, id int64) error {
	return s.ResetLoginState(ctx, id)
}

// ListLocks 返回全部来源 IP 失败记录和存在登录失败状态的用户。
// ListLocks returns all source-IP failure records and users with login failure state.
func (s *Store) ListLocks(ctx context.Context) ([]IPLock, []UserLock, error) {
	ipRows, err := s.DB.QueryContext(ctx, "SELECT ip, failed_count, window_started_at, COALESCE(locked_until, '') FROM ip_failures ORDER BY window_started_at DESC, ip")
	if err != nil {
		return nil, nil, err
	}
	defer ipRows.Close()
	now := time.Now().UTC()
	ipLocks := make([]IPLock, 0)
	for ipRows.Next() {
		var lock IPLock
		if err := ipRows.Scan(&lock.IP, &lock.FailedCount, &lock.WindowStartedAt, &lock.LockedUntil); err != nil {
			return nil, nil, err
		}
		deadline, parseErr := time.Parse(time.RFC3339, lock.LockedUntil)
		lock.LockedNow = parseErr == nil && now.Before(deadline)
		ipLocks = append(ipLocks, lock)
	}
	if err := ipRows.Err(); err != nil {
		return nil, nil, err
	}

	userRows, err := s.DB.QueryContext(ctx, "SELECT id, username, failed_attempts, COALESCE(locked_until, '') FROM users WHERE failed_attempts > 0 OR locked_until IS NOT NULL ORDER BY id")
	if err != nil {
		return nil, nil, err
	}
	defer userRows.Close()
	userLocks := make([]UserLock, 0)
	for userRows.Next() {
		var lock UserLock
		if err := userRows.Scan(&lock.ID, &lock.Username, &lock.FailedAttempts, &lock.LockedUntil); err != nil {
			return nil, nil, err
		}
		deadline, parseErr := time.Parse(time.RFC3339, lock.LockedUntil)
		lock.LockedNow = parseErr == nil && now.Before(deadline)
		userLocks = append(userLocks, lock)
	}
	return ipLocks, userLocks, userRows.Err()
}

// ClearIPLock 删除一个精确匹配的来源 IP 失败记录，并返回记录是否存在。
// ClearIPLock removes one exact source-IP failure record and reports whether it existed.
func (s *Store) ClearIPLock(ctx context.Context, ip string) (bool, error) {
	result, err := s.DB.ExecContext(ctx, "DELETE FROM ip_failures WHERE ip = ?", ip)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

// ClearUserLock 清除一个用户的登录失败状态，并返回用户是否存在。
// ClearUserLock clears one user's failed-login state and reports whether the user existed.
func (s *Store) ClearUserLock(ctx context.Context, id int64) (bool, error) {
	result, err := s.DB.ExecContext(ctx, "UPDATE users SET failed_attempts = 0, locked_until = NULL, updated_at = ? WHERE id = ? AND (failed_attempts > 0 OR locked_until IS NOT NULL)", time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

// ClearAllLocks 清空全部 IP 失败记录和用户登录失败状态，并返回受影响记录数。
// ClearAllLocks clears all IP failure records and user login failure state, returning the affected count.
func (s *Store) ClearAllLocks(ctx context.Context) (int, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	ipResult, err := tx.ExecContext(ctx, "DELETE FROM ip_failures")
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	userResult, err := tx.ExecContext(ctx, "UPDATE users SET failed_attempts = 0, locked_until = NULL, updated_at = ? WHERE failed_attempts > 0 OR locked_until IS NOT NULL", time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	ipCount, err := ipResult.RowsAffected()
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	userCount, err := userResult.RowsAffected()
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int(ipCount + userCount), nil
}

func (s *Store) DeleteUser(ctx context.Context, id int64) ([]string, error) {
	// DeleteUser 事务删除账户并返回其文件路径，调用方随后清理物理文件。
	// DeleteUser transactionally removes the account and returns its file paths for physical cleanup by the caller.
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT storage_path FROM files WHERE user_id = ?", id)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			rows.Close()
			tx.Rollback()
			return nil, err
		}
		paths = append(paths, path)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		tx.Rollback()
		return nil, err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM users WHERE id = ?", id)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		tx.Rollback()
		return nil, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return paths, nil
}

func (s *Store) ListUsers(ctx context.Context, keyword string, page, pageSize int) ([]User, int, error) {
	// ListUsers 按用户名搜索并分页返回账户记录和总数。
	// ListUsers searches by username and returns paginated accounts with the total count.
	pattern := "%" + keyword + "%"
	var total int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(id) FROM users WHERE username LIKE ?", pattern).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT id, username, password_hash, role, language, quota_bytes, used_bytes, disabled, failed_attempts, COALESCE(locked_until, ''), COALESCE(must_change_password, 0), COALESCE(totp_secret, ''), COALESCE(totp_enabled, 0), COALESCE(last_used_totp, ''), COALESCE(ip_acl_enabled, 0), COALESCE(ip_whitelist, ''), COALESCE(read_only_from, ''), COALESCE(read_only_until, ''), created_at, updated_at, (SELECT COUNT(id) FROM folders WHERE folders.user_id = users.id), (SELECT COUNT(id) FROM files WHERE files.user_id = users.id AND files.status = 'ready') FROM users WHERE username LIKE ? ORDER BY id LIMIT ? OFFSET ?", pattern, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	users := make([]User, 0)
	for rows.Next() {
		var user User
		var disabled, mustChange, totpEnabled, ipACLEnabled int
		if err := rows.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.Language, &user.QuotaBytes, &user.UsedBytes, &disabled, &user.FailedAttempts, &user.LockedUntil, &mustChange, &user.TOTPSecret, &totpEnabled, &user.LastUsedTOTP, &ipACLEnabled, &user.IPWhitelist, &user.ReadOnlyFrom, &user.ReadOnlyUntil, &user.CreatedAt, &user.UpdatedAt, &user.FolderCount, &user.FileCount); err != nil {
			return nil, 0, err
		}
		user.Disabled = disabled != 0
		user.MustChangePassword = mustChange != 0
		user.TOTPEnabled = totpEnabled != 0
		user.IPACLEnabled = ipACLEnabled != 0
		users = append(users, user)
	}
	return users, total, rows.Err()
}

// BatchDeleteFiles 在单个事务中软删除全部请求文件，并返回物理清理所需的存储路径。
// BatchDeleteFiles transactionally soft-deletes every requested file and returns its storage paths for physical cleanup.
func (s *Store) BatchDeleteFiles(ctx context.Context, fileIDs []int64, userID int64, admin bool) ([]string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	rollback := func(err error) ([]string, error) {
		_ = tx.Rollback()
		return nil, err
	}
	seen := make(map[int64]struct{}, len(fileIDs))
	type deletion struct {
		id    int64
		owner int64
		size  int64
		path  string
	}
	deletions := make([]deletion, 0, len(fileIDs))
	for _, fileID := range fileIDs {
		if _, exists := seen[fileID]; exists {
			continue
		}
		seen[fileID] = struct{}{}
		var item deletion
		var status string
		if err := tx.QueryRowContext(ctx, "SELECT user_id, size, storage_path, status FROM files WHERE id = ?", fileID).Scan(&item.owner, &item.size, &item.path, &status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return rollback(ErrNotFound)
			}
			return rollback(err)
		}
		if status == "deleted" || (!admin && item.owner != userID) {
			return rollback(ErrNotFound)
		}
		item.id = fileID
		deletions = append(deletions, item)
	}
	if len(deletions) == 0 {
		return rollback(ErrNotFound)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	usedByOwner := make(map[int64]int64)
	for _, item := range deletions {
		if _, err := tx.ExecContext(ctx, "UPDATE files SET status = 'deleted', deleted_at = ? WHERE id = ?", now, item.id); err != nil {
			return rollback(err)
		}
		usedByOwner[item.owner] += item.size
	}
	// Update each owner's quota once so the transaction remains consistent for mixed-admin batches.
	// 对管理员跨用户批量删除按用户汇总扣减，保证配额更新与文件状态同事务提交。
	for owner, size := range usedByOwner {
		if _, err := tx.ExecContext(ctx, "UPDATE users SET used_bytes = MAX(0, used_bytes - ?), updated_at = ? WHERE id = ?", size, now, owner); err != nil {
			return rollback(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(deletions))
	for _, item := range deletions {
		paths = append(paths, item.path)
	}
	return paths, nil
}

func (s *Store) CreateUploadTask(ctx context.Context, task UploadTask) error {
	// CreateUploadTask 在事务中计算现有、待上传和覆盖替换占用，确保配额预留不超限。
	// CreateUploadTask transactionally accounts for existing, pending, and replaced bytes before reserving quota.
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	var quota, used, pending, replacingSize int64
	if err := tx.QueryRowContext(ctx, "SELECT quota_bytes, used_bytes FROM users WHERE id = ?", task.UserID).Scan(&quota, &used); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(SUM(size), 0) FROM upload_tasks WHERE user_id = ? AND status = 'pending'", task.UserID).Scan(&pending); err != nil {
		tx.Rollback()
		return err
	}
	if task.Resolve == "overwrite" {
		_ = tx.QueryRowContext(ctx, "SELECT size FROM files WHERE user_id = ? AND status = 'ready' AND storage_path = ?", task.UserID, filepath.Join(task.StorageDir, task.Name)).Scan(&replacingSize)
	}
	if used-replacingSize+pending+task.Size > quota {
		tx.Rollback()
		return &QuotaError{UsedBytes: used, QuotaBytes: quota, FileSize: task.Size}
	}
	if task.Resolve == "overwrite" {
		var oldID, oldSize int64
		var oldPath string
		err := tx.QueryRowContext(ctx, "SELECT id, size, storage_path FROM files WHERE user_id = ? AND status = 'ready' AND storage_path = ?", task.UserID, filepath.Join(task.StorageDir, task.Name)).Scan(&oldID, &oldSize, &oldPath)
		if err == nil {
			if _, err := tx.ExecContext(ctx, "DELETE FROM files WHERE id = ?", oldID); err != nil {
				tx.Rollback()
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE users SET used_bytes = MAX(0, used_bytes - ?), updated_at = ? WHERE id = ?", oldSize, time.Now().UTC().Format(time.RFC3339), task.UserID); err != nil {
				tx.Rollback()
				return err
			}
			if removeErr := os.Remove(filepath.Join(s.DataDir, oldPath)); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				tx.Rollback()
				return fmt.Errorf("remove overwritten file: %w", removeErr)
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			tx.Rollback()
			return err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = tx.ExecContext(ctx, "INSERT INTO upload_tasks(id, user_id, collection_id, remark, name, size, mime, chunk_size, total_chunks, status, storage_dir, resolve, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?, ?)", task.ID, task.UserID, task.CollectionID, task.Remark, task.Name, task.Size, task.Mime, task.ChunkSize, task.TotalChunks, task.StorageDir, task.Resolve, now, now)
	if err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) GetUploadTask(ctx context.Context, id string) (UploadTask, error) {
	// GetUploadTask 按任务 ID 读取上传任务，并将缺失任务映射为 ErrNotFound。
	// GetUploadTask loads an upload task by ID and maps a missing task to ErrNotFound.
	var task UploadTask
	err := s.DB.QueryRowContext(ctx, "SELECT id, user_id, COALESCE(collection_id, 0), COALESCE(remark, ''), name, size, mime, chunk_size, total_chunks, status, COALESCE(storage_dir, ''), COALESCE(resolve, '') FROM upload_tasks WHERE id = ?", id).Scan(&task.ID, &task.UserID, &task.CollectionID, &task.Remark, &task.Name, &task.Size, &task.Mime, &task.ChunkSize, &task.TotalChunks, &task.Status, &task.StorageDir, &task.Resolve)
	if errors.Is(err, sql.ErrNoRows) {
		return UploadTask{}, ErrNotFound
	}
	return task, err
}

// SetChunk 以幂等方式记录已写入的分片元数据。
// SetChunk upserts the metadata for a successfully written chunk.
func (s *Store) SetChunk(ctx context.Context, taskID string, idx int, size int64, sha256 string) error {
	_, err := s.DB.ExecContext(ctx, "INSERT INTO chunks(task_id, idx, size, sha256) VALUES(?, ?, ?, ?) ON CONFLICT(task_id, idx) DO UPDATE SET size = excluded.size, sha256 = excluded.sha256", taskID, idx, size, sha256)
	return err
}

// ListChunks 读取任务已上传的分片，数据库记录是断点续传的唯一事实来源。
// ListChunks reads uploaded chunks from the database, the source of truth for resuming uploads.
func (s *Store) ListChunks(ctx context.Context, taskID string) (map[int]ChunkInfo, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT idx, size, sha256 FROM chunks WHERE task_id = ?", taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	chunks := make(map[int]ChunkInfo)
	for rows.Next() {
		var idx int
		var chunk ChunkInfo
		if err := rows.Scan(&idx, &chunk.Size, &chunk.SHA256); err != nil {
			return nil, err
		}
		chunks[idx] = chunk
	}
	return chunks, rows.Err()
}

// TaskProgress 描述一个进行中上传任务的实时进度，供服务端推送（SSE）。
// TaskProgress describes the live progress of an in-flight upload task for server push (SSE).
type TaskProgress struct {
	TaskID      string `json:"taskId"`
	Name        string `json:"name"`
	TotalChunks int    `json:"totalChunks"`
	Uploaded    int    `json:"uploaded"`
	Status      string `json:"status"`
}

// ListPendingTaskProgress 返回指定用户的所有 pending 上传任务及其已上传分片数。
// ListPendingTaskProgress returns all pending upload tasks for a user with their uploaded-chunk counts.
func (s *Store) ListPendingTaskProgress(ctx context.Context, userID int64) ([]TaskProgress, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT t.id, t.name, t.total_chunks, t.status, COUNT(c.task_id) AS uploaded
		FROM upload_tasks t
		LEFT JOIN chunks c ON c.task_id = t.id
		WHERE t.user_id = ? AND t.status = 'pending'
		GROUP BY t.id, t.name, t.total_chunks, t.status, t.created_at
		ORDER BY t.created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	progress := make([]TaskProgress, 0, 8)
	for rows.Next() {
		var p TaskProgress
		if err := rows.Scan(&p.TaskID, &p.Name, &p.TotalChunks, &p.Status, &p.Uploaded); err != nil {
			return nil, err
		}
		progress = append(progress, p)
	}
	return progress, rows.Err()
}

// DeleteChunks 清理已完成任务的分片元数据。
// DeleteChunks removes chunk metadata after a task has completed successfully.
func (s *Store) DeleteChunks(ctx context.Context, taskID string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM chunks WHERE task_id = ?", taskID)
	return err
}

// ListExpiredUploadTasks 返回超过 maxAge 仍未完成的上传任务 ID，供后台清理。
// ListExpiredUploadTasks returns upload task IDs still pending beyond maxAge for background cleanup.
func (s *Store) ListExpiredUploadTasks(ctx context.Context, maxAge time.Duration) ([]string, error) {
	cutoff := time.Now().UTC().Add(-maxAge).Format(time.RFC3339)
	rows, err := s.DB.QueryContext(ctx, "SELECT id FROM upload_tasks WHERE status = 'pending' AND created_at < ?", cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0, 8)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// DeleteUploadTask 删除上传任务及其分片记录（磁盘 tmp 目录由调用方清理）。
// DeleteUploadTask removes an upload task and its chunk records (the caller removes the tmp directory).
func (s *Store) DeleteUploadTask(ctx context.Context, taskID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// Keep the expiration check and deletion in one write transaction. A task can
	// finish between ListExpiredUploadTasks and this call, and must then be kept.
	if _, err := tx.ExecContext(ctx, `DELETE FROM chunks
		WHERE task_id = ? AND EXISTS (SELECT 1 FROM upload_tasks WHERE id = ? AND status = 'pending')`, taskID, taskID); err != nil {
		tx.Rollback()
		return err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM upload_tasks WHERE id = ? AND status = 'pending'", taskID)
	if err != nil {
		tx.Rollback()
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		tx.Rollback()
		return err
	}
	if count != 1 {
		tx.Rollback()
		return ErrNotFound
	}
	return tx.Commit()
}

func (s *Store) FindUploadConflict(ctx context.Context, userID int64, directory, storedName string) (File, error) {
	// FindUploadConflict 检查用户目录中的 ready 文件是否占用目标存储路径。
	// FindUploadConflict checks whether a ready file already occupies the target path for the user.
	return scanFile(s.DB.QueryRowContext(ctx, "SELECT id, user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at, COALESCE(deleted_at, '') FROM files WHERE user_id = ? AND status = 'ready' AND storage_path = ?", userID, filepath.Join(directory, storedName)))
}

// FindInstantMatch 按 md5 优先、sha256 兜底查找当前用户的同大小文件。
// FindInstantMatch finds a same-sized ready file for the user, preferring MD5 and falling back to SHA-256.
func (s *Store) FindInstantMatch(ctx context.Context, userID int64, md5, sha256 string, size int64) (File, error) {
	if md5 != "" {
		file, err := scanFile(s.DB.QueryRowContext(ctx, "SELECT id, user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at, COALESCE(deleted_at, '') FROM files WHERE user_id = ? AND status = 'ready' AND md5 = ? AND size = ? ORDER BY id LIMIT 1", userID, md5, size))
		if err == nil || !errors.Is(err, ErrNotFound) {
			return file, err
		}
	}
	return scanFile(s.DB.QueryRowContext(ctx, "SELECT id, user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at, COALESCE(deleted_at, '') FROM files WHERE user_id = ? AND status = 'ready' AND sha256 = ? AND size = ? ORDER BY id LIMIT 1", userID, sha256, size))
}

// FindInstantMatchInDirectory restricts instant upload to the requested storage directory.
// FindInstantMatchInDirectory 仅在目标目录内查找秒传文件，避免跨目录复用内容。
func (s *Store) FindInstantMatchInDirectory(ctx context.Context, userID int64, directory, md5, sha256 string, size int64) (File, error) {
	prefix := filepath.Clean(directory) + string(filepath.Separator)
	query := `SELECT id, user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at, COALESCE(deleted_at, '')
FROM files WHERE user_id = ? AND status = 'ready' AND substr(storage_path, 1, length(?)) = ? AND size = ? AND `
	args := []any{userID, prefix, prefix, size}
	if md5 != "" {
		file, err := scanFile(s.DB.QueryRowContext(ctx, query+"md5 = ? ORDER BY id LIMIT 1", append(args, md5)...))
		if err == nil || !errors.Is(err, ErrNotFound) {
			return file, err
		}
	}
	return scanFile(s.DB.QueryRowContext(ctx, query+"sha256 = ? ORDER BY id LIMIT 1", append(args, sha256)...))
}

func (s *Store) CompleteUpload(ctx context.Context, task UploadTask, file File) (File, error) {
	// CompleteUpload 在事务中写入文件记录、更新已用配额并完成上传任务。
	// CompleteUpload transactionally inserts the file, updates used quota, and completes the upload task.
	return s.completeUpload(ctx, task, file, nil)
}

// CompleteUploadWithPlacement 在数据库事务打开期间分配最终存储名，再由调用方原子放置文件内容。
// CompleteUploadWithPlacement allocates the final storage name while the database transaction is open, then lets the caller atomically place the content.
func (s *Store) CompleteUploadWithPlacement(ctx context.Context, task UploadTask, file File, place func(storagePath string) error) (File, error) {
	return s.completeUpload(ctx, task, file, place)
}

func (s *Store) completeUpload(ctx context.Context, task UploadTask, file File, place func(storagePath string) error) (File, error) {
	return s.completeUploadWithCollection(ctx, task, file, place, "", "")
}

func (s *Store) completeUploadWithCollection(ctx context.Context, task UploadTask, file File, place func(storagePath string) error, originalName, remark string) (File, error) {
	// completeUpload 在同一事务中协调路径分配、文件放置、元数据写入、配额更新和任务完成。
	// completeUpload coordinates path allocation, content placement, metadata, quota, and task completion in one transaction.
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return File{}, err
	}
	if place != nil {
		file.StoredName, file.StoragePath, err = allocateStorageName(ctx, tx, file.UserID, file.StoragePath, file.StoredName)
		if err != nil {
			tx.Rollback()
			return File{}, err
		}
		// 分配器为同名冲突生成了数字后缀（如 multi (1).txt）时，用户可见名同步跟随，
		// 否则列表/下载中多个同名文件无法区分。原名不再占用时（如覆盖场景）保持原名。
		// When the allocator produced a suffixed storage name (e.g. "multi (1).txt"), the
		// user-visible name follows so concurrent same-name uploads stay distinguishable.
		if file.StoredName != file.Name {
			file.Name = file.StoredName
		}
		if err := place(file.StoragePath); err != nil {
			tx.Rollback()
			return File{}, err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := tx.ExecContext(ctx, "INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, 'ready', ?, ?)", file.UserID, file.Name, file.StoredName, file.Size, file.Mime, file.SHA256, file.MD5, file.StoragePath, now)
	if err != nil {
		tx.Rollback()
		return File{}, err
	}
	file.ID, err = result.LastInsertId()
	if err != nil {
		tx.Rollback()
		return File{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE users SET used_bytes = used_bytes + ?, updated_at = ? WHERE id = ?", file.Size, now, file.UserID); err != nil {
		tx.Rollback()
		return File{}, err
	}
	if task.CollectionID > 0 {
		var expiresAt, revokedAt string
		if err := tx.QueryRowContext(ctx, "SELECT expires_at, COALESCE(revoked_at, '') FROM upload_collections WHERE id = ?", task.CollectionID).Scan(&expiresAt, &revokedAt); err != nil {
			tx.Rollback()
			return File{}, err
		}
		if revokedAt != "" {
			tx.Rollback()
			return File{}, ErrCollectionRevoked
		}
		if expiresAt <= now {
			tx.Rollback()
			return File{}, ErrCollectionExpired
		}
		if err := s.insertCollectionFileTx(ctx, tx, task.CollectionID, file.ID, originalName, remark, now); err != nil {
			tx.Rollback()
			return File{}, err
		}
	}
	result, err = tx.ExecContext(ctx, "UPDATE upload_tasks SET status = 'complete', updated_at = ? WHERE id = ? AND status = 'pending'", now, task.ID)
	if err != nil {
		tx.Rollback()
		return File{}, err
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		tx.Rollback()
		if err != nil {
			return File{}, err
		}
		return File{}, ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return File{}, err
	}
	file.Status = "ready"
	file.CreatedAt = now
	return file, nil
}

func allocateStorageName(ctx context.Context, tx *sql.Tx, userID int64, directory, base string) (string, string, error) {
	// allocateStorageName 在目标用户目录中选择最小可用的原名或数字后缀名。
	// allocateStorageName selects the original name or the smallest available suffix before the extension in the user's directory.
	directory = filepath.Clean(directory)
	if directory == "." || directory == "" || base == "" {
		return "", "", errors.New("invalid storage directory or name")
	}
	rows, err := tx.QueryContext(ctx, "SELECT stored_name, storage_path FROM files WHERE user_id = ? AND status != 'deleted'", userID)
	if err != nil {
		return "", "", err
	}
	used := map[string]bool{}
	for rows.Next() {
		var storedName, storagePath string
		if err := rows.Scan(&storedName, &storagePath); err != nil {
			rows.Close()
			return "", "", err
		}
		if filepath.Clean(filepath.Dir(storagePath)) == directory {
			used[storedName] = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return "", "", err
	}
	rows.Close()
	for suffix := 0; ; suffix++ {
		candidate := storageNameCandidate(base, suffix)
		if !used[candidate] {
			storagePath := filepath.Join(directory, candidate)
			// 目标路径若仍被软删除记录占用，清理该过期记录以复用路径（其磁盘内容早已物理删除）。
			// Reuse a storage path still held by a soft-deleted record by removing that stale row.
			if _, err := tx.ExecContext(ctx, "DELETE FROM files WHERE storage_path = ? AND status = 'deleted'", storagePath); err != nil {
				return "", "", err
			}
			return candidate, storagePath, nil
		}
	}
}

func storageNameCandidate(base string, suffix int) string {
	if suffix == 0 && len(base) <= 255 {
		return base
	}
	extension := filepath.Ext(base)
	stem := strings.TrimSuffix(base, extension)
	trailer := fmt.Sprintf(" (%d)", suffix)
	available := 255 - len(trailer)
	if available < 0 {
		return truncateUTF8Bytes(trailer, 255)
	}
	extension = truncateUTF8Bytes(extension, available)
	stem = truncateUTF8Bytes(stem, available-len(extension))
	return stem + trailer + extension
}

func truncateUTF8Bytes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	length := 0
	for _, char := range value {
		width := utf8.RuneLen(char)
		if width < 0 || length+width > limit {
			break
		}
		length += width
	}
	return value[:length]
}

func (s *Store) FindFile(ctx context.Context, id int64) (File, error) {
	// FindFile 按文件 ID 读取文件元数据。
	// FindFile loads file metadata by file ID.
	return scanFile(s.DB.QueryRowContext(ctx, "SELECT id, user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at, COALESCE(deleted_at, '') FROM files WHERE id = ?", id))
}

// CreateShare persists a new sharing link with its expiration and download limit.
// CreateShare 保存带有效期和下载次数限制的新分享链接。
func (s *Store) CreateShare(ctx context.Context, fileID, createdBy int64, token string, expiresAt time.Time, maxDownloads int) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO shares(file_id, token, created_by, expires_at, download_count, max_downloads, created_at)
VALUES(?, ?, ?, ?, 0, ?, ?)`, fileID, token, createdBy, expiresAt.UTC().Format(time.RFC3339), maxDownloads, time.Now().UTC().Format(time.RFC3339))
	if err != nil && isUniqueError(err) {
		return ErrConflict
	}
	return err
}

// GetShareByToken returns one sharing link and maps an unknown token to ErrNotFound.
// GetShareByToken 按 token 读取分享记录，未知 token 映射为 ErrNotFound。
func (s *Store) GetShareByToken(ctx context.Context, token string) (Share, error) {
	return s.getShareByToken(ctx, token, false)
}

// GetShareByTokenIncludingRevoked returns a link for audit classification, including soft-revoked records.
// GetShareByTokenIncludingRevoked 返回包含软撤销记录的分享链接，供审计分类使用。
func (s *Store) GetShareByTokenIncludingRevoked(ctx context.Context, token string) (Share, error) {
	return s.getShareByToken(ctx, token, true)
}

func (s *Store) getShareByToken(ctx context.Context, token string, includeRevoked bool) (Share, error) {
	where := "token = ?"
	if !includeRevoked {
		where += " AND revoked_at IS NULL"
	}
	var share Share
	err := s.DB.QueryRowContext(ctx, `SELECT id, file_id, token, created_by, expires_at, download_count, max_downloads, COALESCE(revoked_at, ''), created_at
FROM shares WHERE `+where, token).Scan(&share.ID, &share.FileID, &share.Token, &share.CreatedBy, &share.ExpiresAt, &share.DownloadCount, &share.MaxDownloads, &share.RevokedAt, &share.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Share{}, ErrNotFound
	}
	return share, err
}

// ListSharesByFile lists all sharing links for a file for owner-facing management views.
// ListSharesByFile 列出文件的全部分享链接，供文件所有者管理和展示。
func (s *Store) ListSharesByFile(ctx context.Context, fileID int64) ([]Share, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, file_id, token, created_by, expires_at, download_count, max_downloads, COALESCE(revoked_at, ''), created_at
FROM shares WHERE file_id = ? ORDER BY created_at DESC, id DESC`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	shares := make([]Share, 0)
	for rows.Next() {
		var share Share
		if err := rows.Scan(&share.ID, &share.FileID, &share.Token, &share.CreatedBy, &share.ExpiresAt, &share.DownloadCount, &share.MaxDownloads, &share.RevokedAt, &share.CreatedAt); err != nil {
			return nil, err
		}
		shares = append(shares, share)
	}
	return shares, rows.Err()
}

// ListSharesByOwner lists all links created by one user and joins the visible file name.
// ListSharesByOwner 列出用户创建的全部分享，并 JOIN 返回可见文件名。
func (s *Store) ListSharesByOwner(ctx context.Context, userID int64) ([]Share, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT s.id, s.file_id, f.name, s.token, s.created_by, s.expires_at, s.download_count, s.max_downloads, COALESCE(s.revoked_at, ''), s.created_at
FROM shares s JOIN files f ON f.id = s.file_id
WHERE s.created_by = ? ORDER BY s.created_at DESC, s.id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	shares := make([]Share, 0)
	for rows.Next() {
		var share Share
		if err := rows.Scan(&share.ID, &share.FileID, &share.FileName, &share.Token, &share.CreatedBy, &share.ExpiresAt, &share.DownloadCount, &share.MaxDownloads, &share.RevokedAt, &share.CreatedAt); err != nil {
			return nil, err
		}
		shares = append(shares, share)
	}
	return shares, rows.Err()
}

// DeleteSharesByFile revokes every sharing link for a file and returns the number removed.
// DeleteSharesByFile 撤销文件的全部分享链接并返回删除条数。
func (s *Store) DeleteSharesByFile(ctx context.Context, fileID int64) (int64, error) {
	result, err := s.DB.ExecContext(ctx, "UPDATE shares SET revoked_at = ? WHERE file_id = ? AND revoked_at IS NULL", time.Now().UTC().Format(time.RFC3339), fileID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// UpdateShareExpiry extends a link without allowing its expiry to move backwards.
// UpdateShareExpiry 延长分享有效期，并保证新时间不会缩短原有效期。
func (s *Store) UpdateShareExpiry(ctx context.Context, token string, newExpiresAt time.Time, ownerID int64) error {
	value := newExpiresAt.UTC().Format(time.RFC3339)
	result, err := s.DB.ExecContext(ctx, `UPDATE shares SET expires_at = CASE WHEN expires_at > ? THEN expires_at ELSE ? END
WHERE token = ? AND created_by = ? AND revoked_at IS NULL`, value, value, token, ownerID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrNotFound
	}
	return nil
}

// UpdateShareMaxDownloads raises a link limit atomically and rejects decreases.
// UpdateShareMaxDownloads 原子提高分享次数上限，并拒绝降低上限。
func (s *Store) UpdateShareMaxDownloads(ctx context.Context, token string, newMax int, ownerID int64) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE shares SET max_downloads = ?
WHERE token = ? AND created_by = ? AND revoked_at IS NULL AND max_downloads > 0 AND (? = 0 OR ? > max_downloads)`, newMax, token, ownerID, newMax, newMax)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		current, lookupErr := s.GetShareByTokenIncludingRevoked(ctx, token)
		if lookupErr != nil || current.CreatedBy != ownerID || current.RevokedAt != "" {
			return ErrNotFound
		}
		return ErrConflict
	}
	return nil
}

// DeleteShareByToken soft-revokes one owner-scoped link and preserves its audit history.
// DeleteShareByToken 软撤销单条归属分享，并保留其审计历史。
func (s *Store) DeleteShareByToken(ctx context.Context, token string, ownerID int64) error {
	result, err := s.DB.ExecContext(ctx, "UPDATE shares SET revoked_at = ? WHERE token = ? AND created_by = ? AND revoked_at IS NULL", time.Now().UTC().Format(time.RFC3339), token, ownerID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrNotFound
	}
	return nil
}

// IncrementShareDownloads atomically consumes one available download slot.
// IncrementShareDownloads 原子消耗一次可用下载次数，过期或超限时返回 false。
func (s *Store) IncrementShareDownloads(ctx context.Context, token string, maxDownloads int) (bool, error) {
	_ = maxDownloads
	result, err := s.DB.ExecContext(ctx, `UPDATE shares SET download_count = download_count + 1
WHERE token = ? AND revoked_at IS NULL AND expires_at > ? AND (max_downloads = 0 OR download_count < max_downloads)`, token, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

// CreateUploadCollection stores a new public upload collection.
// CreateUploadCollection 创建新的公开上传收集链接。
func (s *Store) CreateUploadCollection(ctx context.Context, collection UploadCollection) (UploadCollection, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.DB.ExecContext(ctx, `INSERT INTO upload_collections(created_by, name, token, expires_at, max_uploads, upload_count, max_file_bytes, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, 0, ?, ?, ?)`, collection.CreatedBy, collection.Name, collection.Token, collection.ExpiresAt, collection.MaxUploads, collection.MaxFileBytes, now, now)
	if err != nil {
		if isUniqueError(err) {
			return UploadCollection{}, ErrConflict
		}
		return UploadCollection{}, err
	}
	collection.ID, err = result.LastInsertId()
	collection.CreatedAt = now
	return collection, err
}

// GetUploadCollectionByToken loads a collection for anonymous token authentication.
// GetUploadCollectionByToken 按公开 token 读取收集链接。
func (s *Store) GetUploadCollectionByToken(ctx context.Context, token string) (UploadCollection, error) {
	return scanUploadCollection(s.DB.QueryRowContext(ctx, `SELECT id, created_by, name, token, expires_at, max_uploads, upload_count, max_file_bytes, COALESCE(revoked_at, ''), created_at
FROM upload_collections WHERE token = ?`, token))
}

// GetUploadCollection returns an owner-scoped collection record.
// GetUploadCollection 按归属读取收集链接，管理员可跨用户读取。
func (s *Store) GetUploadCollection(ctx context.Context, id, userID int64, admin bool) (UploadCollection, error) {
	where := "id = ?"
	args := []any{id}
	if !admin {
		where += " AND created_by = ?"
		args = append(args, userID)
	}
	return scanUploadCollection(s.DB.QueryRowContext(ctx, `SELECT id, created_by, name, token, expires_at, max_uploads, upload_count, max_file_bytes, COALESCE(revoked_at, ''), created_at
FROM upload_collections WHERE `+where, args...))
}

// ListUploadCollections lists collections owned by a user.
// ListUploadCollections 列出当前用户创建的收集链接。
func (s *Store) ListUploadCollections(ctx context.Context, userID int64, admin bool) ([]UploadCollection, error) {
	query := `SELECT id, created_by, name, token, expires_at, max_uploads, upload_count, max_file_bytes, COALESCE(revoked_at, ''), created_at
FROM upload_collections`
	args := []any{}
	if !admin {
		query += " WHERE created_by = ?"
		args = append(args, userID)
	}
	query += " ORDER BY created_at DESC, id DESC"
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	collections := make([]UploadCollection, 0)
	for rows.Next() {
		var collection UploadCollection
		if err := rows.Scan(&collection.ID, &collection.CreatedBy, &collection.Name, &collection.Token, &collection.ExpiresAt, &collection.MaxUploads, &collection.UploadCount, &collection.MaxFileBytes, &collection.RevokedAt, &collection.CreatedAt); err != nil {
			return nil, err
		}
		collections = append(collections, collection)
	}
	return collections, rows.Err()
}

// RevokeUploadCollection disables a collection without touching received files.
// RevokeUploadCollection 撤销收集链接，但保留已经收到的文件。
func (s *Store) RevokeUploadCollection(ctx context.Context, id, userID int64, admin bool) error {
	query := "UPDATE upload_collections SET revoked_at = ?, updated_at = ? WHERE id = ?"
	args := []any{time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339), id}
	if !admin {
		query += " AND created_by = ?"
		args = append(args, userID)
	}
	result, err := s.DB.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return ErrNotFound
	}
	return err
}

func scanUploadCollection(row *sql.Row) (UploadCollection, error) {
	var collection UploadCollection
	err := row.Scan(&collection.ID, &collection.CreatedBy, &collection.Name, &collection.Token, &collection.ExpiresAt, &collection.MaxUploads, &collection.UploadCount, &collection.MaxFileBytes, &collection.RevokedAt, &collection.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return UploadCollection{}, ErrNotFound
	}
	return collection, err
}

func collectionState(collection UploadCollection, now string) error {
	if collection.RevokedAt != "" {
		return ErrCollectionRevoked
	}
	if collection.ExpiresAt <= now {
		return ErrCollectionExpired
	}
	if collection.MaxUploads > 0 && collection.UploadCount >= collection.MaxUploads {
		return ErrCollectionLimit
	}
	return nil
}

// IncrementCollectionUploads atomically reserves one collection upload slot.
// IncrementCollectionUploads 原子预留一次收集上传次数。
func (s *Store) IncrementCollectionUploads(ctx context.Context, token string) (UploadCollection, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	collection, err := s.GetUploadCollectionByToken(ctx, token)
	if err != nil {
		return UploadCollection{}, err
	}
	if err := collectionState(collection, now); err != nil {
		return UploadCollection{}, err
	}
	result, err := s.DB.ExecContext(ctx, `UPDATE upload_collections SET upload_count = upload_count + 1, updated_at = ?
WHERE token = ? AND revoked_at IS NULL AND expires_at > ? AND (max_uploads = 0 OR upload_count < max_uploads)`, now, token, now)
	if err != nil {
		return UploadCollection{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return UploadCollection{}, err
	}
	if count != 1 {
		latest, latestErr := s.GetUploadCollectionByToken(ctx, token)
		if latestErr != nil {
			return UploadCollection{}, latestErr
		}
		return UploadCollection{}, collectionState(latest, now)
	}
	collection.UploadCount++
	return collection, nil
}

// CreateCollectionUploadTask reserves a collection slot, quota, and task atomically.
// CreateCollectionUploadTask 在一个事务中预留收集次数、用户配额和分片任务。
func (s *Store) CreateCollectionUploadTask(ctx context.Context, task UploadTask, token string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var collection UploadCollection
	err = tx.QueryRowContext(ctx, `SELECT id, created_by, name, token, expires_at, max_uploads, upload_count, max_file_bytes, COALESCE(revoked_at, ''), created_at
FROM upload_collections WHERE token = ?`, token).Scan(&collection.ID, &collection.CreatedBy, &collection.Name, &collection.Token, &collection.ExpiresAt, &collection.MaxUploads, &collection.UploadCount, &collection.MaxFileBytes, &collection.RevokedAt, &collection.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := collectionState(collection, now); err != nil {
		return err
	}
	if task.CollectionID == 0 {
		task.CollectionID = collection.ID
	}
	if task.UserID != collection.CreatedBy {
		return ErrNotFound
	}
	var quota, used, pending int64
	if err := tx.QueryRowContext(ctx, "SELECT quota_bytes, used_bytes FROM users WHERE id = ?", task.UserID).Scan(&quota, &used); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(SUM(size), 0) FROM upload_tasks WHERE user_id = ? AND status = 'pending'", task.UserID).Scan(&pending); err != nil {
		return err
	}
	if used+pending+task.Size > quota {
		return &QuotaError{UsedBytes: used, QuotaBytes: quota, FileSize: task.Size}
	}
	result, err := tx.ExecContext(ctx, `UPDATE upload_collections SET upload_count = upload_count + 1, updated_at = ?
WHERE id = ? AND revoked_at IS NULL AND expires_at > ? AND (max_uploads = 0 OR upload_count < max_uploads)`, now, collection.ID, now)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil {
		return err
	} else if count != 1 {
		return ErrCollectionLimit
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO upload_tasks(id, user_id, collection_id, remark, name, size, mime, chunk_size, total_chunks, status, storage_dir, resolve, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, '', ?, ?)", task.ID, task.UserID, task.CollectionID, task.Remark, task.Name, task.Size, task.Mime, task.ChunkSize, task.TotalChunks, task.StorageDir, now, now)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) insertCollectionFileTx(ctx context.Context, tx *sql.Tx, collectionID, fileID int64, originalName, remark, now string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO upload_collection_files(collection_id, file_id, original_name, remark, created_at) VALUES(?, ?, ?, ?, ?)", collectionID, fileID, originalName, remark, now)
	return err
}

// CreateCollectionFile records an instant-upload result and consumes one slot atomically.
// CreateCollectionFile 原子记录秒传文件并消耗一次收集次数。
func (s *Store) CreateCollectionFile(ctx context.Context, token string, fileID int64, originalName, remark string) (UploadCollectionFile, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return UploadCollectionFile{}, err
	}
	defer tx.Rollback()
	var collection UploadCollection
	err = tx.QueryRowContext(ctx, `SELECT id, created_by, name, token, expires_at, max_uploads, upload_count, max_file_bytes, COALESCE(revoked_at, ''), created_at
FROM upload_collections WHERE token = ?`, token).Scan(&collection.ID, &collection.CreatedBy, &collection.Name, &collection.Token, &collection.ExpiresAt, &collection.MaxUploads, &collection.UploadCount, &collection.MaxFileBytes, &collection.RevokedAt, &collection.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return UploadCollectionFile{}, ErrNotFound
	}
	if err != nil {
		return UploadCollectionFile{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := collectionState(collection, now); err != nil {
		return UploadCollectionFile{}, err
	}
	var owner int64
	if err := tx.QueryRowContext(ctx, "SELECT user_id FROM files WHERE id = ? AND status = 'ready'", fileID).Scan(&owner); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return UploadCollectionFile{}, ErrNotFound
		}
		return UploadCollectionFile{}, err
	}
	if owner != collection.CreatedBy {
		return UploadCollectionFile{}, ErrNotFound
	}
	result, err := tx.ExecContext(ctx, `UPDATE upload_collections SET upload_count = upload_count + 1, updated_at = ?
WHERE id = ? AND revoked_at IS NULL AND expires_at > ? AND (max_uploads = 0 OR upload_count < max_uploads)`, now, collection.ID, now)
	if err != nil {
		return UploadCollectionFile{}, err
	}
	if count, err := result.RowsAffected(); err != nil {
		return UploadCollectionFile{}, err
	} else if count != 1 {
		return UploadCollectionFile{}, ErrCollectionLimit
	}
	linkResult, err := tx.ExecContext(ctx, "INSERT INTO upload_collection_files(collection_id, file_id, original_name, remark, created_at) VALUES(?, ?, ?, ?, ?)", collection.ID, fileID, originalName, remark, now)
	if err != nil {
		return UploadCollectionFile{}, err
	}
	id, err := linkResult.LastInsertId()
	if err != nil {
		return UploadCollectionFile{}, err
	}
	if err := tx.Commit(); err != nil {
		return UploadCollectionFile{}, err
	}
	file, err := s.FindFile(ctx, fileID)
	if err != nil {
		return UploadCollectionFile{}, err
	}
	return UploadCollectionFile{ID: id, CollectionID: collection.ID, FileID: fileID, OriginalName: originalName, Remark: remark, CreatedAt: now, File: file}, nil
}

// CompleteCollectionFile links a completed task to its collection in one transaction.
// CompleteCollectionFile 将已完成的任务和收集链接记录在同一事务中。
func (s *Store) CompleteCollectionFile(ctx context.Context, task UploadTask, file File, originalName, remark string) (File, error) {
	return s.CompleteCollectionFileWithPlacement(ctx, task, file, nil, originalName, remark)
}

// CompleteCollectionFileWithPlacement commits a collected file and its remark atomically.
// CompleteCollectionFileWithPlacement 原子提交收集文件内容、元数据和备注。
func (s *Store) CompleteCollectionFileWithPlacement(ctx context.Context, task UploadTask, file File, place func(storagePath string) error, originalName, remark string) (File, error) {
	completed, err := s.completeUploadWithCollection(ctx, task, file, place, originalName, remark)
	if err != nil {
		return File{}, err
	}
	return completed, nil
}

// ListUploadCollectionFiles lists received files with the visitor remark.
// ListUploadCollectionFiles 列出收集链接已收到的文件和备注。
func (s *Store) ListUploadCollectionFiles(ctx context.Context, collectionID int64) ([]UploadCollectionFile, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT ucf.id, ucf.collection_id, ucf.file_id, ucf.original_name, ucf.remark, ucf.created_at,
f.id, f.user_id, f.name, f.stored_name, f.size, f.mime, f.sha256, f.md5, f.status, f.storage_path, f.created_at, COALESCE(f.deleted_at, '')
FROM upload_collection_files ucf JOIN files f ON f.id = ucf.file_id
WHERE ucf.collection_id = ? ORDER BY ucf.created_at DESC, ucf.id DESC`, collectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]UploadCollectionFile, 0)
	for rows.Next() {
		var item UploadCollectionFile
		if err := rows.Scan(&item.ID, &item.CollectionID, &item.FileID, &item.OriginalName, &item.Remark, &item.CreatedAt, &item.File.ID, &item.File.UserID, &item.File.Name, &item.File.StoredName, &item.File.Size, &item.File.Mime, &item.File.SHA256, &item.File.MD5, &item.File.Status, &item.File.StoragePath, &item.File.CreatedAt, &item.File.DeletedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// CreateFolder 校验父目录存在与路径唯一后创建用户目录。
// CreateFolder creates a user folder after validating the parent path and path uniqueness.
func (s *Store) CreateFolder(ctx context.Context, userID int64, parentPath, name string) (Folder, error) {
	path := name
	if parentPath != "" {
		path = parentPath + "/" + name
		if _, err := s.GetFolderByPath(ctx, userID, parentPath); err != nil {
			return Folder{}, err
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Folder{}, err
	}
	var parentID *int64
	if parentPath != "" {
		var id int64
		if err := tx.QueryRowContext(ctx, "SELECT id FROM folders WHERE user_id = ? AND path = ?", userID, parentPath).Scan(&id); err != nil {
			tx.Rollback()
			if errors.Is(err, sql.ErrNoRows) {
				return Folder{}, ErrNotFound
			}
			return Folder{}, err
		}
		parentID = &id
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := tx.ExecContext(ctx, "INSERT INTO folders(user_id, parent_id, name, path, created_at) VALUES(?, ?, ?, ?, ?)", userID, parentID, name, path, now)
	if err != nil {
		tx.Rollback()
		if isUniqueError(err) {
			return Folder{}, ErrConflict
		}
		return Folder{}, err
	}
	if err := tx.Commit(); err != nil {
		return Folder{}, err
	}
	id, _ := result.LastInsertId()
	return Folder{ID: id, UserID: userID, ParentID: parentID, Name: name, Path: path, CreatedAt: now}, nil
}

// GetFolderByPath 按相对路径读取用户目录，缺失映射为 ErrNotFound。
// GetFolderByPath loads a user folder by relative path, mapping missing entries to ErrNotFound.
func (s *Store) GetFolderByPath(ctx context.Context, userID int64, path string) (Folder, error) {
	var folder Folder
	err := s.DB.QueryRowContext(ctx, "SELECT id, user_id, parent_id, name, path, created_at FROM folders WHERE user_id = ? AND path = ?", userID, path).Scan(&folder.ID, &folder.UserID, &folder.ParentID, &folder.Name, &folder.Path, &folder.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Folder{}, ErrNotFound
	}
	return folder, err
}

// GetFolderByID 按 ID 读取用户目录，归属或存在性校验由调用方负责。
// GetFolderByID loads a user folder by ID; ownership/existence checks are the caller's responsibility.
func (s *Store) GetFolderByID(ctx context.Context, id, userID int64) (Folder, error) {
	var folder Folder
	err := s.DB.QueryRowContext(ctx, "SELECT id, user_id, parent_id, name, path, created_at FROM folders WHERE id = ? AND user_id = ?", id, userID).Scan(&folder.ID, &folder.UserID, &folder.ParentID, &folder.Name, &folder.Path, &folder.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Folder{}, ErrNotFound
	}
	return folder, err
}

// ListFolders 返回用户的全部目录（前端自行构建树/面包屑）。
// ListFolders returns all of a user's folders for the frontend to build trees and breadcrumbs.
func (s *Store) ListFolders(ctx context.Context, userID int64) ([]Folder, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id, user_id, parent_id, name, path, created_at FROM folders WHERE user_id = ? ORDER BY path", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	folders := make([]Folder, 0)
	for rows.Next() {
		var folder Folder
		if err := rows.Scan(&folder.ID, &folder.UserID, &folder.ParentID, &folder.Name, &folder.Path, &folder.CreatedAt); err != nil {
			return nil, err
		}
		folders = append(folders, folder)
	}
	return folders, rows.Err()
}

// EnsureFolderPath 按 mkdir -p 语义为缺失的路径段创建目录记录，保证上传目录与导航一致。
// EnsureFolderPath creates missing folder records for each path segment (mkdir -p semantics) so uploads stay consistent with navigation.
func (s *Store) EnsureFolderPath(ctx context.Context, userID int64, path string) error {
	if path == "" {
		return nil
	}
	acc := ""
	for _, seg := range strings.Split(path, "/") {
		acc = strings.TrimPrefix(acc+"/"+seg, "/")
		var count int
		if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(id) FROM folders WHERE user_id = ? AND path = ?", userID, acc).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			parent := ""
			if idx := strings.LastIndex(acc, "/"); idx >= 0 {
				parent = acc[:idx]
			}
			if _, err := s.CreateFolder(ctx, userID, parent, seg); err != nil && !errors.Is(err, ErrConflict) {
				return err
			}
		}
	}
	return nil
}

// RenameFolder 事务性重命名目录：更新自身与全部子孙的 path、批量替换文件 storage_path 前缀，并物理移动磁盘目录。
// RenameFolder transactionally renames a folder: updates the path of itself and descendants, rewrites file storage prefixes, and moves the disk directory.
func (s *Store) RenameFolder(ctx context.Context, id, userID int64, newName string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var oldPath string
	if err := tx.QueryRowContext(ctx, "SELECT path FROM folders WHERE id = ? AND user_id = ?", id, userID).Scan(&oldPath); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	newPath := newName
	if index := strings.LastIndex(oldPath, "/"); index >= 0 {
		newPath = oldPath[:index+1] + newName
	}
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(id) FROM folders WHERE user_id = ? AND path = ? AND id != ?", userID, newPath, id).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return ErrConflict
	}
	oldPrefix := oldPath + "/"
	newPrefix := newPath + "/"
	if _, err := tx.ExecContext(ctx, "UPDATE folders SET name = ?, path = ? WHERE id = ?", newName, newPath, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE folders SET path = ? || substr(path, length(?) + 1) WHERE user_id = ? AND path LIKE ? ESCAPE '\\'", newPrefix, oldPrefix, userID, escapeLike(oldPrefix)+"%"); err != nil {
		return err
	}
	filePrefix := filepath.Join("files", strconv.FormatInt(userID, 10), oldPath)
	newFilePrefix := filepath.Join("files", strconv.FormatInt(userID, 10), newPath)
	// 软删除记录仍占用旧 storage_path；重命名会把旧前缀下的 ready 文件改写为新前缀，
	// 与目标前缀下已存在的 deleted 记录（如先删除后重传的同名文件）发生 UNIQUE 冲突。
	// deleted 记录的内容已物理删除，改写前清除目标前缀下的它们，避免重命名目录 500
	//（与 D-S2-1 同类）。
	// Deleted rows keep their storage_path; rewriting ready files to the new prefix can
	// collide with a deleted row already holding that path (delete-then-reupload). Their
	// content is gone, so clear the target prefix before the rewrite.
	if _, err := tx.ExecContext(ctx, "DELETE FROM files WHERE user_id = ? AND status = 'deleted' AND storage_path LIKE ? ESCAPE '\\'", userID, escapeLike(newFilePrefix)+"%"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE files SET storage_path = ? || substr(storage_path, length(?) + 1) WHERE user_id = ? AND status != 'deleted' AND storage_path LIKE ? ESCAPE '\\'", newFilePrefix, filePrefix, userID, escapeLike(filePrefix)+"%"); err != nil {
		return err
	}
	oldDisk := filepath.Join(s.DataDir, filePrefix)
	newDisk := filepath.Join(s.DataDir, newFilePrefix)
	if err := os.Rename(oldDisk, newDisk); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("rename disk directory: %w", err)
	}
	return tx.Commit()
}

// DeleteFolder 仅删除空目录（无 ready 文件且无子目录），物理移除并删除记录。
// DeleteFolder removes only empty folders (no ready files and no children), deleting the disk directory and the record.
func (s *Store) DeleteFolder(ctx context.Context, id, userID int64) (bool, error) {
	folder, err := s.GetFolderByID(ctx, id, userID)
	if err != nil {
		return false, err
	}
	pathPrefix := filepath.Join("files", strconv.FormatInt(userID, 10), folder.Path) + string(filepath.Separator)
	var fileCount int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(id) FROM files WHERE user_id = ? AND status != 'deleted' AND substr(storage_path, 1, length(?)) = ?", userID, pathPrefix, pathPrefix).Scan(&fileCount); err != nil {
		return false, err
	}
	if fileCount > 0 {
		return false, ErrNotEmpty
	}
	var childCount int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(id) FROM folders WHERE user_id = ? AND parent_id = ?", userID, id).Scan(&childCount); err != nil {
		return false, err
	}
	if childCount > 0 {
		return false, ErrNotEmpty
	}
	disk := filepath.Join(s.DataDir, filepath.FromSlash(filepath.Join("files", strconv.FormatInt(userID, 10), folder.Path)))
	if err := os.Remove(disk); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	result, err := s.DB.ExecContext(ctx, "DELETE FROM folders WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "%", `\%`)
	value = strings.ReplaceAll(value, "_", `\_`)
	return value
}

func scanFile(row *sql.Row) (File, error) {
	var file File
	err := row.Scan(&file.ID, &file.UserID, &file.Name, &file.StoredName, &file.Size, &file.Mime, &file.SHA256, &file.MD5, &file.Status, &file.StoragePath, &file.CreatedAt, &file.DeletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return File{}, ErrNotFound
	}
	return file, err
}

func (s *Store) ListFiles(ctx context.Context, userID int64, admin bool, keyword, dir string, page, pageSize int) ([]File, int, error) {
	// ListFiles 只返回 ready 文件；普通用户按所有权隔离，管理员可查看全部文件；dir 限定 storage_path 前缀（v011 目录过滤）。
	// ListFiles returns ready files only; regular users are isolated by ownership while admins can view all files; dir filters by storage-path prefix.
	pattern := "%" + keyword + "%"
	where := "status = 'ready' AND name LIKE ?"
	args := []any{pattern}
	if !admin {
		where += " AND user_id = ?"
		args = append(args, userID)
	}
	if dir != "" {
		prefix := filepath.Join("files", strconv.FormatInt(userID, 10), dir) + string(filepath.Separator)
		where += " AND substr(storage_path, 1, length(?)) = ?"
		args = append(args, prefix, prefix)
	} else if admin {
		// 管理员无 dir 参数 = 全部文件（既有语义）
	} else {
		// 普通用户无 dir 参数 = 仅根目录层文件（v011 目录模型：子目录文件只在目录视图出现）
		prefix := filepath.Join("files", strconv.FormatInt(userID, 10)) + string(filepath.Separator)
		where += " AND substr(storage_path, 1, length(?)) = ? AND instr(substr(storage_path, length(?) + 1), ?) = 0"
		args = append(args, prefix, prefix, prefix, string(filepath.Separator))
	}
	var total int
	countArgs := append([]any{}, args...)
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(id) FROM files WHERE "+where, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.DB.QueryContext(ctx, "SELECT id, user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at, COALESCE(deleted_at, '') FROM files WHERE "+where+" ORDER BY created_at DESC LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	files := make([]File, 0)
	for rows.Next() {
		var file File
		if err := rows.Scan(&file.ID, &file.UserID, &file.Name, &file.StoredName, &file.Size, &file.Mime, &file.SHA256, &file.MD5, &file.Status, &file.StoragePath, &file.CreatedAt, &file.DeletedAt); err != nil {
			return nil, 0, err
		}
		files = append(files, file)
	}
	return files, total, rows.Err()
}

func (s *Store) DeleteFile(ctx context.Context, fileID, userID int64, admin bool) (string, error) {
	// DeleteFile 事务软删除记录并扣减配额，返回路径供调用方立即删除内容。
	// DeleteFile transactionally soft-deletes the record and subtracts quota, returning the path for immediate content removal.
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	var owner, size int64
	var path, status string
	if err := tx.QueryRowContext(ctx, "SELECT user_id, size, storage_path, status FROM files WHERE id = ?", fileID).Scan(&owner, &size, &path, &status); err != nil {
		tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	if !admin && owner != userID {
		tx.Rollback()
		return "", ErrNotFound
	}
	if status == "deleted" {
		tx.Rollback()
		return "", ErrNotFound
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, "UPDATE files SET status = 'deleted', deleted_at = ? WHERE id = ?", now, fileID); err != nil {
		tx.Rollback()
		return "", err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE users SET used_bytes = MAX(0, used_bytes - ?), updated_at = ? WHERE id = ?", size, now, owner); err != nil {
		tx.Rollback()
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return path, nil
}

func (s *Store) Stats(ctx context.Context) (map[string]int64, error) {
	// Stats 汇总用户数、ready 文件数、已使用文件字节数和分享访问统计。
	// Stats aggregates user count, ready-file bytes, and sharing access counters.
	stats := map[string]int64{}
	now := time.Now().UTC().Format(time.RFC3339)
	queries := map[string]string{
		"users":          "SELECT COUNT(id) FROM users",
		"files":          "SELECT COUNT(id) FROM files WHERE status = 'ready'",
		"bytes":          "SELECT COALESCE(SUM(size), 0) FROM files WHERE status = 'ready'",
		"shares":         "SELECT COUNT(id) FROM shares WHERE revoked_at IS NULL AND expires_at > ?",
		"shareDownloads": "SELECT COALESCE(SUM(download_count), 0) FROM shares",
	}
	for key, query := range queries {
		var value int64
		var err error
		if key == "shares" {
			err = s.DB.QueryRowContext(ctx, query, now).Scan(&value)
		} else {
			err = s.DB.QueryRowContext(ctx, query).Scan(&value)
		}
		if err != nil {
			return nil, err
		}
		stats[key] = value
	}
	return stats, nil
}

func (s *Store) GetLogSettings(ctx context.Context) (LogSettings, error) {
	// GetLogSettings 读取日志留存和登录锁定设置，并为缺失或非法值使用默认值。
	// GetLogSettings reads retention and lockout settings, applying defaults for missing or invalid values.
	settings := LogSettings{LogRetentionDays: 30, LockThreshold: 5, AutoUnlockEnabled: true, AutoUnlockMinutes: 5, DefaultLang: "zh-CN", ThemeColor: DefaultThemeColor, PasswordMinLength: 8, PasswordComplexity: 3, IPLockWindowMinutes: 10, IPLockThreshold: 50, IPAutoUnlockEnabled: true, IPUnlockMinutes: 30, RegisterEnabled: false, UploadRateLimit: 0, TrustProxy: false}
	rows, err := s.DB.QueryContext(ctx, "SELECT key, value FROM settings WHERE key IN ('logRetentionDays', 'lockThreshold', 'autoUnlockEnabled', 'autoUnlockMinutes', 'defaultLang', 'theme_color', 'passwordMinLength', 'passwordComplexity', 'ipLockWindowMinutes', 'ipLockThreshold', 'ipAutoUnlockEnabled', 'ipUnlockMinutes', 'registerEnabled', 'uploadRateLimit', 'trustProxy')")
	if err != nil {
		return settings, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return settings, err
		}
		switch key {
		case "logRetentionDays":
			if parsed, err := strconv.Atoi(value); err == nil && parsed >= 0 {
				settings.LogRetentionDays = parsed
			}
		case "lockThreshold":
			if parsed, err := strconv.Atoi(value); err == nil && parsed >= 0 {
				settings.LockThreshold = parsed
			}
		case "autoUnlockEnabled":
			settings.AutoUnlockEnabled = value == "true"
		case "autoUnlockMinutes":
			if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
				settings.AutoUnlockMinutes = parsed
			}
		case "defaultLang":
			if value == "zh-CN" || value == "zh-TW" || value == "en" {
				settings.DefaultLang = value
			}
		case "theme_color":
			if themeColorPattern.MatchString(strings.TrimSpace(value)) {
				settings.ThemeColor = normalizeThemeColor(value)
			}
		case "passwordMinLength":
			if parsed, err := strconv.Atoi(value); err == nil && parsed >= 1 && parsed <= 200 {
				settings.PasswordMinLength = parsed
			}
		case "passwordComplexity":
			if parsed, err := strconv.Atoi(value); err == nil && parsed >= 0 && parsed <= 4 {
				settings.PasswordComplexity = parsed
			}
		case "ipLockWindowMinutes":
			if parsed, err := strconv.Atoi(value); err == nil && parsed >= 1 {
				settings.IPLockWindowMinutes = parsed
			}
		case "ipLockThreshold":
			if parsed, err := strconv.Atoi(value); err == nil && parsed >= 0 {
				settings.IPLockThreshold = parsed
			}
		case "ipAutoUnlockEnabled":
			settings.IPAutoUnlockEnabled = value == "true"
		case "ipUnlockMinutes":
			if parsed, err := strconv.Atoi(value); err == nil && parsed >= 1 {
				settings.IPUnlockMinutes = parsed
			}
		case "registerEnabled":
			if parsed, err := strconv.ParseBool(value); err == nil {
				settings.RegisterEnabled = parsed
			}
		case "uploadRateLimit":
			if parsed, err := strconv.ParseInt(value, 10, 64); err == nil && parsed >= 0 {
				settings.UploadRateLimit = parsed
			}
		case "trustProxy":
			if parsed, err := strconv.ParseBool(value); err == nil {
				settings.TrustProxy = parsed
			}
		}
	}
	return settings, rows.Err()
}

func (s *Store) UpdateLogSettings(ctx context.Context, settings LogSettings) error {
	// UpdateLogSettings 校验非负留存/阈值和正数解锁时长后事务更新设置。
	// UpdateLogSettings validates non-negative retention/thresholds and positive unlock duration before a transactional update.
	settings.ThemeColor = normalizeThemeColor(settings.ThemeColor)
	if settings.LogRetentionDays < 0 || settings.LockThreshold < 0 || settings.AutoUnlockMinutes < 1 || settings.PasswordMinLength < 1 || settings.PasswordMinLength > 200 || settings.PasswordComplexity < 0 || settings.PasswordComplexity > 4 || settings.IPLockWindowMinutes < 1 || settings.IPLockThreshold < 0 || settings.IPUnlockMinutes < 1 || settings.UploadRateLimit < 0 || (settings.DefaultLang != "zh-CN" && settings.DefaultLang != "zh-TW" && settings.DefaultLang != "en") || (settings.ThemeColor != "" && !themeColorPattern.MatchString(settings.ThemeColor)) {
		return errors.New("invalid settings")
	}
	values := map[string]string{
		"logRetentionDays":    strconv.Itoa(settings.LogRetentionDays),
		"lockThreshold":       strconv.Itoa(settings.LockThreshold),
		"autoUnlockEnabled":   strconv.FormatBool(settings.AutoUnlockEnabled),
		"autoUnlockMinutes":   strconv.Itoa(settings.AutoUnlockMinutes),
		"defaultLang":         settings.DefaultLang,
		"theme_color":         settings.ThemeColor,
		"passwordMinLength":   strconv.Itoa(settings.PasswordMinLength),
		"passwordComplexity":  strconv.Itoa(settings.PasswordComplexity),
		"ipLockWindowMinutes": strconv.Itoa(settings.IPLockWindowMinutes),
		"ipLockThreshold":     strconv.Itoa(settings.IPLockThreshold),
		"ipAutoUnlockEnabled": strconv.FormatBool(settings.IPAutoUnlockEnabled),
		"ipUnlockMinutes":     strconv.Itoa(settings.IPUnlockMinutes),
		"registerEnabled":     strconv.FormatBool(settings.RegisterEnabled),
		"uploadRateLimit":     strconv.FormatInt(settings.UploadRateLimit, 10),
		"trustProxy":          strconv.FormatBool(settings.TrustProxy),
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for key, value := range values {
		if _, err := tx.ExecContext(ctx, "INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, value); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func normalizeThemeColor(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) == 4 && themeColorPattern.MatchString(value) {
		return "#" + string(value[1]) + string(value[1]) + string(value[2]) + string(value[2]) + string(value[3]) + string(value[3])
	}
	return value
}

func (s *Store) AddAuditLog(ctx context.Context, userID *int64, username, action, target, ip, result, reason string) error {
	return s.AddAuditLogWithShareOwner(ctx, userID, nil, username, action, target, ip, result, reason)
}

// AddAuditLogWithShareOwner records an audit event and links anonymous share activity to its creator.
// AddAuditLogWithShareOwner 写入审计事件，并将匿名分享活动关联到分享创建者。
func (s *Store) AddAuditLogWithShareOwner(ctx context.Context, userID, shareOwnerID *int64, username, action, target, ip, result, reason string) error {
	// AddAuditLog 在写入新记录前按留存天数惰性清理旧记录，并与写入保持同一事务。
	// AddAuditLog lazily prunes old records by retention days before inserting, in the same transaction.
	settings, err := s.GetLogSettings(ctx)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	cutoff := time.Now().UTC().Add(-time.Duration(settings.LogRetentionDays) * 24 * time.Hour).Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, "DELETE FROM audit_logs WHERE created_at < ?", cutoff); err != nil {
		tx.Rollback()
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, "INSERT INTO audit_logs(user_id, share_owner_id, username, action, target, ip, result, reason, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)", userID, shareOwnerID, username, action, target, ip, result, nullableString(reason), now); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) ListAuditLogs(ctx context.Context, userID *int64, action, result, keyword string, page, pageSize int) ([]AuditLog, int, error) {
	// ListAuditLogs 支持按用户、操作、结果和关键字筛选审计记录并分页返回。
	// ListAuditLogs filters audit records by user, action, result, and keyword, then returns a page.
	where := []string{"1 = 1"}
	args := []any{}
	if userID != nil {
		where = append(where, "(user_id = ? OR share_owner_id = ?)")
		args = append(args, *userID, *userID)
	}
	if action != "" {
		where = append(where, "action = ?")
		args = append(args, action)
	}
	if result != "" {
		where = append(where, "result = ?")
		args = append(args, result)
	}
	if keyword != "" {
		where = append(where, "(username LIKE ? OR target LIKE ? OR ip LIKE ? OR reason LIKE ?)")
		pattern := "%" + keyword + "%"
		args = append(args, pattern, pattern, pattern, pattern)
	}
	condition := strings.Join(where, " AND ")
	var total int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(id) FROM audit_logs WHERE "+condition, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, pageSize, (page-1)*pageSize)
	rows, err := s.DB.QueryContext(ctx, "SELECT id, user_id, share_owner_id, username, action, target, ip, result, COALESCE(reason, ''), created_at FROM audit_logs WHERE "+condition+" ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?", queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	logs := make([]AuditLog, 0)
	for rows.Next() {
		var entry AuditLog
		if err := rows.Scan(&entry.ID, &entry.UserID, &entry.ShareOwnerID, &entry.Username, &entry.Action, &entry.Target, &entry.IP, &entry.Result, &entry.Reason, &entry.CreatedAt); err != nil {
			return nil, 0, err
		}
		logs = append(logs, entry)
	}
	return logs, total, rows.Err()
}

// ListShareAuditLogs returns download activity for an owner-scoped share token.
// ListShareAuditLogs 返回归属分享 token 的下载活动。
func (s *Store) ListShareAuditLogs(ctx context.Context, token string, ownerID int64, page, pageSize int) ([]AuditLog, int, error) {
	args := []any{token, ownerID}
	condition := "action = 'share_download' AND target = ? AND share_owner_id = ?"
	var total int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(id) FROM audit_logs WHERE "+condition, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	queryArgs := append(args, pageSize, (page-1)*pageSize)
	rows, err := s.DB.QueryContext(ctx, "SELECT id, user_id, share_owner_id, username, action, target, ip, result, COALESCE(reason, ''), created_at FROM audit_logs WHERE "+condition+" ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?", queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	logs := make([]AuditLog, 0)
	for rows.Next() {
		var entry AuditLog
		if err := rows.Scan(&entry.ID, &entry.UserID, &entry.ShareOwnerID, &entry.Username, &entry.Action, &entry.Target, &entry.IP, &entry.Result, &entry.Reason, &entry.CreatedAt); err != nil {
			return nil, 0, err
		}
		logs = append(logs, entry)
	}
	return logs, total, rows.Err()
}

// ListUsedActions 返回用户日志中实际存在的动作类型（按最近发生时间去重）；管理员返回全库动作。
// ListUsedActions returns the action types actually present in a user's logs (deduplicated by
// recency); admins get actions across all users.
func (s *Store) ListUsedActions(ctx context.Context, userID int64, admin bool) ([]string, error) {
	query := "SELECT action FROM audit_logs"
	args := []any{}
	if !admin {
		query += " WHERE (user_id = ? OR share_owner_id = ?)"
		args = append(args, userID, userID)
	}
	query += " GROUP BY action ORDER BY MAX(id) DESC"
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	used := make([]string, 0, 16)
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			return nil, err
		}
		used = append(used, action)
	}
	return used, rows.Err()
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// isUniqueError 仅将 UNIQUE 约束冲突识别为 ErrConflict，避免把外键等其他
// "constraint failed" 错误（如 FOREIGN KEY constraint failed）误判为同名冲突。
// isUniqueError treats only genuine UNIQUE violations as ErrConflict so that other
// constraint failures (for example FOREIGN KEY) are not misreported as name clashes.
func isUniqueError(err error) bool {
	return err != nil && (contains(err.Error(), "UNIQUE constraint failed") || contains(err.Error(), "UNIQUE"))
}
func contains(value, part string) bool { return len(value) >= len(part) && stringContains(value, part) }
func stringContains(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
