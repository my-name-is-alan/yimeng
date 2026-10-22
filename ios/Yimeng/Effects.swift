import AVFoundation
import AudioToolbox
import Accelerate
import MediaToolbox
import SwiftUI
#if canImport(UIKit)
import UIKit
#endif

// MARK: - Settings

/// 音效 (ViPER-style) settings. OFF → AVPlayer passthrough (bit-perfect, Dolby/Atmos untouched).
/// ON → MTAudioProcessingTap on the streaming AVPlayerItem: preamp → 12 biquads (10 graphic + bass shelf + vocal) → limiter.
struct FXSettings: Codable, Equatable {
    var on = false
    var preset = "平直"
    var bands: [Double] = Array(repeating: 0, count: 10)   // dB, ±12
    var bass: Double = 0          // 0…12 dB low shelf @ 80 Hz
    var vocal: Double = 0         // 0…8 dB presence @ 2.8 kHz
    var preamp: Double = 0        // dB, −12…+6
    var autoHeadroom = true       // preamp = −(max boost) so the EQ can't clip
    var limiter = true

    static let freqs: [Float] = [31, 62, 125, 250, 500, 1000, 2000, 4000, 8000, 16000]
    static let freqLabels = ["31", "62", "125", "250", "500", "1k", "2k", "4k", "8k", "16k"]

    struct Preset { let name: String; let bands: [Double]; let bass: Double; let vocal: Double }
    static let presets: [Preset] = [
        .init(name: "平直", bands: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0], bass: 0, vocal: 0),
        .init(name: "超重低音", bands: [7, 6, 4.5, 2, 0, -1, 0, 0, 1, 1], bass: 6, vocal: 0),
        .init(name: "清澈人声", bands: [-3, -2, -1, 1, 3, 4, 4, 2, 1, 0], bass: 0, vocal: 4),
        .init(name: "现场", bands: [2, 1, 0, -1, 0, 1.5, 2.5, 3, 3, 2], bass: 1, vocal: 1.5),
        .init(name: "摇滚", bands: [5, 4, 2, -1, -2, -1, 2, 4, 5, 5], bass: 2, vocal: 0),
        .init(name: "古典", bands: [4, 3, 2, 0, -1, -1, 0, 2, 3, 4], bass: 0, vocal: 0),
        .init(name: "流行", bands: [-1, 1, 3, 4, 3, 0, -1, -1, 1, 2], bass: 1, vocal: 2),
        .init(name: "电子", bands: [6, 5, 2, 0, -2, 1, 0, 2, 4, 5], bass: 3, vocal: 0),
        .init(name: "爵士", bands: [3, 2, 1, 2, -1, -1, 0, 1, 2, 3], bass: 0, vocal: 1),
        .init(name: "耳机增强", bands: [3, 2.5, 1, 0, -0.5, 0, 1.5, 3, 2, 3.5], bass: 2, vocal: 1),
        .init(name: "夜间", bands: [-2, -1, 0, 0, 1, 1, 0, -2, -3, -4], bass: 0, vocal: 1),
    ]

    /// Effective pre-gain: user preamp, or automatic headroom = −(largest boost along the chain).
    var effectivePreamp: Double {
        guard autoHeadroom else { return preamp }
        let peak = max(0, (bands.max() ?? 0)) + bass * 0.7 + vocal * 0.5
        return min(preamp, -peak)
    }

    static let key = "ym.fx.v1"
    static func load() -> FXSettings {
        (UserDefaults.standard.data(forKey: key)).flatMap { try? JSONDecoder().decode(FXSettings.self, from: $0) } ?? FXSettings()
    }
    func save() { if let d = try? JSONEncoder().encode(self) { UserDefaults.standard.set(d, forKey: Self.key) } }
}

@MainActor
final class FXStore: ObservableObject {
    static let shared = FXStore()
    @Published var s = FXSettings.load() {
        didSet {
            guard s != oldValue else { return }
            s.save()
            FXParams.shared.set(s)
            if s.on != oldValue.on { PlayerStore.shared.effectsToggled() }
        }
    }
    /// Set when the current track can't take effects (e.g. E-AC-3 / Atmos → passthrough).
    @Published var note: String?
    func pick(_ p: FXSettings.Preset) { var n = s; n.preset = p.name; n.bands = p.bands; n.bass = p.bass; n.vocal = p.vocal; s = n }
}

// MARK: - Tap DSP (streaming)

