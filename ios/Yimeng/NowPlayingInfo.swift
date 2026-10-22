import Foundation
import MediaPlayer
import UIKit
#if canImport(ActivityKit)
import ActivityKit
#endif

/// Lock screen / Control Center metadata + remote commands, and the Live Activity (Dynamic Island).
final class PlaybackSystem {
    static let shared = PlaybackSystem()
    private var configured = false
    var onCommand: ((String) -> Void)?
    /// Last Live Activity error / status for diagnostics (simulator may lack entitlement).
    private(set) var liveActivityNote: String = "idle"

    func update(_ t: DreamTrack, elapsed: TimeInterval, playing: Bool) {
        configureRemote()
        var info: [String: Any] = [
            MPMediaItemPropertyTitle: t.title,
            MPMediaItemPropertyArtist: t.artist,
            MPMediaItemPropertyPlaybackDuration: t.duration,
            MPNowPlayingInfoPropertyElapsedPlaybackTime: elapsed,
            MPNowPlayingInfoPropertyPlaybackRate: playing ? 1.0 : 0.0,
        ]
        if let img = UIImage(named: t.cover) {
            info[MPMediaItemPropertyArtwork] = MPMediaItemArtwork(boundsSize: img.size) { _ in img }
        }
        MPNowPlayingInfoCenter.default().nowPlayingInfo = info
        MPNowPlayingInfoCenter.default().playbackState = playing ? .playing : .paused
        updateActivity(t, elapsed: elapsed, playing: playing)
    }

    func forceLiveActivity() {
        let t = DreamMock.zig
        update(t, elapsed: 72, playing: true)
    }

    func endLiveActivity() {
        #if canImport(ActivityKit)
        if let a = activityRef as? Activity<YimengActivityAttributes> {
            Task { await a.end(nil, dismissalPolicy: .immediate) }
            activityRef = nil
            liveActivityNote = "ended"
        }
        #endif
    }

    private func configureRemote() {
        guard !configured else { return }; configured = true
        let c = MPRemoteCommandCenter.shared()
        c.playCommand.addTarget { [weak self] _ in self?.onCommand?("play"); return .success }
        c.pauseCommand.addTarget { [weak self] _ in self?.onCommand?("pause"); return .success }
        c.togglePlayPauseCommand.addTarget { [weak self] _ in self?.onCommand?("toggle"); return .success }
        c.nextTrackCommand.addTarget { [weak self] _ in self?.onCommand?("next"); return .success }
        c.previousTrackCommand.addTarget { [weak self] _ in self?.onCommand?("prev"); return .success }
    }

    // MARK: Live Activity
    private var activityRef: Any?

    private func updateActivity(_ t: DreamTrack, elapsed: TimeInterval, playing: Bool) {
        #if canImport(ActivityKit)
        if UserDefaults.standard.object(forKey: "ym.liveActivity") as? Bool == false { endLiveActivity(); return }
        let auth = ActivityAuthorizationInfo()
        guard auth.areActivitiesEnabled else {
            liveActivityNote = "disabled (Settings / entitlement)"
            return
        }
        let state = YimengActivityAttributes.ContentState(
            title: t.title, artist: t.artist, cover: t.cover,
            startDate: Date().addingTimeInterval(-elapsed), duration: t.duration, isPlaying: playing
        )
        if let a = activityRef as? Activity<YimengActivityAttributes> {
            Task { await a.update(ActivityContent(state: state, staleDate: nil)) }
            liveActivityNote = "updated \(t.title)"
        } else {
            do {
                let activity = try Activity.request(
                    attributes: YimengActivityAttributes(app: "译梦"),
                    content: ActivityContent(state: state, staleDate: nil),
                    pushType: nil
                )
                activityRef = activity
                liveActivityNote = "started id=\(activity.id)"
                print("LiveActivity started:", activity.id)
            } catch {
                liveActivityNote = "error: \(error.localizedDescription)"
                print("LiveActivity:", error)
            }
        }
        #else
        liveActivityNote = "ActivityKit unavailable"
        #endif
    }
}
