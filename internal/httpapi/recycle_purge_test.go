package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// TestPurgeRecycleRequiresReauth 覆盖 v041：清空回收站必须二次认证——不带凭据或凭据错误时既不能清空，
// 也不能删除回收站里的文件（安全等级与"清空全部文件"一致）。
func TestPurgeRecycleRequiresReauth(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)

	rel := "files/0/olduser/keep.txt"
	full := filepath.Join(db.DataDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(0, 'keep.txt', 'keep.txt', 4, 'text/plain', '', '', 'ready', ?, ?)", rel, "2026-09-13T00:00:00Z"); err != nil {
		t.Fatalf("seed recycle file: %v", err)
	}

	countRecycle := func() int {
		var count int
		if err := db.DB.QueryRow("SELECT COUNT(*) FROM files WHERE user_id = 0 AND status = 'ready'").Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	if got := countRecycle(); got != 1 {
		t.Fatalf("seed count = %d, want 1", got)
	}

	// 无凭据：必须被拒绝，回收站内容不变。
	missing := testJSONRequest(t, handler, http.MethodPost, "/api/admin/recycle/purge", token, `{}`)
	if missing.Code != http.StatusUnauthorized {
		t.Fatalf("purge without credentials = %d: %s", missing.Code, missing.Body.String())
	}
	if got := countRecycle(); got != 1 {
		t.Fatalf("recycle bin must be untouched after a rejected purge, count=%d", got)
	}
	if _, err := os.Stat(full); err != nil {
		t.Fatalf("recycled file must still exist on disk: %v", err)
	}

	// 错误密码：同样拒绝。
	wrong := testJSONRequest(t, handler, http.MethodPost, "/api/admin/recycle/purge", token, `{"password":"definitely-not-the-password"}`)
	if wrong.Code != http.StatusUnauthorized && wrong.Code != http.StatusTooManyRequests {
		t.Fatalf("purge with a wrong password = %d: %s", wrong.Code, wrong.Body.String())
	}
	if got := countRecycle(); got != 1 {
		t.Fatalf("recycle bin must be untouched after a wrong password, count=%d", got)
	}
}
