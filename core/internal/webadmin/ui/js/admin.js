// Admin pages: dashboard, devices, settings (roots, providers, transcoding, backups, sources), logs.
import { get, post, put, patch, del, qs, api, onEvent, wsState, store } from "./api.js";
import { h, icon, fmtBytes, fmtNum, fmtLong, fmtTime, fmtAgo, fmtUptime, empty, toast, errToast, confirmDlg, promptDlg, dialog } from "./ui.js";
import { state, isAdmin } from "./state.js";
import { pluginsSection, localHidden } from "./online.js";

const head = (title, sub, ...actions) => h("div", { class: "page-head" }, h("div", { style: { flex: 1, minWidth: 0 } }, h("h1", null, title), sub ? h("div", { class: "sub" }, sub) : null), ...actions);
const card = (title, ic, ...body) => h("div", { class: "card" }, h("h3", null, ic ? icon(ic) : null, title), ...body);
const kv = (pairs) => h("dl", { class: "kv" }, pairs.filter(Boolean).map(([k, v]) => [h("dt", null, k), h("dd", null, v)]));
const stat = (k, v, unit, hero) => h("div", { class: `stat ${hero ? "accent" : ""}` }, h("div", { class: "k" }, k), h("div", { class: "v" }, v, unit ? h("small", null, unit) : null));
const bool = (b, on = "启用", off = "关闭") => h("span", { class: `badge ${b ? "ok" : ""}` }, b ? on : off);
const adminGate = () => isAdmin() ? null : h("div", { class: "card", style: { marginBottom: "16px" } }, h("b", null, "只读模式"), h("div", { class: "muted" }, "当前设备不是管理员，修改类操作会被 Core 拒绝。可在已是管理员的设备上把本设备设为管理员。"));
const SCAN_STATUS = { running: "扫描中", completed: "已完成", failed: "失败", cancelled: "已取消", queued: "排队中", interrupted: "已中断" };

