import SwiftUI
import UIKit

/// Loads Navidrome cover art with the required User-Agent (AsyncImage is 403'd by the CDN/WAF).
struct CoverImage: View {
    let url: URL?
    var body: some View {
        Group {
            if let url {
                CoverImageInner(url: url)
            } else {
                Color.gray.opacity(0.2)
            }
        }
    }
}

private struct CoverImageInner: View {
    let url: URL
    @State private var image: UIImage?
    @State private var failed = false

    var body: some View {
        Group {
            if let image {
                Image(uiImage: image).resizable().scaledToFill()
            } else if failed {
                Color.gray.opacity(0.25)
            } else {
                Color.gray.opacity(0.15).overlay(ProgressView().scaleEffect(0.8))
            }
        }
        .task(id: url) { await load() }
    }

    private func load() async {
        image = nil
        failed = false
        if let cached = CoverCache.shared.image(for: url) {
            image = cached
            return
        }
        do {
            let img = try await CoverCache.shared.fetch(url)
            image = img
        } catch {
            failed = true
        }
    }
}

actor CoverCache {
    static let shared = CoverCache()
    private var mem: [URL: UIImage] = [:]
    private let session: URLSession = {
        let c = URLSessionConfiguration.ephemeral
        c.httpAdditionalHeaders = [
            "User-Agent": "Yimeng/0.3 (iOS; art.yimeng.app)",
            "Accept": "image/*",
        ]
        c.timeoutIntervalForRequest = 20
        return URLSession(configuration: c)
    }()

    func image(for url: URL) -> UIImage? { mem[url] }
    func clear() { mem.removeAll() }

    func fetch(_ url: URL) async throws -> UIImage {
        if let m = mem[url] { return m }
        let (data, resp) = try await session.data(from: url)
        let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
        guard code == 200, let img = UIImage(data: data) else {
            throw URLError(.badServerResponse)
        }
        mem[url] = img
        return img
    }
}

import CoreImage

/// Diffused (downsampled + blurred + saturated) versions of remote covers for backdrops and glows.
@MainActor
final class RemoteDiffusion: ObservableObject {
    static let shared = RemoteDiffusion()
    struct Entry { let image: UIImage; let color: Color }
    @Published private(set) var entries: [URL: Entry] = [:]
    private var inflight: Set<URL> = []
    private static let ctx = CIContext()

    func entry(_ url: URL?) -> Entry? { url.flatMap { entries[$0] } }

    func load(_ url: URL?) {
        guard let url, entries[url] == nil, !inflight.contains(url) else { return }
        inflight.insert(url)
        Task {
            defer { inflight.remove(url) }
            guard let src = try? await CoverCache.shared.fetch(url) else { return }
            let e = await Task.detached { Self.diffuse(src) }.value
            if let e { entries[url] = e }
        }
    }

    nonisolated private static func diffuse(_ src: UIImage) -> Entry? {
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
        .onAppear { urls.forEach { diff.load($0) } }
        .onChange(of: urls) { _, n in n.forEach { diff.load($0) } }
    }
}
