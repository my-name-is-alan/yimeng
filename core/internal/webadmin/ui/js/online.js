// Online sources (音源插件): plugin settings, online home sections, charts,
// external album/artist/playlist pages, first-run mode wizard.
// Credentials are posted straight to Core; nothing secret is kept in the page.
import { get, post, put, del, qs } from "./api.js";
import { h, icon, cover, fmtNum, empty, toast, errToast, dialog, confirmDlg, originName } from "./ui.js";
import { player } from "./player.js";
import { trackList } from "./components.js";
import { isAdmin, nav, state } from "./state.js";

export const isExternal = (id) => typeof id === "string" && id.includes(":");
const split = (id) => { const i = id.indexOf(":"); return [id.slice(0, i), id.slice(i + 1)]; };
export const onlineHref = (kind, id) => { const [o, n] = split(id); return `#/online/${o}/${kind}/${encodeURIComponent(n)}`; };
const ERR = { plugin_login_required: "需要登录", risk_control: "平台风控，稍后再试", not_supported: "不支持", plugin_disabled: "未启用", upstream: "平台暂不可用" };

// Normalize an online Track DTO to the library track shape used by trackList/player.
export const normTrack = (t) => ({ ...t, album_id: t.album?.id || t.album_id || "", album_title: t.album?.name || t.album_title || "", cover_url: t.cover_url });
export const normTracks = (ts) => (ts || []).map(normTrack);

export function playableCounts(ts) {
  const c = { full: 0, preview: 0, unavailable: 0, local: 0 };
  for (const t of ts) { c[t.playable || "full"]++; if (t.local_id) c.local++; }
  return c;
}

// ── mode (first-run wizard) ──
export async function loadMode() {
  try { state.mode = await get("/settings/mode"); } catch { state.mode = { mode: "", local_tracks: 0, online_enabled: [] }; }
  return state.mode;
}
// localHidden: hide local-only sections in online mode or when the library is empty.
export const localHidden = () => { const m = state.mode; return !!m && (m.mode === "online" || (m.mode !== "local" && !m.local_tracks)); };

export function modeWizard(onDone) {
  let pick = state.mode?.local_tracks ? "both" : "online";
  const opts = [["local", "folder", "本地曲库", "只播放自己的音乐文件（NAS / 硬盘）"], ["online", "cloud", "在线音源", "登录网易云等平台账号，按账号权限播放"], ["both", "sparkle", "两者都要", "本地优先，在线补充（推荐）"]];
  const cards = opts.map(([v, ic, t, s]) => h("button", { class: "mode-card", "aria-pressed": String(v === pick), onclick: (e) => { pick = v; cards.forEach((c) => c.setAttribute("aria-pressed", c === e.currentTarget)); } },
    icon(ic), h("div", { class: "t" }, t), h("div", { class: "s" }, s)));
  const d = dialog({ title: "欢迎使用译梦", wide: true,
    body: [h("p", { class: "muted", style: { margin: 0 } }, "选择你的使用方式，之后可以在「设置」中随时修改。"), h("div", { class: "mode-grid" }, cards)],
    foot: [h("button", { class: "btn primary", onclick: async () => {
      try { state.mode = await put("/settings/mode", { mode: pick }); d.close(); onDone?.(pick); } catch (e) { errToast(e); }
    } }, "开始使用")] });
}

// ── plugin settings ──
const LOGIN = { qr: "扫码登录", cookie: "Cookie", password: "账号密码" };
export function pluginsSection(sec, reload) {
  const box = h("div", { class: "stack" }, h("div", { class: "skel", style: { height: "64px" } }));
  const s = sec("音源插件", "每个在线平台都是一个插件：启用、登录、设置音质与请求频率。账号凭据只保存在 Core 的加密 Vault 中，从不下发到网页或客户端。非会员账号仅能播放平台提供的试听片段。", box);
  const render = async () => {
    try {
      const r = await get("/plugins?check=1");
      box.replaceChildren(...(r.items.length ? r.items.map((p) => pluginRow(p, render)) : [empty("没有可用的音源插件", "cloud")]));
    } catch (e) { box.replaceChildren(empty(e.message)); }
  };
  render();
  return s;
}