// ── dashboard ──
export async function dashboard(main, { cleanup }) {
  const scanBox = h("div"), sysBox = h("div"), statsBox = h("div", { class: "stats" }), playersBox = h("div"), logBox = h("div", { class: "logs", style: { maxHeight: "260px" } });
  const healthBox = h("div"), jobsBox = h("div"), rootsBox = h("div"), diagBox = h("div");
  const live = h("span", { class: "ws-ind muted" });
  main.replaceChildren(head("仪表盘", h("span", null, "Core 运行状态与后台任务 · ", live),
      h("button", { class: "btn", onclick: () => startScan(false) }, icon("refresh"), "增量扫描"),
      h("button", { class: "btn primary", onclick: () => startScan(true) }, icon("disc"), "完整重扫")),
    adminGate() || "", statsBox,
    h("div", { class: "grid-cards section", style: { marginTop: "18px" } }, card("媒体库扫描", "disc", scanBox), card("后台任务", "pulse", jobsBox), card("系统", "settings", sysBox),
      card("音乐目录", "folder", rootsBox), card("曲库健康", "check", healthBox), card("播放器", "speaker", playersBox)),
    h("div", { class: "section" }, diagBox),
    h("section", { class: "section" }, h("div", { class: "section-head" }, h("h2", null, "最近日志"), h("a", { class: "btn sm", href: "#/logs" }, "全部日志")), logBox));
  const setLive = () => live.replaceChildren(h("span", { class: `dot ${wsState.connected ? "ok" : "err"}` }), wsState.connected ? "实时连接" : "轮询中");
  setLive();
  async function startScan(full) {
    try { await post("/library/scan", { full }); toast(full ? "已开始完整重扫" : "已开始增量扫描"); refresh(); }
    catch (e) { e.status === 409 ? toast("已有扫描在进行") : errToast(e); }
  }
  function renderScan(j) {
    if (!j) return scanBox.replaceChildren(h("p", { class: "muted" }, "还没有扫描记录。"));
    const running = j.status === "running" || j.status === "queued";
    const pct = Math.round((j.progress || 0) * 100);
    scanBox.replaceChildren(
      h("div", { class: "row" }, h("span", { class: `badge ${running ? "warn" : j.status === "completed" ? "ok" : "err"}` }, SCAN_STATUS[j.status] || j.status), h("span", { class: "spacer" }), h("b", null, running ? `${pct}%` : "")),
      h("div", { class: `progress ${running && !j.total ? "indet" : ""}`, style: { margin: "12px 0" }, role: "progressbar", "aria-valuenow": pct, "aria-valuemin": 0, "aria-valuemax": 100 }, h("i", { style: { width: `${running ? pct : 100}%` } })),
      kv([["进度", `${fmtNum(j.processed)} / ${fmtNum(j.total)} 个文件`], ["新增 / 更新", `${j.added} / ${j.updated}`], ["未变 / 移除", `${j.unchanged} / ${j.removed}`],
        j.failed ? ["失败", h("span", { style: { color: "var(--danger)" } }, j.failed)] : null,
        ["开始", fmtTime(j.started_at)], j.finished_at ? ["结束", `${fmtTime(j.finished_at)}（${Math.max(0, (new Date(j.finished_at) - new Date(j.started_at)) / 1000).toFixed(1)} 秒）`] : null,
        j.error ? ["错误", j.error] : null]));
  }
  async function refresh() {
    try {
      const [s, health, players, logs] = await Promise.all([get("/system"), get("/library/health").catch(() => null), get("/players").catch(() => ({ items: [] })),
        isAdmin() ? get(`/system/logs${qs({ level: "info", limit: 12 })}`).catch(() => ({ items: [] })) : { items: [] }]);
      state.system = s;
      const L = s.library;
      statsBox.replaceChildren(stat("曲目", fmtNum(L.tracks), "首", true), stat("专辑", fmtNum(L.albums)), stat("艺人", fmtNum(L.artists)), stat("总时长", Math.round(L.duration_ms / 3600000), "小时"),
        stat("占用空间", fmtBytes(L.bytes)), stat("无损占比", L.files ? Math.round((L.lossless_files / L.files) * 100) : 0, "%"), stat("播放次数", fmtNum(L.plays)), stat("歌单", fmtNum(L.playlists)));
      renderScan(s.scan);
      const t = s.transcode;
      jobsBox.replaceChildren(h("div", { class: "stack" },
        h("div", null, h("div", { class: "row" }, h("b", null, "响度分析 (ReplayGain/R128)"), h("span", { class: "spacer" }), bool(s.loudness.enabled)),
          s.loudness.enabled ? h("div", { style: { marginTop: "8px" } }, h("div", { class: `progress ${s.loudness.pending ? "indet" : ""}` }, h("i", { style: { width: s.loudness.pending ? "30%" : "100%" } })),
            h("div", { class: "muted", style: { fontSize: "13px", marginTop: "6px" } }, s.loudness.pending ? `待分析 ${s.loudness.pending} 个文件` : "全部完成", s.loudness.failed ? ` · 失败 ${s.loudness.failed}` : "")) : null),
        h("div", null, h("div", { class: "row" }, h("b", null, "转码 (ffmpeg)"), h("span", { class: "spacer" }), bool(t.available, "可用", "不可用")),
          h("div", { class: "progress", style: { margin: "8px 0 6px" } }, h("i", { style: { width: `${t.cache_max_bytes ? Math.min(100, (t.cache_bytes / t.cache_max_bytes) * 100) : 0}%` } })),
          h("div", { class: "muted", style: { fontSize: "13px" } }, `缓存 ${fmtBytes(t.cache_bytes)} / ${fmtBytes(t.cache_max_bytes)} · ${t.formats.filter((f) => f.format !== "original").map((f) => f.format.toUpperCase()).join(" / ")}`)),
        h("div", null, h("div", { class: "row" }, h("b", null, "数据库备份"), h("span", { class: "spacer" }), h("span", { class: "badge" }, `${s.backups.count} 份`)),
          h("div", { class: "muted", style: { fontSize: "13px", marginTop: "6px" } }, s.backups.last ? `最近一次：${fmtAgo(s.backups.last)}（${fmtTime(s.backups.last)}）` : "还没有备份")),
        h("div", null, h("div", { class: "row" }, h("b", null, "元数据补全"), h("span", { class: "spacer" }), bool(s.features.providers)),
          h("div", { class: "row wrap", style: { marginTop: "8px" } }, h("button", { class: "btn sm", onclick: async () => { try { await post("/metadata/enrich"); toast("已触发 MusicBrainz 补全"); } catch (e) { e.status === 503 || e.status === 501 ? toast("未启用 MusicBrainz 自动补全（可在设置中查看）", "err") : errToast(e); } } }, icon("wand"), "立即补全")))));
      sysBox.replaceChildren(kv([["版本", `${s.version} · schema ${s.schema_version}`], ["运行时", `${s.go_version} · ${s.os}/${s.arch}`], ["已运行", fmtUptime(s.uptime_sec)],
        ["数据库", `${fmtBytes(s.database.size_bytes)}（WAL ${fmtBytes(s.database.wal_bytes)}）`], ["磁盘剩余", fmtBytes(s.database.free_bytes)],
        ["设备", `${s.devices.paired} 台已配对 · 24h 活跃 ${s.devices.active_24h}`],
        ["功能", h("div", { class: "chips" }, Object.entries(s.features).map(([k, v]) => h("span", { class: `badge ${v ? "ok" : ""}` }, k)))]]));
      rootsBox.replaceChildren(...(s.roots.length ? s.roots.map((r) => h("div", { class: "li" }, h("span", { class: `dot ${r.ok ? "ok" : "err"}` }),
        h("div", { class: "grow" }, h("div", { class: "t ellipsis mono", title: r.path }, r.path), h("div", { class: "s" }, r.ok ? `${fmtNum(r.files)} 个文件 · 剩余 ${fmtBytes(r.free_bytes)}` : r.error)))) : [h("p", { class: "muted" }, "未配置音乐目录")]),
        h("a", { class: "btn sm", href: "#/settings", style: { marginTop: "10px" } }, "管理目录"));
      if (health) healthBox.replaceChildren(kv([["缺失文件", warnNum(health.missing_files)], ["缺少封面的专辑", warnNum(health.no_cover_albums)], ["缺少流派的曲目", warnNum(health.no_genre_tracks)],
        ["重复路径", warnNum(health.duplicate_paths)], ["失效的手动修改", warnNum(health.orphan_overrides)],
        ["格式分布", h("div", { class: "chips" }, Object.entries(health.by_format || {}).map(([f, n]) => h("span", { class: "badge" }, `${f.toUpperCase()} ${n}`)))]]),
        h("a", { class: "btn sm", href: "#/metadata", style: { marginTop: "12px" } }, icon("edit"), "去整理元数据"));
      playersBox.replaceChildren(...(players.items.length ? players.items.slice(0, 6).map((p) => h("div", { class: "li" }, h("span", { class: `dot ${p.online ? "ok" : ""}` }),
        h("div", { class: "grow" }, h("div", { class: "t ellipsis" }, p.track ? `${p.track.title} — ${p.track.artist_name}` : "空闲"), h("div", { class: "s ellipsis" }, `${deviceName(p.player_id)} · ${{ playing: "播放中", paused: "已暂停", idle: "空闲", buffering: "缓冲中" }[p.transport] || p.transport}`)),
        p.track ? h("button", { class: "btn icon sm", "aria-label": "播放/暂停", onclick: async () => { try { await post(`/players/${p.player_id}/${p.transport === "playing" ? "pause" : "play"}`, {}); setTimeout(refresh, 300); } catch (e) { errToast(e); } } }, icon(p.transport === "playing" ? "pause" : "play")) : null))
        : [h("p", { class: "muted" }, "没有在线的播放器")]));
      diagBox.replaceChildren(...(s.diagnostics || []).map((d) => h("div", { class: "li" }, h("span", { class: `badge ${d.level === "error" ? "err" : d.level === "warn" ? "warn" : ""}` }, d.level), h("div", { class: "grow" }, h("b", null, d.code), " ", h("span", { class: "muted" }, d.message)))));
      logBox.replaceChildren(...(logs.items.length ? logs.items.slice().reverse().map(logLine) : [h("div", { class: "dim" }, isAdmin() ? "暂无日志" : "需要管理员权限查看日志")]));
    } catch (e) { if (e.status !== 401) errToast(e); }
  }
  const warnNum = (n) => h("span", { style: { color: n ? "var(--warn)" : "inherit", fontWeight: n ? 700 : 400 } }, fmtNum(n));
  let devices = {};
  get("/devices").then((r) => { r.items.forEach((d) => (devices[d.id] = d.name)); }).catch(() => {});
  const deviceName = (id) => devices[id] || id.slice(0, 12);
  await refresh();
  const offs = [onEvent("scan.progress", (j) => { renderScan(j); if (j.status !== "running") setTimeout(refresh, 400); }), onEvent("library.loudness", () => refresh()),
    onEvent("player.state", () => refresh()), onEvent("ws.status", setLive)];
  const timer = setInterval(() => { if (!document.hidden) refresh(); }, wsState.connected ? 10000 : 3000);
  cleanup(() => { offs.forEach((f) => f()); clearInterval(timer); });
}

