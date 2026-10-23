import SwiftUI

// MARK: - Unified 梦境环 icon language
// One brand mark (ring + sparkle) + hand-drawn stroke replacements for SF Symbols.
// Same line weight / crayon second-pass / lime only when selected. No screen invents its own style.

enum YMGlyph: String, CaseIterable {
    // tabs / sidebar
    case home, library, collection, search, me
    // player
    case play, pause, previous, next, shuffle, `repeat`, repeatOne, order, lyrics, queue, more
    // actions
    case heart, heartFill, star, download, share, settings, back, close, recognize, chevron
}

enum YMIconPaths {
    /// Unit square 0…1 paths; scaled by YMIcon.
    static func path(_ g: YMGlyph) -> Path {
        switch g {
        case .home: return house()
        case .library: return notes()
        case .collection: return stacks()
        case .search: return magnifier()
        case .me: return person()
        case .play: return play()
        case .pause: return pause()
        case .previous: return skip(back: true)
        case .next: return skip(back: false)
        case .shuffle: return shuffle()
        case .repeat: return loop(one: false)
        case .repeatOne: return loop(one: true)
        case .order: return arrow()
        case .lyrics: return quote()
        case .queue: return list()
        case .more: return dots()
        case .heart, .heartFill: return heart(fill: g == .heartFill)
        case .star: return star()
        case .download: return download()
        case .share: return share()
        case .settings: return gear()
        case .back: return chevron(left: true)
        case .close: return xmark()
        case .recognize: return wave()
        case .chevron: return chevron(left: false)
        }
    }

