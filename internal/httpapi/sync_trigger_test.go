package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"filebox/internal/store"
)

// newTriggeredTestServer creates one admin user, one remote system and one enabled
// triggered task (source "docs") with a short debounce for tests.
func newTriggeredTestServer(t *testing.T) (*store.Store, *Server, store.SyncTask) {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE users SET must_change_password = 0 WHERE username = 'admin'"); err != nil {
		t.Fatal(err)
	}
	user, err := db.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	system, err := db.CreateRemoteSystem(context.Background(), store.RemoteSystem{UserID: user.ID, Name: "remote", Kind: "sftp", Host: "sftp.example", Port: 22, Username: "sync", AuthType: "password", AuthSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(db, Config{DataDir: db.DataDir, JWTSecret: []byte("test-secret")})
	server.triggerDebounce = 60 * time.Millisecond
	task, err := db.CreateSyncTask(context.Background(), store.SyncTask{UserID: user.ID, Name: "watch", Direction: "push", RemoteSystemID: system.ID, SourceType: "filebox", SourcePath: "docs", SourceKind: "directory", TargetType: "sftp", TargetPath: ".", ConflictPolicy: "overwrite", ScheduleType: "triggered", Enabled: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	return db, server, task
}

func TestTriggerSourceMatches(t *testing.T) {
	cases := []struct {
		source, changed string
		want            bool
	}{
		{"", "a", true}, {"", "", true}, {"", "a/b/c", true},
		{"a", "a", true}, {"a", "a/b", true}, {"a/b", "a/b/c/d", true},
		{"a", "b", false}, {"a", "ab", false}, {"a/b", "a", false},
		{"a/b", "a/b2", false}, {"a\\b", "a\\b\\c", true}, {"a/b", "a\\b", true},
	}
	for _, c := range cases {
		if got := triggerSourceMatches(c.source, c.changed); got != c.want {
			t.Errorf("triggerSourceMatches(%q, %q) = %v, want %v", c.source, c.changed, got, c.want)
		}
	}
}

func TestTriggerUserDirHelpers(t *testing.T) {
	if got := userDirFromStorageDir("files/7/a/b"); got != "a/b" {
		t.Errorf("userDirFromStorageDir(files/7/a/b) = %q", got)
	}
	if got := userDirFromStorageDir("files/7"); got != "" {
		t.Errorf("userDirFromStorageDir(files/7) = %q", got)
	}
	if got := userDirFromStorageDir("uploads/tok/sub"); got != "uploads/tok/sub" {
		t.Errorf("userDirFromStorageDir(uploads/tok/sub) = %q", got)
	}
	dirs := userDirsFromStoragePaths([]string{"files/7/a/f.txt", "files/7/a/g.txt", "files/7/b/h.txt", "files/7/other/x"})
	if strings.Join(dirs, "|") != "a|b|other" {
		t.Errorf("userDirsFromStoragePaths = %q", strings.Join(dirs, "|"))
	}
}

func TestTriggeredDebounceAfterFinalChangeRunsOnce(t *testing.T) {
	_, server, task := newTriggeredTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server.StartSyncTriggers(ctx)
	runs := make(chan int64, 8)
	server.triggerRunHook = func(ctx context.Context, item store.SyncTask) { runs <- item.ID }

	server.notifyFileBoxChange(task.UserID, "docs")
	select {
	case <-runs:
		t.Fatal("task ran before the debounce window elapsed")
	case <-time.After(20 * time.Millisecond):
	}
	// 去抖窗口内的再次变更会重启倒计时，最后一次变更后约 debounce 才执行一次。
	server.notifyFileBoxChange(task.UserID, "docs/sub")
	select {
	case id := <-runs:
		if id != task.ID {
			t.Fatalf("ran wrong task %d", id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("task did not run after debounce")
	}
	select {
	case extra := <-runs:
		t.Fatalf("unexpected extra run %d", extra)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestTriggeredCoalescesChangesDuringActiveRun(t *testing.T) {
	_, server, task := newTriggeredTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server.StartSyncTriggers(ctx)

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	var count int
	server.triggerRunHook = func(ctx context.Context, item store.SyncTask) {
		count++
		if count == 1 {
			close(started)
			<-release
		}
		if count >= 2 {
			select {
			case <-done:
			default:
				close(done)
			}
		}
	}

	server.notifyFileBoxChange(task.UserID, "docs")
	<-started // first run in progress (manager holds the serial lock)
	// 运行期间的多次变更合并为“完成后的一次补跑”。
	server.notifyFileBoxChange(task.UserID, "docs/a")
	server.notifyFileBoxChange(task.UserID, "docs/b")
	close(release)
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatalf("coalesced follow-up run missing; count=%d", count)
	}
	time.Sleep(200 * time.Millisecond)
	if count != 2 {
		t.Fatalf("expected exactly 2 runs after coalescing, got %d", count)
	}
}

func TestTriggeredIgnoresNonMatchingAndDisabled(t *testing.T) {
	db, server, task := newTriggeredTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server.StartSyncTriggers(ctx)
	runs := make(chan int64, 8)
	server.triggerRunHook = func(ctx context.Context, item store.SyncTask) { runs <- item.ID }

	server.notifyFileBoxChange(task.UserID, "unrelated")
	select {
	case <-runs:
		t.Fatal("non-matching change ran the task")
	case <-time.After(180 * time.Millisecond):
	}
	// 停用任务：registry 刷新后不应再执行。
	if err := db.UpdateSyncTask(context.Background(), store.SyncTask{ID: task.ID, UserID: task.UserID, Name: task.Name, Direction: task.Direction, RemoteSystemID: task.RemoteSystemID, SourceType: task.SourceType, SourcePath: task.SourcePath, SourceKind: task.SourceKind, TargetType: task.TargetType, TargetPath: task.TargetPath, ConflictPolicy: task.ConflictPolicy, ScheduleType: task.ScheduleType, Cron: task.Cron, Enabled: false}, task.UserID, false); err != nil {
		t.Fatal(err)
	}
	server.refreshTriggeredTasks(context.Background())
	server.notifyFileBoxChange(task.UserID, "docs")
	select {
	case <-runs:
		t.Fatal("disabled task ran")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestTriggeredValidationRejectsNonFileBoxSource(t *testing.T) {
	base := syncTaskRequest{Name: "t", Direction: "pull", RemoteSystemID: 1, SourceType: "sftp", SourcePath: "x", TargetType: "filebox", ConflictPolicy: "overwrite", ScheduleType: "triggered"}
	if _, err := validateSyncTaskInput(base); err == nil {
		t.Fatal("triggered pull accepted")
	}
	base.Direction = "push"
	base.SourceType = "filebox"
	base.TargetType = "sftp"
	base.SourceKind = "file"
	if _, err := validateSyncTaskInput(base); err == nil {
		t.Fatal("triggered single-file source accepted")
	}
	base.SourceKind = "directory"
	validated, err := validateSyncTaskInput(base)
	if err != nil {
		t.Fatalf("valid triggered push rejected: %v", err)
	}
	if validated.ScheduleType != "triggered" || validated.Cron != "" || validated.SourceKind != "directory" {
		t.Fatalf("validated = %+v", validated)
	}
}

func TestFolderCreateEndpointNotifiesTriggeredTask(t *testing.T) {
	db, server, task := newTriggeredTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server.StartSyncTriggers(ctx)
	runs := make(chan int64, 8)
	server.triggerRunHook = func(ctx context.Context, item store.SyncTask) { runs <- item.ID }

	// 预建源目录记录（直接经 store，不经过通知路径），随后通过文件夹创建 API
	// 在源目录下新建子目录，验证端点确实把变化通知给协调器。
	admin, err := db.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateFolder(context.Background(), admin.ID, "", "docs"); err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	token := testAdminToken(t, handler)
	created := testJSONRequest(t, handler, http.MethodPost, "/api/folders", token, `{"name":"new","parent":"docs"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create folder = %d: %s", created.Code, created.Body.String())
	}
	select {
	case id := <-runs:
		if id != task.ID {
			t.Fatalf("triggered wrong task %d", id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("folder-create change did not run the triggered task")
	}
}

func TestTriggeredRestEndpointRejectsPullAndAcceptsPush(t *testing.T) {
	_, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	created := testJSONRequest(t, handler, http.MethodPost, "/api/sync/systems", token, `{"name":"remote","host":"sftp.example","port":22,"username":"sync","authType":"password","authSecret":"secret"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create system = %d: %s", created.Code, created.Body.String())
	}
	systemID := int64(responseData(t, created)["id"].(float64))
	idText := strconv.FormatInt(systemID, 10)
	pullBody := `{"name":"bad","direction":"pull","remoteSystemId":` + idText + `,"sourceType":"sftp","sourcePath":"x","targetType":"filebox","scheduleType":"triggered","enabled":true}`
	rejected := testJSONRequest(t, handler, http.MethodPost, "/api/sync/tasks", token, pullBody)
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("triggered pull create = %d: %s", rejected.Code, rejected.Body.String())
	}
	pushBody := `{"name":"good","direction":"push","remoteSystemId":` + idText + `,"sourceType":"filebox","sourcePath":"docs","sourceKind":"directory","targetType":"sftp","targetPath":".","conflictPolicy":"overwrite","scheduleType":"triggered","enabled":true}`
	accepted := testJSONRequest(t, handler, http.MethodPost, "/api/sync/tasks", token, pushBody)
	if accepted.Code != http.StatusCreated {
		t.Fatalf("triggered push create = %d: %s", accepted.Code, accepted.Body.String())
	}
	data := responseData(t, accepted)
	if data["scheduleType"] != "triggered" || data["cron"] != "" {
		t.Fatalf("created task data = %#v", data)
	}
}
