package store

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

// TestShareGroupExtendAndIncreaseLimits verifies the aggregate-share edit guards (#6):
// extend never shortens the deadline, increase never lowers the limit, and cross-owner
// attempts map to ErrNotFound.
// TestShareGroupExtendAndIncreaseLimits 验证聚合分享编辑守卫（#6）：延期不缩短截止时间、
// 增次不降低上限、越权一律 ErrNotFound。
func TestShareGroupExtendAndIncreaseLimits(t *testing.T) {
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
	var fileIDs []int64
	for index, name := range []string{"g1.txt", "g2.txt"} {
		result, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(?, ?, ?, 1, 'text/plain', 'sha', 'md5', 'ready', ?, ?)", user.ID, name, name, "files/"+strconv.FormatInt(user.ID, 10)+"/g"+strconv.Itoa(index)+".txt", now)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := result.LastInsertId()
		fileIDs = append(fileIDs, id)
	}
	group, _, err := db.CreateShareGroup(ctx, user.ID, "group-token", fileIDs, time.Now().UTC().Add(24*time.Hour).Format(time.RFC3339), 2)
	if err != nil {
		t.Fatal(err)
	}
	// 越权延期/增次：ErrNotFound。
	if err := db.UpdateShareGroupExpiry(ctx, "group-token", time.Now().UTC().Add(48*time.Hour), user.ID+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner expiry error = %v", err)
	}
	if err := db.UpdateShareGroupMaxDownloads(ctx, "group-token", 5, user.ID+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner max-downloads error = %v", err)
	}
	// 延期：新截止时间生效且不缩短原时间。
	original := group.ExpiresAt
	if err := db.UpdateShareGroupExpiry(ctx, "group-token", time.Now().UTC().Add(48*time.Hour), user.ID); err != nil {
		t.Fatal(err)
	}
	updated, err := db.GetShareGroupByToken(ctx, "group-token")
	if err != nil {
		t.Fatal(err)
	}
	if !(updated.ExpiresAt > original) {
		t.Fatalf("expiry did not move forward: %s vs %s", updated.ExpiresAt, original)
	}
	// 缩短截止时间被拒绝（保持更晚的截止时间）。
	if err := db.UpdateShareGroupExpiry(ctx, "group-token", time.Now().UTC().Add(1*time.Hour), user.ID); err != nil {
		t.Fatalf("shortening expiry should keep the later deadline, got error = %v", err)
	}
	kept, err := db.GetShareGroupByToken(ctx, "group-token")
	if err != nil || kept.ExpiresAt != updated.ExpiresAt {
		t.Fatalf("shortening expiry changed deadline: %s, %v", kept.ExpiresAt, err)
	}
	// 增次：2 → 5 成功；降低为 2 返回 ErrConflict；提升为 0（不限）成功。
	if err := db.UpdateShareGroupMaxDownloads(ctx, "group-token", 5, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateShareGroupMaxDownloads(ctx, "group-token", 2, user.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("decreasing max downloads error = %v", err)
	}
	if err := db.UpdateShareGroupMaxDownloads(ctx, "group-token", 0, user.ID); err != nil {
		t.Fatal(err)
	}
	final, err := db.GetShareGroupByToken(ctx, "group-token")
	if err != nil || final.MaxDownloads != 0 {
		t.Fatalf("final group = %+v, %v", final, err)
	}
	// 已撤销聚合分享不能编辑。
	if err := db.RevokeShareGroup(ctx, "group-token", user.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateShareGroupExpiry(ctx, "group-token", time.Now().UTC().Add(48*time.Hour), user.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expiry on revoked group error = %v", err)
	}
}

// TestShareGroupFileCountMatchesReadyMembers ensures metadata matches the public file list.
// TestShareGroupFileCountMatchesReadyMembers 确保元数据计数与公开文件列表只统计 ready 成员。
func TestShareGroupFileCountMatchesReadyMembers(t *testing.T) {
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
	now := time.Now().UTC().Format(time.RFC3339)
	fileIDs := make([]int64, 0, 3)
	for index := 0; index < 3; index++ {
		result, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(?, ?, ?, 1, 'text/plain', 'sha', 'md5', 'ready', ?, ?)", user.ID, "count-"+strconv.Itoa(index), "count-"+strconv.Itoa(index), "files/"+strconv.FormatInt(user.ID, 10)+"/count-"+strconv.Itoa(index), now)
		if err != nil {
			t.Fatal(err)
		}
		fileID, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		fileIDs = append(fileIDs, fileID)
	}
	if _, _, err := db.CreateShareGroup(context.Background(), user.ID, "ready-count-group", fileIDs, time.Now().UTC().Add(time.Hour).Format(time.RFC3339), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE files SET status = 'deleted', deleted_at = ? WHERE id = ?", now, fileIDs[0]); err != nil {
		t.Fatal(err)
	}
	group, err := db.GetShareGroupByToken(context.Background(), "ready-count-group")
	if err != nil {
		t.Fatal(err)
	}
	if group.FileCount != 2 {
		t.Fatalf("group file count = %d, want 2", group.FileCount)
	}
	groups, err := db.ListShareGroupsByOwner(context.Background(), user.ID, false)
	if err != nil || len(groups) != 1 || groups[0].FileCount != 2 {
		t.Fatalf("owner group list = %+v, %v", groups, err)
	}
	files, err := db.ListShareGroupFiles(context.Background(), group.ID)
	if err != nil || len(files) != 2 {
		t.Fatalf("group files = %d, %v", len(files), err)
	}
}
