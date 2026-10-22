import SwiftUI
import os

/// Request log (method + latency only — never the URL, which carries the auth token).
enum NetLog {
    static let logger = Logger(subsystem: "art.yimeng.app", category: "net")
    private static let lock = NSLock()
    nonisolated(unsafe) private static var counts: [String: Int] = [:]
    static func record(_ method: String, ms: Int, bytes: Int) {
        lock.lock(); counts[method, default: 0] += 1; let n = counts[method]!; lock.unlock()
        logger.info("ND \(method, privacy: .public) \(ms)ms \(bytes / 1024)KB #\(n)")
    }
}

/// 2×2 mosaic of the first four distinct album covers, or the server's playlist cover.
struct PlaylistCover: View {
    let playlist: NDPlaylist
    var songs: [NDSong] = []
    var size: CGFloat
    var corner: CGFloat = 16
    @ObservedObject private var library = LibraryStore.shared
    private var tiles: [String] {
        var seen = Set<String>(), out: [String] = []
        for s in songs { if let c = s.coverId ?? s.albumId, seen.insert(c).inserted { out.append(c) }; if out.count == 4 { break } }
        return out
    }
    var body: some View {
        Group {
            if tiles.count == 4 {
                let h = size / 2
                VStack(spacing: 0) {
                    HStack(spacing: 0) { tile(tiles[0], h); tile(tiles[1], h) }
                    HStack(spacing: 0) { tile(tiles[2], h); tile(tiles[3], h) }
                }
            } else {
                CoverImage(url: library.coverArtURL(id: playlist.coverId ?? tiles.first, size: 600))
            }
        }
        .frame(width: size, height: size)
        .clipShape(RoundedRectangle(cornerRadius: corner, style: .continuous))
    }
    func tile(_ id: String, _ s: CGFloat) -> some View {
        CoverImage(url: library.coverArtURL(id: id, size: 300)).frame(width: s, height: s).clipped()
    }
}

struct PlaylistDetailView: View {
    let playlist: NDPlaylist
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @ObservedObject private var library = LibraryStore.shared
    @ObservedObject private var player = PlayerStore.shared
    @ObservedObject private var stars = StarStore.shared
    @State private var info: NDPlaylist?
    @State private var songs: [NDSong] = []
    @State private var loading = true
    @State private var error: String?
    @State private var pull: CGFloat = 0
    @State private var renaming = false
    @State private var editingDesc = false
    @State private var draft = ""
    @State private var confirmDelete = false

    private var shown: NDPlaylist { info ?? playlist }
    private var totalDuration: Int { shown.duration ?? songs.reduce(0) { $0 + ($1.duration ?? 0) } }
    private var bloomURL: URL? {
        library.coverArtURL(id: shown.coverId ?? songs.first.flatMap { $0.coverId ?? $0.albumId }, size: 300)
    }

