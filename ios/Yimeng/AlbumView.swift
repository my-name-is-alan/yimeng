import SwiftUI

struct AlbumView: View {
    let album: Album
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    var body: some View {
        let header = Color.header([album.cover], progress: 0, dark: scheme == .dark)
        ZStack(alignment: .top) {
            DiffusedBackdrop(images: [album.cover]).ignoresSafeArea()
            ScrollView(showsIndicators: false) {
                VStack(spacing: 0) {
                    HStack {
                        Button { state.path.removeLast() } label: { CircleIconButton(icon: "chevron.left") }
                        Spacer()
                        CircleIconButton(icon: "ellipsis")
                    }.buttonStyle(.plain).padding(.horizontal, Theme.margin).padding(.top, 8)
                    Image(album.cover).resizable().scaledToFill().frame(width: 240, height: 240)
                        .clipShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
                        .shadow(color: .black.opacity(0.4), radius: 24, y: 12).padding(.top, 16)
                    VStack(spacing: 4) {
                        Text(album.title).font(.system(size: 22, weight: .bold))
                        Text(album.artist).font(.system(size: 18)).opacity(0.65)
                        Text("专辑 · \(album.year) · FLAC 24bit").font(.system(size: 13)).opacity(0.55)
                    }.foregroundStyle(header).multilineTextAlignment(.center).padding(.top, 24).padding(.horizontal, Theme.margin)
                    HStack(spacing: 12) {
                        Button { state.nowPlaying = true } label: { Label("播放", systemImage: "play.fill") }.buttonStyle(PrimaryButtonStyle())
                        Button {} label: { Label("随机播放", systemImage: "shuffle") }.buttonStyle(SecondaryButtonStyle())
                    }.padding(.horizontal, Theme.margin).padding(.top, 24)
                    VStack(spacing: 0) {
                        ForEach(Array(Mock.tracks.enumerated()), id: \.offset) { i, t in
                            TrackRow(index: i + 1, title: t.title, trailing: t.duration, playing: i == 0)
                        }
                    }.padding(.top, 24)
                    Text("2023年5月20日 · 6 首歌曲 · 26 分钟").font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme))
                        .frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, Theme.margin).padding(.top, 16)
                    Color.clear.frame(height: 170)
                }
            }
        }
    }
}

struct ArtistView: View {
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    var body: some View {
        ScrollView(showsIndicators: false) {
            VStack(spacing: 0) {
                ZStack(alignment: .bottomLeading) {
                    Image("ym_artist_coldplay").resizable().scaledToFill().frame(height: 360).frame(maxWidth: .infinity).clipped()
                    LinearGradient(stops: [.init(color: .black.opacity(0.55), location: 0), .init(color: .clear, location: 0.35),
                                           .init(color: .clear, location: 0.6), .init(color: Theme.base(scheme), location: 1)], startPoint: .top, endPoint: .bottom)
                    HStack(alignment: .bottom) {
                        Text("Coldplay").font(.system(size: 22, weight: .bold)).foregroundStyle(Theme.fg(scheme))
                        Spacer()
                        Button { state.nowPlaying = true } label: { PlayCircle() }.buttonStyle(.plain)
                    }.padding(.horizontal, Theme.margin).padding(.bottom, 16)
                }
                .overlay(alignment: .topLeading) {
                    Button { state.path.removeLast() } label: { CircleIconButton(icon: "chevron.left", dark: true) }
                        .buttonStyle(.plain).padding(.leading, Theme.margin).padding(.top, 60)
                }
                SectionHeader(title: "热门歌曲")
                ForEach(Mock.coldplayTop, id: \.1) { art, t, s in TrackRow(index: 0, title: t, subtitle: s, art: art) }
                SectionHeader(title: "专辑")
                AlbumRow(albums: Mock.coldplayAlbums) { a in state.path.append(.album(a)) }
                Color.clear.frame(height: 170)
            }
        }
        .ignoresSafeArea(edges: .top)
    }
}

struct LibrarySearchView: View {
    @Environment(\.colorScheme) var scheme
    @State private var q = ""
    let cats: [(String, Color)] = [("周杰伦", Color(red: 0.48, green: 0.36, blue: 1)), ("独立摇滚", Color(red: 0.91, green: 0.36, blue: 1)),
                                   ("古风", Color(red: 0.23, green: 0.51, blue: 0.96)), ("电子", Color(red: 0.08, green: 0.72, blue: 0.65))]
    var body: some View {
        ScrollView(showsIndicators: false) {
            VStack(alignment: .leading, spacing: 0) {
                Text("搜索").font(.system(size: 28, weight: .bold)).padding(.horizontal, Theme.margin).padding(.top, 16)
                HStack(spacing: 8) {
                    Image(systemName: "magnifyingglass").foregroundStyle(Theme.secondary(scheme))
                    TextField("艺人、专辑、歌曲", text: $q).font(.system(size: 16))
                }
                .padding(.horizontal, 12).frame(height: 44).background(Capsule().fill(Theme.fill(scheme)))
                .padding(.horizontal, Theme.margin).padding(.top, 16)
                SectionHeader(title: "我的曲库", more: false)
                ForEach([("music.note", "歌曲", "4,812"), ("square.stack", "专辑", "386"), ("person", "艺人", "142"), ("music.note.list", "歌单", "24")], id: \.1) { ic, n, c in
                    HStack(spacing: 16) {
                        Image(systemName: ic).font(.system(size: 18)).frame(width: 24)
                        HStack {
                            Text(n).font(.system(size: 17)); Spacer()
                            Text(c).font(.system(size: 15)).foregroundStyle(Theme.secondary(scheme))
                            Image(systemName: "chevron.right").font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme))
                        }.frame(maxHeight: .infinity).overlay(alignment: .bottom) { Rectangle().fill(Theme.divider(scheme)).frame(height: 0.5) }
                    }.frame(height: 52).padding(.horizontal, Theme.margin)
                }
                SectionHeader(title: "浏览分类", more: false)
                LazyVGrid(columns: [GridItem(.flexible(), spacing: 16), GridItem(.flexible())], spacing: 16) {
                    ForEach(cats, id: \.0) { n, c in
                        Text(n).font(.system(size: 16, weight: .bold)).foregroundStyle(.white).padding(12)
                            .frame(maxWidth: .infinity, minHeight: 96, alignment: .bottomLeading)
                            .background(RoundedRectangle(cornerRadius: 12, style: .continuous).fill(LinearGradient(colors: [c, c.opacity(0.55)], startPoint: .topLeading, endPoint: .bottomTrailing)))
                    }
                }.padding(.horizontal, Theme.margin)
                Color.clear.frame(height: 170)
            }
        }
    }
}
