(function () {
  const key = decodeURIComponent(window.location.pathname.slice(1));
  const apiURL = `/api/clipboard/${encodeURIComponent(key)}`;
  const pollIntervalMs = 3000;

  const roomTitle = document.getElementById("roomTitle");
  const status = document.getElementById("status");
  const ttlValue = document.getElementById("ttlValue");
  const ttlUnit = document.getElementById("ttlUnit");
  const expiresText = document.getElementById("expiresText");
  const versionText = document.getElementById("versionText");
  const content = document.getElementById("content");
  const reloadButton = document.getElementById("reloadButton");
  const clearButton = document.getElementById("clearButton");

  let knownVersion = 0;
  let saving = false;
  let saveTimer = 0;
  let saveController = null;
  const autosaveDelayMs = 500;

  roomTitle.textContent = `/${key}`;
  content.addEventListener("input", scheduleSave);

  ttlValue.addEventListener("input", scheduleSave);
  ttlUnit.addEventListener("change", scheduleSave);

  reloadButton.addEventListener("click", () => load({ force: true }));
  clearButton.addEventListener("click", clearClipboard);

  load({ force: true });
  window.setInterval(() => load({ force: false }), pollIntervalMs);

  async function load(options) {
    try {
      const response = await fetch(apiURL, { headers: { "Accept": "application/json" } });
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      const data = await response.json();

      if (!options.force && data.version === knownVersion) {
        updateMeta(data);
        return;
      }

      applyRemote(data);
      setStatus(data.exists ? "远端已刷新" : "空剪贴板");
    } catch (error) {
      setStatus("连接失败");
    }
  }

  function scheduleSave() {
    window.clearTimeout(saveTimer);
    setStatus("等待自动保存");
    saveTimer = window.setTimeout(save, autosaveDelayMs);
  }

  async function save() {
    const ttlSeconds = readTTLSeconds();
    if (!ttlSeconds) {
      setStatus("TTL 无效");
      return;
    }

    saving = true;
    if (saveController) {
      saveController.abort();
    }
    saveController = new AbortController();
    setStatus("保存中");

    try {
      const response = await fetch(apiURL, {
        method: "PUT",
        headers: {
          "Accept": "application/json",
          "Content-Type": "application/json"
        },
        body: JSON.stringify({
          content: content.value,
          ttlSeconds
        }),
        signal: saveController.signal
      });
      const data = await response.json();
      if (!response.ok) {
        throw new Error(data.error || `HTTP ${response.status}`);
      }
      applyRemote(data);
      setStatus("自动保存完成");
    } catch (error) {
      if (error.name === "AbortError") {
        return;
      }
      setStatus(error.message || "保存失败");
    } finally {
      saving = false;
      saveController = null;
    }
  }

  async function clearClipboard() {
    window.clearTimeout(saveTimer);
    if (saveController) {
      saveController.abort();
      saveController = null;
    }
    clearButton.disabled = true;
    setStatus("清空中");

    try {
      const response = await fetch(apiURL, { method: "DELETE" });
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      knownVersion = 0;
      content.value = "";
      updateMeta({ version: 0, ttlSeconds: 3600, exists: false });
      setStatus("已清空");
    } catch (error) {
      setStatus("清空失败");
    } finally {
      clearButton.disabled = false;
    }
  }

  function applyRemote(data) {
    knownVersion = data.version;
    content.value = data.content || "";
    setTTLControls(data.ttlSeconds || 3600);
    updateMeta(data);
  }

  function updateMeta(data) {
    versionText.textContent = `v${data.version || 0}`;
    if (data.expiresAt) {
      const expiresAt = new Date(data.expiresAt);
      expiresText.textContent = `过期：${expiresAt.toLocaleString()}`;
    } else {
      expiresText.textContent = data.exists ? "已保存" : "未保存";
    }
  }

  function readTTLSeconds() {
    const value = Number(ttlValue.value);
    const unit = Number(ttlUnit.value);
    if (!Number.isSafeInteger(value) || value <= 0 || !Number.isSafeInteger(unit) || unit <= 0) {
      return 0;
    }
    const seconds = value * unit;
    return Number.isSafeInteger(seconds) && seconds > 0 ? seconds : 0;
  }

  function setTTLControls(seconds) {
    const units = [86400, 3600, 60];
    const unit = units.find((candidate) => seconds % candidate === 0) || 60;
    ttlUnit.value = String(unit);
    ttlValue.value = String(Math.max(1, Math.floor(seconds / unit)));
  }

  function setStatus(text) {
    if (!saving || text !== "等待自动保存") {
      status.textContent = text;
    }
  }
})();
