import { get, post, del } from "./api.js";
import { h, icon, toast, errToast, confirmDlg, dialog } from "./ui.js";
import { isAdmin } from "./state.js";

// A grant authorizes audio only. Personal playlists/recommendations never use it.
export async function grantStreamAccount(plugin, account, done) {
  try {
    const { items = [] } = await get("/users");
    const members = items.filter((u) => u.role === "user");
    if (!members.length) { toast("请先在用户页面创建一个成员账号", "err"); return; }
    const user = h("select", { class: "input", "aria-label": "获授权成员" }, members.map((u) => h("option", { value: u.id }, u.username)));
    const label = h("input", { class: "input", value: `${plugin.manifest.name} · 管理员共享`, maxlength: 80, "aria-label": "共享来源名称" });
    const error = h("p", { class: "muted", role: "alert" });
    const save = h("button", { class: "btn primary", onclick: async () => {
      save.disabled = true; error.textContent = "";
      try {
        await post("/admin/stream-grants", { consumer_user_id: user.value, platform: plugin.manifest.id, account_id: account.id, label: label.value.trim() });
        modal.close(); toast("取流授权已保存；成员的个人数据仍使用其自己的账号", "ok"); done?.();
      } catch (e) { error.textContent = e.message; } finally { save.disabled = false; }
    } }, "授权取流");
    const modal = dialog({ title: "授权指定账号取流", body: [
      h("p", { class: "muted" }, `出借账号：${account.nickname || account.id}。授权仅用于播放，不开放你的收藏、歌单、推荐或登录凭据。`),
      h("label", null, "成员账号", user), h("label", null, "成员看到的播放来源名称", label),
      h("p", { class: "muted" }, "授权绑定这个具体账号，不随你的默认账号切换。平台仍可能根据取流行为更新该账号画像。"), error
    ], foot: [h("button", { class: "btn", onclick: () => modal.close() }, "取消"), save] });
  } catch (e) { errToast(e); }
}

export function sharedStreamsSection(sec) {
  const box = h("div", { class: "stack", "aria-live": "polite" });
  const section = sec("播放来源与共享授权", "收藏、歌单、推荐、FM 和反馈使用你自己的平台账号；成员播放固定使用管理员指定授权的账号，不自动换号。", box);
  async function render() {
    try {
      const b = await get("/me/platform-bindings");
      const admin = isAdmin();
      const users = admin ? await get("/users") : { items: [] };
      const names = new Map((users.items || []).map((u) => [u.id, u.username]));
      const note = h("p", { class: "muted" }, admin
        ? "在下方音源插件的「账号」列表中选择具体账号 →「授权取流」。默认不共享；你自己的播放使用个人默认账号。"
        : (b.grants || []).some((g) => !g.revoked) ? "播放固定使用下方已授权来源；你的收藏、推荐与反馈仍属于自己的账号。" : "没有共享授权时无法播放在线歌曲；登录自己的平台账号仍可获取自己的收藏与推荐。请联系管理员授权。");
      const rows = (b.grants || []).map((g) => h("div", { class: "li", "data-grant": g.id },
        h("div", { class: "grow" }, h("div", { class: "t" }, g.label, " ", h("span", { class: `badge ${g.revoked || g.available === false ? "err" : "ok"}` }, g.revoked ? "已撤销" : g.unavailable_reason === "shared_account_needs_login" ? "请管理员重新登录" : g.available === false ? "暂不可用" : "固定取流来源")),
          h("div", { class: "s" }, admin ? `${names.get(g.consumer_user_id) || "成员"} · ${g.platform} · 指定账号 ${g.account_id}` : `${g.platform} · 仅播放授权，不包含出借者私人数据`)),
        admin && !g.revoked ? h("button", { class: "btn sm danger", onclick: async () => {
          if (!await confirmDlg("撤销取流授权", "撤销后旧链接、预取和新的播放请求都会失效，正在传输的音频会被中止。", "撤销", true)) return;
          try { await del(`/admin/stream-grants/${encodeURIComponent(g.id)}`); toast("授权已撤销"); await render(); } catch (e) { errToast(e); }
        } }, "撤销") : null));
      const claim = admin && b.legacy_claim_required ? h("div", { class: "stack" },
        h("p", { class: "muted" }, "旧服务器账号尚未归属。多管理员环境不会自动分配；只有原账号所有者应认领。认领不会开启共享。"),
        h("button", { class: "btn", onclick: async () => {
          if (!await confirmDlg("认领旧平台账号", "确认旧服务器保存的平台账号属于你？这些账号会迁移到你的个人名下，不会自动共享。", "确认认领")) return;
          try { await post("/admin/platform-legacy/claim"); location.reload(); } catch (e) { errToast(e); }
        } }, "认领旧平台账号")) : null;
      box.replaceChildren(note, ...rows, ...(claim ? [claim] : []), h("button", { class: "btn sm", onclick: render }, icon("refresh"), "刷新授权状态"));
    } catch (e) { box.replaceChildren(h("p", { role: "alert", class: "muted" }, e.message), h("button", { class: "btn sm", onclick: render }, "重试")); }
  }
  render(); return section;
}
