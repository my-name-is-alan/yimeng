import SwiftUI

/// 我的: profile, 音源设置, 偏好设置, 系统设置 — grouped doodle cards.
struct SettingsView: View {
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @ObservedObject private var sources = SourceStore.shared
    @ObservedObject private var library = LibraryStore.shared
    @AppStorage("ym.nickname") private var nickname = ""
    @AppStorage("ym.maxBitRate") private var maxBitRate = 0
    @AppStorage("ym.appearance") private var appearance = 0
    @AppStorage("ym.lyricTrans") private var lyricTrans = true
    @AppStorage("ym.liveActivity") private var liveActivity = true
    @AppStorage("ym.haptics") private var haptics = true
    @AppStorage("ym.hapticBeat") private var hapticBeat = false
    @State private var showSwitch = false
    @State private var showReport = false
    @State private var showLastfm = false
    @State private var showAlerts = false
    @State private var showProfile = false
    @ObservedObject private var coreMe = CoreProfileStore.shared
    @ObservedObject private var acct = CoreAccountStore.shared
    @State private var confirmLogout = false
    @State private var showSources = false
    @State private var editingName = false
    @State private var cacheBytes: Int64 = 0
    @State private var showReplay = false
    @State private var showFonts = false
    @State private var showLicenses = false
    @State private var showOffline = false

