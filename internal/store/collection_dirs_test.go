package store

import "testing"

// TestCollectionDirNameSanitizes 覆盖收集目录命名的安全与稳定规则（v030 #7）：
// 保留用户语言名称、剔除路径分隔符与 Windows 保留字符、空名回落、保留设备名加前缀。
func TestCollectionDirNameSanitizes(t *testing.T) {
	token := "abcdef1234567890"
	cases := []struct{ name, want string }{
		{"我的收集 A", "我的收集-A-abcdef12"},
		{"a/b\\c:d*e?f", "abcdef-abcdef12"},
		{"...", "collection-abcdef12"},
		{"", "collection-abcdef12"},
		{"CON", "c-CON-abcdef12"},
		{"trailing. ", "trailing-abcdef12"},
	}
	for _, c := range cases {
		if got := CollectionDirName(c.name, token); got != c.want {
			t.Errorf("CollectionDirName(%q) = %q want %q", c.name, got, c.want)
		}
	}
	if got := CollectionDirName("x", "short"); got != "x-short" {
		t.Errorf("short token = %q want %q", got, "x-short")
	}
}
