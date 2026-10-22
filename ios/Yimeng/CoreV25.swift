import SwiftUI
import UIKit

// MARK: - Palette from cover (server colors first, else averaged from the image) → readable paper/ink

struct PaperPalette: Equatable {
    var paper: Color; var accent: Color; var ink: Color; var glow: [Color]
    static let fallback = PaperPalette(paper: Color(hex: 0xf7f1e6), accent: Color(hex: 0x7a1cff), ink: Color(white: 0.1),
                                       glow: [Color(hex: 0xffd6ec), Color(hex: 0xd9ccff), Color(hex: 0xc9f2e0)])
    static func make(hex: [String]?, image: UIImage?) -> PaperPalette {
        var ui: [UIColor] = (hex ?? []).compactMap { h in
            UInt32(h.trimmingCharacters(in: CharacterSet(charactersIn: "#")), radix: 16).map {
                UIColor(red: CGFloat(($0 >> 16) & 0xff) / 255, green: CGFloat(($0 >> 8) & 0xff) / 255, blue: CGFloat($0 & 0xff) / 255, alpha: 1) }
        }
        if ui.isEmpty, let img = image, let a = average(img) { ui = [a] }
        guard let base = ui.first else { return fallback }
        func mix(_ c: UIColor, _ w: CGFloat) -> UIColor {   // toward white by w
            var r: CGFloat = 0, g: CGFloat = 0, b: CGFloat = 0, a: CGFloat = 0; c.getRed(&r, green: &g, blue: &b, alpha: &a)
            return UIColor(red: r + (1 - r) * w, green: g + (1 - g) * w, blue: b + (1 - b) * w, alpha: 1)
        }
        func deep(_ c: UIColor) -> UIColor {   // dark, saturated ink-ish accent (contrast on light paper)
            var h: CGFloat = 0, s: CGFloat = 0, v: CGFloat = 0, a: CGFloat = 0; c.getHue(&h, saturation: &s, brightness: &v, alpha: &a)
            return UIColor(hue: h, saturation: max(s, 0.55), brightness: min(v, 0.55), alpha: 1)
        }
        let second = ui.count > 1 ? ui[1] : base, third = ui.count > 2 ? ui[2] : second
        return PaperPalette(paper: Color(mix(base, 0.9)), accent: Color(deep(base)), ink: Color(white: 0.1),
                            glow: [Color(mix(base, 0.35)), Color(mix(second, 0.4)), Color(mix(third, 0.45))])
    }
    static func average(_ img: UIImage) -> UIColor? {
        let f = UIGraphicsImageRendererFormat(); f.scale = 1
        let px = UIGraphicsImageRenderer(size: CGSize(width: 1, height: 1), format: f).image { _ in img.draw(in: CGRect(x: 0, y: 0, width: 1, height: 1)) }
        guard let cg = px.cgImage, let d = cg.dataProvider?.data, let p = CFDataGetBytePtr(d) else { return nil }
        return UIColor(red: CGFloat(p[0]) / 255, green: CGFloat(p[1]) / 255, blue: CGFloat(p[2]) / 255, alpha: 1)
    }
}

// MARK: - Generic list page (每日推荐 / 推荐歌单 / 排行 / 平台歌单): header, 播放全部, tracks. Only explicit taps play.

struct TrackListSpec: Hashable {
    enum Kind: Hashable { case daily, playlist(String) }
    let kind: Kind
    let title: String
    var subtitle: String = ""
    var origin: String? = nil
    var cover: URL? = nil
    var songs: [NDSong] = []
}

