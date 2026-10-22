import WidgetKit
import SwiftUI
import AppIntents

private let lime = Color(red: 0.784, green: 0.961, blue: 0.259)
private let ink = Color(red: 0.07, green: 0.06, blue: 0.09)

// MARK: - Timeline

struct YMEntry: TimelineEntry {
    let date: Date
    var now: YMShared.NowPlaying?
    var card: YMShared.Card?
    var fm: YMShared.FM?
}

struct YMProvider: TimelineProvider {
    func placeholder(in c: Context) -> YMEntry {
        YMEntry(date: .now, now: .init(id: "p", title: "出现又离开", artist: "梁博", playing: true, cover: nil, updated: .now),
                card: .init(day: "10-22", quote: "谁也没有想过再想回来", title: "出现又离开", artist: "梁博", cover: nil), fm: .init(origins: ["netease"]))
    }
    func getSnapshot(in c: Context, completion: @escaping (YMEntry) -> Void) { completion(YMShared.available ? current() : placeholder(in: c)) }
    func getTimeline(in c: Context, completion: @escaping (Timeline<YMEntry>) -> Void) {
        completion(Timeline(entries: [current()], policy: .after(.now.addingTimeInterval(30 * 60))))
    }
    private func current() -> YMEntry {
        YMEntry(date: .now, now: YMShared.read(YMShared.NowPlaying.self, YMShared.kNow),
                card: YMShared.read(YMShared.Card.self, YMShared.kCard), fm: YMShared.read(YMShared.FM.self, YMShared.kFM))
    }
}

// MARK: - Pieces

private struct Cover: View {
    let data: Data?; var size: CGFloat; var round = false
    var body: some View {
        Group {
            if let data, let ui = UIImage(data: data) { Image(uiImage: ui).resizable().scaledToFill() }
            else { ZStack { LinearGradient(colors: [Color(red: 0.48, green: 0.11, blue: 1), Color(red: 1, green: 0.37, blue: 0.66)], startPoint: .topLeading, endPoint: .bottomTrailing); Image(systemName: "music.note").font(.system(size: size * 0.4, weight: .bold)).foregroundStyle(.white.opacity(0.9)) } }
        }
        .frame(width: size, height: size)
        .clipShape(RoundedRectangle(cornerRadius: round ? size / 2 : size * 0.18, style: .continuous))
    }
}

/// Hand-drawn wobble underline, the 梦中的涂鸦 signature.
private struct Squiggle: Shape {
    func path(in r: CGRect) -> Path {
        var p = Path(); p.move(to: CGPoint(x: 0, y: r.midY))
        let n = 6
        for i in 1...n { let x = r.width * CGFloat(i) / CGFloat(n); p.addQuadCurve(to: CGPoint(x: x, y: r.midY), control: CGPoint(x: x - r.width / CGFloat(n * 2), y: i % 2 == 0 ? r.minY : r.maxY)) }
        return p
    }
}

