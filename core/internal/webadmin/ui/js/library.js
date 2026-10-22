// Library pages: home, albums, album, artists, artist, tracks, search, playlists, favorites, history.
import { get, post, put, patch, del, qs, coverURL, api, artistImageURL } from "./api.js";
import { h, icon, cover, fmtDur, fmtLong, fmtNum, fmtAgo, empty, toast, errToast, confirmDlg, promptDlg, dialog, menu, debounce, isLocal, sourceBadge } from "./ui.js";
import { player } from "./player.js";
import { albumCard, artistCard, trackList, albumTracks, editAlbum, editArtist, editArtistImage, pager, addToPlaylist } from "./components.js";
import { isAdmin, nav, state } from "./state.js";
import { onlineHome, searchGroups, scopedPlaylists, localHidden, loadMode, modeWizard, noPlugins, normTrack } from "./online.js";
import { originName } from "./ui.js";

const head = (title, sub, ...actions) => h("div", { class: "page-head" }, h("div", { style: { flex: 1, minWidth: 0 } }, h("h1", null, title), sub ? h("div", { class: "sub" }, sub) : null), ...actions);
const skelGrid = (n = 12) => h("div", { class: "album-grid" }, Array.from({ length: n }, () => h("div", null, h("div", { class: "skel", style: { aspectRatio: 1 } }), h("div", { class: "skel", style: { height: "14px", marginTop: "10px", width: "70%" } }))));

export async function home(main) {
  const hour = new Date().getHours();
  const greet = hour < 6 ? "夜深了" : hour < 11 ? "早上好" : hour < 14 ? "中午好" : hour < 18 ? "下午好" : "晚上好";
  main.replaceChildren(head(greet, "今天想听点什么？"), skelGrid(6));
  const mode = await loadMode();
  if (!mode.configured && isAdmin()) modeWizard(() => home(main));
  const online = (mode.online_enabled?.length ? onlineHome() : Promise.resolve([])).catch(() => []);
  if (localHidden()) {
    const secs = await online;
    main.replaceChildren(head(greet, "在线音源 · 按账号权限播放"), ...(secs.length ? secs : [noPlugins()]));
    return;
  }
  const [recent, plays, otd, stats] = await Promise.all([
    get(`/albums${qs({ sort: "added", order: "desc", limit: 12 })}`),
    get(`/history/top${qs({ type: "album", period: "30d", limit: 12 })}`).catch(() => ({ items: [] })),
    (() => { const d = new Date(); return get(`/stats/on-this-day${qs({ month: d.getMonth() + 1, day: d.getDate() })}`).catch(() => null); })(),
    get("/stats").catch(() => null),
  ]);
  const shelf = (title, items, link) => h("section", { class: "section" }, h("div", { class: "section-head" }, h("h2", null, title), link ? h("a", { class: "btn sm ghost", href: link }, "查看全部") : null),
    h("div", { class: "shelf" }, items));
  const quick = h("div", { class: "row wrap" },
    h("button", { class: "btn primary lg", onclick: async () => { try { const p = await get(`/tracks${qs({ limit: 200, sort: "added", order: "desc" })}`); player.playList(p.items, Math.floor(Math.random() * p.items.length), { shuffle: true }); } catch (e) { errToast(e); } } }, icon("shuffle"), "随机播放全部"),
    h("a", { class: "btn lg", href: "#/favorites" }, icon("heart"), "我的典藏"));
  const parts = [head(greet, stats ? `曲库里有 ${fmtNum(stats.tracks)} 首歌、${fmtNum(stats.albums)} 张专辑，累计播放 ${fmtNum(stats.play_count)} 次` : "今天想听点什么？"), quick];
  if (recent.items.length) parts.push(shelf("最近添加", recent.items.map((a) => albumCard(a)), "#/albums?sort=added"));
  else parts.push(empty("曲库还是空的。前往「设置」添加音乐目录并扫描。", "folder", h("a", { class: "btn primary", href: "#/settings" }, "去设置")));
  if (plays.items?.length) {
    const al = await Promise.all(plays.items.map((x) => get(`/albums/${x.id}`).then((r) => r.album).catch(() => null)));
    const ok = al.filter(Boolean); if (ok.length) parts.push(shelf("本月常听", ok.map((a) => albumCard(a))));
  }
  const otdItems = otd ? [...(otd.albums || []), ...(otd.released || [])] : [];
  if (otdItems.length) parts.push(shelf("那年今日", otdItems.slice(0, 12).filter((a) => a.title).map((a) => albumCard(a))));
  if (stats?.top_artists?.length) parts.push(h("section", { class: "section" }, h("div", { class: "section-head" }, h("h2", null, "常听艺人")),
    h("div", { class: "chips" }, stats.top_artists.map((a) => h("a", { class: "chip", href: `#/artist/${a.id}`, style: { display: "inline-flex", alignItems: "center" } }, `${a.name} · ${a.count}`)))));
  parts.push(...(await online));
  main.replaceChildren(...parts);
}

