import SwiftUI

/// 曲库 — full Navidrome library: 歌曲 / 专辑 / 歌手 / 歌单 / 收藏. Lives in the 典藏 tab when a server is connected.
struct LibraryView: View {
    enum Seg: String, CaseIterable { case songs = "歌曲", albums = "专辑", artists = "歌手", playlists = "歌单", starred = "收藏" }
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @ObservedObject private var library = LibraryStore.shared

    var body: some View {
        ScrollView(showsIndicators: false) {
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
                            Button { withAnimation(.snappy) { state.librarySeg = s } } label: {
                                Text(label(s)).font(.system(size: 14, weight: .semibold))
                                    .padding(.horizontal, 16).frame(height: 34)
                                    .foregroundStyle(seg == s ? Theme.base(scheme) : Theme.fg(scheme))
                                    .background(Capsule().fill(seg == s ? Theme.fg(scheme) : Theme.fill(scheme)))
                            }.buttonStyle(.plain)
                        }
                    }.padding(.horizontal, Theme.margin)
                }.padding(.top, 16)

                Group {
                    switch state.librarySeg {
                    case .songs: SongsList()
                    case .albums: AlbumsGrid()
                    case .artists: ArtistsList()
                    case .playlists: PlaylistsList()
                    case .starred: StarredList()
                    }
                }.padding(.top, 12)
                Color.clear.frame(height: 170)
            }
        }
        .background(Theme.base(scheme))
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
    @EnvironmentObject var state: AppState
    @ObservedObject private var player = PlayerStore.shared
    @ObservedObject private var stars = StarStore.shared
    var body: some View {
        let s = songs[i]
        TrackRow(index: i + 1, title: s.title, subtitle: [s.artist, s.album ?? ""].filter { !$0.isEmpty }.joined(separator: " · "),
                 trailing: s.durationLabel, playing: player.song?.id == s.id,
                 accessory: AnyView(StarButton(on: stars.isStarred(song: s.id), size: 15) { stars.toggle(s) })) {
            PlayerStore.shared.play(songs: songs, startAt: i)
        }
    }
}

private struct SongsList: View {
    @State private var songs: [NDSong] = []
    @State private var done = false
    @State private var loading = false
    var body: some View {
        LazyVStack(spacing: 0) {
            ForEach(songs.indices, id: \.self) { i in
                SongRowPlay(songs: songs, i: i)
                    .onAppear { if i >= songs.count - 10 { Task { await more() } } }
            }
            if loading { ProgressView().padding() }
            if done && songs.isEmpty { EmptyState(kind: .songs, action: ("重新载入", { done = false; Task { await more() } })) }
        }
        .task { if songs.isEmpty { await more() } }
    }
    private func more() async {
        guard !loading, !done else { return }
        loading = true; defer { loading = false }
        let page = (try? await NavidromeClient.shared.songs(offset: songs.count, count: 60)) ?? []
        if page.isEmpty { done = true } else { songs += page }
    }
}

