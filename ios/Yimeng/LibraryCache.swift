import SwiftUI

/// App-level 曲库 cache: survives tab/segment switches, persisted per segment, stale-while-revalidate.
/// Keys: "songs", "albums:<sort>", "artists", "playlists".
@MainActor
final class LibraryCache: ObservableObject {
    static let shared = LibraryCache()
    static let ttl: TimeInterval = 10 * 60
    static let songPage = 50
    static let albumPage = 60

    @Published private(set) var songs: [NDSong] = []
    @Published private(set) var albums: [String: [NDAlbum]] = [:]
    @Published private(set) var artists: [NDArtist] = []
    @Published private(set) var loading: Set<String> = []
    @Published private(set) var paging: Set<String> = []
    @Published private(set) var done: Set<String> = []
    @Published private(set) var failed: Set<String> = []
    /// Album lists may be a window that starts mid-library after an elevator jump (server offset of item 0).
    @Published private(set) var albumBase: [String: Int] = [:]
    /// key → server bucket → first offset (alphabeticalByName), found by k-ary search; memoised per session.
    private var letterOffset: [String: [Int: Int]] = [:]
    /// Per-segment scroll anchor (not published: restoring shouldn't re-render).
    var scrollAnchor: [String: String] = [:]
    private var stamps: [String: Date] = [:]
    private var diskLoaded: Set<String> = []
    private var prefetched = false

    private struct Box<T: Codable>: Codable { var items: T; var savedAt: Date }

