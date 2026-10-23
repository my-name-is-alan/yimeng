import SwiftUI
import Photos

/// 今日乐签 — one song a day as a keepsake card (our doodle take, not a copy of anyone's).
/// Song: first track of a logged-in platform's daily list (platform pick, passed through); otherwise a
/// deterministic pick of the day from the user's own library (seeded by the date). Never labelled 推荐.
@MainActor
final class YueqianStore: ObservableObject {
    static let shared = YueqianStore()
    struct Card: Equatable {
        let song: NDSong; let quote: String; let date: Date; let origin: String?; let coverURL: URL?
        static func == (a: Card, b: Card) -> Bool { a.song.id == b.song.id && a.quote == b.quote }
    }
    @Published private(set) var card: Card?
    @Published var cover: UIImage?
    @Published private(set) var palette = PaperPalette.fallback
    static func dayKey(_ d: Date = Date()) -> String { let f = DateFormatter(); f.dateFormat = "yyyy-MM-dd"; return f.string(from: d) }

    /// Local open log: which days this month the card was opened.
    @Published private(set) var opened = YueqianStore.openedThisMonth
    func markOpened() {
        Self.recordOpen(); opened = Self.openedThisMonth
    }
    static func recordOpen() {
        var s = Set(UserDefaults.standard.stringArray(forKey: "ym.yueqian.days") ?? [])
        s.insert(dayKey()); UserDefaults.standard.set(Array(Array(s).sorted().suffix(400)), forKey: "ym.yueqian.days")
    }
    static var openedThisMonth: Int {
        let m = String(dayKey().prefix(7))
        return (UserDefaults.standard.stringArray(forKey: "ym.yueqian.days") ?? []).filter { $0.hasPrefix(m) }.count
    }

    func load() async {
        guard LibraryStore.shared.connected else { return }
        var pick: NDSong?, origin: String?
        if NavidromeClient.core {
            // Prefer authenticated daily capability; fall back to PlatformStore / home sections
            // when anonymous server/info briefly wiped platforms (真机 2026-10-23).
            var origins = CoreState.platforms(can: "daily").map(\.origin)
            if origins.isEmpty { origins = PlatformStore.shared.dailies.map(\.origin) }
            for o in origins {
                if let d = try? await CoreClient.shared.daily(o), let t = d.list.first {
                    pick = CoreClient.nd(t); origin = o; break
                }
            }
            if pick == nil, let d = PlatformStore.shared.dailies.first, let t = d.tracks.first {
                pick = t; origin = d.origin
            }
            if pick == nil, let h = try? await CoreClient.shared.home(), let secs = h.sections {
                for s in secs where s.error == nil && (s.id.hasSuffix(".daily")) {
                    if let o = s.origin, let t = s.tracks?.first {
                        pick = CoreClient.nd(t); origin = o; break
                    }
                }
            }
        }
        if pick == nil { pick = await Self.dayPick() }
        guard let s = pick else {
            // Connected Core with empty library/platform: keep a stub so the home ticket still renders.
            if NavidromeClient.core && LibraryStore.shared.connected {
                let stub = NDSong(id: "yueqian:stub", title: "今日乐签", artist: "点开取签", album: nil, albumId: nil, coverId: nil, duration: nil, track: nil)
                card = Card(song: stub, quote: "今日乐签 · 点开取签", date: Date(), origin: nil, coverURL: nil)
                cover = nil; palette = .fallback
            } else { card = nil }
            return
        }
        let url = LibraryStore.shared.coverArtURL(id: s.coverId ?? s.albumId, size: 600)
        // Paint card first (album/title quote); lyrics fill in after — avoids stuck loading on slow lyric fetch.
        card = Card(song: s, quote: s.album ?? s.title, date: Date(), origin: origin, coverURL: url)
        palette = PaperPalette.make(hex: s.colors ?? CoreState.colors(for: url), image: cover)
        if let url { cover = try? await CoverCache.shared.fetch(url); palette = PaperPalette.make(hex: s.colors ?? CoreState.colors(for: url), image: cover) }
        YMWidgetBridge.card(card, cover: cover)
        let lines = await LyricsStore.fetch(s).0
        if let q = Self.highlight(lines.map(\.text)) {
            card = Card(song: s, quote: q, date: Date(), origin: origin, coverURL: url)
            YMWidgetBridge.card(card, cover: cover)
        }
    }
    /// Seeded by the date → same song all day, different tomorrow.
    static func dayPick() async -> NDSong? {
        let total = LibraryStore.shared.songTotal
        guard let n = total, n > 0 else { return nil }
        var rng = SeededRNG(UInt64(abs(dayKey().hashValue32)))
        let off = Int(rng.next() % UInt64(n))
        return (try? await NavidromeClient.shared.songs(offset: off, count: 1))?.first
    }
    /// A line worth quoting: mid-song, 6–22 chars, not a credit line.
    static func highlight(_ lines: [String]) -> String? {
        let c = lines.filter { l in l.count >= 6 && l.count <= 22 && !l.contains(":") && !l.contains("：") && !l.contains("作词") && !l.contains("作曲") }
        guard !c.isEmpty else { return nil }
        // repeated lines are usually the hook
        let counts = Dictionary(c.map { ($0, 1) }, uniquingKeysWith: +)
        if let hook = counts.filter({ $0.value > 1 }).max(by: { $0.value < $1.value })?.key { return hook }
        return c[c.count / 2]
    }

}

