// Per-account playback preferences (source switching, D50).
import { h, icon, toast, errToast, originName } from "./ui.js";
import { get, put } from "./api.js";
import { prefs as pp, loadPlaybackPrefs } from "./player.js";
import { setRangeFill } from "./ui.js";

export async function sourceSwitchSec(sec) {
  const st = await get("/me/source-switch").catch(() => null);
  if (!st) return "";
  let order = [...st.priority, ...st.platforms.filter((p) => !st.priority.includes(p))];
  const on = h("input", { type: "checkbox", class: "switch", checked: st.enabled, "aria-label": "自动切换音源" });
  const list = h("div", { class: "stack" });
  const save = async () => {
    try { await put("/me/source-switch", { enabled: on.checked, priority: order }); toast("已保存"); } catch (e) { errToast(e); }
  };
  const move = (i, d) => { const j = i + d; if (j < 0 || j >= order.length) return; [order[i], order[j]] = [order[j], order[i]]; draw(); save(); };
  const draw = () => list.replaceChildren(...(order.length ? order.map((p, i) => h("div", { class: "li" },
    h("span", { class: "badge" }, String(i + 1)), h("div", { class: "grow" }, h("div", { class: "t" }, originName(p))),
    h("button", { class: "btn icon sm ghost", "aria-label": `上移 ${originName(p)}`, disabled: i === 0, onclick: () => move(i, -1) }, icon("up")),
    h("button", { class: "btn icon sm ghost", "aria-label": `下移 ${originName(p)}`, disabled: i === order.length - 1, onclick: () => move(i, 1) }, icon("down"))))
    : [h("p", { class: "muted" }, "还没有启用的在线平台")]));
  on.onchange = save;
  draw();
  return sec("自动切换音源", "曲目在原平台仅能试听或已下架时，自动改用其他已登录且有完整播放权限的平台上的同一首歌（标题、艺人一致，时长相差约 3 秒内，或 ISRC 相同）。只使用正规授权来源。此设置仅对你的账号生效。",
    h("div", { class: "stack" }, h("label", { class: "row" }, on, "启用自动切换"), h("div", { class: "muted", style: { fontSize: "13px" } }, "平台优先级（越靠前越优先）"), list));
}

// ReplayGain + gapless (D53), per account; applied by the web player and apps.
export async function playbackSec(sec) {
  const st = await get("/me/playback").catch(() => null);
  if (!st) return "";
  const save = async (patch) => { try { Object.assign(pp, await put("/me/playback", patch)); loadPlaybackPrefs(); toast("已保存"); } catch (e) { errToast(e); } };
  const mode = h("select", { class: "input", style: { width: "auto" }, "aria-label": "音量标准化", onchange: () => save({ replaygain: mode.value }) },
    [["track", "按单曲（Track）"], ["album", "按专辑（Album）"], ["off", "关闭"]].map(([v, l]) => h("option", { value: v, selected: st.replaygain === v }, l)));
  const pre = h("input", { type: "range", class: "range", min: -15, max: 15, step: 0.5, value: st.preamp_db, "aria-label": "前置增益", style: { flex: 1, maxWidth: "240px" } });
  const preVal = h("span", { class: "mono", style: { minWidth: "56px" } }, `${st.preamp_db} dB`);
  pre.addEventListener("input", () => { preVal.textContent = `${pre.value} dB`; setRangeFill(pre); });
  pre.addEventListener("change", () => save({ preamp_db: +pre.value }));
  setRangeFill(pre);
  const clip = h("input", { type: "checkbox", class: "switch", checked: st.prevent_clipping, "aria-label": "防止削波", onchange: () => save({ prevent_clipping: clip.checked }) });
  const gap = h("input", { type: "checkbox", class: "switch", checked: st.gapless, "aria-label": "无缝播放", onchange: () => save({ gapless: gap.checked }) });
  return sec("播放", "音量标准化使用曲目的 ReplayGain（标签，或在扫描时用 ffmpeg EBU R128 分析得到）。网页播放器只能降低音量，正增益不会放大。无缝播放会提前缓冲下一首。设置仅对你的账号生效，App 也会读取。",
    h("div", { class: "stack" },
      h("div", { class: "row wrap" }, h("span", { class: "muted" }, "音量标准化"), mode),
      h("div", { class: "row" }, h("span", { class: "muted" }, "前置增益"), pre, preVal),
      h("label", { class: "row" }, clip, "防止削波（按峰值限制增益）"),
      h("label", { class: "row" }, gap, "无缝播放（预加载下一首）")));
}
