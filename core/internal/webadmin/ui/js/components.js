// Reusable library widgets: album/artist cards, track lists with actions, editors.
import { get, post, put, del, qs, coverURL, downloadURL, bustCover, artistImageURL } from "./api.js";
import { h, icon, cover, fmtDur, menu, toast, errToast, dialog, conf, confirmDlg, promptDlg, isLocal, sourceBadge, NOT_LOCAL_MSG, providerLabel } from "./ui.js";
import { player, qualityBadge } from "./player.js";
import { isAdmin, nav } from "./state.js";

// Online ids are "<origin>:<native>"; their pages live under #/online/….
const ext = (id) => typeof id === "string" && id.includes(":");
const extHref = (kind, id) => { const i = id.indexOf(":"); return `#/online/${id.slice(0, i)}/${kind}/${encodeURIComponent(id.slice(i + 1))}`; };
export const albumHref = (id) => ext(id) ? extHref("album", id) : `#/album/${id}`;
export const artistHref = (id) => ext(id) ? extHref("artist", id) : `#/artist/${id}`;
const netImg = (u, s) => /music\.126\.net/.test(u) ? `${u.replace(/^http:/, "https:")}?param=${s}y${s}` : u;
export const trackArt = (t, size = 80) => t.cover_url ? netImg(t.cover_url, size) : t.album_id ? coverURL(t.album_id, size) : "";
// 试听 = small lime outlined pill (designer spec); unplayable rows are dimmed
// instead of badged, and explain themselves on tap.
export function playBadge(t) {
  if ((t.preview || t.entitlement === "preview")) return h("span", { class: "pill-pv", title: "当前账号仅能播放平台提供的试听片段" }, "试听");
  return null;
}
export const UNAVAIL_REASON = { vip: "需要平台会员", paid: "需要单独购买", region: "版权地区限制", offline: "已下架", login: "请先在设置中登录该平台" };
export function unavailableReason(t) {
  return UNAVAIL_REASON[t.unavailable_reason] || t.unavailable_message || "当前账号无权播放这首歌（付费、下架或地区限制）";
}
// Source = small grey text after the artist line (网易/QQ/酷狗), no brand colors.
export const SOURCE_SHORT = { netease: "网易", qq: "QQ", kugou: "酷狗", spotify: "Spotify" };
export function sourceText(t) {
  return isLocal(t) ? null : h("span", { class: "src-text" }, SOURCE_SHORT[t.origin] || t.origin || "");
}

export function albumCard(al, { onPlay } = {}) {
  const play = h("button", { class: "play-circle hover-play", "aria-label": `播放 ${al.title}`, onclick: async (e) => {
    e.preventDefault(); e.stopPropagation();
    try { const tr = await albumTracks(al.id); onPlay ? onPlay(tr) : player.playList(tr); } catch (err) { errToast(err); }
  } }, icon("play"));
  return h("a", { class: "album-card", href: `#/album/${al.id}` },
    h("div", { class: "art" }, cover(al.has_cover ? coverURL(al.id, 300) : "", al.title), play),
    h("div", { class: "t ellipsis", title: al.title }, al.title, " ", sourceBadge(al)),
    h("div", { class: "s ellipsis" }, [al.artist_name, al.year].filter(Boolean).join(" · ")));
}
export function artistCard(ar) {
  return h("a", { href: `#/artist/${ar.id}` },
    cover(artistImageURL(ar) || "", ar.name, "round"),
    h("div", { class: "t ellipsis" }, ar.name),
    h("div", { class: "s" }, `${ar.album_count} 张专辑 · ${ar.track_count} 首`));
}
export async function albumTracks(albumId) {
  const p = await get(`/tracks${qs({ album_id: albumId, sort: "album", limit: 500 })}`);
  return p.items;
}

