import SwiftUI
import CoreText

/// Real lyrics for the playing song. Core: /lyrics/best (word timing / trans / roma when present).
/// Emby/Jellyfin: their lyric endpoint (lines only). Others: none (+ hint to configure the Core).
@MainActor
final class LyricsStore: ObservableObject {
    static let shared = LyricsStore()
    enum Phase: Equatable { case idle, loading, ready, empty(String) }
    @Published private(set) var lines: [CoreLyricLine] = []
    @Published private(set) var phase: Phase = .idle
    @Published private(set) var songId: String?
    var hasTrans: Bool { lines.contains { ($0.trans ?? "").isEmpty == false } }
    var hasRoma: Bool { lines.contains { ($0.roma ?? "").isEmpty == false } }

    func load(_ s: NDSong?) {
        guard let s, s.id != songId else { return }
        songId = s.id; lines = []; phase = .loading
        Task {
            let got = await Self.fetch(s)
            guard songId == s.id else { return }
            lines = got.0; phase = got.0.isEmpty ? .empty(got.1) : .ready
        }
    }
    static func fetch(_ s: NDSong) async -> ([CoreLyricLine], String) {
        if NavidromeClient.core {
            guard let r = await CoreClient.shared.lyrics(s) else { return ([], "暂无歌词") }
            if r.found == false { return ([], r.message ?? "暂无歌词") }
            return (r.lines ?? [], r.message ?? "暂无歌词")
        }
        if NavidromeClient.emby {
            let l = await EmbyClient.shared.lyrics(s.id)
            return (l.map { CoreLyricLine(tMs: Int($0.0 * 1000), text: $0.1) }, "暂无歌词")
        }
        return ([], "暂无歌词")
    }
    /// Index of the line at `ms`.
    func index(at ms: Int) -> Int { lines.lastIndex { $0.tMs <= ms } ?? 0 }
}

/// Lyrics UI for real playback: word-by-word highlighter when `words` exist, else line by line.
/// Translation / romaji only render when present and toggled on.
struct LiveLyricsView: View {
    let time: Double
    let accent: Color
    var onConfigureCore: () -> Void = {}
    var onFonts: () -> Void = {}
    /// Mac split NP: no dark panel / chips / desaturation — lyrics sit on DreamField.
    var bare = false
    @ObservedObject private var store = LyricsStore.shared
    @ObservedObject private var fonts = LyricFontStore.shared
    @AppStorage("ym.lyricTrans") private var showTrans = true
    @AppStorage("ym.lyricRoma") private var showRoma = false
    /// Pinch-to-size, persisted (0.7…1.6).
    @AppStorage("ym.lyricScale") private var scale: Double = 1
    @GestureState private var pinch: CGFloat = 1
    private var k: CGFloat { CGFloat(min(1.6, max(0.7, scale * Double(pinch)))) }

    var body: some View {
        let ms = Int(time * 1000)
        let idx = store.index(at: ms)
        ZStack {
            if !bare {
                Color(red: 12 / 255, green: 10 / 255, blue: 14 / 255).opacity(0.52).ignoresSafeArea()
            }
            switch store.phase {
            case .ready:
                GeometryReader { g in
                    // Scroll by anchor, not by a fixed per-line height: wrapped or short lines made the
                    // fixed-step offset drift past the content (blank view after resuming mid-song).
                    ScrollViewReader { proxy in
                        ScrollView(.vertical, showsIndicators: false) {
                            VStack(alignment: .leading, spacing: 22) {
                                ForEach(Array(store.lines.enumerated()), id: \.offset) { i, l in
                                    line(l, ms: ms, i: i, idx: idx)
                                        .frame(maxWidth: .infinity, alignment: .leading).id(i)
                                        .contentShape(Rectangle())
                                        .onTapGesture { Haptics.tick(); PlayerStore.shared.seek(to: Double(l.tMs) / 1000 + 0.01) }
                                }
                            }
                            .padding(.horizontal, bare ? 4 : 28)
                            .padding(.vertical, g.size.height * (bare ? 0.28 : 0.5))
                        }
                        .scrollDisabled(true)
                        .onAppear { proxy.scrollTo(idx, anchor: UnitPoint(x: 0, y: 0.36)) }
                        .onChange(of: store.lines.count) { proxy.scrollTo(idx, anchor: UnitPoint(x: 0, y: 0.36)) }
                        .onChange(of: idx) { _, n in withAnimation(.smooth(duration: 0.6)) { proxy.scrollTo(n, anchor: UnitPoint(x: 0, y: 0.36)) } }
                    }
                    .frame(width: g.size.width, height: g.size.height)
                    .simultaneousGesture(MagnificationGesture()
                        .updating($pinch) { v, st, _ in st = v }
                        .onEnded { v in scale = min(1.6, max(0.7, scale * Double(v))) })
                    .mask(LinearGradient(stops: [.init(color: .clear, location: 0.08), .init(color: .black, location: 0.2),
                                                 .init(color: .black, location: 0.8), .init(color: .clear, location: 0.9)], startPoint: .top, endPoint: .bottom))
                }
            case .loading, .idle:
                DoodleLoader()
            case .empty(let msg):
                LyricsEmpty(message: msg, showCoreHint: !NavidromeClient.core, onConfigure: onConfigureCore)
            }
            if !bare {
                VStack {
                    HStack(spacing: 8) {
                        Spacer()
                        if store.hasTrans { chip("译", on: showTrans) { showTrans.toggle() } }
                        if store.hasRoma { chip("罗", on: showRoma) { showRoma.toggle() } }
                        chip("Aa", on: false) { onFonts() }
                    }.padding(.horizontal, 20).padding(.top, 70)
                    Spacer()
                }
            }
        }
        .saturation(bare ? 1 : 0.55)
    }
    private var lineStep: CGFloat { 62 + (showTrans && store.hasTrans ? 10 : 0) + (showRoma && store.hasRoma ? 10 : 0) }

