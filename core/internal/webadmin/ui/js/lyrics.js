// Lyrics workbench: LRCLIB candidates → preview → bind; manual edit / upload LRC; offset; tap-to-sync.
import { get, put, del, qs, coverURL } from "./api.js";
import { h, icon, cover, fmtDur, toast, errToast, empty, debounce, confirmDlg, isLocal, sourceBadge, NOT_LOCAL_MSG, providerLabel } from "./ui.js";
import { player } from "./player.js";
import { isAdmin, nav } from "./state.js";

const ts = (ms) => { ms = Math.max(0, Math.round(ms)); const m = Math.floor(ms / 60000), s = Math.floor((ms % 60000) / 1000), c = Math.floor((ms % 1000) / 10); return `[${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}.${String(c).padStart(2, "0")}]`; };
const TAG = /^\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\]/;
export function parseLRC(src) {
  const out = []; let offset = 0;
  for (const raw of src.replace(/\r/g, "").split("\n")) {
    let line = raw.trim(); const times = [];
    let m; while ((m = TAG.exec(line))) { const f = m[3] ? +m[3].padEnd(3, "0").slice(0, 3) : 0; times.push(+m[1] * 60000 + +m[2] * 1000 + f); line = line.slice(m[0].length).trim(); }
    const om = /^\[offset:\s*([+-]?\d+)\]$/i.exec(line); if (!times.length && om) { offset = +om[1]; continue; }
    if (!times.length) continue;
    line = line.replace(/<\d{1,3}:\d{1,2}(?:[.:]\d{1,3})?>/g, "");
    for (const t of times) out.push({ t, text: line });
  }
  return { lines: out.sort((a, b) => a.t - b.t), offset };
}
const toLRC = (l) => (l.synced ? l.lines.map((x) => `${ts(x.time_ms)}${x.text}`) : l.lines.map((x) => x.text)).join("\n");

