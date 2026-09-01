package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"filebox/internal/store"
)

func TestReadOnlyWindowControlsWritesAndExposesState(t *testing.T) {
	db, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)
	created := testJSONRequest(t, handler, http.MethodPost, "/api/admin/users", adminToken, `{"username":"readonly-user","password":"Readonly123!","role":"user","quotaBytes":1048576}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create read-only user = %d: %s", created.Code, created.Body.String())
	}
	userID := int64(responseData(t, created)["id"].(float64))
	login := testJSONRequest(t, handler, http.MethodPost, "/api/auth/login", "", `{"username":"readonly-user","password":"Readonly123!"}`)
	if login.Code != http.StatusOK {
		t.Fatalf("read-only user login = %d: %s", login.Code, login.Body.String())
	}
	userToken := responseData(t, login)["token"].(string)

	folder := testJSONRequest(t, handler, http.MethodPost, "/api/folders", userToken, `{"name":"docs"}`)
	if folder.Code != http.StatusCreated {
		t.Fatalf("create folder before window = %d: %s", folder.Code, folder.Body.String())
	}
	folderID := int64(responseData(t, folder)["id"].(float64))
	fileInit := initUpload(t, handler, userToken, "readme.txt", 1, 0, "")
	fileTaskID := fileInit["taskId"].(string)
	if got := testBinaryRequest(t, handler, http.MethodPut, "/api/files/"+fileTaskID+"/chunks/0", userToken, []byte("x")); got.Code != http.StatusOK {
		t.Fatalf("file chunk before window = %d: %s", got.Code, got.Body.String())
	}
	fileComplete := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+fileTaskID+"/complete", userToken, `{}`)
	if fileComplete.Code != http.StatusOK {
		t.Fatalf("file complete before window = %d: %s", fileComplete.Code, fileComplete.Body.String())
	}
	fileID := int64(responseData(t, fileComplete)["id"].(float64))
	pending := initUpload(t, handler, userToken, "pending.txt", 1, 0, "")
	pendingTaskID := pending["taskId"].(string)

	from := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	until := time.Now().UTC().Add(time.Minute).Format(time.RFC3339)
	setWindow := testJSONRequest(t, handler, http.MethodPut, "/api/admin/users/"+formatID(userID)+"/read-only", adminToken, `{"from":"`+from+`","until":"`+until+`"}`)
	if setWindow.Code != http.StatusOK {
		t.Fatalf("set read-only window = %d: %s", setWindow.Code, setWindow.Body.String())
	}
	me := testJSONRequest(t, handler, http.MethodGet, "/api/auth/me", userToken, "")
	meData := responseData(t, me)
	if me.Code != http.StatusOK || meData["readOnly"] != true || meData["readOnlyFrom"] != from || meData["readOnlyUntil"] != until {
		t.Fatalf("me read-only state = %d %#v", me.Code, meData)
	}

	checks := []struct {
		name string
		got  *httptest.ResponseRecorder
	}{
		{name: "upload init", got: testJSONRequest(t, handler, http.MethodPost, "/api/files/upload-init", userToken, `{"name":"blocked.txt","size":1}`)},
		{name: "instant upload", got: testJSONRequest(t, handler, http.MethodPost, "/api/files/check", userToken, `{"sha256":"deadbeef","size":1,"name":"blocked.txt"}`)},
		{name: "change password", got: testJSONRequest(t, handler, http.MethodPost, "/api/auth/change-password", userToken, `{"oldPassword":"Readonly123!","newPassword":"Readonly456!"}`)},
		{name: "update language", got: testJSONRequest(t, handler, http.MethodPut, "/api/auth/language", userToken, `{"language":"en"}`)},
		{name: "upload chunk", got: testBinaryRequest(t, handler, http.MethodPut, "/api/files/"+pendingTaskID+"/chunks/0", userToken, []byte("x"))},
		{name: "upload complete", got: testJSONRequest(t, handler, http.MethodPost, "/api/files/"+pendingTaskID+"/complete", userToken, `{}`)},
		{name: "file delete", got: testJSONRequest(t, handler, http.MethodDelete, "/api/files/"+formatID(fileID), userToken, "")},
		{name: "batch delete", got: testJSONRequest(t, handler, http.MethodPost, "/api/files/batch-delete", userToken, `{"ids":[1]}`)},
		{name: "folder create", got: testJSONRequest(t, handler, http.MethodPost, "/api/folders", userToken, `{"name":"blocked"}`)},
		{name: "folder rename", got: testJSONRequest(t, handler, http.MethodPatch, "/api/folders/"+formatID(folderID), userToken, `{"name":"renamed"}`)},
		{name: "folder delete", got: testJSONRequest(t, handler, http.MethodDelete, "/api/folders/"+formatID(folderID), userToken, "")},
		{name: "share create", got: testJSONRequest(t, handler, http.MethodPost, "/api/files/"+formatID(fileID)+"/share", userToken, `{"expiresInHours":1,"maxDownloads":0}`)},
	}
	for _, check := range checks {
		if check.got.Code != http.StatusForbidden || responseCode(t, check.got) != "READ_ONLY" {
			t.Errorf("%s = %d code=%q body=%s", check.name, check.got.Code, responseCode(t, check.got), check.got.Body.String())
		}
	}
	list := testJSONRequest(t, handler, http.MethodGet, "/api/files", userToken, "")
	if list.Code != http.StatusOK {
		t.Fatalf("read-only file list = %d: %s", list.Code, list.Body.String())
	}
	download := testJSONRequest(t, handler, http.MethodGet, "/api/files/"+formatID(fileID)+"/download", userToken, "")
	if download.Code != http.StatusOK || download.Body.String() != "x" {
		t.Fatalf("read-only download = %d body=%q", download.Code, download.Body.String())
	}

	var auditCount int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE user_id = ? AND reason = 'read_only'", userID).Scan(&auditCount); err != nil || auditCount < len(checks) {
		t.Fatalf("read-only audit count = %d, %v; want at least %d", auditCount, err, len(checks))
	}
	clearWindow := testJSONRequest(t, handler, http.MethodPut, "/api/admin/users/"+formatID(userID)+"/read-only", adminToken, `{"from":"","until":""}`)
	if clearWindow.Code != http.StatusOK {
		t.Fatalf("clear read-only window = %d: %s", clearWindow.Code, clearWindow.Body.String())
	}
	me = testJSONRequest(t, handler, http.MethodGet, "/api/auth/me", userToken, "")
	if responseData(t, me)["readOnly"] != false {
		t.Fatalf("cleared read-only state = %#v", responseData(t, me))
	}
	if got := testJSONRequest(t, handler, http.MethodPost, "/api/files/upload-init", userToken, `{"name":"restored.txt","size":1}`); got.Code != http.StatusOK {
		t.Fatalf("upload after clear = %d: %s", got.Code, got.Body.String())
	}

	unauthorized := testJSONRequest(t, handler, http.MethodPut, "/api/admin/users/"+formatID(userID)+"/read-only", userToken, `{"from":"","until":""}`)
	if unauthorized.Code != http.StatusForbidden {
		t.Fatalf("non-admin read-only update = %d: %s", unauthorized.Code, unauthorized.Body.String())
	}

	adminWindow := testJSONRequest(t, handler, http.MethodPut, "/api/admin/users/1/read-only", adminToken, `{"from":"2000-01-01T00:00:00Z","until":"2999-01-01T00:00:00Z"}`)
	if adminWindow.Code != http.StatusOK {
		t.Fatalf("set admin read-only window = %d: %s", adminWindow.Code, adminWindow.Body.String())
	}
	if got := testJSONRequest(t, handler, http.MethodPost, "/api/files/upload-init", adminToken, `{"name":"admin-still-writes.txt","size":1}`); got.Code != http.StatusOK {
		t.Fatalf("admin write in read-only window = %d: %s", got.Code, got.Body.String())
	}
}

func TestReadOnlyBlocksSyncAndCollectionWrites(t *testing.T) {
	_, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)
	created := testJSONRequest(t, handler, http.MethodPost, "/api/admin/users", adminToken, `{"username":"ro-sync","password":"Readonly123!","role":"user","quotaBytes":1048576}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create read-only user = %d: %s", created.Code, created.Body.String())
	}
	userID := int64(responseData(t, created)["id"].(float64))
	login := testJSONRequest(t, handler, http.MethodPost, "/api/auth/login", "", `{"username":"ro-sync","password":"Readonly123!"}`)
	if login.Code != http.StatusOK {
		t.Fatalf("read-only user login = %d: %s", login.Code, login.Body.String())
	}
	userToken := responseData(t, login)["token"].(string)

	system := testJSONRequest(t, handler, http.MethodPost, "/api/sync/systems", userToken, `{"name":"ro-system","host":"127.0.0.1","port":22,"username":"u","authType":"password","authSecret":"x"}`)
	if system.Code != http.StatusCreated {
		t.Fatalf("create sync system before window = %d: %s", system.Code, system.Body.String())
	}
	systemID := int64(responseData(t, system)["id"].(float64))
	taskBody := `{"name":"ro-task","direction":"push","remoteSystemId":` + formatID(systemID) + `,"sourceType":"filebox","sourcePath":"","targetType":"sftp","targetPath":".","conflictPolicy":"overwrite","scheduleType":"once"}`
	task := testJSONRequest(t, handler, http.MethodPost, "/api/sync/tasks", userToken, taskBody)
	if task.Code != http.StatusCreated {
		t.Fatalf("create sync task before window = %d: %s", task.Code, task.Body.String())
	}
	taskID := int64(responseData(t, task)["id"].(float64))
	collection := testJSONRequest(t, handler, http.MethodPost, "/api/collections", userToken, `{"name":"c","expiresInHours":1}`)
	if collection.Code != http.StatusCreated {
		t.Fatalf("create collection before window = %d: %s", collection.Code, collection.Body.String())
	}
	collectionData := responseData(t, collection)
	collectionID := int64(collectionData["id"].(float64))
	collectionToken := collectionData["token"].(string)

	from := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	until := time.Now().UTC().Add(time.Minute).Format(time.RFC3339)
	setWindow := testJSONRequest(t, handler, http.MethodPut, "/api/admin/users/"+formatID(userID)+"/read-only", adminToken, `{"from":"`+from+`","until":"`+until+`"}`)
	if setWindow.Code != http.StatusOK {
		t.Fatalf("set read-only window = %d: %s", setWindow.Code, setWindow.Body.String())
	}

	validSystem := `{"name":"ro-system-updated","host":"127.0.0.1","port":22,"username":"u","authType":"password","authSecret":""}`
	validTask := `{"name":"ro-task-updated","direction":"push","remoteSystemId":` + formatID(systemID) + `,"sourceType":"filebox","sourcePath":"","targetType":"sftp","targetPath":".","conflictPolicy":"overwrite","scheduleType":"once"}`
	checks := []struct {
		name string
		got  *httptest.ResponseRecorder
	}{
		{name: "sync system create", got: testJSONRequest(t, handler, http.MethodPost, "/api/sync/systems", userToken, `{"name":"blocked-system","host":"127.0.0.1","port":22,"username":"u","authType":"password","authSecret":"x"}`)},
		{name: "sync system update", got: testJSONRequest(t, handler, http.MethodPut, "/api/sync/systems/"+formatID(systemID), userToken, validSystem)},
		{name: "sync system delete", got: testJSONRequest(t, handler, http.MethodDelete, "/api/sync/systems/"+formatID(systemID), userToken, "")},
		{name: "sync task create", got: testJSONRequest(t, handler, http.MethodPost, "/api/sync/tasks", userToken, taskBody)},
		{name: "sync task update", got: testJSONRequest(t, handler, http.MethodPut, "/api/sync/tasks/"+formatID(taskID), userToken, validTask)},
		{name: "sync task delete", got: testJSONRequest(t, handler, http.MethodDelete, "/api/sync/tasks/"+formatID(taskID), userToken, "")},
		{name: "sync task run", got: testJSONRequest(t, handler, http.MethodPost, "/api/sync/tasks/"+formatID(taskID)+"/run", userToken, "")},
		{name: "collection create", got: testJSONRequest(t, handler, http.MethodPost, "/api/collections", userToken, `{"name":"blocked-collection","expiresInHours":1}`)},
		{name: "collection delete", got: testJSONRequest(t, handler, http.MethodDelete, "/api/collections/"+formatID(collectionID), userToken, "")},
		{name: "collection upload init", got: testJSONRequest(t, handler, http.MethodPost, "/api/collections/"+collectionToken+"/upload-init", "", `{"name":"a.txt","size":1}`)},
	}
	for _, check := range checks {
		if check.got.Code != http.StatusForbidden || responseCode(t, check.got) != "READ_ONLY" {
			t.Errorf("%s = %d code=%q body=%s", check.name, check.got.Code, responseCode(t, check.got), check.got.Body.String())
		}
	}
	clearWindow := testJSONRequest(t, handler, http.MethodPut, "/api/admin/users/"+formatID(userID)+"/read-only", adminToken, `{"from":"","until":""}`)
	if clearWindow.Code != http.StatusOK {
		t.Fatalf("clear read-only window = %d: %s", clearWindow.Code, clearWindow.Body.String())
	}
}

