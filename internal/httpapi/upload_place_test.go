package httpapi

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPlaceUploadFileCreatesNestedTarget 覆盖正常路径：目标目录不存在时自动创建并完成改名，
// 源文件消失、内容一致（v038 落盘重试/兜底逻辑的基本行为）。
func TestPlaceUploadFileCreatesNestedTarget(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "merged.tmp")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "nested", "deep", "final.bin")
	if err := placeUploadFile(src, dst); err != nil {
		t.Fatalf("placeUploadFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read placed file: %v", err)
	}
	if string(got) != "payload" {
		t.Fatalf("placed content = %q", got)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("source must be gone after a successful placement, stat err=%v", err)
	}
}

// TestPlaceUploadFileKeepsSourceOnFailure 覆盖失败路径：目标是一个已存在的目录时，改名与复制兜底
// 都无法完成，必须报错且**保留源文件**（避免把已上传的数据丢掉），且不留下 .placing 半成品。
func TestPlaceUploadFileKeepsSourceOnFailure(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "merged2.tmp")
	if err := os.WriteFile(src, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(dir, "blocked")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := placeUploadFile(src, blocked); err == nil {
		t.Fatalf("expected a failure when the target is an existing directory")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("source must be preserved when placement fails: %v", err)
	}
	if _, err := os.Stat(blocked + ".placing"); !os.IsNotExist(err) {
		t.Fatalf("no partial .placing file may be left behind, stat err=%v", err)
	}
}
