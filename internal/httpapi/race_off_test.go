//go:build !race

package httpapi

// raceDetectorEnabled 报告当前测试二进制是否启用了竞态检测（-race）。
// raceDetectorEnabled reports whether the test binary was built with the race detector.
const raceDetectorEnabled = false
