import SwiftUI
import Network
import AVFoundation

/// Phase E: on-disk audio cache. Files are the untouched original stream bytes (same URL the player uses),
/// so cached playback is still bit-exact passthrough when effects are off.
/// Lives in Application Support (not Caches) so iOS doesn't purge it; we enforce our own limit with LRU-by-score eviction.
@MainActor
final class OfflineCache: ObservableObject {
    static let shared = OfflineCache()

    struct Entry: Codable, Equatable { var id: String; var file: String; var bytes: Int64; var lastUsed: Date; var plays: Int; var pinnedDaily: Bool }

    @AppStorage("ym.offline.on") var enabled = true
    @AppStorage("ym.offline.wifiOnly") var wifiOnly = true
    @AppStorage("ym.offline.limitMB") var limitMB = 2048
    @Published private(set) var entries: [String: Entry] = [:]
    @Published private(set) var busy: Set<String> = []
    @Published private(set) var onWiFi = false
    @Published private(set) var online = true

    private let dir: URL
    private let monitor = NWPathMonitor()
    private var plays: [String: Int] = UserDefaults.standard.dictionary(forKey: "ym.offline.plays") as? [String: Int] ?? [:]

    init() {
        dir = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0].appendingPathComponent("OfflineAudio", isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        var u = dir; var rv = URLResourceValues(); rv.isExcludedFromBackup = true; try? u.setResourceValues(rv)
        if let d = try? Data(contentsOf: dir.appendingPathComponent("index.json")), let e = try? JSONDecoder().decode([String: Entry].self, from: d) {
            entries = e.filter { FileManager.default.fileExists(atPath: dir.appendingPathComponent($0.value.file).path) }
        }
        monitor.pathUpdateHandler = { p in
            let wifi = p.status == .satisfied && !p.isExpensive && !p.isConstrained, up = p.status == .satisfied
            Task { @MainActor in OfflineCache.shared.onWiFi = wifi; OfflineCache.shared.online = up; if wifi { OfflineCache.shared.precache() } }
        }
        monitor.start(queue: .global(qos: .utility))
    }

    var usedBytes: Int64 { entries.values.reduce(0) { $0 + $1.bytes } }
    var limitBytes: Int64 { Int64(limitMB) * 1_048_576 }
    var mayDownload: Bool { enabled && limitMB > 0 && (onWiFi || (!wifiOnly && online)) }

    /// Local file for a track, if cached. Touches LRU.
    func file(for id: String) -> URL? {
        guard var e = entries[id] else { return nil }
        let u = dir.appendingPathComponent(e.file)
        guard FileManager.default.fileExists(atPath: u.path) else { entries[id] = nil; persist(); return nil }
        e.lastUsed = .now; entries[id] = e; persist()
        return u
    }
    func isCached(_ id: String) -> Bool { entries[id] != nil }

    /// Called whenever a track starts: counts plays (frequency signal) and caches it in the background
    /// while AVPlayer keeps streaming the same URL (separate download, playback never waits on it).
    func played(_ s: NDSong) {
        plays[s.id, default: 0] += 1
        UserDefaults.standard.set(plays, forKey: "ym.offline.plays")
        if var e = entries[s.id] { e.plays = plays[s.id] ?? e.plays; entries[s.id] = e; persist() }
        else if (plays[s.id] ?? 0) >= 2 { fetch(s, daily: false) }   // replayed ⇒ worth keeping
    }

    /// Smart pre-cache: today's daily picks + most-played history, within the limit, Wi-Fi only by default.
    func precache() {
        guard mayDownload else { return }
        var want: [(NDSong, Bool)] = []
        if NavidromeClient.core { for d in PlatformStore.shared.dailies { want += d.tracks.prefix(20).filter { !$0.isUnplayable }.map { ($0, true) } } }
        let hist = PlayerStore.shared.history
        let top = hist.filter { (plays[$0.id] ?? 0) >= 2 }.sorted { (plays[$0.id] ?? 0) > (plays[$1.id] ?? 0) }.prefix(30)
        want += top.map { ($0, false) }
        for (s, d) in want where entries[s.id] == nil { fetch(s, daily: d) }
    }

    func fetch(_ s: NDSong, daily: Bool) {
        guard mayDownload, entries[s.id] == nil, !busy.contains(s.id), busy.count < 3,
              let url = LibraryStore.shared.streamURL(for: s.id) else { return }
        busy.insert(s.id)
        var req = URLRequest(url: url)
        for (k, v) in PlayerStore.mediaHeaders(for: url) { req.setValue(v, forHTTPHeaderField: k) }
        let id = s.id, d = dir
        Task.detached(priority: .utility) {
            defer { Task { @MainActor in OfflineCache.shared.busy.remove(id) } }
            guard let (tmp, resp) = try? await URLSession.shared.download(for: req),
                  let h = resp as? HTTPURLResponse, (200..<300).contains(h.statusCode) else { return }
            let ext = OfflineCache.ext(mime: h.mimeType, suggested: h.suggestedFilename)
            let name = OfflineCache.safe(id) + "." + ext
            let dst = d.appendingPathComponent(name)
            try? FileManager.default.removeItem(at: dst)
            guard (try? FileManager.default.moveItem(at: tmp, to: dst)) != nil else { return }
            let size = (try? dst.resourceValues(forKeys: [.fileSizeKey]).fileSize).flatMap { Int64($0 ?? 0) } ?? 0
            await MainActor.run { OfflineCache.shared.add(Entry(id: id, file: name, bytes: size, lastUsed: .now, plays: 0, pinnedDaily: daily)) }
        }
    }