    private func chip(_ t: String, on: Bool, _ a: @escaping () -> Void) -> some View {
        Button(action: a) {
            Text(t).font(.system(size: 12, weight: .semibold)).frame(width: 30, height: 24)
                .background(Capsule().fill(Color.white.opacity(on ? 0.28 : 0.1)))
        }.buttonStyle(.plain)
    }

    @ViewBuilder func line(_ l: CoreLyricLine, ms: Int, i: Int, idx: Int) -> some View {
        let active = i == idx
        let past = i < idx
        let d = abs(i - idx)
        // Past/current → theme accent (slowly advances); future → dim.
        let fg: Color = (past || active) ? accent.opacity(past ? 0.9 : 1) : Color.primary.opacity(bare ? 0.4 : 0.42)
        let useWords = (l.words ?? []).isEmpty == false && (active || past)
        VStack(alignment: .leading, spacing: 3) {
            Group {
                if useWords, let w = l.words {
                    // Word colors come from AttributedString — do not override with foregroundStyle.
                    Text(Self.karaoke(w, ms: ms, accent: accent, forcePast: past))
                        .font(fonts.font(size: (active ? 24 : 21) * k))
                } else {
                    Text(l.text)
                        .font(fonts.font(size: (active ? 24 : 21) * k))
                        .foregroundStyle(fg)
                }
            }
            .shadow(color: active && bare ? accent.opacity(0.25) : .clear, radius: 12)
            .background(alignment: .leading) {
                if active && (l.words ?? []).isEmpty && !bare {
                    RoundedRectangle(cornerRadius: 4).fill(accent.opacity(0.32)).padding(.vertical, 4).padding(.horizontal, -4)
                }
            }
            if showRoma, let r = l.roma, !r.isEmpty { Text(r).font(.system(size: 12 * k)).opacity(0.55) }
            if showTrans, let t = l.trans, !t.isEmpty { Text(t).font(.system(size: 13 * k)).opacity(0.6) }
        }
        .opacity(past || active ? 1 : (d == 1 ? 0.7 : 0.45))
        .blur(radius: d >= 3 ? 2.5 : (d == 2 ? 1 : 0))
        // Only ease on line-index change — animating on ms/80 made every karaoke tick flash.
        .animation(.smooth(duration: 0.45), value: idx)
    }

    /// Sung/past = accent; current word = accent; future = dim. Softly advances with playback.
    static func karaoke(_ words: [CoreLyricWord], ms: Int, accent: Color, forcePast: Bool = false) -> AttributedString {
        var out = AttributedString()
        for w in words {
            var a = AttributedString(w.text)
            if forcePast {
                a.foregroundColor = accent.opacity(0.9)
            } else {
                let end = w.tMs + (w.dMs ?? 300)
                if ms >= end {
                    a.foregroundColor = accent          // sung
                } else if ms >= w.tMs {
                    a.foregroundColor = accent          // current (whole word; line anim softens jump)
                } else {
                    a.foregroundColor = Color.white.opacity(0.4)
                }
            }
            out += a
        }
        return out
    }
}

