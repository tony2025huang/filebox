package httpapi

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// createProtectedCollectionForTest creates a password-protected collection; protected
// collections bypass the anonymous per-IP rate limiter so dense validation matrices do not
// hit 429 while still exercising the same upload-init dir code path.
func createProtectedCollectionForTest(t *testing.T, handler http.Handler, ownerToken, name string) string {
	t.Helper()
	created := testJSONRequest(t, handler, http.MethodPost, "/api/collections", ownerToken, `{"name":"`+name+`","expiresInHours":24,"password":"test-secret"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create collection = %d: %s", created.Code, created.Body.String())
	}
	return responseData(t, created)["token"].(string)
}

// TestCollectionUploadDirUsesSharedValidation confirms collection upload-init accepts the
// same nested dir grammar as private /api/files/upload-init and rejects the same traversal
// markers: absolute/volume paths, .., backslashes, control chars, and Windows reserved chars.
func TestCollectionUploadDirUsesSharedValidation(t *testing.T) {
	db, handler := newTestServer(t)
	ownerToken := testAdminToken(t, handler)
	token := createProtectedCollectionForTest(t, handler, ownerToken, "dir-uploads")
	initBody := func(dir string) string {
		return `{"name":"file.txt","size":1,"chunkSize":0,"dir":` + jsonString(dir) + `}`
	}

	cases := []struct {
		name string
		dir  string
		ok   bool
	}{
		{"empty", "", true},
		{"nested", "folderA/sub", true},
		{"deep", "a/b/c/d", true},
		{"dot", ".", false},
		{"double dot", "..", false},
		{"traversal mix", "a/../b", false},
		{"backslash", "a\\b", false},
		{"trailing backslash", "a\\", false},
		{"absolute", "/etc/passwd", false},
		{"volume", "C:/x", false},
		{"control char", "a\x00b", false},
		{"windows reserved", "a:b", false},
		{"asterisk", "a*b", false},
	}
	for _, c := range cases {
		got := testJSONRequestWithCollectionPassword(t, handler, http.MethodPost, "/api/collections/"+token+"/upload-init", "test-secret", initBody(c.dir))
		if c.ok && got.Code != http.StatusOK {
			t.Errorf("dir %q accepted? want 200 got %d: %s", c.dir, got.Code, got.Body.String())
		}
		if !c.ok && got.Code != http.StatusBadRequest {
			t.Errorf("dir %q want 400 got %d: %s", c.dir, got.Code, got.Body.String())
		}
		if !c.ok && !strings.Contains(got.Body.String(), "目录无效") {
			t.Errorf("dir %q expected shared '目录无效' message, got %s", c.dir, got.Body.String())
		}
	}

	// Parity: the exact same bodies against /api/files/upload-init must yield the same code,
	// proving both entry points share the same validation decision (not diverging copies).
	for _, c := range cases {
		got := testJSONRequest(t, handler, http.MethodPost, "/api/files/upload-init", ownerToken, initBody(c.dir))
		if c.ok && got.Code != http.StatusOK {
			t.Errorf("private dir %q want 200 got %d: %s", c.dir, got.Code, got.Body.String())
		}
		if !c.ok && got.Code != http.StatusBadRequest {
			t.Errorf("private dir %q want 400 got %d: %s", c.dir, got.Code, got.Body.String())
		}
	}

	// Nested folder task must be persisted with the dir under the collection uploads root.
	init := testJSONRequestWithCollectionPassword(t, handler, http.MethodPost, "/api/collections/"+token+"/upload-init", "test-secret", initBody("folderA/sub"))
	if init.Code != http.StatusOK {
		t.Fatalf("nested init = %d: %s", init.Code, init.Body.String())
	}
	taskID := responseData(t, init)["taskId"].(string)
	var ownerID int64
	if err := db.DB.QueryRow("SELECT created_by FROM upload_collections WHERE token = ?", token).Scan(&ownerID); err != nil {
		t.Fatalf("owner lookup: %v", err)
	}
	var storageDir string
	if err := db.DB.QueryRow("SELECT storage_dir FROM upload_tasks WHERE id = ?", taskID).Scan(&storageDir); err != nil {
		t.Fatalf("task storage_dir: %v", err)
	}
	wantDir := filepath.ToSlash(filepath.Join("files", itoa(ownerID), "uploads", token, "folderA", "sub"))
	if filepath.ToSlash(storageDir) != wantDir {
		t.Fatalf("storage_dir = %q want %q", storageDir, wantDir)
	}
	// EnsureFolderPath created the nested folder records for owner navigation.
	var folders int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM folders WHERE user_id = ? AND path = ?", ownerID, "uploads/"+token+"/folderA/sub").Scan(&folders); err != nil {
		t.Fatalf("folder lookup: %v", err)
	}
	if folders != 1 {
		t.Fatalf("nested folder record missing, folders=%d", folders)
	}

	// No traversal folders were ever persisted.
	var badFolder int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM folders WHERE path LIKE '%..%' OR path LIKE '%\\%'").Scan(&badFolder); err != nil {
		t.Fatal(err)
	}
	if badFolder != 0 {
		t.Fatalf("traversal folders persisted: %d", badFolder)
	}
}

// TestCollectionUploadNameParityWithPrivate confirms filename validation parity between the
// anonymous collection upload endpoint and private uploads for identical names.
func TestCollectionUploadNameParityWithPrivate(t *testing.T) {
	db, handler := newTestServer(t)
	ownerToken := testAdminToken(t, handler)
	token := createProtectedCollectionForTest(t, handler, ownerToken, "name-parity")
	cases := []string{
		"plain.txt",
		"中文 文件 (1).log",
		"dot file.tar.gz",
		"..",        // traversal
		"a/../../b", // separator
		"a\\b",      // backslash
		"a\x00b",    // control
		"<bad>",     // windows reserved
		`"quote"`,
		"pipe|name",
	}
	for _, name := range cases {
		body := `{"name":` + jsonString(name) + `,"size":1,"chunkSize":0}`
		collection := testJSONRequestWithCollectionPassword(t, handler, http.MethodPost, "/api/collections/"+token+"/upload-init", "test-secret", body)
		private := testJSONRequest(t, handler, http.MethodPost, "/api/files/upload-init", ownerToken, body)
		if collection.Code != private.Code {
			t.Errorf("name %q parity mismatch: collection=%d private=%d (%s)", name, collection.Code, private.Code, collection.Body.String())
		}
	}
	var leftover int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM upload_tasks WHERE status IN ('pending','active')").Scan(&leftover)
	if leftover > 55 {
		t.Fatalf("unexpected leftover tasks: %d", leftover)
	}
}

func TestCollectionUploadDirFolderNotExposedThroughMeta(t *testing.T) {
	_, handler := newTestServer(t)
	ownerToken := testAdminToken(t, handler)
	token := createProtectedCollectionForTest(t, handler, ownerToken, "meta-guard")
	meta := testJSONRequestWithCollectionPassword(t, handler, http.MethodGet, "/api/collections/"+token+"/meta", "test-secret", "")
	if meta.Code != http.StatusOK {
		t.Fatalf("meta = %d: %s", meta.Code, meta.Body.String())
	}
	for _, marker := range []string{"uploads/", "storage_dir", "folderA"} {
		if strings.Contains(meta.Body.String(), marker) {
			t.Fatalf("meta leaked internal storage marker %q: %s", marker, meta.Body.String())
		}
	}
}

func TestEnsureNoCollectionDirTraversalFiles(t *testing.T) {
	_, handler := newTestServer(t)
	ownerToken := testAdminToken(t, handler)
	token := createProtectedCollectionForTest(t, handler, ownerToken, "safe")
	for _, dir := range []string{"..", "../..", "a\\..", "/etc", "C:\\windows"} {
		body := `{"name":"evil.txt","size":1,"chunkSize":0,"dir":` + jsonString(dir) + `}`
		got := testJSONRequestWithCollectionPassword(t, handler, http.MethodPost, "/api/collections/"+token+"/upload-init", "test-secret", body)
		if got.Code == http.StatusOK {
			t.Fatalf("dir %q unexpectedly accepted: %s", dir, got.Body.String())
		}
	}
}

func itoa(value int64) string {
	return strconv.FormatInt(value, 10)
}

func jsonString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