private struct Placeholder: View {
    let text: String
    var body: some View {
        VStack(spacing: 6) {
            Image(systemName: "moon.stars").font(.system(size: 22, weight: .semibold)).foregroundStyle(lime)
            Text(text).font(.system(size: 13, weight: .semibold)).multilineTextAlignment(.center)
        }.frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

// MARK: - 正在播放 (interactive play/pause)

struct NowPlayingWidgetView: View {
    @Environment(\.widgetFamily) var family
    let e: YMEntry
    var body: some View {
        if let n = e.now {
            switch family {
            case .accessoryCircular:
                Button(intent: TogglePlaybackIntent()) { ZStack { AccessoryWidgetBackground(); Image(systemName: n.playing ? "pause.fill" : "play.fill").font(.system(size: 20, weight: .bold)) } }.buttonStyle(.plain)
            case .accessoryInline:
                Label("\(n.title) · \(n.artist)", systemImage: n.playing ? "waveform" : "pause")
            case .accessoryRectangular:
                HStack(spacing: 6) {
                    VStack(alignment: .leading, spacing: 1) {
                        Text(n.title).font(.system(size: 14, weight: .semibold)).lineLimit(1)
                        Text(n.artist).font(.system(size: 12)).lineLimit(1).opacity(0.8)
                    }
                    Spacer(minLength: 0)
                    Button(intent: TogglePlaybackIntent()) { Image(systemName: n.playing ? "pause.fill" : "play.fill").font(.system(size: 16, weight: .bold)) }.buttonStyle(.plain)
                }
            case .systemMedium:
                HStack(spacing: 14) {
                    Cover(data: n.cover, size: 112)
                    VStack(alignment: .leading, spacing: 4) {
                        Text("正在播放").font(.system(size: 11, weight: .semibold)).foregroundStyle(lime)
                        Text(n.title).font(.system(size: 17, weight: .bold)).lineLimit(2)
                        Text(n.artist).font(.system(size: 13)).opacity(0.7).lineLimit(1)
                        Spacer(minLength: 0)
                        HStack(spacing: 18) {
                            Button(intent: TogglePlaybackIntent()) { Image(systemName: n.playing ? "pause.fill" : "play.fill").font(.system(size: 17, weight: .bold)).foregroundStyle(ink).frame(width: 40, height: 40).background(Circle().fill(.white)) }.buttonStyle(.plain)
                            Button(intent: NextTrackIntent()) { Image(systemName: "forward.fill").font(.system(size: 17)) }.buttonStyle(.plain)
                        }
                    }
                    Spacer(minLength: 0)
                }.foregroundStyle(.white)
            default:
                VStack(alignment: .leading, spacing: 6) {
                    HStack(alignment: .top) {
                        Cover(data: n.cover, size: 56)
                        Spacer()
                        Button(intent: TogglePlaybackIntent()) { Image(systemName: n.playing ? "pause.fill" : "play.fill").font(.system(size: 15, weight: .bold)).foregroundStyle(ink).frame(width: 34, height: 34).background(Circle().fill(.white)) }.buttonStyle(.plain)
                    }
                    Spacer(minLength: 0)
                    Text(n.title).font(.system(size: 15, weight: .bold)).lineLimit(1)
                    Text(n.artist).font(.system(size: 12)).opacity(0.7).lineLimit(1)
                }.foregroundStyle(.white)
            }
        } else {
            Placeholder(text: YMShared.available ? "还没有在播放" : "打开译梦").foregroundStyle(family.isAccessory ? .primary : Color.white)
        }
    }
}

struct NowPlayingWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: YMShared.kindNow, provider: YMProvider()) { e in
            NowPlayingWidgetView(e: e).containerBackground(for: .widget) { bg(e.now?.cover) }
        }
        .configurationDisplayName("正在播放").description("当前歌曲，轻点暂停或继续。")
        .supportedFamilies([.systemSmall, .systemMedium, .accessoryCircular, .accessoryRectangular, .accessoryInline])
    }
}

@ViewBuilder private func bg(_ cover: Data?) -> some View {
    ZStack {
        if let cover, let ui = UIImage(data: cover) { Image(uiImage: ui).resizable().scaledToFill().blur(radius: 30).overlay(Color.black.opacity(0.45)) }
        else { LinearGradient(colors: [Color(red: 0.12, green: 0.06, blue: 0.2), Color(red: 0.25, green: 0.1, blue: 0.35)], startPoint: .top, endPoint: .bottom) }
    }
}

// MARK: - 今日乐签

