import SwiftUI

enum Tab: String, CaseIterable { case home = "首页", library = "曲库", settings = "设置", search = "搜索"
    var icon: String { switch self { case .home: "house.fill"; case .library: "square.stack.fill"; case .settings: "gearshape"; case .search: "magnifyingglass" } }
}
enum Route: Hashable { case album(Album), artist }

final class AppState: ObservableObject {
    @Published var tab: Tab = .home
    @Published var path: [Route] = []
    @Published var nowPlaying = false
    @Published var lyricsMode = false
    let demo: String?
    init() {
        // Launch arguments: -screen home|album|artist|nowplaying|lyrics|library   -demo zoom|player
        let args = ProcessInfo.processInfo.arguments
        func arg(_ k: String) -> String? { args.firstIndex(of: k).flatMap { $0 + 1 < args.count ? args[$0 + 1] : nil } }
        demo = arg("-demo")
        switch arg("-screen") {
        case "album": path = [.album(Mock.nightfall)]
        case "artist": path = [.artist]
        case "nowplaying": nowPlaying = true
        case "lyrics": nowPlaying = true; lyricsMode = true
        case "library": tab = .search
        default: break
        }
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
    var body: some Scene { WindowGroup { Shell().environmentObject(state) } }
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
                            .navigationDestination(for: Route.self) { r in
                                Group {
                                    switch r {
                                    case .album(let a): AlbumView(album: a).zoomDestination(a.id, ns)
                                    case .artist: ArtistView().zoomDestination("artist", ns)
                                    }
                                }.toolbar(.hidden, for: .navigationBar)
                            }
                    }
                case .library, .search: LibrarySearchView()
                case .settings: SettingsView()
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
        .task { BackdropCache.warm(Mock.banners.map(\.image) + [Mock.nightfall.cover] + Mock.recent.map(\.cover) + Mock.newIn.map(\.cover)); await runDemo() }
    }

    func runDemo() async {
        guard let d = state.demo else { return }
        try? await Task.sleep(for: .seconds(2))
        switch d {
        case "zoom":
            state.path.append(.album(Mock.recent[1])); try? await Task.sleep(for: .seconds(3.5))
            state.path.removeLast(); try? await Task.sleep(for: .seconds(2))
            state.path.append(.album(Mock.newIn[1]))
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
        default: break
        }
    }

    var chrome: some View {
        VStack(spacing: 12) {
            MiniPlayer().onTapGesture { withAnimation(.ui) { state.nowPlaying = true } }
            HStack(spacing: 12) {
                HStack(spacing: 0) { ForEach([Tab.home, .library, .settings], id: \.self) { t in tabItem(t) } }
                    .padding(.horizontal, 4).frame(height: 62).background(GlassCapsule())
                Button { withAnimation(.snappy) { state.tab = .search } } label: {
                    Image(systemName: "magnifyingglass").font(.system(size: 22, weight: .medium))
                        .foregroundStyle(state.tab == .search ? Theme.accent(scheme) : Theme.fg(scheme))
                        .frame(width: 62, height: 62).background(GlassCapsule())
                }.buttonStyle(.plain)
            }
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
    @Environment(\.colorScheme) var scheme
    @Environment(\.heroNS) var ns
    var body: some View {
        let pal = Palette.of(Mock.nightfall.cover)
        HStack(spacing: 12) {
            cover
            VStack(alignment: .leading, spacing: 1) {
                Text("夜航星").font(.system(size: 14, weight: .semibold))
                Text("不才").font(.system(size: 12)).opacity(0.7)
            }
            Spacer()
            Image(systemName: "pause.fill").font(.system(size: 20))
            Image(systemName: "forward.fill").font(.system(size: 18))
        }
        .foregroundStyle(Theme.fg(scheme))
        .padding(.leading, 8).padding(.trailing, 16)
        .frame(height: 56)
        .background {
            ZStack {
                GlassCapsule()
                // Adaptive scrim tinted from the playing cover's palette → guaranteed contrast over any content beneath.
                Capsule().fill(pal.color.mix(with: Theme.base(scheme), by: 0.55).opacity(scheme == .dark ? 0.55 : 0.6))
            }
        }
        .contentShape(Capsule())
    }
    @ViewBuilder var cover: some View {
        let img = Image("ym_nightfall").resizable().scaledToFill().frame(width: 40, height: 40).clipShape(Circle())
        if let ns { img.matchedGeometryEffect(id: "npcover", in: ns, properties: .frame, anchor: .center, isSource: true) } else { img }
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
    var body: some View {
        VStack(alignment: .leading) {
            Text("设置").font(.system(size: 28, weight: .bold)).padding(.top, 16)
            Text("NAS · 192.168.1.20 已连接").foregroundStyle(Theme.secondary(scheme)).padding(.top, 8)
            Spacer()
        }.frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, Theme.margin)
    }
}