export function logLine(e) {
  const attrs = e.attrs ? Object.entries(e.attrs).map(([k, v]) => `${k}=${v}`).join(" ") : "";
  return h("div", { class: `l lv-${e.level}` }, h("span", { class: "at" }, new Date(e.time).toLocaleTimeString("zh-CN", { hour12: false })), h("span", null, e.level), h("span", null, e.msg, attrs ? h("span", { class: "at" }, "  " + attrs) : ""));
}

// ── logs ──
export async function logs(main, { cleanup }) {
  let level = "info", after = 0, paused = false, filterText = "";
  const box = h("div", { class: "logs", style: { maxHeight: "calc(100dvh - 260px)" } });
  const seg = h("div", { class: "seg" }, ["debug", "info", "warn", "error"].map((l) => h("button", { "aria-pressed": String(l === level), onclick: (e) => { level = l; [...seg.children].forEach((b) => b.setAttribute("aria-pressed", b === e.currentTarget)); after = 0; box.replaceChildren(); load(); } }, l.toUpperCase())));
  const filter = h("input", { class: "input", type: "search", placeholder: "过滤关键字", oninput: () => { filterText = filter.value.toLowerCase(); [...box.children].forEach((c) => c.hidden = filterText && !c.textContent.toLowerCase().includes(filterText)); } });
  const pauseBtn = h("button", { class: "btn", onclick: () => { paused = !paused; pauseBtn.replaceChildren(icon(paused ? "play" : "pause"), paused ? "继续" : "暂停"); } }, icon("pause"), "暂停");
  main.replaceChildren(head("日志", "内存中最近 1000 条记录（敏感字段已脱敏），每 2 秒刷新"), adminGate() || "", h("div", { class: "toolbar" }, seg, filter, pauseBtn,
    h("button", { class: "btn", onclick: () => { const blob = new Blob([box.innerText], { type: "text/plain" }); h("a", { href: URL.createObjectURL(blob), download: `yimeng-core-${Date.now()}.log` }).click(); } }, icon("download"), "导出")), box);
  async function load() {
    if (paused) return;
    try {
      const r = await get(`/system/logs${qs({ level, after, limit: 500 })}`);
      if (!r.items.length) { if (!box.children.length) box.append(h("div", { class: "dim" }, "暂无日志")); return; }
      if (box.firstChild?.classList.contains("dim")) box.replaceChildren();
      const stick = box.scrollTop + box.clientHeight >= box.scrollHeight - 30;
      for (const e of r.items) { const l = logLine(e); if (filterText && !l.textContent.toLowerCase().includes(filterText)) l.hidden = true; box.append(l); }
      after = r.items[r.items.length - 1].seq;
      while (box.children.length > 2000) box.firstChild.remove();
      if (stick) box.scrollTop = box.scrollHeight;
    } catch (e) { if (!box.children.length) box.replaceChildren(h("div", { class: "dim" }, e.status === 403 ? "需要管理员权限" : e.message)); }
  }
  await load(); box.scrollTop = box.scrollHeight;
  const t = setInterval(load, 2000);
  cleanup(() => clearInterval(t));
}

