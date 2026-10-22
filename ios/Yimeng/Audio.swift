import SwiftUI
import AVFoundation
import AVKit
import MediaPlayer

// MARK: - Preferences

/// Sound preferences (UserDefaults). `ym.maxBitRate` 0 = 原始 (stream.view format=raw, no transcode).
enum AudioPrefs {
    static var maxBitRate: Int { UserDefaults.standard.integer(forKey: "ym.maxBitRate") }
    static var crossfade: Double { UserDefaults.standard.double(forKey: "ym.crossfade") }      // seconds, 0 = off
    static var gapless: Bool { UserDefaults.standard.object(forKey: "ym.gapless") as? Bool ?? true }
    static var replayGain: Int { UserDefaults.standard.integer(forKey: "ym.rg.mode") }         // 0 off · 1 track · 2 album
    static var preamp: Double { UserDefaults.standard.double(forKey: "ym.rg.preamp") }         // dB
    static var clipGuard: Bool { UserDefaults.standard.object(forKey: "ym.rg.clip") as? Bool ?? true }
    @MainActor static var eqOn: Bool { FXStore.shared.s.on }
}

struct ReplayGainInfo: Codable, Hashable {
    var trackGain: Double?; var albumGain: Double?; var trackPeak: Double?; var albumPeak: Double?
}

extension NDSong {
    var isLossless: Bool { ["flac", "alac", "wav", "aiff", "aif", "ape", "wv", "dsf", "dff"].contains((suffix ?? "").lowercased()) }
    var isDolby: Bool {
        let s = (suffix ?? "").lowercased(), c = (contentType ?? "").lowercased()
        return ["ec3", "eac3", "ac3", "ac4", "mlp", "thd"].contains(s) || c.contains("eac3") || c.contains("ac3") || c.contains("ac-4")
    }
    /// Whether the server will transcode this song under the current preference.
    var willTranscode: Bool {
        let br = AudioPrefs.maxBitRate
        guard br > 0 else { return false }
        return isLossless || isDolby || (bitRate ?? 0) > br
    }
    /// Badge such as "FLAC 24/96", "Dolby Atmos", "AAC 256".
    var qualityLabel: String? {
        if willTranscode { return "MP3 \(AudioPrefs.maxBitRate) · 转码" }
        if isDolby { return (channelCount ?? 0) > 6 || (suffix ?? "").lowercased() == "ec3" ? "Dolby Atmos" : "Dolby Digital+" }
        guard let sfx = suffix?.uppercased(), !sfx.isEmpty else { return nil }
        let name = sfx == "M4A" ? ((bitRate ?? 0) > 500 ? "ALAC" : "AAC") : sfx
        if isLossless || name == "ALAC" {
            let bits = bitDepth.map(String.init) ?? "16"
            let khz = samplingRate.map { $0 % 1000 == 0 ? "\($0 / 1000)" : String(format: "%.1f", Double($0) / 1000) } ?? "44.1"
            return "\(name) \(bits)/\(khz)"
        }
        return bitRate.map { "\(name) \($0)" } ?? name
    }
}

// MARK: - Output route

@MainActor
final class AudioRoute: ObservableObject {
    static let shared = AudioRoute()
    @Published var name = "iPhone"
    @Published var portType = ""
    @Published var sampleRate: Double = 0
    @Published var channels = 2
    private var obs: NSObjectProtocol?

    init() {
        refresh()
        obs = NotificationCenter.default.addObserver(forName: AVAudioSession.routeChangeNotification, object: nil, queue: .main) { _ in
            MainActor.assumeIsolated { AudioRoute.shared.refresh() }
        }
    }

    func refresh() {
        let s = AVAudioSession.sharedInstance()
        let out = s.currentRoute.outputs.first
        name = out?.portName ?? "iPhone"
        portType = out?.portType.rawValue ?? ""
        sampleRate = s.sampleRate
        channels = s.outputNumberOfChannels
    }

    /// Long-form playback session that allows multichannel / spatial content to pass through
    /// (AirPods spatial, AirPlay, HDMI). Decoding/downmix is left to the system.
    static func configureSession() {
        let s = AVAudioSession.sharedInstance()
        try? s.setCategory(.playback, mode: .default, policy: .longFormAudio, options: [])
        try? s.setSupportsMultichannelContent(true)
        try? s.setActive(true)
    }
}

// MARK: - Quality badge + signal path