extension String {
    /// Stable across launches (Swift's hashValue is seeded per process).
    var hashValue32: Int32 { var h: UInt32 = 2166136261; for b in utf8 { h = (h ^ UInt32(b)) &* 16777619 }; return Int32(bitPattern: h) }
}

struct YueqianCardView: View {
    let card: YueqianStore.Card
    let cover: UIImage?
    var palette: PaperPalette = .fallback
    var interactive = true
    @ObservedObject private var stars = StarStore.shared
    var body: some View {
        let cal = Calendar.current
        let m = cal.component(.month, from: card.date), d = cal.component(.day, from: card.date)
        let ink = palette.ink
        VStack(spacing: 0) {
            ZStack {
                Group {
                    if let cover { Image(uiImage: cover).resizable().scaledToFill() } else { Color(hex: 0xe9e2d6) }
                }
                .frame(width: 250, height: 250).clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                WobblyFrame(seed: UInt64(d * 31 + m), inset: -6).stroke(ink, style: StrokeStyle(lineWidth: 1.8, lineCap: .round)).frame(width: 250, height: 250)
                Crayon(shape: HeartShape(), color: Color(hex: 0xff5fa8)).frame(width: 26, height: 22).offset(x: 128, y: -122)
            }.padding(.top, 26)
            HStack(alignment: .center, spacing: 10) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(card.song.title).font(.system(size: 18, weight: .bold)).lineLimit(1).truncationMode(.middle)
                    HStack(spacing: 6) { Text(card.song.artist).font(.system(size: 13)).opacity(0.65).lineLimit(1); SourceBadge(origin: card.origin) }
                }
                Spacer()
                if interactive {
                    Button { stars.toggle(card.song) } label: { Image(systemName: stars.isStarred(song: card.song.id) ? "heart.fill" : "heart").font(.system(size: 20)) }
                    Button { PlayerStore.shared.play(songs: [card.song], startAt: 0, coverURL: card.coverURL) } label: { Image(systemName: "play.circle.fill").font(.system(size: 30)) }
                }
            }.buttonStyle(.plain).padding(.horizontal, 24).padding(.top, 18)
            HStack(alignment: .firstTextBaseline, spacing: 4) {
                Text(String(format: "%02d", m)).font(.system(size: 54, weight: .black, design: .rounded)).foregroundStyle(palette.accent)
                Crayon(shape: Path { p in p.move(to: CGPoint(x: 26, y: 0)); p.addLine(to: CGPoint(x: 0, y: 46)) }, color: palette.accent)
                    .frame(width: 26, height: 46).offset(y: 6)
                Text(String(format: "%02d", d)).font(.system(size: 54, weight: .black, design: .rounded)).foregroundStyle(palette.accent)
                Spacer()
                Text(Self.weekday(card.date)).font(.system(size: 13)).opacity(0.55)
            }.padding(.horizontal, 24).padding(.top, 6)
            Text("「\(card.quote)」").font(LyricFontStore.shared.font(size: 17)).frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 24).padding(.top, 4).lineLimit(2)
            TicketDivider().stroke(ink.opacity(0.35), style: StrokeStyle(lineWidth: 1, dash: [4, 5])).frame(height: 18).padding(.top, 18)
            HStack {
                Text("这个月第 \(YueqianStore.shared.opened) 天来取乐签").font(.system(size: 12)).opacity(0.6)
                Spacer()
                Text("译梦 · 今日乐签").font(.system(size: 12, weight: .semibold)).opacity(0.6)
            }.padding(.horizontal, 24).padding(.vertical, 16)
        }
        .foregroundStyle(ink)
        .frame(width: 330)
        .background(RoundedRectangle(cornerRadius: 22, style: .continuous).fill(palette.paper))
    }
    static func weekday(_ d: Date) -> String { let f = DateFormatter(); f.locale = Locale(identifier: "zh_CN"); f.dateFormat = "EEEE"; return f.string(from: d) }
}

/// Perforation: dashed line with two half-circle notches.
struct TicketDivider: Shape {
    func path(in r: CGRect) -> Path {
        var p = Path()
        p.addArc(center: CGPoint(x: r.minX, y: r.midY), radius: 9, startAngle: .degrees(-90), endAngle: .degrees(90), clockwise: false)
        p.move(to: CGPoint(x: r.minX + 14, y: r.midY)); p.addLine(to: CGPoint(x: r.maxX - 14, y: r.midY))
        p.addArc(center: CGPoint(x: r.maxX, y: r.midY), radius: 9, startAngle: .degrees(90), endAngle: .degrees(270), clockwise: false)
        return p
    }
}

extension YueqianStore {
    static func saveToPhotos(_ img: UIImage, done: ((Bool) -> Void)? = nil) {
        PHPhotoLibrary.requestAuthorization(for: .addOnly) { s in
            guard s == .authorized || s == .limited else {
                Task { @MainActor in
                    SheetCenter.shared.say("没有相册权限")
                    done?(false)
                }
                return
            }
            PHPhotoLibrary.shared().performChanges { PHAssetChangeRequest.creationRequestForAsset(from: img) } completionHandler: { ok, _ in
                Task { @MainActor in
                    // Toast under fullScreenCover is invisible — callers that pass `done` show in-page feedback.
                    if done == nil { SheetCenter.shared.say(ok ? "已保存到相册" : "保存失败") }
                    done?(ok)
                }
            }
        }
    }
}