export async function albums(main, params) {
  let sort = params.get("sort") || "title", order = params.get("order") || (sort === "added" ? "desc" : "asc"), q = params.get("q") || "", offset = +params.get("offset") || 0;
  const grid = h("div"), count = h("span");
  const sortSel = h("select", { class: "input", style: { width: "auto" }, "aria-label": "排序", onchange: () => { sort = sortSel.value; order = sort === "added" ? "desc" : "asc"; offset = 0; load(); } },
    [["title", "按名称"], ["artist", "按艺人"], ["year", "按年份"], ["added", "最近添加"]].map(([v, l]) => h("option", { value: v, selected: v === sort }, l)));
  const filter = h("input", { class: "input", type: "search", placeholder: "筛选专辑（支持拼音）", value: q, oninput: debounce(() => { q = filter.value; offset = 0; load(); }) });
  main.replaceChildren(head("专辑", count), h("div", { class: "toolbar" }, filter, sortSel), grid);
  async function load() {
    grid.replaceChildren(skelGrid());
    try {
      const p = await get(`/albums${qs({ sort, order, q, limit: 60, offset })}`);
      count.textContent = `${fmtNum(p.total)} 张专辑`;
      grid.replaceChildren(p.items.length ? h("div", { class: "album-grid" }, p.items.map((a) => albumCard(a))) : empty("没有匹配的专辑"),
        pager(p, (o) => { offset = o; load(); main.scrollIntoView(); }));
    } catch (e) { grid.replaceChildren(empty(e.message)); }
  }
  load();
}

export async function album(main, id) {
  main.replaceChildren(h("div", { class: "skel", style: { height: "260px" } }));
  const [al, tracks] = await Promise.all([get(`/albums/${id}`).then((r) => r.album), albumTracks(id)]);
  const reload = () => album(main, id);
  const fav = h("button", { class: `btn icon ${al.favorite ? "fav-on" : ""}`, "aria-label": "收藏专辑", onclick: async () => {
    try { al.favorite ? await del(`/favorites/album/${id}`) : await put(`/favorites/album/${id}`); al.favorite = !al.favorite; fav.classList.toggle("fav-on", al.favorite); toast(al.favorite ? "已收藏专辑" : "已取消收藏"); } catch (e) { errToast(e); }
  } }, icon("heart"));
  const more = h("button", { class: "btn icon", "aria-label": "更多", onclick: () => menu(more, [
    { label: "加入播放队列", icon: "queue", run: () => player.enqueue(tracks) },
    { label: "下一首播放", icon: "next", run: () => player.playNext(tracks) },
    { label: "添加到歌单…", icon: "list", run: () => addToPlaylist(tracks.map((t) => t.id)) },
    isAdmin() && isLocal(al) ? "-" : null,
    isAdmin() && isLocal(al) ? { label: "编辑专辑 / 封面 / 匹配", icon: "edit", run: () => editAlbum(al, reload) } : null,
    isAdmin() && isLocal(al) ? { label: "批量编辑曲目", icon: "wand", run: () => nav(`#/metadata?album_id=${id}`) } : null,
  ]) }, icon("more"));
  const discs = new Set(tracks.map((t) => t.disc_no || 1));
  const lossless = tracks.filter((t) => t.stream?.lossless).length;
  const list = discs.size > 1
    ? [...discs].sort().map((d) => h("div", { class: "section" }, h("div", { class: "eyebrow", style: { margin: "0 10px 8px" } }, `Disc ${d}`), trackList(tracks.filter((t) => (t.disc_no || 1) === d), { showCover: false, numbered: true, onChange: reload })))
    : trackList(tracks, { showCover: false, numbered: true, onChange: reload });
  main.replaceChildren(
    h("section", { class: "hero" }, h("div", { class: "bloom", style: { backgroundImage: al.has_cover ? `url("${coverURL(id, 300)}")` : "none" } }),
      h("div", { class: "hero-row" }, cover(al.has_cover ? coverURL(id, 600) : "", al.title),
        h("div", { class: "hero-meta" }, h("div", { class: "eyebrow" }, "专辑 ", sourceBadge(al)), h("h1", null, al.title),
          h("div", null, (al.artists?.length ? al.artists : [{ name: al.artist_name }]).map((a, i) => [i ? " / " : "", a.id ? h("a", { href: `#/artist/${a.id}`, style: { fontWeight: 700 } }, a.name) : a.name])),
          h("div", { class: "facts" }, [al.year, al.genre, `${al.track_count} 首`, fmtLong(al.duration_ms), lossless ? `${lossless} 首无损` : ""].filter(Boolean).join(" · ")),
          h("div", { class: "hero-actions" }, h("button", { class: "play-circle", "aria-label": "播放", onclick: () => player.playList(tracks) }, icon("play")),
            h("button", { class: "btn", onclick: () => player.playList(tracks, 0, { shuffle: true }) }, icon("shuffle"), "随机"), fav, more))),
    ),
    h("div", { class: "section" }, list));
}