struct CardWidgetView: View {
    @Environment(\.widgetFamily) var family
    let e: YMEntry
    var body: some View {
        if let c = e.card {
            switch family {
            case .accessoryRectangular:
                VStack(alignment: .leading, spacing: 1) {
                    Text("今日乐签").font(.system(size: 11, weight: .semibold)).opacity(0.7)
                    Text("「\(c.quote)」").font(.system(size: 13, weight: .semibold)).lineLimit(2)
                }
            case .accessoryInline: Text("乐签 · \(c.title)")
            case .systemMedium:
                HStack(spacing: 14) {
                    VStack(alignment: .leading, spacing: 6) {
                        Text(c.day).font(.system(size: 30, weight: .heavy, design: .rounded))
                        Squiggle().stroke(lime, style: StrokeStyle(lineWidth: 2.4, lineCap: .round)).frame(width: 70, height: 6)
                        Text("「\(c.quote)」").font(.system(size: 15, weight: .semibold)).lineLimit(3)
                        Text("\(c.title) — \(c.artist)").font(.system(size: 12)).opacity(0.6).lineLimit(1)
                    }
                    Spacer(minLength: 0)
                    Cover(data: c.cover, size: 96).rotationEffect(.degrees(4))
                }
            default:
                VStack(alignment: .leading, spacing: 4) {
                    Text(c.day).font(.system(size: 24, weight: .heavy, design: .rounded))
                    Squiggle().stroke(lime, style: StrokeStyle(lineWidth: 2.2, lineCap: .round)).frame(width: 54, height: 5)
                    Spacer(minLength: 0)
                    Text("「\(c.quote)」").font(.system(size: 14, weight: .semibold)).lineLimit(3)
                    Text(c.title).font(.system(size: 11)).opacity(0.6).lineLimit(1)
                }
            }
        } else { Placeholder(text: "打开译梦\n抽今天的乐签") }
    }
}

struct CardWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: YMShared.kindCard, provider: YMProvider()) { e in
            CardWidgetView(e: e).foregroundStyle(ink).widgetURL(URL(string: "yimeng://yueqian"))
                .containerBackground(for: .widget) { Color(red: 0.98, green: 0.96, blue: 0.9) }
        }
        .configurationDisplayName("今日乐签").description("每天一首歌，一句歌词。")
        .supportedFamilies([.systemSmall, .systemMedium, .accessoryRectangular, .accessoryInline])
    }
}

// MARK: - 私人 FM (tap opens the app and starts FM — an explicit user action)

struct FMWidgetView: View {
    @Environment(\.widgetFamily) var family
    let e: YMEntry
    var body: some View {
        switch family {
        case .accessoryCircular:
            ZStack { AccessoryWidgetBackground(); Image(systemName: "dot.radiowaves.left.and.right").font(.system(size: 18, weight: .bold)) }
        default:
            VStack(alignment: .leading, spacing: 6) {
                Text("私人 FM").font(.system(size: 22, weight: .heavy))
                Squiggle().stroke(.white.opacity(0.85), style: StrokeStyle(lineWidth: 2.4, lineCap: .round)).frame(width: 80, height: 7)
                Spacer(minLength: 0)
                HStack {
                    Text(e.fm?.origins.isEmpty == false ? "按你的口味一直放下去" : "登录平台后可用").font(.system(size: 11)).opacity(0.85)
                    Spacer()
                    Image(systemName: "play.fill").font(.system(size: 14, weight: .bold)).foregroundStyle(Color(red: 0.55, green: 0.2, blue: 0.9)).frame(width: 32, height: 32).background(Circle().fill(.white))
                }
            }.foregroundStyle(.white)
        }
    }
}

struct FMWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: YMShared.kindFM, provider: YMProvider()) { e in
            FMWidgetView(e: e).widgetURL(URL(string: "yimeng://fm"))
                .containerBackground(for: .widget) { LinearGradient(colors: [Color(red: 1, green: 0.3, blue: 0.6), Color(red: 0.5, green: 0.2, blue: 1)], startPoint: .topLeading, endPoint: .bottomTrailing) }
        }
        .configurationDisplayName("私人 FM").description("轻点开始私人 FM。")
        .supportedFamilies([.systemSmall, .accessoryCircular])
    }
}

private extension WidgetFamily { var isAccessory: Bool { self == .accessoryCircular || self == .accessoryRectangular || self == .accessoryInline } }
