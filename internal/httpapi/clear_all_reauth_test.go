package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// TestClearAllReauthThrottlesFailuresWithoutLockingAccount 验证 clear-all 二次认证的失败限速、
// IP 维度计数与"不锁账号"策略，并确认正确凭据在失败桶耗尽后仍可通过。
// TestClearAllReauthThrottlesFailuresWithoutLockingAccount covers failed-attempt throttling,
// source-IP failure accounting, the deliberate absence of account lockout, and that correct
// credentials still pass once the failure bucket is drained.
func TestClearAllReauthThrottlesFailuresWithoutLockingAccount(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	uploaded := uploadTestFile(t, handler, token, "keep-on-throttle.txt", "text/plain", []byte("keep me"))

	for attempt := 1; attempt <= 5; attempt++ {
		denied := testJSONRequest(t, handler, http.MethodPost, "/api/files/clear-all", token, `{"password":"wrong-password"}`)
		if denied.Code != http.StatusUnauthorized {
			t.Fatalf("failed attempt %d = %d, want 401: %s", attempt, denied.Code, denied.Body.String())
		}
	}
	throttled := testJSONRequest(t, handler, http.MethodPost, "/api/files/clear-all", token, `{"password":"wrong-password"}`)
	if throttled.Code != http.StatusTooManyRequests || responseData(t, throttled)["code"] != "REAUTH_RATE_LIMITED" {
		t.Fatalf("sixth failed attempt = %d %s, want 429 REAUTH_RATE_LIMITED", throttled.Code, throttled.Body.String())
	}

	var ipFailures int
	if err := db.DB.QueryRow("SELECT COALESCE(SUM(failed_count), 0) FROM ip_failures").Scan(&ipFailures); err != nil {
		t.Fatal(err)
	}
	if ipFailures == 0 {
		t.Fatal("reauth failures must be recorded in ip_failures")
	}

	admin, err := db.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	if admin.FailedAttempts != 0 || admin.LockedUntil != "" {
		t.Fatalf("reauth failures must not lock the account: failed=%d locked=%q", admin.FailedAttempts, admin.LockedUntil)
	}

	files := testJSONRequest(t, handler, http.MethodGet, "/api/files", token, "")
	fileItems, ok := responseData(t, files)["items"].([]any)
	if files.Code != http.StatusOK || !ok || len(fileItems) != 1 || int64(fileItems[0].(map[string]any)["id"].(float64)) != int64(uploaded["id"].(float64)) {
		t.Fatalf("files after throttled failures = %s", files.Body.String())
	}

	cleared := testJSONRequest(t, handler, http.MethodPost, "/api/files/clear-all", token, `{"password":"admin123"}`)
	if cleared.Code != http.StatusOK {
		t.Fatalf("correct password after throttling = %d: %s", cleared.Code, cleared.Body.String())
	}
}

// TestClearAllReauthLimiterBoundsAttemptsDeterministically 直接固定限速桶语义：以 1 令牌/分钟
// 配置时测试期间不可能补充令牌，因此断言与运行速度无关（含 -race 下的慢速构建）。
// TestClearAllReauthLimiterBoundsAttemptsDeterministically pins the bucket semantics directly:
// at one token per minute no refill can happen during a test run, so the assertion is
// independent of runtime speed, including slow -race builds.
func TestClearAllReauthLimiterBoundsAttemptsDeterministically(t *testing.T) {
	limiter := &rateLimiter{}
	const key = "unit-test\x00clear-all-attempt"
	allowed := 0
	for i := 0; i < 8; i++ {
		if limiter.allowPublicRequest(key, 1, 3) {
			allowed++
		}
	}
	if allowed != 3 {
		t.Fatalf("burst-3 bucket allowed %d attempts, want exactly 3", allowed)
	}
}

// TestClearAllReauthAttemptBucketBoundsCPU 验证尝试桶约束全部尝试（含正确凭据），
// 使 bcrypt/TOTP 计算无法被无限放大。
// TestClearAllReauthAttemptBucketBoundsCPU verifies the attempt bucket bounds every attempt
// (correct credentials included) so bcrypt/TOTP work cannot be amplified.
func TestClearAllReauthAttemptBucketBoundsCPU(t *testing.T) {
	if raceDetectorEnabled {
		t.Skip("-race slows bcrypt ~10-20x so the attempt bucket refills mid-test; bucket semantics are pinned deterministically by TestClearAllReauthLimiterBoundsAttemptsDeterministically")
	}
	_, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	uploadTestFile(t, handler, token, "keep-bounded.txt", "text/plain", []byte("keep me"))

	for attempt := 1; attempt <= 10; attempt++ {
		resp := testJSONRequest(t, handler, http.MethodPost, "/api/files/clear-all", token, `{"password":"wrong-password"}`)
		if resp.Code != http.StatusUnauthorized && resp.Code != http.StatusTooManyRequests {
			t.Fatalf("attempt %d = %d, want 401/429: %s", attempt, resp.Code, resp.Body.String())
		}
	}
	bounded := testJSONRequest(t, handler, http.MethodPost, "/api/files/clear-all", token, `{"password":"admin123"}`)
	if bounded.Code != http.StatusTooManyRequests {
		t.Fatalf("correct credentials with a drained attempt bucket = %d, want 429: %s", bounded.Code, bounded.Body.String())
	}
}

// TestClearAllReauthTOTPCodeCannotBeReplayed 验证动态码经 ConsumeTOTP 消费后不可重放，
// 同一码不能重复授权破坏性清空。
// TestClearAllReauthTOTPCodeCannotBeReplayed verifies the TOTP counter is consumed so one code
// cannot authorize repeated destructive clears.
func TestClearAllReauthTOTPCodeCannotBeReplayed(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	admin, err := db.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	enabled := testJSONRequest(t, handler, http.MethodPut, "/api/admin/users/"+strconv.FormatInt(admin.ID, 10)+"/totp", token, `{"enabled":true}`)
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable admin TOTP = %d: %s", enabled.Code, enabled.Body.String())
	}
	admin, err = db.GetUser(admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(db, Config{DataDir: db.DataDir, JWTSecret: []byte("test-secret")})
	secret, err := server.decryptTOTPSecretForUser(context.Background(), admin)
	if err != nil {
		t.Fatal(err)
	}
	uploadTestFile(t, handler, token, "clear-with-totp.txt", "text/plain", []byte("clear me"))

	code := totpCode(secret, time.Now().UTC().Unix()/30)
	first := testJSONRequest(t, handler, http.MethodPost, "/api/files/clear-all", token, `{"code":"`+code+`"}`)
	if first.Code != http.StatusOK {
		t.Fatalf("first clear with TOTP code = %d: %s", first.Code, first.Body.String())
	}
	replay := testJSONRequest(t, handler, http.MethodPost, "/api/files/clear-all", token, `{"code":"`+code+`"}`)
	if replay.Code != http.StatusUnauthorized {
		t.Fatalf("replayed TOTP code = %d, want 401: %s", replay.Code, replay.Body.String())
	}
}