export async function artists(main, params) {
  let q = params.get("q") || "", offset = 0;
  const grid = h("div"), count = h("span");
  const filter = h("input", { class: "input", type: "search", placeholder: "筛选艺人（支持拼音）", value: q, oninput: debounce(() => { q = filter.value; offset = 0; load(); }) });
  main.replaceChildren(head("艺人", count), h("div", { class: "toolbar" }, filter), grid);
  async function load() {
    try {
      const p = await get(`/artists${qs({ q, limit: 120, offset })}`);
      count.textContent = `${fmtNum(p.total)} 位艺人`;
      grid.replaceChildren(p.items.length ? h("div", { class: "artist-grid" }, p.items.map(artistCard)) : empty("没有匹配的艺人", "artist"), pager(p, (o) => { offset = o; load(); }));
    } catch (e) { grid.replaceChildren(empty(e.message)); }
  }
  load();
}

export async function artist(main, id) {
  const [ar, als, trs] = await Promise.all([get(`/artists/${id}`).then((r) => r.artist), get(`/albums${qs({ artist_id: id, sort: "year", limit: 200 })}`), get(`/tracks${qs({ artist_id: id, sort: "plays", order: "desc", limit: 10 })}`)]);
  const first = als.items.find((a) => a.has_cover);
  const reload = () => artist(main, id);
  main.replaceChildren(
    h("section", { class: "hero" }, h("div", { class: "bloom", style: { backgroundImage: first ? `url("${coverURL(first.id, 300)}")` : "none" } }),
      h("div", { class: "hero-row" }, cover(artistImageURL(ar) || (first ? coverURL(first.id, 400) : ""), ar.name, "round"),
        h("div", { class: "hero-meta" }, h("div", { class: "eyebrow" }, "艺人 ", sourceBadge(ar)), h("h1", null, ar.name),
          h("div", { class: "facts" }, `${ar.album_count} 张专辑 · ${ar.track_count} 首曲目${ar.sort_name && ar.sort_name !== ar.name ? ` · ${ar.sort_name}` : ""}`),
          h("div", { class: "hero-actions" },
            h("button", { class: "play-circle", "aria-label": "播放全部", onclick: async () => { const p = await get(`/tracks${qs({ artist_id: id, sort: "album", limit: 500 })}`); player.playList(p.items); } }, icon("play")),
            h("button", { class: "btn", onclick: async () => { const p = await get(`/tracks${qs({ artist_id: id, limit: 500 })}`); player.playList(p.items, 0, { shuffle: true }); } }, icon("shuffle"), "随机"),
            isAdmin() && isLocal(ar) ? h("button", { class: "btn", onclick: () => editArtist(ar, reload) }, icon("edit"), "编辑") : null,
            isAdmin() && isLocal(ar) ? h("button", { class: "btn", onclick: () => editArtistImage(ar, reload) }, icon("image"), "更换图片") : null)))),
    trs.items.length ? h("section", { class: "section" }, h("div", { class: "section-head" }, h("h2", null, "热门曲目")), trackList(trs.items, { showCover: true, numbered: false })) : null,
    h("section", { class: "section" }, h("div", { class: "section-head" }, h("h2", null, "专辑")), h("div", { class: "album-grid" }, als.items.map((a) => albumCard(a)))));
}

