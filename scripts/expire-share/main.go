// expire-share 是测试辅助工具：把指定 token 的分享链接立即置为过期（写入 DB）。
// 用法：go run ./scripts/expire-share --data=<data-dir> --token=<token>
// 设计：直接打开 --data 下的 SQLite（busy_timeout 与运行中的服务共存），把 expires_at 改为 2000-01-01。
// expire-share is a test helper that marks a share link as expired by rewriting expires_at in the database.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

func main() {
	var dataDir, token string
	flag.StringVar(&dataDir, "data", "", "FileBox data directory containing filebox.db")
	flag.StringVar(&token, "token", "", "share token to expire")
	flag.Parse()
	if dataDir == "" || token == "" {
		fmt.Fprintln(os.Stderr, "usage: expire-share --data=<dir> --token=<token>")
		os.Exit(2)
	}
	dbPath := filepath.Join(dataDir, "filebox.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open db:", err)
		os.Exit(1)
	}
	defer db.Close()
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		fmt.Fprintln(os.Stderr, "busy_timeout:", err)
		os.Exit(1)
	}
	past := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	res, err := db.Exec("UPDATE shares SET expires_at = ? WHERE token = ?", past, token)
	if err != nil {
		fmt.Fprintln(os.Stderr, "update:", err)
		os.Exit(1)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fmt.Fprintln(os.Stderr, "no share found for token")
		os.Exit(1)
	}
	fmt.Printf("expired share %s\n", token)
}