// Track list. opts: {showCover, numbered (album order: show track_no), onChange, playlist, letters}
// Rows start with a small cover and carry no index numbers unless numbered
// (album pages). Unplayable rows are 40% opacity and toast their reason.
export function trackList(tracks, opts = {}) {
  const { showCover = true, numbered = false } = opts;
  const idx = numbered;
  const cls = `${showCover ? "with-cover" : ""} ${idx ? "" : "no-index"}`;
  const wrap = h("div", { class: "tracks", role: "list" });
  wrap.append(h("div", { class: `trow head ${cls}` },
    idx ? h("span", { class: "no" }, "#") : null, showCover ? h("span", { class: "opt" }) : null, h("span", null, "标题"), h("span", { class: "al" }, "专辑"), h("span", { class: "du" }, "时长"), h("span")));
  const blocked = (t) => (t.playable === false || t.entitlement === "unavailable");
  const play = (i) => { const t = tracks[i]; if (blocked(t)) return toast(unavailableReason(t), "err"); player.playList(tracks.filter((x) => !blocked(x)), tracks.filter((x) => !blocked(x)).indexOf(t)); };
  const rows = tracks.map((t, i) => {
    const no = idx ? h("span", { class: "no" }, t.track_no || i + 1) : null;
    const fav = h("button", { class: `btn icon sm ghost ${t.favorite ? "fav-on" : ""}`, "aria-label": "收藏", title: "收藏", onclick: async (e) => {
      e.stopPropagation();
      try { t.favorite ? await del(`/favorites/track/${t.id}`) : await put(`/favorites/track/${t.id}`); t.favorite = !t.favorite; fav.classList.toggle("fav-on", t.favorite); toast(t.favorite ? "已收藏" : "已取消收藏"); } catch (err) { errToast(err); }
    } }, icon("heart"));
    const more = h("button", { class: "btn icon sm ghost", "aria-label": "更多操作", onclick: (e) => { e.stopPropagation(); trackMenu(more, t, tracks, opts); } }, icon("more"));
    const art = showCover ? h("div", { class: "cov-wrap opt" }, cover(trackArt(t, 80), t.album_title || t.title, "")) : null;
    const row = h("div", { class: `trow ${cls} ${blocked(t) ? "unavail" : ""}`, role: "listitem", tabindex: 0, "data-id": t.id,
      "data-letter": t.sort_letter || "", "aria-disabled": blocked(t) ? "true" : undefined, title: blocked(t) ? unavailableReason(t) : undefined,
      ondblclick: () => play(i), onkeydown: (e) => { if (e.key === "Enter") play(i); },
      oncontextmenu: (e) => { e.preventDefault(); trackMenu(more, t, tracks, opts); } },
      no, art,
      h("div", { style: { minWidth: 0 } },
        h("div", { class: "ti ellipsis" }, t.title, " ", t.stream?.lossless ? h("span", { class: "badge hq" }, "无损") : null, playBadge(t),
          t.local_id ? h("span", { class: "badge ok", title: "本地曲库已有此曲，播放时优先使用本地文件" }, "本地") : null),
        h("div", { class: "ar ellipsis" }, t.artist_name || "未知艺人", sourceText(t))),
      h("a", { class: "al ellipsis", href: t.album_id ? albumHref(t.album_id) : undefined, onclick: (e) => e.stopPropagation() }, t.album_title || ""),
      h("span", { class: "du" }, fmtDur(t.duration_ms)),
      h("div", { class: "acts" }, h("button", { class: "btn icon sm ghost", "aria-label": "播放", onclick: (e) => { e.stopPropagation(); play(i); } }, icon("play")), fav, more));
    row.addEventListener("click", (e) => {
      if (e.target.closest("button,a")) return;
      if (blocked(t)) return toast(unavailableReason(t), "err");
      if (matchMedia("(max-width: 700px)").matches) play(i);
    });
    return row;
  });
  wrap.append(...rows);
  const eq = () => h("span", { class: `eq ${player.playing ? "" : "paused"}` }, h("i"), h("i"), h("i"));
  const mark = () => {
    const cur = player.track?.id;
    rows.forEach((r, i) => {
      const on = tracks[i].id === cur;
      r.classList.toggle("playing", on);
      const slot = idx ? r.querySelector(".no") : r.querySelector(".cov-wrap");
      if (!slot) return;
      const e = slot.querySelector(".eq");
      if (on && !e) { if (idx) slot.replaceChildren(eq()); else slot.append(eq()); }
      else if (on) e.classList.toggle("paused", !player.playing);
      else if (e) { if (idx) slot.textContent = tracks[i].track_no || i + 1; else e.remove(); }
    });
  };
  const off = player.on((k) => { if (k !== "time") { if (!wrap.isConnected) return off(); mark(); } });
  mark();
  return wrap;
}

