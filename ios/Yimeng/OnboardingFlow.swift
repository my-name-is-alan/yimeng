import SwiftUI

// MARK: - First-run gate

enum FirstRunPhase: String { case launch, intro, onboarding, sources, main }

enum FirstRun {
    static let doneKey = "ym.onboardingDone"
    static var isDone: Bool {
        get { UserDefaults.standard.bool(forKey: doneKey) }
        set { UserDefaults.standard.set(newValue, forKey: doneKey) }
    }
    static func reset() { UserDefaults.standard.removeObject(forKey: doneKey); UserDefaults.standard.removeObject(forKey: "ym.sources.v1") }
}

// MARK: - Bloom colour field (launch / onboarding backdrops)

struct BloomField: View {
    let base: Color
    let blobs: [Color]
    var bloom: CGFloat = 1          // 0.2…1 scale of the field
    var opacity: Double = 1
    var body: some View {
        GeometryReader { g in
            let w = g.size.width, h = g.size.height
            ZStack {
                base
                Circle().fill(blobs[safe: 0] ?? .pink).frame(width: w * 1.05).position(x: w * 0.05, y: h * 0.0)
                Circle().fill(blobs[safe: 1] ?? .orange).frame(width: w * 0.95).position(x: w * 0.95, y: h * 0.28)
                Circle().fill(blobs[safe: 2] ?? .purple).frame(width: w * 1.0).position(x: w * 0.0, y: h * 0.62)
                Circle().fill(blobs[safe: 3] ?? .pink).frame(width: w * 0.9).position(x: w * 0.95, y: h * 0.88)
            }
            .blur(radius: 70)
            .scaleEffect(bloom)
            .opacity(opacity)
            LinearGradient(colors: [.black.opacity(0.05), .black.opacity(0.25)], startPoint: .top, endPoint: .bottom)
        }
        .ignoresSafeArea()
    }
}

private extension Array {
    subscript(safe i: Int) -> Element? { indices.contains(i) ? self[i] : nil }
}

enum OnboardPalette {
    static let pages: [(base: Color, blobs: [Color])] = [
        (Color(hex: 0x5a0f3a), [Color(hex: 0xff2d7a), Color(hex: 0xff8a3d), Color(hex: 0x7a1cff), Color(hex: 0xff5fa8)]),
        (Color(hex: 0x2a1060), [Color(hex: 0xff5fa8), Color(hex: 0xffd36b), Color(hex: 0x6b2cff), Color(hex: 0xff7ac0)]),
        (Color(hex: 0x7a2a00), [Color(hex: 0xff7a1a), Color(hex: 0xffd23f), Color(hex: 0xff3d2e), Color(hex: 0xffb000)]),
        (Color(hex: 0x06371c), [Color(hex: 0x33d17a), Color(hex: 0xc8f542), Color(hex: 0x0aa58a), Color(hex: 0xe8ff7a)]),
        (Color(hex: 0x0b1640), [Color(hex: 0x2d6bff), Color(hex: 0x9ad0ff), Color(hex: 0x6b4dff), Color(hex: 0x7af0ff)]),
    ]
    static let sources = pages[4]
    static let connect = pages[3]
}

// MARK: - Moonwave icon (draw-on)

struct MoonwaveIcon: View, Animatable {
    var progress: CGFloat = 1
    var size: CGFloat = 140
    /// Canvas isn't animatable by itself — without this, withAnimation(progress) jumped straight to the end frame.
    var animatableData: CGFloat { get { progress } set { progress = newValue } }
    /// Stroke-by-stroke: crescent, then the wave bars, then the sparkle.
    private func seg(_ a: CGFloat, _ b: CGFloat) -> CGFloat { min(1, max(0, (progress - a) / (b - a))) }
    var body: some View {
        Canvas { ctx, sz in
            let s = sz.width / 1024
            var moon = Path()
            // Approximate crescent via two arcs in design coords
            moon.addEllipse(in: CGRect(x: 255, y: 170, width: 430, height: 430))
            var cut = Path()
            cut.addEllipse(in: CGRect(x: 370, y: 210, width: 330, height: 330))
            let crescent = moon.subtracting(cut).applying(CGAffineTransform(scaleX: s, y: s))
            ctx.stroke(crescent.trimmedPath(from: 0, to: seg(0, 0.55)), with: .color(.white),
                       style: StrokeStyle(lineWidth: 6 * s, lineCap: .round, lineJoin: .round))

            // Wave bars
            let bars: [(CGFloat, CGFloat, CGFloat)] = [
                (332, 748, 792), (377, 726, 814), (422, 700, 840), (467, 718, 822),
                (512, 684, 856), (557, 710, 830), (602, 732, 808), (647, 704, 836), (692, 740, 800),
            ]
            var wave = Path()
            for (x, y0, y1) in bars {
                wave.move(to: CGPoint(x: x * s, y: y0 * s))
                wave.addLine(to: CGPoint(x: x * s, y: y1 * s))
            }
            ctx.stroke(wave.trimmedPath(from: 0, to: seg(0.4, 0.85)), with: .color(.white),
                       style: StrokeStyle(lineWidth: 5.5 * s, lineCap: .round))

            // Sparkle
            var spark = Path()
            let cx: CGFloat = 720 * s, cy: CGFloat = 254 * s, r: CGFloat = 34 * s
            spark.move(to: CGPoint(x: cx, y: cy - r))
            spark.addQuadCurve(to: CGPoint(x: cx + r, y: cy), control: CGPoint(x: cx, y: cy))
            spark.addQuadCurve(to: CGPoint(x: cx, y: cy + r), control: CGPoint(x: cx, y: cy))
            spark.addQuadCurve(to: CGPoint(x: cx - r, y: cy), control: CGPoint(x: cx, y: cy))
            spark.addQuadCurve(to: CGPoint(x: cx, y: cy - r), control: CGPoint(x: cx, y: cy))
            ctx.stroke(spark.trimmedPath(from: 0, to: seg(0.78, 1)), with: .color(.white),
                       style: StrokeStyle(lineWidth: 3.5 * s, lineCap: .round, lineJoin: .round))
        }
        .frame(width: size, height: size)
        .shadow(color: .white.opacity(0.35), radius: 8)
    }
}

