// Listening report (D55, per account) and Last.fm settings (D56).
import { h, icon, toast, errToast, confirmDlg, dialog, fmtNum } from "./ui.js";
import { get, put, post, del, qs } from "./api.js";

const TZ = Intl.DateTimeFormat().resolvedOptions().timeZone || "Asia/Shanghai";
const WD = ["一", "二", "三", "四", "五", "六", "日"];
const hrs = (ms) => (ms / 3600000).toFixed(ms >= 36000000 ? 0 : 1);

function bars(values, labels, label) {
  const max = Math.max(1, ...values);
  return h("div", { class: "rep-bars", role: "img", "aria-label": label },
    values.map((v, i) => h("div", { class: "rep-bar", title: `${labels[i]}：${v} 次` },
      h("i", { style: { height: `${(v / max) * 100}%` } }), h("span", null, labels[i]))));
}

function topList(title, items, sub) {
  return h("div", { class: "card" }, h("h3", null, title),
    items.length ? h("ol", { class: "rep-top" }, items.map((e, i) => h("li", { class: "li" }, h("span", { class: "badge" }, String(i + 1)),
      h("div", { class: "grow" }, h("div", { class: "t ellipsis" }, e.name || "未知"), sub && e.artist_name ? h("div", { class: "s ellipsis" }, e.artist_name) : null),
      h("span", { class: "muted mono" }, `${e.plays} 次`))))
      : h("p", { class: "muted" }, "暂无数据"));
}

export async function reportPage(main) {
  const now = new Date();
  let period = "month", year = now.getFullYear(), month = now.getMonth() + 1;
  const body = h("div");
  const title = h("b", { style: { minWidth: "8em", textAlign: "center" } });
  const seg = h("div", { class: "seg" }, [["month", "月度"], ["year", "年度"]].map(([v, l]) =>
    h("button", { "aria-pressed": String(v === period), onclick: (e) => { period = v; [...seg.children].forEach((b) => b.setAttribute("aria-pressed", b === e.currentTarget)); load(); } }, l)));
  const step = (d) => {
    if (period === "year") year += d;
    else { month += d; if (month < 1) { month = 12; year--; } if (month > 12) { month = 1; year++; } }
    load();
  };
  async function load() {
    title.textContent = period === "year" ? `${year} 年` : `${year} 年 ${month} 月`;
    body.replaceChildren(h("p", { class: "muted" }, "加载中…"));
    try {
      const r = await get(`/me/report${qs({ period, year, month, tz: TZ })}`);
      if (!r.plays) { body.replaceChildren(h("div", { class: "card" }, h("p", { class: "muted" }, "这段时间还没有播放记录。"))); return; }
      const dayLabels = period === "year" ? r.by_day.map((_, i) => `${i + 1}月`) : r.by_day.map((_, i) => String(i + 1));
      body.replaceChildren(
        h("div", { class: "stats" },
          h("div", { class: "stat accent" }, h("div", { class: "k" }, "收听时长"), h("div", { class: "v" }, hrs(r.listened_ms), h("small", null, "小时"))),
          h("div", { class: "stat" }, h("div", { class: "k" }, "播放次数"), h("div", { class: "v" }, fmtNum(r.plays))),
          h("div", { class: "stat" }, h("div", { class: "k" }, "不同曲目"), h("div", { class: "v" }, fmtNum(r.unique_tracks))),
          h("div", { class: "stat" }, h("div", { class: "k" }, "艺人"), h("div", { class: "v" }, fmtNum(r.unique_artists))),
          h("div", { class: "stat" }, h("div", { class: "k" }, "专辑"), h("div", { class: "v" }, fmtNum(r.unique_albums))),
          h("div", { class: "stat" }, h("div", { class: "k" }, "活跃天数"), h("div", { class: "v" }, r.active_days, h("small", null, "天")))),
        h("div", { class: "grid-cards section", style: { marginTop: "18px" } },
          topList("最常听的歌曲", r.top_tracks, true), topList("最常听的艺人", r.top_artists), topList("最常听的专辑", r.top_albums, true)),
        h("div", { class: "grid-cards section", style: { marginTop: "18px" } },
          h("div", { class: "card" }, h("h3", null, "按时段"), bars(r.by_hour, r.by_hour.map((_, i) => String(i)), "每小时播放次数")),
          h("div", { class: "card" }, h("h3", null, "按星期"), bars(r.by_weekday, WD.map((d) => `周${d}`), "每周各天播放次数")),
          h("div", { class: "card" }, h("h3", null, period === "year" ? "按月" : "按日"), bars(r.by_day, dayLabels, "播放分布"))));
    } catch (e) { errToast(e); body.replaceChildren(); }
  }
  main.replaceChildren(h("div", { class: "page-head" }, h("div", { style: { flex: 1 } }, h("h1", null, "听歌报告"), h("div", { class: "sub" }, `只统计你自己账号的播放记录 · 时区 ${TZ}`)),
    seg, h("button", { class: "btn icon sm", "aria-label": "上一段", onclick: () => step(-1) }, icon("chevL")), title, h("button", { class: "btn icon sm", "aria-label": "下一段", onclick: () => step(1) }, icon("chevR"))), body);
  load();
}

