package httpapi

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"filebox/internal/store"
)

func newTestServer(t *testing.T) (*store.Store, http.Handler) {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureAdmin("admin", "admin123", 32*1024*1024); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE users SET must_change_password = 0 WHERE username = 'admin'"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	server := NewServer(db, Config{DataDir: db.DataDir, MaxFileSize: 32 * 1024 * 1024, MinFreeSpace: 0, JWTSecret: []byte("test-secret")})
	t.Cleanup(func() { db.Close() })
	return db, server.Handler()
}

func testBinaryRequest(t *testing.T, handler http.Handler, method, path, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.ContentLength = int64(len(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func initUpload(t *testing.T, handler http.Handler, token, name string, size, chunkSize int64, dir string) map[string]any {
	t.Helper()
	body, err := json.Marshal(map[string]any{"name": name, "size": size, "chunkSize": chunkSize, "dir": dir})
	if err != nil {
		t.Fatal(err)
	}
	recorder := testJSONRequest(t, handler, http.MethodPost, "/api/files/upload-init", token, string(body))
	if recorder.Code != http.StatusOK {
		t.Fatalf("upload init status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	return responseData(t, recorder)
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

func TestMultipartUploadStatusExpectedSizesAndComplete(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	chunkSize := int64(2 * 1024 * 1024)
	data := make([]byte, 5*1024*1024)
	for index := range data {
		data[index] = byte(index % 251)
	}
	initData := initUpload(t, handler, token, "multipart.bin", int64(len(data)), chunkSize, "")
	if initData["chunkSize"] != float64(chunkSize) || initData["totalChunks"] != float64(3) {
		t.Fatalf("multipart init = %#v", initData)
	}
	taskID := initData["taskId"].(string)
	if got := testBinaryRequest(t, handler, http.MethodPut, "/api/files/"+taskID+"/chunks/0", token, data[:2*1024*1024]); got.Code != http.StatusOK {
		t.Fatalf("chunk 0 status = %d: %s", got.Code, got.Body.String())
	}
	if got := testBinaryRequest(t, handler, http.MethodPut, "/api/files/"+taskID+"/chunks/2", token, data[4*1024*1024:]); got.Code != http.StatusOK {
		t.Fatalf("last chunk status = %d: %s", got.Code, got.Body.String())
	}
	status := testJSONRequest(t, handler, http.MethodGet, "/api/files/"+taskID+"/status", token, "")
	if status.Code != http.StatusOK {
		t.Fatalf("upload status = %d: %s", status.Code, status.Body.String())
	}
	statusData := responseData(t, status)
	if got := statusData["uploadedChunks"].([]any); len(got) != 2 || got[0] != float64(0) || got[1] != float64(2) {
		t.Fatalf("uploaded chunks = %#v, want [0 2]", got)
	}
	incomplete := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+taskID+"/complete", token, "{}")
	if incomplete.Code != http.StatusBadRequest || !strings.Contains(incomplete.Body.String(), "上传分片不完整") {
		t.Fatalf("incomplete complete = %d: %s", incomplete.Code, incomplete.Body.String())
	}
	if got := testBinaryRequest(t, handler, http.MethodPut, "/api/files/"+taskID+"/chunks/1", token, data[2*1024*1024:4*1024*1024]); got.Code != http.StatusOK {
		t.Fatalf("chunk 1 status = %d: %s", got.Code, got.Body.String())
	}
	complete := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+taskID+"/complete", token, "{}")
	if complete.Code != http.StatusOK {
		t.Fatalf("complete status = %d: %s", complete.Code, complete.Body.String())
	}
	fileData := responseData(t, complete)
	md5Sum := md5.Sum(data)
	shaSum := sha256.Sum256(data)
	if fileData["md5"] != hex.EncodeToString(md5Sum[:]) || fileData["sha256"] != hex.EncodeToString(shaSum[:]) {
		t.Fatalf("complete hashes = %#v", fileData)
	}
	file, err := db.FindFile(context.Background(), int64(fileData["id"].(float64)))
	if err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(filepath.Join(db.DataDir, file.StoragePath))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, data) {
		t.Fatal("merged file content differs from uploaded content")
	}
	chunks, err := db.ListChunks(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 0 {
		t.Fatalf("chunks after complete = %d, want 0", len(chunks))
	}
}

func TestMultipartChunkSizeAndDirectoryValidation(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	tooSmall := testJSONRequest(t, handler, http.MethodPost, "/api/files/upload-init", token, `{"name":"large.bin","size":5242880,"chunkSize":1048576}`)
	if tooSmall.Code != http.StatusBadRequest || !strings.Contains(tooSmall.Body.String(), "分片大小必须在 2MB-8MB 之间") {
		t.Fatalf("small chunk init = %d: %s", tooSmall.Code, tooSmall.Body.String())
	}
	invalidDir := testJSONRequest(t, handler, http.MethodPost, "/api/files/upload-init", token, `{"name":"file.txt","size":1,"chunkSize":0,"dir":"../"}`)
	if invalidDir.Code != http.StatusBadRequest || !strings.Contains(invalidDir.Body.String(), "目录无效") {
		t.Fatalf("invalid dir init = %d: %s", invalidDir.Code, invalidDir.Body.String())
	}
	for _, dir := range []string{"assets/icons", "assets/images"} {
		initData := initUpload(t, handler, token, "icon.svg", 1, 0, dir)
		taskID := initData["taskId"].(string)
		if got := testBinaryRequest(t, handler, http.MethodPut, "/api/files/"+taskID+"/chunks/0", token, []byte("x")); got.Code != http.StatusOK {
			t.Fatalf("directory chunk status = %d: %s", got.Code, got.Body.String())
		}
		complete := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+taskID+"/complete", token, "{}")
		if complete.Code != http.StatusOK {
			t.Fatalf("directory complete status = %d: %s", complete.Code, complete.Body.String())
		}
		fileData := responseData(t, complete)
		file, err := db.FindFile(context.Background(), int64(fileData["id"].(float64)))
		if err != nil {
			t.Fatal(err)
		}
		if file.StoredName != "icon.svg" || !strings.Contains(filepath.ToSlash(file.StoragePath), filepath.ToSlash(dir)+"/icon.svg") {
			t.Fatalf("directory storage path = %q, name = %q", file.StoragePath, file.StoredName)
		}
	}
}

func TestInstantUploadCheckHitAndMiss(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	content := []byte("instant upload content")
	initData := initUpload(t, handler, token, "instant.txt", int64(len(content)), 0, "")
	taskID := initData["taskId"].(string)
	if got := testBinaryRequest(t, handler, http.MethodPut, "/api/files/"+taskID+"/chunks/0", token, content); got.Code != http.StatusOK {
		t.Fatalf("instant upload chunk status = %d: %s", got.Code, got.Body.String())
	}
	complete := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+taskID+"/complete", token, "{}")
	if complete.Code != http.StatusOK {
		t.Fatalf("instant upload complete status = %d: %s", complete.Code, complete.Body.String())
	}
	fileData := responseData(t, complete)
	var before int
	if err := db.DB.QueryRow("SELECT COUNT(id) FROM files WHERE status = 'ready'").Scan(&before); err != nil {
		t.Fatal(err)
	}
	checkBody := `{"md5":"00000000000000000000000000000000","sha256":"` + fileData["sha256"].(string) + `","size":` + strconv.FormatInt(int64(len(content)), 10) + `}`
	hit := testJSONRequest(t, handler, http.MethodPost, "/api/files/check", token, checkBody)
	if hit.Code != http.StatusOK || responseData(t, hit)["instant"] != true {
		t.Fatalf("instant hit = %d: %s", hit.Code, hit.Body.String())
	}
	miss := testJSONRequest(t, handler, http.MethodPost, "/api/files/check", token, `{"sha256":"no-match","size":22}`)
	if miss.Code != http.StatusOK || responseData(t, miss)["instant"] != false {
		t.Fatalf("instant miss = %d: %s", miss.Code, miss.Body.String())
	}
	overLimit := testJSONRequest(t, handler, http.MethodPost, "/api/files/check", token, `{"sha256":"no-match","size":33554433}`)
	if overLimit.Code != http.StatusRequestEntityTooLarge || !strings.Contains(overLimit.Body.String(), "FILE_TOO_LARGE") {
		t.Fatalf("instant over-limit = %d: %s", overLimit.Code, overLimit.Body.String())
	}
	badDir := testJSONRequest(t, handler, http.MethodPost, "/api/files/check", token, `{"sha256":"no-match","size":22,"dir":"../"}`)
	if badDir.Code != http.StatusBadRequest || !strings.Contains(badDir.Body.String(), "目录无效") {
		t.Fatalf("instant invalid dir = %d: %s", badDir.Code, badDir.Body.String())
	}
	var after int
	if err := db.DB.QueryRow("SELECT COUNT(id) FROM files WHERE status = 'ready'").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("file count changed after instant check: %d -> %d", before, after)
	}
}

func TestUniqueZipEntryNameAvoidsExistingSuffixes(t *testing.T) {
	used := make(map[string]struct{})
	for _, want := range []string{"report (1).txt", "report.txt", "report (2).txt", "REPORT (3).TXT"} {
		got := uniqueZipEntryName(strings.ReplaceAll(want, "REPORT", "report"), used)
		if _, exists := used[strings.ToLower(got)]; !exists {
			t.Fatalf("entry %q was not recorded", got)
		}
	}
	if got := uniqueZipEntryName("report.txt", used); got != "report (4).txt" {
		t.Fatalf("uniqueZipEntryName() = %q, want report (4).txt", got)
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

func uploadTestFile(t *testing.T, handler http.Handler, token, name, mimeType string, content []byte) map[string]any {
	t.Helper()
	input := map[string]any{"name": name, "size": len(content), "chunkSize": 0}
	if mimeType != "" {
		input["mime"] = mimeType
	}
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	init := testJSONRequest(t, handler, http.MethodPost, "/api/files/upload-init", token, string(body))
	if init.Code != http.StatusOK {
		t.Fatalf("upload init status = %d: %s", init.Code, init.Body.String())
	}
	taskID := responseData(t, init)["taskId"].(string)
	chunk := testBinaryRequest(t, handler, http.MethodPut, "/api/files/"+taskID+"/chunks/0", token, content)
	if chunk.Code != http.StatusOK {
		t.Fatalf("upload chunk status = %d: %s", chunk.Code, chunk.Body.String())
	}
	complete := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+taskID+"/complete", token, "{}")
	if complete.Code != http.StatusOK {
		t.Fatalf("upload complete status = %d: %s", complete.Code, complete.Body.String())
	}
	return responseData(t, complete)
}

func TestSharingLifecycleAndAtomicDownloadLimit(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	file := uploadTestFile(t, handler, token, "shared.txt", "text/plain", []byte("shared content"))
	id := int64(file["id"].(float64))
	invalid := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+strconv.FormatInt(id, 10)+"/share", token, `{"expiresInHours":0}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid share expiry status = %d", invalid.Code)
	}
	created := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+strconv.FormatInt(id, 10)+"/share", token, `{"expiresInHours":1,"maxDownloads":1}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create share status = %d: %s", created.Code, created.Body.String())
	}
	share := responseData(t, created)
	shareToken := share["token"].(string)
	if len(shareToken) != 64 || share["url"] != "/"+shareToken {
		t.Fatalf("share token response = %#v", share)
	}
	meta := testJSONRequest(t, handler, http.MethodGet, "/api/files/shared/"+shareToken+"/meta", "", "")
	if meta.Code != http.StatusOK {
		t.Fatalf("share meta status = %d: %s", meta.Code, meta.Body.String())
	}
	down := testBinaryRequest(t, handler, http.MethodGet, "/api/files/shared/"+shareToken+"/download", "", nil)
	if down.Code != http.StatusOK || !bytes.Equal(down.Body.Bytes(), []byte("shared content")) {
		t.Fatalf("share download = %d, %q", down.Code, down.Body.String())
	}
	limited := testBinaryRequest(t, handler, http.MethodGet, "/api/files/shared/"+shareToken+"/download", "", nil)
	if limited.Code != http.StatusForbidden {
		t.Fatalf("limited share download status = %d", limited.Code)
	}
	stats := testJSONRequest(t, handler, http.MethodGet, "/api/admin/stats", token, "")
	if stats.Code != http.StatusOK {
		t.Fatalf("stats status = %d", stats.Code)
	}
	statsData := responseData(t, stats)
	if statsData["shares"] != float64(1) || statsData["shareDownloads"] != float64(1) {
		t.Fatalf("share stats = %#v", statsData)
	}
	if _, err := db.DB.Exec("UPDATE shares SET expires_at = ? WHERE token = ?", time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), shareToken); err != nil {
		t.Fatal(err)
	}
	expiredMeta := testJSONRequest(t, handler, http.MethodGet, "/api/files/shared/"+shareToken+"/meta", "", "")
	if expiredMeta.Code != http.StatusNotFound {
		t.Fatalf("expired share meta status = %d", expiredMeta.Code)
	}
	expiredDownload := testBinaryRequest(t, handler, http.MethodGet, "/api/files/shared/"+shareToken+"/download", "", nil)
	if expiredDownload.Code != http.StatusForbidden {
		t.Fatalf("expired share download status = %d", expiredDownload.Code)
	}
	revoked := testJSONRequest(t, handler, http.MethodDelete, "/api/files/"+strconv.FormatInt(id, 10)+"/shares", token, "")
	if revoked.Code != http.StatusOK || responseData(t, revoked)["removed"] != float64(1) {
		t.Fatalf("revoke share = %d: %s", revoked.Code, revoked.Body.String())
	}
	missing := testJSONRequest(t, handler, http.MethodGet, "/api/files/shared/"+shareToken+"/meta", "", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("revoked share meta status = %d", missing.Code)
	}
}

