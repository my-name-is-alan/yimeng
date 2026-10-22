import SwiftUI

/// 曲库 — full Navidrome library: 歌曲 / 专辑 / 歌手 / 歌单 / 收藏. Lives in the 典藏 tab when a server is connected.
struct LibraryView: View {
    enum Seg: String, CaseIterable { case songs = "歌曲", albums = "专辑", artists = "歌手", playlists = "歌单", starred = "收藏" }
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @ObservedObject private var library = LibraryStore.shared

    @ObservedObject private var cache = LibraryCache.shared
    @State private var visited: Set<Seg> = []
    @State private var origin: String? = CoreState.originFilter
    nonisolated(unsafe) static var cycled = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            VStack(alignment: .leading, spacing: 2) {
                Text("曲库").font(.system(size: 28, weight: .bold))
                Text(library.status.isEmpty ? "Navidrome" : library.status)
                    .font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme))
            }
            .padding(.horizontal, Theme.margin).padding(.top, 16)

            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 8) {
                    ForEach(Seg.allCases, id: \.self) { s in
                        let seg = state.librarySeg
                        Button { state.librarySeg = s } label: {
                            Text(label(s)).font(.system(size: 14, weight: .semibold))
                                .padding(.horizontal, 16).frame(height: 34)
                                .foregroundStyle(seg == s ? Theme.base(scheme) : Theme.fg(scheme))
                                .background(Capsule().fill(seg == s ? Theme.fg(scheme) : Theme.fill(scheme)))
                        }.buttonStyle(.plain)
                    }
                }.padding(.horizontal, Theme.margin)
            }.padding(.top, 16).padding(.bottom, NavidromeClient.core ? 4 : 12)
            if NavidromeClient.core && [.songs, .albums].contains(state.librarySeg) {
                SourceFilterChips(origin: $origin).padding(.bottom, 10)
                    .onChange(of: origin) { _, o in
                        CoreState.originFilter = o
                        Task { await cache.ensure(LibraryView.key(state.librarySeg, sort: state.albumSort), force: true) }
                    }
            }

            // Each visited segment stays mounted (own scroll position, no refetch on switch).
            ZStack {
                ForEach(Seg.allCases, id: \.self) { s in
                    if visited.contains(s) || s == state.librarySeg {
                        SegmentScroll(seg: s)
                            .opacity(s == state.librarySeg ? 1 : 0)
                            .allowsHitTesting(s == state.librarySeg)
                    }
                }
            }
        }
        .background(YMBackdrop())
        .onAppear { visited.insert(state.librarySeg) }
        .onChange(of: state.librarySeg) { _, s in visited.insert(s) }
        .task { await cache.open(LibraryView.key(state.librarySeg, sort: state.albumSort), albumSort: state.albumSort) }
        #if DEBUG
        .task {
            guard ProcessInfo.processInfo.arguments.contains("-cycle-segs"), !LibraryView.cycled else { return }
            LibraryView.cycled = true
            let st = state
            Task { @MainActor in
                try? await Task.sleep(for: .seconds(8))
                NetLog.logger.info("CYCLE start")
                for _ in 0..<2 { for s in Seg.allCases { st.librarySeg = s; try? await Task.sleep(for: .seconds(1.2)) } }
                st.tab = .home; try? await Task.sleep(for: .seconds(1.5)); st.tab = .library
                try? await Task.sleep(for: .seconds(1.5))
                for s in Seg.allCases { st.librarySeg = s; try? await Task.sleep(for: .seconds(1.0)) }
                NetLog.logger.info("CYCLE end")
            }
        }
        #endif
    }

    static func key(_ s: Seg, sort: String) -> String {
        switch s {
        case .songs: return "songs"
        case .albums: return "albums:\(sort)"
        case .artists: return "artists"
        case .playlists: return "playlists"
        case .starred: return "starred"
        }
    }
}

/// One segment's scroll view: cached content, scroll anchor restore, pull-to-refresh.
private struct SegmentScroll: View {
    let seg: LibraryView.Seg
    @EnvironmentObject var state: AppState
    @ObservedObject private var cache = LibraryCache.shared
    @State private var anchor: String?
    @ObservedObject private var library = LibraryStore.shared
    @State private var pickSeq = 0
    private var key: String { LibraryView.key(seg, sort: state.albumSort) }

