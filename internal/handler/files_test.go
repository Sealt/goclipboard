package handler

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"goclipboard/internal/model"
	"goclipboard/internal/store"

	"github.com/gorilla/websocket"
)

func newFileTestHandler(password string) *Handler {
	staticFS, _ := fs.Sub(testStatic, "testdata")
	files := store.NewFileStore(store.WithFileRoot(mustTempDir()))
	return New(store.New(), staticFS, nil, Options{
		Files:          files,
		UploadPassword: password,
	})
}

func mustTempDir() string {
	dir, err := os.MkdirTemp("", "goclipboard-files-*")
	if err != nil {
		panic(err)
	}
	return dir
}

func multipartBody(t *testing.T, adminPassword, filePassword, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("password", adminPassword); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteField("filePassword", filePassword); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteField("ttlSeconds", "3600"); err != nil {
		t.Fatal(err)
	}
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

func enableRoomUpload(t *testing.T, mux http.Handler, room, adminPW string) {
	t.Helper()
	body := strings.NewReader(`{"fileUploadEnabled":true,"adminPassword":` + jsonString(adminPW) + `}`)
	req := httptest.NewRequest(http.MethodPut, "/api/clipboard/"+room+"/settings", body)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("enable upload status = %d body %s", res.Code, res.Body.String())
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestFileUploadListDownloadDelete(t *testing.T) {
	h := newFileTestHandler("admin-secret")
	mux := h.Routes()

	// Upload while room closed + no admin password → 401
	{
		body, ctype := multipartBody(t, "", "file-pw", "blocked.txt", []byte("nope"))
		req := httptest.NewRequest(http.MethodPost, "/api/clipboard/room1/files", body)
		req.Header.Set("Content-Type", ctype)
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("upload closed no admin status = %d, body %s", res.Code, res.Body.String())
		}
	}

	// Upload while room closed + wrong admin password → 401
	{
		body, ctype := multipartBody(t, "wrong", "file-pw", "blocked.txt", []byte("nope"))
		req := httptest.NewRequest(http.MethodPost, "/api/clipboard/room1/files", body)
		req.Header.Set("Content-Type", ctype)
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("upload closed bad admin status = %d, body %s", res.Code, res.Body.String())
		}
	}

	// Upload while room closed + correct admin password → 201 (one-shot, does not open room)
	{
		body, ctype := multipartBody(t, "admin-secret", "file-pw", "oneshot.txt", []byte("once"))
		req := httptest.NewRequest(http.MethodPost, "/api/clipboard/room1/files", body)
		req.Header.Set("Content-Type", ctype)
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusCreated {
			t.Fatalf("upload closed with admin status = %d, body %s", res.Code, res.Body.String())
		}
		// Room should still be closed for non-admin uploads.
		body2, ctype2 := multipartBody(t, "", "file-pw", "still-closed.txt", []byte("x"))
		req2 := httptest.NewRequest(http.MethodPost, "/api/clipboard/room1/files", body2)
		req2.Header.Set("Content-Type", ctype2)
		res2 := httptest.NewRecorder()
		mux.ServeHTTP(res2, req2)
		if res2.Code != http.StatusUnauthorized {
			t.Fatalf("room should still be closed, status = %d", res2.Code)
		}
	}

	enableRoomUpload(t, mux, "room1", "admin-secret")

	// Upload without file password → 400
	{
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		_ = w.WriteField("ttlSeconds", "3600")
		part, _ := w.CreateFormFile("file", "x.txt")
		_, _ = part.Write([]byte("x"))
		_ = w.Close()
		req := httptest.NewRequest(http.MethodPost, "/api/clipboard/room1/files", &buf)
		req.Header.Set("Content-Type", w.FormDataContentType())
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("upload missing file pw status = %d, body %s", res.Code, res.Body.String())
		}
	}

	// Upload with only file password (room already enabled) → 201
	var info model.FileInfo
	{
		body, ctype := multipartBody(t, "", "file-pw", "hello.txt", []byte("hello world"))
		req := httptest.NewRequest(http.MethodPost, "/api/clipboard/room1/files", body)
		req.Header.Set("Content-Type", ctype)
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusCreated {
			t.Fatalf("upload status = %d, body %s", res.Code, res.Body.String())
		}
		if err := json.Unmarshal(res.Body.Bytes(), &info); err != nil {
			t.Fatal(err)
		}
		if info.ID == "" || info.Name != "hello.txt" || info.Size != 11 {
			t.Fatalf("unexpected info: %+v", info)
		}
	}

	// List (open) includes upload flag and both uploads (one-shot + open-room)
	{
		req := httptest.NewRequest(http.MethodGet, "/api/clipboard/room1/files", nil)
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("list status = %d", res.Code)
		}
		var list model.FileListResponse
		if err := json.Unmarshal(res.Body.Bytes(), &list); err != nil {
			t.Fatal(err)
		}
		if len(list.Files) < 2 {
			t.Fatalf("list = %+v, want >= 2 files", list)
		}
		found := false
		for _, f := range list.Files {
			if f.ID == info.ID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("list missing hello.txt id %s: %+v", info.ID, list)
		}
		if !list.FileUploadEnabled {
			t.Fatal("list should report fileUploadEnabled=true")
		}
	}

	// Download with admin password must fail (download uses file password)
	{
		req := httptest.NewRequest(http.MethodGet, "/api/clipboard/room1/files/"+info.ID, nil)
		req.Header.Set("X-Admin-Password", "admin-secret")
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("download admin-only status = %d", res.Code)
		}
	}

	// Download with wrong file password → 401
	{
		req := httptest.NewRequest(http.MethodGet, "/api/clipboard/room1/files/"+info.ID, nil)
		req.Header.Set("X-File-Password", "wrong")
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("download bad file pw status = %d", res.Code)
		}
	}

	// Download with file password header → 200
	{
		req := httptest.NewRequest(http.MethodGet, "/api/clipboard/room1/files/"+info.ID, nil)
		req.Header.Set("X-File-Password", "file-pw")
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("download status = %d", res.Code)
		}
		if res.Body.String() != "hello world" {
			t.Fatalf("body = %q", res.Body.String())
		}
		cd := res.Header().Get("Content-Disposition")
		if cd == "" || !bytes.Contains([]byte(cd), []byte("hello.txt")) {
			t.Fatalf("Content-Disposition = %q", cd)
		}
	}

	// Download with query filePassword → 200
	{
		req := httptest.NewRequest(http.MethodGet, "/api/clipboard/room1/files/"+info.ID+"?filePassword=file-pw", nil)
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("download query file pw status = %d body %s", res.Code, res.Body.String())
		}
	}

	// Delete with file password must fail
	{
		req := httptest.NewRequest(http.MethodDelete, "/api/clipboard/room1/files/"+info.ID, nil)
		req.Header.Set("X-File-Password", "file-pw")
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("delete with file pw status = %d", res.Code)
		}
	}

	// Delete without password → 401
	{
		req := httptest.NewRequest(http.MethodDelete, "/api/clipboard/room1/files/"+info.ID, nil)
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("delete no pw status = %d", res.Code)
		}
	}

	// Delete with admin password
	{
		req := httptest.NewRequest(http.MethodDelete, "/api/clipboard/room1/files/"+info.ID, nil)
		req.Header.Set("X-Admin-Password", "admin-secret")
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusNoContent {
			t.Fatalf("delete status = %d body %s", res.Code, res.Body.String())
		}
	}

	// Gone
	{
		req := httptest.NewRequest(http.MethodGet, "/api/clipboard/room1/files/"+info.ID, nil)
		req.Header.Set("X-File-Password", "file-pw")
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusNotFound {
			t.Fatalf("get after delete status = %d", res.Code)
		}
	}
}