private struct AlbumsGrid: View {
    static let types: [(String, String)] = [("newest", "最新"), ("alphabeticalByName", "A–Z"), ("recent", "最近播放"), ("frequent", "常听"), ("random", "随机")]
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @ObservedObject private var library = LibraryStore.shared
    private var type: String { state.albumSort }
    @State private var albums: [NDAlbum] = []
    @State private var done = false
    @State private var loading = false
    var body: some View {
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
            let w = (UIScreen.main.bounds.width - Theme.margin * 2 - 16) / 2
            LazyVGrid(columns: [GridItem(.flexible(), spacing: 16), GridItem(.flexible())], spacing: 20) {
                ForEach(albums.indices, id: \.self) { i in
                    let a = albums[i]
                    Button { state.path.append(.ndAlbum(a)) } label: {
                        VStack(alignment: .leading, spacing: 6) {
                            RemoteCover(url: library.coverURL(a), size: w)
                            Text(a.name).font(.system(size: 14, weight: .semibold)).lineLimit(1).foregroundStyle(Theme.fg(scheme))
                            Text(a.artist).font(.system(size: 12)).foregroundStyle(Theme.secondary(scheme)).lineLimit(1)
                        }
                    }.buttonStyle(.plain)
                    .onAppear { if i >= albums.count - 6 { Task { await more() } } }
                }
            }.padding(.horizontal, Theme.margin)
            if loading { ProgressView().frame(maxWidth: .infinity) }
            if done && albums.isEmpty { EmptyState(kind: .albums) }
            Text("已加载 \(albums.count) 张专辑").font(.system(size: 12)).foregroundStyle(Theme.secondary(scheme))
                .padding(.horizontal, Theme.margin)
        }
        .task(id: type) {
            done = false
            albums = []; await more()
        }
    }
    private func more() async {
        guard !loading, !done else { return }
        loading = true; defer { loading = false }
        let t = type
        let page = (try? await NavidromeClient.shared.albumList(type: t, offset: t == "random" ? 0 : albums.count, size: t == "random" ? 60 : 200)) ?? []
        guard t == type else { return }
        if page.count < 200 || t == "random" { done = true }
        albums += page.filter { p in !albums.contains(where: { $0.id == p.id }) }
    }
}

private struct ArtistsList: View {
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @State private var artists: [NDArtist] = []
    @State private var loaded = false
    var body: some View {
        LazyVStack(alignment: .leading, spacing: 0) {
            if loaded && artists.isEmpty { EmptyState(kind: .artists) }
            ForEach(artists) { ar in
                Button { state.path.append(.ndArtist(ar)) } label: {
                    HStack(spacing: 12) {
                        ArtistAvatar(artist: ar, size: 44)
                        Text(ar.name).font(.system(size: 16)).lineLimit(1)
                        Spacer()
                        if let c = ar.albumCount { Text("\(c) 张").font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme)) }
                        Image(systemName: "chevron.right").font(.system(size: 12)).foregroundStyle(Theme.secondary(scheme))
                    }
                    .foregroundStyle(Theme.fg(scheme))
                    .padding(.horizontal, Theme.margin).frame(height: 60).contentShape(Rectangle())
                }.buttonStyle(.plain)
            }
        }
        .task { if !loaded { artists = (try? await NavidromeClient.shared.artists()) ?? []; loaded = true } }
    }
}

private struct PlaylistsList: View {
    @Environment(\.colorScheme) var scheme
    @ObservedObject private var library = LibraryStore.shared
    @State private var lists: [NDPlaylist] = []
    @State private var loaded = false
    @State private var open: String?
    @State private var songs: [String: [NDSong]] = [:]
    var body: some View {
        LazyVStack(alignment: .leading, spacing: 0) {
            if loaded && lists.isEmpty { EmptyState(kind: .playlists) }
            ForEach(lists) { pl in
                Button {
                    withAnimation(.snappy) { open = open == pl.id ? nil : pl.id }
                    if songs[pl.id] == nil { Task { songs[pl.id] = (try? await NavidromeClient.shared.playlist(id: pl.id)) ?? [] } }
                } label: {
                    HStack(spacing: 12) {
                        RemoteCover(url: library.coverArtURL(id: pl.coverId, size: 120), size: 48, corner: 8)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(pl.name).font(.system(size: 16)).lineLimit(1)
                            Text("\(pl.songCount ?? 0) 首").font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme))
                        }
                        Spacer()
                    }
                    .foregroundStyle(Theme.fg(scheme))
                    .padding(.horizontal, Theme.margin).frame(height: 64).contentShape(Rectangle())
                }.buttonStyle(.plain)
                if open == pl.id, let s = songs[pl.id] {
                    ForEach(s.indices, id: \.self) { i in SongRowPlay(songs: s, i: i) }
                }
            }
        }
        .task { if !loaded { lists = (try? await NavidromeClient.shared.playlists()) ?? []; loaded = true } }
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
