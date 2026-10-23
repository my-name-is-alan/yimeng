// Run: node --experimental-vm-modules --test internal/webadmin/tests/account-isolation.test.mjs
import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import vm from "node:vm";

const source = (name) => readFile(new URL(`../ui/js/${name}`, import.meta.url), "utf8");
const storage = () => {
  const data = new Map();
  return { getItem: (k) => data.get(k) ?? null, setItem: (k, v) => data.set(k, v), removeItem: (k) => data.delete(k) };
};

test("platform login expiry does not sign the user out of Core", async () => {
  let code = "plugin_login_required";
  const context = vm.createContext({ localStorage: storage(), Blob, ArrayBuffer, URLSearchParams,
    fetch: async () => new Response(JSON.stringify({ code, error: code, message: code }), { status: 401, headers: { "Content-Type": "application/json" } }) });
  const mod = new vm.SourceTextModule(await source("api.js"), { context });
  await mod.link(() => { throw new Error("unexpected dependency"); }); await mod.evaluate();
  let signedOut = 0; mod.namespace.onUnauthorized(() => signedOut++);
  await assert.rejects(mod.namespace.get("/online/resolve/netease:1"));
  await assert.rejects(mod.namespace.api("/online/resolve/netease:1", { raw: true }));
  assert.equal(signedOut, 0);
  code = "unauthorized";
  await assert.rejects(mod.namespace.get("/home"));
  assert.equal(signedOut, 1);
});

test("player queue, audio and pending load identity are reset on account switch", async () => {
  const localStorage = storage(); localStorage.setItem("yimeng.player", JSON.stringify({ queue: [{ id: "legacy-admin" }] }));
  class Audio {
    currentTime = 0; volume = 1; paused = true; src = "";
    addEventListener() {} pause() { this.paused = true; } load() {}
    removeAttribute(name) { if (name === "src") this.src = ""; }
  }
  const context = vm.createContext({ localStorage, Audio, navigator: {}, addEventListener() {}, setTimeout() {}, clearTimeout() {}, console });
  const stub = (values) => new vm.SyntheticModule(Object.keys(values), function () { for (const [k, v] of Object.entries(values)) this.setExport(k, v); }, { context });
  const api = stub({ get: async () => ({}), post: async () => ({}), streamURL: (id) => `/stream/${id}`, coverURL() {}, imgURL() {} });
  const ui = stub({ h() {}, icon() {}, cover() {}, fmtDur() {}, setRangeFill() {}, $() {}, toast() {}, isLocal: () => true, NOT_LOCAL_MSG: "" });
  const mod = new vm.SourceTextModule(await source("player.js"), { context });
  await mod.link((id) => id === "./api.js" ? api : ui); await mod.evaluate();
  const p = mod.namespace.player;
  assert.equal(localStorage.getItem("yimeng.player"), null);
  p.selectUser("alice"); p.queue = [{ id: "alice-song" }]; p.order = [0]; p.pos = 0; p.save();
  const oldSequence = p.loadSeq;
  p.selectUser("bob"); p.restore();
  assert.equal(p.queue.length, 0); assert.ok(p.loadSeq > oldSequence); assert.equal(p.ent, null);
  p.queue = [{ id: "bob-song" }]; p.order = [0]; p.pos = 0; p.save();
  p.selectUser("alice"); p.restore(); assert.equal(p.track.id, "alice-song");
  p.selectUser(""); p.restore(); assert.equal(p.queue.length, 0);
  p.selectUser("bob"); p.restore(); assert.equal(p.track.id, "bob-song");
});
