(function () {
  var key = decodeURIComponent(window.location.pathname.slice(1));
  var apiURL = "/api/clipboard/" + encodeURIComponent(key);
  var filesAPIURL = apiURL + "/files";
  var settingsAPIURL = apiURL + "/settings";
  var urlParams = new URLSearchParams(window.location.search || "");
  // ?mode=md | ?mode=plain — share links can default the view on open.
  var MODE_PARAM = (urlParams.get("mode") || "").toLowerCase();
  var wsURL =
    (location.protocol === "https:" ? "wss://" : "ws://") +
    location.host +
    "/api/clipboard/" +
    encodeURIComponent(key) +
    "/ws";
  var ADMIN_PASSWORD_KEY = "goclipboard:adminPassword";
  var FILE_PASSWORD_KEY = "goclipboard:filePassword";
  var THEME_KEY = "goclipboard:theme";
  var LANG_KEY = "goclipboard:lang";

  // --- i18n ----------------------------------------------------------------
  // Keyed by the Chinese source string; English overrides, Chinese falls through.
  var EN = {
    "已保存": "Saved",
    "尚未保存": "Not saved yet",
    "已保存，复制链接就能分享给其他人": "Saved — copy the link to share it",
    "有效期需为大于 0 的整数": "TTL must be a whole number greater than 0",
    "已过期 · ": "Expired · ",
    "过期 ": "Expires ",
    "剩余 {0} · {1} 过期": "{0} left · expires {1}",
    "已过期": "Expired",
    "（剩余 ": " (",
    "）": ")",
    "秒": "s",
    "有效期": "TTL",
    "直接粘贴或输入内容": "Paste or type here",
    "内容实时同步给打开同一链接的人，到期自动销毁": "Everyone with this link sees your edits live. The room self-destructs when it expires.",
    "从剪贴板粘贴": "Paste from clipboard",
    "浏览器未授权读取剪贴板，请按 Ctrl/⌘+V 粘贴": "Clipboard access was blocked — press Ctrl/⌘+V to paste",
    "请按 Ctrl/⌘+V 粘贴": "Press Ctrl/⌘+V to paste",
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
    "未锁定：任何拿到链接的人都能查看和编辑，也可设置密码锁定房间。建议先设密码再分享": "Open: anyone with the link can view, edit, or set a password and lock the room. Set a password before sharing.",
    "已锁定：编辑需要输入密码": "Locked: editing requires the password.",
    "版本历史": "Version history",
    "点击条目预览，确认后再恢复": "Click a snapshot to preview, then restore if it looks right",
    "注意：历史会保留已删除/覆盖的内容，分享链接前请先清空或设密码": "Note: history keeps deleted/overwritten text. Clear it or set a password before sharing.",
    "版本预览": "Snapshot preview",
    "恢复此版本": "Restore this version",
    "字符": "chars",
    "手动": "manual",
    "加载中…": "Loading…",
    "已锁定": "Locked",
    "只读 · 解锁编辑": "Read-only · unlock to edit",
    "解锁编辑": "Unlock editing",
    "此房间的查看不受限制，输入密码后即可编辑": "Anyone can read this room; enter the password to edit",
    "此房间已加密": "This room is locked",
    "此房间已锁定": "This room is locked",
    "查看和编辑都需要房间密码": "Viewing and editing both need the room password",
    "查看无需密码，编辑需要房间密码": "Anyone can view; editing needs the room password",
    "解锁": "Unlock",
    "验证中…": "Checking…",
    "密码错误，请重试": "Wrong password, try again",
    "验证失败，请检查网络后重试": "Could not verify — check your connection and retry",
    "房间已锁定 · 输入密码后才能查看和编辑": "Room locked · enter the password to view and edit",
    "已同步": "Synced",
    "已断开": "Offline",
    "连接中…": "Connecting…",
    "保存中…": "Saving…",
    "正在保存修改": "Saving your changes",
    "我": "me",
    "已连接 · 修改会实时同步给所有打开此房间的人": "Connected · edits sync live to everyone in this room",
    "连接已断开 · 正在重试，修改会先保存在本地": "Disconnected · retrying, edits are kept locally",
    "正在加载剪贴板…": "Loading clipboard…",
    "加载超时，正在重试…": "Still loading — retrying…",
    "加载历史失败": "Failed to load history",
    "查看历史需要房间密码": "Room password required to view history",
    "已存档当前版本": "Snapshot saved",
    "内容未变化，未新增快照": "No changes — no new snapshot",
    "存档失败": "Failed to save snapshot",
    "暂无可存档内容": "Nothing to archive yet",
    "清空历史": "Clear history",
    "确定清空全部版本历史？此操作不可撤销": "Clear all version history? This cannot be undone.",
    "已清空历史": "History cleared",
    "清空历史失败": "Failed to clear history",
    "分享链接": "Share",
    "切换主题": "Toggle theme",
    "分享链接 · 手机扫码或复制链接": "Share — scan the QR code or copy a link",
    "房间链接": "Room link",
    "二维码为房间链接，扫码即可打开": "The QR code encodes the room link — scan to open",
    "复制": "Copy",
    "已复制": "Copied",
    "复制房间链接": "Copy room link",
    "新建房间": "New room",
    "关闭": "Close",
    "存档当前版本": "Save snapshot",
    "恢复": "Restore",
    "暂无历史版本，编辑后自动记录": "No snapshots yet — they are captured automatically as you edit",
    "暂无历史版本": "No snapshots yet",
    "编辑后自动记录，也可手动存档": "Captured automatically as you edit, or save one manually",
    "已恢复到该版本": "Restored",
    "恢复失败": "Restore failed",
    "（当前）": " (current)",
    "当前": "Current",
    "预览": "Preview",
    "编辑": "Edit",
    "自动": "Auto",
    "深色": "Dark",
    "浅色": "Light",
    "主题：": "Theme: ",
    "语言": "Language",
    "切换到": "Switch to ",
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
    if (langBtn) {
      // Monogram shows the language you will switch *to* (same as classic EN/中).
      var next = LANG === "en" ? "zh" : "en";
      var mono = next === "en" ? "EN" : "中";
      langBtn.setAttribute("data-lang", LANG === "en" ? "en" : "zh");
      langBtn.setAttribute("data-next", next);
      var monoEl = langBtn.querySelector(".lang-mono");
      if (monoEl) monoEl.textContent = mono;
      langBtn.title =
        t("语言") +
        " · " +
        (LANG === "en" ? "English → 中文" : "中文 → English");
      langBtn.setAttribute("aria-label", t("切换到") + mono);
    }
    var nodes = document.querySelectorAll("[data-i18n]");
    for (var i = 0; i < nodes.length; i++) {
      var el = nodes[i];
      el.textContent = t(el.getAttribute("data-i18n"));
    }
    var ph = document.querySelectorAll("[data-i18n-placeholder]");
    for (var j = 0; j < ph.length; j++) {
      ph[j].placeholder = t(ph[j].getAttribute("data-i18n-placeholder"));
    }
    var ar = document.querySelectorAll("[data-i18n-aria]");
    for (var k = 0; k < ar.length; k++) {
      ar[k].setAttribute("aria-label", t(ar[k].getAttribute("data-i18n-aria")));
    }
    applyShortcutTitles();
    // Re-render dynamic texts.
    if (typeof renderExpires === "function" && lastExpiresAt) renderExpires(lastExpiresAt);
    if (typeof updateRoomTitleChrome === "function") updateRoomTitleChrome();
    if (typeof updatePeers === "function") updatePeers();
    if (typeof renderHistory === "function") renderHistory();
    if (typeof setFilesStatus === "function") setFilesStatus("");
  }

  // Shortcut hints live in tooltips: the icons stay compact, and the keyboard
  // path (the fastest one for this app) becomes discoverable.
  function applyShortcutTitles() {
    var mod = /Mac/i.test(navigator.platform || "") ? "⌘" : "Ctrl+";
    var hints = {
      undoBtn: [t("撤销"), mod + "Z"],
      redoBtn: [t("重做"), mod + "⇧Z"],
      previewBtn: [t("Markdown 预览"), ""],
      historyBtn: [t("版本历史"), ""],
      shareBtn: [t("分享链接"), ""],
      newRoomBtn: [t("新建房间"), ""]
    };
    Object.keys(hints).forEach(function (id) {
      var el = document.getElementById(id);
      if (!el) return;
      var label = hints[id][0];
      var key = hints[id][1];
      el.title = key ? label + " (" + key + ")" : label;
    });
  }

  // --- Theme ---------------------------------------------------------------
  // SVG icons (Feather-style, stroke inherits currentColor).
  // Lucide (ISC) glyphs, inlined so the icons cost no extra request.
  var THEME_ICONS = {
    dark: '<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path d="M20.985 12.486a9 9 0 1 1-9.473-9.472c.405-.022.617.46.402.803a6 6 0 0 0 8.268 8.268c.344-.215.825-.004.803.401"/></svg>',
    light: '<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/></svg>',
    auto: '<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path d="M12 2v2"/><path d="M14.837 16.385a6 6 0 1 1-7.223-7.222c.624-.147.97.66.715 1.248a4 4 0 0 0 5.26 5.259c.589-.255 1.396.09 1.248.715"/><path d="M16 12a4 4 0 0 0-4-4"/><path d="m19 5-1.256 1.256"/><path d="M20 12h2"/></svg>'
  };

  function currentThemePreference() {
    try {
      return localStorage.getItem(THEME_KEY) || "";
    } catch (e) {
      return "";
    }
  }

  // The highlight.js theme stylesheet is injected on first use (Markdown
  // preview) instead of sitting in <head>, where it was a render-blocking
  // request for a feature most visits never open.
  var hljsThemeLink = null;

  function ensureHljsTheme() {
    if (hljsThemeLink && hljsThemeLink.parentNode) return hljsThemeLink;
    hljsThemeLink = document.createElement("link");
    hljsThemeLink.rel = "stylesheet";
    hljsThemeLink.id = "hljsTheme";
    document.head.appendChild(hljsThemeLink);
    applyHljsThemeHref();
    return hljsThemeLink;
  }

  function applyHljsThemeHref() {
    if (!hljsThemeLink) return;
    var pref = currentThemePreference();
    var dark = pref === "dark" || (pref !== "light" && window.matchMedia("(prefers-color-scheme: dark)").matches);
    hljsThemeLink.href = dark
      ? "/static/vendor/highlight.github-dark.css"
      : "/static/vendor/highlight.github.css";
  }

  function applyTheme() {
    var pref = currentThemePreference();
    document.documentElement.dataset.theme = pref === "light" ? "light" : (pref === "dark" ? "dark" : "");
    applyHljsThemeHref();
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
  var copyRoomBtn = document.getElementById("copyRoomBtn");
  var status = document.getElementById("status");
  var statusText = status ? status.querySelector(".status-text") : null;
  var ttlValue = document.getElementById("ttlValue");
  var ttlUnit = document.getElementById("ttlUnit");
  var expiresText = document.getElementById("expiresText");
  var peerTabs = document.getElementById("peerTabs");
  var dotUp = document.getElementById("dotUp");
  var dotDown = document.getElementById("dotDown");
  var content = document.getElementById("content");
  var cursorLayer = document.getElementById("cursorLayer");
  var loadingOverlay = document.getElementById("loadingOverlay");
  var loadingText = document.getElementById("loadingText");
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
  // Generic confirm dialog
  var confirmModal = document.getElementById("confirmModal");
  var confirmTitle = document.getElementById("confirmTitle");
  var confirmMessage = document.getElementById("confirmMessage");
  var confirmOk = document.getElementById("confirmOk");
  var confirmCancel = document.getElementById("confirmCancel");
  // New feature controls
  var undoBtn = document.getElementById("undoBtn");
  var redoBtn = document.getElementById("redoBtn");
  var previewBtn = document.getElementById("previewBtn");
  var historyBtn = document.getElementById("historyBtn");
  var shareBtn = document.getElementById("shareBtn");
  var newRoomBtn = document.getElementById("newRoomBtn");
  var themeBtn = document.getElementById("themeBtn");
  var langBtn = document.getElementById("langBtn");
  var previewPane = document.getElementById("previewPane");
  var emptyHint = document.getElementById("emptyHint");
  var emptyPasteBtn = document.getElementById("emptyPasteBtn");
  var emptyShareBtn = document.getElementById("emptyShareBtn");
  // Room unlock gate
  var roomLock = document.getElementById("roomLock");
  var roomLockForm = document.getElementById("roomLockForm");
  var roomLockTitle = document.getElementById("roomLockTitle");
  var roomLockHint = document.getElementById("roomLockHint");
  var roomLockLabel = document.getElementById("roomLockLabel");
  var roomLockPassword = document.getElementById("roomLockPassword");
  var roomLockError = document.getElementById("roomLockError");
  var roomLockSubmit = document.getElementById("roomLockSubmit");
  var unlockEditBtn = document.getElementById("unlockEditBtn");
  var previewBody = document.getElementById("previewBody");
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
  var historyClear = document.getElementById("historyClear");
  var historyClose = document.getElementById("historyClose");
  var historyWarn = document.getElementById("historyWarn");
  var historyPreviewModal = document.getElementById("historyPreviewModal");
  var historyPreviewMeta = document.getElementById("historyPreviewMeta");
  var historyPreviewBody = document.getElementById("historyPreviewBody");
  var historyPreviewClose = document.getElementById("historyPreviewClose");
  var historyPreviewRestore = document.getElementById("historyPreviewRestore");
  var historyPreviewIdx = -1;

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
  // Set once the first save of this room has been announced to the user.
  var firstSaveAnnounced = false;
  // True after the server has described the room's initial state once.
  var initialStateSeen = false;
  // Guards the "invalid TTL" toast so a streak of bad input warns only once.
  var ttlInvalidNotified = false;
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
  // Distinguishes "not connected yet" from "was connected, then dropped":
  // only the latter is worth showing as an error.
  var everConnected = false;
  var lastMsgAt = 0; // liveness: server pushes presence every ~5s
  var msgStaleMs = 15000;
  var restPollTimer = 0;
  var restPollMs = 4000;
  var trafficUpTimer = 0;
  var trafficDownTimer = 0;
  var fileUploadEnabled = false;
  var roomTitleClickCount = 0;
  var roomTitleClickTimer = 0;
  // Undo / redo stacks: entries are { ops, at } where del ops carry
  // { after, ch } captured from the doc so redo can revive via fresh ids.
  var undoStack = [];
  var redoStack = [];
  var undoCoalesceMs = 800;
  var maxUndoDepth = 100;
  // Server-side version history (shared across browsers). Cached list from
  // GET /history; auto-capture happens on the server after content changes.
  var historyAPIURL = apiURL + "/history";
  var historySnapshots = [];
  var historyLoading = false;
  // Markdown preview
  var previewVisible = false;
  var previewTimer = 0;
  // Declared with the rest of the state: ?mode=md calls togglePreview() during
  // init, before the loader helpers further down have run their statements.
  var vendorLoads = {};

  roomTitle.textContent = "/" + key;
  updateRoomTitleChrome();
  applyTheme();
  applyLang();
  // Share links can default the view: ?mode=md opens in markdown preview,
  // ?mode=plain (or absent) opens as plain text.
  if (MODE_PARAM === "md" || MODE_PARAM === "markdown") {
    togglePreview();
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
  setupEmptyHint();
  setupRoomLock();

  // Keyboard shortcuts: undo / redo (intercepted so they map to CRDT ops and
  // survive remote edits, unlike the browser's native textarea undo).
  document.addEventListener("keydown", function (e) {
    var mod = e.metaKey || e.ctrlKey;
    if (!mod) return;
    var k = (e.key || "").toLowerCase();
    if (k === "z" && !e.shiftKey) {
      e.preventDefault();
      if (!blockWhenReadOnly()) undo();
    } else if (k === "z" && e.shiftKey) {
      e.preventDefault();
      if (!blockWhenReadOnly()) redo();
    } else if (k === "y") {
      e.preventDefault();
      if (!blockWhenReadOnly()) redo();
    }
  });

  // --- New feature controls (undo/redo, preview, history, share, theme, lang) ---

  function setupNewControls() {
    if (undoBtn) undoBtn.addEventListener("click", function () { if (!blockWhenReadOnly()) undo(); });
    if (redoBtn) redoBtn.addEventListener("click", function () { if (!blockWhenReadOnly()) redo(); });
    if (previewBtn) previewBtn.addEventListener("click", togglePreview);
    if (historyBtn) historyBtn.addEventListener("click", function () { if (!blockWhenReadOnly()) openHistory(); });
    if (shareBtn) shareBtn.addEventListener("click", openShare);
    if (themeBtn) themeBtn.addEventListener("click", cycleTheme);
    if (newRoomBtn) {
      newRoomBtn.addEventListener("click", function () {
        window.location.href = "/";
      });
    }
    if (langBtn) langBtn.addEventListener("click", toggleLang);
    if (shareClose) shareClose.addEventListener("click", closeShare);
    if (historyClose) historyClose.addEventListener("click", closeHistory);
    if (historyCapture) historyCapture.addEventListener("click", function () {
      if (blockWhenReadOnly()) return;
      captureHistoryManual();
    });
    if (historyClear) historyClear.addEventListener("click", function () {
      if (blockWhenReadOnly()) return;
      clearHistoryTrail();
    });
    if (historyPreviewClose) {
      historyPreviewClose.addEventListener("click", closeHistoryPreview);
    }
    if (historyPreviewRestore) {
      historyPreviewRestore.addEventListener("click", function () {
        if (historyPreviewIdx < 0) return;
        if (blockWhenReadOnly()) return;
        var idx = historyPreviewIdx;
        closeHistoryPreview();
        restoreSnapshot(idx);
      });
    }
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
        if (blockWhenReadOnly()) return;
        if (!editPasswordSet) {
          setRoomPassword();
          return;
        }
        var p = roomPassword();
        if (p) {
          // Already remembered — copy without re-prompt (value was verified
          // when it was saved or last used successfully).
          copyText(p, sharePassCopy);
          return;
        }
        // Prompted value must verify against the server before we store it,
        // otherwise a typo becomes a sticky wrong edit password.
        askEditPassword().then(function (pw) {
          if (!pw) return;
          fetch(historyAPIURL, {
            headers: { Accept: "application/json", "X-Goclip-Password": pw }
          }).then(function (res) {
            if (!res.ok) {
              showToast(t("房间密码错误，请重试"));
              return;
            }
            setEditPassword(pw);
            if (passwordScope === "view") rememberViewPassword(pw);
            renderShareUrls();
            copyText(pw, sharePassCopy);
          }).catch(function () {
            showToast(t("房间密码错误，请重试"));
          });
        });
      });
    }
    if (sharePassReset) {
      sharePassReset.addEventListener("click", function () {
        if (blockWhenReadOnly()) return;
        if (editPasswordSet) changeRoomPassword();
      });
    }
    if (sharePassUnlock) {
      sharePassUnlock.addEventListener("click", function () {
        if (blockWhenReadOnly()) return;
        if (editPasswordSet) unlockRoom();
      });
    }
    [shareModal, historyModal, historyPreviewModal].forEach(function (m) {
      if (!m) return;
      m.addEventListener("click", function (e) {
        if (e.target && e.target.closest && e.target.closest("[data-modal-dismiss]")) {
          if (m === historyPreviewModal) closeHistoryPreview();
          else if (m === historyModal) closeHistory();
          else m.hidden = true;
        }
      });
    });
    document.addEventListener("keydown", function (e) {
      if (e.key !== "Escape") return;
      // Topmost dialog first: snapshot preview sits above history, and the
      // confirm prompt sits above everything.
      if (confirmModal && !confirmModal.hidden) {
        closeConfirm(false);
        return;
      }
      if (historyPreviewModal && !historyPreviewModal.hidden) {
        closeHistoryPreview();
        return;
      }
      if (shareModal && !shareModal.hidden) {
        closeShare();
        return;
      }
      if (historyModal && !historyModal.hidden) closeHistory();
    });
    setupModalFocus();
  }

  // Dialogs used to leave focus on the trigger button behind the backdrop, so
  // Tab walked through the page before reaching dialog content and Escape
  // handed focus back to <body>. Keep focus inside whichever dialog is open.
  function modalStack() {
    return [confirmModal, passwordModal, historyPreviewModal, shareModal, historyModal].filter(function (m) {
      return m && !m.hidden;
    });
  }

  function focusablesIn(root) {
    var sel = "button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), " +
      "textarea:not([disabled]), [tabindex]:not([tabindex='-1'])";
    return Array.prototype.filter.call(root.querySelectorAll(sel), function (el) {
      return el.offsetParent !== null || el === document.activeElement;
    });
  }

  function setupModalFocus() {
    document.addEventListener("keydown", function (e) {
      if (e.key !== "Tab") return;
      var stack = modalStack();
      if (!stack.length) return;
      var top = stack[stack.length - 1];
      var items = focusablesIn(top);
      if (!items.length) return;
      var first = items[0];
      var last = items[items.length - 1];
      var active = document.activeElement;
      if (!top.contains(active)) {
        e.preventDefault();
        (e.shiftKey ? last : first).focus();
        return;
      }
      if (e.shiftKey && active === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && active === last) {
        e.preventDefault();
        first.focus();
      }
    });

    if (!window.MutationObserver) return;
    var openReturn = []; // focus targets to restore, one per open dialog
    var observer = new MutationObserver(function (records) {
      for (var i = 0; i < records.length; i++) {
        var rec = records[i];
        var t = rec.target;
        if (!t.classList || !t.classList.contains("modal")) continue;
        if (rec.attributeName !== "hidden") continue;
        // attributeOldValue is null when the attribute was absent. For a
        // boolean attribute its value is "", so presence is what matters:
        // present → was hidden, absent → was visible.
        var wasHidden = rec.oldValue !== null;
        if (!t.hidden && wasHidden) {
          // Save the trigger before moving focus: it is the element that had
          // focus while the dialog was still closed.
          openReturn.push(document.activeElement);
          var items = focusablesIn(t);
          if (items.length) items[0].focus();
        } else if (t.hidden && !wasHidden) {
          var back = openReturn.pop();
          if (back && document.contains(back) && back.offsetParent !== null) back.focus();
        }
      }
    });
    var modals = document.querySelectorAll(".modal");
    for (var m = 0; m < modals.length; m++) {
      observer.observe(modals[m], { attributes: true, attributeFilter: ["hidden"], attributeOldValue: true });
    }
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
    // Belt and braces: every entry point already checks, but a read-only room
    // must never mutate the document through a path we forgot to guard.
    if (blockWhenReadOnly()) return;
    if (!undoStack.length) return;
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
    if (blockWhenReadOnly()) return;
    if (!redoStack.length) return;
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
    if (undoBtn) undoBtn.disabled = undoStack.length === 0;
    if (redoBtn) redoBtn.disabled = redoStack.length === 0;
  }

  function clearUndoHistory() {
    undoStack = [];
    redoStack = [];
    updateUndoButtons();
  }

  // --- Version history (server-side trail, restore via CRDT diff) ------------

  function historyAuthHeaders(extra) {
    var headers = { Accept: "application/json" };
    if (extra) {
      Object.keys(extra).forEach(function (k) {
        headers[k] = extra[k];
      });
    }
    return withRoomPasswordHeaders(headers);
  }

  function applyHistoryPayload(data) {
    historySnapshots = (data && data.snapshots) || [];
    if (!Array.isArray(historySnapshots)) historySnapshots = [];
  }

  // When the room is password-protected, history reads/writes need the room
  // password (edit-scope rooms still keep content GET open, but not history).
  function ensureHistoryPassword() {
    if (!editPasswordSet) return Promise.resolve(true);
    if (roomPassword()) return Promise.resolve(true);
    return askEditPassword().then(function (entered) {
      if (!entered) return false;
      setEditPassword(entered);
      if (passwordScope === "view") rememberViewPassword(entered);
      return true;
    });
  }

  function fetchHistory(retried) {
    historyLoading = true;
    if (historyModal && !historyModal.hidden) renderHistory();
    return fetch(historyAPIURL, { headers: historyAuthHeaders() })
      .then(function (response) {
        if ((response.status === 401 || response.status === 403) && !retried) {
          // Stale or missing password — prompt once and retry.
          return askEditPassword().then(function (entered) {
            if (!entered) {
              historyLoading = false;
              showToast(t("查看历史需要房间密码"));
              if (historyModal && !historyModal.hidden) renderHistory();
              return null;
            }
            setEditPassword(entered);
            if (passwordScope === "view") rememberViewPassword(entered);
            return fetchHistory(true);
          });
        }
        if (response.status === 401 || response.status === 403) {
          // Retried with a (wrong) password — say so instead of a generic
          // network-style failure message.
          historyLoading = false;
          if (historyModal && !historyModal.hidden) renderHistory();
          showToast(t("房间密码错误，请重试"));
          return null;
        }
        if (!response.ok) throw new Error("HTTP " + response.status);
        return response.json();
      })
      .then(function (data) {
        if (data == null) return;
        // Nested retry returns undefined after handling UI.
        if (data && data.snapshots !== undefined) {
          applyHistoryPayload(data);
        }
        historyLoading = false;
        if (historyModal && !historyModal.hidden) renderHistory();
      })
      .catch(function () {
        historyLoading = false;
        if (historyModal && !historyModal.hidden) {
          renderHistory();
          showToast(t("加载历史失败"));
        }
      });
  }

  // Manual archive only — automatic checkpoints are recorded server-side
  // after content mutations (shared across every browser on the room).
  function captureHistoryManual() {
    if (blockWhenReadOnly()) return;
    if (!doc.materialize()) {
      showToast(t("暂无可存档内容"));
      return;
    }
    ensureHistoryPassword().then(function (ok) {
      if (!ok) return;
      var headers = historyAuthHeaders({ "Content-Type": "application/json" });
      fetch(historyAPIURL, { method: "POST", headers: headers, body: "{}" })
        .then(function (response) {
          if (response.status === 404) {
            showToast(t("暂无可存档内容"));
            return null;
          }
          if (response.status === 403 || response.status === 401) {
            showToast(t("查看历史需要房间密码"));
            return null;
          }
          if (!response.ok) throw new Error("HTTP " + response.status);
          return response.json();
        })
        .then(function (data) {
          if (!data) return;
          // Server skips a snapshot whose text matches the last one — only
          // claim "saved" when the trail actually grew.
          var added =
            data.snapshots !== undefined &&
            data.snapshots.length > historySnapshots.length;
          applyHistoryPayload(data);
          if (historyModal && !historyModal.hidden) renderHistory();
          showToast(t(added ? "已存档当前版本" : "内容未变化，未新增快照"), "ok");
        })
        .catch(function () {
          showToast(t("存档失败"));
        });
    });
  }

  function clearHistoryTrail() {
    if (!historySnapshots.length) {
      showToast(t("暂无历史版本"));
      return;
    }
    confirmAction({
      title: t("清空历史"),
      message: t("确定清空全部版本历史？此操作不可撤销"),
      confirmLabel: t("清空历史"),
      danger: true
    }).then(function (ok) {
      if (ok) doClearHistory();
    });
  }

  function doClearHistory() {
    ensureHistoryPassword().then(function (ok) {
      if (!ok) return;
      fetch(historyAPIURL, { method: "DELETE", headers: historyAuthHeaders() })
        .then(function (response) {
          if (response.status === 403 || response.status === 401) {
            showToast(t("查看历史需要房间密码"));
            return null;
          }
          if (!response.ok) throw new Error("HTTP " + response.status);
          return response.json();
        })
        .then(function (data) {
          if (!data) return;
          applyHistoryPayload(data);
          closeHistoryPreview();
          if (historyModal && !historyModal.hidden) renderHistory();
          showToast(t("已清空历史"), "ok");
        })
        .catch(function () {
          showToast(t("清空历史失败"));
        });
    });
  }

  function openHistory() {
    if (!historyModal) return;
    historyModal.hidden = false;
    ensureHistoryPassword().then(function (ok) {
      if (!ok) {
        historyModal.hidden = true;
        showToast(t("查看历史需要房间密码"));
        return;
      }
      fetchHistory();
    });
  }

  function closeHistory() {
    closeHistoryPreview();
    if (historyModal) historyModal.hidden = true;
  }

  function openHistoryPreview(idx) {
    var snap = historySnapshots[idx];
    if (!snap || !historyPreviewModal) return;
    historyPreviewIdx = idx;
    var isCurrent = snap.text === doc.materialize();
    var d = new Date(snap.at);
    var parts = [d.toLocaleString()];
    if (snap.version) parts.push("v" + snap.version);
    parts.push((snap.text || "").length + " " + t("字符"));
    if (isCurrent) parts.push(t("当前"));
    if (snap.manual) parts.push(t("手动"));
    if (historyPreviewMeta) historyPreviewMeta.textContent = parts.join(" · ");
    if (historyPreviewBody) historyPreviewBody.textContent = snap.text || "";
    if (historyPreviewRestore) {
      historyPreviewRestore.disabled = isCurrent;
      historyPreviewRestore.hidden = false;
    }
    historyPreviewModal.hidden = false;
  }

  function closeHistoryPreview() {
    historyPreviewIdx = -1;
    if (historyPreviewModal) historyPreviewModal.hidden = true;
    if (historyPreviewBody) historyPreviewBody.textContent = "";
  }

  function renderHistory() {
    if (!historyList) return;
    historyList.innerHTML = "";
    if (historyLoading && !historySnapshots.length) {
      var loading = document.createElement("li");
      loading.className = "history-empty";
      loading.textContent = t("加载中…");
      historyList.appendChild(loading);
      return;
    }
    if (!historySnapshots.length) {
      var empty = document.createElement("li");
      empty.className = "history-empty";
      var emptyTitle = document.createElement("span");
      emptyTitle.className = "history-empty-title";
      emptyTitle.textContent = t("暂无历史版本");
      empty.appendChild(emptyTitle);
      empty.appendChild(document.createTextNode(t("编辑后自动记录，也可手动存档")));
      historyList.appendChild(empty);
      return;
    }
    var current = doc.materialize();
    for (var i = historySnapshots.length - 1; i >= 0; i--) {
      (function (snap, idx) {
        var isCurrent = snap.text === current;
        var li = document.createElement("li");
        li.className = "history-item" + (isCurrent ? " is-current" : "");
        var meta = document.createElement("div");
        meta.className = "history-meta";
        var when = document.createElement("div");
        when.className = "history-when";
        var d = new Date(snap.at);
        var whenLabel = document.createElement("span");
        whenLabel.textContent = d.toLocaleString();
        when.appendChild(whenLabel);
        if (snap.version) {
          var ver = document.createElement("span");
          ver.className = "history-ver";
          ver.textContent = "v" + snap.version;
          when.appendChild(ver);
        }
        if (snap.manual) {
          var man = document.createElement("span");
          man.className = "history-ver";
          man.textContent = t("手动");
          when.appendChild(man);
        }
        if (isCurrent) {
          var badge = document.createElement("span");
          badge.className = "history-badge";
          badge.textContent = t("当前");
          when.appendChild(badge);
        }
        var prev = document.createElement("div");
        prev.className = "history-preview";
        var text = snap.text || "";
        prev.textContent = text.slice(0, 80) + (text.length > 80 ? "…" : "");
        meta.appendChild(when);
        meta.appendChild(prev);
        // Preview is a real button so the row has one clear action and the
        // restore button next to it is not nested inside another control.
        var open = document.createElement("button");
        open.type = "button";
        open.className = "history-open";
        open.setAttribute("aria-label", t("版本预览") + " · " + d.toLocaleString());
        open.appendChild(meta);
        open.addEventListener("click", function () {
          openHistoryPreview(idx);
        });
        var restore = document.createElement("button");
        restore.type = "button";
        restore.className = "btn-sm" + (isCurrent ? "" : " primary");
        restore.textContent = t("恢复");
        restore.disabled = isCurrent;
        restore.addEventListener("click", function () {
          restoreSnapshot(idx);
        });
        li.appendChild(open);
        li.appendChild(restore);
        historyList.appendChild(li);
      })(historySnapshots[i], i);
    }
  }

  // Restore diffs the current doc against the snapshot text and applies the
  // delta as ordinary CRDT ops — a merge, never a clobber.
  function restoreSnapshot(idx) {
    if (blockWhenReadOnly()) return;
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
    // applyLocalText only mutates the CRDT doc — mirror undo/redo and push the
    // materialized text into the textarea so the editor actually changes.
    applyingRemote = true;
    setContentValue(newText, captureSelection());
    applyingRemote = false;
    onLocalTextChanged(oldText, newText);
    renderCursors();
    scheduleCursorSend();
    updateUndoButtons();
    setFilesStatus(t("已恢复到该版本"), "ok");
    showToast(t("已恢复到该版本"), "ok");
    closeHistory();
  }

  // --- Share links + QR -------------------------------------------------------

  // --- Edit password (room lock) ---------------------------------------------
  // When a room is locked, every write must present this password. It lives
  // only in the locking browser's localStorage — the server never sends it
  // back. PasswordScope ("" | "edit" | "view") decides what the password gates:
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

  // Single source for room-password headers: localStorage (room owner) or
  // sessionStorage (view unlock in this tab). Every X-Goclip-Password site
  // must use this so file list / REST load stay in sync with WS auth.
  function roomPassword() {
    return getEditPassword() || rememberedViewPassword() || "";
  }

  function withRoomPasswordHeaders(headers) {
    headers = headers || {};
    var pw = roomPassword();
    if (pw) headers["X-Goclip-Password"] = pw;
    return headers;
  }

  function generateEditPassword() {
    // 58-char alphabet; rejection sampling avoids modulo bias on Uint32.
    var chars = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789";
    var out = "";
    var n = chars.length;
    var maxUnbiased = Math.floor(0x100000000 / n) * n;
    if (window.crypto && crypto.getRandomValues) {
      var arr = new Uint32Array(1);
      for (var i = 0; i < 12; i++) {
        var x;
        do {
          crypto.getRandomValues(arr);
          x = arr[0];
        } while (x >= maxUnbiased);
        out += chars[x % n];
      }
    } else {
      for (var j = 0; j < 12; j++) out += chars[Math.floor(Math.random() * n)];
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

  // --- Read-only controls ---------------------------------------------------
  // While an edit-locked room has no password the content stays readable, but
  // nothing that writes should be reachable: undo/redo, history capture and
  // restore, password changes, new room. Controls that only read (preview,
  // share link, unlock chip) stay live.

  var WRITE_CONTROLS = [
    "undoBtn", "redoBtn", "historyBtn", "newRoomBtn",
    "historyClear", "historyCapture", "historyPreviewRestore",
    "sharePassCopy", "sharePassReset", "sharePassUnlock"
  ];

  function applyReadOnlyControls() {
    var readOnly = roomNeedsUnlock() && !isViewGate();
    for (var i = 0; i < WRITE_CONTROLS.length; i++) {
      var el = document.getElementById(WRITE_CONTROLS[i]);
      if (!el) continue;
      // Undo/redo additionally depend on their stacks; updateUndoRedo() owns
      // that condition once we are writable again.
      if (!readOnly && el.id === "undoBtn") {
        el.disabled = undoStack.length === 0;
      } else if (!readOnly && el.id === "redoBtn") {
        el.disabled = redoStack.length === 0;
      } else {
        el.disabled = readOnly;
      }
      if (readOnly) el.setAttribute("aria-disabled", "true");
      else el.removeAttribute("aria-disabled");
    }
    // The TTL selector writes as well: changing it re-saves with a new expiry.
    if (ttlValue) ttlValue.disabled = readOnly;
    if (ttlUnit) ttlUnit.disabled = readOnly;
  }

  // Guard for handlers reachable without a click (keyboard, stale reference).
  function blockWhenReadOnly() {
    if (!roomNeedsUnlock() || isViewGate()) return false;
    showToast(t("当前为只读，解锁后才能编辑"));
    return true;
  }

  // --- Room gate (unlock panel) ----------------------------------------------
  // Both scopes lock the same way, so a single in-page panel handles them:
  //   view — the server withholds the content, the gate covers the editor;
  //   edit — content is readable, the gate covers it until the user unlocks.
  // The panel replaces the modal prompt, which used to appear only after a
  // keystroke was rejected, leaving typed text stranded in the editor.

  // "locked" | "unlocked". For view-scoped rooms the server is authoritative;
  // for edit-scoped rooms this reflects whether we hold a working password.
  var roomAccess = "locked";
  var roomLockBusy = false;
  // Set when the password was created or verified in this page session, so the
  // gate does not immediately demand it back from the person who just set it.
  var editPasswordJustSet = false;

  function roomNeedsUnlock() {
    return roomAccess !== "unlocked";
  }

  // Two very different situations share the word "locked":
  //   view scope — nothing is readable, so the gate has to own the editor;
  //   edit scope — reading is public, so covering the text would block the
  //                one thing the visitor is allowed to do.
  // For edit scope the editor stays visible and read-only, with a small
  // "unlock to edit" affordance in the status bar instead of a page-covering
  // panel.
  function isViewGate() {
    return passwordScope === "view";
  }

  function showRoomLock() {
    if (!roomLock) return;
    roomAccess = "locked";
    if (content) content.readOnly = true;
    if (isViewGate()) {
      roomLock.hidden = false;
      if (appRoot) {
        appRoot.classList.add("is-locked");
        appRoot.classList.remove("is-readonly");
      }
      if (roomLockTitle) roomLockTitle.textContent = t("此房间已加密");
      if (roomLockHint) roomLockHint.textContent = t("查看和编辑都需要房间密码");
      if (roomLockLabel) roomLockLabel.textContent = t("房间密码");
      if (roomLockSubmit) roomLockSubmit.textContent = t("解锁");
      if (roomLockError) roomLockError.textContent = "";
      if (roomLockForm) roomLockForm.removeAttribute("data-invalid");
      setStatus("locked");
    } else {
      // Edit scope: reading stays untouched. The editor is read-only and the
      // status bar offers the unlock, so nothing covers the content.
      roomLock.hidden = true;
      if (appRoot) {
        appRoot.classList.remove("is-locked");
        appRoot.classList.add("is-readonly");
      }
      setStatus("locked");
    }
    if (unlockEditBtn) unlockEditBtn.hidden = isViewGate();
    applyReadOnlyControls();
    updateEmptyHint();
  }

  function hideRoomLock() {
    roomAccess = "unlocked";
    if (roomLock) roomLock.hidden = true;
    if (appRoot) {
      appRoot.classList.remove("is-locked");
      appRoot.classList.remove("is-readonly");
    }
    if (content) content.readOnly = false;
    if (unlockEditBtn) unlockEditBtn.hidden = true;
    if (roomLockPassword) roomLockPassword.value = "";
    if (roomLockError) roomLockError.textContent = "";
    if (roomLockForm) roomLockForm.removeAttribute("data-invalid");
    applyReadOnlyControls();
    updateEmptyHint();
  }

  // Open the unlock prompt for an edit-locked room. Same password dialog the
  // rest of the app uses, opened on demand rather than over the content.
  function promptEditUnlock() {
    askPassword({
      passwordKind: "edit",
      title: t("解锁编辑"),
      label: t("编辑密码"),
      hint: t("此房间的查看不受限制，输入密码后即可编辑"),
      confirmLabel: t("解锁"),
      preferRemembered: false
    }).then(function (pw) {
      if (!pw) return;
      unlockWithPassword(pw);
    });
  }

  function unlockWithPassword(pw) {
    tryUnlock(pw).then(function (ok) {
      if (!ok) {
        showToast(t("密码错误，请重试"));
        return;
      }
      rememberRoomPassword(pw, passwordScope);
      editPasswordJustSet = true;
      hideRoomLock();
      scheduleFlush();
    }).catch(function () {
      showToast(t("验证失败，请检查网络后重试"));
    });
  }

  function showRoomLockError(msg) {
    if (roomLockForm) roomLockForm.setAttribute("data-invalid", "true");
    if (roomLockError) roomLockError.textContent = msg;
    if (roomLockPassword) {
      roomLockPassword.focus();
      roomLockPassword.select();
    }
  }

  // Try a password against the server, then authenticate the socket with it.
  // Resolves true when the server accepted it.
  function tryUnlock(pw) {
    return fetch(apiURL, {
      headers: { Accept: "application/json", "X-Goclip-Password": pw }
    }).then(function (res) {
      if (res.status === 401 || res.status === 403) return false;
      if (!res.ok) throw new Error("HTTP " + res.status);
      return true;
    });
  }

  function submitRoomLock(e) {
    if (e && e.preventDefault) e.preventDefault();
    if (roomLockBusy) return;
    var pw = roomLockPassword ? String(roomLockPassword.value || "") : "";
    if (!pw) {
      showRoomLockError(t("请输入密码"));
      return;
    }
    roomLockBusy = true;
    if (roomLockSubmit) {
      roomLockSubmit.disabled = true;
      roomLockSubmit.textContent = t("验证中…");
    }
    if (roomLockError) roomLockError.textContent = "";
    tryUnlock(pw).then(function (ok) {
      roomLockBusy = false;
      if (roomLockSubmit) {
        roomLockSubmit.disabled = false;
        roomLockSubmit.textContent = t("解锁");
      }
      if (!ok) {
        showRoomLockError(t("密码错误，请重试"));
        return;
      }
      rememberRoomPassword(pw, passwordScope);
      editPasswordJustSet = true;
      hideRoomLock();
      if (passwordScope === "view") {
        // The socket session needs the same credential for live sync.
        requestViewPassword();
      } else {
        // Edit unlock: reload so the socket (and pending state) start from an
        // authenticated session rather than replaying a rejected batch.
        scheduleFlush();
        load(true);
      }
    }).catch(function () {
      roomLockBusy = false;
      if (roomLockSubmit) {
        roomLockSubmit.disabled = false;
        roomLockSubmit.textContent = t("解锁");
      }
      showRoomLockError(t("验证失败，请检查网络后重试"));
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

  // Persist the password for both edit (localStorage) and view (sessionStorage)
  // so REST headers and WS auth stay consistent after set/unlock.
  function rememberRoomPassword(pw, scope) {
    if (!pw) return;
    setEditPassword(pw);
    if (scope === "view" || passwordScope === "view") {
      rememberViewPassword(pw);
    }
  }

  function sendAuth(pw) {
    if (!socket || socket.readyState !== WebSocket.OPEN) return;
    try {
      socket.send(JSON.stringify({ type: "auth", password: pw }));
    } catch (e) { /* ignore */ }
  }

  // The socket asked for the view password. A remembered one is replayed
  // silently; otherwise the gate panel takes over (no modal).
  function requestViewPassword() {
    var remembered = rememberedViewPassword() || (editPasswordSet ? getEditPassword() : "");
    if (remembered) {
      sendAuth(remembered);
      return;
    }
    showRoomLock();
  }

  // The server rejected our password: forget it and show the gate again with
  // an inline reason. The stored edit password (localStorage) is the same
  // value, so a stale copy must be dropped too — otherwise the remembered
  // value would auto-resend forever and the panel would never show.
  function onInvalidViewPassword() {
    rememberViewPassword("");
    setEditPassword("");
    showRoomLock();
    showRoomLockError(t("密码错误，请重试"));
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
      // Mark before the request: the lock-state broadcast can arrive before
      // this promise resolves, and syncEditLockGate reads the flag then.
      editPasswordJustSet = true;
      return saveEditPassword(pw, "", roomPassModalScope).then(function (ok) {
        if (ok) {
          editPasswordSet = true;
          passwordScope = roomPassModalScope;
          rememberRoomPassword(pw, passwordScope);
          // Authenticate this session right away: the lock-state broadcast
          // can beat the response handler and would otherwise prompt the
          // person who just set the password.
          if (passwordScope === "view") sendAuth(pw);
          renderShareUrls();
        } else {
          editPasswordJustSet = false;
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
          passwordScope = roomPassModalScope;
          rememberRoomPassword(pw, passwordScope);
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
        rememberViewPassword("");
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
    if (!shareModal) return;
    // A single room link: the URL path is the room key, mode just picks the
    // default view. Access control lives in the room password, not the URL.
    if (shareRoomUrl) {
      var roomUrl = shareUrlWithMode(roomEditUrl());
      shareRoomUrl.textContent = roomUrl;
      // The field ellipsizes on narrow screens; keep the full URL reachable.
      shareRoomUrl.title = roomUrl;
    }
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
    if (!shareModal) return;
    // QR is generated from the room URL; the library is small but only the
    // share dialog needs it, so load it on first open and render when ready.
    if (!window.qrcode) {
      loadScriptOnce("/static/vendor/qrcode.min.js")
        .then(renderShareUrls)
        .catch(function () { /* renderShareUrls falls back to the plain URL */ });
    }
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

  // --- On-demand vendor scripts ---------------------------------------------
  // marked / highlight.js / qrcode are only needed once the user opens the
  // preview or the share dialog. Loading them lazily keeps them off the
  // critical path, so the first content paint waits on app.js alone.
  // (vendorLoads itself is declared up top — see the preview state block.)

  function loadScriptOnce(src) {
    if (vendorLoads[src]) return vendorLoads[src];
    vendorLoads[src] = new Promise(function (resolve, reject) {
      var el = document.createElement("script");
      el.src = src;
      el.async = true;
      el.onload = function () { resolve(); };
      el.onerror = function () { reject(new Error("failed to load " + src)); };
      document.head.appendChild(el);
    });
    // A failed load must stay retryable (e.g. flaky network on first open).
    vendorLoads[src].catch(function () { delete vendorLoads[src]; });
    return vendorLoads[src];
  }

  function loadMarkdownVendors() {
    return Promise.all([
      window.marked ? null : loadScriptOnce("/static/vendor/marked.min.js"),
      window.hljs ? null : loadScriptOnce("/static/vendor/highlight.min.js")
    ]).then(function () {
      ensureHljsTheme();
    });
  }

  // --- Markdown preview -------------------------------------------------------

  function togglePreview() {
    previewVisible = !previewVisible;
    if (previewPane) previewPane.hidden = !previewVisible;
    if (content) content.hidden = previewVisible;
    if (cursorLayer) cursorLayer.hidden = previewVisible;
    if (previewBtn) {
      previewBtn.setAttribute("data-active", String(previewVisible));
      // The icon change alone is invisible to assistive tech.
      previewBtn.setAttribute("aria-pressed", String(previewVisible));
    }
    updateEmptyHint();
    if (previewVisible) {
      // First render shows the raw text until marked arrives, then re-renders.
      loadMarkdownVendors()
        .then(renderPreview)
        .catch(function () { /* keep the plain-text fallback */ });
      renderPreview();
    }
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
      var hrefAttrs = [];
      for (var a = attrs.length - 1; a >= 0; a--) {
        var aname = attrs[a].name.toLowerCase();
        if (aname.indexOf("on") === 0) {
          el.removeAttribute(attrs[a].name);
        } else if (aname === "href" || aname === "xlink:href") {
          // xlink:href (SVG foreign content) is not exposed via
          // hasAttribute("href") — collect it and validate like href,
          // otherwise <a xlink:href="javascript:..."> survives.
          hrefAttrs.push(attrs[a].name);
        }
      }
      for (var i = 0; i < hrefAttrs.length; i++) {
        var href = el.getAttribute(hrefAttrs[i]).trim().toLowerCase();
        if (!/^(https?:|mailto:|#|\/)/.test(href)) {
          el.removeAttribute(hrefAttrs[i]);
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
  armLoadingSlowHint();
  connectWS();
  // WS state carries the authoritative CRDT ids, so it wins any race with the
  // REST load below. The REST copy may still paint first when nothing is
  // unsaved (see applyLoadedSnapshot) — that is what turns a 2-3 s blank
  // editor into first content as soon as the smallest response lands.
  load(false);
  scheduleCursorSend();
  focusEditorOnOpen();

  // Opening a room is almost always followed by "paste something", but focus
  // started on <body>, so Cmd+V went nowhere. Focus the editor once the first
  // snapshot has painted, unless the user already started interacting.
  function focusEditorOnOpen() {
    // On touch devices focusing would pop the on-screen keyboard over a page
    // the user may have opened just to read or copy.
    var touchFirst = window.matchMedia && window.matchMedia("(hover: none) and (pointer: coarse)").matches;
    if (touchFirst) return;
    var done = false;
    var attempt = function () {
      if (done || !content) return;
      var active = document.activeElement;
      var busy = active && active !== document.body && active !== content &&
        (active.tagName === "INPUT" || active.tagName === "SELECT" ||
          active.tagName === "TEXTAREA" || active.tagName === "BUTTON");
      if (busy) {
        done = true;
        return;
      }
      if (!initialSnapshotApplied) return;
      done = true;
      if (previewVisible) return; // ?mode=md opens read-only-ish preview
      try {
        content.focus({ preventScroll: true });
        // Put the caret at the end so typing continues the content.
        var end = content.value.length;
        content.setSelectionRange(end, end);
      } catch (e) {
        // ignore
      }
    };
    var poll = window.setInterval(function () {
      attempt();
      if (done) window.clearInterval(poll);
    }, 120);
    window.setTimeout(function () {
      window.clearInterval(poll);
      done = true;
    }, 8000);
  }

  function connectWS() {
    window.clearTimeout(reconnectTimer);
    intentionalClose = false;
    setConnected(false);
    // A connect attempt is "loading" before any content and "syncing" after:
    // the editor keeps working and edits are queued locally meanwhile. The
    // red "disconnected" state is reserved for an actual drop (see onclose).
    setStatus(initialSnapshotApplied ? "syncing" : "pending");

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
      // The first attempt failing while content is already painted is still a
      // retry-in-progress, not a hard failure; later drops are real errors.
      setStatus(everConnected ? "error" : initialSnapshotApplied ? "syncing" : "pending");
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

  // An edit-locked room is readable but not writable without the password.
  // Gate it before the first keystroke instead of after the server rejects the
  // batch: a password prompt that appears mid-sentence, with the typed text
  // left in the editor, is what made the old flow feel broken.
  //
  // A password this browser already holds is verified silently first, so the
  // room owner (who set it here) never sees the gate. A stale copy from a
  // rotated password fails that check and falls back to the panel.
  function syncEditLockGate() {
    if (passwordScope !== "edit" || !editPasswordSet) {
      if (roomNeedsUnlock()) hideRoomLock();
      return;
    }
    var stored = getEditPassword();
    if (!stored) {
      showRoomLock();
      return;
    }
    // Whoever set the password in this browser already proved they know it;
    // re-prompting them on the spot would lock them out of their own room.
    // Only a password that predates this page load needs verifying.
    if (editPasswordJustSet) {
      hideRoomLock();
      return;
    }
    tryUnlock(stored).then(function (ok) {
      if (ok) {
        hideRoomLock();
        return;
      }
      setEditPassword("");
      rememberViewPassword("");
      showRoomLock();
    }).catch(function () {
      // Offline or server error: keep the room usable and let the normal
      // write path surface the failure, rather than blocking on a guess.
      hideRoomLock();
    });
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
    syncEditLockGate();
    if (data.passwordRequired) {
      // View-protected room, not authenticated: the server withheld the
      // content. The gate stays up (or is armed with a remembered password)
      // until the password is accepted.
      hideLoading();
      requestViewPassword();
      return;
    }
    if (content.readOnly && !roomNeedsUnlock()) content.readOnly = false;
    var localText = content.value;
    var previousSyncedText = lastSyncedText;
    var hadPending = pendingOps.length > 0;
    // Text present on the very first snapshot with no ops behind it was typed
    // before this script finished loading (or restored by the browser). The
    // snapshot must not silently discard it.
    var untypedDraft =
      !initialSnapshotApplied && !hadPending && previousSyncedText === "" ? localText : "";
    // Capture before replacing the doc so caret can stick to the same characters.
    var sel = captureSelection();

    // Always adopt server CRDT tree (authoritative snapshot).
    doc = new CRDT.Doc();
    if (data.itemsSite) {
      // Server elided a linear-chain item list: rebuild the identical ids.
      doc = CRDT.buildFromString(data.itemsSite, data.content || "") || new CRDT.Doc();
    } else if (data.items && data.items.length) {
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
    // Only re-diff when there really are unsynced local edits (replay failed),
    // or when the very first snapshot would overwrite pre-existing text.
    // Without either, the textarea is just a stale view — re-diffing it would
    // revert the very change this snapshot delivers.
    if (!replayed && localText !== serverText && (hadPending || untypedDraft !== "")) {
      var rebasedText = localText;
      if (previousSyncedText && previousSyncedText !== localText) {
        rebasedText = merge3(previousSyncedText, localText, serverText);
      }
      if (rebasedText !== serverText) applyLocalText(rebasedText);
    }
    applyingRemote = true;
    setContentValue(doc.materialize(), sel);
    applyingRemote = false;
    hideLoading();
    refreshRemoteCursorPositions();
    reanchorRemoteCursorsFromPos();
    if (data.ttlSeconds) setTTLControls(data.ttlSeconds);
    renderCursors();
    updateEmptyHint();
    scheduleCursorSend();
    if (pendingOps.length) {
      scheduleFlush();
    } else {
      setIdleStatus();
    }
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
    setIdleStatus();
  }

  // Direct per-batch ack from the server. Broadcast echoes can be coalesced
  // into snapshots (or skipped entirely for idempotent re-applies), so this is
  // the authoritative signal that a sent batch is durable.
  function handleAck(msg) {
    var batch = sentBatches[msg.seq || 0];
    var passwordNeeded =
      msg.error === "edit password required" || msg.error === "view password required";
    var invalidView = msg.error === "invalid view password";

    if (msg.error && (passwordNeeded || invalidView)) {
      // Keep ops in pendingOps so a concurrent state frame re-diffs them and
      // we can re-flush after unlock. Only clear the in-flight batch marker.
      if (batch) {
        delete sentBatches[msg.seq || 0];
      }
      if (invalidView) {
        onInvalidViewPassword();
        return;
      }
      // A rejected batch means the remembered password is wrong or absent:
      // show the gate rather than a modal over content the user just typed.
      setEditPassword("");
      showRoomLock();
      showRoomLockError(t("密码错误，请重试"));
      return;
    }

    if (batch) {
      delete sentBatches[msg.seq || 0];
      pendingOps = pendingOps.filter(function (op) {
        return !(op && op.id && batch.ids[opKey(op)]);
      });
    }
    if (msg.error) {
      // Batch rejected (capacity/validation): ops dropped above; server
      // follows up with a state snapshot to converge on.
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

  // Tab indents instead of leaving the editor: this is a plain-text clipboard
  // people paste code into, and Tab-to-unfocus made pasting indented snippets
  // awkward. Escape then Tab still leaves, which is the documented way out.
  content.addEventListener("keydown", function (e) {
    if (e.key !== "Tab" || e.shiftKey || e.ctrlKey || e.metaKey || e.altKey) return;
    e.preventDefault();
    var start = content.selectionStart;
    var end = content.selectionEnd;
    var value = content.value;
    if (start === end) {
      content.value = value.slice(0, start) + "  " + value.slice(end);
      content.setSelectionRange(start + 2, start + 2);
    } else {
      // Indent each line touched by the selection.
      var lineStart = value.lastIndexOf("\n", start - 1) + 1;
      var block = value.slice(lineStart, end);
      var indented = block.replace(/^/gm, "  ");
      content.value = value.slice(0, lineStart) + indented + value.slice(end);
      content.setSelectionRange(lineStart, lineStart + indented.length);
    }
    onInput();
  });

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
    updateEmptyHint();
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
    if (!checkTTL()) return;
    scheduleFlush();
  }

  // Invalid TTL used to surface only as a red "error" in the corner, with no
  // hint about which field was wrong. Mark the control itself and say why,
  // once per invalid streak so holding a key down does not spam toasts.
  function checkTTL() {
    var ttlSeconds = readTTLSeconds();
    var group = ttlValue.parentNode;
    if (ttlSeconds) {
      ttlInvalidNotified = false;
      group.removeAttribute("data-invalid");
      ttlValue.removeAttribute("aria-invalid");
      return true;
    }
    group.setAttribute("data-invalid", "true");
    ttlValue.setAttribute("aria-invalid", "true");
    if (!ttlInvalidNotified) {
      ttlInvalidNotified = true;
      showToast(t("有效期需为大于 0 的整数"), "warn");
    }
    return false;
  }

  function scheduleFlush() {
    // Show "saving" the moment a keystroke is queued, so the status pill
    // reflects work in progress instead of snapping between idle states.
    if (connected) setStatus("saving");
    window.clearTimeout(flushTimer);
    flushTimer = window.setTimeout(function () {
      flushTimer = 0;
      flushPending();
    }, flushDelayMs);
  }

  function flushPending() {
    var ttlSeconds = readTTLSeconds();
    if (!ttlSeconds) {
      checkTTL();
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
          password: roomPassword() || undefined
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
        checkTTL();
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
    var pw = roomPassword();
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
    syncEditLockGate();
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
    hideLoading();
    shiftRemoteCursorsByText(oldText, merged);
    renderCursors();
    updateEmptyHint();
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
      fetch(apiURL, { headers: withRoomPasswordHeaders({ Accept: "application/json" }) })
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
    fetch(apiURL, { headers: withRoomPasswordHeaders({ Accept: "application/json" }) })
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
        if (!connected) {
          // Nothing renderable arrived; stop pretending to load.
          hideLoading();
          setStatus("error");
        }
      });
  }

  function applyLoadedSnapshot(data, force) {
    // REST has no CRDT items — rebuild from content; WS state will refine.
    //
    // The WS snapshot is authoritative, so a REST response that loses the race
    // is normally dropped. Before the first paint, though, dropping it means
    // waiting out the whole WS handshake + state frame while a perfectly good
    // copy of the text is already in hand. Paint it when nothing is unsaved
    // and no composition is in flight; the WS snapshot then re-adopts the
    // authoritative ids over the same text (setContentValue no-ops on equal
    // text, so the caret does not move).
    // A room with unsynced local text must not be repainted from server data:
    // that would discard what the person just typed while the load was in
    // flight. Only paint early when the editor still matches the server copy.
    var localDirty = content.value !== "" && content.value !== lastSyncedText;
    var canPaintEarly =
      !initialSnapshotApplied && !pendingOps.length && !compositionIsActive() && !localDirty;
    if (
      !force &&
      !canPaintEarly &&
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
    syncEditLockGate();
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
    // WS is the authoritative first load, but when the socket cannot connect
    // this REST snapshot is all the user gets — do not leave it behind an
    // overlay forever.
    hideLoading();
    setTTLControls(data.ttlSeconds || 3600);
    updateMeta(data);
    renderCursors();
    updateEmptyHint();
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
    if (data.expiresAt) {
      lastExpiresAt = data.expiresAt;
      renderExpires(data.expiresAt);
    } else {
      lastExpiresAt = null;
      expiresText.textContent = t(data.exists ? "已保存" : "尚未保存");
    }
    announceFirstSave(!!data.exists || !!data.expiresAt);
  }

  // The very first save in a brand-new room is the moment the link becomes
  // worth sharing, but the only feedback was a small line of text. Show it
  // once per room, then stay quiet so later keystrokes are not interrupted.
  function announceFirstSave(exists) {
    if (!initialStateSeen) {
      initialStateSeen = true;
      // The room already had content when this page opened: nothing was
      // "just saved", so there is nothing to announce.
      if (exists) {
        firstSaveAnnounced = true;
        return;
      }
    }
    if (firstSaveAnnounced || !exists) return;
    firstSaveAnnounced = true;
    showToast(t("已保存，复制链接就能分享给其他人"), "ok");
  }
  function renderExpires(expiresAt) {
    var d = new Date(expiresAt);
    if (Number.isNaN(d.getTime())) {
      expiresText.textContent = "—";
      expiresText.removeAttribute("data-tone");
      expiresText.removeAttribute("title");
      return;
    }
    var ms = d.getTime() - Date.now();
    // Absolute timestamp is the authoritative fact; the countdown is what
    // people actually scan for. Lead with the countdown and keep the exact
    // moment in the tooltip + a short suffix.
    var clock = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
    if (ms <= 0) {
      expiresText.textContent = t("已过期");
      expiresText.setAttribute("data-tone", "expired");
      expiresText.title = d.toLocaleString();
      return;
    }
    expiresText.textContent = tf("剩余 {0} · {1} 过期", formatDuration(ms), clock);
    expiresText.title = t("过期 ") + d.toLocaleString();
    // Under ten minutes the countdown earns attention; under one it is urgent.
    expiresText.setAttribute("data-tone", ms < 60000 ? "urgent" : ms < 600000 ? "soon" : "normal");
  }

  function formatDuration(ms) {
    var s = Math.ceil(ms / 1000);
    if (s < 60) return s + t("秒");
    if (s < 3600) return Math.ceil(s / 60) + t("分钟");
    if (s < 86400) {
      var h = s / 3600;
      return (h < 10 ? h.toFixed(1) : Math.round(h)) + t("小时");
    }
    var days = s / 86400;
    return (days < 10 ? days.toFixed(1) : Math.round(days)) + t("天");
  }

  function shortId(id) {
    return String(id || "").slice(0, 4);
  }

  function setIdleStatus() {
    // A live socket with no snapshot yet means the first load is still in
    // flight — keep saying so instead of claiming "live" over an empty editor.
    if (connected && !initialSnapshotApplied) {
      setStatus("pending");
      return;
    }
    // Being connected is not the headline while the room still needs a
    // password; the lock is what the user can act on.
    if (roomNeedsUnlock()) {
      setStatus("locked");
      return;
    }
    if (connected) {
      setStatus("live");
      return;
    }
    // Never-connected-yet is a loading state; only a socket that was open and
    // then dropped is a real failure.
    setStatus(everConnected || !initialSnapshotApplied ? "syncing" : "error");
  }

  function updatePeers() {
    if (!peerTabs) return;
    peerTabs.innerHTML = "";
    // CSS-rendered labels (::before / ::after) cannot be translated in place,
    // so they read their text from attributes refreshed here.
    peerTabs.setAttribute("data-empty-label", t("离线"));
    peerTabs.setAttribute("data-loading-label", t("连接中…"));
    if (connected || initialSnapshotApplied) {
      peerTabs.removeAttribute("data-state");
    } else {
      peerTabs.setAttribute("data-state", "loading");
    }

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
    if (isSelf) el.setAttribute("data-self-label", t("我"));
    return el;
  }

  function setConnected(isOn) {
    connected = !!isOn;
    if (connected) everConnected = true;
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

  // The editor chip and "connecting" peer label track the first snapshot only:
  // once any snapshot has been applied the room renders itself, so later
  // reconnects must not blank the content the user may be reading or editing.
  var initialSnapshotApplied = false;
  var loadingSlowTimer = 0;

  // Status has four states: pending (nothing painted yet), syncing (content
  // is on screen but the live socket has not finished its first handshake),
  // live, error.
  // The REST snapshot can paint long before the WebSocket handshake finishes;
  // reporting that window as "disconnected" showed a red failure state on a
  // perfectly healthy load.
  function setStatus(kind) {
    // The pill carries a dot for a glanceable state; the text follows the UI
    // language. Writing to the inner <span> keeps the dot node in place.
    var label = kind === "live" ? t("已同步")
      : kind === "saving" ? t("保存中…")
        : kind === "locked" ? t("已锁定")
          : kind === "pending" ? t("加载中…")
            : kind === "syncing" ? t("连接中…")
              : t("已断开");
    if (statusText) {
      statusText.textContent = label;
    } else {
      status.textContent = label;
    }
    if (kind === "live") {
      status.setAttribute("data-tone", "ok");
      status.title = t("已连接 · 修改会实时同步给所有打开此房间的人");
      return;
    }
    if (kind === "locked") {
      status.setAttribute("data-tone", "locked");
      status.title = t("房间已锁定 · 输入密码后才能查看和编辑");
      return;
    }
    if (kind === "saving" || kind === "syncing") {
      status.setAttribute("data-tone", "pending");
      status.title = kind === "saving"
        ? t("正在保存修改")
        : t("内容已加载，正在建立实时连接");
      return;
    }
    if (kind === "pending") {
      status.setAttribute("data-tone", "pending");
      status.title = "";
      return;
    }
    status.setAttribute("data-tone", "err");
    status.title = t("连接已断开 · 正在重试，修改会先保存在本地");
  }

  // The loading chip is visible from first paint (see index.html) and is
  // hidden by hideLoading once a snapshot lands. It tracks the first snapshot
  // only: later reconnects must not cover content the user is reading.

  function hideLoading() {
    initialSnapshotApplied = true;
    window.clearTimeout(loadingSlowTimer);
    if (loadingOverlay) loadingOverlay.hidden = true;
    updatePeers();
  }

  // A slow first snapshot (big room, weak link) keeps the chip honest instead
  // of leaving it spinning silently: after 8s say it is still working.
  function armLoadingSlowHint() {
    window.clearTimeout(loadingSlowTimer);
    loadingSlowTimer = window.setTimeout(function () {
      if (initialSnapshotApplied || !loadingText) return;
      loadingText.textContent = t("加载超时，正在重试…");
    }, 8000);
  }

  // --- Toast notifications (sonner-style, top-center) -------------------------

  var toastWrap = null;
  // Lucide (ISC) glyphs, inlined.
  var TOAST_ICON_OK =
    '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 6 9 17l-5-5"/></svg>';
  var TOAST_ICON_ERR =
    '<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="10"/><line x1="12" x2="12" y1="8" y2="12"/><line x1="12" x2="12.01" y1="16" y2="16"/></svg>';
  var TOAST_ICON_WARN =
    '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3"/><path d="M12 9v4"/><path d="M12 17h.01"/></svg>';
  var TOAST_ICON_CLOSE =
    '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>';

  function ensureToastWrap() {
    if (toastWrap && toastWrap.parentNode) return toastWrap;
    toastWrap = document.createElement("div");
    toastWrap.className = "toast-wrap";
    toastWrap.setAttribute("aria-live", "polite");
    document.body.appendChild(toastWrap);
    return toastWrap;
  }

  // tone: "ok" | "warn" | "err" (default).
  function showToast(msg, tone) {
    var wrap = ensureToastWrap();
    var kind = tone === "ok" ? "ok" : tone === "warn" ? "warn" : "err";
    var el = document.createElement("div");
    el.className = "toast " + kind;
    el.setAttribute("role", kind === "err" ? "alert" : "status");

    var icon = document.createElement("span");
    icon.className = "toast-icon";
    icon.innerHTML =
      kind === "ok" ? TOAST_ICON_OK : kind === "warn" ? TOAST_ICON_WARN : TOAST_ICON_ERR;

    var text = document.createElement("span");
    text.className = "toast-text";
    text.textContent = msg;

    var close = document.createElement("button");
    close.type = "button";
    close.className = "toast-close";
    close.innerHTML = TOAST_ICON_CLOSE;
    close.setAttribute("aria-label", t("关闭"));
    close.addEventListener("click", function () {
      dismissToast(el);
    });

    el.appendChild(icon);
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
    }, 180);
  }

  // --- File paste / drag / password modal ------------------------------------

  var modalState = null; // { resolve, reject, busy }
  var dragDepth = 0;

  function setupPasswordModal() {
    if (!passwordModal) return;
    if (confirmCancel) {
      confirmCancel.addEventListener("click", function () {
        closeConfirm(false);
      });
    }
    if (confirmOk) {
      confirmOk.addEventListener("click", function () {
        closeConfirm(true);
      });
    }
    if (confirmModal) {
      confirmModal.addEventListener("click", function (e) {
        if (e.target && e.target.closest && e.target.closest("[data-modal-dismiss]")) {
          closeConfirm(false);
        }
      });
    }
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
      if (e.target && e.target.closest && e.target.closest("[data-modal-dismiss]")) {
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
      // Uploading writes to the room, which a read-only visitor may not do.
      if (blockWhenReadOnly()) return;
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
      roomTitle.title = t("房间 ") + key + t(" · 文件上传已开启（三击切换）");
      roomTitle.classList.add("upload-on");
    } else {
      roomTitle.title = t("房间 ") + key + t(" · 文件上传已关闭（三击管理员开关）");
      roomTitle.classList.remove("upload-on");
    }
  }

  // --- Empty-room hint ------------------------------------------------------

  // The textarea placeholder explains what the box is; a brand-new room really
  // needs "paste here, then share the link". The hint disappears as soon as
  // the room has any content, and never blocks clicks into the editor.
  function updateEmptyHint() {
    if (!emptyHint) return;
    var empty = !doc.materialize() && !(content && content.value);
    // Never compete with the unlock gate: "paste here" makes no sense while
    // the room is still locked.
    var show = empty && !previewVisible && !roomNeedsUnlock();
    emptyHint.hidden = !show;
    if (appRoot) appRoot.classList.toggle("has-empty-hint", show);
  }

  function setupRoomLock() {
    if (roomLockForm) {
      roomLockForm.addEventListener("submit", submitRoomLock);
    }
    if (roomLockSubmit) {
      roomLockSubmit.addEventListener("click", submitRoomLock);
    }
    if (unlockEditBtn) {
      unlockEditBtn.addEventListener("click", promptEditUnlock);
    }
  }

  function setupEmptyHint() {
    if (emptyPasteBtn) {
      emptyPasteBtn.addEventListener("click", function () {
        if (!content) return;
        content.focus({ preventScroll: true });
        // Clipboard read needs a user gesture + permission; fall back to
        // focusing the editor so a manual Cmd+V works right away.
        if (navigator.clipboard && navigator.clipboard.readText) {
          navigator.clipboard.readText().then(function (text) {
            if (!text) return;
            content.focus({ preventScroll: true });
            document.execCommand("insertText", false, text);
          }).catch(function () {
            showToast(t("浏览器未授权读取剪贴板，请按 Ctrl/⌘+V 粘贴"), "warn");
          });
        } else {
          showToast(t("请按 Ctrl/⌘+V 粘贴"), "warn");
        }
      });
    }
    if (emptyShareBtn) {
      emptyShareBtn.addEventListener("click", function () {
        copyText(location.origin + location.pathname, emptyShareBtn);
      });
    }
  }

  function setupRoomTitleToggle() {
    if (!roomTitle) return;
    if (copyRoomBtn) {
      copyRoomBtn.addEventListener("click", function () {
        copyText(location.origin + location.pathname, copyRoomBtn);
      });
    }
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
        headers: withRoomPasswordHeaders({
          Accept: "application/json",
          "Content-Type": "application/json",
          "X-Admin-Password": adminPassword
        }),
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
            if (result.data && result.data.passwordScope === "view") {
              throw new Error(t("房间密码错误，请重试"));
            }
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
                headers: withRoomPasswordHeaders({
                  Accept: "application/json",
                  "Content-Type": "application/json",
                  "X-Admin-Password": pw
                }),
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
    fetch(filesAPIURL, { headers: withRoomPasswordHeaders({ Accept: "application/json" }) })
      .then(function (response) {
        if (response.status === 401 || response.status === 403) {
          // Normal while the room gate is up: the server withholds everything
          // until the password arrives. Retried after unlock.
          return null;
        }
        if (!response.ok) throw new Error("HTTP " + response.status);
        return response.json();
      })
      .then(function (data) {
        if (!data) return;
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

  // --- Generic confirm dialog ------------------------------------------------
  // window.confirm() looked nothing like the rest of the UI and blocked the
  // main thread; this mirrors the password dialog's styling and flow.
  var confirmState = null;

  function confirmAction(options) {
    options = options || {};
    return new Promise(function (resolve) {
      if (!confirmModal) {
        resolve(window.confirm(options.message || ""));
        return;
      }
      if (confirmState) {
        var prev = confirmState;
        confirmState = null;
        prev.resolve(false);
      }
      confirmState = { resolve: resolve };
      if (confirmTitle) confirmTitle.textContent = options.title || "";
      if (confirmMessage) confirmMessage.textContent = options.message || "";
      if (confirmOk) {
        confirmOk.textContent = options.confirmLabel || t("确认");
        confirmOk.className = options.danger ? "danger-solid" : "primary";
      }
      if (confirmCancel) confirmCancel.textContent = options.cancelLabel || t("取消");
      confirmModal.hidden = false;
    });
  }

  function closeConfirm(value) {
    if (!confirmState) {
      if (confirmModal) confirmModal.hidden = true;
      return;
    }
    var resolve = confirmState.resolve;
    confirmState = null;
    if (confirmModal) confirmModal.hidden = true;
    resolve(value);
  }

  function promptUploadFiles(fileListLike) {
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

    var headers = withRoomPasswordHeaders({ Accept: "application/json" });
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
      if (result.status === 401) {
        if (result.data && result.data.passwordScope === "view") {
          throw new Error(t("房间密码错误，请重试"));
        }
        throw new Error(t("管理员密码错误"));
      }
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
        headers: withRoomPasswordHeaders({ "X-File-Password": password })
      })
        .then(function (response) {
          if (response.status === 401) {
            // Could be room view password or per-file password; clear file
            // cache only when the body does not indicate view scope.
            return response.json().then(function (data) {
              if (data && data.passwordScope === "view") {
                throw new Error(t("房间密码错误，请重试"));
              }
              rememberFilePassword("");
              throw new Error(t("文件密码错误"));
            }).catch(function (e) {
              if (e && e.message) throw e;
              rememberFilePassword("");
              throw new Error(t("文件密码错误"));
            });
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
        headers: withRoomPasswordHeaders({
          Accept: "application/json",
          "Content-Type": "application/json",
          "X-Admin-Password": password
        }),
        body: JSON.stringify({ adminPassword: password })
      })
        .then(function (response) {
          if (response.status === 401) {
            return response.json().then(function (data) {
              if (data && data.passwordScope === "view") {
                throw new Error(t("房间密码错误，请重试"));
              }
              rememberAdminPassword("");
              throw new Error(t("管理员密码错误"));
            }).catch(function (e) {
              if (e && e.message) throw e;
              rememberAdminPassword("");
              throw new Error(t("管理员密码错误"));
            });
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
                headers: withRoomPasswordHeaders({
                  Accept: "application/json",
                  "Content-Type": "application/json",
                  "X-Admin-Password": pw
                }),
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