func TestReadOnlyBlocksCollectionChunkUpload(t *testing.T) {
	db, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)
	created := testJSONRequest(t, handler, http.MethodPost, "/api/admin/users", adminToken, `{"username":"ro-collect","password":"Readonly123!","role":"user","quotaBytes":1048576}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create user = %d: %s", created.Code, created.Body.String())
	}
	userID := int64(responseData(t, created)["id"].(float64))
	login := testJSONRequest(t, handler, http.MethodPost, "/api/auth/login", "", `{"username":"ro-collect","password":"Readonly123!"}`)
	if login.Code != http.StatusOK {
		t.Fatalf("login = %d: %s", login.Code, login.Body.String())
	}
	userToken := responseData(t, login)["token"].(string)
	collection := testJSONRequest(t, handler, http.MethodPost, "/api/collections", userToken, `{"name":"c","expiresInHours":1}`)
	if collection.Code != http.StatusCreated {
		t.Fatalf("create collection = %d: %s", collection.Code, collection.Body.String())
	}
	collectionToken := responseData(t, collection)["token"].(string)
	init := testJSONRequest(t, handler, http.MethodPost, "/api/collections/"+collectionToken+"/upload-init", "", `{"name":"a.txt","size":1,"chunkSize":0}`)
	if init.Code != http.StatusOK {
		t.Fatalf("collection init = %d: %s", init.Code, init.Body.String())
	}
	taskID := responseData(t, init)["taskId"].(string)
	if chunk := testBinaryRequest(t, handler, http.MethodPut, "/api/collections/"+collectionToken+"/upload-chunk/"+taskID+"/0", "", []byte("x")); chunk.Code != http.StatusOK {
		t.Fatalf("collection chunk before window = %d: %s", chunk.Code, chunk.Body.String())
	}
	from := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	until := time.Now().UTC().Add(time.Minute).Format(time.RFC3339)
	setWindow := testJSONRequest(t, handler, http.MethodPut, "/api/admin/users/"+formatID(userID)+"/read-only", adminToken, `{"from":"`+from+`","until":"`+until+`"}`)
	if setWindow.Code != http.StatusOK {
		t.Fatalf("set read-only window = %d: %s", setWindow.Code, setWindow.Body.String())
	}
	blocked := testBinaryRequest(t, handler, http.MethodPut, "/api/collections/"+collectionToken+"/upload-chunk/"+taskID+"/0", "", []byte("y"))
	if blocked.Code != http.StatusForbidden || responseCode(t, blocked) != "READ_ONLY" {
		t.Fatalf("collection chunk in read-only window = %d code=%q body=%s", blocked.Code, responseCode(t, blocked), blocked.Body.String())
	}
	var auditCount int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE reason = 'read_only' AND action = 'upload_collect_fail'").Scan(&auditCount); err != nil || auditCount < 1 {
		t.Fatalf("read-only collection chunk audit count = %d, %v; want >= 1", auditCount, err)
	}
}

func TestUserReadOnlyAtUsesInclusiveBoundariesAndRejectsInvalidWindows(t *testing.T) {
	from := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)
	until := from.Add(time.Hour)
	user := store.User{Role: "user", ReadOnlyFrom: from.Format(time.RFC3339), ReadOnlyUntil: until.Format(time.RFC3339)}
	if !userReadOnlyAt(user, from) || !userReadOnlyAt(user, until) || userReadOnlyAt(user, from.Add(-time.Nanosecond)) || userReadOnlyAt(user, until.Add(time.Nanosecond)) {
		t.Fatal("read-only boundaries are not inclusive")
	}
	user.Role = "admin"
	if userReadOnlyAt(user, from) {
		t.Fatal("administrator should be exempt from read-only")
	}
	user.Role = "user"
	user.ReadOnlyFrom = "invalid"
	if userReadOnlyAt(user, from) {
		t.Fatal("invalid read-only window should not activate")
	}
}

func formatID(id int64) string { return strconv.FormatInt(id, 10) }

func responseCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var body response
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	data, _ := body.Data.(map[string]any)
	code, _ := data["code"].(string)
	return code
}
