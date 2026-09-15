package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestSettingsHashLimitsRoundTrip 验证两个哈希阈值能通过设置接口写入、读回，且部分更新不会重置它们。
func TestSettingsHashLimitsRoundTrip(t *testing.T) {
	_, handler := newTestServer(t)
	token := testAdminToken(t, handler)

	const direct = 512 << 20
	const client = 4 << 30
	update := testJSONRequest(t, handler, http.MethodPut, "/api/admin/settings", token, `{"hashDirectLimitBytes":536870912,"hashClientLimitBytes":4294967296}`)
	if update.Code != http.StatusOK {
		t.Fatalf("hash limit update status = %d, want 200: %s", update.Code, update.Body.String())
	}
	settings := responseData(t, update)
	if settings["hashDirectLimitBytes"] != float64(direct) || settings["hashClientLimitBytes"] != float64(client) {
		t.Fatalf("update response hash limits = (%#v, %#v), want (%d, %d)", settings["hashDirectLimitBytes"], settings["hashClientLimitBytes"], direct, client)
	}

	// 只改一个无关字段时，阈值必须保持原值。
	partial := testJSONRequest(t, handler, http.MethodPut, "/api/admin/settings", token, `{"lockThreshold":7}`)
	if partial.Code != http.StatusOK {
		t.Fatalf("partial settings update status = %d, want 200: %s", partial.Code, partial.Body.String())
	}
	partialData := responseData(t, partial)
	if partialData["hashDirectLimitBytes"] != float64(direct) || partialData["hashClientLimitBytes"] != float64(client) {
		t.Fatalf("partial update hash limits = (%#v, %#v), want (%d, %d)", partialData["hashDirectLimitBytes"], partialData["hashClientLimitBytes"], direct, client)
	}

	fetched := testJSONRequest(t, handler, http.MethodGet, "/api/admin/settings", token, "")
	if fetched.Code != http.StatusOK {
		t.Fatalf("get settings status = %d, want 200: %s", fetched.Code, fetched.Body.String())
	}
	fetchedData := responseData(t, fetched)
	if fetchedData["hashDirectLimitBytes"] != float64(direct) || fetchedData["hashClientLimitBytes"] != float64(client) {
		t.Fatalf("persisted hash limits = (%#v, %#v), want (%d, %d)", fetchedData["hashDirectLimitBytes"], fetchedData["hashClientLimitBytes"], direct, client)
	}
}

// TestSettingsHashLimitsValidation 验证越界阈值与"直算上限大于总上限"被拒绝，且拒绝后不落地。
func TestSettingsHashLimitsValidation(t *testing.T) {
	_, handler := newTestServer(t)
	token := testAdminToken(t, handler)

	cases := []struct {
		name        string
		body        string
		wantMessage string
	}{
		{"direct below minimum", `{"hashDirectLimitBytes":1}`, "校验直算上限无效"},
		{"direct above maximum", `{"hashDirectLimitBytes":8589934592}`, "校验直算上限无效"},
		{"client below minimum", `{"hashClientLimitBytes":1}`, "客户端哈希上限无效"},
		{"client above maximum", `{"hashClientLimitBytes":137438953472}`, "客户端哈希上限无效"},
		{"direct above client", `{"hashDirectLimitBytes":1073741824,"hashClientLimitBytes":268435456}`, "校验直算上限不能大于客户端哈希上限"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rejected := testJSONRequest(t, handler, http.MethodPut, "/api/admin/settings", token, tc.body)
			if rejected.Code != http.StatusBadRequest {
				t.Fatalf("invalid hash limits status = %d, want 400: %s", rejected.Code, rejected.Body.String())
			}
			var body response
			if err := json.Unmarshal(rejected.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Message != tc.wantMessage {
				t.Fatalf("invalid hash limits message = %q, want %q", body.Message, tc.wantMessage)
			}
		})
	}

	// 上述请求全部被拒，阈值应仍是默认值。
	settings := responseData(t, testJSONRequest(t, handler, http.MethodGet, "/api/admin/settings", token, ""))
	if settings["hashDirectLimitBytes"] != float64(256<<20) || settings["hashClientLimitBytes"] != float64(1<<30) {
		t.Fatalf("hash limits changed after rejected updates: (%#v, %#v)", settings["hashDirectLimitBytes"], settings["hashClientLimitBytes"])
	}
}

// TestPublicBrandExposesEffectiveHashLimits 验证未登录请求也能拿到生效阈值——收集上传页依赖这个公开通道。
func TestPublicBrandExposesEffectiveHashLimits(t *testing.T) {
	_, handler := newTestServer(t)

	initial := responseData(t, testJSONRequest(t, handler, http.MethodGet, "/api/brand", "", ""))
	if initial["hashDirectLimitBytes"] != float64(256<<20) || initial["hashClientLimitBytes"] != float64(1<<30) {
		t.Fatalf("default public hash limits = (%#v, %#v), want (%d, %d)", initial["hashDirectLimitBytes"], initial["hashClientLimitBytes"], 256<<20, 1<<30)
	}

	token := testAdminToken(t, handler)
	update := testJSONRequest(t, handler, http.MethodPut, "/api/admin/settings", token, `{"hashDirectLimitBytes":536870912,"hashClientLimitBytes":4294967296}`)
	if update.Code != http.StatusOK {
		t.Fatalf("hash limit update status = %d, want 200: %s", update.Code, update.Body.String())
	}

	published := responseData(t, testJSONRequest(t, handler, http.MethodGet, "/api/brand", "", ""))
	if published["hashDirectLimitBytes"] != float64(512<<20) || published["hashClientLimitBytes"] != float64(4<<30) {
		t.Fatalf("public hash limits = (%#v, %#v), want (%d, %d)", published["hashDirectLimitBytes"], published["hashClientLimitBytes"], 512<<20, 4<<30)
	}
}
