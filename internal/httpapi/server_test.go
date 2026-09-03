package httpapi

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"filebox/internal/store"
)

func newTestServer(t *testing.T) (*store.Store, http.Handler) {
	return newTestServerWithMinFreeSpace(t, 0)
}

func newTestServerWithMinFreeSpace(t *testing.T, minFreeSpace int64) (*store.Store, http.Handler) {
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
	server := NewServer(db, Config{DataDir: db.DataDir, MaxFileSize: 32 * 1024 * 1024, MinFreeSpace: minFreeSpace, JWTSecret: []byte("test-secret")})
	t.Cleanup(func() { db.Close() })
	return db, server.Handler()
}

func TestUploadCollectionLifecycleAndAnonymousUpload(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	created := testJSONRequest(t, handler, http.MethodPost, "/api/collections", token, `{"name":"外部收集","expiresInHours":24,"maxUploads":2,"maxFileBytes":1024}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create collection status = %d: %s", created.Code, created.Body.String())
	}
	collection := responseData(t, created)
	collectionID := int64(collection["id"].(float64))
	collectionToken := collection["token"].(string)
	if !strings.HasPrefix(collection["url"].(string), "/u/") {
		t.Fatalf("collection url = %#v", collection["url"])
	}
	meta := testJSONRequest(t, handler, http.MethodGet, "/api/collections/"+collectionToken+"/meta", "", "")
	if meta.Code != http.StatusOK || responseData(t, meta)["uploadAllowed"] != true {
		t.Fatalf("collection meta = %d: %s", meta.Code, meta.Body.String())
	}
	content := []byte("hello")
	init := testJSONRequest(t, handler, http.MethodPost, "/api/collections/"+collectionToken+"/upload-init", "", `{"name":"received.txt","size":5,"chunkSize":0,"remark":"来自访客"}`)
	if init.Code != http.StatusOK {
		t.Fatalf("collection init status = %d: %s", init.Code, init.Body.String())
	}
	taskID := responseData(t, init)["taskId"].(string)
	chunk := testBinaryRequest(t, handler, http.MethodPut, "/api/collections/"+collectionToken+"/upload-chunk/"+taskID+"/0", "", content)
	if chunk.Code != http.StatusOK {
		t.Fatalf("collection chunk status = %d: %s", chunk.Code, chunk.Body.String())
	}
	complete := testJSONRequest(t, handler, http.MethodPost, "/api/collections/"+collectionToken+"/upload-complete/"+taskID, "", `{}`)
	if complete.Code != http.StatusOK {
		t.Fatalf("collection complete status = %d: %s", complete.Code, complete.Body.String())
	}
	fileID := int64(responseData(t, complete)["id"].(float64))
	files := testJSONRequest(t, handler, http.MethodGet, "/api/collections/"+strconv.FormatInt(collectionID, 10)+"/files", token, "")
	if files.Code != http.StatusOK {
		t.Fatalf("collection files status = %d: %s", files.Code, files.Body.String())
	}
	items := responseData(t, files)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["remark"] != "来自访客" || int64(items[0].(map[string]any)["fileId"].(float64)) != fileID {
		t.Fatalf("collection files = %#v", items)
	}
	ownerFiles := testJSONRequest(t, handler, http.MethodGet, "/api/files?dir="+url.QueryEscape("uploads/"+collectionToken), token, "")
	if ownerFiles.Code != http.StatusOK || responseData(t, ownerFiles)["total"] != float64(1) {
		t.Fatalf("owner collection directory = %d: %s", ownerFiles.Code, ownerFiles.Body.String())
	}
	secondInit := testJSONRequest(t, handler, http.MethodPost, "/api/collections/"+collectionToken+"/upload-init", "", `{"name":"second.txt","size":1,"chunkSize":0}`)
	if secondInit.Code != http.StatusOK {
		t.Fatalf("second collection init status = %d: %s", secondInit.Code, secondInit.Body.String())
	}
	secondTaskID := responseData(t, secondInit)["taskId"].(string)
	secondChunk := testBinaryRequest(t, handler, http.MethodPut, "/api/collections/"+collectionToken+"/upload-chunk/"+secondTaskID+"/0", "", []byte("x"))
	if secondChunk.Code != http.StatusOK {
		t.Fatalf("second collection chunk status = %d: %s", secondChunk.Code, secondChunk.Body.String())
	}
	secondComplete := testJSONRequest(t, handler, http.MethodPost, "/api/collections/"+collectionToken+"/upload-complete/"+secondTaskID, "", `{}`)
	if secondComplete.Code != http.StatusOK {
		t.Fatalf("second collection complete status = %d: %s", secondComplete.Code, secondComplete.Body.String())
	}
	var uploadCount int
	if err := db.DB.QueryRow("SELECT upload_count FROM upload_collections WHERE id = ?", collectionID).Scan(&uploadCount); err != nil || uploadCount != 2 {
		t.Fatalf("collection upload_count = %d, %v; want 2", uploadCount, err)
	}
	thirdInit := testJSONRequest(t, handler, http.MethodPost, "/api/collections/"+collectionToken+"/upload-init", "", `{"name":"third.txt","size":1,"chunkSize":0}`)
	if thirdInit.Code != http.StatusForbidden {
		t.Fatalf("collection limit status = %d: %s", thirdInit.Code, thirdInit.Body.String())
	}
	var limitBody response
	if err := json.Unmarshal(thirdInit.Body.Bytes(), &limitBody); err != nil || limitBody.Data.(map[string]any)["code"] != "COLLECTION_LIMIT" {
		t.Fatalf("collection limit body = %s", thirdInit.Body.String())
	}
	revoked := testJSONRequest(t, handler, http.MethodDelete, "/api/collections/"+strconv.FormatInt(collectionID, 10), token, "")
	if revoked.Code != http.StatusOK {
		t.Fatalf("revoke collection = %d: %s", revoked.Code, revoked.Body.String())
	}
	revokedMeta := testJSONRequest(t, handler, http.MethodGet, "/api/collections/"+collectionToken+"/meta", "", "")
	if revokedMeta.Code != http.StatusOK || responseData(t, revokedMeta)["status"] != "revoked" {
		t.Fatalf("revoked collection meta = %d: %s", revokedMeta.Code, revokedMeta.Body.String())
	}
	blocked := testJSONRequest(t, handler, http.MethodPost, "/api/collections/"+collectionToken+"/upload-init", "", `{"name":"blocked.txt","size":1,"chunkSize":0}`)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("revoked collection init = %d: %s", blocked.Code, blocked.Body.String())
	}
	var auditCount int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE action = 'upload_collect' AND reason = 'collection_upload'").Scan(&auditCount); err != nil || auditCount != 2 {
		t.Fatalf("collection success audit count = %d, %v", auditCount, err)
	}
}

// TestCollectionUploadInitDoesNotConsumeSlots verifies empty initializations do not occupy collection slots.
// TestCollectionUploadInitDoesNotConsumeSlots 验证空 init 不占用收集箱槽位。
func TestCollectionUploadInitDoesNotConsumeSlots(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	created := testJSONRequest(t, handler, http.MethodPost, "/api/collections", token, `{"name":"init-only","expiresInHours":24,"maxUploads":2}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create collection status = %d: %s", created.Code, created.Body.String())
	}
	collection := responseData(t, created)
	collectionToken := collection["token"].(string)
	for i := 0; i < 3; i++ {
		init := testJSONRequest(t, handler, http.MethodPost, "/api/collections/"+collectionToken+"/upload-init", "", `{"name":"empty-`+strconv.Itoa(i)+`.txt","size":1,"chunkSize":0}`)
		if init.Code != http.StatusOK {
			t.Fatalf("empty collection init %d status = %d: %s", i+1, init.Code, init.Body.String())
		}
	}
	var uploadCount int
	if err := db.DB.QueryRow("SELECT upload_count FROM upload_collections WHERE token = ?", collectionToken).Scan(&uploadCount); err != nil || uploadCount != 0 {
		t.Fatalf("empty init upload_count = %d, %v; want 0", uploadCount, err)
	}
}

