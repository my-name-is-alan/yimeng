import SwiftUI

// MARK: - Small pieces agreed with CodingBot

/// Source badge: small grey text after the artist line (only for non-local origins).
struct SourceBadge: View {
    let origin: String?
    @Environment(\.colorScheme) var scheme
    var body: some View {
        if let o = origin, o != "local" {
            Text(CoreOrigin.label(o)).font(.system(size: 11)).foregroundStyle(Theme.secondary(scheme).opacity(0.8))
        }
    }
}

/// 试听: lime outlined capsule.
struct PreviewCapsule: View {
    var body: some View {
        Text("试听").font(.system(size: 10, weight: .semibold)).foregroundStyle(Theme.lime)
            .padding(.horizontal, 6).frame(height: 16)
            .overlay(Capsule().stroke(Theme.lime, lineWidth: 1))
    }
}

/// 曲库 source filter: 全部 / 本地 / 网易云 / QQ / 酷狗 (only platforms the server has enabled).
struct SourceFilterChips: View {
    @Binding var origin: String?
    @Environment(\.colorScheme) var scheme
    var body: some View {
        let enabled = CoreState.platforms.filter { $0.enabled ?? true }.map(\.origin)
        let opts: [String?] = [nil, "local"] + CoreOrigin.all.dropFirst().filter { enabled.isEmpty || enabled.contains($0) }.map { Optional($0) }
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 14) {
                ForEach(opts, id: \.self) { o in
                    let on = origin == o
                    Button { origin = o } label: {
                        Text(o.map(CoreOrigin.label) ?? "全部").font(.system(size: 13, weight: on ? .bold : .regular))
                            .foregroundStyle(on ? Theme.fg(scheme) : Theme.secondary(scheme))
                    }.buttonStyle(.plain)
                }
            }.padding(.horizontal, Theme.margin)
        }
    }
}

extension NDSong {
    /// Unplayable rows dim to 40%; tapping says why.
    var isUnplayable: Bool { playable == false && preview != true }
    var unplayableToast: String { unplayableReason ?? (origin.map { "\(CoreOrigin.label($0)) 暂不能播放这首歌" } ?? "这首歌暂时无法播放") }
}

/// Hand-drawn wobbly frame (seeded), our doodle vocabulary for cards.
struct WobblyFrame: Shape {
    var seed: UInt64 = 7
    var inset: CGFloat = 6
    func path(in r: CGRect) -> Path {
        var rng = SeededRNG(seed)
        let rr = r.insetBy(dx: inset, dy: inset)
        let pts = [CGPoint(x: rr.minX, y: rr.minY), CGPoint(x: rr.maxX, y: rr.minY), CGPoint(x: rr.maxX, y: rr.maxY), CGPoint(x: rr.minX, y: rr.maxY)]
        var p = Path()
        for pass in 0..<2 {
            let amp = min(3, r.width / 40)
            let j = { (v: CGFloat) in v + CGFloat(rng.d(-Double(amp), Double(amp))) * (pass == 0 ? 1 : 1.6) }
            p.move(to: CGPoint(x: j(pts[0].x), y: j(pts[0].y)))
            for i in 1...4 {
                let a = pts[(i - 1) % 4], b = pts[i % 4]
                p.addQuadCurve(to: CGPoint(x: j(b.x), y: j(b.y)), control: CGPoint(x: j((a.x + b.x) / 2), y: j((a.y + b.y) / 2)))
            }
        }
        return p
    }
}

// MARK: - 排行 (charts) with platform switch

