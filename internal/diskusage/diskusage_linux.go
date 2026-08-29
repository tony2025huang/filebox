//go:build linux

package diskusage

import "syscall"

// DiskUsage 获取 dir 所在文件系统的容量、可用空间和已用空间。
// DiskUsage reports the capacity, free space, and used space of the filesystem containing dir.
func DiskUsage(dir string) (total, free, used int64, err error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return 0, 0, 0, err
	}
	totalBytes := uint64(stat.Blocks) * uint64(stat.Bsize)
	freeBytes := uint64(stat.Bavail) * uint64(stat.Bsize)
	if totalBytes > uint64(^uint64(0)>>1) || freeBytes > uint64(^uint64(0)>>1) {
		return 0, 0, 0, syscall.EOVERFLOW
	}
	total, free = int64(totalBytes), int64(freeBytes)
	used = total - free
	if used < 0 {
		used = 0
	}
	return total, free, used, nil
}
