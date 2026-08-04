package handler

import (
	"bytes"
	"crypto/tls"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goclipboard/internal/crdt"
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

func TestExpiredClipboardClearsLastEvent(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	st := store.New(store.WithClock(func() time.Time { return now }))
	h := newTestHandlerWithStore(st)
	st.SetOnExpire(h.PingExpired)

	item, err := st.Save("expired-event", "old", time.Second, "writer")
	if err != nil {
		t.Fatal(err)
	}
	h.noteFullState("expired-event", item.Version, item.Generation, item.UpdatedBy, item.Content)

	now = now.Add(2 * time.Second)
	if _, ok := st.Get("expired-event"); ok {
		t.Fatal("expired room should not exist")
	}

	h.eventMu.Lock()
	_, cached := h.lastEvent["expired-event"]
	h.eventMu.Unlock()
	if cached {
		t.Fatal("expired room event should be removed")
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

func TestPageJSONContent(t *testing.T) {
	h := newTestHandler()
	handler := h.Routes()

	saveReq := httptest.NewRequest(http.MethodPut, "/api/clipboard/pjson", bytes.NewBufferString(saveJSON("plain text body", 3600)))
	saveRes := httptest.NewRecorder()
	handler.ServeHTTP(saveRes, saveReq)
	if saveRes.Code != http.StatusOK {
		t.Fatalf("save status = %d, want 200", saveRes.Code)
	}

	// No User-Agent → the .json page URL must still serve plain text.
	getReq := httptest.NewRequest(http.MethodGet, "/pjson.json", nil)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("GET /pjson.json status = %d, want 200; body: %s", getRes.Code, getRes.Body.String())
	}
	if ct := getRes.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content-type = %q, want text/plain", ct)
	}
	if got := getRes.Body.String(); got != "plain text body" {
		t.Fatalf("body = %q, want %q", got, "plain text body")
	}
}

func TestPageJSONMissing(t *testing.T) {
	h := newTestHandler()
	handler := h.Routes()

	getReq := httptest.NewRequest(http.MethodGet, "/missing.json", nil)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusNotFound {
		t.Fatalf("GET /missing.json status = %d, want 404", getRes.Code)
	}
	if ct := getRes.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content-type = %q, want text/plain", ct)
	}
	if !strings.Contains(getRes.Body.String(), "clipboard not found") {
		t.Fatalf("404 body = %q, want a plain-text not-found message", getRes.Body.String())
	}
}

func TestPageJSONViewProtected(t *testing.T) {
	h := newTestHandler()
	handler := h.Routes()

	saveReq := httptest.NewRequest(http.MethodPut, "/api/clipboard/vjson", bytes.NewBufferString(saveJSON("secret text", 3600)))
	handler.ServeHTTP(httptest.NewRecorder(), saveReq)

	pwBody, _ := json.Marshal(map[string]any{"password": "peek-pass", "scope": "view"})
	pwReq := httptest.NewRequest(http.MethodPut, "/api/clipboard/vjson/password", bytes.NewBuffer(pwBody))
	pwRes := httptest.NewRecorder()
	handler.ServeHTTP(pwRes, pwReq)
	if pwRes.Code != http.StatusOK {
		t.Fatalf("set view password status = %d, want 200", pwRes.Code)
	}

	// Without the password → 401, plain text.
	deniedReq := httptest.NewRequest(http.MethodGet, "/vjson.json", nil)
	deniedRes := httptest.NewRecorder()
	handler.ServeHTTP(deniedRes, deniedReq)
	if deniedRes.Code != http.StatusUnauthorized {
		t.Fatalf("GET .json without password = %d, want 401", deniedRes.Code)
	}

	// With X-Goclip-Password → 200 + content.
	okReq := httptest.NewRequest(http.MethodGet, "/vjson.json", nil)
	okReq.Header.Set("X-Goclip-Password", "peek-pass")
	okRes := httptest.NewRecorder()
	handler.ServeHTTP(okRes, okReq)
	if okRes.Code != http.StatusOK || okRes.Body.String() != "secret text" {
		t.Fatalf("GET .json with password = %d body %q, want 200 + content", okRes.Code, okRes.Body.String())
	}
}

