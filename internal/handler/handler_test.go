package handler

import (
	"bytes"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goclipboard/internal/model"
	"goclipboard/internal/store"

	"github.com/gorilla/websocket"
)

//go:embed testdata/static/*
var testStatic embed.FS

func newTestHandler() *Handler {
	staticFS, _ := fs.Sub(testStatic, "testdata")
	return New(store.New(), staticFS, nil)
}

func newTestHandlerWithStore(s *store.Store) *Handler {
	staticFS, _ := fs.Sub(testStatic, "testdata")
	return New(s, staticFS, nil)
}

func saveJSON(content string, ttl int64) string {
	b, _ := json.Marshal(map[string]any{
		"content":    content,
		"ttlSeconds": ttl,
		"clientId":   "tester",
	})
	return string(b)
}

func TestClipboardLifecycle(t *testing.T) {
	h := newTestHandler()
	handler := h.Routes()

	saveBody := bytes.NewBufferString(saveJSON("hello", 3600))
	saveReq := httptest.NewRequest(http.MethodPut, "/api/clipboard/demo", saveBody)
	saveRes := httptest.NewRecorder()
	handler.ServeHTTP(saveRes, saveReq)

	if saveRes.Code != http.StatusOK {
		t.Fatalf("save status = %d, want %d; body: %s", saveRes.Code, http.StatusOK, saveRes.Body.String())
	}

	var saved model.ClipboardResponse
	if err := json.Unmarshal(saveRes.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode save response: %v", err)
	}
	if saved.Content != "hello" || saved.TTLSeconds != 3600 || saved.Version != 1 || !saved.Exists {
		t.Fatalf("unexpected save response: %+v", saved)
	}
	if saved.UpdatedBy != "tester" {
		t.Fatalf("updatedBy = %q, want tester", saved.UpdatedBy)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/clipboard/demo", nil)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, getReq)

	if getRes.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d", getRes.Code, http.StatusOK)
	}

	var got model.ClipboardResponse
	if err := json.Unmarshal(getRes.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if got.Content != "hello" || got.Version != 1 || !got.Exists {
		t.Fatalf("unexpected get response: %+v", got)
	}
}

func TestSaveLastWriteWinsNoConflict(t *testing.T) {
	h := newTestHandler()
	handler := h.Routes()

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodPut, "/api/clipboard/race", bytes.NewBufferString(saveJSON("a", 3600))))
	if first.Code != http.StatusOK {
		t.Fatalf("first save status = %d", first.Code)
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodPut, "/api/clipboard/race", bytes.NewBufferString(
		`{"content":"b","ttlSeconds":3600,"clientId":"other","baseVersion":0}`,
	)))
	if second.Code != http.StatusOK {
		t.Fatalf("second save status = %d, want 200; body: %s", second.Code, second.Body.String())
	}

	var saved model.ClipboardResponse
	json.Unmarshal(second.Body.Bytes(), &saved)
	if saved.Content != "b" || saved.Version != 2 {
		t.Fatalf("unexpected LWW save: %+v", saved)
	}
}

func TestExpiredClipboardIsRemoved(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	st := store.New(store.WithClock(func() time.Time { return now }))
	h := newTestHandlerWithStore(st)
	handler := h.Routes()

	saveReq := httptest.NewRequest(http.MethodPut, "/api/clipboard/soon", bytes.NewBufferString(saveJSON("gone", 1)))
	saveRes := httptest.NewRecorder()
	handler.ServeHTTP(saveRes, saveReq)
	if saveRes.Code != http.StatusOK {
		t.Fatalf("save status = %d, want %d", saveRes.Code, http.StatusOK)
	}

	now = now.Add(2 * time.Second)
	getReq := httptest.NewRequest(http.MethodGet, "/api/clipboard/soon", nil)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, getReq)

	var got model.ClipboardResponse
	if err := json.Unmarshal(getRes.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if got.Exists || got.Content != "" || got.Version != 0 {
		t.Fatalf("expired clipboard should look empty, got %+v", got)
	}
}

func TestRejectsCapacityExceeded(t *testing.T) {
	st := store.New(store.WithLimits(1, store.DefaultMaxTotalBytes))
	h := newTestHandlerWithStore(st)
	handler := h.Routes()

	okRes := httptest.NewRecorder()
	handler.ServeHTTP(okRes, httptest.NewRequest(http.MethodPut, "/api/clipboard/first", bytes.NewBufferString(saveJSON("a", 3600))))
	if okRes.Code != http.StatusOK {
		t.Fatalf("first save status = %d, want 200; body: %s", okRes.Code, okRes.Body.String())
	}

	fullRes := httptest.NewRecorder()
	handler.ServeHTTP(fullRes, httptest.NewRequest(http.MethodPut, "/api/clipboard/second", bytes.NewBufferString(saveJSON("b", 3600))))
	if fullRes.Code != http.StatusInsufficientStorage {
		t.Fatalf("second save status = %d, want 507; body: %s", fullRes.Code, fullRes.Body.String())
	}
	if fullRes.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After on 507")
	}
}