export function trackMenu(anchor, t, list, opts = {}) {
  const admin = isAdmin();
  menu(anchor, [
    { label: "立即播放", icon: "play", run: () => player.playList([t]) },
    { label: "下一首播放", icon: "next", run: () => player.playNext(t) },
    { label: "加入播放队列", icon: "queue", run: () => player.enqueue(t) },
    isLocal(t) ? { label: "从这首开始电台", icon: "sparkle", run: () => radio(t) } : null,
    { label: "添加到歌单…", icon: "list", run: () => addToPlaylist([t.id]) },
    "-",
    t.album_id ? { label: "前往专辑", icon: "album", run: () => nav(albumHref(t.album_id)) } : null,
    ...(t.artists || []).filter((a) => a.id).slice(0, 2).map((a) => ({ label: `前往艺人 · ${a.name}`, icon: "artist", run: () => nav(artistHref(a.id)) })),
    admin && isLocal(t) ? "-" : null,
    admin && isLocal(t) ? { label: "编辑标签 / 在线匹配", icon: "edit", run: () => editTrack(t, opts.onChange) } : null,
    admin && isLocal(t) ? { label: "歌词匹配与校对", icon: "lyrics", run: () => nav(`#/lyrics/${t.id}`) } : null,
    isLocal(t) ? { label: "下载原始文件", icon: "download", run: () => { location.href = downloadURL(t.id); } } : null,
    opts.playlist && !opts.playlist.smart ? "-" : null,
    opts.playlist && !opts.playlist.smart ? { label: "从歌单移除", icon: "trash", danger: true, run: () => opts.onRemove?.(t, list.indexOf(t)) } : null,
  ]);
}

async function radio(t) {
  try {
    const p = await get(`/tracks/${t.id}`);
    const sim = await get(`/tracks${qs({ artist_id: p.artists?.[0]?.id, limit: 100 })}`).catch(() => ({ items: [] }));
    const rest = (sim.items || []).filter((x) => x.id !== t.id).sort(() => Math.random() - 0.5);
    player.playList([p, ...rest]);
    toast("已开启电台");
  } catch (e) { errToast(e); }
}

export async function addToPlaylist(trackIds) {
  let pls;
  try { pls = (await get("/playlists")).items.filter((p) => !p.smart); } catch (e) { return errToast(e); }
  const list = h("div", { class: "stack" });
  const d = dialog({ title: "添加到歌单", body: list,
    foot: [h("button", { class: "btn", onclick: async () => {
      const name = await promptDlg("新建歌单", "歌单名称"); if (!name) return;
      try { await post("/playlists", { name, track_ids: trackIds }); toast(`已创建「${name}」并添加`); d.close(); } catch (e) { errToast(e); }
    } }, icon("plus"), "新建歌单")] });
  if (!pls.length) list.append(h("p", { class: "muted" }, "还没有普通歌单，点击下方新建。"));
  for (const p of pls) list.append(h("button", { class: "li", style: { border: 0, cursor: "pointer", textAlign: "left", color: "inherit" }, onclick: async () => {
    try { await post(`/playlists/${p.id}/items`, { op: "append", track_ids: trackIds }); toast(`已添加到「${p.name}」`); d.close(); } catch (e) { errToast(e); }
  } }, icon("list"), h("div", { class: "grow" }, h("div", { class: "t" }, p.name), h("div", { class: "s" }, `${p.track_count} 首`))));
}

