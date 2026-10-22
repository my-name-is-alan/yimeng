#!/usr/bin/env python3
"""译梦 Core mock (Debug only — never shipped). Serves the agreed draft API from in-memory fixtures:
ETag/304, cursor paging, pre-signed cover/stream URLs (?sig=&exp=), Range on /api/stream, word-timed lyrics,
charts, platform accounts (daily / fm / recommend / user playlists). Audio: a generated 120 BPM click+tone WAV.
Usage: python3 core_mock.py [port]   (login: demo / any password)"""
import hashlib, io, json, math, os, random, struct, sys, time, urllib.parse, wave, zlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 8787
HERE = os.path.dirname(os.path.abspath(__file__))
BASE = f"http://127.0.0.1:{PORT}"
random.seed(7)

# ---------- media ----------
def make_wav(path, bpm=120, secs=48, sr=22050):
    if os.path.exists(path): return
    w = wave.open(path, "wb"); w.setnchannels(1); w.setsampwidth(2); w.setframerate(sr)
    beat = 60 / bpm; frames = bytearray()
    notes = [261.6, 329.6, 392.0, 523.3, 392.0, 329.6, 293.7, 349.2]
    for i in range(int(secs * sr)):
        t = i / sr; ph = t % beat; k = int(t / beat)
        click = math.exp(-ph * 40) * math.sin(2 * math.pi * 1800 * ph) * (1.0 if k % 4 == 0 else 0.7)
        kick = math.exp(-ph * 18) * math.sin(2 * math.pi * (60 + 80 * math.exp(-ph * 30)) * ph)
        tone = 0.18 * math.sin(2 * math.pi * notes[(k // 2) % len(notes)] * t)
        v = max(-1, min(1, 0.45 * click + 0.6 * kick + tone))
        frames += struct.pack("<h", int(v * 30000))
    w.writeframes(bytes(frames)); w.close()

def png(color, size=300):
    r, g, b = color; raw = bytearray()
    for y in range(size):
        raw.append(0)
        for x in range(size):
            f = (x + y) / (2 * size); raw += bytes([int(r * (1 - f) + 255 * f * 0.3), int(g * (1 - f)), int(b * (1 - f) + 120 * f)])
    def chunk(t, d): return struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d) & 0xffffffff)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", size, size, 8, 2, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(bytes(raw), 6)) + chunk(b"IEND", b"")

WAV = os.path.join(HERE, "sample-120bpm.wav"); make_wav(WAV)
PALETTE = [(122, 28, 255), (255, 61, 139), (51, 209, 122), (255, 122, 26), (43, 108, 176), (200, 245, 66), (255, 95, 168), (10, 165, 138)]
COVER_CACHE = {}
def hexc(c): return "#%02x%02x%02x" % c
SECRET = b"mock-secret"
def sign(path):
    exp = int(time.time()) + 3600
    sig = hashlib.sha256(SECRET + f"{path}{exp}".encode()).hexdigest()[:16]
    return f"{BASE}{path}{'&' if '?' in path else '?'}exp={exp}&sig={sig}"
def cover(cid, n):
    c = PALETTE[n % len(PALETTE)]
    return {"url": sign(f"/api/covers/{cid}?size=600&format=jpeg"), "blurhash": "LEHV6nWB2yk8pyo0adR*.7kCMdnj",
            "colors": [hexc(c), hexc(PALETTE[(n + 3) % 8]), hexc(PALETTE[(n + 5) % 8])]}

# ---------- fixtures (fictional names, mock only) ----------
ARTISTS = [("ar1", "晚星乐队", "W"), ("ar2", "林间", "L"), ("ar3", "Aurora Field", "A"), ("ar4", "北岸", "B"), ("ar5", "Moonlit", "M"), ("ar6", "七月", "Q")]
ALBUMS = []; TRACKS = []
for i in range(8):
    ar = ARTISTS[i % 6]; aid = f"al{i+1}"
    ALBUMS.append({"id": aid, "name": ["夜航", "雾里花园", "Paper Moon", "海风来信", "Glass Garden", "七月的雨", "Neon Tide", "旧城日记"][i],
                   "artist": {"id": ar[0], "name": ar[1]}, "year": 2018 + i, "cover": cover(aid, i), "origin": "local", "track_count": 5,
                   "sort_letter": ["Y", "W", "P", "H", "G", "Q", "N", "J"][i]})
titles = ["梦里的那条长长长长长长长长的街道", "雨停之前", "Night Swim", "灯塔", "Polaroid", "风筝", "回声", "Slow Motion", "白日梦", "潮汐",
          "Satellite", "旧信", "晚安", "Blue Hour", "萤火", "远方", "Paper Planes", "星期天", "微光", "Afterglow", "云朵", "归途", "Echoes", "月台",
          "Daydream", "花火", "In Between", "海岸线", "小夜曲", "Lighthouse", "秋千", "落日飞行", "Riverside", "时光机", "Neon", "雪", "Waves", "城南", "Halo", "晨雾"]
for i, t in enumerate(titles):
    al = ALBUMS[i % 8]
    TRACKS.append({"id": f"tr{i+1}", "title": t, "artists": [al["artist"]], "album": {"id": al["id"], "name": al["name"]}, "album_id": al["id"],
                   "origin": "local", "playable": True, "preview": False, "entitlement": "full", "duration_ms": 48000, "has_lyrics": i % 3 != 2, "favorite": i in (1, 4),
                   "audio": {"codec": "flac" if i % 2 == 0 else "aac", "sample_rate": 44100 if i % 2 else 96000, "bit_depth": 24 if i % 2 == 0 else None,
                             "bitrate": 320 if i % 2 else 2800, "channels": 2},
                   "cover": al["cover"], "stream_url": sign(f"/api/stream/tr{i+1}"), "sort_letter": "A", "track_no": i // 8 + 1,
                   "isrc": f"CNMOCK26{i:05d}", "popularity": round(random.random(), 2)})
ONLINE = [
    {"id": "netease:186001", "title": "网易云上的一首歌", "artists": [{"id": "netease:ar9", "name": "云村歌手"}], "album": {"id": "netease:al9", "name": "云村专辑"},
     "origin": "netease", "playable": True, "preview": False, "duration_ms": 48000, "has_lyrics": True, "cover": cover("netease_186001", 2), "sort_letter": "W"},
    {"id": "qq:003aBc", "title": "仅可试听的歌", "artists": [{"name": "Q 音歌手"}], "album": {"name": "Q 音专辑"}, "origin": "qq", "playable": True, "preview": False, "entitlement": "preview",
     "preview_window": {"start_ms": 30000, "end_ms": 60000}, "duration_ms": 48000, "cover": cover("qq_003", 4), "sort_letter": "J"},
    {"id": "kugou:A1B2", "title": "版权受限的歌", "artists": [{"name": "酷狗歌手"}], "album": {"name": "酷狗专辑"}, "origin": "kugou", "playable": False, "preview": False, "entitlement": "unavailable",
     "unplayable_reason": "酷狗：该歌曲因版权原因暂不可播放", "duration_ms": 200000, "cover": cover("kugou_a1", 6), "sort_letter": "B"},
]
for o in ONLINE: o.setdefault("stream_url", sign(f"/api/stream/{urllib.parse.quote(o['id'], safe='')}"))
ALL_TRACKS = TRACKS + ONLINE
for t in ALL_TRACKS:   # sort letter from title (mock: first latin letter or pinyin-ish table)
    c = t["title"][0].upper(); t["sort_letter"] = c if "A" <= c <= "Z" else (t.get("sort_letter") if t["origin"] != "local" else "#")

def track_by(id): return next((t for t in ALL_TRACKS if t["id"] == id), None)
PLAYLISTS = [{"id": "pl1", "name": "通勤路上", "track_count": 6, "cover": cover("pl1", 1), "origin": "local", "track_ids": ["tr1", "tr3", "netease:186001", "tr5", "qq:003aBc", "tr8"]},
             {"id": "pl2", "name": "睡前", "track_count": 4, "cover": cover("pl2", 5), "origin": "local", "track_ids": ["tr2", "tr9", "tr13", "tr19"]}]
WORDS = [("梦里的", 0), ("那条", 1), ("街", 2)]
def lyric_lines():
    base = [("夜色漫过窗台", "Night spills over the sill", "yoru no iro"), ("你在梦里轻轻唱", "You sing softly in my dream", None),
            ("一拍一拍 心跳成歌", "Beat by beat, the heart becomes a song", "hitotsu hitotsu"), ("风把星星吹亮", None, None),
            ("我们在光里摇晃", "We sway in the light", None), ("一拍一拍 心跳成歌", "Beat by beat, the heart becomes a song", "hitotsu hitotsu"),
            ("直到天亮", "Until dawn", None), ("晚安 我的歌", "Good night, my song", "oyasumi")]
    out = []
    for i, (txt, tr, ro) in enumerate(base):
        t0 = 2000 + i * 5500; parts = [p for p in txt.replace(" ", " |").split("|")]
        chars = list(txt); step = 4000 // max(1, len(chars))
        words = [{"t_ms": t0 + k * step, "d_ms": step, "text": ch} for k, ch in enumerate(chars)]
        l = {"t_ms": t0, "text": txt, "words": words}
        if tr: l["trans"] = tr
        if ro: l["roma"] = ro
        out.append(l)
    return out
PLATFORMS = [{"origin": "netease", "enabled": True, "logged_in": True, "nickname": "云村村民", "capabilities": ["daily", "fm", "recommend_playlists", "user_playlists", "charts", "lyrics"]},
             {"origin": "qq", "enabled": True, "logged_in": True, "nickname": "Q 友", "capabilities": ["daily", "recommend_playlists", "user_playlists", "charts"]},
             {"origin": "kugou", "enabled": True, "logged_in": False, "capabilities": ["charts"]}]
CHARTS = {o: [{"id": f"chart_{o}_{k}", "name": n, "origin": o, "cover": cover(f"chart_{o}_{k}", k + 2), "update_frequency": "每天更新", "updated_at": time.strftime("%Y-%m-%dT%H:%M:%S", time.gmtime(time.time() - 3600 * (k + 1))) + "Z",
               "preview": [dict(TRACKS[(k * 3 + j) % 40], origin=o) for j in range(3)]}
              for k, n in enumerate(["飙升榜", "新歌榜", "热歌榜", "原创榜"])] for o in ["netease", "qq", "kugou"]}
PROGRESS = {}
FAVS = {"track": {"tr2", "tr5"}, "album": {"al2"}, "artist": set()}

def page(items, q, key):
    limit = int(q.get("limit", ["50"])[0]); cur = int(q.get("cursor", ["0"])[0] or 0)
    if "origin" in q: items = [x for x in items if x.get("origin", "local") == q["origin"][0]]
    chunk = items[cur:cur + limit]; nxt = cur + limit if cur + limit < len(items) else None
    return {"items": chunk, "next_cursor": str(nxt) if nxt else None, "updated_at": "2026-10-22T08:00:00Z", "total": len(items)}

def session(name, user="mock"):
    # Core P0: Session {token, device, user}; ymd_ device tokens do not expire.
    return {"token": "ymd_" + os.urandom(8).hex(), "device": {"id": "dev_" + os.urandom(6).hex(), "name": name, "device_type": "ios"},
            "user": {"id": "u1", "username": user, "role": "user"}}
HISTORY = []

class H(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    def log_message(self, f, *a): sys.stderr.write("%s %s\n" % (self.command, self.path.split("?")[0]))
    def body(self):
        n = int(self.headers.get("Content-Length", 0) or 0)
        return json.loads(self.rfile.read(n) or b"{}") if n else {}
    def send_json(self, obj, code=200, etag=True):
        data = json.dumps(obj, ensure_ascii=False).encode()
        tag = '"' + hashlib.sha1(data).hexdigest()[:20] + '"'
        if etag and self.headers.get("If-None-Match") == tag:
            self.send_response(304); self.send_header("ETag", tag); self.send_header("Content-Length", "0"); self.end_headers(); return
        self.send_response(code); self.send_header("Content-Type", "application/json; charset=utf-8")
        if etag: self.send_header("ETag", tag)
        self.send_header("Content-Length", str(len(data))); self.end_headers(); self.wfile.write(data)
    def err(self, code, c, msg): return self.send_json({"error": c, "code": c, "message": msg}, code, False)
    def authed(self):
        a = self.headers.get("Authorization", ""); q = urllib.parse.parse_qs(urllib.parse.urlparse(self.path).query)
        return a.startswith("Bearer ymd_") or "sig" in q or q.get("access_token", [""])[0].startswith("ymd_")
    def do_DELETE(self): self.route("DELETE")
    def do_PUT(self): self.route("PUT")
    def do_PATCH(self): self.route("PATCH")
    def do_POST(self): self.route("POST")
    def do_GET(self): self.route("GET")
    def route(self, m):
        u = urllib.parse.urlparse(self.path); p = u.path; q = urllib.parse.parse_qs(u.query)
        if p == "/api/v1/server/info":
            return self.send_json({"name": "译梦 Core · 书房 NAS (mock)", "server_name": "译梦 Core · 书房 NAS (mock)", "api_version": "v1", "authenticated": self.authed(), "version": "0.1.0-mock", "features": {"lyrics": True, "charts": True, "ws": False, "sse": True},
                                   "platforms": PLATFORMS})
        if p == "/api/v1/auth/login":
            b = self.body()
            if not b.get("username") or not b.get("password"): return self.err(401, "invalid_credentials", "用户名或密码错误")
            return self.send_json(session(b.get("device_name", "mock"), b["username"]), etag=False)
        if p == "/api/v1/pair":
            b = self.body()
            if not b.get("code"): return self.err(400, "invalid_body", "code is required")
            return self.send_json(session(b.get("name", "mock")), 201, False)
        if p == "/api/v1/pair/request": sys.stderr.write("PAIRING CODE (mock) 0000-0000\n"); return self.send_json({"expires_at": "2026-10-22T12:00:00Z"}, 202, False)
        if p == "/api/v1/auth/refresh":
            if not self.body().get("refresh_token"): return self.err(400, "invalid_body", "refresh_token is required")
            return self.send_json(session("mock"), etag=False)
        if p == "/api/v1/auth/logout": self.send_response(204); self.send_header("Content-Length", "0"); self.end_headers(); return
        if p.startswith("/api/covers/"):
            cid = urllib.parse.unquote(p.split("/")[-1]); n = sum(map(ord, cid)) % 8
            img = COVER_CACHE.get(n) or png(PALETTE[n], 240); COVER_CACHE[n] = img
            self.send_response(200); self.send_header("Content-Type", "image/png"); self.send_header("Content-Length", str(len(img))); self.end_headers(); self.wfile.write(img); return
        if p.startswith("/api/stream/"):
            if not self.authed(): return self.err(401, "unauthorized", "unauthorized")
            tid = urllib.parse.unquote(p.split("/")[-1]); t = track_by(tid)
            if t and t.get("playable") is False: return self.err(403, "forbidden", t.get("unplayable_reason"))
            data = open(WAV, "rb").read(); total = len(data); rng = self.headers.get("Range")
            start, end = 0, total - 1
            if rng and rng.startswith("bytes="):
                a, _, b = rng[6:].partition("-"); start = int(a or 0); end = int(b) if b else total - 1
            end = min(end, total - 1); chunk = data[start:end + 1]
            self.send_response(206 if rng else 200); self.send_header("Content-Type", "audio/wav"); self.send_header("Accept-Ranges", "bytes")
            if rng: self.send_header("Content-Range", f"bytes {start}-{end}/{total}")
            self.send_header("X-Yimeng-Codec", "pcm_s16le"); self.send_header("X-Yimeng-SampleRate", "22050"); self.send_header("X-Yimeng-BitDepth", "16"); self.send_header("X-Yimeng-Channels", "1")
            self.send_header("Content-Length", str(len(chunk))); self.end_headers(); self.wfile.write(chunk); return
        if not self.authed(): return self.err(401, "unauthorized", "unauthorized")
        if p == "/api/v1/home":
            return self.send_json({"banner": ALBUMS[:5], "recently_added": ALBUMS[::-1], "recently_played": ALBUMS[2:7], "frequent": ALBUMS[1:6],
                                   "random": random.Random(time.strftime("%Y%m%d")).sample(ALBUMS, 6), "top_artists": [{"id": a, "name": n, "sort_letter": l, "cover": cover(a, i)} for i, (a, n, l) in enumerate(ARTISTS)],
                                   "playlists": [{k: v for k, v in pl.items() if k != "track_ids"} for pl in PLAYLISTS], "charts": {"title": "排行", "origins": ["netease", "qq", "kugou"]}})
        if p == "/api/v1/albums": return self.send_json(page(ALBUMS, q, "albums"))
        if p.startswith("/api/v1/albums/"):
            aid = p.split("/")[-1]; a = next((x for x in ALBUMS if x["id"] == aid), None)
            return self.send_json(dict(a, tracks=[t for t in TRACKS if t["album_id"] == aid])) if a else self.err(404, "not_found", "not found")
        if p == "/api/v1/artists": return self.send_json(page([{"id": a, "name": n, "sort_letter": l, "album_count": 1, "cover": cover(a, i)} for i, (a, n, l) in enumerate(ARTISTS)], q, "ar"))
        if p.startswith("/api/v1/artists/"):
            aid = p.split("/")[4]
            if p.endswith("/albums"): return self.send_json(page([a for a in ALBUMS if a["artist"]["id"] == aid], q, "x"))
            return self.send_json(page([t for t in TRACKS if t["artists"][0]["id"] == aid], q, "x"))
        if p == "/api/v1/tracks":
            items = sorted(ALL_TRACKS, key=lambda t: (t["sort_letter"] == "#", t["sort_letter"], t["title"]))
            return self.send_json(page(items, q, "tracks"))
        if p == "/api/v1/favorites" and m == "GET":
            typ = q.get("type", ["track"])[0]; src = ALL_TRACKS if typ == "track" else ALBUMS
            return self.send_json(page([x for x in src if x["id"] in FAVS.get(typ, set())], q, "f"), etag=False)
        if p.startswith("/api/v1/favorites/"):
            _, _, _, _, typ, fid = p.split("/", 5); fid = urllib.parse.unquote(fid)
            (FAVS.setdefault(typ, set()).add if m == "PUT" else FAVS.setdefault(typ, set()).discard)(fid); return self.send_json({}, etag=False)
        if p == "/api/v1/playlists":
            if m == "POST":
                b = self.body(); pl = {"id": f"pl{len(PLAYLISTS)+1}", "name": b.get("name", "新歌单"), "track_ids": b.get("track_ids", []), "origin": "local"}
                pl["track_count"] = len(pl["track_ids"]); PLAYLISTS.append(pl); return self.send_json({k: v for k, v in pl.items() if k != "track_ids"}, etag=False)
            sc = q.get("scope", ["local"])[0]
            if sc not in ("local", "mine", "recommend", "discover", "charts", "all", "online"): return self.err(400, "invalid_query", "invalid scope")
            loc = [{k: v for k, v in pl.items() if k != "track_ids"} for pl in PLAYLISTS]
            onl = [dict(c, kind="chart") for o in CHARTS for c in CHARTS[o]]
            items = loc if sc in ("local", "mine") else onl if sc in ("online", "charts", "recommend", "discover") else loc + onl
            return self.send_json(page(items, {k: v for k, v in q.items() if k != "origin"}, "pl"))
        if p.startswith("/api/v1/playlists/"):
            parts = p.split("/"); pid = parts[4]
            if pid.startswith("chart_"):
                o = pid.split("_")[1]; return self.send_json(page([dict(t, origin=o) for t in TRACKS[:20]], q, "c"))
            if pid.startswith("rec_") or pid.startswith("u_"):
                return self.send_json(page(TRACKS[5:17], q, "c"))
            pl = next((x for x in PLAYLISTS if x["id"] == pid), None)
            if not pl: return self.err(404, "not_found", "not found")
            if len(parts) > 5:
                if m == "GET": return self.send_json(page([track_by(i) for i in pl["track_ids"] if track_by(i)], q, "pt"), etag=False)
                b = self.body() if m != "DELETE" else {}
                if m == "POST": pl["track_ids"] += b.get("track_ids", [])
                if m == "PUT": pl["track_ids"] = b.get("track_ids", [])
                if m == "DELETE":
                    idx = set(int(x) for x in q.get("indexes", [""])[0].split(",") if x); pl["track_ids"] = [t for k, t in enumerate(pl["track_ids"]) if k not in idx]
                pl["track_count"] = len(pl["track_ids"]); return self.send_json({}, etag=False)
            if m == "PATCH": pl["name"] = self.body().get("name", pl["name"]); return self.send_json({}, etag=False)
            if m == "DELETE": PLAYLISTS.remove(pl); return self.send_json({}, etag=False)
        if p == "/api/v1/lyrics/best":
            tid = q.get("track_id", [""])[0]; t = track_by(tid)
            if not t or not t.get("has_lyrics"): return self.send_json({"found": False, "message": "暂无歌词"})
            return self.send_json({"found": True, "source": "mock", "lines": lyric_lines()})
        if p.startswith("/api/v1/tracks/") and p.endswith("/dream"):
            tid = urllib.parse.unquote(p.split("/")[4]); t = track_by(tid) or {}
            d = {"colors": (t.get("cover") or {}).get("colors"), "tags": ["治愈", "夜晚"] if tid in ("tr1", "netease:186001") else []}
            if tid == "netease:186001": d["bpm"] = 120
            return self.send_json(d)
        if p.startswith("/api/v1/progress"):
            if m == "PUT":
                tid = urllib.parse.unquote(p.split("/")[-1]); b = self.body()
                PROGRESS[tid] = {"track_id": tid, "position_ms": b.get("position_ms", 0), "duration_ms": b.get("duration_ms"), "finished": bool(b.get("finished")),
                                 "device_id": "dev_mock", "updated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}
                return self.send_json(PROGRESS[tid], etag=False)
            ids = q.get("ids", [""])[0].split(",")
            return self.send_json({"items": [PROGRESS[i] if isinstance(PROGRESS[i], dict) else {"track_id": i, "position_ms": PROGRESS[i]} for i in ids if i in PROGRESS]}, etag=False)
        if p == "/api/v1/charts":
            o = q.get("origin", ["netease"])[0]; return self.send_json({"origins": ["netease", "qq", "kugou"], "charts": CHARTS.get(o, [])})
        if p == "/api/v1/search/suggest": return self.send_json({"q": q.get("q", [""])[0], "items": [t["title"] for t in ALL_TRACKS if q.get("q", [""])[0] in t["title"]][:8]})
        if p == "/api/v1/scrobble" and m == "POST":
            b = self.body()
            if not b.get("track_id"): return self.err(400, "invalid_body", "track_id is required")
            HISTORY.insert(0, {"track_id": b["track_id"], "played_at": b.get("played_at") or time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "duration_ms": b.get("duration_ms", 0), "completed": bool(b.get("completed"))})
            self.send_response(204); self.send_header("Content-Length", "0"); self.end_headers(); return
        if p == "/api/v1/history": return self.send_json({"items": HISTORY[:int(q.get("limit", ["50"])[0])], "total": len(HISTORY)}, etag=False)
        if p == "/api/v1/search":
            s = q.get("q", [""])[0]
            return self.send_json({"tracks": [t for t in TRACKS if s in t["title"]], "albums": [a for a in ALBUMS if s in a["name"]], "artists": [],
                                   "playlists": [], "online": [{"origin": "netease", "name": "网易云", "result": {"tracks": [ONLINE[0]]}},
                                              {"origin": "qq", "name": "QQ 音乐", "result": {"tracks": [ONLINE[1]]}}] if q.get("online", ["0"])[0] == "1" else []})
        if p.startswith("/api/v1/online/"):
            parts = p.split("/"); o = parts[4]; rest = "/".join(parts[5:])
            plat = next((x for x in PLATFORMS if x["origin"] == o), None)
            if not plat or not plat["logged_in"]: return self.err(404, "not_logged_in", "未登录该平台")
            def mk(t): return dict(t, origin=o)
            if rest == "daily" and "daily" in plat["capabilities"]: return self.send_json({"origin": o, "date": time.strftime("%Y-%m-%d"), "items": [mk(t) for t in TRACKS[10:22]]}, etag=False)
            if rest == "fm" and "fm" in plat["capabilities"]: return self.send_json({"items": [mk(t) for t in random.sample(TRACKS, 3)]}, etag=False)
            if rest == "fm/feedback": return self.send_json({}, etag=False)
            if rest == "recommend/playlists" and "recommend_playlists" in plat["capabilities"]:
                return self.send_json({"items": [{"id": f"rec_{o}_{k}", "name": n, "origin": o, "track_count": 12, "cover": cover(f"rec{o}{k}", k + 1)} for k, n in enumerate(["深夜独处", "雨天的歌", "城市漫步", "安静写字"])]})
            if rest == "user/playlists" and "user_playlists" in plat["capabilities"]:
                typ = q.get("type", ["created"])[0]
                names = ["我喜欢的音乐", "跑步"] if typ == "created" else ["收藏的老歌"]
                return self.send_json({"items": [{"id": f"u_{o}_{typ}_{k}", "name": n, "origin": o, "track_count": 12, "cover": cover(f"u{o}{typ}{k}", k)} for k, n in enumerate(names)]})
            return self.err(404, "not_supported", "该平台不支持")
        if p == "/api/v1/events":
            self.send_response(200); self.send_header("Content-Type", "text/event-stream"); self.send_header("Cache-Control", "no-cache"); self.end_headers()
            try:
                while True: self.wfile.write(b": keepalive\n\n"); self.wfile.flush(); time.sleep(15)
            except Exception: return
        return self.err(404, "not_found", "not found")

if __name__ == "__main__":
    print(f"译梦 Core mock on {BASE}", flush=True)
    ThreadingHTTPServer(("0.0.0.0", PORT), H).serve_forever()
