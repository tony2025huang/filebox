package httpapi

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// uploadToDir 在指定用户相对目录内上传一个小文件（子目录场景）。
// uploadToDir uploads a small file into a user-relative directory (sub-folder scenario).
func uploadToDir(t *testing.T, handler http.Handler, token, name, mimeType, dir string, content []byte) map[string]any {
	t.Helper()
	init := testJSONRequest(t, handler, http.MethodPost, "/api/files/upload-init", token,
		`{"name":"`+name+`","size":`+formatID(int64(len(content)))+`,"chunkSize":0,"mime":"`+mimeType+`","dir":"`+dir+`"}`)
	if init.Code != http.StatusOK {
		t.Fatalf("upload-init in dir %q = %d: %s", dir, init.Code, init.Body.String())
	}
	taskID := responseData(t, init)["taskId"].(string)
	chunk := testBinaryRequest(t, handler, http.MethodPut, "/api/files/"+taskID+"/chunks/0", token, content)
	if chunk.Code != http.StatusOK {
		t.Fatalf("chunk upload = %d: %s", chunk.Code, chunk.Body.String())
	}
	complete := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+taskID+"/complete", token, `{}`)
	if complete.Code != http.StatusOK {
		t.Fatalf("upload complete = %d: %s", complete.Code, complete.Body.String())
	}
	return responseData(t, complete)
}

