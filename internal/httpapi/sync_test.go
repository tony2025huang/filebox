package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"filebox/internal/store"
)

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

func storeSyncLog(taskID, userID int64, runAt string) store.SyncLog {
	return store.SyncLog{TaskID: taskID, UserID: userID, RunAt: runAt, Direction: "push", Result: "success", Message: "ok"}
}
