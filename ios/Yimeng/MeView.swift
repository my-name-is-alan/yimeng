import SwiftUI
import PhotosUI

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
    @State private var pick: PhotosPickerItem?
    @State private var showSources = false
    @State private var editingName = false
    @State private var cacheBytes: Int64 = 0
    @State private var showReplay = false

    var body: some View {
        ScrollView(showsIndicators: false) {
            VStack(alignment: .leading, spacing: 0) {
                Text("我的").font(.system(size: 28, weight: .bold)).padding(.top, 16).motionIn(0)
                profile.padding(.top, 16).motionIn(0.05)

                group("音源设置", i: 1) {
                    Button { showSources = true } label: {
                        row("antenna.radiowaves.left.and.right", "音乐来源",
                            sources.configs.filter(\.connected).map(\.kind.title).joined(separator: " · "), chevron: true)
                    }.buttonStyle(.plain)
                    divider
                    row("server.rack", "Navidrome", library.connected ? library.host.replacingOccurrences(of: "https://", with: "") : "未连接")
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
                            .font(.system(size: 14, weight: .semibold)).foregroundStyle(Theme.fg(scheme))
                            .padding(.horizontal, 14).frame(height: 32).background(Capsule().fill(Theme.fill(scheme)))
                            .buttonStyle(.plain)
                    }
                    divider
                    toggleRow("dot.radiowaves.left.and.right", "灵动岛 / 实时活动", $liveActivity)
                    divider
                    Button { FirstRun.reset(); showReplay = true } label: {
                        row("arrow.counterclockwise", "重看启动引导", "下次冷启动生效", chevron: true)
                    }.buttonStyle(.plain)
                    divider
                    row("info.circle", "关于译梦", "v\(Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "") (\(Bundle.main.infoDictionary?["CFBundleVersion"] as? String ?? ""))")
                }
                Color.clear.frame(height: 190)
            }
            .padding(.horizontal, Theme.margin)
        }
        .background(Theme.base(scheme))
        .sheet(isPresented: $showSources) { SourcesView(embedded: true) }
        .alert("已重置", isPresented: $showReplay) { Button("好", role: .cancel) {} } message: { Text("下次冷启动会从启动动画开始。") }
        .alert("昵称", isPresented: $editingName) {
            TextField("昵称", text: $nickname)
            Button("好", role: .cancel) {}
        }
        .onChange(of: pick) { _, item in
            Task { if let d = try? await item?.loadTransferable(type: Data.self) { AvatarStore.save(d) } }
        }
        .task { cacheBytes = Cache.size() }
    }

    var profile: some View {
        HStack(spacing: 16) {
            PhotosPicker(selection: $pick, matching: .images) {
                AvatarView(size: 72)
                    .overlay(alignment: .bottomTrailing) {
                        Image(systemName: "camera.fill").font(.system(size: 11)).foregroundStyle(Color(white: 0.07))
                            .frame(width: 24, height: 24).background(Circle().fill(Theme.lime))
                    }
            }
            VStack(alignment: .leading, spacing: 4) {
                Button { editingName = true } label: {
                    HStack(spacing: 6) {
                        Text(nickname.isEmpty ? "设置昵称" : nickname).font(.system(size: 22, weight: .bold))
                        Image(systemName: "pencil").font(.system(size: 13)).opacity(0.6)
                    }
                }.buttonStyle(.plain)
                Text(library.connected ? "\(library.user) @ \(library.host.replacingOccurrences(of: "https://", with: ""))" : "尚未连接音乐来源")
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
        Image(systemName: s).font(.system(size: 16, weight: .medium)).frame(width: 28)
    }

    func row(_ ic: String, _ title: String, _ detail: String, chevron: Bool = false) -> some View {
        HStack(spacing: 14) {
            icon(ic)
            Text(title).font(.system(size: 16))
            Spacer(minLength: 8)
            Text(detail).font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme)).lineLimit(1)
            if chevron { Image(systemName: "chevron.right").font(.system(size: 12, weight: .semibold)).foregroundStyle(Theme.secondary(scheme)) }
        }
        .frame(minHeight: 50).contentShape(Rectangle())
    }

    func toggleRow(_ ic: String, _ title: String, _ b: Binding<Bool>) -> some View {
        HStack(spacing: 14) {
            icon(ic)
            Toggle(title, isOn: b).font(.system(size: 16)).tint(Theme.accent(scheme))
        }.frame(minHeight: 50)
    }

    func pickerRow(_ ic: String, _ title: String, selection: Binding<Int>, options: [(Int, String)]) -> some View {
        HStack(spacing: 14) {
            icon(ic)
            Text(title).font(.system(size: 16))
            Spacer()
            Menu {
                ForEach(options, id: \.0) { o in Button(o.1) { selection.wrappedValue = o.0 } }
            } label: {
                HStack(spacing: 4) {
                    Text(options.first { $0.0 == selection.wrappedValue }?.1 ?? "")
                    Image(systemName: "chevron.up.chevron.down").font(.system(size: 10))
                }
                .font(.system(size: 14, weight: .medium)).foregroundStyle(Theme.fg(scheme))
                .padding(.horizontal, 12).frame(height: 30).background(Capsule().fill(Theme.fill(scheme)))
            }
        }.frame(minHeight: 50)
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