export async function tracks(main, params) {
  let sort = params.get("sort") || "title", order = "asc", q = params.get("q") || "", offset = 0;
  const box = h("div"), count = h("span");
  const sortSel = h("select", { class: "input", style: { width: "auto" }, "aria-label": "排序", onchange: () => { sort = sortSel.value; order = ["added", "plays", "last_played", "rating"].includes(sort) ? "desc" : "asc"; offset = 0; load(); } },
    [["title", "按标题"], ["artist", "按艺人"], ["album", "按专辑"], ["added", "最近添加"], ["plays", "播放次数"], ["last_played", "最近播放"], ["duration", "时长"], ["year", "年份"]].map(([v, l]) => h("option", { value: v, selected: v === sort }, l)));
  const filter = h("input", { class: "input", type: "search", placeholder: "筛选歌曲（支持拼音）", value: q, oninput: debounce(() => { q = filter.value; offset = 0; load(); }) });
  let page;
  main.replaceChildren(head("歌曲", count, h("button", { class: "btn primary", onclick: () => page && player.playList(page.items, 0, { shuffle: true }) }, icon("shuffle"), "随机播放")),
    h("div", { class: "toolbar" }, filter, sortSel), box);
  async function load() {
    try {
      page = await get(`/tracks${qs({ sort, order, q, limit: 100, offset })}`);
      count.textContent = `${fmtNum(page.total)} 首`;
      box.replaceChildren(page.items.length ? trackList(page.items, { showCover: true, numbered: false, onChange: load }) : empty("没有匹配的歌曲", "track"), pager(page, (o) => { offset = o; load(); }));
    } catch (e) { box.replaceChildren(empty(e.message)); }
  }
  load();
}

export async function search(main, params) {
  const q = params.get("q") || "";
  const input = h("input", { class: "input", type: "search", value: q, placeholder: "歌曲、专辑、艺人 · 支持拼音和首字母（如 zjl）", style: { maxWidth: "560px", height: "48px", fontSize: "16px" },
    oninput: debounce(() => history.replaceState(null, "", `#/search?q=${encodeURIComponent(input.value)}`) || run(input.value), 250) });
  const out = h("div");
  main.replaceChildren(head("搜索"), input, out);
  input.focus();
  if (!state.mode) await loadMode();
  async function run(v) {
    if (!v.trim()) return out.replaceChildren(empty("输入关键字开始搜索。支持「zhoujielun」「zjl」这样的拼音检索。", "search"));
    try {
      const withOnline = state.mode?.online_enabled?.length > 0;
      const r = await get(`/search${qs({ q: v, limit: 30, online: withOnline ? 1 : undefined })}`);
      const parts = [];
      if (r.artists?.length) parts.push(h("section", { class: "section" }, h("div", { class: "section-head" }, h("h2", null, "艺人")), h("div", { class: "artist-grid" }, r.artists.slice(0, 12).map(artistCard))));
      if (r.albums?.length) parts.push(h("section", { class: "section" }, h("div", { class: "section-head" }, h("h2", null, "专辑")), h("div", { class: "album-grid" }, r.albums.slice(0, 12).map((a) => albumCard(a)))));
      if (r.tracks?.length) parts.push(h("section", { class: "section" }, h("div", { class: "section-head" }, h("h2", null, "歌曲")), trackList(r.tracks, { showCover: true, numbered: false })));
      if (parts.length && r.online?.length) parts.unshift(h("div", { class: "eyebrow", style: { marginTop: "18px" } }, "本地曲库"));
      parts.push(...searchGroups(r.online));
      out.replaceChildren(...(parts.length ? parts : [empty(`没有找到「${v}」相关内容`, "search")]));
    } catch (e) { out.replaceChildren(empty(e.message)); }
  }
  run(q);
}

// ── playlists ──
const SMART_FIELDS = { title: "标题", artist: "艺人", album: "专辑", albumartist: "专辑艺人", genre: "流派", format: "格式", year: "年份", duration: "时长(秒)", plays: "播放次数", rating: "评分", sample_rate: "采样率", bit_depth: "位深", added: "添加时间", last_played: "最近播放", favorite: "已收藏", lossless: "无损" };
const KIND = { title: "s", artist: "s", album: "s", albumartist: "s", genre: "s", format: "s", year: "n", duration: "n", plays: "n", rating: "n", sample_rate: "n", bit_depth: "n", added: "d", last_played: "d", favorite: "b", lossless: "b" };
const OPS = { s: [["contains", "包含"], ["not_contains", "不包含"], ["is", "等于"], ["is_not", "不等于"], ["starts_with", "开头是"]], n: [["gte", "≥"], ["lte", "≤"], ["eq", "="], ["ne", "≠"], ["gt", ">"], ["lt", "<"]], d: [["in_last", "最近 N 天内"], ["not_in_last", "不在最近 N 天"]], b: [["is", "是"]] };