// ── metadata editors ──
const TRACK_FIELDS = [["title", "标题"], ["artist_name", "艺人"], ["genre", "流派"], ["year", "年份", "number"], ["track_no", "音轨号", "number"], ["disc_no", "碟号", "number"]];
const ALBUM_FIELDS = [["title", "专辑名"], ["artist_name", "专辑艺人"], ["genre", "流派"], ["year", "年份", "number"]];
const ARTIST_FIELDS = [["name", "名称"], ["sort_name", "排序名（拼音/英文）"]];

function fieldsForm(fields, obj) {
  const inputs = {};
  const grid = h("div", { class: "field-grid" }, fields.map(([k, label, type]) => {
    inputs[k] = h("input", { class: "input", type: type || "text", value: obj[k] ?? "", "data-orig": obj[k] ?? "" });
    return h("label", { class: "field" }, h("span", null, label), inputs[k]);
  }));
  const changed = () => {
    const p = {};
    for (const [k, , type] of fields) {
      const v = inputs[k].value.trim();
      if (v === String(inputs[k].dataset.orig)) continue;
      p[k] = type === "number" ? (v === "" ? 0 : Number(v)) : v;
    }
    return p;
  };
  return { grid, inputs, changed };
}
function tabsBar(names, onSel) {
  const t = h("div", { class: "tabs", role: "tablist" });
  names.forEach(([k, l], i) => t.append(h("button", { role: "tab", "aria-selected": i === 0 ? "true" : "false", onclick: (e) => { [...t.children].forEach((b) => b.setAttribute("aria-selected", b === e.currentTarget)); onSel(k); } }, l)));
  return t;
}
async function saveOverride(type, id, patch) {
  if (!Object.keys(patch).length) { toast("没有改动"); return null; }
  const r = await put(`/overrides/${type}/${id}`, patch);
  toast("已保存到 Core 数据库（重扫后保留）");
  return r;
}
async function resetOverride(type, id) {
  if (!(await confirmDlg("还原标签", "清除此条目的手动修改？下次重扫后恢复文件标签值。", "还原", true))) return false;
  try { await del(`/overrides/${type}/${id}`); toast("已还原，将在下次扫描后生效"); return true; } catch (e) { if (e.status === 404) toast("没有手动修改"); else errToast(e); return false; }
}

