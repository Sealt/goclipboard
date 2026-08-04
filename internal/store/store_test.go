package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"goclipboard/internal/crdt"
	"goclipboard/internal/model"
)

func mustSave(t *testing.T, st *Store, key, content string, ttl time.Duration, by string) {
	t.Helper()
	if _, err := st.Save(key, content, ttl, by); err != nil {
		t.Fatalf("Save(%q): %v", key, err)
	}
}

func TestGet(t *testing.T) {
	st := New()
	mustSave(t, st, "demo", "hello", time.Hour, "a")

	item, ok := st.Get("demo")
	if !ok {
		t.Fatal("expected item to exist")
	}
	if item.Content != "hello" || item.Version != 1 || item.UpdatedBy != "a" {
		t.Fatalf("unexpected item: %+v", item)
	}
}

func TestGetNotFound(t *testing.T) {
	st := New()
	_, ok := st.Get("nonexistent")
	if ok {
		t.Fatal("expected item not to exist")
	}
}

func TestGetExpired(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	st := New(WithClock(func() time.Time { return now }))
	mustSave(t, st, "soon", "gone", time.Second, "")

	now = now.Add(2 * time.Second)
	_, ok := st.Get("soon")
	if ok {
		t.Fatal("expired item should not exist")
	}

	_, ok = st.Get("soon")
	if ok {
		t.Fatal("expired item should be permanently removed")
	}
}

func TestGenerationIncreasesAcrossExpiryAndRecreation(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	st := New(WithClock(func() time.Time { return now }))

	first, err := st.Save("room", "old", time.Second, "a")
	if err != nil {
		t.Fatal(err)
	}
	if first.Generation <= 0 || first.Version != 1 {
		t.Fatalf("first identity = generation %d version %d, want positive/1", first.Generation, first.Version)
	}

	now = now.Add(2 * time.Second)
	if _, ok := st.Get("room"); ok {
		t.Fatal("expired room should not exist")
	}

	second, err := st.Save("room", "new", time.Hour, "b")
	if err != nil {
		t.Fatal(err)
	}
	if second.Generation <= first.Generation || second.Version != 1 {
		t.Fatalf("recreated identity = generation %d version %d, want >%d/1", second.Generation, second.Version, first.Generation)
	}
}

func TestGenerationIncreasesAcrossDeleteAndRecreation(t *testing.T) {
	st := New()
	first, err := st.Save("room", "old", time.Hour, "a")
	if err != nil {
		t.Fatal(err)
	}
	st.Delete("room")

	second, err := st.Save("room", "new", time.Hour, "b")
	if err != nil {
		t.Fatal(err)
	}
	if second.Generation != first.Generation+1 || second.Version != 1 {
		t.Fatalf("recreated identity = generation %d version %d, want %d/1", second.Generation, second.Version, first.Generation+1)
	}
}

func TestApplyOpsRecreatesExpiredRoomWithNewGeneration(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	st := New(WithClock(func() time.Time { return now }))
	first, err := st.Save("room", "old", time.Second, "a")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)

	second, changed, err := st.ApplyOps("room", []crdt.Op{{Op: crdt.OpInsert, ID: "b:1", Ch: "N"}}, time.Hour, "b", Auth{})
	if err != nil {
		t.Fatal(err)
	}
	if !changed || second.Content != "N" || second.Version != 1 || second.Generation != first.Generation+1 {
		t.Fatalf("recreated ApplyOps result = content %q version %d generation %d changed %v, want N/1/%d/true", second.Content, second.Version, second.Generation, changed, first.Generation+1)
	}
}

func TestSaveNew(t *testing.T) {
	st := New()
	item, err := st.Save("key1", "content", time.Minute, "c1")
	if err != nil {
		t.Fatal(err)
	}

	if item.Content != "content" {
		t.Fatalf("content = %q, want %q", item.Content, "content")
	}
	if item.Version != 1 {
		t.Fatalf("version = %d, want 1", item.Version)
	}
	if item.TTL != time.Minute {
		t.Fatalf("ttl = %v, want %v", item.TTL, time.Minute)
	}
}

