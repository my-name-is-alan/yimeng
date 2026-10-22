// Web player: plays original streams (/api/stream supports HTTP Range → seekable), local queue,
// shuffle/repeat, Media Session, scrobbles to /api/scrobble, synced lyrics.
import { get, post, streamURL, coverURL } from "./api.js";
import { h, icon, cover, fmtDur, setRangeFill, $, toast, isLocal, NOT_LOCAL_MSG } from "./ui.js";

const SKEY = "yimeng.player";
const audio = new Audio();
audio.preload = "auto";

export const player = {
  queue: [], order: [], pos: -1, shuffle: false, repeat: "off", volume: 1, muted: false,
  lyrics: null, lyricsFor: null, listeners: new Set(), listened: 0, lastTick: 0, scrobbled: false,
  get track() { return this.pos >= 0 ? this.queue[this.order[this.pos]] : null; },
  get playing() { return !audio.paused && !audio.ended; },
  get time() { return audio.currentTime * 1000; },
  get duration() { return (isFinite(audio.duration) && audio.duration ? audio.duration * 1000 : this.track?.duration_ms) || 0; },
  on(fn) { this.listeners.add(fn); return () => this.listeners.delete(fn); },
  emit(kind) { this.listeners.forEach((f) => f(kind)); if (kind !== "time") this.save(); },

  playList(tracks, start = 0, { shuffle } = {}) {
    if (!tracks?.length) return;
    this.scrobble();
    this.queue = tracks.map(slim);
    if (shuffle !== undefined) this.shuffle = shuffle;
    this.buildOrder(start);
    this.pos = this.shuffle ? 0 : start;
    this.load(true);
  },
  playNext(tracks) {
    tracks = [].concat(tracks).map(slim);
    if (!this.track) return this.playList(tracks);
    const at = this.queue.length;
    this.queue.push(...tracks);
    this.order.splice(this.pos + 1, 0, ...tracks.map((_, i) => at + i));
    this.emit("queue"); toast(`将在下一首播放 · ${tracks.length} 首`);
  },
  enqueue(tracks) {
    tracks = [].concat(tracks).map(slim);
    if (!this.track) return this.playList(tracks);
    const at = this.queue.length;
    this.queue.push(...tracks);
    this.order.push(...tracks.map((_, i) => at + i));
    this.emit("queue"); toast(`已加入播放队列 · ${tracks.length} 首`);
  },
  removeAt(p) {
    if (p === this.pos) return;
    this.order.splice(p, 1);
    if (p < this.pos) this.pos--;
    this.emit("queue");
  },
  clear() { audio.pause(); audio.removeAttribute("src"); this.queue = []; this.order = []; this.pos = -1; this.emit("track"); },
  buildOrder(start = 0) {
    const n = this.queue.length;
    this.order = [...Array(n).keys()];
    if (this.shuffle && n > 1) {
      for (let i = n - 1; i > 0; i--) { const j = Math.floor(Math.random() * (i + 1)); [this.order[i], this.order[j]] = [this.order[j], this.order[i]]; }
      const k = this.order.indexOf(start); [this.order[0], this.order[k]] = [this.order[k], this.order[0]];
    }
  },
  toggleShuffle() {
    const cur = this.order[this.pos] ?? 0;
    this.shuffle = !this.shuffle;
    this.buildOrder(cur);
    this.pos = this.shuffle ? 0 : cur;
    this.emit("mode"); this.emit("queue");
  },
  cycleRepeat() { this.repeat = { off: "all", all: "one", one: "off" }[this.repeat]; this.emit("mode"); },
  jump(p) { if (p >= 0 && p < this.order.length) { this.scrobble(); this.pos = p; this.load(true); } },
  load(autoplay, at = 0) {
    const t = this.track; if (!t) return;
    this.listened = 0; this.lastTick = 0; this.scrobbled = false;
    audio.src = streamURL(t.id);
    if (at) audio.currentTime = at / 1000;
    if (autoplay) audio.play().catch((e) => { if (e.name !== "AbortError") toast("无法播放：" + e.message, "err"); });
    this.loadLyrics(); this.media(); this.emit("track");
  },
  toggle() { if (!this.track) return; audio.paused ? audio.play().catch(() => {}) : audio.pause(); },
  next(auto = false) {
    if (!this.track) return;
    if (auto && this.repeat === "one") { audio.currentTime = 0; audio.play(); return; }
    this.scrobble(auto);
    if (this.pos + 1 < this.order.length) { this.pos++; this.load(true); }
    else if (this.repeat === "all" || !auto) { this.pos = 0; this.load(this.repeat === "all" || !auto); }
    else { this.emit("state"); }
  },
  prev() {
    if (audio.currentTime > 3 || this.pos <= 0) { audio.currentTime = 0; return; }
    this.scrobble(); this.pos--; this.load(true);
  },
  seek(ms) { if (this.track) { audio.currentTime = Math.max(0, Math.min(ms, this.duration) / 1000); this.emit("time"); } },
  setVolume(v) { this.volume = v; audio.volume = v; if (v > 0) { this.muted = false; audio.muted = false; } this.emit("volume"); },
  toggleMute() { this.muted = !this.muted; audio.muted = this.muted; this.emit("volume"); },
  scrobble(completed = false) {
    const t = this.track; if (!t || this.scrobbled) return;
    const played = this.listened;
    if (!completed && played < 30000 && played < (t.duration_ms || 0) / 2) return;
    this.scrobbled = true;
    post("/scrobble", { track_id: t.id, duration_ms: Math.round(played), completed, played_at: new Date(Date.now() - played).toISOString() }).catch(() => {});
  },
  async loadLyrics() {
    const t = this.track; this.lyrics = null; this.lyricsFor = t?.id;
    if (!t) return;
    try {
      const l = await get(`/tracks/${t.id}/lyrics`);
      if (this.lyricsFor === t.id) { this.lyrics = l; this.emit("lyrics"); }
    } catch { if (this.lyricsFor === t.id) { this.lyrics = { none: true }; this.emit("lyrics"); } }
  },
  media() {
    const t = this.track; if (!("mediaSession" in navigator) || !t) return;
    navigator.mediaSession.metadata = new MediaMetadata({ title: t.title, artist: t.artist_name, album: t.album_title || "",
      artwork: t.album_id ? [{ src: coverURL(t.album_id, 512), sizes: "512x512" }] : [] });
  },
  save() {
    try {
      localStorage.setItem(SKEY, JSON.stringify({ queue: this.queue.slice(0, 3000), order: this.order.slice(0, 3000), pos: this.pos,
        shuffle: this.shuffle, repeat: this.repeat, volume: this.volume, at: Math.round(audio.currentTime * 1000) }));
    } catch {}
  },
  restore() {
    try {
      const s = JSON.parse(localStorage.getItem(SKEY) || "null"); if (!s?.queue?.length) return;
      Object.assign(this, { queue: s.queue, order: s.order, pos: s.pos, shuffle: !!s.shuffle, repeat: s.repeat || "off", volume: s.volume ?? 1 });
      audio.volume = this.volume;
      if (this.track) { audio.src = streamURL(this.track.id); if (s.at) audio.currentTime = s.at / 1000; this.loadLyrics(); this.media(); this.emit("track"); }
    } catch {}
  },
};
export const slim = (t) => ({ id: t.id, title: t.title, artist_name: t.artist_name, album_id: t.album_id, album_title: t.album_title, duration_ms: t.duration_ms, format: t.format,
  stream: t.stream ? { lossless: t.stream.lossless, sample_rate: t.stream.sample_rate, bit_depth: t.stream.bit_depth, bitrate_kbps: t.stream.bitrate_kbps } : undefined });

