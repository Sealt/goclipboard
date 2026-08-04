package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goclipboard/internal/crdt"
)

// Round-trip: rooms survive a store restart via disk snapshots, keeping
// content, version, generation and view key. Expired rooms are dropped and
// deleted rooms leave no snapshot behind.

func TestPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	s1 := New(
		WithClock(func() time.Time { return now }),
		WithPersistence(dir),
	)
	item, err := s1.Save("persistroom", "你好 world", time.Hour, "alice")
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	viewKey := item.ViewKey
	if viewKey == "" {
		t.Fatal("no view key generated")
	}
	gen := item.Generation
	ver := item.Version
	s1.Close()

	if _, err := os.Stat(filepath.Join(dir, "persistroom.json")); err != nil {
		t.Fatalf("snapshot file missing: %v", err)
	}

	s2 := New(
		WithClock(func() time.Time { return now.Add(time.Minute) }),
		WithPersistence(dir),
	)
	defer s2.Close()

	if n := s2.RestoredRoomCount(); n != 1 {
		t.Fatalf("restored rooms = %d, want 1", n)
	}
	got, ok := s2.Get("persistroom")
	if !ok {
		t.Fatal("room not restored")
	}
	if got.Content != "你好 world" {
		t.Fatalf("restored content = %q", got.Content)
	}
	if got.Version != ver || got.Generation != gen || got.ViewKey != viewKey {
		t.Fatalf("restored meta mismatch: version=%d gen=%d view=%q (want %d/%d/%q)",
			got.Version, got.Generation, got.ViewKey, ver, gen, viewKey)
	}
	if got.Doc == nil || got.Doc.Materialize() != "你好 world" {
		t.Fatal("restored doc does not materialize to saved content")
	}
}

func TestPersistenceSkipsExpiredAndDeleted(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	s1 := New(
		WithClock(func() time.Time { return now }),
		WithPersistence(dir),
	)
	if _, err := s1.Save("keep", "alive", 4*time.Hour, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s1.Save("gone", "expired", time.Hour, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s1.Save("deleted", "bye", time.Hour, "a"); err != nil {
		t.Fatal(err)
	}
	s1.Delete("deleted")
	s1.Close()

	// "gone" expired 1h after save; "keep" still live at now+2h.
	s2 := New(
		WithClock(func() time.Time { return now.Add(2 * time.Hour) }),
		WithPersistence(dir),
	)
	defer s2.Close()

	if _, ok := s2.Get("keep"); !ok {
		t.Fatal("live room should be restored")
	}
	if _, ok := s2.Get("gone"); ok {
		t.Fatal("expired room must not be restored")
	}
	if _, ok := s2.Get("deleted"); ok {
		t.Fatal("deleted room must not be restored")
	}
	if _, err := os.Stat(filepath.Join(dir, "gone.json")); err == nil {
		t.Fatal("expired snapshot file should be removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "deleted.json")); err == nil {
		t.Fatal("deleted snapshot file should be removed")
	}
}

func TestPersistenceSurvivesOpBatch(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	s1 := New(
		WithClock(func() time.Time { return now }),
		WithPersistence(dir),
	)
	// Ops path (WS) must also persist: create + insert.
	ops := []crdt.Op{{Op: crdt.OpInsert, ID: "site:1", After: "", Ch: "哈"}}
	item, _, err := s1.ApplyOps("oproom", ops, time.Hour, "site", Auth{})
	if err != nil {
		t.Fatalf("apply ops: %v", err)
	}
	if item.ViewKey == "" {
		t.Fatal("no view key on op-created room")
	}
	s1.Close()

	s2 := New(
		WithClock(func() time.Time { return now.Add(time.Minute) }),
		WithPersistence(dir),
	)
	defer s2.Close()
	got, ok := s2.Get("oproom")
	if !ok {
		t.Fatal("op-created room not restored")
	}
	if got.Content != "哈" || got.ViewKey != item.ViewKey {
		t.Fatalf("restored op room: content=%q view=%q", got.Content, got.ViewKey)
	}
}

// Legacy snapshots (pre-scope) stored the password under editPassword with no
// scope; they must restore as password-protected with scope "edit".
func TestPersistenceMigratesLegacyEditPassword(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	legacy := `{"key":"legacy","viewKey":"vk1","editPassword":"oldpw","content":"hi",
		"ttlSeconds":3600,"expiresAt":` + fmt.Sprint(now.Add(time.Hour).Unix()) + `,
		"version":1,"generation":1,"updatedAt":0,"updatedBy":"a","items":[]}`
	if err := os.WriteFile(filepath.Join(dir, "legacy.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	s := New(
		WithClock(func() time.Time { return now }),
		WithPersistence(dir),
	)
	defer s.Close()

	set, scope := s.PasswordInfo("legacy")
	if !set || scope != "edit" {
		t.Fatalf("legacy room PasswordInfo = %v %q, want true edit", set, scope)
	}
	if !s.PasswordOK("legacy", "oldpw") {
		t.Fatal("legacy password must verify")
	}
	if s.PasswordOK("legacy", "wrong") {
		t.Fatal("wrong password must fail")
	}
}

// Password + scope survive a round trip through the current snapshot format.
func TestPersistencePasswordScopeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	s1 := New(
		WithClock(func() time.Time { return now }),
		WithPersistence(dir),
	)
	if _, err := s1.Save("pwroom", "body", time.Hour, "a"); err != nil {
		t.Fatal(err)
	}
	if err := s1.SetPassword("pwroom", "", "view-pass", "view"); err != nil {
		t.Fatal(err)
	}
	s1.Close()

	// On-disk snapshot must never contain the plaintext password.
	raw, err := os.ReadFile(filepath.Join(dir, "pwroom.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "view-pass") {
		t.Fatalf("snapshot still has plaintext password: %s", raw)
	}
	if !strings.Contains(string(raw), "passwordHash") || !strings.Contains(string(raw), "passwordSalt") {
		t.Fatalf("snapshot missing hash fields: %s", raw)
	}

	s2 := New(
		WithClock(func() time.Time { return now.Add(time.Minute) }),
		WithPersistence(dir),
	)
	defer s2.Close()
	set, scope := s2.PasswordInfo("pwroom")
	if !set || scope != "view" {
		t.Fatalf("restored PasswordInfo = %v %q, want true view", set, scope)
	}
	if !s2.PasswordOK("pwroom", "view-pass") {
		t.Fatal("restored password must verify")
	}
	// In-memory item must not hold plaintext either.
	item, ok := s2.Peek("pwroom")
	if !ok || item.PasswordHash == "" || item.PasswordSalt == "" {
		t.Fatalf("in-memory lock missing hash/salt: %+v ok=%v", item, ok)
	}
}

// A persistence dir that cannot be created (a regular file sits on the path)
// must degrade to in-memory mode instead of hanging Close() forever on a
// writer goroutine that never started.
func TestCloseAfterMkdirFailure(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := New(WithPersistence(filepath.Join(blocker, "rooms")))
	// Store still serves from memory despite the failed persist dir.
	if _, err := s.Save("mem", "still works", time.Hour, "a"); err != nil {
		t.Fatalf("save after mkdir failure: %v", err)
	}
	s.Close() // must return promptly, not block on persistDone
}
