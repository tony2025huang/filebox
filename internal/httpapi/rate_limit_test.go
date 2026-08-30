package httpapi

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestRateLimiterEvictsOldestWhenFull(t *testing.T) {
	// 容量上限：超出后驱逐最久未访问的键，防止来源键无限增长（G8）。
	// Capacity cap: past the limit the least-recently-seen key is evicted so source keys cannot grow unbounded (G8).
	now := time.Now()
	lastSeen := map[string]time.Time{}
	buckets := map[string]*rate.Limiter{}
	for index := 0; index < rateLimiterMaxKeys; index++ {
		key := "ip-" + strconv.Itoa(index)
		lastSeen[key] = now.Add(-time.Duration(index) * time.Second)
		buckets[key] = rate.NewLimiter(1, 1)
	}
	// 最久未访问的是 ip-0（被改为 10 小时前，早于循环里最旧的 ip-9999 ≈ 2.78 小时前）。
	// The least-recently-seen key is ip-0 (ten hours ago, older than the loop's oldest ip-9999 at ~2.78 hours).
	lastSeen["ip-0"] = now.Add(-10 * time.Hour)
	lastSeen["ip-new"] = now
	buckets["ip-new"] = rate.NewLimiter(1, 1)
	evictIfFull(now, lastSeen, buckets)
	if _, ok := buckets["ip-0"]; ok {
		t.Fatal("oldest bucket was not evicted")
	}
	if len(buckets) != rateLimiterMaxKeys {
		t.Fatalf("bucket count after eviction = %d, want %d", len(buckets), rateLimiterMaxKeys)
	}
	// 达到上限后每次调用驱逐一个最旧键，为后续插入腾出容量。
	// Past the cap each call evicts one oldest key to make room for the next insert.
	evictIfFull(now, lastSeen, buckets)
	if len(buckets) != rateLimiterMaxKeys-1 {
		t.Fatalf("bucket count after second eviction = %d, want %d", len(buckets), rateLimiterMaxKeys-1)
	}
	if _, ok := buckets["ip-new"]; !ok {
		t.Fatal("newest bucket was evicted while older keys remain")
	}
}

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

func TestPublicSharePreviewRateLimitedByIP(t *testing.T) {
	_, handler := newTestServer(t)
	adminToken := testAdminToken(t, handler)
	file := uploadTestFile(t, handler, adminToken, "rate-limited-preview.txt", "text/plain", []byte("preview content"))
	fileID := int64(file["id"].(float64))
	created := testJSONRequest(t, handler, http.MethodPost, "/api/files/"+strconv.FormatInt(fileID, 10)+"/share", adminToken, `{"expiresInHours":1,"maxDownloads":0}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create share status = %d: %s", created.Code, created.Body.String())
	}
	shareToken := responseData(t, created)["token"].(string)

	limited := false
	for i := 0; i < 40; i++ {
		preview := testJSONRequest(t, handler, http.MethodGet, "/api/files/shared/"+shareToken+"/preview", "", "")
		if i == 0 && preview.Code != http.StatusOK {
			t.Fatalf("first share preview status = %d: %s", preview.Code, preview.Body.String())
		}
		if i > 0 && preview.Code == http.StatusTooManyRequests {
			limited = true
		}
	}
	if !limited {
		t.Fatal("share preview was not rate limited")
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
