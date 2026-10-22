// Reusable library widgets: album/artist cards, track lists with actions, editors.
import { get, post, put, del, qs, coverURL, downloadURL, bustCover } from "./api.js";
import { h, icon, cover, fmtDur, menu, toast, errToast, dialog, conf, confirmDlg, promptDlg, isLocal, sourceBadge, NOT_LOCAL_MSG } from "./ui.js";
import { player, qualityBadge } from "./player.js";
import { isAdmin, nav } from "./state.js";

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
    cover("", ar.name, "round"),
    h("div", { class: "t ellipsis" }, ar.name),
    h("div", { class: "s" }, `${ar.album_count} 张专辑 · ${ar.track_count} 首`));
}
export async function albumTracks(albumId) {
  const p = await get(`/tracks${qs({ album_id: albumId, sort: "album", limit: 500 })}`);
  return p.items;
}

// Track list. opts: {showCover, showAlbum, numbered, onChange, playlist}
export function trackList(tracks, opts = {}) {
  const { showCover = false, numbered = true } = opts;
  const wrap = h("div", { class: "tracks", role: "list" });
  wrap.append(h("div", { class: `trow head ${showCover ? "with-cover" : ""}` },
    h("span", { class: "no" }, "#"), showCover ? h("span", { class: "opt" }) : null, h("span", null, "标题"), h("span", { class: "al" }, "专辑"), h("span", { class: "du" }, "时长"), h("span")));
  const rows = tracks.map((t, i) => {
    const no = h("span", { class: "no" }, numbered ? (t.track_no || i + 1) : i + 1);
    const fav = h("button", { class: `btn icon sm ghost ${t.favorite ? "fav-on" : ""}`, "aria-label": "收藏", title: "收藏", onclick: async (e) => {
      e.stopPropagation();
      try { t.favorite ? await del(`/favorites/track/${t.id}`) : await put(`/favorites/track/${t.id}`); t.favorite = !t.favorite; fav.classList.toggle("fav-on", t.favorite); toast(t.favorite ? "已收藏" : "已取消收藏"); } catch (err) { errToast(err); }
    } }, icon("heart"));
    const more = h("button", { class: "btn icon sm ghost", "aria-label": "更多操作", onclick: (e) => { e.stopPropagation(); trackMenu(more, t, tracks, opts); } }, icon("more"));
    const row = h("div", { class: `trow ${showCover ? "with-cover" : ""}`, role: "listitem", tabindex: 0, "data-id": t.id,
      ondblclick: () => player.playList(tracks, i), onkeydown: (e) => { if (e.key === "Enter") player.playList(tracks, i); },
      oncontextmenu: (e) => { e.preventDefault(); trackMenu(more, t, tracks, opts); } },
      no, showCover ? cover(t.album_id ? coverURL(t.album_id, 80) : "", t.album_title || t.title, "opt") : null,
      h("div", { style: { minWidth: 0 } },
        h("div", { class: "ti ellipsis" }, t.title, " ", t.stream?.lossless ? h("span", { class: "badge hq" }, "无损") : null, sourceBadge(t)),
        h("div", { class: "ar ellipsis" }, t.artist_name || "未知艺人")),
      h("a", { class: "al ellipsis", href: t.album_id ? `#/album/${t.album_id}` : undefined, onclick: (e) => e.stopPropagation() }, t.album_title || ""),
      h("span", { class: "du" }, fmtDur(t.duration_ms)),
      h("div", { class: "acts" }, h("button", { class: "btn icon sm ghost", "aria-label": "播放", onclick: (e) => { e.stopPropagation(); player.playList(tracks, i); } }, icon("play")), fav, more));
    row.addEventListener("click", (e) => { if (matchMedia("(max-width: 700px)").matches && !e.target.closest("button,a")) player.playList(tracks, i); });
    return row;
  });
  wrap.append(...rows);
  const mark = () => {
    const cur = player.track?.id;
    rows.forEach((r, i) => {
      const on = tracks[i].id === cur;
      r.classList.toggle("playing", on);
      const n = r.firstChild;
      if (on && !n.querySelector(".eq")) n.replaceChildren(h("span", { class: `eq ${player.playing ? "" : "paused"}` }, h("i"), h("i"), h("i")));
      else if (on) n.querySelector(".eq").classList.toggle("paused", !player.playing);
      else if (n.querySelector(".eq")) n.textContent = numbered ? (tracks[i].track_no || i + 1) : i + 1;
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
    { label: "从这首开始电台", icon: "sparkle", run: () => radio(t) },
    { label: "添加到歌单…", icon: "list", run: () => addToPlaylist([t.id]) },
    "-",
    t.album_id ? { label: "前往专辑", icon: "album", run: () => nav(`#/album/${t.album_id}`) } : null,
    ...(t.artists || []).slice(0, 2).map((a) => ({ label: `前往艺人 · ${a.name}`, icon: "artist", run: () => nav(`#/artist/${a.id}`) })),
    admin && isLocal(t) ? "-" : null,
    admin && isLocal(t) ? { label: "编辑标签 / MusicBrainz 匹配", icon: "edit", run: () => editTrack(t, opts.onChange) } : null,
    admin && isLocal(t) ? { label: "歌词匹配与校对", icon: "lyrics", run: () => nav(`#/lyrics/${t.id}`) } : null,
    { label: "下载原始文件", icon: "download", run: () => { location.href = downloadURL(t.id); } },
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
  const res = h("div", { class: "stack" }, h("p", { class: "muted", style: { margin: 0 } }, "从 MusicBrainz 查找候选，按置信度排序；选中后对比差异，勾选要应用的字段写入 Core 数据库。"));
  const run = async () => {
    res.replaceChildren(h("div", { class: "skel", style: { height: "70px" } }), h("div", { class: "skel", style: { height: "70px" } }));
    try {
      const r = await get(`/${kind}s/${obj.id}/match${qs({ title: q1.value, artist: q2.value })}`);
      if (!r.items.length) return res.replaceChildren(h("p", { class: "muted" }, "没有找到候选。试试修改标题或艺人再搜。"));
      res.replaceChildren(...r.items.sort((a, b) => b.score - a.score).map((c) => {
        const b = h("button", { class: "cand", onclick: () => { [...res.children].forEach((x) => x.setAttribute("aria-selected", x === b)); showDiff(c); } },
          h("div", { class: "row" }, h("b", { class: "ellipsis" }, c.title), h("span", { class: "spacer" }), conf(c.score)),
          h("div", { class: "muted", style: { fontSize: "13px" } }, [c.artist, c.album, c.year, c.label, c.duration_ms ? fmtDur(c.duration_ms) : ""].filter(Boolean).join(" · ")),
          h("div", { class: "dim mono" }, c.mb_recording_id || c.mb_release_id || c.external_id));
        return b;
      }));
    } catch (e) { res.replaceChildren(h("p", { class: "muted" }, e.status === 503 ? "MusicBrainz 未配置。" : `查询失败：${e.message}`)); }
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
    diff.replaceChildren(h("h3", { style: { margin: "6px 0" } }, "应用到表单"), ...rows,
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
  const tabs = tabsBar([["edit", "标签"], ["mb", "MusicBrainz 匹配"]], (k) => body.replaceChildren(panes[k]));
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
  const tabs = tabsBar([["edit", "信息"], ["cover", "封面"], ["mb", "MusicBrainz 匹配"]], (k) => body.replaceChildren(panes[k]));
  const d = dialog({ title: `编辑专辑 · ${al.title}`, wide: true, tabs, body,
    foot: [h("button", { class: "btn danger", onclick: async () => { if (await resetOverride("album", al.id)) { d.close(); onChange?.(); } } }, "还原"), h("span", { class: "spacer" }),
      h("button", { class: "btn", onclick: () => d.close() }, "取消"),
      h("button", { class: "btn primary", onclick: async () => { try { if (await saveOverride("album", al.id, form.changed())) { d.close(); onChange?.(); } } catch (e) { errToast(e); } } }, "保存")] });
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
