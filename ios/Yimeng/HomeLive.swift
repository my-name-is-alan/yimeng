import SwiftUI
import Combine

/// 首页 with a connected Navidrome: hero carousel + real sections. Sections with no data are hidden.
struct HomeLive: View {
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @ObservedObject private var library = LibraryStore.shared
    @AppStorage("ym.nickname") private var nickname = ""
    @ObservedObject private var home = HomeStore.shared
    private var banner: [NDAlbum] { home.data.banner }
    private var newest: [NDAlbum] { home.data.newest }
    private var recent: [NDAlbum] { home.data.recent }
    private var frequent: [NDAlbum] { home.data.frequent }
    private var random: [NDAlbum] { home.data.random }
    private var artists: [NDArtist] { home.data.artists }
    private var playlists: [NDPlaylist] { home.data.playlists }
    private var genres: [NDGenre] { home.data.genres }
    @State private var bannerState = BannerState()

    private var bannerURLs: [URL?] { banner.map { library.coverURL($0, size: 600) } }

    var body: some View {
        GeometryReader { geo in
            let w = geo.size.width - Theme.margin * 2
            let p = bannerState.position
            ScrollView(showsIndicators: false) {
                VStack(alignment: .leading, spacing: 0) {
                    HStack(alignment: .firstTextBaseline) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("首页").font(.system(size: 28, weight: .bold))
                            Text(greeting).font(.system(size: 13)).opacity(0.7)
                        }
                        Spacer()
                    }
                    .foregroundStyle(.white)
                    .padding(.horizontal, Theme.margin).padding(.top, 16).motionIn(0)

                    if !banner.isEmpty {
                        CurtainCarousel(albums: banner, width: w, st: $bannerState)
                            .padding(.horizontal, Theme.margin).padding(.top, 16).motionIn(0.08, from: .scale)
                        BannerDots(count: banner.count, active: bannerState.active % max(banner.count, 1))
                            .frame(maxWidth: .infinity).padding(.top, 12).motionIn(0.14, from: .fade)
                    } else if home.data.isEmpty {
                        // True first run (no cache): a soft doodle, never grey skeleton blocks.
                        FirstRunDoodle().padding(.top, 40)
                    }

                    albumSection("最近添加", newest, sort: "newest", i: 2)
                    albumSection("最近播放", recent, sort: "recent", i: 3)
                    albumSection("常听", frequent, sort: "frequent", i: 4)
                    albumSection("随机推荐", random, sort: "random", i: 5)
                    if !artists.isEmpty {
                        header("热门歌手") { state.librarySeg = .artists; state.tab = .library }.motionIn(stagger(6))
                        ScrollView(.horizontal, showsIndicators: false) {
                            HStack(spacing: 16) {
                                ForEach(artists) { ar in
                                    VStack(spacing: 8) {
                                        ArtistAvatar(artist: ar, size: 84)
                                        Text(ar.name).font(.system(size: 13, weight: .medium)).lineLimit(1)
                                        Text("\(ar.albumCount ?? 0) 张专辑").font(.system(size: 11)).foregroundStyle(Theme.secondary(scheme))
                                    }
                                    .frame(width: 90)
                                    .onTapGesture { state.path.append(.ndArtist(ar)) }
                                }
                            }.padding(.horizontal, Theme.margin)
                        }.motionIn(stagger(6))
                    }
                    if !playlists.isEmpty {
                        header("歌单") { state.librarySeg = .playlists; state.tab = .library }.motionIn(stagger(7))
                        ScrollView(.horizontal, showsIndicators: false) {
                            HStack(spacing: 16) {
                                ForEach(playlists) { pl in
                                    VStack(alignment: .leading, spacing: 4) {
                                        PlaylistCover(playlist: pl, size: 132, corner: 12)
                                        Text(pl.name).font(.system(size: 14, weight: .medium)).lineLimit(1)
                                        Text("\(pl.songCount ?? 0) 首").font(.system(size: 12)).foregroundStyle(Theme.secondary(scheme))
                                    }
                                    .frame(width: 132)
                                    .contentShape(Rectangle())
                                    .onTapGesture { state.path.append(.ndPlaylist(pl)) }
                                }
                            }.padding(.horizontal, Theme.margin)
                        }.motionIn(stagger(7))
                    }
                    if !genres.isEmpty {
                        header("按流派", more: false) {}.motionIn(stagger(8))
                        ScrollView(.horizontal, showsIndicators: false) {
                            HStack(spacing: 10) {
                                ForEach(Array(genres.enumerated()), id: \.element.id) { i, g in
                                    let c = Self.genreColors[i % Self.genreColors.count]
                                    Button {
                                        Task { if let s = try? await NavidromeClient.shared.songsByGenre(g.name), !s.isEmpty { PlayerStore.shared.play(songs: s.shuffled()) } }
                                    } label: {
                                        VStack(alignment: .leading, spacing: 2) {
                                            Text(g.name).font(.system(size: 15, weight: .bold)).lineLimit(1)
                                            Text("\(g.songCount) 首").font(.system(size: 11)).opacity(0.75)
                                        }
                                        .foregroundStyle(.white).padding(12)
                                        .frame(width: 128, height: 72, alignment: .bottomLeading)
                                        .background(RoundedRectangle(cornerRadius: 12, style: .continuous).fill(c))
                                    }.buttonStyle(.plain)
                                }
                            }.padding(.horizontal, Theme.margin)
                        }.motionIn(stagger(8))
                    }
                    Text(library.status).font(.system(size: 12)).foregroundStyle(Theme.secondary(scheme))
                        .padding(.horizontal, Theme.margin).padding(.top, 24)
                    Color.clear.frame(height: 190)
                }
            }
            .background(alignment: .top) { RemoteBackdrop(urls: bannerURLs, progress: p).ignoresSafeArea() }
        }
        .background(Theme.base(scheme))
        .refreshable { await home.refresh(force: true) }
        .task {
            await home.refresh()
            #if DEBUG
            let args = ProcessInfo.processInfo.arguments
            if let i = args.firstIndex(of: "-open"), i + 1 < args.count, args[i + 1].hasPrefix("playlist:") {
                let id = String(args[i + 1].dropFirst(9))
                state.path.append(.ndPlaylist(NDPlaylist(id: id, name: "歌单", songCount: nil, coverId: nil)))
            }
            if args.contains("-selftest-playlists") { Task { @MainActor in await PlaylistSelfTest.run() } }
            if let i = args.firstIndex(of: "-open"), i + 1 < args.count, let a = home.data.banner.first {
                if args[i + 1] == "album" { state.path.append(.ndAlbum(a)) }
                if args[i + 1] == "np", let (al, s) = try? await library.fetchAlbum(id: a.id), !s.isEmpty {
                    PlayerStore.shared.playAlbum(al, songs: s); state.nowPlaying = true
                }
            }
            #endif
        }
    }

    static let genreColors: [Color] = [Color(hex: 0x7a1cff), Color(hex: 0xe85cff), Color(hex: 0x3b82f6), Color(hex: 0x14b8a6), Color(hex: 0xff7a1a), Color(hex: 0xff2d7a)]

    private var greeting: String {
        let h = Calendar.current.component(.hour, from: Date())
        let g = h < 6 ? "夜深了" : h < 12 ? "早上好" : h < 18 ? "下午好" : "晚上好"
        return nickname.isEmpty ? g : "\(g)，\(nickname)"
    }

    @ViewBuilder func albumSection(_ title: String, _ albums: [NDAlbum], sort: String, i: Int) -> some View {
        if !albums.isEmpty {
            Group {
                header(title) { state.albumSort = sort; state.librarySeg = .albums; state.tab = .library }
                RemoteAlbumRow(albums: albums, size: 140)
            }.motionIn(stagger(i))
        }
    }

    func header(_ title: String, more: Bool = true, action: @escaping () -> Void) -> some View {
        HStack(alignment: .firstTextBaseline) {
            Text(title).font(.system(size: 20, weight: .bold))
            Spacer()
            if more {
                Button(action: action) {
                    HStack(spacing: 2) { Text("更多"); Image(systemName: "chevron.right").font(.system(size: 11, weight: .semibold)) }
                        .font(.system(size: 14)).foregroundStyle(Theme.secondary(scheme))
                }.buttonStyle(.plain)
            }
        }
        .padding(.horizontal, Theme.margin).padding(.top, 28).padding(.bottom, 12)
    }
}

