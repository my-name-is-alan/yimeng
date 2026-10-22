import Foundation

// 译梦 Core API v1.1 models (aligned with core/api/openapi.yaml @ fc189b0). Everything optional + lenient so field renames stay local to
// this file. Decoded with `.convertFromSnakeCase` (`duration_ms` → `durationMs`).

struct CoreCover: Codable, Hashable, Sendable {
    var url: String?
    var blurhash: String?
    var colors: [String]?          // 3 × "#rrggbb"
}

struct CoreAudio: Codable, Hashable, Sendable {
    var codec: String?
    var sampleRate: Int?
    var bitDepth: Int?
    var bitrate: Int?              // kbps (draft doesn't say; treated as kbps when < 100 000, else bps)
    var channels: Int?
}

struct CoreRef: Codable, Hashable, Sendable { var id: String?; var name: String? }

struct CorePreviewWindow: Codable, Hashable, Sendable { var startMs: Int?; var endMs: Int? }
/// `preview` is `false`/`true` on online search/list items and `{start_ms,end_ms}` (nullable) on full tracks.
enum CorePreviewField: Codable, Hashable, Sendable {
    case flag(Bool), window(CorePreviewWindow)
    init(from d: Decoder) throws {
        let c = try d.singleValueContainer()
        if let b = try? c.decode(Bool.self) { self = .flag(b) } else { self = .window(try c.decode(CorePreviewWindow.self)) }
    }
    func encode(to e: Encoder) throws { var c = e.singleValueContainer(); switch self { case .flag(let b): try c.encode(b); case .window(let w): try c.encode(w) } }
    var window: CorePreviewWindow? { if case .window(let w) = self { return w }; return nil }
    var isOn: Bool { switch self { case .flag(let b): b; case .window: true } }
}

/// `artist` may come as a plain string or an object; `artists` as [{id,name}].
struct CoreTrack: Codable, Hashable, Sendable {
    var id: String
    var title: String?
    var name: String?
    var artist: CoreLoose?
    var artists: [CoreRef]?
    var artistName: String?
    var album: CoreLoose?
    var albumId: String?
    var albumTitle: String?
    var origin: String?            // "local" | "netease" | "qq" | "kugou" …
    var playable: Bool?
    /// full | preview | unavailable (online tracks; non-VIP gets the official preview only)
    var entitlement: String?
    var preview: CorePreviewField?
    var previewWindow: CorePreviewWindow?
    var coverUrl: String?
    var format: String?
    var playCount: Int?
    var unplayableReason: String?
    var durationMs: Int?
    var hasLyrics: Bool?
    var favorite: Bool?
    var audio: CoreAudio?
    var cover: CoreCover?
    var streamUrl: String?
    var sortLetter: String?
    var trackNo: Int?
    var isrc: String?
    var popularity: Double?
}

/// A string-or-{id,name} field.
enum CoreLoose: Codable, Hashable, Sendable {
    case text(String), ref(CoreRef)
    init(from d: Decoder) throws {
        let c = try d.singleValueContainer()
        if let s = try? c.decode(String.self) { self = .text(s) } else { self = .ref(try c.decode(CoreRef.self)) }
    }
    func encode(to e: Encoder) throws {
        var c = e.singleValueContainer()
        switch self { case .text(let s): try c.encode(s); case .ref(let r): try c.encode(r) }
    }
    var name: String? { switch self { case .text(let s): s; case .ref(let r): r.name } }
    var id: String? { switch self { case .text: nil; case .ref(let r): r.id } }
}

struct CoreAlbum: Codable, Hashable, Sendable {
    var id: String
    var name: String?
    var title: String?
    var artist: CoreLoose?
    var artists: [CoreRef]?
    var artistName: String?
    var year: Int?
    var trackCount: Int?
    var cover: CoreCover?
    var coverUrl: String?
    var hasCover: Bool?
    var origin: String?
    var sortLetter: String?
    var tracks: [CoreTrack]?
}

struct CoreArtist: Codable, Hashable, Sendable {
    var id: String
    var name: String
    var albumCount: Int?
    var cover: CoreCover?
    var origin: String?
    var sortLetter: String?
}

struct CorePlaylist: Codable, Hashable, Sendable {
    var id: String
    var name: String
    var trackCount: Int?
    var cover: CoreCover?
    var coverUrl: String?
    var subtitle: String?
    var kind: String?
    var origin: String?
    var owner: String?
    var description: String?
    var tracks: [CoreTrack]?
}

struct CorePage<T: Codable & Sendable>: Codable, Sendable {
    var items: [T]
    var nextCursor: String?        // opaque — passed back unchanged
    var updatedAt: String?
    var total: Int?
}

struct CoreServerInfo: Codable, Sendable {
    var serverName: String?
    var name: String?
    var apiVersion: String?
    var authenticated: Bool?
    var version: String?
    var features: CoreFlags?
    var platforms: [CorePlatform]?
}
/// Platform account on the Core. Yimeng never recommends on its own — daily / fm / recommend are the platform's
/// account-based picks, passed through and labelled with the platform.
struct CorePlatform: Codable, Hashable, Sendable {
    var origin: String
    var name: String?
    var enabled: Bool?
    var loggedIn: Bool?
    var vip: Bool?
    var nickname: String?
    var capabilities: [String]?
    var active: Bool { (enabled ?? true) && (loggedIn ?? false) }
    func can(_ c: String) -> Bool { active && (capabilities ?? []).contains(c) }
}
/// Feature flags as `{"lyrics":true}` or `["lyrics", …]`.
struct CoreFlags: Codable, Sendable {
    var on: Set<String>
    init(from d: Decoder) throws {
        let c = try d.singleValueContainer()
        if let a = try? c.decode([String].self) { on = Set(a) }
        else { on = Set(((try? c.decode([String: Bool].self)) ?? [:]).filter(\.value).map(\.key)) }
    }
    func encode(to e: Encoder) throws { var c = e.singleValueContainer(); try c.encode(Array(on)) }
    func has(_ k: String) -> Bool { on.contains(k) }
}