/// Small doodle glyphs for source tiles / onboarding.
struct SourceGlyph: View {
    let kind: SourceKind
    var body: some View {
        Canvas { ctx, sz in
            let s = sz.width / 44
            var p = Path()
            switch kind {
            case .core:
                p.addEllipse(in: CGRect(x: 14 * s, y: 11 * s, width: 22 * s, height: 22 * s))
                var cut = Path(); cut.addEllipse(in: CGRect(x: 20 * s, y: 14 * s, width: 16 * s, height: 16 * s))
                p = p.subtracting(cut)
                var sp = Path()
                let c = CGPoint(x: 33 * s, y: 12 * s), r: CGFloat = 4 * s
                sp.move(to: CGPoint(x: c.x, y: c.y - r)); sp.addQuadCurve(to: CGPoint(x: c.x + r, y: c.y), control: c)
                sp.addQuadCurve(to: CGPoint(x: c.x, y: c.y + r), control: c); sp.addQuadCurve(to: CGPoint(x: c.x - r, y: c.y), control: c)
                sp.addQuadCurve(to: CGPoint(x: c.x, y: c.y - r), control: c)
                ctx.stroke(p, with: .color(.white), style: StrokeStyle(lineWidth: 2 * s, lineCap: .round))
                ctx.stroke(sp, with: .color(.white), style: StrokeStyle(lineWidth: 1.6 * s, lineCap: .round))
            case .fnos:
                p.addRoundedRect(in: CGRect(x: 10 * s, y: 9 * s, width: 24 * s, height: 26 * s), cornerSize: CGSize(width: 4 * s, height: 4 * s))
                ctx.stroke(p, with: .color(.white), style: StrokeStyle(lineWidth: 2 * s, lineCap: .round))
                var lines = Path()
                lines.move(to: CGPoint(x: 14 * s, y: 17 * s)); lines.addLine(to: CGPoint(x: 30 * s, y: 17 * s))
                lines.move(to: CGPoint(x: 14 * s, y: 24 * s)); lines.addLine(to: CGPoint(x: 30 * s, y: 24 * s))
                ctx.stroke(lines, with: .color(.white), style: StrokeStyle(lineWidth: 2 * s, lineCap: .round))
                ctx.fill(Path(ellipseIn: CGRect(x: 26.5 * s, y: 28.5 * s, width: 3 * s, height: 3 * s)), with: .color(.white))
            case .navidrome:
                p.addEllipse(in: CGRect(x: 9 * s, y: 9 * s, width: 26 * s, height: 26 * s))
                var note = Path()
                note.move(to: CGPoint(x: 20 * s, y: 28 * s)); note.addLine(to: CGPoint(x: 20 * s, y: 15 * s))
                note.addLine(to: CGPoint(x: 28 * s, y: 18 * s))
                ctx.stroke(p, with: .color(.white), style: StrokeStyle(lineWidth: 2 * s))
                ctx.stroke(note, with: .color(.white), style: StrokeStyle(lineWidth: 2 * s, lineCap: .round, lineJoin: .round))
                ctx.stroke(Path(ellipseIn: CGRect(x: 15 * s, y: 25.5 * s, width: 5.2 * s, height: 5.2 * s)), with: .color(.white), style: StrokeStyle(lineWidth: 2 * s))
            case .emby:
                p.addRoundedRect(in: CGRect(x: 12 * s, y: 10 * s, width: 20 * s, height: 24 * s), cornerSize: .zero)
                var tri = Path()
                tri.move(to: CGPoint(x: 19 * s, y: 16 * s)); tri.addLine(to: CGPoint(x: 27 * s, y: 22 * s)); tri.addLine(to: CGPoint(x: 19 * s, y: 28 * s)); tri.closeSubpath()
                ctx.stroke(p, with: .color(.white), style: StrokeStyle(lineWidth: 2 * s, lineJoin: .round))
                ctx.stroke(tri, with: .color(.white), style: StrokeStyle(lineWidth: 2 * s, lineJoin: .round))
            case .local:
                p.addRoundedRect(in: CGRect(x: 14 * s, y: 8 * s, width: 16 * s, height: 28 * s), cornerSize: CGSize(width: 4 * s, height: 4 * s))
                ctx.stroke(p, with: .color(.white), style: StrokeStyle(lineWidth: 2 * s))
                var bar = Path(); bar.move(to: CGPoint(x: 20 * s, y: 31 * s)); bar.addLine(to: CGPoint(x: 24 * s, y: 31 * s))
                ctx.stroke(bar, with: .color(.white), style: StrokeStyle(lineWidth: 2 * s, lineCap: .round))
            }
        }
        .frame(width: 44, height: 44)
    }
}

// MARK: - Launch

struct LaunchView: View {
    /// Returning launches: quick ~0.9s version.
    var short = false
    var onFinished: () -> Void
    @State private var scale: CGFloat = 0.82
    @State private var bloom: CGFloat = 1.25
    @State private var bloomOp: Double = 0
    @State private var draw: CGFloat = 0
    @State private var word: Double = 0
    @State private var fadeAll: Double = 1
    private let pal = OnboardPalette.pages[0]

    var body: some View {
        ZStack {
            BloomField(base: pal.base, blobs: pal.blobs, bloom: bloom, opacity: bloomOp)
            VStack(spacing: 28) {
                Spacer()
                MoonwaveIcon(progress: draw, size: 140).scaleEffect(scale)
                VStack(spacing: 6) {
                    Text("译梦").font(.system(size: 34, weight: .heavy)).tracking(8)
                    Text("TRANSLATING DREAMS").font(.system(size: 12, weight: .medium)).tracking(4).opacity(0.7)
                }
                .opacity(word)
                .offset(y: (1 - word) * 10)
                Spacer()
            }
            .foregroundStyle(.white)
        }
        .opacity(fadeAll)
        .preferredColorScheme(.dark)
        .task { await run() }
    }

    private func run() async {
        if short {
            withAnimation(.easeOut(duration: 0.5)) { bloom = 1; bloomOp = 1; draw = 1; word = 1 }
            withAnimation(.spring(response: 0.5, dampingFraction: 0.7)) { scale = 1 }
            try? await Task.sleep(for: .milliseconds(650))
            withAnimation(.easeInOut(duration: 0.3)) { fadeAll = 0 }
            try? await Task.sleep(for: .milliseconds(300))
            onFinished(); return
        }
        withAnimation(.easeOut(duration: 1.2)) { bloom = 1; bloomOp = 1 }
        withAnimation(.spring(response: 0.9, dampingFraction: 0.62)) { scale = 1 }
        try? await Task.sleep(for: .milliseconds(150))
        withAnimation(.easeInOut(duration: 1.5)) { draw = 1 }
        try? await Task.sleep(for: .milliseconds(1300))
        withAnimation(.spring(response: 0.55, dampingFraction: 0.75)) { word = 1 }
        try? await Task.sleep(for: .milliseconds(900))
        withAnimation(.easeInOut(duration: 0.45)) { fadeAll = 0 }
        try? await Task.sleep(for: .milliseconds(450))
        onFinished()
    }
}

// MARK: - Onboarding carousel (5 pages)