struct QualityBadge: View {
    let song: NDSong
    @State private var show = false
    var body: some View {
        if let q = song.qualityLabel {
            Button { show = true } label: {
                HStack(spacing: 4) {
                    if song.isLossless && !song.willTranscode { Circle().fill(Theme.lime).frame(width: 5, height: 5) }
                    Text(q).font(.system(size: 11, weight: .semibold, design: .rounded)).tracking(0.5)
                }
                .padding(.horizontal, 9).frame(height: 22)
                .background(Capsule().strokeBorder(.white.opacity(0.45), lineWidth: 1))
            }
            .buttonStyle(.plain)
            .sheet(isPresented: $show) {
                SignalPathSheet(song: song)
                    .presentationDetents([.height(380)])
                    .presentationBackground(.ultraThinMaterial)
                    .presentationCornerRadius(28)
            }
        }
    }
}

struct SignalPathSheet: View {
    let song: NDSong
    @ObservedObject private var route = AudioRoute.shared
    var body: some View {
        let transcoded = song.willTranscode
        let passthrough = !transcoded && !AudioPrefs.eqOn
        VStack(alignment: .leading, spacing: 14) {
            Text("信号路径").font(.system(size: 20, weight: .bold)).padding(.top, 24)
            step("doc", "音源", [song.suffix?.uppercased(), song.bitDepth.map { "\($0) bit" },
                                  song.samplingRate.map { String(format: "%.1f kHz", Double($0) / 1000) },
                                  song.channelCount.map { "\($0) 声道" }, song.bitRate.map { "\($0) kbps" }].compactMap { $0 }.joined(separator: " · "))
            step("arrow.triangle.2.circlepath", "串流", transcoded ? "服务器转码为 MP3 ≤ \(AudioPrefs.maxBitRate) kbps" : "原始文件（format=raw，未转码）")
            step("waveform", "处理", AudioPrefs.eqOn ? "音效开启 · 实时处理 · 非直通" : (AudioPrefs.replayGain > 0 ? "仅回放增益（音量）" : "无处理"))
            step("hifispeaker", "输出", "\(route.name) · \(Int(route.sampleRate)) Hz · \(route.channels) 声道")
            HStack(spacing: 6) {
                Circle().fill(passthrough ? Theme.lime : Color.orange).frame(width: 7, height: 7)
                Text(passthrough ? "直通：解码交给 iOS，多声道 / 杜比交由系统输出" : "非直通").font(.system(size: 13)).opacity(0.75)
            }.padding(.top, 4)
            Spacer()
        }
        .padding(.horizontal, 24)
        .frame(maxWidth: .infinity, alignment: .leading)
        .onAppear { route.refresh() }
    }
    func step(_ ic: String, _ k: String, _ v: String) -> some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: ic).frame(width: 22)
            Text(k).font(.system(size: 14, weight: .semibold)).frame(width: 36, alignment: .leading)
            Text(v.isEmpty ? "未知" : v).font(.system(size: 14)).opacity(0.8)
        }
    }
}

// MARK: - Volume

/// Volume model. Device: system volume via the (hidden) MPVolumeView slider, synced from hardware buttons by
/// KVO on AVAudioSession.outputVolume. Simulator (no system volume): drives the player's own gain so it audibly works.
@MainActor
final class VolumeController: ObservableObject {
    static let shared = VolumeController()
    @Published private(set) var value: Float
    weak var systemSlider: UISlider?
    private var obs: NSKeyValueObservation?
    #if targetEnvironment(simulator)
    static let usesSystem = false
    #else
    static let usesSystem = true
    #endif

    private init() {
        let s = AVAudioSession.sharedInstance()
        value = Self.usesSystem ? s.outputVolume : PlayerStore.shared.userGain
        if Self.usesSystem {
            obs = s.observe(\.outputVolume, options: [.new]) { [weak self] _, ch in
                guard let v = ch.newValue else { return }
                Task { @MainActor in self?.value = v }
            }
        }
    }

    func set(_ v: Float) {
        let v = max(0, min(1, v))
        value = v
        if Self.usesSystem, let sl = systemSlider {
            sl.value = v
            sl.sendActions(for: .valueChanged)
        } else {
            PlayerStore.shared.userGain = v
        }
    }
}