    private static func file(_ key: String) -> URL {
        let d = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0].appendingPathComponent("library", isDirectory: true)
        try? FileManager.default.createDirectory(at: d, withIntermediateDirectories: true)
        return d.appendingPathComponent(key.replacingOccurrences(of: ":", with: "_") + ".v1.json")
    }

    // MARK: Read

    func hasData(_ key: String) -> Bool {
        loadDisk(key)
        switch key {
        case "songs": return !songs.isEmpty
        case "artists": return !artists.isEmpty
        case "playlists": return PlaylistEvents.shared.lists != nil
        default: return !(albums[key] ?? []).isEmpty
        }
    }
    func albumList(_ sort: String) -> [NDAlbum] { loadDisk("albums:\(sort)"); return albums["albums:\(sort)"] ?? [] }

    /// Synchronous disk load the first time a key is touched, so the first frame already has content.
    private func loadDisk(_ key: String) {
        guard !diskLoaded.contains(key) else { return }
        diskLoaded.insert(key)
        let t0 = Date()
        guard let data = try? Data(contentsOf: Self.file(key)) else { return }
        let dec = JSONDecoder()
        switch key {
        case "songs": if let b = try? dec.decode(Box<[NDSong]>.self, from: data) { songs = b.items; stamps[key] = b.savedAt }
        case "artists": if let b = try? dec.decode(Box<[NDArtist]>.self, from: data) { artists = b.items; stamps[key] = b.savedAt }
        case "playlists": if let b = try? dec.decode(Box<[NDPlaylist]>.self, from: data) {
            if PlaylistEvents.shared.lists == nil { PlaylistEvents.shared.lists = b.items }; stamps[key] = b.savedAt }
        default: if let b = try? dec.decode(Box<[NDAlbum]>.self, from: data) { albums[key] = b.items; stamps[key] = b.savedAt }
        }
        NetLog.logger.info("CACHE disk \(key, privacy: .public) \(Int(Date().timeIntervalSince(t0) * 1000))ms")
    }

    private func persist(_ key: String) {
        let enc = JSONEncoder(), now = stamps[key] ?? Date()
        let data: Data?
        switch key {
        case "songs": data = try? enc.encode(Box(items: Array(songs.prefix(200)), savedAt: now))
        case "artists": data = try? enc.encode(Box(items: artists, savedAt: now))
        case "playlists": data = try? enc.encode(Box(items: PlaylistEvents.shared.lists ?? [], savedAt: now))
        default:
            guard (albumBase[key] ?? 0) == 0 else { return }
            data = try? enc.encode(Box(items: Array((albums[key] ?? []).prefix(240)), savedAt: now))
        }
        guard let data else { return }
        let url = Self.file(key)
        Task.detached(priority: .utility) { try? data.write(to: url, options: .atomic) }
    }

    // MARK: Fetch

    /// Show cache immediately; hit the network only if empty, stale (>10 min) or forced.
    func ensure(_ key: String, force: Bool = false) async {
        let t0 = Date()
        let had = hasData(key)
        if had { NetLog.logger.info("TTFC \(key, privacy: .public) cache 0ms") }
        let stale = Date().timeIntervalSince(stamps[key] ?? .distantPast) > Self.ttl
        guard force || !had || stale, !loading.contains(key) else { return }
        loading.insert(key); defer { loading.remove(key) }
        do {
            try await fetchFirst(key, force: force)
            failed.remove(key)
            stamps[key] = Date()
            persist(key)
            if !had { NetLog.logger.info("TTFC \(key, privacy: .public) network \(Int(Date().timeIntervalSince(t0) * 1000))ms") }
        } catch {
            if !had { failed.insert(key) }
        }
    }

    private func fetchFirst(_ key: String, force: Bool = false) async throws {
        let c = NavidromeClient.shared
        switch key {
        case "songs":
            let page = try await c.songs(offset: 0, count: Self.songPage)
            songs = Self.merge(page, into: songs)
            if page.count < Self.songPage { done.insert(key) }
        case "artists":
            if force { await c.invalidateArtists() }
            let a = try await c.artists()
            if a != artists { artists = a }
            done.insert(key)
        case "playlists":
            await PlaylistEvents.shared.load(force: true)
        default:
            if force { albumBase[key] = 0; albums[key] = [] }
            else if (albumBase[key] ?? 0) > 0 { return }   // jumped window: don't splice page 0 into it
            let sort = String(key.dropFirst("albums:".count))
            let size = sort == "random" ? Self.albumPage : Self.albumPage
            let page = try await c.albumList(type: sort, offset: 0, size: size)
            // Random is a fresh draw each time — don't merge, just replace.
            albums[key] = sort == "random" ? page : Self.merge(page, into: albums[key] ?? [])
            if page.count < size || sort == "random" { done.insert(key) }
        }
    }

    /// Fresh first page + whatever we already had beyond it, by stable id — no jump in scroll position.
    private static func merge<T: Identifiable & Equatable>(_ fresh: [T], into old: [T]) -> [T] {
        let ids = Set(fresh.map(\.id))
        let merged = fresh + old.dropFirst(fresh.count).filter { !ids.contains($0.id) }
        return merged == old ? old : merged
    }

    /// Next page, triggered ~20 rows before the end.
    func more(_ key: String) async {
        guard !paging.contains(key), !done.contains(key), !loading.contains(key) else { return }
        paging.insert(key); defer { paging.remove(key) }
        let c = NavidromeClient.shared
        if key == "songs" {
            guard let page = try? await c.songs(offset: songs.count, count: Self.songPage) else { return }
            let ids = Set(songs.map(\.id))
            songs += page.filter { !ids.contains($0.id) }
            if page.count < Self.songPage { done.insert(key) }
        } else if key.hasPrefix("albums:") {
            let sort = String(key.dropFirst("albums:".count))
            let cur = albums[key] ?? []
            guard let page = try? await c.albumList(type: sort, offset: (albumBase[key] ?? 0) + cur.count, size: Self.albumPage) else { return }
            let ids = Set(cur.map(\.id))
            albums[key] = cur + page.filter { !ids.contains($0.id) }
            if page.count < Self.albumPage { done.insert(key) }
        }
    }

    /// Load the page before a jumped window (prepends; scroll anchor keeps the view still).
    func less(_ key: String) async {
        let base = albumBase[key] ?? 0
        guard base > 0, !paging.contains(key) else { return }
        paging.insert(key); defer { paging.remove(key) }
        let sort = String(key.dropFirst("albums:".count))
        let off = max(0, base - Self.albumPage)
        guard let page = try? await NavidromeClient.shared.albumList(type: sort, offset: off, size: base - off) else { return }
        let cur = albums[key] ?? [], ids = Set(cur.map(\.id))
        albums[key] = page.filter { !ids.contains($0.id) } + cur
        albumBase[key] = off
        if off == 0 { persist(key) }
    }

    /// Elevator jump for A–Z albums (paged, 70k+). Returns the album id to scroll to.
    /// 1) boundary already inside the loaded window → no network;
    /// 2) else find the server offset of the letter with a parallel 8-ary search over size=1 probes
    ///    (~3 rounds × 7 requests + one ≤500 fetch), memoised;
    /// 3) offset just past the window (≤300) → extend forward; otherwise replace with a window starting there.
    func jumpAlbums(_ key: String, letter: String) async -> String? {
        let target = Initials.bucket(ofLetter: letter)
        let cur = albums[key] ?? [], base = albumBase[key] ?? 0
        if let i = cur.firstIndex(where: { Initials.serverBucket($0.name) >= target }), i > 0 || base == 0 { return cur[i].id }
        if done.contains(key), base == 0, cur.allSatisfy({ Initials.serverBucket($0.name) < target }) { return cur.last?.id }
        let sort = String(key.dropFirst("albums:".count))
        let t0 = Date()
        guard let off = await letterOffset(sort: sort, key: key, target: target) else { return nil }
        NetLog.logger.info("JUMP albums \(letter, privacy: .public) → offset \(off) in \(Int(Date().timeIntervalSince(t0) * 1000))ms")
        let c = NavidromeClient.shared
        let now = albums[key] ?? [], b = albumBase[key] ?? 0, end = b + now.count
        if off >= b && off < end { return now[off - b].id }
        if off >= end && off - end <= 300, !done.contains(key) {
            guard let page = try? await c.albumList(type: sort, offset: end, size: off - end + Self.albumPage) else { return nil }
            let ids = Set(now.map(\.id))
            albums[key] = now + page.filter { !ids.contains($0.id) }
            if page.count < off - end + Self.albumPage { done.insert(key) }
            let all = albums[key] ?? []
            return off - b < all.count ? all[off - b].id : all.last?.id
        }
        guard let page = try? await c.albumList(type: sort, offset: off, size: Self.albumPage), !page.isEmpty else { return nil }
        albums[key] = page; albumBase[key] = off
        if page.count < Self.albumPage { done.insert(key) } else { done.remove(key) }
        return page.first?.id
    }

    private func letterOffset(sort: String, key: String, target: Int) async -> Int? {
        if let o = letterOffset[key]?[target] { return o }
        let c = NavidromeClient.shared
        // Invariant: everything before `lo` is < target; item at `hi` (or end) is ≥ target.
        var lo = 0, hi = (LibraryStore.shared.albumTotal ?? 200_000) + 2000
        // Tighten with what we already know.
        for (b, o) in letterOffset[key] ?? [:] { if b < target { lo = max(lo, o) } else { hi = min(hi, o) } }
        func probe(_ o: Int) async -> Int {
            guard let a = try? await c.albumList(type: sort, offset: o, size: 1), let f = a.first else { return 99 }
            return Initials.serverBucket(f.name)
        }
        var rounds = 0
        while hi - lo > 500, rounds < 8 {
            rounds += 1
            let pts = (1...7).map { lo + (hi - lo) * $0 / 8 }
            let res = await withTaskGroup(of: (Int, Int).self) { g -> [Int: Int] in
                for p in pts { g.addTask { (p, await probe(p)) } }
                var m: [Int: Int] = [:]; for await (p, r) in g { m[p] = r }; return m
            }
            var nlo = lo, nhi = hi
            for p in pts { if res[p]! < target { nlo = p + 1 } else { nhi = p; break } }
            lo = nlo; hi = nhi
        }
        guard let win = try? await c.albumList(type: sort, offset: lo, size: max(1, min(500, hi - lo + 1))) else { return nil }
        let i = win.firstIndex { Initials.serverBucket($0.name) >= target } ?? win.count
        let off = lo + i
        letterOffset[key, default: [:]][target] = off
        return off
    }

    /// First open of 曲库: visible segment first, then the others' first pages in parallel, quietly.
    func open(_ visible: String, albumSort: String) async {
        await ensure(visible)
        guard !prefetched else { return }
        prefetched = true
        let others = ["songs", "albums:\(albumSort)", "artists", "playlists"].filter { $0 != visible }
        await withTaskGroup(of: Void.self) { g in
            for k in others { g.addTask { await self.ensure(k) } }
        }
        persist("playlists")
    }

    func clear() {
        albumBase = [:]; letterOffset = [:]
        songs = []; albums = [:]; artists = []; stamps = [:]; done = []; diskLoaded = []; prefetched = false
        try? FileManager.default.removeItem(at: Self.file("songs").deletingLastPathComponent())
    }
}

