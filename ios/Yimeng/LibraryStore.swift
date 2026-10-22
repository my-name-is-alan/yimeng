import Foundation
import SwiftUI

struct NDAlbum: Identifiable, Hashable, Codable {
    let id: String
    let name: String
    let artist: String
    var artistId: String? = nil
    let coverId: String?
    let year: Int?
    let songCount: Int?
}

struct NDArtist: Identifiable, Hashable, Codable {
    let id: String
    let name: String
    let albumCount: Int?
    var coverId: String? = nil
}

struct NDGenre: Identifiable, Hashable, Codable {
    var id: String { name }
    let name: String
    let songCount: Int
    let albumCount: Int
}

struct NDPlaylist: Identifiable, Hashable, Codable {
    let id: String
    var name: String
    var songCount: Int?
    let coverId: String?
    var comment: String? = nil
    var isPublic: Bool? = nil
    var duration: Int? = nil
    var owner: String? = nil
}

struct NDSong: Identifiable, Hashable, Codable {
    let id: String
    let title: String
    let artist: String
    let album: String?
    let albumId: String?
    let coverId: String?
    let duration: Int?
    let track: Int?
    // OpenSubsonic audio details (optional; older cached payloads decode without them).
    var suffix: String? = nil
    var contentType: String? = nil
    var bitRate: Int? = nil
    var samplingRate: Int? = nil
    var bitDepth: Int? = nil
    var channelCount: Int? = nil
    var replayGain: ReplayGainInfo? = nil

    var durationLabel: String {
        guard let d = duration else { return "" }
        return String(format: "%d:%02d", d / 60, d % 60)
    }
}

/// Live library from connected sources (Navidrome first).
@MainActor
final class LibraryStore: ObservableObject {
    static let shared = LibraryStore()
    @Published var albums: [NDAlbum] = []
    @Published var artists: [NDArtist] = []
    @Published var status: String = ""
    @Published var connected = false
    @Published var loading = false
    /// Every album on the server (paged 500 at a time), loaded after connect.
    @Published var allAlbums: [NDAlbum] = []
    @Published var albumTotal: Int?
    @Published var songTotal: Int?
    /// Pinyin-sorted artists + A–Z sections for the elevator bar. Built off main, cached by list signature.
    @Published private(set) var artistIndex: ArtistIndex?
    private var artistIndexSig: Int?

    func buildArtistIndex(_ list: [NDArtist]) {
        guard !list.isEmpty else { return }
        var h = Hasher(); h.combine(list.count); for a in list { h.combine(a.id); h.combine(a.name) }
        let sig = h.finalize()
        guard sig != artistIndexSig else { return }
        artistIndexSig = sig
        Task {
            let t0 = Date()
            let idx = await Task.detached(priority: .userInitiated) { ArtistIndex.build(list) }.value
            guard sig == artistIndexSig else { return }
            artistIndex = idx
            NetLog.logger.info("INDEX artists \(list.count) \(Int(Date().timeIntervalSince(t0) * 1000))ms")
        }
    }
    @Published var user = ""
    @Published var host = ""

    #if DEBUG
    static let defaultHost = "https://music.mefun.org"
    static let defaultUser = "lvzy"
    #else
    static let defaultHost = ""
    static let defaultUser = ""
    #endif

    // Cached auth for cover / stream URLs (never logged)
    private var coverBase: URL?
    private var coverUser = ""
    private var coverToken = ""
    private var coverSalt = ""

    /// When true, UI must show only API rows (no DreamData / Mock mix).
    var useLiveOnly: Bool {
        #if DEBUG
        connected || liveMode
        #else
        true
        #endif
    }
    /// Release gate: no source with credentials → show the connect screen instead of tabs.
    var hasSource: Bool { connected || liveMode }
    /// True as soon as Navidrome credentials exist — avoids flashing demo content while login is in flight.
    @Published var liveMode = false

    /// Source switched away: drop session + in-memory library.
    func reset() {
        connected = false; liveMode = false; loading = false; fnosRoot = nil; fnosToken = ""; embyBase = nil; embyToken = ""; embyUserId = ""; embyUsers = []; embyUserName = ""; embyAvatar = nil
        Task { await FnosClient.shared.logout() }
        albums = []; artists = []; allAlbums = []; albumTotal = nil; songTotal = nil
        status = ""; user = ""; host = ""
        coverBase = nil; coverToken = ""; coverSalt = ""; coverUser = ""
        artistIndex = nil; artistIndexSig = nil
    }

