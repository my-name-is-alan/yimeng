import SwiftUI
import UIKit
import ImageIO

/// Loads Navidrome cover art with the required User-Agent (AsyncImage is 403'd by the CDN/WAF).
/// Lazy: the fetch starts when the view appears and is cancelled when it disappears (`.task`).
/// `points` (optional) rewrites the getCoverArt `size` to ~screen-scale × points, bucketed so caches hit.
struct CoverImage: View {
    let url: URL?
    var points: CGFloat? = nil
    var body: some View {
        if let u = url.map({ u in points.map { CoverCache.sized(u, points: $0) } ?? u }) {
            CoverImageInner(url: u).id(CoverCache.key(u))
        } else { Color.gray.opacity(0.2) }
    }
}

/// Memory hit → first frame shows the image (no flicker on re-appear). Otherwise a smaller/other-size
/// cached copy is shown as placeholder, disk is read + decoded off main, network last (limited, cancellable).
private struct CoverImageInner: View {
    let url: URL
    @State private var image: UIImage?
    @State private var stand: UIImage?
    @State private var failed = false
    init(url: URL) {
        self.url = url
        let m = CoverCache.shared.memory(url)
        _image = State(initialValue: m)
        _stand = State(initialValue: m == nil ? CoverCache.shared.memoryAnySize(url) : nil)
    }
    var body: some View {
        ZStack {
            Color.gray.opacity(failed ? 0.25 : 0.12)
            if image == nil, stand == nil, let h = CoreState.blurhash(for: url), let b = Blurhash.image(h) { Image(uiImage: b).resizable().scaledToFill() }
            if image == nil, let stand { Image(uiImage: stand).resizable().scaledToFill() }
            if let image { Image(uiImage: image).resizable().scaledToFill().transition(.opacity) }
        }
        .task(id: url.absoluteString) {
            guard image == nil else { return }
            if let d = await CoverCache.shared.disk(url) { image = d; return }   // local: no fade
            guard url.scheme != CoverCache.offlineScheme else { return }
            do {
                let img = try await CoverCache.shared.fetch(url)
                withAnimation(.easeOut(duration: 0.22)) { image = img }
            } catch is CancellationError {
            } catch { if !Task.isCancelled { failed = true } }
        }
    }
}

/// Counting semaphore with cancellation; LIFO so the most recently appeared rows load first.
final class AsyncLimiter: @unchecked Sendable {
    private let lock = NSLock()
    private var avail: Int
    private var waiters: [(UUID, CheckedContinuation<Bool, Never>)] = []
    init(_ n: Int) { avail = n }
    /// false = cancelled while waiting (no slot held).
    func acquire() async -> Bool {
        let id = UUID()
        return await withTaskCancellationHandler {
            await withCheckedContinuation { (c: CheckedContinuation<Bool, Never>) in
                lock.lock()
                if Task.isCancelled { lock.unlock(); c.resume(returning: false); return }
                if avail > 0 { avail -= 1; lock.unlock(); c.resume(returning: true) }
                else { waiters.append((id, c)); lock.unlock() }
            }
        } onCancel: {
            lock.lock()
            if let i = waiters.firstIndex(where: { $0.0 == id }) {
                let c = waiters.remove(at: i).1; lock.unlock(); c.resume(returning: false)
            } else { lock.unlock() }
        }
    }
    func release() {
        lock.lock()
        if !waiters.isEmpty { let c = waiters.removeLast().1; lock.unlock(); c.resume(returning: true) }
        else { avail += 1; lock.unlock() }
    }
}

