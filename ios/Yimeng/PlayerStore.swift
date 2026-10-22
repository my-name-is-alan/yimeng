import Foundation
import AVFoundation
import Combine
import MediaPlayer

/// AVPlayer-backed playback for Subsonic/Navidrome streams.
@MainActor
final class PlayerStore: ObservableObject {
    static let shared = PlayerStore()

    @Published private(set) var song: NDSong?
    @Published private(set) var queue: [NDSong] = []
    @Published private(set) var index: Int = 0
    @Published var isPlaying = false
    @Published private(set) var elapsed: TimeInterval = 0
    @Published private(set) var duration: TimeInterval = 0
    @Published var coverURL: URL?

    private var player: AVPlayer?
    private var timeObs: Any?
    private var endObs: NSObjectProtocol?
    private var statusObs: NSKeyValueObservation?

    var hasRealPlayback: Bool { song != nil }

    func play(songs: [NDSong], startAt: Int = 0, coverURL: URL? = nil) {
        guard !songs.isEmpty else { return }
        let i = max(0, min(startAt, songs.count - 1))
        queue = songs
        index = i
        self.coverURL = coverURL
        startCurrent()
    }

    func playAlbum(_ album: NDAlbum, songs: [NDSong], startAt: Int = 0) {
        play(songs: songs, startAt: startAt, coverURL: LibraryStore.shared.coverURL(album, size: 600))
    }

    func toggle() {
        guard let player else { return }
        if isPlaying {
            player.pause()
            isPlaying = false
        } else {
            player.play()
            isPlaying = true
        }
        pushNowPlaying()
    }

    func skip(_ d: Int) {
        guard !queue.isEmpty else { return }
        index = (index + d + queue.count) % queue.count
        startCurrent()
    }

    func stop() {
        tearDownPlayer()
        song = nil
        isPlaying = false
        elapsed = 0
        duration = 0
    }

    private func startCurrent() {
        guard index >= 0, index < queue.count else { return }
        let s = queue[index]
        song = s
        duration = TimeInterval(s.duration ?? 0)
        elapsed = 0
        guard let url = LibraryStore.shared.streamURL(for: s.id) else {
            isPlaying = false
            return
        }
        tearDownPlayer()
        do {
            try AVAudioSession.sharedInstance().setCategory(.playback, mode: .default)
            try AVAudioSession.sharedInstance().setActive(true)
        } catch {}
        let headers = ["User-Agent": "Yimeng/0.3 (iOS; art.yimeng.app)", "Accept": "*/*"]
        let asset = AVURLAsset(url: url, options: ["AVURLAssetHTTPHeaderFieldsKey": headers])
        let item = AVPlayerItem(asset: asset)
        let p = AVPlayer(playerItem: item)
        player = p
        statusObs = item.observe(\.status, options: [.new]) { [weak self] item, _ in
            Task { @MainActor in
                guard let self else { return }
                if item.status == .readyToPlay {
                    let d = item.duration.seconds
                    if d.isFinite, d > 0 { self.duration = d }
                }
            }
        }
        timeObs = p.addPeriodicTimeObserver(forInterval: CMTime(seconds: 0.25, preferredTimescale: 600), queue: .main) { [weak self] t in
            Task { @MainActor in
                guard let self else { return }
                self.elapsed = t.seconds.isFinite ? t.seconds : 0
                if self.isPlaying { self.pushNowPlayingLight() }
            }
        }
        endObs = NotificationCenter.default.addObserver(forName: .AVPlayerItemDidPlayToEndTime, object: item, queue: .main) { [weak self] _ in
            Task { @MainActor in self?.skip(1) }
        }
        p.play()
        isPlaying = true
        // Mirror into AppState DreamTrack for existing UI chrome
        if let app = AppStateBridge.current {
            app.applyNDSong(s, coverURL: coverURL, playing: true)
        }
        pushNowPlaying()
    }

    private func tearDownPlayer() {
        if let timeObs, let player { player.removeTimeObserver(timeObs) }
        timeObs = nil
        if let endObs { NotificationCenter.default.removeObserver(endObs) }
        endObs = nil
        statusObs = nil
        player?.pause()
        player = nil
    }

    private func pushNowPlaying() {
        guard let s = song else { return }
        var info: [String: Any] = [
            MPMediaItemPropertyTitle: s.title,
            MPMediaItemPropertyArtist: s.artist,
            MPMediaItemPropertyPlaybackDuration: duration > 0 ? duration : TimeInterval(s.duration ?? 0),
            MPNowPlayingInfoPropertyElapsedPlaybackTime: elapsed,
            MPNowPlayingInfoPropertyPlaybackRate: isPlaying ? 1.0 : 0.0,
        ]
        if let album = s.album { info[MPMediaItemPropertyAlbumTitle] = album }
        MPNowPlayingInfoCenter.default().nowPlayingInfo = info
        MPNowPlayingInfoCenter.default().playbackState = isPlaying ? .playing : .paused
    }

    private var lastNPPush: TimeInterval = 0
    private func pushNowPlayingLight() {
        let now = Date().timeIntervalSinceReferenceDate
        guard now - lastNPPush > 1 else { return }
        lastNPPush = now
        pushNowPlaying()
    }
}

/// Weak bridge so PlayerStore can update AppState without a hard cycle at init.
enum AppStateBridge {
    static weak var current: AppState?
}
