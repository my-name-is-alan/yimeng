import SwiftUI

enum Tab: String, CaseIterable { case home = "首页", library = "曲库", collection = "典藏", search = "搜索", me = "我的"
    var icon: String { switch self { case .home: "house.fill"; case .library: "music.note.list"; case .collection: "square.stack.3d.up.fill"; case .search: "magnifyingglass"; case .me: "person.crop.circle" } }
}
enum Route: Hashable { case album(Album), ndAlbum(NDAlbum), ndArtist(NDArtist), ndPlaylist(NDPlaylist), artist }

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
    @Published var librarySeg: LibraryView.Seg = .songs
    @Published var albumSort = "alphabeticalByName"
    @Published var ambience: Ambience = .doodle
    @Published var intensity: Double = UserDefaults.standard.object(forKey: "ym.intensity") as? Double ?? 0.7 {
        didSet { UserDefaults.standard.set(intensity, forKey: "ym.intensity") }
    }
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
            duration: max(dur, 1),
            lyrics: []
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
        case "library": tab = .library
        case "search": tab = .search
        case "collection": tab = .collection
        case "me": tab = .me
        default: break
        }
        #if DEBUG
        if let s = arg("-seg"), let seg = LibraryView.Seg(rawValue: s) { librarySeg = seg }
        #endif
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
    @AppStorage("ym.appearance") private var appearance = 0   // 0 system · 1 light · 2 dark
    var body: some Scene {
        WindowGroup {
            RootGate(state: state)
                .preferredColorScheme(appearance == 1 ? .light : appearance == 2 ? .dark : nil)
        }
    }
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
                case .library:
                    NavigationStack(path: $state.path) {
                        LibraryView()
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
        .animation(.ui, value: player.hasRealPlayback)
        .onAppear {
            PlaybackSystem.shared.onCommand = { [weak s = state] c in
                DispatchQueue.main.async {
                    guard let s else { return }
                    switch c { case "next": s.skip(1); case "prev": s.skip(-1); case "play": s.setPlaying(true); case "pause": s.setPlaying(false); default: s.setPlaying(!s.isPlaying) }
                }
            }
            if state.demo != nil { state.syncSystem() }
            Task { @MainActor in
                await LibraryStore.shared.bootstrap()
                await PlayerStore.shared.restore()
                async let h: Void = HomeStore.shared.refresh()
                async let s: Void = StarStore.shared.load()
                _ = await (h, s)
            }
        }
        .modifier(GlobalSheets())
        .task { BackdropCache.warm(Mock.banners.map(\.image) + [Mock.nightfall.cover] + Mock.recent.map(\.cover) + Mock.newIn.map(\.cover)); await runDemo() }
    }

    @ViewBuilder func routeView(_ r: Route) -> some View {
        Group {
            switch r {
            case .album(let a): AlbumView(album: a).zoomDestination(a.id, ns)
            case .ndAlbum(let a): NDAlbumView(album: a).zoomDestination("nd-\(a.id)", ns)
            case .ndArtist(let a): ArtistDetailView(artist: a).zoomDestination("ar-\(a.id)", ns)
            case .ndPlaylist(let p): PlaylistDetailView(playlist: p)
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

    @ObservedObject private var player = PlayerStore.shared
    var chrome: some View {
        VStack(spacing: 12) {
            if player.hasRealPlayback {
                MiniPlayer().onTapGesture { withAnimation(.ui) { state.nowPlaying = true } }
                    .transition(.move(edge: .bottom).combined(with: .opacity))
            }
            ZStack(alignment: .bottom) {
                HStack(spacing: 0) {
                    ForEach(Tab.allCases, id: \.self) { t in
                        if t == .collection { Color.clear.frame(width: 76) } else { tabItem(t) }
                    }
                }
                .padding(.horizontal, 4).frame(height: 62).background(GlassCapsule())
                centreButton.offset(y: -18)
            }
        }
        .padding(.horizontal, Theme.margin).padding(.bottom, 2)
    }

    /// 典藏: raised liquid-glass orb tinted by the current cover, crayon moon + star (moonwave glyph). Lime only as a dot.
    var centreButton: some View {
        CentreOrb(active: state.tab == .collection) {
            withAnimation(.snappy) { if state.tab == .collection { state.path = [] }; state.tab = .collection }
        }
    }

    func tabItem(_ t: Tab) -> some View {
        let active = state.tab == t
        return Button { withAnimation(.snappy) { if state.tab == t { state.path = [] }; state.tab = t } } label: {
            VStack(spacing: 2) {
                Image(systemName: t.icon).font(.system(size: 19))
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
            if player.hasRealPlayback {
                Button { SheetCenter.shared.queue = true } label: { Image(systemName: "list.bullet").font(.system(size: 17)) }.buttonStyle(.plain)
            }
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



struct CentreOrb: View {
    let active: Bool
    let action: () -> Void
    @ObservedObject private var player = PlayerStore.shared
    @ObservedObject private var diff = RemoteDiffusion.shared
    @ObservedObject private var home = HomeStore.shared
    @Environment(\.colorScheme) var scheme

    private var tint: Color {
        let lib = LibraryStore.shared
        let u = player.coverURL ?? home.data.banner.first.flatMap { lib.coverURL($0, size: 600) }
        return diff.entry(u)?.color ?? Color(hex: 0x6b4bd8)
    }

    var body: some View {
        Button(action: action) {
            ZStack {
                // soft cover-coloured glow under the glass
                Circle().fill(tint.opacity(active ? 0.55 : 0.32)).blur(radius: active ? 14 : 10).scaleEffect(1.05)
                glass
                VStack(spacing: 2) {
                    ZStack {
                        Crayon(shape: MoonShape(), color: .white, width: 2.2).frame(width: 22, height: 22)
                            .rotationEffect(.degrees(active ? -12 : 0))
                        Crayon(shape: StarShape(), color: .white, width: 1.6).frame(width: 9, height: 9)
                            .offset(x: 13, y: -11).scaleEffect(active ? 1.15 : 0.9)
                    }
                    .frame(width: 30, height: 26)
                    Text("典藏").font(.system(size: 10, weight: .semibold)).foregroundStyle(.white.opacity(0.92))
                }
                if active {
                    Circle().fill(Theme.lime).frame(width: 5, height: 5).offset(y: 27).transition(.scale.combined(with: .opacity))
                }
            }
            .frame(width: 64, height: 64)
            .animation(.spring(response: 0.45, dampingFraction: 0.6), value: active)
            .animation(.smooth(duration: 0.6), value: tint)
        }
        .buttonStyle(.plain)
    }

    @ViewBuilder var glass: some View {
        if #available(iOS 26.0, *) {
            Color.clear.glassEffect(.regular.tint(tint.opacity(0.45)).interactive(), in: Circle())
                .overlay(Circle().strokeBorder(.white.opacity(0.35), lineWidth: 0.8))
        } else {
            ZStack {
                Circle().fill(.ultraThinMaterial)
                Circle().fill(tint.opacity(0.42))
                Circle().fill(RadialGradient(colors: [.white.opacity(0.35), .clear], center: UnitPoint(x: 0.3, y: 0.15), startRadius: 0, endRadius: 40))
                Circle().strokeBorder(.white.opacity(0.4), lineWidth: 0.8)
            }
            .shadow(color: .black.opacity(0.3), radius: 10, y: 5)
        }
    }
}
