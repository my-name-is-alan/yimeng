import SwiftUI
import UIKit

/// Loads Navidrome cover art with the required User-Agent (AsyncImage is 403'd by the CDN/WAF).
struct CoverImage: View {
    let url: URL?
    var body: some View {
        if let url { CoverImageInner(url: url).id(CoverCache.key(url)) } else { Color.gray.opacity(0.2) }
    }
}

/// Starts from the synchronous memory/disk cache so re-created views never flash a placeholder.
private struct CoverImageInner: View {
    let url: URL
    @State private var image: UIImage?
    @State private var failed = false
    init(url: URL) {
        self.url = url
        _image = State(initialValue: CoverCache.shared.cached(url))
    }
    var body: some View {
        ZStack {
            Color.gray.opacity(failed ? 0.25 : 0.12)
            if let image { Image(uiImage: image).resizable().scaledToFill().transition(.opacity) }
        }
        .task(id: url.absoluteString) {
            guard image == nil, url.scheme != CoverCache.offlineScheme else { return }
            if let img = try? await CoverCache.shared.fetch(url) {
                withAnimation(.easeOut(duration: 0.25)) { image = img }
            } else { failed = true }
        }
    }
}

/// Cover cache keyed by Subsonic id+size (URLs carry a per-login token, so never key by full URL).
/// Memory (NSCache) + disk (Caches/covers). Thread-safe; sync lookup for first frame.
final class CoverCache: @unchecked Sendable {
    static let shared = CoverCache()
    static let offlineScheme = "yimeng-cover"
    private let mem = NSCache<NSString, UIImage>()
    private let dir: URL = {
        let d = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0].appendingPathComponent("covers", isDirectory: true)
        try? FileManager.default.createDirectory(at: d, withIntermediateDirectories: true)
        return d
    }()
    private let session: URLSession = {
        let c = URLSessionConfiguration.default
        c.httpAdditionalHeaders = ["User-Agent": "Yimeng/0.3 (iOS; art.yimeng.app)", "Accept": "image/*"]
        c.timeoutIntervalForRequest = 30
        return URLSession(configuration: c)
    }()

    static func key(_ url: URL) -> String {
        let q = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        if let id = q.first(where: { $0.name == "id" })?.value {
            return id + "_" + (q.first(where: { $0.name == "size" })?.value ?? "0")
        }
        return url.absoluteString
    }
    private func file(_ k: String) -> URL {
        dir.appendingPathComponent(k.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? k)
    }

    func cached(_ url: URL) -> UIImage? {
        let k = Self.key(url)
        if let m = mem.object(forKey: k as NSString) { return m }
        if let d = try? Data(contentsOf: file(k)), let img = UIImage(data: d) {
            mem.setObject(img, forKey: k as NSString); return img
        }
        return nil
    }

    func fetch(_ url: URL) async throws -> UIImage {
        if let c = cached(url) { return c }
        let (data, resp) = try await session.data(from: url)
        guard (resp as? HTTPURLResponse)?.statusCode == 200, let img = UIImage(data: data) else { throw URLError(.badServerResponse) }
        let k = Self.key(url)
        mem.setObject(img, forKey: k as NSString)
        try? data.write(to: file(k))
        return img
    }

    func clear() {
        mem.removeAllObjects()
        try? FileManager.default.removeItem(at: dir)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
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
                        }.opacity(w)
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

    }
}