function pluginRow(p, rerender) {
  const m = p.manifest, admin = isAdmin();
  const toggle = h("input", { type: "checkbox", class: "switch", checked: p.enabled, disabled: !admin, "aria-label": `启用 ${m.name}`, onchange: async () => {
    try { await put(`/plugins/${m.id}`, { enabled: toggle.checked }); toast(toggle.checked ? `已启用 ${m.name}` : `已停用 ${m.name}`); rerender(); } catch (e) { toggle.checked = !toggle.checked; errToast(e); }
  } });
  const acct = p.logged_in
    ? h("span", { class: "row", style: { gap: "6px" } }, p.account?.avatar ? h("img", { src: p.account.avatar, alt: "", style: { width: "20px", height: "20px", borderRadius: "50%" } }) : null,
      p.account?.nickname || "已登录", p.account ? h("span", { class: `badge ${p.account.vip ? "ok" : ""}` }, p.account.vip ? (p.account.tier || "会员") : "非会员") : null)
    : h("span", { class: "muted" }, "未登录 · 可匿名浏览与试听");
  const cool = p.cooldown_until ? h("span", { class: "badge warn", title: "触发平台风控，自动退避中" }, `退避至 ${new Date(p.cooldown_until).toLocaleTimeString("zh-CN", { hour12: false })}`) : null;
  const btns = [];
  if (admin && p.enabled) {
    if (p.logged_in) {
      btns.push(h("button", { class: "btn sm", onclick: async () => { try { await post(`/plugins/${m.id}/refresh`); toast("会话已刷新"); rerender(); } catch (e) { errToast(e); } } }, icon("refresh"), "刷新"));
      btns.push(h("button", { class: "btn sm danger", onclick: async () => { if (await confirmDlg("退出登录", `清除 ${m.name} 的登录会话？`, "退出", true)) { try { await del(`/plugins/${m.id}/session`); rerender(); } catch (e) { errToast(e); } } } }, "退出"));
    } else if (m.login_methods?.length) btns.push(h("button", { class: "btn sm primary", onclick: () => pluginLogin(p, rerender) }, icon("key"), "登录"));
    btns.push(h("button", { class: "btn sm", onclick: () => pluginConfig(p, rerender) }, icon("settings"), "设置"));
  }
  return h("div", { class: "li plugin-row", "data-plugin": m.id },
    h("div", { class: "src-logo", style: { background: { netease: "#e60026", qq: "#31c27c", kugou: "#2b8af7" }[m.id] || "#555" } }, { netease: "网", qq: "Q", kugou: "酷" }[m.id] || m.name[0]),
    h("div", { class: "grow" },
      h("div", { class: "t" }, m.name, " ", h("span", { class: `badge ${p.enabled ? "ok" : ""}` }, p.enabled ? "已启用" : "未启用"), m.skeleton ? h("span", { class: "badge warn" }, "开发中") : null, " ", cool),
      h("div", { class: "s" }, p.enabled ? acct : (m.description || ""), p.enabled ? ` · 音质 ${qName(p.config?.quality)} · ${p.config?.rate_per_min ?? "–"} 次/分` : ""),
      p.last_error ? h("div", { class: "s", style: { color: "var(--danger)" } }, p.last_error) : null),
    ...btns, h("label", { class: "row", title: "启用 / 停用" }, toggle));
}
const qName = (q) => ({ standard: "标准", high: "极高", lossless: "无损", hires: "Hi-Res" }[q] || q || "–");

function pluginConfig(p, done) {
  const m = p.manifest, inputs = {};
  const fields = (m.config || []).map((f) => {
    const v = p.config?.[f.key] ?? f.default;
    let el;
    if (f.type === "select") el = h("select", { class: "input" }, (f.options || []).map((o) => h("option", { value: o.value, selected: o.value === v }, o.label || o.value)));
    else if (f.type === "bool") el = h("input", { type: "checkbox", class: "switch", checked: !!v });
    else if (f.type === "number") el = h("input", { class: "input", type: "number", value: v ?? "", min: f.min, max: f.max });
    else el = h("input", { class: "input", value: v ?? "" });
    inputs[f.key] = [f, el];
    return h("label", { class: "field" }, h("span", null, f.label), el, f.help ? h("small", { class: "muted" }, f.help) : null);
  });
  const d = dialog({ title: `${m.name} · 设置`, body: [...fields, m.notice ? h("p", { class: "muted", style: { fontSize: "13px" } }, m.notice) : null],
    foot: [h("button", { class: "btn", onclick: () => d.close() }, "取消"), h("button", { class: "btn primary", onclick: async () => {
      const config = {};
      for (const [k, [f, el]] of Object.entries(inputs)) config[k] = f.type === "bool" ? el.checked : f.type === "number" ? Number(el.value) : el.value;
      try { await put(`/plugins/${m.id}`, { config }); toast("已保存"); d.close(); done(); } catch (e) { errToast(e); }
    } }, "保存")] });
}

