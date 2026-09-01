package httpapi

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"filebox/internal/store"
	"github.com/robfig/cron/v3"
	"golang.org/x/crypto/ssh"
)

// TestLatestCronOccurrenceReturnsMostRecentMissedRun verifies a long outage yields only the latest due occurrence.
// TestLatestCronOccurrenceReturnsMostRecentMissedRun 验证长时间停机只补跑最近一次到期执行。
func TestLatestCronOccurrenceReturnsMostRecentMissedRun(t *testing.T) {
	schedule, err := cron.ParseStandard("* * * * *")
	if err != nil {
		t.Fatal(err)
	}
	baseline := time.Date(2026, time.August, 1, 8, 0, 0, 0, time.Local)
	now := time.Date(2026, time.August, 31, 12, 34, 50, 0, time.Local)
	next, ok := latestCronOccurrence(schedule, baseline, now)
	if !ok {
		t.Fatal("latest cron occurrence not found")
	}
	want := time.Date(2026, time.August, 31, 12, 34, 0, 0, time.Local)
	if !next.Equal(want) {
		t.Fatalf("latest cron occurrence = %s, want %s", next, want)
	}
	if _, ok := latestCronOccurrence(schedule, now, now); ok {
		t.Fatal("cron occurrence found when baseline was not before now")
	}
}