struct OnboardingPage: Identifiable {
    let id: Int
    let title: String
    let subtitle: String
}

struct OnboardingView: View {
    var onSkip: () -> Void
    var onDone: () -> Void
    var initialPage: Int = 0
    @State private var page = 0
    @State private var draw: CGFloat = 0
    @State private var bar: CGFloat = 0
    @State private var barAdvance: Task<Void, Never>? = nil
    private let pageHold: TimeInterval = 4.6
    private let pages: [OnboardingPage] = [
        .init(id: 0, title: "一处听遍所有音乐", subtitle: "译梦服务端、飞牛、Navidrome、Emby 与本机音乐，合成一个安静的曲库。"),
        .init(id: 1, title: "涂鸦随歌而变", subtitle: "每首歌都有自己的梦：节奏、情绪与封面颜色，决定画在它身边的涂鸦。"),
        .init(id: 2, title: "歌词与翻译", subtitle: "大字歌词，逐行翻译；正在唱的那一句，会被轻轻划上记号。"),
        .init(id: 3, title: "典藏你的专辑", subtitle: "像翻唱片架一样浏览收藏，把喜欢的专辑放在最上面。"),
        .init(id: 4, title: "灵动岛与锁屏", subtitle: "不打开 App，也能在灵动岛和锁屏上看见正在做的梦。"),
    ]

    var body: some View {
        ZStack {
            ZStack {
                ForEach(0..<5, id: \.self) { i in
                    let p = OnboardPalette.pages[i]
                    BloomField(base: p.base, blobs: p.blobs)
                        .opacity(page == i ? 1 : 0)
                        .animation(.easeInOut(duration: 0.7), value: page)
                }
            }
            TabView(selection: $page) {
                ForEach(pages) { p in
                    pageContent(p).tag(p.id)
                }
            }
            .tabViewStyle(.page(indexDisplayMode: .never))
            .onChange(of: page) { _, _ in
                restartBar()
                redraw()
            }

            VStack {
                HStack {
                    Spacer()
                    Button("跳过") { onSkip() }
                        .font(.system(size: 16)).foregroundStyle(.white.opacity(0.75)).padding(20)
                }
                Spacer()

                // Progress segments (not dots): current page fills over pageHold
                HStack(spacing: 5) {
                    ForEach(0..<5, id: \.self) { i in
                        GeometryReader { g in
                            Capsule().fill(Color.white.opacity(0.28))
                                .overlay(alignment: .leading) {
                                    Capsule().fill(Color.white)
                                        .frame(width: g.size.width * fill(for: i))
                                }
                        }
                        .frame(height: 3)
                    }
                }
                .padding(.horizontal, 28).padding(.bottom, 16)

                Button {
                    if page < 4 {
                        withAnimation(.easeInOut(duration: 0.55)) { page += 1 }
                    } else { onDone() }
                } label: {
                    Text(page == 4 ? "开始使用" : "继续")
                }
                .buttonStyle(PrimaryButtonStyle())
                .padding(.horizontal, 20).padding(.bottom, 44)
            }
        }
        .preferredColorScheme(.dark)
        .onAppear {
            page = initialPage
            redraw()
            restartBar()
        }
    }

    private func fill(for i: Int) -> CGFloat {
        if i < page { return 1 }
        if i > page { return 0 }
        return bar
    }

    @ViewBuilder
    private func pageContent(_ p: OnboardingPage) -> some View {
        VStack(spacing: 0) {
            ZStack {
                OnboardArt(page: p.id, progress: draw, active: page == p.id)
            }
            .frame(height: 520)
            .padding(.top, 40)

            VStack(alignment: .leading, spacing: 8) {
                Text(p.title).font(.system(size: 28, weight: .bold))
                Text(p.subtitle).font(.system(size: 16)).opacity(0.75).lineSpacing(4)
            }
            .foregroundStyle(.white)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 28)
            Spacer(minLength: 140)
        }
    }

    private func redraw() {
        draw = 0
        withAnimation(.easeOut(duration: 1.15)) { draw = 1 }
    }

    /// Continuous linear fill via SwiftUI animation (display-linked), not Timer ticks.
    private func restartBar() {
        barAdvance?.cancel()
        var t = Transaction(); t.disablesAnimations = true
        withTransaction(t) { bar = 0 }
        // Next runloop so the zero-width frame commits before linear fill.
        DispatchQueue.main.async {
            withAnimation(.linear(duration: pageHold)) { bar = 1 }
        }
        let hold = pageHold
        let cur = page
        barAdvance = Task { @MainActor in
            try? await Task.sleep(for: .seconds(hold))
            guard !Task.isCancelled, page == cur else { return }
            if page < 4 {
                withAnimation(.easeInOut(duration: 0.55)) { page += 1 }
            }
        }
    }
}

