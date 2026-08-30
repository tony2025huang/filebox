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
