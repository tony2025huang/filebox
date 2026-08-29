package srvlog

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoggerWritesStructuredEventAndConsoleFallback(t *testing.T) {
	dir := t.TempDir()
	logger := New(Config{Enabled: true, Dir: dir, RetentionDays: 90})
	logger.Event("login", "operator=%s ip=%s result=%s", "alice", "127.0.0.1", "success")
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "filebox-"+time.Now().Format(dateLayout)+".log"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "INFO [login] operator=alice ip=127.0.0.1 result=success") {
		t.Fatalf("unexpected log line: %q", text)
	}
}

func TestLoggerRetentionRemovesOldArchives(t *testing.T) {
	dir := t.TempDir()
	oldDate := time.Now().AddDate(0, 0, -3).Format(dateLayout)
	oldPath := filepath.Join(dir, "filebox-"+oldDate+".log.gz")
	file, err := os.Create(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	writer := gzip.NewWriter(file)
	_, _ = writer.Write([]byte("old"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	logger := New(Config{Enabled: true, Dir: dir, RetentionDays: 1})
	_ = logger.Close()
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old archive was not removed: %v", err)
	}
}

func TestLoggerArchivesStaleLogOnStartup(t *testing.T) {
	dir := t.TempDir()
	oldDate := time.Now().AddDate(0, 0, -1).Format(dateLayout)
	oldPath := filepath.Join(dir, "filebox-"+oldDate+".log")
	if err := os.WriteFile(oldPath, []byte("yesterday"), 0o640); err != nil {
		t.Fatal(err)
	}
	logger := New(Config{Enabled: true, Dir: dir, RetentionDays: 90})
	_ = logger.Close()
	archivePath := oldPath + ".gz"
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("stale log was not removed after archive: %v", err)
	}
}

func TestCompressLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "filebox-2026-08-28.log")
	if err := os.WriteFile(path, []byte("hello"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := compressLog(path); err != nil {
		t.Fatal(err)
	}
	archive, err := os.Open(path + ".gz")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	_ = archive.Close()
	if string(data) != "hello" {
		t.Fatalf("unexpected archive content: %q", data)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("source log was not removed: %v", err)
	}
}
