import SwiftUI
import AVFoundation

// MARK: - Models (D50–D54)

struct CoreSourceSwitch: Codable, Sendable { var enabled: Bool; var priority: [String]; var platforms: [String]? }
struct CoreAlternative: Codable, Hashable, Sendable, Identifiable {
    var trackId: String; var origin: String?; var title: String?; var artistName: String?; var durationMs: Int?
    var playable: String?; var score: Double?; var method: String?
    var id: String { trackId }
}
struct CorePlaybackPrefs: Codable, Sendable { var replaygain: String?; var preampDb: Double?; var preventClipping: Bool?; var gapless: Bool? }
struct CoreSecurityAlert: Codable, Hashable, Sendable, Identifiable {
    var id: Int; var at: String?; var kind: String?; var severity: String?; var deviceId: String?; var ip: String?; var read: Bool?
    var title: String {
        switch kind ?? "" {
        case "device_paired": "新设备已配对"; case "new_ip_login": "新 IP 登录"; case "login_failed": "多次登录失败"; case "lockout": "登录已被锁定"
        default: kind ?? "安全提醒"
        }
    }
}
struct CoreCloneResult: Codable, Sendable { var playlist: CorePlaylist?; var tracks: Int?; var linkedLocal: Int?; var skipped: Int?; var unavailable: Int? }

// MARK: - Endpoints

extension CoreClient {
    func sourceSwitch() async throws -> CoreSourceSwitch { try await get("/api/v1/me/source-switch") }
    func setSourceSwitch(_ s: CoreSourceSwitch) async throws -> CoreSourceSwitch {
        struct B: Encodable { let enabled: Bool; let priority: [String] }
        return try await send("PUT", "/api/v1/me/source-switch", body: B(enabled: s.enabled, priority: s.priority))
    }
    func alternatives(_ id: String) async throws -> [CoreAlternative] {
        struct R: Codable { var items: [CoreAlternative]? }
        let r: R = try await get("/api/v1/tracks/\(Self.path(id))/alternatives"); return r.items ?? []
    }
    func playbackPrefs() async throws -> CorePlaybackPrefs { try await get("/api/v1/me/playback") }
    func setPlaybackPrefs(_ p: CorePlaybackPrefs) async throws -> CorePlaybackPrefs { try await send("PUT", "/api/v1/me/playback", body: p) }
    func securityAlerts() async throws -> (items: [CoreSecurityAlert], unread: Int) {
        struct R: Codable { var items: [CoreSecurityAlert]?; var unread: Int? }
        let r: R = try await get("/api/v1/security/alerts", query: ["limit": "100"]); return (r.items ?? [], r.unread ?? 0)
    }
    func markAlertsRead(_ ids: [Int]? = nil) async throws {
        struct B: Encodable { let ids: [Int]? }
        try await fire("POST", "/api/v1/security/alerts/read", body: B(ids: ids))
    }
    func myDeviceId() async -> String? {
        struct D: Codable { var id: String }
        return (try? await get("/api/v1/devices/me") as D)?.id
    }
    /// D52: arm (minutes or end_of_track) or clear (empty body) this device's player sleep timer.
    func sleep(player: String, minutes: Int?, endOfTrack: Bool, fadeSeconds: Int) async throws {
        struct B: Encodable { let minutes: Int?; let endOfTrack: Bool?; let fadeSeconds: Int? }
        let b = minutes == nil && !endOfTrack ? B(minutes: 0, endOfTrack: nil, fadeSeconds: nil) : B(minutes: minutes, endOfTrack: endOfTrack ? true : nil, fadeSeconds: fadeSeconds)
        try await fire("POST", "/api/v1/players/\(Self.path(player))/sleep", body: b)
    }
    /// D51: import an online playlist, new (into nil) or merged into one of the caller's playlists.
    func clone(origin: String, playlistId: String, into: String?, name: String?) async throws -> CoreCloneResult {
        struct B: Encodable { let name: String?; let into: String? }
        let raw = playlistId.hasPrefix(origin + ":") ? String(playlistId.dropFirst(origin.count + 1)) : playlistId
        return try await send("POST", "/api/v1/online/\(origin)/playlists/\(Self.path(raw))/clone", body: B(name: name, into: into))
    }
    /// The stream's X-Yimeng-Fallback (origin Core switched to, or "local"); a 1-byte ranged GET.
    func fallbackOrigin(for url: URL) async -> String? {
        var r = URLRequest(url: url); r.setValue("bytes=0-0", forHTTPHeaderField: "Range")
        for (k, v) in CoreState.mediaHeaders(for: url) { r.setValue(v, forHTTPHeaderField: k) }
        guard let (_, resp) = try? await URLSession.shared.data(for: r), let h = resp as? HTTPURLResponse else { return nil }
        return (h.value(forHTTPHeaderField: "X-Yimeng-Fallback")).flatMap { $0.isEmpty ? nil : $0 }
    }
}

