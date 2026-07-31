package crdt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// Shared convergence fixture.
//
// The fixture is a single source of truth for cross-implementation checks:
// internal/crdt (Go) generates it, and static/crdt.test.js consumes it, so
// both implementations are verified against the SAME op lists and expected
// outputs — catching semantic drift between the two CRDT implementations
// (independent per-language property tests cannot do that).
//
// Regenerate after changing the generator or the CRDT semantics with:
//
//	REGEN_FIXTURE=1 go test ./internal/crdt/ -run TestConvergenceFixtureUpToDate

const fixturePath = "testdata/convergence.json"

type fixtureScenario struct {
	Name     string `json:"name"`
	Ops      []Op   `json:"ops"`
	Expected string `json:"expected"`
}

func applyForExpected(ops []Op) (string, error) {
	d := NewDoc()
	for _, op := range ops {
		if _, err := d.Apply(op); err != nil {
			return "", err
		}
	}
	return d.Materialize(), nil
}

func buildFixture(t *testing.T) []fixtureScenario {
	t.Helper()
	var out []fixtureScenario

	// Curated scenarios: the deterministic tricky cases.
	curated := []struct {
		name string
		ops  []Op
	}{
		{"empty", nil},
		{"head-inserts-same-clock", []Op{
			{Op: OpInsert, ID: "a:1", Ch: "A"},
			{Op: OpInsert, ID: "b:1", Ch: "B"},
		}},
		{"same-parent-equal-clock", []Op{
			{Op: OpInsert, ID: "a:1", Ch: "x"},
			{Op: OpInsert, ID: "b:1", After: "a:1", Ch: "B"},
			{Op: OpInsert, ID: "c:1", After: "a:1", Ch: "C"},
		}},
		{"mid-string-insert", []Op{
			{Op: OpInsert, ID: "s:1", Ch: "h"},
			{Op: OpInsert, ID: "s:2", After: "s:1", Ch: "e"},
			{Op: OpInsert, ID: "s:3", After: "s:2", Ch: "l"},
			{Op: OpInsert, ID: "s:4", After: "s:3", Ch: "l"},
			{Op: OpInsert, ID: "s:5", After: "s:4", Ch: "o"},
			{Op: OpInsert, ID: "c:6", After: "s:1", Ch: "X"},
		}},
		{"delete-middle", []Op{
			{Op: OpInsert, ID: "s:1", Ch: "h"},
			{Op: OpInsert, ID: "s:2", After: "s:1", Ch: "e"},
			{Op: OpInsert, ID: "s:3", After: "s:2", Ch: "l"},
			{Op: OpInsert, ID: "s:4", After: "s:3", Ch: "l"},
			{Op: OpInsert, ID: "s:5", After: "s:4", Ch: "o"},
			{Op: OpDelete, ID: "s:3"},
		}},
		{"nested-concurrent", []Op{
			{Op: OpInsert, ID: "a:1", Ch: "a"},
			{Op: OpInsert, ID: "a:2", After: "a:1", Ch: "b"},
			{Op: OpInsert, ID: "b:1", After: "a:1", Ch: "X"},
			{Op: OpInsert, ID: "b:2", After: "b:1", Ch: "Y"},
			{Op: OpInsert, ID: "c:1", After: "a:2", Ch: "Z"},
			{Op: OpDelete, ID: "b:1"},
		}},
		{"multibyte", []Op{
			{Op: OpInsert, ID: "a:1", Ch: "中"},
			{Op: OpInsert, ID: "a:2", After: "a:1", Ch: "😀"},
			{Op: OpInsert, ID: "b:1", After: "a:1", Ch: "λ"},
			{Op: OpInsert, ID: "c:1", After: "a:2", Ch: "文"},
			{Op: OpDelete, ID: "b:1"},
		}},
		{"insert-under-tombstone", []Op{
			{Op: OpInsert, ID: "a:1", Ch: "1"},
			{Op: OpInsert, ID: "a:2", After: "a:1", Ch: "2"},
			{Op: OpDelete, ID: "a:1"},
			{Op: OpInsert, ID: "b:1", After: "a:1", Ch: "!"}, // parent is a tombstone
			{Op: OpInsert, ID: "a:3", After: "a:2", Ch: "3"},
		}},
	}
	for _, c := range curated {
		expected, err := applyForExpected(c.ops)
		if err != nil {
			t.Fatalf("curated scenario %q is invalid: %v", c.name, err)
		}
		out = append(out, fixtureScenario{Name: c.name, Ops: c.ops, Expected: expected})
	}

	// Random scenarios from fixed seeds (deterministic across runs/Go versions).
	for i := 0; i < 10; i++ {
		rng := rand.New(rand.NewSource(1000 + int64(i)))
		sc := genScenario(rng, 4, 60)
		if len(sc.ops) == 0 {
			continue
		}
		expected, err := applyForExpected(sc.ops)
		if err != nil {
			t.Fatalf("random scenario %d is invalid: %v", i, err)
		}
		out = append(out, fixtureScenario{
			Name:     fmt.Sprintf("random-%d", i),
			Ops:      sc.ops,
			Expected: expected,
		})
	}
	return out
}

func TestConvergenceFixtureUpToDate(t *testing.T) {
	data, err := json.MarshalIndent(buildFixture(t), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')

	existing, err := os.ReadFile(fixturePath)
	if err != nil || !bytes.Equal(existing, data) {
		if os.Getenv("REGEN_FIXTURE") == "1" {
			if err := os.MkdirAll(filepath.Dir(fixturePath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(fixturePath, data, 0o644); err != nil {
				t.Fatal(err)
			}
			t.Logf("regenerated %s", fixturePath)
			return
		}
		t.Fatalf("%s is stale — regenerate with:\n\tREGEN_FIXTURE=1 go test ./internal/crdt/ -run TestConvergenceFixtureUpToDate", fixturePath)
	}
}

func TestConvergenceFixture(t *testing.T) {
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var scenarios []fixtureScenario
	if err := json.Unmarshal(raw, &scenarios); err != nil {
		t.Fatal(err)
	}
	if len(scenarios) == 0 {
		t.Fatal("fixture is empty")
	}

	rng := rand.New(rand.NewSource(42))
	for _, sc := range scenarios {
		// Generation order (this is what the fixture expected was computed from).
		got, err := applyForExpected(sc.Ops)
		if err != nil || got != sc.Expected {
			t.Fatalf("%s: generation order = %q, %v; want %q", sc.Name, got, err, sc.Expected)
		}
		// A few random causal delivery orders must converge to the same text.
		s := &convergeScenario{ops: sc.Ops}
		for i := 0; i < 3; i++ {
			d := NewDoc()
			for _, op := range topoOrder(rng, s) {
				if _, err := d.Apply(op); err != nil {
					t.Fatalf("%s: causal order %d: apply %+v: %v", sc.Name, i, op, err)
				}
			}
			if got := d.Materialize(); got != sc.Expected {
				t.Fatalf("%s: causal order %d diverged: %q vs %q", sc.Name, i, got, sc.Expected)
			}
		}
	}
}
