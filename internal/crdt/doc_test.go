package crdt

import (
	"math/rand"
	"testing"
)

func TestBuildAndMaterialize(t *testing.T) {
	d, err := BuildFromString("alice", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Materialize(); got != "hello" {
		t.Fatalf("materialize = %q, want hello", got)
	}
	if d.Len() != 5 {
		t.Fatalf("len = %d, want 5", d.Len())
	}
}

func TestConcurrentInsertSameParent(t *testing.T) {
	// Both insert after "a:1" ('x').
	base := NewDoc()
	if _, err := base.Insert("a:1", "", "x"); err != nil {
		t.Fatal(err)
	}

	opB := Op{Op: OpInsert, ID: "b:1", After: "a:1", Ch: "B"}
	opC := Op{Op: OpInsert, ID: "c:1", After: "a:1", Ch: "C"}

	orders := [][]Op{
		{opB, opC},
		{opC, opB},
	}
	var results []string
	for _, ops := range orders {
		d := base.Clone()
		if _, err := d.ApplyBatch(ops); err != nil {
			t.Fatal(err)
		}
		results = append(results, d.Materialize())
	}
	if results[0] != results[1] {
		t.Fatalf("divergent: %q vs %q", results[0], results[1])
	}
	// Same clock: site ascending → b before c → "xBC"
	if results[0] != "xBC" {
		t.Fatalf("got %q, want xBC", results[0])
	}
}

func TestConcurrentInsertAtHead(t *testing.T) {
	opA := Op{Op: OpInsert, ID: "a:1", After: "", Ch: "A"}
	opB := Op{Op: OpInsert, ID: "b:1", After: "", Ch: "B"}

	d1 := NewDoc()
	d2 := NewDoc()
	_, _ = d1.ApplyBatch([]Op{opA, opB})
	_, _ = d2.ApplyBatch([]Op{opB, opA})
	if d1.Materialize() != d2.Materialize() {
		t.Fatalf("divergent head inserts: %q vs %q", d1.Materialize(), d2.Materialize())
	}
	if d1.Materialize() != "AB" {
		t.Fatalf("got %q, want AB", d1.Materialize())
	}
}

func TestInsertBetweenExistingChain(t *testing.T) {
	// Regression: mid-string insert must land next to the parent, not after the
	// whole right-hand subtree (which looked like characters jumping lines).
	d, err := BuildFromString("s", "hello")
	if err != nil {
		t.Fatal(err)
	}
	// Lamport clock must exceed max seen (5).
	if _, err := d.Insert("c:6", "s:1", "X"); err != nil {
		t.Fatal(err)
	}
	if got := d.Materialize(); got != "hXello" {
		t.Fatalf("got %q, want hXello", got)
	}
	// Insert on another line after newline.
	d2, err := BuildFromString("s", "ab\ncd")
	if err != nil {
		t.Fatal(err)
	}
	// ids: a=s:1 b=s:2 \n=s:3 c=s:4 d=s:5 — insert after newline
	if _, err := d2.Insert("u:10", "s:3", "Z"); err != nil {
		t.Fatal(err)
	}
	if got := d2.Materialize(); got != "ab\nZcd" {
		t.Fatalf("got %q, want ab\\nZcd", got)
	}
}

func TestDeleteThenInsertAfterTombstone(t *testing.T) {
	d := NewDoc()
	_, _ = d.Insert("a:1", "", "A")
	_, _ = d.Insert("a:2", "a:1", "B")
	d.Delete("a:1")
	// Newer clock than a:2 so X sits immediately after the tombstone (before B).
	if _, err := d.Insert("b:3", "a:1", "X"); err != nil {
		t.Fatal(err)
	}
	if got := d.Materialize(); got != "XB" {
		t.Fatalf("got %q, want XB", got)
	}
}

func TestIdempotentOps(t *testing.T) {
	d := NewDoc()
	op := Op{Op: OpInsert, ID: "a:1", After: "", Ch: "Z"}
	changed, err := d.Apply(op)
	if err != nil || !changed {
		t.Fatalf("first insert: changed=%v err=%v", changed, err)
	}
	changed, err = d.Apply(op)
	if err != nil || changed {
		t.Fatalf("second insert: changed=%v err=%v", changed, err)
	}
	if d.Len() != 1 || d.Materialize() != "Z" {
		t.Fatalf("idempotent insert failed: len=%d content=%q", d.Len(), d.Materialize())
	}
	if !d.Delete("a:1") {
		t.Fatal("first delete should change")
	}
	if d.Delete("a:1") {
		t.Fatal("second delete should be no-op")
	}
	if d.Materialize() != "" {
		t.Fatalf("idempotent delete failed: %q", d.Materialize())
	}
}

func TestInterleavedMultiSite(t *testing.T) {
	// Simulate two sites editing the same base.
	seed, err := BuildFromString("s", "hi")
	if err != nil {
		t.Fatal(err)
	}
	// s:1=h s:2=i — use Lamport clocks > max seen (2)
	opsA := []Op{
		{Op: OpInsert, ID: "a:3", After: "s:1", Ch: "A"}, // hAi
	}
	opsB := []Op{
		{Op: OpInsert, ID: "b:3", After: "s:2", Ch: "B"}, // hiB
	}

	d1 := seed.Clone()
	d2 := seed.Clone()
	_, _ = d1.ApplyBatch(append(append([]Op{}, opsA...), opsB...))
	_, _ = d2.ApplyBatch(append(append([]Op{}, opsB...), opsA...))
	if d1.Materialize() != d2.Materialize() {
		t.Fatalf("divergent: %q vs %q", d1.Materialize(), d2.Materialize())
	}
	if d1.Materialize() != "hAiB" {
		t.Fatalf("got %q, want hAiB", d1.Materialize())
	}
}

func TestFromItemsRoundTrip(t *testing.T) {
	d, err := BuildFromString("z", "ok👍")
	if err != nil {
		t.Fatal(err)
	}
	d.Delete(d.VisibleIDs()[0])
	items := d.Items()
	d2 := NewDoc()
	if err := d2.FromItems(items); err != nil {
		t.Fatal(err)
	}
	if d.Materialize() != d2.Materialize() {
		t.Fatalf("roundtrip content %q vs %q", d.Materialize(), d2.Materialize())
	}
	if d.Len() != d2.Len() {
		t.Fatalf("roundtrip len %d vs %d", d.Len(), d2.Len())
	}
}

func TestEmptyDocInsert(t *testing.T) {
	d := NewDoc()
	if _, err := d.Insert("a:1", "", "!"); err != nil {
		t.Fatal(err)
	}
	if d.Materialize() != "!" {
		t.Fatalf("got %q", d.Materialize())
	}
}

func TestMissingParentRejected(t *testing.T) {
	d := NewDoc()
	_, err := d.Insert("a:1", "missing:1", "x")
	if err == nil {
		t.Fatal("expected error for missing parent")
	}
}

func TestShuffledApplyConverges(t *testing.T) {
	// Fixed set of ops with independent inserts after known parents once base applied.
	baseOps := []Op{
		{Op: OpInsert, ID: "root:1", After: "", Ch: "R"},
	}
	extra := []Op{
		{Op: OpInsert, ID: "a:1", After: "root:1", Ch: "1"},
		{Op: OpInsert, ID: "b:1", After: "root:1", Ch: "2"},
		{Op: OpInsert, ID: "c:1", After: "root:1", Ch: "3"},
		{Op: OpDelete, ID: "a:1"},
		{Op: OpInsert, ID: "d:1", After: "b:1", Ch: "X"},
	}

	// All extra ops: deletes need target present — apply a:1 before del a:1.
	// Build valid total orders by shuffling only independent-safe sequences.
	// Use: apply all inserts first in random order, then delete.
	inserts := []Op{extra[0], extra[1], extra[2], extra[4]}
	del := extra[3]

	rng := rand.New(rand.NewSource(42))
	var want string
	for trial := 0; trial < 20; trial++ {
		perm := rng.Perm(len(inserts))
		ops := append([]Op{}, baseOps...)
		for _, i := range perm {
			ops = append(ops, inserts[i])
		}
		ops = append(ops, del)

		d := NewDoc()
		if _, err := d.ApplyBatch(ops); err != nil {
			// d:1 after b:1 requires b:1 first — use causal order fallback
			ordered := []Op{
				baseOps[0],
				{Op: OpInsert, ID: "a:1", After: "root:1", Ch: "1"},
				{Op: OpInsert, ID: "b:1", After: "root:1", Ch: "2"},
				{Op: OpInsert, ID: "c:1", After: "root:1", Ch: "3"},
				{Op: OpInsert, ID: "d:1", After: "b:1", Ch: "X"},
				del,
			}
			d = NewDoc()
			if _, err := d.ApplyBatch(ordered); err != nil {
				t.Fatal(err)
			}
			if want == "" {
				want = d.Materialize()
			}
			continue
		}
		got := d.Materialize()
		if want == "" {
			want = got
		} else if got != want {
			t.Fatalf("trial %d divergent: %q vs %q", trial, got, want)
		}
	}
	if want == "" {
		t.Fatal("no successful trial")
	}
}

func TestCompareID(t *testing.T) {
	if CompareID("a:1", "a:2") >= 0 {
		t.Fatal("a:1 should be < a:2")
	}
	if CompareID("a:2", "b:1") >= 0 {
		t.Fatal("a:2 should be < b:1")
	}
	if CompareID("a:1", "a:1") != 0 {
		t.Fatal("equal")
	}
}

func TestValidateBatch(t *testing.T) {
	if err := ValidateBatch(nil); err == nil {
		t.Fatal("empty should fail")
	}
	if err := ValidateBatch([]Op{{Op: OpInsert, ID: "a:1", Ch: "ab"}}); err == nil {
		t.Fatal("multi-codepoint ch should fail")
	}
}