func TestCollectionUploadRejectsWhenDiskFull(t *testing.T) {
	_, handler := newTestServerWithMinFreeSpace(t, 100)
	adminToken := testAdminToken(t, handler)
	previousDiskUsage := diskUsageFunc
	t.Cleanup(func() { diskUsageFunc = previousDiskUsage })
	diskUsageFunc = func(string) (int64, int64, int64, error) {
		return 1000, 10, 990, nil
	}
	created := testJSONRequest(t, handler, http.MethodPost, "/api/collections", adminToken, `{"name":"disk-check","expiresInHours":1}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create collection = %d: %s", created.Code, created.Body.String())
	}
	collectionToken := responseData(t, created)["token"].(string)
	init := testJSONRequest(t, handler, http.MethodPost, "/api/collections/"+collectionToken+"/upload-init", "", `{"name":"blocked.txt","size":1,"chunkSize":0}`)
	if init.Code != http.StatusServiceUnavailable || responseData(t, init)["code"] != "DISK_FULL" {
		t.Fatalf("collection disk-full init = %d: %s", init.Code, init.Body.String())
	}

	diskUsageFunc = func(string) (int64, int64, int64, error) {
		return 1000, 1000, 0, nil
	}
	allowedInit := testJSONRequest(t, handler, http.MethodPost, "/api/collections/"+collectionToken+"/upload-init", "", `{"name":"blocked.txt","size":1,"chunkSize":0}`)
	if allowedInit.Code != http.StatusOK {
		t.Fatalf("collection init with free space = %d: %s", allowedInit.Code, allowedInit.Body.String())
	}
	taskID := responseData(t, allowedInit)["taskId"].(string)
	diskUsageFunc = func(string) (int64, int64, int64, error) {
		return 1000, 10, 990, nil
	}
	chunk := testBinaryRequest(t, handler, http.MethodPut, "/api/collections/"+collectionToken+"/upload-chunk/"+taskID+"/0", "", []byte("x"))
	if chunk.Code != http.StatusServiceUnavailable || responseData(t, chunk)["code"] != "DISK_FULL" {
		t.Fatalf("collection disk-full chunk = %d: %s", chunk.Code, chunk.Body.String())
	}
}

func TestUploadCollectionLimitsExpiryQuotaAndOwnership(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	tooLarge := testJSONRequest(t, handler, http.MethodPost, "/api/collections", token, `{"name":"small","expiresInHours":24,"maxUploads":0,"maxFileBytes":2}`)
	small := responseData(t, tooLarge)
	largeInit := testJSONRequest(t, handler, http.MethodPost, "/api/collections/"+small["token"].(string)+"/upload-init", "", `{"name":"large.bin","size":3,"chunkSize":0}`)
	if largeInit.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("collection file limit = %d: %s", largeInit.Code, largeInit.Body.String())
	}
	var largeBody response
	if err := json.Unmarshal(largeInit.Body.Bytes(), &largeBody); err != nil || largeBody.Data.(map[string]any)["code"] != "COLLECTION_FILE_TOO_LARGE" {
		t.Fatalf("collection file limit body = %s", largeInit.Body.String())
	}
	expired := testJSONRequest(t, handler, http.MethodPost, "/api/collections", token, `{"name":"expired","expiresInHours":1,"maxUploads":0,"maxFileBytes":0}`)
	expiredData := responseData(t, expired)
	if _, err := db.DB.Exec("UPDATE upload_collections SET expires_at = ? WHERE id = ?", time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), int64(expiredData["id"].(float64))); err != nil {
		t.Fatal(err)
	}
	expiredInit := testJSONRequest(t, handler, http.MethodPost, "/api/collections/"+expiredData["token"].(string)+"/upload-init", "", `{"name":"expired.txt","size":1,"chunkSize":0}`)
	if expiredInit.Code != http.StatusForbidden {
		t.Fatalf("expired collection = %d: %s", expiredInit.Code, expiredInit.Body.String())
	}
	if _, err := db.DB.Exec("UPDATE users SET quota_bytes = 1 WHERE username = 'admin'"); err != nil {
		t.Fatal(err)
	}
	quota := testJSONRequest(t, handler, http.MethodPost, "/api/collections", token, `{"name":"quota","expiresInHours":1,"maxUploads":0,"maxFileBytes":0}`)
	quotaData := responseData(t, quota)
	quotaInit := testJSONRequest(t, handler, http.MethodPost, "/api/collections/"+quotaData["token"].(string)+"/upload-init", "", `{"name":"quota.txt","size":2,"chunkSize":0}`)
	if quotaInit.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("collection quota = %d: %s", quotaInit.Code, quotaInit.Body.String())
	}
	// #9 配额脱敏：匿名收集场景不得泄露 usedBytes/quotaBytes/fileSize 等内部配额明细。
	// #9 quota masking: anonymous collection uploads must not leak usedBytes/quotaBytes/fileSize details.
	var quotaBody response
	if err := json.Unmarshal(quotaInit.Body.Bytes(), &quotaBody); err != nil {
		t.Fatal(err)
	}
	quotaData2, ok := quotaBody.Data.(map[string]any)
	if !ok || quotaData2["code"] != "COLLECTION_QUOTA_EXCEEDED" {
		t.Fatalf("collection quota body = %s", quotaInit.Body.String())
	}
	for _, key := range []string{"usedBytes", "quotaBytes", "fileSize"} {
		if _, exists := quotaData2[key]; exists {
			t.Fatalf("collection quota error leaked %s: %s", key, quotaInit.Body.String())
		}
	}
	other := testJSONRequest(t, handler, http.MethodPost, "/api/admin/users", token, `{"username":"other-collection-user","password":"Other123!","role":"user","quotaBytes":1024}`)
	if other.Code != http.StatusCreated {
		t.Fatalf("create other user = %d: %s", other.Code, other.Body.String())
	}
	otherLogin := testJSONRequest(t, handler, http.MethodPost, "/api/auth/login", "", `{"username":"other-collection-user","password":"Other123!"}`)
	otherToken := responseData(t, otherLogin)["token"].(string)
	collectionID := int64(quotaData["id"].(float64))
	if cross := testJSONRequest(t, handler, http.MethodGet, "/api/collections/"+strconv.FormatInt(collectionID, 10), otherToken, ""); cross.Code != http.StatusNotFound {
		t.Fatalf("cross-owner collection lookup = %d: %s", cross.Code, cross.Body.String())
	}
	if cross := testJSONRequest(t, handler, http.MethodDelete, "/api/collections/"+strconv.FormatInt(collectionID, 10), otherToken, ""); cross.Code != http.StatusNotFound {
		t.Fatalf("cross-owner collection revoke = %d: %s", cross.Code, cross.Body.String())
	}
}

func TestUploadCollectionUpdate(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	created := testJSONRequest(t, handler, http.MethodPost, "/api/collections", token, `{"name":"可编辑","expiresInHours":24,"maxUploads":2,"maxFileBytes":0}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create collection = %d: %s", created.Code, created.Body.String())
	}
	data := responseData(t, created)
	collectionID := int64(data["id"].(float64))
	future := time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339)
	updated := testJSONRequest(t, handler, http.MethodPut, "/api/collections/"+strconv.FormatInt(collectionID, 10), token, `{"name":"已编辑","expiresAt":"`+future+`","maxUploads":5,"maxFileBytes":2048}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update collection = %d: %s", updated.Code, updated.Body.String())
	}
	updatedData := responseData(t, updated)
	if updatedData["name"] != "已编辑" || int(updatedData["maxUploads"].(float64)) != 5 || int64(updatedData["maxFileBytes"].(float64)) != 2048 {
		t.Fatalf("updated collection = %#v", updatedData)
	}
	// 下调 maxUploads 不允许低于当前 uploadCount（先占用 3 次再下调到 2 会被拒绝；0=不限不受限）。
	if _, err := db.DB.Exec("UPDATE upload_collections SET upload_count = 3 WHERE id = ?", collectionID); err != nil {
		t.Fatal(err)
	}
	below := testJSONRequest(t, handler, http.MethodPut, "/api/collections/"+strconv.FormatInt(collectionID, 10), token, `{"name":"x","expiresAt":"`+future+`","maxUploads":2,"maxFileBytes":0}`)
	if below.Code != http.StatusBadRequest {
		t.Fatalf("lower maxUploads below used = %d: %s", below.Code, below.Body.String())
	}
	// 过期时间必须是未来时间。
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	pastUpdate := testJSONRequest(t, handler, http.MethodPut, "/api/collections/"+strconv.FormatInt(collectionID, 10), token, `{"name":"x","expiresAt":"`+past+`","maxUploads":5,"maxFileBytes":0}`)
	if pastUpdate.Code != http.StatusBadRequest {
		t.Fatalf("past expiry update = %d: %s", pastUpdate.Code, pastUpdate.Body.String())
	}
	// 撤销后不可编辑。
	if revoked := testJSONRequest(t, handler, http.MethodDelete, "/api/collections/"+strconv.FormatInt(collectionID, 10), token, ""); revoked.Code != http.StatusOK {
		t.Fatalf("revoke = %d: %s", revoked.Code, revoked.Body.String())
	}
	afterRevoke := testJSONRequest(t, handler, http.MethodPut, "/api/collections/"+strconv.FormatInt(collectionID, 10), token, `{"name":"x","expiresAt":"`+future+`","maxUploads":5,"maxFileBytes":0}`)
	if afterRevoke.Code != http.StatusBadRequest {
		t.Fatalf("edit revoked collection = %d: %s", afterRevoke.Code, afterRevoke.Body.String())
	}
	// 越权编辑按 404 隐藏。
	other := testJSONRequest(t, handler, http.MethodPost, "/api/admin/users", token, `{"username":"edit-other","password":"Other123!","role":"user","quotaBytes":1024}`)
	if other.Code != http.StatusCreated {
		t.Fatalf("create other user = %d: %s", other.Code, other.Body.String())
	}
	otherLogin := testJSONRequest(t, handler, http.MethodPost, "/api/auth/login", "", `{"username":"edit-other","password":"Other123!"}`)
	otherToken := responseData(t, otherLogin)["token"].(string)
	if cross := testJSONRequest(t, handler, http.MethodPut, "/api/collections/"+strconv.FormatInt(collectionID, 10), otherToken, `{"name":"x","expiresAt":"`+future+`","maxUploads":5,"maxFileBytes":0}`); cross.Code != http.StatusNotFound {
		t.Fatalf("cross-owner update = %d: %s", cross.Code, cross.Body.String())
	}
	// collection_update 审计动作已记录。
	var auditCount int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE action = 'collection_update' AND result = 'success'").Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("collection_update audit = %d, %v", auditCount, err)
	}
}

func TestBatchShareGroupLifecycle(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	// 上传两个小文件作为聚合分享成员。
	fileIDs := make([]int64, 0, 2)
	for index, name := range []string{"a.txt", "b.txt"} {
		initData := initUpload(t, handler, token, name, 1, 1, "")
		taskID := initData["taskId"].(string)
		if chunk := testJSONRequest(t, handler, http.MethodPut, "/api/files/"+taskID+"/chunks/0", token, "x"); chunk.Code != http.StatusOK {
			t.Fatalf("chunk %d = %d: %s", index, chunk.Code, chunk.Body.String())
		}
		complete := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+taskID+"/complete", token, "{}")
		if complete.Code != http.StatusOK {
			t.Fatalf("complete %d = %d: %s", index, complete.Code, complete.Body.String())
		}
		fileIDs = append(fileIDs, int64(responseData(t, complete)["id"].(float64)))
	}
	// 创建聚合分享（2 个文件，整体上限 2 次）。
	create := testJSONRequest(t, handler, http.MethodPost, "/api/files/batch-share-group", token, `{"fileIds":[`+strconv.FormatInt(fileIDs[1], 10)+`,`+strconv.FormatInt(fileIDs[0], 10)+`],"expiresInHours":24,"maxDownloads":2}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create share group = %d: %s", create.Code, create.Body.String())
	}
	created := responseData(t, create)
	groupToken := created["token"].(string)
	if created["url"] != "/g/"+groupToken {
		t.Fatalf("share group url = %#v", created["url"])
	}
	items := created["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("share group items = %d, want 2", len(items))
	}
	// 公开元数据：返回 2 个成员文件。
	meta := testJSONRequest(t, handler, http.MethodGet, "/api/shared-groups/"+groupToken+"/meta", "", "")
	if meta.Code != http.StatusOK {
		t.Fatalf("share group meta = %d: %s", meta.Code, meta.Body.String())
	}
	metaData := responseData(t, meta)
	files := metaData["files"].([]any)
	if len(files) != 2 || metaData["maxDownloads"] != float64(2) || metaData["downloadCount"] != float64(0) {
		t.Fatalf("share group meta = %#v", metaData)
	}
	// 单文件下载消耗 1 次。
	single := testJSONRequest(t, handler, http.MethodGet, "/api/shared-groups/"+groupToken+"/download/"+strconv.FormatInt(fileIDs[0], 10), "", "")
	if single.Code != http.StatusOK {
		t.Fatalf("share group single download = %d: %s", single.Code, single.Body.String())
	}
	// ZIP 下载消耗 1 次 → 之后达到上限。
	zipBody := testJSONRequest(t, handler, http.MethodPost, "/api/shared-groups/"+groupToken+"/batch-download", "", `{"ids":[`+strconv.FormatInt(fileIDs[0], 10)+`,`+strconv.FormatInt(fileIDs[1], 10)+`]}`)
	if zipBody.Code != http.StatusOK || zipBody.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("share group zip = %d: %s", zipBody.Code, zipBody.Body.String())
	}
	if !strings.Contains(zipBody.Header().Get("Content-Disposition"), "filebox-batch-"+time.Now().Format("20060102-")) || !strings.Contains(zipBody.Header().Get("Content-Disposition"), ".zip") {
		t.Fatalf("share group zip filename = %q", zipBody.Header().Get("Content-Disposition"))
	}
	blocked := testJSONRequest(t, handler, http.MethodGet, "/api/shared-groups/"+groupToken+"/download/"+strconv.FormatInt(fileIDs[1], 10), "", "")
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("share group limit = %d: %s", blocked.Code, blocked.Body.String())
	}
	// 管理列表可见；撤销后公开元数据为 revoked。
	groupList := testJSONRequest(t, handler, http.MethodGet, "/api/shares/groups", token, "")
	if groupList.Code != http.StatusOK || len(responseData(t, groupList)["items"].([]any)) != 1 {
		t.Fatalf("share group list = %d: %s", groupList.Code, groupList.Body.String())
	}
	revoked := testJSONRequest(t, handler, http.MethodDelete, "/api/shared-groups/"+groupToken, token, "")
	if revoked.Code != http.StatusOK {
		t.Fatalf("revoke share group = %d: %s", revoked.Code, revoked.Body.String())
	}
	revokedMeta := testJSONRequest(t, handler, http.MethodGet, "/api/shared-groups/"+groupToken+"/meta", "", "")
	if revokedMeta.Code != http.StatusForbidden {
		t.Fatalf("revoked share group meta = %d: %s", revokedMeta.Code, revokedMeta.Body.String())
	}
	// 越权：其他用户无法撤销该聚合分享。
	other := testJSONRequest(t, handler, http.MethodPost, "/api/admin/users", token, `{"username":"group-other","password":"Other123!","role":"user","quotaBytes":1024}`)
	if other.Code != http.StatusCreated {
		t.Fatalf("create other user = %d: %s", other.Code, other.Body.String())
	}
	otherLogin := testJSONRequest(t, handler, http.MethodPost, "/api/auth/login", "", `{"username":"group-other","password":"Other123!"}`)
	otherToken := responseData(t, otherLogin)["token"].(string)
	if cross := testJSONRequest(t, handler, http.MethodDelete, "/api/shared-groups/"+groupToken, otherToken, ""); cross.Code != http.StatusNotFound {
		t.Fatalf("cross-owner revoke group = %d: %s", cross.Code, cross.Body.String())
	}
	// 已删除文件从公开列表中隐藏。
	created2 := testJSONRequest(t, handler, http.MethodPost, "/api/files/batch-share-group", token, `{"fileIds":[`+strconv.FormatInt(fileIDs[0], 10)+`],"expiresInHours":24,"maxDownloads":0}`)
	if created2.Code != http.StatusCreated {
		t.Fatalf("create second group = %d: %s", created2.Code, created2.Body.String())
	}
	secondToken := responseData(t, created2)["token"].(string)
	if _, err := db.DeleteFile(context.Background(), fileIDs[0], 1, true); err != nil {
		t.Fatal(err)
	}
	meta2 := testJSONRequest(t, handler, http.MethodGet, "/api/shared-groups/"+secondToken+"/meta", "", "")
	meta2Data := responseData(t, meta2)
	if len(meta2Data["files"].([]any)) != 0 {
		t.Fatalf("deleted file not hidden: %#v", meta2Data["files"])
	}
}