/// Small crayon moon loader shown with skeletons when a segment has no cache yet.
struct DoodleLoader: View {
    var text = "正在翻唱片…"
    @State private var spin = false
    var body: some View {
        HStack(spacing: 10) {
            ZStack {
                Crayon(shape: MoonShape(), color: .primary, width: 1.8).frame(width: 18, height: 18)
                Crayon(shape: StarShape(), color: Theme.lime, width: 1.4).frame(width: 7, height: 7).offset(x: 11, y: -9)
            }
            .rotationEffect(.degrees(spin ? 12 : -12))
            Text(text).font(.system(size: 13)).opacity(0.65)
        }
        .padding(.vertical, 10).frame(maxWidth: .infinity)
        .onAppear { withAnimation(.easeInOut(duration: 0.8).repeatForever(autoreverses: true)) { spin = true } }
    }
}

/// Album tile placeholder matching the 2-column grid.
struct SkeletonTile: View {
    let size: CGFloat
    @Environment(\.colorScheme) var scheme
    @State private var phase = false
    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            RoundedRectangle(cornerRadius: 12, style: .continuous).fill(Theme.fill(scheme)).frame(width: size, height: size)
                .overlay(Crayon(shape: RecordShape(), color: Theme.fg(scheme).opacity(0.12), width: 1.4).frame(width: size * 0.35, height: size * 0.35))
            Capsule().fill(Theme.fill(scheme)).frame(width: size * 0.7, height: 10)
            Capsule().fill(Theme.fill(scheme)).frame(width: size * 0.45, height: 8)
        }
        .opacity(phase ? 0.55 : 1)
        .onAppear { withAnimation(.easeInOut(duration: 0.9).repeatForever(autoreverses: true)) { phase = true } }
    }
}

/// "Loading more" row at the bottom of paged lists.
struct InlineLoader: View {
    var body: some View {
        HStack(spacing: 8) { ProgressView().controlSize(.small); Text("加载更多…").font(.system(size: 12)).opacity(0.6) }
            .frame(maxWidth: .infinity).padding(.vertical, 14)
    }
}
