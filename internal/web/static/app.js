// ComputeBox Craftpanel single page app. No framework, no build step.
"use strict";

const $app = document.getElementById("app");

let me = null;
let meTotp = false;
let sys = null;
let pollTimer = null;
let tabTimer = null;
let consoleES = null;
let currentDetailId = null;

/* ---------- small helpers ---------- */

function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;"
  })[c]);
}

function el(html) {
  const t = document.createElement("template");
  t.innerHTML = html.trim();
  return t.content.firstElementChild;
}

function fmtSize(b) {
  if (b < 1024) return b + " B";
  const units = ["KB", "MB", "GB", "TB"];
  let i = -1;
  do { b /= 1024; i++; } while (b >= 1024 && i < units.length - 1);
  return b.toFixed(b >= 10 ? 0 : 1) + " " + units[i];
}

function fmtDate(ms) {
  if (!ms) return "";
  return new Intl.DateTimeFormat(LANG === "de" ? "de-DE" : "en-GB", {
    dateStyle: "medium", timeStyle: "short"
  }).format(new Date(ms));
}

function fmtUptime(s) {
  s = Math.max(0, Math.floor(s));
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60);
  if (h > 0) return h + "h " + m + "m";
  if (m > 0) return m + "m " + (s % 60) + "s";
  return s + "s";
}

const ICONS = {
  cube: '<svg width="26" height="26" viewBox="0 0 32 32"><polygon points="16,4 28,10 16,16 4,10" fill="#6ea0ff"/><polygon points="4,10 16,16 16,28 4,22" fill="#1e40af"/><polygon points="28,10 16,16 16,28 28,22" fill="#3563e9"/></svg>',
  play: '<svg width="13" height="13" viewBox="0 0 16 16" fill="currentColor"><polygon points="3,2 14,8 3,14"/></svg>',
  stop: '<svg width="13" height="13" viewBox="0 0 16 16" fill="currentColor"><rect x="3" y="3" width="10" height="10"/></svg>',
  restart: '<svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M13.5 8a5.5 5.5 0 1 1-2-4.2"/><polyline points="12,1 12,4.5 8.5,4.5" fill="none"/></svg>',
  skull: '<svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M3 3l10 10M13 3L3 13"/></svg>',
  folder: '<svg width="15" height="15" viewBox="0 0 16 16" fill="#f5a524"><path d="M1 3h5l2 2h7v8H1z"/></svg>',
  file: '<svg width="15" height="15" viewBox="0 0 16 16" fill="none" stroke="#93a0b8" stroke-width="1.4"><path d="M4 1.5h5.5L13 5v9.5H4z"/><path d="M9.5 1.5V5H13"/></svg>',
  download: '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M8 2v8M4.5 7L8 10.5 11.5 7M2.5 13.5h11"/></svg>',
  pencil: '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M2.5 13.5l1-3.5 8-8 2.5 2.5-8 8z"/></svg>',
  trash: '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M2.5 4h11M6.5 4V2h3v2M4.5 4v9.5h7V4M6.5 7v4M9.5 7v4"/></svg>',
  tag: '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M5 2h6M8 2v12M5 14h6"/></svg>',
  copy: '<svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><rect x="5" y="5" width="9" height="9"/><path d="M11 5V2H2v9h3"/></svg>',
  plus: '<svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2"><path d="M8 2v12M2 8h12"/></svg>',
  upload: '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M8 10V2M4.5 5L8 1.5 11.5 5M2.5 13.5h11"/></svg>',
  gear: '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="8" cy="8" r="2.4"/><path d="M8 1.2v2.1M8 12.7v2.1M1.2 8h2.1M12.7 8h2.1M3.2 3.2l1.5 1.5M11.3 11.3l1.5 1.5M12.8 3.2l-1.5 1.5M4.7 11.3l-1.5 1.5"/></svg>',
  archive: '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="1.5" y="2.5" width="13" height="3.5"/><path d="M2.5 6v7.5h11V6M6 9h4"/></svg>'
};

/* ---------- api ---------- */

async function api(path, opts = {}) {
  const init = { method: opts.method || "GET", headers: {} };
  if (init.method !== "GET") init.headers["X-Craftpanel"] = "1";
  if (opts.body !== undefined) {
    init.headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(opts.body);
  }
  if (opts.rawBody !== undefined) init.body = opts.rawBody;
  const res = await fetch(path, init);
  const isJSON = (res.headers.get("Content-Type") || "").includes("application/json");
  const data = isJSON ? await res.json().catch(() => ({})) : await res.text();
  if (!res.ok) {
    const err = new Error((data && data.message) || res.statusText);
    err.code = (data && data.error) || "generic";
    err.data = data;
    err.status = res.status;
    if (res.status === 401 && !opts.noAuthRedirect) {
      stopPolling();
      renderLogin();
    }
    throw err;
  }
  return data;
}

function toastError(e) {
  if (e.code === "java_too_old" && e.data && e.data.need) {
    toast(t("java.tooOld", { need: e.data.need, have: e.data.have }), "err");
    return;
  }
  const known = STRINGS.en["error." + e.code] !== undefined;
  toast(known ? t("error." + e.code) : (e.message || t("error.generic")), "err");
}

function toast(msg, kind) {
  let host = document.getElementById("toasts");
  if (!host) {
    host = el('<div id="toasts"></div>');
    document.body.appendChild(host);
  }
  const item = el(`<div class="toast ${kind || ""}">${esc(msg)}</div>`);
  host.appendChild(item);
  setTimeout(() => item.remove(), 4200);
}

/* ---------- modal infrastructure ---------- */

function closeModal() {
  const o = document.querySelector(".overlay");
  if (o) o.remove();
  document.removeEventListener("keydown", escListener);
}

function escListener(e) {
  if (e.key === "Escape") closeModal();
}

function openModal(inner, wide) {
  closeModal();
  const overlay = el(`<div class="overlay"><div class="modal ${wide ? "wide" : ""}"></div></div>`);
  overlay.firstElementChild.append(...(Array.isArray(inner) ? inner : [inner]));
  document.body.appendChild(overlay);
  document.addEventListener("keydown", escListener);
  return overlay;
}

function promptModal(title, value = "") {
  return new Promise((resolve) => {
    const box = el(`<div>
      <h2>${esc(title)}</h2>
      <input type="text" id="pm-input">
      <div class="modal-actions">
        <button class="btn btn-ghost" id="pm-cancel">${t("misc.cancel")}</button>
        <button class="btn btn-primary" id="pm-ok">OK</button>
      </div>
    </div>`);
    openModal(box);
    const input = box.querySelector("#pm-input");
    input.value = value;
    input.focus();
    input.select();
    const done = (v) => { closeModal(); resolve(v); };
    box.querySelector("#pm-cancel").addEventListener("click", () => done(null));
    box.querySelector("#pm-ok").addEventListener("click", () => done(input.value.trim() || null));
    input.addEventListener("keydown", (e) => { if (e.key === "Enter") done(input.value.trim() || null); });
  });
}

function confirmModal(text, danger) {
  return new Promise((resolve) => {
    const box = el(`<div>
      <p>${esc(text)}</p>
      <div class="modal-actions">
        <button class="btn btn-ghost" id="cm-cancel">${t("misc.cancel")}</button>
        <button class="btn ${danger ? "btn-danger" : "btn-primary"}" id="cm-ok">OK</button>
      </div>
    </div>`);
    openModal(box);
    const done = (v) => { closeModal(); resolve(v); };
    box.querySelector("#cm-cancel").addEventListener("click", () => done(false));
    box.querySelector("#cm-ok").addEventListener("click", () => done(true));
  });
}

/* ---------- boot & auth ---------- */

async function boot() {
  document.documentElement.lang = LANG;
  let st;
  try {
    st = await api("/api/setup-status", { noAuthRedirect: true });
  } catch {
    $app.innerHTML = "<p>Panel API unreachable.</p>";
    return;
  }
  if (st.needsSetup) return renderSetup();
  try {
    const info = await api("/api/me", { noAuthRedirect: true });
    me = info.username;
    meTotp = !!info.totp;
  } catch {
    return renderLogin();
  }
  sys = await api("/api/system").catch(() => null);
  renderShellAndRoute();
}

function authScreen(cardHTML) {
  stopPolling();
  closeConsole();
  $app.innerHTML = "";
  const wrap = el(`<div class="auth-wrap">
    <div class="auth-logo">${ICONS.cube}<span class="wm-name">Craftpanel</span></div>
    ${cardHTML}
    <p class="auth-tagline">${t("app.tagline")}</p>
    <p class="auth-foot">${t("poweredBy")} <a href="https://computebox.de?utm_source=craftpanel" target="_blank" rel="noopener">ComputeBox</a></p>
  </div>`);
  $app.appendChild(wrap);
  return wrap;
}

function renderLogin() {
  const wrap = authScreen(`<form class="auth-card" id="login-form">
    <h1>${t("login.title")}</h1>
    <div id="login-err"></div>
    <label class="field"><span>${t("login.username")}</span><input type="text" name="username" autocomplete="username" required></label>
    <label class="field"><span>${t("login.password")}</span><input type="password" name="password" autocomplete="current-password" required></label>
    <label class="field" id="totp-row" hidden><span>${t("totp.loginCode")}</span><input type="text" name="code" inputmode="numeric" maxlength="6" autocomplete="one-time-code"></label>
    <button class="btn btn-primary" type="submit">${t("login.submit")}</button>
  </form>`);
  wrap.querySelector("#login-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const f = e.target;
    try {
      const data = await api("/api/login", {
        method: "POST",
        noAuthRedirect: true,
        body: { username: f.username.value.trim(), password: f.password.value, code: f.code.value.trim() }
      });
      me = data.username;
      meTotp = true; // refreshed on next boot; only used for the settings modal
      const info = await api("/api/me").catch(() => null);
      if (info) meTotp = !!info.totp;
      sys = await api("/api/system").catch(() => null);
      renderShellAndRoute();
    } catch (err) {
      const box = wrap.querySelector("#login-err");
      const totpRow = wrap.querySelector("#totp-row");
      if (err.code === "totp_required") {
        totpRow.hidden = false;
        f.code.focus();
        box.innerHTML = "";
        return;
      }
      if (err.code === "totp_invalid") totpRow.hidden = false;
      box.innerHTML = `<div class="form-error">${esc(STRINGS.en["error." + err.code] ? t("error." + err.code) : err.message)}</div>`;
    }
  });
}

function renderSetup() {
  const wrap = authScreen(`<form class="auth-card" id="setup-form">
    <h1>${t("setup.title")}</h1>
    <p class="hint">${t("setup.hint")}</p>
    <div id="setup-err"></div>
    <label class="field"><span>${t("login.username")}</span><input type="text" name="username" autocomplete="username" required></label>
    <label class="field"><span>${t("login.password")}</span><input type="password" name="password" autocomplete="new-password" required minlength="8"></label>
    <label class="field"><span>${t("setup.password2")}</span><input type="password" name="password2" autocomplete="new-password" required></label>
    <button class="btn btn-primary" type="submit">${t("setup.submit")}</button>
  </form>`);
  wrap.querySelector("#setup-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const f = e.target;
    const errBox = wrap.querySelector("#setup-err");
    if (f.password.value !== f.password2.value) {
      errBox.innerHTML = `<div class="form-error">${t("setup.mismatch")}</div>`;
      return;
    }
    try {
      me = (await api("/api/setup", {
        method: "POST",
        noAuthRedirect: true,
        body: { username: f.username.value.trim(), password: f.password.value }
      })).username;
      sys = await api("/api/system").catch(() => null);
      renderShellAndRoute();
    } catch (err) {
      errBox.innerHTML = `<div class="form-error">${esc(err.message)}</div>`;
    }
  });
}

/* ---------- shell & router ---------- */

function renderShellAndRoute() {
  $app.innerHTML = "";
  const shell = el(`<div class="shell">
    <header class="topbar">
      <div class="wordmark" id="wm">${ICONS.cube}<span class="wm-name">Craftpanel</span></div>
      <div class="wm-sub">${t("poweredBy")}<br><a href="https://computebox.de?utm_source=craftpanel" target="_blank" rel="noopener">ComputeBox</a></div>
      <div class="spacer"></div>
      <div class="lang-switch">
        <button data-lang="de" class="${LANG === "de" ? "active" : ""}">DE</button>
        <button data-lang="en" class="${LANG === "en" ? "active" : ""}">EN</button>
      </div>
      <button class="btn btn-ghost btn-sm" id="panel-settings-btn" title="${t("panel.title")}">${ICONS.gear}</button>
      <div class="userbox"><span>${esc(me)}</span><button class="btn btn-ghost btn-sm" id="logout">${t("nav.logout")}</button></div>
    </header>
    <main id="content"></main>
    <footer class="foot">
      <span>ComputeBox Craftpanel ${sys ? esc(sys.version) : ""}</span>
      <span>${t("poweredBy")} <a href="https://computebox.de?utm_source=craftpanel" target="_blank" rel="noopener">ComputeBox</a></span>
    </footer>
  </div>`);
  $app.appendChild(shell);
  shell.querySelector("#wm").addEventListener("click", () => { location.hash = "#/"; });
  shell.querySelector("#panel-settings-btn").addEventListener("click", openPanelSettings);
  shell.querySelector("#logout").addEventListener("click", async () => {
    try { await api("/api/logout", { method: "POST" }); } catch {}
    me = null;
    renderLogin();
  });
  shell.querySelectorAll(".lang-switch button").forEach((b) =>
    b.addEventListener("click", () => {
      setLang(b.dataset.lang);
      renderShellAndRoute();
    })
  );
  route();
}

