package store

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestListPendingTaskProgressCountsOnlyStoredChunks 回归 v044.8：进度流必须只统计"磁盘文件也在、
// 且大小相符"的分片。旧实现按数据库 COUNT 报数，于是一个"chunks 表齐全但文件被删/残缺"的任务
// 会报成 1546/1546，前端据此把条目推到 100% 并显示"成功"。
// The progress feed must count only chunks whose file is actually on disk.
func TestListPendingTaskProgressCountsOnlyStoredChunks(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()

	if err := db.EnsureAdmin("admin", "admin123", 1<<30); err != nil {
		t.Fatal(err)
	}
	user, err := db.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}

	const chunkSize = 2 << 20
	task := UploadTask{
		ID: "task-progress-audit", UserID: user.ID, Name: "big.bin",
		Size: chunkSize*2 + 1, ChunkSize: chunkSize, TotalChunks: 3, Status: "pending",
	}
	if err := db.CreateUploadTask(ctx, task); err != nil {
		t.Fatal(err)
	}

	tmpDir := filepath.Join(db.DataDir, "tmp", task.ID)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 分片 0、1：元数据与文件都在；分片 2：只有元数据（模拟被 abort 的覆盖写在旧实现下留下的状态）。
	for _, index := range []int{0, 1} {
		if err := db.SetChunk(ctx, task.ID, index, chunkSize, "hash"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, strconv.Itoa(index)), make([]byte, chunkSize), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SetChunk(ctx, task.ID, 2, 1, "hash"); err != nil {
		t.Fatal(err)
	}

	progress, err := db.ListPendingTaskProgress(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(progress) != 1 {
		t.Fatalf("progress entries = %d, want 1", len(progress))
	}
	got := progress[0]
	if got.TotalChunks != 3 {
		t.Fatalf("totalChunks = %d, want 3", got.TotalChunks)
	}
	if got.Uploaded != 2 {
		t.Fatalf("uploaded = %d, want 2 (chunk 2 has no file on disk)", got.Uploaded)
	}
	if got.UploadedBytes != chunkSize*2 {
		t.Fatalf("uploadedBytes = %d, want %d", got.UploadedBytes, chunkSize*2)
	}
}