// createUserAndLogin 以管理员身份创建普通用户并登录，返回用户 ID 与令牌。
// createUserAndLogin creates a regular user through the admin API and logs in.
func createUserAndLogin(t *testing.T, handler http.Handler, adminToken, username, password string) (int64, string) {
	t.Helper()
	created := testJSONRequest(t, handler, http.MethodPost, "/api/admin/users", adminToken,
		`{"username":"`+username+`","password":"`+password+`","role":"user","quotaBytes":1048576}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create user %s = %d: %s", username, created.Code, created.Body.String())
	}
	userID := int64(responseData(t, created)["id"].(float64))
	login := testJSONRequest(t, handler, http.MethodPost, "/api/auth/login", "", `{"username":"`+username+`","password":"`+password+`"}`)
	if login.Code != http.StatusOK {
		t.Fatalf("login %s = %d: %s", username, login.Code, login.Body.String())
	}
	return userID, responseData(t, login)["token"].(string)
}

// TestClearAllOwnScopeClearsFilesFoldersTasksSharesAndEmptyDirs 覆盖 v028 #1：个人范围清空必须同时
// 清理文件、目录记录、未完成上传任务、软撤销分享，并删除磁盘上的空目录。
// TestClearAllOwnScopeClearsFilesFoldersTasksSharesAndEmptyDirs covers v028 #1: an own-scope clear
// removes files, folder records, unfinished upload tasks and revokes shares, and prunes empty
// directories on disk.
func TestClearAllOwnScopeClearsFilesFoldersTasksSharesAndEmptyDirs(t *testing.T) {
	db, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)
	userID, token := createUserAndLogin(t, handler, adminToken, "clear-own-scope", "Clear0wn!pass")

	if folder := testJSONRequest(t, handler, http.MethodPost, "/api/folders", token, `{"name":"sub"}`); folder.Code != http.StatusCreated {
		t.Fatalf("create folder = %d: %s", folder.Code, folder.Body.String())
	}
	uploadToDir(t, handler, token, "s.txt", "text/plain", "sub", []byte("sub content"))
	uploaded := uploadTestFile(t, handler, token, "r.txt", "text/plain", []byte("root content"))
	fileID := int64(uploaded["id"].(float64))

	share := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+formatID(fileID)+"/share", token, `{"expiresInHours":24,"maxDownloads":0}`)
	if share.Code != http.StatusCreated {
		t.Fatalf("create share = %d: %s", share.Code, share.Body.String())
	}
	shareToken := responseData(t, share)["token"].(string)

	init := testJSONRequest(t, handler, http.MethodPost, "/api/files/upload-init", token, `{"name":"pend.bin","size":33,"chunkSize":0,"mime":"application/octet-stream"}`)
	if init.Code != http.StatusOK {
		t.Fatalf("init pending task = %d: %s", init.Code, init.Body.String())
	}
	taskID := responseData(t, init)["taskId"].(string)

	cleared := testJSONRequest(t, handler, http.MethodPost, "/api/files/clear-all", token, `{"password":"Clear0wn!pass"}`)
	if cleared.Code != http.StatusOK {
		t.Fatalf("clear-all = %d: %s", cleared.Code, cleared.Body.String())
	}
	data := responseData(t, cleared)
	if data["files"] != float64(2) || data["tasks"] != float64(1) || data["shares"] != float64(1) || data["scope"] != "own" {
		t.Fatalf("clear-all counts = %v", data)
	}

	files := testJSONRequest(t, handler, http.MethodGet, "/api/files", token, "")
	if responseData(t, files)["total"] != float64(0) {
		t.Fatalf("files after clear = %s", files.Body.String())
	}
	folders := testJSONRequest(t, handler, http.MethodGet, "/api/folders", token, "")
	if items, ok := responseData(t, folders)["items"].([]any); !ok || len(items) != 0 {
		t.Fatalf("folders after clear = %s", folders.Body.String())
	}
	if status := testJSONRequest(t, handler, http.MethodGet, "/api/files/"+taskID+"/status", token, ""); status.Code != http.StatusNotFound {
		t.Fatalf("unfinished upload task survived clear-all: %d", status.Code)
	}
	shares := testJSONRequest(t, handler, http.MethodGet, "/api/shares", token, "")
	for _, item := range responseData(t, shares)["items"].([]any) {
		if item.(map[string]any)["status"] != "revoked" {
			t.Fatalf("share not revoked after clear-all: %v", item)
		}
	}
	meta := testJSONRequest(t, handler, http.MethodGet, "/api/files/shared/"+shareToken+"/meta", "", "")
	if meta.Code != http.StatusNotFound || responseData(t, meta)["code"] != "SHARE_REVOKED" {
		t.Fatalf("public link after clear-all = %d %s", meta.Code, meta.Body.String())
	}
	if _, err := os.Stat(filepath.Join(db.DataDir, "files", formatID(userID), "sub")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty directory was not pruned after clear-all: %v", err)
	}
}

// TestClearAllGlobalScopeRemoved 覆盖 v029：全库清空已从文件库移除（改为用户管理按用户清空），
// 任何角色传入 scope=all 都应被拒绝。
// TestClearAllGlobalScopeRemoved covers v029: the global wipe moved out of the file library (per-user
// clear now lives in user management), so scope=all is rejected for every role.
func TestClearAllGlobalScopeRemoved(t *testing.T) {
	_, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)
	_, userToken := createUserAndLogin(t, handler, adminToken, "clear-all-forbidden", "ClrAll!pass9")
	uploadTestFile(t, handler, userToken, "user-file.txt", "text/plain", []byte("user content"))

	for name, token := range map[string]string{"admin": adminToken, "user": userToken} {
		rejected := testJSONRequest(t, handler, http.MethodPost, "/api/files/clear-all", token, `{"password":"admin123","scope":"all"}`)
		if rejected.Code != http.StatusBadRequest || responseData(t, rejected)["code"] != "SCOPE_UNSUPPORTED" {
			t.Fatalf("%s scope=all = %d %s, want 400 SCOPE_UNSUPPORTED", name, rejected.Code, rejected.Body.String())
		}
	}
	files := testJSONRequest(t, handler, http.MethodGet, "/api/files", userToken, "")
	if responseData(t, files)["total"] != float64(1) {
		t.Fatalf("files must be untouched after rejected global scope: %s", files.Body.String())
	}
}

// TestClearAllDiskModeWholeTreeRemovesUserTree 覆盖 v028 #1：whole-tree 模式删除用户整个存储目录树。
// TestClearAllDiskModeWholeTreeRemovesUserTree covers v028 #1: whole-tree mode removes the user's
// entire storage tree.
func TestClearAllDiskModeWholeTreeRemovesUserTree(t *testing.T) {
	db, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)
	userID, token := createUserAndLogin(t, handler, adminToken, "clear-whole-tree", "ClrTree!pass9")
	uploadTestFile(t, handler, token, "tree.txt", "text/plain", []byte("content"))
	userRoot := filepath.Join(db.DataDir, "files", formatID(userID))
	if _, err := os.Stat(userRoot); err != nil {
		t.Fatalf("expected user storage tree before clear: %v", err)
	}
	cleared := testJSONRequest(t, handler, http.MethodPost, "/api/files/clear-all", token, `{"password":"ClrTree!pass9","diskMode":"whole-tree"}`)
	if cleared.Code != http.StatusOK {
		t.Fatalf("clear-all whole-tree = %d: %s", cleared.Code, cleared.Body.String())
	}
	if responseData(t, cleared)["diskMode"] != "whole-tree" {
		t.Fatalf("diskMode echo = %v", responseData(t, cleared))
	}
	if _, err := os.Stat(userRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("user storage tree survived whole-tree clear: %v", err)
	}
}

// TestShareErrorsDistinguishMissingLinkAndMissingContent 覆盖 v028 #6：公开分享端点必须区分
// "链接不存在/已撤销/已过期" 与 "分享内容不存在"。
// TestShareErrorsDistinguishMissingLinkAndMissingContent covers v028 #6: anonymous share endpoints
// distinguish a missing/revoked/expired link from missing content.
func TestShareErrorsDistinguishMissingLinkAndMissingContent(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)

	missing := testJSONRequest(t, handler, http.MethodGet, "/api/files/shared/no-such-token/meta", "", "")
	if missing.Code != http.StatusNotFound || responseData(t, missing)["code"] != "SHARE_NOT_FOUND" {
		t.Fatalf("unknown token = %d %s, want SHARE_NOT_FOUND", missing.Code, missing.Body.String())
	}

	uploaded := uploadTestFile(t, handler, token, "content-missing.txt", "text/plain", []byte("content"))
	fileID := int64(uploaded["id"].(float64))
	share := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+formatID(fileID)+"/share", token, `{"expiresInHours":24,"maxDownloads":0}`)
	shareToken := responseData(t, share)["token"].(string)
	if meta := testJSONRequest(t, handler, http.MethodGet, "/api/files/shared/"+shareToken+"/meta", "", ""); meta.Code != http.StatusOK {
		t.Fatalf("share meta before delete = %d: %s", meta.Code, meta.Body.String())
	}
	if deleted := testJSONRequest(t, handler, http.MethodDelete, "/api/files/"+formatID(fileID), token, ""); deleted.Code != http.StatusOK {
		t.Fatalf("delete file = %d: %s", deleted.Code, deleted.Body.String())
	}
	contentMissing := testJSONRequest(t, handler, http.MethodGet, "/api/files/shared/"+shareToken+"/meta", "", "")
	if contentMissing.Code != http.StatusNotFound || responseData(t, contentMissing)["code"] != "SHARE_CONTENT_MISSING" {
		t.Fatalf("deleted content meta = %d %s, want SHARE_CONTENT_MISSING", contentMissing.Code, contentMissing.Body.String())
	}
	downloadMissing := testJSONRequest(t, handler, http.MethodGet, "/api/files/shared/"+shareToken+"/download", "", "")
	if downloadMissing.Code != http.StatusNotFound || responseData(t, downloadMissing)["code"] != "SHARE_CONTENT_MISSING" {
		t.Fatalf("deleted content download = %d %s, want SHARE_CONTENT_MISSING", downloadMissing.Code, downloadMissing.Body.String())
	}

	revokedFile := uploadTestFile(t, handler, token, "revoked-link.txt", "text/plain", []byte("keep"))
	revokedFileID := int64(revokedFile["id"].(float64))
	revokedShare := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+formatID(revokedFileID)+"/share", token, `{"expiresInHours":24,"maxDownloads":0}`)
	revokedToken := responseData(t, revokedShare)["token"].(string)
	if revoke := testJSONRequest(t, handler, http.MethodDelete, "/api/shares/"+revokedToken, token, ""); revoke.Code != http.StatusOK {
		t.Fatalf("revoke share = %d: %s", revoke.Code, revoke.Body.String())
	}
	revokedMeta := testJSONRequest(t, handler, http.MethodGet, "/api/files/shared/"+revokedToken+"/meta", "", "")
	if revokedMeta.Code != http.StatusNotFound || responseData(t, revokedMeta)["code"] != "SHARE_REVOKED" {
		t.Fatalf("revoked link meta = %d %s, want SHARE_REVOKED", revokedMeta.Code, revokedMeta.Body.String())
	}

	expiredFile := uploadTestFile(t, handler, token, "expired-link.txt", "text/plain", []byte("keep"))
	expiredFileID := int64(expiredFile["id"].(float64))
	expiredShare := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+formatID(expiredFileID)+"/share", token, `{"expiresInHours":24,"maxDownloads":0}`)
	expiredToken := responseData(t, expiredShare)["token"].(string)
	if _, err := db.DB.Exec("UPDATE shares SET expires_at = ? WHERE token = ?", time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), expiredToken); err != nil {
		t.Fatal(err)
	}
	expiredMeta := testJSONRequest(t, handler, http.MethodGet, "/api/files/shared/"+expiredToken+"/meta", "", "")
	if expiredMeta.Code != http.StatusNotFound || responseData(t, expiredMeta)["code"] != "SHARE_EXPIRED" {
		t.Fatalf("expired link meta = %d %s, want SHARE_EXPIRED", expiredMeta.Code, expiredMeta.Body.String())
	}
}

// TestSharesListMarksContentMissing 覆盖 v028 #6：文件被删除后，"我的分享"应标记内容缺失。
// TestSharesListMarksContentMissing covers v028 #6: after the file is deleted the owner's share
// list marks the link as content-missing.
func TestSharesListMarksContentMissing(t *testing.T) {
	_, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	uploaded := uploadTestFile(t, handler, token, "list-missing.txt", "text/plain", []byte("content"))
	fileID := int64(uploaded["id"].(float64))
	share := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+formatID(fileID)+"/share", token, `{"expiresInHours":24,"maxDownloads":0}`)
	shareToken := responseData(t, share)["token"].(string)
	if deleted := testJSONRequest(t, handler, http.MethodDelete, "/api/files/"+formatID(fileID), token, ""); deleted.Code != http.StatusOK {
		t.Fatalf("delete file = %d: %s", deleted.Code, deleted.Body.String())
	}
	shares := testJSONRequest(t, handler, http.MethodGet, "/api/shares", token, "")
	found := false
	for _, item := range responseData(t, shares)["items"].([]any) {
		entry := item.(map[string]any)
		if entry["token"] != shareToken {
			continue
		}
		found = true
		if entry["status"] != "content_missing" || entry["contentMissing"] != true {
			t.Fatalf("share list entry = %v, want content_missing", entry)
		}
	}
	if !found {
		t.Fatalf("share entry not found in list: %s", shares.Body.String())
	}
}