func TestSaveLastWriteWins(t *testing.T) {
	st := New()
	mustSave(t, st, "key1", "v1", time.Hour, "a")
	item, err := st.Save("key1", "v2", time.Hour, "b")
	if err != nil {
		t.Fatal(err)
	}

	if item.Content != "v2" {
		t.Fatalf("content = %q, want %q", item.Content, "v2")
	}
	if item.Version != 2 {
		t.Fatalf("version = %d, want 2", item.Version)
	}
	if item.UpdatedBy != "b" {
		t.Fatalf("updatedBy = %q, want b", item.UpdatedBy)
	}
}

func TestSaveIdenticalDoesNotBumpVersion(t *testing.T) {
	st := New()
	first, err := st.Save("key1", "same", time.Hour, "a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.Save("key1", "same", time.Hour, "b")
	if err != nil {
		t.Fatal(err)
	}
	if second.Version != first.Version {
		t.Fatalf("identical save bumped version %d -> %d", first.Version, second.Version)
	}
	if second.UpdatedBy != "b" {
		t.Fatalf("updatedBy = %q, want b", second.UpdatedBy)
	}
}

func TestDelete(t *testing.T) {
	st := New()
	mustSave(t, st, "key1", "content", time.Hour, "")

	st.Delete("key1")
	_, ok := st.Get("key1")
	if ok {
		t.Fatal("deleted item should not exist")
	}

	st.Delete("nonexistent")
}

func TestCleanupExpired(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var expired []string
	st := New(
		WithClock(func() time.Time { return now }),
		WithOnExpire(func(key string) { expired = append(expired, key) }),
	)

	mustSave(t, st, "keep", "alive", time.Hour, "")
	mustSave(t, st, "gone", "expired", time.Second, "")

	now = now.Add(2 * time.Second)
	st.CleanupExpired()

	_, ok := st.Get("keep")
	if !ok {
		t.Fatal("non-expired item should still exist")
	}
	_, ok = st.Get("gone")
	if ok {
		t.Fatal("expired item should be removed")
	}
	if len(expired) != 1 || expired[0] != "gone" {
		t.Fatalf("onExpire calls = %v, want [gone]", expired)
	}
}

func TestConcurrentAccess(t *testing.T) {
	st := New()
	var wg sync.WaitGroup

	mustSave(t, st, "key", "seed", time.Hour, "")

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, _ = st.Save("key", "value", time.Hour, "c")
			st.Get("key")
		}(i)
	}

	wg.Wait()
	item, ok := st.Get("key")
	if !ok {
		t.Fatal("item should exist after concurrent access")
	}
	if item.Version == 0 {
		t.Fatal("version should be > 0")
	}
}

func TestVersionMonotonic(t *testing.T) {
	st := New()
	for i := 1; i <= 5; i++ {
		item, err := st.Save("key", "value-"+string(rune('0'+i)), time.Hour, "")
		if err != nil {
			t.Fatal(err)
		}
		if item.Version != int64(i) {
			t.Fatalf("version = %d, want %d", item.Version, i)
		}
	}
}

func TestCleanupEmptyStore(t *testing.T) {
	st := New(WithOnExpire(func(key string) {
		t.Fatal("onExpire should not be called for empty store")
	}))
	st.CleanupExpired()
}

func TestStartCleanup(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var expired []string
	var mu sync.Mutex
	st := New(
		WithClock(func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return now
		}),
		WithOnExpire(func(key string) {
			mu.Lock()
			defer mu.Unlock()
			expired = append(expired, key)
		}),
	)

	mustSave(t, st, "short", "x", 50*time.Millisecond, "")

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		st.StartCleanup(stop, 20*time.Millisecond)
	}()

	mu.Lock()
	now = now.Add(100 * time.Millisecond)
	mu.Unlock()
	time.Sleep(50 * time.Millisecond)

	close(stop)
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(expired) == 0 {
		t.Fatal("expected at least one cleanup to have run")
	}
}

