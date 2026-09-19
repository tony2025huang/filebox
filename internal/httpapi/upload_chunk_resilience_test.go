package httpapi

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// 服务端要求分片大小在 2MB–8MB，所以用例用 2MiB 分片；3 片 = 2MiB + 2MiB + 1 字节。
const testChunkSize = 2 << 20

func testChunkBody() string { return strings.Repeat("a", testChunkSize) }

// failingReader 先吐出 remaining 个字节，然后报错，用来模拟"客户端中途暂停/断线"的半截请求体。
// failingReader emits a few bytes and then fails, simulating a client that aborted mid-body.
type failingReader struct{ remaining int }

func (r *failingReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, errors.New("simulated client abort")
	}
	n := len(p)
	if n > r.remaining {
		n = r.remaining
	}
	for i := 0; i < n; i++ {
		p[i] = 'b'
	}
	r.remaining -= n
	return n, nil
}

// TestFailedChunkReuploadKeepsPreviousChunk 覆盖"暂停/重试把已成功的分片覆盖成半成品"的回归：
// 同一下标第二次上传中途失败时，上一次成功的分片文件必须原样保留（否则 chunks 表有行、磁盘无文件，
// complete 会报「上传分片不完整」且重试无法自愈）。
// A failed re-upload of the same chunk index must not destroy the previously stored file.
func TestFailedChunkReuploadKeepsPreviousChunk(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	init := initUpload(t, handler, token, "resume.bin", 2*testChunkSize+1, testChunkSize, "")
	taskID, _ := init["taskId"].(string)
	if taskID == "" {
		t.Fatal("upload-init did not return a taskId")
	}
	chunk0 := filepath.Join(db.DataDir, "tmp", taskID, "0")

	first := testJSONRequest(t, handler, http.MethodPut, "/api/files/"+taskID+"/chunks/0", token, testChunkBody())
	if first.Code != http.StatusOK {
		t.Fatalf("first chunk upload = %d: %s", first.Code, first.Body.String())
	}
	if info, err := os.Stat(chunk0); err != nil || info.Size() != testChunkSize {
		t.Fatalf("stored chunk 0 = %v (err=%v), want %d bytes", info, err, testChunkSize)
	}

	// 第二次上传同一下标：声明整片大小，实际只给 1MiB 后报错（等价于暂停把在途 PUT abort 掉）。
	request := httptest.NewRequest(http.MethodPut, "/api/files/"+taskID+"/chunks/0", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Body = io.NopCloser(&failingReader{remaining: testChunkSize / 2})
	request.ContentLength = testChunkSize
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code == http.StatusOK {
		t.Fatalf("aborted re-upload unexpectedly succeeded: %s", recorder.Body.String())
	}

	info, err := os.Stat(chunk0)
	if err != nil {
		t.Fatalf("good chunk 0 was destroyed by a failed re-upload: %v", err)
	}
	if info.Size() != testChunkSize {
		t.Fatalf("chunk 0 size = %d after failed re-upload, want %d", info.Size(), testChunkSize)
	}
	if _, err := os.Stat(chunk0 + ".part"); err == nil {
		t.Fatal("partial .part file was left behind")
	}
}

// TestUploadStatusHidesChunksMissingOnDisk 覆盖"以磁盘为准"的回归：chunks 表有行但磁盘文件已被删时，
// /status 不得再报该下标，否则客户端会跳过它、complete 永远失败。
// A chunk row whose file is gone must not be reported as uploaded.
func TestUploadStatusHidesChunksMissingOnDisk(t *testing.T) {
	db, handler := newTestServer(t)
	token := testAdminToken(t, handler)
	init := initUpload(t, handler, token, "resume2.bin", 2*testChunkSize+1, testChunkSize, "")
	taskID, _ := init["taskId"].(string)
	if taskID == "" {
		t.Fatal("upload-init did not return a taskId")
	}
	for index := 0; index < 2; index++ {
		recorder := testJSONRequest(t, handler, http.MethodPut, "/api/files/"+taskID+"/chunks/"+strconv.Itoa(index), token, testChunkBody())
		if recorder.Code != http.StatusOK {
			t.Fatalf("chunk %d upload = %d: %s", index, recorder.Code, recorder.Body.String())
		}
	}

	// 制造"有记录无文件"：直接删掉分片 1 的文件（等价于一次被 abort 的覆盖写在旧实现下的结果）。
	if err := os.Remove(filepath.Join(db.DataDir, "tmp", taskID, "1")); err != nil {
		t.Fatal(err)
	}

	status := testJSONRequest(t, handler, http.MethodGet, "/api/files/"+taskID+"/status", token, "")
	if status.Code != http.StatusOK {
		t.Fatalf("upload status = %d: %s", status.Code, status.Body.String())
	}
	uploaded, ok := responseData(t, status)["uploadedChunks"].([]any)
	if !ok {
		t.Fatalf("uploadedChunks = %#v", responseData(t, status)["uploadedChunks"])
	}
	if len(uploaded) != 1 || uploaded[0] != float64(0) {
		t.Fatalf("uploadedChunks = %#v, want only index 0 (missing file must be reported as pending)", uploaded)
	}
}
