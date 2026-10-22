import Foundation

enum NavidromeError: LocalizedError {
    case badURL, http(Int), api(String), decode
    var errorDescription: String? {
        switch self {
        case .badURL: return "无效地址"
        case .http(let c): return "HTTP \(c)"
        case .api(let m): return m
        case .decode: return "解析失败"
        }
    }
}

actor NavidromeClient {
    static let shared = NavidromeClient()
    private let session: URLSession = {
        let c = URLSessionConfiguration.ephemeral
        c.httpAdditionalHeaders = [
            "User-Agent": "Yimeng/0.3 (iOS; art.yimeng.app)",
            "Accept": "application/json",
        ]
        c.timeoutIntervalForRequest = 45
        return URLSession(configuration: c)
    }()

    private(set) var baseURL: URL?
    private var user = ""
    private var token = ""
    private var salt = ""

    struct AuthSnapshot: Sendable {
        let base: URL?
        let user: String
        let token: String
        let salt: String
    }

    func authSnapshot() -> AuthSnapshot {
        AuthSnapshot(base: baseURL, user: user, token: token, salt: salt)
    }

    func configure(host: String) throws {
        var h = host.trimmingCharacters(in: .whitespacesAndNewlines)
        if h.hasSuffix("/") { h.removeLast() }
        h = h.replacingOccurrences(of: ":443", with: "")
        guard let u = URL(string: h), u.scheme != nil else { throw NavidromeError.badURL }
        baseURL = u
    }

    func login(username: String, password: String) async throws {
        guard let base = baseURL else { throw NavidromeError.badURL }
        var req = URLRequest(url: base.appendingPathComponent("auth/login"))
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try JSONSerialization.data(withJSONObject: ["username": username, "password": password])
        let (data, resp) = try await session.data(for: req)
        let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
        guard code == 200 else { throw NavidromeError.http(code) }
        guard let json = try JSONSerialization.jsonObject(with: data) as? [String: Any],
              let s = json["subsonicSalt"] as? String,
              let t = json["subsonicToken"] as? String else {
            throw NavidromeError.api("登录响应缺少 token")
        }
        user = username
        salt = s
        token = t
        Keychain.set(password, service: "yimeng.navidrome", account: username)
    }

    func newestAlbums(size: Int = 24) async throws -> [NDAlbum] {
        let obj = try await rest("getAlbumList2.view", query: ["type": "newest", "size": "\(size)"])
        let list = ((obj["albumList2"] as? [String: Any])?["album"]) as? [[String: Any]] ?? []
        return list.compactMap(Self.parseAlbum)
    }

    func album(id: String) async throws -> (NDAlbum, [NDSong]) {
        let obj = try await rest("getAlbum.view", query: ["id": id])
        guard let a = obj["album"] as? [String: Any], let album = Self.parseAlbum(a) else {
            throw NavidromeError.decode
        }
        let raw = a["song"]
        let songsArr: [[String: Any]]
        if let many = raw as? [[String: Any]] { songsArr = many }
        else if let one = raw as? [String: Any] { songsArr = [one] }
        else { songsArr = [] }
        let songs = songsArr.compactMap(Self.parseSong)
        return (album, songs)
    }

    func search(query: String, count: Int = 20) async throws -> (albums: [NDAlbum], artists: [NDArtist], songs: [NDSong]) {
        let obj = try await rest("search3.view", query: [
            "query": query.isEmpty ? "a" : query,
            "albumCount": "\(count)",
            "artistCount": "\(count)",
            "songCount": "\(count)",
        ])
        let r = obj["searchResult3"] as? [String: Any] ?? [:]
        let albums = ((r["album"] as? [[String: Any]]) ?? []).compactMap(Self.parseAlbum)
        let artists = ((r["artist"] as? [[String: Any]]) ?? []).compactMap { a -> NDArtist? in
            guard let id = a["id"] as? String, let name = a["name"] as? String else { return nil }
            return NDArtist(id: id, name: name, albumCount: a["albumCount"] as? Int)
        }
        let songs = ((r["song"] as? [[String: Any]]) ?? []).compactMap(Self.parseSong)
        return (albums, artists, songs)
    }

    func artists() async throws -> [NDArtist] {
        let obj = try await rest("getArtists.view", query: [:])
        let index = ((obj["artists"] as? [String: Any])?["index"]) as? [[String: Any]] ?? []
        var out: [NDArtist] = []
        for bucket in index {
            for a in (bucket["artist"] as? [[String: Any]] ?? []) {
                guard let id = a["id"] as? String, let name = a["name"] as? String else { continue }
                out.append(NDArtist(id: id, name: name, albumCount: a["albumCount"] as? Int, coverId: a["coverArt"] as? String))
            }
        }
        return out
    }

    func songExists(id: String) async throws -> Bool {
        do { let o = try await rest("getSong.view", query: ["id": id]); return o["song"] != nil }
        catch NavidromeError.api { return false }
    }

    /// All songs via search3 with empty query (Navidrome lists everything), paged.
    func songs(offset: Int, count: Int = 50) async throws -> [NDSong] {
        let obj = try await rest("search3.view", query: [
            "query": "", "songCount": "\(count)", "songOffset": "\(offset)",
            "albumCount": "0", "artistCount": "0",
        ])
        let r = obj["searchResult3"] as? [String: Any] ?? [:]
        return ((r["song"] as? [[String: Any]]) ?? []).compactMap(Self.parseSong)
    }

    func albumList(type: String, offset: Int, size: Int = 40) async throws -> [NDAlbum] {
        let obj = try await rest("getAlbumList2.view", query: ["type": type, "size": "\(size)", "offset": "\(offset)"])
        let list = ((obj["albumList2"] as? [String: Any])?["album"]) as? [[String: Any]] ?? []
        return list.compactMap(Self.parseAlbum)
    }

    func artistAlbums(id: String) async throws -> [NDAlbum] {
        let obj = try await rest("getArtist.view", query: ["id": id])
        let a = (obj["artist"] as? [String: Any])?["album"]
        let arr = (a as? [[String: Any]]) ?? ((a as? [String: Any]).map { [$0] } ?? [])
        return arr.compactMap(Self.parseAlbum)
    }

    func starredAlbums() async throws -> [NDAlbum] {
        let obj = try await rest("getStarred2.view", query: [:])
        let r = obj["starred2"] as? [String: Any] ?? [:]
        return ((r["album"] as? [[String: Any]]) ?? []).compactMap(Self.parseAlbum)
    }

    func genres() async throws -> [NDGenre] {
        let obj = try await rest("getGenres.view", query: [:])
        let g = ((obj["genres"] as? [String: Any])?["genre"]) as? [[String: Any]] ?? []
        return g.compactMap { x in
            guard let n = x["value"] as? String, !n.isEmpty else { return nil }
            return NDGenre(name: n, songCount: x["songCount"] as? Int ?? 0, albumCount: x["albumCount"] as? Int ?? 0)
        }.sorted { $0.songCount > $1.songCount }
    }

    func songsByGenre(_ genre: String, count: Int = 50) async throws -> [NDSong] {
        let obj = try await rest("getSongsByGenre.view", query: ["genre": genre, "count": "\(count)"])
        let s = ((obj["songsByGenre"] as? [String: Any])?["song"]) as? [[String: Any]] ?? []
        return s.compactMap(Self.parseSong)
    }

    /// Total songs indexed (getScanStatus.count), if the server reports it.
    func scanCount() async throws -> Int? {
        let obj = try await rest("getScanStatus.view", query: [:])
        return (obj["scanStatus"] as? [String: Any])?["count"] as? Int
    }

    /// First `pages` × 200 albums (A–Z) — the library is far too large to load whole.
    func albumPages(_ pages: Int, type: String = "alphabeticalByName") async throws -> [NDAlbum] {
        var out: [NDAlbum] = []
        for _ in 0..<pages {
            let page = try await albumList(type: type, offset: out.count, size: 200)
            out += page
            if page.count < 200 { break }
        }
        return out
    }

    struct Starred { var albums: [NDAlbum]; var songs: [NDSong]; var artists: [NDArtist] }
    func starred() async throws -> Starred {
        let obj = try await rest("getStarred2.view", query: [:])
        let r = obj["starred2"] as? [String: Any] ?? [:]
        return Starred(albums: ((r["album"] as? [[String: Any]]) ?? []).compactMap(Self.parseAlbum),
                       songs: ((r["song"] as? [[String: Any]]) ?? []).compactMap(Self.parseSong),
                       artists: ((r["artist"] as? [[String: Any]]) ?? []).compactMap(Self.parseArtist))
    }

    enum StarKind: String { case song = "id", album = "albumId", artist = "artistId" }
    func setStar(_ on: Bool, id: String, kind: StarKind) async throws {
        _ = try await rest(on ? "star.view" : "unstar.view", query: [kind.rawValue: id])
    }

    struct ArtistDetail { var artist: NDArtist; var albums: [NDAlbum]; var bio: String; var image: String?; var similar: [NDArtist]; var top: [NDSong] }
    func artistDetail(_ a: NDArtist) async throws -> ArtistDetail {
        async let det = rest("getArtist.view", query: ["id": a.id])
        async let inf = rest("getArtistInfo2.view", query: ["id": a.id, "count": "12"])
        async let top = rest("getTopSongs.view", query: ["artist": a.name, "count": "10"])
        let d = (try await det)["artist"] as? [String: Any] ?? [:]
        let al = d["album"]
        let albums = ((al as? [[String: Any]]) ?? ((al as? [String: Any]).map { [$0] } ?? [])).compactMap(Self.parseAlbum)
        let info = ((try? await inf)?["artistInfo2"] as? [String: Any]) ?? [:]
        var bio = (info["biography"] as? String) ?? ""
        if let r = bio.range(of: "<a ") { bio = String(bio[..<r.lowerBound]) }
        let sim = ((info["similarArtist"] as? [[String: Any]]) ?? []).compactMap(Self.parseArtist)
        let ts = ((((try? await top)?["topSongs"] as? [String: Any])?["song"]) as? [[String: Any]] ?? []).compactMap(Self.parseSong)
        let artist = Self.parseArtist(d) ?? a
        return ArtistDetail(artist: artist, albums: albums, bio: bio.trimmingCharacters(in: .whitespacesAndNewlines),
                            image: (info["largeImageUrl"] as? String) ?? (d["artistImageUrl"] as? String), similar: sim, top: ts)
    }

    static func parseArtist(_ a: [String: Any]) -> NDArtist? {
        guard let id = a["id"] as? String, let name = a["name"] as? String else { return nil }
        return NDArtist(id: id, name: name, albumCount: a["albumCount"] as? Int, coverId: a["coverArt"] as? String)
    }

    func playlists() async throws -> [NDPlaylist] {
        let obj = try await rest("getPlaylists.view", query: [:])
        let list = ((obj["playlists"] as? [String: Any])?["playlist"]) as? [[String: Any]] ?? []
        return list.compactMap(Self.parsePlaylist)
    }

    @discardableResult
    func createPlaylist(name: String, songIds: [String]) async throws -> NDPlaylist? {
        let obj = try await rest("createPlaylist.view", query: ["name": name], multi: songIds.map { ("songId", $0) })
        return (obj["playlist"] as? [String: Any]).flatMap(Self.parsePlaylist)
    }

    /// Replace a playlist's song list in the given order (createPlaylist with playlistId overwrites).
    func setPlaylistSongs(_ playlistId: String, songIds: [String]) async throws {
        _ = try await rest("createPlaylist.view", query: ["playlistId": playlistId], multi: songIds.map { ("songId", $0) })
    }

    func updatePlaylist(_ id: String, name: String? = nil, comment: String? = nil, isPublic: Bool? = nil, removeIndexes: [Int] = []) async throws {
        var q = ["playlistId": id]
        if let name { q["name"] = name }
        if let comment { q["comment"] = comment }
        if let isPublic { q["public"] = isPublic ? "true" : "false" }
        _ = try await rest("updatePlaylist.view", query: q, multi: removeIndexes.map { ("songIndexToRemove", String($0)) })
    }

    func deletePlaylist(_ id: String) async throws {
        _ = try await rest("deletePlaylist.view", query: ["id": id])
    }

    func playlistDetail(id: String) async throws -> (NDPlaylist, [NDSong]) {
        let obj = try await rest("getPlaylist.view", query: ["id": id])
        let p = obj["playlist"] as? [String: Any] ?? [:]
        let e = p["entry"]
        let arr = (e as? [[String: Any]]) ?? ((e as? [String: Any]).map { [$0] } ?? [])
        guard let pl = Self.parsePlaylist(p) else { throw NavidromeError.decode }
        return (pl, arr.compactMap(Self.parseSong))
    }

    static func parsePlaylist(_ a: [String: Any]) -> NDPlaylist? {
        guard let id = a["id"] as? String, let name = a["name"] as? String else { return nil }
        return NDPlaylist(id: id, name: name, songCount: a["songCount"] as? Int, coverId: a["coverArt"] as? String,
                          comment: a["comment"] as? String, isPublic: a["public"] as? Bool,
                          duration: a["duration"] as? Int, owner: a["owner"] as? String)
    }

    func addToPlaylist(_ playlistId: String, songIds: [String]) async throws {
        for s in songIds { _ = try await rest("updatePlaylist.view", query: ["playlistId": playlistId, "songIdToAdd": s]) }
    }

    func playlist(id: String) async throws -> [NDSong] {
        let obj = try await rest("getPlaylist.view", query: ["id": id])
        let e = (obj["playlist"] as? [String: Any])?["entry"]
        let arr = (e as? [[String: Any]]) ?? ((e as? [String: Any]).map { [$0] } ?? [])
        return arr.compactMap(Self.parseSong)
    }

    func starredSongs() async throws -> [NDSong] {
        let obj = try await rest("getStarred2.view", query: [:])
        let r = obj["starred2"] as? [String: Any] ?? [:]
        return ((r["song"] as? [[String: Any]]) ?? []).compactMap(Self.parseSong)
    }

    /// Build authenticated Subsonic URL for stream / cover (same query auth as REST).
    nonisolated static func authedURL(base: URL, method: String, user: String, token: String, salt: String, query: [String: String]) -> URL? {
        var c = URLComponents(url: base.appendingPathComponent("rest/\(method)"), resolvingAgainstBaseURL: false)!
        var items = [
            URLQueryItem(name: "u", value: user),
            URLQueryItem(name: "t", value: token),
            URLQueryItem(name: "s", value: salt),
            URLQueryItem(name: "v", value: "1.16.1"),
            URLQueryItem(name: "c", value: "yimeng"),
        ]
        for (k, v) in query { items.append(URLQueryItem(name: k, value: v)) }
        c.queryItems = items
        return c.url
    }

    private static func parseAlbum(_ a: [String: Any]) -> NDAlbum? {
        guard let id = a["id"] as? String, let name = a["name"] as? String else { return nil }
        return NDAlbum(id: id, name: name,
                       artist: (a["artist"] as? String) ?? "",
                       artistId: a["artistId"] as? String,
                       coverId: a["coverArt"] as? String,
                       year: a["year"] as? Int,
                       songCount: a["songCount"] as? Int)
    }

    private static func parseSong(_ a: [String: Any]) -> NDSong? {
        guard let id = a["id"] as? String, let title = a["title"] as? String else { return nil }
        let dur: Int?
        if let i = a["duration"] as? Int { dur = i }
        else if let d = a["duration"] as? Double { dur = Int(d) }
        else { dur = nil }
        func num(_ k: String) -> Int? { (a[k] as? Int) ?? (a[k] as? Double).map { Int($0) } }
        func dbl(_ d: [String: Any]?, _ k: String) -> Double? { (d?[k] as? Double) ?? (d?[k] as? Int).map(Double.init) }
        let rg = a["replayGain"] as? [String: Any]
        return NDSong(id: id,
                      title: title,
                      artist: (a["artist"] as? String) ?? "",
                      album: a["album"] as? String,
                      albumId: a["albumId"] as? String,
                      coverId: a["coverArt"] as? String,
                      duration: dur,
                      track: a["track"] as? Int,
                      suffix: a["suffix"] as? String,
                      contentType: a["contentType"] as? String,
                      bitRate: num("bitRate"),
                      samplingRate: num("samplingRate"),
                      bitDepth: num("bitDepth"),
                      channelCount: num("channelCount"),
                      replayGain: rg.map { ReplayGainInfo(trackGain: dbl($0, "trackGain"), albumGain: dbl($0, "albumGain"),
                                                          trackPeak: dbl($0, "trackPeak"), albumPeak: dbl($0, "albumPeak")) })
    }

    private func rest(_ method: String, query: [String: String], multi: [(String, String)] = []) async throws -> [String: Any] {
        // Views may ask before bootstrap login finishes — wait briefly instead of failing to an empty list.
        var waited = 0
        while token.isEmpty && waited < 60 { try? await Task.sleep(for: .milliseconds(250)); waited += 1 }
        guard let base = baseURL, !user.isEmpty, !token.isEmpty else { throw NavidromeError.api("未登录") }
        guard let built = Self.authedURL(base: base, method: method, user: user, token: token, salt: salt,
                                       query: query.merging(["f": "json"]) { _, n in n }) else {
            throw NavidromeError.badURL
        }
        var url = built
        if !multi.isEmpty, var c = URLComponents(url: url, resolvingAgainstBaseURL: false) {
            c.queryItems = (c.queryItems ?? []) + multi.map { URLQueryItem(name: $0.0, value: $0.1) }
            if let u = c.url { url = u }
        }
        let t0 = Date()
        let (data, resp) = try await session.data(from: url)
        NetLog.record(method, ms: Int(Date().timeIntervalSince(t0) * 1000), bytes: data.count)
        let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
        guard code == 200 else { throw NavidromeError.http(code) }
        guard let root = try JSONSerialization.jsonObject(with: data) as? [String: Any],
              let sub = root["subsonic-response"] as? [String: Any] else { throw NavidromeError.decode }
        if let status = sub["status"] as? String, status != "ok" {
            let msg = (sub["error"] as? [String: Any])?["message"] as? String ?? "API error"
            throw NavidromeError.api(msg)
        }
        return sub
    }
}
