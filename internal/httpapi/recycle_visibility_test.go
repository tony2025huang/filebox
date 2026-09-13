package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

// TestAdminFileListExcludesRecycleOwner 覆盖 v040：回收站保留账户（users.id = 0）的文件不得出现在
// 管理员的"全库"文件列表里（否则清空自己名下文件后仍会看到它们，看起来像清空无效），
// 但回收站视图必须仍能看到它们——两边都要成立，避免"排过头"把回收站功能一起关掉。
func TestAdminFileListExcludesRecycleOwner(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)

	if _, err := db.DB.Exec(
		"INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(0, 'recycled.txt', 'recycled.txt', 5, 'text/plain', '', '', 'ready', 'files/0/victim/recycled.txt', ?)",
		"2026-09-13T00:00:00Z"); err != nil {
		t.Fatalf("seed recycle file: %v", err)
	}

	list := testJSONRequest(t, handler, http.MethodGet, "/api/files", token, "")
	if list.Code != http.StatusOK {
		t.Fatalf("file list = %d: %s", list.Code, list.Body.String())
	}
	if strings.Contains(list.Body.String(), "recycled.txt") {
		t.Fatalf("recycle-bin file leaked into the admin library listing: %s", list.Body.String())
	}

	recycle := testJSONRequest(t, handler, http.MethodGet, "/api/admin/recycle", token, "")
	if recycle.Code != http.StatusOK {
		t.Fatalf("recycle listing = %d: %s", recycle.Code, recycle.Body.String())
	}
	if !strings.Contains(recycle.Body.String(), "recycled.txt") {
		t.Fatalf("recycle listing must still expose recycled files: %s", recycle.Body.String())
	}
}