export function pluginLogin(p, done) {
  const m = p.manifest, methods = m.login_methods || [];
  const body = h("div", { class: "stack" });
  let poll = null, alive = true;
  const stop = () => { clearTimeout(poll); poll = null; };
  const d = dialog({ title: `登录 ${m.name}`, body, onClose: () => { alive = false; stop(); done(); } });
  const ok = () => { toast("登录成功"); d.close(); };
  const show = (meth) => {
    stop();
    if (meth === "qr") {
      body.replaceChildren(h("div", { class: "skel", style: { width: "220px", height: "220px", justifySelf: "center" } }));
      post(`/plugins/${m.id}/login/qr`).then((c) => {
        const st = h("p", { class: "muted", style: { textAlign: "center", margin: 0 } }, `请使用 ${m.name} App 扫码`);
        body.replaceChildren(h("img", { class: "qr", src: c.image, alt: "登录二维码", width: 220, height: 220, style: { justifySelf: "center", background: "#fff", padding: "10px", borderRadius: "12px" } }), st,
          h("button", { class: "btn sm ghost", style: { justifySelf: "center" }, onclick: () => show("qr") }, icon("refresh"), "刷新二维码"));
        const tick = async () => {
          if (!alive) return;
          try {
            const r = await get(`/plugins/${m.id}/login/qr/${encodeURIComponent(c.key)}`);
            st.textContent = { waiting: "等待扫码…", scanned: `已扫码${r.nickname ? `（${r.nickname}）` : ""}，请在手机上确认`, confirmed: "登录成功", expired: "二维码已过期，请刷新" }[r.status] || r.status;
            if (r.status === "confirmed") return ok();
            if (r.status === "expired") return;
          } catch (e) { st.textContent = e.message; if (e.status === 429) { poll = setTimeout(tick, 10000); return; } return; }
          poll = setTimeout(tick, 2000);
        };
        poll = setTimeout(tick, 2000);
      }).catch((e) => body.replaceChildren(h("p", { class: "muted" }, e.message)));
    } else if (meth === "cookie") {
      const ta = h("textarea", { class: "input mono", rows: 4, placeholder: m.id === "netease" ? "MUSIC_U=…; __csrf=…" : "name=value; …", autocomplete: "off", spellcheck: false });
      body.replaceChildren(h("p", { class: "muted", style: { margin: 0 } }, "从已登录的网页版复制 Cookie。提交后直接加密保存在 Core，网页不保留。"), ta,
        h("button", { class: "btn primary", onclick: async () => { try { await post(`/plugins/${m.id}/login`, { method: "cookie", cookie: ta.value.trim() }); ta.value = ""; ok(); } catch (e) { errToast(e); } } }, "提交"));
    } else if (meth === "password") {
      const u = h("input", { class: "input", placeholder: "手机号或邮箱", autocomplete: "off" });
      const pw = h("input", { class: "input", type: "password", placeholder: "密码", autocomplete: "new-password" });
      body.replaceChildren(h("p", { class: "muted", style: { margin: 0 } }, "账号密码仅用于本次登录请求，Core 只保存平台返回的会话。频繁密码登录可能触发平台风控，推荐扫码。"),
        u, pw, h("button", { class: "btn primary", onclick: async () => { try { await post(`/plugins/${m.id}/login`, { method: "password", username: u.value.trim(), password: pw.value }); pw.value = ""; ok(); } catch (e) { pw.value = ""; errToast(e); } } }, "登录"));
    }
  };
  if (methods.length > 1) d.bodyEl.before(h("div", { class: "tabs" }, methods.map((x, i) => h("button", { "aria-selected": i === 0 ? "true" : "false", onclick: (e) => { [...e.currentTarget.parentNode.children].forEach((b) => b.setAttribute("aria-selected", b === e.currentTarget)); show(x); } }, LOGIN[x] || x))));
  show(methods[0]);
}

