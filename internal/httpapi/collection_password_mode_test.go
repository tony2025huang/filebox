package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// responseData/token helpers are shared from server_test.go.

func createCollectionWithMode(t *testing.T, handler http.Handler, ownerToken, mode, password string) map[string]any {
	t.Helper()
	body := `{"name":"mode-collection","expiresInHours":24,"passwordMode":` + strconv.Quote(mode)
	if mode == collectionPasswordModeManual || (mode == "" && password != "") {
		body += `,"password":` + strconv.Quote(password)
	}
	body += `}`
	rec := testJSONRequest(t, handler, http.MethodPost, "/api/collections", ownerToken, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create collection (mode=%q) = %d: %s", mode, rec.Code, rec.Body.String())
	}
	return responseData(t, rec)
}

func updateCollectionPasswordMode(t *testing.T, handler http.Handler, ownerToken string, id int64, mode, password string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"name":"mode-collection","expiresAt":"` + time.Now().UTC().Add(time.Hour).Format(time.RFC3339) + `","passwordMode":` + strconv.Quote(mode)
	if mode == collectionPasswordModeManual || (mode == "" && password != "") {
		body += `,"password":` + strconv.Quote(password)
	}
	body += `}`
	return testJSONRequest(t, handler, http.MethodPut, "/api/collections/"+strconv.FormatInt(id, 10), ownerToken, body)
}

// TestCollectionRandomPasswordRevealOnceAndNeverListed verifies that random mode
// returns the plaintext password and a fragment passwordUrl exactly once, while
// DB/list/get never expose the plaintext afterwards.
func TestCollectionRandomPasswordRevealOnceAndNeverListed(t *testing.T) {
	db, handler := newTestServer(t)
	ownerToken := testAdminToken(t, handler)
	created := createCollectionWithMode(t, handler, ownerToken, collectionPasswordModeRandom, "")
	password, _ := created["password"].(string)
	passwordURL, _ := created["passwordUrl"].(string)
	if password == "" || created["passwordProtected"] != true {
		t.Fatalf("random mode must reveal a password and be protected: %#v", created)
	}
	if !strings.Contains(passwordURL, "#password=") || strings.Contains(passwordURL, "?password=") {
		t.Fatalf("passwordUrl must use fragment only, got %q", passwordURL)
	}
	unescaped := strings.SplitN(passwordURL, "#password=", 2)[1]
	if decoded, err := url.QueryUnescape(unescaped); err != nil || decoded != password {
		t.Fatalf("passwordUrl fragment must carry the revealed password: url=%q password=%q decoded=%q err=%v", passwordURL, password, decoded, err)
	}
	collectionToken := created["token"].(string)
	id := int64(created["id"].(float64))

	// DB stores only the bcrypt hash, never the plaintext.
	var stored string
	if err := db.DB.QueryRow("SELECT password_hash FROM upload_collections WHERE id = ?", id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == "" || stored == password || strings.Contains(stored, password) {
		t.Fatalf("DB must store only a bcrypt hash: %q", stored)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(password)); err != nil {
		t.Fatalf("stored hash must match the revealed password")
	}

	// Listing and owner detail never include plaintext or the fragment URL.
	list := testJSONRequest(t, handler, http.MethodGet, "/api/collections?page=1&pageSize=100", ownerToken, "")
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), password) || strings.Contains(list.Body.String(), "#password=") {
		t.Fatalf("list must never reveal the plaintext: %s", list.Body.String())
	}
	detail := testJSONRequest(t, handler, http.MethodGet, "/api/collections/"+strconv.FormatInt(id, 10), ownerToken, "")
	if detail.Code != http.StatusOK || strings.Contains(detail.Body.String(), password) || strings.Contains(detail.Body.String(), "#password=") {
		t.Fatalf("owner detail must never reveal the plaintext: %s", detail.Body.String())
	}

	// The password actually protects the anonymous meta endpoint.
	open := testJSONRequest(t, handler, http.MethodGet, "/api/collections/"+collectionToken+"/meta", "", "")
	if open.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous meta without password = %d", open.Code)
	}
	authed := testJSONRequestWithCollectionPassword(t, handler, http.MethodGet, "/api/collections/"+collectionToken+"/meta", password, "")
	if authed.Code != http.StatusOK {
		t.Fatalf("anonymous meta with revealed password = %d: %s", authed.Code, authed.Body.String())
	}
}

// TestCollectionManualPasswordMode verifies manual mode accepts the supplied
// password, reveals it once, and rejects an empty manual password.
func TestCollectionManualPasswordMode(t *testing.T) {
	_, handler := newTestServer(t)
	ownerToken := testAdminToken(t, handler)
	created := createCollectionWithMode(t, handler, ownerToken, collectionPasswordModeManual, "owner-typed-secret")
	if password, _ := created["password"].(string); password != "owner-typed-secret" {
		t.Fatalf("manual mode must reveal the exact supplied password: %#v", created)
	}
	if created["passwordProtected"] != true {
		t.Fatalf("manual mode must protect the collection")
	}
	rejected := testJSONRequest(t, handler, http.MethodPost, "/api/collections", ownerToken, `{"name":"bad","expiresInHours":24,"passwordMode":"manual"}`)
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("manual mode with empty password must be rejected, got %d: %s", rejected.Code, rejected.Body.String())
	}
}

// TestCollectionNoneAndInvalidModes verifies no-password mode and malformed modes.
func TestCollectionNoneAndInvalidModes(t *testing.T) {
	_, handler := newTestServer(t)
	ownerToken := testAdminToken(t, handler)
	created := createCollectionWithMode(t, handler, ownerToken, collectionPasswordModeNone, "")
	if created["passwordProtected"] == true {
		t.Fatalf("none mode must leave the collection open")
	}
	if _, has := created["password"]; has {
		t.Fatalf("none mode must not reveal a password")
	}
	for _, badMode := range []string{"bogus", "RANDOM", "manual "} {
		rec := testJSONRequest(t, handler, http.MethodPost, "/api/collections", ownerToken, `{"name":"bad","expiresInHours":24,"passwordMode":`+strconv.Quote(badMode)+`}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("mode %q must be rejected, got %d: %s", badMode, rec.Code, rec.Body.String())
		}
	}
	// none with a non-empty legacy password is an invalid combination.
	conflict := testJSONRequest(t, handler, http.MethodPost, "/api/collections", ownerToken, `{"name":"bad","expiresInHours":24,"passwordMode":"none","password":"x"}`)
	if conflict.Code != http.StatusBadRequest {
		t.Fatalf("none mode with a password must be rejected, got %d", conflict.Code)
	}
}