/// Off-screen MPVolumeView: gives us the system volume slider (and suppresses the volume HUD while visible).
struct HiddenSystemVolume: UIViewRepresentable {
    func makeUIView(context: Context) -> MPVolumeView {
        let v = MPVolumeView(frame: CGRect(x: -200, y: -200, width: 100, height: 30))
        v.alpha = 0.011
        v.isUserInteractionEnabled = false
        DispatchQueue.main.async {
            VolumeController.shared.systemSlider = v.subviews.compactMap { $0 as? UISlider }.first
        }
        return v
    }
    func updateUIView(_ v: MPVolumeView, context: Context) {}
}

/// Thin custom slider: 3 pt track, small dot thumb that grows while dragging.
struct VolumeSlider: View {
    @ObservedObject private var vol = VolumeController.shared
    @State private var dragging = false
    var body: some View {
        GeometryReader { g in
            let w = g.size.width, x = CGFloat(vol.value) * w
            ZStack(alignment: .leading) {
                Capsule().fill(Color.white.opacity(0.25)).frame(height: dragging ? 5 : 3)
                Capsule().fill(Color.white).frame(width: max(0, x), height: dragging ? 5 : 3)
                Circle().fill(.white).frame(width: dragging ? 14 : 10, height: dragging ? 14 : 10)
                    .offset(x: min(max(0, x - (dragging ? 7 : 5)), w - (dragging ? 14 : 10)))
            }
            .frame(maxHeight: .infinity)
            .contentShape(Rectangle())
            .gesture(DragGesture(minimumDistance: 0)
                .onChanged { v in
                    if !dragging { withAnimation(.spring(response: 0.25, dampingFraction: 0.8)) { dragging = true } }
                    vol.set(Float(v.location.x / max(w, 1)))
                }
                .onEnded { _ in withAnimation(.spring(response: 0.3, dampingFraction: 0.8)) { dragging = false } })
        }
        .frame(height: 24)
        .background(HiddenSystemVolume().frame(width: 1, height: 1))
        .accessibilityElement()
        .accessibilityLabel("音量")
        .accessibilityValue("\(Int(vol.value * 100))%")
        .accessibilityAdjustableAction { d in vol.set(vol.value + (d == .increment ? 0.0625 : -0.0625)) }
    }
}

/// AirPlay / route button: our own SF Symbol, with a near-invisible AVRoutePickerView on top to open the system picker.
struct RouteButton: View {
    @ObservedObject private var route = AudioRoute.shared
    var body: some View {
        let symbol = route.portType.contains("Bluetooth") ? "airpodspro" : route.portType.contains("AirPlay") ? "airplayaudio" : "airplayaudio"
        Image(systemName: symbol)
            .font(.system(size: 14, weight: .medium))
            .foregroundStyle(route.portType.contains("AirPlay") || route.portType.contains("Bluetooth") ? Theme.lime : .white.opacity(0.75))
            .frame(width: 28, height: 28)
            .overlay(RoutePicker().opacity(0.02))
            .contentShape(Rectangle())
    }
}

struct RoutePicker: UIViewRepresentable {
    func makeUIView(context: Context) -> AVRoutePickerView {
        let v = AVRoutePickerView()
        v.tintColor = .white; v.activeTintColor = .white
        v.prioritizesVideoDevices = false
        return v
    }
    func updateUIView(_ v: AVRoutePickerView, context: Context) {}
}

struct VolumeRow: View {
    @ObservedObject private var route = AudioRoute.shared
    var body: some View {
        VStack(spacing: 6) {
            HStack(spacing: 10) {
                Image(systemName: "speaker.fill").font(.system(size: 11)).opacity(0.6).frame(width: 14)
                VolumeSlider()
                Image(systemName: "speaker.wave.3.fill").font(.system(size: 11)).opacity(0.6).frame(width: 18)
                RouteButton()
            }
            HStack(spacing: 4) {
                Image(systemName: route.portType.contains("Bluetooth") ? "airpodspro" : route.portType.contains("AirPlay") ? "airplayaudio" : "iphone")
                Text(route.name)
            }.font(.system(size: 11)).opacity(0.6)
        }
    }
}

// MARK: - Global sheets (queue, add-to-playlist, sound settings)

