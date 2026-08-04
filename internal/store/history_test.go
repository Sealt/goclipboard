package store

import (
	"errors"
	"testing"
	"time"

	"goclipboard/internal/model"
)

func TestHistoryAutoCaptureAndManual(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	clock := now
	s := New(WithClock(func() time.Time { return clock }))

	if _, err := s.Save("histroom", "alpha", time.Hour, "alice"); err != nil {
		t.Fatalf("save1: %v", err)
	}
	hist, ok, err := s.History("histroom", Auth{})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if !ok || len(hist) != 1 || hist[0].Text != "alpha" {
		t.Fatalf("auto history after first save = %+v ok=%v", hist, ok)
	}

	// Within throttle: no new auto entry.
	clock = clock.Add(time.Second)
	if _, err := s.Save("histroom", "beta", time.Hour, "alice"); err != nil {
		t.Fatalf("save2: %v", err)
	}
	hist, _, err = s.History("histroom", Auth{})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(hist) != 1 {
		t.Fatalf("throttled auto history len=%d want 1", len(hist))
	}

	// Past throttle: new entry.
	clock = clock.Add(HistoryThrottle)
	if _, err := s.Save("histroom", "gamma", time.Hour, "bob"); err != nil {
		t.Fatalf("save3: %v", err)
	}
	hist, _, err = s.History("histroom", Auth{})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(hist) != 2 || hist[1].Text != "gamma" {
		t.Fatalf("post-throttle history = %+v", hist)
	}

	// Manual capture of current content when text already matches last: no-op length.
	hist2, err := s.CaptureHistory("histroom", Auth{})
	if err != nil {
		t.Fatalf("manual same: %v", err)
	}
	if len(hist2) != 2 {
		t.Fatalf("manual same-text should not grow: %d", len(hist2))
	}

	// Change then manual inside throttle still records.
	clock = clock.Add(time.Second)
	if _, err := s.Save("histroom", "delta", time.Hour, "bob"); err != nil {
		t.Fatalf("save4: %v", err)
	}
	// auto may be throttled so still 2; force manual.
	hist3, err := s.CaptureHistory("histroom", Auth{})
	if err != nil {
		t.Fatalf("manual: %v", err)
	}
	if len(hist3) < 3 || hist3[len(hist3)-1].Text != "delta" || !hist3[len(hist3)-1].Manual {
		t.Fatalf("manual capture = %+v", hist3)
	}
}

func TestHistoryPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	s1 := New(
		WithClock(func() time.Time { return now }),
		WithPersistence(dir),
	)
	if _, err := s1.Save("proom", "one", time.Hour, "a"); err != nil {
		t.Fatal(err)
	}
	// Force a second snapshot past throttle.
	s1.now = func() time.Time { return now.Add(HistoryThrottle + time.Second) }
	if _, err := s1.Save("proom", "two", time.Hour, "a"); err != nil {
		t.Fatal(err)
	}
	s1.Close()

	s2 := New(
		WithClock(func() time.Time { return now.Add(2 * time.Minute) }),
		WithPersistence(dir),
	)
	defer s2.Close()
	hist, ok, err := s2.History("proom", Auth{})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if !ok || len(hist) < 2 {
		t.Fatalf("restored history = %+v ok=%v", hist, ok)
	}
	if hist[len(hist)-1].Text != "two" {
		t.Fatalf("last restored entry = %q", hist[len(hist)-1].Text)
	}
}

func TestHistoryClearAndEmptyManual(t *testing.T) {
	s := New()
	if _, err := s.Save("croom", "keep-me", time.Hour, "a"); err != nil {
		t.Fatal(err)
	}
	hist, ok, err := s.History("croom", Auth{})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if !ok || len(hist) != 1 {
		t.Fatalf("history = %+v ok=%v", hist, ok)
	}
	_, total1 := s.Stats()

	// Empty content: manual capture must not grow the trail.
	if _, err := s.Save("croom", "", time.Hour, "a"); err != nil {
		t.Fatal(err)
	}
	hist2, err := s.CaptureHistory("croom", Auth{})
	if err != nil {
		t.Fatalf("manual empty: %v", err)
	}
	if len(hist2) != 1 || hist2[0].Text != "keep-me" {
		t.Fatalf("empty manual should not capture: %+v", hist2)
	}

	if err := s.ClearHistory("croom", Auth{}); err != nil {
	if err != nil {
		t.Fatalf("history: %v", err)
	}
		t.Fatalf("clear: %v", err)
	}
	hist3, ok, err := s.History("croom", Auth{})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if !ok || len(hist3) != 0 {
		t.Fatalf("after clear = %+v ok=%v", hist3, ok)
	}
	_, total2 := s.Stats()
	if total2 >= total1 {
		t.Fatalf("clear should free history budget: before=%d after=%d", total1, total2)
	}
	// Idempotent.
	if err := s.ClearHistory("croom", Auth{}); err != nil {
	if err != nil {
		t.Fatalf("history: %v", err)
	}
		t.Fatalf("clear empty: %v", err)
	}
	if err := s.ClearHistory("missing", Auth{}); !errors.Is(err, ErrRoomNotFound) {
	if err != nil {
		t.Fatalf("history: %v", err)
	}
		t.Fatalf("clear missing: %v", err)
	}
}

func TestHistoryCountsTowardMemoryLimit(t *testing.T) {
	// Room fits content + one history entry only.
	content := "payload"
	budget := estimateBytes(content, len([]rune(content))) + historyBytes([]model.HistoryEntry{{Text: content}})
	s := New(WithLimits(10, budget))
	if _, err := s.Save("h", content, time.Hour, "a"); err != nil {
		t.Fatalf("first save: %v", err)
	}
	// A second distinct history entry would exceed budget even without CRDT growth.
	// Use CaptureHistory after changing content via a larger budget path is hard;
	// instead apply a Save of different text that would need a second history slot.
	_, err := s.Save("h", content+"!", time.Hour, "a")
	// May fail on memory (history grow) or succeed if throttle skipped auto and
	// text change still fits estimate — when it auto-captures, budget trips.
	if err == nil {
		// Force a manual capture of a new value past throttle window.
		clock := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
		s2 := New(
			WithClock(func() time.Time { return clock }),
			WithLimits(10, budget),
		)
		if _, err := s2.Save("h2", content, time.Hour, "a"); err != nil {
			t.Fatalf("seed: %v", err)
		}
		// Advance and try another auto capture with new text.
		clock = clock.Add(HistoryThrottle + time.Second)
		if _, err := s2.Save("h2", "other!!", time.Hour, "a"); !errors.Is(err, ErrMemoryLimit) {
			// If estimate of new content alone exceeds, also fine; must not silently grow.
			if err == nil {
				hist, _, err := s2.History("h2", Auth{})
				if err != nil {
					t.Fatalf("history: %v", err)
				}
				if len(hist) > 1 {
					t.Fatalf("expected budget to block second history entry, hist=%+v", hist)
				}
			}
		}
	} else if !errors.Is(err, ErrMemoryLimit) {
		t.Fatalf("unexpected err: %v", err)
	}
}