function mbPane(kind, obj, form, fields) {
  const q1 = h("input", { class: "input", value: obj.title || "", placeholder: "标题" });
  const q2 = h("input", { class: "input", value: obj.artist_name || "", placeholder: "艺人" });
  const res = h("div", { class: "stack" }, h("p", { class: "muted", style: { margin: 0 } }, "从 MusicBrainz、Spotify（配置 Client 凭据后）、网易云、QQ音乐、酷狗 查找候选（匿名公开接口，无需登录），按置信度排序；选中后对比差异，勾选要应用的字段写入本地覆盖（不修改音频文件）。"));
  const run = async () => {
    res.replaceChildren(h("div", { class: "skel", style: { height: "70px" } }), h("div", { class: "skel", style: { height: "70px" } }));
    try {
      const r = await get(`/${kind}s/${obj.id}/match${qs({ title: q1.value, artist: q2.value })}`);
      if (!r.items.length) return res.replaceChildren(h("p", { class: "muted" }, "没有找到候选。试试修改标题或艺人再搜。"));
      res.replaceChildren(...r.items.sort((a, b) => b.score - a.score).map((c) => {
        const b = h("button", { class: "cand", onclick: () => { [...res.children].forEach((x) => x.setAttribute("aria-selected", x === b)); showDiff(c); } },
          h("div", { class: "row" }, c.cover_url ? h("img", { src: c.cover_url, alt: "", loading: "lazy", referrerpolicy: "no-referrer", style: { width: "36px", height: "36px", borderRadius: "4px", objectFit: "cover" } }) : null,
            h("b", { class: "ellipsis" }, c.title), h("span", { class: "spacer" }), h("span", { class: `badge src-${c.provider || ""}` }, providerLabel(c.provider)), conf(c.score)),
          h("div", { class: "muted", style: { fontSize: "13px" } }, [c.artist, c.album, c.release_date || c.year, c.label, c.duration_ms ? fmtDur(c.duration_ms) : "", c.track_count ? `${c.track_count} 首` : ""].filter(Boolean).join(" · ")),
          h("div", { class: "dim mono" }, [c.isrc && `ISRC ${c.isrc}`, c.upc && `UPC ${c.upc}`, c.popularity ? `热度 ${c.popularity}` : "", (c.genres || []).slice(0, 3).join(" / "), c.mb_recording_id || c.mb_release_id || (c.provider === "musicbrainz" ? c.external_id : "")].filter(Boolean).join(" · ")));
        return b;
      }));
    } catch (e) { res.replaceChildren(h("p", { class: "muted" }, e.status === 503 ? "元数据来源未配置。" : `查询失败：${e.message}`)); }
  };
  const diff = h("div");
  const map = kind === "track" ? { title: "title", artist_name: "artist", genre: "genre", year: "year", track_no: "track_no", disc_no: "disc_no" }
    : { title: "title", artist_name: "album_artist", genre: "genre", year: "year" };
  function showDiff(c) {
    const rows = [];
    for (const [k, label] of fields) {
      const nv = c[map[k]]; if (nv === undefined || nv === "" || nv === 0) continue;
      const ov = form.inputs[k].value;
      const same = String(ov) === String(nv);
      const cb = h("input", { type: "checkbox", class: "chk", checked: !same, disabled: same });
      cb.dataset.k = k; cb.dataset.v = nv;
      rows.push(h("label", { class: `diff ${same ? "same" : ""}` }, cb, h("span", { class: "muted" }, label), h("span", { class: "old ellipsis" }, ov || "—"), "→", h("span", { class: "new ellipsis" }, nv)));
    }
    const coverBtn = kind === "album" && c.cover_url ? h("div", { class: "row", style: { gap: "10px", margin: "8px 0" } },
      h("img", { src: c.cover_url, alt: "", referrerpolicy: "no-referrer", style: { width: "64px", height: "64px", borderRadius: "6px", objectFit: "cover" } }),
      h("button", { class: "btn sm", onclick: async () => { try { await post(`/albums/${obj.id}/cover/from-match`, { provider: c.provider, url: c.cover_url }); bustCover(obj.id); toast("封面已替换（本地覆盖，可在「封面」页恢复）"); } catch (e) { errToast(e); } } }, icon("album"), "使用此封面")) : null;
    diff.replaceChildren(h("h3", { style: { margin: "6px 0" } }, "应用到表单"), ...rows, coverBtn,
      h("div", { class: "row", style: { marginTop: "10px" } }, h("span", { class: "spacer" }),
        h("button", { class: "btn accent sm", onclick: () => {
          let n = 0;
          diff.querySelectorAll("input:checked").forEach((cb) => { form.inputs[cb.dataset.k].value = cb.dataset.v; n++; });
          toast(n ? `已填入 ${n} 个字段，记得点「保存」` : "未选择字段"); form.onApplied?.();
        } }, icon("check"), "填入所选字段")));
  }
  return h("div", { class: "stack" }, h("div", { class: "row" }, q1, q2, h("button", { class: "btn primary", onclick: run }, icon("search"), "搜索")), h("div", { class: "two-col" }, res, diff));
}

