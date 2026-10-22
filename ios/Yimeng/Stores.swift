import SwiftUI

/// 首页 payload, persisted to disk and kept alive at app level (stale-while-revalidate).
struct HomePayload: Codable {
    var banner: [NDAlbum] = []
    var newest: [NDAlbum] = []
    var recent: [NDAlbum] = []
    var frequent: [NDAlbum] = []
    var random: [NDAlbum] = []
    var artists: [NDArtist] = []
    var playlists: [NDPlaylist] = []
    var genres: [NDGenre] = []
    var savedAt = Date.distantPast
    var isEmpty: Bool { banner.isEmpty && newest.isEmpty && artists.isEmpty }
}

@MainActor
final class HomeStore: ObservableObject {
    static let shared = HomeStore()
    @Published private(set) var data: HomePayload
    @Published private(set) var refreshing = false
    private static let ttl: TimeInterval = 10 * 60
    private static var file: URL {
        let d = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        try? FileManager.default.createDirectory(at: d, withIntermediateDirectories: true)
        return d.appendingPathComponent("home.v1.json")
    }

    init() {
        data = (try? JSONDecoder().decode(HomePayload.self, from: Data(contentsOf: Self.file))) ?? HomePayload()
    }

    func replacePlaylist(_ p: NDPlaylist) {
        if let i = data.playlists.firstIndex(where: { $0.id == p.id }) { data.playlists[i] = p } else { data.playlists.insert(p, at: 0) }
        persist()
    }
    func removePlaylist(_ id: String) { data.playlists.removeAll { $0.id == id }; persist() }
    private func persist() { if let d = try? JSONEncoder().encode(data) { try? d.write(to: Self.file, options: .atomic) } }

    /// Refresh silently if stale (or forced by pull-to-refresh). Never clears what's on screen.
    func refresh(force: Bool = false) async {
        guard !refreshing, force || Date().timeIntervalSince(data.savedAt) > Self.ttl || data.isEmpty else { return }
        refreshing = true; defer { refreshing = false }
        let c = NavidromeClient.shared
        async let n = c.albumList(type: "newest", offset: 0, size: 20)
        async let r = c.albumList(type: "recent", offset: 0, size: 20)
        async let f = c.albumList(type: "frequent", offset: 0, size: 20)
        async let rd = c.albumList(type: "random", offset: 0, size: 26)
        async let ar = c.artists()
        async let pl = c.playlists()
        async let g = c.genres()
        var next = HomePayload()
        next.newest = (try? await n) ?? data.newest
        next.recent = (try? await r) ?? data.recent
        next.frequent = (try? await f) ?? data.frequent
        let rand = (try? await rd) ?? []
        // Keep the banner stable across refreshes unless empty — avoids slides jumping under the user.
        next.banner = data.banner.isEmpty ? Array(rand.prefix(6)) : data.banner
        next.random = rand.isEmpty ? data.random : Array(rand.dropFirst(6))
        let arts = (try? await ar) ?? []
        next.artists = arts.isEmpty ? data.artists : Array(arts.sorted { ($0.albumCount ?? 0) > ($1.albumCount ?? 0) }.prefix(16))
        next.playlists = (try? await pl).map { Array($0.prefix(16)) } ?? data.playlists
        next.genres = (try? await g).map { Array($0.prefix(16)) } ?? data.genres
        if force { next.banner = rand.isEmpty ? data.banner : Array(rand.prefix(6)) }
        next.savedAt = Date()
        guard !next.isEmpty else { return }
        withAnimation(.smooth(duration: 0.35)) { data = next }
        if let d = try? JSONEncoder().encode(next) { try? d.write(to: Self.file, options: .atomic) }
        // Warm covers to disk so the next launch paints them on frame one.
        let lib = LibraryStore.shared
        let urls = (next.banner.map { lib.coverURL($0, size: 800) } + next.banner.map { lib.coverURL($0, size: 600) }
                    + (next.newest + next.random).prefix(30).map { lib.coverURL($0, size: 300) }).compactMap { $0 }
        for u in urls { _ = try? await CoverCache.shared.fetch(u) }
    }
}

/// The user's 典藏: starred albums / songs / artists, with optimistic toggles.
@MainActor
final class StarStore: ObservableObject {
    static let shared = StarStore()
    @Published private(set) var albums: [NDAlbum] = []
    @Published private(set) var songs: [NDSong] = []
    @Published private(set) var artists: [NDArtist] = []
    @Published private(set) var loaded = false
    @Published private(set) var failed = false

    func isStarred(album id: String) -> Bool { albums.contains { $0.id == id } }
    func isStarred(song id: String) -> Bool { songs.contains { $0.id == id } }
    func isStarred(artist id: String) -> Bool { artists.contains { $0.id == id } }

    func load() async {
        do {
            let s = try await NavidromeClient.shared.starred()
            albums = s.albums; songs = s.songs; artists = s.artists
            failed = false
        } catch { failed = true }
        loaded = true
    }

    func toggle(_ a: NDAlbum) {
        let on = !isStarred(album: a.id)
        withAnimation(.snappy) { if on { albums.insert(a, at: 0) } else { albums.removeAll { $0.id == a.id } } }
        Task { do { try await NavidromeClient.shared.setStar(on, id: a.id, kind: .album) } catch { await load() } }
    }
    func toggle(_ s: NDSong) {
        let on = !isStarred(song: s.id)
        withAnimation(.snappy) { if on { songs.insert(s, at: 0) } else { songs.removeAll { $0.id == s.id } } }
        Task { do { try await NavidromeClient.shared.setStar(on, id: s.id, kind: .song) } catch { await load() } }
    }
    func toggle(_ ar: NDArtist) {
        let on = !isStarred(artist: ar.id)
        withAnimation(.snappy) { if on { artists.insert(ar, at: 0) } else { artists.removeAll { $0.id == ar.id } } }
        Task { do { try await NavidromeClient.shared.setStar(on, id: ar.id, kind: .artist) } catch { await load() } }
    }
}

/// Doodle heart toggle (solid, quiet).
struct StarButton: View {
    let on: Bool
    var size: CGFloat = 18
    let action: () -> Void
    @Environment(\.colorScheme) var scheme
    var body: some View {
        Button(action: action) {
            Image(systemName: on ? "heart.fill" : "heart")
                .font(.system(size: size, weight: .semibold))
                .foregroundStyle(on ? Color(hex: 0xff5fa8) : Theme.secondary(scheme))
                .symbolEffect(.bounce, value: on)
                .frame(width: 36, height: 36).contentShape(Rectangle())
        }.buttonStyle(.plain)
    }
}