/// Effects on the *streaming* AVPlayer: an MTAudioProcessingTap on the item's audioMix receives decoded PCM,
/// we run preamp → 12 biquads (vDSP_biquad, per channel, any channel count) → look-ahead-free peak limiter.
/// Toggling = attach / detach the audioMix on the live item (no restart). OFF = no audioMix = passthrough.
final class FXParams: @unchecked Sendable {
    static let shared = FXParams()
    private let lock = NSLock()
    private var _s = FXSettings.load()
    private(set) var version = 0
    func set(_ s: FXSettings) { lock.lock(); _s = s; version &+= 1; lock.unlock() }
    func get() -> (FXSettings, Int) { lock.lock(); defer { lock.unlock() }; return (_s, version) }

    /// RBJ cookbook biquads normalised for vDSP: [b0, b1, b2, a1, a2] per section.
    static func coefficients(_ s: FXSettings, sampleRate fs: Double) -> [Double] {
        var out: [Double] = []
        func peak(_ f: Double, _ g: Double, _ bw: Double) {
            guard g != 0, f < fs * 0.45 else { return }
            let A = pow(10, g / 40), w = 2 * .pi * f / fs, sn = sin(w), cs = cos(w)
            let al = sn * sinh(log(2) / 2 * bw * w / sn)
            let a0 = 1 + al / A
            out += [(1 + al * A) / a0, (-2 * cs) / a0, (1 - al * A) / a0, (-2 * cs) / a0, (1 - al / A) / a0]
        }
        func lowShelf(_ f: Double, _ g: Double) {
            guard g != 0 else { return }
            let A = pow(10, g / 40), w = 2 * .pi * f / fs, sn = sin(w), cs = cos(w)
            let al = sn / 2 * sqrt(2.0), sA = 2 * sqrt(A) * al
            let a0 = (A + 1) + (A - 1) * cs + sA
            out += [A * ((A + 1) - (A - 1) * cs + sA) / a0, 2 * A * ((A - 1) - (A + 1) * cs) / a0,
                    A * ((A + 1) - (A - 1) * cs - sA) / a0, -2 * ((A - 1) + (A + 1) * cs) / a0,
                    ((A + 1) + (A - 1) * cs - sA) / a0]
        }
        for (i, f) in FXSettings.freqs.enumerated() { peak(Double(f), s.bands[i], 1.0) }
        lowShelf(80, s.bass)
        peak(2800, s.vocal, 1.4)
        return out
    }
}

/// Per-tap state (lives in the tap's storage; touched only on the render thread after prepare).
final class FXTapState {
    var channels = 0
    var interleaved = false
    var sampleRate = 44100.0
    var supported = false
    var version = -1
    var setup: vDSP_biquad_Setup?
    var sections = 0
    var delays: [[Float]] = []
    var preamp: Float = 1
    var limiterOn = true
    var gain: Float = 1          // limiter gain, smoothed
    /// false → analysis-only tap (FX off): beat/energy read, samples untouched.
    var fxOn = true
    let beat = BeatAnalyzer()

    func refresh() {
        let (s, v) = FXParams.shared.get()
        guard v != version else { return }
        version = v
        if let setup { vDSP_biquad_DestroySetup(setup) }
        let c = FXParams.coefficients(s, sampleRate: sampleRate)
        sections = c.count / 5
        setup = sections > 0 ? c.withUnsafeBufferPointer { vDSP_biquad_CreateSetup($0.baseAddress!, vDSP_Length(sections)) } : nil
        delays = Array(repeating: Array(repeating: 0, count: 2 * sections + 2), count: max(channels, 1))
        preamp = Float(pow(10, s.effectivePreamp / 20))
        limiterOn = s.limiter
    }