    private static func house() -> Path {
        var p = Path()
        p.move(to: CGPoint(x: 0.50, y: 0.14))
        p.addLine(to: CGPoint(x: 0.88, y: 0.46))
        p.addLine(to: CGPoint(x: 0.78, y: 0.46))
        p.addLine(to: CGPoint(x: 0.78, y: 0.86))
        p.addLine(to: CGPoint(x: 0.55, y: 0.86))
        p.addLine(to: CGPoint(x: 0.55, y: 0.62))
        p.addLine(to: CGPoint(x: 0.45, y: 0.62))
        p.addLine(to: CGPoint(x: 0.45, y: 0.86))
        p.addLine(to: CGPoint(x: 0.22, y: 0.86))
        p.addLine(to: CGPoint(x: 0.22, y: 0.46))
        p.addLine(to: CGPoint(x: 0.12, y: 0.46))
        p.closeSubpath()
        return p
    }
    private static func notes() -> Path {
        var p = Path()
        p.addEllipse(in: CGRect(x: 0.18, y: 0.62, width: 0.22, height: 0.18))
        p.addEllipse(in: CGRect(x: 0.55, y: 0.55, width: 0.22, height: 0.18))
        p.move(to: CGPoint(x: 0.38, y: 0.70)); p.addLine(to: CGPoint(x: 0.38, y: 0.22))
        p.addLine(to: CGPoint(x: 0.75, y: 0.16)); p.addLine(to: CGPoint(x: 0.75, y: 0.63))
        return p
    }
    private static func stacks() -> Path {
        var p = Path()
        // three offset rounded cards
        for i in 0..<3 {
            let o = CGFloat(i) * 0.08
            let r = CGRect(x: 0.18 + o, y: 0.28 - o * 0.5, width: 0.58, height: 0.42)
            p.addRoundedRect(in: r, cornerSize: CGSize(width: 0.08, height: 0.08))
        }
        return p
    }
    private static func magnifier() -> Path {
        var p = Path()
        p.addEllipse(in: CGRect(x: 0.18, y: 0.14, width: 0.52, height: 0.52))
        p.move(to: CGPoint(x: 0.62, y: 0.58)); p.addLine(to: CGPoint(x: 0.86, y: 0.84))
        return p
    }
    private static func person() -> Path {
        var p = Path()
        p.addEllipse(in: CGRect(x: 0.32, y: 0.12, width: 0.36, height: 0.36))
        p.move(to: CGPoint(x: 0.16, y: 0.88))
        p.addQuadCurve(to: CGPoint(x: 0.84, y: 0.88), control: CGPoint(x: 0.50, y: 0.48))
        return p
    }
    private static func play() -> Path {
        var p = Path()
        p.move(to: CGPoint(x: 0.32, y: 0.18))
        p.addLine(to: CGPoint(x: 0.82, y: 0.50))
        p.addLine(to: CGPoint(x: 0.32, y: 0.82))
        p.closeSubpath()
        return p
    }
    private static func pause() -> Path {
        var p = Path()
        p.addRoundedRect(in: CGRect(x: 0.28, y: 0.18, width: 0.16, height: 0.64), cornerSize: CGSize(width: 0.04, height: 0.04))
        p.addRoundedRect(in: CGRect(x: 0.56, y: 0.18, width: 0.16, height: 0.64), cornerSize: CGSize(width: 0.04, height: 0.04))
        return p
    }
    private static func skip(back: Bool) -> Path {
        var p = Path()
        let flip: CGFloat = back ? -1 : 1
        func x(_ v: CGFloat) -> CGFloat { 0.5 + (v - 0.5) * flip }
        p.move(to: CGPoint(x: x(0.22), y: 0.22)); p.addLine(to: CGPoint(x: x(0.22), y: 0.78))
        p.move(to: CGPoint(x: x(0.30), y: 0.50)); p.addLine(to: CGPoint(x: x(0.78), y: 0.18))
        p.addLine(to: CGPoint(x: x(0.78), y: 0.82)); p.closeSubpath()
        return p
    }
    private static func shuffle() -> Path {
        var p = Path()
        p.move(to: CGPoint(x: 0.12, y: 0.30)); p.addLine(to: CGPoint(x: 0.32, y: 0.30))
        p.addQuadCurve(to: CGPoint(x: 0.68, y: 0.70), control: CGPoint(x: 0.50, y: 0.30))
        p.addLine(to: CGPoint(x: 0.88, y: 0.70))
        p.move(to: CGPoint(x: 0.78, y: 0.58)); p.addLine(to: CGPoint(x: 0.90, y: 0.70)); p.addLine(to: CGPoint(x: 0.78, y: 0.82))
        p.move(to: CGPoint(x: 0.12, y: 0.70)); p.addLine(to: CGPoint(x: 0.32, y: 0.70))
        p.addQuadCurve(to: CGPoint(x: 0.68, y: 0.30), control: CGPoint(x: 0.50, y: 0.70))
        p.addLine(to: CGPoint(x: 0.88, y: 0.30))
        p.move(to: CGPoint(x: 0.78, y: 0.18)); p.addLine(to: CGPoint(x: 0.90, y: 0.30)); p.addLine(to: CGPoint(x: 0.78, y: 0.42))
        return p
    }
    private static func loop(one: Bool) -> Path {
        var p = Path()
        p.addArc(center: CGPoint(x: 0.50, y: 0.50), radius: 0.32, startAngle: .degrees(-40), endAngle: .degrees(220), clockwise: false)
        p.move(to: CGPoint(x: 0.72, y: 0.18)); p.addLine(to: CGPoint(x: 0.86, y: 0.32)); p.addLine(to: CGPoint(x: 0.68, y: 0.36))
        if one {
            p.move(to: CGPoint(x: 0.46, y: 0.38)); p.addLine(to: CGPoint(x: 0.52, y: 0.38)); p.addLine(to: CGPoint(x: 0.52, y: 0.68))
        }
        return p
    }
    private static func arrow() -> Path {
        var p = Path()
        p.move(to: CGPoint(x: 0.18, y: 0.50)); p.addLine(to: CGPoint(x: 0.78, y: 0.50))
        p.move(to: CGPoint(x: 0.62, y: 0.32)); p.addLine(to: CGPoint(x: 0.82, y: 0.50)); p.addLine(to: CGPoint(x: 0.62, y: 0.68))
        return p
    }
    private static func quote() -> Path {
        var p = Path()
        p.addEllipse(in: CGRect(x: 0.18, y: 0.22, width: 0.22, height: 0.22))
        p.move(to: CGPoint(x: 0.28, y: 0.44)); p.addLine(to: CGPoint(x: 0.28, y: 0.72)); p.addLine(to: CGPoint(x: 0.42, y: 0.72))
        p.addEllipse(in: CGRect(x: 0.55, y: 0.22, width: 0.22, height: 0.22))
        p.move(to: CGPoint(x: 0.65, y: 0.44)); p.addLine(to: CGPoint(x: 0.65, y: 0.72)); p.addLine(to: CGPoint(x: 0.79, y: 0.72))
        return p
    }
    private static func list() -> Path {
        var p = Path()
        for y in [0.28, 0.50, 0.72] as [CGFloat] {
            p.addEllipse(in: CGRect(x: 0.16, y: y - 0.05, width: 0.10, height: 0.10))
            p.move(to: CGPoint(x: 0.36, y: y)); p.addLine(to: CGPoint(x: 0.84, y: y))
        }
        return p
    }
    private static func dots() -> Path {
        var p = Path()
        for x in [0.22, 0.45, 0.68] as [CGFloat] { p.addEllipse(in: CGRect(x: x, y: 0.42, width: 0.14, height: 0.14)) }
        return p
    }
    private static func heart(fill: Bool) -> Path {
        var p = Path()
        p.move(to: CGPoint(x: 0.50, y: 0.82))
        p.addCurve(to: CGPoint(x: 0.14, y: 0.38), control1: CGPoint(x: 0.20, y: 0.68), control2: CGPoint(x: 0.08, y: 0.52))
        p.addCurve(to: CGPoint(x: 0.50, y: 0.28), control1: CGPoint(x: 0.18, y: 0.18), control2: CGPoint(x: 0.38, y: 0.16))
        p.addCurve(to: CGPoint(x: 0.86, y: 0.38), control1: CGPoint(x: 0.62, y: 0.16), control2: CGPoint(x: 0.82, y: 0.18))
        p.addCurve(to: CGPoint(x: 0.50, y: 0.82), control1: CGPoint(x: 0.92, y: 0.52), control2: CGPoint(x: 0.80, y: 0.68))
        if !fill { /* stroke only — caller decides */ }
        return p
    }
    private static func star() -> Path {
        DoodlePaths.sparkle(center: CGPoint(x: 0.5, y: 0.5), r: 0.36)
    }
    private static func download() -> Path {
        var p = Path()
        p.move(to: CGPoint(x: 0.50, y: 0.14)); p.addLine(to: CGPoint(x: 0.50, y: 0.62))
        p.move(to: CGPoint(x: 0.32, y: 0.46)); p.addLine(to: CGPoint(x: 0.50, y: 0.66)); p.addLine(to: CGPoint(x: 0.68, y: 0.46))
        p.move(to: CGPoint(x: 0.22, y: 0.78)); p.addLine(to: CGPoint(x: 0.78, y: 0.78))
        return p
    }
    private static func share() -> Path {
        var p = Path()
        p.move(to: CGPoint(x: 0.50, y: 0.70)); p.addLine(to: CGPoint(x: 0.50, y: 0.18))
        p.move(to: CGPoint(x: 0.32, y: 0.34)); p.addLine(to: CGPoint(x: 0.50, y: 0.14)); p.addLine(to: CGPoint(x: 0.68, y: 0.34))
        p.move(to: CGPoint(x: 0.22, y: 0.48)); p.addLine(to: CGPoint(x: 0.22, y: 0.84)); p.addLine(to: CGPoint(x: 0.78, y: 0.84)); p.addLine(to: CGPoint(x: 0.78, y: 0.48))
        return p
    }
    private static func gear() -> Path {
        var p = Path()
        p.addEllipse(in: CGRect(x: 0.36, y: 0.36, width: 0.28, height: 0.28))
        for i in 0..<8 {
            let a = Double(i) / 8 * 2 * .pi
            let i0 = CGPoint(x: 0.5 + cos(a) * 0.22, y: 0.5 + sin(a) * 0.22)
            let o0 = CGPoint(x: 0.5 + cos(a) * 0.40, y: 0.5 + sin(a) * 0.40)
            p.move(to: i0); p.addLine(to: o0)
        }
        return p
    }
    private static func chevron(left: Bool) -> Path {
        var p = Path()
        let f: CGFloat = left ? 1 : -1
        p.move(to: CGPoint(x: 0.5 + 0.18 * f, y: 0.22))
        p.addLine(to: CGPoint(x: 0.5 - 0.18 * f, y: 0.50))
        p.addLine(to: CGPoint(x: 0.5 + 0.18 * f, y: 0.78))
        return p
    }
    private static func xmark() -> Path {
        var p = Path()
        p.move(to: CGPoint(x: 0.26, y: 0.26)); p.addLine(to: CGPoint(x: 0.74, y: 0.74))
        p.move(to: CGPoint(x: 0.74, y: 0.26)); p.addLine(to: CGPoint(x: 0.26, y: 0.74))
        return p
    }
    private static func wave() -> Path {
        var p = Path()
        let xs: [CGFloat] = [0.18, 0.32, 0.46, 0.60, 0.74]
        let hs: [CGFloat] = [0.22, 0.42, 0.62, 0.38, 0.26]
        for (x, h) in zip(xs, hs) {
            p.move(to: CGPoint(x: x, y: 0.5 - h / 2)); p.addLine(to: CGPoint(x: x, y: 0.5 + h / 2))
        }
        return p
    }
}

