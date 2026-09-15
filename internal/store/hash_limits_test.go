package store

import (
	"context"
	"testing"
)

// TestClampHashLimits 覆盖阈值的夹取规则：非正数回退默认值、越界被拉到区间端点、直算上限不超过总上限。
func TestClampHashLimits(t *testing.T) {
	cases := []struct {
		name       string
		direct     int64
		client     int64
		wantDirect int64
		wantClient int64
	}{
		{"non positive falls back to defaults", 0, 0, DefaultHashDirectLimitBytes, DefaultHashClientLimitBytes},
		{"negative falls back to defaults", -1, -1024, DefaultHashDirectLimitBytes, DefaultHashClientLimitBytes},
		{"below minimum is raised", 1, 512, MinHashLimitBytes, MinHashLimitBytes},
		{"above maximum is capped", 1 << 40, 1 << 40, MaxHashDirectLimitBytes, MaxHashClientLimitBytes},
		{"direct is never above client", 1 << 30, 512 << 20, 512 << 20, 512 << 20},
		{"in range values are kept", 512 << 20, 4 << 30, 512 << 20, 4 << 30},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			direct, client := ClampHashLimits(tc.direct, tc.client)
			if direct != tc.wantDirect || client != tc.wantClient {
				t.Fatalf("ClampHashLimits(%d, %d) = (%d, %d), want (%d, %d)", tc.direct, tc.client, direct, client, tc.wantDirect, tc.wantClient)
			}
		})
	}
}

// TestLogSettingsHashLimitsRoundTrip 验证阈值能持久化，并确认库内越界值在读取时被夹取。
func TestLogSettingsHashLimitsRoundTrip(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()

	settings, err := db.GetLogSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.HashDirectLimitBytes != DefaultHashDirectLimitBytes || settings.HashClientLimitBytes != DefaultHashClientLimitBytes {
		t.Fatalf("default hash limits = (%d, %d), want (%d, %d)", settings.HashDirectLimitBytes, settings.HashClientLimitBytes, DefaultHashDirectLimitBytes, DefaultHashClientLimitBytes)
	}

	settings.HashDirectLimitBytes = 512 << 20
	settings.HashClientLimitBytes = 4 << 30
	if err := db.UpdateLogSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	loaded, err := db.GetLogSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.HashDirectLimitBytes != 512<<20 || loaded.HashClientLimitBytes != 4<<30 {
		t.Fatalf("persisted hash limits = (%d, %d), want (%d, %d)", loaded.HashDirectLimitBytes, loaded.HashClientLimitBytes, 512<<20, 4<<30)
	}

	// 手工写入越界值（模拟旧库或直接改库）：非正数回退默认值。
	if _, err := db.DB.ExecContext(ctx, "INSERT INTO settings(key, value) VALUES('hashDirectLimitBytes', '0') ON CONFLICT(key) DO UPDATE SET value = excluded.value"); err != nil {
		t.Fatal(err)
	}
	fallback, err := db.GetLogSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fallback.HashDirectLimitBytes != DefaultHashDirectLimitBytes || fallback.HashClientLimitBytes != 4<<30 {
		t.Fatalf("fallback hash limits = (%d, %d), want (%d, %d)", fallback.HashDirectLimitBytes, fallback.HashClientLimitBytes, DefaultHashDirectLimitBytes, 4<<30)
	}

	// 总上限被夹到最小值后，直算上限随之被压到同一值（256MiB > 1MiB）。
	if _, err := db.DB.ExecContext(ctx, "INSERT INTO settings(key, value) VALUES('hashClientLimitBytes', '1024') ON CONFLICT(key) DO UPDATE SET value = excluded.value"); err != nil {
		t.Fatal(err)
	}
	clamped, err := db.GetLogSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if clamped.HashDirectLimitBytes != MinHashLimitBytes || clamped.HashClientLimitBytes != MinHashLimitBytes {
		t.Fatalf("clamped hash limits = (%d, %d), want (%d, %d)", clamped.HashDirectLimitBytes, clamped.HashClientLimitBytes, MinHashLimitBytes, MinHashLimitBytes)
	}
}
