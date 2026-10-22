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
    /// Resume point is per source.
    private static var saveKey: String { "ym.playback.v1." + MusicSources.activeId }

    /// Source switch: stop audio, clear now-playing UI, but keep the old source's saved resume point.
    func stopForSourceSwitch() {
        tearDownPlayer()
        song = nil; queue = []; index = 0
        isPlaying = false; elapsed = 0; duration = 0; coverURL = nil; coverId = nil
        syncApp()
    }

    @Published private(set) var song: NDSong?
    @Published private(set) var queue: [NDSong] = []
    @Published private(set) var index: Int = 0
    @Published var isPlaying = false
    @Published private(set) var elapsed: TimeInterval = 0
    @Published private(set) var duration: TimeInterval = 0
    @Published var coverURL: URL?
    enum RepeatMode: String, Codable { case off, all, one }
    @Published private(set) var shuffle = false
    @Published private(set) var repeatMode: RepeatMode = .all
    private var unshuffled: [NDSong]?
    private var fadeTask: Task<Void, Never>?
    private var fadingOut = false
    private var preloaded: (id: String, asset: AVURLAsset)?
    var upcoming: [NDSong] { index + 1 < queue.count ? Array(queue[(index + 1)...]) : [] }

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
        let fx = EffectsEngine.shared
        if fx.active {
            if isPlaying { fx.pause(); isPlaying = false; elapsed = fx.currentTime; save() } else { fx.resume(); isPlaying = true }
            syncApp(); pushNowPlaying(); return
        }
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

    // MARK: Queue

    func playNext(_ s: NDSong) {
        guard song != nil else { play(songs: [s]); return }
        queue.insert(s, at: min(index + 1, queue.count)); save()
    }
    func enqueue(_ s: NDSong) {
        guard song != nil else { play(songs: [s]); return }
        queue.append(s); save()
    }
    func jump(toUpcoming i: Int) {
        let target = index + 1 + i
        guard target < queue.count else { return }
        index = target; coverURL = nil
        startCurrent(autoplay: true, seek: nil)
    }
    func moveUpcoming(from: IndexSet, to: Int) {
        var up = upcoming; up.move(fromOffsets: from, toOffset: to)
        queue = Array(queue[...index]) + up; save()
    }
    func removeUpcoming(at: IndexSet) {
        var up = upcoming; up.remove(atOffsets: at)
        queue = Array(queue[...index]) + up; save()
    }
    func clearUpcoming() { guard !queue.isEmpty else { return }; queue = Array(queue[...index]); save() }
    func toggleShuffle() {
        if shuffle, let orig = unshuffled, let cur = song {
            queue = orig; index = orig.firstIndex(of: cur) ?? 0; unshuffled = nil; shuffle = false
        } else if !queue.isEmpty {
            unshuffled = queue
            queue = Array(queue[...index]) + upcoming.shuffled(); shuffle = true
        }
        save()
    }
    func cycleRepeat() { repeatMode = repeatMode == .all ? .one : repeatMode == .one ? .off : .all }

    /// ReplayGain → AVPlayer.volume (≤ 1; system volume stays untouched, no DSP so passthrough is kept).
    func targetVolume(for s: NDSong?) -> Float {
        guard let s, AudioPrefs.replayGain > 0, let rg = s.replayGain else { return 1 }
        let album = AudioPrefs.replayGain == 2
        guard let g = (album ? rg.albumGain : rg.trackGain) ?? rg.trackGain else { return 1 }
        var v = pow(10, (g + AudioPrefs.preamp) / 20)
        if AudioPrefs.clipGuard, let peak = (album ? rg.albumPeak : rg.trackPeak) ?? rg.trackPeak, peak > 0 { v = min(v, 1 / peak) }
        return Float(max(0, min(1, v)))
    }
    func applyGain() { if !fadingOut { setVolume(targetVolume(for: song)) } }
    private var volume: Float { EffectsEngine.shared.active ? EffectsEngine.shared.volume : (player?.volume ?? 1) }
    private func setVolume(_ v: Float) { if EffectsEngine.shared.active { EffectsEngine.shared.volume = v } else { player?.volume = v } }

    private func fade(to target: Float, over secs: Double) {
        fadeTask?.cancel()
        guard player != nil || EffectsEngine.shared.active else { return }
        let from = volume, steps = max(1, Int(secs * 20))
        fadeTask = Task { @MainActor [weak self] in
            for i in 1...steps {
                try? await Task.sleep(for: .milliseconds(50))
                if Task.isCancelled { return }
                self?.setVolume(from + (target - from) * Float(i) / Float(steps))
            }
        }
    }

    private func advanceAtEnd() {
        if EffectsEngine.shared.active {
            if repeatMode == .one { try? EffectsEngine.shared.play(from: 0); fadingOut = false; applyGain(); return }
            if index + 1 >= queue.count && repeatMode == .off {
                isPlaying = false; tearDownPlayer(); elapsed = 0; pendingSeek = 0; syncApp(); pushNowPlaying(); return
            }
            skip(1); return
        }
        if repeatMode == .one { player?.seek(to: .zero); player?.play(); fadingOut = false; applyGain(); return }
        if index + 1 >= queue.count && repeatMode == .off {
            isPlaying = false; player?.pause(); player?.seek(to: .zero); elapsed = 0; syncApp(); pushNowPlaying(); return
        }
        skip(1)
    }

    private func preloadNext() {
        guard AudioPrefs.gapless, index + 1 < queue.count || repeatMode == .all, !queue.isEmpty else { return }
        let next = queue[(index + 1) % queue.count]
        guard preloaded?.id != next.id, let url = LibraryStore.shared.streamURL(for: next.id) else { return }
        let asset = Self.asset(url)
        preloaded = (next.id, asset)
        Task { _ = try? await asset.load(.isPlayable) }
    }

    private static func asset(_ url: URL) -> AVURLAsset {
        let headers = ["User-Agent": "Yimeng/0.3 (iOS; art.yimeng.app)", "Accept": "*/*"]
        return AVURLAsset(url: url, options: ["AVURLAssetHTTPHeaderFieldsKey": headers])
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
        AudioRoute.configureSession()
        if FXStore.shared.s.on && !passthroughOnce.contains(s.id) {
            startEngine(s, url: url, autoplay: autoplay, seek: seek, handoff: false)
            return
        }
        let asset: AVURLAsset = (preloaded?.id == s.id ? preloaded?.asset : nil) ?? Self.asset(url)
        preloaded = nil
        let item = AVPlayerItem(asset: asset)
        // Let iOS render multichannel / Atmos (spatial on AirPods, passthrough on AirPlay/HDMI) — no in-app downmix.
        item.allowedAudioSpatializationFormats = .monoStereoAndMultichannel
        let p = AVPlayer(playerItem: item)
        player = p
        fadingOut = false
        let vol = targetVolume(for: s)
        if AudioPrefs.crossfade > 0 && autoplay { p.volume = 0; fadeTask?.cancel() } else { p.volume = vol }
        AudioRoute.shared.refresh()
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
                let left = self.duration - self.elapsed
                if self.duration > 0, left < 20 { self.preloadNext() }
                let cf = AudioPrefs.crossfade
                if cf > 0, self.duration > cf * 2, left > 0, left <= cf, !self.fadingOut, self.repeatMode != .one {
                    self.fadingOut = true
                    self.fade(to: 0, over: left)
                }
                let now = Date().timeIntervalSinceReferenceDate
                if now - self.lastSave > 3 { self.lastSave = now; self.save(); self.pushNowPlaying() }
            }
        }
        endObs = NotificationCenter.default.addObserver(forName: .AVPlayerItemDidPlayToEndTime, object: item, queue: .main) { [weak self] _ in
            Task { @MainActor in self?.advanceAtEnd() }
        }
        if let seek, seek > 1 {
            p.seek(to: CMTime(seconds: seek, preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero)
        }
        if autoplay {
            p.play(); isPlaying = true
            if AudioPrefs.crossfade > 0 { fade(to: vol, over: min(AudioPrefs.crossfade, 4)) }
        } else { isPlaying = false }
        syncApp()
        save()
        pushNowPlaying()
    }

    // MARK: Effects (AVAudioEngine) path

    /// Songs whose format the engine couldn't open: play them passthrough instead.
    private var passthroughOnce: Set<String> = []
    private var engineTick: Task<Void, Never>?
    private var engineGen = 0

    /// `handoff`: AVPlayer keeps playing while the file downloads; at hand-over we read its live position.
    private func startEngine(_ s: NDSong, url: URL, autoplay: Bool, seek: TimeInterval?, handoff: Bool) {
        engineGen += 1; let g = engineGen
        if !handoff { isPlaying = autoplay; syncApp() }
        let fx = EffectsEngine.shared
        Task { @MainActor in
            do {
                let local = try await fx.fetch(url, id: s.id, suffix: s.suffix)
                guard g == engineGen, song?.id == s.id else { return }
                try fx.load(local)
                var at = seek ?? 0
                var play = autoplay
                if handoff, let p = player {
                    at = p.currentTime().seconds.isFinite ? p.currentTime().seconds : elapsed
                    play = p.rate > 0
                    tearDownAVPlayer()
                }
                fx.onEnd = { [weak self] in self?.advanceAtEnd() }
                fx.volume = targetVolume(for: s)
                try fx.play(from: at)
                if !play { fx.pause() }
                isPlaying = play
                let d = fx.duration; if d > 0 { duration = d }
                elapsed = at
                AudioRoute.shared.refresh()
                startEngineTick()
                syncApp(); save(); pushNowPlaying()
            } catch {
                guard g == engineGen, song?.id == s.id else { return }
                NetLog.logger.error("FX engine failed: \(error.localizedDescription, privacy: .public)")
                SheetCenter.shared.say("此格式无法使用音效，已改为直通播放")
                passthroughOnce.insert(s.id)
                if !handoff { startCurrent(autoplay: autoplay, seek: seek) }
            }
        }
    }

    private func startEngineTick() {
        engineTick?.cancel()
        engineTick = Task { @MainActor [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .milliseconds(500))
                guard let self, EffectsEngine.shared.active else { return }
                guard self.isPlaying else { continue }
                self.elapsed = EffectsEngine.shared.currentTime
                let left = self.duration - self.elapsed
                let cf = AudioPrefs.crossfade
                if cf > 0, self.duration > cf * 2, left > 0, left <= cf, !self.fadingOut, self.repeatMode != .one {
                    self.fadingOut = true; self.fade(to: 0, over: left)
                }
                let now = Date().timeIntervalSinceReferenceDate
                if now - self.lastSave > 3 { self.lastSave = now; self.save(); self.pushNowPlaying() }
            }
        }
    }

    /// 音效 switched on/off mid-track: keep position (and play state) across the path change.
    func effectsToggled() {
        guard let s = song, let url = LibraryStore.shared.streamURL(for: s.id) else { return }
        passthroughOnce.remove(s.id)
        if FXStore.shared.s.on {
            if player != nil { startEngine(s, url: url, autoplay: isPlaying, seek: elapsed, handoff: true) }
            else { pendingSeek = pendingSeek ?? elapsed }   // not started yet: next play() builds the engine
        } else if EffectsEngine.shared.active {
            let at = EffectsEngine.shared.currentTime, playing = isPlaying
            startCurrent(autoplay: playing, seek: at)
        } else { engineGen += 1 }   // cancel an in-flight hand-off
    }

    private func tearDownPlayer() {
        engineGen += 1
        engineTick?.cancel(); engineTick = nil
        EffectsEngine.shared.onEnd = nil
        EffectsEngine.shared.stop()
        tearDownAVPlayer()
    }

    private func tearDownAVPlayer() {
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