window.addEventListener("hashchange", () => {
  if (me && document.getElementById("content")) route();
});

function stopTabTimer() {
  if (tabTimer) { clearInterval(tabTimer); tabTimer = null; }
}

function route() {
  stopPolling();
  stopTabTimer();
  closeConsole();
  closeModal();
  const h = location.hash || "#/";
  const m = h.match(/^#\/server\/([^/]+)(?:\/([a-z]+))?/);
  if (m) renderDetail(decodeURIComponent(m[1]), m[2] || "console");
  else renderDash();
}

function stopPolling() {
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
}

function startPolling(fn, ms) {
  stopPolling();
  pollTimer = setInterval(fn, ms);
}

function content() {
  return document.getElementById("content");
}

/* ---------- dashboard ---------- */

// Cards are kept across polls and patched in place. Re-creating them every
// three seconds would restart their entrance animation.
let dashCards = new Map();

async function renderDash() {
  currentDetailId = null;
  dashCards = new Map();
  const c = content();
  c.innerHTML = `<div class="page-head"><h1>${t("dash.title")}</h1>
    <button class="btn btn-primary" id="new-server">${ICONS.plus} ${t("dash.new")}</button></div>
    <div id="update-banner"></div>
    <div id="eula-banner"></div>
    <div id="java-warning"></div>
    <div id="server-grid"></div>`;
  c.querySelector("#new-server").addEventListener("click", openCreateModal);
  if (sys && sys.updateAvailable && localStorage.getItem("cp_hide_update") !== sys.latest) {
    const b = el(`<div class="notice">${esc(t("update.banner", { v: sys.latest }))}
      <button class="btn btn-ok btn-sm" id="upd-now">${t("update.now")}</button>
      <button class="btn btn-ghost btn-sm" id="upd-hide">${t("update.dismiss")}</button></div>`);
    b.querySelector("#upd-now").addEventListener("click", (e) => {
      e.target.disabled = true;
      doSelfUpdate();
    });
    b.querySelector("#upd-hide").addEventListener("click", () => {
      localStorage.setItem("cp_hide_update", sys.latest);
      b.remove();
    });
    c.querySelector("#update-banner").appendChild(b);
  }
  await refreshDash();
  startPolling(refreshDash, 3000);
}

async function refreshDash() {
  const grid = document.getElementById("server-grid");
  if (!grid) return;
  let servers;
  try {
    servers = await api("/api/servers");
  } catch (e) {
    if (e.status !== 401) toastError(e);
    return;
  }

  renderEulaBanner(servers);
  renderJavaWarning(servers);

  if (servers.length === 0) {
    if (!grid.querySelector(".empty")) {
      dashCards.clear();
      grid.innerHTML = `<div class="empty"><h2>${t("dash.empty.title")}</h2>
        <p>${t("dash.empty.text")}</p>
        <button class="btn btn-primary" id="empty-new">${ICONS.plus} ${t("dash.new")}</button></div>`;
      grid.querySelector("#empty-new").addEventListener("click", openCreateModal);
    }
    return;
  }

  let wrap = grid.querySelector(".grid");
  if (!wrap) {
    grid.innerHTML = `<div class="grid"></div>`;
    wrap = grid.firstElementChild;
    dashCards.clear();
  }

  const seen = new Set();
  for (const s of servers) {
    seen.add(s.id);
    let card = dashCards.get(s.id);
    if (!card) {
      card = createServerCard(s.id);
      dashCards.set(s.id, card);
      wrap.appendChild(card);
    }
    updateServerCard(card, s);
  }
  for (const [id, card] of dashCards) {
    if (!seen.has(id)) {
      card.remove();
      dashCards.delete(id);
    }
  }
}

function renderJavaWarning(servers) {
  const host = document.getElementById("java-warning");
  if (!host) return;
  let html = "";
  if (sys && sys.java && !sys.java.found) {
    html = `<div class="notice">${esc(t("java.missing"))}</div>`;
  } else if (sys && sys.java && sys.java.major) {
    const need = Math.max(0, ...servers.map((s) => s.javaMajor || 0));
    if (need > sys.java.major) {
      html = `<div class="notice">${esc(t("java.tooOldHost", { have: sys.java.major }))}</div>`;
    }
  }
  if (host.innerHTML !== html) host.innerHTML = html;
}

// The EULA gate is the one thing that blocks every server, so it gets a banner
// at the very top of the dashboard rather than being buried in a settings tab.
function renderEulaBanner(servers) {
  const host = document.getElementById("eula-banner");
  if (!host) return;
  const pending = servers.filter((s) => !s.eula && s.status !== "installing" && s.status !== "install_failed");
  if (pending.length === 0) {
    if (host.childElementCount) host.innerHTML = "";
    host.dataset.ids = "";
    return;
  }
  const ids = pending.map((s) => s.id).join(",");
  if (host.dataset.ids === ids) return; // already showing exactly these servers
  host.dataset.ids = ids;

  host.innerHTML = `<div class="panel eula-gate">
    <h2>${t("eula.banner.title")}</h2>
    <p class="hint">${t("eula.banner.text")}
      <a href="https://aka.ms/MinecraftEULA" target="_blank" rel="noopener">${t("eula.link")}</a></p>
    <div class="eula-gate-actions"></div>
  </div>`;
  const actions = host.querySelector(".eula-gate-actions");

  if (pending.length === 1) {
    const b = el(`<button class="btn btn-ok">${t("eula.accept")}</button>`);
    b.addEventListener("click", () => acceptEula([pending[0].id]));
    actions.appendChild(b);
  } else {
    const all = el(`<button class="btn btn-ok">${t("eula.acceptAll")}</button>`);
    all.addEventListener("click", () => acceptEula(pending.map((s) => s.id)));
    actions.appendChild(all);
    for (const s of pending) {
      const b = el(`<button class="btn btn-sm">${esc(t("eula.acceptFor", { name: s.name }))}</button>`);
      b.addEventListener("click", () => acceptEula([s.id]));
      actions.appendChild(b);
    }
  }
}

async function acceptEula(ids) {
  try {
    for (const id of ids) {
      await api(`/api/servers/${encodeURIComponent(id)}/eula`, { method: "POST", body: { accept: true } });
    }
    toast(t("eula.accepted"), "ok");
    const host = document.getElementById("eula-banner");
    if (host) host.dataset.ids = "";
    await refreshDash();
  } catch (e) {
    toastError(e);
  }
}

function statusBadge(s) {
  return `<span class="badge st-${esc(s.status)}"><i class="led"></i>${t("status." + s.status)}</span>`;
}

function createServerCard(id) {
  const card = el(`<div class="card server-card">
    <div class="sc-top"><h3></h3><span class="sc-badges"></span></div>
    <div class="sc-meta"></div>
    <div class="sc-extra"></div>
    <div class="sc-actions"></div>
  </div>`);
  card.addEventListener("click", () => { location.hash = "#/server/" + encodeURIComponent(id); });
  return card;
}

function updateServerCard(card, s) {
  const name = card.querySelector("h3");
  if (name.textContent !== s.name) name.textContent = s.name;

  const badges = card.querySelector(".sc-badges");
  const badgeHTML = statusBadge(s) +
    (!s.eula && s.status !== "installing" && s.status !== "install_failed"
      ? `<span class="badge st-install_failed"><i class="led"></i>${t("eula.required")}</span>` : "");
  if (badges.innerHTML !== badgeHTML) badges.innerHTML = badgeHTML;

  const meta = card.querySelector(".sc-meta");
  const typeLabel = s.modpack && s.modpack.title
    ? `${esc(s.modpack.title)} · ${esc(s.type)}`
    : esc(s.type);
  const metaHTML = `<span>${typeLabel} <b>${esc(s.version)}</b></span>
    ${s.domain ? `<span><b>${esc(s.domain + (s.domainPort ? ":" + s.domainPort : ""))}</b></span>`
      : `<span>${t("misc.port")} <b>${s.port}</b></span>`}
    ${s.javaMajor ? `<span>${esc(t("java.needs", { need: s.javaMajor }))}</span>` : ""}
    ${s.status === "running" && s.players ? `<span>${t("players.label")} <b>${s.players.online}/${s.players.max}</b></span>` : ""}
    ${s.status === "running" && s.rssMB ? `<span>RAM <b>${s.rssMB} MB</b></span>` : ""}
    ${s.status === "running" ? `<span>${t("detail.uptime")} <b>${fmtUptime(s.uptimeS)}</b></span>` : ""}`;
  if (meta.innerHTML !== metaHTML) meta.innerHTML = metaHTML;

  const extra = card.querySelector(".sc-extra");
  if (s.status === "installing") {
    let bar = extra.querySelector(".progress i");
    if (!bar) {
      extra.innerHTML = `<div class="progress"><i></i></div>`;
      bar = extra.querySelector("i");
    }
    bar.style.width = Math.round(s.progress * 100) + "%";
  } else if (s.status === "install_failed") {
    const html = `<p class="hint">${esc(s.error || "")}</p>`;
    if (extra.innerHTML !== html) extra.innerHTML = html;
  } else if (extra.childElementCount) {
    extra.innerHTML = "";
  }

  // Rebuild the action row only when the set of buttons actually changes.
  const actions = card.querySelector(".sc-actions");
  const wantEula = !s.eula && s.status !== "installing" && s.status !== "install_failed";
  const key = wantEula ? "eula" : s.status;
  if (actions.dataset.key === key) return;
  actions.dataset.key = key;
  actions.innerHTML = "";

  const stopClick = (fn) => (e) => { e.stopPropagation(); fn(); };
  const add = (cls, label, fn) => {
    const b = el(`<button class="btn btn-sm ${cls}">${label}</button>`);
    b.addEventListener("click", stopClick(fn));
    actions.appendChild(b);
  };
  if (wantEula) {
    add("btn-ok", t("eula.accept"), () => acceptEula([s.id]));
  } else if (s.status === "stopped") {
    add("btn-ok", `${ICONS.play} ${t("actions.start")}`, () => serverAction(s.id, "start"));
  } else if (s.status === "running" || s.status === "starting") {
    add("", `${ICONS.stop} ${t("actions.stop")}`, () => serverAction(s.id, "stop"));
  } else if (s.status === "install_failed") {
    add("", `${ICONS.restart} ${t("actions.retryInstall")}`, () => serverAction(s.id, "retry-install"));
  }
}

async function serverAction(id, action) {
  try {
    await api(`/api/servers/${encodeURIComponent(id)}/${action}`, { method: "POST", body: {} });
    if (currentDetailId) await updateDetailHead(currentDetailId);
    else await refreshDash();
  } catch (e) {
    toastError(e);
  }
}

/* ---------- create modal ---------- */

const MEM_OPTIONS = [1024, 2048, 4096, 6144, 8192, 12288, 16384];

function isModdedType(typ) {
  return typ === "fabric" || typ === "forge" || typ === "neoforge" || typ === "quilt";
}

async function openCreateModal() {
  const box = el(`<div>
    <h2>${t("create.title")}</h2>
    <form id="create-form">
      <label class="field"><span>${t("create.name")}</span><input type="text" name="name" maxlength="40" required></label>
      <div class="form-row">
        <label class="field"><span>${t("create.type")}</span>
          <select name="type">
            <option value="paper">Paper</option>
            <option value="vanilla">Vanilla</option>
            <option value="fabric">Fabric</option>
            <option value="forge">Forge</option>
            <option value="neoforge">NeoForge</option>
            <option value="quilt">Quilt</option>
            <option value="modpack">${t("create.typeModpack")}</option>
            <option value="bedrock">Bedrock</option>
            <option value="velocity">Velocity (Proxy)</option>
          </select>
        </label>
        <label class="field" id="create-version-field"><span>${t("create.version")}</span>
          <select name="version" disabled><option>${t("create.loadingVersions")}</option></select>
        </label>
      </div>
      <div id="modpack-picker" hidden>
        <label class="field"><span>${t("create.modpackSearch")}</span>
          <div class="form-row">
            <input type="text" id="mp-query" placeholder="${esc(t("create.modpackPlaceholder"))}">
            <button type="button" class="btn btn-sm btn-primary" id="mp-search">${t("create.modpackSearchBtn")}</button>
          </div>
        </label>
        <div id="mp-results" class="plg-results"></div>
        <label class="field" id="mp-version-field" hidden><span>${t("create.modpackVersion")}</span>
          <select id="mp-version"></select>
        </label>
        <input type="hidden" id="mp-project" value="">
      </div>
      <div class="form-row">
        <label class="field" id="create-mem"><span>${t("create.memory")}</span>
          <select name="memoryMB">${MEM_OPTIONS.map((m) =>
            `<option value="${m}" ${m === 2048 ? "selected" : ""}>${m / 1024} GB</option>`).join("")}</select>
        </label>
        <label class="field"><span>${t("create.port")}</span>
          <input type="number" name="port" min="1024" max="65535" placeholder="${t("create.portAuto")}">
        </label>
      </div>
      <p class="hint" id="bedrock-hint" hidden>${t("create.bedrockHint")}</p>
      <p class="hint" id="velocity-hint" hidden>${t("create.velocityHint")}</p>
      <p class="hint" id="modded-hint" hidden>${t("create.moddedHint")}</p>
      <p class="hint" id="modpack-hint" hidden>${t("create.modpackHint")}</p>
      <div class="modal-actions">
        <button type="button" class="btn btn-ghost" id="create-cancel">${t("misc.cancel")}</button>
        <button type="submit" class="btn btn-primary" id="create-submit">${t("create.submit")}</button>
      </div>
    </form>
  </div>`);
  openModal(box);
  box.querySelector("#create-cancel").addEventListener("click", closeModal);

  const typeSel = box.querySelector("select[name=type]");
  const verSel = box.querySelector("select[name=version]");
  const memSel = box.querySelector("select[name=memoryMB]");
  const mpProject = box.querySelector("#mp-project");
  const mpVersion = box.querySelector("#mp-version");
  const mpResults = box.querySelector("#mp-results");

  async function loadVersions() {
    if (typeSel.value === "modpack") return;
    verSel.disabled = true;
    verSel.innerHTML = `<option>${t("create.loadingVersions")}</option>`;
    try {
      const list = await api("/api/versions?type=" + typeSel.value);
      verSel.innerHTML = list.map((v) =>
        `<option value="${esc(v.id)}">${esc(v.id)}${v.latest ? " (latest)" : ""}</option>`).join("");
      verSel.disabled = false;
    } catch (e) {
      verSel.innerHTML = `<option value="">?</option>`;
      toastError(e);
    }
  }

  async function searchModpacks() {
    const q = box.querySelector("#mp-query").value.trim();
    mpResults.innerHTML = `<p class="hint">${t("misc.loading")}</p>`;
    try {
      const hits = await api(`/api/modpacks/search?q=${encodeURIComponent(q)}&sort=downloads`);
      if (!hits.length) {
        mpResults.innerHTML = `<p class="hint">${t("create.modpackNoResults")}</p>`;
        return;
      }
      mpResults.innerHTML = "";
      hits.forEach((hit) => {
        const row = el(`<div class="plg-hit">
          <div><strong>${esc(hit.title)}</strong>
            <span class="plg-desc">${esc(hit.description || "")}</span>
            <span class="plg-dl">${(hit.loaders || []).map(esc).join(", ")} · ${hit.downloads.toLocaleString()} ${t("plugins.downloads")}</span>
          </div>
          <button type="button" class="btn btn-sm">${t("create.modpackSelect")}</button>`);
        row.querySelector("button").addEventListener("click", async () => {
          mpProject.value = hit.projectId;
          box.querySelector("#mp-version-field").hidden = false;
          mpVersion.innerHTML = `<option>${t("create.loadingVersions")}</option>`;
          try {
            const vers = await api(`/api/modpacks/${encodeURIComponent(hit.projectId)}/versions`);
            mpVersion.innerHTML = vers.map((v) =>
              `<option value="${esc(v.id)}">${esc(v.name || v.versionNumber)} (${esc((v.gameVersions || []).join(", "))}${v.loaders && v.loaders.length ? " · " + esc(v.loaders.join(", ")) : ""})</option>`
            ).join("");
            toast(t("create.modpackSelected", { name: hit.title }), "ok");
          } catch (e) {
            toastError(e);
          }
        });
        mpResults.appendChild(row);
      });
    } catch (e) {
      mpResults.innerHTML = "";
      toastError(e);
    }
  }

  const syncTypeUI = () => {
    const typ = typeSel.value;
    const bedrock = typ === "bedrock";
    const modpack = typ === "modpack";
    box.querySelector("#create-mem").hidden = bedrock;
    box.querySelector("#create-version-field").hidden = modpack;
    box.querySelector("#modpack-picker").hidden = !modpack;
    box.querySelector("#bedrock-hint").hidden = !bedrock;
    box.querySelector("#velocity-hint").hidden = typ !== "velocity";
    box.querySelector("#modded-hint").hidden = !isModdedType(typ);
    box.querySelector("#modpack-hint").hidden = !modpack;
    if (modpack && parseInt(memSel.value, 10) < 4096) memSel.value = "4096";
    else if (isModdedType(typ) && parseInt(memSel.value, 10) < 2048) memSel.value = "2048";
  };
  typeSel.addEventListener("change", () => {
    syncTypeUI();
    loadVersions();
    if (typeSel.value === "modpack" && !mpResults.childElementCount) searchModpacks();
  });
  box.querySelector("#mp-search").addEventListener("click", searchModpacks);
  box.querySelector("#mp-query").addEventListener("keydown", (e) => {
    if (e.key === "Enter") { e.preventDefault(); searchModpacks(); }
  });
  syncTypeUI();
  loadVersions();

  box.querySelector("#create-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const f = e.target;
    const submit = box.querySelector("#create-submit");
    submit.disabled = true;
    try {
      const body = {
        name: f.name.value.trim(),
        type: f.type.value,
        memoryMB: parseInt(f.memoryMB.value, 10)
      };
      if (f.port.value) body.port = parseInt(f.port.value, 10);
      if (f.type.value === "modpack") {
        if (!mpProject.value || !mpVersion.value) {
          throw Object.assign(new Error(t("create.modpackRequired")), { status: 400 });
        }
        body.modpackProject = mpProject.value;
        body.modpackVersion = mpVersion.value;
      } else {
        body.version = f.version.value;
      }
      const created = await api("/api/servers", { method: "POST", body });
      closeModal();
      location.hash = "#/server/" + encodeURIComponent(created.id);
    } catch (err) {
      submit.disabled = false;
      toastError(err);
    }
  });
}

