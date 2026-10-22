import SwiftUI
import AppKit

@main
struct YimengMacApp: App {
    @StateObject private var state = AppState()
    @AppStorage("ym.appearance") private var appearance = 2
    init() {
        guard ProcessInfo.processInfo.arguments.contains("-shotMac") else { return }
        // Offscreen path: WindowGroup .task is unreliable for CLI launches.
        DispatchQueue.main.async {
            let s = AppState()
            Task { @MainActor in
                await MacShot.run(state: s)
            }
        }
        DispatchQueue.main.asyncAfter(deadline: .now() + 18) {
            fputs("[ym] shotMac watchdog\n", stderr)
            exit(0)
        }
    }
    var body: some Scene {
        WindowGroup {
            MacRoot(state: state)
                .preferredColorScheme(appearance == 1 ? .light : appearance == 2 ? .dark : nil)
                .frame(minWidth: 1100, minHeight: 700)
        }
        .windowStyle(.automatic)
        .defaultSize(width: 1360, height: 860)
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
            Button("正在播放") { withAnimation(.ui) { state.nowPlaying = true } }
                .keyboardShortcut("p", modifiers: [.command])
            Button("搜索…") { state.tab = .search; state.nowPlaying = false }
                .keyboardShortcut("f", modifiers: [.command])
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

/// Desktop-native chrome: narrow sidebar + one wide content stage + floating mini player.
/// Not an iPad three-column split.
struct MacRoot: View {
    @ObservedObject var state: AppState
    @Environment(\.colorScheme) private var scheme
    @ObservedObject private var player = PlayerStore.shared
    @ObservedObject private var library = LibraryStore.shared

    var body: some View {
        ZStack {
            HStack(spacing: 0) {
                MacSidebar(state: state)
                    .frame(width: 220)
                    .background(Theme.base(scheme))

                Rectangle()
                    .fill(Theme.fg(scheme).opacity(0.08))
                    .frame(width: 1)
                    .ignoresSafeArea()

                ZStack(alignment: .bottom) {
                    MacContent(state: state)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                        .background(
                            GeometryReader { g in
                                Color.clear.preference(key: MacContentWidthKey.self, value: g.size.width)
                            }
                        )

                    MacMiniBar(state: state)
                        .padding(.horizontal, 28)
                        .padding(.bottom, 18)
                }
                .background(Theme.base(scheme))
            }
            .opacity(state.nowPlaying ? 0.34 : 1)

            if state.nowPlaying {
                MacNowPlaying(state: state)
                    .transition(.opacity.combined(with: .scale(scale: 0.98)))
                    .zIndex(2)
            }
        }
        .background(Theme.base(scheme).ignoresSafeArea())
        .onPreferenceChange(MacContentWidthKey.self) { w in
            guard w > 0 else { return }
            YMLayout.pad = true
            YMLayout.width = w
        }
        .task { await bootstrap() }
        .onChange(of: player.song?.id) { _, _ in updateTitle() }
        .onChange(of: player.isPlaying) { _, _ in updateTitle() }
        .onChange(of: state.nowPlaying) { _, on in
            if on { updateTitle() }
        }
    }

    private func updateTitle() {
        if state.nowPlaying, let s = player.song {
            NSApp.mainWindow?.title = "\(s.title) — \(s.artist)"
        } else if let s = player.song {
            NSApp.mainWindow?.title = "译梦 · \(s.title)"
        } else if library.connected {
            NSApp.mainWindow?.title = "译梦 · \(library.coreServerName.isEmpty ? "已连接" : library.coreServerName)"
        } else {
            NSApp.mainWindow?.title = "译梦"
        }
    }

    private func bootstrap() async {
        YMLayout.pad = true
        if YMLayout.width < 700 { YMLayout.width = 1000 }
        // Offscreen MacShot remounts MacRoot — don't reset status to「连接中…」.
        if LibraryStore.shared.connected {
            updateTitle()
            return
        }
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

private struct MacContentWidthKey: PreferenceKey {
    static var defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) {
        let n = nextValue(); if n > 0 { value = n }
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
            .padding(.leading, 8).padding(.bottom, 18).padding(.top, 16)

            ForEach(Tab.allCases, id: \.self) { t in
                let on = state.tab == t && !state.nowPlaying
                Button {
                    withAnimation(.snappy) {
                        state.nowPlaying = false
                        if state.tab == t { state.path = [] }
                        state.tab = t
                    }
                } label: {
                    HStack(spacing: 12) {
                        ZStack {
                            if on {
                                Crayon(shape: WobblyCircle(), color: Theme.accent(scheme), width: 1.6)
                                    .frame(width: 34, height: 34)
                            }
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
                        Spacer(minLength: 0)
                    }
                    .padding(.horizontal, 8).frame(height: 44)
                    .background(
                        RoundedRectangle(cornerRadius: 14, style: .continuous)
                            .fill(on ? Theme.fill(scheme) : .clear)
                    )
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
            Spacer(minLength: 0)
            Crayon(shape: WaveShape(), color: Theme.fg(scheme).opacity(0.25), width: 1.4)
                .frame(width: 90, height: 10)
                .padding(.leading, 10).padding(.bottom, 20)
        }
        .padding(.horizontal, 14)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .foregroundStyle(Theme.fg(scheme))
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
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            .navigationDestination(for: Route.self) { r in routeView(r) }
        }
        // Room for the floating mini bar.
        .safeAreaInset(edge: .bottom, spacing: 0) {
            Color.clear.frame(height: 88)
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

/// Floating liquid-glass mini player — Music.app style, not a third column.
struct MacMiniBar: View {
    @ObservedObject var state: AppState
    @Environment(\.colorScheme) private var scheme
    @ObservedObject private var player = PlayerStore.shared

    var body: some View {
        let playing = player.hasRealPlayback ? player.isPlaying : state.isPlaying
        HStack(spacing: 14) {
            Button {
                withAnimation(.ui) { state.nowPlaying = true }
            } label: {
                HStack(spacing: 12) {
                    cover
                    VStack(alignment: .leading, spacing: 2) {
                        Text(player.song?.title ?? state.track.title)
                            .font(.system(size: 14, weight: .semibold))
                            .lineLimit(1).truncationMode(.middle)
                        Text(player.song?.artist ?? state.track.artist)
                            .font(.system(size: 12))
                            .foregroundStyle(Theme.secondary(scheme))
                            .lineLimit(1)
                    }
                    .frame(maxWidth: 280, alignment: .leading)
                }
            }
            .buttonStyle(.plain)

            Spacer(minLength: 8)

            HStack(spacing: 18) {
                Button { state.skip(-1) } label: {
                    YMIcon(glyph: .previous, size: 18, weight: 1.8, filled: true)
                }.buttonStyle(.plain)
                Button { state.setPlaying(!playing) } label: {
                    YMIcon(glyph: playing ? .pause : .play, size: 22, weight: 1.9, filled: true)
                        .frame(width: 40, height: 40)
                        .background(Circle().fill(Theme.fg(scheme).opacity(scheme == .dark ? 0.12 : 0.08)))
                }.buttonStyle(.plain)
                Button { state.skip(1) } label: {
                    YMIcon(glyph: .next, size: 18, weight: 1.8, filled: true)
                }.buttonStyle(.plain)
            }

            Spacer(minLength: 8)

            Button {
                withAnimation(.ui) { state.nowPlaying = true }
            } label: {
                YMIcon(glyph: .lyrics, size: 16, weight: 1.6)
                    .opacity(0.7)
            }.buttonStyle(.plain)
        }
        .foregroundStyle(Theme.fg(scheme))
        .padding(.leading, 10).padding(.trailing, 18)
        .frame(height: 64)
        .background {
            ZStack {
                Capsule().fill(.ultraThinMaterial)
                Capsule().fill(Theme.base(scheme).opacity(scheme == .dark ? 0.45 : 0.55))
                Capsule().strokeBorder(Theme.fg(scheme).opacity(0.12), lineWidth: 1)
            }
        }
        .shadow(color: .black.opacity(scheme == .dark ? 0.35 : 0.12), radius: 18, y: 8)
        .frame(maxWidth: 920)
    }

    @ViewBuilder private var cover: some View {
        Group {
            if let url = state.remoteCoverURL ?? player.coverURL {
                CoverImage(url: url, points: 44)
            } else if !state.track.cover.isEmpty {
                Image(state.track.cover).resizable().scaledToFill()
            } else {
                Theme.fill(scheme).overlay(YMBrandMark(size: 22).opacity(0.6))
            }
        }
        .frame(width: 44, height: 44)
        .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
    }
}

/// Full-window now-playing (opened from the mini bar) — cover + doodles + transport.
struct MacNowPlaying: View {
    @ObservedObject var state: AppState
    @Environment(\.colorScheme) private var scheme
    @ObservedObject private var player = PlayerStore.shared
    private let coverSide: CGFloat = 320

    var body: some View {
        ZStack {
            YMBackdrop().ignoresSafeArea()
            DreamField(track: state.track, animated: true).opacity(0.55).ignoresSafeArea()

            VStack(spacing: 22) {
                HStack {
                    Button {
                        withAnimation(.ui) { state.nowPlaying = false }
                    } label: {
                        YMIcon(glyph: .close, size: 16, weight: 1.8)
                            .frame(width: 36, height: 36)
                            .background(Circle().fill(Theme.fill(scheme)))
                    }.buttonStyle(.plain)
                    Spacer()
                    Text("正在播放").font(.system(size: 13, weight: .semibold)).opacity(0.6)
                    Spacer()
                    Color.clear.frame(width: 36, height: 36)
                }
                .padding(.horizontal, 28).padding(.top, 18)

                Spacer(minLength: 12)

                ZStack {
                    GeometryReader { g in
                        let rect = CGRect(
                            x: (g.size.width - coverSide) / 2,
                            y: (g.size.height - coverSide) / 2,
                            width: coverSide, height: coverSide
                        )
                        DoodleLayer(
                            track: state.track,
                            slot: CoverSlot(rect: rect, radius: 14),
                            ambience: state.ambience == .aurora ? .pure : state.ambience,
                            intensity: state.intensity * 0.75,
                            beat: state.beatSync
                        )
                    }
                    .frame(width: coverSide + 120, height: coverSide + 120)
                    .allowsHitTesting(false)

                    Group {
                        if let u = state.remoteCoverURL ?? player.coverURL {
                            CoverImage(url: u, points: coverSide)
                        } else if !state.track.cover.isEmpty {
                            Image(state.track.cover).resizable().scaledToFill()
                        } else {
                            Theme.fill(scheme).overlay(YMBrandMark(size: 96).opacity(0.5))
                        }
                    }
                    .frame(width: coverSide, height: coverSide)
                    .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                    .shadow(color: .black.opacity(0.35), radius: 28, y: 14)

                    Crayon(shape: WobblyFrame(), color: Theme.fg(scheme), width: 2.2)
                        .frame(width: coverSide + 24, height: coverSide + 24)
                        .allowsHitTesting(false)
                }
                .frame(width: coverSide + 120, height: coverSide + 120)

                VStack(spacing: 6) {
                    Text(player.song?.title ?? state.track.title)
                        .font(.system(size: 26, weight: .bold))
                        .lineLimit(2).multilineTextAlignment(.center)
                    Text(player.song?.artist ?? state.track.artist)
                        .font(.system(size: 16))
                        .foregroundStyle(Theme.secondary(scheme))
                }
                .padding(.horizontal, 40)

                PlayerTransport(big: true).environmentObject(state)
                    .frame(maxWidth: 520)
                    .padding(.top, 8)

                Spacer(minLength: 40)
            }
            .foregroundStyle(Theme.fg(scheme))
        }
    }
}

enum MacShot {
    @MainActor static func applyTab(_ state: AppState) {
        let a = ProcessInfo.processInfo.arguments
        state.nowPlaying = false
        if a.contains("-shotMacLib") { state.tab = .library }
        else if a.contains("-shotMacMe") { state.tab = .me }
        else if a.contains("-shotMacSearch") { state.tab = .search }
        else if a.contains("-shotMacColl") { state.tab = .collection }
        else { state.tab = .home }
        state.path = []
    }

    @MainActor static func waitReady(timeout: TimeInterval = 10) async {
        let lib = LibraryStore.shared
        let deadline = Date().addingTimeInterval(timeout)
        while Date() < deadline {
            if lib.connected && !lib.status.contains("连接中") {
                // Give LibraryCache a moment to populate after connect.
                if (lib.songTotal ?? 0) > 0 || !LibraryCache.shared.songs.isEmpty { return }
                try? await Task.sleep(for: .milliseconds(300))
                if Date() > deadline.addingTimeInterval(-0.5) { return }
                continue
            }
            let s = lib.status
            if !s.isEmpty && !s.contains("连接中") && s != "待连接译梦服务端" && lib.connected { return }
            try? await Task.sleep(for: .milliseconds(200))
        }
    }

    @MainActor static func preparePlaying(_ state: AppState) async {
        if PlatformStore.shared.dailies.isEmpty { await PlatformStore.shared.refresh() }
        if let d = PlatformStore.shared.dailies.first {
            let songs = d.tracks.filter { !$0.isUnplayable }
            if !songs.isEmpty {
                PlayerStore.shared.play(songs: songs)
                try? await Task.sleep(for: .milliseconds(1100))
                withAnimation(.ui) { state.nowPlaying = true }
                return
            }
        }
        state.track = DreamMock.rose
        state.isPlaying = true
        state.remoteCoverURL = nil
        withAnimation(.ui) { state.nowPlaying = true }
    }

    @MainActor static func run(state: AppState) async {
        // Ensure Core is up even if MacRoot.bootstrap is still racing.
        await ensureCore()
        await waitReady(timeout: 8)
        await HomeStore.shared.refresh()
        applyTab(state)
        let a = ProcessInfo.processInfo.arguments
        if a.contains("-shotMacLib") {
            state.tab = .library
            await LibraryCache.shared.open(LibraryView.key(.songs, sort: state.albumSort), albumSort: state.albumSort)
            try? await Task.sleep(for: .milliseconds(600))
        }
        if a.contains("-shotMacPlaying") {
            state.tab = .home
            await preparePlaying(state)
            try? await Task.sleep(for: .milliseconds(400))
        }
        try? await Task.sleep(for: .milliseconds(400))
        capture(state: state)
    }

    @MainActor static func ensureCore() async {
        YMLayout.pad = true
        YMLayout.width = 1140
        if LibraryStore.shared.connected { return }
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
        guard !pass.isEmpty else { return }
        if SourceStore.shared.active != .core { await SourceStore.shared.activate(.core) }
        await LibraryStore.shared.connectCore(host: "http://localhost:8741", username: user, password: pass)
    }

    @MainActor static func capture(state: AppState?) {
        let s = state ?? AppState()
        let a = ProcessInfo.processInfo.arguments
        if !a.contains("-shotMacPlaying") { applyTab(s) }
        else { s.tab = .home; s.nowPlaying = true }

        YMLayout.pad = true
        YMLayout.width = 1140

        let root = MacRoot(state: s)
            .environment(\.colorScheme, .dark)
            .preferredColorScheme(.dark)
            .frame(width: 1360, height: 860)
            .background(Theme.base(.dark))
        let host = NSHostingView(rootView: root)
        host.frame = NSRect(x: 0, y: 0, width: 1360, height: 860)
        host.appearance = NSAppearance(named: .darkAqua)
        host.layoutSubtreeIfNeeded()
        RunLoop.current.run(until: Date().addingTimeInterval(0.45))
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
        let name: String
        if a.contains("-shotMacPlaying") { name = "v43-mac-playing.png" }
        else if a.contains("-shotMacLib") { name = "v43-mac-library.png" }
        else if a.contains("-shotMacMe") { name = "v43-mac-me.png" }
        else if a.contains("-shotMacSearch") { name = "v43-mac-search.png" }
        else if a.contains("-shotMacColl") { name = "v43-mac-collection.png" }
        else { name = "v43-mac-home.png" }
        let url = dir.appendingPathComponent(name)
        try? png.write(to: url)
        fputs("[ym] wrote \(url.path) \(png.count)\n", stderr)
        exit(0)
    }
}
