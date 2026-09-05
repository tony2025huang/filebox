package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"filebox/internal/store"
)

func TestCollectionQueuedTaskAPIAndAnonymousStateIsolation(t *testing.T) {
	db, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)
	created := testJSONRequest(t, handler, http.MethodPost, "/api/collections", adminToken, `{"name":"queue-api","expiresInHours":24}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create collection = %d: %s", created.Code, created.Body.String())
	}
	collection := responseData(t, created)
	collectionID := int64(collection["id"].(float64))
	collectionToken := collection["token"].(string)
	ctx := context.Background()
	for i := 0; i < store.MaxPendingCollectionUploadTasks; i++ {
		if err := db.CreateCollectionUploadTask(ctx, store.UploadTask{
			ID: "http-queue-active-" + strconv.Itoa(i), UserID: 1, CollectionID: collectionID,
			Name: "active", Size: 1, ChunkSize: 1, TotalChunks: 1, Status: "pending", Mime: "text/plain",
		}, collectionToken); err != nil {
			t.Fatal(err)
		}
	}
	init := testJSONRequest(t, handler, http.MethodPost, "/api/collections/"+collectionToken+"/upload-init", "", `{"name":"queued.txt","size":1,"chunkSize":0}`)
	if init.Code != http.StatusOK {
		t.Fatalf("queued init = %d: %s", init.Code, init.Body.String())
	}
	taskID := responseData(t, init)["taskId"].(string)
	state := testJSONRequest(t, handler, http.MethodGet, "/api/collections/"+collectionToken+"/upload-queue/"+taskID, "", "")
	if state.Code != http.StatusOK {
		t.Fatalf("queued state = %d: %s", state.Code, state.Body.String())
	}
	stateData := responseData(t, state)
	if stateData["taskId"] != taskID || stateData["state"] != "queued" || stateData["queuePosition"] != float64(1) || stateData["waitReason"] != nil {
		t.Fatalf("queued state data = %#v", stateData)
	}
	if _, ok := stateData["name"]; ok {
		t.Fatalf("queued state leaked task name: %#v", stateData)
	}
	for _, request := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/collections/" + collectionToken + "/upload-status/" + taskID, ""},
		{http.MethodPost, "/api/collections/" + collectionToken + "/upload-complete/" + taskID, "{}"},
	} {
		got := testJSONRequest(t, handler, request.method, request.path, "", request.body)
		if got.Code != http.StatusConflict || responseData(t, got)["code"] != "COLLECTION_TASK_QUEUED" {
			t.Fatalf("queued %s = %d: %s", request.path, got.Code, got.Body.String())
		}
	}
	chunk := testBinaryRequest(t, handler, http.MethodPut, "/api/collections/"+collectionToken+"/upload-chunk/"+taskID+"/0", "", []byte("x"))
	if chunk.Code != http.StatusConflict || responseData(t, chunk)["code"] != "COLLECTION_TASK_QUEUED" {
		t.Fatalf("queued chunk = %d: %s", chunk.Code, chunk.Body.String())
	}

	other := testJSONRequest(t, handler, http.MethodPost, "/api/collections", adminToken, `{"name":"other-queue-api","expiresInHours":24}`)
	if other.Code != http.StatusCreated {
		t.Fatal(other.Body.String())
	}
	otherToken := responseData(t, other)["token"].(string)
	cross := testJSONRequest(t, handler, http.MethodGet, "/api/collections/"+otherToken+"/upload-queue/"+taskID, "", "")
	if cross.Code != http.StatusNotFound {
		t.Fatalf("cross-collection state = %d: %s", cross.Code, cross.Body.String())
	}

	activeState := testJSONRequest(t, handler, http.MethodGet, "/api/collections/"+collectionToken+"/upload-queue/http-queue-active-0", "", "")
	if activeState.Code != http.StatusOK || responseData(t, activeState)["state"] != "active" {
		t.Fatalf("active state = %d: %s", activeState.Code, activeState.Body.String())
	}
}