struct ChartsView: View {
    @State private var origin: String = "netease"
    @State private var charts: [CoreChart] = []
    @State private var origins: [String] = []
    @State private var loading = true
    @State private var failed = false
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                HStack {
                    Button { if !state.path.isEmpty { state.path.removeLast() } } label: { CircleIconButton(icon: "chevron.left") }
                    Spacer()
                }.padding(.horizontal, Theme.margin).padding(.top, 8)
                Text("排行").font(.system(size: 28, weight: .bold)).padding(.horizontal, Theme.margin).padding(.top, 6)
                DoodleTabs(items: origins, selected: $origin).padding(.horizontal, Theme.margin).padding(.top, 14)
                if loading && charts.isEmpty { DoodleLoader().padding(.top, 40) }
                else if charts.isEmpty { EmptyState(kind: failed ? .network : .playlists).padding(.top, 30) }
                LazyVStack(spacing: 14) {
                    ForEach(charts, id: \.id) { c in
                        Button { state.path.append(.trackList(TrackListSpec(kind: .playlist(c.id), title: c.name, subtitle: c.updateFrequency ?? "", origin: c.origin ?? origin, cover: LibraryStore.shared.coverArtURL(id: c.cover?.url ?? c.coverUrl ?? c.cover?.sourceUrl, size: 600)))) } label: { row(c) }
                            .buttonStyle(.plain)
                    }
                }.padding(.horizontal, Theme.margin).padding(.top, 18)
                Color.clear.frame(height: 160)
            }
        }
        .background(YMBackdrop())
        .task {
            let caps = CoreState.platforms(can: "charts").map(\.origin)
            origins = caps.isEmpty ? CoreState.platforms.filter { $0.enabled ?? true }.map(\.origin).filter { $0 != "local" } : caps
            if origins.isEmpty { origins = ["netease", "qq", "kugou"] }
            if !origins.contains(origin) { origin = origins[0] }
            await load()
        }
        .onChange(of: origin) { _, _ in Task { await load() } }
    }
    private func load() async {
        loading = true; defer { loading = false }
        do {
            let r = try await CoreClient.shared.charts(origin: origin)
            charts = r.charts; failed = false
            if let o = r.origins, !o.isEmpty, o != origins { origins = o }
        } catch { charts = []; failed = true }
    }
    private func chartURL(_ raw: String?) -> URL? {
        guard let raw, !raw.isEmpty else { return nil }
        if let u = LibraryStore.shared.coverArtURL(id: raw, size: 300) { return u }
        // Absolute CDN only — never URL(string:) a bare relative path.
        return raw.hasPrefix("http") ? URL(string: raw) : nil
    }
    private func row(_ c: CoreChart) -> some View {
        let top = Array((c.preview ?? []).prefix(3))
        // CodingBot: cover.url is relative /api/v1/images/… — must go through coverArtURL, not URL(string:).
        let coverRaw = c.cover?.url ?? c.coverUrl ?? c.cover?.sourceUrl
        let urls: [URL?] = top.isEmpty
            ? [chartURL(coverRaw)]
            : top.map { chartURL($0.cover?.url ?? $0.coverUrl ?? $0.cover?.sourceUrl) }
        return HStack(alignment: .center, spacing: 16) {
            ChartCollage(urls: urls.isEmpty || urls.allSatisfy({ $0 == nil }) ? [chartURL(coverRaw)] : urls)
            VStack(alignment: .leading, spacing: 6) {
                HStack(spacing: 6) {
                    Text(c.name).font(.system(size: 17, weight: .bold)).lineLimit(1).truncationMode(.middle)
                    SourceBadge(origin: c.origin ?? origin)
                }
                ForEach(Array(top.enumerated()), id: \.offset) { i, t in
                    HStack(spacing: 8) {
                        RankBadge(rank: i + 1)
                        Text("\(t.title ?? t.name ?? "") — \(t.artists?.first?.name ?? t.artist?.name ?? "")")
                            .font(.system(size: 13)).foregroundStyle(Theme.fg(scheme).opacity(0.75)).lineLimit(1).truncationMode(.middle)
                    }
                }
                Text(Self.updated(c)).font(.system(size: 11)).foregroundStyle(Theme.secondary(scheme))
            }
            Spacer(minLength: 0)
        }
        .padding(14)
        .background(RoundedRectangle(cornerRadius: 18, style: .continuous).fill(Theme.fill(scheme)))
        .contentShape(Rectangle())
    }
    static func updated(_ c: CoreChart) -> String {
        var parts: [String] = []
        if let s = c.updatedAt, let d = ISO8601DateFormatter().date(from: s) {
            let f = DateFormatter(); f.dateFormat = Calendar.current.isDateInToday(d) ? "今天 HH:mm" : "M月d日"; parts.append("更新于 " + f.string(from: d))
        }
        if let f = c.updateFrequency { parts.append(f) }
        return parts.joined(separator: " · ")
    }
}