// TestFileBoxRemoteClientRejectsMalformedBrowseEntry verifies malformed remote field types return an error instead of panicking.
// TestFileBoxRemoteClientRejectsMalformedBrowseEntry 验证远端 browse 字段类型错误时返回错误而不是 panic。
func TestFileBoxRemoteClientRejectsMalformedBrowseEntry(t *testing.T) {
	details := []string{}
	processed := false
	if err := walkFileBoxEntries([]map[string]any{
		{"name": 123, "isDir": false},
		{"name": "bad.txt", "isDir": "no"},
	}, "", func(remoteBrowseEntry, string) error {
		processed = true
		return nil
	}, func(string, string) error { return nil }, &details); err != nil {
		t.Fatal(err)
	}
	if processed || len(details) != 2 {
		t.Fatalf("malformed map handling processed=%t details=%v", processed, details)
	}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"items":[{"name":"bad.txt","path":"bad.txt","isDir":"no","kind":"file","size":1,"id":1}]}}`))
	}))
	defer remote.Close()
	client := &fileBoxRemoteClient{baseURL: remote.URL, http: remote.Client()}
	if _, err := client.findFile(context.Background(), "", "bad.txt"); err == nil {
		t.Fatal("malformed browse entry unexpectedly succeeded")
	}
}

// TestFileBoxDownloadUsesDataDirectoryTemp verifies FileBox pull staging stays on the data volume.
// TestFileBoxDownloadUsesDataDirectoryTemp 验证 FileBox 拉取暂存文件位于数据目录所在卷。
func TestFileBoxDownloadUsesDataDirectoryTemp(t *testing.T) {
	dataDir := t.TempDir()
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/files/1/download" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("downloaded"))
	}))
	defer remote.Close()
	client := &fileBoxRemoteClient{baseURL: remote.URL, http: remote.Client()}
	tempPath, size, err := client.downloadFile(context.Background(), 1, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tempPath)
	if size != int64(len("downloaded")) || filepath.Dir(filepath.Dir(tempPath)) != dataDir || filepath.Base(filepath.Dir(tempPath)) != "tmp" {
		t.Fatalf("download temp path = %q, size = %d; want under %s\\tmp with size %d", tempPath, size, dataDir, len("downloaded"))
	}
	content, err := os.ReadFile(tempPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "downloaded" {
		t.Fatalf("downloaded content = %q", content)
	}
}

func TestHostKeyFingerprintMatches(t *testing.T) {
	bytes := make([]byte, 32)
	for index := range bytes {
		bytes[index] = byte(index)
	}
	base64Fingerprint := base64.RawStdEncoding.EncodeToString(bytes)
	mismatchedBytes := append([]byte(nil), bytes...)
	mismatchedBytes[0]++
	// paddingBitCorrupted 仅翻转 43 字符 raw base64 末字符的低 2 bit（padding 位），
	// 非严格解码结果与原始字节完全相同，严格解码必须拒绝。
	// paddingBitCorrupted flips only the low 2 bits (padding bits) of the last character
	// of the 43-char raw base64; non-strict decoding yields identical bytes, strict must reject it.
	paddingBitCorrupted := base64Fingerprint
	{
		last := base64Fingerprint[len(base64Fingerprint)-1]
		const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
		charIndex := strings.IndexByte(alphabet, last)
		if charIndex < 0 || charIndex&0x3 != 0 {
			t.Fatalf("last base64 char %q index = %d, want a zero low-2-bit index", last, charIndex)
		}
		paddingBitCorrupted = base64Fingerprint[:len(base64Fingerprint)-1] + string(alphabet[charIndex^0x3])
	}
	tests := []struct {
		name  string
		got   string
		want  string
		match bool
	}{
		{"matching SHA256 base64", "SHA256:" + base64Fingerprint, base64Fingerprint, true},
		{"mismatched base64", "SHA256:" + base64Fingerprint, base64.RawStdEncoding.EncodeToString(mismatchedBytes), false},
		{"hex against SHA256 base64", hex.EncodeToString(bytes), "SHA256:" + base64Fingerprint, true},
		{"wrong length base64", "SHA256:" + base64.RawStdEncoding.EncodeToString(bytes[:31]), base64Fingerprint, false},
		{"invalid base64", "SHA256:not-valid!", base64Fingerprint, false},
		{"padding-bit corrupted base64", "SHA256:" + paddingBitCorrupted, base64Fingerprint, false},
		{"padding-bit corrupted as want", base64Fingerprint, paddingBitCorrupted, false},
		{"empty strings", "", "", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hostKeyFingerprintMatches(test.got, test.want); got != test.match {
				t.Fatalf("hostKeyFingerprintMatches(%q, %q) = %t, want %t", test.got, test.want, got, test.match)
			}
		})
	}
}

func TestHostKeyFingerprintCallback(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := ssh.FingerprintSHA256(publicKey)
	callback := hostKeyCallbackFor("127.0.0.1:22", fingerprint)
	if err := callback("sftp.example", nil, publicKey); err != nil {
		t.Fatalf("matching host key callback = %v", err)
	}
	corrupted := fingerprint[:len(fingerprint)-1]
	last := fingerprint[len(fingerprint)-1]
	if last == 'A' {
		corrupted += "B"
	} else {
		corrupted += "A"
	}
	if err := hostKeyCallbackFor("127.0.0.1:22", corrupted)("sftp.example", nil, publicKey); err == nil {
		t.Fatal("corrupted host key fingerprint unexpectedly matched")
	}
}

func TestValidateSyncSystemInputHostKeyFingerprint(t *testing.T) {
	bytes := make([]byte, 32)
	validBase64 := "SHA256:" + base64.RawStdEncoding.EncodeToString(bytes)
	validHex := hex.EncodeToString(bytes)
	tests := []struct {
		name        string
		fingerprint string
		valid       bool
	}{
		{"valid SHA256", "  " + validBase64 + "  ", true},
		{"garbage", "garbage", false},
		{"over-long", strings.Repeat("a", 513), false},
		{"valid hex", validHex, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input, err := validateSyncSystemInput(syncSystemRequest{Name: "remote", Host: "example", Username: "user", AuthType: "password", HostKeyFingerprint: test.fingerprint}, false)
			if (err == nil) != test.valid {
				t.Fatalf("validateSyncSystemInput() error = %v, want valid=%t", err, test.valid)
			}
			if test.valid && input.HostKeyFingerprint != strings.TrimSpace(test.fingerprint) {
				t.Fatalf("trimmed fingerprint = %q, want %q", input.HostKeyFingerprint, strings.TrimSpace(test.fingerprint))
			}
		})
	}
}

func TestSyncSystemTaskOwnershipEncryptionAndDeleteProtection(t *testing.T) {
	db, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)
	createdSystem := testJSONRequest(t, handler, http.MethodPost, "/api/sync/systems", adminToken, `{"name":"backup","host":"sftp.example","port":22,"username":"backup-user","authType":"password","authSecret":"secret-password"}`)
	if createdSystem.Code != http.StatusCreated {
		t.Fatalf("create sync system = %d: %s", createdSystem.Code, createdSystem.Body.String())
	}
	systemData := responseData(t, createdSystem)
	systemID := int64(systemData["id"].(float64))
	if systemData["hasCredentials"] != true {
		t.Fatalf("system public data = %#v", systemData)
	}
	var storedSecret string
	if err := db.DB.QueryRowContext(context.Background(), "SELECT auth_secret FROM remote_systems WHERE id = ?", systemID).Scan(&storedSecret); err != nil {
		t.Fatal(err)
	}
	if storedSecret == "secret-password" || storedSecret == "" {
		t.Fatalf("credential was not encrypted: %q", storedSecret)
	}
	updatedSystem := testJSONRequest(t, handler, http.MethodPut, "/api/sync/systems/"+strconv.FormatInt(systemID, 10), adminToken, `{"name":"backup-renamed","host":"sftp.example","port":2222,"username":"backup-user","authType":"password","authSecret":""}`)
	if updatedSystem.Code != http.StatusOK || responseData(t, updatedSystem)["name"] != "backup-renamed" || responseData(t, updatedSystem)["port"] != float64(2222) {
		t.Fatalf("update sync system = %d: %s", updatedSystem.Code, updatedSystem.Body.String())
	}

	taskBody := `{"name":"nightly","direction":"push","remoteSystemId":` + strconv.FormatInt(systemID, 10) + `,"sourceType":"filebox","sourcePath":"docs","targetType":"sftp","targetPath":"/backup","conflictPolicy":"skip","scheduleType":"periodic","cron":"0 3 * * *","enabled":true}`
	createdTask := testJSONRequest(t, handler, http.MethodPost, "/api/sync/tasks", adminToken, taskBody)
	if createdTask.Code != http.StatusCreated {
		t.Fatalf("create sync task = %d: %s", createdTask.Code, createdTask.Body.String())
	}
	taskID := int64(responseData(t, createdTask)["id"].(float64))
	updatedTask := testJSONRequest(t, handler, http.MethodPut, "/api/sync/tasks/"+strconv.FormatInt(taskID, 10), adminToken, `{"name":"nightly-renamed","direction":"push","remoteSystemId":`+strconv.FormatInt(systemID, 10)+`,"sourceType":"filebox","sourcePath":"docs","targetType":"sftp","targetPath":"/backup","conflictPolicy":"skip","scheduleType":"periodic","cron":"0 3 * * *","enabled":true}`)
	if updatedTask.Code != http.StatusOK || responseData(t, updatedTask)["name"] != "nightly-renamed" {
		t.Fatalf("update sync task = %d: %s", updatedTask.Code, updatedTask.Body.String())
	}
	protected := testJSONRequest(t, handler, http.MethodDelete, "/api/sync/systems/"+strconv.FormatInt(systemID, 10), adminToken, "")
	if protected.Code != http.StatusConflict || responseData(t, protected)["references"] != float64(1) {
		t.Fatalf("delete referenced system = %d: %s", protected.Code, protected.Body.String())
	}

	createOther := testJSONRequest(t, handler, http.MethodPost, "/api/admin/users", adminToken, `{"username":"sync-other","password":"SyncOther123!","role":"user","quotaBytes":1048576}`)
	if createOther.Code != http.StatusCreated {
		t.Fatalf("create other user = %d: %s", createOther.Code, createOther.Body.String())
	}
	otherLogin := testJSONRequest(t, handler, http.MethodPost, "/api/auth/login", "", `{"username":"sync-other","password":"SyncOther123!"}`)
	otherToken := responseData(t, otherLogin)["token"].(string)
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		path := "/api/sync/tasks/" + strconv.FormatInt(taskID, 10)
		body := ""
		if method == http.MethodPut {
			body = taskBody
		}
		response := testJSONRequest(t, handler, method, path, otherToken, body)
		if response.Code != http.StatusNotFound {
			t.Fatalf("cross-user %s task = %d: %s", method, response.Code, response.Body.String())
		}
	}

	deletedTask := testJSONRequest(t, handler, http.MethodDelete, "/api/sync/tasks/"+strconv.FormatInt(taskID, 10), adminToken, "")
	if deletedTask.Code != http.StatusOK {
		t.Fatalf("delete sync task = %d: %s", deletedTask.Code, deletedTask.Body.String())
	}
	deleted := testJSONRequest(t, handler, http.MethodDelete, "/api/sync/systems/"+strconv.FormatInt(systemID, 10), adminToken, "")
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete unused system = %d: %s", deleted.Code, deleted.Body.String())
	}
}

func TestSyncSystemSecretEndpoint(t *testing.T) {
	_, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)
	created := testJSONRequest(t, handler, http.MethodPost, "/api/sync/systems", adminToken, `{"name":"secret-view","host":"sftp.example","port":22,"username":"backup-user","authType":"password","authSecret":"original-secret"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create sync system = %d: %s", created.Code, created.Body.String())
	}
	systemID := int64(responseData(t, created)["id"].(float64))
	secretPath := "/api/sync/systems/" + strconv.FormatInt(systemID, 10) + "/secret"
	secretResponse := testJSONRequest(t, handler, http.MethodGet, secretPath, adminToken, "")
	if secretResponse.Code != http.StatusOK {
		t.Fatalf("get sync system secret = %d: %s", secretResponse.Code, secretResponse.Body.String())
	}
	secretData := responseData(t, secretResponse)
	if secretData["secret"] != "original-secret" || secretData["authPassphrase"] != "" {
		t.Fatalf("sync system secret data = %#v", secretData)
	}

	createdOther := testJSONRequest(t, handler, http.MethodPost, "/api/admin/users", adminToken, `{"username":"sync-secret-other","password":"SyncSecretOther123!","role":"user","quotaBytes":1048576}`)
	if createdOther.Code != http.StatusCreated {
		t.Fatalf("create other user = %d: %s", createdOther.Code, createdOther.Body.String())
	}
	otherLogin := testJSONRequest(t, handler, http.MethodPost, "/api/auth/login", "", `{"username":"sync-secret-other","password":"SyncSecretOther123!"}`)
	otherToken := responseData(t, otherLogin)["token"].(string)
	unauthorized := testJSONRequest(t, handler, http.MethodGet, secretPath, otherToken, "")
	if unauthorized.Code != http.StatusNotFound {
		t.Fatalf("cross-user get sync system secret = %d: %s", unauthorized.Code, unauthorized.Body.String())
	}
}