// TestCollectionUpdatePasswordModes covers random/manual/none/keep and legacy
// clear behavior on PUT, plus owner-only enforcement.
func TestCollectionUpdatePasswordModes(t *testing.T) {
	db, handler := newTestServer(t)
	ownerToken := testAdminToken(t, handler)
	// Start with a random password so each update has a baseline.
	created := createCollectionWithMode(t, handler, ownerToken, collectionPasswordModeRandom, "")
	id := int64(created["id"].(float64))
	token := created["token"].(string)
	firstPassword, _ := created["password"].(string)

	// keep: nothing changes and no reveal is produced.
	keep := updateCollectionPasswordMode(t, handler, ownerToken, id, collectionPasswordModeKeep, "")
	if keep.Code != http.StatusOK {
		t.Fatalf("keep update = %d: %s", keep.Code, keep.Body.String())
	}
	keepData := responseData(t, keep)
	if _, has := keepData["password"]; has {
		t.Fatalf("keep must not reveal a password: %s", keep.Body.String())
	}
	if got := testJSONRequestWithCollectionPassword(t, handler, http.MethodGet, "/api/collections/"+token+"/meta", firstPassword, ""); got.Code != http.StatusOK {
		t.Fatalf("first password should still work after keep: %d", got.Code)
	}

	// manual: sets an explicit password and reveals it once.
	manual := updateCollectionPasswordMode(t, handler, ownerToken, id, collectionPasswordModeManual, "updated-secret")
	if manual.Code != http.StatusOK {
		t.Fatalf("manual update = %d: %s", manual.Code, manual.Body.String())
	}
	if password, _ := responseData(t, manual)["password"].(string); password != "updated-secret" {
		t.Fatalf("manual update must reveal supplied password: %s", manual.Body.String())
	}
	if got := testJSONRequestWithCollectionPassword(t, handler, http.MethodGet, "/api/collections/"+token+"/meta", "updated-secret", ""); got.Code != http.StatusOK {
		t.Fatalf("updated password should unlock meta: %d", got.Code)
	}
	if got := testJSONRequestWithCollectionPassword(t, handler, http.MethodGet, "/api/collections/"+token+"/meta", firstPassword, ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("old password must stop working after manual update: %d", got.Code)
	}

	// none: clears the password (no reveal).
	none := updateCollectionPasswordMode(t, handler, ownerToken, id, collectionPasswordModeNone, "")
	if none.Code != http.StatusOK {
		t.Fatalf("none update = %d: %s", none.Code, none.Body.String())
	}
	if responseData(t, none)["passwordProtected"] == true {
		t.Fatalf("none update must clear protection: %s", none.Body.String())
	}
	if _, has := responseData(t, none)["password"]; has {
		t.Fatalf("none must not reveal a password")
	}
	if got := testJSONRequest(t, handler, http.MethodGet, "/api/collections/"+token+"/meta", "", ""); got.Code != http.StatusOK {
		t.Fatalf("cleared collection meta must be open: %d", got.Code)
	}

	// Legacy path (no passwordMode): an explicit empty password clears.
	if err := db.CreateUser(context.Background(), "mode-owner", string(testPasswordHashFor("mode-owner-pass")), "user", 1024*1024); err != nil {
		t.Fatal(err)
	}
	legacySet := updateCollectionPasswordMode(t, handler, ownerToken, id, "", "legacy-secret")
	if legacySet.Code != http.StatusOK || responseData(t, legacySet)["passwordProtected"] != true {
		t.Fatalf("legacy set = %d: %s", legacySet.Code, legacySet.Body.String())
	}
	if _, has := responseData(t, legacySet)["password"]; has {
		t.Fatalf("legacy update must not reveal a password")
	}
	// A non-owner cannot change the password (owner-only).
	otherLogin := testJSONRequest(t, handler, http.MethodPost, "/api/auth/login", "", `{"username":"mode-owner","password":"mode-owner-pass"}`)
	otherToken := responseData(t, otherLogin)["token"].(string)
	denied := updateCollectionPasswordMode(t, handler, otherToken, id, collectionPasswordModeRandom, "")
	if denied.Code != http.StatusNotFound {
		t.Fatalf("non-owner password change must be 404, got %d: %s", denied.Code, denied.Body.String())
	}

	// Legacy clear semantics: an explicit empty password removes protection.
	legacyClear := testJSONRequest(t, handler, http.MethodPut, "/api/collections/"+strconv.FormatInt(id, 10), ownerToken, `{"name":"mode-collection","expiresAt":"`+time.Now().UTC().Add(time.Hour).Format(time.RFC3339)+`","password":""}`)
	if legacyClear.Code != http.StatusOK || responseData(t, legacyClear)["passwordProtected"] == true {
		t.Fatalf("legacy clear = %d: %s", legacyClear.Code, legacyClear.Body.String())
	}
}

