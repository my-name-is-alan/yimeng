import SwiftUI

struct NowPlayingView: View {
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @Environment(\.heroNS) var ns
    @State private var start = Date().addingTimeInterval(-13)
    let album = Mock.nightfall
    var body: some View {
        TimelineView(.animation) { tl in
            let t = tl.date.timeIntervalSince(start).truncatingRemainder(dividingBy: 36)
            ZStack {
                VStack(spacing: 0) {
                    header.motionIn(0.05, from: .top)
                    if state.lyricsMode { LyricsView(time: t).frame(maxHeight: .infinity).transition(.opacity) }
                    else { visualizer(time: tl.date.timeIntervalSinceReferenceDate).padding(.top, 8).motionIn(0.08, from: .scale); titleBlock(time: t).motionIn(0.16); Spacer(minLength: 0) }
                    progress(time: t).padding(.top, 16).motionIn(0.22)
                    transport.padding(.top, 20).motionIn(0.28)
                    bottomBar.padding(.top, 24).padding(.bottom, 8).motionIn(0.34)
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background {
                ZStack(alignment: .top) {
                    Theme.base(scheme)
                    DiffusedBackdrop(images: [album.cover], height: 900)
                    Color.black.opacity(scheme == .dark ? 0.25 : 0)
                }.ignoresSafeArea()
            }
        }
        .foregroundStyle(Theme.fg(scheme))
    }

    var header: some View {
        HStack {
            Button { withAnimation(.ui) { state.nowPlaying = false } } label: { CircleIconButton(icon: "chevron.down") }.buttonStyle(.plain)
            Spacer()
            if state.lyricsMode {
                HStack(spacing: 10) {
                    Image(album.cover).resizable().scaledToFill().frame(width: 32, height: 32).clipShape(Circle())
                    VStack(alignment: .leading, spacing: 0) { Text(album.title).font(.system(size: 13, weight: .semibold)); Text(album.artist).font(.system(size: 11)).opacity(0.6) }
                    Circle().fill(Theme.accent(scheme)).frame(width: 6, height: 6)
                }.padding(.leading, 6).padding(.trailing, 16).frame(height: 44).background(Capsule().fill(Theme.fill(scheme)))
            } else {
                Text("来自 · 每日推荐").font(.system(size: 13)).opacity(0.7)
            }
            Spacer()
            CircleIconButton(icon: "ellipsis")
        }.padding(.horizontal, Theme.margin).padding(.top, 4)
    }

    func visualizer(time: Double) -> some View {
        ZStack {
            ParticleField(time: time)
            heroCover(time: time)
                .shadow(color: Color(red: 0.91, green: 0.36, blue: 1).opacity(0.45), radius: 30)
        }.frame(width: 390, height: 390)
    }

    @ViewBuilder func heroCover(time: Double) -> some View {
        let img = Image(album.cover).resizable().scaledToFill().frame(width: 270, height: 270).clipShape(Circle())
            .rotationEffect(.degrees(time.truncatingRemainder(dividingBy: 30) * 12))
        if let ns { img.matchedGeometryEffect(id: "npcover", in: ns, properties: .frame, anchor: .center, isSource: true) } else { img }
    }

    func titleBlock(time: Double) -> some View {
        let idx = Mock.lyrics.lastIndex { $0.0 <= time } ?? 0
        return VStack(alignment: .leading, spacing: 8) {
            HStack {
                VStack(alignment: .leading, spacing: 2) { Text(album.title).font(.system(size: 20, weight: .bold)); Text(album.artist).font(.system(size: 17)).opacity(0.6) }
                Spacer()
                CircleIconButton(icon: "heart.fill")
            }
            Text("「 \(Mock.lyrics[idx].1) 」").font(.system(size: 15)).opacity(0.7).lineLimit(1)
        }.padding(.horizontal, Theme.margin)
    }

    func progress(time: Double) -> some View {
        let f = 0.25 + time / 36 * 0.5
        return VStack(spacing: 8) {
            GeometryReader { g in
                ZStack(alignment: .leading) {
                    Capsule().fill(Theme.fill(scheme).opacity(2)).frame(height: 5)
                    Capsule().fill(Theme.accent(scheme)).frame(width: g.size.width * f, height: 5)
                    Circle().fill(.white).frame(width: 14, height: 14).shadow(radius: 2).offset(x: g.size.width * f - 7)
                }
            }.frame(height: 14)
            HStack { Text("1:51"); Spacer(); Text("FLAC 24/96").padding(.horizontal, 8).padding(.vertical, 2).background(Capsule().fill(Theme.fill(scheme))); Spacer(); Text("-3:01") }
                .font(.system(size: 12)).opacity(0.6)
        }.padding(.horizontal, Theme.margin)
    }

    var transport: some View {
        HStack {
            Image(systemName: "shuffle").font(.system(size: 20)).opacity(0.7)
            Spacer(); Image(systemName: "backward.fill").font(.system(size: 30))
            Spacer()
            Image(systemName: "pause.fill").font(.system(size: 32)).foregroundStyle(scheme == .dark ? Color.black : .white)
                .frame(width: 76, height: 76).background(Circle().fill(scheme == .dark ? Color.white : Color(white: 0.07)))
            Spacer(); Image(systemName: "forward.fill").font(.system(size: 30))
            Spacer(); Image(systemName: "repeat").font(.system(size: 20)).opacity(0.7)
        }.padding(.horizontal, Theme.margin + 8)
    }

    var bottomBar: some View {
        HStack {
            Button { withAnimation(.smooth(duration: 0.45)) { state.lyricsMode.toggle() } } label: {
                Image(systemName: "quote.bubble").foregroundStyle(state.lyricsMode ? Theme.accent(scheme) : Theme.fg(scheme))
            }
            Spacer(); Image(systemName: "airpodspro"); Spacer(); Image(systemName: "list.bullet")
        }
        .font(.system(size: 20)).buttonStyle(.plain)
        .padding(.horizontal, 40).frame(height: 56).background(GlassCapsule())
        .padding(.horizontal, Theme.margin)
    }
}

/// Audio-reactive-style visualiser: three glowing frequency rings + ~240 orbiting particles with trails.
struct ParticleField: View {
    let time: Double
    static let seeds: [(a: Double, r: Double, v: Double, z: Double, h: Double)] = {
        var s: UInt64 = 7; func R() -> Double { s = s &* 6364136223846793005 &+ 1442695040888963407; return Double(s >> 33) / Double(1 << 31) }
        return (0..<240).map { _ in (R() * 6.283, 150 + R() * 45, 0.12 + R() * 0.35, 0.6 + R() * 2.2, R()) }
    }()
    static let bins: [Double] = { var s: UInt64 = 3; return (0..<96).map { _ in s = s &* 6364136223846793005 &+ 1; return Double(s >> 33) / Double(1 << 31) } }()
    var body: some View {
        Canvas { ctx, size in
            let c = CGPoint(x: size.width / 2, y: size.height / 2)
            ctx.blendMode = .plusLighter
            let ringColors = [Theme.lime.opacity(0.55), Color(red: 0.91, green: 0.36, blue: 1).opacity(0.45), Color(red: 0.48, green: 0.36, blue: 1).opacity(0.5)]
            for k in 0..<3 {
                var path = Path()
                for i in 0...96 {
                    let a = Double(i) / 96 * 2 * .pi
                    let e = Self.bins[i % 96] * 0.6 + 0.4 * abs(sin(time * 2.5 + Double(i) * 0.35 + Double(k)))
                    let r = 140 + Double(k) * 6 + e * (24 - Double(k) * 6)
                    let p = CGPoint(x: c.x + cos(a) * r, y: c.y + sin(a) * r)
                    i == 0 ? path.move(to: p) : path.addLine(to: p)
                }
                var g = ctx; g.addFilter(.shadow(color: ringColors[k], radius: 8))
                g.stroke(path, with: .color(ringColors[k]), lineWidth: 2.2 - Double(k) * 0.5)
            }
            for p in Self.seeds {
                let a = p.a + time * p.v
                let r = p.r + sin(time * 1.6 + p.a * 3) * 8
                let col: Color = p.h < 0.25 ? Theme.lime : (p.h < 0.6 ? Color(red: 0.91, green: 0.36, blue: 1) : Color(red: 0.6, green: 0.48, blue: 1))
                let pt = CGPoint(x: c.x + cos(a) * r, y: c.y + sin(a) * r)
                var trail = Path(); trail.move(to: pt)
                trail.addLine(to: CGPoint(x: c.x + cos(a - p.v * 0.25) * r, y: c.y + sin(a - p.v * 0.25) * r))
                ctx.stroke(trail, with: .color(col.opacity(0.25)), lineWidth: p.z * 0.8)
                ctx.fill(Path(ellipseIn: CGRect(x: pt.x - p.z, y: pt.y - p.z, width: p.z * 2, height: p.z * 2)), with: .color(col.opacity(0.85)))
            }
        }
    }
}

/// Synced lyrics: current line large with karaoke fill, neighbours dimmed + blurred by distance.
struct LyricsView: View {
    let time: Double
    @Environment(\.colorScheme) var scheme
    var body: some View {
        let lines = Mock.lyrics
        let idx = lines.lastIndex { $0.0 <= time } ?? 0
        let next = idx + 1 < lines.count ? lines[idx + 1].0 : lines[idx].0 + 4
        let frac = min(1, max(0, (time - lines[idx].0) / (next - lines[idx].0)))
        GeometryReader { g in
            VStack(alignment: .leading, spacing: 16) {
                ForEach(Array(lines.enumerated()), id: \.offset) { i, l in
                    let d = abs(i - idx)
                    Group {
                    if d == 0 {
                        Text(l.1).font(.system(size: 28, weight: .heavy))
                            .foregroundStyle(LinearGradient(stops: [.init(color: Theme.accent(scheme), location: frac), .init(color: Theme.fg(scheme), location: frac)], startPoint: .leading, endPoint: .trailing))
                            .shadow(color: Theme.accent(scheme).opacity(0.35), radius: 12)
                    } else {
                        Text(l.1).font(.system(size: d == 1 ? 24 : 22, weight: .bold))
                            .foregroundStyle(Theme.fg(scheme).opacity(d == 1 ? 0.5 : 0.28)).blur(radius: d == 1 ? 0.6 : 1.6)
                    }
                    }.blurIn(stagger(i, base: 0.1, step: 0.06))
                }
            }
            .padding(.horizontal, Theme.margin)
            .offset(y: g.size.height * 0.38 - CGFloat(idx) * 50)
            .animation(.spring(duration: 0.6), value: idx)
            .frame(width: g.size.width, height: g.size.height, alignment: .topLeading)
            .clipped()
            .mask(LinearGradient(stops: [.init(color: .clear, location: 0), .init(color: .black, location: 0.12), .init(color: .black, location: 0.8), .init(color: .clear, location: 1)], startPoint: .top, endPoint: .bottom))
        }
    }
}
