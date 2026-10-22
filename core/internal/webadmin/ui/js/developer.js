// 开发者 (D59): personal API keys (/api/v1, scoped) and Subsonic app passwords (/rest).
import { h, icon, toast, errToast, confirmDlg, dialog, fmtTime, fmtAgo, empty } from "./ui.js";
import { get, post, del } from "./api.js";
import { isAdmin } from "./state.js";

const SCOPE = { read: "读取", playback: "播放", "library-write": "曲库写入", admin: "管理" };
const SCOPE_TIP = { read: "所有 GET 接口（始终包含）", playback: "播放器、队列、串流、记录播放", "library-write": "歌单、收藏、元数据等写操作", admin: "用户、设置、系统（仅管理员账号）" };
const head = (title, sub, ...actions) => h("div", { class: "page-head" }, h("div", { style: { flex: 1, minWidth: 0 } }, h("h1", null, title), sub ? h("div", { class: "sub" }, sub) : null), ...actions);

function seg(options, value, onChange) {
  const el = h("div", { class: "seg" }, options.map(([v, l]) => h("button", { type: "button", "aria-pressed": String(v === value), onclick: (e) => {
    [...el.children].forEach((b) => b.setAttribute("aria-pressed", String(b === e.currentTarget))); onChange(v);
  } }, l)));
  return el;
}

function createDlg(kind, onDone) {
  let scopes = new Set(kind === "api_key" ? ["read"] : ["read", "playback", "library-write"]);
  let days = 0, plain = false;
  const name = h("input", { class: "input", maxlength: 64, placeholder: kind === "api_key" ? "例如：我的脚本 / 第三方 App" : "例如：Symfonium / DSub", autocomplete: "off" });
  const chips = h("div", { class: "chips" }, Object.keys(SCOPE).filter((s) => s !== "admin" || isAdmin()).map((s) => h("button", {
    type: "button", class: "chip", "aria-pressed": String(scopes.has(s)), title: SCOPE_TIP[s], disabled: s === "read",
    onclick: (e) => { scopes.has(s) ? scopes.delete(s) : scopes.add(s); e.currentTarget.setAttribute("aria-pressed", String(scopes.has(s))); },
  }, SCOPE[s])));
  const plainSw = h("input", { type: "checkbox", class: "switch", "aria-label": "允许明文密码", onchange: (e) => { plain = e.target.checked; } });
  const d = dialog({ title: kind === "api_key" ? "新建 API 密钥" : "新建 Subsonic 应用密码", body: [
    h("label", { class: "field" }, h("span", null, "名称"), name),
    h("div", { class: "field" }, h("span", null, "权限范围"), chips),
    h("div", { class: "field" }, h("span", null, "有效期"), seg([[0, "永久"], [30, "30 天"], [90, "90 天"], [365, "1 年"]], 0, (v) => { days = v; })),
    kind === "app_password" ? h("label", { class: "row", style: { gap: "10px" } }, plainSw, h("div", null, h("div", null, "允许明文密码 (p=)"), h("div", { class: "muted", style: { fontSize: "12px" } }, "仅为不支持 token 认证的老客户端开启；默认关闭"))) : null,
    h("p", { class: "muted", style: { margin: 0 } }, kind === "api_key"
      ? "用于 /api/v1：请求头 Authorization: Bearer yk_…。也可作为 Subsonic 的 apiKey。"
      : "用于 Subsonic 客户端：用户名填你的账号，密码填生成的应用密码（不是登录密码）。随时可撤销。")],
  foot: [h("button", { class: "btn", onclick: () => d.close() }, "取消"), h("button", { class: "btn primary", onclick: async () => {
    if (!name.value.trim()) return toast("请填写名称", "err");
    try {
      const r = await post("/developer/credentials", { kind, name: name.value.trim(), scopes: [...scopes], allow_plain: plain, expires_in_days: days });
      d.close(); showSecret(r, onDone);
    } catch (e) { errToast(e); }
  } }, "创建")] });
  setTimeout(() => name.focus(), 50);
}

