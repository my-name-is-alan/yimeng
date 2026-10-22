import Foundation

/// A non-Subsonic library backend. When the active source is one of these, `NavidromeClient` forwards its
/// library calls here, so every screen keeps working on the shared ND* models.
protocol LibraryBackend: Actor {
    func albums(offset: Int, size: Int) async throws -> [NDAlbum]
    /// Subsonic-style list types: alphabeticalByName, newest, recent, frequent, random, starred.
    func albumList(type: String, offset: Int, size: Int) async throws -> [NDAlbum]
    func album(id: String) async throws -> (NDAlbum, [NDSong])
    func search(_ q: String, count: Int) async throws -> (albums: [NDAlbum], artists: [NDArtist], songs: [NDSong])
    func artists() async throws -> [NDArtist]
    func songs(offset: Int, count: Int) async throws -> [NDSong]
    func artistAlbums(_ id: String) async throws -> [NDAlbum]
    func artistTracks(_ id: String, size: Int) async throws -> [NDSong]
    func genres() async throws -> [NDGenre]
    func albumsByGenre(_ genre: String, offset: Int, size: Int) async throws -> [NDAlbum]
    func songsByGenre(_ genre: String, offset: Int, count: Int) async throws -> [NDSong]
    func songTotal() async -> Int?
    func favorites() async throws -> (albums: [NDAlbum], songs: [NDSong], artists: [NDArtist])
    func setFavorite(_ on: Bool, id: String, kind: NavidromeClient.StarKind) async throws
    func playlists() async throws -> [NDPlaylist]
    func playlistTracks(_ id: String) async throws -> [NDSong]
    func createPlaylist(name: String, songIds: [String]) async throws -> NDPlaylist?
    func addToPlaylist(_ id: String, songIds: [String]) async throws
    func setPlaylistSongs(_ id: String, songIds: [String]) async throws
    func removeFromPlaylist(_ id: String, indexes: [Int]) async throws
    func deletePlaylist(_ id: String) async throws
}

enum BackendError {
    static func unsupported(_ what: String) -> NavidromeError { .api("当前音源暂不支持\(what)") }
}

extension LibraryBackend {
    func albumList(type: String, offset: Int, size: Int) async throws -> [NDAlbum] { try await albums(offset: offset, size: size) }
    func albumsByGenre(_ genre: String, offset: Int, size: Int) async throws -> [NDAlbum] { [] }
    func createPlaylist(name: String, songIds: [String]) async throws -> NDPlaylist? { throw BackendError.unsupported("编辑歌单") }
    func addToPlaylist(_ id: String, songIds: [String]) async throws { throw BackendError.unsupported("编辑歌单") }
    func setPlaylistSongs(_ id: String, songIds: [String]) async throws { throw BackendError.unsupported("编辑歌单") }
    func removeFromPlaylist(_ id: String, indexes: [Int]) async throws { throw BackendError.unsupported("编辑歌单") }
    func deletePlaylist(_ id: String) async throws { throw BackendError.unsupported("编辑歌单") }
}

extension FnosClient: LibraryBackend {
    func favorites() async throws -> (albums: [NDAlbum], songs: [NDSong], artists: [NDArtist]) { ([], try await favoriteSongs(), []) }
    func setFavorite(_ on: Bool, id: String, kind: NavidromeClient.StarKind) async throws {
        guard kind == .song else { throw NavidromeError.api("飞牛音乐只支持收藏歌曲") }
        try await setFavorite(on, song: id)
    }
}