func TestRejectsInvalidKeysAndTTL(t *testing.T) {
	h := newTestHandler()
	handler := h.Routes()

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{
			name:   "multi segment key",
			method: http.MethodGet,
			path:   "/api/clipboard/a/b",
			status: http.StatusNotFound,
		},
		{
			name:   "reserved key page",
			method: http.MethodGet,
			path:   "/api",
			status: http.StatusNotFound,
		},
		{
			name:   "zero ttl",
			method: http.MethodPut,
			path:   "/api/clipboard/demo",
			body:   saveJSON("x", 0),
			status: http.StatusBadRequest,
		},
		{
			name:   "unknown JSON field",
			method: http.MethodPut,
			path:   "/api/clipboard/demo",
			body:   `{"content":"x","ttlSeconds":60,"unexpected":true}`,
			status: http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != tc.status {
				t.Fatalf("status = %d, want %d; body: %s", res.Code, tc.status, res.Body.String())
			}
		})
	}
}

func TestPageAndRootRoutes(t *testing.T) {
	h := newTestHandler()
	handler := h.Routes()

	pageReq := httptest.NewRequest(http.MethodGet, "/room-1", nil)
	pageRes := httptest.NewRecorder()
	handler.ServeHTTP(pageRes, pageReq)
	if pageRes.Code != http.StatusOK {
		t.Fatalf("page status = %d, want %d", pageRes.Code, http.StatusOK)
	}

	rootReq := httptest.NewRequest(http.MethodGet, "/", nil)
	rootRes := httptest.NewRecorder()
	handler.ServeHTTP(rootRes, rootReq)
	if rootRes.Code != http.StatusFound {
		t.Fatalf("root status = %d, want %d", rootRes.Code, http.StatusFound)
	}
	if rootRes.Header().Get("Location") == "" {
		t.Fatal("root redirect should set Location")
	}
}

func TestDeleteClipboard(t *testing.T) {
	h := newTestHandler()
	handler := h.Routes()

	saveReq := httptest.NewRequest(http.MethodPut, "/api/clipboard/todelete", bytes.NewBufferString(saveJSON("bye", 60)))
	handler.ServeHTTP(httptest.NewRecorder(), saveReq)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/clipboard/todelete", nil)
	delRes := httptest.NewRecorder()
	handler.ServeHTTP(delRes, delReq)
	if delRes.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d", delRes.Code, http.StatusNoContent)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/clipboard/todelete", nil)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, getReq)

	var got model.ClipboardResponse
	json.Unmarshal(getRes.Body.Bytes(), &got)
	if got.Exists {
		t.Fatal("deleted clipboard should not exist")
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := newTestHandler()
	handler := h.Routes()

	req := httptest.NewRequest(http.MethodPost, "/api/clipboard/test", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusMethodNotAllowed)
	}
}

func TestHealthCheck(t *testing.T) {
	h := newTestHandler()
	handler := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want %d", res.Code, http.StatusOK)
	}
}

func TestHealthCheckMethodNotAllowed(t *testing.T) {
	h := newTestHandler()
	handler := h.Routes()

	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusMethodNotAllowed)
	}
}

func TestVersionUpdate(t *testing.T) {
	h := newTestHandler()
	handler := h.Routes()

	for i := 1; i <= 3; i++ {
		body := bytes.NewBufferString(saveJSON("value-"+string(rune('0'+i)), 3600))
		req := httptest.NewRequest(http.MethodPut, "/api/clipboard/ver", body)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)

		var got model.ClipboardResponse
		json.Unmarshal(res.Body.Bytes(), &got)
		if res.Code != http.StatusOK {
			t.Fatalf("save %d status = %d body %s", i, res.Code, res.Body.String())
		}
		if got.Version != int64(i) {
			t.Fatalf("version = %d, want %d", got.Version, i)
		}
	}
}

func TestSaveResponseMatchesGet(t *testing.T) {
	h := newTestHandler()
	handler := h.Routes()

	saveReq := httptest.NewRequest(http.MethodPut, "/api/clipboard/check", bytes.NewBufferString(saveJSON("same", 7200)))
	saveRes := httptest.NewRecorder()
	handler.ServeHTTP(saveRes, saveReq)

	var saved model.ClipboardResponse
	json.Unmarshal(saveRes.Body.Bytes(), &saved)

	getReq := httptest.NewRequest(http.MethodGet, "/api/clipboard/check", nil)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, getReq)

	var got model.ClipboardResponse
	json.Unmarshal(getRes.Body.Bytes(), &got)

	if saved.Content != got.Content || saved.Version != got.Version || saved.TTLSeconds != got.TTLSeconds {
		t.Fatal("save response should match get response")
	}
}