// MARK: - Stores

@MainActor
final class CoreAccountStore: ObservableObject {
    static let shared = CoreAccountStore()
    @Published var sourceSwitch: CoreSourceSwitch?
    @Published var alerts: [CoreSecurityAlert] = []
    @Published var unread = 0
    @Published var sleepFade = UserDefaults.standard.object(forKey: "ym.sleep.fade") as? Bool ?? true {
        didSet { UserDefaults.standard.set(sleepFade, forKey: "ym.sleep.fade") }
    }
    var deviceId: String?

    func refreshAll() async {
        guard NavidromeClient.core else { return }
        let c = CoreClient.shared
        if deviceId == nil { deviceId = await c.myDeviceId() }
        sourceSwitch = try? await c.sourceSwitch()
        await refreshAlerts()
        if let p = try? await c.playbackPrefs() { Self.adopt(p) }
    }
    func refreshAlerts() async {
        if let r = try? await CoreClient.shared.securityAlerts() { alerts = r.items; unread = r.unread }
    }
    func markAllRead() async {
        try? await CoreClient.shared.markAlertsRead()
        unread = 0; alerts = alerts.map { var a = $0; a.read = true; return a }
    }
    func saveSwitch(_ s: CoreSourceSwitch) {
        sourceSwitch = s
        Task { if let r = try? await CoreClient.shared.setSourceSwitch(s) { sourceSwitch = r } else { SheetCenter.shared.say("保存失败") } }
    }

    /// /me/playback → the local AudioPrefs keys PlayerStore already reads (volume-only ReplayGain, gapless preroll).
    static func adopt(_ p: CorePlaybackPrefs) {
        let d = UserDefaults.standard
        if let m = p.replaygain { d.set(m == "album" ? 2 : m == "track" ? 1 : 0, forKey: "ym.rg.mode") }
        if let a = p.preampDb { d.set(a, forKey: "ym.rg.preamp") }
        if let c = p.preventClipping { d.set(c, forKey: "ym.rg.clip") }
        if let g = p.gapless { d.set(g, forKey: "ym.gapless") }
        PlayerStore.shared.applyGain()
    }
    /// Local change → PUT (partial). Called from the 音效/播放 settings.
    static func pushPlaybackPrefs() {
        guard NavidromeClient.core else { return }
        let m = AudioPrefs.replayGain
        let p = CorePlaybackPrefs(replaygain: m == 2 ? "album" : m == 1 ? "track" : "off", preampDb: AudioPrefs.preamp, preventClipping: AudioPrefs.clipGuard, gapless: AudioPrefs.gapless)
        Task { _ = try? await CoreClient.shared.setPlaybackPrefs(p) }
    }

    /// Server sleep timer (D52). Local timer stays the source of truth for the UI countdown.
    func armSleep(minutes: Int?, endOfTrack: Bool) {
        guard NavidromeClient.core else { return }
        let fade = sleepFade ? 30 : 0
        Task {
            if deviceId == nil { deviceId = await CoreClient.shared.myDeviceId() }
            guard let id = deviceId else { return }
            try? await CoreClient.shared.sleep(player: id, minutes: minutes, endOfTrack: endOfTrack, fadeSeconds: fade)
        }
    }
    /// Events from CoreEvents.
    func handle(type: String, payload: [String: Any]) {
        let pid = payload["player_id"] as? String
        #if DEBUG
        NSLog("YMEVT %@ mine=%d playing=%d", type, (pid == nil || pid == deviceId) ? 1 : 0, PlayerStore.shared.isPlaying ? 1 : 0)
        #endif
        switch type {
        case "security.alert":
            unread += 1
            let k = CoreSecurityAlert(id: payload["id"] as? Int ?? -1, kind: payload["kind"] as? String).title
            SheetCenter.shared.say("安全提醒：\(k)")
            Task { await refreshAlerts() }
        case "player.sleep.fade" where pid == nil || pid == deviceId:
            if sleepFade { PlayerStore.shared.fadeOut(over: Double((payload["fade_ms"] as? Int) ?? 30000) / 1000) }
        case "player.sleep" where pid == nil || pid == deviceId:
            PlayerStore.shared.sleepFromServer()
        default: break
        }
    }
}

