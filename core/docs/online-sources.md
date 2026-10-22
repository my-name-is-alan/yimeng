# Online sources (音源插件)

Every online platform is a **plugin** inside Core, written natively in Go
(`internal/online/<id>`). Users enable, configure and log in to each plugin
from the web Settings UI (设置 → 音源插件). Credentials live only in the Core
Vault and never reach clients.

## 1. Plugin interface (`internal/online/plugin.go`)

A plugin is a `Factory func(Env) (Plugin, error)`. `Env` carries the
per-account HTTP `Client` and the validated `Config`. The minimum is
`Manifest()`. Capabilities are optional interfaces, discovered with type
assertions and declared in `Manifest.Capabilities`:

| Interface | Methods | Capability |
|---|---|---|
| `Authenticator` | `StartQR`, `PollQR`, `LoginCookie`, `LoginPassword`, `Account`, `Refresh` | per `LoginMethods` (qr, cookie, password) |
| `Catalog` | `Search`, `Tracks`, `Album`, `Artist`, `Playlist`, `Lyrics` | `search`, `lyrics` |
| `Discovery` | `Charts`, `RecommendTracks`, `RecommendPlaylists`, `DiscoverPlaylists` | `charts`, `recommend`, `discover` |
| `UserLibrary` | `UserPlaylists` (includes the platform "liked songs" list, `kind:"liked"`) | `user_playlists` |
| `Streamer` | `Resolve(nativeID, quality) → Stream` | `stream` |

DTOs are platform-neutral: `Track`, `Album`, `Artist`, `Playlist`, `Lyrics`
and `Account`. Ids are `"<plugin id>:<native id>"` (e.g. `netease:186016`).
`Track.Playable` is the listing-time **entitlement** hint (`full | preview |
unavailable`). `Stream` carries `URL` and `Header`, both `json:"-"`, which
never leave Core. It also carries entitlement, quality, format and the
preview window (`preview_start_ms`, `preview_end_ms`).

Errors are sentinels mapped to HTTP codes by the gateway:
- `ErrNeedsLogin` → 401 `plugin_login_required`
- `ErrRiskControl` → 429 `risk_control`
- `ErrDisabled` → 409 `plugin_disabled`
- `ErrUnknown` → 404
- `ErrNotSupported` → 501
- `ErrUnavailable` → 404
- `ErrUpstream` → 502

## 2. Registry and config schema (`registry.go`, migration 0015)

