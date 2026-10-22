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
