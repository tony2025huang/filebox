package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"filebox/internal/store"
)

func newTestServer(t *testing.T) (*store.Store, http.Handler) {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureAdmin("admin", "admin123", 1024*1024); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE users SET must_change_password = 0 WHERE username = 'admin'"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	server := NewServer(db, Config{DataDir: db.DataDir, MaxFileSize: 1024 * 1024, MinFreeSpace: 0, JWTSecret: []byte("test-secret")})
	t.Cleanup(func() { db.Close() })
	return db, server.Handler()
}

func testJSONRequest(t *testing.T, handler http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func responseData(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body response
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	data, ok := body.Data.(map[string]any)
	if !ok {
		t.Fatalf("response data type = %T, want object", body.Data)
	}
	return data
}

func testAdminToken(t *testing.T, handler http.Handler) string {
	t.Helper()
	recorder := testJSONRequest(t, handler, http.MethodPost, "/api/auth/login", "", `{"username":"admin","password":"admin123"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	data := responseData(t, recorder)
	token, ok := data["token"].(string)
	if !ok || token == "" {
		t.Fatalf("login token = %#v", data["token"])
	}
	return token
}

func TestZeroByteUploadCompletesAndIsListed(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	init := testJSONRequest(t, handler, http.MethodPost, "/api/files/upload-init", token, `{"name":"empty.bin","size":0,"chunkSize":0}`)
	if init.Code != http.StatusOK {
		t.Fatalf("empty upload init status = %d, want 200: %s", init.Code, init.Body.String())
	}
	initData := responseData(t, init)
	if initData["totalChunks"] != float64(1) || initData["chunkSize"] != float64(1) {
		t.Fatalf("empty upload init data = %#v, want one chunk of size one", initData)
	}
	taskID, ok := initData["taskId"].(string)
	if !ok || taskID == "" {
		t.Fatalf("empty upload task ID = %#v", initData["taskId"])
	}
	chunk := testJSONRequest(t, handler, http.MethodPut, "/api/files/"+taskID+"/chunks/0", token, "")
	if chunk.Code != http.StatusOK {
		t.Fatalf("empty upload chunk status = %d, want 200: %s", chunk.Code, chunk.Body.String())
	}
	complete := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+taskID+"/complete", token, "{}")
	if complete.Code != http.StatusOK {
		t.Fatalf("empty upload complete status = %d, want 200: %s", complete.Code, complete.Body.String())
	}
	fileData := responseData(t, complete)
	if fileData["md5"] != "d41d8cd98f00b204e9800998ecf8427e" || fileData["sha256"] != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("empty upload hashes = %#v", fileData)
	}
	list := testJSONRequest(t, handler, http.MethodGet, "/api/files", token, "")
	if list.Code != http.StatusOK {
		t.Fatalf("empty upload list status = %d, want 200: %s", list.Code, list.Body.String())
	}
	items := responseData(t, list)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("empty upload list length = %d, want 1", len(items))
	}
	file, err := db.FindFile(context.Background(), int64(fileData["id"].(float64)))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(db.DataDir, file.StoragePath))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("stored empty file size = %d, want 0", info.Size())
	}
}

func TestPartialSettingsUpdatePreservesOtherValues(t *testing.T) {
	_, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	update := testJSONRequest(t, handler, http.MethodPut, "/api/admin/settings", token, `{"ipLockThreshold":3}`)
	if update.Code != http.StatusOK {
		t.Fatalf("partial settings update status = %d, want 200: %s", update.Code, update.Body.String())
	}
	settings := responseData(t, update)
	if settings["ipLockThreshold"] != float64(3) || settings["passwordMinLength"] != float64(8) || settings["passwordComplexity"] != float64(3) {
		t.Fatalf("partial settings response = %#v", settings)
	}
	invalid := testJSONRequest(t, handler, http.MethodPut, "/api/admin/settings", token, `{"passwordMinLength":0}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid settings status = %d, want 400: %s", invalid.Code, invalid.Body.String())
	}
	var body response
	if err := json.Unmarshal(invalid.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Message != "密码最小长度无效" {
		t.Fatalf("invalid settings message = %q, want field-specific message", body.Message)
	}
}
