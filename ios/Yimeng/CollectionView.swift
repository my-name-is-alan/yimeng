import SwiftUI

/// 典藏: when Navidrome is connected, show live albums only (no DreamMock mix).
/// Offline demo keeps the 3D DreamMock card stack.
struct CollectionView: View {
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @ObservedObject private var library = LibraryStore.shared
    @State private var focus: Double = 0
    @State private var drag: CGFloat = 0
    let items = DreamMock.collection
    private var liveOnly: Bool { library.useLiveOnly }

    var body: some View {
        #if DEBUG
        if liveOnly { LiveStack() } else { dreamBody }
        #else
        LiveStack()
        #endif
    }

    private var liveBody: some View {
        ScrollView(showsIndicators: false) {
            VStack(alignment: .leading, spacing: 0) {
                HStack(alignment: .firstTextBaseline) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("典藏").font(.system(size: 28, weight: .bold))
                        Text(library.status.isEmpty ? "Navidrome" : library.status)
                            .font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme))
                    }
                    Spacer()
                }
                .padding(.horizontal, Theme.margin).padding(.top, 16).motionIn(0)

                if library.loading {
                    ProgressView().frame(maxWidth: .infinity).padding(.top, 40)
                } else if library.albums.isEmpty {
                    Text("暂无专辑").foregroundStyle(Theme.secondary(scheme))
                        .padding(.horizontal, Theme.margin).padding(.top, 24)
                } else {
                    LazyVGrid(columns: YMLayout.grid(), spacing: 20) {
                        ForEach(library.albums) { a in
                            Button { state.path.append(.ndAlbum(a)) } label: {
                                VStack(alignment: .leading, spacing: 6) {
                                    RemoteCover(url: library.coverURL(a), size: YMLayout.tile(), corner: 12)
                                    Text(a.name).font(.system(size: 14, weight: .semibold)).lineLimit(1).foregroundStyle(Theme.fg(scheme))
                                    Text(a.artist).font(.system(size: 12)).foregroundStyle(Theme.secondary(scheme)).lineLimit(1)
                                }
                            }.buttonStyle(.plain)
                            .albumMenu(a)
                        }
                    }
                    .padding(.horizontal, Theme.margin).padding(.top, 20)
                }
                Color.clear.frame(height: 170)
            }
        }
        .background(YMBackdrop())
    }

    private var dreamBody: some View {
        let f = max(0, min(Double(items.count - 1), focus - Double(drag / 220)))
        let cur = items[Int(f.rounded())]
        return ZStack {
            DreamField(track: cur).animation(.smooth(duration: 0.6), value: cur.id)
            VStack(alignment: .leading, spacing: 0) {
                HStack(alignment: .firstTextBaseline) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("典藏").font(.system(size: 28, weight: .bold))
                        Text("\(items.count) 张专辑 · 本地无损").font(.system(size: 13)).opacity(0.7)
                    }
                    Spacer()
                    Image(systemName: "arrow.up.arrow.down").font(.system(size: 16, weight: .semibold))
                        .frame(width: 36, height: 36).background(Circle().fill(Color.white.opacity(0.14)))
                }
                .padding(.horizontal, Theme.margin).padding(.top, 16).motionIn(0)

                ZStack {
                    ForEach(Array(items.enumerated()).reversed(), id: \.offset) { i, t in
                        card(t, rel: Double(i) - f)
                    }
                }
                .frame(maxWidth: .infinity).frame(height: 420)
                .contentShape(Rectangle())
                .gesture(DragGesture()
                    .onChanged { drag = -$0.translation.height }
                    .onEnded { v in
                        let target = (focus - Double(-v.predictedEndTranslation.height / 220)).rounded()
                        focus = f; drag = 0
                        withAnimation(.interpolatingSpring(stiffness: 200, damping: 26)) { focus = max(0, min(Double(items.count - 1), target)) }
                    })
                .motionIn(0.08, from: .scale)

                VStack(spacing: 6) {
                    HStack(spacing: 8) { sticker("NEW", rotation: -6); sticker("♻︎ 循环 42 次", rotation: 4) }
                    Text(cur.title).font(.system(size: 22, weight: .bold)).padding(.top, 8)
                    Text("\(cur.artist) · 2024").font(.system(size: 14)).opacity(0.7)
                    HStack(spacing: 12) {
                        Button { state.track = cur; withAnimation(.ui) { state.nowPlaying = true } } label: { Label("播放", systemImage: "play.fill") }
                            .buttonStyle(PrimaryButtonStyle()).environment(\.colorScheme, .dark).frame(width: 120)
                        Button {} label: { Text("打开") }.buttonStyle(SecondaryButtonStyle()).environment(\.colorScheme, .dark).frame(width: 100)
                    }.padding(.top, 12)
                }
                .frame(maxWidth: .infinity).motionIn(0.2)
                Spacer()
            }
            .foregroundStyle(.white)
        }
    }

    func card(_ t: DreamTrack, rel: Double) -> some View {
        let isFocus = abs(rel) < 0.5
        let lift = max(0, 1 - abs(rel))
        return Image(t.cover).resizable().scaledToFill()
            .frame(width: 250, height: 250)
            .clipShape(RoundedRectangle(cornerRadius: 18, style: .continuous))
            .shadow(color: t.blobs[0].opacity(0.75 * lift), radius: 30 * lift)
            .shadow(color: .black.opacity(0.35), radius: 20, y: 14)
            .rotation3DEffect(.degrees(rel > 0 ? -38 : 0), axis: (x: 1, y: 0, z: 0), anchor: .top, perspective: 0.6)
            .scaleEffect(1 - min(0.3, abs(rel) * 0.08) + 0.04 * lift)
            .offset(y: rel > 0 ? -CGFloat(rel) * 34 + 40 : -CGFloat(rel) * 260 + 40)
            .opacity(rel < -1 ? 0 : (rel > 4 ? 0 : 1))
            .zIndex(isFocus ? 100 : -abs(rel))
    }

    func sticker(_ s: String, rotation: Double) -> some View {
        Text(s).font(.system(size: 11, weight: .heavy)).foregroundStyle(Color(white: 0.1))
            .padding(.horizontal, 10).padding(.vertical, 4)
            .background(Capsule().fill(Color.white))
            .rotationEffect(.degrees(rotation))
    }
}


