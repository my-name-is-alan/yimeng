// Metadata workbench: track table with multi-select batch edit, per-row editor, album list with merge.
import { get, post, qs, coverURL } from "./api.js";
import { h, icon, cover, fmtDur, fmtNum, toast, errToast, empty, debounce, confirmDlg, dialog } from "./ui.js";
import { editTrack, editAlbum, batchEdit, pager } from "./components.js";
import { isAdmin, nav } from "./state.js";
import { player } from "./player.js";

export async function metadataPage(main, params) {
  let tab = params.get("tab") || "tracks";
  const body = h("div");
  const seg = h("div", { class: "seg" }, [["tracks", "曲目"], ["albums", "专辑"]].map(([k, l]) => h("button", { "aria-pressed": k === tab, onclick: (e) => { tab = k; [...seg.children].forEach((b) => b.setAttribute("aria-pressed", b === e.currentTarget)); render(); } }, l)));
  main.replaceChildren(h("div", { class: "page-head" }, h("div", { style: { flex: 1 } }, h("h1", null, "元数据"), h("div", { class: "sub" }, "编辑保存在 Core 数据库中（不改动音频文件），重扫后保留；可随时还原为文件标签。")), seg),
    isAdmin() ? "" : h("div", { class: "card", style: { marginBottom: "16px" } }, "当前设备不是管理员，只能查看。"), body);
  const render = () => tab === "tracks" ? tracksTab(body, params) : albumsTab(body);
  render();
}

async function tracksTab(box, params) {
  let q = "", offset = 0, problem = "", page, albumId = params.get("album_id") || "";
  const sel = new Set();
  const list = h("div"), bar = h("div");
  const filter = h("input", { class: "input", type: "search", placeholder: "筛选标题 / 艺人 / 专辑（拼音）", oninput: debounce(() => { q = filter.value; offset = 0; load(); }) });
  const chips = h("div", { class: "chips" }, [["", "全部"], ["genre", "缺少流派"], ["year", "缺少年份"], ["track", "缺少音轨号"]].map(([k, l]) => h("button", { class: "chip", "aria-pressed": k === problem, onclick: (e) => { problem = k; [...chips.children].forEach((b) => b.setAttribute("aria-pressed", b === e.currentTarget)); draw(); } }, l)));
  box.replaceChildren(h("div", { class: "toolbar" }, filter, chips, albumId ? h("button", { class: "chip", "aria-pressed": "true", onclick: () => { albumId = ""; nav("#/metadata"); } }, "仅本专辑 ✕") : null), bar, list);
  const drawBar = () => bar.replaceChildren(sel.size ? h("div", { class: "batchbar" }, h("b", null, `已选 ${sel.size} 首`), h("span", { class: "spacer" }),
    h("button", { class: "btn sm", onclick: () => { sel.clear(); draw(); } }, "取消选择"),
    h("button", { class: "btn sm", onclick: () => player.playList(page.items.filter((t) => sel.has(t.id))) }, icon("play"), "播放"),
    h("button", { class: "btn sm accent", disabled: !isAdmin(), onclick: () => batchEdit([...sel], () => { sel.clear(); load(); }) }, icon("wand"), "批量编辑")) : "");
  function draw() {
    const items = page.items.filter((t) => problem === "genre" ? !t.genre : problem === "year" ? !t.year : problem === "track" ? !t.track_no : true);
    const all = h("input", { type: "checkbox", class: "chk", "aria-label": "全选", checked: items.length && items.every((t) => sel.has(t.id)), onchange: (e) => { items.forEach((t) => e.target.checked ? sel.add(t.id) : sel.delete(t.id)); draw(); } });
    list.replaceChildren(items.length ? h("div", { class: "tracks" },
      h("div", { class: "trow head with-check" }, all, h("span"), h("span", null, "标题 / 艺人"), h("span", { class: "al" }, "专辑"), h("span", { class: "ge" }, "流派 · 年份"), h("span", { class: "du" }, "时长"), h("span")),
      items.map((t) => {
        const cb = h("input", { type: "checkbox", class: "chk", checked: sel.has(t.id), "aria-label": `选择 ${t.title}`, onchange: (e) => { e.target.checked ? sel.add(t.id) : sel.delete(t.id); row.classList.toggle("sel", e.target.checked); drawBar(); } });
        const row = h("div", { class: `trow with-check ${sel.has(t.id) ? "sel" : ""}` }, cb, cover(t.album_id ? coverURL(t.album_id, 80) : "", t.album_title || t.title),
          h("div", { style: { minWidth: 0 } }, h("div", { class: "ti ellipsis" }, t.track_no ? h("span", { class: "dim" }, `${t.disc_no > 1 ? t.disc_no + "-" : ""}${t.track_no}. `) : null, t.title), h("div", { class: "ar ellipsis" }, t.artist_name || h("span", { style: { color: "var(--warn)" } }, "无艺人"))),
          h("div", { class: "al ellipsis" }, t.album_title || "—"),
          h("div", { class: "ge ellipsis" }, t.genre || h("span", { style: { color: "var(--warn)" } }, "无流派"), " · ", t.year || h("span", { style: { color: "var(--warn)" } }, "—")),
          h("span", { class: "du" }, fmtDur(t.duration_ms)),
          h("div", { class: "row", style: { justifyContent: "flex-end", gap: "2px" } },
            h("button", { class: "btn icon sm ghost", "aria-label": "编辑", disabled: !isAdmin(), onclick: () => editTrack(t, load) }, icon("edit")),
            h("a", { class: "btn icon sm ghost", "aria-label": "歌词", href: `#/lyrics/${t.id}` }, icon("lyrics"))));
        return row;
      })) : empty("没有符合条件的曲目", "check"), pager(page, (o) => { offset = o; load(); }));
    drawBar();
  }
  async function load() {
    try { page = await get(`/tracks${qs({ q, offset, limit: 200, sort: "album", album_id: albumId })}`); draw(); } catch (e) { list.replaceChildren(empty(e.message)); }
  }
  load();
}