/* ---------- server detail ---------- */

async function renderDetail(id, tab) {
  currentDetailId = id;
  let s;
  try {
    s = await api("/api/servers/" + encodeURIComponent(id));
  } catch (e) {
    if (e.status === 404) { location.hash = "#/"; return; }
    toastError(e);
    return;
  }
  const c = content();
  c.innerHTML = `
    <div id="detail-head"></div>
    <nav class="tabs" id="tabs">
      <button data-tab="console">${t("tabs.console")}</button>
      ${s.type === "velocity" ? "" : `<button data-tab="players">${t("tabs.players")}</button>`}
      ${s.type === "velocity" ? `<button data-tab="network">${t("tabs.network")}</button>` : ""}
      ${s.type === "paper" || s.type === "velocity" ? `<button data-tab="plugins">${t("tabs.plugins")}</button>` : ""}
      ${isModdedType(s.type) ? `<button data-tab="mods">${t("tabs.mods")}</button>` : ""}
      <button data-tab="files">${t("tabs.files")}</button>
      <button data-tab="backups">${t("tabs.backups")}</button>
      <button data-tab="settings">${t("tabs.settings")}</button>
    </nav>
    <section id="tab-body"></section>`;
  renderDetailHead(s);
  c.querySelectorAll("#tabs button").forEach((b) => {
    b.classList.toggle("active", b.dataset.tab === tab);
    b.addEventListener("click", () => {
      location.hash = `#/server/${encodeURIComponent(id)}/${b.dataset.tab}`;
    });
  });
  const plugins = s.type === "paper" || s.type === "velocity";
  if (tab === "files") renderFilesTab(id);
  else if (tab === "players" && s.type !== "velocity") renderPlayersTab(id);
  else if (tab === "network" && s.type === "velocity") renderNetworkTab(id);
  else if (tab === "plugins" && plugins) renderPluginsTab(id);
  else if (tab === "mods" && isModdedType(s.type)) renderModsTab(id);
  else if (tab === "backups") renderBackupsTab(id);
  else if (tab === "settings") renderSettingsTab(id, s);
  else renderConsoleTab(id, s);

  startPolling(() => updateDetailHead(id), 3000);
}

function renderDetailHead(s) {
  const host = location.hostname || "localhost";
  // With a domain mapping the server has a real hostname; domainPort is 0
  // when players do not need to type a port at all.
  const addr = s.domain
    ? s.domain + (s.domainPort ? ":" + s.domainPort : "")
    : `${host}:${s.port}`;
  const head = document.getElementById("detail-head");
  if (!head) return;
  head.innerHTML = `
    <div class="detail-head">
      <button class="btn btn-ghost btn-sm" id="back">&larr; ${t("misc.back")}</button>
      <h1>${esc(s.name)}</h1>
      ${statusBadge(s)}
      <div class="detail-actions" id="head-actions"></div>
    </div>
    <div class="detail-sub">
      <span>${s.modpack && s.modpack.title ? `${esc(s.modpack.title)} · ` : ""}${esc(s.type)} ${esc(s.version)}${s.loaderVersion ? ` <span class="plg-desc">(${esc(s.loaderVersion)})</span>` : ""}</span>
      <span>${t("detail.address")} <code>${esc(addr)}</code>${s.type === "bedrock" ? " (UDP)" : ""}
        <button class="btn btn-ghost btn-sm" id="copy-addr" title="${t("detail.copy")}">${ICONS.copy}</button></span>
      ${s.status === "running" ? `<span>${t("detail.uptime")} ${fmtUptime(s.uptimeS)}</span>` : ""}
      ${s.status === "running" && s.players ? `<span>${t("players.label")} ${s.players.online}/${s.players.max}</span>` : ""}
      ${s.status === "running" && s.rssMB ? `<span>RAM ${s.rssMB} MB</span>` : ""}
      ${s.status === "running" && s.cpuPct ? `<span>CPU ${s.cpuPct}%</span>` : ""}
      ${s.diskMB ? `<span>${t("misc.disk")} ${fmtSize(s.diskMB * 1048576)}</span>` : ""}
      ${s.status === "installing" ? `<span>${t("detail.installing")} (${Math.round(s.progress * 100)}%)</span>` : ""}
      ${s.status === "install_failed" ? `<span class="c-err">${esc(s.error || "")}</span>` : ""}
    </div>`;
  head.querySelector("#back").addEventListener("click", () => { location.hash = "#/"; });
  head.querySelector("#copy-addr").addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(addr);
      toast(t("detail.copied"), "ok");
    } catch {}
  });

  const actions = head.querySelector("#head-actions");
  const add = (cls, icon, label, fn, confirmKey) => {
    const b = el(`<button class="btn ${cls}">${icon} ${label}</button>`);
    b.addEventListener("click", async () => {
      if (confirmKey && !(await confirmModal(t(confirmKey), true))) return;
      fn();
    });
    actions.appendChild(b);
  };
  const needsEula = !s.eula && s.status !== "installing" && s.status !== "install_failed";
  if (needsEula) {
    add("btn-ok", "", t("eula.accept"), async () => {
      try {
        await api(`/api/servers/${encodeURIComponent(s.id)}/eula`, { method: "POST", body: { accept: true } });
        toast(t("eula.accepted"), "ok");
        await updateDetailHead(s.id);
      } catch (e) { toastError(e); }
    });
  }
  switch (s.status) {
    case "stopped":
      if (!needsEula) add("btn-ok", ICONS.play, t("actions.start"), () => serverAction(s.id, "start"));
      break;
    case "running":
    case "starting":
      add("", ICONS.stop, t("actions.stop"), () => serverAction(s.id, "stop"));
      add("", ICONS.restart, t("actions.restart"), () => serverAction(s.id, "restart"));
      add("btn-danger", ICONS.skull, t("actions.kill"), () => serverAction(s.id, "kill"), "misc.confirmKill");
      break;
    case "stopping":
      add("btn-danger", ICONS.skull, t("actions.kill"), () => serverAction(s.id, "kill"), "misc.confirmKill");
      break;
    case "install_failed":
      add("", ICONS.restart, t("actions.retryInstall"), () => serverAction(s.id, "retry-install"));
      break;
  }
}

async function updateDetailHead(id) {
  if (currentDetailId !== id) return;
  try {
    const s = await api("/api/servers/" + encodeURIComponent(id));
    renderDetailHead(s);
  } catch (e) {
    if (e.status === 404) location.hash = "#/";
  }
}

/* ---------- console tab ---------- */

function closeConsole() {
  if (consoleES) { consoleES.close(); consoleES = null; }
}

