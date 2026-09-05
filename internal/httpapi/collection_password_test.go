package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestProtectedCollectionRequiresPasswordAcrossAnonymousEndpoints(t *testing.T) {
	db, handler := newTestServer(t)
	ownerToken := testAdminToken(t, handler)
	created := testJSONRequest(t, handler, http.MethodPost, "/api/collections", ownerToken, `{"name":"protected","expiresInHours":24,"password":"collection-secret"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create protected collection = %d: %s", created.Code, created.Body.String())
	}
	collection := responseData(t, created)
	collectionToken := collection["token"].(string)
	if collection["passwordProtected"] != true || strings.Contains(created.Body.String(), "collection-secret") || strings.Contains(created.Body.String(), "passwordHash") {
		t.Fatalf("create response exposed password data: %s", created.Body.String())
	}

	missing := testJSONRequestWithCollectionPassword(t, handler, http.MethodGet, "/api/collections/"+collectionToken+"/meta", "", "")
	wrong := testJSONRequestWithCollectionPassword(t, handler, http.MethodGet, "/api/collections/"+collectionToken+"/meta", "wrong-password", "")
	if missing.Code != http.StatusUnauthorized || wrong.Code != http.StatusUnauthorized || missing.Body.String() != wrong.Body.String() {
		t.Fatalf("missing and wrong password responses differ: missing=%d %s wrong=%d %s", missing.Code, missing.Body.String(), wrong.Code, wrong.Body.String())
	}
	if strings.Contains(missing.Body.String(), "collection-secret") || !strings.Contains(missing.Body.String(), "COLLECTION_UNAUTHORIZED") {
		t.Fatalf("unauthorized response leaked password or stable code: %s", missing.Body.String())
	}

	meta := testJSONRequestWithCollectionPassword(t, handler, http.MethodGet, "/api/collections/"+collectionToken+"/meta", "collection-secret", "")
	if meta.Code != http.StatusOK || responseData(t, meta)["passwordProtected"] != true {
		t.Fatalf("authorized meta = %d: %s", meta.Code, meta.Body.String())
	}
	initBody := `{"name":"protected.txt","size":5,"chunkSize":0}`
	if got := testJSONRequestWithCollectionPassword(t, handler, http.MethodPost, "/api/collections/"+collectionToken+"/upload-init", "wrong-password", initBody); got.Code != http.StatusUnauthorized {
		t.Fatalf("wrong init = %d: %s", got.Code, got.Body.String())
	}
	init := testJSONRequestWithCollectionPassword(t, handler, http.MethodPost, "/api/collections/"+collectionToken+"/upload-init", "collection-secret", initBody)
	if init.Code != http.StatusOK {
		t.Fatalf("authorized init = %d: %s", init.Code, init.Body.String())
	}
	taskID := responseData(t, init)["taskId"].(string)
	base := "/api/collections/" + collectionToken
	checks := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, base + "/upload-status/" + taskID, ""},
		{http.MethodGet, base + "/upload-queue/" + taskID, ""},
		{http.MethodPut, base + "/upload-chunk/" + taskID + "/0", "hello"},
		{http.MethodPost, base + "/upload-complete/" + taskID, `{}`},
	}
	for _, check := range checks {
		got := testRequestWithCollectionPassword(t, handler, check.method, check.path, "wrong-password", []byte(check.body))
		if got.Code != http.StatusUnauthorized || !strings.Contains(got.Body.String(), "COLLECTION_UNAUTHORIZED") {
			t.Fatalf("wrong password %s %s = %d: %s", check.method, check.path, got.Code, got.Body.String())
		}
	}
	status := testRequestWithCollectionPassword(t, handler, http.MethodGet, base+"/upload-status/"+taskID, "collection-secret", nil)
	if status.Code != http.StatusOK {
		t.Fatalf("authorized status = %d: %s", status.Code, status.Body.String())
	}
	queue := testRequestWithCollectionPassword(t, handler, http.MethodGet, base+"/upload-queue/"+taskID, "collection-secret", nil)
	if queue.Code != http.StatusOK {
		t.Fatalf("authorized queue status = %d: %s", queue.Code, queue.Body.String())
	}
	chunk := testRequestWithCollectionPassword(t, handler, http.MethodPut, base+"/upload-chunk/"+taskID+"/0", "collection-secret", []byte("hello"))
	if chunk.Code != http.StatusOK {
		t.Fatalf("authorized chunk = %d: %s", chunk.Code, chunk.Body.String())
	}
	complete := testRequestWithCollectionPassword(t, handler, http.MethodPost, base+"/upload-complete/"+taskID, "collection-secret", []byte(`{}`))
	if complete.Code != http.StatusOK {
		t.Fatalf("authorized complete = %d: %s", complete.Code, complete.Body.String())
	}

	rows, err := db.DB.Query("SELECT username, action, target, reason FROM audit_logs")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var username, action string
		var target, reason sql.NullString
		if err := rows.Scan(&username, &action, &target, &reason); err != nil {
			t.Fatal(err)
		}
		fields := []string{username, action, target.String, reason.String}
		if strings.Contains(strings.Join(fields, "\x00"), "collection-secret") {
			t.Fatalf("audit log leaked collection password: %#v", fields)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestCollectionPasswordCanOnlyBeChangedByOwnerAndCanBeCleared(t *testing.T) {
	db, handler := newTestServer(t)
	ownerToken := testAdminToken(t, handler)
	created := testJSONRequest(t, handler, http.MethodPost, "/api/collections", ownerToken, `{"name":"owner-only","expiresInHours":24}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create collection = %d: %s", created.Code, created.Body.String())
	}
	data := responseData(t, created)
	id := int64(data["id"].(float64))
	collectionToken := data["token"].(string)

	hash, err := bcrypt.GenerateFromPassword([]byte("member-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateUser(context.Background(), "member", string(hash), "user", 1024*1024); err != nil {
		t.Fatal(err)
	}
	memberLogin := testJSONRequest(t, handler, http.MethodPost, "/api/auth/login", "", `{"username":"member","password":"member-password"}`)
	if memberLogin.Code != http.StatusOK {
		t.Fatalf("member login = %d: %s", memberLogin.Code, memberLogin.Body.String())
	}
	memberToken := responseData(t, memberLogin)["token"].(string)
	expiresAt := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	body := func(password string) string {
		return `{"name":"owner-only","expiresAt":"` + expiresAt + `","maxUploads":0,"maxFileBytes":0,"password":` + strconv.Quote(password) + `}`
	}
	denied := testJSONRequest(t, handler, http.MethodPut, "/api/collections/"+strconv.FormatInt(id, 10), memberToken, body("new-password"))
	if denied.Code != http.StatusNotFound {
		t.Fatalf("non-owner password change = %d: %s", denied.Code, denied.Body.String())
	}
	set := testJSONRequest(t, handler, http.MethodPut, "/api/collections/"+strconv.FormatInt(id, 10), ownerToken, body("new-password"))
	if set.Code != http.StatusOK || responseData(t, set)["passwordProtected"] != true {
		t.Fatalf("owner password change = %d: %s", set.Code, set.Body.String())
	}
	protected := testJSONRequestWithCollectionPassword(t, handler, http.MethodGet, "/api/collections/"+collectionToken+"/meta", "new-password", "")
	if protected.Code != http.StatusOK {
		t.Fatalf("new collection password = %d: %s", protected.Code, protected.Body.String())
	}
	clear := testJSONRequest(t, handler, http.MethodPut, "/api/collections/"+strconv.FormatInt(id, 10), ownerToken, body(""))
	if clear.Code != http.StatusOK || responseData(t, clear)["passwordProtected"] != false {
		t.Fatalf("owner clear password = %d: %s", clear.Code, clear.Body.String())
	}
	plain := testJSONRequest(t, handler, http.MethodGet, "/api/collections/"+collectionToken+"/meta", "", "")
	if plain.Code != http.StatusOK {
		t.Fatalf("cleared collection meta = %d: %s", plain.Code, plain.Body.String())
	}
}

func testRequestWithCollectionPassword(t *testing.T, handler http.Handler, method, path, password string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.ContentLength = int64(len(body))
	if password != "" {
		request.Header.Set(collectionPasswordHeader, password)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func testJSONRequestWithCollectionPassword(t *testing.T, handler http.Handler, method, path, password, body string) *httptest.ResponseRecorder {
	t.Helper()
	return testRequestWithCollectionPassword(t, handler, method, path, password, []byte(body))
}
