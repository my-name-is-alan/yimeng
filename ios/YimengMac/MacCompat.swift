import SwiftUI
import AppKit

enum FirstRun {
    static let doneKey = "ym.onboardingDone"
    static var isDone: Bool {
        get { UserDefaults.standard.bool(forKey: doneKey) }
        set { UserDefaults.standard.set(newValue, forKey: doneKey) }
    }
    static func reset() {
        UserDefaults.standard.removeObject(forKey: doneKey)
        UserDefaults.standard.removeObject(forKey: "ym.sources.v1")
    }
}

struct CoreRecognizeResult: Codable, Sendable {
    var matches: [CoreRecognizeMatch] = []
}
struct CoreRecognizeMatch: Codable, Sendable, Hashable {
    var title: String?
    var artist: String?
    var album: String?
    var songId: String?
    var score: Double?
}
struct RecognizeSheet: View { var body: some View { Text("识曲仅 iOS").padding() } }

enum Multipart {
    static func body(boundary b: String, file: Data, filename: String, mime: String, fields: [(String, String)]) -> Data {
        var d = Data()
        func s(_ x: String) { d.append(Data(x.utf8)) }
        for (k, v) in fields { s("--\(b)\r\nContent-Disposition: form-data; name=\"\(k)\"\r\n\r\n\(v)\r\n") }
        s("--\(b)\r\nContent-Disposition: form-data; name=\"audio\"; filename=\"\(filename)\"\r\nContent-Type: \(mime)\r\n\r\n")
        d.append(file); s("\r\n--\(b)--\r\n")
        return d
    }
}

final class YueqianStore: ObservableObject {
    static let shared = YueqianStore()
    struct YueqianCard: Equatable {}
    @Published var card: YueqianCard? = nil
    var palette: PaperPalette { .fallback }
    var cover: PlatformImage? { nil }
    func load() async {}
    static func saveToPhotos(_ img: PlatformImage) {}
}

struct SourcesView: View {
    var embedded = false
    var initialConnect: SourceKind? = nil
    var body: some View { Text("译梦服务端 · localhost:8741").padding() }
}
struct BloomField: View {
    let base: Color
    let blobs: [Color]
    var bloom: CGFloat = 1
    var opacity: Double = 1
    var body: some View {
        GeometryReader { g in
            let w = g.size.width, h = g.size.height
            ZStack {
                base
                ForEach(0..<min(4, blobs.count), id: \.self) { i in
                    Circle().fill(blobs[i]).frame(width: w * 0.9)
                        .position(x: i % 2 == 0 ? w * 0.1 : w * 0.9, y: h * (0.15 + CGFloat(i) * 0.22))
                }
            }
            .blur(radius: 70).opacity(opacity)
            LinearGradient(colors: [.black.opacity(0.05), .black.opacity(0.25)], startPoint: .top, endPoint: .bottom)
        }
        .ignoresSafeArea()
    }
}
struct MoonwaveIcon: View, Animatable {
    var progress: CGFloat = 1
    var size: CGFloat = 140
    var animatableData: CGFloat { get { progress } set { progress = newValue } }
    var body: some View { YMBrandMark(size: size) }
}

struct CoreMe: Codable, Sendable, Equatable {
    var id: String?
    var username: String?
    var nickname: String?
    var avatarUrl: String?
}
@MainActor
final class CoreProfileStore: ObservableObject {
    static let shared = CoreProfileStore()
    @Published var me: CoreMe?
    @Published var avatar: PlatformImage?
    @Published var nickname: String = ""
    var displayName: String? { me.flatMap { ($0.nickname?.isEmpty == false ? $0.nickname : $0.username) } ?? (nickname.isEmpty ? nil : nickname) }
    func refresh() async {}
    func load(force: Bool = false) async {}
    func rename(_ n: String) async throws {}
    func clear() { me = nil; avatar = nil }
}

struct ListeningReportView: View { var body: some View { EmptyView() } }
struct LastfmView: View { var body: some View { EmptyView() } }
struct ProfileView: View {
    var body: some View { EmptyView() }
    static func upload(_ img: PlatformImage) async {}
}
struct NowPlayingView: View { var body: some View { EmptyView() } }
struct NowPlayingMore: View { var body: some View { EmptyView() } }

enum DebugSnap {
    static func install() {}
    static func schedule() {}
}

enum YMWidgetBridge {
    static func nowPlaying(_ s: Any?, playing: Bool, coverURL: URL?) {}
    static func open(_ url: URL) {}
    static func installHooks() {}
    static func fm(_ origins: [String]) {}
}

struct AvatarView: View {
    var size: CGFloat = 36
    @ObservedObject private var library = LibraryStore.shared
    var body: some View {
        MonogramAvatar(name: library.user.isEmpty ? "梦" : library.user, size: size)
    }
}
struct MonogramAvatar: View {
    let name: String
    var size: CGFloat = 36
    var seed: Int? = nil
    static let palette: [Color] = [Color(hex: 0x7a1cff), Color(hex: 0xe85cff), Color(hex: 0x3b82f6), Color(hex: 0x14b8a6), Color(hex: 0xff7a1a), Color(hex: 0xff2d7a)]
    var body: some View {
        let ch = String(name.trimmingCharacters(in: .whitespaces).first ?? "梦").uppercased()
        let i = seed ?? abs(name.unicodeScalars.reduce(0) { $0 &* 31 &+ Int($1.value) })
        let c = Self.palette[i % Self.palette.count]
        ZStack {
            Circle().fill(LinearGradient(colors: [c, c.opacity(0.7)], startPoint: .topLeading, endPoint: .bottomTrailing))
            Text(ch).font(.system(size: size * 0.44, weight: .heavy, design: .rounded)).foregroundStyle(.white)
        }
        .frame(width: size, height: size)
    }
}
struct CoverSlot { let rect: CGRect; let radius: CGFloat }
struct DoodleLayer: View {
    let track: DreamTrack
    let slot: CoverSlot
    var ambience: Ambience = .doodle
    var intensity: Double = 0.7
    var beat = true
    var body: some View { Color.clear }
}