// ── cards / shelves ──
export function onlinePlaylistCard(pl) {
  return h("a", { class: "album-card", href: onlineHref("playlist", pl.id) },
    h("div", { class: "art" }, cover(pl.cover_url ? sized(pl.cover_url, 300) : "", pl.name),
      h("button", { class: "play-circle hover-play", "aria-label": `播放 ${pl.name}`, onclick: async (e) => { e.preventDefault(); e.stopPropagation(); try { const r = await get(`/online/${split(pl.id).join("/playlists/")}`); playOnline(normTracks(r.playlist.tracks)); } catch (err) { errToast(err); } } }, icon("play"))),
    h("div", { class: "t ellipsis", title: pl.name }, pl.name, pl.kind === "liked" ? h("span", { class: "badge src" }, "我喜欢") : null),
    h("div", { class: "s ellipsis" }, pl.subtitle || [pl.owner, pl.track_count ? `${pl.track_count} 首` : "", pl.play_count ? `${fmtCount(pl.play_count)} 次播放` : ""].filter(Boolean).join(" · ")));
}
export function onlineAlbumCard(al) {
  return h("a", { class: "album-card", href: onlineHref("album", al.id) }, h("div", { class: "art" }, cover(al.cover_url ? sized(al.cover_url, 300) : "", al.title)),
    h("div", { class: "t ellipsis" }, al.title), h("div", { class: "s ellipsis" }, [al.artist_name, al.year].filter(Boolean).join(" · ")));
}
export function onlineArtistCard(ar) {
  return h("a", { href: onlineHref("artist", ar.id) }, cover(ar.picture_url ? sized(ar.picture_url, 200) : "", ar.name, "round"),
    h("div", { class: "t ellipsis" }, ar.name), h("div", { class: "s" }, ar.album_count ? `${ar.album_count} 张专辑` : originName(ar.origin)));
}
const fmtCount = (n) => n >= 1e8 ? `${(n / 1e8).toFixed(1)}亿` : n >= 1e4 ? `${Math.round(n / 1e4)}万` : fmtNum(n);
// NetEase image CDN supports ?param=WxH; other hosts ignore unknown params.
export const sized = (u, s) => /music\.126\.net/.test(u) ? `${u.replace(/^http:/, "https:")}?param=${s}y${s}` : u;

export function playOnline(tracks, start = 0) {
  const ok = tracks.filter((t) => t.playable !== "unavailable");
  if (!ok.length) return toast("当前账号无权播放这些曲目", "err");
  const s = Math.max(0, ok.indexOf(tracks[start]));
  player.playList(ok, s);
}

export function onlineTracks(tracks) { return trackList(normTracks(tracks), { showCover: true, numbered: false }); }

export async function onlineHome() {
  const r = await get("/home");
  return r.sections.filter((s) => (s.tracks?.length || s.playlists?.length) && !s.error).map((s) => h("section", { class: "section" },
    h("div", { class: "section-head" }, h("h2", null, s.title), s.kind === "tracks" ? h("button", { class: "btn sm", onclick: () => playOnline(normTracks(s.tracks)) }, icon("play"), "播放全部") : null),
    s.kind === "tracks" ? onlineTracks(s.tracks.slice(0, 10)) : h("div", { class: "shelf" }, s.playlists.slice(0, 12).map(onlinePlaylistCard))));
}

// ── pages ──
const head = (title, sub, ...a) => h("div", { class: "page-head" }, h("div", { style: { flex: 1, minWidth: 0 } }, h("h1", null, title), sub ? h("div", { class: "sub" }, sub) : null), ...a);

export async function charts(main, params) {
  const origin = params.get("origin") || "";
  const r = await get(`/charts${qs({ origin })}`);
  const groups = r.items.filter((g) => g.items?.length);
  main.replaceChildren(head("排行榜", "来自已启用的在线音源"),
    ...(groups.length ? groups.map((g) => h("section", { class: "section" }, h("div", { class: "section-head" }, h("h2", null, originName(g.origin))),
      h("div", { class: "album-grid" }, g.items.map(onlinePlaylistCard)))) : [noPlugins()]));
}

export const noPlugins = () => empty("还没有启用在线音源。前往「设置 → 音源插件」启用网易云音乐等平台。", "cloud", h("a", { class: "btn primary", href: "#/settings" }, "去设置"));

