// Guard: native append/replaceChildren/prepend/before/after stringify null and
// undefined into literal "null"/"undefined" text. Drop them (and false) so
// optional sections can be passed inline everywhere.
export const clean = (kids) => kids.flat(Infinity).filter((c) => c !== null && c !== undefined && c !== false);
for (const proto of [Element.prototype, DocumentFragment.prototype, Document.prototype]) {
  for (const m of ["append", "prepend", "replaceChildren"]) {
    const orig = proto[m];
    if (orig && !orig.__ymGuard) { const f = function (...k) { return orig.apply(this, clean(k)); }; f.__ymGuard = true; proto[m] = f; }
  }
}
for (const m of ["before", "after", "replaceWith"]) {
  const orig = Element.prototype[m];
  if (orig && !orig.__ymGuard) { const f = function (...k) { return orig.apply(this, clean(k)); }; f.__ymGuard = true; Element.prototype[m] = f; }
}
// txt renders a possibly-missing value: null/undefined/"null"/NaN → fallback.
export const txt = (v, fallback = "") => (v === null || v === undefined || v === "null" || v === "undefined" || (typeof v === "number" && Number.isNaN(v)) ? fallback : String(v));
// DOM helpers, icons, toasts, dialogs, menus, formatting.
export function h(tag, attrs, ...kids) {
  const el = document.createElement(tag);
  if (attrs) for (const [k, v] of Object.entries(attrs)) {
    if (v === undefined || v === null || v === false) continue;
    if (k === "class") el.className = v;
    else if (k === "style" && typeof v === "object") Object.assign(el.style, v);
    else if (k.startsWith("on") && typeof v === "function") el.addEventListener(k.slice(2), v);
    else if (k === "html") el.innerHTML = v;
    else if (k === "value" || k === "checked" || k === "disabled" || k === "selected") el[k] = v;
    else el.setAttribute(k, v === true ? "" : v);
  }
  for (const c of clean(kids)) el.append(c instanceof Node ? c : document.createTextNode(txt(c)));
  return el;
}
export const $ = (s, r = document) => r.querySelector(s);
export const $$ = (s, r = document) => [...r.querySelectorAll(s)];

