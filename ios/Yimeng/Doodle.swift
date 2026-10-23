import SwiftUI

// MARK: - Hand-drawn primitives (paths in a local rect; jitter from a seeded RNG → deterministic per track)

enum DoodlePaths {
    static func jit(_ r: inout SeededRNG, _ j: CGFloat) -> CGFloat { CGFloat(r.d(-1, 1)) * j }

    /// Double scribble frame around a rect.
    static func frame(_ rc: CGRect, seed: UInt64, jitter: CGFloat) -> Path {
        var r = SeededRNG(seed); var p = Path()
        for pass in 0..<2 {
            let o = CGFloat(pass) * 6
            let pts = [CGPoint(x: rc.minX - o, y: rc.minY + o), CGPoint(x: rc.maxX + o, y: rc.minY - o),
                       CGPoint(x: rc.maxX - o, y: rc.maxY + o), CGPoint(x: rc.minX + o, y: rc.maxY - o), CGPoint(x: rc.minX - o + 4, y: rc.minY + o - 3)]
            p.move(to: pts[0])
            for i in 1..<pts.count {
                let a = pts[i - 1], b = pts[i]
                let mid = CGPoint(x: (a.x + b.x) / 2 + jit(&r, jitter * 2), y: (a.y + b.y) / 2 + jit(&r, jitter * 2))
                p.addQuadCurve(to: CGPoint(x: b.x + jit(&r, jitter), y: b.y + jit(&r, jitter)), control: mid)
            }
        }
        return p
    }
    /// Elliptical scribble orbit (1.6 turns).
    static func orbit(center c: CGPoint, rx: CGFloat, ry: CGFloat, seed: UInt64, jitter: CGFloat) -> Path {
        var r = SeededRNG(seed); var p = Path(); let n = 120
        for i in 0...n {
            let a = Double(i) / Double(n) * 2 * .pi * 1.6 - 0.4
            let k = 1 + 0.05 * sin(a * 3) + Double(jit(&r, jitter)) * 0.004
            let pt = CGPoint(x: c.x + CGFloat(cos(a) * k) * rx, y: c.y + CGFloat(sin(a) * k) * ry)
            i == 0 ? p.move(to: pt) : p.addLine(to: pt)
        }
        return p
    }
    static func cloud(at o: CGPoint, w: CGFloat) -> Path {
        var p = Path(); let h = w * 0.42
        p.move(to: CGPoint(x: o.x, y: o.y + h))
        p.addCurve(to: CGPoint(x: o.x + w * 0.22, y: o.y + h * 0.35), control1: CGPoint(x: o.x - w * 0.08, y: o.y + h * 0.7), control2: CGPoint(x: o.x + w * 0.05, y: o.y + h * 0.3))
        p.addCurve(to: CGPoint(x: o.x + w * 0.62, y: o.y + h * 0.2), control1: CGPoint(x: o.x + w * 0.28, y: o.y - h * 0.25), control2: CGPoint(x: o.x + w * 0.55, y: o.y - h * 0.3))
        p.addCurve(to: CGPoint(x: o.x + w, y: o.y + h * 0.8), control1: CGPoint(x: o.x + w * 0.85, y: o.y + h * 0.05), control2: CGPoint(x: o.x + w * 1.08, y: o.y + h * 0.45))
        p.addLine(to: CGPoint(x: o.x, y: o.y + h))
        return p
    }
    static func moon(center c: CGPoint, r: CGFloat) -> Path {
        var p = Path()
        p.addArc(center: c, radius: r, startAngle: .degrees(-60), endAngle: .degrees(200), clockwise: false)
        p.addQuadCurve(to: CGPoint(x: c.x + r * cos(-.pi / 3), y: c.y + r * sin(-.pi / 3)), control: CGPoint(x: c.x + r * 0.15, y: c.y + r * 0.1))
        return p
    }
    static func sparkle(center c: CGPoint, r: CGFloat) -> Path {
        var p = Path()
        p.move(to: CGPoint(x: c.x, y: c.y - r)); p.addQuadCurve(to: CGPoint(x: c.x + r, y: c.y), control: c)
        p.addQuadCurve(to: CGPoint(x: c.x, y: c.y + r), control: c); p.addQuadCurve(to: CGPoint(x: c.x - r, y: c.y), control: c)
        p.addQuadCurve(to: CGPoint(x: c.x, y: c.y - r), control: c)
        return p
    }
    static func star(center c: CGPoint, r: CGFloat, rot: Double) -> Path {
        var p = Path()
        for i in 0...10 {
            let a = rot + Double(i) * .pi / 5 - .pi / 2; let rr = i % 2 == 0 ? r : r * 0.45
            let pt = CGPoint(x: c.x + CGFloat(cos(a)) * rr, y: c.y + CGFloat(sin(a)) * rr)
            i == 0 ? p.move(to: pt) : p.addLine(to: pt)
        }
        return p
    }
    static func lightning(at o: CGPoint, h: CGFloat) -> Path {
        var p = Path()
        p.move(to: o); p.addLine(to: CGPoint(x: o.x - h * 0.3, y: o.y + h * 0.5)); p.addLine(to: CGPoint(x: o.x + h * 0.05, y: o.y + h * 0.48))
        p.addLine(to: CGPoint(x: o.x - h * 0.2, y: o.y + h)); return p
    }
    static func zigline(from o: CGPoint, length: CGFloat, amp: CGFloat, step: CGFloat) -> Path {
        var p = Path(); p.move(to: o); var x: CGFloat = 0; var up = true
        while x < length { x += step; p.addLine(to: CGPoint(x: o.x + x, y: o.y + (up ? -amp : amp))); up.toggle() }
        return p
    }
    static func flower(center c: CGPoint, r: CGFloat, petals: Int) -> Path {
        var p = Path()
        for i in 0..<petals {
            let a = Double(i) / Double(petals) * 2 * .pi
            let tip = CGPoint(x: c.x + CGFloat(cos(a)) * r, y: c.y + CGFloat(sin(a)) * r)
            let l = CGPoint(x: c.x + CGFloat(cos(a - 0.45)) * r * 0.7, y: c.y + CGFloat(sin(a - 0.45)) * r * 0.7)
            let rr = CGPoint(x: c.x + CGFloat(cos(a + 0.45)) * r * 0.7, y: c.y + CGFloat(sin(a + 0.45)) * r * 0.7)
            p.move(to: c); p.addQuadCurve(to: tip, control: l); p.addQuadCurve(to: c, control: rr)
        }
        p.addEllipse(in: CGRect(x: c.x - r * 0.18, y: c.y - r * 0.18, width: r * 0.36, height: r * 0.36))
        return p
    }
    static func loops(from o: CGPoint, length: CGFloat, r: CGFloat) -> Path {
        var p = Path(); let n = 160
        for i in 0...n {
            let t = Double(i) / Double(n); let a = t * 2 * .pi * 5
            let pt = CGPoint(x: o.x + CGFloat(t) * length + CGFloat(cos(a)) * r, y: o.y + CGFloat(sin(a)) * r)
            i == 0 ? p.move(to: pt) : p.addLine(to: pt)
        }
        return p
    }
    static func drip(top o: CGPoint, length: CGFloat) -> Path {
        var p = Path(); p.move(to: o); p.addLine(to: CGPoint(x: o.x, y: o.y + length))
        p.addEllipse(in: CGRect(x: o.x - 3.5, y: o.y + length, width: 7, height: 9)); return p
    }
}

