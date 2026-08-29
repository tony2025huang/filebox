// Package srvlog provides FileBox service logging with daily local-time files.
// srvlog 提供按本地日期滚动的 FileBox 服务日志。
package srvlog

import (
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	filePrefix = "filebox-"
	dateLayout = "2006-01-02"
)

// Config controls service log output and retention.
// Config 控制服务日志输出与保留策略。
type Config struct {
	Enabled       bool
	Dir           string
	RetentionDays int
}

// Logger writes to the console and, when enabled, to one local-date file.
// Logger 同时写入控制台，并在启用时写入一个按本地日期命名的文件。
type Logger struct {
	mu          sync.Mutex
	config      Config
	console     *log.Logger
	file        *os.File
	currentDate string
	currentPath string
	closed      bool
}

// New creates a logger. File setup errors fall back to console output because
// logging must not prevent the service from starting.
// New 创建日志器；文件初始化失败时退回控制台，避免日志故障阻止服务启动。
func New(config Config) *Logger {
	if config.RetentionDays < 0 {
		config.RetentionDays = 90
	}
	logger := &Logger{config: config, console: log.New(os.Stderr, "", 0)}
	if config.Enabled {
		if err := os.MkdirAll(config.Dir, 0o755); err != nil {
			logger.console.Printf("ERROR [srvlog] operator=system ip=- initialize log directory: %v", err)
			logger.config.Enabled = false
		} else {
			logger.archiveOldLogs(time.Now())
			logger.cleanup(time.Now())
		}
	}
	return logger
}

// Infof writes an informational service message.
// Infof 写入信息级服务日志。
func (l *Logger) Infof(format string, args ...any) {
	l.write("INFO", "", fmt.Sprintf(format, args...))
}

// Errorf writes an error service message.
// Errorf 写入错误级服务日志。
func (l *Logger) Errorf(format string, args ...any) {
	l.write("ERROR", "", fmt.Sprintf(format, args...))
}

// Event writes a structured service event. Callers must format operator and ip
// as the first two fields, for example: operator=%s ip=%s result=%s.
// Event 写入结构化事件；调用方必须将 operator 与 ip 作为前两个格式化字段。
func (l *Logger) Event(event, format string, args ...any) {
	l.write("INFO", event, fmt.Sprintf(format, args...))
}

func (l *Logger) write(level, event, message string) {
	message = strings.NewReplacer("\r", " ", "\n", " ").Replace(message)
	now := time.Now()
	line := fmt.Sprintf("%s %s", now.Format(time.RFC3339), level)
	if event != "" {
		line += " [" + event + "]"
	}
	line += " " + message

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	l.console.Print(line)
	if !l.config.Enabled {
		return
	}
	if err := l.ensureFile(now); err != nil {
		l.console.Printf("ERROR [srvlog] operator=system ip=- open service log: %v", err)
		return
	}
	if _, err := l.file.WriteString(line + "\n"); err != nil {
		l.console.Printf("ERROR [srvlog] operator=system ip=- write service log: %v", err)
	}
}

func (l *Logger) ensureFile(now time.Time) error {
	date := now.Format(dateLayout)
	if l.file != nil && l.currentDate == date {
		return nil
	}
	if l.file != nil {
		oldFile, oldPath := l.file, l.currentPath
		l.file = nil
		l.currentPath = ""
		l.currentDate = ""
		if err := oldFile.Close(); err != nil {
			l.console.Printf("ERROR [srvlog] operator=system ip=- close service log: %v", err)
		}
		if err := compressLog(oldPath); err != nil {
			l.console.Printf("ERROR [srvlog] operator=system ip=- archive service log: %v", err)
		}
		l.archiveOldLogs(now)
		l.cleanup(now)
	}
	path := filepath.Join(l.config.Dir, filePrefix+date+".log")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	l.file = file
	l.currentDate = date
	l.currentPath = path
	return nil
}

func (l *Logger) archiveOldLogs(now time.Time) {
	entries, err := os.ReadDir(l.config.Dir)
	if err != nil {
		l.console.Printf("ERROR [srvlog] operator=system ip=- scan service logs for archive: %v", err)
		return
	}
	today := now.In(time.Local)
	todayStart := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.Local)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, filePrefix) || !strings.HasSuffix(name, ".log") {
			continue
		}
		date, ok := logDate(name)
		if ok && date.Before(todayStart) {
			if err := compressLog(filepath.Join(l.config.Dir, name)); err != nil {
				l.console.Printf("ERROR [srvlog] operator=system ip=- archive stale service log: %v", err)
			}
		}
	}
}

func (l *Logger) cleanup(now time.Time) {
	entries, err := os.ReadDir(l.config.Dir)
	if err != nil {
		l.console.Printf("ERROR [srvlog] operator=system ip=- scan service logs: %v", err)
		return
	}
	localNow := now.In(time.Local)
	cutoff := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, -l.config.RetentionDays)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, filePrefix) || !(strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".log.gz")) {
			continue
		}
		date, ok := logDate(name)
		if !ok {
			if info, infoErr := entry.Info(); infoErr == nil {
				modified := info.ModTime().In(time.Local)
				date = time.Date(modified.Year(), modified.Month(), modified.Day(), 0, 0, 0, 0, time.Local)
				ok = true
			}
		}
		if ok && date.Before(cutoff) {
			if err := os.Remove(filepath.Join(l.config.Dir, name)); err != nil && !os.IsNotExist(err) {
				l.console.Printf("ERROR [srvlog] operator=system ip=- remove expired service log: %v", err)
			}
		}
	}
}

func logDate(name string) (time.Time, bool) {
	if !strings.HasPrefix(name, filePrefix) {
		return time.Time{}, false
	}
	dateText := strings.TrimPrefix(name, filePrefix)
	dateText = strings.TrimSuffix(dateText, ".gz")
	dateText = strings.TrimSuffix(dateText, ".log")
	date, err := time.ParseInLocation(dateLayout, dateText, time.Local)
	return date, err == nil
}

func compressLog(path string) error {
	if path == "" {
		return nil
	}
	input, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer input.Close()

	temporary, err := os.CreateTemp(filepath.Dir(path), ".filebox-archive-*.gz")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	writer := gzip.NewWriter(temporary)
	_, copyErr := io.Copy(writer, input)
	closeGzipErr := writer.Close()
	closeFileErr := temporary.Close()
	if copyErr != nil {
		_ = input.Close()
		return copyErr
	}
	if closeGzipErr != nil {
		_ = input.Close()
		return closeGzipErr
	}
	if closeFileErr != nil {
		_ = input.Close()
		return closeFileErr
	}
	if err := input.Close(); err != nil {
		return err
	}
	archivePath := path + ".gz"
	if err := os.Rename(temporaryName, archivePath); err != nil {
		if _, statErr := os.Stat(archivePath); statErr == nil {
			return os.Remove(path)
		}
		return err
	}
	return os.Remove(path)
}

// Close closes the current file. The current day's file remains uncompressed
// so another process can continue appending to it safely.
// Close 关闭当前文件；当天文件保留为未压缩状态以支持其他进程继续追加。
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}
