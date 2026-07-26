package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClipboardLifecycle(t *testing.T) {
	app := &server{store: newStore()}
	handler := app.routes()

	saveBody := bytes.NewBufferString(`{"content":"hello","ttlSeconds":3600}`)
	saveReq := httptest.NewRequest(http.MethodPut, "/api/clipboard/demo", saveBody)
	saveRes := httptest.NewRecorder()
	handler.ServeHTTP(saveRes, saveReq)

	if saveRes.Code != http.StatusOK {
		t.Fatalf("save status = %d, want %d; body: %s", saveRes.Code, http.StatusOK, saveRes.Body.String())
	}

	var saved clipboardResponse
	if err := json.Unmarshal(saveRes.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode save response: %v", err)
	}
	if saved.Content != "hello" || saved.TTLSeconds != 3600 || saved.Version != 1 || !saved.Exists {
		t.Fatalf("unexpected save response: %+v", saved)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/clipboard/demo", nil)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, getReq)

	if getRes.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d", getRes.Code, http.StatusOK)
	}

	var got clipboardResponse
	if err := json.Unmarshal(getRes.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if got.Content != "hello" || got.Version != 1 || !got.Exists {
		t.Fatalf("unexpected get response: %+v", got)
	}
}

func TestExpiredClipboardIsRemoved(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	st := newStore()
	st.now = func() time.Time { return now }
	app := &server{store: st}
	handler := app.routes()

	saveReq := httptest.NewRequest(http.MethodPut, "/api/clipboard/soon", bytes.NewBufferString(`{"content":"gone","ttlSeconds":1}`))
	saveRes := httptest.NewRecorder()
	handler.ServeHTTP(saveRes, saveReq)
	if saveRes.Code != http.StatusOK {
		t.Fatalf("save status = %d, want %d", saveRes.Code, http.StatusOK)
	}

	now = now.Add(2 * time.Second)
	getReq := httptest.NewRequest(http.MethodGet, "/api/clipboard/soon", nil)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, getReq)

	var got clipboardResponse
	if err := json.Unmarshal(getRes.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if got.Exists || got.Content != "" || got.Version != 0 {
		t.Fatalf("expired clipboard should look empty, got %+v", got)
	}
}

func TestRejectsInvalidKeysAndTTL(t *testing.T) {
	app := &server{store: newStore()}
	handler := app.routes()

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
			body:   `{"content":"x","ttlSeconds":0}`,
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
	app := &server{store: newStore()}
	handler := app.routes()

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
