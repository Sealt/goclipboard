package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"goclipboard/internal/model"
)

func historyURL(srvURL, key string) string {
	return srvURL + "/api/clipboard/" + key + "/history"
}

func getHistory(t *testing.T, url string, password string) (*http.Response, []model.HistoryEntry) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if password != "" {
		req.Header.Set("X-Goclip-Password", password)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET history: %v", err)
	}
	defer res.Body.Close()
	var body struct {
		Snapshots []model.HistoryEntry `json:"snapshots"`
	}
	if res.StatusCode == http.StatusOK {
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			t.Fatalf("decode history: %v", err)
		}
	}
	return res, body.Snapshots
}

func TestHistoryOpenRoom(t *testing.T) {
	h := newTestHandler()
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	putReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/clipboard/hopen", bytes.NewBufferString(saveJSON("alpha", 3600)))
	putRes, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	putRes.Body.Close()
	if putRes.StatusCode != http.StatusOK {
		t.Fatalf("put status %d", putRes.StatusCode)
	}

	res, snaps := getHistory(t, historyURL(srv.URL, "hopen"), "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET history status %d", res.StatusCode)
	}
	if len(snaps) != 1 || snaps[0].Text != "alpha" {
		t.Fatalf("snapshots = %+v", snaps)
	}

	// Empty room (never saved): empty trail, not 404.
	res2, snaps2 := getHistory(t, historyURL(srv.URL, "missing-room"), "")
	if res2.StatusCode != http.StatusOK || len(snaps2) != 0 {
		t.Fatalf("missing room: status=%d snaps=%+v", res2.StatusCode, snaps2)
	}
}

func TestHistoryRequiresPasswordWhenLocked(t *testing.T) {
	h := newTestHandler()
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	putReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/clipboard/hedit", bytes.NewBufferString(saveJSON("secret-hist", 3600)))
	putRes, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	putRes.Body.Close()

	// Edit-scope lock: content GET stays open, but history requires password.
	res, _ := putPassword(t, srv.URL, "hedit", map[string]any{"password": "hist-pass", "scope": "edit"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("lock status %d", res.StatusCode)
	}

	getRes, _ := getHistory(t, historyURL(srv.URL, "hedit"), "")
	if getRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("history without password = %d, want 401", getRes.StatusCode)
	}

	// Password-failure responses carry passwordScope so clients can tell a
	// room-password failure from an admin/file-password one.
	rawReq, _ := http.NewRequest(http.MethodGet, historyURL(srv.URL, "hedit"), nil)
	rawRes, err := http.DefaultClient.Do(rawReq)
	if err != nil {
		t.Fatal(err)
	}
	defer rawRes.Body.Close()
	if rawRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("raw history GET = %d, want 401", rawRes.StatusCode)
	}
	var errBody struct {
		Error         string `json:"error"`
		PasswordScope string `json:"passwordScope"`
	}
	if err := json.NewDecoder(rawRes.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode 401 body: %v", err)
	}
	if errBody.PasswordScope != model.PasswordScopeView {
		t.Fatalf("passwordScope = %q, want %q", errBody.PasswordScope, model.PasswordScopeView)
	}

	getRes2, snaps := getHistory(t, historyURL(srv.URL, "hedit"), "hist-pass")
	if getRes2.StatusCode != http.StatusOK {
		t.Fatalf("history with password = %d", getRes2.StatusCode)
	}
	if len(snaps) < 1 || snaps[0].Text != "secret-hist" {
		t.Fatalf("snapshots = %+v", snaps)
	}

	// Wrong password.
	getRes3, _ := getHistory(t, historyURL(srv.URL, "hedit"), "wrong")
	if getRes3.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password = %d, want 401", getRes3.StatusCode)
	}
}

func TestHistoryManualCaptureAndClear(t *testing.T) {
	h := newTestHandler()
	// Use a store with a controllable clock so we can force auto + manual entries.
	// newTestHandler uses real clock; manual capture of same text is a no-op, so
	// change content via PUT then POST capture after throttle isn't needed for clear.
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	putReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/clipboard/hclear", bytes.NewBufferString(saveJSON("v1", 3600)))
	putRes, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	putRes.Body.Close()

	// Manual capture (same text) is fine / no-op growth.
	postReq, _ := http.NewRequest(http.MethodPost, historyURL(srv.URL, "hclear"), bytes.NewBufferString("{}"))
	postRes, err := http.DefaultClient.Do(postReq)
	if err != nil {
		t.Fatal(err)
	}
	postRes.Body.Close()
	if postRes.StatusCode != http.StatusOK {
		t.Fatalf("POST capture status %d", postRes.StatusCode)
	}

	// Clear trail.
	delReq, _ := http.NewRequest(http.MethodDelete, historyURL(srv.URL, "hclear"), nil)
	delRes, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatal(err)
	}
	defer delRes.Body.Close()
	if delRes.StatusCode != http.StatusOK {
		t.Fatalf("DELETE history status %d", delRes.StatusCode)
	}
	var body struct {
		Snapshots []model.HistoryEntry `json:"snapshots"`
	}
	json.NewDecoder(delRes.Body).Decode(&body)
	if len(body.Snapshots) != 0 {
		t.Fatalf("after clear snapshots = %+v", body.Snapshots)
	}

	res, snaps := getHistory(t, historyURL(srv.URL, "hclear"), "")
	if res.StatusCode != http.StatusOK || len(snaps) != 0 {
		t.Fatalf("GET after clear: status=%d snaps=%+v", res.StatusCode, snaps)
	}
}

func TestHistoryClearRequiresPassword(t *testing.T) {
	h := newTestHandler()
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	putReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/clipboard/hlock", bytes.NewBufferString(saveJSON("v1", 3600)))
	putRes, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	putRes.Body.Close()
	putPassword(t, srv.URL, "hlock", map[string]any{"password": "p", "scope": "edit"})

	delReq, _ := http.NewRequest(http.MethodDelete, historyURL(srv.URL, "hlock"), nil)
	delRes, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatal(err)
	}
	delRes.Body.Close()
	if delRes.StatusCode != http.StatusForbidden {
		t.Fatalf("clear without password = %d, want 403", delRes.StatusCode)
	}

	delReq2, _ := http.NewRequest(http.MethodDelete, historyURL(srv.URL, "hlock"), nil)
	delReq2.Header.Set("X-Goclip-Password", "p")
	delRes2, err := http.DefaultClient.Do(delReq2)
	if err != nil {
		t.Fatal(err)
	}
	delRes2.Body.Close()
	if delRes2.StatusCode != http.StatusOK {
		t.Fatalf("clear with password = %d", delRes2.StatusCode)
	}
}
