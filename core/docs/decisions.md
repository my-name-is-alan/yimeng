# Core design decisions

Decisions taken while implementing Core autonomously; each lists the
alternative considered. Newest items at the bottom.

## D1 Backup / restore
- Snapshots use `VACUUM INTO` (consistent, compacted, works while serving) packed
  as a zip (`.ymbak`: `library.db` + `manifest.json`).
- Vault credentials are rows in `vault_entries` (AES-256-GCM), so every snapshot
  contains them **encrypted**. The master key is never written to stored backups;
  `GET /api/system/export?include_key=true` can add it explicitly for migration
  to a new host.
- Restore is **staged**: the upload is validated (zip, manifest, SQLite
  `integrity_check`, schema not newer than the binary) and moved to
  `<db>.restore-pending`; it is swapped in at the next start (old DB kept as
  `<db>.pre-restore-<ts>`). Hot-swapping a live SQLite file under open
  connections/goroutines was rejected as unsafe.
- An automatic backup (`pre-migrate-N`) runs before pending migrations on an
  existing database; failure aborts startup rather than migrating unprotected.
- Scheduled backups: `YIMENG_BACKUP_INTERVAL` (24h), keep `YIMENG_BACKUP_KEEP` (7).
  Rotation applies to all stored backups regardless of reason.
- Cover/transcode caches are not backed up (re-derivable).

## D2 Device roles
- Backup, restore and other system/admin endpoints need an **admin device**.
  Migration 0005 makes the oldest paired device admin; whenever no admin exists
  the next paired device becomes admin. The last admin cannot be demoted.
  Rejected: a separate admin password (one more secret to manage).

## D3 Lyrics
- Order: sidecar `.lrc` (same basename) → embedded tag → optional provider;
  synced beats plain. Sidecars are read at request time (no rescan after
  editing), must resolve inside a library root (symlink escape refused), ≤1 MiB,
  BOM stripped, invalid UTF-8 decoded as GB18030.
- Embedded lyrics are stored in `source_tracks.lyrics` (≤256 KiB). Migration 0006
  clears `codec` so the next scan re-reads every file once to pick them up.
- LRC: multiple timestamps per line, `[offset]` applied server-side, enhanced
  word timing `<mm:ss.xx>` stripped (line-level only for now).

## D4 History, favorites, ratings
- A play counts (Last.fm rule) when the track ended, or ≥ min(50 %, 4 min) was
  heard; tracks ≤30 s must complete. Core-managed players are recorded when the
  orchestrator leaves a track (skip, end, replace, stop); seeking is not leaving.
  `POST /api/scrobble` records client-side plays verbatim (client applies the rule).
- `plays`, `track_stats`, `favorites`, `ratings` have **no FK** to tracks: user
  data survives a file being temporarily missing (NAS offline) and reattaches
  because canonical IDs are deterministic. Listings mark such rows `available:false`.
- `track_stats` is a denormalized counter so sorting by plays scales (no
  aggregate over `plays` per list query).
- Single-user model: favorites/ratings are global, not per device.

## D5 Playlists
- Static items have no FK to tracks (missing files keep their slot; `missing`
  count reported). Smart playlists store rule JSON evaluated at read time; only
  whitelisted field→SQL expressions are interpolated, values are bound.
- M3U import never opens referenced files; it matches against indexed paths,
  root-relative paths, unique basenames, then `#EXTINF` artist/title. Export
  writes absolute (default) or root-relative paths — paths are only exposed to
  authenticated devices, which already own the library.

## D6 ReplayGain / loudness
- Read REPLAYGAIN_* (Vorbis, ID3 TXXX, MP4 freeform) and Opus R128_*_GAIN
  (Q7.8 vs -23 LUFS → +5 dB to the RG2 -18 LUFS reference). Values outside
  ±60 dB are ignored.
- Missing gains are measured in the background with ffmpeg `ebur128=peak=true`
  (one file at a time, 10 min timeout, kicked after every merge; failures are
  recorded and not retried until the file changes). Gain = -18 − I.
  Album gain for analyzed albums = duration-weighted energy mean of track
  loudness (approximation of a joint album measurement; avoids re-decoding).