    func bootstrap() async {
        var cfg = SourceStore.shared.config(for: .navidrome)
        if cfg.host.isEmpty || cfg.host.contains(":4533") || cfg.host.contains(":443") {
            cfg.host = Self.defaultHost
        }
        if cfg.username.isEmpty { cfg.username = Self.defaultUser }
        if !cfg.username.isEmpty, cfg.secret.isEmpty, let pw = Keychain.get(service: SourceKind.navidrome.keychainService, account: cfg.username), !pw.isEmpty {
            cfg.secret = pw
        }
        // Seed Keychain only if missing (env / Application Support) — never log the value
        if cfg.secret.isEmpty, !Self.defaultUser.isEmpty, let seeded = Self.readSeedPassword() {
            cfg.secret = seeded
            Keychain.set(seeded, service: SourceKind.navidrome.keychainService, account: Self.defaultUser)
        }
        SourceStore.shared.upsert(cfg)
        liveMode = !cfg.secret.isEmpty
        guard !cfg.secret.isEmpty else {
            status = "待连接 Navidrome"
            return
        }
        await connect(host: cfg.host, username: cfg.username.isEmpty ? Self.defaultUser : cfg.username, password: cfg.secret)
    }

    private static func readSeedPassword() -> String? {
        #if !DEBUG
        return nil
        #endif
        let urls = [
            FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first?
                .appendingPathComponent("yimeng/nd.seed"),
            URL(fileURLWithPath: NSHomeDirectory()).appendingPathComponent(".yimeng/nd.seed"),
        ].compactMap { $0 }
        for u in urls {
            if let s = try? String(contentsOf: u, encoding: .utf8) {
                let t = s.trimmingCharacters(in: .whitespacesAndNewlines)
                if !t.isEmpty { return t }
            }
        }
        if let e = ProcessInfo.processInfo.environment["YIMENG_ND_PASSWORD"], !e.isEmpty { return e }
        return nil
    }

    /// 飞牛音乐: separate 「音乐」 app account; token → Cookie. Library calls route via NavidromeClient.fnos.
    @Published var fnosRoot: URL?
    @Published var fnosToken = ""
    func bootstrapFnos() async {
        let cfg = SourceStore.shared.config(for: .fnos)
        guard !cfg.host.isEmpty, !cfg.username.isEmpty,
              let pw = Keychain.get(service: SourceKind.fnos.keychainService, account: cfg.username), !pw.isEmpty else {
            #if DEBUG
            // Sim/dev only: YIMENG_FNOS_URL / _USER / _PASSWORD from the launch env (never logged); login stores it in Keychain.
            let env = ProcessInfo.processInfo.environment
            if let u = env["YIMENG_FNOS_URL"], let n = env["YIMENG_FNOS_USER"], let p = env["YIMENG_FNOS_PASSWORD"], !u.isEmpty, !n.isEmpty {
                liveMode = true; await connectFnos(host: u, username: n, password: p); return
            }
            #endif
            status = "待连接飞牛音乐"; return
        }
        liveMode = true
        await connectFnos(host: cfg.host, username: cfg.username, password: pw)
    }
    func connectFnos(host: String, username: String, password: String) async {
        loading = true; status = "连接中…"
        defer { loading = false }
        do {
            let f = FnosClient.shared
            try await f.configure(host: host)
            try await f.login(username: username, password: password)
            let snap = await f.snapshot()
            fnosRoot = snap.root; fnosToken = snap.token
            albums = try await f.albums(offset: 0, size: 30)
            artists = Array(try await f.artists().prefix(40))
            connected = true; liveMode = true
            user = username; self.host = snap.root?.absoluteString ?? host
            songTotal = await f.songTotal()
            status = (["飞牛音乐 · \(username)", songTotal.map { "\($0) 首歌曲" } ?? ""].filter { !$0.isEmpty }).joined(separator: " · ")
            var cfg = SourceStore.shared.config(for: .fnos)
            cfg.host = self.host; cfg.username = username; cfg.connected = true; cfg.statusNote = status
            SourceStore.shared.upsert(cfg)
        } catch {
            connected = false
            status = error.localizedDescription
        }
    }