// TestBatchDownloadsRejectOversizedArchives verifies both authenticated and anonymous ZIP paths reject oversized raw totals.
// TestBatchDownloadsRejectOversizedArchives 验证登录与匿名 ZIP 入口均拒绝超过原始字节上限的归档。
func TestBatchDownloadsRejectOversizedArchives(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	createdAt := time.Now().UTC().Format(time.RFC3339)
	result, err := db.DB.Exec(`INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, 'ready', ?, ?)`, 1, "oversized.bin", "oversized.bin", batchDownloadMaxBytes+1, "application/octet-stream", "", "", "files/1/oversized.bin", createdAt)
	if err != nil {
		t.Fatal(err)
	}
	fileID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	assertBatchTooLarge := func(t *testing.T, recorder *httptest.ResponseRecorder) {
		t.Helper()
		if recorder.Code != http.StatusRequestEntityTooLarge || responseData(t, recorder)["code"] != "BATCH_TOO_LARGE" {
			t.Fatalf("oversized batch response = %d: %s", recorder.Code, recorder.Body.String())
		}
	}
	assertBatchTooLarge(t, testJSONRequest(t, handler, http.MethodPost, "/api/files/batch-download", token, `{"ids":[`+strconv.FormatInt(fileID, 10)+`]}`))
	group := testJSONRequest(t, handler, http.MethodPost, "/api/files/batch-share-group", token, `{"fileIds":[`+strconv.FormatInt(fileID, 10)+`],"expiresInHours":1}`)
	if group.Code != http.StatusCreated {
		t.Fatalf("create oversized share group = %d: %s", group.Code, group.Body.String())
	}
	groupToken := responseData(t, group)["token"].(string)
	assertBatchTooLarge(t, testJSONRequest(t, handler, http.MethodPost, "/api/shared-groups/"+groupToken+"/batch-download", "", `{"ids":[`+strconv.FormatInt(fileID, 10)+`]}`))
}

func TestBatchDownloadsExposeArchiveCreationReason(t *testing.T) {
	_, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	file := uploadTestFile(t, handler, token, "archive-error.txt", "text/plain", []byte("content"))
	fileID := int64(file["id"].(float64))
	group := testJSONRequest(t, handler, http.MethodPost, "/api/files/batch-share-group", token, `{"fileIds":[`+strconv.FormatInt(fileID, 10)+`],"expiresInHours":1}`)
	if group.Code != http.StatusCreated {
		t.Fatalf("create archive-error share group = %d: %s", group.Code, group.Body.String())
	}
	groupToken := responseData(t, group)["token"].(string)

	previousCreateBatchTempFile := createBatchTempFile
	t.Cleanup(func() { createBatchTempFile = previousCreateBatchTempFile })
	createBatchTempFile = func(string, string) (*os.File, error) {
		return nil, &fs.PathError{Op: "create", Path: `C:\\private\\filebox\\tmp`, Err: syscall.EACCES}
	}

	assertArchiveReason := func(t *testing.T, recorder *httptest.ResponseRecorder) {
		t.Helper()
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("archive creation status = %d: %s", recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(recorder.Body.String(), "创建下载文件失败：目录无写入权限") {
			t.Fatalf("archive creation message = %s", recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), `C:\\private\\filebox\\tmp`) || responseData(t, recorder)["code"] != "ZIP_CREATE_FAILED" {
			t.Fatalf("archive creation response leaked details or code is wrong: %s", recorder.Body.String())
		}
	}
	assertArchiveReason(t, testJSONRequest(t, handler, http.MethodPost, "/api/files/batch-download", token, `{"ids":[`+strconv.FormatInt(fileID, 10)+`]}`))
	assertArchiveReason(t, testJSONRequest(t, handler, http.MethodPost, "/api/shared-groups/"+groupToken+"/batch-download", "", `{"ids":[`+strconv.FormatInt(fileID, 10)+`]}`))
}

func TestBatchDownloadsReportMissingSourceContent(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	file := uploadTestFile(t, handler, token, "missing-source.txt", "text/plain", []byte("content"))
	fileID := int64(file["id"].(float64))
	storedPath, err := db.FindFile(context.Background(), fileID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(db.DataDir, storedPath.StoragePath)); err != nil {
		t.Fatal(err)
	}
	recorder := testJSONRequest(t, handler, http.MethodPost, "/api/files/batch-download", token, `{"ids":[`+strconv.FormatInt(fileID, 10)+`]}`)
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), "文件内容不存在") {
		t.Fatalf("missing source response = %d: %s", recorder.Code, recorder.Body.String())
	}
}