/// Animated page illustrations — TimelineView only ticks while `active`.
struct OnboardArt: View {
    let page: Int
    var progress: CGFloat
    var active: Bool
    var body: some View {
        Group {
            switch page {
            case 0: SourcesAnim(progress: progress, active: active)
            case 1: DoodleAnim(progress: progress, active: active)
            case 2: LyricsAnim(progress: progress, active: active)
            case 3: StackAnim(progress: progress, active: active)
            default: IslandAnim(progress: progress, active: active)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

// MARK: Page 1 — sources float + pulse links

private struct SourcesAnim: View {
    var progress: CGFloat
    var active: Bool
    private let kinds: [SourceKind] = [.fnos, .navidrome, .emby, .local]
    var body: some View {
        TimelineView(.animation(minimumInterval: 1 / 30, paused: !active)) { tl in
            let t = active ? tl.date.timeIntervalSinceReferenceDate : 0
            GeometryReader { g in
                let w = g.size.width
                let c = CGPoint(x: w / 2, y: 220)
                let anchors: [CGPoint] = [
                    CGPoint(x: w * 0.18, y: 120), CGPoint(x: w * 0.82, y: 120),
                    CGPoint(x: w * 0.18, y: 340), CGPoint(x: w * 0.82, y: 340),
                ]
                ZStack {
                    // dashed spokes with marching dash + pulse opacity
                    Canvas { ctx, _ in
                        for (i, pt) in anchors.enumerated() {
                            var p = Path(); p.move(to: pt); p.addLine(to: c)
                            let pulse = 0.35 + 0.35 * (0.5 + 0.5 * sin(t * 2.2 + Double(i) * 0.9))
                            ctx.stroke(p.trimmedPath(from: 0, to: progress),
                                       with: .color(.white.opacity(pulse * Double(progress))),
                                       style: StrokeStyle(lineWidth: 1.6, lineCap: .round,
                                                          dash: [3, 7], dashPhase: CGFloat(-t * 28 + Double(i) * 6)))
                        }
                        // soft ring pulses from center
                        for k in 0..<3 {
                            let age = (t * 0.55 + Double(k) / 3).truncatingRemainder(dividingBy: 1)
                            let r = 18 + CGFloat(age) * 70
                            var ring = Path(ellipseIn: CGRect(x: c.x - r, y: c.y - r, width: r * 2, height: r * 2))
                            ctx.stroke(ring, with: .color(.white.opacity((1 - age) * 0.35 * Double(progress))),
                                       style: StrokeStyle(lineWidth: 1.5))
                        }
                    }
                    MoonwaveIcon(progress: progress, size: 90)
                        .scaleEffect(1 + 0.03 * sin(t * 1.4))
                        .position(c)
                        .opacity(Double(progress))

                    ForEach(Array(kinds.enumerated()), id: \.offset) { i, kind in
                        let base = anchors[i]
                        let bob = CGFloat(sin(t * 1.6 + Double(i) * 1.3)) * 7
                        let pop = 0.88 + 0.12 * progress
                        RoundedRectangle(cornerRadius: 14, style: .continuous)
                            .fill(kind.tile)
                            .frame(width: 48, height: 48)
                            .overlay { SourceGlyph(kind: kind) }
                            .overlay {
                                Circle().stroke(Theme.lime.opacity(0.55 + 0.45 * sin(t * 2.8 + Double(i))), lineWidth: 2)
                                    .frame(width: 54, height: 54)
                                    .opacity(kind == .navidrome || kind == .fnos ? 0.7 : 0.25)
                            }
                            .scaleEffect(pop)
                            .shadow(color: kind.tile.opacity(0.5), radius: 10, y: 4)
                            .position(x: base.x, y: base.y + bob * progress)
                            .opacity(Double(progress))
                    }
                }
            }
        }
    }
}

// MARK: Page 2 — doodle draw + beat wobble + cover breathe

private struct DoodleAnim: View {
    var progress: CGFloat
    var active: Bool
    var body: some View {
        TimelineView(.animation(minimumInterval: 1 / 30, paused: !active)) { tl in
            let t = active ? tl.date.timeIntervalSinceReferenceDate : 0
            let beat = 60.0 / 128.0
            let br = 0.5 - 0.5 * cos(t / beat * .pi * 2)
            let floatY = CGFloat(4 * sin(t * 1.2))
            // looped redraw every ~5.5s
            let cycle = t.truncatingRemainder(dividingBy: 5.5)
            let loopDraw: CGFloat = cycle < 1.2 ? CGFloat(cycle / 1.2) : 1
            let trim = progress * loopDraw
            ZStack {
                // glow breathe
                Circle()
                    .fill(RadialGradient(colors: [Color(hex: 0xffd36b).opacity(0.55), .clear],
                                         center: .center, startRadius: 20, endRadius: 140))
                    .frame(width: 260, height: 260)
                    .scaleEffect(1 + 0.06 * CGFloat(sin(t / 3.2 * .pi * 2)) + 0.02 * CGFloat(br))
                    .opacity(0.35 + 0.25 * (0.5 + 0.5 * sin(t / 3.2 * .pi * 2)))

                Image("cvD").resizable().scaledToFill()
                    .frame(width: 180, height: 180).clipShape(Circle())
                    .shadow(color: .black.opacity(0.35), radius: 24, y: 12)
                    .scaleEffect((0.96 + 0.04 * progress) * (1 + 0.012 * CGFloat(br)))
                    .offset(y: floatY)

                // orbit doodle + sparkles
                Canvas { ctx, sz in
                    let mid = CGPoint(x: sz.width / 2, y: sz.height / 2)
                    var orbit = Path()
                    let n = 90
                    for i in 0...n {
                        let a = Double(i) / Double(n) * 2 * .pi * 1.15 - 0.3
                        let rx = 118.0 + 4 * sin(a * 3 + t)
                        let ry = 118.0 + 3 * cos(a * 2 + t * 0.7)
                        let pt = CGPoint(x: mid.x + CGFloat(cos(a) * rx), y: mid.y + CGFloat(sin(a) * ry) + floatY * 0.4)
                        i == 0 ? orbit.move(to: pt) : orbit.addLine(to: pt)
                    }
                    ctx.stroke(orbit.trimmedPath(from: 0, to: trim), with: .color(.white.opacity(0.8)),
                               style: StrokeStyle(lineWidth: 2.4, lineCap: .round))
                    // zig accents
                    var zig = Path()
                    let ox = mid.x + 90, oy = mid.y - 40 + floatY
                    zig.move(to: CGPoint(x: ox, y: oy))
                    for k in 0..<6 {
                        zig.addLine(to: CGPoint(x: ox + CGFloat(k) * 10, y: oy + (k % 2 == 0 ? -8 : 8)))
                    }
                    ctx.stroke(zig.trimmedPath(from: 0, to: trim), with: .color(.white.opacity(0.75)),
                               style: StrokeStyle(lineWidth: 2.2, lineCap: .round, lineJoin: .round))
                    // stars
                    for (i, pt) in [CGPoint(x: mid.x - 110, y: mid.y - 90),
                                    CGPoint(x: mid.x + 105, y: mid.y + 95),
                                    CGPoint(x: mid.x - 95, y: mid.y + 100)].enumerated() {
                        let tw = 0.5 + 0.5 * sin(t * 2.5 + Double(i))
                        var star = Path()
                        let r: CGFloat = 7
                        star.move(to: CGPoint(x: pt.x, y: pt.y - r))
                        star.addQuadCurve(to: CGPoint(x: pt.x + r, y: pt.y), control: pt)
                        star.addQuadCurve(to: CGPoint(x: pt.x, y: pt.y + r), control: pt)
                        star.addQuadCurve(to: CGPoint(x: pt.x - r, y: pt.y), control: pt)
                        star.addQuadCurve(to: CGPoint(x: pt.x, y: pt.y - r), control: pt)
                        ctx.stroke(star, with: .color(.white.opacity(0.55 + 0.4 * tw)),
                                   style: StrokeStyle(lineWidth: 1.6, lineCap: .round))
                    }
                }
                .frame(width: 300, height: 300)
                .rotationEffect(.degrees(0.7 * sin(t / (beat * 4) * .pi * 2) * Double(trim)))
                .scaleEffect(1 + 0.012 * CGFloat(br) * trim)
            }
            .opacity(0.25 + 0.75 * Double(progress))
        }
    }
}

// MARK: Page 3 — lyrics smooth scroll (continuous interpolate)

private struct LyricsAnim: View {
    var progress: CGFloat
    var active: Bool
    private let lines: [(String, String)] = [
        ("夜色漫过旧城的街", "Night spills over the old town"),
        ("你说别回头", "You said don't look back"),
        ("弄丢了爱 失去了你", "Lost my love, lost you too"),
        ("橘子海 在心里起伏", "A tangerine sea rising in me"),
        ("风把晚霞吹散", "The wind scatters the dusk"),
        ("我们终会再见", "We will meet again"),
        ("在梦里", "In a dream"),
        ("在每一个清晨", "In every morning"),
    ]
    private let lineH: CGFloat = 64
    private let dwell: Double = 1.9

    var body: some View {
        TimelineView(.animation(minimumInterval: 1 / 30, paused: !active)) { tl in
            let t = active ? tl.date.timeIntervalSinceReferenceDate : 0
            // Continuous position — no integer jumps. Ease within each dwell for a soft settle.
            let raw = t / dwell
            let base = floor(raw)
            let frac = raw - base
            let easedFrac = smooth(frac)          // linger then ease to next
            let pos = base + easedFrac            // continuous scroll index
            let n = lines.count

            ZStack {
                // yellow highlight under active line, width follows frac
                let highlightW = 36 + CGFloat(smooth(min(1, frac * 1.35))) * 210
                Capsule()
                    .fill(Color(hex: 0xffd23f).opacity(0.36))
                    .frame(width: highlightW, height: 32)
                    .offset(y: -lineH * 0.15)
                    .opacity(Double(progress))

                ForEach(0..<n, id: \.self) { i in
                    let d = Self.wrap(Double(i) - pos, n: Double(n))
                    let ad = abs(d)
                    let op = max(0, 1.0 - ad * 0.55) * (ad < 0.15 ? 1 : (ad < 1.1 ? 0.55 : 0.22))
                    let blur = min(2.2, CGFloat(ad) * 1.15)
                    let scale = 1.0 - min(0.12, ad * 0.07)
                    let weight: Font.Weight = ad < 0.35 ? .heavy : .bold
                    let size: CGFloat = 22 + CGFloat(max(0, 1 - ad)) * 6
                    VStack(alignment: .leading, spacing: 4) {
                        Text(lines[i].0)
                            .font(.system(size: size, weight: weight))
                        Text(lines[i].1)
                            .font(.system(size: ad < 0.35 ? 15 : 13))
                            .opacity(ad < 0.35 ? 0.78 : 0.35)
                    }
                    .foregroundStyle(.white)
                    .scaleEffect(scale, anchor: .leading)
                    .blur(radius: blur)
                    .opacity(op * Double(progress))
                    .offset(y: CGFloat(d) * lineH)
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
            .padding(.horizontal, 32)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .center)
            .opacity(Double(progress))
            .mask(
                LinearGradient(colors: [.clear, .black, .black, .clear],
                               startPoint: .top, endPoint: .bottom)
            )
        }
    }

    private func smooth(_ x: Double) -> Double {
        let u = min(1, max(0, x))
        return u * u * u * (u * (u * 6 - 15) + 10)
    }
    private static func wrap(_ v: Double, n: Double) -> Double {
        var d = v
        while d > n / 2 { d -= n }
        while d < -n / 2 { d += n }
        return d
    }
}

// MARK: Page 4 — smooth Cover Flow (continuous rotateY / perspective)

private struct StackAnim: View {
    var progress: CGFloat
    var active: Bool
    private let covers = ["cvC", "cvE", "cvF", "cvD", "cvG", "cvA"]
    /// Seconds to advance one card — continuous, never snaps.
    private let pace: Double = 2.6

    var body: some View {
        TimelineView(.animation(minimumInterval: 1 / 30, paused: !active)) { tl in
            let t = active ? tl.date.timeIntervalSinceReferenceDate : 0
            // Continuous floating index (no floor jumps for pose).
            let pos = t / pace
            let n = covers.count
            let spacing: CGFloat = 118
            let maxAngle: Double = 58

            ZStack {
                ForEach(0..<n, id: \.self) { i in
                    let d = Self.wrap(Double(i) - pos, n: Double(n))
                    let ad = abs(d)
                    // Cover Flow: rotateY toward center, fan on X
                    let angle = -sin(min(.pi / 2, max(-.pi / 2, d * 0.95))) * maxAngle
                    let x = CGFloat(sin(d * 0.85)) * spacing * 1.35
                    let zBoost = exp(-ad * ad * 0.55)          // center pops forward
                    let scale = 0.78 + 0.22 * zBoost
                    let op = max(0.18, 0.25 + 0.75 * zBoost)
                    let y = CGFloat(8 * (1 - zBoost)) + CGFloat(sin(t * 1.05 + Double(i) * 0.4)) * 2 * CGFloat(zBoost)

                    Image(covers[i]).resizable().scaledToFill()
                        .frame(width: 188, height: 188)
                        .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
                        .overlay {
                            RoundedRectangle(cornerRadius: 14, style: .continuous)
                                .stroke(.white.opacity(0.15 + 0.45 * zBoost), lineWidth: 1)
                        }
                        .shadow(color: .black.opacity(0.15 + 0.35 * zBoost), radius: 8 + 16 * zBoost, y: 8 + 10 * zBoost)
                        .scaleEffect(scale * (0.92 + 0.08 * progress))
                        .rotation3DEffect(
                            .degrees(angle),
                            axis: (x: 0, y: 1, z: 0),
                            anchor: d >= 0 ? .leading : .trailing,
                            anchorZ: 0,
                            perspective: 0.65
                        )
                        .offset(x: x, y: y)
                        .opacity(op * Double(progress))
                        .zIndex(zBoost * 100)
                }
            }
            .frame(height: 300)
            .opacity(Double(progress))
        }
    }
    private static func wrap(_ v: Double, n: Double) -> Double {
        var d = v
        while d > n / 2 { d -= n }
        while d <= -n / 2 { d += n }
        return d
    }
}

// MARK: Page 5 — island expand/contract + progress tick

private struct IslandAnim: View {
    var progress: CGFloat
    var active: Bool
    var body: some View {
        TimelineView(.animation(minimumInterval: 1 / 30, paused: !active)) { tl in
            let t = active ? tl.date.timeIntervalSinceReferenceDate : 0
            // expand for 1.6s, hold, contract — loop ~5s
            let cycle = t.truncatingRemainder(dividingBy: 5.2)
            let expand: CGFloat = {
                if cycle < 1.4 { return CGFloat(easeOut(cycle / 1.4)) }
                if cycle < 3.4 { return 1 }
                if cycle < 4.6 { return CGFloat(1 - easeOut((cycle - 3.4) / 1.2)) }
                return 0
            }()
            let prog = (0.22 + t * 0.04).truncatingRemainder(dividingBy: 0.85)
            let wave = 0.5 + 0.5 * sin(t * 6)

            VStack(spacing: 28) {
                // Dynamic Island mock
                ZStack {
                    Capsule().fill(Color.black)
                    HStack(spacing: 10) {
                        Image("cvG").resizable().scaledToFill()
                            .frame(width: 28 + 16 * expand, height: 28 + 16 * expand)
                            .clipShape(RoundedRectangle(cornerRadius: 8 + 4 * expand, style: .continuous))
                        if expand > 0.15 {
                            VStack(alignment: .leading, spacing: 2) {
                                Text("月光人").font(.system(size: 14, weight: .semibold)).lineLimit(1)
                                Text("Ólafur Arnalds").font(.system(size: 11)).foregroundStyle(.secondary).lineLimit(1)
                            }
                            .opacity(Double(expand))
                            .offset(x: (1 - expand) * -8)
                        }
                        Spacer(minLength: 0)
                        // waveform bars
                        HStack(spacing: 3) {
                            ForEach(0..<5, id: \.self) { i in
                                Capsule().fill(Theme.lime)
                                    .frame(width: 3, height: 6 + CGFloat(8 + 6 * sin(t * 5 + Double(i))) * (0.4 + 0.6 * expand))
                            }
                        }
                        .opacity(0.7 + 0.3 * wave)
                    }
                    .padding(.horizontal, 14)
                    .padding(.vertical, 10 + 6 * expand)
                }
                .frame(width: 160 + 120 * expand, height: 44 + 40 * expand)
                .shadow(color: .black.opacity(0.45), radius: 18, y: 10)
                .animation(nil, value: expand) // driven by timeline

                // Lock-screen player
                ZStack {
                    Image("cvG").resizable().scaledToFill().blur(radius: 28).brightness(-0.25)
                    VStack(alignment: .leading, spacing: 8) {
                        Text("锁屏 · 9:41").font(.system(size: 12)).opacity(0.7)
                        Text("月光人").font(.system(size: 16, weight: .semibold))
                        HStack(spacing: 8) {
                            Text(fmt(72 + t)).font(.system(size: 11)).monospacedDigit()
                            GeometryReader { g in
                                Capsule().fill(.white.opacity(0.3)).overlay(alignment: .leading) {
                                    Capsule().fill(.white).frame(width: g.size.width * CGFloat(prog))
                                }
                            }.frame(height: 3)
                            Text("4:16").font(.system(size: 11)).monospacedDigit()
                        }
                    }
                    .foregroundStyle(.white)
                    .padding(18)
                }
                .frame(height: 130)
                .clipShape(RoundedRectangle(cornerRadius: 26, style: .continuous))
                .padding(.horizontal, 46)
                .scaleEffect(0.97 + 0.03 * expand)
                .opacity(0.85 + 0.15 * Double(expand))
            }
            .opacity(Double(progress))
        }
    }
    private func easeOut(_ x: Double) -> Double {
        let u = min(1, max(0, x)); return 1 - pow(1 - u, 3)
    }
    private func fmt(_ t: Double) -> String {
        let s = Int(t) % 256; return "\(s / 60):" + String(format: "%02d", s % 60)
    }
}

// MARK: - Sources list + connect

struct SourcesView: View {
    var embedded = false          // true when pushed from Settings
    var initialConnect: SourceKind? = nil
    var onContinue: (() -> Void)? = nil
    @ObservedObject private var store = SourceStore.shared
    @State private var editing: SourceKind?
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        ZStack {
            let p = OnboardPalette.sources
            BloomField(base: p.base, blobs: p.blobs)
            Color.black.opacity(0.45).ignoresSafeArea()

            VStack(alignment: .leading, spacing: 0) {
                HStack {
                    if embedded || onContinue == nil {
                        Button { dismiss() } label: { CircleIconButton(icon: "chevron.left", dark: true) }
                    } else {
                        Color.clear.frame(width: 40, height: 40)
                    }
                    Spacer()
                    if onContinue != nil { Text("2 / 3").font(.system(size: 13)).opacity(0.6) }
                }
                .padding(.horizontal, 20).padding(.top, 8)

                Text("选择音乐来源").font(.system(size: 28, weight: .bold)).padding(.horizontal, 20).padding(.top, 10)
                Text("一次使用一个来源，译梦只显示它的曲库。")
                    .font(.system(size: 16)).opacity(0.7)
                    .padding(.horizontal, 20).padding(.top, 4)

                VStack(spacing: 0) {
                    ForEach(SourceKind.allCases) { kind in
                        sourceRow(store.config(for: kind))
                    }
                }
                .padding(.top, 22)

                HStack(spacing: 8) {
                    Image(systemName: "info.circle").font(.system(size: 14))
                    Text("之后可在 我的 › 音乐来源 中修改")
                }
                .font(.system(size: 13)).opacity(0.55)
                .padding(.horizontal, 20).padding(.top, 18)

                Spacer()

                if let onContinue {
                    Button { onContinue() } label: {
                        Text("继续 · \(store.active.title)")
                    }
                    .buttonStyle(PrimaryButtonStyle())
                    .padding(.horizontal, 20).padding(.bottom, 44)
                }
            }
            .foregroundStyle(.white)
        }
        .preferredColorScheme(.dark)
        .onAppear { if let k = initialConnect { editing = k } }
        .sheet(item: $editing) { kind in
            SourceConnectView(kind: kind) { editing = nil }
                .presentationDetents([.large])
        }
    }

    private func sourceRow(_ c: SourceConfig) -> some View {
        HStack(spacing: 14) {
            RoundedRectangle(cornerRadius: 14, style: .continuous)
                .fill(c.kind == .core
                      ? AnyShapeStyle(LinearGradient(colors: [Color(hex: 0xff3d8b), Color(hex: 0x7a1cff)], startPoint: .topLeading, endPoint: .bottomTrailing))
                      : AnyShapeStyle(c.kind.tile))
                .frame(width: 48, height: 48)
                .overlay { SourceGlyph(kind: c.kind) }

            VStack(alignment: .leading, spacing: 2) {
                Text(c.kind.title).font(.system(size: 16, weight: .semibold))
                Text(c.kind.subtitle).font(.system(size: 13)).opacity(0.6)
            }
            Spacer()
            if !c.kind.isImplemented {
                Text("即将推出").font(.system(size: 12)).opacity(0.45)
            } else if store.active == c.kind && c.connected {
                HStack(spacing: 6) {
                    Circle().fill(Theme.lime).frame(width: 7, height: 7)
                    Text("使用中").font(.system(size: 13)).foregroundStyle(Theme.lime)
                }
                .onTapGesture { if !c.kind.isLocal { editing = c.kind } }
            } else if c.connected {
                Button(store.switching ? "切换中…" : "切换") { Task { await store.activate(c.kind) } }
                    .font(.system(size: 13, weight: .semibold)).foregroundStyle(.white)
                    .padding(.horizontal, 16).frame(height: 34)
                    .background(Capsule().fill(Color.white.opacity(0.14)))
                    .disabled(store.switching)
            } else {
                Button("连接") { editing = c.kind }
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(.white)
                    .padding(.horizontal, 16).frame(height: 34)
                    .background(Capsule().fill(Color.white.opacity(0.14)))
            }
        }
        .padding(.horizontal, 20)
        .frame(height: 72)
        .opacity(c.kind.isImplemented ? 1 : 0.6)
        .overlay(alignment: .bottom) { Rectangle().fill(Color.white.opacity(0.07)).frame(height: 1) }
    }
}

struct SourceConnectView: View {
    let kind: SourceKind
    var onClose: () -> Void
    @ObservedObject private var store = SourceStore.shared
    @State private var draft: SourceConfig
    @State private var showSecret = false
    @State private var busy = false
    @State private var embyMode: LibraryStore.EmbyMode = LibraryStore.embyMode
    @State private var embyPick: String?
    @ObservedObject private var lib = LibraryStore.shared

    init(kind: SourceKind, onClose: @escaping () -> Void) {
        self.kind = kind
        self.onClose = onClose
        var c = SourceConfig(kind: kind)
        #if DEBUG
        c.host = kind.hostPlaceholder
        #endif
        _draft = State(initialValue: c)
    }

    var body: some View {
        ZStack {
            let p = OnboardPalette.connect
            BloomField(base: p.base, blobs: p.blobs)
            Color.black.opacity(0.55).ignoresSafeArea()

            VStack(alignment: .leading, spacing: 0) {
                HStack {
                    Button { onClose() } label: { CircleIconButton(icon: "chevron.left", dark: true) }
                    Spacer()
                }
                .padding(.horizontal, 20).padding(.top, 12)

                HStack(spacing: 14) {
                    RoundedRectangle(cornerRadius: 14).fill(kind.tile).frame(width: 48, height: 48)
                        .overlay { SourceGlyph(kind: kind) }
                    VStack(alignment: .leading, spacing: 2) {
                        Text(kind.title).font(.system(size: 28, weight: .bold))
                        Text(connectSubtitle).font(.system(size: 13)).opacity(0.6)
                    }
                }
                .padding(.horizontal, 20).padding(.top, 8)

                if kind.isLocal {
                    localBody
                } else {
                    formBody
                }

                Spacer()

                Button {
                    busy = true
                    Task { @MainActor in
                        if kind == .navidrome {
                            var h = draft.host.isEmpty ? LibraryStore.defaultHost : draft.host
                            h = h.replacingOccurrences(of: ":443", with: "")
                            let u = draft.username.isEmpty ? LibraryStore.defaultUser : draft.username
                            let pw = draft.secret.isEmpty
                                ? (Keychain.get(service: SourceKind.navidrome.keychainService, account: u) ?? "")
                                : draft.secret
                            await LibraryStore.shared.connect(host: h, username: u, password: pw)
                            draft.host = h
                            draft.username = u
                            draft.secret = pw
                            draft.connected = LibraryStore.shared.connected
                            draft.statusNote = LibraryStore.shared.status
                            store.upsert(draft)
                        } else if kind == .emby {
                            // Emby / Jellyfin: password (may be empty) or API key. Secrets → Keychain only.
                            let u = draft.username.trimmingCharacters(in: .whitespaces)
                            var c = draft; c.username = u; c.secret = ""; store.upsert(c)
                            LibraryStore.embyMode = embyMode
                            if store.active != .emby {
                                // Activate first (switch source), then log in with what was typed.
                                await store.activate(.emby)
                            }
                            await LibraryStore.shared.connectEmby(host: draft.host, mode: embyMode, username: u, secret: draft.secret, userId: embyPick)
                            let typed = draft.secret
                            draft = store.config(for: .emby)
                            if !LibraryStore.shared.embyUsers.isEmpty { draft.secret = typed }   // keep the key while picking a user
                            draft.connected = LibraryStore.shared.connected
                            draft.statusNote = LibraryStore.shared.status
                            store.upsert(draft)
                        } else if kind == .fnos {
                            // 飞牛音乐: the separate 「音乐」 app account (not the fnOS system login).
                            let u = draft.username.trimmingCharacters(in: .whitespaces)
                            let pw = draft.secret.isEmpty ? (Keychain.get(service: SourceKind.fnos.keychainService, account: u) ?? "") : draft.secret
                            var c = draft; c.username = u; c.secret = ""; store.upsert(c)
                            if !pw.isEmpty { Keychain.set(pw, service: SourceKind.fnos.keychainService, account: u) }
                            if store.active != .fnos { await store.activate(.fnos) }
                            else { await LibraryStore.shared.connectFnos(host: draft.host, username: u, password: pw) }
                            draft = store.config(for: .fnos)
                            draft.connected = LibraryStore.shared.connected
                            draft.statusNote = LibraryStore.shared.status
                            store.upsert(draft)
                        } else {
                            try? await Task.sleep(for: .milliseconds(400))
                            draft = store.connectDemo(draft)
                        }
                        busy = false
                    }
                } label: {
                    Text(busy ? "连接中…" : (draft.connected ? "保存并同步" : "连接并保存"))
                }
                .buttonStyle(PrimaryButtonStyle())
                .disabled(busy)
                .padding(.horizontal, 20)

                Text("密码仅保存在本机钥匙串")
                    .font(.system(size: 13)).opacity(0.55)
                    .frame(maxWidth: .infinity).padding(.top, 12).padding(.bottom, 36)
            }
            .foregroundStyle(.white)
        }
        .preferredColorScheme(.dark)
        .onAppear {
            var c = store.config(for: kind)
            #if DEBUG
            if c.host.isEmpty { c.host = kind.hostPlaceholder }
            #endif
            if kind == .navidrome {
                if c.host.isEmpty || c.host.contains("home.lan") { c.host = LibraryStore.defaultHost }
                c.host = c.host.replacingOccurrences(of: ":443", with: "")
                if c.username.isEmpty { c.username = LibraryStore.defaultUser }
                if c.secret.isEmpty, let pw = Keychain.get(service: SourceKind.navidrome.keychainService, account: c.username) {
                    c.secret = pw
                }
            }
            draft = c
        }
    }

    private var connectSubtitle: String {
        switch kind {
        case .navidrome: "通过 Subsonic API 连接"
        case .emby: "通过 Emby REST API 连接"
        case .fnos: "飞牛音乐 · 主机地址"
        case .core: "译梦 Core · 主机 + Token"
        case .local: "从本机或文件 App 导入"
        }
    }

    @ViewBuilder private var localBody: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("演示模式会开启本机曲库入口。正式版将使用文件选择器与媒体库权限。")
                .font(.system(size: 15)).opacity(0.75).padding(.top, 18)
            if draft.connected {
                successBanner("已开启 · 本机音乐")
            }
        }
        .padding(.horizontal, 20)
    }

    @ViewBuilder private var formBody: some View {
        VStack(alignment: .leading, spacing: 14) {
            if kind == .emby {
                Picker("登录方式", selection: $embyMode) {
                    Text("账号密码").tag(LibraryStore.EmbyMode.password)
                    Text("API Key").tag(LibraryStore.EmbyMode.apiKey)
                }
                .pickerStyle(.segmented)
                .onChange(of: embyMode) { _, _ in draft.secret = ""; embyPick = nil; lib.embyUsers = [] }
            }
            field("服务器地址", text: $draft.host, prompt: kind.hostPlaceholder)
            if kind.usesUserPass {
                field(kind == .emby && embyMode == .apiKey ? "用户名（可选）" : "用户名", text: $draft.username,
                      prompt: kind == .emby && embyMode == .apiKey ? "留空则从列表中选择" : "用户名")
            }
            HStack {
                field(kind == .emby ? (embyMode == .apiKey ? "API Key" : "密码（可留空）")
                      : (kind.usesToken && !kind.usesUserPass ? "API Key / Token" : "密码 / API Key"),
                      text: $draft.secret, secure: !showSecret)
                Button { showSecret.toggle() } label: {
                    Image(systemName: showSecret ? "eye" : "eye.slash").opacity(0.6)
                }.padding(.trailing, 8)
            }
            if kind == .emby, !lib.embyUsers.isEmpty {
                VStack(alignment: .leading, spacing: 8) {
                    Text("选择用户").font(.system(size: 13)).opacity(0.6)
                    ForEach(lib.embyUsers, id: \.id) { u in
                        Button { embyPick = u.id } label: {
                            HStack {
                                Image(systemName: embyPick == u.id ? "largecircle.fill.circle" : "circle")
                                Text(u.name); if u.isAdmin { Text("管理员").font(.system(size: 11)).opacity(0.55) }
                                Spacer()
                            }.padding(.vertical, 6).contentShape(Rectangle())
                        }.buttonStyle(.plain)
                    }
                }
            }
            Toggle(isOn: $draft.allowSelfSigned) {
                Text("允许自签名证书").font(.system(size: 16))
            }
            .tint(Theme.lime)
            .padding(.top, 4)

            if draft.connected, !draft.statusNote.isEmpty {
                successBanner(draft.statusNote)
            } else if !draft.statusNote.isEmpty, !draft.connected {
                Text(draft.statusNote).font(.system(size: 13)).foregroundStyle(.orange).padding(.top, 8)
            }
        }
        .padding(.horizontal, 20).padding(.top, 18)
    }

    private func field(_ label: String, text: Binding<String>, secure: Bool = false, prompt: String = "") -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(label).font(.system(size: 13)).opacity(0.6)
            Group {
                if secure { SecureField("", text: text) } else { TextField("", text: text, prompt: Text(prompt).foregroundStyle(.white.opacity(0.3))) }
            }
            .font(.system(size: 16))
            .padding(.horizontal, 14).frame(height: 50)
            .background(RoundedRectangle(cornerRadius: 14).fill(Color.white.opacity(0.08)))
            .overlay {
                RoundedRectangle(cornerRadius: 14)
                    .stroke(Theme.lime.opacity(text.wrappedValue.isEmpty ? 0 : 0.85), lineWidth: 1.5)
            }
        }
    }