    func process(_ abl: UnsafeMutableAudioBufferListPointer, frames n: Int) {
        guard supported, n > 0 else { return }
        if !fxOn {
            if let b = abl.first, let d = b.mData?.assumingMemoryBound(to: Float.self) { beat.feed(d, stride: max(Int(b.mNumberChannels), 1), frames: n) }
            return
        }
        refresh()
        // Gather channel pointers + strides (non-interleaved: one buffer per channel; interleaved: stride = channels).
        var ch: [(UnsafeMutablePointer<Float>, Int)] = []
        for b in abl {
            guard let d = b.mData?.assumingMemoryBound(to: Float.self) else { continue }
            let k = Int(b.mNumberChannels)
            if k <= 1 { ch.append((d, 1)) } else { for c in 0..<k { ch.append((d + c, k)) } }
        }
        if ch.count != delays.count { delays = Array(repeating: Array(repeating: 0, count: 2 * sections + 2), count: ch.count) }
        var pre = preamp
        for (i, (p, st)) in ch.enumerated() {
            vDSP_vsmul(p, st, &pre, p, st, vDSP_Length(n))
            if let setup { delays[i].withUnsafeMutableBufferPointer { vDSP_biquad(setup, $0.baseAddress!, p, st, p, st, vDSP_Length(n)) } }
        }
        if let (p0, st0) = ch.first { beat.feed(p0, stride: st0, frames: n) }
        guard limiterOn else { return }
        // Block peak limiter (64-frame blocks, instant attack, ~80 ms release), ceiling −0.3 dBFS, linked channels.
        let ceil: Float = 0.966, rel = Float(exp(-64.0 / (0.08 * sampleRate)))
        var f = 0
        while f < n {
            let m = min(64, n - f)
            var peak: Float = 0
            for (p, st) in ch { var mx: Float = 0; vDSP_maxmgv(p + f * st, st, &mx, vDSP_Length(m)); peak = max(peak, mx) }
            let target: Float = peak * gain > ceil ? ceil / max(peak, 1e-9) : 1
            let g0 = gain
            gain = target < gain ? target : min(1, target - (target - gain) * rel)
            var start = g0, step = (gain - g0) / Float(m)
            if target < g0 { start = gain; step = 0 }       // attack: clamp the whole block
            if start != 1 || step != 0 {
                for (p, st) in ch { vDSP_vrampmul(p + f * st, st, &start, &step, p + f * st, st, vDSP_Length(m)); start -= step * Float(m) }
            }
            f += m
        }
    }
    deinit { if let setup { vDSP_biquad_DestroySetup(setup) } }
}

enum FXTap {
    /// Formats the tap can't see as PCM (bitstreamed / spatial Dolby): keep passthrough.
    static func unsupported(_ s: NDSong?) -> Bool {
        let x = ((s?.suffix ?? "") + " " + (s?.contentType ?? "")).lowercased()
        return ["eac3", "ec3", "ac3", "ac-3", "e-ac-3", "atmos", "truehd", "mlp", "dts"].contains { x.contains($0) }
    }

    /// Build an audioMix with our tap for the item's first audio track.
    static func makeMix(for item: AVPlayerItem, fx: Bool = true) async -> AVAudioMix? {
        guard let track = try? await item.asset.loadTracks(withMediaType: .audio).first else { return nil }
        var cb = MTAudioProcessingTapCallbacks(
            version: kMTAudioProcessingTapCallbacksVersion_0, clientInfo: fx ? UnsafeMutableRawPointer(bitPattern: 1) : nil,
            init: { _, info, storageOut in
                let s = FXTapState(); s.fxOn = info != nil
                storageOut.pointee = Unmanaged.passRetained(s).toOpaque()
            },
            finalize: { tap in
                Unmanaged<FXTapState>.fromOpaque(MTAudioProcessingTapGetStorage(tap)).release()
            },
            prepare: { tap, _, fmt in
                let st = Unmanaged<FXTapState>.fromOpaque(MTAudioProcessingTapGetStorage(tap)).takeUnretainedValue()
                let f = fmt.pointee
                st.channels = Int(f.mChannelsPerFrame)
                st.sampleRate = f.mSampleRate
                st.interleaved = f.mFormatFlags & kAudioFormatFlagIsNonInterleaved == 0
                st.supported = f.mFormatID == kAudioFormatLinearPCM && f.mFormatFlags & kAudioFormatFlagIsFloat != 0 && f.mBitsPerChannel == 32
                st.version = -1
                st.beat.prepare(sampleRate: f.mSampleRate)
                NetLog.logger.info("FX tap prepare ch=\(st.channels) sr=\(Int(st.sampleRate)) float=\(st.supported) interleaved=\(st.interleaved)")
            },
            unprepare: nil,
            process: { tap, frames, _, abl, framesOut, flagsOut in
                guard MTAudioProcessingTapGetSourceAudio(tap, frames, abl, flagsOut, nil, framesOut) == noErr else { return }
                let st = Unmanaged<FXTapState>.fromOpaque(MTAudioProcessingTapGetStorage(tap)).takeUnretainedValue()
                st.process(UnsafeMutableAudioBufferListPointer(abl), frames: Int(framesOut.pointee))
            })
        var tapOut: MTAudioProcessingTap?
        guard MTAudioProcessingTapCreate(kCFAllocatorDefault, &cb, kMTAudioProcessingTapCreationFlag_PostEffects, &tapOut) == noErr,
              let tap = tapOut else { return nil }
        let p = AVMutableAudioMixInputParameters(track: track)
        p.audioTapProcessor = tap
        let mix = AVMutableAudioMix()
        mix.inputParameters = [p]
        return mix
    }