- Gain is exposed (stream.replaygain on tracks, track.replaygain in
  player.state); applying it is the client's job — originals are never altered
  (bit-perfect), and transcodes are not gain-adjusted either.
- Disable with `YIMENG_LOUDNESS_ANALYSIS=false`.

## D7 Logging and /api/system
- slog text|json via `YIMENG_LOG_FORMAT` / `YIMENG_LOG_LEVEL`. Access log is one
  structured line per request: method, path (never query — tokens live there),
  status, bytes, duration, remote IP, device id when authenticated.
- `GET /api/system` is readable by every paired device (status for clients);
  backup/restore stay admin-only. Diagnostics surface root permission errors,
  low disk, low inotify watches, missing ffmpeg, stale backups, scan failures.

## D8 mDNS discovery
- Announce `_yimeng._tcp` via DNS-SD (grandcat/zeroconf) with TXT `path=/`,
  `version=…`, `proto=http`. Instance name "Yimeng". Port taken from `server.bind`.
  Disabled with `YIMENG_MDNS=false`. Clients browse the service type; no custom
  UDP protocol. Goodbye packet on shutdown.

## D9 Transfer playback and presence
- Online = at least one open WebSocket for that device. Multiple tabs share a
  count; only 0↔1 transitions publish device.online / device.offline. Hello
  includes the current online set.
- Transfer moves queue + session (incl. shuffle seed/order) to the destination
  and clears the source. Playing becomes paused on arrival so the new renderer
  can buffer before resume. Rejected: simultaneously keeping both playing
  (would require dual leases and split scrobbling).

## D10 Admin APIs and override layer
- Library roots are runtime-editable (admin), persisted in `settings`, loaded on
  start (override config file roots). Absolute paths only.
- `metadata_overrides` store a JSON patch per entity; whitelisted fields only.
  Applied immediately and re-applied at the end of every Merge so rescans cannot
  clobber user edits. Clearing an override lets the next scan restore tags.
  Artwork/tag write-back remains out of scope (read-only library).
- Album merge moves tracks then deletes source albums; split moves tracks to a
  new/existing album and leaves an empty source (no auto-delete) so a mistaken
  split can be merged back.

## D11 Cover thumbnails
- Optional `?size=&format=` on cover endpoints. Pure-Go decode (jpeg/png/gif/webp)
  + Catmull-Rom resize + encode jpeg/webp/png. Cache key includes source
  path/mtime/size so edits invalidate. Original (no query) still served as-is.

## D12 Metadata provider registry + remote lyrics
- `metadata.Registry` owns MetadataProvider factories; MusicBrainz registers when
  `metadata.musicbrainz.enabled` or `providers` includes `musicbrainz`.
- Remote lyrics use a separate `lyrics/providers` Chain adapted to
  `library.LyricsFetcher`. Default `providers = ["lrclib"]` (public, no-auth).
- NetEase / QQ Music: SPI stubs only — no documented public API without scraping
  or private mobile endpoints. Do not enable by default; warn if listed.
- `GET /api/metadata/providers` lists registry MetadataProviders (not lyrics stubs).

## D13 HTTPS, trusted proxies, rate limits
- TLS is optional via cert/key paths; both required together. No automatic ACME
  (NAS often terminates TLS at a reverse proxy).
- X-Forwarded-For / X-Real-IP are honoured only when the direct peer is in
  `trusted_proxies` CIDRs. Empty list = never trust (safe default on LAN).
- Token-bucket rate limits: per-client (Bearer prefix else IP) and optional
  global. `/healthz` exempt. 429 + Retry-After. Default 50/s burst 200.

## D14 Offline download
- `GET /api/tracks/{id}/download` is for client-side offline caches, not streaming.
  Always `Content-Disposition: attachment` with a sanitized `Artist - Title.ext`
  filename (RFC 5987). Original by default; optional format/bitrate materializes
  a full transcode into the cache first so Range/Content-Length work. Query
  token allowed. Rejected: zip-of-album endpoint (clients iterate track IDs).

## D15 Subsonic /rest
- Optional (`subsonic.enabled`, default on). Password = paired device token
  (`ymd_…` or `enc:<hex>`); Bearer also accepted. Token+salt (t/s) rejected
  because Core stores only SHA-256 of tokens — clients must use password mode.