function renderConsoleTab(id, s) {
  const body = document.getElementById("tab-body");
  body.innerHTML = `
    <div class="console-tools">
      <input type="text" id="console-filter" placeholder="${esc(t("console.filter"))}">
      <button class="btn btn-sm" id="console-dl">${ICONS.download} ${t("console.download")}</button>
    </div>
    <div class="console-box" id="console"></div>
    <form class="console-form" id="cmd-form">
      <span class="prompt">&gt;</span>
      <input type="text" id="cmd-input" placeholder="${esc(t("console.placeholder"))}" autocomplete="off">
      <button class="btn btn-primary" type="submit">${t("console.send")}</button>
    </form>
    <div class="console-opts">
      <input type="checkbox" id="autoscroll" checked>
      <label for="autoscroll">${t("console.autoscroll")}</label>
    </div>`;
  const box = body.querySelector("#console");

  const filterInput = body.querySelector("#console-filter");
  filterInput.addEventListener("input", () => {
    const q = filterInput.value.toLowerCase();
    for (const div of box.children) {
      div.classList.toggle("hide", q !== "" && !div.textContent.toLowerCase().includes(q));
    }
  });
  body.querySelector("#console-dl").addEventListener("click", () => {
    const text = [...box.children].map((d) => d.textContent).join("\n");
    const blob = new Blob([text], { type: "text/plain" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = id + "-console.txt";
    a.click();
    URL.revokeObjectURL(a.href);
  });
  let sawLine = false;

  closeConsole();
  consoleES = new EventSource(`/api/servers/${encodeURIComponent(id)}/console`);
  consoleES.addEventListener("open", () => {
    // The server resends the full history after every (re)connect.
    box.innerHTML = "";
    sawLine = false;
    setTimeout(() => {
      if (!sawLine && !box.childElementCount) {
        appendConsoleLine(box, "[craftpanel] " + t("console.hint.stopped"));
      }
    }, 400);
  });
  consoleES.addEventListener("message", (e) => {
    let ev;
    try { ev = JSON.parse(e.data); } catch { return; }
    if (ev.t === "line") {
      if (!sawLine) { sawLine = true; box.innerHTML = ""; }
      appendConsoleLine(box, ev.line);
    } else if (ev.t === "status") {
      updateDetailHead(id);
    }
  });

  // Command history: arrow keys walk previous commands, per server.
  const histKey = "cp_hist_" + id;
  let hist = [];
  try { hist = JSON.parse(localStorage.getItem(histKey) || "[]"); } catch {}
  let histIdx = -1;
  let draft = "";
  const cmdInput = body.querySelector("#cmd-input");
  cmdInput.addEventListener("keydown", (e) => {
    if (e.key === "ArrowUp") {
      if (histIdx < hist.length - 1) {
        if (histIdx === -1) draft = cmdInput.value;
        histIdx++;
        cmdInput.value = hist[hist.length - 1 - histIdx];
      }
      e.preventDefault();
    } else if (e.key === "ArrowDown") {
      if (histIdx > -1) {
        histIdx--;
        cmdInput.value = histIdx === -1 ? draft : hist[hist.length - 1 - histIdx];
      }
      e.preventDefault();
    }
  });

  body.querySelector("#cmd-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const cmd = cmdInput.value.trim();
    if (!cmd) return;
    cmdInput.value = "";
    if (hist[hist.length - 1] !== cmd) {
      hist.push(cmd);
      if (hist.length > 100) hist.shift();
      localStorage.setItem(histKey, JSON.stringify(hist));
    }
    histIdx = -1;
    draft = "";
    try {
      await api(`/api/servers/${encodeURIComponent(id)}/command`, { method: "POST", body: { command: cmd } });
    } catch (err) {
      toastError(err);
    }
  });
}

function appendConsoleLine(box, line) {
  const div = document.createElement("div");
  let cls = "";
  if (line.startsWith("> ")) cls = "c-cmd";
  else if (line.startsWith("[craftpanel]")) cls = "c-meta";
  else if (/ERROR|SEVERE|Exception|error/.test(line)) cls = "c-err";
  else if (/WARN/.test(line)) cls = "c-warn";
  if (cls) div.className = cls;
  div.textContent = line;
  const filter = document.getElementById("console-filter");
  if (filter && filter.value !== "" && !line.toLowerCase().includes(filter.value.toLowerCase())) {
    div.classList.add("hide");
  }
  box.appendChild(div);
  while (box.childElementCount > 2000) box.firstElementChild.remove();
  const auto = document.getElementById("autoscroll");
  if (!auto || auto.checked) box.scrollTop = box.scrollHeight;
}

/* ---------- files tab ---------- */

let filesPath = ".";

function renderFilesTab(id) {
  filesPath = ".";
  const body = document.getElementById("tab-body");
  body.innerHTML = `
    <div class="files-bar">
      <div class="crumbs" id="crumbs"></div>
      <div class="spacer"></div>
      <button class="btn btn-sm" id="mkdir">${ICONS.plus} ${t("files.newFolder")}</button>
      <button class="btn btn-sm btn-primary" id="upload">${ICONS.upload} ${t("files.upload")}</button>
      <input type="file" id="upload-input" multiple hidden>
    </div>
    <div id="files-body"></div>`;
  body.querySelector("#mkdir").addEventListener("click", async () => {
    const name = await promptModal(t("files.folderPrompt"));
    if (!name) return;
    try {
      await api(`/api/servers/${encodeURIComponent(id)}/files/mkdir`, {
        method: "POST",
        body: { path: joinPath(filesPath, name) }
      });
      loadFiles(id);
    } catch (e) { toastError(e); }
  });
  const uploadInput = body.querySelector("#upload-input");
  body.querySelector("#upload").addEventListener("click", () => uploadInput.click());
  uploadInput.addEventListener("change", async () => {
    for (const file of uploadInput.files) {
      const fd = new FormData();
      fd.append("file", file);
      try {
        const res = await fetch(
          `/api/servers/${encodeURIComponent(id)}/files/upload?path=${encodeURIComponent(filesPath)}`,
          { method: "POST", headers: { "X-Craftpanel": "1" }, body: fd }
        );
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          throw Object.assign(new Error(data.message || res.statusText), { code: data.error });
        }
        toast(t("files.uploaded") + ": " + file.name, "ok");
      } catch (e) { toastError(e); }
    }
    uploadInput.value = "";
    loadFiles(id);
  });
  loadFiles(id);
}

function joinPath(dir, name) {
  return dir === "." ? name : dir + "/" + name;
}

async function loadFiles(id) {
  const host = document.getElementById("files-body");
  if (!host) return;
  let entries;
  try {
    entries = await api(`/api/servers/${encodeURIComponent(id)}/files?path=${encodeURIComponent(filesPath)}`);
  } catch (e) {
    toastError(e);
    return;
  }
  renderCrumbs(id);
  if (entries.length === 0) {
    host.innerHTML = `<div class="empty"><p>${t("files.empty")}</p></div>`;
    return;
  }
  host.innerHTML = `<table class="files">
    <thead><tr><th>${t("files.name")}</th><th>${t("files.size")}</th><th>${t("files.modified")}</th><th></th></tr></thead>
    <tbody></tbody></table>`;
  const tbody = host.querySelector("tbody");
  for (const f of entries) {
    const full = joinPath(filesPath, f.name);
    const tr = el(`<tr>
      <td><span class="fname">${f.dir ? ICONS.folder : ICONS.file} ${esc(f.name)}</span></td>
      <td class="fsize">${f.dir ? "" : fmtSize(f.size)}</td>
      <td class="fdate">${fmtDate(f.modTime)}</td>
      <td class="facts"></td>
    </tr>`);
    tr.querySelector(".fname").addEventListener("click", () => {
      if (f.dir) {
        filesPath = full;
        loadFiles(id);
      } else {
        openEditor(id, full, f.size);
      }
    });
    const acts = tr.querySelector(".facts");
    const btn = (icon, title, fn) => {
      const b = el(`<button title="${esc(title)}">${icon}</button>`);
      b.addEventListener("click", fn);
      acts.appendChild(b);
    };
    if (!f.dir) {
      btn(ICONS.download, t("files.download"), () => {
        const a = document.createElement("a");
        a.href = `/api/servers/${encodeURIComponent(id)}/file?path=${encodeURIComponent(full)}&dl=1`;
        document.body.appendChild(a);
        a.click();
        a.remove();
      });
      btn(ICONS.pencil, t("files.edit"), () => openEditor(id, full, f.size));
    }
    btn(ICONS.tag, t("files.rename"), async () => {
      const name = await promptModal(t("files.renamePrompt"), f.name);
      if (!name || name === f.name) return;
      try {
        await api(`/api/servers/${encodeURIComponent(id)}/files/rename`, {
          method: "POST",
          body: { from: full, to: joinPath(filesPath, name) }
        });
        loadFiles(id);
      } catch (e) { toastError(e); }
    });
    btn(ICONS.trash, t("files.delete"), async () => {
      if (!(await confirmModal(t("files.deleteConfirm", { name: f.name }), true))) return;
      try {
        await api(`/api/servers/${encodeURIComponent(id)}/file?path=${encodeURIComponent(full)}`, { method: "DELETE" });
        loadFiles(id);
      } catch (e) { toastError(e); }
    });
    tbody.appendChild(tr);
  }
}

function renderCrumbs(id) {
  const crumbs = document.getElementById("crumbs");
  if (!crumbs) return;
  crumbs.innerHTML = "";
  const rootBtn = el(`<button>/</button>`);
  rootBtn.addEventListener("click", () => { filesPath = "."; loadFiles(id); });
  crumbs.appendChild(rootBtn);
  if (filesPath === ".") return;
  const parts = filesPath.split("/");
  parts.forEach((part, i) => {
    crumbs.appendChild(el(`<span class="sep">/</span>`));
    const b = el(`<button>${esc(part)}</button>`);
    const target = parts.slice(0, i + 1).join("/");
    b.addEventListener("click", () => { filesPath = target; loadFiles(id); });
    crumbs.appendChild(b);
  });
}

async function openEditor(id, path, size) {
  if (size > 2 * 1024 * 1024) {
    toast(t("error.generic") + ": " + path, "err");
    return;
  }
  let text;
  try {
    const res = await fetch(`/api/servers/${encodeURIComponent(id)}/file?path=${encodeURIComponent(path)}`);
    if (!res.ok) {
      const data = await res.json().catch(() => ({}));
      toast(data.message || res.statusText, "err");
      return;
    }
    text = await res.text();
  } catch {
    toast(t("error.generic"), "err");
    return;
  }
  const box = el(`<div>
    <h2>${esc(path)}</h2>
    <textarea class="editor" spellcheck="false"></textarea>
    <div class="modal-actions">
      <button class="btn btn-ghost" id="ed-cancel">${t("misc.cancel")}</button>
      <button class="btn btn-primary" id="ed-save">${t("files.save")}</button>
    </div>
  </div>`);
  box.querySelector("textarea").value = text;
  openModal(box, true);
  box.querySelector("#ed-cancel").addEventListener("click", closeModal);
  box.querySelector("#ed-save").addEventListener("click", async () => {
    try {
      await api(`/api/servers/${encodeURIComponent(id)}/file?path=${encodeURIComponent(path)}`, {
        method: "PUT",
        rawBody: box.querySelector("textarea").value
      });
      toast(t("files.saved"), "ok");
      closeModal();
      loadFiles(id);
    } catch (e) { toastError(e); }
  });
}

/* ---------- players tab ---------- */

// reasonModal asks for confirmation plus an optional reason. Resolves to
// null on cancel, or {reason} on confirm.
function reasonModal(text) {
  return new Promise((resolve) => {
    const box = el(`<div>
      <p>${esc(text)}</p>
      <label class="field"><span>${t("players.reason")}</span>
        <input type="text" id="rm-reason" maxlength="100"></label>
      <div class="modal-actions">
        <button class="btn btn-ghost" id="rm-cancel">${t("misc.cancel")}</button>
        <button class="btn btn-danger" id="rm-ok">OK</button>
      </div>
    </div>`);
    openModal(box);
    box.querySelector("#rm-reason").focus();
    const done = (v) => { closeModal(); resolve(v); };
    box.querySelector("#rm-cancel").addEventListener("click", () => done(null));
    box.querySelector("#rm-ok").addEventListener("click", () =>
      done({ reason: box.querySelector("#rm-reason").value.trim() }));
  });
}

async function playerAction(id, action, name, reason) {
  try {
    await api(`/api/servers/${encodeURIComponent(id)}/players/action`, {
      method: "POST",
      body: { action, name, reason: reason || "" }
    });
    toast(t("players.done"), "ok");
    setTimeout(() => loadPlayers(id), 900);
  } catch (e) {
    toastError(e);
  }
}

function renderPlayersTab(id) {
  const body = document.getElementById("tab-body");
  body.innerHTML = `
    <div class="panel">
      <h2>${t("players.online")}</h2>
      <div id="pl-online">${t("misc.loading")}</div>
    </div>
    <div class="panel" id="pl-banned-panel" hidden>
      <h2>${t("players.banned")}</h2>
      <div id="pl-banned"></div>
    </div>`;
  loadPlayers(id);
  stopTabTimer();
  tabTimer = setInterval(() => loadPlayers(id), 8000);
}

async function loadPlayers(id) {
  const host = document.getElementById("pl-online");
  if (!host) return;
  let info;
  try {
    info = await api(`/api/servers/${encodeURIComponent(id)}/players`);
  } catch (e) {
    if (e.status !== 401) toastError(e);
    return;
  }

  if (!info.running) {
    host.innerHTML = `<p class="hint">${t("players.notRunning")}</p>`;
  } else if (info.online.length === 0) {
    host.innerHTML = `<p class="hint">${t("players.none")}${info.max ? ` (0/${info.max})` : ""}</p>`;
  } else {
    host.innerHTML = `<p class="hint">${info.online.length}/${info.max || "?"}</p><ul class="plist" id="pl-list"></ul>
      ${info.bedrock ? `<p class="hint">${t("players.bedrockHint")}</p>` : ""}`;
    const ul = host.querySelector("#pl-list");
    for (const pl of info.online) {
      const li = el(`<li>
        <span class="pname">${esc(pl.name)}${pl.op ? ` <span class="badge st-running"><i class="led"></i>OP</span>` : ""}</span>
        <span class="pacts"></span>
      </li>`);
      const acts = li.querySelector(".pacts");
      const btn = (label, cls, fn) => {
        const b = el(`<button class="btn btn-sm ${cls}">${label}</button>`);
        b.addEventListener("click", fn);
        acts.appendChild(b);
      };
      if (pl.op) {
        btn(t("players.deop"), "", async () => {
          if (await confirmModal(t("players.confirmDeop", { name: pl.name }), false)) {
            playerAction(id, "deop", pl.name);
          }
        });
      } else {
        btn(t("players.op"), "", async () => {
          if (await confirmModal(t("players.confirmOp", { name: pl.name }), false)) {
            playerAction(id, "op", pl.name);
          }
        });
      }
      btn(t("players.kick"), "", async () => {
        const res = await reasonModal(t("players.confirmKick", { name: pl.name }));
        if (res) playerAction(id, "kick", pl.name, res.reason);
      });
      if (!info.bedrock) {
        btn(t("players.ban"), "btn-danger", async () => {
          const res = await reasonModal(t("players.confirmBan", { name: pl.name }));
          if (res) playerAction(id, "ban", pl.name, res.reason);
        });
      }
      ul.appendChild(li);
    }
  }

  const bannedPanel = document.getElementById("pl-banned-panel");
  const bannedHost = document.getElementById("pl-banned");
  if (!bannedPanel || !bannedHost) return;
  if (info.bedrock) {
    bannedPanel.hidden = true;
    return;
  }
  bannedPanel.hidden = false;
  if (info.banned.length === 0) {
    bannedHost.innerHTML = `<p class="hint">${t("players.noBanned")}</p>`;
    return;
  }
  bannedHost.innerHTML = `<ul class="plist" id="pl-ban-list"></ul>`;
  const ul = bannedHost.querySelector("#pl-ban-list");
  for (const b of info.banned) {
    const li = el(`<li>
      <span class="pname">${esc(b.name)}${b.reason ? ` <span class="pban-reason">${esc(b.reason)}</span>` : ""}</span>
      <span class="pacts"></span>
    </li>`);
    const un = el(`<button class="btn btn-sm">${t("players.pardon")}</button>`);
    un.addEventListener("click", async () => {
      if (await confirmModal(t("players.confirmPardon", { name: b.name }), false)) {
        playerAction(id, "pardon", b.name);
      }
    });
    li.querySelector(".pacts").appendChild(un);
    ul.appendChild(li);
  }
}