audio.addEventListener("timeupdate", () => {
  const now = audio.currentTime * 1000;
  if (player.lastTick && now > player.lastTick && now - player.lastTick < 2000) player.listened += now - player.lastTick;
  player.lastTick = now;
  player.emit("time");
});
audio.addEventListener("seeking", () => { player.lastTick = 0; });
audio.addEventListener("play", () => player.emit("state"));
audio.addEventListener("pause", () => { player.emit("state"); });
audio.addEventListener("ended", () => player.next(true));
audio.addEventListener("loadedmetadata", () => player.emit("time"));
audio.addEventListener("error", () => { if (audio.getAttribute("src") && player.track) toast("音频加载失败（该格式可能不受浏览器支持）", "err"); });
if ("mediaSession" in navigator) {
  const ms = navigator.mediaSession;
  ms.setActionHandler("play", () => audio.play());
  ms.setActionHandler("pause", () => audio.pause());
  ms.setActionHandler("nexttrack", () => player.next());
  ms.setActionHandler("previoustrack", () => player.prev());
  try { ms.setActionHandler("seekto", (d) => player.seek(d.seekTime * 1000)); } catch {}
}
addEventListener("pagehide", () => player.save());

// ── UI: now-playing bar + full-screen view ──
export function qualityBadge(t) {
  const s = t?.stream; if (!s) return t?.format ? h("span", { class: "badge hq" }, t.format.toUpperCase()) : null;
  if (s.lossless) return h("span", { class: "badge hq" }, (s.bit_depth > 16 || s.sample_rate > 48000) ? "HI-RES" : "无损");
  return h("span", { class: "badge hq" }, s.bitrate_kbps ? `${s.bitrate_kbps}K` : (t.format || "").toUpperCase());
}