// MARK: - 音源设置 · 跨平台自动换源

struct SourceSwitchView: View {
    @ObservedObject private var store = CoreAccountStore.shared
    @Environment(\.colorScheme) private var scheme
    var body: some View {
        List {
            if let s = store.sourceSwitch {
                Section {
                    Toggle("跨平台自动换源", isOn: Binding(get: { s.enabled }, set: { var n = s; n.enabled = $0; store.saveSwitch(n) }))
                } footer: { Text("歌曲在当前平台无版权或只能试听时，自动改用其他平台的同一首歌（按下方优先级）。") }
                Section {
                    ForEach(order(s), id: \.self) { o in
                        HStack(spacing: 12) {
                            PlatformGlyph(origin: o, color: Theme.lime).frame(width: 22, height: 22)
                            Text(CoreOrigin.label(o))
                            Spacer()
                        }
                    }
                    .onMove { from, to in var n = s; var l = order(s); l.move(fromOffsets: from, toOffset: to); n.priority = l; store.saveSwitch(n) }
                } header: { Text("平台优先级（拖动排序）") }
                .disabled(!s.enabled)
            } else {
                ProgressView().frame(maxWidth: .infinity)
            }
        }
        .environment(\.editMode, .constant(.active))
        .navigationTitle("跨平台换源")
        .task { await store.refreshAll() }
    }
    private func order(_ s: CoreSourceSwitch) -> [String] {
        let all = s.platforms ?? []
        return s.priority.filter(all.contains) + all.filter { !s.priority.contains($0) }
    }
}

// MARK: - 替代版本 (unplayable → pick another platform's copy)

struct AlternativesSheet: View {
    let song: NDSong
    @Environment(\.dismiss) private var dismiss
    @State private var items: [CoreAlternative] = []
    @State private var phase = 0
    var body: some View {
        NavigationStack {
            Group {
                if phase == 0 { ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity) }
                else if items.isEmpty {
                    VStack(spacing: 8) {
                        Image(systemName: "music.note.list").font(.system(size: 30)).opacity(0.5)
                        Text("其他平台也没有这首歌").font(.system(size: 16, weight: .semibold))
                        Text(song.unplayableToast).font(.system(size: 13)).opacity(0.6)
                    }.frame(maxWidth: .infinity, maxHeight: .infinity)
                } else {
                    List(items) { a in
                        Button { play(a) } label: {
                            HStack(spacing: 12) {
                                PlatformGlyph(origin: a.origin ?? "", color: Theme.lime).frame(width: 22, height: 22)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(a.title ?? song.title).font(.system(size: 15)).lineLimit(1)
                                    Text([a.artistName, a.durationMs.map { String(format: "%d:%02d", $0 / 60000, $0 / 1000 % 60) }, a.playable == "preview" ? "试听" : nil]
                                        .compactMap { $0 }.joined(separator: " · ")).font(.system(size: 12)).opacity(0.6)
                                }
                                Spacer()
                                Image(systemName: "play.circle").opacity(a.playable == "unavailable" ? 0.3 : 1)
                            }
                        }.disabled(a.playable == "unavailable")
                    }
                }
            }
            .navigationTitle("换个平台播放").navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("关闭") { dismiss() } } }
        }
        .task {
            items = (try? await CoreClient.shared.alternatives(song.id)) ?? []
            phase = 1
        }
    }
    private func play(_ a: CoreAlternative) {
        var s = NDSong(id: a.trackId, title: a.title ?? song.title, artist: a.artistName ?? song.artist, album: song.album, albumId: song.albumId,
                       coverId: song.coverId, duration: a.durationMs.map { $0 / 1000 } ?? song.duration, track: song.track)
        s.origin = a.origin; s.playable = true; s.preview = a.playable == "preview"; s.isrc = song.isrc
        PlayerStore.shared.play(songs: [s], startAt: 0)
        dismiss()
    }
}

// MARK: - 导入歌单 (D51)

