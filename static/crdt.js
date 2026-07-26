// Character-level RGA sequence CRDT (mirrors internal/crdt in Go).
// Tree: each item's `after` is its parent; children sorted by ID ascending;
// document order is DFS pre-order from root parent "".
(function (global) {
  "use strict";

  function parseID(id) {
    if (!id || typeof id !== "string") return null;
    var i = id.indexOf(":");
    if (i <= 0 || i === id.length - 1) return null;
    var site = id.slice(0, i);
    var clock = Number(id.slice(i + 1));
    if (!site || !Number.isFinite(clock) || clock <= 0 || clock !== Math.floor(clock)) {
      return null;
    }
    return { site: site, clock: clock };
  }

  function formatID(site, clock) {
    return site + ":" + clock;
  }

  function compareID(a, b) {
    if (a === b) return 0;
    var pa = parseID(a);
    var pb = parseID(b);
    if (!pa || !pb) {
      if (a < b) return -1;
      if (a > b) return 1;
      return 0;
    }
    if (pa.site < pb.site) return -1;
    if (pa.site > pb.site) return 1;
    if (pa.clock < pb.clock) return -1;
    if (pa.clock > pb.clock) return 1;
    return 0;
  }

  // Sibling order: higher Lamport clock first, then site ascending.
  // Matches Go crdt.CompareSibling — clocks must be Lamport ( > any seen ).
  function compareSibling(a, b) {
    if (a === b) return 0;
    var pa = parseID(a);
    var pb = parseID(b);
    if (!pa || !pb) return compareID(a, b);
    if (pa.clock > pb.clock) return -1;
    if (pa.clock < pb.clock) return 1;
    if (pa.site < pb.site) return -1;
    if (pa.site > pb.site) return 1;
    return 0;
  }

  function maxClockFromIds(ids) {
    var max = 0;
    for (var i = 0; i < (ids || []).length; i++) {
      var p = parseID(ids[i]);
      if (p && p.clock > max) max = p.clock;
    }
    return max;
  }

  // One Unicode code point (not UTF-16 code unit).
  function singleCodePoint(ch) {
    if (typeof ch !== "string" || ch.length === 0) return false;
    var cp = ch.codePointAt(0);
    var len = cp > 0xffff ? 2 : 1;
    return ch.length === len;
  }

  function codePoints(str) {
    return Array.from(String(str || ""));
  }

  function Doc() {
    this.items = Object.create(null); // id -> {id, after, ch, del}
    this.children = Object.create(null); // parent -> [child ids] sorted
  }

  Doc.prototype.len = function () {
    return Object.keys(this.items).length;
  };

  Doc.prototype.insertChild = function (parent, id) {
    var kids = this.children[parent];
    if (!kids) {
      kids = [];
      this.children[parent] = kids;
    }
    var i = 0;
    while (i < kids.length && compareSibling(kids[i], id) < 0) i++;
    if (i < kids.length && kids[i] === id) return;
    kids.splice(i, 0, id);
  };

  Doc.prototype.maxClock = function () {
    return maxClockFromIds(Object.keys(this.items));
  };

  Doc.prototype.insert = function (id, after, ch) {
    if (!parseID(id)) return { ok: false, error: "invalid id" };
    if (!singleCodePoint(ch)) return { ok: false, error: "ch must be one code point" };
    if (this.items[id]) return { ok: true, changed: false };
    if (after) {
      if (!this.items[after]) return { ok: false, error: "unknown parent" };
    } else {
      after = "";
    }
    this.items[id] = { id: id, after: after, ch: ch, del: false };
    this.insertChild(after, id);
    return { ok: true, changed: true };
  };

  Doc.prototype.delete = function (id) {
    var item = this.items[id];
    if (!item || item.del) return { ok: true, changed: false };
    item.del = true;
    return { ok: true, changed: true };
  };

  Doc.prototype.apply = function (op) {
    if (!op || !op.op) return { ok: false, error: "bad op" };
    if (op.op === "ins") {
      return this.insert(op.id, op.after || "", op.ch);
    }
    if (op.op === "del") {
      if (!parseID(op.id)) return { ok: false, error: "invalid id" };
      return this.delete(op.id);
    }
    return { ok: false, error: "unknown op" };
  };

  Doc.prototype.applyBatch = function (ops) {
    if (!ops || !ops.length) return { ok: false, error: "empty ops", changed: false };
    var changed = false;
    for (var i = 0; i < ops.length; i++) {
      var r = this.apply(ops[i]);
      if (!r.ok) return { ok: false, error: r.error, changed: changed, index: i };
      if (r.changed) changed = true;
    }
    return { ok: true, changed: changed };
  };

  Doc.prototype.walk = function (parent, fn) {
    var kids = this.children[parent] || [];
    for (var i = 0; i < kids.length; i++) {
      var id = kids[i];
      var item = this.items[id];
      if (!item) continue;
      fn(item);
      this.walk(id, fn);
    }
  };

  Doc.prototype.materialize = function () {
    var parts = [];
    this.walk("", function (item) {
      if (!item.del) parts.push(item.ch);
    });
    return parts.join("");
  };

  Doc.prototype.visibleIds = function () {
    var ids = [];
    this.walk("", function (item) {
      if (!item.del) ids.push(item.id);
    });
    return ids;
  };

  Doc.prototype.toItems = function () {
    var out = [];
    this.walk("", function (item) {
      out.push({
        id: item.id,
        after: item.after,
        ch: item.ch,
        del: !!item.del
      });
    });
    return out;
  };

  Doc.prototype.fromItems = function (items) {
    this.items = Object.create(null);
    this.children = Object.create(null);
    items = items || [];
    var i;
    for (i = 0; i < items.length; i++) {
      var it = items[i];
      if (!it || !parseID(it.id) || !singleCodePoint(it.ch || it.Value)) {
        return { ok: false, error: "bad item" };
      }
      var ch = it.ch != null ? it.ch : it.Value;
      var after = it.after != null ? it.after : "";
      if (this.items[it.id]) return { ok: false, error: "duplicate" };
      this.items[it.id] = {
        id: it.id,
        after: after,
        ch: ch,
        del: !!(it.del || it.Deleted)
      };
    }
    for (i = 0; i < items.length; i++) {
      var it2 = items[i];
      var after2 = it2.after != null ? it2.after : "";
      if (after2 && !this.items[after2]) {
        return { ok: false, error: "unknown parent" };
      }
      this.insertChild(after2, it2.id);
    }
    return { ok: true };
  };

  Doc.prototype.clone = function () {
    var d = new Doc();
    d.fromItems(this.toItems());
    return d;
  };

  function buildFromString(site, content) {
    var d = new Doc();
    var cps = codePoints(content);
    var prev = "";
    for (var i = 0; i < cps.length; i++) {
      var id = formatID(site, i + 1);
      var r = d.insert(id, prev, cps[i]);
      if (!r.ok) return null;
      prev = id;
    }
    return d;
  }

  /**
   * Diff old code-point array + visible ids against new code-point array.
   * Returns { ops, nextIds } using site/clock allocator.
   */
  function diffToOps(oldChars, oldIds, newChars, site, clockStart) {
    oldChars = oldChars || [];
    oldIds = oldIds || [];
    newChars = newChars || [];
    var clock = clockStart || 0;
    var ops = [];

    // Two-end greedy match (good enough for typing / local paste).
    var lo = 0;
    var roOld = oldChars.length;
    var roNew = newChars.length;
    while (lo < roOld && lo < roNew && oldChars[lo] === newChars[lo]) {
      lo++;
    }
    while (
      roOld > lo &&
      roNew > lo &&
      oldChars[roOld - 1] === newChars[roNew - 1]
    ) {
      roOld--;
      roNew--;
    }

    // Delete middle of old.
    for (var d = lo; d < roOld; d++) {
      ops.push({ op: "del", id: oldIds[d] });
    }

    // Insert middle of new after last kept prefix id (or "").
    var after = lo > 0 ? oldIds[lo - 1] : "";
    for (var n = lo; n < roNew; n++) {
      clock++;
      var id = formatID(site, clock);
      ops.push({ op: "ins", id: id, after: after, ch: newChars[n] });
      after = id;
    }

    // Rebuild next visible ids for the new string.
    var nextIds = [];
    for (var i = 0; i < lo; i++) nextIds.push(oldIds[i]);
    // inserted ids are the last (roNew-lo) insert ops
    var insertOps = ops.filter(function (o) {
      return o.op === "ins";
    });
    for (var j = 0; j < insertOps.length; j++) nextIds.push(insertOps[j].id);
    for (var k = roOld; k < oldChars.length; k++) nextIds.push(oldIds[k]);

    return { ops: ops, nextIds: nextIds, clock: clock };
  }

  // UTF-16 index <-> code-point index helpers for textarea.
  function utf16ToCodePointOffset(str, utf16Index) {
    var s = String(str || "");
    var i = 0;
    var cp = 0;
    var target = Math.max(0, Math.min(utf16Index, s.length));
    while (i < target) {
      var code = s.codePointAt(i);
      i += code > 0xffff ? 2 : 1;
      cp++;
    }
    return cp;
  }

  function codePointToUtf16Offset(str, cpIndex) {
    var s = String(str || "");
    var i = 0;
    var cp = 0;
    var target = Math.max(0, cpIndex);
    while (i < s.length && cp < target) {
      var code = s.codePointAt(i);
      i += code > 0xffff ? 2 : 1;
      cp++;
    }
    return i;
  }

  global.CRDT = {
    Doc: Doc,
    parseID: parseID,
    formatID: formatID,
    compareID: compareID,
    compareSibling: compareSibling,
    maxClockFromIds: maxClockFromIds,
    codePoints: codePoints,
    buildFromString: buildFromString,
    diffToOps: diffToOps,
    utf16ToCodePointOffset: utf16ToCodePointOffset,
    codePointToUtf16Offset: codePointToUtf16Offset
  };
})(typeof window !== "undefined" ? window : globalThis);