function controls() {
  const playBtn = h("button", { class: "play-circle", "aria-label": "播放/暂停", onclick: () => player.toggle() });
  const shuf = h("button", { class: "btn icon ghost opt", "aria-label": "随机播放", title: "随机播放", onclick: () => player.toggleShuffle() }, icon("shuffle"));
  const rep = h("button", { class: "btn icon ghost opt", "aria-label": "循环模式", onclick: () => player.cycleRepeat() }, icon("repeat"));
  const wrap = h("div", { class: "nb-controls" }, shuf,
    h("button", { class: "btn icon ghost opt", "aria-label": "上一首", onclick: () => player.prev() }, icon("prev")),
    playBtn,
    h("button", { class: "btn icon ghost", "aria-label": "下一首", onclick: () => player.next() }, icon("next")), rep);
  const sync = () => {
    playBtn.replaceChildren(icon(player.playing ? "pause" : "play"));
    shuf.classList.toggle("on", player.shuffle);
    rep.classList.toggle("on", player.repeat !== "off");
    rep.title = { off: "不循环", all: "列表循环", one: "单曲循环" }[player.repeat];
    rep.replaceChildren(icon("repeat"));
    if (player.repeat === "one") rep.append(h("small", { style: { fontSize: "9px", fontWeight: 800, marginLeft: "-5px" } }, "1"));
  };
  return { el: wrap, sync };
}
function seekbar() {
  const r = h("input", { type: "range", class: "range", min: 0, max: 1000, value: 0, "aria-label": "播放进度" });
  const a = h("span", null, "0:00"), b = h("span", null, "0:00");
  let dragging = false;
  r.addEventListener("input", () => { dragging = true; setRangeFill(r); a.textContent = fmtDur((r.value / 1000) * player.duration); });
  r.addEventListener("change", () => { dragging = false; player.seek((r.value / 1000) * player.duration); });
  const sync = () => {
    if (dragging) return;
    const d = player.duration; r.value = d ? Math.round((player.time / d) * 1000) : 0; setRangeFill(r);
    a.textContent = fmtDur(player.time); b.textContent = fmtDur(d);
  };
  return { el: h("div", { class: "seek" }, a, r, b), sync };
}

