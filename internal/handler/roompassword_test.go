package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goclipboard/internal/model"

	"github.com/gorilla/websocket"
)

func putPassword(t *testing.T, srvURL, key string, body map[string]any) (*http.Response, map[string]any) {
	t.Helper()
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPut, srvURL+"/api/clipboard/"+key+"/password", bytes.NewReader(data))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put password: %v", err)
	}
	defer res.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode password response: %v", err)
	}
	return res, out
}

func TestRoomPasswordEditScope(t *testing.T) {
	h := newTestHandler()
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	// Create the room.
	putReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/clipboard/edroom", bytes.NewBufferString(saveJSON("hello", 3600)))
	putRes, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	putRes.Body.Close()

	// Lock with a manually chosen password, scope=edit (writes only).
	res, out := putPassword(t, srv.URL, "edroom", map[string]any{"password": "my-pass", "scope": "edit"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("lock status = %d: %v", res.StatusCode, out)
	}
	if out["passwordSet"] != true || out["scope"] != "edit" {
		t.Fatalf("lock response = %v, want passwordSet=true scope=edit", out)
	}

	// GET stays open for edit-scope rooms.
	getReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/clipboard/edroom", nil)
	getRes, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	defer getRes.Body.Close()
	var got model.ClipboardResponse
	json.NewDecoder(getRes.Body).Decode(&got)
	if got.Content != "hello" || !got.EditPasswordSet || got.PasswordScope != "edit" {
		t.Fatalf("GET on edit-scope room = %+v, want open content + scope=edit", got)
	}

	// Write without password → 403.
	writeReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/clipboard/edroom", bytes.NewBufferString(saveJSON("nope", 3600)))
	writeRes, err := http.DefaultClient.Do(writeReq)
	if err != nil {
		t.Fatal(err)
	}
	writeRes.Body.Close()
	if writeRes.StatusCode != http.StatusForbidden {
		t.Fatalf("write without password = %d, want 403", writeRes.StatusCode)
	}

	// Write with the password → 200.
	withBody, _ := json.Marshal(map[string]any{"content": "ok", "ttlSeconds": 3600, "password": "my-pass"})
	writeReq2, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/clipboard/edroom", bytes.NewReader(withBody))
	writeRes2, err := http.DefaultClient.Do(writeReq2)
	if err != nil {
		t.Fatal(err)
	}
	writeRes2.Body.Close()
	if writeRes2.StatusCode != http.StatusOK {
		t.Fatalf("write with password = %d, want 200", writeRes2.StatusCode)
	}
}

func TestRoomPasswordViewScopeGatesReads(t *testing.T) {
	h := newTestHandler()
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	putReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/clipboard/vroom", bytes.NewBufferString(saveJSON("secret content", 3600)))
	putRes, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	putRes.Body.Close()

	res, out := putPassword(t, srv.URL, "vroom", map[string]any{"password": "peek-pass", "scope": "view"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("lock status = %d: %v", res.StatusCode, out)
	}
	if out["scope"] != "view" {
		t.Fatalf("lock response scope = %v, want view", out["scope"])
	}

	// GET without a password → 401 + passwordScope, content withheld.
	getReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/clipboard/vroom", nil)
	getRes, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	defer getRes.Body.Close()
	var denied struct {
		Error         string `json:"error"`
		PasswordScope string `json:"passwordScope"`
	}
	json.NewDecoder(getRes.Body).Decode(&denied)
	if getRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET without password = %d, want 401", getRes.StatusCode)
	}
	if denied.PasswordScope != "view" {
		t.Fatalf("401 body passwordScope = %q, want view", denied.PasswordScope)
	}

	// Wrong password → 401.
	badReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/clipboard/vroom", nil)
	badReq.Header.Set("X-Goclip-Password", "wrong")
	badRes, err := http.DefaultClient.Do(badReq)
	if err != nil {
		t.Fatal(err)
	}
	badRes.Body.Close()
	if badRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET with wrong password = %d, want 401", badRes.StatusCode)
	}

	// Correct password via header → 200 with content.
	okReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/clipboard/vroom", nil)
	okReq.Header.Set("X-Goclip-Password", "peek-pass")
	okRes, err := http.DefaultClient.Do(okReq)
	if err != nil {
		t.Fatal(err)
	}
	defer okRes.Body.Close()
	var got model.ClipboardResponse
	json.NewDecoder(okRes.Body).Decode(&got)
	if okRes.StatusCode != http.StatusOK || got.Content != "secret content" {
		t.Fatalf("GET with password = %d content %q, want 200 + content", okRes.StatusCode, got.Content)
	}
	if got.PasswordScope != "view" || !got.EditPasswordSet {
		t.Fatalf("GET response lock fields = %+v, want scope=view + set", got)
	}

	// Correct password via query → 200 (CLI convenience).
	qReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/clipboard/vroom?password=peek-pass", nil)
	qRes, err := http.DefaultClient.Do(qReq)
	if err != nil {
		t.Fatal(err)
	}
	qRes.Body.Close()
	if qRes.StatusCode != http.StatusOK {
		t.Fatalf("GET with ?password= = %d, want 200", qRes.StatusCode)
	}

	// Writes also require the password (view scope implies write lock).
	writeReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/clipboard/vroom", bytes.NewBufferString(saveJSON("x", 3600)))
	writeRes, err := http.DefaultClient.Do(writeReq)
	if err != nil {
		t.Fatal(err)
	}
	writeRes.Body.Close()
	if writeRes.StatusCode != http.StatusForbidden {
		t.Fatalf("write without password = %d, want 403", writeRes.StatusCode)
	}
}

