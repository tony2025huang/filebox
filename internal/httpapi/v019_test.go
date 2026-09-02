package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestListFoldersFiltersLegacyPaths v019 #4：历史遗留的非法目录路径（v010 反斜杠 uploads\xxx、`.`/`..`）
// 不再返回，files/ 前缀路径归一化为相对路径，避免前端点击目录触发「目录无效」。
// TestListFoldersFiltersLegacyPaths (v019 #4): legacy invalid folder paths (v010 backslash uploads\xxx, "."/"..")
// are dropped from the listing, and files/-prefixed paths are normalized, so navigating never hits "invalid directory".
func TestListFoldersFiltersLegacyPaths(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	// 正常目录（createFolder 会校验）
	normal := testJSONRequest(t, handler, http.MethodPost, "/api/folders", token, `{"name":"docs"}`)
	if normal.Code != http.StatusCreated {
		t.Fatalf("create normal folder = %d: %s", normal.Code, normal.Body.String())
	}
	// 模拟历史遗留记录：直接写库（绕过校验，与 v010 数据一致）
	legacyRows := [][]any{
		{`uploads\mixEN4VBRRs8p83z7bJMtuCnHzcsWA0iGo61wBHluzaKYhQioKdrvUZWtUxbQswB`},
		{"files/Net3.5/sxs2012"},
		{"files/2/docs"},
		{".."},
	}
	for _, row := range legacyRows {
		if _, err := db.DB.ExecContext(context.Background(), "INSERT INTO folders(user_id, parent_id, name, path, created_at) VALUES(?, NULL, ?, ?, ?)", 1, "legacy", row[0], "2026-09-01T00:00:00Z"); err != nil {
			t.Fatalf("seed legacy folder %v: %v", row[0], err)
		}
	}
	list := testJSONRequest(t, handler, http.MethodGet, "/api/folders", token, "")
	if list.Code != http.StatusOK {
		t.Fatalf("list folders = %d: %s", list.Code, list.Body.String())
	}
	items := responseData(t, list)["items"].([]any)
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.(map[string]any)["path"].(string)] = true
	}
	// 正常目录保留
	if !seen["docs"] {
		t.Fatalf("normal folder 'docs' missing from listing: %v", seen)
	}
	// 非法/遗留路径被过滤
	for _, bad := range []string{`uploads\mixEN4VBRRs8p83z7bJMtuCnHzcsWA0iGo61wBHluzaKYhQioKdrvUZWtUxbQswB`, "..", "."} {
		if seen[bad] {
			t.Fatalf("legacy invalid path %q should be filtered, got %v", bad, seen)
		}
	}
	// files/ 前缀归一化
	if !seen["Net3.5/sxs2012"] {
		t.Fatalf("files/Net3.5/sxs2012 should normalize to Net3.5/sxs2012, got %v", seen)
	}
	if !seen["docs"] && seen["files/2/docs"] {
		t.Fatalf("files/2/docs should normalize to docs (collides with existing docs; either acceptable as long as no files/ prefix remains): %v", seen)
	}
	for path := range seen {
		if strings.HasPrefix(path, "files/") || strings.Contains(path, "\\") || strings.HasPrefix(path, "uploads/") {
			t.Fatalf("path %q still carries legacy prefix/backslash", path)
		}
	}
	// 归一化后的目录点击导航不再报「目录无效」
	nav := testJSONRequest(t, handler, http.MethodGet, "/api/files?page=1&pageSize=20&dir="+url.QueryEscape("Net3.5/sxs2012"), token, "")
	if nav.Code == http.StatusBadRequest || strings.Contains(nav.Body.String(), "目录无效") {
		t.Fatalf("navigating normalized dir returned error: %d %s", nav.Code, nav.Body.String())
	}
}

// TestValidateFolderNameRejectsDotDirs v019 #4：目录名 "." / ".." 必须被拒绝，防止创建出点击必 400 的目录。
// TestValidateFolderNameRejectsDotDirs (v019 #4): folder names "." and ".." are rejected so no un-navigable folder is created.
func TestValidateFolderNameRejectsDotDirs(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	for _, name := range []string{".", ".."} {
		res := testJSONRequest(t, handler, http.MethodPost, "/api/folders", token, `{"name":"`+name+`"}`)
		if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "目录名无效") {
			t.Fatalf("create folder %q = %d: %s (want 400 目录名无效)", name, res.Code, res.Body.String())
		}
	}
	var count int
	if err := db.DB.QueryRow("SELECT COUNT(id) FROM folders").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("dot folders should not be created, got %d rows", count)
	}
}

// TestNormalizeFolderPath 覆盖归一化辅助函数的分支。
// TestNormalizeFolderPath covers the normalization helper branches.
func TestNormalizeFolderPath(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		allowed bool
	}{
		{"docs", "docs", true},
		{"a/b", "a/b", true},
		{"files/Net3.5/sxs2012", "Net3.5/sxs2012", true},
		{"files/2/docs", "docs", true},
		{"files/OpenJDK", "OpenJDK", true},
		{`uploads\mixEN4VBRR`, "", false},
		{"..", "", false},
		{".", "", false},
		{"/abs/path", "", false},
	}
	for _, tc := range cases {
		got, ok := normalizeFolderPath(tc.in)
		if ok != tc.allowed || got != tc.want {
			t.Fatalf("normalizeFolderPath(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.allowed)
		}
	}
}