- Minimal OpenSubsonic-flavoured surface: ping, license, folders, indexes,
  artists, albumList2, album, song, search3, playlists, lyrics, coverArt,
  stream/download (delegates to Core stream). JSON via `f=json`.

## D16 StreamSource account management
- `/api/sources*` manages StreamSource stubs (netease/qq/apple). Credentials go
  only into the AES-GCM vault; list/get never return secrets. Login/logout/sync
  are admin-only. ResolveStream/SyncLibrary stay ErrNotSupported until a
  licensed integration exists — no scraping private APIs.

## D17 AIFF / APE / DSF / DFF
- Extensions added to the local adapter index map. Pure-Go probes for duration
  and stream info (AIFF COMM, DSF fmt, DSDIFF PROP/DSD, APE v3.98+ header).
- Tags: whatever dhowden/tag can read (DSF ID3 yes; APE/DFF/AIFF best-effort).
  Transcode of DSD/APE remains ffmpeg-dependent when available.

## D18 Pinyin + fuzzy search
- Migration adds `search_text` on artists/albums/tracks: lowercase name + full
  pinyin + initials (mozillazg/go-pinyin). Filled on every Merge.
- List/search `q` expands via ExpandQuery (original + pinyin of Chinese chars)
  and ORs LIKE against name fields and search_text. Not edit-distance fuzzy —
  substring + pinyin is enough for LAN library size.

## D19 /api/v1 alias
- Middleware rewrites `/api/v1/...` → `/api/...`. No separate handler set.

## D20 Config hot-reload
- SIGHUP reloads safe fields (log, rate limits, trusted proxies, mdns, subsonic,
  providers, watch/rescan). Bind/DB/vault/TLS paths require restart.

## D21 Sleep timer
- In-memory per-player deadline; `POST .../sleep` with minutes (0=clear).
  Background ticker pauses and publishes `player.sleep`. Not persisted across
  Core restarts (acceptable for a nap timer).

## D22 Radio / auto-continue
- `POST .../radio` seeds the queue from same artist then genre (RANDOM).
  No ML; clients call again for more. Stubs keep Core free of upstream APIs.

## D23 Genre normalization
- `normGenre` lowercases and maps common aliases at Merge time. Sort names
  already use `normKey` on artists.

## D24 Classical / multi-disc
- `albums.disc_count` maintained on Merge (MAX disc_no). Tracks keep disc_no/
  track_no; list sort `track` is disc then track. Composer column reserved
  (filled when scanner persists composer — tags already read by local adapter).

## D25 Library health + stats
- `GET /api/library/health` and `GET /api/stats` (+ on-this-day) are read-only
  aggregates over local tables. No separate analytics DB.

## D26 Embedded web admin
- `/admin/` serves a single HTML page for pairing + system/health/stats. Not a
  full SPA; credentials stay in localStorage device token. Setup wizard = pair
  flow on first visit.

## D27 100k-track perf
- Migration 0012 adds indexes on search_text, disc order, genre, composer.
  Query limits remain capped (list max 500).

## D28 Shared TS types
- `packages/shared` holds hand-maintained TypeScript interfaces mirroring Core
  DTOs for web/clients. Not codegen yet.

## D29 Metadata editing is local-library only
- `tracks`, `albums` and `artists` carry `origin` (migration 0014, `NOT NULL
  DEFAULT 'local'`); it is exposed as `origin` in the API. Items built from
  files under a library root are `local`. Future online StreamSources
  (NetEase, QQ, Apple, …) must insert their items with their own origin
  (`netease`, `qq`, `apple`, …).
- Only `local` items can be edited or matched. Guarded endpoints:
  - overrides `PUT/DELETE /api/overrides/{type}/{id}`
  - album merge/split
  - cover replace/clear
  - MusicBrainz match for tracks and albums
  - lyrics bind/edit/clear and LRCLIB search-for-binding

  A non-local target gets **409** `{"error":"not_local_source", "ids", "origins"}`. If a request mixes online and missing ids, 409 wins over 404.
  `POST /api/overrides/batch` applies only the local ids. It reports skipped ids in
  `skipped_not_local`, and their result rows carry `error:"not_local_source"`.