func TestUploadDisabledWithoutPassword(t *testing.T) {
	h := newFileTestHandler("") // not configured
	mux := h.Routes()

	body, ctype := multipartBody(t, "x", "y", "a.txt", []byte("a"))
	req := httptest.NewRequest(http.MethodPost, "/api/clipboard/r/files", body)
	req.Header.Set("Content-Type", ctype)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body %s", res.Code, res.Body.String())
	}
}

// View-scoped rooms gate file upload/list on the room password, not only
// admin/file passwords.
func TestViewPasswordGatesFileUploadAndList(t *testing.T) {
	h := newFileTestHandler("admin-secret")
	mux := h.Routes()

	// Create room + lock with view scope.
	putReq := httptest.NewRequest(http.MethodPut, "/api/clipboard/vfiles", strings.NewReader(
		`{"content":"secret","ttlSeconds":3600}`))
	putReq.Header.Set("Content-Type", "application/json")
	putRes := httptest.NewRecorder()
	mux.ServeHTTP(putRes, putReq)
	if putRes.Code != http.StatusOK {
		t.Fatalf("create room = %d", putRes.Code)
	}
	pwBody := strings.NewReader(`{"password":"room-pw","scope":"view"}`)
	pwReq := httptest.NewRequest(http.MethodPut, "/api/clipboard/vfiles/password", pwBody)
	pwReq.Header.Set("Content-Type", "application/json")
	pwRes := httptest.NewRecorder()
	mux.ServeHTTP(pwRes, pwReq)
	if pwRes.Code != http.StatusOK {
		t.Fatalf("set password = %d body %s", pwRes.Code, pwRes.Body.String())
	}

	// Enable upload with admin + room password.
	setBody := strings.NewReader(`{"fileUploadEnabled":true,"adminPassword":"admin-secret"}`)
	setReq := httptest.NewRequest(http.MethodPut, "/api/clipboard/vfiles/settings", setBody)
	setReq.Header.Set("Content-Type", "application/json")
	setReq.Header.Set("X-Goclip-Password", "room-pw")
	setRes := httptest.NewRecorder()
	mux.ServeHTTP(setRes, setReq)
	if setRes.Code != http.StatusOK {
		t.Fatalf("enable upload = %d body %s", setRes.Code, setRes.Body.String())
	}

	// List without room password → 401.
	listReq := httptest.NewRequest(http.MethodGet, "/api/clipboard/vfiles/files", nil)
	listRes := httptest.NewRecorder()
	mux.ServeHTTP(listRes, listReq)
	if listRes.Code != http.StatusUnauthorized {
		t.Fatalf("list without room password = %d, want 401", listRes.Code)
	}

	// Upload without room password → 401 (even with file password + enabled room).
	body, ctype := multipartBody(t, "", "file-pw", "a.txt", []byte("data"))
	upReq := httptest.NewRequest(http.MethodPost, "/api/clipboard/vfiles/files", body)
	upReq.Header.Set("Content-Type", ctype)
	upRes := httptest.NewRecorder()
	mux.ServeHTTP(upRes, upReq)
	if upRes.Code != http.StatusUnauthorized {
		t.Fatalf("upload without room password = %d, want 401; body %s", upRes.Code, upRes.Body.String())
	}

	// Upload with room password → 201.
	body2, ctype2 := multipartBody(t, "", "file-pw", "a.txt", []byte("data"))
	upReq2 := httptest.NewRequest(http.MethodPost, "/api/clipboard/vfiles/files", body2)
	upReq2.Header.Set("Content-Type", ctype2)
	upReq2.Header.Set("X-Goclip-Password", "room-pw")
	upRes2 := httptest.NewRecorder()
	mux.ServeHTTP(upRes2, upReq2)
	if upRes2.Code != http.StatusCreated {
		t.Fatalf("upload with room password = %d, want 201; body %s", upRes2.Code, upRes2.Body.String())
	}
}

