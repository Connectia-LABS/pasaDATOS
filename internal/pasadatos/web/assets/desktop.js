(() => {
  "use strict";

  const $ = (id) => document.getElementById(id);
  const tokenFromURL = new URLSearchParams(location.search).get("native");
  if (tokenFromURL) sessionStorage.setItem("pasadatos_native", tokenFromURL);
  const nativeToken = tokenFromURL || sessionStorage.getItem("pasadatos_native") || "";
  if (tokenFromURL) history.replaceState({}, "", "/desktop/");

  const app = $("desktopApp");
  const authError = $("authError");
  if (!nativeToken) {
    app.classList.add("hidden");
    authError.classList.remove("hidden");
    return;
  }

  let currentState = null;
  let currentView = "send";
  let lastQR = "";
  let historyFilter = "";
  let stateTimer = null;
  let busyState = false;

  const viewMeta = {
    send: ["Transferencia de archivos", "Enviar"],
    link: ["Conexiones seguras", "Dispositivos"],
    history: ["Registro local", "Historial"],
    settings: ["Preferencias", "Ajustes"]
  };

  async function nativeFetch(path, options = {}) {
    const headers = new Headers(options.headers || {});
    headers.set("X-pasaDATOS-Native", nativeToken);
    if (options.body && !(options.body instanceof Blob) && typeof options.body !== "string") {
      headers.set("Content-Type", "application/json");
      options.body = JSON.stringify(options.body);
    }
    const response = await fetch(path, { ...options, headers, cache: "no-store" });
    let payload = null;
    const type = response.headers.get("content-type") || "";
    if (type.includes("application/json")) {
      payload = await response.json().catch(() => null);
    }
    if (!response.ok) {
      throw new Error(payload?.message || payload?.error || `Error ${response.status}`);
    }
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

  function formatBytes(bytes) {
    const value = Number(bytes || 0);
    if (!Number.isFinite(value) || value <= 0) return "0 B";
    const units = ["B", "KB", "MB", "GB", "TB"];
    const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
    const amount = value / Math.pow(1024, index);
    return `${amount >= 100 || index === 0 ? amount.toFixed(0) : amount.toFixed(1)} ${units[index]}`;
  }

  function formatDate(value) {
    if (!value) return "—";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "—";
    return new Intl.DateTimeFormat("es-AR", {
      day: "2-digit", month: "2-digit", year: "2-digit", hour: "2-digit", minute: "2-digit"
    }).format(date);
  }

  function relativeExpiry(value) {
    if (!value) return "";
    const seconds = Math.max(0, Math.round((new Date(value).getTime() - Date.now()) / 1000));
    if (seconds <= 0) return "El código venció. Generá uno nuevo.";
    return `Vence en ${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
  }

  function statusLabel(status) {
    return ({
      pending: "Preparando", uploading: "Transfiriendo", ready: "Disponible",
      delivered: "Recibido", cancelled: "Cancelado", error: "Error"
    })[status] || status || "—";
  }

  function extension(name) {
    const parts = String(name || "").split(".");
    return parts.length > 1 ? parts.pop().slice(0, 4) : "FILE";
  }

  function toast(message, type = "") {
    const node = document.createElement("div");
    node.className = `toast ${type}`;
    node.textContent = message;
    $("toastStack").appendChild(node);
    setTimeout(() => node.remove(), 4400);
  }

  function showView(view) {
    if (!viewMeta[view]) return;
    currentView = view;
    document.querySelectorAll(".nav-btn").forEach((button) => button.classList.toggle("active", button.dataset.view === view));
    document.querySelectorAll(".view").forEach((section) => section.classList.toggle("active", section.id === `view-${view}`));
    $("pageKicker").textContent = viewMeta[view][0];
    $("pageHeading").textContent = viewMeta[view][1];
  }

  function currentMode() {
    return currentState?.active_mode === "remote" ? "remote" : "local";
  }

  function modeInfo() {
    return currentMode() === "remote" ? currentState?.remote : currentState?.local;
  }

  function renderState() {
    if (!currentState) return;
    const mode = currentMode();
    const info = modeInfo() || {};

    document.querySelectorAll("[data-mode]").forEach((button) => button.classList.toggle("active", button.dataset.mode === mode));
    const status = $("connectionStatus");
    status.querySelector(".status-dot").className = `status-dot ${info.online ? "online" : (info.configured ? "warn" : "")}`;
    status.querySelector("span:last-child").textContent = info.online ? (mode === "local" ? "Red local lista" : "Relay conectado") : (info.configured ? "Sin conexión" : "No configurado");

    $("sidebarAddress").textContent = currentState.local?.display_url || "Sin dirección LAN";
    $("modeTitle").textContent = mode === "local" ? "Wi‑Fi local" : "Modo remoto";
    $("modeDescription").textContent = mode === "local"
      ? "Los archivos viajan directamente por tu red. Es el modo más rápido y no usa Internet."
      : "Los archivos viajan por tu relay privado para llegar desde cualquier lugar. Usá una URL HTTPS para proteger la conexión.";
    $("modeOnline").textContent = info.online ? "Disponible" : (info.configured ? "Desconectado" : "Sin configurar");
    $("modePeers").textContent = `${(info.peers || []).length} vinculados`;
    $("receiveFolderMini").textContent = currentState.receive_folder || "—";
    $("receiveFolderMini").title = currentState.receive_folder || "";
    $("linkMode").textContent = mode === "local" ? "Wi‑Fi local" : "Remoto";
    $("linkAddress").textContent = info.display_url || "—";
    $("linkAddress").title = info.display_url || "";

    renderSelected();
    renderPeers();
    renderJobs();
    renderPairing();
    renderHistory();
    renderSettings();
  }

  function renderSelected() {
    const files = currentState?.selected_files || [];
    const host = $("selectedFiles");
    host.innerHTML = files.map((file) => `
      <div class="file-row">
        <div class="file-icon">${escapeHTML(extension(file.name))}</div>
        <div class="truncate"><div class="file-name truncate">${escapeHTML(file.name)}</div><div class="file-meta">${formatBytes(file.size)} · ${escapeHTML(file.path)}</div></div>
        <span class="state-badge ready">Listo</span>
      </div>`).join("");
    $("dropzone").style.marginBottom = files.length ? "12px" : "22px";
    updateSendButton();
  }

  function renderPeers() {
    const info = modeInfo() || {};
    const peers = info.peers || [];
    const select = $("peerSelect");
    const previous = select.value;
    select.innerHTML = peers.length
      ? `<option value="">Elegí un dispositivo</option>${peers.map((peer) => `<option value="${escapeHTML(peer.id)}">${escapeHTML(peer.name)}${peer.online ? " · disponible" : ""}</option>`).join("")}`
      : `<option value="">Vinculá un dispositivo primero</option>`;
    if (peers.some((peer) => peer.id === previous)) select.value = previous;

    const devices = $("devicesList");
    if (!peers.length) {
      devices.innerHTML = `<div class="empty-state"><div class="drop-icon">${icon("i-device")}</div><strong>No hay dispositivos vinculados</strong><div class="card-copy">Generá un código y abrilo desde el celular.</div></div>`;
    } else {
      devices.innerHTML = peers.map((peer) => `
        <div class="peer-row">
          <div class="device-avatar">${icon(peer.platform === "windows" ? "i-pc" : "i-device")}</div>
          <div class="truncate"><div class="peer-name truncate">${escapeHTML(peer.name)}</div><div class="peer-meta"><span class="status-dot ${peer.online ? "online" : ""}" style="display:inline-block;margin-right:7px"></span>${peer.online ? "Disponible ahora" : `Última actividad ${formatDate(peer.last_seen_at)}`}</div></div>
          <button class="btn btn-danger btn-sm" data-unlink="${escapeHTML(peer.id)}">Quitar</button>
        </div>`).join("");
    }
    updateSendButton();
  }

  function renderJobs() {
    const jobs = currentState?.jobs || [];
    const host = $("jobsList");
    if (!jobs.length) {
      host.innerHTML = `<div class="empty-state" style="padding:18px"><span class="muted">No hay transferencias activas.</span></div>`;
      return;
    }
    host.innerHTML = jobs.slice(0, 12).map((job) => {
      const percent = job.size > 0 ? Math.max(0, Math.min(100, Math.round((job.done || 0) * 100 / job.size))) : (job.status === "delivered" || job.status === "ready" ? 100 : 0);
      return `<div class="transfer-row"><div class="file-icon">${escapeHTML(extension(job.filename))}</div><div class="truncate"><div class="transfer-title"><span class="truncate">${escapeHTML(job.filename)}</span><span>${percent}%</span></div><div class="progress"><span style="width:${percent}%"></span></div><div class="transfer-sub"><span>${job.direction === "received" ? "Recibido de" : "Enviado a"} ${escapeHTML(job.peer_name || "dispositivo")}</span><span>${formatBytes(job.done)} / ${formatBytes(job.size)}</span></div>${job.error ? `<div class="file-meta" style="color:var(--red)">${escapeHTML(job.error)}</div>` : ""}</div><span class="state-badge ${escapeHTML(job.status)}">${escapeHTML(statusLabel(job.status))}</span></div>`;
    }).join("");
  }

  function renderPairing() {
    const info = modeInfo() || {};
    const code = info.pairing_code || "";
    $("pairCode").textContent = code || "— — — — —";
    $("pairExpiry").textContent = code ? relativeExpiry(info.pairing_expiry) : "Generá un código para comenzar.";
    $("copyJoin").disabled = !info.join_url;
    const qr = $("pairQR");
    if (info.join_url && info.join_url !== lastQR) {
      qr.innerHTML = "";
      try {
        new QRCode(qr, { text: info.join_url, width: 154, height: 154, colorDark: "#07111f", colorLight: "#ffffff", correctLevel: QRCode.CorrectLevel.M });
        lastQR = info.join_url;
      } catch (error) {
        qr.innerHTML = `<span class="qr-placeholder">${escapeHTML(error.message)}</span>`;
      }
    } else if (!info.join_url && lastQR) {
      qr.innerHTML = `<span class="qr-placeholder">El QR aparecerá acá</span>`;
      lastQR = "";
    }
  }

  function renderHistory() {
    const values = currentState?.history || [];
    const query = historyFilter.trim().toLowerCase();
    const items = !query ? values : values.filter((item) => [item.filename, item.peer_name, item.source_path, item.destination_path, item.mode, item.direction].some((value) => String(value || "").toLowerCase().includes(query)));
    const host = $("historyList");
    if (!items.length) {
      host.innerHTML = `<div class="empty-state"><div class="drop-icon">${icon("i-history")}</div><strong>${query ? "No hay coincidencias" : "Todavía no hay actividad"}</strong><div class="card-copy">Cada envío y recepción aparecerá acá.</div></div>`;
      return;
    }
    host.innerHTML = items.map((item) => {
      const path = item.direction === "received" ? item.destination_path : item.source_path;
      return `<div class="history-row"><div class="file-icon">${escapeHTML(extension(item.filename))}</div><div class="truncate"><div class="file-name truncate">${escapeHTML(item.filename)}</div><div class="history-path" title="${escapeHTML(path)}">${escapeHTML(path || "Ruta administrada por el dispositivo")}</div></div><div class="history-peer">${item.direction === "received" ? "De" : "A"} <strong>${escapeHTML(item.peer_name || "dispositivo")}</strong><br><span class="faint">${escapeHTML(item.mode === "remote" ? "Remoto" : "Wi‑Fi local")}</span></div><div class="history-date">${formatDate(item.completed_at || item.started_at)}<br><span class="faint">${formatBytes(item.size)}</span></div><div style="display:flex;align-items:center;gap:7px"><span class="state-badge ${escapeHTML(item.status)}">${escapeHTML(statusLabel(item.status))}</span>${path ? `<button class="btn btn-icon btn-sm btn-ghost" title="Abrir ubicación" data-open-path="${escapeHTML(path)}">${icon("i-open")}</button>` : ""}</div></div>`;
    }).join("");
  }

  function setInputUnlessFocused(id, value) {
    const element = $(id);
    if (document.activeElement !== element) element.value = value || "";
  }

  function renderSettings() {
    setInputUnlessFocused("deviceName", currentState.device_name);
    setInputUnlessFocused("receiveFolder", currentState.receive_folder);
    setInputUnlessFocused("remoteURL", currentState.remote?.server_url);
    $("autoStart").checked = Boolean(currentState.auto_start);
    $("autoDownload").checked = Boolean(currentState.auto_download);
    $("appVersion").textContent = currentState.version || "—";
    $("deviceID").textContent = currentState.device_id || "—";
    $("deviceID").title = currentState.device_id || "";
    const notice = $("remoteNotice");
    const dot = notice.querySelector(".status-dot");
    dot.className = `status-dot ${currentState.remote?.online ? "online" : (currentState.remote?.configured ? "warn" : "")}`;
    notice.querySelector("span:last-child").textContent = currentState.remote?.online ? "Servidor remoto conectado" : (currentState.remote?.configured ? (currentState.remote.error || "Servidor no disponible") : "Sin configurar");
  }

  function updateSendButton() {
    const files = currentState?.selected_files || [];
    $("sendFiles").disabled = !files.length || !$("peerSelect").value;
  }

  async function refreshState() {
    if (busyState) return;
    busyState = true;
    try {
      const payload = await nativeFetch("/native/state");
      currentState = payload.state;
      renderState();
    } catch (error) {
      if (/autoriz/i.test(error.message)) {
        app.classList.add("hidden");
        authError.classList.remove("hidden");
      } else {
        const status = $("connectionStatus");
        status.querySelector(".status-dot").className = "status-dot warn";
        status.querySelector("span:last-child").textContent = "Reconectando…";
      }
    } finally {
      busyState = false;
    }
  }

  async function stageFiles(files) {
    for (const file of files) {
      toast(`Preparando ${file.name}…`);
      const headers = new Headers({ "X-pasaDATOS-Native": nativeToken, "Content-Type": file.type || "application/octet-stream" });
      const response = await fetch(`/native/stage/${encodeURIComponent(file.name)}`, { method: "POST", headers, body: file });
      const payload = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(payload.message || `No se pudo preparar ${file.name}`);
    }
    await refreshState();
  }

  document.querySelectorAll(".nav-btn").forEach((button) => button.addEventListener("click", () => showView(button.dataset.view)));
  document.querySelectorAll("[data-go-view]").forEach((button) => button.addEventListener("click", () => showView(button.dataset.goView)));
  document.querySelectorAll("[data-mode]").forEach((button) => button.addEventListener("click", async () => {
    const mode = button.dataset.mode;
    try {
      await nativeFetch("/native/settings", { method: "POST", body: { active_mode: mode } });
      await refreshState();
      if (mode === "remote" && !currentState.remote?.configured) {
        showView("settings");
        toast("Configurá la URL del servidor remoto para usar este modo.");
      }
    } catch (error) { toast(error.message, "error"); }
  }));

  $("pickFiles").addEventListener("click", async () => {
    try { await nativeFetch("/native/pick-files", { method: "POST", body: {} }); await refreshState(); }
    catch (error) { if (!/seleccionó|cancel/i.test(error.message)) toast(error.message, "error"); }
  });

  const dropzone = $("dropzone");
  ["dragenter", "dragover"].forEach((event) => dropzone.addEventListener(event, (e) => { e.preventDefault(); dropzone.classList.add("dragover"); }));
  ["dragleave", "drop"].forEach((event) => dropzone.addEventListener(event, (e) => { e.preventDefault(); dropzone.classList.remove("dragover"); }));
  dropzone.addEventListener("drop", async (event) => {
    const files = Array.from(event.dataTransfer?.files || []);
    if (!files.length) return;
    try { await stageFiles(files); toast(`${files.length} archivo${files.length === 1 ? "" : "s"} listo${files.length === 1 ? "" : "s"}.`, "success"); }
    catch (error) { toast(error.message, "error"); }
  });

  $("peerSelect").addEventListener("change", updateSendButton);
  $("sendFiles").addEventListener("click", async () => {
    const paths = (currentState?.selected_files || []).map((file) => file.path);
    const peerID = $("peerSelect").value;
    try {
      await nativeFetch("/native/send", { method: "POST", body: { mode: currentMode(), peer_id: peerID, paths } });
      toast(`${paths.length} archivo${paths.length === 1 ? "" : "s"} en transferencia.`, "success");
      await refreshState();
    } catch (error) { toast(error.message, "error"); }
  });

  $("createPair").addEventListener("click", async () => {
    try { await nativeFetch("/native/pairing", { method: "POST", body: { mode: currentMode() } }); await refreshState(); toast("Código generado.", "success"); }
    catch (error) { toast(error.message, "error"); }
  });
  $("copyJoin").addEventListener("click", async () => {
    const url = modeInfo()?.join_url;
    if (!url) return;
    try { await navigator.clipboard.writeText(url); toast("Enlace copiado.", "success"); }
    catch { toast("No se pudo copiar el enlace.", "error"); }
  });
  $("joinPair").addEventListener("click", async () => {
    const code = $("joinCode").value.trim();
    if (!code) return toast("Ingresá el código de vinculación.", "error");
    try { await nativeFetch("/native/join", { method: "POST", body: { mode: currentMode(), code } }); $("joinCode").value = ""; await refreshState(); toast("Dispositivo vinculado.", "success"); }
    catch (error) { toast(error.message, "error"); }
  });
  $("joinCode").addEventListener("input", (event) => {
    let value = event.target.value.toUpperCase().replace(/[^A-Z0-9]/g, "").slice(0, 10);
    if (value.length > 5) value = `${value.slice(0, 5)}-${value.slice(5)}`;
    event.target.value = value;
  });
  $("joinCode").addEventListener("keydown", (event) => { if (event.key === "Enter") $("joinPair").click(); });

  $("devicesList").addEventListener("click", async (event) => {
    const button = event.target.closest("[data-unlink]");
    if (!button) return;
    if (!confirm("¿Quitar este dispositivo? Podrás volver a vincularlo cuando quieras.")) return;
    try { await nativeFetch(`/native/peers/${encodeURIComponent(button.dataset.unlink)}?mode=${currentMode()}`, { method: "DELETE" }); await refreshState(); toast("Dispositivo desvinculado."); }
    catch (error) { toast(error.message, "error"); }
  });

  $("historySearch").addEventListener("input", (event) => { historyFilter = event.target.value; renderHistory(); });
  $("historyList").addEventListener("click", async (event) => {
    const button = event.target.closest("[data-open-path]");
    if (!button) return;
    try { await nativeFetch("/native/open", { method: "POST", body: { path: button.dataset.openPath } }); }
    catch (error) { toast(error.message, "error"); }
  });
  $("clearHistory").addEventListener("click", async () => {
    if (!confirm("¿Limpiar todo el historial local? Los archivos no se borrarán.")) return;
    try { await nativeFetch("/native/history", { method: "DELETE" }); await refreshState(); toast("Historial limpiado."); }
    catch (error) { toast(error.message, "error"); }
  });

  $("pickFolder").addEventListener("click", async () => {
    try { await nativeFetch("/native/pick-folder", { method: "POST", body: {} }); await refreshState(); }
    catch (error) { if (!/seleccionó|cancel/i.test(error.message)) toast(error.message, "error"); }
  });
  $("saveLocalSettings").addEventListener("click", async () => {
    try { await nativeFetch("/native/settings", { method: "POST", body: { device_name: $("deviceName").value.trim(), receive_folder: $("receiveFolder").value.trim() } }); await refreshState(); toast("Configuración guardada.", "success"); }
    catch (error) { toast(error.message, "error"); }
  });
  $("saveRemoteSettings").addEventListener("click", async () => {
    try { await nativeFetch("/native/settings", { method: "POST", body: { remote_server_url: $("remoteURL").value.trim() } }); await refreshState(); toast("Servidor remoto guardado. La comprobación se realiza automáticamente.", "success"); }
    catch (error) { toast(error.message, "error"); }
  });
  $("saveBehavior").addEventListener("click", async () => {
    try { await nativeFetch("/native/settings", { method: "POST", body: { auto_start: $("autoStart").checked, auto_download: $("autoDownload").checked } }); await refreshState(); toast("Preferencias guardadas.", "success"); }
    catch (error) { toast(error.message, "error"); }
  });

  ["openReceiveMini", "openReceiveSettings"].forEach((id) => $(id).addEventListener("click", async () => {
    try { await nativeFetch("/native/open-receive-folder", { method: "POST", body: {} }); }
    catch (error) { toast(error.message, "error"); }
  }));

  $("exitApp").addEventListener("click", async () => {
    if (!confirm("¿Cerrar completamente pasaDATOS? Dejará de recibir archivos hasta que vuelvas a abrirlo.")) return;
    try { await nativeFetch("/native/exit", { method: "POST", body: {} }); window.close(); }
    catch (error) { toast(error.message, "error"); }
  });

  window.addEventListener("focus", refreshState);
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible") refreshState();
  });

  refreshState();
  stateTimer = setInterval(refreshState, 1600);
  window.addEventListener("beforeunload", () => clearInterval(stateTimer));
})();