/// Hand-drawn glyph at a point size. Lime when `active`.
struct YMIcon: View {
    let glyph: YMGlyph
    var size: CGFloat = 22
    var weight: CGFloat = 1.7
    var active = false
    var filled = false
    var color: Color? = nil
    @Environment(\.colorScheme) private var scheme
    @Environment(\.ymIconInk) private var envInk
    var body: some View {
        let ink = color ?? envInk ?? (active ? Theme.accent(scheme) : Theme.fg(scheme))
        // Paths are authored in a 0…1 unit square — scale to `size` so they are not 1 pt dots.
        let path = YMIconPaths.path(glyph).applying(CGAffineTransform(scaleX: size, y: size))
        let lw = max(1.2, weight * size / 22)
        ZStack {
            path
                .stroke(ink.opacity(0.35), style: StrokeStyle(lineWidth: lw * 0.85, lineCap: .round, lineJoin: .round))
                .offset(x: 0.6, y: 0.7)
            if filled || glyph == .play || glyph == .pause || glyph == .heartFill {
                path.fill(ink, style: FillStyle(eoFill: false))
            }
            path.stroke(ink, style: StrokeStyle(lineWidth: lw, lineCap: .round, lineJoin: .round))
        }
        .frame(width: size, height: size)
    }
}