async function albumsTab(box) {
  let q = "", offset = 0, page;
  const sel = new Set();
  const list = h("div"), bar = h("div");
  const filter = h("input", { class: "input", type: "search", placeholder: "筛选专辑", oninput: debounce(() => { q = filter.value; offset = 0; load(); }) });
  box.replaceChildren(h("div", { class: "toolbar" }, filter, h("span", { class: "muted" }, "选择两张以上专辑可合并（例如被拆散的同一张专辑）。")), bar, list);
  const drawBar = () => bar.replaceChildren(sel.size >= 2 ? h("div", { class: "batchbar" }, h("b", null, `已选 ${sel.size} 张`), h("span", { class: "spacer" }),
    h("button", { class: "btn sm", onclick: () => { sel.clear(); draw(); } }, "取消"),
    h("button", { class: "btn sm accent", disabled: !isAdmin(), onclick: mergeDlg }, "合并专辑…")) : "");
  function mergeDlg() {
    const items = page.items.filter((a) => sel.has(a.id));
    let target = items[0].id;
    const d = dialog({ title: "合并专辑", body: [h("p", { class: "muted", style: { margin: 0 } }, "选择保留哪张专辑的信息，其余专辑的曲目将并入。"),
      ...items.map((a) => h("label", { class: "li", style: { cursor: "pointer" } }, h("input", { type: "radio", name: "tgt", checked: a.id === target, onchange: () => (target = a.id) }),
        h("div", { style: { width: "44px" } }, cover(a.has_cover ? coverURL(a.id, 80) : "", a.title)), h("div", { class: "grow" }, h("div", { class: "t" }, a.title), h("div", { class: "s" }, `${a.artist_name} · ${a.track_count} 首`))))],
      foot: [h("button", { class: "btn", onclick: () => d.close() }, "取消"), h("button", { class: "btn primary", onclick: async () => {
        try { await post("/albums/merge", { target, sources: items.map((a) => a.id).filter((x) => x !== target) }); toast("已合并"); sel.clear(); d.close(); load(); } catch (e) { errToast(e); }
      } }, "合并")] });
  }
  function draw() {
    list.replaceChildren(h("div", null, page.items.map((a) => h("div", { class: "li" },
      h("input", { type: "checkbox", class: "chk", checked: sel.has(a.id), "aria-label": `选择 ${a.title}`, onchange: (e) => { e.target.checked ? sel.add(a.id) : sel.delete(a.id); drawBar(); } }),
      h("div", { style: { width: "52px" } }, cover(a.has_cover ? coverURL(a.id, 100) : "", a.title)),
      h("div", { class: "grow" }, h("a", { class: "t ellipsis", href: `#/album/${a.id}`, style: { display: "block" } }, a.title), h("div", { class: "s ellipsis" }, [a.artist_name, a.year, a.genre, `${a.track_count} 首`].filter(Boolean).join(" · "))),
      a.has_cover ? null : h("span", { class: "badge warn" }, "无封面"),
      h("button", { class: "btn sm", disabled: !isAdmin(), onclick: () => editAlbum(a, load) }, icon("edit"), "编辑")))), pager(page, (o) => { offset = o; load(); }));
    drawBar();
  }
  async function load() { try { page = await get(`/albums${qs({ q, offset, limit: 100 })}`); draw(); } catch (e) { list.replaceChildren(empty(e.message)); } }
  load();
}
