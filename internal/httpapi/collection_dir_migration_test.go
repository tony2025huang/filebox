package httpapi

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// TestCollectionDirMigrationRewritesPaths 覆盖 v030 #7 的迁移：collect upload 的落盘目录由
// uploads/<token> 改写为 collections/<收集名>-<token 前 8 位>，upload_tasks.storage_dir 与
// folders.path 同步改写，遗留的 uploads 目录记录被清理。
func TestCollectionDirMigrationRewritesPaths(t *testing.T) {
	db, handler := newTestServer(t)
	ownerToken := testAdminToken(t, handler)
	token := createProtectedCollectionForTest(t, handler, ownerToken, "我的收集 A")
	init := testJSONRequestWithCollectionPassword(t, handler, http.MethodPost, "/api/collections/"+token+"/upload-init", "test-secret", `{"name":"file.txt","size":1,"chunkSize":0,"dir":"folderA"}`)
	if init.Code != http.StatusOK {
		t.Fatalf("upload-init = %d: %s", init.Code, init.Body.String())
	}
	taskID := responseData(t, init)["taskId"].(string)

	var ownerID int64
	if err := db.DB.QueryRow("SELECT created_by FROM upload_collections WHERE token = ?", token).Scan(&ownerID); err != nil {
		t.Fatalf("owner lookup: %v", err)
	}
	var beforeDir string
	if err := db.DB.QueryRow("SELECT storage_dir FROM upload_tasks WHERE id = ?", taskID).Scan(&beforeDir); err != nil {
		t.Fatalf("task storage_dir: %v", err)
	}
	newFolderPath := "collections/我的收集-A-" + token[:8]
	// 收集目录在界面（GET /api/folders）显示用户语言的收集名，而不是带 token 后缀的落盘目录名（v030 #7）。
	// The folder listing must show the user-language collection name, not the token-suffixed path.
	folderList := testJSONRequest(t, handler, http.MethodGet, "/api/folders", ownerToken, "")
	if folderList.Code != http.StatusOK {
		t.Fatalf("list folders = %d: %s", folderList.Code, folderList.Body.String())
	}
	seenCollectionName := ""
	for _, entry := range responseData(t, folderList)["items"].([]any) {
		item := entry.(map[string]any)
		if item["path"] == newFolderPath {
			seenCollectionName, _ = item["name"].(string)
		}
	}
	if seenCollectionName != "我的收集 A" {
		t.Fatalf("folder listing name = %q want %q", seenCollectionName, "我的收集 A")
	}
	// 把任务与目录记录回退到旧布局，模拟升级前遗留的数据，再验证迁移能改写它们。
	// Rewind the task and folder records to the legacy layout so the migration has something to rewrite.
	legacyDir := filepath.ToSlash(filepath.Join("files", itoa(ownerID), "uploads", token))
	if _, err := db.DB.Exec("UPDATE upload_tasks SET storage_dir = ? WHERE id = ?", filepath.Join(legacyDir, "folderA"), taskID); err != nil {
		t.Fatalf("rewind task: %v", err)
	}
	if _, err := db.DB.Exec("UPDATE folders SET path = ? WHERE user_id = ? AND path = ?", "uploads/"+token+"/folderA", ownerID, newFolderPath+"/folderA"); err != nil {
		t.Fatalf("rewind nested folder: %v", err)
	}
	if _, err := db.DB.Exec("UPDATE folders SET path = ? WHERE user_id = ? AND path = ?", "uploads/"+token, ownerID, newFolderPath); err != nil {
		t.Fatalf("rewind collection folder: %v", err)
	}
	if _, err := db.DB.Exec("UPDATE folders SET path = 'uploads' WHERE user_id = ? AND path = 'collections'", ownerID); err != nil {
		t.Fatalf("rewind parent folder: %v", err)
	}
	if err := db.DB.QueryRow("SELECT storage_dir FROM upload_tasks WHERE id = ?", taskID).Scan(&beforeDir); err != nil {
		t.Fatalf("task storage_dir after rewind: %v", err)
	}
	if !strings.Contains(filepath.ToSlash(beforeDir), "/uploads/") {
		t.Fatalf("precondition failed: task dir %q is not on the legacy layout", beforeDir)
	}

	ctx := context.Background()
	migrations, err := db.ListCollectionDirMigrations(ctx)
	if err != nil {
		t.Fatalf("list migrations: %v", err)
	}
	matched := false
	for _, item := range migrations {
		if item.Token != token {
			continue
		}
		matched = true
		if !strings.HasPrefix(item.NewFolderPath, "collections/") || !strings.HasSuffix(item.NewFolderPath, "-"+token[:8]) {
			t.Fatalf("new folder path %q must be collections/<name>-<token8>", item.NewFolderPath)
		}
		files, folders, tasks, rewriteErr := db.RewriteCollectionStoragePaths(ctx, item)
		if rewriteErr != nil {
			t.Fatalf("rewrite paths: %v", rewriteErr)
		}
		if tasks != 1 {
			var taskCollectionID int64
			var rawDir string
			_ = db.DB.QueryRow("SELECT collection_id, storage_dir FROM upload_tasks WHERE id = ?", taskID).Scan(&taskCollectionID, &rawDir)
			t.Fatalf("upload_tasks rewritten = %d want 1 (planID=%d taskCollectionID=%d dir=%q oldDir=%q)", tasks, item.CollectionID, taskCollectionID, rawDir, item.OldFolderPath)
		}
		if folders < 2 {
			t.Fatalf("folders rewritten = %d want >= 2", folders)
		}
		if files != 0 {
			t.Fatalf("files rewritten = %d want 0 (no completed upload)", files)
		}
		break
	}
	if !matched {
		t.Fatalf("no migration plan for token %s", token)
	}

	var afterDir string
	if err := db.DB.QueryRow("SELECT storage_dir FROM upload_tasks WHERE id = ?", taskID).Scan(&afterDir); err != nil {
		t.Fatalf("task storage_dir after: %v", err)
	}
	wantPrefix := filepath.ToSlash(filepath.Join("files", itoa(ownerID), "collections", "我的收集-A-"+token[:8]))
	if !strings.HasPrefix(filepath.ToSlash(afterDir), wantPrefix) {
		t.Fatalf("storage_dir = %q want prefix %q", afterDir, wantPrefix)
	}

	var legacyFolders int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM folders WHERE user_id = ? AND (path = 'uploads' OR path LIKE 'uploads/%')", ownerID).Scan(&legacyFolders); err != nil {
		t.Fatal(err)
	}
	if legacyFolders != 0 {
		t.Fatalf("legacy uploads folder records left: %d", legacyFolders)
	}
	var legacyTasks int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM upload_tasks WHERE collection_id > 0 AND storage_dir LIKE '%/uploads/%'").Scan(&legacyTasks); err != nil {
		t.Fatal(err)
	}
	if legacyTasks != 0 {
		t.Fatalf("tasks still on the legacy layout: %d", legacyTasks)
	}
}