    private func successBanner(_ text: String) -> some View {
        HStack(spacing: 10) {
            Image(systemName: "checkmark.circle.fill").foregroundStyle(Theme.lime)
            Text(text).font(.system(size: 13))
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 14).fill(Theme.lime.opacity(0.10)))
        .padding(.top, 8)
    }
}

// MARK: - Root gate

struct RootGate: View {
    @ObservedObject var state: AppState
    @State private var phase: FirstRunPhase

    static func arg(_ k: String) -> String? {
        let args = ProcessInfo.processInfo.arguments
        return args.firstIndex(of: k).flatMap { $0 + 1 < args.count ? args[$0 + 1] : nil }
    }
    static func argInt(_ k: String) -> Int? { arg(k).flatMap(Int.init) }
    static func argSource(_ k: String) -> SourceKind? {
        guard let v = arg(k)?.lowercased() else { return nil }
        if v == "nv" { return .navidrome }
        return SourceKind(rawValue: v)
    }

    init(state: AppState) {
        self.state = state
        let args = ProcessInfo.processInfo.arguments
        if args.contains("-fresh") { FirstRun.reset() }
        func arg(_ k: String) -> String? { args.firstIndex(of: k).flatMap { $0 + 1 < args.count ? args[$0 + 1] : nil } }
        let forced: FirstRunPhase? = {
            switch arg("-screen") {
            case "launch": return .launch
            case "onboarding": return .onboarding
            case "sources": return .sources
            case "home", "album", "artist", "nowplaying", "np-cloud", "np-zig", "np-ink",
                 "lyrics", "library", "search", "collection", "me": return .main
            default: return nil
            }
        }()
        if let forced { _phase = State(initialValue: forced) }
        else if FirstRun.isDone { _phase = State(initialValue: .intro) }
        else { _phase = State(initialValue: .launch) }
    }

    var body: some View {
        ZStack {
            switch phase {
            case .launch:
                LaunchView { withAnimation(.easeInOut(duration: 0.35)) { phase = .onboarding } }
            case .intro:
                LaunchView(short: true) { withAnimation(.easeInOut(duration: 0.3)) { phase = .main } }
            case .onboarding:
                OnboardingView(
                    onSkip: { withAnimation { phase = .sources } },
                    onDone: { withAnimation { phase = .sources } },
                    initialPage: Self.argInt("-page") ?? 0
                )
            case .sources:
                SourcesView(
                    initialConnect: Self.argSource("-source") ?? (LibraryStore.shared.hasSource ? nil : .navidrome),
                    onContinue: {
                    FirstRun.isDone = true
                    withAnimation(.easeInOut(duration: 0.4)) { phase = .main }
                })
            case .main:
                Shell().environmentObject(state)
            }
        }
        .animation(.easeInOut(duration: 0.35), value: phase)
    }
}
