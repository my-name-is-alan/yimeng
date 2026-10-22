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
            let j = { (v: CGFloat) in v + CGFloat(rng.d(-3, 3)) * (pass == 0 ? 1 : 1.6) }
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
                Text("排行").font(.system(size: 28, weight: .bold)).padding(.horizontal, Theme.margin).padding(.top, 8)
                Picker("平台", selection: $origin) {
                    ForEach(origins, id: \.self) { Text(CoreOrigin.label($0)).tag($0) }
                }
                .pickerStyle(.segmented).padding(.horizontal, Theme.margin).padding(.top, 14)
                if loading && charts.isEmpty { DoodleLoader().padding(.top, 40) }
                else if charts.isEmpty { EmptyState(kind: failed ? .network : .playlists).padding(.top, 30) }
                LazyVStack(spacing: 14) {
                    ForEach(charts, id: \.id) { c in
                        Button { state.path.append(.ndPlaylist(NDPlaylist(id: c.id, name: c.name, songCount: nil, coverId: c.cover?.url))) } label: { row(c) }
                            .buttonStyle(.plain)
                    }
                }.padding(.horizontal, Theme.margin).padding(.top, 18)
                Color.clear.frame(height: 160)
            }
        }
        .background(Theme.base(scheme))
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
    private func row(_ c: CoreChart) -> some View {
        HStack(alignment: .top, spacing: 14) {
            RemoteCover(url: c.cover?.url.flatMap(URL.init(string:)), size: 96, corner: 12)
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 6) {
                    Text(c.name).font(.system(size: 16, weight: .semibold)).lineLimit(1).truncationMode(.middle)
                    SourceBadge(origin: c.origin ?? origin)
                }
                if let f = c.updateFrequency { Text(f).font(.system(size: 11)).foregroundStyle(Theme.secondary(scheme)) }
                ForEach(Array((c.preview ?? []).prefix(3).enumerated()), id: \.offset) { i, t in
                    Text("\(i + 1). \(t.title ?? t.name ?? "") — \(t.artists?.first?.name ?? t.artist?.name ?? "")")
                        .font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme)).lineLimit(1).truncationMode(.middle)
                }
            }
            Spacer(minLength: 0)
        }
        .contentShape(Rectangle())
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
            if let x = try? await c.daily(p.origin), !x.tracks.isEmpty { d.append(Daily(origin: p.origin, tracks: x.tracks.map(CoreClient.nd), date: x.date)) }
        }
        for p in CoreState.platforms(can: "recommend_playlists") {
            if let l = try? await c.recommendPlaylists(p.origin), !l.isEmpty { r.append((p.origin, l.map(CoreClient.nd))) }
        }
        dailies = d; recommend = r
        fmOrigins = CoreState.platforms(can: "fm").map(\.origin)
    }
    func loadUserPlaylists() async {
        guard NavidromeClient.core else { userPlaylists = []; return }
        var out: [(String, [NDPlaylist], [NDPlaylist])] = []
        for p in CoreState.platforms(can: "user_playlists") {
            let cr = (try? await CoreClient.shared.userPlaylists(p.origin, type: "created"))?.map(CoreClient.nd) ?? []
            let co = (try? await CoreClient.shared.userPlaylists(p.origin, type: "collected"))?.map(CoreClient.nd) ?? []
            if !cr.isEmpty || !co.isEmpty { out.append((p.origin, cr, co)) }
        }
        userPlaylists = out
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
                Button { fm.like() } label: { Image(systemName: stars.isStarred(song: s.id) ? "heart.fill" : "heart") }
                Button { fm.trash() } label: { Image(systemName: "trash") }
                Button { player.skip(1) } label: { Image(systemName: "forward.end") }
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
        ForEach(store.dailies) { d in
            SectionHeader(title: "\(CoreOrigin.label(d.origin)) · 每日推荐", more: false)
            DailyMixCard(origin: d.origin, songs: d.tracks).padding(.horizontal, Theme.margin)
        }
        ForEach(store.fmOrigins, id: \.self) { o in
            SectionHeader(title: "\(CoreOrigin.label(o)) · 私人 FM", more: false)
            FMCard(origin: o).padding(.horizontal, Theme.margin)
        }
        ForEach(store.recommend, id: \.origin) { r in
            SectionHeader(title: "\(CoreOrigin.label(r.origin)) · 推荐歌单", more: false)
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 14) {
                    ForEach(r.lists) { p in
                        Button { state.path.append(.ndPlaylist(p)) } label: {
                            VStack(alignment: .leading, spacing: 6) {
                                RemoteCover(url: LibraryStore.shared.coverArtURL(id: p.coverId, size: 300), size: 140)
                                Text(p.name).font(.system(size: 13, weight: .medium)).lineLimit(2).frame(width: 140, alignment: .leading)
                            }
                        }.buttonStyle(.plain)
                    }
                }.padding(.horizontal, Theme.margin)
            }
        }
    }
}

struct DailyMixCard: View {
    let origin: String; let songs: [NDSong]
    var body: some View {
        let day = Calendar.current.component(.day, from: Date())
        Button { PlayerStore.shared.play(songs: songs.filter { !$0.isUnplayable }, startAt: 0) } label: {
            HStack(spacing: 16) {
                ZStack {
                    RoundedRectangle(cornerRadius: 16, style: .continuous).fill(Color(hex: 0x7a1cff).opacity(0.85))
                    WobblyFrame(seed: UInt64(day)).stroke(.white.opacity(0.8), lineWidth: 1.6)
                    Text("\(day)").font(.system(size: 40, weight: .black, design: .rounded)).foregroundStyle(.white)
                }.frame(width: 92, height: 92)
                VStack(alignment: .leading, spacing: 4) {
                    Text("\(songs.count) 首 · 按 \(CoreOrigin.label(origin)) 账号").font(.system(size: 13)).opacity(0.65)
                    ForEach(songs.prefix(2)) { s in Text("\(s.title) — \(s.artist)").font(.system(size: 14)).lineLimit(1).truncationMode(.middle) }
                }
                Spacer(minLength: 0)
                Image(systemName: "play.circle.fill").font(.system(size: 30))
            }.contentShape(Rectangle())
        }.buttonStyle(.plain)
    }
}

struct FMCard: View {
    let origin: String
    var body: some View {
        Button { Task { await FMStore.shared.start(origin) } } label: {
            ZStack(alignment: .leading) {
                RoundedRectangle(cornerRadius: 20, style: .continuous)
                    .fill(LinearGradient(colors: [Color(hex: 0xff3d8b), Color(hex: 0x7a1cff)], startPoint: .topLeading, endPoint: .bottomTrailing))
                Crayon(shape: WaveShape(), color: .white.opacity(0.5)).frame(width: 180, height: 16).offset(x: 150, y: 30)
                VStack(alignment: .leading, spacing: 6) {
                    Text("私人 FM").font(.system(size: 26, weight: .bold))
                    Text("\(CoreOrigin.label(origin)) 按你的口味一直放下去").font(.system(size: 13)).opacity(0.8)
                    Image(systemName: "play.fill").font(.system(size: 18)).padding(.top, 6)
                }.padding(20).foregroundStyle(.white)
            }.frame(height: 150)
        }.buttonStyle(.plain)
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
                            Button { state.path.append(.ndPlaylist(p)) } label: {
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
