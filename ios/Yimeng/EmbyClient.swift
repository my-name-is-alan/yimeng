import Foundation

/// Emby / Jellyfin REST client (same API family). Docs:
/// https://dev.emby.media/doc/restapi/User-Authentication.html · https://dev.emby.media/doc/restapi/Audio-Streaming.html
/// https://dev.emby.media/reference/RestAPI/UserService/postUsersAuthenticatebyname.html
/// https://dev.emby.media/reference/RestAPI/PlaystateService/postSessionsPlaying.html · https://api.jellyfin.org
/// Auth: either AuthenticateByName {Username, Pw} → AccessToken + User.Id, or an API key (+ user pick).
/// The token goes out as `X-Emby-Token` (and `api_key` on stream URLs). Secrets live in the Keychain only.
actor EmbyClient {
    static let shared = EmbyClient()
    static let ticksPerSecond = 10_000_000.0

    private let session: URLSession = {
        let c = URLSessionConfiguration.ephemeral
        c.httpAdditionalHeaders = ["User-Agent": "Yimeng/0.3 (iOS; art.yimeng.app)", "Accept": "application/json"]
        c.timeoutIntervalForRequest = 45
        return URLSession(configuration: c)
    }()
    private(set) var base: URL?
    private(set) var token = ""
    private(set) var userId = ""
    private(set) var product = "Emby"            // "Emby" | "Jellyfin"
    private var entryIds: [String: [String]] = [:]

    struct ServerInfo: Sendable, Equatable { let product: String; let name: String; let version: String; let id: String }
    struct User: Sendable, Equatable, Hashable { let id: String; let name: String; let isAdmin: Bool }
    struct Snapshot: Sendable { let base: URL?; let token: String; let userId: String; let product: String }
    func snapshot() -> Snapshot { Snapshot(base: base, token: token, userId: userId, product: product) }

    nonisolated static var deviceId: String {
        if let d = UserDefaults.standard.string(forKey: "ym.emby.deviceId") { return d }
        let d = UUID().uuidString; UserDefaults.standard.set(d, forKey: "ym.emby.deviceId"); return d
    }
    nonisolated static var appVersion: String { Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "0.3" }
    /// Keeps a user-entered `/emby` (Emby's own prefix); strips `/web/...` pasted from the browser.
    nonisolated static func normalize(_ host: String) -> URL? {
        var h = host.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !h.isEmpty else { return nil }
        if !h.contains("://") { h = "http://" + h }
        guard var c = URLComponents(string: h), c.host != nil else { return nil }
        if let r = c.path.range(of: "/web") { c.path = String(c.path[..<r.lowerBound]) }
        while c.path.hasSuffix("/") { c.path.removeLast() }
        c.query = nil; c.fragment = nil
        return c.url
    }
    nonisolated static func authHeader(userId: String?, token: String?) -> String {
        var parts = ["Client=\"译梦\"", "Device=\"iPhone\"", "DeviceId=\"\(deviceId)\"", "Version=\"\(appVersion)\""]
        if let userId, !userId.isEmpty { parts.insert("UserId=\"\(userId)\"", at: 0) }
        if let token, !token.isEmpty { parts.append("Token=\"\(token)\"") }
        return parts.joined(separator: ", ")
    }

    // MARK: connect

    func configure(host: String) throws {
        guard let u = Self.normalize(host) else { throw NavidromeError.badURL }
        base = u
    }
    /// Public, unauthenticated. Tells Emby from Jellyfin.
    func serverInfo() async throws -> ServerInfo {
        let o = try await json("GET", "/System/Info/Public", auth: false) as? [String: Any] ?? [:]
        let info = Self.parseServerInfo(o)
        product = info.product
        return info
    }
    nonisolated static func parseServerInfo(_ o: [String: Any]) -> ServerInfo {
        let pn = (o["ProductName"] as? String) ?? ""
        return ServerInfo(product: pn.localizedCaseInsensitiveContains("jellyfin") ? "Jellyfin" : "Emby",
                          name: (o["ServerName"] as? String) ?? "", version: (o["Version"] as? String) ?? "", id: (o["Id"] as? String) ?? "")
    }
    func login(username: String, password: String) async throws {
        let o = try await json("POST", "/Users/AuthenticateByName", body: ["Username": username, "Pw": password], auth: false)
        guard let d = o as? [String: Any], let t = d["AccessToken"] as? String, let uid = (d["User"] as? [String: Any])?["Id"] as? String else {
            throw NavidromeError.api("登录响应缺少 AccessToken")
        }
        token = t; userId = uid
    }
    /// API-key mode. Returns the users the key can act as (one entry for a user-scoped key).
    func useAPIKey(_ key: String) async throws -> [User] {
        token = key; userId = ""
        if let me = try? await json("GET", "/Users/Me") as? [String: Any], let u = Self.parseUser(me) { return [u] }
        let list = try await json("GET", "/Users") as? [[String: Any]] ?? []
        return list.compactMap(Self.parseUser)
    }
    nonisolated static func parseUser(_ o: [String: Any]) -> User? {
        guard let id = o["Id"] as? String, let n = o["Name"] as? String else { return nil }
        return User(id: id, name: n, isAdmin: ((o["Policy"] as? [String: Any])?["IsAdministrator"] as? Bool) ?? false)
    }
    func select(userId: String) { self.userId = userId }
    /// Saved session (token + user from Keychain/config): just verify it still works.
    func restore(token: String, userId: String) { self.token = token; self.userId = userId }
    func validate() async throws {
        guard !userId.isEmpty else { throw NavidromeError.api("未选择 Emby 用户") }
        _ = try await items(["IncludeItemTypes": "Audio", "Recursive": "true", "Limit": "1"])
    }
    func logout() async {
        if !token.isEmpty { _ = try? await json("POST", "/Sessions/Logout") }
        token = ""; userId = ""
    }

    // MARK: transport

    private func json(_ method: String, _ path: String, query: [String: String] = [:], body: Any? = nil, auth: Bool = true) async throws -> Any? {
        guard let base else { throw NavidromeError.badURL }
        var c = URLComponents(url: base.appendingPathComponent(path), resolvingAgainstBaseURL: false)!
        if !query.isEmpty { c.queryItems = query.sorted { $0.key < $1.key }.map { URLQueryItem(name: $0.key, value: $0.value) } }
        guard let url = c.url else { throw NavidromeError.badURL }
        var req = URLRequest(url: url); req.httpMethod = method
        let a = Self.authHeader(userId: auth ? userId : nil, token: auth ? token : nil)
        req.setValue("Emby " + a, forHTTPHeaderField: "X-Emby-Authorization")
        req.setValue("MediaBrowser " + a, forHTTPHeaderField: "Authorization")   // Jellyfin's preferred form
        if auth, !token.isEmpty { req.setValue(token, forHTTPHeaderField: "X-Emby-Token") }
        if let body { req.setValue("application/json", forHTTPHeaderField: "Content-Type"); req.httpBody = try JSONSerialization.data(withJSONObject: body) }
        let (data, resp) = try await session.data(for: req)
        let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
        switch code {
        case 200..<300: return data.isEmpty ? nil : try? JSONSerialization.jsonObject(with: data)
        case 401: throw NavidromeError.api(auth ? "\(product) 登录已失效或 API Key 无效" : "用户名或密码错误")
        case 403: throw NavidromeError.api("\(product) 拒绝访问（权限不足）")
        case 404: throw NavidromeError.api("找不到 \(product) 接口，检查服务器地址（Emby 可能需要加 /emby）")
        default: throw NavidromeError.http(code)
        }
    }
    private func items(_ q: [String: String]) async throws -> (list: [[String: Any]], total: Int) {
        var q = q
        q["Fields"] = q["Fields"] ?? "PrimaryImageAspectRatio,ProductionYear,ChildCount,SongCount,AlbumCount,Genres,DateCreated,MediaSources,UserData"
        q["EnableImageTypes"] = "Primary"; q["ImageTypeLimit"] = "1"
        let o = try await json("GET", "/Users/\(userId)/Items", query: q) as? [String: Any] ?? [:]
        return ((o["Items"] as? [[String: Any]]) ?? [], (o["TotalRecordCount"] as? Int) ?? 0)
    }
    nonisolated static func paging(_ offset: Int, _ size: Int) -> [String: String] { ["StartIndex": "\(offset)", "Limit": "\(size)"] }

    // MARK: parsing → ND models

    nonisolated static func num(_ v: Any?) -> Int? { (v as? Int) ?? (v as? Double).map { Int($0) } ?? (v as? String).flatMap { Int($0) } }
    nonisolated static func hasPrimary(_ o: [String: Any]) -> Bool { ((o["ImageTags"] as? [String: Any])?["Primary"]) != nil }
    nonisolated static func parseAlbum(_ o: [String: Any]) -> NDAlbum? {
        guard let id = o["Id"] as? String, let name = o["Name"] as? String else { return nil }
        let aa = (o["AlbumArtists"] as? [[String: Any]])?.first
        return NDAlbum(id: id, name: name, artist: (o["AlbumArtist"] as? String) ?? (aa?["Name"] as? String) ?? "",
                       artistId: aa?["Id"] as? String, coverId: hasPrimary(o) ? id : nil,
                       year: num(o["ProductionYear"]), songCount: num(o["SongCount"]) ?? num(o["ChildCount"]))
    }
    nonisolated static func parseArtist(_ o: [String: Any]) -> NDArtist? {
        guard let id = o["Id"] as? String, let name = o["Name"] as? String else { return nil }
        return NDArtist(id: id, name: name, albumCount: num(o["AlbumCount"]) ?? num(o["ChildCount"]), coverId: hasPrimary(o) ? id : nil)
    }
    nonisolated static func parsePlaylist(_ o: [String: Any]) -> NDPlaylist? {
        guard let id = o["Id"] as? String, let name = o["Name"] as? String else { return nil }
        var p = NDPlaylist(id: id, name: name, songCount: num(o["ChildCount"]) ?? num(o["SongCount"]), coverId: hasPrimary(o) ? id : nil)
        p.duration = num(o["RunTimeTicks"]).map { Int(Double($0) / ticksPerSecond) }
        return p
    }
    nonisolated static func parseSong(_ o: [String: Any]) -> NDSong? {
        guard let id = o["Id"] as? String, let name = o["Name"] as? String else { return nil }
        let artists = ((o["Artists"] as? [String]) ?? []).joined(separator: " / ")
        let cover: String? = hasPrimary(o) ? id : (o["AlbumPrimaryImageTag"] != nil ? (o["AlbumId"] as? String) : nil)
        var s = NDSong(id: id, title: name, artist: artists.isEmpty ? ((o["AlbumArtist"] as? String) ?? "") : artists,
                       album: o["Album"] as? String, albumId: o["AlbumId"] as? String, coverId: cover,
                       duration: num(o["RunTimeTicks"]).map { Int(Double($0) / ticksPerSecond) }, track: num(o["IndexNumber"]))
        let src = (o["MediaSources"] as? [[String: Any]])?.first
        let audio = ((src?["MediaStreams"] as? [[String: Any]]) ?? []).first { ($0["Type"] as? String) == "Audio" }
        s.suffix = ((src?["Container"] as? String) ?? (o["Container"] as? String))?.lowercased()
        s.bitRate = (num(audio?["BitRate"]) ?? num(src?["Bitrate"])).map { $0 / 1000 }
        s.samplingRate = num(audio?["SampleRate"]); s.bitDepth = num(audio?["BitDepth"]); s.channelCount = num(audio?["Channels"])
        return s
    }
    /// Jellyfin `/Audio/{id}/Lyrics` → LRC-ish lines (Start in ticks). Emby: lyrics arrive as subtitle streams; not mapped yet.
    nonisolated static func parseLyrics(_ o: [String: Any]) -> [(TimeInterval, String)] {
        ((o["Lyrics"] as? [[String: Any]]) ?? []).compactMap { l in
            guard let t = l["Text"] as? String else { return nil }
            return (Double(num(l["Start"]) ?? 0) / ticksPerSecond, t)
        }
    }

    // MARK: media URLs

    nonisolated static func imageURL(base: URL, id: String, size: Int) -> URL? {
        var c = URLComponents(url: base.appendingPathComponent("/Items/\(id)/Images/Primary"), resolvingAgainstBaseURL: false)
        c?.queryItems = [URLQueryItem(name: "maxWidth", value: "\(size)"), URLQueryItem(name: "quality", value: "90")]
        return c?.url
    }
    /// maxKbps 0 → static original file (FLAC / ALAC / E-AC-3 untouched, seekable); else universal → MP3 transcode.
    nonisolated static func streamURL(base: URL, id: String, token: String, userId: String, maxKbps: Int, playSessionId: String) -> URL? {
        var c: URLComponents?
        if maxKbps <= 0 {
            c = URLComponents(url: base.appendingPathComponent("/Audio/\(id)/stream"), resolvingAgainstBaseURL: false)
            c?.queryItems = [.init(name: "static", value: "true"), .init(name: "DeviceId", value: deviceId), .init(name: "PlaySessionId", value: playSessionId), .init(name: "api_key", value: token)]
        } else {
            c = URLComponents(url: base.appendingPathComponent("/Audio/\(id)/universal"), resolvingAgainstBaseURL: false)
            c?.queryItems = [.init(name: "UserId", value: userId), .init(name: "DeviceId", value: deviceId),
                             .init(name: "MaxStreamingBitrate", value: "\(maxKbps * 1000)"), .init(name: "Container", value: "mp3,aac,m4a"),
                             .init(name: "TranscodingContainer", value: "mp3"), .init(name: "TranscodingProtocol", value: "http"),
                             .init(name: "AudioCodec", value: "mp3"), .init(name: "PlaySessionId", value: playSessionId), .init(name: "api_key", value: token)]
        }
        return c?.url
    }

    // MARK: playback reporting (play counts, 最近播放, resume on other clients)

    enum PlayEvent: String { case start = "/Sessions/Playing", progress = "/Sessions/Playing/Progress", stopped = "/Sessions/Playing/Stopped" }
    nonisolated static func playBody(id: String, position: TimeInterval, paused: Bool, playSessionId: String, transcoding: Bool) -> [String: Any] {
        ["ItemId": id, "PlaySessionId": playSessionId, "PositionTicks": Int64(position * ticksPerSecond), "IsPaused": paused,
         "CanSeek": !transcoding, "PlayMethod": transcoding ? "Transcode" : "DirectStream", "MediaSourceId": id]
    }
    func report(_ e: PlayEvent, id: String, position: TimeInterval, paused: Bool, playSessionId: String, transcoding: Bool) async {
        guard !token.isEmpty else { return }
        _ = try? await json("POST", e.rawValue, body: Self.playBody(id: id, position: position, paused: paused, playSessionId: playSessionId, transcoding: transcoding))
    }
    func lyrics(_ id: String) async -> [(TimeInterval, String)] {
        guard product == "Jellyfin", let o = try? await json("GET", "/Audio/\(id)/Lyrics") as? [String: Any] else { return [] }
        return Self.parseLyrics(o)
    }
}