- Rationale: online catalogs are owned by the provider. Local overrides would
  silently diverge from, or be clobbered by, provider refreshes. Bound lyrics and
  covers would attach to ids the provider may recycle.
- Rejected: deriving "local" from `source_tracks.source` on every request (costs a
  join per check, and albums/artists have no direct link). Also rejected:
  enforcing only in the UI.
- The web UI hides edit, match, cover and lyrics actions for non-local items.
  It shows a source badge, and batch selection excludes them.

## D30 Online platforms are Core plugins
- Every online platform is a Go plugin in `internal/online/<id>` behind the
  `online.Plugin` interface, with optional capability interfaces. It is
  registered in `cmd/core`, and users enable, configure and log in to it from
  Settings → 音源插件. See `docs/online-sources.md`.
- Settings are in `plugin_settings` (non-secret). Sessions are in the Vault as
  `plugin.<id>` and are never returned by any endpoint. Mutations are admin-only.
- Everything is rewritten natively in Go. MIT sources are credited in `NOTICE`.
  AGPL projects were read for protocol behaviour only.
- The legacy `playback.StreamSource` stubs for NetEase and QQ are removed. Apple remains a stub.

## D31 Entitlement policy: play only what the account may play
- Each online stream is classified `full`, `preview` (the platform's own trial
  segment, with its window) or `unavailable`. A non-VIP account gets the
  official preview and the UI marks it 试听; never the full track.
- Forbidden: unblock, 解灰, song_url_match, ncmget, cross-platform grabbing, decrypting
  ncm/qmc/kgm, and any DRM bypass.
- Fallback goes only to (1) local files or (2) other platforms where the user
  is logged in and fully entitled, matched by ISRC or title + artist + duration
  (≥ 0.75), and recorded in `track_equivalents`.
- Upstream URLs and cookies stay in Core. `/api/stream/{id}` proxies, and
  `/api/online/resolve/{id}` reports the decision.

## D32 Materialize external items into the canonical tables
- When an online track is favorited, rated, added to a playlist or cloned, it
  is written to `tracks/albums/artists` with `origin=<plugin>` and id
  `<plugin>:<native>`. Favorites, ratings, history, playlists and the D29 edit
  guard therefore need no parallel store. Merge and rescan touch only `origin='local'`.
- Album covers of online items are CDN URLs, served by a 302 redirect.

## D33 Library lists default to local; federated search is opt-in
- `/api/artists|albums|tracks` default to `origin=local` unless `origin`,
  `favorite`, `album_id` or `artist_id` is given. Materialized online items
  therefore never pollute the local library.
- `/api/search` queries plugins only with `online=1`, so existing clients keep
  fast local search. Results are grouped per platform, and errors are per group.

## D34 Online-only mode
- `settings['app.mode']` ∈ `local | online | both` is set by the first-run wizard.
  The web UI hides local-only sections in online mode or when the library is empty.

## D35 Device identity and secret settings in the Vault
- Jar keys prefixed `device.` (e.g. Kugou dfid/mid, QQ guid) and `secret.` (API client credentials from `secret` config fields) are persisted to the Vault even without a login, survive logout and never count as a session. `secret.*` values are never returned by the API, never stored in `plugin_settings` and never sent as cookies.

## D36 Spotify is a metadata provider, not a music source
- Only the client-credentials grant is used, for match suggestions in the local metadata editor (search, ISRC, covers, genres, dates, popularity, artist images). No user OAuth, playlist import, catalog sections or playback. Its plugin entry has no capabilities and is excluded from source lists. Applying a suggestion is a local override (D29 rules unchanged).

## D37 Previews show only 试听
- The UI labels preview tracks with the 试听 badge only; the preview window (start/end) stays in the API for lyrics offsets and seeking, but is not displayed.

