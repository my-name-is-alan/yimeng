import SwiftUI

/// Full-bleed palette colour field. iOS 18: slowly breathing MeshGradient. iOS 17: blurred colour circles.
struct DreamField: View {
    let track: DreamTrack
    var animated = true
    var body: some View {
        ZStack {
            track.base
            if #available(iOS 18.0, *) {
                TimelineView(.animation(minimumInterval: 1 / 30, paused: !animated)) { tl in
                    let t = Float(tl.date.timeIntervalSinceReferenceDate.truncatingRemainder(dividingBy: 600))
                    MeshGradient(width: 3, height: 3, points: Self.points(t), colors: meshColors)
                }
            } else {
                GeometryReader { g in
                    let w = g.size.width, h = g.size.height
                    ZStack {
                        Circle().fill(track.blobs[0]).frame(width: w * 1.05).position(x: w * 0.1, y: h * 0.08)
                        Circle().fill(track.blobs[1]).frame(width: w * 0.95).position(x: w * 0.9, y: h * 0.32)
                        Circle().fill(track.blobs[2]).frame(width: w * 1.0).position(x: w * 0.05, y: h * 0.68)
                        Circle().fill(track.blobs[3]).frame(width: w * 0.9).position(x: w * 0.95, y: h * 0.92)
                    }.blur(radius: 70)
                }
            }
            if animated && PlayerStore.shared.hasRealPlayback {
                // Background breathing on the beat (doodles + field only; controls/text never move).
                TimelineView(.animation(minimumInterval: 1 / 30)) { _ in
                    let p = BeatStore.pulse(serverBPM: track.serverBPM)
                    RadialGradient(colors: [track.blobs[0].opacity(0.22 * p), .clear], center: .center, startRadius: 20, endRadius: 420)
                        .blendMode(.screen)
                }.allowsHitTesting(false)
            }
            // soft haze so text stays legible
            LinearGradient(colors: [.black.opacity(0.10), .clear, .black.opacity(0.28)], startPoint: .top, endPoint: .bottom)
        }
        .ignoresSafeArea()
    }
    private var meshColors: [Color] {
        let b = track.blobs
        return [b[0], b[1], b[3], b[2], track.base, b[0], b[3], b[2], b[1]]
    }
    static func points(_ t: Float) -> [SIMD2<Float>] {
        let s = { (k: Float) in 0.08 * sin(t * 0.35 + k) }
        return [[0, 0], [0.5, 0], [1, 0],
                [0, 0.5 + s(1)], [0.5 + s(2), 0.5 + s(3)], [1, 0.5 + s(4)],
                [0, 1], [0.5 + s(5), 1], [1, 1]]
    }
}