export async function lyricsPage(main, id, { cleanup }) {
  if (!id) return pickTrack(main);
  const t = await get(`/tracks/${id}`);
  if (!isLocal(t)) {
    main.replaceChildren(h("div", { class: "page-head" }, h("div", null, h("h1", null, t.title, " ", sourceBadge(t)), h("div", { class: "sub" }, t.artist_name || ""))),
      empty(NOT_LOCAL_MSG + "。在线来源的歌词由来源提供，不能在这里绑定、编辑或上传。", "lyrics", h("a", { class: "btn", href: "#/lyrics" }, "换一首")));
    return;
  }
  let current = null, bound = null;
  try { current = await get(`/tracks/${id}/lyrics`); } catch {}
  if (isAdmin()) { try { bound = await get(`/tracks/${id}/lyrics/user`); } catch {} }

  const ta = h("textarea", { class: "input", rows: 18, spellcheck: false, placeholder: "[00:12.30]第一句歌词\n[00:16.80]第二句…\n\n也可以粘贴纯文本，然后用「打点」逐句对齐时间。" });
  ta.value = bound ? bound.lrc : current ? toLRC(current) : "";
  let offset = bound?.offset_ms || 0, source = bound?.source || "manual";
  const offVal = h("b", { class: "mono" });
  const off = h("input", { type: "range", class: "range", min: -5000, max: 5000, step: 50, value: offset, "aria-label": "时间偏移", style: { flex: 1 } });
  const syncOff = () => { offVal.textContent = `${offset > 0 ? "+" : ""}${(offset / 1000).toFixed(2)}s`; off.value = offset; off.style.setProperty("--p", `${((offset + 5000) / 10000) * 100}%`); renderPreview(); };
  off.oninput = () => { offset = +off.value; syncOff(); };
  const preview = h("div", { class: "lrc-preview", "aria-live": "off" });
  let parsed = { lines: [] }, pLines = [];
  function renderPreview() {
    parsed = parseLRC(ta.value);
    if (!parsed.lines.length) { preview.replaceChildren(h("div", { class: "dim" }, ta.value.trim() ? "纯文本歌词（无时间轴）。使用下方「打点」对齐。" : "空")); pLines = []; return; }
    pLines = parsed.lines.map((l) => h("div", { onclick: () => { if (player.track?.id === id) player.seek(Math.max(0, l.t - offset - parsed.offset)); } }, h("span", { class: "tm" }, ts(Math.max(0, l.t - offset - parsed.offset)).slice(1, -1)), l.text || "♪"));
    preview.replaceChildren(...pLines);
    hl(true);
  }
  let lastI = -2;
  function hl(force) {
    if (player.track?.id !== id || !pLines.length) return;
    const now = player.time; let i = -1;
    parsed.lines.forEach((l, k) => { if (l.t - offset - parsed.offset <= now + 100) i = k; });
    if (i === lastI && !force) return; lastI = i;
    pLines.forEach((p, k) => p.classList.toggle("on", k === i));
    if (i >= 0) preview.scrollTo({ top: pLines[i].offsetTop - preview.clientHeight / 2, behavior: "smooth" });
  }
  ta.addEventListener("input", debounce(renderPreview, 200));
  cleanup(player.on((k) => { if (k === "time") hl(); }));

  // tap-to-sync: stamp current playback time onto the next un-stamped line
  let tapIdx = 0;
  const tapBtn = h("button", { class: "btn sm", title: "播放时按此按钮（或 T 键）给下一行打上当前时间", onclick: () => tap() }, icon("pulse"), "打点");
  function tap() {
    if (player.track?.id !== id) return toast("请先播放这首歌", "err");
    const lines = ta.value.replace(/\r/g, "").split("\n");
    while (tapIdx < lines.length && !lines[tapIdx].replace(TAG, "").trim()) tapIdx++;
    if (tapIdx >= lines.length) return toast("已到最后一行");
    lines[tapIdx] = ts(player.time) + lines[tapIdx].replace(new RegExp(TAG.source, "g"), "").replace(/^(\[\d+:\d+(?:[.:]\d+)?\])+/, "");
    tapIdx++; ta.value = lines.join("\n"); renderPreview();
  }
  const onKey = (e) => { if (e.key.toLowerCase() === "t" && !e.target.closest("textarea,input")) tap(); };
  addEventListener("keydown", onKey); cleanup(() => removeEventListener("keydown", onKey));

  const file = h("input", { type: "file", accept: ".lrc,.txt,text/plain", hidden: true, onchange: async () => {
    const f = file.files[0]; if (!f) return;
    const buf = await f.arrayBuffer();
    let text = new TextDecoder("utf-8", { fatal: false }).decode(buf);
    if (text.includes("\uFFFD")) { try { text = new TextDecoder("gb18030").decode(buf); } catch {} }
    ta.value = text.replace(/^\uFEFF/, ""); source = "manual"; renderPreview(); toast(`已载入 ${f.name}`);
  } });

  // candidates
  const q = h("input", { class: "input", type: "search", value: `${t.title} ${t.artist_name || ""}`.trim() });
  const cands = h("div", { class: "stack" });
  async function search() {
    cands.replaceChildren(h("div", { class: "skel", style: { height: "64px" } }), h("div", { class: "skel", style: { height: "64px" } }));
    try {
      const r = await get(`/tracks/${id}/lyrics/search${qs({ q: q.value })}`);
      if (!r.items.length) return cands.replaceChildren(h("p", { class: "muted" }, "LRCLIB / 网易云 / QQ音乐 / 酷狗 都没有找到候选，换个关键词试试。"));
      const dur = t.duration_ms;
      // Server order = shared scorer (lyrics/rank), same as the app API.
      const scored = r.items.map((c) => ({ c, diff: c.duration_ms && dur ? Math.abs(c.duration_ms - dur) : 1e9 }));
      cands.replaceChildren(...scored.map(({ c, diff }) => {
        const b = h("button", { class: "cand", onclick: () => {
          [...cands.children].forEach((x) => x.setAttribute("aria-selected", x === b));
          ta.value = c.synced_lyrics || c.plain_lyrics || ""; source = c.provider || "lrclib"; offset = 0; syncOff(); renderPreview();
        } },
          h("div", { class: "row" }, h("b", { class: "ellipsis" }, c.title), h("span", { class: "spacer" }),
            c.score != null ? h("span", { class: `score-pill ${c.score >= 80 ? "hi" : c.score >= 50 ? "mid" : "lo"}`, title: scoreTip(c.score_breakdown) }, `${c.score}`) : null,
            h("span", { class: "src-text" }, providerLabel(c.provider)),
            c.translation_lyrics ? h("span", { class: "src-text" }, "含翻译") : null,
            c.instrumental ? h("span", { class: "badge" }, "纯音乐") : h("span", { class: `badge ${c.synced ? "ok" : ""}` }, c.synced ? "逐行同步" : "纯文本")),
          h("div", { class: "muted ellipsis", style: { fontSize: "13px" } }, [c.artist, c.album, c.duration_ms ? fmtDur(c.duration_ms) : ""].filter(Boolean).join(" · "),
            diff < 1e9 ? h("span", { style: { color: diff < 2000 ? "var(--accent)" : diff < 5000 ? "var(--warn)" : "var(--danger)", marginLeft: "8px" } }, diff < 2000 ? "时长吻合" : `时长差 ${(diff / 1000).toFixed(1)}s`) : null),
          h("div", { class: "dim ellipsis", style: { fontSize: "12.5px" } }, (c.synced_lyrics || c.plain_lyrics || "").replace(/\[[^\]]*\]/g, "").split("\n").filter(Boolean).slice(0, 2).join(" / ")));
        return b;
      }));
    } catch (e) { cands.replaceChildren(h("p", { class: "muted" }, e.status === 502 ? "LRCLIB 暂时无法访问（检查 Core 的网络）。" : e.message)); }
  }
  q.addEventListener("keydown", (e) => { if (e.key === "Enter") search(); });

  const status = h("span", { class: `badge ${bound ? "ok" : current ? "" : "warn"}` }, bound ? `已绑定 · ${bound.source}` : current ? `当前来源：${{ sidecar: "同名 .lrc 文件", embedded: "内嵌标签" }[current.source] || current.source}` : "无歌词");
  const saveBtn = h("button", { class: "btn primary", disabled: !isAdmin(), onclick: async () => {
    if (!ta.value.trim()) return toast("歌词为空", "err");
    try { await put(`/tracks/${id}/lyrics`, { lrc: ta.value, offset_ms: Math.round(offset), source }); toast("歌词已绑定到 Core（重扫后保留）"); if (player.track?.id === id) player.loadLyrics(); lyricsPage(main, id, { cleanup }); } catch (e) { errToast(e); }
  } }, icon("link"), "绑定歌词");
  const unbind = bound ? h("button", { class: "btn danger", onclick: async () => {
    if (!(await confirmDlg("解除绑定", "删除手动绑定的歌词？将回退到同名 .lrc / 内嵌歌词 / 在线来源。", "解除", true))) return;
    try { await del(`/tracks/${id}/lyrics`); toast("已解除绑定"); if (player.track?.id === id) player.loadLyrics(); lyricsPage(main, id, { cleanup }); } catch (e) { errToast(e); }
  } }, "解除绑定") : null;

  main.replaceChildren(
    h("div", { class: "page-head" }, h("div", { class: "row", style: { flex: 1, minWidth: 0, gap: "16px" } }, h("div", { style: { width: "72px" } }, cover(t.album_id ? coverURL(t.album_id, 160) : "", t.album_title || t.title)),
      h("div", { style: { minWidth: 0 } }, h("div", { class: "eyebrow" }, "歌词匹配与校对"), h("h1", { class: "ellipsis" }, t.title), h("div", { class: "sub" }, [t.artist_name, t.album_title, fmtDur(t.duration_ms)].filter(Boolean).join(" · "), " ", status))),
      h("button", { class: "btn", onclick: () => player.playList([t]) }, icon("play"), "试听"), h("a", { class: "btn", href: "#/lyrics" }, "换一首")),
    !isAdmin() ? h("div", { class: "card", style: { marginBottom: "16px" } }, "当前设备不是管理员，只能预览，无法绑定。") : "",
    h("div", { class: "two-col" },
      h("div", { class: "card stack" }, h("h3", null, icon("search"), "在线候选 · 本地 + LRCLIB + 网易云 + QQ + 酷狗"), h("div", { class: "row" }, q, h("button", { class: "btn primary", onclick: search }, "搜索")), cands),
      h("div", { class: "card stack" }, h("h3", null, icon("edit"), "编辑 LRC"), ta,
        h("div", { class: "row wrap" }, tapBtn, h("button", { class: "btn sm", onclick: () => { tapIdx = 0; ta.value = ta.value.split("\n").map((l) => l.replace(/^(\[\d+:\d+(?:[.:]\d+)?\])+/, "")).join("\n"); renderPreview(); toast("已清除时间轴，播放后按「打点」逐行对齐"); } }, "清除时间轴"),
          h("button", { class: "btn sm", onclick: () => file.click() }, icon("upload"), "上传 .lrc"), file,
          h("button", { class: "btn sm", onclick: () => { const blob = new Blob([ta.value], { type: "text/plain" }); h("a", { href: URL.createObjectURL(blob), download: `${t.title}.lrc` }).click(); } }, icon("download"), "下载")),
        h("div", { class: "row" }, h("span", { class: "muted", style: { whiteSpace: "nowrap" } }, "时间偏移"), off, offVal, h("button", { class: "btn sm ghost", onclick: () => { offset = 0; syncOff(); } }, "归零")),
        h("div", { class: "muted", style: { fontSize: "12.5px" } }, "偏移为正：歌词提前显示。播放中点击预览行可跳转。"),
        preview,
        h("div", { class: "row" }, unbind, h("span", { class: "spacer" }), saveBtn))));
  syncOff(); search();
}