    private func add(_ e: Entry) {
        var e = e; e.plays = plays[e.id] ?? 0
        entries[e.id] = e
        for id in Self.evict(Array(entries.values), limit: limitBytes, keep: e.id) { remove(id, save: false) }
        persist()
    }
    func remove(_ id: String, save: Bool = true) {
        if let e = entries[id] { try? FileManager.default.removeItem(at: dir.appendingPathComponent(e.file)) }
        entries[id] = nil; if save { persist() }
    }
    func clearAll() { for id in entries.keys { remove(id, save: false) }; persist() }
    func enforceLimit() { for id in Self.evict(Array(entries.values), limit: limitBytes, keep: nil) { remove(id, save: false) }; persist() }

    private func persist() { if let d = try? JSONEncoder().encode(entries) { try? d.write(to: dir.appendingPathComponent("index.json"), options: .atomic) } }

    /// Which ids to delete so total ≤ limit. Lowest value first: never-replayed before replayed, older before newer;
    /// today's dailies count as one extra play. Pure for unit tests.
    nonisolated static func evict(_ es: [Entry], limit: Int64, keep: String?) -> [String] {
        var total = es.reduce(0) { $0 + $1.bytes }
        guard total > limit else { return [] }
        let order = es.filter { $0.id != keep }.sorted {
            let a = $0.plays + ($0.pinnedDaily ? 1 : 0), b = $1.plays + ($1.pinnedDaily ? 1 : 0)
            return a != b ? a < b : $0.lastUsed < $1.lastUsed
        }
        var out: [String] = []
        for e in order where total > limit { out.append(e.id); total -= e.bytes }
        return out
    }
    nonisolated static func safe(_ id: String) -> String {
        let h = id.utf8.reduce(UInt64(5381)) { ($0 << 5) &+ $0 &+ UInt64($1) }
        return String(id.prefix(40).map { $0.isASCII && ($0.isLetter || $0.isNumber) ? $0 : "_" }) + "-" + String(h, radix: 36)
    }
    nonisolated static func ext(mime: String?, suggested: String?) -> String {
        if let s = suggested, let e = s.split(separator: ".").last, s.contains("."), e.count <= 5 { return e.lowercased() }
        switch (mime ?? "").lowercased() {
        case let m where m.contains("flac"): return "flac"
        case let m where m.contains("mp4") || m.contains("m4a") || m.contains("aac") || m.contains("alac"): return "m4a"
        case let m where m.contains("wav"): return "wav"
        case let m where m.contains("ogg") || m.contains("opus"): return "ogg"
        case let m where m.contains("eac3") || m.contains("ec-3"): return "ec3"
        default: return "mp3"
        }
    }
}

struct OfflineSettingsView: View {
    var back = false
    @ObservedObject var c = OfflineCache.shared
    @Environment(\.colorScheme) var scheme
    @State private var confirmClear = false
    private let limits = [512, 1024, 2048, 4096, 8192, 16384]
    private func gb(_ mb: Int) -> String { mb >= 1024 ? "\(mb / 1024) GB" : "\(mb) MB" }
    var body: some View {
        YMPage("离线缓存", back: back) {
            // Doodle storage ring
            HStack(spacing: 22) {
                ZStack {
                    YMDoodleRing(fraction: Double(c.usedBytes) / Double(max(c.limitBytes, 1)))
                    VStack(spacing: 2) {
                        Text("\(Int((Double(c.usedBytes) / Double(max(c.limitBytes, 1)) * 100).rounded()))%").font(.system(size: 26, weight: .bold, design: .rounded))
                        Text("已用").font(.system(size: 12)).foregroundStyle(Theme.secondary(scheme))
                    }
                }
                VStack(alignment: .leading, spacing: 6) {
                    Text(ByteCountFormatter.string(fromByteCount: c.usedBytes, countStyle: .file)).font(.system(size: 22, weight: .bold))
                    Text("共 \(gb(c.limitMB)) · \(c.entries.count) 首").font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme))
                    HStack(spacing: 6) {
                        Circle().fill(c.onWiFi ? Theme.accent(scheme) : Theme.secondary(scheme)).frame(width: 6, height: 6)
                        Text(c.busy.isEmpty ? (c.onWiFi ? "Wi-Fi" : c.online ? "蜂窝网络" : "离线") : "正在缓存 \(c.busy.count) 首")
                            .font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme))
                    }
                }
                Spacer(minLength: 0)
            }
            .padding(18).ymCard(padding: 0).padding(.top, 12)
            .accessibilityIdentifier("offline.settings")

            YMSection(title: "下载", footer: "自动缓存今日每日推荐和常听的歌，断网时直接从缓存播放。缓存是原始文件，不转码。") {
                YMToggleRow(icon: "sparkles", "智能预缓存", isOn: $c.enabled)
                YMDivider()
                YMToggleRow(icon: "wifi", "仅 Wi-Fi 下载", isOn: $c.wifiOnly)
                YMDivider()
                YMPickerRow(icon: "internaldrive", "存储上限", selection: $c.limitMB, options: limits.map { ($0, gb($0)) })
            }
            HStack(spacing: 12) {
                Button("立即缓存") { c.precache(); Haptics.tap() }.buttonStyle(PrimaryButtonStyle()).disabled(!c.mayDownload).opacity(c.mayDownload ? 1 : 0.4)
                Button("清空缓存") { confirmClear = true }.buttonStyle(YMDestructiveButtonStyle()).disabled(c.entries.isEmpty).opacity(c.entries.isEmpty ? 0.4 : 1)
            }.padding(.top, 24)
        }
        .onChange(of: c.limitMB) { c.enforceLimit() }
        .confirmationDialog("清空离线缓存？", isPresented: $confirmClear, titleVisibility: .visible) {
            Button("清空", role: .destructive) { c.clearAll() }
        } message: { Text("已缓存的 \(c.entries.count) 首会被删除，需要时会重新下载。") }
    }
}
