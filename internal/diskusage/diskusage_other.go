//go:build !windows && !linux

package diskusage

import "errors"

// DiskUsage 在不支持的平台上返回不可用错误。
// DiskUsage returns an unavailable error on unsupported operating systems.
func DiskUsage(_ string) (total, free, used int64, err error) {
	return 0, 0, 0, errors.New("disk usage is unsupported on this operating system")
}