    private var hasIndex: Bool {
        switch seg {
        case .artists: return library.artistIndex != nil
        case .albums: return state.albumSort == "alphabeticalByName" && !cache.albumList(state.albumSort).isEmpty
        default: return false   // 歌曲: search3 order isn't by name on the server — no meaningful A–Z
        }
    }
    private var activeLetter: String? {
        guard let a = anchor else { return nil }
        switch seg {
        case .artists: return library.artistIndex?.letterOf[a]
        case .albums: return cache.albumList(state.albumSort).first { $0.id == a }.map { Initials.serverLetter($0.name) }
        default: return nil
        }
    }

    private func pick(_ l: String, _ proxy: ScrollViewProxy) {
        pickSeq += 1; let seq = pickSeq
        switch seg {
        case .artists:
            guard let idx = library.artistIndex else { return }
            let from = Initials.letters.firstIndex(of: l) ?? 0
            guard let id = Initials.letters[from...].lazy.compactMap({ idx.firstId[$0] }).first
                    ?? Initials.letters[..<from].reversed().lazy.compactMap({ idx.firstId[$0] }).first else { return }
            proxy.scrollTo(id, anchor: .top)
        case .albums:
            Task {
                guard let id = await cache.jumpAlbums(key, letter: l), seq == pickSeq else { return }
                await Task.yield()
                proxy.scrollTo(id, anchor: .top)
            }
        default: break
        }
    }

    var body: some View {
        ScrollViewReader { proxy in
        ScrollView(showsIndicators: false) {
            Group {
                switch seg {
                case .songs: SongsList()
                case .albums: AlbumsGrid()
                case .artists: ArtistsList()
                case .playlists: VStack(spacing: 0) { PlaylistsList(); if NavidromeClient.core { PlatformPlaylistGroups() } }
                case .starred: StarredList()
                }
            }
            .scrollTargetLayout()
            Color.clear.frame(height: 170)
        }
        .scrollPosition(id: $anchor, anchor: .top)
        .overlay(alignment: .trailing) {
            if hasIndex {
                IndexBar(available: seg == .artists ? library.artistIndex?.available : nil,
                         active: activeLetter) { pick($0, proxy) }
                    .padding(.trailing, 1).padding(.bottom, 120)
                    .transition(.opacity)
            }
        }
        }
        .onAppear { anchor = cache.scrollAnchor[seg.rawValue] }
        .onChange(of: anchor) { _, a in if let a { cache.scrollAnchor[seg.rawValue] = a } }
        .refreshable {
            if seg == .starred { await StarStore.shared.load() } else { await cache.ensure(key, force: true) }
        }
        .task(id: key) { if seg != .starred { await cache.ensure(key) } }
    }
}

extension LibraryView {
    func label(_ s: Seg) -> String {
        switch s {
        case .songs: return library.songTotal.map { "歌曲 \($0)" } ?? s.rawValue
        case .albums: return library.albumTotal.map { "专辑 \($0)" } ?? s.rawValue
        default: return s.rawValue
        }
    }
}

/// Song row that plays itself with `list` as queue.
struct SongRowPlay: View {
    let songs: [NDSong]; let i: Int
    var showCover = true
    @EnvironmentObject var state: AppState
    @ObservedObject private var player = PlayerStore.shared
    @ObservedObject private var stars = StarStore.shared
    var body: some View {
        let s = songs[i]
        TrackRow(song: s, index: i + 1, title: s.title, subtitle: [s.artist, s.album ?? ""].filter { !$0.isEmpty }.joined(separator: " · "),
                 trailing: s.durationLabel, playing: player.song?.id == s.id,
                 accessory: AnyView(StarButton(on: stars.isStarred(song: s.id), size: 15) { stars.toggle(s) }),
                 showCover: showCover, coverURL: showCover ? LibraryStore.shared.coverArtURL(id: s.coverId ?? s.albumId, size: 120) : nil) {
            PlayerStore.shared.play(songs: songs, startAt: i)
        }
    }
}