    var body: some View {
        ScrollView(showsIndicators: false) {
            VStack(alignment: .leading, spacing: 0) {
                Text("我的").font(.system(size: 28, weight: .bold)).padding(.top, 16).motionIn(0)
                profile.padding(.top, 16).motionIn(0.05)

                group("音源设置", i: 1) {
                    Button { showSources = true } label: {
                        row("antenna.radiowaves.left.and.right", "音乐来源",
                            sources.active.title, chevron: true)
                    }.buttonStyle(.plain)
                    divider
                    row("server.rack", NavidromeClient.emby ? library.embyProduct : NavidromeClient.core ? (library.coreServerName.isEmpty ? "译梦服务端" : library.coreServerName) : "Navidrome", library.connected ? library.host.replacingOccurrences(of: "https://", with: "") : (library.status.isEmpty || library.status == "待连接译梦服务端" ? "未连接" : library.status))
                    if NavidromeClient.core && library.connected {
                        row("person.badge.key", "账号", library.user.isEmpty ? "设备配对" : library.user)
                        divider
                        Button { showReport = true } label: { row("chart.bar.xaxis", "听歌报告", "本月 · 今年", chevron: true) }.buttonStyle(.plain)
                        divider
                        Button { showLastfm = true } label: { row("dot.radiowaves.left.and.right", "Last.fm", "", chevron: true) }.buttonStyle(.plain)
                        divider
                        Button { showSwitch = true } label: { row("arrow.triangle.swap", "跨平台换源", acct.sourceSwitch.map { $0.enabled ? "已开启" : "关闭" } ?? "", chevron: true) }.buttonStyle(.plain)
                        divider
                        Button { showAlerts = true } label: {
                            HStack(spacing: 0) {
                                row("shield.lefthalf.filled", "安全提醒", acct.unread > 0 ? "" : "无未读", chevron: true)
                                    .overlay(alignment: .trailing) { if acct.unread > 0 { UnreadBadge(n: acct.unread).padding(.trailing, 22) } }
                            }
                        }.buttonStyle(.plain)
                        divider
                        Button { confirmLogout = true } label: { row("rectangle.portrait.and.arrow.right", "退出登录", "") }.buttonStyle(.plain)
                            .confirmationDialog("退出译梦服务端账号？", isPresented: $confirmLogout, titleVisibility: .visible) {
                                Button("退出登录", role: .destructive) { Task { await library.logoutCore() } }
                            } message: { Text("本机上该账号的播放队列和缓存会被清除。") }
                    }
                    divider
                    row("square.stack", "曲库", [library.albumTotal.map { "\($0) 张专辑" }, library.songTotal.map { "\($0) 首" }].compactMap { $0 }.joined(separator: " · "))
                }

                group("偏好设置", i: 2) {
                    pickerRow("waveform", "串流音质", selection: $maxBitRate,
                              options: [(0, "原始 · 无损"), (320, "320k"), (192, "192k"), (128, "128k")])
                    divider
                    Button { SheetCenter.shared.sound = true } label: {
                        row("slider.horizontal.3", "音效设置", "过渡 · 回放增益 · 均衡器", chevron: true)
                    }.buttonStyle(.plain)
                    divider
                    pickerRow("circle.lefthalf.filled", "外观", selection: $appearance,
                              options: [(0, "跟随系统"), (1, "浅色"), (2, "深色")])
                    divider
                    toggleRow("character.bubble", "歌词翻译", $lyricTrans)
                    divider
                    toggleRow("hand.tap", "触感反馈", $haptics)
                    if haptics { divider; toggleRow("waveform.path", "随节拍轻震", $hapticBeat).onChange(of: hapticBeat) { Haptics.syncBeatPulse() } }
                    divider
                    HStack(spacing: 14) {
                        icon("scribble.variable")
                        Text("涂鸦强度").font(.system(size: 16))
                        Slider(value: $state.intensity, in: 0...1).tint(Theme.accent(scheme))
                    }.padding(.vertical, 12)
                }

                group("系统设置", i: 3) {
                    HStack {
                        row("internaldrive", "缓存", ByteCountFormatter.string(fromByteCount: cacheBytes, countStyle: .file))
                        Button("清除") { Cache.clear(); LibraryCache.shared.clear(); cacheBytes = Cache.size() }
                            .buttonStyle(YMPillButtonStyle())
                    }
                    divider
                    Button { showOffline = true } label: {
                        row("arrow.down.circle", "离线缓存", "\(OfflineCache.shared.entries.count) 首 · " + ByteCountFormatter.string(fromByteCount: OfflineCache.shared.usedBytes, countStyle: .file), chevron: true)
                    }.buttonStyle(.plain)
                    divider
                    #if os(iOS)
                    toggleRow("dot.radiowaves.left.and.right", "灵动岛 / 实时活动", $liveActivity)
                    divider
                    #endif
                    Button { showFonts = true } label: { row("textformat", "歌词字体", LyricFontStore.options.first { $0.id == LyricFontStore.shared.selected }?.name ?? "系统字体", chevron: true) }.buttonStyle(.plain)
                    divider
                    Button { FirstRun.reset(); showReplay = true } label: {
                        row("arrow.counterclockwise", "重看启动引导", "下次冷启动生效", chevron: true)
                    }.buttonStyle(.plain)
                    divider
                    Button { showLicenses = true } label: { row("doc.text", "开源许可", "字体 · SIL OFL 1.1", chevron: true) }.buttonStyle(.plain)
                    divider
                    row("info.circle", "关于译梦", "v\(Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "") (\(Bundle.main.infoDictionary?["CFBundleVersion"] as? String ?? ""))")
                }
                Color.clear.frame(height: 190)
            }
            .padding(.horizontal, Theme.margin)
        }
        .background(YMBackdrop())
        .sheet(isPresented: $showSources) { SourcesView(embedded: true) }
        .sheet(isPresented: $showFonts) { LyricFontPicker().presentationDetents([.medium, .large]) }
        .sheet(isPresented: $showLicenses) { NavigationStack { LicensesView() } }
        .sheet(isPresented: $showOffline) { NavigationStack { OfflineSettingsView() } }
        #if DEBUG
        .task {
            let a = ProcessInfo.processInfo.arguments
            if let i = a.firstIndex(of: "-me"), i + 1 < a.count { try? await Task.sleep(for: .seconds(4)); if a[i + 1] == "switch" { showSwitch = true } else if a[i + 1] == "report" { showReport = true } else if a[i + 1] == "lastfm" { showLastfm = true } else if a[i + 1] == "alerts" { showAlerts = true } else if a[i + 1] == "profile" { showProfile = true } else if a[i + 1] == "offline" { showOffline = true } else if a[i + 1] == "fonts" { showFonts = true } else if a[i + 1] == "sound" { SheetCenter.shared.sound = true } else if a[i + 1] == "licenses" { showLicenses = true } }
            #if os(iOS)
            if let i = a.firstIndex(of: "-uploadAvatarFrom"), i + 1 < a.count, let img = UIImage(contentsOfFile: a[i + 1]) { try? await Task.sleep(for: .seconds(3)); await ProfileView.upload(img) }
            #endif
            if let i = a.firstIndex(of: "-setNick"), i + 1 < a.count { try? await Task.sleep(for: .seconds(3)); try? await CoreProfileStore.shared.rename(a[i + 1]) }
        }
        #endif
        .ymFullScreenCover(isPresented: $showReport) { ListeningReportView() }
        .sheet(isPresented: $showLastfm) { NavigationStack { LastfmView() } }
        .sheet(isPresented: $showSwitch) { NavigationStack { SourceSwitchView() } }
        .sheet(isPresented: $showAlerts) { NavigationStack { SecurityAlertsView() } }
        .sheet(isPresented: $showProfile) { NavigationStack { ProfileView() } }
        .alert("已重置", isPresented: $showReplay) { Button("好", role: .cancel) {} } message: { Text("下次冷启动会从启动动画开始。") }
        .alert("昵称", isPresented: $editingName) {
            TextField("昵称", text: $nickname)
            Button("好", role: .cancel) {}
        }
        .task { cacheBytes = Cache.size() }
    }