struct TrackListPage: View {
    let spec: TrackListSpec
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @State private var songs: [NDSong] = []
    @State private var loading = true
    @State private var failed = false
    var body: some View {
        ScrollView(showsIndicators: false) {
            VStack(alignment: .leading, spacing: 0) {
                HStack {
                    Button { if !state.path.isEmpty { state.path.removeLast() } } label: { CircleIconButton(icon: "chevron.left") }
                    Spacer()
                }.padding(.horizontal, Theme.margin).padding(.top, 8)
                HStack(alignment: .bottom, spacing: 16) {
                    ZStack {
                        if spec.kind == .daily { DateTile(size: 132) }
                        else { RemoteCover(url: spec.cover, size: 132, corner: 14) }
                        WobblyFrame(seed: 11, inset: -5).stroke(Theme.fg(scheme).opacity(0.55), lineWidth: 1.4).frame(width: 132, height: 132)
                    }.rotationEffect(.degrees(-2))
                    VStack(alignment: .leading, spacing: 6) {
                        Text(spec.title).font(.system(size: 22, weight: .bold)).lineLimit(2)
                        HStack(spacing: 6) {
                            Text(spec.subtitle.isEmpty ? "\(songs.count) 首" : spec.subtitle).font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme)).lineLimit(1)
                            SourceBadge(origin: spec.origin)
                        }
                        Button { play(0) } label: {
                            Label("播放全部", systemImage: "play.fill").font(.system(size: 14, weight: .semibold))
                                .padding(.horizontal, 16).frame(height: 36)
                                .foregroundStyle(Theme.base(scheme)).background(Capsule().fill(Theme.fg(scheme)))
                        }.buttonStyle(.plain).disabled(songs.isEmpty).padding(.top, 6)
                    }
                    Spacer(minLength: 0)
                }.padding(.horizontal, Theme.margin).padding(.top, 14).padding(.bottom, 18)
                if loading && songs.isEmpty { DoodleLoader().frame(maxWidth: .infinity).padding(.top, 30) }
                else if songs.isEmpty { EmptyState(kind: failed ? .network : .songs).padding(.top, 20) }
                LazyVStack(spacing: 0) {
                    ForEach(songs.indices, id: \.self) { i in SongRowPlay(songs: songs, i: i) }
                }
                Color.clear.frame(height: 180)
            }
        }
        .background(alignment: .top) {
            RemoteCover(url: spec.cover, size: 420, corner: 0).blur(radius: 50).opacity(spec.cover == nil ? 0 : 0.35)
                .frame(height: 340).clipped().ignoresSafeArea()
                .mask(LinearGradient(colors: [.black, .clear], startPoint: .top, endPoint: .bottom))
        }
        .background(Theme.base(scheme))
        .task {
            if !spec.songs.isEmpty { songs = spec.songs; loading = false; return }
            guard case .playlist(let id) = spec.kind else { loading = false; return }
            do { songs = try await CoreClient.shared.playlistTracks(id) } catch { failed = true }
            loading = false
        }
    }
    private func play(_ i: Int) {
        let ok = songs.filter { !$0.isUnplayable }
        guard !ok.isEmpty else { return }
        PlayerStore.shared.play(songs: ok, startAt: i, coverURL: spec.cover)
    }
}

/// Date doodle tile used by 每日推荐 (card + list header).
struct DateTile: View {
    var size: CGFloat = 92
    var body: some View {
        let day = Calendar.current.component(.day, from: Date())
        ZStack {
            RoundedRectangle(cornerRadius: size * 0.17, style: .continuous).fill(Color(hex: 0x7a1cff).opacity(0.88))
            WobblyFrame(seed: UInt64(day)).stroke(.white.opacity(0.8), lineWidth: 1.6)
            Text("\(day)").font(.system(size: size * 0.44, weight: .black, design: .rounded)).foregroundStyle(.white)
        }.frame(width: size, height: size)
    }
}

// MARK: - 今日乐签 page (pushed; no popup)