// TestCollectionRandomUpdateRevealOnce verifies random update regenerates and the
// new password replaces the old one while DB stays hashed-only.
func TestCollectionRandomUpdateRevealOnce(t *testing.T) {
	db, handler := newTestServer(t)
	ownerToken := testAdminToken(t, handler)
	created := createCollectionWithMode(t, handler, ownerToken, collectionPasswordModeNone, "")
	id := int64(created["id"].(float64))
	token := created["token"].(string)

	randomUpdate := updateCollectionPasswordMode(t, handler, ownerToken, id, collectionPasswordModeRandom, "")
	if randomUpdate.Code != http.StatusOK {
		t.Fatalf("random update = %d: %s", randomUpdate.Code, randomUpdate.Body.String())
	}
	password, _ := responseData(t, randomUpdate)["password"].(string)
	if password == "" || responseData(t, randomUpdate)["passwordProtected"] != true {
		t.Fatalf("random update must protect and reveal: %s", randomUpdate.Body.String())
	}
	var stored string
	if err := db.DB.QueryRow("SELECT password_hash FROM upload_collections WHERE id = ?", id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(password)); err != nil {
		t.Fatalf("DB hash must match the new random password")
	}
	if got := testJSONRequestWithCollectionPassword(t, handler, http.MethodGet, "/api/collections/"+token+"/meta", password, ""); got.Code != http.StatusOK {
		t.Fatalf("new random password should unlock meta: %d", got.Code)
	}
}

// testPasswordHashFor builds a bcrypt hash for helper users.
func testPasswordHashFor(password string) []byte {
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	return hash
}