## D38 Web console uses username + password; pairing codes are for apps
- First run shows a setup page that creates the first admin (atomic: only while `users` is empty). Passwords: argon2id (m=19 MiB, t=2, p=1), policy ≥10 chars and 3 of 4 classes (or ≥16), not common, not containing the username; checked in Core and mirrored in the UI. No default password exists.
- A login issues the same `ymd_` bearer device token as pairing (`players.user_id` links it), so authorization is unchanged; the account role sets `is_admin` on all its sessions. Logout revokes the token; a password change revokes the account's other sessions. The token is kept in the browser's localStorage as before (no cookie, so no CSRF surface); it is never put in URLs except the existing media `access_token` exceptions.
- Failed logins: 8/min per IP and 5/min per username, then a 15-minute lockout (same limiter as pairing). Unknown usernames take the same time as wrong passwords.
- Escape hatches: `yimeng-core reset-password [user]` and `YIMENG_ADMIN_RESET=1` (removes all web accounts; paired apps stay).
- Pairing codes are generated only by admins in the web UI (设备 → 添加设备 / 生成配对码) or printed to the log via `/api/pair/request`.

## D39 Anonymous platform metadata/lyrics sources
网易云 / QQ音乐 / 酷狗 search, detail and lyrics are used through their anonymous public endpoints on a separate cookie-less client per platform (`internal/online/anon`), rate-limited (60/min). They feed 在线匹配, the lyrics workbench and (when listed in `providers`) the background lyrics fetch for local tracks with no lyrics. They never touch a logged-in session and work whether or not the streaming plugin is enabled. Spotify still needs only the client credentials in the Vault.

## D40 Upstream audio must be audio
Platform CDNs sometimes answer `200 audio/mpeg` with an HTML "Forbidden" page (QQ: some `sip` hosts). The stream proxy sniffs the first bytes, tries the plugin's alternate CDN hosts (`Stream.Alternates`), and otherwise returns a JSON `502 upstream_rejected` with a Chinese message; the web player turns stream errors into Chinese reasons instead of the browser's generic message.

## D41 Lyrics API for apps: any song, one scorer
Apps need lyrics for songs that are not in the library (e.g. playing from another source) and must not hold platform credentials. `GET /api/v1/lyrics/search` and `/best` take title/artist/album/duration/isrc and are open to any paired device or signed-in user (not admin-only). Core queries local/user-bound lyrics (library tracks matching the title or ISRC) and LRCLIB, NetEase, QQ and Kugou in parallel with a 12 s per-source and 15 s total budget; partial results are returned.
Scoring lives in `internal/lyrics/rank` and is shared with the web lyrics workbench, so both rank candidates the same way. Points: title 40, artist 30 (best pairwise match over split artists), album 5, duration 15 (≤2 s / ≤5 s / ≤10 s), synced 5, translation 2, source reliability 3. Similarity is `anon.Sim`: exact, pinyin-equal (so "qing tian" matches 晴天), bracket-stripped, or containment. A title with zero similarity multiplies the total by 0.3. Each candidate exposes the breakdown so clients and admins can see why it ranked where it did.
Results are cached in memory for 6 h (empty results for 30 min), keyed by the normalized query. Only cache misses are rate-limited (30/min per token, burst 10 → 429 + Retry-After). "Nothing found" is a 200 with `{found:false, message:"暂无歌词"}`, so the app can show 「暂无歌词，可在设置中配置译梦以获取歌词」. `/best` returns the LRC, plain text, the translation LRC and merged `lines[{t_ms,text,trans}]`. `romanization` is reserved; no source provides it yet. Romanised artist aliases (e.g. "Jay Chou" ↔ 周杰伦) are not resolved yet: such queries rely on the title and duration.

## D42 No Core-made recommendations
译梦 has no recommendation system of its own. 每日推荐, 私人 FM and 推荐歌单 are passed through as-is from the platform account the user logged into. Home in both the API and the web UI shows only platform sections and objective library facts: 最近添加, 最近播放, 常听, 那年今日 and 随机播放. Core never ranks or generates "recommended for you" content. The "start radio from this track" action stays: it is a user-invoked shuffle over similar local tags, not a feed.

