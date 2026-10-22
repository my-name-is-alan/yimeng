import Foundation
import AVFoundation
import Combine
import MediaPlayer
import UIKit

/// Persisted playback snapshot (no credentials).
struct PlaybackSnapshot: Codable {
    var source: String = "navidrome"
    var queue: [NDSong]
    var index: Int
    var elapsed: TimeInterval
    var duration: TimeInterval
    var coverId: String?
}

/// AVPlayer-backed playback for Subsonic/Navidrome streams, with resume-after-relaunch.
@MainActor
final class PlayerStore: ObservableObject {
    static let shared = PlayerStore()
    private static let saveKey = "ym.playback.v1"

    @Published private(set) var song: NDSong?
    @Published private(set) var queue: [NDSong] = []
    @Published private(set) var index: Int = 0
    @Published var isPlaying = false
    @Published private(set) var elapsed: TimeInterval = 0
    @Published private(set) var duration: TimeInterval = 0
    @Published var coverURL: URL?

    private var coverId: String?
    private var player: AVPlayer?
    private var timeObs: Any?
    private var endObs: NSObjectProtocol?
    private var statusObs: NSKeyValueObservation?
    /// Restored but AVPlayer not yet created; first play seeks here.
    private var pendingSeek: TimeInterval?
    private var lastSave: TimeInterval = 0
    private var lifecycle: [NSObjectProtocol] = []

    var hasRealPlayback: Bool { song != nil }

