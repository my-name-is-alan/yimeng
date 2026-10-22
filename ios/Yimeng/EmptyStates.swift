import SwiftUI

/// Crayon-style stroke: draws a path twice with a slight offset + jitter for a hand-drawn feel.
struct Crayon<S: Shape>: View {
    let shape: S
    var color: Color = .white
    var width: CGFloat = 2.4
    var body: some View {
        ZStack {
            shape.stroke(color.opacity(0.9), style: StrokeStyle(lineWidth: width, lineCap: .round, lineJoin: .round))
            shape.stroke(color.opacity(0.35), style: StrokeStyle(lineWidth: width * 0.7, lineCap: .round, lineJoin: .round))
                .offset(x: 0.8, y: -0.6).rotationEffect(.degrees(0.8))
        }
    }
}

struct MoonShape: Shape {
    func path(in r: CGRect) -> Path {
        var p = Path()
        let c = CGPoint(x: r.midX, y: r.midY), R = min(r.width, r.height) / 2
        p.addArc(center: c, radius: R, startAngle: .degrees(-60), endAngle: .degrees(200), clockwise: false)
        p.addQuadCurve(to: CGPoint(x: c.x + R * cos(.pi * -60 / 180), y: c.y + R * sin(.pi * -60 / 180)),
                       control: CGPoint(x: c.x + R * 0.05, y: c.y + R * 0.1))
        return p
    }
}
struct StarShape: Shape {
    func path(in r: CGRect) -> Path {
        var p = Path(); let c = CGPoint(x: r.midX, y: r.midY), R = min(r.width, r.height) / 2
        p.move(to: CGPoint(x: c.x, y: c.y - R)); p.addQuadCurve(to: CGPoint(x: c.x + R, y: c.y), control: c)
        p.addQuadCurve(to: CGPoint(x: c.x, y: c.y + R), control: c); p.addQuadCurve(to: CGPoint(x: c.x - R, y: c.y), control: c)
        p.addQuadCurve(to: CGPoint(x: c.x, y: c.y - R), control: c)
        return p
    }
}
struct RecordShape: Shape {
    func path(in r: CGRect) -> Path {
        var p = Path(); let c = CGPoint(x: r.midX, y: r.midY), R = min(r.width, r.height) / 2
        p.addEllipse(in: CGRect(x: c.x - R, y: c.y - R, width: R * 2, height: R * 2))
        p.addEllipse(in: CGRect(x: c.x - R * 0.62, y: c.y - R * 0.62, width: R * 1.24, height: R * 1.24))
        p.addEllipse(in: CGRect(x: c.x - R * 0.16, y: c.y - R * 0.16, width: R * 0.32, height: R * 0.32))
        return p
    }
}
struct WaveShape: Shape {
    func path(in r: CGRect) -> Path {
        var p = Path(); p.move(to: CGPoint(x: r.minX, y: r.midY)); var x = r.minX; var up = true
        while x < r.maxX { let nx = min(r.maxX, x + r.width / 6); p.addQuadCurve(to: CGPoint(x: nx, y: r.midY), control: CGPoint(x: (x + nx) / 2, y: up ? r.minY : r.maxY)); x = nx; up.toggle() }
        return p
    }
}
struct HeartShape: Shape {
    func path(in r: CGRect) -> Path {
        var p = Path(); let w = r.width, h = r.height
        p.move(to: CGPoint(x: r.midX, y: r.maxY))
        p.addCurve(to: CGPoint(x: r.minX, y: r.minY + h * 0.3), control1: CGPoint(x: r.minX + w * 0.1, y: r.minY + h * 0.75), control2: CGPoint(x: r.minX, y: r.minY + h * 0.5))
        p.addArc(center: CGPoint(x: r.minX + w * 0.25, y: r.minY + h * 0.3), radius: w * 0.25, startAngle: .degrees(180), endAngle: .degrees(0), clockwise: false)
        p.addArc(center: CGPoint(x: r.minX + w * 0.75, y: r.minY + h * 0.3), radius: w * 0.25, startAngle: .degrees(180), endAngle: .degrees(0), clockwise: false)
        p.addCurve(to: CGPoint(x: r.midX, y: r.maxY), control1: CGPoint(x: r.maxX, y: r.minY + h * 0.5), control2: CGPoint(x: r.maxX - w * 0.1, y: r.minY + h * 0.75))
        return p
    }
}

/// Doodle empty-state illustration + line + optional quiet action.
struct EmptyState: View {
    enum Kind { case songs, albums, artists, playlists, starred, search, collection, network }
    let kind: Kind
    var action: (title: String, run: () -> Void)? = nil
    @Environment(\.colorScheme) var scheme
    @State private var float = false

    var copy: (String, String) {
        switch kind {
        case .songs: ("还没有歌曲", "曲库里空空的，去音源设置看看服务器有没有扫描完成。")
        case .albums: ("这里还没有专辑", "换个排序方式试试，或者等服务器扫描完成。")
        case .artists: ("还没有歌手", "歌手会随着专辑一起出现在这里。")
        case .playlists: ("还没有歌单", "在 Navidrome 里建一个歌单，它就会飘到这里。")
        case .starred: ("还没有收藏", "听到喜欢的歌，点一下小心心。")
        case .search: ("没有找到", "换个关键词，或者只搜歌手的名字试试。")
        case .collection: ("典藏还是空的", "在专辑页或歌曲旁点亮 ♡，它们会被收进这座梦里的唱片架。")
        case .network: ("连接不上服务器", "检查一下网络或音源设置，再试一次。")
        }
    }