/// Cover cache keyed by Subsonic id+size (URLs carry a per-login token, so never key by full URL).
/// Memory (NSCache, cost = decoded bytes) + disk (Caches/covers, original bytes). Decode = ImageIO downsample, off main.
final class CoverCache: @unchecked Sendable {
    static let shared = CoverCache()
    static let offlineScheme = "yimeng-cover"
    static let buckets = [64, 128, 192, 256, 300, 384, 512, 600, 768, 800, 1024]
    private let mem: NSCache<NSString, UIImage> = {
        let c = NSCache<NSString, UIImage>(); c.totalCostLimit = 96 * 1024 * 1024; return c
    }()
    private let limiter = AsyncLimiter(6)
    /// Per-source namespace (ids are only unique within one server).
    private var dir: URL {
        let d = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0].appendingPathComponent("covers", isDirectory: true)
            .appendingPathComponent(MusicSources.activeId, isDirectory: true)
        try? FileManager.default.createDirectory(at: d, withIntermediateDirectories: true)
        return d
    }
    func resetMemory() { mem.removeAllObjects() }
    private let session: URLSession = {
        let c = URLSessionConfiguration.default
        c.httpAdditionalHeaders = ["User-Agent": "Yimeng/0.3 (iOS; art.yimeng.app)", "Accept": "image/*"]
        c.timeoutIntervalForRequest = 30
        c.httpMaximumConnectionsPerHost = 6
        c.urlCache = nil   // we keep our own disk cache
        return URLSession(configuration: c)
    }()

    /// Rewrite `size=` to the bucket ≥ points × screen scale (≈3× on iPhone).
    static func sized(_ url: URL, points: CGFloat) -> URL {
        let want = Int((points * min(3, UIScreen.main.scale)).rounded(.up))
        let px = buckets.first { $0 >= want } ?? buckets.last!
        if url.query?.contains("sig=") == true { return url }   // 译梦 Core pre-signed: size may be part of the signature
        guard var c = URLComponents(url: url, resolvingAgainstBaseURL: false) else { return url }
        var q = c.queryItems ?? []
        q.removeAll { $0.name == "size" }; q.append(URLQueryItem(name: "size", value: "\(px)"))
        c.queryItems = q
        return c.url ?? url
    }

    private static func parts(_ url: URL) -> (id: String, size: Int)? {
        let q = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        if url.path.contains("/api/covers/") {   // 译梦 Core: id is in the path; sig/exp must not split the cache
            return (CoreState.coverKey(url.absoluteString), Int(q.first(where: { $0.name == "size" })?.value ?? "0") ?? 0)
        }
        guard let id = q.first(where: { $0.name == "id" })?.value else { return nil }
        return (id, Int(q.first(where: { $0.name == "size" })?.value ?? "0") ?? 0)
    }
    static func key(_ url: URL) -> String {
        if let p = parts(url) { return p.id + "_" + "\(p.size)" }
        return url.absoluteString
    }
    private func file(_ k: String) -> URL {
        dir.appendingPathComponent(k.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? k)
    }
    private func put(_ img: UIImage, _ k: String) {
        let cost = Int(img.size.width * img.size.height * img.scale * img.scale * 4)
        mem.setObject(img, forKey: k as NSString, cost: cost)
    }

    func memory(_ url: URL) -> UIImage? { mem.object(forKey: Self.key(url) as NSString) }

    /// Any in-memory size of the same cover (largest first) — a no-flicker stand-in while the right size loads.
    func memoryAnySize(_ url: URL) -> UIImage? {
        guard let p = Self.parts(url) else { return nil }
        for s in Self.buckets.reversed() where s != p.size {
            if let m = mem.object(forKey: "\(p.id)_\(s)" as NSString) { return m }
        }
        return nil
    }

    /// Sync memory → disk (used by backdrops that need a value during body evaluation).
    func cached(_ url: URL) -> UIImage? {
        let k = Self.key(url)
        if let m = mem.object(forKey: k as NSString) { return m }
        if let d = try? Data(contentsOf: file(k)), let img = Self.decode(d, maxPixel: Self.parts(url)?.size ?? 0) {
            put(img, k); return img
        }
        return nil
    }

    /// Async disk read + downsampled decode, off the main thread.
    func disk(_ url: URL) async -> UIImage? {
        let k = Self.key(url)
        if let m = mem.object(forKey: k as NSString) { return m }
        let f = file(k), px = Self.parts(url)?.size ?? 0
        let img = await Task.detached(priority: .userInitiated) { () -> UIImage? in
            guard let d = try? Data(contentsOf: f) else { return nil }
            return Self.decode(d, maxPixel: px)
        }.value
        if let img { put(img, k) }
        return img
    }

    func fetch(_ url: URL) async throws -> UIImage {
        if let c = await disk(url) { return c }
        guard await limiter.acquire() else { throw CancellationError() }
        defer { limiter.release() }
        try Task.checkCancellation()
        var req = URLRequest(url: url)
        for (k, v) in CoreState.mediaHeaders(for: url) { req.setValue(v, forHTTPHeaderField: k) }
        let (data, resp) = try await session.data(for: req)
        let k = Self.key(url), px = Self.parts(url)?.size ?? 0
        guard (resp as? HTTPURLResponse)?.statusCode == 200 else { throw URLError(.badServerResponse) }
        let img = await Task.detached(priority: .userInitiated) { Self.decode(data, maxPixel: px) }.value
        guard let img else { throw URLError(.cannotDecodeContentData) }
        put(img, k)
        let f = file(k)
        Task.detached(priority: .utility) { try? data.write(to: f, options: .atomic) }
        return img
    }

    /// ImageIO thumbnail decode: never inflates a full-size bitmap, forces decode now (not at first draw on main).
    static func decode(_ data: Data, maxPixel: Int) -> UIImage? {
        guard let src = CGImageSourceCreateWithData(data as CFData, [kCGImageSourceShouldCache: false] as CFDictionary) else { return nil }
        var opts: [CFString: Any] = [kCGImageSourceCreateThumbnailFromImageAlways: true,
                                     kCGImageSourceCreateThumbnailWithTransform: true,
                                     kCGImageSourceShouldCacheImmediately: true]
        if maxPixel > 0 { opts[kCGImageSourceThumbnailMaxPixelSize] = maxPixel }
        guard let cg = CGImageSourceCreateThumbnailAtIndex(src, 0, opts as CFDictionary) else { return UIImage(data: data) }
        return UIImage(cgImage: cg)
    }

    func clear() {
        mem.removeAllObjects()
        try? FileManager.default.removeItem(at: dir.deletingLastPathComponent())
    }
}