// Last.fm section for settings (per account; keys never shown again).
export async function lastfmSec(sec) {
  let st = await get("/me/lastfm").catch((e) => (e.status === 403 ? "noacct" : null));
  if (!st) return "";
  const box = h("div", { class: "stack" });
  const desc = "可选。使用你自己在 last.fm/api 申请的 API Key 与 Secret；它们只加密保存在 Core 的 Vault 中，保存后不会再显示。只同步你账号的播放（播放过半或 4 分钟、且曲长 ≥30 秒）。";
  if (st === "noacct") return sec("Last.fm", desc, h("p", { class: "muted" }, "请使用账号登录后再连接 Last.fm。"));
  const refresh = async () => { st = await get("/me/lastfm"); draw(); };
  const keysDlg = () => {
    const k = h("input", { class: "input", type: "password", autocomplete: "off", placeholder: "API Key（32 位）" });
    const s = h("input", { class: "input", type: "password", autocomplete: "off", placeholder: "Shared Secret（32 位）" });
    const d = dialog({ title: "设置 Last.fm API 凭据", body: h("div", { class: "stack" }, k, s, h("div", { class: "muted", style: { fontSize: "13px" } }, "在 last.fm/api/account/create 免费创建。")),
      foot: [h("button", { class: "btn", onclick: () => d.close() }, "取消"), h("button", { class: "btn primary", onclick: async () => {
        try { st = await put("/me/lastfm/keys", { api_key: k.value.trim(), api_secret: s.value.trim() }); k.value = s.value = ""; d.close(); toast("已保存"); draw(); } catch (e) { errToast(e); }
      } }, "保存")] });
  };
  const authorize = async () => {
    try {
      const { auth_url } = await post("/me/lastfm/auth");
      window.open(auth_url, "_blank", "noopener");
      const d = dialog({ title: "授权 Last.fm", body: h("p", null, "已在新标签页打开 Last.fm。点“Yes, allow access”授权后，回到这里点“我已授权”。"),
        foot: [h("button", { class: "btn", onclick: () => d.close() }, "取消"), h("button", { class: "btn primary", onclick: async () => {
          try { st = await post("/me/lastfm/auth/complete"); d.close(); toast(`已连接 ${st.username}`); draw(); } catch (e) { e.status === 409 ? toast("还没有完成授权", "err") : errToast(e); }
        } }, "我已授权")] });
    } catch (e) { errToast(e); }
  };
  function draw() {
    const sw = h("input", { type: "checkbox", class: "switch", checked: st.enabled, disabled: !st.connected, "aria-label": "同步到 Last.fm",
      onchange: async () => { try { st = await put("/me/lastfm", { enabled: sw.checked }); toast("已保存"); } catch (e) { errToast(e); sw.checked = !sw.checked; } } });
    box.replaceChildren(
      h("div", { class: "li" }, h("div", { class: "grow" }, h("div", { class: "t" }, st.connected ? `已连接：${st.username}` : st.configured ? "已保存凭据，尚未授权" : "未设置"),
        h("div", { class: "s" }, st.connected ? `待同步 ${st.pending} 条` : "")), st.connected ? sw : null),
      h("div", { class: "row wrap" },
        h("button", { class: "btn sm", onclick: keysDlg }, icon("key"), st.configured ? "更换凭据" : "设置凭据"),
        st.configured ? h("button", { class: "btn sm primary", onclick: authorize }, st.connected ? "重新授权" : "授权") : null,
        st.configured ? h("button", { class: "btn sm danger", onclick: async () => {
          if (!(await confirmDlg("断开 Last.fm", "将删除保存的凭据、会话和待同步记录。", "断开", true))) return;
          try { await del("/me/lastfm"); await refresh(); toast("已断开"); } catch (e) { errToast(e); }
        } }, "断开") : null));
  }
  draw();
  return sec("Last.fm", desc, box);
}