struct YueqianPage: View {
    @ObservedObject private var store = YueqianStore.shared
    @EnvironmentObject var state: AppState
    @State private var rendered: UIImage?
    var body: some View {
        let pal = store.palette
        ZStack(alignment: .top) {
            // Light, colourful backdrop: the cover enlarged + blurred, washed with the palette.
            ZStack {
                LinearGradient(colors: pal.glow, startPoint: .topLeading, endPoint: .bottomTrailing)
                if let c = store.cover { Image(uiImage: c).resizable().scaledToFill().scaleEffect(1.6).blur(radius: 60).opacity(0.75) }
                Color.white.opacity(0.28)
            }.ignoresSafeArea()
            ScrollView(showsIndicators: false) {
                VStack(spacing: 22) {
                    if let c = store.card {
                        YueqianCardView(card: c, cover: store.cover, palette: pal).shadow(color: .black.opacity(0.12), radius: 24, y: 10)
                        HStack(spacing: 14) {
                            Button { save() } label: { pill("保存到相册", "square.and.arrow.down", pal) }.buttonStyle(.plain)
                            if let img = rendered {
                                ShareLink(item: Image(uiImage: img), preview: SharePreview("今日乐签", image: Image(uiImage: img))) { pill("分享", "square.and.arrow.up", pal) }
                            } else { pill("分享", "square.and.arrow.up", pal).opacity(0.5) }
                        }
                    } else { DoodleLoader().padding(.top, 160) }
                }.frame(maxWidth: .infinity).padding(.top, 64).padding(.bottom, 160)
            }
            HStack {
                Button { if !state.path.isEmpty { state.path.removeLast() } } label: { CircleIconButton(icon: "chevron.left") }
                Spacer()
                Text("今日乐签").font(.system(size: 16, weight: .semibold)).foregroundStyle(pal.ink)
                Spacer(); Color.clear.frame(width: 44, height: 44)
            }.padding(.horizontal, Theme.margin).padding(.top, 6)
        }
        .environment(\.colorScheme, .light)
        .task {
            store.markOpened()
            if store.card == nil { await store.load() }
            render()
        }
        .onChange(of: store.cover) { _, _ in render() }
    }
    private func pill(_ t: String, _ ic: String, _ p: PaperPalette) -> some View {
        Label(t, systemImage: ic).font(.system(size: 15, weight: .semibold)).foregroundStyle(p.ink)
            .padding(.horizontal, 22).frame(height: 46)
            .background(Capsule().fill(p.paper)).overlay(Capsule().stroke(p.ink.opacity(0.15), lineWidth: 1))
    }
    private func render() {
        guard let c = store.card else { return }
        let pal = store.palette
        let v = YueqianCardView(card: c, cover: store.cover, palette: pal, interactive: false)
            .padding(28).background(LinearGradient(colors: pal.glow, startPoint: .topLeading, endPoint: .bottomTrailing))
            .environment(\.colorScheme, .light)
        let r = ImageRenderer(content: v); r.scale = 3; rendered = r.uiImage
    }
    private func save() {
        render(); guard let img = rendered else { return }
        YueqianStore.saveToPhotos(img)
    }
}

/// Home entry: a mini ticket (date stub | perforation | quote + cover).
struct YueqianHomeTicket: View {
    @ObservedObject private var store = YueqianStore.shared
    @EnvironmentObject var state: AppState
    var body: some View {
        if let c = store.card {
            let pal = store.palette
            let cal = Calendar.current
            Button { state.path.append(.yueqian) } label: {
                HStack(spacing: 0) {
                    VStack(spacing: 0) {
                        Text("\(cal.component(.month, from: c.date))月").font(.system(size: 12, weight: .semibold))
                        Text(String(format: "%02d", cal.component(.day, from: c.date))).font(.system(size: 36, weight: .black, design: .rounded))
                        Text(YueqianCardView.weekday(c.date)).font(.system(size: 10)).opacity(0.6)
                    }.frame(width: 78).foregroundStyle(pal.accent)
                    TicketPerforation().stroke(pal.ink.opacity(0.3), style: StrokeStyle(lineWidth: 1, dash: [3, 4])).frame(width: 14)
                    VStack(alignment: .leading, spacing: 4) {
                        Text("今日乐签").font(.system(size: 11, weight: .bold)).foregroundStyle(pal.accent)
                        Text("「\(c.quote)」").font(.system(size: 15, weight: .semibold)).lineLimit(2).foregroundStyle(pal.ink)
                        Text("\(c.song.title) — \(c.song.artist)").font(.system(size: 12)).lineLimit(1).truncationMode(.middle).foregroundStyle(pal.ink.opacity(0.6))
                    }.padding(.leading, 10)
                    Spacer(minLength: 8)
                    ZStack {
                        RemoteCover(url: c.coverURL, size: 62, corner: 8)
                        WobblyFrame(seed: 5, inset: -3).stroke(pal.ink.opacity(0.55), lineWidth: 1.2).frame(width: 62, height: 62)
                    }.rotationEffect(.degrees(4)).padding(.trailing, 14)
                }
                .padding(.vertical, 14)
                .background(TicketShape().fill(pal.paper).shadow(color: .black.opacity(0.08), radius: 10, y: 4))
            }.buttonStyle(.plain)
        }
    }
}

