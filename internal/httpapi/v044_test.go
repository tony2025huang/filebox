package httpapi

import (
	"net/http"
	"testing"
)

// TestMeCountsMatchFileListAndFilesTable 覆盖 v044.25：/api/auth/me 的 fileCount 曾恒为 0
// —— GetUser/GetUserByUsername 的 SELECT 里没有计数子查询，只有 ListUsers 有，
// 于是清空弹窗显示"名下有 0 个文件"而文件列表里却有文件。
// 本用例要求四处数字同源一致：进入 /me 的用户对象、登录返回的用户对象、/api/files 列表、files 表。
// TestMeCountsMatchFileListAndFilesTable covers v044.25: /api/auth/me reported fileCount = 0 because
// GetUser/GetUserByUsername lacked the count subqueries that ListUsers had, so the clear dialog
// claimed the user owned zero files while the file list showed files. The test pins four sources together.
func TestMeCountsMatchFileListAndFilesTable(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	uploadTestFile(t, handler, token, "count-a.txt", "text/plain", []byte("abcde"))
	uploadTestFile(t, handler, token, "count-b.txt", "text/plain", []byte("0123456"))
	uploadTestFile(t, handler, token, "count-c.txt", "text/plain", []byte("xy"))

	var tableCount, tableBytes int64
	if err := db.DB.QueryRow("SELECT COUNT(id), COALESCE(SUM(size), 0) FROM files WHERE status = 'ready' AND user_id = (SELECT id FROM users WHERE username = 'admin')").Scan(&tableCount, &tableBytes); err != nil {
		t.Fatal(err)
	}
	if tableCount != 3 || tableBytes != 14 {
		t.Fatalf("files table = %d rows, %d bytes; want 3 rows, 14 bytes", tableCount, tableBytes)
	}

	me := responseData(t, testJSONRequest(t, handler, http.MethodGet, "/api/auth/me", token, ""))
	list := responseData(t, testJSONRequest(t, handler, http.MethodGet, "/api/files?page=1&pageSize=50", token, ""))
	login := responseData(t, testJSONRequest(t, handler, http.MethodPost, "/api/auth/login", "", `{"username":"admin","password":"admin123"}`))

	var listBytes int64
	for _, item := range list["items"].([]any) {
		listBytes += int64(item.(map[string]any)["size"].(float64))
	}
	loginUser, ok := login["user"].(map[string]any)
	if !ok {
		t.Fatalf("login user type = %T", login["user"])
	}
	for name, source := range map[string]map[string]any{"me": me, "login": loginUser} {
		if got := source["fileCount"]; got != float64(tableCount) {
			t.Fatalf("%s fileCount = %#v, want %d", name, got, tableCount)
		}
		if got := source["usedBytes"]; got != float64(tableBytes) {
			t.Fatalf("%s usedBytes = %#v, want %d", name, got, tableBytes)
		}
	}
	if got := list["total"]; got != float64(tableCount) {
		t.Fatalf("file list total = %#v, want %d", got, tableCount)
	}
	if listBytes != tableBytes {
		t.Fatalf("file list bytes = %d, want %d", listBytes, tableBytes)
	}
}
