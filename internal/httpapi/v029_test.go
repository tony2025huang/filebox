package httpapi

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAdminClearUserFilesRequiresAdminAndReauth 覆盖 v029：按用户清空端点仅管理员可用，
// 需要二次认证，且只影响目标用户。
// TestAdminClearUserFilesRequiresAdminAndReauth covers v029: the per-user clear endpoint is
// admin-only, requires re-authentication, and only affects the target user.
func TestAdminClearUserFilesRequiresAdminAndReauth(t *testing.T) {
	_, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)
	targetID, targetToken := createUserAndLogin(t, handler, adminToken, "clear-target", "ClrTarget!9")
	_, otherToken := createUserAndLogin(t, handler, adminToken, "clear-other", "ClrOther!9")
	uploadTestFile(t, handler, targetToken, "target-file.txt", "text/plain", []byte("target content"))
	uploadTestFile(t, handler, otherToken, "other-file.txt", "text/plain", []byte("other content"))

	forbidden := testJSONRequest(t, handler, http.MethodPost, "/api/admin/users/"+formatID(targetID)+"/clear-files", otherToken, `{"password":"ClrOther!9"}`)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("non-admin per-user clear = %d, want 403", forbidden.Code)
	}
	denied := testJSONRequest(t, handler, http.MethodPost, "/api/admin/users/"+formatID(targetID)+"/clear-files", adminToken, `{"password":"wrong-password"}`)
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("per-user clear with wrong password = %d, want 401: %s", denied.Code, denied.Body.String())
	}
	cleared := testJSONRequest(t, handler, http.MethodPost, "/api/admin/users/"+formatID(targetID)+"/clear-files", adminToken, `{"password":"admin123"}`)
	if cleared.Code != http.StatusOK {
		t.Fatalf("per-user clear = %d: %s", cleared.Code, cleared.Body.String())
	}
	if data := responseData(t, cleared); data["files"] != float64(1) || data["username"] != "clear-target" {
		t.Fatalf("per-user clear payload = %v", data)
	}
	targetFiles := testJSONRequest(t, handler, http.MethodGet, "/api/files", targetToken, "")
	if responseData(t, targetFiles)["total"] != float64(0) {
		t.Fatalf("target files after clear = %s", targetFiles.Body.String())
	}
	otherFiles := testJSONRequest(t, handler, http.MethodGet, "/api/files", otherToken, "")
	if responseData(t, otherFiles)["total"] != float64(1) {
		t.Fatalf("other user's files must survive = %s", otherFiles.Body.String())
	}
}