/* ---------- plugins tab (paper) ---------- */

function renderPluginsTab(id) {
  const body = document.getElementById("tab-body");
  body.innerHTML = `
    <div class="panel">
      <h2>${t("plugins.installed")}</h2>
      <p class="hint">${t("plugins.hint")}</p>
      <div class="files-bar">
        <button class="btn btn-sm btn-primary" id="plg-upload">${ICONS.upload} ${t("plugins.upload")}</button>
        <input type="file" id="plg-file" accept=".jar" multiple hidden>
      </div>
      <div id="plg-list">${t("misc.loading")}</div>
    </div>
    <div class="panel">
      <h2>${t("plugins.search")}</h2>
      <div class="add-row">
        <input type="text" id="plg-query" placeholder="${esc(t("plugins.searchPlaceholder"))}">
        <select id="plg-sort" class="plg-sort">
          <option value="relevance">${t("plugins.sortRelevance")}</option>
          <option value="downloads">${t("plugins.sortDownloads")}</option>
          <option value="follows">${t("plugins.sortFollows")}</option>
          <option value="newest">${t("plugins.sortNewest")}</option>
          <option value="updated">${t("plugins.sortUpdated")}</option>
        </select>
        <button class="btn btn-sm btn-primary" id="plg-go">${t("plugins.searchBtn")}</button>
      </div>
      <h3 class="sub" id="plg-results-head" hidden></h3>
      <div id="plg-results"></div>
    </div>`;

  const fileInput = body.querySelector("#plg-file");
  body.querySelector("#plg-upload").addEventListener("click", () => fileInput.click());
  fileInput.addEventListener("change", async () => {
    for (const file of fileInput.files) {
      const fd = new FormData();
      fd.append("file", file);
      try {
        const res = await fetch(`/api/servers/${encodeURIComponent(id)}/plugins/upload`,
          { method: "POST", headers: { "X-Craftpanel": "1" }, body: fd });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          throw Object.assign(new Error(data.message || res.statusText), { code: data.error });
        }
        toast(t("files.uploaded") + ": " + file.name, "ok");
      } catch (e) { toastError(e); }
    }
    fileInput.value = "";
    loadPluginList(id);
  });

  const doSearch = async () => {
    const q = body.querySelector("#plg-query").value.trim();
    const sort = body.querySelector("#plg-sort").value;
    const host = body.querySelector("#plg-results");
    const head = body.querySelector("#plg-results-head");
    head.hidden = false;
    head.textContent = q ? t("plugins.results") : t("plugins.popular");
    host.innerHTML = `<p class="hint">${t("misc.loading")}</p>`;
    let hits;
    try {
      hits = await api(`/api/servers/${encodeURIComponent(id)}/plugins/search?q=${encodeURIComponent(q)}&sort=${encodeURIComponent(sort)}`);
    } catch (e) { host.innerHTML = ""; toastError(e); return; }
    if (hits.length === 0) {
      host.innerHTML = `<p class="hint">${t("plugins.noResults")}</p>`;
      return;
    }
    host.innerHTML = `<ul class="plist"></ul>`;
    const ul = host.querySelector("ul");
    for (const hit of hits) {
      const li = el(`<li class="plg-hit">
        <span class="pname"><b>${esc(hit.title)}</b><span class="plg-desc">${esc(hit.description)}</span>
          <span class="plg-dl">${hit.downloads.toLocaleString()} ${t("plugins.downloads")}</span></span>
        <span class="pacts"></span>
      </li>`);
      const btn = el(`<button class="btn btn-sm ${hit.installed ? "" : "btn-ok"}" ${hit.installed ? "disabled" : ""}>
        ${hit.installed ? t("plugins.alreadyInstalled") : t("plugins.install")}</button>`);
      if (!hit.installed) {
        btn.addEventListener("click", async () => {
          btn.disabled = true;
          btn.textContent = t("misc.loading");
          try {
            const entry = await api(`/api/servers/${encodeURIComponent(id)}/plugins/install`,
              { method: "POST", body: { projectId: hit.projectId } });
            toast(t("plugins.installedOk", { name: entry.title, v: entry.version }), "ok");
            btn.textContent = t("plugins.alreadyInstalled");
            loadPluginList(id);
          } catch (e) {
            btn.disabled = false;
            btn.textContent = t("plugins.install");
            toastError(e);
          }
        });
      }
      li.querySelector(".pacts").appendChild(btn);
      ul.appendChild(li);
    }
  };
  body.querySelector("#plg-go").addEventListener("click", doSearch);
  body.querySelector("#plg-sort").addEventListener("change", doSearch);
  body.querySelector("#plg-query").addEventListener("keydown", (e) => {
    if (e.key === "Enter") { e.preventDefault(); doSearch(); }
  });

  loadPluginList(id);
  doSearch(); // empty query shows the most popular plugins as suggestions
}

async function loadPluginList(id) {
  const host = document.getElementById("plg-list");
  if (!host) return;
  let list;
  try {
    list = await api(`/api/servers/${encodeURIComponent(id)}/plugins?check=1`);
  } catch (e) {
    host.textContent = "";
    if (e.status !== 401) toastError(e);
    return;
  }
  if (list.length === 0) {
    host.innerHTML = `<p class="hint">${t("plugins.none")}</p>`;
    return;
  }
  host.innerHTML = `<ul class="plist"></ul>`;
  const ul = host.querySelector("ul");
  for (const p of list) {
    const label = p.title ? `<b>${esc(p.title)}</b> <span class="plg-desc">${esc(p.file)}</span>` : esc(p.file);
    const li = el(`<li>
      <span class="pname">${label}
        ${p.version ? `<span class="badge">${esc(p.version)}</span>` : `<span class="plg-desc">${t("plugins.manual")}</span>`}
        <span class="plg-dl">${fmtSize(p.size)}</span></span>
      <span class="pacts"></span>
    </li>`);
    const acts = li.querySelector(".pacts");
    if (p.updateAvailable) {
      const up = el(`<button class="btn btn-sm btn-ok">${esc(t("plugins.update", { v: p.newVersion }))}</button>`);
      up.addEventListener("click", async () => {
        up.disabled = true;
        up.textContent = t("misc.loading");
        try {
          await api(`/api/servers/${encodeURIComponent(id)}/plugins/install`,
            { method: "POST", body: { projectId: p.projectId } });
          toast(t("plugins.installedOk", { name: p.title, v: p.newVersion }), "ok");
          loadPluginList(id);
        } catch (e) {
          up.disabled = false;
          toastError(e);
        }
      });
      acts.appendChild(up);
    }
    const del = el(`<button class="btn btn-sm btn-danger">${t("files.delete")}</button>`);
    del.addEventListener("click", async () => {
      if (!(await confirmModal(t("plugins.deleteConfirm", { name: p.title || p.file }), true))) return;
      try {
        await api(`/api/servers/${encodeURIComponent(id)}/plugins?file=${encodeURIComponent(p.file)}`, { method: "DELETE" });
        loadPluginList(id);
      } catch (e) { toastError(e); }
    });
    acts.appendChild(del);
    ul.appendChild(li);
  }
}

/* ---------- mods tab (fabric/forge/neoforge/quilt) ---------- */

function renderModsTab(id) {
  const body = document.getElementById("tab-body");
  body.innerHTML = `
    <div class="panel">
      <h2>${t("mods.installed")}</h2>
      <p class="hint">${t("mods.hint")}</p>
      <div class="files-bar">
        <button class="btn btn-sm btn-primary" id="mod-upload">${ICONS.upload} ${t("mods.upload")}</button>
        <input type="file" id="mod-file" accept=".jar" multiple hidden>
      </div>
      <div id="mod-list">${t("misc.loading")}</div>
    </div>
    <div class="panel">
      <h2>${t("mods.search")}</h2>
      <div class="add-row">
        <input type="text" id="mod-query" placeholder="${esc(t("mods.searchPlaceholder"))}">
        <select id="mod-sort" class="plg-sort">
          <option value="relevance">${t("plugins.sortRelevance")}</option>
          <option value="downloads">${t("plugins.sortDownloads")}</option>
          <option value="follows">${t("plugins.sortFollows")}</option>
          <option value="newest">${t("plugins.sortNewest")}</option>
          <option value="updated">${t("plugins.sortUpdated")}</option>
        </select>
        <button class="btn btn-sm btn-primary" id="mod-go">${t("mods.searchBtn")}</button>
      </div>
      <h3 class="sub" id="mod-results-head" hidden></h3>
      <div id="mod-results"></div>
    </div>`;

  const fileInput = body.querySelector("#mod-file");
  body.querySelector("#mod-upload").addEventListener("click", () => fileInput.click());
  fileInput.addEventListener("change", async () => {
    for (const file of fileInput.files) {
      const fd = new FormData();
      fd.append("file", file);
      try {
        const res = await fetch(`/api/servers/${encodeURIComponent(id)}/mods/upload`,
          { method: "POST", headers: { "X-Craftpanel": "1" }, body: fd });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          throw Object.assign(new Error(data.message || res.statusText), { code: data.error });
        }
        toast(t("files.uploaded") + ": " + file.name, "ok");
      } catch (e) { toastError(e); }
    }
    fileInput.value = "";
    loadModList(id);
  });

  const doSearch = async () => {
    const q = body.querySelector("#mod-query").value.trim();
    const sort = body.querySelector("#mod-sort").value;
    const host = body.querySelector("#mod-results");
    const head = body.querySelector("#mod-results-head");
    head.hidden = false;
    head.textContent = q ? t("mods.results") : t("mods.popular");
    host.innerHTML = `<p class="hint">${t("misc.loading")}</p>`;
    let hits;
    try {
      hits = await api(`/api/servers/${encodeURIComponent(id)}/mods/search?q=${encodeURIComponent(q)}&sort=${encodeURIComponent(sort)}`);
    } catch (e) { host.innerHTML = ""; toastError(e); return; }
    if (hits.length === 0) {
      host.innerHTML = `<p class="hint">${t("mods.noResults")}</p>`;
      return;
    }
    host.innerHTML = `<ul class="plist"></ul>`;
    const ul = host.querySelector("ul");
    for (const hit of hits) {
      const li = el(`<li class="plg-hit">
        <span class="pname"><b>${esc(hit.title)}</b><span class="plg-desc">${esc(hit.description)}</span>
          <span class="plg-dl">${hit.downloads.toLocaleString()} ${t("plugins.downloads")}</span></span>
        <span class="pacts"></span>
      </li>`);
      const btn = el(`<button class="btn btn-sm ${hit.installed ? "" : "btn-ok"}" ${hit.installed ? "disabled" : ""}>
        ${hit.installed ? t("mods.alreadyInstalled") : t("mods.install")}</button>`);
      if (!hit.installed) {
        btn.addEventListener("click", async () => {
          btn.disabled = true;
          btn.textContent = t("misc.loading");
          try {
            const entry = await api(`/api/servers/${encodeURIComponent(id)}/mods/install`,
              { method: "POST", body: { projectId: hit.projectId } });
            toast(t("mods.installedOk", { name: entry.title, v: entry.version }), "ok");
            btn.textContent = t("mods.alreadyInstalled");
            loadModList(id);
          } catch (e) {
            btn.disabled = false;
            btn.textContent = t("mods.install");
            toastError(e);
          }
        });
      }
      li.querySelector(".pacts").appendChild(btn);
      ul.appendChild(li);
    }
  };
  body.querySelector("#mod-go").addEventListener("click", doSearch);
  body.querySelector("#mod-query").addEventListener("keydown", (e) => {
    if (e.key === "Enter") { e.preventDefault(); doSearch(); }
  });
  doSearch();
  loadModList(id);
}

