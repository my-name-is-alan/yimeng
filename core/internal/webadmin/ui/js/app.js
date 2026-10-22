// 译梦 web console: shell, pairing/login, hash router.
import { get, post, store, connectWS, closeWS, onUnauthorized, onEvent, wsState } from "./api.js";
import { h, icon, $, toast, errToast, empty } from "./ui.js";
import { state, isAdmin } from "./state.js";
import { player, mountNowBar, closeNP } from "./player.js";
import * as lib from "./library.js";
import * as adm from "./admin.js";
import { lyricsPage } from "./lyrics.js";
import { metadataPage } from "./metadata.js";

adm.applyTheme();
matchMedia("(prefers-color-scheme: light)").addEventListener("change", adm.applyTheme);
const app = $("#app"), main = $("#main");
const navigate = (hash) => { location.hash = hash; };

const NAV = [
  ["曲库", [["#/", "首页", "home"], ["#/albums", "专辑", "album"], ["#/artists", "艺人", "artist"], ["#/tracks", "歌曲", "track"], ["#/playlists", "歌单", "list"], ["#/favorites", "收藏", "heart"], ["#/history", "播放历史", "history"]]],
  ["管理", [["#/dashboard", "仪表盘", "dash"], ["#/metadata", "元数据", "edit"], ["#/lyrics", "歌词匹配", "lyrics"], ["#/devices", "设备", "device"], ["#/settings", "设置", "settings"], ["#/logs", "日志", "logs"]]],
];
const TABS = [["#/", "首页", "home"], ["#/albums", "曲库", "album"], ["#/search", "搜索", "search", true], ["#/playlists", "歌单", "list"], ["#/dashboard", "管理", "dash"]];

function shell() {
  $("#nav").replaceChildren(...NAV.flatMap(([sec, items]) => [h("div", { class: "nav-sec" }, sec), ...items.map(([href, label, ic]) => h("a", { class: "nav-item", href }, icon(ic), label))]));
  $("#tabbar").replaceChildren(...TABS.map(([href, label, ic, raised]) => h("a", { href, class: raised ? "raised" : "" }, raised ? h("span", { class: "orb" }, icon(ic)) : icon(ic), raised ? null : label)));
  const wsDot = h("span", { class: "dot" });
  const syncWs = () => { wsDot.className = `dot ${wsState.connected ? "ok" : "err"}`; wsDot.title = wsState.connected ? "实时连接正常" : "实时连接断开，重连中"; };
  onEvent("ws.status", syncWs); syncWs();
  $("#sideFoot").replaceChildren(
    h("div", { class: "row" }, wsDot, h("span", { class: "ellipsis", style: { flex: 1 } }, state.me.name), h("span", { class: `badge ${isAdmin() ? "ok" : ""}` }, isAdmin() ? "管理员" : "成员")),
    h("div", { class: "row" },
      h("button", { class: "btn sm ghost", title: "切换主题", onclick: () => { const cur = document.documentElement.dataset.theme; localStorage.setItem("yimeng.theme", cur === "dark" ? "light" : "dark"); adm.applyTheme(); } }, icon("moon"), "主题"),
      h("span", { class: "spacer" }),
      h("button", { class: "btn sm ghost", title: "退出（仅清除本浏览器令牌）", onclick: logout }, icon("logout"), "退出")));
  $("#searchForm").onsubmit = (e) => { e.preventDefault(); navigate(`#/search?q=${encodeURIComponent($("#searchInput").value)}`); };
  addEventListener("keydown", (e) => {
    if ((e.key === "/" || (e.key === "k" && (e.metaKey || e.ctrlKey))) && !e.target.closest("input,textarea")) { e.preventDefault(); const i = $("#searchInput"); if (i.offsetParent) i.focus(); else navigate("#/search"); }
  });
}

function logout() {
  if (!confirm("退出后需要重新输入配对码。设备在 Core 中仍保持配对，可在「设备」页撤销。确定退出？")) return;
  store.token = ""; closeWS(); player.clear(); location.hash = "#/"; location.reload();
}

