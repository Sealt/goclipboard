(function () {
  var key = decodeURIComponent(window.location.pathname.slice(1));
  var apiURL = "/api/clipboard/" + encodeURIComponent(key);
  var filesAPIURL = apiURL + "/files";
  var settingsAPIURL = apiURL + "/settings";
  var wsURL =
    (location.protocol === "https:" ? "wss://" : "ws://") +
    location.host +
    "/api/clipboard/" +
    encodeURIComponent(key) +
    "/ws";
  var ADMIN_PASSWORD_KEY = "goclipboard:adminPassword";
  var FILE_PASSWORD_KEY = "goclipboard:filePassword";

  // Distinct palette; prefer unused colors among live peers to avoid collisions.
  var CURSOR_COLORS = [
    "#e06c75", "#61afef", "#98c379", "#d19a66", "#c678dd", "#56b6c2",
    "#e5c07b", "#be5046", "#528bff", "#e2b86b", "#7aa2f7", "#9ece6a",
    "#f7768e", "#bb9af7", "#2ac3de", "#ff9e64", "#73daca", "#ff7a93"
  ];

  function generateClientId() {
    var chars = "0123456789abcdef";
    var id = "";
    for (var i = 0; i < 16; i++) {
      id += chars[Math.floor(Math.random() * 16)];
    }
    return id;
  }

  function hashId(id) {
    var hash = 0;
    var s = String(id || "");
    for (var i = 0; i < s.length; i++) {
      hash = ((hash << 5) - hash) + s.charCodeAt(i);
      hash |= 0;
    }
    return hash;
  }

  function normalizeColor(color) {
    return String(color || "").trim().toLowerCase();
  }

  function pickColor(id, usedColors) {
    var hash = hashId(id);
    var start = Math.abs(hash) % CURSOR_COLORS.length;
    var used = {};
    if (usedColors && usedColors.length) {
      for (var u = 0; u < usedColors.length; u++) {
        used[normalizeColor(usedColors[u])] = true;
      }
    }
    for (var i = 0; i < CURSOR_COLORS.length; i++) {
      var candidate = CURSOR_COLORS[(start + i) % CURSOR_COLORS.length];
      if (!used[candidate]) return candidate;
    }
    return hashToHexColor(hash);
  }

  function hashToHexColor(hash) {
    var hue = Math.abs(hash) % 360;
    var s = 0.62;
    var l = 0.52;
    var c = (1 - Math.abs(2 * l - 1)) * s;
    var x = c * (1 - Math.abs((hue / 60) % 2 - 1));
    var m = l - c / 2;
    var r = 0;
    var g = 0;
    var b = 0;
    if (hue < 60) {
      r = c; g = x;
    } else if (hue < 120) {
      r = x; g = c;
    } else if (hue < 180) {
      g = c; b = x;
    } else if (hue < 240) {
      g = x; b = c;
    } else if (hue < 300) {
      r = x; b = c;
    } else {
      r = c; b = x;
    }
    function hex(n) {
      var v = Math.round((n + m) * 255);
      if (v < 0) v = 0;
      if (v > 255) v = 255;
      var s = v.toString(16);
      return s.length === 1 ? "0" + s : s;
    }
    return "#" + hex(r) + hex(g) + hex(b);
  }

  function colorWithAlpha(color, alpha) {
    if (!color || color[0] !== "#") {
      return "rgba(97, 175, 239, " + alpha + ")";
    }
    var hex = color.slice(1);
    if (hex.length === 3) {
      hex = hex[0] + hex[0] + hex[1] + hex[1] + hex[2] + hex[2];
    }
    if (hex.length !== 6) {
      return "rgba(97, 175, 239, " + alpha + ")";
    }
    var r = parseInt(hex.slice(0, 2), 16);
    var g = parseInt(hex.slice(2, 4), 16);
    var b = parseInt(hex.slice(4, 6), 16);
    if (Number.isNaN(r) || Number.isNaN(g) || Number.isNaN(b)) {
      return "rgba(97, 175, 239, " + alpha + ")";
    }
    return "rgba(" + r + ", " + g + ", " + b + ", " + alpha + ")";
  }

  var CLIENT_ID = generateClientId();
  var CLIENT_COLOR = pickColor(CLIENT_ID);
  var CRDT = window.CRDT;
  if (!CRDT) {
    console.error("CRDT library failed to load (check script order / cache / CSP)");
    return;
  }

  var roomTitle = document.getElementById("roomTitle");
  var status = document.getElementById("status");
  var ttlValue = document.getElementById("ttlValue");
  var ttlUnit = document.getElementById("ttlUnit");
  var expiresText = document.getElementById("expiresText");
  var peerTabs = document.getElementById("peerTabs");
  var dotUp = document.getElementById("dotUp");
  var dotDown = document.getElementById("dotDown");
  var content = document.getElementById("content");
  var cursorLayer = document.getElementById("cursorLayer");
  var appRoot = document.querySelector(".app");
  var fileList = document.getElementById("fileList");
  var filesCount = document.getElementById("filesCount");
  var filesStatus = document.getElementById("filesStatus");
  var filesPanel = document.getElementById("filesPanel");
  var passwordModal = document.getElementById("passwordModal");
  var modalTitle = document.getElementById("modalTitle");
  var modalHint = document.getElementById("modalHint");
  var modalFileNames = document.getElementById("modalFileNames");
  var modalPasswordLab = document.getElementById("modalPasswordLab");
  var modalPassword = document.getElementById("modalPassword");
  var modalError = document.getElementById("modalError");
  var modalCancel = document.getElementById("modalCancel");
  var modalConfirm = document.getElementById("modalConfirm");

  if (!roomTitle || !content || !ttlValue || !ttlUnit || !status) {
    console.error("GoClipboard: required DOM nodes missing (stale HTML/JS cache?)");
    return;
  }

  // CRDT document state
  var doc = new CRDT.Doc();
  var localClock = 0;
  var knownVersion = 0;
  var pendingOps = []; // local ops not yet acked by the server
  var sentBatches = {}; // seq -> { ids: {opId:true}, at: ms } awaiting ack
  var nextSeq = 1;
  var ackTimeoutMs = 5000;
  var lastSyncedText = ""; // server text at knownVersion (3-way merge base)
  var flushTimer = 0;
  var flushDelayMs = 60;
  var putFallbackTimer = 0;
  var putFailures = 0;
  var putInFlight = false;
  var applyingRemote = false;
  var lastExpiresAt = null;
  var syncRequestedAt = 0;

  var cursorTimer = 0;
  var cursorDelayMs = 80;
  var presenceHeartbeatMs = 5000;
  var presenceHeartbeatTimer = 0;
  var presencePruneMs = 2000;
  var presencePruneTimer = 0;
  var peerStaleMs = 14000;
  var remoteCursors = {};
  var socket = null;
  var reconnectTimer = 0;
  var reconnectAttempts = 0;
  var intentionalClose = false;
  var connected = false;
  var lastMsgAt = 0; // liveness: server pushes presence every ~5s
  var msgStaleMs = 15000;
  var restPollTimer = 0;
  var restPollMs = 4000;
  var trafficUpTimer = 0;
  var trafficDownTimer = 0;
  var fileUploadEnabled = false;
  var roomTitleClickCount = 0;
  var roomTitleClickTimer = 0;

  roomTitle.textContent = "/" + key;
  updateRoomTitleChrome();
  content.addEventListener("input", onInput);
  ttlValue.addEventListener("input", onSettingsChange);
  ttlUnit.addEventListener("change", onSettingsChange);
  content.addEventListener("mouseup", scheduleCursorSend);
  content.addEventListener("keyup", scheduleCursorSend);
  content.addEventListener("select", scheduleCursorSend);
  content.addEventListener("mousemove", function (e) {
    if (e.buttons === 1 && document.activeElement === content) {
      scheduleCursorSend();
    }
  });
  document.addEventListener("selectionchange", function () {
    if (document.activeElement === content) {
      scheduleCursorSend();
    }
  });
  content.addEventListener("scroll", renderCursors);
  window.addEventListener("resize", renderCursors);
  window.addEventListener("beforeunload", function () {
    intentionalClose = true;
    window.clearTimeout(reconnectTimer);
    if (socket) {
      try {
        socket.close();
      } catch (e) {
        // ignore
      }
    }
  });

  setupFileDropPaste();
  setupPasswordModal();
  setupRoomTitleToggle();

  window.setInterval(function () {
    if (lastExpiresAt) renderExpires(lastExpiresAt);
  }, 60000);
  // Refresh file list periodically so other clients see new uploads.
  window.setInterval(function () {
    loadFiles(true);
  }, 15000);

  updatePeers();
  load(false);
  loadFiles();
  connectWS();
  scheduleCursorSend();

  function connectWS() {
    window.clearTimeout(reconnectTimer);
    intentionalClose = false;
    setConnected(false);
    setStatus("error");

    if (socket) {
      try {
        socket.close();
      } catch (e) {
        // ignore
      }
      socket = null;
    }

    var url = wsURL + "?clientId=" + encodeURIComponent(CLIENT_ID);
    var ws;
    try {
      ws = new WebSocket(url);
    } catch (e) {
      setStatus("error");
      scheduleReconnect();
      return;
    }
    socket = ws;

    ws.onopen = function () {
      if (socket !== ws) return;
      reconnectAttempts = 0;
      stopRestPoll();
      lastMsgAt = Date.now();
      setConnected(true);
      setIdleStatus();
      scheduleCursorSend();
      startPresenceHeartbeat();
      // Unacked local ops are replayed when the initial state arrives.
    };

    ws.onmessage = function (event) {
      if (socket !== ws) return;
      lastMsgAt = Date.now();
      pulseTraffic("down");

      var msg;
      try {
        msg = JSON.parse(event.data);
      } catch (e) {
        setStatus("error");
        return;
      }
      if (!msg || !msg.type) return;

      if (msg.type === "state" || msg.type === "update") {
        handleState(msg);
        return;
      }
      if (msg.type === "ops") {
        handleRemoteOps(msg);
        return;
      }
      if (msg.type === "ack") {
        handleAck(msg);
        return;
      }
      if (msg.type === "files") {
        handleRemoteFiles(msg);
        return;
      }
      if (msg.type === "cursor") {
        updateRemoteCursors(msg);
        if (!pendingOps.length && !flushTimer) setIdleStatus();
      }
    };

    ws.onerror = function () {
      // onclose handles reconnect
    };

    ws.onclose = function () {
      if (socket !== ws) return;
      socket = null;
      stopPresenceHeartbeat();
      // Batches in flight have unknown fate; their ops stay in pendingOps and
      // are replayed onto the state snapshot after reconnect.
      sentBatches = {};
      remoteCursors = {};
      renderCursors();
      setConnected(false);
      updatePeers();
      setStatus("error");
      if (intentionalClose) {
        return;
      }
      scheduleReconnect();
    };
  }

  function scheduleReconnect() {
    reconnectAttempts++;
    // Exponential backoff with jitter: 1s, 2s, 4s, 8s, 15s cap.
    var delay = Math.min(15000, 1000 * Math.pow(2, Math.min(reconnectAttempts - 1, 4))) +
      Math.floor(Math.random() * 400);
    if (reconnectAttempts >= 2) {
      // WS looks unusable (blocked proxy / long outage): sync via REST too.
      startRestPoll();
    }
    window.clearTimeout(reconnectTimer);
    reconnectTimer = window.setTimeout(connectWS, delay);
  }

  function forceReconnect() {
    if (socket) {
      try {
        socket.close();
      } catch (e) {
        // ignore
      }
    }
  }

  function startPresenceHeartbeat() {
    stopPresenceHeartbeat();
    presenceHeartbeatTimer = window.setInterval(function () {
      if (!connected) return;
      if (socketLooksDead()) {
        // Half-open TCP: the browser still reports OPEN but nothing flows.
        forceReconnect();
        return;
      }
      if (typeof document !== "undefined" && document.visibilityState === "hidden") {
        return;
      }
      sendCursorPosition(true);
    }, presenceHeartbeatMs);
    presencePruneTimer = window.setInterval(pruneStalePeers, presencePruneMs);
  }

  // The server pushes presence every ~5s and acks every op batch; silence on
  // either channel means the connection is dead even if readyState says OPEN.
  function socketLooksDead() {
    var now = Date.now();
    if (lastMsgAt && now - lastMsgAt > msgStaleMs) return true;
    var seqs = Object.keys(sentBatches);
    for (var i = 0; i < seqs.length; i++) {
      var b = sentBatches[seqs[i]];
      if (b && now - b.at > ackTimeoutMs) return true;
    }
    return false;
  }

  function stopPresenceHeartbeat() {
    window.clearInterval(presenceHeartbeatTimer);
    presenceHeartbeatTimer = 0;
    window.clearInterval(presencePruneTimer);
    presencePruneTimer = 0;
  }

  function pruneStalePeers() {
    var now = Date.now();
    var next = {};
    var changed = false;
    Object.keys(remoteCursors).forEach(function (id) {
      var c = remoteCursors[id];
      if (!c) {
        changed = true;
        return;
      }
      var ts = typeof c.timestamp === "number" ? c.timestamp : 0;
      if (ts > 0 && now - ts > peerStaleMs) {
        changed = true;
        return;
      }
      next[id] = c;
    });
    if (!changed) return;
    remoteCursors = next;
    renderCursors();
    updatePeers();
  }

  document.addEventListener("visibilitychange", function () {
    if (document.visibilityState === "visible" && connected) {
      sendCursorPosition(true);
    }
  });

  // --- CRDT collaboration ----------------------------------------------------

  // Lamport: any observed clock in the doc (all sites), not only ours.
  function bumpClockFromDoc() {
    var m = doc.maxClock ? doc.maxClock() : 0;
    if (m > localClock) localClock = m;
  }

  function bumpClockFromOps(ops) {
    (ops || []).forEach(function (op) {
      if (!op || !op.id) return;
      var p = CRDT.parseID(op.id);
      if (p && p.clock > localClock) localClock = p.clock;
    });
  }

  function handleState(data) {
    var version = data.version || 0;
    var localText = content.value;
    var hadPending = pendingOps.length > 0;
    // Capture before replacing the doc so caret can stick to the same characters.
    var sel = captureSelection();

    // Always adopt server CRDT tree (authoritative snapshot).
    doc = new CRDT.Doc();
    if (data.items && data.items.length) {
      var fr = doc.fromItems(data.items);
      if (!fr.ok) {
        doc = CRDT.buildFromString(CLIENT_ID, data.content || "") || new CRDT.Doc();
      }
    } else if (data.content) {
      doc = CRDT.buildFromString("server", data.content) || new CRDT.Doc();
    }
    localClock = 0;
    bumpClockFromDoc();
    knownVersion = version;
    syncRequestedAt = 0;
    updateMeta(data);

    var serverText = doc.materialize();
    lastSyncedText = serverText;
    // In-flight batches are superseded by this snapshot; unacked ops are
    // replayed below and will be flushed again.
    sentBatches = {};

    // Replay unacked local ops onto the server tree (true CRDT merge).
    // Re-diffing the textarea instead would resurrect text that other peers
    // deleted while we were disconnected.
    var replayed = false;
    if (pendingOps.length) {
      var r = doc.applyBatch(pendingOps);
      if (r.ok) {
        replayed = true;
        bumpClockFromDoc();
      } else {
        // Ids no longer line up (e.g. doc was rebuilt via REST) — fall back
        // to preserving the visible draft by re-diffing.
        pendingOps = [];
      }
    }
    // Only re-diff when there really are unsynced local edits (replay failed).
    // Without pending ops the textarea is just a stale view — re-diffing it
    // would revert the very change this snapshot delivers.
    if (!replayed && hadPending && localText !== serverText) {
      applyLocalText(localText);
    }
    applyingRemote = true;
    setContentValue(doc.materialize(), sel);
    applyingRemote = false;
    refreshRemoteCursorPositions();
    reanchorRemoteCursorsFromPos();
    if (data.ttlSeconds) setTTLControls(data.ttlSeconds);
    renderCursors();
    scheduleCursorSend();
    if (pendingOps.length) {
      scheduleFlush();
    } else {
      setIdleStatus();
    }
  }

  function handleRemoteOps(data) {
    var version = data.version || 0;
    var fromSelf = !!(data.updatedBy && data.updatedBy === CLIENT_ID);

    if (version <= knownVersion) {
      if (version === knownVersion) updateMeta(data);
      return;
    }
    if (version > knownVersion + 1) {
      // We missed intermediate updates (dropped/coalesced under bad network).
      // Applying just this batch would silently diverge — get a snapshot.
      requestSync();
      return;
    }

    if (fromSelf) {
      // Our own batch echoed back: ops are already in the local doc.
      knownVersion = version;
      updateMeta(data);
      if (typeof data.content === "string") lastSyncedText = data.content;
      // Secondary ack path (covers a dropped direct ack).
      var acked = {};
      (data.ops || []).forEach(function (op) {
        if (op && op.id) acked[op.id] = true;
      });
      if (Object.keys(acked).length) {
        pendingOps = pendingOps.filter(function (op) {
          return !(op && op.id && acked[op.id]);
        });
        forgetSentIds(acked);
      }
      if (!flushTimer && !pendingOps.length) setIdleStatus();
      else if (pendingOps.length) scheduleFlush();
      return;
    }

    var sel = captureSelection();
    var oldText = doc.materialize();
    var ops = data.ops || [];
    if (ops.length) {
      var r = doc.applyBatch(ops);
      if (!r.ok) {
        // Diverged — ask the server for an authoritative snapshot.
        requestSync();
        return;
      }
      bumpClockFromOps(ops);
      if (typeof data.content === "string") lastSyncedText = data.content;
    } else if (typeof data.content === "string") {
      // Fallback if ops missing but content present
      applyingRemote = true;
      setContentValue(data.content, sel);
      applyingRemote = false;
      knownVersion = version;
      lastSyncedText = data.content;
      updateMeta(data);
      onLocalTextChanged(oldText, data.content || "");
      renderCursors();
      scheduleCursorSend();
      setIdleStatus();
      return;
    }

    knownVersion = version;
    updateMeta(data);

    var next = doc.materialize();
    applyingRemote = true;
    setContentValue(next, sel);
    applyingRemote = false;
    // Keep other peers' carets sticky across this remote edit.
    onLocalTextChanged(oldText, next);
    renderCursors();
    scheduleCursorSend();
    setIdleStatus();
  }

  // Direct per-batch ack from the server. Broadcast echoes can be coalesced
  // into snapshots (or skipped entirely for idempotent re-applies), so this is
  // the authoritative signal that a sent batch is durable.
  function handleAck(msg) {
    var batch = sentBatches[msg.seq || 0];
    if (batch) {
      delete sentBatches[msg.seq || 0];
      pendingOps = pendingOps.filter(function (op) {
        return !(op && op.id && batch.ids[op.id]);
      });
    }
    if (msg.error) {
      // Batch rejected (capacity/validation): its ops were dropped above and
      // the server follows up with a state snapshot to converge on.
      setStatus("error");
      return;
    }
    var version = msg.version || 0;
    if (version === knownVersion + 1) {
      knownVersion = version;
    }
    // version > knownVersion + 1: peers' intermediate updates are still in
    // flight on this socket; let them advance knownVersion in order.
    if (msg.expiresAt) updateMeta(msg);
    if (pendingOps.length) scheduleFlush();
    else if (!flushTimer) setIdleStatus();
  }

  function forgetSentIds(ackedIds) {
    Object.keys(sentBatches).forEach(function (seq) {
      var b = sentBatches[seq];
      if (!b) return;
      Object.keys(b.ids).forEach(function (id) {
        if (ackedIds[id]) delete b.ids[id];
      });
      if (!Object.keys(b.ids).length) delete sentBatches[seq];
    });
  }

  // Ask the server for a full snapshot (rate-limited; used on version gaps).
  function requestSync() {
    var now = Date.now();
    if (syncRequestedAt && now - syncRequestedAt < 1500) return;
    if (socket && socket.readyState === WebSocket.OPEN) {
      syncRequestedAt = now;
      try {
        socket.send(JSON.stringify({ type: "sync" }));
      } catch (e) {
        // Socket broken; reconnect path will resync via initial state.
      }
    } else {
      load(true);
    }
  }

  function applyLocalText(newText) {
    var oldText = doc.materialize();
    var oldChars = CRDT.codePoints(oldText);
    var oldIds = doc.visibleIds();
    var newChars = CRDT.codePoints(newText);
    if (oldText === newText) return;

    // Lamport: new ids must outrank any existing sibling clocks.
    bumpClockFromDoc();
    var diff = CRDT.diffToOps(oldChars, oldIds, newChars, CLIENT_ID, localClock);
    if (!diff.ops.length) return;

    localClock = diff.clock;
    var r = doc.applyBatch(diff.ops);
    if (!r.ok) {
      setStatus("error");
      return;
    }
    pendingOps = pendingOps.concat(diff.ops);
    scheduleFlush();
  }

  function onInput() {
    if (applyingRemote) return;
    var oldText = doc.materialize();
    applyLocalText(content.value);
    // Prefer CRDT materialize so caret math matches visibleIds / afterId anchors.
    var newText = doc.materialize();
    onLocalTextChanged(oldText, newText);
    renderCursors();
    scheduleCursorSend();
  }

  // After local or remote doc text changes, keep peer carets on the same characters.
  function onLocalTextChanged(oldText, newText) {
    if (oldText === newText) return;
    var ids = doc.visibleIds();
    Object.keys(remoteCursors).forEach(function (id) {
      var c = remoteCursors[id];
      if (!c) return;
      if (c.afterId !== undefined && c.afterId !== null) {
        var p = posFromAfterId(c.afterId, ids);
        if (p >= 0) {
          c.pos = p;
        } else {
          c.pos = mapCaretIndex(oldText, newText, c.pos);
          c.afterId = anchorIdAt(c.pos, ids);
        }
      } else {
        c.pos = mapCaretIndex(oldText, newText, c.pos);
        c.afterId = anchorIdAt(c.pos, ids);
      }
      if (c.endAfterId !== undefined && c.endAfterId !== null) {
        var e = posFromAfterId(c.endAfterId, ids);
        if (e >= 0) {
          c.end = e;
        } else {
          c.end = mapCaretIndex(oldText, newText, c.end != null ? c.end : c.pos);
          c.endAfterId = anchorIdAt(c.end, ids);
        }
      } else {
        c.end = mapCaretIndex(oldText, newText, c.end != null ? c.end : c.pos);
        c.endAfterId = anchorIdAt(c.end, ids);
      }
    });
  }

  function onSettingsChange() {
    // TTL-only change: push a no-content flush with ttl when possible, else PUT.
    scheduleFlush();
  }

  function scheduleFlush() {
    window.clearTimeout(flushTimer);
    flushTimer = window.setTimeout(function () {
      flushTimer = 0;
      flushPending();
    }, flushDelayMs);
  }

  function flushPending() {
    var ttlSeconds = readTTLSeconds();
    if (!ttlSeconds) {
      setStatus("error");
      return;
    }

    if (connected && socket && socket.readyState === WebSocket.OPEN) {
      // Only send ops not already awaiting an ack; ops stay in pendingOps
      // until the server confirms them (WebSocket.send is just a local
      // buffer write — on a flaky link "sent" is not "delivered").
      var outstanding = {};
      Object.keys(sentBatches).forEach(function (seq) {
        var b = sentBatches[seq];
        if (!b) return;
        Object.keys(b.ids).forEach(function (id) {
          outstanding[id] = true;
        });
      });
      var batch = pendingOps.filter(function (op) {
        return op && op.id && !outstanding[op.id];
      });
      if (!batch.length && pendingOps.length) {
        // Everything is in flight; ack or the watchdog will move things on.
        return;
      }
      // Empty batch = TTL-only update; the server refreshes expiry without a
      // full-content PUT (which could clobber peers' edits we haven't seen).
      var seq = nextSeq++;
      try {
        socket.send(JSON.stringify({
          type: "ops",
          ops: batch,
          ttlSeconds: ttlSeconds,
          seq: seq
        }));
        pulseTraffic("up");
        if (batch.length) {
          var ids = {};
          batch.forEach(function (op) {
            ids[op.id] = true;
          });
          sentBatches[seq] = { ids: ids, at: Date.now() };
        }
      } catch (e) {
        // Socket died mid-send; ops remain pending and the reconnect's state
        // snapshot replay will carry them over.
      }
      return;
    }

    // WS down. While a quick reconnect is still likely, hold the ops — the
    // post-reconnect snapshot replay merges them without data loss. Only fall
    // back to REST once WS looks unusable.
    if (!intentionalClose && reconnectAttempts < 2) return;
    schedulePutFallback();
  }

  function schedulePutFallback() {
    window.clearTimeout(putFallbackTimer);
    // Backoff: 200ms → 3.2s cap, so a dead server isn't hammered.
    var delay = Math.min(3200, 200 * Math.pow(2, Math.min(putFailures, 4)));
    putFallbackTimer = window.setTimeout(function () {
      putFallbackTimer = 0;
      var ttlSeconds = readTTLSeconds();
      if (!ttlSeconds) {
        setStatus("error");
        return;
      }
      putReplace(content.value, ttlSeconds);
    }, delay);
  }

  function putReplace(text, ttlSeconds) {
    if (putInFlight) {
      schedulePutFallback();
      return;
    }
    putInFlight = true;
    pulseTraffic("up");
    var body = {
      content: text,
      ttlSeconds: ttlSeconds,
      clientId: CLIENT_ID
    };
    // Conditional save: if the server moved past what we saw, we get a 409
    // with current state and merge instead of overwriting peers' edits.
    if (knownVersion > 0) body.baseVersion = knownVersion;
    fetch(apiURL, {
      method: "PUT",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json"
      },
      body: JSON.stringify(body)
    })
      .then(function (response) {
        pulseTraffic("down");
        return response.json().then(function (data) {
          return { ok: response.ok, status: response.status, data: data };
        }).catch(function () {
          return { ok: response.ok, status: response.status, data: {} };
        });
      })
      .then(function (result) {
        putInFlight = false;
        if (result.status === 409) {
          putFailures = 0;
          mergeServerState(result.data);
          return;
        }
        if (result.status === 429) {
          putFailures++;
          setStatus("error");
          schedulePutFallback();
          return;
        }
        if (!result.ok) {
          throw new Error(result.data.error || ("HTTP " + result.status));
        }
        putFailures = 0;
        knownVersion = result.data.version || 0;
        updateMeta(result.data);
        pendingOps = [];
        sentBatches = {};
        // After PUT replace, rebuild local doc to match server chain.
        doc = CRDT.buildFromString(CLIENT_ID, text) || new CRDT.Doc();
        localClock = 0;
        bumpClockFromDoc();
        lastSyncedText = text;
        setTTLControls(result.data.ttlSeconds || ttlSeconds);
        if (content.value !== text) {
          // User kept typing while the PUT was in flight — push the rest.
          schedulePutFallback();
        } else {
          setIdleStatus();
        }
      })
      .catch(function () {
        putInFlight = false;
        putFailures++;
        setStatus("error");
        schedulePutFallback();
      });
  }

  // Adopt newer server state seen over REST (409 conflict or offline poll),
  // merging concurrent edits three-way instead of last-write-wins.
  function mergeServerState(data) {
    var theirs = data.content || "";
    var version = data.version || 0;
    var base = lastSyncedText;
    var mine = content.value;
    var sel = captureSelection();
    sel.useAnchor = false;

    var merged;
    if (mine === base || mine === theirs) {
      merged = theirs;
    } else if (theirs === base) {
      merged = mine;
    } else {
      merged = merge3(base, mine, theirs);
    }

    doc = CRDT.buildFromString(CLIENT_ID, merged) || new CRDT.Doc();
    localClock = 0;
    bumpClockFromDoc();
    knownVersion = version;
    lastSyncedText = theirs;
    pendingOps = [];
    sentBatches = {};
    updateMeta(data);
    if (data.ttlSeconds) setTTLControls(data.ttlSeconds);
    var oldText = mine;
    applyingRemote = true;
    setContentValue(merged, sel);
    applyingRemote = false;
    shiftRemoteCursorsByText(oldText, merged);
    renderCursors();
    if (merged !== theirs) {
      // Our side still has edits the server lacks — push the merged text.
      schedulePutFallback();
    } else {
      setIdleStatus();
    }
  }

  // Three-way text merge via the CRDT: apply both sides' diffs against the
  // common base as concurrent op sets and let RGA ordering resolve them.
  function merge3(base, mine, theirs) {
    var d = CRDT.buildFromString("base", base);
    if (!d) return mine;
    var baseCps = CRDT.codePoints(base);
    var ids = d.visibleIds();
    var clock = d.maxClock();
    var dTheirs = CRDT.diffToOps(baseCps, ids, CRDT.codePoints(theirs), "their", clock);
    var dMine = CRDT.diffToOps(baseCps, ids, CRDT.codePoints(mine), "mine", clock);
    if (dTheirs.ops.length && !d.applyBatch(dTheirs.ops).ok) return mine;
    if (dMine.ops.length && !d.applyBatch(dMine.ops).ok) return mine;
    return d.materialize();
  }

  // REST polling keeps two-way sync alive when WS is unusable (blocked proxy,
  // long outage); previously offline clients never saw peers' edits at all.
  function startRestPoll() {
    if (restPollTimer) return;
    restPollTimer = window.setInterval(function () {
      if (connected || putInFlight) return;
      fetch(apiURL, { headers: { Accept: "application/json" } })
        .then(function (response) {
          if (!response.ok) throw new Error("HTTP " + response.status);
          return response.json();
        })
        .then(function (data) {
          if (connected || putInFlight) return;
          if ((data.version || 0) !== knownVersion) {
            mergeServerState(data);
          }
        })
        .catch(function () {
          // Still offline; keep polling.
        });
    }, restPollMs);
  }

  function stopRestPoll() {
    window.clearInterval(restPollTimer);
    restPollTimer = 0;
  }

  // Map a caret (code-point gap index) through a text change. Sticky-left at the
  // edit site: caret at the insert point stays before newly inserted characters.
  function mapCaretIndex(oldText, newText, pos) {
    var oldChars = CRDT.codePoints(oldText || "");
    var newChars = CRDT.codePoints(newText || "");
    pos = clampPos(pos, oldChars.length);
    if (oldText === newText) return pos;

    var lo = 0;
    var roOld = oldChars.length;
    var roNew = newChars.length;
    while (lo < roOld && lo < roNew && oldChars[lo] === newChars[lo]) lo++;
    while (
      roOld > lo &&
      roNew > lo &&
      oldChars[roOld - 1] === newChars[roNew - 1]
    ) {
      roOld--;
      roNew--;
    }

    if (pos <= lo) return pos;
    if (pos >= roOld) return pos + (roNew - roOld);
    // Inside deleted span → collapse to start of the edit.
    return lo;
  }

  function shiftRemoteCursorsByText(oldText, newText) {
    if (oldText === newText) return;
    Object.keys(remoteCursors).forEach(function (id) {
      var c = remoteCursors[id];
      if (!c) return;
      c.pos = mapCaretIndex(oldText, newText, c.pos);
      c.end = mapCaretIndex(oldText, newText, c.end != null ? c.end : c.pos);
    });
    // Re-derive anchors from the shifted indices against the current CRDT doc.
    reanchorRemoteCursorsFromPos();
  }

  function anchorIdAt(pos, ids) {
    pos = clampPos(pos, ids.length);
    return pos > 0 ? ids[pos - 1] : "";
  }

  // afterId "" / null → document start. Returns -1 if id is unknown.
  function posFromAfterId(afterId, ids) {
    if (afterId === "" || afterId == null) return 0;
    for (var i = 0; i < ids.length; i++) {
      if (ids[i] === afterId) return i + 1;
    }
    return -1;
  }

  function reanchorRemoteCursorsFromPos() {
    var ids = doc.visibleIds();
    Object.keys(remoteCursors).forEach(function (id) {
      var c = remoteCursors[id];
      if (!c) return;
      c.pos = clampPos(c.pos, ids.length);
      c.end = clampPos(c.end != null ? c.end : c.pos, ids.length);
      c.afterId = anchorIdAt(c.pos, ids);
      c.endAfterId = anchorIdAt(c.end, ids);
    });
  }

  // Prefer CRDT anchors; fall back to stored display indices.
  function refreshRemoteCursorPositions() {
    var ids = doc.visibleIds();
    Object.keys(remoteCursors).forEach(function (id) {
      var c = remoteCursors[id];
      if (!c) return;
      if (c.afterId !== undefined && c.afterId !== null) {
        var p = posFromAfterId(c.afterId, ids);
        if (p >= 0) c.pos = p;
        else c.pos = clampPos(c.pos, ids.length);
      } else {
        c.pos = clampPos(c.pos, ids.length);
      }
      if (c.endAfterId !== undefined && c.endAfterId !== null) {
        var e = posFromAfterId(c.endAfterId, ids);
        if (e >= 0) c.end = e;
        else c.end = clampPos(c.end != null ? c.end : c.pos, ids.length);
      } else {
        c.end = clampPos(c.end != null ? c.end : c.pos, ids.length);
      }
    });
  }

  function updateRemoteCursors(data) {
    var ids = doc.visibleIds();
    var next = {};
    (data.cursors || []).forEach(function (c) {
      if (!c || !c.clientId || c.clientId === CLIENT_ID) return;
      var prev = remoteCursors[c.clientId];
      var rawPos = Number(c.cursorPos);
      if (!Number.isFinite(rawPos) || rawPos < 0) rawPos = 0;
      var rawEnd =
        c.selectionEnd === undefined || c.selectionEnd === null
          ? rawPos
          : Number(c.selectionEnd);
      if (!Number.isFinite(rawEnd) || rawEnd < 0) rawEnd = rawPos;

      // Wire anchors (preferred). Empty string = document start.
      var hasAfter = typeof c.afterId === "string";
      var hasEndAfter = typeof c.selectionAfterId === "string";
      var afterId = hasAfter ? c.afterId : undefined;
      var endAfterId = hasEndAfter ? c.selectionAfterId : undefined;

      var entry = {
        clientId: c.clientId,
        color: c.color,
        timestamp: c.timestamp,
        rawPos: rawPos,
        rawEnd: rawEnd,
        cursorPos: rawPos,
        selectionEnd: rawEnd
      };

      if (hasAfter) {
        var p = posFromAfterId(afterId, ids);
        if (p < 0 && prev && prev.afterId === afterId) {
          // Anchor not in doc yet (or tombstoned) — keep last display.
          entry.pos = prev.pos;
          entry.end = prev.end;
          entry.afterId = prev.afterId;
          entry.endAfterId = prev.endAfterId;
          next[c.clientId] = entry;
          return;
        }
        entry.afterId = afterId;
        entry.pos = p >= 0 ? p : clampPos(rawPos, ids.length);
        if (hasEndAfter) {
          var e = posFromAfterId(endAfterId, ids);
          entry.endAfterId = endAfterId;
          entry.end = e >= 0 ? e : clampPos(rawEnd, ids.length);
        } else {
          entry.end = entry.pos;
          entry.endAfterId = entry.afterId;
        }
        next[c.clientId] = entry;
        return;
      }

      // Legacy absolute indices only.
      // Presence ticks rebroadcast stale absolutes while our doc is already ahead —
      // keep the locally shifted display so the caret does not jump left each keystroke.
      if (
        prev &&
        Number(prev.rawPos) === rawPos &&
        Number(prev.rawEnd) === rawEnd
      ) {
        entry.pos = prev.pos;
        entry.end = prev.end;
        entry.afterId = prev.afterId;
        entry.endAfterId = prev.endAfterId;
        next[c.clientId] = entry;
        return;
      }

      entry.pos = clampPos(rawPos, ids.length);
      entry.end = clampPos(rawEnd, ids.length);
      entry.afterId = anchorIdAt(entry.pos, ids);
      entry.endAfterId = anchorIdAt(entry.end, ids);
      next[c.clientId] = entry;
    });

    if (cursorsVisuallyEqual(remoteCursors, next)) {
      Object.keys(next).forEach(function (id) {
        if (remoteCursors[id] && next[id]) {
          remoteCursors[id].timestamp = next[id].timestamp;
        }
      });
      return;
    }
    remoteCursors = next;
    ensureUniqueSelfColor();
    renderCursors();
    updatePeers();
  }

  function cursorsVisuallyEqual(a, b) {
    var ka = Object.keys(a).sort();
    var kb = Object.keys(b).sort();
    if (ka.length !== kb.length) return false;
    for (var i = 0; i < ka.length; i++) {
      if (ka[i] !== kb[i]) return false;
      var ca = a[ka[i]];
      var cb = b[kb[i]];
      if (!ca || !cb) return false;
      if (String(ca.afterId || "") !== String(cb.afterId || "")) return false;
      if (String(ca.endAfterId || "") !== String(cb.endAfterId || "")) return false;
      if (Number(ca.pos) !== Number(cb.pos)) return false;
      if (Number(ca.end) !== Number(cb.end)) return false;
      if (normalizeColor(ca.color) !== normalizeColor(cb.color)) return false;
    }
    return true;
  }

  function peerUsedColors() {
    var used = [];
    Object.keys(remoteCursors).forEach(function (id) {
      var c = remoteCursors[id];
      if (c && c.color) used.push(c.color);
    });
    return used;
  }

  function colorForPeer(id, cursor) {
    if (id === CLIENT_ID) return CLIENT_COLOR;
    if (cursor && cursor.color) return cursor.color;
    var c = remoteCursors[id];
    if (c && c.color) return c.color;
    return pickColor(id, [CLIENT_COLOR].concat(peerUsedColors()));
  }

  function ensureUniqueSelfColor() {
    var selfColor = normalizeColor(CLIENT_COLOR);
    var conflictId = null;
    Object.keys(remoteCursors).forEach(function (id) {
      var c = remoteCursors[id];
      if (c && normalizeColor(c.color) === selfColor) {
        if (!conflictId || id > conflictId) conflictId = id;
      }
    });
    if (!conflictId) return;
    if (CLIENT_ID < conflictId) return;

    var next = pickColor(CLIENT_ID, peerUsedColors());
    if (normalizeColor(next) === selfColor) return;
    CLIENT_COLOR = next;
    scheduleCursorSend();
  }

  function renderCursors() {
    cursorLayer.innerHTML = "";
    var scrollTop = content.scrollTop;
    var scrollLeft = content.scrollLeft;
    var text = content.value;
    var visibleIds = doc.visibleIds();
    var maxCp = Math.max(visibleIds.length, CRDT.codePoints(text).length);

    // Stable peer order so overlapping carets don't reshuffle on each presence tick.
    var ids = Object.keys(remoteCursors).sort();
    if (!ids.length) return;

    ids.forEach(function (id, order) {
      var cursor = remoteCursors[id];
      // Prefer live CRDT anchors so concurrent local typing cannot slide the caret.
      var caretCp = -1;
      var otherCp = -1;
      if (cursor.afterId !== undefined && cursor.afterId !== null) {
        caretCp = posFromAfterId(cursor.afterId, visibleIds);
      }
      if (cursor.endAfterId !== undefined && cursor.endAfterId !== null) {
        otherCp = posFromAfterId(cursor.endAfterId, visibleIds);
      }
      if (caretCp < 0) caretCp = clampPos(cursor.pos != null ? cursor.pos : cursor.cursorPos, maxCp);
      if (otherCp < 0) {
        otherCp = clampPos(
          cursor.end != null
            ? cursor.end
            : cursor.selectionEnd != null
              ? cursor.selectionEnd
              : caretCp,
          maxCp
        );
      }
      caretCp = clampPos(caretCp, maxCp);
      otherCp = clampPos(otherCp, maxCp);

      var caret = CRDT.codePointToUtf16Offset(text, caretCp);
      var other = CRDT.codePointToUtf16Offset(text, otherCp);
      var color = colorForPeer(id, cursor);

      var selStart = Math.min(caret, other);
      var selEnd = Math.max(caret, other);
      if (selEnd > selStart) {
        paintSelection(selStart, selEnd, color, scrollTop, scrollLeft);
      }

      var coords = getCaretMetrics(caret);
      if (!coords) return;

      var el = document.createElement("div");
      el.className = "cursor-indicator";
      // Fixed paint order only — no position offset when carets coincide.
      el.style.zIndex = String(3 + order);
      el.style.top = (coords.top - scrollTop) + "px";
      el.style.left = (coords.left - scrollLeft) + "px";
      el.style.height = coords.height + "px";
      el.style.backgroundColor = color;

      var label = document.createElement("span");
      label.className = "cursor-label";
      label.textContent = (cursor.clientId || id).substring(0, 4);
      label.style.backgroundColor = colorWithAlpha(color, 0.92);
      if (coords.top - scrollTop < 18) {
        label.classList.add("cursor-label-below");
      }
      el.appendChild(label);

      cursorLayer.appendChild(el);
    });
  }

  function clampPos(pos, max) {
    var n = Number(pos);
    if (!Number.isFinite(n) || n < 0) return 0;
    return Math.min(Math.floor(n), max);
  }

  function syncMirrorStyles(mirror) {
    var style = getComputedStyle(content);
    var props = [
      "boxSizing", "width", "font", "fontFamily", "fontSize", "fontWeight",
      "fontStyle", "fontVariant", "fontFeatureSettings", "fontKerning",
      "letterSpacing", "wordSpacing", "textIndent", "textTransform",
      "textAlign", "lineHeight", "tabSize", "MozTabSize", "whiteSpace",
      "wordBreak", "overflowWrap", "wordWrap", "paddingTop", "paddingRight",
      "paddingBottom", "paddingLeft", "borderTopWidth", "borderRightWidth",
      "borderBottomWidth", "borderLeftWidth", "borderTopStyle",
      "borderRightStyle", "borderBottomStyle", "borderLeftStyle"
    ];
    for (var i = 0; i < props.length; i++) {
      var p = props[i];
      try {
        mirror.style[p] = style[p];
      } catch (e) {
        // ignore unsupported
      }
    }
    mirror.style.width = content.clientWidth + "px";
    mirror.style.height = "auto";
    mirror.style.minHeight = content.clientHeight + "px";
    mirror.style.borderColor = "transparent";
    mirror.style.overflow = "hidden";
    mirror.style.whiteSpace = "pre-wrap";
    mirror.style.wordWrap = "break-word";
    mirror.style.overflowWrap = "break-word";
  }

  function escapeHTML(str) {
    return String(str)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/\n/g, "<br>");
  }

  function paintSelection(start, end, color, scrollTop, scrollLeft) {
    var mirror = document.getElementById("cursorMirror");
    syncMirrorStyles(mirror);

    var text = content.value;
    start = clampPos(start, text.length);
    end = clampPos(end, text.length);
    if (end <= start) return;

    var before = escapeHTML(text.slice(0, start));
    var mid = escapeHTML(text.slice(start, end));
    var after = escapeHTML(text.slice(end));

    if (text.slice(start, end).endsWith("\n")) {
      mid += "<br>";
    }

    mirror.innerHTML =
      before +
      '<mark class="mirror-sel">' + (mid || "\u200b") + "</mark>" +
      after;

    var mark = mirror.querySelector(".mirror-sel");
    if (!mark) return;

    var mirrorRect = mirror.getBoundingClientRect();
    var rects = mark.getClientRects();
    var fill = colorWithAlpha(color, 0.22);

    for (var i = 0; i < rects.length; i++) {
      var r = rects[i];
      if (r.width < 0.5 && r.height < 0.5) continue;

      var sel = document.createElement("div");
      sel.className = "cursor-selection";
      sel.style.top = (r.top - mirrorRect.top - scrollTop) + "px";
      sel.style.left = (r.left - mirrorRect.left - scrollLeft) + "px";
      sel.style.width = Math.max(r.width, 2) + "px";
      sel.style.height = Math.max(r.height, 2) + "px";
      sel.style.backgroundColor = fill;
      cursorLayer.appendChild(sel);
    }
  }

  function getCaretMetrics(pos) {
    var mirror = document.getElementById("cursorMirror");
    syncMirrorStyles(mirror);

    var text = content.value;
    pos = clampPos(pos, text.length);
    var before = escapeHTML(text.slice(0, pos));
    var after = escapeHTML(text.slice(pos));

    if (pos === text.length && text.endsWith("\n")) {
      before += "<br>";
    }

    mirror.innerHTML =
      before +
      '<span class="mirror-caret">\u200b</span>' +
      after;

    var caret = mirror.querySelector(".mirror-caret");
    if (!caret) return null;

    var mirrorRect = mirror.getBoundingClientRect();
    var caretRect = caret.getBoundingClientRect();
    var height = caretRect.height || parseFloat(getComputedStyle(content).lineHeight) || 20;

    return {
      left: caretRect.left - mirrorRect.left,
      top: caretRect.top - mirrorRect.top,
      height: height
    };
  }

  function scheduleCursorSend() {
    window.clearTimeout(cursorTimer);
    cursorTimer = window.setTimeout(sendCursorPosition, cursorDelayMs);
  }

  function sendCursorPosition(silent) {
    if (!socket || socket.readyState !== WebSocket.OPEN) {
      return;
    }
    var text = content.value;
    var ids = doc.visibleIds();
    var startCp = CRDT.utf16ToCodePointOffset(text, content.selectionStart);
    var endCp = CRDT.utf16ToCodePointOffset(text, content.selectionEnd);
    startCp = clampPos(startCp, ids.length);
    endCp = clampPos(endCp, ids.length);
    var caret = content.selectionDirection === "backward" ? startCp : endCp;
    var anchor = caret === startCp ? endCp : startCp;
    // CRDT left-neighbor anchors — peers resolve these against their own doc so
    // absolute indices that lag by one keystroke cannot pull carets sideways.
    var afterId = caret > 0 ? ids[caret - 1] : "";
    var selectionAfterId = anchor > 0 ? ids[anchor - 1] : "";
    try {
      socket.send(JSON.stringify({
        type: "cursor",
        cursorPos: caret,
        selectionEnd: anchor,
        afterId: afterId,
        selectionAfterId: selectionAfterId,
        color: CLIENT_COLOR
      }));
      if (!silent) pulseTraffic("up");
    } catch (e) {
      // ignore send errors; reconnect handles recovery
    }
  }

  function load(force) {
    pulseTraffic("down");
    fetch(apiURL, { headers: { Accept: "application/json" } })
      .then(function (response) {
        if (!response.ok) throw new Error("HTTP " + response.status);
        return response.json();
      })
      .then(function (data) {
        // REST has no CRDT items — rebuild from content; WS state will refine.
        if (!force && pendingOps.length) {
          return;
        }
        // REST rebuilds CRDT ids from scratch — keep caret by code-point index only.
        var sel = captureSelection();
        sel.useAnchor = false;
        doc = CRDT.buildFromString("server", data.content || "") || new CRDT.Doc();
        localClock = 0;
        knownVersion = data.version || 0;
        lastSyncedText = data.content || "";
        pendingOps = [];
        sentBatches = {};
        applyingRemote = true;
        setContentValue(data.content || "", sel);
        applyingRemote = false;
        setTTLControls(data.ttlSeconds || 3600);
        updateMeta(data);
        renderCursors();
        setIdleStatus();
      })
      .catch(function () {
        setStatus("error");
      });
  }

  // Snapshot local selection against the current CRDT sequence so remote merges
  // can restore the caret after the same characters instead of a raw UTF-16 index.
  function captureSelection() {
    var text = content.value;
    var start = content.selectionStart || 0;
    var end = content.selectionEnd || 0;
    var dir = content.selectionDirection || "none";
    var ids = doc.visibleIds();
    var startCp = CRDT.utf16ToCodePointOffset(text, start);
    var endCp = CRDT.utf16ToCodePointOffset(text, end);
    startCp = clampPos(startCp, ids.length);
    endCp = clampPos(endCp, ids.length);
    return {
      startCp: startCp,
      endCp: endCp,
      dir: dir,
      // Stick to the character left of each edge when CRDT ids survive the merge.
      startLeftId: startCp > 0 ? ids[startCp - 1] : null,
      endLeftId: endCp > 0 ? ids[endCp - 1] : null,
      useAnchor: true,
      hadFocus: document.activeElement === content
    };
  }

  // leftId === null → caret at document start.
  // leftId === undefined → no anchor; use fallbackCp.
  function resolveCaretCp(leftId, fallbackCp, ids) {
    if (leftId === undefined) return clampPos(fallbackCp, ids.length);
    if (leftId == null) return 0;
    for (var i = 0; i < ids.length; i++) {
      if (ids[i] === leftId) return i + 1;
    }
    // Left neighbor deleted — keep best-effort index, clamped.
    return clampPos(fallbackCp, ids.length);
  }

  function restoreSelection(sel) {
    if (!sel) return;
    var text = content.value;
    var ids = doc.visibleIds();
    var maxCp = Math.max(ids.length, CRDT.codePoints(text).length);
    var startCp;
    var endCp;
    if (sel.useAnchor === false) {
      // Full rebuild without shared CRDT ids — keep absolute code-point index.
      startCp = clampPos(sel.startCp, maxCp);
      endCp = clampPos(sel.endCp, maxCp);
    } else {
      startCp = resolveCaretCp(sel.startLeftId, sel.startCp, ids);
      endCp = resolveCaretCp(sel.endLeftId, sel.endCp, ids);
      startCp = clampPos(startCp, maxCp);
      endCp = clampPos(endCp, maxCp);
    }
    var start = CRDT.codePointToUtf16Offset(text, startCp);
    var end = CRDT.codePointToUtf16Offset(text, endCp);
    try {
      if (sel.dir && sel.dir !== "none") {
        content.setSelectionRange(start, end, sel.dir);
      } else {
        content.setSelectionRange(start, end);
      }
    } catch (e) {
      // ignore
    }
  }

  function setContentValue(next, sel) {
    var value = next || "";
    var hadFocus = sel ? !!sel.hadFocus : document.activeElement === content;
    if (content.value === value) {
      // Text unchanged: leave the native caret alone (do not re-apply selection).
      return;
    }
    // Read selection before assigning .value — browsers reset the caret on write.
    var prevStart = content.selectionStart || 0;
    var prevEnd = content.selectionEnd || 0;
    content.value = value;
    if (sel) {
      restoreSelection(sel);
    } else {
      var max = value.length;
      try {
        content.setSelectionRange(Math.min(prevStart, max), Math.min(prevEnd, max));
      } catch (e) {
        // ignore
      }
    }
    if (hadFocus && document.activeElement !== content) {
      content.focus();
    }
  }

  function updateMeta(data) {
    if (data.expiresAt) {
      lastExpiresAt = data.expiresAt;
      renderExpires(data.expiresAt);
    } else {
      lastExpiresAt = null;
      expiresText.textContent = data.exists ? "已保存" : "尚未保存";
    }
  }

  function renderExpires(expiresAt) {
    var d = new Date(expiresAt);
    if (Number.isNaN(d.getTime())) {
      expiresText.textContent = "—";
      return;
    }
    var ms = d.getTime() - Date.now();
    if (ms <= 0) {
      expiresText.textContent = "已过期 · " + d.toLocaleString();
      return;
    }
    expiresText.textContent = "过期 " + d.toLocaleString() + "（剩余 " + formatDuration(ms) + "）";
  }

  function formatDuration(ms) {
    var s = Math.ceil(ms / 1000);
    if (s < 60) return s + " 秒";
    if (s < 3600) return Math.ceil(s / 60) + " 分钟";
    if (s < 86400) {
      var h = s / 3600;
      return (h < 10 ? h.toFixed(1) : Math.round(h)) + " 小时";
    }
    var days = s / 86400;
    return (days < 10 ? days.toFixed(1) : Math.round(days)) + " 天";
  }

  function shortId(id) {
    return String(id || "").slice(0, 4);
  }

  function setIdleStatus() {
    setStatus(connected ? "live" : "error");
  }

  function updatePeers() {
    if (!peerTabs) return;
    peerTabs.innerHTML = "";

    if (connected) {
      peerTabs.appendChild(makePeerTab(CLIENT_ID, colorForPeer(CLIENT_ID), true));
    }

    // Stable order: sort by clientId so tabs don't reshuffle on each presence update.
    Object.keys(remoteCursors).sort().forEach(function (id) {
      peerTabs.appendChild(makePeerTab(id, colorForPeer(id, remoteCursors[id]), false));
    });
  }

  function makePeerTab(id, color, isSelf) {
    var el = document.createElement("span");
    el.className = "peer-tab" + (isSelf ? " is-self" : "");
    el.style.setProperty("--peer-color", color || "#888");
    el.textContent = shortId(id);
    el.title = (isSelf ? "自己 · " : "协作者 · ") + id;
    return el;
  }

  function setConnected(isOn) {
    connected = !!isOn;
    updatePeers();
  }

  function pulseTraffic(dir) {
    var el = dir === "up" ? dotUp : dotDown;
    if (!el) return;
    el.setAttribute("data-active", "true");
    if (dir === "up") {
      window.clearTimeout(trafficUpTimer);
      trafficUpTimer = window.setTimeout(function () {
        el.setAttribute("data-active", "false");
      }, 480);
    } else {
      window.clearTimeout(trafficDownTimer);
      trafficDownTimer = window.setTimeout(function () {
        el.setAttribute("data-active", "false");
      }, 480);
    }
  }

  function readTTLSeconds() {
    var value = Number(ttlValue.value);
    var unit = Number(ttlUnit.value);
    if (!Number.isSafeInteger(value) || value <= 0 || !Number.isSafeInteger(unit) || unit <= 0) {
      return 0;
    }
    var seconds = value * unit;
    return Number.isSafeInteger(seconds) && seconds > 0 ? seconds : 0;
  }

  function setTTLControls(seconds) {
    var units = [86400, 3600, 60];
    var unit = units.find(function (candidate) { return seconds % candidate === 0; }) || 60;
    ttlUnit.value = String(unit);
    ttlValue.value = String(Math.max(1, Math.floor(seconds / unit)));
  }

  // Only two user-facing states: live | error
  function setStatus(kind) {
    if (kind === "live") {
      status.textContent = "live";
      status.setAttribute("data-tone", "ok");
      return;
    }
    status.textContent = "error";
    status.setAttribute("data-tone", "err");
  }

  // --- File paste / drag / password modal ------------------------------------

  var modalState = null; // { resolve, reject, busy }
  var dragDepth = 0;

  function setupPasswordModal() {
    if (!passwordModal) return;
    if (modalCancel) {
      modalCancel.addEventListener("click", function () {
        closePasswordModal(null);
      });
    }
    if (modalConfirm) {
      modalConfirm.addEventListener("click", function () {
        submitPasswordModal();
      });
    }
    if (modalPassword) {
      modalPassword.addEventListener("keydown", function (e) {
        if (e.key === "Enter") {
          e.preventDefault();
          submitPasswordModal();
        } else if (e.key === "Escape") {
          e.preventDefault();
          closePasswordModal(null);
        }
      });
    }
    passwordModal.addEventListener("click", function (e) {
      if (e.target && e.target.getAttribute("data-modal-dismiss") != null) {
        closePasswordModal(null);
      }
    });
    document.addEventListener("keydown", function (e) {
      if (e.key === "Escape" && passwordModal && !passwordModal.hidden) {
        closePasswordModal(null);
      }
    });
  }

  function setupFileDropPaste() {
    // Drag files onto the page / editor.
    document.addEventListener("dragenter", function (e) {
      if (!hasFiles(e)) return;
      e.preventDefault();
      dragDepth++;
      if (appRoot) appRoot.classList.add("is-file-dragover");
    });
    document.addEventListener("dragover", function (e) {
      if (!hasFiles(e)) return;
      e.preventDefault();
      if (e.dataTransfer) e.dataTransfer.dropEffect = "copy";
    });
    document.addEventListener("dragleave", function (e) {
      if (!hasFiles(e) && dragDepth === 0) return;
      e.preventDefault();
      dragDepth = Math.max(0, dragDepth - 1);
      if (dragDepth === 0 && appRoot) {
        appRoot.classList.remove("is-file-dragover");
      }
    });
    document.addEventListener("drop", function (e) {
      dragDepth = 0;
      if (appRoot) appRoot.classList.remove("is-file-dragover");
      if (!hasFiles(e)) return;
      e.preventDefault();
      e.stopPropagation();
      if (e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files.length) {
        promptUploadFiles(e.dataTransfer.files);
      }
    });

    // Paste files (screenshot / Finder copy). Pure file paste always uploads;
    // mixed text+file paste only uploads when not focused on editor text intent.
    document.addEventListener("paste", function (e) {
      var items = e.clipboardData && e.clipboardData.items;
      if (!items || !items.length) return;
      var files = [];
      var hasText = false;
      for (var i = 0; i < items.length; i++) {
        if (items[i].kind === "file") {
          var f = items[i].getAsFile();
          if (f) files.push(f);
        } else if (items[i].kind === "string") {
          hasText = true;
        }
      }
      if (!files.length) return;
      // If user is typing and clipboard also has text, prefer text paste.
      if (document.activeElement === content && hasText) return;
      e.preventDefault();
      promptUploadFiles(files);
    });
  }

  function hasFiles(e) {
    if (!e.dataTransfer) return false;
    if (e.dataTransfer.types) {
      for (var i = 0; i < e.dataTransfer.types.length; i++) {
        if (e.dataTransfer.types[i] === "Files") return true;
      }
    }
    return !!(e.dataTransfer.files && e.dataTransfer.files.length);
  }

  function setFilesStatus(msg, tone) {
    if (!filesStatus) return;
    filesStatus.textContent = msg || "";
    if (tone) filesStatus.setAttribute("data-tone", tone);
    else filesStatus.removeAttribute("data-tone");
  }

  function setFilesPanelVisible(visible) {
    if (!filesPanel) return;
    filesPanel.hidden = !visible;
  }

  function setFileUploadEnabled(enabled) {
    fileUploadEnabled = !!enabled;
    updateRoomTitleChrome();
  }

  function updateRoomTitleChrome() {
    if (!roomTitle) return;
    roomTitle.textContent = "/" + key;
    if (fileUploadEnabled) {
      roomTitle.title = "房间 " + key + " · 文件上传已开启（三击切换）";
      roomTitle.classList.add("upload-on");
    } else {
      roomTitle.title = "房间 " + key + " · 文件上传已关闭（三击管理员开关）";
      roomTitle.classList.remove("upload-on");
    }
  }

  function setupRoomTitleToggle() {
    if (!roomTitle) return;
    roomTitle.addEventListener("click", function () {
      roomTitleClickCount++;
      window.clearTimeout(roomTitleClickTimer);
      roomTitleClickTimer = window.setTimeout(function () {
        roomTitleClickCount = 0;
      }, 550);
      if (roomTitleClickCount < 3) return;
      roomTitleClickCount = 0;
      window.clearTimeout(roomTitleClickTimer);
      toggleRoomFileUpload();
    });
  }

  function toggleRoomFileUpload() {
    var next = !fileUploadEnabled;
    var action = next ? "开启" : "关闭";
    askPassword({
      title: action + "本空间文件上传",
      hint: next
        ? "验证管理员密码后，允许在此空间上传文件"
        : "验证管理员密码后，禁止在此空间上传新文件（已有文件仍可下载/删除）",
      confirmLabel: action,
      passwordKind: "admin",
      preferRemembered: true
    }).then(function (adminPassword) {
      if (!adminPassword) return;
      rememberAdminPassword(adminPassword);
      setFilesStatus("正在" + action + "文件上传…");
      return fetch(settingsAPIURL, {
        method: "PUT",
        headers: {
          Accept: "application/json",
          "Content-Type": "application/json",
          "X-Admin-Password": adminPassword
        },
        body: JSON.stringify({
          fileUploadEnabled: next,
          adminPassword: adminPassword
        })
      })
        .then(function (response) {
          return response.json().then(function (data) {
            return { ok: response.ok, status: response.status, data: data };
          }).catch(function () {
            return { ok: response.ok, status: response.status, data: {} };
          });
        })
        .then(function (result) {
          if (result.status === 401) {
            rememberAdminPassword("");
            throw new Error("管理员密码错误");
          }
          if (result.status === 403) {
            throw new Error(result.data.error || "文件功能未启用");
          }
          if (!result.ok) {
            throw new Error(result.data.error || ("HTTP " + result.status));
          }
          setFileUploadEnabled(!!result.data.fileUploadEnabled);
          setFilesStatus(
            fileUploadEnabled ? "本空间已开启文件上传" : "本空间已关闭文件上传",
            "ok"
          );
          // Make sure status is visible even when the file list is empty.
          if (fileUploadEnabled) setFilesPanelVisible(true);
          loadFiles(true);
        })
        .catch(function (err) {
          setFilesStatus(err.message || "设置失败", "err");
          if (err.message === "管理员密码错误") {
            return askPassword({
              title: action + "本空间文件上传",
              hint: "管理员密码错误，请重试",
              confirmLabel: action,
              passwordKind: "admin",
              preferRemembered: false
            }).then(function (pw) {
              if (!pw) return;
              rememberAdminPassword(pw);
              return fetch(settingsAPIURL, {
                method: "PUT",
                headers: {
                  Accept: "application/json",
                  "Content-Type": "application/json",
                  "X-Admin-Password": pw
                },
                body: JSON.stringify({
                  fileUploadEnabled: next,
                  adminPassword: pw
                })
              }).then(function (response) {
                if (response.status === 401) throw new Error("管理员密码错误");
                if (!response.ok) throw new Error("设置失败");
                return response.json();
              }).then(function (data) {
                setFileUploadEnabled(!!data.fileUploadEnabled);
                setFilesStatus(
                  fileUploadEnabled ? "本空间已开启文件上传" : "本空间已关闭文件上传",
                  "ok"
                );
                if (fileUploadEnabled) setFilesPanelVisible(true);
                loadFiles(true);
              }).catch(function (e2) {
                setFilesStatus(e2.message || "设置失败", "err");
              });
            });
          }
        });
    });
  }

  function loadFiles(silent) {
    if (!fileList) return;
    fetch(filesAPIURL, { headers: { Accept: "application/json" } })
      .then(function (response) {
        if (!response.ok) throw new Error("HTTP " + response.status);
        return response.json();
      })
      .then(function (data) {
        if (typeof data.fileUploadEnabled === "boolean") {
          setFileUploadEnabled(data.fileUploadEnabled);
        }
        renderFileList(data.files || []);
        if (!silent) setFilesStatus("");
      })
      .catch(function () {
        if (!silent) setFilesStatus("文件列表加载失败", "err");
      });
  }

  function renderFileList(files) {
    if (!fileList) return;
    fileList.innerHTML = "";
    var n = files.length;
    if (filesCount) filesCount.textContent = String(n);
    // Hide entire panel when empty — only show after files exist.
    setFilesPanelVisible(n > 0);
    if (n === 0) return;

    files.forEach(function (f) {
      var li = document.createElement("li");
      li.className = "file-item";

      var meta = document.createElement("div");
      meta.className = "file-meta";

      var nameEl = document.createElement("div");
      nameEl.className = "file-name";
      nameEl.textContent = f.name || f.id;
      nameEl.title = f.name || f.id;

      var sub = document.createElement("div");
      sub.className = "file-sub";
      sub.textContent =
        formatFileSize(f.size) +
        (f.expiresAt ? " · 过期 " + formatShortTime(f.expiresAt) : "");

      meta.appendChild(nameEl);
      meta.appendChild(sub);

      var actions = document.createElement("div");
      actions.className = "file-actions";

      var dl = document.createElement("button");
      dl.type = "button";
      dl.textContent = "下载";
      dl.addEventListener("click", function () {
        downloadFile(f.id, f.name);
      });

      var del = document.createElement("button");
      del.type = "button";
      del.className = "danger";
      del.textContent = "删除";
      del.addEventListener("click", function () {
        deleteFile(f.id, f.name);
      });

      actions.appendChild(dl);
      actions.appendChild(del);
      li.appendChild(meta);
      li.appendChild(actions);
      fileList.appendChild(li);
    });
  }

  function formatFileSize(n) {
    n = Number(n) || 0;
    if (n < 1024) return n + " B";
    if (n < 1024 * 1024) return (n / 1024).toFixed(n < 10 * 1024 ? 1 : 0) + " KB";
    return (n / (1024 * 1024)).toFixed(n < 10 * 1024 * 1024 ? 1 : 0) + " MB";
  }

  function formatShortTime(iso) {
    var d = new Date(iso);
    if (Number.isNaN(d.getTime())) return iso;
    return d.toLocaleString();
  }

  function rememberedAdminPassword() {
    try {
      return sessionStorage.getItem(ADMIN_PASSWORD_KEY) || "";
    } catch (e) {
      return "";
    }
  }

  function rememberedFilePassword() {
    try {
      return sessionStorage.getItem(FILE_PASSWORD_KEY) || "";
    } catch (e) {
      return "";
    }
  }

  function rememberAdminPassword(password) {
    try {
      if (password) sessionStorage.setItem(ADMIN_PASSWORD_KEY, password);
      else sessionStorage.removeItem(ADMIN_PASSWORD_KEY);
    } catch (e) {
      // ignore
    }
  }

  function rememberFilePassword(password) {
    try {
      if (password) sessionStorage.setItem(FILE_PASSWORD_KEY, password);
      else sessionStorage.removeItem(FILE_PASSWORD_KEY);
    } catch (e) {
      // ignore
    }
  }

  /**
   * Ask for a password via modal. Resolves with password string, or null if cancelled.
   * options: {
   *   title, hint, fileNames, confirmLabel,
   *   passwordKind: "admin" | "file",
   *   label, placeholder,
   *   preferRemembered
   * }
   */
  function askPassword(options) {
    options = options || {};
    return new Promise(function (resolve) {
      if (!passwordModal) {
        resolve(null);
        return;
      }
      // Close any previous modal waiters.
      if (modalState && typeof modalState.resolve === "function") {
        modalState.resolve(null);
      }
      modalState = { resolve: resolve, busy: false };

      var kind = options.passwordKind === "admin" ? "admin" : "file";
      var defaultTitle = kind === "admin" ? "输入管理员密码" : "输入文件密码";
      var defaultLabel = kind === "admin" ? "管理员密码" : "文件密码";
      var defaultPlaceholder = kind === "admin" ? "管理员密码" : "文件密码（下载用）";

      if (modalTitle) {
        modalTitle.textContent = options.title || defaultTitle;
      }
      if (modalHint) {
        modalHint.textContent = options.hint || "";
      }
      if (modalPasswordLab) {
        modalPasswordLab.textContent = options.label || defaultLabel;
      }
      if (modalFileNames) {
        modalFileNames.innerHTML = "";
        (options.fileNames || []).forEach(function (name) {
          var li = document.createElement("li");
          li.textContent = name;
          li.title = name;
          modalFileNames.appendChild(li);
        });
      }
      if (modalError) modalError.textContent = "";
      if (modalConfirm) {
        modalConfirm.textContent = options.confirmLabel || "确认";
        modalConfirm.disabled = false;
      }
      if (modalCancel) modalCancel.disabled = false;
      if (modalPassword) {
        var remembered =
          kind === "admin" ? rememberedAdminPassword() : rememberedFilePassword();
        modalPassword.value = options.preferRemembered !== false ? remembered : "";
        modalPassword.placeholder = options.placeholder || defaultPlaceholder;
        modalPassword.autocomplete = kind === "admin" ? "current-password" : "off";
        modalPassword.disabled = false;
      }

      passwordModal.hidden = false;
      window.setTimeout(function () {
        if (modalPassword) {
          modalPassword.focus();
          modalPassword.select();
        }
      }, 30);
    });
  }

  function setModalBusy(busy) {
    if (!modalState) return;
    modalState.busy = !!busy;
    if (modalConfirm) modalConfirm.disabled = !!busy;
    if (modalCancel) modalCancel.disabled = !!busy;
    if (modalPassword) modalPassword.disabled = !!busy;
  }

  function setModalError(msg) {
    if (modalError) modalError.textContent = msg || "";
  }

  function submitPasswordModal() {
    if (!modalState || modalState.busy) return;
    var pw = modalPassword ? String(modalPassword.value || "") : "";
    if (!pw) {
      setModalError("请输入密码");
      if (modalPassword) modalPassword.focus();
      return;
    }
    setModalError("");
    var resolve = modalState.resolve;
    modalState = null;
    passwordModal.hidden = true;
    resolve(pw);
  }

  function closePasswordModal(value) {
    if (!modalState) {
      if (passwordModal) passwordModal.hidden = true;
      return;
    }
    if (modalState.busy) return;
    var resolve = modalState.resolve;
    modalState = null;
    if (passwordModal) passwordModal.hidden = true;
    resolve(value);
  }

  function promptUploadFiles(fileListLike) {
    var files = Array.prototype.slice.call(fileListLike || []).filter(Boolean);
    if (!files.length) return;

    var names = files.map(function (f) {
      return f.name || "未命名文件";
    });

    // Room open → only file password.
    // Room closed → admin password first, then file password (one-shot; does not open the room).
    var start;
    if (fileUploadEnabled) {
      start = Promise.resolve(null);
    } else {
      start = askPassword({
        title: "上传文件 · 管理员密码",
        hint: "本空间未开放上传，需管理员密码（共 " + files.length + " 个文件）",
        fileNames: names,
        confirmLabel: "下一步",
        passwordKind: "admin"
      });
    }

    start.then(function (adminPassword) {
      if (!fileUploadEnabled && !adminPassword) return;
      return askPassword({
        title: "上传文件 · 文件密码",
        hint: "设置文件密码；下载时需要此密码（本批共 " + files.length + " 个，共用）",
        fileNames: names,
        confirmLabel: "上传",
        passwordKind: "file"
      }).then(function (filePassword) {
        if (!filePassword) return;
        uploadFilesWithPasswords(files, adminPassword || "", filePassword);
      });
    });
  }

  function uploadFilesWithPasswords(files, adminPassword, filePassword) {
    if (adminPassword) rememberAdminPassword(adminPassword);
    rememberFilePassword(filePassword);
    var ttlSeconds = readTTLSeconds() || 3600;
    // Show panel early so status is visible during upload.
    setFilesPanelVisible(true);
    setFilesStatus("正在上传 " + files.length + " 个文件…");

    var chain = Promise.resolve();
    var ok = 0;
    var fail = 0;
    var lastErr = "";
    var badAdmin = false;

    files.forEach(function (file) {
      chain = chain.then(function () {
        if (badAdmin) {
          fail++;
          return;
        }
        return uploadOneFile(file, adminPassword, filePassword, ttlSeconds)
          .then(function () {
            ok++;
          })
          .catch(function (err) {
            fail++;
            lastErr = err.message || String(err);
            if (lastErr === "管理员密码错误") {
              badAdmin = true;
              rememberAdminPassword("");
            }
          });
      });
    });

    chain.then(function () {
      if (badAdmin) {
        setFilesStatus("");
        loadFiles(true);
        return askPassword({
          title: "上传文件 · 管理员密码",
          hint: "管理员密码错误，请重试",
          fileNames: files.map(function (f) { return f.name || "未命名文件"; }),
          confirmLabel: "下一步",
          passwordKind: "admin",
          preferRemembered: false
        }).then(function (adminPw) {
          if (!adminPw) {
            loadFiles(true);
            return;
          }
          uploadFilesWithPasswords(files, adminPw, filePassword);
        });
      }
      loadFiles(true);
      if (fail === 0) {
        setFilesStatus("已上传 " + ok + " 个文件", "ok");
      } else if (ok > 0) {
        setFilesStatus(
          "成功 " + ok + " · 失败 " + fail + (lastErr ? " · " + lastErr : ""),
          "err"
        );
      } else {
        // Keep panel visible briefly so the error is readable even if list is empty.
        setFilesPanelVisible(true);
        setFilesStatus(lastErr || "上传失败", "err");
      }
    });
  }

  function uploadOneFile(file, adminPassword, filePassword, ttlSeconds) {
    var form = new FormData();
    var name = file.name;
    // Screenshots / clipboard blobs often have empty name.
    if (!name || name === "image.png" || name === "blob") {
      var ext = guessExt(file.type);
      name = "paste-" + Date.now() + ext;
    }
    form.append("file", file, name);
    form.append("filePassword", filePassword);
    form.append("ttlSeconds", String(ttlSeconds));
    if (adminPassword) {
      form.append("adminPassword", adminPassword);
    }

    var headers = { Accept: "application/json" };
    if (adminPassword) {
      headers["X-Admin-Password"] = adminPassword;
    }

    return fetch(filesAPIURL, {
      method: "POST",
      headers: headers,
      body: form
    }).then(function (response) {
      return response.json().then(function (data) {
        return { ok: response.ok, status: response.status, data: data };
      }).catch(function () {
        return { ok: response.ok, status: response.status, data: {} };
      });
    }).then(function (result) {
      if (result.status === 401) throw new Error("管理员密码错误");
      if (result.status === 403) {
        throw new Error(result.data.error || "上传未启用");
      }
      if (result.status === 413) throw new Error("文件过大");
      if (!result.ok) throw new Error(result.data.error || ("HTTP " + result.status));
      return result.data;
    });
  }

  function guessExt(mime) {
    mime = String(mime || "").toLowerCase();
    if (mime === "image/png") return ".png";
    if (mime === "image/jpeg") return ".jpg";
    if (mime === "image/gif") return ".gif";
    if (mime === "image/webp") return ".webp";
    if (mime === "application/pdf") return ".pdf";
    if (mime.indexOf("text/") === 0) return ".txt";
    return ".bin";
  }

  function handleRemoteFiles(msg) {
    // Authoritative metadata snapshot from server (upload/delete/expiry).
    if (msg && typeof msg.fileUploadEnabled === "boolean") {
      setFileUploadEnabled(msg.fileUploadEnabled);
    }
    var files = msg && msg.files ? msg.files : [];
    renderFileList(files);
    if (files.length) {
      setFilesStatus("");
    }
  }

  function downloadFile(id, name) {
    askPassword({
      title: "下载文件",
      hint: "下载需要文件密码（上传时设置）",
      fileNames: name ? [name] : [],
      confirmLabel: "下载",
      passwordKind: "file"
    }).then(function (password) {
      if (!password) return;
      rememberFilePassword(password);
      setFilesStatus("正在下载…");
      return fetch(filesAPIURL + "/" + encodeURIComponent(id), {
        method: "GET",
        headers: { "X-File-Password": password }
      })
        .then(function (response) {
          if (response.status === 401) {
            rememberFilePassword("");
            throw new Error("文件密码错误");
          }
          if (response.status === 403) throw new Error("文件访问未启用");
          if (response.status === 404) throw new Error("文件不存在");
          if (!response.ok) {
            return response.json().then(function (data) {
              throw new Error(data.error || ("HTTP " + response.status));
            }).catch(function (e) {
              if (e && e.message && e.message.indexOf("HTTP") !== 0 && e.name !== "SyntaxError") throw e;
              throw new Error("HTTP " + response.status);
            });
          }
          var filename = name || id;
          var cd = response.headers.get("Content-Disposition") || "";
          var m = /filename\*?=(?:UTF-8''|")?([^";]+)/i.exec(cd);
          if (m && m[1]) {
            try {
              filename = decodeURIComponent(m[1].replace(/"/g, "").trim());
            } catch (e) {
              filename = m[1].replace(/"/g, "").trim() || filename;
            }
          }
          return response.blob().then(function (blob) {
            return { blob: blob, filename: filename };
          });
        })
        .then(function (result) {
          var url = URL.createObjectURL(result.blob);
          var a = document.createElement("a");
          a.href = url;
          a.download = result.filename;
          a.rel = "noopener";
          document.body.appendChild(a);
          a.click();
          a.remove();
          window.setTimeout(function () {
            URL.revokeObjectURL(url);
          }, 2000);
          setFilesStatus("已开始下载", "ok");
        })
        .catch(function (err) {
          setFilesStatus(err.message || "下载失败", "err");
          if (err.message === "文件密码错误") {
            return askPassword({
              title: "下载文件",
              hint: "文件密码错误，请重试",
              fileNames: name ? [name] : [],
              confirmLabel: "下载",
              passwordKind: "file",
              preferRemembered: false
            }).then(function (pw) {
              if (!pw) return;
              rememberFilePassword(pw);
              // Retry once with the new password without nested re-prompt loop.
              return fetch(filesAPIURL + "/" + encodeURIComponent(id), {
                method: "GET",
                headers: { "X-File-Password": pw }
              }).then(function (response) {
                if (!response.ok) {
                  throw new Error(response.status === 401 ? "文件密码错误" : "下载失败");
                }
                return response.blob().then(function (blob) {
                  var url = URL.createObjectURL(blob);
                  var a = document.createElement("a");
                  a.href = url;
                  a.download = name || id;
                  document.body.appendChild(a);
                  a.click();
                  a.remove();
                  window.setTimeout(function () { URL.revokeObjectURL(url); }, 2000);
                  setFilesStatus("已开始下载", "ok");
                });
              }).catch(function (e2) {
                setFilesStatus(e2.message || "下载失败", "err");
              });
            });
          }
        });
    });
  }

  function deleteFile(id, name) {
    // Always prompt for admin password — never silent delete with cached password only.
    askPassword({
      title: "删除文件",
      hint: "删除需要管理员密码",
      fileNames: name ? [name] : [id],
      confirmLabel: "删除",
      passwordKind: "admin"
    }).then(function (password) {
      if (!password) return;
      rememberAdminPassword(password);
      setFilesStatus("正在删除…");
      return fetch(filesAPIURL + "/" + encodeURIComponent(id), {
        method: "DELETE",
        headers: {
          Accept: "application/json",
          "Content-Type": "application/json",
          "X-Admin-Password": password
        },
        body: JSON.stringify({ adminPassword: password })
      })
        .then(function (response) {
          if (response.status === 401) {
            rememberAdminPassword("");
            throw new Error("管理员密码错误");
          }
          if (response.status === 404) throw new Error("文件不存在");
          if (!response.ok && response.status !== 204) {
            return response.json().then(function (data) {
              throw new Error(data.error || ("HTTP " + response.status));
            }).catch(function (e) {
              if (e.message) throw e;
              throw new Error("HTTP " + response.status);
            });
          }
          // Local refresh; remote peers get WS type=files from the server.
          loadFiles(true);
          setFilesStatus("已删除", "ok");
        })
        .catch(function (err) {
          setFilesStatus(err.message || "删除失败", "err");
          if (err.message === "管理员密码错误") {
            return askPassword({
              title: "删除文件",
              hint: "管理员密码错误，请重试",
              fileNames: name ? [name] : [id],
              confirmLabel: "删除",
              passwordKind: "admin",
              preferRemembered: false
            }).then(function (pw) {
              if (!pw) return;
              rememberAdminPassword(pw);
              return fetch(filesAPIURL + "/" + encodeURIComponent(id), {
                method: "DELETE",
                headers: {
                  Accept: "application/json",
                  "Content-Type": "application/json",
                  "X-Admin-Password": pw
                },
                body: JSON.stringify({ adminPassword: pw })
              }).then(function (response) {
                if (response.status === 401) throw new Error("管理员密码错误");
                if (!response.ok && response.status !== 204) throw new Error("删除失败");
                loadFiles(true);
                setFilesStatus("已删除", "ok");
              }).catch(function (e2) {
                setFilesStatus(e2.message || "删除失败", "err");
              });
            });
          }
        });
    });
  }
})();
