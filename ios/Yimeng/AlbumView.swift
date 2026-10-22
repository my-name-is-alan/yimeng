import SwiftUI

struct AlbumView: View {
    let album: Album
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @State private var revealed = false
    @State private var expanded = false
    var tracks: [Track] { expanded ? Mock.tracks + Mock.moreTracks : Mock.tracks }
    var body: some View {
        let header = Color.header([album.cover], progress: 0, dark: scheme == .dark)
        ZStack(alignment: .top) {
            ScrollView(showsIndicators: false) {
                VStack(spacing: 0) {
                    HStack {
                        Button { if !state.path.isEmpty { state.path.removeLast() } } label: { CircleIconButton(icon: "chevron.left") }
                        Spacer()
                        CircleIconButton(icon: "ellipsis")
                    }.buttonStyle(.plain).padding(.horizontal, Theme.margin).padding(.top, 8).motionIn(0, from: .top)
                    Image(album.cover).resizable().scaledToFill().frame(width: 240, height: 240)
                        .clipShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
                        .shadow(color: .black.opacity(0.4), radius: 24, y: 12).padding(.top, 16).motionIn(0.05, from: .scale)
                    VStack(spacing: 4) {
                        Text(album.title).font(.system(size: 22, weight: .bold))
                        Text(album.artist).font(.system(size: 18)).opacity(0.65)
                        Text("专辑 · \(album.year) · FLAC 24bit").font(.system(size: 13)).opacity(0.55)
                    }.foregroundStyle(header).multilineTextAlignment(.center).padding(.top, 24).padding(.horizontal, Theme.margin).motionIn(0.12)
                    HStack(spacing: 12) {
                        Button { withAnimation(.ui) { state.nowPlaying = true } } label: { Label("播放", systemImage: "play.fill") }.buttonStyle(PrimaryButtonStyle())
                        Button {} label: { Label("随机播放", systemImage: "shuffle") }.buttonStyle(SecondaryButtonStyle())
                    }.padding(.horizontal, Theme.margin).padding(.top, 24).motionIn(0.18)
                    VStack(spacing: 0) {
                        ForEach(Array(tracks.enumerated()), id: \.element.id) { i, t in
                            TrackRow(index: i + 1, title: t.title, trailing: t.duration, playing: i == 0)
                                .opacity(revealed ? 1 : 0)
                                .offset(y: revealed ? 0 : 16)
                                .animation(.snappy(duration: 0.45).delay(revealed ? 0.22 + Double(i % 6) * 0.05 : 0), value: revealed)
                                .transition(.asymmetric(insertion: .opacity.combined(with: .offset(y: 12)), removal: .opacity))
                        }
                        Button { withAnimation(.smooth(duration: 0.45)) { expanded.toggle() } } label: {
                            HStack(spacing: 6) { Text(expanded ? "收起" : "显示全部 10 首"); Image(systemName: "chevron.down").rotationEffect(.degrees(expanded ? 180 : 0)) }
                                .font(.system(size: 15, weight: .medium)).foregroundStyle(Theme.secondary(scheme))
                                .frame(maxWidth: .infinity, minHeight: 44)
                        }.buttonStyle(.plain).padding(.top, 4)
                    }.padding(.top, 24)
                    .onAppear { revealed = true }
                    Text("2023年5月20日 · 6 首歌曲 · 26 分钟").font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme))
                        .frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, Theme.margin).padding(.top, 16)
                    Color.clear.frame(height: 170)
                }
            }
        }
        .background(alignment: .top) { ZStack(alignment: .top) { Theme.base(scheme); DiffusedBackdrop(images: [album.cover]).motionIn(0, from: .fade) }.ignoresSafeArea() }
    }
}

/// Navidrome album detail — loads `getAlbum.view` tracks + cover art; play via stream.view / AVPlayer.
struct NDAlbumView: View {
    let album: NDAlbum
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @ObservedObject private var library = LibraryStore.shared
    @ObservedObject private var player = PlayerStore.shared
    @ObservedObject private var stars = StarStore.shared
    @State private var songs: [NDSong] = []
    @State private var detail: NDAlbum?
    @State private var loading = true
    @State private var error: String?
    @State private var revealed = false
    @State private var pull: CGFloat = 0

    private var shown: NDAlbum { detail ?? album }