export async function detail(main, origin, kind, native) {
  const id = `${origin}:${native}`;
  const path = { album: "albums", artist: "artists", playlist: "playlists" }[kind];
  const raw = await get(`/online/${origin}/${path}/${encodeURIComponent(native)}`);
  const x = kind === "playlist" ? raw.playlist : raw;
  const tracks = normTracks(kind === "artist" ? x.top_tracks : x.tracks);
  const c = playableCounts(tracks);
  const title = x.title || x.name, img = x.cover_url || x.picture_url;
  const facts = [originName(origin), kind === "album" ? [x.artist_name, x.year].filter(Boolean).join(" · ") : kind === "playlist" ? x.owner : "", `${tracks.length} 首`].filter(Boolean);
  const status = h("div", { class: "row wrap", style: { gap: "6px" } },
    c.full ? h("span", { class: "badge ok" }, `${c.full} 首可完整播放`) : null,
    c.preview ? h("span", { class: "badge warn" }, `${c.preview} 首仅试听`) : null,
    c.unavailable ? h("span", { class: "badge err" }, `${c.unavailable} 首不可播放`) : null,
    c.local ? h("span", { class: "badge src" }, `${c.local} 首本地已有`) : null);
  const actions = [h("button", { class: "play-circle", "aria-label": "播放", onclick: () => playOnline(tracks) }, icon("play"))];
  if (kind === "playlist") actions.push(h("button", { class: "btn", onclick: () => clone(origin, native, x) }, icon("plus"), "克隆为本地歌单"));
  main.replaceChildren(
    h("section", { class: "hero" }, h("div", { class: "bloom", style: { backgroundImage: img ? `url("${sized(img, 300)}")` : "none" } }),
      h("div", { class: "hero-row" }, cover(img ? sized(img, 600) : "", title, kind === "artist" ? "round" : ""),
        h("div", { class: "hero-meta" }, h("div", { class: "eyebrow" }, { album: "专辑", artist: "艺人", playlist: x.kind === "chart" ? "排行榜" : "歌单" }[kind], " ", h("span", { class: "badge src" }, originName(origin))),
          h("h1", null, title), h("div", { class: "facts" }, facts.join(" · ")),
          x.description ? h("div", { class: "facts ellipsis", style: { maxWidth: "640px" }, title: x.description }, x.description) : null, status,
          h("div", { class: "hero-actions" }, ...actions)))),
    h("div", { class: "section" }, tracks.length ? trackList(tracks, { showCover: true, numbered: kind === "album" }) : empty("没有曲目")));
  void id;
}

async function clone(origin, native, pl) {
  try {
    const r = await post(`/online/${origin}/playlists/${encodeURIComponent(native)}/clone`, { name: pl.name, prefer_local: true, skip_unavailable: false });
    toast(`已克隆 ${r.tracks} 首${r.linked_local ? `（${r.linked_local} 首使用本地文件）` : ""}`);
    nav(`#/playlist/${r.playlist.id}`);
  } catch (e) { errToast(e); }
}

// External playlists by scope for the 歌单 page.
export async function scopedPlaylists(scope) {
  const r = await get(`/playlists${qs({ scope })}`);
  const groups = r.groups.filter((g) => g.items?.length || g.error);
  if (!groups.length) return [noPlugins()];
  return groups.map((g) => h("section", { class: "section" }, h("div", { class: "section-head" }, h("h2", null, originName(g.origin))),
    g.items?.length ? h("div", { class: "album-grid" }, g.items.map(onlinePlaylistCard)) : empty(g.error === "plugin_login_required" ? "登录该平台账号后可查看" : (ERR[g.error] || g.error), "cloud")));
}

// Federated search groups.
export function searchGroups(groups) {
  return (groups || []).filter((g) => g.error !== "not supported" && !/not supported/.test(g.error || "")).map((g) => {
    const r = g.result;
    const parts = [h("div", { class: "section-head" }, h("h2", null, `${g.name || originName(g.origin)}`), g.error ? h("span", { class: "badge warn" }, ERR[g.error] || g.error) : null)];
    if (r?.tracks?.length) parts.push(onlineTracks(r.tracks.slice(0, 20)));
    if (r?.albums?.length) parts.push(h("div", { class: "eyebrow", style: { margin: "18px 0 8px" } }, "专辑"), h("div", { class: "shelf" }, r.albums.slice(0, 12).map(onlineAlbumCard)));
    if (r?.artists?.length) parts.push(h("div", { class: "eyebrow", style: { margin: "18px 0 8px" } }, "艺人"), h("div", { class: "artist-grid" }, r.artists.slice(0, 6).map(onlineArtistCard)));
    if (r?.playlists?.length) parts.push(h("div", { class: "eyebrow", style: { margin: "18px 0 8px" } }, "歌单"), h("div", { class: "shelf" }, r.playlists.slice(0, 12).map(onlinePlaylistCard)));
    return h("section", { class: "section online-group", "data-origin": g.origin }, ...parts);
  });
}