export function smartEditor(initial, onSave) {
  const rules = h("div", { class: "stack" });
  const name = h("input", { class: "input", value: initial?.name || "", placeholder: "例如：最近添加的无损" });
  const match = h("select", { class: "input", style: { width: "auto" } }, h("option", { value: "all" }, "满足全部条件"), h("option", { value: "any" }, "满足任一条件"));
  const sort = h("select", { class: "input", style: { width: "auto" } }, [["added", "按添加时间"], ["plays", "按播放次数"], ["last_played", "按最近播放"], ["title", "按标题"], ["artist", "按艺人"], ["year", "按年份"], ["random", "随机"]].map(([v, l]) => h("option", { value: v }, l)));
  const order = h("select", { class: "input", style: { width: "auto" } }, h("option", { value: "desc" }, "降序"), h("option", { value: "asc" }, "升序"));
  const limit = h("input", { class: "input", type: "number", value: initial?.rules?.limit || 200, style: { width: "100px" } });
  const r0 = initial?.rules;
  if (r0) { match.value = r0.match || "all"; sort.value = r0.sort || "added"; order.value = r0.order || "desc"; }
  const addRule = (ru = { field: "genre", op: "contains", value: "" }) => {
    const f = h("select", { class: "input", style: { width: "auto" } }, Object.entries(SMART_FIELDS).map(([k, l]) => h("option", { value: k, selected: k === ru.field }, l)));
    const op = h("select", { class: "input", style: { width: "auto" } });
    const val = h("input", { class: "input", value: ru.value ?? "", style: { flex: 1, minWidth: "100px" } });
    const sync = (keep) => { const k = KIND[f.value]; op.replaceChildren(...OPS[k].map(([v, l]) => h("option", { value: v, selected: keep && v === ru.op }, l))); val.type = k === "s" ? "text" : "number"; val.hidden = k === "b"; };
    f.onchange = () => sync(false); sync(true);
    const row = h("div", { class: "row wrap" }, f, op, val, h("button", { class: "btn icon sm ghost", "aria-label": "删除条件", onclick: () => row.remove() }, icon("close")));
    row.read = () => { const k = KIND[f.value]; return { field: f.value, op: op.value, value: k === "b" ? true : k === "s" ? val.value : Number(val.value) }; };
    rules.append(row);
  };
  (r0?.rules?.length ? r0.rules : [{ field: "lossless", op: "is", value: true }]).forEach(addRule);
  const d = dialog({ title: initial ? "编辑智能歌单" : "新建智能歌单", wide: true,
    body: [h("label", { class: "field" }, h("span", null, "名称"), name), h("div", { class: "row wrap" }, match), rules,
      h("button", { class: "btn sm", style: { justifySelf: "start" }, onclick: () => addRule() }, icon("plus"), "添加条件"),
      h("div", { class: "row wrap" }, h("span", { class: "muted" }, "排序"), sort, order, h("span", { class: "muted" }, "最多"), limit, h("span", { class: "muted" }, "首"))],
    foot: [h("button", { class: "btn", onclick: () => d.close() }, "取消"), h("button", { class: "btn primary", onclick: async () => {
      const body = { name: name.value.trim(), rules: { match: match.value, rules: [...rules.children].map((r) => r.read()), sort: sort.value, order: order.value, limit: +limit.value || 200 } };
      if (!body.name) return toast("请输入名称", "err");
      try { await onSave(body); d.close(); } catch (e) { errToast(e); }
    } }, "保存")] });
}