/// Login / pairing / refresh. Live servers answer either `{access_token, refresh_token, expires_in}` or the
/// openapi `Session {token, device, user}` (`POST /pair` → `{device, token}`); the ymd_ device token then doubles as
/// the refresh token for `POST /auth/refresh`.
struct CoreTokens: Codable, Sendable {
    var accessToken: String
    var refreshToken: String?
    var expiresIn: Int?
    enum K: String, CodingKey { case accessToken = "access_token", refreshToken = "refresh_token", expiresIn = "expires_in", token }
    init(accessToken: String, refreshToken: String?, expiresIn: Int?) { self.accessToken = accessToken; self.refreshToken = refreshToken; self.expiresIn = expiresIn }
    init(from d: Decoder) throws {
        let c = try d.container(keyedBy: K.self)
        let tok = try c.decodeIfPresent(String.self, forKey: .token)
        guard let a = try c.decodeIfPresent(String.self, forKey: .accessToken) ?? tok else {
            throw DecodingError.keyNotFound(K.accessToken, .init(codingPath: [], debugDescription: "no access_token/token"))
        }
        accessToken = a
        refreshToken = try c.decodeIfPresent(String.self, forKey: .refreshToken) ?? tok
        expiresIn = try c.decodeIfPresent(Int.self, forKey: .expiresIn)
    }
    func encode(to e: Encoder) throws {
        var c = e.container(keyedBy: K.self)
        try c.encode(accessToken, forKey: .accessToken); try c.encodeIfPresent(refreshToken, forKey: .refreshToken); try c.encodeIfPresent(expiresIn, forKey: .expiresIn)
    }
}
/// `{error, code, message}`
struct CoreAPIError: LocalizedError, Sendable {
    let status: Int; let code: String; let message: String
    var errorDescription: String? { message.isEmpty ? code : message }
}

struct CoreChartsEntry: Codable, Hashable, Sendable { var title: String?; var origins: [String]?; var cover: CoreCover? }

struct CoreHome: Codable, Sendable {
    var banner: [CoreAlbum]?
    var recentlyAdded: [CoreAlbum]?
    var recentlyPlayed: [CoreAlbum]?
    var frequent: [CoreAlbum]?
    var random: [CoreAlbum]?          // factual library sections only — never labelled 推荐
    var topArtists: [CoreArtist]?
    var playlists: [CorePlaylist]?
    var charts: CoreChartsEntry?
    var recentlyPlayedTracks: [CoreTrack]?
    var frequentTracks: [CoreTrack]?
    var localTracks: Int?
    var mode: String?
    /// Per-platform pass-through sections (`{id, title, origin, kind, playlists|tracks, error?}`).
    var sections: [CoreHomeSection]?
}
struct CoreHomeSection: Codable, Sendable {
    var id: String
    var title: String?
    var origin: String?
    var kind: String?
    var playlists: [CorePlaylist]?
    var tracks: [CoreTrack]?
    var error: String?
}

struct CoreChart: Codable, Hashable, Sendable {
    var id: String
    var name: String
    var origin: String?
    var cover: CoreCover?
    var updateFrequency: String?
    var updatedAt: String?
    var subtitle: String?
    var description: String?
    var coverUrl: String?
    var trackCount: Int?
    var preview: [CoreTrack]?      // top few tracks for the card
}
struct CoreCharts: Codable, Sendable { var origins: [String]?; var charts: [CoreChart] }

struct CoreLyricWord: Codable, Hashable, Sendable { var tMs: Int; var dMs: Int?; var text: String }
struct CoreLyricLine: Codable, Hashable, Sendable {
    var tMs: Int
    var text: String
    var trans: String?
    var roma: String?
    var words: [CoreLyricWord]?
}
struct CoreLyrics: Codable, Sendable {
    var found: Bool?
    var message: String?
    var source: String?
    var lines: [CoreLyricLine]?
}

struct CoreDream: Codable, Sendable {
    var colors: [String]?
    var tags: [String]?
    var bpm: Double?
}

struct CoreSearch: Codable, Sendable {
    var tracks: [CoreTrack]?
    var albums: [CoreAlbum]?
    var artists: [CoreArtist]?
    var playlists: [CorePlaylist]?
    var online: [CoreOnlineGroup]?
}
struct CoreOnlineGroup: Codable, Sendable {
    var origin: String
    var name: String?
    var result: CoreOnlineResult?
    var error: String?
}
struct CoreOnlineResult: Codable, Sendable { var tracks: [CoreTrack]?; var albums: [CoreAlbum]?; var playlists: [CorePlaylist]? }
struct CoreSuggest: Codable, Sendable { var items: [String]? }

struct CoreProgress: Codable, Sendable { var trackId: String; var positionMs: Int; var durationMs: Int?; var finished: Bool? }
struct CoreProgressList: Codable, Sendable { var items: [CoreProgress] }

struct CoreDaily: Codable, Sendable {
    var date: String?; var title: String?; var tracks: [CoreTrack]?; var items: [CoreTrack]?; var cover: CoreCover?
    var list: [CoreTrack] { tracks ?? items ?? [] }
}

enum CoreOrigin {
    static let all = ["local", "netease", "qq", "kugou"]
    static func label(_ o: String?) -> String {
        switch o ?? "local" {
        case "local": "本地"
        case "netease": "网易云"
        case "qq": "QQ"
        case "kugou": "酷狗"
        default: o ?? ""
        }
    }
}
