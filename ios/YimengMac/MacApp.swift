import SwiftUI
import AppKit

@main
struct YimengMacApp: App {
    @StateObject private var state = AppState()
    @AppStorage("ym.appearance") private var appearance = 2
    init() {
        if ProcessInfo.processInfo.arguments.contains("-shotMac") {
            let s = AppState()
            MacShot.applyTab(s)
            // Offscreen capture; hard timeout so the process never hangs on Core.
            DispatchQueue.main.asyncAfter(deadline: .now() + 2.8) {
                MacShot.capture(state: s)
            }
            DispatchQueue.main.asyncAfter(deadline: .now() + 8.0) {
                fputs("[ym] shotMac watchdog exit\n", stderr)
                exit(0)
            }
        }
    }
    var body: some Scene {
        WindowGroup {
            MacRoot(state: state)
                .preferredColorScheme(appearance == 1 ? .light : appearance == 2 ? .dark : nil)
                .frame(minWidth: 980, minHeight: 640)
                .onAppear { }
        }
        .windowStyle(.automatic)
        .defaultSize(width: 1280, height: 800)
        .commands { MacCommands(state: state) }
    }
}

struct MacCommands: Commands {
    @ObservedObject var state: AppState
    @ObservedObject private var player = PlayerStore.shared
    var body: some Commands {
        CommandMenu("播放") {
            Button(player.isPlaying || state.isPlaying ? "暂停" : "播放") {
                state.setPlaying(!(player.hasRealPlayback ? player.isPlaying : state.isPlaying))
            }
            .keyboardShortcut(.space, modifiers: [])
            Button("上一首") { state.skip(-1) }.keyboardShortcut("[", modifiers: [.command])
            Button("下一首") { state.skip(1) }.keyboardShortcut("]", modifiers: [.command])
            Divider()
            Button("搜索…") { state.tab = .search }.keyboardShortcut("f", modifiers: [.command])
        }
        CommandGroup(replacing: .appInfo) {
            Button("关于译梦") {
                NSApp.orderFrontStandardAboutPanel(options: [
                    .applicationName: "译梦",
                    .credits: NSAttributedString(string: "梦中的涂鸦 · 梦境环")
                ])
            }
        }
    }
}

struct MacRoot: View {
    @ObservedObject var state: AppState
    @Environment(\.colorScheme) private var scheme
    @ObservedObject private var player = PlayerStore.shared
    @ObservedObject private var library = LibraryStore.shared
    var body: some View {
        NavigationSplitView {
            MacSidebar(state: state)
                .navigationSplitViewColumnWidth(min: 200, ideal: 220, max: 260)
        } content: {
            MacContent(state: state)
                .navigationSplitViewColumnWidth(min: 380, ideal: 560)
        } detail: {
            MacPlayerColumn(state: state)
                .navigationSplitViewColumnWidth(min: 320, ideal: 400)
        }
        .navigationSplitViewStyle(.balanced)
        .background(Theme.base(scheme).ignoresSafeArea())
        .task { await bootstrap() }
        .onChange(of: player.song?.id) { _, _ in updateTitle() }
        .onChange(of: player.isPlaying) { _, _ in updateTitle() }
    }
    private func updateTitle() {
        if let s = player.song {
            NSApp.mainWindow?.title = "\(s.title) — \(s.artist)"
        } else if library.connected {
            NSApp.mainWindow?.title = "译梦 · \(library.coreServerName.isEmpty ? "已连接" : library.coreServerName)"
        } else {
            NSApp.mainWindow?.title = "译梦"
        }
    }
    private func bootstrap() async {
        YMLayout.pad = true
        YMLayout.width = 720
        let home = FileManager.default.homeDirectoryForCurrentUser
        let url = home.appendingPathComponent(".ym-core-account.env")
        var user = "yimeng-test"; var pass = ""
        if let t = try? String(contentsOf: url, encoding: .utf8) {
            for line in t.split(whereSeparator: \.isNewline) {
                let p = line.split(separator: "=", maxSplits: 1)
                guard p.count == 2 else { continue }
                let k = String(p[0]).trimmingCharacters(in: .whitespaces)
                let v = String(p[1]).trimmingCharacters(in: .whitespaces)
                if k == "YM_CORE_USER" || k == "USER" { user = v }
                if k == "YM_CORE_PASS" || k == "PASS" || k == "PASSWORD" { pass = v }
            }
        }
        guard !pass.isEmpty else {
            NSLog("[ym] mac bootstrap: missing credentials file")
            return
        }
        if SourceStore.shared.active != .core {
            await SourceStore.shared.activate(.core)
        }
        await LibraryStore.shared.connectCore(host: "http://localhost:8741", username: user, password: pass)
        await HomeStore.shared.refresh()
        updateTitle()
    }
}

