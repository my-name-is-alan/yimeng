import SwiftUI

/// Session cache for genre pages (albums paged via getAlbumList2 type=byGenre, songs via getSongsByGenre).
@MainActor
final class GenreCache: ObservableObject {
    static let shared = GenreCache()
    static let page = 40
    @Published private(set) var albums: [String: [NDAlbum]] = [:]
    @Published private(set) var songs: [String: [NDSong]] = [:]
    @Published private(set) var done: Set<String> = []
    @Published private(set) var paging: Set<String> = []
    @Published private(set) var all: [NDGenre] = []

    func load(_ g: String) async {
        guard albums[g] == nil else { return }
        async let a = NavidromeClient.shared.albumsByGenre(g, offset: 0, size: Self.page)
        async let s = NavidromeClient.shared.songsByGenre(g, count: 100)
        albums[g] = (try? await a) ?? []
        songs[g] = (try? await s) ?? []
        if (albums[g]?.count ?? 0) < Self.page { done.insert(g) }
    }
    func more(_ g: String) async {
        guard !done.contains(g), !paging.contains(g), let cur = albums[g] else { return }
        paging.insert(g); defer { paging.remove(g) }
        guard let p = try? await NavidromeClient.shared.albumsByGenre(g, offset: cur.count, size: Self.page) else { return }
        let ids = Set(cur.map(\.id))
        albums[g] = cur + p.filter { !ids.contains($0.id) }
        if p.count < Self.page { done.insert(g) }
    }
    func loadAll() async {
        guard all.isEmpty else { return }
        all = (try? await NavidromeClient.shared.genres()) ?? []
    }
}

/// Genre page: cover-bloom backdrop from its albums, play-all / shuffle, songs preview, paged album grid.
struct GenreView: View {
    let genre: NDGenre
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @ObservedObject private var cache = GenreCache.shared
    @ObservedObject private var library = LibraryStore.shared
    private var albums: [NDAlbum] { cache.albums[genre.name] ?? [] }
    private var songs: [NDSong] { cache.songs[genre.name] ?? [] }

    var body: some View {
        let w = (UIScreen.main.bounds.width - Theme.margin * 2 - 16) / 2
        ScrollView(showsIndicators: false) {
            VStack(alignment: .leading, spacing: 0) {
                Button { if !state.path.isEmpty { state.path.removeLast() } } label: { CircleIconButton(icon: "chevron.left", dark: scheme == .dark) }
                    .buttonStyle(.plain).padding(.horizontal, Theme.margin).padding(.top, 8)
                VStack(alignment: .leading, spacing: 4) {
                    Text("流派").font(.system(size: 12)).tracking(2).opacity(0.6)
                    Text(genre.name).font(.system(size: 32, weight: .bold)).lineLimit(2)
                    Text("\(genre.albumCount) 张专辑 · \(genre.songCount) 首").font(.system(size: 13)).opacity(0.7)
                }
                .foregroundStyle(Theme.fg(scheme))
                .padding(.horizontal, Theme.margin).padding(.top, 70).motionIn(0.02)

                HStack(spacing: 12) {
                    Button { PlayerStore.shared.play(songs: songs) } label: { Label("播放全部", systemImage: "play.fill") }.buttonStyle(PrimaryButtonStyle())
                    Button { PlayerStore.shared.play(songs: songs.shuffled()) } label: { Label("随机播放", systemImage: "shuffle") }.buttonStyle(SecondaryButtonStyle())
                }
                .disabled(songs.isEmpty)
                .padding(.horizontal, Theme.margin).padding(.top, 20).motionIn(0.06)

                if !songs.isEmpty {
                    Text("歌曲").font(.system(size: 20, weight: .bold)).padding(.horizontal, Theme.margin).padding(.top, 28).padding(.bottom, 6)
                    ForEach(0..<min(6, songs.count), id: \.self) { i in SongRowPlay(songs: songs, i: i) }
                }
                Text("专辑").font(.system(size: 20, weight: .bold)).padding(.horizontal, Theme.margin).padding(.top, 28).padding(.bottom, 12)
                if albums.isEmpty && !cache.done.contains(genre.name) {
                    DoodleLoader(text: "正在翻这类唱片…")
                }
                LazyVGrid(columns: [GridItem(.flexible(), spacing: 16), GridItem(.flexible())], spacing: 20) {
                    ForEach(Array(albums.enumerated()), id: \.element.id) { i, a in
                        Button { state.path.append(.ndAlbum(a)) } label: {
                            VStack(alignment: .leading, spacing: 6) {
                                RemoteCover(url: library.coverURL(a), size: w)
                                Text(a.name).font(.system(size: 14, weight: .semibold)).lineLimit(1).foregroundStyle(Theme.fg(scheme))
                                Text(a.artist).font(.system(size: 12)).foregroundStyle(Theme.secondary(scheme)).lineLimit(1)
                            }
                        }.buttonStyle(.plain)
                        .onAppear { if i >= albums.count - 10 { Task { await cache.more(genre.name) } } }
                    }
                }.padding(.horizontal, Theme.margin)
                if cache.paging.contains(genre.name) { InlineLoader() }
                Color.clear.frame(height: 190)
            }
        }
        .background(alignment: .top) {
            RemoteBackdrop(urls: albums.prefix(1).map { library.coverURL($0, size: 600) }, progress: 0, height: 640).ignoresSafeArea()
        }
        .background(Theme.base(scheme))
        .task { await cache.load(genre.name) }
    }
}

/// All genres, quiet colored tiles.
struct GenreListView: View {
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @ObservedObject private var cache = GenreCache.shared
    var body: some View {
        ScrollView(showsIndicators: false) {
            VStack(alignment: .leading, spacing: 0) {
                Button { if !state.path.isEmpty { state.path.removeLast() } } label: { CircleIconButton(icon: "chevron.left", dark: scheme == .dark) }
                    .buttonStyle(.plain).padding(.horizontal, Theme.margin).padding(.top, 8)
                Text("全部流派").font(.system(size: 28, weight: .bold)).padding(.horizontal, Theme.margin).padding(.top, 16)
                Text("\(cache.all.count) 种").font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme)).padding(.horizontal, Theme.margin)
                if cache.all.isEmpty { DoodleLoader(text: "正在分类…").padding(.top, 20) }
                LazyVGrid(columns: [GridItem(.flexible(), spacing: 10), GridItem(.flexible())], spacing: 10) {
                    ForEach(Array(cache.all.enumerated()), id: \.element.id) { i, g in
                        Button { state.path.append(.genre(g)) } label: {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(g.name).font(.system(size: 15, weight: .bold)).lineLimit(1)
                                Text("\(g.albumCount) 张 · \(g.songCount) 首").font(.system(size: 11)).opacity(0.75)
                            }
                            .foregroundStyle(.white).padding(12)
                            .frame(maxWidth: .infinity, minHeight: 72, alignment: .bottomLeading)
                            .background(RoundedRectangle(cornerRadius: 12, style: .continuous).fill(HomeLive.genreColors[i % HomeLive.genreColors.count]))
                        }.buttonStyle(.plain)
                    }
                }
                .padding(.horizontal, Theme.margin).padding(.top, 18)
                Color.clear.frame(height: 190)
            }
        }
        .background(Theme.base(scheme))
        .task { await cache.loadAll() }
    }
}
