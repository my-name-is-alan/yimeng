import Foundation
import AVFoundation
import Combine
import MediaPlayer
#if canImport(UIKit)
import UIKit
#elseif canImport(AppKit)
import AppKit
#endif

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
    private static var saveKey: String { "ym.playback.v1." + MusicSources.scopeId }

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
    @Published var isPlaying = false { didSet { if isPlaying != oldValue { Haptics.syncBeatPulse() } } }
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
    /// Gapless: the next item already enqueued on the AVQueuePlayer (no crossfade), adopted at item end without a gap.
    private var queuedNext: (id: String, item: AVPlayerItem)?
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
        #if os(iOS)
        let names: [Notification.Name] = [UIApplication.willResignActiveNotification, UIApplication.didEnterBackgroundNotification, UIApplication.willTerminateNotification]
        #elseif os(macOS)
        let names: [Notification.Name] = [NSApplication.willResignActiveNotification, NSApplication.willTerminateNotification]
        #else
        let names: [Notification.Name] = []
        #endif
        for n in names {
            lifecycle.append(nc.addObserver(forName: n, object: nil, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.save() }
            })
        }
    }

    // MARK: Public

    func play(songs: [NDSong], startAt: Int = 0, coverURL: URL? = nil) {
        guard !songs.isEmpty else { return }
        let start = max(0, min(startAt, songs.count - 1))
        if songs[start].isUnplayable { SheetCenter.shared.unplayable(songs[start]); return }
        if !FMStore.shared.owns(songs) { FMStore.shared.stop() }
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
        else { player.defaultRate = rate; player.play(); isPlaying = true }
        // Emby only persists PlaybackPositionTicks on Stopped → pause closes the session, resume opens a new one.
        if let l = embyLive { embyReport(isPlaying ? .start : .stopped, id: l.id, pos: elapsed, paused: !isPlaying) }
        syncApp()
        pushNowPlaying()
    }

    func skip(_ d: Int) {
        guard !queue.isEmpty else { return }
        index = (index + d + queue.count) % queue.count
        coverURL = nil
        startCurrent(autoplay: true, seek: nil)
    }

    /// User seek (progress bar drag, lyric tap).
    func seek(to t: TimeInterval) {
        let pos = max(0, duration > 0 ? min(t, duration - 0.5) : t)
        guard let player else { pendingSeek = pos; elapsed = pos; pushNowPlaying(); return }
        player.seek(to: CMTime(seconds: pos, preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero) { _ in }
        elapsed = pos; pushNowPlaying()
    }
    func playNext(_ ss: [NDSong]) {
        let ok = ss.filter { !$0.isUnplayable }; guard !ok.isEmpty else { return }
        guard song != nil else { play(songs: ok); return }
        queue.insert(contentsOf: ok, at: min(index + 1, queue.count)); save()
    }
    func enqueue(_ ss: [NDSong]) {
        let ok = ss.filter { !$0.isUnplayable }; guard !ok.isEmpty else { return }
        guard song != nil else { play(songs: ok); return }
        queue.append(contentsOf: ok); save()
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
    // MARK: Play mode / rate / sleep / history

    enum PlayMode: String, CaseIterable { case loop = "列表循环", one = "单曲循环", shuffle = "随机播放", order = "顺序播放" }
    var playMode: PlayMode { shuffle ? .shuffle : repeatMode == .one ? .one : repeatMode == .off ? .order : .loop }
    func cyclePlayMode() {
        switch playMode {
        case .loop: repeatMode = .one
        case .one: repeatMode = .all; if !shuffle { toggleShuffle() }
        case .shuffle: toggleShuffle(); repeatMode = .off
        case .order: repeatMode = .all
        }
        objectWillChange.send()
    }

    /// Playback speed. AVPlayer time-stretches (pitch kept); the effects tap sits after decode so it works at any rate.
    @Published private(set) var rate: Float = 1
    func setRate(_ r: Float) {
        rate = r
        player?.defaultRate = r
        if isPlaying { player?.rate = r }
    }

    /// 定时关闭: fixed time, or stop when the current track ends.
    @Published private(set) var sleepAt: Date?
    @Published private(set) var sleepAfterTrack = false
    private var sleepTask: Task<Void, Never>?
    func setSleep(minutes: Int?) {
        sleepTask?.cancel(); sleepAt = nil; sleepAfterTrack = false
        // D52: arm the same timer on Core (other sessions see it; Core sends player.sleep.fade / player.sleep).
        CoreAccountStore.shared.armSleep(minutes: minutes == 0 ? nil : minutes, endOfTrack: minutes == 0)
        guard let minutes else { return }
        if minutes == 0 { sleepAfterTrack = true; return }
        let at = Date().addingTimeInterval(TimeInterval(minutes * 60))
        sleepAt = at
        sleepTask = Task { @MainActor [weak self] in
            let fs: Double = CoreAccountStore.shared.sleepFade ? (NavidromeClient.core ? 30 : 8) : 0
            // Core drives the timer when connected (player.sleep.fade / player.sleep); this is the offline backstop.
            try? await Task.sleep(for: .seconds(max(0, at.timeIntervalSinceNow - fs + (NavidromeClient.core ? 3 : 0))))
            guard let self, !Task.isCancelled else { return }
            if fs > 0, !self.fadingOut { self.fade(to: 0, over: max(1, at.timeIntervalSinceNow)) }
            try? await Task.sleep(for: .seconds(max(0, at.timeIntervalSinceNow + (NavidromeClient.core ? 3 : 0))))
            guard !Task.isCancelled else { return }
            self.sleepFire()
        }
    }
    /// Core `player.sleep.fade`: the app fades its own volume (Core never touches audio).
    func fadeOut(over secs: Double) { guard isPlaying else { return }; fadingOut = true; fade(to: 0, over: max(1, secs)) }
    /// Core `player.sleep`: pause now (timer or end of track).
    func sleepFromServer() {
        sleepTask?.cancel()
        guard isPlaying || sleepAt != nil || sleepAfterTrack else { return }
        fadingOut = false; sleepFire()
    }
    /// X-Yimeng-Fallback of the current stream: the platform Core switched to ("local" = 本地曲库).
    @Published private(set) var fallbackFrom: String?
    private func sleepFire() {
        if isPlaying { toggle() }
        sleepAt = nil; sleepAfterTrack = false
        applyGain()
        SheetCenter.shared.say("定时关闭：已暂停")
    }

    /// 播放历史 (per source, newest first, ≤ 100).
    @Published private(set) var history: [NDSong] = PlayerStore.loadHistory()
    private static var historyKey: String { "ym.history.v1." + MusicSources.activeId }
    private static func loadHistory() -> [NDSong] {
        (UserDefaults.standard.data(forKey: historyKey)).flatMap { try? JSONDecoder().decode([NDSong].self, from: $0) } ?? []
    }
    private func record(_ s: NDSong) {
        history.removeAll { $0.id == s.id }
        history.insert(s, at: 0)
        if history.count > 100 { history.removeLast(history.count - 100) }
        if let d = try? JSONEncoder().encode(history) { UserDefaults.standard.set(d, forKey: Self.historyKey) }
    }
    func reloadHistory() { history = Self.loadHistory() }
    func clearHistory() { history = []; UserDefaults.standard.removeObject(forKey: Self.historyKey) }
    func removeHistory(at o: IndexSet) {
        history.remove(atOffsets: o)
        if let d = try? JSONEncoder().encode(history) { UserDefaults.standard.set(d, forKey: Self.historyKey) }
    }

    func cycleRepeat() { repeatMode = repeatMode == .all ? .one : repeatMode == .one ? .off : .all }

    /// ReplayGain → AVPlayer.volume (≤ 1; system volume stays untouched, no DSP so passthrough is kept).
    /// Simulator-only user volume (device uses system volume). Multiplies the ReplayGain target.
    var userGain: Float = Float(UserDefaults.standard.object(forKey: "ym.userGain") as? Double ?? 1) {
        didSet { UserDefaults.standard.set(Double(userGain), forKey: "ym.userGain"); applyGain() }
    }
    func targetVolume(for s: NDSong?) -> Float { rgVolume(for: s) * (VolumeController.usesSystem ? 1 : userGain) }
    private func rgVolume(for s: NDSong?) -> Float {
        guard let s, AudioPrefs.replayGain > 0, let rg = s.replayGain else { return 1 }
        let album = AudioPrefs.replayGain == 2
        guard let g = (album ? rg.albumGain : rg.trackGain) ?? rg.trackGain else { return 1 }
        var v = pow(10, (g + AudioPrefs.preamp) / 20)
        if AudioPrefs.clipGuard, let peak = (album ? rg.albumPeak : rg.trackPeak) ?? rg.trackPeak, peak > 0 { v = min(v, 1 / peak) }
        return Float(max(0, min(1, v)))
    }
    func applyGain() { if !fadingOut { player?.volume = targetVolume(for: song) } }

    private func fade(to target: Float, over secs: Double) {
        fadeTask?.cancel()
        guard let p = player else { return }
        let from = p.volume, steps = max(1, Int(secs * 20))
        fadeTask = Task { @MainActor [weak p] in
            for i in 1...steps {
                try? await Task.sleep(for: .milliseconds(50))
                if Task.isCancelled { return }
                p?.volume = from + (target - from) * Float(i) / Float(steps)
            }
        }
    }

    private func advanceAtEnd() {
        if sleepAfterTrack { sleepAfterTrack = false; isPlaying = false; player?.pause(); syncApp(); pushNowPlaying(); SheetCenter.shared.say("定时关闭：本首已播完"); return }
        if repeatMode == .one, queuedNext != nil { queuedNext = nil; startCurrent(autoplay: true, seek: nil); return }
        if let q = queuedNext, !queue.isEmpty, queue[(index + 1) % queue.count].id == q.id, repeatMode != .one,
           (player as? AVQueuePlayer)?.items().contains(q.item) == true || player?.currentItem === q.item {
            index = (index + 1) % queue.count; coverURL = nil
            startCurrent(autoplay: true, seek: nil, adopt: q.item); return
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
        guard preloaded?.id != next.id, queuedNext?.id != next.id else { return }
        if let local = OfflineCache.shared.file(for: next.id) {
            enqueuePreload(next, url: local)
            return
        }
        if NavidromeClient.core {
            let nid = next.id
            Task { @MainActor [weak self] in
                guard let self, self.preloaded?.id != nid, self.queuedNext?.id != nid else { return }
                guard let url = try? await CoreClient.shared.resolvePlayURL(id: nid) else { return }
                guard self.song != nil, self.preloaded?.id != nid, self.queuedNext?.id != nid else { return }
                // Still the upcoming track?
                guard !self.queue.isEmpty, self.queue[(self.index + 1) % self.queue.count].id == nid else { return }
                self.enqueuePreload(next, url: url)
            }
            return
        }
        guard let url = LibraryStore.shared.streamURL(for: next.id) else { return }
        enqueuePreload(next, url: url)
    }

    private func enqueuePreload(_ next: NDSong, url: URL) {
        guard preloaded?.id != next.id, queuedNext?.id != next.id else { return }
        let asset = Self.asset(url)
        if AudioPrefs.crossfade == 0, repeatMode != .one, !sleepAfterTrack, let qp = player as? AVQueuePlayer {
            let item = AVPlayerItem(asset: asset)
            item.allowedAudioSpatializationFormats = .monoStereoAndMultichannel
            if qp.canInsert(item, after: nil) { qp.insert(item, after: nil); queuedNext = (next.id, item); attachFX(item, next); return }
        }
        preloaded = (next.id, asset)
        Task { _ = try? await asset.load(.isPlayable) }
    }

    private static func asset(_ url: URL) -> AVURLAsset {
        if url.isFileURL { return AVURLAsset(url: url) }   // offline cache: original bytes, no network
        return AVURLAsset(url: url, options: ["AVURLAssetHTTPHeaderFieldsKey": mediaHeaders(for: url)])
    }
    static func mediaHeaders(for url: URL) -> [String: String] {
        var headers = ["User-Agent": "Yimeng/0.3 (iOS; art.yimeng.app)", "Accept": "*/*"]
        // 飞牛: stream auth is the music-token cookie; AVPlayer sends these headers on every Range request.
        if NavidromeClient.fnos, !LibraryStore.shared.fnosToken.isEmpty {
            headers["Cookie"] = FnosClient.cookieHeader(root: LibraryStore.shared.fnosRoot, token: LibraryStore.shared.fnosToken); headers["X-Music-API"] = "v1"
        }
        if NavidromeClient.core { headers.merge(CoreState.mediaHeaders(for: url)) { _, b in b } }
        return headers
    }

    /// Account/server switch: stop and forget the visible queue without deleting any saved queue.
    func resetInMemory() {
        tearDownPlayer()
        song = nil; queue = []; index = 0
        isPlaying = false; elapsed = 0; duration = 0; coverURL = nil; coverId = nil
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
        // Pre-v27 Core queues were saved unscoped (shared by the mock and every Core): drop them.
        UserDefaults.standard.removeObject(forKey: "ym.playback.v1.core")
        guard song == nil,
              let data = UserDefaults.standard.data(forKey: Self.saveKey),
              let snap = try? JSONDecoder().decode(PlaybackSnapshot.self, from: data),
              snap.index >= 0, snap.index < snap.queue.count else { return }
        let s = snap.queue[snap.index]
        guard LibraryStore.shared.connected else { return }
        // Core online ids (qq:/netease:/kugou:) 404 on /tracks/{id}; they are not library rows.
        let online = NavidromeClient.core && s.id.contains(":")
        let exists = online ? true : ((try? await NavidromeClient.shared.songExists(id: s.id)) ?? false)
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
        LyricsStore.shared.load(s)
        syncApp()
        pushNowPlaying()
    }

    // MARK: Internals

    private func syncApp() {
        guard let s = song else { return }
        AppStateBridge.current?.applyNDSong(s, coverURL: coverURL, playing: isPlaying)
    }

    private func startCurrent(autoplay: Bool, seek: TimeInterval?, adopt: AVPlayerItem? = nil) {
        #if DEBUG
        // Sim verification: `-autopause N` pauses N s after the first track starts (exercises Emby Stopped + resume point).
        if let i = ProcessInfo.processInfo.arguments.firstIndex(of: "-autopause"), i + 1 < ProcessInfo.processInfo.arguments.count,
           let n = Double(ProcessInfo.processInfo.arguments[i + 1]), !Self.autopaused {
            Self.autopaused = true
            Task { @MainActor [weak self] in try? await Task.sleep(for: .seconds(n)); if self?.isPlaying == true { self?.toggle() } }
        }
        #endif
        guard index >= 0, index < queue.count else { return }
        let s = queue[index]
        song = s
        if coverURL == nil {
            coverURL = LibraryStore.shared.coverArtURL(id: s.coverId ?? s.albumId, size: 600)
        }
        duration = TimeInterval(s.duration ?? 0)
        elapsed = seek ?? 0
        pendingSeek = nil
        // Offline cache first; Core online resolves CDN (play_url / 302) before AVPlayer.
        let local = OfflineCache.shared.file(for: s.id)
        if let adopt, let cur = player as? AVQueuePlayer {
            beginWithPlayer(adopt: adopt, cur: cur, song: s, url: local ?? URL(fileURLWithPath: "/"), autoplay: autoplay, seek: seek, isLocal: local != nil)
            return
        }
        if let local {
            beginPlayback(url: local, song: s, autoplay: autoplay, seek: seek, isLocal: true)
            return
        }
        if NavidromeClient.core {
            let trackId = s.id
            Task { @MainActor [weak self] in
                guard let self, self.song?.id == trackId else { return }
                do {
                    let url = try await CoreClient.shared.resolvePlayURL(id: trackId)
                    guard self.song?.id == trackId else { return }
                    self.beginPlayback(url: url, song: s, autoplay: autoplay, seek: seek, isLocal: false)
                } catch let e as CoreAPIError where e.status == 401 || e.code == "plugin_login_required" || e.code == "not_logged_in" || e.code == "unauthorized" {
                    self.isPlaying = false
                    SheetCenter.shared.say(e.message.isEmpty ? "需要登录音源账号才能播放" : e.message)
                } catch {
                    if let url = LibraryStore.shared.streamURL(for: trackId) {
                        self.beginPlayback(url: url, song: s, autoplay: autoplay, seek: seek, isLocal: false)
                    } else {
                        self.isPlaying = false
                        SheetCenter.shared.say("暂时无法播放")
                    }
                }
            }
            return
        }
        guard let url = LibraryStore.shared.streamURL(for: s.id) else { isPlaying = false; return }
        beginPlayback(url: url, song: s, autoplay: autoplay, seek: seek, isLocal: false)
    }

    private func beginWithPlayer(adopt: AVPlayerItem, cur: AVQueuePlayer, song s: NDSong, url: URL, autoplay: Bool, seek: TimeInterval?, isLocal: Bool) {
        if let l = embyLive { embyReport(.stopped, id: l.id, pos: l.pos, paused: false); embyLive = nil }
        detachObservers()
        if cur.currentItem !== adopt { cur.advanceToNextItem() }
        let p: AVPlayer = cur; let item = adopt
        queuedNext = nil; preloaded = nil
        finishStart(p: p, item: item, song: s, url: url, autoplay: autoplay, seek: seek, isLocal: isLocal)
    }

    private func beginPlayback(url: URL, song s: NDSong, autoplay: Bool, seek: TimeInterval?, isLocal: Bool) {
        tearDownPlayer()
        AudioRoute.configureSession()
        let asset: AVURLAsset = (preloaded?.id == s.id && !isLocal ? preloaded?.asset : nil) ?? Self.asset(url)
        preloaded = nil
        let item = AVPlayerItem(asset: asset)
        item.allowedAudioSpatializationFormats = .monoStereoAndMultichannel
        let p = AVQueuePlayer(playerItem: item)
        player = p
        attachFX(item, s)
        finishStart(p: p, item: item, song: s, url: url, autoplay: autoplay, seek: seek, isLocal: isLocal)
    }

    private func finishStart(p: AVPlayer, item: AVPlayerItem, song s: NDSong, url: URL, autoplay: Bool, seek: TimeInterval?, isLocal: Bool) {
        // Preview: do not block start; duration from metadata / readyToPlay still applies.
        _ = s.previewEndMs
        OfflineCache.shared.played(s)
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
                if let l = self.embyLive {
                    self.embyLive = (l.id, self.elapsed)
                    self.coreScrobbleIfDue()
                    if now - self.embyLastProgress > 10 { self.embyLastProgress = now; self.embyReport(.progress, id: l.id, pos: self.elapsed, paused: false); if NavidromeClient.core { let e = self.elapsed; let dur = self.duration; Task { await CoreClient.shared.putProgress(l.id, ms: Int(e * 1000), durationMs: dur > 0 ? Int(dur * 1000) : nil) } } }
                }
                if now - self.lastSave > 3 { self.lastSave = now; self.save(); self.pushNowPlaying() }
            }
        }
        endObs = NotificationCenter.default.addObserver(forName: .AVPlayerItemDidPlayToEndTime, object: item, queue: .main) { [weak self] _ in
            Task { @MainActor in self?.advanceAtEnd() }
        }
        if let seek, seek > 1 {
            p.seek(to: CMTime(seconds: seek, preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero)
        }
        p.defaultRate = rate
        record(s)
        fallbackFrom = nil
        if NavidromeClient.core, s.id.contains(":") {
            let id = s.id
            Task { let f = await CoreClient.shared.fallbackOrigin(for: url); if self.song?.id == id { self.fallbackFrom = f } }
        }
        LyricsStore.shared.load(s)
        DreamStore.shared.load(s)
        FMStore.shared.trackStarted()
        if NavidromeClient.core, (seek ?? 0) < 1 {
            let id = s.id
            Task { [weak self] in
                guard let ms = await CoreClient.shared.progress(ids: [id])[id] else { return }
                let pos = Double(ms) / 1000; guard pos > 5 else { return }
                guard let self, self.song?.id == id, self.elapsed < 3, self.duration <= 0 || pos < self.duration - 10 else { return }
                self.player?.seek(to: CMTime(seconds: pos, preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero) { _ in }
                self.elapsed = pos; self.pushNowPlaying()
            }
        }
        embyLive = (s.id, elapsed); embyLastProgress = Date().timeIntervalSinceReferenceDate
        embyReport(.start, id: s.id, pos: elapsed, paused: !autoplay)
        if NavidromeClient.emby, (seek ?? 0) < 1 {
            // Continue from Emby's saved position (set by this or another client), unless nearly finished.
            let id = s.id
            Task { [weak self] in
                guard let pos = await EmbyClient.shared.resumePosition(id), pos > 5 else { return }
                guard let self, self.song?.id == id, self.elapsed < 3, self.duration <= 0 || pos < self.duration - 10 else { return }
                self.player?.seek(to: CMTime(seconds: pos, preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero) { _ in }
                self.elapsed = pos; self.embyLive = (id, pos); self.pushNowPlaying()
            }
        }
        if autoplay {
            p.play(); isPlaying = true
            if AudioPrefs.crossfade > 0 { fade(to: vol, over: min(AudioPrefs.crossfade, 4)) }
        } else { isPlaying = false }
        syncApp()
        save()
        pushNowPlaying()
    }

    // MARK: Effects (tap on the streaming item)

    private func attachFX(_ item: AVPlayerItem, _ s: NDSong) {
        let t0 = Date()
        Task { @MainActor in
            let on = FXStore.shared.s.on
            let ok = await FXTap.apply(on: on, item: item, song: s)
            guard player?.currentItem === item else { return }
            FXStore.shared.note = on && !ok ? "此曲为杜比 / 多声道比特流，音效不可用，已保持直通。" : nil
            NetLog.logger.info("FX attach on=\(on) ok=\(ok) \(Int(Date().timeIntervalSince(t0) * 1000))ms")
        }
    }

    /// 音效 toggled: just attach/detach the audioMix on the live item — playback keeps going.
    func effectsToggled() {
        guard let item = player?.currentItem, let s = song else { return }
        attachFX(item, s)
    }

    // Emby/Jellyfin: report start / progress / stop so play counts, 最近播放 and resume work server-side.
    private var embyLive: (id: String, pos: TimeInterval)?
    private static var autopaused = false
    /// 译梦 Core history = scrobbles only: report once per play at ≥50% or 4 min (Last.fm-style rule, same as Core players).
    private var coreScrobbled: String?
    fileprivate func coreScrobbleIfDue() {
        guard NavidromeClient.core, let s = song, coreScrobbled != s.id, duration > 30,
              elapsed >= min(duration * 0.5, 240) else { return }
        coreScrobbled = s.id
        let id = s.id, ms = Int(elapsed * 1000)
        Task { await CoreClient.shared.scrobble(id, listenedMs: ms, completed: false); await HomeStore.shared.refreshPlayed() }
    }
    private var embyLastProgress: TimeInterval = 0
    private func embyReport(_ e: EmbyClient.PlayEvent, id: String, pos: TimeInterval, paused: Bool) {
        guard NavidromeClient.emby else { return }
        let ps = LibraryStore.shared.playSession(for: id)
        let tr = UserDefaults.standard.integer(forKey: "ym.maxBitRate") > 0
        Task {
            await EmbyClient.shared.report(e, id: id, position: pos, paused: paused, playSessionId: ps, transcoding: tr)
            if e == .stopped { await HomeStore.shared.refreshPlayed() }
        }
    }

    private func tearDownPlayer() {
        if let l = embyLive { embyReport(.stopped, id: l.id, pos: l.pos, paused: true); embyLive = nil }
        detachObservers()
        player?.pause()
        player = nil
        queuedNext = nil
    }
    private func detachObservers() {
        if let timeObs, let player { player.removeTimeObserver(timeObs) }
        timeObs = nil
        if let endObs { NotificationCenter.default.removeObserver(endObs) }
        endObs = nil
        statusObs = nil
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
        YMWidgetBridge.nowPlaying(s, playing: isPlaying, coverURL: coverURL)
    }
}

/// Weak bridge so PlayerStore can update AppState without a hard cycle at init.
enum AppStateBridge {
    static weak var current: AppState?
}