// wsFrame reads one JSON frame as a generic map (skips control frames).
func wsFrame(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var msg map[string]any
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("read frame: %v", err)
	}
	return msg
}

func waitForState(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	for i := 0; i < 20; i++ {
		msg := wsFrame(t, conn)
		if msg["type"] == "state" {
			return msg
		}
	}
	t.Fatal("no state frame")
	return nil
}

// waitForAck skips state/cursor/files frames and returns the next ack.
func waitForAck(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	for i := 0; i < 20; i++ {
		msg := wsFrame(t, conn)
		if msg["type"] == "ack" {
			return msg
		}
	}
	t.Fatal("no ack frame")
	return nil
}

func TestViewPasswordWebSocketFlow(t *testing.T) {
	h := newTestHandler()
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	putReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/clipboard/wsv", bytes.NewBufferString(saveJSON("private text", 3600)))
	putRes, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	putRes.Body.Close()
	putPassword(t, srv.URL, "wsv", map[string]any{"password": "s3cret", "scope": "view"})

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/clipboard/wsv/ws?clientId=editor"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Initial state is the locked frame: content withheld, password required.
	st := waitForState(t, conn)
	if st["passwordRequired"] != true {
		t.Fatalf("initial state passwordRequired = %v, want true", st["passwordRequired"])
	}
	// Empty content is omitted by omitempty, so missing/empty both mean "no
	// content leaked".
	if c, _ := st["content"].(string); c != "" {
		t.Fatalf("locked state leaked content: %q", c)
	}
	if st["passwordScope"] != "view" {
		t.Fatalf("locked state passwordScope = %v, want view", st["passwordScope"])
	}
	if st["viewKey"] != nil && st["viewKey"] != "" {
		t.Fatalf("locked state leaked viewKey: %v", st["viewKey"])
	}

	// Ops before auth are rejected with a clear error ack.
	if err := conn.WriteJSON(map[string]any{
		"type": "ops",
		"seq":  1,
		"ops": []map[string]any{
			{"op": "ins", "id": "editor:1", "after": "", "ch": "X"},
		},
		"ttlSeconds": 3600,
	}); err != nil {
		t.Fatal(err)
	}
	ack := waitForAck(t, conn)
	if ack["error"] != "view password required" {
		t.Fatalf("pre-auth ops ack = %v, want error 'view password required'", ack)
	}

	// Wrong password → explicit error ack, still locked.
	if err := conn.WriteJSON(map[string]any{"type": "auth", "seq": 2, "password": "nope"}); err != nil {
		t.Fatal(err)
	}
	ack = waitForAck(t, conn)
	if ack["error"] != "invalid view password" {
		t.Fatalf("wrong auth ack = %v, want error 'invalid view password'", ack)
	}

	// Correct password → full state with content.
	if err := conn.WriteJSON(map[string]any{"type": "auth", "seq": 3, "password": "s3cret"}); err != nil {
		t.Fatal(err)
	}
	st = waitForState(t, conn)
	if st["passwordRequired"] != nil && st["passwordRequired"] == true {
		t.Fatalf("still locked after auth: %v", st)
	}
	if st["content"] != "private text" {
		t.Fatalf("post-auth content = %q, want 'private text'", st["content"])
	}

	// After auth, ops no longer need the password — the session already
	// proved it once (the room password gates viewing and editing alike).
	if err := conn.WriteJSON(map[string]any{
		"type": "ops",
		"seq":  4,
		"ops": []map[string]any{
			{"op": "ins", "id": "editor:2", "after": "", "ch": "Y"},
		},
		"ttlSeconds": 3600,
	}); err != nil {
		t.Fatal(err)
	}
	ack = waitForAck(t, conn)
	if ack["error"] != nil {
		t.Fatalf("post-auth ops without password ack = %v, want clean ack", ack)
	}

	// A second connection that never authenticates still cannot edit.
	wsURL2 := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/clipboard/wsv/ws?clientId=peek2"
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL2, nil)
	if err != nil {
		t.Fatalf("dial second: %v", err)
	}
	defer conn2.Close()
	waitForState(t, conn2) // locked frame
	if err := conn2.WriteJSON(map[string]any{
		"type": "ops",
		"seq":  9,
		"ops": []map[string]any{
			{"op": "ins", "id": "peek2:1", "after": "", "ch": "Z"},
		},
		"ttlSeconds": 3600,
	}); err != nil {
		t.Fatal(err)
	}
	ack = waitForAck(t, conn2)
	if ack["error"] != "view password required" {
		t.Fatalf("unauthenticated ops ack = %v, want 'view password required'", ack)
	}
}