@MainActor
final class SheetCenter: ObservableObject {
    static let shared = SheetCenter()
    @Published var queue = false
    @Published var sound = false
    @Published var addToPlaylist: NDSong?
    @Published var alternatives: NDSong?
    /// Greyed track tapped: on Core offer other platforms' copies (D50), elsewhere just explain.
    /// Unplayable (D58 `unavailable`): open alternatives when Core has any, otherwise an empty-state toast.
    func unplayable(_ s: NDSong) {
        guard NavidromeClient.core else { say(s.unplayableToast); return }
        Task { @MainActor in
            let alts = ((try? await CoreClient.shared.alternatives(s.id)) ?? []).filter { $0.playable != "unavailable" }
            if alts.isEmpty { self.say(s.unplayableToast + "，其他平台也没找到") } else { self.alternatives = s }
        }
    }
    @Published var toast: String?
    func say(_ s: String) {
        withAnimation(.snappy) { toast = s }
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.6) { withAnimation(.snappy) { if self.toast == s { self.toast = nil } } }
    }
}

struct GlobalSheets: ViewModifier {
    @ObservedObject private var c = SheetCenter.shared
    func body(content: Content) -> some View {
        content
            .sheet(isPresented: $c.queue) {
                QueueSheet().presentationDetents([.medium, .large]).presentationCornerRadius(28)
                    .presentationBackground(Color(white: 0.085)).environment(\.colorScheme, .dark)
            }
            .sheet(isPresented: $c.sound) { NavigationStack { SoundSettingsView() } .presentationCornerRadius(28) }
            .sheet(item: $c.addToPlaylist) { s in AddToPlaylistSheet(song: s).presentationDetents([.medium, .large]).presentationCornerRadius(28) }
            .sheet(item: $c.alternatives) { s in AlternativesSheet(song: s).presentationDetents([.medium, .large]).presentationCornerRadius(28) }
            .overlay(alignment: .top) {
                if let t = c.toast {
                    Text(t).font(.system(size: 14, weight: .semibold)).foregroundStyle(.white)
                        .padding(.horizontal, 16).frame(height: 36).background(Capsule().fill(Color(white: 0.12)))
                        .padding(.top, 8).transition(.move(edge: .top).combined(with: .opacity))
                }
            }
    }
}

