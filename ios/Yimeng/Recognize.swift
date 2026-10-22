import SwiftUI
import AVFoundation

// MARK: - F: 听歌识曲 through Core (POST /api/v1/recognize, multipart). Entry hidden unless Core is the source.

struct CoreRecognizeMatch: Codable, Hashable, Sendable, Identifiable {
    var title: String; var artist: String?; var album: String?; var cover: String?
    var origin: String?; var trackId: String?; var offsetMs: Int?
    var id: String { (trackId ?? "") + "|" + title + "|" + (artist ?? "") }
    /// Playable row when Core resolved it to a track id.
    var song: NDSong? {
        guard let t = trackId, !t.isEmpty else { return nil }
        var s = NDSong(id: t, title: title, artist: artist ?? "", album: album, albumId: nil, coverId: cover, duration: nil, track: nil)
        s.origin = origin
        return s
    }
}
struct CoreRecognizeResult: Codable, Sendable { var matches: [CoreRecognizeMatch] }

enum RecognizeFailure: Equatable {
    case notLoggedIn, noMatch, tooLong, notSupported, micDenied, other(String)
    var text: String {
        switch self {
        case .notLoggedIn: "识曲需要先在译梦服务端登录网易云账号"
        case .noMatch: "没听出来，靠近声源再试一次"
        case .tooLong: "录音太长了（最多 15 秒）"
        case .notSupported: "当前服务端还不支持识曲"
        case .micDenied: "需要麦克风权限才能识曲（可在「设置」中开启）"
        case .other(let s): s
        }
    }
    static func from(_ e: Error) -> RecognizeFailure {
        if let c = e as? CoreAPIError {
            switch c.code {
            case "not_logged_in": return .notLoggedIn
            case "no_match": return .noMatch
            case "too_long", "payload_too_large": return .tooLong
            case "not_found", "not_supported": return .notSupported
            default: return .other(c.message.isEmpty ? c.code : c.message)
            }
        }
        if case NavidromeError.api(let m) = e, m.contains("找不到") { return .notSupported }
        return .other(e.localizedDescription)
    }
}

enum Multipart {
    /// One file part named `audio` + small text fields. Pure for unit tests.
    static func body(boundary b: String, file: Data, filename: String, mime: String, fields: [(String, String)]) -> Data {
        var d = Data()
        func s(_ x: String) { d.append(Data(x.utf8)) }
        for (k, v) in fields { s("--\(b)\r\nContent-Disposition: form-data; name=\"\(k)\"\r\n\r\n\(v)\r\n") }
        s("--\(b)\r\nContent-Disposition: form-data; name=\"audio\"; filename=\"\(filename)\"\r\nContent-Type: \(mime)\r\n\r\n")
        d.append(file); s("\r\n--\(b)--\r\n")
        return d
    }
}

@MainActor
final class RecognizeStore: ObservableObject {
    enum Phase: Equatable { case idle, listening(Double), matching, done([CoreRecognizeMatch]), failed(RecognizeFailure) }
    static let clipSeconds = 10.0
    @Published var phase: Phase = .idle
    private var recorder: AVAudioRecorder?
    private var task: Task<Void, Never>?
    private var resumeAfter = false

    func start() {
        guard NavidromeClient.core else { phase = .failed(.notSupported); return }
        task?.cancel()
        task = Task { await run() }
    }
    func cancel() { task?.cancel(); recorder?.stop(); recorder = nil; restore(); phase = .idle }

    private func run() async {
        guard await AVAudioApplication.requestRecordPermission() else { phase = .failed(.micDenied); return }
        // Our own playback would be "heard" — pause it while listening, resume after.
        resumeAfter = PlayerStore.shared.isPlaying
        if resumeAfter { PlayerStore.shared.toggle() }
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("ym-recognize.m4a")
        try? FileManager.default.removeItem(at: url)
        do {
            let ses = AVAudioSession.sharedInstance()
            try ses.setCategory(.playAndRecord, mode: .measurement, options: [.defaultToSpeaker, .allowBluetoothHFP])
            try ses.setActive(true)
            let r = try AVAudioRecorder(url: url, settings: [AVFormatIDKey: kAudioFormatMPEG4AAC, AVSampleRateKey: 44_100,
                                                             AVNumberOfChannelsKey: 1, AVEncoderBitRateKey: 96_000])
            recorder = r
            guard r.record(forDuration: Self.clipSeconds) else { throw NavidromeError.api("无法开始录音") }
            let t0 = Date()
            while r.isRecording, !Task.isCancelled {
                phase = .listening(min(1, Date().timeIntervalSince(t0) / Self.clipSeconds))
                try? await Task.sleep(for: .milliseconds(100))
            }
            r.stop(); recorder = nil
            restore()
            guard !Task.isCancelled else { return }
            phase = .matching
            let data = try Data(contentsOf: url)
            let res = try await CoreClient.shared.recognize(data, filename: "clip.m4a", mime: "audio/mp4", durationMs: Int(Self.clipSeconds * 1000))
            if res.matches.isEmpty { phase = .failed(.noMatch); Haptics.tick() } else { phase = .done(res.matches); Haptics.success() }
        } catch {
            recorder?.stop(); recorder = nil; restore()
            if !Task.isCancelled { phase = .failed(.from(error)) }
        }
    }
    private func restore() {
        AudioRoute.configureSession()
        if resumeAfter { resumeAfter = false; if !PlayerStore.shared.isPlaying { PlayerStore.shared.toggle() } }
    }
}