struct MacSidebar: View {
    @ObservedObject var state: AppState
    @Environment(\.colorScheme) private var scheme
    @ObservedObject private var acct = CoreAccountStore.shared
    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 10) {
                YMBrandMark(size: 34)
                Text("译梦").font(.system(size: 20, weight: .bold))
            }
            .padding(.leading, 8).padding(.bottom, 18).padding(.top, 12)
            ForEach(Tab.allCases, id: \.self) { t in
                let on = state.tab == t
                Button {
                    withAnimation(.snappy) { if state.tab == t { state.path = [] }; state.tab = t }
                } label: {
                    HStack(spacing: 12) {
                        ZStack {
                            if on { Crayon(shape: WobblyCircle(), color: Theme.accent(scheme), width: 1.6).frame(width: 34, height: 34) }
                            YMIcon(glyph: t.glyph, size: 18, weight: on ? 2.0 : 1.6, active: on)
                            if t == .me, acct.unread > 0 {
                                Text(acct.unread > 99 ? "99+" : "\(acct.unread)")
                                    .font(.system(size: 9, weight: .bold)).foregroundStyle(.white)
                                    .padding(.horizontal, 4).padding(.vertical, 1)
                                    .background(Capsule().fill(Color.red))
                                    .offset(x: 14, y: -12)
                            }
                        }.frame(width: 36, height: 36)
                        Text(t.rawValue).font(.system(size: 15, weight: on ? .semibold : .regular))
                        Spacer()
                    }
                    .padding(.horizontal, 8).frame(height: 46)
                    .background(RoundedRectangle(cornerRadius: 14, style: .continuous).fill(on ? Theme.fill(scheme) : .clear))
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
            Spacer()
            Crayon(shape: WaveShape(), color: Theme.fg(scheme).opacity(0.25), width: 1.4)
                .frame(width: 90, height: 10).padding(.leading, 10).padding(.bottom, 20)
        }
        .padding(.horizontal, 14)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .foregroundStyle(Theme.fg(scheme))
        .background(Theme.base(scheme))
    }
}

struct MacContent: View {
    @ObservedObject var state: AppState
    var body: some View {
        NavigationStack(path: $state.path) {
            Group {
                switch state.tab {
                case .home: HomeView().environmentObject(state)
                case .library: LibraryView().environmentObject(state)
                case .collection: CollectionView().environmentObject(state)
                case .search: LibrarySearchView().environmentObject(state)
                case .me: SettingsView().environmentObject(state)
                }
            }
            .navigationDestination(for: Route.self) { r in routeView(r) }
        }
    }
    @ViewBuilder
    private func routeView(_ r: Route) -> some View {
        switch r {
        case .album(let a): AlbumView(album: a).environmentObject(state)
        case .ndAlbum(let a): NDAlbumView(album: a).environmentObject(state)
        case .ndArtist(let a): ArtistDetailView(artist: a).environmentObject(state)
        case .ndPlaylist(let p): PlaylistDetailView(playlist: p).environmentObject(state)
        case .artist: ArtistView().environmentObject(state)
        case .genre(let g): GenreView(genre: g).environmentObject(state)
        case .genres: GenreListView().environmentObject(state)
        case .charts: ChartsView().environmentObject(state)
        case .yueqian: YueqianPage()
        case .trackList(let s): TrackListPage(spec: s).environmentObject(state)
        }
    }
}

