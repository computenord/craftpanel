// ComputeBox Craftpanel single page app. No framework, no build step.
"use strict";

const $app = document.getElementById("app");

let me = null;
let meAdmin = false;
let meTotp = false;
let meRecoveryLeft = 0;
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
  archive: '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="1.5" y="2.5" width="13" height="3.5"/><path d="M2.5 6v7.5h11V6M6 9h4"/></svg>',
  search: '<svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6"><circle cx="7" cy="7" r="4.5"/><path d="M10.5 10.5L14 14"/></svg>',
  navDash: '<svg width="13" height="13" viewBox="0 0 13 13" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="1" y="1" width="11" height="11"/></svg>',
  navPlus: '<svg width="13" height="13" viewBox="0 0 13 13" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M6.5 2v9M2 6.5h9"/></svg>',
  navGear: '<svg width="13" height="13" viewBox="0 0 13 13" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="6.5" cy="6.5" r="2.5"/><path d="M6.5 1v2M6.5 10v2M1 6.5h2M10 6.5h2"/></svg>',
  navBox: '<svg width="13" height="13" viewBox="0 0 13 13" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M6.5 1v8M3 6l3.5 3.5L10 6M1 12h11"/></svg>',
  navGlobe: '<svg width="13" height="13" viewBox="0 0 13 13" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="6.5" cy="6.5" r="5"/><path d="M1.5 6.5h10M6.5 1.5c-2.7 2.8-2.7 7.2 0 10c2.7-2.8 2.7-7.2 0-10z"/></svg>',
  navKey: '<svg width="13" height="13" viewBox="0 0 13 13" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="4" cy="6.5" r="2.5"/><path d="M6.5 6.5H12M10 6.5v2.5"/></svg>',
  navPlug: '<svg width="13" height="13" viewBox="0 0 13 13" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="3" y="1.5" width="7" height="6"/><path d="M5 7.5v2M8 7.5v2M6.5 9.5v2"/></svg>',
  navUsers: '<svg width="13" height="13" viewBox="0 0 13 13" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="6.5" cy="4.5" r="3"/><path d="M1.5 12c1-3 9-3 10 0"/></svg>',
  navNodes: '<svg width="13" height="13" viewBox="0 0 13 13" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="1" y="2" width="11" height="4"/><rect x="1" y="8" width="11" height="4"/></svg>',
  navLog: '<svg width="13" height="13" viewBox="0 0 13 13" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M2 3h9M2 6.5h9M2 10h6"/></svg>',
  navTpl: '<svg width="13" height="13" viewBox="0 0 13 13" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="1" y="1" width="11" height="11"/><path d="M1 4.5h11M4.5 4.5V12"/></svg>',
  navJava: '<svg width="13" height="13" viewBox="0 0 13 13" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M2 11h9M4 2c2 2-2 3 0 5M7.5 2c2 2-2 3 0 5"/></svg>',
  navConsole: '<svg width="13" height="13" viewBox="0 0 13 13" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M2 3l4 3.5L2 10M7 10h4"/></svg>',
  navPlayers: '<svg width="13" height="13" viewBox="0 0 13 13" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="6.5" cy="4.5" r="3"/><path d="M1.5 12c1-3 9-3 10 0"/></svg>',
  navMetrics: '<svg width="13" height="13" viewBox="0 0 13 13" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M1 11l3-4 3 2 4-6"/></svg>',
  navFolder: '<svg width="13" height="13" viewBox="0 0 13 13" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M1 4h4l1.5 2H12v6H1z"/></svg>',
  navNet: '<svg width="13" height="13" viewBox="0 0 13 13" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="6.5" cy="2.5" r="1.5"/><circle cx="2.5" cy="10.5" r="1.5"/><circle cx="10.5" cy="10.5" r="1.5"/><path d="M6.5 4v3M6.5 7L3.5 9.5M6.5 7l3 2.5"/></svg>'
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
    meAdmin = !!info.admin;
    meTotp = !!info.totp;
    meRecoveryLeft = info.recoveryRemaining || 0;
  } catch {
    return renderLogin();
  }
  sys = await api("/api/system").catch(() => null);
  renderShellAndRoute();
}

function authScreen(cardHTML) {
  stopPolling();
  stopSidebar();
  closeConsole();
  closePalette();
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
    <label class="field" id="totp-row" hidden><span>${t("totp.loginCode")}</span><input type="text" name="code" inputmode="text" maxlength="12" autocomplete="one-time-code" placeholder="${esc(t("totp.loginPlaceholder"))}"></label>
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
      const info = await api("/api/me").catch(() => null);
      if (info) {
        meTotp = !!info.totp;
        meAdmin = !!info.admin;
        meRecoveryLeft = info.recoveryRemaining || 0;
      }
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
      // The first account is always the administrator.
      meAdmin = true;
      sys = await api("/api/system").catch(() => null);
      renderShellAndRoute();
    } catch (err) {
      errBox.innerHTML = `<div class="form-error">${esc(err.message)}</div>`;
    }
  });
}

/* ---------- shell & router ---------- */

// The sidebar keeps its own server cache and slow refresh loop, independent of
// the per-page pollTimer, so the server list and status dots stay fresh on
// every page.
let sidebarServers = [];
let sidebarTimer = null;

function stopSidebar() {
  if (sidebarTimer) { clearInterval(sidebarTimer); sidebarTimer = null; }
}

const PANEL_PAGES = [
  { id: "general", icon: "navGear", key: "panel.nav.general" },
  { id: "backups", icon: "navBox", key: "panel.nav.backups" },
  { id: "sftp", icon: "navKey", key: "panel.nav.sftp" },
  { id: "domain", icon: "navGlobe", key: "panel.nav.domain" },
  { id: "integrations", icon: "navPlug", key: "panel.nav.integrations" }
];
const ADMIN_PAGES = [
  { id: "users", icon: "navUsers", key: "admin.nav.users" },
  { id: "nodes", icon: "navNodes", key: "admin.nav.nodes" },
  { id: "audit", icon: "navLog", key: "admin.nav.audit" },
  { id: "templates", icon: "navTpl", key: "admin.nav.templates" },
  { id: "java", icon: "navJava", key: "admin.nav.java" }
];

function avatarInitials(name) {
  return esc(String(name || "?").slice(0, 2).toUpperCase());
}

function renderShellAndRoute() {
  $app.innerHTML = "";
  const isMac = /Mac|iP(hone|ad|od)/.test(navigator.platform || "");
  const shell = el(`<div class="shell">
    <header class="appbar">
      <button class="menu-btn" id="menu-btn" aria-label="Menu">&#9776;</button>
      <div class="wordmark" id="wm">${ICONS.cube}<span class="wm-name">COMPUTE<b>BOX</b></span><span class="wm-sub">Craftpanel</span></div>
      <button class="ab-search" id="ab-search">${ICONS.search}<span class="ph">${t("search.placeholder")}</span><span class="kbd">${isMac ? "⌘" : "Ctrl"} K</span></button>
      <div class="spacer"></div>
      <div class="lang-switch">
        <button data-lang="de" class="${LANG === "de" ? "active" : ""}">DE</button>
        <button data-lang="en" class="${LANG === "en" ? "active" : ""}">EN</button>
      </div>
      <button class="avatar" id="ab-avatar" title="${t("account.title")}">${avatarInitials(me)}</button>
    </header>
    <div class="layout">
      <nav class="sidebar" id="sidebar"></nav>
      <div class="sb-backdrop" id="sb-backdrop"></div>
      <main class="content" id="content"></main>
    </div>
  </div>`);
  $app.appendChild(shell);
  shell.querySelector("#wm").addEventListener("click", () => { location.hash = "#/"; });
  shell.querySelector("#ab-search").addEventListener("click", openPalette);
  shell.querySelector("#ab-avatar").addEventListener("click", () => { location.hash = "#/account"; });
  shell.querySelector("#menu-btn").addEventListener("click", () => {
    shell.querySelector("#sidebar").classList.toggle("open");
    shell.querySelector("#sb-backdrop").classList.toggle("open");
  });
  shell.querySelector("#sb-backdrop").addEventListener("click", closeSidebarMobile);
  shell.querySelectorAll(".lang-switch button").forEach((b) =>
    b.addEventListener("click", () => {
      setLang(b.dataset.lang);
      renderShellAndRoute();
    })
  );
  renderSidebar();
  refreshSidebarServers();
  stopSidebar();
  sidebarTimer = setInterval(refreshSidebarServers, 10000);
  route();
}

function closeSidebarMobile() {
  const sb = document.getElementById("sidebar");
  const bd = document.getElementById("sb-backdrop");
  if (sb) sb.classList.remove("open");
  if (bd) bd.classList.remove("open");
}

function statusDotClass(status) {
  if (status === "running") return "d-ok";
  if (status === "starting" || status === "stopping" || status === "installing") return "d-warn";
  if (status === "install_failed") return "d-err";
  return "";
}

function renderSidebar() {
  const sb = document.getElementById("sidebar");
  if (!sb) return;
  const navItem = (hash, icon, label, extra) =>
    `<button class="sb-i" data-nav="${esc(hash)}">${ICONS[icon] || ""}<span class="lbl">${label}</span>${extra || ""}</button>`;

  const serverItems = sidebarServers.map((s) => {
    const count = s.status === "running" && s.players ? `<span class="n">${s.players.online}</span>` : "";
    return `<button class="sb-i" data-nav="#/server/${encodeURIComponent(s.id)}">
      <span class="sb-dot ${statusDotClass(s.status)}"></span><span class="lbl">${esc(s.name)}</span>${count}</button>`;
  }).join("");

  sb.innerHTML = `
    <div class="sb-h">${t("nav.server")}</div>
    ${navItem("#/", "navDash", t("nav.dashboard"))}
    ${meAdmin ? navItem("#/create", "navPlus", t("dash.new")) : ""}
    ${serverItems}
    ${meAdmin ? `<div class="sb-h">${t("nav.panel")}</div>
      ${PANEL_PAGES.map((p) => navItem("#/panel/" + p.id, p.icon, t(p.key))).join("")}
      <div class="sb-h">${t("nav.admin")}</div>
      ${ADMIN_PAGES.map((p) => navItem("#/panel/" + p.id, p.icon, t(p.key))).join("")}` : ""}
    <div class="sb-gap"></div>
    <div class="sb-user" data-nav="#/account" title="${t("account.title")}">
      <span class="avatar">${avatarInitials(me)}</span><span class="lbl">${esc(me)}</span>
      <button class="out" id="sb-logout" title="${t("nav.logout")}"><svg width="13" height="13" viewBox="0 0 13 13" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M5 1.5H1.5v10H5M8.5 9L11 6.5 8.5 4M4.5 6.5H11"/></svg></button>
    </div>
    <div class="sb-ver">Craftpanel ${sys ? esc(sys.version) : ""} · <a href="https://computebox.de?utm_source=craftpanel" target="_blank" rel="noopener">ComputeBox</a></div>`;

  sb.querySelectorAll("[data-nav]").forEach((b) =>
    b.addEventListener("click", () => { location.hash = b.dataset.nav; })
  );
  const out = sb.querySelector("#sb-logout");
  if (out) out.addEventListener("click", async (e) => {
    e.stopPropagation();
    try { await api("/api/logout", { method: "POST" }); } catch {}
    me = null;
    renderLogin();
  });
  updateSidebarActive();
}

// Re-renders the sidebar only when the list actually changed, so open menus
// and hover states are not clobbered every ten seconds.
function updateSidebarServers(servers) {
  const key = servers.map((s) =>
    `${s.id}:${s.name}:${s.status}:${s.status === "running" && s.players ? s.players.online : ""}`).join("|");
  const prev = sidebarServers.map((s) =>
    `${s.id}:${s.name}:${s.status}:${s.status === "running" && s.players ? s.players.online : ""}`).join("|");
  sidebarServers = servers;
  if (key !== prev) renderSidebar();
}

async function refreshSidebarServers() {
  try {
    const servers = await api("/api/servers");
    updateSidebarServers(servers);
  } catch {}
}