/// Rounded rect with two notches where the perforation sits (x = 85).
struct TicketShape: Shape {
    var notchX: CGFloat = 85
    func path(in r: CGRect) -> Path {
        var p = Path(roundedRect: r, cornerRadius: 16, style: .continuous)
        p = p.subtracting(Path(ellipseIn: CGRect(x: notchX - 8, y: r.minY - 8, width: 16, height: 16)))
        p = p.subtracting(Path(ellipseIn: CGRect(x: notchX - 8, y: r.maxY - 8, width: 16, height: 16)))
        return p
    }
}
struct TicketPerforation: Shape {
    func path(in r: CGRect) -> Path { var p = Path(); p.move(to: CGPoint(x: r.midX, y: r.minY + 6)); p.addLine(to: CGPoint(x: r.midX, y: r.maxY - 6)); return p }
}

// MARK: - 排行 doodle tabs, glyphs, rank badges, collage

struct ScribbleUnderline: Shape {
    var seed: UInt64 = 3
    func path(in r: CGRect) -> Path {
        var rng = SeededRNG(seed); var p = Path()
        let y = r.midY
        p.move(to: CGPoint(x: r.minX, y: y + 1))
        let n = 6
        for i in 1...n {
            let x = r.minX + r.width * CGFloat(i) / CGFloat(n)
            p.addQuadCurve(to: CGPoint(x: x, y: y + CGFloat(rng.d(-1.5, 1.5))), control: CGPoint(x: x - r.width / CGFloat(2 * n), y: y + (i % 2 == 0 ? -3 : 3)))
        }
        // return stroke, slightly lower: highlighter double pass
        p.addQuadCurve(to: CGPoint(x: r.minX + 4, y: y + 4), control: CGPoint(x: r.midX, y: y + 7))
        return p
    }
}
struct ScribbleCircle: Shape {
    var seed: UInt64 = 1
    func path(in r: CGRect) -> Path {
        var rng = SeededRNG(seed); var p = Path()
        let c = CGPoint(x: r.midX, y: r.midY)
        for k in 0..<46 {
            let a = Double(k) / 40 * 2 * .pi - .pi / 2
            let rx = r.width / 2 + CGFloat(rng.d(-1.2, 1.2)), ry = r.height / 2 + CGFloat(rng.d(-1.2, 1.2))
            let pt = CGPoint(x: c.x + rx * CGFloat(cos(a)), y: c.y + ry * CGFloat(sin(a)))
            k == 0 ? p.move(to: pt) : p.addLine(to: pt)
        }
        return p
    }
}
struct StarScribble: Shape {
    func path(in r: CGRect) -> Path {
        var p = Path(); let c = CGPoint(x: r.midX, y: r.midY), R = min(r.width, r.height) / 2
        for i in 0...5 {
            let a = Double(i * 144) * .pi / 180 - .pi / 2
            let pt = CGPoint(x: c.x + R * CGFloat(cos(a)), y: c.y + R * CGFloat(sin(a)))
            i == 0 ? p.move(to: pt) : p.addLine(to: pt)
        }
        return p
    }
}
struct NoteScribble: Shape {
    func path(in r: CGRect) -> Path {
        var p = Path(); let w = r.width, h = r.height
        p.addEllipse(in: CGRect(x: r.minX, y: r.minY + h * 0.62, width: w * 0.42, height: h * 0.34))
        p.move(to: CGPoint(x: r.minX + w * 0.42, y: r.minY + h * 0.78)); p.addLine(to: CGPoint(x: r.minX + w * 0.42, y: r.minY + h * 0.05))
        p.addQuadCurve(to: CGPoint(x: r.maxX, y: r.minY + h * 0.35), control: CGPoint(x: r.minX + w * 0.95, y: r.minY + h * 0.05))
        return p
    }
}
struct CrownScribble: Shape {
    func path(in r: CGRect) -> Path {
        var p = Path(); let w = r.width, h = r.height
        p.move(to: CGPoint(x: r.minX, y: r.maxY)); p.addLine(to: CGPoint(x: r.minX, y: r.minY + h * 0.25))
        p.addLine(to: CGPoint(x: r.minX + w * 0.3, y: r.minY + h * 0.6)); p.addLine(to: CGPoint(x: r.midX, y: r.minY))
        p.addLine(to: CGPoint(x: r.minX + w * 0.7, y: r.minY + h * 0.6)); p.addLine(to: CGPoint(x: r.maxX, y: r.minY + h * 0.25))
        p.addLine(to: CGPoint(x: r.maxX, y: r.maxY)); p.closeSubpath()
        return p
    }
}