## D43 Web IA mirrors the app
- 曲库 is one list: local tracks plus each logged-in platform's liked songs, with a 全部/本地/网易云/QQ/酷狗 filter and an A–Z rail.
- Each track carries a server-side `sort_letter`, the pinyin initial for Chinese titles.
- In online-only mode, 曲库 shows the platform favorites rather than being hidden.
- 典藏 holds only what the user favorited in Core.
- 排行榜 left the navigation: home shows one 排行榜 entry, and the charts page has a platform switcher.
- Rows start with a small cover and have no index numbers (album pages keep track numbers).
- The source appears as small grey text after the artist line, and 试听 as a lime outlined pill.
- Unplayable rows are dimmed to 40% and tapping one shows the reason.
- Now-playing surfaces carry no source badge.

## D44 — One platform cache layer

Every NetEase/QQ/Kugou catalog call goes through `online.cached()`, backed by `external_cache` (migration 0019).

- **Per-kind policy** (fresh / stale window / negative):
  - track 30d / 60d / 6h
  - album 14d / 30d / 6h
  - artist 7d / 30d / 6h
  - playlist 6h / 7d / 1h
  - charts 1h / 7d / 10m
  - lyrics 90d / 365d / 24h
  - search 10m / 1h / 10m
  - daily recommendations: per account, the key carries the date
  - user playlists 10m / 7d
- **Stale-while-revalidate:** a stale entry is returned immediately and one coalesced background refresh replaces it. If that refresh fails, the stale copy stays.
- **Coalescing:** concurrent misses on one key make a single upstream call. The fetch is detached from the first request's cancellation.
- **Negative caching:** only for `unavailable` and `bad_input`. Transient and risk-control errors are never remembered.
- **Risk control:** while a platform is in its cooldown (per-platform limiter plus 30s→15m doubling backoff in `Client`), stale copies are served without contacting it.
- **Size:** capped by `YIMENG_ONLINE_CACHE_MB` (default 256), with LRU eviction by `last_access` (touched at most hourly).
- **Hot refresh:** an hourly tick prunes, then refreshes charts and the account's daily recommendations, with paced requests.
- **Stream URLs:** not stored here. They stay in short leases that honour the platform expiry.
- **Settings:** stats and clear at `GET/DELETE /api/settings/cache` and in the 设置 → 平台缓存 section.

## D45 — App API contract (iOS client)
Aligned with the designer's mock (`ios/Tools/core-mock/core_mock.py`). All routes also live under `/api/v1`.
- **Auth:** login and pair return `{access_token (yma_, 15 min), refresh_token (the ymd_ device token), expires_in, token_type}`. `POST /auth/refresh` rotates the refresh token. The old ymd_ bearer still works.
- **Signed media URLs:** `stream_url` and `cover.url` are signed with HMAC (vault-derived key) over path + device + exp only (exp rounded to the hour, TTL 6 h). `size`/`format` are not part of the signature.
- **Covers:** `/covers/{id}?size=120|300|600|1200&format=webp|jpeg` with a strong ETag and immutable caching. `/covers/{id}/meta` returns `{blurhash, colors}`.
- **Lists:** `{items, total, cursor, next_cursor, updated_at}`, with an opaque cursor, ETag and 304.
- **Errors:** `{error, code, message}`.
- **Tracks:** `playable` is a bool, plus `entitlement` (full|preview|unavailable). Bitrate is in kbps.
- **Progress:** `/progress` is resume points only and does not write history. Last-writer by `updated_at` wins.
- **Platform pass-through:** daily, fm, recommend/playlists and user/playlists. Not logged in → 404 `not_logged_in`. 译梦 has no own recommendation (D42).
- **Events:** `/ws` and SSE `/events`, scoped to the owner.

## D46 — Merged lyrics
`/lyrics/best` (also `?track_id=`) merges matching candidates by timestamp into `lines[{t_ms, text, trans?, roma?, words?[{t_ms, d_ms, text}]}]`, with `sources` naming the provider of each part.
- **Word-timing priority:** Kugou KRC > QQ QRC > NetEase YRC.
- **QQ QRC:** deferred. It uses a non-standard DES variant, and AGPL code must not be copied.

## D47 — Reduced dream metadata
`/tracks/{id}/dream` does no audio analysis.
- **colors/blurhash:** from the cover.
- **tags:** from genre tags.
- **bpm:** only when a source provides it, else null.