const P = {
  home: '<path d="M3 11 12 4l9 7"/><path d="M5 10v10h14V10"/>',
  dash: '<rect x="3" y="3" width="7" height="9" rx="2"/><rect x="14" y="3" width="7" height="5" rx="2"/><rect x="14" y="12" width="7" height="9" rx="2"/><rect x="3" y="16" width="7" height="5" rx="2"/>',
  album: '<circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="2.5"/>',
  artist: '<circle cx="12" cy="8" r="4"/><path d="M4 21c1.5-4 4.5-6 8-6s6.5 2 8 6"/>',
  track: '<path d="M9 18V5l11-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="17" cy="16" r="3"/>',
  list: '<path d="M4 6h16M4 12h10M4 18h7"/><path d="M17 15v6l4-3z"/>',
  heart: '<path d="M12 20s-7-4.4-9-9a5 5 0 0 1 9-3 5 5 0 0 1 9 3c-2 4.6-9 9-9 9z"/>',
  history: '<path d="M3 12a9 9 0 1 0 3-6.7L3 8"/><path d="M3 3v5h5M12 7v5l3 2"/>',
  edit: '<path d="M4 20h4L19 9l-4-4L4 16z"/><path d="m14 6 4 4"/>',
  lyrics: '<path d="M4 6h16M4 10h12M4 14h16M4 18h9"/>',
  device: '<rect x="6" y="2" width="12" height="20" rx="3"/><path d="M11 18h2"/>',
  settings: '<circle cx="12" cy="12" r="3"/><path d="M12 2v3M12 19v3M4.9 4.9l2.1 2.1M17 17l2.1 2.1M2 12h3M19 12h3M4.9 19.1 7 17M17 7l2.1-2.1"/>',
  logs: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="m7 9 3 3-3 3M13 15h4"/>',
  search: '<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>',
  play: '<path d="M7 4.5v15a1 1 0 0 0 1.5.9l12-7.5a1 1 0 0 0 0-1.8l-12-7.5A1 1 0 0 0 7 4.5z"/>',
  pause: '<rect x="6" y="4" width="4" height="16" rx="1"/><rect x="14" y="4" width="4" height="16" rx="1"/>',
  next: '<path d="M5 5v14l10-7zM17 5h2v14h-2z"/>',
  prev: '<path d="M19 5v14L9 12zM5 5h2v14H5z"/>',
  shuffle: '<path d="M16 3h5v5M4 20 21 3M21 16v5h-5M15 15l6 6M4 4l5 5"/>',
  repeat: '<path d="m17 2 4 4-4 4"/><path d="M3 11V9a3 3 0 0 1 3-3h15M7 22l-4-4 4-4"/><path d="M21 13v2a3 3 0 0 1-3 3H3"/>',
  queue: '<path d="M3 6h13M3 12h13M3 18h9"/><path d="m17 15 4 3-4 3z"/>',
  volume: '<path d="M11 5 6 9H2v6h4l5 4z"/><path d="M15.5 8.5a5 5 0 0 1 0 7M19 5a10 10 0 0 1 0 14"/>',
  mute: '<path d="M11 5 6 9H2v6h4l5 4z"/><path d="m22 9-6 6M16 9l6 6"/>',
  more: '<circle cx="5" cy="12" r="1.5"/><circle cx="12" cy="12" r="1.5"/><circle cx="19" cy="12" r="1.5"/>',
  close: '<path d="M6 6l12 12M18 6 6 18"/>',
  down: '<path d="m6 9 6 6 6-6"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  trash: '<path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3"/>',
  refresh: '<path d="M21 12a9 9 0 1 1-3-6.7L21 8"/><path d="M21 3v5h-5"/>',
  upload: '<path d="M12 16V4M7 9l5-5 5 5"/><path d="M4 16v4h16v-4"/>',
  download: '<path d="M12 4v12M7 11l5 5 5-5"/><path d="M4 20h16"/>',
  check: '<path d="m5 12 5 5L20 7"/>',
  link: '<path d="M10 14a5 5 0 0 0 7 0l3-3a5 5 0 0 0-7-7l-1 1"/><path d="M14 10a5 5 0 0 0-7 0l-3 3a5 5 0 0 0 7 7l1-1"/>',
  key: '<circle cx="8" cy="15" r="4"/><path d="m11 12 9-9M17 6l3 3"/>',
  disc: '<circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="3"/><path d="M12 3a9 9 0 0 1 9 9"/>',
  wand: '<path d="M15 4V2M15 10V8M11 6h2M17 6h2M4 20 14 10"/><path d="m18 14 1 1M20 10l1-1"/>',
  logout: '<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>',
  pulse: '<path d="M3 12h4l3-8 4 16 3-8h4"/>',
  folder: '<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>',
  cloud: '<path d="M7 18a5 5 0 1 1 1-9.9A6 6 0 0 1 19.5 10 4 4 0 0 1 18 18z"/>',
  speaker: '<rect x="5" y="2" width="14" height="20" rx="3"/><circle cx="12" cy="14" r="4"/><circle cx="12" cy="6" r="1"/>',
  moon: '<path d="M20 14.5A8 8 0 0 1 9.5 4 8 8 0 1 0 20 14.5z"/>',
  sparkle: '<path d="M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8z"/>',
};
export function icon(name, cls = "") {
  const s = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  s.setAttribute("viewBox", "0 0 24 24"); s.setAttribute("aria-hidden", "true");
  s.innerHTML = P[name] || "";
  const fill = ["play", "pause", "next", "prev"].includes(name);
  s.setAttribute("class", `${cls}${fill ? " fillsvg" : ""}`.trim());
  return s;
}

export function toast(msg, kind = "ok", ms = 2600) {
  const t = h("div", { class: `toast ${kind}` }, msg);
  document.getElementById("toasts").append(t);
  setTimeout(() => t.remove(), ms);
}
export const errToast = (e) => toast(e?.status === 403 ? "需要管理员设备权限" : (e?.message || String(e)), "err", 4200);

