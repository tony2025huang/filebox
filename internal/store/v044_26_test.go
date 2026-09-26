package store

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// TestListFilesMatchesLegacyForwardSlashPaths 覆盖 v044.26：历史上回收站移出曾把 storage_path
// 写成 filepath.ToSlash 的正斜杠形式，列表的根目录/目录过滤必须归一化分隔符后才能命中这些记录
// （否则用户看得到配额被占、列表里却没有文件）。
// TestListFilesMatchesLegacyForwardSlashPaths covers v044.26: rows written with forward slashes
// (legacy recycle-move writer) must still be found by the root and directory listings.
func TestListFilesMatchesLegacyForwardSlashPaths(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.EnsureAdmin("admin", "admin123", 1024*1024); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := db.CreateUser(ctx, "legacy", "hash", "user", 1024*1024); err != nil {
		t.Fatal(err)
	}
	user, err := db.GetUserByUsername("legacy")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	root := strconv.FormatInt(user.ID, 10)
	insert := func(name, path string) {
		t.Helper()
		if _, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(?, ?, ?, 4, 'application/octet-stream', 'sha', 'md5', 'ready', ?, ?)", user.ID, name, name, path, now); err != nil {
			t.Fatal(err)
		}
	}
	// 两个都写成正斜杠：一个在用户根目录，一个在 sub/ 子目录。
	insert("legacy-root.txt", "files/"+root+"/legacy-root.txt")
	insert("legacy-dir.txt", "files/"+root+"/sub/legacy-dir.txt")
	// 对照组：本机分隔符写法（上传路径的惯例）必须继续命中。
	insert("native-root.txt", filepath.Join("files", root, "native-root.txt"))

	list := func(dir string) []string {
		t.Helper()
		files, _, err := db.ListFilesSorted(ctx, user.ID, false, "", dir, FileSort{By: "name"}, 1, 20)
		if err != nil {
			t.Fatal(err)
		}
		names := make([]string, 0, len(files))
		for _, file := range files {
			names = append(names, file.Name)
		}
		return names
	}

	rootNames := list("")
	if len(rootNames) != 2 || rootNames[0] != "legacy-root.txt" || rootNames[1] != "native-root.txt" {
		t.Fatalf("root listing = %v, want [legacy-root.txt native-root.txt]", rootNames)
	}
	dirNames := list("sub")
	if len(dirNames) != 1 || dirNames[0] != "legacy-dir.txt" {
		t.Fatalf("sub/ listing = %v, want [legacy-dir.txt]", dirNames)
	}
}
