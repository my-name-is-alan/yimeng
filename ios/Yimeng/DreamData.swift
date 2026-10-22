import SwiftUI

/// v10 "dream doodle" track model. Palette values mirror mobile-v10/gen.py (they would come from Palette.extract in production).
struct LyricLine: Hashable { let t: TimeInterval; let text: String; let en: String }

enum Motif: String { case cloud, zig, ink }
enum DreamLayout { case frame, orbit, drip }
enum Ambience: String, CaseIterable, Identifiable { case doodle = "涂鸦", stardust = "星尘", aurora = "极光", pure = "纯净"; var id: String { rawValue } }

struct DreamTrack: Identifiable, Hashable {
    let id: String
    let title: String
    let artist: String
    let cover: String
    let base: Color
    let blobs: [Color]          // exactly 4
    let bpm: Int
    let energy: Double          // 0…1
    let genre: String
    let duration: TimeInterval
    var lyrics: [LyricLine] = DreamMock.lyrics
    /// 译梦 Core: platform-provided tempo / tags (nil/empty → on-device estimate / nothing shown).
    var serverBPM: Double? = nil
    var tags: [String] = []

    // rules() from part2.py
    var motif: Motif { energy > 0.7 ? .zig : (["folk", "acoustic", "jazz"].contains(genre) ? .ink : .cloud) }
    var layout: DreamLayout { switch motif { case .zig: .orbit; case .ink: .drip; case .cloud: .frame } }
    var density: Int { Int(4 + energy * 8) }
    var stroke: CGFloat { 1.6 + energy * 1.4 }
    var jitter: CGFloat { 1.5 + energy * 2.5 }
    var seed: UInt64 { UInt64(id.unicodeScalars.reduce(0) { $0 + Int($1.value) }) }
}

extension Color {
    init(hex: UInt32) { self.init(red: Double((hex >> 16) & 0xff) / 255, green: Double((hex >> 8) & 0xff) / 255, blue: Double(hex & 0xff) / 255) }
}

func fmtTime(_ t: TimeInterval) -> String { let s = max(0, Int(t)); return "\(s / 60):" + String(format: "%02d", s % 60) }

#if DEBUG
enum DreamMock {
    static let lyrics: [LyricLine] = [
        .init(t: 0, text: "夜色漫过旧城的街", en: "Night spills over the old town"),
        .init(t: 5, text: "你说别回头", en: "You said don't look back"),
        .init(t: 10, text: "弄丢了爱 失去了你", en: "Lost my love, lost you too"),
        .init(t: 15, text: "橘子海 在心里起伏", en: "A tangerine sea rising in me"),
        .init(t: 20, text: "风把晚霞吹散", en: "The wind scatters the dusk"),
        .init(t: 25, text: "我们终会再见", en: "We will meet again"),
        .init(t: 30, text: "在梦里", en: "In a dream"),
        .init(t: 35, text: "在每一个清晨", en: "In every morning"),
    ]
    static let cloud = DreamTrack(id: "ym-0412", title: "云朵来信", artist: "王菲", cover: "cvH", base: Color(hex: 0x7a2240),
                                  blobs: [Color(hex: 0xff6a88), Color(hex: 0xffb199), Color(hex: 0xff9a5a), Color(hex: 0xffd3c4)], bpm: 68, energy: 0.3, genre: "ballad", duration: 256)
    static let zig = DreamTrack(id: "ym-1187", title: "紫色黄昏", artist: "不才", cover: "cvD", base: Color(hex: 0x2a1060),
                                blobs: [Color(hex: 0xff5fa8), Color(hex: 0xffd36b), Color(hex: 0x6b2cff), Color(hex: 0xff7ac0)], bpm: 128, energy: 0.85, genre: "electronic", duration: 292)
    static let ink = DreamTrack(id: "ym-0733", title: "绿野仙踪", artist: "陈粒", cover: "cvC", base: Color(hex: 0x06371c),
                                blobs: [Color(hex: 0x33d17a), Color(hex: 0xc8f542), Color(hex: 0x0aa58a), Color(hex: 0xe8ff7a)], bpm: 92, energy: 0.5, genre: "folk", duration: 247)
    static let rose = DreamTrack(id: "ym-0001", title: "玫瑰色的你", artist: "不才", cover: "cvA", base: Color(hex: 0x5a0f3a),
                                 blobs: [Color(hex: 0xff2d7a), Color(hex: 0xff8a3d), Color(hex: 0x7a1cff), Color(hex: 0xff5fa8)], bpm: 84, energy: 0.4, genre: "pop", duration: 238)
    static let orange = DreamTrack(id: "ym-0099", title: "橘子海", artist: "陈粒", cover: "cvB", base: Color(hex: 0x7a2a00),
                                   blobs: [Color(hex: 0xff7a1a), Color(hex: 0xffd23f), Color(hex: 0xff3d2e), Color(hex: 0xffb000)], bpm: 96, energy: 0.55, genre: "acoustic", duration: 251)
    static let all = [cloud, zig, ink, rose, orange]
    /// 典藏 stack
    static let collection = [zig, rose, orange, cloud, ink]
}
#else
/// Release: a single neutral idle track (no titles, no lyrics) — never shown, only a non-optional default.
enum DreamMock {
    static let lyrics: [LyricLine] = []
    static let idle = DreamTrack(id: "idle", title: "", artist: "", cover: "cvA", base: Color(hex: 0x1a1028),
                                 blobs: [Color(hex: 0x7a1cff), Color(hex: 0xff5fa8), Color(hex: 0x33d17a), Color(hex: 0xffd36b)], bpm: 100, energy: 0.45, genre: "", duration: 1)
    static let cloud = idle, zig = idle, ink = idle, rose = idle, orange = idle
    static let all = [idle]
    static let collection: [DreamTrack] = []
}
#endif