import CoreImage

/// Diffused (downsampled + blurred + saturated) versions of remote covers for backdrops and glows.
@MainActor
final class RemoteDiffusion: ObservableObject {
    static let shared = RemoteDiffusion()
    struct Entry { let image: UIImage; let color: Color }
    /// Not @Published: lookups may fill it synchronously during body evaluation. `rev` signals async arrivals.
    private var store: [String: Entry] = [:]
    @Published private(set) var rev = 0
    private var inflight: Set<String> = []
    func reset() { store = [:]; rev += 1 }
    private static let ctx = CIContext()

    /// Sync: memory → disk-cached cover (diffused once, then memoised).
    func entry(_ url: URL?) -> Entry? {
        guard let url else { return nil }
        let k = CoverCache.key(url)
        if let e = store[k] { return e }
        if let img = CoverCache.shared.cached(url), let e = Self.diffuse(img) { store[k] = e; return e }
        return nil
    }

    func load(_ url: URL?) {
        guard let url, entry(url) == nil else { return }
        let k = CoverCache.key(url)
        guard !inflight.contains(k) else { return }
        inflight.insert(k)
        Task {
            defer { inflight.remove(k) }
            guard let src = try? await CoverCache.shared.fetch(url) else { return }
            let e = await Task.detached { Self.diffuse(src) }.value
            if let e { store[k] = e; rev += 1 }
        }
    }

    nonisolated static func diffuse(_ src: UIImage) -> Entry? {
        guard let cg = src.cgImage else { return nil }
        let ci = CIImage(cgImage: cg)
        let scale = 96 / max(ci.extent.width, ci.extent.height)
        let small = ci.transformed(by: .init(scaleX: scale, y: scale))
        let sat = small.applyingFilter("CIColorControls", parameters: [kCIInputSaturationKey: 1.8])
        let blur = sat.clampedToExtent().applyingFilter("CIGaussianBlur", parameters: [kCIInputRadiusKey: 10]).cropped(to: small.extent)
        let avg = small.applyingFilter("CIAreaAverage", parameters: [kCIInputExtentKey: CIVector(cgRect: small.extent)])
        var px = [UInt8](repeating: 0, count: 4)
        ctx.render(avg, toBitmap: &px, rowBytes: 4, bounds: CGRect(x: 0, y: 0, width: 1, height: 1), format: .RGBA8, colorSpace: CGColorSpaceCreateDeviceRGB())
        guard let out = ctx.createCGImage(blur, from: small.extent) else { return nil }
        return Entry(image: UIImage(cgImage: out), color: Color(red: Double(px[0]) / 255, green: Double(px[1]) / 255, blue: Double(px[2]) / 255))
    }
}

/// Remote-cover version of DiffusedBackdrop: each URL contributes by circular weight of `progress`.
struct RemoteBackdrop: View {
    let urls: [URL?]
    var progress: Double = 0
    var height: CGFloat = 640
    @Environment(\.colorScheme) var scheme
    @ObservedObject private var diff = RemoteDiffusion.shared
    var body: some View {
        GeometryReader { geo in
            ZStack {
                Theme.base(scheme)
                ForEach(Array(urls.enumerated()), id: \.offset) { i, u in
                    let w = DiffusedBackdrop.weight(i, progress, urls.count)
                    if w > 0.001, let e = diff.entry(u) {
                        ZStack {
                            e.color
                            Image(uiImage: e.image).resizable().interpolation(.medium).scaledToFill()
                                .frame(width: geo.size.width, height: geo.size.width)
                                .scaleEffect(2.6)
                                .position(x: geo.size.width / 2, y: height * 0.4)
                        }.opacity(w).transition(.opacity)
                    }
                }
                LinearGradient(stops: [.init(color: Theme.base(scheme).opacity(0), location: 0),
                                       .init(color: Theme.base(scheme).opacity(0.13), location: 0.25),
                                       .init(color: Theme.base(scheme).opacity(0.67), location: 0.62),
                                       .init(color: Theme.base(scheme), location: 1)], startPoint: .top, endPoint: .bottom)
            }
            .frame(width: geo.size.width, height: height).clipped()
            .drawingGroup()
        }
        .frame(height: height).allowsHitTesting(false)
        .animation(.easeOut(duration: 0.7), value: diff.rev)
        .task(id: urls.compactMap { $0?.absoluteString }.joined()) { for u in urls { diff.load(u) } }
    }
}
