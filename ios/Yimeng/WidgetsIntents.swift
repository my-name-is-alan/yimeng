import SwiftUI
import AppIntents
import WidgetKit
import UIKit

// MARK: - Feeding the widgets (only when an App Group is compiled in; see YMShared)

@MainActor
enum YMWidgetBridge {
    private static var lastId = ""
    private static var lastPlaying: Bool?
    private static var coverData: Data?

    static func installHooks() {
        YMIntentHooks.toggle = { AppStateBridge.current?.setPlaying(!PlayerStore.shared.isPlaying) ?? PlayerStore.shared.toggle() }
        YMIntentHooks.next = { PlayerStore.shared.skip(1) }
    }

    static func nowPlaying(_ s: NDSong?, playing: Bool, coverURL: URL?) {
        guard YMShared.available else { return }
        guard let s else { YMShared.write(Optional<YMShared.NowPlaying>.none, YMShared.kNow); WidgetCenter.shared.reloadTimelines(ofKind: YMShared.kindNow); return }
        if s.id == lastId, playing == lastPlaying { return }
        let changed = s.id != lastId
        lastId = s.id; lastPlaying = playing
        if changed { coverData = nil }
        write(s, playing)
        if changed, let u = LibraryStore.shared.coverArtURL(id: s.coverId ?? s.albumId, size: 300) ?? coverURL {
            Task {
                guard let img = try? await CoverCache.shared.fetch(u), lastId == s.id else { return }
                coverData = img.preparingThumbnail(of: CGSize(width: 240, height: 240))?.jpegData(compressionQuality: 0.8)
                write(s, lastPlaying ?? playing)
            }
        }
    }
    private static func write(_ s: NDSong, _ playing: Bool) {
        YMShared.write(YMShared.NowPlaying(id: s.id, title: s.title, artist: s.artist, playing: playing, cover: coverData, updated: .now), YMShared.kNow)
        WidgetCenter.shared.reloadTimelines(ofKind: YMShared.kindNow)
    }
    static func card(_ c: YueqianStore.Card?, cover: UIImage?) {
        guard YMShared.available, let c else { return }
        let f = DateFormatter(); f.dateFormat = "M月d日"
        YMShared.write(YMShared.Card(day: f.string(from: c.date), quote: c.quote, title: c.song.title, artist: c.song.artist,
                                     cover: cover?.preparingThumbnail(of: CGSize(width: 240, height: 240))?.jpegData(compressionQuality: 0.8)), YMShared.kCard)
        WidgetCenter.shared.reloadTimelines(ofKind: YMShared.kindCard)
    }
    static func fm(_ origins: [String]) {
        guard YMShared.available else { return }
        YMShared.write(YMShared.FM(origins: origins), YMShared.kFM)
        WidgetCenter.shared.reloadTimelines(ofKind: YMShared.kindFM)
    }
    static func clear() {
        for k in [YMShared.kNow, YMShared.kCard, YMShared.kFM] { YMShared.defaults?.removeObject(forKey: k) }
        lastId = ""; lastPlaying = nil
        WidgetCenter.shared.reloadAllTimelines()
    }

    /// yimeng://fm · yimeng://yueqian · yimeng://player (widget taps — always a user action).
    static func open(_ url: URL) {
        guard url.scheme == "yimeng" else { return }
        switch url.host {
        case "fm": Task { await YMActions.playFM() }
        case "yueqian": Nav.go(.yueqian)
        case "player": AppStateBridge.current?.nowPlaying = true
        default: break
        }
    }
}

// MARK: - Shared actions (Shortcuts + widget URLs)

@MainActor
enum YMActions {
    enum Failure: Error, CustomLocalizedStringResourceConvertible {
        case needsCore, nothing(String)
        var localizedStringResource: LocalizedStringResource {
            switch self { case .needsCore: "这个功能需要连接译梦服务端"; case .nothing(let s): "\(s)" }
        }
    }
    static func playDaily() async throws {
        guard NavidromeClient.core else { throw Failure.needsCore }
        if PlatformStore.shared.dailies.isEmpty { await PlatformStore.shared.refresh() }
        guard let d = PlatformStore.shared.dailies.first, !d.tracks.isEmpty else { throw Failure.nothing("今天还没有每日推荐（登录音乐平台后可用）") }
        PlayerStore.shared.play(songs: d.tracks.filter { !$0.isUnplayable })
    }
    static func playFM() async {
        guard NavidromeClient.core else { SheetCenter.shared.say("私人 FM 需要译梦服务端"); return }
        if PlatformStore.shared.fmOrigins.isEmpty { await PlatformStore.shared.refresh() }
        guard let o = PlatformStore.shared.fmOrigins.first else { SheetCenter.shared.say("登录网易云后才有私人 FM"); return }
        await FMStore.shared.start(o)
    }
    static func allPlaylists() async -> [NDPlaylist] {
        var out = (try? await NavidromeClient.shared.playlists()) ?? []
        if NavidromeClient.core {
            if PlatformStore.shared.userPlaylists.isEmpty { await PlatformStore.shared.loadUserPlaylists() }
            for u in PlatformStore.shared.userPlaylists { out += u.created + u.collected }
        }
        return out
    }
    static func playPlaylist(named q: String) async throws -> String {
        let all = await allPlaylists()
        let key = q.trimmingCharacters(in: .whitespaces).lowercased()
        guard let p = all.first(where: { $0.name.lowercased() == key }) ?? all.first(where: { $0.name.lowercased().contains(key) }) else {
            throw Failure.nothing("没找到叫「\(q)」的歌单")
        }
        let songs = (try? await NavidromeClient.shared.playlist(id: p.id)) ?? []
        guard !songs.isEmpty else { throw Failure.nothing("歌单「\(p.name)」是空的") }
        PlayerStore.shared.play(songs: songs.filter { !$0.isUnplayable })
        return p.name
    }
}