    var profile: some View {
        HStack(spacing: 16) {
            AvatarView(size: 72).onTapGesture { if NavidromeClient.core { showProfile = true } }
            VStack(alignment: .leading, spacing: 4) {
                Button { if NavidromeClient.core { showProfile = true } else { editingName = true } } label: {
                    HStack(spacing: 6) {
                        Text(NavidromeClient.core ? (coreMe.displayName ?? library.user) : !nickname.isEmpty ? nickname : (NavidromeClient.emby && !library.embyUserName.isEmpty ? library.embyUserName : "设置昵称")).font(.system(size: 22, weight: .bold))
                        YMIcon(glyph: NavidromeClient.core ? .chevron : .more, size: 12, weight: 1.5).opacity(0.6)
                    }
                }.buttonStyle(.plain)
                Text(library.connected ? "\(NavidromeClient.emby ? "\(library.embyProduct) · " + (library.embyUserName.isEmpty ? library.user : library.embyUserName) : library.user) @ \(library.host.replacingOccurrences(of: "https://", with: ""))" : "尚未连接音乐来源")
                    .font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme)).lineLimit(1)
            }
            Spacer()
        }
        .foregroundStyle(Theme.fg(scheme))
        .padding(18)
        .background(card)
    }

    var card: some View {
        RoundedRectangle(cornerRadius: 20, style: .continuous).fill(Theme.fill(scheme))
            .overlay(RoundedRectangle(cornerRadius: 20, style: .continuous)
                .strokeBorder(Theme.fg(scheme).opacity(0.08), style: StrokeStyle(lineWidth: 1, dash: [6, 4])))
    }

    var divider: some View { Rectangle().fill(Theme.divider(scheme)).frame(height: 0.5) }

    func group<C: View>(_ title: String, i: Int, @ViewBuilder _ content: () -> C) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title).font(.system(size: 13, weight: .semibold)).foregroundStyle(Theme.secondary(scheme)).padding(.leading, 4)
            VStack(spacing: 0) { content() }
                .padding(.horizontal, 16).padding(.vertical, 4)
                .background(card)
        }
        .foregroundStyle(Theme.fg(scheme))
        .padding(.top, 24)
        .motionIn(stagger(i, base: 0.08))
    }

    func icon(_ s: String) -> some View {
        YMLeadingIcon(name: s)
    }

    func row(_ ic: String, _ title: String, _ detail: String, chevron: Bool = false) -> some View {
        YMRow(icon: ic, title, detail, chevron: chevron)
    }

    func toggleRow(_ ic: String, _ title: String, _ b: Binding<Bool>) -> some View {
        YMToggleRow(icon: ic, title, isOn: b)
    }

    func pickerRow(_ ic: String, _ title: String, selection: Binding<Int>, options: [(Int, String)]) -> some View {
        YMPickerRow(icon: ic, title, selection: selection, options: options)
    }
}

enum Cache {
    static var dir: URL { FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0] }
    static func size() -> Int64 {
        let e = FileManager.default.enumerator(at: dir, includingPropertiesForKeys: [.fileSizeKey])
        var total: Int64 = Int64(URLCache.shared.currentDiskUsage)
        while let u = e?.nextObject() as? URL { total += Int64((try? u.resourceValues(forKeys: [.fileSizeKey]).fileSize) ?? 0) }
        return total
    }
    static func clear() {
        URLCache.shared.removeAllCachedResponses()
        if let items = try? FileManager.default.contentsOfDirectory(at: dir, includingPropertiesForKeys: nil) {
            for i in items { try? FileManager.default.removeItem(at: i) }
        }
        CoverCache.shared.clear()
    }
}
