(() => {
  "use strict";

  const $ = (id) => document.getElementById(id);
  const identityKey = "pasadatos.identity.v1";
  const historyKey = "pasadatos.history.v1";
  const $all = (selector) => Array.from(document.querySelectorAll(selector));

  let identity = loadIdentity();
  let peers = [];
  let inbox = [];
  let selectedFiles = [];
  let queue = [];
  let pairing = null;
  let pairingJoinURL = "";
  let online = false;
  let pollTimer = null;
  let deferredInstallPrompt = null;
  let lastQR = "";

  function randomToken(bytes = 32) {
    const buffer = new Uint8Array(bytes);
    crypto.getRandomValues(buffer);
    let binary = "";
    buffer.forEach((value) => { binary += String.fromCharCode(value); });
    return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
  }

  function defaultName() {
    const ua = navigator.userAgent.toLowerCase();
    if (/iphone|ipad|ipod/.test(ua)) return "Mi iPhone";
    if (/android/.test(ua)) return "Mi Android";
    return "Mi celular";
  }

  function platformName() {
    const ua = navigator.userAgent.toLowerCase();
    if (/iphone|ipad|ipod/.test(ua)) return "ios";
    if (/android/.test(ua)) return "android";
    return "web";
  }

  function loadIdentity() {
    try {
      const stored = JSON.parse(localStorage.getItem(identityKey) || "null");
      if (stored?.id && stored?.token && stored?.name) return stored;
    } catch { /* create a new identity */ }
    const value = { id: `device_${randomToken(16)}`, token: randomToken(32), name: defaultName(), platform: platformName() };
    localStorage.setItem(identityKey, JSON.stringify(value));
    return value;
  }

  function saveIdentity() {
    localStorage.setItem(identityKey, JSON.stringify(identity));
  }

  function getHistory() {
    try {
      const list = JSON.parse(localStorage.getItem(historyKey) || "[]");
      return Array.isArray(list) ? list : [];
    } catch { return []; }
  }

  function addHistory(item) {
    const list = getHistory();
    list.unshift({ id: `hist_${randomToken(8)}`, at: new Date().toISOString(), ...item });
    localStorage.setItem(historyKey, JSON.stringify(list.slice(0, 500)));
    renderHistory();
  }

  function updateHistory(transferID, patch) {
    const list = getHistory();
    const index = list.findIndex((item) => item.transfer_id === transferID);
    if (index >= 0) list[index] = { ...list[index], ...patch };
    localStorage.setItem(historyKey, JSON.stringify(list));
    renderHistory();
  }

  async function api(path, options = {}) {
    const headers = new Headers(options.headers || {});
    headers.set("X-Device-ID", identity.id);
    headers.set("Authorization", `Bearer ${identity.token}`);
    if (options.body && !(options.body instanceof Blob) && typeof options.body !== "string") {
      headers.set("Content-Type", "application/json");
      options.body = JSON.stringify(options.body);
    }
    const response = await fetch(path, { ...options, headers, cache: "no-store" });
    const type = response.headers.get("content-type") || "";
    const payload = type.includes("application/json") ? await response.json().catch(() => null) : null;
    if (!response.ok) throw new Error(payload?.message || payload?.error || `Error ${response.status}`);
    return payload || { ok: true };
  }

  function escapeHTML(value) {
    return String(value ?? "").replace(/[&<>'"]/g, (char) => ({
      "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;"
    })[char]);
  }

  function icon(id) {
    return `<span class="icon"><svg><use href="#${id}"></use></svg></span>`;
  }

  function extension(name) {
    const parts = String(name || "").split(".");
    return parts.length > 1 ? parts.pop().slice(0, 4) : "FILE";
  }

  function formatBytes(bytes) {
    const value = Number(bytes || 0);
    if (!Number.isFinite(value) || value <= 0) return "0 B";
    const units = ["B", "KB", "MB", "GB", "TB"];
    const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
    const amount = value / Math.pow(1024, index);
    return `${amount >= 100 || index === 0 ? amount.toFixed(0) : amount.toFixed(1)} ${units[index]}`;
  }

  function formatDate(value) {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "—";
    return new Intl.DateTimeFormat("es-AR", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" }).format(date);
  }

  function relativeExpiry(value) {
    if (!value) return "";
    const seconds = Math.max(0, Math.round((new Date(value).getTime() - Date.now()) / 1000));
    if (seconds <= 0) return "El código venció. Generá uno nuevo.";
    return `Vence en ${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
  }

  function statusLabel(status) {
    return ({ pending: "Preparando", uploading: "Enviando", ready: "Disponible", delivered: "Recibido", error: "Error", cancelled: "Cancelado" })[status] || status;
  }

  function toast(message, type = "") {
    const node = document.createElement("div");
    node.className = `toast ${type}`;
    node.textContent = message;
    $("toastStack").appendChild(node);
    setTimeout(() => node.remove(), 4200);
  }

  function showView(view) {
    $all(".mobile-nav-btn").forEach((button) => button.classList.toggle("active", button.dataset.mobileView === view));
    $all(".mobile-view").forEach((section) => section.classList.toggle("active", section.id === `mobile-${view}`));
    if (view === "inbox") refreshData();
    window.scrollTo({ top: 0, behavior: "smooth" });
  }

  function setOnline(value) {
    online = value;
    $("mobileStatus").querySelector(".status-dot").className = `status-dot ${value ? "online" : "warn"}`;
    $("mobileStatus").title = value ? "Conectado" : "Sin conexión";
  }

  async function register() {
    const response = await fetch(`/api/v1/devices/${encodeURIComponent(identity.id)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token: identity.token, name: identity.name, platform: identity.platform })
    });
    const payload = await response.json().catch(() => null);
    if (!response.ok) throw new Error(payload?.message || "No se pudo registrar el dispositivo");
    setOnline(true);
  }

  async function joinFromURL() {
    const params = new URLSearchParams(location.search);
    const code = params.get("pair");
    if (!code) return;
    try {
      const response = await api("/api/v1/pairings/join", { method: "POST", body: { code } });
      toast(`Vinculado con ${response.peer?.name || "el dispositivo"}.`, "success");
      history.replaceState({}, "", "/");
      showView("send");
    } catch (error) {
      toast(error.message, "error");
      showView("devices");
      $("mobileJoinCode").value = code;
    }
  }

  async function refreshData() {
    try {
      const [peerResponse, inboxResponse] = await Promise.all([
        api("/api/v1/me/peers"),
        api("/api/v1/transfers?box=inbox&limit=100")
      ]);
      peers = peerResponse.peers || [];
      inbox = inboxResponse.transfers || [];
      setOnline(true);
      renderAll();
    } catch (error) {
      setOnline(false);
      if (!navigator.onLine) return;
      console.warn(error);
    }
  }

  function renderAll() {
    renderSelected();
    renderPeers();
    renderInbox();
    renderQueue();
    renderPairing();
    renderHistory();
    renderSettings();
  }

  function renderSelected() {
    $("mobileSelected").innerHTML = selectedFiles.map((file) => `
      <div class="file-row"><div class="file-icon">${escapeHTML(extension(file.name))}</div><div class="truncate"><div class="file-name truncate">${escapeHTML(file.name)}</div><div class="file-meta">${formatBytes(file.size)}</div></div></div>`).join("");
    updateSendButton();
  }

  function renderPeers() {
    const select = $("mobilePeer");
    const previous = select.value;
    select.innerHTML = peers.length
      ? `<option value="">Elegí un dispositivo</option>${peers.map((peer) => `<option value="${escapeHTML(peer.id)}">${escapeHTML(peer.name)}${peer.online ? " · disponible" : ""}</option>`).join("")}`
      : `<option value="">Vinculá un dispositivo primero</option>`;
    if (peers.some((peer) => peer.id === previous)) select.value = previous;
    const host = $("mobileDeviceList");
    if (!peers.length) {
      host.innerHTML = `<div class="empty-state" style="padding:18px"><span class="muted">No hay dispositivos vinculados.</span></div>`;
    } else {
      host.innerHTML = peers.map((peer) => `
        <div class="mobile-peer"><div class="device-avatar">${icon(peer.platform === "windows" ? "i-pc" : "i-device")}</div><div class="truncate"><div class="peer-name truncate">${escapeHTML(peer.name)}</div><div class="peer-meta"><span class="status-dot ${peer.online ? "online" : ""}" style="display:inline-block;margin-right:6px"></span>${peer.online ? "Disponible" : "Sin conexión reciente"}</div></div><button class="btn btn-danger btn-sm" data-mobile-unlink="${escapeHTML(peer.id)}">Quitar</button></div>`).join("");
    }
    updateSendButton();
  }

  function renderInbox() {
    const ready = inbox.filter((item) => item.status === "ready");
    const host = $("inboxList");
    if (!ready.length) {
      host.innerHTML = `<div class="card mobile-card"><div class="empty-state"><div class="drop-icon">${icon("i-inbox")}</div><strong>No hay archivos pendientes</strong><div class="card-copy">Cuando te envíen algo, aparecerá acá.</div></div></div>`;
      return;
    }
    host.innerHTML = ready.map((item) => `
      <div class="inbox-item"><div class="inbox-top"><div class="file-icon">${escapeHTML(extension(item.filename))}</div><div class="inbox-info"><div class="inbox-name">${escapeHTML(item.filename)}</div><div class="inbox-meta">De ${escapeHTML(item.sender_display_name || "dispositivo")} · ${formatBytes(item.size)} · ${formatDate(item.created_at)}</div></div><span class="state-badge ready">Listo</span></div><div class="inbox-actions"><button class="btn btn-cyan" data-download="${escapeHTML(item.id)}">${icon("i-download")}Descargar</button></div></div>`).join("");
  }

  function renderQueue() {
    const host = $("mobileQueue");
    if (!queue.length) {
      host.innerHTML = `<div class="empty-state" style="padding:14px"><span class="muted">No hay transferencias activas.</span></div>`;
      return;
    }
    host.innerHTML = queue.slice(0, 20).map((item) => {
      const percent = item.size > 0 ? Math.max(0, Math.min(100, Math.round((item.done || 0) * 100 / item.size))) : (item.status === "ready" ? 100 : 0);
      return `<div class="transfer-row"><div class="file-icon">${escapeHTML(extension(item.filename))}</div><div class="truncate"><div class="transfer-title"><span class="truncate">${escapeHTML(item.filename)}</span><span>${percent}%</span></div><div class="progress"><span style="width:${percent}%"></span></div><div class="transfer-sub"><span>${escapeHTML(statusLabel(item.status))}</span><span>${formatBytes(item.done)} / ${formatBytes(item.size)}</span></div>${item.error ? `<div class="file-meta" style="color:var(--red)">${escapeHTML(item.error)}</div>` : ""}</div></div>`;
    }).join("");
  }

  function renderPairing() {
    $("mobilePairCode").textContent = pairing?.code || "— — — — —";
    $("mobilePairExpiry").textContent = pairing ? relativeExpiry(pairing.expires_at) : "Todavía no generaste un código.";
    $("mobileCopyPair").disabled = !pairingJoinURL;
    const qr = $("mobilePairQR");
    if (pairingJoinURL && pairingJoinURL !== lastQR) {
      qr.classList.remove("hidden");
      qr.innerHTML = "";
      try {
        new QRCode(qr, { text: pairingJoinURL, width: 146, height: 146, colorDark: "#07111f", colorLight: "#ffffff", correctLevel: QRCode.CorrectLevel.M });
        lastQR = pairingJoinURL;
      } catch { qr.classList.add("hidden"); }
    } else if (!pairingJoinURL) {
      qr.classList.add("hidden");
      qr.innerHTML = "";
      lastQR = "";
    }
  }

  function renderHistory() {
    const list = getHistory();
    const host = $("mobileHistoryList");
    if (!list.length) {
      host.innerHTML = `<div class="card mobile-card"><div class="empty-state"><div class="drop-icon">${icon("i-history")}</div><strong>Sin actividad todavía</strong><div class="card-copy">Tus envíos y descargas quedarán registrados acá.</div></div></div>`;
      return;
    }
    host.innerHTML = list.map((item) => `
      <div class="mobile-history-item"><div class="file-icon">${escapeHTML(extension(item.filename))}</div><div class="truncate"><div class="mobile-history-title">${escapeHTML(item.filename)}</div><div class="mobile-history-meta">${item.direction === "received" ? "Recibido de" : "Enviado a"} ${escapeHTML(item.peer_name || "dispositivo")} · ${formatBytes(item.size)} · ${formatDate(item.at)}</div></div><span class="state-badge ${escapeHTML(item.status || "ready")}">${escapeHTML(statusLabel(item.status || "ready"))}</span></div>`).join("");
  }

  function renderSettings() {
    if (document.activeElement !== $("mobileDeviceName")) $("mobileDeviceName").value = identity.name;
    $("mobileServer").textContent = location.origin;
    $("mobileServer").title = location.origin;
    const localHost = /^(localhost|127\.|10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.)/.test(location.hostname);
    $("mobileConnectionType").textContent = localHost ? "Wi‑Fi local" : (location.protocol === "https:" ? "Remoto por HTTPS" : "Servidor directo");
    $("mobileDeviceID").textContent = identity.id;
    $("mobileDeviceID").title = identity.id;
  }

  function updateSendButton() {
    $("mobileSend").disabled = !selectedFiles.length || !$("mobilePeer").value || queue.some((item) => item.status === "uploading");
  }

  function uploadContent(transfer, file, queueItem) {
    return new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest();
      xhr.open("PUT", `/api/v1/transfers/${encodeURIComponent(transfer.id)}/content`);
      xhr.setRequestHeader("X-Device-ID", identity.id);
      xhr.setRequestHeader("Authorization", `Bearer ${identity.token}`);
      xhr.setRequestHeader("Content-Type", file.type || "application/octet-stream");
      xhr.upload.onprogress = (event) => {
        queueItem.done = event.loaded;
        queueItem.status = "uploading";
        renderQueue();
      };
      xhr.onerror = () => reject(new Error("Se interrumpió la conexión durante el envío"));
      xhr.onabort = () => reject(new Error("Envío cancelado"));
      xhr.onload = () => {
        let payload = null;
        try { payload = JSON.parse(xhr.responseText || "null"); } catch { /* ignore */ }
        if (xhr.status >= 200 && xhr.status < 300) resolve(payload?.transfer || transfer);
        else reject(new Error(payload?.message || `Error ${xhr.status}`));
      };
      xhr.send(file);
    });
  }

  async function sendSelected() {
    const peerID = $("mobilePeer").value;
    const peer = peers.find((item) => item.id === peerID);
    if (!peerID || !selectedFiles.length) return;
    const files = [...selectedFiles];
    selectedFiles = [];
    renderSelected();
    for (const file of files) {
      const item = { id: `queue_${randomToken(6)}`, filename: file.name, size: file.size, done: 0, status: "pending" };
      queue.unshift(item);
      renderQueue();
      try {
        const response = await api("/api/v1/transfers", {
          method: "POST",
          body: { receiver_id: peerID, filename: file.name, size: file.size, mime: file.type || "application/octet-stream", source_label: "Almacenamiento del celular" }
        });
        const transfer = response.transfer;
        item.transfer_id = transfer.id;
        await uploadContent(transfer, file, item);
        item.done = file.size;
        item.status = "ready";
        addHistory({ transfer_id: transfer.id, direction: "sent", filename: file.name, size: file.size, peer_name: peer?.name || "dispositivo", status: "ready", source_path: "Almacenamiento del celular" });
        toast(`${file.name} quedó disponible.`, "success");
      } catch (error) {
        item.status = "error";
        item.error = error.message;
        addHistory({ transfer_id: item.transfer_id, direction: "sent", filename: file.name, size: file.size, peer_name: peer?.name || "dispositivo", status: "error", error: error.message });
        toast(error.message, "error");
      }
      renderQueue();
    }
    updateSendButton();
    refreshData();
  }

  async function downloadTransfer(transferID) {
    const transfer = inbox.find((item) => item.id === transferID);
    if (!transfer) return;
    try {
      const response = await api(`/api/v1/transfers/${encodeURIComponent(transferID)}/download-token`, { method: "POST", body: {} });
      const link = document.createElement("a");
      link.href = response.url;
      link.download = transfer.filename;
      link.rel = "noopener";
      document.body.appendChild(link);
      link.click();
      link.remove();
      await new Promise((resolve) => setTimeout(resolve, 500));
      await api(`/api/v1/transfers/${encodeURIComponent(transferID)}/received`, { method: "POST", body: { destination_label: "Descargas del dispositivo (ruta administrada por el sistema)" } });
      addHistory({ transfer_id: transfer.id, direction: "received", filename: transfer.filename, size: transfer.size, peer_name: transfer.sender_display_name || "dispositivo", status: "delivered", destination_path: "Descargas del dispositivo" });
      inbox = inbox.filter((item) => item.id !== transferID);
      renderInbox();
      toast("Descarga iniciada.", "success");
    } catch (error) { toast(error.message, "error"); }
  }

  async function createPair() {
    try {
      const response = await api("/api/v1/pairings", { method: "POST", body: {} });
      pairing = response.pairing;
      pairingJoinURL = response.join_url || `${location.origin}/?pair=${pairing.code}`;
      renderPairing();
      toast("Código generado.", "success");
    } catch (error) { toast(error.message, "error"); }
  }

  async function joinPair() {
    const code = $("mobileJoinCode").value.trim();
    if (!code) return toast("Ingresá el código de vinculación.", "error");
    try {
      const response = await api("/api/v1/pairings/join", { method: "POST", body: { code } });
      $("mobileJoinCode").value = "";
      toast(`Vinculado con ${response.peer?.name || "el dispositivo"}.`, "success");
      await refreshData();
      showView("send");
    } catch (error) { toast(error.message, "error"); }
  }

  $all(".mobile-nav-btn").forEach((button) => button.addEventListener("click", () => showView(button.dataset.mobileView)));
  $("mobileFiles").addEventListener("change", (event) => {
    selectedFiles = Array.from(event.target.files || []);
    renderSelected();
  });
  $("mobilePeer").addEventListener("change", updateSendButton);
  $("mobileSend").addEventListener("click", sendSelected);
  $("refreshInbox").addEventListener("click", async () => { await refreshData(); toast("Bandeja actualizada."); });
  $("inboxList").addEventListener("click", (event) => {
    const button = event.target.closest("[data-download]");
    if (button) downloadTransfer(button.dataset.download);
  });

  $("mobileCreatePair").addEventListener("click", createPair);
  $("mobileCopyPair").addEventListener("click", async () => {
    if (!pairingJoinURL) return;
    try { await navigator.clipboard.writeText(pairingJoinURL); toast("Enlace copiado.", "success"); }
    catch { toast("No se pudo copiar el enlace.", "error"); }
  });
  $("mobileJoin").addEventListener("click", joinPair);
  $("mobileJoinCode").addEventListener("input", (event) => {
    let value = event.target.value.toUpperCase().replace(/[^A-Z0-9]/g, "").slice(0, 10);
    if (value.length > 5) value = `${value.slice(0, 5)}-${value.slice(5)}`;
    event.target.value = value;
  });
  $("mobileJoinCode").addEventListener("keydown", (event) => { if (event.key === "Enter") joinPair(); });
  $("mobileDeviceList").addEventListener("click", async (event) => {
    const button = event.target.closest("[data-mobile-unlink]");
    if (!button) return;
    if (!confirm("¿Quitar este dispositivo?")) return;
    try { await api(`/api/v1/me/peers/${encodeURIComponent(button.dataset.mobileUnlink)}`, { method: "DELETE" }); await refreshData(); toast("Dispositivo desvinculado."); }
    catch (error) { toast(error.message, "error"); }
  });

  $("clearMobileHistory").addEventListener("click", () => {
    if (!confirm("¿Limpiar el historial de este dispositivo? Los archivos no se borrarán.")) return;
    localStorage.removeItem(historyKey);
    renderHistory();
  });
  $("saveMobileName").addEventListener("click", async () => {
    const name = $("mobileDeviceName").value.trim();
    if (!name) return toast("Ingresá un nombre para el dispositivo.", "error");
    identity.name = name.slice(0, 80);
    saveIdentity();
    try { await register(); await refreshData(); toast("Nombre guardado.", "success"); }
    catch (error) { toast(error.message, "error"); }
  });
  $("mobileStatus").addEventListener("click", () => toast(online ? `Conectado a ${location.host}` : "Sin conexión con el servidor", online ? "success" : "error"));

  window.addEventListener("beforeinstallprompt", (event) => {
    event.preventDefault();
    deferredInstallPrompt = event;
    $("installBanner").classList.remove("hidden");
  });
  $("installApp").addEventListener("click", async () => {
    if (deferredInstallPrompt) {
      deferredInstallPrompt.prompt();
      await deferredInstallPrompt.userChoice;
      deferredInstallPrompt = null;
      $("installBanner").classList.add("hidden");
      return;
    }
    showView("settings");
    $("iosInstallHelp").classList.remove("hidden");
  });
  window.addEventListener("appinstalled", () => $("installBanner").classList.add("hidden"));

  async function initInstallUI() {
    const isStandalone = matchMedia("(display-mode: standalone)").matches || navigator.standalone === true;
    const isIOS = /iphone|ipad|ipod/i.test(navigator.userAgent);
    if (!isStandalone && isIOS) {
      $("installBanner").classList.remove("hidden");
      $("installCopy").textContent = "Agregala a Inicio desde el menú Compartir de Safari.";
      $("installApp").textContent = "Ver cómo";
      $("iosInstallHelp").classList.remove("hidden");
    }
    if ("serviceWorker" in navigator && window.isSecureContext) {
      navigator.serviceWorker.register("/sw.js").catch(() => {});
    }
  }

  async function init() {
    renderAll();
    await initInstallUI();
    try {
      await register();
      await joinFromURL();
      await refreshData();
    } catch (error) {
      setOnline(false);
      toast(error.message, "error");
    }
    pollTimer = setInterval(refreshData, 3500);
  }

  window.addEventListener("online", refreshData);
  window.addEventListener("offline", () => setOnline(false));
  document.addEventListener("visibilitychange", () => { if (document.visibilityState === "visible") refreshData(); });
  window.addEventListener("beforeunload", () => clearInterval(pollTimer));

  init();
})();