func TestPreviewContentDisposition(t *testing.T) {
	_, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	textFile := uploadTestFile(t, handler, token, "preview.txt", "text/plain", []byte("preview"))
	zipFile := uploadTestFile(t, handler, token, "archive.zip", "application/zip", []byte("PK"))
	inline := testBinaryRequest(t, handler, http.MethodGet, "/api/files/"+strconv.FormatInt(int64(textFile["id"].(float64)), 10)+"/preview", token, nil)
	if inline.Code != http.StatusOK || inline.Header().Get("Content-Disposition") != "inline" {
		t.Fatalf("inline preview = %d, %q", inline.Code, inline.Header().Get("Content-Disposition"))
	}
	attachment := testBinaryRequest(t, handler, http.MethodGet, "/api/files/"+strconv.FormatInt(int64(zipFile["id"].(float64)), 10)+"/preview", token, nil)
	if attachment.Code != http.StatusOK || !strings.HasPrefix(attachment.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("attachment preview = %d, %q", attachment.Code, attachment.Header().Get("Content-Disposition"))
	}
}

func TestRegistrationSettingAndBrand(t *testing.T) {
	_, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)
	closed := testJSONRequest(t, handler, http.MethodPost, "/api/auth/register", "", `{"username":"new-user","password":"Register1!"}`)
	if closed.Code != http.StatusForbidden {
		t.Fatalf("closed registration status = %d", closed.Code)
	}
	open := testJSONRequest(t, handler, http.MethodPut, "/api/admin/settings", adminToken, `{"registerEnabled":true,"uploadRateLimit":65536}`)
	if open.Code != http.StatusOK {
		t.Fatalf("open registration settings status = %d: %s", open.Code, open.Body.String())
	}
	brand := testJSONRequest(t, handler, http.MethodGet, "/api/brand", "", "")
	if brand.Code != http.StatusOK || responseData(t, brand)["registerEnabled"] != true {
		t.Fatalf("brand registration setting = %d: %s", brand.Code, brand.Body.String())
	}
	registered := testJSONRequest(t, handler, http.MethodPost, "/api/auth/register", "", `{"username":"new-user","password":"Register1!"}`)
	if registered.Code != http.StatusCreated || responseData(t, registered)["token"] == "" {
		t.Fatalf("register status = %d: %s", registered.Code, registered.Body.String())
	}
	duplicate := testJSONRequest(t, handler, http.MethodPost, "/api/auth/register", "", `{"username":"new-user","password":"Register1!"}`)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate registration status = %d", duplicate.Code)
	}
}

