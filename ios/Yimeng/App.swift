import SwiftUI

enum Tab: String, CaseIterable { case home = "首页", collection = "典藏", search = "搜索", me = "我的"
    var icon: String { switch self { case .home: "house.fill"; case .collection: "square.stack.3d.up.fill"; case .search: "magnifyingglass"; case .me: "person.crop.circle" } }
}
enum Route: Hashable { case album(Album), ndAlbum(NDAlbum), artist }

@MainActor
final class AppState: ObservableObject {
    @Published var tab: Tab = .home
    @Published var path: [Route] = []
    @Published var nowPlaying = false
    @Published var lyricsMode = false
    @Published var track: DreamTrack = DreamMock.rose { didSet { if !PlayerStore.shared.hasRealPlayback { syncSystem() } } }
    @Published var isPlaying = false {
        didSet {
            guard !syncingFromPlayer else { return }
            if !PlayerStore.shared.hasRealPlayback { syncSystem() }
        }
    }
    @Published var ambience: Ambience = .doodle
    @Published var intensity: Double = 0.7
    @Published var beatSync = true
    @Published var remoteCoverURL: URL?
    private var trackStart = Date()
    let demo: String?
    /// Suppress recursive isPlaying sync from PlayerStore.
    private var syncingFromPlayer = false

    func setPlaying(_ on: Bool) {
        if PlayerStore.shared.hasRealPlayback {
            if PlayerStore.shared.isPlaying != on { PlayerStore.shared.toggle() }
            syncingFromPlayer = true
            isPlaying = PlayerStore.shared.isPlaying
            syncingFromPlayer = false
        } else {
            isPlaying = on
        }
    }

    func skip(_ d: Int) {
        if PlayerStore.shared.hasRealPlayback {
            PlayerStore.shared.skip(d)
            return
        }
        let all = DreamMock.all
        let i = all.firstIndex(of: track) ?? 0
        withAnimation(.ui) { track = all[(i + d + all.count) % all.count] }
        trackStart = Date()
    }
    func syncSystem() {
        guard !PlayerStore.shared.hasRealPlayback else { return }
        PlaybackSystem.shared.update(track, elapsed: Date().timeIntervalSince(trackStart).truncatingRemainder(dividingBy: track.duration), playing: isPlaying)
    }

    /// Called by PlayerStore when an ND stream starts / advances.
    func applyNDSong(_ s: NDSong, coverURL: URL?, playing: Bool) {
        syncingFromPlayer = true
        defer { syncingFromPlayer = false }
        remoteCoverURL = coverURL ?? LibraryStore.shared.coverArtURL(id: s.coverId ?? s.albumId, size: 600)
        let dur = TimeInterval(s.duration ?? 0)
        track = DreamTrack(
            id: s.id,
            title: s.title,
            artist: s.artist,
            cover: "cvA",
            base: Color(hex: 0x1a1028),
            blobs: [Color(hex: 0x7a1cff), Color(hex: 0xff5fa8), Color(hex: 0x33d17a), Color(hex: 0xffd36b)],
            bpm: 100,
            energy: 0.45,
            genre: "navidrome",
            duration: max(dur, 1)
        )
        isPlaying = playing
        trackStart = Date()
    }

    init() {
        let args = ProcessInfo.processInfo.arguments
        func arg(_ k: String) -> String? { args.firstIndex(of: k).flatMap { $0 + 1 < args.count ? args[$0 + 1] : nil } }
        demo = arg("-demo")
        switch arg("-screen") {
        case "album": path = [.album(Mock.nightfall)]
        case "artist": path = [.artist]
        case "nowplaying", "np-cloud": nowPlaying = true; track = DreamMock.cloud; isPlaying = true
        case "np-zig": nowPlaying = true; track = DreamMock.zig; isPlaying = true
        case "np-ink": nowPlaying = true; track = DreamMock.ink; isPlaying = true
        case "lyrics": nowPlaying = true; lyricsMode = true; track = DreamMock.orange; isPlaying = true
        case "library", "search": tab = .search
        case "collection": tab = .collection
        case "me": tab = .me
        default: break
        }
        if demo == "island" { nowPlaying = true; track = DreamMock.zig; isPlaying = true }
        AppStateBridge.current = self
    }
}

