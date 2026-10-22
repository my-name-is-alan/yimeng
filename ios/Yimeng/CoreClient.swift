import Foundation

/// 译梦 Core client (flagship source). Draft API v1 — see CoreModels.swift.
/// Auth: Bearer access token (Keychain), rotating refresh token, one transparent refresh+retry on 401.
/// Lists are cursor-paged; the LibraryBackend adapter maps offset paging onto remembered cursors.
/// GETs that return an ETag are cached on disk per source; revalidated with If-None-Match (304 → cached body).
actor CoreClient {
    static let shared = CoreClient()

    private(set) var base: URL?
    private(set) var accessToken = ""
    private var refreshToken = ""
    private(set) var info: CoreServerInfo?
    private var cursors: [String: String] = [:]          // "<path>?<query>@<offset>" → cursor
    private var refreshing: Task<Bool, Never>?
    var lastTrackTotal: Int?

    struct Snapshot: Sendable { let base: URL?; let token: String }
    func snapshot() -> Snapshot { Snapshot(base: base, token: accessToken) }

    private let session: URLSession = {
        let c = URLSessionConfiguration.ephemeral
        c.timeoutIntervalForRequest = 20
        c.httpAdditionalHeaders = ["User-Agent": "Yimeng/0.3 (iOS; art.yimeng.app)", "Accept": "application/json"]
        c.urlCache = nil
        return URLSession(configuration: c)
    }()
    nonisolated static let decoder: JSONDecoder = { let d = JSONDecoder(); d.keyDecodingStrategy = .convertFromSnakeCase; return d }()
    nonisolated static let encoder: JSONEncoder = { let e = JSONEncoder(); e.keyEncodingStrategy = .convertToSnakeCase; return e }()

    // MARK: connect

    nonisolated static func normalize(_ host: String) -> URL? {
        var h = host.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !h.isEmpty else { return nil }
        if !h.contains("://") { h = "https://" + h }
        while h.hasSuffix("/") { h.removeLast() }
        for s in ["/api/v1", "/api"] where h.hasSuffix(s) { h.removeLast(s.count) }
        guard let u = URL(string: h), u.host != nil else { return nil }
        return u
    }
    func configure(host: String) throws {
        guard let u = Self.normalize(host) else { throw NavidromeError.badURL }
        if u != base { cursors = [:] }
        base = u
    }
    /// No auth. Used by the connect form to show the server's name before login.
    func serverInfo() async throws -> CoreServerInfo {
        let i: CoreServerInfo = try await get("/api/v1/server/info", auth: false)
        info = i
        CoreState.setPlatforms(i.platforms ?? [])
        return i
    }
    func login(username: String, password: String) async throws {
        struct Body: Encodable { let username: String; let password: String; let deviceName: String }
        let t: CoreTokens = try await send("POST", "/api/v1/auth/login", body: Body(username: username, password: password, deviceName: Self.deviceName), auth: false)
        store(t)
    }
    func restore(access: String, refresh: String) { accessToken = access; refreshToken = refresh; CoreState.token = access }
    func logout() async {
        if !accessToken.isEmpty {
            struct B: Encodable { let refreshToken: String }
            _ = try? await raw("POST", "/api/v1/auth/logout", body: try? Self.encoder.encode(B(refreshToken: refreshToken)))
        }
        accessToken = ""; refreshToken = ""; CoreState.setPlatforms([])
        Keychain.delete(service: Self.svc, account: "access"); Keychain.delete(service: Self.svc, account: "refresh")
    }
    nonisolated static let svc = SourceKind.core.keychainService
    nonisolated static var deviceName: String { "译梦 iOS" }
    private func store(_ t: CoreTokens) {
        accessToken = t.accessToken; CoreState.token = t.accessToken
        if let r = t.refreshToken, !r.isEmpty { refreshToken = r }
        Keychain.set(accessToken, service: Self.svc, account: "access")
        if !refreshToken.isEmpty { Keychain.set(refreshToken, service: Self.svc, account: "refresh") }
    }
    /// Rotating refresh: the old refresh token is spent; concurrent 401s share one refresh.
    private func refresh() async -> Bool {
        if let r = refreshing { return await r.value }
        guard !refreshToken.isEmpty else { return false }
        let t = Task { () -> Bool in
            struct B: Encodable { let refreshToken: String }
            guard let d = try? await self.raw("POST", "/api/v1/auth/refresh", body: try? Self.encoder.encode(B(refreshToken: self.refreshToken)), auth: false),
                  d.1 == 200, let tok = try? Self.decoder.decode(CoreTokens.self, from: d.0) else { return false }
            await self.store(tok)
            return true
        }
        refreshing = t
        let ok = await t.value
        refreshing = nil
        return ok
    }

    // MARK: transport

    nonisolated static func url(_ base: URL, _ path: String, _ query: [String: String] = [:]) -> URL? {
        var c = URLComponents(url: base.appendingPathComponent(path), resolvingAgainstBaseURL: false)
        if !query.isEmpty { c?.queryItems = query.sorted { $0.key < $1.key }.map { URLQueryItem(name: $0.key, value: $0.value) } }
        return c?.url
    }
    private func raw(_ method: String, _ path: String, query: [String: String] = [:], body: Data? = nil, auth: Bool = true,
                     etag: String? = nil) async throws -> (Data, Int, HTTPURLResponse?) {
        guard let base, let u = Self.url(base, path, query) else { throw NavidromeError.badURL }
        var r = URLRequest(url: u); r.httpMethod = method
        if auth, !accessToken.isEmpty { r.setValue("Bearer \(accessToken)", forHTTPHeaderField: "Authorization") }
        if let etag { r.setValue(etag, forHTTPHeaderField: "If-None-Match") }
        if let body { r.httpBody = body; r.setValue("application/json", forHTTPHeaderField: "Content-Type") }
        let (d, resp) = try await session.data(for: r)
        let h = resp as? HTTPURLResponse
        return (d, h?.statusCode ?? 0, h)
    }
    /// Authenticated call with one refresh-and-retry on 401.
    private func call(_ method: String, _ path: String, query: [String: String] = [:], body: Data? = nil, auth: Bool = true,
                      etag: String? = nil) async throws -> (Data, Int, HTTPURLResponse?) {
        var res = try await raw(method, path, query: query, body: body, auth: auth, etag: etag)
        if auth, res.1 == 401, await refresh() { res = try await raw(method, path, query: query, body: body, auth: auth, etag: etag) }
        switch res.1 {
        case 200..<300, 304: return res
        case 401: throw NavidromeError.api(auth ? "译梦服务端登录已失效，请重新登录" : "用户名或密码错误")
        case 403: throw NavidromeError.api("译梦服务端拒绝访问")
        case 404: throw NavidromeError.api("找不到译梦服务端接口（地址对吗？）")
        default:
            if let m = (try? JSONSerialization.jsonObject(with: res.0) as? [String: Any])?["message"] as? String { throw NavidromeError.api(m) }
            throw NavidromeError.http(res.1)
        }
    }
    /// GET with ETag revalidation + per-source disk cache.
    func get<T: Decodable>(_ path: String, query: [String: String] = [:], auth: Bool = true, cache: Bool = false) async throws -> T {
        let key = path + "?" + query.sorted { $0.key < $1.key }.map { "\($0.key)=\($0.value)" }.joined(separator: "&")
        let hit = cache ? CoreETagCache.read(key) : nil
        do {
            let (d, code, h) = try await call("GET", path, query: query, auth: auth, etag: hit?.etag)
            if code == 304, let hit { return try Self.decoder.decode(T.self, from: hit.body) }
            if cache, let e = h?.value(forHTTPHeaderField: "ETag") { CoreETagCache.write(key, etag: e, body: d) }
            return try Self.decoder.decode(T.self, from: d)
        } catch let e as URLError {
            // Offline: serve the last good body if we have one.
            if let hit, let v = try? Self.decoder.decode(T.self, from: hit.body) { return v }
            throw e
        }
    }
    func send<T: Decodable, B: Encodable>(_ method: String, _ path: String, body: B, auth: Bool = true) async throws -> T {
        let (d, _, _) = try await call(method, path, body: try Self.encoder.encode(body), auth: auth)
        return try Self.decoder.decode(T.self, from: d)
    }
    func fire<B: Encodable>(_ method: String, _ path: String, body: B?, query: [String: String] = [:]) async throws {
        _ = try await call(method, path, query: query, body: try body.map { try Self.encoder.encode($0) })
    }
    func fire(_ method: String, _ path: String, query: [String: String] = [:]) async throws {
        _ = try await call(method, path, query: query)
    }

    /// Cursor paging behind an offset API: offset 0 = first page; later offsets reuse the cursor stored for them.
    func page<T: Codable & Sendable>(_ path: String, _ q: [String: String], offset: Int, limit: Int) async throws -> CorePage<T> {
        let sig = path + "?" + q.sorted { $0.key < $1.key }.map { "\($0.key)=\($0.value)" }.joined(separator: "&")
        var query = q; query["limit"] = "\(limit)"
        if offset > 0 {
            guard let c = cursors["\(sig)@\(offset)"] else { return CorePage(items: [], nextCursor: nil) }   // past the end
            query["cursor"] = c
        }
        let p: CorePage<T> = try await get(path, query: query, cache: offset == 0)
        if let n = p.nextCursor, !n.isEmpty { cursors["\(sig)@\(offset + p.items.count)"] = n }
        return p
    }

    // MARK: endpoints

    func home() async throws -> CoreHome {
        try await get("/api/v1/home", cache: true)
    }
    func lyrics(_ trackId: String) async -> CoreLyrics? { try? await get("/api/v1/lyrics/best", query: ["track_id": trackId], cache: true) }
    func dream(_ trackId: String) async -> CoreDream? { try? await get("/api/v1/tracks/\(trackId)/dream", cache: true) }
    func charts(origin: String) async throws -> CoreCharts { try await get("/api/v1/charts", query: ["origin": origin], cache: true) }
    func playlists(scope: String) async throws -> [CorePlaylist] {
        let p: CorePage<CorePlaylist> = try await page("/api/v1/playlists", ["scope": scope], offset: 0, limit: 200); return p.items
    }
    func suggest(_ q: String) async -> [String] { ((try? await get("/api/v1/search/suggest", query: ["q": q]) as CoreSuggest)?.items) ?? [] }
    func searchAll(_ q: String, online: Bool) async throws -> CoreSearch { try await get("/api/v1/search", query: ["q": q, "online": online ? "1" : "0"]) }
    func progress(ids: [String]) async -> [String: Int] {
        guard !ids.isEmpty, let l: CoreProgressList = try? await get("/api/v1/progress", query: ["ids": ids.joined(separator: ",")]) else { return [:] }
        return Dictionary(l.items.map { ($0.trackId, $0.positionMs) }, uniquingKeysWith: { a, _ in a })
    }
    func putProgress(_ id: String, ms: Int) async {
        struct B: Encodable { let positionMs: Int }
        try? await fire("PUT", "/api/v1/progress/\(Self.path(id))", body: B(positionMs: ms))
    }
    func renamePlaylist(_ id: String, name: String) async throws {
        struct B: Encodable { let name: String }
        try await fire("PATCH", "/api/v1/playlists/\(id)", body: B(name: name))
    }

    // Logged-in platform content (assumed endpoints — see report).
    func daily(_ origin: String) async throws -> CoreDaily { try await get("/api/v1/online/\(origin)/daily", cache: true) }
    /// nil = platform has no FM (404 not_supported).
    func fm(_ origin: String) async throws -> [CoreTrack]? {
        do { let p: CorePage<CoreTrack> = try await get("/api/v1/online/\(origin)/fm"); return p.items }
        catch NavidromeError.api(let m) where m.contains("找不到") { return nil }
    }
    func fmFeedback(_ origin: String, trackId: String, action: String) async {
        struct B: Encodable { let trackId: String; let action: String }
        try? await fire("POST", "/api/v1/online/\(origin)/fm/feedback", body: B(trackId: trackId, action: action))
    }
    func recommendPlaylists(_ origin: String) async throws -> [CorePlaylist] {
        let p: CorePage<CorePlaylist> = try await get("/api/v1/online/\(origin)/recommend/playlists", cache: true); return p.items
    }
    func userPlaylists(_ origin: String, type: String) async throws -> [CorePlaylist] {
        let p: CorePage<CorePlaylist> = try await get("/api/v1/online/\(origin)/user/playlists", query: ["type": type], cache: true); return p.items
    }

    /// Online ids look like `netease:12345` — keep them in one path segment.
    nonisolated static func path(_ id: String) -> String { id.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed.subtracting(CharacterSet(charactersIn: "/"))) ?? id }

    // MARK: media URLs

    /// Pre-signed URLs from payloads are used as-is; otherwise build the plain path (header auth via `mediaHeaders`).
    nonisolated static func coverURL(base: URL, idOrURL: String, size: Int) -> URL? {
        if idOrURL.hasPrefix("http") { return URL(string: idOrURL) }
        if idOrURL.hasPrefix("/") { return URL(string: idOrURL, relativeTo: base)?.absoluteURL }
        return url(base, "/api/covers/\(path(idOrURL))", ["size": "\(bucket(size))", "format": "webp"])
    }
    nonisolated static func bucket(_ s: Int) -> Int { [120, 300, 600, 1200].first { $0 >= s } ?? 1200 }
    nonisolated static func streamURL(base: URL, id: String) -> URL? {
        if let s = CoreState.stream(id), let u = URL(string: s, relativeTo: base)?.absoluteURL { return u }
        return url(base, "/api/stream/\(path(id))")
    }
}