/// 典藏 with Navidrome = the user's favourites: 3D stack of starred albums + starred songs.
struct LiveStack: View {
    enum Seg: String, CaseIterable { case albums = "专辑", songs = "歌曲" }
    @EnvironmentObject var state: AppState
    @ObservedObject private var library = LibraryStore.shared
    @ObservedObject private var stars = StarStore.shared
    @ObservedObject private var diff = RemoteDiffusion.shared
    @State private var seg: Seg = .albums
    @State private var focus: Double = 0
    @State private var drag: CGFloat = 0
    static let glows: [Color] = [Color(hex: 0xff5fa8), Color(hex: 0xc8f542), Color(hex: 0x7a1cff), Color(hex: 0xff9a5a), Color(hex: 0x33d17a)]

    private var items: [NDAlbum] { stars.albums }

    var body: some View {
        let n = items.count
        let f = n == 0 ? 0 : max(0, min(Double(n - 1), focus - Double(drag / 220)))
        let ci = n == 0 ? 0 : Int(f.rounded())
        let cur = n > 0 ? items[min(ci, n - 1)] : nil
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .firstTextBaseline) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("典藏").font(.system(size: 28, weight: .bold))
                    Text("\(stars.albums.count) 张专辑 · \(stars.songs.count) 首歌曲").font(.system(size: 13)).opacity(0.7)
                }
                Spacer()
                HStack(spacing: 4) {
                    ForEach(Seg.allCases, id: \.self) { s in
                        Button { withAnimation(.snappy) { seg = s } } label: {
                            Text(s.rawValue).font(.system(size: 13, weight: .semibold))
                                .padding(.horizontal, 12).frame(height: 30)
                                .foregroundStyle(seg == s ? Color(white: 0.07) : .white)
                                .background(Capsule().fill(seg == s ? Color.white : Color.white.opacity(0.12)))
                        }.buttonStyle(.plain)
                    }
                }
            }
            .padding(.horizontal, Theme.margin).padding(.top, 16).motionIn(0)

            if seg == .albums {
                if n == 0 {
                    if stars.loaded { EmptyState(kind: .collection, action: ("去曲库逛逛", { state.librarySeg = .albums; state.tab = .library })).padding(.top, 60) }
                    Spacer()
                } else {
                    ZStack {
                        // Stable ids (album id) + window — cards are never re-created while swiping.
                        ForEach(Array(items.enumerated())[max(0, ci - 2)..<min(n, ci + 6)], id: \.element.id) { i, a in
                            card(a, i: i, rel: Double(i) - f)
                        }
                    }
                    .frame(maxWidth: .infinity).frame(height: 420)
                    .contentShape(Rectangle())
                    .onTapGesture { if let cur { state.path.append(.ndAlbum(cur)) } }
                    .gesture(DragGesture()
                        .onChanged { drag = -$0.translation.height }
                        .onEnded { v in
                            let target = (focus - Double(-v.predictedEndTranslation.height / 220)).rounded()
                            focus = f; drag = 0
                            withAnimation(.interpolatingSpring(stiffness: 200, damping: 26)) { focus = max(0, min(Double(n - 1), target)) }
                        })
                    .motionIn(0.08, from: .scale)

                    if let cur {
                        VStack(spacing: 6) {
                            Text(cur.name).font(.system(size: 22, weight: .bold)).lineLimit(1).padding(.horizontal, Theme.margin)
                            Text([cur.artist, cur.year.map(String.init) ?? ""].filter { !$0.isEmpty }.joined(separator: " · "))
                                .font(.system(size: 14)).opacity(0.7).lineLimit(1)
                            HStack(spacing: 12) {
                                Button {
                                    Task { if let (a, s) = try? await library.fetchAlbum(id: cur.id), !s.isEmpty { PlayerStore.shared.playAlbum(a, songs: s) } }
                                } label: { Label("播放", systemImage: "play.fill") }
                                    .buttonStyle(PrimaryButtonStyle()).environment(\.colorScheme, .dark).frame(width: 120)
                                Button { state.path.append(.ndAlbum(cur)) } label: { Text("打开") }
                                    .buttonStyle(SecondaryButtonStyle()).environment(\.colorScheme, .dark).frame(width: 100)
                            }.padding(.top, 10)
                        }
                        .frame(maxWidth: .infinity).motionIn(0.2)
                    }
                    Spacer()
                }
            } else {
                ScrollView(showsIndicators: false) {
                    LazyVStack(spacing: 0) {
                        if stars.songs.isEmpty { EmptyState(kind: .starred) }
                        ForEach(stars.songs.indices, id: \.self) { i in SongRowPlay(songs: stars.songs, i: i) }
                        Color.clear.frame(height: 190)
                    }.padding(.top, 12)
                }
                .environment(\.colorScheme, .dark)
            }
        }
        .foregroundStyle(.white)
        .background {
            ZStack(alignment: .top) {
                Color.black
                // Cross-fade backdrop by continuous stack progress (no per-frame image swaps).
                RemoteBackdrop(urls: items.map { library.coverURL($0, size: 600) }, progress: f, height: 900)
            }.ignoresSafeArea()
        }
        .task { if !stars.loaded { await stars.load() } }
        .onChange(of: items.count) { _, c in focus = min(focus, Double(max(0, c - 1))) }
    }

    func card(_ a: NDAlbum, i: Int, rel: Double) -> some View {
        let isFocus = abs(rel) < 0.5
        let lift = max(0, 1 - abs(rel))
        let glow = diff.entry(library.coverURL(a, size: 600))?.color ?? Self.glows[i % Self.glows.count]
        return CoverImage(url: library.coverURL(a, size: 600))
            .frame(width: 250, height: 250)
            .clipShape(RoundedRectangle(cornerRadius: 18, style: .continuous))
            .shadow(color: glow.opacity(0.8 * lift), radius: 32 * lift)
            .shadow(color: .black.opacity(0.35), radius: 20, y: 14)
            .rotation3DEffect(.degrees(rel > 0 ? -38 : 0), axis: (x: 1, y: 0, z: 0), anchor: .top, perspective: 0.6)
            .scaleEffect(1 - min(0.3, abs(rel) * 0.08) + 0.04 * lift)
            .offset(y: rel > 0 ? -CGFloat(rel) * 34 + 40 : -CGFloat(rel) * 260 + 40)
            .opacity(rel < 0 ? max(0, 1 + rel * 1.8) : (rel > 4.5 ? 0 : 1))
            .zIndex(isFocus ? 100 : -abs(rel))
    }
}