async function loadModList(id) {
  const host = document.getElementById("mod-list");
  if (!host) return;
  let list;
  try {
    list = await api(`/api/servers/${encodeURIComponent(id)}/mods?check=1`);
  } catch (e) {
    host.innerHTML = "";
    toastError(e);
    return;
  }
  if (list.length === 0) {
    host.innerHTML = `<p class="hint">${t("mods.none")}</p>`;
    return;
  }
  host.innerHTML = `<ul class="plist"></ul>`;
  const ul = host.querySelector("ul");
  for (const p of list) {
    const label = p.title ? `<b>${esc(p.title)}</b> <span class="plg-desc">${esc(p.file)}</span>` : esc(p.file);
    const li = el(`<li>
      <span class="pname">${label}
        ${p.version ? `<span class="badge">${esc(p.version)}</span>` : `<span class="plg-desc">${t("mods.manual")}</span>`}
        <span class="plg-dl">${fmtSize(p.size)}</span></span>
      <span class="pacts"></span>
    </li>`);
    const acts = li.querySelector(".pacts");
    if (p.updateAvailable) {
      const up = el(`<button class="btn btn-sm btn-ok">${esc(t("mods.update", { v: p.newVersion }))}</button>`);
      up.addEventListener("click", async () => {
        up.disabled = true;
        up.textContent = t("misc.loading");
        try {
          await api(`/api/servers/${encodeURIComponent(id)}/mods/install`,
            { method: "POST", body: { projectId: p.projectId } });
          toast(t("mods.installedOk", { name: p.title, v: p.newVersion }), "ok");
          loadModList(id);
        } catch (e) {
          up.disabled = false;
          toastError(e);
        }
      });
      acts.appendChild(up);
    }
    const del = el(`<button class="btn btn-sm btn-danger">${t("files.delete")}</button>`);
    del.addEventListener("click", async () => {
      if (!(await confirmModal(t("mods.deleteConfirm", { name: p.title || p.file }), true))) return;
      try {
        await api(`/api/servers/${encodeURIComponent(id)}/mods?file=${encodeURIComponent(p.file)}`, { method: "DELETE" });
        loadModList(id);
      } catch (e) { toastError(e); }
    });
    acts.appendChild(del);
    ul.appendChild(li);
  }
}

/* ---------- backups tab ---------- */

async function renderBackupsTab(id) {
  const body = document.getElementById("tab-body");
  body.innerHTML = `
    <div class="panel">
      <h2>${t("tabs.backups")}</h2>
      <div class="files-bar">
        <button class="btn btn-primary" id="bk-create">${ICONS.archive} ${t("backup.create")}</button>
        <span class="badge st-installing" id="bk-busy" hidden><i class="led"></i>${t("backup.busy")}</span>
      </div>
      <div id="bk-list">${t("misc.loading")}</div>
    </div>
    <div class="panel">
      <h2>${t("backup.auto")}</h2>
      <form id="bk-auto">
        <label class="check"><input type="checkbox" name="backupAuto"><span>${t("backup.auto")}</span></label>
        <div class="form-row">
          <label class="field"><span>${t("backup.time")}</span><input type="time" name="backupTime" value="04:00"></label>
          <label class="field"><span>${t("backup.keep")}</span><input type="number" name="backupKeep" min="1" max="365" value="7"></label>
        </div>
        <p class="hint">${t("backup.keepHint")}</p>
        <button class="btn btn-primary" type="submit">${t("settings.save")}</button>
      </form>
    </div>`;

  try {
    const s = await api("/api/servers/" + encodeURIComponent(id));
    const f = body.querySelector("#bk-auto");
    f.backupAuto.checked = !!s.backupAuto;
    if (s.backupTime) f.backupTime.value = s.backupTime;
    if (s.backupKeep) f.backupKeep.value = s.backupKeep;
  } catch {}

  body.querySelector("#bk-auto").addEventListener("submit", async (e) => {
    e.preventDefault();
    const f = e.target;
    try {
      await api("/api/servers/" + encodeURIComponent(id), {
        method: "PATCH",
        body: {
          backupAuto: f.backupAuto.checked,
          backupTime: f.backupTime.value,
          backupKeep: parseInt(f.backupKeep.value, 10) || 7
        }
      });
      toast(t("settings.saved"), "ok");
    } catch (err) { toastError(err); }
  });

  body.querySelector("#bk-create").addEventListener("click", async () => {
    try {
      await api(`/api/servers/${encodeURIComponent(id)}/backups`, { method: "POST", body: {} });
      toast(t("backup.started"), "ok");
      loadBackups(id);
    } catch (e) { toastError(e); }
  });

  loadBackups(id);
  stopTabTimer();
  tabTimer = setInterval(() => loadBackups(id), 5000);
}

async function loadBackups(id) {
  const host = document.getElementById("bk-list");
  if (!host) return;
  let list, s;
  try {
    [list, s] = await Promise.all([
      api(`/api/servers/${encodeURIComponent(id)}/backups`),
      api("/api/servers/" + encodeURIComponent(id))
    ]);
  } catch (e) {
    if (e.status !== 401) toastError(e);
    return;
  }
  const busy = document.getElementById("bk-busy");
  if (busy) busy.hidden = !s.backupBusy;

  if (list.length === 0) {
    host.innerHTML = `<p class="hint">${t("backup.empty")}</p>`;
    return;
  }
  host.innerHTML = `<table class="files">
    <thead><tr><th>${t("files.name")}</th><th>${t("files.size")}</th><th>${t("files.modified")}</th><th></th></tr></thead>
    <tbody></tbody></table>`;
  const tbody = host.querySelector("tbody");
  for (const b of list) {
    const tr = el(`<tr>
      <td><span class="fname">${ICONS.archive} ${esc(b.name)}</span></td>
      <td class="fsize">${fmtSize(b.size)}</td>
      <td class="fdate">${fmtDate(b.time)}</td>
      <td class="facts"></td>
    </tr>`);
    const acts = tr.querySelector(".facts");
    const btn = (icon, title, fn) => {
      const x = el(`<button title="${esc(title)}">${icon}</button>`);
      x.addEventListener("click", fn);
      acts.appendChild(x);
    };
    btn(ICONS.download, t("files.download"), () => {
      const a = document.createElement("a");
      a.href = `/api/servers/${encodeURIComponent(id)}/backups/download?name=${encodeURIComponent(b.name)}`;
      document.body.appendChild(a);
      a.click();
      a.remove();
    });
    btn(ICONS.restart, t("backup.restore"), async () => {
      if (!(await confirmModal(t("backup.restoreConfirm", { name: b.name }), true))) return;
      try {
        await api(`/api/servers/${encodeURIComponent(id)}/backups/restore`, { method: "POST", body: { name: b.name } });
        toast(t("backup.restoreStarted"), "ok");
        loadBackups(id);
      } catch (e) { toastError(e); }
    });
    btn(ICONS.trash, t("files.delete"), async () => {
      if (!(await confirmModal(t("backup.deleteConfirm", { name: b.name }), true))) return;
      try {
        await api(`/api/servers/${encodeURIComponent(id)}/backups?name=${encodeURIComponent(b.name)}`, { method: "DELETE" });
        loadBackups(id);
      } catch (e) { toastError(e); }
    });
    tbody.appendChild(tr);
  }
}

/* ---------- settings tab ---------- */

async function renderSettingsTab(id, s) {
  const body = document.getElementById("tab-body");
  body.innerHTML = `
    ${s.type === "velocity" ? "" : `<div class="panel" id="eula-panel">
      <h2>${t("eula.title")}</h2>
      <p class="hint">${t("eula.text")}
        <a href="https://aka.ms/MinecraftEULA" target="_blank" rel="noopener">${t("eula.link")}</a></p>
      <div id="eula-state"></div>
    </div>`}

    <div class="panel">
      <h2>${t("settings.title")}</h2>
      <form id="settings-form">
        <label class="field"><span>${t("settings.name")}</span>
          <input type="text" name="name" maxlength="40" value="${esc(s.name)}" required></label>
        ${s.type === "bedrock" ? "" : `<div class="form-row">
          <label class="field"><span>${t("settings.memory")}</span>
            <input type="number" name="memoryMB" min="512" max="65536" step="256" value="${s.memoryMB}"></label>
          <label class="field"><span>${t("settings.javaPath")}</span>
            <input type="text" name="javaPath" value="${esc(s.javaPath || "")}" placeholder="java"></label>
        </div>
        <p class="hint">${t("settings.javaPathHint")}</p>`}
        <label class="check"><input type="checkbox" name="autostart" ${s.autostart ? "checked" : ""}>
          <span>${t("settings.autostart")}</span></label>
        <label class="check"><input type="checkbox" name="restartOnCrash" ${s.restartOnCrash ? "checked" : ""}>
          <span>${t("settings.restartOnCrash")}</span></label>
        <button class="btn btn-primary" type="submit">${t("settings.save")}</button>
      </form>
    </div>

    <div class="panel">
      <h2>${t("restart.title")}</h2>
      <p class="hint">${t("restart.hint")}</p>
      <form id="rs-form">
        <label class="check"><input type="checkbox" name="restartAuto" ${s.restartAuto ? "checked" : ""}>
          <span>${t("restart.enable")}</span></label>
        <div class="form-row">
          <label class="field"><span>${t("restart.time")}</span>
            <input type="time" name="restartTime" value="${esc(s.restartTime || "04:00")}"></label>
          <label class="field"><span>${t("restart.warn")}</span>
            <input type="number" name="restartWarn" min="0" max="60" value="${s.restartWarn || 5}"></label>
        </div>
        <p class="hint">${t("restart.warnHint")}</p>
        <button class="btn btn-primary" type="submit">${t("settings.save")}</button>
      </form>
    </div>

    ${s.modpack ? `<div class="panel">
      <h2>${t("modpack.infoTitle")}</h2>
      <p class="hint">${t("modpack.infoHint")}</p>
      <p><b>${esc(s.modpack.title || s.modpack.slug || "")}</b>
        ${s.modpack.version ? `<span class="badge">${esc(s.modpack.version)}</span>` : ""}</p>
      <p class="hint">${esc(s.type)} ${esc(s.version)}${s.loaderVersion ? " · loader " + esc(s.loaderVersion) : ""}</p>
      <div class="form-row">
        <label class="field"><span>${t("modpack.changeVersion")}</span>
          <select id="mp-up-version"><option>${t("create.loadingVersions")}</option></select></label>
      </div>
      <button class="btn" id="mp-up-btn">${ICONS.restart} ${t("modpack.upgrade")}</button>
    </div>` : `<div class="panel">
      <h2>${t("upgrade.title")}</h2>
      <p class="hint">${t("upgrade.hint")}</p>
      <div class="form-row">
        <label class="field"><span>${t("create.version")}</span>
          <select id="up-version"><option>${t("create.loadingVersions")}</option></select></label>
      </div>
      <button class="btn" id="up-btn">${ICONS.restart} ${t("upgrade.button")}</button>
    </div>`}

    ${s.type === "velocity" ? "" : `<div class="panel">
      <h2>${t("access.title")}</h2>
      <div id="access-body">${t("misc.loading")}</div>
    </div>`}

    <div class="panel">
      <h2>${t("discord.title")}</h2>
      <p class="hint">${t("discord.hint")}</p>
      <form id="dc-form">
        <label class="field"><span>${t("discord.webhook")}</span>
          <input type="text" name="webhook" value="${esc((s.discord && s.discord.webhook) || "")}" placeholder="https://discord.com/api/webhooks/..."></label>
        <label class="field"><span>${t("discord.lang")}</span>
          <select name="lang">
            <option value="de" ${s.discord && s.discord.lang === "de" ? "selected" : ""}>Deutsch</option>
            <option value="en" ${!s.discord || s.discord.lang !== "de" ? "selected" : ""}>English</option>
          </select></label>
        <label class="check"><input type="checkbox" name="status" ${s.discord && s.discord.status ? "checked" : ""}>
          <span>${t("discord.status")}</span></label>
        <label class="check"><input type="checkbox" name="backups" ${s.discord && s.discord.backups ? "checked" : ""}>
          <span>${t("discord.backups")}</span></label>
        <label class="check"><input type="checkbox" name="players" ${s.discord && s.discord.players ? "checked" : ""}>
          <span>${t("discord.players")}</span></label>
        ${s.type === "bedrock" || s.type === "velocity"
          ? ""
          : `<label class="check"><input type="checkbox" name="chat" ${s.discord && s.discord.chat ? "checked" : ""}>
              <span>${t("discord.chat")}</span></label>`}
        <div class="modal-actions">
          <button type="button" class="btn" id="dc-test">${t("discord.test")}</button>
          <button type="submit" class="btn btn-primary">${t("settings.save")}</button>
        </div>
      </form>
    </div>

    ${s.type === "velocity" ? "" : `<div class="panel">
      <h2>${t("props.title")}</h2>
      <p class="hint">${t("props.hint")}</p>
      <div id="props-body">${t("misc.loading")}</div>
    </div>`}

    <div class="panel danger">
      <h2>${t("danger.title")}</h2>
      <p class="hint">${t("danger.text")}</p>
      <label class="field"><span>${t("danger.confirm")}</span>
        <input type="text" id="del-confirm" autocomplete="off"></label>
      <button class="btn btn-danger" id="del-btn" disabled>${ICONS.trash} ${t("danger.delete")}</button>
    </div>`;

  if (s.type !== "velocity") renderEulaState(id, s.eula);

  body.querySelector("#settings-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const f = e.target;
    try {
      const patch = {
        name: f.name.value.trim(),
        autostart: f.autostart.checked,
        restartOnCrash: f.restartOnCrash.checked
      };
      if (f.memoryMB) patch.memoryMB = parseInt(f.memoryMB.value, 10);
      if (f.javaPath) patch.javaPath = f.javaPath.value.trim();
      const updated = await api("/api/servers/" + encodeURIComponent(id), {
        method: "PATCH",
        body: patch
      });
      toast(t("settings.saved"), "ok");
      renderDetailHead(updated);
    } catch (err) { toastError(err); }
  });

  const delInput = body.querySelector("#del-confirm");
  const delBtn = body.querySelector("#del-btn");
  delInput.addEventListener("input", () => {
    delBtn.disabled = delInput.value !== s.name;
  });
  delBtn.addEventListener("click", async () => {
    try {
      await api("/api/servers/" + encodeURIComponent(id), { method: "DELETE" });
      location.hash = "#/";
    } catch (e) { toastError(e); }
  });

  if (s.modpack) {
    (async () => {
      const sel = body.querySelector("#mp-up-version");
      try {
        const list = await api(`/api/modpacks/${encodeURIComponent(s.modpack.projectId)}/versions`);
        sel.innerHTML = list.map((v) =>
          `<option value="${esc(v.id)}" ${v.id === s.modpack.versionId ? "selected" : ""}>${esc(v.name || v.versionNumber)} (${esc((v.gameVersions || []).join(", "))}${v.loaders && v.loaders.length ? " · " + esc(v.loaders.join(", ")) : ""})</option>`
        ).join("");
      } catch {
        sel.innerHTML = "<option></option>";
      }
    })();
    body.querySelector("#mp-up-btn").addEventListener("click", async () => {
      const v = body.querySelector("#mp-up-version").value;
      if (!v || v === s.modpack.versionId) return;
      if (!(await confirmModal(t("modpack.upgradeConfirm"), false))) return;
      try {
        await api(`/api/servers/${encodeURIComponent(id)}/modpack/upgrade`, { method: "POST", body: { versionId: v } });
        toast(t("modpack.upgradeStarted"), "ok");
        updateDetailHead(id);
      } catch (e) { toastError(e); }
    });
  } else {
    (async () => {
      const sel = body.querySelector("#up-version");
      try {
        const list = await api("/api/versions?type=" + s.type);
        sel.innerHTML = list.map((v) =>
          `<option value="${esc(v.id)}" ${v.id === s.version ? "selected" : ""}>${esc(v.id)}${v.latest ? " (latest)" : ""}</option>`).join("");
      } catch {
        sel.innerHTML = "<option></option>";
      }
    })();
    body.querySelector("#up-btn").addEventListener("click", async () => {
      const v = body.querySelector("#up-version").value;
      if (!v || v === s.version) return;
      if (!(await confirmModal(t("upgrade.confirm", { version: v }), false))) return;
      try {
        await api(`/api/servers/${encodeURIComponent(id)}/upgrade`, { method: "POST", body: { version: v } });
        toast(t("upgrade.started"), "ok");
        updateDetailHead(id);
      } catch (e) { toastError(e); }
    });
  }

  const rsForm = body.querySelector("#rs-form");
  rsForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    try {
      await api("/api/servers/" + encodeURIComponent(id), {
        method: "PATCH",
        body: {
          restartAuto: rsForm.restartAuto.checked,
          restartTime: rsForm.restartTime.value,
          restartWarn: parseInt(rsForm.restartWarn.value, 10) || 0
        }
      });
      toast(t("settings.saved"), "ok");
    } catch (err) { toastError(err); }
  });

  const dcForm = body.querySelector("#dc-form");
  dcForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    try {
      await api("/api/servers/" + encodeURIComponent(id), {
        method: "PATCH",
        body: {
          discord: {
            webhook: dcForm.webhook.value.trim(),
            lang: dcForm.lang.value,
            status: dcForm.status.checked,
            backups: dcForm.backups.checked,
            players: dcForm.players.checked,
            chat: dcForm.chat ? dcForm.chat.checked : false
          }
        }
      });
      toast(t("settings.saved"), "ok");
    } catch (err) { toastError(err); }
  });
  body.querySelector("#dc-test").addEventListener("click", async () => {
    try {
      await api(`/api/servers/${encodeURIComponent(id)}/discord/test`, { method: "POST", body: {} });
      toast(t("discord.testOk"), "ok");
    } catch (err) { toastError(err); }
  });

  if (s.type !== "velocity") {
    loadAccess(id);
    loadProperties(id, s.type);
  }
}