    // MARK: Emby / Jellyfin
    enum EmbyMode: String { case password, apiKey }
    @Published var embyBase: URL?
    @Published var embyToken = ""
    @Published var embyUserId = ""
    @Published var embyProduct = "Emby"
    @Published var embyUserName = ""
    @Published var embyAvatar: URL?
    /// API key without a chosen user → list to pick from (form shows a picker).
    @Published var embyUsers: [EmbyClient.User] = []
    private var playSessions: [String: String] = [:]
    static var embyMode: EmbyMode {
        get { EmbyMode(rawValue: UserDefaults.standard.string(forKey: "ym.emby.mode") ?? "") ?? .password }
        set { UserDefaults.standard.set(newValue.rawValue, forKey: "ym.emby.mode") }
    }
    func playSession(for songId: String) -> String {
        if let s = playSessions[songId] { return s }
        let s = UUID().uuidString.replacingOccurrences(of: "-", with: ""); playSessions[songId] = s; return s
    }
    func bootstrapEmby() async {
        let cfg = SourceStore.shared.config(for: .emby)
        let svc = SourceKind.emby.keychainService
        let uid = UserDefaults.standard.string(forKey: "ym.emby.userId") ?? ""
        guard !cfg.host.isEmpty, let tok = Keychain.get(service: svc, account: "token"), !tok.isEmpty, !uid.isEmpty else {
            #if DEBUG
            // Sim/dev only: YIMENG_EMBY_URL / _USER / _PASSWORD from the launch env (never logged); token then lands in Keychain.
            let env = ProcessInfo.processInfo.environment
            if let u = env["YIMENG_EMBY_URL"], let n = env["YIMENG_EMBY_USER"], let p = env["YIMENG_EMBY_PASSWORD"], !u.isEmpty, !n.isEmpty {
                liveMode = true
                await connectEmby(host: u, mode: .password, username: n, secret: p); return
            }
            #endif
            status = "待连接 Emby"; return
        }
        liveMode = true
        await connectEmby(host: cfg.host, mode: Self.embyMode, username: cfg.username, secret: nil, savedToken: tok, userId: uid)
    }
    /// secret = password (may be empty) or API key. savedToken → reuse a stored session instead of logging in.
    func connectEmby(host: String, mode: EmbyMode, username: String, secret: String?, savedToken: String? = nil, userId pick: String? = nil) async {
        loading = true; status = "连接中…"
        defer { loading = false }
        let e = EmbyClient.shared, svc = SourceKind.emby.keychainService
        do {
            try await e.configure(host: host)
            let info: EmbyClient.ServerInfo
            do { info = try await e.serverInfo() } catch { throw NavidromeError.api("连不上服务器：\(error.localizedDescription)") }
            embyProduct = info.product
            if let savedToken, let pick {
                await e.restore(token: savedToken, userId: pick)
            } else if mode == .password {
                try await e.login(username: username, password: secret ?? "")
            } else {
                let users = try await e.useAPIKey(secret ?? "")
                let chosen = pick.flatMap { id in users.first { $0.id == id } }
                    ?? users.first { !username.isEmpty && $0.name.caseInsensitiveCompare(username) == .orderedSame }
                    ?? (users.count == 1 ? users.first : nil)
                guard let u = chosen else {
                    embyUsers = users
                    status = users.isEmpty ? "这个 API Key 看不到任何用户" : "请选择要使用的 \(info.product) 用户"
                    return
                }
                await e.select(userId: u.id)
            }
            try await e.validate()
            embyUsers = []
            let snap = await e.snapshot()
            embyBase = snap.base; embyToken = snap.token; embyUserId = snap.userId
            Keychain.set(snap.token, service: svc, account: "token")
            if mode == .apiKey, let k = secret, !k.isEmpty { Keychain.set(k, service: svc, account: "apikey") }
            UserDefaults.standard.set(snap.userId, forKey: "ym.emby.userId")
            Self.embyMode = mode
            if let me = await e.currentUser() {
                embyUserName = me.name
                embyAvatar = me.imageTag.flatMap { t in snap.base.flatMap { EmbyClient.avatarURL(base: $0, userId: snap.userId, tag: t) } }
            }
            albums = try await e.albumList(type: "newest", offset: 0, size: 30)
            artists = Array(try await e.artists().prefix(40))
            connected = true; liveMode = true
            user = username; self.host = snap.base?.absoluteString ?? host
            songTotal = await e.songTotal()
            status = (["\(info.product)\(info.name.isEmpty ? "" : " · " + info.name)", songTotal.map { "\($0) 首歌曲" } ?? ""].filter { !$0.isEmpty }).joined(separator: " · ")
            var cfg = SourceStore.shared.config(for: .emby)
            cfg.host = self.host; cfg.username = username; cfg.connected = true; cfg.statusNote = status
            SourceStore.shared.upsert(cfg)
            Task { await HomeStore.shared.refreshPlayed(); await StarStore.shared.load() }   // server-side plays / favorites from other clients
        } catch {
            connected = false
            if savedToken != nil { liveMode = false }
            status = error.localizedDescription
        }
    }