// ── devices ──
export async function devices(main) {
  const [r, me] = await Promise.all([get("/devices"), get("/devices/me")]);
  const reload = () => devices(main);
  const newCode = async () => {
    try {
      const c = await post("/pair/codes");
      const left = h("span"); const exp = new Date(c.expires_at);
      const tick = setInterval(() => { const s = Math.max(0, Math.round((exp - Date.now()) / 1000)); left.textContent = `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`; if (!s) clearInterval(tick); }, 500);
      dialog({ title: "配对新设备", body: [h("p", { class: "muted", style: { margin: 0 } }, "在新设备（iOS / Android / 桌面 / 另一个浏览器）的配对界面输入下面的配对码："), h("div", { class: "pill-code" }, c.code), h("p", { class: "muted", style: { margin: 0, textAlign: "center" } }, "剩余有效时间 ", left)], onClose: () => { clearInterval(tick); reload(); } });
    } catch (e) { errToast(e); }
  };
  const TYPE = { web: "网页", ios: "iOS", android: "Android", desktop: "桌面", mac: "Mac", windows: "Windows", subsonic: "Subsonic" };
  main.replaceChildren(head("设备", `${r.items.length} 台已配对设备 · 配对令牌只保存在各设备本地`, h("button", { class: "btn primary", onclick: newCode }, icon("plus"), "配对新设备")), adminGate() || "",
    h("div", { class: "table-wrap card", style: { padding: "6px 10px" } }, h("table", { class: "table" },
      h("thead", null, h("tr", null, ["设备", "类型", "角色", "最近活跃", "配对时间", ""].map((x) => h("th", null, x)))),
      h("tbody", null, r.items.map((d) => h("tr", null,
        h("td", null, h("div", { class: "row" }, icon("device"), h("b", null, d.name), d.id === me.id ? h("span", { class: "badge ok" }, "本机") : null)),
        h("td", null, TYPE[d.device_type] || d.device_type || "—"),
        h("td", null, h("label", { class: "row", title: "管理员可修改设置与元数据" }, h("input", { type: "checkbox", class: "switch", checked: d.admin, disabled: !isAdmin(), "aria-label": "管理员", onchange: async (e) => {
          try { await patch(`/devices/${d.id}`, { admin: e.target.checked }); toast(e.target.checked ? `${d.name} 已设为管理员` : `${d.name} 已取消管理员`); } catch (err) { e.target.checked = !e.target.checked; err.code === "last_admin" ? toast("至少需要保留一台管理员设备", "err") : errToast(err); }
        } }), h("span", { class: "muted" }, d.admin ? "管理员" : "成员"))),
        h("td", { class: "muted" }, fmtAgo(d.last_seen_at)),
        h("td", { class: "muted" }, fmtTime(d.created_at)),
        h("td", null, h("div", { class: "row", style: { justifyContent: "flex-end" } },
          h("button", { class: "btn sm", onclick: async () => { const n = await promptDlg("重命名设备", "设备名称", d.name); if (n) { try { await patch(`/devices/${d.id}`, { name: n }); reload(); } catch (e) { errToast(e); } } } }, "重命名"),
          h("button", { class: "btn sm danger", onclick: async () => {
            if (!(await confirmDlg("撤销设备", d.id === me.id ? "这是当前浏览器。撤销后需要重新配对。" : `撤销「${d.name}」？该设备的令牌将立即失效。`, "撤销", true))) return;
            try { await del(`/devices/${d.id}`); toast("已撤销"); if (d.id === me.id) { store.token = ""; location.reload(); } else reload(); } catch (e) { e.code === "last_admin" ? toast("不能撤销最后一台管理员设备", "err") : errToast(e); }
          } }, "撤销")))))))));
}