/// Context menu for any song row: play next / add to queue / add to playlist / favourite.
struct SongMenu: ViewModifier {
    let song: NDSong
    func body(content: Content) -> some View {
        content.contextMenu {
            Button { PlayerStore.shared.playNext(song); SheetCenter.shared.say("将在下一首播放") } label: { Label("下一首播放", systemImage: "text.line.first.and.arrowtriangle.forward") }
            Button { PlayerStore.shared.enqueue(song); SheetCenter.shared.say("已加入播放队列") } label: { Label("加入播放队列", systemImage: "text.badge.plus") }
            Button { SheetCenter.shared.addToPlaylist = song } label: { Label("添加到歌单…", systemImage: "music.note.list") }
            Button { Nav.album(of: song) } label: { Label("前往专辑", systemImage: "square.stack") }.disabled(song.albumId == nil)
            Button { Nav.artist(of: song) } label: { Label("前往歌手", systemImage: "music.mic") }
            Button { StarStore.shared.toggle(song); Haptics.tap() } label: {
                Label(StarStore.shared.isStarred(song: song.id) ? "取消收藏" : "收藏", systemImage: "heart")
            }
        } preview: {
            SongMenuPreview(song: song)
        }
    }
}
struct SongMenuPreview: View {
    let song: NDSong
    var body: some View {
        HStack(spacing: 12) {
            CoverImage(url: LibraryStore.shared.coverArtURL(id: song.coverId ?? song.albumId, size: 300), points: 64)
                .frame(width: 64, height: 64).clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
            VStack(alignment: .leading, spacing: 3) {
                Text(song.title).font(.system(size: 16, weight: .semibold)).lineLimit(2)
                Text([song.artist, song.album].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ")).font(.system(size: 13)).foregroundStyle(.secondary).lineLimit(1)
                HStack(spacing: 6) { SourceBadge(origin: song.origin); if song.preview == true { PreviewCapsule() }; Text(song.durationLabel).font(.system(size: 12)).foregroundStyle(.secondary) }
            }
            Spacer(minLength: 0)
        }.padding(14).frame(width: 300).background(Color(uiColor: .systemBackground))
    }
}
extension View { func songMenu(_ s: NDSong) -> some View { modifier(SongMenu(song: s)) } }

// MARK: - Queue sheet

/// Minimal queue: title + count, quiet play-mode + clear, a 队列/历史 text switch; swipe left to remove.
struct QueueSheet: View {
    @ObservedObject private var player = PlayerStore.shared
    @State private var history = false
    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .firstTextBaseline, spacing: 14) {
                tab("播放列表", count: player.queue.count, on: !history) { history = false }
                tab("历史", count: player.history.count, on: history) { history = true }
                Spacer()
                if !history {
                    Button { withAnimation(.spring(response: 0.3)) { player.cyclePlayMode() } } label: {
                        Image(systemName: modeIcon).font(.system(size: 14))
                    }.accessibilityLabel(player.playMode.rawValue)
                }
                Button(history ? "清空" : "清空") { history ? player.clearHistory() : player.clearUpcoming() }
                    .font(.system(size: 13))
            }
            .buttonStyle(.plain).foregroundStyle(.secondary)
            .padding(.horizontal, 22).padding(.top, 26).padding(.bottom, 10)
            List {
                if history {
                    ForEach(Array(player.history.enumerated()), id: \.offset) { i, s in
                        row(s, playing: player.song?.id == s.id)
                            .onTapGesture { player.play(songs: Array(player.history[i...])) }
                            .swipeActions { Button(role: .destructive) { player.removeHistory(at: [i]) } label: { Label("移除", systemImage: "trash") } }
                    }
                } else {
                    if let s = player.song { row(s, playing: true) }
                    ForEach(Array(player.upcoming.enumerated()), id: \.offset) { i, s in
                        row(s, playing: false)
                            .onTapGesture { player.jump(toUpcoming: i) }
                            .swipeActions { Button(role: .destructive) { player.removeUpcoming(at: [i]) } label: { Label("移除", systemImage: "trash") } }
                    }
                }
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
        }
    }
    private func tab(_ t: String, count: Int, on: Bool, _ a: @escaping () -> Void) -> some View {
        Button(action: a) {
            HStack(alignment: .firstTextBaseline, spacing: 4) {
                Text(t).font(.system(size: on ? 18 : 15, weight: on ? .bold : .regular)).foregroundStyle(on ? Color.primary : Color.secondary)
                Text("\(count)").font(.system(size: 12).monospacedDigit()).foregroundStyle(.secondary)
            }
        }
    }
    private var modeIcon: String {
        switch player.playMode { case .loop: "repeat"; case .one: "repeat.1"; case .shuffle: "shuffle"; case .order: "arrow.right" }
    }
    func row(_ s: NDSong, playing: Bool) -> some View {
        HStack(spacing: 12) {
            CoverImage(url: LibraryStore.shared.coverArtURL(id: s.coverId ?? s.albumId, size: 120), points: 40)
                .frame(width: 40, height: 40).clipShape(RoundedRectangle(cornerRadius: 6, style: .continuous))
            VStack(alignment: .leading, spacing: 2) {
                Text(s.title).font(.system(size: 15, weight: playing ? .semibold : .regular)).lineLimit(1).truncationMode(.middle)
                Text(s.artist).font(.system(size: 12)).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer()
            if playing { Image(systemName: "waveform").font(.system(size: 12)).foregroundStyle(Theme.lime) }
        }
        .contentShape(Rectangle())
        .listRowBackground(playing ? Color.white.opacity(0.05) : Color.clear)
        .listRowSeparator(.hidden)
    }
}

// MARK: - Add to playlist

struct AddToPlaylistSheet: View {
    let song: NDSong
    @Environment(\.dismiss) var dismiss
    @State private var lists: [NDPlaylist] = []
    @State private var loaded = false
    @State private var newName = ""
    @State private var busy = false
    var body: some View {
        NavigationStack {
            List {
                Section {
                    HStack {
                        TextField("新建歌单名称", text: $newName)
                        Button("创建并添加") { Task { await create() } }
                            .disabled(newName.trimmingCharacters(in: .whitespaces).isEmpty || busy)
                    }
                }
                Section("添加到已有歌单") {
                    if !loaded { ProgressView() }
                    else if lists.isEmpty { Text("还没有歌单").foregroundStyle(.secondary) }
                    ForEach(lists) { pl in
                        Button { Task { await add(pl) } } label: {
                            HStack {
                                Text(pl.name)
                                Spacer()
                                Text("\(pl.songCount ?? 0) 首").foregroundStyle(.secondary)
                            }
                        }.disabled(busy)
                    }
                }
            }
            .navigationTitle("添加「\(song.title)」").navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .topBarTrailing) { Button("取消") { dismiss() } } }
        }
        .task { await PlaylistEvents.shared.load(); lists = PlaylistEvents.shared.lists ?? []; loaded = true }
    }
    private func add(_ pl: NDPlaylist) async {
        busy = true; defer { busy = false }
        do {
            try await NavidromeClient.shared.addToPlaylist(pl.id, songIds: [song.id])
            var p = pl; p.songCount = (p.songCount ?? 0) + 1; PlaylistEvents.updated(p)
            SheetCenter.shared.say("已添加到「\(pl.name)」"); dismiss()
        }
        catch { SheetCenter.shared.say("添加失败") }
    }
    private func create() async {
        busy = true; defer { busy = false }
        let name = newName.trimmingCharacters(in: .whitespaces)
        do {
            if let p = try await NavidromeClient.shared.createPlaylist(name: name, songIds: [song.id]) { PlaylistEvents.updated(p) }
            SheetCenter.shared.say("已创建「\(name)」"); dismiss()
        }
        catch { SheetCenter.shared.say("创建失败") }
    }
}

