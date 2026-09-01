package httpapi

import (
	"net/http"
	"strconv"
	"testing"
	"time"
)

// TestShareGroupMemberManagementAndEdit 验证聚合分享成员增删与属性编辑 API（v018 #2）。
// TestShareGroupMemberManagementAndEdit verifies the aggregate-share member and attribute-edit APIs (v018 #2).
func TestShareGroupMemberManagementAndEdit(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	fileIDs := make([]int64, 0, 2)
	for _, name := range []string{"m1.txt", "m2.txt"} {
		initData := initUpload(t, handler, token, name, 1, 1, "")
		taskID := initData["taskId"].(string)
		if chunk := testJSONRequest(t, handler, http.MethodPut, "/api/files/"+taskID+"/chunks/0", token, "x"); chunk.Code != http.StatusOK {
			t.Fatalf("chunk = %d: %s", chunk.Code, chunk.Body.String())
		}
		complete := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+taskID+"/complete", token, "{}")
		if complete.Code != http.StatusOK {
			t.Fatalf("complete = %d: %s", complete.Code, complete.Body.String())
		}
		fileIDs = append(fileIDs, int64(responseData(t, complete)["id"].(float64)))
	}
	create := testJSONRequest(t, handler, http.MethodPost, "/api/files/batch-share-group", token, `{"fileIds":[`+strconv.FormatInt(fileIDs[0], 10)+`],"expiresInHours":24,"maxDownloads":2}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create group = %d: %s", create.Code, create.Body.String())
	}
	tok := responseData(t, create)["token"].(string)

	list := testJSONRequest(t, handler, http.MethodGet, "/api/shared-groups/"+tok+"/files", token, "")
	if list.Code != http.StatusOK {
		t.Fatalf("list files = %d: %s", list.Code, list.Body.String())
	}
	if items := responseData(t, list)["items"].([]any); len(items) != 1 {
		t.Fatalf("member files = %d, want 1", len(items))
	}
	// 添加成员 → 2 个；重复添加 → 400。
	add := testJSONRequest(t, handler, http.MethodPost, "/api/shared-groups/"+tok+"/files", token, `{"fileIds":[`+strconv.FormatInt(fileIDs[1], 10)+`]}`)
	if add.Code != http.StatusOK {
		t.Fatalf("add file = %d: %s", add.Code, add.Body.String())
	}
	list2 := testJSONRequest(t, handler, http.MethodGet, "/api/shared-groups/"+tok+"/files", token, "")
	if items := responseData(t, list2)["items"].([]any); len(items) != 2 {
		t.Fatalf("member files after add = %d, want 2", len(items))
	}
	dup := testJSONRequest(t, handler, http.MethodPost, "/api/shared-groups/"+tok+"/files", token, `{"fileIds":[`+strconv.FormatInt(fileIDs[1], 10)+`]}`)
	if dup.Code != http.StatusBadRequest {
		t.Fatalf("duplicate add = %d: %s", dup.Code, dup.Body.String())
	}
	// 移除成员 → 1 个。
	rem := testJSONRequest(t, handler, http.MethodDelete, "/api/shared-groups/"+tok+"/files/"+strconv.FormatInt(fileIDs[1], 10), token, "")
	if rem.Code != http.StatusOK {
		t.Fatalf("remove file = %d: %s", rem.Code, rem.Body.String())
	}
	list3 := testJSONRequest(t, handler, http.MethodGet, "/api/shared-groups/"+tok+"/files", token, "")
	if items := responseData(t, list3)["items"].([]any); len(items) != 1 {
		t.Fatalf("member files after remove = %d, want 1", len(items))
	}
	// 属性编辑：未来时间 + 新上限；过去时间 → 400。
	future := time.Now().UTC().Add(72 * time.Hour).Format(time.RFC3339)
	edit := testJSONRequest(t, handler, http.MethodPut, "/api/shared-groups/"+tok, token, `{"expiresAt":"`+future+`","maxDownloads":9}`)
	if edit.Code != http.StatusOK {
		t.Fatalf("edit group = %d: %s", edit.Code, edit.Body.String())
	}
	if responseData(t, edit)["maxDownloads"] != float64(9) {
		t.Fatalf("edit maxDownloads = %#v", responseData(t, edit)["maxDownloads"])
	}
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	bad := testJSONRequest(t, handler, http.MethodPut, "/api/shared-groups/"+tok, token, `{"expiresAt":"`+past+`","maxDownloads":9}`)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("past expiry = %d: %s", bad.Code, bad.Body.String())
	}
	// 越权：其他用户列表 → 404。
	other := testJSONRequest(t, handler, http.MethodPost, "/api/admin/users", token, `{"username":"v018-other","password":"Other123!","role":"user","quotaBytes":1024}`)
	if other.Code != http.StatusCreated {
		t.Fatalf("create other = %d: %s", other.Code, other.Body.String())
	}
	otherLogin := testJSONRequest(t, handler, http.MethodPost, "/api/auth/login", "", `{"username":"v018-other","password":"Other123!"}`)
	otherToken := responseData(t, otherLogin)["token"].(string)
	if cross := testJSONRequest(t, handler, http.MethodGet, "/api/shared-groups/"+tok+"/files", otherToken, ""); cross.Code != http.StatusNotFound {
		t.Fatalf("cross-owner list = %d: %s", cross.Code, cross.Body.String())
	}
	_ = db
}

// TestPaginationCapsPageSize 验证 pagination() 把 pageSize 截断到上限 100（v018 #7）。
// TestPaginationCapsPageSize verifies pagination() caps pageSize at 100 (v018 #7).
func TestPaginationCapsPageSize(t *testing.T) {
	req := func(query string) *http.Request {
		r, err := http.NewRequest(http.MethodGet, "/api/x?"+query, nil)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if _, size := pagination(req("page=1&pageSize=500")); size != 100 {
		t.Fatalf("pageSize cap = %d, want 100", size)
	}
	if _, size := pagination(req("pageSize=50")); size != 50 {
		t.Fatalf("pageSize 50 = %d, want 50", size)
	}
	if page, size := pagination(req("page=2&pageSize=20")); page != 2 || size != 20 {
		t.Fatalf("pagination = %d/%d, want 2/20", page, size)
	}
}

// TestNextSyncRunTime 验证周期任务下次执行时间计算（v018 #5）。
// TestNextSyncRunTime verifies the next-run-time calculation for periodic tasks (v018 #5).
func TestNextSyncRunTime(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	if value := nextSyncRunTime("once", "", now); value != "" {
		t.Fatalf("once next run = %q, want empty", value)
	}
	if value := nextSyncRunTime("periodic", "", now); value != "" {
		t.Fatalf("empty cron next run = %q, want empty", value)
	}
	if value := nextSyncRunTime("periodic", "not-a-cron", now); value != "" {
		t.Fatalf("invalid cron next run = %q, want empty", value)
	}
	value := nextSyncRunTime("periodic", "0 3 * * *", now)
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("next run parse: %v", err)
	}
	if !parsed.After(now) {
		t.Fatalf("next run %s not after now", value)
	}
	if parsed.Hour() != 3 {
		t.Fatalf("next run hour = %d, want 3", parsed.Hour())
	}
}