func TestSyncTaskValidationAndLogRetention(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	createdSystem := testJSONRequest(t, handler, http.MethodPost, "/api/sync/systems", token, `{"name":"local-test","host":"localhost","username":"u","authType":"password","authSecret":"p"}`)
	systemID := int64(responseData(t, createdSystem)["id"].(float64))
	for _, policy := range []string{"overwrite", "skip", "rename"} {
		body := `{"name":"` + policy + `","direction":"pull","remoteSystemId":` + strconv.FormatInt(systemID, 10) + `,"sourceType":"sftp","sourcePath":"/in","targetType":"filebox","targetPath":"out","conflictPolicy":"` + policy + `","scheduleType":"once","enabled":true}`
		response := testJSONRequest(t, handler, http.MethodPost, "/api/sync/tasks", token, body)
		if response.Code != http.StatusCreated {
			t.Fatalf("create %s task = %d: %s", policy, response.Code, response.Body.String())
		}
	}
	invalid := testJSONRequest(t, handler, http.MethodPost, "/api/sync/tasks", token, `{"name":"bad","direction":"push","remoteSystemId":1,"sourceType":"filebox","sourcePath":"../escape","targetType":"sftp","targetPath":"/x","conflictPolicy":"overwrite","scheduleType":"periodic","cron":"not-cron","enabled":true}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid task = %d: %s", invalid.Code, invalid.Body.String())
	}

	tasks, err := db.ListSyncTasks(context.Background(), 1, true)
	if err != nil || len(tasks) == 0 {
		t.Fatalf("list sync tasks = %d, %v", len(tasks), err)
	}
	task := tasks[0]
	old := time.Now().UTC().Add(-31 * 24 * time.Hour).Format(time.RFC3339)
	if _, err := db.CreateSyncLog(context.Background(), storeSyncLog(task.ID, task.UserID, old)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateSyncLog(context.Background(), storeSyncLog(task.ID, task.UserID, time.Now().UTC().Format(time.RFC3339))); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PruneSyncLogs(context.Background(), 30); err != nil {
		t.Fatal(err)
	}
	logs, total, err := db.ListSyncLogs(context.Background(), task.ID, 1, 20)
	if err != nil || total != 1 || len(logs) != 1 || logs[0].RunAt == old {
		t.Fatalf("retained sync logs = total %d items %#v err %v", total, logs, err)
	}
}

func TestSyncTaskExecutionCompletesLog(t *testing.T) {
	_, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	createdSystem := testJSONRequest(t, handler, http.MethodPost, "/api/sync/systems", token, `{"name":"unreachable-sftp","host":"127.0.0.1","port":1,"username":"sync","authType":"password","authSecret":"secret"}`)
	if createdSystem.Code != http.StatusCreated {
		t.Fatalf("create unreachable sftp = %d: %s", createdSystem.Code, createdSystem.Body.String())
	}
	systemID := int64(responseData(t, createdSystem)["id"].(float64))
	createdTask := testJSONRequest(t, handler, http.MethodPost, "/api/sync/tasks", token, `{"name":"unreachable-task","direction":"push","remoteSystemId":`+strconv.FormatInt(systemID, 10)+`,"sourceType":"filebox","sourcePath":"","targetType":"sftp","targetPath":"/","conflictPolicy":"overwrite","scheduleType":"once","enabled":true}`)
	if createdTask.Code != http.StatusCreated {
		t.Fatalf("create unreachable task = %d: %s", createdTask.Code, createdTask.Body.String())
	}
	taskID := int64(responseData(t, createdTask)["id"].(float64))
	run := testJSONRequest(t, handler, http.MethodPost, "/api/sync/tasks/"+strconv.FormatInt(taskID, 10)+"/run", token, "")
	if run.Code != http.StatusAccepted {
		t.Fatalf("run unreachable task = %d: %s", run.Code, run.Body.String())
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		detail := testJSONRequest(t, handler, http.MethodGet, "/api/sync/tasks/"+strconv.FormatInt(taskID, 10), token, "")
		if detail.Code == http.StatusOK {
			for _, rawLog := range responseData(t, detail)["logs"].([]any) {
				entry := rawLog.(map[string]any)
				if entry["result"] != "running" {
					finishedAt, _ := entry["finishedAt"].(string)
					if finishedAt == "" {
						t.Fatalf("completed sync log has no finishedAt: %#v", entry)
					}
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("sync task left only running logs")
}

func storeSyncLog(taskID, userID int64, runAt string) store.SyncLog {
	return store.SyncLog{TaskID: taskID, UserID: userID, RunAt: runAt, Direction: "push", Result: "success", Message: "ok"}
}

func TestValidateRemotePathNormalization(t *testing.T) {
	// 归一化规则：`/` 与绝对路径均合法（#4 目录上级导航的后端基础）。
	// Normalization rules: `/` and absolute paths are valid (backend basis for #4 parent navigation).
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{"", ".", true},
		{"   ", ".", true},
		{".", ".", true},
		{"/", "/", true},
		{"/tmp", "/tmp", true},
		{"/tmp/", "/tmp", true},
		{"/tmp/a", "/tmp/a", true},
		{"foo", "foo", true},
		{"foo/bar", "foo/bar", true},
		{"../escape", "", false},
		{"/tmp/../etc", "/etc", true},
		{"a\\b", "", false},
		{"bad\x00path", "", false},
	}
	for _, test := range tests {
		got, err := validateRemotePath(test.input)
		if (err == nil) != test.ok || (err == nil && got != test.want) {
			t.Fatalf("validateRemotePath(%q) = %q, %v; want %q ok=%t", test.input, got, err, test.want, test.ok)
		}
	}
}

func TestValidateFileBoxSyncPathNormalization(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{"docs", "docs", true},
		{"docs/readme.txt", "docs/readme.txt", true},
		{"a\\b", "a/b", true},
		{"/absolute", "", false},
		{"..", "", false},
		{"a/../b", "", false},
		{"a:b", "", false},
		{"a\x00b", "", false},
	}
	for _, test := range tests {
		got, err := validateFileBoxSyncPath(test.input, false)
		if (err == nil) != test.ok || (err == nil && got != test.want) {
			t.Fatalf("validateFileBoxSyncPath(%q, false) = %q, %v; want %q ok=%t", test.input, got, err, test.want, test.ok)
		}
	}
	// allowEmpty 分支：空路径合法。
	got, err := validateFileBoxSyncPath("", true)
	if err != nil || got != "" {
		t.Fatalf("validateFileBoxSyncPath(\"\", true) = %q, %v", got, err)
	}
}

func TestValidateSyncTaskDirectionMatrix(t *testing.T) {
	// 方向矩阵（#5）：push 支持 filebox→sftp / filebox→filebox；pull 支持 sftp→filebox / filebox→filebox。
	// Direction matrix (#5): push allows filebox→sftp / filebox→filebox; pull allows sftp→filebox / filebox→filebox.
	valid := []struct {
		name, direction, sourceType, targetType string
	}{
		{"push sftp", "push", "filebox", "sftp"},
		{"push filebox", "push", "filebox", "filebox"},
		{"pull sftp", "pull", "sftp", "filebox"},
		{"pull filebox", "pull", "filebox", "filebox"},
	}
	for _, test := range valid {
		targetPath := "."
		if test.targetType == "filebox" {
			targetPath = ""
		}
		input := syncTaskRequest{Name: "t", Direction: test.direction, RemoteSystemID: 1, SourceType: test.sourceType, SourcePath: "a", TargetType: test.targetType, TargetPath: targetPath, ConflictPolicy: "overwrite", ScheduleType: "once"}
		if _, err := validateSyncTaskInput(input); err != nil {
			t.Fatalf("valid matrix %s (%s→%s): %v", test.name, test.sourceType, test.targetType, err)
		}
	}
	invalid := []struct {
		name, direction, sourceType, targetType string
	}{
		{"push sftp source", "push", "sftp", "filebox"},
		{"pull sftp target", "pull", "filebox", "sftp"},
		{"both sftp", "push", "sftp", "sftp"},
	}
	for _, test := range invalid {
		input := syncTaskRequest{Name: "t", Direction: test.direction, RemoteSystemID: 1, SourceType: test.sourceType, SourcePath: "a", TargetType: test.targetType, TargetPath: ".", ConflictPolicy: "overwrite", ScheduleType: "once"}
		if _, err := validateSyncTaskInput(input); err == nil {
			t.Fatalf("invalid matrix %s (%s→%s) accepted", test.name, test.sourceType, test.targetType)
		}
	}
	// sourceKind 消歧：file 与 directory 合法，其他拒绝。
	for _, kind := range []string{"", "directory", "file"} {
		input := syncTaskRequest{Name: "t", Direction: "push", RemoteSystemID: 1, SourceType: "filebox", SourcePath: "a", SourceKind: kind, TargetType: "sftp", TargetPath: ".", ConflictPolicy: "overwrite", ScheduleType: "once"}
		if _, err := validateSyncTaskInput(input); err != nil {
			t.Fatalf("sourceKind %q rejected: %v", kind, err)
		}
	}
	bad := syncTaskRequest{Name: "t", Direction: "push", RemoteSystemID: 1, SourceType: "filebox", SourcePath: "a", SourceKind: "folder", TargetType: "sftp", TargetPath: ".", ConflictPolicy: "overwrite", ScheduleType: "once"}
	if _, err := validateSyncTaskInput(bad); err == nil {
		t.Fatal("invalid sourceKind accepted")
	}
}

func TestValidateSyncSystemInputFileBoxKind(t *testing.T) {
	// FileBox 目标系统校验：url 必须合法 http(s)、仅密码认证、禁内嵌账号密码。
	// FileBox remote validation: url must be valid http(s), password auth only, no embedded credentials.
	valid := syncSystemRequest{Name: "remote-filebox", Kind: "filebox", URL: "https://files.example.com", Username: "u", AuthType: "password", AuthSecret: "p"}
	if _, err := validateSyncSystemInput(valid, true); err != nil {
		t.Fatalf("valid filebox system rejected: %v", err)
	}
	invalid := []syncSystemRequest{
		{Name: "n", Kind: "filebox", URL: "ftp://files.example.com", Username: "u", AuthType: "password", AuthSecret: "p"},
		{Name: "n", Kind: "filebox", URL: "https://user:pass@files.example.com", Username: "u", AuthType: "password", AuthSecret: "p"},
		{Name: "n", Kind: "filebox", URL: "not-a-url", Username: "u", AuthType: "password", AuthSecret: "p"},
		{Name: "n", Kind: "filebox", URL: "https://files.example.com", Username: "u", AuthType: "key", AuthSecret: "p"},
		{Name: "n", Kind: "filebox", URL: "https://files.example.com", Username: "u", AuthType: "password", AuthSecret: ""},
		{Name: "n", Kind: "bogus", URL: "https://files.example.com", Username: "u", AuthType: "password", AuthSecret: "p"},
	}
	for index, input := range invalid {
		if _, err := validateSyncSystemInput(input, true); err == nil {
			t.Fatalf("invalid filebox system #%d accepted: %#v", index, input)
		}
	}
}

func TestCreateFileBoxSystemRequiresPassword(t *testing.T) {
	// v014（#3）：FileBox 目标系统必须提供账号密码，缺少时返回明确中文提示。
	// v014 (#3): a FileBox remote system must carry a password; omitting it returns a clear message.
	db, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)
	missing := testJSONRequest(t, handler, http.MethodPost, "/api/sync/systems", adminToken, `{"name":"remote-filebox","kind":"filebox","url":"https://files.example.com","username":"u","authType":"password","authSecret":""}`)
	if missing.Code != http.StatusBadRequest || !strings.Contains(missing.Body.String(), "FileBox 目标系统必须设置账号密码") {
		t.Fatalf("create filebox without password = %d: %s", missing.Code, missing.Body.String())
	}
	created := testJSONRequest(t, handler, http.MethodPost, "/api/sync/systems", adminToken, `{"name":"remote-filebox","kind":"filebox","url":"https://files.example.com","username":"u","authType":"password","authSecret":"secret"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create filebox with password = %d: %s", created.Code, created.Body.String())
	}
	systemID := int64(responseData(t, created)["id"].(float64))
	// 编辑时留空密码 = 保留原凭据，应成功。
	// A blank password on edit keeps the existing credentials and must succeed.
	updated := testJSONRequest(t, handler, http.MethodPut, "/api/sync/systems/"+strconv.FormatInt(systemID, 10), adminToken, `{"name":"remote-filebox","kind":"filebox","url":"https://files.example.com","username":"u","authType":"password","authSecret":""}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update filebox keeping password = %d: %s", updated.Code, updated.Body.String())
	}
	// 更新为 SFTP 后不带密码：filebox 校验不再触发，但 requireSecret=false 允许留空保留原凭据。
	// 切换回 filebox 且原系统没有凭据的场景由 updateSyncSystem 拒绝，这里验证正常路径不受影响。
	var secretColumn string
	if err := db.DB.QueryRow("SELECT auth_secret FROM remote_systems WHERE id = ?", systemID).Scan(&secretColumn); err != nil || secretColumn == "" {
		t.Fatalf("kept credential after update = %q, %v", secretColumn, err)
	}
}

func TestSyncTaskRootPathAllowedAndSaved(t *testing.T) {
	// v014（#4）：源/目标路径为空串表示根目录，后端校验放行并可保存任务。
	// v014 (#4): an empty source/target path means the root directory; validation allows it and
	// the task saves successfully.
	db, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)
	createdSystem := testJSONRequest(t, handler, http.MethodPost, "/api/sync/systems", adminToken, `{"name":"local-test","host":"localhost","username":"u","authType":"password","authSecret":"p"}`)
	systemID := int64(responseData(t, createdSystem)["id"].(float64))
	rootTask := `{"name":"root-sync","direction":"push","remoteSystemId":` + strconv.FormatInt(systemID, 10) + `,"sourceType":"filebox","sourcePath":"","sourceKind":"directory","targetType":"sftp","targetPath":"","conflictPolicy":"overwrite","scheduleType":"once","enabled":true}`
	created := testJSONRequest(t, handler, http.MethodPost, "/api/sync/tasks", adminToken, rootTask)
	if created.Code != http.StatusCreated {
		t.Fatalf("create root-path task = %d: %s", created.Code, created.Body.String())
	}
	taskID := int64(responseData(t, created)["id"].(float64))
	// 拉取侧根目录：远端源空=home（.），本地目标空=根目录。
	pullRoot := `{"name":"root-pull","direction":"pull","remoteSystemId":` + strconv.FormatInt(systemID, 10) + `,"sourceType":"sftp","sourcePath":"","targetType":"filebox","targetPath":"","conflictPolicy":"skip","scheduleType":"once","enabled":true}`
	if response := testJSONRequest(t, handler, http.MethodPost, "/api/sync/tasks", adminToken, pullRoot); response.Code != http.StatusCreated {
		t.Fatalf("create root pull task = %d: %s", response.Code, response.Body.String())
	}
	// 非法路径仍被拒绝并返回明确中文提示。
	bad := testJSONRequest(t, handler, http.MethodPost, "/api/sync/tasks", adminToken, `{"name":"bad","direction":"push","remoteSystemId":`+strconv.FormatInt(systemID, 10)+`,"sourceType":"filebox","sourcePath":"../escape","targetType":"sftp","targetPath":"/x","conflictPolicy":"overwrite","scheduleType":"once","enabled":true}`)
	if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), "同步任务参数无效") {
		t.Fatalf("invalid path task = %d: %s", bad.Code, bad.Body.String())
	}
	task, err := db.GetSyncTask(context.Background(), taskID, 1, true)
	if err != nil || task.SourcePath != "" || task.TargetPath != "." {
		t.Fatalf("stored root task = %+v, %v", task, err)
	}
}

func TestSyncSystemConnectivityTestEndpoint(t *testing.T) {
	// v014（#5）：POST /api/sync/systems/{id}/test 返回 {ok:false, message, testedAt}，
	// message 不含凭据，并把失败结果持久化到 last_test_at/last_test_result。
	// v014 (#5): POST /api/sync/systems/{id}/test returns {ok:false, message, testedAt} without
	// leaking credentials and persists the failure into last_test_at/last_test_result.
	db, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)
	createdSFTP := testJSONRequest(t, handler, http.MethodPost, "/api/sync/systems", adminToken, `{"name":"dead-sftp","host":"127.0.0.1","port":1,"username":"u","authType":"password","authSecret":"top-secret"}`)
	sftpID := int64(responseData(t, createdSFTP)["id"].(float64))
	sftpTest := testJSONRequest(t, handler, http.MethodPost, "/api/sync/systems/"+strconv.FormatInt(sftpID, 10)+"/test", adminToken, "")
	if sftpTest.Code != http.StatusOK {
		t.Fatalf("sftp test status = %d: %s", sftpTest.Code, sftpTest.Body.String())
	}
	sftpData := responseData(t, sftpTest)
	if sftpData["ok"] != false || sftpData["testedAt"] == "" || sftpData["message"] == "" || strings.Contains(sftpData["message"].(string), "top-secret") {
		t.Fatalf("sftp test payload = %#v", sftpData)
	}
	createdFileBox := testJSONRequest(t, handler, http.MethodPost, "/api/sync/systems", adminToken, `{"name":"dead-filebox","kind":"filebox","url":"http://127.0.0.1:1","username":"u","authType":"password","authSecret":"secret-2"}`)
	fileboxID := int64(responseData(t, createdFileBox)["id"].(float64))
	fileboxTest := testJSONRequest(t, handler, http.MethodPost, "/api/sync/systems/"+strconv.FormatInt(fileboxID, 10)+"/test", adminToken, "")
	if fileboxTest.Code != http.StatusOK {
		t.Fatalf("filebox test status = %d: %s", fileboxTest.Code, fileboxTest.Body.String())
	}
	fileboxData := responseData(t, fileboxTest)
	if fileboxData["ok"] != false || fileboxData["testedAt"] == "" || fileboxData["message"] == "" || strings.Contains(fileboxData["message"].(string), "secret-2") {
		t.Fatalf("filebox test payload = %#v", fileboxData)
	}
	var lastTestAt, lastTestResult string
	if err := db.DB.QueryRow("SELECT last_test_at, last_test_result FROM remote_systems WHERE id = ?", fileboxID).Scan(&lastTestAt, &lastTestResult); err != nil {
		t.Fatal(err)
	}
	if lastTestAt == "" || lastTestResult != "failure" {
		t.Fatalf("persisted last test = %q, %q", lastTestAt, lastTestResult)
	}
	// 越权：普通用户不能测试他人系统。
	_ = testJSONRequest(t, handler, http.MethodPost, "/api/admin/users", adminToken, `{"username":"sync-other","password":"SyncOther123!","role":"user","quotaBytes":1048576}`)
	otherLogin := testJSONRequest(t, handler, http.MethodPost, "/api/auth/login", "", `{"username":"sync-other","password":"SyncOther123!"}`)
	if otherLogin.Code != http.StatusOK {
		t.Fatalf("other user login = %d: %s", otherLogin.Code, otherLogin.Body.String())
	}
	otherToken := responseData(t, otherLogin)["token"].(string)
	if denied := testJSONRequest(t, handler, http.MethodPost, "/api/sync/systems/"+strconv.FormatInt(sftpID, 10)+"/test", otherToken, ""); denied.Code != http.StatusNotFound {
		t.Fatalf("cross-user test = %d: %s", denied.Code, denied.Body.String())
	}
}

func TestShareGroupExtendAndIncreaseManagement(t *testing.T) {
	// v014（#6）：聚合分享支持延期（不缩短）与增次（不降低），越权一律 404。
	// v014 (#6): aggregate shares support extend (never shortened) and increase (never lowered);
	// unauthorized access returns 404.
	db, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)
	var adminID int64
	if err := db.DB.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	var fileIDs []string
	for _, name := range []string{"group-a.txt", "group-b.txt"} {
		result, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(?, ?, ?, 1, 'text/plain', 'sha', 'md5', 'ready', ?, ?)", adminID, name, name, "files/"+strconv.FormatInt(adminID, 10)+"/"+name, time.Now().UTC().Format(time.RFC3339))
		if err != nil {
			t.Fatal(err)
		}
		id, _ := result.LastInsertId()
		fileIDs = append(fileIDs, strconv.FormatInt(id, 10))
	}
	created := testJSONRequest(t, handler, http.MethodPost, "/api/files/batch-share-group", adminToken, `{"fileIds":[`+strings.Join(fileIDs, ",")+`],"expiresInHours":24,"maxDownloads":2}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create share group = %d: %s", created.Code, created.Body.String())
	}
	groupToken := responseData(t, created)["token"].(string)
	// 越权：其他用户延期/增次/撤销均 404。
	_ = testJSONRequest(t, handler, http.MethodPost, "/api/admin/users", adminToken, `{"username":"group-other","password":"GroupOther123!","role":"user","quotaBytes":1048576}`)
	otherLogin := testJSONRequest(t, handler, http.MethodPost, "/api/auth/login", "", `{"username":"group-other","password":"GroupOther123!"}`)
	otherToken := responseData(t, otherLogin)["token"].(string)
	for _, path := range []string{"/extend", "/increase"} {
		body := `{"expiresInHours":5}`
		if strings.HasSuffix(path, "increase") {
			body = `{"maxDownloads":5}`
		}
		if denied := testJSONRequest(t, handler, http.MethodPut, "/api/shared-groups/"+groupToken+path, otherToken, body); denied.Code != http.StatusNotFound {
			t.Fatalf("cross-user %s = %d: %s", path, denied.Code, denied.Body.String())
		}
	}
	// 上限校验：小时越界 / 次数降低 / 无效上限。
	if bad := testJSONRequest(t, handler, http.MethodPut, "/api/shared-groups/"+groupToken+"/extend", adminToken, `{"expiresInHours":0}`); bad.Code != http.StatusBadRequest {
		t.Fatalf("bad extend hours = %d: %s", bad.Code, bad.Body.String())
	}
	if bad := testJSONRequest(t, handler, http.MethodPut, "/api/shared-groups/"+groupToken+"/increase", adminToken, `{"maxDownloads":1}`); bad.Code != http.StatusBadRequest {
		t.Fatalf("decrease max = %d: %s", bad.Code, bad.Body.String())
	}
	if bad := testJSONRequest(t, handler, http.MethodPut, "/api/shared-groups/"+groupToken+"/increase", adminToken, `{"maxDownloads":-1}`); bad.Code != http.StatusBadRequest {
		t.Fatalf("negative max = %d: %s", bad.Code, bad.Body.String())
	}
	// 合法延期：新截止时间晚于原截止时间（87600 小时 ≈ 10 年）。
	extended := testJSONRequest(t, handler, http.MethodPut, "/api/shared-groups/"+groupToken+"/extend", adminToken, `{"expiresInHours":87600}`)
	if extended.Code != http.StatusOK {
		t.Fatalf("extend group = %d: %s", extended.Code, extended.Body.String())
	}
	extendedData := responseData(t, extended)
	expiresAt, _ := time.Parse(time.RFC3339, extendedData["expiresAt"].(string))
	if !expiresAt.After(time.Now().UTC().Add(80 * 24 * time.Hour)) {
		t.Fatalf("extended expiresAt not pushed far enough: %s", extendedData["expiresAt"])
	}
	// 合法增次：上限从 2 提升到 5。
	increased := testJSONRequest(t, handler, http.MethodPut, "/api/shared-groups/"+groupToken+"/increase", adminToken, `{"maxDownloads":5}`)
	if increased.Code != http.StatusOK || responseData(t, increased)["maxDownloads"] != float64(5) {
		t.Fatalf("increase group = %d: %s", increased.Code, increased.Body.String())
	}
	// 审计登记。
	var extendAudits, increaseAudits int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE action = 'share_group_extend'").Scan(&extendAudits); err != nil || extendAudits != 1 {
		t.Fatalf("share_group_extend audits = %d, %v", extendAudits, err)
	}
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE action = 'share_group_increase'").Scan(&increaseAudits); err != nil || increaseAudits != 1 {
		t.Fatalf("share_group_increase audits = %d, %v", increaseAudits, err)
	}
}