// MARK: - Shared state readable from any thread (signed stream URLs, blurhashes, accounts, filter)

enum CoreState {
    private final class Box: @unchecked Sendable {
        let lock = NSLock()
        var stream: [String: String] = [:]
        var blur: [String: String] = [:]
        var colors: [String: [String]] = [:]
        var platforms: [CorePlatform] = []
        var origin: String? = nil
        var token = ""
    }
    private static let b = Box()
    private static func with<T>(_ f: (Box) -> T) -> T { b.lock.lock(); defer { b.lock.unlock() }; return f(b) }
    static func remember(_ t: CoreTrack) {
        with { b in
            if let s = t.streamUrl { b.stream[t.id] = s }
            if let c = t.cover { remember(cover: c, in: b) }
        }
    }
    static func remember(cover c: CoreCover?) { guard let c else { return }; with { remember(cover: c, in: $0) } }
    private static func remember(cover c: CoreCover, in b: Box) {
        guard let u = c.url else { return }
        let k = CoreState.coverKey(u)
        if let h = c.blurhash { b.blur[k] = h }
        if let col = c.colors { b.colors[k] = col }
    }
    static func stream(_ id: String) -> String? { with { $0.stream[id] } }
    /// Cover identity independent of size/signature: `/api/covers/{id}` → id, else the path.
    static func coverKey(_ s: String) -> String {
        guard let u = URLComponents(string: s) else { return s }
        if let r = u.path.range(of: "/api/covers/") { return String(u.path[r.upperBound...]) }
        return u.path
    }
    static func blurhash(for url: URL) -> String? { with { $0.blur[coverKey(url.absoluteString)] } }
    static func colors(for url: URL?) -> [String]? { guard let url else { return nil }; return with { $0.colors[coverKey(url.absoluteString)] } }
    static func setPlatforms(_ a: [CorePlatform]) { with { $0.platforms = a } }
    static var platforms: [CorePlatform] { with { $0.platforms } }
    static func platforms(can c: String) -> [CorePlatform] { platforms.filter { $0.can(c) } }
    static var originFilter: String? { get { with { $0.origin } } set { with { $0.origin = newValue } } }
    static var token: String { get { with { $0.token } } set { with { $0.token = newValue } } }
    /// Header auth for unsigned media (covers/streams) on the Core host.
    static func mediaHeaders(for url: URL) -> [String: String] {
        guard NavidromeClient.core, !url.absoluteString.contains("sig="), !token.isEmpty else { return [:] }
        return ["Authorization": "Bearer \(token)"]
    }
}

// MARK: - ETag disk cache (per source, small JSON bodies)

enum CoreETagCache {
    struct Hit { let etag: String; let body: Data }
    private static var dir: URL {
        let d = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("core-etag", isDirectory: true).appendingPathComponent(MusicSources.activeId, isDirectory: true)
        try? FileManager.default.createDirectory(at: d, withIntermediateDirectories: true)
        return d
    }
    private static func file(_ key: String) -> URL {
        var h: UInt64 = 1469598103934665603
        for b in key.utf8 { h = (h ^ UInt64(b)) &* 1099511628211 }
        return dir.appendingPathComponent(String(h, radix: 16))
    }
    static func read(_ key: String) -> Hit? {
        guard let d = try? Data(contentsOf: file(key)), let nl = d.firstIndex(of: 10) else { return nil }
        return Hit(etag: String(decoding: d[..<nl], as: UTF8.self), body: d[(nl + 1)...])
    }
    static func write(_ key: String, etag: String, body: Data) {
        var d = Data(etag.utf8); d.append(10); d.append(body)
        try? d.write(to: file(key), options: .atomic)
    }
}