function showSecret(r, onDone) {
  const copy = async () => { try { await navigator.clipboard.writeText(r.secret); toast("已复制"); } catch { toast("复制失败，请手动选择", "err"); } };
  dialog({ title: "请立即保存", body: [
    h("p", { class: "muted", style: { margin: 0 } }, "密钥只显示这一次，关闭后无法再次查看。"),
    h("div", { class: "pill-code", style: { fontSize: "15px", letterSpacing: ".04em", wordBreak: "break-all", userSelect: "all" } }, r.secret),
    h("div", { class: "row", style: { justifyContent: "center" } }, h("button", { class: "btn primary", onclick: copy }, icon("key"), "复制"))],
  onClose: onDone });
}

export async function developerPage(main) {
  const reload = () => developerPage(main);
  let r;
  try { r = await get("/developer/credentials"); } catch (e) { errToast(e); return main.replaceChildren(head("开发者"), empty("无法加载", "key")); }
  const items = r.items || [];
  const row = (c) => h("tr", null,
    h("td", null, h("div", { class: "row" }, icon("key"), h("b", null, c.name), h("span", { class: "muted mono" }, `${c.prefix}…`))),
    h("td", null, h("span", { class: "badge" }, c.kind === "api_key" ? "API 密钥" : "应用密码"), c.allow_plain ? h("span", { class: "badge", title: "允许明文 p=" }, "明文") : null),
    h("td", null, h("div", { class: "chips" }, (c.scopes || []).map((s) => h("span", { class: `badge ${s === "admin" ? "ok" : ""}`, title: SCOPE_TIP[s] }, SCOPE[s] || s)))),
    h("td", { class: "muted" }, c.last_used_at ? fmtAgo(c.last_used_at) : "从未"),
    h("td", { class: "muted" }, c.expires_at ? fmtTime(c.expires_at) : "永久"),
    h("td", null, h("div", { class: "row", style: { justifyContent: "flex-end" } }, h("button", { class: "btn sm danger", onclick: async () => {
      if (!(await confirmDlg("撤销凭据", `撤销「${c.name}」？使用它的应用会立即失去访问权限。`, "撤销", true))) return;
      try { await del(`/developer/credentials/${c.id}`); toast("已撤销"); reload(); } catch (e) { errToast(e); }
    } }, "撤销"))));
  main.replaceChildren(
    head("开发者", "让第三方 App 与脚本接入：标准 OpenSubsonic 接口（/rest）与开放 API（/api/v1）。凭据只属于你的账号。",
      h("button", { class: "btn", onclick: () => createDlg("app_password", reload) }, icon("plus"), "Subsonic 应用密码"),
      h("button", { class: "btn primary", onclick: () => createDlg("api_key", reload) }, icon("plus"), "API 密钥")),
    items.length ? h("div", { class: "table-wrap card", style: { padding: "6px 10px" } }, h("table", { class: "table" },
      h("thead", null, h("tr", null, ["名称", "类型", "权限", "最近使用", "到期", ""].map((x) => h("th", null, x)))),
      h("tbody", null, items.map(row)))) : empty("还没有凭据。创建一个 API 密钥或 Subsonic 应用密码开始接入。", "key"),
    h("div", { class: "card" }, h("h3", null, "接入说明"),
      h("ul", { class: "muted", style: { margin: 0, paddingLeft: "18px", lineHeight: 1.8 } },
        h("li", null, "Subsonic 客户端：服务器地址填 ", h("span", { class: "mono" }, location.origin), "，用户名为你的账号，密码填应用密码；支持 OpenSubsonic apiKey。"),
        h("li", null, "开放 API：", h("span", { class: "mono" }, "Authorization: Bearer yk_…"), "，接口文档见仓库中的 core/api/openapi.yaml 与 core/docs/developer-api.md。"),
        h("li", null, "仅试听或不可播放的在线曲目不会通过 Subsonic 提供；不做任何 DRM 绕过。"))));
}