// Modal <dialog>: showModal + closedby="any" (light dismiss) with a click-outside fallback for Safari.
export function dialog({ title, body, foot, wide = false, onClose, tabs }) {
  const d = h("dialog", { class: wide ? "wide" : "", closedby: "any", "aria-label": title });
  d.append(h("div", { class: "dlg-head" }, h("h2", null, title), h("button", { class: "btn icon sm ghost", "aria-label": "关闭", onclick: () => d.close() }, icon("close"))));
  if (tabs) d.append(tabs);
  const b = h("div", { class: "dlg-body" }, body);
  d.append(b);
  const f = h("div", { class: "dlg-foot" }, foot);
  if (foot) d.append(f);
  if (!("closedBy" in HTMLDialogElement.prototype)) {
    d.addEventListener("click", (ev) => {
      if (ev.target !== d) return;
      const r = d.getBoundingClientRect();
      if (ev.clientX < r.left || ev.clientX > r.right || ev.clientY < r.top || ev.clientY > r.bottom) d.close();
    });
  }
  d.addEventListener("close", () => { onClose?.(); d.remove(); });
  document.body.append(d);
  d.showModal();
  d.bodyEl = b; d.footEl = f;
  return d;
}
export function confirmDlg(title, msg, okText = "确定", danger = false) {
  return new Promise((res) => {
    let ok = false;
    const d = dialog({ title, body: h("p", { class: "muted", style: { margin: 0 } }, msg),
      foot: [h("button", { class: "btn", onclick: () => d.close() }, "取消"),
             h("button", { class: `btn ${danger ? "danger" : "primary"}`, onclick: () => { ok = true; d.close(); } }, okText)],
      onClose: () => res(ok) });
  });
}
export function promptDlg(title, label, value = "", placeholder = "") {
  return new Promise((res) => {
    let out = null;
    const inp = h("input", { class: "input", value, placeholder });
    const d = dialog({ title, body: h("label", { class: "field" }, h("span", null, label), inp),
      foot: [h("button", { class: "btn", onclick: () => d.close() }, "取消"),
             h("button", { class: "btn primary", onclick: () => { out = inp.value; d.close(); } }, "确定")],
      onClose: () => res(out) });
    inp.addEventListener("keydown", (e) => { if (e.key === "Enter") { out = inp.value; d.close(); } });
    inp.focus();
  });
}

let openMenu;
addEventListener("hashchange", () => { openMenu?.remove(); openMenu = null; });
export function menu(anchor, items) {
  openMenu?.remove();
  const m = h("div", { class: "menu", role: "menu" });
  for (const it of items) {
    if (!it) continue;
    if (it === "-") { m.append(h("hr")); continue; }
    m.append(h("button", { role: "menuitem", class: it.danger ? "danger" : "", onclick: () => { m.remove(); it.run(); } }, it.icon ? icon(it.icon) : null, it.label));
  }
  document.body.append(m);
  const r = anchor.getBoundingClientRect();
  const mw = m.offsetWidth, mh = m.offsetHeight;
  m.style.left = Math.max(8, Math.min(r.right - mw, innerWidth - mw - 8)) + "px";
  m.style.top = (r.bottom + mh + 8 > innerHeight ? Math.max(8, r.top - mh - 6) : r.bottom + 6) + "px";
  openMenu = m;
  setTimeout(() => document.addEventListener("pointerdown", function off(e) { if (!m.contains(e.target)) { m.remove(); document.removeEventListener("pointerdown", off); } }), 0);
  m.addEventListener("keydown", (e) => { if (e.key === "Escape") { m.remove(); anchor.focus(); } });
  m.querySelector("button")?.focus();
}

export const fmtDur = (ms) => {
  if (!ms && ms !== 0) return "–";
  const s = Math.max(0, Math.round(ms / 1000)), hh = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), ss = s % 60;
  return hh ? `${hh}:${String(m).padStart(2, "0")}:${String(ss).padStart(2, "0")}` : `${m}:${String(ss).padStart(2, "0")}`;
};
export const fmtLong = (ms) => {
  const m = Math.round((ms || 0) / 60000);
  return m >= 60 ? `${Math.floor(m / 60)} 小时 ${m % 60} 分钟` : `${m} 分钟`;
};
export const fmtBytes = (b) => {
  if (!b) return "0 B";
  const u = ["B", "KB", "MB", "GB", "TB"]; let i = 0;
  while (b >= 1024 && i < u.length - 1) { b /= 1024; i++; }
  return `${b.toFixed(b < 10 && i ? 1 : 0)} ${u[i]}`;
};
export const fmtTime = (t) => t ? new Date(t).toLocaleString("zh-CN", { hour12: false }) : "–";
export const fmtAgo = (t) => {
  if (!t) return "–";
  const s = (Date.now() - new Date(t).getTime()) / 1000;
  if (s < 60) return "刚刚";
  if (s < 3600) return `${Math.floor(s / 60)} 分钟前`;
  if (s < 86400) return `${Math.floor(s / 3600)} 小时前`;
  return `${Math.floor(s / 86400)} 天前`;
};
export const fmtUptime = (sec) => {
  const d = Math.floor(sec / 86400), hh = Math.floor((sec % 86400) / 3600), m = Math.floor((sec % 3600) / 60);
  return d ? `${d} 天 ${hh} 小时` : hh ? `${hh} 小时 ${m} 分` : `${m} 分钟`;
};
export const fmtNum = (n) => (n ?? 0).toLocaleString("zh-CN");
export const initials = (s) => (s || "?").trim().slice(0, 1).toUpperCase();