export async function editTrack(t, onChange) {
  try { t = await get(`/tracks/${t.id}`); } catch (e) { return errToast(e); }
  if (!isLocal(t)) return toast(NOT_LOCAL_MSG, "err");
  const form = fieldsForm(TRACK_FIELDS, t);
  const info = h("dl", { class: "kv" },
    h("dt", null, "ID"), h("dd", { class: "mono" }, t.id),
    h("dt", null, "格式"), h("dd", null, [t.format?.toUpperCase(), t.stream?.sample_rate && `${t.stream.sample_rate / 1000} kHz`, t.stream?.bit_depth && `${t.stream.bit_depth} bit`, t.stream?.bitrate_kbps && `${t.stream.bitrate_kbps} kbps`].filter(Boolean).join(" · ")),
    h("dt", null, "专辑"), h("dd", null, t.album_title || "—"),
    t.stream?.replaygain ? [h("dt", null, "ReplayGain"), h("dd", null, `${t.stream.replaygain.track_gain} dB（${t.stream.replaygain.source}）`)] : null);
  const editPane = h("div", { class: "stack" }, form.grid, info);
  const panes = { edit: editPane, mb: mbPane("track", t, form, TRACK_FIELDS) };
  const body = h("div", null, editPane);
  form.onApplied = () => tabs.children[0].click();
  const tabs = tabsBar([["edit", "标签"], ["mb", "在线匹配"]], (k) => body.replaceChildren(panes[k]));
  const d = dialog({ title: `编辑曲目 · ${t.title}`, wide: true, tabs, body,
    foot: [h("button", { class: "btn danger", onclick: async () => { if (await resetOverride("track", t.id)) { d.close(); onChange?.(); } } }, "还原为文件标签"), h("span", { class: "spacer" }),
      h("button", { class: "btn", onclick: () => d.close() }, "取消"),
      h("button", { class: "btn primary", onclick: async () => { try { if (await saveOverride("track", t.id, form.changed())) { d.close(); onChange?.(); } } catch (e) { errToast(e); } } }, "保存")] });
}

export async function editAlbum(al, onChange) {
  try { al = (await get(`/albums/${al.id}`)).album; } catch (e) { return errToast(e); }
  if (!isLocal(al)) return toast(NOT_LOCAL_MSG, "err");
  const form = fieldsForm(ALBUM_FIELDS, al);
  const preview = h("div", { style: { width: "180px" } }, cover(al.has_cover ? coverURL(al.id, 400) : "", al.title));
  const file = h("input", { type: "file", accept: "image/jpeg,image/png,image/webp", hidden: true, onchange: async () => {
    const f = file.files[0]; if (!f) return;
    if (f.size > 10 << 20) return toast("图片不能超过 10 MB", "err");
    try { await put(`/albums/${al.id}/cover`, f, { headers: { "Content-Type": f.type } }); bustCover(al.id); preview.replaceChildren(cover(coverURL(al.id, 400), al.title)); toast("封面已替换"); onChange?.(); } catch (e) { errToast(e); }
  } });
  const coverPane = h("div", { class: "row wrap", style: { alignItems: "flex-start", gap: "20px" } }, preview,
    h("div", { class: "stack", style: { flex: 1, minWidth: "220px" } },
      h("p", { class: "muted", style: { margin: 0 } }, "上传 JPEG / PNG / WebP（≤10 MB）。替换后的封面保存在 Core 缓存目录，重扫不会覆盖；原始音频文件不会被修改。"),
      h("div", { class: "row wrap" }, h("button", { class: "btn primary", onclick: () => file.click() }, icon("upload"), "选择图片"),
        h("button", { class: "btn danger", onclick: async () => { try { await del(`/albums/${al.id}/cover`); bustCover(al.id); toast("已移除自定义封面，重扫后恢复原封面"); onChange?.(); } catch (e) { e.status === 404 ? toast("没有自定义封面") : errToast(e); } } }, "恢复原封面")),
      file));
  coverPane.addEventListener("dragover", (e) => e.preventDefault());
  coverPane.addEventListener("drop", (e) => { e.preventDefault(); if (e.dataTransfer.files[0]) { file.files = e.dataTransfer.files; file.dispatchEvent(new Event("change")); } });
  const editPane = h("div", { class: "stack" }, form.grid, h("p", { class: "dim", style: { margin: 0, fontSize: "13px" } }, "专辑级修改只影响专辑条目；如需批量修改曲目流派/年份，请在「元数据」页多选曲目批量编辑。"));
  const panes = { edit: editPane, cover: coverPane, mb: mbPane("album", al, form, ALBUM_FIELDS) };
  const body = h("div", null, editPane);
  form.onApplied = () => tabs.children[0].click();
  const tabs = tabsBar([["edit", "信息"], ["cover", "封面"], ["mb", "在线匹配"]], (k) => body.replaceChildren(panes[k]));
  const d = dialog({ title: `编辑专辑 · ${al.title}`, wide: true, tabs, body,
    foot: [h("button", { class: "btn danger", onclick: async () => { if (await resetOverride("album", al.id)) { d.close(); onChange?.(); } } }, "还原"), h("span", { class: "spacer" }),
      h("button", { class: "btn", onclick: () => d.close() }, "取消"),
      h("button", { class: "btn primary", onclick: async () => { try { if (await saveOverride("album", al.id, form.changed())) { d.close(); onChange?.(); } } catch (e) { errToast(e); } } }, "保存")] });
}

