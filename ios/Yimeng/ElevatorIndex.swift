import SwiftUI
import UIKit

/// A–Z + # section keys. Chinese → pinyin via CFStringTransform (ToLatin, strip marks) → first letter.
enum Initials {
    static let letters: [String] = (65...90).map { String(UnicodeScalar($0)!) } + ["#"]
    /// Navidrome's default IgnoredArticles (it sorts "The Beatles" under B).
    static let articles = ["the ", "el ", "la ", "los ", "las ", "le ", "les ", "os ", "as ", "o ", "a "]

    static func stripArticle(_ s: String) -> String {
        let l = s.lowercased()
        for a in articles where l.hasPrefix(a) && l.count > a.count { return String(l.dropFirst(a.count)) }
        return l
    }

    /// Sort key + section letter for locally sorted lists (artists). Pinyin-aware.
    static func latin(_ s: String) -> String {
        let m = NSMutableString(string: stripArticle(s).trimmingCharacters(in: .whitespaces))
        CFStringTransform(m, nil, kCFStringTransformToLatin, false)
        CFStringTransform(m, nil, kCFStringTransformStripCombiningMarks, false)
        return (m as String).lowercased()
    }
    static func letter(ofLatin l: String) -> String {
        guard let c = l.unicodeScalars.first, c.value >= 97, c.value <= 122 else { return "#" }
        return String(Character(c)).uppercased()
    }

    /// Server-order bucket for Navidrome alphabeticalByName (raw unicode order, no transliteration):
    /// 0 = digits/symbols (sort first), 1…26 = a–z, 27 = everything else (CJK etc, sorts after z).
    static func serverBucket(_ name: String) -> Int {
        // Navidrome ignores leading quotes/brackets when sorting ("\"A\"" sits under A); keeps other symbols first.
        let t = stripArticle(name).drop { "\"'“”‘’「」『』([{《【 ".contains($0) }
        guard let c = t.unicodeScalars.first else { return 0 }
        if c.value >= 97 && c.value <= 122 { return Int(c.value) - 96 }
        return c.value < 97 ? 0 : 27
    }
    static func serverLetter(_ name: String) -> String {
        let b = serverBucket(name)
        return (1...26).contains(b) ? letters[b - 1] : "#"
    }
    static func bucket(ofLetter l: String) -> Int { l == "#" ? 27 : (letters.firstIndex(of: l) ?? 26) + 1 }
}

/// Precomputed, pinyin-sorted artist list with section starts. Built off the main thread.
struct ArtistIndex: Sendable {
    let artists: [NDArtist]
    let firstId: [String: String]          // letter → first artist id
    let letterOf: [String: String]         // artist id → letter
    var available: Set<String> { Set(firstId.keys) }

    static func build(_ src: [NDArtist]) -> ArtistIndex {
        let keyed: [(NDArtist, String, String)] = src.map { a in
            let l = Initials.latin(a.name)
            return (a, Initials.letter(ofLatin: l), l)
        }
        let order = Dictionary(uniqueKeysWithValues: Initials.letters.enumerated().map { ($1, $0) })
        let sorted = keyed.sorted { x, y in
            let ox = order[x.1]!, oy = order[y.1]!
            return ox != oy ? ox < oy : x.2 < y.2
        }
        var first: [String: String] = [:], of: [String: String] = [:]
        for (a, l, _) in sorted { if first[l] == nil { first[l] = a.id }; of[a.id] = l }
        return ArtistIndex(artists: sorted.map(\.0), firstId: first, letterOf: of)
    }
}

/// Right-side elevator: quiet small letters, lime only on the active one, floating bubble while dragging.
struct IndexBar: View {
    var letters: [String] = Initials.letters
    var available: Set<String>? = nil
    let active: String?
    let onPick: (String) -> Void
    @Environment(\.colorScheme) var scheme
    @State private var touch: String?
    @State private var busy = false
    private let rowH: CGFloat = 15
    private let haptic = UISelectionFeedbackGenerator()

    var body: some View {
        let shown = touch ?? active
        VStack(spacing: 0) {
            ForEach(letters, id: \.self) { l in
                let on = l == shown
                let has = available?.contains(l) ?? true
                Text(l)
                    .font(.system(size: 10, weight: on ? .bold : .medium, design: .rounded))
                    .foregroundStyle(on ? Theme.accent(scheme) : Theme.secondary(scheme).opacity(has ? 0.9 : 0.35))
                    .frame(width: 18, height: rowH)
            }
        }
        .padding(.vertical, 4)
        .contentShape(Rectangle())
        .gesture(
            DragGesture(minimumDistance: 0, coordinateSpace: .local)
                .onChanged { v in
                    let i = max(0, min(letters.count - 1, Int((v.location.y - 4) / rowH)))
                    let l = letters[i]
                    guard l != touch else { return }
                    if touch == nil { haptic.prepare() }
                    withAnimation(.spring(response: 0.28, dampingFraction: 0.78)) { touch = l }
                    haptic.selectionChanged()
                    onPick(l)
                }
                .onEnded { _ in
                    withAnimation(.spring(response: 0.35, dampingFraction: 0.85).delay(0.25)) { touch = nil }
                }
        )
        .overlay(alignment: .topTrailing) {
            if let t = touch, let i = letters.firstIndex(of: t) {
                Text(t)
                    .font(.system(size: 26, weight: .semibold, design: .rounded))
                    .foregroundStyle(Theme.fg(scheme))
                    .frame(width: 52, height: 52)
                    .background(Circle().fill(Theme.base(scheme)))
                    .overlay(Circle().stroke(Theme.divider(scheme), lineWidth: 0.5))
                    .overlay(alignment: .bottomTrailing) { Circle().fill(Theme.accent(scheme)).frame(width: 5, height: 5).offset(x: -6, y: -6) }
                    .shadow(color: .black.opacity(0.12), radius: 10, y: 4)
                    .offset(x: -44, y: 4 + CGFloat(i) * rowH + rowH / 2 - 26)
                    .transition(.scale(scale: 0.6).combined(with: .opacity))
                    .allowsHitTesting(false)
            }
        }
        .accessibilityLabel("索引")
    }
}
