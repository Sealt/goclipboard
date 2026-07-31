package crdt

import (
	"fmt"
	"math/rand"
	"testing"
	"unicode/utf8"
)

// Property tests for the convergence guarantees the realtime protocol relies
// on: concurrent edits from many sites must converge to the same document
// under ANY causal delivery order, re-applies must be idempotent, and
// snapshots must survive a from-items rebuild.

var codePoints = []string{"a", "b", "中", "λ", "😀", " ", "\n"}

// convergeScenario is a random multi-site edit session. Ops are generated
// with a parent relation that is always a strictly earlier op (or the head),
// so any topological ordering is a valid causal delivery order.
type convergeScenario struct {
	ops     []Op
	inserts int
	deleted map[string]bool // unique delete targets
}

func genScenario(rng *rand.Rand, siteCount, maxOps int) *convergeScenario {
	sc := &convergeScenario{deleted: make(map[string]bool)}
	sites := make([]string, siteCount)
	clocks := make(map[string]int64)
	for i := range sites {
		sites[i] = fmt.Sprintf("s%d", i)
		clocks[sites[i]] = 0
	}
	// ids[0] is "" (document head); every generated insert id is appended.
	ids := []string{""}
	n := rng.Intn(maxOps)
	for i := 0; i < n; i++ {
		if rng.Float64() < 0.8 || len(ids) == 1 {
			site := sites[rng.Intn(len(sites))]
			clocks[site]++
			id := FormatID(site, clocks[site])
			parent := ids[rng.Intn(len(ids))]
			ch := codePoints[rng.Intn(len(codePoints))]
			sc.ops = append(sc.ops, Op{Op: OpInsert, ID: id, After: parent, Ch: ch})
			ids = append(ids, id)
			sc.inserts++
		} else {
			id := ids[1+rng.Intn(len(ids)-1)]
			sc.ops = append(sc.ops, Op{Op: OpDelete, ID: id})
			sc.deleted[id] = true
		}
	}
	return sc
}

// topoOrder returns a random linear extension of the ops that respects
// causality: an insert comes after its parent (after) edge, and a delete
// comes after the insert it targets (deletes of ids not yet delivered are
// dropped by design, so the causal order is required for correct replay).
// A few duplicate ops are appended at the end, mirroring how the ws replay
// protocol re-delivers unacked ops after a snapshot — idempotency check.
func topoOrder(rng *rand.Rand, sc *convergeScenario) []Op {
	index := make(map[string]int, len(sc.ops))
	for i, op := range sc.ops {
		if op.Op == OpInsert {
			index[op.ID] = i
		}
	}
	indeg := make([]int, len(sc.ops))
	children := make([][]int, len(sc.ops))
	for i, op := range sc.ops {
		switch {
		case op.Op == OpInsert && op.After != "":
			if p, ok := index[op.After]; ok {
				indeg[i]++
				children[p] = append(children[p], i)
			}
		case op.Op == OpDelete:
			if p, ok := index[op.ID]; ok {
				indeg[i]++
				children[p] = append(children[p], i)
			}
		}
	}
	var ready []int
	for i := range sc.ops {
		if indeg[i] == 0 {
			ready = append(ready, i)
		}
	}
	order := make([]Op, 0, len(sc.ops)+2)
	for len(ready) > 0 {
		k := rng.Intn(len(ready))
		i := ready[k]
		ready = append(ready[:k], ready[k+1:]...)
		order = append(order, sc.ops[i])
		for _, c := range children[i] {
			indeg[c]--
			if indeg[c] == 0 {
				ready = append(ready, c)
			}
		}
	}
	if len(order) != len(sc.ops) {
		panic("generator produced a cyclic parent relation")
	}
	// Replay semantics: duplicate ops re-delivered after the whole batch.
	base := len(order)
	if base > 0 {
		for d := 0; d < rng.Intn(3); d++ {
			order = append(order, order[rng.Intn(base)])
		}
	}
	return order
}

