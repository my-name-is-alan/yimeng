# Security review — Core + web admin (2026-10-22)

**Scope:** Go Core (`core/`) and the embedded web admin (`core/internal/webadmin/ui`). The iOS, Android and desktop clients are out of scope.

**Method:** multi-round manual review, plus:
- `govulncheck`, `gosec`, `staticcheck`
- `go test -race ./...`
- `npm audit`: N/A. `packages/shared` has no dependencies, and the web admin is dependency-free vanilla JS.
- A web UI sweep for native controls.

No secrets are recorded here.

## Summary by severity

| # | Sev | Finding | Status |
|---|-----|---------|--------|
| 1 | High | **Cross-account data access (IDOR).** Playlists, favorites, ratings, history, scrobbles and progress were global; any account could read or modify others' playlists by id. | **Fixed:** per-account ownership (D49, migration 0021), 404 for foreign ids, IDOR tests (`idor_test.go`). |
| 2 | High | **Player hijack.** Any paired device could control, transfer or hand off any other account's player, and saw every player's state over WebSocket. | **Fixed:** `canControl` guard on `/players/{id}/*`, transfer target, handoff and WS inbound; filtered WS hello and events. |
| 3 | High | **Device takeover.** A non-admin could revoke any device, including the admin's. | **Fixed** (5443a0f): self or same owner only, else 404. The device list is filtered for non-admins. |
| 4 | High | **SSRF.** Remote cover fetch and the CDN stream proxy could be pointed at internal addresses. | **Fixed** (5443a0f): `safehttp` blocks non-public IPs at dial time, which also covers redirects and DNS rebinding. |
| 5 | Medium | **Admin operations open to non-admins:** library scan, metadata enrich, artist-split settings. | **Fixed:** these are admin-only. |
| 6 | Medium | **`/system` leaked host paths**, root errors, diagnostics and backup/device counts to any account. | **Fixed:** redacted for non-admins. |
| 7 | Medium | **No framing protection** outside the web UI, so the API, room page and JSON could be framed (clickjacking). | **Fixed:** global `X-Frame-Options: DENY`, `frame-ancestors 'none'`, COOP same-origin. The web UI keeps its strict CSP. |
| 8 | Low | **No HTTP server limits:** no idle timeout and no header-size cap. | **Fixed:** `IdleTimeout` 2 min, `MaxHeaderBytes` 64 KiB. JSON bodies are already capped at 64 KiB and M3U imports at 4 MiB. |
| 9 | Low | **Data race in a test fake** (`anon_test.go`). | **Fixed:** atomic counter. `go test -race ./...` is clean. |
| 10 | Low | **staticcheck:** one dead store (spotify) and one loop that always exits (artistimage). | **Fixed.** |
| 11 | Low | **Native `confirm()` and native `title` tooltips** in the web UI. | **Fixed:** `confirmDlg` and app-styled tooltips (`tooltip.js`; text via textContent). Selects, checkboxes, radios, ranges, file inputs and scrollbars were already app-styled or hidden. |
| 12 | Low | **Per-username login lockout** lets an attacker lock a known account for the lockout window (lockout DoS). | **Not fixed (accepted):** the per-IP limit still applies, and the trade-off favours brute-force resistance. |
| 13 | Low | **The refresh token is the long-lived `ymd_` device bearer.** A stolen token stays valid until revoked. | **Not fixed (by design):** devices can be revoked individually and tokens are stored hashed. |
| 14 | Low | **`/stats` aggregates are server-wide**, not per account. | **Not fixed (accepted):** counts only, no per-user rows. |
| 15 | Info | **WebSocket `InsecureSkipVerify`** (any Origin is accepted). | **Accepted:** auth is a bearer or `access_token` only, with no cookies, so cross-site WebSocket hijacking is not possible. |
| 16 | Info | **gosec:** SHA-1 and MD5 for cache keys and platform protocol signatures; PKCS#1 v1.5 RSA required by the Kugou protocol. | **Accepted:** not used for confidentiality; an annotated lint ignore is in place. |
| 17 | Info | **Default bind is `0.0.0.0:8741`.** | **Accepted:** LAN server by design. Configurable via `server.bind` / `ZHILE_BIND`. TLS is supported. |
| 18 | Info | **govulncheck:** no reachable vulnerabilities. | — |

## Areas reviewed with no issue found

- **Auth:**
  - argon2id (t=2, m=19 MiB)
  - dummy hash for unknown users (no username timing oracle)
  - per-IP and per-username lockout
  - constant-time pairing-code compare
  - single-use pairing codes with expiry
  - tokens stored hashed
  - "Don't create a default password": first-run setup only
- **Signed URLs:** HMAC over path + expiry, `hmac.Equal`, path-bound.
- **Query tokens in logs:** the access log records the path only; Core never logs query strings.
- **CORS/CSRF:** no cookie auth and no permissive CORS, so CSRF is not applicable.
- **SQL injection:** all queries are parameterised. The only literal (`ownerLit`) is SQL-quoted from the authenticated user id.
- **Path traversal:** streams and covers are resolved by id from the DB, never from client paths. M3U import never opens the referenced files.
- **XSS:** the web UI builds DOM via `h()` / textContent. `innerHTML` is used only for static SVG icon constants. The room page uses textContent. The CSP is `default-src 'self'`.
- **Vault and secrets:** platform credentials stay server-side and are never returned by the API. Spotify secrets come from the environment and are not logged.
- **Uploads and DoS:**
  - bodies are capped (64 KiB JSON, 4 MiB M3U, image uploads capped)
  - WebSocket read limit 16 KiB
  - lyrics lookups are rate-limited
  - 弹幕 limited to 5 per 10 s per device, with the last 200 kept
  - the online cache is bounded by TTL

## Follow-ups (not done)

- Optional `ReadTimeout` per route. A global one would break long uploads and streams.
- Optional per-account `/stats`.

## D59 developer API review (2026-10-22)

- **Secrets.** API keys are stored only as SHA-256 hashes and shown once. App
  passwords must be reversible for Subsonic MD5 token auth; they live encrypted in
  the vault (master key) and are random 24-char values unique per app, so a leak
  does not reveal the login password and is revocable on its own. Plaintext `p=` is
  refused unless the user opted in per app password. Credential lists show only a prefix.
- **Least privilege.** Scope check runs in `requireAuth` before any handler. Default
  deny for credential/device/session/pairing/Last.fm-key endpoints. `admin` scope
  needs an admin account; an admin's key without it is treated as a member.
- **Brute force.** Per user+IP lockout (10 / 15 min) on Subsonic password auth plus
  the global rate limiter; failures and lockouts feed security alerts.
  `apiKey`+`u` together → error 43 (spec), no fallback chaining.
- **Isolation.** Every credential resolves to its owner's account context (D49);
  tests cover cross-account list/revoke and playlists.
- **CORS.** Exact-origin allow-list, validated (http/https origin only); no
  credentials mode, so a malicious page cannot ride a cookie (there are none anyway).
- **Accepted risk.** MD5 token auth is weak by design of Subsonic; recommend apiKey
  to client authors. Legacy `p=ymd_…` device tokens still work for old setups.
- **Follow-ups.** Per-key rate limits; OAuth device flow for public apps.