struct LyricsEmpty: View {
    let message: String
    var showCoreHint: Bool
    var onConfigure: () -> Void
    var body: some View {
        VStack(spacing: 14) {
            ZStack {
                Crayon(shape: MoonShape(), color: .white).frame(width: 54, height: 54).offset(x: -18, y: -6)
                Crayon(shape: WaveShape(), color: .white.opacity(0.6)).frame(width: 96, height: 12).offset(y: 40).rotationEffect(.degrees(-5))
                Crayon(shape: HeartShape(), color: Color(hex: 0xff5fa8)).frame(width: 26, height: 22).offset(x: 34, y: -26)
            }.frame(height: 120)
            Text(message.isEmpty ? "暂无歌词" : message).font(.system(size: 17, weight: .semibold)).opacity(0.8)
            if showCoreHint {
                Button(action: onConfigure) {
                    Text("配置译梦服务端可自动获取歌词").font(.system(size: 13)).underline().opacity(0.6)
                }.buttonStyle(.plain)
            }
        }
    }
}

// MARK: - Lyric fonts (OFL). Two bundled as-is; two large ones fetched on demand, unmodified, from their official releases.

@MainActor
final class LyricFontStore: ObservableObject {
    static let shared = LyricFontStore()
    struct Option: Identifiable, Hashable {
        let id: String; let name: String; let ps: String?; let file: String?; let remote: URL?; let sizeLabel: String
        let license: String; let licenseFile: String
    }
    static let options: [Option] = [
        .init(id: "system", name: "系统字体", ps: nil, file: nil, remote: nil, sizeLabel: "", license: "", licenseFile: ""),
        .init(id: "smiley", name: "得意黑 Smiley Sans", ps: "SmileySans-Oblique", file: "SmileySans-Oblique.otf", remote: nil, sizeLabel: "内置",
              license: "SIL OFL 1.1 · © atelierAnchor", licenseFile: "OFL-SmileySans"),
        .init(id: "kuaile", name: "站酷快乐体 ZCOOL KuaiLe", ps: "ZCOOLKuaiLe-Regular", file: "ZCOOLKuaiLe-Regular.ttf", remote: nil, sizeLabel: "内置",
              license: "SIL OFL 1.1 · © The ZCOOL KuaiLe Project Authors", licenseFile: "OFL-ZCOOLKuaiLe"),
        .init(id: "wenkai", name: "霞鹜文楷 LXGW WenKai", ps: "LXGWWenKai-Regular", file: "LXGWWenKai-Regular.ttf",
              remote: URL(string: "https://github.com/lxgw/LxgwWenKai/releases/latest/download/LXGWWenKai-Regular.ttf"), sizeLabel: "约 25 MB · 按需下载",
              license: "SIL OFL 1.1 · © LXGW", licenseFile: "OFL-LXGWWenKai"),
        .init(id: "shserif", name: "思源宋体 Source Han Serif", ps: "SourceHanSerifCN-Light", file: "SourceHanSerifCN-Light.otf",
              remote: URL(string: "https://github.com/adobe-fonts/source-han-serif/raw/release/SubsetOTF/CN/SourceHanSerifCN-Light.otf"), sizeLabel: "约 11 MB · 按需下载",
              license: "SIL OFL 1.1 · © Adobe", licenseFile: "OFL-SourceHanSerif"),
    ]
    @Published var selected: String = UserDefaults.standard.string(forKey: "ym.lyricFont") ?? "system" {
        didSet { UserDefaults.standard.set(selected, forKey: "ym.lyricFont") }
    }
    @Published private(set) var downloading: String?
    @Published private(set) var available: Set<String> = ["system"]