// Artist image editor (admin, local artists): upload / drag-drop, pick a
// candidate from metadata sources, or remove. Stored by Core; survives rescans.
export async function editArtistImage(ar, onChange) {
  if (!isLocal(ar)) return toast(NOT_LOCAL_MSG, "err");
  const preview = h("div", { class: "ai-preview" });
  const show = (a) => preview.replaceChildren(cover(artistImageURL(a) || "", a.name, "round"));
  show(ar);
  const done = (a) => { ar = a || ar; show(ar); onChange?.(); };
  const upload = async (f) => {
    if (!f) return;
    if (!/^image\/(jpeg|png|webp)$/.test(f.type)) return toast("请选择 JPEG / PNG / WebP 图片", "err");
    if (f.size > 10 << 20) return toast("图片不能超过 10 MB", "err");
    try { const r = await put(`/artists/${ar.id}/image`, f, { headers: { "Content-Type": f.type } }); toast("艺人图片已更新"); done(r.artist || r); } catch (e) { errToast(e); }
  };
  const file = h("input", { type: "file", accept: "image/jpeg,image/png,image/webp", hidden: true, onchange: () => upload(file.files[0]) });
  const drop = h("div", { class: "ai-drop", tabindex: 0, role: "button", "aria-label": "选择或拖入图片", onclick: () => file.click(), onkeydown: (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); file.click(); } } },
    icon("upload"), h("b", null, "拖入图片或点击选择"), h("span", null, "JPEG / PNG / WebP，≤10 MB"));
  drop.addEventListener("dragover", (e) => { e.preventDefault(); drop.classList.add("over"); });
  drop.addEventListener("dragleave", () => drop.classList.remove("over"));
  drop.addEventListener("drop", (e) => { e.preventDefault(); drop.classList.remove("over"); upload(e.dataTransfer.files[0]); });
  const cands = h("div", { class: "ai-cands" }, h("p", { class: "muted" }, "正在从元数据来源查找…"));
  const remove = h("button", { class: "btn danger", onclick: async () => { try { await del(`/artists/${ar.id}/image`); toast("已移除艺人图片"); done({ ...ar, image_updated_at: undefined }); } catch (e) { e.status === 404 ? toast("没有设置图片") : errToast(e); } } }, "移除图片");
  const d = dialog({ title: `艺人图片 · ${ar.name}`, wide: true,
    body: h("div", { class: "ai-grid" }, h("div", { class: "stack" }, preview, drop, file), h("div", { class: "stack" }, h("h3", null, "在线候选"), cands)),
    foot: [remove, h("span", { class: "spacer" }), h("button", { class: "btn", onclick: () => d.close() }, "完成")] });
  try {
    const r = await get(`/artists/${ar.id}/image/candidates`);
    cands.replaceChildren(...(r.items.length ? r.items.map((c) => h("button", { class: "ai-cand", title: `${providerLabel(c.provider)} · ${c.name}`, onclick: async (e) => {
      e.currentTarget.disabled = true;
      try { const a = await post(`/artists/${ar.id}/image/from-match`, { provider: c.provider, url: c.image_url }); toast(`已使用 ${providerLabel(c.provider)} 的图片`); done(a.artist || a); } catch (err) { errToast(err); } finally { e.currentTarget.disabled = false; }
    } }, h("img", { src: c.image_url, alt: "", loading: "lazy", referrerpolicy: "no-referrer" }), h("span", { class: "n ellipsis" }, c.name), h("span", { class: "src-text" }, providerLabel(c.provider))))
      : [h("p", { class: "muted" }, "没有找到候选图片，可以手动上传。")]));
  } catch (e) { cands.replaceChildren(h("p", { class: "muted" }, e.status === 503 ? "没有可用的元数据来源（可在设置中启用）。" : `查找失败：${e.message}`)); }
}