// TestDeleteUploadTaskReleasesQuotaAndTemporaryChunks verifies cancellation removes the pending reservation and tmp directory.
// TestDeleteUploadTaskReleasesQuotaAndTemporaryChunks 验证取消任务会释放 pending 配额并清理临时目录。
func TestDeleteUploadTaskReleasesQuotaAndTemporaryChunks(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	if _, err := db.DB.Exec("UPDATE users SET quota_bytes = 10 WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	initData := initUpload(t, handler, token, "cancel.bin", 8, 8, "")
	taskID := initData["taskId"].(string)
	tmpDir := filepath.Join(db.DataDir, "tmp", taskID)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tmpFile, err := os.Create(filepath.Join(tmpDir, "0"))
	if err != nil {
		t.Fatal(err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatal(err)
	}
	var pending int64
	if err := db.DB.QueryRow("SELECT COALESCE(SUM(size), 0) FROM upload_tasks WHERE user_id = 1 AND status = 'pending'").Scan(&pending); err != nil || pending != 8 {
		t.Fatalf("pending quota before delete = %d, %v", pending, err)
	}
	deleted := testJSONRequest(t, handler, http.MethodDelete, "/api/files/tasks/"+taskID, token, "")
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete upload task = %d: %s", deleted.Code, deleted.Body.String())
	}
	if _, err := os.Stat(tmpDir); !os.IsNotExist(err) {
		t.Fatalf("temporary task directory still exists: %v", err)
	}
	if err := db.DB.QueryRow("SELECT COALESCE(SUM(size), 0) FROM upload_tasks WHERE user_id = 1 AND status = 'pending'").Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("pending quota after delete = %d, %v", pending, err)
	}
	initUpload(t, handler, token, "after-cancel.bin", 10, 10, "")
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

// TestSecurityHeadersIncludeFrameProtection 验证每个处理器响应都带有点击劫持防护。
// TestSecurityHeadersIncludeFrameProtection verifies every handler response carries clickjacking protection.
func TestSecurityHeadersIncludeFrameProtection(t *testing.T) {
	_, handler := newTestServer(t)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/auth/me", nil))
	if recorder.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want DENY", recorder.Header().Get("X-Frame-Options"))
	}
	if !strings.Contains(recorder.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatalf("CSP missing frame-ancestors protection: %q", recorder.Header().Get("Content-Security-Policy"))
	}
}

// TestPasswordChangeWritesAudit 验证成功和拒绝的自助改密都会写入审计记录。
// TestPasswordChangeWritesAudit verifies successful and rejected self-service password changes are audited.
func TestPasswordChangeWritesAudit(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	bad := testJSONRequest(t, handler, http.MethodPost, "/api/auth/change-password", token, `{"oldPassword":"wrong","newPassword":"Changed123!"}`)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("failed password change = %d: %s", bad.Code, bad.Body.String())
	}
	var result, reason string
	if err := db.DB.QueryRow("SELECT result, reason FROM audit_logs WHERE action = 'password_change' ORDER BY id DESC LIMIT 1").Scan(&result, &reason); err != nil || result != "failure" || reason != "failure" {
		t.Fatalf("failed password audit = %q/%q, %v", result, reason, err)
	}
	good := testJSONRequest(t, handler, http.MethodPost, "/api/auth/change-password", token, `{"oldPassword":"admin123","newPassword":"Changed123!"}`)
	if good.Code != http.StatusOK {
		t.Fatalf("successful password change = %d: %s", good.Code, good.Body.String())
	}
	if err := db.DB.QueryRow("SELECT result, reason FROM audit_logs WHERE action = 'password_change' ORDER BY id DESC LIMIT 1").Scan(&result, &reason); err != nil || result != "success" || reason != "success" {
		t.Fatalf("successful password audit = %q/%q, %v", result, reason, err)
	}
}

// TestCompleteRejectsChangedChunkHash 验证 complete 会重新校验服务端记录的分片哈希。
// TestCompleteRejectsChangedChunkHash verifies complete rechecks server-recorded chunk hashes.
func TestCompleteRejectsChangedChunkHash(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	content := []byte("chunk hash source")
	initData := initUpload(t, handler, token, "chunk-hash.txt", int64(len(content)), 0, "")
	taskID := initData["taskId"].(string)
	if got := testBinaryRequest(t, handler, http.MethodPut, "/api/files/"+taskID+"/chunks/0", token, content); got.Code != http.StatusOK {
		t.Fatalf("chunk upload = %d: %s", got.Code, got.Body.String())
	}
	if _, err := db.DB.Exec("UPDATE chunks SET sha256 = 'tampered' WHERE task_id = ?", taskID); err != nil {
		t.Fatal(err)
	}
	complete := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+taskID+"/complete", token, "{}")
	if complete.Code != http.StatusBadRequest || !strings.Contains(complete.Body.String(), "上传分片校验值不匹配") {
		t.Fatalf("changed chunk hash complete = %d: %s", complete.Code, complete.Body.String())
	}
}

// TestAdminGuardsLastAdministratorAndRemovesUserDirectory 验证唯一管理员保护和用户目录清理。
// TestAdminGuardsLastAdministratorAndRemovesUserDirectory verifies account safety and filesystem cleanup.
func TestAdminGuardsLastAdministratorAndRemovesUserDirectory(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	demote := testJSONRequest(t, handler, http.MethodPut, "/api/admin/users/1", token, `{"role":"user"}`)
	if demote.Code != http.StatusBadRequest || !strings.Contains(demote.Body.String(), "唯一管理员") {
		t.Fatalf("last administrator demotion = %d: %s", demote.Code, demote.Body.String())
	}
	created := testJSONRequest(t, handler, http.MethodPost, "/api/admin/users", token, `{"username":"directory-user","password":"Directory123!","role":"user","quotaBytes":1048576}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create directory user = %d: %s", created.Code, created.Body.String())
	}
	userID := int64(responseData(t, created)["id"].(float64))
	userDir := filepath.Join(db.DataDir, "files", strconv.FormatInt(userID, 10), "nested")
	if err := os.MkdirAll(userDir, 0o700); err != nil {
		t.Fatal(err)
	}
	deleted := testJSONRequest(t, handler, http.MethodDelete, "/api/admin/users/"+formatID(userID), token, "")
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete directory user = %d: %s", deleted.Code, deleted.Body.String())
	}
	if _, err := os.Stat(filepath.Dir(userDir)); !os.IsNotExist(err) {
		t.Fatalf("user directory still exists, stat error = %v", err)
	}
}

// TestUnlimitedShareCanBecomeFinite 验证 HTTP 层允许不限次数分享转为有限次数。
// TestUnlimitedShareCanBecomeFinite verifies the unlimited-to-finite transition at the HTTP layer.
func TestUnlimitedShareCanBecomeFinite(t *testing.T) {
	_, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	file := uploadTestFile(t, handler, token, "unlimited-share.txt", "text/plain", []byte("share"))
	fileID := int64(file["id"].(float64))
	created := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+formatID(fileID)+"/share", token, `{"expiresInHours":1,"maxDownloads":0}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create unlimited share = %d: %s", created.Code, created.Body.String())
	}
	shareToken := responseData(t, created)["token"].(string)
	updated := testJSONRequest(t, handler, http.MethodPut, "/api/shares/"+shareToken+"/increase", token, `{"maxDownloads":2}`)
	if updated.Code != http.StatusOK || responseData(t, updated)["maxDownloads"] != float64(2) {
		t.Fatalf("unlimited share increase = %d: %s", updated.Code, updated.Body.String())
	}
}