const SCOPES = [["local", "本地歌单"], ["mine", "我的云歌单"], ["recommend", "推荐"], ["discover", "发现"]];
export async function playlists(main, params) {
  const scope = params?.get("scope") || (localHidden() ? "mine" : "local");
  const tabs = h("div", { class: "seg", role: "group", "aria-label": "歌单范围", style: { marginBottom: "18px" } },
    SCOPES.map(([v, l]) => h("button", { "aria-pressed": String(v === scope), onclick: () => nav(`#/playlists?scope=${v}`) }, l)));
  if (scope !== "local") {
    main.replaceChildren(head("歌单", "来自已启用的在线音源"), tabs, skelGrid(6));
    main.replaceChildren(head("歌单", "来自已启用的在线音源"), tabs, ...(await scopedPlaylists(scope)));
    return;
  }
  const p = await get("/playlists");
  const reload = () => playlists(main);
  main.replaceChildren(head("歌单", `${p.items.length} 个歌单`,
    h("button", { class: "btn", onclick: () => smartEditor(null, async (b) => { const r = await post("/playlists", b); nav(`#/playlist/${r.id}`); }) }, icon("sparkle"), "智能歌单"),
    h("button", { class: "btn", onclick: () => { const f = h("input", { type: "file", accept: ".m3u,.m3u8,audio/x-mpegurl", onchange: async () => { const file = f.files[0]; if (!file) return; try { const r = await post(`/playlists/import${qs({ name: file.name.replace(/\.m3u8?$/i, "") })}`, file); toast(`已导入 ${r.matched ?? ""} 首`); reload(); } catch (e) { errToast(e); } } }); f.click(); } }, icon("upload"), "导入 M3U"),
    h("button", { class: "btn primary", onclick: async () => { const name = await promptDlg("新建歌单", "歌单名称"); if (!name) return; try { const r = await post("/playlists", { name }); nav(`#/playlist/${r.id}`); } catch (e) { errToast(e); } } }, icon("plus"), "新建歌单")),
    tabs,
    p.items.length ? h("div", { class: "album-grid" }, p.items.map((pl) => h("a", { class: "album-card", href: `#/playlist/${pl.id}` },
      h("div", { class: "cover", style: { background: pl.smart ? "linear-gradient(135deg, #C8F542, #2d3d00)" : "linear-gradient(135deg, #3a3a48, #15151a)" } },
        h("div", { class: "ph", style: { color: pl.smart ? "#0A0A0C" : "rgb(255 255 255 / .5)" } }, icon(pl.smart ? "sparkle" : "list"))),
      h("div", { class: "t ellipsis" }, pl.name), h("div", { class: "s" }, `${pl.smart ? "智能 · " : ""}${pl.track_count} 首 · ${fmtLong(pl.duration_ms)}`))))
      : empty("还没有歌单。新建一个普通歌单，或用规则创建智能歌单。", "list"));
}

export async function playlist(main, id) {
  const p = await get(`/playlists/${id}${qs({ limit: 500 })}`);
  const tracks = p.tracks?.items || [];
  const reload = () => playlist(main, id);
  const covers = [...new Set(tracks.map((t) => t.album_id).filter(Boolean))].slice(0, 4);
  const mosaic = h("div", { class: "cover", style: { display: "grid", gridTemplateColumns: covers.length >= 4 ? "1fr 1fr" : "1fr" } },
    covers.length ? (covers.length >= 4 ? covers : covers.slice(0, 1)).map((a) => { const i = h("img", { src: coverURL(a, 300), alt: "", class: "loaded", style: { position: "static" } }); return i; }) : h("div", { class: "ph" }, icon(p.smart ? "sparkle" : "list")));
  const more = h("button", { class: "btn icon", "aria-label": "更多", onclick: () => menu(more, [
    { label: "重命名", icon: "edit", run: async () => { const n = await promptDlg("重命名歌单", "名称", p.name); if (n) { try { await patch(`/playlists/${id}`, { name: n }); reload(); } catch (e) { errToast(e); } } } },
    { label: "编辑描述", icon: "lyrics", run: async () => { const n = await promptDlg("歌单描述", "描述", p.comment || ""); if (n !== null) { try { await patch(`/playlists/${id}`, { comment: n }); reload(); } catch (e) { errToast(e); } } } },
    p.smart ? { label: "编辑规则", icon: "sparkle", run: () => smartEditor(p, async (b) => { await patch(`/playlists/${id}`, b); reload(); }) } : null,
    { label: "导出 M3U8", icon: "download", run: async () => { try { const r = await api(`/playlists/${id}/m3u`, { raw: true }); const blob = await r.blob(); const a = h("a", { href: URL.createObjectURL(blob), download: `${p.name}.m3u8` }); a.click(); } catch (e) { errToast(e); } } },
    "-",
    { label: "删除歌单", icon: "trash", danger: true, run: async () => { if (await confirmDlg("删除歌单", `确定删除「${p.name}」？`, "删除", true)) { try { await del(`/playlists/${id}`); nav("#/playlists"); } catch (e) { errToast(e); } } } },
  ]) }, icon("more"));
  main.replaceChildren(
    h("section", { class: "hero" }, h("div", { class: "bloom", style: { backgroundImage: covers[0] ? `url("${coverURL(covers[0], 300)}")` : "linear-gradient(135deg,#C8F542,#223)" } }),
      h("div", { class: "hero-row" }, mosaic, h("div", { class: "hero-meta" }, h("div", { class: "eyebrow" }, p.smart ? "智能歌单" : "歌单"), h("h1", null, p.name),
        p.comment ? h("div", { class: "facts" }, p.comment) : null,
        h("div", { class: "facts" }, `${p.track_count} 首 · ${fmtLong(p.duration_ms)}${p.missing ? ` · ${p.missing} 首缺失` : ""}`),
        h("div", { class: "hero-actions" }, h("button", { class: "play-circle", "aria-label": "播放", onclick: () => player.playList(tracks) }, icon("play")),
          h("button", { class: "btn", onclick: () => player.playList(tracks, 0, { shuffle: true }) }, icon("shuffle"), "随机"), more)))),
    h("div", { class: "section" }, tracks.length ? trackList(tracks, { showCover: true, numbered: false, playlist: p, onChange: reload,
      onRemove: async (t, idx) => { try { await post(`/playlists/${id}/items`, { op: "remove", index: idx }); toast("已移除"); reload(); } catch (e) { errToast(e); } } })
      : empty(p.smart ? "当前没有符合规则的曲目" : "歌单是空的。在曲目菜单中选择「添加到歌单」。", "list")));
}