struct MacPlayerColumn: View {
    @ObservedObject var state: AppState
    @ObservedObject private var player = PlayerStore.shared
    @Environment(\.colorScheme) private var scheme
    var body: some View {
        VStack(spacing: 18) {
            Spacer(minLength: 20)
            ZStack {
                if let u = player.coverURL {
                    CoverImage(url: u).frame(width: 260, height: 260)
                        .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
                } else if !state.track.cover.isEmpty {
                    // demo track cover name — fall back to brand
                    RoundedRectangle(cornerRadius: 8).fill(Theme.fill(scheme)).frame(width: 260, height: 260)
                        .overlay(YMBrandMark(size: 80).opacity(0.5))
                } else {
                    RoundedRectangle(cornerRadius: 8).fill(Theme.fill(scheme)).frame(width: 260, height: 260)
                        .overlay(YMBrandMark(size: 80).opacity(0.5))
                }
                Crayon(shape: WobblyFrame(), color: Theme.fg(scheme), width: 2).frame(width: 280, height: 280)
            }
            if let s = player.song {
                Text(s.title).font(.system(size: 20, weight: .semibold)).lineLimit(2).truncationMode(.middle)
                Text(s.artist).font(.system(size: 14)).foregroundStyle(Theme.secondary(scheme))
            } else {
                Text(state.track.title).font(.system(size: 20, weight: .semibold))
                Text(state.track.artist).font(.system(size: 14)).foregroundStyle(Theme.secondary(scheme))
            }
            PlayerTransport(big: true).environmentObject(state)
            Spacer()
        }
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(YMBackdrop())
    }
}

enum MacShot {
    @MainActor static func applyTab(_ state: AppState) {
        let a = ProcessInfo.processInfo.arguments
        if a.contains("-shotMacLib") { state.tab = .library }
        else if a.contains("-shotMacMe") { state.tab = .me }
        else if a.contains("-shotMacSearch") { state.tab = .search }
        else if a.contains("-shotMacColl") { state.tab = .collection }
        else { state.tab = .home }
    }
    @MainActor static func capture(state: AppState?) {
        let s = state ?? AppState()
        applyTab(s)
        let root = MacRoot(state: s)
            .environment(\.colorScheme, .dark)
            .preferredColorScheme(.dark)
            .frame(width: 1280, height: 800)
            .background(Theme.base(.dark))
        let host = NSHostingView(rootView: root)
        host.frame = NSRect(x: 0, y: 0, width: 1280, height: 800)
        host.appearance = NSAppearance(named: .darkAqua)
        host.layoutSubtreeIfNeeded()
        guard let img = host.bitmapImageRepForCachingDisplay(in: host.bounds) else {
            fputs("[ym] shotMac bitmap failed\n", stderr); exit(0)
        }
        host.cacheDisplay(in: host.bounds, to: img)
        guard let png = img.representation(using: .png, properties: [:]) else {
            fputs("[ym] shotMac png failed\n", stderr); exit(0)
        }
        let dir = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("ymshots")
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let a = ProcessInfo.processInfo.arguments
        let name: String
        if a.contains("-shotMacLib") { name = "v42-mac-library.png" }
        else if a.contains("-shotMacMe") { name = "v42-mac-me.png" }
        else if a.contains("-shotMacSearch") { name = "v42-mac-search.png" }
        else if a.contains("-shotMacColl") { name = "v42-mac-collection.png" }
        else { name = "v42-mac-home.png" }
        let url = dir.appendingPathComponent(name)
        try? png.write(to: url)
        fputs("[ym] wrote \(url.path) \(png.count)\n", stderr)
        if name == "v42-mac-home.png" {
            try? png.write(to: dir.appendingPathComponent("v42-mac-shell.png"))
        }
        exit(0)
    }
}