private struct YMIconInkKey: EnvironmentKey { static let defaultValue: Color? = nil }
extension EnvironmentValues {
    var ymIconInk: Color? {
        get { self[YMIconInkKey.self] }
        set { self[YMIconInkKey.self] = newValue }
    }
}
extension View {
    func ymIconInk(_ c: Color) -> some View { environment(\.ymIconInk, c) }
}

/// Brand mark: 梦境环 — wobble ring + sparkle. Used in AppIcon, sidebar, splash, About, empty states.
struct YMBrandMark: View {
    var size: CGFloat = 36
    var ringWidth: CGFloat = 1.8
    var sparkle = true
    var accentSparkle = true
    @Environment(\.colorScheme) private var scheme
    var body: some View {
        let ink = Theme.fg(scheme)
        let lime = Theme.accent(scheme)
        ZStack {
            Crayon(shape: WobblyCircle(seed: 3), color: ink, width: ringWidth)
                .frame(width: size * 0.92, height: size * 0.92)
            if sparkle {
                Path { p in
                    p.addPath(DoodlePaths.sparkle(center: CGPoint(x: size * 0.78, y: size * 0.22), r: size * 0.16))
                }
                .stroke(accentSparkle ? lime : ink, style: StrokeStyle(lineWidth: max(1.2, ringWidth * 0.85), lineCap: .round, lineJoin: .round))
            }
        }
        .frame(width: size, height: size)
        .accessibilityLabel("译梦")
    }
}