// MARK: - Composition layer

struct CoverSlot { let rect: CGRect; let radius: CGFloat }

extension DreamLayout {
    /// Cover placement in a 402-pt-wide design canvas (scaled to width at runtime).
    func slot(width w: CGFloat) -> CoverSlot {
        let k = w / 402
        switch self {
        case .frame: return CoverSlot(rect: CGRect(x: 51 * k, y: 150 * k, width: 300 * k, height: 300 * k), radius: 10)
        case .orbit: return CoverSlot(rect: CGRect(x: 40 * k, y: 180 * k, width: 230 * k, height: 230 * k), radius: 999)
        case .drip:  return CoverSlot(rect: CGRect(x: 128 * k, y: 136 * k, width: 232 * k, height: 232 * k), radius: 24)
        }
    }
    var titleY: CGFloat { switch self { case .frame: 530; case .orbit: 560; case .drip: 540 } }
    var centered: Bool { self == .frame }
}

/// Generative doodles seeded by the track. Drawn in with `.trim` on track change; wobble via Metal distortion; optional beat pulse.
struct DoodleLayer: View {
    let track: DreamTrack
    let slot: CoverSlot
    var ambience: Ambience = .doodle
    var intensity: Double = 0.7
    var beat = true
    @State private var drawn: CGFloat = 0
    @Environment(\.accessibilityReduceMotion) private var reduceEnv
    private var calm: Bool { YMMotion.calm(reduceEnv) }

