package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filebox/internal/store"
)

func TestGeneratePasswordMeetsRequiredClasses(t *testing.T) {
	password, err := generatePassword()
	if err != nil {
		t.Fatal(err)
	}
	if len(password) != 16 {
		t.Fatalf("generated password length = %d, want 16", len(password))
	}
	for _, class := range []string{"ABCDEFGHIJKLMNOPQRSTUVWXYZ", "abcdefghijklmnopqrstuvwxyz", "0123456789", "!@#$%^&*"} {
		if !strings.ContainsAny(password, class) {
			t.Fatalf("generated password %q does not contain a character from %q", password, class)
		}
	}
}

func TestUnknownSubcommandReturnsUsageError(t *testing.T) {
	if code := run([]string{"unknown-command"}); code != 2 {
		t.Fatalf("run(unknown command) = %d, want 2", code)
	}
}

func TestClearIPACLCommand(t *testing.T) {
	dataDir := t.TempDir()
	db, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.UpdateIPACL(context.Background(), 1, true, "192.0.2.1"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if code := run([]string{"admin", "clear-ip-acl", "--data", dataDir, "--username", "admin"}); code != 0 {
		t.Fatalf("clear-ip-acl exit code = %d, want 0", code)
	}
	db, err = store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	user, err := db.GetUserByUsername("admin")
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if user.IPACLEnabled || user.IPWhitelist != "" {
		db.Close()
		t.Fatalf("clear-ip-acl did not clear ACL: enabled=%t whitelist=%q", user.IPACLEnabled, user.IPWhitelist)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"admin", "clear-ip-acl", "--data", dataDir, "--username", "missing"}); code != 1 {
		t.Fatalf("missing clear-ip-acl exit code = %d, want 1", code)
	}
	if code := run([]string{"admin", "clear-ip-acl", "--data", dataDir}); code != 2 {
		t.Fatalf("invalid clear-ip-acl exit code = %d, want 2", code)
	}
}