function updateSidebarActive() {
  const sb = document.getElementById("sidebar");
  if (!sb) return;
  const h = location.hash || "#/";
  sb.querySelectorAll("[data-nav]").forEach((b) => {
    const nav = b.dataset.nav;
    let on = false;
    if (nav === "#/") on = h === "#/" || h === "";
    else on = h === nav || h.startsWith(nav + "/");
    b.classList.toggle("on", on);
  });
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
  closePalette();
  closeSidebarMobile();
  const h = location.hash || "#/";
  let m = h.match(/^#\/server\/([^/]+)(?:\/([a-z]+))?(?:\/([a-z]+))?/);
  if (m) {
    renderDetail(decodeURIComponent(m[1]), m[2] || "console", m[3] || "");
  } else if (h === "#/create") {
    renderCreateWizard();
  } else if ((m = h.match(/^#\/panel\/([a-z]+)/))) {
    renderPanelPage(m[1]);
  } else if (h.startsWith("#/account")) {
    renderAccountPage();
  } else {
    renderDash();
  }
  updateSidebarActive();
  const c = content();
  if (c) c.scrollTop = 0;
}

/* ---------- command palette (Ctrl/Cmd+K) ---------- */

let paletteEl = null;

function closePalette() {
  if (paletteEl) { paletteEl.remove(); paletteEl = null; }
}

// Search entries carry labels in both languages so a German user typing the
// English term (or vice versa) still lands on the right page.
function paletteIndex() {
  const both = (key) => `${STRINGS.en[key] || key} ${(STRINGS.de && STRINGS.de[key]) || ""}`;
  const items = [];
  for (const s of sidebarServers) {
    items.push({
      label: s.name,
      crumb: t("nav.server"),
      match: s.name,
      dot: statusDotClass(s.status),
      go: "#/server/" + encodeURIComponent(s.id)
    });
  }
  items.push({ label: t("nav.dashboard"), crumb: t("nav.server"), match: both("nav.dashboard"), icon: "navDash", go: "#/" });
  if (meAdmin) {
    items.push({ label: t("dash.new"), crumb: t("nav.server"), match: both("dash.new"), icon: "navPlus", go: "#/create" });
    for (const p of PANEL_PAGES) {
      items.push({ label: t(p.key), crumb: t("nav.panel"), match: both(p.key), icon: p.icon, go: "#/panel/" + p.id });
    }
    for (const p of ADMIN_PAGES) {
      items.push({ label: t(p.key), crumb: t("nav.admin"), match: both(p.key), icon: p.icon, go: "#/panel/" + p.id });
    }
  }
  items.push({ label: t("account.title"), crumb: esc(me || ""), match: both("account.title"), icon: "navUsers", go: "#/account" });
  if (currentDetailId) {
    const s = sidebarServers.find((x) => x.id === currentDetailId);
    const name = s ? s.name : currentDetailId;
    const tabs = [
      ["console", "tabs.console"], ["players", "tabs.players"], ["metrics", "tabs.metrics"],
      ["files", "tabs.files"], ["backups", "tabs.backups"], ["settings", "tabs.settings"]
    ];
    for (const [tab, key] of tabs) {
      items.push({
        label: t(key),
        crumb: name,
        match: `${name} ${both(key)}`,
        icon: "navConsole",
        go: `#/server/${encodeURIComponent(currentDetailId)}/${tab}`
      });
    }
  }
  return items;
}

function openPalette() {
  closePalette();
  const items = paletteIndex();
  paletteEl = el(`<div class="palette">
    <div class="pal-box">
      <input type="text" id="pal-q" placeholder="${esc(t("search.placeholder"))}" autocomplete="off" spellcheck="false">
      <div class="pal-list" id="pal-list"></div>
    </div>
  </div>`);
  document.body.appendChild(paletteEl);
  const input = paletteEl.querySelector("#pal-q");
  const list = paletteEl.querySelector("#pal-list");
  let hits = [];
  let sel = 0;

  const run = (item) => {
    closePalette();
    if (!item) return;
    if (item.act) item.act();
    else if (item.go) {
      if (location.hash === item.go) route();
      else location.hash = item.go;
    }
  };

  const draw = () => {
    if (!hits.length) {
      list.innerHTML = `<div class="pal-empty">${t("search.noResults")}</div>`;
      return;
    }
    list.innerHTML = hits.map((it, i) => `<div class="pal-item ${i === sel ? "sel" : ""}" data-i="${i}">
      ${it.dot !== undefined ? `<span class="sb-dot ${it.dot}"></span>` : (ICONS[it.icon] || "")}
      <span>${esc(it.label)}</span><span class="crumb">${esc(it.crumb)}</span>
      ${i === sel ? '<span class="kbd">&#9166;</span>' : ""}
    </div>`).join("");
    list.querySelectorAll(".pal-item").forEach((elm) => {
      elm.addEventListener("click", () => run(hits[+elm.dataset.i]));
      elm.addEventListener("mousemove", () => {
        if (sel !== +elm.dataset.i) { sel = +elm.dataset.i; draw(); }
      });
    });
    const selEl = list.querySelector(".pal-item.sel");
    if (selEl) selEl.scrollIntoView({ block: "nearest" });
  };

  const filter = () => {
    const q = input.value.trim().toLowerCase();
    hits = q === ""
      ? items.slice(0, 12)
      : items.filter((it) => it.match.toLowerCase().includes(q)).slice(0, 12);
    sel = 0;
    draw();
  };

  input.addEventListener("input", filter);
  input.addEventListener("keydown", (e) => {
    if (e.key === "ArrowDown") { sel = Math.min(sel + 1, hits.length - 1); draw(); e.preventDefault(); }
    else if (e.key === "ArrowUp") { sel = Math.max(sel - 1, 0); draw(); e.preventDefault(); }
    else if (e.key === "Enter") { run(hits[sel]); e.preventDefault(); }
    else if (e.key === "Escape") { closePalette(); e.preventDefault(); }
  });
  paletteEl.addEventListener("mousedown", (e) => {
    if (e.target === paletteEl) closePalette();
  });
  filter();
  input.focus();
}

document.addEventListener("keydown", (e) => {
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
    if (!me || !document.getElementById("content")) return;
    e.preventDefault();
    if (paletteEl) closePalette();
    else openPalette();
  }
});

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
let dashView = localStorage.getItem("cp_dash_view") === "list" ? "list" : "cards";
// Sparkline data is fetched at most once a minute per server.
const sparkCache = new Map();

async function renderDash() {
  currentDetailId = null;
  dashCards = new Map();
  const c = content();
  c.innerHTML = `<div class="page-head">
      <h1>${t("dash.title")}</h1><span class="sum" id="dash-sum"></span>
      <span class="spacer"></span>
      <div class="seg" id="dash-view">
        <button data-v="cards" class="${dashView === "cards" ? "on" : ""}">&#9638; ${t("dash.viewCards")}</button>
        <button data-v="list" class="${dashView === "list" ? "on" : ""}">&#9776; ${t("dash.viewList")}</button>
      </div>
      ${meAdmin ? `<button class="btn btn-primary" id="new-server">${ICONS.plus} ${t("dash.new")}</button>` : ""}</div>
    <div id="update-banner"></div>
    <div id="eula-banner"></div>
    <div id="java-warning"></div>
    <div id="server-grid"></div>`;
  const newBtn = c.querySelector("#new-server");
  if (newBtn) newBtn.addEventListener("click", () => { location.hash = "#/create"; });
  c.querySelectorAll("#dash-view button").forEach((b) =>
    b.addEventListener("click", () => {
      if (dashView === b.dataset.v) return;
      dashView = b.dataset.v;
      localStorage.setItem("cp_dash_view", dashView);
      c.querySelectorAll("#dash-view button").forEach((x) =>
        x.classList.toggle("on", x.dataset.v === dashView));
      const grid = document.getElementById("server-grid");
      if (grid) { grid.innerHTML = ""; dashCards.clear(); }
      refreshDash();
    })
  );
  if (sys && sys.updateAvailable && localStorage.getItem("cp_hide_update") !== sys.latest) {
    const b = el(`<div class="notice">&#9888; ${esc(t("update.banner", { v: sys.latest }))}
      <span class="spacer" style="flex:1"></span>
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

  updateSidebarServers(servers);
  renderEulaBanner(servers);
  renderJavaWarning(servers);

  const sum = document.getElementById("dash-sum");
  if (sum) {
    const running = servers.filter((s) => s.status === "running").length;
    const players = servers.reduce((n, s) =>
      n + (s.status === "running" && s.players ? s.players.online : 0), 0);
    sum.textContent = t("dash.sum", { total: servers.length, running, players });
  }

  if (servers.length === 0) {
    if (!grid.querySelector(".empty")) {
      dashCards.clear();
      grid.innerHTML = `<div class="empty"><div class="cube-lg">${ICONS.cube}</div>
        <h2>${t("dash.empty.title")}</h2>
        <p>${t("dash.empty.text")}</p>
        ${meAdmin ? `<button class="btn btn-primary" id="empty-new">${ICONS.plus} ${t("dash.new")}</button>` : ""}</div>`;
      const btn = grid.querySelector("#empty-new");
      if (btn) btn.addEventListener("click", () => { location.hash = "#/create"; });
    }
    return;
  }

  if (dashView === "list") {
    renderDashList(grid, servers);
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

// The list view is rebuilt on every poll; rows carry no animations, so the
// churn is invisible and much simpler than patching.
function renderDashList(grid, servers) {
  let table = grid.querySelector("table.tbl");
  if (!table) {
    grid.innerHTML = `<table class="tbl">
      <thead><tr>
        <th>${t("dash.title")}</th><th>Status</th><th>${t("detail.address")}</th>
        <th>${t("players.label")}</th><th>RAM</th><th>${t("detail.uptime")}</th><th></th>
      </tr></thead><tbody></tbody></table>`;
    table = grid.querySelector("table.tbl");
  }
  const host = location.hostname || "localhost";
  const tbody = table.querySelector("tbody");
  tbody.innerHTML = "";
  for (const s of servers) {
    const addr = s.domain
      ? s.domain + (s.domainPort ? ":" + s.domainPort : "")
      : `${host}:${s.port}`;
    const ramPct = s.memoryMB ? Math.min(100, Math.round((s.rssMB / s.memoryMB) * 100)) : 0;
    const tr = el(`<tr>
      <td><b>${esc(s.name)}</b>${s.nodeName ? ` <span class="hint">· ${esc(s.nodeName)}</span>` : ""}</td>
      <td>${statusBadge(s)}</td>
      <td class="mono">${esc(addr)}</td>
      <td class="mono">${s.status === "running" && s.players ? `${s.players.online}/${s.players.max}` : "—"}</td>
      <td><div class="meter" style="width:90px"><i class="${ramPct > 88 ? "warn" : ""}" style="width:${ramPct}%"></i></div></td>
      <td class="mono">${s.status === "running" ? fmtUptime(s.uptimeS) : "—"}</td>
      <td class="row-act"></td>
    </tr>`);
    tr.addEventListener("click", () => { location.hash = "#/server/" + encodeURIComponent(s.id); });
    appendServerAction(tr.querySelector(".row-act"), s);
    tbody.appendChild(tr);
  }
}

// The one context-dependent quick action (start/stop/accept EULA/retry).
function appendServerAction(hostEl, s) {
  const stopClick = (fn) => (e) => { e.stopPropagation(); fn(); };
  const add = (cls, label, fn) => {
    const b = el(`<button class="btn btn-sm ${cls}">${label}</button>`);
    b.addEventListener("click", stopClick(fn));
    hostEl.appendChild(b);
  };
  const wantEula = !s.eula && s.status !== "installing" && s.status !== "install_failed";
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
    <div class="sc-top"><h3></h3><span class="sc-badges"></span><span class="sc-actions"></span></div>
    <div class="sc-meta"></div>
    <div class="sc-stats"></div>
    <div class="sc-res"></div>
    <div class="meter"><i></i></div>
    <div class="sc-extra"></div>
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

  const host = location.hostname || "localhost";
  const addr = s.domain
    ? s.domain + (s.domainPort ? ":" + s.domainPort : "")
    : `${host}:${s.port}`;
  const typeLabel = s.modpack && s.modpack.title
    ? `${esc(s.modpack.title)} · ${esc(s.type)} ${esc(s.version)}`
    : `${esc(s.type)} ${esc(s.version)}`;
  const meta = card.querySelector(".sc-meta");
  const nodeBit = s.nodeName ? `<span>·</span><span title="${esc(s.nodeId || "")}">${esc(s.nodeName)}${s.apiReady === false ? " ⚠" : ""}</span>` : "";
  const metaHTML = `<span>${esc(addr)}</span><span>·</span><span>${typeLabel}</span>${nodeBit}`;
  if (meta.innerHTML !== metaHTML) meta.innerHTML = metaHTML;

  const running = s.status === "running";
  const stats = card.querySelector(".sc-stats");
  const statsHTML = `<span>${t("players.label")} <b>${running && s.players ? `${s.players.online}/${s.players.max}` : "—"}</b></span>
    <span>${t("detail.uptime")} <b>${running ? fmtUptime(s.uptimeS) : "—"}</b></span>
    <svg class="spark" width="80" height="24" viewBox="0 0 80 24"></svg>`;
  if (stats.dataset.html !== statsHTML) {
    stats.dataset.html = statsHTML;
    const old = stats.querySelector(".spark");
    stats.innerHTML = statsHTML;
    // Keep the previously drawn polyline across patches.
    if (old && old.childElementCount) stats.querySelector(".spark").innerHTML = old.innerHTML;
  }
  loadSpark(card, s.id);

  const res = card.querySelector(".sc-res");
  const memTxt = s.memoryMB
    ? `RAM ${running && s.rssMB ? (s.rssMB / 1024).toFixed(1) : "0"} / ${(s.memoryMB / 1024).toFixed(0)} GB`
    : (running && s.rssMB ? `RAM ${s.rssMB} MB` : "RAM —");
  const resHTML = `<span>${memTxt}</span><span>CPU ${running ? Math.round(s.cpuPct || 0) + "%" : "0%"}</span>`;
  if (res.innerHTML !== resHTML) res.innerHTML = resHTML;

  const bar = card.querySelector(".meter i");
  const pct = s.memoryMB && running ? Math.min(100, Math.round((s.rssMB / s.memoryMB) * 100)) : 0;
  bar.style.width = pct + "%";
  bar.className = pct > 88 ? "warn" : "";

  const extra = card.querySelector(".sc-extra");
  if (s.status === "installing") {
    let prog = extra.querySelector(".progress i");
    if (!prog) {
      extra.innerHTML = `<div class="progress"><i></i></div>`;
      prog = extra.querySelector("i");
    }
    prog.style.width = Math.round(s.progress * 100) + "%";
  } else if (s.status === "install_failed") {
    const html = `<p class="hint">${esc(s.error || "")}</p>`;
    if (extra.innerHTML !== html) extra.innerHTML = html;
  } else if (extra.childElementCount) {
    extra.innerHTML = "";
  }

  // Rebuild the action button only when the state actually changes.
  const actions = card.querySelector(".sc-actions");
  const wantEula = !s.eula && s.status !== "installing" && s.status !== "install_failed";
  const key = wantEula ? "eula" : s.status;
  if (actions.dataset.key === key) return;
  actions.dataset.key = key;
  actions.innerHTML = "";
  appendServerAction(actions, s);
}

// 1h CPU sparkline from the metrics history, refreshed once a minute.
async function loadSpark(card, id) {
  const now = Date.now();
  const at = +(card.dataset.sparkAt || 0);
  if (now - at < 60000) return;
  card.dataset.sparkAt = now;
  let pts = null;
  const cached = sparkCache.get(id);
  if (cached && now - cached.at < 60000) {
    pts = cached.pts;
  } else {
    try {
      const list = await api(`/api/servers/${encodeURIComponent(id)}/metrics`);
      pts = list.slice(-60).map((m) => m.cpu || 0);
      sparkCache.set(id, { at: now, pts });
    } catch { return; }
  }
  const svg = card.querySelector(".spark");
  if (!svg) return;
  if (!pts || pts.length < 2) {
    svg.innerHTML = `<polyline points="0,12 80,12" stroke-dasharray="3 3" stroke-width="1.5" fill="none" style="stroke:var(--faint)"></polyline>`;
    return;
  }
  const max = Math.max(100, ...pts);
  const step = 80 / (pts.length - 1);
  const path = pts.map((v, i) =>
    `${(i * step).toFixed(1)},${(22 - (v / max) * 20).toFixed(1)}`).join(" ");
  svg.innerHTML = `<polyline points="${path}" stroke-width="1.5" fill="none"></polyline>`;
}

async function serverAction(id, action) {
  try {
    if (action === "start") {
      try {
        const pf = await api(`/api/servers/${encodeURIComponent(id)}/preflight`);
        if (!pf.ok) {
          const err = (pf.checks || []).find((c) => !c.ok && c.level === "error");
          throw Object.assign(new Error(err ? err.message : t("preflight.blocked")), { status: 400 });
        }
        const warns = (pf.checks || []).filter((c) => c.level === "warn");
        if (warns.length) toast(warns.map((w) => w.message).join(" · "), "ok");
      } catch (e) {
        if (e.status) throw e;
      }
    }
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
function isPluginType(typ) {
  return typ === "paper" || typ === "purpur" || typ === "folia" || typ === "velocity" || typ === "waterfall";
}
function isProxyType(typ) {
  return typ === "velocity" || typ === "waterfall";
}

/* ---------- create wizard (design 1h: 4 steps at #/create) ---------- */

const WIZ_SOURCES = [
  { id: "empty", icon: "&#9634;", key: "wiz.src.empty", desc: "wiz.src.emptyDesc" },
  { id: "modpack", icon: "&#9636;", key: "wiz.src.modpack", desc: "wiz.src.modpackDesc" },
  { id: "template", icon: "&#9638;", key: "wiz.src.template", desc: "wiz.src.templateDesc" },
  { id: "clone", icon: "&#8857;", key: "wiz.src.clone", desc: "wiz.src.cloneDesc" },
  { id: "import", icon: "&#8613;", key: "wiz.src.import", desc: "wiz.src.importDesc" }
];

async function renderCreateWizard() {
  if (!meAdmin) { location.hash = "#/"; return; }
  currentDetailId = null;
  let settings = {};
  try { settings = await api("/api/settings"); } catch {}

  // All wizard state lives here; each step renders from it and writes back,
  // so going backwards never loses input (design: back is always free).
  const st = {
    step: 1,
    source: "",
    type: "paper", version: "", loaderVersion: "", versions: null, loaders: [],
    mpSource: "modrinth", mpQuery: "", mpHits: null, mpProject: "", mpTitle: "",
    mpHitSource: "modrinth", mpVersions: [], mpVersion: "", mpVersionLabel: "",
    analysis: null,
    templates: null, templateId: "",
    cloneId: "",
    importFile: null, importType: "paper", importVersion: "",
    name: "", memoryMB: 2048, port: "",
    nodeId: "", nodeName: "", nodes: []
  };
  try {
    const nodes = await api("/api/nodes");
    st.nodes = (nodes || []).filter((n) => n.apiReady);
  } catch { st.nodes = []; }

  const c = content();
  c.innerHTML = `
    <div class="page-head"><h1>${t("dash.new")}</h1>
      <span class="sum" id="wiz-head"></span></div>
    <div class="meter wiz-meter"><i id="wiz-prog"></i></div>
    <div id="wiz-body"></div>
    <div class="wiz-foot">
      <span class="spacer"></span>
      <button class="btn btn-ghost" id="wiz-cancel">${t("misc.cancel")}</button>
      <button class="btn" id="wiz-back" hidden>&lsaquo; ${t("wiz.back")}</button>
      <button class="btn btn-primary" id="wiz-next" disabled>${t("wiz.next")} &rsaquo;</button>
    </div>`;
  const body = c.querySelector("#wiz-body");
  const nextBtn = c.querySelector("#wiz-next");
  const backBtn = c.querySelector("#wiz-back");
  c.querySelector("#wiz-cancel").addEventListener("click", () => { location.hash = "#/"; });
  backBtn.addEventListener("click", () => { st.step--; draw(); });
  nextBtn.addEventListener("click", () => {
    if (nextBtn.disabled) return;
    if (st.step < 4) { st.step++; draw(); }
    else submit();
  });

  const stepValid = () => {
    switch (st.step) {
      case 1: return st.source !== "";
      case 2:
        if (st.source === "empty") return st.version !== "";
        if (st.source === "modpack") {
          return st.mpProject !== "" && st.mpVersion !== "" &&
            (!st.analysis || st.analysis.suitability !== "client");
        }
        if (st.source === "template") return st.templateId !== "";
        if (st.source === "clone") return st.cloneId !== "";
        if (st.source === "import") return st.importFile !== null;
        return false;
      case 3: return st.name.trim() !== "";
      default: return true;
    }
  };

  const syncFoot = () => {
    const labels = [t("wiz.s1"), t("wiz.s2"), t("wiz.s3"), t("wiz.s4")];
    c.querySelector("#wiz-head").textContent =
      t("wiz.step", { n: st.step }) + " — " + labels.join(" · ");
    c.querySelector("#wiz-prog").style.width = (st.step * 25) + "%";
    backBtn.hidden = st.step === 1;
    nextBtn.textContent = st.step === 4 ? t("create.submit") : t("wiz.next") + " ›";
    nextBtn.disabled = !stepValid();
  };

  /* ---- step 1: source ---- */
  const drawSource = () => {
    body.innerHTML = `<div class="wiz-cards">${WIZ_SOURCES.map((s2) => `
      <button class="wiz-card ${st.source === s2.id ? "sel" : ""}" data-src="${s2.id}">
        <b>${s2.icon} ${t(s2.key)}</b><span class="hint">${t(s2.desc)}</span>
      </button>`).join("")}</div>`;
    body.querySelectorAll(".wiz-card").forEach((card) =>
      card.addEventListener("click", () => {
        st.source = card.dataset.src;
        body.querySelectorAll(".wiz-card").forEach((x) =>
          x.classList.toggle("sel", x.dataset.src === st.source));
        syncFoot();
      })
    );
  };

  /* ---- step 2: type & version, per source ---- */
  const loadVersions = async () => {
    st.versions = null;
    st.version = "";
    const typ = st.type;
    try {
      const list = await api("/api/versions?type=" + typ);
      if (st.type !== typ) return;
      st.versions = list;
      st.version = (list.find((v) => v.latest) || list[0] || {}).id || "";
    } catch { st.versions = []; }
    await loadLoaders();
    if (st.step === 2 && st.source === "empty") {
      drawStep2();
      syncFoot();
    }
  };

  const loadLoaders = async () => {
    st.loaders = [];
    st.loaderVersion = "";
    if (!isModdedType(st.type) || !st.version) return;
    try {
      st.loaders = await api(`/api/loaders?type=${encodeURIComponent(st.type)}&version=${encodeURIComponent(st.version)}`);
    } catch {}
  };

  const typeHint = () => {
    if (st.type === "bedrock") return t("create.bedrockHint");
    if (st.type === "velocity") return t("create.velocityHint");
    if (isModdedType(st.type)) return t("create.moddedHint");
    return "";
  };

  const drawEmpty = () => {
    const versions = st.versions;
    body.innerHTML = `<div class="page-narrow">
      <div class="form-row">
        <label class="field"><span>${t("create.type")}</span>
          <select id="wz-type">
            ${["paper", "purpur", "folia", "vanilla", "fabric", "forge", "neoforge", "quilt", "bedrock", "velocity", "waterfall"]
              .map((typ2) => `<option value="${typ2}" ${st.type === typ2 ? "selected" : ""}>${typ2 === "velocity" ? "Velocity (Proxy)" : typ2 === "waterfall" ? "Waterfall (Proxy)" : typ2.charAt(0).toUpperCase() + typ2.slice(1)}</option>`).join("")}
          </select></label>
        <label class="field"><span>${t("create.version")}</span>
          <select id="wz-version" ${versions === null ? "disabled" : ""}>
            ${versions === null
              ? `<option>${t("create.loadingVersions")}</option>`
              : versions.map((v) => `<option value="${esc(v.id)}" ${v.id === st.version ? "selected" : ""}>${esc(v.id)}${v.latest ? " (latest)" : ""}</option>`).join("")}
          </select></label>
      </div>
      ${isModdedType(st.type) ? `<label class="field"><span>${t("create.loaderVersion")}</span>
        <select id="wz-loader">
          <option value="">${t("create.loaderLatest")}</option>
          ${st.loaders.map((v) => `<option value="${esc(v.id)}" ${v.id === st.loaderVersion ? "selected" : ""}>${esc(v.id)}${v.latest ? " (latest)" : ""}</option>`).join("")}
        </select></label>` : ""}
      ${typeHint() ? `<p class="hint">${typeHint()}</p>` : ""}
    </div>`;
    body.querySelector("#wz-type").addEventListener("change", (e) => {
      st.type = e.target.value;
      if (st.type === "modpack") st.type = "paper";
      if (isModdedType(st.type) && st.memoryMB < 2048) st.memoryMB = 2048;
      drawEmpty();
      syncFoot();
      loadVersions();
    });
    const verSel = body.querySelector("#wz-version");
    verSel.addEventListener("change", async () => {
      st.version = verSel.value;
      await loadLoaders();
      drawEmpty();
      syncFoot();
    });
    const loaderSel = body.querySelector("#wz-loader");
    if (loaderSel) loaderSel.addEventListener("change", () => { st.loaderVersion = loaderSel.value; });
  };

  const searchModpacks = async () => {
    const host = body.querySelector("#wz-mp-results");
    if (host) host.innerHTML = `<p class="hint">${t("misc.loading")}</p>`;
    try {
      st.mpHits = await api(`/api/modpacks/search?q=${encodeURIComponent(st.mpQuery)}&sort=downloads&source=${encodeURIComponent(st.mpSource)}`);
    } catch (e) {
      st.mpHits = [];
      toastError(e);
    }
    if (st.step === 2 && st.source === "modpack") {
      drawModpack();
      syncFoot();
    }
  };

  const analyzeModpack = async () => {
    st.analysis = null;
    if (!st.mpProject || !st.mpVersion) return;
    try {
      st.analysis = await api(`/api/modpacks/analyze?source=${encodeURIComponent(st.mpHitSource)}&project=${encodeURIComponent(st.mpProject)}&version=${encodeURIComponent(st.mpVersion)}`);
      if (st.analysis.suggestedMemoryMB) st.memoryMB = st.analysis.suggestedMemoryMB;
    } catch {}
    if (st.step === 2 && st.source === "modpack") drawModpack();
    syncFoot();
  };

  const drawModpack = () => {
    const a = st.analysis;
    const badge = !a ? "" : a.suitability === "good" ? t("create.packGood") : a.suitability === "mixed" ? t("create.packMixed") : t("create.packClient");
    body.innerHTML = `<div class="page-narrow">
      <div class="form-row">
        <label class="field"><span>${t("create.modpackSource")}</span>
          <select id="wz-mp-source">
            <option value="modrinth" ${st.mpSource === "modrinth" ? "selected" : ""}>Modrinth</option>
            <option value="curseforge" ${st.mpSource === "curseforge" ? "selected" : ""} ${settings.curseForgeKeySet ? "" : "disabled"}>CurseForge${!settings.curseForgeKeySet ? " (" + t("create.cfKeyMissing") + ")" : ""}</option>
            <option value="all" ${st.mpSource === "all" ? "selected" : ""}>${t("create.modpackSourceAll")}</option>
          </select></label>
        <label class="field"><span>${t("create.modpackSearch")}</span>
          <div class="add-row">
            <input type="text" id="wz-mp-q" placeholder="${esc(t("create.modpackPlaceholder"))}">
            <button type="button" class="btn btn-sm btn-primary" id="wz-mp-go">${t("create.modpackSearchBtn")}</button>
          </div></label>
      </div>
      <div id="wz-mp-results">
        ${st.mpHits === null ? "" : !st.mpHits.length ? `<p class="hint">${t("create.modpackNoResults")}</p>`
          : st.mpHits.map((h) => `<div class="plg-hit ${h.projectId === st.mpProject ? "sel" : ""}" data-p="${esc(h.projectId)}" data-src="${esc(h.source || "modrinth")}" data-title="${esc(h.title)}">
              <div><strong>${esc(h.title)}</strong>
                <span class="plg-desc">${esc(h.description || "")}</span>
                <span class="plg-dl">${esc(h.source || "modrinth")} · ${(h.loaders || []).map(esc).join(", ")} · ${h.downloads.toLocaleString()} ${t("plugins.downloads")}</span></div>
              <button type="button" class="btn btn-sm">${h.projectId === st.mpProject ? "✓" : t("create.modpackSelect")}</button>
            </div>`).join("")}
      </div>
      ${st.mpProject ? `<label class="field"><span>${t("create.modpackVersion")}</span>
        <select id="wz-mp-ver">
          ${st.mpVersions.length === 0 ? `<option>${t("create.loadingVersions")}</option>`
            : st.mpVersions.map((v) => `<option value="${esc(v.id)}" ${v.id === st.mpVersion ? "selected" : ""}>${esc(v.name || v.versionNumber)} (${esc((v.gameVersions || []).join(", "))}${v.loaders && v.loaders.length ? " · " + esc(v.loaders.join(", ")) : ""})</option>`).join("")}
        </select></label>` : ""}
      ${a ? `<p class="hint">${esc(badge)}: ${esc(a.message || "")}</p>` : ""}
      <p class="hint">${t("create.modpackHint")}</p>
    </div>`;
    body.querySelector("#wz-mp-source").addEventListener("change", (e) => { st.mpSource = e.target.value; });
    const q = body.querySelector("#wz-mp-q");
    q.value = st.mpQuery;
    q.addEventListener("input", () => { st.mpQuery = q.value; });
    q.addEventListener("keydown", (e) => {
      if (e.key === "Enter") { e.preventDefault(); searchModpacks(); }
    });
    body.querySelector("#wz-mp-go").addEventListener("click", searchModpacks);
    body.querySelectorAll(".plg-hit button").forEach((btn) =>
      btn.addEventListener("click", async () => {
        const hit = btn.closest(".plg-hit");
        st.mpProject = hit.dataset.p;
        st.mpHitSource = hit.dataset.src;
        st.mpTitle = hit.dataset.title;
        st.mpVersions = [];
        st.mpVersion = "";
        st.analysis = null;
        drawModpack();
        syncFoot();
        try {
          st.mpVersions = await api(`/api/modpacks/${encodeURIComponent(st.mpProject)}/versions?source=${encodeURIComponent(st.mpHitSource)}`);
          if (st.mpVersions.length) {
            st.mpVersion = st.mpVersions[0].id;
            st.mpVersionLabel = st.mpVersions[0].name || st.mpVersions[0].versionNumber || "";
          }
          drawModpack();
          syncFoot();
          analyzeModpack();
        } catch (e) { toastError(e); }
      })
    );
    const verSel = body.querySelector("#wz-mp-ver");
    if (verSel) verSel.addEventListener("change", () => {
      st.mpVersion = verSel.value;
      const v = st.mpVersions.find((x) => x.id === st.mpVersion);
      st.mpVersionLabel = v ? (v.name || v.versionNumber || "") : "";
      analyzeModpack();
      syncFoot();
    });
  };

  const drawTemplate = async () => {
    if (st.templates === null) {
      body.innerHTML = `<p class="hint">${t("misc.loading")}</p>`;
      try { st.templates = await api("/api/templates"); } catch { st.templates = []; }
    }
    body.innerHTML = `<div class="page-narrow">
      ${!st.templates.length ? `<p class="hint">${t("templates.empty")}</p>`
        : `<div class="wiz-cards">${st.templates.map((tpl) => `
            <button class="wiz-card ${st.templateId === tpl.id ? "sel" : ""}" data-id="${esc(tpl.id)}" data-name="${esc(tpl.name)}">
              <b>${esc(tpl.name)}</b><span class="hint">${esc(tpl.type)} ${esc(tpl.version)}</span>
            </button>`).join("")}</div>`}
    </div>`;
    body.querySelectorAll(".wiz-card").forEach((card) =>
      card.addEventListener("click", () => {
        st.templateId = card.dataset.id;
        st.templateName = card.dataset.name;
        if (!st.name) st.name = card.dataset.name;
        body.querySelectorAll(".wiz-card").forEach((x) =>
          x.classList.toggle("sel", x.dataset.id === st.templateId));
        syncFoot();
      })
    );
  };

  const drawClone = () => {
    const list = sidebarServers.filter((s2) => !s2.nodeId);
    body.innerHTML = `<div class="page-narrow">
      ${!list.length ? `<p class="hint">${t("dash.empty.title")}</p>`
        : `<div class="wiz-cards">${list.map((s2) => `
            <button class="wiz-card ${st.cloneId === s2.id ? "sel" : ""}" data-id="${esc(s2.id)}" data-name="${esc(s2.name)}">
              <b>${esc(s2.name)}</b><span class="hint">${esc(s2.type)} ${esc(s2.version)}</span>
            </button>`).join("")}</div>`}
      <p class="hint">${t("clone.hint")}</p>
    </div>`;
    body.querySelectorAll(".wiz-card").forEach((card) =>
      card.addEventListener("click", () => {
        st.cloneId = card.dataset.id;
        st.cloneName = card.dataset.name;
        if (!st.name) st.name = card.dataset.name + " copy";
        body.querySelectorAll(".wiz-card").forEach((x) =>
          x.classList.toggle("sel", x.dataset.id === st.cloneId));
        syncFoot();
      })
    );
  };

  const drawImport = () => {
    body.innerHTML = `<div class="page-narrow">
      <p class="hint">${t("import.hint")}</p>
      <label class="field"><span>${t("import.title")}</span>
        <input type="file" id="wz-imp-file" accept=".zip,application/zip"></label>
      <div class="form-row">
        <label class="field"><span>${t("create.type")}</span>
          <select id="wz-imp-type">
            ${["paper", "purpur", "vanilla", "fabric"].map((typ2) =>
              `<option value="${typ2}" ${st.importType === typ2 ? "selected" : ""}>${typ2.charAt(0).toUpperCase() + typ2.slice(1)}</option>`).join("")}
          </select></label>
        <label class="field"><span>${t("create.version")}</span>
          <input type="text" id="wz-imp-ver" placeholder="1.21.1"></label>
      </div>
    </div>`;
    const file = body.querySelector("#wz-imp-file");
    file.addEventListener("change", () => {
      st.importFile = file.files[0] || null;
      if (st.importFile && !st.name) st.name = st.importFile.name.replace(/\.zip$/i, "");
      syncFoot();
    });
    body.querySelector("#wz-imp-type").addEventListener("change", (e) => { st.importType = e.target.value; });
    const ver = body.querySelector("#wz-imp-ver");
    ver.value = st.importVersion;
    ver.addEventListener("input", () => { st.importVersion = ver.value; });
  };

  const drawStep2 = () => {
    if (st.source === "empty") {
      drawEmpty();
      if (st.versions === null) loadVersions();
    } else if (st.source === "modpack") {
      drawModpack();
      if (st.mpHits === null) searchModpacks();
    } else if (st.source === "template") drawTemplate();
    else if (st.source === "clone") drawClone();
    else drawImport();
  };

  /* ---- step 3: resources ---- */
  const needsResources = () => st.source === "empty" || st.source === "modpack";
  const drawResources = () => {
    const bedrock = st.source === "empty" && st.type === "bedrock";
    const minMem = st.source === "modpack" ? 4096 : isModdedType(st.type) ? 2048 : 1024;
    if (st.memoryMB < minMem) st.memoryMB = minMem;
    const nodeOpts = [`<option value="">${esc(t("create.nodeLocal"))}</option>`]
      .concat(st.nodes.map((n) =>
        `<option value="${esc(n.id)}" ${st.nodeId === n.id ? "selected" : ""}>${esc(n.name)}</option>`));
    // Clone/import/template stay on the panel host; empty+modpack can target a node.
    const canPickNode = (st.source === "empty" || st.source === "modpack") && st.nodes.length > 0;
    body.innerHTML = `<div class="page-narrow">
      <label class="field"><span>${t("create.name")}</span>
        <input type="text" id="wz-name" maxlength="40"></label>
      ${canPickNode ? `<label class="field"><span>${t("create.node")}</span>
        <select id="wz-node">${nodeOpts.join("")}</select></label>
        <p class="hint">${t("create.nodeHint")}</p>` : ""}
      ${needsResources() ? `<div class="form-row">
        ${bedrock ? "" : `<label class="field"><span>${t("create.memory")}</span>
          <select id="wz-mem">${MEM_OPTIONS.map((m) =>
            `<option value="${m}" ${m === st.memoryMB ? "selected" : ""}>${m / 1024} GB</option>`).join("")}</select></label>`}
        <label class="field"><span>${t("create.port")}</span>
          <input type="number" id="wz-port" min="1024" max="65535" placeholder="${t("create.portAuto")}"></label>
      </div>` : ""}
      ${st.analysis && st.analysis.suggestedMemoryMB ? `<p class="hint">${esc(st.analysis.suggestedJavaMajor
        ? t("create.recHint", { ram: st.analysis.suggestedMemoryMB, java: st.analysis.suggestedJavaMajor })
        : t("create.recHintRam", { ram: st.analysis.suggestedMemoryMB }))}</p>` : ""}
    </div>`;
    const name = body.querySelector("#wz-name");
    name.value = st.name;
    name.addEventListener("input", () => { st.name = name.value; syncFoot(); });
    name.focus();
    const nodeSel = body.querySelector("#wz-node");
    if (nodeSel) {
      nodeSel.addEventListener("change", () => {
        st.nodeId = nodeSel.value;
        const n = st.nodes.find((x) => x.id === st.nodeId);
        st.nodeName = n ? n.name : "";
      });
    }
    const mem = body.querySelector("#wz-mem");
    if (mem) mem.addEventListener("change", () => { st.memoryMB = parseInt(mem.value, 10); });
    const port = body.querySelector("#wz-port");
    if (port) {
      port.value = st.port;
      port.addEventListener("input", () => { st.port = port.value; });
    }
  };

  /* ---- step 4: confirm ---- */
  const drawConfirm = () => {
    const rows = [];
    const src = WIZ_SOURCES.find((s2) => s2.id === st.source);
    rows.push([t("wiz.s1"), t(src.key)]);
    if (st.source === "empty") {
      rows.push([t("create.type"), `${st.type} ${st.version}${st.loaderVersion ? " · " + st.loaderVersion : ""}`]);
    } else if (st.source === "modpack") {
      rows.push([t("wiz.src.modpack"), `${st.mpTitle}${st.mpVersionLabel ? " · " + st.mpVersionLabel : ""} (${st.mpHitSource})`]);
    } else if (st.source === "template") {
      rows.push([t("templates.title"), st.templateName || st.templateId]);
    } else if (st.source === "clone") {
      rows.push([t("wiz.src.clone"), st.cloneName || st.cloneId]);
    } else {
      rows.push([t("wiz.src.import"), `${st.importFile ? esc(st.importFile.name) : ""} · ${st.importType} ${st.importVersion}`]);
    }
    rows.push([t("create.name"), st.name.trim()]);
    if (st.nodeId) rows.push([t("create.node"), st.nodeName || st.nodeId]);
    if (needsResources()) {
      if (!(st.source === "empty" && st.type === "bedrock")) {
        rows.push([t("create.memory"), (st.memoryMB / 1024) + " GB"]);
      }
      rows.push([t("create.port"), st.port || t("create.portAuto")]);
    }
    body.innerHTML = `<div class="page-narrow"><div class="panel">
      <h2>${t("wiz.s4")}</h2>
      <table class="wiz-sum">${rows.map(([k, v]) =>
        `<tr><td>${esc(k)}</td><td>${esc(v)}</td></tr>`).join("")}</table>
      <div id="wz-err"></div>
    </div></div>`;
  };

  const submit = async () => {
    nextBtn.disabled = true;
    try {
      let view;
      if (st.source === "empty") {
        const req = { name: st.name.trim(), type: st.type, version: st.version, memoryMB: st.memoryMB };
        if (st.port) req.port = parseInt(st.port, 10);
        if (isModdedType(st.type) && st.loaderVersion) req.loaderVersion = st.loaderVersion;
        if (st.nodeId) req.nodeId = st.nodeId;
        view = await api("/api/servers", { method: "POST", body: req });
      } else if (st.source === "modpack") {
        const req = {
          name: st.name.trim(), type: "modpack", memoryMB: st.memoryMB,
          modpackProject: st.mpProject, modpackVersion: st.mpVersion, modpackSource: st.mpHitSource
        };
        if (st.port) req.port = parseInt(st.port, 10);
        if (st.nodeId) req.nodeId = st.nodeId;
        view = await api("/api/servers", { method: "POST", body: req });
      } else if (st.source === "template") {
        view = await api("/api/servers/from-template", {
          method: "POST", body: { templateId: st.templateId, name: st.name.trim() }
        });
      } else if (st.source === "clone") {
        view = await api(`/api/servers/${encodeURIComponent(st.cloneId)}/clone`, {
          method: "POST", body: { name: st.name.trim() }
        });
      } else {
        const fd = new FormData();
        fd.append("file", st.importFile);
        fd.append("name", st.name.trim());
        fd.append("type", st.importType);
        fd.append("version", st.importVersion.trim());
        const res = await fetch("/api/servers/import", {
          method: "POST", headers: { "X-Craftpanel": "1" }, body: fd
        });
        const data = await res.json().catch(() => ({}));
        if (!res.ok) throw Object.assign(new Error(data.message || res.statusText), { code: data.error });
        view = data;
      }
      refreshSidebarServers();
      location.hash = `#/server/${encodeURIComponent(view.id)}/console`;
    } catch (e) {
      nextBtn.disabled = false;
      toastError(e);
    }
  };

  const draw = () => {
    if (st.step === 1) drawSource();
    else if (st.step === 2) drawStep2();
    else if (st.step === 3) drawResources();
    else drawConfirm();
    syncFoot();
  };
  draw();
}

/* ---------- server detail ---------- */

async function renderDetail(id, tab, sub) {
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
    <div class="detail-body">
      <nav class="subnav" id="subnav"></nav>
      <section id="tab-body"></section>
    </div>`;
  renderDetailHead(s);
  renderSubnav(s, tab, sub);
  if (tab === "files") renderFilesTab(id);
  else if (tab === "players" && !isProxyType(s.type)) renderPlayersTab(id);
  else if (tab === "metrics") renderMetricsTab(id);
  else if (tab === "network" && s.type === "velocity") renderNetworkTab(id);
  else if (tab === "plugins" && isPluginType(s.type)) renderPluginsTab(id);
  else if (tab === "mods" && isModdedType(s.type)) renderModsTab(id);
  else if (tab === "datapacks" && s.type !== "bedrock" && !isProxyType(s.type)) renderDatapacksTab(id);
  else if (tab === "backups") renderBackupsTab(id);
  else if (tab === "settings") renderSettingsTab(id, s, sub);
  else renderConsoleTab(id, s);

  startPolling(() => updateDetailHead(id), 3000);
}

// The secondary nav replaces the old horizontal tab strip (design 3a): grouped
// entries, only the ones matching the server type, settings expand inline.
function renderSubnav(s, tab, sub) {
  const nav = document.getElementById("subnav");
  if (!nav) return;
  const base = `#/server/${encodeURIComponent(s.id)}`;
  const item = (tb, icon, label, extra) =>
    `<button class="sb-i ${tab === tb ? "on" : ""}" data-nav="${base}/${tb}">${ICONS[icon] || ""}<span class="lbl">${label}</span>${extra || ""}</button>`;
  const proxy = isProxyType(s.type);
  const playersCount = s.status === "running" && s.players
    ? `<span class="n" id="subnav-players">${s.players.online}</span>` : `<span class="n" id="subnav-players"></span>`;

  let html = item("console", "navConsole", t("tabs.console"));
  html += `<div class="sb-h">${t("subnav.ops")}</div>`;
  if (!proxy) html += item("players", "navPlayers", t("tabs.players"), playersCount);
  html += item("metrics", "navMetrics", t("tabs.metrics"));
  if (s.type === "velocity") html += item("network", "navNet", t("tabs.network"));
  html += `<div class="sb-h">${t("subnav.content")}</div>`;
  if (isPluginType(s.type)) html += item("plugins", "navPlug", t("tabs.plugins"));
  if (isModdedType(s.type)) html += item("mods", "navPlug", t("tabs.mods"));
  if (s.type !== "bedrock" && !proxy) html += item("datapacks", "navBox", t("tabs.datapacks"));
  html += item("files", "navFolder", t("tabs.files"));
  html += `<div class="sb-h">&nbsp;</div>`;
  html += item("backups", "navBox", t("tabs.backups"));
  html += item("settings", "navGear", t("tabs.settings"));
  if (tab === "settings") {
    const subs = [
      ["general", t("set.general"), ""],
      ["automation", t("set.automation"), ""],
      ["integrations", t("set.integrations"), ""],
      ["properties", "server.properties", "mono"],
      ["world", t("set.world"), ""],
      ["danger", t("set.danger"), ""]
    ];
    for (const [sid, label, cls] of subs) {
      if (sid === "properties" && (s.type === "velocity" || s.type === "waterfall")) continue;
      if (sid === "world" && proxy) continue;
      const active = (sub || "general") === sid;
      html += `<button class="sb-i sub ${sid === "danger" ? "danger" : ""} ${active ? "on" : ""}"
        data-nav="${base}/settings/${sid}"><span class="lbl ${cls}">${label}</span></button>`;
    }
  }
  nav.innerHTML = html;
  nav.querySelectorAll("[data-nav]").forEach((b) =>
    b.addEventListener("click", () => { location.hash = b.dataset.nav; }));
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
  const running = s.status === "running";
  const typeLabel = `${s.modpack && s.modpack.title ? `${esc(s.modpack.title)} · ` : ""}${esc(s.type)} ${esc(s.version)}${s.loaderVersion ? ` (${esc(s.loaderVersion)})` : ""}`;
  head.innerHTML = `
    <div class="detail-bar">
      <h1>${esc(s.name)}</h1>
      ${statusBadge(s)}
      <span class="addr">${esc(addr)}${s.type === "bedrock" ? " (UDP)" : ""}<button id="copy-addr" title="${t("detail.copy")}">${ICONS.copy}</button></span>
      <span class="stat">${typeLabel}</span>
      ${running && s.players ? `<span class="stat">${t("players.label")} <b>${s.players.online}/${s.players.max}</b></span>` : ""}
      ${running && s.tps ? `<span class="stat">TPS <b style="color:var(${s.tps >= 18 ? "--ok" : s.tps >= 15 ? "--warn" : "--err"})">${s.tps.toFixed(1)}</b></span>` : ""}
      ${running ? `<span class="stat">${t("detail.uptime")} <b>${fmtUptime(s.uptimeS)}</b></span>` : ""}
      ${running && s.rssMB ? `<span class="stat">RAM <b>${s.rssMB} MB</b></span>` : ""}
      ${running && s.cpuPct ? `<span class="stat">CPU <b>${Math.round(s.cpuPct)}%</b></span>` : ""}
      ${s.diskMB ? `<span class="stat">${t("misc.disk")} <b>${fmtSize(s.diskMB * 1048576)}</b></span>` : ""}
      ${s.status === "installing" ? `<span class="stat">${t("detail.installing")} (${Math.round(s.progress * 100)}%)</span>` : ""}
      ${s.status === "install_failed" ? `<span class="stat" style="color:var(--err)">${esc(s.error || "")}</span>` : ""}
      <div class="detail-actions" id="head-actions"></div>
    </div>`;
  const pl = document.getElementById("subnav-players");
  if (pl) pl.textContent = running && s.players ? s.players.online : "";
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

/* ---------- metrics tab ---------- */

function drawMetricChart(svg, pts, colorVar) {
  if (!svg) return;
  const W = 600, H = 120, PAD = 4;
  if (!pts || pts.length < 2) {
    svg.innerHTML = `<line x1="0" y1="${H / 2}" x2="${W}" y2="${H / 2}" stroke="var(--faint)" stroke-dasharray="3 5" stroke-width="1"></line>`;
    return { max: 0, last: 0 };
  }
  const max = Math.max(1, ...pts);
  const step = (W - PAD * 2) / (pts.length - 1);
  const path = pts.map((v, i) =>
    `${(PAD + i * step).toFixed(1)},${(H - PAD - (v / max) * (H - PAD * 2)).toFixed(1)}`).join(" ");
  const gridLines = [0.25, 0.5, 0.75].map((f) =>
    `<line x1="0" y1="${(H * f).toFixed(0)}" x2="${W}" y2="${(H * f).toFixed(0)}" stroke="var(--line)" stroke-dasharray="2 6" stroke-width="1"></line>`).join("");
  svg.innerHTML = `${gridLines}<polyline points="${path}" stroke="var(${colorVar})" stroke-width="2" fill="none"></polyline>`;
  return { max, last: pts[pts.length - 1] };
}

function renderMetricsTab(id) {
  const body = document.getElementById("tab-body");
  body.innerHTML = `
    <div class="chart-card"><h2>CPU <span class="hint" id="mx-cpu-now"></span></h2>
      <svg id="mx-cpu" viewBox="0 0 600 120" preserveAspectRatio="none" style="height:120px"></svg></div>
    <div class="chart-card"><h2>RAM <span class="hint" id="mx-ram-now"></span></h2>
      <svg id="mx-ram" viewBox="0 0 600 120" preserveAspectRatio="none" style="height:120px"></svg></div>
    <p class="hint" id="mx-hint">${t("metrics.hint")}</p>`;
  const load = async () => {
    const cpuSvg = document.getElementById("mx-cpu");
    if (!cpuSvg) return;
    let list;
    try {
      list = await api(`/api/servers/${encodeURIComponent(id)}/metrics`);
    } catch { return; }
    if (!list.length) {
      const hint = document.getElementById("mx-hint");
      if (hint) hint.textContent = t("metrics.empty");
    }
    const cpu = drawMetricChart(cpuSvg, list.map((m) => m.cpu || 0), "--accent");
    const ram = drawMetricChart(document.getElementById("mx-ram"), list.map((m) => m.rss || 0), "--ok");
    const cpuNow = document.getElementById("mx-cpu-now");
    if (cpuNow) cpuNow.textContent = list.length ? `${Math.round(cpu.last)}% · max ${Math.round(cpu.max)}%` : "";
    const ramNow = document.getElementById("mx-ram-now");
    if (ramNow) ramNow.textContent = list.length ? `${Math.round(ram.last)} MB · max ${Math.round(ram.max)} MB` : "";
  };
  load();
  stopTabTimer();
  tabTimer = setInterval(load, 30000);
}

/* ---------- console tab ---------- */

function closeConsole() {
  if (consoleES) { consoleES.close(); consoleES = null; }
}

// A console line passes when it matches the active category chip AND the free
// text filter.
function consoleLineVisible(div) {
  const chips = document.getElementById("console-chips");
  const filter = document.getElementById("console-filter");
  const mode = chips ? chips.dataset.mode || "all" : "all";
  if (mode === "warn" && !div.classList.contains("c-warn") && !div.classList.contains("c-err")) return false;
  if (mode === "err" && !div.classList.contains("c-err")) return false;
  if (mode === "chat" && !/<[^>]+>/.test(div.textContent)) return false;
  const q = filter ? filter.value.trim().toLowerCase() : "";
  if (q !== "" && !div.textContent.toLowerCase().includes(q)) return false;
  return true;
}

function applyConsoleFilter() {
  const box = document.getElementById("console");
  if (!box) return;
  for (const div of box.children) {
    div.classList.toggle("hide", !consoleLineVisible(div));
  }
}

function renderConsoleTab(id, s) {
  const body = document.getElementById("tab-body");
  body.innerHTML = `
    <div class="chips" id="console-chips" data-mode="all">
      <button class="chip on" data-f="all">${t("console.chip.all")}</button>
      <button class="chip" data-f="warn">${t("console.chip.warn")}</button>
      <button class="chip" data-f="err">${t("console.chip.err")}</button>
      <button class="chip" data-f="chat">${t("console.chip.chat")}</button>
      <input type="text" id="console-filter" placeholder="${esc(t("console.filter"))}">
      <span class="spacer"></span>
      <button class="chip on" id="autoscroll">${t("console.autoscroll")}</button>
      <button class="chip" id="console-dl" title="${esc(t("console.download"))}">&#8595; Log</button>
    </div>
    <div class="console-box" id="console"></div>
    <form class="console-form" id="cmd-form">
      <div class="cmd-wrap">
        <span class="prompt">&gt;</span>
        <input type="text" id="cmd-input" placeholder="${esc(t("console.placeholder"))}" autocomplete="off">
        <span class="cmd-hint">${t("console.histHint")}</span>
      </div>
      <button class="btn btn-primary" type="submit">${t("console.send")}</button>
    </form>`;
  const box = body.querySelector("#console");

  const chips = body.querySelector("#console-chips");
  chips.querySelectorAll(".chip[data-f]").forEach((c) =>
    c.addEventListener("click", () => {
      chips.dataset.mode = c.dataset.f;
      chips.querySelectorAll(".chip[data-f]").forEach((x) =>
        x.classList.toggle("on", x.dataset.f === c.dataset.f));
      applyConsoleFilter();
    })
  );
  const auto = body.querySelector("#autoscroll");
  auto.addEventListener("click", () => {
    auto.classList.toggle("on");
    if (auto.classList.contains("on")) box.scrollTop = box.scrollHeight;
  });
  const filterInput = body.querySelector("#console-filter");
  filterInput.addEventListener("input", applyConsoleFilter);
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
  if (!consoleLineVisible(div)) div.classList.add("hide");
  box.appendChild(div);
  while (box.childElementCount > 2000) box.firstElementChild.remove();
  const auto = document.getElementById("autoscroll");
  if (!auto || auto.classList.contains("on")) box.scrollTop = box.scrollHeight;
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
        <button class="btn btn-sm" id="mod-update-all">${t("mods.updateAll")}</button>
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
  body.querySelector("#mod-update-all").addEventListener("click", async () => {
    try {
      const preview = await api(`/api/servers/${encodeURIComponent(id)}/mods/updates`);
      if (!preview.count) { toast(t("mods.noUpdates"), "ok"); return; }
      if (!(await confirmModal(t("mods.updateAllConfirm", { n: preview.count }), false))) return;
      const res = await api(`/api/servers/${encodeURIComponent(id)}/mods/update-all`, { method: "POST", body: {} });
      toast(t("mods.updateAllOk", { n: res.updated }), "ok");
      loadModList(id);
    } catch (e) { toastError(e); }
  });
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
            const preview = await api(`/api/servers/${encodeURIComponent(id)}/mods/preview?projectId=${encodeURIComponent(hit.projectId)}`);
            const missing = (preview.dependencies || []).filter((d) => d.missing);
            if (missing.length) {
              const names = missing.map((d) => d.title || d.projectId).join(", ");
              if (!(await confirmModal(t("mods.depsConfirm", { deps: names }), false))) {
                btn.disabled = false;
                btn.textContent = t("mods.install");
                return;
              }
            }
            if (preview.message && !preview.compatible) {
              if (!(await confirmModal(preview.message + " — " + t("mods.installAnyway"), false))) {
                btn.disabled = false;
                btn.textContent = t("mods.install");
                return;
              }
            }
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
        ${p.disabled ? `<span class="badge">${t("mods.disabled")}</span>` : ""}
        ${p.version ? `<span class="badge">${esc(p.version)}</span>` : `<span class="plg-desc">${t("mods.manual")}</span>`}
        <span class="plg-dl">${fmtSize(p.size)}</span></span>
      <span class="pacts"></span>
    </li>`);
    const acts = li.querySelector(".pacts");
    if (p.updateAvailable && !p.disabled) {
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
    const tog = el(`<button class="btn btn-sm">${p.disabled ? t("mods.enable") : t("mods.disable")}</button>`);
    tog.addEventListener("click", async () => {
      try {
        await api(`/api/servers/${encodeURIComponent(id)}/mods/enable`, {
          method: "POST", body: { file: p.file, enabled: !!p.disabled }
        });
        loadModList(id);
      } catch (e) { toastError(e); }
    });
    acts.appendChild(tog);
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

function renderDatapacksTab(id) {
  const body = document.getElementById("tab-body");
  body.innerHTML = `
    <div class="panel">
      <h2>${t("datapacks.installed")}</h2>
      <p class="hint">${t("datapacks.hint")}</p>
      <div class="files-bar">
        <button class="btn btn-sm btn-primary" id="dp-upload">${ICONS.upload} ${t("datapacks.upload")}</button>
        <input type="file" id="dp-file" accept=".zip" multiple hidden>
      </div>
      <div id="dp-list">${t("misc.loading")}</div>
    </div>
    <div class="panel">
      <h2>${t("datapacks.search")}</h2>
      <div class="add-row">
        <input type="text" id="dp-query" placeholder="${esc(t("datapacks.searchPlaceholder"))}">
        <button class="btn btn-sm btn-primary" id="dp-go">${t("datapacks.searchBtn")}</button>
      </div>
      <div id="dp-results"></div>
    </div>`;
  const fileInput = body.querySelector("#dp-file");
  body.querySelector("#dp-upload").addEventListener("click", () => fileInput.click());
  fileInput.addEventListener("change", async () => {
    for (const file of fileInput.files) {
      const fd = new FormData();
      fd.append("file", file);
      try {
        const res = await fetch(`/api/servers/${encodeURIComponent(id)}/datapacks/upload`,
          { method: "POST", headers: { "X-Craftpanel": "1" }, body: fd });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          throw Object.assign(new Error(data.message || res.statusText), { code: data.error });
        }
        toast(t("files.uploaded") + ": " + file.name, "ok");
      } catch (e) { toastError(e); }
    }
    fileInput.value = "";
    loadDatapackList(id);
  });
  const doSearch = async () => {
    const q = body.querySelector("#dp-query").value.trim();
    const host = body.querySelector("#dp-results");
    host.innerHTML = `<p class="hint">${t("misc.loading")}</p>`;
    try {
      const hits = await api(`/api/servers/${encodeURIComponent(id)}/datapacks/search?q=${encodeURIComponent(q)}`);
      if (!hits.length) { host.innerHTML = `<p class="hint">${t("datapacks.noResults")}</p>`; return; }
      host.innerHTML = `<ul class="plist"></ul>`;
      const ul = host.querySelector("ul");
      for (const hit of hits) {
        const li = el(`<li class="plg-hit"><span class="pname"><b>${esc(hit.title)}</b>
          <span class="plg-desc">${esc(hit.description)}</span></span><span class="pacts"></span></li>`);
        const btn = el(`<button class="btn btn-sm ${hit.installed ? "" : "btn-ok"}" ${hit.installed ? "disabled" : ""}>
          ${hit.installed ? t("datapacks.alreadyInstalled") : t("datapacks.install")}</button>`);
        if (!hit.installed) {
          btn.addEventListener("click", async () => {
            btn.disabled = true;
            try {
              const entry = await api(`/api/servers/${encodeURIComponent(id)}/datapacks/install`,
                { method: "POST", body: { projectId: hit.projectId } });
              toast(t("datapacks.installedOk", { name: entry.title, v: entry.version }), "ok");
              loadDatapackList(id);
              btn.textContent = t("datapacks.alreadyInstalled");
            } catch (e) { btn.disabled = false; toastError(e); }
          });
        }
        li.querySelector(".pacts").appendChild(btn);
        ul.appendChild(li);
      }
    } catch (e) { host.innerHTML = ""; toastError(e); }
  };
  body.querySelector("#dp-go").addEventListener("click", doSearch);
  body.querySelector("#dp-query").addEventListener("keydown", (e) => {
    if (e.key === "Enter") { e.preventDefault(); doSearch(); }
  });
  doSearch();
  loadDatapackList(id);
}

async function loadDatapackList(id) {
  const host = document.getElementById("dp-list");
  if (!host) return;
  let list;
  try { list = await api(`/api/servers/${encodeURIComponent(id)}/datapacks?check=1`); }
  catch (e) { host.innerHTML = ""; toastError(e); return; }
  if (!list.length) { host.innerHTML = `<p class="hint">${t("datapacks.none")}</p>`; return; }
  host.innerHTML = `<ul class="plist"></ul>`;
  const ul = host.querySelector("ul");
  for (const p of list) {
    const li = el(`<li><span class="pname"><b>${esc(p.title || p.file)}</b>
      ${p.disabled ? `<span class="badge">${t("mods.disabled")}</span>` : ""}
      ${p.version ? `<span class="badge">${esc(p.version)}</span>` : ""}
      <span class="plg-dl">${fmtSize(p.size)}</span></span><span class="pacts"></span></li>`);
    const acts = li.querySelector(".pacts");
    const tog = el(`<button class="btn btn-sm">${p.disabled ? t("mods.enable") : t("mods.disable")}</button>`);
    tog.addEventListener("click", async () => {
      try {
        await api(`/api/servers/${encodeURIComponent(id)}/datapacks/enable`, {
          method: "POST", body: { file: p.file, enabled: !!p.disabled }
        });
        loadDatapackList(id);
      } catch (e) { toastError(e); }
    });
    acts.appendChild(tog);
    const del = el(`<button class="btn btn-sm btn-danger">${t("files.delete")}</button>`);
    del.addEventListener("click", async () => {
      if (!(await confirmModal(t("datapacks.deleteConfirm", { name: p.title || p.file }), true))) return;
      try {
        await api(`/api/servers/${encodeURIComponent(id)}/datapacks?file=${encodeURIComponent(p.file)}`, { method: "DELETE" });
        loadDatapackList(id);
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
    if (meAdmin) {
      btn(ICONS.plus, t("clone.fromBackup"), async () => {
        const name = prompt(t("create.name"));
        if (!name) return;
        try {
          const view = await api("/api/servers/from-backup", {
            method: "POST",
            body: { sourceId: id, backupName: b.name, name }
          });
          toast(t("clone.ok"), "ok");
          location.hash = `#/server/${encodeURIComponent(view.id)}/console`;
        } catch (e) { toastError(e); }
      });
    }
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