func TestWebSocketUpdateAndCursor(t *testing.T) {
	h := newTestHandler()
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/clipboard/live/ws?clientId=reader1"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	// Initial snapshot: state + cursor (order may vary but both should arrive).
	gotState, gotCursor := false, false
	for i := 0; i < 4 && !(gotState && gotCursor); i++ {
		var msg wsOutbound
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("read initial frame: %v", err)
		}
		switch msg.Type {
		case "state", "update":
			gotState = true
			if msg.Exists == nil || *msg.Exists {
				t.Fatalf("empty room should have exists=false, got %+v", msg)
			}
		case "cursor":
			gotCursor = true
		}
	}
	if !gotState || !gotCursor {
		t.Fatalf("missing initial frames state=%v cursor=%v", gotState, gotCursor)
	}

	// Cursor from another client must not re-broadcast content when version unchanged.
	writerURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/clipboard/live/ws?clientId=writer1"
	writer, _, err := websocket.DefaultDialer.Dial(writerURL, nil)
	if err != nil {
		t.Fatalf("dial writer: %v", err)
	}
	defer writer.Close()

	// Drain writer's initial frames.
	_ = writer.SetReadDeadline(time.Now().Add(time.Second))
	for i := 0; i < 2; i++ {
		var dump wsOutbound
		_ = writer.ReadJSON(&dump)
	}

	if err := writer.WriteJSON(map[string]any{
		"type":         "cursor",
		"cursorPos":    1,
		"selectionEnd": 4,
		"color":        "#e06c75",
	}); err != nil {
		t.Fatalf("write cursor: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		var msg wsOutbound
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("read after cursor: %v", err)
		}
		if msg.Type == "update" || msg.Type == "state" || msg.Type == "ops" {
			t.Fatal("cursor-only change re-sent content update")
		}
		if msg.Type == "cursor" {
			found := false
			for _, c := range msg.Cursors {
				if c.ClientID == "writer1" && c.CursorPos == 1 {
					found = true
				}
			}
			if found {
				break
			}
		}
	}

	// Content save should push an update frame to the reader.
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/clipboard/live", bytes.NewBufferString(saveJSON("hello-ws", 3600)))
	req.Header.Set("Content-Type", "application/json")
	putRes, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	putRes.Body.Close()
	if putRes.StatusCode != http.StatusOK {
		t.Fatalf("put status = %d", putRes.StatusCode)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		var msg wsOutbound
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("read after save: %v", err)
		}
		if (msg.Type == "state" || msg.Type == "update") && msg.Content == "hello-ws" && msg.Version == 1 {
			return
		}
	}
}

func TestSanitizeHelpers(t *testing.T) {
	if got := sanitizeClientID(" ab/cd!ef "); got != "abcdef" {
		t.Fatalf("sanitizeClientID = %q", got)
	}
	if got := sanitizeColor("#E06C75"); got != "#e06c75" {
		t.Fatalf("sanitizeColor hex = %q", got)
	}
	if got := sanitizeColor("red"); got != "#61afef" {
		t.Fatalf("sanitizeColor fallback = %q", got)
	}
}

func TestWebSocketOpsMerge(t *testing.T) {
	h := newTestHandler()
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	// Seed via PUT
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/clipboard/merge", bytes.NewBufferString(saveJSON("x", 3600)))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	// Get parent id from store
	item, ok := h.store.Get("merge")
	if !ok || item.Doc == nil {
		t.Fatal("missing seed")
	}
	parent := item.Doc.VisibleIDs()[0]

	dial := func(id string) *websocket.Conn {
		u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/clipboard/merge/ws?clientId=" + id
		c, _, err := websocket.DefaultDialer.Dial(u, nil)
		if err != nil {
			t.Fatalf("dial %s: %v", id, err)
		}
		// drain initial
		_ = c.SetReadDeadline(time.Now().Add(time.Second))
		for i := 0; i < 2; i++ {
			var dump wsOutbound
			_ = c.ReadJSON(&dump)
		}
		return c
	}

	a := dial("aaa")
	defer a.Close()
	b := dial("bbb")
	defer b.Close()

	if err := a.WriteJSON(map[string]any{
		"type": "ops",
		"ops": []map[string]any{
			{"op": "ins", "id": "aaa:2", "after": parent, "ch": "A"},
		},
		"ttlSeconds": 3600,
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.WriteJSON(map[string]any{
		"type": "ops",
		"ops": []map[string]any{
			{"op": "ins", "id": "bbb:2", "after": parent, "ch": "B"},
		},
		"ttlSeconds": 3600,
	}); err != nil {
		t.Fatal(err)
	}

	// Wait for both ops to apply
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, ok := h.store.Get("merge")
		if ok && (got.Content == "xAB" || got.Content == "xBA") {
			// same clock 2: site aaa < bbb → xAB
			if got.Content != "xAB" {
				t.Fatalf("content = %q, want xAB", got.Content)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, _ := h.store.Get("merge")
	t.Fatalf("timeout waiting for merge, content=%q", got.Content)
}