    private var shotMac: Bool { ProcessInfo.processInfo.arguments.contains("-shotMac") }
    /// Mac full-window NP: doodles sit on a dark stage — keep strokes bright (iPhone already has high contrast).
    private var strokeOpacity: Double {
        #if os(macOS)
        return min(1, 0.88 + 0.12 * intensity)
        #else
        return 0.6 + 0.3 * intensity
        #endif
    }
    private var strokeWidth: CGFloat {
        #if os(macOS)
        return track.stroke * 1.25
        #else
        return track.stroke
        #endif
    }

    var body: some View {
        GeometryReader { g in
            // Reduce Motion: no beat pulse, wobble slowed to a near-still drift, low frame rate.
            TimelineView(.animation(minimumInterval: calm ? 1 / 6 : 1 / 30)) { tl in
                let t = tl.date.timeIntervalSinceReferenceDate * (calm ? 0.08 : 1)
                let pulse = (beat && !calm) ? 1 + (PlayerStore.shared.hasRealPlayback ? 0.04 * intensity * BeatStore.pulse(serverBPM: track.serverBPM) : 0.025 * intensity * max(0, sin(t * Double(track.bpm) / 60 * 2 * .pi))) : 1
                ZStack {
                    switch ambience {
                    case .doodle:
                        strokes(size: g.size)
                            .trim(from: 0, to: drawn)
                            .stroke(Color.white.opacity(strokeOpacity), style: StrokeStyle(lineWidth: strokeWidth, lineCap: .round, lineJoin: .round))
                            .shadow(color: .white.opacity(0.65), radius: 5)
                        stars(size: g.size).opacity(Double(drawn))
                    case .stardust: StardustHalo(track: track, slot: slot, time: t, intensity: intensity)
                    case .aurora: AuroraBands(track: track, time: t)
                    case .pure: Color.clear
                    }
                }
                .scaleEffect(pulse, anchor: UnitPoint(x: slot.rect.midX / max(g.size.width, 1), y: slot.rect.midY / max(g.size.height, 1)))
                #if os(iOS)
                .distortionEffect(ShaderLibrary.doodleWobble(.float(Float(t.truncatingRemainder(dividingBy: 1000))), .float(Float(track.jitter * intensity))),
                                  maxSampleOffset: CGSize(width: 6, height: 6))
                #endif
            }
        }
        .allowsHitTesting(false)
        .onAppear {
            if shotMac { drawn = 1 } else { redraw() }
        }
        .onChange(of: track.id) { _, _ in
            if shotMac { drawn = 1 } else { redraw() }
        }
    }

    private func redraw() {
        drawn = 0
        withAnimation(.easeInOut(duration: 1.6)) { drawn = 1 }
    }

    /// All line-art for the motif, merged into one Path so a single trim draws it in.
    func strokes(size: CGSize) -> Path {
        let rc = slot.rect, s = rc.width, x = rc.minX, y = rc.minY
        var p = Path()
        switch track.motif {
        case .cloud:
            p.addPath(DoodlePaths.frame(rc.insetBy(dx: -18, dy: -18), seed: track.seed, jitter: track.jitter))
            p.addPath(DoodlePaths.frame(rc.insetBy(dx: -36, dy: -36), seed: track.seed &+ 7, jitter: track.jitter * 1.1))
            p.addPath(DoodlePaths.cloud(at: CGPoint(x: x - 56, y: y + 36), w: 120))
            p.addPath(DoodlePaths.cloud(at: CGPoint(x: x + s - 36, y: y + s + 28), w: 100))
            p.addPath(DoodlePaths.moon(center: CGPoint(x: x + s + 14, y: y - 14), r: 18))
            p.addPath(DoodlePaths.moon(center: CGPoint(x: x - 28, y: y + s + 72), r: 12))
            p.addPath(DoodlePaths.sparkle(center: CGPoint(x: x + s - 12, y: y - 48), r: 9))
            p.addPath(DoodlePaths.sparkle(center: CGPoint(x: x + 24, y: y - 28), r: 5))
        case .zig:
            p.addPath(DoodlePaths.orbit(center: CGPoint(x: rc.midX + 14, y: rc.midY), rx: s * 0.62, ry: s * 0.5, seed: track.seed, jitter: track.jitter))
            p.addPath(DoodlePaths.lightning(at: CGPoint(x: x + s - 10, y: y - 34), h: 46))
            p.addPath(DoodlePaths.lightning(at: CGPoint(x: x - 16, y: y + s - 20), h: 32))
            p.addPath(DoodlePaths.zigline(from: CGPoint(x: x + s + 6, y: y + s * 0.55), length: min(80, size.width - x - s - 10), amp: 10, step: 14))
        case .ink:
            p.addPath(DoodlePaths.flower(center: CGPoint(x: x + s - 6, y: y + 8), r: 22, petals: 6))
            p.addPath(DoodlePaths.flower(center: CGPoint(x: x + 10, y: y + s - 6), r: 15, petals: 5))
            p.addPath(DoodlePaths.loops(from: CGPoint(x: x - 30, y: y - 24), length: 120, r: 9))
            for (i, dx) in [0.22, 0.48, 0.71].enumerated() {
                p.addPath(DoodlePaths.drip(top: CGPoint(x: x + s * dx, y: y + s + 4), length: CGFloat(22 + (i * 14) % 30 + 10)))
            }
            p.addPath(DoodlePaths.flower(center: CGPoint(x: size.width - 72, y: y + s + 120), r: 10, petals: 5))
        }
        return p
    }