func TestFileChangesBroadcastOnWebSocket(t *testing.T) {
	staticFS, _ := fs.Sub(testStatic, "testdata")
	files := store.NewFileStore(store.WithFileRoot(t.TempDir()))
	h := New(store.New(), staticFS, nil, Options{
		Files:          files,
		UploadPassword: "secret",
	})
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/clipboard/syncfiles/ws?clientId=viewer1"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Drain until we see initial files snapshot (possibly after state/cursor).
	deadline := time.Now().Add(3 * time.Second)
	sawFiles := false
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		var msg map[string]any
		if err := conn.ReadJSON(&msg); err != nil {
			continue
		}
		if msg["type"] == "files" {
			sawFiles = true
			break
		}
	}
	if !sawFiles {
		t.Fatal("expected initial files message on connect")
	}

	// Enable room upload first.
	setBody := strings.NewReader(`{"fileUploadEnabled":true,"adminPassword":"secret"}`)
	setReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/clipboard/syncfiles/settings", setBody)
	setReq.Header.Set("Content-Type", "application/json")
	setRes, err := http.DefaultClient.Do(setReq)
	if err != nil {
		t.Fatal(err)
	}
	setRes.Body.Close()
	if setRes.StatusCode != http.StatusOK {
		t.Fatalf("enable upload status = %d", setRes.StatusCode)
	}

	// Upload a file via HTTP; WS peer should receive type=files with the new entry.
	body, ctype := multipartBody(t, "secret", "dl-pw", "sync.txt", []byte("payload"))
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/clipboard/syncfiles/files", body)
	req.Header.Set("Content-Type", ctype)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d", res.StatusCode)
	}

	deadline = time.Now().Add(3 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		var msg map[string]any
		if err := conn.ReadJSON(&msg); err != nil {
			continue
		}
		if msg["type"] != "files" {
			continue
		}
		raw, _ := json.Marshal(msg["files"])
		var list []model.FileInfo
		_ = json.Unmarshal(raw, &list)
		if len(list) == 1 && list[0].Name == "sync.txt" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected WS files update after upload")
	}
}

func TestClearClipboardRemovesFiles(t *testing.T) {
	h := newFileTestHandler("pw")
	mux := h.Routes()
	enableRoomUpload(t, mux, "zap", "pw")

	body, ctype := multipartBody(t, "pw", "fp", "a.txt", []byte("data"))
	up := httptest.NewRequest(http.MethodPost, "/api/clipboard/zap/files", body)
	up.Header.Set("Content-Type", ctype)
	upRes := httptest.NewRecorder()
	mux.ServeHTTP(upRes, up)
	if upRes.Code != http.StatusCreated {
		t.Fatalf("upload = %d %s", upRes.Code, upRes.Body.String())
	}

	del := httptest.NewRequest(http.MethodDelete, "/api/clipboard/zap", nil)
	delRes := httptest.NewRecorder()
	mux.ServeHTTP(delRes, del)
	if delRes.Code != http.StatusNoContent {
		t.Fatalf("delete room = %d", delRes.Code)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/clipboard/zap/files", nil)
	listRes := httptest.NewRecorder()
	mux.ServeHTTP(listRes, listReq)
	var list model.FileListResponse
	_ = json.Unmarshal(listRes.Body.Bytes(), &list)
	if len(list.Files) != 0 {
		t.Fatalf("files should be cleared, got %+v", list.Files)
	}
}