export function mountNowBar(nav) {
  const bar = $("#nowbar");
  const tint = h("div", { class: "tint" });
  const art = h("div");
  const title = h("div", { class: "nb-title ellipsis" }), sub = h("div", { class: "nb-sub ellipsis" });
  const info = h("div", { class: "nb-track", role: "button", tabindex: 0, "aria-label": "打开正在播放", onclick: () => openNP(nav), onkeydown: (e) => e.key === "Enter" && openNP(nav) },
    art, h("div", { style: { minWidth: 0 } }, title, sub));
  const c = controls(), s = seekbar();
  const vol = h("input", { type: "range", class: "range vol", min: 0, max: 100, value: player.volume * 100, "aria-label": "音量" });
  vol.addEventListener("input", () => { player.setVolume(vol.value / 100); setRangeFill(vol); });
  const muteBtn = h("button", { class: "btn icon ghost", "aria-label": "静音", onclick: () => player.toggleMute() });
  const lyrBtn = h("button", { class: "btn icon ghost", "aria-label": "歌词", title: "歌词", onclick: () => openNP(nav, "lyrics") }, icon("lyrics"));
  const qBtn = h("button", { class: "btn icon ghost", "aria-label": "播放队列", title: "播放队列", onclick: () => openNP(nav, "queue") }, icon("queue"));
  const mini = h("div", { class: "mini-prog" }, h("i"));
  bar.replaceChildren(tint, info, h("div", { class: "nb-center" }, c.el, s.el), h("div", { class: "nb-right" }, lyrBtn, qBtn, muteBtn, vol), mini);
  let cur;
  player.on((k) => {
    const t = player.track;
    bar.hidden = !t;
    if (!t) { cur = null; return; }
    if (k === "time") { s.sync(); mini.firstChild.style.width = `${(player.time / (player.duration || 1)) * 100}%`; return; }
    if (cur !== t.id) {
      cur = t.id;
      art.replaceChildren(cover(coverURL(t.album_id, 120), t.album_title || t.title));
      tint.style.backgroundImage = t.album_id ? `url("${coverURL(t.album_id, 120)}")` : "none";
      title.replaceChildren(t.title, " "); const qb = qualityBadge(t); if (qb) title.append(qb);
      sub.textContent = [t.artist_name, t.album_title].filter(Boolean).join(" — ");
    }
    document.title = player.playing ? `▶ ${t.title} · ${t.artist_name} — 译梦` : "译梦 · 控制台";
    c.sync(); s.sync();
    muteBtn.replaceChildren(icon(player.muted || player.volume === 0 ? "mute" : "volume"));
    vol.value = player.volume * 100; setRangeFill(vol);
  });
  document.addEventListener("keydown", (e) => {
    if (e.target.closest("input, textarea, select, [contenteditable], dialog")) return;
    if (e.code === "Space") { e.preventDefault(); player.toggle(); }
    else if (e.key === "ArrowRight" && e.shiftKey) player.next();
    else if (e.key === "ArrowLeft" && e.shiftKey) player.prev();
    else if (e.key === "ArrowRight" && e.altKey) player.seek(player.time + 10000);
    else if (e.key === "ArrowLeft" && e.altKey) player.seek(player.time - 10000);
  });
}