    var body: some View {
        List {
            header
                .listRowInsets(EdgeInsets()).listRowSeparator(.hidden).listRowBackground(Color.clear)
            if loading && songs.isEmpty {
                ForEach(0..<6, id: \.self) { _ in SkeletonRow() }
                    .listRowInsets(EdgeInsets()).listRowSeparator(.hidden).listRowBackground(Color.clear)
            } else if let error, songs.isEmpty {
                EmptyState(kind: .network, action: ("重试", { Task { await load() } }))
                    .listRowSeparator(.hidden).listRowBackground(Color.clear)
            } else if songs.isEmpty {
                EmptyState(kind: .playlists).listRowSeparator(.hidden).listRowBackground(Color.clear)
            }
            ForEach(Array(songs.enumerated()), id: \.offset) { i, s in
                TrackRow(song: s, index: i + 1, title: s.title, subtitle: s.artist, trailing: s.durationLabel, playing: player.song?.id == s.id,
                         accessory: AnyView(StarButton(on: stars.isStarred(song: s.id), size: 15) { stars.toggle(s) })) {
                    PlayerStore.shared.play(songs: songs, startAt: i, coverURL: nil)
                    withAnimation(.ui) { state.nowPlaying = true }
                }
                .songMenu(s)
                .listRowInsets(EdgeInsets()).listRowSeparator(.hidden).listRowBackground(Color.clear)
            }
            .onDelete(perform: remove)
            .onMove(perform: move)
            Color.clear.frame(height: 170).listRowSeparator(.hidden).listRowBackground(Color.clear)
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .coordinateSpace(name: "plScroll")
        .background(alignment: .top) {
            ZStack(alignment: .top) {
                Theme.base(scheme)
                CoverBloom(url: bloomURL, height: 700, pull: pull)
            }.ignoresSafeArea()
        }
        .foregroundStyle(Theme.fg(scheme))
        .toolbar(.hidden, for: .navigationBar)
        .task(id: playlist.id) { await load() }
        .refreshable { await load() }
        .alert("重命名歌单", isPresented: $renaming) {
            TextField("歌单名称", text: $draft)
            Button("取消", role: .cancel) {}
            Button("保存") { let n = draft.trimmingCharacters(in: .whitespaces); if !n.isEmpty { apply { $0.name = n } remote: { try await NavidromeClient.shared.updatePlaylist(playlist.id, name: n) } } }
        }
        .alert("歌单描述", isPresented: $editingDesc) {
            TextField("写点什么…", text: $draft)
            Button("取消", role: .cancel) {}
            Button("保存") { let d = draft; apply { $0.comment = d } remote: { try await NavidromeClient.shared.updatePlaylist(playlist.id, comment: d) } }
        }
        .confirmationDialog("删除「\(shown.name)」？", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("删除歌单", role: .destructive) { Task { await deletePlaylist() } }
        } message: { Text("歌单会从服务器删除，歌曲本身不受影响。") }
    }

    var header: some View {
        VStack(spacing: 0) {
            Color.clear.frame(height: 0).background(
                GeometryReader { g in Color.clear.onChange(of: g.frame(in: .named("plScroll")).minY) { _, y in pull = y } }
            )
            HStack {
                Button { if !state.path.isEmpty { state.path.removeLast() } } label: { CircleIconButton(icon: "chevron.left") }
                Spacer()
                Menu {
                    Button { draft = shown.name; renaming = true } label: { Label("重命名", systemImage: "pencil") }
                    Button { draft = shown.comment ?? ""; editingDesc = true } label: { Label("编辑描述", systemImage: "text.alignleft") }
                    Button {
                        let v = !(shown.isPublic ?? false)
                        apply { $0.isPublic = v } remote: { try await NavidromeClient.shared.updatePlaylist(playlist.id, isPublic: v) }
                    } label: { Label((shown.isPublic ?? false) ? "设为私密" : "设为公开", systemImage: (shown.isPublic ?? false) ? "lock" : "globe") }
                    Divider()
                    Button(role: .destructive) { confirmDelete = true } label: { Label("删除歌单", systemImage: "trash") }
                } label: { CircleIconButton(icon: "ellipsis") }
            }
            .buttonStyle(.plain).padding(.horizontal, Theme.margin).padding(.top, 8)

            PlaylistCover(playlist: shown, songs: songs, size: 220)
                .shadow(color: .black.opacity(0.4), radius: 24, y: 12)
                .padding(.top, 12).motionIn(0.05, from: .scale)

            VStack(spacing: 4) {
                Text(shown.name).font(.system(size: 22, weight: .bold)).multilineTextAlignment(.center)
                if let o = shown.owner, !o.isEmpty { Text(o).font(.system(size: 16)).opacity(0.65) }
                Text(meta).font(.system(size: 13)).opacity(0.55)
                if let c = shown.comment, !c.isEmpty {
                    Text(c).font(.system(size: 13)).opacity(0.7).lineLimit(3).multilineTextAlignment(.center).padding(.top, 4)
                }
            }
            .padding(.top, 20).padding(.horizontal, Theme.margin).motionIn(0.12)

            HStack(spacing: 12) {
                Button { playAll(false) } label: { Label("播放全部", systemImage: "play.fill") }
                    .buttonStyle(PrimaryButtonStyle()).disabled(songs.isEmpty)
                Button { playAll(true) } label: { Label("随机播放", systemImage: "shuffle") }
                    .buttonStyle(SecondaryButtonStyle()).disabled(songs.isEmpty)
            }
            .padding(.horizontal, Theme.margin).padding(.top, 20).padding(.bottom, 12).motionIn(0.18)
        }
    }

    private var meta: String {
        var parts = ["歌单", "\(info?.songCount ?? songs.count) 首"]
        let d = totalDuration
        if d > 0 { parts.append(d >= 3600 ? "\(d / 3600) 小时 \(d % 3600 / 60) 分" : "\(d / 60) 分钟") }
        if shown.isPublic == true { parts.append("公开") }
        return parts.joined(separator: " · ")
    }

    private func load() async {
        loading = true; error = nil
        do {
            let (p, s) = try await NavidromeClient.shared.playlistDetail(id: playlist.id)
            info = p
            if s.map(\.id) != songs.map(\.id) { songs = s }
            PlaylistEvents.updated(p)
        } catch { self.error = error.localizedDescription }
        loading = false
    }

    private func playAll(_ shuffle: Bool) {
        guard !songs.isEmpty else { return }
        PlayerStore.shared.play(songs: shuffle ? songs.shuffled() : songs)
        withAnimation(.ui) { state.nowPlaying = true }
    }

    /// Optimistic metadata edit; reverts on failure.
    private func apply(_ change: (inout NDPlaylist) -> Void, remote: @escaping () async throws -> Void) {
        let before = shown
        var p = shown; change(&p); info = p
        PlaylistEvents.updated(p)
        Task {
            do { try await remote() }
            catch { info = before; PlaylistEvents.updated(before); SheetCenter.shared.say("保存失败") }
        }
    }

    private func remove(_ offsets: IndexSet) {
        let before = songs, beforeInfo = info
        songs.remove(atOffsets: offsets)
        var p = shown; p.songCount = songs.count; info = p
        PlaylistEvents.updated(p)
        Task {
            do { try await NavidromeClient.shared.updatePlaylist(playlist.id, removeIndexes: offsets.sorted()) }
            catch { songs = before; info = beforeInfo; SheetCenter.shared.say("移除失败") }
        }
    }

    private func move(_ from: IndexSet, _ to: Int) {
        let before = songs
        songs.move(fromOffsets: from, toOffset: to)
        let ids = songs.map(\.id)
        Task {
            do { try await NavidromeClient.shared.setPlaylistSongs(playlist.id, songIds: ids) }
            catch { songs = before; SheetCenter.shared.say("排序保存失败") }
        }
    }

    private func deletePlaylist() async {
        do {
            try await NavidromeClient.shared.deletePlaylist(playlist.id)
            PlaylistEvents.deleted(playlist.id)
            if !state.path.isEmpty { state.path.removeLast() }
            SheetCenter.shared.say("已删除歌单")
        } catch { SheetCenter.shared.say("删除失败") }
    }
}

/// Playlist changes broadcast to lists/caches (optimistic UI everywhere).
@MainActor
final class PlaylistEvents: ObservableObject {
    static let shared = PlaylistEvents()
    @Published var lists: [NDPlaylist]? = nil
    static func updated(_ p: NDPlaylist) {
        guard var l = shared.lists else { return }
        if let i = l.firstIndex(where: { $0.id == p.id }) { l[i] = p } else { l.insert(p, at: 0) }
        shared.lists = l
        HomeStore.shared.replacePlaylist(p)
    }
    static func deleted(_ id: String) {
        shared.lists?.removeAll { $0.id == id }
        HomeStore.shared.removePlaylist(id)
    }
    func load(force: Bool = false) async {
        if lists != nil && !force { return }
        if let l = try? await NavidromeClient.shared.playlists() { lists = l } else if lists == nil { lists = [] }
    }
}

/// Shimmering placeholder row (only shown when there is no cache).
struct SkeletonRow: View {
    @Environment(\.colorScheme) var scheme
    @State private var phase = false
    var body: some View {
        HStack(spacing: 14) {
            RoundedRectangle(cornerRadius: 8, style: .continuous).fill(Theme.fill(scheme)).frame(width: 44, height: 44)
            VStack(alignment: .leading, spacing: 7) {
                Capsule().fill(Theme.fill(scheme)).frame(width: 150, height: 10)
                Capsule().fill(Theme.fill(scheme)).frame(width: 90, height: 8)
            }
            Spacer()
        }
        .padding(.horizontal, Theme.margin).frame(height: 64)
        .opacity(phase ? 0.55 : 1)
        .onAppear { withAnimation(.easeInOut(duration: 0.9).repeatForever(autoreverses: true)) { phase = true } }
    }
}

#if DEBUG
/// Exercises the same client calls the playlist UI uses; creates and deletes a temporary playlist.
enum PlaylistSelfTest {
    @MainActor static func run() async {
        let c = NavidromeClient.shared, log = NetLog.logger
        func step(_ n: String, _ ok: Bool) { log.info("SELFTEST \(n, privacy: .public): \(ok ? "ok" : "FAIL", privacy: .public)") }
        guard let (_, base) = try? await c.playlistDetail(id: "QHV6UBvIqk4VEV8nocKPuh"), base.count >= 2 else { step("read 译梦测试歌单", false); return }
        step("read 译梦测试歌单 (\(base.count) songs)", true)
        guard let p = try? await c.createPlaylist(name: "译梦自测-临时", songIds: [base[0].id]) else { step("create", false); return }
        step("create", true)
        step("rename", (try? await c.updatePlaylist(p.id, name: "译梦自测-已重命名", comment: "自测描述", isPublic: false)) != nil)
        step("add", (try? await c.addToPlaylist(p.id, songIds: [base[1].id])) != nil)
        step("reorder", (try? await c.setPlaylistSongs(p.id, songIds: [base[1].id, base[0].id])) != nil)
        if let (info, s) = try? await c.playlistDetail(id: p.id) {
            step("verify name=\(info.name) order=\(s.map(\.id) == [base[1].id, base[0].id])", info.name == "译梦自测-已重命名" && s.map(\.id) == [base[1].id, base[0].id])
        } else { step("verify", false) }
        step("remove index 0", (try? await c.updatePlaylist(p.id, removeIndexes: [0])) != nil)
        if let (_, s) = try? await c.playlistDetail(id: p.id) { step("verify remove (\(s.count) left)", s.count == 1) }
        step("delete", (try? await c.deletePlaylist(p.id)) != nil)
        let still = (try? await c.playlists())?.contains { $0.id == p.id } ?? true
        step("verify deleted", !still)
    }
}
#else
enum PlaylistSelfTest { static func run() async {} }
#endif