async function pickTrack(main) {
  const q = h("input", { class: "input", type: "search", placeholder: "搜索要匹配歌词的歌曲（支持拼音）", style: { maxWidth: "520px" } });
  const out = h("div", { class: "section" });
  main.replaceChildren(h("div", { class: "page-head" }, h("div", null, h("h1", null, "歌词匹配"), h("div", { class: "sub" }, "搜索 LRCLIB 候选、预览、绑定；手动编辑或上传 LRC，调整偏移，逐行打点。"))),
    h("div", { class: "toolbar" }, q, player.track ? h("button", { class: "btn primary", onclick: () => nav(`#/lyrics/${player.track.id}`) }, icon("play"), `正在播放：${player.track.title}`) : null), out);
  const run = async () => {
    const p = await get(`/tracks${qs({ q: q.value, limit: 50, sort: q.value ? "" : "added", order: "desc" })}`);
    out.replaceChildren(p.items.length ? h("div", null, p.items.map((t) => h("a", { class: "li", href: `#/lyrics/${t.id}` },
      h("div", { style: { width: "44px" } }, cover(t.album_id ? coverURL(t.album_id, 80) : "", t.album_title || t.title)),
      h("div", { class: "grow" }, h("div", { class: "t ellipsis" }, t.title), h("div", { class: "s ellipsis" }, [t.artist_name, t.album_title].filter(Boolean).join(" — "))),
      h("span", { class: "muted" }, fmtDur(t.duration_ms)), icon("lyrics")))) : empty("没有匹配的歌曲", "track"));
  };
  q.addEventListener("input", debounce(run, 250));
  run();
}

function scoreTip(b) {
  if (!b) return "";
  return `匹配分（满分100）\n标题 ${b.title}/40 · 艺人 ${b.artist}/30 · 专辑 ${b.album}/5\n时长 ${b.duration}/15${b.duration_delta_ms >= 0 ? `（差 ${(b.duration_delta_ms / 1000).toFixed(1)}s）` : ""} · 同步 ${b.synced}/5\n翻译 ${b.translation}/2 · 来源可靠度 ${b.reliability}/3${b.penalty < 1 ? "\n标题不符，已降权" : ""}`;
}