// ── settings ──
export async function settings(main) {
  const admin = isAdmin();
  const sec = (title, sub, ...body) => h("section", { class: "card", style: { marginBottom: "16px" } }, h("div", { class: "section-head" }, h("div", { style: { flex: 1 } }, h("h2", null, title), sub ? h("div", { class: "muted", style: { fontSize: "13px", marginTop: "4px" } }, sub) : null)), ...body);
  const [rt, roots, split, sq, providers, sources, backups] = await Promise.all([
    admin ? get("/settings/runtime").catch(() => ({})) : {}, admin ? get("/settings/library-roots").catch(() => ({ roots: [] })) : { roots: [] }, get("/settings/artist-split").catch(() => null),
    get("/devices/me/stream-quality").catch(() => null), get("/metadata/providers").catch(() => ({ items: [] })), get("/sources").catch(() => ({ items: [] })),
    admin ? get("/system/backups").catch(() => ({ items: [] })) : { items: [] }]);
  const reload = () => settings(main);

  // roots
  const rootList = h("div", { class: "stack" });
  const addRoot = (v = "") => { const inp = h("input", { class: "input mono", value: v, placeholder: "/Volumes/Music 或 /music" }); const row = h("div", { class: "row" }, icon("folder"), inp, h("button", { class: "btn icon sm ghost", "aria-label": "删除目录", onclick: () => row.remove() }, icon("trash"))); row.inp = inp; rootList.append(row); };
  (roots.roots || []).forEach(addRoot);
  const rootsSec = sec("音乐目录", "Core 扫描的本地/NAS 目录（绝对路径）。环境变量 YIMENG_LIBRARY_ROOTS 设置的目录会在重启时覆盖这里。", rootList,
    h("div", { class: "row wrap", style: { marginTop: "12px" } }, h("button", { class: "btn sm", onclick: () => addRoot() }, icon("plus"), "添加目录"), h("span", { class: "spacer" }),
      h("button", { class: "btn sm", disabled: !admin, onclick: async () => { try { await post("/library/scan", { full: false }); toast("已开始扫描"); } catch (e) { e.status === 409 ? toast("扫描进行中") : errToast(e); } } }, icon("refresh"), "重新扫描"),
      h("button", { class: "btn primary sm", disabled: !admin, onclick: async () => {
        const list = [...rootList.children].map((r) => r.inp.value.trim()).filter(Boolean);
        try { await put("/settings/library-roots", { roots: list }); toast("目录已保存，开始扫描"); await post("/library/scan", {}).catch(() => {}); reload(); } catch (e) { errToast(e); }
      } }, "保存并扫描")));

  // watcher / scanning (runtime config, read-only)
  const ro = (k, v) => [k, v ?? "—"];
  const scanSec = sec("扫描与监听", "以下为 Core 启动时的配置（来自配置文件 / 环境变量），修改后需重启 Core 生效。", kv([
    ro("文件监听", h("span", null, bool(rt.watch_mode && rt.watch_mode !== "off", rt.watch_mode || "auto"), " 防抖 ", rt.watch_debounce)),
    ro("定时重扫", rt.rescan_interval === "0s" ? "关闭" : rt.rescan_interval), ro("启动时扫描", bool(rt.scan_on_start)),
    ro("响度分析", bool(rt.loudness_analysis)), ro("Subsonic API", bool(rt.subsonic)), ro("mDNS 发现", bool(rt.mdns)), ro("HTTPS", bool(rt.tls)),
    ro("配对码有效期", rt.pair_code_ttl), ro("日志", `${rt.log_level || "info"} · ${rt.log_format || "text"}`), ro("数据库", h("span", { class: "mono" }, rt.db_path)),
    ro("环境变量", h("span", { class: "mono dim" }, "YIMENG_WATCH · YIMENG_RESCAN_INTERVAL · YIMENG_SCAN_ON_START · YIMENG_LOUDNESS_ANALYSIS"))]));

  // providers
  const provSec = sec("元数据与歌词来源", "MusicBrainz 自动补全默认关闭（需要联系方式 User-Agent）。手动匹配（编辑器中的「在线匹配」：MusicBrainz + 已配置的 Spotify，及「歌词匹配」）按需调用，不会后台联网。", kv([
    ro("MusicBrainz 自动补全", h("span", null, bool(rt.musicbrainz_enabled), rt.musicbrainz_enabled ? "" : h("span", { class: "dim mono" }, "  YIMENG_MB_ENABLED=true + YIMENG_MB_CONTACT=邮箱"))),
    ro("MB 联系方式", rt.musicbrainz_contact_set ? "已设置" : "未设置"),
    ro("歌词提供方", (rt.providers || []).length ? h("div", { class: "chips" }, rt.providers.map((p) => h("span", { class: "badge ok" }, p))) : h("span", { class: "dim mono" }, "未启用（YIMENG_PROVIDERS=lrclib）")),
    ro("已注册提供方", (providers.items || []).length ? providers.items.map((p) => `${p.id}（${p.health}）`).join("、") : "—")]));

  // artist split
  let splitSec = "";
  if (split) {
    const amp = h("input", { type: "checkbox", class: "switch", checked: split.split_ampersand });
    const comma = h("input", { type: "checkbox", class: "switch", checked: split.split_comma });
    const extra = h("input", { class: "input", value: (split.extra_separators || []).join(" "), placeholder: "如：、 ／ feat." });
    const exc = h("textarea", { class: "input", rows: 3 }, (split.exceptions || []).join("\n"));
    splitSec = sec("多艺人拆分", "控制 “A & B”、“A, B” 等艺人字段如何拆分为多位艺人；保存后会重新合并曲库。",
      h("div", { class: "stack" }, h("label", { class: "row" }, amp, "按 & 拆分"), h("label", { class: "row" }, comma, "按逗号拆分"),
        h("label", { class: "field" }, h("span", null, "额外分隔符（空格分隔）"), extra), h("label", { class: "field" }, h("span", null, "例外（每行一个，不拆分）"), exc),
        h("div", { class: "row" }, h("span", { class: "spacer" }), h("button", { class: "btn primary sm", disabled: !admin, onclick: async () => {
          try { await put("/settings/artist-split", { split_ampersand: amp.checked, split_comma: comma.checked, extra_separators: extra.value.split(/\s+/).filter(Boolean), exceptions: exc.value.split("\n").map((s) => s.trim()).filter(Boolean) }); toast("已保存，曲库已重新合并"); } catch (e) { errToast(e); }
        } }, "保存"))));
  }

  // transcoding (per-device stream quality + capabilities)
  const sys = state.system || (await get("/system").catch(() => null));
  const fmts = sys?.transcode?.formats || [{ format: "original" }];
  const fmtSel = h("select", { class: "input", style: { width: "auto" } }, fmts.map((f) => h("option", { value: f.format, selected: sq?.format === f.format }, f.format === "original" ? "原始文件（不转码）" : `${f.format.toUpperCase()}${f.lossless ? "（无损）" : ""}`)));
  const brSel = h("select", { class: "input", style: { width: "auto" } });
  const syncBr = () => { const f = fmts.find((x) => x.format === fmtSel.value); brSel.replaceChildren(...(f?.bitrates || []).map((b) => h("option", { value: b, selected: (sq?.bitrate_kbps || f.default_bitrate) === b }, `${b} kbps`))); brSel.hidden = !f?.bitrates?.length; };
  fmtSel.onchange = syncBr; syncBr();
  const tcSec = sec("转码", `ffmpeg：${rt.ffmpeg ? "可用" : sys?.transcode?.available ? "可用" : "不可用"} · 并发 ${rt.transcode_concurrency ?? "—"} · 缓存上限 ${fmtBytes(rt.cache_max_bytes || sys?.transcode?.cache_max_bytes)}。网页播放器始终播放原始文件；以下是本设备的默认串流音质（供移动端/Subsonic 使用）。`,
    h("div", { class: "row wrap" }, h("span", { class: "muted" }, "本设备默认音质"), fmtSel, brSel, h("button", { class: "btn sm primary", onclick: async () => {
      try { await put("/devices/me/stream-quality", { format: fmtSel.value, bitrate_kbps: brSel.hidden ? null : +brSel.value }); toast("已保存"); } catch (e) { errToast(e); }
    } }, "保存")));

  // backups
  const bkList = h("div");
  const renderBk = (items) => bkList.replaceChildren(...(items.length ? items.map((b) => h("div", { class: "li" }, icon("disc"),
    h("div", { class: "grow" }, h("div", { class: "t ellipsis mono" }, b.name), h("div", { class: "s" }, `${fmtTime(b.created_at)} · ${fmtBytes(b.size)}${b.reason ? ` · ${b.reason}` : ""}`)),
    h("button", { class: "btn icon sm", "aria-label": "下载", onclick: () => download(`/system/backups/${encodeURIComponent(b.name)}`, b.name) }, icon("download")),
    h("button", { class: "btn icon sm danger", "aria-label": "删除", onclick: async () => { if (await confirmDlg("删除备份", b.name, "删除", true)) { try { await del(`/system/backups/${encodeURIComponent(b.name)}`); reload(); } catch (e) { errToast(e); } } } }, icon("trash"))))
    : [h("p", { class: "muted" }, "还没有备份")]));
  renderBk(backups.items || []);
  const restoreInput = h("input", { type: "file", accept: ".ymbak,application/octet-stream", hidden: true, onchange: async () => {
    const f = restoreInput.files[0]; if (!f) return;
    if (!(await confirmDlg("从备份恢复", `将使用「${f.name}」恢复数据库。恢复会在 Core 下次重启时生效，当前数据库会保留一份 *.pre-restore 副本。`, "暂存恢复", true))) return;
    try { const r = await post("/system/restore", f); toast(r.message || "已暂存，重启 Core 后生效", "ok", 6000); } catch (e) { errToast(e); }
  } });
  const bkSec = sec("备份与恢复", `自动备份间隔 ${rt.backup_interval || "—"}，保留 ${rt.backup_keep ?? "—"} 份 · 目录 ${rt.backup_dir || "—"}。备份包含数据库与密钥（Vault 凭据仍为加密状态）。`, bkList,
    h("div", { class: "row wrap", style: { marginTop: "12px" } },
      h("button", { class: "btn sm primary", disabled: !admin, onclick: async () => { try { await post("/system/backups", {}); toast("备份已创建"); reload(); } catch (e) { errToast(e); } } }, icon("plus"), "立即备份"),
      h("button", { class: "btn sm", disabled: !admin, onclick: () => download("/system/export", `yimeng-export-${Date.now()}.ymbak`) }, icon("download"), "导出"),
      h("button", { class: "btn sm danger", disabled: !admin, onclick: () => restoreInput.click() }, icon("upload"), "从文件恢复…"), restoreInput));

  // sources (Vault)
  const SRC = { netease: ["网易云音乐", "#e60026", "网"], qq: ["QQ 音乐", "#31c27c", "Q"], apple: ["Apple Music", "#fa2d48", ""] };
  const HEALTH = { ok: ["正常", "ok"], needs_reauth: ["未登录", ""], degraded: ["降级", "warn"], down: ["不可用", "err"] };
  const srcSec = sec("其他音源 (占位)", "Apple Music 需在 iOS / macOS 客户端授权；该音源当前为占位实现。",
    ...(sources.items || []).map((s) => {
      const [name, color, letter] = SRC[s.id] || [s.id, "#555", s.id[0]];
      const [hl, hc] = HEALTH[s.health] || [s.health, ""];
      return h("div", { class: "li" }, h("div", { class: "src-logo", style: { background: color } }, letter || ""),
        h("div", { class: "grow" }, h("div", { class: "t" }, name, " ", h("span", { class: `badge ${s.linked ? "ok" : hc}` }, s.linked ? "已登录" : hl)),
          h("div", { class: "s" }, s.linked ? `账号：${s.account?.display_name || "—"}` : `登录方式：${s.login_methods.map((m) => ({ cookie: "Cookie", qr: "扫码", oauth: "OAuth", password: "密码" }[m.method] || m.method)).join(" / ")}`)),
        s.linked ? [h("button", { class: "btn sm", disabled: !admin, onclick: async () => { try { await post(`/sources/${s.id}/sync`); toast("已同步"); } catch (e) { errToast(e); } } }, "同步"),
          h("button", { class: "btn sm danger", disabled: !admin, onclick: async () => { if (await confirmDlg("退出登录", `清除 ${name} 的凭据？`, "退出", true)) { try { await del(`/sources/${s.id}/credentials`); reload(); } catch (e) { errToast(e); } } } }, "退出")]
          : h("button", { class: "btn sm primary", disabled: !admin, onclick: () => sourceLogin(s, name, reload) }, icon("key"), "登录"));
    }));

  // appearance
  const theme = localStorage.getItem("yimeng.theme") || "dark";
  const themeSeg = h("div", { class: "seg" }, [["dark", "深色"], ["light", "浅色"], ["auto", "跟随系统"]].map(([v, l]) => h("button", { "aria-pressed": String(v === theme), onclick: (e) => {
    localStorage.setItem("yimeng.theme", v); applyTheme(); [...themeSeg.children].forEach((b) => b.setAttribute("aria-pressed", b === e.currentTarget));
  } }, l)));
  const lookSec = sec("外观", "与 iOS 端一致的深色主题为默认。", h("div", { class: "row" }, h("span", { class: "muted" }, "主题"), themeSeg));

  const modeSeg = h("div", { class: "seg" }, [["local", "本地曲库"], ["online", "在线音源"], ["both", "两者"]].map(([v, l]) => h("button", { "aria-pressed": String(v === (state.mode?.mode || "")), disabled: !admin, onclick: async () => {
    try { state.mode = await put("/settings/mode", { mode: v }); toast("已切换使用方式"); location.reload(); } catch (e) { errToast(e); }
  } }, l)));
  const modeSec = sec("使用方式", "仅在线模式下会隐藏本地曲库相关页面；本地曲库为空时也会自动隐藏。", h("div", { class: "row" }, modeSeg));
  const plugSec = pluginsSection(sec, reload);
  const local = !localHidden();
  main.replaceChildren(head("设置"), adminGate() || "", modeSec, plugSec, ...(local ? [rootsSec, scanSec] : []), provSec, ...(local ? [splitSec] : []), tcSec, bkSec, srcSec, lookSec);
}
export function applyTheme() {
  const t = localStorage.getItem("yimeng.theme") || "dark";
  const light = t === "light" || (t === "auto" && matchMedia("(prefers-color-scheme: light)").matches);
  document.documentElement.dataset.theme = light ? "light" : "dark";
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", light ? "#ffffff" : "#0A0A0C");
}

