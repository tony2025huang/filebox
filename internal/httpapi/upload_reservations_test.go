package httpapi

import (
	"net/http"
	"testing"
	"time"
)

// TestUploadInitReleasesStaleQuotaReservation 覆盖 v037：一条长期无活动的未完成上传会一直占用
// 预留配额（界面上看不到），必须在下次上传开始前被回收，否则用户会看到"已用远低于配额却配额不足"。
// A long-idle unfinished upload keeps reserving quota invisibly; it must be reclaimed before the next
// upload so the user does not get "quota exceeded" while the UI shows plenty of free space.
func TestUploadInitReleasesStaleQuotaReservation(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)

	// 造一条 7 小时前的废弃任务，预留 200GiB（测试管理员配额 100GiB）足以触发配额拒绝。
	stale := time.Now().UTC().Add(-7 * time.Hour).Format(time.RFC3339)
	if _, err := db.DB.Exec(
		"INSERT INTO upload_tasks(id, user_id, collection_id, name, size, mime, chunk_size, total_chunks, status, created_at, updated_at) VALUES(?, 1, 0, 'stale.bin', ?, '', 0, 0, 'pending', ?, ?)",
		"stale-reservation-1", int64(200)*1024*1024*1024, stale, stale); err != nil {
		t.Fatalf("seed stale reservation: %v", err)
	}

	init := testJSONRequest(t, handler, http.MethodPost, "/api/files/upload-init", token, `{"name":"fresh.txt","size":1024,"chunkSize":0}`)
	if init.Code != http.StatusOK {
		t.Fatalf("upload-init after releasing the stale reservation = %d: %s", init.Code, init.Body.String())
	}

	var left int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM upload_tasks WHERE id = 'stale-reservation-1'").Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("stale reservation still present: %d", left)
	}
}

// TestUploadInitKeepsRecentReservation 确认回收只作用于长期无活动的任务：刚创建的任务（例如正常
// 上传或并发上传）不会被误删，配额仍然照常生效。
// Reclaiming must only touch long-idle tasks: a fresh reservation (a normal or concurrent upload) stays.
func TestUploadInitKeepsRecentReservation(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)

	recent := time.Now().UTC().Add(-1 * time.Minute).Format(time.RFC3339)
	if _, err := db.DB.Exec(
		"INSERT INTO upload_tasks(id, user_id, collection_id, name, size, mime, chunk_size, total_chunks, status, created_at, updated_at) VALUES(?, 1, 0, 'recent.bin', ?, '', 0, 0, 'pending', ?, ?)",
		"recent-reservation-1", int64(500)*1024*1024*1024, recent, recent); err != nil {
		t.Fatalf("seed recent reservation: %v", err)
	}

	// 500GiB 预留 + 本次 1KiB 超过 100GiB 配额，必须仍然被拒绝（且不能删除那条新预留）。
	init := testJSONRequest(t, handler, http.MethodPost, "/api/files/upload-init", token, `{"name":"fresh.txt","size":1024,"chunkSize":0}`)
	if init.Code != http.StatusForbidden {
		t.Fatalf("upload-init with a fresh oversized reservation = %d, want 403: %s", init.Code, init.Body.String())
	}

	var left int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM upload_tasks WHERE id = 'recent-reservation-1'").Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 1 {
		t.Fatalf("a recent reservation must not be reclaimed, left=%d", left)
	}
}
