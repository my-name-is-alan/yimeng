import Foundation

// 译梦 Core → shared ND* models, so every existing screen works unchanged. Paths beyond the agreed draft
// (albums/artists/tracks/genres/favorites listing) are assumptions — listed in the report for CodingBot.

extension CoreClient {
    nonisolated static func nd(_ t: CoreTrack) -> NDSong {
        CoreState.remember(t)
        let artist = t.artists?.compactMap(\.name).joined(separator: " / ").nilIfEmpty ?? t.artist?.name ?? ""
        var s = NDSong(id: t.id, title: t.title ?? t.name ?? "未知歌曲", artist: artist, album: t.album?.name, albumId: t.albumId ?? t.album?.id,
                       coverId: t.cover?.url, duration: t.durationMs.map { $0 / 1000 }, track: t.trackNo)
        if let a = t.audio {
            s.suffix = a.codec?.lowercased(); s.samplingRate = a.sampleRate; s.bitDepth = a.bitDepth; s.channelCount = a.channels
            s.bitRate = a.bitrate.map { $0 > 100_000 ? $0 / 1000 : $0 }
        }
        s.origin = t.origin; s.playable = t.playable; s.preview = t.preview
        s.previewStartMs = t.previewWindow?.startMs; s.previewEndMs = t.previewWindow?.endMs
        s.unplayableReason = t.unplayableReason; s.sortLetter = t.sortLetter; s.isrc = t.isrc; s.popularity = t.popularity
        s.hasLyrics = t.hasLyrics; s.colors = t.cover?.colors
        return s
    }
    nonisolated static func nd(_ a: CoreAlbum) -> NDAlbum {
        CoreState.remember(cover: a.cover)
        var x = NDAlbum(id: a.id, name: a.name ?? a.title ?? "未知专辑",
                        artist: a.artists?.compactMap(\.name).joined(separator: " / ").nilIfEmpty ?? a.artist?.name ?? "",
                        artistId: a.artists?.first?.id ?? a.artist?.id, coverId: a.cover?.url, year: a.year, songCount: a.trackCount)
        x.sortLetter = a.sortLetter; x.origin = a.origin; x.colors = a.cover?.colors
        return x
    }
    nonisolated static func nd(_ a: CoreArtist) -> NDArtist {
        CoreState.remember(cover: a.cover)
        var x = NDArtist(id: a.id, name: a.name, albumCount: a.albumCount, coverId: a.cover?.url)
        x.sortLetter = a.sortLetter
        return x
    }
    nonisolated static func nd(_ p: CorePlaylist) -> NDPlaylist {
        CoreState.remember(cover: p.cover)
        var x = NDPlaylist(id: p.id, name: p.name, songCount: p.trackCount, coverId: p.cover?.url, comment: p.description, owner: p.owner)
        x.origin = p.origin
        return x
    }
    private nonisolated static func sort(_ type: String) -> String {
        switch type { case "newest": "added"; case "recent": "played"; case "frequent": "plays"; case "random": "random"; case "starred": "favorite"; default: "name" }
    }
    private func originQ(_ q: [String: String]) -> [String: String] {
        var q = q; if let o = CoreState.originFilter { q["origin"] = o }; return q
    }
}

