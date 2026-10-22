import SwiftUI

/// Real artist page: hero image + diffusion, collapsed bio, top songs, albums grid, similar artists.
struct ArtistDetailView: View {
    let artist: NDArtist
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @ObservedObject private var library = LibraryStore.shared
    @ObservedObject private var stars = StarStore.shared
    @State private var detail: NavidromeClient.ArtistDetail?
    @State private var fallbackTop: [NDSong] = []
    @State private var bioOpen = false
    @State private var failed = false

    private var heroURL: URL? { library.coverArtURL(id: artist.coverId ?? "ar-\(artist.id)", size: 800) }
    private var top: [NDSong] { (detail?.top.isEmpty == false ? detail!.top : fallbackTop) }

    var body: some View {
        ScrollView(showsIndicators: false) {
            VStack(alignment: .leading, spacing: 0) {
                ZStack(alignment: .bottomLeading) {
                    ZStack {
                        ArtistAvatar(artist: artist, size: 420).clipShape(Rectangle()).scaleEffect(1.1)
                    }
                    .frame(height: 360).frame(maxWidth: .infinity).clipped()
                    LinearGradient(stops: [.init(color: .black.opacity(0.45), location: 0), .init(color: .clear, location: 0.35),
                                           .init(color: .clear, location: 0.6), .init(color: Theme.base(scheme), location: 1)], startPoint: .top, endPoint: .bottom)
                    HStack(alignment: .bottom) {
                        VStack(alignment: .leading, spacing: 4) {
                            Text(artist.name).font(.system(size: 26, weight: .bold)).lineLimit(2)
                            Text("\(detail?.albums.count ?? artist.albumCount ?? 0) 张专辑").font(.system(size: 13)).opacity(0.7)
                        }.foregroundStyle(Theme.fg(scheme))
                        Spacer()
                        StarButton(on: stars.isStarred(artist: artist.id), size: 20) { stars.toggle(artist) }
                    }
                    .padding(.horizontal, Theme.margin).padding(.bottom, 12)
                }
                .overlay(alignment: .topLeading) {
                    Button { if !state.path.isEmpty { state.path.removeLast() } } label: { CircleIconButton(icon: "chevron.left", dark: true) }
                        .buttonStyle(.plain).padding(.leading, Theme.margin).padding(.top, 60)
                }
                .motionIn(0, from: .fade)

                HStack(spacing: 12) {
                    Button { play(shuffle: false) } label: { Label("播放全部", systemImage: "play.fill") }.buttonStyle(PrimaryButtonStyle())
                    Button { play(shuffle: true) } label: { Label("随机播放", systemImage: "shuffle") }.buttonStyle(SecondaryButtonStyle())
                }
                .disabled(top.isEmpty)
                .padding(.horizontal, Theme.margin).padding(.top, 8).motionIn(0.08)

                if let bio = detail?.bio, !bio.isEmpty {
                    Button { withAnimation(.smooth) { bioOpen.toggle() } } label: {
                        VStack(alignment: .leading, spacing: 6) {
                            Text(bio).font(.system(size: 14)).lineLimit(bioOpen ? nil : 3).multilineTextAlignment(.leading)
                            Text(bioOpen ? "收起" : "展开").font(.system(size: 13, weight: .semibold)).foregroundStyle(Theme.secondary(scheme))
                        }
                        .foregroundStyle(Theme.fg(scheme).opacity(0.85))
                        .padding(16).frame(maxWidth: .infinity, alignment: .leading)
                        .background(RoundedRectangle(cornerRadius: 16, style: .continuous).fill(Theme.fill(scheme)))
                    }.buttonStyle(.plain).padding(.horizontal, Theme.margin).padding(.top, 20).motionIn(0.12)
                }

                if !top.isEmpty {
                    SectionHeader(title: "热门歌曲", more: false).motionIn(0.16)
                    ForEach(Array(top.prefix(10).enumerated()), id: \.element.id) { i, _ in
                        SongRowPlay(songs: Array(top.prefix(10)), i: i, showCover: false).motionIn(stagger(i, base: 0.18))
                    }
                }
                if let albums = detail?.albums, !albums.isEmpty {
                    SectionHeader(title: "专辑", more: false)
                    let w = YMLayout.tile()
                    LazyVGrid(columns: YMLayout.grid(), spacing: 20) {
                        ForEach(albums) { a in
                            Button { state.path.append(.ndAlbum(a)) } label: {
                                VStack(alignment: .leading, spacing: 6) {
                                    RemoteCover(url: library.coverURL(a), size: w)
                                    Text(a.name).font(.system(size: 14, weight: .semibold)).lineLimit(1).foregroundStyle(Theme.fg(scheme))
                                    Text(a.year.map(String.init) ?? "").font(.system(size: 12)).foregroundStyle(Theme.secondary(scheme))
                                }
                            }.buttonStyle(.plain)
                            .albumMenu(a)
                        }
                    }.padding(.horizontal, Theme.margin)
                }
                if let sim = detail?.similar, !sim.isEmpty {
                    SectionHeader(title: "相似歌手", more: false)
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 16) {
                            ForEach(sim) { s in
                                Button { state.path.append(.ndArtist(s)) } label: {
                                    VStack(spacing: 6) {
                                        ArtistAvatar(artist: s, size: 76)
                                        Text(s.name).font(.system(size: 12)).lineLimit(1).frame(width: 84).foregroundStyle(Theme.fg(scheme))
                                    }
                                }.buttonStyle(.plain)
                                .artistMenu(s)
                            }
                        }.padding(.horizontal, Theme.margin)
                    }
                }
                if failed && detail == nil { EmptyState(kind: .network, action: ("重试", { Task { await load() } })) }
                Color.clear.frame(height: 190)
            }
        }
        .background(alignment: .top) { CoverBloom(url: heroURL, height: 700).ignoresSafeArea() }
        .background(YMBackdrop())
        .ignoresSafeArea(edges: .top)
        .task(id: artist.id) { await load() }
    }

    private func load() async {
        failed = false
        do {
            let d = try await NavidromeClient.shared.artistDetail(artist)
            detail = d
            if d.top.isEmpty, let first = d.albums.first {
                // getTopSongs needs last.fm data; fall back to the artist's albums.
                var out: [NDSong] = []
                for a in d.albums.prefix(3) { if let (_, s) = try? await library.fetchAlbum(id: a.id) { out += s } }
                fallbackTop = out.isEmpty ? [] : out
                _ = first
            }
        } catch { failed = true }
    }

    private func play(shuffle: Bool) {
        guard !top.isEmpty else { return }
        PlayerStore.shared.play(songs: shuffle ? top.shuffled() : top)
    }
}