// TestDeleteUserKeepsFilesInRecycleBin 覆盖 v029：删除用户时选择保留文件 → 文件迁入回收站，
// 目录以原用户名命名，分享记录被清理，且可通过回收站接口列出/彻底删除。
// TestDeleteUserKeepsFilesInRecycleBin covers v029: deleting a user while keeping files moves them
// into the recycle bin under a directory named after the user, cleans up their shares, and exposes
// them through the recycle endpoints.
func TestDeleteUserKeepsFilesInRecycleBin(t *testing.T) {
	db, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)
	userID, token := createUserAndLogin(t, handler, adminToken, "recycle-me", "Recycle!2026")
	if folder := testJSONRequest(t, handler, http.MethodPost, "/api/folders", token, `{"name":"docs"}`); folder.Code != http.StatusCreated {
		t.Fatalf("create folder = %d: %s", folder.Code, folder.Body.String())
	}
	uploadToDir(t, handler, token, "keep.txt", "text/plain", "docs", []byte("recycled content"))
	fileList := testJSONRequest(t, handler, http.MethodGet, "/api/files?dir=docs", token, "")
	fileID := int64(responseData(t, fileList)["items"].([]any)[0].(map[string]any)["id"].(float64))
	if created := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+formatID(fileID)+"/share", token, `{"expiresInHours":24,"maxDownloads":0}`); created.Code != http.StatusCreated {
		t.Fatalf("create share = %d: %s", created.Code, created.Body.String())
	}

	deleted := testJSONRequest(t, handler, http.MethodDelete, "/api/admin/users/"+formatID(userID), adminToken, `{"keepFiles":true}`)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete user keeping files = %d: %s", deleted.Code, deleted.Body.String())
	}
	if responseData(t, deleted)["recycled"] != float64(1) {
		t.Fatalf("recycled count = %v", responseData(t, deleted))
	}

	var recycledOwner int64
	var recycledPath string
	if err := db.DB.QueryRow("SELECT user_id, storage_path FROM files WHERE status = 'ready'").Scan(&recycledOwner, &recycledPath); err != nil {
		t.Fatal(err)
	}
	if recycledOwner != 0 {
		t.Fatalf("recycled owner = %d, want 0", recycledOwner)
	}
	segments := strings.Split(filepath.ToSlash(recycledPath), "/")
	if len(segments) < 4 || segments[0] != "files" || segments[1] != "0" || segments[2] != "recycle-me" {
		t.Fatalf("recycled path %q must live under files/0/recycle-me/", recycledPath)
	}
	if _, err := os.Stat(filepath.Join(db.DataDir, recycledPath)); err != nil {
		t.Fatalf("recycled content missing on disk: %v", err)
	}
	var shareRows int
	if err := db.DB.QueryRow("SELECT COUNT(id) FROM shares WHERE created_by = ?", userID).Scan(&shareRows); err != nil {
		t.Fatal(err)
	}
	if shareRows != 0 {
		t.Fatalf("deleted user's shares must be cleaned, found %d", shareRows)
	}
	if _, err := os.Stat(filepath.Join(db.DataDir, "files", formatID(userID))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("former user directory must be gone: %v", err)
	}

	listed := testJSONRequest(t, handler, http.MethodGet, "/api/admin/recycle", adminToken, "")
	items, ok := responseData(t, listed)["items"].([]any)
	if listed.Code != http.StatusOK || !ok || len(items) != 1 {
		t.Fatalf("recycle list = %d %s", listed.Code, listed.Body.String())
	}
	entry := items[0].(map[string]any)
	if entry["recycleUser"] != "recycle-me" || entry["relativePath"] != "docs/keep.txt" {
		t.Fatalf("recycle entry = %v", entry)
	}
	recycledID := int64(entry["id"].(float64))
	if removed := testJSONRequest(t, handler, http.MethodDelete, "/api/admin/recycle/"+formatID(recycledID), adminToken, ""); removed.Code != http.StatusOK {
		t.Fatalf("delete recycled file = %d: %s", removed.Code, removed.Body.String())
	}
	after := testJSONRequest(t, handler, http.MethodGet, "/api/admin/recycle", adminToken, "")
	if responseData(t, after)["total"] != float64(0) {
		t.Fatalf("recycle bin after delete = %s", after.Body.String())
	}
}

// TestDeleteUserRemovesFilesByDefault 覆盖 v029：默认（不传 keepFiles）仍删除该用户全部文件。
// TestDeleteUserRemovesFilesByDefault covers v029: the default (no keepFiles) still deletes files.
func TestDeleteUserRemovesFilesByDefault(t *testing.T) {
	db, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)
	userID, token := createUserAndLogin(t, handler, adminToken, "purge-me", "Purge!2026")
	uploadTestFile(t, handler, token, "gone.txt", "text/plain", []byte("gone"))

	deleted := testJSONRequest(t, handler, http.MethodDelete, "/api/admin/users/"+formatID(userID), adminToken, "")
	if deleted.Code != http.StatusOK {
		t.Fatalf("default delete = %d: %s", deleted.Code, deleted.Body.String())
	}
	if responseData(t, deleted)["recycled"] != float64(0) {
		t.Fatalf("default delete must not recycle: %v", responseData(t, deleted))
	}
	var remaining int
	if err := db.DB.QueryRow("SELECT COUNT(id) FROM files WHERE user_id = ? OR user_id = 0", userID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("files must be deleted by default, found %d", remaining)
	}
	if _, err := os.Stat(filepath.Join(db.DataDir, "files", formatID(userID))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("user directory must be gone: %v", err)
	}
}
