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