// 典藏 = only what the user favorited here (albums + songs). Platform "liked"
// lists live in 曲库 under their source.
export async function favorites(main) {
  const [tr, al] = await Promise.all([get(`/tracks${qs({ favorite: true, limit: 500 })}`), get(`/albums${qs({ favorite: true, limit: 200 })}`)]);
  main.replaceChildren(head("典藏", `${tr.total} 首歌曲 · ${al.total} 张专辑`, tr.items.length ? h("button", { class: "btn primary", onclick: () => player.playList(tr.items, 0, { shuffle: true }) }, icon("shuffle"), "随机播放") : null),
    al.items.length ? h("section", { class: "section" }, h("div", { class: "section-head" }, h("h2", null, "专辑")), h("div", { class: "album-grid" }, al.items.map((a) => albumCard(a)))) : null,
    h("section", { class: "section" }, h("div", { class: "section-head" }, h("h2", null, "歌曲")), tr.items.length ? trackList(tr.items) : empty("点击曲目旁的 ♡，把喜欢的歌放进典藏", "heart")));
}

// 曲库 = unified library: local tracks + each platform's liked songs, with a
// source filter (全部/本地/网易云/QQ/酷狗) and an A–Z index. Online-only mode
// shows the platform favorites (never hidden).
const LETTERS = [..."ABCDEFGHIJKLMNOPQRSTUVWXYZ", "#"];
const collator = new Intl.Collator("zh-Hans-CN-u-co-pinyin", { sensitivity: "base", numeric: true });
export async function libraryPage(main, params) {
  let src = params.get("src") || "all", q = "";
  main.replaceChildren(head("曲库", "加载中…"), h("div", { class: "skel", style: { height: "300px" } }));
  const showLocal = !localHidden();
  const [loc, cloud] = await Promise.all([
    showLocal ? get(`/tracks${qs({ sort: "title", limit: 2000 })}`).catch(() => ({ items: [] })) : { items: [] },
    get("/online/favorites").catch(() => ({ items: [] })),
  ]);
  const all = [...loc.items.map((t) => ({ ...t, origin: t.origin || "local" }))];
  const notes = [];
  for (const g of cloud.items || []) {
    if (g.error) { notes.push(g); continue; }
    for (const t of g.playlist?.tracks || []) all.push({ ...normTrack(t), favorite: true });
  }
  const key = (t) => (t.sort_letter && t.sort_letter !== "#" ? t.sort_letter : /^[a-z]/i.test(t.title || "") ? t.title[0].toUpperCase() : "#");
  all.forEach((t) => { t.sort_letter = key(t); });
  all.sort((a, b) => (a.sort_letter === "#") - (b.sort_letter === "#") || a.sort_letter.localeCompare(b.sort_letter) || collator.compare(a.title || "", b.title || ""));
  const sources = [["all", "全部"], ...(showLocal && loc.items.length ? [["local", "本地"]] : []),
    ...[...new Set((cloud.items || []).map((g) => g.origin))].map((o) => [o, originName(o)])];
  const count = h("span"), box = h("div", { class: "lib-list" }), rail = h("nav", { class: "az-rail", "aria-label": "按字母跳转" });
  const filter = h("input", { class: "input", type: "search", placeholder: "筛选（支持拼音）", oninput: debounce(() => { q = filter.value.trim().toLowerCase(); render(); }, 150) });
  const chips = h("div", { class: "seg", role: "tablist", "aria-label": "来源" }, sources.map(([v, l]) =>
    h("button", { role: "tab", class: v === src ? "on" : "", "aria-selected": String(v === src), onclick: (e) => { src = v; [...chips.children].forEach((c) => { c.classList.toggle("on", c === e.currentTarget); c.setAttribute("aria-selected", String(c === e.currentTarget)); }); history.replaceState(null, "", `#/library?src=${v}`); render(); } }, l)));
  const noteEls = notes.filter((g) => src === "all" || g.origin === src).map((g) => h("div", { class: "notice" }, `${originName(g.origin)}：`, g.error === "plugin_login_required" ? "未登录，登录后这里会显示你在该平台收藏的歌曲 · " : `暂时无法获取（${g.error}） · `, h("a", { href: "#/settings" }, "去设置")));
  function render() {
    const list = all.filter((t) => (src === "all" || t.origin === src) && (!q || `${t.title} ${t.artist_name} ${t.album_title}`.toLowerCase().includes(q)));
    count.textContent = `${fmtNum(list.length)} 首`;
    const have = new Set(list.map((t) => t.sort_letter));
    rail.replaceChildren(...LETTERS.map((L) => h("button", { type: "button", disabled: !have.has(L), "aria-label": `跳到 ${L}`, onclick: () => {
      const row = box.querySelector(`.trow[data-letter="${CSS.escape(L)}"]`); if (row) { row.scrollIntoView({ block: "start", behavior: "smooth" }); row.focus({ preventScroll: true }); }
    } }, L)));
    box.replaceChildren(list.length ? trackList(list) : empty(src === "all" ? "曲库还是空的：添加本地音乐目录，或登录在线平台后收藏歌曲。" : "这个来源下没有歌曲", "track"));
  }
  main.replaceChildren(head("曲库", count, h("button", { class: "btn primary", onclick: () => { const l = all.filter((t) => (src === "all" || t.origin === src) && t.playable !== "unavailable"); l.length && player.playList(l, 0, { shuffle: true }); } }, icon("shuffle"), "随机播放")),
    h("div", { class: "toolbar" }, chips, filter), ...noteEls, h("div", { class: "lib-wrap" }, box, rail));
  render();
}