/// Shared namespace for zoom (cover → album) and matched geometry (mini player → full player).
private struct NSKey: EnvironmentKey { static let defaultValue: Namespace.ID? = nil }
extension EnvironmentValues { var heroNS: Namespace.ID? { get { self[NSKey.self] } set { self[NSKey.self] = newValue } } }

extension Animation { static let ui = Animation.smooth(duration: 0.45) }

extension View {
    /// iOS 18+: zoom navigation source. iOS 17: no-op (falls back to standard push).
    @ViewBuilder func zoomSource(_ id: String, _ ns: Namespace.ID?) -> some View {
        if #available(iOS 18.0, *), let ns { matchedTransitionSource(id: id, in: ns) { $0.clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous)) } } else { self }
    }
    @ViewBuilder func zoomDestination(_ id: String, _ ns: Namespace.ID?) -> some View {
        if #available(iOS 18.0, *), let ns { navigationTransition(.zoom(sourceID: id, in: ns)) } else { self }
    }
}

@main
struct YimengApp: App {
    @StateObject var state = AppState()
    var body: some Scene { WindowGroup { RootGate(state: state) } }
}

struct Shell: View {
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @Namespace private var ns

    var body: some View {
        ZStack(alignment: .bottom) {
            Group {
                switch state.tab {
                case .home:
                    NavigationStack(path: $state.path) {
                        HomeView()
                            .toolbar(.hidden, for: .navigationBar)
                            .navigationDestination(for: Route.self) { r in routeView(r) }
                    }
                case .collection:
                    NavigationStack(path: $state.path) {
                        CollectionView()
                            .toolbar(.hidden, for: .navigationBar)
                            .navigationDestination(for: Route.self) { r in routeView(r) }
                    }
                case .search:
                    NavigationStack(path: $state.path) {
                        LibrarySearchView()
                            .toolbar(.hidden, for: .navigationBar)
                            .navigationDestination(for: Route.self) { r in routeView(r) }
                    }
                case .me: SettingsView()
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Theme.base(scheme))
            .ignoresSafeArea(edges: .bottom)

            if !state.nowPlaying { chrome.motionIn(0.25).transition(.move(edge: .bottom).combined(with: .opacity)) }

            if state.nowPlaying {
                NowPlayingView()
                    .transition(.asymmetric(insertion: .opacity.combined(with: .offset(y: 60)), removal: .opacity.combined(with: .offset(y: 80))))
                    .zIndex(2)
            }
        }
        .environment(\.heroNS, ns)
        .animation(.ui, value: state.nowPlaying)
        .onAppear {
            PlaybackSystem.shared.onCommand = { [weak s = state] c in
                DispatchQueue.main.async {
                    guard let s else { return }
                    switch c { case "next": s.skip(1); case "prev": s.skip(-1); case "play": s.setPlaying(true); case "pause": s.setPlaying(false); default: s.setPlaying(!s.isPlaying) }
                }
            }
            state.syncSystem()
            Task { @MainActor in
                await LibraryStore.shared.bootstrap()
                let args = ProcessInfo.processInfo.arguments
                let openND = args.contains("-openFirstND") || UserDefaults.standard.bool(forKey: "ym.debugOpenFirstND")
                let playND = args.contains("-playFirstND") || UserDefaults.standard.bool(forKey: "ym.debugPlayFirstND")
                if openND, let a = LibraryStore.shared.albums.first {
                    state.path.append(.ndAlbum(a))
                    if playND {
                        // NDAlbumView will auto-play when ym.debugPlayFirstND / -playFirstND set
                    }
                }
                UserDefaults.standard.removeObject(forKey: "ym.debugOpenFirstND")
            }
        }
        .task { BackdropCache.warm(Mock.banners.map(\.image) + [Mock.nightfall.cover] + Mock.recent.map(\.cover) + Mock.newIn.map(\.cover)); await runDemo() }
    }

    @ViewBuilder func routeView(_ r: Route) -> some View {
        Group {
            switch r {
            case .album(let a): AlbumView(album: a).zoomDestination(a.id, ns)
            case .ndAlbum(let a): NDAlbumView(album: a).zoomDestination("nd-\(a.id)", ns)
            case .artist: ArtistView().zoomDestination("artist", ns)
            }
        }.toolbar(.hidden, for: .navigationBar)
    }

    func runDemo() async {
        guard let d = state.demo else { return }
        try? await Task.sleep(for: .seconds(2))
        switch d {
        case "zoom":
            state.path.append(.album(Mock.recent[1])); try? await Task.sleep(for: .seconds(3.5))
            state.path.removeLast(); try? await Task.sleep(for: .seconds(2))
            state.path.append(.album(Mock.newIn[1]))
        case "ambience":
            withAnimation(.ui) { state.nowPlaying = true }; try? await Task.sleep(for: .seconds(2))
            for a in [Ambience.stardust, .aurora, .pure, .doodle] { withAnimation(.snappy) { state.ambience = a }; try? await Task.sleep(for: .seconds(2)) }
            for _ in 0..<2 { state.skip(1); try? await Task.sleep(for: .seconds(2.5)) }
        case "enter":
            state.path.append(.album(Mock.recent[0])); try? await Task.sleep(for: .seconds(2.5))
            state.path.removeLast(); try? await Task.sleep(for: .seconds(1))
            state.path.append(.artist); try? await Task.sleep(for: .seconds(2.5))
            state.path = []; withAnimation(.snappy) { state.tab = .search }; try? await Task.sleep(for: .seconds(2.5))
            withAnimation(.snappy) { state.tab = .home }; withAnimation(.ui) { state.nowPlaying = true }; try? await Task.sleep(for: .seconds(3))
            withAnimation(.smooth(duration: 0.45)) { state.lyricsMode = true }
        case "player":
            withAnimation(.ui) { state.nowPlaying = true }; try? await Task.sleep(for: .seconds(3.5))
            withAnimation(.ui) { state.nowPlaying = false }
        case "island":
            withAnimation(.ui) { state.nowPlaying = true }
            PlaybackSystem.shared.forceLiveActivity()
            try? await Task.sleep(for: .seconds(6))
        default: break
        }
    }

    var chrome: some View {
        VStack(spacing: 12) {
            MiniPlayer().onTapGesture { withAnimation(.ui) { state.nowPlaying = true } }
            HStack(spacing: 0) { ForEach(Tab.allCases, id: \.self) { t in tabItem(t) } }
                .padding(.horizontal, 4).frame(height: 62).background(GlassCapsule())
        }
        .padding(.horizontal, Theme.margin).padding(.bottom, 2)
    }

    func tabItem(_ t: Tab) -> some View {
        let active = state.tab == t
        return Button { withAnimation(.snappy) { if state.tab == t { state.path = [] }; state.tab = t } } label: {
            VStack(spacing: 2) {
                Image(systemName: t.icon).font(.system(size: 20))
                Text(t.rawValue).font(.system(size: 10, weight: .semibold))
            }
            .foregroundStyle(active ? Theme.accent(scheme) : Theme.fg(scheme).opacity(0.75))
            .frame(maxWidth: .infinity).frame(height: 54)
            .background { if active { Capsule().fill(Theme.fill(scheme).opacity(1.6)).matchedGeometryEffect(id: "tabpill", in: tabNS) } }
        }.buttonStyle(.plain)
    }
    @Namespace private var tabNS
}

struct MiniPlayer: View {
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @Environment(\.heroNS) var ns
    @ObservedObject private var player = PlayerStore.shared
    var body: some View {
        let t = state.track
        let playing = player.hasRealPlayback ? player.isPlaying : state.isPlaying
        HStack(spacing: 12) {
            cover
            VStack(alignment: .leading, spacing: 1) {
                Text(t.title).font(.system(size: 14, weight: .semibold)).lineLimit(1)
                Text(t.artist).font(.system(size: 12)).opacity(0.7).lineLimit(1)
            }
            Spacer()
            Button { state.setPlaying(!playing) } label: { Image(systemName: playing ? "pause.fill" : "play.fill").font(.system(size: 20)) }.buttonStyle(.plain)
            Button { state.skip(1) } label: { Image(systemName: "forward.fill").font(.system(size: 18)) }.buttonStyle(.plain)
        }
        .foregroundStyle(Theme.fg(scheme))
        .padding(.leading, 8).padding(.trailing, 16)
        .frame(height: 56)
        .background {
            ZStack {
                GlassCapsule()
                Capsule().fill(Theme.base(scheme).opacity(scheme == .dark ? 0.55 : 0.6))
            }
        }
        .contentShape(Capsule())
    }
    @ViewBuilder var cover: some View {
        Group {
            if let url = state.remoteCoverURL ?? player.coverURL {
                CoverImage(url: url)
            } else {
                Image(state.track.cover).resizable().scaledToFill()
            }
        }
        .frame(width: 40, height: 40).clipShape(Circle())
        .modifier(OptionalMatched(ns: ns, id: "npcover"))
    }
}

private struct OptionalMatched: ViewModifier {
    let ns: Namespace.ID?
    let id: String
    @ViewBuilder func body(content: Content) -> some View {
        if let ns { content.matchedGeometryEffect(id: id, in: ns, properties: .frame, anchor: .center, isSource: true) }
        else { content }
    }
}

extension Color {
    func mix(with other: Color, by t: Double) -> Color {
        let a = UIColor(self).cgColor.components ?? [0, 0, 0, 1], b = UIColor(other).cgColor.components ?? [0, 0, 0, 1]
        func c(_ x: [CGFloat], _ i: Int) -> Double { Double(x.count >= 3 ? x[i] : x[0]) }
        return Color(red: c(a, 0) * (1 - t) + c(b, 0) * t, green: c(a, 1) * (1 - t) + c(b, 1) * t, blue: c(a, 2) * (1 - t) + c(b, 2) * t)
    }
}

struct SettingsView: View {
    @Environment(\.colorScheme) var scheme
    @ObservedObject private var sources = SourceStore.shared
    @State private var showSources = false
    @State private var showReplay = false
    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    Text("我的").font(.system(size: 28, weight: .bold)).padding(.top, 16)
                    Text(LibraryStore.shared.connected
                         ? LibraryStore.shared.status
                         : (sources.connectedCount > 0
                            ? "已连接 \(sources.connectedCount) 个来源"
                            : "尚未连接音乐来源"))
                        .foregroundStyle(Theme.secondary(scheme)).padding(.top, 8)

