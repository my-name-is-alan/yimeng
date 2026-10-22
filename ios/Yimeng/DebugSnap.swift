#if DEBUG
import UIKit
import SwiftUI

/// `-snap <name> -snapDelay <s>`: render the key window to Documents/<name>.png (simctl io is unreliable headless).
enum DebugSnap {
    static func schedule() {
        let a = ProcessInfo.processInfo.arguments
        func arg(_ k: String) -> String? { a.firstIndex(of: k).flatMap { $0 + 1 < a.count ? a[$0 + 1] : nil } }
        if a.contains("-sheet-sound") { DispatchQueue.main.asyncAfter(deadline: .now() + 2) { SheetCenter.shared.sound = true } }
        if a.contains("-fx-test") { fxTest() }
        if a.contains("-np-live") {
            Task { @MainActor in
                try? await Task.sleep(for: .seconds(6))
                let songs = LibraryCache.shared.songs
                guard !songs.isEmpty else { return }
                PlayerStore.shared.play(songs: songs, startAt: 0)
                AppStateBridge.current?.nowPlaying = true
                if let v = arg("-np-vol").flatMap(Float.init) {
                    try? await Task.sleep(for: .seconds(2))
                    VolumeController.shared.set(v)
                    NetLog.logger.info("NPTEST vol set \(v) → controller \(VolumeController.shared.value) userGain \(PlayerStore.shared.userGain) system=\(VolumeController.usesSystem)")
                }
                if let n = arg("-np-amb"), let amb = Ambience(rawValue: n) {
                    try? await Task.sleep(for: .seconds(1))
                    withAnimation(.spring(response: 0.5, dampingFraction: 0.82)) { AppStateBridge.current?.ambience = amb }
                }
            }
        }
        guard let name = arg("-snap") else { return }
        let delay = Double(arg("-snapDelay") ?? "") ?? 12
        DispatchQueue.main.asyncAfter(deadline: .now() + delay) {
            guard let w = UIApplication.shared.connectedScenes.compactMap({ $0 as? UIWindowScene }).flatMap(\.windows).first(where: \.isKeyWindow) else { return }
            let img = UIGraphicsImageRenderer(bounds: w.bounds).image { _ in w.drawHierarchy(in: w.bounds, afterScreenUpdates: true) }
            let url = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask)[0].appendingPathComponent(name + ".png")
            try? img.pngData()?.write(to: url)
        }
    }
}
extension DebugSnap {
    /// Plays the first cached song with effects ON, toggles OFF/ON mid-track, logs positions + engine state.
    static func fxTest() {
        Task { @MainActor in
            try? await Task.sleep(for: .seconds(9))
            let songs = LibraryCache.shared.songs
            guard !songs.isEmpty else { NetLog.logger.error("FXTEST no songs"); return }
            FXStore.shared.pick(FXSettings.presets[1]); FXStore.shared.s.on = true
            PlayerStore.shared.play(songs: songs, startAt: 0)
            for i in 0..<12 {
                try? await Task.sleep(for: .seconds(2))
                                NetLog.logger.info("FXTEST t\(i * 2) fx=\(FXStore.shared.s.on) pos=\(String(format: "%.1f", PlayerStore.shared.elapsed), privacy: .public) playing=\(PlayerStore.shared.isPlaying)")
                if i == 5 { FXStore.shared.s.on = false; NetLog.logger.info("FXTEST → OFF") }
                if i == 8 { FXStore.shared.s.on = true; NetLog.logger.info("FXTEST → ON") }
            }
        }
    }
}
#endif
