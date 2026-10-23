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

    /// Single scribble loop (one pass) — closed orbit for chase light / star trail.
    static func frameLoop(_ rc: CGRect, seed: UInt64, jitter: CGFloat, pass: CGFloat = 0) -> Path {
        var r = SeededRNG(seed); var p = Path()
        let o = pass * 6
        let pts = [CGPoint(x: rc.minX - o, y: rc.minY + o), CGPoint(x: rc.maxX + o, y: rc.minY - o),
                   CGPoint(x: rc.maxX - o, y: rc.maxY + o), CGPoint(x: rc.minX + o, y: rc.maxY - o), CGPoint(x: rc.minX - o + 4, y: rc.minY + o - 3)]
        p.move(to: pts[0])
        for i in 1..<pts.count {
            let a = pts[i - 1], b = pts[i]
            let mid = CGPoint(x: (a.x + b.x) / 2 + jit(&r, jitter * 2), y: (a.y + b.y) / 2 + jit(&r, jitter * 2))
            p.addQuadCurve(to: CGPoint(x: b.x + jit(&r, jitter), y: b.y + jit(&r, jitter)), control: mid)
        }
        return p
    }
}

// MARK: - Path sampling (arc-length) for particles along a doodle orbit

enum PathSampler {
    static func polyline(_ path: Path) -> [CGPoint] {
        var poly: [CGPoint] = []
        path.cgPath.applyWithBlock { ep in
            let e = ep.pointee
            switch e.type {
            case .moveToPoint:
                poly.append(e.points[0])
            case .addLineToPoint:
                poly.append(e.points[0])
            case .addQuadCurveToPoint:
                let c = e.points[0], end = e.points[1]
                let start = poly.last ?? c
                for i in 1...10 {
                    let t = CGFloat(i) / 10, u = 1 - t
                    poly.append(CGPoint(x: u * u * start.x + 2 * u * t * c.x + t * t * end.x,
                                        y: u * u * start.y + 2 * u * t * c.y + t * t * end.y))
                }
            case .addCurveToPoint:
                let c1 = e.points[0], c2 = e.points[1], end = e.points[2]
                let start = poly.last ?? c1
                for i in 1...12 {
                    let t = CGFloat(i) / 12, u = 1 - t
                    poly.append(CGPoint(
                        x: u * u * u * start.x + 3 * u * u * t * c1.x + 3 * u * t * t * c2.x + t * t * t * end.x,
                        y: u * u * u * start.y + 3 * u * u * t * c1.y + 3 * u * t * t * c2.y + t * t * t * end.y))
                }
            case .closeSubpath:
                if let f = poly.first { poly.append(f) }
            @unknown default: break
            }
        }
        return poly
    }

    static func points(_ path: Path, count: Int) -> [CGPoint] {
        let poly = polyline(path)
        guard poly.count >= 2, count >= 2 else { return poly }
        var dists: [CGFloat] = [0]
        for i in 1..<poly.count {
            dists.append(dists[i - 1] + hypot(poly[i].x - poly[i - 1].x, poly[i].y - poly[i - 1].y))
        }
        let total = dists.last!
        guard total > 0 else { return Array(poly.prefix(count)) }
        var out: [CGPoint] = []
        out.reserveCapacity(count)
        for i in 0..<count {
            let target = total * CGFloat(i) / CGFloat(count)
            var j = 1
            while j < dists.count && dists[j] < target { j += 1 }
            let j0 = min(max(j, 1), dists.count - 1)
            let seg = dists[j0] - dists[j0 - 1]
            let f = seg > 0 ? (target - dists[j0 - 1]) / seg : 0
            let a = poly[j0 - 1], b = poly[min(j0, poly.count - 1)]
            out.append(CGPoint(x: a.x + (b.x - a.x) * f, y: a.y + (b.y - a.y) * f))
        }
        return out
    }