                    Button { showSources = true } label: {
                        settingsRow(icon: "antenna.radiowaves.left.and.right", title: "音乐来源",
                                    detail: sources.configs.filter(\.connected).map(\.kind.title).joined(separator: " · "))
                    }.buttonStyle(.plain)

                    Button {
                        PlaybackSystem.shared.forceLiveActivity()
                    } label: {
                        settingsRow(icon: "dot.radiowaves.left.and.right", title: "启动灵动岛",
                                    detail: "演示 Live Activity")
                    }.buttonStyle(.plain)

                    Button {
                        FirstRun.reset()
                        showReplay = true
                    } label: {
                        settingsRow(icon: "arrow.counterclockwise", title: "重看启动引导",
                                    detail: "下次冷启动走完整流程")
                    }.buttonStyle(.plain)

                    Spacer(minLength: 120)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, Theme.margin)
            }
            .background(Theme.base(scheme))
            .sheet(isPresented: $showSources) {
                SourcesView(embedded: true)
            }
            .alert("已重置", isPresented: $showReplay) {
                Button("好", role: .cancel) {}
            } message: {
                Text("下次用 -fresh 或杀掉 App 再开，会从启动动画开始。")
            }
        }
    }

    private func settingsRow(icon: String, title: String, detail: String) -> some View {
        HStack(spacing: 14) {
            Image(systemName: icon).font(.system(size: 18)).frame(width: 28)
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.system(size: 16, weight: .semibold))
                if !detail.isEmpty {
                    Text(detail).font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme)).lineLimit(1)
                }
            }
            Spacer()
            Image(systemName: "chevron.right").font(.system(size: 13, weight: .semibold))
                .foregroundStyle(Theme.secondary(scheme))
        }
        .foregroundStyle(Theme.fg(scheme))
        .padding(.vertical, 16)
        .overlay(alignment: .bottom) { Rectangle().fill(Theme.divider(scheme)).frame(height: 0.5) }
        .padding(.top, 8)
    }
}