extension YMGlyph {
    /// Best-effort map from legacy SF Symbol names so call sites can migrate gradually.
    static func from(systemName s: String) -> YMGlyph? {
        switch s {
        case "house", "house.fill": return .home
        case "music.note.list", "music.note": return .library
        case "square.stack.3d.up.fill", "square.stack.3d.up": return .collection
        case "magnifyingglass": return .search
        case "person", "person.crop.circle", "person.fill": return .me
        case "play", "play.fill", "play.circle.fill": return .play
        case "pause", "pause.fill": return .pause
        case "backward.fill", "backward.end", "backward": return .previous
        case "forward.fill", "forward.end", "forward": return .next
        case "shuffle": return .shuffle
        case "repeat": return .repeat
        case "repeat.1": return .repeatOne
        case "arrow.right": return .order
        case "quote.bubble", "text.quote": return .lyrics
        case "list.bullet": return .queue
        case "ellipsis": return .more
        case "heart": return .heart
        case "heart.fill": return .heartFill
        case "star", "star.fill": return .star
        case "square.and.arrow.down", "arrow.down.circle": return .download
        case "square.and.arrow.up": return .share
        case "gear", "gearshape", "gearshape.fill": return .settings
        case "chevron.left": return .back
        case "chevron.right": return .chevron
        case "xmark": return .close
        case "waveform": return .recognize
        case "pencil": return .more
        case "server.rack", "externaldrive", "internaldrive": return .library
        case "paintbrush", "paintpalette", "circle.lefthalf.filled": return .settings
        case "bell", "bell.fill", "bell.badge": return .more
        case "info.circle", "info.circle.fill": return .more
        case "arrow.clockwise", "arrow.triangle.2.circlepath": return .order
        case "trash", "trash.fill": return .close
        case "person.crop.circle.badge.plus": return .me
        case "lock", "lock.fill", "key": return .settings
        case "wifi", "network": return .recognize
        case "opticaldisc", "opticaldisc.fill": return .collection
        case "music.quarternote.3", "music.mic": return .library
        case "text.justify", "list.dash": return .queue
        case "square.and.arrow.up.fill": return .share
        case "arrow.down.circle.fill", "arrow.down.to.line": return .download
        case "star.circle", "star.circle.fill": return .star
        case "heart.circle", "heart.circle.fill": return .heart
        case "plus", "plus.circle": return .more
        case "minus", "minus.circle": return .close
        case "checkmark", "checkmark.circle": return .star
        case "speaker.wave.2", "speaker.wave.2.fill", "speaker.fill": return .recognize
        case "airplayaudio", "airpodspro", "iphone": return .more
        case "moon.zzz", "moon.zzz.fill", "moon.fill": return .collection
        case "sparkles": return .star
        case "quote.opening": return .lyrics
        default: return nil
        }
    }
}

extension Tab {
    var glyph: YMGlyph {
        switch self {
        case .home: .home
        case .library: .library
        case .collection: .collection
        case .search: .search
        case .me: .me
        }
    }
}