func TestPageHelpVsBrowser(t *testing.T) {
	h := newTestHandler()
	handler := h.Routes()

	// Non-browser UA on a bare room URL → plain-text usage hint.
	curlReq := httptest.NewRequest(http.MethodGet, "/room-1", nil)
	curlReq.Header.Set("User-Agent", "curl/8.7.1")
	curlRes := httptest.NewRecorder()
	handler.ServeHTTP(curlRes, curlReq)
	if curlRes.Code != http.StatusOK {
		t.Fatalf("curl status = %d, want 200", curlRes.Code)
	}
	if ct := curlRes.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("curl content-type = %q, want text/plain", ct)
	}
	if body := curlRes.Body.String(); !strings.Contains(body, ".json") {
		t.Fatalf("help body should mention .json, got: %q", body)
	}
	if body := curlRes.Body.String(); !strings.Contains(body, "curl http://") {
		t.Fatalf("help body should default to http:// without X-Forwarded-Proto, got: %q", body)
	}

	// Same request behind an https reverse proxy → help shows https:// URLs.
	httpsReq := httptest.NewRequest(http.MethodGet, "/room-1", nil)
	httpsReq.Header.Set("User-Agent", "curl/8.7.1")
	httpsReq.Header.Set("X-Forwarded-Proto", "https")
	httpsRes := httptest.NewRecorder()
	handler.ServeHTTP(httpsRes, httpsReq)
	if body := httpsRes.Body.String(); !strings.Contains(body, "curl https://") {
		t.Fatalf("help body should use https:// when X-Forwarded-Proto is https, got: %q", body)
	}

	// Direct TLS termination (no proxy headers, server terminates TLS) →
	// https:// in help.
	tlsReq := httptest.NewRequest(http.MethodGet, "/room-1", nil)
	tlsReq.Header.Set("User-Agent", "curl/8.7.1")
	tlsReq.TLS = &tls.ConnectionState{}
	tlsRes := httptest.NewRecorder()
	handler.ServeHTTP(tlsRes, tlsReq)
	if body := tlsRes.Body.String(); !strings.Contains(body, "curl https://") {
		t.Fatalf("help body should use https:// for direct TLS termination, got: %q", body)
	}

	// Browser UA on the same URL → the SPA HTML.
	brReq := httptest.NewRequest(http.MethodGet, "/room-1", nil)
	brReq.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")
	brRes := httptest.NewRecorder()
	handler.ServeHTTP(brRes, brReq)
	if brRes.Code != http.StatusOK {
		t.Fatalf("browser status = %d, want 200", brRes.Code)
	}
	if ct := brRes.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("browser content-type = %q, want text/html", ct)
	}
}

