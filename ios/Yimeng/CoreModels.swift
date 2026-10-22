import Foundation

// 译梦 Core — draft API models (openapi.yaml pending). Everything optional + lenient so field renames stay local to
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

/// `artist` may come as a plain string or an object; `artists` as [{id,name}].
struct CoreTrack: Codable, Hashable, Sendable {
    var id: String
    var title: String?
    var name: String?
    var artist: CoreLoose?
    var artists: [CoreRef]?
    var album: CoreLoose?
    var albumId: String?
    var origin: String?            // "local" | "netease" | "qq" | "kugou" …
    var playable: Bool?
    var preview: Bool?
    var previewWindow: CorePreviewWindow?
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
    var year: Int?
    var trackCount: Int?
    var cover: CoreCover?
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
    var origin: String?
    var owner: String?
    var description: String?
    var tracks: [CoreTrack]?
}

struct CorePage<T: Codable & Sendable>: Codable, Sendable {
    var items: [T]
    var nextCursor: String?
    var updatedAt: String?
    var total: Int?
}

struct CoreServerInfo: Codable, Sendable {
    var name: String?
    var version: String?
    var features: CoreFlags?
    var platforms: [CorePlatform]?
}
/// Platform account on the Core. Yimeng never recommends on its own — daily / fm / recommend are the platform's
/// account-based picks, passed through and labelled with the platform.
struct CorePlatform: Codable, Hashable, Sendable {
    var origin: String
    var enabled: Bool?
    var loggedIn: Bool?
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

struct CoreTokens: Codable, Sendable {
    var accessToken: String
    var refreshToken: String?
    var expiresIn: Int?
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
}

struct CoreChart: Codable, Hashable, Sendable {
    var id: String
    var name: String
    var origin: String?
    var cover: CoreCover?
    var updateFrequency: String?
    var updatedAt: String?
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
    var online: [String: [CoreTrack]]?
}
struct CoreSuggest: Codable, Sendable { var items: [String]? }

struct CoreProgress: Codable, Sendable { var trackId: String; var positionMs: Int }
struct CoreProgressList: Codable, Sendable { var items: [CoreProgress] }

struct CoreDaily: Codable, Sendable { var date: String?; var title: String?; var tracks: [CoreTrack]; var cover: CoreCover? }

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