/* ---------- velocity network ---------- */

function renderNetworkTab(id) {
  const body = document.getElementById("tab-body");
  body.innerHTML = `
    <div class="panel">
      <h2>${t("network.title")}</h2>
      <p class="hint">${t("network.hint")}</p>
      <div id="net-body">${t("misc.loading")}</div>
    </div>`;
  loadNetwork(id);
}

async function loadNetwork(id) {
  const host = document.getElementById("net-body");
  if (!host) return;
  let info;
  try {
    info = await api(`/api/servers/${encodeURIComponent(id)}/network`);
  } catch (e) { host.textContent = ""; toastError(e); return; }

  if (info.backends.length === 0) {
    host.innerHTML = `<p class="hint">${t("network.noBackends")}</p>`;
    return;
  }
  // With a domain mapping every linked backend gets its own hostname via
  // forced hosts; players need no port when the proxy listens on 25565 or
  // the panel manages SRV records.
  let domainHint = "";
  if (info.domain) {
    domainHint = `<p class="hint">${esc(t("network.domainHint", { domain: info.domain }))}</p>`;
    if (info.proxyPort !== 25565 && !info.dnsManaged) {
      domainHint += `<div class="notice">${esc(t("network.domainPortWarn", { port: info.proxyPort }))}</div>`;
    }
  }
  host.innerHTML = `${domainHint}<ul class="plist" id="net-list"></ul>
    <div class="modal-actions"><button class="btn btn-primary" id="net-save">${t("network.save")}</button></div>
    <div id="net-warn"></div>`;
  const ul = host.querySelector("#net-list");
  for (const b of info.backends) {
    const mapped = info.domain ? ` <span class="plg-desc">&rarr; ${esc(b.id + "." + info.domain)}</span>` : "";
    const li = el(`<li>
      <label class="check" style="margin:0"><input type="checkbox" ${b.linked ? "checked" : ""} data-sid="${esc(b.id)}">
        <span><b>${esc(b.name)}</b> <span class="plg-desc">${t("misc.port")} ${b.port}</span>${mapped}</span></label>
    </li>`);
    ul.appendChild(li);
  }
  host.querySelector("#net-save").addEventListener("click", async () => {
    const servers = [...ul.querySelectorAll("input:checked")].map((i) => i.dataset.sid);
    try {
      const res = await api(`/api/servers/${encodeURIComponent(id)}/network`, { method: "PUT", body: { servers } });
      toast(t("network.saved"), "ok");
      const warnHost = host.querySelector("#net-warn");
      warnHost.innerHTML = (res.warnings || []).map((w) => `<div class="notice">${esc(w)}</div>`).join("");
    } catch (e) { toastError(e); }
  });
}

/* ---------- whitelist and ops ---------- */

async function loadAccess(id) {
  const host = document.getElementById("access-body");
  if (!host) return;
  let info;
  try {
    info = await api(`/api/servers/${encodeURIComponent(id)}/access`);
  } catch (e) {
    host.textContent = "";
    if (e.status !== 401) toastError(e);
    return;
  }
  const opsBlock = info.bedrock ? "" : `
    <h3 class="sub">${t("access.opsTitle")}</h3>
    <ul class="plist" id="op-list"></ul>
    <div class="add-row">
      <input type="text" id="op-name" placeholder="${esc(t("access.placeholder"))}" maxlength="16">
      <button class="btn btn-sm btn-primary" id="op-add">${t("access.add")}</button>
    </div>`;
  host.innerHTML = `
    ${info.bedrock ? `<p class="hint">${t("access.bedrockHint")}</p>` : ""}
    ${info.bedrock || info.onlineMode ? "" : `<p class="hint">${t("access.offlineHint")}</p>`}
    <label class="check"><input type="checkbox" id="wl-mode" ${info.whitelistOn ? "checked" : ""}>
      <span>${t("access.enforce")}</span></label>
    <h3 class="sub">${info.bedrock ? t("access.allowlistTitle") : t("access.whitelistTitle")}</h3>
    <ul class="plist" id="wl-list"></ul>
    <div class="add-row">
      <input type="text" id="wl-name" placeholder="${esc(info.bedrock ? t("access.gamertag") : t("access.placeholder"))}" maxlength="20">
      <button class="btn btn-sm btn-primary" id="wl-add">${t("access.add")}</button>
    </div>
    ${opsBlock}`;

  const fillList = (ul, entries, list) => {
    ul.innerHTML = "";
    if (!entries.length) {
      ul.appendChild(el(`<li class="pempty">${t("access.empty")}</li>`));
      return;
    }
    for (const entry of entries) {
      const li = el(`<li><span>${esc(entry.name)}</span><button title="${esc(t("files.delete"))}">${ICONS.trash}</button></li>`);
      li.querySelector("button").addEventListener("click", async () => {
        try {
          await api(`/api/servers/${encodeURIComponent(id)}/access/${list}?name=${encodeURIComponent(entry.name)}`, { method: "DELETE" });
          loadAccess(id);
        } catch (err) { toastError(err); }
      });
      ul.appendChild(li);
    }
  };
  fillList(host.querySelector("#wl-list"), info.whitelist, "whitelist");
  if (!info.bedrock) fillList(host.querySelector("#op-list"), info.ops, "ops");

  host.querySelector("#wl-mode").addEventListener("change", async (e) => {
    try {
      await api(`/api/servers/${encodeURIComponent(id)}/access/whitelist-mode`, { method: "PUT", body: { enabled: e.target.checked } });
    } catch (err) {
      toastError(err);
      loadAccess(id);
    }
  });

  const wire = (inputSel, btnSel, list) => {
    const input = host.querySelector(inputSel);
    const add = async () => {
      const name = input.value.trim();
      if (!name) return;
      try {
        await api(`/api/servers/${encodeURIComponent(id)}/access/${list}`, { method: "POST", body: { name } });
        input.value = "";
        loadAccess(id);
      } catch (err) { toastError(err); }
    };
    host.querySelector(btnSel).addEventListener("click", add);
    input.addEventListener("keydown", (e) => {
      if (e.key === "Enter") { e.preventDefault(); add(); }
    });
  };
  wire("#wl-name", "#wl-add", "whitelist");
  if (!info.bedrock) wire("#op-name", "#op-add", "ops");
}

/* ---------- self update ---------- */

// renderVersionLine shows the installed version in the panel settings modal,
// with a manual update check and, when one is available, the update button.
function renderVersionLine(box) {
  const host = box.querySelector("#ps-version");
  if (!host) return;
  const upd = sys && sys.updateAvailable
    ? ` · ${esc(t("panel.updateAvailable", { v: sys.latest }))} <button class="btn btn-ok btn-sm" id="ps-update">${t("update.now")}</button>`
    : "";
  host.innerHTML = `${t("panel.version")}: ${sys ? esc(sys.version) : "?"}${upd}
    <button class="btn btn-ghost btn-sm" id="ps-check">${t("panel.checkUpdate")}</button>`;
  const updBtn = host.querySelector("#ps-update");
  if (updBtn) {
    updBtn.addEventListener("click", (e) => {
      e.target.disabled = true;
      doSelfUpdate();
    });
  }
  host.querySelector("#ps-check").addEventListener("click", async (e) => {
    e.target.disabled = true;
    try {
      const res = await api("/api/system/check-update", { method: "POST", body: {} });
      if (sys) {
        sys.latest = res.latest;
        sys.updateAvailable = res.updateAvailable;
      }
      if (!res.latest) toast(t("panel.checkFailed"), "err");
      else if (res.updateAvailable) toast(t("panel.updateAvailable", { v: res.latest }), "ok");
      else toast(t("panel.upToDate", { v: res.version }), "ok");
    } catch (err) {
      toastError(err);
    }
    renderVersionLine(box);
  });
}

// Triggers the in-place panel update, then waits for the restarted process to
// come back with a different version before reloading the page.
async function doSelfUpdate() {
  const oldVersion = sys ? sys.version : "";
  try {
    await api("/api/system/update", { method: "POST", body: {} });
  } catch (e) {
    toastError(e);
    return;
  }
  closeModal();
  stopPolling();
  stopTabTimer();
  const wait = el(`<div class="overlay"><div class="modal"><h2>${t("update.updating")}</h2>
    <p class="hint">${t("update.waiting")}</p></div></div>`);
  document.body.appendChild(wait);
  const poll = async () => {
    try {
      const res = await fetch("/api/setup-status", { cache: "no-store" });
      if (res.ok) {
        const st = await res.json();
        if (st.version && st.version !== oldVersion) {
          location.reload();
          return;
        }
      }
    } catch {}
    setTimeout(poll, 2000);
  };
  setTimeout(poll, 3000);
}

/* ---------- panel settings modal ---------- */

