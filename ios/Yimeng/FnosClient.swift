import Foundation
import CryptoKit

/// 飞牛音乐 (fnOS Music) private web API — `<NAS>/music/api/v1`, envelope `{code,msg,data}`.
/// Not officially documented; protocol taken from public community clients (see docs/fnos-music.md).
/// Auth: POST /user/password-login {username, password: sha256(pw), deviceId} → data.userToken,
/// then every request carries `Cookie: music-token=<token>` (+ `X-Music-API: v1` except the login routes).
/// Account = the separate 飞牛「音乐」app account, not the fnOS system account.
actor FnosClient {
    static let shared = FnosClient()
    static let tokenExpiredCodes: Set<Int> = [120001, 99999, 401]

    private let session: URLSession = {
        let c = URLSessionConfiguration.ephemeral
        c.httpAdditionalHeaders = ["User-Agent": "Yimeng/0.3 (iOS; art.yimeng.app)", "Accept": "application/json"]
        c.timeoutIntervalForRequest = 45
        c.httpShouldSetCookies = false
        return URLSession(configuration: c)
    }()
    private(set) var root: URL?          // e.g. http://nas:5666  (no /music)
    private(set) var token = ""
    private var genreGuids: [String: String] = [:]

    struct Snapshot: Sendable { let root: URL?; let token: String }
    func snapshot() -> Snapshot { Snapshot(root: root, token: token) }

    /// Accepts `nas:5666`, `http://nas:5666/`, `https://x.fnos.net/music`, `…/music/api/v1`.
    nonisolated static func normalize(_ host: String) -> URL? {
        var h = host.trimmingCharacters(in: .whitespacesAndNewlines)
        if !h.contains("://") { h = "http://" + h }
        while h.hasSuffix("/") { h.removeLast() }
        for suffix in ["/music/api/v1", "/music"] where h.hasSuffix(suffix) { h.removeLast(suffix.count) }
        guard var u = URL(string: h), let host = u.host else { return nil }
        // FN Connect (*.fnos.net) only answers on HTTPS; plain http gets dropped.
        if isRelay(host), u.scheme == "http", var c = URLComponents(url: u, resolvingAgainstBaseURL: false) { c.scheme = "https"; u = c.url ?? u }
        return u
    }
    /// FN Connect relay: its nginx bounces every request without `mode=relay` to the fnos.net landing page, whose JS
    /// probes direct vs relay (`/api/v1/fn/con`, `/trimcon`) and then gets the cookie set. The cookie is static, so we just send it.
    nonisolated static func isRelay(_ host: String?) -> Bool { host.map { $0 == "fnos.net" || $0.hasSuffix(".fnos.net") } ?? false }
    nonisolated static func cookieHeader(root: URL?, token: String?) -> String? {
        var parts: [String] = []
        if isRelay(root?.host) { parts.append("mode=relay") }
        if let token, !token.isEmpty { parts.append("music-token=\(token)") }
        return parts.isEmpty ? nil : parts.joined(separator: "; ")
    }
    nonisolated static func sha256(_ s: String) -> String {
        SHA256.hash(data: Data(s.utf8)).map { String(format: "%02x", $0) }.joined()
    }
    nonisolated static var deviceId: String {
        if let d = UserDefaults.standard.string(forKey: "ym.fnos.deviceId") { return d }
        let d = (0..<16).map { _ in String(format: "%02x", UInt8.random(in: 0...255)) }.joined()
        UserDefaults.standard.set(d, forKey: "ym.fnos.deviceId"); return d
    }
    nonisolated static func api(_ root: URL, _ path: String) -> URL { root.appendingPathComponent("music/api/v1" + path) }

    func configure(host: String) throws {
        guard let u = Self.normalize(host) else { throw NavidromeError.badURL }
        root = u
        Self.installCookie(root: u, token: "")
    }

    func login(username: String, password: String) async throws {
        guard let root else { throw NavidromeError.badURL }
        // Reachability first so a wrong address reads as such, not as a bad password.
        _ = try? await request("GET", "/initialization/state", query: [:], auth: false)
        let d = try await request("POST", "/user/password-login",
                                  body: ["username": username, "password": Self.sha256(password), "deviceId": Self.deviceId], auth: false)
        guard let t = (d as? [String: Any])?["userToken"] as? String, !t.isEmpty else { throw NavidromeError.api("登录响应缺少 userToken") }
        token = t
        Self.installCookie(root: root, token: t)
        Keychain.set(password, service: SourceKind.fnos.keychainService, account: username)
    }
    func logout() { token = ""; genreGuids = [:] }

    /// Cover loads go through CoverCache's default-config URLSession, which uses the shared cookie jar.
    nonisolated static func installCookie(root: URL, token: String) {
        guard let host = root.host else { return }
        let sec = root.scheme == "https" ? "TRUE" : "FALSE"
        var jar: [(String, String)] = []
        if !token.isEmpty { jar.append(("music-token", token)) }
        if isRelay(host) { jar.append(("mode", "relay")) }
        for (n, v) in jar {
            if let c = HTTPCookie(properties: [.domain: host, .path: "/", .name: n, .value: v, .secure: sec]) { HTTPCookieStorage.shared.setCookie(c) }
        }
    }

    // MARK: transport

    private func request(_ method: String, _ path: String, query: [String: String] = [:], body: [String: Any]? = nil, auth: Bool = true) async throws -> Any? {
        guard let root else { throw NavidromeError.badURL }
        var c = URLComponents(url: Self.api(root, path), resolvingAgainstBaseURL: false)!
        if !query.isEmpty { c.queryItems = query.sorted { $0.key < $1.key }.map { URLQueryItem(name: $0.key, value: $0.value) } }
        guard let url = c.url else { throw NavidromeError.badURL }
        var req = URLRequest(url: url); req.httpMethod = method
        if auth { req.setValue("v1", forHTTPHeaderField: "X-Music-API") }
        if let ck = Self.cookieHeader(root: root, token: auth ? token : nil) { req.setValue(ck, forHTTPHeaderField: "Cookie") }
        if let body { req.setValue("application/json", forHTTPHeaderField: "Content-Type"); req.httpBody = try JSONSerialization.data(withJSONObject: body) }
        let (data, resp) = try await session.data(for: req)
        let http = (resp as? HTTPURLResponse)?.statusCode ?? 0
        if resp.url?.host == "fnos.net" { throw NavidromeError.api("FN Connect 中转拒绝了请求（被重定向到 fnos.net 落地页）") }
        if http == 401 { throw NavidromeError.api("飞牛音乐登录已失效，请重新登录") }
        guard (200..<300).contains(http) else { throw NavidromeError.http(http) }
        return try Self.unwrap(data)
    }

    nonisolated static func unwrap(_ data: Data) throws -> Any? {
        guard let obj = try JSONSerialization.jsonObject(with: data) as? [String: Any] else { throw NavidromeError.decode }
        let code = (obj["code"] as? Int) ?? Int(obj["code"] as? String ?? "") ?? -1
        if code == 0 { return obj["data"] }
        if code == 120001 { throw NavidromeError.api("用户名或密码错误，或登录已失效（用飞牛「音乐」里的独立账号）") }
        if tokenExpiredCodes.contains(code) { throw NavidromeError.api("飞牛音乐登录已失效，请重新登录") }
        throw NavidromeError.api((obj["msg"] as? String).map { $0.isEmpty ? "飞牛错误 \(code)" : $0 } ?? "飞牛错误 \(code)")
    }

    private func page(_ path: String, _ q: [String: String]) async throws -> (list: [[String: Any]], total: Int) {
        let d = try await request("GET", path, query: q) as? [String: Any] ?? [:]
        return ((d["list"] as? [[String: Any]]) ?? [], (d["total"] as? Int) ?? 0)
    }
    /// Our offset/count → fnOS 1-based page/size (offset is always a multiple of count at call sites).
    nonisolated static func pageQuery(offset: Int, count: Int) -> [String: String] {
        let size = max(count, 1); return ["page": "\(offset / size + 1)", "size": "\(size)"]
    }

    // MARK: parsing → shared ND models

    nonisolated static func num(_ v: Any?) -> Int? { (v as? Int) ?? (v as? Double).map { Int($0) } ?? (v as? String).flatMap { Int($0) } }
    nonisolated static func artistNames(_ a: Any?) -> String {
        ((a as? [[String: Any]]) ?? []).compactMap { $0["name"] as? String }.joined(separator: " / ")
    }
    nonisolated static func parseSong(_ t: [String: Any]) -> NDSong? {
        guard let id = t["guid"] as? String, !id.isEmpty else { return nil }
        let al = t["album"] as? [String: Any]
        let spec = t["audioSpec"] as? [String: Any]
        let ms = num(t["duration"])                         // milliseconds
        var s = NDSong(id: id, title: (t["title"] as? String) ?? "未知标题", artist: artistNames(t["artists"]),
                       album: al?["name"] as? String, albumId: al?["guid"] as? String,
                       coverId: (t["coverId"] as? String) ?? (al?["coverId"] as? String),
                       duration: ms.map { $0 / 1000 }, track: num(t["trackNo"]))
        s.suffix = (spec?["format"] as? String)?.lowercased()
        s.bitRate = num(spec?["bitrate"]).map { $0 > 10_000 ? $0 / 1000 : $0 }
        s.samplingRate = num(spec?["sampleRate"]); s.bitDepth = num(spec?["bitDepth"]); s.channelCount = num(spec?["channel"])
        return s
    }
    nonisolated static func parseAlbum(_ a: [String: Any]) -> NDAlbum? {
        guard let id = a["guid"] as? String, !id.isEmpty else { return nil }
        let year = num(a["year"]) ?? num(a["releaseDate"]).flatMap { d in
            d > 10_000_000_000 ? Calendar.current.component(.year, from: Date(timeIntervalSince1970: Double(d) / 1000)) : (d > 3000 ? Calendar.current.component(.year, from: Date(timeIntervalSince1970: Double(d))) : d) }
        let artists = a["artists"] as? [[String: Any]]
        return NDAlbum(id: id, name: (a["name"] as? String) ?? "未知专辑",
                       artist: artistNames(artists).isEmpty ? ((a["artist"] as? [String: Any])?["name"] as? String ?? (a["artistName"] as? String ?? "")) : artistNames(artists),
                       artistId: artists?.first?["guid"] as? String, coverId: a["coverId"] as? String, year: year, songCount: num(a["trackCount"]))
    }
    nonisolated static func parseArtist(_ a: [String: Any]) -> NDArtist? {
        guard let id = a["guid"] as? String, !id.isEmpty else { return nil }
        return NDArtist(id: id, name: (a["name"] as? String) ?? "未知歌手", albumCount: num(a["albumCount"]), coverId: a["coverId"] as? String)
    }
    nonisolated static func parsePlaylist(_ a: [String: Any]) -> NDPlaylist? {
        guard let id = a["guid"] as? String, !id.isEmpty else { return nil }
        return NDPlaylist(id: id, name: (a["name"] as? String) ?? "未知歌单", songCount: num(a["trackCount"]), coverId: a["coverId"] as? String)
    }

    // MARK: library

    func albums(offset: Int, size: Int) async throws -> [NDAlbum] {
        try await page("/album/list", Self.pageQuery(offset: offset, count: size)).list.compactMap(Self.parseAlbum)
    }
    func albumTracks(_ id: String) async throws -> [NDSong] {
        try await page("/track/album-detail/list", ["albumGUID": id, "page": "1", "size": "500"]).list.compactMap(Self.parseSong)
    }
    func album(id: String) async throws -> (NDAlbum, [NDSong]) {
        let songs = try await albumTracks(id)
        var head: NDAlbum?
        if let d = try? await request("GET", "/album/detail", query: ["guid": id]) as? [String: Any] { head = Self.parseAlbum((d["album"] as? [String: Any]) ?? d) }
        let f = songs.first
        let a = head ?? NDAlbum(id: id, name: f?.album ?? "", artist: f?.artist ?? "", coverId: f?.coverId, year: nil, songCount: songs.count)
        return (a.artist.isEmpty ? NDAlbum(id: a.id, name: a.name, artist: f?.artist ?? "", artistId: a.artistId, coverId: a.coverId ?? f?.coverId, year: a.year, songCount: a.songCount ?? songs.count) : a, songs)
    }
    func artists() async throws -> [NDArtist] {
        let d = try await request("GET", "/artist/list-all") as? [String: Any]
        return ((d?["list"] as? [[String: Any]]) ?? []).compactMap(Self.parseArtist)
    }
    func artistTracks(_ id: String, size: Int = 300) async throws -> [NDSong] {
        try await page("/track/artist-detail/list", ["artistGUID": id, "page": "1", "size": "\(size)"]).list.compactMap(Self.parseSong)
    }
    /// fnOS has no artist→albums route; derive the album set from the artist's tracks.
    func artistAlbums(_ id: String) async throws -> [NDAlbum] {
        var seen = Set<String>(); var out: [NDAlbum] = []
        for s in try await artistTracks(id) { if let aid = s.albumId, seen.insert(aid).inserted {
            out.append(NDAlbum(id: aid, name: s.album ?? "", artist: s.artist, coverId: s.coverId, year: nil, songCount: nil)) } }
        return out
    }
    func songs(offset: Int, count: Int) async throws -> [NDSong] {
        try await page("/track/list", Self.pageQuery(offset: offset, count: count)).list.compactMap(Self.parseSong)
    }
    func songTotal() async -> Int? { try? await page("/track/list", ["page": "1", "size": "1"]).total }
    func search(_ q: String, count: Int) async throws -> (albums: [NDAlbum], artists: [NDArtist], songs: [NDSong]) {
        let p = ["q": q, "page": "1", "size": "\(count)"]
        async let al = try? page("/search/album", p); async let ar = try? page("/search/artist", p); async let tr = page("/search/track", p)
        return ((await al)?.list.compactMap(Self.parseAlbum) ?? [], (await ar)?.list.compactMap(Self.parseArtist) ?? [], try await tr.list.compactMap(Self.parseSong))
    }
    func genres() async throws -> [NDGenre] {
        let l = try await page("/genre/list", ["page": "1", "size": "500"]).list
        for g in l { if let n = g["name"] as? String, let id = g["guid"] as? String { genreGuids[n] = id } }
        return l.compactMap { g in (g["name"] as? String).map { NDGenre(name: $0, songCount: Self.num(g["trackCount"]) ?? 0, albumCount: 0) } }
    }
    func songsByGenre(_ name: String, offset: Int, count: Int) async throws -> [NDSong] {
        if genreGuids[name] == nil { _ = try await genres() }
        guard let id = genreGuids[name] else { return [] }
        var q = Self.pageQuery(offset: offset, count: count); q["genreGUID"] = id
        return try await page("/track/genre-detail/list", q).list.compactMap(Self.parseSong)
    }
    // Favorites: song-level only on fnOS.
    func favoriteSongs() async throws -> [NDSong] {
        try await page("/favorite-track/list", ["page": "1", "size": "-1"]).list.compactMap(Self.parseSong)
    }
    func setFavorite(_ on: Bool, song id: String) async throws {
        _ = try await request("POST", on ? "/favorite-track/create" : "/favorite-track/delete", body: ["trackGUID": id])
    }
    func playlists() async throws -> [NDPlaylist] {
        try await page("/playlist/list", ["page": "1", "size": "200"]).list.compactMap(Self.parsePlaylist)
    }
    func playlistTracks(_ id: String) async throws -> [NDSong] {
        try await page("/track/playlist-detail/list", ["playlistGUID": id, "page": "1", "size": "1000"]).list.compactMap(Self.parseSong)
    }

    // MARK: media URLs (auth via Cookie header / shared cookie jar, not the URL)

    nonisolated static func coverURL(root: URL, coverId: String, size: Int) -> URL? {
        var c = URLComponents(url: api(root, "/static/cover"), resolvingAgainstBaseURL: false)
        c?.queryItems = [URLQueryItem(name: "coverId", value: coverId), URLQueryItem(name: "size", value: "\(size)")]
        return c?.url
    }
    /// Original file (FLAC passthrough when the source is FLAC); supports Range.
    nonisolated static func streamURL(root: URL, guid: String) -> URL? {
        var c = URLComponents(url: api(root, "/track/stream"), resolvingAgainstBaseURL: false)
        c?.queryItems = [URLQueryItem(name: "guid", value: guid)]
        return c?.url
    }
}
