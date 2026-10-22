import SwiftUI
#if canImport(UIKit)
import UIKit
#endif

// MARK: - Edge swipe back everywhere (custom nav bars hide UINavigationBar, which disables the pop gesture)

#if os(iOS)
extension UINavigationController: @retroactive UIGestureRecognizerDelegate {
    override open func viewDidLoad() {
        super.viewDidLoad()
        interactivePopGestureRecognizer?.delegate = self
    }
    public func gestureRecognizerShouldBegin(_ g: UIGestureRecognizer) -> Bool {
        g == interactivePopGestureRecognizer ? viewControllers.count > 1 : true
    }
    public func gestureRecognizer(_ g: UIGestureRecognizer, shouldRecognizeSimultaneouslyWith o: UIGestureRecognizer) -> Bool {
        g == interactivePopGestureRecognizer
    }
}
#endif

// MARK: - Navigation helpers used by menus

@MainActor
enum Nav {
    static func go(_ r: Route) {
        guard let s = AppStateBridge.current else { return }
        withAnimation(.ui) { s.nowPlaying = false; s.lyricsMode = false }
        if s.tab == .me { s.tab = .home }
        s.path.append(r)
    }
    static func album(of song: NDSong) {
        guard let id = song.albumId else { SheetCenter.shared.say("这首歌没有专辑信息"); return }
        go(.ndAlbum(NDAlbum(id: id, name: song.album ?? "", artist: song.artist, coverId: song.coverId, year: nil, songCount: nil)))
    }
    static func artist(of song: NDSong) {
        if let id = song.artistId, !id.isEmpty {
            go(.ndArtist(NDArtist(id: id, name: song.artist.components(separatedBy: " / ").first ?? song.artist, albumCount: nil))); return
        }
        let name = song.artist.components(separatedBy: CharacterSet(charactersIn: "/&,;")).first?.trimmingCharacters(in: .whitespaces) ?? song.artist
        Task { @MainActor in
            var ar = LibraryCache.shared.artists.first { $0.name == name }
            if ar == nil { ar = (try? await LibraryStore.shared.search(query: name))?.artists.first { $0.name == name } }
            guard let ar else { SheetCenter.shared.say("没找到这位歌手"); return }
            go(.ndArtist(ar))
        }
    }
}

// MARK: - Long-press menus with previews

/// Fetch-then-act for collection menus (album / playlist / artist).
@MainActor
private func withSongs(_ load: @escaping () async throws -> [NDSong], _ act: @escaping ([NDSong]) -> Void, done: String) {
    Task { @MainActor in
        guard let ss = try? await load(), !ss.isEmpty else { SheetCenter.shared.say("没有可播放的歌曲"); return }
        act(ss); SheetCenter.shared.say(done)
    }
}

private struct MenuPreview: View {
    let url: URL?; let title: String; let subtitle: String; var round = false
    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            CoverImage(url: url, points: 240).frame(width: 240, height: 240)
                .clipShape(RoundedRectangle(cornerRadius: round ? 120 : 14, style: .continuous))
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.system(size: 17, weight: .semibold)).lineLimit(2)
                Text(subtitle).font(.system(size: 13)).foregroundStyle(.secondary).lineLimit(1)
            }.padding(.horizontal, 4)
        }
        .padding(14).frame(width: 268).background(Color.ymSystemBackground)
    }
}

struct AlbumMenu: ViewModifier {
    let album: NDAlbum
    func body(content: Content) -> some View {
        content.contextMenu {
            Button { withSongs({ try await NavidromeClient.shared.album(id: album.id).1 }, { PlayerStore.shared.play(songs: $0.filter { !$0.isUnplayable }) }, done: "开始播放") } label: { Label("播放", systemImage: "play.fill") }
            Button { withSongs({ try await NavidromeClient.shared.album(id: album.id).1 }, { PlayerStore.shared.playNext($0) }, done: "将在下一首播放") } label: { Label("下一首播放", systemImage: "text.line.first.and.arrowtriangle.forward") }
            Button { withSongs({ try await NavidromeClient.shared.album(id: album.id).1 }, { PlayerStore.shared.enqueue($0) }, done: "已加入播放队列") } label: { Label("加入播放队列", systemImage: "text.badge.plus") }
            if let aid = album.artistId { Button { Nav.go(.ndArtist(NDArtist(id: aid, name: album.artist, albumCount: nil))) } label: { Label("前往歌手", systemImage: "music.mic") } }
            Button { StarStore.shared.toggle(album); Haptics.tap() } label: { Label(StarStore.shared.isStarred(album: album.id) ? "取消收藏" : "收藏", systemImage: "heart") }
        } preview: {
            MenuPreview(url: LibraryStore.shared.coverURL(album), title: album.name, subtitle: album.artist)
        }
    }
}