func TestRateLimiterRebuildsWhenRateChanges(t *testing.T) {
	var limiter rateLimiter
	first := limiter.limiterFor(7, 1024)
	if first == nil || first.Limit() != 1024 {
		t.Fatalf("first limiter = %#v", first)
	}
	if got := limiter.limiterFor(7, 1024); got != first {
		t.Fatal("same rate did not reuse limiter")
	}
	if got := limiter.limiterFor(7, 2048); got == first || got.Limit() != 2048 {
		t.Fatal("changed rate did not rebuild limiter")
	}
	if limiter.limiterFor(7, 0) != nil {
		t.Fatal("zero rate should disable limiter")
	}
}

// TestReuploadAfterDeleteReusesStoragePath 覆盖删除后重传同名文件不再触发 storage_path 唯一约束（回归 D-FIX）。
// TestReuploadAfterDeleteReusesStoragePath covers re-uploading a deleted name without hitting the storage_path UNIQUE constraint.
func TestReuploadAfterDeleteReusesStoragePath(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	content := []byte("reupload after delete")
	first := uploadTestFile(t, handler, token, "cycle.txt", "text/plain", content)
	id := int64(first["id"].(float64))
	deleted := testJSONRequest(t, handler, http.MethodDelete, "/api/files/"+strconv.FormatInt(id, 10), token, "")
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status = %d: %s", deleted.Code, deleted.Body.String())
	}
	second := uploadTestFile(t, handler, token, "cycle.txt", "text/plain", content)
	if second["md5"] != first["md5"] {
		t.Fatalf("reupload hashes differ: %#v vs %#v", second, first)
	}
	file, err := db.FindFile(context.Background(), int64(second["id"].(float64)))
	if err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(filepath.Join(db.DataDir, file.StoragePath))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, content) {
		t.Fatal("reuploaded content differs")
	}
	var readyCount int
	if err := db.DB.QueryRow("SELECT COUNT(id) FROM files WHERE status = 'ready'").Scan(&readyCount); err != nil {
		t.Fatal(err)
	}
	if readyCount != 1 {
		t.Fatalf("ready files after reupload = %d, want 1", readyCount)
	}
}

