package store

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

// TestShareGroupMemberCRUDAndAttributeEdit 验证聚合分享成员文件增删与属性编辑（v018 #2）。
// TestShareGroupMemberCRUDAndAttributeEdit verifies aggregate-share member add/remove and attribute edits (v018 #2).
func TestShareGroupMemberCRUDAndAttributeEdit(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.EnsureAdmin("admin", "admin123", 1024*1024); err != nil {
		t.Fatal(err)
	}
	user, err := db.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	fileIDs := make([]int64, 0, 3)
	for index := 0; index < 3; index++ {
		result, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(?, ?, ?, 1, 'text/plain', 'sha', 'md5', 'ready', ?, ?)", user.ID, "v018-"+strconv.Itoa(index), "v018-"+strconv.Itoa(index), "files/"+strconv.FormatInt(user.ID, 10)+"/v018-"+strconv.Itoa(index), now)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := result.LastInsertId()
		fileIDs = append(fileIDs, id)
	}
	group, _, err := db.CreateShareGroup(ctx, user.ID, "v018-group", fileIDs[:1], time.Now().UTC().Add(24*time.Hour).Format(time.RFC3339), 2)
	if err != nil {
		t.Fatal(err)
	}
	// 添加成员：fileIDs[0] 已在分享（跳过）、fileIDs[1]/fileIDs[2] 新增 → 共 2 个。
	added, err := db.AddShareGroupFiles(ctx, group.ID, []int64{fileIDs[0], fileIDs[1], fileIDs[2]})
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 2 {
		t.Fatalf("added = %d, want 2", len(added))
	}
	files, err := db.ListShareGroupFiles(ctx, group.ID)
	if err != nil || len(files) != 3 {
		t.Fatalf("member files = %d, %v; want 3", len(files), err)
	}
	// 全重复 → ErrConflict；不存在的文件 → ErrNotFound。
	if _, err := db.AddShareGroupFiles(ctx, group.ID, []int64{fileIDs[0]}); !errors.Is(err, ErrConflict) {
		t.Fatalf("all-duplicate add error = %v", err)
	}
	if _, err := db.AddShareGroupFiles(ctx, group.ID, []int64{999999}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing file add error = %v", err)
	}
	// 移除成员：重复移除第二次 → ErrNotFound。
	if err := db.RemoveShareGroupFile(ctx, group.ID, fileIDs[1]); err != nil {
		t.Fatal(err)
	}
	if err := db.RemoveShareGroupFile(ctx, group.ID, fileIDs[1]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double remove error = %v", err)
	}
	files, err = db.ListShareGroupFiles(ctx, group.ID)
	if err != nil || len(files) != 2 {
		t.Fatalf("member files after remove = %d, %v; want 2", len(files), err)
	}
	// 属性编辑：未来时间 + 新上限生效。
	future := time.Now().UTC().Add(72 * time.Hour).Format(time.RFC3339)
	if err := db.UpdateShareGroupAttributes(ctx, "v018-group", user.ID, future, 9); err != nil {
		t.Fatal(err)
	}
	updated, err := db.GetShareGroupByToken(ctx, "v018-group")
	if err != nil || updated.MaxDownloads != 9 || updated.ExpiresAt != future {
		t.Fatalf("updated group = %+v, %v", updated, err)
	}
	// 过去时间 → ErrConflict；越权 → ErrNotFound。
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	if err := db.UpdateShareGroupAttributes(ctx, "v018-group", user.ID, past, 9); !errors.Is(err, ErrConflict) {
		t.Fatalf("past expiry error = %v", err)
	}
	if err := db.UpdateShareGroupAttributes(ctx, "v018-group", user.ID+1, future, 9); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner edit error = %v", err)
	}
}
