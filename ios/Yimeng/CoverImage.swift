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