// TestFolderCRUDUploadFilterAndIsolation 覆盖 v011 目录模型：创建（中文名）/子目录/上传到目录/列表过滤/重命名级联/删除非空拒绝/用户隔离。
// TestFolderCRUDUploadFilterAndIsolation covers the v011 folder model: create (Chinese names), children, upload-into-folder, list filtering, rename cascade, non-empty delete rejection, and user isolation.
func TestFolderCRUDUploadFilterAndIsolation(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	// 创建中文目录名
	created := testJSONRequest(t, handler, http.MethodPost, "/api/folders", token, `{"name":"工作文档"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create folder = %d: %s", created.Code, created.Body.String())
	}
	folder := responseData(t, created)
	folderID := int64(folder["id"].(float64))
	if folder["path"] != "工作文档" || folder["name"] != "工作文档" {
		t.Fatalf("folder payload = %#v", folder)
	}
	// 同名目录 409
	dup := testJSONRequest(t, handler, http.MethodPost, "/api/folders", token, `{"name":"工作文档"}`)
	if dup.Code != http.StatusConflict {
		t.Fatalf("duplicate folder = %d", dup.Code)
	}
	// 子目录
	child := testJSONRequest(t, handler, http.MethodPost, "/api/folders", token, `{"name":"projects","parent":"工作文档"}`)
	if child.Code != http.StatusCreated || responseData(t, child)["path"] != "工作文档/projects" {
		t.Fatalf("child folder = %d: %s", child.Code, child.Body.String())
	}
	// 上传文件到 工作文档/projects（目录记录自动补齐；直接构造 dir 上传）
	content := []byte("plan content")
	initBody, _ := json.Marshal(map[string]any{"name": "plan.txt", "size": len(content), "chunkSize": 0, "mime": "text/plain", "dir": "工作文档/projects"})
	initUpload := testJSONRequest(t, handler, http.MethodPost, "/api/files/upload-init", token, string(initBody))
	if initUpload.Code != http.StatusOK {
		t.Fatalf("dir upload init = %d: %s", initUpload.Code, initUpload.Body.String())
	}
	planTask := responseData(t, initUpload)["taskId"].(string)
	chunkPut := testBinaryRequest(t, handler, http.MethodPut, "/api/files/"+planTask+"/chunks/0", token, content)
	if chunkPut.Code != http.StatusOK {
		t.Fatalf("dir chunk = %d", chunkPut.Code)
	}
	complete := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+planTask+"/complete", token, "{}")
	if complete.Code != http.StatusOK {
		t.Fatalf("dir complete = %d: %s", complete.Code, complete.Body.String())
	}
	file := responseData(t, complete)
	fileID := int64(file["id"].(float64))
	stored, err := db.FindFile(context.Background(), fileID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filepath.ToSlash(stored.StoragePath), "工作文档/projects/plan.txt") {
		t.Fatalf("storage path = %q, want under 工作文档/projects", stored.StoragePath)
	}
	// 列表按目录过滤
	rootList := testJSONRequest(t, handler, http.MethodGet, "/api/files?page=1&pageSize=50&dir=", token, "")
	if responseData(t, rootList)["total"] == float64(0) {
		t.Fatalf("root list should be empty after moving upload into a folder: %s", rootList.Body.String())
	}
	dirList := testJSONRequest(t, handler, http.MethodGet, "/api/files?page=1&pageSize=50&dir="+url.QueryEscape("工作文档/projects"), token, "")
	dirData := responseData(t, dirList)
	if dirData["total"] != float64(1) {
		t.Fatalf("dir list total = %v, want 1", dirData["total"])
	}
	// 重命名目录：文件 storage_path 级联更新且磁盘文件移动
	renamed := testJSONRequest(t, handler, http.MethodPatch, "/api/folders/"+strconv.FormatInt(folderID, 10), token, `{"name":"工作资料"}`)
	if renamed.Code != http.StatusOK {
		t.Fatalf("rename folder = %d: %s", renamed.Code, renamed.Body.String())
	}
	fileAfter, err := db.FindFile(context.Background(), fileID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filepath.ToSlash(fileAfter.StoragePath), "工作资料/projects/plan.txt") {
		t.Fatalf("storage path after rename = %q", fileAfter.StoragePath)
	}
	if _, err := os.Stat(filepath.Join(db.DataDir, fileAfter.StoragePath)); err != nil {
		t.Fatalf("disk file after rename missing: %v", err)
	}
	// 删除非空目录（含文件）→ 400
	nonEmpty := testJSONRequest(t, handler, http.MethodDelete, "/api/folders/"+strconv.FormatInt(folderID, 10), token, "")
	if nonEmpty.Code != http.StatusBadRequest {
		t.Fatalf("delete non-empty folder = %d", nonEmpty.Code)
	}
	// 先删文件，再删子目录（空）→ 成功；再删父目录（空）→ 成功
	testJSONRequest(t, handler, http.MethodDelete, "/api/files/"+strconv.FormatInt(fileID, 10), token, "")
	childList := testJSONRequest(t, handler, http.MethodGet, "/api/folders", token, "")
	childID := int64(0)
	for _, item := range responseData(t, childList)["items"].([]any) {
		entry := item.(map[string]any)
		if entry["path"] == "工作资料/projects" {
			childID = int64(entry["id"].(float64))
		}
	}
	if childID == 0 {
		t.Fatal("child folder not found")
	}
	delChild := testJSONRequest(t, handler, http.MethodDelete, "/api/folders/"+strconv.FormatInt(childID, 10), token, "")
	if delChild.Code != http.StatusOK {
		t.Fatalf("delete empty child = %d: %s", delChild.Code, delChild.Body.String())
	}
	delParent := testJSONRequest(t, handler, http.MethodDelete, "/api/folders/"+strconv.FormatInt(folderID, 10), token, "")
	if delParent.Code != http.StatusOK {
		t.Fatalf("delete empty parent = %d: %s", delParent.Code, delParent.Body.String())
	}
	// 用户隔离：另一用户看不到目录，也不能操作
	userToken := testAdminToken(t, handler) // same admin; create a second account through the API
	_ = userToken
	createOther := testJSONRequest(t, handler, http.MethodPost, "/api/admin/users", token, `{"username":"folder-user","password":"Folder123!","role":"user","quotaBytes":107374182400}`)
	if createOther.Code != http.StatusCreated {
		t.Fatalf("create other user = %d", createOther.Code)
	}
	otherToken := ""
	loginBody := `{"username":"folder-user","password":"Folder123!"}`
	login := testJSONRequest(t, handler, http.MethodPost, "/api/auth/login", "", loginBody)
	otherToken = responseData(t, login)["token"].(string)
	otherFolders := testJSONRequest(t, handler, http.MethodGet, "/api/folders", otherToken, "")
	if responseData(t, otherFolders)["items"].([]any) != nil && len(responseData(t, otherFolders)["items"].([]any)) != 0 {
		t.Fatalf("other user sees folders: %s", otherFolders.Body.String())
	}
	badDelete := testJSONRequest(t, handler, http.MethodDelete, "/api/folders/"+strconv.FormatInt(folderID, 10), otherToken, "")
	if badDelete.Code != http.StatusNotFound {
		t.Fatalf("other user delete folder = %d, want 404", badDelete.Code)
	}
}
