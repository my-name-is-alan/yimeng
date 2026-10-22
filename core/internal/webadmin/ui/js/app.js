// 译梦 web console: shell, username/password sign-in, hash router.
import { get, post, store, connectWS, closeWS, onUnauthorized, onEvent, wsState } from "./api.js";
import { h, icon, $, toast, errToast, empty, strengthMeter, pwIssues } from "./ui.js";
import { state, isAdmin } from "./state.js";
import { player, mountNowBar, closeNP } from "./player.js";
import * as lib from "./library.js";
import * as adm from "./admin.js";
import { lyricsPage } from "./lyrics.js";
import { metadataPage } from "./metadata.js";
import * as onl from "./online.js";
import { initHScroll } from "./hscroll.js";
import { initSelects } from "./select.js";

adm.applyTheme();
matchMedia("(prefers-color-scheme: light)").addEventListener("change", adm.applyTheme);
const app = $("#app"), main = $("#main");
const navigate = (hash) => { location.hash = hash; };

const NAV = [
  ["曲库", [["#/", "首页", "home"], ["#/library", "曲库", "track"], ["#/favorites", "典藏", "heart"], ["#/albums", "专辑", "album", "local"], ["#/artists", "艺人", "artist", "local"], ["#/playlists", "歌单", "list"], ["#/history", "播放历史", "history"]]],
  ["管理", [["#/dashboard", "仪表盘", "dash"], ["#/metadata", "元数据", "edit"], ["#/lyrics", "歌词匹配", "lyrics"], ["#/devices", "设备", "device"], ["#/users", "用户", "artist"], ["#/settings", "设置", "settings"], ["#/logs", "日志", "logs"]]],
];
const TABS = [["#/", "首页", "home"], ["#/library", "曲库", "track"], ["#/search", "搜索", "search", true], ["#/playlists", "歌单", "list"], ["#/dashboard", "管理", "dash"]];

function shell() {
  // Local-only sections are hidden in online-only mode or while the local library is empty.
  const hideLocal = onl.localHidden();
  $("#nav").replaceChildren(...NAV.flatMap(([sec, items]) => [h("div", { class: "nav-sec" }, sec), ...items.filter((x) => !(hideLocal && x[3] === "local")).map(([href, label, ic]) => h("a", { class: "nav-item", href }, icon(ic), label))]));
  $("#tabbar").replaceChildren(...TABS.map(([href, label, ic, raised]) => h("a", { href, class: raised ? "raised" : "" }, raised ? h("span", { class: "orb" }, icon(ic)) : icon(ic), raised ? null : label)));
  const wsDot = h("span", { class: "dot" });
  const syncWs = () => { wsDot.className = `dot ${wsState.connected ? "ok" : "err"}`; wsDot.title = wsState.connected ? "实时连接正常" : "实时连接断开，重连中"; };
  onEvent("ws.status", syncWs); syncWs();
  $("#sideFoot").replaceChildren(
    h("div", { class: "row" }, wsDot, h("span", { class: "ellipsis", style: { flex: 1 } }, state.me.name), h("span", { class: `badge ${isAdmin() ? "ok" : ""}` }, isAdmin() ? "管理员" : "成员")),
    h("div", { class: "row" },
      h("button", { class: "btn sm ghost", title: "切换主题", onclick: () => { const cur = document.documentElement.dataset.theme; localStorage.setItem("yimeng.theme", cur === "dark" ? "light" : "dark"); adm.applyTheme(); } }, icon("moon"), "主题"),
      h("span", { class: "spacer" }),
      h("button", { class: "btn sm ghost", title: "退出登录", onclick: logout }, icon("logout"), "退出")));
  $("#searchForm").onsubmit = (e) => { e.preventDefault(); navigate(`#/search?q=${encodeURIComponent($("#searchInput").value)}`); };
  addEventListener("keydown", (e) => {
    if ((e.key === "/" || (e.key === "k" && (e.metaKey || e.ctrlKey))) && !e.target.closest("input,textarea")) { e.preventDefault(); const i = $("#searchInput"); if (i.offsetParent) i.focus(); else navigate("#/search"); }
  });
}

async function logout() {
  if (!confirm("确定退出登录？")) return;
  try { await post("/auth/logout"); } catch {}
  store.token = ""; closeWS(); player.clear(); location.hash = "#/"; location.reload();
}