    func stars(size: CGSize) -> some View {
        var r = SeededRNG(track.seed &+ 99)
        let rc = slot.rect
        let items: [(CGPoint, CGFloat, Double, Double)] = track.motif == .zig ? (0..<track.density).map { _ in
            (CGPoint(x: r.d(20, Double(size.width) - 20), y: r.d(Double(rc.minY) - 50, Double(rc.maxY) + 60)), CGFloat(r.d(4, 10)), r.d(0.5, 0.95), r.d(0, 1))
        } : []
        return ZStack {
            ForEach(Array(items.enumerated()), id: \.offset) { _, it in
                DoodlePaths.star(center: it.0, r: it.1, rot: it.3).stroke(Color.white.opacity(it.2), lineWidth: track.stroke * 0.8)
            }
        }
    }
}

struct StardustHalo: View {
    let track: DreamTrack; let slot: CoverSlot; let time: Double; let intensity: Double
    var body: some View {
        Canvas { ctx, _ in
            var r = SeededRNG(track.seed)
            let c = CGPoint(x: slot.rect.midX, y: slot.rect.midY), R = slot.rect.width * 0.68
            let cols = track.blobs + [.white, .white]
            for _ in 0..<200 {
                let a = r.d(0, 2 * .pi) + time * 0.05
                let rr = R * (1 + 0.08 * sin(a * 7)) + r.d(-10, 10) + pow(r.d(0, 1), 3) * 60
                let s = r.d(0.6, 2.4)
                let pt = CGPoint(x: c.x + CGFloat(cos(a) * rr), y: c.y + CGFloat(sin(a) * rr))
                let col = cols[Int(r.d(0, Double(cols.count) - 0.001))]
                ctx.fill(Path(ellipseIn: CGRect(x: pt.x - s, y: pt.y - s, width: s * 2, height: s * 2)), with: .color(col.opacity(r.d(0.35, 1) * (0.5 + 0.5 * intensity))))
            }
        }
        .blendMode(.plusLighter)
    }
}

struct AuroraBands: View {
    let track: DreamTrack; let time: Double
    var body: some View {
        GeometryReader { g in
            ZStack {
                Capsule().fill(LinearGradient(colors: [.clear, track.blobs[1].opacity(0.66), Color(hex: 0x7af0c8).opacity(0.66), .clear], startPoint: .leading, endPoint: .trailing))
                    .frame(width: g.size.width * 1.4, height: 180).rotationEffect(.degrees(-14 + sin(time * 0.3) * 3)).position(x: g.size.width / 2, y: 170)
                Capsule().fill(LinearGradient(colors: [.clear, Color(hex: 0x9b7bff).opacity(0.66), track.blobs[0].opacity(0.6), .clear], startPoint: .leading, endPoint: .trailing))
                    .frame(width: g.size.width * 1.4, height: 120).rotationEffect(.degrees(-8 + cos(time * 0.25) * 3)).position(x: g.size.width / 2, y: 280)
            }.blur(radius: 30)
        }
    }
}
