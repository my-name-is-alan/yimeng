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

                    if !state.nowPlaying {
                        MacMiniBar(state: state)
                            .padding(.horizontal, 28)
                            .padding(.bottom, 18)
                            .transition(.move(edge: .bottom).combined(with: .opacity))
                    }
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
        .modifier(GlobalSheets())
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
        await StarStore.shared.load()
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
                if state.macSoundStage {
                    SoundSettingsView()
                } else if state.macQueueStage {
                    QueueSheet()
                        .environment(\.colorScheme, .dark)
                } else if state.macOfflineStage {
                    OfflineSettingsView()
                } else {
                    switch state.tab {
                    case .home: HomeView().environmentObject(state)
                    case .library: LibraryView().environmentObject(state)
                    case .collection: CollectionView().environmentObject(state)
                    case .search: LibrarySearchView().environmentObject(state)
                    case .me: SettingsView().environmentObject(state)
                    }
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

            Button { SheetCenter.shared.queue = true } label: {
                YMIcon(glyph: .queue, size: 16, weight: 1.6).opacity(0.7)
            }.buttonStyle(.plain)
            Button {
                withAnimation(.ui) { state.nowPlaying = true; state.lyricsMode = true }
                if let s = player.song { LyricsStore.shared.load(s) }
            } label: {
                YMIcon(glyph: .lyrics, size: 16, weight: 1.6).opacity(0.7)
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
    @ObservedObject private var lyrics = LyricsStore.shared
    private let coverSide: CGFloat = 320

    var body: some View {
        ZStack {
            YMBackdrop().ignoresSafeArea()
            DreamField(track: state.track, animated: true).opacity(0.55).ignoresSafeArea()

            VStack(spacing: 0) {
                HStack(spacing: 10) {
                    Button {
                        withAnimation(.ui) { state.nowPlaying = false }
                    } label: {
                        YMIcon(glyph: .close, size: 16, weight: 1.8)
                            .frame(width: 36, height: 36)
                            .background(Circle().fill(Theme.fill(scheme)))
                    }.buttonStyle(.plain)
                    Spacer()
                    Text(state.lyricsMode ? "歌词" : "正在播放").font(.system(size: 13, weight: .semibold)).opacity(0.6)
                    Spacer()
                    Button {
                        withAnimation(.ui) { state.lyricsMode.toggle() }
                        if state.lyricsMode, let s = player.song { LyricsStore.shared.load(s) }
                    } label: {
                        Text("词").font(.system(size: 13, weight: .bold))
                            .frame(width: 36, height: 36)
                            .foregroundStyle(state.lyricsMode ? Color(white: 0.07) : Theme.fg(scheme))
                            .background(Circle().fill(state.lyricsMode ? Color.white : Theme.fill(scheme)))
                    }.buttonStyle(.plain)
                    Button {
                        SheetCenter.shared.queue = true
                    } label: {
                        YMIcon(glyph: .queue, size: 15, weight: 1.7)
                            .frame(width: 36, height: 36)
                            .background(Circle().fill(Theme.fill(scheme)))
                    }.buttonStyle(.plain)
                    Menu {
                        Button("音效设置") { SheetCenter.shared.sound = true }
                        Button("播放列表") { SheetCenter.shared.queue = true }
                        Divider()
                        Button(state.lyricsMode ? "封面模式" : "歌词模式") {
                            withAnimation(.ui) { state.lyricsMode.toggle() }
                            if state.lyricsMode, let s = player.song { LyricsStore.shared.load(s) }
                        }
                    } label: {
                        YMIcon(glyph: .more, size: 15, weight: 1.7)
                            .frame(width: 36, height: 36)
                            .background(Circle().fill(Theme.fill(scheme)))
                    }
                }
                .padding(.horizontal, 28).padding(.top, 18)

                if state.lyricsMode {
                    HStack(alignment: .center, spacing: 36) {
                        coverBlock(side: 240)
                            .frame(width: 280)
                        LiveLyricsView(time: player.elapsed, accent: Theme.accent(scheme))
                            .frame(maxWidth: .infinity, maxHeight: .infinity)
                            .clipShape(RoundedRectangle(cornerRadius: 20, style: .continuous))
                    }
                    .padding(.horizontal, 36)
                    .padding(.top, 12)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)

                    PlayerTransport(big: true).environmentObject(state)
                        .frame(maxWidth: 520)
                        .padding(.vertical, 18)
                } else {
                    Spacer(minLength: 12)
                    coverBlock(side: coverSide)
                    VStack(spacing: 6) {
                        Text(player.song?.title ?? state.track.title)
                            .font(.system(size: 26, weight: .bold))
                            .lineLimit(2).multilineTextAlignment(.center)
                        Text(player.song?.artist ?? state.track.artist)
                            .font(.system(size: 16))
                            .foregroundStyle(Theme.secondary(scheme))
                    }
                    .padding(.horizontal, 40)
                    .padding(.top, 8)

                    PlayerTransport(big: true).environmentObject(state)
                        .frame(maxWidth: 520)
                        .padding(.top, 8)

                    Spacer(minLength: 40)
                }
            }
            .foregroundStyle(Theme.fg(scheme))
        }
    }

    @ViewBuilder private func coverBlock(side: CGFloat) -> some View {
        ZStack {
            GeometryReader { g in
                let rect = CGRect(
                    x: (g.size.width - side) / 2,
                    y: (g.size.height - side) / 2,
                    width: side, height: side
                )
                DoodleLayer(
                    track: state.track,
                    slot: CoverSlot(rect: rect, radius: 14),
                    ambience: state.ambience == .aurora ? .pure : state.ambience,
                    intensity: state.intensity * 0.75,
                    beat: state.beatSync
                )
            }
            .frame(width: side + 100, height: side + 100)
            .allowsHitTesting(false)

            Group {
                if let u = state.remoteCoverURL ?? player.coverURL {
                    let candidates = [side, CGFloat(320), CGFloat(600), CGFloat(44)].map { CoverCache.sized(u, points: $0) }
                    let img = candidates.lazy.compactMap { CoverCache.shared.memory($0) ?? CoverCache.shared.cached($0) }.first
                        ?? CoverCache.shared.memoryAnySize(candidates[0])
                    if let img {
                        Image(platformImage: img).resizable().scaledToFill()
                    } else {
                        CoverImage(url: u, points: side)
                    }
                } else if !state.track.cover.isEmpty {
                    Image(state.track.cover).resizable().scaledToFill()
                } else {
                    Theme.fill(scheme).overlay(YMBrandMark(size: 96).opacity(0.5))
                }
            }
            .frame(width: side, height: side)
            .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
            .shadow(color: .black.opacity(0.35), radius: 28, y: 14)

            Crayon(shape: WobblyFrame(), color: Theme.fg(scheme), width: 2.2)
                .frame(width: side + 24, height: side + 24)
                .allowsHitTesting(false)
        }
        .frame(width: side + 100, height: side + 100)
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
        else if a.contains("-shotMacSound") { state.tab = .me }
        else if a.contains("-shotMacAlbums") { state.tab = .library }
        else if a.contains("-shotMacQueue") { state.tab = .home }
        else if a.contains("-shotMacOffline") { state.tab = .me }
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
                // Wait for coverURL from startCurrent / applyNDSong.
                for _ in 0..<20 {
                    if PlayerStore.shared.coverURL != nil || state.remoteCoverURL != nil { break }
                    try? await Task.sleep(for: .milliseconds(40))
                }
                if let u = PlayerStore.shared.coverURL ?? state.remoteCoverURL {
                    let big = CoverCache.sized(u, points: 320)
                    let mid = CoverCache.sized(u, points: 240)
                    let mini = CoverCache.sized(u, points: 44)
                    do {
                        _ = try await CoverCache.shared.fetch(big)
                        _ = try await CoverCache.shared.fetch(mid)
                        _ = try await CoverCache.shared.fetch(mini)
                        let hit = CoverCache.shared.memory(big) != nil
                        fputs("[ym] cover ok mem=\(hit) \(big.path)\n", stderr)
                    } catch {
                        fputs("[ym] cover fail \(error.localizedDescription)\n", stderr)
                    }
                } else {
                    fputs("[ym] coverURL nil after play\n", stderr)
                }
                withAnimation(.ui) { state.nowPlaying = true }
                try? await Task.sleep(for: .milliseconds(500))
                return
            }
        }
        state.track = DreamMock.rose
        state.isPlaying = true
        state.remoteCoverURL = nil
        withAnimation(.ui) { state.nowPlaying = true }
    }

    
    /// Prefetch visible covers so Home/Library/Now Playing shots aren't blurhash stand-ins.
    @MainActor static func warmCovers(limit: Int = 24) async {
        var urls: [URL] = []
        for r in PlatformStore.shared.recommend {
            for p in r.lists.prefix(8) {
                if let u = LibraryStore.shared.coverArtURL(id: p.coverId, size: 300) {
                    urls.append(CoverCache.sized(u, points: YMLayout.cover(140)))
                }
            }
        }
        for s in LibraryCache.shared.songs.prefix(16) {
            if let u = LibraryStore.shared.coverArtURL(id: s.coverId ?? s.albumId, size: 120) {
                urls.append(CoverCache.sized(u, points: 48))
            }
        }
        if let u = PlayerStore.shared.coverURL {
            urls.append(CoverCache.sized(u, points: 320))
            urls.append(CoverCache.sized(u, points: 44))
        }
        await withTaskGroup(of: Void.self) { g in
            for u in urls.prefix(limit) {
                g.addTask { _ = try? await CoverCache.shared.fetch(u) }
            }
        }
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
        if a.contains("-shotMacColl") {
            state.tab = .collection
            await StarStore.shared.load()
            try? await Task.sleep(for: .milliseconds(500))
        }
        if a.contains("-shotMacMe") {
            state.tab = .me
            await StarStore.shared.load()
        }
        if a.contains("-shotMacPlaying") {
            state.tab = .home
            await preparePlaying(state)
            try? await Task.sleep(for: .milliseconds(400))
        }
        state.macSoundStage = false
        state.macQueueStage = false
        state.macOfflineStage = false
        if a.contains("-shotMacSound") {
            state.tab = .me
            state.nowPlaying = false
            state.path = []
            state.macSoundStage = true
            try? await Task.sleep(for: .milliseconds(300))
        }
        if a.contains("-shotMacQueue") {
            state.tab = .home
            state.nowPlaying = false
            if PlatformStore.shared.dailies.isEmpty { await PlatformStore.shared.refresh() }
            if let d = PlatformStore.shared.dailies.first {
                let songs = d.tracks.filter { !$0.isUnplayable }
                if !songs.isEmpty { PlayerStore.shared.play(songs: songs) }
            }
            // Prefetch queue row covers so ScrollView shot isn't empty placeholders.
            var qurls: [URL] = []
            for s in PlayerStore.shared.queue.prefix(24) {
                if let u = LibraryStore.shared.coverArtURL(id: s.coverId ?? s.albumId, size: 120) {
                    qurls.append(CoverCache.sized(u, points: 40))
                }
            }
            await withTaskGroup(of: Void.self) { g in
                for u in qurls { g.addTask { _ = try? await CoverCache.shared.fetch(u) } }
            }
            state.macQueueStage = true
            try? await Task.sleep(for: .milliseconds(600))
        }
        if a.contains("-shotMacOffline") {
            state.tab = .me
            state.nowPlaying = false
            state.macOfflineStage = true
            try? await Task.sleep(for: .milliseconds(300))
        }
        if a.contains("-shotMacSearch") {
            state.tab = .search
            state.nowPlaying = false
            if LibraryStore.shared.albums.isEmpty {
                _ = try? await LibraryStore.shared.search(query: "") // no-op if unsupported
                await HomeStore.shared.refresh()
            }
            // Prefer cache albums if LibraryStore.newest is thin.
            if LibraryStore.shared.albums.count < 8 {
                await LibraryCache.shared.open(LibraryView.key(.albums, sort: "newest"), albumSort: "newest")
                let fromCache = LibraryCache.shared.albumList("newest")
                if !fromCache.isEmpty { LibraryStore.shared.albums = Array(fromCache.prefix(30)) }
            }
            for alb in LibraryStore.shared.albums.prefix(24) {
                if let u = LibraryStore.shared.coverURL(alb, size: 600) {
                    _ = try? await CoverCache.shared.fetch(CoverCache.sized(u, points: YMLayout.tile()))
                }
            }
            try? await Task.sleep(for: .milliseconds(700))
        }
        if a.contains("-shotMacLyrics") {
            state.tab = .home
            await preparePlaying(state)
            if let s = PlayerStore.shared.song {
                LyricsStore.shared.load(s)
                // Wait for lyrics lines.
                for _ in 0..<40 {
                    if case .ready = LyricsStore.shared.phase { break }
                    if case .empty = LyricsStore.shared.phase { break }
                    try? await Task.sleep(for: .milliseconds(100))
                }
            }
            withAnimation(.ui) { state.lyricsMode = true; state.nowPlaying = true }
            try? await Task.sleep(for: .milliseconds(500))
        }
        if a.contains("-shotMacAlbums") {
            state.tab = .library
            state.librarySeg = .albums
            await LibraryCache.shared.open(LibraryView.key(.albums, sort: state.albumSort), albumSort: state.albumSort)
            // Prefetch album grid covers.
            for alb in LibraryCache.shared.albumList(state.albumSort).prefix(18) {
                if let u = LibraryStore.shared.coverURL(alb, size: 300) {
                    _ = try? await CoverCache.shared.fetch(CoverCache.sized(u, points: YMLayout.tile()))
                }
            }
            try? await Task.sleep(for: .milliseconds(500))
        }
        if a.contains("-shotMacHome") || (!a.contains("-shotMacPlaying") && !a.contains("-shotMacSound")
            && !a.contains("-shotMacLyrics") && !a.contains("-shotMacQueue") && !a.contains("-shotMacOffline")
            && !a.contains("-shotMacSearch") && !a.contains("-shotMacLib") && !a.contains("-shotMacAlbums")
            && !a.contains("-shotMacMe") && !a.contains("-shotMacColl")) {
            await PlatformStore.shared.refresh()
            await warmCovers()
        }
        try? await Task.sleep(for: .milliseconds(500))
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
        await HomeStore.shared.refresh()
        await StarStore.shared.load()
    }

    @MainActor static func capture(state: AppState?) {
        let s = state ?? AppState()
        let a = ProcessInfo.processInfo.arguments
        if a.contains("-shotMacPlaying") || a.contains("-shotMacLyrics") {
            s.tab = .home; s.nowPlaying = true
            if a.contains("-shotMacLyrics") { s.lyricsMode = true }
        } else if a.contains("-shotMacAlbums") {
            s.tab = .library; s.librarySeg = .albums; s.nowPlaying = false
        } else if a.contains("-shotMacQueue") {
            s.tab = .home; s.nowPlaying = false; s.macQueueStage = true
        } else if a.contains("-shotMacOffline") {
            s.tab = .me; s.nowPlaying = false; s.macOfflineStage = true
        } else if a.contains("-shotMacSound") {
            s.tab = .me; s.macSoundStage = true
        } else if a.contains("-shotMacSearch") {
            s.tab = .search; s.nowPlaying = false
        } else {
            applyTab(s)
        }

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
        if a.contains("-shotMacPlaying") { name = "v46-mac-playing.png" }
        else if a.contains("-shotMacLyrics") { name = "v46-mac-lyrics.png" }
        else if a.contains("-shotMacAlbums") { name = "v46-mac-albums.png" }
        else if a.contains("-shotMacSound") { name = "v46-mac-sound.png" }
        else if a.contains("-shotMacQueue") { name = "v46-mac-queue.png" }
        else if a.contains("-shotMacOffline") { name = "v46-mac-offline.png" }
        else if a.contains("-shotMacLib") { name = "v46-mac-library.png" }
        else if a.contains("-shotMacMe") { name = "v46-mac-me.png" }
        else if a.contains("-shotMacSearch") { name = "v46-mac-search.png" }
        else if a.contains("-shotMacColl") { name = "v46-mac-collection.png" }
        else if a.contains("-shotMacHome") { name = "v46-mac-home.png" }
        else { name = "v46-mac-home.png" }
        let url = dir.appendingPathComponent(name)
        try? png.write(to: url)
        fputs("[ym] wrote \(url.path) \(png.count)\n", stderr)
        exit(0)
    }
}
