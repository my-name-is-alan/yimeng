// Thin client for the Core REST API (/api/v1) + /ws. The device token lives in localStorage only.
const KEY = "yimeng.token";
export const store = {
  get token() { return localStorage.getItem(KEY) || ""; },
  set token(v) { v ? localStorage.setItem(KEY, v) : localStorage.removeItem(KEY); },
};

export class ApiError extends Error {
  constructor(status, body) {
    super(body?.message || body?.error || `HTTP ${status}`);
    this.status = status; this.code = body?.error || "";
  }
}

const unauth = new Set();
export const onUnauthorized = (fn) => unauth.add(fn);

export async function api(path, { method = "GET", body, raw, headers = {}, signal } = {}) {
  const h = { ...headers };
  if (store.token) h.Authorization = `Bearer ${store.token}`;
  let b = body;
  if (body !== undefined && !(body instanceof Blob) && !(body instanceof ArrayBuffer) && typeof body !== "string") {
    h["Content-Type"] = "application/json"; b = JSON.stringify(body);
  }
  const res = await fetch(`/api/v1${path}`, { method, headers: h, body: b, signal });
  if (res.status === 401 && !path.startsWith("/pair") && !path.startsWith("/auth/")) unauth.forEach((f) => f());
  if (raw) { if (!res.ok) throw new ApiError(res.status, await res.json().catch(() => null)); return res; }
  if (res.status === 204) return null;
  const ct = res.headers.get("content-type") || "";
  const data = ct.includes("json") ? await res.json().catch(() => null) : await res.text();
  if (!res.ok) throw new ApiError(res.status, data);
  return data;
}
export const get = (p, o) => api(p, o);
export const post = (p, body, o) => api(p, { ...o, method: "POST", body });
export const put = (p, body, o) => api(p, { ...o, method: "PUT", body });
export const patch = (p, body, o) => api(p, { ...o, method: "PATCH", body });
export const del = (p, o) => api(p, { ...o, method: "DELETE" });

export const qs = (o) => {
  const u = new URLSearchParams();
  for (const [k, v] of Object.entries(o)) if (v !== undefined && v !== null && v !== "") u.set(k, v);
  const s = u.toString(); return s ? `?${s}` : "";
};
const tok = () => encodeURIComponent(store.token);
let coverBust = {};
export const bustCover = (albumId) => { coverBust[albumId] = Date.now(); };
export const coverURL = (albumId, size = 300) => albumId ? `/api/v1/albums/${albumId}/cover?size=${size}&access_token=${tok()}${coverBust[albumId] ? `&v=${coverBust[albumId]}` : ""}` : "";
export const artistImageURL = (ar, size) => ar?.image_updated_at ? `/api/v1/artists/${ar.id}/image?access_token=${tok()}&v=${encodeURIComponent(ar.image_updated_at)}` : "";
export const avatarURL = (u) => u?.avatar_url ? `${u.avatar_url}&access_token=${tok()}` : "";
export const streamURL = (trackId) => `/api/v1/stream/${trackId}?access_token=${tok()}`;
export const downloadURL = (trackId) => `/api/v1/tracks/${trackId}/download?access_token=${tok()}`;

// ── live events over /ws with auto-reconnect ──
const wsHandlers = new Map();
let ws, wsTimer, wsBackoff = 1000;
export const wsState = { connected: false };
export function onEvent(type, fn) {
  if (!wsHandlers.has(type)) wsHandlers.set(type, new Set());
  wsHandlers.get(type).add(fn);
  return () => wsHandlers.get(type)?.delete(fn);
}
function emit(type, data) {
  wsHandlers.get(type)?.forEach((f) => f(data));
  wsHandlers.get("*")?.forEach((f) => f({ type, data }));
}
export function connectWS() {
  if (!store.token || (ws && ws.readyState <= 1)) return;
  const proto = location.protocol === "https:" ? "wss" : "ws";
  ws = new WebSocket(`${proto}://${location.host}/ws?access_token=${tok()}`);
  ws.onopen = () => { wsBackoff = 1000; wsState.connected = true; emit("ws.status", true); };
  ws.onmessage = (m) => { try { const e = JSON.parse(m.data); emit(e.type, e.data); } catch {} };
  ws.onclose = () => {
    wsState.connected = false; emit("ws.status", false);
    if (!store.token) return;
    clearTimeout(wsTimer); wsTimer = setTimeout(connectWS, wsBackoff); wsBackoff = Math.min(wsBackoff * 2, 30000);
  };
}
export function closeWS() { clearTimeout(wsTimer); const w = ws; ws = null; try { w?.close(); } catch {} }