async function download(path, name) {
  try { const r = await api(path, { raw: true }); const blob = await r.blob(); h("a", { href: URL.createObjectURL(blob), download: name }).click(); } catch (e) { errToast(e); }
}

function sourceLogin(s, name, done) {
  const methods = s.login_methods.map((m) => m.method);
  const body = h("div", { class: "stack" });
  const d = dialog({ title: `登录 ${name}`, body, onClose: () => { clearInterval(poll); done(); } });
  let poll;
  const show = (m) => {
    clearInterval(poll);
    if (m === "cookie") {
      const ta = h("textarea", { class: "input", rows: 4, placeholder: "粘贴浏览器中的 Cookie（例如 MUSIC_U=…）", autocomplete: "off" });
      body.replaceChildren(h("p", { class: "muted", style: { margin: 0 } }, "Cookie 直接提交到 Core 并加密保存，网页不会保留。"), ta,
        h("button", { class: "btn primary", onclick: async () => { try { await post(`/sources/${s.id}/login`, { method: "cookie", cookie: ta.value.trim() }); toast("登录成功"); d.close(); } catch (e) { e.status === 501 ? toast("该音源暂未实现（占位）", "err") : errToast(e); } } }, "提交"));
    } else if (m === "qr") {
      body.replaceChildren(h("div", { class: "skel", style: { width: "200px", height: "200px", justifySelf: "center" } }));
      post(`/sources/${s.id}/login/qr`).then((c) => {
        const st = h("p", { class: "muted", style: { textAlign: "center", margin: 0 } }, "请使用手机 App 扫码");
        body.replaceChildren(h("img", { class: "qr", src: c.image_url, alt: "登录二维码" }), st);
        poll = setInterval(async () => { try { const r = await get(`/sources/${s.id}/login/qr/${c.id}`); st.textContent = { pending: "等待扫码…", scanned: "已扫码，请在手机上确认", confirmed: "登录成功", expired: "二维码已过期" }[r.status] || r.status; if (r.status === "confirmed") { clearInterval(poll); toast("登录成功"); d.close(); } if (r.status === "expired") clearInterval(poll); } catch { clearInterval(poll); } }, 2000);
      }).catch((e) => body.replaceChildren(h("p", { class: "muted" }, e.status === 501 ? "该音源的扫码登录暂未实现（占位）。" : e.message)));
    } else if (m === "oauth") {
      body.replaceChildren(h("p", { class: "muted" }, "Apple Music 需要 MusicKit 开发者令牌与用户授权，请在 iOS / macOS 客户端完成授权，令牌会同步到 Core 的 Vault。"));
    }
  };
  if (methods.length > 1) d.bodyEl.before(h("div", { class: "tabs" }, methods.map((m, i) => h("button", { "aria-selected": i === 0 ? "true" : "false", onclick: (e) => { [...e.currentTarget.parentNode.children].forEach((b) => b.setAttribute("aria-selected", b === e.currentTarget)); show(m); } }, { cookie: "Cookie", qr: "扫码登录", oauth: "OAuth" }[m] || m))));
  show(methods[0]);
}
