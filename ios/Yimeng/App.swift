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
    init() {
        // Launch arguments for screenshots: -screen home|album|artist|nowplaying|lyrics|library
        let args = ProcessInfo.processInfo.arguments
        if let i = args.firstIndex(of: "-screen"), i + 1 < args.count {
            switch args[i + 1] {
            case "album": path = [.album(Mock.nightfall)]
            case "artist": path = [.artist]
            case "nowplaying": nowPlaying = true
            case "lyrics": nowPlaying = true; lyricsMode = true
            case "library": tab = .search
            default: break
            }
        }
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
    @State private var glassRects: [CGRect] = []

    var body: some View {
        ZStack(alignment: .bottom) {
            content
                .glassLens(glassRects)
            chrome
        }
        .coordinateSpace(name: "shell")
        .onPreferenceChange(GlassFrameKey.self) { glassRects = $0 }
        .background(Theme.base(scheme).ignoresSafeArea())
        .fullScreenCover(isPresented: $state.nowPlaying) { NowPlayingView() }
    }

    @ViewBuilder var content: some View {
        Group {
            switch state.tab {
            case .home:
                if let r = state.path.last {
                    switch r { case .album(let a): AlbumView(album: a); case .artist: ArtistView() }
                } else { HomeView() }
            case .library, .search: LibrarySearchView()
            case .settings: SettingsView()
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Theme.base(scheme))
        .ignoresSafeArea(edges: .bottom)
    }

    var chrome: some View {
        VStack(spacing: 12) {
            MiniPlayer().onTapGesture { state.nowPlaying = true }
            HStack(spacing: 12) {
                HStack(spacing: 0) {
                    ForEach([Tab.home, .library, .settings], id: \.self) { t in tabItem(t) }
                }
                .padding(.horizontal, 4)
                .frame(height: 62)
                .background(GlassCapsule())
                .glassFrame()
                Button { state.tab = .search; state.path = [] } label: {
                    Image(systemName: "magnifyingglass").font(.system(size: 22, weight: .medium))
                        .foregroundStyle(state.tab == .search ? Theme.accent(scheme) : Theme.fg(scheme))
                        .frame(width: 62, height: 62).background(GlassCapsule())
                }.buttonStyle(.plain).glassFrame()
            }
        }
        .padding(.horizontal, Theme.margin)
        .padding(.bottom, 2)
    }

    func tabItem(_ t: Tab) -> some View {
        let active = state.tab == t || (t == .library && state.tab == .search && false)
        return Button { state.tab = t; state.path = [] } label: {
            VStack(spacing: 2) {
                Image(systemName: t.icon).font(.system(size: 20))
                Text(t.rawValue).font(.system(size: 10, weight: .semibold))
            }
            .foregroundStyle(active ? Theme.accent(scheme) : Theme.fg(scheme).opacity(0.75))
            .frame(maxWidth: .infinity).frame(height: 54)
            .background(Capsule().fill(active ? Theme.fill(scheme).opacity(1.6) : .clear))
        }.buttonStyle(.plain)
    }
}

struct MiniPlayer: View {
    @Environment(\.colorScheme) var scheme
    var body: some View {
        HStack(spacing: 12) {
            Image("ym_nightfall").resizable().scaledToFill().frame(width: 40, height: 40).clipShape(Circle())
            VStack(alignment: .leading, spacing: 1) {
                Text("夜航星").font(.system(size: 14, weight: .semibold))
                Text("不才").font(.system(size: 12)).foregroundStyle(Theme.secondary(scheme))
            }
            Spacer()
            Image(systemName: "pause.fill").font(.system(size: 20))
            Image(systemName: "forward.fill").font(.system(size: 18))
        }
        .foregroundStyle(Theme.fg(scheme))
        .padding(.leading, 8).padding(.trailing, 16)
        .frame(height: 56)
        .background(GlassCapsule())
        .contentShape(Capsule())
        .glassFrame()
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