private struct SongsList: View {
    @ObservedObject private var cache = LibraryCache.shared
    var body: some View {
        let songs = cache.songs
        LazyVStack(spacing: 0) {
            if songs.isEmpty {
                if cache.failed.contains("songs") {
                    EmptyState(kind: .network, action: ("重试", { Task { await cache.ensure("songs", force: true) } }))
                } else if cache.done.contains("songs") {
                    EmptyState(kind: .songs)
                } else {
                    DoodleLoader()
                    ForEach(0..<8, id: \.self) { _ in SkeletonRow() }
                }
            }
            ForEach(Array(songs.enumerated()), id: \.element.id) { i, _ in
                SongRowPlay(songs: songs, i: i)
                    .id(songs[i].id)
                    .onAppear { if i >= songs.count - 20 { Task { await cache.more("songs") } } }
            }
            if cache.paging.contains("songs") { InlineLoader() }
        }
    }
}

private struct AlbumsGrid: View {
    static let types: [(String, String)] = [("newest", "最新"), ("alphabeticalByName", "A–Z"), ("recent", "最近播放"), ("frequent", "常听"), ("random", "随机")]
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @ObservedObject private var library = LibraryStore.shared
    private var type: String { state.albumSort }
    @ObservedObject private var cache = LibraryCache.shared
    private var key: String { "albums:\(type)" }
    var body: some View {
        let albums = cache.albumList(type)
        VStack(alignment: .leading, spacing: 12) {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 14) {
                    ForEach(Self.types, id: \.0) { t in
                        Button { state.albumSort = t.0 } label: {
                            Text(t.1).font(.system(size: 13, weight: type == t.0 ? .bold : .regular))
                                .foregroundStyle(type == t.0 ? Theme.fg(scheme) : Theme.secondary(scheme))
                        }.buttonStyle(.plain)
                    }
                }.padding(.horizontal, Theme.margin)
            }
            let w = YMLayout.tile()
            if (cache.albumBase[key] ?? 0) > 0 {
                Button { Task { await cache.less(key) } } label: {
                    Label("从第 \(cache.albumBase[key] ?? 0) 张起 · 载入前面", systemImage: "chevron.up")
                        .font(.system(size: 12)).foregroundStyle(Theme.secondary(scheme))
                }.buttonStyle(.plain).padding(.horizontal, Theme.margin)
            }
            if albums.isEmpty {
                if cache.failed.contains(key) {
                    EmptyState(kind: .network, action: ("重试", { Task { await cache.ensure(key, force: true) } }))
                } else if cache.done.contains(key) {
                    EmptyState(kind: .albums)
                } else {
                    DoodleLoader()
                    LazyVGrid(columns: YMLayout.grid(), spacing: 20) {
                        ForEach(0..<6, id: \.self) { _ in SkeletonTile(size: w) }
                    }.padding(.horizontal, Theme.margin)
                }
            }
            LazyVGrid(columns: YMLayout.grid(), spacing: 20) {
                ForEach(Array(albums.enumerated()), id: \.element.id) { i, a in
                    Button { state.path.append(.ndAlbum(a)) } label: {
                        VStack(alignment: .leading, spacing: 6) {
                            RemoteCover(url: library.coverURL(a), size: w)
                            Text(a.name).font(.system(size: 14, weight: .semibold)).lineLimit(1).foregroundStyle(Theme.fg(scheme))
                            Text(a.artist).font(.system(size: 12)).foregroundStyle(Theme.secondary(scheme)).lineLimit(1)
                        }
                    }.buttonStyle(.plain)
                    .albumMenu(a)
                    .id(a.id)
                    .onAppear { if i >= albums.count - 20 { Task { await cache.more(key) } } }
                }
            }.padding(.horizontal, Theme.margin)
            if cache.paging.contains(key) { InlineLoader() }
            if !albums.isEmpty {
                Text("已加载 \(albums.count) 张专辑").font(.system(size: 12)).foregroundStyle(Theme.secondary(scheme))
                    .padding(.horizontal, Theme.margin)
            }
        }
    }
}