struct ArtistMenu: ViewModifier {
    let artist: NDArtist
    func body(content: Content) -> some View {
        content.contextMenu {
            Button { withSongs({ try await NavidromeClient.shared.artistDetail(artist).top }, { PlayerStore.shared.play(songs: $0.filter { !$0.isUnplayable }) }, done: "播放热门歌曲") } label: { Label("播放热门歌曲", systemImage: "play.fill") }
            Button { withSongs({ try await NavidromeClient.shared.artistDetail(artist).top }, { PlayerStore.shared.playNext($0) }, done: "将在下一首播放") } label: { Label("下一首播放", systemImage: "text.line.first.and.arrowtriangle.forward") }
            Button { withSongs({ try await NavidromeClient.shared.artistDetail(artist).top }, { PlayerStore.shared.enqueue($0) }, done: "已加入播放队列") } label: { Label("加入播放队列", systemImage: "text.badge.plus") }
            Button { Nav.go(.ndArtist(artist)) } label: { Label("前往歌手", systemImage: "music.mic") }
            Button { StarStore.shared.toggle(artist); Haptics.tap() } label: { Label(StarStore.shared.isStarred(artist: artist.id) ? "取消关注" : "关注", systemImage: "heart") }
        } preview: {
            MenuPreview(url: LibraryStore.shared.coverArtURL(id: artist.coverId ?? artist.id, size: 600), title: artist.name, subtitle: artist.albumCount.map { "\($0) 张专辑" } ?? "歌手", round: true)
        }
    }
}

struct PlaylistMenu: ViewModifier {
    let playlist: NDPlaylist
    func body(content: Content) -> some View {
        content.contextMenu {
            Button { withSongs({ try await NavidromeClient.shared.playlist(id: playlist.id) }, { PlayerStore.shared.play(songs: $0.filter { !$0.isUnplayable }) }, done: "开始播放") } label: { Label("播放", systemImage: "play.fill") }
            Button { withSongs({ try await NavidromeClient.shared.playlist(id: playlist.id) }, { PlayerStore.shared.playNext($0) }, done: "将在下一首播放") } label: { Label("下一首播放", systemImage: "text.line.first.and.arrowtriangle.forward") }
            Button { withSongs({ try await NavidromeClient.shared.playlist(id: playlist.id) }, { PlayerStore.shared.enqueue($0) }, done: "已加入播放队列") } label: { Label("加入播放队列", systemImage: "text.badge.plus") }
            Button { Nav.go(.ndPlaylist(playlist)) } label: { Label("打开歌单", systemImage: "music.note.list") }
        } preview: {
            MenuPreview(url: LibraryStore.shared.coverArtURL(id: playlist.coverId ?? playlist.id, size: 600), title: playlist.name, subtitle: playlist.songCount.map { "\($0) 首" } ?? "歌单")
        }
    }
}

extension View {
    func albumMenu(_ a: NDAlbum) -> some View { modifier(AlbumMenu(album: a)) }
    func artistMenu(_ a: NDArtist) -> some View { modifier(ArtistMenu(artist: a)) }
    func playlistMenu(_ p: NDPlaylist) -> some View { modifier(PlaylistMenu(playlist: p)) }
}

// MARK: - Haptics (F) — central so a single setting turns them all off

