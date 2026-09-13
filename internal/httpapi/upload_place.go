package httpapi

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

// placeUploadFile 把合并后的临时文件放到最终路径（v038）。
//
// 为什么不能只调一次 os.Rename：Windows 上刚写完的大文件常被杀软/Defender 或索引服务短暂持有，
// 单次 rename 会以 sharing violation / access denied 失败——这正是历史上两次 save_failed
// （6GB 文件传到 100% 后落盘失败）的最可能成因。因此这里：先按退避重试改名，仍失败则退化为
// "复制到目标目录内的临时名再改名"，最后才把带有底层原因的错误返回。每一步都写结构化日志，
// 便于事后分析（此前底层错误只进 stderr，无法从日志复现）。
//
// placeUploadFile moves the merged temp file to its final path. A single os.Rename is not enough on
// Windows: antivirus/indexers briefly hold a freshly written large file, which is the most likely cause
// of the two historical save_failed incidents on a 6GB upload. It retries with backoff, then falls back
// to copying into the target directory and renaming, and logs the underlying error on every step.
func placeUploadFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		log.Printf("upload_place result=failure via=mkdir err=%v dst=%s", err, dst)
		return fmt.Errorf("create target directory %s: %w", filepath.Dir(dst), err)
	}
	var lastErr error
	delay := 200 * time.Millisecond
	for attempt := 1; attempt <= 5; attempt++ {
		lastErr = os.Rename(src, dst)
		if lastErr == nil {
			log.Printf("upload_place result=success via=rename attempt=%d dst=%s", attempt, dst)
			return nil
		}
		log.Printf("upload_place result=retry via=rename attempt=%d err=%v src=%s dst=%s", attempt, lastErr, src, dst)
		time.Sleep(delay)
		if delay < 2*time.Second {
			delay *= 2
		}
	}

	// 改名反复失败：复制到目标目录内的临时名，再在同目录内改名（同目录改名不再受"源被占用"影响）。
	// Repeated rename failures: copy into the target directory, then rename within that directory.
	tmp := dst + ".placing"
	if err := copyFileContents(src, tmp); err != nil {
		log.Printf("upload_place result=failure via=copy err=%v renameErr=%v src=%s dst=%s", err, lastErr, src, dst)
		return fmt.Errorf("place %s -> %s: rename failed (%v) and copy failed (%w)", src, dst, lastErr, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		log.Printf("upload_place result=failure via=copy-rename err=%v renameErr=%v dst=%s", err, lastErr, dst)
		return fmt.Errorf("place %s -> %s: rename failed (%v) and copy-rename failed (%w)", src, dst, lastErr, err)
	}
	_ = os.Remove(src)
	log.Printf("upload_place result=success via=copy dst=%s", dst)
	return nil
}

// copyFileContents 复制文件内容；任何失败都会清理半成品目标文件，避免留下不完整的文件。
// copyFileContents copies a file and removes a partial destination on any failure.
func copyFileContents(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
}