extension CoreClient: LibraryBackend {
    func albums(offset: Int, size: Int) async throws -> [NDAlbum] { try await albumList(type: "alphabeticalByName", offset: offset, size: size) }
    func albumList(type: String, offset: Int, size: Int) async throws -> [NDAlbum] {
        let p: CorePage<CoreAlbum> = try await page("/api/v1/albums", originQ(["sort": Self.sort(type)]), offset: offset, limit: size)
        return p.items.map(Self.nd)
    }
    func album(id: String) async throws -> (NDAlbum, [NDSong]) {
        let a: CoreAlbum = try await get("/api/v1/albums/\(Self.path(id))", cache: true)
        return (Self.nd(a), (a.tracks ?? []).map(Self.nd))
    }
    func search(_ q: String, count: Int) async throws -> (albums: [NDAlbum], artists: [NDArtist], songs: [NDSong]) {
        let r = try await searchAll(q, online: true)
        let online = CoreOrigin.all.flatMap { r.online?[$0] ?? [] } + (r.online?.filter { !CoreOrigin.all.contains($0.key) }.flatMap(\.value) ?? [])
        return ((r.albums ?? []).map(Self.nd), (r.artists ?? []).map(Self.nd), ((r.tracks ?? []) + online).prefix(count * 2).map(Self.nd))
    }
    func artists() async throws -> [NDArtist] {
        let p: CorePage<CoreArtist> = try await page("/api/v1/artists", originQ([:]), offset: 0, limit: 1000)
        return p.items.map(Self.nd)
    }
    func songs(offset: Int, count: Int) async throws -> [NDSong] {
        let p: CorePage<CoreTrack> = try await page("/api/v1/tracks", originQ(["sort": "name"]), offset: offset, limit: count)
        if offset == 0 { lastTrackTotal = p.total }
        return p.items.map(Self.nd)
    }
    func artistAlbums(_ id: String) async throws -> [NDAlbum] {
        let p: CorePage<CoreAlbum> = try await page("/api/v1/artists/\(Self.path(id))/albums", [:], offset: 0, limit: 200); return p.items.map(Self.nd)
    }
    func artistTracks(_ id: String, size: Int) async throws -> [NDSong] {
        let p: CorePage<CoreTrack> = try await page("/api/v1/artists/\(Self.path(id))/tracks", ["sort": "plays"], offset: 0, limit: size); return p.items.map(Self.nd)
    }
    func genres() async throws -> [NDGenre] { [] }   // not in the draft
    func songsByGenre(_ genre: String, offset: Int, count: Int) async throws -> [NDSong] { [] }
    func songTotal() async -> Int? {
        if let t = lastTrackTotal { return t }
        let p: CorePage<CoreTrack>? = try? await page("/api/v1/tracks", originQ(["sort": "name"]), offset: 0, limit: 1)
        return p?.total
    }
    func favorites() async throws -> (albums: [NDAlbum], songs: [NDSong], artists: [NDArtist]) {
        async let t: CorePage<CoreTrack> = page("/api/v1/favorites", ["type": "track"], offset: 0, limit: 500)
        async let a: CorePage<CoreAlbum> = page("/api/v1/favorites", ["type": "album"], offset: 0, limit: 500)
        async let r: CorePage<CoreArtist> = page("/api/v1/favorites", ["type": "artist"], offset: 0, limit: 500)
        return ((try await a).items.map(Self.nd), (try await t).items.map(Self.nd), ((try? await r)?.items ?? []).map(Self.nd))
    }
    func setFavorite(_ on: Bool, id: String, kind: NavidromeClient.StarKind) async throws {
        let type: String = switch kind { case .song: "track"; case .album: "album"; case .artist: "artist" }
        try await fire(on ? "PUT" : "DELETE", "/api/v1/favorites/\(type)/\(Self.path(id))")
    }
    func playlists() async throws -> [NDPlaylist] { try await playlists(scope: "mine").map(Self.nd) }
    func playlistTracks(_ id: String) async throws -> [NDSong] {
        let p: CorePage<CoreTrack> = try await page("/api/v1/playlists/\(Self.path(id))/tracks", [:], offset: 0, limit: 1000); return p.items.map(Self.nd)
    }
    func createPlaylist(name: String, songIds: [String]) async throws -> NDPlaylist? {
        struct B: Encodable { let name: String; let trackIds: [String] }
        let p: CorePlaylist = try await send("POST", "/api/v1/playlists", body: B(name: name, trackIds: songIds))
        return Self.nd(p)
    }
    func addToPlaylist(_ id: String, songIds: [String]) async throws {
        struct B: Encodable { let trackIds: [String] }
        try await fire("POST", "/api/v1/playlists/\(Self.path(id))/tracks", body: B(trackIds: songIds))
    }
    func setPlaylistSongs(_ id: String, songIds: [String]) async throws {
        struct B: Encodable { let trackIds: [String] }
        try await fire("PUT", "/api/v1/playlists/\(Self.path(id))/tracks", body: B(trackIds: songIds))   // full order = reorder
    }
    func removeFromPlaylist(_ id: String, indexes: [Int]) async throws {
        try await fire("DELETE", "/api/v1/playlists/\(Self.path(id))/tracks", query: ["indexes": indexes.map(String.init).joined(separator: ",")])
    }
    func deletePlaylist(_ id: String) async throws { try await fire("DELETE", "/api/v1/playlists/\(Self.path(id))") }
}

extension String { var nilIfEmpty: String? { isEmpty ? nil : self } }