struct PlatformGlyph: View {
    let origin: String; var color: Color
    var body: some View {
        Group {
            switch origin {
            case "netease": NoteScribble().stroke(color, style: StrokeStyle(lineWidth: 1.8, lineCap: .round, lineJoin: .round))
            case "qq": StarScribble().stroke(color, style: StrokeStyle(lineWidth: 1.6, lineCap: .round, lineJoin: .round))
            case "kugou": WaveShape().stroke(color, style: StrokeStyle(lineWidth: 1.8, lineCap: .round))
            default: ScribbleCircle(seed: 2).stroke(color, lineWidth: 1.6)
            }
        }.frame(width: 15, height: 15)
    }
}

struct DoodleTabs: View {
    let items: [String]
    @Binding var selected: String
    @Environment(\.colorScheme) var scheme
    @State private var drawn: CGFloat = 1
    var body: some View {
        HStack(spacing: 26) {
            ForEach(items, id: \.self) { o in
                let on = o == selected
                Button { guard !on else { return }; selected = o } label: {
                    VStack(spacing: 2) {
                        HStack(spacing: 6) {
                            PlatformGlyph(origin: o, color: on ? Theme.fg(scheme) : Theme.secondary(scheme))
                            Text(CoreOrigin.label(o)).font(.system(size: 18, weight: on ? .bold : .medium))
                                .foregroundStyle(on ? Theme.fg(scheme) : Theme.secondary(scheme))
                        }
                        .padding(.horizontal, 6).padding(.vertical, 4)
                        .background {
                            if on {
                                ScribbleCircle(seed: UInt64(o.count)).trim(from: 0, to: drawn)
                                    .stroke(Theme.fg(scheme).opacity(0.55), style: StrokeStyle(lineWidth: 1.3, lineCap: .round))
                                    .padding(-5)
                            }
                        }
                        ScribbleUnderline(seed: UInt64(o.count + 7)).trim(from: 0, to: on ? drawn : 0)
                            .stroke(Theme.lime.opacity(0.85), style: StrokeStyle(lineWidth: 5, lineCap: .round, lineJoin: .round))
                            .frame(height: 10).padding(.horizontal, 4)
                    }
                }.buttonStyle(.plain)
            }
            Spacer(minLength: 0)
        }
        .onChange(of: selected) { _, _ in drawn = 0; withAnimation(.easeOut(duration: 0.55)) { drawn = 1 } }
    }
}

struct RankBadge: View {
    let rank: Int
    var body: some View {
        let colors = [Color(hex: 0xf2b705), Color(hex: 0x9aa4b2), Color(hex: 0xc7773a)]
        let c = colors[min(rank - 1, 2)]
        ZStack {
            ScribbleCircle(seed: UInt64(rank * 3)).fill(c.opacity(0.18))
            ScribbleCircle(seed: UInt64(rank * 3)).stroke(c, style: StrokeStyle(lineWidth: 1.4, lineCap: .round))
            Text("\(rank)").font(.system(size: 11, weight: .heavy, design: .rounded)).foregroundStyle(c)
            if rank == 1 { CrownScribble().stroke(c, style: StrokeStyle(lineWidth: 1.3, lineJoin: .round)).frame(width: 12, height: 8).offset(y: -12) }
        }.frame(width: 20, height: 20)
    }
}

struct ChartCollage: View {
    let urls: [URL?]
    var body: some View {
        ZStack {
            ForEach(Array(urls.prefix(3).enumerated().reversed()), id: \.offset) { i, u in
                RemoteCover(url: u, size: 70, corner: 9)
                    .overlay(RoundedRectangle(cornerRadius: 9).stroke(.white, lineWidth: 2))
                    .rotationEffect(.degrees([0, 7, -8][i]))
                    .offset(x: [CGFloat(-10), 14, -2][i], y: [CGFloat(-10), 0, 14][i])
                    .shadow(color: .black.opacity(0.12), radius: 4, y: 2)
            }
        }.frame(width: 104, height: 104)
    }
}