export async function historyPage(main) {
  const [hist, top] = await Promise.all([get(`/history${qs({ limit: 100 })}`), get(`/history/top${qs({ type: "track", period: "30d", limit: 10 })}`).catch(() => ({ items: [] }))]);
  const rows = hist.items.map((x) => h("div", { class: "li" }, cover(x.album_id ? coverURL(x.album_id, 80) : "", x.title, ""),
    h("div", { class: "grow" }, h("div", { class: "t ellipsis" }, x.title || "未知曲目"), h("div", { class: "s ellipsis" }, [x.artist_name, x.album_title].filter(Boolean).join(" — "))),
    h("span", { class: "dim", style: { fontSize: "12px" } }, fmtAgo(x.played_at)),
    x.available ? h("button", { class: "btn icon sm ghost", "aria-label": "播放", onclick: async () => { try { player.playList([x.track_id.includes(":") ? { id: x.track_id, origin: x.track_id.split(":")[0], title: x.title, artist_name: x.artist_name, album_id: x.album_id, album_title: x.album_title } : await get(`/tracks/${x.track_id}`)]); } catch (e) { errToast(e); } } }, icon("play")) : h("span", { class: "badge" }, "已移除")));
  rows.forEach((r) => { const c = r.querySelector(".cover"); c.style.width = "44px"; c.style.borderRadius = "8px"; });
  main.replaceChildren(head("播放历史", `共 ${fmtNum(hist.total)} 次播放`),
    top.items?.length ? h("section", { class: "section", style: { marginTop: 0 } }, h("div", { class: "section-head" }, h("h2", null, "本月最常播放")),
      h("div", { class: "chips" }, top.items.map((t, i) => h("span", { class: "chip", style: { display: "inline-flex", alignItems: "center" } }, `${i + 1}. ${t.name} · ${t.plays} 次`)))) : null,
    h("section", { class: "section" }, rows.length ? h("div", null, rows) : empty("还没有播放记录", "history")));
}