// MARK: - 音效设置

struct SoundSettingsView: View {
    @AppStorage("ym.maxBitRate") private var maxBitRate = 0
    @AppStorage("ym.crossfade") private var crossfade = 0.0
    @AppStorage("ym.gapless") private var gapless = true
    @AppStorage("ym.rg.mode") private var rgMode = 0
    @AppStorage("ym.rg.preamp") private var preamp = 0.0
    @AppStorage("ym.rg.clip") private var clipGuard = true
    @Environment(\.dismiss) var dismiss

    var body: some View {
        form
            .onChange(of: rgMode) { CoreAccountStore.pushPlaybackPrefs(); PlayerStore.shared.applyGain() }
            .onChange(of: preamp) { CoreAccountStore.pushPlaybackPrefs(); PlayerStore.shared.applyGain() }
            .onChange(of: clipGuard) { CoreAccountStore.pushPlaybackPrefs(); PlayerStore.shared.applyGain() }
            .onChange(of: gapless) { CoreAccountStore.pushPlaybackPrefs() }
    }
    private var form: some View {
        Form {
            Section {
                EffectsPanel()
            } footer: {
                Text("关闭音效 = 原始直通（AVPlayer，FLAC / 杜比不经处理）；开启 = 边下边播 + 实时 12 段 EQ + 峰值限制。切换无需重启播放。")
            }
            Section {
                Picker("串流音质", selection: $maxBitRate) {
                    Text("原始 · 无损直通").tag(0)
                    Text("320 kbps").tag(320); Text("192 kbps").tag(192); Text("128 kbps").tag(128)
                }
            } header: { Text("音质") } footer: {
                Text("「原始」请求未转码文件（FLAC / ALAC / 杜比 E-AC-3），由 iOS 直接解码；多声道与杜比全景声交给 AirPods、AirPlay 或 HDMI 输出。")
            }
            Section {
                Toggle("无缝播放（预加载下一首）", isOn: $gapless)
                VStack(alignment: .leading) {
                    HStack { Text("淡入淡出"); Spacer(); Text(crossfade == 0 ? "关" : "\(Int(crossfade)) 秒").foregroundStyle(.secondary) }
                    Slider(value: $crossfade, in: 0...12, step: 1)
                }
            } header: { Text("过渡") } footer: { Text("无缝播放在淡入淡出为「关」时生效：下一首提前排入播放队列，首尾零间隙。") }
            Section { NavigationLink("离线缓存") { OfflineSettingsView() } }
            Section {
                Picker("回放增益", selection: $rgMode) { Text("关").tag(0); Text("单曲").tag(1); Text("专辑").tag(2) }
                    .pickerStyle(.segmented)
                if rgMode > 0 {
                    VStack(alignment: .leading) {
                        HStack { Text("预增益"); Spacer(); Text(String(format: "%+.0f dB", preamp)).foregroundStyle(.secondary) }
                        Slider(value: $preamp, in: -12...6, step: 1)
                    }
                    Toggle("防削波（按峰值限制）", isOn: $clipGuard)
                }
            } header: { Text("音量标准化") } footer: {
                Text("使用服务器提供的 ReplayGain 标签调整音量，不改变音频数据，不影响直通。")
            }
        }
        .navigationTitle("音效设置")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { ToolbarItem(placement: .topBarTrailing) { Button("完成") { dismiss() } } }
        .onChange(of: rgMode) { _, _ in PlayerStore.shared.applyGain() }
        .onChange(of: preamp) { _, _ in PlayerStore.shared.applyGain() }
        .onChange(of: clipGuard) { _, _ in PlayerStore.shared.applyGain() }
    }
}
