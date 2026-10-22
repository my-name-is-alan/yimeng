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