    var body: some View {
        ZStack(alignment: .top) {
            ScrollView(showsIndicators: false) {
                VStack(spacing: 0) {
                    ScrollPullReader(space: "albumScroll", pull: $pull)
                    HStack {
                        Button { if !state.path.isEmpty { state.path.removeLast() } } label: { CircleIconButton(icon: "chevron.left") }
                        Spacer()
                        StarButton(on: stars.isStarred(album: shown.id), size: 18) { stars.toggle(shown) }
                            .background(Circle().fill(Theme.fill(scheme)).frame(width: 44, height: 44))
                            .frame(width: 44, height: 44)
                    }.buttonStyle(.plain).padding(.horizontal, Theme.margin).padding(.top, 8).motionIn(0, from: .top)

                    RemoteCover(url: library.coverURL(shown, size: 600), size: 240, corner: 16)
                        .shadow(color: .black.opacity(0.4), radius: 24, y: 12)
                        .padding(.top, 16).motionIn(0.05, from: .scale)

                    VStack(spacing: 4) {
                        Text(shown.name).font(.system(size: 22, weight: .bold))
                        Button {
                            if let aid = shown.artistId { state.path.append(.ndArtist(NDArtist(id: aid, name: shown.artist, albumCount: nil))) }
                        } label: { Text(shown.artist).font(.system(size: 18)).opacity(0.65) }.buttonStyle(.plain)
                        let meta: String = {
                            var parts = ["专辑"]
                            if let y = shown.year { parts.append(String(y)) }
                            parts.append("\(shown.songCount ?? songs.count) 首")
                            return parts.joined(separator: " · ")
                        }()
                        Text(meta)
                            .font(.system(size: 13)).opacity(0.55)
                    }
                    .foregroundStyle(Theme.fg(scheme))
                    .multilineTextAlignment(.center)
                    .padding(.top, 24).padding(.horizontal, Theme.margin).motionIn(0.12)

                    HStack(spacing: 12) {
                        Button { playAll(shuffle: false) } label: { Label("播放", systemImage: "play.fill") }
                            .buttonStyle(PrimaryButtonStyle())
                            .disabled(songs.isEmpty)
                        Button { playAll(shuffle: true) } label: { Label("随机播放", systemImage: "shuffle") }
                            .buttonStyle(SecondaryButtonStyle())
                            .disabled(songs.isEmpty)
                    }.padding(.horizontal, Theme.margin).padding(.top, 24).motionIn(0.18)

                    if loading {
                        ProgressView().padding(.top, 40)
                    } else if let error {
                        Text(error).font(.system(size: 14)).foregroundStyle(.red).padding(.top, 24)
                    } else {
                        VStack(spacing: 0) {
                            ForEach(Array(songs.enumerated()), id: \.element.id) { i, t in
                                let playing = player.song?.id == t.id
                                TrackRow(index: i + 1, title: t.title, trailing: t.durationLabel, playing: playing,
                                         accessory: AnyView(StarButton(on: stars.isStarred(song: t.id), size: 15) { stars.toggle(t) })) {
                                    PlayerStore.shared.playAlbum(shown, songs: songs, startAt: i)
                                    withAnimation(.ui) { state.nowPlaying = true }
                                }
                                .songMenu(t)
                                .opacity(revealed ? 1 : 0)
                                .offset(y: revealed ? 0 : 16)
                                .animation(.snappy(duration: 0.45).delay(revealed ? 0.12 + Double(i % 8) * 0.04 : 0), value: revealed)
                            }
                        }.padding(.top, 24)
                        .onAppear { revealed = true }
                        Text("\(songs.count) 首歌曲")
                            .font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme))
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.horizontal, Theme.margin).padding(.top, 16)
                    }
                    Color.clear.frame(height: 170)
                }
                .background(alignment: .top) {
                    // Cover → ×2.8, blur 80 → soft colour field behind the header, fading to the page colour.
                    CoverBloom(url: library.coverURL(shown, size: 300), height: 700, pull: pull)
                        .offset(y: -180)
                }
            }
            .coordinateSpace(name: "albumScroll")
        }
        .background(Theme.base(scheme).ignoresSafeArea())
        .task(id: album.id) { await load() }
    }

    private func load() async {
        loading = true
        error = nil
        do {
            let (a, s) = try await library.fetchAlbum(id: album.id)
            detail = a
            songs = s
            revealed = false
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.05) { revealed = true }
        } catch {
            self.error = error.localizedDescription
        }
        loading = false
    }

    private func playAll(shuffle: Bool) {
        guard !songs.isEmpty else { return }
        var q = songs
        var start = 0
        if shuffle {
            q.shuffle()
            start = 0
        }
        PlayerStore.shared.playAlbum(shown, songs: q, startAt: start)
        withAnimation(.ui) { state.nowPlaying = true }
    }
}