export function editArtist(ar, onChange) {
  if (!isLocal(ar)) return toast(NOT_LOCAL_MSG, "err");
  const form = fieldsForm(ARTIST_FIELDS, ar);
  const d = dialog({ title: `编辑艺人 · ${ar.name}`, body: form.grid,
    foot: [h("button", { class: "btn danger", onclick: async () => { if (await resetOverride("artist", ar.id)) { d.close(); onChange?.(); } } }, "还原"), h("span", { class: "spacer" }),
      h("button", { class: "btn", onclick: () => d.close() }, "取消"),
      h("button", { class: "btn primary", onclick: async () => { try { if (await saveOverride("artist", ar.id, form.changed())) { d.close(); onChange?.(); } } catch (e) { errToast(e); } } }, "保存")] });
}

export function batchEdit(ids, onDone) {
  const fields = [["artist_name", "艺人"], ["genre", "流派"], ["year", "年份", "number"], ["disc_no", "碟号", "number"]];
  const rows = fields.map(([k, label, type]) => {
    const on = h("input", { type: "checkbox", class: "chk" });
    const inp = h("input", { class: "input", type: type || "text", placeholder: "留空表示清空", oninput: () => { on.checked = true; } });
    return { k, type, on, inp, el: h("label", { class: "row" }, on, h("span", { style: { width: "64px" } }, label), inp) };
  });
  const d = dialog({ title: `批量编辑 · ${ids.length} 首曲目`, body: [h("p", { class: "muted", style: { margin: 0 } }, "勾选要统一修改的字段。修改保存在 Core 数据库（不改动音频文件），重扫后依然有效。"), ...rows.map((r) => r.el)],
    foot: [h("button", { class: "btn", onclick: () => d.close() }, "取消"), h("button", { class: "btn primary", onclick: async () => {
      const patch = {};
      for (const r of rows) if (r.on.checked) patch[r.k] = r.type === "number" ? Number(r.inp.value || 0) : r.inp.value.trim();
      if (!Object.keys(patch).length) return toast("请至少勾选一个字段", "err");
      try { const res = await post("/overrides/batch", { type: "track", ids, patch }); toast(`已更新 ${res.updated} 首${res.skipped_not_local?.length ? `，跳过 ${res.skipped_not_local.length} 首在线来源曲目` : ""}`); d.close(); onDone?.(); } catch (e) { errToast(e); }
    } }, "应用")] });
}

export function pager(page, onGo) {
  const pages = Math.ceil(page.total / page.limit) || 1, cur = Math.floor(page.offset / page.limit) + 1;
  if (pages <= 1) return h("div");
  return h("div", { class: "pager" },
    h("button", { class: "btn sm", disabled: cur <= 1, onclick: () => onGo((cur - 2) * page.limit) }, "上一页"),
    h("span", { class: "muted" }, `${cur} / ${pages}`),
    h("button", { class: "btn sm", disabled: cur >= pages, onclick: () => onGo(cur * page.limit) }, "下一页"));
}
