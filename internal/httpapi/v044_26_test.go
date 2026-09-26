package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
)

// listNames 取出文件列表响应里的文件名（用于断言列表可见性）。
// listNames returns the file names from a list response.
func listNames(t *testing.T, recorder *httptest.ResponseRecorder) []string {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("list = %d: %s", recorder.Code, recorder.Body.String())
	}
	items, ok := responseData(t, recorder)["items"].([]any)
	if !ok {
		t.Fatalf("list items type = %T", responseData(t, recorder)["items"])
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.(map[string]any)["name"].(string))
	}
	return names
}

// TestRecycleMoveVisibleToTargetUser 覆盖 v044.26：管理员把回收站文件"移动到用户目录"后，
// 目标用户必须在自己的文件库里真正看到它（根列表与目录视图各一）。
// 曾经的缺陷：写入用 filepath.ToSlash（正斜杠），而普通用户的目录/根目录过滤按本机分隔符
// 做前缀比较，于是 /me 计数与配额都算上了这些文件，界面上却哪儿都不显示（只有管理员的全库
// 视图因为做了 REPLACE 归一化而看得到）。
// TestRecycleMoveVisibleToTargetUser covers v044.26: after moving recycle-bin files into a user's
// space, that user must actually see them in the file library (root listing and directory view).
func TestRecycleMoveVisibleToTargetUser(t *testing.T) {
	db, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)

	sourceID, sourceToken := createUserAndLogin(t, handler, adminToken, "move-source", "MoveSrc!9")
	uploadTestFile(t, handler, sourceToken, "root-move.txt", "text/plain", []byte("ROOTMOVE"))
	uploadToDir(t, handler, sourceToken, "dir-move.txt", "text/plain", "sub", []byte("DIRMOVE"))

	deleted := testJSONRequest(t, handler, http.MethodDelete, "/api/admin/users/"+formatID(sourceID), adminToken, `{"keepFiles":true}`)
	if deleted.Code != http.StatusOK || responseData(t, deleted)["recycled"] != float64(2) {
		t.Fatalf("delete user with keepFiles = %d: %s", deleted.Code, deleted.Body.String())
	}

	listed := testJSONRequest(t, handler, http.MethodGet, "/api/admin/recycle", adminToken, "")
	items, ok := responseData(t, listed)["items"].([]any)
	if listed.Code != http.StatusOK || !ok || len(items) != 2 {
		t.Fatalf("recycle list = %d: %s", listed.Code, listed.Body.String())
	}
	recycledIDs := map[string]int64{}
	for _, item := range items {
		entry := item.(map[string]any)
		recycledIDs[entry["name"].(string)] = int64(entry["id"].(float64))
	}

	targetID, targetToken := createUserAndLogin(t, handler, adminToken, "move-target", "MoveTgt!9")
	moveTo := func(name, dir string) {
		t.Helper()
		body := `{"fileIds":[` + formatID(recycledIDs[name]) + `],"targetUserId":` + formatID(targetID) + `,"targetDir":"` + dir + `"}`
		moved := testJSONRequest(t, handler, http.MethodPost, "/api/admin/recycle/move", adminToken, body)
		if moved.Code != http.StatusOK || responseData(t, moved)["moved"] != float64(1) {
			t.Fatalf("recycle move %s to %q = %d: %s", name, dir, moved.Code, moved.Body.String())
		}
	}
	moveTo("root-move.txt", "")
	moveTo("dir-move.txt", "sub")

	root := testJSONRequest(t, handler, http.MethodGet, "/api/files?page=1&pageSize=20", targetToken, "")
	if names := listNames(t, root); len(names) != 1 || names[0] != "root-move.txt" {
		t.Fatalf("target root listing = %v, want [root-move.txt]", names)
	}
	dir := testJSONRequest(t, handler, http.MethodGet, "/api/files?page=1&pageSize=20&dir=sub", targetToken, "")
	if names := listNames(t, dir); len(names) != 1 || names[0] != "dir-move.txt" {
		t.Fatalf("target sub/ listing = %v, want [dir-move.txt]", names)
	}
	me := responseData(t, testJSONRequest(t, handler, http.MethodGet, "/api/auth/me", targetToken, ""))
	if me["fileCount"] != float64(2) || me["usedBytes"] != float64(15) {
		t.Fatalf("target counts = fileCount %v, usedBytes %v; want 2, 15", me["fileCount"], me["usedBytes"])
	}

	// 写侧约定：storage_path 必须是本机分隔符写法（与上传/回收站落盘一致），
	// 否则目录过滤、文件夹展开、覆盖同名上传的相等比较都会失配。
	var storedPath string
	if err := db.DB.QueryRow("SELECT storage_path FROM files WHERE name = 'dir-move.txt'").Scan(&storedPath); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("files", strconv.FormatInt(targetID, 10), "sub", "dir-move.txt"); storedPath != want {
		t.Fatalf("stored storage_path = %q, want %q", storedPath, want)
	}

	// 内容与路径解析不受影响：移动后的文件仍可下载。
	download := testBinaryRequest(t, handler, http.MethodGet, "/api/files/"+formatID(recycledIDs["dir-move.txt"])+"/download", targetToken, nil)
	if download.Code != http.StatusOK || download.Body.String() != "DIRMOVE" {
		t.Fatalf("download moved file = %d %q", download.Code, download.Body.String())
	}
}
