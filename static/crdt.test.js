#!/usr/bin/env node
"use strict";

// Cross-check for static/crdt.js: the same convergence properties the Go
// implementation is tested against (internal/crdt/converge_test.go) must
// hold for the browser implementation. Run with: node static/crdt.test.js
//
// No dependencies; exits non-zero on failure.

const assert = require("assert");
const fs = require("fs");
const path = require("path");
require("./crdt.js");
const CRDT = globalThis.CRDT;
const Doc = CRDT.Doc;

const CODE_POINTS = ["a", "b", "中", "λ", "😀", " ", "\n"];

// Deterministic PRNG (mulberry32) so failures are reproducible.
function mulberry32(seed) {
  let a = seed >>> 0;
  return function () {
    a |= 0;
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

function genScenario(rng, siteCount, maxOps) {
  const sites = [];
  const clocks = {};
  for (let i = 0; i < siteCount; i++) {
    const s = "s" + i;
    sites.push(s);
    clocks[s] = 0;
  }
  const ops = [];
  const ids = [""];
  let inserts = 0;
  const deleted = new Set();
  const n = Math.floor(rng() * maxOps);
  for (let i = 0; i < n; i++) {
    if (rng() < 0.8 || ids.length === 1) {
      const site = sites[Math.floor(rng() * sites.length)];
      clocks[site]++;
      const id = CRDT.formatID(site, clocks[site]);
      const parent = ids[Math.floor(rng() * ids.length)];
      const ch = CODE_POINTS[Math.floor(rng() * CODE_POINTS.length)];
      ops.push({ op: "ins", id: id, after: parent, ch: ch });
      ids.push(id);
      inserts++;
    } else {
      const id = ids[1 + Math.floor(rng() * (ids.length - 1))];
      ops.push({ op: "del", id: id });
      deleted.add(id);
    }
  }
  return { ops, inserts, deleted };
}

function topoOrder(rng, sc) {
  const index = new Map();
  sc.ops.forEach((op, i) => {
    if (op.op === "ins") index.set(op.id, i);
  });
  const indeg = new Array(sc.ops.length).fill(0);
  const children = sc.ops.map(() => []);
  sc.ops.forEach((op, i) => {
    if (op.op === "ins" && op.after !== "") {
      const p = index.get(op.after);
      if (p !== undefined) {
        indeg[i]++;
        children[p].push(i);
      }
    } else if (op.op === "del") {
      // Deletes must follow the insert they target (causal delivery);
      // deletes of not-yet-delivered ids are dropped by design.
      const p = index.get(op.id);
      if (p !== undefined) {
        indeg[i]++;
        children[p].push(i);
      }
    }
  });
  const ready = [];
  for (let i = 0; i < sc.ops.length; i++) {
    if (indeg[i] === 0) ready.push(i);
  }
  const order = [];
  while (ready.length) {
    const k = Math.floor(rng() * ready.length);
    const i = ready.splice(k, 1)[0];
    order.push(sc.ops[i]);
    for (const c of children[i]) {
      if (--indeg[c] === 0) ready.push(c);
    }
  }
  if (order.length !== sc.ops.length) throw new Error("cyclic parent relation");
  // Replay semantics: duplicates re-delivered after the whole batch.
  const base = order.length;
  if (base > 0) {
    const dups = Math.floor(rng() * 3);
    for (let d = 0; d < dups; d++) {
      order.push(order[Math.floor(rng() * base)]);
    }
  }
  return order;
}

function applyAll(doc, ops) {
  for (const op of ops) {
    const r = doc.apply(op);
    if (!r.ok) throw new Error("apply failed: " + r.error);
  }
}

function itemsOf(doc) {
  // Full structural signature: id, parent (after), tombstone, value.
  return doc.toItems().map((it) => it.id + "|" + it.after + "|" + it.del + "|" + it.ch);
}

let failures = 0;
function check(name, fn) {
  try {
    fn();
    console.log("ok   " + name);
  } catch (e) {
    failures++;
    console.error("FAIL " + name + ": " + e.message);
  }
}

check("convergence under random causal delivery", () => {
  for (let seed = 1; seed <= 30; seed++) {
    const rng = mulberry32(seed * 7919);
    const sc = genScenario(rng, 4, 300);
    if (sc.ops.length === 0) continue;
    let expected = null;
    let expectedItems = null;
    for (let site = 0; site < 5; site++) {
      const d = new Doc();
      applyAll(d, topoOrder(rng, sc));
      const text = d.materialize();
      if (site === 0) {
        expected = text;
        expectedItems = itemsOf(d);
        assert.strictEqual(d.len(), sc.inserts, "seed " + seed + ": item count");
        assert.strictEqual(
          Array.from(text).length,
          sc.inserts - sc.deleted.size,
          "seed " + seed + ": materialized length"
        );
      } else {
        assert.strictEqual(text, expected, "seed " + seed + " site " + site + " diverged");
        assert.deepStrictEqual(itemsOf(d), expectedItems, "seed " + seed + " site " + site + " item order");
      }
    }
  }
});

check("fromItems roundtrip (shuffled)", () => {
  for (let seed = 1; seed <= 15; seed++) {
    const rng = mulberry32(seed * 104729);
    const sc = genScenario(rng, 3, 200);
    const d = new Doc();
    applyAll(d, sc.ops);
    const want = d.materialize();
    const items = d.toItems();
    for (let i = items.length - 1; i > 0; i--) {
      const j = Math.floor(rng() * (i + 1));
      [items[i], items[j]] = [items[j], items[i]];
    }
    const rebuilt = new Doc();
    const r = rebuilt.fromItems(items);
    assert.ok(r.ok, "seed " + seed + ": fromItems " + (r.error || ""));
    assert.strictEqual(rebuilt.materialize(), want, "seed " + seed + " roundtrip diverged");
  }
});

check("idempotent re-apply", () => {
  const rng = mulberry32(12345);
  const sc = genScenario(rng, 3, 150);
  const d = new Doc();
  const order = topoOrder(rng, sc);
  applyAll(d, order);
  const want = d.materialize();
  let changed = false;
  for (const op of order) {
    const r = d.apply(op);
    assert.ok(r.ok, "re-apply failed: " + r.error);
    if (r.changed) changed = true;
  }
  assert.strictEqual(changed, false, "re-apply mutated the document");
  assert.strictEqual(d.materialize(), want, "re-apply changed content");
});

check("delete no-ops match Go semantics", () => {
  const d = new Doc();
  // Delete of unknown id: no-op, no error.
  let r = d.apply({ op: "del", id: "x:1" });
  assert.ok(r.ok && !r.changed, "delete unknown should be a no-op");
  // Delete of live item changes the doc; re-delete is a no-op.
  r = d.insert("x:1", "", "X");
  assert.ok(r.ok && r.changed, "insert should change");
  r = d.apply({ op: "del", id: "x:1" });
  assert.ok(r.ok && r.changed, "delete live should change");
  r = d.apply({ op: "del", id: "x:1" });
  assert.ok(r.ok && !r.changed, "re-delete should be a no-op");
  // Delete before insert is dropped, insert still lands live.
  const d2 = new Doc();
  d2.apply({ op: "del", id: "y:1" });
  d2.insert("y:1", "", "Y");
  assert.strictEqual(d2.materialize(), "Y", "delete must not block later insert");
});

check("shared fixture matches Go expectations", () => {
  // Cross-implementation agreement: the SAME op lists and expected outputs
  // that internal/crdt/fixture_test.go checks (generated by the Go
  // implementation) must reproduce here. Independent per-language property
  // tests can't catch semantic drift between the two CRDT implementations.
  const p = path.join(__dirname, "..", "internal", "crdt", "testdata", "convergence.json");
  const scenarios = JSON.parse(fs.readFileSync(p, "utf8"));
  assert.ok(Array.isArray(scenarios) && scenarios.length > 0, "fixture missing or empty");
  scenarios.forEach((sc) => { sc.ops = sc.ops || []; }); // Go nil slice serializes as null

  scenarios.forEach((sc, idx) => {
    const d = new Doc();
    applyAll(d, sc.ops);
    assert.strictEqual(d.materialize(), sc.expected, "fixture '" + sc.name + "' diverged from Go");

    // One shuffled causal delivery must converge to the same text.
    if (sc.ops.length > 0) {
      const rng = mulberry32((idx + 1) * 99991);
      const d2 = new Doc();
      applyAll(d2, topoOrder(rng, { ops: sc.ops }));
      assert.strictEqual(d2.materialize(), sc.expected, "fixture '" + sc.name + "' diverged under causal shuffle");
    }
  });
});

check("multibyte code-point offsets", () => {
  const s = "a😀中b";
  // "a😀中b" = code points a, 😀, 中, b (UTF-16: a, surrogates, 中, b)
  assert.strictEqual(CRDT.utf16ToCodePointOffset(s, 0), 0);
  assert.strictEqual(CRDT.utf16ToCodePointOffset(s, 1), 1);
  assert.strictEqual(CRDT.utf16ToCodePointOffset(s, 3), 2);
  assert.strictEqual(CRDT.utf16ToCodePointOffset(s, 4), 3);
  assert.strictEqual(CRDT.codePointToUtf16Offset(s, 1), 1);
  assert.strictEqual(CRDT.codePointToUtf16Offset(s, 2), 3);
  assert.strictEqual(CRDT.codePointToUtf16Offset(s, 4), s.length);
});

if (failures > 0) {
  console.error(failures + " test(s) failed");
  process.exit(1);
}
console.log("all crdt.js tests passed");