## D48 — Sessions, handoff and 一起听 rooms
- **Sessions:** `/sessions` lists the caller's devices. `/sessions/handoff` reuses the player transfer.
- **Rooms:** kept in memory and ephemeral. A 6-char code and a host-authoritative state are broadcast with `server_time`.
- **弹幕:** 1–100 chars, max 5 per 10 s per device; the last 200 are kept.
- **Room events:** SSE at `/rooms/{id}/events`.
- **Web page:** `/room/{code}`.
- **Search streaming:** `/search?stream=1` streams local results first, then each platform as it finishes.
- **NetEase login:** SMS login added (captcha/sent + w/login/cellphone with the PC identity). QR keeps the iPhone identity by default; PC identity is opt-in via YIMENG_NETEASE_QR_PC=1 (it got -462 on polling).

## D49 — Per-account isolation
- **Owned per user account:** playlists (`owner_id`), favorites, ratings, play history and scrobbles (`plays.user_id`), and progress.
- **Devices:** a device belongs to the account that issued its pairing code. Web logins belong to the logged-in account.
- **Migration 0021:** existing rows move to the first admin account.
- **Admin access:** an admin may open, edit or delete any playlist by id. Listings show only the caller's own data, including for admin.
- **Other accounts' data:** playlists, devices and players return **404**, never 403, so ids cannot be probed.
- **Player control:** player control, queue, transfer, handoff and sessions are limited to devices of the same account, plus admin. WebSocket `hello` and `player.*` / `queue.*` events are filtered the same way.
  - Players that are not a paired device (server outputs) stay shared.
  - Installs without accounts stay one shared household.
- **Rooms (一起听):** unchanged. Anyone with the code can join. Members can only send 弹幕 and follow; only the host controls.
- **/system:** non-admins get a redacted view with no host paths, diagnostics or ops figures.
- **Stays server-wide:** platform logins, online caches and `/stats` aggregates.
- **Lyrics ranking:**
  - A duration mismatch now multiplies the score: ×0.8 above 10 s, ×0.55 above 20 s, ×0.3 above 45 s.
  - The last synced timestamp is checked against the track: lyrics that run past the end get ×0.4; lyrics that end before 55 % of the track get ×0.6. This needs ≥5 lines and a track of ≥60 s.
  - For an online track id, that platform's own lyrics are added as a candidate with a +15 bonus.

## D50 — Cross-platform auto source switch
- **When it applies:** a track is preview-only or unavailable on its platform.
- **What Core does:** `Resolve` plays, in order:
  1. a local equivalent;
  2. a known equivalent on another platform;
  3. a search hit on another *enabled, logged-in* platform. It must have full entitlement and match by ISRC, or by normalized title + artist with duration within 3 s (score ≥ 0.9).
- **What it never does:** use unblock or grey-listed sources, or any DRM bypass.
- **Per-account preference** (`user_prefs` key `source_switch`, migration 0022), set via `GET/PUT /me/source-switch`:
  - `enabled` (default on);
  - `priority`, a list of platform ids.
  - With switching off, only a local equivalent is used. Leases are cached per preference.
- **What clients see:**
  - Stream headers `X-Yimeng-Fallback` and `X-Yimeng-Play-Id`, and `fallback`/`play_id` on `/online/resolve/{id}`.
  - Online track detail embeds `alternatives` when the track is not fully playable.
  - `GET /tracks/{id}/alternatives` lists them on demand.

## D51 — Merged playlists
- **Mixed tracks:** a 译梦 playlist can hold local and online ids side by side.
- **Snapshots:** online tracks are stored as metadata snapshots (title, artists, album, cover, duration) in `tracks`.
- **Import:** `POST /online/{origin}/playlists/{id}/clone` creates a new playlist, or with `into` appends to one of the caller's playlists (owner-checked: 404 otherwise).
  - Unavailable tracks are kept and counted as `unavailable`.
  - `prefer_local` maps tracks to local equivalents.
- **Availability:** the last known availability is recorded in `external_status` (migration 0023) from listings, imports and resolves. Listings expose it as `entitlement` / `playable` / `preview`.
- **Unavailable or preview items:** they stay in the playlist. Playback auto-switches to an entitled source when one exists (D50).