func TestJWTInvalidatedAfterPasswordChange(t *testing.T) {
	_, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)
	created := testJSONRequest(t, handler, http.MethodPost, "/api/admin/users", adminToken, `{"username":"jwt-rotate","password":"Readonly123!","role":"user"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create JWT test user = %d: %s", created.Code, created.Body.String())
	}
	userID := int64(responseData(t, created)["id"].(float64))
	login := testJSONRequest(t, handler, http.MethodPost, "/api/auth/login", "", `{"username":"jwt-rotate","password":"Readonly123!"}`)
	if login.Code != http.StatusOK {
		t.Fatalf("initial user login = %d: %s", login.Code, login.Body.String())
	}
	token1 := responseData(t, login)["token"].(string)
	if me := testJSONRequest(t, handler, http.MethodGet, "/api/auth/me", token1, ""); me.Code != http.StatusOK {
		t.Fatalf("initial token auth = %d: %s", me.Code, me.Body.String())
	}

	updated := testJSONRequest(t, handler, http.MethodPut, "/api/admin/users/"+formatID(userID), adminToken, `{"password":"BrandNew123!"}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("admin password change = %d: %s", updated.Code, updated.Body.String())
	}
	if me := testJSONRequest(t, handler, http.MethodGet, "/api/auth/me", token1, ""); me.Code != http.StatusUnauthorized {
		t.Fatalf("old token after admin password change = %d: %s", me.Code, me.Body.String())
	}

	login = testJSONRequest(t, handler, http.MethodPost, "/api/auth/login", "", `{"username":"jwt-rotate","password":"BrandNew123!"}`)
	if login.Code != http.StatusOK {
		t.Fatalf("new password login = %d: %s", login.Code, login.Body.String())
	}
	token2 := responseData(t, login)["token"].(string)
	if me := testJSONRequest(t, handler, http.MethodGet, "/api/auth/me", token2, ""); me.Code != http.StatusOK {
		t.Fatalf("new token auth = %d: %s", me.Code, me.Body.String())
	}

	selfChange := testJSONRequest(t, handler, http.MethodPost, "/api/auth/change-password", token2, `{"oldPassword":"BrandNew123!","newPassword":"SelfChange123!"}`)
	if selfChange.Code != http.StatusOK {
		t.Fatalf("self-service password change = %d: %s", selfChange.Code, selfChange.Body.String())
	}
	newToken, ok := responseData(t, selfChange)["token"].(string)
	if !ok || newToken == "" {
		t.Fatalf("self-service replacement token = %#v", responseData(t, selfChange)["token"])
	}
	if me := testJSONRequest(t, handler, http.MethodGet, "/api/auth/me", token2, ""); me.Code != http.StatusUnauthorized {
		t.Fatalf("old token after self-service password change = %d: %s", me.Code, me.Body.String())
	}
	if me := testJSONRequest(t, handler, http.MethodGet, "/api/auth/me", newToken, ""); me.Code != http.StatusOK {
		t.Fatalf("self-service replacement token auth = %d: %s", me.Code, me.Body.String())
	}
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
	update := testJSONRequest(t, handler, http.MethodPut, "/api/admin/settings", token, `{"ipLockThreshold":3,"trustProxy":true}`)
	if update.Code != http.StatusOK {
		t.Fatalf("partial settings update status = %d, want 200: %s", update.Code, update.Body.String())
	}
	settings := responseData(t, update)
	if settings["ipLockThreshold"] != float64(3) || settings["passwordMinLength"] != float64(8) || settings["passwordComplexity"] != float64(3) || settings["trustProxy"] != true {
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

func TestCreateUserAppliesSecuritySettings(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	created := testJSONRequest(t, handler, http.MethodPost, "/api/admin/users", token, `{"username":"secure-user","password":"Secure123!","role":"user","quotaBytes":1073741824,"totpEnabled":true,"ipAclEnabled":true,"ipWhitelist":"127.0.0.1, 10.0.0.0/8"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create secure user status = %d: %s", created.Code, created.Body.String())
	}
	data := responseData(t, created)
	secret, ok := data["totpSecret"].(string)
	if !ok || len(secret) != 32 {
		t.Fatalf("created TOTP secret = %#v", data["totpSecret"])
	}
	if data["totpEnabled"] != true || data["ipAclEnabled"] != true || data["ipWhitelist"] != "127.0.0.1,10.0.0.0/8" {
		t.Fatalf("created security settings = %#v", data)
	}
	user, err := db.GetUserByUsername("secure-user")
	if err != nil {
		t.Fatal(err)
	}
	if user.TOTPSecret == "" || !user.TOTPEnabled || !user.IPACLEnabled || user.IPWhitelist != "127.0.0.1,10.0.0.0/8" {
		t.Fatalf("stored security settings = %+v", user)
	}

	reenrolled := testJSONRequest(t, handler, http.MethodPost, "/api/admin/users", token, `{"username":"reenroll-user","password":"Reenroll123!","role":"user","reenroll":true}`)
	if reenrolled.Code != http.StatusCreated {
		t.Fatalf("create re-enroll user status = %d: %s", reenrolled.Code, reenrolled.Body.String())
	}
	reenrolledData := responseData(t, reenrolled)
	if reenrolledData["totpSecret"] == "" || reenrolledData["totpEnabled"] != false {
		t.Fatalf("re-enroll security settings = %#v", reenrolledData)
	}
}

func TestRequestIPRequiresTrustProxySetting(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	server := NewServer(db, Config{TrustedProxies: []*net.IPNet{{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(32, 32)}}})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "127.0.0.1:8080"
	request.Header.Set("X-Forwarded-For", "203.0.113.10, 127.0.0.1")
	if got := server.requestIP(request); got != "127.0.0.1" {
		t.Fatalf("request IP with default setting = %q, want proxy address", got)
	}
	settings, err := db.GetLogSettings(request.Context())
	if err != nil {
		t.Fatal(err)
	}
	settings.TrustProxy = true
	if err := db.UpdateLogSettings(request.Context(), settings); err != nil {
		t.Fatal(err)
	}
	if got := server.requestIP(request); got != "203.0.113.10" {
		t.Fatalf("request IP with trusted proxy setting = %q, want forwarded address", got)
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

func TestBatchDeleteFilesAndFoldersPrechecksNonEmptyFolders(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)

	emptyFolderResponse := testJSONRequest(t, handler, http.MethodPost, "/api/folders", token, `{"name":"empty-folder"}`)
	if emptyFolderResponse.Code != http.StatusCreated {
		t.Fatalf("create empty folder = %d: %s", emptyFolderResponse.Code, emptyFolderResponse.Body.String())
	}
	emptyFolderID := int64(responseData(t, emptyFolderResponse)["id"].(float64))
	nonEmptyFolderResponse := testJSONRequest(t, handler, http.MethodPost, "/api/folders", token, `{"name":"non-empty-folder"}`)
	if nonEmptyFolderResponse.Code != http.StatusCreated {
		t.Fatalf("create non-empty folder = %d: %s", nonEmptyFolderResponse.Code, nonEmptyFolderResponse.Body.String())
	}
	nonEmptyFolderData := responseData(t, nonEmptyFolderResponse)
	nonEmptyFolderID := int64(nonEmptyFolderData["id"].(float64))
	nonEmptyFolderPath := nonEmptyFolderData["path"].(string)

	init := initUpload(t, handler, token, "mixed-delete.txt", 7, 0, nonEmptyFolderPath)
	taskID := init["taskId"].(string)
	if chunk := testBinaryRequest(t, handler, http.MethodPut, "/api/files/"+taskID+"/chunks/0", token, []byte("content")); chunk.Code != http.StatusOK {
		t.Fatalf("upload mixed-delete file chunk = %d: %s", chunk.Code, chunk.Body.String())
	}
	complete := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+taskID+"/complete", token, `{}`)
	if complete.Code != http.StatusOK {
		t.Fatalf("complete mixed-delete file = %d: %s", complete.Code, complete.Body.String())
	}
	fileID := int64(responseData(t, complete)["id"].(float64))

	rejected := testJSONRequest(t, handler, http.MethodPost, "/api/files/batch-delete", token, `{"ids":[`+strconv.FormatInt(fileID, 10)+`],"folder_ids":[`+strconv.FormatInt(nonEmptyFolderID, 10)+`]}`)
	if rejected.Code != http.StatusBadRequest || responseData(t, rejected)["code"] != "FOLDER_NOT_EMPTY" {
		t.Fatalf("reject non-empty mixed delete = %d: %s", rejected.Code, rejected.Body.String())
	}
	remaining := testJSONRequest(t, handler, http.MethodGet, "/api/files?dir="+url.QueryEscape(nonEmptyFolderPath), token, "")
	if remaining.Code != http.StatusOK || responseData(t, remaining)["total"] != float64(1) {
		t.Fatalf("file after rejected mixed delete = %d: %s", remaining.Code, remaining.Body.String())
	}

	deleted := testJSONRequest(t, handler, http.MethodPost, "/api/files/batch-delete", token, `{"ids":[`+strconv.FormatInt(fileID, 10)+`],"folder_ids":[`+strconv.FormatInt(emptyFolderID, 10)+`]}`)
	if deleted.Code != http.StatusOK {
		t.Fatalf("successful mixed delete = %d: %s", deleted.Code, deleted.Body.String())
	}
	deletedData := responseData(t, deleted)
	if deletedData["deleted"] != float64(1) || deletedData["foldersDeleted"] != float64(1) {
		t.Fatalf("successful mixed delete response = %s", deleted.Body.String())
	}
	if _, err := db.GetFolderByID(context.Background(), emptyFolderID, 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("empty folder after mixed delete = %v, want not found", err)
	}
	deletedFile, err := db.FindFile(context.Background(), fileID)
	if err != nil || deletedFile.Status != "deleted" {
		t.Fatalf("file after mixed delete = %+v, %v; want deleted", deletedFile, err)
	}
}

func TestBatchDeleteForceRemovesFolderTree(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)

	rootResponse := testJSONRequest(t, handler, http.MethodPost, "/api/folders", token, `{"name":"recursive-root"}`)
	if rootResponse.Code != http.StatusCreated {
		t.Fatalf("create recursive root = %d: %s", rootResponse.Code, rootResponse.Body.String())
	}
	root := responseData(t, rootResponse)
	rootID := int64(root["id"].(float64))
	rootPath := root["path"].(string)
	childResponse := testJSONRequest(t, handler, http.MethodPost, "/api/folders", token, `{"name":"nested-child","parent":"`+rootPath+`"}`)
	if childResponse.Code != http.StatusCreated {
		t.Fatalf("create nested child = %d: %s", childResponse.Code, childResponse.Body.String())
	}
	child := responseData(t, childResponse)
	childID := int64(child["id"].(float64))
	childPath := child["path"].(string)

	content := []byte("recursive deletion content")
	init := initUpload(t, handler, token, "tree-file.txt", int64(len(content)), 0, childPath)
	taskID := init["taskId"].(string)
	chunk := testBinaryRequest(t, handler, http.MethodPut, "/api/files/"+taskID+"/chunks/0", token, content)
	if chunk.Code != http.StatusOK {
		t.Fatalf("upload tree file chunk = %d: %s", chunk.Code, chunk.Body.String())
	}
	complete := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+taskID+"/complete", token, `{}`)
	if complete.Code != http.StatusOK {
		t.Fatalf("complete tree file = %d: %s", complete.Code, complete.Body.String())
	}
	fileID := int64(responseData(t, complete)["id"].(float64))

	var usedBefore int64
	if err := db.DB.QueryRow("SELECT used_bytes FROM users WHERE id = 1").Scan(&usedBefore); err != nil {
		t.Fatal(err)
	}
	if usedBefore != int64(len(content)) {
		t.Fatalf("used bytes before force delete = %d, want %d", usedBefore, len(content))
	}

	rejected := testJSONRequest(t, handler, http.MethodPost, "/api/files/batch-delete", token, `{"ids":[],"folder_ids":[`+strconv.FormatInt(rootID, 10)+`]}`)
	if rejected.Code != http.StatusBadRequest || responseData(t, rejected)["code"] != "FOLDER_NOT_EMPTY" || !strings.Contains(rejected.Body.String(), "recursive-root") {
		t.Fatalf("reject non-empty recursive delete = %d: %s", rejected.Code, rejected.Body.String())
	}

	deleted := testJSONRequest(t, handler, http.MethodPost, "/api/files/batch-delete", token, `{"ids":[],"folder_ids":[`+strconv.FormatInt(rootID, 10)+`],"force":true}`)
	if deleted.Code != http.StatusOK {
		t.Fatalf("force delete folder tree = %d: %s", deleted.Code, deleted.Body.String())
	}
	deletedData := responseData(t, deleted)
	if deletedData["deleted"] != float64(1) || deletedData["foldersDeleted"] != float64(2) {
		t.Fatalf("force delete response = %s", deleted.Body.String())
	}
	if _, err := db.GetFolderByID(context.Background(), rootID, 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("root folder after force delete = %v, want not found", err)
	}
	if _, err := db.GetFolderByID(context.Background(), childID, 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("child folder after force delete = %v, want not found", err)
	}
	deletedFile, err := db.FindFile(context.Background(), fileID)
	if err != nil || deletedFile.Status != "deleted" {
		t.Fatalf("file after force delete = %+v, %v; want deleted", deletedFile, err)
	}
	var usedAfter int64
	if err := db.DB.QueryRow("SELECT used_bytes FROM users WHERE id = 1").Scan(&usedAfter); err != nil {
		t.Fatal(err)
	}
	if usedAfter != 0 {
		t.Fatalf("used bytes after force delete = %d, want 0", usedAfter)
	}
	if _, err := os.Stat(filepath.Join(db.DataDir, "files", "1", rootPath)); !os.IsNotExist(err) {
		t.Fatalf("root folder on disk after force delete = %v, want not found", err)
	}
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
	var limitReason string
	if err := db.DB.QueryRow("SELECT reason FROM audit_logs WHERE action = 'share_download' AND target = ? ORDER BY id DESC LIMIT 1", shareToken).Scan(&limitReason); err != nil || limitReason != "share_limit" {
		t.Fatalf("limited audit reason = %q, %v", limitReason, err)
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
	var expiredReason string
	if err := db.DB.QueryRow("SELECT reason FROM audit_logs WHERE action = 'share_download' AND target = ? ORDER BY id DESC LIMIT 1", shareToken).Scan(&expiredReason); err != nil || expiredReason != "share_expired" {
		t.Fatalf("expired audit reason = %q, %v", expiredReason, err)
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

// TestCheckInstantUploadReturns500WhenConflictLookupFails verifies conflict lookup errors do not permit instant upload.
// TestCheckInstantUploadReturns500WhenConflictLookupFails 验证冲突查询失败时不会放行秒传。
func TestCheckInstantUploadReturns500WhenConflictLookupFails(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.EnsureAdmin("admin", "admin123", 32*1024*1024); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE users SET must_change_password = 0 WHERE username = 'admin'"); err != nil {
		t.Fatal(err)
	}
	server := NewServer(db, Config{DataDir: db.DataDir, MaxFileSize: 32 * 1024 * 1024, JWTSecret: []byte("test-secret")})
	handler := server.Handler()
	token := testAdminToken(t, handler)
	file := uploadTestFile(t, handler, token, "instant-error.txt", "text/plain", []byte("instant content"))
	server.findUploadConflict = func(context.Context, int64, string, string) (store.File, error) {
		return store.File{}, errors.New("injected conflict lookup failure")
	}
	body, err := json.Marshal(map[string]any{"sha256": file["sha256"], "size": file["size"], "name": "different-target.txt"})
	if err != nil {
		t.Fatal(err)
	}
	check := testJSONRequest(t, handler, http.MethodPost, "/api/files/check", token, string(body))
	if check.Code != http.StatusInternalServerError {
		t.Fatalf("instant check with conflict lookup error = %d: %s", check.Code, check.Body.String())
	}
}

func TestSharePreviewLimitsLargeTextAndRange(t *testing.T) {
	_, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	content := bytes.Repeat([]byte("preview content\n"), 4097)
	file := uploadTestFile(t, handler, token, "large-preview.txt", "text/plain", content)
	id := int64(file["id"].(float64))
	created := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+strconv.FormatInt(id, 10)+"/share", token, `{"expiresInHours":1,"maxDownloads":1}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create share status = %d: %s", created.Code, created.Body.String())
	}
	shareToken := responseData(t, created)["token"].(string)
	const previewLimit = 64 * 1024

	preview := testBinaryRequest(t, handler, http.MethodGet, "/api/files/shared/"+shareToken+"/preview", "", nil)
	if preview.Code != http.StatusOK || len(preview.Body.Bytes()) != previewLimit {
		t.Fatalf("large share preview = %d bytes, status %d; want %d bytes and 200", preview.Body.Len(), preview.Code, previewLimit)
	}
	if preview.Header().Get("X-Content-Length-Limit") != strconv.Itoa(previewLimit) {
		t.Fatalf("preview limit header = %q, want %d", preview.Header().Get("X-Content-Length-Limit"), previewLimit)
	}

	rangeRequest := httptest.NewRequest(http.MethodGet, "/api/files/shared/"+shareToken+"/preview", nil)
	rangeRequest.Header.Set("Range", "bytes=0-100000")
	rangeResponse := httptest.NewRecorder()
	handler.ServeHTTP(rangeResponse, rangeRequest)
	if rangeResponse.Code != http.StatusPartialContent || len(rangeResponse.Body.Bytes()) != previewLimit {
		t.Fatalf("large share range preview = %d bytes, status %d; want %d bytes and 206", rangeResponse.Body.Len(), rangeResponse.Code, previewLimit)
	}
	if got := rangeResponse.Header().Get("Content-Range"); got != "bytes 0-65535/65536" {
		t.Fatalf("range content header = %q, want bytes 0-65535/65536", got)
	}
}

func TestSharePreviewDoesNotConsumeDownloadCount(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	file := uploadTestFile(t, handler, token, "preview.txt", "text/plain", []byte("preview content"))
	id := int64(file["id"].(float64))
	created := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+strconv.FormatInt(id, 10)+"/share", token, `{"expiresInHours":1,"maxDownloads":1}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create share status = %d: %s", created.Code, created.Body.String())
	}
	shareToken := responseData(t, created)["token"].(string)
	preview := testBinaryRequest(t, handler, http.MethodGet, "/api/files/shared/"+shareToken+"/preview", "", nil)
	if preview.Code != http.StatusOK || !bytes.Equal(preview.Body.Bytes(), []byte("preview content")) {
		t.Fatalf("share preview = %d, %q", preview.Code, preview.Body.String())
	}
	if preview.Header().Get("Content-Disposition") != "inline" {
		t.Fatalf("share preview content disposition = %q, want inline", preview.Header().Get("Content-Disposition"))
	}
	meta := testJSONRequest(t, handler, http.MethodGet, "/api/files/shared/"+shareToken+"/meta", "", "")
	metaData := responseData(t, meta)
	if metaData["downloadAvailable"] != true {
		t.Fatalf("preview consumed a download slot: %#v", metaData)
	}
	down := testBinaryRequest(t, handler, http.MethodGet, "/api/files/shared/"+shareToken+"/download", "", nil)
	if down.Code != http.StatusOK || !bytes.Equal(down.Body.Bytes(), []byte("preview content")) {
		t.Fatalf("share download after preview = %d, %q", down.Code, down.Body.String())
	}
	limited := testBinaryRequest(t, handler, http.MethodGet, "/api/files/shared/"+shareToken+"/download", "", nil)
	if limited.Code != http.StatusForbidden {
		t.Fatalf("limited share download status = %d", limited.Code)
	}
	var previewResult string
	if err := db.DB.QueryRow("SELECT result FROM audit_logs WHERE action = 'share_preview' ORDER BY id DESC LIMIT 1").Scan(&previewResult); err != nil || previewResult != "success" {
		t.Fatalf("share_preview audit = %q, %v", previewResult, err)
	}
}

// TestOverwriteUploadReplacesFileAtomically 验证覆盖上传端到端：旧文件保留到 complete，
// complete 后内容与记录原子换新（G5/G6）。
// TestOverwriteUploadReplacesFileAtomically verifies the overwrite upload end to end: the old
// file survives until complete, then content and record are replaced atomically (G5/G6).
func TestOverwriteUploadReplacesFileAtomically(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	first := uploadTestFile(t, handler, token, "overwrite-me.txt", "text/plain", []byte("version-one"))
	firstID := int64(first["id"].(float64))

	// 覆盖任务创建后旧文件仍可下载（G6：上传失败不丢旧内容）。
	// After the overwrite task is created the old file must still be downloadable (G6).
	init := testJSONRequest(t, handler, http.MethodPost, "/api/files/upload-init", token, `{"name":"overwrite-me.txt","size":2,"chunkSize":0,"resolve":"overwrite"}`)
	if init.Code != http.StatusOK {
		t.Fatalf("overwrite init status = %d: %s", init.Code, init.Body.String())
	}
	taskID := responseData(t, init)["taskId"].(string)
	before := testJSONRequest(t, handler, http.MethodGet, "/api/files/"+formatID(firstID)+"/download", token, "")
	if before.Code != http.StatusOK || before.Body.String() != "version-one" {
		t.Fatalf("old file while overwrite pending = %d, %q", before.Code, before.Body.String())
	}

	chunk := testBinaryRequest(t, handler, http.MethodPut, "/api/files/"+taskID+"/chunks/0", token, []byte("v2"))
	if chunk.Code != http.StatusOK {
		t.Fatalf("overwrite chunk status = %d: %s", chunk.Code, chunk.Body.String())
	}
	complete := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+taskID+"/complete", token, `{}`)
	if complete.Code != http.StatusOK {
		t.Fatalf("overwrite complete status = %d: %s", complete.Code, complete.Body.String())
	}
	completed := responseData(t, complete)
	newID := int64(completed["id"].(float64))
	if newID == firstID {
		t.Fatalf("overwrite reused the old file id %d, want a new record", firstID)
	}
	// 新文件内容与记录就位，目录中同名文件仅剩一条。
	// The new content and record are in place; only one same-named file remains.
	after := testJSONRequest(t, handler, http.MethodGet, "/api/files/"+formatID(newID)+"/download", token, "")
	if after.Code != http.StatusOK || after.Body.String() != "v2" {
		t.Fatalf("replaced file download = %d, %q", after.Code, after.Body.String())
	}
	list := testJSONRequest(t, handler, http.MethodGet, "/api/files", token, "")
	var sameName int
	for _, item := range responseData(t, list)["items"].([]any) {
		if item.(map[string]any)["name"] == "overwrite-me.txt" {
			sameName++
		}
	}
	if sameName != 1 {
		t.Fatalf("same-named file count after overwrite = %d, want 1", sameName)
	}
	// 旧记录已删除，新记录占用同一存储路径。
	// The old record is gone and the new one owns the same storage path.
	var storagePath string
	var rowCount int
	if err := db.DB.QueryRow("SELECT storage_path FROM files WHERE id = ?", newID).Scan(&storagePath); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.QueryRow("SELECT COUNT(id) FROM files WHERE storage_path = ?", storagePath).Scan(&rowCount); err != nil {
		t.Fatal(err)
	}
	if rowCount != 1 {
		t.Fatalf("rows at storage path %q = %d, want 1", storagePath, rowCount)
	}
}

func TestBatchShareCreatesIndependentLinksAndRejectsUnauthorizedBatch(t *testing.T) {
	db, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)
	first := uploadTestFile(t, handler, adminToken, "batch-share-one.txt", "text/plain", []byte("one"))
	second := uploadTestFile(t, handler, adminToken, "batch-share-two.txt", "text/plain", []byte("two"))
	firstID := int64(first["id"].(float64))
	secondID := int64(second["id"].(float64))
	batchDownload := testBinaryRequest(t, handler, http.MethodPost, "/api/files/batch-download", adminToken, []byte(`{"ids":[`+strconv.FormatInt(firstID, 10)+`,`+strconv.FormatInt(secondID, 10)+`]}`))
	if batchDownload.Code != http.StatusOK || batchDownload.Header().Get("Content-Length") != strconv.Itoa(batchDownload.Body.Len()) {
		t.Fatalf("batch download content length = %d/%q", batchDownload.Code, batchDownload.Header().Get("Content-Length"))
	}
	if !strings.Contains(batchDownload.Header().Get("Content-Disposition"), "filebox-batch-"+time.Now().Format("20060102-")) || !strings.Contains(batchDownload.Header().Get("Content-Disposition"), ".zip") {
		t.Fatalf("batch download filename = %q", batchDownload.Header().Get("Content-Disposition"))
	}
	body := `{"fileIds":[` + strconv.FormatInt(firstID, 10) + `,` + strconv.FormatInt(secondID, 10) + `],"expiresInHours":24,"maxDownloads":3}`
	created := testJSONRequest(t, handler, http.MethodPost, "/api/files/batch-share", adminToken, body)
	if created.Code != http.StatusCreated {
		t.Fatalf("batch share status = %d: %s", created.Code, created.Body.String())
	}
	items := responseData(t, created)["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("batch share item count = %d", len(items))
	}
	tokens := make(map[string]bool)
	for _, raw := range items {
		item := raw.(map[string]any)
		token, ok := item["token"].(string)
		if !ok || len(token) != 64 || tokens[token] || item["url"] != "/"+token || item["fileId"] == nil || item["fileName"] == nil {
			t.Fatalf("batch share item = %#v", item)
		}
		tokens[token] = true
	}
	shares := testJSONRequest(t, handler, http.MethodGet, "/api/shares", adminToken, "")
	if shares.Code != http.StatusOK || len(responseData(t, shares)["items"].([]any)) != 2 {
		t.Fatalf("batch shares management list = %d: %s", shares.Code, shares.Body.String())
	}
	var auditResult, auditReason string
	if err := db.DB.QueryRow("SELECT result, reason FROM audit_logs WHERE action = 'batch_share' ORDER BY id DESC LIMIT 1").Scan(&auditResult, &auditReason); err != nil || auditResult != "success" || auditReason != "batch" {
		t.Fatalf("batch share audit = %q/%q, %v", auditResult, auditReason, err)
	}

	other := testJSONRequest(t, handler, http.MethodPost, "/api/admin/users", adminToken, `{"username":"batch-share-other","password":"Batch123!","role":"user","quotaBytes":1048576}`)
	if other.Code != http.StatusCreated {
		t.Fatalf("create batch share test user = %d: %s", other.Code, other.Body.String())
	}
	login := testJSONRequest(t, handler, http.MethodPost, "/api/auth/login", "", `{"username":"batch-share-other","password":"Batch123!"}`)
	if login.Code != http.StatusOK {
		t.Fatalf("batch share test user login = %d: %s", login.Code, login.Body.String())
	}
	otherToken := responseData(t, login)["token"].(string)
	forbidden := testJSONRequest(t, handler, http.MethodPost, "/api/files/batch-share", otherToken, `{"fileIds":[`+strconv.FormatInt(firstID, 10)+`,`+strconv.FormatInt(secondID, 10)+`],"expiresInHours":24}`)
	if forbidden.Code != http.StatusNotFound {
		t.Fatalf("unauthorized batch share status = %d: %s", forbidden.Code, forbidden.Body.String())
	}
	sharesAfter := testJSONRequest(t, handler, http.MethodGet, "/api/shares", adminToken, "")
	if sharesAfter.Code != http.StatusOK || len(responseData(t, sharesAfter)["items"].([]any)) != 2 {
		t.Fatalf("unauthorized batch share created a link: %s", sharesAfter.Body.String())
	}
	if err := db.DB.QueryRow("SELECT result FROM audit_logs WHERE action = 'batch_share' AND user_id = (SELECT id FROM users WHERE username = 'batch-share-other') ORDER BY id DESC LIMIT 1").Scan(&auditResult); err != nil || auditResult != "failure" {
		t.Fatalf("unauthorized batch share audit = %q, %v", auditResult, err)
	}
}

func TestBatchOperationsRejectOverLimit(t *testing.T) {
	_, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	ids := make([]string, 501)
	for i := range ids {
		ids[i] = strconv.Itoa(i + 1)
	}
	idList := strings.Join(ids, ",")

	batchShare := testJSONRequest(t, handler, http.MethodPost, "/api/files/batch-share", token, `{"fileIds":[`+idList+`],"expiresInHours":24}`)
	if batchShare.Code != http.StatusBadRequest {
		t.Fatalf("over-limit batch share status = %d: %s", batchShare.Code, batchShare.Body.String())
	}

	batchDownload := testJSONRequest(t, handler, http.MethodPost, "/api/files/batch-download", token, `{"ids":[`+idList+`]}`)
	if batchDownload.Code != http.StatusBadRequest {
		t.Fatalf("over-limit batch download status = %d: %s", batchDownload.Code, batchDownload.Body.String())
	}

	batchDelete := testJSONRequest(t, handler, http.MethodPost, "/api/files/batch-delete", token, `{"ids":[`+idList+`]}`)
	if batchDelete.Code != http.StatusBadRequest {
		t.Fatalf("over-limit batch delete status = %d: %s", batchDelete.Code, batchDelete.Body.String())
	}
}

func TestShareManagementAndOwnerDownloadLogs(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	file := uploadTestFile(t, handler, token, "managed-share.txt", "text/plain", []byte("managed share"))
	id := int64(file["id"].(float64))
	created := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+strconv.FormatInt(id, 10)+"/share", token, `{"expiresInHours":1,"maxDownloads":1}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create managed share = %d: %s", created.Code, created.Body.String())
	}
	shareToken := responseData(t, created)["token"].(string)
	list := testJSONRequest(t, handler, http.MethodGet, "/api/shares", token, "")
	if list.Code != http.StatusOK {
		t.Fatalf("list shares = %d: %s", list.Code, list.Body.String())
	}
	items := responseData(t, list)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["fileName"] != "managed-share.txt" || items[0].(map[string]any)["status"] != "active" {
		t.Fatalf("share list = %#v", items)
	}
	detail := testJSONRequest(t, handler, http.MethodGet, "/api/shares/"+shareToken, token, "")
	if detail.Code != http.StatusOK || responseData(t, detail)["downloadCount"] != float64(0) {
		t.Fatalf("share detail = %d: %s", detail.Code, detail.Body.String())
	}
	extend := testJSONRequest(t, handler, http.MethodPut, "/api/shares/"+shareToken+"/extend", token, `{"expiresInHours":48}`)
	if extend.Code != http.StatusOK || responseData(t, extend)["fileName"] != "managed-share.txt" {
		t.Fatalf("extend share = %d: %s", extend.Code, extend.Body.String())
	}
	increase := testJSONRequest(t, handler, http.MethodPut, "/api/shares/"+shareToken+"/increase", token, `{"maxDownloads":2}`)
	if increase.Code != http.StatusOK || responseData(t, increase)["maxDownloads"] != float64(2) {
		t.Fatalf("increase share = %d: %s", increase.Code, increase.Body.String())
	}
	if down := testBinaryRequest(t, handler, http.MethodGet, "/api/files/shared/"+shareToken+"/download", "", nil); down.Code != http.StatusOK {
		t.Fatalf("managed share download = %d: %s", down.Code, down.Body.String())
	}
	logs := testJSONRequest(t, handler, http.MethodGet, "/api/shares/"+shareToken+"/logs", token, "")
	if logs.Code != http.StatusOK {
		t.Fatalf("share logs = %d: %s", logs.Code, logs.Body.String())
	}
	logItems := responseData(t, logs)["items"].([]any)
	if len(logItems) != 1 || logItems[0].(map[string]any)["result"] != "success" || logItems[0].(map[string]any)["shareOwnerId"] != float64(1) {
		t.Fatalf("share logs = %#v", logItems)
	}
	revoked := testJSONRequest(t, handler, http.MethodDelete, "/api/shares/"+shareToken, token, "")
	if revoked.Code != http.StatusOK {
		t.Fatalf("single revoke = %d: %s", revoked.Code, revoked.Body.String())
	}
	if meta := testJSONRequest(t, handler, http.MethodGet, "/api/files/shared/"+shareToken+"/meta", "", ""); meta.Code != http.StatusNotFound {
		t.Fatalf("revoked meta = %d: %s", meta.Code, meta.Body.String())
	}
	if down := testBinaryRequest(t, handler, http.MethodGet, "/api/files/shared/"+shareToken+"/download", "", nil); down.Code != http.StatusNotFound {
		t.Fatalf("revoked download = %d: %s", down.Code, down.Body.String())
	}
	var revokedReason string
	if err := db.DB.QueryRow("SELECT reason FROM audit_logs WHERE action = 'share_download' AND target = ? ORDER BY id DESC LIMIT 1", shareToken).Scan(&revokedReason); err != nil || revokedReason != "share_revoked" {
		t.Fatalf("revoked audit reason = %q, %v", revokedReason, err)
	}
	ownerLogs := testJSONRequest(t, handler, http.MethodGet, "/api/logs?action=share_download", token, "")
	if ownerLogs.Code != http.StatusOK || responseData(t, ownerLogs)["total"] != float64(2) {
		t.Fatalf("owner share logs = %d: %s", ownerLogs.Code, ownerLogs.Body.String())
	}
}