struct ArtistView: View {
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    var body: some View {
        ScrollView(showsIndicators: false) {
            VStack(spacing: 0) {
                ZStack(alignment: .bottomLeading) {
                    Image("ym_artist_coldplay").resizable().scaledToFill().frame(height: 360).frame(maxWidth: .infinity).clipped().motionIn(0, from: .fade)
                    LinearGradient(stops: [.init(color: .black.opacity(0.55), location: 0), .init(color: .clear, location: 0.35),
                                           .init(color: .clear, location: 0.6), .init(color: Theme.base(scheme), location: 1)], startPoint: .top, endPoint: .bottom)
                    HStack(alignment: .bottom) {
                        Text("Coldplay").font(.system(size: 22, weight: .bold)).foregroundStyle(Theme.fg(scheme))
                        Spacer()
                        Button { withAnimation(.ui) { state.nowPlaying = true } } label: { PlayCircle() }.buttonStyle(.plain)
                    }.padding(.horizontal, Theme.margin).padding(.bottom, 16).motionIn(0.1)
                }
                .overlay(alignment: .topLeading) {
                    Button { if !state.path.isEmpty { state.path.removeLast() } } label: { CircleIconButton(icon: "chevron.left", dark: true) }
                        .buttonStyle(.plain).padding(.leading, Theme.margin).padding(.top, 60)
                }
                SectionHeader(title: "热门歌曲").motionIn(0.16)
                ForEach(Array(Mock.coldplayTop.enumerated()), id: \.offset) { i, x in TrackRow(index: 0, title: x.1, subtitle: x.2, art: x.0).motionIn(stagger(i, base: 0.2)) }
                Group { SectionHeader(title: "专辑"); AlbumRow(albums: Mock.coldplayAlbums) }.motionIn(0.45)
                Color.clear.frame(height: 170)
            }
        }
        .ignoresSafeArea(edges: .top)
    }
}

struct LibrarySearchView: View {
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @ObservedObject private var library = LibraryStore.shared
    @State private var q = ""
    @State private var albums: [NDAlbum] = []
    @State private var artists: [NDArtist] = []
    @State private var songs: [NDSong] = []
    @State private var searching = false
    let cats: [(String, Color)] = [("周杰伦", Color(red: 0.48, green: 0.36, blue: 1)), ("独立摇滚", Color(red: 0.91, green: 0.36, blue: 1)),
                                   ("古风", Color(red: 0.23, green: 0.51, blue: 0.96)), ("电子", Color(red: 0.08, green: 0.72, blue: 0.65))]
    private var liveOnly: Bool { library.useLiveOnly }

