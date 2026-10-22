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
    @State private var page: Double = 0
    @State private var drag: CGFloat = 0
    @State private var dragging = false
    @State private var lastInteraction = Date.distantPast
    let timer = Timer.publish(every: 4, on: .main, in: .common).autoconnect()

    private var bannerURLs: [URL?] { banner.map { library.coverURL($0, size: 600) } }

    var body: some View {
        GeometryReader { geo in
            let w = geo.size.width - Theme.margin * 2
            let p = page - Double(drag / max(w, 1))
            ScrollView(showsIndicators: false) {
                VStack(alignment: .leading, spacing: 0) {
                    HStack(alignment: .firstTextBaseline) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("首页").font(.system(size: 28, weight: .bold))
                            Text(greeting).font(.system(size: 13)).opacity(0.7)
                        }
                        Spacer()
                        AvatarView(size: 36).onTapGesture { state.tab = .me }
                    }
                    .foregroundStyle(.white)
                    .padding(.horizontal, Theme.margin).padding(.top, 16).motionIn(0)

                    if !banner.isEmpty {
                        carousel(width: w, progress: p).padding(.top, 16).motionIn(0.08, from: .scale)
                        HStack(spacing: 6) {
                            ForEach(0..<banner.count, id: \.self) { i in
                                let wt = DiffusedBackdrop.weight(i, p, banner.count)
                                Capsule().fill(Color.white).frame(width: 6 + 12 * wt, height: 6).opacity(0.4 + 0.6 * wt)
                            }
                        }.frame(maxWidth: .infinity).padding(.top, 12).motionIn(0.14, from: .fade)
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
                                        RemoteCover(url: library.coverArtURL(id: pl.coverId, size: 300), size: 132)
                                        Text(pl.name).font(.system(size: 14, weight: .medium)).lineLimit(1)
                                        Text("\(pl.songCount ?? 0) 首").font(.system(size: 12)).foregroundStyle(Theme.secondary(scheme))
                                    }
                                    .frame(width: 132)
                                    .onTapGesture { Task { if let s = try? await NavidromeClient.shared.playlist(id: pl.id), !s.isEmpty { PlayerStore.shared.play(songs: s) } } }
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
        .task { await home.refresh() }
        .onReceive(timer) { _ in
            guard banner.count > 1, !dragging, Date().timeIntervalSince(lastInteraction) > 3 else { return }
            withAnimation(.smooth(duration: 0.6)) { page = page.rounded() + 1 }
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

    func carousel(width w: CGFloat, progress p: Double) -> some View {
        let n = banner.count
        let base = Int(floor(p))
        return ZStack(alignment: .leading) {
            ForEach((base - 1)...(base + 2), id: \.self) { v in
                let a = banner[((v % n) + n) % n]
                let rel = Double(v) - p
                ZStack(alignment: .bottomLeading) {
                    CoverImage(url: library.coverURL(a, size: 800))
                        .frame(width: w * 1.2, height: 200)
                        .offset(x: -CGFloat(rel) * w * 0.18)
                        .frame(width: w, height: 200).clipped()
                    LinearGradient(colors: [.clear, .black.opacity(0.15), .black.opacity(0.75)], startPoint: .top, endPoint: .bottom)
                    HStack(alignment: .bottom) {
                        VStack(alignment: .leading, spacing: 4) {
                            Text("为你推荐").font(.system(size: 12, weight: .semibold)).foregroundStyle(.white.opacity(0.7))
                            Text(a.name).font(.system(size: 20, weight: .bold)).foregroundStyle(.white).lineLimit(1)
                            Text([a.artist, a.year.map(String.init) ?? ""].filter { !$0.isEmpty }.joined(separator: " · "))
                                .font(.system(size: 13)).foregroundStyle(.white.opacity(0.7)).lineLimit(1)
                        }
                        Spacer()
                        Button {
                            Task { if let (al, s) = try? await library.fetchAlbum(id: a.id), !s.isEmpty { PlayerStore.shared.playAlbum(al, songs: s) } }
                        } label: { PlayCircle() }.buttonStyle(.plain)
                    }.padding(20)
                }
                .frame(width: w, height: 200)
                .offset(x: CGFloat(rel) * w)
                .onTapGesture { state.path.append(.ndAlbum(a)) }
            }
        }
        .frame(width: w, height: 200, alignment: .leading)
        .clipShape(RoundedRectangle(cornerRadius: 20, style: .continuous))
        .contentShape(Rectangle())
        .padding(.horizontal, Theme.margin)
        .highPriorityGesture(DragGesture(minimumDistance: 8)
            .onChanged { v in dragging = true; drag = v.translation.width }
            .onEnded { v in
                let projected = page - Double(v.predictedEndTranslation.width / w)
                let current = page - Double(v.translation.width / w)
                let target = max(page.rounded() - 1, min(page.rounded() + 1, projected.rounded()))
                page = current; drag = 0
                withAnimation(.interpolatingSpring(stiffness: 220, damping: 28)) { page = target }
                dragging = false; lastInteraction = Date()
            })
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