func TestLogsEndpointTimeRangeFilter(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	admin, err := db.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	// 管理员登录已写入一条审计日志（created_at=当前时间）。
	base := time.Now().UTC()
	from := base.Add(-24 * time.Hour).Format(time.RFC3339)
	to := base.Add(24 * time.Hour).Format(time.RFC3339)
	_ = admin
	ranged := testJSONRequest(t, handler, http.MethodGet, "/api/logs?from="+url.QueryEscape(from)+"&to="+url.QueryEscape(to), token, "")
	if ranged.Code != http.StatusOK {
		t.Fatalf("ranged logs = %d: %s", ranged.Code, ranged.Body.String())
	}
	total := responseData(t, ranged)["total"].(float64)
	if total < 1 {
		t.Fatalf("ranged logs total = %v, want >= 1 (login audit within window)", total)
	}
	// 未来区间：不应命中当前日志。
	future := testJSONRequest(t, handler, http.MethodGet, "/api/logs?from="+url.QueryEscape(base.Add(48*time.Hour).Format(time.RFC3339)), token, "")
	if future.Code != http.StatusOK || responseData(t, future)["total"] != float64(0) {
		t.Fatalf("future-window logs = %d: %s", future.Code, future.Body.String())
	}
	// 非法 from：400。
	invalid := testJSONRequest(t, handler, http.MethodGet, "/api/logs?from=not-a-time", token, "")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid from status = %d, want 400", invalid.Code)
	}
	// 非法 to：400。
	invalidTo := testJSONRequest(t, handler, http.MethodGet, "/api/logs?to=2026-99-99T00:00:00Z", token, "")
	if invalidTo.Code != http.StatusBadRequest {
		t.Fatalf("invalid to status = %d, want 400", invalidTo.Code)
	}
}