    func connect(host: String, username: String, password: String) async {
        loading = true
        status = "连接中…"
        defer { loading = false }
        do {
            let client = NavidromeClient.shared
            var h = host.trimmingCharacters(in: .whitespacesAndNewlines)
            h = h.replacingOccurrences(of: ":443", with: "")
            try await client.configure(host: h)
            try await client.login(username: username, password: password)
            let auth = await client.authSnapshot()
            coverBase = auth.base
            coverUser = auth.user
            coverToken = auth.token
            coverSalt = auth.salt
            let al = try await client.newestAlbums(size: 30)
            let ar = try await client.artists()
            albums = al
            artists = Array(ar.sorted { ($0.albumCount ?? 0) > ($1.albumCount ?? 0) }.prefix(40))
            connected = true
            self.user = username
            self.host = h
            status = "Navidrome · \(username)"
            Task { await loadTotals() }
            var cfg = SourceStore.shared.config(for: .navidrome)
            cfg.host = h
            cfg.username = username
            cfg.secret = password
            cfg.connected = true
            cfg.statusNote = status
            SourceStore.shared.upsert(cfg)
        } catch {
            connected = false
            status = error.localizedDescription
        }
    }

    /// maxBitRate from 偏好设置 (0 = original, no transcoding).
    static func streamQuery(_ id: String) -> [String: String] {
        let br = UserDefaults.standard.integer(forKey: "ym.maxBitRate")
        // Transcoded: force MP3 (server default here is Ogg/Opus, which AVPlayer cannot play).
        // 原始: ask for the untouched file so FLAC/ALAC/E-AC-3 reach AVPlayer bit-for-bit.
        return br > 0 ? ["id": id, "maxBitRate": "\(br)", "format": "mp3"] : ["id": id, "format": "raw"]
    }

    func loadTotals() async {
        // Album total = Σ albumCount over the artist index (one request), not a paged crawl.
        if let ar = try? await NavidromeClient.shared.artists(), !ar.isEmpty {
            albumTotal = ar.reduce(0) { $0 + ($1.albumCount ?? 0) }
        }
        if let first = try? await NavidromeClient.shared.albumPages(2) { allAlbums = first }
        songTotal = (try? await NavidromeClient.shared.scanCount()) ?? nil
        let a = albumTotal.map { "\($0) 张专辑" } ?? ""
        let s = songTotal.map { "\($0) 首歌曲" } ?? ""
        status = (["Navidrome · \(user)", a, s].filter { !$0.isEmpty }).joined(separator: " · ")
    }

    func fetchAlbum(id: String) async throws -> (NDAlbum, [NDSong]) {
        try await NavidromeClient.shared.album(id: id)
    }

    func search(query: String) async throws -> (albums: [NDAlbum], artists: [NDArtist], songs: [NDSong]) {
        try await NavidromeClient.shared.search(query: query)
    }

    func coverURL(_ album: NDAlbum, size: Int = 300) -> URL? {
        coverArtURL(id: album.coverId ?? album.id, size: size)
    }

    func coverArtURL(id: String?, size: Int = 300) -> URL? {
        guard let id else { return nil }
        if NavidromeClient.emby {
            guard let b = embyBase else { return nil }
            return EmbyClient.imageURL(base: b, id: id.hasPrefix("ar-") ? String(id.dropFirst(3)) : id, size: size)
        }
        if NavidromeClient.fnos {
            guard let r = fnosRoot else { return nil }
            return id.hasPrefix("ar-") ? nil : FnosClient.coverURL(root: r, coverId: id, size: size)
        }
        guard let base = coverBase, !coverToken.isEmpty else {
            // Before login: token-less cache-only URL (same id_size key) so disk-cached covers render instantly.
            var c = URLComponents(); c.scheme = CoverCache.offlineScheme; c.host = "cover"; c.path = "/getCoverArt.view"
            c.queryItems = [URLQueryItem(name: "id", value: id), URLQueryItem(name: "size", value: "\(size)")]
            return c.url
        }
        return NavidromeClient.authedURL(base: base, method: "getCoverArt.view",
                                         user: coverUser, token: coverToken, salt: coverSalt,
                                         query: ["id": id, "size": "\(size)"])
    }

    /// Subsonic stream.view — AVPlayer uses this URL (u+t+s auth).
    func streamURL(for songId: String) -> URL? {
        if NavidromeClient.emby {
            guard let b = embyBase, !embyToken.isEmpty else { return nil }
            return EmbyClient.streamURL(base: b, id: songId, token: embyToken, userId: embyUserId,
                                        maxKbps: UserDefaults.standard.integer(forKey: "ym.maxBitRate"), playSessionId: playSession(for: songId))
        }
        if NavidromeClient.fnos { return fnosRoot.flatMap { FnosClient.streamURL(root: $0, guid: songId) } }
        guard let base = coverBase, !coverToken.isEmpty else { return nil }
        return NavidromeClient.authedURL(base: base, method: "stream.view",
                                         user: coverUser, token: coverToken, salt: coverSalt,
                                         query: Self.streamQuery(songId))
    }
}