func sameItems(a, b []Item) bool {
	if len(a) != len(b) {
		return false
	}
	// Full structural equality: ID, parent (After), value and tombstone.
	// Materialized-text equality alone would let a broken parent pointer
	// slip through, and parent pointers are what future ops anchor on.
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestConvergenceRandomConcurrentEdits(t *testing.T) {
	for seed := int64(1); seed <= 50; seed++ {
		rng := rand.New(rand.NewSource(seed))
		sc := genScenario(rng, 4, 300)
		if len(sc.ops) == 0 {
			continue
		}

		var expected string
		var expectedItems []Item
		for site := 0; site < 5; site++ {
			d := NewDoc()
			for _, op := range topoOrder(rng, sc) {
				if _, err := d.Apply(op); err != nil {
					t.Fatalf("seed %d site %d: apply %+v: %v", seed, site, op, err)
				}
			}
			if site == 0 {
				expected = d.Materialize()
				expectedItems = d.Items()
				// Invariant: every insert survives as an item (tombstone or
				// live); materialized length is inserts minus unique deletes.
				if len(expectedItems) != sc.inserts {
					t.Fatalf("seed %d: item count = %d, want %d inserts", seed, len(expectedItems), sc.inserts)
				}
				// Count code points, not bytes: content includes multi-byte runes.
				if want := sc.inserts - len(sc.deleted); utf8.RuneCountInString(expected) != want {
					t.Fatalf("seed %d: materialized code points = %d, want %d", seed, utf8.RuneCountInString(expected), want)
				}
			} else {
				if got := d.Materialize(); got != expected {
					t.Fatalf("seed %d site %d diverged:\n got %q\nwant %q", seed, site, got, expected)
				}
				if !sameItems(d.Items(), expectedItems) {
					t.Fatalf("seed %d site %d: item sequence diverged", seed, site)
				}
			}
		}
	}
}

func TestFromItemsRoundtripRandom(t *testing.T) {
	for seed := int64(1); seed <= 20; seed++ {
		rng := rand.New(rand.NewSource(seed))
		sc := genScenario(rng, 3, 200)
		d := NewDoc()
		for _, op := range sc.ops {
			if _, err := d.Apply(op); err != nil {
				t.Fatalf("seed %d: apply %+v: %v", seed, op, err)
			}
		}
		want := d.Materialize()
		// Snapshot the original DFS item sequence before shuffling: the
		// roundtrip must reproduce the full tree structure (ids, parents,
		// values, tombstones), not just the materialized text — a broken
		// parent pointer could otherwise slip through.
		orig := append([]Item(nil), d.Items()...)
		items := append([]Item(nil), orig...)
		rng.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
		rebuilt := NewDoc()
		if err := rebuilt.FromItems(items); err != nil {
			t.Fatalf("seed %d: FromItems: %v", seed, err)
		}
		if got := rebuilt.Materialize(); got != want {
			t.Fatalf("seed %d: roundtrip diverged: %q vs %q", seed, got, want)
		}
		if !sameItems(rebuilt.Items(), orig) {
			t.Fatalf("seed %d: roundtrip item structure diverged", seed)
		}
	}
}

func TestIdempotentReapply(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	sc := genScenario(rng, 3, 150)
	d := NewDoc()
	order := topoOrder(rng, sc)
	for _, op := range order {
		if _, err := d.Apply(op); err != nil {
			t.Fatal(err)
		}
	}
	want := d.Materialize()
	// Full replay (same ops again) must not change anything.
	changed := false
	for _, op := range order {
		c, err := d.Apply(op)
		if err != nil {
			t.Fatal(err)
		}
		changed = changed || c
	}
	if changed {
		t.Fatal("re-applying the same ops changed the document")
	}
	if got := d.Materialize(); got != want {
		t.Fatalf("replay changed content: %q vs %q", got, want)
	}
}

func TestDeleteNoOps(t *testing.T) {
	// Deletes of unknown ids are dropped by design (no error, no change):
	// the realtime protocol guarantees causal delivery, so a delete always
	// follows the insert it targets.
	d := NewDoc()
	if changed, err := d.Apply(Op{Op: OpDelete, ID: "x:1"}); err != nil || changed {
		t.Fatalf("delete unknown = %v, %v; want no-op", changed, err)
	}
	if _, err := d.Insert("x:1", "", "X"); err != nil {
		t.Fatal(err)
	}
	if changed, _ := d.Apply(Op{Op: OpDelete, ID: "x:1"}); !changed {
		t.Fatal("delete of live item should change the document")
	}
	// Second delete of the same id is a no-op.
	if changed, err := d.Apply(Op{Op: OpDelete, ID: "x:1"}); err != nil || changed {
		t.Fatalf("re-delete = %v, %v; want no-op", changed, err)
	}
	// Deleting before an insert leaves the item live: no-op, then insert works.
	d2 := NewDoc()
	if _, err := d2.Apply(Op{Op: OpDelete, ID: "y:1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d2.Insert("y:1", "", "Y"); err != nil {
		t.Fatal(err)
	}
	if got := d2.Materialize(); got != "Y" {
		t.Fatalf("materialize = %q, want Y (delete must not block later insert)", got)
	}
}