@MainActor
enum Haptics {
    static var enabled: Bool { UserDefaults.standard.object(forKey: "ym.haptics") as? Bool ?? true }
    static var beatPulse: Bool { UserDefaults.standard.bool(forKey: "ym.hapticBeat") }
    #if os(iOS)
    private static let light = UIImpactFeedbackGenerator(style: .light)
    private static let soft = UIImpactFeedbackGenerator(style: .soft)
    private static let sel = UISelectionFeedbackGenerator()
    private static let note = UINotificationFeedbackGenerator()
    static func tap() { guard enabled else { return }; light.impactOccurred() }
    static func tick() { guard enabled else { return }; sel.selectionChanged() }
    static func success() { guard enabled else { return }; note.notificationOccurred(.success) }
    static func pulse(_ strength: Double) { guard enabled, beatPulse else { return }; soft.impactOccurred(intensity: max(0.2, min(1, strength))) }
    private static var beatTimer: Timer?
    private static var lastPulse = 0.0
    static func syncBeatPulse() {
        let want = enabled && beatPulse && PlayerStore.shared.isPlaying
        if !want { beatTimer?.invalidate(); beatTimer = nil; return }
        guard beatTimer == nil else { return }
        beatTimer = Timer.scheduledTimer(withTimeInterval: 1.0 / 30, repeats: true) { _ in
            MainActor.assumeIsolated {
                #if canImport(UIKit)
                // BeatStore lives in BeatTracker (iOS ambience); optional.
                #endif
            }
        }
    }
    #else
    static func tap() {}
    static func tick() {}
    static func success() {}
    static func pulse(_ strength: Double) {}
    static func syncBeatPulse() {}
    #endif
}

struct MiniSwipe: ViewModifier {
    @EnvironmentObject var state: AppState
    @State private var dx: CGFloat = 0
    func body(content: Content) -> some View {
        content
            .offset(x: dx * 0.6)
            .gesture(DragGesture(minimumDistance: 14).onChanged { v in
                if abs(v.translation.width) > abs(v.translation.height) { dx = v.translation.width }
            }.onEnded { v in
                let w = v.translation.width, h = v.translation.height
                withAnimation(.snappy) { dx = 0 }
                if h < -40, abs(h) > abs(w) { withAnimation(.ui) { state.nowPlaying = true } }
                else if abs(w) > 60, abs(w) > abs(h) { Haptics.tick(); state.skip(w < 0 ? 1 : -1) }
            })
    }
}

/// Full player: drag down anywhere to collapse (follows the finger).
struct CollapseSwipe: ViewModifier {
    @EnvironmentObject var state: AppState
    @State private var dy: CGFloat = 0
    func body(content: Content) -> some View {
        content
            .offset(y: max(0, dy))
            .simultaneousGesture(DragGesture(minimumDistance: 20).onChanged { v in
                if v.translation.height > 0, v.translation.height > abs(v.translation.width) * 1.4 { dy = v.translation.height }
            }.onEnded { v in
                if dy > 140 || (v.predictedEndTranslation.height > 380 && dy > 40) {
                    withAnimation(.ui) { state.nowPlaying = false; state.lyricsMode = false }
                    DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) { dy = 0 }
                } else { withAnimation(.snappy) { dy = 0 } }
            })
    }
}

/// Cover: swipe left/right to change track, with a little throw.
struct CoverSwipe: ViewModifier {
    @EnvironmentObject var state: AppState
    @State private var dx: CGFloat = 0
    func body(content: Content) -> some View {
        content
            .offset(x: dx).rotationEffect(.degrees(Double(dx) / 40))
            .gesture(DragGesture(minimumDistance: 12).onChanged { v in
                if abs(v.translation.width) > abs(v.translation.height) { dx = v.translation.width }
            }.onEnded { v in
                let w = v.translation.width
                if abs(w) > 80 || abs(v.predictedEndTranslation.width) > 260 {
                    Haptics.tick()
                    withAnimation(.easeIn(duration: 0.15)) { dx = w < 0 ? -420 : 420 }
                    DispatchQueue.main.asyncAfter(deadline: .now() + 0.15) {
                        state.skip(w < 0 ? 1 : -1)
                        dx = w < 0 ? 300 : -300
                        withAnimation(.spring(response: 0.42, dampingFraction: 0.8)) { dx = 0 }
                    }
                } else { withAnimation(.snappy) { dx = 0 } }
            })
    }
}
