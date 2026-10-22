import SwiftUI

// MARK: - First-run gate

enum FirstRunPhase: String { case launch, onboarding, sources, main }

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

struct MoonwaveIcon: View {
    var progress: CGFloat = 1
    var size: CGFloat = 140
    var body: some View {
        Canvas { ctx, sz in
            let s = sz.width / 1024
            var moon = Path()
            // Approximate crescent via two arcs in design coords
            moon.addEllipse(in: CGRect(x: 255, y: 170, width: 430, height: 430))
            var cut = Path()
            cut.addEllipse(in: CGRect(x: 370, y: 210, width: 330, height: 330))
            let crescent = moon.subtracting(cut).applying(CGAffineTransform(scaleX: s, y: s))
            ctx.stroke(crescent.trimmedPath(from: 0, to: progress), with: .color(.white),
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
            ctx.stroke(wave.trimmedPath(from: 0, to: progress), with: .color(.white),
                       style: StrokeStyle(lineWidth: 5.5 * s, lineCap: .round))

            // Sparkle
            var spark = Path()
            let cx: CGFloat = 720 * s, cy: CGFloat = 254 * s, r: CGFloat = 34 * s
            spark.move(to: CGPoint(x: cx, y: cy - r))
            spark.addQuadCurve(to: CGPoint(x: cx + r, y: cy), control: CGPoint(x: cx, y: cy))
            spark.addQuadCurve(to: CGPoint(x: cx, y: cy + r), control: CGPoint(x: cx, y: cy))
            spark.addQuadCurve(to: CGPoint(x: cx - r, y: cy), control: CGPoint(x: cx, y: cy))
            spark.addQuadCurve(to: CGPoint(x: cx, y: cy - r), control: CGPoint(x: cx, y: cy))
            ctx.stroke(spark.trimmedPath(from: 0, to: progress), with: .color(.white),
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
    var onFinished: () -> Void
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
                MoonwaveIcon(progress: draw, size: 140)
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
        withAnimation(.easeOut(duration: 1.6)) { bloom = 1; bloomOp = 1 }
        try? await Task.sleep(for: .milliseconds(300))
        withAnimation(.easeOut(duration: 1.2)) { draw = 1 }
        try? await Task.sleep(for: .milliseconds(1600))
        withAnimation(.easeOut(duration: 0.8)) { word = 1 }
        try? await Task.sleep(for: .milliseconds(1600))
        withAnimation(.easeInOut(duration: 0.55)) { fadeAll = 0 }
        try? await Task.sleep(for: .milliseconds(550))
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
            .onChange(of: page) { _, _ in redraw() }

            VStack {
                HStack {
                    Spacer()
                    Button("跳过") { onSkip() }
                        .font(.system(size: 16)).foregroundStyle(.white.opacity(0.75)).padding(20)
                }
                Spacer()
                HStack(spacing: 6) {
                    ForEach(0..<5, id: \.self) { i in
                        Capsule().fill(.white).frame(width: i == page ? 18 : 6, height: 6)
                            .opacity(i == page ? 1 : 0.35)
                            .animation(.easeOut(duration: 0.35), value: page)
                    }
                    Spacer()
                }
                .padding(.horizontal, 28).padding(.bottom, 16)

                Button {
                    if page < 4 { withAnimation { page += 1 } } else { onDone() }
                } label: {
                    Text(page == 4 ? "开始使用" : "继续")
                }
                .buttonStyle(PrimaryButtonStyle())
                .padding(.horizontal, 20).padding(.bottom, 44)
            }
        }
        .preferredColorScheme(.dark)
        .onAppear { page = initialPage; redraw() }
    }

    @ViewBuilder
    private func pageContent(_ p: OnboardingPage) -> some View {
        VStack(spacing: 0) {
            ZStack {
                OnboardArt(page: p.id, progress: draw)
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
}

/// Simplified page illustrations (doodle + mock UI).
struct OnboardArt: View {
    let page: Int
    var progress: CGFloat
    var body: some View {
        GeometryReader { g in
            let w = g.size.width
            ZStack {
                switch page {
                case 0:
                    // Hub: moon + 4 source tiles
                    MoonwaveIcon(progress: progress, size: 90)
                        .position(x: w / 2, y: 220)
                    tile(.fnos, x: w * 0.18, y: 120)
                    tile(.navidrome, x: w * 0.82, y: 120)
                    tile(.emby, x: w * 0.18, y: 340)
                    tile(.local, x: w * 0.82, y: 340)
                    dashedSpokes(w: w, progress: progress)
                case 1:
                    CoverBubble(name: "cvD", progress: progress)
                case 2:
                    LyricsPreview(progress: progress)
                case 3:
                    StackPreview(progress: progress)
                default:
                    IslandPreview(progress: progress)
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private func tile(_ k: SourceKind, x: CGFloat, y: CGFloat) -> some View {
        RoundedRectangle(cornerRadius: 14, style: .continuous)
            .fill(k.tile)
            .frame(width: 48, height: 48)
            .overlay { SourceGlyph(kind: k).opacity(Double(progress)) }
            .position(x: x, y: y)
            .opacity(Double(progress))
    }

    private func dashedSpokes(w: CGFloat, progress: CGFloat) -> some View {
        Canvas { ctx, _ in
            var p = Path()
            let c = CGPoint(x: w / 2, y: 220)
            for pt in [CGPoint(x: w * 0.18, y: 120), CGPoint(x: w * 0.82, y: 120),
                       CGPoint(x: w * 0.18, y: 340), CGPoint(x: w * 0.82, y: 340)] {
                p.move(to: pt); p.addLine(to: c)
            }
            ctx.stroke(p.trimmedPath(from: 0, to: progress), with: .color(.white.opacity(0.55)),
                       style: StrokeStyle(lineWidth: 1.5, lineCap: .round, dash: [2, 7]))
        }
    }
}

private struct CoverBubble: View {
    let name: String; var progress: CGFloat
    var body: some View {
        ZStack {
            Image(name).resizable().scaledToFill()
                .frame(width: 180, height: 180).clipShape(Circle())
                .shadow(color: .black.opacity(0.35), radius: 24, y: 12)
            Canvas { ctx, sz in
                var orbit = Path()
                orbit.addEllipse(in: CGRect(x: 40, y: 40, width: sz.width - 80, height: sz.height - 80))
                ctx.stroke(orbit.trimmedPath(from: 0, to: progress), with: .color(.white.opacity(0.75)),
                           style: StrokeStyle(lineWidth: 2.2, lineCap: .round, dash: [1, 0]))
            }
        }
        .frame(width: 280, height: 280)
        .opacity(0.3 + 0.7 * Double(progress))
    }
}

private struct LyricsPreview: View {
    var progress: CGFloat
    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("你说别回头").font(.system(size: 22, weight: .heavy)).opacity(0.45)
            Text("You said don't look back").font(.system(size: 13)).opacity(0.35)
            ZStack(alignment: .leading) {
                Capsule().fill(Color(hex: 0xffd23f).opacity(0.4 * Double(progress)))
                    .frame(width: 220 * progress, height: 28).offset(y: -2)
                Text("弄丢了爱 失去了你").font(.system(size: 28, weight: .heavy))
            }
            Text("Lost my love, lost you too").font(.system(size: 15)).opacity(0.75)
            Text("橘子海 在心里起伏").font(.system(size: 22, weight: .heavy)).opacity(0.3).blur(radius: 1)
        }
        .foregroundStyle(.white)
        .padding(.horizontal, 32)
        .frame(maxWidth: .infinity, alignment: .leading)
        .opacity(Double(progress))
    }
}

private struct StackPreview: View {
    var progress: CGFloat
    var body: some View {
        ZStack {
            Image("cvF").resizable().scaledToFill().frame(width: 200, height: 200)
                .clipShape(RoundedRectangle(cornerRadius: 8)).rotation3DEffect(.degrees(-58), axis: (1, 0, 0))
                .offset(y: -40).opacity(0.55)
            Image("cvE").resizable().scaledToFill().frame(width: 200, height: 200)
                .clipShape(RoundedRectangle(cornerRadius: 8)).rotation3DEffect(.degrees(-58), axis: (1, 0, 0))
                .offset(y: -10).opacity(0.75)
            Image("cvC").resizable().scaledToFill().frame(width: 200, height: 200)
                .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
                .shadow(color: .black.opacity(0.4), radius: 24, y: 16)
                .overlay { RoundedRectangle(cornerRadius: 14).stroke(.white.opacity(0.5), lineWidth: 1) }
        }
        .scaleEffect(0.85 + 0.15 * progress)
        .opacity(Double(progress))
    }
}

private struct IslandPreview: View {
    var progress: CGFloat
    var body: some View {
        VStack(spacing: 28) {
            // Compact Dynamic Island mock
            HStack(spacing: 10) {
                Image("cvG").resizable().scaledToFill().frame(width: 40, height: 40)
                    .clipShape(RoundedRectangle(cornerRadius: 10))
                VStack(alignment: .leading, spacing: 2) {
                    Text("月光人").font(.system(size: 14, weight: .semibold))
                    Text("Ólafur Arnalds").font(.system(size: 12)).foregroundStyle(.secondary)
                }
                Spacer()
                Image(systemName: "waveform").foregroundStyle(Theme.lime)
            }
            .padding(16)
            .frame(width: 270)
            .background(Capsule().fill(Color.black))
            .shadow(color: .black.opacity(0.4), radius: 20, y: 12)

            // Lock-screen player mock
            ZStack {
                Image("cvG").resizable().scaledToFill().blur(radius: 28).brightness(-0.25)
                VStack(alignment: .leading, spacing: 8) {
                    Text("锁屏 · 9:41").font(.system(size: 12)).opacity(0.7)
                    Text("月光人").font(.system(size: 16, weight: .semibold))
                    HStack(spacing: 8) {
                        Text("1:12").font(.system(size: 11)).monospacedDigit()
                        GeometryReader { g in
                            Capsule().fill(.white.opacity(0.3)).overlay(alignment: .leading) {
                                Capsule().fill(.white).frame(width: g.size.width * 0.3 * progress)
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
        }
        .opacity(Double(progress))
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
                Text("可以同时连接多个，译梦会合并成一个曲库。")
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
                        Text("继续 · 已选 \(store.connectedCount) 个来源")
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
            if c.connected {
                HStack(spacing: 6) {
                    Circle().fill(Theme.lime).frame(width: 7, height: 7)
                    Text(c.kind.isLocal ? "已开启" : "已连接").font(.system(size: 13)).foregroundStyle(Theme.lime)
                }
                .onTapGesture {
                    if !c.kind.isLocal { editing = c.kind }
                }
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

    init(kind: SourceKind, onClose: @escaping () -> Void) {
        self.kind = kind
        self.onClose = onClose
        var c = SourceConfig(kind: kind)
        c.host = kind.hostPlaceholder
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
                    DispatchQueue.main.asyncAfter(deadline: .now() + 0.55) {
                        draft = store.connectDemo(draft)
                        busy = false
                    }
                } label: {
                    Text(busy ? "连接中…" : (draft.connected ? "保存并同步" : "连接并保存"))
                }
                .buttonStyle(PrimaryButtonStyle())
                .disabled(busy)
                .padding(.horizontal, 20)

                Text("凭据仅保存在本机（演示：UserDefaults）")
                    .font(.system(size: 13)).opacity(0.55)
                    .frame(maxWidth: .infinity).padding(.top, 12).padding(.bottom, 36)
            }
            .foregroundStyle(.white)
        }
        .preferredColorScheme(.dark)
        .onAppear {
            var c = store.config(for: kind)
            if c.host.isEmpty { c.host = kind.hostPlaceholder }
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
            field("服务器地址", text: $draft.host)
            if kind.usesUserPass {
                field("用户名", text: $draft.username)
            }
            HStack {
                field(kind.usesToken && !kind.usesUserPass ? "API Key / Token" : "密码 / API Key",
                      text: $draft.secret, secure: !showSecret)
                Button { showSecret.toggle() } label: {
                    Image(systemName: showSecret ? "eye" : "eye.slash").opacity(0.6)
                }.padding(.trailing, 8)
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

    private func field(_ label: String, text: Binding<String>, secure: Bool = false) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(label).font(.system(size: 13)).opacity(0.6)
            Group {
                if secure { SecureField("", text: text) } else { TextField("", text: text) }
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
        else if FirstRun.isDone { _phase = State(initialValue: .main) }
        else { _phase = State(initialValue: .launch) }
    }

    var body: some View {
        ZStack {
            switch phase {
            case .launch:
                LaunchView { withAnimation(.easeInOut(duration: 0.35)) { phase = .onboarding } }
            case .onboarding:
                OnboardingView(
                    onSkip: { withAnimation { phase = .sources } },
                    onDone: { withAnimation { phase = .sources } },
                    initialPage: Self.argInt("-page") ?? 0
                )
            case .sources:
                SourcesView(
                    initialConnect: Self.argSource("-source"),
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