extension EmbyClient: LibraryBackend {
    private static func sort(_ type: String) -> [String: String] {
        switch type {
        case "newest": ["SortBy": "DateCreated,SortName", "SortOrder": "Descending"]
        case "recent": ["SortBy": "DatePlayed", "SortOrder": "Descending", "Filters": "IsPlayed"]
        case "frequent": ["SortBy": "PlayCount,SortName", "SortOrder": "Descending", "Filters": "IsPlayed"]
        case "random": ["SortBy": "Random"]
        case "starred": ["SortBy": "SortName", "Filters": "IsFavorite"]
        default: ["SortBy": "SortName", "SortOrder": "Ascending"]
        }
    }
    func albums(offset: Int, size: Int) async throws -> [NDAlbum] { try await albumList(type: "alphabeticalByName", offset: offset, size: size) }
    func albumList(type: String, offset: Int, size: Int) async throws -> [NDAlbum] {
        var q = Self.sort(type).merging(Self.paging(offset, size)) { $1 }
        q["IncludeItemTypes"] = "MusicAlbum"; q["Recursive"] = "true"
        return try await items(q).list.compactMap(Self.parseAlbum)
    }
    func album(id: String) async throws -> (NDAlbum, [NDSong]) {
        async let head = json("GET", "/Users/\(userId)/Items/\(id)")
        let tr = try await items(["ParentId": id, "IncludeItemTypes": "Audio", "Recursive": "true", "SortBy": "ParentIndexNumber,IndexNumber,SortName"])
        let songs = tr.list.compactMap(Self.parseSong)
        let a = ((try? await head) as? [String: Any]).flatMap(Self.parseAlbum)
            ?? NDAlbum(id: id, name: songs.first?.album ?? "", artist: songs.first?.artist ?? "", coverId: songs.first?.coverId, year: nil, songCount: songs.count)
        return (a, songs)
    }
    func search(_ q: String, count: Int) async throws -> (albums: [NDAlbum], artists: [NDArtist], songs: [NDSong]) {
        func s(_ t: String) -> [String: String] { ["SearchTerm": q, "IncludeItemTypes": t, "Recursive": "true", "Limit": "\(count)"] }
        async let al = items(s("MusicAlbum")); async let ar = items(s("MusicArtist")); async let so = items(s("Audio"))
        return (try await al.list.compactMap(Self.parseAlbum), (try? await ar)?.list.compactMap(Self.parseArtist) ?? [], try await so.list.compactMap(Self.parseSong))
    }
    func artists() async throws -> [NDArtist] {
        let o = try await json("GET", "/Artists/AlbumArtists", query: ["UserId": userId, "Recursive": "true", "SortBy": "SortName",
                                                                    "Fields": "ChildCount,AlbumCount", "EnableImageTypes": "Primary", "ImageTypeLimit": "1"]) as? [String: Any]
        return ((o?["Items"] as? [[String: Any]]) ?? []).compactMap(Self.parseArtist)
    }
    func songs(offset: Int, count: Int) async throws -> [NDSong] {
        try await items(["IncludeItemTypes": "Audio", "Recursive": "true", "SortBy": "SortName"].merging(Self.paging(offset, count)) { $1 }).list.compactMap(Self.parseSong)
    }
    func artistAlbums(_ id: String) async throws -> [NDAlbum] {
        let l = try await items(["IncludeItemTypes": "MusicAlbum", "Recursive": "true", "AlbumArtistIds": id, "SortBy": "ProductionYear,SortName", "SortOrder": "Descending"]).list
        if !l.isEmpty { return l.compactMap(Self.parseAlbum) }
        return try await items(["IncludeItemTypes": "MusicAlbum", "Recursive": "true", "ArtistIds": id, "SortBy": "ProductionYear,SortName", "SortOrder": "Descending"]).list.compactMap(Self.parseAlbum)
    }
    func artistTracks(_ id: String, size: Int) async throws -> [NDSong] {
        try await items(["IncludeItemTypes": "Audio", "Recursive": "true", "ArtistIds": id, "SortBy": "PlayCount,SortName", "SortOrder": "Descending", "Limit": "\(size)"]).list.compactMap(Self.parseSong)
    }
    func genres() async throws -> [NDGenre] {
        let o = try await json("GET", "/MusicGenres", query: ["UserId": userId, "Recursive": "true", "SortBy": "SortName", "Fields": "SongCount,AlbumCount,ChildCount"]) as? [String: Any]
        return ((o?["Items"] as? [[String: Any]]) ?? []).compactMap { g in
            (g["Name"] as? String).map { NDGenre(name: $0, songCount: Self.num(g["SongCount"]) ?? Self.num(g["ChildCount"]) ?? 0, albumCount: Self.num(g["AlbumCount"]) ?? 0) }
        }
    }
    func albumsByGenre(_ genre: String, offset: Int, size: Int) async throws -> [NDAlbum] {
        try await items(["IncludeItemTypes": "MusicAlbum", "Recursive": "true", "Genres": genre, "SortBy": "SortName"].merging(Self.paging(offset, size)) { $1 }).list.compactMap(Self.parseAlbum)
    }
    func songsByGenre(_ genre: String, offset: Int, count: Int) async throws -> [NDSong] {
        try await items(["IncludeItemTypes": "Audio", "Recursive": "true", "Genres": genre, "SortBy": "Random"].merging(Self.paging(offset, count)) { $1 }).list.compactMap(Self.parseSong)
    }
    func songTotal() async -> Int? { try? await items(["IncludeItemTypes": "Audio", "Recursive": "true", "Limit": "0"]).total }
    func favorites() async throws -> (albums: [NDAlbum], songs: [NDSong], artists: [NDArtist]) {
        func f(_ t: String) -> [String: String] { ["IncludeItemTypes": t, "Recursive": "true", "Filters": "IsFavorite", "SortBy": "SortName"] }
        async let al = items(f("MusicAlbum")); async let so = items(f("Audio")); async let ar = items(f("MusicArtist"))
        return (try await al.list.compactMap(Self.parseAlbum), try await so.list.compactMap(Self.parseSong), (try? await ar)?.list.compactMap(Self.parseArtist) ?? [])
    }
    func setFavorite(_ on: Bool, id: String, kind: NavidromeClient.StarKind) async throws {
        _ = try await json(on ? "POST" : "DELETE", "/Users/\(userId)/FavoriteItems/\(id)")
    }
    func playlists() async throws -> [NDPlaylist] {
        try await items(["IncludeItemTypes": "Playlist", "Recursive": "true", "SortBy": "SortName", "Fields": "ChildCount,DateCreated"]).list.compactMap(Self.parsePlaylist)
    }
    /// Playlist entries carry `PlaylistItemId` (needed for removal), so we keep them per playlist.
    func playlistTracks(_ id: String) async throws -> [NDSong] {
        let o = try await json("GET", "/Playlists/\(id)/Items", query: ["UserId": userId, "Fields": "MediaSources,UserData", "EnableImageTypes": "Primary"]) as? [String: Any]
        let l = (o?["Items"] as? [[String: Any]]) ?? []
        entryIds[id] = l.map { ($0["PlaylistItemId"] as? String) ?? ($0["Id"] as? String ?? "") }
        return l.compactMap(Self.parseSong)
    }
    func createPlaylist(name: String, songIds: [String]) async throws -> NDPlaylist? {
        let o = try await json("POST", "/Playlists", query: ["Name": name, "Ids": songIds.joined(separator: ","), "UserId": userId, "MediaType": "Audio"]) as? [String: Any]
        return (o?["Id"] as? String).map { NDPlaylist(id: $0, name: name, songCount: songIds.count, coverId: nil) }
    }
    func addToPlaylist(_ id: String, songIds: [String]) async throws {
        _ = try await json("POST", "/Playlists/\(id)/Items", query: ["Ids": songIds.joined(separator: ","), "UserId": userId])
    }
    func removeFromPlaylist(_ id: String, indexes: [Int]) async throws {
        if entryIds[id] == nil { _ = try await playlistTracks(id) }
        let e = entryIds[id] ?? []
        let ids = indexes.filter { $0 >= 0 && $0 < e.count }.map { e[$0] }
        guard !ids.isEmpty else { return }
        _ = try await json("DELETE", "/Playlists/\(id)/Items", query: ["EntryIds": ids.joined(separator: ",")])
        entryIds[id] = nil
    }
    func setPlaylistSongs(_ id: String, songIds: [String]) async throws {
        _ = try await playlistTracks(id)
        let n = entryIds[id]?.count ?? 0
        if n > 0 { try await removeFromPlaylist(id, indexes: Array(0..<n)) }
        if !songIds.isEmpty { try await addToPlaylist(id, songIds: songIds) }
    }
    func deletePlaylist(_ id: String) async throws {
        if product == "Jellyfin" { _ = try await json("DELETE", "/Items/\(id)") }
        else { _ = try await json("POST", "/Items/Delete", query: ["Ids": id]) }
    }
}