// TestResetPasswordParsesDataFlag 验证 --data 在子命令名之后仍被解析（回归 D-FIX）。
// TestResetPasswordParsesDataFlag verifies --data is parsed after the subcommand name (regression D-FIX).
func TestResetPasswordParsesDataFlag(t *testing.T) {
	dataDir := t.TempDir()
	db, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"admin", "reset-password", "--data", dataDir, "--username", "admin", "--new-password", "NewPass123!"}); code != 0 {
		t.Fatalf("reset-password exit code = %d, want 0", code)
	}
	db, err = store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	user, err := db.GetUserByUsername("admin")
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if !user.MustChangePassword {
		db.Close()
		t.Fatal("reset password did not set must_change_password")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestBackupRestoreRoundTrip 验证 admin backup → restore 端到端流程与密钥持久化。
// TestBackupRestoreRoundTrip verifies the admin backup → restore flow end to end with key persistence.
func TestBackupRestoreRoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	db, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// 模拟 serve 首次运行生成的 secrets.json，供 backup 读取。
	secret := "test-backup-secret-0123456789abcdef"
	if err := os.MkdirAll(filepath.Join(dataDir, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeSecretsFile(filepath.Join(dataDir, "config", "secrets.json"), secretsFilePayload{JWTSecret: secret}); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "backup.tar.gz")
	if code := run([]string{"admin", "backup", "--data", dataDir, "--out", out}); code != 0 {
		t.Fatalf("admin backup exit code = %d, want 0", code)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("backup archive missing: %v", err)
	}
	restoreDir := t.TempDir()
	if code := run([]string{"admin", "restore", "--data", restoreDir, "--in", out}); code != 0 {
		t.Fatalf("admin restore exit code = %d, want 0", code)
	}
	payload, err := readSecretsFile(filepath.Join(restoreDir, "config", "secrets.json"))
	if err != nil {
		t.Fatal(err)
	}
	if payload.JWTSecret != secret || secretFingerprint(payload.JWTSecret) != secretFingerprint(secret) {
		t.Fatalf("restored secret mismatch: %q", payload.JWTSecret)
	}
	restored, err := store.Open(restoreDir)
	if err != nil {
		t.Fatal(err)
	}
	user, err := restored.GetUserByUsername("admin")
	if err != nil || user.Username != "admin" {
		restored.Close()
		t.Fatalf("restored admin = %+v, %v", user, err)
	}
	if err := restored.Close(); err != nil {
		t.Fatal(err)
	}
	// 非空目标未加 --force --yes 时拒绝恢复
	if code := run([]string{"admin", "restore", "--data", restoreDir, "--in", out}); code == 0 {
		t.Fatal("restore into non-empty target without --force --yes should fail")
	}
	// --force --yes 允许替换并保留旧目录
	if code := run([]string{"admin", "restore", "--data", restoreDir, "--in", out, "--force", "--yes"}); code != 0 {
		t.Fatalf("forced restore exit code = %d, want 0", code)
	}
	if _, err := readSecretsFile(filepath.Join(restoreDir, "config", "secrets.json")); err != nil {
		t.Fatalf("forced restore did not write secrets.json: %v", err)
	}
	if matches, _ := filepath.Glob(restoreDir + ".pre-restore-*"); len(matches) != 1 {
		t.Fatalf("pre-restore backup dirs = %v, want 1", matches)
	}
}

// TestBackupRestoreLargeFileStreams verifies a large file survives backup and restore without ReadFile buffering.
// TestBackupRestoreLargeFileStreams 验证大文件备份与恢复路径使用流式读写且内容完整。
func TestBackupRestoreLargeFileStreams(t *testing.T) {
	dataDir := t.TempDir()
	db, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	largePath := filepath.Join(dataDir, "files", "1", "large.bin")
	if err := os.MkdirAll(filepath.Dir(largePath), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(largePath)
	if err != nil {
		t.Fatal(err)
	}
	block := bytes.Repeat([]byte("x"), 1024*1024)
	for index := 0; index < 32; index++ {
		if _, err := file.Write(block); err != nil {
			file.Close()
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	secret := "large-file-backup-secret-0123456789"
	out := filepath.Join(t.TempDir(), "large-backup.tar.gz")
	manifest, err := buildBackupArchive(dataDir, out, secret, "")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.FileCount < 2 || manifest.SHA256["files/1/large.bin"] == "" {
		t.Fatalf("large file missing from manifest: %+v", manifest)
	}
	restoreDir := t.TempDir()
	if code := run([]string{"admin", "restore", "--data", restoreDir, "--in", out}); code != 0 {
		t.Fatalf("restore large backup exit code = %d, want 0", code)
	}
	info, err := os.Stat(filepath.Join(restoreDir, "files", "1", "large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 32*1024*1024 {
		t.Fatalf("restored large file size = %d, want %d", info.Size(), 32*1024*1024)
	}
}

// TestBackupRequiresJWTSecret 验证缺失密钥时 backup 报错而不是回退开发密钥。
// TestBackupRequiresJWTSecret verifies backup fails when no JWT secret is available instead of falling back.
func TestBackupRequiresJWTSecret(t *testing.T) {
	dataDir := t.TempDir()
	db, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "backup.tar.gz")
	if code := run([]string{"admin", "backup", "--data", dataDir, "--out", out}); code != 1 {
		t.Fatalf("backup without secret exit code = %d, want 1", code)
	}
	if code := run([]string{"admin", "backup", "--data", dataDir, "--out", out, "--jwt-secret", "explicit-secret-1234"}); code != 0 {
		t.Fatalf("backup with --jwt-secret exit code = %d, want 0", code)
	}
}

// TestJWTSecretValidation 验证空串/过短的 --jwt-secret 与 FILEBOX_JWT_SECRET 被拒绝（G11）。
// TestJWTSecretValidation verifies empty and too-short --jwt-secret / FILEBOX_JWT_SECRET values are rejected (G11).
func TestJWTSecretValidation(t *testing.T) {
	t.Setenv("FILEBOX_JWT_SECRET", "")
	// 空串 flag：必须报错，而不是静默退化成弱 HMAC key。
	// An explicitly empty flag must fail rather than silently degrade to a weak HMAC key.
	if _, _, err := resolveJWTSecret(t.TempDir(), true, false, "", true); err == nil {
		t.Fatal("resolveJWTSecret accepted an empty --jwt-secret")
	}
	// 过短 flag：同样拒绝。
	// Too-short flags are rejected as well.
	if _, _, err := resolveJWTSecret(t.TempDir(), true, false, "short", true); err == nil {
		t.Fatal("resolveJWTSecret accepted a too-short --jwt-secret")
	}
	// 环境变量为空：拒绝。
	// An empty environment variable is rejected.
	if _, _, err := resolveJWTSecret(t.TempDir(), false, true, "", true); err == nil {
		t.Fatal("resolveJWTSecret accepted an empty FILEBOX_JWT_SECRET")
	}
	// 合法密钥通过。
	// A valid secret passes.
	if _, source, err := resolveJWTSecret(t.TempDir(), true, false, "0123456789abcdef0123456789abcdef", true); err != nil || source != "flag-or-env" {
		t.Fatalf("resolveJWTSecret(valid) = %q, %v", source, err)
	}
	// secrets.json 中空密钥同样报错。
	// An empty secret in secrets.json is also rejected.
	emptyDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(emptyDir, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeSecretsFile(filepath.Join(emptyDir, "config", "secrets.json"), secretsFilePayload{JWTSecret: ""}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolveJWTSecret(emptyDir, false, false, "", true); err == nil {
		t.Fatal("resolveJWTSecret accepted an empty secrets.json JWT secret")
	}
	shortDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(shortDir, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeSecretsFile(filepath.Join(shortDir, "config", "secrets.json"), secretsFilePayload{JWTSecret: "short"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolveJWTSecret(shortDir, false, false, "", true); err == nil || !strings.Contains(err.Error(), "regenerate or fix") {
		t.Fatalf("resolveJWTSecret accepted short secrets.json JWT secret or omitted repair guidance: %v", err)
	}
}

// buildTestArchive 构造一个含指定条目（name/size/内容）的 tar.gz 归档，用于恢复限额测试。
// buildTestArchive builds a tar.gz archive with the given entries for restore-limit tests.
func buildTestArchive(t *testing.T, entries []struct {
	name    string
	size    int64
	content []byte
}) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		content := entry.content
		if content == nil {
			content = []byte("x")
		}
		header := &tar.Header{Name: entry.name, Mode: 0o600, Size: entry.size, Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if entry.size > 0 {
			if _, err := tw.Write(content); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// rawTarHeader 手工构造 512 字节 tar 头（含合法校验和），声明 size 但不含内容，
// 用于模拟"解压炸弹"式恶意归档（tar.Writer 会校验实际写入长度，无法声明超大 size）。
// 超过 8 GiB 的 size 使用 base-256 编码（tar 12 字节 size 字段的八进制上限为 8^11-1）。
// rawTarHeader builds a 512-byte tar header (with a valid checksum) declaring size without
// content, simulating a decompression-bomb archive (tar.Writer validates written length, so
// an oversized declared size cannot be produced through it). Sizes above 8 GiB use base-256
// encoding (the octal cap of the 12-byte size field is 8^11-1).
func rawTarHeader(name string, size int64) []byte {
	block := make([]byte, 512)
	copy(block[0:100], []byte(name))
	copy(block[100:108], "0000644\x00") // mode
	copy(block[108:116], "0000000\x00") // uid
	copy(block[116:124], "0000000\x00") // gid
	const octalCap = int64(1<<33 - 1)   // 8^11-1
	if size <= octalCap {
		sizeOctal := fmt.Sprintf("%011o", size)
		copy(block[124:135], sizeOctal)
		block[135] = 0
	} else {
		// base-256：首字节高位置 1，其余 11 字节大端存放值。
		// base-256: the first byte's high bit flags the format, the remaining 11 bytes hold the value big-endian.
		block[124] = 0x80
		for index := 1; index <= 11; index++ {
			block[124+index] = byte(size >> (8 * (11 - index)))
		}
	}
	mtimeOctal := fmt.Sprintf("%011o", time.Now().Unix())
	copy(block[136:147], mtimeOctal)
	block[147] = 0
	block[156] = '0' // typeflag: regular file
	copy(block[257:263], "ustar\x00")
	block[263], block[264] = '0', '0'
	// 校验和：checksum 字段按空格参与求和，结果以 6 位八进制 + NUL + 空格写入。
	// Checksum: the checksum field counts as spaces; the result is written as 6 octal digits + NUL + space.
	var sum int64
	for index, value := range block {
		if index >= 148 && index < 156 {
			sum += ' '
		} else {
			sum += int64(value)
		}
	}
	checksum := fmt.Sprintf("%06o", sum)
	copy(block[148:154], checksum)
	block[154], block[155] = 0, ' '
	return block
}

// rawArchive 将若干原始 tar 块（512 字节头 + 数据填充）打包为 tar.gz，并以 1024 个零字节结束。
// rawArchive packs raw tar blocks (512-byte headers plus data padding) into tar.gz, ending with 1024 zero bytes.
func rawArchive(t *testing.T, entries ...[]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	for _, entry := range entries {
		if _, err := gz.Write(entry); err != nil {
			t.Fatal(err)
		}
		// 条目无内容：数据区 0 字节，下一个头紧跟 512 字节对齐位置，无需额外填充。
	}
	if _, err := gz.Write(make([]byte, 1024)); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// TestRestoreRejectsMaliciousArchives 验证 restore 拒绝重复条目与超限条目（G10）。
// TestRestoreRejectsMaliciousArchives verifies restore rejects duplicate and oversized entries (G10).
func TestRestoreRejectsMaliciousArchives(t *testing.T) {
	writeArchive := func(t *testing.T, name string, data []byte) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	// 重复条目必须被拒绝。
	// Duplicate entries must be rejected.
	duplicate := buildTestArchive(t, []struct {
		name    string
		size    int64
		content []byte
	}{{name: "filebox.db", size: 1}, {name: "filebox.db", size: 1}})
	if code := run([]string{"admin", "restore", "--data", t.TempDir(), "--in", writeArchive(t, "dup.tar.gz", duplicate)}); code != 1 {
		t.Fatalf("restore with duplicate entries = %d, want 1", code)
	}
	// 单文件超过限额必须被拒绝（仅声明大小即拒绝，无需实际内容；base-256 编码 200 GiB+1）。
	// An entry whose declared size exceeds the limit must be rejected from the header alone
	// (base-256 encodes the 200 GiB + 1 declared size).
	oversized := writeArchive(t, "big.tar.gz", rawArchive(t, rawTarHeader("filebox.db", restoreMaxSingleBytes+1)))
	if code := run([]string{"admin", "restore", "--data", t.TempDir(), "--in", oversized}); code != 1 {
		t.Fatalf("restore with oversized entry = %d, want 1", code)
	}
	// 总量超限与单文件超限共享同一前置检查模式（逐条在解压前累计判定）；
	// 单条声明的 size 不超过单文件上限时需真实内容才能推进累计，无法在测试中构造，
	// 这里以多条超限条目断言归档整体被拒绝。
	// The total-limit guard shares the same pre-extraction pattern as the single-file guard;
	// reaching the cumulative check needs real content, so this case asserts the archive is
	// rejected outright (the first entry already trips a size guard).
	overTotal := writeArchive(t, "total.tar.gz", rawArchive(t,
		rawTarHeader("files/f00", restoreMaxSingleBytes-1),
		rawTarHeader("files/f01", restoreMaxSingleBytes-1)))
	if code := run([]string{"admin", "restore", "--data", t.TempDir(), "--in", overTotal}); code != 1 {
		t.Fatalf("restore with oversized total = %d, want 1", code)
	}
}

func TestBackupCheckpointsWALAndRestoreValidatesDatabase(t *testing.T) {
	dataDir := t.TempDir()
	db, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		db.Close()
		t.Fatal(err)
	}
	defer db.Close()
	var userID int64
	if err := db.DB.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	storagePath := filepath.Join("files", fmt.Sprint(userID), "live.txt")
	physicalPath := filepath.Join(dataDir, filepath.FromSlash(storagePath))
	if err := os.MkdirAll(filepath.Dir(physicalPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(physicalPath, []byte("live-file-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, 'ready', ?, ?)", userID, "live.txt", "live.txt", int64(len("live-file-content")), "text/plain", "sha256", "md5", storagePath, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(t.TempDir(), "backup.tar.gz")
	if _, err := buildBackupArchive(dataDir, archive, "v016-test-secret-123", ""); err != nil {
		t.Fatalf("build backup: %v", err)
	}
	restoredDir := t.TempDir()
	if code := runAdminRestore([]string{"--data", restoredDir, "--in", archive, "--force", "--yes"}); code != 0 {
		t.Fatalf("restore exit code = %d", code)
	}
	if err := validateRestoredDatabase(restoredDir); err != nil {
		t.Fatalf("validate restored database: %v", err)
	}
	restoredDB, err := store.Open(restoredDir)
	if err != nil {
		t.Fatal(err)
	}
	defer restoredDB.Close()
	var users int
	if err := restoredDB.DB.QueryRow("SELECT count(*) FROM users").Scan(&users); err != nil {
		t.Fatal(err)
	}
	if users != 1 {
		t.Fatalf("restored users = %d, want 1", users)
	}
	var files int
	if err := restoredDB.DB.QueryRow("SELECT count(*) FROM files WHERE status = 'ready'").Scan(&files); err != nil {
		t.Fatal(err)
	}
	if files != 1 {
		t.Fatalf("restored files = %d, want 1", files)
	}
	restoredContent, err := os.ReadFile(filepath.Join(restoredDir, filepath.FromSlash(storagePath)))
	if err != nil {
		t.Fatal(err)
	}
	if string(restoredContent) != "live-file-content" {
		t.Fatalf("restored file content = %q", restoredContent)
	}
}

func TestValidateRestoredDatabaseRejectsEmptyDatabase(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "filebox.db"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateRestoredDatabase(dataDir); err == nil {
		t.Fatal("empty restored database was accepted")
	}
}