// MARK: - App Intents / App Shortcuts (no capability needed — works on a free team)

struct PlayDailyIntent: AppIntent {
    static var title: LocalizedStringResource = "播放每日推荐"
    static var description = IntentDescription("播放今天的每日推荐。")
    static var openAppWhenRun = true
    @MainActor func perform() async throws -> some IntentResult & ProvidesDialog {
        try await YMActions.playDaily(); return .result(dialog: "开始播放每日推荐")
    }
}

struct PlayFMIntent: AppIntent {
    static var title: LocalizedStringResource = "播放私人 FM"
    static var openAppWhenRun = true
    @MainActor func perform() async throws -> some IntentResult & ProvidesDialog {
        guard NavidromeClient.core else { throw YMActions.Failure.needsCore }
        await YMActions.playFM(); return .result(dialog: "私人 FM 开始了")
    }
}

struct PlaylistEntity: AppEntity {
    static var typeDisplayRepresentation: TypeDisplayRepresentation = "歌单"
    static var defaultQuery = PlaylistQuery()
    var id: String; var name: String
    var displayRepresentation: DisplayRepresentation { DisplayRepresentation(title: "\(name)") }
}
struct PlaylistQuery: EntityStringQuery {
    @MainActor func entities(for ids: [String]) async throws -> [PlaylistEntity] {
        await YMActions.allPlaylists().filter { ids.contains($0.id) }.map { PlaylistEntity(id: $0.id, name: $0.name) }
    }
    @MainActor func entities(matching s: String) async throws -> [PlaylistEntity] {
        await YMActions.allPlaylists().filter { $0.name.localizedCaseInsensitiveContains(s) }.map { PlaylistEntity(id: $0.id, name: $0.name) }
    }
    @MainActor func suggestedEntities() async throws -> [PlaylistEntity] {
        Array(await YMActions.allPlaylists().prefix(20)).map { PlaylistEntity(id: $0.id, name: $0.name) }
    }
}

struct PlayPlaylistIntent: AppIntent {
    static var title: LocalizedStringResource = "播放歌单"
    static var openAppWhenRun = true
    @Parameter(title: "歌单") var playlist: PlaylistEntity
    static var parameterSummary: some ParameterSummary { Summary("播放 \(\.$playlist)") }
    @MainActor func perform() async throws -> some IntentResult & ProvidesDialog {
        let n = try await YMActions.playPlaylist(named: playlist.name); return .result(dialog: "开始播放「\(n)」")
    }
}

struct SleepTimerIntent: AppIntent {
    static var title: LocalizedStringResource = "睡眠定时"
    @Parameter(title: "分钟", default: 30, inclusiveRange: (0, 180)) var minutes: Int
    static var parameterSummary: some ParameterSummary { Summary("\(\.$minutes) 分钟后停止播放") }
    @MainActor func perform() async throws -> some IntentResult & ProvidesDialog {
        PlayerStore.shared.setSleep(minutes: minutes > 0 ? minutes : nil)
        return .result(dialog: minutes > 0 ? "\(minutes) 分钟后停止播放" : "已关闭睡眠定时")
    }
}

struct YimengShortcuts: AppShortcutsProvider {
    static var shortcutTileColor: ShortcutTileColor = .purple
    static var appShortcuts: [AppShortcut] {
        AppShortcut(intent: PlayDailyIntent(), phrases: ["用\(.applicationName)播放每日推荐", "\(.applicationName)每日推荐"], shortTitle: "每日推荐", systemImageName: "calendar")
        AppShortcut(intent: PlayFMIntent(), phrases: ["用\(.applicationName)播放私人FM", "\(.applicationName)私人FM"], shortTitle: "私人 FM", systemImageName: "dot.radiowaves.left.and.right")
        AppShortcut(intent: PlayPlaylistIntent(), phrases: ["用\(.applicationName)播放\(\.$playlist)", "\(.applicationName)播放歌单"], shortTitle: "播放歌单", systemImageName: "music.note.list")
        AppShortcut(intent: SleepTimerIntent(), phrases: ["\(.applicationName)睡眠定时", "用\(.applicationName)定时停止"], shortTitle: "睡眠定时", systemImageName: "moon.zzz")
    }
}