    static func point(_ path: Path, fraction: CGFloat) -> CGPoint {
        let pts = points(path, count: 96)
        guard !pts.isEmpty else { return .zero }
        var f = fraction.truncatingRemainder(dividingBy: 1)
        if f < 0 { f += 1 }
        let idx = f * CGFloat(pts.count)
        let i0 = Int(idx) % pts.count
        let i1 = (i0 + 1) % pts.count
        let t = idx - CGFloat(Int(idx))
        return CGPoint(x: pts[i0].x + (pts[i1].x - pts[i0].x) * t,
                       y: pts[i0].y + (pts[i1].y - pts[i0].y) * t)
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


/// Mutable orbit phase updated each TimelineView tick (class so we can advance without @State thrash).
private final class DoodleOrbitClock {
    var phase: Double = 0
    var lastTick: CFTimeInterval = 0
    /// Smoothed BPM so tempo changes ease instead of snapping the angular velocity.
    var smoothedBPM: Double = 0
    /// Per-star phase offsets (set once per track) so dots are not evenly locked.
    var starOffsets: [Double] = [0, 0.17, 0.41, 0.63, 0.88]
    var starRates: [Double] = [1.0, 0.92, 1.08, 0.85, 1.12] // slight speed drift vs chase
}

/// Generative doodles seeded by the track. Drawn in with `.trim` on track change; wobble via Metal distortion.
/// Beat: E chase-light + G star trail along the cover frame (no whole-layer scale pulse).
struct DoodleLayer: View {
    let track: DreamTrack
    let slot: CoverSlot
    var ambience: Ambience = .doodle
    var intensity: Double = 0.7
    var beat = true
    @State private var drawn: CGFloat = 0
    @State private var orbitClock = DoodleOrbitClock()
    @Environment(\.accessibilityReduceMotion) private var reduceEnv
    private var calm: Bool { YMMotion.calm(reduceEnv) }

    private var shotMac: Bool { ProcessInfo.processInfo.arguments.contains("-shotMac") }
    /// Mac full-window NP: doodles sit on a dark stage — keep strokes bright (iPhone already has high contrast).
    private var strokeOpacity: Double {
        #if os(macOS)
        return min(1, 0.88 + 0.12 * intensity)
        #else
        return 0.55 + 0.25 * intensity
        #endif
    }
    private var strokeWidth: CGFloat {
        #if os(macOS)
        return track.stroke * 1.25
        #else
        return track.stroke
        #endif
    }

    /// Lemon → cyan → pink → violet (E/G palette).
    private static let chaseColors: [Color] = [
        Color(hex: 0xc4f542), Color(hex: 0x5eead4), Color(hex: 0xff7ab8), Color(hex: 0x9b6dff)
    ]

    /// Transport playing? Real AVPlayer wins; mock NP uses AppState.
    private var transportPlaying: Bool {
        let p = PlayerStore.shared
        if p.hasRealPlayback { return p.isPlaying }
        return AppStateBridge.current?.isPlaying ?? true
    }

    /// Tempo target: server BPM → live BeatStore → track.bpm.
    private func targetBPM() -> Double {
        if let s = track.serverBPM, s >= 50 { return min(168, s) }
        let r = BeatStore.read()
        if r.live, r.bpm >= 50 { return min(168, r.bpm) }
        let fb = Double(track.bpm)
        return fb >= 50 ? min(168, fb) : 96
    }

    /// One chase lap ≈ 22 beats, clamped 8…14s — slower than build 18, still continuous neon drift.
    private func lapSeconds(bpm: Double) -> Double {
        let raw = 22.0 * 60.0 / max(bpm, 50)
        return min(14, max(8, raw))
    }

    /// Continuous phase via CACurrentMediaTime + angular velocity. BPM only steers speed (lerped).
    /// Never hard-lock to PlayerStore.elapsed (that caused the PPT stutter). Freeze on pause.
    private func advanceOrbitPhase() -> Double {
        let now = CACurrentMediaTime()
        if orbitClock.lastTick == 0 {
            orbitClock.lastTick = now
            orbitClock.smoothedBPM = targetBPM()
        }
        let dt = min(0.05, max(0, now - orbitClock.lastTick)) // small clamp keeps motion silky if a frame hitch
        orbitClock.lastTick = now

        // Ease BPM toward target (~0.6s time constant) so tempo changes don't jerk the chase.
        let target = targetBPM()
        let bpmBlend = 1 - exp(-dt / 0.6)
        orbitClock.smoothedBPM += (target - orbitClock.smoothedBPM) * bpmBlend
        let lap = lapSeconds(bpm: orbitClock.smoothedBPM)

        if transportPlaying && !calm && !shotMac {
            orbitClock.phase += dt / lap
        }
        return orbitClock.phase.truncatingRemainder(dividingBy: 1)
    }

    var body: some View {
        GeometryReader { g in
            TimelineView(.animation(minimumInterval: calm ? 1 / 6 : 1 / 30)) { tl in
                let t = tl.date.timeIntervalSinceReferenceDate * (calm ? 0.08 : 1)
                let run = advanceOrbitPhase()
                // Soft 0…1 beat breath (no hard white flash). Squash so peaks are gentle.
                let beatAmt: Double = {
                    guard beat && !calm else { return 0 }
                    let raw: Double
                    if PlayerStore.shared.hasRealPlayback {
                        let pulse = BeatStore.pulse(serverBPM: track.serverBPM)
                        let energy = BeatStore.read().energy
                        raw = max(pulse * 0.85, energy * 0.25)
                    } else {
                        let bpm = orbitClock.smoothedBPM > 0 ? orbitClock.smoothedBPM : targetBPM()
                        raw = max(0, sin(t * bpm / 60 * 2 * .pi)) * 0.55
                    }
                    // Ease curve: keep most of the motion in the soft mid, cap loud peaks.
                    let soft = raw * raw * (3 - 2 * raw) // smoothstep-ish
                    return intensity * min(1, soft)
                }()
                ZStack {
                    switch ambience {
                    case .doodle:
                        // Static base ink (cover itself stays still; no whole-layer scale).
                        strokes(size: g.size)
                            .trim(from: 0, to: drawn)
                            .stroke(Color.white.opacity(strokeOpacity * 0.85), style: StrokeStyle(lineWidth: strokeWidth, lineCap: .round, lineJoin: .round))
                            .shadow(color: .white.opacity(0.35), radius: 4)
                        stars(size: g.size).opacity(Double(drawn))
                        if !calm {
                            chaseAndTrail(size: g.size, run: run, beatAmt: beatAmt)
                                .opacity(Double(drawn))
                        }
                    case .stardust: StardustHalo(track: track, slot: slot, time: t, intensity: intensity)
                    case .aurora: AuroraBands(track: track, time: t)
                    case .pure: Color.clear
                    }
                }
                #if os(iOS)
                .distortionEffect(ShaderLibrary.doodleWobble(.float(Float(t.truncatingRemainder(dividingBy: 1000))), .float(Float(track.jitter * intensity * (calm ? 0.15 : 1)))),
                                  maxSampleOffset: CGSize(width: 6, height: 6))
                #endif
            }
        }
        .allowsHitTesting(false)
        .onAppear {
            if shotMac { drawn = 1 } else { redraw() }
            resetOrbitClock()
        }
        .onChange(of: track.id) { _, _ in
            if shotMac { drawn = 1 } else { redraw() }
            resetOrbitClock()
        }
    }

    private func redraw() {
        drawn = 0
        withAnimation(.easeInOut(duration: 1.6)) { drawn = 1 }
    }

    private func resetOrbitClock() {
        let c = DoodleOrbitClock()
        c.smoothedBPM = targetBPM()
        // Deterministic but uneven spacing + speed micro-drift per track.
        var rng = SeededRNG(track.seed &+ 404)
        c.starOffsets = (0..<5).map { _ in rng.d(0, 1) }
        c.starRates = (0..<5).map { _ in rng.d(0.82, 1.18) }
        orbitClock = c
    }

    // MARK: E chase + G particles

    /// Hand-drawn cover orbit: frame loop for cloud/ink, scribble ellipse for zig.
    private func beatOrbit(size: CGSize) -> Path {
        let rc = slot.rect, s = rc.width
        switch track.motif {
        case .zig:
            return DoodlePaths.orbit(center: CGPoint(x: rc.midX + 14, y: rc.midY), rx: s * 0.62, ry: s * 0.5, seed: track.seed, jitter: track.jitter)
        case .cloud, .ink:
            return DoodlePaths.frameLoop(rc.insetBy(dx: -18, dy: -18), seed: track.seed, jitter: track.jitter, pass: 0)
        }
    }

    private func chaseColor(at phase: Double) -> Color {
        let cols = Self.chaseColors
        let n = cols.count
        let p = (phase.truncatingRemainder(dividingBy: 1) + 1).truncatingRemainder(dividingBy: 1) * Double(n)
        let i0 = Int(p) % n
        let i1 = (i0 + 1) % n
        let f = CGFloat(p - Double(Int(p)))
        return Self.lerp(cols[i0], cols[i1], f)
    }

    private static func lerp(_ a: Color, _ b: Color, _ t: CGFloat) -> Color {
        #if canImport(UIKit)
        let ua = UIColor(a), ub = UIColor(b)
        var r1: CGFloat = 0, g1: CGFloat = 0, b1: CGFloat = 0, a1: CGFloat = 0
        var r2: CGFloat = 0, g2: CGFloat = 0, b2: CGFloat = 0, a2: CGFloat = 0
        ua.getRed(&r1, green: &g1, blue: &b1, alpha: &a1)
        ub.getRed(&r2, green: &g2, blue: &b2, alpha: &a2)
        return Color(red: Double(r1 + (r2 - r1) * t), green: Double(g1 + (g2 - g1) * t), blue: Double(b1 + (b2 - b1) * t), opacity: Double(a1 + (a2 - a1) * t))
        #elseif canImport(AppKit)
        let ua = NSColor(a), ub = NSColor(b)
        guard let ca = ua.usingColorSpace(.deviceRGB), let cb = ub.usingColorSpace(.deviceRGB) else { return a }
        return Color(red: Double(ca.redComponent + (cb.redComponent - ca.redComponent) * t),
                     green: Double(ca.greenComponent + (cb.greenComponent - ca.greenComponent) * t),
                     blue: Double(ca.blueComponent + (cb.blueComponent - ca.blueComponent) * t),
                     opacity: Double(ca.alphaComponent + (cb.alphaComponent - ca.alphaComponent) * t))
        #else
        return t < 0.5 ? a : b
        #endif
    }

    @ViewBuilder
    private func chaseAndTrail(size: CGSize, run: Double, beatAmt: Double) -> some View {
        let orbit = beatOrbit(size: size)
        let runF = CGFloat(run)
        let seg: CGFloat = 0.09
        // Hue rotates continuously along the lap (and a bit faster than position for neon drift).
        let col = chaseColor(at: run * 1.35)
        let hot = beatAmt
        // Beat: only +8…15% width / slight opacity — no white slam.
        let chaseW = strokeWidth * (1.85 + 0.28 * hot)
        let chaseOpacity = 0.72 + 0.18 * intensity + 0.10 * hot

        ZStack {
            // G faint dashed color trail — soft, phase-driven.
            orbit
                .stroke(
                    col.opacity(0.45),
                    style: StrokeStyle(lineWidth: strokeWidth * 0.75, lineCap: .round, dash: [6, 18], dashPhase: -runF * 90)
                )
                .opacity(0.26 + 0.18 * intensity)
                .blur(radius: 0.45)

            // E chase segment
            chaseStroke(orbit, from: runF, length: seg, color: col, width: chaseW)
                .shadow(color: col.opacity(0.55 + 0.25 * hot), radius: 6 + 3 * hot)
                .opacity(chaseOpacity)

            // G star dots — staggered phase + slight speed drift
            starTrail(orbit: orbit, run: run, beatAmt: hot)
        }
    }

    @ViewBuilder
    private func chaseStroke(_ path: Path, from start: CGFloat, length: CGFloat, color: Color, width: CGFloat) -> some View {
        let style = StrokeStyle(lineWidth: width, lineCap: .round, lineJoin: .round)
        let s0 = start.truncatingRemainder(dividingBy: 1)
        let s = s0 < 0 ? s0 + 1 : s0
        let e = s + length
        if e <= 1 {
            path.trim(from: s, to: e).stroke(color, style: style)
        } else {
            ZStack {
                path.trim(from: s, to: 1).stroke(color, style: style)
                path.trim(from: 0, to: e - 1).stroke(color, style: style)
            }
        }
    }

    @ViewBuilder
    private func starTrail(orbit: Path, run: Double, beatAmt: Double) -> some View {
        let n = 5
        let sizes: [CGFloat] = [6.5, 4.5, 5.5, 3.8, 5]
        let offsets = orbitClock.starOffsets
        let rates = orbitClock.starRates
        ForEach(0..<n, id: \.self) { i in
            let off = i < offsets.count ? offsets[i] : Double(i) / Double(n)
            let rate = i < rates.count ? rates[i] : 1.0
            // Drift slightly faster/slower than chase; phase not evenly locked.
            let frac = CGFloat((run * rate + off).truncatingRemainder(dividingBy: 1))
            let pt = PathSampler.point(orbit, fraction: frac < 0 ? frac + 1 : frac)
            let col = chaseColor(at: run * 1.35 + off)
            let breath = 1 + 0.10 * beatAmt + 0.06 * sin(run * 2 * .pi * rate + off * 6)
            Circle()
                .fill(col.opacity(0.85 + 0.12 * beatAmt))
                .frame(width: sizes[i], height: sizes[i])
                .scaleEffect(breath)
                .shadow(color: col.opacity(0.7), radius: 5 + 2 * beatAmt)
                .position(pt)
                .blendMode(.plusLighter)
        }
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