- `plugin_settings(id, enabled, config JSON, updated_at)` stores only non-secret settings.
- Every manifest gets two common fields prepended:
  - `quality` (select over the plugin's qualities)
  - `rate_per_min` (number, 5–600)
- `ValidateConfig` rejects unknown keys, invalid options and out-of-range numbers, then fills defaults. The UI renders the form generically from `manifest.config` (types: select, number, bool, text).
- Sessions are stored in the Vault entry `plugin.<id>` (AES-256-GCM, existing `vault_entries`). The value is the cookie jar plus the login method. On start, `Load` restores the jar, so a session survives restarts.
- `Status` is the client view: manifest, enabled, config, logged_in, login_method, account (nickname, avatar, VIP), cooldown_until and last_error. It never includes cookies.
- QR codes are rendered server-side to a PNG data URL (`rsc.io/qr`). The client only polls.

## 3. Per-account HTTP client (`client.go`)

- Each plugin has one `Client`, holding the account's cookie jar.
  - A dirty flag triggers persisting to the Vault after any `Set-Cookie`.
  - Expired or empty cookies are deleted from the jar.
- A token-bucket limiter (`golang.org/x/time/rate`) enforces `rate_per_min`.
- Risk-control backoff:
  - `Penalize()` on HTTP 429 or a platform risk code (NetEase -462/405/8810/8821) starts an exponential cooldown: 30 s, 1 m, 2 m … capped at 15 m.
  - While cooling down, requests fail fast with `ErrRiskControl`. `Reward()` resets the strikes.
  - The Service calls these automatically after each plugin call.

## 4. Cache (`external_cache`, `cache.go`)

Normalized DTO JSON keyed `origin:kind:id`, with these TTLs:

| kind | TTL |
|---|---|
| track | 7 d |
| album | 3 d |
| artist | 1 d |
| playlist | 30 min |
| search | 10 min |
| lyrics | 30 d |
| charts / recommend / discover | 1 h |
| user playlists | 10 min (per account) |

- User-specific keys include the account id.
- Stream URLs are **not** stored in this table. They live in an in-memory lease map, valid until 30 s before the URL expires.
- Expired rows are pruned hourly.

## 5. Materialization and track links

External tracks that a user touches (favorite, rating, playlist add, clone) are
upserted into the canonical `tracks/albums/artists` tables with
`origin=<plugin id>` and their full id (`library.UpsertExternal`). Because of
that, favorites, ratings, history and playlists work unchanged, and the D29
guard keeps them read-only for metadata editing.
- Album `cover_path` holds the CDN URL. `GET /api/albums/{id}/cover` answers `302` to it (CSP allows `https:` images).
- Library list endpoints default to `origin=local`. Pass `origin=<id>` to list one platform. Favorites, album and artist filters show all origins.
- Merge and rescans touch only `origin='local'` rows.

`track_equivalents(track_id, alt_track_id, confidence, method)` links the same
recording across origins. `MatchScore` gives:
- ISRC → 1.0
- normalized title + artist with |Δduration| ≤ 3 s → 0.9
- ≤ 8 s → 0.75
- no duration → 0.7

Track DTOs are annotated with `local_id` when a local copy is known.

## 6. Entitlement policy and just-in-time resolve

Core behaves like the official client. It plays only what the logged-in
account (or anonymous access) is entitled to.

- `full`: the platform returned a full-length URL for this account.
- `preview`: the platform returned only its official trial segment (NetEase `freeTrialInfo{start,end}`). The UI shows a 试听 badge and the window; Core never tries to obtain more.
- `unavailable`: no URL (paid album, removed, region). The stream answers `403 not_entitled` with the platform's reason.

Core **never** uses unblock, 解灰, song_url_match, ncmget or any third-party grabbing. It never decrypts ncm/qmc/kgm and never bypasses DRM.

`Service.Resolve(id)` fallback order, used only when the requested platform is not `full`:
1. A known local equivalent, or a local `FindMatch` (≥ 0.75) → play the local file.
2. A known external equivalent on **another platform the user is logged into** that resolves `full`.
3. Search other logged-in, enabled plugins by title + artist. Accept a candidate with match ≥ 0.75 that resolves `full`, then record the equivalence.
4. Otherwise return the original result (preview or unavailable).

`GET /api/online/resolve/{id}` returns the decision (entitlement, preview
window, `fallback`, `play_id`, `stream_url`) without the upstream URL. The web
player calls it before playing an online track, because `<audio>` cannot read
response headers.

`GET /api/stream/{id}` with an online id is the proxy:
- It resolves (lease cached) and serves a local fallback from disk.
- Otherwise it fetches the CDN URL with the plugin's headers and forwards `Range`.
- It sets `X-Yimeng-Entitlement`, `X-Yimeng-Play-Id`, `X-Yimeng-Fallback` and `X-Yimeng-Preview-Start-Ms`/`End-Ms`.
- On upstream 403/404/410 it invalidates the lease and re-resolves once.
- Clients never see the CDN URL or cookies. Transcoding is not applied to online streams.

## 7. API

All routes require a device token. Mutations of plugin state require the admin role.

| Route | Purpose |
|---|---|
| `GET /api/plugins[?check=1]` | list with status |
| `GET /api/plugins/{id}` | one plugin |
| `PUT /api/plugins/{id}` (admin) | `{enabled?, config?}` |
| `POST /api/plugins/{id}/login` (admin) | `{method:"cookie",cookie}` or `{method:"password",username,password}` |
| `POST /api/plugins/{id}/login/qr` (admin) | → `{key, content, image, expires_at}` |
| `GET /api/plugins/{id}/login/qr/{key}` (admin) | → `waiting | scanned | confirmed | expired` |
| `POST /api/plugins/{id}/refresh` (admin) | refresh session |
| `DELETE /api/plugins/{id}/session` (admin) | logout (Vault entry removed) |
| `GET /api/home` | sections per enabled plugin: daily songs*, my playlists*, recommended playlists, charts (* = logged in) |
| `GET /api/search?q=&online=1[&origins=]` | adds `online: [{origin, name, result, error}]` (federated, parallel, 6 s budget each) |
| `GET /api/playlists?scope=mine|recommend|discover|charts[&origin=]` | external playlists grouped by platform; no scope = local playlists |
| `GET /api/charts[?origin=]` | charts grouped by platform |
| `GET /api/online/{origin}/{tracks|albums|artists|playlists}/{id}` | detail; playlists return `{playlist, counts{full,preview,unavailable,local}}` |
| `POST /api/online/{origin}/playlists/{id}/clone` | `{name?, prefer_local?=true, skip_unavailable?}` → local static playlist (local copies substituted) |
| `GET /api/online/favorites[?origin=]` | each account's cloud "liked songs" |
| `GET /api/online/resolve/{id}` | play decision (see §6) |
| `GET /api/stream/{id}` | proxy for online ids |
| `GET /api/tracks/{id}/lyrics` | online ids use the plugin (with `translation`) |
| `PUT /api/favorites/track/{id}`, `PUT /api/ratings/track/{id}` | materialize online tracks first |
| `GET/PUT /api/settings/mode` | first-run mode `local | online | both`, plus `local_tracks` and `online_enabled` |

## 8. Online-only mode

- The first-run wizard (web) asks local / online / both and stores `app.mode`.
- The UI hides local-only sections (scan, library roots, local albums) when the mode is `online`, or when the library has no local tracks and the mode is not `local`.
- Home, search, charts and playlists work with plugins alone.

## 9. NetEase plugin (`internal/online/netease`)

Protocol knowledge comes from MIT sources (see `NOTICE`), re-implemented in Go.

- **weapi:** AES-128-CBC twice (preset key, then a random 16-char key, IV `0102030405060708`), plus a raw RSA of the reversed key → `params` and `encSecKey`.
- **eapi:** AES-128-ECB over `uri-36cd479b6b5-json-36cd479b6b5-md5(...)`, upper-hex.
- Login:
  - QR (`login/qrcode/unikey`, `client/login`; 800 expired, 801 waiting, 802 scanned, 803 ok)
  - cookie (must contain `MUSIC_U`, validated via account)
  - phone and email with MD5 password
  - refresh and account status
- Catalog: cloudsearch, `v3/song/detail`, album, artist, lyric (+ translation), `v6/playlist/detail` (fetches missing `trackIds`).
- Discovery: toplist, recommend songs and resources (logged in), personalized and top playlists (anonymous).
- User: `user/playlist` (specialType 5 = liked).
- Stream: `song/enhance/player/url/v1` with level `standard | exhigh | lossless | hires`. Entitlement comes from `privilege` and `freeTrialInfo`.
- Tests are offline (`netease_test.go`): fixed crypto vectors cross-checked with OpenSSL, plus an httptest fake that decrypts eapi to route by URI.
- Live check (manual, read-only): `YIMENG_LIVE_SMOKE=1 go run ./cmd/netease-smoke "陈奕迅 十年"`. Optionally set `YIMENG_NETEASE_COOKIE` (never printed).

## 9a. Kugou plugin (`internal/online/kugou`)

Protocol knowledge from MIT sources (see `NOTICE`) and our own live probes; AGPL projects were read for protocol knowledge only.

- **Device identity:** `register_dev` (AES with a random 6-char key, base64 body, RSA PKCS#1 key wrap) yields `dfid`; `mid = decimal(md5(guid))`. Stored as `device.*` jar keys, which the Registry persists to the Vault even while anonymous and keeps on logout (they never count as a login). Re-registered weekly; falls back to dfid `-`.
- **Signing:** Android `md5(salt + sorted k=v + body + salt)`; web signature for `login-user.kugou.com` QR. Default params dfid/mid/uuid/appid 1005/clientver 20489/clienttime (+ token/userid when logged in). Backend chosen with `x-router` on `gateway.kugou.com`.
- **Login:** QR (status 1 waiting, 2 scanned, 4 ok, 0 expired), cookie (`token`+`userid` or `KuGoo=KugooID&t`), SMS (`send_mobile_code` + `login_by_verifycode`, via `POST /api/plugins/kugou/login/sms`), refresh (`login_by_token`), account (`get_my_info` + `get_union_vip`).
- **Catalog:** search (song/album/author/special), detail via privilege lite, album (page size 50), artist, playlist (`pl-`), rank (`rank-`), lyrics (`lyrics.kugou.com` search → download `fmt=lrc`; KRC is never used), charts, everyday recommend, recommended/discover playlists, user playlists.
- **Stream:** `/v5/url` with `key = md5(hash+salt+mid+userid)`; full URL at the requested quality, then 128k; a clip URL (`/yp/p_0_…`) is never reported as full. Otherwise `IsFreePart=1` gives the official preview clip with its `hash_offset` window; otherwise unavailable.
- Tests: `kugou/plugin_test.go` with trimmed live fixtures. Smoke: `go run ./cmd/kugou-smoke`.

## 9b. QQ Music plugin (`internal/online/qq`)

- **Transport:** `u.y.qq.com/cgi-bin/musicu.fcg` batched requests. Most modules use the web comm (`ct:24, platform:yqq.json`); the vkey module requires ct 24. Search (`music.search.SearchCgiService.DoSearchForQQMusicDesktop`) needs the desktop comm (`ct:"19", cv:"1859"`) without remoteplace/grp, and each batched sub-response is decoded separately.
- **Login (our own implementation):** `xlogin` (pt_login_sig) → `ptqrshow` (PNG + qrsig, sent to the UI as a data URL) → poll `ptqrlogin` with `ptqrtoken = hash33(qrsig)` (66 waiting, 67 scanned, 65 expired, 0 ok) → follow `check_sig` → `p_skey` → `graph.qq.com/oauth2.0/authorize` (g_tk from p_skey, client_id 100497308) → code → `QQConnectLogin.LoginServer.QQLogin` → `musicid/musickey`. The intermediate QQ cookies live in a throwaway jar; only uin/qm_keyst/refresh keys plus the device guid reach the Vault. Cookie login (`uin` + `qm_keyst`), refresh (`music.login.LoginServer.Login`), account (`GetLoginUserInfo` + VIP query). Password login is not supported.
- **Catalog:** search (song/album/singer/playlist), track detail, album, artist, diss playlists (`pl-`), toplists (`top-`), lyrics (`GetPlayLyricInfo`, plain LRC), charts, recommend (radio 99 when logged in, else new songs), hot recommend playlists, playlist plaza by tag, user playlists (dirId 201 = liked).
- **Stream:** vkey ladder F000 (FLAC) / M800 (320k) / M500 (128k) / C400 (AAC) capped by the quality setting; an empty `purl` means denied. Paid songs fall back to the official trial file `RS02…mp3` with `try_begin/try_end`; otherwise unavailable. No decryption of any format.
- Tests: `qq/qq_test.go` with trimmed live fixtures (hash33/gtk vectors included). Smoke: `go run ./cmd/qq-smoke`.

## 9c. Spotify (metadata provider only)

- **Scope:** client-credentials Web API reads for the metadata editor's 在线匹配: track/album/artist search, ISRC lookup (`isrc:` search), album art, genres (artist genres for the best track match; album genres when Spotify returns them), release dates, label/UPC, popularity, artist images. No user OAuth, no playlist/library import, no home/catalog sections, no playback or StreamSource.
- **Wiring:** `internal/adapters/spotify` implements `metadata.MetadataProvider`; the editor's matcher is `metadata.Multi{MusicBrainz, Spotify}`. Spotify is skipped (ErrUnavailable, no network) until it is enabled and both credentials are set. Applying a candidate is a local override; covers go through `POST /api/albums/{id}/cover/from-match`, which only downloads from Spotify's image CDN (`*.scdn.co`, `*.spotifycdn.com`) and stores the image like an uploaded cover.
- **Settings:** `internal/online/spotify` is a metadata-only plugin entry (no capabilities, excluded from `EnabledIDs`, so it never appears as a music source). Its `client_id` / `client_secret` are `secret` config fields: the Registry moves them into the Vault (`secret.*` jar keys), never into `plugin_settings`, never returns them (status only says `secrets: {key: true}`) and never sends them as cookies. The access token lives only in memory.
- **Local bootstrap:** when `YIMENG_SPOTIFY_CLIENT_ID/SECRET` are in the environment and the Vault has no value yet, Core seeds them once (logs only the field name). `YIMENG_SPOTIFY_API_BASE_URL` / `_AUTH_BASE_URL` override the endpoints.
- Tests: `adapters/spotify/spotify_test.go` (fake API with trimmed live fixtures: token caching, 401 refresh, ISRC, bad credentials not echoed, CDN allowlist, Multi skipping) and `online/spotify/plugin_test.go` (secrets never in status/plugin_settings, Vault round-trip, clear, disabled = no secret). Smoke: `go run ./cmd/spotify-smoke` with the env vars set (prints neither credentials nor token).

## 10. Known gaps

- One account per platform.
- Logged-in flows are covered by fixtures only and have not been exercised against a real account: NetEase (QR, cookie, phone, email, daily recommend, user playlists, VIP `full` URLs); Kugou (QR confirm, SMS, refresh, account, user playlists, VIP full URLs); QQ (QR confirm → QQ Connect → musicu login, refresh, account, user playlists, radio recommend). SMS was never sent live.
- QQ password login is not supported; QQ/Kugou `hires` qualities are not offered.
- Spotify feeds only the on-demand match UI; the background MusicBrainz enricher does not use it. Artist images are returned by the API but there is no local artist-image editor to apply them yet.
- No cloud write-back (liking on the platform).