// MARK: - Logged-in platform content (the platform's own recommendations, passed through + labelled)

@MainActor
final class PlatformStore: ObservableObject {
    static let shared = PlatformStore()
    struct Daily: Identifiable { var id: String { origin }; let origin: String; let tracks: [NDSong]; let date: String? }
    @Published var dailies: [Daily] = []
    @Published var recommend: [(origin: String, lists: [NDPlaylist])] = []
    @Published var fmOrigins: [String] = []
    @Published var userPlaylists: [(origin: String, created: [NDPlaylist], collected: [NDPlaylist])] = []

    func clear() { dailies = []; recommend = []; fmOrigins = []; userPlaylists = [] }
    func refresh() async {
        guard NavidromeClient.core else { clear(); return }
        let c = CoreClient.shared
        var d: [Daily] = [], r: [(String, [NDPlaylist])] = []
        for p in CoreState.platforms(can: "daily") {
            if let x = try? await c.daily(p.origin), !x.list.isEmpty { d.append(Daily(origin: p.origin, tracks: x.list.map(CoreClient.nd), date: x.date)) }
        }
        // /home carries platform sections (one ETag'd call). Eat daily/mine/playlists even if
        // CoreState.platforms was briefly anonymous (loggedIn=false).
        var mineByOrigin: [String: [NDPlaylist]] = [:]
        var dailyOrigins = Set(d.map(\.origin))
        if let h = try? await c.home(), let secs = h.sections {
            for s in secs where s.error == nil {
                guard let o = s.origin else { continue }
                if s.id.hasSuffix(".daily"), let tracks = s.tracks, !tracks.isEmpty, !dailyOrigins.contains(o) {
                    d.append(Daily(origin: o, tracks: tracks.map(CoreClient.nd), date: nil))
                    dailyOrigins.insert(o)
                }
                if let l = s.playlists, !l.isEmpty {
                    if s.id.hasSuffix(".playlists") { r.append((o, l.map(CoreClient.nd))) }
                    if s.id.hasSuffix(".mine") { mineByOrigin[o, default: []] += l.map(CoreClient.nd) }
                }
            }
        } else {
            for p in CoreState.platforms(can: "recommend_playlists") {
                if let l = try? await c.recommendPlaylists(p.origin), !l.isEmpty { r.append((p.origin, l.map(CoreClient.nd))) }
            }
        }
        dailies = d; recommend = r
        if !mineByOrigin.isEmpty {
            // Home `*.mine` is a flat list — put under 我创建的; loadUserPlaylists will refine created/collected.
            userPlaylists = mineByOrigin.keys.sorted().map { o in (o, mineByOrigin[o] ?? [], []) }
        }
        fmOrigins = CoreState.platforms(can: "fm").map(\.origin)
        if fmOrigins.isEmpty {
            // Soft fallback while platforms catch up: origins that already delivered daily usually have FM.
            fmOrigins = d.map(\.origin)
        }
        YMWidgetBridge.fm(fmOrigins)
        await loadUserPlaylists()
        OfflineCache.shared.precache()
    }
    func loadUserPlaylists() async {
        guard NavidromeClient.core else { userPlaylists = []; return }
        var out: [(String, [NDPlaylist], [NDPlaylist])] = []
        for p in CoreState.platforms(can: "user_playlists") {
            let cr = (try? await CoreClient.shared.userPlaylists(p.origin, type: "created"))?.map(CoreClient.nd) ?? []
            let co = (try? await CoreClient.shared.userPlaylists(p.origin, type: "collected"))?.map(CoreClient.nd) ?? []
            if !cr.isEmpty || !co.isEmpty { out.append((p.origin, cr, co)) }
        }
        // Prefer refined created/collected; keep home `*.mine` fallback when endpoints return nothing.
        if !out.isEmpty { userPlaylists = out }
    }
}

/// 私人 FM: an endless queue from the platform; refills before the last track; like / trash feed back.
@MainActor
final class FMStore: ObservableObject {
    static let shared = FMStore()
    @Published private(set) var origin: String?
    @Published private(set) var ids: Set<String> = []
    var active: Bool { origin != nil && PlayerStore.shared.song.map { ids.contains($0.id) } == true }