func TestBatchDeleteFilesEndpointDeletesAtomically(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	first := uploadTestFile(t, handler, token, "batch-one.txt", "text/plain", []byte("one"))
	second := uploadTestFile(t, handler, token, "batch-two.txt", "text/plain", []byte("two"))
	firstID := int64(first["id"].(float64))
	secondID := int64(second["id"].(float64))
	firstMeta, err := db.FindFile(context.Background(), firstID)
	if err != nil {
		t.Fatal(err)
	}
	secondMeta, err := db.FindFile(context.Background(), secondID)
	if err != nil {
		t.Fatal(err)
	}
	deleted := testJSONRequest(t, handler, http.MethodPost, "/api/files/batch-delete", token, `{"ids":[`+strconv.FormatInt(firstID, 10)+`,`+strconv.FormatInt(secondID, 10)+`]}`)
	if deleted.Code != http.StatusOK {
		t.Fatalf("batch delete status = %d: %s", deleted.Code, deleted.Body.String())
	}
	if responseData(t, deleted)["deleted"] != float64(2) {
		t.Fatalf("batch delete response = %s", deleted.Body.String())
	}
	list := testJSONRequest(t, handler, http.MethodGet, "/api/files", token, "")
	if list.Code != http.StatusOK || responseData(t, list)["total"] != float64(0) {
		t.Fatalf("files after batch delete = %d: %s", list.Code, list.Body.String())
	}
	admin, err := db.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	if admin.UsedBytes != 0 {
		t.Fatalf("used bytes after batch delete = %d", admin.UsedBytes)
	}
	for _, file := range []store.File{firstMeta, secondMeta} {
		if _, err := os.Stat(filepath.Join(db.DataDir, file.StoragePath)); !os.IsNotExist(err) {
			t.Fatalf("physical file %q still exists, stat error = %v", file.StoragePath, err)
		}
	}
	logs, _, err := db.ListAuditLogs(context.Background(), &admin.ID, "delete", "", "", "", "", 1, 20)
	if err != nil || len(logs) == 0 || logs[0].Reason != "batch" || logs[0].Result != "success" {
		t.Fatalf("batch delete audit logs = %+v, %v", logs, err)
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

// TestLogoutRevokesJWT ensures a token cannot authenticate after logout.
// TestLogoutRevokesJWT 确保令牌登出后不能继续通过认证。
func TestLogoutRevokesJWT(t *testing.T) {
	_, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	logout := testJSONRequest(t, handler, http.MethodPost, "/api/auth/logout", token, "")
	if logout.Code != http.StatusOK {
		t.Fatalf("logout status = %d: %s", logout.Code, logout.Body.String())
	}
	me := testJSONRequest(t, handler, http.MethodGet, "/api/auth/me", token, "")
	if me.Code != http.StatusUnauthorized {
		t.Fatalf("request with logged-out token = %d: %s", me.Code, me.Body.String())
	}
}

// TestBatchShareRollsBackCreatedShares ensures a later create failure leaves no active links.
// TestBatchShareRollsBackCreatedShares 确保后续创建失败时不留下有效分享链接。
func TestBatchShareRollsBackCreatedShares(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	first := uploadTestFile(t, handler, token, "rollback-first.txt", "text/plain", []byte("first"))
	second := uploadTestFile(t, handler, token, "rollback-second.txt", "text/plain", []byte("second"))
	firstID := int64(first["id"].(float64))
	secondID := int64(second["id"].(float64))
	trigger := "CREATE TRIGGER fail_batch_share_second BEFORE INSERT ON shares WHEN NEW.file_id = " + strconv.FormatInt(secondID, 10) + " BEGIN SELECT RAISE(ABORT, 'forced batch share failure'); END"
	if _, err := db.DB.Exec(trigger); err != nil {
		t.Fatal(err)
	}
	created := testJSONRequest(t, handler, http.MethodPost, "/api/files/batch-share", token, `{"fileIds":[`+strconv.FormatInt(firstID, 10)+`,`+strconv.FormatInt(secondID, 10)+`],"expiresInHours":24}`)
	if created.Code != http.StatusInternalServerError {
		t.Fatalf("failed batch share status = %d: %s", created.Code, created.Body.String())
	}
	var active, total int
	if err := db.DB.QueryRow("SELECT COUNT(id) FROM shares WHERE revoked_at IS NULL").Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.QueryRow("SELECT COUNT(id) FROM shares").Scan(&total); err != nil {
		t.Fatal(err)
	}
	if active != 0 || total != 1 {
		t.Fatalf("shares after rollback = active %d, total %d; want 0, 1 revoked record", active, total)
	}
}

// TestRegistrationRateLimit rejects the sixth registration request from one IP in a minute.
// TestRegistrationRateLimit 限制同一 IP 每分钟最多发起五次注册请求。
func TestRegistrationRateLimit(t *testing.T) {
	_, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)
	settings := testJSONRequest(t, handler, http.MethodPut, "/api/admin/settings", adminToken, `{"registerEnabled":true}`)
	if settings.Code != http.StatusOK {
		t.Fatalf("enable registration status = %d: %s", settings.Code, settings.Body.String())
	}
	for index := 0; index < 5; index++ {
		request := testJSONRequest(t, handler, http.MethodPost, "/api/auth/register", "", `{}`)
		if request.Code == http.StatusTooManyRequests {
			t.Fatalf("registration request %d was rate limited too early", index+1)
		}
	}
	limited := testJSONRequest(t, handler, http.MethodPost, "/api/auth/register", "", `{}`)
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("sixth registration status = %d: %s", limited.Code, limited.Body.String())
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