func TestApplyOpsConcurrentMerge(t *testing.T) {
	st := New()
	base, err := st.Save("room", "x", time.Hour, "seed")
	if err != nil {
		t.Fatal(err)
	}
	// seed site builds x as seed:1
	ids := base.Doc.VisibleIDs()
	if len(ids) != 1 {
		t.Fatalf("visible ids = %v", ids)
	}
	parent := ids[0]

	// Lamport clocks must exceed seed's max (1).
	opsA := []crdt.Op{{Op: crdt.OpInsert, ID: "a:2", After: parent, Ch: "A"}}
	opsB := []crdt.Op{{Op: crdt.OpInsert, ID: "b:2", After: parent, Ch: "B"}}

	if _, _, err := st.ApplyOps("room", opsA, 0, "a", Auth{}); err != nil {
		t.Fatal(err)
	}
	item, _, err := st.ApplyOps("room", opsB, 0, "b", Auth{})
	if err != nil {
		t.Fatal(err)
	}
	// Same clock 2: site a < b → xAB
	if item.Content != "xAB" {
		t.Fatalf("content = %q, want xAB", item.Content)
	}
	if item.Version != 3 {
		t.Fatalf("version = %d, want 3", item.Version)
	}
}

func TestApplyOpsIdempotentNoVersionBump(t *testing.T) {
	st := New()
	mustSave(t, st, "room", "z", time.Hour, "s")
	op := []crdt.Op{{Op: crdt.OpInsert, ID: "s:1", After: "", Ch: "z"}}
	// s:1 already exists from BuildFromString
	item, _, err := st.ApplyOps("room", op, 0, "s", Auth{})
	if err != nil {
		t.Fatal(err)
	}
	if item.Version != 1 {
		t.Fatalf("version = %d, want 1 (no-op)", item.Version)
	}
}

func TestMaxRooms(t *testing.T) {
	st := New(WithLimits(2, DefaultMaxTotalBytes))
	mustSave(t, st, "a", "1", time.Hour, "")
	mustSave(t, st, "b", "2", time.Hour, "")

	_, err := st.Save("c", "3", time.Hour, "")
	if !errors.Is(err, ErrTooManyRooms) {
		t.Fatalf("err = %v, want ErrTooManyRooms", err)
	}

	// Updates to existing rooms still work.
	if _, err := st.Save("a", "1-updated", time.Hour, ""); err != nil {
		t.Fatalf("update existing: %v", err)
	}

	st.Delete("b")
	if _, err := st.Save("c", "3", time.Hour, ""); err != nil {
		t.Fatalf("after free room: %v", err)
	}
}

func TestMemoryLimitBlocksGrowth(t *testing.T) {
	// Budget fits one tiny room including its auto-captured history entry;
	// a second room does not.
	budget := estimateBytes("x", 1) + historyBytes([]model.HistoryEntry{{Text: "x"}})
	st := New(WithLimits(100, budget))
	mustSave(t, st, "tiny", "x", time.Hour, "")

	rooms, total := st.Stats()
	if rooms != 1 || total <= 0 {
		t.Fatalf("stats rooms=%d total=%d", rooms, total)
	}

	_, err := st.Save("big", "yy", time.Hour, "")
	if !errors.Is(err, ErrMemoryLimit) {
		t.Fatalf("err = %v, want ErrMemoryLimit", err)
	}

	// Existing room may still shrink or stay same size (new history may
	// append when text changes; keep within budget by using same budget
	// headroom from the second-room rejection).
	if _, err := st.Save("tiny", "z", time.Hour, ""); err != nil {
		t.Fatalf("shrink/update existing: %v", err)
	}
}

func TestMemoryLimitAllowsShrinkAndSameSize(t *testing.T) {
	st := New(WithLimits(10, estimateBytes(strings.Repeat("a", 100), 100)+roomBaseBytes))
	content := strings.Repeat("a", 100)
	mustSave(t, st, "room", content, time.Hour, "")

	// Same size refresh
	if _, err := st.Save("room", content, time.Hour, "x"); err != nil {
		t.Fatalf("same size: %v", err)
	}
	// Shrink
	if _, err := st.Save("room", "ok", time.Hour, "x"); err != nil {
		t.Fatalf("shrink: %v", err)
	}
}

func TestContentTooLarge(t *testing.T) {
	st := New()
	big := strings.Repeat("x", MaxContentBytes+1)
	_, err := st.Save("k", big, time.Hour, "")
	if !errors.Is(err, ErrContentTooLarge) {
		t.Fatalf("err = %v, want ErrContentTooLarge", err)
	}
}