    private static var dir: URL {
        let d = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0].appendingPathComponent("fonts", isDirectory: true)
        try? FileManager.default.createDirectory(at: d, withIntermediateDirectories: true); return d
    }
    init() { registerAll() }
    private func registerAll() {
        for o in Self.options { if let u = localURL(o) { register(u); available.insert(o.id) } }
        if !available.contains(selected) { selected = "system" }
    }
    func localURL(_ o: Option) -> URL? {
        guard let f = o.file else { return nil }
        if o.remote == nil { return Bundle.main.url(forResource: (f as NSString).deletingPathExtension, withExtension: (f as NSString).pathExtension) }
        let u = Self.dir.appendingPathComponent(f)
        return FileManager.default.fileExists(atPath: u.path) ? u : nil
    }
    private func register(_ u: URL) { CTFontManagerRegisterFontsForURL(u as CFURL, .process, nil) }
    func download(_ o: Option) async {
        guard let r = o.remote, let f = o.file, downloading == nil else { return }
        downloading = o.id; defer { downloading = nil }
        do {
            let (tmp, resp) = try await URLSession.shared.download(from: r)
            guard (resp as? HTTPURLResponse)?.statusCode == 200 else { throw URLError(.badServerResponse) }
            let dest = Self.dir.appendingPathComponent(f)
            try? FileManager.default.removeItem(at: dest)
            try FileManager.default.moveItem(at: tmp, to: dest)
            register(dest); available.insert(o.id); selected = o.id
        } catch { SheetCenter.shared.say("字体下载失败") }
    }
    func font(size: CGFloat) -> Font {
        guard let o = Self.options.first(where: { $0.id == selected }), let ps = o.ps, available.contains(o.id) else { return .system(size: size, weight: .bold) }
        return .custom(ps, size: size)
    }
}

/// Font picker with live preview on the real lyric lines of the playing song.
struct LyricFontPicker: View {
    @ObservedObject private var fonts = LyricFontStore.shared
    @ObservedObject private var lyrics = LyricsStore.shared
    @ObservedObject private var player = PlayerStore.shared
    var body: some View {
        let sample = lyrics.lines.map(\.text).filter { !$0.isEmpty }.prefix(2)
        let preview = sample.isEmpty ? [player.song?.title ?? "", player.song?.album ?? ""].filter { !$0.isEmpty } : Array(sample)
        NavigationStack {
            YMPage("歌词字体") {
                VStack(alignment: .leading, spacing: 8) {
                    ForEach(preview, id: \.self) { Text($0).font(fonts.font(size: 22)).lineLimit(1).truncationMode(.middle) }
                }
                .frame(maxWidth: .infinity, alignment: .leading).padding(.vertical, 14).ymCard().padding(.top, 12)
                YMSection(title: "歌词字体", footer: "字体均为 SIL Open Font License 1.1 开源授权，许可见 我的 › 开源许可。") {
                    ForEach(Array(LyricFontStore.options.enumerated()), id: \.element.id) { i, o in
                        if i > 0 { YMDivider() }
                        Button {
                            if fonts.available.contains(o.id) { fonts.selected = o.id; Haptics.tick() } else { Task { await fonts.download(o) } }
                        } label: {
                            HStack {
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(o.name).font(fonts.available.contains(o.id) && o.ps != nil ? .custom(o.ps!, size: 17) : .system(size: 17))
                                    if !o.sizeLabel.isEmpty { Text(o.sizeLabel).font(.system(size: 11)).opacity(0.55) }
                                }
                                Spacer()
                                if fonts.downloading == o.id { ProgressView() }
                                else if fonts.selected == o.id { Image(systemName: "checkmark").font(.system(size: 14, weight: .bold)).foregroundStyle(Theme.lime) }
                                else if !fonts.available.contains(o.id) { Image(systemName: "icloud.and.arrow.down").opacity(0.55) }
                            }.frame(minHeight: 54).contentShape(Rectangle())
                        }.buttonStyle(.plain)
                    }
                }
            }
        }
    }
}

/// 关于 › 开源许可: font notices (full license text from the bundle).
struct LicensesView: View {
    var body: some View {
        YMPage("开源许可") {
            YMSection {
                ForEach(Array(LyricFontStore.options.filter { $0.ps != nil }.enumerated()), id: \.element.id) { i, o in
                    if i > 0 { YMDivider() }
                    NavigationLink {
                        YMPage(o.name, back: true) {
                            Text(Self.text(o.licenseFile)).font(.system(size: 12, design: .monospaced)).padding(.vertical, 14).ymCard().padding(.top, 12)
                        }
                    } label: { YMRow(icon: "doc.text", o.name, o.license, chevron: true) }.buttonStyle(.plain)
                }
            }
        }
    }
    static func text(_ name: String) -> String {
        Bundle.main.url(forResource: name, withExtension: "txt").flatMap { try? String(contentsOf: $0, encoding: .utf8) } ?? "SIL Open Font License 1.1"
    }
}