func TestRoomPasswordRotateAndUnlock(t *testing.T) {
	h := newTestHandler()
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	putReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/clipboard/rot", bytes.NewBufferString(saveJSON("x", 3600)))
	putRes, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	putRes.Body.Close()

	// Lock with a manual password (scope omitted → defaults to edit).
	res, out := putPassword(t, srv.URL, "rot", map[string]any{"password": "first"})
	if res.StatusCode != http.StatusOK || out["scope"] != "edit" {
		t.Fatalf("lock = %d %v, want 200 scope=edit", res.StatusCode, out)
	}

	// Rotate without the current password → 403.
	res, _ = putPassword(t, srv.URL, "rot", map[string]any{"password": "second", "currentPassword": "wrong"})
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("rotate with wrong current = %d, want 403", res.StatusCode)
	}

	// Rotate + switch scope to view with the current password → 200.
	res, out = putPassword(t, srv.URL, "rot", map[string]any{"password": "second", "currentPassword": "first", "scope": "view"})
	if res.StatusCode != http.StatusOK || out["scope"] != "view" {
		t.Fatalf("rotate = %d %v, want 200 scope=view", res.StatusCode, out)
	}

	// New password gates reads now.
	getReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/clipboard/rot", nil)
	getRes, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	getRes.Body.Close()
	if getRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET after scope switch = %d, want 401", getRes.StatusCode)
	}

	// Unlock (empty password) requires the current password too.
	res, _ = putPassword(t, srv.URL, "rot", map[string]any{"password": "", "currentPassword": "second"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("unlock = %d, want 200", res.StatusCode)
	}
	getReq2, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/clipboard/rot", nil)
	getRes2, err := http.DefaultClient.Do(getReq2)
	if err != nil {
		t.Fatal(err)
	}
	defer getRes2.Body.Close()
	var got model.ClipboardResponse
	json.NewDecoder(getRes2.Body).Decode(&got)
	if got.EditPasswordSet || got.PasswordScope != "" {
		t.Fatalf("after unlock lock fields = %+v, want clear", got)
	}
}

// Locking a room that does not exist yet (fresh page, nothing typed) must
// create the room so the password has somewhere to live — otherwise the
// first "set password" on a brand-new room would 404 and look like a no-op.
func TestRoomPasswordCreatesMissingRoom(t *testing.T) {
	h := newTestHandler()
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	// Never-created room: set password with scope=view → 200 and locked.
	res, out := putPassword(t, srv.URL, "brandnew", map[string]any{"password": "pw", "scope": "view"})
	if res.StatusCode != http.StatusOK || out["passwordSet"] != true || out["scope"] != "view" {
		t.Fatalf("set password on missing room = %d %v, want 200 set view", res.StatusCode, out)
	}
	// The room now exists and reads require the password.
	getReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/clipboard/brandnew", nil)
	getRes, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	getRes.Body.Close()
	if getRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET on locked fresh room = %d, want 401", getRes.StatusCode)
	}

	// Empty unlock on a missing room is a harmless no-op success.
	res2, out2 := putPassword(t, srv.URL, "neverexisted", map[string]any{"password": ""})
	if res2.StatusCode != http.StatusOK || out2["passwordSet"] != false {
		t.Fatalf("unlock missing room = %d %v, want 200 not set", res2.StatusCode, out2)
	}
}

func TestRoomPasswordInvalidScopeRejected(t *testing.T) {
	h := newTestHandler()
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	putReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/clipboard/badscope", bytes.NewBufferString(saveJSON("x", 3600)))
	putRes, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	putRes.Body.Close()

	res, out := putPassword(t, srv.URL, "badscope", map[string]any{"password": "p", "scope": "delete"})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid scope = %d %v, want 400", res.StatusCode, out)
	}
}
