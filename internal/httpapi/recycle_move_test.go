package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMoveRecycleFilesIntoUserDirectory 覆盖 v041 的核心路径：回收站文件能被移动到指定用户的指定目录，
// 磁盘与数据库一致、目标用户 used_bytes 增加、回收站不再列出该文件。
func TestMoveRecycleFilesIntoUserDirectory(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	ctx := context.Background()

	if err := db.CreateUser(ctx, "mover", "hash", "user", 10*1024*1024*1024); err != nil {
		t.Fatalf("create target user: %v", err)
	}
	target, err := db.GetUserByUsername("mover")
	if err != nil {
		t.Fatalf("load target user: %v", err)
	}

	rel := "files/0/olduser/notes.txt"
	full := filepath.Join(db.DataDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(0, 'notes.txt', 'notes.txt', 5, 'text/plain', '', '', 'ready', ?, ?)", rel, "2026-09-13T00:00:00Z")
	if err != nil {
		t.Fatalf("seed recycle file: %v", err)
	}
	fileID, _ := res.LastInsertId()

	body := fmt.Sprintf(`{"fileIds":[%d],"targetUserId":%d,"targetDir":"docs/2026"}`, fileID, target.ID)
	move := testJSONRequest(t, handler, http.MethodPost, "/api/admin/recycle/move", token, body)
	if move.Code != http.StatusOK {
		t.Fatalf("move = %d: %s", move.Code, move.Body.String())
	}

	var ownerID int64
	var storagePath string
	if err := db.DB.QueryRow("SELECT user_id, storage_path FROM files WHERE id = ?", fileID).Scan(&ownerID, &storagePath); err != nil {
		t.Fatal(err)
	}
	wantPrefix := fmt.Sprintf("files/%d/docs/2026/", target.ID)
	if ownerID != target.ID || !strings.HasPrefix(filepath.ToSlash(storagePath), wantPrefix) {
		t.Fatalf("file row after move: owner=%d path=%q want owner=%d prefix=%q", ownerID, storagePath, target.ID, wantPrefix)
	}
	if _, err := os.Stat(filepath.Join(db.DataDir, filepath.FromSlash(storagePath))); err != nil {
		t.Fatalf("moved file missing on disk: %v", err)
	}
	if _, err := os.Stat(full); !os.IsNotExist(err) {
		t.Fatalf("source file must be gone after the move, stat err=%v", err)
	}
	updated, err := db.GetUser(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.UsedBytes != 5 {
		t.Fatalf("target used_bytes = %d, want 5", updated.UsedBytes)
	}
	recycle := testJSONRequest(t, handler, http.MethodGet, "/api/admin/recycle", token, "")
	if strings.Contains(recycle.Body.String(), "notes.txt") {
		t.Fatalf("moved file must leave the recycle listing: %s", recycle.Body.String())
	}
}

// TestMoveRecycleFilesRejectsQuotaAndBadDir 覆盖两条拒绝路径：目标用户配额不足（403 + 配额明细）、
// 目标目录非法（400，沿用上传目录校验，不能借移动绕过）。
func TestMoveRecycleFilesRejectsQuotaAndBadDir(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	ctx := context.Background()

	if err := db.CreateUser(ctx, "tiny", "hash", "user", 1); err != nil {
		t.Fatalf("create tight user: %v", err)
	}
	tight, err := db.GetUserByUsername("tiny")
	if err != nil {
		t.Fatal(err)
	}

	rel := "files/0/olduser/big.txt"
	full := filepath.Join(db.DataDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(0, 'big.txt', 'big.txt', 10, 'text/plain', '', '', 'ready', ?, ?)", rel, "2026-09-13T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	fileID, _ := res.LastInsertId()

	quotaBody := fmt.Sprintf(`{"fileIds":[%d],"targetUserId":%d,"targetDir":""}`, fileID, tight.ID)
	quota := testJSONRequest(t, handler, http.MethodPost, "/api/admin/recycle/move", token, quotaBody)
	if quota.Code != http.StatusForbidden || !strings.Contains(quota.Body.String(), "QUOTA_EXCEEDED") {
		t.Fatalf("quota rejection = %d: %s", quota.Code, quota.Body.String())
	}
	if _, err := os.Stat(full); err != nil {
		t.Fatalf("a rejected move must leave the file in place: %v", err)
	}

	badBody := fmt.Sprintf(`{"fileIds":[%d],"targetUserId":%d,"targetDir":"../evil"}`, fileID, tight.ID)
	bad := testJSONRequest(t, handler, http.MethodPost, "/api/admin/recycle/move", token, badBody)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid target dir = %d: %s", bad.Code, bad.Body.String())
	}
}