    init() {
        let nc = NotificationCenter.default
        for n in [UIApplication.willResignActiveNotification, UIApplication.didEnterBackgroundNotification, UIApplication.willTerminateNotification] {
            lifecycle.append(nc.addObserver(forName: n, object: nil, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.save() }
            })
        }
    }

    // MARK: Public

    func play(songs: [NDSong], startAt: Int = 0, coverURL: URL? = nil) {
        guard !songs.isEmpty else { return }
        queue = songs
        index = max(0, min(startAt, songs.count - 1))
        self.coverURL = coverURL
        coverId = nil
        startCurrent(autoplay: true, seek: nil)
    }

    func playAlbum(_ album: NDAlbum, songs: [NDSong], startAt: Int = 0) {
        play(songs: songs, startAt: startAt, coverURL: LibraryStore.shared.coverURL(album, size: 600))
        coverId = album.coverId ?? album.id
    }

    func toggle() {
        if player == nil, song != nil {
            // Restored-from-disk: build the player now and seek to the saved time.
            startCurrent(autoplay: true, seek: pendingSeek ?? elapsed)
            return
        }
        guard let player else { return }
        if isPlaying { player.pause(); isPlaying = false; save() }
        else { player.play(); isPlaying = true }
        syncApp()
        pushNowPlaying()
    }

    func skip(_ d: Int) {
        guard !queue.isEmpty else { return }
        index = (index + d + queue.count) % queue.count
        coverURL = nil
        startCurrent(autoplay: true, seek: nil)
    }

    func clear() {
        tearDownPlayer()
        song = nil; queue = []; index = 0
        isPlaying = false; elapsed = 0; duration = 0; coverURL = nil; coverId = nil
        UserDefaults.standard.removeObject(forKey: Self.saveKey)
    }

    // MARK: Persistence

    func save() {
        guard let s = song, !queue.isEmpty else { return }
        let snap = PlaybackSnapshot(queue: queue, index: index, elapsed: elapsed,
                                    duration: duration, coverId: coverId ?? s.coverId ?? s.albumId)
        if let data = try? JSONEncoder().encode(snap) {
            UserDefaults.standard.set(data, forKey: Self.saveKey)
        }
    }

    /// Call after Navidrome is connected. Restores paused state; hides bar if song is gone.
    func restore() async {
        guard song == nil,
              let data = UserDefaults.standard.data(forKey: Self.saveKey),
              let snap = try? JSONDecoder().decode(PlaybackSnapshot.self, from: data),
              snap.index >= 0, snap.index < snap.queue.count else { return }
        let s = snap.queue[snap.index]
        guard LibraryStore.shared.connected else { return }
        let exists = (try? await NavidromeClient.shared.songExists(id: s.id)) ?? false
        guard exists else { clear(); return }
        queue = snap.queue
        index = snap.index
        song = s
        coverId = snap.coverId
        coverURL = LibraryStore.shared.coverArtURL(id: snap.coverId ?? s.coverId ?? s.albumId, size: 600)
        duration = snap.duration > 0 ? snap.duration : TimeInterval(s.duration ?? 0)
        elapsed = snap.elapsed
        pendingSeek = snap.elapsed
        isPlaying = false
        syncApp()
        pushNowPlaying()
    }

    // MARK: Internals

    private func syncApp() {
        guard let s = song else { return }
        AppStateBridge.current?.applyNDSong(s, coverURL: coverURL, playing: isPlaying)
    }

    private func startCurrent(autoplay: Bool, seek: TimeInterval?) {
        guard index >= 0, index < queue.count else { return }
        let s = queue[index]
        song = s
        if coverURL == nil {
            coverURL = LibraryStore.shared.coverArtURL(id: s.coverId ?? s.albumId, size: 600)
        }
        duration = TimeInterval(s.duration ?? 0)
        elapsed = seek ?? 0
        pendingSeek = nil
        guard let url = LibraryStore.shared.streamURL(for: s.id) else { isPlaying = false; return }
        tearDownPlayer()
        try? AVAudioSession.sharedInstance().setCategory(.playback, mode: .default)
        try? AVAudioSession.sharedInstance().setActive(true)
        let headers = ["User-Agent": "Yimeng/0.3 (iOS; art.yimeng.app)", "Accept": "*/*"]
        let asset = AVURLAsset(url: url, options: ["AVURLAssetHTTPHeaderFieldsKey": headers])
        let item = AVPlayerItem(asset: asset)
        let p = AVPlayer(playerItem: item)
        player = p
        statusObs = item.observe(\.status, options: [.new]) { [weak self] item, _ in
            Task { @MainActor in
                guard let self, item.status == .readyToPlay else { return }
                let d = item.duration.seconds
                if d.isFinite, d > 0 { self.duration = d }
            }
        }
        timeObs = p.addPeriodicTimeObserver(forInterval: CMTime(seconds: 0.5, preferredTimescale: 600), queue: .main) { [weak self] t in
            MainActor.assumeIsolated {
                guard let self, self.isPlaying else { return }
                self.elapsed = t.seconds.isFinite ? t.seconds : 0
                let now = Date().timeIntervalSinceReferenceDate
                if now - self.lastSave > 3 { self.lastSave = now; self.save(); self.pushNowPlaying() }
            }
        }
        endObs = NotificationCenter.default.addObserver(forName: .AVPlayerItemDidPlayToEndTime, object: item, queue: .main) { [weak self] _ in
            Task { @MainActor in self?.skip(1) }
        }
        if let seek, seek > 1 {
            p.seek(to: CMTime(seconds: seek, preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero)
        }
        if autoplay { p.play(); isPlaying = true } else { isPlaying = false }
        syncApp()
        save()
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
            MPMediaItemPropertyPlaybackDuration: duration,
            MPNowPlayingInfoPropertyElapsedPlaybackTime: elapsed,
            MPNowPlayingInfoPropertyPlaybackRate: isPlaying ? 1.0 : 0.0,
        ]
        if let album = s.album { info[MPMediaItemPropertyAlbumTitle] = album }
        MPNowPlayingInfoCenter.default().nowPlayingInfo = info
        MPNowPlayingInfoCenter.default().playbackState = isPlaying ? .playing : .paused
    }
}

/// Weak bridge so PlayerStore can update AppState without a hard cycle at init.
enum AppStateBridge {
    static weak var current: AppState?
}