/// Deterministic per-track RNG (SplitMix64).
struct SeededRNG: RandomNumberGenerator {
    var state: UInt64
    init(_ seed: UInt64) { state = seed &+ 0x9E3779B97F4A7C15 }
    mutating func next() -> UInt64 {
        state &+= 0x9E3779B97F4A7C15
        var z = state
        z = (z ^ (z >> 30)) &* 0xBF58476D1CE4E5B9
        z = (z ^ (z >> 27)) &* 0x94D049BB133111EB
        return z ^ (z >> 31)
    }
    mutating func d(_ a: Double, _ b: Double) -> Double { Double.random(in: a...b, using: &self) }
}

// MARK: - Real-playback doodle style (v39): metadata tags first, then the on-device beat/energy analysis. Stable per track.

enum DreamStyle {
    static let inkWords = ["folk", "acoustic", "jazz", "classical", "piano", "ambient", "lofi", "lo-fi", "new age", "chill",
                           "民谣", "古风", "爵士", "古典", "钢琴", "轻音乐", "纯音乐", "治愈", "安静", "国风", "器乐"]
    static let zigWords = ["rock", "metal", "punk", "edm", "electronic", "electro", "dance", "house", "techno", "dubstep", "drum", "hip hop", "hip-hop", "rap", "trap",
                           "摇滚", "金属", "朋克", "电子", "舞曲", "说唱", "嘻哈", "燃", "热血"]
    /// Genre class from tags / genre text: "folk" (→ ink), "rock" (→ zig) or nil (no opinion).
    static func genreClass(_ texts: [String]) -> String? {
        let t = texts.map { $0.lowercased() }
        if t.contains(where: { s in zigWords.contains { s.contains($0) } }) { return "rock" }
        if t.contains(where: { s in inkWords.contains { s.contains($0) } }) { return "folk" }
        return nil
    }
    /// Stable per-track jitter in −0.06…0.06 (same id → same value, across launches).
    static func jitter(_ id: String) -> Double {
        let h = id.utf8.reduce(UInt64(1469598103934665603)) { ($0 ^ UInt64($1)) &* 1099511628211 }
        return Double(h % 1000) / 1000 * 0.12 - 0.06
    }
    /// Energy 0…1 from tempo + on-device loudness/onset energy (either may be missing).
    static func energy(bpm: Double?, measured: Double?, id: String) -> Double {
        var parts: [Double] = []
        if let b = bpm, b > 0 { parts.append(max(0, min(1, (b - 70) / 90))) }
        if let m = measured { parts.append(max(0, min(1, m))) }
        let base = parts.isEmpty ? 0.45 : parts.reduce(0, +) / Double(parts.count)
        return max(0, min(1, base + jitter(id)))
    }
    /// Fold a tempo into 70…180 by octaves (half/double-time detections).
    static func octave(_ bpm: Double) -> Double {
        guard bpm > 0 else { return 0 }
        var b = bpm
        while b < 70 { b *= 2 }
        while b > 180 { b /= 2 }
        return b
    }
    static func median(_ xs: [Double]) -> Double? {
        guard !xs.isEmpty else { return nil }
        let s = xs.sorted(); let n = s.count
        return n % 2 == 1 ? s[n / 2] : (s[n / 2 - 1] + s[n / 2]) / 2
    }
    /// Stable tempo from the first ~20 s of beat readings: octave-correct each, then take the median.
    static func stableTempo(_ samples: [Double]) -> Double? { median(samples.filter { $0 > 0 }.map(octave)) }

    /// Persistent per-track lock: once decided, the style never changes between launches (tags may still override).
    struct Locked: Codable, Equatable { var energy: Double; var genre: String; var fromTags: Bool }
    nonisolated(unsafe) static var store: UserDefaults = .standard
    static let key = "ym.dreamstyle.v1"
    static func cached(_ id: String) -> Locked? {
        guard let d = store.data(forKey: key), let m = try? JSONDecoder().decode([String: Locked].self, from: d) else { return nil }
        return m[id]
    }
    static func lock(_ id: String, _ l: Locked) {
        var m = (store.data(forKey: key)).flatMap { try? JSONDecoder().decode([String: Locked].self, from: $0) } ?? [:]
        if m.count > 4000 { m.removeAll() }
        m[id] = l
        if let d = try? JSONEncoder().encode(m) { store.set(d, forKey: key) }
    }

    /// Decide (energy, genre) for DreamTrack so `motif` yields zig / ink / cloud for real songs.
    static func decide(id: String, tags: [String], bpm: Double?, measured: Double?) -> (energy: Double, genre: String) {
        let g = genreClass(tags)
        var e = energy(bpm: bpm, measured: measured, id: id)
        if g == "rock" { e = max(e, 0.75) }
        if g == "folk" { e = min(e, 0.6) }
        // No metadata opinion: very calm, slow material reads as ink, loud fast as zig (via energy).
        let genre = g ?? ((e < 0.28 && (bpm ?? 90) < 95) ? "folk" : "pop")
        return (e, genre)
    }
}