async function openPanelSettings() {
  let settings = { backupDir: "" };
  try { settings = await api("/api/settings"); } catch {}
  try {
    const info = await api("/api/me");
    meTotp = !!info.totp;
  } catch {}

  const box = el(`<div>
    <h2>${t("panel.title")}</h2>
    <p class="hint" id="ps-version"></p>
    <label class="field"><span>${t("panel.backupDir")}</span><input type="text" id="ps-backupdir"></label>
    <p class="hint">${esc(t("panel.backupDirHint"))}</p>
    <hr class="sep-line">
    <h2>${t("panel.domainTitle")}</h2>
    <p class="hint">${esc(t("panel.domainHint"))}</p>
    <label class="field"><span>${t("panel.domain")}</span><input type="text" id="ps-domain" placeholder="mc.example.com" autocomplete="off"></label>
    <label class="field"><span>${t("panel.dnsProvider")}</span>
      <select id="ps-dnsprov">
        <option value="">${t("panel.dnsManual")}</option>
        <option value="cloudflare">${t("panel.dnsCloudflare")}</option>
      </select></label>
    <div id="ps-dns-cf">
      <label class="field"><span>${t("panel.dnsToken")}</span><input type="password" id="ps-dnstoken" autocomplete="off"></label>
      <p class="hint">${esc(t("panel.dnsTokenHint"))}</p>
      <label class="field"><span>${t("panel.dnsTarget")}</span><input type="text" id="ps-dnstarget" autocomplete="off"></label>
      <p class="hint">${esc(t("panel.dnsTargetHint"))}</p>
    </div>
    <div class="modal-actions"><button class="btn btn-primary" id="ps-save">${t("settings.save")}</button></div>
    <div id="ps-dns-sync" class="add-row" style="align-items:center">
      <button class="btn btn-sm" id="ps-dnssync">${t("panel.dnsSync")}</button>
      <span class="hint" id="ps-dnsstatus"></span>
    </div>
    <div id="ps-warnings"></div>
    <hr class="sep-line">
    <h2>${t("totp.title")}</h2>
    <div id="totp-body"></div>
    <div class="modal-actions"><button class="btn btn-ghost" id="ps-close">${t("misc.close")}</button></div>
  </div>`);
  openModal(box, true);
  renderVersionLine(box);
  box.querySelector("#ps-backupdir").value = settings.backupDir || "";
  box.querySelector("#ps-domain").value = settings.domain || "";
  box.querySelector("#ps-dnstarget").value = settings.dnsTarget || "";
  const prov = box.querySelector("#ps-dnsprov");
  prov.value = settings.dnsProvider || "";

  const applyDnsUI = () => {
    const cf = prov.value === "cloudflare";
    box.querySelector("#ps-dns-cf").style.display = cf ? "" : "none";
    box.querySelector("#ps-dns-sync").style.display = cf && settings.dnsTokenSet ? "" : "none";
    box.querySelector("#ps-dnstoken").placeholder = settings.dnsTokenSet ? t("panel.dnsTokenSaved") : "";
    renderDnsStatus(box.querySelector("#ps-dnsstatus"), settings.dns);
  };
  applyDnsUI();
  prov.addEventListener("change", applyDnsUI);

  box.querySelector("#ps-close").addEventListener("click", closeModal);
  box.querySelector("#ps-save").addEventListener("click", async () => {
    const body = {
      backupDir: box.querySelector("#ps-backupdir").value.trim(),
      domain: box.querySelector("#ps-domain").value.trim(),
      dnsProvider: prov.value,
      dnsTarget: box.querySelector("#ps-dnstarget").value.trim()
    };
    const token = box.querySelector("#ps-dnstoken").value.trim();
    if (token) body.dnsToken = token;
    try {
      settings = await api("/api/settings", { method: "PUT", body });
      box.querySelector("#ps-dnstoken").value = "";
      box.querySelector("#ps-warnings").innerHTML =
        (settings.warnings || []).map((w) => `<div class="notice">${esc(w)}</div>`).join("");
      applyDnsUI();
      toast(t("settings.saved"), "ok");
    } catch (e) { toastError(e); }
  });
  box.querySelector("#ps-dnssync").addEventListener("click", async (e) => {
    e.target.disabled = true;
    try {
      settings.dns = await api("/api/dns/sync", { method: "POST", body: {} });
      renderDnsStatus(box.querySelector("#ps-dnsstatus"), settings.dns);
      toast(t("panel.dnsSynced", { n: settings.dns.records }), "ok");
    } catch (err) { toastError(err); } finally { e.target.disabled = false; }
  });
  renderTotpBody(box.querySelector("#totp-body"));
}

function renderDnsStatus(host, st) {
  if (!host) return;
  if (!st || !st.lastSync) { host.textContent = t("panel.dnsNever"); return; }
  let text = st.ok
    ? t("panel.dnsOk", { time: fmtDate(st.lastSync), n: st.records, target: st.target })
    : t("panel.dnsFailed", { err: st.error || "?" });
  if (st.warnings && st.warnings.length) text += " — " + st.warnings.join("; ");
  host.textContent = text;
}

function renderTotpBody(host) {
  if (meTotp) {
    host.innerHTML = `<p class="hint">${t("totp.on")}</p>
      <div class="add-row">
        <input type="text" id="totp-code" placeholder="${esc(t("totp.code"))}" inputmode="numeric" maxlength="6" autocomplete="one-time-code">
        <button class="btn btn-danger btn-sm" id="totp-off">${t("totp.disable")}</button>
      </div>`;
    host.querySelector("#totp-off").addEventListener("click", async () => {
      try {
        await api("/api/account/totp/disable", { method: "POST", body: { code: host.querySelector("#totp-code").value.trim() } });
        meTotp = false;
        renderTotpBody(host);
        toast(t("totp.off"), "ok");
      } catch (e) { toastError(e); }
    });
    return;
  }
  host.innerHTML = `<p class="hint">${t("totp.off")}</p>
    <button class="btn btn-ok btn-sm" id="totp-start">${t("totp.enable")}</button>
    <div id="totp-setup"></div>`;
  host.querySelector("#totp-start").addEventListener("click", async () => {
    let init;
    try {
      init = await api("/api/account/totp/init", { method: "POST", body: {} });
    } catch (e) { toastError(e); return; }
    const setup = host.querySelector("#totp-setup");
    setup.innerHTML = `<p class="hint">${t("totp.setupHint")}</p>
      <p class="totp-secret"><code>${esc(init.secret)}</code></p>
      <p class="hint"><code class="wrap">${esc(init.url)}</code></p>
      <div class="add-row">
        <input type="text" id="totp-code2" placeholder="${esc(t("totp.code"))}" inputmode="numeric" maxlength="6" autocomplete="one-time-code">
        <button class="btn btn-ok btn-sm" id="totp-confirm">${t("totp.confirm")}</button>
      </div>`;
    setup.querySelector("#totp-confirm").addEventListener("click", async () => {
      try {
        await api("/api/account/totp/enable", { method: "POST", body: { code: setup.querySelector("#totp-code2").value.trim() } });
        meTotp = true;
        renderTotpBody(host);
        toast(t("totp.on"), "ok");
      } catch (e) { toastError(e); }
    });
  });
}

function renderEulaState(id, accepted) {
  const host = document.getElementById("eula-state");
  if (!host) return;
  if (accepted) {
    host.innerHTML = `<span class="badge st-running"><i class="led"></i>${t("eula.accepted")}</span>`;
    return;
  }
  host.innerHTML = `<span class="badge st-install_failed"><i class="led"></i>${t("eula.notAccepted")}</span>
    <button class="btn btn-ok" id="eula-accept">${t("eula.accept")}</button>`;
  host.querySelector("#eula-accept").addEventListener("click", async () => {
    try {
      await api(`/api/servers/${encodeURIComponent(id)}/eula`, { method: "POST", body: { accept: true } });
      renderEulaState(id, true);
      toast(t("eula.accepted"), "ok");
    } catch (e) { toastError(e); }
  });
}

/* Curated server.properties fields with friendly inputs. Everything not in
   this schema lives in the collapsible advanced table below. */
const PROP_SCHEMA = {
  java: [
    { key: "motd", type: "text" },
    { key: "max-players", type: "number", min: 1, max: 1000, def: "20" },
    { key: "gamemode", type: "select", options: ["survival", "creative", "adventure", "spectator"], def: "survival" },
    { key: "difficulty", type: "select", options: ["peaceful", "easy", "normal", "hard"], def: "easy" },
    { key: "hardcore", type: "bool", def: "false" },
    { key: "pvp", type: "bool", def: "true" },
    { key: "online-mode", type: "bool", def: "true" },
    { key: "level-seed", type: "text" },
    { key: "view-distance", type: "number", min: 3, max: 32, def: "10" },
    { key: "simulation-distance", type: "number", min: 3, max: 32, def: "10" },
    { key: "spawn-protection", type: "number", min: 0, max: 1000, def: "16" },
    { key: "allow-flight", type: "bool", def: "false" },
    { key: "enable-command-block", type: "bool", def: "false" },
    { key: "player-idle-timeout", type: "number", min: 0, max: 1440, def: "0" }
  ],
  bedrock: [
    { key: "server-name", type: "text" },
    { key: "max-players", type: "number", min: 1, max: 1000, def: "10" },
    { key: "gamemode", type: "select", options: ["survival", "creative", "adventure"], def: "survival" },
    { key: "difficulty", type: "select", options: ["peaceful", "easy", "normal", "hard"], def: "easy" },
    { key: "allow-cheats", type: "bool", def: "false" },
    { key: "online-mode", type: "bool", def: "true" },
    { key: "level-seed", type: "text" },
    { key: "view-distance", type: "number", min: 5, max: 96, def: "32" },
    { key: "player-idle-timeout", type: "number", min: 0, max: 1440, def: "30" },
    { key: "default-player-permission-level", type: "select", options: ["visitor", "member", "operator"], def: "member" },
    { key: "texturepack-required", type: "bool", def: "false" }
  ]
};

async function loadProperties(id, type) {
  const host = document.getElementById("props-body");
  if (!host) return;
  let props;
  try {
    props = await api(`/api/servers/${encodeURIComponent(id)}/properties`);
  } catch (e) {
    host.textContent = "";
    toastError(e);
    return;
  }
  const cur = {};
  for (const p of props) cur[p.key] = p.value;
  const schema = PROP_SCHEMA[type === "bedrock" ? "bedrock" : "java"];
  const curatedKeys = new Set(schema.map((f) => f.key));

  host.innerHTML = "";
  const grid = el(`<div class="props-grid"></div>`);
  host.appendChild(grid);

  // Each getter reports [key, currentValue, changedSinceLoad].
  const getters = [];
  for (const f of schema) {
    const inFile = Object.prototype.hasOwnProperty.call(cur, f.key);
    const orig = inFile ? cur[f.key] : (f.def !== undefined ? f.def : "");
    const label = t("prop." + f.key + ".label");
    const desc = t("prop." + f.key + ".desc");
    let cell, read;

    if (f.type === "bool") {
      cell = el(`<div class="prop-field">
        <label class="check"><input type="checkbox"><span>${esc(label)}</span></label>
        <p class="prop-desc">${esc(desc)}</p>
      </div>`);
      const input = cell.querySelector("input");
      input.checked = orig === "true";
      read = () => (input.checked ? "true" : "false");
    } else if (f.type === "select") {
      const opts = [...f.options];
      if (orig !== "" && !opts.includes(orig)) opts.unshift(orig);
      cell = el(`<div class="prop-field">
        <label class="field"><span>${esc(label)}</span>
          <select>${opts.map((o) =>
            `<option value="${esc(o)}" ${o === orig ? "selected" : ""}>${esc(STRINGS.en["opt." + o] ? t("opt." + o) : o)}</option>`).join("")}</select>
        </label>
        <p class="prop-desc">${esc(desc)}</p>
      </div>`);
      const input = cell.querySelector("select");
      read = () => input.value;
    } else {
      const typeAttr = f.type === "number"
        ? `type="number" ${f.min !== undefined ? `min="${f.min}"` : ""} ${f.max !== undefined ? `max="${f.max}"` : ""}`
        : `type="text"`;
      cell = el(`<div class="prop-field">
        <label class="field"><span>${esc(label)}</span><input ${typeAttr}></label>
        <p class="prop-desc">${esc(desc)}</p>
      </div>`);
      const input = cell.querySelector("input");
      input.value = orig;
      read = () => input.value.trim();
    }
    grid.appendChild(cell);
    getters.push(() => {
      const val = read();
      // Only write keys the user actually changed, so defaults for keys that
      // are not in the file yet do not get persisted unasked.
      return [f.key, val, val !== orig || (inFile && val !== cur[f.key])];
    });
  }

  // Advanced: raw view of everything else in the file.
  const advEntries = props.filter((p) => !curatedKeys.has(p.key));
  const adv = el(`<details class="adv">
    <summary>${t("props.advanced")} (${advEntries.length})</summary>
    <p class="hint">${t("props.advancedHint")}</p>
    <table class="kv-table"><tbody></tbody></table>
    <button type="button" class="btn btn-ghost btn-sm" id="prop-add">${ICONS.plus}</button>
  </details>`);
  const tbody = adv.querySelector("tbody");
  const addRow = (key, value, keyEditable) => {
    const tr = el(`<tr>
      <td><input type="text" class="pk" ${keyEditable ? "" : "readonly"}></td>
      <td><input type="text" class="pv"></td>
    </tr>`);
    tr.querySelector(".pk").value = key;
    tr.querySelector(".pv").value = value;
    tbody.appendChild(tr);
  };
  for (const p of advEntries) addRow(p.key, p.value, false);
  adv.querySelector("#prop-add").addEventListener("click", () => addRow("", "", true));
  host.appendChild(adv);

  const bar = el(`<div class="modal-actions">
    <button class="btn btn-primary" id="props-save">${t("props.save")}</button>
  </div>`);
  host.appendChild(bar);
  bar.querySelector("#props-save").addEventListener("click", async () => {
    const set = {};
    for (const get of getters) {
      const [key, val, changed] = get();
      if (changed && val !== "") set[key] = val;
    }
    tbody.querySelectorAll("tr").forEach((tr) => {
      const k = tr.querySelector(".pk").value.trim();
      const v = tr.querySelector(".pv").value;
      if (k) set[k] = v;
    });
    if (Object.keys(set).length === 0) return;
    try {
      await api(`/api/servers/${encodeURIComponent(id)}/properties`, { method: "PUT", body: set });
      toast(t("props.saved"), "ok");
      loadProperties(id, type);
    } catch (e) { toastError(e); }
  });
}

/* ---------- go ---------- */

boot();
