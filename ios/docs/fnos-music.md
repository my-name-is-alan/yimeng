# 飞牛音乐 (fnOS Music) source — protocol notes

Status: **implemented from community docs, not yet verified against a real server.**

## Findings
- No official/public API and **no Subsonic/OpenSubsonic** endpoint in the built-in 飞牛「音乐」app.
  (OpenSubsonic on fnOS only via 3rd-party apps such as MusicFree, which would work with our Navidrome source.)
- Private web API at `<NAS>/music/api/v1` (default `http://<NAS>:5666`), envelope `{code,msg,data}`.
- Account: the **separate account created inside 飞牛「音乐」**, not the fnOS system login
  (fnOS OAuth `/user/auth-login` is not open to 3rd parties).
- Login `POST /user/password-login {username, password: sha256(pw), deviceId(32 hex)}` → `data.userToken`.
- Auth on every other call: `Cookie: music-token=<token>`; `X-Music-API: v1` header except the login routes.
- Token invalid: HTTP 401, `code 99999 INVALID TOKEN`; `120001` = bad credentials / please login again.
- Library: `/album/list`, `/artist/list(-all)`, `/track/list`, `/track/album-detail/list?albumGUID`,
  `/track/artist-detail/list?artistGUID`, `/genre/list`, `/track/genre-detail/list?genreGUID`,
  `/search/{track,album,artist}?q`, `/playlist/list`, `/track/playlist-detail/list?playlistGUID`,
  `/favorite-track/{list,create,delete}` (song-level favorites only), `/lyric/list?trackGUID`.
  Paging `page` (1-based) + `size`; durations in **ms**.
- Media: `/static/cover?coverId=&size=`, `/track/stream?guid=` (original file, Range supported; transcode via `/track/transcode`).
- Remote: FN Connect relay (`Cookie: mode=relay`), FN ID lookup via a signed cloud API — not implemented.
- `authx` request signature (md5 of prefix + path + nonce + ts + md5(payload) + key) is reported by one blog for
  every call; the newest public client (FeiNiuMusic) only uses it for the FN Connect cloud lookup and talks to the
  music API with the cookie alone. **We do not ship any authx constants.** If a server rejects cookie-only
  requests, add a signer (keys must come from the user's own server web bundle) + an AVAssetResourceLoader
  for signed Range requests.

## Sources
- https://blog.zwbcc.cn/archives/f966c451-281c-4d3a-a8dc-838b109e13df (Android client write-up, endpoint table)
- https://github.com/kuilei0926/FeiNiuMusic (`lib/app/services/feiniu/api_client.dart`, `api_models.dart`)
- https://www.infowe.site/post/36 (fnOS auth / music login reverse-engineering, v0.9.24)
- https://music.http5.cn/docs/guide/fnmusic.html (separate 音乐 account, `/music` address, song-only favorites)
- https://club.fnnas.com/forum.php?mod=viewthread&tid=64012 (MusicFree: OpenSubsonic on fnOS)