func TestExpireFreesBudget(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Budget fits exactly one short room including auto history.
	budget := estimateBytes("aaaa", 4) + historyBytes([]model.HistoryEntry{{Text: "aaaa"}})
	st := New(
		WithClock(func() time.Time { return now }),
		WithLimits(10, budget),
	)
	mustSave(t, st, "temp", "aaaa", time.Second, "")
	_, total1 := st.Stats()

	// Second room should not fit while first is live.
	if _, err := st.Save("other", "bbbb", time.Hour, ""); !errors.Is(err, ErrMemoryLimit) {
		t.Fatalf("err = %v, want ErrMemoryLimit", err)
	}

	now = now.Add(2 * time.Second)
	st.CleanupExpired()
	rooms, total2 := st.Stats()
	if rooms != 0 || total2 != 0 {
		t.Fatalf("after expire rooms=%d total=%d (was %d)", rooms, total2, total1)
	}

	// Budget free again.
	if _, err := st.Save("next", "bbbb", time.Hour, ""); err != nil {
		t.Fatalf("after expire free: %v", err)
	}
}

func TestApplyOpsRespectsMemoryLimit(t *testing.T) {
	// Room for seed "a" plus a few inserts only.
	st := New(WithLimits(10, estimateBytes("a", 1)+bytesPerCRDTItem*3))
	mustSave(t, st, "room", "a", time.Hour, "s")

	// Flood inserts until budget is hit (or we give up).
	var hit bool
	after := "s:1"
	for clock := int64(2); clock < 50; clock++ {
		id := crdt.FormatID("a", clock)
		ops := []crdt.Op{{Op: crdt.OpInsert, ID: id, After: after, Ch: "x"}}
		_, _, err := st.ApplyOps("room", ops, 0, "a", Auth{})
		if errors.Is(err, ErrMemoryLimit) {
			hit = true
			break
		}
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		after = id
	}
	if !hit {
		t.Fatal("expected ErrMemoryLimit from ApplyOps growth")
	}
}

func TestApplyOpsTTLChangeBumpsVersion(t *testing.T) {
	st := New()
	mustSave(t, st, "room", "z", time.Hour, "s")

	// Same TTL: pure refresh, no version bump.
	item, changed, err := st.ApplyOps("room", nil, time.Hour, "s", Auth{})
	if err != nil {
		t.Fatal(err)
	}
	if changed || item.Version != 1 {
		t.Fatalf("refresh: changed=%v version=%d, want false/1", changed, item.Version)
	}

	// Different TTL: version bump so WS subscribers rebroadcast expiry.
	item, changed, err = st.ApplyOps("room", nil, 2*time.Hour, "s", Auth{})
	if err != nil {
		t.Fatal(err)
	}
	if changed || item.Version != 2 {
		t.Fatalf("ttl change: changed=%v version=%d, want false/2", changed, item.Version)
	}
	if item.TTL != 2*time.Hour {
		t.Fatalf("ttl = %v, want 2h", item.TTL)
	}
}

func TestApplyOpsEmptyBatchCreatesRoom(t *testing.T) {
	st := New()
	item, changed, err := st.ApplyOps("fresh", nil, time.Hour, "s", Auth{})
	if err != nil {
		t.Fatal(err)
	}
	if changed || item.Version != 1 || item.Content != "" {
		t.Fatalf("got changed=%v version=%d content=%q", changed, item.Version, item.Content)
	}
	if _, ok := st.Get("fresh"); !ok {
		t.Fatal("room not created")
	}
}

func TestApplyOpsReportsContentChanged(t *testing.T) {
	st := New()
	mustSave(t, st, "room", "z", time.Hour, "s")
	op := []crdt.Op{{Op: crdt.OpInsert, ID: "a:2", After: "s:1", Ch: "!"}}

	item, changed, err := st.ApplyOps("room", op, 0, "a", Auth{})
	if err != nil {
		t.Fatal(err)
	}
	if !changed || item.Version != 2 {
		t.Fatalf("first apply: changed=%v version=%d, want true/2", changed, item.Version)
	}

	// Re-applying the same batch (client resend after ack loss) is a no-op.
	item, changed, err = st.ApplyOps("room", op, 0, "a", Auth{})
	if err != nil {
		t.Fatal(err)
	}
	if changed || item.Version != 2 {
		t.Fatalf("resend: changed=%v version=%d, want false/2", changed, item.Version)
	}
}

