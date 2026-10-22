import Foundation
import AppIntents

/// App ⇄ widget snapshot. Needs an App Group, which a free personal team cannot provision on device,
/// so it is compiled in only with YM_APP_GROUP (on by default for simulator builds). Without it widgets
/// show their "打开译梦" placeholder; the play/pause intent still works (it runs in the app process).
enum YMShared {
    static let group = "group.art.yimeng.app"
    #if YM_APP_GROUP
    static let defaults = UserDefaults(suiteName: group)
    #else
    static let defaults: UserDefaults? = nil
    #endif
    static var available: Bool { defaults != nil }

    struct NowPlaying: Codable, Equatable { var id: String; var title: String; var artist: String; var playing: Bool; var cover: Data?; var updated: Date }
    struct Card: Codable, Equatable { var day: String; var quote: String; var title: String; var artist: String; var cover: Data? }
    struct FM: Codable, Equatable { var origins: [String] }

    static func write<T: Encodable>(_ v: T?, _ key: String) {
        guard let d = defaults else { return }
        if let v, let data = try? JSONEncoder().encode(v) { d.set(data, forKey: key) } else { d.removeObject(forKey: key) }
    }
    static func read<T: Decodable>(_ t: T.Type, _ key: String) -> T? {
        guard let data = defaults?.data(forKey: key) else { return nil }
        return try? JSONDecoder().decode(T.self, from: data)
    }
    static let kNow = "w.now", kCard = "w.card", kFM = "w.fm"
    static let kindNow = "ym.now", kindCard = "ym.card", kindFM = "ym.fm"
}

/// Hooks the app installs at launch; intents compiled into the widget call through them when run in-app.
enum YMIntentHooks {
    nonisolated(unsafe) static var toggle: (@MainActor () -> Void)?
    nonisolated(unsafe) static var next: (@MainActor () -> Void)?
}

/// Interactive play/pause from the widget. AudioPlaybackIntent ⇒ the system runs it in the app process.
struct TogglePlaybackIntent: AudioPlaybackIntent {
    static var title: LocalizedStringResource = "播放/暂停"
    static var isDiscoverable: Bool = false
    init() {}
    @MainActor func perform() async throws -> some IntentResult { YMIntentHooks.toggle?(); return .result() }
}
struct NextTrackIntent: AudioPlaybackIntent {
    static var title: LocalizedStringResource = "下一首"
    static var isDiscoverable: Bool = false
    init() {}
    @MainActor func perform() async throws -> some IntentResult { YMIntentHooks.next?(); return .result() }
}