    var body: some View {
        ScrollView(showsIndicators: false) {
            VStack(alignment: .leading, spacing: 0) {
                Text("搜索").font(.system(size: 28, weight: .bold)).padding(.horizontal, Theme.margin).padding(.top, 16).motionIn(0)
                HStack(spacing: 8) {
                    Image(systemName: "magnifyingglass").foregroundStyle(Theme.secondary(scheme))
                    TextField(liveOnly ? "搜索曲库…" : "艺人、专辑、歌曲", text: $q)
                        .font(.system(size: 16))
                        .onSubmit { Task { await runSearch() } }
                    if searching { ProgressView().scaleEffect(0.8) }
                }
                .padding(.horizontal, 12).frame(height: 44).background(Capsule().fill(Theme.fill(scheme)))
                .padding(.horizontal, Theme.margin).padding(.top, 16).motionIn(0.06)
                .onChange(of: q) { _, new in
                    guard liveOnly else { return }
                    Task {
                        try? await Task.sleep(for: .milliseconds(350))
                        if q == new { await runSearch() }
                    }
                }

                if liveOnly {
                    if !albums.isEmpty {
                        SectionHeader(title: "专辑", more: false)
                        RemoteAlbumRow(albums: albums)
                    }
                    if !songs.isEmpty {
                        SectionHeader(title: "歌曲", more: false)
                        ForEach(Array(songs.prefix(30).enumerated()), id: \.element.id) { i, s in
                            TrackRow(index: i + 1, title: s.title, subtitle: s.artist, trailing: s.durationLabel) {
                                PlayerStore.shared.play(songs: songs, startAt: i,
                                                        coverURL: library.coverArtURL(id: s.coverId ?? s.albumId, size: 600))
                                withAnimation(.ui) { state.nowPlaying = true }
                            }
                        }
                    }
                    if !artists.isEmpty {
                        SectionHeader(title: "艺人", more: false)
                        ForEach(artists.prefix(20)) { ar in
                            HStack(spacing: 12) {
                                ArtistAvatar(artist: ar, size: 40)
                                Text(ar.name).font(.system(size: 16))
                                Spacer()
                                if let c = ar.albumCount {
                                    Text("\(c) 张专辑").font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme))
                                }
                            }
                            .padding(.horizontal, Theme.margin).padding(.vertical, 8)
                            .contentShape(Rectangle())
                            .onTapGesture { state.path.append(.ndArtist(ar)) }
                        }
                    }
                    if albums.isEmpty && songs.isEmpty && artists.isEmpty && !q.isEmpty && !searching {
                        EmptyState(kind: .search)
                    }
                    if q.isEmpty && !library.albums.isEmpty {
                        SectionHeader(title: "曲库 · 新入库", more: false)
                        RemoteAlbumRow(albums: Array(library.albums.prefix(16)))
                        Text(library.status)
                            .font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme))
                            .padding(.horizontal, Theme.margin).padding(.top, 8)
                    }
                } else {
                    SectionHeader(title: "我的曲库", more: false).motionIn(0.12)
                    ForEach(Array([("music.note", "歌曲", "4,812"), ("square.stack", "专辑", "386"), ("person", "艺人", "142"), ("music.note.list", "歌单", "24")].enumerated()), id: \.offset) { i, x in let (ic, n, c) = x;
                        HStack(spacing: 16) {
                            Image(systemName: ic).font(.system(size: 18)).frame(width: 24)
                            HStack {
                                Text(n).font(.system(size: 17)); Spacer()
                                Text(c).font(.system(size: 15)).foregroundStyle(Theme.secondary(scheme))
                                Image(systemName: "chevron.right").font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme))
                            }.frame(maxHeight: .infinity).overlay(alignment: .bottom) { Rectangle().fill(Theme.divider(scheme)).frame(height: 0.5) }
                        }.frame(height: 52).padding(.horizontal, Theme.margin).motionIn(stagger(i, base: 0.16))
                    }
                    SectionHeader(title: "浏览分类", more: false).motionIn(0.4)
                    LazyVGrid(columns: [GridItem(.flexible(), spacing: 16), GridItem(.flexible())], spacing: 16) {
                        ForEach(Array(cats.enumerated()), id: \.offset) { i, x in let (n, c) = x;
                            Text(n).font(.system(size: 16, weight: .bold)).foregroundStyle(.white).padding(12)
                                .frame(maxWidth: .infinity, minHeight: 96, alignment: .bottomLeading)
                                .background(RoundedRectangle(cornerRadius: 12, style: .continuous).fill(LinearGradient(colors: [c, c.opacity(0.55)], startPoint: .topLeading, endPoint: .bottomTrailing)))
                                .motionIn(stagger(i, base: 0.45), from: .scale).scrollReveal()
                        }
                    }.padding(.horizontal, Theme.margin)
                }
                Color.clear.frame(height: 170)
            }
        }
        .task {
            guard liveOnly, albums.isEmpty, q.isEmpty else { return }
            albums = Array(library.albums.prefix(16))
        }
    }

    private func runSearch() async {
        guard liveOnly else { return }
        let query = q.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !query.isEmpty else {
            albums = Array(library.albums.prefix(16))
            songs = []; artists = []
            return
        }
        searching = true
        defer { searching = false }
        do {
            let r = try await library.search(query: query)
            albums = r.albums
            artists = r.artists
            songs = r.songs
        } catch {
            albums = []; artists = []; songs = []
        }
    }
}
