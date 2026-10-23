import Foundation
import SwiftUI

/// Demo music source kinds (adapters are stubs; credentials stored locally).
enum SourceKind: String, CaseIterable, Identifiable, Codable {
    case core, fnos, navidrome, emby, local
    var id: String { rawValue }
    var title: String {
        switch self {
        case .core: "译梦服务端"
        case .fnos: "飞牛音乐"
        case .navidrome: "Navidrome"
        case .emby: "Emby"
        case .local: "本机音乐"
        }
    }
    var subtitle: String {
        switch self {
        case .core: "Yimeng Core · 推荐"
        case .fnos: "fnOS · NAS"
        case .navidrome: "Subsonic API · nv"
        case .emby: "Emby REST API"
        case .local: "文件 App · iCloud 云盘"
        }
    }
    var tile: Color {
        switch self {
        case .core: Color(hex: 0x7a1cff)
        case .fnos: Color(hex: 0x2b6cb0)
        case .navidrome: Color(hex: 0x1f8a5b)
        case .emby: Color(hex: 0x3a7d3a)
        case .local: Color(hex: 0x5a5a66)
        }
    }
    var hostPlaceholder: String {
        switch self {
        case .core: "https://core.yimeng.lan:8443"
        case .fnos: "http://192.168.1.10:5666/music"
        case .navidrome: "https://music.example.com"
        case .emby: "http://192.168.1.10:8096"
        case .local: ""
        }
    }
    var usesUserPass: Bool { self == .navidrome || self == .fnos || self == .core || self == .emby }
    var usesToken: Bool { self == .core }
    var isLocal: Bool { self == .local }
    /// Keychain service for this source's secret (one item per source + account).
    var keychainService: String { "yimeng.\(rawValue)" }
    @MainActor var isImplemented: Bool { MusicSources.make(self).isImplemented }
}

// MARK: - Single active source

/// One backend. The app shows and plays exactly one of these at a time (no merging / dedup across sources).
@MainActor
protocol MusicSource {
    var kind: SourceKind { get }
    /// False → listed as 即将推出 and cannot be activated.
    var isImplemented: Bool { get }
    /// Bring up the library for this source (login with Keychain credentials, first fetches).
    func activate() async
    /// Drop live sessions / in-memory state. Disk caches stay (they are namespaced by source id).
    func deactivate()
}

enum MusicSources {
    @MainActor static func make(_ k: SourceKind) -> MusicSource {
        switch k {
        case .navidrome: NavidromeSource()
        case .fnos: FnosSource()
        case .emby: EmbySource()
        case .core: CoreSource()
        default: ComingSoonSource(kind: k)
        }
    }
    /// Namespace for per-source disk caches & persisted playback. Read from any thread.
    /// Cache/queue scope: the source id, plus the server for Core, so a mock or another
    /// Core never paints its ids (e.g. a stale mock queue) into this server's UI.
    static var scopeId: String {
        guard activeId == SourceKind.core.rawValue, let h = UserDefaults.standard.string(forKey: coreScopeKey), !h.isEmpty else { return activeId }
        let u = UserDefaults.standard.string(forKey: coreUserKey) ?? ""
        return "core-" + (h + (u.isEmpty ? "" : "-" + u)).map { $0.isLetter || $0.isNumber ? $0 : "_" }.reduce("") { $0 + String($1) }
    }
    static let coreScopeKey = "ym.core.scope"
    static let coreUserKey = "ym.core.user"
    nonisolated(unsafe) static var activeId: String = UserDefaults.standard.string(forKey: SourceStore.activeKey) ?? SourceKind.navidrome.rawValue
}

@MainActor struct NavidromeSource: MusicSource {
    let kind = SourceKind.navidrome
    let isImplemented = true
    func activate() async {
        await LibraryStore.shared.bootstrap()
        await PlayerStore.shared.restore()
    }
    func deactivate() { LibraryStore.shared.reset() }
}

@MainActor struct FnosSource: MusicSource {
    let kind = SourceKind.fnos
    let isImplemented = true
    func activate() async {
        await LibraryStore.shared.bootstrapFnos()
        await PlayerStore.shared.restore()
    }
    func deactivate() { LibraryStore.shared.reset() }
}

@MainActor struct EmbySource: MusicSource {
    let kind = SourceKind.emby
    let isImplemented = true
    func activate() async {
        await LibraryStore.shared.bootstrapEmby()
        await PlayerStore.shared.restore()
    }
    func deactivate() { LibraryStore.shared.reset() }
}

@MainActor struct CoreSource: MusicSource {
    let kind = SourceKind.core
    let isImplemented = true
    func activate() async {
        await LibraryStore.shared.bootstrapCore()
        await PlayerStore.shared.restore()
    }
    func deactivate() { LibraryStore.shared.reset(); CoreEvents.shared.stop() }
}

@MainActor struct ComingSoonSource: MusicSource {
    let kind: SourceKind
    let isImplemented = false
    func activate() async {}
    func deactivate() {}
}

struct SourceConfig: Identifiable, Codable, Equatable {
    var id: String { kind.rawValue }
    var kind: SourceKind
    var host: String = ""
    var username: String = ""
    var secret: String = ""          // password / API key / token
    var allowSelfSigned = false
    var connected = false
    var statusNote: String = ""

