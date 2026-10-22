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
        case .fnos: "https://fnos.home.lan:5666"
        case .navidrome: "https://music.home.lan:4533"
        case .emby: "https://emby.home.lan:8096"
        case .local: ""
        }
    }
    var usesUserPass: Bool { self == .navidrome || self == .fnos || self == .core }
    var usesToken: Bool { self == .emby || self == .core }
    var isLocal: Bool { self == .local }
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
}

@MainActor
final class SourceStore: ObservableObject {
    static let shared = SourceStore()
    private let key = "ym.sources.v1"
    @Published var configs: [SourceConfig]

    init() {
        if let data = UserDefaults.standard.data(forKey: key),
           let decoded = try? JSONDecoder().decode([SourceConfig].self, from: data) {
            configs = decoded
        } else {
            configs = SourceKind.allCases.map {
                var c = SourceConfig(kind: $0)
                if $0 == .local { c.connected = true; c.statusNote = "已开启" }
                return c
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
