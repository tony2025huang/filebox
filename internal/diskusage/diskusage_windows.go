//go:build windows

package diskusage

import (
	"errors"

	"golang.org/x/sys/windows"
)

// DiskUsage 获取 dir 所在卷的容量、可用空间和已用空间。
// DiskUsage reports the capacity, free space, and used space of the volume containing dir.
func DiskUsage(dir string) (total, free, used int64, err error) {
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, 0, 0, err
	}
	var available, capacity, freeBytes uint64
	if err := windows.GetDiskFreeSpaceEx(path, &available, &capacity, &freeBytes); err != nil {
		return 0, 0, 0, err
	}
	if capacity > uint64(^uint64(0)>>1) || available > uint64(^uint64(0)>>1) {
		return 0, 0, 0, errors.New("disk usage exceeds int64 range")
	}
	total, free = int64(capacity), int64(available)
	used = total - free
	if used < 0 {
		used = 0
	}
	return total, free, used, nil
}