func TestSaveWithBaseConflict(t *testing.T) {
	st := New()
	mustSave(t, st, "room", "v1", time.Hour, "a") // version 1
	mustSave(t, st, "room", "v2", time.Hour, "b") // version 2

	// Stale base → conflict, nothing written.
	_, err := st.SaveWithBase("room", "stomp", time.Hour, "a", 1, Auth{})
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("err = %v, want ErrVersionConflict", err)
	}
	got, _ := st.Get("room")
	if got.Content != "v2" {
		t.Fatalf("content = %q, want v2 untouched", got.Content)
	}

	// Matching base → accepted.
	item, err := st.SaveWithBase("room", "v3", time.Hour, "a", 2, Auth{})
	if err != nil {
		t.Fatal(err)
	}
	if item.Content != "v3" || item.Version != 3 {
		t.Fatalf("item = %q v%d, want v3/3", item.Content, item.Version)
	}

	// Base on a missing room → conflict (room may have expired).
	if _, err := st.SaveWithBase("other", "x", time.Hour, "a", 5, Auth{}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("missing room err = %v, want ErrVersionConflict", err)
	}

	// baseVersion 0 keeps legacy unconditional LWW.
	if _, err := st.SaveWithBase("room", "v4", time.Hour, "c", 0, Auth{}); err != nil {
		t.Fatalf("legacy save: %v", err)
	}
}

// --- Peek (no-clone read) contract -------------------------------------------

func TestPeekSharesDocument(t *testing.T) {
	st := New()
	mustSave(t, st, "demo", "hello world", time.Hour, "a")

	first, ok1 := st.Peek("demo")
	second, ok2 := st.Peek("demo")
	if !ok1 || !ok2 {
		t.Fatal("expected item to exist")
	}
	if first.Doc != second.Doc {
		t.Fatal("Peek must not clone: two Peek calls returned different *Doc pointers")
	}

	// Get stays the defensive API: it must return an isolated clone.
	cloned, ok := st.Get("demo")
	if !ok {
		t.Fatal("expected item to exist")
	}
	if cloned.Doc == first.Doc {
		t.Fatal("Get must return a defensive clone, not the shared document")
	}
}

func TestPeekDocumentSurvivesApplyOps(t *testing.T) {
	// The safety contract behind Peek: a previously returned *Doc is never
	// mutated in place by later writes, so readers can hold the reference
	// across concurrent room updates.
	st := New()
	mustSave(t, st, "demo", "hello", time.Hour, "a")

	peeked, ok := st.Peek("demo")
	if !ok {
		t.Fatal("expected item to exist")
	}
	oldDoc := peeked.Doc
	oldText := oldDoc.Materialize()

	// A write must replace the stored document, not mutate the old one.
	// Clock must exceed the document max (5) for a mid-string insert (Lamport).
	_, _, err := st.ApplyOps("demo", []crdt.Op{{Op: crdt.OpInsert, ID: "b:6", After: "a:1", Ch: "X"}}, 0, "b", Auth{})
	if err != nil {
		t.Fatal(err)
	}
	if oldDoc.Materialize() != oldText {
		t.Fatal("ApplyOps mutated the document previously returned by Peek")
	}
	cur, ok := st.Get("demo")
	if !ok || cur.Content != "hXello" {
		t.Fatalf("stored document should have been replaced: %q", cur.Content)
	}
}

func TestPeekConcurrentWithApplyOps(t *testing.T) {
	// Run with -race: Peek readers sharing the stored document while writers
	// replace rooms must not race or observe torn state.
	st := New()
	mustSave(t, st, "demo", "hello", time.Hour, "a")

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				if item, ok := st.Peek("demo"); ok {
					_ = item.Doc.Materialize()
					_ = item.Content
					_ = item.Version
				}
			}
		}
	}()

	for i := 0; i < 500; i++ {
		if _, _, err := st.ApplyOps("demo", []crdt.Op{{Op: crdt.OpInsert, ID: "b:" + strconv.Itoa(i+1), After: "", Ch: "X"}}, 0, "b", Auth{}); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
}