async function renderSettingsTab(id, s, sub) {
  sub = sub || "general";
  const body = document.getElementById("tab-body");
  body.innerHTML = `
    ${s.type === "velocity" ? "" : `<div class="panel" data-sub="general" id="eula-panel">
      <h2>${t("eula.title")}</h2>
      <p class="hint">${t("eula.text")}
        <a href="https://aka.ms/MinecraftEULA" target="_blank" rel="noopener">${t("eula.link")}</a></p>
      <div id="eula-state"></div>
    </div>`}

    <div class="panel" data-sub="general">
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
        <label class="field"><span>${t("isolation.title")}</span>
          <select name="isolation">
            <option value="none" ${!s.isolation ? "selected" : ""}>${t("isolation.none")}</option>
            <option value="systemd" ${s.isolation === "systemd" ? "selected" : ""}>systemd</option>
            <option value="docker" ${s.isolation === "docker" ? "selected" : ""}>docker</option>
          </select></label>
        <p class="hint">${t("isolation.hint")}</p>
        <button class="btn btn-primary" type="submit">${t("settings.save")}</button>
      </form>
    </div>

    <div class="panel" data-sub="automation">
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

    ${s.modpack ? `<div class="panel" data-sub="general">
      <h2>${t("modpack.infoTitle")}</h2>
      <p class="hint">${t("modpack.infoHint")}</p>
      <p><b>${esc(s.modpack.title || s.modpack.slug || "")}</b>
        ${s.modpack.version ? `<span class="badge">${esc(s.modpack.version)}</span>` : ""}
        ${s.modpack.source ? `<span class="badge">${esc(s.modpack.source)}</span>` : ""}</p>
      <p class="hint">${esc(s.type)} ${esc(s.version)}${s.loaderVersion ? " · loader " + esc(s.loaderVersion) : ""}</p>
      <div class="form-row">
        <label class="field"><span>${t("modpack.changeVersion")}</span>
          <select id="mp-up-version"><option>${t("create.loadingVersions")}</option></select></label>
      </div>
      <div class="files-bar">
        <button class="btn" id="mp-up-btn">${ICONS.restart} ${t("modpack.upgrade")}</button>
        <button class="btn btn-sm" id="mp-client-copy">${t("modpack.copyClient")}</button>
      </div>
      <pre class="hint" id="mp-client-text" style="white-space:pre-wrap"></pre>
    </div>` : `<div class="panel" data-sub="general">
      <h2>${t("upgrade.title")}</h2>
      <p class="hint">${t("upgrade.hint")}</p>
      <div id="build-line"></div>
      <div class="form-row">
        <label class="field"><span>${t("create.version")}</span>
          <select id="up-version"><option>${t("create.loadingVersions")}</option></select></label>
      </div>
      <button class="btn" id="up-btn">${ICONS.restart} ${t("upgrade.button")}</button>
    </div>`}

    ${s.type === "velocity" ? "" : `<div class="panel" data-sub="general">
      <h2>${t("access.title")}</h2>
      <div id="access-body">${t("misc.loading")}</div>
    </div>`}

    <div class="panel" data-sub="integrations">
      <h2>${t("discord.title")}</h2>
      <p class="hint">${t("discord.hint")}</p>
      <form id="dc-form">
        <label class="field"><span>${t("discord.webhook")}</span>
          <input type="text" name="webhook" value="${esc((s.discord && s.discord.webhook) || "")}" placeholder="https://discord.com/api/webhooks/... or https://..."></label>
        <p class="hint">${t("discord.webhookHint")}</p>
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

    <div class="panel" data-sub="automation">
      <h2>${t("schedCmd.title")}</h2>
      <p class="hint">${t("schedCmd.hint")}</p>
      <div id="sched-list"></div>
      <div class="form-row">
        <label class="field"><span>${t("schedCmd.time")}</span>
          <input type="time" id="sched-time" value="12:00"></label>
        <label class="field"><span>${t("schedCmd.command")}</span>
          <input type="text" id="sched-cmd" placeholder="say Hello" maxlength="200"></label>
      </div>
      <button class="btn btn-sm" type="button" id="sched-add">${t("schedCmd.add")}</button>
    </div>

    ${isPluginType(s.type) || isModdedType(s.type) ? `<div class="panel" data-sub="integrations">
      <h2>${t("geyser.title")}</h2>
      <p class="hint">${t("geyser.hint")}</p>
      <div id="geyser-body">${t("misc.loading")}</div>
      <button class="btn btn-sm" id="geyser-install">${t("geyser.install")}</button>
    </div>` : ""}

    ${!isProxyType(s.type) ? `<div class="panel" data-sub="world">
      <h2>${t("world.title")}</h2>
      <p class="hint">${t("world.hint")}</p>
      <input type="file" id="world-file" accept=".zip,application/zip">
      <button class="btn btn-sm" id="world-import">${t("world.import")}</button>
      <a class="btn btn-sm" id="world-download" href="/api/servers/${encodeURIComponent(id)}/world/download">${t("world.download")}</a>
    </div>
    <div class="panel" data-sub="world">
      <h2>${t("rpack.title")}</h2>
      <p class="hint">${t("rpack.hint")}</p>
      <div id="rpack-body">${t("misc.loading")}</div>
      <input type="file" id="rpack-file" accept=".zip,application/zip">
      <label class="check"><input type="checkbox" id="rpack-req"> <span>${t("rpack.required")}</span></label>
      <button class="btn btn-sm" id="rpack-upload">${t("rpack.upload")}</button>
      <button class="btn btn-sm btn-ghost" id="rpack-delete">${t("rpack.delete")}</button>
    </div>` : ""}

    ${meAdmin ? `<div class="panel" data-sub="general">
      <h2>${t("clone.title")}</h2>
      <p class="hint">${t("clone.hint")}</p>
      <label class="field"><span>${t("create.name")}</span>
        <input type="text" id="clone-name" maxlength="40" value="${esc(s.name)} copy"></label>
      <button class="btn btn-sm" id="clone-btn">${t("clone.button")}</button>
    </div>` : ""}

    ${s.type === "velocity" || s.type === "waterfall" ? "" : `<div class="panel" data-sub="properties">
      <h2>${t("props.title")}</h2>
      <p class="hint">${t("props.hint")}</p>
      <div id="props-body">${t("misc.loading")}</div>
    </div>`}

    <div class="panel danger" data-sub="danger">
      <h2>${t("danger.title")}</h2>
      <p class="hint">${t("danger.text")}</p>
      <label class="field"><span>${t("danger.confirm")}</span>
        <input type="text" id="del-confirm" autocomplete="off"></label>
      <button class="btn btn-danger" id="del-btn" disabled>${ICONS.trash} ${t("danger.delete")}</button>
    </div>`;

  // Show only the panels of the active settings sub-page. The rest stay in
  // the DOM so their wiring and loaders survive sub-navigation.
  body.querySelectorAll(".panel[data-sub]").forEach((p) => {
    p.hidden = p.dataset.sub !== sub;
  });

  if (s.type !== "velocity") renderEulaState(id, s.eula);

  body.querySelector("#settings-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const f = e.target;
    try {
      const patch = {
        name: f.name.value.trim(),
        autostart: f.autostart.checked,
        restartOnCrash: f.restartOnCrash.checked,
        isolation: f.isolation ? f.isolation.value : "none"
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
      const src = s.modpack.source || "modrinth";
      try {
        const list = await api(`/api/modpacks/${encodeURIComponent(s.modpack.projectId)}/versions?source=${encodeURIComponent(src)}`);
        sel.innerHTML = list.map((v) =>
          `<option value="${esc(v.id)}" ${v.id === s.modpack.versionId ? "selected" : ""}>${esc(v.name || v.versionNumber)} (${esc((v.gameVersions || []).join(", "))}${v.loaders && v.loaders.length ? " · " + esc(v.loaders.join(", ")) : ""})</option>`
        ).join("");
      } catch {
        sel.innerHTML = "<option></option>";
      }
      try {
        const info = await api(`/api/servers/${encodeURIComponent(id)}/client-pack`);
        body.querySelector("#mp-client-text").textContent = info.text || "";
      } catch {}
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
    body.querySelector("#mp-client-copy").addEventListener("click", async () => {
      const text = body.querySelector("#mp-client-text").textContent;
      try {
        await navigator.clipboard.writeText(text);
        toast(t("modpack.copied"), "ok");
      } catch { toastError(new Error(t("error.generic"))); }
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

    // Jar build updates within the installed version (Paper family, Purpur):
    // "you are N builds behind" without having to read the server log.
    (async () => {
      const line = body.querySelector("#build-line");
      if (!line) return;
      let info;
      try {
        info = await api(`/api/servers/${encodeURIComponent(id)}/build-update`);
      } catch { return; }
      if (!info.supported) return;
      const cur = `${esc(s.type)} ${esc(s.version)}${info.build ? " · Build " + esc(info.build) : ""}`;
      const upToDate = !info.updateAvailable && info.latest && info.build;
      let html = `<p class="hint" style="font-family:var(--font-mono);font-size:11.5px">${cur}${upToDate ? ` <span style="color:var(--ok)">✓ ${t("build.upToDate")}</span>` : ""}</p>`;
      if (info.updateAvailable || (!info.build && info.latest)) {
        html = `<div class="notice">&#9888; ${esc(info.build
          ? t("build.available", { latest: info.latest, current: info.build })
          : t("build.availableUnknown", { latest: info.latest }))}
          <span style="flex:1"></span>
          <button class="btn btn-sm btn-primary" id="build-upd">${t("build.update")}</button></div>` + html;
      }
      line.innerHTML = html;
      const btn = line.querySelector("#build-upd");
      if (btn) btn.addEventListener("click", async () => {
        if (!(await confirmModal(t("build.confirm", { version: s.version }), false))) return;
        try {
          await api(`/api/servers/${encodeURIComponent(id)}/upgrade`, { method: "POST", body: { version: s.version } });
          toast(t("upgrade.started"), "ok");
          updateDetailHead(id);
        } catch (e) { toastError(e); }
      });
    })();
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

  // Scheduled console commands
  let schedCmds = Array.isArray(s.scheduledCommands) ? s.scheduledCommands.slice() : [];
  const renderSched = () => {
    const host = body.querySelector("#sched-list");
    if (!host) return;
    if (!schedCmds.length) {
      host.innerHTML = `<p class="hint">${t("schedCmd.empty")}</p>`;
      return;
    }
    host.innerHTML = `<ul class="plist">${schedCmds.map((c, i) =>
      `<li><label class="check"><input type="checkbox" data-i="${i}" class="sched-en" ${c.enabled ? "checked" : ""}>
        <span><code>${esc(c.time)}</code> ${esc(c.command)}</span></label>
        <button class="btn btn-ghost btn-sm sched-del" data-i="${i}">${ICONS.trash}</button></li>`).join("")}</ul>`;
    host.querySelectorAll(".sched-en").forEach((cb) => {
      cb.addEventListener("change", async () => {
        schedCmds[+cb.dataset.i].enabled = cb.checked;
        await saveSched();
      });
    });
    host.querySelectorAll(".sched-del").forEach((btn) => {
      btn.addEventListener("click", async () => {
        schedCmds.splice(+btn.dataset.i, 1);
        await saveSched();
        renderSched();
      });
    });
  };
  const saveSched = async () => {
    try {
      const updated = await api("/api/servers/" + encodeURIComponent(id), {
        method: "PATCH", body: { scheduledCommands: schedCmds }
      });
      schedCmds = updated.scheduledCommands || [];
      toast(t("settings.saved"), "ok");
    } catch (e) { toastError(e); }
  };
  renderSched();
  body.querySelector("#sched-add").addEventListener("click", async () => {
    const time = body.querySelector("#sched-time").value;
    const command = body.querySelector("#sched-cmd").value.trim();
    if (!command) return;
    schedCmds.push({ id: "cmd-" + Date.now(), time, command, enabled: true });
    body.querySelector("#sched-cmd").value = "";
    await saveSched();
    renderSched();
  });

  const geyserBody = body.querySelector("#geyser-body");
  if (geyserBody) {
    (async () => {
      try {
        const st = await api(`/api/servers/${encodeURIComponent(id)}/geyser`);
        geyserBody.textContent = st.supported
          ? `Geyser: ${st.geyser ? "✓" : "—"} · Floodgate: ${st.floodgate ? "✓" : "—"}` + (st.hint ? " — " + st.hint : "")
          : (st.hint || "");
      } catch (e) { geyserBody.textContent = ""; }
    })();
    body.querySelector("#geyser-install").addEventListener("click", async () => {
      try {
        const st = await api(`/api/servers/${encodeURIComponent(id)}/geyser`, { method: "POST", body: { floodgate: true } });
        geyserBody.textContent = `Geyser: ${st.geyser ? "✓" : "—"} · Floodgate: ${st.floodgate ? "✓" : "—"}`;
        toast(t("geyser.ok"), "ok");
      } catch (e) { toastError(e); }
    });
  }

  const worldBtn = body.querySelector("#world-import");
  if (worldBtn) {
    worldBtn.addEventListener("click", async () => {
      const f = body.querySelector("#world-file").files[0];
      if (!f) return;
      if (!(await confirmModal(t("world.confirm"), false))) return;
      const fd = new FormData();
      fd.append("file", f);
      try {
        const res = await fetch(`/api/servers/${encodeURIComponent(id)}/world/import`, {
          method: "POST", headers: { "X-Craftpanel": "1" }, body: fd
        });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          throw Object.assign(new Error(data.message || res.statusText), { code: data.error });
        }
        toast(t("world.ok"), "ok");
      } catch (e) { toastError(e); }
    });
  }

  const rpackBody = body.querySelector("#rpack-body");
  if (rpackBody) {
    const refreshRP = async () => {
      try {
        const info = await api(`/api/servers/${encodeURIComponent(id)}/resource-pack`);
        rpackBody.textContent = info.present
          ? `${fmtSize(info.size)} · SHA1 ${info.sha1 || "—"}`
          : t("rpack.none");
      } catch { rpackBody.textContent = ""; }
    };
    refreshRP();
    body.querySelector("#rpack-upload").addEventListener("click", async () => {
      const f = body.querySelector("#rpack-file").files[0];
      if (!f) return;
      const fd = new FormData();
      fd.append("file", f);
      if (body.querySelector("#rpack-req").checked) fd.append("required", "true");
      try {
        const res = await fetch(`/api/servers/${encodeURIComponent(id)}/resource-pack`, {
          method: "POST", headers: { "X-Craftpanel": "1" }, body: fd
        });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          throw Object.assign(new Error(data.message || res.statusText), { code: data.error });
        }
        toast(t("rpack.ok"), "ok");
        refreshRP();
      } catch (e) { toastError(e); }
    });
    body.querySelector("#rpack-delete").addEventListener("click", async () => {
      try {
        await api(`/api/servers/${encodeURIComponent(id)}/resource-pack`, { method: "DELETE" });
        refreshRP();
      } catch (e) { toastError(e); }
    });
  }

  const cloneBtn = body.querySelector("#clone-btn");
  if (cloneBtn) {
    cloneBtn.addEventListener("click", async () => {
      const name = body.querySelector("#clone-name").value.trim();
      if (!name) return;
      try {
        const view = await api(`/api/servers/${encodeURIComponent(id)}/clone`, { method: "POST", body: { name } });
        toast(t("clone.ok"), "ok");
        location.hash = `#/server/${encodeURIComponent(view.id)}/settings`;
      } catch (e) { toastError(e); }
    });
  }

  if (!isProxyType(s.type)) {
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

  const hints = (info.hints || []).map((h) => `<li class="hint">${esc(h)}</li>`).join("");
  let domainHint = "";
  if (info.domain) {
    domainHint = `<p class="hint">${esc(t("network.domainHint", { domain: info.domain }))}</p>`;
    if (info.proxyPort !== 25565 && !info.dnsManaged) {
      domainHint += `<div class="notice">${esc(t("network.domainPortWarn", { port: info.proxyPort }))}</div>`;
    }
  }
  const backendsHTML = info.backends.length === 0
    ? `<p class="hint">${t("network.noBackends")}</p>`
    : `<ul class="plist" id="net-list"></ul>`;
  host.innerHTML = `${domainHint}
    <label class="check"><input type="checkbox" id="net-pp" ${info.proxyProtocol ? "checked" : ""}>
      <span>${t("network.proxyProtocol")}</span></label>
    <p class="hint">${t("network.proxyProtocolHint")}</p>
    <ul style="margin:0 0 1rem;padding-left:1.2rem">${hints}</ul>
    ${backendsHTML}
    <div class="modal-actions"><button class="btn btn-primary" id="net-save">${t("network.save")}</button></div>
    <div id="net-warn"></div>`;
  const ul = host.querySelector("#net-list");
  if (ul) {
    for (const b of info.backends) {
      const mapped = info.domain ? ` <span class="plg-desc">&rarr; ${esc(b.id + "." + info.domain)}</span>` : "";
      const li = el(`<li>
        <label class="check" style="margin:0"><input type="checkbox" ${b.linked ? "checked" : ""} data-sid="${esc(b.id)}">
          <span><b>${esc(b.name)}</b> <span class="plg-desc">${t("misc.port")} ${b.port}</span>${mapped}</span></label>
      </li>`);
      ul.appendChild(li);
    }
  }
  host.querySelector("#net-save").addEventListener("click", async () => {
    const servers = ul ? [...ul.querySelectorAll("input:checked")].map((i) => i.dataset.sid) : [];
    try {
      const res = await api(`/api/servers/${encodeURIComponent(id)}/network`, {
        method: "PUT",
        body: { servers, proxyProtocol: host.querySelector("#net-pp").checked }
      });
      toast(t("network.saved"), "ok");
      const warnHost = host.querySelector("#net-warn");
      warnHost.innerHTML = (res.warnings || []).map((w) => `<div class="notice">${esc(w)}</div>`).join("");
      loadNetwork(id);
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

/* ---------- panel pages (settings as routed pages, design 1f) ---------- */

// Every former modal section is now its own routed page under #/panel/<id>.
// Saves go through PUT /api/settings, which applies only the fields present
// in the body, so each page can save just its own values.

async function saveSettingsPatch(body) {
  const settings = await api("/api/settings", { method: "PUT", body });
  toast(t("settings.saved"), "ok");
  return settings;
}

async function renderPanelPage(section) {
  if (!meAdmin) { location.hash = "#/account"; return; }
  currentDetailId = null;
  const page = PANEL_PAGES.concat(ADMIN_PAGES).find((p) => p.id === section);
  if (!page) { location.hash = "#/panel/general"; return; }
  const c = content();
  c.innerHTML = `<div class="page-head"><h1>${t(page.key)}</h1></div><div id="pp"></div>`;
  const box = document.getElementById("pp");
  switch (section) {
    case "general": return panelGeneralPage(box);
    case "backups": return panelBackupsPage(box);
    case "sftp": return panelSftpPage(box);
    case "domain": return panelDomainPage(box);
    case "integrations": return panelIntegrationsPage(box);
    case "users": return panelUsersPage(box);
    case "nodes": return panelNodesPage(box);
    case "audit": return panelAuditPage(box);
    case "templates": return panelTemplatesPage(box);
    case "java": return panelJavaPage(box);
  }
}

function panelGeneralPage(box) {
  box.innerHTML = `<div class="page-narrow">
    <div class="panel">
      <h2>${t("panel.version")}</h2>
      <p class="hint" id="ps-version"></p>
    </div>
  </div>`;
  renderVersionLine(box);
}

async function panelBackupsPage(box) {
  let settings = {};
  try { settings = await api("/api/settings"); } catch {}
  box.innerHTML = `<div class="page-narrow"><div class="panel">
    <h2>${t("panel.backupDir")}</h2>
    <label class="field"><span>${t("panel.backupDir")}</span><input type="text" id="ps-backupdir"></label>
    <p class="hint">${esc(t("panel.backupDirHint"))}</p>
    <div class="modal-actions"><button class="btn btn-primary" id="ps-save">${t("settings.save")}</button></div>
  </div></div>`;
  box.querySelector("#ps-backupdir").value = settings.backupDir || "";
  box.querySelector("#ps-save").addEventListener("click", async () => {
    try {
      await saveSettingsPatch({ backupDir: box.querySelector("#ps-backupdir").value.trim() });
    } catch (e) { toastError(e); }
  });
}

async function panelSftpPage(box) {
  let settings = {};
  try { settings = await api("/api/settings"); } catch {}
  box.innerHTML = `<div class="page-narrow"><div class="panel">
    <h2>${t("panel.sftpAddr")}</h2>
    <label class="field"><span>${t("panel.sftpAddr")}</span><input type="text" id="ps-sftp" placeholder=":2222" autocomplete="off"></label>
    <p class="hint">${esc(t("panel.sftpHint"))}</p>
    <div class="modal-actions"><button class="btn btn-primary" id="ps-save">${t("settings.save")}</button></div>
  </div></div>`;
  box.querySelector("#ps-sftp").value = settings.sftpAddr || "";
  box.querySelector("#ps-save").addEventListener("click", async () => {
    try {
      await saveSettingsPatch({ sftpAddr: box.querySelector("#ps-sftp").value.trim() });
    } catch (e) { toastError(e); }
  });
}

async function panelDomainPage(box) {
  let settings = {};
  try { settings = await api("/api/settings"); } catch {}
  box.innerHTML = `<div class="page-narrow"><div class="panel">
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
  </div></div>`;
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

  box.querySelector("#ps-save").addEventListener("click", async () => {
    const body = {
      domain: box.querySelector("#ps-domain").value.trim(),
      dnsProvider: prov.value,
      dnsTarget: box.querySelector("#ps-dnstarget").value.trim()
    };
    const token = box.querySelector("#ps-dnstoken").value.trim();
    if (token) body.dnsToken = token;
    try {
      settings = await saveSettingsPatch(body);
      box.querySelector("#ps-dnstoken").value = "";
      box.querySelector("#ps-warnings").innerHTML =
        (settings.warnings || []).map((w) => `<div class="notice">${esc(w)}</div>`).join("");
      applyDnsUI();
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
}

async function panelIntegrationsPage(box) {
  let settings = {};
  try { settings = await api("/api/settings"); } catch {}
  box.innerHTML = `<div class="page-narrow"><div class="panel">
    <h2>${t("panel.curseForgeKey")}</h2>
    <label class="field"><span>${t("panel.curseForgeKey")}</span><input type="password" id="ps-cfkey" autocomplete="off"></label>
    <p class="hint">${esc(t("panel.curseForgeKeyHint"))}</p>
    <div class="modal-actions"><button class="btn btn-primary" id="ps-save">${t("settings.save")}</button></div>
  </div></div>`;
  const cfInput = box.querySelector("#ps-cfkey");
  cfInput.placeholder = settings.curseForgeKeySet ? t("panel.curseForgeKeySaved") : "";
  box.querySelector("#ps-save").addEventListener("click", async () => {
    const cfKey = cfInput.value.trim();
    if (!cfKey) return;
    try {
      settings = await saveSettingsPatch({ curseForgeKey: cfKey });
      cfInput.value = "";
      cfInput.placeholder = settings.curseForgeKeySet ? t("panel.curseForgeKeySaved") : "";
    } catch (e) { toastError(e); }
  });
}

async function panelUsersPage(box) {
  const permKeys = ["view", "console", "files", "control", "settings", "backups", "delete"];
  let serverList = [];
  try { serverList = await api("/api/servers"); } catch {}
  serverList = serverList.filter((s) => !s.nodeId);

  box.innerHTML = `<div class="page-narrow"><div class="panel">
    <h2>${t("users.title")}</h2>
    <p class="hint">${t("users.hint")}</p>
    <div id="ps-users"></div>
    <div class="form-row">
      <label class="field"><span>${t("login.username")}</span><input id="ps-u-name" maxlength="32"></label>
      <label class="field"><span>${t("login.password")}</span><input type="password" id="ps-u-pass"></label>
      <label class="field"><span>${t("users.role")}</span>
        <select id="ps-u-role"><option value="user">user</option><option value="admin">admin</option></select></label>
    </div>
    <button class="btn btn-sm" id="ps-u-add">${t("users.add")}</button>
  </div></div>`;

  const loadUsers = async () => {
    const host = box.querySelector("#ps-users");
    if (!host) return;
    try {
      const list = await api("/api/users");
      host.innerHTML = list.map((u) => {
        const isAdmin = (u.role || "admin") === "admin";
        let accessHTML = "";
        if (!isAdmin) {
          accessHTML = `<div class="u-access" data-u="${esc(u.username)}" style="margin:.4rem 0 .8rem .5rem">
            ${serverList.map((s) => {
              const a = (u.access && u.access[s.id]) || {};
              return `<div class="hint"><b>${esc(s.name)}</b>
                ${permKeys.map((p) =>
                  `<label class="check" style="display:inline;margin-right:.5rem"><input type="checkbox" data-sid="${esc(s.id)}" data-perm="${p}" ${a[p] ? "checked" : ""}> ${p}</label>`
                ).join("")}</div>`;
            }).join("")}
            <button class="btn btn-sm u-save-access" data-u="${esc(u.username)}">${t("users.saveAccess")}</button>
          </div>`;
        }
        return `<div class="u-row">
          <span><b>${esc(u.username)}</b> <span class="badge">${esc(u.role || "admin")}</span>
          ${u.username === me ? "" : `<button class="btn btn-ghost btn-sm u-del" data-u="${esc(u.username)}">${ICONS.trash}</button>`}</span>
          ${accessHTML}</div>`;
      }).join("") || `<p class="hint">${t("users.hint")}</p>`;
      host.querySelectorAll(".u-del").forEach((btn) => {
        btn.addEventListener("click", async () => {
          if (!(await confirmModal(t("users.deleteConfirm"), true))) return;
          try {
            await api("/api/users/" + encodeURIComponent(btn.dataset.u), { method: "DELETE" });
            loadUsers();
          } catch (e) { toastError(e); }
        });
      });
      host.querySelectorAll(".u-save-access").forEach((btn) => {
        btn.addEventListener("click", async () => {
          const wrap = host.querySelector(`.u-access[data-u="${CSS.escape(btn.dataset.u)}"]`);
          const access = {};
          wrap.querySelectorAll("input[type=checkbox]").forEach((cb) => {
            const sid = cb.dataset.sid;
            if (!access[sid]) access[sid] = {};
            access[sid][cb.dataset.perm] = cb.checked;
          });
          // Ensure view is on if any other perm is on
          Object.keys(access).forEach((sid) => {
            const a = access[sid];
            if (a.console || a.files || a.control || a.settings || a.backups || a.delete) a.view = true;
            if (!a.view) delete access[sid];
          });
          try {
            await api("/api/users/" + encodeURIComponent(btn.dataset.u), {
              method: "PATCH", body: { role: "user", access }
            });
            toast(t("users.accessSaved"), "ok");
            loadUsers();
          } catch (e) { toastError(e); }
        });
      });
    } catch (e) { host.textContent = ""; }
  };
  loadUsers();
  box.querySelector("#ps-u-add").addEventListener("click", async () => {
    try {
      await api("/api/users", {
        method: "POST",
        body: {
          username: box.querySelector("#ps-u-name").value.trim(),
          password: box.querySelector("#ps-u-pass").value,
          role: box.querySelector("#ps-u-role").value
        }
      });
      box.querySelector("#ps-u-name").value = "";
      box.querySelector("#ps-u-pass").value = "";
      loadUsers();
      toast(t("users.added"), "ok");
    } catch (e) { toastError(e); }
  });
}

async function panelNodesPage(box) {
  box.innerHTML = `<div class="page-narrow"><div class="panel">
    <h2>${t("nodes.title")}</h2>
    <p class="hint">${t("nodes.hint")}</p>
    <div id="ps-nodes"></div>
    <div class="form-row">
      <label class="field"><span>${t("nodes.name")}</span><input id="ps-node-name" maxlength="40" placeholder="node-2"></label>
      <button class="btn btn-sm btn-primary" id="ps-node-add">${t("nodes.enroll")}</button>
    </div>
    <div id="ps-node-join" hidden>
      <p class="hint">${t("nodes.once")}</p>
      <pre id="ps-node-once" class="hint" style="white-space:pre-wrap;user-select:all"></pre>
      <button class="btn btn-sm" type="button" id="ps-node-copy">${t("nodes.copy")}</button>
    </div>
  </div></div>`;

  const loadNodes = async () => {
    const host = box.querySelector("#ps-nodes");
    if (!host) return;
    try {
      const list = await api("/api/nodes");
      host.innerHTML = list.length
        ? `<ul class="plist">${list.map((n) =>
          `<li><span><b>${esc(n.name)}</b> <span class="badge ${n.online ? "st-running" : "st-install_failed"}"><i class="led"></i>${n.online ? "online" : "offline"}</span>
            <span class="plg-desc">${n.serverCount || 0} servers${n.apiReady ? " · API" : n.online ? " · " + t("nodes.noApi") : ""}</span>
            ${n.apiUrl ? `<span class="plg-desc mono">${esc(n.apiUrl)}</span>` : ""}</span>
            <button class="btn btn-ghost btn-sm node-del" data-id="${esc(n.id)}">${ICONS.trash}</button></li>`).join("")}</ul>`
        : `<p class="hint">${t("nodes.empty")}</p>`;
      host.querySelectorAll(".node-del").forEach((btn) => {
        btn.addEventListener("click", async () => {
          try {
            await api("/api/nodes/" + encodeURIComponent(btn.dataset.id), { method: "DELETE" });
            loadNodes();
          } catch (e) { toastError(e); }
        });
      });
    } catch { host.textContent = ""; }
  };
  loadNodes();
  box.querySelector("#ps-node-add").addEventListener("click", async () => {
    try {
      const n = await api("/api/nodes", { method: "POST", body: { name: box.querySelector("#ps-node-name").value.trim() || "node" } });
      const join = n.joinCommand || `curl -fsSL '${location.origin}/api/nodes/bootstrap?token=${n.token}' | sudo bash`;
      box.querySelector("#ps-node-once").textContent = join;
      box.querySelector("#ps-node-join").hidden = false;
      box.querySelector("#ps-node-name").value = "";
      loadNodes();
    } catch (e) { toastError(e); }
  });
  box.querySelector("#ps-node-copy").addEventListener("click", async () => {
    const cmd = box.querySelector("#ps-node-once").textContent.trim();
    try {
      await navigator.clipboard.writeText(cmd);
      toast(t("nodes.copied"), "ok");
    } catch { toastError(new Error(t("error.generic"))); }
  });
}

async function panelAuditPage(box) {
  box.innerHTML = `<table class="tbl"><thead><tr>
      <th>${t("audit.time")}</th><th>${t("audit.actor")}</th><th>${t("audit.action")}</th><th>${t("audit.detail")}</th>
    </tr></thead><tbody id="audit-rows"></tbody></table>
    <p class="hint" id="audit-empty" hidden>${t("audit.empty")}</p>`;
  try {
    const list = await api("/api/audit?limit=100");
    const tbody = box.querySelector("#audit-rows");
    if (!list.length) {
      box.querySelector("#audit-empty").hidden = false;
      return;
    }
    tbody.innerHTML = list.map((e) => `<tr style="cursor:default">
      <td class="mono">${esc(fmtDate(new Date(e.time).getTime()))}</td>
      <td><b>${esc(e.actor)}</b></td>
      <td class="mono">${esc(e.action)}${e.serverId ? ` <span class="plg-desc">[${esc(e.serverId)}]</span>` : ""}</td>
      <td class="mono">${esc(e.detail || "")}</td>
    </tr>`).join("");
  } catch { box.querySelector("#audit-empty").hidden = false; }
}

async function panelTemplatesPage(box) {
  box.innerHTML = `<div class="page-narrow">
    <div class="panel">
      <h2>${t("templates.title")}</h2>
      <div id="ps-templates"></div>
    </div>
    <div class="panel">
      <h2>${t("import.title")}</h2>
      <p class="hint">${t("import.hint")}</p>
      <label class="field"><span>${t("create.name")}</span><input id="ps-imp-name" maxlength="40"></label>
      <div class="form-row">
        <label class="field"><span>${t("create.type")}</span>
          <select id="ps-imp-type"><option value="paper">Paper</option><option value="purpur">Purpur</option><option value="vanilla">Vanilla</option><option value="fabric">Fabric</option></select></label>
        <label class="field"><span>${t("create.version")}</span><input id="ps-imp-ver" placeholder="1.21.1"></label>
      </div>
      <input type="file" id="ps-imp-file" accept=".zip,application/zip">
      <button class="btn btn-sm" id="ps-imp-go">${t("import.button")}</button>
    </div>
  </div>`;

  const loadTemplates = async () => {
    const host = box.querySelector("#ps-templates");
    if (!host) return;
    try {
      const list = await api("/api/templates");
      host.innerHTML = list.length
        ? `<ul class="plist">${list.map((tpl) =>
          `<li><span><b>${esc(tpl.name)}</b> <span class="plg-desc">${esc(tpl.type)} ${esc(tpl.version)}</span></span>
            <button class="btn btn-sm tpl-use" data-id="${esc(tpl.id)}">${t("templates.use")}</button>
            <button class="btn btn-ghost btn-sm tpl-del" data-id="${esc(tpl.id)}">${ICONS.trash}</button></li>`).join("")}</ul>`
        : `<p class="hint">${t("templates.empty")}</p>`;
      host.querySelectorAll(".tpl-use").forEach((btn) => {
        btn.addEventListener("click", async () => {
          const res = await promptModal(t("create.name"));
          if (!res) return;
          try {
            const view = await api("/api/servers/from-template", {
              method: "POST", body: { templateId: btn.dataset.id, name: res }
            });
            location.hash = `#/server/${encodeURIComponent(view.id)}/console`;
          } catch (e) { toastError(e); }
        });
      });
      host.querySelectorAll(".tpl-del").forEach((btn) => {
        btn.addEventListener("click", async () => {
          try {
            await api("/api/templates/" + encodeURIComponent(btn.dataset.id), { method: "DELETE" });
            loadTemplates();
          } catch (e) { toastError(e); }
        });
      });
    } catch { host.textContent = ""; }
  };
  loadTemplates();

  box.querySelector("#ps-imp-go").addEventListener("click", async () => {
    const f = box.querySelector("#ps-imp-file").files[0];
    if (!f) return;
    const fd = new FormData();
    fd.append("file", f);
    fd.append("name", box.querySelector("#ps-imp-name").value.trim());
    fd.append("type", box.querySelector("#ps-imp-type").value);
    fd.append("version", box.querySelector("#ps-imp-ver").value.trim());
    try {
      const res = await fetch("/api/servers/import", {
        method: "POST", headers: { "X-Craftpanel": "1" }, body: fd
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw Object.assign(new Error(data.message || res.statusText), { code: data.error });
      location.hash = `#/server/${encodeURIComponent(data.id)}/console`;
    } catch (e) { toastError(e); }
  });
}

async function panelJavaPage(box) {
  box.innerHTML = `<div class="page-narrow"><div class="panel">
    <h2>${t("javaMgr.title")}</h2>
    <p class="hint">${t("javaMgr.hint")}</p>
    <div id="ps-java"></div>
    <div class="form-row">
      <label class="field"><span>${t("javaMgr.major")}</span>
        <select id="ps-java-maj"><option>17</option><option>21</option><option selected>22</option><option>25</option></select></label>
      <button class="btn btn-sm" id="ps-java-install">${t("javaMgr.install")}</button>
    </div>
  </div></div>`;

  const loadJava = async () => {
    const host = box.querySelector("#ps-java");
    if (!host) return;
    try {
      const list = await api("/api/java");
      host.innerHTML = list.length
        ? `<ul class="plist">${list.map((j) =>
          `<li><span>Java ${j.major} <span class="plg-desc">${esc(j.path)}</span></span>
            <button class="btn btn-ghost btn-sm java-copy" data-p="${esc(j.path)}">${t("javaMgr.usePath")}</button></li>`).join("")}</ul>`
        : `<p class="hint">${t("javaMgr.empty")}</p>`;
      host.querySelectorAll(".java-copy").forEach((btn) => {
        btn.addEventListener("click", async () => {
          try {
            await navigator.clipboard.writeText(btn.dataset.p);
            toast(t("javaMgr.copied"), "ok");
          } catch {}
        });
      });
    } catch { host.textContent = ""; }
  };
  loadJava();
  box.querySelector("#ps-java-install").addEventListener("click", async (e) => {
    e.target.disabled = true;
    try {
      const major = parseInt(box.querySelector("#ps-java-maj").value, 10);
      await api("/api/java", { method: "POST", body: { major } });
      toast(t("javaMgr.ok"), "ok");
      loadJava();
    } catch (err) { toastError(err); } finally { e.target.disabled = false; }
  });
}

/* ---------- account page (2FA + own API tokens) ---------- */

async function renderAccountPage() {
  currentDetailId = null;
  try {
    const info = await api("/api/me");
    meTotp = !!info.totp;
    meRecoveryLeft = info.recoveryRemaining || 0;
  } catch {}
  const c = content();
  c.innerHTML = `<div class="page-head"><h1>${t("account.title")}</h1>
      <span class="sum">${esc(me)}${meAdmin ? " · admin" : ""}</span></div>
    <div class="page-narrow">
      <div class="panel"><h2>${t("totp.title")}</h2><div id="totp-body"></div></div>
      <div class="panel">
        <h2>${t("tokens.title")}</h2>
        <p class="hint">${t("tokens.hint")}</p>
        <div id="ps-tokens"></div>
        <div class="form-row">
          <label class="field"><span>${t("tokens.name")}</span><input id="ps-tok-name" maxlength="64"></label>
          <button class="btn btn-sm" id="ps-tok-add">${t("tokens.create")}</button>
        </div>
        <pre id="ps-tok-once" class="hint" style="white-space:pre-wrap"></pre>
      </div>
    </div>`;
  renderTotpBody(c.querySelector("#totp-body"));

  const loadTokens = async () => {
    const host = c.querySelector("#ps-tokens");
    if (!host) return;
    try {
      const list = await api("/api/tokens");
      host.innerHTML = list.length
        ? `<ul class="plist">${list.map((tok) =>
          `<li><span>${esc(tok.name)} <span class="plg-desc">${esc(tok.id)}</span></span>
            <button class="btn btn-ghost btn-sm tok-del" data-id="${esc(tok.id)}">${ICONS.trash}</button></li>`).join("")}</ul>`
        : `<p class="hint">${t("tokens.empty")}</p>`;
      host.querySelectorAll(".tok-del").forEach((btn) => {
        btn.addEventListener("click", async () => {
          try {
            await api("/api/tokens/" + encodeURIComponent(btn.dataset.id), { method: "DELETE" });
            loadTokens();
          } catch (e) { toastError(e); }
        });
      });
    } catch (e) { host.textContent = ""; }
  };
  loadTokens();
  c.querySelector("#ps-tok-add").addEventListener("click", async () => {
    const name = c.querySelector("#ps-tok-name").value.trim() || "api";
    try {
      const tok = await api("/api/tokens", { method: "POST", body: { name } });
      c.querySelector("#ps-tok-once").textContent = t("tokens.once") + "\n" + tok.token;
      c.querySelector("#ps-tok-name").value = "";
      loadTokens();
    } catch (e) { toastError(e); }
  });
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

function showRecoveryCodes(codes) {
  if (!codes || !codes.length) return;
  const list = codes.map((c) => `<li><code>${esc(c)}</code></li>`).join("");
  const dlg = el(`<div class="overlay" id="totp-recovery-modal">
    <div class="modal">
      <h2>${t("totp.recoveryTitle")}</h2>
      <p class="hint">${t("totp.recoveryHint")}</p>
      <ul class="totp-recovery-list">${list}</ul>
      <div class="modal-actions">
        <button class="btn btn-sm" id="totp-recovery-copy">${t("totp.recoveryCopy")}</button>
        <button class="btn btn-ok btn-sm" id="totp-recovery-close">${t("totp.recoverySaved")}</button>
      </div>
    </div>
  </div>`);
  document.body.appendChild(dlg);
  dlg.querySelector("#totp-recovery-copy").addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(codes.join("\n"));
      toast(t("totp.recoveryCopied"), "ok");
    } catch { toast(codes.join("\n"), "ok"); }
  });
  dlg.querySelector("#totp-recovery-close").addEventListener("click", () => dlg.remove());
}

function renderTotpBody(host) {
  if (meTotp) {
    const left = typeof meRecoveryLeft === "number" ? meRecoveryLeft : "?";
    host.innerHTML = `<p class="hint">${t("totp.on")}</p>
      <p class="hint">${t("totp.recoveryLeft", { n: left })}</p>
      <div class="add-row">
        <input type="text" id="totp-code" placeholder="${esc(t("totp.code"))}" inputmode="text" maxlength="12" autocomplete="one-time-code">
        <button class="btn btn-sm" id="totp-regen">${t("totp.recoveryRegen")}</button>
        <button class="btn btn-danger btn-sm" id="totp-off">${t("totp.disable")}</button>
      </div>`;
    host.querySelector("#totp-off").addEventListener("click", async () => {
      try {
        await api("/api/account/totp/disable", { method: "POST", body: { code: host.querySelector("#totp-code").value.trim() } });
        meTotp = false;
        meRecoveryLeft = 0;
        renderTotpBody(host);
        toast(t("totp.off"), "ok");
      } catch (e) { toastError(e); }
    });
    host.querySelector("#totp-regen").addEventListener("click", async () => {
      try {
        const res = await api("/api/account/totp/recovery", { method: "POST", body: { code: host.querySelector("#totp-code").value.trim() } });
        meRecoveryLeft = (res.recoveryCodes || []).length;
        showRecoveryCodes(res.recoveryCodes);
        renderTotpBody(host);
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
      ${init.qr ? `<img class="totp-qr" src="${esc(init.qr)}" alt="TOTP QR" width="220" height="220">` : ""}
      <p class="totp-secret"><code>${esc(init.secret)}</code></p>
      <p class="hint"><code class="wrap">${esc(init.url)}</code></p>
      <div class="add-row">
        <input type="text" id="totp-code2" placeholder="${esc(t("totp.code"))}" inputmode="numeric" maxlength="6" autocomplete="one-time-code">
        <button class="btn btn-ok btn-sm" id="totp-confirm">${t("totp.confirm")}</button>
      </div>`;
    setup.querySelector("#totp-confirm").addEventListener("click", async () => {
      try {
        const res = await api("/api/account/totp/enable", { method: "POST", body: { code: setup.querySelector("#totp-code2").value.trim() } });
        meTotp = true;
        meRecoveryLeft = (res.recoveryCodes || []).length;
        showRecoveryCodes(res.recoveryCodes);
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
