# Core overnight progress

Branch: `feat/core-sqlite-scanner` · local commits only · no remote · no Docker build.

| # | Item | Status | Commit |
|---|---|---|---|
| 1 | Backup/restore + admin role | done | `4ad5560` |
| 2 | Lyrics (embedded + sidecar LRC) | done | `c4fc025` |
| 3 | History / scrobble / favorites / ratings | done | `1694404` |
| 4 | Playlists + smart + M3U | done | `7886fcf` |
| 5 | ReplayGain / R128 + ebur128 analysis | done | `70b8d1f` |
| 6 | Structured logging + `/api/system` | done | a483986 |
| 7 | mDNS `_yimeng._tcp` | done | 1112484 |
| 8 | Multi-player transfer + online/offline WS | done | 474c46e |
| 9 | Admin APIs (devices, roots, overrides, album merge/split) | done | 0c8f936 |
| 10 | Cover thumbnails (pure-Go, webp/jpeg) | done | 89d6cd6 |
| 11 | Metadata provider registry + lyrics provider stub | done | `65027f1` |
| 12 | HTTPS + trusted proxy + rate limits | done | `e5b4571` |
| 13 | Offline download endpoint | done | `dedfcb9` |
| 14 | Subsonic API layer | done | `6b50626` |
| 15 | StreamSource account management API | done | `901578b` |
| 16 | AIFF / APE / DSF / DFF | done | `d30be52` |
| E1 | Pinyin + fuzzy search | done | `aa38d23` |
| E2 | Radio / auto-continue | done | `c7ac85d` |
| E3 | Classical / multi-disc | done | `6f46277` |
| E4 | Genre normalization + sort names | done | `c7ac85d` |
| E5 | Library health reports | done | `6f46277` |
| E6 | Stats / on-this-day | done | `6f46277` |
| E7 | Embedded web admin + setup wizard | done | `6f46277` |
| E8 | Sleep timer + session resume | done | `c7ac85d` |
| E9 | 100k-track perf | done | `6f46277` |
| E10 | Config hot-reload | done | `c7ac85d` |
| E11 | `/api/v1` alias | done | `c7ac85d` |
| E12 | TS types → `packages/shared` | done | `6f46277` |

## Web admin + player (`feat/web-admin`)

The UI is embedded at `/admin/` (`internal/webadmin/ui`, vanilla ES modules, no build step). It is in Chinese and follows the iOS visual style.

Features:
- Pairing and login; device management.
- Live dashboard: scan progress, jobs, health and logs.
- Library: albums, artists, tracks and search, including pinyin and initials.
- Playlists, smart playlists and M3U.
- Web player: queue, shuffle, repeat, synced lyrics and Media Session.
- Metadata editing: cover replace, MusicBrainz match with confidence scores, and batch edit.
- Lyrics workbench: LRCLIB search, bind, edit, upload, offset and tap-to-sync.
- Settings: roots, rescan, providers, transcoding, backups and sources.

New endpoints (admin-only):
- `GET|PUT|DELETE /api/tracks/{id}/lyrics(/user)`
- `GET /api/tracks/{id}/lyrics/search`
- `PUT|DELETE /api/albums/{id}/cover`
- `GET /api/tracks/{id}/match`
- `GET /api/albums/{id}/match`
- `POST /api/overrides/batch`
- `GET /api/system/logs`
- `GET /api/settings/runtime`

Migration `0013_user_edits` adds `user_lyrics` and `user_covers`. Both survive rescans.

Covers: WebP thumbnails require cgo. `CGO_ENABLED=0` builds now fall back to JPEG.

## Online source plugins (`feat/online-sources`)

Design: `docs/online-sources.md`. Decisions: D30–D34. Credits: `NOTICE`.

- Framework (`internal/online`):
  - plugin interfaces
  - registry with schema-validated config and Vault sessions
  - per-account client (cookie jar, rate limiter, risk-control backoff)
  - TTL cache
  - federated search, discovery and user-library facade
  - just-in-time resolve with the entitlement policy and local/other-account fallback
- Migration `0015_online`: `plugin_settings`, `external_cache`, `track_equivalents`, and origin indexes.
- Gateway:
  - `/api/plugins…`
  - `/api/home`
  - `/api/charts`
  - `/api/playlists?scope=`
  - `/api/online/{origin}/{kind}/{id}`
  - clone
  - cloud favorites
  - resolve
  - `/api/stream/{id}` proxy for online ids
  - online lyrics
  - `search?online=1`
  - `/api/settings/mode`
- NetEase plugin:
  - weapi and eapi crypto
  - QR, cookie, phone and email login
  - search and detail; album, artist, playlist and lyrics
  - toplist, recommend, personalized and top playlists
  - user playlists and liked songs
  - `song_url_v1` with entitlement
  - Offline tests. Live smoke: `cmd/netease-smoke` (anonymous search, charts and lyrics verified live; VIP songs resolve as 30 s preview).
- Web UI: 音源插件 settings, online home, platform-grouped search, charts, playlist scopes, online album/artist/playlist pages with clone, 试听 badge in the player, first-run mode wizard.
- Not verified with a real logged-in account: all login flows, daily recommend, user playlists, VIP full URLs.
- Kugou plugin: device register (dfid/mid in the Vault), Android/web signing, QR/cookie/SMS login, refresh, account; search, detail, lyrics (LRC), album, artist, playlists, ranks, everyday recommend, recommended/discover playlists, user playlists; `/v5/url` with full/preview/unavailable. Verified live anonymously (search, VIP → 60 s official preview, free → full 320k, lyrics, charts, everyday, playlists, QR challenge).
- QQ Music plugin: own ptlogin QR → QQ Connect → musicu login, cookie login, refresh, account; musicu search/detail/lyrics/album/artist/playlists/toplists/recommend/plaza/user playlists; vkey ladder → official RS02 trial → unavailable. Verified live anonymously (search, VIP → trial window, free → full 128k, lyrics, album, artist, charts, recommend, 511-track playlist, QR challenge).
- Spotify: metadata provider only (client credentials, Vault-stored secret settings, feeds 在线匹配 in the editor with ISRC/cover/genre/release/popularity; cover apply endpoint). Live read-only smoke passed.
- Web: SMS login form, per-platform cookie hints, 试听 badge without time window, 校对歌词 button follows track changes in the fullscreen player, mobile last row clear of the now-playing bar, secret settings fields, 在线匹配 with provider badges and covers.
- Not done: logged-in flows against real accounts (all platforms), multi-account support, cloud write-back.

## Web accounts (username + password)
- Migration 0016 `users` + `players.user_id`; `internal/auth/users.go` (argon2id, strength policy, setup/login with lockout, roles, change/reset password, remove-all); `/api/auth/{setup,login,logout,password}`, `/api/users` CRUD (admin); `/api/pair/codes` is admin-only.
- Web: first-run setup page with strength meter, username/password login, 账号 section in 设置 (change password), 用户 page (add/edit role/reset password/delete), 设备 page shows the account per session and 添加设备 / 生成配对码.
- CLI `yimeng-core reset-password`, env `YIMENG_ADMIN_RESET=1`. Tests: `auth/users_test.go`, `gateway/users_test.go`.