/// Sheet opened only by tapping the 识曲 button (never automatically).
struct RecognizeSheet: View {
    @StateObject private var store = RecognizeStore()
    @Environment(\.dismiss) private var dismiss
    @Environment(\.colorScheme) private var scheme
    private var lime: Color { Theme.accent(scheme) }
    var body: some View {
        VStack(spacing: 22) {
            Text("听歌识曲").font(.system(size: 17, weight: .semibold)).padding(.top, 18)
            ZStack {
                // Doodle ring (decorative): the only thing that animates with the listening progress.
                Circle().strokeBorder(lime.opacity(0.25), style: StrokeStyle(lineWidth: 2, dash: [4, 6])).frame(width: 168, height: 168)
                Circle().trim(from: 0, to: progress).stroke(lime, style: StrokeStyle(lineWidth: 4, lineCap: .round))
                    .rotationEffect(.degrees(-90)).frame(width: 150, height: 150)
                Button { if case .listening = store.phase { store.cancel() } else { store.start() } } label: {
                    Image(systemName: icon).font(.system(size: 40, weight: .semibold)).foregroundStyle(scheme == .dark ? Color.black : .white)
                        .frame(width: 104, height: 104).background(Circle().fill(lime))
                }
                .accessibilityIdentifier("recognize.go")
            }
            Text(status).font(.system(size: 15)).foregroundStyle(Theme.secondary(scheme)).multilineTextAlignment(.center).padding(.horizontal, 24)
                .accessibilityIdentifier("recognize.status")
            if case .done(let ms) = store.phase {
                ScrollView { VStack(spacing: 10) { ForEach(ms) { MatchRow(m: $0) } }.padding(.horizontal, 16) }
            }
            Spacer(minLength: 0)
        }
        .foregroundStyle(Theme.fg(scheme))
        .frame(maxWidth: .infinity)
        .background(YMBackdrop())
        .onDisappear { store.cancel() }
    }
    private var progress: CGFloat { if case .listening(let p) = store.phase { CGFloat(p) }; return store.phase == .matching ? 1 : 0 }
    private var icon: String {
        switch store.phase { case .listening: "stop.fill"; case .matching: "ellipsis"; default: "waveform" }
    }
    private var status: String {
        switch store.phase {
        case .idle: "轻点开始，录约 10 秒，由译梦服务端识别"
        case .listening(let p): "正在听… \(Int(RecognizeStore.clipSeconds * (1 - p)) + 1) 秒"
        case .matching: "识别中…"
        case .done(let m): "找到 \(m.count) 个结果"
        case .failed(let f): f.text
        }
    }
}

private struct MatchRow: View {
    let m: CoreRecognizeMatch
    var body: some View {
        HStack(spacing: 12) {
            CoverImage(url: LibraryStore.shared.coverArtURL(id: m.cover, size: 200), points: 52)
                .frame(width: 52, height: 52).clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
            VStack(alignment: .leading, spacing: 3) {
                Text(m.title).font(.system(size: 16, weight: .semibold)).lineLimit(1).truncationMode(.middle)
                Text([m.artist, m.album].compactMap { $0 }.joined(separator: " · ")).font(.system(size: 13)).opacity(0.6)
                    .lineLimit(1).truncationMode(.middle)
            }
            Spacer(minLength: 8)
            if let s = m.song {
                // Only the play button plays.
                Button { PlayerStore.shared.play(songs: [s], startAt: 0); Haptics.tap() } label: { PlayCircle(size: 36) }.accessibilityLabel("播放 \(m.title)")
            }
        }
        .padding(.vertical, 6).ymCard(padding: 10)
    }
}