/// Locally stored avatar (picked in 我的), falling back to the bundled one.
struct AvatarView: View {
    var size: CGFloat = 36
    @AppStorage("ym.avatarRev") private var rev = 0
    var body: some View {
        Group {
            if let img = AvatarStore.load() { Image(uiImage: img).resizable().scaledToFill() }
            else { Image("ym_avatar").resizable().scaledToFill() }
        }
        .id(rev)
        .frame(width: size, height: size).clipShape(Circle())
    }
}

enum AvatarStore {
    static var url: URL { FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0].appendingPathComponent("avatar.jpg") }
    static func load() -> UIImage? { UIImage(contentsOfFile: url.path) }
    static func save(_ data: Data) {
        try? FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        if let img = UIImage(data: data), let jpg = img.jpegData(compressionQuality: 0.85) { try? jpg.write(to: url) }
        UserDefaults.standard.set(UserDefaults.standard.integer(forKey: "ym.avatarRev") + 1, forKey: "ym.avatarRev")
    }
}


/// First-launch placeholder: moon + stars drifting in, no skeletons.
struct FirstRunDoodle: View {
    @State private var on = false
    var body: some View {
        VStack(spacing: 12) {
            ZStack {
                Crayon(shape: MoonShape()).frame(width: 56, height: 56)
                Crayon(shape: StarShape(), color: Theme.lime, width: 1.8).frame(width: 14, height: 14).offset(x: 40, y: -26).opacity(on ? 1 : 0.3)
                Crayon(shape: StarShape(), width: 1.6).frame(width: 9, height: 9).offset(x: -38, y: 22).opacity(on ? 0.4 : 1)
            }
            Text("正在把曲库搬进梦里…").font(.system(size: 14)).opacity(0.7)
        }
        .foregroundStyle(.white)
        .frame(maxWidth: .infinity)
        .opacity(on ? 1 : 0.5).offset(y: on ? 0 : 6)
        .onAppear { withAnimation(.easeInOut(duration: 1.6).repeatForever(autoreverses: true)) { on = true } }
    }
}
