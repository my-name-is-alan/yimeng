import Foundation

/// Server push: WebSocket `/ws` (Bearer header), falling back to SSE `/api/v1/events`. Reconnects with backoff.
/// Events only trigger cheap refreshes of what's already on screen.
@MainActor
final class CoreEvents {
    static let shared = CoreEvents()
    private var task: Task<Void, Never>?
    private var ws: URLSessionWebSocketTask?

    func start() {
        stop()
        task = Task { [weak self] in
            var backoff: UInt64 = 2
            while !Task.isCancelled, NavidromeClient.core {
                let snap = await CoreClient.shared.snapshot()
                guard let base = snap.base, !snap.token.isEmpty else { return }
                let ok = await self?.runWS(base: base, token: snap.token) ?? false
                if !ok { _ = await self?.runSSE(base: base, token: snap.token) }
                try? await Task.sleep(nanoseconds: backoff * 1_000_000_000)
                backoff = min(backoff * 2, 60)
            }
        }
    }
    func stop() { task?.cancel(); task = nil; ws?.cancel(with: .goingAway, reason: nil); ws = nil }

    private func runWS(base: URL, token: String) async -> Bool {
        guard var c = URLComponents(url: base.appendingPathComponent("/ws"), resolvingAgainstBaseURL: false) else { return false }
        c.scheme = base.scheme == "https" ? "wss" : "ws"
        guard let u = c.url else { return false }
        var r = URLRequest(url: u); r.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        let t = URLSession.shared.webSocketTask(with: r); ws = t; t.resume()
        var got = false
        while !Task.isCancelled {
            guard let m = try? await t.receive() else { break }
            got = true
            switch m {
            case .string(let s): handle(Data(s.utf8))
            case .data(let d): handle(d)
            @unknown default: break
            }
        }
        return got
    }
    private func runSSE(base: URL, token: String) async -> Bool {
        var r = URLRequest(url: base.appendingPathComponent("/api/v1/events"))
        r.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization"); r.setValue("text/event-stream", forHTTPHeaderField: "Accept")
        r.timeoutInterval = 3600
        guard let (bytes, resp) = try? await URLSession.shared.bytes(for: r), (resp as? HTTPURLResponse)?.statusCode == 200 else { return false }
        var event = ""
        do {
            for try await line in bytes.lines {
                if line.hasPrefix("event:") { event = line.dropFirst(6).trimmingCharacters(in: .whitespaces) }
                else if line.hasPrefix("data:") {
                    let d = line.dropFirst(5).trimmingCharacters(in: .whitespaces)
                    handle(Data(d.utf8), fallbackType: event)
                }
            }
        } catch {}
        return true
    }
    /// `{"type":"favorites.changed", …}` (WS) or SSE `event:` + `data:`.
    private func handle(_ d: Data, fallbackType: String = "") {
        let o = (try? JSONSerialization.jsonObject(with: d)) as? [String: Any]
        let type = (o?["type"] as? String) ?? (o?["event"] as? String) ?? fallbackType
        switch type {
        case "favorites.changed": Task { await StarStore.shared.load() }
        case "playlists.changed": Task { await HomeStore.shared.refresh(force: true) }
        case "history.added": Task { await HomeStore.shared.refreshPlayed() }
        case "library.updated": Task { await HomeStore.shared.refresh(force: true); await LibraryCache.shared.ensure("songs", force: true) }
        case "progress.changed": break   // read on next play of that track
        default: break
        }
    }
}
