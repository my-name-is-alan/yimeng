// Sleep timer + fade-out for the web player (D52). The timer lives in Core
// on this device's player, so apps see/change it; the fade is applied here.
import { get, post, onEvent } from "./api.js";
import { player } from "./player.js";
import { h, icon, menu, toast, errToast } from "./ui.js";

let me = null, info = null, fadeStart = 0, fadeLen = 0, tick = null;
const listeners = new Set();
const emit = () => listeners.forEach((f) => f(info));
export const onSleep = (f) => { listeners.add(f); return () => listeners.delete(f); };

async function myId() { if (!me) me = (await get("/devices/me")).id; return me; }

function reset() { info = null; fadeLen = 0; player.fade = 1; player.applyVol(); clearInterval(tick); tick = null; emit(); }

function sync(sleep) {
  info = sleep || null;
  if (!info) return reset();
  fadeLen = 0; player.fade = 1; player.applyVol();
  clearInterval(tick); tick = setInterval(step, 250);
  emit();
}

// remaining ms until the player should stop
function remaining() {
  if (!info) return Infinity;
  if (info.end_of_track) return player.playing ? Math.max(0, player.duration - player.time) : Infinity;
  return new Date(info.until).getTime() - Date.now();
}

function step() {
  if (!info) return;
  const rem = remaining(), fade = info.fade_ms || 0;
  if (!info.end_of_track && rem <= 0) { player.pause(); toast("睡眠定时已到，已暂停播放"); return reset(); }
  player.fade = fade > 0 && rem < fade ? Math.max(0, rem / fade) : 1;
  player.applyVol();
  emit();
}

// end of track: stop instead of advancing
player.sleepHook = () => {
  if (!info?.end_of_track) return false;
  arm({}).catch(() => {});
  reset(); toast("本首播放完毕，已停止播放");
  return true;
};

export async function arm(body) {
  const st = await post(`/players/${await myId()}/sleep`, body);
  sync(st.sleep);
  return st;
}

export function initSleep() {
  myId().then(async (id) => { const st = await get(`/players/${id}`).catch(() => null); if (st?.sleep) sync(st.sleep); }).catch(() => {});
  onEvent("player.state", (st) => { if (me && st?.player_id === me) sync(st.sleep); });
  onEvent("player.sleep", (e) => { if (me && e?.player_id === me && info) { player.pause(); reset(); } });
}

export const sleepLabel = () => {
  if (!info) return "";
  if (info.end_of_track) return "本首结束";
  const m = Math.ceil(remaining() / 60000);
  return m > 0 ? `${m} 分钟` : "即将停止";
};

export function sleepButton() {
  const label = h("span", { class: "sleep-left" });
  const btn = h("button", { class: "btn icon ghost sleep-btn", "aria-label": "睡眠定时", title: "睡眠定时（结束前 30 秒渐弱）", onclick: () => {
    const set = (body, msg) => arm(body).then(() => toast(msg)).catch(errToast);
    menu(btn, [
      ...[15, 30, 45, 60, 90].map((m) => ({ label: `${m} 分钟后停止`, icon: "clock", run: () => set({ minutes: m }, `将在 ${m} 分钟后停止播放`) })),
      { label: "播放完本首后停止", icon: "clock", run: () => set({ end_of_track: true }, "将在本首结束后停止") },
      info ? "-" : null,
      info ? { label: "关闭睡眠定时", icon: "close", run: () => set({}, "已关闭睡眠定时") } : null,
    ]);
  } }, icon("clock"), label);
  const upd = () => { label.textContent = sleepLabel(); btn.classList.toggle("on", !!info); };
  onSleep(upd); upd();
  return btn;
}
