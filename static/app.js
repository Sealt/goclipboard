(function () {
  var key = decodeURIComponent(window.location.pathname.slice(1));
  var apiURL = "/api/clipboard/" + encodeURIComponent(key);
  var filesAPIURL = apiURL + "/files";
  var settingsAPIURL = apiURL + "/settings";
  // ?view=true opens the room in read-only mode (server-enforced). Legacy
  // view links with a view key still work; the client treats any ?view= as
  // read-only and passes the raw value through to the WebSocket.
  var urlParams = new URLSearchParams(window.location.search || "");
  var VIEW_KEY_PARAM = urlParams.get("view") || "";
  var READ_ONLY = !!VIEW_KEY_PARAM;
  // ?mode=md | ?mode=plain — share links can default the view on open.
  var MODE_PARAM = (urlParams.get("mode") || "").toLowerCase();
  var wsURL =
    (location.protocol === "https:" ? "wss://" : "ws://") +
    location.host +
    "/api/clipboard/" +
    encodeURIComponent(key) +
    "/ws" +
    (READ_ONLY ? "?view=" + encodeURIComponent(VIEW_KEY_PARAM) : "");
  var ADMIN_PASSWORD_KEY = "goclipboard:adminPassword";
  var FILE_PASSWORD_KEY = "goclipboard:filePassword";
  var THEME_KEY = "goclipboard:theme";
  var LANG_KEY = "goclipboard:lang";

  // --- i18n ----------------------------------------------------------------
  // Keyed by the Chinese source string; English overrides, Chinese falls through.
  var EN = {
    "已保存": "Saved",
    "尚未保存": "Not saved yet",
    "已过期 · ": "Expired · ",
    "过期 ": "Expires ",
    "（剩余 ": " (",
    "）": ")",
    "秒": "s",
    " 分钟": " min",
    " 小时": " h",
    " 天": " d",
    "自己 · ": "me · ",
    "协作者 · ": "peer · ",
    "房间 ": "room ",
    " · 文件上传已开启（三击切换）": " · file upload ON (triple-click to toggle)",
    " · 文件上传已关闭（三击管理员开关）": " · file upload OFF (triple-click, admin)",
    "开启": "Enable",
    "关闭": "Disable",
    "开启本空间文件上传": "Enable file uploads in this room",
    "关闭本空间文件上传": "Disable file uploads in this room",
    "验证管理员密码后，允许在此空间上传文件": "Verify the admin password to allow file uploads in this room",
    "验证管理员密码后，禁止在此空间上传新文件（已有文件仍可下载/删除）": "Verify the admin password to stop new uploads (existing files stay downloadable)",
    "正在开启文件上传…": "Enabling file upload…",
    "正在关闭文件上传…": "Disabling file upload…",
    "管理员密码错误": "Wrong admin password",
    "管理员密码错误，请重试": "Wrong admin password, please retry",
    "文件功能未启用": "File features not enabled",
    "本空间已开启文件上传": "File uploads enabled",
    "本空间已关闭文件上传": "File uploads disabled",
    "设置失败": "Update failed",
    "文件列表加载失败": "Failed to load file list",
    "下载": "Download",
    "删除": "Delete",
    " · 过期 ": " · expires ",
    "输入管理员密码": "Admin password required",
    "输入文件密码": "File password required",
    "管理员密码": "Admin password",
    "文件密码": "File password",
    "文件密码（下载用）": "File password (for downloads)",
    "确认": "Confirm",
    "取消": "Cancel",
    "请输入密码": "Enter a password",
    "未命名文件": "Unnamed file",
    "上传文件 · 管理员密码": "Upload files · admin password",
    "本空间未开放上传，需管理员密码（共 ": "Uploads are closed in this room — admin password required (",
    " 个文件）": " file(s))",
    "下一步": "Next",
    "上传文件 · 文件密码": "Upload files · file password",
    "设置文件密码；下载时需要此密码（本批共 ": "Set a file password; it is required to download (",
    " 个，共用）": " file(s), shared)",
    "上传": "Upload",
    "正在上传 ": "Uploading ",
    " 个文件…": " file(s)…",
    "已上传 ": "Uploaded ",
    " 个文件": " file(s)",
    "成功 ": "OK ",
    " · 失败 ": " · failed ",
    "上传失败": "Upload failed",
    "上传未启用": "Uploads not enabled",
    "文件过大": "File too large",
    "下载文件": "Download file",
    "下载需要文件密码（上传时设置）": "Enter the file password set at upload time",
    "正在下载…": "Downloading…",
    "文件密码错误": "Wrong file password",
    "文件密码错误，请重试": "Wrong file password, please retry",
    "文件访问未启用": "File access not enabled",
    "文件不存在": "File not found",
    "已开始下载": "Download started",
    "下载失败": "Download failed",
    "删除文件": "Delete file",
    "删除需要管理员密码": "Admin password required to delete",
    "正在删除…": "Deleting…",
    "已删除": "Deleted",
    "删除失败": "Delete failed",
    "离线": "offline",
    "只读模式": "Read-only",
    "撤销": "Undo",
    "重做": "Redo",
    "Markdown 预览": "Markdown preview",
    "打开方式": "Open as",
    "纯文本": "Plain text",
    "编辑密码": "Edit password",
    "房间密码": "Room password",
    "设置房间密码": "Set room password",
    "修改房间密码": "Change room password",
    "输入房间密码": "Enter room password",
    "仅编辑需要密码": "Edit — password only for editing",
    "查看和编辑都需要密码": "View — password for viewing and editing",
    "此房间已加密，需要密码才能查看": "This room is password-protected — enter the password to view",
    "房间密码错误，请重试": "Wrong room password, please retry",
    "解除锁定失败": "Failed to unlock",
    "密码可手动输入或重新生成，范围决定验证的是查看还是编辑": "Type a password (or regenerate) and pick its scope: view or edit",
    "已锁定：查看和编辑都需要输入密码": "Locked: viewing and editing both require the password",
    "设置": "Set",
    "修改": "Change",
    "查看": "View",
    "未设置": "Not set",
    "已设置": "Set",
    "生成": "Generate",
    "重新生成": "Regenerate",
    "解除锁定": "Unlock",
    "输入编辑密码": "Enter edit password",
    "此房间已锁定，编辑需要密码": "This room is locked; editing requires the password",
    "未锁定：任何拿到链接的人都能查看和编辑。建议先设置密码再分享": "Open: anyone with the link can view and edit. Set a password before sharing.",
    "已锁定：编辑需要输入密码": "Locked: editing requires the password.",
    "版本历史": "Version history",
    "分享链接": "Share links",
    "切换主题": "Toggle theme",
    "分享链接 · 手机扫码或复制链接": "Share — scan the QR code or copy a link",
    "房间链接": "Room link",
    "二维码为房间链接，扫码即可打开": "The QR code encodes the room link — scan to open",
    "复制": "Copy",
    "已复制": "Copied",
    "关闭": "Close",
    "存档当前版本": "Save snapshot",
    "恢复": "Restore",
    "暂无历史版本，编辑后自动记录": "No snapshots yet — they are captured automatically as you edit",
    "已恢复到该版本": "Restored",
    "恢复失败": "Restore failed",
    "（当前）": " (current)",
    "预览": "Preview",
    "编辑": "Edit",
    "自动": "Auto",
    "深色": "Dark",
    "浅色": "Light",
    "主题：": "Theme: ",
    "语言": "Language",
    "TTL 数值": "TTL value",
    "TTL 单位": "TTL unit",
    "分钟": "min",
    "小时": "h",
    "天": "d",
    "粘贴或输入文本，内容会自动保存 · 开启上传后可粘贴/拖入文件": "Paste or type — content saves automatically · paste/drag files once uploads are on",
    "文件": "Files",
    "在线用户": "Peers",
    "输入密码": "Enter password",
    "密码": "Password",
    "发送": "Send",
    "接收": "Receive"
  };

  function t(s) {
    if (LANG === "en" && EN[s] != null) return EN[s];
    return s;
  }

  // t with {0}/{1} positional args.
  function tf(s) {
    var out = t(s);
    for (var i = 1; i < arguments.length; i++) {
      out = out.split("{" + (i - 1) + "}").join(String(arguments[i]));
    }
    return out;
  }

  var LANG =
    (function () {
      try {
        var saved = localStorage.getItem(LANG_KEY);
        if (saved === "en" || saved === "zh") return saved;
      } catch (e) { /* ignore */ }
      var nav = (navigator.language || "zh").toLowerCase();
      return nav.indexOf("zh") === 0 ? "zh" : "en";
    })();

  function applyLang() {
    document.documentElement.lang = LANG === "en" ? "en" : "zh-CN";
    var langBtn = document.getElementById("langBtn");
    if (langBtn) langBtn.textContent = LANG === "en" ? "中" : "EN";
    var nodes = document.querySelectorAll("[data-i18n]");
    for (var i = 0; i < nodes.length; i++) {
      var el = nodes[i];
      el.textContent = t(el.getAttribute("data-i18n"));
    }
    var ph = document.querySelectorAll("[data-i18n-placeholder]");
    for (var j = 0; j < ph.length; j++) {
      ph[j].placeholder = t(ph[j].getAttribute("data-i18n-placeholder"));
    }
    if (readOnlyMode) {
      // The edit hint ("paste or type…") is misleading on a read-only link.
      var ro = document.querySelectorAll("[data-i18n-placeholder]");
      for (var jj = 0; jj < ro.length; jj++) ro[jj].removeAttribute("placeholder");
    }
    var ar = document.querySelectorAll("[data-i18n-aria]");
    for (var k = 0; k < ar.length; k++) {
      ar[k].setAttribute("aria-label", t(ar[k].getAttribute("data-i18n-aria")));
    }
    // Re-render dynamic texts.
    if (typeof renderExpires === "function" && lastExpiresAt) renderExpires(lastExpiresAt);
    if (typeof updateRoomTitleChrome === "function") updateRoomTitleChrome();
    if (typeof updatePeers === "function") updatePeers();
    if (typeof renderHistory === "function") renderHistory();
    if (typeof setFilesStatus === "function") setFilesStatus("");
  }

  // --- Theme ---------------------------------------------------------------
  // SVG icons (Feather-style, stroke inherits currentColor).
  var THEME_ICONS = {
    dark: '<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"></path></svg>',
    light: '<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><circle cx="12" cy="12" r="5"></circle><line x1="12" y1="1" x2="12" y2="3"></line><line x1="12" y1="21" x2="12" y2="23"></line><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"></line><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"></line><line x1="1" y1="12" x2="3" y2="12"></line><line x1="21" y1="12" x2="23" y2="12"></line><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"></line><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"></line></svg>',
    auto: '<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><rect x="2" y="3" width="20" height="14" rx="2" ry="2"></rect><line x1="8" y1="21" x2="16" y2="21"></line><line x1="12" y1="17" x2="12" y2="21"></line></svg>'
  };

  function currentThemePreference() {
    try {
      return localStorage.getItem(THEME_KEY) || "";
    } catch (e) {
      return "";
    }
  }

  function applyTheme() {
    var pref = currentThemePreference();
    var dark = pref === "dark" || (pref !== "light" && window.matchMedia("(prefers-color-scheme: dark)").matches);
    document.documentElement.dataset.theme = pref === "light" ? "light" : (pref === "dark" ? "dark" : "");
    var hljsLink = document.getElementById("hljsTheme");
    if (hljsLink) {
      hljsLink.href = dark
        ? "/static/vendor/highlight.github-dark.css"
        : "/static/vendor/highlight.github.css";
    }
    var btn = document.getElementById("themeBtn");
    if (btn) {
      btn.innerHTML = THEME_ICONS[pref === "dark" ? "dark" : pref === "light" ? "light" : "auto"];
      btn.title = tf("主题：{0}", t(pref === "dark" ? "深色" : pref === "light" ? "浅色" : "自动"));
    }
  }

  function cycleTheme() {
    var pref = currentThemePreference();
    var next = pref === "dark" ? "light" : pref === "light" ? "" : "dark";
    try {
      localStorage.setItem(THEME_KEY, next);
    } catch (e) { /* ignore */ }
    applyTheme();
  }

  // Theme follow system changes while in auto mode.
  if (window.matchMedia) {
    window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", applyTheme);
  }

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

  // The same tab keeps the same client id across reloads/reconnects, so the
  // peer list shows a stable name + cursor color; sessionStorage is per-tab,
  // so different tabs stay independent peers. Reuse is safe: op ids are
  // site:clock and the Lamport clock is always bumped past the doc's max
  // clock when the server state is adopted, so ids never collide.
  var CLIENT_ID_KEY = "goclipboard:clientId";

  function loadOrCreateClientId() {
    var id = "";
    try {
      id = sessionStorage.getItem(CLIENT_ID_KEY) || "";
    } catch (e) {
      // storage unavailable — fall through to a fresh id
    }
    if (/^[0-9a-f]{16}$/.test(id)) return id;
    id = generateClientId();
    try {
      sessionStorage.setItem(CLIENT_ID_KEY, id);
    } catch (e) {
      // ignore
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

  var CLIENT_ID = loadOrCreateClientId();
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
  var modalPasswordGen = document.getElementById("modalPasswordGen");
  var modalScopeWrap = document.getElementById("modalScopeWrap");
  var modalError = document.getElementById("modalError");
  var modalCancel = document.getElementById("modalCancel");
  var modalConfirm = document.getElementById("modalConfirm");
  // New feature controls
  var undoBtn = document.getElementById("undoBtn");
  var redoBtn = document.getElementById("redoBtn");
  var previewBtn = document.getElementById("previewBtn");
  var historyBtn = document.getElementById("historyBtn");
  var shareBtn = document.getElementById("shareBtn");
  var themeBtn = document.getElementById("themeBtn");
  var langBtn = document.getElementById("langBtn");
  var previewPane = document.getElementById("previewPane");
  var previewBody = document.getElementById("previewBody");
  var readonlyBanner = document.getElementById("readonlyBanner");
  var shareModal = document.getElementById("shareModal");
  var shareQrWrap = document.getElementById("shareQrWrap");
  var shareQrNote = document.getElementById("shareQrNote");
  var shareRoomUrl = document.getElementById("shareRoomUrl");
  var shareRoomCopy = document.getElementById("shareRoomCopy");
  var sharePassValue = document.getElementById("sharePassValue");
  var sharePassCopy = document.getElementById("sharePassCopy");
  var sharePassReset = document.getElementById("sharePassReset");
  var sharePassUnlock = document.getElementById("sharePassUnlock");
  var shareHint = document.getElementById("shareHint");
  var shareClose = document.getElementById("shareClose");
  var historyModal = document.getElementById("historyModal");
  var historyList = document.getElementById("historyList");
  var historyCapture = document.getElementById("historyCapture");
  var historyClose = document.getElementById("historyClose");

  if (!roomTitle || !content || !ttlValue || !ttlUnit || !status) {
    console.error("GoClipboard: required DOM nodes missing (stale HTML/JS cache?)");
    return;
  }

  // CRDT document state
  var doc = new CRDT.Doc();
  var localClock = 0;
  var knownVersion = 0;
  var knownGeneration = 0;
  var knownExists = false;
  var pendingOps = []; // local ops not yet acked by the server
  var sentBatches = {}; // seq -> { ids: {opId:true}, at: ms } awaiting ack
  var nextSeq = 1;
  var ackTimeoutMs = 5000;
  var lastSyncedText = ""; // server text at knownVersion (3-way merge base)
  var flushTimer = 0;
  var flushDelayMs = 60;
  var maxOpsPerSend = 1000; // server caps 4096 ops / 256KiB per WS message
  var putFallbackTimer = 0;
  var putFailures = 0;
  var putInFlight = false;
  var applyingRemote = false;
  // IME composition is a provisional textarea value, not a sequence of
  // committed CRDT edits.  Remote .value assignments during composition can
  // make the browser submit the composition twice (most visible with Chinese
  // input), so hold content messages until compositionend.
  var compositionActive = false;
  var compositionCommitPending = false;
  var compositionInputPending = false;
  var compositionBaseText = "";
  var compositionBaseDoc = null;
  var deferredContentMessages = [];
  var lastExpiresAt = null;
  var syncRequestedAt = 0;

  var cursorTimer = 0;
  var cursorDelayMs = 80;
  var presenceHeartbeatMs = 5000;
  var presenceHeartbeatTimer = 0;
  var presencePruneMs = 5000;
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
  // Read-only (view link) mode
  var viewKey = ""; // server-generated read-only key for this room
  var readOnlyMode = READ_ONLY;
  // Undo / redo stacks: entries are { ops, at } where del ops carry
  // { after, ch } captured from the doc so redo can revive via fresh ids.
  var undoStack = [];
  var redoStack = [];
  var undoCoalesceMs = 800;
  var maxUndoDepth = 100;
  // Local version history: { text, version, at } snapshots (per browser).
  var historySnapshots = [];
  var historyMax = 20;
  var historyThrottleMs = 5000;
  var lastHistoryAt = 0;
  // Markdown preview
  var previewVisible = false;
  var previewTimer = 0;

  roomTitle.textContent = "/" + key;
  updateRoomTitleChrome();
  applyTheme();
  applyLang();
  // Share links can default the view: ?mode=md opens in markdown preview,
  // ?mode=plain (or absent) opens as plain text.
  if (MODE_PARAM === "md" || MODE_PARAM === "markdown") {
    togglePreview();
  }
  if (readOnlyMode) {
    content.readOnly = true;
    if (readonlyBanner) readonlyBanner.hidden = false;
    var ttlField = document.querySelector(".ttl-field");
    if (ttlField) ttlField.hidden = true;
    if (undoBtn) undoBtn.hidden = true;
    if (redoBtn) redoBtn.hidden = true;
    if (historyBtn) historyBtn.hidden = true;
    if (shareBtn) shareBtn.hidden = true;
  }
  content.addEventListener("input", onInput);
  content.addEventListener("compositionstart", onCompositionStart);
  content.addEventListener("compositionend", onCompositionEnd);
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
  setupNewControls();

  // Keyboard shortcuts: undo / redo (intercepted so they map to CRDT ops and
  // survive remote edits, unlike the browser's native textarea undo).
  document.addEventListener("keydown", function (e) {
    var mod = e.metaKey || e.ctrlKey;
    if (!mod) return;
    var k = (e.key || "").toLowerCase();
    if (k === "z" && !e.shiftKey) {
      e.preventDefault();
      undo();
    } else if (k === "z" && e.shiftKey) {
      e.preventDefault();
      redo();
    } else if (k === "y") {
      e.preventDefault();
      redo();
    }
  });

  // --- New feature controls (undo/redo, preview, history, share, theme, lang) ---

  function setupNewControls() {
    if (undoBtn) undoBtn.addEventListener("click", undo);
    if (redoBtn) redoBtn.addEventListener("click", redo);
    if (previewBtn) previewBtn.addEventListener("click", togglePreview);
    if (historyBtn) historyBtn.addEventListener("click", openHistory);
    if (shareBtn) shareBtn.addEventListener("click", openShare);
    if (themeBtn) themeBtn.addEventListener("click", cycleTheme);
    if (langBtn) langBtn.addEventListener("click", toggleLang);
    if (shareClose) shareClose.addEventListener("click", closeShare);
    if (historyClose) historyClose.addEventListener("click", closeHistory);
    if (historyCapture) historyCapture.addEventListener("click", function () {
      captureHistory(true);
    });
    if (shareRoomCopy) {
      shareRoomCopy.addEventListener("click", function () {
        copyText(shareRoomUrl ? shareRoomUrl.textContent : "", shareRoomCopy);
      });
    }
    var shareModeSel = document.getElementById("shareMode");
    if (shareModeSel) {
      shareModeSel.addEventListener("change", function () {
        shareMode = shareModeSel.value || "plain";
        renderShareUrls();
      });
    }
    if (sharePassCopy) {
      sharePassCopy.addEventListener("click", function () {
        if (!editPasswordSet) {
          setRoomPassword();
          return;
        }
        var p = getEditPassword();
        if (p) {
          copyText(p, sharePassCopy);
          return;
        }
        askEditPassword().then(function (pw) {
          if (!pw) return;
          setEditPassword(pw);
          renderShareUrls();
          copyText(pw, sharePassCopy);
        });
      });
    }
    if (sharePassReset) {
      sharePassReset.addEventListener("click", function () {
        if (editPasswordSet) changeRoomPassword();
      });
    }
    if (sharePassUnlock) {
      sharePassUnlock.addEventListener("click", function () {
        if (editPasswordSet) unlockRoom();
      });
    }
    [shareModal, historyModal].forEach(function (m) {
      if (!m) return;
      m.addEventListener("click", function (e) {
        if (e.target && e.target.getAttribute && e.target.getAttribute("data-modal-dismiss") != null) {
          m.hidden = true;
        }
      });
    });
    document.addEventListener("keydown", function (e) {
      if (e.key !== "Escape") return;
      if (shareModal && !shareModal.hidden) shareModal.hidden = true;
      if (historyModal && !historyModal.hidden) historyModal.hidden = true;
    });
  }

  function toggleLang() {
    LANG = LANG === "en" ? "zh" : "en";
    try {
      localStorage.setItem(LANG_KEY, LANG);
    } catch (e) { /* ignore */ }
    applyLang();
  }

  // --- Undo / redo (CRDT-aware: inverse ops, tombstone-safe) ------------------

  // Push a local op batch onto the undo stack; consecutive bursts within the
  // coalesce window merge into one entry so one Ctrl+Z undoes a typing burst.
  function pushUndoEntry(ops) {
    if (!ops || !ops.length) return;
    var last = undoStack[undoStack.length - 1];
    if (last && Date.now() - last.at <= undoCoalesceMs) {
      last.ops = last.ops.concat(ops);
      last.at = Date.now();
    } else {
      undoStack.push({ ops: ops, at: Date.now() });
      if (undoStack.length > maxUndoDepth) undoStack.shift();
    }
    redoStack = [];
    updateUndoButtons();
  }

  // Build the inverse of a batch (applied in reverse). Inserts become
  // tombstones; deletes are revived as *fresh* inserts (re-inserting the
  // original id is a no-op once tombstoned — the fresh id with a higher
  // Lamport clock lands exactly where the deleted character was for
  // chain-typed text).
  function inverseOps(ops) {
    var inverse = [];
    for (var i = ops.length - 1; i >= 0; i--) {
      var op = ops[i];
      if (op.op === "ins") {
        inverse.push({ op: "del", id: op.id });
      } else if (op.op === "del") {
        var item = doc.items ? doc.items[op.id] : null;
        if (!item) continue; // id no longer present (doc rebuilt) — drop
        localClock++;
        inverse.push({
          op: "ins",
          id: CRDT.formatID(CLIENT_ID, localClock),
          after: item.after || "",
          ch: item.ch
        });
      }
    }
    return inverse;
  }

  // Apply an op batch straight to the local doc and enqueue it for sync
  // (used by undo/redo; no text re-diffing).
  function applyOpsLocal(ops) {
    if (!ops || !ops.length) return true;
    bumpClockFromDoc();
    var r = doc.applyBatch(ops);
    if (!r.ok) {
      setStatus("error");
      return false;
    }
    bumpClockFromOps(ops);
    pendingOps = pendingOps.concat(ops);
    scheduleFlush();
    return true;
  }

  function undo() {
    if (readOnlyMode || !undoStack.length) return;
    var entry = undoStack.pop();
    var ops = inverseOps(entry.ops);
    if (!ops.length) {
      updateUndoButtons();
      return;
    }
    var oldText = doc.materialize();
    if (!applyOpsLocal(ops)) {
      undoStack.push(entry);
      return;
    }
    redoStack.push({ ops: ops, at: Date.now() });
    var next = doc.materialize();
    applyingRemote = true;
    setContentValue(next, captureSelection());
    applyingRemote = false;
    onLocalTextChanged(oldText, next);
    renderCursors();
    scheduleCursorSend();
    updateUndoButtons();
  }

  function redo() {
    if (readOnlyMode || !redoStack.length) return;
    var entry = redoStack.pop();
    var ops = inverseOps(entry.ops); // redo = inverse of the undo batch
    if (!ops.length) {
      updateUndoButtons();
      return;
    }
    var oldText = doc.materialize();
    if (!applyOpsLocal(ops)) {
      redoStack.push(entry);
      return;
    }
    undoStack.push({ ops: ops, at: Date.now() });
    var next = doc.materialize();
    applyingRemote = true;
    setContentValue(next, captureSelection());
    applyingRemote = false;
    onLocalTextChanged(oldText, next);
    renderCursors();
    scheduleCursorSend();
    updateUndoButtons();
  }

  function updateUndoButtons() {
    if (undoBtn) undoBtn.disabled = readOnlyMode || undoStack.length === 0;
    if (redoBtn) redoBtn.disabled = readOnlyMode || redoStack.length === 0;
  }

  function clearUndoHistory() {
    undoStack = [];
    redoStack = [];
    updateUndoButtons();
  }

  // --- Version history (local snapshots, restore via CRDT diff) --------------

  function captureHistory(manual) {
    var now = Date.now();
    if (!manual && now - lastHistoryAt < historyThrottleMs) return;
    var text = doc.materialize();
    var last = historySnapshots[historySnapshots.length - 1];
    if (last && last.text === text) return;
    historySnapshots.push({ text: text, version: knownVersion, at: now });
    if (historySnapshots.length > historyMax) historySnapshots.shift();
    lastHistoryAt = now;
    if (historyModal && !historyModal.hidden) renderHistory();
  }

  function openHistory() {
    if (!historyModal) return;
    renderHistory();
    historyModal.hidden = false;
  }

  function closeHistory() {
    if (historyModal) historyModal.hidden = true;
  }

  function renderHistory() {
    if (!historyList) return;
    historyList.innerHTML = "";
    if (!historySnapshots.length) {
      var empty = document.createElement("li");
      empty.className = "history-empty";
      empty.textContent = t("暂无历史版本，编辑后自动记录");
      historyList.appendChild(empty);
      return;
    }
    var current = doc.materialize();
    for (var i = historySnapshots.length - 1; i >= 0; i--) {
      (function (snap, idx) {
        var li = document.createElement("li");
        li.className = "history-item";
        var meta = document.createElement("div");
        meta.className = "history-meta";
        var when = document.createElement("div");
        when.className = "history-when";
        var d = new Date(snap.at);
        when.textContent =
          d.toLocaleString() +
          (snap.version ? " · v" + snap.version : "") +
          (snap.text === current ? " · " + t("（当前）") : "");
        var prev = document.createElement("div");
        prev.className = "history-preview";
        prev.textContent = snap.text.slice(0, 80) + (snap.text.length > 80 ? "…" : "");
        prev.title = snap.text;
        meta.appendChild(when);
        meta.appendChild(prev);
        var restore = document.createElement("button");
        restore.type = "button";
        restore.textContent = t("恢复");
        restore.disabled = snap.text === current;
        restore.addEventListener("click", function () {
          restoreSnapshot(idx);
        });
        li.appendChild(meta);
        li.appendChild(restore);
        historyList.appendChild(li);
      })(historySnapshots[i], i);
    }
  }

  // Restore diffs the current doc against the snapshot text and applies the
  // delta as ordinary CRDT ops — a merge, never a clobber.
  function restoreSnapshot(idx) {
    var snap = historySnapshots[idx];
    if (!snap) return;
    var oldText = doc.materialize();
    if (snap.text === oldText) return;
    applyLocalText(snap.text);
    var newText = doc.materialize();
    if (newText === oldText) {
      setFilesStatus(t("恢复失败"), "err");
      showToast(t("恢复失败"));
      return;
    }
    onLocalTextChanged(oldText, newText);
    renderCursors();
    scheduleCursorSend();
    setFilesStatus(t("已恢复到该版本"), "ok");
    closeHistory();
  }

  // --- Share links + QR -------------------------------------------------------

  // --- Edit password (room lock) ---------------------------------------------
  // When a room is locked, every write must present this password. It lives
  // only in the locking browser's localStorage — the server never sends it
  // back — so view-link holders who strip ?view=true still cannot edit.
  // PasswordScope ("" | "edit" | "view") decides what the password gates:
  // "edit" = writes only (legacy), "view" = reads and writes.
  var EDIT_PASS_STORE_KEY = "goclipboard:editPass:" + key;
  var VIEW_PASS_STORE_KEY = "goclipboard:viewPass:" + key;
  var editPasswordSet = false;
  var passwordScope = "";
  var viewPassPromptInFlight = false;

  function getEditPassword() {
    try {
      return localStorage.getItem(EDIT_PASS_STORE_KEY) || "";
    } catch (e) {
      return "";
    }
  }

  function setEditPassword(p) {
    try {
      if (p) localStorage.setItem(EDIT_PASS_STORE_KEY, p);
      else localStorage.removeItem(EDIT_PASS_STORE_KEY);
    } catch (e) { /* ignore */ }
  }

  function generateEditPassword() {
    var chars = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789";
    var out = "";
    var arr = new Uint32Array(12);
    if (window.crypto && crypto.getRandomValues) {
      crypto.getRandomValues(arr);
      for (var i = 0; i < 12; i++) out += chars[arr[i] % chars.length];
    } else {
      for (var i = 0; i < 12; i++) out += chars[Math.floor(Math.random() * chars.length)];
    }
    return out;
  }

  function putEditPassword(next, current, scope) {
    return fetch(apiURL + "/password", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ password: next, currentPassword: current || "", scope: scope || "" })
    });
  }

  // Set / rotate / clear the edit password. On 403 (wrong current password)
  // prompts once and retries. Resolves true on success. scope ("edit" |
  // "view") decides what the password gates; empty keeps the current scope.
  function saveEditPassword(next, current, scope) {
    var attempt = function (cur) {
      return putEditPassword(next, cur, scope).then(function (res) {
        if (res.status === 403) {
          // Wrong (or missing) current password — surface it, then prompt.
          showToast(t("房间密码错误，请重试"));
          return askEditPassword().then(function (pw) {
            if (!pw) return false;
            return attempt(pw);
          });
        }
        return res.ok;
      });
    };
    return attempt(current || getEditPassword()).catch(function () { return false; });
  }

  // Promise<string|null>: prompt for the room password (null = cancelled).
  function askEditPassword() {
    var viewScope = passwordScope === "view";
    return askPassword({
      passwordKind: "edit",
      title: t(viewScope ? "输入房间密码" : "输入编辑密码"),
      label: t(viewScope ? "房间密码" : "编辑密码"),
      hint: t(viewScope ? "此房间已加密，需要密码才能查看" : "此房间已锁定，编辑需要密码")
    });
  }

  // --- View password (room unlock) -------------------------------------------
  // View-protected rooms withhold content until the password is presented.
  // The password is remembered in sessionStorage (per tab) so the room owner
  // (whose localStorage holds the same password) and repeat visitors do not
  // re-enter it on every reload.

  function rememberedViewPassword() {
    try {
      return sessionStorage.getItem(VIEW_PASS_STORE_KEY) || "";
    } catch (e) {
      return "";
    }
  }

  function rememberViewPassword(p) {
    try {
      if (p) sessionStorage.setItem(VIEW_PASS_STORE_KEY, p);
      else sessionStorage.removeItem(VIEW_PASS_STORE_KEY);
    } catch (e) { /* ignore */ }
  }

  function sendAuth(pw) {
    if (!socket || socket.readyState !== WebSocket.OPEN) return;
    try {
      socket.send(JSON.stringify({ type: "auth", password: pw }));
    } catch (e) { /* ignore */ }
  }

  // Prompt for the room password (unless remembered) and authenticate the
  // WebSocket session; the server then pushes the real room state.
  function requestViewPassword() {
    if (viewPassPromptInFlight) return;
    var remembered = rememberedViewPassword() || (editPasswordSet ? getEditPassword() : "");
    if (remembered) {
      sendAuth(remembered);
      return;
    }
    viewPassPromptInFlight = true;
    askPassword({
      passwordKind: "view",
      title: t("输入房间密码"),
      label: t("房间密码"),
      hint: t("此房间已加密，需要密码才能查看"),
      preferRemembered: false
    }).then(function (pw) {
      viewPassPromptInFlight = false;
      if (!pw) {
        setStatus("error");
        return;
      }
      rememberViewPassword(pw);
      sendAuth(pw);
    });
  }

  // The server rejected our password: forget it and ask again. The stored
  // edit password (localStorage) is the same value, so a stale copy must be
  // dropped too — otherwise the remembered value would auto-resend forever
  // and the prompt would never show.
  function onInvalidViewPassword() {
    rememberViewPassword("");
    setEditPassword("");
    showToast(t("房间密码错误，请重试"));
    viewPassPromptInFlight = false;
    requestViewPassword();
  }

  // --- Set / change the room password (manual entry + scope) -----------------

  // Scope picked in the dialog; updated by the scope radios via scopeChange.
  var roomPassModalScope = "edit";

  // Open the set-password dialog: prefilled with a fresh generated password
  // the user can keep, edit, or regenerate, plus the scope selector.
  function openRoomPasswordDialog(titleKey, hintKey) {
    roomPassModalScope = passwordScope || "edit";
    return askPassword({
      passwordKind: "edit",
      title: t(titleKey),
      label: t("房间密码"),
      hint: t(hintKey),
      confirmLabel: t("确认"),
      initialPassword: generateEditPassword(),
      generate: true,
      scope: roomPassModalScope,
      scopeChange: function (v) { roomPassModalScope = v; },
      preferRemembered: false
    }).then(function (pw) {
      if (!pw) return false;
      return saveEditPassword(pw, "", roomPassModalScope).then(function (ok) {
        if (ok) {
          setEditPassword(pw);
          editPasswordSet = true;
          passwordScope = roomPassModalScope;
          // Authenticate this session right away: the lock-state broadcast
          // can beat the response handler and would otherwise prompt the
          // person who just set the password.
          if (passwordScope === "view") sendAuth(pw);
          renderShareUrls();
        } else {
          // Server rejected the request (e.g. stale lock) — say so instead
          // of silently looking like nothing happened.
          showToast(t("设置失败"));
        }
        return ok;
      });
    });
  }

  function setRoomPassword() {
    return openRoomPasswordDialog("设置房间密码", "密码可手动输入或重新生成，范围决定验证的是查看还是编辑");
  }

  function changeRoomPassword() {
    roomPassModalScope = passwordScope || "edit";
    return askPassword({
      passwordKind: "edit",
      title: t("修改房间密码"),
      label: t("房间密码"),
      hint: t("密码可手动输入或重新生成，范围决定验证的是查看还是编辑"),
      confirmLabel: t("确认"),
      initialPassword: generateEditPassword(),
      generate: true,
      scope: roomPassModalScope,
      scopeChange: function (v) { roomPassModalScope = v; },
      preferRemembered: false
    }).then(function (pw) {
      if (!pw) return false;
      // Rotating a locked room's password requires the current one; if this
      // browser does not have it, saveEditPassword re-prompts on 403.
      return saveEditPassword(pw, getEditPassword(), roomPassModalScope).then(function (ok) {
        if (ok) {
          setEditPassword(pw);
          passwordScope = roomPassModalScope;
          if (passwordScope === "view") sendAuth(pw);
          renderShareUrls();
        } else {
          showToast(t("设置失败"));
        }
        return ok;
      });
    });
  }

  function unlockRoom() {
    return saveEditPassword("", getEditPassword()).then(function (ok) {
      if (ok) {
        setEditPassword("");
        editPasswordSet = false;
        passwordScope = "";
        renderShareUrls();
      } else {
        showToast(t("解除锁定失败"));
      }
      return ok;
    });
  }

  function roomEditUrl() {
    return location.origin + location.pathname;
  }

  // Share-mode chosen in the dialog ("plain" default, "md").
  var shareMode = "plain";

  function shareUrlWithMode(url) {
    if (!shareMode) return url;
    return url + (url.indexOf("?") >= 0 ? "&" : "?") + "mode=" + encodeURIComponent(shareMode);
  }

  function renderShareUrls() {
    if (!shareModal || readOnlyMode) return;
    // A single room link: the URL path is the room key, mode just picks the
    // default view. Access control lives in the room password, not the URL.
    if (shareRoomUrl) shareRoomUrl.textContent = shareUrlWithMode(roomEditUrl());
    // Room-password row: reflects the server's lock state and scope.
    if (sharePassValue) {
      sharePassValue.textContent = editPasswordSet
        ? t("已设置") + " · " + t(passwordScope === "view" ? "查看" : "编辑")
        : t("未设置");
    }
    if (sharePassCopy) {
      sharePassCopy.textContent = editPasswordSet ? t("复制") : t("设置");
    }
    if (sharePassReset) sharePassReset.hidden = !editPasswordSet;
    if (sharePassUnlock) sharePassUnlock.hidden = !editPasswordSet;
    if (shareHint) {
      shareHint.textContent = !editPasswordSet
        ? t("未锁定：任何拿到链接的人都能查看和编辑。建议先设置密码再分享")
        : passwordScope === "view"
          ? t("已锁定：查看和编辑都需要输入密码")
          : t("已锁定：编辑需要输入密码");
    }
    if (shareQrNote) {
      shareQrNote.textContent = t("二维码为房间链接，扫码即可打开");
    }
    if (shareQrWrap) {
      shareQrWrap.innerHTML = "";
      var url = shareUrlWithMode(roomEditUrl());
      try {
        if (window.qrcode) {
          var qr = qrcode(0, "M");
          qr.addData(url);
          qr.make();
          // SVG stays crisp and needs no data: URL (CSP-friendly).
          shareQrWrap.innerHTML = qr.createSvgTag(4, 10);
        } else {
          shareQrWrap.textContent = url;
        }
      } catch (e) {
        shareQrWrap.textContent = url;
      }
    }
  }

  function openShare() {
    if (!shareModal || readOnlyMode) return;
    renderShareUrls();
    shareModal.hidden = false;
  }

  function closeShare() {
    if (shareModal) shareModal.hidden = true;
  }

  function copyText(text, btn) {
    if (!text) return;
    var done = function () {
      if (!btn) return;
      var old = btn.textContent;
      btn.textContent = t("已复制");
      btn.classList.add("copy-ok");
      window.setTimeout(function () {
        btn.textContent = old;
        btn.classList.remove("copy-ok");
      }, 1200);
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done, function () {
        fallbackCopy(text);
        done();
      });
    } else {
      fallbackCopy(text);
      done();
    }
  }

  function fallbackCopy(text) {
    var ta = document.createElement("textarea");
    ta.value = text;
    ta.style.position = "fixed";
    ta.style.opacity = "0";
    document.body.appendChild(ta);
    ta.select();
    try {
      document.execCommand("copy");
    } catch (e) { /* ignore */ }
    ta.remove();
  }

  // --- Markdown preview -------------------------------------------------------

  function togglePreview() {
    previewVisible = !previewVisible;
    if (previewPane) previewPane.hidden = !previewVisible;
    if (content) content.hidden = previewVisible;
    if (cursorLayer) cursorLayer.hidden = previewVisible;
    if (previewBtn) previewBtn.setAttribute("data-active", String(previewVisible));
    if (previewVisible) renderPreview();
  }

  function schedulePreviewRefresh() {
    if (!previewVisible) return;
    window.clearTimeout(previewTimer);
    previewTimer = window.setTimeout(renderPreview, 250);
  }

  function renderPreview() {
    if (!previewVisible || !previewBody) return;
    var text = content ? content.value : "";
    var html;
    if (window.marked) {
      try {
        html = window.marked.parse(text, { gfm: true, breaks: false });
      } catch (e) {
        html = escapeHTML(text);
      }
    } else {
      html = escapeHTML(text);
    }
    var host = document.createElement("div");
    host.innerHTML = html;
    sanitizeMarkdown(host);
    if (window.hljs) {
      var blocks = host.querySelectorAll("pre code");
      for (var i = 0; i < blocks.length; i++) {
        try {
          window.hljs.highlightElement(blocks[i]);
        } catch (e) { /* ignore */ }
      }
    }
    previewBody.innerHTML = host.innerHTML;
  }

  // Strip anything that could execute or exfiltrate: raw HTML is rendered by
  // marked, so remove dangerous elements/attributes after the fact.
  function sanitizeMarkdown(root) {
    var walker = document.createTreeWalker(root, NodeFilter.SHOW_ELEMENT);
    var toRemove = [];
    var el;
    while ((el = walker.nextNode())) {
      var tag = el.tagName.toLowerCase();
      if (
        tag === "script" || tag === "style" || tag === "iframe" ||
        tag === "object" || tag === "embed" || tag === "form" ||
        tag === "input" || tag === "button" || tag === "meta" || tag === "link"
      ) {
        toRemove.push(el);
        continue;
      }
      var attrs = el.attributes;
      for (var a = attrs.length - 1; a >= 0; a--) {
        if (attrs[a].name.toLowerCase().indexOf("on") === 0) {
          el.removeAttribute(attrs[a].name);
        }
      }
      if (el.hasAttribute("href")) {
        var href = el.getAttribute("href").trim().toLowerCase();
        if (!/^(https?:|mailto:|#|\/)/.test(href)) {
          el.removeAttribute("href");
        }
      }
      if (el.hasAttribute("src")) {
        var src = el.getAttribute("src").trim().toLowerCase();
        if (!/^(https?:|data:image\/|\/)/.test(src)) {
          el.removeAttribute("src");
        }
      }
    }
    for (var i = 0; i < toRemove.length; i++) toRemove[i].remove();
  }


  window.setInterval(function () {
    if (lastExpiresAt) renderExpires(lastExpiresAt);
  }, 60000);
  // Refresh file list periodically so other clients see new uploads.
  window.setInterval(function () {
    loadFiles(true);
  }, 15000);

  updatePeers();
  loadFiles();
  connectWS();
  // Let the WS state establish the authoritative CRDT ids first.  A REST
  // response rebuilds a text-only document with fresh "server:*" ids; if it
  // wins a race with the initial WS snapshot, local ops can no longer be
  // replayed by anchor and the draft may be re-diffed as a duplicate tail.
  load(false);
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

    var url = wsURL + (wsURL.indexOf("?") >= 0 ? "&" : "?") + "clientId=" + encodeURIComponent(CLIENT_ID);
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

      if (
        (msg.type === "state" || msg.type === "update" || msg.type === "ops") &&
        (compositionActive || compositionCommitPending)
      ) {
        // Keep the native IME buffer untouched.  The queued messages are
        // applied after the committed local composition has become CRDT ops.
        deferredContentMessages.push(msg);
        return;
      }

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

  // Sent/acked bookkeeping key. An "ins" and a later "del" target the same
  // item id, so tracking bare ids would let an ack for the insert batch drop a
  // still-unsent delete of the same character (lost op → divergence).
  function opKey(op) {
    return op.op + "|" + op.id;
  }

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

  function messageGeneration(data) {
    var n = Number(data && data.generation);
    if (!Number.isFinite(n) || n < 0) return 0;
    return Math.floor(n);
  }

  function messageExists(data) {
    return !(data && data.exists === false);
  }

  // Track the room's lock state from snapshots (WS state or REST load).
  function noteEditPasswordSet(data) {
    if (data && data.editPasswordSet !== undefined && data.editPasswordSet !== null) {
      editPasswordSet = !!data.editPasswordSet;
    }
    if (data && data.passwordScope) {
      passwordScope = String(data.passwordScope);
    } else if (data && data.editPasswordSet !== undefined && data.editPasswordSet !== null) {
      // Rooms locked before scope support gate edits only.
      passwordScope = editPasswordSet ? "edit" : "";
    }
  }

  // A state snapshot can be delayed behind an ops broadcast on the same
  // connection because sync and broker notifications have separate producers.
  // Room generation distinguishes a recreated key from the old incarnation;
  // version then orders snapshots within one incarnation.
  function snapshotIsStale(data) {
    var generation = messageGeneration(data);
    var exists = messageExists(data);
    var version = Number(data && data.version) || 0;
    var hasGeneration = data && data.generation !== undefined && data.generation !== null;

    if (!hasGeneration) {
      return knownExists && exists && version < knownVersion;
    }
    if (generation < knownGeneration) return true;
    if (generation > knownGeneration) return false;
    if (!exists && knownExists) return false;
    if (exists && !knownExists) return true;
    return exists && knownExists && version < knownVersion;
  }

  function restSnapshotIsStale(data) {
    if (snapshotIsStale(data)) return true;
    var generation = messageGeneration(data);
    var exists = messageExists(data);
    var version = Number(data && data.version) || 0;
    var hasGeneration = data && data.generation !== undefined && data.generation !== null;
    if (hasGeneration) {
      if (generation !== knownGeneration) return false;
      if (exists && (knownExists ? version <= knownVersion : true)) return true;
      return false;
    }
    return knownExists && exists && version <= knownVersion;
  }

  function handleState(data) {
    if (compositionIsActive()) {
      deferredContentMessages.push(data);
      return;
    }
    var version = data.version || 0;
    var generation = messageGeneration(data);
    var exists = messageExists(data);
    if (snapshotIsStale(data)) return;
    noteEditPasswordSet(data);
    if (data.passwordRequired) {
      // View-protected room, not authenticated: the server withheld the
      // content. Keep the editor locked until the password is entered (or
      // remembered from an earlier unlock in this tab).
      content.readOnly = true;
      requestViewPassword();
      return;
    }
    if (content.readOnly && !readOnlyMode) content.readOnly = false;
    var localText = content.value;
    var previousSyncedText = lastSyncedText;
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
    // Empty-room responses intentionally omit generation; retain the last
    // incarnation so a delayed old snapshot cannot look newer than tombstone 0.
    if (data.generation !== undefined && data.generation !== null) {
      knownGeneration = generation;
    }
    knownExists = exists;
    syncRequestedAt = 0;
    updateMeta(data);
    // Only a new room incarnation (generation bump) invalidates undo anchors;
    // plain sync snapshots keep the same ids, so the stacks survive.
    if (generation > knownGeneration) clearUndoHistory();

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
        // to a text three-way rebase.  Re-diffing the entire textarea directly
        // can turn a peer's already-visible tail into local inserts, and then
        // the peer ops append that tail a second time.
        pendingOps = [];
      }
    }
    // Only re-diff when there really are unsynced local edits (replay failed).
    // Without pending ops the textarea is just a stale view — re-diffing it
    // would revert the very change this snapshot delivers.
    if (!replayed && hadPending && localText !== serverText) {
      var rebasedText = localText;
      if (previousSyncedText && previousSyncedText !== localText) {
        rebasedText = merge3(previousSyncedText, localText, serverText);
      }
      if (rebasedText !== serverText) applyLocalText(rebasedText);
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
    captureHistory(false);
  }

  function handleRemoteOps(data) {
    if (compositionIsActive()) {
      deferredContentMessages.push(data);
      return;
    }
    var version = data.version || 0;
    var generation = messageGeneration(data);
    var hasGeneration = data && data.generation !== undefined && data.generation !== null;
    var fromSelf = !!(data.updatedBy && data.updatedBy === CLIENT_ID);

    if (hasGeneration && generation < knownGeneration) return;
    if (hasGeneration && generation > knownGeneration) {
      requestSync();
      return;
    }
    if (hasGeneration && !knownExists && data.exists !== false) {
      requestSync();
      return;
    }
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
      if (hasGeneration) knownGeneration = generation;
      knownExists = true;
      updateMeta(data);
      if (typeof data.content === "string") lastSyncedText = data.content;
      // Secondary ack path (covers a dropped direct ack).
      var acked = {};
      (data.ops || []).forEach(function (op) {
        if (op && op.id) acked[opKey(op)] = true;
      });
      if (Object.keys(acked).length) {
        pendingOps = pendingOps.filter(function (op) {
          return !(op && op.id && acked[opKey(op)]);
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
      if (hasGeneration) knownGeneration = generation;
      knownExists = true;
      lastSyncedText = data.content;
      updateMeta(data);
      onLocalTextChanged(oldText, data.content || "");
      renderCursors();
      scheduleCursorSend();
      setIdleStatus();
      return;
    }

    knownVersion = version;
    if (hasGeneration) knownGeneration = generation;
    knownExists = true;
    updateMeta(data);

    var next = doc.materialize();
    applyingRemote = true;
    setContentValue(next, sel);
    applyingRemote = false;
    // Keep other peers' carets sticky across this remote edit.
    onLocalTextChanged(oldText, next);
    renderCursors();
    scheduleCursorSend();
    captureHistory(false);
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
        return !(op && op.id && batch.ids[opKey(op)]);
      });
    }
    if (msg.error) {
      // Batch rejected (capacity/validation): its ops were dropped above and
      // the server follows up with a state snapshot to converge on.
      if (msg.error === "edit password required" || msg.error === "view password required") {
        // Locked room: ask for the room password, remember it, then resync
        // the full text over REST (the rejected batch is already dropped).
        askEditPassword().then(function (p) {
          if (!p) {
            setStatus("error");
            return;
          }
          setEditPassword(p);
          putFailures = 0;
          schedulePutFallback();
        });
        return;
      }
      if (msg.error === "invalid view password") {
        onInvalidViewPassword();
        return;
      }
      setStatus("error");
      return;
    }
    var version = msg.version || 0;
    var generation = messageGeneration(msg);
    var hasGeneration = msg.generation !== undefined && msg.generation !== null;
    if (hasGeneration && generation >= knownGeneration) {
      knownGeneration = generation;
      knownExists = msg.exists !== false;
    }
    if (version === knownVersion + 1) {
      knownVersion = version;
    }
    // version > knownVersion + 1: peers' intermediate updates are still in
    // flight on this socket; let them advance knownVersion in order.
    if (msg.expiresAt) updateMeta(msg);
    if (pendingOps.length) scheduleFlush();
    else if (!flushTimer) setIdleStatus();
    captureHistory(false);
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
    pushUndoEntry(diff.ops);
    scheduleFlush();
    captureHistory(false);
  }

  function onCompositionStart() {
    if (applyingRemote || compositionActive) return;
    // Selection changes generated by the IME (including the provisional
    // pinyin caret) are local implementation details.  Do not let a cursor
    // timer that was scheduled just before composition start leak them.
    window.clearTimeout(cursorTimer);
    cursorTimer = 0;
    compositionActive = true;
    compositionCommitPending = false;
    compositionInputPending = false;
    compositionBaseText = content.value;
    compositionBaseDoc = doc.clone();
  }

  function onCompositionEnd() {
    if (!compositionActive) return;
    compositionActive = false;
    compositionCommitPending = true;
    // Some browsers dispatch the final input event immediately after
    // compositionend.  Let that event update textarea.value before reading it.
    window.setTimeout(commitComposition, 0);
  }

  function compositionIsActive() {
    return compositionActive || compositionCommitPending;
  }

  function captureTextSelection(text) {
    var value = String(text || "");
    return {
      startCp: CRDT.utf16ToCodePointOffset(value, content.selectionStart || 0),
      endCp: CRDT.utf16ToCodePointOffset(value, content.selectionEnd || 0),
      dir: content.selectionDirection || "none",
      useAnchor: false,
      hadFocus: document.activeElement === content
    };
  }

  function commitComposition() {
    if (!compositionCommitPending) return;

    var baseText = compositionBaseText;
    var baseDoc = compositionBaseDoc;
    var finalText = content.value;
    var sel = captureTextSelection(finalText);
    var oldText = doc.materialize();
    var applied = true;

    if (!baseDoc) {
      baseDoc = doc.clone();
      baseText = oldText;
    }

    // Build the composition diff against the document that was visible when
    // the IME started, then apply those anchored ops to the live document.
    // Remote content messages were held meanwhile, so the anchors remain
    // valid and the composition cannot be mistaken for a whole-tail insert.
    bumpClockFromDoc();
    var diff = CRDT.diffToOps(
      CRDT.codePoints(baseText),
      baseDoc.visibleIds(),
      CRDT.codePoints(finalText),
      CLIENT_ID,
      localClock
    );
    if (diff.ops.length) {
      localClock = diff.clock;
      var result = doc.applyBatch(diff.ops);
      if (!result.ok) {
        applied = false;
        setStatus("error");
        requestSync();
      } else {
        pendingOps = pendingOps.concat(diff.ops);
        pushUndoEntry(diff.ops);
      }
    }

    compositionBaseText = "";
    compositionBaseDoc = null;
    compositionInputPending = false;
    compositionCommitPending = false;

    if (applied) {
      var next = doc.materialize();
      applyingRemote = true;
      setContentValue(next, sel);
      applyingRemote = false;
      onLocalTextChanged(oldText, next);
      renderCursors();
      scheduleCursorSend();
      if (pendingOps.length) scheduleFlush();
    }

    flushDeferredContentMessages();
  }

  function flushDeferredContentMessages() {
    if (compositionIsActive() || !deferredContentMessages.length) return;
    var queued = deferredContentMessages;
    deferredContentMessages = [];
    queued.forEach(function (msg) {
      if (!msg || !msg.type) return;
      if (msg.type === "state" || msg.type === "update") {
        handleState(msg);
      } else if (msg.type === "ops") {
        handleRemoteOps(msg);
      } else if (msg.type === "rest") {
        mergeServerState(msg.data);
      } else if (msg.type === "rest-load") {
        applyLoadedSnapshot(msg.data, msg.force);
      }
    });
  }

  function onInput() {
    if (applyingRemote) return;
    if (compositionActive || compositionCommitPending) {
      compositionInputPending = true;
      return;
    }
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
        Object.keys(b.ids).forEach(function (k) {
          outstanding[k] = true;
        });
      });
      var batch = pendingOps.filter(function (op) {
        return op && op.id && !outstanding[opKey(op)];
      });
      if (!batch.length && pendingOps.length) {
        // Everything is in flight; ack or the watchdog will move things on.
        return;
      }
      // Cap each message: the server rejects >4096 ops per batch and closes
      // the socket past its 256KiB read limit — one huge paste must not turn
      // into an endless reconnect/resend loop. Ops are causally ordered, so a
      // prefix is always safe to send; the remainder follows on the next flush.
      var overflow = batch.length > maxOpsPerSend;
      if (overflow) batch = batch.slice(0, maxOpsPerSend);
      // Empty batch = TTL-only update; the server refreshes expiry without a
      // full-content PUT (which could clobber peers' edits we haven't seen).
      var seq = nextSeq++;
      try {
        socket.send(JSON.stringify({
          type: "ops",
          ops: batch,
          ttlSeconds: ttlSeconds,
          seq: seq,
          // View-authenticated sessions are exempt server-side; the password
          // still travels for edit-scoped rooms and REST fallbacks.
          password: getEditPassword() || rememberedViewPassword() || undefined
        }));
        pulseTraffic("up");
        if (batch.length) {
          var ids = {};
          batch.forEach(function (op) {
            ids[opKey(op)] = true;
          });
          sentBatches[seq] = { ids: ids, at: Date.now() };
        }
        if (overflow) scheduleFlush();
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
    var pw = getEditPassword() || rememberedViewPassword();
    if (pw) body.password = pw;
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
        if (result.status === 403) {
          // Locked room: ask for the edit password, remember it, retry.
          askEditPassword().then(function (p) {
            if (!p) {
              putFailures++;
              setStatus("error");
              return;
            }
            setEditPassword(p);
            putFailures = 0;
            schedulePutFallback();
          });
          return;
        }
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
        knownGeneration = messageGeneration(result.data);
        knownExists = true;
        updateMeta(result.data);
        pendingOps = [];
        sentBatches = {};
        // After PUT replace, rebuild local doc to match server chain.
        doc = CRDT.buildFromString(CLIENT_ID, text) || new CRDT.Doc();
        localClock = 0;
        bumpClockFromDoc();
        lastSyncedText = text;
        captureHistory(false);
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
    if (compositionIsActive()) {
      deferredContentMessages.push({
        type: "rest",
        data: data
      });
      return;
    }
    noteEditPasswordSet(data);
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
    if (data.generation !== undefined && data.generation !== null) {
      knownGeneration = messageGeneration(data);
    }
    knownExists = messageExists(data);
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
      var headers = { Accept: "application/json" };
      var vp = rememberedViewPassword();
      if (vp) headers["X-Goclip-Password"] = vp;
      fetch(apiURL, { headers: headers })
        .then(function (response) {
          if (!response.ok) throw new Error("HTTP " + response.status);
          return response.json();
        })
        .then(function (data) {
          if (connected || putInFlight) return;
          if (
            messageGeneration(data) !== knownGeneration ||
            messageExists(data) !== knownExists ||
            (messageExists(data) && (data.version || 0) !== knownVersion)
          ) {
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
    showSelfLabel();
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
    cursorTimer = 0;
    if (compositionIsActive()) return;
    cursorTimer = window.setTimeout(sendCursorPosition, cursorDelayMs);
  }

  function sendCursorPosition(silent) {
    if (compositionIsActive()) return;
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
    var headers = { Accept: "application/json" };
    var vp = rememberedViewPassword();
    if (vp) headers["X-Goclip-Password"] = vp;
    fetch(apiURL, { headers: headers })
      .then(function (response) {
        if (!response.ok) throw new Error("HTTP " + response.status);
        return response.json();
      })
      .then(function (data) {
        if (compositionIsActive()) {
          deferredContentMessages.push({
            type: "rest-load",
            data: data,
            force: force
          });
          return;
        }
        applyLoadedSnapshot(data, force);
      })
      .catch(function () {
        // 401 on a view-protected room is expected before the user enters the
        // password — the WebSocket locked-state prompt handles it.
        if (!connected) setStatus("error");
      });
  }

  function applyLoadedSnapshot(data, force) {
    // REST has no CRDT items — rebuild from content; WS state will refine.
    if (
      !force &&
      socket &&
      (socket.readyState === WebSocket.CONNECTING || socket.readyState === WebSocket.OPEN)
    ) {
      return;
    }
    if (!force && pendingOps.length) {
      return;
    }
    // A WS snapshot at the same or newer version already landed; a slower
    // REST response must not clobber the live CRDT tree with fresh ids.
    if (!force && connected && restSnapshotIsStale(data)) {
      return;
    }
    noteEditPasswordSet(data);
    // REST rebuilds CRDT ids from scratch — keep caret by code-point index only.
    var sel = captureSelection();
    sel.useAnchor = false;
    doc = CRDT.buildFromString("server", data.content || "") || new CRDT.Doc();
    localClock = 0;
    knownVersion = data.version || 0;
    if (data.generation !== undefined && data.generation !== null) {
      knownGeneration = messageGeneration(data);
    }
    knownExists = messageExists(data);
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
    schedulePreviewRefresh();
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
    if (data.viewKey) {
      viewKey = String(data.viewKey);
    }
    if (data.expiresAt) {
      lastExpiresAt = data.expiresAt;
      renderExpires(data.expiresAt);
    } else {
      lastExpiresAt = null;
      expiresText.textContent = t(data.exists ? "已保存" : "尚未保存");
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
      expiresText.textContent = t("已过期 · ") + d.toLocaleString();
      return;
    }
    expiresText.textContent = t("过期 ") + d.toLocaleString() + t("（剩余 ") + formatDuration(ms) + t("）");
  }

  function formatDuration(ms) {
    var s = Math.ceil(ms / 1000);
    if (s < 60) return s + t("秒");
    if (s < 3600) return Math.ceil(s / 60) + t(" 分钟");
    if (s < 86400) {
      var h = s / 3600;
      return (h < 10 ? h.toFixed(1) : Math.round(h)) + t(" 小时");
    }
    var days = s / 86400;
    return (days < 10 ? days.toFixed(1) : Math.round(days)) + t(" 天");
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
    el.title = (isSelf ? t("自己 · ") : t("协作者 · ")) + id;
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

  // --- Toast notifications (top-right) ----------------------------------------

  var toastWrap = null;

  function ensureToastWrap() {
    if (toastWrap && toastWrap.parentNode) return toastWrap;
    toastWrap = document.createElement("div");
    toastWrap.className = "toast-wrap";
    toastWrap.setAttribute("aria-live", "polite");
    document.body.appendChild(toastWrap);
    return toastWrap;
  }

  // Show a transient top-right toast. tone: "" (error, default) | "ok".
  function showToast(msg, tone) {
    var wrap = ensureToastWrap();
    var el = document.createElement("div");
    el.className = "toast" + (tone === "ok" ? " ok" : "");
    var text = document.createElement("span");
    text.className = "toast-text";
    text.textContent = msg;
    var close = document.createElement("button");
    close.type = "button";
    close.className = "toast-close";
    close.textContent = "×";
    close.setAttribute("aria-label", t("关闭"));
    close.addEventListener("click", function () {
      dismissToast(el);
    });
    el.appendChild(text);
    el.appendChild(close);
    wrap.appendChild(el);
    window.setTimeout(function () {
      dismissToast(el);
    }, 4000);
  }

  function dismissToast(el) {
    if (!el || !el.parentNode) return;
    el.classList.add("leaving");
    window.setTimeout(function () {
      if (el.parentNode) el.parentNode.removeChild(el);
    }, 200);
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
    // Room-password dialog extras: regenerate button + scope radios.
    if (modalPasswordGen) {
      modalPasswordGen.addEventListener("click", function () {
        if (modalPassword) modalPassword.value = generateEditPassword();
        if (modalPassword) modalPassword.focus();
      });
    }
    if (modalScopeWrap) {
      var scopeRadios = modalScopeWrap.querySelectorAll("input[type=radio]");
      for (var ri = 0; ri < scopeRadios.length; ri++) {
        scopeRadios[ri].addEventListener("change", function () {
          if (modalState && modalState.options && modalState.options.scopeChange) {
            modalState.options.scopeChange(this.value);
          }
        });
      }
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
      if (readOnlyMode || !hasFiles(e)) return;
      e.preventDefault();
      dragDepth++;
      if (appRoot) appRoot.classList.add("is-file-dragover");
    });
    document.addEventListener("dragover", function (e) {
      if (readOnlyMode || !hasFiles(e)) return;
      e.preventDefault();
      if (e.dataTransfer) e.dataTransfer.dropEffect = "copy";
    });
    document.addEventListener("dragleave", function (e) {
      if (readOnlyMode || (!hasFiles(e) && dragDepth === 0)) return;
      e.preventDefault();
      dragDepth = Math.max(0, dragDepth - 1);
      if (dragDepth === 0 && appRoot) {
        appRoot.classList.remove("is-file-dragover");
      }
    });
    document.addEventListener("drop", function (e) {
      dragDepth = 0;
      if (appRoot) appRoot.classList.remove("is-file-dragover");
      if (readOnlyMode || !hasFiles(e)) return;
      e.preventDefault();
      e.stopPropagation();
      if (e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files.length) {
        promptUploadFiles(e.dataTransfer.files);
      }
    });

    // Paste files (screenshot / Finder copy). Pure file paste always uploads;
    // mixed text+file paste only uploads when not focused on editor text intent.
    document.addEventListener("paste", function (e) {
      if (readOnlyMode) return;
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
      roomTitle.title = t("房间 ") + key + t(" · 文件上传已开启（三击切换）");
      roomTitle.classList.add("upload-on");
    } else {
      roomTitle.title = t("房间 ") + key + t(" · 文件上传已关闭（三击管理员开关）");
      roomTitle.classList.remove("upload-on");
    }
  }

  function setupRoomTitleToggle() {
    if (!roomTitle) return;
    roomTitle.addEventListener("click", function () {
      if (readOnlyMode) return;
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
    if (readOnlyMode) return;
    var next = !fileUploadEnabled;
    var action = next ? t("开启") : t("关闭");
    askPassword({
      title: (next ? t("开启本空间文件上传") : t("关闭本空间文件上传")),
      hint: next
        ? t("验证管理员密码后，允许在此空间上传文件")
        : t("验证管理员密码后，禁止在此空间上传新文件（已有文件仍可下载/删除）"),
      confirmLabel: action,
      passwordKind: "admin",
      preferRemembered: true
    }).then(function (adminPassword) {
      if (!adminPassword) return;
      rememberAdminPassword(adminPassword);
      setFilesStatus(t("正在" + (next ? "开启" : "关闭") + "文件上传…"));
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
            throw new Error(t("管理员密码错误"));
          }
          if (result.status === 403) {
            throw new Error(result.data.error || t("文件功能未启用"));
          }
          if (!result.ok) {
            throw new Error(result.data.error || ("HTTP " + result.status));
          }
          setFileUploadEnabled(!!result.data.fileUploadEnabled);
          setFilesStatus(
            fileUploadEnabled ? t("本空间已开启文件上传") : t("本空间已关闭文件上传"),
            "ok"
          );
          // Make sure status is visible even when the file list is empty.
          if (fileUploadEnabled) setFilesPanelVisible(true);
          loadFiles(true);
        })
        .catch(function (err) {
          setFilesStatus(err.message || t("设置失败"), "err");
          showToast(err.message || t("设置失败"));
          if (err.message === t("管理员密码错误")) {
            return askPassword({
              title: (next ? t("开启本空间文件上传") : t("关闭本空间文件上传")),
              hint: t("管理员密码错误，请重试"),
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
                if (response.status === 401) throw new Error(t("管理员密码错误"));
                if (!response.ok) throw new Error(t("设置失败"));
                return response.json();
              }).then(function (data) {
                setFileUploadEnabled(!!data.fileUploadEnabled);
                setFilesStatus(
                  fileUploadEnabled ? t("本空间已开启文件上传") : t("本空间已关闭文件上传"),
                  "ok"
                );
                if (fileUploadEnabled) setFilesPanelVisible(true);
                loadFiles(true);
              }).catch(function (e2) {
                setFilesStatus(e2.message || t("设置失败"), "err");
                showToast(e2.message || t("设置失败"));
              });
            });
          }
        });
    });
  }

  function loadFiles(silent) {
    if (!fileList) return;
    var headers = { Accept: "application/json" };
    var vp = rememberedViewPassword();
    if (vp) headers["X-Goclip-Password"] = vp;
    fetch(filesAPIURL, { headers: headers })
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
        if (!silent) {
          setFilesStatus(t("文件列表加载失败"), "err");
          showToast(t("文件列表加载失败"));
        }
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
        (f.expiresAt ? t(" · 过期 ") + formatShortTime(f.expiresAt) : "");

      meta.appendChild(nameEl);
      meta.appendChild(sub);

      var actions = document.createElement("div");
      actions.className = "file-actions";

      var dl = document.createElement("button");
      dl.type = "button";
      dl.textContent = t("下载");
      dl.addEventListener("click", function () {
        downloadFile(f.id, f.name);
      });

      var del = document.createElement("button");
      del.type = "button";
      del.className = "danger";
      del.textContent = t("删除");
      del.addEventListener("click", function () {
        deleteFile(f.id, f.name);
      });

      actions.appendChild(dl);
      if (!readOnlyMode) actions.appendChild(del);
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
   *   passwordKind: "admin" | "edit" | "file" | "view",
   *   label, placeholder,
   *   preferRemembered,          // prefill with the remembered password
   *   initialPassword,          // explicit prefill (overrides remembered)
   *   generate,                 // show the "regenerate" button
   *   scope,                    // checked scope for the scope radios
   *   scopeChange               // fn(newScope) fired on radio change
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
      modalState = { resolve: resolve, busy: false, options: options };

      var kind = options.passwordKind === "admin" ? "admin" : options.passwordKind === "edit" || options.passwordKind === "view" ? "edit" : "file";
      var defaultTitle = kind === "admin" ? t("输入管理员密码") : kind === "edit" ? t("输入编辑密码") : t("输入文件密码");
      var defaultLabel = kind === "admin" ? t("管理员密码") : kind === "edit" ? t("编辑密码") : t("文件密码");
      var defaultPlaceholder = kind === "admin" ? t("管理员密码") : kind === "edit" ? t("编辑密码") : t("文件密码（下载用）");

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
        modalConfirm.textContent = options.confirmLabel || t("确认");
        modalConfirm.disabled = false;
      }
      if (modalCancel) modalCancel.disabled = false;
      if (modalPassword) {
        var remembered =
          kind === "admin" ? rememberedAdminPassword() : kind === "edit" ? getEditPassword() : rememberedFilePassword();
        modalPassword.value = options.initialPassword !== undefined
          ? options.initialPassword
          : (options.preferRemembered !== false ? remembered : "");
        modalPassword.placeholder = options.placeholder || defaultPlaceholder;
        modalPassword.autocomplete = kind === "admin" ? "current-password" : "off";
        modalPassword.disabled = false;
      }
      // Room-password dialog extras: regenerate button + scope radios.
      if (modalPasswordGen) {
        modalPasswordGen.hidden = !options.generate;
      }
      if (modalScopeWrap) {
        modalScopeWrap.hidden = !options.scope;
        if (options.scope) {
          var radios = modalScopeWrap.querySelectorAll("input[type=radio]");
          for (var sri = 0; sri < radios.length; sri++) {
            radios[sri].checked = radios[sri].value === options.scope;
          }
        }
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
      setModalError(t("请输入密码"));
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
    if (readOnlyMode) return;
    var files = Array.prototype.slice.call(fileListLike || []).filter(Boolean);
    if (!files.length) return;

    var names = files.map(function (f) {
      return f.name || t("未命名文件");
    });

    // Room open → only file password.
    // Room closed → admin password first, then file password (one-shot; does not open the room).
    var start;
    if (fileUploadEnabled) {
      start = Promise.resolve(null);
    } else {
      start = askPassword({
        title: t("上传文件 · 管理员密码"),
        hint: t("本空间未开放上传，需管理员密码（共 ") + files.length + t(" 个文件）"),
        fileNames: names,
        confirmLabel: t("下一步"),
        passwordKind: "admin"
      });
    }

    start.then(function (adminPassword) {
      if (!fileUploadEnabled && !adminPassword) return;
      return askPassword({
        title: t("上传文件 · 文件密码"),
        hint: t("设置文件密码；下载时需要此密码（本批共 ") + files.length + t(" 个，共用）"),
        fileNames: names,
        confirmLabel: t("上传"),
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
    setFilesStatus(t("正在上传 ") + files.length + t(" 个文件…"));

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
            if (lastErr === t("管理员密码错误")) {
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
          title: t("上传文件 · 管理员密码"),
          hint: t("管理员密码错误，请重试"),
          fileNames: files.map(function (f) { return f.name || t("未命名文件"); }),
          confirmLabel: t("下一步"),
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
        setFilesStatus(t("已上传 ") + ok + t(" 个文件"), "ok");
      } else if (ok > 0) {
        var partialMsg = t("成功 ") + ok + t(" · 失败 ") + fail + (lastErr ? " · " + lastErr : "");
        setFilesStatus(partialMsg, "err");
        showToast(partialMsg);
      } else {
        // Keep panel visible briefly so the error is readable even if list is empty.
        setFilesPanelVisible(true);
        var allFailMsg = lastErr || t("上传失败");
        setFilesStatus(allFailMsg, "err");
        showToast(allFailMsg);
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
      if (result.status === 401) throw new Error(t("管理员密码错误"));
      if (result.status === 403) {
        throw new Error(result.data.error || t("上传未启用"));
      }
      if (result.status === 413) throw new Error(t("文件过大"));
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
      title: t("下载文件"),
      hint: t("下载需要文件密码（上传时设置）"),
      fileNames: name ? [name] : [],
      confirmLabel: t("下载"),
      passwordKind: "file"
    }).then(function (password) {
      if (!password) return;
      rememberFilePassword(password);
      setFilesStatus(t("正在下载…"));
      return fetch(filesAPIURL + "/" + encodeURIComponent(id), {
        method: "GET",
        headers: { "X-File-Password": password }
      })
        .then(function (response) {
          if (response.status === 401) {
            rememberFilePassword("");
            throw new Error(t("文件密码错误"));
          }
          if (response.status === 403) throw new Error(t("文件访问未启用"));
          if (response.status === 404) throw new Error(t("文件不存在"));
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
          setFilesStatus(t("已开始下载"), "ok");
        })
        .catch(function (err) {
          setFilesStatus(err.message || t("下载失败"), "err");
          showToast(err.message || t("下载失败"));
          if (err.message === t("文件密码错误")) {
            return askPassword({
              title: t("下载文件"),
              hint: t("文件密码错误，请重试"),
              fileNames: name ? [name] : [],
              confirmLabel: t("下载"),
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
                  throw new Error(response.status === 401 ? t("文件密码错误") : t("下载失败"));
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
                  setFilesStatus(t("已开始下载"), "ok");
                });
              }).catch(function (e2) {
                setFilesStatus(e2.message || t("下载失败"), "err");
                showToast(e2.message || t("下载失败"));
              });
            });
          }
        });
    });
  }

  function deleteFile(id, name) {
    if (readOnlyMode) return;
    // Always prompt for admin password — never silent delete with cached password only.
    askPassword({
      title: t("删除文件"),
      hint: t("删除需要管理员密码"),
      fileNames: name ? [name] : [id],
      confirmLabel: t("删除"),
      passwordKind: "admin"
    }).then(function (password) {
      if (!password) return;
      rememberAdminPassword(password);
      setFilesStatus(t("正在删除…"));
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
            throw new Error(t("管理员密码错误"));
          }
          if (response.status === 404) throw new Error(t("文件不存在"));
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
          setFilesStatus(t("已删除"), "ok");
        })
        .catch(function (err) {
          setFilesStatus(err.message || t("删除失败"), "err");
          showToast(err.message || t("删除失败"));
          if (err.message === t("管理员密码错误")) {
            return askPassword({
              title: t("删除文件"),
              hint: t("管理员密码错误，请重试"),
              fileNames: name ? [name] : [id],
              confirmLabel: t("删除"),
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
                if (response.status === 401) throw new Error(t("管理员密码错误"));
                if (!response.ok && response.status !== 204) throw new Error(t("删除失败"));
                loadFiles(true);
                setFilesStatus(t("已删除"), "ok");
              }).catch(function (e2) {
                setFilesStatus(e2.message || t("删除失败"), "err");
                showToast(e2.message || t("删除失败"));
              });
            });
          }
        });
    });
  }
})();