// ── sign-in / first-run setup ──
async function loginView(msg) {
  app.dataset.state = "login"; closeNP();
  let setup = false;
  try { setup = (await get("/auth/setup")).needs_setup; } catch {}
  const user = h("input", { class: "input", autocomplete: "username", "aria-label": "用户名", spellcheck: false, autocapitalize: "off", placeholder: setup ? "例如 admin" : "" });
  const pw = h("input", { class: "input", type: "password", autocomplete: setup ? "new-password" : "current-password", "aria-label": "密码" });
  const pw2 = h("input", { class: "input", type: "password", autocomplete: "new-password", "aria-label": "确认密码" });
  const err = h("div", { role: "alert", style: { color: "var(--danger)", minHeight: "1.2em", textAlign: "center", fontSize: "14px" } }, msg || "");
  const btn = h("button", { class: "btn primary lg", type: "submit" }, setup ? "创建管理员并进入" : "登录");
  const form = h("form", { class: "login-card", onsubmit: async (e) => {
    e.preventDefault(); err.textContent = "";
    if (setup) {
      const iss = pwIssues(user.value, pw.value);
      if (iss.length) { err.textContent = "密码强度不足：" + iss.join("；"); return; }
      if (pw.value !== pw2.value) { err.textContent = "两次输入的密码不一致"; return; }
    }
    btn.disabled = true;
    try {
      const r = await post(setup ? "/auth/setup" : "/auth/login", { username: user.value.trim(), password: pw.value, device_name: `网页 · ${browserName()}`, device_type: "web" });
      store.token = r.token; toast(setup ? "管理员账号已创建" : `欢迎回来，${r.user.username}`); boot();
    } catch (ex) {
      err.textContent = ex.status === 401 ? "用户名或密码错误" : ex.status === 429 ? "失败次数过多，已临时锁定 15 分钟" : ex.status === 409 && setup ? "管理员已存在，请刷新后登录" : ex.message;
      btn.disabled = false; pw.select();
    }
  } },
    h("span", { class: "brand-orb", "aria-hidden": "true" }),
    h("h1", null, setup ? "欢迎使用译梦" : "登录译梦 Core"),
    h("p", { class: "lead" }, setup ? "首次启动：创建管理员账号。密码只以 argon2id 哈希保存在 Core 中。" : "使用用户名和密码登录网页控制台"),
    h("label", { class: "field" }, h("span", null, "用户名"), user),
    h("label", { class: "field" }, h("span", null, "密码"), pw, ...(setup ? strengthMeter(user, pw) : [])),
    setup ? h("label", { class: "field" }, h("span", null, "确认密码"), pw2) : null,
    err, btn,
    setup ? null : h("details", null, h("summary", { class: "muted", style: { cursor: "pointer", fontSize: "13px" } }, "忘记密码？"),
      h("p", { class: "muted", style: { fontSize: "13px" } }, "在运行 Core 的机器上执行 ", h("code", null, "yimeng-core reset-password <用户名>"),
        "（Docker：", h("code", null, "docker exec -it yimeng yimeng-core reset-password admin"), "）。也可以设置环境变量 ", h("code", null, "YIMENG_ADMIN_RESET=1"), " 重启一次以重新进入首次设置。")));
  main.replaceChildren(h("div", { class: "login" }, form));
  user.focus();
}
const browserName = () => { const u = navigator.userAgent; return /Edg\//.test(u) ? "Edge" : /Chrome\//.test(u) ? "Chrome" : /Safari\//.test(u) ? "Safari" : /Firefox\//.test(u) ? "Firefox" : "浏览器"; };

// ── router ──
let cleanups = [];
const ROUTES = [
  [/^\/?$/, (m, p, c) => lib.home(main, p, c)],
  [/^\/dashboard$/, (m, p, c) => adm.dashboard(main, c)],
  [/^\/albums$/, (m, p) => lib.albums(main, p)],
  [/^\/album\/([\w-]+)$/, (m) => lib.album(main, m[1])],
  [/^\/album\/([\w-]+):(.+)$/, (m) => onl.detail(main, m[1], "album", m[2])],
  [/^\/artist\/([\w-]+):(.+)$/, (m) => onl.detail(main, m[1], "artist", m[2])],
  [/^\/artists$/, (m, p) => lib.artists(main, p)],
  [/^\/artist\/([\w-]+)$/, (m) => lib.artist(main, m[1])],
  [/^\/tracks$/, (m, p) => lib.tracks(main, p)],
  [/^\/library$/, (m, p) => lib.libraryPage(main, p)],
  [/^\/search$/, (m, p) => lib.search(main, p)],
  [/^\/playlists$/, (m, p) => lib.playlists(main, p)],
  [/^\/charts$/, (m, p) => onl.charts(main, p)],
  [/^\/online\/([\w-]+)\/(album|artist|playlist)\/([^/]+)$/, (m) => onl.detail(main, m[1], m[2], decodeURIComponent(m[3]))],
  [/^\/playlist\/([\w-]+)$/, (m) => lib.playlist(main, m[1])],
  [/^\/favorites$/, () => lib.favorites(main)],
  [/^\/history$/, () => lib.historyPage(main)],
  [/^\/metadata$/, (m, p) => metadataPage(main, p)],
  [/^\/lyrics(?:\/([\w-]+))?$/, (m, p, c) => lyricsPage(main, m[1], c)],
  [/^\/devices$/, () => adm.devices(main)],
  [/^\/users$/, () => adm.users(main)],
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
  catch (e) { if (e.status === 401) { store.token = ""; return loginView("登录已失效，请重新登录"); } main.replaceChildren(empty(`无法连接 Core：${e.message}`, "cloud", h("button", { class: "btn", onclick: boot }, "重试"))); return; }
  app.dataset.state = "ready";
  await onl.loadMode();
  shell();
  onEvent("plugin.updated", async () => { await onl.loadMode(); shell(); });
  if (!booted) { booted = true; mountNowBar(navigate); player.restore(); }
  connectWS();
  onEvent("library.cover", () => {}); // reserved for future live refresh
  route();
}
onUnauthorized(() => { if (app.dataset.state === "ready") { store.token = ""; closeWS(); loginView("登录已失效或会话已被撤销，请重新登录"); } });
initHScroll();
initSelects();
boot();