    func start(_ o: String) async {
        guard let batch = try? await CoreClient.shared.fm(o) else { SheetCenter.shared.say("\(CoreOrigin.label(o)) 没有私人 FM"); return }
        guard !batch.isEmpty else { return }
        origin = o
        let songs = batch.map(CoreClient.nd)
        ids = Set(songs.map(\.id))
        PlayerStore.shared.play(songs: songs, startAt: 0)
    }
    func stop() { origin = nil; ids = [] }
    func owns(_ s: [NDSong]) -> Bool { origin != nil && s.allSatisfy { ids.contains($0.id) } }
    /// Called by PlayerStore when a track starts; tops the queue up near the end.
    func trackStarted() {
        guard let o = origin, active else { return }
        let p = PlayerStore.shared
        guard p.index >= p.queue.count - 2 else { return }
        Task {
            guard let more = try? await CoreClient.shared.fm(o) else { return }
            for s in more.map(CoreClient.nd) where !ids.contains(s.id) { ids.insert(s.id); p.enqueue(s) }
        }
    }
    func like() {
        guard let o = origin, let s = PlayerStore.shared.song else { return }
        if !StarStore.shared.isStarred(song: s.id) { StarStore.shared.toggle(s) }
        Task { await CoreClient.shared.fmFeedback(o, trackId: s.id, action: "like") }
    }
    func trash() {
        guard let o = origin, let s = PlayerStore.shared.song else { return }
        Task { await CoreClient.shared.fmFeedback(o, trackId: s.id, action: "trash") }
        PlayerStore.shared.skip(1)
    }
}

/// FM controls on the now playing screen (replaces nothing; sits above the transport when FM is playing).
struct FMControls: View {
    @ObservedObject private var fm = FMStore.shared
    @ObservedObject private var player = PlayerStore.shared
    @ObservedObject private var stars = StarStore.shared
    var body: some View {
        if fm.active, let s = player.song {
            HStack(spacing: 34) {
                Button { fm.like(); Haptics.success() } label: { YMIcon(glyph: stars.isStarred(song: s.id) ? .heartFill : .heart, size: 18, weight: 1.7, filled: stars.isStarred(song: s.id)) }
                Button { fm.trash(); Haptics.tick() } label: { YMIcon(glyph: .close, size: 16, weight: 1.7) }
                Button { player.skip(1) } label: { YMIcon(glyph: .next, size: 18, weight: 1.8, filled: true) }
                if let o = fm.origin { Text("\(CoreOrigin.label(o)) · 私人 FM").font(.system(size: 12)).opacity(0.6) }
            }
            .font(.system(size: 19, weight: .medium)).buttonStyle(.plain)
        }
    }
}