    /// Attach / detach on the live item — no rebuild, position untouched.
    @MainActor static func apply(on: Bool, item: AVPlayerItem?, song: NDSong?) async -> Bool {
        guard let item else { return false }
        if unsupported(song) { item.audioMix = nil; BeatStore.reset(); return !on }   // bitstream: no PCM to analyze either
        // FX off: a lightweight analysis-only tap (beat breathing for the doodles); samples pass through untouched.
        guard on else { item.audioMix = await makeMix(for: item, fx: false); return true }
        guard let mix = await makeMix(for: item) else { item.audioMix = nil; return false }
        item.audioMix = mix
        return true
    }
}

// MARK: - UI

/// Effects panel (lives inside 音效设置). Quiet, solid fills, lime only on the live thing.
struct EffectsPanel: View {
    @ObservedObject private var fx = FXStore.shared
    @Environment(\.colorScheme) var scheme

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text("音效").font(.system(size: 17, weight: .semibold))
                    Text(fx.s.on ? "已开启 · \(fx.s.preset)" : "关闭 · 原始直通").font(.system(size: 12)).foregroundStyle(Theme.secondary(scheme))
                }
                Spacer()
                Toggle("", isOn: Binding(get: { fx.s.on }, set: { v in withAnimation(.spring(response: 0.4, dampingFraction: 0.85)) { fx.s.on = v } }))
                    .labelsHidden().toggleStyle(YMToggleStyle()).fixedSize()
            }
            if fx.s.on {
                HStack(alignment: .top, spacing: 8) {
                    Image(systemName: "info.circle").font(.system(size: 12))
                    Text(fx.note ?? "音效开启时直通与杜比全景声空间音频不可用（仍为边下边播）。多声道（5.1 / 7.1）逐声道处理。")
                        .font(.system(size: 12))
                }
                .foregroundStyle(Theme.secondary(scheme))
                .transition(.opacity.combined(with: .move(edge: .top)))
            }

            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 8) {
                    ForEach(FXSettings.presets, id: \.name) { p in
                        let on = fx.s.preset == p.name
                        Button { withAnimation(.spring(response: 0.45, dampingFraction: 0.78)) { fx.pick(p) } } label: {
                            HStack(spacing: 5) {
                                if on { Circle().fill(Theme.accent(scheme)).frame(width: 5, height: 5) }
                                Text(p.name).font(.system(size: 13, weight: on ? .semibold : .regular))
                            }
                            .padding(.horizontal, 13).frame(height: 32)
                            .foregroundStyle(on ? Theme.base(scheme) : Theme.fg(scheme))
                            .background(Capsule().fill(on ? Theme.fg(scheme) : Theme.fill(scheme)))
                        }.buttonStyle(.plain)
                    }
                }
            }

            EQCurve(bands: Binding(get: { fx.s.bands }, set: { v in var n = fx.s; n.bands = v; n.preset = "自定义"; fx.s = n }))
                .frame(height: 170)
                .opacity(fx.s.on ? 1 : 0.55)

            knob("低音增强", value: Binding(get: { fx.s.bass }, set: { fx.s.bass = $0 }), range: 0...12, fmt: "+%.0f dB")
            knob("人声清晰", value: Binding(get: { fx.s.vocal }, set: { fx.s.vocal = $0 }), range: 0...8, fmt: "+%.0f dB")
            Toggle("自动预留余量（防削波）", isOn: Binding(get: { fx.s.autoHeadroom }, set: { fx.s.autoHeadroom = $0 }))
                .font(.system(size: 15)).tint(Theme.accent(scheme))
            knob("前级增益", value: Binding(get: { fx.s.preamp }, set: { fx.s.preamp = $0 }), range: -12...6, fmt: "%+.0f dB",
                 note: fx.s.autoHeadroom ? String(format: "实际 %+.1f dB", fx.s.effectivePreamp) : nil)
            Toggle("峰值限制器", isOn: Binding(get: { fx.s.limiter }, set: { fx.s.limiter = $0 }))
                .font(.system(size: 15)).tint(Theme.accent(scheme))
        }
        .padding(.vertical, 6)
    }

    private func knob(_ t: String, value: Binding<Double>, range: ClosedRange<Double>, fmt: String, note: String? = nil) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(t).font(.system(size: 15))
                Spacer()
                if let note { Text(note).font(.system(size: 11)).foregroundStyle(Theme.secondary(scheme)) }
                Text(String(format: fmt, value.wrappedValue)).font(.system(size: 13).monospacedDigit()).foregroundStyle(Theme.secondary(scheme))
            }
            Slider(value: value, in: range, step: 0.5).tint(Theme.fg(scheme).opacity(0.7))
        }
    }
}