private struct ArtistsList: View {
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @ObservedObject private var cache = LibraryCache.shared
    @ObservedObject private var library = LibraryStore.shared
    var body: some View {
        let artists = cache.artists
        LazyVStack(alignment: .leading, spacing: 0) {
            if artists.isEmpty {
                if cache.failed.contains("artists") {
                    EmptyState(kind: .network, action: ("重试", { Task { await cache.ensure("artists", force: true) } }))
                } else if cache.done.contains("artists") {
                    EmptyState(kind: .artists)
                } else {
                    DoodleLoader(text: "正在点名歌手…")
                    ForEach(0..<8, id: \.self) { _ in SkeletonRow() }
                }
            }
            ForEach(library.artistIndex?.artists ?? artists) { ar in
                Button { state.path.append(.ndArtist(ar)) } label: {
                    HStack(spacing: 12) {
                        ArtistAvatar(artist: ar, size: 44)
                        Text(ar.name).font(.system(size: 16)).lineLimit(1)
                        Spacer()
                        if let c = ar.albumCount { Text("\(c) 张").font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme)) }
                        YMIcon(glyph: .chevron, size: 11, weight: 1.5).font(.system(size: 12)).foregroundStyle(Theme.secondary(scheme))
                    }
                    .foregroundStyle(Theme.fg(scheme))
                    .padding(.horizontal, Theme.margin).frame(height: 60).contentShape(Rectangle())
                }.buttonStyle(.plain)
                .artistMenu(ar)
                .id(ar.id)
            }
        }
        .task(id: artists.count) { library.buildArtistIndex(artists) }
        .onChange(of: artists) { _, a in library.buildArtistIndex(a) }
    }
}

private struct PlaylistsList: View {
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @ObservedObject private var events = PlaylistEvents.shared
    @State private var creating = false
    @State private var name = ""
    var body: some View {
        LazyVStack(alignment: .leading, spacing: 0) {
            Button { name = ""; creating = true } label: { Label("新建歌单", systemImage: "plus") }
                .buttonStyle(SecondaryButtonStyle())
                .padding(.horizontal, Theme.margin).padding(.bottom, 8)
            if let lists = events.lists {
                if lists.isEmpty { EmptyState(kind: .playlists) }
                ForEach(lists) { pl in
                    Button { state.path.append(.ndPlaylist(pl)) } label: {
                        HStack(spacing: 12) {
                            PlaylistCover(playlist: pl, size: 52, corner: 8)
                            VStack(alignment: .leading, spacing: 2) {
                                Text(pl.name).font(.system(size: 16)).lineLimit(1)
                                Text([ "\(pl.songCount ?? 0) 首", pl.owner ?? "", pl.isPublic == true ? "公开" : ""].filter { !$0.isEmpty }.joined(separator: " · "))
                                    .font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme))
                            }
                            Spacer()
                            YMIcon(glyph: .chevron, size: 11, weight: 1.5).font(.system(size: 12)).foregroundStyle(Theme.secondary(scheme))
                        }
                        .foregroundStyle(Theme.fg(scheme))
                        .padding(.horizontal, Theme.margin).frame(height: 68).contentShape(Rectangle())
                    }.buttonStyle(.plain)
                    .playlistMenu(pl)
                }
            } else {
                DoodleLoader(text: "正在整理歌单…")
                ForEach(0..<5, id: \.self) { _ in SkeletonRow() }
            }
        }
        .alert("新建歌单", isPresented: $creating) {
            TextField("歌单名称", text: $name)
            Button("取消", role: .cancel) {}
            Button("创建") {
                let n = name.trimmingCharacters(in: .whitespaces)
                guard !n.isEmpty else { return }
                Task {
                    if let p = try? await NavidromeClient.shared.createPlaylist(name: n, songIds: []) {
                        PlaylistEvents.updated(p); state.path.append(.ndPlaylist(p))
                    } else { SheetCenter.shared.say("创建失败") }
                }
            }
        }
    }
}

private struct StarredList: View {
    @ObservedObject private var stars = StarStore.shared
    var body: some View {
        LazyVStack(spacing: 0) {
            if stars.loaded && stars.songs.isEmpty { EmptyState(kind: .starred) }
            ForEach(stars.songs.indices, id: \.self) { i in SongRowPlay(songs: stars.songs, i: i) }
        }
    }
}