/// Home: platform sections, each labelled with its platform. Hidden entirely without logged-in accounts.
struct PlatformHomeSections: View {
    @ObservedObject private var store = PlatformStore.shared
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    var body: some View {
        if YMLayout.wide {
            // Wide: daily cards in grid. 乐签 stays above (HomeLive) — do not hide or duplicate here.
            let cols = Array(repeating: GridItem(.flexible(), spacing: 20, alignment: .top), count: YMLayout.cardColumns)
            LazyVGrid(columns: cols, alignment: .leading, spacing: 4) {
                ForEach(store.dailies) { d in
                    VStack(alignment: .leading, spacing: 0) {
                        SectionHeader(title: "\(CoreOrigin.label(d.origin)) · 每日推荐", more: false).padding(.horizontal, -Theme.margin)
                        DailyMixCard(origin: d.origin, songs: d.tracks)
                    }
                }
            }.padding(.horizontal, Theme.margin)
            Color.clear.frame(height: 0).id("fm")
            let fm = store.fmOrigins.first
            let rec = store.recommend.first
            if fm != nil || rec != nil {
                HStack(alignment: .top, spacing: 20) {
                    if let o = fm {
                        VStack(alignment: .leading, spacing: 0) {
                            SectionHeader(title: "\(CoreOrigin.label(o)) · 私人 FM", more: false).padding(.horizontal, -Theme.margin)
                            FMCard(origin: o)
                        }.frame(width: (YMLayout.width - Theme.margin * 2 - 20) / 2)
                    }
                    if let r = rec {
                        VStack(alignment: .leading, spacing: 0) {
                            SectionHeader(title: "\(CoreOrigin.label(r.origin)) · 推荐歌单", more: false).padding(.horizontal, -Theme.margin)
                            recRow(r, pad: 0)
                        }
                    }
                }.padding(.horizontal, Theme.margin)
            }
            ForEach(store.fmOrigins.dropFirst(), id: \.self) { o in
                SectionHeader(title: "\(CoreOrigin.label(o)) · 私人 FM", more: false)
                FMCard(origin: o).padding(.horizontal, Theme.margin)
            }
            ForEach(store.recommend.dropFirst(), id: \.origin) { r in
                SectionHeader(title: "\(CoreOrigin.label(r.origin)) · 推荐歌单", more: false)
                recRow(r, pad: Theme.margin)
            }
            mineHomeSections
        } else {
        ForEach(store.dailies) { d in
            SectionHeader(title: "\(CoreOrigin.label(d.origin)) · 每日推荐", more: false)
            DailyMixCard(origin: d.origin, songs: d.tracks).padding(.horizontal, Theme.margin)
        }
        Color.clear.frame(height: 0).id("fm")
        ForEach(store.fmOrigins, id: \.self) { o in
            SectionHeader(title: "\(CoreOrigin.label(o)) · 私人 FM", more: false)
            FMCard(origin: o).padding(.horizontal, Theme.margin)
        }
        ForEach(store.recommend, id: \.origin) { r in
            SectionHeader(title: "\(CoreOrigin.label(r.origin)) · 推荐歌单", more: false)
            recRow(r, pad: Theme.margin)
        }
        mineHomeSections
        }
    }

    @ViewBuilder private var mineHomeSections: some View {
        ForEach(store.userPlaylists, id: \.origin) { g in
            let lists = g.created + g.collected
            if !lists.isEmpty {
                SectionHeader(title: "\(CoreOrigin.label(g.origin)) · 我的歌单", more: false)
                recRow((g.origin, lists), pad: Theme.margin)
            }
        }
    }
    @ViewBuilder func recRow(_ r: (origin: String, lists: [NDPlaylist]), pad: CGFloat) -> some View {
            let cover = YMLayout.cover(140)
            if YMLayout.wide && pad == 0 {
                let cols = Array(repeating: GridItem(.flexible(), spacing: 14, alignment: .top), count: max(2, min(4, r.lists.count)))
                LazyVGrid(columns: cols, alignment: .leading, spacing: 14) {
                    ForEach(r.lists.prefix(8)) { p in
                        Button { state.path.append(.trackList(TrackListSpec(kind: .playlist(p.id), title: p.name, subtitle: p.songCount.map { "\($0) 首" } ?? "", origin: r.origin, cover: LibraryStore.shared.coverArtURL(id: p.coverId, size: 600)))) } label: {
                            VStack(alignment: .leading, spacing: 6) {
                                RemoteCover(url: LibraryStore.shared.coverArtURL(id: p.coverId, size: 300), size: cover)
                                Text(p.name).font(.system(size: 13, weight: .medium)).lineLimit(2).frame(width: cover, alignment: .leading)
                            }
                        }.buttonStyle(.plain)
                    }
                }
            } else if YMLayout.wide {
                let n = max(3, min(5, Int((YMLayout.width - pad * 2) / (cover + 14))))
                let cols = Array(repeating: GridItem(.flexible(), spacing: 14, alignment: .top), count: n)
                LazyVGrid(columns: cols, alignment: .leading, spacing: 16) {
                    ForEach(r.lists.prefix(n * 2)) { p in
                        Button { state.path.append(.trackList(TrackListSpec(kind: .playlist(p.id), title: p.name, subtitle: p.songCount.map { "\($0) 首" } ?? "", origin: r.origin, cover: LibraryStore.shared.coverArtURL(id: p.coverId, size: 600)))) } label: {
                            VStack(alignment: .leading, spacing: 6) {
                                RemoteCover(url: LibraryStore.shared.coverArtURL(id: p.coverId, size: 300), size: cover)
                                Text(p.name).font(.system(size: 13, weight: .medium)).lineLimit(2).frame(maxWidth: .infinity, alignment: .leading)
                            }
                        }.buttonStyle(.plain)
                    }
                }.padding(.horizontal, pad)
            } else {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 14) {
                        ForEach(r.lists) { p in
                            Button { state.path.append(.trackList(TrackListSpec(kind: .playlist(p.id), title: p.name, subtitle: p.songCount.map { "\($0) 首" } ?? "", origin: r.origin, cover: LibraryStore.shared.coverArtURL(id: p.coverId, size: 600)))) } label: {
                                VStack(alignment: .leading, spacing: 6) {
                                    RemoteCover(url: LibraryStore.shared.coverArtURL(id: p.coverId, size: 300), size: cover)
                                    Text(p.name).font(.system(size: 13, weight: .medium)).lineLimit(2).frame(width: cover, alignment: .leading)
                                }
                            }.buttonStyle(.plain)
                        }
                    }.padding(.horizontal, pad)
                }
            }
    }

}

