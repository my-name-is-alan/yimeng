// Per-account playback preferences (source switching, D50).
import { h, icon, toast, errToast, originName } from "./ui.js";
import { get, put } from "./api.js";

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