// ClaimPassword on SaveWithBase locks an unlocked room under the same write
// as the content (CLI push -password create-and-lock).
func TestSaveWithBaseClaimLocksAtomically(t *testing.T) {
	st := New()
	item, err := st.SaveWithBase("cli", "secret-body", time.Hour, "cli", 0, Auth{
		Password:      "s3cret",
		ClaimPassword: "s3cret",
		ClaimScope:    "edit",
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.Content != "secret-body" {
		t.Fatalf("content = %q", item.Content)
	}
	if !st.HasPassword("cli") {
		t.Fatal("room should be claim-locked after SaveWithBase")
	}
	set, scope := st.PasswordInfo("cli")
	if !set || scope != "edit" {
		t.Fatalf("PasswordInfo = %v %q, want locked edit", set, scope)
	}
	// Unauthenticated write must fail — content never sat unlocked.
	if _, err := st.SaveWithBase("cli", "stolen", time.Hour, "x", 0, Auth{}); !errors.Is(err, ErrPasswordMismatch) {
		t.Fatalf("write without password after claim: %v", err)
	}
	// Correct password still works; claim on an already-locked room is a no-op
	// (must not rotate with ClaimPassword alone).
	credBefore := st.PasswordCredential("cli")
	if _, err := st.SaveWithBase("cli", "ok", time.Hour, "x", 0, Auth{
		Password:      "s3cret",
		ClaimPassword: "other",
		ClaimScope:    "view",
	}); err != nil {
		t.Fatal(err)
	}
	if got := st.PasswordCredential("cli"); got != credBefore {
		t.Fatal("ClaimPassword must not rotate an already-locked room")
	}
	set, scope = st.PasswordInfo("cli")
	if !set || scope != "edit" {
		t.Fatalf("scope should stay edit after ignored claim, got %v %q", set, scope)
	}
}

// SaveWithBase / ApplyOps / DeleteAuth must reject wrong passwords under the
// same lock as the mutation (handler-only checks are TOCTOU).
func TestMutationsRequirePasswordAuth(t *testing.T) {
	st := New()
	mustSave(t, st, "locked", "body", time.Hour, "a")
	if err := st.SetPassword("locked", "", "s3cret", "edit"); err != nil {
		t.Fatal(err)
	}

	if _, err := st.SaveWithBase("locked", "hacked", time.Hour, "x", 0, Auth{}); !errors.Is(err, ErrPasswordMismatch) {
		t.Fatalf("SaveWithBase without password: %v", err)
	}
	if _, err := st.SaveWithBase("locked", "hacked", time.Hour, "x", 0, Auth{Password: "wrong"}); !errors.Is(err, ErrPasswordMismatch) {
		t.Fatalf("SaveWithBase wrong password: %v", err)
	}
	item, err := st.SaveWithBase("locked", "ok", time.Hour, "x", 0, Auth{Password: "s3cret"})
	if err != nil || item.Content != "ok" {
		t.Fatalf("SaveWithBase correct password: item=%+v err=%v", item, err)
	}

	if _, _, err := st.ApplyOps("locked", []crdt.Op{{Op: crdt.OpInsert, ID: "z:1", Ch: "Z"}}, 0, "z", Auth{}); !errors.Is(err, ErrPasswordMismatch) {
		t.Fatalf("ApplyOps without password: %v", err)
	}
	if _, _, err := st.ApplyOps("locked", []crdt.Op{{Op: crdt.OpInsert, ID: "z:1", Ch: "Z"}}, 0, "z", Auth{Password: "s3cret"}); err != nil {
		t.Fatalf("ApplyOps with password: %v", err)
	}

	// Session credential path (WS re-auth).
	cred := st.PasswordCredential("locked")
	if _, _, err := st.ApplyOps("locked", nil, 2*time.Hour, "z", Auth{Cred: cred}); err != nil {
		t.Fatalf("ApplyOps with session cred: %v", err)
	}

	if err := st.DeleteAuth("locked", Auth{}); !errors.Is(err, ErrPasswordMismatch) {
		t.Fatalf("DeleteAuth without password: %v", err)
	}
	if err := st.DeleteAuth("locked", Auth{Password: "s3cret"}); err != nil {
		t.Fatalf("DeleteAuth with password: %v", err)
	}
}

func TestLegacySHA256PasswordStillVerifies(t *testing.T) {
	// Simulate a pre-bcrypt snapshot: PasswordHash = hex(SHA-256(salt:password)).
	st := New()
	mustSave(t, st, "legacy", "body", time.Hour, "a")
	st.mu.Lock()
	item := st.items["legacy"]
	salt := "0123456789abcdef0123456789abcdef"
	sum := sha256.Sum256([]byte(salt + ":" + "oldpass"))
	item.PasswordSalt = salt
	item.PasswordHash = hex.EncodeToString(sum[:])
	item.PasswordScope = "view"
	st.items["legacy"] = item
	st.mu.Unlock()

	if !st.PasswordOK("legacy", "oldpass") {
		t.Fatal("legacy SHA-256 hash must still verify")
	}
	if st.PasswordOK("legacy", "nope") {
		t.Fatal("legacy hash must reject wrong password")
	}
	// New password uses bcrypt.
	if err := st.SetPassword("legacy", "oldpass", "newpass", "view"); err != nil {
		t.Fatal(err)
	}
	item2, _ := st.Get("legacy")
	if !strings.HasPrefix(item2.PasswordHash, "$2") {
		t.Fatalf("rotated hash should be bcrypt, got %q", item2.PasswordHash)
	}
}

// Passwords are trimmed on both the set and verify sides, and AuthCredential
// validates + returns the credential in one atomic step so a concurrent
// rotation cannot interleave between check and credential read.
func TestPasswordTrimAndAuthCredential(t *testing.T) {
	st := New()
	mustSave(t, st, "pw", "body", time.Hour, "a")

	if err := st.SetPassword("pw", "", "  secret  ", "edit"); err != nil {
		t.Fatal(err)
	}
	if !st.PasswordOK("pw", "secret") {
		t.Fatal("PasswordOK must match the trimmed password")
	}
	if !st.PasswordOK("pw", "  secret  ") {
		t.Fatal("PasswordOK must trim the presented password")
	}
	if st.PasswordOK("pw", "   ") {
		t.Fatal("whitespace-only input must never match")
	}

	// Correct password: credential returned and equal to PasswordCredential.
	cred, ok := st.AuthCredential("pw", "  secret  ")
	if !ok || cred == "" {
		t.Fatalf("AuthCredential = %q, %v; want non-empty cred + ok", cred, ok)
	}
	if cred != st.PasswordCredential("pw") {
		t.Fatal("AuthCredential credential must match PasswordCredential")
	}
	if _, ok := st.AuthCredential("pw", "wrong"); ok {
		t.Fatal("AuthCredential must reject a wrong password")
	}

	// Rotation must change the credential (stale sessions expire).
	st2 := New()
	mustSave(t, st2, "rot", "body", time.Hour, "a")
	if err := st2.SetPassword("rot", "", "one", "view"); err != nil {
		t.Fatal(err)
	}
	cred1, ok1 := st2.AuthCredential("rot", "one")
	if !ok1 || cred1 == "" {
		t.Fatalf("first auth = %q, %v", cred1, ok1)
	}
	if err := st2.SetPassword("rot", "one", "two", "view"); err != nil {
		t.Fatal(err)
	}
	cred2, ok2 := st2.AuthCredential("rot", "two")
	if !ok2 || cred2 == "" || cred1 == cred2 {
		t.Fatalf("rotation must change credential: %q -> %q (ok2=%v)", cred1, cred2, ok2)
	}

	// Unprotected room: any password accepted, no credential to bind to.
	st3 := New()
	mustSave(t, st3, "open", "body", time.Hour, "a")
	cred3, ok3 := st3.AuthCredential("open", "whatever")
	if !ok3 || cred3 != "" {
		t.Fatalf("open room AuthCredential = %q, %v; want empty cred + ok", cred3, ok3)
	}
}