    /// `secret` is deliberately NOT persisted to UserDefaults — passwords live in the Keychain only.
    enum CodingKeys: String, CodingKey { case kind, host, username, allowSelfSigned, connected, statusNote }
}

@MainActor
final class SourceStore: ObservableObject {
    static let shared = SourceStore()
    private let key = "ym.sources.v1"
    nonisolated static let activeKey = "ym.activeSource"
    @Published var configs: [SourceConfig]
    /// The one source the app is showing. Persisted.
    @Published private(set) var active: SourceKind = SourceKind(rawValue: MusicSources.activeId) ?? .navidrome
    @Published private(set) var switching = false
    var activeSource: MusicSource { MusicSources.make(active) }

    /// Switch source: save + stop playback cleanly, drop in-memory library state, swap cache namespace, activate.
    func activate(_ kind: SourceKind) async {
        guard kind != active, MusicSources.make(kind).isImplemented, !switching else { return }
        switching = true; defer { switching = false }
        PlayerStore.shared.save()                 // resume point stays with the old source
        PlayerStore.shared.stopForSourceSwitch()
        activeSource.deactivate()
        LibraryCache.shared.resetMemory()
        RemoteDiffusion.shared.reset()
        CoverCache.shared.resetMemory()
        active = kind
        MusicSources.activeId = kind.rawValue
        UserDefaults.standard.set(kind.rawValue, forKey: Self.activeKey)
        PlayerStore.shared.reloadHistory()
        await activeSource.activate()
    }

    init() {
        Self.migrateLegacy()
        if let data = UserDefaults.standard.data(forKey: key),
           let decoded = try? JSONDecoder().decode([SourceConfig].self, from: data) {
            configs = decoded
        } else {
            configs = SourceKind.allCases.map {
                var c = SourceConfig(kind: $0)
                if $0 == .local { c.connected = true; c.statusNote = "已开启" }
                #if DEBUG
                if $0 == .navidrome {
                    c.host = "https://music.mefun.org"
                    c.username = "lvzy"
                }
                #endif
                return c
            }
        }
    }

    /// Pre-single-source builds stored caches/resume un-namespaced: move them under navidrome once.
    private static func migrateLegacy() {
        let d = UserDefaults.standard, fm = FileManager.default
        guard !d.bool(forKey: "ym.migrated.sourceNS") else { return }
        d.set(true, forKey: "ym.migrated.sourceNS")
        if let old = d.data(forKey: "ym.playback.v1") { d.set(old, forKey: "ym.playback.v1.navidrome"); d.removeObject(forKey: "ym.playback.v1") }
        let ns = SourceKind.navidrome.rawValue
        for (root, name) in [(FileManager.SearchPathDirectory.applicationSupportDirectory, "library"), (.cachesDirectory, "covers")] {
            let base = fm.urls(for: root, in: .userDomainMask)[0].appendingPathComponent(name, isDirectory: true)
            let dest = base.appendingPathComponent(ns, isDirectory: true)
            guard let items = try? fm.contentsOfDirectory(atPath: base.path) else { continue }
            try? fm.createDirectory(at: dest, withIntermediateDirectories: true)
            for i in items where !SourceKind.allCases.map(\.rawValue).contains(i) {
                try? fm.moveItem(at: base.appendingPathComponent(i), to: dest.appendingPathComponent(i))
            }
        }
    }

    func config(for kind: SourceKind) -> SourceConfig {
        configs.first(where: { $0.kind == kind }) ?? SourceConfig(kind: kind)
    }

    func upsert(_ c: SourceConfig) {
        if let i = configs.firstIndex(where: { $0.kind == c.kind }) { configs[i] = c }
        else { configs.append(c) }
        persist()
    }

    func disconnect(_ kind: SourceKind) {
        guard var c = configs.first(where: { $0.kind == kind }) else { return }
        c.connected = false
        c.statusNote = ""
        upsert(c)
    }

    /// Demo "connect": validates non-empty host (except local) and marks connected.
    func connectDemo(_ draft: SourceConfig) -> SourceConfig {
        var c = draft
        if c.kind.isLocal {
            c.connected = true
            c.statusNote = "已开启"
        } else if c.host.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            c.connected = false
            c.statusNote = "请填写服务器地址"
        } else {
            #if !DEBUG
            c.connected = false; c.statusNote = "即将推出"; upsert(c); return c
            #endif
            c.connected = true
            switch c.kind {
            case .navidrome: c.statusNote = "连接成功 · Navidrome 0.53 · 8,214 首"
            case .emby: c.statusNote = "连接成功 · Emby · 库已就绪"
            case .fnos: c.statusNote = "连接成功 · 飞牛音乐 · 等待同步"
            case .core: c.statusNote = "连接成功 · 译梦 Core · 合并曲库"
            case .local: break
            }
        }
        upsert(c)
        return c
    }

    var connectedCount: Int { configs.filter(\.connected).count }

    private func persist() {
        if let data = try? JSONEncoder().encode(configs) {
            UserDefaults.standard.set(data, forKey: key)
        }
    }
}
