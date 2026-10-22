import AVFoundation
import AudioToolbox
import SwiftUI

// MARK: - Settings

/// 音效 (ViPER-style) settings. OFF → AVPlayer passthrough (bit-perfect, Dolby/Atmos untouched).
/// ON → AVAudioEngine path: player → 12-band AVAudioUnitEQ (10 graphic + bass shelf + vocal presence) → peak limiter.
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
            EffectsEngine.shared.apply(s)
            if s.on != oldValue.on { PlayerStore.shared.effectsToggled() }
        }
    }
    func pick(_ p: FXSettings.Preset) { var n = s; n.preset = p.name; n.bands = p.bands; n.bass = p.bass; n.vocal = p.vocal; s = n }
}

// MARK: - Engine

/// AVAudioEngine playback for the effects path. The stream is fetched to a local file first (AVAudioFile needs
/// random access), decoded by AudioToolbox, and scheduled from any frame — so switching in/out keeps position.
/// The graph runs in the file's own processing format, so 5.1/7.1 layouts stay intact through EQ + limiter
/// (both process every channel); only the final mixer → output step adapts to the hardware route.
@MainActor
final class EffectsEngine {
    static let shared = EffectsEngine()
    private let engine = AVAudioEngine()
    private let node = AVAudioPlayerNode()
    private let eq = AVAudioUnitEQ(numberOfBands: 12)
    private let limiter = AVAudioUnitEffect(audioComponentDescription: AudioComponentDescription(
        componentType: kAudioUnitType_Effect, componentSubType: kAudioUnitSubType_PeakLimiter,
        componentManufacturer: kAudioUnitManufacturer_Apple, componentFlags: 0, componentFlagsMask: 0))
    private var file: AVAudioFile?
    private var startFrame: AVAudioFramePosition = 0
    private var gen = 0
    private(set) var active = false
    private(set) var channels: AVAudioChannelCount = 0
    var onEnd: (() -> Void)?

    private let session: URLSession = {
        let c = URLSessionConfiguration.default
        c.httpAdditionalHeaders = ["User-Agent": "Yimeng/0.3 (iOS; art.yimeng.app)", "Accept": "*/*"]
        c.timeoutIntervalForRequest = 60
        return URLSession(configuration: c)
    }()
    private var dir: URL {
        let d = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("fx", isDirectory: true).appendingPathComponent(MusicSources.activeId, isDirectory: true)
        try? FileManager.default.createDirectory(at: d, withIntermediateDirectories: true)
        return d
    }

    private init() {
        engine.attach(node); engine.attach(eq); engine.attach(limiter)
        apply(FXSettings.load())
    }

    /// Local copy of the track (cached; keeps the newest 12).
    func fetch(_ url: URL, id: String, suffix: String?) async throws -> URL {
        let f = dir.appendingPathComponent("\(id).\(suffix ?? "bin")")
        if FileManager.default.fileExists(atPath: f.path) { return f }
        let (tmp, resp) = try await session.download(from: url)
        guard (resp as? HTTPURLResponse)?.statusCode == 200 else { throw URLError(.badServerResponse) }
        try? FileManager.default.removeItem(at: f)
        try FileManager.default.moveItem(at: tmp, to: f)
        prune()
        return f
    }
    private func prune() {
        let fm = FileManager.default
        guard let items = try? fm.contentsOfDirectory(at: dir, includingPropertiesForKeys: [.contentModificationDateKey]) else { return }
        let sorted = items.sorted {
            ((try? $0.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate) ?? .distantPast) >
            ((try? $1.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate) ?? .distantPast)
        }
        for u in sorted.dropFirst(12) { try? fm.removeItem(at: u) }
    }

    /// Open + wire the graph in the file's format (keeps channel count/layout).
    func load(_ local: URL) throws {
        stop()
        let f = try AVAudioFile(forReading: local)
        let fmt = f.processingFormat
        engine.disconnectNodeOutput(node); engine.disconnectNodeOutput(eq); engine.disconnectNodeOutput(limiter)
        engine.connect(node, to: eq, format: fmt)
        engine.connect(eq, to: limiter, format: fmt)
        engine.connect(limiter, to: engine.mainMixerNode, format: fmt)
        file = f; channels = fmt.channelCount
        engine.prepare()
    }

    var duration: Double { file.map { Double($0.length) / $0.processingFormat.sampleRate } ?? 0 }

    func play(from t: Double) throws {
        guard let file else { return }
        gen += 1; let g = gen
        node.stop()
        let sr = file.processingFormat.sampleRate
        startFrame = max(0, min(file.length - 1, AVAudioFramePosition(t * sr)))
        let count = AVAudioFrameCount(file.length - startFrame)
        node.scheduleSegment(file, startingFrame: startFrame, frameCount: count, at: nil, completionCallbackType: .dataPlayedBack) { [weak self] _ in
            Task { @MainActor in if let self, g == self.gen { self.onEnd?() } }
        }
        if !engine.isRunning { try engine.start() }
        node.play()
        active = true
    }
    func pause() { node.pause() }
    func resume() { if !engine.isRunning { try? engine.start() }; node.play() }
    var volume: Float { get { node.volume } set { node.volume = newValue } }

    var currentTime: Double {
        guard let file, let nt = node.lastRenderTime, let pt = node.playerTime(forNodeTime: nt) else {
            return file.map { Double(startFrame) / $0.processingFormat.sampleRate } ?? 0
        }
        return Double(startFrame + pt.sampleTime) / file.processingFormat.sampleRate
    }

    func stop() {
        gen += 1
        node.stop()
        if engine.isRunning { engine.stop() }
        file = nil; active = false
    }

    /// Live parameter update (no glitch: AU parameters are ramped by the unit).
    func apply(_ s: FXSettings) {
        for (i, f) in FXSettings.freqs.enumerated() {
            let b = eq.bands[i]
            b.filterType = .parametric; b.frequency = f; b.bandwidth = 1.0
            b.gain = Float(s.bands[i]); b.bypass = false
        }
        let bass = eq.bands[10]
        bass.filterType = .lowShelf; bass.frequency = 80; bass.gain = Float(s.bass); bass.bypass = s.bass == 0
        let voc = eq.bands[11]
        voc.filterType = .parametric; voc.frequency = 2800; voc.bandwidth = 1.4; voc.gain = Float(s.vocal); voc.bypass = s.vocal == 0
        eq.globalGain = Float(max(-24, min(24, s.effectivePreamp)))
        limiter.bypass = !s.limiter
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
                    .labelsHidden().tint(Theme.accent(scheme))
            }
            if fx.s.on {
                HStack(alignment: .top, spacing: 8) {
                    Image(systemName: "info.circle").font(.system(size: 12))
                    Text("音效开启时由 App 内解码处理：直通、杜比全景声空间音频与无缝预加载暂不可用。多声道（5.1 / 7.1）会保留声道逐声道处理，最终按当前输出设备混音。")
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
                    if dragging == nil { withAnimation(.spring(response: 0.25, dampingFraction: 0.7)) { dragging = i }; UISelectionFeedbackGenerator().selectionChanged() }
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
