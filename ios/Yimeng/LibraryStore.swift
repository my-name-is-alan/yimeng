import Foundation
import SwiftUI

struct NDAlbum: Identifiable, Hashable {
    let id: String
    let name: String
    let artist: String
    let coverId: String?
    let year: Int?
    let songCount: Int?
}

struct NDArtist: Identifiable, Hashable {
    let id: String
    let name: String
    let albumCount: Int?
}

struct NDPlaylist: Identifiable, Hashable {
    let id: String
    let name: String
    let songCount: Int?
    let coverId: String?
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

    static let defaultHost = "https://music.mefun.org"
    static let defaultUser = "lvzy"

    // Cached auth for cover / stream URLs (never logged)
    private var coverBase: URL?
    private var coverUser = ""
    private var coverToken = ""
    private var coverSalt = ""

    /// When true, UI must show only API rows (no DreamData / Mock mix).
    var useLiveOnly: Bool { connected }

    func bootstrap() async {
        var cfg = SourceStore.shared.config(for: .navidrome)
        if cfg.host.isEmpty || cfg.host.contains(":4533") || cfg.host.contains(":443") {
            cfg.host = Self.defaultHost
        }
        if cfg.username.isEmpty { cfg.username = Self.defaultUser }
        if let pw = Keychain.get(service: "yimeng.navidrome", account: Self.defaultUser), !pw.isEmpty {
            cfg.secret = pw
        }
        // Seed Keychain only if missing (env / Application Support) — never log the value
        if cfg.secret.isEmpty, let seeded = Self.readSeedPassword() {
            cfg.secret = seeded
            Keychain.set(seeded, service: "yimeng.navidrome", account: Self.defaultUser)
        }
        SourceStore.shared.upsert(cfg)
        guard !cfg.secret.isEmpty else {
            status = "待连接 Navidrome"
            return
        }
        await connect(host: cfg.host, username: cfg.username.isEmpty ? Self.defaultUser : cfg.username, password: cfg.secret)
    }

    private static func readSeedPassword() -> String? {
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
            artists = Array(ar.prefix(40))
            connected = true
            status = "Navidrome · \(al.count) 张专辑"
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
        guard let id, let base = coverBase, !coverToken.isEmpty else { return nil }
        return NavidromeClient.authedURL(base: base, method: "getCoverArt.view",
                                         user: coverUser, token: coverToken, salt: coverSalt,
                                         query: ["id": id, "size": "\(size)"])
    }

    /// Subsonic stream.view — AVPlayer uses this URL (u+t+s auth).
    func streamURL(for songId: String) -> URL? {
        guard let base = coverBase, !coverToken.isEmpty else { return nil }
        return NavidromeClient.authedURL(base: base, method: "stream.view",
                                         user: coverUser, token: coverToken, salt: coverSalt,
                                         query: ["id": songId])
    }
}