// Cover art: gradient placeholder with initial, image fades in once decoded.
export function cover(src, name = "", cls = "") {
  const c = h("div", { class: `cover ${cls}` }, h("div", { class: "ph" }, initials(name)));
  if (src) {
    const img = h("img", { alt: "", loading: "lazy", decoding: "async", src });
    img.onload = () => img.classList.add("loaded");
    img.onerror = () => img.remove();
    c.append(img);
  }
  return c;
}
export function empty(msg, ic = "disc", action) {
  return h("div", { class: "empty" }, icon(ic), h("div", null, msg), action || null);
}
export const debounce = (fn, ms = 250) => { let t; return (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); }; };
export function setRangeFill(r) { const p = ((r.value - r.min) / ((r.max - r.min) || 1)) * 100; r.style.setProperty("--p", `${p}%`); }
export function conf(score) {
  const p = Math.round((score || 0) * 100);
  return h("span", { class: `conf ${p >= 80 ? "" : p >= 50 ? "mid" : "low"}` }, h("span", { class: "bar" }, h("i", { style: { width: `${p}%` } })), `${p}%`);
}

// Only local library items (origin "local") can be edited/matched; online
// StreamSource items are read-only (backend answers 409 not_local_source).
export const PROVIDER_LABEL = { musicbrainz: "MusicBrainz", spotify: "Spotify", lrclib: "LRCLIB", netease: "网易云", qq: "QQ音乐", kugou: "酷狗" };
export const providerLabel = (p) => PROVIDER_LABEL[p] || p || "未知来源";
const ORIGINS = { netease: "网易云", qq: "QQ 音乐", apple: "Apple Music", spotify: "Spotify", tidal: "TIDAL", qobuz: "Qobuz", kugou: "酷狗", kuwo: "酷我" };
export const isLocal = (x) => !x || !x.origin || x.origin === "local";
export const originName = (o) => ORIGINS[o] || o;
export const NOT_LOCAL_MSG = "在线来源的内容不可编辑或匹配，仅本地曲库支持";
export function sourceBadge(x) {
  return isLocal(x) ? null : h("span", { class: "badge src", title: `来源：${originName(x.origin)}（元数据只读）` }, originName(x.origin));
}

// Client-side mirror of the Core password policy (Core re-checks).
export function pwIssues(user, pw) {
  const out = [];
  if ([...pw].length < 10) out.push("至少 10 个字符");
  const classes = [/[a-z]/, /[A-Z]/, /[0-9]/, /[^a-zA-Z0-9]/].filter((re) => re.test(pw)).length;
  if (classes < 3 && [...pw].length < 16) out.push("包含小写、大写、数字、符号中的 3 类（或 16 位以上）");
  if (new Set(pw).size < 5) out.push("不要过于重复");
  if (user.trim().length >= 3 && pw.toLowerCase().includes(user.trim().toLowerCase())) out.push("不能包含用户名");
  return out;
}
export function strengthMeter(userEl, pwEl) {
  const bar = h("div", { class: "pw-meter" }, h("i")), hint = h("small", { class: "muted" });
  const upd = () => {
    const pw = pwEl.value, iss = pwIssues(userEl ? userEl.value : "", pw);
    const score = !pw ? 0 : iss.length ? Math.max(1, 3 - iss.length) : [...pw].length >= 14 ? 4 : 3;
    bar.dataset.score = score; hint.textContent = !pw ? "" : iss.length ? "还需要：" + iss.join("；") : score === 4 ? "强" : "良好";
  };
  pwEl.addEventListener("input", upd); userEl?.addEventListener("input", upd);
  return [bar, hint];
}