struct CloneSheet: View {
    let origin: String
    let playlistId: String
    let name: String
    @Environment(\.dismiss) private var dismiss
    @State private var mine: [NDPlaylist] = []
    @State private var into: String?
    @State private var busy = false
    var body: some View {
        NavigationStack {
            List {
                Section {
                    Button { into = nil } label: { pick("新建歌单「\(name)」", on: into == nil) }
                }
                Section("导入到已有歌单") {
                    if mine.isEmpty { Text("你还没有自己的歌单").opacity(0.55) }
                    ForEach(mine) { p in Button { into = p.id } label: { pick(p.name, on: into == p.id) } }
                }
            }
            .navigationTitle("导入歌单").navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) { Button(busy ? "导入中…" : "导入") { Task { await run() } }.disabled(busy) }
            }
        }
        .task {
            let all = (try? await CoreClient.shared.playlists(scope: "local")) ?? []
            mine = all.map(CoreClient.nd).filter { $0.isMine && ($0.origin ?? "local") == "local" }
        }
    }
    private func pick(_ t: String, on: Bool) -> some View {
        HStack { Text(t).foregroundStyle(.primary); Spacer(); if on { Image(systemName: "checkmark").foregroundStyle(Theme.lime) } }
    }
    private func run() async {
        busy = true; defer { busy = false }
        do {
            let r = try await CoreClient.shared.clone(origin: origin, playlistId: playlistId, into: into, name: into == nil ? name : nil)
            let n = r.tracks ?? 0, u = r.unavailable ?? 0
            SheetCenter.shared.say((into == nil ? "已新建歌单" : "已导入") + " · \(n) 首" + (u > 0 ? "，\(u) 首暂不可播" : ""))
            await HomeStore.shared.refresh(force: true)
            dismiss()
        } catch let e as CoreAPIError where e.status == 404 {
            SheetCenter.shared.say("目标歌单不存在或已被删除")
        } catch { SheetCenter.shared.say("导入失败") }
    }
}

// MARK: - 安全提醒 (D54)

struct SecurityAlertsView: View {
    @ObservedObject private var store = CoreAccountStore.shared
    var body: some View {
        List {
            if store.alerts.isEmpty {
                VStack(spacing: 8) {
                    Image(systemName: "checkmark.shield").font(.system(size: 30)).foregroundStyle(Theme.lime)
                    Text("一切正常").font(.system(size: 16, weight: .semibold))
                    Text("新设备配对、陌生 IP 登录或多次登录失败时，会在这里提醒你。").font(.system(size: 13)).opacity(0.6).multilineTextAlignment(.center)
                }.frame(maxWidth: .infinity).padding(.vertical, 30).listRowBackground(Color.clear)
            }
            ForEach(store.alerts) { a in
                HStack(alignment: .top, spacing: 12) {
                    Image(systemName: a.severity == "critical" ? "exclamationmark.octagon.fill" : a.severity == "warn" ? "exclamationmark.triangle.fill" : "info.circle.fill")
                        .foregroundStyle(a.severity == "critical" ? .red : a.severity == "warn" ? .orange : .secondary)
                    VStack(alignment: .leading, spacing: 3) {
                        Text(a.title).font(.system(size: 15, weight: a.read == true ? .regular : .semibold))
                        Text([a.ip, Self.when(a.at)].compactMap { $0 }.joined(separator: " · ")).font(.system(size: 12)).opacity(0.6)
                    }
                    Spacer()
                    if a.read != true { Circle().fill(.red).frame(width: 7, height: 7).padding(.top, 6) }
                }
            }
        }
        .navigationTitle("安全提醒")
        .toolbar { if store.unread > 0 { Button("全部已读") { Task { await store.markAllRead() } } } }
        .task { await store.refreshAlerts() }
    }
    static func when(_ s: String?) -> String? {
        guard let s else { return nil }
        let f = ISO8601DateFormatter(); f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        guard let d = f.date(from: s) ?? ISO8601DateFormatter().date(from: s) else { return s }
        let o = DateFormatter(); o.dateFormat = "M月d日 HH:mm"; return o.string(from: d)
    }
}

struct UnreadBadge: View {
    let n: Int
    var body: some View {
        Text(n > 99 ? "99+" : "\(n)").font(.system(size: 10, weight: .bold)).foregroundStyle(.white)
            .padding(.horizontal, 5).frame(minWidth: 16, minHeight: 16).background(Capsule().fill(Color.red))
    }
}
struct DebugID: Identifiable { let id: String }
