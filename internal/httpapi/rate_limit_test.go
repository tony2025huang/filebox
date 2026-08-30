package httpapi

import (
	"net/http"
	"strconv"
	"testing"
)

func TestPublicShareMetaRateLimitedByIP(t *testing.T) {
	_, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)
	file := uploadTestFile(t, handler, adminToken, "rate-limited-share.txt", "text/plain", []byte("shared content"))
	fileID := int64(file["id"].(float64))
	created := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+strconv.FormatInt(fileID, 10)+"/share", adminToken, `{"expiresInHours":1,"maxDownloads":0}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create share status = %d: %s", created.Code, created.Body.String())
	}
	shareToken := responseData(t, created)["token"].(string)

	limited := false
	for i := 0; i < 40; i++ {
		meta := testJSONRequest(t, handler, http.MethodGet, "/api/files/shared/"+shareToken+"/meta", "", "")
		if i == 0 && meta.Code != http.StatusOK {
			t.Fatalf("first share meta status = %d: %s", meta.Code, meta.Body.String())
		}
		if i > 0 && meta.Code == http.StatusTooManyRequests {
			limited = true
		}
	}
	if !limited {
		t.Fatal("share meta was not rate limited")
	}
}

func TestPublicCollectionMetaRateLimitedByIP(t *testing.T) {
	_, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)
	created := testJSONRequest(t, handler, http.MethodPost, "/api/collections", adminToken, `{"name":"c","expiresInHours":1}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create collection status = %d: %s", created.Code, created.Body.String())
	}
	collectionToken := responseData(t, created)["token"].(string)

	limited := false
	for i := 0; i < 40; i++ {
		meta := testJSONRequest(t, handler, http.MethodGet, "/api/collections/"+collectionToken+"/meta", "", "")
		if i == 0 && meta.Code != http.StatusOK {
			t.Fatalf("first collection meta status = %d: %s", meta.Code, meta.Body.String())
		}
		if i > 0 && meta.Code == http.StatusTooManyRequests {
			limited = true
		}
	}
	if !limited {
		t.Fatal("collection meta was not rate limited")
	}
}