    var body: some View {
        let ink = Theme.fg(scheme)
        VStack(spacing: 14) {
            ZStack {
                switch kind {
                case .starred, .collection:
                    Crayon(shape: HeartShape(), color: Color(hex: 0xff5fa8)).frame(width: 64, height: 56).offset(x: 6, y: 8)
                    Crayon(shape: MoonShape(), color: ink).frame(width: 44, height: 44).offset(x: -40, y: -26)
                case .network:
                    Crayon(shape: MoonShape(), color: ink).frame(width: 60, height: 60)
                    Crayon(shape: WaveShape(), color: ink.opacity(0.6)).frame(width: 90, height: 12).offset(y: 46).rotationEffect(.degrees(-6))
                case .search:
                    Crayon(shape: Circle(), color: ink).frame(width: 52, height: 52).offset(x: -8, y: -6)
                    Crayon(shape: Path { p in p.move(to: CGPoint(x: 0, y: 0)); p.addLine(to: CGPoint(x: 20, y: 20)) }, color: ink).frame(width: 20, height: 20).offset(x: 26, y: 26)
                    Crayon(shape: MoonShape(), color: ink.opacity(0.7)).frame(width: 22, height: 22).offset(x: -8, y: -6)
                default:
                    Crayon(shape: RecordShape(), color: ink).frame(width: 76, height: 76).rotationEffect(.degrees(float ? 8 : -4))
                    Crayon(shape: MoonShape(), color: ink.opacity(0.85)).frame(width: 30, height: 30).offset(x: 46, y: -40)
                }
                Crayon(shape: StarShape(), color: Theme.accent(scheme), width: 1.8).frame(width: 14, height: 14).offset(x: 52, y: 30).opacity(float ? 1 : 0.4)
                Crayon(shape: StarShape(), color: ink.opacity(0.6), width: 1.6).frame(width: 9, height: 9).offset(x: -54, y: 28)
            }
            .frame(width: 150, height: 120)
            .offset(y: float ? -3 : 3)
            Text(copy.0).font(.system(size: 17, weight: .semibold)).foregroundStyle(ink)
            Text(copy.1).font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme)).multilineTextAlignment(.center).frame(maxWidth: 260)
            if let action {
                Button(action.title, action: action.run).buttonStyle(SecondaryButtonStyle()).frame(width: 160).padding(.top, 4)
            }
        }
        .frame(maxWidth: .infinity).padding(.vertical, 36)
        .onAppear { withAnimation(.easeInOut(duration: 2.4).repeatForever(autoreverses: true)) { float = true } }
    }
}

#if os(iOS)
/// Artist avatar: Navidrome artist image (UA cache) over a doodle monogram tinted by the artist id.
struct ArtistAvatar: View {
    let artist: NDArtist
    var size: CGFloat = 56
    @ObservedObject private var library = LibraryStore.shared
    static let tints: [Color] = [Color(hex: 0x7a1cff), Color(hex: 0xff5fa8), Color(hex: 0x33d17a), Color(hex: 0xff9a5a), Color(hex: 0x3b82f6), Color(hex: 0x14b8a6)]
    var body: some View {
        let tint = Self.tints[artist.id.unicodeScalars.reduce(0) { $0 + Int($1.value) } % Self.tints.count]
        ZStack {
            Circle().fill(LinearGradient(colors: [tint.opacity(0.85), tint.opacity(0.35)], startPoint: .topLeading, endPoint: .bottomTrailing))
            Text(String(artist.name.prefix(1)).uppercased()).font(.system(size: size * 0.38, weight: .heavy)).foregroundStyle(.white.opacity(0.9))
            Crayon(shape: Circle(), color: .white.opacity(0.5), width: 1.2).padding(3)
            ArtistImage(url: library.coverArtURL(id: artist.coverId ?? "ar-\(artist.id)", size: Int(size * 3)))
        }
        .frame(width: size, height: size).clipShape(Circle())
    }
}

/// Like CoverImage, but transparent until loaded and tolerant of Navidrome's placeholder art.
private struct ArtistImage: View {
    let url: URL?
    @State private var image: UIImage?
    init(url: URL?) { self.url = url; _image = State(initialValue: url.flatMap { CoverCache.shared.memory($0) }) }
    var body: some View {
        ZStack { if let image { Image(uiImage: image).resizable().scaledToFill().transition(.opacity) } }
            .task(id: url.map(CoverCache.key)) {
                guard image == nil, let url else { return }
                if let d = await CoverCache.shared.disk(url) { image = d; return }
                guard url.scheme != CoverCache.offlineScheme, let img = try? await CoverCache.shared.fetch(url) else { return }
                withAnimation(.easeOut(duration: 0.25)) { image = img }
            }
    }
}
#endif