/// Smooth EQ curve with 10 draggable band knobs (±12 dB). Lime marks only the knob under your finger.
struct EQCurve: View {
    @Binding var bands: [Double]
    @Environment(\.colorScheme) var scheme
    @State private var dragging: Int?

    var body: some View {
        GeometryReader { g in
            let w = g.size.width, h = g.size.height - 18
            let pts = bands.enumerated().map { i, v in
                CGPoint(x: w * (CGFloat(i) + 0.5) / 10, y: h / 2 - CGFloat(v / 12) * (h / 2 - 8))
            }
            ZStack(alignment: .topLeading) {
                // grid: 0 dB line + ±6
                ForEach([-6.0, 0, 6], id: \.self) { d in
                    Path { p in let y = h / 2 - CGFloat(d / 12) * (h / 2 - 8); p.move(to: .init(x: 0, y: y)); p.addLine(to: .init(x: w, y: y)) }
                        .stroke(Theme.divider(scheme), style: StrokeStyle(lineWidth: d == 0 ? 1 : 0.5, dash: d == 0 ? [] : [3, 4]))
                }
                Self.smooth(pts, w: w).stroke(Theme.fg(scheme).opacity(0.85), style: StrokeStyle(lineWidth: 2, lineCap: .round))
                Self.smooth(pts, w: w, closeTo: h).fill(Theme.fg(scheme).opacity(0.06))
                ForEach(0..<10, id: \.self) { i in
                    let on = dragging == i
                    Circle().fill(on ? Theme.accent(scheme) : Theme.base(scheme))
                        .overlay(Circle().stroke(Theme.fg(scheme).opacity(0.8), lineWidth: 1.5))
                        .frame(width: on ? 16 : 11, height: on ? 16 : 11)
                        .position(pts[i])
                    Text(FXSettings.freqLabels[i]).font(.system(size: 9)).foregroundStyle(Theme.secondary(scheme))
                        .position(x: pts[i].x, y: h + 10)
                    if on {
                        Text(String(format: "%+.1f", bands[i])).font(.system(size: 10, weight: .semibold).monospacedDigit())
                            .foregroundStyle(Theme.fg(scheme)).position(x: pts[i].x, y: max(8, pts[i].y - 18))
                    }
                }
            }
            .contentShape(Rectangle())
            .gesture(DragGesture(minimumDistance: 0)
                .onChanged { v in
                    let i = dragging ?? max(0, min(9, Int(v.startLocation.x / (w / 10))))
                    if dragging == nil {
                        withAnimation(.spring(response: 0.25, dampingFraction: 0.7)) { dragging = i }
                        #if os(iOS)
                        UISelectionFeedbackGenerator().selectionChanged()
                        #endif
                    }
                    let db = ((h / 2 - v.location.y) / (h / 2 - 8)) * 12
                    var b = bands; b[i] = (max(-12, min(12, Double(db))) * 2).rounded() / 2
                    if b != bands { bands = b }
                }
                .onEnded { _ in withAnimation(.spring(response: 0.35, dampingFraction: 0.8)) { dragging = nil } })
        }
    }

    /// Catmull-Rom → cubic Bézier through the band points, flat to the edges.
    static func smooth(_ p: [CGPoint], w: CGFloat, closeTo bottom: CGFloat? = nil) -> Path {
        var path = Path()
        guard let f = p.first, let l = p.last else { return path }
        let all = [CGPoint(x: 0, y: f.y)] + p + [CGPoint(x: w, y: l.y)]
        if let bottom { path.move(to: .init(x: 0, y: bottom)); path.addLine(to: all[0]) } else { path.move(to: all[0]) }
        for i in 0..<(all.count - 1) {
            let p0 = all[max(0, i - 1)], p1 = all[i], p2 = all[i + 1], p3 = all[min(all.count - 1, i + 2)]
            let c1 = CGPoint(x: p1.x + (p2.x - p0.x) / 6, y: p1.y + (p2.y - p0.y) / 6)
            let c2 = CGPoint(x: p2.x - (p3.x - p1.x) / 6, y: p2.y - (p3.y - p1.y) / 6)
            path.addCurve(to: p2, control1: c1, control2: c2)
        }
        if let bottom { path.addLine(to: .init(x: w, y: bottom)); path.closeSubpath() }
        return path
    }
}