let npClose = null;
export const closeNP = () => npClose?.();
export function openNP(nav, tab = "lyrics") {
  if (!player.track) return;
  npClose?.();
  const np = $("#np"); np.hidden = false; document.documentElement.style.overflow = "hidden";
  const bloom = h("div", { class: "bloom" });
  const art = h("div", { class: "np-art" });
  const ti = h("div", { class: "t ellipsis" }), su = h("div", { class: "s ellipsis" });
  const c = controls(), s = seekbar();
  const lyr = h("div", { class: "lyrics" });
  const ql = h("div", { class: "queue-list" });
  let mode = tab;
  const tabs = h("div", { class: "seg", role: "group", "aria-label": "视图" });
  const pane = h("div", { class: "np-right" });
  const setTab = (m) => { mode = m; [...tabs.children].forEach((b) => b.setAttribute("aria-pressed", b.dataset.m === m)); pane.replaceChildren(m === "lyrics" ? lyr : ql); m === "queue" ? renderQueue() : renderLyrics(true); };
  for (const [m, l] of [["lyrics", "歌词"], ["queue", "播放队列"]]) tabs.append(h("button", { "data-m": m, onclick: () => setTab(m) }, l));
  const close = () => { np.hidden = true; document.documentElement.style.overflow = ""; off(); removeEventListener("keydown", esc); npClose = null; np.replaceChildren(); };
  const editLyr = h("button", { class: "btn sm", hidden: !isLocal(player.track), onclick: () => { if (!isLocal(player.track)) return toast(NOT_LOCAL_MSG, "err"); const id = player.track.id; close(); nav(`#/lyrics/${id}`); } }, icon("edit"), "校对歌词");
  np.replaceChildren(bloom,
    h("div", { class: "np-top" }, h("button", { class: "btn icon ghost", "aria-label": "收起", onclick: () => close() }, icon("down")), h("div", { class: "spacer" }), tabs, h("div", { class: "spacer" }), editLyr),
    h("div", { class: "np-body" }, h("div", { class: "np-left" }, art, h("div", { class: "np-info" }, ti, su), s.el, c.el), pane));
  let lines = [], lastIdx = -2, curId;
  function renderLyrics(force) {
    const L = player.lyrics;
    if (!L) { lyr.className = "lyrics plain"; lyr.replaceChildren(h("p", null, "正在加载歌词…")); lines = []; return; }
    if (L.none || !L.lines?.length) {
      lyr.className = "lyrics plain";
      lyr.replaceChildren(h("p", null, "暂无歌词"), h("p", { style: { fontSize: "14px", opacity: .6 } }, "可在「歌词匹配」中搜索 LRCLIB 或手动上传 LRC"));
      lines = []; return;
    }
    lyr.className = L.synced ? "lyrics" : "lyrics plain";
    lines = L.lines.map((ln) => h("p", { onclick: () => L.synced && player.seek(ln.time_ms) }, ln.text || "♪"));
    lyr.replaceChildren(...lines); lastIdx = -2; if (force) syncLyrics(true);
  }
  function syncLyrics(instant) {
    const L = player.lyrics; if (!L?.synced || !lines.length) return;
    const t = player.time + 150; let i = -1;
    for (let k = 0; k < L.lines.length; k++) { if (L.lines[k].time_ms <= t) i = k; else break; }
    if (i === lastIdx) return; lastIdx = i;
    lines.forEach((p, k) => { p.classList.toggle("on", k === i); p.classList.toggle("past", k < i); });
    const target = lines[Math.max(i, 0)];
    lyr.scrollTo({ top: target.offsetTop - lyr.clientHeight * 0.4, behavior: instant ? "instant" : "smooth" });
  }
  function renderQueue() {
    ql.replaceChildren(...player.order.map((qi, p) => {
      const q = player.queue[qi];
      return h("div", { class: `qi ${p === player.pos ? "cur" : ""}`, onclick: () => player.jump(p) },
        cover(coverURL(q.album_id, 80), q.album_title || q.title),
        h("div", { style: { minWidth: 0, flex: 1 } }, h("div", { class: "ellipsis", style: { fontWeight: 600 } }, q.title), h("div", { class: "ellipsis", style: { opacity: .6, fontSize: "13px" } }, q.artist_name)),
        h("span", { style: { opacity: .6, fontSize: "12px" } }, fmtDur(q.duration_ms)),
        p !== player.pos ? h("button", { class: "btn icon sm ghost", "aria-label": "移出队列", onclick: (e) => { e.stopPropagation(); player.removeAt(p); } }, icon("close")) : null);
    }));
    ql.children[player.pos]?.scrollIntoView({ block: "center" });
  }
  const update = (k) => {
    const t = player.track; if (!t) return close();
    if (k === "time") { s.sync(); syncLyrics(); return; }
    if (curId !== t.id) {
      curId = t.id;
      bloom.style.backgroundImage = t.album_id ? `url("${coverURL(t.album_id, 300)}")` : "none";
      art.replaceChildren(cover(coverURL(t.album_id, 900), t.album_title || t.title));
      ti.textContent = t.title;
      su.replaceChildren(t.artist_name || "", t.album_title ? " — " : "", t.album_id ? h("a", { href: `#/album/${t.album_id}`, onclick: () => close() }, t.album_title) : "");
    }
    if (k === "lyrics" || k === "track") { if (mode === "lyrics") renderLyrics(true); }
    if (mode === "queue" && (k === "queue" || k === "track")) renderQueue();
    c.sync(); s.sync();
  };
  const off = player.on(update);
  const esc = (e) => { if (e.key === "Escape" && !document.querySelector("dialog[open]")) close(); };
  addEventListener("keydown", esc);
  setTab(tab); update("track");
  npClose = close;
}
