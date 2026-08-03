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

// View links: the server issues a per-room viewKey; ?view=<key> WebSocket
// sessions are read-only (ops rejected), wrong keys are refused.

func TestViewKeyInGetAndState(t *testing.T) {
	h := newTestHandler()
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	putReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/clipboard/viewroom", bytes.NewBufferString(saveJSON("secret", 3600)))
	putRes, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	defer putRes.Body.Close()
	var saved model.ClipboardResponse
	if err := json.NewDecoder(putRes.Body).Decode(&saved); err != nil {
		t.Fatal(err)
	}
	if saved.ViewKey == "" {
		t.Fatal("created room must carry a viewKey")
	}
	if len(saved.ViewKey) < 16 {
		t.Fatalf("viewKey too short: %q", saved.ViewKey)
	}

	// Same key on repeat GET (stable across requests).
	getReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/clipboard/viewroom", nil)
	getRes, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	defer getRes.Body.Close()
	var got model.ClipboardResponse
	if err := json.NewDecoder(getRes.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ViewKey != saved.ViewKey {
		t.Fatalf("viewKey changed between requests: %q vs %q", got.ViewKey, saved.ViewKey)
	}

	// WS initial state carries the viewKey too.
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/clipboard/viewroom/ws?clientId=editor"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	gotViewKey := ""
	for i := 0; i < 4; i++ {
		var msg wsOutbound
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("read frame: %v", err)
		}
		if msg.Type == "state" {
			gotViewKey = msg.ViewKey
			break
		}
	}
	if gotViewKey != saved.ViewKey {
		t.Fatalf("ws state viewKey = %q, want %q", gotViewKey, saved.ViewKey)
	}
}

func TestViewKeySessionIsReadOnly(t *testing.T) {
	h := newTestHandler()
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	putReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/clipboard/ro", bytes.NewBufferString(saveJSON("base", 3600)))
	putRes, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	defer putRes.Body.Close()
	var saved model.ClipboardResponse
	json.NewDecoder(putRes.Body).Decode(&saved)

	// Wrong view key → 403 before upgrade.
	badURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/clipboard/ro/ws?clientId=peek&view=deadbeef"
	if _, _, err := websocket.DefaultDialer.Dial(badURL, nil); err == nil {
		t.Fatal("wrong view key must be refused")
	}

	// Valid view key → read-only session: ops are ignored, state still flows.
	viewURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/clipboard/ro/ws?clientId=peek&view=" + saved.ViewKey
	conn, _, err := websocket.DefaultDialer.Dial(viewURL, nil)
	if err != nil {
		t.Fatalf("dial view session: %v", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	readState := func() wsOutbound {
		t.Helper()
		for i := 0; i < 20; i++ {
			var msg wsOutbound
			if err := conn.ReadJSON(&msg); err != nil {
				t.Fatalf("read waiting for state: %v", err)
			}
			if msg.Type == "state" {
				return msg
			}
		}
		t.Fatal("no state frame")
		return wsOutbound{}
	}
	st := readState()
	if st.Content != "base" {
		t.Fatalf("read-only state content = %q, want base", st.Content)
	}

	// Ops from a read-only session must not change the room.
	if err := conn.WriteJSON(map[string]any{
		"type": "ops",
		"seq":  1,
		"ops": []map[string]any{
			{"op": "ins", "id": "peek:1", "after": "", "ch": "X"},
		},
		"ttlSeconds": 3600,
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	item, ok := h.store.Get("ro")
	if !ok {
		t.Fatal("room vanished")
	}
	if item.Content != "base" {
		t.Fatalf("read-only ops mutated room: %q", item.Content)
	}
	if item.Version != 1 {
		t.Fatalf("read-only ops bumped version: %d", item.Version)
	}
}

func TestViewKeyMissingRoomRefused(t *testing.T) {
	h := newTestHandler()
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/clipboard/absent/ws?clientId=peek&view=deadbeef"
	if _, _, err := websocket.DefaultDialer.Dial(u, nil); err == nil {
		t.Fatal("view session on missing room must be refused")
	}
}
