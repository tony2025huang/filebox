package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func main() {
	// main 将 Vite 产物复制到 Go embed 目录，供单文件服务打包使用。
	// main copies the Vite output into the Go embed directory for single-binary packaging.
	source := filepath.FromSlash("web/dist")
	destination := filepath.FromSlash("internal/webassets/dist")
	if _, err := os.Stat(source); err != nil {
		panic(fmt.Errorf("frontend bundle not found at %s; run npm --prefix web run build first: %w", source, err))
	}
	if err := os.RemoveAll(destination); err != nil {
		panic(err)
	}
	if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, contents, 0o644)
	}); err != nil {
		panic(err)
	}
	fmt.Printf("synced %s to %s\n", source, destination)
}
