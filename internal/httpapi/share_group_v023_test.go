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

func createTestShareGroup(t *testing.T, db *store.Store, expiresAt, token string) int64 {
	t.Helper()
	user, err := db.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	result, err := db.DB.Exec("INSERT INTO files(user_id, name, stored_name, size, mime, sha256, md5, status, storage_path, created_at) VALUES(?, ?, ?, 1, 'text/plain', 'sha', 'md5', 'ready', ?, ?)", user.ID, token+".txt", token+".txt", "files/"+strconv.FormatInt(user.ID, 10)+"/"+token+".txt", time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	fileID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.CreateShareGroup(context.Background(), user.ID, token, []int64{fileID}, expiresAt, 0); err != nil {
		t.Fatal(err)
	}
	return fileID
}

func TestShareGroupMetaRejectsExpiredGroup(t *testing.T) {
	db, handler := newTestServer(t)
	createTestShareGroup(t, db, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), "expired-meta-group")

	meta := testJSONRequest(t, handler, http.MethodGet, "/api/shared-groups/expired-meta-group/meta", "", "")
	if meta.Code != http.StatusNotFound {
		t.Fatalf("expired share group meta = %d: %s", meta.Code, meta.Body.String())
	}
	if !strings.Contains(meta.Body.String(), "分享不存在或已过期") {
		t.Fatalf("expired share group meta message = %s", meta.Body.String())
	}
	if strings.Contains(meta.Body.String(), "files") || strings.Contains(meta.Body.String(), "expired-meta-group.txt") {
		t.Fatalf("expired share group meta leaked member data: %s", meta.Body.String())
	}
}

func TestShareGroupMetaActiveGroupStillReturnsMembers(t *testing.T) {
	db, handler := newTestServer(t)
	createTestShareGroup(t, db, time.Now().UTC().Add(time.Hour).Format(time.RFC3339), "active-meta-group")

	meta := testJSONRequest(t, handler, http.MethodGet, "/api/shared-groups/active-meta-group/meta", "", "")
	if meta.Code != http.StatusOK {
		t.Fatalf("active share group meta = %d: %s", meta.Code, meta.Body.String())
	}
	files, ok := responseData(t, meta)["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("active share group files = %#v, want one member", responseData(t, meta)["files"])
	}
}

func TestShareGroupDownloadsRejectExpiredGroup(t *testing.T) {
	db, handler := newTestServer(t)
	fileID := createTestShareGroup(t, db, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), "expired-download-group")

	download := testJSONRequest(t, handler, http.MethodGet, "/api/shared-groups/expired-download-group/download/"+strconv.FormatInt(fileID, 10), "", "")
	if download.Code != http.StatusForbidden {
		t.Fatalf("expired share group download = %d: %s", download.Code, download.Body.String())
	}
}