func TestPageJSONMethodNotAllowed(t *testing.T) {
	h := newTestHandler()
	handler := h.Routes()

	putReq := httptest.NewRequest(http.MethodPut, "/room-1.json", bytes.NewBufferString(saveJSON("x", 3600)))
	putRes := httptest.NewRecorder()
	handler.ServeHTTP(putRes, putReq)
	if putRes.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT /room-1.json status = %d, want 405", putRes.Code)
	}
	if allow := putRes.Header().Get("Allow"); allow != http.MethodGet {
		t.Fatalf("Allow header = %q, want %q", allow, http.MethodGet)
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

func TestWebSocketAckSyncAndTTLOnly(t *testing.T) {
	h := newTestHandler()
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/clipboard/ackroom/ws?clientId=aaa"
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	readUntil := func(typ string) wsOutbound {
		t.Helper()
		for i := 0; i < 20; i++ {
			var msg wsOutbound
			if err := conn.ReadJSON(&msg); err != nil {
				t.Fatalf("read waiting for %q: %v", typ, err)
			}
			if msg.Type == typ {
				return msg
			}
		}
		t.Fatalf("no %q frame", typ)
		return wsOutbound{}
	}

	// Op batch with seq → direct ack with same seq and new version.
	if err := conn.WriteJSON(map[string]any{
		"type": "ops",
		"seq":  7,
		"ops": []map[string]any{
			{"op": "ins", "id": "aaa:1", "after": "", "ch": "H"},
		},
		"ttlSeconds": 3600,
	}); err != nil {
		t.Fatal(err)
	}
	ack := readUntil("ack")
	if ack.Seq != 7 || ack.Version != 1 || ack.ErrMsg != "" {
		t.Fatalf("ack = %+v, want seq=7 version=1", ack)
	}

	// Idempotent resend (ack was "lost"): still acked, version unchanged.
	if err := conn.WriteJSON(map[string]any{
		"type": "ops",
		"seq":  8,
		"ops": []map[string]any{
			{"op": "ins", "id": "aaa:1", "after": "", "ch": "H"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	ack = readUntil("ack")
	if ack.Seq != 8 || ack.Version != 1 || ack.ErrMsg != "" {
		t.Fatalf("resend ack = %+v, want seq=8 version=1", ack)
	}

	// TTL-only update (empty ops): acked, version bumped, no content PUT.
	if err := conn.WriteJSON(map[string]any{
		"type":       "ops",
		"seq":        9,
		"ttlSeconds": 7200,
	}); err != nil {
		t.Fatal(err)
	}
	ack = readUntil("ack")
	if ack.Seq != 9 || ack.Version != 2 || ack.TTLSeconds != 7200 {
		t.Fatalf("ttl ack = %+v, want seq=9 version=2 ttl=7200", ack)
	}
	if got, _ := h.store.Get("ackroom"); got.Content != "H" || got.TTL != 2*time.Hour {
		t.Fatalf("store content=%q ttl=%v", got.Content, got.TTL)
	}

	// Explicit sync request returns a full state snapshot.
	if err := conn.WriteJSON(map[string]any{"type": "sync"}); err != nil {
		t.Fatal(err)
	}
	st := readUntil("state")
	if st.Content != "H" || st.Version != 2 || len(st.Items) == 0 {
		t.Fatalf("sync state = %+v", st)
	}

	// Rejected batch (unknown parent) → error ack, then state resync.
	if err := conn.WriteJSON(map[string]any{
		"type": "ops",
		"seq":  10,
		"ops": []map[string]any{
			{"op": "ins", "id": "aaa:9", "after": "nope:1", "ch": "X"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	ack = readUntil("ack")
	if ack.Seq != 10 || ack.ErrMsg == "" {
		t.Fatalf("reject ack = %+v, want seq=10 with error", ack)
	}
	st = readUntil("state")
	if st.Content != "H" {
		t.Fatalf("resync state content = %q, want H", st.Content)
	}
}

func TestSaveBaseVersionConflict(t *testing.T) {
	h := newTestHandler()
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	put := func(body string) (*http.Response, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/clipboard/occ", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var data map[string]any
		_ = json.NewDecoder(res.Body).Decode(&data)
		return res, data
	}

	res, _ := put(`{"content":"v1","ttlSeconds":3600,"clientId":"a"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("seed status = %d", res.StatusCode)
	}
	res, _ = put(`{"content":"v2","ttlSeconds":3600,"clientId":"b"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("second status = %d", res.StatusCode)
	}

	// Stale base → 409 with current state so the client can merge.
	res, data := put(`{"content":"stomp","ttlSeconds":3600,"clientId":"a","baseVersion":1}`)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("stale base status = %d, want 409", res.StatusCode)
	}
	if data["content"] != "v2" || data["version"] != float64(2) || data["error"] == nil {
		t.Fatalf("conflict body = %v", data)
	}

	// Matching base → accepted.
	res, data = put(`{"content":"v3","ttlSeconds":3600,"clientId":"a","baseVersion":2}`)
	if res.StatusCode != http.StatusOK || data["version"] != float64(3) {
		t.Fatalf("matching base: status=%d body=%v", res.StatusCode, data)
	}

	// No baseVersion → legacy LWW still works.
	res, _ = put(`{"content":"v4","ttlSeconds":3600,"clientId":"c"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("legacy status = %d", res.StatusCode)
	}
}

// A subscriber must never receive an ops diff that skips versions: when its
// last delivered version lags by more than one (coalesced wakeups under load
// or a slow link), it gets a full snapshot instead.
func TestWebSocketNoOpsGapAfterCoalescedUpdates(t *testing.T) {
	h := newTestHandler()
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	// Seed room.
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/clipboard/gap", bytes.NewBufferString(saveJSON("x", 3600)))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	item, _ := h.store.Get("gap")
	parent := item.Doc.VisibleIDs()[0]

	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/clipboard/gap/ws?clientId=reader"
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	// Apply two batches back-to-back directly through the store, then ping
	// once — the reader wakes a single time with lastVersion two behind.
	if _, _, err := h.store.ApplyOps("gap", []crdt.Op{{Op: crdt.OpInsert, ID: "w:2", After: parent, Ch: "A"}}, 0, "w", store.Auth{}); err != nil {
		t.Fatal(err)
	}
	cur, _, err := h.store.ApplyOps("gap", []crdt.Op{{Op: crdt.OpInsert, ID: "w:3", After: "w:2", Ch: "B"}}, 0, "w", store.Auth{})
	if err != nil {
		t.Fatal(err)
	}
	h.noteOps("gap", cur.Version, cur.Generation, "w", []crdt.Op{{Op: crdt.OpInsert, ID: "w:3", After: "w:2", Ch: "B"}}, cur.Content)
	h.broker.ping("gap")

	for {
		var msg wsOutbound
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("read: %v", err)
		}
		if msg.Type == "ops" && msg.Version == cur.Version {
			t.Fatalf("got ops diff across a version gap (v%d) — must be a state snapshot", msg.Version)
		}
		if (msg.Type == "state" || msg.Type == "update") && msg.Version == cur.Version {
			if msg.Content != "xAB" {
				t.Fatalf("snapshot content = %q, want xAB", msg.Content)
			}
			return
		}
	}
}

func newTestHandlerWithOpts(opts Options) *Handler {
	staticFS, _ := fs.Sub(testStatic, "testdata")
	return New(store.New(), staticFS, nil, opts)
}

func TestWebSocketConnLimits(t *testing.T) {
	h := newTestHandlerWithOpts(Options{MaxWSConns: 2, MaxWSConnsPerIP: 1})
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/clipboard/lim/ws?clientId=a"
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("first dial should succeed: %v", err)
	}
	defer conn.Close()

	// Both dials come from the same IP (httptest loopback); the per-IP cap
	// (and the global cap) must reject the second with 503 before upgrade.
	conn2, resp, err := websocket.DefaultDialer.Dial(u+"2", nil)
	if err == nil {
		conn2.Close()
		t.Fatal("second dial should have been rejected")
	}
	if resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 rejection, got response %v err %v", resp, err)
	}
}

func TestWebSocketMessageRateLimit(t *testing.T) {
	h := newTestHandlerWithOpts(Options{WSMsgRate: 1, WSMsgBurst: 2})
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/clipboard/rate/ws?clientId=c"
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Drain the initial snapshot frames (state + cursor).
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	for i := 0; i < 4; i++ {
		var dump wsOutbound
		if err := conn.ReadJSON(&dump); err != nil {
			break
		}
	}

	// Burst=2: the first two inbound messages pass; the third exhausts the
	// bucket and the server must close the connection.
	for i := 0; i < 3; i++ {
		msg := fmt.Sprintf(`{"type":"ops","seq":%d,"ttlSeconds":3600}`, i+1)
		if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var dump wsOutbound
	if err := conn.ReadJSON(&dump); err == nil {
		t.Fatal("expected connection to be closed after exceeding message budget")
	}
}