struct DailyMixCard: View {
    let origin: String; let songs: [NDSong]
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) private var scheme
    static func spec(_ origin: String, _ songs: [NDSong]) -> TrackListSpec {
        TrackListSpec(kind: .daily, title: "\(CoreOrigin.label(origin)) · 每日推荐", subtitle: "\(songs.count) 首 · 按 \(CoreOrigin.label(origin)) 账号", origin: origin, songs: songs)
    }
    private var wash: Color {
        if let c = Color(hexString: songs.first?.colors?.first) { return c.opacity(0.55) }
        // Soft lime/ink wash — never plain Theme.fill alone.
        return Theme.accent(scheme).opacity(scheme == .dark ? 0.22 : 0.18)
    }
    var body: some View {
        // Body opens the list; only the explicit play button plays.
        HStack(spacing: 14) {
            Button { state.path.append(.trackList(Self.spec(origin, songs))) } label: {
                HStack(spacing: 14) {
                    ZStack {
                        let covers = Array(songs.prefix(3))
                        if covers.count >= 2 {
                            ForEach(Array(covers.enumerated().reversed()), id: \.offset) { i, s in
                                RemoteCover(url: LibraryStore.shared.coverArtURL(id: s.coverId ?? s.albumId, size: 200), size: 72, corner: 12)
                                    .rotationEffect(.degrees([0, 8, -7][i]))
                                    .offset(x: [CGFloat(0), 10, -6][i], y: [CGFloat(0), 4, 8][i])
                                    .shadow(color: .black.opacity(0.12), radius: 3, y: 1)
                            }
                        } else {
                            DateTile(size: 84, tint: Color(hexString: songs.first?.colors?.first) ?? Theme.accent(scheme).opacity(0.85))
                        }
                        WobblyFrame(seed: 11, inset: 2).stroke(Theme.fg(scheme).opacity(0.35), lineWidth: 1.4)
                            .frame(width: 92, height: 92)
                    }
                    .frame(width: 96, height: 96)
                    VStack(alignment: .leading, spacing: 4) {
                        Text("\(songs.count) 首 · 按 \(CoreOrigin.label(origin)) 账号").font(.system(size: 13)).opacity(0.7)
                        ForEach(songs.prefix(2)) { s in
                            Text("\(s.title) — \(s.artist)").font(.system(size: 14, weight: .medium)).lineLimit(1).truncationMode(.middle)
                        }
                    }
                    Spacer(minLength: 0)
                }
                .padding(12)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(
                    RoundedRectangle(cornerRadius: 20, style: .continuous)
                        .fill(wash)
                        .overlay(RoundedRectangle(cornerRadius: 20, style: .continuous)
                            .strokeBorder(Theme.fg(scheme).opacity(0.08), style: StrokeStyle(lineWidth: 1, dash: [6, 4])))
                )
                .contentShape(Rectangle())
            }.buttonStyle(.plain)
            Button { PlayerStore.shared.play(songs: songs.filter { !$0.isUnplayable }, startAt: 0) } label: {
                PlayCircle(size: 44)
            }.buttonStyle(.plain)
        }
    }
}