// ── pairing / login ──
function loginView(msg) {
  app.dataset.state = "login"; closeNP();
  const code = h("input", { class: "input code-input", placeholder: "XXXX-XXXX", autocomplete: "one-time-code", maxlength: 12, "aria-label": "配对码", spellcheck: false, autofocus: true });
  const name = h("input", { class: "input", value: localStorage.getItem("yimeng.devname") || `网页 · ${browserName()}`, "aria-label": "设备名称" });
  const err = h("div", { role: "alert", style: { color: "var(--danger)", minHeight: "1.2em", textAlign: "center", fontSize: "14px" } }, msg || "");
  const btn = h("button", { class: "btn primary lg", type: "submit" }, "配对并进入");
  code.addEventListener("input", () => { let v = code.value.toUpperCase().replace(/[^A-Z0-9]/g, ""); if (v.length > 4) v = v.slice(0, 4) + "-" + v.slice(4, 8); code.value = v; });
  const form = h("form", { class: "login-card", onsubmit: async (e) => {
    e.preventDefault(); err.textContent = ""; btn.disabled = true;
    try {
      const r = await post("/pair", { code: code.value.trim(), name: name.value.trim() || "网页", device_type: "web" });
      store.token = r.token; localStorage.setItem("yimeng.devname", name.value.trim());
      toast(`配对成功${r.device.admin ? " · 管理员" : ""}`); boot();
    } catch (ex) { err.textContent = ex.status === 401 ? "配对码无效或已过期" : ex.status === 429 ? "尝试过于频繁，请稍后再试" : ex.message; btn.disabled = false; }
  } },
    h("span", { class: "brand-orb", "aria-hidden": "true" }), h("h1", null, "连接到译梦 Core"), h("p", { class: "lead" }, "输入配对码，把这个浏览器配对为一台设备"),
    h("label", { class: "field" }, h("span", null, "配对码"), code), h("label", { class: "field" }, h("span", null, "设备名称"), name), err, btn,
    h("details", null, h("summary", { class: "muted", style: { cursor: "pointer", fontSize: "13px" } }, "如何获取配对码？"),
      h("ol", { class: "steps", style: { marginTop: "10px" } },
        h("li", null, "首次启动时，Core 会在日志中打印配对码（", h("code", null, "docker logs yimeng"), "）。第一台配对的设备自动成为管理员。"),
        h("li", null, "已配对的管理员可在「设备 → 配对新设备」生成配对码。"),
        h("li", null, "或点击下方按钮，让 Core 在日志里打印一个新配对码：")),
      h("button", { class: "btn sm", type: "button", style: { marginTop: "8px" }, onclick: async () => {
        try { await post("/pair/request"); toast("新的配对码已打印在 Core 日志中", "ok", 5000); } catch (ex) { errToast(ex); }
      } }, "在 Core 日志中打印配对码")));
  main.replaceChildren(h("div", { class: "login" }, form));
  code.focus();
}
const browserName = () => { const u = navigator.userAgent; return /Edg\//.test(u) ? "Edge" : /Chrome\//.test(u) ? "Chrome" : /Safari\//.test(u) ? "Safari" : /Firefox\//.test(u) ? "Firefox" : "浏览器"; };

// ── router ──
let cleanups = [];
const ROUTES = [
  [/^\/?$/, (m, p, c) => lib.home(main, p, c)],
  [/^\/dashboard$/, (m, p, c) => adm.dashboard(main, c)],
  [/^\/albums$/, (m, p) => lib.albums(main, p)],
  [/^\/album\/([\w-]+)$/, (m) => lib.album(main, m[1])],
  [/^\/artists$/, (m, p) => lib.artists(main, p)],
  [/^\/artist\/([\w-]+)$/, (m) => lib.artist(main, m[1])],
  [/^\/tracks$/, (m, p) => lib.tracks(main, p)],
  [/^\/search$/, (m, p) => lib.search(main, p)],
  [/^\/playlists$/, () => lib.playlists(main)],
  [/^\/playlist\/([\w-]+)$/, (m) => lib.playlist(main, m[1])],
  [/^\/favorites$/, () => lib.favorites(main)],
  [/^\/history$/, () => lib.historyPage(main)],
  [/^\/metadata$/, (m, p) => metadataPage(main, p)],
  [/^\/lyrics(?:\/([\w-]+))?$/, (m, p, c) => lyricsPage(main, m[1], c)],
  [/^\/devices$/, () => adm.devices(main)],
  [/^\/settings$/, () => adm.settings(main)],
  [/^\/logs$/, (m, p, c) => adm.logs(main, c)],
];
async function route() {
  if (app.dataset.state !== "ready") return;
  cleanups.forEach((f) => { try { f(); } catch {} }); cleanups = [];
  closeNP();
  const raw = location.hash.slice(1) || "/";
  const [path, query] = raw.split("?");
  const params = new URLSearchParams(query || "");
  for (const a of document.querySelectorAll(".nav-item, .tabbar a")) {
    const href = a.getAttribute("href").slice(1);
    const on = href === "/" ? path === "/" : path === href || path.startsWith(href + "/") || (href === "/albums" && path.startsWith("/album/"));
    on ? a.setAttribute("aria-current", "page") : a.removeAttribute("aria-current");
  }
  if (path === "/search") $("#searchInput").value = params.get("q") || "";
  const ctx = { cleanup: (f) => cleanups.push(f) };
  for (const [re, fn] of ROUTES) {
    const m = path.match(re);
    if (!m) continue;
    main.replaceChildren(h("div", { class: "skel", style: { height: "40px", width: "220px", marginBottom: "24px" } }), h("div", { class: "skel", style: { height: "240px" } }));
    try {
      const go = () => fn(m, params, ctx);
      if (document.startViewTransition && !matchMedia("(prefers-reduced-motion: reduce)").matches) await document.startViewTransition(go).updateCallbackDone;
      else await go();
    } catch (e) {
      if (e.status === 401) return;
      main.replaceChildren(empty(e.status === 404 ? "内容不存在或已被移除" : e.status === 403 ? "需要管理员设备权限" : `加载失败：${e.message}`, "disc", h("a", { class: "btn", href: "#/" }, "回到首页")));
    }
    main.focus({ preventScroll: true }); scrollTo({ top: 0 });
    return;
  }
  main.replaceChildren(empty("页面不存在", "disc", h("a", { class: "btn", href: "#/" }, "回到首页")));
}
addEventListener("hashchange", route);

let booted = false;
async function boot() {
  if (!store.token) return loginView();
  try { state.me = await get("/devices/me"); }
  catch (e) { if (e.status === 401) { store.token = ""; return loginView("令牌已失效，请重新配对"); } main.replaceChildren(empty(`无法连接 Core：${e.message}`, "cloud", h("button", { class: "btn", onclick: boot }, "重试"))); return; }
  app.dataset.state = "ready";
  shell();
  if (!booted) { booted = true; mountNowBar(navigate); player.restore(); }
  connectWS();
  onEvent("library.cover", () => {}); // reserved for future live refresh
  route();
}
onUnauthorized(() => { if (app.dataset.state === "ready") { store.token = ""; closeWS(); loginView("令牌已失效或设备已被撤销，请重新配对"); } });
boot();