struct FMCard: View {
    let origin: String
    @Environment(\.colorScheme) private var scheme
    var body: some View {
        // Quiet doodle card (same tokens as 我的); the lime crayon wave is the only accent — no fixed brand gradient.
        ZStack(alignment: .leading) {
                RoundedRectangle(cornerRadius: 20, style: .continuous).fill(Theme.fill(scheme))
                    .overlay(RoundedRectangle(cornerRadius: 20, style: .continuous).strokeBorder(Theme.fg(scheme).opacity(0.08), style: StrokeStyle(lineWidth: 1, dash: [6, 4])))
                Crayon(shape: WaveShape(), color: Theme.accent(scheme).opacity(0.8)).frame(width: 180, height: 16).offset(x: 150, y: 30)
                VStack(alignment: .leading, spacing: 6) {
                    Text("私人 FM").font(.system(size: 26, weight: .bold))
                    Text("\(CoreOrigin.label(origin)) 按你的口味一直放下去").font(.system(size: 13)).opacity(0.8)
                    // Only this button starts FM; the card body does nothing on tap.
                    Button { Task { await FMStore.shared.start(origin) } } label: { PlayCircle(size: 44) }
                        .buttonStyle(.plain).padding(.top, 4)
                }.padding(20).foregroundStyle(Theme.fg(scheme))
            }.frame(height: 160)
    }
}

/// 曲库 · 歌单: the user's own platform playlists, grouped by platform.
struct PlatformPlaylistGroups: View {
    @ObservedObject private var store = PlatformStore.shared
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    var body: some View {
        ForEach(store.userPlaylists, id: \.origin) { g in
            VStack(alignment: .leading, spacing: 8) {
                Text("\(CoreOrigin.label(g.origin)) 的歌单").font(.system(size: 17, weight: .bold)).padding(.top, 20)
                ForEach([("我创建的", g.created), ("我收藏的", g.collected)], id: \.0) { title, lists in
                    if !lists.isEmpty {
                        Text(title).font(.system(size: 12)).foregroundStyle(Theme.secondary(scheme)).padding(.top, 4)
                        ForEach(lists) { p in
                            Button { state.path.append(.trackList(TrackListSpec(kind: .playlist(p.id), title: p.name, subtitle: p.songCount.map { "\($0) 首" } ?? "", origin: g.origin, cover: LibraryStore.shared.coverArtURL(id: p.coverId, size: 600)))) } label: {
                                HStack(spacing: 12) {
                                    RemoteCover(url: LibraryStore.shared.coverArtURL(id: p.coverId, size: 120), size: 48, corner: 8)
                                    VStack(alignment: .leading, spacing: 2) {
                                        Text(p.name).font(.system(size: 15)).lineLimit(1).truncationMode(.middle)
                                        HStack(spacing: 6) {
                                            if let n = p.songCount { Text("\(n) 首").font(.system(size: 12)).foregroundStyle(Theme.secondary(scheme)) }
                                            SourceBadge(origin: g.origin)
                                        }
                                    }
                                    Spacer()
                                }.contentShape(Rectangle())
                            }.buttonStyle(.plain)
                        }
                    }
                }
            }.padding(.horizontal, Theme.margin)
        }
        .task { await store.loadUserPlaylists() }
    }
}

/// Dream data on now playing: tags + tempo, each shown only if present (server bpm, else on-device estimate once stable).
@MainActor
final class DreamStore {
    static let shared = DreamStore()
    func load(_ s: NDSong) {
        guard NavidromeClient.core else { return }
        let id = s.id
        Task {
            guard let d = await CoreClient.shared.dream(CoreClient.path(id)) else { return }
            AppStateBridge.current?.applyDream(id, d)
        }
    }
}

struct BeatTagLine: View {
    let track: DreamTrack
    var body: some View {
        TimelineView(.periodic(from: .now, by: 2)) { _ in
            let r = BeatStore.read()
            let bpm = track.serverBPM ?? (r.live && r.bpm > 0 ? r.bpm : nil)
            let parts = track.tags.prefix(3).map { $0 } + (bpm.map { ["\(Int($0.rounded())) BPM"] } ?? [])
            if !parts.isEmpty { Text(parts.joined(separator: " · ")).font(.system(size: 11)).opacity(0.5) }
        }
    }
}
